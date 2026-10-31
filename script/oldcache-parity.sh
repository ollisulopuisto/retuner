#!/bin/sh
# Startup removal of old Radio Browser cache files (RemoveOldRBCacheFiles):
# with RBCacheTTL>0 the "RB*-*.cache" files older than the TTL go, others stay;
# with TTL 0 nothing goes. Radio Browser itself is disabled, as Pascal runs this
# regardless.
#   ./script/oldcache-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT INT TERM
P=18746; fail=0
run() { # dir bin ttl
  mkdir -p "$1/config" "$1/cache"; cp "$2" "$1/retuner"; cp "$HERE/../cfg/avr.ini" "$1/config/avr.ini"
  cat > "$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$P
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
RBCacheTTL=$3
[MyStations]
Enable=0
EOT
  for f in RBS-aa-1-2-3.cache RBC-aa-1-3.cache RBnodash.cache RBS-aa-1.txt icon-old.cache; do echo x > "$1/cache/$f"; touch -d '5 hours ago' "$1/cache/$f"; done
  echo x > "$1/cache/RBS-new-1-2-3.cache"
  ( cd "$1" && exec ./retuner >server.log 2>&1 ) & bp=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$1/server.log"; exit 1; }; sleep 0.2; done
  kill $bp 2>/dev/null || true; wait $bp 2>/dev/null || true; sleep 0.3
  ls "$1/cache" | sort | tr '\n' ' '
}
for ttl in 1 0; do
  r=$(run "$W/ref$ttl" "$REF" $ttl); n=$(run "$W/new$ttl" "$NEW" $ttl)
  if [ "$r" = "$n" ]; then echo "  same  TTL=$ttl: $r"; else echo "  DIFF  TTL=$ttl: ref=[$r] new=[$n]"; fail=1; fi
done
[ $fail = 0 ] && echo "All compared results match." || exit 1
