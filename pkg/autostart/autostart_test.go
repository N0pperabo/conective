//go:build windows
// +build windows

package autostart

import (
	"testing"
)

func TestAutostartToggle(t *testing.T) {
	orig := IsEnabled()
	defer func() {
		_ = SetEnabled(orig)
	}()

	// 1. Enable
	if err := SetEnabled(true); err != nil {
		t.Fatalf("SetEnabled(true) failed: %v", err)
	}
	if !IsEnabled() {
		t.Fatalf("expected IsEnabled() == true after SetEnabled(true)")
	}

	// 2. Disable
	if err := SetEnabled(false); err != nil {
		t.Fatalf("SetEnabled(false) failed: %v", err)
	}
	if IsEnabled() {
		t.Fatalf("expected IsEnabled() == false after SetEnabled(false)")
	}

	// 3. Idempotent disable
	if err := SetEnabled(false); err != nil {
		t.Fatalf("second SetEnabled(false) failed: %v", err)
	}
	if IsEnabled() {
		t.Fatalf("expected IsEnabled() == false after second SetEnabled(false)")
	}
}
