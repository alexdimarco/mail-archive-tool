# Friction review (light): the words slice C0c added

Walked 8 November 2026 (Mac clock; the Windows VM clock read 12:07 AM, 2026-11-08). Process:
`../assurance-kit/process/friction-review.md` and `METHODOLOGY.md`, light form: the new words
only, as the gate owed by commit ec9ee2b ("the wizard's new words owe a light friction look").
Also read: `docs/ux-contract.md` (X-series), the repo `CLAUDE.md`, completeness design §6 (the
labels by version, fold L5) and catalog rows MA-1195, MA-1204, MA-1205.

**Verdict: SHIPPABLE_WITH_NOTES.** What the code does is right in every walked cell: nothing in
Thunderbird is changed on a refusal, the command line exits 1, the wizard goes on with an export
of what is already here, and the label follows the Thunderbird version. The words are not yet
right for a person who is not technical. Four findings are medium (F1 to F4) and should be fixed
before the public release. They are all word changes (Type III, buildable-away), and per the kit
Type III items are built before ship. None needs a design change. The replacement words are in
§3. They are proposals for the lead and the owner to accept; this review does not self-certify them.

## Header

| | |
|---|---|
| Reviewed commit | ec9ee2b, branch `tb-completeness` (worktree pulled, already up to date) |
| Program | `mailarchive` built natively on macOS from ec9ee2b (`go build ./cmd/mailarchive`), into the main checkout's untracked `.claude/fric-c0c/` |
| Profiles | Made-up profiles only, written by a script under `.claude/fric-c0c/` (never a real profile): a `prefs.js`, an optional `compatibility.ini` and `user.js`, and an `ImapMail/<host>/` folder holding one INBOX and its `.msf` |
| How the words were got | The real command line (`mailarchive -out … -enable-offline -input <store>`) on each profile; its stderr is the error text exactly. The wizard shows the same error text, then `"." + "\n\nThis export goes ahead with the mail already on this computer."` (`imapPrepAccount`, `internal/wizard/wizard.go`). The wizard dialog was not driven |
| Windows | **Not rendered.** Two tries to show the text in the same Win32 message box zenity uses (a PowerShell stand-in, title "MailArchive wizard", warning icon) did not put a box on screen where `prlctl capture` could see it. The VM was left as found (the stand-in process was stopped). It was not cheap, so the Windows points below are judged from the strings and from how a Win32 message box behaves |
| Cells walked | 12: all 12 work as built; 4 medium-friction findings, 7 low |
| Exit codes seen | Every refusal: exit 1 (X1). The two successful runs: exit 0 |

## 1. The rendered words

Store folder `imap.gmail.com`, two live accounts on that server with no folder setting of their
own (`server1`, `server2`). The command line prints:

> mailarchive: MailArchive could not tell which Thunderbird account the folder imap.gmail.com
> belongs to, so it changed nothing; in Thunderbird, open that account's settings and turn on
> "Keep messages in all folders for this account on this computer" yourself. Why: more than one
> account in prefs.js (server1, server2) could own the store folder imap.gmail.com, so
> MailArchive does not pick one

The wizard shows the same words without "mailarchive: ", ending with a full stop, then a blank
line and "This export goes ahead with the mail already on this computer."

The label, by version (checked against design §6 fold L5):

| `compatibility.ini` `LastVersion` | Label shown | Right? |
|---|---|---|
| 153.6.0 | "Keep messages in all folders for this account on this computer" | Yes |
| 160.0 | "Save copies of all folders and messages on this computer for offline use" | Yes |
| none | the first label, then "(in Thunderbird 160 and later: " the second ")" | Yes |

The other reasons ("Why: …"), as printed:

| Case | Why: … |
|---|---|
| An account removed from Thunderbird, its folder left behind | no account in prefs.js matches the store folder mail.oldjob.com |
| No account list | prefs.js lists no account (mail.accountmanager.accounts), so no account owns the store folder imap.gmail.com |
| user.js is a link | /Users/…/user.js is not a regular file (a link) |
| prefs.js cannot be read | open /Users/…/prefs.js: permission denied |

The user.js refusal (MA-1205), on the command line:

> mailarchive: /Users/…/userjs/user.js sets this account's offline setting
> (mail.server.server1.offline_download), and Thunderbird applies user.js over prefs.js every
> time it starts, so MailArchive changed nothing. To keep this account's messages on this
> computer, close Thunderbird, then change that line in user.js to true or remove it

The log lines, one run over two accounts on imap.gmail.com, then a second run:

> Enabled offline download for imap.gmail.com (prefs backed up to prefs.js.mailarchive-bak).
> Enabled offline download for imap.gmail.com (the backup prefs.js.mailarchive-bak was kept as it was, from before MailArchive first changed prefs.js).
> Offline download already enabled for imap.gmail.com.

A refusal run left no `-out` folder behind. So "nothing was exported" is true when the command
line refuses.

## 2. Cells and findings

