#!/bin/sh
# ReplaceDiacritics on the bookmark listing: a My Stations entry with Latin-1,
# Extended-A and Extended-B letters is added as a favourite and listed, with
# the avr.ini replace flags off and on. Reference and Go binary are compared.
#   ./script/bookmark-diacritics-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
P=18743
phase() { # flag name bin
  mkdir -p "$W/$2/config"; cp "$3" "$W/$2/retuner"; cp "$HERE/../cfg/avr.ini" "$W/$2/config/avr.ini"
  sed -i "s/^\(ReplaceUTF8Latin1[A-Za-z]*\)=0/\1=$1/" "$W/$2/config/avr.ini"
  printf '[Lists]\nCaf\303\251 \305\201\303\263d\305\272 \307\205 \306\221\303\237=http://127.0.0.1:1/s.mp3\n' > "$W/$2/config/stations.ini"
  cat > "$W/$2/retuner.ini" <<EOT
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
[MyStations]
Enable=1
[Bookmark]
Enable=1
EOT
  ( cd "$W/$2" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.2; done
  id=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/mystations/Lists" | grep -o 'MS_[0-9A-F]*' | head -1)
  curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/favxml.asp?id=$id&fav=add&mac=aabbccddee"
  { curl -s --noproxy '*' -D - "http://127.0.0.1:$P/retuner/bookmark?mac=aabbccddee" | grep -iv "^date:\|^server:\|^status:" | tr -d '\r'
    curl -s --noproxy '*' "http://127.0.0.1:$P/setupapp/x/statxml.asp?id=MB_${id#*_}&mac=aabbccddee"; } > "$W/out-$2"
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5; rm -rf "$W/$2"
}
fail=0
for f in 0 1; do
  phase $f ref "$REF"; phase $f new "$NEW"
  if cmp -s "$W/out-ref" "$W/out-new"; then echo "  same  [flags=$f] ($(grep -c StationName "$W/out-new") StationName lines)"; grep -m1 StationName "$W/out-new"; else echo "  DIFF  [flags=$f]"; diff "$W/out-ref" "$W/out-new" | head; fail=1; fi
done
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
