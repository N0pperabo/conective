//go:build windows

package procutil

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

// AppInfo represents an application detected on the system.
type AppInfo struct {
	Name    string `json:"name"`
	ExeName string `json:"exe_name"`
	Title   string `json:"title"`
	Path    string `json:"path"`
}

var (
	modUser32    = syscall.NewLazyDLL("user32.dll")
	modKernel32  = syscall.NewLazyDLL("kernel32.dll")
	modDwmapi    = syscall.NewLazyDLL("dwmapi.dll")
	modVersion   = syscall.NewLazyDLL("version.dll")

	procEnumWindows             = modUser32.NewProc("EnumWindows")
	procEnumDesktopWindows      = modUser32.NewProc("EnumDesktopWindows")
	procOpenWindowStationW      = modUser32.NewProc("OpenWindowStationW")
	procSetProcessWindowStation = modUser32.NewProc("SetProcessWindowStation")
	procOpenDesktopW            = modUser32.NewProc("OpenDesktopW")
	procCloseDesktop            = modUser32.NewProc("CloseDesktop")
	procIsWindowVisible         = modUser32.NewProc("IsWindowVisible")
	procGetWindowTextW          = modUser32.NewProc("GetWindowTextW")
	procGetWindowTextLengthW    = modUser32.NewProc("GetWindowTextLengthW")
	procGetWindowThreadProcessId= modUser32.NewProc("GetWindowThreadProcessId")
	procGetWindowLongW          = modUser32.NewProc("GetWindowLongW")
	procOpenProcess             = modKernel32.NewProc("OpenProcess")
	procCloseHandle             = modKernel32.NewProc("CloseHandle")
	procQueryFullProcessImageName = modKernel32.NewProc("QueryFullProcessImageNameW")
	procDwmGetWindowAttribute   = modDwmapi.NewProc("DwmGetWindowAttribute")

	procGetFileVersionInfoSizeW = modVersion.NewProc("GetFileVersionInfoSizeW")
	procGetFileVersionInfoW     = modVersion.NewProc("GetFileVersionInfoW")
	procVerQueryValueW          = modVersion.NewProc("VerQueryValueW")
)

const (
	gwlExStyle                        = 0xFFFFFFEC // -20 in two's complement uint32
	wsExToolWindow                    = 0x00000080
	wsExAppWindow                     = 0x00040000
	dwmwaCloaked                      = 14
	processQueryLimitedInformation    = 0x1000
	processQueryInformation           = 0x0400
	processVMRead                     = 0x0010
)

// Filter out background Windows system processes that shouldn't appear in split-tunnel app picker
var ignoredSystemProcesses = map[string]bool{
	"svchost.exe":                 true,
	"explorer.exe":                true,
	"conhost.exe":                 true,
	"runtimebroker.exe":           true,
	"applicationframehost.exe":    true,
	"searchapp.exe":               true,
	"searchhost.exe":              true,
	"shellexperiencehost.exe":     true,
	"startmenuexperiencehost.exe": true,
	"taskmgr.exe":                 true,
	"dwm.exe":                     true,
	"ctfmon.exe":                  true,
	"fontdrvhost.exe":             true,
	"systemsettings.exe":          true,
	"textinputhost.exe":           true,
	"lockapp.exe":                 true,
	"widgetservice.exe":           true,
	"widgets.exe":                 true,
	"securityhealthsystray.exe":   true,
	"securityhealthhost.exe":      true,
	"smartscreen.exe":             true,
	"csrss.exe":                   true,
	"winlogon.exe":                true,
	"wininit.exe":                 true,
	"services.exe":                true,
	"lsass.exe":                   true,
	"spoolsv.exe":                 true,
	"dllhost.exe":                 true,
	"smss.exe":                    true,
	"compattelrunner.exe":         true,
	"backgroundtaskhost.exe":      true,
	"audiodg.exe":                 true,
	"wudfhost.exe":                true,
	"sihost.exe":                  true,
	"freenode.exe":                true,
	"conective.exe":               true,
	"wv2ray.exe":                  true,
	"xray.exe":                    true,
	"cmd.exe":                     true,
	"wscript.exe":                 true,
	"cscript.exe":                 true,
}

