**MailArchive** — a mail archiver for Outlook `.pst`/`.ost`, Thunderbird, Evolution
and Microsoft 365. It saves your mail as self-contained web pages with attachment
zips and a full-text search index, encrypted if you choose. It comes with
**MailArchive Desktop** (a private dashboard in your browser for your archives,
scheduled backups, alerts and status), a step-by-step wizard, and a command line.

### macOS — start here
- **GUI (double-click app):** download **`MailArchive-macos.zip`**, unzip it, then
  because it's unsigned: **right-click "Mail Archive.app" → Open → Open** (once).
  Or in Terminal: `xattr -cr "Mail Archive.app"` then double-click. Universal —
  runs on Apple Silicon and Intel.
- **CLI (optional, Terminal):** download **`mailarchive-cli-macos.tar.gz`** — a
  command-line program, **not** a double-click app (double-clicking a bare
  program opens it in a text editor). In Terminal:
  `tar -xzf mailarchive-cli-macos.tar.gz && cd mailarchive-cli-macos && xattr -cr mailarchive && ./mailarchive -h`.
  Universal — Apple Silicon and Intel.
- macOS ships exactly these two downloads (the app and the CLI tarball); there
  are no bare per-arch macOS binaries to pick wrong.

### Windows — start here
- **Install it (recommended):** download **`MailArchiveDesktop-<version>-x64.msi`** (the
  installer; `<version>` is this release's number, for example `MailArchiveDesktop-1.5.4-x64.msi`)
  and double-click it. The installer is not signed yet, so you will see warnings:
  - Your browser may warn when you download it. In Microsoft Edge, if it says the file
    "isn't commonly downloaded", choose **Keep** (and **Keep anyway** if it asks again).
  - If **"Windows protected your PC"** appears, click **More info**, then **Run anyway**.
  - If Windows says **"The publisher could not be verified"**, click **Run**.
  - Windows then asks for permission to make changes, and names the publisher as
    **Unknown publisher**. Click **Yes**. Installing needs administrator rights: if your
    account is not an administrator, Windows asks for an administrator's password.

  It installs MailArchive for everyone on this computer, in Program Files. It adds
  **MailArchive Desktop**, **MailArchive wizard**, **Copy MailArchive link** and
  **Uninstall MailArchive Desktop** to the Start menu and a **MailArchive Desktop** shortcut
  on the desktop, and it starts MailArchive Desktop quietly, without opening your browser,
  each time anyone signs in to this computer (every account on it, not only yours). Open
  **MailArchive Desktop** from the Start menu to begin.
- **Updating to a newer version.** First click **Shut down** at the top of MailArchive
  Desktop's page; if a backup is running, let it finish first. On a computer several
  people are signed in to, each of them has MailArchive Desktop running: ask them to sign
  out first. Then download the new installer and double-click it: it replaces the
  installed version. If you forgot to shut it down, the old version keeps running until
  you sign out and back in (or restart the computer), so do that once.
- **Removing it.** First, in MailArchive Desktop, click **Uninstall MailArchive…**: it
  removes your scheduled backups, settings and sign-in from this computer (removing the
  program alone leaves the scheduled backups behind, pointing at a program that is gone). Then use **Settings → Apps → Installed apps → MailArchive
  Desktop → Uninstall**. Your archives are kept either way.
- **No administrator rights?** Download **`mailarchive-windows-amd64.zip`** instead, unzip it,
  then double-click `mailarchivew.exe` (if SmartScreen warns: **More info → Run anyway**). It
  works the same, but it has no Start menu entries, no desktop shortcut, and does not start
  when you sign in.
- **Moving from the zip to the installer.** Your archives stay where they are; only the
  program changes. A scheduled backup you set up with the zip still runs the programs in
  the unzipped folder, so point it at the installed program before you delete that folder:
  1. If MailArchive Desktop is open, click **Shut down** at the top of its page.
  2. Install the MSI as above, then open **MailArchive Desktop** from the Start menu.
  3. For each archive with a scheduled backup: open it (in **Your archives**, click **Switch
     to this archive** if it is not the one shown), then click **Repair** on its **Scheduled
     backup** card. How often it runs and
     its Thunderbird sync setting are kept. A backup set up at the command line has no
     **Repair** button: run its `mailarchive schedule … -install` command again with the
     installed `C:\Program Files\MailArchive Desktop\mailarchive.exe`.
  4. Then delete the unzipped folder.

Verify downloads against `SHA256SUMS` (the installer is listed there too). For an IT/RMM
rollout, the same installer installs silently (`msiexec /i MailArchiveDesktop-<version>-x64.msi /qn`),
and `mailarchive-desktop-windows-msi-payload.zip` still lets you build the MSI yourself
(`packaging/mailarchive-desktop/README-RMM.md`).

### Linux

MailArchive is one program with verbs; there is no separate GUI download.

| Platform | Download | Run |
|---|---|---|
| Linux x86-64 | `mailarchive-linux-amd64` | `mailarchive wizard` or `mailarchive desktop` (the wizard needs `zenity` and a desktop session) |
| Linux arm64 | `mailarchive-linux-arm64` | the same verbs (compiled, not lab-run) |
| Linux app menu (optional) | `mailarchive-linux-desktop-entry.tar.gz` | adds **MailArchive Desktop**, with its icon, to your app menu: unpack it and follow its `READ ME.txt` |

### New in 1.7.0 — MailArchive tells you whether Thunderbird has downloaded all of your mail
- **After each backup of Thunderbird mail, MailArchive compares what it saved with what Thunderbird lists.** Each account gets one of three plain answers: "Backup complete." when Thunderbird has downloaded everything it lists, "Backed up what Thunderbird has so far." with both numbers (for example "about 120,000 of 318,861 messages") while Thunderbird is still downloading, or "Backup finished." with one sentence saying why MailArchive cannot tell. You see it in the wizard, Run now, Recent backups, the archive card, Status & health and `mailarchive status`. Local Folders and POP accounts get no such line.
- **Gmail is counted the way Gmail keeps mail.** One message with several labels counts once; All Mail sets the total. Spam and Trash, which Thunderbird does not keep on the computer by default, get a note, not a warning.
- **Accounts are named the way Thunderbird names them**, usually the email address. Alert emails name the account by its folder instead, and log files never contain the address.
- **Every fix-it step starts where you click:** "in Thunderbird, click the menu button (≡, top right), then Account Settings…", and names the page and the setting as Thunderbird shows them (checked in Thunderbird 153 and 159; Thunderbird 160's renamed settings are quoted too).
- **One email if Thunderbird is still downloading after 3 days, and one if it stops.** Scheduled backups with email alerts on send "MailArchive: Thunderbird still downloading (backups OK)" once per download, and "…stopped downloading (backups OK)" only after 3 days and at least a day of backups with no progress. Never for an encrypted archive. A computer whose clock is wrong is told to check its date and time instead of getting a wrong email.
- **Alert emails no longer repeat.** An email the mail server accepted counts as sent even if the connection drops right after, which could make the "Thunderbird not synced" email arrive at every backup. An email that cannot be sent is tried 5 times, then once a day, and Status & health says to check Backup alerts and use Send test email; any email that gets through starts the tries again.
- **`-enable-offline` and the wizard's "Yes, prepare" pick the right account and never guess.** Two accounts on one server (two Gmail accounts, for example) each get their own setting; a removed account's leftovers are ignored. If Thunderbird's settings files hold anything MailArchive cannot read with certainty, it changes nothing and says which setting to tick in Thunderbird yourself. The first original `prefs.js` is kept as `prefs.js.mailarchive-bak`.
- **Linux:** the wizard's Backup health text now opens in a plain message window before the list, so names and the copyable `verify` command show exactly as written.
- **For scripts:** `mailarchive status -json` adds `thunderbird_completeness`, `thunderbird_lead`, `thunderbird_more` and `behind_since`; the archive's `README.txt` names the new `.mailarchive-tbstreak.json` file, which counts how long Thunderbird has been downloading.
- **Not yet tried:** the Linux dialogs on a real Linux desktop, and Thunderbird 160 on screen.

### Changed in 1.6.2 — clearer words after a backup, and a link to report a mistake
- **The address to report a mistake in MailArchive is a link.** On MailArchive Desktop's page (Recent backups, the Scheduled backup card's note, Archive now and Status & health) https://github.com/alexdimarco/mail-archive-tool/issues opens in a new tab when you click it.
- **Recent backups no longer repeats itself.** Backups in a row that ended the same way are one line that says how often and when the last one ran, for example "Incomplete (3 times, last at 5:04 PM)". Counts read in plain words: "1 message", "1,240 messages".
- **A backup that may be stuck on another computer says which computer it calls itself.** When an archive on a shared drive is held by a backup that started more than a day ago on another computer, the words say "another computer, which calls itself OFFICE-PC" and: "If that computer is yours and the backup should have finished by now, restart it to end the backup (restarting this one does not); if you don't recognise it, check who else uses this folder before restarting anything." Only the part of the name before its first dot is said.
- **Direction-changing characters are left out beside the report link.** Words in Recent backups, Archive now, Run now and Status & health no longer show the invisible characters that can turn text around, so the report address always reads as itself.
- **`mailarchive status` and Status & health count what was not captured in plain words**: "1 folder is NOT in this archive", "3 messages could not be captured", never "item(s)" or "folder(s)".
- **BACKUP-NEEDS-ATTENTION.txt says when in your own time**, with its offset from UTC (for example "2026-11-07 17:25:10 -05:00"), as the alert email does.
- **Run now after a backup that did not save everything also says which messages are not downloaded in full yet**, as it does after a backup that finished.
- **Messages not downloaded in full are worded by where they came from.** Thunderbird's own mail says "Thunderbird has not downloaded them yet". A plain mail file says "the mail file holds only part of these messages; if it is a copy, copy it again from its mail program", and no longer blames a mail app.
- **Windows: the commands `mailarchive status` and Status & health print name the archive folder as you type it**, for example `mailarchive verify -out "C:\Users\Public\Archive"`, not with every backslash doubled. A drive root such as `E:\` keeps its closing quote.
- **The scheduled backup's log names the alert email it sent:** "backup alert email sent (did not save everything)" or "(skipped, the archive was still in use)", instead of "(failed)" for both.
- **`mailarchive status` and Status & health use the same words for messages not in full.** A plain mail file's archive no longer says "download for offline use in your mail app"; it says the mail file holds only part of them, as Run now and Recent backups do.
- **`mailarchive status` and Status & health count properly:** "1 message is kept", "2 runs, 5 events", "1 file with no recorded fixity", never "message(s)" or "run(s)". A folder's line says "(the original is untouched; 3 messages not read)" in one bracket, not two in a row.
- **An Incomplete backup in Recent backups also says which messages are not in full yet**, as a backup that finished does.
- **Windows: the backup's own summary in the command line, and the wizard's summary window, name the archive folder as you type it**, for example `mailarchive serve -out "C:\Users\Public\Archive"`, not with every backslash doubled.
- **For scripts:** the last-run record and the backup history add `fillable_from` (`thunderbird`, `mail_app`, `mail_file` or `microsoft365`): where the run read its mail. Nothing is renamed.

### Changed in 1.6.1 — backups that could not save everything say why and what to do
- **A backup that could not save everything now says why and what to do.** Instead of "Failed — partial: 4 item(s) not captured — see attachments-report.tsv", Recent backups says **Incomplete** and, in plain words, what was not saved and what to do — for example "4 messages could not be saved because of a mistake in MailArchive — your mail is fine (the original is untouched); please report it at https://github.com/alexdimarco/mail-archive-tool/issues". The same words are on the Scheduled backup card, in Status & health, in `mailarchive status`, in the alert email and in the BACKUP-NEEDS-ATTENTION.txt file. The attachments-report.tsv file is still there, named as the details.
- **A backup that meets another one still running is no longer called "failed".** Clicking **Run now** while a scheduled backup is still running now says "Another backup of this archive is already running (started 20:00 …) — this backup was skipped; nothing is wrong. Try again when it finishes." If the backup that is running started more than a day ago, the words say it may be stuck and how to end it.
- **Mail Thunderbird has not downloaded yet is said as that.** Recent backups and Run now say how many messages are not on this computer in full yet, and that later backups add them once Thunderbird has downloaded them.
- **"Thunderbird was already open" now says what the backup did:** "Thunderbird was already open, so the backup left it alone".
- **For scripts:** `mailarchive status -json` adds `last_run.partial` (what a partial run could not save, by kind). The reason codes, `last_run.status` and the gap counts are unchanged; the text in `last_run.error` is the new plain words.

### New in 1.6.0 — restore an archive into Thunderbird
- **Put an archive's mail back into Thunderbird.** MailArchive Desktop has a new **Restore to Thunderbird** card, shown once an archive is chosen. Click **Restore to Thunderbird…**, choose **The whole archive** or **Choose folders**, then click **Check**: it shows where the mail will go, how much there is, what cannot go back and why, and the free space, and writes nothing yet. Click **Restore** to go ahead. **Cancel the restore** stops it and removes what it wrote, so nothing is added.
- **It adds one new folder and changes nothing else.** The mail goes into a new folder under **Local Folders** in Thunderbird, named **Restored — ** and the archive folder's name (for example **Restored — Work mail**). Your Thunderbird accounts and the mail already in Thunderbird are never touched.
- **Close Thunderbird first, and leave it closed until the restore is finished.** MailArchive never starts or closes Thunderbird itself. If Thunderbird is open, the restore says so and writes nothing.
- **You get back the exact original emails.** The archive keeps each email as it arrived, and the restore puts that same email back. Before Thunderbird can show the new folder, MailArchive reads back every message it wrote and checks it against the archive's copy. The result says how many messages went back; **Save this record** keeps the plan and the result as a text file.
- **What cannot be restored, and why.** Only mail whose original email the archive kept can go back.
  - An archive made from Outlook data files (`.pst` or `.ost`) cannot be restored into Thunderbird: Outlook keeps mail in its own format, so there is no original email file to put back.
  - Mail in an archive made without keeping each message's original (the archive's **Settings** card shows this) has no original either, so it cannot go back. To be able to restore later, make the archive with keeping originals on. For a Microsoft 365 archive, ticking **Keep each message's original .eml too** in Settings keeps the originals of mail backed up from then on.
  - The plan says how many messages cannot go back, and why.
- **Encrypted archives** can be restored only while they are unlocked. The restored copy in Thunderbird is **not** encrypted, so you tick **I understand the restored mail will not be encrypted** first. The new folder and its files are made readable only by your account on this computer. If that cannot be promised (for example on a USB drive, a network share or a folder a sync service uploads), the plan says who else could read it and asks you a second time.
- **Some folder names are changed.** A folder name that Thunderbird would not read back properly, or that Windows does not allow (for example one holding `#`, `;` or `%`, or longer than 55 characters), is changed to a safe one, shortened where needed with a short code at the end to keep names apart. The plan lists each changed name, old name → new name (up to 50, then how many more), and Thunderbird shows the changed names. For an encrypted archive the plan gives only how many names were changed, so the folder names are not shown. Folders nested too deep for Windows to hold are named in the plan and are not restored.
- **A Thunderbird profile with no Local Folders** (a Thunderbird with no account yet) cannot be restored into, and Check says so. To make Local Folders: in Thunderbird, click the menu button (**≡**, top right), then **Add Account**, then **Newsgroup**. Type any name, email address and server name (for example `news.example.invalid`) and click **Next** until **Finish**. You may delete that newsgroup account afterwards in Account Settings; Local Folders stays. Then close Thunderbird and click **Check** again.
- **Backups leave the restored folder out.** When this archive backs up the same Thunderbird profile, its backups skip the restored folder, so the restored mail is not copied into the archive a second time. A restore that did not finish (for example because the computer was switched off) leaves a folder Thunderbird does not show; MailArchive Desktop says so at the top of its page, and the next restore into that profile removes it.
- **At the command line:** `mailarchive restore-thunderbird -out FOLDER` shows the same plan and asks before it restores. `-plan` only shows the plan, `-list-profiles` lists this computer's Thunderbird profiles, and `-folder PATH` (repeatable) restores only some folders.
- **Also in 1.6.0: an archive in a cloud-sync folder (OneDrive, Dropbox, Google Drive, iCloud Drive, Nextcloud and others) is no longer a warning.** You chose that folder, so **Status & health** and `mailarchive status` now show it as an information line: it still says the folder is synced and what a local, non-synced folder would change, but the archive's status stays green.
- **Tested on:**
  - **Windows 11 with Thunderbird 153.6.0** (on a test computer, with Microsoft Defender's real-time scanning on), each passed: a test archive with nested folders, non-English folder names, Gmail labels and mixed line endings, every message compared byte for byte (lab row MA-1093); a deep folder tree under a long user name (MA-1092); unusual and over-long folder names, and which Thunderbird setting decides where Local Folders is (MA-1095, after a fix found by this same test); Thunderbird started during a restore stays out and opens normally afterwards (MA-1090, and MA-1134's Windows half); MailArchive force-closed part way, and a cancelled restore, leave nothing Thunderbird shows, and the next restore cleans up (MA-1135); a scheduled backup that starts during a restore (MA-1137); a folder of 4.3 GB (MA-1121); making Local Folders through the Newsgroup menu (MA-1133). In part: Defender scanning during restores gave clean results (MA-1136), and the plan warns about a profile on a network drive (MA-1097).
  - **macOS and Linux:** only the lock that keeps Thunderbird out during a restore was tried (MA-1134: macOS with Thunderbird 159.0, and Linux with Thunderbird 153.6.0esr in a test container without a screen).
  - **Not yet tried:** a full restore on a macOS or Linux desktop, Thunderbird 115, the Microsoft Store version of Thunderbird, Local Folders inside a OneDrive folder, and an antivirus program actually quarantining a restored file.

### Fixed in 1.5.9 — backups no longer fail on old mail with oddly named pictures
- **Backups no longer fail on an old message whose inline picture has an unusual name.** Some older mail programs (for example a Korean one from around 2013) named a picture inside an email in their own character set. MailArchive stopped on such a message with "export-panic", so every backup of that mailbox said **Failed**, even though everything else was saved. Those messages are now archived with their pictures, and the backup finishes normally.

### New in 1.5.8 — MailArchive has its own icon on Mac, Windows and Linux
- **MailArchive has an icon.** The same icon — a letter dropping into an archive box — now shows for the Mac app in the Finder and the Dock (where the app is also filed under Productivity), for `mailarchive.exe`, `mailarchivew.exe` and the installer's shortcuts on Windows, and in the browser tab of MailArchive Desktop. On Linux, the new download `mailarchive-linux-desktop-entry.tar.gz` adds MailArchive Desktop to your app menu with the icon; the Linux programs themselves are unchanged.

### Fixed in 1.5.7 — making an encrypted archive shows each step, and the recovery key is told apart from the recovery file
- **Making an encrypted archive no longer seems to stop after you choose its folder.** MailArchive Desktop now moves the page to the password boxes and puts the cursor in the first one, with a line saying what to do next (it did not move, so the page looked stuck while it waited for a password far down the page). The same happens at each later step, and after **Open an archive folder…** opens a locked encrypted archive. The page also says plainly that the **recovery key** (short groups of letters and digits) is what you type back, and that the **recovery file** is a different thing that MailArchive opens itself: if you paste the recovery file's contents where the key is asked, it now says so.
- **After an encrypted archive is made, the page shows its first backup.** MailArchive Desktop now goes to the new archive's row in **Your archives** and says there how its first backup is going, then whether it finished or failed (before, the page reloaded and never showed it). What the button says before the password step (a folder refused, Cancel, the hint while the wizard's windows are open) is now said beside the button. **Switch to this archive** on a locked encrypted archive goes to its **Unlock** box. Pressing Enter in the box where you type the recovery key back now uses it. The saved recovery file is now named "MailArchive recovery file — NAME (not the recovery key).txt".

