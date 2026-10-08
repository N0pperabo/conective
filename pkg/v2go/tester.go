package v2go

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	xcore "github.com/xtls/xray-core/core"
	xserial "github.com/xtls/xray-core/infra/conf/serial"

	// Register all Xray-core protocols and transports
	_ "github.com/xtls/xray-core/main/distro/all"

	"freenode/pkg/models"
)

type IPDataDetails struct {
	IP            string  `json:"ip"`
	IsEU          bool    `json:"is_eu"`
	City          string  `json:"city"`
	Region        string  `json:"region"`
	CountryName   string  `json:"country_name"`
	CountryCode   string  `json:"country_code"`
	ContinentName string  `json:"continent_name"`
	ContinentCode string  `json:"continent_code"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	CurrencyCode  string  `json:"currency_code"`
	TimeZone      string  `json:"time_zone"`
	Organisation  string  `json:"organisation"`
	TrustScore    int     `json:"trust_score"`
	RiskLevel     string  `json:"risk_level"`
	ThreatsCount  int     `json:"threats_count"`
	IsDatacenter  bool    `json:"is_datacenter"`
	IsVPN         bool    `json:"is_vpn"`
	IsProxy       bool    `json:"is_proxy"`
	IsTor         bool    `json:"is_tor"`
	IsThreat      bool    `json:"is_threat"`
	IsAttacker    bool    `json:"is_attacker"`
	RawJSON       string  `json:"raw_json,omitempty"`
}

type TestResult struct {
	Config       *models.Config
	Latency      int // ms, -1 if failed
	ExitIP       string
	TrustScore   int
	RiskLevel    string
	ThreatsCount int
	Organisation string
	IPData       *IPDataDetails
	Error        error
}

type LiveTester struct {
	endpoint   string
	timeout    time.Duration
	exitIPURL  string
}

func NewLiveTester(endpoint string, timeoutSec int) *LiveTester {
	if endpoint == "" || strings.Contains(endpoint, "gstatic") {
		endpoint = "http://cp.cloudflare.com/generate_204"
	}
	if timeoutSec <= 0 {
		timeoutSec = 5
	}
	return &LiveTester{
		endpoint:  endpoint,
		timeout:   time.Duration(timeoutSec) * time.Second,
		exitIPURL: "http://www.cloudflare.com/cdn-cgi/trace",
	}
}

// TestSingle tests a single configuration through an embedded in-memory Xray-core instance
func (t *LiveTester) TestSingle(ctx context.Context, c *models.Config) (int, string, *IPDataDetails, error) {
	if c.Protocol == "psiphon" || strings.HasPrefix(c.Identity, "cleanip-") || c.Source == "Clean IP Fronting" {
		port := c.Port
		if port <= 0 {
			port = 443
		}
		addr := net.JoinHostPort(c.Server, strconv.Itoa(port))
		timeout := t.timeout
		if timeout <= 0 {
			timeout = 5 * time.Second
		}

		probeSNI := c.SNI
		if probeSNI == "" {
			if strings.Contains(strings.ToLower(c.Tags), "cloudflare") || strings.Contains(strings.ToLower(c.Name), "cloudflare") {
				probeSNI = "cp.cloudflare.com"
			} else if strings.Contains(strings.ToLower(c.Tags), "cloudfront") || strings.Contains(strings.ToLower(c.Name), "cloudfront") {
				probeSNI = "d1.cloudfront.net"
			} else {
				probeSNI = "a248.e.akamai.net"
			}
		}

		dialer := &net.Dialer{Timeout: timeout}
		tlsDialer := &tls.Dialer{
			NetDialer: dialer,
			Config: &tls.Config{
				ServerName:         probeSNI,
				InsecureSkipVerify: true,
			},
		}

		start := time.Now()
		conn, err := tlsDialer.DialContext(ctx, "tcp", addr)
		if err != nil {
			conn, err = dialer.DialContext(ctx, "tcp", addr)
		}
		if err != nil {
			return -1, "", nil, err
		}
		_ = conn.Close()

		latency := int(time.Since(start).Milliseconds())
		if latency <= 0 {
			latency = 1
		}
		c.Status = "working"
		exitIP := c.Server
		return latency, exitIP, nil, nil
	}
	return t.testCore(ctx, c, true)
}

func (t *LiveTester) testCore(ctx context.Context, c *models.Config, checkTrust bool) (int, string, *IPDataDetails, error) {
	if c.Protocol == "psiphon" || strings.HasPrefix(c.Identity, "cleanip-") || c.Source == "Clean IP Fronting" {
		return t.TestSingle(ctx, c)
	}

	// Fast TCP pre-dial check:
	// For standard TCP/WS/gRPC configs, if the host:port cannot even establish a TCP connection,
	// the server is completely unreachable. Pre-filtering avoids creating an Xray-core instance,
	// saving ~90% CPU & memory allocations across dead nodes.
	if c.Protocol != "hysteria2" && c.Protocol != "tuic" && c.Protocol != "wireguard" && c.Transport != "kcp" && c.Transport != "quic" {
		if c.Server != "" && c.Port > 0 {
			addr := net.JoinHostPort(c.Server, strconv.Itoa(c.Port))
			dialTimeout := t.timeout
			if dialTimeout < 5*time.Second {
				dialTimeout = 5 * time.Second
			}
			d := net.Dialer{Timeout: dialTimeout}
			conn, err := d.DialContext(ctx, "tcp", addr)
			if err != nil {
				return -1, "", nil, fmt.Errorf("pre-check server unreachable: %w", err)
			}
			_ = conn.Close()
		}
	}

	outbound, err := ConvertToXrayOutbound(c)
	if err != nil {
		return -1, "", nil, fmt.Errorf("converting outbound: %w", err)
	}

	fullConfig := M{
		"log": M{"loglevel": "none"},
		"dns": M{
			"servers": []string{
				"1.1.1.1",
				"8.8.8.8",
			},
		},
		"outbounds": []M{
			outbound,
			{"protocol": "freedom", "tag": "direct"},
		},
	}

	jsonBytes, err := json.Marshal(fullConfig)
	if err != nil {
		return -1, "", nil, fmt.Errorf("marshalling config: %w", err)
	}

	cfg, err := xserial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		return -1, "", nil, fmt.Errorf("loading xray json: %w", err)
	}

	instance, err := xcore.New(cfg)
	if err != nil {
		return -1, "", nil, fmt.Errorf("creating xray instance: %w", err)
	}

	if err := instance.Start(); err != nil {
		return -1, "", nil, fmt.Errorf("starting xray instance: %w", err)
	}
	defer instance.Close()

	dialer := func(dialCtx context.Context, network, addr string) (net.Conn, error) {
		host, portStr, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		port, err := net.LookupPort(network, portStr)
		if err != nil {
			return nil, err
		}
		dest := xnet.TCPDestination(xnet.ParseAddress(host), xnet.Port(port))
		return xcore.Dial(dialCtx, instance, dest)
	}

	tr := &http.Transport{
		DialContext:           dialer,
		ResponseHeaderTimeout: t.timeout,
		DisableKeepAlives:     true,
	}
	defer tr.CloseIdleConnections()

	httpClient := &http.Client{
		Timeout:   t.timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	// Step 1: Connectivity and Latency test with multi-CDN fallback (Cloudflare, Google, Mozilla)
	endpoints := []string{t.endpoint}
	fallbackEndpoints := []string{
		"http://cp.cloudflare.com/generate_204",
		"http://www.cloudflare.com/cdn-cgi/trace",
		"http://detectportal.firefox.com/success.txt",
	}
	for _, fb := range fallbackEndpoints {
		found := false
		for _, ep := range endpoints {
			if ep == fb {
				found = true
				break
			}
		}
		if !found {
			endpoints = append(endpoints, fb)
		}
	}

	var lastErr error
	var latency int
	success := false

	for _, ep := range endpoints {
		reqCtx, reqCancel := context.WithTimeout(ctx, t.timeout)
		start := time.Now()
		req, err := http.NewRequestWithContext(reqCtx, "GET", ep, nil)
		if err != nil {
			reqCancel()
			lastErr = err
			continue
		}
		req.Header.Set("User-Agent", "Mozilla/5.0")

		resp, err := httpClient.Do(req)
		if err != nil {
			reqCancel()
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
			latency = int(time.Since(start).Milliseconds())
			if latency <= 0 {
				latency = 1
			}
			resp.Body.Close()
			reqCancel()
			success = true
			break
		}
		resp.Body.Close()
		reqCancel()
		lastErr = fmt.Errorf("HTTP %d error", resp.StatusCode)
	}

	if !success {
		return -1, "", nil, lastErr
	}

	// Step 2: Best-effort Exit IP probe through the node
	exitIP := t.probeExitIP(ctx, httpClient)
	if exitIP == "" {
		if net.ParseIP(c.Server) != nil {
			exitIP = c.Server
		}
	}

	// Step 3: IPData trust score check (best-effort, non-fatal)
	var ipDetails *IPDataDetails
	if checkTrust && exitIP != "" {
		ipdataCtx, ipdataCancel := context.WithTimeout(ctx, 3500*time.Millisecond)
		ipDetails, _ = QueryIPData(ipdataCtx, httpClient, exitIP)
		ipdataCancel()
	}

	return latency, exitIP, ipDetails, nil
}

func (t *LiveTester) probeExitIP(ctx context.Context, client *http.Client) string {
	endpoints := []string{
		"http://icanhazip.com",
		"http://checkip.amazonaws.com",
		"http://api.ipify.org",
	}

	for _, ep := range endpoints {
		probeCtx, cancel := context.WithTimeout(ctx, 3500*time.Millisecond)
		req, err := http.NewRequestWithContext(probeCtx, "GET", ep, nil)
		if err != nil {
			cancel()
			continue
		}
		req.Header.Set("User-Agent", "curl/8.4.0")

		resp, err := client.Do(req)
		if err != nil {
			cancel()
			continue
		}

		body, err := io.ReadAll(io.LimitReader(resp.Body, 1024))
		_ = resp.Body.Close()
		cancel()

		if err != nil || (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent) {
			continue
		}

		ipStr := strings.TrimSpace(string(body))
		if ip := net.ParseIP(ipStr); ip != nil {
			return ip.String()
		}
	}

	return ""
}

// TestPool tests candidate configs concurrently with worker semaphore and cancellation
func (t *LiveTester) TestPool(
	ctx context.Context,
	configs []*models.Config,
	concurrency int,
	onResult func(res TestResult),
) []TestResult {
	if concurrency <= 0 {
		concurrency = 100
	}

	var results []TestResult
	if onResult == nil {
		results = make([]TestResult, len(configs))
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, c := range configs {
		select {
		case <-ctx.Done():
			if results != nil {
				results[i] = TestResult{Config: c, Latency: -1, TrustScore: -1, Error: ctx.Err()}
			}
			continue
		default:
		}

		wg.Add(1)
		sem <- struct{}{}

		go func(idx int, item *models.Config) {
			defer wg.Done()
			defer func() { <-sem }()
			defer func() {
				if r := recover(); r != nil {
					res := TestResult{Config: item, Latency: -1, TrustScore: -1, Error: fmt.Errorf("panic in tester: %v", r)}
					if results != nil {
						results[idx] = res
					}
					if onResult != nil {
						onResult(res)
					}
				}
			}()

			latency, exitIP, ipData, err := t.testCore(ctx, item, false)
			res := TestResult{
				Config:     item,
				Latency:    latency,
				ExitIP:     exitIP,
				TrustScore: -1,
				Error:      err,
			}
			if ipData != nil {
				res.TrustScore = ipData.TrustScore
				res.RiskLevel = ipData.RiskLevel
				res.ThreatsCount = ipData.ThreatsCount
				res.Organisation = ipData.Organisation
				res.IPData = ipData
			}
			if results != nil {
				results[idx] = res
			}

			if onResult != nil {
				onResult(res)
			}
		}(i, c)
	}

	wg.Wait()
	return results
}

// QueryIPData queries ipdata.co API for full threat and trust score profile
func QueryIPData(ctx context.Context, client *http.Client, targetIP string) (*IPDataDetails, error) {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}

	targetIP = strings.TrimSpace(targetIP)
	if targetIP == "" {
		return nil, fmt.Errorf("empty target IP")
	}

	targetURL := fmt.Sprintf("https://api.ipdata.co/%s?api-key=eca677b284b3bac29eb72f5e496aa9047f26543605efe99ff2ce35c9", targetIP)
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://ipdata.co")
	req.Header.Set("Origin", "https://ipdata.co")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ipdata returned status %d: %s", resp.StatusCode, string(body))
	}

	var raw struct {
		IP            string  `json:"ip"`
		IsEU          bool    `json:"is_eu"`
		City          string  `json:"city"`
		Region        string  `json:"region"`
		CountryName   string  `json:"country_name"`
		CountryCode   string  `json:"country_code"`
		ContinentName string  `json:"continent_name"`
		ContinentCode string  `json:"continent_code"`
		Latitude      float64 `json:"latitude"`
		Longitude     float64 `json:"longitude"`
		Currency      struct {
			Code string `json:"code"`
		} `json:"currency"`
		TimeZone struct {
			Name string `json:"name"`
		} `json:"time_zone"`
		ASN struct {
			Name string `json:"name"`
		} `json:"asn"`
		Company struct {
			Name string `json:"name"`
		} `json:"company"`
		Threat struct {
			IsTor           bool          `json:"is_tor"`
			IsVPN           bool          `json:"is_vpn"`
			IsProxy         bool          `json:"is_proxy"`
			IsDatacenter    bool          `json:"is_datacenter"`
			IsAnonymous     bool          `json:"is_anonymous"`
			IsKnownAttacker bool          `json:"is_known_attacker"`
			IsKnownAbuser   bool          `json:"is_known_abuser"`
			IsThreat        bool          `json:"is_threat"`
			IsBogon         bool          `json:"is_bogon"`
			Blocklists      []interface{} `json:"blocklists"`
			Scores          struct {
				TrustScore int `json:"trust_score"`
			} `json:"scores"`
		} `json:"threat"`
	}

	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, err
	}

	threatCount := 0
	if raw.Threat.IsTor {
		threatCount++
	}
	if raw.Threat.IsVPN {
		threatCount++
	}
	if raw.Threat.IsProxy {
		threatCount++
	}
	if raw.Threat.IsDatacenter {
		threatCount++
	}
	if raw.Threat.IsAnonymous {
		threatCount++
	}
	if raw.Threat.IsKnownAttacker {
		threatCount++
	}
	if raw.Threat.IsKnownAbuser {
		threatCount++
	}
	if raw.Threat.IsThreat {
		threatCount++
	}
	threatCount += len(raw.Threat.Blocklists)

	trustScore := raw.Threat.Scores.TrustScore
	var riskLevel string
	if trustScore >= 60 {
		riskLevel = "Low risk"
	} else if trustScore >= 40 {
		riskLevel = "Moderate risk"
	} else if trustScore >= 0 {
		riskLevel = "High risk"
	} else {
		riskLevel = "Unknown"
	}

	org := raw.Company.Name
	if org == "" {
		org = raw.ASN.Name
	}

	return &IPDataDetails{
		IP:            raw.IP,
		IsEU:          raw.IsEU,
		City:          raw.City,
		Region:        raw.Region,
		CountryName:   raw.CountryName,
		CountryCode:   raw.CountryCode,
		ContinentName: raw.ContinentName,
		ContinentCode: raw.ContinentCode,
		Latitude:      raw.Latitude,
		Longitude:     raw.Longitude,
		CurrencyCode:  raw.Currency.Code,
		TimeZone:      raw.TimeZone.Name,
		Organisation:  org,
		TrustScore:    trustScore,
		RiskLevel:     riskLevel,
		ThreatsCount:  threatCount,
		IsDatacenter:  raw.Threat.IsDatacenter,
		IsVPN:         raw.Threat.IsVPN,
		IsProxy:       raw.Threat.IsProxy,
		IsTor:         raw.Threat.IsTor,
		IsThreat:      raw.Threat.IsThreat,
		IsAttacker:    raw.Threat.IsKnownAttacker,
		RawJSON:       string(body),
	}, nil
}
