package scheduler

import (
	"path/filepath"
	"testing"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/proxy"
	"freenode/pkg/v2go"
	"freenode/pkg/xray"
)

func TestFailoverSkipsPsiphon(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("database.Open failed: %v", err)
	}
	defer db.Close()

	engine := v2go.NewEngine(db, nil)
	runner := xray.NewRunner(10808, 10809, "")
	proxyMgr := proxy.NewManager()

	ctrl := NewController(db, engine, runner, proxyMgr)

	// Test 1: failedNode with Protocol "psiphon"
	psiphonNode := &models.Config{
		Identity: "psiphon-node-1",
		Protocol: "psiphon",
		Name:     "Psiphon Clean IP",
		Server:   "104.16.1.1",
	}

	ctrl.failCount = 0
	ctrl.handleFailover(psiphonNode)
	if ctrl.failCount != 0 {
		t.Fatalf("expected failCount to remain 0 for psiphon node, got %d", ctrl.failCount)
	}

	// Test 2: failedNode with Source "Clean IP Fronting"
	cleanIPNode := &models.Config{
		Identity: "cleanip-node-2",
		Protocol: "vless",
		Source:   "Clean IP Fronting",
		Name:     "Cloudflare Fronting",
		Server:   "104.16.1.2",
	}

	ctrl.failCount = 0
	ctrl.handleFailover(cleanIPNode)
	if ctrl.failCount != 0 {
		t.Fatalf("expected failCount to remain 0 for Clean IP Fronting node, got %d", ctrl.failCount)
	}

	// Test 3: failedNode with prefix "cleanip-"
	prefixedNode := &models.Config{
		Identity: "cleanip-104.16.1.3",
		Protocol: "shadowsocks",
		Name:     "Akamai Fronting",
		Server:   "104.16.1.3",
	}

	ctrl.failCount = 0
	ctrl.handleFailover(prefixedNode)
	if ctrl.failCount != 0 {
		t.Fatalf("expected failCount to remain 0 for cleanip- prefixed node, got %d", ctrl.failCount)
	}
}