### New in 1.5.6 — deleting an archive, and switching archives says what happened where you clicked
- **Update MailArchive on every computer that backs up into a shared folder or drive.** Once a delete starts removing an archive's files, it leaves a note in the archive's folder (`.mailarchive-deleting.json`) until it finishes, and MailArchive 1.5.6 backs up into no folder holding that note; an older MailArchive does not look for it, so it could go on backing up into a half-deleted archive.
- **Delete an archive for good.** Each row of **Your archives** (and the **This archive** card, when it is your only archive) has **Delete…**. It first shows a plan beside the row: what it would remove, the scheduled backups it would stop, and the files of yours in the folder that it keeps. **Save this plan** saves it as a text file, for example to have it approved. Type the folder's name exactly as shown (capital letters count) and click **Delete permanently**. The files are not moved to the Recycle Bin or the Trash. The result says what was removed: **Save deletion record** saves it, because MailArchive keeps no copy.
- **Remove from list** takes an archive off Your archives and keeps its folder and every file. **Open an archive folder…** brings it back.
- **At the command line:** `mailarchive delete-archive -out DIR` shows the same plan and asks for the folder's name (`-plan-out FILE` only saves the plan; `-record-out FILE` saves the deletion record); `mailarchive unlist-archive -out DIR` is Remove from list; `mailarchive add-archive DIR` lists an archive folder you already have, such as one made at the command line, so the other two can reach it. Each takes `-config FILE` for a MailArchive Desktop started with `-config`, and delete-archive and unlist-archive run only while MailArchive Desktop is shut down.
- **Delete not finished.** If a delete stops part way (the computer was switched off, or a file could not be removed), the row says **Delete not finished: click Delete again**, and `mailarchive status` says so too. Until then **Archive now** and **Run now** start nothing on that archive, and it cannot be paused, resumed, switched to or removed from the list. Click **Delete…** again (or run `mailarchive delete-archive -out DIR` again): the plan says it finishes the delete begun earlier, and the deletion record counts what every run of it removed.
- **After deleting the current archive**, no archive is chosen: click **Switch to this archive** on another row, or **Open an archive folder…**.
- **The deletion log.** Each delete adds a line to `deletions.log` in MailArchive's settings folder: `%APPDATA%\mailarchive\deletions.log` on Windows, `~/Library/Application Support/mailarchive/deletions.log` on a Mac, `~/.config/mailarchive/deletions.log` on Linux. A saved deletion record is checked against it. Uninstalling MailArchive removes it, so copy it first, somewhere you keep records: `mailarchive uninstall -yes` now refuses while it holds any line, until you run `mailarchive uninstall -yes -log-copied`. Uninstalling still never deletes your mail.
- **Switch to this archive no longer stops to ask about a backup it doesn't affect.** An archive made from mail files (for example Thunderbird) has a scheduled backup that always backs up its own folder, so switching to another archive leaves it running as it is, with no question. A Microsoft 365 backup, which follows the current archive, still asks first.
- **Messages appear beside the button you pressed.** Switch to this archive, Forget this encrypted archive here, Pause, Resume, Open an archive folder… and Use this archive now show their messages and questions right there, not in the Settings card further down the page, where they were easy to miss and the button seemed to do nothing.
- **Not yet tried on real hardware:** a delete interrupted by unplugging a USB drive from a Mac or a Linux computer, an exFAT drive pulled out mid-delete, a full drive, a network share, and OneDrive. On Windows, a virtual drive was unplugged in the middle of a delete and plugged back in, at the same letter and at another letter, and the delete was then finished.

