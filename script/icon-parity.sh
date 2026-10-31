#!/bin/sh
# Differential check of /retuner/icon for podcast episodes: logo fetch, the
# placeholder fallback (feed without logo, unknown id), the bad-id 404, HEAD,
# and the icon cache (the feed mock's request log proves refetch behaviour).
#
# Bodies are NOT compared byte for byte, and this is deliberate: the Pascal
# 26.08.28.4 build re-encodes icons with FPC's JPEG writer, whose output is
# defective - Go's image/jpeg and ffmpeg both reject or fail most of the scan
# (ffmpeg: 66 errors; only the top MCU rows decode), and Pascal appends a
# stray LF after EOI. The Go port serves a valid JPEG of the same mark at the
# same size. Compared instead: status lines, all headers except Connection
# (Go closes icon responses explicitly for curl -X HEAD; Pascal closes
# silently) and Content-Length, the image format and pixel dimensions, and
# the mock's request counts.
#   ./script/icon-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
M=18846; P=18746
hdr() { curl -s --noproxy '*' -D - -o "$2" "http://127.0.0.1:$P$1" | grep -iv '^date:\|^connection:\|^content-length:\|^status:' | tr -d '\r'; }
dims() { python3 - "$1" <<'PY'
import struct, sys
d = open(sys.argv[1], 'rb').read()
if d[:2] != b'\xff\xd8': print('not-jpeg'); sys.exit(0)
i = 2
while i < len(d) - 9:
    if d[i] != 0xff: i += 1; continue
    m = d[i+1]
    if m in (0xc0, 0xc1, 0xc2):
        h, w = struct.unpack('>HH', d[i+5:i+9]); print(f'jpeg {w}x{h}'); sys.exit(0)
    if m in (0xd8, 0xd9) or 0xd0 <= m <= 0xd7: i += 2; continue
    i += 2 + int.from_bytes(d[i+2:i+4], 'big')
print('jpeg no-sof')
PY
}
phase() { # name bin
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  PL_LOG="$W/$1.log" python3 "$HERE/testdata/mock-podcasts.py" $M >/dev/null 2>&1 & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$M/good.xml"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  : > "$W/$1.log"
  cat > "$W/$1/config/podcasts.ini" <<EOT
[News]
Logo=http://127.0.0.1:$M/many.xml|http://127.0.0.1:$M/logo.png
Plain=http://127.0.0.1:$M/many.xml
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
  pairs=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts" | python3 -c '
import re, sys
for blk in re.findall(r"<Item>(.*?)</Item>", sys.stdin.read(), re.S):
    t = re.search(r"<Title>([^<]*)</Title>", blk); i = re.search(r"(PF_[0-9A-F]+)", blk)
    if t and i: print(t.group(1), i.group(1))')
  flogo=$(echo "$pairs" | awk "/^Logo /{print \$2}"); fplain=$(echo "$pairs" | awk "/^Plain /{print \$2}")
  elogo=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts/$flogo" | grep -o 'PC_[0-9A-F]*' | head -1)
  eplain=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts/$fplain" | grep -o 'PC_[0-9A-F]*' | head -1)
  { echo "== logo $elogo"; hdr "/retuner/icon?id=$elogo" "$O/b-logo"
    echo "== plain $eplain"; hdr "/retuner/icon?id=$eplain" "$O/b-plain"
    echo "== unknown"; hdr "/retuner/icon?id=PC_000000000000" "$O/b-unknown"
    echo "== badid"; hdr "/retuner/icon?id=../../x" "$O/b-badid"
    echo "== head"; curl -s --noproxy '*' -I "http://127.0.0.1:$P/retuner/icon?id=$eplain" | grep -iv '^date:\|^connection:\|^content-length:\|^status:' | tr -d '\r'
    echo "== logo again (cache)"; hdr "/retuner/icon?id=$elogo" "$O/b-logo2"
  } >> "$O/all"
  { dims "$O/b-logo"; dims "$O/b-plain"; dims "$O/b-unknown"; dims "$O/b-logo2"; } > "$O/dims"
  sort "$W/$1.log" | uniq -c > "$O/log"
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5; rm -rf "$W/$1"
}
phase ref "$REF"; phase new "$NEW"
fail=0
note() { echo "  $1"; }
if cmp -s "$W/out-ref/all" "$W/out-new/all"; then note "same  statuses and headers (Connection/Content-Length excepted)"; else note "DIFF  statuses/headers"; diff "$W/out-ref/all" "$W/out-new/all" | head -20; fail=1; fi
if cmp -s "$W/out-ref/dims" "$W/out-new/dims"; then note "same  image format and dimensions ($(head -1 "$W/out-new/dims"))"; else note "DIFF  image format/dimensions"; diff "$W/out-ref/dims" "$W/out-new/dims"; fail=1; fi
if cmp -s "$W/out-ref/log" "$W/out-new/log"; then note "same  requests seen by the feed mock"; sed 's/^/        /' "$W/out-new/log"; else note "DIFF  mock requests"; diff "$W/out-ref/log" "$W/out-new/log" | head; fail=1; fi
note "info  body bytes differ by design: Pascal 26.08.28.4's JPEG writer emits a defective stream (ref $(stat -c%s "$W/out-ref/b-plain")B vs go $(stat -c%s "$W/out-new/b-plain")B); see header comment"
[ $fail -eq 0 ] && echo "All compared responses match (bodies excepted, see header)." || { echo "Differences found."; exit 1; }
