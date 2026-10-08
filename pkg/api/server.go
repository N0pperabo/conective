package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"freenode/pkg/autostart"
	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/gaming"
	"freenode/pkg/lan"
	"freenode/pkg/models"
	"freenode/pkg/procutil"
	"freenode/pkg/proxy"
	"freenode/pkg/scheduler"
	"freenode/pkg/subscription"
	"freenode/pkg/tun"
	"freenode/pkg/updater"
	"freenode/pkg/v2go"
	"freenode/pkg/xray"
)

type Server struct {
	db         *database.DB
	engine     *v2go.Engine
	runner     *xray.Runner
	proxyMgr   *proxy.Manager
	scheduler  *scheduler.Controller
	subMgr     *subscription.Manager
	cleanIPMgr *cleanip.Manager
	updater    *updater.Updater
	mux        *http.ServeMux
}

func NewServer(
	db *database.DB,
	engine *v2go.Engine,
	runner *xray.Runner,
	proxyMgr *proxy.Manager,
	sched *scheduler.Controller,
) *Server {
	cleanMgr := cleanip.NewManager()
	if engine != nil && engine.Geo() != nil {
		cleanMgr.SetGeoResolver(engine.Geo())
	} else if geo := cleanip.GetDefaultGeoResolver(); geo != nil {
		cleanMgr.SetGeoResolver(geo)
	}

	s := &Server{
		db:         db,
		engine:     engine,
		runner:     runner,
		proxyMgr:   proxyMgr,
		scheduler:  sched,
		subMgr:     subscription.NewManager(db),
		cleanIPMgr: cleanMgr,
		updater:    updater.NewUpdater(db, updater.CurrentVersion),
		mux:        http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) Handler() http.Handler {
	return s.mux
}

func (s *Server) registerRoutes() {
	// Status & Live Stats
	s.mux.HandleFunc("/api/status", s.handleStatus)

	// Scan Engine control
	s.mux.HandleFunc("/api/scan/start", s.handleScanStart)
	s.mux.HandleFunc("/api/scan/cancel", s.handleScanCancel)

	// Nodes
	s.mux.HandleFunc("/api/nodes", s.handleNodes)
	s.mux.HandleFunc("/api/nodes/test", s.handleTestSingleNode)
	s.mux.HandleFunc("/api/nodes/ipdata", s.handleNodeIPData)
	s.mux.HandleFunc("/api/nodes/check-all-trust", s.handleCheckAllTrust)
	s.mux.HandleFunc("/api/nodes/ping-all", s.handlePingAll)
	s.mux.HandleFunc("/api/nodes/ping-cancel", s.handlePingCancel)
	s.mux.HandleFunc("/api/nodes/connect", s.handleConnect)
	s.mux.HandleFunc("/api/nodes/disconnect", s.handleDisconnect)
	s.mux.HandleFunc("/api/nodes/delete", s.handleDeleteNode)
	s.mux.HandleFunc("/api/nodes/clear", s.handleClearNodes)
	s.mux.HandleFunc("/api/nodes/favorite", s.handleToggleFavorite)
	s.mux.HandleFunc("/api/nodes/tag", s.handleSetTags)
	s.mux.HandleFunc("/api/connection/toggle-tun", s.handleToggleTun)
	s.mux.HandleFunc("/api/connection/toggle-gaming", s.handleToggleGaming)
	s.mux.HandleFunc("/api/connection/toggle-share-lan", s.handleToggleShareLAN)
	s.mux.HandleFunc("/api/system/lan-info", s.handleLANInfo)

	// Sources
	s.mux.HandleFunc("/api/sources", s.handleSources)
	s.mux.HandleFunc("/api/sources/save", s.handleSaveSource)
	s.mux.HandleFunc("/api/sources/delete", s.handleDeleteSource)

	// History
	s.mux.HandleFunc("/api/history", s.handleHistory)
	s.mux.HandleFunc("/api/history/clear", s.handleClearHistory)

	// Settings
	s.mux.HandleFunc("/api/settings", s.handleSettings)

	// Subscription Import & Export
	s.mux.HandleFunc("/api/sub/export", s.handleExportSub)
	s.mux.HandleFunc("/api/sub/import", s.handleImportSub)
	s.mux.HandleFunc("/api/subscription/export", s.handleExportSub)
	s.mux.HandleFunc("/api/subscription/import", s.handleImportSub)

	// Server-Sent Events (SSE) for Real-Time UI updates
	s.mux.HandleFunc("/api/events", s.handleEventsSSE)

	// Graceful App Termination & Updates
	s.mux.HandleFunc("/api/system/exit", s.handleSystemExit)
	s.mux.HandleFunc("/api/system/check-update", s.handleCheckUpdate)
	s.mux.HandleFunc("/api/system/apply-update", s.handleApplyUpdate)

	// Split Tunneling Visual App Picker
	s.mux.HandleFunc("/api/system/running-apps", s.handleRunningApps)

	// Cloudflare Clean IP Scanner & Standalone VPN Connect
	s.mux.HandleFunc("/api/tools/clean-ip/start", s.handleCleanIPStart)
	s.mux.HandleFunc("/api/tools/clean-ip/cancel", s.handleCleanIPCancel)
	s.mux.HandleFunc("/api/tools/clean-ip/status", s.handleCleanIPStatus)
	s.mux.HandleFunc("/api/tools/clean-ip/connect", s.handleCleanIPConnect)
	s.mux.HandleFunc("/api/tools/clean-ip/save", s.handleCleanIPSave)
	s.mux.HandleFunc("/api/tools/clean-ip/save-all", s.handleCleanIPSaveAll)
}

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	stats, _ := s.db.GetStats()
	connStatus := s.runner.GetStatus()
	sysProxySetting := s.db.GetSetting("system_proxy", "true") == "true"
	connStatus.SystemProxy = sysProxySetting

	scanProgress := s.engine.GetProgress()

	writeJSON(w, http.StatusOK, map[string]any{
		"connection": connStatus,
		"scan":       scanProgress,
		"stats":      stats,
	})
}

