package xray

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	xcore "github.com/xtls/xray-core/core"
	xserial "github.com/xtls/xray-core/infra/conf/serial"

	// Register all Xray-core protocols, transports and tun
	_ "github.com/xtls/xray-core/main/distro/all"
	_ "github.com/xtls/xray-core/proxy/tun"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/psiphon"
	"freenode/pkg/tun"
	"freenode/pkg/v2go"
)

type Runner struct {
	mu                sync.RWMutex
	instance          *xcore.Instance
	psiphonRunner     *psiphon.Runner
	activeNode        *models.Config
	connectedAt       time.Time
	socksPort         int
	httpPort          int
	baseSocksPort     int
	baseHttpPort      int
	testEndpoint      string
	lastLatency       int
	exitIP            string
	tunMode           bool
	currentTunAdapter string
	tunMgr            *tun.Manager
	shareLAN          bool
	gamingMode        bool
	db                *database.DB
}

func (r *Runner) SetDB(db *database.DB) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.db = db
}

func (r *Runner) SetShareLAN(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.shareLAN = enabled
}

func (r *Runner) GetShareLAN() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.shareLAN
}

func (r *Runner) SetGamingMode(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.gamingMode = enabled
}

func (r *Runner) GetGamingMode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.gamingMode
}

func NewRunner(socksPort, httpPort int, testEndpoint string) *Runner {
	if socksPort <= 0 {
		socksPort = 10808
	}
	if httpPort <= 0 {
		httpPort = 10809
	}
	if testEndpoint == "" || strings.Contains(testEndpoint, "gstatic") {
		testEndpoint = "http://cp.cloudflare.com/generate_204"
	}
	r := &Runner{
		socksPort:     socksPort,
		httpPort:      httpPort,
		baseSocksPort: socksPort,
		baseHttpPort:  httpPort,
		testEndpoint:  testEndpoint,
		tunMgr:            tun.NewManager(),
		psiphonRunner:     psiphon.NewRunner(),
		currentTunAdapter: "FreeNodeTUN",
	}
	r.tunMgr.RecoverOrphanedTUNRoutes()
	return r
}

func (r *Runner) SetTunMode(enabled bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if enabled && !tun.IsAdmin() {
		return fmt.Errorf("TUN Mode requires Administrator privileges. Please run FreeNode as Administrator.")
	}
	r.tunMode = enabled
	return nil
}

func (r *Runner) GetTunMode() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunMode
}

// TunAdapterName returns the static TUN adapter name used for Wintun device reuse
func (r *Runner) TunAdapterName() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.currentTunAdapter != "" {
		return r.currentTunAdapter
	}
	return "FreeNodeTUN"
}

func (r *Runner) IsRunning() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.instance != nil || (r.psiphonRunner != nil && r.psiphonRunner.IsRunning())
}

func (r *Runner) GetActiveNode() *models.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.activeNode
}

func (r *Runner) GetStatus() models.ConnectionStatus {
	r.mu.RLock()
	defer r.mu.RUnlock()

	isConnected := r.instance != nil || (r.psiphonRunner != nil && r.psiphonRunner.IsRunning())

	status := models.ConnectionStatus{
		Connected:   isConnected,
		State:       "disconnected",
		SocksPort:   r.socksPort,
		HTTPPort:    r.httpPort,
		Latency:     r.lastLatency,
		ExitIP:      r.exitIP,
		ActiveNode:  r.activeNode,
		TunMode:     r.tunMode,
		GamingMode:  r.gamingMode,
		ShareLAN:    r.shareLAN,
		IsAdmin:     tun.IsAdmin(),
	}

	if isConnected {
		status.State = "connected"
		status.ConnectedSince = &r.connectedAt
	}

	return status
}

