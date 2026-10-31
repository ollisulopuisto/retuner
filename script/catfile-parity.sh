#!/bin/sh
# catFile on-disk cache: (1) the files each binary writes are identical,
# names and bytes, category and station lists alike (the station file name
# hash matches Pascal's string.GetHashCode); (2) after a restart the cache
# answers without upstream requests; (3) the other binary reads the first
# one's whole cache without upstream requests; (4) a TTL of 1 hour with
# files older than that forces a refetch, in both.
#   ./script/catfile-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
P=18745; M=18845
URLS="/retuner/radiobrowser/country /retuner/radiobrowser/genre /retuner/radiobrowser/language /retuner/radiobrowser/country/Testland /retuner/radiobrowser/genre/test /retuner/radiobrowser/popular /retuner/search?search=radio"
setup() { # dir bin ttl
  mkdir -p "$1/config"; cp "$2" "$1/retuner"; cp "$HERE/../cfg/avr.ini" "$1/config/avr.ini"
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
RBCacheTTL=$3
[MyStations]
Enable=0
EOT
}
session() { # dir logname -> runs the binary in dir, requests URLS, stops; leaves request count in $W/count
  RB_LOG="$W/$2.rblog" python3 "$HERE/testdata/mock-radiobrowser.py" $M ok >/dev/null 2>&1 & mp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$M/json/servers"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  : > "$W/$2.rblog"
  ( cd "$1" && exec ./retuner >server.log 2>&1 ) & bp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$1/server.log"; exit 1; }; sleep 0.2; done
  sleep 1 # let the startup requests (uuid list) finish
  : > "$W/$2.rblog"
  for u in $URLS; do curl -s --noproxy '*' "http://127.0.0.1:$P$u" >> "$W/$2.pages"; done
  kill $bp $mp 2>/dev/null || true; wait $bp $mp 2>/dev/null || true; sleep 0.3
  grep -c . "$W/$2.rblog" > "$W/count" || echo 0 > "$W/count"
}
sums() { ( cd "$1/cache" && for f in RB*.cache; do echo "$(wc -c < "$f") $f"; done | sort ); }
fail=0
chk() { if [ "$2" = "$3" ]; then echo "  same  $1 ($2)"; else echo "  DIFF  $1: ref=$2 new=$3"; fail=1; fi; }
for n in ref new; do
  bin=$REF; [ $n = new ] && bin=$NEW
  setup "$W/$n" "$bin" 0
  session "$W/$n" "$n-1"; eval "c1_$n=$(cat $W/count)"
  session "$W/$n" "$n-2"; eval "c2_$n=$(cat $W/count)"
done
chk "upstream requests on first run" "$c1_ref" "$c1_new"
chk "upstream requests after restart (cache reused)" "$c2_ref" "$c2_new"
[ "$c2_new" = 0 ] || { echo "  FAIL  Go refetched after restart"; fail=1; }
chk "cache files (name and size)" "$(sums $W/ref | tr '\n' ,)" "$(sums $W/new | tr '\n' ,)"
cmp -s "$W/ref-1.pages" "$W/new-1.pages" && echo "  same  pages served" || { echo "  DIFF  pages served"; fail=1; }
# cross reads: Go over Pascal's cache folder and Pascal over Go's
for pair in "ref new" "new ref"; do set -- $pair
  d="$W/x-$1-$2"; bin=$NEW; [ $2 = ref ] && bin=$REF
  mkdir -p "$d/cache"; cp "$W/$1/cache/"*.cache "$d/cache/"; setup "$d" "$bin" 0
  session "$d" "x-$1-$2"
  chk "$2 reading $1's cache files (whole cache shared)" "$(cat $W/count)" 0
done
# TTL: files 3 hours old, TTL 1 hour
for n in ref new; do
  bin=$REF; [ $n = new ] && bin=$NEW
  d="$W/t-$n"; mkdir -p "$d/cache"; cp "$W/$n/cache/"*.cache "$d/cache/"; touch -d '3 hours ago' "$d/cache/"*.cache; setup "$d" "$bin" 1
  session "$d" "t-$n"; eval "t_$n=$(cat $W/count)"
done
chk "upstream requests with expired files" "$t_ref" "$t_new"
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
