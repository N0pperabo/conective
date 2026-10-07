# Conective Windows Build Script
# Builds both Portable Conective.exe and Single-File Conective-Setup.exe installer

$ErrorActionPreference = "Stop"

Write-Host "==========================================" -ForegroundColor Cyan
Write-Host "     Building Conective for Windows       " -ForegroundColor Cyan
Write-Host "==========================================" -ForegroundColor Cyan

# 1. Verify Go toolchain
$goVersion = go version
Write-Host "[1/4] Go Toolchain: $goVersion" -ForegroundColor Green

# 2. Build Portable Conective.exe
Write-Host "[2/4] Compiling Conective.exe (GUI Subsystem, Strip Symbols)..." -ForegroundColor Yellow
go build -ldflags="-H=windowsgui -s -w" -o Conective.exe ./cmd/conective
if ($LASTEXITCODE -ne 0) { throw "Failed to build Conective.exe" }

# 3. Build Embedded Binaries & Assets for Installer
Write-Host "[3/4] Preparing embedded binaries and assets for installer..." -ForegroundColor Yellow
Copy-Item "Conective.exe" -Destination "cmd\installer\Conective.exe" -Force
Copy-Item "bin" -Destination "cmd\installer\bin" -Recurse -Force
Copy-Item "assets\GeoLite2-Country.mmdb" -Destination "cmd\installer\GeoLite2-Country.mmdb" -Force
Copy-Item "assets\wintun.dll" -Destination "cmd\installer\wintun.dll" -Force

# 4. Build Conective-Setup.exe Installer
Write-Host "[4/4] Compiling Conective-Setup.exe..." -ForegroundColor Yellow
go build -ldflags="-H=windowsgui -s -w" -o Conective-Setup.exe ./cmd/installer
if ($LASTEXITCODE -ne 0) { throw "Failed to build Conective-Setup.exe" }

# Cleanup intermediate installer assets
Remove-Item "cmd\installer\Conective.exe" -Force -ErrorAction SilentlyContinue
Remove-Item "cmd\installer\bin" -Recurse -Force -ErrorAction SilentlyContinue
Remove-Item "cmd\installer\GeoLite2-Country.mmdb" -Force -ErrorAction SilentlyContinue
Remove-Item "cmd\installer\wintun.dll" -Force -ErrorAction SilentlyContinue

Write-Host ""
Write-Host "Build Complete Successfully!" -ForegroundColor Green
Get-Item "Conective.exe", "Conective-Setup.exe" | Select-Object Name, Length, LastWriteTime | Format-Table -AutoSize
