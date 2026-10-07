# Conective

<p align="center">
  <strong>High-Performance, Next-Generation Anti-Censorship & VPN Desktop Client for Windows</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.27+-00ADD8?style=for-the-badge&logo=go" alt="Go" />
  <img src="https://img.shields.io/badge/Platform-Windows%2010%2F11-0078D6?style=for-the-badge&logo=windows" alt="Windows" />
  <img src="https://img.shields.io/badge/Core-Xray--core%20v1.26+-blue?style=for-the-badge" alt="Xray" />
  <img src="https://img.shields.io/badge/Driver-Wintun%20TUN-darkgreen?style=for-the-badge" alt="Wintun" />
  <img src="https://img.shields.io/badge/UI-WebView2%20Modern%20Dashboard-purple?style=for-the-badge" alt="WebView2" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
</p>

---

## 📖 Overview | درباره برنامه

**Conective** is an all-in-one, high-performance desktop proxy and VPN client engineered specifically for heavily restricted network environments (such as Iran and China). It combines an embedded **Xray-core** engine, **Psiphon / Aether** obfuscation, **Clean IP Fronting**, and high-speed **Wintun TUN** virtual adapter mode into an elegant, modern native Windows application.

**کانکتیو (Conective)** یک کلاینت وی‌پی‌ان و پراکسی دسکتاپ پرسرعت و مدرن برای ویندوز است که به طور ویژه برای عبور از فیلترینگ شدید و اینترنت‌های محدود توسعه یافته است. این برنامه ترکیبی قدرتمند از هسته درونی **Xray-core**، پروتکل‌های مبهم‌سازی **سایفون (Psiphon / Aether)**، اسکن و فرانتینگ **آی‌پی‌های تمیز کلودفلر (Clean IP)**، و حالت وی‌پی‌ان سیستمی کامل با درایور **Wintun TUN** می‌باشد.

---

## ✨ Key Features | قابلیت‌های کلیدی

### 🛡️ Circumvention & Protocols | پروتکل‌ها و دور زدن فیلترینگ
- **Complete Protocol Support**: Full in-process support for **VLESS**, **VMess**, **Trojan**, **Shadowsocks**, **WireGuard**, and **Reality**.
- **Psiphon & Aether Core**: Native, background-managed Psiphon multi-hop tunnel core with zero-flicker silent process management.
- **Anti-DPI TLS SNI Fragmentation**: RFC-compliant `packets: 1-1` TLS ClientHello record fragmentation targeting only SNI handshakes, ensuring line-rate 0ms latency for streaming and high-volume data payloads.
- **Clean IP & Cloudflare Fronting**:
  - Live GeoIP resolution (accurate country flags & ISP detection).
  - Bulk & manual IP import (support for raw IPs, subnets, and tagged subscriptions).
  - Built-in multi-threaded HTTP/HTTPS ping and jitter measurement.

### 🌐 Advanced Networking | شبکه و مسیریابی
- **True System TUN Mode**: RFC 2544 benchmark subnet (`198.18.0.0/16`) using WireGuard's high-speed Layer 3 `wintun.dll` driver. Features deterministic static device reuse (`FreeNodeTUN`) with zero PnP churn and fallback recovery.
- **Routing Loop Prevention**: Physical network interface auto-binding (`IP_UNICAST_IF`) guaranteeing that direct traffic and tunnel handshakes never loop back into Wintun.
- **Split Routing Modes**:
  - **Blacklist (Bypass Iran)**: Automatically routes Iranian national `.ir` domains, domestic banking, and local applications directly outside the proxy.
  - **Whitelist**: Routes only designated applications and target domains through the proxy.
  - **Global**: Routes 100% of network traffic through the encrypted tunnel.
- **Low-Latency Gaming Mode**: UDP fast-path prioritizing game packets with `TCP_NODELAY`, `TCP Fast Open (TFO)`, and optimized MTU (1400).
- **LAN Proxy Sharing**: Share active proxy across local network devices (Wi-Fi hotspot / LAN).

