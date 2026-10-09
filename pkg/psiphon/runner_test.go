package psiphon

import (
	"strings"
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

func TestShiroCdnFrontingConfigWithRegion(t *testing.T) {
	// 1. With DE region
	dataDE, err := ShiroCdnFrontingConfigWithRegion("23.209.210.213", "a248.e.akamai.net", "DE")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sDE := string(dataDE)
	if !strings.Contains(sDE, `"EgressRegion": "DE"`) {
		t.Errorf("expected EgressRegion DE in config, got:\n%s", sDE)
	}
	if !strings.Contains(sDE, "23.209.210.213") {
		t.Errorf("expected cleanIP in config")
	}

	// 2. Without region
	dataEmpty, err := ShiroCdnFrontingConfigWithRegion("23.209.210.213", "a248.e.akamai.net", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	sEmpty := string(dataEmpty)
	if strings.Contains(sEmpty, "EgressRegion") {
		t.Errorf("expected no EgressRegion when empty, got:\n%s", sEmpty)
	}

	// 3. With AUTO or ANY
	dataAuto, err := ShiroCdnFrontingConfigWithRegion("23.209.210.213", "a248.e.akamai.net", "AUTO")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(string(dataAuto), "EgressRegion") {
		t.Errorf("expected no EgressRegion when AUTO, got:\n%s", string(dataAuto))
	}
}

func TestRunnerEgressRegionGetter(t *testing.T) {
	r := NewRunner()
	if r.GetEgressRegion() != "" {
		t.Errorf("expected initial egress region empty, got %q", r.GetEgressRegion())
	}
}
