#!/bin/sh
# Differential check: start two Retuner binaries (reference Pascal build and the
# Go port) on a throwaway config and compare what each answers, byte for byte,
# status code and body. Headers other than Content-Type are not compared
# (Date/Server/Status vary by implementation).
#
#   ./script/parity-diff.sh <reference-binary> <go-binary>
#
# The Server header carries APP_VERSION, so the reference and the port must be
# built from the same version for bodies that embed it (the about page).
set -eu
REF=${1:?reference binary}
NEW=${2:?go binary}
WORK=$(mktemp -d)
PIDS=""
cleanup() { for p in $PIDS; do kill "$p" 2>/dev/null || true; wait "$p" 2>/dev/null || true; done; rm -rf "$WORK"; }
trap cleanup EXIT INT TERM

start() { # $1=name $2=bin $3=port
  mkdir -p "$WORK/$1"
  cp "$2" "$WORK/$1/retuner"
  mkdir -p "$WORK/$1/config"
  cat > "$WORK/$1/config/stations.ini" <<'EOS'
[Jazz & Blues]
Radio One=http://one.example/stream|http://one.example/logo.png
Secure Two=https://two.example/s?a=1&b=2
no equals here
Third=http://three.example/x|

[Rock 2000s]
Äänekoski FM=http://fm.example/a.mp3|http://fm.example/l.jpg
Dup=http://d.example/x
Dup=http://d.example/x

[Empty One]
EOS
  cat > "$WORK/$1/retuner.ini" <<EOT
[Configuration]
INIVersion=1.2.2
IPAddress=127.0.0.1
[WebServer]
WebServerIPAddress=127.0.0.1
WebServerPort=$3
[DNSServer]
Enable=0
[RadioBrowser]
Enable=0
[MyStations]
Enable=1
[Bookmark]
Enable=1
EOT
  ( cd "$WORK/$1" && exec ./retuner -p "$3" > server.log 2>&1 ) &
  PIDS="$PIDS $!"
  i=0
  while ! curl -fsS --noproxy '*' -o /dev/null "http://127.0.0.1:$3/setupapp/x/loginxml.asp?token=0" 2>/dev/null; do
    i=$((i+1)); [ $i -gt 50 ] && { echo "$1 did not start"; cat "$WORK/$1/server.log"; exit 1; }
    sleep 0.2
  done
}
start ref "$REF" 18201
start new "$NEW" 18202

fail=0
while IFS= read -r url; do
  [ -z "$url" ] && continue
  for n in ref new; do
    port=18201; [ $n = new ] && port=18202
    curl -s --noproxy '*' -o "$WORK/$n.body" -w '%{http_code} %{content_type} %{size_download}\n' "http://127.0.0.1:$port$url" > "$WORK/$n.meta" || true
  done
  if [ "${url#*/icon}" != "$url" ]; then
    # Two JPEG encoders never agree to the byte: compare status and type only.
    for n in ref new; do cut -d' ' -f1,2 "$WORK/$n.meta" > "$WORK/$n.meta2"; mv "$WORK/$n.meta2" "$WORK/$n.meta"; : > "$WORK/$n.body"; done
  fi
  if cmp -s "$WORK/ref.meta" "$WORK/new.meta" && cmp -s "$WORK/ref.body" "$WORK/new.body"; then
    echo "  same  $url"
  else
    echo "  DIFF  $url"; echo "    ref: $(cat "$WORK/ref.meta")"; echo "    new: $(cat "$WORK/new.meta")"; fail=1
  fi
