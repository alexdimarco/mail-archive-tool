MailArchive on Linux: a menu entry with its icon
================================================

These files add MailArchive Desktop to your app menu, with MailArchive's icon.
You need nothing else installed, and nothing here needs administrator rights.

1. Download the program from the same release into this folder:
   mailarchive-linux-amd64 (most computers), or mailarchive-linux-arm64 (an ARM
   computer, such as a Raspberry Pi).
2. Open a terminal in this folder and run these three lines:

   install -D -m 755 mailarchive-linux-amd64 ~/.local/bin/mailarchive
   install -D -m 644 mailarchive.png ~/.local/share/icons/hicolor/256x256/apps/mailarchive.png
   install -D -m 644 mailarchive.desktop ~/.local/share/applications/mailarchive.desktop

   On an ARM computer, write mailarchive-linux-arm64 in the first line instead.

MailArchive Desktop is now in your app menu. If clicking it does nothing, sign
out and back in once: some desktops only look in ~/.local/bin after that.

mailarchive.svg is the same icon as a drawing, for a desktop that wants one.

To update MailArchive later, click Shut down at the top of MailArchive Desktop's
page, then run the first line again with the new program.
To remove the menu entry, delete the three files the lines above made. Your
archives are not touched either way.
