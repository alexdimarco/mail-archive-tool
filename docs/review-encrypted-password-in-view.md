# Independent review: the password step in view, and the recovery key told from the recovery file

Review of branch `encrypted-password-in-view` at bc84ca5 (three commits on 97f3698, which is 1.5.6):
0d15689, e1b5ad3, bc84ca5. Catalog rows MA-1070 (a step is brought into view) and MA-1071 (the
recovery key told from the recovery file). Reviewed 2026-11-04 by an independent reviewer (a model),
for the owner. No product code was changed by this review.

**Verdict: both owner reports are fixed on Windows in Edge, with the owner's real Thunderbird mail.
Nothing HIGH. Three MED findings and six LOW ones, below. None needs a product ruling to fix, except
where an owner question is named.**

## What was checked, and how

1. **Code first** (before the builder's report): the full diff 97f3698..bc84ca5, the page script
   (`showStep` in dashboard.js, every caller in encryption.js), the server's new checks
   (`keyring.LooksLikeRecoveryFile`, the restore and type-back routes), the new words, and every
   changed or new test.
2. **Tests, re-run on the Mac:** `internal/desktop`, `internal/keyring` and `assureblock` all pass
   (`.claude/marev-mac-tests.txt` in the main checkout). The three Node page-script scenarios ran
   (they did not skip). One mutation of my own: removing the `showStep` call for the password step
   turned both `TestTheNewArchivesPasswordStepComesIntoView` and
   `TestTheServedPageBringsEachStepInThroughOneHelper` red; reverted.
3. **Windows walk** in the Parallels "Windows 11" VM, Edge (InPrivate), keyboard only, with the
   program built from bc84ca5 the way `packaging/build-release.sh` builds `mailarchivew.exe`. The
   review's own settings folders and archives lived only in `C:\Users\Public\ma-review-realmail`
   (removed afterwards). The source was the VM user's real Thunderbird mail (owner's permission,
   3 Nov 2026).

## Findings

### MED-1. A refusal after the wizard's windows, and the hint while they are open, are still out of sight

**Scenario (reproduced on Windows, Edge, bc84ca5):** click **Make a new encrypted archive…**, answer
the seven windows, and choose a folder that is not empty (a very common first choice, such as
Documents). The wizard closes. The only words the page says, "Could not make an encrypted archive: an
encrypted archive is made in a new or empty folder: … choose a new or empty folder", are 2,553 pixels
down a 706-pixel window. The keyboard is back on the button. Nothing in view changed. To the person,
the page has again "seemingly stopped", which is the owner's first report with a different cause.

