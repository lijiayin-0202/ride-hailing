<#
.SYNOPSIS
    Module 1 (passenger account & profile) smoke test: covers the six owned capabilities.

.DESCRIPTION
    Calls the real HTTP API (no mock). Two ways to run:
      1) Pass -Mobile only: the script sends an sms code, then asks you to type the code
         that the server printed to its console log.
      2) Pass -Token: reuse an existing JWT and skip the login step.

    File is intentionally ASCII-only so it parses correctly in both
    Windows PowerShell 5.1 and PowerShell 7 regardless of file encoding.

.EXAMPLE
    .\scripts\smoke.ps1 -Mobile 13800000001

.EXAMPLE
    .\scripts\smoke.ps1 -Token "eyJhbGciOi..."

.NOTES
    Exit code: 0 = all passed, 1 = at least one failure.
#>
param(
    [string]$BaseUrl = "http://127.0.0.1:8000/api/v1/passenger",
    [string]$Mobile = "13800000001",
    [string]$Code = "",
    [string]$Token = "",
    # Sample ID card number (GB 11643 example). Recorded as SKIP if already bound.
    [string]$IdCardNo = "11010519491231002X"
)

$ErrorActionPreference = "Stop"
$script:results = @()

# Pre-check: fail fast with a clear hint when the service is not listening,
# otherwise the first request dies with a vague "unable to connect to the remote server".
try {
    $uri = [Uri]$BaseUrl
    $probe = New-Object System.Net.Sockets.TcpClient
    $probe.Connect($uri.Host, $uri.Port)
    $probe.Close()
}
catch {
    Write-Host "Fatal: no service listening on $($uri.Host):$($uri.Port)." -ForegroundColor Red
    Write-Host "Start the service first (in another window): go run ./cmd/ride-hailing -conf ./configs" -ForegroundColor Yellow
    exit 1
}

function Add-Result {
    param([string]$Name, [string]$Status, [string]$Detail)
    $script:results += [pscustomobject]@{ Item = $Name; Status = $Status; Detail = $Detail }
}

function Get-ErrBody {
    param($Record)
    if ($Record.ErrorDetails -and $Record.ErrorDetails.Message) { return $Record.ErrorDetails.Message }
    return $Record.Exception.Message
}

