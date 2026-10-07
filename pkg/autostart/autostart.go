//go:build windows
// +build windows

package autostart

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

const (
	// AppName is the registry value name under the Run key.
	AppName = "Conective"

	// RunKeyPath is the Windows registry subkey for per-user autostart.
	RunKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
)

// IsEnabled checks if Conective is registered to start with Windows in HKCU\...\Run.
func IsEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, RunKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()

	val, _, err := k.GetStringValue(AppName)
	if err != nil || strings.TrimSpace(val) == "" {
		return false
	}
	return true
}

// SetEnabled adds or removes Conective from HKCU\...\Run.
func SetEnabled(enable bool) error {
	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}
		exePath, err = filepath.Abs(exePath)
		if err != nil {
			return fmt.Errorf("failed to get absolute executable path: %w", err)
		}

		// Ensure path is quoted in case of spaces in directory path, and start minimized
		cmd := fmt.Sprintf("\"%s\" --minimized", exePath)

		k, _, err := registry.CreateKey(registry.CURRENT_USER, RunKeyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("failed to open/create registry run key: %w", err)
		}
		defer k.Close()

		if err := k.SetStringValue(AppName, cmd); err != nil {
			return fmt.Errorf("failed to write registry value: %w", err)
		}
		return nil
	}

	// Disable autostart
	k, err := registry.OpenKey(registry.CURRENT_USER, RunKeyPath, registry.SET_VALUE)
	if err != nil {
		if errors.Is(err, registry.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("failed to open registry run key: %w", err)
	}
	defer k.Close()

	err = k.DeleteValue(AppName)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("failed to delete registry value: %w", err)
	}
	return nil
}
