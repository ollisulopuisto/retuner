#!/bin/sh
# A station that fails once is not a dead station.
#
#   ./script/test-preset-strikes.sh
#
# make-preset.py --prune drops any entry that does not answer with audio, which
# is right when a human is watching the output and wrong when a weekly job is
# doing it unattended: one CDN hiccup at 04:00 on a Sunday and a national
# broadcaster is gone from the preset. --strikes makes the removal take several
# consecutive failures, and this checks the counting -- including that a
# recovery clears the count, so three failures spread over a year never add up
# to a removal.
#
# Needs no network: script/testdata/mock-stream.py stands in for the streams.
set -eu

# shellcheck disable=SC1007  # CDPATH= is a deliberate empty assignment for cd
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GEN="$ROOT/script/make-preset.py"
MOCK="$ROOT/script/testdata/mock-stream.py"

# urllib reads http_proxy, and a proxy in the environment would send these
# requests somewhere other than the mock -- which fails as a dead stream and
# would make every station look dead.
no_proxy=127.0.0.1,localhost
NO_PROXY=$no_proxy
export no_proxy NO_PROXY

WORK=$(mktemp -d)
MOCK_PID=""
cleanup() {
  if [ -n "$MOCK_PID" ]; then
    kill "$MOCK_PID" 2>/dev/null || true
    wait "$MOCK_PID" 2>/dev/null || true
  fi
  rm -rf "$WORK"
}
trap cleanup EXIT INT TERM PIPE

fail=0
has() {
  if printf '%s' "$2" | grep -q "$3"; then echo "  ok   $1"
  else echo "  FAIL $1 (expected '$3')"; printf '       got: %.300s\n' "$2"; fail=1; fi
}
hasnt() {
  if printf '%s' "$2" | grep -q "$3"; then
    echo "  FAIL $1 (did not expect '$3')"; printf '       got: %.300s\n' "$2"; fail=1
  else echo "  ok   $1"; fi
}
is() {
  if [ "$2" = "$3" ]; then echo "  ok   $1"
  else echo "  FAIL $1 (want '$3', got '$2')"; fail=1; fi
}

# Each phase gets its own port and its own mock. Sharing them would let a mock
# that failed to bind leave the previous phase's server answering, which has
# produced confident and completely false results in this project twice.
start_mock() { # $1 = port, remaining = paths that should 404
  port=$1; shift
  # Whoever is already on this port would answer the readiness probe below and
  # be mistaken for ours for the rest of the run.
  if curl -fsS --noproxy '*' -o /dev/null -m 2 "http://127.0.0.1:$port/up" 2>/dev/null; then
    echo "error: something is already serving on port $port" >&2
    exit 1
  fi
  python3 "$MOCK" "$port" "$@" >/dev/null 2>&1 &
  MOCK_PID=$!
  i=0
  while [ "$i" -lt 50 ]; do
    if curl -fsS --noproxy '*' --max-time 15 -o /dev/null "http://127.0.0.1:$port/up" 2>/dev/null; then
      return 0
    fi
    kill -0 "$MOCK_PID" 2>/dev/null || { echo "error: mock died" >&2; exit 1; }
    i=$((i + 1)); sleep 0.1
  done
  echo "error: mock did not start on $port" >&2; exit 1
}
stop_mock() {
  [ -n "$MOCK_PID" ] || return 0
  kill "$MOCK_PID" 2>/dev/null || true
  wait "$MOCK_PID" 2>/dev/null || true
  MOCK_PID=""
}

write_preset() { # $1 = file, $2 = port
  cat > "$1" <<EOF
; test preset
[Pop]
Steady FM=http://127.0.0.1:$2/steady
Flaky FM=http://127.0.0.1:$2/flaky
EOF
}

count_of() { # $1 = strikes file, $2 = station name -- prints the strike count
  python3 - "$1" "$2" <<'PY'
import json, sys
try:
    data = json.load(open(sys.argv[1]))
except (OSError, ValueError):
    print("no-file"); raise SystemExit
found = [e["strikes"] for f in data.values() for k, e in f.items()
         if sys.argv[2] in k]
print(found[0] if found else "absent")
PY
}

echo "Testing preset strike counting"

# --- a station that fails three weeks running ---------------------------------
echo "- a dead station is dropped only after the third failure"
PORT=18700
STRIKES="$WORK/strikes.json"
P="$WORK/fi.ini"
start_mock "$PORT" /flaky
write_preset "$P" "$PORT"

"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >"$WORK/run1.log" 2>&1 || true
is   "one failure is one strike"          "$(count_of "$STRIKES" 'Flaky FM')" "1"
has  "and the station is still in the file" "$(cat "$P")" "Flaky FM"
has  "the healthy station is kept"          "$(cat "$P")" "Steady FM"
is   "a station that played has no strikes" "$(count_of "$STRIKES" 'Steady FM')" "absent"

"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >"$WORK/run2.log" 2>&1 || true
is   "two failures is two strikes"        "$(count_of "$STRIKES" 'Flaky FM')" "2"
has  "still not dropped"                  "$(cat "$P")" "Flaky FM"

