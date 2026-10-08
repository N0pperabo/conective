package v2go

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"freenode/pkg/models"
)

func TestLiveTester_CleanIPAndPsiphonProbes(t *testing.T) {
	// Spin up a local TLS server to simulate responsive node
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer ts.Close()

	host, portStr, err := net.SplitHostPort(ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("failed to split host port: %v", err)
	}
	port, _ := strconv.Atoi(portStr)

	tester := NewLiveTester("http://cp.cloudflare.com/generate_204", 3)

	configs := []*models.Config{
		{
			Identity: "psiphon-node-1",
			Protocol: "psiphon",
			Server:   host,
			Port:     port,
		},
		{
			Identity: "cleanip-104.16.1.1",
			Protocol: "vless",
			Server:   host,
			Port:     port,
		},
		{
			Identity: "fronting-node-1",
			Protocol: "vmess",
			Source:   "Clean IP Fronting",
			Server:   host,
			Port:     port,
		},
	}

	for _, cfg := range configs {
		t.Run(cfg.Identity, func(t *testing.T) {
			latency, exitIP, _, err := tester.TestSingle(context.Background(), cfg)
			if err != nil {
				t.Fatalf("expected successful direct probe, got error: %v", err)
			}
			if latency <= 0 {
				t.Errorf("expected positive latency, got %d", latency)
			}
			if cfg.Status != "working" {
				t.Errorf("expected status 'working', got %q", cfg.Status)
			}
			if exitIP != host {
				t.Errorf("expected exitIP %q, got %q", host, exitIP)
			}
		})
	}
}
