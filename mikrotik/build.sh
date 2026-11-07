#!/bin/sh
# Build the image and export it as a docker-archive tar for /container add file=...
# Usage: ./build.sh [arm64|amd64]   (default arm64, for the RB5009)
set -e
ARCH=${1:-arm64}
cd "$(dirname "$0")"
docker buildx build --platform "linux/$ARCH" -f Dockerfile -t "viritin:$ARCH" --load ..
docker save "viritin:$ARCH" -o "viritin-$ARCH.tar"
ls -lh "viritin-$ARCH.tar"
