#!/bin/sh
# Seed /data on first start, then run Retuner with its ini file on /data.
set -e
mkdir -p /data/config
# A /data written by YTuner keeps its settings in ytuner.ini. Carry them across
# rather than starting from defaults, which would look like the config was ignored.
if [ ! -f /data/retuner.ini ] && [ -f /data/ytuner.ini ]; then
  mv /data/ytuner.ini /data/retuner.ini
  echo "Renamed ytuner.ini to retuner.ini in /data."
fi
[ -f /data/retuner.ini ] || cp /opt/retuner/defaults/retuner.ini /data/retuner.ini
for f in /opt/retuner/defaults/config/*; do
  [ -e "/data/config/${f##*/}" ] || cp "$f" /data/config/
done
ln -sf /data/retuner.ini /opt/retuner/retuner.ini
cd /opt/retuner
exec ./retuner