### New in 1.5.5 — MailArchive's windows open in front on Windows
- **On Windows, the wizard and MailArchive Desktop's other windows open in front of your browser.** When you clicked **Make a new archive…** (or **Open an archive folder…**, or another button that opens a window) in MailArchive Desktop on Windows, the window could open behind the browser, so it looked as if nothing had happened. It now opens in front of the browser, ready for you to type, and does not stay on top once you switch to another window. A window you did not just ask for — for example the summary at the end of a long copy, if you went back to the browser meanwhile — does not take your typing: its button flashes in the taskbar until you click it. When MailArchive's windows close, your typing goes back to the browser.

### New in 1.5.4 — a Windows installer
- **Windows now has an installer.** Each release now includes **`MailArchiveDesktop-<version>-x64.msi`**: double-click it to install MailArchive with Start menu entries, a desktop shortcut, and MailArchive Desktop starting quietly whenever anyone signs in to the computer. It is not signed yet, so Windows shows **"Windows protected your PC"** (click **More info**, then **Run anyway**) and names an **Unknown publisher** when it asks for administrator permission. The zip is still there for computers where you cannot install programs. If you used the zip, the install section above says how to move your scheduled backups to the installed program.

### New in 1.5.3 — large archives open normally
- **An archive of more than about 2,200 messages works in Your archives again.** Since 1.1.0, MailArchive Desktop read only the first 1 MiB of an archive's record (the `.mailarchive-manifest.json` file in its folder), so a larger archive was shown greyed, as "Not reachable right now", with no **Switch to this archive** button, and **Open an archive folder…** refused its folder as one whose record "cannot be read". MailArchive Desktop now reads the whole record, a little at a time, so a large archive shows as a normal row: **Switch to this archive** and **Pause** are offered, and **Open an archive folder…** opens it. One email with an unusually long subject no longer makes an archive look broken either. Nothing in your archives changes, and nothing else changes in this version.