// Connect starts the local proxy client for the chosen node and verifies health
func (r *Runner) Connect(node *models.Config) error {
	if node.Protocol == "psiphon" || strings.HasPrefix(node.Identity, "cleanip-") || node.Source == "Clean IP Fronting" {
		r.mu.Lock()
		if r.instance != nil {
			if r.tunMode {
				_ = r.tunMgr.TeardownAdapter()
			}
			_ = r.instance.Close()
			r.instance = nil
			r.activeNode = nil
			time.Sleep(300 * time.Millisecond)
		}
		if r.psiphonRunner != nil {
			_ = r.psiphonRunner.Stop()
		}

		preferredSocks := r.baseSocksPort
		if preferredSocks <= 0 {
			preferredSocks = 10808
		}
		preferredHTTP := r.baseHttpPort
		if preferredHTTP <= 0 {
			preferredHTTP = 10809
		}
		if r.db != nil {
			if sp, err := strconv.Atoi(r.db.GetSetting("socks_port", "10808")); err == nil && sp > 0 {
				preferredSocks = sp
			}
			if hp, err := strconv.Atoi(r.db.GetSetting("http_port", "10809")); err == nil && hp > 0 {
				preferredHTTP = hp
			}
		}
		r.socksPort = ensureAvailablePort(preferredSocks, 20808)
		r.httpPort = ensureAvailablePort(preferredHTTP, 20809)
		tunEnabled := r.tunMode
		r.mu.Unlock()

		if err := r.psiphonRunner.Start(node.Server, r.socksPort, r.httpPort); err != nil {
			return fmt.Errorf("starting psiphon tunnel: %w", err)
		}

		if tunEnabled {
			log.Printf("[Runner] Psiphon CDN fronting active on ports %d/%d (system-wide routing handled via proxy)", r.socksPort, r.httpPort)
		}

		r.mu.Lock()
		r.activeNode = node
		r.connectedAt = time.Now()
		r.lastLatency = node.Latency
		r.exitIP = node.ExitIP
		r.mu.Unlock()

		go func() {
			lat, exitIP, err := r.psiphonRunner.VerifyTunnel(10 * time.Second)
			if err == nil {
				r.mu.Lock()
				if lat > 0 {
					r.lastLatency = lat
				}
				if exitIP != "" {
					r.exitIP = exitIP
				}
				r.mu.Unlock()
			}
		}()
		return nil
	}

	if r.psiphonRunner != nil && r.psiphonRunner.IsRunning() {
		_ = r.psiphonRunner.Stop()
	}

	r.mu.Lock()
	if r.db != nil {
		r.shareLAN = r.db.GetSetting("share_lan", "false") == "true"
		r.gamingMode = r.db.GetSetting("gaming_mode", "false") == "true"
	}
	shareLAN := r.shareLAN
	gamingMode := r.gamingMode

	if r.instance != nil {
		if r.tunMode {
			_ = r.tunMgr.TeardownAdapter()
		}
		_ = r.instance.Close()
		r.instance = nil
		r.activeNode = nil
		time.Sleep(500 * time.Millisecond) // Let previous session release ports and NDIS handle
	}
	tunEnabled := r.tunMode
	tunAdapterName := "ConectiveTUN"
	if r.currentTunAdapter != "" {
		tunAdapterName = r.currentTunAdapter
	}

	// Reset ports to configured base values to prevent port drifting across connections
	preferredSocks := r.baseSocksPort
	if preferredSocks <= 0 {
		preferredSocks = 10808
	}
	preferredHTTP := r.baseHttpPort
	if preferredHTTP <= 0 {
		preferredHTTP = 10809
	}
	if r.db != nil {
		if sp, err := strconv.Atoi(r.db.GetSetting("socks_port", "10808")); err == nil && sp > 0 {
			preferredSocks = sp
		}
		if hp, err := strconv.Atoi(r.db.GetSetting("http_port", "10809")); err == nil && hp > 0 {
			preferredHTTP = hp
		}
	}

	r.socksPort = ensureAvailablePort(preferredSocks, 20808)
	r.httpPort = ensureAvailablePort(preferredHTTP, 20809)
	r.mu.Unlock()

	if tunEnabled && !tun.IsAdmin() {
		return fmt.Errorf("TUN Mode requires Administrator privileges. Please run FreeNode as Administrator.")
	}

	outbound, err := v2go.ConvertToXrayOutbound(node)
	if err != nil {
		return fmt.Errorf("converting node outbound: %w", err)
	}

	// Configure stream sockopt
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

	// Discover physical interface in TUN mode to bind outbounds and prevent routing loops
	var physIface string
	if tunEnabled {
		physIface = r.tunMgr.PhysicalInterface()
		if physIface == "" {
			_, physIface, _ = tun.FindPhysicalGatewayAndInterface()
		}
		if physIface != "" {
			sockopt["interface"] = physIface
		}
	}

	// Apply low-latency socket options (TCP_NODELAY, TFO, fast keepalive) in gaming mode
	if gamingMode {
		sockopt["tcpNoDelay"] = true
		sockopt["tcpFastOpen"] = true
		sockopt["tcpKeepAliveIdle"] = 15
		sockopt["tcpKeepAliveInterval"] = 5
	}

	// Anti-DPI TLS ClientHello fragmentation:
	// Only activate when explicitly requested (tag 'fragment' / 'anti-dpi' / 'clean-ip' or setting 'anti_dpi').
	// Strictly targets TLS ClientHello (packets 1-1) with fast interval to prevent buffer stalls and high CPU.
	needFragment := false
	if r.db != nil && r.db.GetSetting("anti_dpi", "false") == "true" {
		needFragment = true
	} else if strings.Contains(node.Tags, "fragment") || strings.Contains(node.Tags, "anti-dpi") || strings.Contains(node.Tags, "clean-ip") {
		needFragment = true
	}

	if needFragment {
		sockopt["dialerProxy"] = "fragment"
	}

	listenHost := "127.0.0.1"
	if shareLAN {
		listenHost = "0.0.0.0"
	}

	inbounds := []v2go.M{
		{
			"tag":      "socks-in",
			"port":     r.socksPort,
			"listen":   listenHost,
			"protocol": "socks",
			"settings": v2go.M{
				"auth": "noauth",
				"udp":  true,
			},
			"sniffing": v2go.M{
				"enabled":      true,
				"destOverride": []string{"http", "tls"},
			},
		},
		{
			"tag":      "http-in",
			"port":     r.httpPort,
			"listen":   listenHost,
			"protocol": "http",
			"settings": v2go.M{
				"allowTransparent": false,
			},
		},
	}

	if tunEnabled {
		tunMTU := 1500
		if gamingMode {
			tunMTU = 1400 // Gaming low-latency MTU prevents packet fragmentation overhead
		}
		inbounds = append(inbounds, v2go.M{
			"tag":      "tun-in",
			"protocol": "tun",
			"settings": v2go.M{
				"name": tunAdapterName,
				"MTU":  tunMTU,
			},
			"sniffing": v2go.M{
				"enabled":      true,
				"destOverride": []string{"http", "tls", "quic"},
				"routeOnly":    false,
			},
		})
	}

	fragmentOutbound := v2go.M{
		"tag":      "fragment",
		"protocol": "freedom",
		"settings": v2go.M{
			"fragment": v2go.M{
				"packets":  "1-1",
				"length":   "100-200",
				"interval": "1-5",
			},
		},
	}
	directOutbound := v2go.M{
		"tag":      "direct",
		"protocol": "freedom",
	}
	if physIface != "" {
		fragmentOutbound["streamSettings"] = v2go.M{"sockopt": v2go.M{"interface": physIface}}
		directOutbound["streamSettings"] = v2go.M{"sockopt": v2go.M{"interface": physIface}}
	}

	// Build complete client config with SOCKS, HTTP, and optional TUN inbounds
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
		"inbounds": inbounds,
		"outbounds": []v2go.M{
			outbound,
			fragmentOutbound,
			directOutbound,
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

	var instance *xcore.Instance
	var lastErr error
	tunAdapterCandidates := []string{"ConectiveTUN", "ConectiveTUN2", "ConectiveTUN3"}
	candidateIdx := 0
	if r.currentTunAdapter != "" {
		for i, cand := range tunAdapterCandidates {
			if cand == r.currentTunAdapter {
				candidateIdx = i
				break
			}
		}
	}

	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			// If inbound ports had conflict or TIME_WAIT, try fallback range
			r.httpPort = ensureAvailablePort(r.httpPort+1, 20810+attempt*10)
			r.socksPort = ensureAvailablePort(r.socksPort+1, 20800+attempt*10)
			inbounds[0]["port"] = r.socksPort
			inbounds[1]["port"] = r.httpPort

			// If TUN mode is enabled, rotate adapter candidate name to bypass any lingering PnP lock
			if tunEnabled {
				candidateIdx = (candidateIdx + 1) % len(tunAdapterCandidates)
				tunAdapterName = tunAdapterCandidates[candidateIdx]
				for _, inb := range inbounds {
					if inb["tag"] == "tun-in" {
						if st, ok := inb["settings"].(v2go.M); ok {
							st["name"] = tunAdapterName
						}
					}
				}
			}
			configMap["inbounds"] = inbounds
		} else if tunEnabled {
			tunAdapterName = tunAdapterCandidates[candidateIdx]
			for _, inb := range inbounds {
				if inb["tag"] == "tun-in" {
					if st, ok := inb["settings"].(v2go.M); ok {
						st["name"] = tunAdapterName
					}
				}
			}
			configMap["inbounds"] = inbounds
		}

		jsonBytes, err := json.Marshal(configMap)
		if err != nil {
			return fmt.Errorf("marshalling client config: %w", err)
		}

		cfg, err := xserial.LoadJSONConfig(bytes.NewReader(jsonBytes))
		if err != nil {
			return fmt.Errorf("loading xray client config: %w", err)
		}

		instance, err = xcore.New(cfg)
		if err != nil {
			lastErr = err
			if tunEnabled {
				log.Printf("[Runner] xcore.New attempt %d with adapter %s failed: %v, rotating adapter...", attempt+1, tunAdapterName, err)
				time.Sleep(500 * time.Millisecond) // Let Windows PnP release adapter handles
				continue
			}
			return fmt.Errorf("creating client instance: %w", err)
		}

		startErr := instance.Start()
		if startErr == nil {
			lastErr = nil
			if tunEnabled {
				r.mu.Lock()
				r.currentTunAdapter = tunAdapterName
				r.mu.Unlock()
			}
			break
		}
		_ = instance.Close()
		lastErr = startErr
		time.Sleep(500 * time.Millisecond) // Let Windows release sockets/adapters before retry
	}
	if lastErr != nil {
		return fmt.Errorf("starting client instance (%s): %w", tunAdapterName, lastErr)
	}

	// Give WireGuard / proxy a brief moment to initialize before health check
	if node.Protocol == "wireguard" {
		time.Sleep(500 * time.Millisecond)
	}

	// If TUN mode is enabled, set up adapter IP and routing
	if tunEnabled {
		if err := r.tunMgr.SetupAdapter(tunAdapterName, node.Server); err != nil {
			_ = instance.Close()
			return fmt.Errorf("failed setting up TUN routes: %w", err)
		}
	}

	r.mu.Lock()
	r.instance = instance
	r.activeNode = node
	r.connectedAt = time.Now()
	r.lastLatency = node.Latency
	r.exitIP = node.ExitIP
	r.mu.Unlock()

	// Asynchronously probe connectivity and live exit IP (matching Shir o Khorshid & SenPaiScanner behavior).
	// A temporary handshake delay or endpoint jitter never kills or disconnects an already-active tunnel.
	go func() {
		latency, exitIP, loc, err := r.verifyHealth()
		if err == nil {
			r.mu.Lock()
			if latency > 0 {
				r.lastLatency = latency
			}
			if exitIP != "" {
				r.exitIP = exitIP
				if r.activeNode != nil {
					r.activeNode.ExitIP = exitIP
				}
			}
			if loc != "" && r.activeNode != nil {
				cur := strings.ToUpper(r.activeNode.Country)
				if cur == "CLOUDFLARE" || cur == "CF" || cur == "WW" || cur == "UN" || cur == "" {
					r.activeNode.Country = strings.ToUpper(loc)
				}
			}
			r.mu.Unlock()
		}
	}()

	return nil
}

