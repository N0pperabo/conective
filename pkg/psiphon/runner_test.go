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

func TestBuildArgs_DirectModeNoDirectShape(t *testing.T) {
	// 1. Direct mode with empty cleanIP: MUST NOT have "--psiphon-mode direct"
	args1 := BuildArgs("psiphon.exe", "", "", "", "", "", 10808, 10809)
	for i, a := range args1 {
		if a == "--psiphon-mode" && i+1 < len(args1) && args1[i+1] == "direct" {
			t.Fatalf("expected --psiphon-mode direct NOT to be present, but found in args: %v", args1)
		}
	}
	// Verify --psiphon-mode is omitted completely for direct
	for _, a := range args1 {
		if a == "--psiphon-mode" {
			t.Fatalf("expected --psiphon-mode to be omitted for direct connection, but found in args: %v", args1)
		}
	}

	// 2. Direct mode with "direct": MUST NOT have "--psiphon-mode direct"
	args2 := BuildArgs("psiphon.exe", "", "direct", "", "", "", 10808, 10809)
	for _, a := range args2 {
		if a == "--psiphon-mode" {
			t.Fatalf("expected --psiphon-mode to be omitted for direct connection, but found in args: %v", args2)
		}
	}

	// 3. Direct mode with "auto": MUST NOT have "--psiphon-mode direct"
	args3 := BuildArgs("psiphon.exe", "", "auto", "", "", "", 10808, 10809)
	for _, a := range args3 {
		if a == "--psiphon-mode" {
			t.Fatalf("expected --psiphon-mode to be omitted for auto cleanIP, but found in args: %v", args3)
		}
	}
}

func TestBuildArgs_RegionHandling(t *testing.T) {
	// 1. Empty region: should NOT contain --psiphon-region
	argsEmpty := BuildArgs("psiphon.exe", "", "direct", "", "", "", 10808, 10809)
	for _, a := range argsEmpty {
		if a == "--psiphon-region" {
			t.Fatalf("expected --psiphon-region to be omitted when region empty, but found in args: %v", argsEmpty)
		}
	}

	// 2. "AUTO" or "ANY": should NOT contain --psiphon-region
	argsAuto := BuildArgs("psiphon.exe", "", "direct", "", "", "AUTO", 10808, 10809)
	for _, a := range argsAuto {
		if a == "--psiphon-region" {
			t.Fatalf("expected --psiphon-region to be omitted when region is AUTO, but found in args: %v", argsAuto)
		}
	}

	// 3. Specific region e.g. "DE": MUST contain --psiphon-region DE
	argsDE := BuildArgs("psiphon.exe", "", "direct", "", "", "DE", 10808, 10809)
	hasRegion := false
	for i, a := range argsDE {
		if a == "--psiphon-region" && i+1 < len(argsDE) && argsDE[i+1] == "DE" {
			hasRegion = true
			break
		}
	}
	if !hasRegion {
		t.Fatalf("expected --psiphon-region DE in args, but got: %v", argsDE)
	}
}

func TestBuildArgs_CdnMode(t *testing.T) {
	// CDN fronting mode with valid clean IP: MUST contain --psiphon-cdn-ips and --psiphon-region DE
	argsCDN := BuildArgs("psiphon.exe", "", "23.209.210.213", "a248.e.akamai.net", "psiphon-akamai", "DE", 10808, 10809)
	hasCdnIPs := false
	hasRegion := false
	for i, a := range argsCDN {
		if a == "--psiphon-mode" && i+1 < len(argsCDN) && argsCDN[i+1] == "direct" {
			t.Fatalf("unexpected --psiphon-mode direct in CDN mode: %v", argsCDN)
		}
		if a == "--psiphon-cdn-ips" && i+1 < len(argsCDN) && argsCDN[i+1] == "23.209.210.213" {
			hasCdnIPs = true
		}
		if a == "--psiphon-region" && i+1 < len(argsCDN) && argsCDN[i+1] == "DE" {
			hasRegion = true
		}
	}
	if !hasCdnIPs {
		t.Fatalf("expected --psiphon-cdn-ips 23.209.210.213 in args, but got: %v", argsCDN)
	}
	if !hasRegion {
		t.Fatalf("expected --psiphon-region DE in args, but got: %v", argsCDN)
	}
}