### New in 1.5.2 — Run now syncs Thunderbird on a Mac and on Windows
- **Run now syncs Thunderbird first, on a Mac and on Windows.**
  - On a Mac, Run now syncs Thunderbird first when the archive's Thunderbird sync is on, even while you are at the computer: it starts Thunderbird out of sight, waits for its mail (up to 20 minutes, often about 13; Stop waiting and back up now ends the wait), and leaves it open for you to quit. Scheduled backups still skip the sync while you are using the computer.
  - On Windows, Run now syncs Thunderbird first too, in the same way: it starts Thunderbird minimized, without taking the keyboard from what you are typing, waits for its mail, and leaves it open for you to quit from its icon on the taskbar, or among the small icons by the clock. A window where Thunderbird asks you something can still appear and take the keyboard: if Thunderbird has a Primary Password, its password window does so each time Run now starts Thunderbird, and if it is not your default mail app, it may ask whether to become it.
- **How to use MailArchive.** MailArchive Desktop's sidebar starts with a new **How to use MailArchive** item, on every page. It is a short guide: how to make your first archive, what each part of the page is for, what Thunderbird needs (and what **Sync Thunderbird before each backup** does on a Mac and on Windows), and how to connect a Microsoft 365 mailbox, step by step, from registering MailArchive in the Microsoft Entra admin center to the first **Archive now**. With no archive set up yet, the Overview points to it.
- **Thunderbird's own setting, said plainly.** Thunderbird keeps a new IMAP account's messages on this computer by default, but downloads them over time, and only while it is open; a message it has not downloaded yet is not in the archive yet. The guide and the wizard's question for an IMAP account now say this (the wizard no longer says Thunderbird keeps mail only "on demand").
- **Run now is easier to find on an archive made from mail files.** The Archive now card says Archive now is for Microsoft 365 archives and points at **Run now**; once the archive has a scheduled backup made in MailArchive Desktop or the wizard, the sidebar shows **Run now** instead of Archive now. A backup set up at the command line says why it has no Run now and how to get one, and what removing it does not keep.
- **Run now's note is short.** When Run now finishes it leads with one plain line, such as "Backup finished: 12 new messages archived.", then only what needs you. Messages already in the archive that were saved again are named as such, with "no copies are made", and the list of messages to look at is named as the file attachments-report.tsv in the archive's folder.
- **Back up only when you click Run now.** On a Mac and on Windows, **How often** in Settings now offers **Only when I click Run now** for an archive made from mail files. MailArchive Desktop saves the backup but sets up no schedule: nothing runs by itself, and **Run now** backs the archive up whenever you choose (with the Thunderbird sync first, if it is ticked). **Status** and the Scheduled backup card treat it as your choice, not as a missing schedule, and no backup alert is sent for it. Choose Hourly, Daily or Weekly later to put it back on a schedule; **Remove** removes it.
- **Remove says what it removed.** A scheduled backup set up at the command line under a name of its own is not one Remove can take out; MailArchive Desktop now says so, gives the command that removes it, and no longer shows "Removed." when nothing was.
- **Sleep and scheduled backups.** On a Mac and on Windows, the wizard and the guide now say that a backup time the computer slept through runs when it wakes up; only a computer that was switched off skips it.
- **The Microsoft 365 connection card** says under **App-only (admin)** that MailArchive Desktop always signs in as you; App-only is for the command line.
- **On Windows, the sync before a scheduled backup starts Thunderbird minimized.** It no longer shows Thunderbird's window first and then minimizes it.
- **Encrypted archives on a Mac and on Windows** can sync Thunderbird before Run now too: turn on **Sync Thunderbird before each Run now** in Settings. Until that backup starts reading mail, **Lock** cancels it.
- **Skipped syncs are no longer silent.** If Thunderbird could not be synced before an archive's scheduled backups for 3 days, **Status** says so and what to do, and backup alerts send one email about it (when **a failed backup, or Thunderbird not synced for 3 days** is ticked on the Backup alerts card).
- **A softer warning** shows in **Status** when Thunderbird was started before each scheduled backup for 3 days but no new mail arrived: it may be waiting for a password or sign-in. No email is sent for this one, and it clears by itself the next time a backup sees new mail arrive in Thunderbird, or a scheduled backup or Run now finds Thunderbird already open.
- **Hourly backups with the Thunderbird sync** still work. In an hour with no new mail, Thunderbird stays open for about 20 minutes while the backup waits for mail; Settings and `mailarchive schedule` say so when you pick Hourly with the sync on.
- **Run now can be cancelled.** While Run now is running, **Cancel the backup** stops it. The archive is left safe, and the next backup finishes what it left.
- **See how each sync went.** For an archive that syncs Thunderbird, **Recent backups** shows a short tag on each run (for example "Thunderbird synced first" or "Thunderbird not synced: you were using the computer"). **Status** adds a line "Thunderbird sync at the last backup", and `mailarchive status -json` adds `thunderbird_sync`.
- **Dated log lines.** Each Thunderbird sync line in `mailarchive.log` now starts with the date and time.
- **Clearer words.** With no archive set up, MailArchive Desktop no longer calls itself a Microsoft 365 archive, and its pages, help text, warnings and the command line's help now say what this version really does: for example, a scheduled backup skips the Thunderbird sync while you are using the computer (it does not wait), and the Security & privacy card names the Thunderbird sync as the one thing that can change your mail.
- **Two status tiles instead of four.** The Overview no longer shows the "Archive engine: Ready" and "Capture: Idle" tiles: they never changed, so they told you nothing. It shows **Microsoft 365** and **Scheduled backup**.
- **Make a new archive…** MailArchive Desktop's first card, when no archive is set up yet, now has a **Make a new archive…** button: it opens the MailArchive wizard's windows to make an archive from Thunderbird, Outlook or other mail files on this computer. When the wizard is done, the page offers the new archive at the top: click **Use this archive**. The wizard's windows may open behind your browser; if they are not in view, move or minimize the browser window. If you stop the wizard while it copies your mail, the page says so, keeps the mail copied so far in the list, and says how to finish the archive (it no longer says "Archive added"). On the **Add another archive** card, which shows once an archive is in use, the button that did the same, **Add another archive…**, is now called **Make a new archive…** too.
- **The program calls itself MailArchive.** The wizard's question and message windows are now titled **MailArchive wizard** (they said "Mail Archive Export"; its file and folder pickers are titled with what they ask), a scheduled backup's notices are titled **MailArchive backup**, and every message calls the program MailArchive. On a Mac the app keeps its name, Mail Archive, in the Finder and the Dock (**Mail Archive.app**), so nothing you have set up changes.

