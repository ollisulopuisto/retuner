#!/bin/sh
# Differential check of ResolvePlaylists: My Stations entries that point at .pls,
# .m3u, .asx and .xspf (plain, nested, empty, self-referencing, oversized) are
# listed and played by a reference binary and the Go port, each against its own
# copy of the playlist mock. Compares the listing, the play redirect and the
# requests the mock saw. With ResolvePlaylists=0 nothing may be fetched.
#   ./script/playlist-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
run() { # name bin webport mockport resolve
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  PL_LOG="$W/$1.log" python3 "$HERE/testdata/mock-playlists.py" "$4" >/dev/null 2>&1 & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$4/a.pls"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  : > "$W/$1.log"
  {
    echo "[Lists]"
    for n in a.pls b.m3u c.asx d.xspf nest.m3u nest2.m3u empty.m3u self.m3u big.m3u missing.m3u "a.pls?token=1" ; do
      echo "S-$n=http://127.0.0.1:$4/$n"
    done
    echo "Plain=http://127.0.0.1:$4/stream.mp3"
  } > "$W/$1/config/stations.ini"
  cat > "$W/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
ResolvePlaylists=$5
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$3
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$3/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$W/$1/server.log"; exit 1; }; sleep 0.2; done
}
fail=0
# Station ids hash the URL, so both binaries must see the same mock port: they run
# one after the other against the same ports and their outputs are compared.
phase() { # resolve name bin
  run "$2" "$3" 18741 18841 "$1"
  mkdir -p "$W/out-$2"; : > "$W/out-$2/all"
  ids=$(curl -s --noproxy '*' "http://127.0.0.1:18741/retuner/mystations/Lists" | grep -o 'MS_[0-9A-F]*' | sort -u)
  for p in "/retuner/mystations/Lists" $(for id in $ids; do echo "/retuner/play?id=$id"; done); do
    { echo "== $p"; curl -s --noproxy '*' -D - "http://127.0.0.1:18741$p" | grep -iv '^date:\|^server:\|^status:' | tr -d '\r'; echo; } >> "$W/out-$2/all"
  done
  sort "$W/$2.log" > "$W/out-$2/log"
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5
  rm -rf "$W/$2"
}
one() {
  phase "$1" ref "$REF"; phase "$1" new "$NEW"
  if cmp -s "$W/out-ref/all" "$W/out-new/all"; then echo "  same  [resolve=$1] listing and play redirects ($(grep -c '^==' "$W/out-new/all") requests)"; else echo "  DIFF  [resolve=$1] pages"; diff "$W/out-ref/all" "$W/out-new/all" | head -10; fail=1; fi
  if cmp -s "$W/out-ref/log" "$W/out-new/log"; then echo "  same  [resolve=$1] requests seen by the mock ($(wc -l < "$W/out-new/log"))"; else echo "  DIFF  [resolve=$1] mock requests"; diff "$W/out-ref/log" "$W/out-new/log" | head; fail=1; fi
  if [ "$1" = 0 ] && [ -s "$W/out-new/log" ]; then echo "  FAIL  resolve=0 fetched playlists"; fail=1; fi
}
one 1
one 0
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