func (s *Server) handleScanStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	concurrency, _ := strconv.Atoi(s.db.GetSetting("test_concurrency", "100"))
	timeout, _ := strconv.Atoi(s.db.GetSetting("test_timeout_sec", "5"))
	endpoint := s.db.GetSetting("test_endpoint", "http://cp.cloudflare.com/generate_204")

	err := s.engine.StartScan(v2go.ScanOptions{
		Concurrency: concurrency,
		TimeoutSec:  timeout,
		Endpoint:    endpoint,
	})
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Scan started"})
}

func (s *Server) handleScanCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.engine.CancelScan()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Scan cancelled"})
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	maxLatency, _ := strconv.Atoi(q.Get("max_latency"))
	minScore, _ := strconv.Atoi(q.Get("min_score"))

	if limit <= 0 || limit > 500 {
		limit = 100
	}

	sortBy := q.Get("sort_by")
	if sortBy == "" {
		sortBy = "latency"
	}

	var isFav *bool
	if favStr := q.Get("is_favorite"); favStr != "" {
		b := favStr == "true" || favStr == "1"
		isFav = &b
	}
	tag := q.Get("tag")

	filter := database.ConfigFilter{
		Status:     q.Get("status"),
		Protocol:   q.Get("protocol"),
		Country:    q.Get("country"),
		Search:     q.Get("search"),
		MaxLatency: maxLatency,
		MinScore:   minScore,
		SortBy:     sortBy,
		SortOrder:  q.Get("sort_order"),
		Limit:      limit,
		Offset:     offset,
		IsFavorite: isFav,
		Tag:        tag,
	}

	configs, total, err := s.db.GetConfigs(filter)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  configs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

func (s *Server) handleTestSingleNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid node id")
		return
	}

	node, err := s.db.GetConfigByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}

	wasWorking := node.Status == "working" || node.Score > 0

	var latency int = -1
	var exitIP string
	var ipData *v2go.IPDataDetails
	var testErr error

	isPsiphonOrCleanIP := node.IsCDNIP() || node.Protocol == "psiphon" || strings.HasPrefix(node.Identity, "cleanip-") || node.Source == "Clean IP Fronting"

	if isPsiphonOrCleanIP {
		port := node.Port
		if port <= 0 {
			port = 443
		}
		addr := net.JoinHostPort(node.Server, strconv.Itoa(port))
		dialer := &net.Dialer{Timeout: 3 * time.Second}
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{
				ServerName:         "cp.cloudflare.com",
				InsecureSkipVerify: true,
			},
		}

		start := time.Now()
		conn, dialErr := tlsDialer.DialContext(r.Context(), "tcp", addr)
		if dialErr != nil {
			conn, dialErr = dialer.DialContext(r.Context(), "tcp", addr)
		}

		if dialErr == nil {
			_ = conn.Close()
			latency = int(time.Since(start).Milliseconds())
			if latency <= 0 {
				latency = 1
			}
			exitIP = node.Server
			if node.ExitIP != "" {
				exitIP = node.ExitIP
			}
		} else {
			latency = -1
			testErr = dialErr
		}
	} else {
		timeout, _ := strconv.Atoi(s.db.GetSetting("test_timeout_sec", "5"))
		endpoint := s.db.GetSetting("test_endpoint", "http://cp.cloudflare.com/generate_204")

		tester := v2go.NewLiveTester(endpoint, timeout)
		latency, exitIP, ipData, testErr = tester.TestSingle(r.Context(), node)
	}

	_ = testErr

	status := "dead"
	score := 0
	trustScore := -1
	riskLevel := ""
	threatsCount := 0
	org := ""

	if latency > 0 {
		status = "working"
		score = 90
		if ipData != nil {
			trustScore = ipData.TrustScore
			riskLevel = ipData.RiskLevel
			threatsCount = ipData.ThreatsCount
			org = ipData.Organisation
		} else {
			trustScore = node.TrustScore
			riskLevel = node.RiskLevel
			threatsCount = node.ThreatsCount
			org = node.Organisation
		}
		_ = s.db.UpdateTestResult(node.Identity, latency, status, exitIP, "", "", "", score, trustScore, riskLevel, threatsCount, org)
	} else {
		if wasWorking {
			// Do NOT set score = 0 or corrupt the node's previous identity if it was already working; update last_tested
			_ = s.db.UpdateLastTested(id)
		} else {
			_ = s.db.UpdateTestResult(node.Identity, -1, "dead", "", "", "", "", 0, -1, "", 0, "")
		}
	}

	updated, _ := s.db.GetConfigByID(id)
	if updated == nil {
		updated = node
	}

	writeJSON(w, http.StatusOK, updated)
}

