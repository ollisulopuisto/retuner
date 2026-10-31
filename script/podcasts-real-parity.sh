#!/bin/sh
# OPT-IN, needs internet: downloads six real podcast feeds once (plain GETs),
# serves the identical bytes over local http and compares both binaries:
# feed list, episode lists, paging, station info and play redirects.
# Not part of CI.   ./script/podcasts-real-parity.sh <reference-binary> <go-binary>
set -eu
REF=${1:?reference}; NEW=${2:?go binary}
W=$(mktemp -d); M=18852; P=18752
trap 'kill $(jobs -p) 2>/dev/null||true; rm -rf "$W"' EXIT INT TERM
mkdir "$W/feeds"; i=0
for u in https://feeds.npr.org/510318/podcast.xml https://podcasts.files.bbci.co.uk/p02nq0gn.rss https://lexfridman.com/feed/podcast/ https://feeds.simplecast.com/54nAGcIl https://www.theguardian.com/news/series/todayinfocus/podcast.xml https://feeds.megaphone.fm/hubermanlab; do
  i=$((i+1)); curl -sL -m 60 -A Retuner/26.08.28.4 -o "$W/feeds/real$i.xml" "$u" || true
done
( cd "$W/feeds" && exec python3 -m http.server $M --bind 127.0.0.1 >/dev/null 2>&1 ) &
sleep 1
for n in ref new; do
  bin=$REF; [ $n = new ] && bin=$NEW; d=$W/$n; mkdir -p $d/config; cp $bin $d/retuner
  { echo "[Real]"; for k in 1 2 3 4 5 6; do echo "Real$k=http://127.0.0.1:$M/real$k.xml"; done; } > $d/config/podcasts.ini
  cat > $d/retuner.ini <<EOT
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
  ( cd $d && exec ./retuner >server.log 2>&1 ) & bp=$!
  j=0; until curl -s --noproxy '*' -o /dev/null "http://127.0.0.1:$P/setupapp/x/loginxml.asp?token=0"; do j=$((j+1)); [ $j -gt 100 ] && break; sleep 0.3; done; sleep 2
  g() { curl -s --noproxy '*' -D - "http://127.0.0.1:$P$1" | grep -iv '^date:\|^server:\|^status:' | tr -d '\r'; echo; }
  { g /retuner/podcasts; feeds=$(curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts" | grep -o 'PF_[0-9A-F]*' | sort -u)
    for f in $feeds; do g "/retuner/podcasts/$f"; g "/retuner/podcasts/$f?startitems=3&enditems=5"; done
    for e in $(for f in $feeds; do curl -s --noproxy '*' "http://127.0.0.1:$P/retuner/podcasts/$f"; done | grep -o 'PC_[0-9A-F]*' | sort -u | head -30); do g "/retuner/play?id=$e"; g "/setupapp/x/statxml.asp?id=$e"; done; } > $d/out
  kill $bp; wait $bp 2>/dev/null || true; sleep 0.5
done
echo "  bytes: ref=$(wc -c < $W/ref/out) new=$(wc -c < $W/new/out), episodes listed: $(grep -c '<ItemType>Station' $W/ref/out)"
if cmp -s $W/ref/out $W/new/out; then echo "All compared responses match."; else diff $W/ref/out $W/new/out | head -20; echo "Differences found."; exit 1; fi
