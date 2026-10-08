package cleanip

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"encoding/binary"
	"fmt"
	"math/rand"
	"net"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"freenode/pkg/database"
	"freenode/pkg/geoip"
	"freenode/pkg/models"
	"freenode/pkg/v2go"
)

// Official Akamai Edge ranges tested for ShirOKhorshid & Psiphon CDN fronting
var AkamaiCIDRs = []string{
	"23.209.210.0/24",  // Akamai Edge (Psiphon Fronting - Primary)
	"23.213.161.0/24",  // Akamai Edge (Psiphon Fronting - Primary)
	"23.207.210.0/24",  // Akamai Edge
	"184.24.77.0/24",   // Akamai Edge
	"92.123.102.0/24",  // Akamai Edge Europe
	"2.16.10.0/24",     // Akamai Edge Europe
	"2.22.250.0/24",    // Akamai Edge Global
	"23.58.193.0/24",   // Akamai Edge Global
	"23.48.23.0/24",    // Akamai Edge Global
	"23.43.237.0/24",   // Akamai Edge Global
	"104.112.146.0/24", // Akamai Edge
	"72.246.28.0/24",   // Akamai Edge
	"185.200.232.0/24", // Akamai Edge
	"23.202.138.0/24",  // Akamai Edge
	"72.18.63.0/24",    // Akamai Edge
	"92.16.53.0/24",    // Akamai Edge
	"92.16.19.0/24",    // Akamai Edge
	"185.143.232.0/24", // Akamai Edge
	"2.19.126.0/24",    // Akamai Edge
	"23.2.13.0/24",     // Akamai Edge
}

// Amazon CloudFront ranges for CDN Fronting
var CloudFrontCIDRs = []string{
	"13.32.0.0/16",
	"13.224.0.0/16",
	"13.249.0.0/16",
	"54.192.0.0/16",
	"54.230.0.0/16",
	"52.84.0.0/16",
	"99.84.0.0/16",
	"99.86.0.0/16",
	"143.204.0.0/16",
}

// Fastly edge ranges
var FastlyCIDRs = []string{
	"151.101.65.0/24",
	"151.101.1.0/24",
	"151.101.129.0/24",
	"151.101.193.0/24",
	"199.232.0.0/16",
}

// ShirOKhorshidCuratedIPs are proven pre-tested IPs for ShirOKhorshid / Psiphon CDN fronting
var ShirOKhorshidCuratedIPs = []string{
	"23.209.210.213",
	"23.213.161.22",
	"23.207.210.81",
	"184.24.77.42",
	"92.123.102.43",
	"2.22.250.149",
	"23.58.193.140",
	"23.48.23.151",
	"23.48.23.186",
	"23.48.23.133",
	"104.112.146.82",
	"72.246.28.3",
	"185.200.232.49",
	"185.200.232.50",
	"185.200.232.42",
	"23.202.138.125",
	"2.19.126.81",
}

// Official CDN IPv4 CIDR blocks commonly used for circumvention fronting (Cloudflare, Akamai, Fastly)
var IranCloudflareCIDRs = []string{
	"162.159.192.0/24", // Primary Cloudflare WARP Anycast edge pool
	"162.159.193.0/24", // Secondary Cloudflare WARP Anycast edge pool
	"162.159.195.0/24", // Alternative WARP Anycast block
	"188.114.96.0/24",  // European WARP Anycast edge (ultra reliable across Iranian ISPs)
	"188.114.97.0/24",  // European WARP Anycast edge
	"188.114.98.0/24",  // European WARP Anycast edge
	"188.114.99.0/24",  // European WARP Anycast edge
	"23.209.210.0/24",  // Akamai Edge (Psiphon Fronting)
	"23.213.161.0/24",  // Akamai Edge (Psiphon Fronting)
	"23.207.210.0/24",  // Akamai Edge
	"184.24.77.0/24",   // Akamai Edge
	"92.123.102.0/24",  // Akamai Edge Europe
	"2.16.10.0/24",     // Akamai Edge Europe
	"151.101.65.0/24",  // Fastly PyPI / GitHub fronting edge
	"151.101.1.0/24",   // Fastly Global
}

