#!/bin/sh
# Differential check of favourites (bookmark.pas SetBookmark through
# /setupapp/favxml.asp): add, duplicate add, delete, delete-when-absent,
# delete-without-a-file and re-add, for a podcast episode and a My Stations
# entry, plus the bookmark listing after each step and the persisted
# bookmark file itself. Compared byte for byte against the Pascal binary.
# The action query parameter is 'fav' (fav=add / fav=del). A valid receiver
# MAC is passed: Go deliberately answers 400 for a missing/malformed mac
# (Pascal's own IsSafeAVRMAC rule) where Pascal silently writes '.xml' -
# that known divergence is documented in GO-PORT.md and not exercised here.
#   ./script/favourite-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
M=18848; P=18748
get() { curl -s --noproxy '*' -D - "http://127.0.0.1:$P$1" | grep -iv '^date:\|^server:\|^status:' | tr -d '\r'; echo; }
phase() { # name bin
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  python3 "$HERE/testdata/mock-podcasts.py" $M >/dev/null 2>&1 & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$M/good.xml"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  printf '[News]\nMany=http://127.0.0.1:%s/many.xml\n' $M > "$W/$1/config/podcasts.ini"
  printf '[Lists]\nTest Station=http://127.0.0.1:1/s.mp3\n' > "$W/$1/config/stations.ini"
  cat > "$W/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
RelayHTTPS=0
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$P
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
[Podcasts]
Enable=1
PodcastEpisodesLimit=100
[Bookmark]
Enable=1
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$W/$1/server.log"; exit 1; }; sleep 0.2; done
  O="$W/out-$1"; mkdir -p "$O"; : > "$O/all"
  f=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts" | grep -o 'PF_[0-9A-F]*' | head -1)
  ep=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts/$f" | grep -o 'PC_[0-9A-F]*' | head -1)
  ms=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/mystations/Lists" | grep -o 'MS_[0-9A-F]*' | head -1)
  { echo "== statxml $ep"; get "/setupapp/x/statxml.asp?id=$ep"
    echo "== add ep"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ep&fav=add"
    echo "== add ep again"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ep&fav=add"
    echo "== list"; get "/retuner/bookmark"
    echo "== del ep"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ep&fav=del"
    echo "== list"; get "/retuner/bookmark"
    echo "== del ep again (absent)"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ep&fav=del"
    echo "== del unknown id"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=PC_000000000000&fav=del"
    echo "== add ms $ms"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ms&fav=add"
    echo "== add ms again"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ms&fav=add"
    echo "== add ep back"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ep&fav=add"
    echo "== list (two items)"; get "/retuner/bookmark"
    echo "== del ms"; get "/setupapp/favxml.asp?mac=001122334455AABB&id=$ms&fav=del"
    echo "== list (one item)"; get "/retuner/bookmark"
  } >> "$O/all"
  [ -f "$W/$1/config/bookmark.xml" ] && cp "$W/$1/config/bookmark.xml" "$O/bookmark.xml"
  [ -n "${KEEP:-}" ] && { cp "$O/all" "$KEEP.$1.all"; [ -f "$W/$1/config/bookmark.xml" ] && cp "$W/$1/config/bookmark.xml" "$KEEP.$1.xml" || true; }
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5; rm -rf "$W/$1"
}
phase ref "$REF"; phase new "$NEW"
fail=0
if cmp -s "$W/out-ref/all" "$W/out-new/all"; then echo "  same  favourite sequence ($(grep -c '^==' "$W/out-new/all") steps)"; else echo "  DIFF  favourite sequence"; diff "$W/out-ref/all" "$W/out-new/all" | head -20; fail=1; fi
if cmp -s "$W/out-ref/bookmark.xml" "$W/out-new/bookmark.xml"; then echo "  same  persisted bookmark file ($(stat -c%s "$W/out-new/bookmark.xml") bytes)"; else echo "  DIFF  bookmark file"; diff "$W/out-ref/bookmark.xml" "$W/out-new/bookmark.xml" | head; fail=1; fi
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
