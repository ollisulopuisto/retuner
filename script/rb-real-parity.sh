#!/bin/sh
# OPT-IN, needs internet: compares both binaries on the REAL public Radio
# Browser directory. A snapshot proxy (testdata/rb-snapshot-proxy.py) fetches
# each upstream path once, read-only, and serves identical bytes to both, so
# the data cannot differ between runs and the Pascal binary needs no OpenSSL.
# Not part of CI. RB_CACHE picks RBCacheType (default catMemStr).
#   ./script/rb-real-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}; CT=${RB_CACHE:-catMemStr}
HERE=$(cd "$(dirname "$0")" && pwd); W=$(mktemp -d); P=18747; M=18847
trap 'kill $(jobs -p) 2>/dev/null||true; rm -rf "$W"' EXIT INT TERM
python3 "$HERE/testdata/rb-snapshot-proxy.py" $M "$W/proxy.log" & PX=$!; trap 'kill $PX 2>/dev/null; kill $(jobs -p) 2>/dev/null||true; rm -rf "$W"' EXIT INT TERM; sleep 1
for n in ref new; do
  bin=$REF; [ $n = new ] && bin=$NEW; d=$W/$n; mkdir -p $d/config $d/cache; cp $bin $d/retuner; cp "$HERE/../cfg/avr.ini" $d/config/
  cat > $d/retuner.ini <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
LocalCountry=Finland
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$P
[DNSServer]
Enable=0
[RadioBrowser]
Enable=1
RBAPIURL=http://127.0.0.1:$M
RBCacheType=$CT
[MyStations]
Enable=0
EOT
  ( cd $d && exec ./retuner >server.log 2>&1 ) & bp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 300 ] && break; sleep 0.5; done; sleep 3
  for u in radiobrowser/country radiobrowser/country/Finland radiobrowser/genre radiobrowser/genre/jazz radiobrowser/language radiobrowser/popular "search?search=yle"; do
    curl -s -m 90 --noproxy '*' -D - "http://127.0.0.1:$P/retuner/$u" | grep -v '^Date:\|^Status:' >> $d/pages
  done
  kill $bp; wait $bp 2>/dev/null || true; sleep 0.5
done
echo "  bytes: ref=$(wc -c < $W/ref/pages) new=$(wc -c < $W/new/pages), $(grep -c . $W/proxy.log) distinct-or-repeated upstream requests"
if cmp -s $W/ref/pages $W/new/pages; then echo "All compared responses match."; else diff $W/ref/pages $W/new/pages | head -20; echo "Differences found."; exit 1; fi
