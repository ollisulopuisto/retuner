#!/bin/sh
# Differential check of the stations editor API: the same requests go to a
# reference binary and to the Go port; status and body must match (status
# route compared with the version line masked).
#   ./script/webgui-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
W=$(mktemp -d); PIDS=""
trap 'for p in $PIDS; do kill $p 2>/dev/null||true; done; rm -rf "$W"' EXIT INT TERM
run() { # name bin port
  mkdir -p "$W/$1/config"; cp "$2" "$W/$1/retuner"
  printf '[Cat A]\nOne=http://e.com/1|http://e.com/l.png\nTwo=http://e.com/2\n[Cat B]\nThree=http://e.com/3\n' > "$W/$1/config/stations.ini"
  cat > "$W/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$(($3+100))
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
[WebGUI]
Enable=1
WebGUIIPAddress=127.0.0.1
WebGUIPort=$3
WebGUIUser=admin
WebGUIPassword=pw
EOT
  ( cd "$W/$1" && exec ./retuner >server.log 2>&1 ) & PIDS="$PIDS $!"
  i=0; until curl -s --noproxy '*' -o /dev/null -u admin:pw "http://127.0.0.1:$3/api/status"; do i=$((i+1)); [ $i -gt 50 ] && { echo "$1 did not start"; exit 1; }; sleep 0.2; done
}
run ref "$REF" 18721; run new "$NEW" 18722
fail=0
# JSON bodies are compared as data (the reference pretty-prints, key order
# differs); the status line and any non-JSON body are compared as text.
norm() {
  python3 - "$1" <<'PY'
import sys, json
p = sys.argv[1]
t = open(p).read()
body, _, tail = t.rpartition("\n")
try:
    d = json.loads(body)
    if isinstance(d, dict) and "version" in d:
        d.pop("version")
    body = json.dumps(d, sort_keys=True)
except Exception:
    body = body.strip("\n")
open(p, "w").write(body + "\n" + tail + "\n")
PY
}
req() { # label method path [body]
  for n in ref:18721 new:18722; do
    name=${n%%:*}; port=${n##*:}
    if [ $# -ge 4 ]; then
      curl -s --noproxy '*' -u admin:pw -X "$2" -H 'Content-Type: application/json' -d "$4" -w '\n%{http_code} %{content_type}' "http://127.0.0.1:$port$3" > "$W/$name.out"
    else
      curl -s --noproxy '*' -u admin:pw -X "$2" -w '\n%{http_code} %{content_type}' "http://127.0.0.1:$port$3" > "$W/$name.out"
    fi
    norm "$W/$name.out"
  done
  if cmp -s "$W/ref.out" "$W/new.out"; then echo "  same  $1"; else echo "  DIFF  $1"; diff "$W/ref.out" "$W/new.out" | head -6; fail=1; fi
}
req "GET /api/stations" GET /api/stations
req "GET /api/status" GET /api/status
req "GET /" GET /
req "GET /remote" GET /remote
req "GET /nothing" GET /nothing
req "POST wrong type" POST /api/stations
req "malformed save" POST /api/stations '{"x":1}'
# not-JSON body: Pascal says "Could not save: <parser message>", Go says "Malformed request." (deliberate, not compared)
req "save with escapes" POST /api/stations '{"categories":[{"name":" New ","stations":[{"name":"A=b|c","url":"http://x|y","logo":"http://l|m"},{"name":"","url":"u"},{"name":"n","url":""}]},{"name":"","stations":[]},{"name":"Empty"}]}'
req "GET /api/stations after save" GET /api/stations
cmp -s "$W/ref/config/stations.ini" "$W/new/config/stations.ini" && echo "  same  saved stations.ini" || { echo "  DIFF  saved stations.ini"; diff "$W/ref/config/stations.ini" "$W/new/config/stations.ini"; fail=1; }
req "remote cmd GET" GET /api/remote/cmd
req "remote cmd malformed" POST /api/remote/cmd 'x'
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
