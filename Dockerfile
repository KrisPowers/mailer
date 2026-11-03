# Mailer's container: the mail server with its admin dashboard and webmail, cloudflared
# (inbound mail through the Cloudflare Tunnel) and Tailscale (Funnel for
# mail apps). Nothing needs publishing beyond the dashboard.
# The UI and server stages run on the build machine and cross-compile, so
# multi-platform images build without emulating Node or Go.

FROM --platform=$BUILDPLATFORM node:26.13.0-alpine AS ui
WORKDIR /src/internal/ui
COPY internal/ui/package.json internal/ui/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY internal/ui/ ./
COPY docs/ /src/docs/
RUN npm run build

FROM --platform=$BUILDPLATFORM golang:1.27.3-alpine AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod ./
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=ui /src/internal/admin/web/ internal/admin/web/
COPY --from=ui /src/internal/webmail/web/ internal/webmail/web/
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
# Admin dashboard, webmail, and the Cloudflare sign-in callback.
EXPOSE 8080 8081 8877
ENTRYPOINT ["/bin/sh", "/entrypoint.sh"]
