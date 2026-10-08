//go:build windows
// +build windows

package tray

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	cmdShow       = 1001
	cmdStatus     = 1002
	cmdDisconnect = 1003
	cmdExit       = 1004
)

var (
	attachedWindows   sync.Map // map[uintptr]*Tray
	trayClassAtom     uintptr
	classInitOnce     sync.Once
	taskbarCreatedMsg uintptr
)

// Options specifies configuration for Tray.
type Options struct {
	IconBytes    []byte
	Title        string
	Tooltip      string
	OnShow       func()
	OnDisconnect func()
	OnExit       func()
}

// Tray manages the Windows system tray icon, context menu, and window minimize-to-tray hooks.
type Tray struct {
	opts Options

	mu          sync.RWMutex
	hwnd        uintptr
	threadID    uint32
	hIcon       windows.Handle
	connected   bool
	tooltip     string
	isExiting   bool
	stopped     bool
	readyChan   chan error
	balloonOnce sync.Once

	// Attached main window hook
	targetHwnd        uintptr
	origTargetWndProc uintptr
	isMinimizeEnabled func() bool
}

// New creates a new Tray instance.
func New(opts Options) *Tray {
	if opts.Title == "" {
		opts.Title = "Conective"
	}
	if opts.Tooltip == "" {
		opts.Tooltip = "Conective - Disconnected"
	}

	return &Tray{
		opts:      opts,
		tooltip:   opts.Tooltip,
		readyChan: make(chan error, 1),
	}
}

// Start registers the system tray icon and launches the dedicated Win32 message loop.
// Start blocks until the tray icon is registered or fails.
func (t *Tray) Start() error {
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		t.run()
	}()

	return <-t.readyChan
}

func (t *Tray) run() {
	var err error
	t.threadID = windows.GetCurrentThreadId()

	// If current thread desktop is not the interactive shell desktop (e.g. running under tests/service),
	// attempt to switch to "Default" desktop so Shell_NotifyIcon can reach Shell_TrayWnd.
	hDef, _, _ := procOpenDesktopW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Default"))), 0, 0, 0x10000000)
	if hDef != 0 {
		procSetThreadDesktop.Call(hDef)
		procCloseDesktop.Call(hDef)
	}

	hInst, _, _ := procGetModuleHandleW.Call(0)
	hInstance := windows.Handle(hInst)

	className := "ConectiveTrayWindowClass"
	classNamePtr := windows.StringToUTF16Ptr(className)

	classInitOnce.Do(func() {
		wc := WNDCLASSEXW{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			HInstance:     hInstance,
			LpszClassName: classNamePtr,
			LpfnWndProc:   windows.NewCallback(trayWndProc),
		}
		r, _, errReg := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
		if r == 0 {
			err = fmt.Errorf("failed to register tray window class: %v", errReg)
			return
		}
		trayClassAtom = r

		rMsg, _, _ := procRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TaskbarCreated"))))
		taskbarCreatedMsg = rMsg
	})

	if err != nil {
		t.readyChan <- err
		return
	}

	windowTitlePtr := windows.StringToUTF16Ptr("Conective Tray Listener")
	hwnd, _, errCreate := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(classNamePtr)),
		uintptr(unsafe.Pointer(windowTitlePtr)),
		0,
		0, 0, 0, 0,
		0, 0,
		uintptr(hInstance),
		0,
	)
	if hwnd == 0 {
		t.readyChan <- fmt.Errorf("failed to create tray window: %v", errCreate)
		return
	}

	t.hwnd = hwnd
	attachedWindows.Store(hwnd, t)

	// Load Icon
	t.hIcon = loadIcon(t.opts.IconBytes)

	// Register tray icon with Windows Shell
	t.addTrayIcon()

	// Ready!
	t.readyChan <- nil

	// Win32 Message Loop
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(
			uintptr(unsafe.Pointer(&msg)),
			0,
			0,
			0,
		)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	// Cleanup tray icon upon message loop termination
	delNid := NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   windows.Handle(t.hwnd),
		UID:    1,
	}
	shellNotifyIcon(NIM_DELETE, &delNid)

	if t.hIcon != 0 {
		procDestroyIcon.Call(uintptr(t.hIcon))
		t.hIcon = 0
	}
}

// Stop cleanly terminates the tray icon and its message loop.
func (t *Tray) Stop() {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	hwnd := t.hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	}
}

