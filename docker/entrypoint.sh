#!/bin/sh
# Starts Tailscale, then Mailer, and stops both together. Tailscale runs
# with userspace networking, so the container needs no extra privileges,
# and keeps its state in the data volume so this server keeps its name on
# the tailnet across restarts. Mailer signs it in with the name and key
# given in its first-run setup.
set -u

mkdir -p /data/tailscale /var/run/tailscale
tailscaled --tun=userspace-networking --statedir=/data/tailscale \
    --socket=/var/run/tailscale/tailscaled.sock &
ts_pid=$!
i=0
while [ ! -S /var/run/tailscale/tailscaled.sock ] && [ "$i" -lt 30 ]; do
    sleep 1
    i=$((i + 1))
done

mailer &
mailer_pid=$!
trap 'kill -TERM "$mailer_pid" 2>/dev/null' TERM INT
wait "$mailer_pid"
status=$?
# A signal interrupts wait before mailer has finished shutting down.
if kill -0 "$mailer_pid" 2>/dev/null; then
    wait "$mailer_pid"
    status=$?
fi
kill -TERM "$ts_pid" 2>/dev/null
wait "$ts_pid"
exit "$status"
