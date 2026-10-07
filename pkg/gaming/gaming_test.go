package gaming

import (
	"net"
	"path/filepath"
	"testing"

	"freenode/pkg/database"
)

func TestGamingConstants(t *testing.T) {
	if DefaultMTU != 1500 {
		t.Errorf("expected DefaultMTU 1500, got %d", DefaultMTU)
	}
	if GamingMTU != 1400 {
		t.Errorf("expected GamingMTU 1400, got %d", GamingMTU)
	}
}

func TestGamingSockoptMap(t *testing.T) {
	opts := GamingSockoptMap()
	if opts["tcpNoDelay"] != true {
		t.Errorf("expected tcpNoDelay=true, got %v", opts["tcpNoDelay"])
	}
	if opts["tcpFastOpen"] != true {
		t.Errorf("expected tcpFastOpen=true, got %v", opts["tcpFastOpen"])
	}
	if opts["tcpKeepAliveIdle"] != 15 {
		t.Errorf("expected tcpKeepAliveIdle=15, got %v", opts["tcpKeepAliveIdle"])
	}
	if opts["tcpKeepAliveInterval"] != 5 {
		t.Errorf("expected tcpKeepAliveInterval=5, got %v", opts["tcpKeepAliveInterval"])
	}
}

func TestGamingLANIPs(t *testing.T) {
	ips := GamingLANIPs()
	required := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"100.64.0.0/10",
		"224.0.0.0/4",
		"255.255.255.255/32",
	}
	set := make(map[string]bool)
	for _, ip := range ips {
		set[ip] = true
	}
	for _, req := range required {
		if !set[req] {
			t.Errorf("missing expected gaming LAN IP: %s", req)
		}
	}
}

func TestDefaultGamingApps(t *testing.T) {
	apps := DefaultGamingApps()
	expectedApps := []string{"cs2.exe", "valorant.exe", "dota2.exe", "leagueclient.exe"}
	set := make(map[string]bool)
	for _, app := range apps {
		set[app] = true
	}
	for _, exp := range expectedApps {
		if !set[exp] {
			t.Errorf("expected gaming app %s not found in DefaultGamingApps", exp)
		}
	}
}

func TestOptimizeOutbound(t *testing.T) {
	// Nil map should not panic
	OptimizeOutbound(nil, true)

	// Disabled should not modify outbound
	outboundDisabled := map[string]interface{}{
		"protocol": "vless",
	}
	OptimizeOutbound(outboundDisabled, false)
	if _, ok := outboundDisabled["streamSettings"]; ok {
		t.Errorf("did not expect streamSettings when gamingMode is false")
	}

	// Enabled should inject sockopt
	outboundEnabled := map[string]interface{}{
		"protocol": "vless",
	}
	OptimizeOutbound(outboundEnabled, true)
	streamSettings, ok := outboundEnabled["streamSettings"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected streamSettings to be map[string]interface{}")
	}
	sockopt, ok := streamSettings["sockopt"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected sockopt map in streamSettings")
	}
	if sockopt["tcpNoDelay"] != true {
		t.Errorf("expected tcpNoDelay=true in sockopt")
	}
}

func TestIsGamingModeAndToggle(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_gaming.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	if IsGamingMode(nil) {
		t.Errorf("expected false for nil db")
	}
	if IsGamingMode(db) {
		t.Errorf("expected false by default")
	}

	if err := ToggleGamingMode(db, nil, true); err != nil {
		t.Fatalf("ToggleGamingMode(true) error: %v", err)
	}
	if !IsGamingMode(db) {
		t.Errorf("expected true after ToggleGamingMode(true)")
	}

	if err := ToggleGamingMode(db, nil, false); err != nil {
		t.Fatalf("ToggleGamingMode(false) error: %v", err)
	}
	if IsGamingMode(db) {
		t.Errorf("expected false after ToggleGamingMode(false)")
	}
}

func TestSetSocketLowLatency(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen failed: %v", err)
	}
	defer ln.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err == nil {
			_ = conn.Close()
		}
	}()

	clientConn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("net.Dial failed: %v", err)
	}
	defer clientConn.Close()

	if err := SetSocketLowLatency(clientConn); err != nil {
		t.Errorf("SetSocketLowLatency returned error: %v", err)
	}
	<-done
}
