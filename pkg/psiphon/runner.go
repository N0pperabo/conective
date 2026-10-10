package psiphon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"freenode/pkg/cleanip"
	"freenode/pkg/logger"
)

// Runner manages the lifecycle of the Psiphon / Aether circumvention engine
type Runner struct {
	mu           sync.Mutex
	cmd          *exec.Cmd
	running      bool
	cleanIP      string
	egressRegion string
	socksPort    int
	httpPort     int
	connectedAt  time.Time
	lastLatency  int
	exitIP       string
}

// NewRunner creates a new Psiphon runner instance
func NewRunner() *Runner {
	return &Runner{}
}

// FindBinaries locates aether.exe and psiphon-tunnel-core.exe
func FindBinaries() (aetherPath, psiphonPath string, err error) {
	searchDirs := make([]string, 0, 10)

	if exePath, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exePath)
		searchDirs = append(searchDirs,
			filepath.Join(exeDir, "bin"),
			filepath.Join(exeDir, "assets", "bin"),
			exeDir,
		)
	}

	if localApp := os.Getenv("LOCALAPPDATA"); localApp != "" {
		searchDirs = append(searchDirs,
			filepath.Join(localApp, "Programs", "Conective", "bin"),
			filepath.Join(localApp, "Programs", "Conective"),
		)
	}

	if appData := os.Getenv("APPDATA"); appData != "" {
		searchDirs = append(searchDirs,
			filepath.Join(appData, "Conective", "bin"),
			filepath.Join(appData, "Conective"),
		)
	}

	// Development and workspace fallbacks
	searchDirs = append(searchDirs,
		filepath.Join("assets", "bin"),
		"bin",
		".",
		`C:\Users\HONAR\.gemini\antigravity\brain\7cfd3c6c-9523-4020-bcca-684350dd66a1\scratch\se7en_app\Resources\aether`,
		`C:\Users\HONAR\.gemini\antigravity\brain\7cfd3c6c-9523-4020-bcca-684350dd66a1\scratch\se7en_app\Resources`,
		`d:\conective\freenode\assets\bin`,
	)

	for _, dir := range searchDirs {
		aPath := filepath.Join(dir, "aether.exe")
		if _, err := os.Stat(aPath); err == nil && aetherPath == "" {
			aetherPath = aPath
		}
		pPath := filepath.Join(dir, "psiphon-tunnel-core.exe")
		if _, err := os.Stat(pPath); err == nil && psiphonPath == "" {
			psiphonPath = pPath
		}
	}

	if aetherPath == "" {
		return "", "", fmt.Errorf("aether.exe not found in search paths")
	}
	if psiphonPath == "" {
		return "", "", fmt.Errorf("psiphon-tunnel-core.exe not found in search paths")
	}

	return aetherPath, psiphonPath, nil
}

func detectIPDefaults(ip string) (sni, cdnSet string) {
	_, detSNI, detSet := cleanip.DetectCDN(ip)
	if detSNI != "" {
		return detSNI, detSet
	}
	return "a248.e.akamai.net", "psiphon-akamai"
}