// All official CDN IPv4 CIDR blocks (matching SenPaiScanner / Se7en Pro pool)
var AllCloudflareCIDRs = []string{
	"173.245.48.0/20",
	"103.21.244.0/22",
	"103.22.200.0/22",
	"103.31.4.0/22",
	"141.101.64.0/18",
	"108.162.192.0/18",
	"190.93.240.0/20",
	"188.114.96.0/20",
	"197.234.240.0/22",
	"198.41.128.0/17",
	"162.158.0.0/15",
	"104.16.0.0/13",
	"104.24.0.0/14",
	"172.64.0.0/13",
	"131.0.72.0/22",
	"162.159.192.0/24",
	"162.159.193.0/24",
	"162.159.195.0/24",
	"23.209.210.0/24",
	"23.213.161.0/24",
	"23.207.210.0/24",
	"184.24.77.0/24",
	"92.123.102.0/24",
	"2.16.10.0/24",
	"151.101.0.0/16",
	"199.232.0.0/16",
}

// Curated high-reputation Cloudflare WARP Anycast IPs in Iran
var CuratedEdgeIPs = []string{
	"188.114.96.1",
	"188.114.97.1",
	"188.114.98.1",
	"188.114.99.1",
	"188.114.96.2",
	"188.114.97.2",
	"188.114.98.2",
	"188.114.99.2",
	"162.159.192.1",
	"162.159.193.1",
	"162.159.195.1",
	"162.159.192.2",
	"162.159.193.2",
	"162.159.195.2",
	"162.159.192.5",
	"162.159.193.5",
	"162.159.195.5",
	"162.159.192.7",
	"162.159.193.7",
	"162.159.192.10",
	"162.159.193.10",
	"162.159.195.10",
}

// IPResult represents a tested CDN clean IP
type IPResult struct {
	IP          string    `json:"ip"`
	Latency     int       `json:"latency"` // ms (-1 if unreachable)
	Subnet      string    `json:"subnet"`
	CDN         string    `json:"cdn"`     // "Akamai", "CloudFront", "Cloudflare", "Fastly"
	SNI         string    `json:"sni"`     // e.g. "a248.e.akamai.net", "d1.cloudfront.net"
	Country     string    `json:"country,omitempty"`
	CountryName string    `json:"country_name,omitempty"`
	CheckedAt   time.Time `json:"checked_at"`
}

// ScanOptions configures the Clean IP scan run
type ScanOptions struct {
	Workers     int      `json:"workers"`      // default: 100
	TimeoutMs   int      `json:"timeout_ms"`   // default: 1500
	SampleSize  int      `json:"sample_size"`  // default: 300
	CustomCIDRs []string `json:"custom_cidrs"` // optional custom CIDRs
	Port        int      `json:"port"`         // default: 443
	CDNTarget   string   `json:"cdn_target"`   // "akamai" (default), "cloudfront", "all", "cloudflare"
}


// Progress tracks the live state of a Clean IP scan
type Progress struct {
	State       string     `json:"state"` // "idle", "scanning", "completed", "cancelled"
	TotalIPs    int        `json:"total_ips"`
	TestedIPs   int        `json:"tested_ips"`
	WorkingIPs  int        `json:"working_ips"`
	ProgressPct int        `json:"progress_pct"` // 0 - 100
	BestLatency int        `json:"best_latency"` // lowest latency in ms
	CurrentIP   string     `json:"current_ip,omitempty"`
	BestIPs     []IPResult `json:"best_ips"`
	Message     string     `json:"message"`
}

// Manager controls clean IP scanning sessions
type Manager struct {
	mu         sync.RWMutex
	progress   Progress
	cancelFunc context.CancelFunc
	bestIPs    []IPResult
	isScanning bool
	geo        *geoip.Resolver
}

// NewManager initializes a new clean IP manager
func NewManager() *Manager {
	return &Manager{
		progress: Progress{
			State:   "idle",
			Message: "Ready to scan Cloudflare Clean IPs",
		},
		geo: GetDefaultGeoResolver(),
	}
}

var (
	defaultGeoResolver   *geoip.Resolver
	defaultGeoResolverMu sync.RWMutex
)

// SetDefaultGeoResolver sets the package-level GeoIP resolver
func SetDefaultGeoResolver(geo *geoip.Resolver) {
	defaultGeoResolverMu.Lock()
	defer defaultGeoResolverMu.Unlock()
	defaultGeoResolver = geo
}

