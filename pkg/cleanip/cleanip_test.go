package cleanip

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"freenode/pkg/geoip"
	"freenode/pkg/models"
)

func TestGenerateCandidateIPs(t *testing.T) {
	cidrs := []string{
		"104.16.0.0/13",
		"162.158.0.0/15",
		"172.64.0.0/13",
	}

	sampleSize := 50
	candidates := GenerateCandidateIPs(cidrs, sampleSize)

	if len(candidates) == 0 {
		t.Fatalf("expected candidates, got 0")
	}

	if len(candidates) > sampleSize {
		t.Fatalf("expected <= %d candidates, got %d", sampleSize, len(candidates))
	}

	seen := make(map[string]bool)
	for _, c := range candidates {
		if c.IP == "" {
			t.Errorf("empty IP found")
		}
		ip := net.ParseIP(c.IP)
		if ip == nil || ip.To4() == nil {
			t.Errorf("invalid IPv4 address: %s", c.IP)
		}
		if seen[c.IP] {
			t.Errorf("duplicate IP found: %s", c.IP)
		}
		seen[c.IP] = true
	}
}

func TestApplyCleanIP_VLESS(t *testing.T) {
	node := &models.Config{
		Protocol:  "vless",
		Name:      "Test-VLESS",
		Server:    "cdn.example.com",
		Port:      443,
		UUID:      "12345678-1234-1234-1234-123456789abc",
		Transport: "ws",
		TLS:       "tls",
		Path:      "/websocket",
		RawLink:   "vless://12345678-1234-1234-1234-123456789abc@cdn.example.com:443?type=ws&security=tls&path=%2Fwebsocket#Test-VLESS",
	}

	cleanIP := "104.16.123.45"
	err := ApplyCleanIP(node, cleanIP)
	if err != nil {
		t.Fatalf("ApplyCleanIP failed: %v", err)
	}

	if node.Server != cleanIP {
		t.Errorf("expected Server = %s, got %s", cleanIP, node.Server)
	}
	if node.SNI != "cdn.example.com" {
		t.Errorf("expected SNI = cdn.example.com, got %s", node.SNI)
	}
	if node.Host != "cdn.example.com" {
		t.Errorf("expected Host = cdn.example.com, got %s", node.Host)
	}
	if !strings.Contains(node.RawLink, cleanIP) {
		t.Errorf("expected RawLink to contain clean IP %s, got %s", cleanIP, node.RawLink)
	}
	if !strings.Contains(node.RawLink, "sni=cdn.example.com") {
		t.Errorf("expected RawLink to contain sni, got %s", node.RawLink)
	}
	if node.Identity == "" {
		t.Errorf("expected non-empty identity")
	}
}

func TestApplyCleanIP_VMess(t *testing.T) {
	vmessJSON := map[string]any{
		"v":    "2",
		"ps":   "Test-VMess",
		"add":  "origin-cdn.domain.org",
		"port": 443,
		"id":   "abcdef12-1234-5678-abcd-000000000000",
		"aid":  0,
		"net":  "ws",
		"type": "none",
		"host": "",
		"path": "/ray",
		"tls":  "tls",
		"sni":  "",
	}
	b, _ := json.Marshal(vmessJSON)
	rawLink := "vmess://" + base64.StdEncoding.EncodeToString(b)

	node := &models.Config{
		Protocol:  "vmess",
		Name:      "Test-VMess",
		Server:    "origin-cdn.domain.org",
		Port:      443,
		UUID:      "abcdef12-1234-5678-abcd-000000000000",
		Transport: "ws",
		TLS:       "tls",
		Path:      "/ray",
		RawLink:   rawLink,
	}

	cleanIP := "162.158.44.12"
	err := ApplyCleanIP(node, cleanIP)
	if err != nil {
		t.Fatalf("ApplyCleanIP failed: %v", err)
	}

	if node.Server != cleanIP {
		t.Errorf("expected Server = %s, got %s", cleanIP, node.Server)
	}
	if node.SNI != "origin-cdn.domain.org" {
		t.Errorf("expected preserved SNI, got %s", node.SNI)
	}
	if node.Host != "origin-cdn.domain.org" {
		t.Errorf("expected preserved Host, got %s", node.Host)
	}

	// Verify decoded VMess link has updated "add", "sni", "host"
	raw := strings.TrimPrefix(node.RawLink, "vmess://")
	decoded, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatalf("decode vmess link failed: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		t.Fatalf("unmarshal vmess json failed: %v", err)
	}
	if parsed["add"] != cleanIP {
		t.Errorf("expected vmess add = %s, got %v", cleanIP, parsed["add"])
	}
	if parsed["sni"] != "origin-cdn.domain.org" {
		t.Errorf("expected vmess sni = origin-cdn.domain.org, got %v", parsed["sni"])
	}
}

func TestApplyCleanIP_InvalidIP(t *testing.T) {
	node := &models.Config{
		Protocol: "vless",
		Server:   "example.com",
		Port:     443,
	}

	err := ApplyCleanIP(node, "invalid-ip-string")
	if err == nil {
		t.Fatalf("expected error for invalid IP, got nil")
	}

	err = ApplyCleanIP(node, "999.999.999.999")
	if err == nil {
		t.Fatalf("expected error for out of range IP, got nil")
	}
}

