MailArchive Desktop — branding assets for the Windows MSI
=========================================================

Drop the installer/product art here; packaging/build-release.sh copies every
file in this directory (except this README) into the MSI payload's branding/
folder, and Steve's WiX build references them from there.

Expected (supply as they are produced; the release build does not require them):

  mailarchive.ico        app + Add/Remove-Programs icon (multi-size .ico)
  banner.bmp             WiX top banner   (493 x 58 px, 24-bit BMP)
  dialog.bmp             WiX dialog side   (493 x 312 px, 24-bit BMP)
  license.rtf            license text shown by the installer (AGPL-3.0)

Until a real .ico exists the binaries build and ship fine; only the MSI's icon
and installer chrome are placeholders.
