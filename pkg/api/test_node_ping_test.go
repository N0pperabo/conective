package api

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/xray"
)

func setupNodePingTestServer(t *testing.T) (*Server, func()) {
	tmpDir, err := os.MkdirTemp("", "ping_test_*")
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

func TestHandleTestSingleNode_CleanIPSuccess(t *testing.T) {
	s, cleanup := setupNodePingTestServer(t)
	defer cleanup()

	// Spin up a local TLS server to simulate responsive Clean IP / Psiphon node
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	host, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	cleanNode := &models.Config{
		Identity:    "cleanip-test-node",
		Protocol:    "psiphon",
		Server:      host,
		Port:        port,
		Source:      "Clean IP Fronting",
		Status:      "untested",
		Score:       0,
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
	}

	_, err = s.db.UpsertConfig(cleanNode)
	if err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	stored, err := s.db.GetConfigByIdentity(cleanNode.Identity)
	if err != nil || stored == nil {
		t.Fatalf("failed to get stored config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/nodes/test?id=%d", stored.ID), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.Config
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	if updated.Status != "working" {
		t.Errorf("expected status 'working', got %q", updated.Status)
	}
	if updated.Score != 90 {
		t.Errorf("expected score 90, got %d", updated.Score)
	}
	if updated.Latency <= 0 {
		t.Errorf("expected latency > 0, got %d", updated.Latency)
	}
	if updated.Identity != "cleanip-test-node" {
		t.Errorf("identity altered: got %q", updated.Identity)
	}
}

func TestHandleTestSingleNode_WorkingNodeDoesNotCorruptOnTimeout(t *testing.T) {
	s, cleanup := setupNodePingTestServer(t)
	defer cleanup()

	// An already working node pointing to an unreachable IP
	initialTested := time.Now().Add(-10 * time.Minute)
	workingNode := &models.Config{
		Identity:    "working-node-123",
		Protocol:    "vless",
		Server:      "192.0.2.1", // unroutable test net IP
		Port:        54321,
		Status:      "working",
		Score:       95,
		Latency:     120,
		FirstSeen:   time.Now().Add(-1 * time.Hour),
		LastSeen:    time.Now(),
		LastTested:  initialTested,
	}

	_, err := s.db.UpsertConfig(workingNode)
	if err != nil {
		t.Fatalf("failed to insert config: %v", err)
	}

	stored, err := s.db.GetConfigByIdentity(workingNode.Identity)
	if err != nil || stored == nil {
		t.Fatalf("failed to get stored config: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/nodes/test?id=%d", stored.ID), nil)
	w := httptest.NewRecorder()
	s.mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated models.Config
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}

	// Verify the working node was NOT corrupted to dead or score=0
	if updated.Status != "working" {
		t.Errorf("expected status to remain 'working', got %q", updated.Status)
	}
	if updated.Score == 0 {
		t.Errorf("expected score NOT to be wiped to 0, got %d", updated.Score)
	}
	if updated.Identity != "working-node-123" {
		t.Errorf("expected identity to remain intact, got %q", updated.Identity)
	}

	// Verify in database as well
	dbNode, err := s.db.GetConfigByID(stored.ID)
	if err != nil {
		t.Fatalf("failed to fetch config from db: %v", err)
	}
	if dbNode.Status != "working" {
		t.Errorf("db record status corrupted: got %q", dbNode.Status)
	}
	if dbNode.Score == 0 {
		t.Errorf("db record score wiped to 0: got %d", dbNode.Score)
	}
	if dbNode.Identity != "working-node-123" {
		t.Errorf("db record identity corrupted: got %q", dbNode.Identity)
	}
	if dbNode.LastTested.Before(initialTested) || dbNode.LastTested.Equal(initialTested) {
		t.Errorf("expected last_tested to be updated, initial=%v, current=%v", initialTested, dbNode.LastTested)
	}
}
