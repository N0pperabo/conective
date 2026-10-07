package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/lan"
	"freenode/pkg/updater"
	"freenode/pkg/xray"
)

func setupLanGamingTestServer(t *testing.T) (*Server, *database.DB, *xray.Runner) {
	dbPath := filepath.Join(t.TempDir(), "test_api_server.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	runner := xray.NewRunner(10808, 10809, "http://cp.cloudflare.com/generate_204")
	runner.SetDB(db)

	s := &Server{
		db:         db,
		runner:     runner,
		cleanIPMgr: cleanip.NewManager(),
		updater:    updater.NewUpdater(db, "v1.0.0"),
		mux:        http.NewServeMux(),
	}
	s.registerRoutes()
	return s, db, runner
}

func TestLANInfoEndpoint(t *testing.T) {
	s, db, _ := setupLanGamingTestServer(t)
	defer db.Close()

	req := httptest.NewRequest(http.MethodGet, "/api/system/lan-info", nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var info lan.LANInfo
	if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
		t.Fatalf("failed to unmarshal LANInfo: %v", err)
	}

	if info.Enabled != false {
		t.Errorf("expected Enabled=false initially, got true")
	}
	if info.HTTPPort != 10809 || info.SOCKSPort != 10808 {
		t.Errorf("unexpected ports: http=%d socks=%d", info.HTTPPort, info.SOCKSPort)
	}
}

func TestToggleGamingEndpoint(t *testing.T) {
	s, db, runner := setupLanGamingTestServer(t)
	defer db.Close()

	// Initial is false
	if runner.GetGamingMode() != false {
		t.Errorf("expected gamingMode initially false")
	}

	// Toggle on with explicit JSON
	body := bytes.NewBufferString(`{"enabled": true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/connection/toggle-gaming", body)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["gaming_mode"] != true {
		t.Errorf("expected gaming_mode=true, got %v", res["gaming_mode"])
	}
	if runner.GetGamingMode() != true {
		t.Errorf("expected runner gamingMode=true")
	}
	if db.GetSetting("gaming_mode", "false") != "true" {
		t.Errorf("expected db gaming_mode=true")
	}

	// Toggle off without body (toggles current state)
	req2 := httptest.NewRequest(http.MethodPost, "/api/connection/toggle-gaming", nil)
	w2 := httptest.NewRecorder()
	s.mux.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
	var res2 map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &res2)
	if res2["gaming_mode"] != false {
		t.Errorf("expected gaming_mode=false, got %v", res2["gaming_mode"])
	}
	if runner.GetGamingMode() != false {
		t.Errorf("expected runner gamingMode=false")
	}
}

func TestToggleShareLANEndpoint(t *testing.T) {
	s, db, runner := setupLanGamingTestServer(t)
	defer db.Close()

	if runner.GetShareLAN() != false {
		t.Errorf("expected shareLAN initially false")
	}

	// Toggle on
	body := bytes.NewBufferString(`{"enabled": true}`)
	req := httptest.NewRequest(http.MethodPost, "/api/connection/toggle-share-lan", body)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var res map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &res)
	if res["share_lan"] != true {
		t.Errorf("expected share_lan=true, got %v", res["share_lan"])
	}
	if runner.GetShareLAN() != true {
		t.Errorf("expected runner shareLAN=true")
	}
	if db.GetSetting("share_lan", "false") != "true" {
		t.Errorf("expected db share_lan=true")
	}

	// Now check /api/system/lan-info reflects enabled=true
	reqInfo := httptest.NewRequest(http.MethodGet, "/api/system/lan-info", nil)
	wInfo := httptest.NewRecorder()
	s.mux.ServeHTTP(wInfo, reqInfo)

	var info lan.LANInfo
	_ = json.Unmarshal(wInfo.Body.Bytes(), &info)
	if !info.Enabled {
		t.Errorf("expected LANInfo.Enabled=true")
	}
}
