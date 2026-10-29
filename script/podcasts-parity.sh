#!/bin/sh
# Differential check of podcasts: feed list, episode lists (paging, limit, cache,
# bad feeds), station info and play redirects, plus the requests the feed mock
# saw (cache hits must not refetch). Both binaries run one after the other on
# the same ports, because feed and episode ids hash the URL.
#   ./script/podcasts-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
M=18842; P=18742
get() { curl -s --noproxy '*' -D - "http://127.0.0.1:$P$1" | grep -iv '^date:\|^server:\|^status:' | tr -d '\r'; echo; }
phase() { # name bin
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  PL_LOG="$W/$1.log" python3 "$HERE/testdata/mock-podcasts.py" $M >/dev/null 2>&1 & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$M/good.xml"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  : > "$W/$1.log"
  cat > "$W/$1/config/podcasts.ini" <<EOT
[News]
Good=http://127.0.0.1:$M/good.xml|http://127.0.0.1:$M/logo.png
Many=http://127.0.0.1:$M/many.xml
Empty=http://127.0.0.1:$M/empty.xml
[Other]
NoAudio=http://127.0.0.1:$M/noaudio.xml
Bad=http://127.0.0.1:$M/bad.xml
NotRSS=http://127.0.0.1:$M/notrss.xml
Gone=http://127.0.0.1:$M/gone.xml
EOT
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
Enable=0
[Podcasts]
Enable=1
PodcastEpisodesLimit=100
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$W/$1/server.log"; exit 1; }; sleep 0.2; done
  O="$W/out-$1"; mkdir -p "$O"; : > "$O/all"
  { get "/retuner/podcasts"; get "/retuner/podcasts?startitems=2&enditems=3"; get "/setupapp/x/loginxml.asp?token=0"; } >> "$O/all"
  feeds=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts" | grep -o 'PF_[0-9A-F]*' | sort -u)
  for f in $feeds; do get "/retuner/podcasts/$f"; get "/retuner/podcasts/$f?startitems=3&enditems=5"; done >> "$O/all"
  get "/retuner/podcasts/PF_000000000000" >> "$O/all"
  eps=$(for f in $feeds; do curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts/$f"; done | grep -o 'PC_[0-9A-F]*' | sort -u | head -12)
  for e in $eps; do get "/retuner/play?id=$e"; get "/setupapp/x/statxml.asp?id=$e"; done >> "$O/all"
  get "/retuner/play?id=PC_000000000000" >> "$O/all"
  get "/setupapp/x/statxml.asp?id=PC_000000000000" >> "$O/all"
  [ -n "${KEEP:-}" ] && cp "$O/all" "$KEEP.$1"
  sort "$W/$1.log" | uniq -c > "$O/log"
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5; rm -rf "$W/$1"
}
phase ref "$REF"; phase new "$NEW"
fail=0
if cmp -s "$W/out-ref/all" "$W/out-new/all"; then echo "  same  pages ($(grep -c '^HTTP' "$W/out-new/all") replies)"; else echo "  DIFF  pages"; diff "$W/out-ref/all" "$W/out-new/all" | head -20; fail=1; fi
if cmp -s "$W/out-ref/log" "$W/out-new/log"; then echo "  same  requests seen by the feed mock"; sed 's/^/        /' "$W/out-new/log"; else echo "  DIFF  mock requests"; diff "$W/out-ref/log" "$W/out-new/log" | head; fail=1; fi
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
