package psiphon

import (
	"context"
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
)

// Runner manages the lifecycle of the Psiphon / Aether circumvention engine
type Runner struct {
	mu          sync.Mutex
	cmd         *exec.Cmd
	running     bool
	cleanIP     string
	socksPort   int
	httpPort    int
	connectedAt time.Time
	lastLatency int
	exitIP      string
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

// Start launches Aether in Psiphon-only mode with the selected Clean CDN IP
func (r *Runner) Start(cleanIP string, socksPort, httpPort int) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Stop any existing session
	r.stopLocked()

	aetherBin, psiphonBin, err := FindBinaries()
	if err != nil {
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

	args := []string{
		"--psiphon-only",
		"--psiphon-bin", psiphonBin,
		"--bind", fmt.Sprintf("127.0.0.1:%d", socksPort),
		"--psiphon-http", fmt.Sprintf("127.0.0.1:%d", httpPort),
		"--log-level", "info",
	}

	if r.cleanIP != "" && r.cleanIP != "auto" {
		args = append(args, "--psiphon-cdn-ips", r.cleanIP)
	}

	cmd := exec.Command(aetherBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}

	log.Printf("[Psiphon] Starting engine: %s %s", aetherBin, strings.Join(args, " "))
	if err := cmd.Start(); err != nil {
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

	// Wait up to 15 seconds for socks/http ports to start accepting connections
	deadline := time.Now().Add(15 * time.Second)
	ready := false
	for time.Now().Before(deadline) {
		time.Sleep(300 * time.Millisecond)
		conn, dialErr := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", socksPort), 500*time.Millisecond)
		if dialErr == nil {
			_ = conn.Close()
			ready = true
			break
		}
	}

	if !ready {
		r.stopLocked()
		return fmt.Errorf("psiphon tunnel failed to bind ports %d/%d within 15s", socksPort, httpPort)
	}

	log.Printf("[Psiphon] Tunnel active on SOCKS5 127.0.0.1:%d and HTTP 127.0.0.1:%d", socksPort, httpPort)
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

	r.running = false
	r.cleanIP = ""
	r.lastLatency = 0
	r.exitIP = ""

	runtime.GC()
	debug.FreeOSMemory()
}

// IsRunning reports whether Psiphon is currently executing
func (r *Runner) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running
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