| Cell (actor × scenario) | Works? | Friction findings | Type | Fix |
|---|---|---|---|---|
| 1. Owner on Windows, wizard, Yes, prepare, two accounts on one server, Thunderbird 153 | Yes | F2, F3, F4, F5, F6 | III | W1 |
| 2. Same, Thunderbird 160 | Yes | as cell 1; the label is right | III | W1 (160 form) |
| 3. Same, version not readable | Yes | as cell 1; the label quotes both versions, which is long but right | III | W1 (unknown form) |
| 4. Owner, wizard, the folder of an account they removed from Thunderbird | Yes | F7: the remedy cannot be done (there is no such account) | III | W2 |
| 5. Owner, wizard, a profile with no account list (rare) | Yes | F2 (`mail.accountmanager.accounts` shown) | III | W1's plain reason |
| 6. Owner, wizard, user.js keeps it off | Yes | F9 | III | W4 |
| 7. Owner, wizard, user.js is a link (rare, or planted) | Yes | F8: the headline blames the account match; the cause is a file it could not read | III | W3 |
| 8. Owner, wizard, prefs.js cannot be read | Yes | F8, and a raw system error | III | W3 |
| 9. Command-line user, `-enable-offline`, no account it can tell, follows the remedy, runs the same command again | Yes, but loops | **F1: a dead end.** The run refuses again every time, because the check refuses before it looks at the setting (`AccountToEnable`, `internal/thunderbird/thunderbird.go`; the check over every store, `cmd/mailarchive/main.go`). Also, nothing says the export did not run | III | W5 |
| 10. Command-line user, user.js keeps it off | Yes | F1 again (no "run again without -enable-offline"); F9 | III | W6 |
| 11. Command-line user, two accounts on one server, both turned on in one run | Yes | F10: two lines name the same server; you cannot tell which account each is | III | W7 |
| 12. Owner, wizard, the profile is not found | Yes | F11: no reason and no next step | III | W8 |

### Medium (fix before the public release)

- **F1. The command line's remedy is a dead end.** After you tick the setting in Thunderbird and
  run the same command again, it refuses again with the same words. The words do not say that
  the export did not run, and do not say to run again without `-enable-offline`. X2 asks that a
  refusal name what to do; this one names a step that does not get you past the refusal.
- **F2. "Why: …" uses words a Windows owner cannot act on.** prefs.js, `server1, server2`,
  "store folder", `mail.accountmanager.accounts`, and raw system errors with full paths. The
  command line can keep them. The wizard should give a plain reason, with the technical one at
  most as a last "Details:" line.
- **F3. "That account" is unclear, and "folder" means something else in Thunderbird.** In
  Thunderbird a folder is Inbox, Sent and the like, so "the folder imap.gmail.com" reads as a mail
  folder the person has never seen. The usual cause of this refusal is two accounts on the same
  server, so the person cannot tell which one "that account" is. Tell them to do it for each
  account on that server.
- **F4. It does not say where the setting is.** "Open that account's settings" leaves a
  non-technical person to search. Name the path: Account Settings, then the account's
  "Synchronization & Storage" page (below 160; design §6's source re-read), or the "Offline
  message storage" group (160 and later). The path still needs lab row MA-1191 on real installs.
- **F6. The wizard does not say mail may be missing.** "This export goes ahead with the mail
  already on this computer" is true, but it does not say that mail Thunderbird has not downloaded
  yet is left out, or that a later run adds it. The question just before this one already says
  this ("a later run adds it once Thunderbird has it"). The refusal should say it again, because
  the person chose Yes to avoid exactly that.

### Low

- **F5.** "Turn on … yourself": a new IMAP account has this setting on already (the Windows lab
  of 2026-10-31). "Make sure … is ticked" is truer and less alarming.
- **F7.** An account removed from Thunderbird is likely the most common cause in real use, since
  Thunderbird writes a folder setting for every account it has. This is a judgement; it was not
  measured. For that case the remedy is impossible. Say so (W2).
- **F8.** When prefs.js or user.js cannot be read, the headline says MailArchive could not tell the
  account. The real cause is a file it could not read. Give it its own words (W3).
- **F9. The user.js words.** They begin with a long path. They say "this account's" without
  naming the server. They use "applies user.js over prefs.js". They do not say "if this was set on
  purpose, leave it", which matters because user.js is usually put there on purpose, by the person
  or their IT. On Windows the path runs through the hidden AppData folder, so give the path last,
  on its own line.
- **F10.** The kept-backup line is true, but it names only the server, so two accounts on
  imap.gmail.com print two lines that look alike. It does not say where the backup is (the
  Thunderbird profile folder).
- **F11.** "Couldn't find the Thunderbird profile, so nothing was changed." gives no likely cause.
- **F12 (Linux only, not seen on screen, an older pattern).** On Linux, closing the warning with
  the window's close button may return "cancelled". The wizard treats "cancelled" as a quiet exit
  (`wizard.go`, the first `ErrCanceled` case), so "This export goes ahead" would then be false. On
  Windows a message box with only an OK button returns OK for Esc and the close button (Win32
  behaviour), so Windows goes ahead; this was not seen live. A small fix: treat cancel from this
  one warning as OK.
- **F13 (cosmetic).** The command line stutters: "mailarchive: MailArchive could not tell…".

### Against the UX contract

- X1: holds. Every refusal exits 1, and a successful run exits 0.
- X2: partly. The words say what failed. The command line's remedy does not get you past the
  refusal (F1).
- X7: holds. No crash text appears.
- X8: holds. Refusals go to stderr.
- X11 covers the dashboard only, so it does not apply here. But its spirit, plain words with no
  jargon, is what F2 and F3 ask of the wizard.
- No contradiction otherwise.

## 3. Proposed words

`ACCOUNT HOST` is the store folder's name (for example imap.gmail.com). `LABEL` is the label
chosen by version, as built. Every wizard text ends with the shared tail **W0**:

> This export goes ahead now with the mail already on this computer. Mail that Thunderbird has
> not downloaded yet is not in it; a later run adds it once Thunderbird has it.

**W1, the wizard, no account it can tell (two or more accounts on one server).** Below 160:

> MailArchive could not tell which of your Thunderbird accounts on imap.gmail.com this mail
> belongs to, so it did not change any Thunderbird setting.
>
> To make sure Thunderbird keeps all of this mail on this computer:
> 1. In Thunderbird, open Account Settings.
> 2. For each account on imap.gmail.com, open its "Synchronization & Storage" page.
> 3. Make sure "Keep messages in all folders for this account on this computer" is ticked.
>
> Why: more than one account in Thunderbird uses imap.gmail.com, and MailArchive does not guess.
>
> W0

At 160 and later, steps 2 and 3 become: "2. For each account on imap.gmail.com, find "Offline
message storage". 3. Make sure "Save copies of all folders and messages on this computer for
offline use" is ticked." With no version, use the steps for below 160 and add to step 3: "(in
Thunderbird 160 and later it is called "Save copies of all folders and messages on this
computer for offline use")". With no account list, the Why line becomes: "Why: Thunderbird's
settings list no accounts."

**W2, the wizard, no account uses the folder (often a removed account).**

> MailArchive could not find a Thunderbird account for the mail in mail.oldjob.com, so it did not
> change any Thunderbird setting.
>
> Why: no account in Thunderbird uses this mail now. This is usual when an account was removed from
> Thunderbird and its mail was left on this computer; then there is nothing to turn on.
>
> If the account is still in Thunderbird: open Account Settings, open that account's
> "Synchronization & Storage" page, and make sure LABEL is ticked.
>
> W0

**W3, the wizard, a settings file it could not read.**

> MailArchive could not read Thunderbird's settings, so it did not change any of them.
>
> Details: open C:\Users\…\prefs.js: permission denied
>
> W0

**W4, the wizard, user.js keeps it off.**

> Thunderbird is set, in its file user.js, not to keep the mail of imap.gmail.com on this
> computer, and it applies that file every time it starts. So MailArchive changed nothing: a
> change would be undone.
>
> If you or your IT department set this on purpose, leave it. Otherwise: close Thunderbird, open
> user.js in a plain text editor (Notepad on Windows), and delete the line that contains
> mail.server.server1.offline_download (or change its false to true).
>
> The file: C:\Users\…\AppData\Roaming\Thunderbird\Profiles\….default-release\user.js
>
> W0

**W5, the command line, no account it can tell.**

> mailarchive: -enable-offline changed nothing, and nothing was exported: MailArchive could not
> tell which Thunderbird account the mail in imap.gmail.com belongs to (more than one account in
> prefs.js could own it: server1, server2; it does not guess). In Thunderbird, open Account
> Settings and, for each account on imap.gmail.com, make sure LABEL is ticked (on its
> "Synchronization & Storage" page). Then run the same command again without -enable-offline.

For a removed account: "(no account in prefs.js uses it now; if the account was removed from
Thunderbird there is nothing to turn on)". The rest is the same.