// GetDefaultGeoResolver retrieves the package-level GeoIP resolver
func GetDefaultGeoResolver() *geoip.Resolver {
	defaultGeoResolverMu.RLock()
	defer defaultGeoResolverMu.RUnlock()
	return defaultGeoResolver
}

// SetGeoResolver sets the GeoIP resolver for clean IP candidate country detection
func (m *Manager) SetGeoResolver(geo *geoip.Resolver) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.geo = geo
	SetDefaultGeoResolver(geo)
}

// DetectCDN identifies the CDN provider, the required SNI, and the Psiphon CDN set name for an IP
func DetectCDN(ip string) (cdnName, sni, cdnSet string) {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
	}

	parsed := net.ParseIP(ip)
	if parsed == nil {
		return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
	}

	// 1. Check ShirOKhorshid / Akamai curated IPs
	for _, cur := range ShirOKhorshidCuratedIPs {
		if ip == cur {
			return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
		}
	}

	// 2. Check Akamai CIDRs
	for _, cidr := range AkamaiCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(parsed) {
			return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
		}
	}

	// 3. Check CloudFront CIDRs
	for _, cidr := range CloudFrontCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(parsed) {
			return "CloudFront", "d1.cloudfront.net", "cloudfront"
		}
	}

	// 4. Check Fastly CIDRs
	for _, cidr := range FastlyCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(parsed) {
			return "Fastly", "", "fastly"
		}
	}

	// 5. Check Cloudflare curated & CIDRs
	for _, cur := range CuratedEdgeIPs {
		if ip == cur {
			return "Cloudflare", "cloudflare.com", "cloudflare"
		}
	}
	for _, cidr := range IranCloudflareCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(parsed) {
			return "Cloudflare", "cloudflare.com", "cloudflare"
		}
	}
	for _, cidr := range AllCloudflareCIDRs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err == nil && ipnet.Contains(parsed) {
			return "Cloudflare", "cloudflare.com", "cloudflare"
		}
	}

	// Heuristic / prefix checks
	if strings.HasPrefix(ip, "23.") || strings.HasPrefix(ip, "184.24.") || strings.HasPrefix(ip, "92.123.") || strings.HasPrefix(ip, "2.16.") || strings.HasPrefix(ip, "2.22.") || strings.HasPrefix(ip, "104.112.") || strings.HasPrefix(ip, "72.246.") || strings.HasPrefix(ip, "185.200.") || strings.HasPrefix(ip, "185.143.") || strings.HasPrefix(ip, "2.19.") || strings.HasPrefix(ip, "72.18.") {
		return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
	}
	if strings.HasPrefix(ip, "13.") || strings.HasPrefix(ip, "54.") || strings.HasPrefix(ip, "52.") || strings.HasPrefix(ip, "99.") || strings.HasPrefix(ip, "143.204.") {
		return "CloudFront", "d1.cloudfront.net", "cloudfront"
	}
	if strings.HasPrefix(ip, "162.159.") || strings.HasPrefix(ip, "188.114.") || strings.HasPrefix(ip, "104.16.") || strings.HasPrefix(ip, "104.24.") || strings.HasPrefix(ip, "172.64.") || strings.HasPrefix(ip, "198.41.") {
		return "Cloudflare", "cloudflare.com", "cloudflare"
	}

	// Default fallback to Akamai (ShirOKhorshid default)
	return "Akamai", "a248.e.akamai.net", "psiphon-akamai"
}

