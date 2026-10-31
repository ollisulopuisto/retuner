#!/bin/sh
# Differential check of the HTTPS relay. Both binaries run one after the other on
# the same ports against the same upstream mock. Station URLs are https:// so the
# listing must point at the relay; the relay itself is then asked for stations
# whose real URL is plain http on the mock (it fetches whatever the id maps to).
# Compares listing, relay replies (status line and headers except Date, body
# bytes) and what the upstream saw. A live stream is forwarded until the client
# leaves (the relay reconnects when the upstream ends), so each request is cut
# after 3 s and only the first 3000 body bytes are compared.
#   ./script/relay-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
UP=18851; WEB=18751; RELAY=18752
phase() { # name bin
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  RL_LOG="$W/$1.up" python3 "$HERE/testdata/mock-relay-upstream.py" $UP >/dev/null 2>&1 & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$UP/ok"; do i=$((i+1)); [ $i -gt 50 ] && exit 1; sleep 0.1; done
  : > "$W/$1.up"
  printf '[R]\nSecure=https://127.0.0.1:%s/ok\nOk=http://127.0.0.1:%s/ok\nPlain=http://127.0.0.1:%s/plain\nDead=http://127.0.0.1:%s/dead\n' $UP $UP $UP $UP > "$W/$1/config/stations.ini"
  cat > "$W/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
ActAsHost=127.0.0.1
RelayHTTPS=1
RelayPort=$RELAY
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$WEB
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$WEB/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$W/$1/server.log"; exit 1; }; sleep 0.2; done
  sleep 0.5
  out="$W/out-$1"; : > "$out"
  echo "== listing" >> "$out"; curl -s --noproxy '*' "http://127.0.0.1:$WEB/retuner/mystations/R" >> "$out"
  ids=$(curl -s --noproxy '*' "http://127.0.0.1:$WEB/retuner/mystations/R" | grep -o 'MS_[0-9A-F]*' | sort -u)
  for id in $ids; do
    for icy in 0 1; do
      echo "== relay $id icy=$icy" >> "$out"
      if [ $icy = 1 ]; then H='Icy-MetaData: 1'; else H='X-None: 1'; fi
      curl -s --noproxy '*' --http1.0 -m 3 -D "$W/h" -o "$W/b" -H "$H" "http://127.0.0.1:$RELAY/relay?id=$id" || true
      grep -iv '^date:' "$W/h" | tr -d '\r' >> "$out"; echo "body first3000 $(head -c 3000 "$W/b" | md5sum | cut -c1-12) size>=3000: $([ "$(wc -c < "$W/b")" -ge 3000 ] && echo y || echo n)" >> "$out"
    done
  done
  for path in "/relay?id=MS_000000000000" "/relay" "/relay?x=1" "/other?id=1"; do
    echo "== relay $path" >> "$out"
    curl -s --noproxy '*' --http1.0 -m 10 -D "$W/h" -o "$W/b" "http://127.0.0.1:$RELAY$path" || true
    grep -iv '^date:' "$W/h" | tr -d '\r' >> "$out"; echo "body $(wc -c < "$W/b")" >> "$out"
  done
  sort "$W/$1.up" > "$W/up-$1"
  for q in $PIDS; do kill $q 2>/dev/null || true; done; PIDS=""; sleep 0.5; rm -rf "$W/$1"
}
phase ref "$REF"; phase new "$NEW"
fail=0
cmp -s "$W/out-ref" "$W/out-new" && echo "  same  listing and relay replies ($(grep -c '^==' "$W/out-new") requests)" || { echo "  DIFF  replies"; diff "$W/out-ref" "$W/out-new" | head -20; fail=1; }
cmp -s "$W/up-ref" "$W/up-new" && echo "  same  upstream requests ($(wc -l < "$W/up-new"))" || { echo "  DIFF  upstream requests"; diff "$W/up-ref" "$W/up-new" | head; fail=1; }
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
