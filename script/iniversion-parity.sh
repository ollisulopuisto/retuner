#!/bin/sh
# INIVersion handling: for retuner.ini and avr.ini that are missing the key, or
# carry an old one, compare the files each binary leaves behind and the warning
# lines it logs (timestamps stripped).
#   ./script/iniversion-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
HERE=$(cd "$(dirname "$0")" && pwd)
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT INT TERM
P=18744
run() { # case name bin
  d="$W/$1-$2"; mkdir -p "$d/config"; cp "$3" "$d/retuner"; cp "$HERE/../cfg/avr.ini" "$d/config/avr.ini"
  printf '[Configuration]\nIPAddress=127.0.0.1\n[WebServer]\nWebServerIPAddress=127.0.0.1\nWebServerPort=%s\n[DNSServer]\nEnable=0\n[RadioBrowser]\nEnable=0\n[MyStations]\nEnable=0\n' $P > "$d/retuner.ini"
  case $1 in
    oldmain) sed -i '1i [Configuration]\nINIVersion=0.9' "$d/retuner.ini"; sed -i '0,/^\[Configuration\]$/!{0,/^\[Configuration\]$/{/^\[Configuration\]$/d}}' "$d/retuner.ini" ;;
    oldavr) sed -i 's/^INIVersion=.*/INIVersion=0.1/' "$d/config/avr.ini" ;;
    noavr) sed -i '/^INIVersion=/d' "$d/config/avr.ini" ;;
  esac
  ( cd "$d" && exec ./retuner >server.log 2>&1 ) & pid=$!
  i=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do i=$((i+1)); [ $i -gt 50 ] && { cat "$d/server.log"; exit 1; }; sleep 0.2; done
  kill $pid 2>/dev/null || true; wait $pid 2>/dev/null || true; sleep 0.3
  { echo "--retuner.ini"; grep 'INIVersion' "$d/retuner.ini"; echo "--avr.ini"; grep 'INIVersion' "$d/config/avr.ini"; echo "--log"; grep -i 'outdated\|out of INI\|fill in the missing\|updated with new' "$d/server.log" | sed -n 's/^.*\(You are running\|Your \|Retuner will try\)/\1/p' | sed 's/\.*$//'; } > "$W/out-$1-$2"
}
fail=0
for c in ok oldmain oldavr noavr; do
  run $c ref "$REF"; run $c new "$NEW"
  if cmp -s "$W/out-$c-ref" "$W/out-$c-new"; then echo "  same  [$c]"; else echo "  DIFF  [$c]"; diff "$W/out-$c-ref" "$W/out-$c-new" | head; fail=1; fi
done
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