var ignoredTitles = map[string]bool{
	"program manager":                  true,
	"windows input experience":         true,
	"settings":                         true,
	"desktopwindowxamlsource":          true,
	"microsoft text input application": true,
	"msctfime ui":                      true,
	"default ime":                      true,
}

func isIgnoredTitle(title string) bool {
	t := strings.ToLower(strings.TrimSpace(title))
	if t == "" || ignoredTitles[t] {
		return true
	}
	if strings.HasPrefix(t, "gdi+ window") ||
		strings.HasPrefix(t, ".net-broadcasteventwindow") ||
		strings.HasPrefix(t, "qtrayicon") ||
		strings.HasPrefix(t, "dde server") ||
		strings.HasPrefix(t, "task host window") ||
		strings.HasPrefix(t, "systemresource") ||
		strings.HasPrefix(t, "ms_webcheck") ||
		strings.HasPrefix(t, "h.notifyicon") ||
		strings.HasPrefix(t, "nvcontainer") ||
		strings.HasPrefix(t, "bluetoothnotification") {
		return true
	}
	return false
}

// GetRunningApps enumerates running GUI applications with visible windows using Windows API syscalls.
func GetRunningApps() ([]AppInfo, error) {
	currentPID := uint32(os.Getpid())
	seenProcesses := make(map[string]bool)
	var apps []AppInfo

	cb := syscall.NewCallback(func(hwnd uintptr, lParam uintptr) uintptr {
		// 1. Check window visibility
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}

		// 2. Check window title length
		textLen, _, _ := procGetWindowTextLengthW.Call(hwnd)
		if textLen == 0 {
			return 1
		}

		// 3. Skip Tool Windows (unless they explicitly have WS_EX_APPWINDOW)
		exStyle, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
		if (exStyle&wsExToolWindow) != 0 && (exStyle&wsExAppWindow) == 0 {
			return 1
		}

		// 4. Skip cloaked windows (hidden virtual desktop / suspended UWP windows)
		if procDwmGetWindowAttribute.Find() == nil {
			var cloaked uint32
			r, _, _ := procDwmGetWindowAttribute.Call(
				hwnd,
				uintptr(dwmwaCloaked),
				uintptr(unsafe.Pointer(&cloaked)),
				unsafe.Sizeof(cloaked),
			)
			if r == 0 && cloaked != 0 {
				return 1
			}
		}

		// 5. Read window title
		titleBuf := make([]uint16, textLen+1)
		actualLen, _, _ := procGetWindowTextW.Call(
			hwnd,
			uintptr(unsafe.Pointer(&titleBuf[0])),
			uintptr(len(titleBuf)),
		)
		if actualLen == 0 {
			return 1
		}
		title := syscall.UTF16ToString(titleBuf[:actualLen])
		title = strings.TrimSpace(title)
		if isIgnoredTitle(title) {
			return 1
		}

		// 6. Get window process ID
		var pid uint32
		procGetWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		if pid == 0 || pid == currentPID {
			return 1
		}

		// 7. Open process to query image path
		hProcess, _, _ := procOpenProcess.Call(
			uintptr(processQueryLimitedInformation),
			0,
			uintptr(pid),
		)
		if hProcess == 0 {
			hProcess, _, _ = procOpenProcess.Call(
				uintptr(processQueryInformation|processVMRead),
				0,
				uintptr(pid),
			)
		}
		if hProcess == 0 {
			return 1
		}
		defer procCloseHandle.Call(hProcess)

		pathBuf := make([]uint16, 1024)
		pathSize := uint32(len(pathBuf))
		qRes, _, _ := procQueryFullProcessImageName.Call(
			hProcess,
			0,
			uintptr(unsafe.Pointer(&pathBuf[0])),
			uintptr(unsafe.Pointer(&pathSize)),
		)
		if qRes == 0 || pathSize == 0 {
			return 1
		}

		fullPath := syscall.UTF16ToString(pathBuf[:pathSize])
		exeName := filepath.Base(fullPath)
		lowerExe := strings.ToLower(exeName)

		// Filter out system processes
		if ignoredSystemProcesses[lowerExe] {
			return 1
		}
		lowerPath := strings.ToLower(fullPath)
		if strings.Contains(lowerPath, "\\windows\\systemapps\\") ||
			strings.Contains(lowerPath, "\\windows\\system32\\") {
			return 1
		}

		// Deduplicate apps by ExeName
		if seenProcesses[lowerExe] {
			return 1
		}
		seenProcesses[lowerExe] = true

		// Friendly name resolution: try FileDescription from version resource, else fallback to exe
		appName := getFileDescription(fullPath)
		if appName == "" {
			appName = cleanNameFromExe(exeName)
		}

		apps = append(apps, AppInfo{
			Name:    appName,
			ExeName: exeName,
			Title:   title,
			Path:    fullPath,
		})

		return 1
	})

	// First try EnumWindows (primary standard Windows API)
	procEnumWindows.Call(cb, 0)

	// If no windows enumerated (e.g. executing in background agent/service desktop),
	// connect to WinSta0\Default and enumerate desktop windows.
	if len(apps) == 0 {
		winstaName, _ := syscall.UTF16PtrFromString("WinSta0")
		hwinsta, _, _ := procOpenWindowStationW.Call(uintptr(unsafe.Pointer(winstaName)), 0, 0x037F)
		if hwinsta != 0 {
			procSetProcessWindowStation.Call(hwinsta)
		}
		deskName, _ := syscall.UTF16PtrFromString("Default")
		hdesk, _, _ := procOpenDesktopW.Call(uintptr(unsafe.Pointer(deskName)), 0, 0, 0x0001|0x0040)
		if hdesk != 0 {
			defer procCloseDesktop.Call(hdesk)
			procEnumDesktopWindows.Call(hdesk, cb, 0)
		}
	}

	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})

	return apps, nil
}

