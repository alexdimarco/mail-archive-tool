#!/bin/bash
# Render MailArchive's icon drawings to the master PNGs in png/, once, on a Mac with
# Chromium (or Google Chrome) installed. The PNGs are committed; CI and the release
# build never run this. Then run, from the repository root,
#   go run ./packaging/icon/pack
# to make the smaller sizes from the masters and rebuild mailarchive.icns, the Windows
# .ico, the Desktop page's favicon and the .exe files' icon resource (README.txt).
#
# Which drawing makes which master:
#   mailarchive-16.svg      -> png/mailarchive-16.png
#   mailarchive-32.svg      -> png/mailarchive-24.png, -32.png, -48.png
#   mailarchive.svg         -> png/macos-1024.png    (macOS, on Apple's grid)
#   mailarchive-square.svg  -> png/square-1024.png   (Windows and Linux)
#
# Headless Chromium draws each SVG into a window of the size asked for, through
# render.html, on a transparent background.
set -euo pipefail
D="$(cd "$(dirname "$0")" && pwd)"
CHR="${CHROMIUM:-/Applications/Chromium.app/Contents/MacOS/Chromium}"
[ -x "$CHR" ] || CHR="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
[ -x "$CHR" ] || { echo "no Chromium or Google Chrome found; set CHROMIUM=/path/to/binary"; exit 1; }
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$D/png"

shot() { # svg size out
  rm -f "$3"
  "$CHR" --headless --disable-gpu --hide-scrollbars --default-background-color=00000000 \
    --force-device-scale-factor=1 --virtual-time-budget=3000 --window-size="$2,$2" \
    --user-data-dir="$TMP/profile" --screenshot="$3" "file://$D/render.html?$1" >/dev/null 2>&1 || true
  [ -s "$3" ] || { echo "no image for $1 at $2"; exit 1; }
}

shot mailarchive-16.svg 16 "$D/png/mailarchive-16.png"
for s in 24 32 48; do shot mailarchive-32.svg "$s" "$D/png/mailarchive-$s.png"; done
shot mailarchive.svg 1024 "$D/png/macos-1024.png"
shot mailarchive-square.svg 1024 "$D/png/square-1024.png"
ls -la "$D/png"
