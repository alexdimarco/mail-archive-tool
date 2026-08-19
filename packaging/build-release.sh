#!/usr/bin/env bash
# Build the full cross-platform release matrix into dist/, plus SHA256SUMS.
# Used by both `make dist` and the release GitHub Action. Pure cross-compile.
#
# Usage: packaging/build-release.sh [VERSION]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 0.0.0-dev)}"
DIST="$ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST"

CLI=./cmd/mailarchive
GUI=./cmd/mailarchive-gui

echo "==> CLI binaries"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-linux-amd64"       $CLI
GOOS=linux   GOARCH=arm64 go build -o "$DIST/mailarchive-linux-arm64"       $CLI
GOOS=windows GOARCH=amd64 go build -o "$DIST/mailarchive-windows-amd64.exe" $CLI
GOOS=darwin  GOARCH=arm64 go build -o "$DIST/mailarchive-darwin-arm64"      $CLI
GOOS=darwin  GOARCH=amd64 go build -o "$DIST/mailarchive-darwin-amd64"      $CLI

echo "==> GUI binaries (Windows: no console)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-gui-linux-amd64"   $GUI
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o "$DIST/mailarchive-gui-windows-amd64.exe" $GUI
GOOS=darwin  GOARCH=arm64 go build -o "$DIST/mailarchive-gui-darwin-arm64"  $GUI
GOOS=darwin  GOARCH=amd64 go build -o "$DIST/mailarchive-gui-darwin-amd64"  $GUI

echo "==> macOS .app bundle + universal CLI"
OUT="$DIST" bash "$ROOT/packaging/macos/build-app.sh" "$VERSION"

echo "==> checksums"
( cd "$DIST" && sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS )

echo "Done -> $DIST"
ls -1 "$DIST" | sed 's/^/  /'
