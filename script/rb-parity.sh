#!/bin/sh
# Differential check for the Radio Browser menu: a reference binary and the Go
# port each get their own copy of the radio-browser mock. Compares the pages
# they serve (status, content type, body) and the exact requests each sent
# upstream.
#
#   ./script/rb-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference binary}
NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
WORK=${KEEP_WORK:-$(mktemp -d)}; mkdir -p "$WORK"
PIDS=""
cleanup() { for p in $PIDS; do kill "$p" 2>/dev/null || true; wait "$p" 2>/dev/null || true; done; [ -n "${KEEP_WORK:-}" ] || rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

start() { # $1=name $2=bin $3=web port $4=mock port
  mkdir -p "$WORK/$1/config"
  cp "$2" "$WORK/$1/retuner"
  cp "$HERE/../cfg/avr.ini" "$WORK/$1/config/avr.ini" 2>/dev/null || true
  if [ -n "${RB_DIAC:-}" ]; then sed -i 's/^\(ReplaceUTF8Latin1[A-Za-z]*\)=0/\1=1/' "$WORK/$1/config/avr.ini"; fi
  RB_LOG="$WORK/$1.rblog" python3 "$HERE/testdata/mock-radiobrowser.py" "$4" ok >/dev/null 2>&1 &
  PIDS="$PIDS $!"
  i=0
  until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$4/json/servers"; do
    i=$((i+1)); [ $i -gt 50 ] && { echo "mock for $1 did not start"; exit 1; }
    sleep 0.1
  done
  : > "$WORK/$1.rblog"
  cat > "$WORK/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
LocalCountry=Testland
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$3
[DNSServer]
Enable=0
[RadioBrowser]
Enable=1
RBAPIURL=http://127.0.0.1:$4
RBMinStationsPerCategory=1
RBCacheType=${RB_CACHE:-catMemStr}
DBLibFile=default
[MyStations]
Enable=0
[Bookmark]
Enable=1
EOT
  ( cd "$WORK/$1" && exec ./retuner -p "$3" > server.log 2>&1 ) &
  PIDS="$PIDS $!"
  i=0
  while ! curl -fsS --noproxy '*' -o /dev/null "http://127.0.0.1:$3/setupapp/x/loginxml.asp?token=0" 2>/dev/null; do
    i=$((i+1)); [ $i -gt 50 ] && { echo "$1 did not start"; cat "$WORK/$1/server.log"; exit 1; }
    sleep 0.2
  done
}
start ref "$REF" 18211 18311
start new "$NEW" 18212 18312

fail=0
while IFS= read -r url; do
  [ -z "$url" ] && continue
  for n in ref new; do
    port=18211; [ $n = new ] && port=18212
    curl -s --noproxy '*' -o "$WORK/$n.body" -D "$WORK/$n.hdr" "http://127.0.0.1:$port$url" || true
    { sed -n 1p "$WORK/$n.hdr" | tr -d '\r'; grep -i '^content-type\|^location' "$WORK/$n.hdr" | tr -d '\r'; cat "$WORK/$n.body"; } > "$WORK/$n.cmp"
  done
  case $url in */icon*) # two JPEG encoders never agree to the byte: compare status and type only
    for n in ref new; do sed -n 1,2p "$WORK/$n.cmp" > "$WORK/$n.c2"; mv "$WORK/$n.c2" "$WORK/$n.cmp"; done ;; esac
  cat "$WORK/new.cmp" >> "$WORK/all.new"; cat "$WORK/ref.cmp" >> "$WORK/all.ref"
  if cmp -s "$WORK/ref.cmp" "$WORK/new.cmp"; then echo "  same  $url"; else echo "  DIFF  $url"; diff "$WORK/ref.cmp" "$WORK/new.cmp" | head -10; fail=1; fi
done <<'EOU'
/setupapp/x/loginxml.asp?mac=aabbccddee
/retuner/radiobrowser?mac=aabbccddee
/ytuner/radiobrowser
/setupapp/x/navxml.asp?gofile=S-ByLocation
/retuner/radiobrowser/country
/retuner/radiobrowser/country?start=1&howmany=1
/retuner/radiobrowser/country?startitems=2&enditems=5
/retuner/radiobrowser/genre
/retuner/radiobrowser/language
/retuner/radiobrowser/country/Testland
/retuner/radiobrowser/country/Testland?start=2&howmany=2
/retuner/radiobrowser/country/Testland?startitems=1&enditems=1
/retuner/radiobrowser/country/Nowhere
/retuner/radiobrowser/genre/test
/retuner/radiobrowser/language/testish
/retuner/radiobrowser/popular
/retuner/radiobrowser/popular/x
/retuner/radiobrowser/bogus
/retuner/radiobrowser/bogus/x
/retuner/radiobrowser/country/Testland/extra
/retuner/search?search=yle
/retuner/search?search=ab
/setupapp/x/search.asp?search=radio&sSearchtype=2
/setupapp/x/search.asp?search=RB_000000010000&sSearchtype=3
/setupapp/x/statxml.asp?id=RB_000000010000
/setupapp/x/statxml.asp?id=RB_00000000
/setupapp/x/statxml.asp?id=RB_FFFFFFFF0000
/retuner/play?id=RB_000000010000
/retuner/play?id=RB_FFFFFFFF0000
/retuner/icon?id=RB_000000010000
EOU
# Known divergence, not compared: a search text with a space. The Pascal build puts
# it into the upstream URL unencoded, the request fails and the receiver sees
# "No station(s) found"; the Go port encodes it and the search works.
sleep 0.3
if cmp -s "$WORK/ref.rblog" "$WORK/new.rblog"; then echo "  same  upstream requests ($(wc -l < "$WORK/ref.rblog") lines)"; else echo "  DIFF  upstream requests"; diff "$WORK/ref.rblog" "$WORK/new.rblog" | head -20; fail=1; fi
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
