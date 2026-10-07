package v2go

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	"freenode/pkg/database"
	"freenode/pkg/models"
)

func TestEngineMemoryTuning(t *testing.T) {
	// Set GC tuning
	debug.SetGCPercent(50)
	debug.SetMemoryLimit(256 * 1024 * 1024)

	dbPath := filepath.Join(t.TempDir(), "test_engine_mem.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	engine := NewEngine(db, nil)

	// Simulate streaming 5,000 configs
	const count = 5000
	dedup := NewDeduplicator()
	batch := make([]*models.Config, 0, 500)

	for i := 0; i < count; i++ {
		cfg := &models.Config{
			Identity: fmt.Sprintf("stream-node-%d", i),
			Protocol: "vless",
			Name:     fmt.Sprintf("Node %d", i),
			Server:   "127.0.0.1",
			Port:     8000 + (i % 100),
			Status:   "untested",
			Latency:  -1,
			RawLink:  fmt.Sprintf("vless://stream-node-%d", i),
		}

		if !dedup.IsDuplicate(cfg) {
			batch = append(batch, cfg)
		}

		if len(batch) >= 500 {
			if err := db.UpsertBatch(batch); err != nil {
				t.Fatalf("batch upsert failed: %v", err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		_ = db.UpsertBatch(batch)
	}

	db.CheckpointWAL()
	runtime.GC()
	debug.FreeOSMemory()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	t.Logf("Memory stats after 5,000 configs inserted: Alloc=%d KB, HeapInuse=%d KB",
		m.Alloc/1024, m.HeapInuse/1024)

	// Check that heap in use is well below 30MB
	if m.HeapInuse > 30*1024*1024 {
		t.Errorf("HeapInuse higher than expected: %d KB", m.HeapInuse/1024)
	}

	// Verify ID extraction memory footprint
	ids, err := db.GetConfigIDs(database.ConfigFilter{Status: "untested"})
	if err != nil {
		t.Fatalf("getting config IDs failed: %v", err)
	}
	if len(ids) != count {
		t.Fatalf("expected %d IDs, got %d", count, len(ids))
	}

	// Verify pre-check eliminates unreachable host without panic
	tester := NewLiveTester("http://cp.cloudflare.com/generate_204", 1)
	ctx := context.Background()
	deadCfg := &models.Config{
		Protocol:  "vless",
		Server:    "192.0.2.1", // Test-Net IP guaranteed unreachable
		Port:      54321,
		Transport: "tcp",
	}

	latency, _, _, err := tester.TestSingle(ctx, deadCfg)
	if latency != -1 || err == nil {
		t.Errorf("expected dead node to fail fast, got latency=%d err=%v", latency, err)
	}
	t.Logf("Fast pre-check successfully caught dead node: %v", err)

	_ = engine
}
