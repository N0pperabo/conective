# Conective

<p align="center">
  <strong>High-Performance, Multi-Engine Anti-Censorship & VPN Desktop Client for Windows</strong>
  <br />
  <strong>کلاینت دسکتاپ پرسرعت، چندموتوره و پیشرفته عبور از فیلترینگ برای ویندوز</strong>
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Language-Go%201.22+-00ADD8?style=for-the-badge&logo=go" alt="Go" />
  <img src="https://img.shields.io/badge/Platform-Windows%2010%20%2F%2011%20(x64)-0078D6?style=for-the-badge&logo=windows" alt="Windows" />
  <img src="https://img.shields.io/badge/Core-Xray--core%20v1.26+-blue?style=for-the-badge" alt="Xray" />
  <img src="https://img.shields.io/badge/Engine-Psiphon%20%2F%20Aether-orange?style=for-the-badge" alt="Psiphon" />
  <img src="https://img.shields.io/badge/Driver-Wintun%20TUN%20Adapter-darkgreen?style=for-the-badge" alt="Wintun" />
  <img src="https://img.shields.io/badge/UI-WebView2%20Dashboard-purple?style=for-the-badge" alt="WebView2" />
  <img src="https://img.shields.io/badge/License-MIT-green?style=for-the-badge" alt="License" />
</p>

---

## English

### Overview

**Conective** is a modern, high-performance Windows desktop circumvention client designed for heavily restricted network environments (such as Iran). Built with Go and a hardware-accelerated Microsoft Edge WebView2 user interface, Conective merges the capabilities of **Xray-core**, **Psiphon / Aether**, **Akamai Clean IP Fronting**, and low-latency **Wintun TUN** virtual networking into a single, cohesive desktop experience.

---

### Key Features

#### 1. Multi-Engine Circumvention
- **Xray Core Integration**: Native support for modern proxy protocols including **VLESS**, **VMess**, **Trojan**, **Shadowsocks**, and **VLESS-Reality**.
- **Psiphon & Aether Engine**: Built-in direct Psiphon circumvention engine running in high-performance mode with automated tactic negotiation.
- **Egress Region Selection**: Connect directly through 20 curated exit countries (Germany, United States, United Kingdom, Netherlands, Japan, Canada, France, Singapore, Switzerland, Norway, Sweden, Italy, Spain, Australia, Finland, Belgium, Austria, Poland, Ireland, India) or use intelligent **Auto (Fastest)** mode.
- **Unified Tactics Datastore**: Centralized circumvention cache (`psiphon.boltdb`) shared across all country selections, preserving learned DPI bypass paths and accelerating handshakes down to 2–5 seconds.
- **Anti-DPI TLS SNI Fragmentation**: RFC-compliant `packets: 1-1` TLS ClientHello SNI record splitting to bypass Deep Packet Inspection firewalls without packet loss.
- **Censorship-Resistant DNS & QUIC Protection**: Built-in DNS-over-HTTPS (DoH) fallback to block DNS poisoning; automatic blocking of UDP port 443 (QUIC) to prevent ISP throttling.

#### 2. Advanced Network & TUN Routing
- **True System-Wide TUN Mode**: High-throughput Layer 3 virtual network interface powered by WireGuard's `wintun.dll` driver (`198.18.0.0/16` subnet, prioritized interface metric).
- **Concurrent System Proxy & TUN**: Keeps Windows System Proxy (`127.0.0.1:10809`) active alongside TUN mode to guarantee zero IPv6 or browser leaks on dual-stack ISP connections.
- **Smart Routing Modes**:
  - **Bypass Iran**: Automatically routes domestic `.ir` domains, Iranian banking IP ranges, and local traffic direct while tunneling blocked international destinations.
  - **Global**: Routes 100% of system traffic through the active tunnel.
- **Low-Latency Gaming Mode**: UDP socket priority, MTU optimization (1400 in gaming mode vs 1500 default), and `TCP_NODELAY` for reduced ping in online games.
- **Routing Loop Prevention**: Explicit process-level exemption rules for circumvention engines (`psiphon-tunnel-core.exe`, `aether.exe`) ensuring tunnel handshakes never get trapped in virtual routing loops.

#### 3. Akamai Clean IP Scanner & Fronting
- **Akamai (ShirOKhorshid & Global)**: Dedicated scanner targeting high-reputation Akamai Anycast CIDRs and verified edge IP pools.
- **Customizable Scan Parameters**: Adjust worker threads (50 to 300), handshake timeouts (1000ms to 2500ms), and candidate sample sizes.
- **One-Click Deployment**: Directly connect to or add discovered clean IPs to your saved node list.

#### 4. Real-Time Live Logs
- **Server-Sent Events (SSE) Stream**: Live diagnostic terminal streaming real-time stdout/stderr from Psiphon, Aether, Xray, TUN adapter, and REST API.
- **Developer Controls**: Filter logs by source, search log messages, toggle auto-scroll, copy console output to clipboard, or clear with one click.