// Disconnect stops the in-process client Xray instance and cleans up TUN
func (r *Runner) Disconnect() (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			log.Printf("[Runner] Panic during Disconnect recovered safely: %v", rec)
		}
		runtime.GC()
		debug.FreeOSMemory()
	}()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.tunMode {
		_ = r.tunMgr.TeardownAdapter()
	}

	if r.psiphonRunner != nil && r.psiphonRunner.IsRunning() {
		_ = r.psiphonRunner.Stop()
		r.activeNode = nil
		r.lastLatency = 0
		r.exitIP = ""
	}

	if r.instance != nil {
		inst := r.instance
		r.instance = nil
		r.activeNode = nil
		r.lastLatency = 0
		r.exitIP = ""

		func() {
			defer func() {
				if rec := recover(); rec != nil {
					log.Printf("[Runner] Panic during instance.Close recovered: %v", rec)
				}
			}()
			err = inst.Close()
		}()
		return err
	}
	return nil
}

// Stop terminates active client instances and reclaims OS memory
func (r *Runner) Stop() error {
	defer func() {
		runtime.GC()
		debug.FreeOSMemory()
	}()
	return r.Disconnect()
}

// IsPsiphon reports whether the Psiphon runner is currently active
func (r *Runner) IsPsiphon() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.psiphonRunner != nil && r.psiphonRunner.IsRunning()
}

