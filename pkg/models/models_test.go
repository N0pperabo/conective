package models

import (
	"testing"
)

func TestIsCDNIPProtocol(t *testing.T) {
	tests := []struct {
		proto string
		want  bool
	}{
		{"psiphon", true},
		{"Psiphon", true},
		{"CDN IP", true},
		{"cdn ip", true},
		{"cdn-ip", true},
		{"cdnip", true},
		{"clean-ip", true},
		{"clean ip", true},
		{"vless", false},
		{"vmess", false},
		{"trojan", false},
		{"shadowsocks", false},
		{"", false},
	}

	for _, tc := range tests {
		if got := IsCDNIPProtocol(tc.proto); got != tc.want {
			t.Errorf("IsCDNIPProtocol(%q) = %v, want %v", tc.proto, got, tc.want)
		}
	}
}

func TestIsCDNIPTag(t *testing.T) {
	tests := []struct {
		tag  string
		want bool
	}{
		{"CDN IP", true},
		{"cdn ip", true},
		{"cdn-ip", true},
		{"clean-ip", true},
		{"psiphon", true},
		{"cdn-fronting", true},
		{"vip", false},
		{"gaming", false},
		{"", false},
	}

	for _, tc := range tests {
		if got := IsCDNIPTag(tc.tag); got != tc.want {
			t.Errorf("IsCDNIPTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}

func TestNormalizeProtocol(t *testing.T) {
	if got := NormalizeProtocol("psiphon"); got != ProtocolCDNIP {
		t.Errorf("NormalizeProtocol(psiphon) = %q, want %q", got, ProtocolCDNIP)
	}
	if got := NormalizeProtocol("CDN IP"); got != ProtocolCDNIP {
		t.Errorf("NormalizeProtocol(CDN IP) = %q, want %q", got, ProtocolCDNIP)
	}
	if got := NormalizeProtocol("cdn-ip"); got != ProtocolCDNIP {
		t.Errorf("NormalizeProtocol(cdn-ip) = %q, want %q", got, ProtocolCDNIP)
	}
	if got := NormalizeProtocol("vless"); got != "vless" {
		t.Errorf("NormalizeProtocol(vless) = %q, want 'vless'", got)
	}
}

func TestConfigTagAndCDNIPMethods(t *testing.T) {
	cfg := &Config{
		Protocol: "vless",
		Tags:     "fast, gaming",
	}

	if cfg.HasTag("CDN IP") {
		t.Errorf("expected HasTag(CDN IP) = false initially")
	}
	if cfg.IsCDNIP() {
		t.Errorf("expected IsCDNIP() = false initially")
	}

	cfg.AddTag("CDN IP")
	if !cfg.HasTag("CDN IP") {
		t.Errorf("expected HasTag(CDN IP) = true after AddTag")
	}
	if !cfg.HasTag("cdn ip") {
		t.Errorf("expected HasTag(cdn ip) = true (case-insensitive)")
	}
	if !cfg.IsCDNIP() {
		t.Errorf("expected IsCDNIP() = true after adding CDN IP tag")
	}

	// Adding duplicate tag should not duplicate
	cfg.AddTag("CDN IP")
	count := 0
	for _, tName := range []string{"fast", "gaming", "CDN IP"} {
		if cfg.HasTag(tName) {
			count++
		}
	}
	if count != 3 {
		t.Errorf("expected 3 distinct tags, got %d", count)
	}

	// Psiphon protocol detection
	psiCfg := &Config{
		Protocol: "psiphon",
	}
	if !psiCfg.IsCDNIP() {
		t.Errorf("expected psiCfg.IsCDNIP() = true")
	}

	// Clean IP identity prefix detection
	cleanCfg := &Config{
		Identity: "cleanip-1.2.3.4",
		Protocol: "vless",
	}
	if !cleanCfg.IsCDNIP() {
		t.Errorf("expected cleanCfg.IsCDNIP() = true")
	}
}