"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >"$WORK/run3.log" 2>&1 || true
hasnt "the third failure drops it"        "$(cat "$P")" "Flaky FM"
has   "and says so"                       "$(cat "$WORK/run3.log")" "Flaky FM"
has   "the healthy station survives all three passes" "$(cat "$P")" "Steady FM"
is    "the dropped station is forgotten, not left counting" \
      "$(count_of "$STRIKES" 'Flaky FM')" "absent"
stop_mock

# --- a station that comes back ------------------------------------------------
# Three failures spread over a year are three hiccups, not a dead station. Only
# consecutive failures count, so a single good week has to clear the record.
echo "- a station that recovers starts from zero again"
PORT=18701
STRIKES="$WORK/strikes2.json"
P="$WORK/se.ini"
start_mock "$PORT" /flaky
write_preset "$P" "$PORT"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "two strikes before it recovers"     "$(count_of "$STRIKES" 'Flaky FM')" "2"
stop_mock

start_mock "$PORT"                        # nothing is dead this time
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "one good week clears the record"    "$(count_of "$STRIKES" 'Flaky FM')" "absent"
stop_mock

start_mock "$PORT" /flaky
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
has  "and it takes three more failures to drop it" "$(cat "$P")" "Flaky FM"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
hasnt "which the third one does"          "$(cat "$P")" "Flaky FM"
stop_mock

# --- an edited URL is a different station -------------------------------------
# Someone fixing a broken URL by hand should not inherit the strikes the old one
# collected and lose the station on the next run.
echo "- editing the URL clears the strikes it had"
PORT=18702
STRIKES="$WORK/strikes3.json"
P="$WORK/no.ini"
start_mock "$PORT" /flaky /mended
write_preset "$P" "$PORT"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "two strikes against the old URL"    "$(count_of "$STRIKES" 'Flaky FM')" "2"
sed -i.bak "s|/flaky|/mended|" "$P"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "the new URL starts at one"          "$(count_of "$STRIKES" 'Flaky FM')" "1"
has  "so the station is still there"      "$(cat "$P")" "Flaky FM"
stop_mock

# --- housekeeping -------------------------------------------------------------
# A record for a station nobody ships any more is dead weight that never
# expires, and the file is committed by the weekly job.
echo "- records for stations no longer in the file are forgotten"
PORT=18703
STRIKES="$WORK/strikes4.json"
P="$WORK/dk.ini"
start_mock "$PORT" /flaky
write_preset "$P" "$PORT"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "the station has a record"           "$(count_of "$STRIKES" 'Flaky FM')" "1"
sed -i.bak "/Flaky FM/d" "$P"
"$GEN" --prune "$P" --strikes "$STRIKES" --timeout 3 >/dev/null 2>&1 || true
is   "removing it by hand clears the record" "$(count_of "$STRIKES" 'Flaky FM')" "absent"
stop_mock

# --- without --strikes, nothing changes ---------------------------------------
# The unattended job is the only caller that wants patience. A human running
# --prune is watching the output and expects the old behaviour.
echo "- --prune on its own still drops on the first failure"
PORT=18704
P="$WORK/ee.ini"
start_mock "$PORT" /flaky
write_preset "$P" "$PORT"
"$GEN" --prune "$P" --timeout 3 >/dev/null 2>&1 || true
hasnt "dropped straight away"             "$(cat "$P")" "Flaky FM"
has   "and the good one is kept"          "$(cat "$P")" "Steady FM"
stop_mock

# --- the weekly wrapper -------------------------------------------------------
# script/refresh-presets.sh is what the scheduled workflow runs. Two things
# about it are worth asserting: an empty presets/ is not a failure (the
# generator ships before the files do), and one country failing does not stop
# the next country being checked.
echo "- the weekly wrapper walks every preset"
PORT=18705
REFRESH="$ROOT/script/refresh-presets.sh"

EMPTY="$WORK/empty"
mkdir -p "$EMPTY"
# set -e would kill the suite before $? could be read, which would make the
# exit-status check below assert nothing at all.
if OUT=$("$REFRESH" "$EMPTY" "$WORK/strikes-empty.json" 2>&1); then RC=0; else RC=$?; fi
is   "an empty preset directory is not a failure" "$RC" "0"
has  "and says why there was nothing to do"       "$OUT" "nothing to re-check"

MANY="$WORK/many"
mkdir -p "$MANY"
start_mock "$PORT" /flaky
write_preset "$MANY/fi.ini" "$PORT"
write_preset "$MANY/se.ini" "$PORT"
if OUT=$("$REFRESH" "$MANY" "$WORK/strikes-many.json" 2>&1); then RC=0; else RC=$?; fi
is   "a run over several presets succeeds"   "$RC" "0"
has  "the first country is checked"          "$OUT" "fi.ini"
has  "and so is the one after it"            "$OUT" "se.ini"
has  "the failing station is reported"       "$OUT" "Flaky FM"
is   "each country keeps its own record" \
     "$(python3 -c "import json,sys; print(len(json.load(open(sys.argv[1]))))" "$WORK/strikes-many.json")" "2"
stop_mock

if [ "$fail" -ne 0 ]; then
  echo "--- logs ---" >&2
  cat "$WORK"/run*.log 2>/dev/null >&2 || true
  exit 1
fi
echo "All strike checks passed."
