package database

import (
	"fmt"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"testing"

	"freenode/pkg/models"
)

func TestChunkStreamingAndMemory(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_chunk.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Insert 2,000 test configs
	const count = 2000
	configs := make([]*models.Config, count)
	for i := 0; i < count; i++ {
		configs[i] = &models.Config{
			Identity: fmt.Sprintf("bench-identity-%d", i),
			Protocol: "vless",
			Name:     fmt.Sprintf("Node %d", i),
			Server:   fmt.Sprintf("192.168.1.%d", (i%254)+1),
			Port:     443,
			Country:  "US",
			Status:   "untested",
			Latency:  -1,
			RawLink:  fmt.Sprintf("vless://bench-identity-%d", i),
		}
	}

	if err := db.UpsertBatch(configs); err != nil {
		t.Fatalf("failed to upsert batch: %v", err)
	}

	// Verify GetConfigIDs
	ids, err := db.GetConfigIDs(ConfigFilter{Status: "untested"})
	if err != nil {
		t.Fatalf("failed to get IDs: %v", err)
	}
	if len(ids) != count {
		t.Fatalf("expected %d IDs, got %d", count, len(ids))
	}

	// Stream in chunks of 200
	const chunkSize = 200
	var streamedTotal int
	for start := 0; start < len(ids); start += chunkSize {
		end := start + chunkSize
		if end > len(ids) {
			end = len(ids)
		}
		chunkIDs := ids[start:end]
		chunkConfigs, err := db.GetConfigsByIDs(chunkIDs)
		if err != nil {
			t.Fatalf("failed to get configs by IDs: %v", err)
		}
		if len(chunkConfigs) != len(chunkIDs) {
			t.Fatalf("expected %d configs in chunk, got %d", len(chunkIDs), len(chunkConfigs))
		}
		streamedTotal += len(chunkConfigs)
	}

	if streamedTotal != count {
		t.Fatalf("expected %d streamed configs total, got %d", count, streamedTotal)
	}

	// Test WAL checkpoint and memory reclaim
	db.CheckpointWAL()
	runtime.GC()
	debug.FreeOSMemory()

	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	// HeapInuse should remain modest (under 50MB) even with 2000 records
	if m.HeapInuse > 50*1024*1024 {
		t.Errorf("HeapInuse too high: %d bytes", m.HeapInuse)
	}
}