// StartScan begins a concurrent scan of CDN IPs
func (m *Manager) StartScan(opts ScanOptions) error {
	m.mu.Lock()
	if m.isScanning {
		m.mu.Unlock()
		return fmt.Errorf("a scan is already in progress")
	}

	if opts.Workers <= 0 {
		opts.Workers = 100
	}
	if opts.TimeoutMs <= 0 {
		opts.TimeoutMs = 1500
	}
	if opts.SampleSize <= 0 {
		opts.SampleSize = 300
	}
	if opts.Port <= 0 {
		opts.Port = 443
	}

	target := opts.CDNTarget
	if target == "" {
		target = "akamai"
	}

	candidates := GenerateCandidateIPsForTarget(target, opts.CustomCIDRs, opts.SampleSize)
	if len(candidates) == 0 {
		m.mu.Unlock()
		return fmt.Errorf("no valid IP candidates generated from CIDRs")
	}

	targetLabel := "Akamai / ShirOKhorshid"
	switch strings.ToLower(target) {
	case "cloudfront":
		targetLabel = "Amazon CloudFront"
	case "cloudflare":
		targetLabel = "Cloudflare"
	case "all":
		targetLabel = "All CDNs"
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.cancelFunc = cancel
	m.isScanning = true
	m.bestIPs = nil
	m.progress = Progress{
		State:       "scanning",
		TotalIPs:    len(candidates),
		TestedIPs:   0,
		WorkingIPs:  0,
		ProgressPct: 0,
		BestLatency: 0,
		Message:     fmt.Sprintf("Testing %d %s edge IPs with %d workers...", len(candidates), targetLabel, opts.Workers),
	}
	m.mu.Unlock()

	go m.runScan(ctx, candidates, opts)
	return nil
}

// CancelScan cancels an ongoing scan
func (m *Manager) CancelScan() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.isScanning && m.cancelFunc != nil {
		m.cancelFunc()
		m.isScanning = false
		m.progress.State = "cancelled"
		m.progress.Message = "Scan cancelled by user"
	}
}

// GetProgress returns the current progress of the scan
func (m *Manager) GetProgress() Progress {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p := m.progress
	limit := len(m.bestIPs)
	p.BestIPs = make([]IPResult, limit)
	copy(p.BestIPs, m.bestIPs[:limit])
	return p
}

// GetBestIPs returns the top fastest IPs found
func (m *Manager) GetBestIPs(limit int) []IPResult {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 || limit > len(m.bestIPs) {
		limit = len(m.bestIPs)
	}
	result := make([]IPResult, limit)
	copy(result, m.bestIPs[:limit])
	return result
}

// runScan executes concurrent testing across workers
func (m *Manager) runScan(ctx context.Context, candidates []candidateInfo, opts ScanOptions) {
	defer func() {
		m.mu.Lock()
		m.isScanning = false
		if m.progress.State == "scanning" {
			m.progress.State = "completed"
			m.progress.ProgressPct = 100
			m.progress.Message = fmt.Sprintf("Completed. Found %d working Clean IPs (fastest: %d ms)",
				m.progress.WorkingIPs, m.progress.BestLatency)
		}
		m.mu.Unlock()
	}()

	total := len(candidates)
	taskChan := make(chan candidateInfo, total)
	for _, c := range candidates {
		taskChan <- c
	}
	close(taskChan)

	var testedCount int32
	var workingCount int32
	var resultsMu sync.Mutex
	timeout := time.Duration(opts.TimeoutMs) * time.Millisecond

	workerCount := opts.Workers
	if workerCount > total {
		workerCount = total
	}

	var wg sync.WaitGroup
	wg.Add(workerCount)

	for w := 0; w < workerCount; w++ {
		go func() {
			defer wg.Done()
			for {
				select {
				case <-ctx.Done():
					return
				case c, ok := <-taskChan:
					if !ok {
						return
					}

					lat, err := TestIPWithSNI(ctx, c.IP, opts.Port, c.SNI, timeout)
					tested := int(atomic.AddInt32(&testedCount, 1))

					if err == nil && lat > 0 {
						atomic.AddInt32(&workingCount, 1)

						countryCode := "US"
						countryName := c.CDN + " Edge"
						if c.CDN == "Cloudflare" {
							countryCode = "CF"
							countryName = "Cloudflare Anycast"
						}
						m.mu.RLock()
						geo := m.geo
						m.mu.RUnlock()
						if geo == nil {
							geo = GetDefaultGeoResolver()
						}
						if geo != nil {
							g := geo.Lookup(c.IP)
							if g.Code != "" && g.Code != "UN" {
								countryCode = g.Code
								countryName = g.Name
							}
						}

						res := IPResult{
							IP:          c.IP,
							Latency:     lat,
							Subnet:      c.Subnet,
							CDN:         c.CDN,
							SNI:         c.SNI,
							Country:     countryCode,
							CountryName: countryName,
							CheckedAt:   time.Now(),
						}

						resultsMu.Lock()
						m.mu.Lock()
						m.bestIPs = append(m.bestIPs, res)
						sort.Slice(m.bestIPs, func(i, j int) bool {
							return m.bestIPs[i].Latency < m.bestIPs[j].Latency
						})
						if len(m.bestIPs) > 0 {
							m.progress.BestLatency = m.bestIPs[0].Latency
						}
						m.mu.Unlock()
						resultsMu.Unlock()
					}

					// Periodically update progress snapshot
					if tested%5 == 0 || tested == total {
						m.mu.Lock()
						if m.progress.State == "scanning" {
							m.progress.TestedIPs = tested
							m.progress.WorkingIPs = int(atomic.LoadInt32(&workingCount))
							m.progress.CurrentIP = c.IP
							if total > 0 {
								m.progress.ProgressPct = int(float64(tested) / float64(total) * 100)
							}
						}
						m.mu.Unlock()
					}
				}
			}
		}()
	}

	wg.Wait()
}

