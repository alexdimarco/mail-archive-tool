MailArchive Desktop — Windows MSI payload
=========================================

This folder is the staging input for Steve's WiX build. packaging/build-release.sh
assembles it (see "==> Desktop MSI payload") and zips it as the release asset
mailarchive-desktop-windows-msi-payload.zip. Unzip it next to the .wxs and build.

Contents of the payload
------------------------
  mailarchive-desktop.exe   the dashboard-and-launcher (no console window;
                            built -ldflags -H=windowsgui). This is the product
                            the Start-menu / Desktop shortcut launches.
  mailarchive-gui.exe       the native-dialog wizard (no console window).
  mailarchive.exe           the engine CLI (console). Bundled for app-only /
                            multi-mailbox graph captures and headless CLI use; the
                            dashboard never shells out to it at runtime (in-process,
                            P1). The Desktop's own weekly backup does NOT use this —
                            it runs mailarchive-desktop.exe -capture in-process (DC1).
  branding/                 icon + installer art (see branding/README.txt).
  docs/                     LICENSE.txt, release-notes.md, and the user docs the
                            dashboard links to (goback, graph-app-setup).

The install names drop the -windows-amd64 suffix (mailarchive.exe,
mailarchive-gui.exe, mailarchive-desktop.exe) — the same names the Makefile's
build-windows target produces and that shortcuts / schtasks reference.

How the MSI launches it (no-console note)
-----------------------------------------
mailarchive-desktop.exe is a GUI-subsystem binary, so it has NO console: the URL
it prints to stdout (http://127.0.0.1:8097/) is invisible when double-clicked.
The binary therefore opens that URL in the OS default browser itself once the
server is listening (MA-281, former risk R1) — the Desktop/Start-menu shortcut
needs no custom action. If an instance is already running (the Startup shortcut
started one at login) it reconnects and opens the browser to that instance
instead of failing to bind. The Startup shortcut passes -no-browser so the
at-login launch stays quiet (no browser tab every sign-in). The dashboard and its
reader bind loopback only and refuse any other address (design P2 / X9).

MSI identity (design DC6): use a stable UpgradeCode so our MSI cleanly replaces
Steve's earlier out-of-repo build (both detect HKLM\Software\MailArchive Desktop);
uninstall must preserve the user's archive directory.