### New in 1.5.1 — one place for backup settings
- **Settings is where you change a backup.** How often, the time, and **Sync Thunderbird before each backup** are all in the Desktop's **Settings** card. The **Scheduled backup** card shows what is set up, with **Run now**, **Repair** and **Remove**.
- **Each archive gets its own schedule.** An archive made from Thunderbird mail gets its own backup that re-reads its mail files, even on a computer where Microsoft 365 is set up.
- **Hourly backups** for archives made from local mail (Thunderbird and other mail files). Microsoft 365 backups stay Daily or Weekly.
- **Every button shows it was pressed.** A button turns grey with a spinner and a "Working…" label until its job is done, and the card then says what happened ("Repaired.", "Removed.").
- **Fewer emails when a backup keeps failing:** one alert per failing streak and one when it recovers. An archive whose mail source can't be reached is flagged after **3 days** and its backups are paused after **30 days**, counted in days, not runs.
- **Fixed:** the Desktop could count its own settings file as a backup job, which could refuse a Microsoft 365 backup or let Remove delete that settings file. Remove now deletes only real backup-job files.
- **Fixed:** a download of the program copies kept in an encrypted archive that stalled could be counted as damaged and started over, instead of resuming where it stopped.

### New in 1.5 — encrypted archives
- **Make an encrypted archive.** Everything in it — messages, attachments, the search index, the folder list and what it was made from — is encrypted on the drive and opens only with its **password** or its **recovery key**. Make one in MailArchive Desktop (**Add another archive → Make a new encrypted archive…**) or at a terminal: `mailarchive -encrypt -out NEWFOLDER -input MAIL`. You choose a password; MailArchive shows a **recovery key once** and asks you to type it back. Keep both safe: if you lose the password and the recovery key, nobody can open the archive — there is no reset.
- **Use it like any archive.** Unlock it on the Desktop's **Encryption** card (or let this computer remember its key), then back it up with **Run now**, search it, read it and go back in time. **Lock** closes it again. Backups into an encrypted archive run while you are there to unlock it; scheduled backups for encrypted archives are not available yet.
- **Change the password, make a new recovery key, put lost key files back** (`change-password`, `new-recovery-key`, `restore-key-file`), and **check it** with the key: `mailarchive verify -full`.
- **The way out.** **Make an unencrypted copy…** (or `mailarchive decrypt`) writes a readable copy into a new folder, showing its plan first and asking twice when the folder may leave this computer (a synced folder, a removable drive).
- **Opening it years from now.** By default the archive keeps copies of MailArchive for Mac, Windows and Linux (about 134 MB each time a new version updates them) in its `program` folder, checked against this release's `SHA256SUMS`, with a `README.txt` saying how to open it. You can switch this off on the Encryption card or with `-program-copies off`. Published releases of MailArchive can no longer be changed, so a copy that matches `SHA256SUMS` is the file that was published.
- **Security update.** The HTML library MailArchive uses to clean and index email (`golang.org/x/net`) moves to v0.62.0 for two published vulnerabilities (GO-2026-6866, GO-2026-6860); its text library moves with it. Mail already archived reads back exactly as before.
- **Limits of this first version.** An existing archive cannot be encrypted in place: make a new encrypted archive beside it and back up into it. And a restored older copy of an encrypted archive is not noticed: MailArchive carries on from that copy, and mail backed up after it is collected again only if it is still in the mailbox (a plain archive does not notice this either).

