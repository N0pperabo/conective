package lan

import (
	"net"
	"path/filepath"
	"strings"
	"testing"

	"freenode/pkg/database"
)

func TestGetLocalIPs(t *testing.T) {
	ips, err := GetLocalIPs()
	if err != nil {
		t.Fatalf("GetLocalIPs returned error: %v", err)
	}

	for _, ipStr := range ips {
		parsed := net.ParseIP(ipStr)
		if parsed == nil {
			t.Errorf("expected valid IP string, got: %s", ipStr)
			continue
		}
		if parsed.To4() == nil {
			t.Errorf("expected IPv4 address, got IPv6: %s", ipStr)
		}
		if parsed.IsLoopback() {
			t.Errorf("expected no loopback IP, got: %s", ipStr)
		}
		if parsed.IsLinkLocalUnicast() {
			t.Errorf("expected no APIPA link-local IP, got: %s", ipStr)
		}
		// Check RFC 2544 benchmark subnet used by Wintun (198.18.0.0/15)
		ipv4 := parsed.To4()
		if ipv4[0] == 198 && (ipv4[1] == 18 || ipv4[1] == 19) {
			t.Errorf("expected no Wintun RFC 2544 benchmark IP, got: %s", ipStr)
		}
	}
}

func TestGetListenHost(t *testing.T) {
	if got := GetListenHost(true); got != "0.0.0.0" {
		t.Errorf("expected 0.0.0.0 for shareLAN=true, got %s", got)
	}
	if got := GetListenHost(false); got != "127.0.0.1" {
		t.Errorf("expected 127.0.0.1 for shareLAN=false, got %s", got)
	}
}

func TestGetLANInfo(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_lan.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Default disabled
	info, err := GetLANInfo(db, 10809, 10808)
	if err != nil {
		t.Fatalf("GetLANInfo returned error: %v", err)
	}
	if info.Enabled {
		t.Errorf("expected Enabled=false by default, got true")
	}
	if info.HTTPPort != 10809 || info.SOCKSPort != 10808 {
		t.Errorf("unexpected ports: %d, %d", info.HTTPPort, info.SOCKSPort)
	}
	if !strings.HasPrefix(info.ProxyURL, "http://") {
		t.Errorf("expected proxy URL starting with http://, got %s", info.ProxyURL)
	}

	// Enabled
	_ = db.SetSetting("share_lan", "true")
	infoEnabled, err := GetLANInfo(db, 10809, 10808)
	if err != nil {
		t.Fatalf("GetLANInfo returned error: %v", err)
	}
	if !infoEnabled.Enabled {
		t.Errorf("expected Enabled=true after setting update")
	}

	// Test with nil DB
	infoNilDB, err := GetLANInfo(nil, 8080, 1080)
	if err != nil {
		t.Fatalf("GetLANInfo with nil DB returned error: %v", err)
	}
	if infoNilDB.Enabled {
		t.Errorf("expected Enabled=false with nil db")
	}
	if infoNilDB.HTTPPort != 8080 || infoNilDB.SOCKSPort != 1080 {
		t.Errorf("expected ports 8080 and 1080, got %d, %d", infoNilDB.HTTPPort, infoNilDB.SOCKSPort)
	}
}

func TestToggleLANSharing(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_toggle_lan.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}
	defer db.Close()

	// Toggle true
	if err := ToggleLANSharing(db, nil, true); err != nil {
		t.Fatalf("ToggleLANSharing(true) error: %v", err)
	}
	if db.GetSetting("share_lan", "false") != "true" {
		t.Errorf("expected share_lan to be true in DB")
	}

	// Toggle false
	if err := ToggleLANSharing(db, nil, false); err != nil {
		t.Fatalf("ToggleLANSharing(false) error: %v", err)
	}
	if db.GetSetting("share_lan", "false") != "false" {
		t.Errorf("expected share_lan to be false in DB")
	}
}
