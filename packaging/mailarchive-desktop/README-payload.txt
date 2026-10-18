MailArchive Desktop — Windows MSI payload
=========================================

This folder is the staging input for Steve's WiX build. packaging/build-release.sh
assembles it (see "==> Desktop MSI payload") and zips it as the release asset
mailarchive-desktop-windows-msi-payload.zip. Unzip it next to the .wxs and build.

Contents of the payload
------------------------
  mailarchive.exe           the command-line build (console). Used for app-only /
                            multi-mailbox graph captures and headless CLI use.
  mailarchivew.exe          the no-console build (-ldflags -H=windowsgui) of the same
                            program. mailarchivew.exe desktop is the dashboard-and-
                            launcher the Start-menu / Desktop shortcut runs, and
                            mailarchivew.exe wizard is the native-dialog wizard. The
                            Desktop's own weekly backup runs mailarchivew.exe desktop
                            -capture in-process (DC1), never a separate engine (P1).
  mailarchive-desktop.exe   a former name: a byte-identical copy of mailarchivew.exe.
  mailarchive-gui.exe       a former name too, a copy of mailarchivew.exe, kept this
                            release so shortcuts and schedules under the old names keep
                            running; a later release removes them.
  branding/                 icon + installer art (see branding/README.txt).
  docs/                     LICENSE.txt, release-notes.md, and the user docs the
                            dashboard links to (goback, graph-app-setup).

The install names drop the -windows-amd64 suffix (mailarchive.exe, mailarchivew.exe,
and the former names mailarchive-desktop.exe and mailarchive-gui.exe) — the same
names the build-windows target produces and that shortcuts / schtasks reference.

How the MSI launches it (no-console note)
-----------------------------------------
mailarchivew.exe is a GUI-subsystem binary, so it has NO console: nothing it
prints is visible when double-clicked. The binary therefore opens the dashboard in
the OS default browser itself once the server is listening (MA-281, former risk
R1) — the Desktop/Start-menu shortcut needs no custom action. It opens it at a
private one-time link it makes for the Windows account that started it: the
dashboard and the reader each listen on a free loopback port (there is no fixed
address, and none is to be typed or bookmarked), and a page reached without the
link shows only "This MailArchive Desktop page has to be opened from the app". If
an instance of this account is already running (the Startup shortcut started one at
login) the launch reconnects to it — once the instance has proved itself with a key
only this account can read — and has it open the browser, instead of starting a
second one; a launch with another configuration, or that finds the instance not
responding, says so in a dialog. If the browser cannot be opened, a dialog shows
the link with a Copy button, and the Start-menu shortcut "Copy MailArchive link"
(mailarchivew.exe desktop -copy-link) does the same on demand. The Startup shortcut
passes -no-browser so the at-login launch stays quiet (no browser tab every
sign-in). The dashboard and its reader bind loopback only and refuse any other
address (design P2 / X9).

MSI identity (design DC6): use a stable UpgradeCode so our MSI cleanly replaces
Steve's earlier out-of-repo build (both detect HKLM\Software\MailArchive Desktop);
uninstall must preserve the user's archive directory.