done <<'EOU'
/setupapp/x/loginxml.asp?token=0
/setupapp/x/loginxml.asp?mac=aabbccddee
/setupapp/karcher/asp/BrowseXML/loginXML.asp?mac=aabbccddee
/retuner/about?mac=aabbccddee
/ytuner/about
/retuner/empty
/ytuner/empty
/setupapp/x/statxml.asp?id=x
/retuner/mystations?mac=aabbccddee
/retuner/mystations?start=1&howmany=1
/retuner/mystations?startitems=2&enditems=3
/retuner/mystations?startitems=9&enditems=2
/retuner/mystations/Jazz%20%26%20Blues
/retuner/mystations/jazz+%26+blues
/retuner/mystations/Rock%202000s?start=2&howmany=5
/retuner/mystations/Empty%20One
/retuner/mystations/Nothing
/ytuner/mystations/Jazz%20%26%20Blues
/retuner/play?id=MS_NOTREAL
/retuner/play
/setupapp/x/statxml.asp?id=
/setupapp/x/statxml.asp
/setupapp/favxml.asp
/retuner/mystations
/retuner/icon?id=MS_AAAAAAAAAAAA
/nothing
/
/retuner/doesnotexist
EOU
# Ids are derived (md5 of name+url), so read them from the reference rather
# than hard-coding, then ask both servers about each one.
ids=$(curl -s --noproxy '*' "http://127.0.0.1:18201/retuner/mystations/Jazz%20%26%20Blues" | sed -n 's|.*<StationId>\(.*\)</StationId>.*|\1|p')
for id in $ids; do
  for u in "/setupapp/x/statxml.asp?id=$id" "/retuner/play?id=$id&mac=aa"; do
    for n in ref new; do
      port=18201; [ $n = new ] && port=18202
      curl -s --noproxy '*' -o "$WORK/$n.body" -D "$WORK/$n.hdr" "http://127.0.0.1:$port$u" || true
      { sed -n 1p "$WORK/$n.hdr" | tr -d '\r'; grep -i '^location' "$WORK/$n.hdr" | tr -d '\r'; cat "$WORK/$n.body"; } > "$WORK/$n.cmp"
    done
    if cmp -s "$WORK/ref.cmp" "$WORK/new.cmp"; then echo "  same  $u"; else echo "  DIFF  $u"; fail=1; fi
  done
done
# Bookmarks are stateful, so they are compared as a sequence run against both
# servers in the same order, each reply compared as it comes.
set -- $ids
id1=$1; id2=$2; id3=$3
b1="MSB${id1#MS_}"
echo "Bookmark sequence"
while IFS= read -r u; do
  [ -z "$u" ] && continue
  u=$(printf '%s' "$u" | sed "s/@1/$id1/g; s/@2/$id2/g; s/@3/$id3/g; s/@B1/$b1/g")
  for n in ref new; do
    port=18201; [ $n = new ] && port=18202
    curl -s --noproxy '*' -o "$WORK/$n.body" -D "$WORK/$n.hdr" "http://127.0.0.1:$port$u" || true
    { sed -n 1p "$WORK/$n.hdr" | tr -d '\r'; grep -i '^content-type' "$WORK/$n.hdr" | tr -d '\r'; cat "$WORK/$n.body"; } > "$WORK/$n.cmp"
    if [ -f "$WORK/$n/config/bookmark.xml" ]; then cat "$WORK/$n/config/bookmark.xml" >> "$WORK/$n.cmp"; else echo "[no bookmark file]" >> "$WORK/$n.cmp"; fi
  done
  if cmp -s "$WORK/ref.cmp" "$WORK/new.cmp"; then echo "  same  $u"; else echo "  DIFF  $u"; diff "$WORK/ref.cmp" "$WORK/new.cmp" | head -8; fail=1; fi
done <<'EOB'
/retuner/bookmark?mac=aabbccddee
/setupapp/x/loginxml.asp?mac=aabbccddee
/setupapp/x/statxml.asp?id=@B1&mac=aabbccddee
/setupapp/favxml.asp?id=@1&fav=del&mac=aabbccddee
/setupapp/favxml.asp?id=@1&fav=add&mac=aabbccddee
/setupapp/favxml.asp?id=@1&fav=add&mac=aabbccddee
/setupapp/favxml.asp?id=@2&fav=add&mac=aabbccddee
/setupapp/favxml.asp?id=@3&fav=add&mac=other
/setupapp/favxml.asp?id=MS_NOTREAL&fav=add&mac=aabbccddee
/setupapp/favxml.asp?id=&fav=add
/setupapp/favxml.asp?id=@2&fav=bogus
/retuner/bookmark?mac=aabbccddee
/retuner/bookmark?mac=aabbccddee&start=2&howmany=1
/retuner/bookmark?mac=aabbccddee&startitems=2&enditems=9
/ytuner/bookmark
/setupapp/x/loginxml.asp?mac=aabbccddee
/setupapp/x/statxml.asp?id=@B1&mac=aabbccddee
/setupapp/x/statxml.asp?id=MSBNOTTHERE&mac=aabbccddee
/setupapp/favxml.asp?id=@1&fav=del&mac=aabbccddee
/retuner/bookmark?mac=aabbccddee
/setupapp/favxml.asp?id=@2&fav=del&mac=aabbccddee
/setupapp/favxml.asp?id=@3&fav=del&mac=aabbccddee
/retuner/bookmark?mac=aabbccddee
/setupapp/x/statxml.asp?id=@B1&mac=aabbccddee
/setupapp/favxml.asp?id=@2&fav=del&mac=aabbccddee
EOB
[ $fail -eq 0 ] && echo "All compared responses match." || { echo "Differences found."; exit 1; }