**W6, the command line, user.js keeps it off.**

> mailarchive: -enable-offline changed nothing, and nothing was exported: PATH\user.js sets
> mail.server.server1.offline_download (imap.gmail.com) to off, and Thunderbird applies user.js
> over prefs.js at every start, so a change would be undone. If that is on purpose, run again
> without -enable-offline. Otherwise close Thunderbird, delete that line from user.js (or set it
> to true), and run again.

**W7, the log lines.**

> Enabled offline download for imap.gmail.com (server1); prefs.js backed up to
> prefs.js.mailarchive-bak in the Thunderbird profile folder.
>
> Enabled offline download for imap.gmail.com (server2). prefs.js was not backed up again:
> prefs.js.mailarchive-bak, in the Thunderbird profile folder, still holds prefs.js from before
> MailArchive first changed it.

**W8, the wizard, no profile found.**

> MailArchive could not find the Thunderbird profile this mail belongs to (the mail may have been
> copied out of Thunderbird), so it did not change any Thunderbird setting.
>
> W0

### What the words need from the code (for the lead; nothing changed here)

- **The two kinds of reason.** The wizard needs a plain reason and the command line keeps the
  technical one. So `NoAccountError` would carry a kind (several accounts, none, no list, could
  not read), and the wizard and the command line would each render their own words. Today both
  share `Error()`.
- **Tests pinned to the old words.** These tests pin the current strings: MA-1204 and MA-1205
  (`assure.Names`), and MA-1195's `TestTheKeptBackupIsReportedTruly`. They would change with the
  words, test-first, under their rows.
- **Lab row MA-1191.** "Account Settings", "Synchronization & Storage" and "Offline message
  storage" come from the source re-read only. Lab row MA-1191 confirms them on 153.6.0esr (the
  Windows VM) and 159.0 (the Mac) before the words ship, as design §6 already requires.

## 4. Not walked

- The wizard dialog on screen, on any platform. Its words were taken from the code path above.
- A real Thunderbird profile, on purpose: only made-up profiles were used.
- Mac and Linux dialogs. F12 is reasoned from the zenity source, not seen.

## Re-look of the words as built (2026-11-08)

Walked 8 November 2026, Mac clock 01:25 EST. A light re-look (same process doc, light form) of
the words slice C0c ships, at tip 32c2e53 of `tb-completeness` (worktree pulled, already up to
date). Read: §1 to §4 above, `docs/ux-contract.md`, `internal/thunderbird/offline_words.go` (every
sentence), `imapPrepAccount` / `prepFailWords` / `imapNotPrepared` in `internal/wizard/wizard.go`,
`checkOfflineStore` in `cmd/mailarchive/main.go`, and catalog row MA-1260. Windows judged first.

