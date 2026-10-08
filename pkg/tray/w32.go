//go:build windows
// +build windows

package tray

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetModuleHandleW         = kernel32.NewProc("GetModuleHandleW")
	procGetCurrentThreadId       = kernel32.NewProc("GetCurrentThreadId")

	procRegisterClassExW         = user32.NewProc("RegisterClassExW")
	procUnregisterClassW         = user32.NewProc("UnregisterClassW")
	procCreateWindowExW          = user32.NewProc("CreateWindowExW")
	procDestroyWindow            = user32.NewProc("DestroyWindow")
	procShowWindow               = user32.NewProc("ShowWindow")
	procSetForegroundWindow      = user32.NewProc("SetForegroundWindow")
	procGetCursorPos             = user32.NewProc("GetCursorPos")
	procPostMessageW             = user32.NewProc("PostMessageW")
	procPostQuitMessage          = user32.NewProc("PostQuitMessage")
	procGetMessageW              = user32.NewProc("GetMessageW")
	procTranslateMessage         = user32.NewProc("TranslateMessage")
	procDispatchMessageW         = user32.NewProc("DispatchMessageW")
	procDefWindowProcW           = user32.NewProc("DefWindowProcW")
	procSendMessageW             = user32.NewProc("SendMessageW")
	procLoadImageW               = user32.NewProc("LoadImageW")
	procLoadIconW                = user32.NewProc("LoadIconW")
	procDestroyIcon              = user32.NewProc("DestroyIcon")
	procCreateIconFromResourceEx = user32.NewProc("CreateIconFromResourceEx")
	procCreatePopupMenu          = user32.NewProc("CreatePopupMenu")
	procAppendMenuW              = user32.NewProc("AppendMenuW")
	procTrackPopupMenuEx         = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu              = user32.NewProc("DestroyMenu")
	procSetMenuDefaultItem       = user32.NewProc("SetMenuDefaultItem")
	procGetSystemMetrics         = user32.NewProc("GetSystemMetrics")
	procSetWindowLongPtrW        = user32.NewProc("SetWindowLongPtrW")
	procSetWindowLongW           = user32.NewProc("SetWindowLongW")
	procCallWindowProcW          = user32.NewProc("CallWindowProcW")
	procIsWindow                 = user32.NewProc("IsWindow")
	procIsWindowVisible          = user32.NewProc("IsWindowVisible")
	procOpenDesktopW             = user32.NewProc("OpenDesktopW")
	procSetThreadDesktop         = user32.NewProc("SetThreadDesktop")
	procCloseDesktop             = user32.NewProc("CloseDesktop")
	procRegisterWindowMessageW   = user32.NewProc("RegisterWindowMessageW")

	procShell_NotifyIconW        = shell32.NewProc("Shell_NotifyIconW")
)

const (
	WM_NULL          = 0x0000
	WM_DESTROY       = 0x0002
	WM_CLOSE         = 0x0010
	WM_QUIT          = 0x0012
	WM_COMMAND       = 0x0111
	WM_LBUTTONUP     = 0x0202
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP     = 0x0205
	WM_CONTEXTMENU   = 0x007B
	WM_SETICON       = 0x0080
	ICON_SMALL       = 0
	ICON_BIG         = 1
	WM_USER          = 0x0400
	WM_APP           = 0x8000

	// Tray custom notification message
	WM_TRAYNOTIFY    = WM_USER + 101

	// Shell_NotifyIcon messages
	NIM_ADD          = 0x00000000
	NIM_MODIFY       = 0x00000001
	NIM_DELETE       = 0x00000002
	NIM_SETVERSION   = 0x00000004

	// Shell_NotifyIcon flags
	NIF_MESSAGE      = 0x00000001
	NIF_ICON         = 0x00000002
	NIF_TIP          = 0x00000004
	NIF_STATE        = 0x00000008
	NIF_INFO         = 0x00000010
	NIF_GUID         = 0x00000020
	NIF_SHOWTIP      = 0x00000080

	// Balloon notification flags
	NIIF_NONE        = 0x00000000
	NIIF_INFO        = 0x00000001
	NIIF_WARNING     = 0x00000002
	NIIF_ERROR       = 0x00000003

	// Menu flags
	MF_STRING        = 0x00000000
	MF_GRAYED        = 0x00000001
	MF_DISABLED      = 0x00000002
	MF_CHECKED       = 0x00000008
	MF_SEPARATOR     = 0x00000800

	// TrackPopupMenu flags
	TPM_LEFTALIGN    = 0x0000
	TPM_RIGHTBUTTON  = 0x0002
	TPM_BOTTOMALIGN  = 0x0020
	TPM_NONOTIFY     = 0x0080
	TPM_RETURNCMD    = 0x0100

	// ShowWindow commands
	SW_HIDE          = 0
	SW_NORMAL        = 1
	SW_SHOWMINIMIZED = 2
	SW_MAXIMIZE      = 3
	SW_SHOW          = 5
	SW_MINIMIZE      = 6
	SW_RESTORE       = 9

	// System metrics
	SM_CXSMICON      = 49
	SM_CYSMICON      = 50
	SM_CXICON        = 11
	SM_CYICON        = 12

	// LoadImage types and flags
	IMAGE_ICON       = 1
	LR_LOADFROMFILE  = 0x00000010
	LR_DEFAULTSIZE   = 0x00000040
	LR_SHARED        = 0x00008000

	// Window Long
	GWLP_WNDPROC     = -4
	GWL_WNDPROC      = -4
)

