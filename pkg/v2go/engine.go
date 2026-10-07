package v2go

import (
	"context"
	"fmt"
	"log"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"freenode/pkg/database"
	"freenode/pkg/geoip"
	"freenode/pkg/models"
)

type EngineEvent struct {
	Type      string              `json:"type"` // e.g. "ScanStarted", "NodeTested", "ScanCompleted"
	Message   string              `json:"message"`
	Progress  models.ScanProgress `json:"progress"`
	Node      *models.Config      `json:"node,omitempty"`
	Timestamp time.Time           `json:"timestamp"`
}

type ScanOptions struct {
	Concurrency int
	TimeoutSec  int
	Endpoint    string
	CustomURLs  []string // Optional manual URLs to scan
}

type Engine struct {
	db       *database.DB
	geo      *geoip.Resolver
	mu       sync.RWMutex
	cancelFn context.CancelFunc
	running  bool

	progress models.ScanProgress

	listenersMu sync.Mutex
	listeners   []chan EngineEvent
}

func NewEngine(db *database.DB, geo *geoip.Resolver) *Engine {
	return &Engine{
		db:  db,
		geo: geo,
		progress: models.ScanProgress{
			State: "idle",
		},
	}
}

func (e *Engine) Geo() *geoip.Resolver {
	return e.geo
}

func (e *Engine) Subscribe() chan EngineEvent {
	e.listenersMu.Lock()
	defer e.listenersMu.Unlock()

	ch := make(chan EngineEvent, 100)
	e.listeners = append(e.listeners, ch)
	return ch
}

func (e *Engine) Unsubscribe(ch chan EngineEvent) {
	e.listenersMu.Lock()
	defer e.listenersMu.Unlock()

	for i, c := range e.listeners {
		if c == ch {
			e.listeners = append(e.listeners[:i], e.listeners[i+1:]...)
			close(ch)
			break
		}
	}
}

func (e *Engine) emit(evtType, msg string, node *models.Config) {
	e.mu.RLock()
	prog := e.progress
	prog.Message = msg
	e.mu.RUnlock()

	evt := EngineEvent{
		Type:      evtType,
		Message:   msg,
		Progress:  prog,
		Node:      node,
		Timestamp: time.Now(),
	}

	e.listenersMu.Lock()
	defer e.listenersMu.Unlock()

	for _, ch := range e.listeners {
		select {
		case ch <- evt:
		default: // Non-blocking if listener channel full
		}
	}
}

func (e *Engine) IsScanning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

func (e *Engine) GetProgress() models.ScanProgress {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.progress
}

func (e *Engine) CancelScan() {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running && e.cancelFn != nil {
		e.cancelFn()
		e.progress.State = "cancelled"
		e.progress.Message = "Scan cancelled by user"
	}
}

func (e *Engine) StartScan(opts ScanOptions) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("a scan is already in progress")
	}

	if opts.Concurrency <= 0 {
		opts.Concurrency = 100
	}
	if opts.TimeoutSec <= 0 {
		opts.TimeoutSec = 5
	}
	if opts.Endpoint == "" || strings.Contains(opts.Endpoint, "gstatic") {
		opts.Endpoint = "http://cp.cloudflare.com/generate_204"
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFn = cancel
	e.running = true

	// Create DB session
	sessionID, _ := e.db.CreateScanSession()

	e.progress = models.ScanProgress{
		SessionID: sessionID,
		State:     "starting",
		Message:   "Starting scan...",
	}
	e.mu.Unlock()

	e.emit("ScanStarted", "Scanning Free Nodes started", nil)

	go e.runPipeline(ctx, sessionID, opts)

	return nil
}

