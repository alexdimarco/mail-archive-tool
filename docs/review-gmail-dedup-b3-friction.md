# Friction review: Gmail de-dup slice B3 (folder views and the Gmail question)

**Verdict: HIGH_FRICTION_NOT_SHIPPABLE for the Gmail question as built. The folder views are
SHIPPABLE once F10 and the LOW wording items are fixed.** Two problems block shipping. First,
a Desktop "Not now" can never be undone from the Desktop (F1). Second, an archive with no
backup settings shows the question but refuses both answers (F2). Both are Type III: tooling
that is missing and can be built. Neither one is a design fault. The two texts the owner must
sign off are judged under "Owner texts" below. Text (a) fails the phone-reader test. Text (b)
passes only in part, and other screens contradict it.

| Header | Value |
|---|---|
| Reviewed | branch `gmail-dedup`, commit `ce06fce` (slice B3 verified; code from `b3670c4`) |
| Process | `../assurance-kit/process/friction-review.md` |
| Date | 2026-11-13 |
| Environment | Windows 11 VM "Windows 11" (Parallels, ARM, running the amd64 build under emulation), Microsoft Edge, cross-built `GOOS=windows GOARCH=amd64` (`mailarchive.exe`, and `mailarchivew.exe` built with `-H=windowsgui`); macOS (native build), terminal driven through a real pseudo-terminal (`expect`), Desktop read with `curl`. Linux was not run (the terminal words are the same code on every platform). |
| Lab | A throw-away, Gmail-shaped Thunderbird profile written by a lab-only helper (never committed): one IMAP account on `imap.gmail.com`, `is_gmail` first false and then true, with INBOX, `[Gmail]/All Mail` and `[Gmail]/Sent Mail`. It made four throw-away archives (see "Lab setup"). Every run used a throw-away `-config`, `HOME`, `APPDATA` and `LOCALAPPDATA`. No real mail was used, nothing was sent, and the owner's settings were not touched. |
| Actor | the owner, an ordinary user at the Desktop or a terminal, reading on a phone-sized attention span |
| Cells walked | 15: 14 live, 1 by reading the code (an encrypted archive) |
| Functions | fully 10 · partly 3 · no 1 · not walked live 1 |
| High-friction findings | 2 HIGH, 9 MED, 7 LOW |
| Screenshots | private, not committed: `.claude/lab/shots/` in the review worktree. The two the brief asked for are `w05-edge.png` (the question card on Windows) and `w18-receipts-page.png` (a folder view on Windows). |

## Owner texts

### (a) The Gmail question (called Q-B in the design)

> Show Sent Mail and your other Gmail folders in full, and store each email once? Every computer
> that backs up into this archive then needs MailArchive 1.8 or later.
> (encrypted adds) Emails this archive already holds more than once stay as they are, because it
> is encrypted.

**Where it appears (seen):**
- **Desktop.** On the archive's row in Your archives, or on the "This archive" card when it is the
  only archive (Mac, read with curl). It shows only after a backup that ended well and found a
  Gmail account not yet switched on. It is never shown for an encrypted archive.
- **Terminal.** During a backup typed at a terminal, before the Gmail account is read (Mac, real
  terminal).
- **Help text.** The first sentence is repeated in the help for `-gmail-folders`.

**Verdict: fails as written.** A reader on a phone cannot tell four things from it:
1. **What is wrong now.** "In full" assumes the reader knows Sent Mail and the labels are missing
   emails today. Nothing says so.
2. **Whether anything gets deleted.** "Store each email once" sounds like copies will be removed.
   Yes deletes nothing. Tidying up the old copies is a separate offer that comes later.
3. **That it cannot be undone.** The design says there is no way back from Yes (Q-C), and the
   question never says so.
4. **What each answer does.** Not now changes nothing, and Yes takes effect at the next backup.
   The question says neither.

It also has no heading. On the Desktop the reader sees a sentence and a "Yes" button sitting among
Switch, Pause, Remove and Delete (F5).

**Proposed words (Desktop and terminal alike):**

> **Gmail: show every folder?**
> Some Gmail emails are missing from Sent Mail and your labels in this archive, because
> Thunderbird keeps only one copy of each. Yes lists each email in every folder it is in, and
> saves new emails only once. Nothing is deleted.
> After Yes, every computer that backs up into this archive needs MailArchive 1.8 or newer. This
> cannot be undone.
> Buttons: **Yes, show every folder** · **Not now**

Encrypted archives add: "Emails already saved twice stay as they are: MailArchive cannot tidy an
encrypted archive."

The terminal keeps its answer line: "Type y and press Enter for Yes. Press Enter for Not now."
It reads well.

### (b) The tidy-up words (`state.TidyUpWords`, `internal/state/dedup.go` line 197)