**Verdict: SHIPPABLE_WITH_NOTES.** W0 to W8 were applied faithfully, mostly word for word, and
in three places better than proposed (below). The wizard words carry no prefs.js, no server key,
no raw error and no full path except user.js's, which is meant. F1 (the dead end) is fixed: every
command-line refusal now ends with "Then run the same command again without -enable-offline."
Two medium findings remain, both word changes (Type III): **R1**, the steps do not say how to
open Account Settings on Windows, where Thunderbird's menu bar is hidden; **R2**, on the command
line the lead's leftover-folder sentence says "you can ignore this message" right after
"nothing was exported". One more medium sits outside this slice (**R8**, the Desktop guide names
a label that does not match these words). The rest are low. The replacement words are below, for
the lead and the owner to accept; this re-look does not self-certify them.

### How the words were got

- A small program under the worktree's gitignored `.claude/worktrees/fric-relook/`, built from
  32c2e53. It made 10 made-up profiles there (never a real one). For the wizard it made the same
  calls `imapPrepAccount` makes (`FindProfileDir`, `AccountToEnable`, `CheckOffline`,
  `PlainWords`) and added `imapNotPrepared` copied word for word. For the command line it ran the
  real `mailarchive -out … -enable-offline -input <store>` built from 32c2e53 and kept its output
  and exit code.
- The tests that pin these words, run verbosely, all pass: `TestYesPrepareRefusalsArePlain`
  (MA-1260), `TestYesPrepareSaysWhyAndDoesNotCarryOn` (MA-1204, MA-1205),
  `TestTheIMAPQuestionSaysWhatThunderbirdDoes` (MA-947), `TestClosingThePrepWarningGoesOn`
  (MA-1209), `TestEveryPrepRefusalGivesTheWayOut`, `TestNoAccountRefusesWithTheReason`,
  `TestTheKeptBackupIsReportedTruly`, `TestUserJSRefusalOnTheCommandLine`,
  `TestEnableOfflineSaysWhenItWasOnByDefault`, `TestUserJSControlsTheOfflineSetting`.
- **Windows: not rendered on screen** (the same limit as the first walk). Judged from the strings
  and from how a Win32 message box behaves: zenity's Windows warning is `win.MessageBox`
  (`msg_windows.go`), which shows `&` as written, so "Synchronization & Storage" is safe, and
  Ctrl+C copies the box's text. Not seen live.

Every refusal exited 1 and left no `-out` folder. The one healthy profile exited 0. With no
profile found, the command line does not refuse (R10).

### The words as built (wizard; each ends with W0, shown once at the end)

Two accounts on one server, Thunderbird 153 (816 characters with W0):

> MailArchive could not tell which of your Thunderbird accounts on imap.gmail.com this mail
> belongs to, so it did not change any Thunderbird setting.
>
> To make sure Thunderbird keeps all of this mail on this computer:
> 1. In Thunderbird, open Account Settings.
> 2. For each account on imap.gmail.com, open its "Synchronization & Storage" page.
> 3. Make sure "Keep messages in all folders for this account on this computer" is ticked.
>
> Why: more than one account in Thunderbird uses imap.gmail.com, and MailArchive does not guess.
> If you removed an account from Thunderbird, its leftover mail folder can cause this; if so, you
> can ignore this message.
>
> This export goes ahead now with the mail already on this computer. Mail that Thunderbird has
> not downloaded yet is not in it; a later run adds it once Thunderbird has it.

