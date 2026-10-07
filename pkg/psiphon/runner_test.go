package psiphon

import (
	"testing"
)

func TestRunnerLifecycle(t *testing.T) {
	r := NewRunner()
	if r == nil {
		t.Fatal("expected non-nil runner")
	}

	if r.IsRunning() {
		t.Fatal("new runner should not be running")
	}

	// Calling Stop() when not started should safely clean up and not panic
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() returned error: %v", err)
	}

	if r.IsRunning() {
		t.Fatal("runner should still be stopped after Stop()")
	}
}

func TestProbeOnceNoPorts(t *testing.T) {
	r := NewRunner()
	// When ports are not set, probeOnce should fail immediately without hanging
	lat, exitIP, err := r.probeOnce()
	if err == nil {
		t.Fatalf("expected error when no ports configured, got lat=%d, ip=%s", lat, exitIP)
	}
}
