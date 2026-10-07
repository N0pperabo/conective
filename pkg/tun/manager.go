//go:build windows

package tun

import (
	"bufio"
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// IsAdmin checks if the current process has Windows Administrator privileges
func IsAdmin() bool {
	var sid *windows.SID
	err := windows.AllocateAndInitializeSid(
		&windows.SECURITY_NT_AUTHORITY,
		2,
		windows.SECURITY_BUILTIN_DOMAIN_RID,
		windows.DOMAIN_ALIAS_RID_ADMINS,
		0, 0, 0, 0, 0, 0,
		&sid,
	)
	if err != nil {
		return false
	}
	defer windows.FreeSid(sid)

	token := windows.Token(0)
	member, err := token.IsMember(sid)
	return err == nil && member
}

type Manager struct {
	mu                sync.Mutex
	active            bool
	ifIndex           int
	adapterName       string
	serverIP          string
	defaultGateway    string
	physicalInterface string
}

func silentCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

func NewManager() *Manager {
	return &Manager{}
}

// FindPhysicalGatewayAndInterface parses "route print 0.0.0.0" and returns both the gateway IP
// and the physical network interface name (e.g. "Wi-Fi" or "Ethernet")
func FindPhysicalGatewayAndInterface() (gw string, ifaceName string, err error) {
	cmd := silentCmd("route", "print", "0.0.0.0")
	out, err := cmd.Output()
	if err != nil {
		return "", "", err
	}

	scanner := bufio.NewScanner(bytes.NewReader(out))
	foundActive := false
	var targetIfaceIP string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Active Routes:") {
			foundActive = true
			continue
		}
		if foundActive && strings.HasPrefix(line, "0.0.0.0") {
			fields := strings.Fields(line)
			if len(fields) >= 4 {
				candGW := fields[2]
				candIP := fields[3]
				if candGW != "On-link" && net.ParseIP(candGW) != nil && net.ParseIP(candIP) != nil {
					gw = candGW
					targetIfaceIP = candIP
					break
				}
			}
		}
	}
	if gw == "" {
		return "", "", fmt.Errorf("default gateway not found")
	}

	if targetIfaceIP != "" {
		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				addrs, err := iface.Addrs()
				if err != nil {
					continue
				}
				for _, addr := range addrs {
					if ipnet, ok := addr.(*net.IPNet); ok {
						if ipnet.IP.String() == targetIfaceIP {
							ifaceName = iface.Name
							return gw, ifaceName, nil
						}
					}
				}
			}
		}
	}

	return gw, ifaceName, nil
}

// FindDefaultGateway parses "route print 0.0.0.0" to locate the real internet gateway
func FindDefaultGateway() (string, error) {
	gw, _, err := FindPhysicalGatewayAndInterface()
	return gw, err
}