// candidateInfo couples an IP with its parent CIDR subnet and CDN settings
type candidateInfo struct {
	IP     string
	Subnet string
	CDN    string
	SNI    string
	CDNSet string
}

func sampleCIDRs(cidrs []string, count int, seen map[string]bool, cdnName, sni, cdnSet, subnetPrefix string, rng *rand.Rand) []candidateInfo {
	var candidates []candidateInfo
	if len(cidrs) == 0 || count <= 0 {
		return candidates
	}
	samplesPerCIDR := count / len(cidrs)
	if samplesPerCIDR < 2 {
		samplesPerCIDR = 2
	}

	for _, cidr := range cidrs {
		_, ipnet, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		baseIP := ipnet.IP.To4()
		if baseIP == nil {
			continue
		}
		baseUint := binary.BigEndian.Uint32(baseIP)
		maskOnes, _ := ipnet.Mask.Size()
		hostCount := uint32(1) << (32 - maskOnes)
		if hostCount <= 2 {
			continue
		}

		step := hostCount / uint32(samplesPerCIDR)
		if step == 0 {
			step = 1
		}

		for i := 0; i < samplesPerCIDR; i++ {
			jitter := uint32(0)
			if step > 2 {
				jitter = uint32(rng.Intn(int(step - 1)))
			}
			offset := (uint32(i) * step) + jitter + 1
			if offset >= hostCount-1 {
				offset = hostCount - 2
			}

			ipUint := baseUint + offset
			ipBytes := make(net.IP, 4)
			binary.BigEndian.PutUint32(ipBytes, ipUint)
			ipStr := ipBytes.String()

			if !seen[ipStr] {
				seen[ipStr] = true
				label := subnetPrefix
				if label == "" {
					label = getSubnetLabel(cidr)
				}
				candidates = append(candidates, candidateInfo{
					IP:     ipStr,
					Subnet: label,
					CDN:    cdnName,
					SNI:    sni,
					CDNSet: cdnSet,
				})
			}
		}
	}
	return candidates
}

