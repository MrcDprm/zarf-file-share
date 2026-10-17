# Builds the Windows installer: both zarf.exe files (x64 and ARM64), then Inno Setup.
# Usage, from the repository root:  powershell -File installer\build.ps1 -Version 1.1.0
param([Parameter(Mandatory = $true)][string]$Version)
$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    foreach ($arch in "amd64", "arm64") {
        $env:GOOS = "windows"; $env:GOARCH = $arch; $env:CGO_ENABLED = "0"
        go build -trimpath -ldflags "-s -w -X main.version=$Version" -o "installer\build\$arch\zarf.exe" .\cmd\zarf
        if ($LASTEXITCODE -ne 0) { throw "go build failed for $arch" }
    }
    Remove-Item Env:GOOS, Env:GOARCH, Env:CGO_ENABLED
    $iscc = @("$env:LOCALAPPDATA\Programs\Inno Setup 6\ISCC.exe", "${env:ProgramFiles(x86)}\Inno Setup 6\ISCC.exe") | Where-Object { Test-Path $_ } | Select-Object -First 1
    if (-not $iscc) { throw "Inno Setup 6 (ISCC.exe) not found" }
    & $iscc "/DAppVersion=$Version" "installer\zarf.iss"
    if ($LASTEXITCODE -ne 0) { throw "ISCC failed" }
    Write-Host "Installer: installer\Output\zarf-setup.exe"
} finally {
    Pop-Location
}