func (s *Server) handleNodeIPData(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid node id")
		return
	}

	node, err := s.db.GetConfigByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}

	targetIP := node.ExitIP
	if targetIP == "" {
		targetIP = node.Server
	}

	details, err := v2go.QueryIPData(r.Context(), nil, targetIP)
	if err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("Failed querying ipdata.co: %v", err))
		return
	}

	// Persist to database
	_ = s.db.UpdateIPData(node.ID, details.IP, details.TrustScore, details.RiskLevel, details.ThreatsCount, details.Organisation)

	writeJSON(w, http.StatusOK, details)
}

func (s *Server) handleCheckAllTrust(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	go func() {
		configs, _, err := s.db.GetConfigs(database.ConfigFilter{
			Status: "working",
			Limit:  200,
		})
		if err != nil {
			return
		}

		for _, cfg := range configs {
			targetIP := cfg.ExitIP
			if targetIP == "" {
				targetIP = cfg.Server
			}
			details, err := v2go.QueryIPData(context.Background(), nil, targetIP)
			if err == nil && details != nil {
				_ = s.db.UpdateIPData(cfg.ID, details.IP, details.TrustScore, details.RiskLevel, details.ThreatsCount, details.Organisation)
			}
			time.Sleep(250 * time.Millisecond) // avoid exceeding rate limits
		}
	}()

	writeJSON(w, http.StatusOK, map[string]string{"message": "Background trust score check started"})
}

func (s *Server) handleConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	idStr := r.URL.Query().Get("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid id")
		return
	}

	node, err := s.db.GetConfigByID(id)
	if err != nil {
		writeErr(w, http.StatusNotFound, "node not found")
		return
	}

	// Connect internal Xray
	if err := s.runner.Connect(node); err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("Failed to connect: %v", err))
		return
	}

	// Enable Windows system proxy if enabled in settings or for Psiphon / CDN IP nodes
	sysProxyEnabled := s.db.GetSetting("system_proxy", "true") == "true" || node.Protocol == "psiphon" || node.IsCDNIP()
	if sysProxyEnabled && s.proxyMgr != nil {
		status := s.runner.GetStatus()
		_ = s.proxyMgr.Enable(status.HTTPPort)
	}

	_, _ = s.db.RecordConnectionStart(node.ID, node.Server, node.Protocol)

	writeJSON(w, http.StatusOK, s.runner.GetStatus())
}

