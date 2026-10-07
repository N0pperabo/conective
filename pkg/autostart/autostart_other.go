//go:build !windows
// +build !windows

package autostart

// IsEnabled returns false on non-Windows platforms.
func IsEnabled() bool {
	return false
}

// SetEnabled is a no-op on non-Windows platforms.
func SetEnabled(enable bool) error {
	return nil
}