// GenerateCandidateIPsForTarget generates candidate IPs tailored to the specified CDN target
func GenerateCandidateIPsForTarget(target string, customCIDRs []string, sampleSize int) []candidateInfo {
	if sampleSize <= 0 {
		sampleSize = 300
	}
	target = strings.ToLower(strings.TrimSpace(target))
	if target == "" {
		target = "akamai"
	}

	var candidates []candidateInfo
	seen := make(map[string]bool)
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	if len(customCIDRs) > 0 {
		for _, cidr := range customCIDRs {
			cName, cSni, cSet := DetectCDN(strings.Split(cidr, "/")[0])
			sampled := sampleCIDRs([]string{cidr}, sampleSize, seen, cName, cSni, cSet, cName+" Edge", rng)
			candidates = append(candidates, sampled...)
		}
		if len(candidates) > sampleSize {
			candidates = candidates[:sampleSize]
		}
		return candidates
	}

	switch target {
	case "akamai":
		// 1. Proven curated IPs from ShirOKhorshid
		for _, ip := range ShirOKhorshidCuratedIPs {
			if !seen[ip] {
				seen[ip] = true
				candidates = append(candidates, candidateInfo{
					IP:     ip,
					Subnet: "ShirOKhorshid / Akamai Edge",
					CDN:    "Akamai",
					SNI:    "a248.e.akamai.net",
					CDNSet: "psiphon-akamai",
				})
			}
		}
		// 2. Sample from Akamai CIDRs
		rem := sampleSize - len(candidates)
		if rem > 0 {
			sampled := sampleCIDRs(AkamaiCIDRs, rem, seen, "Akamai", "a248.e.akamai.net", "psiphon-akamai", "Akamai Edge", rng)
			candidates = append(candidates, sampled...)
		}

	case "cloudfront":
		sampled := sampleCIDRs(CloudFrontCIDRs, sampleSize, seen, "CloudFront", "d1.cloudfront.net", "cloudfront", "Amazon CloudFront Edge", rng)
		candidates = append(candidates, sampled...)

	case "cloudflare":
		// 1. Curated Cloudflare edge IPs
		for _, ip := range CuratedEdgeIPs {
			if !seen[ip] {
				seen[ip] = true
				candidates = append(candidates, candidateInfo{
					IP:     ip,
					Subnet: "Europe / Anycast Edge",
					CDN:    "Cloudflare",
					SNI:    "cloudflare.com",
					CDNSet: "cloudflare",
				})
			}
		}
		// 2. Sample from Cloudflare CIDRs
		cidrs := IranCloudflareCIDRs
		if sampleSize > 500 {
			cidrs = AllCloudflareCIDRs
		}
		rem := sampleSize - len(candidates)
		if rem > 0 {
			sampled := sampleCIDRs(cidrs, rem, seen, "Cloudflare", "cloudflare.com", "cloudflare", "Cloudflare Anycast", rng)
			candidates = append(candidates, sampled...)
		}

	case "all":
		// Mix Akamai (50%), CloudFront (25%), Cloudflare (25%)
		for _, ip := range ShirOKhorshidCuratedIPs {
			if !seen[ip] {
				seen[ip] = true
				candidates = append(candidates, candidateInfo{
					IP:     ip,
					Subnet: "ShirOKhorshid / Akamai Edge",
					CDN:    "Akamai",
					SNI:    "a248.e.akamai.net",
					CDNSet: "psiphon-akamai",
				})
			}
		}
		akamaiCount := sampleSize / 2
		cfCount := sampleSize / 4
		cfrontCount := sampleSize - akamaiCount - cfCount
		candidates = append(candidates, sampleCIDRs(AkamaiCIDRs, akamaiCount, seen, "Akamai", "a248.e.akamai.net", "psiphon-akamai", "Akamai Edge", rng)...)
		candidates = append(candidates, sampleCIDRs(CloudFrontCIDRs, cfrontCount, seen, "CloudFront", "d1.cloudfront.net", "cloudfront", "Amazon CloudFront Edge", rng)...)
		candidates = append(candidates, sampleCIDRs(IranCloudflareCIDRs, cfCount, seen, "Cloudflare", "cloudflare.com", "cloudflare", "Cloudflare Anycast", rng)...)
	}

	// Shuffle candidate order so workers test across diverse CIDRs simultaneously
	rng.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	if len(candidates) > sampleSize {
		candidates = candidates[:sampleSize]
	}

	return candidates
}

// GenerateCandidateIPs creates a distributed, balanced pool of IPs from CIDRs
func GenerateCandidateIPs(cidrs []string, sampleSize int) []candidateInfo {
	if len(cidrs) > 0 {
		return GenerateCandidateIPsForTarget("custom", cidrs, sampleSize)
	}
	return GenerateCandidateIPsForTarget("akamai", nil, sampleSize)
}

// TestIPWithSNI performs a TCP connect followed by a TLS handshake with the specified SNI.
func TestIPWithSNI(ctx context.Context, ip string, port int, sni string, timeout time.Duration) (int, error) {
	if port <= 0 {
		port = 443
	}
	if sni == "" {
		sni = "a248.e.akamai.net"
	}

	addr := net.JoinHostPort(ip, strconv.Itoa(port))
	dialer := &net.Dialer{
		Timeout: timeout,
	}

	start := time.Now()

	// 1. TCP Connection
	rawConn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return -1, err
	}
	defer rawConn.Close()

	// 2. TLS Handshake with designated CDN SNI
	tlsConfig := &tls.Config{
		ServerName:         sni,
		InsecureSkipVerify: true,
	}

	tlsConn := tls.Client(rawConn, tlsConfig)
	_ = tlsConn.SetDeadline(time.Now().Add(timeout))

	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return -1, err
	}

	latency := int(time.Since(start).Milliseconds())
	if latency <= 0 {
		latency = 1
	}

	return latency, nil
}

