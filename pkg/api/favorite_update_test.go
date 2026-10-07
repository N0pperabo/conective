package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/proxy"
	"freenode/pkg/scheduler"
	"freenode/pkg/updater"
	"freenode/pkg/v2go"
	"freenode/pkg/xray"
)

func setupFavTestServer(t *testing.T) (*Server, *database.DB) {
	dbPath := filepath.Join(t.TempDir(), "fav_test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	engine := v2go.NewEngine(db, nil)
	runner := xray.NewRunner(10808, 10809, "http://cp.cloudflare.com/generate_204")
	proxyMgr := proxy.NewManager()
	sched := scheduler.NewController(db, engine, runner, proxyMgr)

	srv := NewServer(db, engine, runner, proxyMgr, sched)
	return srv, db
}

func TestAPIFavoriteAndTag(t *testing.T) {
	srv, db := setupFavTestServer(t)
	defer db.Close()

	// Seed one config
	node := &models.Config{
		Identity: "test-node-1",
		Protocol: "vless",
		Name:     "Test Node 1",
		Server:   "1.2.3.4",
		Port:     443,
		Country:  "US",
		Status:   "working",
		RawLink:  "vless://test-1",
	}
	err := db.UpsertBatch([]*models.Config{node})
	if err != nil {
		t.Fatalf("failed to seed config: %v", err)
	}

	configs, _, _ := db.GetConfigs(database.ConfigFilter{})
	if len(configs) == 0 {
		t.Fatalf("expected at least 1 config")
	}
	nodeID := configs[0].ID

	// 1. POST /api/nodes/favorite?id=...
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/nodes/favorite?id=%d", nodeID), nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/nodes/favorite failed with status %d: %s", w.Code, w.Body.String())
	}

	var favResp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &favResp); err != nil {
		t.Fatalf("failed parsing favorite response: %v", err)
	}
	if favResp["is_favorite"] != true {
		t.Errorf("expected is_favorite=true, got %v", favResp["is_favorite"])
	}

	// 2. POST /api/nodes/tag with JSON body
	tagPayload := map[string]any{
		"id":   nodeID,
		"tags": "fast, secure",
	}
	body, _ := json.Marshal(tagPayload)
	req2 := httptest.NewRequest(http.MethodPost, "/api/nodes/tag", bytes.NewReader(body))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("POST /api/nodes/tag failed with status %d: %s", w2.Code, w2.Body.String())
	}

	var tagResp map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &tagResp); err != nil {
		t.Fatalf("failed parsing tag response: %v", err)
	}
	if tagResp["tags"] != "fast, secure" {
		t.Errorf("expected tags 'fast, secure', got %v", tagResp["tags"])
	}

	// 3. GET /api/nodes?is_favorite=true
	req3 := httptest.NewRequest(http.MethodGet, "/api/nodes?is_favorite=true", nil)
	w3 := httptest.NewRecorder()
	srv.mux.ServeHTTP(w3, req3)

	var listResp struct {
		Items []models.Config `json:"items"`
		Total int             `json:"total"`
	}
	_ = json.Unmarshal(w3.Body.Bytes(), &listResp)
	if listResp.Total != 1 || len(listResp.Items) != 1 {
		t.Errorf("expected 1 favorite node in filter, got %d", listResp.Total)
	}
}

func TestAPIUpdateEndpoints(t *testing.T) {
	srv, db := setupFavTestServer(t)
	defer db.Close()

	// 1. GET /api/system/check-update
	mockRelease := map[string]any{
		"tag_name":     "v2.0.0",
		"name":         "Version 2.0.0",
		"body":         "Major release",
		"html_url":     "https://github.com/connective-app/connective/releases/tag/v2.0.0",
		"published_at": "2026-10-05T00:00:00Z",
		"assets": []map[string]any{
			{
				"name":                 "Conective-Setup.exe",
				"browser_download_url": "https://github.com/connective-app/connective/releases/download/v2.0.0/Conective-Setup.exe",
				"size":                 50000000,
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockRelease)
	}))
	defer ts.Close()

	srv.updater.SetHTTPClient(&http.Client{
		Transport: &testRoundTripper{mockURL: ts.URL},
	})

	req := httptest.NewRequest(http.MethodGet, "/api/system/check-update", nil)
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/system/check-update failed with status %d: %s", w.Code, w.Body.String())
	}

	var relInfo updater.ReleaseInfo
	if err := json.Unmarshal(w.Body.Bytes(), &relInfo); err != nil {
		t.Fatalf("failed to decode check-update response: %v", err)
	}

	if !relInfo.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=true")
	}
	if relInfo.LatestVersion != "v2.0.0" {
		t.Errorf("expected LatestVersion=v2.0.0, got %s", relInfo.LatestVersion)
	}
}

type testRoundTripper struct {
	mockURL string
}

func (m *testRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	mockReq, err := http.NewRequest(req.Method, m.mockURL, req.Body)
	if err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(mockReq)
}
