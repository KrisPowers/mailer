# Starts or updates the whole mail server. Run it any time:
#   - installs Git, Go, Node.js and Tailscale if they're missing
#   - pulls the latest code
#   - rebuilds the admin UI and the server from a copy of the code, so the
#     checkout itself is never changed and the next pull stays clean
#   - stops the running server and its tunnel, installs the new build and
#     starts it as a boot task
#   - re-applies Cloudflare (Worker, Email Routing, DNS) for every domain and
#     Tailscale Funnel, then shows the status checks
#
# The server and its data live in %ProgramData%\mailer, outside OneDrive,
# readable only by Administrators and SYSTEM.

$ErrorActionPreference = "Stop"

# Elevate: the boot task runs as SYSTEM and the install folder is locked down.
$admin = ([Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()).IsInRole(
    [Security.Principal.WindowsBuiltInRole]::Administrator)
if (-not $admin) {
    Start-Process powershell -Verb RunAs -ArgumentList "-NoExit", "-ExecutionPolicy", "Bypass", "-File", "`"$PSCommandPath`""
    exit
}

$repo = $PSScriptRoot
$src = Join-Path $env:TEMP "mailer-src"
$root = Join-Path $env:ProgramData "mailer"
$exe = Join-Path $root "mailer.exe"
$data = Join-Path $root "data"

function Step($text) { Write-Host "`n== $text" -ForegroundColor Cyan }

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

function Refresh-Path {
    $env:Path = [Environment]::GetEnvironmentVariable("Path", "Machine") + ";" +
        [Environment]::GetEnvironmentVariable("Path", "User")
}

# Need installs a tool with winget unless its command (or known path) exists.
function Need($command, $wingetId, $knownPath) {
    if (Get-Command $command -ErrorAction SilentlyContinue) { return }
    if ($knownPath -and (Test-Path $knownPath)) { return }
    Write-Host "Installing $wingetId"
    winget install --id $wingetId --exact --silent --accept-source-agreements --accept-package-agreements
    Check "winget install $wingetId"
    Refresh-Path
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
# cmd does the redirect: PowerShell 5.1 turns redirected native stderr into
# errors, which would stop this script when the task doesn't exist yet.
cmd /c "schtasks /End /TN Mailer >nul 2>&1"
# The task's process and the tunnel connector it started, if still around.
Get-Process mailer, cloudflared -ErrorAction SilentlyContinue |
    Where-Object { $_.Path -and $_.Path.StartsWith($root, [StringComparison]::OrdinalIgnoreCase) } |
    Stop-Process -Force
Start-Sleep -Seconds 2

Step "Installing to $root"
New-Item -ItemType Directory -Force $data | Out-Null
icacls $root /inheritance:r /grant:r "*S-1-5-18:(OI)(CI)F" "*S-1-5-32-544:(OI)(CI)F" | Out-Null
Check "locking down $root"
Move-Item -Force $build $exe
& $exe -data $data install
Check "mailer install"

Step "Waiting for the server"
$port = 8080
$settings = Join-Path $data "settings.json"
if (Test-Path $settings) {
    $p = (Get-Content $settings -Raw | ConvertFrom-Json).ports.admin
    if ($p) { $port = $p }
}
$url = "http://127.0.0.1:$port"
$headers = @{ "X-Mailer" = "1" }
$ov = $null
for ($i = 0; $i -lt 30 -and -not $ov; $i++) {
    try { $ov = Invoke-RestMethod "$url/api/overview" -TimeoutSec 5 } catch { Start-Sleep -Seconds 1 }
}
if (-not $ov) { throw "the server didn't answer on $url; check Task Scheduler > Mailer" }
Write-Host "Running at $url"

Step "Tailscale"
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
    if ($r.action_url) { Write-Host "Approve here, then run this script again: $($r.action_url)" -ForegroundColor Yellow }
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

Start-Process $url
Write-Host "`nDone. Admin page: $url"
