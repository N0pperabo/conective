package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/subscription"
	"freenode/pkg/xray"
)

func setupTestServer(t *testing.T) (*Server, func()) {
	tmpDir, err := os.MkdirTemp("", "cleanip_api_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("failed to open database: %v", err)
	}

	runner := xray.NewRunner(10808, 10809, "http://cp.cloudflare.com/generate_204")
	runner.SetDB(db)

	s := &Server{
		db:         db,
		runner:     runner,
		cleanIPMgr: cleanip.NewManager(),
		mux:        http.NewServeMux(),
	}
	s.registerRoutes()

	cleanup := func() {
		db.Close()
		os.RemoveAll(tmpDir)
	}

	return s, cleanup
}

func TestCleanIP_API_Lifecycle(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. GET /api/tools/clean-ip/status (initial idle)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/tools/clean-ip/status", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("status code %d, expected 200", w.Code)
		}

		var prog cleanip.Progress
		if err := json.Unmarshal(w.Body.Bytes(), &prog); err != nil {
			t.Fatalf("unmarshal progress error: %v", err)
		}
		if prog.State != "idle" {
			t.Errorf("expected initial state idle, got %s", prog.State)
		}
	}

	// 2. POST /api/tools/clean-ip/start
	{
		startPayload := map[string]any{
			"workers":      5,
			"timeout_ms":   200,
			"sample_size":  10,
			"custom_cidrs": []string{"127.0.0.0/24"},
		}
		b, _ := json.Marshal(startPayload)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/start", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("start code %d, expected 200: %s", w.Code, w.Body.String())
		}
	}

	// 3. POST /api/tools/clean-ip/cancel
	{
		req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/cancel", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("cancel code %d, expected 200: %s", w.Code, w.Body.String())
		}
	}

	// 4. GET /api/tools/clean-ip/status (after cancel)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/tools/clean-ip/status", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		var prog cleanip.Progress
		_ = json.Unmarshal(w.Body.Bytes(), &prog)
		if prog.State != "cancelled" && prog.State != "completed" {
			t.Errorf("expected cancelled or completed, got %s", prog.State)
		}
	}
}

