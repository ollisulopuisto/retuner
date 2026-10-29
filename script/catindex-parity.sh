#!/bin/sh
# catFile startup index (Pascal LoadRBCacheFilesInfo): cache files are only
# used when they were present at startup (or written by this process). A file
# dropped into the cache folder while the server runs is ignored and refetched.
#   ./script/catindex-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d)
trap 'kill $(jobs -p) 2>/dev/null||true; [ -n "${KEEP:-}" ] || rm -rf "$W"; [ -z "${KEEP:-}" ] || echo "kept $W"' EXIT INT TERM
P=18746; M=18846
URL=/retuner/radiobrowser/country
setup() {
  mkdir -p "$1/config" "$1/cache"; cp "$2" "$1/retuner"; cp "$HERE/../cfg/avr.ini" "$1/config/avr.ini"
  cat > "$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
LocalCountry=Testland
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$P
[DNSServer]
Enable=0
[RadioBrowser]
Enable=1
RBAPIURL=http://127.0.0.1:$M
RBMinStationsPerCategory=1
RBCacheType=catFile
RBCacheTTL=0
[MyStations]
Enable=0
EOT
}
run() { # dir log [mid-run command]
  RB_LOG="$W/$2.rblog" python3 "$HERE/testdata/mock-radiobrowser.py" $M ok >/dev/null 2>&1 & mp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$M/json/servers"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  ( cd "$1" && exec ./retuner >server.log 2>&1 ) & bp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.2; done
  sleep 1; : > "$W/$2.rblog"
  [ -z "${3:-}" ] || eval "$3"
  curl -s --noproxy '*' "http://127.0.0.1:$P$URL" > "$W/$2.page"
  kill $bp $mp 2>/dev/null || true; wait $bp $mp 2>/dev/null || true; sleep 0.3
  grep -c . "$W/$2.rblog" || true
}
fail=0
for n in ref new; do
  bin=$REF; [ $n = new ] && bin=$NEW
  setup "$W/$n" "$bin"
  run "$W/$n" "$n-seed" >/dev/null; mkdir "$W/$n-seed"; cp "$W/$n/cache/"RBC-*.cache "$W/$n-seed/"; rm -f "$W/$n/cache/"*.cache
  a=$(run "$W/$n" "$n-dropped" "cp $W/$n-seed/* $W/$n/cache/")
  eval "a_$n=$a"
done
if [ "$a_ref" = "$a_new" ]; then echo "  same  upstream requests for a cache file dropped in while running ($a_ref)"; else echo "  DIFF  dropped-in file: ref=$a_ref new=$a_new"; fail=1; fi
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
