# Rebuilds the admin UI into internal/admin/web, the copy the Go binary
# embeds for `go vet`, `go test` and builds outside Docker. The container
# image builds its own copy (see Dockerfile). Needs Node.js.

# Failures are caught by exit code: npm prints progress on stderr, which
# PowerShell 5.1 turns into errors when output is redirected.
Set-Location (Join-Path (Split-Path $PSScriptRoot -Parent) internal\admin\ui) -ErrorAction Stop

function Check($what) {
    if ($LASTEXITCODE -ne 0) { throw "$what failed (exit code $LASTEXITCODE)" }
}

npm ci --no-audit --no-fund
Check "npm ci"
npm run build
Check "admin UI build"

Write-Host "Built internal\admin\web"
