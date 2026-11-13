MailArchive on Linux: a menu entry with its icon
================================================

These files add MailArchive Desktop to your app menu, with MailArchive's icon.
You need nothing else installed, and nothing here needs administrator rights.

1. Put the program in this folder. Download it from the same release page as
   these files, then move it here from your Downloads folder:
   mailarchive-linux-amd64 for most computers, or mailarchive-linux-arm64 for a
   64-bit ARM computer, such as a Raspberry Pi running a 64-bit system.
2. Open a terminal in this folder (in most file managers: right-click an empty
   spot in the folder and choose Open in Terminal), then run these three lines:

   install -D -m 755 mailarchive-linux-amd64 ~/.local/bin/mailarchive
   install -D -m 644 mailarchive.png ~/.local/share/icons/hicolor/256x256/apps/mailarchive.png
   sed "s|^Exec=mailarchive |Exec=\"$HOME/.local/bin/mailarchive\" |" mailarchive.desktop | install -D -m 644 /dev/stdin ~/.local/share/applications/mailarchive.desktop

   On an ARM computer, write mailarchive-linux-arm64 in the first line instead.
   The third line is long: copy it whole. It writes where the program now is
   (in your home folder) into the menu entry, so the menu can start it.

If MailArchive Desktop is not in your app menu yet, or clicking it does
nothing, sign out and back in once: some menus only notice a new entry after a
new sign-in. If it is still missing after that, you can always start it from a
terminal with:

   ~/.local/bin/mailarchive start

You can leave mailarchive.svg where it is: it is the same icon as a drawing,
and the three lines do not need it.

To update MailArchive later, click Shut down at the top of MailArchive Desktop's
page, put the new program in this folder, and run the first line again.

To remove MailArchive, first click the Uninstall MailArchive button on
MailArchive Desktop's page: it stops your scheduled backups and removes
MailArchive's settings. Then delete the three files the lines above made (to
remove only the menu entry and its icon, delete the last two). Your archives
are not touched either way.
