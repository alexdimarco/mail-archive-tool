MailArchive for Windows
=======================

  mailarchivew.exe  Double-click this. It opens MailArchive Desktop in your web
                    browser: your archives, Settings, the scheduled backup with
                    Run now, backup alerts, Recent backups and Status & health.
                    For the step-by-step wizard, run: mailarchivew.exe wizard
  mailarchive.exe   The same program, for a terminal (Command Prompt or
                    PowerShell). Type: mailarchive.exe -h
                    Double-clicking it only tells you which file to double-click.

The first time, Windows may say "Windows protected your PC". Choose More info,
then Run anyway.

To read an archive: in MailArchive Desktop, choose "Open an archive folder…" and
pick the archive's folder. At a terminal:
  mailarchive.exe serve -out "<the archive's folder>"

Scheduled backups are set up in MailArchive Desktop's Settings card. They run
while you are signed in to Windows, even when MailArchive Desktop is closed.

Official download and checksums:
  https://github.com/alexdimarco/mail-archive-tool/releases
