#!/usr/bin/env bash
set -euo pipefail

usage() {
    echo "Usage: $0 <arch> [split]"
    echo "  arch:  arm | x86 | universal"
    echo "  split: (optional) produce per-ABI APKs instead of a fat APK"
    echo ""
    echo "  arm       - arm64 + arm (arm64-v8a, armeabi-v7a)"
    echo "  x86       - amd64 + 386 (x86_64, x86)"
    echo "  universal - all four ABIs"
    exit 1
}

[[ $# -lt 1 ]] && usage

ARCH="$1"
SPLIT="${2:-}"

case "$ARCH" in
    arm)       TARGETS="android/arm64,android/arm" ;;
    x86)       TARGETS="android/amd64,android/386" ;;
    universal) TARGETS="android/arm64,android/arm,android/amd64,android/386" ;;
    *)         echo "Unknown arch: $ARCH"; usage ;;
esac

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
GOLIB_DIR="$SCRIPT_DIR/golib"
AAR_OUT="$SCRIPT_DIR/android/app/libs/golib.aar"

echo "==> Building golib.aar (targets: $TARGETS)"
cd "$GOLIB_DIR"
gomobile bind -v \
    -ldflags="-checklinkname=0" \
    -target="$TARGETS" \
    -androidapi=24 \
    -o "$AAR_OUT" \
    ./
echo "==> golib.aar built: $AAR_OUT"

echo "==> Building Flutter APK"
cd "$SCRIPT_DIR"
if [[ "$SPLIT" == "split" ]]; then
    flutter build apk --split-per-abi
else
    flutter build apk
fi

echo "==> Done"