func (s *Server) handleDisconnect(w http.ResponseWriter, r *http.Request) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[Server] Panic during handleDisconnect: %v", rec)
		}
	}()

	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	if s.runner != nil {
		_ = s.runner.Disconnect()
	}
	if s.proxyMgr != nil {
		_ = s.proxyMgr.Disable()
	}

	if s.runner != nil {
		writeJSON(w, http.StatusOK, s.runner.GetStatus())
	} else {
		writeJSON(w, http.StatusOK, map[string]any{"connected": false, "state": "disconnected"})
	}
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if id > 0 {
		_ = s.db.DeleteConfig(id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func (s *Server) handleClearNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	_ = s.db.ClearAllConfigs()
	writeJSON(w, http.StatusOK, map[string]string{"message": "all configs cleared"})
}

func (s *Server) handleToggleFavorite(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var id int64
	idStr := r.URL.Query().Get("id")
	if idStr != "" {
		id, _ = strconv.ParseInt(idStr, 10, 64)
	}
	if id == 0 && r.Body != nil {
		var req struct {
			ID int64 `json:"id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		id = req.ID
	}
	if id <= 0 {
		writeErr(w, http.StatusBadRequest, "valid node id is required")
		return
	}

	isFav, err := s.db.ToggleFavorite(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"is_favorite": isFav,
	})
}

func (s *Server) handleSetTags(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		ID   int64  `json:"id"`
		Tags string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		idStr := r.URL.Query().Get("id")
		id, _ := strconv.ParseInt(idStr, 10, 64)
		req.ID = id
		req.Tags = r.URL.Query().Get("tags")
	}

	if req.ID <= 0 {
		writeErr(w, http.StatusBadRequest, "valid node id is required")
		return
	}

	if err := s.db.SetNodeTags(req.ID, req.Tags); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"tags":    req.Tags,
	})
}

func (s *Server) handleSources(w http.ResponseWriter, r *http.Request) {
	sources, err := s.db.GetSources()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleSaveSource(w http.ResponseWriter, r *http.Request) {
	var src models.Source
	if err := json.NewDecoder(r.Body).Decode(&src); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	if src.Name == "" || src.URL == "" {
		writeErr(w, http.StatusBadRequest, "name and url are required")
		return
	}
	src.URL = v2go.NormalizeURL(src.URL)
	if src.Format == "" {
		src.Format = "auto"
	}

	if src.ID > 0 {
		_ = s.db.UpdateSource(src)
	} else {
		src.Enabled = true
		_, _ = s.db.AddSource(src)
	}

	sources, _ := s.db.GetSources()
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleDeleteSource(w http.ResponseWriter, r *http.Request) {
	idStr := r.URL.Query().Get("id")
	id, _ := strconv.ParseInt(idStr, 10, 64)
	if id > 0 {
		_ = s.db.DeleteSource(id)
	}
	sources, _ := s.db.GetSources()
	writeJSON(w, http.StatusOK, sources)
}

func (s *Server) handleHistory(w http.ResponseWriter, r *http.Request) {
	sessions, err := s.db.GetScanSessions()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleClearHistory(w http.ResponseWriter, r *http.Request) {
	_ = s.db.ClearScanHistory()
	writeJSON(w, http.StatusOK, map[string]string{"message": "history cleared"})
}

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		concurrency, _ := strconv.Atoi(s.db.GetSetting("test_concurrency", "100"))
		timeout, _ := strconv.Atoi(s.db.GetSetting("test_timeout_sec", "5"))
		endpoint := s.db.GetSetting("test_endpoint", "http://cp.cloudflare.com/generate_204")
		socksPort, _ := strconv.Atoi(s.db.GetSetting("socks_port", "10808"))
		httpPort, _ := strconv.Atoi(s.db.GetSetting("http_port", "10809"))
		sysProxy := s.db.GetSetting("system_proxy", "true") == "true"
		tunMode := s.db.GetSetting("tun_mode", "false") == "true"
		shareLAN := s.db.GetSetting("share_lan", "false") == "true"
		gamingMode := s.db.GetSetting("gaming_mode", "false") == "true"
		autoScan := s.db.GetSetting("auto_scan", "true") == "true"
		autoScanInterval, _ := strconv.Atoi(s.db.GetSetting("auto_scan_interval", "180"))
		autoFailover := s.db.GetSetting("auto_failover", "true") == "true"
		maxFailover, _ := strconv.Atoi(s.db.GetSetting("max_failover_tries", "3"))

		cleanIPWorkers, _ := strconv.Atoi(s.db.GetSetting("clean_ip_workers", "100"))
		if cleanIPWorkers <= 0 {
			cleanIPWorkers = 100
		}
		cleanIPTimeout, _ := strconv.Atoi(s.db.GetSetting("clean_ip_timeout", "1500"))
		if cleanIPTimeout <= 0 {
			cleanIPTimeout = 1500
		}
		cleanIPSampleSize, _ := strconv.Atoi(s.db.GetSetting("clean_ip_sample_size", "500"))
		if cleanIPSampleSize <= 0 {
			cleanIPSampleSize = 500
		}
		cleanIPPort, _ := strconv.Atoi(s.db.GetSetting("clean_ip_port", "443"))
		if cleanIPPort <= 0 {
			cleanIPPort = 443
		}

		routingMode := s.db.GetSetting("routing_mode", "blacklist")
		if routingMode == "bypass_iran" || routingMode == "" {
			routingMode = "blacklist"
		} else if routingMode == "global" {
			routingMode = "proxy_all"
		} else if routingMode == "proxy_apps_only" {
			routingMode = "whitelist"
		}
		directDomains := s.db.GetSetting("direct_domains", "regexp:.*\\.ir$\nshaparak.ir\ndigikala.com\ndivar.ir\nsnapp.ir\ntorob.com\nvarzesh3.com\ntelewebion.com\nbale.ai\neitaa.com\nrubika.ir\naparat.com\nfilimo.com")
		proxyDomains := s.db.GetSetting("proxy_domains", "google.com\nyoutube.com\ntwitter.com\nx.com\nt.me\ntelegram.org\ninstagram.com\nfacebook.com\ndiscord.com")
		directApps := s.db.GetSetting("direct_apps", "cs2.exe\nvalorant.exe\ndota2.exe\nleagueclient.exe\nidman.exe")
		proxyApps := s.db.GetSetting("proxy_apps", "telegram.exe\ndiscord.exe\nchrome.exe\nmsedge.exe\nfirefox.exe\nspotify.exe")
		blockDomains := s.db.GetSetting("block_domains", "")
		updateRepo := s.db.GetSetting("update_repo", "N0pperabo/conective")
		startWithWindows := s.db.GetSetting("start_with_windows", "false") == "true" || autostart.IsEnabled()
		minimizeToTray := s.db.GetSetting("minimize_to_tray", "true") == "true"

		settings := models.Settings{
			TestConcurrency:   concurrency,
			TestTimeoutSec:    timeout,
			TestEndpoint:      endpoint,
			SocksPort:         socksPort,
			HTTPPort:          httpPort,
			SystemProxy:       sysProxy,
			TunMode:           tunMode,
			GamingMode:        gamingMode,
			ShareLAN:          shareLAN,
			AutoScan:          autoScan,
			AutoScanInterval:  autoScanInterval,
			AutoFailover:      autoFailover,
			MaxFailoverTries:  maxFailover,
			StartWithWindows:  startWithWindows,
			MinimizeToTray:    minimizeToTray,
			CleanIPWorkers:    cleanIPWorkers,
			CleanIPTimeout:    cleanIPTimeout,
			CleanIPSampleSize: cleanIPSampleSize,
			CleanIPPort:       cleanIPPort,
			RoutingMode:       routingMode,
			DirectDomains:     directDomains,
			ProxyDomains:      proxyDomains,
			DirectApps:        directApps,
			ProxyApps:         proxyApps,
			BlockDomains:      blockDomains,
			UpdateRepo:        updateRepo,
		}
		writeJSON(w, http.StatusOK, settings)
		return
	}

	if r.Method == http.MethodPost {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			writeErr(w, http.StatusBadRequest, "failed to read body")
			return
		}

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(bodyBytes, &raw); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid json")
			return
		}

		var set models.Settings
		_ = json.Unmarshal(bodyBytes, &set)

		if _, ok := raw["update_repo"]; ok && set.UpdateRepo != "" {
			_ = s.db.SetSetting("update_repo", set.UpdateRepo)
		}

		if _, ok := raw["test_concurrency"]; ok && set.TestConcurrency > 0 {
			_ = s.db.SetSetting("test_concurrency", strconv.Itoa(set.TestConcurrency))
		}
		if _, ok := raw["test_timeout_sec"]; ok && set.TestTimeoutSec > 0 {
			_ = s.db.SetSetting("test_timeout_sec", strconv.Itoa(set.TestTimeoutSec))
		}
		if _, ok := raw["test_endpoint"]; ok && set.TestEndpoint != "" {
			if strings.Contains(set.TestEndpoint, "gstatic") {
				set.TestEndpoint = "http://cp.cloudflare.com/generate_204"
			}
			_ = s.db.SetSetting("test_endpoint", set.TestEndpoint)
		}
		if _, ok := raw["socks_port"]; ok && set.SocksPort > 0 {
			_ = s.db.SetSetting("socks_port", strconv.Itoa(set.SocksPort))
		}
		if _, ok := raw["http_port"]; ok && set.HTTPPort > 0 {
			_ = s.db.SetSetting("http_port", strconv.Itoa(set.HTTPPort))
		}

		if _, ok := raw["system_proxy"]; ok {
			_ = s.db.SetSetting("system_proxy", strconv.FormatBool(set.SystemProxy))
		}
		if _, ok := raw["tun_mode"]; ok {
			_ = s.db.SetSetting("tun_mode", strconv.FormatBool(set.TunMode))
			if s.runner != nil {
				_ = s.runner.SetTunMode(set.TunMode)
			}
		}
		if _, ok := raw["share_lan"]; ok {
			_ = s.db.SetSetting("share_lan", strconv.FormatBool(set.ShareLAN))
			if s.runner != nil {
				s.runner.SetShareLAN(set.ShareLAN)
			}
		}
		if _, ok := raw["gaming_mode"]; ok {
			_ = s.db.SetSetting("gaming_mode", strconv.FormatBool(set.GamingMode))
			if s.runner != nil {
				s.runner.SetGamingMode(set.GamingMode)
			}
		}
		if _, ok := raw["auto_scan"]; ok {
			_ = s.db.SetSetting("auto_scan", strconv.FormatBool(set.AutoScan))
		}
		if _, ok := raw["auto_scan_interval"]; ok && set.AutoScanInterval > 0 {
			_ = s.db.SetSetting("auto_scan_interval", strconv.Itoa(set.AutoScanInterval))
		}
		if _, ok1 := raw["auto_scan"]; ok1 || raw["auto_scan_interval"] != nil {
			if s.scheduler != nil {
				autoScan := s.db.GetSetting("auto_scan", "true") == "true"
				interval, _ := strconv.Atoi(s.db.GetSetting("auto_scan_interval", "180"))
				s.scheduler.ConfigureAutoScan(autoScan, interval)
			}
		}
		if _, ok := raw["auto_failover"]; ok {
			_ = s.db.SetSetting("auto_failover", strconv.FormatBool(set.AutoFailover))
		}
		if _, ok := raw["max_failover_tries"]; ok && set.MaxFailoverTries > 0 {
			_ = s.db.SetSetting("max_failover_tries", strconv.Itoa(set.MaxFailoverTries))
		}

		// Clean IP settings
		if _, ok := raw["clean_ip_workers"]; ok && set.CleanIPWorkers > 0 {
			_ = s.db.SetSetting("clean_ip_workers", strconv.Itoa(set.CleanIPWorkers))
		}
		if _, ok := raw["clean_ip_timeout"]; ok && set.CleanIPTimeout > 0 {
			_ = s.db.SetSetting("clean_ip_timeout", strconv.Itoa(set.CleanIPTimeout))
		}
		if _, ok := raw["clean_ip_sample_size"]; ok && set.CleanIPSampleSize > 0 {
			_ = s.db.SetSetting("clean_ip_sample_size", strconv.Itoa(set.CleanIPSampleSize))
		}
		if _, ok := raw["clean_ip_port"]; ok && set.CleanIPPort > 0 {
			_ = s.db.SetSetting("clean_ip_port", strconv.Itoa(set.CleanIPPort))
		}

		// Autostart and Minimize-to-tray settings
		if _, ok := raw["minimize_to_tray"]; ok {
			_ = s.db.SetSetting("minimize_to_tray", strconv.FormatBool(set.MinimizeToTray))
		}
		if _, ok := raw["start_with_windows"]; ok {
			_ = s.db.SetSetting("start_with_windows", strconv.FormatBool(set.StartWithWindows))
			if err := autostart.SetEnabled(set.StartWithWindows); err != nil {
				fmt.Printf("[Warning] Failed to update Windows autostart: %v\n", err)
			}
		}

		// Routing & Split Tunneling
		if set.RoutingMode != "" {
			_ = s.db.SetSetting("routing_mode", set.RoutingMode)
			if _, ok := raw["direct_domains"]; ok {
				_ = s.db.SetSetting("direct_domains", set.DirectDomains)
			}
			if _, ok := raw["proxy_domains"]; ok {
				_ = s.db.SetSetting("proxy_domains", set.ProxyDomains)
			}
			if _, ok := raw["direct_apps"]; ok {
				_ = s.db.SetSetting("direct_apps", set.DirectApps)
			}
			if _, ok := raw["proxy_apps"]; ok {
				_ = s.db.SetSetting("proxy_apps", set.ProxyApps)
			}
			if _, ok := raw["block_domains"]; ok {
				_ = s.db.SetSetting("block_domains", set.BlockDomains)
			}
		}

		// If connected, seamlessly reload runner with new routing rules
		if _, ok := raw["routing_mode"]; ok {
			if s.runner != nil && s.runner.IsRunning() {
				activeNode := s.runner.GetActiveNode()
				if activeNode != nil {
					go func(n *models.Config) {
						_ = s.runner.Connect(n)
					}(activeNode)
				}
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"message": "settings saved"})
		return
	}

	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
}

func (s *Server) handlePingAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	targetStatus := r.URL.Query().Get("status")
	concurrency, _ := strconv.Atoi(s.db.GetSetting("test_concurrency", "100"))
	timeout, _ := strconv.Atoi(s.db.GetSetting("test_timeout_sec", "5"))
	endpoint := s.db.GetSetting("test_endpoint", "http://cp.cloudflare.com/generate_204")

	opts := v2go.ScanOptions{
		Concurrency: concurrency,
		TimeoutSec:  timeout,
		Endpoint:    endpoint,
	}

	if err := s.engine.StartPingAll(opts, targetStatus); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Ping test started"})
}

func (s *Server) handlePingCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.engine.CancelScan()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Ping test cancelled"})
}

func (s *Server) handleToggleTun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	current := s.runner.GetTunMode()
	newMode := !current
	if err := s.runner.SetTunMode(newMode); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	_ = s.db.SetSetting("tun_mode", strconv.FormatBool(newMode))
	writeJSON(w, http.StatusOK, map[string]any{
		"tun_mode": newMode,
		"is_admin": tun.IsAdmin(),
	})
}

func (s *Server) handleToggleGaming(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	newVal := !s.runner.GetGamingMode()
	if req.Enabled != nil {
		newVal = *req.Enabled
	}
	if err := gaming.ToggleGamingMode(s.db, s.runner, newVal); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"gaming_mode": newVal,
	})
}

func (s *Server) handleToggleShareLAN(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var req struct {
		Enabled *bool `json:"enabled"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	newVal := !s.runner.GetShareLAN()
	if req.Enabled != nil {
		newVal = *req.Enabled
	}
	if err := lan.ToggleLANSharing(s.db, s.runner, newVal); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"share_lan": newVal,
	})
}

func (s *Server) handleLANInfo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	httpPort, _ := strconv.Atoi(s.db.GetSetting("http_port", "10809"))
	socksPort, _ := strconv.Atoi(s.db.GetSetting("socks_port", "10808"))
	info, err := lan.GetLANInfo(s.db, httpPort, socksPort)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleExportSub(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	format := q.Get("format")
	if format == "" {
		format = "plain"
	}

	filter := database.ConfigFilter{
		Protocol: q.Get("protocol"),
		Country:  q.Get("country"),
		Tag:      q.Get("tag"),
	}

	subText, err := s.subMgr.GenerateSubscription(filter, format)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(subText))
}

