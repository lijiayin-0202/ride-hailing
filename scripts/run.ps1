<#
.SYNOPSIS
    One-command local start for ride-hailing.

.DESCRIPTION
    Resolves RIDE_DATABASE_SOURCE in this order (first hit wins):
      1. -Dsn parameter
      2. RIDE_DATABASE_SOURCE already set in this shell
      3. .env in the repo root (git-ignored, KEY=VALUE lines)
      4. ..\radish-admin-server\config.dev.yaml (pgsql block of the admin service)
      5. interactive prompt

    RIDE_JWT_SECRET is generated once and persisted to .env so that issued
    tokens survive a restart. RIDE_SMS_ECHO_CODE defaults to 1 for local
    debugging (sms code printed to the service log).

    This file is intentionally ASCII-only so it parses correctly in both
    Windows PowerShell 5.1 and PowerShell 7 regardless of file encoding.

.EXAMPLE
    .\scripts\run.ps1

.EXAMPLE
    .\scripts\run.ps1 -Dsn "postgres://user:pass@host:5432/gva?sslmode=disable"

.EXAMPLE
    powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\run.ps1
#>
param(
    [string]$Dsn = "",
    [string]$Conf = "./configs"
)

$ErrorActionPreference = "Stop"

# Repo root = parent directory of scripts/
$root = Split-Path -Parent $PSScriptRoot
Set-Location $root

$envFile = Join-Path $root ".env"
$httpPort = 8000
$grpcPort = 9000

function Read-EnvFile {
    param([string]$Path)
    $map = @{}
    if (-not (Test-Path $Path)) { return $map }
    foreach ($line in Get-Content $Path) {
        $t = $line.Trim()
        if ($t -eq "" -or $t.StartsWith("#")) { continue }
        $i = $t.IndexOf("=")
        if ($i -lt 1) { continue }
        $map[$t.Substring(0, $i).Trim()] = $t.Substring($i + 1).Trim().Trim('"').Trim("'")
    }
    return $map
}

function Set-EnvLine {
    param([string]$Path, [string]$Key, [string]$Value)
    $lines = @()
    if (Test-Path $Path) { $lines = @(Get-Content $Path) }
    $done = $false
    for ($i = 0; $i -lt $lines.Count; $i++) {
        if ($lines[$i] -match "^\s*$([regex]::Escape($Key))\s*=") {
            $lines[$i] = "$Key=$Value"
            $done = $true
        }
    }
    if (-not $done) { $lines += "$Key=$Value" }
    Set-Content -Path $Path -Value $lines -Encoding ASCII
}

# Build a DSN from the sibling admin service config so the password lives in
# exactly one place. Rotating it there keeps both services in sync.
function Get-DsnFromAdminConfig {
    $admin = Join-Path (Split-Path -Parent $root) "radish-admin-server\config.dev.yaml"
    if (-not (Test-Path $admin)) { return "" }

    $vals = @{}
    $inBlock = $false
    foreach ($line in Get-Content $admin) {
        if ($line -match "^pgsql:\s*$") { $inBlock = $true; continue }
        if ($inBlock -and $line -match "^[A-Za-z0-9_-]+:\s*$") { break }
        if ($inBlock -and $line -match "^\s+([A-Za-z0-9_-]+):\s*(.*)$") {
            $vals[$Matches[1]] = $Matches[2].Trim().Trim('"').Trim("'")
        }
    }

    if (-not $vals["path"] -or -not $vals["username"] -or -not $vals["db-name"]) { return "" }
    $port = "5432"
    if ($vals["port"]) { $port = $vals["port"] }
    $query = "sslmode=disable"
    if ($vals["config"]) { $query = $vals["config"] }

    return "postgres://" + [uri]::EscapeDataString($vals["username"]) + ":" +
        [uri]::EscapeDataString($vals["password"]) + "@" + $vals["path"] + ":" + $port + "/" +
        [uri]::EscapeDataString($vals["db-name"]) + "?" + $query
}

function Hide-Secret {
    param([string]$S)
    return [regex]::Replace($S, "(://[^:@/]+:)[^@]*(@)", '$1***$2')
}

