package lan

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/xray"
)

// LANInfo represents the current status and network details for LAN sharing
type LANInfo struct {
	Enabled   bool     `json:"enabled"`
	LocalIPs  []string `json:"local_ips"`
	HTTPPort  int      `json:"http_port"`
	SOCKSPort int      `json:"socks_port"`
	ProxyURL  string   `json:"proxy_url"`
}

// GetLocalIPs finds the machine's genuine local IPv4 addresses (e.g., 192.168.1.x, 10.x.x.x),
// filtering out loopback, link-local (169.254.x.x), and virtual Wintun/TUN adapter subnets.
func GetLocalIPs() ([]string, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("failed to get network interfaces: %w", err)
	}

	var ips []string
	seen := make(map[string]bool)

	for _, iface := range ifaces {
		// Ignore interfaces that are down or are loopback
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		// Ignore TUN / TAP / Wintun virtual adapters by name
		lowerName := strings.ToLower(iface.Name)
		if strings.Contains(lowerName, "tun") || strings.Contains(lowerName, "tap") || strings.Contains(lowerName, "wintun") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}

			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}

			ipv4 := ip.To4()

			// Skip link-local APIPA addresses (169.254.0.0/16)
			if ipv4.IsLinkLocalUnicast() {
				continue
			}

			// Skip RFC 2544 benchmark subnet used by Wintun (198.18.0.0/15)
			if ipv4[0] == 198 && (ipv4[1] == 18 || ipv4[1] == 19) {
				continue
			}

			ipStr := ipv4.String()
			if !seen[ipStr] {
				seen[ipStr] = true
				ips = append(ips, ipStr)
			}
		}
	}

	return ips, nil
}

// GetListenHost returns "0.0.0.0" when shareLAN is enabled, otherwise "127.0.0.1"
func GetListenHost(shareLAN bool) string {
	if shareLAN {
		return "0.0.0.0"
	}
	return "127.0.0.1"
}

// GetLANInfo gathers the active LAN configuration, local IPs, and proxy URLs
func GetLANInfo(db *database.DB, httpPort, socksPort int) (LANInfo, error) {
	enabled := false
	if db != nil {
		enabled = db.GetSetting("share_lan", "false") == "true"
	}

	ips, err := GetLocalIPs()
	if err != nil {
		ips = []string{}
	}

	proxyURL := ""
	if enabled && len(ips) > 0 {
		proxyURL = fmt.Sprintf("http://%s:%d", ips[0], httpPort)
	} else if len(ips) > 0 {
		proxyURL = fmt.Sprintf("http://%s:%d", ips[0], httpPort)
	} else {
		proxyURL = fmt.Sprintf("http://127.0.0.1:%d", httpPort)
	}

	return LANInfo{
		Enabled:   enabled,
		LocalIPs:  ips,
		HTTPPort:  httpPort,
		SOCKSPort: socksPort,
		ProxyURL:  proxyURL,
	}, nil
}

// ToggleLANSharing toggles LAN sharing in the database and reloads the runner if active
func ToggleLANSharing(db *database.DB, runner *xray.Runner, enabled bool) error {
	if db != nil {
		if err := db.SetSetting("share_lan", strconv.FormatBool(enabled)); err != nil {
			return fmt.Errorf("updating share_lan setting: %w", err)
		}
	}

	if runner != nil {
		runner.SetShareLAN(enabled)
		if runner.IsRunning() {
			activeNode := runner.GetActiveNode()
			if activeNode != nil {
				go func(n *models.Config) {
					_ = runner.Connect(n)
				}(activeNode)
			}
		}
	}

	return nil
}