The same place, far below, also holds:
- the hint shown while the windows are open ("Answer the questions in the windows that open on your
  computer. They may be behind this browser window…"), which is the one line meant for a person whose
  window went behind the browser;
- the answers to choosing the Outlook app, "an add is already in progress", and "Nothing was made."
  after Cancel.

**Why it matters:** the fix covers each step that needs typing. It does not cover a step that failed,
which a person has to read before trying again.

**Smallest fix:** say these lines where the person is: next to the button that started the flow, or
bring the note into view with the same helper, keeping the keyboard on the button.

### MED-2. Switch to this archive on a locked encrypted archive leaves its Unlock box out of sight

**Scenario (reproduced twice on Windows):** in **Your archives**, click **Switch to this archive**
on a locked encrypted archive. The page is drawn again with the keyboard on the page itself. The
Unlock box is 1,200 to 2,070 pixels below the window, 32 to 34 Tab presses away. The sweep fixed the
same thing for **Open an archive folder…**. Switch is the more common way to reach a locked archive.

**Smallest fix:** set the same one-time mark (`mailarchive-step-unlock`) when a switch succeeds. It
is already guarded and tested. The storage-count test would then name a fourth call.

### MED-3. The first backup's result is not shown after the page reloads

**Scenario (Windows, real mail):** after the recovery key is typed back, the page says "Its first
backup is running now — it shows here as it goes." Eight seconds later the page reloads. The new
archive is not the current one, so its activity is not shown anywhere on the page.
- On the second archive, the backup of the real mailbox (617 messages) finished, and the page never said so.
- On the first archive, the first backup failed (a lab cause, see "Lab notes"). The page never said
  that either. The archive's card said only "Finish setting up… has no backup yet".

**Why it matters:** "it shows here as it goes" is a promise the page breaks. A person may believe
their mail is backed up when it is not. This was not built in this branch. It is the step the branch
brings into view, so it belongs to this review. It is the owner's question 5.

**Smallest fix:** after "archive made", land on **Your archives** with the new archive's row saying
its first backup is running, then finished or failed. Or offer to switch to it at once.

### LOW-1. A recovery key typed with a label is told it is the recovery file

**Scenario (reproduced):** at the type-back box, the person pastes `Recovery key: ABCD-…`, the way a
note or password manager entry often holds it. The page answers "That is the recovery file's
contents, not the recovery key." That is wrong, and confusing for someone who never opened the file.
The cause is that `LooksLikeRecoveryFile` treats any colon as the file's.

**Fix:** drop `:` from the characters that mark the file. Every line of the file still has a quote
or a brace, so detection does not get weaker. Add a labelled key, and a key in quotes, to the
negative twins of `TestARecoveryFilesContentsAreToldFromARecoveryKey`.

### LOW-2. The Unlock box does not recognise the recovery file

**Scenario (reproduced):** the Unlock box asks for "Password or recovery key". The person pastes the
recovery file there. The page answers "That does not open this archive… put them back with
mailarchive restore-key-file -out …". It does not say that this is the recovery file. This is the
third place a recovery key is asked for, and MA-1071 covers only two of them.

**Fix:** use the same check in the unlock route, before the slow key work, with the same words.

### LOW-3. The last message of the flow uses different words, and a command, about the recovery file

After the key takes effect, the page shows the command line's own sentence (`keyring.RecoveryFileNote`):
"The recovery file opens nothing on its own: … (`mailarchive restore-key-file`). Keep it with your
backups, and the recovery key apart from it." Three steps earlier, the same person read "MailArchive
opens it… you never type or paste it."
- The backticked command means nothing to someone using the Desktop.
- "Keep it with your backups" is the owner's question 1, said differently in two places.

**Fix:** on the Desktop, use the Desktop's own `recoveryFileSavedNote` (or a shorter form of it) for that line.

### LOW-4. The keyboard lands on "Save the recovery file" first

When the recovery key appears, the keyboard is put on **Save the recovery file (for lost key
files)…**. A keyboard user who presses Enter saves the file, which is the very confusion in the
owner's second report. The new words reduce the risk: the window's title says "it is not the recovery
key". But the first action offered is still the file.

**Fix:** put the keyboard on the key itself (make it focusable), or on the step, so the status line
and the key are read first.

### LOW-5. Screen readers: maybe silent, maybe said twice (not checked with a screen reader)

Each step's status line is hidden until the moment it is filled. It is shown and filled in the same
instant that the keyboard moves into the field. Some screen-reader and browser pairs do not announce
a status line that appears already filled. In that case only the field's label is heard, so
"Next, choose a password…" may never be spoken.

At the type-back step the label and the status line say almost the same long sentence, so a reader
that does announce both says it twice.

**Fix:** keep the status line present and empty, then fill it. Shorten the type-back label to "Recovery key".

**Not verified:** no screen reader was run, by the builder or by this review.

### LOW-6. Smaller friction seen on the walk

These were not built in this branch:
- The recovery file's folder window opens inside the new archive's folder. That is the one place
  where a recovery file is refused. The refusal is clear and in view.
- After a refused paste, the pasted text stays in the box, so the person has to clear it first.
- Pressing Enter in the type-back box does nothing (owner's question 4).
- After the key takes effect, the keyboard falls to the page itself, because the focused button is hidden.

## Checked and sound

- **Bug 1 on Windows, with real mail:** after the folder is picked, the password box is in view
  (116 px from the top) with the keyboard in it, Edge is in front, and the status line says what to do next.
  Passwords that do not match: the answer is in view, and the keyboard stays on Continue.
  Continue: the key box is in view. I have saved the recovery key: the type-back box is in view with
  the keyboard in it. Archive made: the line is in view.
  This held twice: once as the first archive, once as "Add another".
- **Keyboard not stolen:** a refusal moves nothing and keeps the keyboard where it was. A cancelled
  wizard moves nothing (the Node scenario, and the code). The load-time second move only runs on the
  page drawn after **Open an archive folder…**.
- **Reduced motion:** the helper checks `prefers-reduced-motion: reduce` and scrolls at once. A page
  just drawn always scrolls at once. The "archive made" scroll uses the default, which is at once.
- **No secret exposed by the change:**
  - `showStep` reads no field.
  - The new checks quote nothing back.
  - The one-time mark in browser storage holds only `1`.
  - The new words are fixed text, written as text.
  - On the real saved file: it holds neither the recovery key (with or without dashes) nor the
    password, its fields are format, kind, archive_id and the wrapped entry, and on Windows it is
    readable by its owner only.
- **The recovery file pasted where the key is asked:**
  - at the type-back box it gets the plain explanation, and the key still waits;
  - in Put back the key file's box it gets the explanation before any window opens;
  - typed right, the key takes effect.
- **Words a first-time person meets:** the key box, the folder window's title ("…MailArchive opens
  this file itself; it is not the recovery key"), the "Saved the recovery file" line and the restore
  form all tell the two apart.
- **Tests not weakened:**
  - No marker was removed. No test was skipped or narrowed.
  - The storage count rose from 15 to 18, and each new call is named and guarded. That is a new
    guarded call, not a lowered gate.
  - Three ids drawn only for some archives were added to the list of optional page ids. Each lookup
    of them is null-checked.
  - The Node scenarios skip on Windows, where Node is not installed.
- **Wizard scan time (1.5.5 friction note B):** with the real profiles, the mailbox scan took under a
  second. Every wizard window was in front when it appeared and still in front 1.5 seconds later. No
  window only flashed.

## Lab notes (not findings)

- **Settings folder permissions.** The review's settings folder was under `C:\Users\Public`, whose
  inherited permissions let any signed-in user change files there. MailArchive's owner-only read
  therefore refused its own run job. The Desktop then said the archive "was made at the command
  line" and offered no Run now. Making the lab's settings folders owner-only, as a real `%APPDATA%`
  is, restored Run now. A real user's settings are owner-only, so this is a lab cause. The wording
  ("made at the command line") is wrong for that cause, should it ever happen.
- **The first archive's sources.** Its sources were reached through a link in the lab's settings
  folder. The reader would not follow the link ("cannot find the path"), which is a lab cause too.
  The second archive used the real folder directly and backed up 617 messages of the real mailbox
  (the default-release profile's IMAP account, about 106 MB on disk).
- **The program-copy warning.** "The copy for Windows (x86-64) was not written" appeared because the
  lab copied only `mailarchivew.exe`.

## Changes this review made to the VM, and their clean-up

- **A Thunderbird setting changed, then put back.** The first attempt's wizard-answering script
  failed to recognise the Thunderbird offline-download question. It pressed **Yes, prepare** for
  both IMAP stores.
  - For the default-esr profile, MailArchive turned on offline download in that profile's prefs.js
    at 00:11:23. It kept its own backup of the file first, as `prefs.js.mailarchive-bak`.
  - Thunderbird was not running. I put prefs.js back byte for byte from that backup at once, after
    checking that the two differed only in that one line.
  - That backup file had to be overwritten for this. It is now the same as prefs.js. Any older copy
    under that name, if there was one, is gone.
  - The default-release profile already had the setting (from 2 Nov), so nothing changed there.
  - The script was then fixed never to press that button, and later runs pressed **No, export
    what's cached**. Thunderbird was never started or quit. No mail was sent, moved or deleted.
- **A browser sync prompt.** Opening the saved recovery file in a new tab of the review's Edge
  opened a normal (not InPrivate) window. That window signed itself into the VM user's Microsoft
  account and showed "We are now syncing". I closed it within a minute. The review's Edge profile
  folder was deleted at clean-up.
- **Removed:** the review's MailArchive Desktop (stopped), its Edge and profile, and the whole
  `C:\Users\Public\ma-review-realmail` folder. The link to the real Thunderbird folder was removed as
  a link first. The real profiles still held 343 files before and after.
- **Not touched:** no scheduled task was created (checked), the installed MailArchive Desktop and its
  data, and the VM user's Notepad.

## Owner questions (the builder's five), with this review's recommendation

1. **Keep the file together with, or apart from, the key:** keep "apart" everywhere, and change the
   last message's "Keep it with your backups" (LOW-3) so all surfaces agree.
2. **A label inside the file:** no. It changes the format, and older versions would refuse the file.
   The words around the file now do this job.
3. **A clearer file name:** yes. "MailArchive recovery file — NAME (not the recovery key).txt" is
   cheap and is seen at the moment of confusion.
4. **Enter in the type-back box:** yes, Enter should use the key.
5. **Where to land after "archive made":** on Your archives, at the new archive's row, showing its
   first backup (MED-3).

---
Date worked: 2026-11-04, from the session clock and the VM clock (00:10 to 00:40, UTC−5). Register: a
review finding. Drawn on: the code at bc84ca5, my own test runs, and my own walk on the Windows VM;
all reached. Not checked: a Mac or Linux browser walk; a screen reader.
