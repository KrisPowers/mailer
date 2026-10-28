#!/bin/sh
# Installs or updates Mailer on Linux and macOS:
#
#   curl -fsSL https://raw.githubusercontent.com/KrisPowers/Mailer/main/scripts/install.sh | sh
#
# Downloads compose.yaml into MAILER_HOME (default ~/.mailer), pulls the
# published image and starts the container. Running it again updates.
set -eu

repo="https://raw.githubusercontent.com/KrisPowers/Mailer/main"
dir="${MAILER_HOME:-$HOME/.mailer}"

if ! command -v docker >/dev/null 2>&1; then
    echo "Mailer runs in Docker. Install it first: https://docs.docker.com/get-docker/" >&2
    exit 1
fi
if ! docker compose version >/dev/null 2>&1; then
    echo "Mailer needs Docker Compose v2 (the 'docker compose' command)." >&2
    exit 1
fi

mkdir -p "$dir"
cd "$dir"
curl -fsSL "$repo/compose.yaml" -o compose.yaml
# Mail lives beside compose.yaml. An existing .env is left alone, so
# settings and keys put there survive updates.
if [ ! -f .env ]; then
    printf 'MAILER_DATA_DIR=./data\n' > .env
    chmod 600 .env
fi
docker compose pull
docker compose up -d

echo
echo "Mailer is running. Open http://localhost:8080 to finish setup."
echo "Data and settings: $dir"
