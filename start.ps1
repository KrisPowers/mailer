# Starts or updates the whole mail server, in this window:
#   - installs Git, Go, Node.js and Tailscale if they're missing
#   - pulls the latest code
#   - rebuilds the admin UI and the server from a copy of the code, so the
#     checkout itself is never changed and the next pull stays clean
#   - stops the running server, then starts the new build here
#   - re-applies Tailscale Funnel and Cloudflare (Worker, Email Routing, DNS)
#     for every domain and shows the status checks
# The server keeps running in this window; Ctrl+C or closing it stops it.
#
# The server and its data live in %LOCALAPPDATA%\mailer, outside OneDrive.

$ErrorActionPreference = "Stop"

$repo = $PSScriptRoot
$src = Join-Path $env:TEMP "mailer-src"
$root = Join-Path $env:LOCALAPPDATA "mailer"
$exe = Join-Path $root "mailer.exe"
$data = Join-Path $root "data"

function Step($text) { Write-Host "`n== $text" -ForegroundColor Cyan }

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

# Need installs a tool with winget unless its command (or known path) exists.
function Need($command, $wingetId, $knownPath) {
    if (Get-Command $command -ErrorAction SilentlyContinue) { return }
    if ($knownPath -and (Test-Path $knownPath)) { return }
    Write-Host "Installing $wingetId"
    winget install --id $wingetId --exact --silent --accept-source-agreements --accept-package-agreements
    Check "winget install $wingetId"
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
        [Environment]::GetEnvironmentVariable("Path", "User")
}

Step "Tools"
Need git "Git.Git"
Need go "GoLang.Go"
Need npm "OpenJS.NodeJS.LTS"
Need tailscale "Tailscale.Tailscale" "$env:ProgramFiles\Tailscale\tailscale.exe"
$tailscale = (Get-Command tailscale -ErrorAction SilentlyContinue).Source
if (-not $tailscale) { $tailscale = "$env:ProgramFiles\Tailscale\tailscale.exe" }

Step "Latest code"
Set-Location $repo
git pull --ff-only
Check "git pull"

Step "Admin UI"
robocopy $repo $src /MIR /XD .git node_modules /NFL /NDL /NJH /NJS /NP | Out-Null
if ($LASTEXITCODE -ge 8) { throw "copying the code to $src failed (robocopy exit code $LASTEXITCODE)" }
Push-Location (Join-Path $src "internal\admin\ui")
npm ci --no-audit --no-fund
Check "npm ci"
npm run build
Check "UI build"
Pop-Location

Step "Server"
$build = Join-Path $env:TEMP "mailer-build.exe"
Push-Location $src
go build -o $build ./cmd/mailer
Check "go build"
Pop-Location

Step "Stopping the running server"
# The previous version of this script ran the server as a SYSTEM boot task
# from %ProgramData%\mailer. One admin prompt removes it and moves its data
# here.
cmd /c "schtasks /Query /TN Mailer >nul 2>&1"
if ($LASTEXITCODE -eq 0) {
    $old = Join-Path $env:ProgramData "mailer"
    $cleanup = @"
schtasks /End /TN Mailer | Out-Null
schtasks /Delete /TN Mailer /F | Out-Null
Get-Process mailer, cloudflared -ErrorAction SilentlyContinue | Where-Object { `$_.Path -like '$old\*' } | Stop-Process -Force
Start-Sleep -Seconds 2
if ((Test-Path '$old\data') -and -not (Test-Path '$data')) { robocopy '$old\data' '$data' /E /NFL /NDL /NJH /NJS /NP | Out-Null }
Remove-Item -Recurse -Force '$old'
"@
    $enc = [Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($cleanup))
    Start-Process powershell -Verb RunAs -Wait -WindowStyle Hidden -ArgumentList "-NoProfile -EncodedCommand $enc"
}
# Any running copy holds the ports; its tunnel connector goes with it.
Get-Process mailer -ErrorAction SilentlyContinue | Stop-Process -Force
Get-Process cloudflared -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -and $_.Path.StartsWith($root, [StringComparison]::OrdinalIgnoreCase) } |
    Stop-Process -Force
Start-Sleep -Seconds 2

Step "Starting the server"
New-Item -ItemType Directory -Force $data | Out-Null
Move-Item -Force $build $exe
$app = Start-Process $exe -ArgumentList "-data", "`"$data`"" -NoNewWindow -PassThru

$port = 8080
$settings = Join-Path $data "settings.json"
if (Test-Path $settings) {
    $p = (Get-Content $settings -Raw | ConvertFrom-Json).ports.admin
    if ($p) { $port = $p }
}
$url = "http://127.0.0.1:$port"
$headers = @{ "X-Mailer" = "1" }
$ov = $null
for ($i = 0; $i -lt 30 -and -not $ov -and -not $app.HasExited; $i++) {
    try { $ov = Invoke-RestMethod "$url/api/overview" -TimeoutSec 5 } catch { Start-Sleep -Seconds 1 }
}
if (-not $ov) { throw "the server didn't start; see its output above" }

Step "Tailscale"
# cmd does the redirect: PowerShell 5.1 turns redirected native stderr into
# errors, which would stop this script.
cmd /c "`"$tailscale`" status >nul 2>&1"
if ($LASTEXITCODE -ne 0) {
    Write-Host "Signing in to Tailscale"
    & $tailscale up
    Check "tailscale up"
}
$r = Invoke-RestMethod -Method Post "$url/api/tailscale/enable" -Headers $headers -TimeoutSec 120
if ($r.ok) {
    Write-Host "Funnel on: $($r.dns_name) (IMAP :443, SMTP :8443)"
} else {
    Write-Host $r.error -ForegroundColor Yellow
    if ($r.action_url) { Write-Host "Approve here, then enable Funnel on the admin page: $($r.action_url)" -ForegroundColor Yellow }
}

Step "Cloudflare"
if (-not $ov.cloudflare_setup) {
    Write-Host "Not connected yet: paste your API token on the admin page." -ForegroundColor Yellow
} else {
    foreach ($d in $ov.domains) {
        $body = @{ token = ""; zone = $d } | ConvertTo-Json
        $r = Invoke-RestMethod -Method Post "$url/api/cloudflare/connect" -Headers $headers `
            -ContentType "application/json" -Body $body -TimeoutSec 300
        Write-Host $d
        foreach ($s in $r.steps) {
            $mark = if ($s.ok) { "ok  " } else { "FAIL" }
            Write-Host "  $mark $($s.name) $($s.detail)"
        }
        if ($r.error) { Write-Host "  $($r.error)" -ForegroundColor Red }
    }
}

Step "Status"
$c = Invoke-RestMethod -Method Post "$url/api/check" -Headers $headers -TimeoutSec 120
foreach ($item in $c.items) {
    $color = if ($item.ok) { "Green" } else { "Red" }
    Write-Host ("  {0,-40} {1}" -f $item.name, $item.detail) -ForegroundColor $color
}

Write-Host "`nMailer is running. Admin page: $url" -ForegroundColor Green
Write-Host "Ctrl+C or closing this window stops it."
Wait-Process -Id $app.Id