func (s *Server) handleImportSub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Content string `json:"content"`
		URL     string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}

	var imported, duplicates int
	var err error

	if req.URL != "" {
		imported, duplicates, err = s.subMgr.ImportURL(req.URL, "Imported URL")
	} else if req.Content != "" {
		imported, duplicates, err = s.subMgr.ImportContent(req.Content, "Manual Import")
	} else {
		writeErr(w, http.StatusBadRequest, "empty content or url")
		return
	}

	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"imported":   imported,
		"duplicates": duplicates,
		"message":    fmt.Sprintf("Imported %d new nodes (%d duplicates)", imported, duplicates),
	})
}

func (s *Server) handleEventsSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := s.engine.Subscribe()
	defer s.engine.Unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case evt, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(evt)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		}
	}
}

func (s *Server) handleSystemExit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Shutting down FreeNode..."})
	go func() {
		time.Sleep(300 * time.Millisecond)
		if s.runner != nil && s.runner.IsRunning() {
			_ = s.runner.Disconnect()
		}
		if s.proxyMgr != nil {
			_ = s.proxyMgr.Disable()
		}
		tun.CleanupStaleWintunAdapters()
		os.Exit(0)
	}()
}

func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	info, err := s.updater.CheckUpdate(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func (s *Server) handleApplyUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		DownloadURL string `json:"download_url"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	path, err := s.updater.ApplyUpdate(r.Context(), req.DownloadURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"path":    path,
	})
}

func (s *Server) handleRunningApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	appType := r.URL.Query().Get("type")
	var apps []procutil.AppInfo
	var err error

	switch appType {
	case "installed":
		apps, err = procutil.GetInstalledApps()
	case "all":
		apps, err = procutil.GetAllApps()
	default:
		apps, err = procutil.GetRunningApps()
	}

	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if apps == nil {
		apps = []procutil.AppInfo{}
	}

	writeJSON(w, http.StatusOK, apps)
}

// Clean IP Scanner & CDN Fronting handlers

func (s *Server) handleCleanIPStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		Workers     int      `json:"workers"`
		TimeoutMs   int      `json:"timeout_ms"`
		SampleSize  int      `json:"sample_size"`
		CustomCIDRs []string `json:"custom_cidrs"`
		Port        int      `json:"port"`
	}

	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	if req.Workers <= 0 {
		req.Workers, _ = strconv.Atoi(s.db.GetSetting("clean_ip_workers", "100"))
	}
	if req.TimeoutMs <= 0 {
		req.TimeoutMs, _ = strconv.Atoi(s.db.GetSetting("clean_ip_timeout", "1500"))
	}
	if req.SampleSize <= 0 {
		req.SampleSize, _ = strconv.Atoi(s.db.GetSetting("clean_ip_sample_size", "500"))
	}
	if req.Port <= 0 {
		req.Port, _ = strconv.Atoi(s.db.GetSetting("clean_ip_port", "443"))
	}

	opts := cleanip.ScanOptions{
		Workers:     req.Workers,
		TimeoutMs:   req.TimeoutMs,
		SampleSize:  req.SampleSize,
		CustomCIDRs: req.CustomCIDRs,
		Port:        req.Port,
	}

	if err := s.cleanIPMgr.StartScan(opts); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"message": "Clean IP scan started"})
}

func (s *Server) handleCleanIPCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	s.cleanIPMgr.CancelScan()
	writeJSON(w, http.StatusOK, map[string]string{"message": "Clean IP scan cancelled"})
}

func (s *Server) handleCleanIPStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	progress := s.cleanIPMgr.GetProgress()
	writeJSON(w, http.StatusOK, progress)
}

func (s *Server) handleCleanIPConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		IP      string `json:"ip"`
		Port    int    `json:"port"`
		Latency int    `json:"latency"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	cleanIP := strings.TrimSpace(req.IP)
	if cleanIP == "" {
		best := s.cleanIPMgr.GetBestIPs(1)
		if len(best) > 0 {
			cleanIP = best[0].IP
			req.Latency = best[0].Latency
		} else {
			cleanIP = "188.114.96.1"
		}
	}

	cleanNode, err := cleanip.CreateOrUpdateCleanIPNode(s.db, cleanIP, req.Latency)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("failed setting up Clean IP tunnel: %v", err))
		return
	}

	if r.URL.Query().Get("dry_run") == "true" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "node": cleanNode, "message": "dry run"})
		return
	}

	if err := s.runner.Connect(cleanNode); err != nil {
		writeErr(w, http.StatusBadGateway, fmt.Sprintf("failed connecting via Clean IP %s: %v", cleanIP, err))
		return
	}

	lat, exitIP, err := s.runner.VerifyTunnel(12 * time.Second)
	if err == nil {
		cleanNode.Latency = lat
		cleanNode.ExitIP = exitIP
		cleanNode.Status = "working"
		_, _ = s.db.UpsertConfig(cleanNode)
	}

	if s.db.GetSetting("system_proxy", "true") == "true" && s.proxyMgr != nil {
		httpPort, _ := strconv.Atoi(s.db.GetSetting("http_port", "10809"))
		_ = s.proxyMgr.Enable(httpPort)
	}

	_, _ = s.db.RecordConnectionStart(cleanNode.ID, cleanNode.Server, cleanNode.Protocol)

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"node":    cleanNode,
		"message": fmt.Sprintf("Connected via Clean IP %s (Psiphon Fronting)", cleanIP),
	})
}

