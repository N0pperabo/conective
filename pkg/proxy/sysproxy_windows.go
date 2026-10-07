//go:build windows

package proxy

import (
	"fmt"
	"sync"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

var (
	modWinInet            = syscall.NewLazyDLL("wininet.dll")
	procInternetSetOption = modWinInet.NewProc("InternetSetOptionW")
)

const (
	internetOptionSettingsChanged = 39
	internetOptionRefresh         = 37
	internetSettingsKeyPath       = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
)

type Manager struct {
	mu           sync.Mutex
	backupEnable uint32
	backupServer string
	hasBackup    bool
}

func NewManager() *Manager {
	return &Manager{}
}

// BackupCurrentState records the existing system proxy settings
func (m *Manager) BackupCurrentState() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	enable, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil {
		m.backupEnable = uint32(enable)
	}

	server, _, err := k.GetStringValue("ProxyServer")
	if err == nil {
		m.backupServer = server
	}

	m.hasBackup = true
	return nil
}

// Enable sets the Windows system proxy to 127.0.0.1:port
func (m *Manager) Enable(httpPort int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Backup if not backed up yet
	if !m.hasBackup {
		_ = m.backupCurrentStateLocked()
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("opening registry key for write: %w", err)
	}
	defer k.Close()

	proxyAddress := fmt.Sprintf("127.0.0.1:%d", httpPort)
	if err := k.SetStringValue("ProxyServer", proxyAddress); err != nil {
		return fmt.Errorf("setting ProxyServer: %w", err)
	}

	if err := k.SetDWordValue("ProxyEnable", 1); err != nil {
		return fmt.Errorf("setting ProxyEnable: %w", err)
	}

	// Bypass local addresses
	_ = k.SetStringValue("ProxyOverride", "<local>;localhost;127.*;10.*;172.16.*;192.168.*")

	m.notifyWinINet()
	return nil
}

// Disable restores proxy to disabled or the original backup state
func (m *Manager) Disable() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	if m.hasBackup && m.backupEnable == 1 {
		_ = k.SetStringValue("ProxyServer", m.backupServer)
		_ = k.SetDWordValue("ProxyEnable", 1)
	} else {
		_ = k.SetDWordValue("ProxyEnable", 0)
	}

	m.notifyWinINet()
	return nil
}

// RecoverOrphanedProxy checks if proxy was left pointing to local freenode and resets it on crash recovery
func (m *Manager) RecoverOrphanedProxy(httpPort int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKeyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer k.Close()

	enable, _, err := k.GetIntegerValue("ProxyEnable")
	if err != nil || enable != 1 {
		return
	}

	server, _, err := k.GetStringValue("ProxyServer")
	if err != nil {
		return
	}

	// If it pointed to 127.0.0.1 from a previous crashed run, turn it off
	expected := fmt.Sprintf("127.0.0.1:%d", httpPort)
	if server == expected || server == "127.0.0.1:10809" {
		_ = k.SetDWordValue("ProxyEnable", 0)
		m.notifyWinINet()
	}
}

func (m *Manager) backupCurrentStateLocked() error {
	k, err := registry.OpenKey(registry.CURRENT_USER, internetSettingsKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()

	enable, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil {
		m.backupEnable = uint32(enable)
	}

	server, _, err := k.GetStringValue("ProxyServer")
	if err == nil {
		m.backupServer = server
	}

	m.hasBackup = true
	return nil
}

func (m *Manager) notifyWinINet() {
	go func() {
		defer func() {
			_ = recover()
		}()
		_, _, _ = procInternetSetOption.Call(0, uintptr(internetOptionSettingsChanged), 0, 0)
		_, _, _ = procInternetSetOption.Call(0, uintptr(internetOptionRefresh), 0, 0)
	}()
}
