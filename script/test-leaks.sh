#!/bin/sh
# Does the heap grow with load?
#
#   CHECKED=1 ./script/build.sh && ./script/test-leaks.sh
#
# Needs a binary built with CHECKED=1, which links heaptrc and leaves cmem out
# so heaptrc can see the whole heap -- see the uses clause in src/retuner.pas
# for why that ordering matters.
#
# The program ends every run with a small fixed residue (3 blocks, 120 bytes at
# the time of writing), which is the intentional Indy leaks noted in
# src/retuner.pas. A fixed residue is not what this looks for. It serves a light
# load and a heavy one and compares: anything leaked per request shows up as
# growth between the two, and growth is what fails.
set -eu

# shellcheck disable=SC1007  # CDPATH= is a deliberate empty assignment for cd
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
BIN=${1:-}
if [ -z "$BIN" ]; then
  # shellcheck disable=SC2012  # one glob under bin/, named by the target triple
  BIN=$(ls "$ROOT"/bin/*/retuner 2>/dev/null | head -1) \
    || { echo "error: no binary found; run CHECKED=1 ./script/build.sh first" >&2; exit 1; }
fi
[ -x "$BIN" ] || { echo "error: $BIN is not executable" >&2; exit 1; }

PORT=${PORT:-18900}
WORK=$(mktemp -d)
cleanup() { rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

# Ports are passed in, never taken from a shared variable. An earlier version
# incremented a global inside these functions, but each call site is a command
# substitution -- a subshell -- so the increment never escaped and both phases
# ran on the same ports, in the same directory. The result looked completely
# convincing: a knowingly leaky binary reported an identical figure at both
# loads and the check passed it.
#
# $1 = request pairs to serve, $2 = web port, $3 = maintenance port.
# Prints the unfreed byte count.
unfreed_after() {
  pairs=$1
  PORT=$2
  MPORT=$3
  run=$WORK/run$PORT
  mkdir -p "$run"
  cp "$BIN" "$run/retuner"
  cat > "$run/retuner.ini" <<INI
[Configuration]
INIVersion=1.2.2
MessageInfoLevel=1
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$PORT
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
[MaintenanceServer]
Enable=1
MaintenanceServerIPAddress=127.0.0.1
MaintenanceServerPort=$MPORT
INI
  printf '[Test]\nA=http://127.0.0.1:1/a.mp3\n' > "$run/stations.ini"

  ( cd "$run" && HEAPTRC="log=$run/heap.txt" exec ./retuner -p "$PORT" > server.log 2>&1 ) &
  pid=$!
  i=0
  while [ "$i" -lt 60 ]; do
    if curl -fsS --noproxy '*' --max-time 15 -o /dev/null \
         "http://127.0.0.1:$PORT/setupapp/x/loginxml.asp?token=0" 2>/dev/null; then
      break
    fi
    kill -0 "$pid" 2>/dev/null || { echo "error: server exited during startup" >&2; cat "$run/server.log" >&2; exit 1; }
    i=$((i + 1)); sleep 0.2
  done

  i=0
  while [ "$i" -lt "$pairs" ]; do
    curl -s --noproxy '*' --max-time 15 -o /dev/null "http://127.0.0.1:$PORT/retuner/mystations?mac=aabbccddee"
    curl -s --noproxy '*' --max-time 15 -o /dev/null "http://127.0.0.1:$PORT/retuner/mystations/Test?mac=aabbccddee"
    i=$((i + 1))
  done

  # heaptrc only reports on a clean exit, so the process is asked to stop
  # rather than killed.
  curl -s --noproxy '*' --max-time 15 -o /dev/null "http://127.0.0.1:$MPORT/retuner/down" || true
  i=0
  while [ "$i" -lt 100 ]; do
    kill -0 "$pid" 2>/dev/null || break
    i=$((i + 1)); sleep 0.2
  done
  if kill -0 "$pid" 2>/dev/null; then
    kill "$pid" 2>/dev/null || true
    echo "error: server did not shut down cleanly, so heaptrc wrote no report" >&2
    exit 1
  fi

  [ -f "$run/heap.txt" ] || {
    echo "error: no heaptrc report. Was the binary built with CHECKED=1?" >&2
    exit 1
  }
  sed -n 's/^\([0-9]*\) unfreed memory blocks : \([0-9]*\)$/\2/p' "$run/heap.txt" | head -1
}

echo "Checking whether the heap grows with load, using $BIN"

LIGHT=$(unfreed_after 1  "$PORT"            "$((PORT + 1))")
HEAVY=$(unfreed_after 25 "$((PORT + 2))"   "$((PORT + 3))")
[ -n "$LIGHT" ] && [ -n "$HEAVY" ] || {
  echo "error: could not read an unfreed-bytes figure from the heaptrc report" >&2
  exit 1
}

echo "  2 requests  -> $LIGHT bytes unfreed"
echo "  50 requests -> $HEAVY bytes unfreed"

if [ "$HEAVY" -gt "$LIGHT" ]; then
  echo "  FAIL the heap grew by $((HEAVY - LIGHT)) bytes over 48 more requests."
  echo "       A fixed residue is expected; growth means something is leaked per request."
  exit 1
fi
echo "  ok   no growth: the residue is fixed, so nothing leaks per request."
echo "All leak checks passed."
