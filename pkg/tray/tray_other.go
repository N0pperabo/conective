//go:build !windows
// +build !windows

package tray

// Options specifies configuration for Tray.
type Options struct {
	IconBytes    []byte
	Title        string
	Tooltip      string
	OnShow       func()
	OnDisconnect func()
	OnExit       func()
}

// Tray is a stub implementation for non-Windows platforms.
type Tray struct{}

func New(opts Options) *Tray {
	return &Tray{}
}

func (t *Tray) Start() error {
	return nil
}

func (t *Tray) Stop() {}

func (t *Tray) SetConnected(connected bool) {}

func (t *Tray) SetTooltip(tip string) {}

func (t *Tray) ShowBalloon(title, message string) {}

func (t *Tray) AttachWindow(hwnd uintptr, isMinimizeEnabled func() bool) {}

func (t *Tray) ShowWindow() {}

func (t *Tray) HideWindow() {}

func (t *Tray) ExitApp() {}

// SetWindowIcon is a stub implementation for non-Windows platforms.
func SetWindowIcon(hwnd uintptr, iconBytes []byte) error {
	return nil
}