// TestIP tests an IP using its auto-detected CDN SNI.
func TestIP(ctx context.Context, ip string, port int, timeout time.Duration) (int, error) {
	_, sni, _ := DetectCDN(ip)
	return TestIPWithSNI(ctx, ip, port, sni, timeout)
}

// ApplyCleanIP updates the node's server address to the clean IP while preserving
// the SNI and Host header. It also updates the RawLink and recomputes the node's identity.
func ApplyCleanIP(config *models.Config, cleanIP string) error {
	cleanIP = strings.TrimSpace(cleanIP)
	parsedIP := net.ParseIP(cleanIP)
	if parsedIP == nil || parsedIP.To4() == nil {
		return fmt.Errorf("invalid clean IPv4 address: %q", cleanIP)
	}
	cleanIP = parsedIP.String()

	origServer := strings.TrimSpace(config.Server)
	origIsIP := net.ParseIP(origServer) != nil

	// Preserve SNI: If SNI is not set, set it to the original domain name
	if config.SNI == "" {
		if !origIsIP && origServer != "" {
			config.SNI = origServer
		} else if config.Host != "" && net.ParseIP(config.Host) == nil {
			config.SNI = config.Host
		}
	}

	// Preserve Host: If Host header is not set, preserve the SNI or original server domain
	if config.Host == "" {
		if config.SNI != "" {
			config.Host = config.SNI
		} else if !origIsIP && origServer != "" {
			config.Host = origServer
		}
	}

	// Front node with the new clean IP
	config.Server = cleanIP

	// If using standard Cloudflare HTTPS ports, ensure TLS is set
	if (config.TLS == "" || config.TLS == "none") && isCloudflareTLSPort(config.Port) {
		config.TLS = "tls"
	}

	// Recompute normalized identity hash
	config.Identity = v2go.GenerateIdentity(config)

	// Update RawLink for subscription export and sharing
	updateRawLinkWithCleanIP(config, cleanIP)

	return nil
}

// isCloudflareTLSPort checks if a port is one of Cloudflare's supported HTTPS ports
func isCloudflareTLSPort(port int) bool {
	switch port {
	case 443, 8443, 2053, 2083, 2087, 2096:
		return true
	default:
		return false
	}
}

// IsCDNCompatible determines if a configuration can be fronted via Cloudflare Clean IP
func IsCDNCompatible(c *models.Config) bool {
	if c == nil {
		return false
	}

	// 1. WebSockets, gRPC, xhttp, and HTTP transports are classic CDN frontable
	transport := strings.ToLower(c.Transport)
	if transport == "ws" || transport == "grpc" || transport == "xhttp" || transport == "splithttp" || transport == "http" {
		return true
	}

	// 2. Explicit SNI or Host header configured
	if c.SNI != "" || c.Host != "" {
		return true
	}

	// 3. Cloudflare standard proxy ports (HTTPS or HTTP)
	switch c.Port {
	case 443, 8443, 2053, 2083, 2087, 2096, 80, 8080, 8880, 2052, 2082, 2086, 2095:
		return true
	}

	// 4. Server address is a domain name (not a raw IP)
	if net.ParseIP(c.Server) == nil && strings.Contains(c.Server, ".") {
		return true
	}

	return false
}

// updateRawLinkWithCleanIP safely updates the server address, host and SNI in raw links
func updateRawLinkWithCleanIP(c *models.Config, cleanIP string) {
	if c.RawLink == "" {
		return
	}

	// VMess base64 JSON payload
	if strings.HasPrefix(c.RawLink, "vmess://") {
		raw := strings.TrimPrefix(c.RawLink, "vmess://")
		if decoded, err := base64.StdEncoding.DecodeString(raw); err == nil {
			var m map[string]any
			if err := json.Unmarshal(decoded, &m); err == nil {
				m["add"] = cleanIP
				if c.SNI != "" {
					m["sni"] = c.SNI
				}
				if c.Host != "" {
					m["host"] = c.Host
				}
				if b, err := json.Marshal(m); err == nil {
					c.RawLink = "vmess://" + base64.StdEncoding.EncodeToString(b)
				}
			}
		}
		return
	}

	// URL format (vless://, trojan://, hysteria2://, tuic://, etc.)
	if strings.Contains(c.RawLink, "://") {
		if u, err := url.Parse(c.RawLink); err == nil && u.Scheme != "" {
			u.Host = net.JoinHostPort(cleanIP, strconv.Itoa(c.Port))
			q := u.Query()
			if c.SNI != "" {
				q.Set("sni", c.SNI)
			}
			if c.Host != "" {
				q.Set("host", c.Host)
			}
			u.RawQuery = q.Encode()
			c.RawLink = u.String()
		}
	}
}