function Get-ListenerPid {
    param([int]$Port)
    $conn = Get-NetTCPConnection -State Listen -LocalPort $Port -ErrorAction SilentlyContinue
    if (-not $conn) { return 0 }
    return @($conn)[0].OwningProcess
}

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Write-Host "FATAL: 'go' not found in PATH." -ForegroundColor Red
    exit 1
}

$fileEnv = Read-EnvFile $envFile

# ---- 1. resolve DSN -------------------------------------------------------
if (-not $Dsn) { $Dsn = $env:RIDE_DATABASE_SOURCE }
if (-not $Dsn) { $Dsn = $fileEnv["RIDE_DATABASE_SOURCE"] }
if (-not $Dsn) { $Dsn = Get-DsnFromAdminConfig }
if (-not $Dsn) {
    Write-Host "No database DSN found." -ForegroundColor Yellow
    Write-Host "Paste a PostgreSQL URL, e.g. postgres://user:pass@host:5432/gva?sslmode=disable"
    $Dsn = Read-Host "RIDE_DATABASE_SOURCE"
}
if (-not $Dsn) {
    Write-Host "FATAL: empty DSN, abort." -ForegroundColor Red
    exit 1
}
# Guard against the placeholder text from configs/config.yaml being pasted.
if ($Dsn -match "<|>|username|password@|@host:") {
    Write-Host "FATAL: DSN still contains placeholder text. Replace it with real values." -ForegroundColor Red
    Write-Host ("       got: " + (Hide-Secret $Dsn)) -ForegroundColor DarkGray
    exit 1
}
$env:RIDE_DATABASE_SOURCE = $Dsn

# ---- 2. stable JWT secret -------------------------------------------------
if (-not $env:RIDE_JWT_SECRET) { $env:RIDE_JWT_SECRET = $fileEnv["RIDE_JWT_SECRET"] }
if (-not $env:RIDE_JWT_SECRET) {
    $bytes = New-Object byte[] 32
    $rng = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    $rng.GetBytes($bytes)
    $env:RIDE_JWT_SECRET = [Convert]::ToBase64String($bytes)
    Set-EnvLine $envFile "RIDE_JWT_SECRET" $env:RIDE_JWT_SECRET
    Write-Host "Generated RIDE_JWT_SECRET once, saved to .env (git-ignored)." -ForegroundColor DarkGray
}

# ---- 3. local debugging defaults -----------------------------------------
if (-not $env:RIDE_SMS_ECHO_CODE) { $env:RIDE_SMS_ECHO_CODE = "1" }
if (-not $env:RIDE_ID_CARD_SALT) { $env:RIDE_ID_CARD_SALT = "local-dev-salt-2026" }

# ---- 4. port pre-check ---------------------------------------------------
$busy = @()
foreach ($p in @($httpPort, $grpcPort)) {
    $owner = Get-ListenerPid $p
    if ($owner -ne 0) { $busy += "port $p held by pid $owner" }
}
if ($busy.Count -gt 0) {
    Write-Host ("FATAL: " + ($busy -join "; ")) -ForegroundColor Red
    Write-Host "Stop it first, e.g.: Stop-Process -Id <pid>" -ForegroundColor Yellow
    exit 1
}

# ---- 5. start ------------------------------------------------------------
Write-Host ""
Write-Host "  ride-hailing  ->  go run ./cmd/ride-hailing -conf $Conf" -ForegroundColor Cyan
Write-Host ("  database      :  " + (Hide-Secret $Dsn)) -ForegroundColor DarkGray
Write-Host ("  http / grpc   :  :$httpPort / :$grpcPort") -ForegroundColor DarkGray
Write-Host "  sms echo code :  $env:RIDE_SMS_ECHO_CODE  (codes printed to this console)" -ForegroundColor DarkGray
Write-Host "  stop          :  Ctrl+C" -ForegroundColor DarkGray
Write-Host ""

go run ./cmd/ride-hailing -conf $Conf
exit $LASTEXITCODE
