# Mailer's container: the mail server with its web dashboard, cloudflared
# (inbound mail through the Cloudflare Tunnel) and Tailscale (Funnel for
# mail apps). Nothing needs publishing beyond the dashboard.
# The UI and server stages run on the build machine and cross-compile, so
# multi-platform images build without emulating Node or Go.

FROM --platform=$BUILDPLATFORM node:24.22.1-alpine AS ui
WORKDIR /src/internal/admin/ui
COPY internal/admin/ui/package.json internal/admin/ui/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY internal/admin/ui/ ./
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=ui /src/internal/admin/web/ internal/admin/web/
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/mailer ./cmd/mailer

FROM tailscale/tailscale:v1.104.2 AS tailscale
FROM cloudflare/cloudflared:2026.10.4 AS cloudflared

FROM alpine:3.24
RUN apk add --no-cache ca-certificates
COPY --from=tailscale /usr/local/bin/tailscale /usr/local/bin/tailscaled /usr/local/bin/
COPY --from=cloudflared /usr/local/bin/cloudflared /usr/local/bin/
COPY --from=build /out/mailer /usr/local/bin/
COPY docker/entrypoint.sh /entrypoint.sh
ENV MAILER_DATA=/data MAILER_LISTEN=0.0.0.0
VOLUME /data
# Dashboard, and the Cloudflare sign-in callback.
EXPOSE 8080 8877
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