func getSubnetLabel(cidr string) string {
	switch {
	case strings.HasPrefix(cidr, "188.114."):
		return "Europe / ME Edge"
	case strings.HasPrefix(cidr, "104.16.") || strings.HasPrefix(cidr, "104.24."):
		return "Global Anycast"
	case strings.HasPrefix(cidr, "172.64."):
		return "North America / Global"
	case strings.HasPrefix(cidr, "162.158.") || strings.HasPrefix(cidr, "162.159."):
		return "High Availability Edge"
	case strings.HasPrefix(cidr, "103.21.") || strings.HasPrefix(cidr, "103.22.") || strings.HasPrefix(cidr, "103.31."):
		return "Asia-Pacific Edge"
	case strings.HasPrefix(cidr, "141.101."):
		return "Europe Edge"
	default:
		return cidr
	}
}

// CreateOrUpdateCleanIPNode creates or updates a node connecting via Clean IP CDN fronting
func CreateOrUpdateCleanIPNode(db *database.DB, cleanIP string, latency int) (*models.Config, error) {
	return CreateOrUpdateCleanIPNodeWithName(db, cleanIP, latency, "")
}

// CreateOrUpdateCleanIPNodeWithName creates or updates a dedicated node using Clean IP CDN fronting (matching Se7en-Pro & Shir o Khorshid)
func CreateOrUpdateCleanIPNodeWithName(db *database.DB, cleanIP string, latency int, customName string) (*models.Config, error) {
	cleanIP = strings.TrimSpace(cleanIP)
	parsedIP := net.ParseIP(cleanIP)
	if parsedIP == nil || parsedIP.To4() == nil {
		cleanIP = "23.209.210.213"
	} else {
		cleanIP = parsedIP.String()
	}

	if latency <= 0 {
		latency = 120
	}

	cdnName, sni, _ := DetectCDN(cleanIP)

	nodeName := customName
	if nodeName == "" {
		nodeName = fmt.Sprintf("⚡ %s CDN | %s", cdnName, cleanIP)
	}

	countryCode := "CF"
	countryName := "Cloudflare Anycast"
	if cdnName == "CloudFront" {
		countryCode = "US"
		countryName = "Amazon CloudFront"
	}
	if geo := GetDefaultGeoResolver(); geo != nil {
		g := geo.Lookup(cleanIP)
		if g.Code != "" && g.Code != "UN" {
			countryCode = g.Code
			countryName = g.Name
		}
	}

	node := &models.Config{
		Identity:    fmt.Sprintf("cleanip-%s", cleanIP),
		Name:        nodeName,
		Protocol:    "psiphon",
		Server:      cleanIP,
		Port:        443,
		SNI:         sni,
		Transport:   "fronted-meek",
		TLS:         "tls",
		Country:     countryCode,
		CountryName: countryName,
		Source:      fmt.Sprintf("%s Fronting", cdnName),
		Latency:     latency,
		Status:      "working",
		Score:       1000,
		IsFavorite:  false,
		Tags:        fmt.Sprintf("CDN IP,%s,clean-ip,psiphon,cdn-fronting", strings.ToLower(cdnName)),
		FirstSeen:   time.Now(),
		LastSeen:    time.Now(),
		LastTested:  time.Now(),
		RawLink:     fmt.Sprintf("psiphon://%s:443?fronting=cdn&sni=%s#%s-CDN-%s", cleanIP, sni, cdnName, cleanIP),
	}

	if db != nil {
		if _, err := db.UpsertConfig(node); err != nil {
			return nil, fmt.Errorf("saving clean ip node: %w", err)
		}
		stored, err := db.GetConfigByIdentity(node.Identity)
		if err == nil && stored != nil {
			return stored, nil
		}
	}

	return node, nil
}