#### 5. Native Windows Experience
- **WebView2 UI**: Clean, responsive dark dashboard consuming less than 50MB RAM.
- **Clean Aesthetic**: Modern, professional UI design strictly free of emojis.
- **System Tray**: Minimize to Windows Notification Area on close, preserving background connectivity.
- **Persistent State**: Automatic storage of settings, filters, and preferences via SQLite (WAL mode) and local storage.

---

### Architecture

```
Conective/
├── cmd/
│   ├── conective/        # Main application entry point (GUI window, tray, WebView2)
│   ├── installer/        # Single-file setup builder (Conective-Setup.exe)
│   └── ipdata_tool/      # GeoIP analysis utility
├── pkg/
│   ├── api/              # Local REST API server powering the frontend
│   ├── autostart/        # Windows startup registry integration
│   ├── cleanip/          # Akamai Clean IP scanner, probe, and fronting manager
│   ├── database/         # SQLite storage engine with migrations
│   ├── gaming/           # Gaming mode optimizer
│   ├── geoip/            # GeoLite2 country resolver
│   ├── logger/           # Live streaming log ring buffer & SSE broadcaster
│   ├── models/           # Data models for nodes, settings, and logs
│   ├── proxy/            # Windows WinINet system proxy manager
│   ├── psiphon/          # Psiphon & Aether engine controller
│   ├── scheduler/        # Background health check and auto-scanner
│   ├── subscription/     # Multi-format subscription importer (Base64, Clash, Sing-Box)
│   ├── tray/             # Native Win32 system tray integration
│   ├── tun/              # Wintun Layer 3 virtual adapter manager
│   ├── updater/          # In-app GitHub release updater
│   ├── v2go/             # High-speed node latency tester
│   └── xray/             # Embedded Xray-core manager & routing engine
├── assets/
│   ├── web/              # Dashboard frontend (HTML5, CSS3, ES6 JavaScript)
│   ├── app.ico           # Application icon
│   └── bin/              # Bundled circumvention binaries (aether.exe, psiphon, xray)
└── scripts/
    └── build.ps1         # Automated PowerShell build script
```

---

### Building from Source

#### Prerequisites
1. **Windows 10 / 11** (64-bit)
2. **Go 1.22+** installed and added to `PATH`
3. **Git** for Windows

#### Build Commands
Clone the repository and run in PowerShell:

```powershell
# Build portable executable
go build -ldflags="-H=windowsgui -s -w" -o Conective.exe ./cmd/conective

# Or run the automated script to build Conective.exe and Conective-Setup.exe
.\scripts\build.ps1
```

---

## فارسی (Persian)

### معرفی پروژه

**کانکتیو (Conective)** یک نرم‌افزار دسکتاپ جامع، مدرن و پرسرعت برای سیستم‌عامل ویندوز است که به طور ویژه برای دور زدن فیلترینگ شدید و سانسور اینترنت در ایران توسعه یافته است. این برنامه ترکیبی از هسته پیشرفته **Xray-core**، موتور دور زننده **سایفون و ایثر (Psiphon / Aether)**، اسکنر فرانتینگ **آی‌پی‌های تمیز آکامای (Akamai Clean IP)** و درایور پرسرعت شبکه مجازی **Wintun TUN** می‌باشد.

---

### قابلیت‌های کلیدی

#### ۱. موتورهای چندگانه عبور از فیلترینگ
- **پشتیبانی کامل از Xray**: پشتیبانی داخلی از پروتکل‌های **VLESS**، **VMess**، **Trojan**، **Shadowsocks** و **VLESS-Reality**.
- **موتور قدرتمند سایفون (Psiphon / Aether)**: اتصال مستقیم بدون نیاز به کانفیگ با استفاده از تاکتیک‌های مایک، مبهم‌سازی OSSH و شکل‌دهی خودکار ترافیک.
- **انتخاب کشور خروجی (Egress Region)**: امکان انتخاب ۲۰ کشور معتبر جهان (آلمان، آمریکا، انگلستان، هلند، ژاپن، کانادا، فرانسه، سنگاپور، سوئیس، نروژ، سوئد، ایتالیا، اسپانیا، استرالیا، فنلاند، بلژیک، اتریش، لهستان، ایرلند، هند) یا حالت هوشمند خودکار (سریع‌ترین سرور).
- **دیتابیس تاکتیک‌های یکپارچه**: دیتابیس یادگیرنده مرکزی (`psiphon.boltdb`) میان تمام کشورها به اشتراک گذاشته شده است تا تاکتیک‌های موفق دور زدن سانسور حفظ شده و زمان اتصال به ۲ الی ۵ ثانیه کاهش یابد.
- **تکه‌تکه‌سازی هدر TLS (SNI Fragmentation)**: پیاده‌سازی استاندارد قطعه‌بندی `packets: 1-1` در رکورد ClientHello برای فریب فایروال‌های DPI بدون افت پهنای باند و بدون ایجاد تأخیر.
- **محافظت در برابر مسمومیت DNS و QUIC**: مجهز به سامانه امن DNS-over-HTTPS و مسدودسازی ترافیک UDP پورت ۴۴۳ برای جلوگیری از اختلالات عمدی پروتکل QUIC توسط اپراتورها.

