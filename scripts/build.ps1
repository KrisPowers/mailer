# Builds the Mailer desktop app into builds\Mailer.exe: the admin UI, the
# Windows icon and manifest, then the server and its window in one exe.
# Needs Go and Node.js. Opening Mailer.exe starts the mail server and shows
# its window; Mailer > Quit Mailer stops it.

$ErrorActionPreference = "Stop"
Set-Location (Split-Path $PSScriptRoot -Parent)

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

Push-Location internal\admin\ui
npm ci --no-audit --no-fund
Check "npm ci"
npm run build
Check "admin UI build"
Pop-Location

go mod tidy
Check "go mod tidy"

go run github.com/tc-hib/go-winres@v0.3.3 simply --arch amd64 --manifest gui --icon cmd\mailer\icon.png `
    --product-name Mailer --file-description Mailer --original-filename Mailer.exe --out cmd\mailer\rsrc
Check "icon and manifest"

New-Item -ItemType Directory -Force builds | Out-Null
go build -tags desktop,production -trimpath -ldflags "-w -s -H windowsgui" -o builds\Mailer.exe ./cmd/mailer
Check "go build"

Write-Host "Built builds\Mailer.exe"