### 🖥️ Desktop Experience | تجربه کاربری
- **Modern Responsive Dashboard**: Hardware-accelerated UI powered by Microsoft Edge WebView2 with ultra-low memory consumption (`< 50MB RAM`).
- **Native System Tray**: Minimize to Windows Notification Area with live connection status indicators and quick-disconnect menu.
- **Automated Failover & Health**: Non-blocking background health check with iterative backup node migration upon connection drops.

---

## 🏗️ Architecture | معماری پروژه

```
Conective/
├── cmd/
│   ├── conective/        # Main application entry point (GUI window, tray, WebView2)
│   ├── installer/        # Standalone single-file setup builder (Conective-Setup.exe)
│   └── ipdata_tool/      # Offline IP metadata and GeoIP analysis utility
├── pkg/
│   ├── api/              # Local REST API server powering the frontend
│   ├── autostart/        # Windows Startup registry integration
│   ├── cleanip/          # Cloudflare Clean IP scanner, ping, and DNS resolver
│   ├── database/         # SQLite storage with WAL mode and vacuum management
│   ├── gaming/           # Gaming mode optimizer (MTU & low-latency sockets)
│   ├── geoip/            # MaxMind GeoLite2 country database lookup
│   ├── lan/              # LAN address discovery & proxy sharing
│   ├── models/           # Data structs for configs, sources, and status
│   ├── procutil/         # Process lifecycle & silent task manager
│   ├── proxy/            # Windows WinINet system proxy manager
│   ├── psiphon/          # Psiphon tunnel core & Aether controller
│   ├── scheduler/        # Background auto-scan & iterative failover manager
│   ├── subscription/     # Multi-format subscription importer (Base64, Clash, Sing-Box)
│   ├── tray/             # Win32 system tray icon & menu
│   ├── tun/              # Wintun Layer 3 adapter manager & routing table engine
│   ├── updater/          # In-app GitHub release updater
│   ├── v2go/             # Node parser & multi-core benchmark scanner
│   └── xray/             # Embedded Xray-core runner & routing rule compiler
├── assets/
│   ├── web/              # Dashboard frontend (HTML5, CSS3, ES6 JavaScript)
│   ├── app.ico           # Application icon
│   ├── GeoLite2-Country  # GeoIP database
│   └── bin/              # Bundled circumvention engines (aether.exe, psiphon)
├── scripts/
│   └── build.ps1         # Windows PowerShell automated build script
├── go.mod                # Go module definition
├── go.sum                # Checksum dependencies
├── LICENSE               # MIT License
└── README.md             # Project documentation
```

---

## 🚀 Building from Source | ساخت از سورس‌کد

### Prerequisites | پیش‌نیازها
1. **Windows 10 / 11** (64-bit)
2. **Go 1.22+** installed and added to `PATH`
3. **Git** for Windows

### Build Instructions | دستور ساخت

Clone or download the repository, open PowerShell in the project directory, and run:

```powershell
# Run the automated build script
.\scripts\build.ps1
```

This will produce:
- **`Conective.exe`**: Portable, silent GUI application (Subsystem: Windows GUI, zero terminal popups).
- **`Conective-Setup.exe`**: All-in-one standalone installer that creates desktop shortcuts and program registry entries.

Or build manually via Go CLI:

```powershell
# Build portable executable
go build -ldflags="-H=windowsgui -s -w" -o Conective.exe ./cmd/conective

# Build single-file installer
Copy-Item "Conective.exe" -Destination "cmd\installer\Conective.exe" -Force
go build -ldflags="-H=windowsgui -s -w" -o Conective-Setup.exe ./cmd/installer
Remove-Item "cmd\installer\Conective.exe" -Force
```

---

## 🔒 Security & Privacy | امنیت و حریم خصوصی

- **No Remote Tracking**: Conective is 100% open-source and performs no telemetry, data collection, or remote logging.
- **Crash Recovery**: Automatically restores Windows proxy and deletes TUN routing overrides if terminated unexpectedly.
- **Admin Rights**: Administrator privileges are only required when enabling **TUN Mode** to configure the virtual network adapter and routing tables.

---

## 📄 License | مجوز

This project is licensed under the [MIT License](LICENSE).
Xray-core and Psiphon tunnel cores are trademarks of their respective projects and licensed under their open-source licenses.