> A newer MailArchive is part-way through tidying this archive (keeping one copy of each Gmail
> email). Finish it there, or install that version on this computer and choose Resume tidy-up.
> Until then this computer does not back up into it.

**Where it appears.** Lab archive `Tidy-up`, a copy stamped with format version 8. These were seen
on Windows:
- **Your archives row.** The words show, but in the greyed "unavailable" style (F16).
- **Backup at the command line.** The words show, then "(manifest …: format version 8; this build
  understands 7)". The exit code is 1.
- **`status`.** The words show three times, each under "RED: the archive's manifest cannot be
  read".

Three screens do **not** use the words and contradict them (F9):
- **`add-archive`** says "record … cannot be read — run status … to repair it".
- **The Desktop's Scheduled backup card** says "MailArchive can't read this archive's record right
  now (is its drive connected?)" and "a backup set up at the command line". The second part is
  also wrong: the lab archive was set to Run now only.

**Verdict: passes in part.** The last sentence (this computer won't back up) is clear. The rest
fails on a phone in four ways:
- "there" does not say where.
- "That version" does not name a version.
- "Resume tidy-up" does not say where to click it.
- Nothing says the mail is safe. The other screens, which say "cannot be read" and "repair",
  push the reader toward thinking the archive is broken.

**Proposed words:**

> Another computer is tidying this archive with a newer MailArchive (it removes extra copies of
> Gmail emails). Nothing is lost. Until it finishes, this computer does not back up into it.
> To finish: let it finish on that computer, or install the newest MailArchive here and click
> **Resume tidy-up** on this archive's card.

The same words should replace the three contradicting texts (F9). In `status`, this should be
WARN or its own posture rather than "manifest cannot be read". Which posture is the lead's call.

## Findings

Severity is HIGH (blocks ship), MED (fix before release) or LOW (polish). The type is the
process's: I is security-inherent, II is a design choice, III is buildable-away.

**F1 · HIGH · Type III. A Desktop "Not now" is permanent, and nothing on the Desktop brings the
question back.**
- *Where seen:* Your archives row, Windows Edge (`w24`, `w26`).
- *What happens:* After Not now the question vanishes and the row shows nothing about Gmail. The
  design promised a copy of the question in Settings ("Settings keeps the same question
  available"), and it is not built. The guide "How to use MailArchive" does not mention it. A
  terminal backup then also stops asking, silently, because it reads the same Not now from the
  job file. The only way back is the `-gmail-folders` flag, which a Desktop user will not know.
- *Fix:* Add a line in Settings, or on the archive's card: "Gmail folders: not switched on.
  [Turn on…]". It opens the same question. Alternatively, keep a small "Ask me again" link on
  the row.

**F2 · HIGH · Type III. With no backup settings on this computer, the question shows but neither
answer works.**
- *Where seen:* lab archive `No settings` (no job file), Windows Edge (`w07`, `w08`).
- *What happens:* Both Yes and Not now reply "MailArchive could not keep your answer: this archive
  has no backup settings on this computer. Back it up once from this page, then answer." That row
  has no backup button. A backup typed at a terminal leaves the question there, so the row shows
  a question the person can neither answer nor dismiss.
- *Fix:* Keep the answer where every archive has a place on this computer (Desktop settings,
  keyed by the archive's folder). Failing that, do not show the buttons, and name the real steps:
  "Click Switch to this archive, set How often in Settings, then answer here."

**F3 · MED · Type III. The confirmation after Yes or Not now is easy to miss.**
- *Where seen:* Windows Edge (`w25`).
- *What happens:* The note appears below the row's buttons. When the row sits near the bottom of
  the screen the note is off-screen, and the page reloads after 3 seconds. In the walk, the Not
  now note was never visible before the reload.
- *Fix:* Put the note in place of the question text, on the row itself. Then drop the automatic
  reload, or give it at least 8 seconds.

**F4 · MED · Type II (words). Text (a) fails the phone-reader test.** See "Owner texts" above for
the reasons and the proposed words.

**F5 · MED · Type III. The question is not marked off from the row's other buttons.**
- *Where seen:* Windows Edge (`w05`).
- *What happens:* The question is a plain line of text. "Yes" and "Not now" are drawn exactly
  like "Switch to this archive", "Pause", "Remove from list" and "Delete…", on the same line, so a
  bare "Yes" can be read as agreeing to something on the row. With two Gmail archives, the long
  question repeats on both rows.
- *Fix:* Give the question its own small box with the heading "Gmail: show every folder?". Use
  the buttons "Yes, show every folder" and "Not now".

**F6 · MED · Type III. Switching on happens silently.**
- A new Gmail archive becomes version 7 at its first run, which means every computer then needs
  1.8. No screen says so: `state.GmailSetupLine` ("Gmail: each email is stored once and shown in
  all of its folders.") is defined but used nowhere. The design asks for it at setup and in
  Settings.
- After a Desktop Yes, the "you said yes" line stays until a backup runs. The Run now result that
  then switches the archive on says nothing about Gmail (`w12`). Afterwards the row says nothing
  either (`w24`).
- *Fix:* Show the setup line on the archive's card, or in Settings, for every switched-on archive.
  Add "Gmail: switched on for this archive" to the result of the run that switched it.

**F7 · MED · Type II. The terminal asks again on every run, and waits forever.**
- *Every run.* A terminal Not now is not remembered ("MailArchive asks again at the next backup
  you start at a terminal"). Someone who backs up at a terminal is asked on every run. The
  Desktop's Not now means "never again", so the two places behave differently.
- *Forever (found by reading the code, not walked).* The question is asked inside the run, after
  the archive's lock is taken, and has no time limit. A person who walks away from the question
  holds the lock, and scheduled backups then skip the archive.
- *Fix:* Remember a terminal Not now on this computer even with no job file. Treat no answer
  within about 2 minutes as Not now.

**F8 · MED · Type III. Nothing outside the question says it is waiting (UX contract X6:
legibility).**
- *Where seen:* Mac and Windows (`1b`, `1c`, setup output).
- *What happens:* A backup with no terminal (a script, or a scheduled run) and `status` say nothing
  when the question is waiting. A command-line user never learns that it exists.
- *Fix:* Add a `status` line: "Gmail: not switched on for imap.gmail.com — answer the question in
  MailArchive Desktop, or back up once with -gmail-folders." Print the same line in the summary of
  a run with no terminal.

**F9 · MED · Type III. Other screens contradict the tidy-up words.**
- *Where seen:* Windows: `add-archive` output, the Desktop's Scheduled backup card (`w16`),
  `status`.
- *What happens:* `add-archive` says "cannot be read … repair it". The Scheduled backup card says
  "is its drive connected?" and "a backup set up at the command line". `status` labels it RED,
  "manifest cannot be read". A reader sent to repair, or to look for a drive, may damage a
  tidy-up in progress on the other computer, or give up.
- *Fix:* Every refusal of a newer format uses `NewerFormatWords`, including the two Desktop texts
  and `add-archive`.

**F10 · MED · Type III. The "This folder" link on a message page leads to All Mail, whatever
folder the reader came from (design GD-18b, not built).**
- *Where seen:* Windows Edge (`w19`).
- *What happens:* Opened from the `Receipts` folder page, the message page's "This folder" goes to
  All Mail, and nothing says so. The design (§8.4 fold) requires "This folder: All Mail".
- *Fix:* As the design says, name the folder in the link's words.

**F11 · LOW · Type III. The internal folder path `[Gmail]/All Mail` is shown to the reader.**
- *Where seen:* "(saved in [Gmail]/All Mail)" on folder pages (`w18`), "In: [Gmail]/All Mail,
  INBOX, Receipts" in the reader (`w22`), and the reader's Folder filter (`w23`).
- *Fix:* Show "All Mail" and "Sent Mail" in these new texts. The builder's own wording was
  "(saved in All Mail)".

**F12 · LOW · Type III. The count line is jargon, and its number clashes with the line beside it.**
- *What happens:* The line reads "Gmail: 19 messages. 4 folder entries taken from Thunderbird's
  folder lists." It sits beside "Thunderbird has downloaded everything it lists for
  sam.walker@gmail.com (12 messages)". The reader sees 19 next to 12, and "folder entries" means
  nothing to them.
- *Fix:* "Gmail: 19 emails saved; 4 of them are also listed in another folder without a second
  copy." Say why 19 differs from Thunderbird's 12, or do not put the two lines side by side.

**F13 · LOW · Type III. Terminal yes: two lines say the same thing, and the question goes to the
wrong output.**
- *What happens:* After y, "Yes: from this backup on, each Gmail email is stored once…" is
  followed at once by "Gmail: you said yes — from now on each email is stored once…". The
  question is also written to standard output, while the backup's progress goes to standard
  error. The UX contract (X8) keeps the conversation with the operator on standard error.
- *Fix:* Print one line, and write the question to standard error.

**F14 · LOW · Type II. The `-gmail-folders` flag (checked by name, as design §7.4 (c) asks).**
- *What happens:* The name reads like "back up the Gmail folders" rather than "say yes to the
  question". Its help repeats the question but does not say that yes cannot be undone.
- *Fix:* Keep the name, which is short and matches the words "Gmail folders". Start its help with
  "Say yes to the Gmail question without being asked" and add "This cannot be undone."

**F15 · LOW · Type III. The "you said yes" line does not say how to make it take effect.**
- *What happens:* The line says "From this archive's next backup on…". For an archive set to Run
  now only, the next backup never comes by itself.
- *Fix:* Add "(click Run now to start it now)" when the archive has no schedule.

**F16 · LOW · Type III. On Windows the tidy-up words are drawn in light grey.**
- *What happens:* The row uses the greyed "unavailable" style, so the one line that matters has
  the lowest contrast on the card.
- *Fix:* Draw the problem text at normal contrast.

**F17 · LOW · Type III. The command-line `search` lists only the home folder.**
- *What happens:* `search -folder Receipts` finds the email but shows "[Gmail]/All Mail" as its
  folder. The reader shows "In: …" for the same email.
- *Fix:* Print the same "In: …" in the command-line results.

**F18 · MED · Type II (design O1). An encrypted archive is never asked on the Desktop.**
- *Not walked:* found by reading the code (`gmailCard` returns nothing for an encrypted archive).
  The builder's record lists it as not built.
- *What happens:* A Desktop-only user with an encrypted Gmail archive can never switch it on. Only
  a terminal backup asks.
- *Fix (the lead's call):* Ask on the encrypted card while the archive is unlocked, or add a
  Settings switch.

**Seen and not a finding:** INBOX lists "Lunch on Friday?" twice in the lab. The lab helper gave a
record a membership in INBOX while INBOX also held its own copy, a state the product does not
make.

**For the lead: the builder's calls.**
- *Applying the card's yes at the next backup:* acceptable, given F6 and F15.
- *A flag instead of a separate command:* acceptable, given F14.
- *GD-18b not built:* that is F10.
- *Release:* B3 must not ship ahead of D1 and M1. Until they ship, a Yes changes nothing visible,
  yet the words promise "shown in all of its folders".

## Cells

| # | Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog |
|---|---|---|---|---|---|
| 1 | Owner, an older archive (made before the account counted as Gmail), backup with no terminal (Mac, Windows) | yes | silent: nothing says a question waits | III | F8 |
| 2 | Owner at a terminal, presses Enter (Not now) (Mac) | yes | asked again on every run; waits with no time limit | II | F7 |
| 3 | Owner at a terminal, types y (Mac, real pseudo-terminal via `expect`) | yes (version 7, `gmail-1`) | two lines say the same; question on standard output | III | F13 |
| 4 | Script with `-gmail-folders` (Mac, Windows) | yes | flag help omits "cannot be undone" | II | F14 |
| 5 | New archive of a Gmail account (Mac) | yes (version 7 at first run) | switched silently; the 1.8 requirement is never said | III | F6 |
| 6 | Desktop Yes with backup settings, then Run now (Windows Edge) | yes | note missed; Run now silent; "next backup" unexplained | III | F3, F6, F15 |
| 7 | Desktop Not now with backup settings (Windows Edge) | yes | gone for good, nothing brings it back; note off-screen | III | F1, F3 |
| 8 | Desktop question on an archive with no backup settings (Windows Edge) | **no** | both answers refused; cannot be dismissed | III | F2 |
| 9 | Only one archive: the "This archive" card (Mac, curl) | yes | the question shows on the card | — | — |
| 10 | Folder page of a folder that holds only listed emails (`Receipts`) (Windows Edge) | yes | `[Gmail]/` shown | III | F11 |
| 11 | Message opened from a member folder; "This folder" link (Windows Edge) | partly | leads to All Mail without saying so | III | F10 |
| 12 | Reader search "In:" line and Folder filter with counts (Windows Edge) | yes | `[Gmail]/` shown | III | F11 |
| 13 | Another computer part-way through the tidy-up (version 8): Desktop row, Scheduled backup card, command-line backup, `status`, `add-archive` (Windows) | partly | contradicting texts; grey text; RED "cannot be read" | III | F9, F16, text (b) |
| 14 | Command-line `search -folder Receipts` (Mac) | partly | home folder only | III | F17 |
| 15 | Encrypted Gmail archive (code read only) | not walked | never asked on the Desktop | II | F18 |

## Lab setup (for whoever re-walks it)

Four archives were made from one profile.
- `Sam Gmail`: backed up while the account did not count as Gmail, then backed up again with no
  terminal after the switch to Gmail. The question waits, and it has a job file (Run now only).
- `No settings`: the same, with no job file.
- `Folders on`: `-gmail-folders`, then hand-made listed memberships (INBOX and `Receipts` for
  message 1, `[Gmail]/Sent Mail` for 10, `Receipts` for 7). B3 itself fills none; slices D1 and
  M1 will. Then `reindex -rebuild`.
- `Tidy-up`: listed in Your archives, then its manifest stamped 8.

The scripts live in the review worktree's `.claude/lab/`, and the Windows side lives at
`P:\fric-b3`. Neither is committed.

Not covered:
- the encrypted terminal question and the encrypted card (cell 15);
- Linux;
- the wizard (`mailarchive-gui`) runs, which do not ask (the builder's note);
- the time-limit concern in F7, which comes from reading the code.
