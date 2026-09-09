Mail Archive — opening it on macOS
==================================

Most people want the app. The command-line tool is optional and separate.


The app (double-click)
----------------------

The app is "Mail Archive.app". It is NOT signed by Apple (there is no paid
developer certificate), so the FIRST time you open it macOS warns that it is
from an unidentified developer. That is expected — you only do this once:

  1. If it is still zipped, double-click "MailArchive-macos.zip" to unzip it.
  2. RIGHT-CLICK (or Control-click) "Mail Archive.app" and choose Open.
  3. In the warning box, click Open again.

After that first time it opens with a normal double-click.

If a double-click ever opens something as TEXT, or nothing happens, you are
opening the wrong file. The app is "Mail Archive.app" — a bundle with the app
icon — not a plain file with no icon. Do not double-click the command-line
program below; macOS opens bare programs in a text editor.


The command-line tool (optional, Terminal only)
-----------------------------------------------

"mailarchive" (inside mailarchive-cli-macos.tar.gz) is a Terminal program, not
a double-click app. Double-clicking it opens it in a text editor — that is
normal for a command-line program. Run it from Terminal instead:

  tar -xzf mailarchive-cli-macos.tar.gz
  cd mailarchive-cli-macos
  xattr -cr mailarchive         # clears the "downloaded from the internet" block
  ./mailarchive -h

It is a universal binary — it runs on both Apple Silicon and Intel Macs.