### New in 1.1–1.3
- **Archive manager.** MailArchive Desktop now manages more than one archive: a **Your archives** card lists every archive this computer knows about, marks one **current**, and switches between them. **Pause (make read-only)** an archive so nothing can change it, and **Resume** it later — nothing is deleted either way.
- **Built-in email alerts.** Have a scheduled backup email you when it **fails** (or is auto-paused, or — if you want — succeeds). Built-in SMTP, no script to install: **Office 365**, **Gmail**, or a **standard SMTP** server; the password is kept in your OS credential vault, and **Send test email** confirms it before you rely on it. Works for both Microsoft 365 and local scheduled backups.
- **Backup run history.** A **Recent backups** card (and an archive-local history record) shows the last several runs — when, result, how many messages — not just the latest.
- **Run now / Shut down / Add another archive.** Run the backup immediately instead of waiting for the schedule; shut the dashboard down cleanly to free its port (do this before an upgrade); and set up an additional archive with the wizard without disturbing the current one.
- **Source-loss surfacing.** When an archive's **source** (its mailbox or mail folder) stops being reachable across scheduled runs, the dashboard shows a banner and `status` reports a WARN (`source_lost`), so a stalled backup is never silent.
- **Uninstall keeps your archives.** **Uninstall MailArchive…** in MailArchive Desktop removes MailArchive's program and schedules from this computer, shows a plan first, and **never deletes an archive**.
- **Upgrading to a newer version.** Stop the program (**Shut down**), replace the files, relaunch — your archives are untouched; if the program's path changed, the app's **Repair** re-points the schedule. See "Upgrading to a newer version" in the README.

