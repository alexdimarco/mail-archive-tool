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

echo "==> CLI binaries (macOS ships as one universal tarball, below — not bare)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-linux-amd64"       $CLI
GOOS=linux   GOARCH=arm64 go build -o "$DIST/mailarchive-linux-arm64"       $CLI
GOOS=windows GOARCH=amd64 go build -o "$DIST/mailarchive-windows-amd64.exe" $CLI

echo "==> GUI binaries (Windows: no console; macOS ships as the .app, below)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-gui-linux-amd64"   $GUI
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o "$DIST/mailarchive-gui-windows-amd64.exe" $GUI

echo "==> macOS .app bundle + universal CLI"
OUT="$DIST" bash "$ROOT/packaging/macos/build-app.sh" "$VERSION"

# Package the macOS CLI as a .tar.gz, never a bare binary: Finder opens a bare,
# extension-less Mach-O in a text editor when double-clicked, and a download
# loses its executable bit. A tarball unarchives to a clearly-named program
# beside its instructions, with the exec bit preserved. Drop the bare universal
# CLI so the release page shows exactly two macOS downloads (the app + this).
echo "==> macOS CLI tarball"
CLIDIR="$DIST/mailarchive-cli-macos"
mkdir -p "$CLIDIR"
mv "$DIST/mailarchive-macos-universal" "$CLIDIR/mailarchive"
chmod +x "$CLIDIR/mailarchive"
cp "$ROOT/packaging/macos/README-macOS.txt" "$CLIDIR/READ ME.txt"
( cd "$DIST" && tar -czf mailarchive-cli-macos.tar.gz mailarchive-cli-macos )
rm -rf "$CLIDIR"

echo "==> checksums"
( cd "$DIST" && sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS )

echo "Done -> $DIST"
ls -1 "$DIST" | sed 's/^/  /'