func TestIsCDNCompatible(t *testing.T) {
	// 1. WS transport -> Compatible
	if !IsCDNCompatible(&models.Config{Transport: "ws"}) {
		t.Errorf("expected ws node to be CDN compatible")
	}

	// 2. gRPC transport -> Compatible
	if !IsCDNCompatible(&models.Config{Transport: "grpc"}) {
		t.Errorf("expected grpc node to be CDN compatible")
	}

	// 3. Port 443 with domain server -> Compatible
	if !IsCDNCompatible(&models.Config{Server: "my-cdn-node.org", Port: 443}) {
		t.Errorf("expected domain on 443 to be CDN compatible")
	}

	// 4. Raw IP with SS protocol and TCP on random port -> Not compatible
	if IsCDNCompatible(&models.Config{Protocol: "ss", Server: "185.12.34.56", Port: 8388, Transport: "tcp"}) {
		t.Errorf("expected raw IP SS node to NOT be CDN compatible")
	}
}

func TestManager_ScanLifecycle(t *testing.T) {
	mgr := NewManager()

	prog := mgr.GetProgress()
	if prog.State != "idle" {
		t.Errorf("expected initial state idle, got %s", prog.State)
	}

	opts := ScanOptions{
		Workers:     5,
		TimeoutMs:   200,
		SampleSize:  10,
		CustomCIDRs: []string{"127.0.0.0/24"}, // local loopback CIDR for fast test
	}

	err := mgr.StartScan(opts)
	if err != nil {
		t.Fatalf("StartScan failed: %v", err)
	}

	prog = mgr.GetProgress()
	if prog.State != "scanning" && prog.State != "completed" {
		t.Errorf("expected state scanning or completed, got %s", prog.State)
	}

	// Second start while scanning should return error
	err = mgr.StartScan(opts)
	if err == nil && prog.State == "scanning" {
		t.Errorf("expected error when starting duplicate scan")
	}

	mgr.CancelScan()
	prog = mgr.GetProgress()
	if prog.State != "cancelled" && prog.State != "completed" {
		t.Errorf("expected cancelled or completed, got %s", prog.State)
	}
}

func TestTestIP_MockServer(t *testing.T) {
	// Generate self-signed cert for mock server
	cert, err := generateSelfSignedCert()
	if err != nil {
		t.Fatalf("generating self-signed cert: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	listener, err := tls.Listen("tcp", "127.0.0.1:0", tlsConfig)
	if err != nil {
		t.Fatalf("failed to start mock TLS listener: %v", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port

	// Accept connections in background
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				tlsConn, ok := c.(*tls.Conn)
				if ok {
					_ = tlsConn.Handshake()
				}
			}(conn)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	latency, err := TestIP(ctx, "127.0.0.1", port, 2*time.Second)
	if err != nil {
		t.Fatalf("TestIP failed against local TLS server: %v", err)
	}
	if latency <= 0 {
		t.Errorf("expected positive latency, got %d", latency)
	}
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Cloudflare Mock"},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:              []string{"cloudflare.com"},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	cert := tls.Certificate{
		Certificate: [][]byte{derBytes},
		PrivateKey:  priv,
	}
	return cert, nil
}

func TestCreateOrUpdateCleanIPNodeWithName_Fallback(t *testing.T) {
	// Ensure default resolver is nil for this test
	orig := GetDefaultGeoResolver()
	SetDefaultGeoResolver(nil)
	defer SetDefaultGeoResolver(orig)

	cleanIP := "188.114.96.1"
	node, err := CreateOrUpdateCleanIPNodeWithName(nil, cleanIP, 120, "")
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanIPNodeWithName failed: %v", err)
	}

	if node.Country != "CF" {
		t.Errorf("expected fallback country 'CF', got %q", node.Country)
	}
	if node.CountryName != "Cloudflare Anycast" {
		t.Errorf("expected fallback country name 'Cloudflare Anycast', got %q", node.CountryName)
	}
	if node.Country == "US" {
		t.Errorf("expected country to never be hardcoded to 'US'")
	}
	if node.Server != cleanIP {
		t.Errorf("expected server %s, got %s", cleanIP, node.Server)
	}
	if !strings.Contains(node.Name, cleanIP) {
		t.Errorf("expected node name to contain IP, got %s", node.Name)
	}
}

func TestCreateOrUpdateCleanIPNode_Wrapper(t *testing.T) {
	orig := GetDefaultGeoResolver()
	SetDefaultGeoResolver(nil)
	defer SetDefaultGeoResolver(orig)

	node, err := CreateOrUpdateCleanIPNode(nil, "162.159.192.1", 90)
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanIPNode failed: %v", err)
	}

	if node.Country != "CF" {
		t.Errorf("expected fallback country 'CF', got %q", node.Country)
	}
	if node.Country == "US" {
		t.Errorf("expected country to not be hardcoded to 'US'")
	}
	if node.Latency != 90 {
		t.Errorf("expected latency 90, got %d", node.Latency)
	}
}