At 160 steps 2 and 3 read: `2. For each account on imap.gmail.com, find "Offline message
storage".` and `3. Make sure "Save copies of all folders and messages on this computer for
offline use" is ticked.` With no version (998 characters, the longest), step 2 adds "(in
Thunderbird 160 and later, find "Offline message storage")" and step 3 adds "(in Thunderbird 160
and later it is called "Save copies of all folders and messages on this computer for offline
use")".

The others, first paragraph and what follows:

| Case | Wizard words (before W0) |
|---|---|
| A removed account's folder | "MailArchive could not find a Thunderbird account for the mail in mail.oldjob.com, so it did not change any Thunderbird setting." / "Why: no account in Thunderbird uses this mail now." + the leftover sentence / "If the account is still in Thunderbird:" + steps 1 to 3 for "that account" |
| No account list | the two-accounts headline / steps for "the account on imap.gmail.com" / "Why: Thunderbird's settings list no accounts." |
| A settings file it could not read (user.js a link; prefs.js too big) | "MailArchive could not read Thunderbird's settings, so it did not change any of them." / the steps |
| A hand-edited line (160) | "Thunderbird's settings file has been edited by hand where this account's setting is, so MailArchive could not be sure what it says and did not change any Thunderbird setting." / the 160 steps |
| user.js keeps it off | "Thunderbird is set, in its file user.js, not to keep the mail of imap.gmail.com on this computer, and it applies that file every time it starts. So MailArchive changed nothing: a change would be undone." / "If you or your IT set this on purpose, leave it. Otherwise: close Thunderbird, open user.js in a plain text editor (Notepad on Windows), and delete the line that contains mail.server.server1.offline_download (or change its false to true)." / "The file: <path>" |
| No profile | W8, word for word |

The command line, two accounts on one server, 153:

> mailarchive: -enable-offline changed nothing, and nothing was exported: MailArchive could not
> tell which Thunderbird account the mail in imap.gmail.com belongs to (more than one account in
> prefs.js (server1, server2) could own the store folder imap.gmail.com, so MailArchive does not
> pick one). If you removed an account from Thunderbird, its leftover mail folder can cause this;
> if so, you can ignore this message. In Thunderbird, open Account Settings and, for each account
> on imap.gmail.com, make sure "Keep messages in all folders for this account on this computer"
> is ticked (on its "Synchronization & Storage" page). Then run the same command again without
> -enable-offline.

Every other command-line refusal has the same shape: "-enable-offline changed nothing, and
nothing was exported:", the reason with its technical detail in brackets, where to tick, then the
same last sentence. The user.js one ends "…and run again. The file: <path>". The log line on a
healthy profile: "Enabled offline download for imap.gmail.com (folder imap.gmail.com, server1);
prefs.js backed up to prefs.js.mailarchive-bak in the Thunderbird profile folder."

### Were W0 to W8 applied?

| Proposed | As built | Faithful? |
|---|---|---|
| W0 | Word for word, on every wizard refusal, including the fallback | Yes |
| W1 | Word for word for 153, 160 and no version. With no version, step 2 also gives the 160 place, which W1 left out | Yes, better |
| W2 | Steps instead of one line. The lead's leftover sentence replaces "This is usual when…" | Yes; see R2, R3 |
| W3 | The "Details:" line with the raw error is dropped, and the steps are given | Yes, better: the person gets something to do, and no error text they cannot act on |
| W4 | Word for word, except "your IT department" became "your IT" | Yes; see R6 |
| W5 | Word for word, plus the leftover sentence | Yes; see R2 |
| W6 | Word for word, with "or change its false to true" | Yes |
| W7 | As proposed, and the account is named by folder and server too | Yes, better |
| W8 | Word for word | Yes |
| F12, F13 | Fixed: a closed warning goes on (MA-1209); no "mailarchive: MailArchive" stutter | Yes |

### Findings

**Medium (build before the public release; words only):**

- **R1. Step 1 does not say how to open Account Settings, and on Windows that is the first place
  a person gets stuck.** Thunderbird on Windows hides its menu bar by default, so there is no
  Tools menu to see. The Windows lab of 7 November (`docs/lab/restore-thunderbird-windows-MA-1133.md`, its table
  row "≡ → Account Settings") saw Account Settings in the menu button (≡) of Thunderbird 153.6.0
  on Windows. The shipped Restore words already say "click the menu button (≡, top right)", so
  this also makes the two features say it the same way. The same button is on the Mac, so one
  sentence serves both. Replace step 1 (in `Place.steps`):

  > 1. In Thunderbird, click the menu button (≡, top right), then Account Settings.

  On the command line (`Place.remedy`), optional, for the same reason:

  > open Account Settings (the menu button ≡, top right) and, for …

- **R2. On the command line, "you can ignore this message" is wrong.** That sentence comes right
  after "nothing was exported". A person who ignores the message has no export at all. What they
  need is the last sentence (run again without -enable-offline), and nothing to tick. Replace the
  command-line form of the leftover sentence (two-accounts and removed-account cases) with:

  > If you once removed an account on HOST from Thunderbird, this may be its leftover mail
  > folder; if so, there is nothing to turn on: just run the same command again without
  > -enable-offline.

  HOST is the folder's name (imap.gmail.com, mail.oldjob.com). This needs the sentence split from
  the wizard's (today one constant, `leftoverWords`, serves both).

**Medium, outside this slice (before the public release):**

- **R8. The Desktop guide names a different label.** "How to use MailArchive" (`internal/desktop/help.go`,
  around line 215, pinned by MA-945) says "Keep messages for this account on this computer". The
  source read of ESR 153 (`docs/lab/thunderbird-completeness-source-esr153.md`, row 12) found that
  label in no version read: below 160 it is "Keep messages in all folders for this account on
  this computer", the label these words use. A person who reads the guide and then the wizard sees
  two names for one box. The guide should take the same label and the same 160 form, test-first
  under MA-945. Not changed here.

**Low (the lead's call):**

- **R3. The leftover sentence in the wizard.** Where it is placed does not confuse the two-accounts
  case. The code (`matchWith`, `thunderbird.go`) gives "two accounts" whenever two live accounts
  share the server and neither has this folder as its own, which includes a removed account's
  leftover folder, so the sentence belongs there. It sits after the steps, in Why, and the steps
  do no harm if it was a leftover. But "ignore this message" is broader than meant, since W0
  still holds, and the person cannot tell which account was removed. Wording that names the
  server and reads right before or after the steps:

  > If you once removed an account on HOST from Thunderbird, this may be its leftover mail; if so,
  > there is nothing to turn on.

- **R4. 160 gives no page.** "find "Offline message storage"" does not say where. The source read
  did not read 160's page name (`thunderbird-completeness-source-esr153.md`, "Whether 160 still
  names the panel… not read"), and lab row MA-1191's machines are 153.6.0 and 159.0, so no lab
  check yet sees the 160 screen. Words that claim nothing unread:

  > 2. For each account on HOST, look through the pages listed under its name on the left until
  > you see "Offline message storage".

  For no version: "(in Thunderbird 160 and later, look through its pages for "Offline message
  storage")".

- **R5. "Account on imap.gmail.com".** Account Settings lists accounts by their names, usually
  the email address, not by server. A Gmail user will guess. With another provider, a person may
  not. If the lead wants it, add to step 2: "(an account's "Server Settings" page shows its
  server name)". About 60 characters more.

- **R6. user.js on Windows.** "your IT" is clipped: say "If you, or whoever looks after this
  computer, set this on purpose, leave it." For Windows, "open user.js in a plain text editor
  (Notepad on Windows)" leaves two traps. First, Notepad's Open box shows only .txt files unless
  you type the name. Second, double-clicking a .js file on Windows runs it with Windows Script
  Host (a harmless error box), and does not open it for editing. This is from Windows'
  defaults, not seen in the lab. Words that stay right on every platform:

  > Otherwise: close Thunderbird and open the file below in a plain text editor (on Windows:
  > start Notepad, choose File, then Open, and type or paste the whole path into "File name";
  > double-clicking the file does not open it for editing). Delete the line that contains KEY (or
  > change its false to true), and save.

- **R7. "Edited by hand"** reads as blame, and an add-on or an IT tool may have made the change.
  Say: "Thunderbird's settings file was changed outside Thunderbird where this account's setting
  is, so MailArchive could not be sure what it says and did not change any Thunderbird setting."

- **R9 (cosmetic, command line).** The two-accounts reason has brackets inside brackets and says
  twice that MailArchive does not choose: "(more than one account in prefs.js (server1, server2)
  could own the store folder …, so MailArchive does not pick one)". It is readable, and the
  technical part may stay here. No change needed.

