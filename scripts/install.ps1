# Installs or updates Mailer on Windows:
#
#   irm https://raw.githubusercontent.com/KrisPowers/mailer/main/scripts/install.ps1 | iex
#
# Downloads compose.yaml into $env:MAILER_HOME (default
# %LOCALAPPDATA%\mailer), pulls the published image and starts the
# container. Running it again updates.

# A script block, so a failure returns instead of closing the window
# the one-liner was pasted into.
& {
    $repo = 'https://raw.githubusercontent.com/KrisPowers/mailer/main'
    $dir = if ($env:MAILER_HOME) { $env:MAILER_HOME } else { Join-Path $env:LOCALAPPDATA 'mailer' }

    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) {
        Write-Host 'Mailer runs in Docker. Install Docker Desktop first: https://docs.docker.com/desktop/setup/install/windows-install/' -ForegroundColor Red
        return
    }

    New-Item -ItemType Directory -Force $dir | Out-Null
    $compose = Join-Path $dir 'compose.yaml'
    try {
        Invoke-WebRequest "$repo/compose.yaml" -OutFile $compose -UseBasicParsing
    } catch {
        Write-Host "Couldn't download compose.yaml: $_" -ForegroundColor Red
        return
    }

    # Failures are caught by exit code: docker prints progress on stderr.
    docker compose -f $compose pull
    if ($LASTEXITCODE -ne 0) { Write-Host 'Pulling the Mailer image failed.' -ForegroundColor Red; return }
    docker compose -f $compose up -d
    if ($LASTEXITCODE -ne 0) { Write-Host 'Starting Mailer failed.' -ForegroundColor Red; return }

    Write-Host ''
    Write-Host 'Mailer is running. Open http://localhost:8080 on this computer to finish setup and make the admin account.' -ForegroundColor Green
    Write-Host "Data and settings: $(Join-Path $env:LOCALAPPDATA 'mailer\data')"
}
