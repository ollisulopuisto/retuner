#!/bin/sh
# Seed /data on first start, then run Viritin with its ini file on /data.
set -e
mkdir -p /data/config
# A /data written by YTuner keeps its settings in ytuner.ini. Carry them across
# rather than starting from defaults, which would look like the config was ignored.
if [ ! -f /data/viritin.ini ] && [ -f /data/ytuner.ini ]; then
  mv /data/ytuner.ini /data/viritin.ini
  echo "Renamed ytuner.ini to viritin.ini in /data."
fi
[ -f /data/viritin.ini ] || cp /opt/viritin/defaults/viritin.ini /data/viritin.ini
for f in /opt/viritin/defaults/config/*; do
  [ -e "/data/config/${f##*/}" ] || cp "$f" /data/config/
done
ln -sf /data/viritin.ini /opt/viritin/viritin.ini
cd /opt/viritin
exec ./viritin