- **R10 (behaviour, not words; from before this slice).** With no profile found, the command line
  prints "Could not find the Thunderbird profile for <path>; skipping prep." and exports with exit
  0. Every other -enable-offline refusal stops with exit 1. The wizard goes ahead in both cases,
  so the two surfaces agree on what happens. Noted for the lead; not judged here.

### Length, on a phone and in the dialog

The wizard texts are 540 to 1,000 characters, about 90 to 170 words. A Win32 message box wraps
them into one tall box with no scrolling, which fits a laptop screen. The no-version text is the
densest, because steps 2 and 3 each carry a bracket. It appears only when compatibility.ini
cannot be read, which is rare, since Thunderbird writes that file every time it starts. R1 adds
about 40 characters and R4 about 50. Nothing needs cutting.

### Against the UX contract

- X1 holds: every refusal exits 1, and the healthy run exits 0.
- X2 now holds for these refusals: each says what failed and a remedy that gets you past it
  (F1 fixed). R2 is the one sentence that points the other way.
- X7 holds: no crash text. X8 holds: refusals go to stderr. X17 holds: the program is
  "MailArchive" throughout.
- X11 covers the dashboard only. Its spirit, plain words, is met by the wizard texts. R8 is the
  dashboard side of the same label.
- No contradiction otherwise.

### What the changes need (for the lead; nothing changed here)

- R1 to R3 and R6 change words that tests pin: `TestYesPrepareRefusalsArePlain` (its
  `leftover` constant and step 1), `TestEveryPrepRefusalGivesTheWayOut`, `TestNoAccountRefusesWithTheReason`
  and `TestYesPrepareSaysWhyAndDoesNotCarryOn`. Catalog row MA-1260 quotes the leftover sentence
  word for word. Each would change test-first under its row, with prove-fail on the old words.
- R2 needs `leftoverWords` split into a wizard form and a command-line form, each taking HOST.
- The ≡ path (R1) is lab-seen on 153.6.0 on Windows only. Lab row MA-1191 can confirm it on the
  Mac's 159.0 along with the labels.

## Final look after round 3 (2026-11-08)

Walked 8 November 2026, Mac clock 02:32 EST. The last light look (same process doc, light form)
at the words round 3 added or changed, at tip 1adcf64 of `tb-completeness` (worktree pulled,
already up to date). Read: the two sections above, `docs/ux-contract.md`, and
`git show 1adcf64 -- internal/thunderbird/offline_words.go internal/desktop/help.go
cmd/mailarchive/main.go`. Windows judged first.