// SetConnected updates the tray connection status in tooltip and menu.
func (t *Tray) SetConnected(connected bool) {
	t.mu.Lock()
	t.connected = connected
	if connected {
		t.tooltip = "Conective - Connected"
	} else {
		t.tooltip = "Conective - Disconnected"
	}
	hwnd := t.hwnd
	tip := t.tooltip
	t.mu.Unlock()

	if hwnd != 0 {
		nid := NOTIFYICONDATAW{
			CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
			HWnd:   windows.Handle(hwnd),
			UID:    1,
			UFlags: NIF_TIP,
		}
		copyUTF16(nid.SzTip[:], tip)
		shellNotifyIcon(NIM_MODIFY, &nid)
	}
}

// SetTooltip updates the hover tooltip string for the tray icon.
func (t *Tray) SetTooltip(tip string) {
	t.mu.Lock()
	t.tooltip = tip
	hwnd := t.hwnd
	t.mu.Unlock()

	if hwnd != 0 {
		nid := NOTIFYICONDATAW{
			CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
			HWnd:   windows.Handle(hwnd),
			UID:    1,
			UFlags: NIF_TIP,
		}
		copyUTF16(nid.SzTip[:], tip)
		shellNotifyIcon(NIM_MODIFY, &nid)
	}
}

// ShowBalloon displays a native Windows balloon / toast notification.
func (t *Tray) ShowBalloon(title, message string) {
	t.mu.RLock()
	hwnd := t.hwnd
	t.mu.RUnlock()

	if hwnd == 0 {
		return
	}

	nid := NOTIFYICONDATAW{
		CbSize:      uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:        windows.Handle(hwnd),
		UID:         1,
		UFlags:      NIF_INFO,
		DwInfoFlags: NIIF_INFO,
	}
	copyUTF16(nid.SzInfoTitle[:], title)
	copyUTF16(nid.SzInfo[:], message)
	shellNotifyIcon(NIM_MODIFY, &nid)
}

// AttachWindow subclasses the target window to intercept the close button (WM_CLOSE).
// AttachWindow records the main application window handle for tray restore/hide actions.
func (t *Tray) AttachWindow(hwnd uintptr, isMinimizeEnabled func() bool) {
	if hwnd == 0 {
		return
	}
	t.mu.Lock()
	t.targetHwnd = hwnd
	t.isMinimizeEnabled = isMinimizeEnabled
	t.mu.Unlock()

	attachedWindows.Store(hwnd, t)
}

// ShowWindow restores and focuses the attached window.
func (t *Tray) ShowWindow() {
	t.mu.RLock()
	target := t.targetHwnd
	iconBytes := t.opts.IconBytes
	t.mu.RUnlock()

	if target != 0 {
		if len(iconBytes) > 0 {
			_ = SetWindowIcon(target, iconBytes)
		}
		procShowWindow.Call(target, SW_SHOW)
		procShowWindow.Call(target, SW_RESTORE)
		procSetForegroundWindow.Call(target)
	}
}

// HideWindow hides the attached window.
func (t *Tray) HideWindow() {
	t.mu.RLock()
	target := t.targetHwnd
	t.mu.RUnlock()

	if target != 0 {
		procShowWindow.Call(target, SW_HIDE)
	}
}

// ExitApp signals the app to exit cleanly, bypassing the minimize-to-tray interceptor.
func (t *Tray) ExitApp() {
	t.mu.Lock()
	t.isExiting = true
	target := t.targetHwnd
	onExit := t.opts.OnExit
	t.mu.Unlock()

	if onExit != nil {
		onExit()
	}

	// Post WM_CLOSE to target window so it destroys cleanly
	if target != 0 {
		procPostMessageW.Call(target, WM_CLOSE, 0, 0)
	}

	t.Stop()
}

