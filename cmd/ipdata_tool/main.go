package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	xnet "github.com/xtls/xray-core/common/net"
	xcore "github.com/xtls/xray-core/core"
	xserial "github.com/xtls/xray-core/infra/conf/serial"
	_ "github.com/xtls/xray-core/main/distro/all"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/v2go"
)

type IPDataResponse struct {
	IP            string `json:"ip"`
	IsEU          bool   `json:"is_eu"`
	City          string `json:"city"`
	Region        string `json:"region"`
	RegionCode    string `json:"region_code"`
	CountryName   string `json:"country_name"`
	CountryCode   string `json:"country_code"`
	ContinentName string `json:"continent_name"`
	ContinentCode string `json:"continent_code"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	Postal        string  `json:"postal"`
	CallingCode   string  `json:"calling_code"`
	Flag          string  `json:"flag"`
	EmojiFlag     string  `json:"emoji_flag"`
	EmojiUnicode  string  `json:"emoji_unicode"`
	ASN           struct {
		ASN    string `json:"asn"`
		Name   string `json:"name"`
		Domain string `json:"domain"`
		Route  string `json:"route"`
		Type   string `json:"type"`
	} `json:"asn"`
	Company struct {
		Name    string `json:"name"`
		Domain  string `json:"domain"`
		Network string `json:"network"`
		Type    string `json:"type"`
	} `json:"company"`
	Carrier struct {
		Name string `json:"name"`
		MCC  string `json:"mcc"`
		MNC  string `json:"mnc"`
	} `json:"carrier"`
	Languages []struct {
		Name   string `json:"name"`
		Native string `json:"native"`
		Code   string `json:"code"`
	} `json:"languages"`
	Currency struct {
		Name   string `json:"name"`
		Code   string `json:"code"`
		Symbol string `json:"symbol"`
		Native string `json:"native"`
		Plural string `json:"plural"`
	} `json:"currency"`
	TimeZone struct {
		Name         string `json:"name"`
		Abbreviation string `json:"abbr"`
		Offset       string `json:"offset"`
		IsDST        bool   `json:"is_dst"`
		CurrentTime  string `json:"current_time"`
	} `json:"time_zone"`
	Threat struct {
		IsTOR           bool          `json:"is_tor"`
		IsVPN           bool          `json:"is_vpn"`
		IsCrawler       bool          `json:"is_crawler"`
		IsProxy         bool          `json:"is_proxy"`
		IsDatacenter    bool          `json:"is_datacenter"`
		IsAnonymous     bool          `json:"is_anonymous"`
		IsKnownAttacker bool          `json:"is_known_attacker"`
		IsKnownAbuser   bool          `json:"is_known_abuser"`
		IsThreat        bool          `json:"is_threat"`
		IsBogon         bool          `json:"is_bogon"`
		Blocklists      []interface{} `json:"blocklists"`
		Scores          struct {
			VPNScore   int `json:"vpn_score"`
			ProxyScore int `json:"proxy_score"`
			ThreatScore int `json:"threat_score"`
		} `json:"scores"`
	} `json:"threat"`
	Message string `json:"message"` // if error
}

func main() {
	appData := os.Getenv("APPDATA")
	dbPath := filepath.Join(appData, "FreeNode", "freenode.db")
	fmt.Printf("Connecting to database: %s\n", dbPath)

	db, err := database.Open(dbPath)
	if err != nil {
		fmt.Printf("Error opening db: %v\n", err)
		return
	}
	defer db.Close()

	// Check counts
	stats, err := db.GetStats()
	if err != nil {
		fmt.Printf("Error getting stats: %v\n", err)
		return
	}
	fmt.Printf("Total configs in DB: %d (Working: %d, Dead: %d, AvgLatency: %dms)\n",
		stats.TotalConfigs, stats.WorkingConfigs, stats.DeadConfigs, stats.AvgLatency)

	var workingConfigs []models.Config
	if stats.WorkingConfigs > 0 {
		workingConfigs, _, err = db.GetConfigs(database.ConfigFilter{
			Status: "working",
			Limit:  10,
		})
		if err != nil {
			fmt.Printf("Error getting working configs: %v\n", err)
		}
	}

	// If no working configs yet in DB, fetch some untested configs to test
	if len(workingConfigs) == 0 {
		fmt.Println("No 'working' configs found in DB. Fetching top untested configs to test...")
		workingConfigs, _, err = db.GetConfigs(database.ConfigFilter{
			Limit: 20,
		})
	}

	fmt.Printf("Checking quality for %d candidate configs using ipdata.co...\n\n", len(workingConfigs))

	for i, cfg := range workingConfigs {
		fmt.Printf("====================================================================\n")
		fmt.Printf("[%d/%d] Testing: %s (%s - %s:%d)\n", i+1, len(workingConfigs), cfg.Name, cfg.Protocol, cfg.Server, cfg.Port)
		
		res, rawJSON, err := checkConfigWithIPData(&cfg)
		if err != nil {
			fmt.Printf("❌ Failed: %v\n\n", err)
			continue
		}

		fmt.Printf("✅ SUCCESS! Response from ipdata.co received via proxy node:\n")
		fmt.Printf("   - Exit IP:        %s\n", res.IP)
		fmt.Printf("   - Location:       %s, %s, %s (%s)\n", res.City, res.Region, res.CountryName, res.CountryCode)
		fmt.Printf("   - Coordinates:    %.4f, %.4f\n", res.Latitude, res.Longitude)
		fmt.Printf("   - ASN:            %s (%s)\n", res.ASN.ASN, res.ASN.Name)
		fmt.Printf("   - Company:        %s (%s - %s)\n", res.Company.Name, res.Company.Domain, res.Company.Type)
		fmt.Printf("   - Threat / Risk Profile:\n")
		fmt.Printf("       * Is Proxy:        %v\n", res.Threat.IsProxy)
		fmt.Printf("       * Is Datacenter:   %v\n", res.Threat.IsDatacenter)
		fmt.Printf("       * Is VPN:          %v\n", res.Threat.IsVPN)
		fmt.Printf("       * Is Tor:          %v\n", res.Threat.IsTOR)
		fmt.Printf("       * Is Known Threat: %v\n", res.Threat.IsThreat)
		fmt.Printf("       * Is Attacker:     %v\n", res.Threat.IsKnownAttacker)
		fmt.Printf("       * Threat Score:    %d\n", res.Threat.Scores.ThreatScore)
		fmt.Printf("   - TimeZone:       %s (%s, Offset: %s)\n", res.TimeZone.Name, res.TimeZone.Abbreviation, res.TimeZone.Offset)
		fmt.Printf("   - Currency:       %s (%s)\n", res.Currency.Name, res.Currency.Code)
		fmt.Printf("\nRaw JSON excerpt:\n%s\n\n", truncateJSON(rawJSON, 400))
	}
}

func truncateJSON(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "... [truncated]"
}

func checkConfigWithIPData(c *models.Config) (*IPDataResponse, string, error) {
	outbound, err := v2go.ConvertToXrayOutbound(c)
	if err != nil {
		return nil, "", fmt.Errorf("converting outbound: %w", err)
	}

	fullConfig := v2go.M{
		"log": v2go.M{"loglevel": "none"},
		"outbounds": []v2go.M{
			outbound,
			{"protocol": "freedom", "tag": "direct"},
		},
	}

	jsonBytes, err := json.Marshal(fullConfig)
	if err != nil {
		return nil, "", fmt.Errorf("marshalling config: %w", err)
	}

	cfg, err := xserial.LoadJSONConfig(bytes.NewReader(jsonBytes))
	if err != nil {
		return nil, "", fmt.Errorf("loading xray json: %w", err)
	}

	instance, err := xcore.New(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("creating xray instance: %w", err)
	}

	if err := instance.Start(); err != nil {
		return nil, "", fmt.Errorf("starting xray instance: %w", err)
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

	client := &http.Client{
		Timeout: 7 * time.Second,
		Transport: &http.Transport{
			DialContext:           dialer,
			ResponseHeaderTimeout: 7 * time.Second,
			DisableKeepAlives:     true,
		},
	}

	// Step 1: Probe Exit IP through the node's proxy connection
	var targetIP string
	probeCtx, pCancel := context.WithTimeout(context.Background(), 5*time.Second)
	pReq, err := http.NewRequestWithContext(probeCtx, "GET", "http://api.ipify.org", nil)
	if err == nil {
		pReq.Header.Set("User-Agent", "curl/8.4.0")
		pResp, pErr := client.Do(pReq)
		if pErr == nil && pResp.StatusCode == http.StatusOK {
			b, _ := io.ReadAll(io.LimitReader(pResp.Body, 64))
			pResp.Body.Close()
			targetIP = strings.TrimSpace(string(b))
		}
	}
	pCancel()

	if targetIP == "" {
		// Fallback to server IP/domain if ipify probe timed out
		targetIP = c.Server
	}

	// Step 2: Query ipdata.co through the node for the target exit IP
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	url := fmt.Sprintf("https://api.ipdata.co/%s?api-key=eca677b284b3bac29eb72f5e496aa9047f26543605efe99ff2ce35c9", targetIP)
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Referer", "https://ipdata.co")
	req.Header.Set("Origin", "https://ipdata.co")

	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("HTTP request to ipdata failed: %w", err)
	}
	defer resp.Body.Close()

	duration := time.Since(start)

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, "", fmt.Errorf("reading body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, string(bodyBytes), fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var ipData IPDataResponse
	if err := json.Unmarshal(bodyBytes, &ipData); err != nil {
		return nil, string(bodyBytes), fmt.Errorf("parsing JSON: %w", err)
	}

	fmt.Printf("   [Latency to ipdata.co: %v]\n", duration.Round(time.Millisecond))
	return &ipData, string(bodyBytes), nil
}