// ShiroCdnFrontingConfigWithRegion generates fronting configuration matching ShirOKhorshid with optional egress region
func ShiroCdnFrontingConfigWithRegion(cleanIP, sni, egressRegion string) ([]byte, error) {
	if sni == "" {
		sni = "a248.e.akamai.net"
	}

	cfg := map[string]any{
		"LimitTunnelProtocols": []string{
			"FRONTED-MEEK-CDN-OSSH",
			"FRONTED-MEEK-CDN-HTTP-OSSH",
			"FRONTED-MEEK-CDN-QUIC-OSSH",
		},
		"DisableTactics":                      true,
		"FrontedMeekDialOverridesProbability": 1.0,
		"FrontedMeekCDNScanUseBuiltInSpec":    true,
		"FrontedMeekDialOverrides": []map[string]any{
			{
				"OverrideID":                     "fastly-provider",
				"MatchFrontingProviderIDRegexes": []string{"(?i)fastly"},
				"DialAddresses":                  []string{"pypi.org"},
				"SNIServerName":                  "pypi.org",
				"VerifyServerNames": []string{
					"www.python.org", "pypi.org", "fastly.com", "www.fastly.com",
					"developer.fastly.com", "githubassets.com", "github.com",
					"github.io", "githubusercontent.com",
				},
				"ALPNProtocols": []string{"h2", "http/1.1"},
				"TLSProfile":    "Chrome-83",
			},
			{
				"OverrideID":              "fastly-address",
				"MatchDialAddressRegexes": []string{"(?i)(fastly|pypi|python|github)"},
				"DialAddresses":           []string{"pypi.org"},
				"SNIServerName":           "pypi.org",
				"VerifyServerNames": []string{
					"www.python.org", "pypi.org", "fastly.com", "www.fastly.com",
					"developer.fastly.com", "githubassets.com", "github.com",
					"github.io", "githubusercontent.com",
				},
				"ALPNProtocols": []string{"h2", "http/1.1"},
				"TLSProfile":    "Chrome-83",
			},
			{
				"OverrideID":              "edge-custom",
				"MatchDialAddressRegexes": []string{".*"},
				"DialAddresses":           []string{cleanIP},
				"SNIServerName":           sni,
				"VerifyServerNames": []string{
					sni,
					cleanIP,
					"a248.e.akamai.net",
					"a.akamaized.net",
					"a.akamaized-staging.net",
					"a.akamaihd.net",
					"a.akamaihd-staging.net",
					"www.akamai.com",
				},
				"ALPNProtocols": []string{"http/1.1"},
				"TLSProfile":    "Chrome-83",
			},
		},
		"FrontedMeekCDNScanSpec": map[string]any{
			"IPCandidates":   []string{cleanIP},
			"SNIServerNames": []string{sni},
		},
	}

	reg := strings.ToUpper(strings.TrimSpace(egressRegion))
	if reg != "" && reg != "AUTO" && reg != "ANY" {
		cfg["EgressRegion"] = reg
	}

	return json.MarshalIndent(cfg, "", "  ")
}

// ShiroCdnFrontingConfig generates the exact configuration matching ShirOKhorshid Android client
func ShiroCdnFrontingConfig(cleanIP, sni string) ([]byte, error) {
	return ShiroCdnFrontingConfigWithRegion(cleanIP, sni, "")
}

// Start launches Aether in Psiphon-only mode with the selected Clean CDN IP
func (r *Runner) Start(cleanIP string, socksPort, httpPort int) error {
	return r.StartWithRegion(cleanIP, "", "", "", socksPort, httpPort)
}

func getSafePsiphonDir() string {
	appData := os.Getenv("APPDATA")
	if appData == "" {
		appData = "."
	}
	baseDir := filepath.Join(appData, "Conective", "psiphon")
	_ = os.MkdirAll(baseDir, 0755)

	lockFile := filepath.Join(baseDir, "ca.psiphon.PsiphonTunnel.tunnel-core", "psiphon.boltdb")
	if _, err := os.Stat(lockFile); err == nil {
		f, openErr := os.OpenFile(lockFile, os.O_RDWR, 0600)
		if openErr != nil {
			// Primary database is held by another process; use an isolated instance directory
			uniqueDir := filepath.Join(baseDir, fmt.Sprintf("session_%d", time.Now().UnixNano()%100000))
			_ = os.MkdirAll(uniqueDir, 0755)
			return uniqueDir
		}
		_ = f.Close()
	}
	_ = os.Remove(filepath.Join(baseDir, "psiphon.boltdb.lock"))
	return baseDir
}

// StartWithSNI launches Aether in Psiphon-only mode with the selected Clean CDN IP, custom SNI and CDN set
func (r *Runner) StartWithSNI(cleanIP, sni, cdnSet string, socksPort, httpPort int) error {
	return r.StartWithRegion(cleanIP, sni, cdnSet, "", socksPort, httpPort)
}