func TestCleanIP_API_Apply(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	// Insert test CDN nodes
	node1 := &models.Config{
		Identity:  "id1",
		Protocol:  "vless",
		Name:      "CDN-Node-1",
		Server:    "cdn1.mysite.com",
		Port:      443,
		Transport: "ws",
		TLS:       "tls",
		Path:      "/ws",
		RawLink:   "vless://uuid@cdn1.mysite.com:443?type=ws&security=tls#CDN-Node-1",
	}
	node2 := &models.Config{
		Identity:  "id2",
		Protocol:  "trojan",
		Name:      "CDN-Node-2",
		Server:    "cdn2.mysite.com",
		Port:      443,
		Transport: "grpc",
		TLS:       "tls",
		RawLink:   "trojan://pass@cdn2.mysite.com:443#CDN-Node-2",
	}
	// Non-CDN direct node
	node3 := &models.Config{
		Identity:  "id3",
		Protocol:  "ss",
		Name:      "Direct-SS",
		Server:    "1.2.3.4",
		Port:      8388,
		Transport: "tcp",
		TLS:       "none",
		RawLink:   "ss://base64@1.2.3.4:8388#Direct-SS",
	}

	_ = s.db.UpsertBatch([]*models.Config{node1, node2, node3})

	// Test Clean IP Direct VPN Connect (Cloudflare WARP Standalone Mode)
	cleanIP := "104.16.88.99"
	connectPayload := map[string]any{
		"ip":      cleanIP,
		"port":    2408,
		"latency": 95,
	}
	b, _ := json.Marshal(connectPayload)
	req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/connect?dry_run=true", bytes.NewReader(b))
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("connect clean ip code %d, expected 200: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Success bool           `json:"success"`
		Node    *models.Config `json:"node"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed decoding json: %v", err)
	}

	if !resp.Success || resp.Node == nil {
		t.Fatalf("expected successful connection response with node")
	}

	if resp.Node.Server != cleanIP {
		t.Errorf("expected server = %s, got %s", cleanIP, resp.Node.Server)
	}
	if !strings.Contains(resp.Node.Tags, "clean-ip") {
		t.Errorf("expected clean-ip tag, got %s", resp.Node.Tags)
	}

	// 2. Test POST /api/tools/clean-ip/save
	savePayload := map[string]any{
		"ip":      "162.159.192.1",
		"port":    443,
		"latency": 85,
		"name":    "⚡ Fast CF Clean IP",
	}
	sb, _ := json.Marshal(savePayload)
	saveReq := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/save", bytes.NewReader(sb))
	saveW := httptest.NewRecorder()
	s.mux.ServeHTTP(saveW, saveReq)

	if saveW.Code != http.StatusOK {
		t.Fatalf("save clean ip code %d, expected 200: %s", saveW.Code, saveW.Body.String())
	}

	var saveResp struct {
		Success bool           `json:"success"`
		Node    *models.Config `json:"node"`
		Message string         `json:"message"`
	}
	if err := json.Unmarshal(saveW.Body.Bytes(), &saveResp); err != nil {
		t.Fatalf("failed decoding save json: %v", err)
	}
	if !saveResp.Success || saveResp.Node == nil {
		t.Fatalf("expected successful save response")
	}
	if saveResp.Node.Server != "162.159.192.1" {
		t.Errorf("unexpected saved node server: %+v", saveResp.Node)
	}

	// Verify node exists in DB
	configs, _, err := s.db.GetConfigs(database.ConfigFilter{Search: "162.159.192.1"})
	if err != nil || len(configs) == 0 {
		t.Fatalf("expected saved node in database, err: %v, len: %d", err, len(configs))
	}

	// 3. Test POST /api/tools/clean-ip/save-all
	saveAllPayload := map[string]any{
		"ips": []map[string]any{
			{"ip": "172.64.32.1", "port": 1701, "latency": 75},
			{"ip": "104.16.1.1", "port": 4500, "latency": 80},
		},
	}
	sab, _ := json.Marshal(saveAllPayload)
	saveAllReq := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/save-all", bytes.NewReader(sab))
	saveAllW := httptest.NewRecorder()
	s.mux.ServeHTTP(saveAllW, saveAllReq)

	if saveAllW.Code != http.StatusOK {
		t.Fatalf("save-all code %d, expected 200: %s", saveAllW.Code, saveAllW.Body.String())
	}

	var saveAllResp struct {
		Success bool             `json:"success"`
		Count   int              `json:"count"`
		Nodes   []*models.Config `json:"nodes"`
	}
	if err := json.Unmarshal(saveAllW.Body.Bytes(), &saveAllResp); err != nil {
		t.Fatalf("failed decoding save-all json: %v", err)
	}
	if !saveAllResp.Success || saveAllResp.Count != 2 {
		t.Errorf("expected count = 2, got %d", saveAllResp.Count)
	}
}

func TestSettings_CleanIPAndPersistence(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. GET /api/settings (default values)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/settings failed with %d: %s", w.Code, w.Body.String())
		}

		var set models.Settings
		if err := json.Unmarshal(w.Body.Bytes(), &set); err != nil {
			t.Fatalf("failed decoding settings JSON: %v", err)
		}

		if set.CleanIPWorkers != 100 {
			t.Errorf("expected default CleanIPWorkers=100, got %d", set.CleanIPWorkers)
		}
		if set.CleanIPTimeout != 1500 {
			t.Errorf("expected default CleanIPTimeout=1500, got %d", set.CleanIPTimeout)
		}
		if set.CleanIPSampleSize != 500 {
			t.Errorf("expected default CleanIPSampleSize=500, got %d", set.CleanIPSampleSize)
		}
		if set.CleanIPPort != 443 {
			t.Errorf("expected default CleanIPPort=443, got %d", set.CleanIPPort)
		}
		if set.TestConcurrency != 100 {
			t.Errorf("expected default TestConcurrency=100, got %d", set.TestConcurrency)
		}
		if !set.AutoScan {
			t.Errorf("expected default AutoScan=true, got false")
		}
		if set.AutoScanInterval != 180 {
			t.Errorf("expected default AutoScanInterval=180, got %d", set.AutoScanInterval)
		}
	}

	// 2. POST /api/settings updating clean IP and individual scan settings without socks_port
	{
		payload := map[string]any{
			"clean_ip_workers":     250,
			"clean_ip_timeout":     2000,
			"clean_ip_sample_size": 1000,
			"clean_ip_port":        8443,
			"auto_scan":            false,
			"auto_scan_interval":   60,
			"test_concurrency":     300,
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/settings failed with %d: %s", w.Code, w.Body.String())
		}
	}

	// 3. GET /api/settings (verify updated settings)
	{
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/settings failed with %d: %s", w.Code, w.Body.String())
		}

		var set models.Settings
		if err := json.Unmarshal(w.Body.Bytes(), &set); err != nil {
			t.Fatalf("failed decoding settings JSON: %v", err)
		}

		if set.CleanIPWorkers != 250 {
			t.Errorf("expected updated CleanIPWorkers=250, got %d", set.CleanIPWorkers)
		}
		if set.CleanIPTimeout != 2000 {
			t.Errorf("expected updated CleanIPTimeout=2000, got %d", set.CleanIPTimeout)
		}
		if set.CleanIPSampleSize != 1000 {
			t.Errorf("expected updated CleanIPSampleSize=1000, got %d", set.CleanIPSampleSize)
		}
		if set.CleanIPPort != 8443 {
			t.Errorf("expected updated CleanIPPort=8443, got %d", set.CleanIPPort)
		}
		if set.TestConcurrency != 300 {
			t.Errorf("expected updated TestConcurrency=300, got %d", set.TestConcurrency)
		}
		if set.AutoScan {
			t.Errorf("expected updated AutoScan=false, got true")
		}
		if set.AutoScanInterval != 60 {
			t.Errorf("expected updated AutoScanInterval=60, got %d", set.AutoScanInterval)
		}
	}

	// 4. POST /api/settings with partial update (only clean_ip_timeout)
	{
		payload := map[string]any{
			"clean_ip_timeout": 3000,
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/settings", bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/settings partial failed with %d: %s", w.Code, w.Body.String())
		}
	}

	// 5. Verify partial update preserved previous settings
	{
		req := httptest.NewRequest(http.MethodGet, "/api/settings", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		var set models.Settings
		_ = json.Unmarshal(w.Body.Bytes(), &set)

		if set.CleanIPTimeout != 3000 {
			t.Errorf("expected CleanIPTimeout=3000, got %d", set.CleanIPTimeout)
		}
		if set.CleanIPWorkers != 250 {
			t.Errorf("expected CleanIPWorkers to remain 250, got %d", set.CleanIPWorkers)
		}
		if set.AutoScan {
			t.Errorf("expected AutoScan to remain false, got true")
		}
		if set.TestConcurrency != 300 {
			t.Errorf("expected TestConcurrency to remain 300, got %d", set.TestConcurrency)
		}
	}
}

func TestCleanIP_ProtocolAndTagQuerying(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Save a clean IP node
	node, err := cleanip.CreateOrUpdateCleanIPNodeWithName(s.db, "104.16.88.99", 45, "CF Edge Node")
	if err != nil {
		t.Fatalf("failed creating clean IP node: %v", err)
	}
	if !node.HasTag("CDN IP") {
		t.Errorf("expected node to have tag 'CDN IP', got %q", node.Tags)
	}
	if !node.IsCDNIP() {
		t.Errorf("expected node.IsCDNIP() = true")
	}

	// Also insert a normal VMess node for contrast
	vmessNode := &models.Config{
		Identity: "vmess-contrast-1",
		Protocol: "vmess",
		Name:     "Standard VMess",
		Server:   "1.1.1.1",
		Port:     443,
		Tags:     "standard",
		Status:   "working",
		Latency:  100,
	}
	_, _ = s.db.UpsertConfig(vmessNode)

	// 2. Query /api/nodes?protocol=CDN IP
	{
		req := httptest.NewRequest(http.MethodGet, "/api/nodes?protocol=CDN%20IP", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/nodes?protocol=CDN IP failed: %d", w.Code)
		}
		var resp struct {
			Items []models.Config `json:"items"`
			Total int             `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].Server != "104.16.88.99" {
			t.Errorf("expected 1 CDN IP node, got total=%d items=%d", resp.Total, len(resp.Items))
		}
	}

	// 3. Query /api/nodes?protocol=psiphon
	{
		req := httptest.NewRequest(http.MethodGet, "/api/nodes?protocol=psiphon", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/nodes?protocol=psiphon failed: %d", w.Code)
		}
		var resp struct {
			Items []models.Config `json:"items"`
			Total int             `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].Server != "104.16.88.99" {
			t.Errorf("expected 1 psiphon node, got total=%d items=%d", resp.Total, len(resp.Items))
		}
	}

	// 4. Query /api/nodes?tag=CDN IP
	{
		req := httptest.NewRequest(http.MethodGet, "/api/nodes?tag=CDN%20IP", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/nodes?tag=CDN IP failed: %d", w.Code)
		}
		var resp struct {
			Items []models.Config `json:"items"`
			Total int             `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Total != 1 || len(resp.Items) != 1 || resp.Items[0].Server != "104.16.88.99" {
			t.Errorf("expected 1 node matching tag 'CDN IP', got total=%d items=%d", resp.Total, len(resp.Items))
		}
	}

	// 5. Query /api/subscription/export?protocol=CDN IP
	{
		s.subMgr = subscription.NewManager(s.db)
		req := httptest.NewRequest(http.MethodGet, "/api/subscription/export?protocol=CDN%20IP", nil)
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("export protocol=CDN IP failed: %d", w.Code)
		}
		body := w.Body.String()
		if !strings.Contains(body, "104.16.88.99") {
			t.Errorf("export missing 104.16.88.99, got: %s", body)
		}
		if strings.Contains(body, "1.1.1.1") {
			t.Errorf("export should not contain non-CDN IP node, got: %s", body)
		}
	}
}