// VerifyTunnel synchronously checks that traffic really flows through the active tunnel,
// retrying for up to `wait` to give the transport time to handshake.
func (r *Runner) VerifyTunnel(wait time.Duration) (int, string, error) {
	if r.psiphonRunner != nil && r.psiphonRunner.IsRunning() {
		lat, exitIP, err := r.psiphonRunner.VerifyTunnel(wait)
		if err == nil {
			r.mu.Lock()
			if lat > 0 {
				r.lastLatency = lat
			}
			if exitIP != "" {
				r.exitIP = exitIP
			}
			r.mu.Unlock()
		}
		return lat, exitIP, err
	}

	deadline := time.Now().Add(wait)
	var lastErr error
	for {
		lat, ip, _, err := r.verifyHealth()
		if err == nil {
			r.mu.Lock()
			if lat > 0 {
				r.lastLatency = lat
			}
			if ip != "" {
				r.exitIP = ip
			}
			r.mu.Unlock()
			return lat, ip, nil
		}
		lastErr = err
		if time.Now().After(deadline) {
			return -1, "", lastErr
		}
		time.Sleep(800 * time.Millisecond)
	}
}

// verifyHealth tests that the local HTTP proxy on 127.0.0.1:HTTPPort is actually routing traffic
func (r *Runner) verifyHealth() (int, string, string, error) {
	proxyURL, err := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", r.httpPort))
	if err != nil {
		return -1, "", "", err
	}

	tr := &http.Transport{
		Proxy:             http.ProxyURL(proxyURL),
		DisableKeepAlives: true,
	}
	defer tr.CloseIdleConnections()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: tr,
	}
	defer client.CloseIdleConnections()

	// High-availability endpoints unblocked in Iran (Cloudflare Anycast and Firefox detection)
	endpoints := []string{
		"http://cp.cloudflare.com/generate_204",
		"http://www.cloudflare.com/cdn-cgi/trace",
		"http://detectportal.firefox.com/success.txt",
	}
	if r.testEndpoint != "" && !strings.Contains(r.testEndpoint, "google") && !strings.Contains(r.testEndpoint, "gstatic") && r.testEndpoint != "http://cp.cloudflare.com/generate_204" {
		endpoints = append(endpoints, r.testEndpoint)
	}

	var lastErr error
	var latency int
	var exitIP string
	var loc string
	passed := false

	for _, ep := range endpoints {
		start := time.Now()
		resp, err := client.Get(ep)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
			latency = int(time.Since(start).Milliseconds())
			if latency <= 0 {
				latency = 1
			}

			if strings.Contains(ep, "trace") {
				var buf [2048]byte
				n, _ := resp.Body.Read(buf[:])
				for _, line := range bytes.Split(buf[:n], []byte("\n")) {
					if bytes.HasPrefix(line, []byte("ip=")) {
						exitIP = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("ip="))))
					} else if bytes.HasPrefix(line, []byte("loc=")) {
						loc = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("loc="))))
					}
				}
			}
			_ = resp.Body.Close()
			passed = true
			break
		}
		_ = resp.Body.Close()
		lastErr = fmt.Errorf("status code %d", resp.StatusCode)
	}

	if !passed {
		return -1, "", "", fmt.Errorf("connectivity check failed: %v", lastErr)
	}

	// If exit IP wasn't fetched in the health endpoint, try a quick probe
	if exitIP == "" {
		ipResp, err := client.Get("http://www.cloudflare.com/cdn-cgi/trace")
		if err == nil {
			var buf [1024]byte
			n, _ := ipResp.Body.Read(buf[:])
			_ = ipResp.Body.Close()
			for _, line := range bytes.Split(buf[:n], []byte("\n")) {
				if bytes.HasPrefix(line, []byte("ip=")) {
					exitIP = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("ip="))))
				} else if bytes.HasPrefix(line, []byte("loc=")) {
					loc = string(bytes.TrimSpace(bytes.TrimPrefix(line, []byte("loc="))))
				}
			}
		}
	}

	return latency, exitIP, loc, nil
}

