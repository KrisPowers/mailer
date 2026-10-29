# Development

## Layout

| Path | What's there |
|---|---|
| `cmd/mailer` | The server's entry point |
| `internal/` | Packages: IMAP and SMTP servers, store, queue, Cloudflare setup, Tailscale, tunnel, admin API |
| `internal/admin/ui` | The dashboard: React and TypeScript, built with Vite |
| `internal/admin/web` | The committed dashboard build the Go binary embeds |
| `internal/cloudflare/worker` | The Email Routing Worker deployed to Cloudflare |
| `docker/` | The container's entrypoint |
| `scripts/` | Installers and the dashboard build script |

## Server

Needs Go (version in `go.mod`).

```sh
go vet ./...
go test ./...
MAILER_NET_TESTS=1 go test ./internal/tunnel   # also tests the real cloudflared download
```

## Dashboard

The image builds the dashboard itself. A copy in `internal/admin/web` is committed
so `go vet`, `go test` and builds outside Docker work without Node. Rebuild it after
changing the UI, with `scripts/build.ps1` or:

```sh
cd internal/admin/ui
npm install
npm run dev     # hot reload on :5173, proxies /api to a running mailer on :8080
npm run build   # type-check, then rebuild internal/admin/web; commit the result
```

CI fails a pull request whose committed build is out of date.

## Container

```sh
docker compose up -d --build
```

## CI and releases

`CI` runs on every pull request: `go vet`, `go test` and `govulncheck`, an `npm audit`
and rebuild of the dashboard, and the image build. `CodeQL` scans the Go and
TypeScript code. Dependabot opens updates for the actions, the base images and the
dashboard's packages; a dashboard update needs `internal/admin/web` rebuilt and
committed on its branch.

On every push to `main` and every `v*` tag, `Release` runs `CI` and, only if it passes, publishes the image
to `ghcr.io/krispowers/mailer` with an SBOM and provenance: `latest` from `main`,
and the version from `v*` tags.