// FindAdapterIndex waits for Wintun device to settle and returns its OS interface index and name
func FindAdapterIndex(adapterName string) (int, string, error) {
	for i := 0; i < 40; i++ {
		ifaces, err := net.Interfaces()
		if err == nil {
			for _, iface := range ifaces {
				if strings.EqualFold(iface.Name, adapterName) {
					return iface.Index, iface.Name, nil
				}
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	return -1, "", fmt.Errorf("Wintun adapter %q not detected in Windows network stack (requires Admin privileges and wintun.dll)", adapterName)
}

// IsWintunDevice checks if a device instance ID and description belong to a FreeNode/Conective/Xray Wintun adapter
func IsWintunDevice(instanceID, desc string) bool {
	idUpper := strings.ToUpper(strings.TrimSpace(instanceID))
	descLower := strings.ToLower(strings.TrimSpace(desc))
	if !strings.HasPrefix(idUpper, `SWD\WINTUN\`) {
		return false
	}
	return strings.Contains(descLower, "xray tunnel") ||
		strings.Contains(descLower, "freenode") ||
		strings.Contains(descLower, "conective") ||
		strings.Contains(descLower, "wintun")
}

// ParseWintunDevices parses pnputil /enum-devices output and returns Instance IDs
// of Wintun adapters matching FreeNode / Xray Tunnel.
func ParseWintunDevices(output string) []string {
	var ids []string
	scanner := bufio.NewScanner(strings.NewReader(output))
	var currentInstanceID string
	var currentDesc string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "Instance ID:") {
			currentInstanceID = ""
			currentDesc = ""
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentInstanceID = strings.TrimSpace(parts[1])
			}
		} else if strings.HasPrefix(line, "Device Description:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				currentDesc = strings.TrimSpace(parts[1])
			}
			if currentInstanceID != "" && currentDesc != "" {
				if IsWintunDevice(currentInstanceID, currentDesc) {
					ids = append(ids, currentInstanceID)
				}
				currentInstanceID = ""
				currentDesc = ""
			}
		}
	}
	return ids
}

// IsStaleAdapterName returns true if an adapter name represents a stale or duplicate candidate
// such as "FreeNodeTUN*", "ConectiveTUN2", "ConectiveTUN3", etc.
func IsStaleAdapterName(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	if strings.HasPrefix(n, "freenode") {
		return true
	}
	if strings.HasPrefix(n, "conectivetun") && n != "conectivetun" {
		return true
	}
	return false
}

// FindAdapterInstanceID looks up the PnP Instance ID for a given network interface name from registry.
func FindAdapterInstanceID(adapterName string) (string, error) {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`, registry.READ)
	if err != nil {
		return "", err
	}
	defer k.Close()

	guids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return "", err
	}

	for _, guid := range guids {
		connKey, err := registry.OpenKey(k, guid+`\Connection`, registry.READ)
		if err != nil {
			continue
		}
		name, _, _ := connKey.GetStringValue("Name")
		pnpID, _, _ := connKey.GetStringValue("PnPInstanceId")
		connKey.Close()

		if strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(adapterName)) {
			return pnpID, nil
		}
	}
	return "", fmt.Errorf("adapter %q not found in registry", adapterName)
}

// FindStaleWintunAdapters returns all PnP instance IDs for stale or duplicate adapters
// (e.g. FreeNodeTUN*, ConectiveTUN2, ConectiveTUN3, and any duplicate ConectiveTUN instances).
func FindStaleWintunAdapters() []string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`, registry.READ)
	if err != nil {
		return nil
	}
	defer k.Close()

	guids, err := k.ReadSubKeyNames(-1)
	if err != nil {
		return nil
	}

	var staleIDs []string
	foundPrimary := false

	for _, guid := range guids {
		connKey, err := registry.OpenKey(k, guid+`\Connection`, registry.READ)
		if err != nil {
			continue
		}
		name, _, _ := connKey.GetStringValue("Name")
		pnpID, _, _ := connKey.GetStringValue("PnPInstanceId")
		connKey.Close()

		pnpUpper := strings.ToUpper(strings.TrimSpace(pnpID))
		if !strings.HasPrefix(pnpUpper, `SWD\WINTUN\`) {
			continue
		}

		nameTrim := strings.TrimSpace(name)
		if IsStaleAdapterName(nameTrim) {
			staleIDs = append(staleIDs, pnpID)
			continue
		}

		if strings.EqualFold(nameTrim, "ConectiveTUN") {
			if foundPrimary {
				// Duplicate ConectiveTUN adapter
				staleIDs = append(staleIDs, pnpID)
			} else {
				foundPrimary = true
			}
		}
	}

	return staleIDs
}

// CleanupStaleWintunAdapters scans for any leftover or duplicate Wintun adapters
// (e.g. ConectiveTUN2, ConectiveTUN3, FreeNodeTUN*) and forcefully removes them via pnputil.
// It preserves the primary healthy "ConectiveTUN" adapter for fast static reuse.
func CleanupStaleWintunAdapters() {
	if !IsAdmin() {
		return
	}

	removed := make(map[string]bool)

	// 1. Remove stale adapters identified from registry
	for _, id := range FindStaleWintunAdapters() {
		idUpper := strings.ToUpper(strings.TrimSpace(id))
		if idUpper != "" && !removed[idUpper] {
			delCmd := silentCmd("pnputil", "/remove-device", id)
			_ = delCmd.Run()
			removed[idUpper] = true
		}
	}

	// 2. Also inspect pnputil output for any devices with FreeNode description
	cmd := silentCmd("pnputil", "/enum-devices", "/class", "Net")
	out, err := cmd.Output()
	if err == nil {
		scanner := bufio.NewScanner(strings.NewReader(string(out)))
		var currentInstanceID string
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if strings.HasPrefix(line, "Instance ID:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					currentInstanceID = strings.TrimSpace(parts[1])
				}
			} else if strings.HasPrefix(line, "Device Description:") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					descLower := strings.ToLower(strings.TrimSpace(parts[1]))
					if strings.Contains(descLower, "freenode") && currentInstanceID != "" {
						idUpper := strings.ToUpper(currentInstanceID)
						if !removed[idUpper] {
							delCmd := silentCmd("pnputil", "/remove-device", currentInstanceID)
							_ = delCmd.Run()
							removed[idUpper] = true
						}
					}
				}
				currentInstanceID = ""
			}
		}
	}
}

// ResetOrRemoveAdapter forcefully removes a stuck or corrupted adapter by name using pnputil
// so that Windows PnP cleans it up and Wintun can recreate it cleanly.
func ResetOrRemoveAdapter(adapterName string) error {
	if !IsAdmin() {
		return fmt.Errorf("resetting adapter requires administrator privileges")
	}

	instanceID, err := FindAdapterInstanceID(adapterName)
	if err == nil && instanceID != "" {
		delCmd := silentCmd("pnputil", "/remove-device", instanceID)
		_ = delCmd.Run()
		time.Sleep(500 * time.Millisecond)
		return nil
	}

	// Fallback: search pnputil for Wintun device matching Xray/Conective
	cmd := silentCmd("pnputil", "/enum-devices", "/class", "Net")
	out, err := cmd.Output()
	if err == nil {
		ids := ParseWintunDevices(string(out))
		for _, id := range ids {
			delCmd := silentCmd("pnputil", "/remove-device", id)
			_ = delCmd.Run()
		}
		time.Sleep(500 * time.Millisecond)
	}

	return nil
}

// ResetOrRemoveAdapter is a method on Manager for convenience
func (m *Manager) ResetOrRemoveAdapter(adapterName string) error {
	return ResetOrRemoveAdapter(adapterName)
}

// SetupAdapter configures the Wintun adapter IP, DNS, and on-link default routing
func (m *Manager) SetupAdapter(adapterName, serverHost string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !IsAdmin() {
		return fmt.Errorf("TUN Mode requires Administrator privileges! Run FreeNode as Administrator.")
	}

	// 1. Locate physical default gateway and network interface
	gw, physIface, err := FindPhysicalGatewayAndInterface()
	if err != nil {
		return fmt.Errorf("locating default gateway: %w", err)
	}
	m.defaultGateway = gw
	m.physicalInterface = physIface

	// 2. Resolve server host to IP if it's a domain name
	var sIP string
	if ip := net.ParseIP(serverHost); ip != nil {
		sIP = ip.String()
	} else if serverHost != "" {
		ips, err := net.LookupIP(serverHost)
		if err == nil && len(ips) > 0 {
			sIP = ips[0].String()
		}
	}
	m.serverIP = sIP

	// 3. Find Wintun adapter index in Windows network stack
	ifIndex, realName, err := FindAdapterIndex(adapterName)
	if err != nil {
		return err
	}
	m.ifIndex = ifIndex
	m.adapterName = realName

	// 4. Configure IP: 198.18.0.2 / 255.255.0.0 (RFC 2544 benchmark range, guaranteed no LAN conflict)
	// Gateway set to 'none' because Wintun is a point-to-point Layer 3 adapter without ARP!
	setIP := silentCmd("netsh", "interface", "ipv4", "set", "address",
		fmt.Sprintf("name=%s", realName), "static", "198.18.0.2", "255.255.0.0", "none")
	_ = setIP.Run()

	// 5. Configure DNS servers
	setDNS1 := silentCmd("netsh", "interface", "ipv4", "set", "dns",
		fmt.Sprintf("name=%s", realName), "static", "1.1.1.1")
	_ = setDNS1.Run()

	setDNS2 := silentCmd("netsh", "interface", "ipv4", "add", "dns",
		fmt.Sprintf("name=%s", realName), "8.8.8.8", "index=2")
	_ = setDNS2.Run()

	// 6. Direct route for proxy server through physical gateway (prevents routing loop)
	if sIP != "" {
		addServerRoute := silentCmd("route", "add", sIP, "mask", "255.255.255.255", gw, "metric", "1")
		_ = addServerRoute.Run()
	}

	// 7. Add on-link /1 default routes directly through Wintun interface index
	// Gateway 0.0.0.0 with 'if <ifIndex>' passes raw IP packets directly to Wintun without ARP
	r1 := silentCmd("route", "add", "0.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "if", strconv.Itoa(ifIndex), "metric", "1")
	_ = r1.Run()

	r2 := silentCmd("route", "add", "128.0.0.0", "mask", "128.0.0.0", "0.0.0.0", "if", strconv.Itoa(ifIndex), "metric", "1")
	_ = r2.Run()

	m.active = true
	return nil
}

// TeardownAdapter removes TUN routes and restores original internet routing cleanly
func (m *Manager) TeardownAdapter() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Delete TUN /1 default routes
	r1 := silentCmd("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = r1.Run()

	r2 := silentCmd("route", "delete", "128.0.0.0", "mask", "128.0.0.0")
	_ = r2.Run()

	if m.serverIP != "" {
		delServerRoute := silentCmd("route", "delete", m.serverIP)
		_ = delServerRoute.Run()
		m.serverIP = ""
	}

	m.adapterName = ""
	m.active = false
	// Do NOT call CleanupStaleWintunAdapters() on normal teardown!
	// Leaving the static adapter installed allows instant reuse on subsequent connections
	// without PnP teardown latency or Windows ERROR_FILE_EXISTS errors.
	// Leaving the static IP address (198.18.0.2 / 255.255.0.0) alone without DHCP
	// avoids Windows 10-30s "Identifying..." DHCP lease timeouts.
	return nil
}

// PhysicalInterface returns the physical network interface name (e.g. "Wi-Fi" or "Ethernet")
func (m *Manager) PhysicalInterface() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.physicalInterface
}

// RecoverOrphanedTUNRoutes cleans up any lingering /1 routes and stale Wintun adapters from crashed prior runs
func (m *Manager) RecoverOrphanedTUNRoutes() {
	r1 := silentCmd("route", "delete", "0.0.0.0", "mask", "128.0.0.0")
	_ = r1.Run()

	r2 := silentCmd("route", "delete", "128.0.0.0", "mask", "128.0.0.0")
	_ = r2.Run()

	CleanupStaleWintunAdapters()
}

// IsActive returns whether TUN mode routing is currently engaged
func (m *Manager) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.active
}