var (
	installedCacheMu sync.RWMutex
	installedCache   []AppInfo
	installedCacheAt time.Time
)

// GetInstalledApps scans common installed applications from Windows Registry and Program Files.
func GetInstalledApps() ([]AppInfo, error) {
	installedCacheMu.RLock()
	if time.Since(installedCacheAt) < 60*time.Second && len(installedCache) > 0 {
		defer installedCacheMu.RUnlock()
		res := make([]AppInfo, len(installedCache))
		copy(res, installedCache)
		return res, nil
	}
	installedCacheMu.RUnlock()

	seenExes := make(map[string]bool)
	var apps []AppInfo

	addApp := func(name, fullPath string) {
		if fullPath == "" {
			return
		}
		// Strip quotes or parameters
		fullPath = strings.Trim(fullPath, "\"")
		if idx := strings.Index(strings.ToLower(fullPath), ".exe"); idx != -1 {
			fullPath = fullPath[:idx+4]
		}
		exeName := filepath.Base(fullPath)
		lowerExe := strings.ToLower(exeName)

		// Filter out uninstallers, helpers, updaters, and system exes
		if ignoredSystemProcesses[lowerExe] ||
			strings.HasPrefix(lowerExe, "unins") ||
			strings.Contains(lowerExe, "uninstall") ||
			strings.Contains(lowerExe, "update_helper") ||
			strings.Contains(lowerExe, "crashreporter") ||
			strings.Contains(lowerExe, "notification") ||
			seenExes[lowerExe] {
			return
		}

		if _, err := os.Stat(fullPath); err != nil {
			return
		}

		seenExes[lowerExe] = true

		if name == "" {
			name = getFileDescription(fullPath)
		}
		if name == "" {
			name = cleanNameFromExe(exeName)
		}

		apps = append(apps, AppInfo{
			Name:    name,
			ExeName: exeName,
			Title:   name,
			Path:    fullPath,
		})
	}

	// 1. Scan App Paths registry keys (HKLM & HKCU)
	registryRoots := []registry.Key{registry.LOCAL_MACHINE, registry.CURRENT_USER}
	for _, root := range registryRoots {
		k, err := registry.OpenKey(root, `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths`, registry.READ)
		if err == nil {
			names, _ := k.ReadSubKeyNames(-1)
			for _, sub := range names {
				subKey, err := registry.OpenKey(k, sub, registry.READ)
				if err == nil {
					p, _, err := subKey.GetStringValue("")
					if err == nil && p != "" {
						addApp("", p)
					}
					subKey.Close()
				}
			}
			k.Close()
		}
	}

	// 2. Scan Uninstall registry keys (contains DisplayName and DisplayIcon / InstallLocation)
	uninstallPaths := []string{
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`,
		`SOFTWARE\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall`,
	}
	for _, root := range registryRoots {
		for _, uPath := range uninstallPaths {
			k, err := registry.OpenKey(root, uPath, registry.READ)
			if err != nil {
				continue
			}
			subKeys, _ := k.ReadSubKeyNames(-1)
			for _, sk := range subKeys {
				item, err := registry.OpenKey(k, sk, registry.READ)
				if err != nil {
					continue
				}
				dispName, _, _ := item.GetStringValue("DisplayName")
				dispIcon, _, _ := item.GetStringValue("DisplayIcon")
				installLoc, _, _ := item.GetStringValue("InstallLocation")
				item.Close()

				if dispIcon != "" && strings.Contains(strings.ToLower(dispIcon), ".exe") {
					addApp(dispName, dispIcon)
				} else if installLoc != "" {
					// Check for exe in install location (1 shallow level)
					entries, err := os.ReadDir(installLoc)
					if err == nil {
						for _, ent := range entries {
							if !ent.IsDir() && strings.HasSuffix(strings.ToLower(ent.Name()), ".exe") {
								addApp(dispName, filepath.Join(installLoc, ent.Name()))
								break
							}
						}
					}
				}
			}
			k.Close()
		}
	}

	// 3. Scan LocalAppData\Programs (Discord, Telegram, VS Code, etc.)
	localPrograms := filepath.Join(os.Getenv("LocalAppData"), "Programs")
	if localPrograms != "" {
		if entries, err := os.ReadDir(localPrograms); err == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					subDir := filepath.Join(localPrograms, ent.Name())
					if subEntries, err := os.ReadDir(subDir); err == nil {
						for _, se := range subEntries {
							if !se.IsDir() && strings.HasSuffix(strings.ToLower(se.Name()), ".exe") {
								addApp(ent.Name(), filepath.Join(subDir, se.Name()))
							}
						}
					}
				}
			}
		}
	}

	// 4. Scan Program Files & Program Files (x86) common app directories
	progRoots := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")}
	for _, pRoot := range progRoots {
		if pRoot == "" {
			continue
		}
		if entries, err := os.ReadDir(pRoot); err == nil {
			for _, ent := range entries {
				if ent.IsDir() {
					appDir := filepath.Join(pRoot, ent.Name())
					if subEntries, err := os.ReadDir(appDir); err == nil {
						for _, se := range subEntries {
							if !se.IsDir() && strings.EqualFold(filepath.Ext(se.Name()), ".exe") {
								addApp(ent.Name(), filepath.Join(appDir, se.Name()))
							}
						}
					}
				}
			}
		}
	}

	// 5. Scan Start Menu shortcuts (ProgramData & AppData)
	startMenuDirs := []string{
		filepath.Join(os.Getenv("ProgramData"), "Microsoft", "Windows", "Start Menu", "Programs"),
		filepath.Join(os.Getenv("AppData"), "Microsoft", "Windows", "Start Menu", "Programs"),
	}
	reExePath := regexp.MustCompile(`([A-Za-z]:\\[^"<>|\x00-\x1f]+\.exe)`)
	for _, sDir := range startMenuDirs {
		if sDir == "" {
			continue
		}
		_ = filepath.WalkDir(sDir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d == nil || d.IsDir() {
				return nil
			}
			if strings.EqualFold(filepath.Ext(d.Name()), ".lnk") {
				content, err := os.ReadFile(path)
				if err == nil && len(content) > 0 {
					matches := reExePath.FindSubmatch(content)
					if len(matches) > 1 {
						targetExe := string(matches[1])
						cleanShortcutName := strings.TrimSuffix(d.Name(), filepath.Ext(d.Name()))
						addApp(cleanShortcutName, targetExe)
					}
				}
			}
			return nil
		})
	}

	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})

	installedCacheMu.Lock()
	installedCache = apps
	installedCacheAt = time.Now()
	installedCacheMu.Unlock()

	return apps, nil
}