func TestCreateOrUpdateCleanIPNodeWithName_GeoResolution(t *testing.T) {
	geoPaths := []string{
		"../../assets/GeoLite2-Country.mmdb",
		"assets/GeoLite2-Country.mmdb",
	}

	var geoPath string
	for _, p := range geoPaths {
		if _, err := os.Stat(p); err == nil {
			geoPath = p
			break
		}
	}

	if geoPath == "" {
		t.Skip("GeoLite2-Country.mmdb not found, skipping real geo resolution test")
	}

	resolver, err := geoip.New(geoPath)
	if err != nil {
		t.Fatalf("failed to initialize GeoIP resolver: %v", err)
	}
	defer resolver.Close()

	orig := GetDefaultGeoResolver()
	SetDefaultGeoResolver(resolver)
	defer SetDefaultGeoResolver(orig)

	testCases := []struct {
		ip             string
		expectNonEmpty bool
		mustNotBeUS    bool
	}{
		{ip: "188.114.96.1", expectNonEmpty: true, mustNotBeUS: false},
		{ip: "193.174.198.10", expectNonEmpty: true, mustNotBeUS: true}, // DE IP (Germany)
		{ip: "212.58.244.20", expectNonEmpty: true, mustNotBeUS: true},  // GB IP (UK)
		{ip: "10.0.0.1", expectNonEmpty: false, mustNotBeUS: true},       // Private IP (unresolvable -> fallback CF)
	}

	for _, tc := range testCases {
		node, err := CreateOrUpdateCleanIPNodeWithName(nil, tc.ip, 100, "")
		if err != nil {
			t.Errorf("CreateOrUpdateCleanIPNodeWithName(%s) error: %v", tc.ip, err)
			continue
		}

		g := resolver.Lookup(tc.ip)
		if g.Code != "" && g.Code != "UN" {
			if node.Country != g.Code {
				t.Errorf("IP %s: expected country %s, got %s", tc.ip, g.Code, node.Country)
			}
			if node.CountryName != g.Name {
				t.Errorf("IP %s: expected country name %s, got %s", tc.ip, g.Name, node.CountryName)
			}
		} else {
			// Unresolvable IP must fallback to CF (Cloudflare Anycast)
			if node.Country != "CF" {
				t.Errorf("IP %s: expected fallback 'CF', got %s", tc.ip, node.Country)
			}
			if node.CountryName != "Cloudflare Anycast" {
				t.Errorf("IP %s: expected fallback 'Cloudflare Anycast', got %s", tc.ip, node.CountryName)
			}
		}

		if tc.mustNotBeUS && node.Country == "US" {
			t.Errorf("IP %s: country was unexpectedly hardcoded to US", tc.ip)
		}
	}
}

func TestBuildCleanIPCandidates_CountryResolution(t *testing.T) {
	orig := GetDefaultGeoResolver()
	SetDefaultGeoResolver(nil)
	defer SetDefaultGeoResolver(orig)

	candidates := BuildCleanIPCandidates(nil, "188.114.96.1", 100, 5)
	if len(candidates) == 0 {
		t.Fatalf("expected fallback candidates, got 0")
	}

	for _, c := range candidates {
		if c.Country == "" {
			t.Errorf("candidate %s has empty country", c.Name)
		}
		if c.Country == "US" {
			t.Errorf("candidate %s has unexpected hardcoded US country", c.Name)
		}
	}
}

func TestCleanIP_IsFavorite_DefaultFalse(t *testing.T) {
	node, err := CreateOrUpdateCleanIPNodeWithName(nil, "104.16.88.99", 50, "TestNode")
	if err != nil {
		t.Fatalf("CreateOrUpdateCleanIPNodeWithName failed: %v", err)
	}
	if node.IsFavorite {
		t.Errorf("expected clean IP node IsFavorite = false, got true")
	}

	warpNode, err := CreateOrUpdateWarpNodeWithName(nil, "104.16.88.99", 2408, 50, "WarpNode")
	if err != nil {
		t.Fatalf("CreateOrUpdateWarpNodeWithName failed: %v", err)
	}
	if warpNode.IsFavorite {
		t.Errorf("expected warp node IsFavorite = false, got true")
	}

	candidates := BuildCleanIPCandidates(nil, "104.16.88.99", 50, 5)
	if len(candidates) == 0 {
		t.Fatalf("expected candidates, got 0")
	}
	for _, c := range candidates {
		if c.IsFavorite {
			t.Errorf("candidate %s expected IsFavorite = false, got true", c.Name)
		}
	}
}

func TestManager_GetProgress_NoLimitClamp(t *testing.T) {
	mgr := NewManager()
	mgr.mu.Lock()
	for i := 0; i < 50; i++ {
		mgr.bestIPs = append(mgr.bestIPs, IPResult{
			IP:      fmt.Sprintf("104.16.0.%d", i),
			Latency: 10 + i,
		})
	}
	mgr.mu.Unlock()

	p := mgr.GetProgress()
	if len(p.BestIPs) != 50 {
		t.Fatalf("expected 50 best IPs in progress (no limit clamp), got %d", len(p.BestIPs))
	}
}


