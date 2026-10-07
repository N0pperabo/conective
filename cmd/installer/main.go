package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows/registry"
)

//go:embed Conective.exe
var binConective []byte

//go:embed GeoLite2-Country.mmdb
var binGeoIP []byte

//go:embed wintun.dll
var binWintun []byte

//go:embed bin/aether.exe
var binAether []byte

//go:embed bin/psiphon-tunnel-core.exe
var binPsiphon []byte

var (
	modUser32     = syscall.NewLazyDLL("user32.dll")
	procMessageBox = modUser32.NewProc("MessageBoxW")
)

func msgBox(title, text string, style uint) {
	t, _ := syscall.UTF16PtrFromString(title)
	m, _ := syscall.UTF16PtrFromString(text)
	_, _, _ = procMessageBox.Call(0, uintptr(unsafe.Pointer(m)), uintptr(unsafe.Pointer(t)), uintptr(style))
}

func silentCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000, // CREATE_NO_WINDOW
	}
	return cmd
}

func main() {
	uninstall := flag.Bool("uninstall", false, "Uninstall Conective")
	silent := flag.Bool("silent", false, "Silent installation")
	flag.Parse()

	localAppData := os.Getenv("LOCALAPPDATA")
	if localAppData == "" {
		localAppData = os.Getenv("USERPROFILE")
	}

	installDir := filepath.Join(localAppData, "Programs", "Conective")
	targetExe := filepath.Join(installDir, "Conective.exe")
	targetAssets := filepath.Join(installDir, "assets")
	targetGeo := filepath.Join(targetAssets, "GeoLite2-Country.mmdb")

	if *uninstall {
		// Stop any running instance
		_ = silentCmd("taskkill", "/F", "/IM", "Conective.exe").Run()

		// Remove desktop & start menu shortcuts
		desktop := filepath.Join(os.Getenv("USERPROFILE"), "Desktop", "Conective.lnk")
		_ = os.Remove(desktop)
		startMenuDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Conective")
		_ = os.RemoveAll(startMenuDir)

		// Remove registry uninstall entry
		_ = registry.DeleteKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\Conective`)

		// Remove install directory
		_ = os.RemoveAll(installDir)

		if !*silent {
			msgBox("Conective", "Conective has been successfully uninstalled from your computer.", 0x40)
		}
		return
	}

	// 1. Create install directories
	if err := os.MkdirAll(targetAssets, 0755); err != nil {
		msgBox("Conective Setup Error", fmt.Sprintf("Failed to create installation directory: %v", err), 0x10)
		return
	}

	// 2. Extract Conective.exe
	if err := os.WriteFile(targetExe, binConective, 0755); err != nil {
		msgBox("Conective Setup Error", fmt.Sprintf("Failed to write Conective.exe: %v", err), 0x10)
		return
	}

	// 3. Extract GeoLite2-Country.mmdb
	_ = os.WriteFile(targetGeo, binGeoIP, 0644)

	// 4. Extract wintun.dll (for TUN Mode / VPN Adapter)
	targetWintun := filepath.Join(installDir, "wintun.dll")
	if len(binWintun) > 0 {
		_ = os.WriteFile(targetWintun, binWintun, 0755)
	}

	// 5. Extract circumvention binaries (aether.exe & psiphon-tunnel-core.exe)
	targetBin := filepath.Join(installDir, "bin")
	_ = os.MkdirAll(targetBin, 0755)
	if len(binAether) > 0 {
		_ = os.WriteFile(filepath.Join(targetBin, "aether.exe"), binAether, 0755)
	}
	if len(binPsiphon) > 0 {
		_ = os.WriteFile(filepath.Join(targetBin, "psiphon-tunnel-core.exe"), binPsiphon, 0755)
	}

	// 6. Create Desktop Shortcut
	desktopDir := filepath.Join(os.Getenv("USERPROFILE"), "Desktop")
	desktopShortcut := filepath.Join(desktopDir, "Conective.lnk")
	createShortcut(desktopShortcut, targetExe, installDir)

	// 6. Create Start Menu Shortcut
	startMenuDir := filepath.Join(os.Getenv("APPDATA"), "Microsoft", "Windows", "Start Menu", "Programs", "Conective")
	_ = os.MkdirAll(startMenuDir, 0755)
	startMenuShortcut := filepath.Join(startMenuDir, "Conective.lnk")
	createShortcut(startMenuShortcut, targetExe, installDir)

	// 7. Register in Windows Add/Remove Programs (Registry)
	regPath := `Software\Microsoft\Windows\CurrentVersion\Uninstall\Conective`
	k, _, err := registry.CreateKey(registry.CURRENT_USER, regPath, registry.ALL_ACCESS)
	if err == nil {
		_ = k.SetStringValue("DisplayName", "Conective")
		_ = k.SetStringValue("DisplayVersion", "1.0.0")
		_ = k.SetStringValue("Publisher", "Conective")
		_ = k.SetStringValue("InstallLocation", installDir)
		_ = k.SetStringValue("DisplayIcon", targetExe)
		_ = k.SetStringValue("UninstallString", fmt.Sprintf(`"%s" -uninstall`, os.Args[0]))
		k.Close()
	}

	// 8. Prompt completion and launch
	if !*silent {
		msgBox("Conective Setup", "Installation Complete!\n\nConective is ready to use.\nShortcuts have been created on your Desktop and Start Menu.", 0x40)
	}

	// Launch Conective
	_ = silentCmd(targetExe).Start()
}

func createShortcut(shortcutPath, targetPath, workingDir string) {
	script := fmt.Sprintf(`$ws = New-Object -ComObject WScript.Shell; $s = $ws.CreateShortcut('%s'); $s.TargetPath = '%s'; $s.WorkingDirectory = '%s'; $s.IconLocation = '%s,0'; $s.Save()`,
		shortcutPath, targetPath, workingDir, targetExeLocation(targetPath))
	_ = silentCmd("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", script).Run()
}

func targetExeLocation(p string) string {
	return p
}
