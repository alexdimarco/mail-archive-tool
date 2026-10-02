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
DESKTOP=./cmd/mailarchive-desktop

echo "==> CLI binaries (macOS ships as one universal tarball, below — not bare)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-linux-amd64"       $CLI
GOOS=linux   GOARCH=arm64 go build -o "$DIST/mailarchive-linux-arm64"       $CLI
GOOS=windows GOARCH=amd64 go build -o "$DIST/mailarchive-windows-amd64.exe" $CLI

echo "==> GUI binaries (Windows: no console; macOS ships as the .app, below)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-gui-linux-amd64"   $GUI
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o "$DIST/mailarchive-gui-windows-amd64.exe" $GUI

echo "==> Desktop dashboard binaries (Windows: no console, same as GUI; Linux)"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-desktop-linux-amd64"   $DESKTOP
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o "$DIST/mailarchive-desktop-windows-amd64.exe" $DESKTOP

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

# Windows MSI payload: the staging input for Steve's (out-of-repo) WiX build.
# The three Windows binaries under their INSTALL names (no -windows-amd64 suffix,
# matching `make build-windows` and the names shortcuts/schtasks reference),
# plus branding art and the user docs the dashboard links to. Shipped as one
# release asset; the WiX source consumes it unchanged.
echo "==> Desktop MSI payload (Windows)"
PAY="$DIST/mailarchive-desktop-windows-msi-payload"
mkdir -p "$PAY/branding" "$PAY/docs"
cp "$DIST/mailarchive-desktop-windows-amd64.exe" "$PAY/mailarchive-desktop.exe"
cp "$DIST/mailarchive-gui-windows-amd64.exe"     "$PAY/mailarchive-gui.exe"
cp "$DIST/mailarchive-windows-amd64.exe"         "$PAY/mailarchive.exe"
cp "$ROOT/packaging/mailarchive-desktop/README-payload.txt" "$PAY/READ ME.txt"
# Branding art is optional until a real .ico exists (branding/README.txt names
# the expected files); copy whatever is present without failing the release.
cp -a "$ROOT/packaging/mailarchive-desktop/branding/." "$PAY/branding/" 2>/dev/null || true
cp "$ROOT/LICENSE"                   "$PAY/docs/LICENSE.txt"
cp "$ROOT/packaging/release-notes.md" "$PAY/docs/release-notes.md"
cp "$ROOT/docs/goback.md"            "$PAY/docs/goback.md"
cp "$ROOT/docs/graph-app-setup.md"   "$PAY/docs/graph-app-setup.md"
( cd "$DIST" && zip -q -r -y mailarchive-desktop-windows-msi-payload.zip mailarchive-desktop-windows-msi-payload )
rm -rf "$PAY"

echo "==> checksums"
( cd "$DIST" && sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS )

echo "Done -> $DIST"
ls -1 "$DIST" | sed 's/^/  /'
