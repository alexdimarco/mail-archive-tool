#!/usr/bin/env bash
# Package the GUI as a universal (Intel + Apple Silicon), double-clickable macOS
# .app bundle, plus a universal CLI binary. Pure cross-compile — runs on any OS
# with the Go toolchain; no Mac required. The universal binaries are fused with
# github.com/randall77/makefat (already in the module graph via zenity).
#
# Usage: packaging/macos/build-app.sh [VERSION]
# Output dir: $OUT (default: bin/). Writes:
#   $OUT/MailArchive-macos.zip        (the double-clickable app)
#   $OUT/mailarchive-macos-universal  (universal CLI)
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 0.0.0-dev)}"
VERSION="${VERSION#v}"
OUT="${OUT:-$ROOT/bin}"
mkdir -p "$OUT"

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT

echo "==> macOS universal GUI .app + CLI (v$VERSION)"
GOOS=darwin GOARCH=arm64 go build -o "$WORK/gui-arm64" ./cmd/mailarchive-gui
GOOS=darwin GOARCH=amd64 go build -o "$WORK/gui-amd64" ./cmd/mailarchive-gui
GOOS=darwin GOARCH=arm64 go build -o "$WORK/cli-arm64" ./cmd/mailarchive
GOOS=darwin GOARCH=amd64 go build -o "$WORK/cli-amd64" ./cmd/mailarchive

app="$WORK/Mail Archive.app"
mkdir -p "$app/Contents/MacOS"
go run github.com/randall77/makefat "$app/Contents/MacOS/mailarchive-gui" "$WORK/gui-arm64" "$WORK/gui-amd64"
chmod +x "$app/Contents/MacOS/mailarchive-gui"
go run github.com/randall77/makefat "$OUT/mailarchive-macos-universal" "$WORK/cli-arm64" "$WORK/cli-amd64"
chmod +x "$OUT/mailarchive-macos-universal"

sed "s/@VERSION@/$VERSION/g" "$ROOT/packaging/macos/Info.plist.in" > "$app/Contents/Info.plist"

# Ship the open-me instructions beside the app so an unzip shows both, and a
# user who hits the unsigned-app warning (or opens the wrong file) has the fix
# in hand without the web page.
cp "$ROOT/packaging/macos/README-macOS.txt" "$WORK/READ ME — opening on macOS.txt"

rm -f "$OUT/MailArchive-macos.zip"
( cd "$WORK" && zip -q -r -y "$OUT/MailArchive-macos.zip" "Mail Archive.app" "READ ME — opening on macOS.txt" )

echo "    $OUT/MailArchive-macos.zip"
echo "    $OUT/mailarchive-macos-universal"
