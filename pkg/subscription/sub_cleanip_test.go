package subscription

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"

	"freenode/pkg/database"
)

func TestParseCleanIPLine(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantIP     string
		wantPort   int
		wantName   string
		wantOk     bool
	}{
		{
			name:     "bare IPv4",
			input:    "104.16.88.99",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "bare IPv4 with spaces",
			input:    "   188.114.96.1   ",
			wantIP:   "188.114.96.1",
			wantPort: 443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "bare IPv4 with quotes",
			input:    "\"172.67.182.203\"",
			wantIP:   "172.67.182.203",
			wantPort: 443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "IP:Port standard 443",
			input:    "188.114.96.1:443",
			wantIP:   "188.114.96.1",
			wantPort: 443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "IP:Port custom 8443",
			input:    "104.16.88.99:8443",
			wantIP:   "104.16.88.99",
			wantPort: 8443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "IP#Name",
			input:    "23.209.210.116#Akamai",
			wantIP:   "23.209.210.116",
			wantPort: 443,
			wantName: "Akamai",
			wantOk:   true,
		},
		{
			name:     "IP#Name with spaces",
			input:    "104.16.88.99#Cloudflare CDN",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "Cloudflare CDN",
			wantOk:   true,
		},
		{
			name:     "IP#Name with URL escaped name",
			input:    "104.16.88.99#Cloudflare%20Edge",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "Cloudflare Edge",
			wantOk:   true,
		},
		{
			name:     "IP:Port#Name",
			input:    "23.209.210.116:8443#Akamai",
			wantIP:   "23.209.210.116",
			wantPort: 8443,
			wantName: "Akamai",
			wantOk:   true,
		},
		{
			name:     "IP:Port#Name with spaces and custom port",
			input:    "188.114.96.1:2053#CF Fast Node",
			wantIP:   "188.114.96.1",
			wantPort: 2053,
			wantName: "CF Fast Node",
			wantOk:   true,
		},
		{
			name:     "psiphon bare",
			input:    "psiphon://104.16.88.99",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "psiphon with port",
			input:    "psiphon://104.16.88.99:8443",
			wantIP:   "104.16.88.99",
			wantPort: 8443,
			wantName: "",
			wantOk:   true,
		},
		{
			name:     "psiphon with name",
			input:    "psiphon://23.209.210.116#Akamai CDN",
			wantIP:   "23.209.210.116",
			wantPort: 443,
			wantName: "Akamai CDN",
			wantOk:   true,
		},
		{
			name:     "psiphon with port and name",
			input:    "psiphon://188.114.96.1:443#Cloudflare",
			wantIP:   "188.114.96.1",
			wantPort: 443,
			wantName: "Cloudflare",
			wantOk:   true,
		},
		{
			name:     "psiphon with query params and fragment",
			input:    "psiphon://104.16.88.99:443?fronting=cdn#Clean-IP-104.16.88.99",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "Clean-IP-104.16.88.99",
			wantOk:   true,
		},
		{
			name:     "psiphon with trailing slash and name",
			input:    "psiphon://104.16.88.99:443/#EdgeNode",
			wantIP:   "104.16.88.99",
			wantPort: 443,
			wantName: "EdgeNode",
			wantOk:   true,
		},
		// Invalid / Non-CleanIP cases
		{
			name:   "vless link should not be clean IP",
			input:  "vless://uuid@104.16.88.99:443?security=tls#Test",
			wantOk: false,
		},
		{
			name:   "vmess link should not be clean IP",
			input:  "vmess://eyJ2IjoiMiIsInBzIjoiVGVzdCJ9",
			wantOk: false,
		},
		{
			name:   "trojan link should not be clean IP",
			input:  "trojan://password@104.16.88.99:443#TrojanNode",
			wantOk: false,
		},
		{
			name:   "http url should not be clean IP",
			input:  "http://104.16.88.99/sub.txt",
			wantOk: false,
		},
		{
			name:   "comment with //",
			input:  "// 104.16.88.99",
			wantOk: false,
		},
		{
			name:   "invalid IPv4 octet",
			input:  "999.999.999.999",
			wantOk: false,
		},
		{
			name:   "invalid port > 65535",
			input:  "104.16.88.99:99999",
			wantOk: false,
		},
		{
			name:   "invalid port zero",
			input:  "104.16.88.99:0",
			wantOk: false,
		},
		{
			name:   "invalid port non-number",
			input:  "104.16.88.99:xyz",
			wantOk: false,
		},
		{
			name:   "domain instead of IP",
			input:  "cloudflare.com:443",
			wantOk: false,
		},
		{
			name:   "empty string",
			input:  "",
			wantOk: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, port, name, ok := parseCleanIPLine(tt.input)
			if ok != tt.wantOk {
				t.Fatalf("parseCleanIPLine(%q) ok = %v, wantOk = %v", tt.input, ok, tt.wantOk)
			}
			if !ok {
				return
			}
			if ip != tt.wantIP {
				t.Errorf("ip = %q, want %q", ip, tt.wantIP)
			}
			if port != tt.wantPort {
				t.Errorf("port = %d, want %d", port, tt.wantPort)
			}
			if name != tt.wantName {
				t.Errorf("name = %q, want %q", name, tt.wantName)
			}
		})
	}
}

