#!/bin/sh
# Where a station logo comes from when the directory has not got one.
#
#   ./script/test-preset-icons.sh
#
# Most radio-browser entries carry no favicon, so most generated presets have
# no logos and every station on the receiver shows the same Retuner mark. The
# station's own homepage usually knows where its icon is. This checks that
# make-preset.py reads it, and -- more to the point -- that it refuses what the
# page is wrong or lying about: a link to an HTML page, four megabytes of
# something claiming to be a PNG, a 200 with no body.
#
# Needs no network: script/testdata/mock-website.py is both the directory and
# the station homepages it points at.
set -eu

# shellcheck disable=SC1007  # CDPATH= is a deliberate empty assignment for cd
ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
GEN="$ROOT/script/make-preset.py"
MOCK="$ROOT/script/testdata/mock-website.py"

# urllib reads http_proxy, and a proxy in the environment would send these
# requests somewhere other than the mock.
no_proxy=127.0.0.1,localhost
NO_PROXY=$no_proxy
export no_proxy NO_PROXY

WORK=$(mktemp -d)
SITE_PID=""
cleanup() {
  if [ -n "$SITE_PID" ]; then
    kill "$SITE_PID" 2>/dev/null || true
    wait "$SITE_PID" 2>/dev/null || true
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

start_site() { # $1 = port, $2 = "nofavicon" or empty
  # A mock left over from an interrupted run answers the readiness probe below
  # just as well as our own, and then every assertion is about its idea of the
  # station list rather than this one's. That is not hypothetical: it happened
  # here, with a mock orphaned by a SIGPIPE, still serving the list from before
  # a station was added.
  if curl -fsS --noproxy '*' -o /dev/null -m 2 "http://127.0.0.1:$1/plain/" 2>/dev/null; then
    echo "error: something is already serving on port $1" >&2
    exit 1
  fi
  python3 "$MOCK" "$1" ${2:+"$2"} >/dev/null 2>&1 &
  SITE_PID=$!
  i=0
  while [ "$i" -lt 50 ]; do
    if curl -fsS --noproxy '*' --max-time 15 -o /dev/null "http://127.0.0.1:$1/plain/" 2>/dev/null; then
      return 0
    fi
    kill -0 "$SITE_PID" 2>/dev/null || { echo "error: mock site died" >&2; exit 1; }
    i=$((i + 1)); sleep 0.1
  done
  echo "error: mock site did not start on $1" >&2; exit 1
}
stop_site() {
  [ -n "$SITE_PID" ] || return 0
  kill "$SITE_PID" 2>/dev/null || true
  wait "$SITE_PID" 2>/dev/null || true
  SITE_PID=""
}

# The line for one station, logo and all.
line_of() { # $1 = file, $2 = station name
  grep "^$2=" "$1" 2>/dev/null || true
}

echo "Testing where a station logo comes from"

# --- a site that says where its icon is ---------------------------------------
echo "- the homepage is read when the directory has no favicon"
PORT=18800
P="$WORK/xx.ini"
start_site "$PORT"
# --per-category 20: every station in this fixture is tagged pop, and the
# default cap of 8 silently dropped the ninth -- which was the one proving the
# directory's own favicon is preferred.
"$GEN" --country xx --out "$P" --api "http://127.0.0.1:$PORT" --timeout 5 --per-category 20 \
  >"$WORK/build.log" 2>&1 || true

has  "a root-relative href is resolved"   "$(line_of "$P" 'Plain FM')" \
     "|http://127.0.0.1:$PORT/plain/icon.png"
has  "an absolute href is taken as it is" "$(line_of "$P" 'Absolute FM')" \
     "|http://127.0.0.1:$PORT/absolute/logo.png"
has  "an apple-touch-icon counts, and a page-relative href resolves" \
     "$(line_of "$P" 'Apple FM')" "|http://127.0.0.1:$PORT/apple/apple.png"
has  "a site with no link at all still yields /favicon.ico" \
     "$(line_of "$P" 'Bare FM')" "|http://127.0.0.1:$PORT/favicon.ico"

# Each of these pages points at something that is not an icon, and each site
# also has a real /favicon.ico. So the assertion is in two halves: the thing the
# page named is not used, and the scraper did not give up on the site either.
# Checking only the first would pass against a scraper that fetched nothing at
# all, which is the mistake this suite was written with.
echo "- and what the page is wrong about is refused, without giving up"
hasnt "a link to an HTML page is not a logo"  "$(line_of "$P" 'Notimage FM')" \
      "page.html"
has   "the site's own favicon is used instead" "$(line_of "$P" 'Notimage FM')" \
      "|http://127.0.0.1:$PORT/favicon.ico"
hasnt "four megabytes is not an icon"         "$(line_of "$P" 'Huge FM')" \
      "enormous.png"
has   "and that site falls back too"          "$(line_of "$P" 'Huge FM')" \
      "|http://127.0.0.1:$PORT/favicon.ico"
hasnt "a 200 with no body is not an icon"     "$(line_of "$P" 'Empty FM')" \
      "nothing.png"
has   "nor does an empty body stop the fallback" "$(line_of "$P" 'Empty FM')" \
      "|http://127.0.0.1:$PORT/favicon.ico"
# Content-Length is a claim. This one sends four megabytes and declares
# nothing, so only a reader that bounds what actually arrives stops early --
# the case a mutation of the arriving-size check survived without.
hasnt "an undeclared four megabytes is not an icon either" \
      "$(line_of "$P" 'Unmeasured FM')" "flood.png"
has   "that site falls back as well"          "$(line_of "$P" 'Unmeasured FM')" \
      "|http://127.0.0.1:$PORT/favicon.ico"
has   "the stations themselves are still kept" "$(cat "$P")" "Notimage FM="

echo "- a station the directory already has a logo for is left alone"
has  "the directory's own favicon is used"    "$(line_of "$P" 'Known FM')" \
     "|http://127.0.0.1:$PORT/absolute/logo.png"
hasnt "and its homepage was never fetched for one" \
     "$(line_of "$P" 'Known FM')" "/plain/icon.png"
stop_site

# --- a site with no icon anywhere ---------------------------------------------
# The scraper's last guess is <origin>/favicon.ico, which either exists or does
# not; a second instance is the only way to see the other half.
echo "- a site with no icon anywhere leaves the station without one"
PORT=18801
P="$WORK/yy.ini"
start_site "$PORT" nofavicon
"$GEN" --country yy --out "$P" --api "http://127.0.0.1:$PORT" --timeout 5 --per-category 20 \
  >"$WORK/build2.log" 2>&1 || true
hasnt "no logo is invented"                   "$(line_of "$P" 'Bare FM')" "|http"
has   "and the station is still listed"       "$(cat "$P")" "Bare FM="
stop_site

# --- the switch ---------------------------------------------------------------
# Scraping means a request to every logo-less station's own site. That is worth
# being able to turn off, and worth proving it is off when it is off.
echo "- --no-icons does not go to the stations' sites at all"
PORT=18802
P="$WORK/zz.ini"
start_site "$PORT"
"$GEN" --country zz --out "$P" --api "http://127.0.0.1:$PORT" --timeout 5 --per-category 20 \
  --no-icons >"$WORK/build3.log" 2>&1 || true
hasnt "nothing was scraped"                   "$(line_of "$P" 'Plain FM')" "|http"
has   "the directory's own favicon still is"  "$(line_of "$P" 'Known FM')" \
      "|http://127.0.0.1:$PORT/absolute/logo.png"
stop_site

if [ "$fail" -ne 0 ]; then
  echo "--- logs ---" >&2
  cat "$WORK"/build*.log 2>/dev/null >&2 || true
  exit 1
fi
echo "All logo checks passed."
