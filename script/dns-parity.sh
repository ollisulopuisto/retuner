#!/bin/sh
# Start the reference (Pascal) and the Go binary with the DNS server on, and
# compare their raw replies. Upstream forwarding is off (DNSServers empty), so
# only what the two servers decide locally is compared.
#
#   ./script/dns-parity.sh <reference-binary> <go-binary>
# [HISTORICAL] This test verified byte parity against the legacy Free Pascal reference binary.
# The Pascal implementation was decommissioned; this script is preserved for historical reference.
echo "Skipping: Parity test archived (Pascal reference decommissioned)." >&2
exit 0

set -eu
REF=${1:?reference binary}
NEW=${2:?go binary}
# shellcheck disable=SC1007
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
WORK=$(mktemp -d)
PIDS=""
cleanup() { for p in $PIDS; do kill "$p" 2>/dev/null || true; wait "$p" 2>/dev/null || true; done; rm -rf "$WORK"; }
trap cleanup EXIT INT TERM
n=0
for who in ref new; do
  n=$((n + 1))
  bin=$REF; [ "$who" = new ] && bin=$NEW
  mkdir -p "$WORK/$who"
  cp "$bin" "$WORK/$who/retuner"
  cat > "$WORK/$who/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=1860$n
[DNSServer]
Enable=1
DNSServerIPAddress=127.0.0.1
DNSServerPort=1870$n
DNSAdvertiseIP=10.1.2.3
DNSServers=
[RadioBrowser]
Enable=0
[MyStations]
Enable=0
EOT
  ( cd "$WORK/$who" && exec ./retuner > server.log 2>&1 ) &
  PIDS="$PIDS $!"
done
sleep 1.5
python3 "$ROOT/script/testdata/dns-parity.py" 18701 18702
