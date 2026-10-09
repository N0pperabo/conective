package logger

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRingBufferCapacityAndOrdering(t *testing.T) {
	hub := New(5)

	for i := 1; i <= 7; i++ {
		hub.Info("test", "msg %d", i)
	}

	history := hub.GetHistory()
	if len(history) != 5 {
		t.Fatalf("expected history length 5, got %d", len(history))
	}

	// Should contain msg 3, 4, 5, 6, 7 in order
	for i, entry := range history {
		expectedMsg := fmt.Sprintf("msg %d", i+3)
		if entry.Message != expectedMsg {
			t.Errorf("entry[%d]: expected '%s', got '%s'", i, expectedMsg, entry.Message)
		}
		if entry.Source != "test" {
			t.Errorf("entry[%d]: expected source 'test', got '%s'", i, entry.Source)
		}
		if entry.Level != "info" {
			t.Errorf("entry[%d]: expected level 'info', got '%s'", i, entry.Level)
		}
	}
}

func TestRingBufferClear(t *testing.T) {
	hub := New(10)
	hub.Info("test", "hello")
	hub.Warn("test", "world")

	if len(hub.GetHistory()) != 2 {
		t.Fatalf("expected 2 items, got %d", len(hub.GetHistory()))
	}

	hub.Clear()
	history := hub.GetHistory()
	if len(history) != 0 {
		t.Fatalf("expected 0 items after clear, got %d", len(history))
	}
}

func TestPubSubSubscription(t *testing.T) {
	hub := New(50)
	ch, unsub := hub.Subscribe()

	hub.Info("psiphon", "started")

	select {
	case entry := <-ch:
		if entry.Source != "psiphon" || entry.Message != "started" {
			t.Fatalf("unexpected entry received: %+v", entry)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for subscription entry")
	}

	unsub()

	// Another message after unsubscribe
	hub.Info("psiphon", "stopped")

	select {
	case entry := <-ch:
		t.Fatalf("received unexpected entry after unsubscribe: %+v", entry)
	case <-time.After(100 * time.Millisecond):
		// Expected - no new messages
	}
}

func TestPubSubNonBlocking(t *testing.T) {
	hub := New(10)
	ch, unsub := hub.Subscribe()
	defer unsub()

	// Fill channel buffer (capacity 256) plus more without reading
	for i := 0; i < 300; i++ {
		hub.Info("flood", "item %d", i)
	}

	// Hub should not deadlock even if channel buffer overflowed
	if len(hub.GetHistory()) != 10 {
		t.Fatalf("expected 10 history items, got %d", len(hub.GetHistory()))
	}

	// Verify we can read from channel
	select {
	case <-ch:
	default:
		t.Fatal("expected at least one item in channel")
	}
}

func TestWriterFilteringAndLevels(t *testing.T) {
	hub := New(50)
	w := hub.Writer("xray", "info")

	inputs := []struct {
		text          string
		expectedLevel string
		expectedMsg   string
	}{
		{"Normal informational log line\n", "info", "Normal informational log line"},
		{"[ERROR] outbound failed to connect\n", "error", "[ERROR] outbound failed to connect"},
		{"an unhandled error occurred\n", "error", "an unhandled error occurred"},
		{"fatal: cannot bind socket\n", "error", "fatal: cannot bind socket"},
		{"[WARN] high memory pressure\n", "warn", "[WARN] high memory pressure"},
		{"warning: ping timeout to server\n", "warn", "warning: ping timeout to server"},
		{"normal text with clean default\r\n", "info", "normal text with clean default"},
	}

	for _, tc := range inputs {
		n, err := w.Write([]byte(tc.text))
		if err != nil || n != len(tc.text) {
			t.Fatalf("Write failed: n=%d err=%v", n, err)
		}
	}

	history := hub.GetHistory()
	if len(history) != len(inputs) {
		t.Fatalf("expected %d entries, got %d", len(inputs), len(history))
	}

	for i, tc := range inputs {
		if history[i].Level != tc.expectedLevel {
			t.Errorf("[%d] expected level '%s', got '%s' (msg: '%s')", i, tc.expectedLevel, history[i].Level, history[i].Message)
		}
		if history[i].Message != tc.expectedMsg {
			t.Errorf("[%d] expected message '%s', got '%s'", i, tc.expectedMsg, history[i].Message)
		}
		if history[i].Source != "xray" {
			t.Errorf("[%d] expected source 'xray', got '%s'", i, history[i].Source)
		}
	}
}

func TestWriterChunkingAndCarriageReturns(t *testing.T) {
	hub := New(10)
	w := hub.Writer("tun", "debug")

	// Write across multiple partial chunks
	_, _ = w.Write([]byte("first\r"))
	_, _ = w.Write([]byte(" part of "))
	_, _ = w.Write([]byte("line\r\nsecond line\n"))

	history := hub.GetHistory()
	if len(history) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(history))
	}

	if history[0].Message != "first part of line" {
		t.Errorf("entry 0 message: expected 'first part of line', got '%s'", history[0].Message)
	}
	if history[0].Level != "debug" {
		t.Errorf("entry 0 level: expected 'debug', got '%s'", history[0].Level)
	}
	if history[1].Message != "second line" {
		t.Errorf("entry 1 message: expected 'second line', got '%s'", history[1].Message)
	}
}

func TestStdLogCapture(t *testing.T) {
	Clear()
	InitStdLogCapture()

	log.Printf("[TestCapture] system log entry captured via log.Printf")

	history := GetHistory()
	found := false
	for _, entry := range history {
		if entry.Source == "system" && strings.Contains(entry.Message, "[TestCapture]") {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected standard log capture to find '[TestCapture]' entry in history")
	}
}

func TestConcurrentLogging(t *testing.T) {
	hub := New(100)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				hub.Info("worker", "worker %d iteration %d", worker, j)
			}
		}(i)
	}

	wg.Wait()
	history := hub.GetHistory()
	if len(history) != 100 {
		t.Fatalf("expected 100 entries, got %d", len(history))
	}
}
