#!/bin/sh
# Differential check of the maintenance endpoint. A client that is not the
# configured address (127.0.0.2 against 127.0.0.1) must get 503, an unknown path
# 404, and /retuner/down from the right address stops the process. Compares status
# and body for each binary and whether the process exited.
#   ./script/maintenance-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
phase() {
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  cat > "$W/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=18761
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=0
[MaintenanceServer]
Enable=1
MaintenanceServerIPAddress=127.0.0.1
MaintenanceServerPort=18762
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & pid=$!; PIDS="$PIDS $pid"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:18762/x"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$W/$1/server.log"; exit 1; }; sleep 0.2; done
  out="$W/out-$1"; : > "$out"
  r() { echo "== $1 $2" >> "$out"; curl -s --noproxy '*' $1 -m 10 -w '\n%{http_code} %{content_type}' "http://127.0.0.1:18762$2" >> "$out"; echo >> "$out"; }
  r "--interface 127.0.0.2" /retuner/down
  r "" /nothing
  r "" /retuner
  echo "alive before: $(kill -0 $pid 2>/dev/null && echo y || echo n)" >> "$out"
  r "" /RETUNER/Down
  sleep 6
  echo "alive after: $(kill -0 $pid 2>/dev/null && echo y || echo n)" >> "$out"
  kill $pid 2>/dev/null || true
}
phase ref "$REF"; phase new "$NEW"
cat "$W/out-ref" > /tmp/m-ref; cat "$W/out-new" > /tmp/m-new
if cmp -s "$W/out-ref" "$W/out-new"; then echo "  same  maintenance replies and shutdown"; echo "All compared responses match."; else diff "$W/out-ref" "$W/out-new"; echo "Differences found."; exit 1; fi
