package updater

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"freenode/pkg/database"
)

func TestCompareSemver(t *testing.T) {
	tests := []struct {
		v1       string
		v2       string
		expected int
	}{
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"v2.0.0", "v1.9.9", 1},
		{"1.10.0", "1.9.0", 1},
		{"0.9.5", "1.0.0", -1},
		{"1.0.0", "1.0.0-beta", 1},
		{"1.0.0-beta", "1.0.0", -1},
	}

	for _, tt := range tests {
		got := CompareSemver(tt.v1, tt.v2)
		if got != tt.expected {
			t.Errorf("CompareSemver(%q, %q) = %d; expected %d", tt.v1, tt.v2, got, tt.expected)
		}
	}
}

func TestNormalizeRepo(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"connective-app/connective", "connective-app/connective"},
		{"https://github.com/connective-app/connective", "connective-app/connective"},
		{"https://github.com/connective-app/connective.git", "connective-app/connective"},
		{"http://www.github.com/owner/repo/", "owner/repo"},
		{"", ""},
	}

	for _, tt := range tests {
		got := NormalizeRepo(tt.input)
		if got != tt.expected {
			t.Errorf("NormalizeRepo(%q) = %q; expected %q", tt.input, got, tt.expected)
		}
	}
}

func TestCheckUpdateMockServer(t *testing.T) {
	// 1. Mock server that returns a newer release
	mockRelease := githubRelease{
		TagName:     "v1.2.0",
		Name:        "Release 1.2.0",
		Body:        "- Added Favorites and Tags\n- Added In-App Updater",
		HTMLURL:     "https://github.com/connective-app/connective/releases/tag/v1.2.0",
		PublishedAt: "2026-10-05T20:00:00Z",
		Assets: []githubAsset{
			{
				Name:               "Conective-Setup-1.2.0.exe",
				BrowserDownloadURL: "https://github.com/connective-app/connective/releases/download/v1.2.0/Conective-Setup-1.2.0.exe",
				Size:               12345678,
			},
		},
	}

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(mockRelease)
	}))
	defer ts.Close()

	dbPath := filepath.Join(t.TempDir(), "updater_test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	u := NewUpdater(db, "1.0.0")

	// Custom HTTP client rewriting requests to mock server
	transport := &mockRoundTripper{
		mockURL: ts.URL,
	}
	u.SetHTTPClient(&http.Client{Transport: transport})

	info, err := u.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate failed: %v", err)
	}

	if !info.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=true for v1.2.0 vs v1.0.0")
	}
	if info.CurrentVersion != "1.0.0" {
		t.Errorf("expected CurrentVersion=1.0.0, got %s", info.CurrentVersion)
	}
	if info.LatestVersion != "v1.2.0" {
		t.Errorf("expected LatestVersion=v1.2.0, got %s", info.LatestVersion)
	}
	if info.DownloadURL != "https://github.com/connective-app/connective/releases/download/v1.2.0/Conective-Setup-1.2.0.exe" {
		t.Errorf("unexpected DownloadURL: %s", info.DownloadURL)
	}
	if info.ReleaseNotes == "" {
		t.Errorf("expected non-empty release notes")
	}

	// 2. Same version test (no update)
	u2 := NewUpdater(db, "1.2.0")
	u2.SetHTTPClient(&http.Client{Transport: transport})
	info2, err := u2.CheckUpdate(context.Background())
	if err != nil {
		t.Fatalf("CheckUpdate failed: %v", err)
	}
	if info2.UpdateAvailable {
		t.Errorf("expected UpdateAvailable=false for v1.2.0 vs v1.2.0")
	}
}

type mockRoundTripper struct {
	mockURL string
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	mockReq, err := http.NewRequest(req.Method, m.mockURL, req.Body)
	if err != nil {
		return nil, err
	}
	return http.DefaultTransport.RoundTrip(mockReq)
}