// GetAllApps returns a unified list of running and installed applications,
// prioritizing running applications first.
func GetAllApps() ([]AppInfo, error) {
	running, _ := GetRunningApps()
	installed, _ := GetInstalledApps()

	seen := make(map[string]bool)
	var result []AppInfo

	for _, a := range running {
		lower := strings.ToLower(a.ExeName)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, a)
		}
	}

	for _, a := range installed {
		lower := strings.ToLower(a.ExeName)
		if !seen[lower] {
			seen[lower] = true
			result = append(result, a)
		}
	}

	return result, nil
}

// getFileDescription reads the FileDescription string from the Windows Version resource.
func getFileDescription(path string) string {
	if procGetFileVersionInfoSizeW.Find() != nil ||
		procGetFileVersionInfoW.Find() != nil ||
		procVerQueryValueW.Find() != nil {
		return ""
	}

	pPath, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return ""
	}

	var handle uint32
	size, _, _ := procGetFileVersionInfoSizeW.Call(uintptr(unsafe.Pointer(pPath)), uintptr(unsafe.Pointer(&handle)))
	if size == 0 {
		return ""
	}

	buf := make([]byte, size)
	res, _, _ := procGetFileVersionInfoW.Call(
		uintptr(unsafe.Pointer(pPath)),
		0,
		size,
		uintptr(unsafe.Pointer(&buf[0])),
	)
	if res == 0 {
		return ""
	}

	subBlocks := []string{
		`\StringFileInfo\040904b0\FileDescription`,
		`\StringFileInfo\040904e4\FileDescription`,
		`\StringFileInfo\000004b0\FileDescription`,
		`\StringFileInfo\04090000\FileDescription`,
		`\StringFileInfo\040904E4\ProductName`,
		`\StringFileInfo\040904b0\ProductName`,
	}

	for _, sb := range subBlocks {
		pBlock, err := syscall.UTF16PtrFromString(sb)
		if err != nil {
			continue
		}
		var lpBuffer uintptr
		var puLen uint32
		qRes, _, _ := procVerQueryValueW.Call(
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(pBlock)),
			uintptr(unsafe.Pointer(&lpBuffer)),
			uintptr(unsafe.Pointer(&puLen)),
		)
		if qRes != 0 && puLen > 0 && lpBuffer != 0 {
			desc := syscall.UTF16ToString((*[512]uint16)(unsafe.Pointer(lpBuffer))[:puLen])
			desc = strings.TrimSpace(desc)
			if desc != "" {
				return desc
			}
		}
	}

	return ""
}

func cleanNameFromExe(exeName string) string {
	name := strings.TrimSuffix(exeName, filepath.Ext(exeName))
	name = strings.ReplaceAll(name, "_", " ")
	name = strings.ReplaceAll(name, "-", " ")
	parts := strings.Fields(name)
	for i, p := range parts {
		if len(p) > 0 {
			parts[i] = strings.ToUpper(p[:1]) + p[1:]
		}
	}
	res := strings.Join(parts, " ")
	if res == "" {
		return exeName
	}
	return res
}
