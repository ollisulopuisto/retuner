#!/bin/sh
set -eu

ROOT=$(cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

OUT_CPU=$(go env GOARCH)
OUT_OS=$(go env GOOS)

if [ "$OUT_OS" = "darwin" ]; then
    OUT_OS="darwin"
elif [ "$OUT_OS" = "linux" ]; then
    OUT_OS="linux"
fi

if [ "$OUT_CPU" = "amd64" ]; then
    OUT_CPU="x86_64"
elif [ "$OUT_CPU" = "arm64" ]; then
    OUT_CPU="aarch64"
elif [ "$OUT_CPU" = "386" ]; then
    OUT_CPU="i386"
fi

TARGET_DIR=$ROOT/bin/$OUT_CPU-$OUT_OS
mkdir -p "$TARGET_DIR"

EXE_SUFFIX=""
if [ "$OUT_OS" = "windows" ]; then
  EXE_SUFFIX=".exe"
fi

go build -o "$TARGET_DIR/retuner$EXE_SUFFIX" main.go
echo "Built $TARGET_DIR/retuner$EXE_SUFFIX"
