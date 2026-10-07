package scheduler

import (
	"context"
	"log"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/proxy"
	"freenode/pkg/v2go"
	"freenode/pkg/xray"
)

type Controller struct {
	db          *database.DB
	engine      *v2go.Engine
	runner      *xray.Runner
	proxyMgr    *proxy.Manager
	mu          sync.Mutex
	ctx         context.Context
	cancelFn    context.CancelFunc
	scanTicker  *time.Ticker
	checkTicker *time.Ticker

	autoScanEnabled bool
	autoScanMinutes int
	failoverEnabled bool
	maxFailover     int
	failCount       int
}

func NewController(
	db *database.DB,
	engine *v2go.Engine,
	runner *xray.Runner,
	proxyMgr *proxy.Manager,
) *Controller {
	ctx, cancel := context.WithCancel(context.Background())
	autoScan := db.GetSetting("auto_scan", "true") == "true"
	interval, _ := strconv.Atoi(db.GetSetting("auto_scan_interval", "180"))
	if interval <= 0 {
		interval = 180
	}
	failover := db.GetSetting("auto_failover", "true") == "true"
	maxFailover, _ := strconv.Atoi(db.GetSetting("max_failover_tries", "3"))
	if maxFailover <= 0 {
		maxFailover = 3
	}

	return &Controller{
		db:              db,
		engine:          engine,
		runner:          runner,
		proxyMgr:        proxyMgr,
		ctx:             ctx,
		cancelFn:        cancel,
		autoScanEnabled: autoScan,
		autoScanMinutes: interval,
		failoverEnabled: failover,
		maxFailover:     maxFailover,
	}
}

func (c *Controller) Start() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.checkTicker = time.NewTicker(20 * time.Second)
	go c.healthLoop()
	go c.memoryLoop()

	if c.autoScanEnabled && c.autoScanMinutes > 0 {
		c.scanTicker = time.NewTicker(time.Duration(c.autoScanMinutes) * time.Minute)
		go c.scanLoop()
	}
}

func (c *Controller) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cancelFn != nil {
		c.cancelFn()
	}
	if c.checkTicker != nil {
		c.checkTicker.Stop()
	}
	if c.scanTicker != nil {
		c.scanTicker.Stop()
	}
}

func (c *Controller) ConfigureAutoScan(enabled bool, minutes int) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.autoScanEnabled = enabled
	if minutes > 0 {
		c.autoScanMinutes = minutes
	}

	if c.scanTicker != nil {
		c.scanTicker.Stop()
		c.scanTicker = nil
	}

	if c.autoScanEnabled && c.autoScanMinutes > 0 {
		c.scanTicker = time.NewTicker(time.Duration(c.autoScanMinutes) * time.Minute)
		go c.scanLoop()
	}
}

func (c *Controller) scanLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.scanTicker.C:
			if !c.engine.IsScanning() {
				log.Println("[Scheduler] Starting automatic background scan...")
				_ = c.engine.StartScan(v2go.ScanOptions{
					Concurrency: 100,
					TimeoutSec:  5,
				})
			}
		}
	}
}

func (c *Controller) healthLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-c.checkTicker.C:
			c.checkActiveConnection()
		}
	}
}

func (c *Controller) memoryLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			// If not actively scanning, proactively reclaim unused memory back to Windows OS
			if !c.engine.IsScanning() {
				c.db.CheckpointWAL()
				runtime.GC()
				debug.FreeOSMemory()
			}
		}
	}
}

func (c *Controller) checkActiveConnection() {
	if !c.runner.IsRunning() {
		c.mu.Lock()
		c.failCount = 0
		c.mu.Unlock()
		return
	}

	activeNode := c.runner.GetActiveNode()
	if activeNode == nil {
		return
	}

	// Psiphon handles its own internal routing, server hopping, and reconnection logic.
	// Never trigger automatic failover reconnect loops when Psiphon is active.
	if activeNode.Protocol == "psiphon" || activeNode.Source == "Clean IP Fronting" || strings.HasPrefix(activeNode.Identity, "cleanip-") || c.runner.IsPsiphon() {
		return
	}

	// Verify health
	status := c.runner.GetStatus()
	if !status.Connected {
		c.handleFailover(activeNode)
	}
}

func (c *Controller) handleFailover(failedNode *models.Config) {
	c.mu.Lock()
	if !c.failoverEnabled {
		c.mu.Unlock()
		return
	}

	if failedNode != nil && (failedNode.Protocol == "psiphon" || failedNode.Source == "Clean IP Fronting" || strings.HasPrefix(failedNode.Identity, "cleanip-")) {
		c.mu.Unlock()
		log.Printf("[Failover] Psiphon active; skipping automatic failover to prevent reconnect loop.")
		return
	}
	if c.runner.IsPsiphon() {
		c.mu.Unlock()
		log.Printf("[Failover] Psiphon runner active; skipping automatic failover.")
		return
	}

	maxTries := c.maxFailover
	if maxTries <= 0 {
		maxTries = 3
	}
	c.failCount++
	currentTries := c.failCount
	c.mu.Unlock()

	if currentTries > maxTries {
		log.Printf("[Failover] Reached maximum failover tries (%d). Disconnecting.", maxTries)
		_ = c.runner.Disconnect()
		_ = c.proxyMgr.Disable()
		return
	}

	log.Printf("[Failover] Active node %s seems dead. Attempting failover (%d/%d)...",
		failedNode.Name, currentTries, maxTries)

	// Mark failed node as dead in DB
	_ = c.db.UpdateTestResult(failedNode.Identity, -1, "dead", "", "", "", "", 0, -1, "", 0, "")

	// Fetch backup working nodes
	candidates, _, err := c.db.GetConfigs(database.ConfigFilter{
		Status:    "working",
		SortBy:    "score",
		SortOrder: "DESC",
		Limit:     10,
	})
	if err != nil || len(candidates) == 0 {
		log.Println("[Failover] No backup working nodes available in database.")
		return
	}

	// Iteratively try available backup candidates up to maxTries without recursion
	for _, cand := range candidates {
		if cand.Identity == failedNode.Identity || cand.Protocol == "psiphon" || cand.Source == "Clean IP Fronting" || strings.HasPrefix(cand.Identity, "cleanip-") {
			continue
		}
		cCopy := cand
		log.Printf("[Failover] Switching to backup node: %s (%s)", cCopy.Name, cCopy.Server)
		if err := c.runner.Connect(&cCopy); err != nil {
			log.Printf("[Failover] Failed to connect to backup node %s: %v", cCopy.Name, err)
			_ = c.db.UpdateTestResult(cCopy.Identity, -1, "dead", "", "", "", "", 0, -1, "", 0, "")
			c.mu.Lock()
			c.failCount++
			if c.failCount > maxTries {
				c.mu.Unlock()
				break
			}
			c.mu.Unlock()
			continue
		}

		c.mu.Lock()
		c.failCount = 0
		c.mu.Unlock()

		// Ensure system proxy remains active
		_ = c.proxyMgr.Enable(c.runner.GetStatus().HTTPPort)
		log.Printf("[Failover] Successfully switched to node: %s", cCopy.Name)
		return
	}

	log.Printf("[Failover] All backup candidates exhausted or max tries reached. Disconnecting.")
	_ = c.runner.Disconnect()
	_ = c.proxyMgr.Disable()
}
