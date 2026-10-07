package gaming

import (
	"fmt"
	"net"
	"strconv"
	"time"

	"freenode/pkg/database"
	"freenode/pkg/models"
	"freenode/pkg/xray"
)

const (
	// DefaultMTU is the standard MTU for general browsing and downloading
	DefaultMTU = 1500

	// GamingMTU is the optimized MTU (1400) for low-latency gaming TUN mode,
	// preventing IP fragmentation overhead with UDP and game packet encapsulations.
	GamingMTU = 1400
)

// GamingSockoptMap returns low-latency socket options for Xray streamSettings
func GamingSockoptMap() map[string]interface{} {
	return map[string]interface{}{
		"tcpNoDelay":           true,
		"tcpFastOpen":          true,
		"tcpKeepAliveIdle":     15,
		"tcpKeepAliveInterval": 5,
	}
}

// SetSocketLowLatency configures low-latency TCP socket options (TCP_NODELAY, keep-alive) on net.Conn
func SetSocketLowLatency(conn net.Conn) error {
	if tcpConn, ok := conn.(*net.TCPConn); ok {
		_ = tcpConn.SetNoDelay(true)
		_ = tcpConn.SetKeepAlive(true)
		_ = tcpConn.SetKeepAlivePeriod(15 * time.Second)
	}
	return nil
}

// GamingLANIPs returns the list of private, multicast, and broadcast CIDR blocks for local gaming LAN bypass
func GamingLANIPs() []string {
	return []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"100.64.0.0/10",
		"224.0.0.0/4",       // Multicast used by LAN multiplayer game discovery
		"255.255.255.255/32", // Broadcast packets
	}
}

// DefaultGamingApps returns well-known online competitive multiplayer game executables
func DefaultGamingApps() []string {
	return []string{
		"cs2.exe",
		"valorant.exe",
		"dota2.exe",
		"leagueclient.exe",
		"overwatch.exe",
		"fortniteclient-win64-shipping.exe",
		"r5apex.exe",
		"cod.exe",
		"rainbowsix.exe",
		"gta5.exe",
		"pubg.exe",
		"rocketleague.exe",
	}
}

// OptimizeOutbound injects low-latency socket options (TCP_NODELAY, TFO, fast keepalive)
// into an Xray outbound streamSettings map.
func OptimizeOutbound(outbound map[string]interface{}, gamingMode bool) {
	if !gamingMode || outbound == nil {
		return
	}

	streamSettings, ok := outbound["streamSettings"].(map[string]interface{})
	if !ok || streamSettings == nil {
		streamSettings = make(map[string]interface{})
		outbound["streamSettings"] = streamSettings
	}

	streamSettings["sockopt"] = GamingSockoptMap()
}

// IsGamingMode checks if gaming mode is currently enabled in the database
func IsGamingMode(db *database.DB) bool {
	if db == nil {
		return false
	}
	return db.GetSetting("gaming_mode", "false") == "true"
}

// ToggleGamingMode updates the gaming_mode setting in the database and reloads the runner if active
func ToggleGamingMode(db *database.DB, runner *xray.Runner, enabled bool) error {
	if db != nil {
		if err := db.SetSetting("gaming_mode", strconv.FormatBool(enabled)); err != nil {
			return fmt.Errorf("updating gaming_mode setting: %w", err)
		}
	}

	if runner != nil {
		runner.SetGamingMode(enabled)
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