#### ۲. شبکه مجازی و حالت سیستم‌عامل (TUN Mode)
- **حالت TUN واقعی**: هدایت کل ترافیک سیستم از طریق درایور لایه ۳ پرسرعت وایرگارد (`wintun.dll`) در ساب‌نت `198.18.0.0/16` با اولویت متریک کارت شبکه.
- **فعالیت همزمان پراکسی سیستمی و تونل**: فعال نگه داشتن پراکسی ویندوز (`127.0.0.1:10809`) در کنار حالت TUN جهت تضمین عدم نشت ترافیک IPv6 و مرورگرها (کروم، اج، فایرفاکس).
- **حالت‌های مسیریابی هوشمند**:
  - **دور زدن ایران (Bypass Iran)**: عبور مستقیم دامنه‌های ملی (`.ir`)، درگاه‌های بانکی و وب‌سایت‌های داخلی بدون عبور از فیلترشکن.
  - **حالت سراسری (Global)**: عبور ۱۰۰٪ ترافیک کل سیستم از داخل تونل امن.
- **حالت کاهش پینگ مخصوص بازی (Gaming Mode)**: اولویت‌بندی پکت‌های UDP، تنظیم خودکار MTU روی ۱۴۰۰ و فعال‌سازی `TCP_NODELAY`.
- **جلوگیری از لوپ روتینگ**: مستثنی کردن خودکار پروسه‌های سایفون و ایثر در روت‌های ویندوز برای جلوگیری از افتادن بسته‌ها در چرخه برگشتی شبکه.

#### ۳. اسکنر آی‌پی تمیز آکامای (Akamai Clean IP)
- **شبکه Akamai و شیر و خورشید**: اسکن هوشمند بازه‌های رنج تمیز CDN آکامای به منظور فرانتینگ پایدار.
- **تنظیمات دقیق اسکن**: انتخاب تعداد ورکرها (۵۰ تا ۳۰۰ ترد)، تایم‌اوت پینگ (۱۰۰۰ تا ۲۵۰۰ میلی‌ثانیه) و اندازه نمونه آماری.
- **اتصال با یک کلیک**: اتصال مستقیم به آی‌پی کشف‌شده یا ذخیره آن در لیست کانفیگ‌ها.

#### ۴. کنسول و لاگ زنده سیستم (Live Logs)
- **استریم بیدرنگ با پروتکل SSE**: نمایش زنده و خط‌به‌خط پیام‌ها و خروجی‌های هسته Xray، سایفون، آداپتور TUN و سرور API.
- **امکانات خط فرمان**: فیلتر بر اساس منبع لاگ، جستجوی زنده، اسکرول خودکار، کپی با یک کلیک در کلیپ‌بورد و پاک‌سازی کنسول.

#### ۵. تجربه کاربری پیشرفته
- **داشبورد بومی و مدرن WebView2**: رابط کاربری شیک، دارک و سبک با مصرف کمتر از ۵۰ مگابایت رم.
- **طراحی حرفه‌ای**: عاری از ایموجی و سازگار با استانداردهای نرم‌افزارهای دسکتاپ مدرن.
- **ناحیه اعلان‌ها (System Tray)**: مینیمایز شدن در کنار ساعت ویندوز در هنگام بستن پنجره بدون قطع شدن اتصال.
- **ذخیره‌سازی پایدار**: حفظ پایدار تمامی تنظیمات، فیلترها و مقادیر در دیتابیس SQLite و حافظه محلی.

---

### ساخت و کامپایل از سورس‌کد

#### پیش‌نیازها
1. سیستم‌عامل **ویندوز ۱۰ یا ۱۱** نسخه ۶۴ بیتی
2. نصب بودن **Go نسخه ۱.۲۲ یا بالاتر** در متغیرهای محیطی سیستم (`PATH`)
3. ابزار **Git**

#### دستورات کامپایل در PowerShell

```powershell
# ساخت فایل اجرایی پرتابل بدون پنجره ترمینال
go build -ldflags="-H=windowsgui -s -w" -o Conective.exe ./cmd/conective

# یا اجرای اسکریپت بیلد کامل برای ساخت فایل نصبی Conective-Setup.exe
.\scripts\build.ps1
```

---

## 🔒 Security & Privacy | امنیت و حریم خصوصی

- **حفظ حریم خصوصی**: نرم‌افزار ۱۰۰٪ متن‌باز بوده و فاقد هرگونه تله‌متری، جمع‌آوری داده یا گزارش‌دهی به سرورهای خارجی است.
- **بازیابی پس از کرش**: در صورت خروج ناگهانی، پراکسی سیستم به صورت خودکار خاموش و روت‌های آداپتور شبکه پاک‌سازی می‌شوند.
- **دسترسی ادمین**: دسترسی Administrator تنها برای ایجاد کارت شبکه مجازی Wintun در حالت TUN مورد نیاز است.

---

## 📄 License | مجوز

This project is licensed under the [MIT License](LICENSE).  
Xray-core, Psiphon, and Wintun are trademarks of their respective owners and distributed under their open-source licenses.