func (e *Engine) runPipeline(ctx context.Context, sessionID int64, opts ScanOptions) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Pipeline Panic Recovered] %v\n", r)
		}
		e.mu.Lock()
		e.running = false
		e.mu.Unlock()
	}()

	// 1. Gather sources from DB
	allSources, err := e.db.GetSources()
	if err != nil || len(allSources) == 0 {
		_ = e.db.SeedDefaultSources(DefaultSources())
		allSources, _ = e.db.GetSources()
	}

	var activeSources []models.Source
	for _, s := range allSources {
		if s.Enabled {
			activeSources = append(activeSources, s)
		}
	}

	e.mu.Lock()
	e.progress.State = "fetching"
	e.progress.SourcesTotal = len(activeSources)
	e.progress.SourcesDone = 0
	e.mu.Unlock()

	// 2. Fetch all sources
	fetcher := NewFetcher(opts.TimeoutSec * 3)
	fetchResults := fetcher.FetchAll(
		ctx,
		activeSources,
		8,
		func(s models.Source) {
			e.emit("SourceStarted", fmt.Sprintf("Fetching source: %s", s.Name), nil)
		},
		func(res FetchResult) {
			e.mu.Lock()
			e.progress.SourcesDone++
			e.progress.FetchedTotal += len(res.Lines)
			e.mu.Unlock()

			errMsg := ""
			if res.Error != nil {
				errMsg = res.Error.Error()
			}
			_ = e.db.UpdateSourceStats(res.Source.ID, len(res.Lines), 0, errMsg)

			e.emit("SourceCompleted", fmt.Sprintf("Source %s: %d configs", res.Source.Name, len(res.Lines)), nil)
		},
	)

	if ctx.Err() != nil {
		e.handleInterrupted(sessionID, "cancelled")
		return
	}

	// 3. Parse configs with bounded memory streaming (500 configs per batch directly to DB)
	e.mu.Lock()
	e.progress.State = "parsing"
	e.progress.Message = "Parsing and validating configurations..."
	e.mu.Unlock()

	dedup := NewDeduplicator()
	var totalParsed, duplicates int
	parseBatch := make([]*models.Config, 0, 500)

	for i := range fetchResults {
		fr := &fetchResults[i]
		if fr.Error != nil {
			continue
		}
		for _, line := range fr.Lines {
			cfg, err := ParseLink(line, fr.Source.Name)
			if err != nil {
				continue
			}
			totalParsed++

			if dedup.IsDuplicate(cfg) {
				duplicates++
				continue
			}

			// Quick GeoIP (non-blocking IP literal lookup)
			if e.geo != nil {
				geo := e.geo.Lookup(cfg.Server)
				cfg.Country = geo.Code
				cfg.CountryName = geo.Name
			}

			parseBatch = append(parseBatch, cfg)
			if len(parseBatch) >= 500 {
				_ = e.db.UpsertBatch(parseBatch)
				parseBatch = parseBatch[:0]
			}
		}
		// Clear raw text lines immediately to release memory
		fr.Lines = nil
	}

	if len(parseBatch) > 0 {
		_ = e.db.UpsertBatch(parseBatch)
		parseBatch = nil
	}

	// Free raw fetch results, deduplicator and checkpoint DB
	fetchResults = nil
	dedup = nil
	e.db.CheckpointWAL()
	runtime.GC()
	debug.FreeOSMemory()

	if ctx.Err() != nil {
		e.handleInterrupted(sessionID, "cancelled")
		return
	}

	// Fetch candidate IDs to test (minimal memory footprint: 240KB for 30,000 IDs)
	candidateIDs, err := e.db.GetConfigIDs(database.ConfigFilter{
		Status: "untested",
	})
	if err != nil || len(candidateIDs) == 0 {
		candidateIDs, _ = e.db.GetConfigIDs(database.ConfigFilter{})
	}

	e.mu.Lock()
	e.progress.ParsedTotal = totalParsed
	e.progress.Duplicates = duplicates
	e.progress.TestingTotal = len(candidateIDs)
	e.progress.TestingDone = 0
	e.progress.State = "testing"
	e.progress.Message = fmt.Sprintf("Live testing %d candidates with Xray-core...", len(candidateIDs))
	e.mu.Unlock()

	e.emit("ConfigsParsed", fmt.Sprintf("Parsed %d, testing %d unique candidates", totalParsed, len(candidateIDs)), nil)

	if ctx.Err() != nil {
		e.handleInterrupted(sessionID, "cancelled")
		return
	}

	// 4. Live Test candidates in streamed chunks of 300
	tester := NewLiveTester(opts.Endpoint, opts.TimeoutSec)
	var testedCount atomic.Int64
	var workingCount atomic.Int64
	var totalLatency atomic.Int64

	dbCh := make(chan database.TestResultUpdate, 500)
	var dbWg sync.WaitGroup
	dbWg.Add(1)
	go func() {
		defer dbWg.Done()
		batch := make([]database.TestResultUpdate, 0, 50)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		flush := func() {
			if len(batch) > 0 {
				_ = e.db.UpdateTestResultBatch(batch)
				batch = batch[:0]
			}
		}

		for {
			select {
			case item, ok := <-dbCh:
				if !ok {
					flush()
					return
				}
				batch = append(batch, item)
				if len(batch) >= 50 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	chunkSize := 300
	for chunkStart := 0; chunkStart < len(candidateIDs); chunkStart += chunkSize {
		if ctx.Err() != nil {
			break
		}
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(candidateIDs) {
			chunkEnd = len(candidateIDs)
		}

		chunkIDs := candidateIDs[chunkStart:chunkEnd]
		chunkConfigs, err := e.db.GetConfigsByIDs(chunkIDs)
		if err != nil || len(chunkConfigs) == 0 {
			continue
		}

		tester.TestPool(
			ctx,
			chunkConfigs,
			opts.Concurrency,
			func(res TestResult) {
				d := testedCount.Add(1)
				status := "dead"
				score := 0
				country := res.Config.Country
				countryName := res.Config.CountryName
				var stdName string

				if res.Latency > 0 {
					w := workingCount.Add(1)
					totalLatency.Add(int64(res.Latency))
					status = "working"
					score = calculateScore(res.Latency)

					if res.ExitIP != "" {
						exitGeo := e.geo.Lookup(res.ExitIP)
						if exitGeo.Code != "UN" {
							country = exitGeo.Code
							countryName = exitGeo.Name
						}
					}

					flag := geoip.Flag(country)
					stdName = fmt.Sprintf("v2go | %s %s | %s | #%d", flag, country, res.Config.Protocol, w)
					res.Config.Name = stdName
					res.Config.Country = country
					res.Config.CountryName = countryName
					res.Config.Latency = res.Latency
					res.Config.Status = status
					res.Config.Score = score
					res.Config.TrustScore = res.TrustScore
					res.Config.RiskLevel = res.RiskLevel
					res.Config.ThreatsCount = res.ThreatsCount
					res.Config.Organisation = res.Organisation
					res.Config.ExitIP = res.ExitIP
				}

				// Queue batched DB update
				dbCh <- database.TestResultUpdate{
					Identity:     res.Config.Identity,
					Latency:      res.Latency,
					Status:       status,
					ExitIP:       res.ExitIP,
					Country:      country,
					CountryName:  countryName,
					StdName:      stdName,
					Score:        score,
					TrustScore:   res.TrustScore,
					RiskLevel:    res.RiskLevel,
					ThreatsCount: res.ThreatsCount,
					Organisation: res.Organisation,
				}

				// Update progress
				e.mu.Lock()
				e.progress.TestingDone = int(d)
				e.progress.WorkingCount = int(workingCount.Load())
				if workingCount.Load() > 0 {
					e.progress.AverageLatency = int(totalLatency.Load() / workingCount.Load())
				}
				e.mu.Unlock()

				if res.Latency > 0 {
					e.emit("NodeTested", fmt.Sprintf("Node working: %s (%dms)", res.Config.Name, res.Latency), res.Config)
				}
			},
		)

		// Reclaim chunk memory and proactively return physical pages to Windows OS
		chunkConfigs = nil
		runtime.GC()
		debug.FreeOSMemory()
	}

	close(dbCh)
	dbWg.Wait()

	// Reclaim remaining memory and checkpoint WAL
	candidateIDs = nil
	e.db.CheckpointWAL()
	runtime.GC()
	debug.FreeOSMemory()

	// 5. Finalize scan session in DB
	finalStatus := "completed"
	if ctx.Err() != nil {
		finalStatus = "cancelled"
	}

	wCount := int(workingCount.Load())
	tCount := int(testedCount.Load())
	_ = e.db.UpdateScanSession(sessionID, finalStatus, e.progress.FetchedTotal, totalParsed, duplicates, tCount, wCount)

	e.mu.Lock()
	e.progress.State = finalStatus
	e.progress.Message = fmt.Sprintf("Scan finished: %d working nodes found", wCount)
	e.mu.Unlock()

	e.emit("ScanCompleted", fmt.Sprintf("Scan completed. %d working nodes found.", wCount), nil)
}

func (e *Engine) handleInterrupted(sessionID int64, status string) {
	e.mu.Lock()
	e.progress.State = status
	e.progress.Message = "Scan was cancelled"
	e.mu.Unlock()
	_ = e.db.UpdateScanSession(sessionID, status, e.progress.FetchedTotal, e.progress.ParsedTotal, e.progress.Duplicates, e.progress.TestingDone, e.progress.WorkingCount)
	e.emit("ScanCancelled", "Scan was cancelled", nil)
}

func calculateScore(latency int) int {
	if latency <= 0 {
		return 0
	}
	switch {
	case latency < 80:
		return 98
	case latency < 150:
		return 92
	case latency < 250:
		return 85
	case latency < 400:
		return 78
	case latency < 700:
		return 68
	case latency < 1200:
		return 55
	default:
		return 40
	}
}

// StartPingAll tests all existing nodes concurrently without loading all configs into RAM
func (e *Engine) StartPingAll(opts ScanOptions, targetStatus string) error {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return fmt.Errorf("a scan or ping test is already in progress")
	}

	filter := database.ConfigFilter{}
	if targetStatus != "" && targetStatus != "all" {
		filter.Status = targetStatus
	}

	ids, err := e.db.GetConfigIDs(filter)
	if err != nil || len(ids) == 0 {
		e.mu.Unlock()
		return fmt.Errorf("no configurations found in database to test")
	}

	if opts.Concurrency <= 0 {
		opts.Concurrency = 100
	}
	if opts.TimeoutSec <= 0 {
		opts.TimeoutSec = 5
	}
	if opts.Endpoint == "" || strings.Contains(opts.Endpoint, "gstatic") {
		opts.Endpoint = "http://cp.cloudflare.com/generate_204"
	}

	ctx, cancel := context.WithCancel(context.Background())
	e.cancelFn = cancel
	e.running = true

	e.progress = models.ScanProgress{
		State:        "testing",
		TestingTotal: len(ids),
		TestingDone:  0,
		WorkingCount: 0,
		Message:      fmt.Sprintf("Ping testing %d configs with %d workers...", len(ids), opts.Concurrency),
	}
	e.mu.Unlock()

	e.emit("ScanStarted", fmt.Sprintf("Ping test started for %d configs", len(ids)), nil)

	go e.runPingPipeline(ctx, ids, opts)

	return nil
}

func (e *Engine) runPingPipeline(ctx context.Context, ids []int64, opts ScanOptions) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Ping Panic Recovered] %v\n", r)
		}
		e.mu.Lock()
		e.running = false
		e.progress.State = "idle"
		e.mu.Unlock()
	}()

	tester := NewLiveTester(opts.Endpoint, opts.TimeoutSec)
	var testedCount atomic.Int64
	var workingCount atomic.Int64
	var totalLatency atomic.Int64

	dbCh := make(chan database.TestResultUpdate, 500)
	var dbWg sync.WaitGroup
	dbWg.Add(1)
	go func() {
		defer dbWg.Done()
		batch := make([]database.TestResultUpdate, 0, 50)
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()

		flush := func() {
			if len(batch) > 0 {
				_ = e.db.UpdateTestResultBatch(batch)
				batch = batch[:0]
			}
		}

		for {
			select {
			case item, ok := <-dbCh:
				if !ok {
					flush()
					return
				}
				batch = append(batch, item)
				if len(batch) >= 50 {
					flush()
				}
			case <-ticker.C:
				flush()
			}
		}
	}()

	chunkSize := 300
	for chunkStart := 0; chunkStart < len(ids); chunkStart += chunkSize {
		if ctx.Err() != nil {
			break
		}
		chunkEnd := chunkStart + chunkSize
		if chunkEnd > len(ids) {
			chunkEnd = len(ids)
		}

		chunkIDs := ids[chunkStart:chunkEnd]
		configs, err := e.db.GetConfigsByIDs(chunkIDs)
		if err != nil || len(configs) == 0 {
			continue
		}

		tester.TestPool(
			ctx,
			configs,
			opts.Concurrency,
			func(res TestResult) {
				d := testedCount.Add(1)
				status := "dead"
				score := 0
				country := res.Config.Country
				countryName := res.Config.CountryName
				stdName := res.Config.Name

				if res.Latency > 0 {
					workingCount.Add(1)
					totalLatency.Add(int64(res.Latency))
					status = "working"
					score = calculateScore(res.Latency)

					if res.ExitIP != "" && (country == "" || country == "UN") {
						exitGeo := e.geo.Lookup(res.ExitIP)
						if exitGeo.Code != "UN" {
							country = exitGeo.Code
							countryName = exitGeo.Name
						}
					}

					res.Config.Country = country
					res.Config.CountryName = countryName
					res.Config.Latency = res.Latency
					res.Config.Status = status
					res.Config.Score = score
					res.Config.TrustScore = res.TrustScore
					res.Config.RiskLevel = res.RiskLevel
					res.Config.ThreatsCount = res.ThreatsCount
					res.Config.Organisation = res.Organisation
					res.Config.ExitIP = res.ExitIP
				} else {
					res.Config.Latency = -1
					res.Config.Status = "dead"
					res.Config.Score = 0
				}

				dbCh <- database.TestResultUpdate{
					Identity:     res.Config.Identity,
					Latency:      res.Latency,
					Status:       status,
					ExitIP:       res.ExitIP,
					Country:      country,
					CountryName:  countryName,
					StdName:      stdName,
					Score:        score,
					TrustScore:   res.TrustScore,
					RiskLevel:    res.RiskLevel,
					ThreatsCount: res.ThreatsCount,
					Organisation: res.Organisation,
				}

				e.mu.Lock()
				e.progress.TestingDone = int(d)
				e.progress.WorkingCount = int(workingCount.Load())
				if workingCount.Load() > 0 {
					e.progress.AverageLatency = int(totalLatency.Load() / workingCount.Load())
				}
				e.mu.Unlock()

				if res.Latency > 0 {
					e.emit("NodeTested", fmt.Sprintf("Node working: %dms", res.Latency), res.Config)
				}
			},
		)

		// Free tested chunk configs immediately and return memory to Windows
		configs = nil
		runtime.GC()
		debug.FreeOSMemory()
	}

	close(dbCh)
	dbWg.Wait()

	ids = nil
	e.db.CheckpointWAL()
	runtime.GC()
	debug.FreeOSMemory()

	if ctx.Err() != nil {
		e.emit("ScanCancelled", "Ping test cancelled by user", nil)
		return
	}

	wCount := int(workingCount.Load())
	tCount := int(testedCount.Load())
	e.emit("ScanCompleted", fmt.Sprintf("Ping test complete! %d working of %d tested", wCount, tCount), nil)
}

