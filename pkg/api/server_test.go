package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"freenode/pkg/procutil"
)

func TestHandleRunningAppsEndpoint(t *testing.T) {
	s := &Server{
		mux: http.NewServeMux(),
	}
	s.registerRoutes()

	req := httptest.NewRequest(http.MethodGet, "/api/system/running-apps", nil)
	w := httptest.NewRecorder()

	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var apps []procutil.AppInfo
	if err := json.Unmarshal(w.Body.Bytes(), &apps); err != nil {
		t.Fatalf("failed to decode JSON response: %v", err)
	}

	t.Logf("Endpoint returned %d running apps", len(apps))
	for i, app := range apps {
		if i < 5 {
			t.Logf("[%d] Name=%s, ExeName=%s, Title=%s, Path=%s", i+1, app.Name, app.ExeName, app.Title, app.Path)
		}
	}

	// Test with ?type=installed
	reqInstalled := httptest.NewRequest(http.MethodGet, "/api/system/running-apps?type=installed", nil)
	wInstalled := httptest.NewRecorder()
	s.mux.ServeHTTP(wInstalled, reqInstalled)
	if wInstalled.Code != http.StatusOK {
		t.Fatalf("expected status 200 for installed, got %d", wInstalled.Code)
	}

	// Test with ?type=all
	reqAll := httptest.NewRequest(http.MethodGet, "/api/system/running-apps?type=all", nil)
	wAll := httptest.NewRecorder()
	s.mux.ServeHTTP(wAll, reqAll)
	if wAll.Code != http.StatusOK {
		t.Fatalf("expected status 200 for all, got %d", wAll.Code)
	}
}
