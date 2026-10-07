package main

import (
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"syscall"
	"time"

	"freenode/assets"
	"freenode/pkg/api"
	"freenode/pkg/cleanip"
	"freenode/pkg/database"
	"freenode/pkg/geoip"
	"freenode/pkg/proxy"
	"freenode/pkg/scheduler"
	"freenode/pkg/tray"
	"freenode/pkg/tun"
	"freenode/pkg/v2go"
	"freenode/pkg/xray"

	"github.com/jchv/go-webview2"
)

var (
	version = "1.0.0"
)

func main() {
	// Configure Go runtime memory management for ultra-low RAM footprint
	debug.SetGCPercent(50)                  // Reclaim heap aggressively (default is 100)
	debug.SetMemoryLimit(256 * 1024 * 1024) // 256MB soft ceiling
	debug.FreeOSMemory()                   // Return OS pages immediately

	// Configure Chromium WebView2 memory-saving flags via environment variable
	// Reduces WebView2/Chromium RAM footprint by 50-70%
	existingArgs := os.Getenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS")
	memFlags := "--disable-features=Translate,OptimizationHints,MediaRouter,DialLocalDiscovery,CalculateNativeWinOcclusion,InterestFeedContentSuggestions " +
		"--renderer-process-limit=1 " +
		"--disable-gpu-shader-disk-cache " +
		"--disk-cache-size=10485760 " +
		"--media-cache-size=5242880 " +
		"--disable-background-networking " +
		"--disable-component-update " +
		"--disable-sync " +
		"--disable-extensions " +
		"--disable-default-apps " +
		"--disable-domain-reliability " +
		"--no-first-run " +
		"--no-default-browser-check " +
		"--disable-hang-monitor " +
		"--disable-prompt-on-repost " +
		"--disable-client-side-phishing-detection " +
		"--disable-breakpad " +
		"--disable-crash-reporter " +
		"--js-flags=--max-old-space-size=64"
	if existingArgs != "" {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", existingArgs+" "+memFlags)
	} else {
		_ = os.Setenv("WEBVIEW2_ADDITIONAL_BROWSER_ARGUMENTS", memFlags)
	}

	// Lock main thread for Win32 message loop & WebView2
	runtime.LockOSThread()

	headlessFlag := flag.Bool("headless", false, "run in headless background mode without opening UI window")
	minimizedFlag := flag.Bool("minimized", false, "start minimized to system tray")
	portFlag := flag.Int("port", 0, "explicit port for local web UI (default: random free port)")
	dataDirFlag := flag.String("data-dir", "", "custom path for database and assets")
	flag.Parse()

	// Determine data directory
	dataDir := *dataDirFlag
	if dataDir == "" {
		appData := os.Getenv("APPDATA")
		if appData != "" {
			dataDir = filepath.Join(appData, "Conective")
		} else {
			dataDir = "./data"
		}
	}
	_ = os.MkdirAll(dataDir, 0755)

	// In GUI mode, ensure logs are recorded to file
	logFile, err := os.OpenFile(filepath.Join(dataDir, "conective.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		defer logFile.Close()
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
	}

	log.Printf("========================================")
	log.Printf("  Conective v%s - Starting Application  ", version)
	log.Printf("========================================")

	dbPath := filepath.Join(dataDir, "conective.db")
	// Migrate older freenode.db if it exists and conective.db does not
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		appData := os.Getenv("APPDATA")
		oldDbPath := filepath.Join(appData, "FreeNode", "freenode.db")
		if data, errOld := os.ReadFile(oldDbPath); errOld == nil {
			_ = os.WriteFile(dbPath, data, 0644)
			log.Printf("[Init] Migrated existing database from: %s", oldDbPath)
		}
	}
	log.Printf("[Init] Database path: %s", dbPath)

	// 1. Initialize SQLite Database
	db, err := database.Open(dbPath)
	if err != nil {
		log.Fatalf("[Error] Failed to open database: %v", err)
	}
	defer db.Close()

	// Seed default sources (freedom and v2go enabled, others disabled)
	log.Printf("[Init] Verifying and seeding default sources...")
	_ = db.SeedDefaultSources(v2go.DefaultSources())
	debug.FreeOSMemory()

	// 2. Locate / Extract GeoIP database
	geoPath := filepath.Join(dataDir, "GeoLite2-Country.mmdb")
	if _, err := os.Stat(geoPath); os.IsNotExist(err) {
		// Look in current working directory or assets
		localGeo := filepath.Join("assets", "GeoLite2-Country.mmdb")
		if _, err := os.Stat(localGeo); err == nil {
			geoPath = localGeo
		}
	}

	geoResolver, err := geoip.New(geoPath)
	if err != nil {
		log.Printf("[Warning] GeoIP resolver not loaded: %v (country lookups will use fallbacks)", err)
	} else {
		defer geoResolver.Close()
		cleanip.SetDefaultGeoResolver(geoResolver)
		log.Printf("[Init] GeoIP resolver loaded from: %s", geoPath)
	}

	// 3. System Proxy & Crash Recovery
	proxyMgr := proxy.NewManager()
	httpPort, _ := strconv.Atoi(db.GetSetting("http_port", "10809"))
	socksPort, _ := strconv.Atoi(db.GetSetting("socks_port", "10808"))
	testEndpoint := db.GetSetting("test_endpoint", "http://cp.cloudflare.com/generate_204")

	// Restore any orphaned proxy & TUN routes from prior crashed runs
	proxyMgr.RecoverOrphanedProxy(httpPort)
	tun.NewManager().RecoverOrphanedTUNRoutes()

	// Ensure wintun.dll is present next to executable and current directory for TUN mode
	if exePath, err := os.Executable(); err == nil {
		wintunTarget := filepath.Join(filepath.Dir(exePath), "wintun.dll")
		if _, err := os.Stat(wintunTarget); os.IsNotExist(err) && len(assets.WintunDLL) > 0 {
			_ = os.WriteFile(wintunTarget, assets.WintunDLL, 0644)
		}

		targetBin := filepath.Join(filepath.Dir(exePath), "bin")
		_ = os.MkdirAll(targetBin, 0755)
		for _, bName := range []string{"aether.exe", "psiphon-tunnel-core.exe"} {
			dest := filepath.Join(targetBin, bName)
			if _, err := os.Stat(dest); os.IsNotExist(err) {
				for _, srcDir := range []string{
					filepath.Join("assets", "bin"),
					filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs", "Conective", "bin"),
					filepath.Join(os.Getenv("APPDATA"), "Conective", "bin"),
				} {
					src := filepath.Join(srcDir, bName)
					if data, errRead := os.ReadFile(src); errRead == nil && len(data) > 0 {
						_ = os.WriteFile(dest, data, 0755)
						break
					}
				}
			}
		}
	}
	if _, err := os.Stat("wintun.dll"); os.IsNotExist(err) && len(assets.WintunDLL) > 0 {
		_ = os.WriteFile("wintun.dll", assets.WintunDLL, 0644)
	}

	// 4. Client Xray Engine
	runner := xray.NewRunner(socksPort, httpPort, testEndpoint)
	runner.SetDB(db)
	if db.GetSetting("tun_mode", "false") == "true" {
		_ = runner.SetTunMode(true)
	}
	if db.GetSetting("share_lan", "false") == "true" {
		runner.SetShareLAN(true)
	}
	if db.GetSetting("gaming_mode", "false") == "true" {
		runner.SetGamingMode(true)
	}

	// 5. Embedded v2go Engine
	v2goEngine := v2go.NewEngine(db, geoResolver)

	// 6. Scheduler & Failover Controller
	schedCtrl := scheduler.NewController(db, v2goEngine, runner, proxyMgr)
	schedCtrl.Start()
	defer schedCtrl.Stop()

	// 7. API and UI Server
	apiServer := api.NewServer(db, v2goEngine, runner, proxyMgr, schedCtrl)

	// Static assets handler from embedded filesystem
	subFS, err := fs.Sub(assets.WebFS, "web")
	if err != nil {
		log.Fatalf("[Error] Failed to read embedded assets: %v", err)
	}

	mainMux := http.NewServeMux()
	mainMux.Handle("/api/", apiServer.Handler())
	mainMux.Handle("/", http.FileServer(http.FS(subFS)))

	// Find free port or use flag
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", *portFlag))
	if err != nil {
		log.Fatalf("[Error] Failed to bind local server: %v", err)
	}
	serverURL := fmt.Sprintf("http://127.0.0.1:%d", listener.Addr().(*net.TCPAddr).Port)
	log.Printf("[Server] Conective UI available at: %s", serverURL)

	httpServer := &http.Server{
		Handler: mainMux,
	}

	go func() {
		if err := httpServer.Serve(listener); err != nil && err != http.ErrServerClosed {
			log.Printf("[Error] HTTP server error: %v", err)
		}
	}()

	// Cleanup helper function
	performCleanup := func() {
		log.Printf("\n[Shutdown] Cleaning up Conective...")

		// Cancel scan if running
		if v2goEngine.IsScanning() {
			log.Printf("[Shutdown] Stopping active scan...")
			v2goEngine.CancelScan()
		}

		// Disconnect Xray client
		if runner.IsRunning() {
			log.Printf("[Shutdown] Stopping Xray client...")
			_ = runner.Disconnect()
		}

		// Disable system proxy to leave Windows clean
		log.Printf("[Shutdown] Restoring Windows system proxy...")
		_ = proxyMgr.Disable()

		// Ensure any stale Wintun adapters are cleaned up
		tun.CleanupStaleWintunAdapters()

		_ = httpServer.Close()
		log.Printf("[Shutdown] Conective stopped cleanly.")
	}

	// 8. Initialize System Tray (Feature 31 & Feature 32)
	var appTray *tray.Tray
	var w webview2.WebView

	appTray = tray.New(tray.Options{
		IconBytes: assets.AppIcon,
		Title:     "Conective",
		Tooltip:   "Conective - Disconnected",
		OnShow: func() {
			if appTray != nil {
				appTray.ShowWindow()
			}
		},
		OnDisconnect: func() {
			if runner.IsRunning() {
				_ = runner.Disconnect()
				_ = proxyMgr.Disable()
			}
		},
		OnExit: func() {
			if appTray != nil {
				appTray.ExitApp()
			}
			performCleanup()
			if w != nil {
				w.Terminate()
			}
			os.Exit(0)
		},
	})

	if err := appTray.Start(); err != nil {
		log.Printf("[Warning] System tray initialization: %v", err)
	} else {
		defer appTray.Stop()
	}

	// Status synchronization loop for tray icon tooltip and context menu
	go func() {
		lastState := false
		for {
			time.Sleep(1 * time.Second)
			running := runner.IsRunning()
			if running != lastState {
				lastState = running
				appTray.SetConnected(running)
			}
		}
	}()

	// 9. Launch Native Desktop Window (WebView2) or wait in headless mode
	if !*headlessFlag {
		w = webview2.NewWithOptions(webview2.WebViewOptions{
			Debug:    false,
			DataPath: filepath.Join(dataDir, "webview_cache"),
			WindowOptions: webview2.WindowOptions{
				Title:  "Conective",
				Width:  1260,
				Height: 820,
				IconId: 1,
				Center: true,
			},
		})

		if w != nil {
			defer w.Destroy()
			w.Navigate(serverURL)

			// Attach window to system tray for minimize-to-tray handling on close
			hwnd := uintptr(w.Window())
			appTray.AttachWindow(hwnd, func() bool {
				return db.GetSetting("minimize_to_tray", "true") == "true"
			})

			if *minimizedFlag {
				appTray.HideWindow()
			}

			// Signal listener to terminate webview gracefully
			sigChan := make(chan os.Signal, 1)
			signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
			go func() {
				<-sigChan
				w.Terminate()
			}()

			// Run blocks until the user closes the window or exits
			w.Run()
			performCleanup()
			return
		}

		// Fallback if WebView2 runtime is missing
		log.Println("[Warning] WebView2 runtime unavailable, falling back to browser window...")
		go launchWindowFallback(serverURL)
	}

	// Wait for termination signal in headless or fallback mode
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	performCleanup()
}

func launchWindowFallback(url string) {
	if runtime.GOOS == "windows" {
		edgePaths := []string{
			os.ExpandEnv(`%ProgramFiles(x86)%\Microsoft\Edge\Application\msedge.exe`),
			os.ExpandEnv(`%ProgramFiles%\Microsoft\Edge\Application\msedge.exe`),
		}

		for _, p := range edgePaths {
			if _, err := os.Stat(p); err == nil {
				cmd := exec.Command(p,
					fmt.Sprintf("--app=%s", url),
					"--window-size=1260,820",
					"--renderer-process-limit=1",
					"--disk-cache-size=10485760",
					"--media-cache-size=5242880",
					"--disable-background-networking",
					"--disable-features=Translate,OptimizationHints",
					"--js-flags=--max-old-space-size=64",
				)
				if err := cmd.Start(); err == nil {
					return
				}
			}
		}

		// Fallback to default browser
		_ = silentCmd("cmd", "/c", "start", url).Start()
	}
}

func silentCmd(name string, args ...string) *exec.Cmd {
	cmd := exec.Command(name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		HideWindow:    true,
		CreationFlags: 0x08000000,
	}
	return cmd
}