# POST helper: send the body as UTF-8 bytes so non-ASCII payloads are not mangled.
function Post-Json {
    param([string]$Url, $Body, [string]$Method = "Post")
    $bytes = [System.Text.Encoding]::UTF8.GetBytes(($Body | ConvertTo-Json -Compress))
    return Invoke-RestMethod $Url -Method $Method -ContentType "application/json; charset=utf-8" `
        -Headers $script:headers -Body $bytes
}

function Get-Api {
    param([string]$Url)
    return Invoke-RestMethod $Url -Headers $script:headers
}

try {
    # ---------- 1. login ----------
    if (-not $Token) {
        $sms = Invoke-RestMethod "$BaseUrl/auth/sms-codes" -Method Post -ContentType "application/json" `
            -Body (@{ mobile = $Mobile; scene = "LOGIN" } | ConvertTo-Json)
        if (-not $Code) {
            # The server prints the code to its own console (RIDE_SMS_ECHO_CODE=1), e.g. "code=123456".
            Write-Host ""
            Write-Host "==> Look at the server console, find the line containing: code=XXXXXX" -ForegroundColor Yellow
            Write-Host "==> Then type ONLY the 6 digits here (you can also paste the whole log line)." -ForegroundColor Yellow
            $Code = Read-Host "Enter the 6-digit sms code"
        }
        # Tolerate pasting a whole log line: extract the 6 digits from it.
        if ($Code -match '(\d{6})') { $Code = $Matches[1] }
        if ($Code -notmatch '^\d{6}$') {
            throw "Invalid sms code '$Code': expected 6 digits."
        }
        $login = Invoke-RestMethod "$BaseUrl/auth/mobile-login" -Method Post -ContentType "application/json" `
            -Body (@{ mobile = $Mobile; sms_code = $Code; verification_id = $sms.data.verification_id; device_id = "smoke" } | ConvertTo-Json)
        $Token = $login.data.access_token
        Add-Result "1.Login" "PASS" "passenger_id=$($login.data.passenger.id) is_new=$($login.data.is_new_user) token_len=$($Token.Length)"
    }
    else {
        Add-Result "1.Login" "SKIP" "reuse provided token"
    }

    $script:headers = @{ Authorization = "Bearer $Token" }

    # ---------- 2. passenger profile ----------
    try {
        $p = Get-Api "$BaseUrl/profile"
        Add-Result "2.Profile" "PASS" "no=$($p.data.passenger.passenger_no) mask=$($p.data.passenger.mobile) realname=$($p.data.passenger.real_name_status)"
    }
    catch { Add-Result "2.Profile" "FAIL" (Get-ErrBody $_) }

    # ---------- 6. preferences ----------
    try {
        $s = Post-Json "$BaseUrl/settings" @{ preferences = @{ promotion_notification_enabled = $false } } "Patch"
        Add-Result "6.Settings" "PASS" "promotion=$($s.data.preferences.promotion_notification_enabled) security=$($s.data.preferences.security_notification_enabled)"
    }
    catch { Add-Result "6.Settings" "FAIL" (Get-ErrBody $_) }

    # ---------- 4. addresses: create / list / update / delete ----------
    try {
        $a = Post-Json "$BaseUrl/addresses" @{ address_type = 1; address_name = "home"; address = "Wangjing SOHO T1"; longitude = 116.48; latitude = 39.99; is_default = $true }
        $aid = $a.data.address.id
        $list = Get-Api "$BaseUrl/addresses?page=1&page_size=10"
        $upd = Post-Json "$BaseUrl/addresses/$aid" @{ address_name = "home-v2" } "Patch"
        $del = Post-Json "$BaseUrl/addresses/$aid" @{} "Delete"
        Add-Result "4.Address" "PASS" "create=$aid total=$($list.data.total) rename=$($upd.data.address.address_name) delete=$($del.data.deleted)"
    }
    catch { Add-Result "4.Address" "FAIL" (Get-ErrBody $_) }

    # ---------- 5a. coupons ----------
    try {
        $cp = Get-Api "$BaseUrl/coupons?page=1&page_size=10"
        Add-Result "5.Coupons" "PASS" "total=$($cp.data.total) available=$($cp.data.available_count)"
    }
    catch { Add-Result "5.Coupons" "FAIL" (Get-ErrBody $_) }

    # ---------- 5b. points + daily sign-in ----------
    try {
        $p1 = Get-Api "$BaseUrl/points?page=1&page_size=10"
        $si = Post-Json "$BaseUrl/sign-ins" @{}
        $p2 = Get-Api "$BaseUrl/points?page=1&page_size=10"
        Add-Result "5.SignIn" "PASS" "$($p1.data.point_balance)->$($p2.data.point_balance) reward=$($si.data.reward_points) days=$($si.data.continuous_days) already=$($si.data.already_signed)"
    }
    catch { Add-Result "5.SignIn" "FAIL" (Get-ErrBody $_) }

    # ---------- 3. real-name ----------
    try {
        $rn = Post-Json "$BaseUrl/real-name" @{ real_name = "Zhang San"; id_card_no = $IdCardNo }
        $rg = Get-Api "$BaseUrl/real-name"
        Add-Result "3.RealName" "PASS" "name=$($rn.data.real_name_masked) card=$($rn.data.id_card_masked) status=$($rg.data.real_name_status)/$($rg.data.record_status)"
    }
    catch {
        $msg = Get-ErrBody $_
        # Conflict means this sample ID is already bound / already verified: expected, not a bug.
        if ($msg -match "BUSINESS_CONFLICT") {
            Add-Result "3.RealName" "SKIP" "conflict: $msg"
        }
        else { Add-Result "3.RealName" "FAIL" $msg }
    }

    # ---------- extra. notifications ----------
    try {
        $n = Get-Api "$BaseUrl/notifications?page=1&page_size=10"
        Add-Result "7.Notifications" "PASS" "total=$($n.data.total) unread=$($n.data.unread_count)"
    }
    catch { Add-Result "7.Notifications" "FAIL" (Get-ErrBody $_) }
}
catch {
    Write-Host ("Fatal: " + (Get-ErrBody $_)) -ForegroundColor Red
    exit 1
}

$script:results | Format-Table -AutoSize

$failed = @($script:results | Where-Object { $_.Status -eq "FAIL" })
if ($failed.Count -gt 0) {
    Write-Host "$($failed.Count) step(s) FAILED" -ForegroundColor Red
    exit 1
}
Write-Host "ALL PASS" -ForegroundColor Green
exit 0