func (s *Server) handleCleanIPSave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		IP      string `json:"ip"`
		Port    int    `json:"port"`
		Latency int    `json:"latency"`
		Name    string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body")
		return
	}

	cleanIP := strings.TrimSpace(req.IP)
	if cleanIP == "" {
		writeErr(w, http.StatusBadRequest, "ip address is required")
		return
	}

	latency := req.Latency
	if latency <= 0 {
		latency = 120
	}

	cleanNode, err := cleanip.CreateOrUpdateCleanIPNodeWithName(s.db, cleanIP, latency, req.Name)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, fmt.Sprintf("failed saving clean ip config: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"node":    cleanNode,
		"message": fmt.Sprintf("Clean IP %s added to main configs", cleanIP),
	})
}

func (s *Server) handleCleanIPSaveAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req struct {
		IPs []struct {
			IP      string `json:"ip"`
			Port    int    `json:"port"`
			Latency int    `json:"latency"`
			Name    string `json:"name"`
		} `json:"ips"`
	}

	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	var savedNodes []*models.Config
	if len(req.IPs) > 0 {
		for _, item := range req.IPs {
			ip := strings.TrimSpace(item.IP)
			if ip == "" {
				continue
			}
			latency := item.Latency
			if latency <= 0 {
				latency = 120
			}
			node, err := cleanip.CreateOrUpdateCleanIPNodeWithName(s.db, ip, latency, item.Name)
			if err == nil && node != nil {
				savedNodes = append(savedNodes, node)
			}
		}
	} else {
		best := s.cleanIPMgr.GetBestIPs(50)
		for _, item := range best {
			node, err := cleanip.CreateOrUpdateCleanIPNodeWithName(s.db, item.IP, item.Latency, "")
			if err == nil && node != nil {
				savedNodes = append(savedNodes, node)
			}
		}
	}

	if len(savedNodes) == 0 {
		writeErr(w, http.StatusBadRequest, "no clean IPs found to save")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"count":   len(savedNodes),
		"nodes":   savedNodes,
		"message": fmt.Sprintf("%d Clean IP configs added to main list", len(savedNodes)),
	})
}


