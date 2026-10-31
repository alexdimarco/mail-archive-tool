#!/usr/bin/env bash
# Build the full cross-platform release matrix into dist/, plus SHA256SUMS.
# Used by both `make dist` and the release GitHub Action. Pure cross-compile.
#
# One program: every platform ships the single `mailarchive` program (its GUI
# surfaces are verbs, §4.1). On Windows that is a PAIR built from ./cmd/mailarchive
# — the console build `mailarchive.exe` and the no-console build `mailarchivew.exe`
# (-ldflags -H=windowsgui) — plus, from release N until the floor (§7.1), two
# byte-identical copies of mailarchivew.exe under the former names
# mailarchive-desktop.exe and mailarchive-gui.exe, so shortcuts and schedules
# installed under the old names keep working (MA-614).
#
# Usage: packaging/build-release.sh [VERSION]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-$(git describe --tags --always 2>/dev/null || echo 0.0.0-dev)}"
PRODUCT_VERSION="${VERSION#v}"   # leading-v stripped; the MSI binds ProductVersion to this (MA-614)
DIST="$ROOT/dist"
rm -rf "$DIST"
mkdir -p "$DIST"

ONE=./cmd/mailarchive

echo "==> Linux binaries (the one program; run as 'mailarchive wizard' / 'mailarchive desktop')"
GOOS=linux   GOARCH=amd64 go build -o "$DIST/mailarchive-linux-amd64" $ONE
GOOS=linux   GOARCH=arm64 go build -o "$DIST/mailarchive-linux-arm64" $ONE

# The Windows pair — the SAME program built two ways. MA-614 and MA-628 pin that
# these two GOOS=windows lines of ./cmd/mailarchive differ only in -o and in
# -ldflags -H=windowsgui on the mailarchivew line.
echo "==> Windows pair (console mailarchive.exe + no-console mailarchivew.exe)"
GOOS=windows GOARCH=amd64 go build -o "$DIST/mailarchive.exe" $ONE
GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui -o "$DIST/mailarchivew.exe" $ONE

echo "==> macOS .app bundle + universal CLI"
OUT="$DIST" RELEASE_TAG="$VERSION" bash "$ROOT/packaging/macos/build-app.sh" "$VERSION"

# Package the macOS CLI as a .tar.gz, never a bare binary: Finder opens a bare,
# extension-less Mach-O in a text editor when double-clicked, and a download loses
# the exec bit. A tarball unarchives to a clearly-named program beside its
# instructions, exec bit preserved.
echo "==> macOS CLI tarball"
CLIDIR="$DIST/mailarchive-cli-macos"
mkdir -p "$CLIDIR"
mv "$DIST/mailarchive-macos-universal" "$CLIDIR/mailarchive"
chmod +x "$CLIDIR/mailarchive"
cp "$ROOT/packaging/macos/README-macOS.txt" "$CLIDIR/READ ME.txt"
( cd "$DIST" && tar -czf mailarchive-cli-macos.tar.gz mailarchive-cli-macos )
rm -rf "$CLIDIR"

# Refuse, before packaging any release file, a program that is not exactly this
# release: every shipped binary — both Linux programs, the Windows pair, and the
# macOS slices (checked in build-app.sh) — must name the tag as its main-module
# version and a clean checkout (SC7, SC11; SC33 closed: Go stamps the tag of a clean
# tagged checkout). The program copies an encrypted archive keeps trust a release
# only when this holds (design-encrypted-archives-phase3-program §7). Every build, a
# release or not, must still be traceable to a clean commit (SC7, SC11); release.yml
# refuses a tag that is not exact, so a release never skips the version check.
require_stamped() {
  local f="$1" info
  info="$(go version -m "$f" 2>/dev/null || true)"
  grep -q 'vcs.revision=' <<<"$info"      || { echo "NOT STAMPED: $f has no vcs.revision (build from a committed checkout)"; exit 1; }
  grep -q 'vcs.modified=false' <<<"$info" || { echo "NOT STAMPED: $f is vcs.modified=true (commit or stash changes before a release build)"; exit 1; }
}
require_stamped "$DIST/mailarchive.exe"
require_stamped "$DIST/mailarchivew.exe"
# v0.0.0 is the lab builds' placeholder (windows-msi-lab.yml), never a release tag.
if [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ && "$VERSION" != v0.0.0 ]]; then
  bash "$ROOT/packaging/require-release-stamp.sh" "$VERSION" \
    "$DIST/mailarchive-linux-amd64" "$DIST/mailarchive-linux-arm64" \
    "$DIST/mailarchive.exe" "$DIST/mailarchivew.exe"
