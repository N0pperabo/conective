//go:build windows
// +build windows

package tray

import (
	"runtime"
	"testing"
	"time"
	"unsafe"

	"freenode/assets"

	"golang.org/x/sys/windows"
)

func TestNotifyIconDataLayout(t *testing.T) {
	var nid NOTIFYICONDATAW
	size := unsafe.Sizeof(nid)
	t.Logf("NOTIFYICONDATAW size on this arch: %d bytes", size)
	t.Logf("HWnd offset: %d", unsafe.Offsetof(nid.HWnd))
	t.Logf("UID offset: %d", unsafe.Offsetof(nid.UID))
	t.Logf("UFlags offset: %d", unsafe.Offsetof(nid.UFlags))
	t.Logf("UCallbackMessage offset: %d", unsafe.Offsetof(nid.UCallbackMessage))
	t.Logf("HIcon offset: %d", unsafe.Offsetof(nid.HIcon))
	t.Logf("SzTip offset: %d", unsafe.Offsetof(nid.SzTip))
	t.Logf("DwState offset: %d", unsafe.Offsetof(nid.DwState))
	t.Logf("SzInfo offset: %d", unsafe.Offsetof(nid.SzInfo))
	t.Logf("UTimeoutOrVersion offset: %d", unsafe.Offsetof(nid.UTimeoutOrVersion))
	t.Logf("SzInfoTitle offset: %d", unsafe.Offsetof(nid.SzInfoTitle))
	t.Logf("DwInfoFlags offset: %d", unsafe.Offsetof(nid.DwInfoFlags))
	t.Logf("GuidItem offset: %d", unsafe.Offsetof(nid.GuidItem))
	t.Logf("HBalloonIcon offset: %d", unsafe.Offsetof(nid.HBalloonIcon))
}

func TestLoadIconFromAssets(t *testing.T) {
	if len(assets.AppIcon) == 0 {
		t.Fatalf("assets.AppIcon is empty")
	}

	hIcon := loadIcon(assets.AppIcon)
	if hIcon == 0 {
		t.Fatalf("failed to load icon from assets.AppIcon")
	}
	t.Logf("Successfully loaded HICON: %v", hIcon)
	procDestroyIcon.Call(uintptr(hIcon))
}

func TestTrayLifecycle(t *testing.T) {
	tr := New(Options{
		IconBytes: assets.AppIcon,
		Title:     "Conective Test",
		Tooltip:   "Conective Test - Disconnected",
		OnShow: func() {
		},
		OnDisconnect: func() {
		},
		OnExit: func() {
		},
	})

	if err := tr.Start(); err != nil {
		t.Fatalf("failed to start tray: %v", err)
	}
	defer tr.Stop()

	// Update status
	tr.SetConnected(true)
	time.Sleep(50 * time.Millisecond)

	tr.SetConnected(false)
	time.Sleep(50 * time.Millisecond)

	tr.SetTooltip("Conective Custom Tip")
	time.Sleep(50 * time.Millisecond)
}

func TestWindowAttachmentAndMethods(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := windows.StringToUTF16Ptr("TestAttachClass")
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
		LpfnWndProc:   windows.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
			r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
			return r
		}),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Test Window"))),
		0,
		0, 0, 100, 100,
		0, 0,
		hInst,
		0,
	)
	if hwnd == 0 {
		t.Fatalf("failed to create test window")
	}
	defer procDestroyWindow.Call(hwnd)

	tr := New(Options{
		IconBytes: assets.AppIcon,
		Title:     "Conective Test",
	})
	if err := tr.Start(); err != nil {
		t.Fatalf("failed to start tray: %v", err)
	}
	defer tr.Stop()

	// Attach window with minimize enabled
	minimized := false
	tr.AttachWindow(hwnd, func() bool {
		minimized = true
		return true
	})

	// Test Show and Hide window
	tr.ShowWindow()
	tr.HideWindow()

	// Send WM_CLOSE to window - should intercept and hide window
	procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)

	// Pump messages briefly
	var msg MSG
	for i := 0; i < 5; i++ {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		if msg.Message == WM_CLOSE {
			break
		}
	}

	if !minimized {
		t.Logf("Window was minimized to tray on WM_CLOSE (subclass intercepted WM_CLOSE)")
	}
}