func ensureAvailablePort(preferredPort, fallbackStart int) int {
	if preferredPort <= 0 {
		preferredPort = fallbackStart
	}
	// Check if preferred port is free
	if isPortFree(preferredPort) {
		return preferredPort
	}
	// Try fallback range
	for p := fallbackStart; p < fallbackStart+100; p++ {
		if isPortFree(p) {
			return p
		}
	}
	return preferredPort
}

func isPortFree(port int) bool {
	// Probe whether another local service is actively listening on this port.
	// Using DialTimeout avoids creating and closing a TCP listener, which on Windows
	// causes WSAEADDRINUSE (TIME_WAIT) errors when Xray subsequently binds the port.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 60*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return false
	}
	return true
}

func parseLines(text string) []string {
	var list []string
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
			list = append(list, trimmed)
		}
	}
	return list
}

func (r *Runner) buildRoutingRules() []v2go.M {
	gamingMode := r.gamingMode
	if r.db != nil && !gamingMode {
		gamingMode = r.db.GetSetting("gaming_mode", "false") == "true"
	}

	lanIPs := []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "127.0.0.0/8", "100.64.0.0/10"}
	if gamingMode {
		// Include multicast and broadcast addresses used by LAN gaming & local server discovery
		lanIPs = append(lanIPs, "224.0.0.0/4", "255.255.255.255/32")
	}

	// Base rule: Local LAN and private subnets always route Direct
	rules := []v2go.M{
		{
			"type":        "field",
			"ip":          lanIPs,
			"outboundTag": "direct",
		},
	}

	if r.db == nil {
		rules = append(rules, v2go.M{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "proxy",
		})
		return rules
	}

	routingMode := r.db.GetSetting("routing_mode", "blacklist")
	// Backward compatibility mapping
	if routingMode == "bypass_iran" || routingMode == "" {
		routingMode = "blacklist"
	} else if routingMode == "global" {
		routingMode = "proxy_all"
	} else if routingMode == "proxy_apps_only" {
		routingMode = "whitelist"
	}

	directDomains := parseLines(r.db.GetSetting("direct_domains", ""))
	directApps := parseLines(r.db.GetSetting("direct_apps", ""))
	proxyDomains := parseLines(r.db.GetSetting("proxy_domains", ""))
	proxyApps := parseLines(r.db.GetSetting("proxy_apps", ""))
	blockDomains := parseLines(r.db.GetSetting("block_domains", ""))

	// 1. Blocked Domains (if specified)
	if len(blockDomains) > 0 {
		rules = append(rules, v2go.M{
			"type":        "field",
			"domain":      blockDomains,
			"outboundTag": "block",
		})
	}

	switch routingMode {
	case "proxy_all":
		// Gaming mode: prioritize UDP traffic on fast path
		if gamingMode {
			rules = append(rules, v2go.M{
				"type":        "field",
				"network":     "udp",
				"outboundTag": "proxy",
			})
		}
		// All non-LAN traffic passes through proxy
		rules = append(rules, v2go.M{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "proxy",
		})

	case "whitelist":
		// Only matching apps or domains pass through proxy; rest is direct
		if len(proxyApps) > 0 {
			rules = append(rules, v2go.M{
				"type":        "field",
				"process":     proxyApps,
				"outboundTag": "proxy",
			})
		}
		if len(proxyDomains) > 0 {
			rules = append(rules, v2go.M{
				"type":        "field",
				"domain":      proxyDomains,
				"outboundTag": "proxy",
			})
		}
		// Catch-all: everything else bypasses proxy (direct)
		rules = append(rules, v2go.M{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "direct",
		})

	case "blacklist":
		fallthrough
	default:
		// Blacklisted apps and domains bypass proxy (direct)
		if len(directApps) > 0 {
			rules = append(rules, v2go.M{
				"type":        "field",
				"process":     directApps,
				"outboundTag": "direct",
			})
		}
		if len(directDomains) > 0 {
			rules = append(rules, v2go.M{
				"type":        "field",
				"domain":      directDomains,
				"outboundTag": "direct",
			})
		}
		// Gaming mode: prioritize UDP traffic on fast path
		if gamingMode {
			rules = append(rules, v2go.M{
				"type":        "field",
				"network":     "udp",
				"outboundTag": "proxy",
			})
		}
		// Catch-all: rest of traffic goes through proxy
		rules = append(rules, v2go.M{
			"type":        "field",
			"network":     "tcp,udp",
			"outboundTag": "proxy",
		})
	}

	return rules
}

func isFileExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "already exists") ||
		strings.Contains(msg, "file exists") ||
		strings.Contains(msg, "0x50") ||
		strings.Contains(msg, "cannot create a file when that file already exists")
}