**Verdict: SHIPPABLE_WITH_NOTES.** Every medium from the re-look is fixed as proposed: R1 (the ≡
step, on both surfaces), R2 (the command line's leftover sentence now gives the way on) and R8
(the Desktop guide names both labels, by version, pinned so it cannot drift from the wizard). The
low notes R3 to R7 went in with the proposed words. The new words for MA-1261 (strict reading) and
MA-1263 (a stop after a change) are true and readable. No new finding is medium. Six low notes
follow (Q1 to Q6), with exact words, for the lead's call; none must be done before the release.
R10 is judged below: not required before the release; a word fix is recommended. These are
proposals; this look does not self-certify them.

### How the words were got

- A small program under the worktree's gitignored `.claude/worktrees/fric-final/`, built from
  1adcf64, made 17 made-up profiles there (never a real one). For the wizard it makes the same
  calls `imapPrepAccount` makes (`FindProfileDir`, `AccountToEnable`, `CheckOffline`,
  `PlainWords`) and adds `imapNotPrepared` copied word for word. For the command line it runs the
  real `mailarchive -out … -enable-offline -input <store>` built from 1adcf64 and keeps the output
  and exit code.
- Strict reading (MA-1261), each in prefs.js on Thunderbird 153: a NUL byte, a missing `;`, a
  `pref(` line, a `sticky_pref(` line, a setting name with a backslash, a quoted value going on
  past its line. Also a `pref(` line at 160, and a `pref(` line in user.js turning the setting on.
- MA-1263 cannot be staged from outside package `main`. The program's fault seam (`tbRunning`) is
  package-private, and the real check looks at every store before any change. So its words were
  put together from `stoppedAfterChange.Error()`, copied word for word, plus two real reasons
  from the real program: a profile whose `lock` link reads as Thunderbird running, and a profile
  folder made read-only (the real failed-write error). `TestALaterStopSaysWhatWasChanged` proves
  the wiring end to end. It passes.
- These tests pass, run verbosely: `TestALaterStopSaysWhatWasChanged`,
  `TestAPrefCallInUserJSIsNeverAlreadyOn`, `TestYesPrepareRefusalsArePlain` (9 cases),
  `TestEveryPrepRefusalGivesTheWayOut`, `TestUserJSRefusalOnTheCommandLine`,
  `TestTheGuideNamesTheOfflineBoxAsTheWizardDoes` and
  `TestHowToUseMailArchiveSaysWhatThisVersionDoes`. This was a filtered run, for the words only.
  It is not the gate run.
- **Windows: not rendered on screen** (the same limit as both earlier walks). The Desktop guide's
  ≡ is shown in Edge, and the Restore lab has seen it there. The wizard's ≡ (U+2261) sits in a
  Win32 message box (`MessageBoxW`, Segoe UI), where it should draw as written. That was reasoned,
  not seen.

Every refusal exited 1 and left no `-out` folder. The healthy profile exited 0. One mistake of
my own, kept here: the first missing-`;` profile was not edited as meant, so it was healthy and
exported. Made correctly, it refuses with exit 1 and leaves no `-out` folder. The re-run used a
fresh `-out` path.

### The new words as rendered

**The ≡ step (R1)**, in every wizard refusal with steps:

> 1. In Thunderbird, click the menu button (≡, top right), then Account Settings.

The command line: "open Account Settings (the menu button ≡, top right) and, for …".

**The split leftover sentence (R2, R3).** Wizard, two accounts on imap.gmail.com, in Why:

> If you once removed an account on imap.gmail.com from Thunderbird, this may be its leftover
> mail folder; if so, there is nothing to turn on.

Command line, the same case:

> … If you once removed an account on imap.gmail.com from Thunderbird, this may be its leftover
> mail folder; if so, there is nothing to turn on: just run the same command again without
> -enable-offline. In Thunderbird, open Account Settings (the menu button ≡, top right) and, for
> each account on imap.gmail.com, make sure "Keep messages in all folders for this account on
> this computer" is ticked (on its "Synchronization & Storage" page). Then run the same command
> again without -enable-offline.

**MA-1261, the wizard** (every strict-reading case, prefs.js or user.js; 701 characters with W0
at 153):

> Thunderbird's settings file was changed outside Thunderbird, so MailArchive could not be sure
> what it says and did not change any Thunderbird setting.
>
> To make sure Thunderbird keeps all of this mail on this computer:
> 1. In Thunderbird, click the menu button (≡, top right), then Account Settings.
> 2. For the account on imap.gmail.com (an account's "Server Settings" page shows its server
> name), open its "Synchronization & Storage" page.
> 3. Make sure "Keep messages in all folders for this account on this computer" is ticked.
>
> W0

**MA-1261, the command line**, the shape of each:

> mailarchive: -enable-offline changed nothing, and nothing was exported: prefs.js line 7 holds
> WHAT. Thunderbird never writes such a line, so the file was changed outside Thunderbird (by
> hand or by another program), and MailArchive cannot be sure what Thunderbird applies for
> mail.server.server1.offline_download. In Thunderbird, open Account Settings (the menu button ≡,
> top right) and, for the account on imap.gmail.com, make sure LABEL is ticked (on its
> "Synchronization & Storage" page). Then run the same command again without -enable-offline.

WHAT, as rendered: "a NUL (zero) byte, where Thunderbird stops reading the file"; "text that is
not a whole user_pref("name", value); statement" (the missing `;`, reported on its own line 4);
"a pref( statement, which Thunderbird does not apply in prefs.js or user.js" (also user.js line
1); "a sticky_pref( statement, …"; "a setting name holding a backslash (an escape code)"; "a quoted
value that goes on past the end of its line".

**MA-1263** (the command line only), Thunderbird started for the second profile:

> mailarchive: -enable-offline stopped, and nothing was exported. Before it stopped, it changed
> prefs.js to turn on offline download for imap.gmail.com (folder imap.gmail.com, server1), in
> the Thunderbird profile …/partial-a; that change stays (the log above says where prefs.js was
> backed up). Why it stopped: Thunderbird looks like it is running for the profile …/partial-b.
> Close it, then run the same command again (prefs.js must not be edited while Thunderbird is
> open).

With a failed write, "Why it stopped:" is followed by the raw error: "back up prefs.js: temp file
in …/partial-c: open …/partial-c/.mailarchive-prefs-3245455535.tmp: permission denied".

**user.js (R6)**, the wizard: "If you, or whoever looks after this computer, set this on purpose,
leave it. Otherwise: close Thunderbird and open the file below in a plain text editor (on Windows:
start Notepad, choose File, then Open, and type or paste the whole path into "File name";
double-clicking the file does not open it for editing). Delete the line that contains
mail.server.server1.offline_download (or change its false to true), and save." The file's path
follows on its own line, then W0 (943 characters in all, with this walk's long test path).

**The Desktop guide (R8)**, `help.go`, the third requirement:

> … For an IMAP account this is the setting **Keep messages in all folders for this account on
> this computer** (in Thunderbird 160 and later it is called **Save copies of all folders and
> messages on this computer for offline use**). Thunderbird turns it on for a new account, so
> usually there is nothing to change. To check it, in Thunderbird click the menu button (≡, top
> right), then **Account Settings**, choose your account's **Synchronization & Storage** page (in
> Thunderbird 160 and later, look through its pages for **Offline message storage**), and see that
> the box is ticked. …

### The questions asked

- **Would a non-technical Windows person know what to do?** Yes, in the wizard. Each refusal says
  what MailArchive did (nothing), where to click (≡, Account Settings, the page, the box), and
  that the export goes ahead anyway. Doing nothing is safe in every case, so no wizard text is a
  dead end. The hardest is the user.js case. It asks for Notepad and a long path. But "leave it"
  comes first, so stopping there is safe too.
- **Jargon.** The wizard texts hold no "statement", "NUL", "user_pref", "store", "prefs.js" or
  raw error. The words "user.js" and its setting name are kept on purpose: the person has to find
  that line. The command line keeps all of these words, as agreed in the first walk (F2). Each one
  comes with a gloss: "NUL (zero) byte, where Thunderbird stops reading", "an escape code".
- **MA-1263's "that change stays".** It is clear and true, and it is not alarming to a
  command-line user. Two small gaps remain (Q3). "That change" stays singular when several
  profiles were changed. And it does not say that the change is the one the person asked for, or
  that running again skips it ("Offline download already enabled", the code path for an account
  already on).
- **The guide's two names.** Readable. The guide cannot know the version, so naming both is
  right. The bracket that gives the 160 name comes straight after the first name, so a reader on
  either version finds theirs. It is a long list item, as it was before.

### Low notes (the lead's call; none required before the release)

- **Q1. Step 2's server hint interrupts the step.** "For each account on imap.gmail.com (an
  account's "Server Settings" page shows its server name), open its …" puts the bracket between
  "For each account" and what to do, and "its" then reads as the Server Settings page. Words:

  > 2. For each account on imap.gmail.com, open its "Synchronization & Storage" page. (To see
  > which server an account uses, look at its "Server Settings" page.)

  At 160: "2. For each account on HOST, look through the pages listed under its name on the left
  until you see "Offline message storage". (To see …)". The test that pins the hint's place is
  `TestYesPrepareRefusalsArePlain`.

- **Q2. Pasting the user.js path on Windows.** You cannot select text in a Win32 message box.
  Ctrl+C copies the whole message, not the path (Win32 behaviour, not seen live). So "type or
  paste the whole path" mostly means typing a long AppData path. Words:

  > … and type the whole path below into "File name" (or press Ctrl+C on this message, paste it
  > into Notepad, and copy the path from there); …

- **Q3. MA-1263's middle sentence.** Words, one change:

  > -enable-offline stopped, and nothing was exported. Before it stopped, it turned on offline
  > download for ACCOUNT, in the Thunderbird profile P, by changing prefs.js. That change is kept:
  > it is what -enable-offline is for, and running the same command again leaves it as it is (the
  > log above says where the old prefs.js was backed up). Why it stopped: REASON

  With more than one: "Those changes are kept: they are what …, and running the same command
  again leaves them as they are".

- **Q4. A failed write gives a raw error and no next step.** This was the case before this
  slice, and it is now also the "Why it stopped:" of MA-1263. It is the command line only, and
  rare. Words, for a write or backup error of EnableOffline:

  > -enable-offline changed nothing, and nothing was exported: MailArchive could not write
  > prefs.js or its backup in the Thunderbird profile P (RAW ERROR). Check that the folder is not
  > read-only and that the disk has room, then run the same command again.

  On the first store this is true as written. As MA-1263's reason, the prefix comes off as it
  does today.

- **Q5 (cosmetic, command line).** "holds a setting name holding a backslash" stutters. Make it
  "has a setting name with a backslash (an escape code) in it". A missing `;` reads as "text that
  is not a whole user_pref("name", value); statement", and the person cannot see what is wrong
  with that line. Add: "(often a missing ; at its end)".

- **Q6 (cosmetic, command line).** In the two leftover cases, "run the same command again without
  -enable-offline" appears twice in one message. It is true both times. No change needed.

### R10, judged

**What it is.** On the command line, `-enable-offline` with a store whose Thunderbird profile
cannot be found (mail copied out of Thunderbird, or a folder not inside a profile) logs "Could not
find the Thunderbird profile for <path>; skipping prep." Then the export runs, with exit 0. Every
other `-enable-offline` case where nothing can be turned on refuses: exit 1, nothing exported. That
includes the leftover-folder case, whose words now say "there is nothing to turn on: just run the
same command again without -enable-offline". The wizard goes ahead in both cases (W8), so the two
surfaces differ only on the command line's exit code and words.

**Must it be done before the release? No.** It predates this slice. It harms nothing: the export
is the same one the wizard makes, of the mail that is there. The line goes to the log and says
prep was skipped. `-enable-offline` cannot be scheduled (`job.go` refuses it unattended), so no
unattended job hides it. But two things are worth doing:

- **Words, recommended before the release (Type III).** "skipping prep" does not say that
  `-enable-offline` did nothing, or why. Words:

  > -enable-offline did nothing for PATH: MailArchive could not find the Thunderbird profile this
  > mail belongs to (the mail may have been copied out of Thunderbird), so it changed no
  > Thunderbird setting. The export goes ahead with the mail as it is.

- **Exit code, a ruling for the lead or owner, not a word change.** Should it refuse (exit 1) like
  the leftover-folder case? Or should the leftover case go ahead like this one? Either keeps
  one shape. My lean is to refuse. That matches X2's one refusal voice, and the scheduler already
  keeps unattended jobs from depending on it. But this changes behaviour, so it is not
  self-granted here.

### Against the UX contract

- X1 holds: every refusal walked exits 1, and the healthy run exits 0. R10 is the one case that
  exits 0, and it is not a refusal today.
- X2 holds for every refusal walked. Each says what failed, and each gives a remedy that gets you
  past it. Q4 is the one case with no remedy.
- X7 holds: no crash text. X8 was not re-checked separately this round; the earlier walks saw
  refusals on stderr. X17 holds: "MailArchive" throughout.
- X11 (the dashboard): the guide's words are plain, and both labels match the wizard's.
- No contradiction.

### Still owed (not words; already gated elsewhere)

- Lab row MA-1191. The labels and the ≡ path on the Mac's 159.0. The 160 screen ("Offline
  message storage", and "the pages listed under its name on the left"), which no lab has seen
  yet. The wizard's ≡ in a Windows message box.
- The adversarial pass's re-run on round 3 (the commit names it as the next gate).
