package models

import "time"

// Source represents an upstream subscription or config source
type Source struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	URL          string    `json:"url"`
	Enabled      bool      `json:"enabled"`
	Format       string    `json:"format"` // "base64", "text", "auto"
	LastUpdate   time.Time `json:"last_update"`
	ConfigsFound int       `json:"configs_found"`
	WorkingNodes int       `json:"working_nodes"`
	Error        string    `json:"error"`
}

// Config represents a parsed proxy node
type Config struct {
	ID          int64     `json:"id"`
	Identity    string    `json:"identity"` // Normalized identity hash for deduplication
	Protocol    string    `json:"protocol"` // "vless", "vmess", "trojan", "ss", "hysteria2", etc.
	Name        string    `json:"name"`
	Server      string    `json:"server"`
	Port        int       `json:"port"`
	UUID        string    `json:"uuid,omitempty"`
	Password    string    `json:"password,omitempty"`
	Transport   string    `json:"transport"` // "tcp", "ws", "grpc", "xhttp", "http", "quic"
	TLS         string    `json:"tls"`       // "none", "tls", "reality"
	SNI         string    `json:"sni,omitempty"`
	Path        string    `json:"path,omitempty"`
	Host        string    `json:"host,omitempty"`
	Reality     string    `json:"reality,omitempty"` // Public key or short ID
	Country     string    `json:"country"`           // 2-letter ISO code e.g. "DE", "US"
	CountryName string    `json:"country_name"`      // e.g. "Germany", "United States"
	Source      string    `json:"source"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
	LastTested  time.Time `json:"last_tested"`
	Latency     int       `json:"latency"` // ms (-1 if dead/failed)
	Status      string    `json:"status"`  // "working", "dead", "untested"
	Score        int       `json:"score"`   // 0 - 100
	TrustScore   int       `json:"trust_score"`   // ipdata Trust Score: 0-100 (-1 if unrated)
	RiskLevel    string    `json:"risk_level"`    // "Low risk", "Moderate risk", "High risk"
	ThreatsCount int       `json:"threats_count"` // Number of threat flags
	Organisation string    `json:"organisation"`  // e.g. "M247 Europe SRL", "GTHost"
	ExitIP       string    `json:"exit_ip"`       // Probed outbound public IP
	RawLink      string    `json:"raw_link"`
	IsFavorite   bool      `json:"is_favorite"`   // User favorite flag
	Tags         string    `json:"tags"`          // User defined tags, comma separated
}

// TestResult records a single test attempt
type TestResult struct {
	ID           int64     `json:"id"`
	ConfigID     int64     `json:"config_id"`
	TestedAt     time.Time `json:"tested_at"`
	Latency      int       `json:"latency"`
	Status       string    `json:"status"` // "working", "dead", "timeout"
	Error        string    `json:"error,omitempty"`
	ExitIP       string    `json:"exit_ip,omitempty"`
	TrustScore   int       `json:"trust_score"`
	RiskLevel    string    `json:"risk_level,omitempty"`
	ThreatsCount int       `json:"threats_count"`
	Organisation string    `json:"organisation,omitempty"`
}

// ScanSession records metadata for a full v2go scan run
type ScanSession struct {
	ID           int64     `json:"id"`
	StartedAt    time.Time `json:"started_at"`
	FinishedAt   time.Time `json:"finished_at"`
	Status       string    `json:"status"` // "running", "completed", "cancelled", "failed"
	FetchedCount int       `json:"fetched_count"`
	ParsedCount  int       `json:"parsed_count"`
	DedupCount   int       `json:"dedup_count"`
	TestedCount  int       `json:"tested_count"`
	WorkingCount int       `json:"working_count"`
}

// ConnectionHistory records past connections
type ConnectionHistory struct {
	ID             int64      `json:"id"`
	ConfigID       int64      `json:"config_id"`
	Server         string     `json:"server"`
	Protocol       string     `json:"protocol"`
	ConnectedAt    time.Time  `json:"connected_at"`
	DisconnectedAt *time.Time `json:"disconnected_at,omitempty"`
	Status         string     `json:"status"`
}

// ScanProgress is the real-time progress payload emitted to the UI
type ScanProgress struct {
	SessionID      int64  `json:"session_id"`
	State          string `json:"state"` // "idle", "fetching", "parsing", "deduping", "testing", "completed", "cancelled"
	SourcesTotal   int    `json:"sources_total"`
	SourcesDone    int    `json:"sources_done"`
	CurrentSource  string `json:"current_source"`
	FetchedTotal   int    `json:"fetched_total"`
	ParsedTotal    int    `json:"parsed_total"`
	Duplicates     int    `json:"duplicates"`
	TestingTotal   int    `json:"testing_total"`
	TestingDone    int    `json:"testing_done"`
	WorkingCount   int    `json:"working_count"`
	AverageLatency int    `json:"average_latency"`
	Message        string `json:"message"`
}

// ConnectionStatus represents the live client connection state
type ConnectionStatus struct {
	Connected      bool       `json:"connected"`
	State          string     `json:"state"` // "disconnected", "connecting", "connected", "error"
	ActiveNode     *Config    `json:"active_node,omitempty"`
	Latency        int        `json:"latency"`
	ExitIP         string     `json:"exit_ip,omitempty"`
	ConnectedSince *time.Time `json:"connected_since,omitempty"`
	SocksPort      int        `json:"socks_port"`
	HTTPPort       int        `json:"http_port"`
	SystemProxy    bool       `json:"system_proxy"`
	TunMode        bool       `json:"tun_mode"`
	GamingMode     bool       `json:"gaming_mode"`
	ShareLAN       bool       `json:"share_lan"`
	IsAdmin        bool       `json:"is_admin"`
	Error          string     `json:"error,omitempty"`
}

// Settings represents user configurable preferences
type Settings struct {
	TestConcurrency   int    `json:"test_concurrency"`   // 50, 100, 250, 500, 1000
	TestTimeoutSec    int    `json:"test_timeout_sec"`   // default: 5
	TestEndpoint      string `json:"test_endpoint"`      // default: http://cp.cloudflare.com/generate_204
	SocksPort         int    `json:"socks_port"`         // default: 10808
	HTTPPort          int    `json:"http_port"`          // default: 10809
	SystemProxy       bool   `json:"system_proxy"`       // default: true
	TunMode           bool   `json:"tun_mode"`           // default: false
	GamingMode        bool   `json:"gaming_mode"`        // default: false
	ShareLAN          bool   `json:"share_lan"`          // default: false
	AutoScan          bool   `json:"auto_scan"`          // default: true
	AutoScanInterval  int    `json:"auto_scan_interval"` // minutes (e.g. 180 for 3 hours)
	AutoFailover      bool   `json:"auto_failover"`      // default: true
	MaxFailoverTries  int    `json:"max_failover_tries"` // default: 3
	StartWithWindows  bool   `json:"start_with_windows"`
	MinimizeToTray    bool   `json:"minimize_to_tray"`

	// Routing & Split Tunneling Settings
	RoutingMode   string `json:"routing_mode"`   // "bypass_iran" (default), "global", "proxy_apps_only", "custom"
	DirectDomains string `json:"direct_domains"` // Domains to bypass proxy (e.g. .ir, banking)
	ProxyDomains  string `json:"proxy_domains"`  // Domains to force through proxy
	DirectApps    string `json:"direct_apps"`    // App executables to bypass proxy (e.g. cs2.exe, valorant.exe)
	ProxyApps     string `json:"proxy_apps"`     // App executables to force through proxy (e.g. telegram.exe, discord.exe)
	BlockDomains  string `json:"block_domains"`  // Domains to block (e.g. ads)
	UpdateRepo    string `json:"update_repo"`    // GitHub repository for auto-updates (e.g. "connective-app/connective")
}
