package xray

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"freenode/pkg/models"
	"freenode/pkg/v2go"
	xcore "github.com/xtls/xray-core/core"
	xserial "github.com/xtls/xray-core/infra/conf/serial"
)

func TestFragmentOutboundWithDialerProxy(t *testing.T) {
	node := &models.Config{
		Protocol:  "vless",
		Server:    "188.114.96.1",
		Port:      443,
		UUID:      "8546bcef-8456-41a2-82bc-d2231b42b769",
		Transport: "ws",
		TLS:       "tls",
		SNI:       "test.workers.dev",
		Host:      "test.workers.dev",
		Path:      "/",
	}

	outbound, err := v2go.ConvertToXrayOutbound(node)
	if err != nil {
		t.Fatalf("convert to outbound: %v", err)
	}

	streamSettings, ok := outbound["streamSettings"].(v2go.M)
	if !ok || streamSettings == nil {
		streamSettings = v2go.M{}
		outbound["streamSettings"] = streamSettings
	}
	sockopt, ok := streamSettings["sockopt"].(v2go.M)
	if !ok || sockopt == nil {
		sockopt = v2go.M{}
		streamSettings["sockopt"] = sockopt
	}
	sockopt["dialerProxy"] = "fragment"

	configMap := v2go.M{
		"log": v2go.M{"loglevel": "warning"},
		"inbounds": []v2go.M{
			{
				"tag":      "socks-in",
				"port":     25800,
				"listen":   "127.0.0.1",
				"protocol": "socks",
				"settings": v2go.M{"auth": "noauth"},
			},
		},
		"outbounds": []v2go.M{
			outbound,
			{
				"tag":      "fragment",
				"protocol": "freedom",
				"settings": v2go.M{
					"fragment": v2go.M{
						"packets":  "1-1",
						"length":   "100-200",
						"interval": "1-5",
					},
				},
			},
			{
				"tag":      "direct",
				"protocol": "freedom",
			},
		},
	}

	jsonBytes, err := json.Marshal(configMap)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	cfg, err := xserial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		t.Fatalf("LoadJSONConfig failed: %v", err)
	}

	instance, err := xcore.New(cfg)
	if err != nil {
		t.Fatalf("xcore.New failed: %v", err)
	}

	if err := instance.Start(); err != nil {
		t.Fatalf("instance.Start failed: %v", err)
	}
	_ = instance.Close()
}

func TestRunnerStopAndLifecycle(t *testing.T) {
	r := NewRunner(10808, 10809, "")
	if r == nil {
		t.Fatal("expected non-nil runner")
	}

	if r.IsRunning() {
		t.Fatal("new runner should not be running")
	}

	if r.IsPsiphon() {
		t.Fatal("psiphon should not be active initially")
	}

	// Calling Stop() when disconnected should not error or panic and free memory
	if err := r.Stop(); err != nil {
		t.Fatalf("Stop() failed: %v", err)
	}

	if r.IsRunning() {
		t.Fatal("runner should remain stopped")
	}
}

func TestTunAdapterNameFixed(t *testing.T) {
	r := NewRunner(10808, 10809, "")
	if r.TunAdapterName() != "ConectiveTUN" {
		t.Fatalf("expected fixed TUN adapter name ConectiveTUN, got: %s", r.TunAdapterName())
	}
}

func TestPsiphonTunConfigValidity(t *testing.T) {
	r := NewRunner(10808, 10809, "")
	tunMTU := 1500
	if r.gamingMode {
		tunMTU = 1400
	}

	configMap := v2go.M{
		"log": v2go.M{
			"loglevel": "warning",
		},
		"dns": v2go.M{
			"servers": []string{
				"1.1.1.1",
				"8.8.8.8",
			},
		},
		"inbounds": []v2go.M{
			{
				"tag":      "tun-in",
				"protocol": "tun",
				"settings": v2go.M{
					"name": "ConectiveTUN",
					"MTU":  tunMTU,
				},
				"sniffing": v2go.M{
					"enabled":      true,
					"destOverride": []string{"http", "tls", "quic"},
					"routeOnly":    false,
				},
			},
		},
		"outbounds": []v2go.M{
			{
				"tag":      "proxy",
				"protocol": "socks",
				"settings": v2go.M{
					"servers": []v2go.M{
						{
							"address": "127.0.0.1",
							"port":    r.socksPort,
						},
					},
				},
			},
			{
				"tag":      "direct",
				"protocol": "freedom",
			},
			{
				"tag":      "block",
				"protocol": "blackhole",
			},
		},
		"routing": v2go.M{
			"domainStrategy": "IPIfNonMatch",
			"rules":          r.buildRoutingRules(),
		},
	}

	jsonBytes, err := json.Marshal(configMap)
	if err != nil {
		t.Fatalf("marshalling config: %v", err)
	}

	cfg, err := xserial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		t.Fatalf("LoadJSONConfig failed for Psiphon TUN bridge config: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil parsed config")
	}
}

func TestPsiphonNodeDetection(t *testing.T) {
	nodes := []*models.Config{
		{Protocol: "psiphon", Server: "1.2.3.4"},
		{Protocol: "CDN IP", Server: "1.2.3.4"},
		{Protocol: "vless", Tags: "CDN IP", Server: "1.2.3.4"},
		{Protocol: "vless", Identity: "cleanip-123", Server: "1.2.3.4"},
		{Protocol: "vmess", Source: "Clean IP Fronting", Server: "1.2.3.4"},
	}

	for _, n := range nodes {
		isPsiphon := n.IsCDNIP() || n.Protocol == "psiphon" || strings.HasPrefix(n.Identity, "cleanip-") || n.Source == "Clean IP Fronting"
		if !isPsiphon {
			t.Errorf("node %+v should be detected as Psiphon/Clean IP node", n)
		}
	}
}