func (t *Tray) showContextMenu() {
	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}
	defer procDestroyMenu.Call(hMenu)

	// 1. Show Conective (bold default item)
	showText, _ := windows.UTF16PtrFromString("Show Conective")
	procAppendMenuW.Call(hMenu, MF_STRING, cmdShow, uintptr(unsafe.Pointer(showText)))
	procSetMenuDefaultItem.Call(hMenu, cmdShow, 0)

	// Separator
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)

	// 2. Status: Connected / Disconnected
	t.mu.RLock()
	isConnected := t.connected
	t.mu.RUnlock()

	var statusTextStr string
	var disconnectFlags uintptr = MF_STRING
	if isConnected {
		statusTextStr = "Status: Connected"
	} else {
		statusTextStr = "Status: Disconnected"
		disconnectFlags = MF_GRAYED | MF_DISABLED
	}
	statusText, _ := windows.UTF16PtrFromString(statusTextStr)
	procAppendMenuW.Call(hMenu, MF_GRAYED|MF_DISABLED, cmdStatus, uintptr(unsafe.Pointer(statusText)))

	// 3. Disconnect
	disconnText, _ := windows.UTF16PtrFromString("Disconnect")
	procAppendMenuW.Call(hMenu, disconnectFlags, cmdDisconnect, uintptr(unsafe.Pointer(disconnText)))

	// Separator
	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)

	// 4. Exit Conective
	exitText, _ := windows.UTF16PtrFromString("Exit Conective")
	procAppendMenuW.Call(hMenu, MF_STRING, cmdExit, uintptr(unsafe.Pointer(exitText)))

	// Track Menu
	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWindow.Call(t.hwnd)

	cmd, _, _ := procTrackPopupMenuEx.Call(
		hMenu,
		TPM_RETURNCMD|TPM_RIGHTBUTTON|TPM_BOTTOMALIGN,
		uintptr(pt.X),
		uintptr(pt.Y),
		t.hwnd,
		0,
	)
	procPostMessageW.Call(t.hwnd, WM_NULL, 0, 0)

	switch cmd {
	case cmdShow:
		t.ShowWindow()
		if t.opts.OnShow != nil {
			t.opts.OnShow()
		}
	case cmdDisconnect:
		if t.opts.OnDisconnect != nil {
			t.opts.OnDisconnect()
		}
	case cmdExit:
		t.ExitApp()
	}
}

func (t *Tray) notifyFirstMinimize() {
	t.balloonOnce.Do(func() {
		t.ShowBalloon(
			"Conective",
			"Conective is still running in the system tray. Click the tray icon to restore the window.",
		)
	})
}

func (t *Tray) addTrayIcon() bool {
	t.mu.RLock()
	hwnd := t.hwnd
	hIcon := t.hIcon
	tip := t.tooltip
	t.mu.RUnlock()

	if hwnd == 0 {
		return false
	}

	nid := NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             windows.Handle(hwnd),
		UID:              1,
		UFlags:           NIF_MESSAGE | NIF_ICON | NIF_TIP,
		UCallbackMessage: WM_TRAYNOTIFY,
		HIcon:            hIcon,
	}
	copyUTF16(nid.SzTip[:], tip)

	ok, _ := shellNotifyIcon(NIM_ADD, &nid)
	return ok
}

// trayWndProc handles window messages for the tray hidden window.
func trayWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	val, ok := attachedWindows.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	t := val.(*Tray)

	if taskbarCreatedMsg != 0 && msg == taskbarCreatedMsg {
		t.addTrayIcon()
		return 0
	}

	switch msg {
	case WM_TRAYNOTIFY:
		switch lParam {
		case WM_LBUTTONUP, WM_LBUTTONDBLCLK:
			// Left Click: Restore and focus window
			t.ShowWindow()
			if t.opts.OnShow != nil {
				t.opts.OnShow()
			}
		case WM_RBUTTONUP, WM_CONTEXTMENU:
			// Right Click: Open Context Menu
			t.showContextMenu()
		}
		return 0

	case WM_CLOSE:
		procPostQuitMessage.Call(0)
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

// targetSubclassWndProc handles window messages for the subclassed main application window.
func targetSubclassWndProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	val, ok := attachedWindows.Load(hwnd)
	if !ok {
		r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
		return r
	}
	t := val.(*Tray)

	if msg == WM_CLOSE {
		t.mu.RLock()
		isExiting := t.isExiting
		minEnabled := t.isMinimizeEnabled == nil || t.isMinimizeEnabled()
		t.mu.RUnlock()

		if !isExiting && minEnabled {
			// Minimize to tray: hide window instead of closing
			procShowWindow.Call(hwnd, SW_HIDE)
			t.notifyFirstMinimize()
			return 0
		}
	}

	if t.origTargetWndProc != 0 {
		return callWindowProc(t.origTargetWndProc, hwnd, uint32(msg), wParam, lParam)
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}