### Fixed in 0.7.2
- **The wizard's Repair no longer breaks a weekly backup set up in MailArchive Desktop.** When an archive's schedule had been created by MailArchive Desktop (a weekly Microsoft 365 capture), the launch wizard's health check could offer *Repair* and, on clicking it, convert that backup into a job-file schedule that failed every night while `status` stayed green. The wizard now recognises a MailArchive Desktop schedule, does not offer to repair it, and points you to MailArchive Desktop instead (R14, R18).
- **A scheduled capture that can't start is no longer silently green.** A scheduled Microsoft 365 backup that refused at launch because it was not configured or signed in wrote no run record, so `status` and the health view stayed GREEN until the run was twice its period late. Such a refusal now records a failed run, so status reads RED with the reason straight away (R18).
- **An unplugged drive no longer looks like a brand-new archive.** When an archive's folder was unreachable (a removable or network drive not connected, or a deleted folder), health read it as a new, empty archive and suggested installing a *second* schedule. It now says the location is unreachable and that nothing is wrong with the archive itself, and drops the misleading "no schedule" advice (R18).
### Opening the reader and the dashboard now needs a private link
`serve`, `setup` and MailArchive Desktop are opened only through a **private one-time link** the
program makes each time it starts, so other accounts on the same computer and web pages cannot
open your archive or dashboard (a program running as you still can).
- **`mailarchive serve`** opens your browser itself at its link (not with `-no-browser`, or without
  a display); on a terminal it also prints a second link, and Enter prints another. With no
  terminal (nohup, launchd, systemd, a detached tmux) run **`mailarchive serve -link -out <folder>`**
  from any terminal of your account for a fresh link; an `ssh -L` tunnel keeps working (open the
  link through the forwarded port). `mailarchive setup` now picks a free port.
- **Scripts that called `serve`'s HTTP API with `curl` stop working** (they have no session): use
  `mailarchive search` (same query grammar; `-json` for machine output).
- **`serve -addr 0.0.0.0`** keeps its warning and now says the archive and the session cookie
  cross the network in clear text; each browser still needs its own link.
- **MailArchive Desktop (Windows):** the dashboard and the reader each take a free port, so a
  bookmark of `http://127.0.0.1:8097/` or `:8099/` no longer reaches them — open it from its
  shortcut; the Start Menu has **Copy MailArchive link** for a default browser that cannot be
  opened. Each Windows account (and each sign-in session) has its own instance. An upgrade ends
  running instances so the fix applies in every session at once.
- On Linux and macOS another account that can list running programs could read a link in the
  instant a browser is opened with it; macOS now hands the link to the browser without putting it
  in the program list. A link works once, and "already used" when you did not use it means: stop
  and start the program again.