// WNDCLASSEXW represents the Win32 window class structure.
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     windows.Handle
	HIcon         windows.Handle
	HCursor       windows.Handle
	HbrBackground windows.Handle
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       windows.Handle
}

// POINT represents a 2D integer coordinate.
type POINT struct {
	X int32
	Y int32
}

// MSG represents a Windows message.
type MSG struct {
	HWnd    windows.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

// NOTIFYICONDATAW represents the Shell_NotifyIcon parameters.
type NOTIFYICONDATAW struct {
	CbSize            uint32
	HWnd              windows.Handle
	UID               uint32
	UFlags            uint32
	UCallbackMessage  uint32
	HIcon             windows.Handle
	SzTip             [128]uint16
	DwState           uint32
	DwStateMask       uint32
	SzInfo            [256]uint16
	UTimeoutOrVersion uint32
	SzInfoTitle       [64]uint16
	DwInfoFlags       uint32
	GuidItem          windows.GUID
	HBalloonIcon      windows.Handle
}

func shellNotifyIcon(dwMessage uint32, data *NOTIFYICONDATAW) (bool, error) {
	data.CbSize = uint32(unsafe.Sizeof(*data))
	r, _, err := procShell_NotifyIconW.Call(uintptr(dwMessage), uintptr(unsafe.Pointer(data)))
	if r == 0 {
		return false, err
	}
	return true, nil
}

func setWindowLongPtr(hwnd uintptr, index int, newLong uintptr) uintptr {
	if procSetWindowLongPtrW.Find() == nil {
		ret, _, _ := procSetWindowLongPtrW.Call(hwnd, uintptr(index), newLong)
		return ret
	}
	ret, _, _ := procSetWindowLongW.Call(hwnd, uintptr(index), newLong)
	return ret
}

func callWindowProc(prevWndProc uintptr, hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	ret, _, _ := procCallWindowProcW.Call(prevWndProc, hwnd, uintptr(msg), wParam, lParam)
	return ret
}

func copyUTF16(dst []uint16, src string) {
	u16 := windows.StringToUTF16(src)
	copy(dst, u16)
	if len(u16) < len(dst) {
		dst[len(u16)] = 0
	} else if len(dst) > 0 {
		dst[len(dst)-1] = 0
	}
}

// loadIcon loads an HICON from raw bytes, falling back to temp file, module resource, or default.
func loadIcon(iconBytes []byte) windows.Handle {
	cx, _, _ := procGetSystemMetrics.Call(SM_CXSMICON)
	cy, _, _ := procGetSystemMetrics.Call(SM_CYSMICON)
	if cx == 0 {
		cx = 16
	}
	if cy == 0 {
		cy = 16
	}

	// 1. Try parsing ICO directory and creating icon from resource
	if len(iconBytes) > 22 {
		hIcon := loadIconFromICOBytes(iconBytes, int(cx), int(cy))
		if hIcon != 0 {
			return hIcon
		}
	}

	// 2. Try writing to temp file and loading via LoadImageW
	if len(iconBytes) > 0 {
		tempDir := os.TempDir()
		tempIco := filepath.Join(tempDir, "conective_app_tray.ico")
		if err := os.WriteFile(tempIco, iconBytes, 0644); err == nil {
			pathPtr := windows.StringToUTF16Ptr(tempIco)
			r, _, _ := procLoadImageW.Call(
				0,
				uintptr(unsafe.Pointer(pathPtr)),
				IMAGE_ICON,
				cx,
				cy,
				LR_LOADFROMFILE|LR_DEFAULTSIZE,
			)
			if r != 0 {
				return windows.Handle(r)
			}
		}
	}

	// 3. Try loading icon resource ID 1 from current executable
	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst != 0 {
		r, _, _ := procLoadImageW.Call(
			hInst,
			1,
			IMAGE_ICON,
			cx,
			cy,
			LR_SHARED,
		)
		if r != 0 {
			return windows.Handle(r)
		}
	}

	// 4. Default application icon fallback (IDI_APPLICATION = 32512)
	r, _, _ := procLoadIconW.Call(0, 32512)
	return windows.Handle(r)
}

// icoHeader structure
type icoHeader struct {
	Reserved uint16
	Type     uint16
	Count    uint16
}

// icoDirEntry structure
type icoDirEntry struct {
	Width       byte
	Height      byte
	ColorCount  byte
	Reserved    byte
	Planes      uint16
	BitCount    uint16
	BytesInRes  uint32
	ImageOffset uint32
}

func loadIconFromICOBytes(data []byte, targetWidth, targetHeight int) windows.Handle {
	if len(data) < 6 {
		return 0
	}

	header := icoHeader{
		Reserved: binary.LittleEndian.Uint16(data[0:2]),
		Type:     binary.LittleEndian.Uint16(data[2:4]),
		Count:    binary.LittleEndian.Uint16(data[4:6]),
	}
	if header.Reserved != 0 || header.Type != 1 || header.Count == 0 {
		return 0
	}

	bestIdx := -1
	bestDiff := 999999
	entryOffset := 6

	for i := 0; i < int(header.Count); i++ {
		if entryOffset+16 > len(data) {
			break
		}
		entry := icoDirEntry{
			Width:       data[entryOffset],
			Height:      data[entryOffset+1],
			ColorCount:  data[entryOffset+2],
			Reserved:    data[entryOffset+3],
			Planes:      binary.LittleEndian.Uint16(data[entryOffset+4 : entryOffset+6]),
			BitCount:    binary.LittleEndian.Uint16(data[entryOffset+6 : entryOffset+8]),
			BytesInRes:  binary.LittleEndian.Uint32(data[entryOffset+8 : entryOffset+12]),
			ImageOffset: binary.LittleEndian.Uint32(data[entryOffset+12 : entryOffset+16]),
		}

		w := int(entry.Width)
		if w == 0 {
			w = 256
		}
		diff := w - targetWidth
		if diff < 0 {
			diff = -diff
		}
		if diff < bestDiff {
			bestDiff = diff
			bestIdx = i
		}

		entryOffset += 16
	}

	if bestIdx < 0 {
		return 0
	}

	offset := 6 + bestIdx*16
	bytesInRes := binary.LittleEndian.Uint32(data[offset+8 : offset+12])
	imgOffset := binary.LittleEndian.Uint32(data[offset+12 : offset+16])

	if int(imgOffset+bytesInRes) > len(data) {
		return 0
	}

	resData := data[imgOffset : imgOffset+bytesInRes]
	r, _, _ := procCreateIconFromResourceEx.Call(
		uintptr(unsafe.Pointer(&resData[0])),
		uintptr(bytesInRes),
		1,          // TRUE (is icon)
		0x00030000, // dwVersion
		uintptr(targetWidth),
		uintptr(targetHeight),
		0,
	)
	return windows.Handle(r)
}

// SetWindowIcon applies the provided icon bytes as both the small and large window title bar icons.
func SetWindowIcon(hwnd uintptr, iconBytes []byte) error {
	if hwnd == 0 || len(iconBytes) == 0 {
		return nil
	}

	cxSmall, _, _ := procGetSystemMetrics.Call(SM_CXSMICON)
	cySmall, _, _ := procGetSystemMetrics.Call(SM_CYSMICON)
	cxBig, _, _ := procGetSystemMetrics.Call(SM_CXICON)
	cyBig, _, _ := procGetSystemMetrics.Call(SM_CYICON)

	if cxSmall == 0 {
		cxSmall = 16
	}
	if cySmall == 0 {
		cySmall = 16
	}
	if cxBig == 0 {
		cxBig = 32
	}
	if cyBig == 0 {
		cyBig = 32
	}

	hIconSmall := loadIconFromICOBytes(iconBytes, int(cxSmall), int(cySmall))
	if hIconSmall == 0 {
		hIconSmall = loadIcon(iconBytes)
	}

	hIconBig := loadIconFromICOBytes(iconBytes, int(cxBig), int(cyBig))
	if hIconBig == 0 {
		hIconBig = loadIcon(iconBytes)
	}

	if hIconSmall != 0 {
		procSendMessageW.Call(hwnd, 0x0080 /* WM_SETICON */, 0 /* ICON_SMALL */, uintptr(hIconSmall))
	}
	if hIconBig != 0 {
		procSendMessageW.Call(hwnd, 0x0080 /* WM_SETICON */, 1 /* ICON_BIG */, uintptr(hIconBig))
	}

	return nil
}