func TestImportContent_CleanIPAllFormats(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_cleanip.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	mgr := NewManager(db)

	rawContent := strings.Join([]string{
		"# CDN Clean IPs test list",
		"104.16.88.99",
		"188.114.96.1:443",
		"23.209.210.116#Akamai",
		"172.67.182.203:8443#Cloudflare CDN",
		"psiphon://104.21.45.67:443?fronting=cdn#EdgeCast",
	}, "\n")

	imported, duplicates, err := mgr.ImportContent(rawContent, "Manual Clean IP Test")
	if err != nil {
		t.Fatalf("ImportContent failed: %v", err)
	}

	if imported != 5 {
		t.Errorf("expected 5 imported nodes, got %d", imported)
	}
	if duplicates != 0 {
		t.Errorf("expected 0 duplicates, got %d", duplicates)
	}

	// Verify all nodes saved in database
	configs, total, err := db.GetConfigs(database.ConfigFilter{Status: "working", Limit: 100})
	if err != nil {
		t.Fatalf("failed to get configs: %v", err)
	}
	if total != 5 {
		t.Fatalf("expected 5 nodes in database, got total=%d", total)
	}

	nodeMap := make(map[string]database.ConfigFilter) // map by server IP
	_ = nodeMap
	for _, c := range configs {
		if c.Protocol != "psiphon" {
			t.Errorf("node %s protocol = %q, want 'psiphon'", c.Server, c.Protocol)
		}
		if c.Status != "working" {
			t.Errorf("node %s status = %q, want 'working'", c.Server, c.Status)
		}
		if !c.IsFavorite {
			t.Errorf("node %s expected IsFavorite = true", c.Server)
		}
		if !strings.Contains(c.Tags, "clean-ip") {
			t.Errorf("node %s expected tags containing 'clean-ip', got %q", c.Server, c.Tags)
		}

		switch c.Server {
		case "104.16.88.99":
			if c.Port != 443 {
				t.Errorf("104.16.88.99: expected port 443, got %d", c.Port)
			}
			if !strings.Contains(c.Name, "104.16.88.99") {
				t.Errorf("104.16.88.99: expected default name with IP, got %q", c.Name)
			}
		case "188.114.96.1":
			if c.Port != 443 {
				t.Errorf("188.114.96.1: expected port 443, got %d", c.Port)
			}
		case "23.209.210.116":
			if c.Name != "Akamai" {
				t.Errorf("23.209.210.116: expected name 'Akamai', got %q", c.Name)
			}
		case "172.67.182.203":
			if c.Port != 8443 {
				t.Errorf("172.67.182.203: expected port 8443, got %d", c.Port)
			}
			if c.Name != "Cloudflare CDN" {
				t.Errorf("172.67.182.203: expected name 'Cloudflare CDN', got %q", c.Name)
			}
		case "104.21.45.67":
			if c.Port != 443 {
				t.Errorf("104.21.45.67: expected port 443, got %d", c.Port)
			}
			if c.Name != "EdgeCast" {
				t.Errorf("104.21.45.67: expected name 'EdgeCast', got %q", c.Name)
			}
		default:
			t.Errorf("unexpected server IP in database: %s", c.Server)
		}
	}
}

func TestImportContent_Base64(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_b64.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	mgr := NewManager(db)

	raw := "104.16.88.99\n188.114.96.1:443#Cloudflare\n23.209.210.116#Akamai"
	b64 := base64.StdEncoding.EncodeToString([]byte(raw))

	imported, duplicates, err := mgr.ImportContent(b64, "Base64 Subscription")
	if err != nil {
		t.Fatalf("ImportContent failed on base64: %v", err)
	}
	if imported != 3 {
		t.Errorf("expected 3 imported nodes, got %d", imported)
	}
	if duplicates != 0 {
		t.Errorf("expected 0 duplicates, got %d", duplicates)
	}
}

func TestImportContent_Duplicates(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_dups.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	mgr := NewManager(db)

	raw := "104.16.88.99\n104.16.88.99#Duplicate"

	imported, duplicates, err := mgr.ImportContent(raw, "Duplicate Test")
	if err != nil {
		t.Fatalf("ImportContent failed: %v", err)
	}
	if imported != 1 {
		t.Errorf("expected 1 imported node, got %d", imported)
	}
	if duplicates != 1 {
		t.Errorf("expected 1 duplicate, got %d", duplicates)
	}
}

func TestImportContent_MixedWithProxyLinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_mixed.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	mgr := NewManager(db)

	vlessLink := "vless://550e8400-e29b-41d4-a716-446655440000@example.com:443?encryption=none&security=none&type=ws&path=%2F#VLESS-Node"
	raw := "104.16.88.99\n23.209.210.116#Akamai\n" + vlessLink

	imported, duplicates, err := mgr.ImportContent(raw, "Mixed Test")
	if err != nil {
		t.Fatalf("ImportContent failed on mixed input: %v", err)
	}
	if imported != 3 {
		t.Errorf("expected 3 imported nodes, got %d", imported)
	}
	if duplicates != 0 {
		t.Errorf("expected 0 duplicates, got %d", duplicates)
	}
}
