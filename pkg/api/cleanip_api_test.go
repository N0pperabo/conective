package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/models"
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
