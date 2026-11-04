MailArchive Desktop — branding assets for the Windows MSI
=========================================================

Drop the installer/product art here; packaging/build-release.sh copies every
file in this directory (except this README) into the MSI payload's branding/
folder, and Steve's WiX build references them from there.

  mailarchive.ico        app + Add/Remove-Programs icon (multi-size .ico).
                         MailArchive's icon (design A, 2026-11-04), made by
                         packaging/icon/pack from packaging/icon/png: do not edit
                         it by hand; see packaging/icon/README.txt. The same
                         file is in mailarchive.exe and mailarchivew.exe, through
                         cmd/mailarchive/rsrc_windows_amd64.syso.

Expected later (supply as they are produced; the release build does not require them):

  banner.bmp             WiX top banner   (493 x 58 px, 24-bit BMP)
  dialog.bmp             WiX dialog side   (493 x 312 px, 24-bit BMP)
  license.rtf            license text shown by the installer (AGPL-3.0)