// BuildArgs constructs the command line arguments for launching Aether in Psiphon mode.
// For direct connections (cleanIP empty, "auto", or "direct"), --psiphon-mode direct is NOT passed,
// leaving Aether to run in Auto shape using its built-in tactics.
// For CDN fronting connections, --psiphon-mode cdn is passed along with fronting configurations.
// If egressRegion is empty, "AUTO", or "ANY", --psiphon-region is omitted to leave egress open to any available server.
func BuildArgs(psiphonBin, psiphonDir, cleanIP, sni, cdnSet, egressRegion string, socksPort, httpPort int) []string {
	if socksPort <= 0 {
		socksPort = 10808
	}
	if httpPort <= 0 {
		httpPort = 10809
	}

	cleanIP = strings.TrimSpace(cleanIP)
	egressRegion = strings.ToUpper(strings.TrimSpace(egressRegion))
	if egressRegion == "AUTO" || egressRegion == "ANY" || egressRegion == "NONE" || egressRegion == "DEFAULT" {
		egressRegion = ""
	}

	args := []string{
		"--psiphon-only",
		"--psiphon-bin", psiphonBin,
		"--bind", fmt.Sprintf("127.0.0.1:%d", socksPort),
		"--psiphon-http", fmt.Sprintf("127.0.0.1:%d", httpPort),
		"--psiphon-dir", psiphonDir,
		"--log-level", "info",
	}

	if egressRegion != "" {
		args = append(args, "--psiphon-region", egressRegion)
	}

	isDirect := cleanIP == "" || cleanIP == "auto" || cleanIP == "direct"

	if !isDirect {
		args = append(args, "--psiphon-mode", "cdn")
		if sni == "" || cdnSet == "" {
			detSNI, detSet := detectIPDefaults(cleanIP)
			if sni == "" {
				sni = detSNI
			}
			if cdnSet == "" {
				cdnSet = detSet
			}
		}

		// Write ShirOKhorshid-exact config JSON and feed it to Aether
		cfgJSON, err := ShiroCdnFrontingConfigWithRegion(cleanIP, sni, egressRegion)
		if err == nil && psiphonDir != "" {
			configPath := filepath.Join(psiphonDir, "shiro_config.json")
			if writeErr := os.WriteFile(configPath, cfgJSON, 0644); writeErr == nil {
				args = append(args, "--psiphon-config", configPath)
			}
		}

		args = append(args, "--psiphon-cdn-ips", cleanIP)
		if sni != "" {
			args = append(args, "--psiphon-cdn-sni", sni)
		}
		if cdnSet != "" {
			args = append(args, "--psiphon-cdn-sets", cdnSet)
		}
	}

	return args
}