fi

# The Windows release asset: one zip of the install names the owner double-clicks.
# Flat members mailarchive.exe, mailarchivew.exe and READ ME.txt; the two programs are
# byte-identical to the MSI payload's, and READ ME.txt is the home user's note
# (README-windows.txt), not the payload's IT note (2026-10-31, the wording pass before
# 1.5.2: this zip is also the Windows copy an encrypted archive keeps). No bare
# -windows-amd64.exe is published (§4.10, SC11).
# OP-5: an operator who declines the zip ships the MSI alone and skips this block.
echo "==> Windows zip (install names)"
WINZIP="$DIST/mailarchive-windows"
mkdir -p "$WINZIP"
cp "$DIST/mailarchive.exe"  "$WINZIP/mailarchive.exe"
cp "$DIST/mailarchivew.exe" "$WINZIP/mailarchivew.exe"
cp "$ROOT/packaging/mailarchive-desktop/README-windows.txt" "$WINZIP/READ ME.txt"
( cd "$WINZIP" && zip -q -y "$DIST/mailarchive-windows-amd64.zip" mailarchive.exe mailarchivew.exe "READ ME.txt" )
rm -rf "$WINZIP"

# Windows MSI payload: the staging input for the WiX build. The PAIR under their
# install names, PLUS the two former-name copies of mailarchivew.exe (release N
# until the floor, §7.1), branding, the payload README, PRODUCT-VERSION.txt (the
# tag the MSI binds its ProductVersion to, MA-614) and the docs. One release asset.
echo "==> Desktop MSI payload (Windows)"
PAY="$DIST/mailarchive-desktop-windows-msi-payload"
mkdir -p "$PAY/branding" "$PAY/docs"
cp "$DIST/mailarchive.exe"  "$PAY/mailarchive.exe"
cp "$DIST/mailarchivew.exe" "$PAY/mailarchivew.exe"
cp "$DIST/mailarchivew.exe" "$PAY/mailarchive-desktop.exe"   # former-name copy of mailarchivew.exe (§7.1)
cp "$DIST/mailarchivew.exe" "$PAY/mailarchive-gui.exe"       # former-name copy of mailarchivew.exe (§7.1)
printf '%s\n' "$PRODUCT_VERSION" > "$PAY/PRODUCT-VERSION.txt"
cp "$ROOT/packaging/mailarchive-desktop/README-payload.txt" "$PAY/READ ME.txt"
# Branding art is optional until a real .ico exists (branding/README.txt names the
# expected files); copy whatever is present without failing the release.
cp -a "$ROOT/packaging/mailarchive-desktop/branding/." "$PAY/branding/" 2>/dev/null || true
cp "$ROOT/LICENSE"                    "$PAY/docs/LICENSE.txt"
cp "$ROOT/packaging/release-notes.md" "$PAY/docs/release-notes.md"
cp "$ROOT/docs/goback.md"             "$PAY/docs/goback.md"
cp "$ROOT/docs/graph-app-setup.md"    "$PAY/docs/graph-app-setup.md"
( cd "$DIST" && zip -q -r -y mailarchive-desktop-windows-msi-payload.zip mailarchive-desktop-windows-msi-payload )
rm -rf "$PAY"

echo "==> checksums"
( cd "$DIST" && sha256sum $(ls | grep -v '^SHA256SUMS$') > SHA256SUMS )

echo "Done -> $DIST"
ls -1 "$DIST" | sed 's/^/  /'
