MailArchive's icon
==================

Design A (the owner's pick, 2026-11-04): a white letter dropping into an open
cardboard archive box, on a teal rounded square. One icon on every system.

The drawings (edit these):
  mailarchive.svg          the macOS drawing, on Apple's 824-of-1024 grid, with a shadow
  mailarchive-square.svg   the same, cut close with no outer shadow: Windows and Linux
  mailarchive-32.svg       the small drawing, for 24, 32 and 48 px
  mailarchive-16.svg       the 16 px drawing, every edge on a whole pixel

What is made from them (committed; never edit by hand):
  png/                     the master PNGs render.sh draws, and the smaller sizes pack makes
  mailarchive.icns         the Mac app's icon (Contents/Resources/, via build-app.sh)
  ../mailarchive-desktop/branding/mailarchive.ico
                           the Windows icon: the installer, its shortcuts, Add/Remove Programs
  ../../cmd/mailarchive/rsrc_windows_amd64.syso
                           the same .ico as a resource in mailarchive.exe and mailarchivew.exe
  ../../internal/desktop/favicon.png
                           MailArchive Desktop's browser-tab icon (the 32 px PNG)
  ../linux/build-desktop-entry.sh puts png/square-256.png and mailarchive-square.svg
                           in the Linux release asset mailarchive-linux-desktop-entry.tar.gz

To change the icon, on a Mac with Chromium or Google Chrome:
  1. edit the SVGs
  2. bash packaging/icon/render.sh     (draws the masters into png/)
  3. go run ./packaging/icon/pack       (from the repository root; makes everything else)
  4. go test ./packaging/icon/... ./cmd/mailarchive/ ./internal/desktop/
  5. commit every file that changed

CI and the release build only copy these files; a test (MA-1077) fails if any of
them is not what pack makes from the masters.

When Go is upgraded: pack writes PNGs with Go's own image/png and compress
packages, and MA-1077 compares bytes. A newer Go that compresses differently turns
MA-1077 red with "is not what pack makes from the masters". If only the
compression changed, the pixels are the same, and step 3 above (go run ./packaging/icon/pack), then steps 4 and 5, settle it.
Add this to the toolchain bump beside the other regenerated files.