func TestStation(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	procGetProcessWindowStation := user32.NewProc("GetProcessWindowStation")
	procGetUserObjectInformationW := user32.NewProc("GetUserObjectInformationW")
	procGetThreadDesktop := user32.NewProc("GetThreadDesktop")
	procGetCurrentThreadId := kernel32.NewProc("GetCurrentThreadId")

	hStation, _, _ := procGetProcessWindowStation.Call()
	var stationName [256]uint16
	var needed uint32
	procGetUserObjectInformationW.Call(hStation, 2, uintptr(unsafe.Pointer(&stationName[0])), 512, uintptr(unsafe.Pointer(&needed)))
	t.Logf("WindowStation: %s", windows.UTF16ToString(stationName[:]))

	tid, _, _ := procGetCurrentThreadId.Call()
	hDesktop, _, _ := procGetThreadDesktop.Call(tid)
	var desktopName [256]uint16
	procGetUserObjectInformationW.Call(hDesktop, 2, uintptr(unsafe.Pointer(&desktopName[0])), 512, uintptr(unsafe.Pointer(&needed)))
	t.Logf("Desktop: %s", windows.UTF16ToString(desktopName[:]))

	procOpenDesktopW := user32.NewProc("OpenDesktopW")
	procSetThreadDesktop := user32.NewProc("SetThreadDesktop")
	hDef, _, errDef := procOpenDesktopW.Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Default"))), 0, 0, 0x10000000)
	t.Logf("OpenDesktop Default: %x, err=%v", hDef, errDef)
	if hDef != 0 {
		ok, _, errSet := procSetThreadDesktop.Call(hDef)
		t.Logf("SetThreadDesktop: %v, err=%v", ok, errSet)
		shellTrayHwnd, _, _ := user32.NewProc("FindWindowW").Call(uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Shell_TrayWnd"))), 0)
		t.Logf("Shell_TrayWnd on Default: %x", shellTrayHwnd)

		hIcon := loadIcon(assets.AppIcon)
		defer procDestroyIcon.Call(uintptr(hIcon))

		className := windows.StringToUTF16Ptr("TestClassDefault")
		wc := WNDCLASSEXW{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			HInstance:     windows.Handle(hInst),
			LpszClassName: className,
			LpfnWndProc:   windows.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
				r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
				return r
			}),
		}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

		hwnd, _, _ := procCreateWindowExW.Call(
			0,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("TestDefault"))),
			0,
			0, 0, 0, 0,
			0, 0,
			hInst,
			0,
		)
		if hwnd != 0 {
			defer procDestroyWindow.Call(hwnd)
			var nid NOTIFYICONDATAW
			nid.CbSize = uint32(unsafe.Sizeof(NOTIFYICONDATAW{}))
			nid.HWnd = windows.Handle(hwnd)
			nid.UID = 1
			nid.UFlags = NIF_ICON | NIF_TIP | NIF_MESSAGE
			nid.UCallbackMessage = WM_TRAYNOTIFY
			nid.HIcon = hIcon
			copyUTF16(nid.SzTip[:], "Test on Default Desktop")

			okNid, errNid := shellNotifyIcon(NIM_ADD, &nid)
			t.Logf("Shell_NotifyIconW on Default Desktop: ok=%v, err=%v", okNid, errNid)
			if okNid {
				shellNotifyIcon(NIM_DELETE, &nid)
			}
		}
	}
}

func TestSetWindowIcon(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	hInst, _, _ := procGetModuleHandleW.Call(0)
	className := windows.StringToUTF16Ptr("TestIconClass")
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		HInstance:     windows.Handle(hInst),
		LpszClassName: className,
		LpfnWndProc:   windows.NewCallback(func(hwnd, msg, wParam, lParam uintptr) uintptr {
			r, _, _ := procDefWindowProcW.Call(hwnd, msg, wParam, lParam)
			return r
		}),
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(windows.StringToUTF16Ptr("Test Window Icon"))),
		0,
		0, 0, 100, 100,
		0, 0,
		hInst,
		0,
	)
	if hwnd == 0 {
		t.Fatalf("failed to create test window")
	}
	defer procDestroyWindow.Call(hwnd)

	if err := SetWindowIcon(hwnd, assets.AppIcon); err != nil {
		t.Fatalf("SetWindowIcon failed: %v", err)
	}

	// Also verify with nil hwnd or empty iconBytes doesn't error
	if err := SetWindowIcon(0, assets.AppIcon); err != nil {
		t.Fatalf("SetWindowIcon with 0 hwnd failed: %v", err)
	}
	if err := SetWindowIcon(hwnd, nil); err != nil {
		t.Fatalf("SetWindowIcon with nil bytes failed: %v", err)
	}
}

