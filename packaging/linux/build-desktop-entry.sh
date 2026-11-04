#!/usr/bin/env bash
# Write DIST/mailarchive-linux-desktop-entry.tar.gz: the files that add MailArchive
# Desktop, with its icon, to a Linux app menu (the owner's ruling of 2026-11-04). The
# bare programs mailarchive-linux-amd64 and -arm64 stay as they are; this is one more
# release asset beside them, made by build-release.sh.
#
# It holds one folder, mailarchive-linux-desktop-entry/, with:
#   mailarchive.png      the icon, 256 px (packaging/icon/png/square-256.png)
#   mailarchive.svg      the icon as a drawing (packaging/icon/mailarchive-square.svg)
#   mailarchive.desktop  the menu entry (runs `mailarchive desktop`)
#   READ ME.txt          the three lines that put them in place
#
# Usage: packaging/linux/build-desktop-entry.sh DIST
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
DIST="${1:?usage: build-desktop-entry.sh DIST}"
NAME=mailarchive-linux-desktop-entry

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir "$WORK/$NAME"
cp "$ROOT/packaging/icon/png/square-256.png"      "$WORK/$NAME/mailarchive.png"
cp "$ROOT/packaging/icon/mailarchive-square.svg"  "$WORK/$NAME/mailarchive.svg"
cp "$ROOT/packaging/linux/mailarchive.desktop"    "$WORK/$NAME/mailarchive.desktop"
cp "$ROOT/packaging/linux/README-desktop-entry.txt" "$WORK/$NAME/READ ME.txt"
chmod 755 "$WORK/$NAME"
chmod 644 "$WORK/$NAME"/*
# COPYFILE_DISABLE: on a Mac, tar would otherwise add ._ files for extended attributes.
( cd "$WORK" && COPYFILE_DISABLE=1 tar -czf "$DIST/$NAME.tar.gz" "$NAME" )
echo "    $DIST/$NAME.tar.gz"