### Fixed in 0.7.1
- **Windows: rebuilding the search index no longer fails while the archive is open.** A leaked database handle kept `search.db` open, so `reindex -rebuild` (and the dashboard's *Rebuild index*) could fail with "Access is denied" on Windows while a viewer held the index. The handle is now released; rebuild and reindex work with the archive open.
- **Windows: a lost or overwritten archive lock is now detected mid-run.** The lock-loss check relied on POSIX file identity, which Windows cannot provide, so in rare cases two runs on the same archive could overlap on Windows. The lock now carries a content marker re-checked during the run, so a run whose lock is taken over stops instead of racing (R5) — on every OS.
- **Clearer schedule preview on macOS and Windows.** `schedule` now shows the full command on one readable line (as on Linux) plus the same operator notes, not only the raw launchd/schtasks definition.

### What's new
- **MailArchive Desktop (Windows) — a local control panel for Microsoft 365.** A new single
  app opens a private dashboard on `127.0.0.1` (this computer only) that signs you in to M365,
  archives your mailbox, browses it (with go-back-in-time), schedules a weekly backup, and
  checks the archive's health — all **in-process** (no separate engine to install), with a
  plain-language Security & privacy panel. Double-click it and it opens the page in your
  browser; the weekly backup keeps running with the app closed. Ships with an MSI/RMM kit for
  IT. The dashboard and its reader bind loopback only and refuse any network address; the
  sign-in is read-only (`Mail.Read`) and stays in the OS vault. Cross-platform core (Linux/
  macOS run the same dashboard); Windows integration (Credential Manager, shortcuts, MSI) is
  Windows-first and validated by hand, not in CI.
- **Guided Microsoft 365 setup (`mailarchive setup`).** A local wizard walks an admin through
  the Entra app registration — tenant ID, application (client) ID, and (app-only) the client
  secret — and clarifies the two-Object-ID trap: you need the **Application (client) ID**, not
  an Object ID. The secret is stored in your OS credential vault, never on the command line.
- **Your Microsoft sign-in lives in the OS vault.** On Windows the delegated sign-in (and any
  app secret) are kept in **Windows Credential Manager**, not a plaintext file; elsewhere, a
  file only you can read. Only the refresh token is stored (the short-lived access token is
  regenerated as needed), and nothing is uploaded. Sign out any time (*Clear sign-in*);
  uninstalling keeps your archive.
- **Microsoft 365 without admin — sign in as yourself (`-auth device`):** you no longer need a tenant-wide app permission to archive an M365 mailbox. `mailarchive graph -auth device -out ./archive -tenant T -client-id C` prints a code and a URL; you sign in once in a browser and approve, and the tool archives **your own** mailbox with a read-only **delegated** `Mail.Read` grant — no admin, no RBAC, no client secret. The sign-in is saved (a refresh token, `chmod 600`, under your OS config dir) so scheduled runs need no re-sign-in; run the schedule as the same account you signed in as. The original **`-auth app`** (app-only, admin-consented, many mailboxes from one job) is unchanged and still the default. Read-only throughout, either way. See [`docs/graph-app-setup.md`](../docs/graph-app-setup.md).
- **Reused Message-IDs, told apart (Microsoft 365 / Graph):** some senders stamp two genuinely *different* messages with the *same* `Message-ID`. Earlier versions kept one copy per Message-ID, so a distinct reuse could be skipped. A live Graph archive now uses each message's Microsoft **immutable id** plus a body-inclusive **content hash** recorded at capture: a reuse whose id it has not archived is downloaded and, when its content differs, kept as its own message — even when the two share an identical envelope (a `$100` and a `$250` invoice that reuse one id both survive). A copy filed into several folders, or a message whose id is reissued by a mailbox restore/migration, is recognised as the same content and kept as **one** record (no duplicate, no re-download). The manifest format bumps to **v6**; this closure applies to messages captured **fresh** under v6 — an existing archive keeps the Message-ID behaviour for what it already holds (no re-download to upgrade), and sources without a per-message id (IMAP / local imports) keep it too. See [`docs/goback.md`](../docs/goback.md).
- **Go back in time:** for a live Microsoft 365 (Graph) archive, `mailarchive serve` now shows the mailbox as it was on any past date, Wayback-style — a scheduled daily capture records an append-only timeline, so a message deleted online stays visible at earlier dates (under its then-folder) while the current view follows moves and hides it. Deleting the files and running `reindex` redacts a message across all dates. Deleted Items and Junk Email are excluded from a Graph capture by default (`-include-deleted` / `-include-junk` opt in). A live archive keeps one copy per message and records folder moves in place (no re-download, no duplicate); the manifest format bumps to v5 (v6 adds the reused-Message-ID closure above), and the first run over a pre-existing live archive **collapses** any per-folder duplicates into one record (recognised by Message-ID, not re-downloaded — see Known limitations). The date view is served-only — the offline pages still show each message's first-captured folder. See [`docs/goback.md`](../docs/goback.md).
- **Keep it current:** `mailarchive schedule` takes any backup job (export, Graph, reindex, verify), validates it when you schedule it, works with any Windows path, logs to a rotating file, and the GUI offers *Keep this archive current?* after a run. One schedule per archive.
- **Know where you stand:** `mailarchive status -out DIR` — completeness, last run, schedule, GREEN/WARN/RED with remedies; the GUI shows the same on launch.
- **Incremental fills gaps:** mail archived before its content was downloaded is re-examined by every incremental run and filled once it is there; `-mode full` is no longer the remedy.
- **Verify the files:** `mailarchive verify -out DIR` re-hashes every archived file against the checksum recorded when it was written (exit 0 attested, 2 not attested), and `verify -record` baselines an older archive; `status` shows a *Fixity coverage* and *Last verify* line, and a scheduled verify that finds modified or missing files goes RED.
- **Scriptable:** `mailarchive search -json`/`-paths`/`-0` and `mailarchive status -json` (versioned, with `reason_codes`) make both surfaces machine-readable.
- **Safe offline:** every exported page carries its own policy — nothing runs or loads from the network when opened from disk; `serve` is hardened too.
- **Legible archive:** README.txt inside the archive, navigation and attachment links on every message page, full headers with original time offsets, paginated folder pages, optional `-raw` originals.

### Known limitations
- Reads **classic** Outlook `.pst`/`.ost`; *New Outlook* and *Outlook for Mac* (`.olm`) are not supported locally — use the Microsoft 365 (Graph) path.
- IMAP mail exports only if downloaded locally; do the one-time prep (`-enable-offline -sync-wait` for Thunderbird, "Mail to keep offline → All" for Outlook, *Synchronize remote mail locally* for Evolution). The archive records what is still missing and later runs fill it; `attachments-report.tsv` and `status` show the count.
- Windows scheduled runs happen only while you are logged in; a console window appears briefly for CLI-scheduled jobs.
- Go-back (the point-in-time view) is a `serve` feature and needs a live Graph capture that records the timeline; a one-shot local import (`.pst`/mbox/maildir) has none. Its resolution is your run cadence — capture daily for a daily timeline.
- Upgrading a **pre-existing** live (Graph) archive **consolidates** it on the first run: per-folder move-duplicates collapse into one mailbox-wide record and the search index is re-keyed, so each still-present message is recognised by its Internet-Message-ID and **not** re-downloaded (one copy per message, no re-fetch). A collapsed duplicate's file is never deleted. The reused-Message-ID closure (above) applies to messages captured **fresh** under format v6: an existing archive keeps the Internet-Message-ID behaviour for its already-captured messages (a distinct reuse of one of those is skipped, as before — the tool never re-downloads a whole mailbox to upgrade), and IMAP / local imports (no per-message id) keep it too — in all cases **no file is ever deleted** (`verify` flags any orphan). A message copied into several folders is kept as one record but its shown "current folder" and the date-view timeline may flap between those folders each run (cosmetic; the message is never lost, duplicated, or re-fetched).
- The Outlook-app export path (`-outlook`) and the real Windows/macOS scheduler installs are validated by hand, not in CI (catalog rows MA-60, MA-79, MA-95). MailArchive Desktop's Windows-specific integration — the Credential Manager sign-in/secret, Desktop/Startup shortcuts, and the MSI build/install/upgrade — is likewise validated on Windows by hand (the dashboard, in-process capture, and reader are CI-tested cross-platform on Linux). Reading a mailbox's own `sensitivity` from Microsoft 365 is best-effort: a tenant that doesn't expose the underlying property is archived without it, not failed.

Licensed under **AGPL-3.0**. Provided "as is", without warranty — use at your own risk.