// StartWithRegion launches Aether in Psiphon-only mode with the selected Clean CDN IP, custom SNI, CDN set and egress region
func (r *Runner) StartWithRegion(cleanIP, sni, cdnSet, egressRegion string, socksPort, httpPort int) error {
	r.mu.Lock()

	// Stop any existing session
	r.stopLocked()

	aetherBin, psiphonBin, err := FindBinaries()
	if err != nil {
		r.mu.Unlock()
		return fmt.Errorf("locating psiphon binaries: %w", err)
	}

	if socksPort <= 0 {
		socksPort = 10808
	}
	if httpPort <= 0 {
		httpPort = 10809
	}

	r.socksPort = socksPort
	r.httpPort = httpPort
	r.cleanIP = strings.TrimSpace(cleanIP)
	r.egressRegion = strings.ToUpper(strings.TrimSpace(egressRegion))
	if r.egressRegion == "AUTO" || r.egressRegion == "ANY" || r.egressRegion == "NONE" || r.egressRegion == "DEFAULT" {
		r.egressRegion = ""
	}

	psiphonDir := getSafePsiphonDir()
	isDirect := r.cleanIP == "" || r.cleanIP == "auto" || r.cleanIP == "direct"

	args := BuildArgs(psiphonBin, psiphonDir, r.cleanIP, sni, cdnSet, r.egressRegion, socksPort, httpPort)

	cmd := exec.Command(aetherBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	cmd.Stdout = logger.Writer("psiphon", "info")
	cmd.Stderr = logger.Writer("psiphon", "warn")

	if isDirect {
		regLog := r.egressRegion
		if regLog == "" {
			regLog = "auto (any available)"
		}
		logger.Info("psiphon", "Starting direct Psiphon engine: region=%s, socks=%d, http=%d", regLog, socksPort, httpPort)
	} else {
		regLog := r.egressRegion
		if regLog == "" {
			regLog = "auto (any available)"
		}
		logger.Info("psiphon", "Starting engine: cleanIP=%s, sni=%s, cdnSet=%s, region=%s, socks=%d, http=%d", r.cleanIP, sni, cdnSet, regLog, socksPort, httpPort)
	}
	log.Printf("[Psiphon] Starting engine: %s %s", aetherBin, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
		r.mu.Unlock()
		return fmt.Errorf("starting aether psiphon process: %w", err)
	}

	r.cmd = cmd
	r.running = true
	r.connectedAt = time.Now()

	// Monitor process exit in background
	go func(c *exec.Cmd) {
		_ = c.Wait()
		r.mu.Lock()
		if r.cmd == c {
			r.running = false
			r.cmd = nil
		}
		r.mu.Unlock()
	}(cmd)

	// Release mutex so we do not block other operations or deadlock in probe loops
	r.mu.Unlock()

	// Step 1: Wait up to 15 seconds for local ports to bind
	logger.Info("psiphon", "Waiting for local SOCKS5 proxy 127.0.0.1:%d to bind...", socksPort)
	bindDeadline := time.Now().Add(15 * time.Second)
	portsBound := false
	for time.Now().Before(bindDeadline) {
		if !r.IsRunning() {
			_ = r.Stop()
			return fmt.Errorf("psiphon process terminated before ports bound")
		}
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), 400*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			portsBound = true
			break
		}
		time.Sleep(300 * time.Millisecond)
	}

	if !portsBound {
		logger.Error("psiphon", "Psiphon tunnel failed to bind ports %d/%d within 15s", socksPort, httpPort)
		_ = r.Stop()
		return fmt.Errorf("psiphon tunnel failed to bind ports %d/%d within 15s", socksPort, httpPort)
	}

	regLog := r.egressRegion
	if regLog == "" {
		regLog = "auto"
	}
	if isDirect {
		logger.Info("psiphon", "Local proxy ports bound (SOCKS5:%d, HTTP:%d). Negotiating direct Psiphon tunnel (region: %s)...", socksPort, httpPort, regLog)
	} else {
		logger.Info("psiphon", "Local proxy ports bound (SOCKS5:%d, HTTP:%d). Negotiating CDN tunnel with %s (region: %s)...", socksPort, httpPort, r.cleanIP, regLog)
	}

	// Step 2: Probe through Psiphon's local HTTP proxy until tunnel is active and passing traffic
	// Meek CDN handshake takes ~10-25s to establish the encrypted OSSH tunnel
	tunnelClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(&url.URL{
				Scheme: "http",
				Host:   fmt.Sprintf("127.0.0.1:%d", httpPort),
			}),
			DisableKeepAlives: true,
		},
		Timeout: 3 * time.Second,
	}

	tunnelDeadline := time.Now().Add(45 * time.Second)
	tunnelReady := false
	attempt := 0

	for time.Now().Before(tunnelDeadline) {
		attempt++
		if !r.IsRunning() {
			_ = r.Stop()
			return fmt.Errorf("psiphon process terminated unexpectedly during tunnel establishment")
		}

		resp, err := tunnelClient.Get("http://cp.cloudflare.com/generate_204")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == 204 || resp.StatusCode == 200 {
				tunnelReady = true
				break
			}
		}

		if attempt%3 == 0 {
			if isDirect {
				logger.Info("psiphon", "Direct Psiphon handshake in progress (waiting for tunnel to establish, %ds elapsed)...", attempt)
			} else {
				logger.Info("psiphon", "CDN fronting handshake in progress (waiting for tunnel to establish, %ds elapsed)...", attempt)
			}
		}
		time.Sleep(1 * time.Second)
	}

	if !tunnelReady {
		if isDirect {
			logger.Error("psiphon", "Psiphon direct tunnel handshake timed out after 45s (no active tunnel established)")
			_ = r.Stop()
			return fmt.Errorf("direct Psiphon tunnel failed to establish connection within 45s")
		} else {
			logger.Error("psiphon", "Psiphon tunnel handshake timed out after 45s (no active tunnel established)")
			_ = r.Stop()
			return fmt.Errorf("psiphon tunnel failed to establish connection with CDN node %s within 45s", r.cleanIP)
		}
	}

	logger.Info("psiphon", "Tunnel successfully active and verified on SOCKS5 127.0.0.1:%d and HTTP 127.0.0.1:%d", socksPort, httpPort)
	log.Printf("[Psiphon] Tunnel active and verified on SOCKS5 127.0.0.1:%d and HTTP 127.0.0.1:%d", socksPort, httpPort)
	return nil
}