func TestDirectPsiphonAndEgressRegionConnect(t *testing.T) {
	s, cleanup := setupTestServer(t)
	defer cleanup()

	// 1. Direct Psiphon connect dry run with Germany (DE)
	{
		payload := map[string]any{
			"direct": true,
			"region": "DE",
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/connect?dry_run=true", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool           `json:"success"`
			Node    *models.Config `json:"node"`
			Message string         `json:"message"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal error: %v", err)
		}
		if !resp.Success || resp.Node == nil {
			t.Fatalf("expected success with node")
		}
		if resp.Node.Protocol != "psiphon" {
			t.Errorf("expected protocol psiphon, got %s", resp.Node.Protocol)
		}
		if resp.Node.Server != "direct" {
			t.Errorf("expected server direct, got %s", resp.Node.Server)
		}
		if resp.Node.Country != "DE" {
			t.Errorf("expected country DE, got %s", resp.Node.Country)
		}
		if resp.Node.CountryName != "Germany" {
			t.Errorf("expected country name Germany, got %s", resp.Node.CountryName)
		}
	}

	// 2. Clean IP with custom Egress Region (Netherlands - NL)
	{
		payload := map[string]any{
			"ip":     "23.209.210.213",
			"region": "NL",
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/connect?dry_run=true", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Success bool           `json:"success"`
			Node    *models.Config `json:"node"`
			Message string         `json:"message"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &resp)
		if resp.Node.Country != "NL" {
			t.Errorf("expected country NL, got %s", resp.Node.Country)
		}
		if resp.Node.CountryName != "Netherlands" {
			t.Errorf("expected country name Netherlands, got %s", resp.Node.CountryName)
		}
		if resp.Node.Server != "23.209.210.213" {
			t.Errorf("expected server 23.209.210.213, got %s", resp.Node.Server)
		}
	}

	// 3. Save Direct Psiphon node with UK (GB)
	{
		payload := map[string]any{
			"direct": true,
			"region": "GB",
		}
		b, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/save", bytes.NewReader(b))
		w := httptest.NewRecorder()
		s.mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		configs, total, err := s.db.GetConfigs(database.ConfigFilter{Protocol: "psiphon"})
		if err != nil || total == 0 {
			t.Fatalf("expected saved direct psiphon node in db, err=%v total=%d", err, total)
		}
		var directNode *models.Config
		for i := range configs {
			if configs[i].Server == "direct" && configs[i].Country == "GB" {
				directNode = &configs[i]
				break
			}
		}
		if directNode == nil {
			t.Fatalf("did not find saved direct psiphon node with server=direct and country=GB")
		}

		// 4. Test Node Ping on Direct Psiphon (Must not fail with DNS error for 'direct:443')
		{
			pingReq := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/nodes/test?id=%d", directNode.ID), nil)
			pingW := httptest.NewRecorder()
			s.mux.ServeHTTP(pingW, pingReq)

			if pingW.Code != http.StatusOK {
				t.Fatalf("expected 200 on testing direct psiphon node, got %d: %s", pingW.Code, pingW.Body.String())
			}
			var testedNode models.Config
			if err := json.Unmarshal(pingW.Body.Bytes(), &testedNode); err != nil {
				t.Fatalf("unmarshal error: %v", err)
			}
			if testedNode.Status != "working" {
				t.Errorf("expected direct psiphon node to be marked 'working', got %q", testedNode.Status)
			}
			if testedNode.Latency <= 0 {
				t.Errorf("expected positive latency, got %d", testedNode.Latency)
			}
		}

		// 5. Save Direct Psiphon with custom name
		{
			customPayload := map[string]any{
				"direct": true,
				"region": "NL",
				"name":   "My Custom Direct Node",
			}
			b, _ := json.Marshal(customPayload)
			saveReq := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/save", bytes.NewReader(b))
			saveW := httptest.NewRecorder()
			s.mux.ServeHTTP(saveW, saveReq)

			if saveW.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", saveW.Code, saveW.Body.String())
			}
			var saveResp struct {
				Node *models.Config `json:"node"`
			}
			_ = json.Unmarshal(saveW.Body.Bytes(), &saveResp)
			if saveResp.Node.Name != "My Custom Direct Node" {
				t.Errorf("expected custom name 'My Custom Direct Node', got %q", saveResp.Node.Name)
			}
		}

		// 6. Test Save-All with Egress Region preserves region and doesn't default to US
		{
			saveAllPayload := map[string]any{
				"region": "DE",
				"ips": []map[string]any{
					{"ip": "2.21.0.1", "port": 443, "latency": 80, "cdn": "Akamai", "sni": "a248.e.akamai.net"},
					{"ip": "92.123.102.43", "port": 443, "latency": 95, "cdn": "Akamai", "sni": "a248.e.akamai.net"},
				},
			}
			b, _ := json.Marshal(saveAllPayload)
			allReq := httptest.NewRequest(http.MethodPost, "/api/tools/clean-ip/save-all", bytes.NewReader(b))
			allW := httptest.NewRecorder()
			s.mux.ServeHTTP(allW, allReq)

			if allW.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d: %s", allW.Code, allW.Body.String())
			}
			var allResp struct {
				Success bool             `json:"success"`
				Count   int              `json:"count"`
				Nodes   []*models.Config `json:"nodes"`
			}
			_ = json.Unmarshal(allW.Body.Bytes(), &allResp)
			if allResp.Count != 2 {
				t.Errorf("expected 2 saved nodes, got %d", allResp.Count)
			}
			for _, n := range allResp.Nodes {
				if n.Country != "DE" {
					t.Errorf("node %s expected country DE, got %s", n.Server, n.Country)
				}
				if !strings.Contains(n.RawLink, "region=DE") {
					t.Errorf("node %s expected RawLink to have region=DE, got %s", n.Server, n.RawLink)
				}
			}
		}
	}
}

