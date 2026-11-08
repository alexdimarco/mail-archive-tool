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