// Stop terminates the Psiphon/Aether process and kills any lingering instances
func (r *Runner) Stop() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.stopLocked()
	return nil
}

func silentCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

func (r *Runner) stopLocked() {
	if r.cmd != nil && r.cmd.Process != nil {
		pid := r.cmd.Process.Pid
		log.Printf("[Psiphon] Stopping engine (PID %d)...", pid)

		// Terminate process tree cleanly without flashing console window
		_ = silentCmd("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid)).Run()
		_ = r.cmd.Process.Kill()
		r.cmd = nil
	}

	// Cleanup any orphan engine processes silently
	_ = silentCmd("taskkill", "/F", "/IM", "aether.exe").Run()
	_ = silentCmd("taskkill", "/F", "/IM", "psiphon-tunnel-core.exe").Run()

	appData := os.Getenv("APPDATA")
	if appData != "" {
		_ = os.Remove(filepath.Join(appData, "Conective", "psiphon", "psiphon.boltdb.lock"))
	}

	r.running = false
	r.cleanIP = ""
	r.egressRegion = ""
	r.lastLatency = 0
	r.exitIP = ""

	runtime.GC()
	debug.FreeOSMemory()
	logger.Info("psiphon", "Engine stopped cleanly")
}

// IsRunning reports whether Psiphon is currently executing
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
}

// GetEgressRegion returns the active egress country/region code
func (r *Runner) GetEgressRegion() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.egressRegion
}

// VerifyTunnel performs a real outbound HTTP probe through the local SOCKS/HTTP proxy
func (r *Runner) VerifyTunnel(wait time.Duration) (int, string, error) {
	deadline := time.Now().Add(wait)
	var lastErr error

	for time.Now().Before(deadline) {
		lat, exitIP, err := r.probeOnce()
		if err == nil {
			r.mu.Lock()
			if lat > 0 {
				r.lastLatency = lat
			}
			if exitIP != "" {
				r.exitIP = exitIP
			}
			r.mu.Unlock()
			return lat, exitIP, nil
		}
		lastErr = err
		time.Sleep(500 * time.Millisecond)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("verification timed out")
	}
	return -1, "", lastErr
}

func (r *Runner) probeOnce() (int, string, error) {
	r.mu.Lock()
	sp := r.socksPort
	hp := r.httpPort
	r.mu.Unlock()

	if sp <= 0 && hp <= 0 {
		return -1, "", fmt.Errorf("no active proxy ports")
	}

	// Try via HTTP proxy first, fallback to SOCKS5
	var proxyURL *url.URL
	if hp > 0 {
		proxyURL, _ = url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hp))
	} else {
		proxyURL, _ = url.Parse(fmt.Sprintf("socks5://127.0.0.1:%d", sp))
	}

	tr := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Transport: tr,
		Timeout:   5 * time.Second,
	}
	defer client.CloseIdleConnections()

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Test against Cloudflare generate_204 or Cloudflare trace
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://www.cloudflare.com/cdn-cgi/trace", nil)
	resp, err := client.Do(req)
	if err != nil {
		// Fallback to google.com 204
		req2, _ := http.NewRequestWithContext(ctx, "GET", "http://cp.cloudflare.com/generate_204", nil)
		resp2, err2 := client.Do(req2)
		if err2 != nil {
			return -1, "", err
		}
		defer resp2.Body.Close()
		latency := int(time.Since(start).Milliseconds())
		return latency, "", nil
	}
	defer resp.Body.Close()

	latency := int(time.Since(start).Milliseconds())

	// Read trace output for exit IP
	buf := make([]byte, 2048)
	n, _ := resp.Body.Read(buf)
	body := string(buf[:n])
	exitIP := ""
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "ip=") {
			exitIP = strings.TrimPrefix(line, "ip=")
			break
		}
	}

	return latency, exitIP, nil
}
