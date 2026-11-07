# Restore to Thunderbird — friction review of the R7 Desktop flow

**Verdict: SHIPPABLE_WITH_NOTES.** Every step of the Desktop flow worked on Windows, by keyboard
and by mouse, and every refusal said what happened and what to do, in place. Six wording problems
were fixed in this commit (test-first). Nine behaviour proposals are left for the lead; none blocks
the flow, but two of them (the profile list and the page after a crash) are the ones a person is
most likely to trip on. macOS was walked only up to Check, because the owner's own Thunderbird was
open on this Mac and the restore rightly refuses while any Thunderbird runs. Linux was not walked.

Gate: the R7 friction review (design §13, slice R7; `../assurance-kit/process/friction-review.md`)
and the X-series walk (`docs/ux-contract.md`).

## Header

| | |
|---|---|
| Reviewed | branch `restore-thunderbird`, the build of 54bb12b (R7's 82d2852 plus lab row MA-1133's W-NOLOCAL words), built as `packaging/build-release.sh` builds the Windows pair (`GOOS=windows GOARCH=amd64 go build`, `mailarchivew.exe` with `-ldflags -H=windowsgui`) and, for macOS, `go build ./cmd/mailarchive` |
| Windows | the Parallels VM "Windows 11" (ARM64, amd64 programs emulated), Microsoft Edge driven through its DevTools port with real key and mouse events, 7 November 2026, 06:00–06:35 VM time (UTC−5) |
| macOS | this Mac, Chrome, the same day; Thunderbird 159.0 installed (the owner's, running on the owner's profile, never touched) |
| Linux | **not walked** |
| Thunderbird on Windows | 153.6.0, throwaway profiles only, under `C:\Users\Public\tb-restore-lab2\` |
| Actor | the owner, non-technical, keyboard first, at the computer |
| Isolation | Windows: the walk's programs ran with `USERPROFILE`, `APPDATA` and `LOCALAPPDATA` pointed at `C:\Users\Public\tb-restore-lab2\walkhome` (checked first: Windows' known-folder lookup that MailArchive uses for Thunderbird's profile list follows `USERPROFILE`), its own `-config`, its own Edge profile, and a link stand-in for `rundll32`. macOS: `HOME` pointed at `.claude/rtbwalk/machome-walk`, its own `-config`, a stand-in for `osascript`. Never the VM user's or the owner's MailArchive settings or Thunderbird profiles |
| Archives | **Walk Plain**: a Thunderbird-style folder with `-raw` (Inbox 6, Projects 5, Projects/Plans 1 = 12 messages; one 2 KiB attachment; one message with `From ` and `>From ` body lines), backed up from the walk-good profile's Local Folders, so W-RECAPTURE applies. **Walk Encrypted**: the same 12 messages, made at a terminal (a pseudo-terminal on the Mac) and copied to the VM. **Walk Outlook**: `testdata/support.pst`, nothing restorable. **Walk Big**: 15,000 messages, 178.9 MiB, for the mid-run steps. **Walk Encrypted Big**: 6,000 messages, for Lock during a run |
| Profiles listed | walk-good (Local Folders, the walk's mail), walk-nolocal (made by Thunderbird, no account), walk-junction (reached through a junction outside the walk's home), walk-synced (under a folder named `Dropbox`; no sync runs) |
| Cells walked | 20 (table below): 19 function fully, 1 partly (macOS, stopped at Check by design) |
| Findings | 0 high · 7 medium · 14 low; 6 fixed in wording here, the rest proposals |

## The walk

Friction: **H** high, **M** medium, **L** low. Type (per the friction review): **I** the ceremony is
the control; **II** reducible by redesign; **III** buildable-away legibility.

| # | Cell | Functions? | What happened | Friction |
|---|---|---|---|---|
| 1 | Reach the card by keyboard | yes | 56 Tab presses from the top of the page to **Restore to Thunderbird…**; the sidebar has no entry for it (nor for Maintenance). | L (II) — F10 |
| 2 | One profile | yes | The profile step was skipped; the What step came up with the keyboard on **The whole archive** and its line said what to do. | none |
| 3 | Two profiles, then four (one unusable) | yes | Listed with their folders; "(this archive backs it up)" beside walk-good; walk-junction disabled with its W-JUNCTION reason beneath; arrow keys skip the disabled one. But **walk-nolocal, which has no Local Folders, was the pre-selected one** (the archive does not back it up), and nothing on the list says it cannot be used. | **M (II) — F1** |
| 4 | Check on the profile with no Local Folders | yes | W-NOLOCAL in place, the keyboard on Check, with MA-1133's real menu path. There is no way back to the profile step except Cancel and start again, and the words (before this commit) did not mention choosing another profile. | M (II) — F1, F2; wording fixed |
| 5 | Choose folders | yes | The tree loaded with counts: "Local Folders (12)", "Inbox (6)", "Projects (6)", "Plans (1)". Projects holds 5; its 6 includes Plans. Ticking Projects leaves Plans unticked, though it will be restored. | L (II) — F8; F9 wording fixed |
| 6 | No folder ticked, Check | yes | "No folder is ticked, so nothing was planned…" in place. | none |
| 7 | Check with Thunderbird open (throwaway Thunderbird on walk-good) | yes | W-RUNNING in place, the keyboard on Check. It named "File → Exit" for Windows; Thunderbird 153 hides the File menu bar by default (seen in MA-1133's screenshots); its ≡ menu has Exit. | M (III) — F6, fixed |
| 8 | The plan's order | yes | W-RECAPTURE first, then the destination, what, damaged-later, space, "Thunderbird is closed.", scheduled backups, the check, **W-WAIT last**. For an archive that does not back the profile up (Walk Big) no W-RECAPTURE line; for the encrypted archive "cannot tell". The new folder's name moves on to " (2)" when the first is taken. | none; L notes F17, F20 |
| 9 | Full restore, then Thunderbird | yes | "Restored 12 messages … 'Restored — Walk Plain' … MailArchive checked every message it wrote." The throwaway Thunderbird showed **Restored — Walk Plain › Inbox (6), Projects (5) › Plans**; the `From here…` and `>From there…` lines exactly as written; the attachment "walk-bytes.bin 2.0 KB"; "Building summary file for Projects…". | none |
| 10 | Save this record | yes | A UTF-8 text file (no byte-order mark, LF lines) with date, computer, account, version, archive, the plan and the result. Edge saved it **straight into the downloads folder without asking**; the encrypted archive's note said "it is saved only where you choose". | M (III) — F5, fixed |
| 11 | Outlook-only archive | yes | The person goes through the profile step and the What step before Check says W-PST ("…none of it can go back into Thunderbird"). The tree, when opened, already showed "(0)" everywhere. | M (II) — F7 |
| 12 | Encrypted archive, locked, key not remembered | yes | The button disabled, its reason beside it: "…then click Restore to Thunderbird…." (an ellipsis and a full stop). After Unlock the button worked. | L (III) — F12, fixed |
| 13 | Encrypted: the tick, and the second yes (Local Folders under `Dropbox`) | yes | Restore without the tick: W-TICK in place, the keyboard on the tick. With the tick but not the second box: W-NOTAGAIN, the keyboard on the second box. Both ticked: restored, with the record note. The plan said NOT-ENCRYPTED and W-LEAVES, and "You are asked a second time before it starts." W-NOTAGAIN says "Confirm it again" while the box says "Restore it there anyway". | none; L F15 |
| 14 | Lock during a run (Walk Encrypted Big, 6,000) | yes | Lock answered "Locked. The restore into Thunderbird already started goes on until it finishes — Shut down stops it, and nothing is added to Thunderbird." The restore finished. Afterwards the Restore button stayed enabled on the now-locked archive until the page was reloaded. | L (III) — F16 |
| 15 | Cancel mid-run (Walk Big) | yes | Cancel pressed 1.5 s in (progress still "Restoring: 1 of 15,000 messages (Big A)", the line moves every 3 s): "The restore was stopped, so nothing was added to Thunderbird. The part already written was removed. …" Local Folders: no " (2)" left. A first attempt was too slow: 15,000 messages restored in about 4 s. | none |
| 16 | Shut down mid-run | yes | Shut down's first press says "Click again to stop MailArchive Desktop. You can reopen it any time; your archives and schedules are untouched." — **nothing about the running restore**. On the first try the restore ended meanwhile and its result step **took the keyboard away from Confirm shut down** onto Save this record. Confirmed mid-run on the second try: restores.log "ended … stopped", nothing left in Local Folders. | M (III) — F4 |
| 17 | Forced kill mid-run, then the next restore | yes | Desktop killed (`Stop-Process -Force`) 1.5 s in. **The page went on showing "Restoring: 1 of 15,000 messages (Big A)" with no sign that MailArchive had stopped.** On disk: `Restored — Walk Big (3).sbd` with a partial `Big A` and no parent file. Next start: W-UNFINISHED at the top of the page, naming the folder and its path. The next plan: "An earlier restore into this profile did not finish (…). Its partial folder, which Thunderbird does not show, will be removed first." The next restore removed it and made " (4)". The notice stayed at the top until the page was reloaded. | **M (III) — F3**; L F16, F18 |
| 18 | The guide, "Putting mail back into Thunderbird" | yes | Clear and true to the flow; said the folder count was only a folder's own messages, and said nothing about where the record is saved or a profile with no Local Folders. | L (III) — fixed |
| 19 | Reduced motion | yes | With `prefers-reduced-motion: reduce` emulated, the step came into view at once (no smooth scroll), the keyboard on its first control. Without it, every step is scrolled so its status line sits at the very top edge, and the card's heading is out of view. | L (II) — F11 |
| 20 | macOS pass (Chrome) | partly | Two throwaway profiles listed, mac-good pre-selected; the tree's counts as on Windows; the locked button's reason; Check refused W-RUNNING, correctly: the owner's Thunderbird was open on its own profile, and §5.1 check B refuses while any Thunderbird runs. A full restore, Cancel and Lock were not walked on macOS. | L (I) — F21 |

Mouse checks (Windows): Next, Choose folders, ticking a folder by clicking its label text, Check and
the plan's Cancel all worked by mouse; the panel closed with "Nothing was restored." and the
keyboard back on the button.

## Findings by severity

### Medium

- **F1 — a profile with no Local Folders is pre-selected and not marked.** The list judges a
  profile only by its folder and `prefs.js`; Local Folders (and the maildir kind) are judged at
  Check. With two profiles the default rule (one this archive does not back up) picked the empty
  one. The person learns at Check and has to Cancel and start again. *Fixed in words:* W-NOLOCAL now
  says first "If you have another Thunderbird profile, choose that one instead." *Proposal P1:* judge
  Local Folders and the store kind when the list is made (it is read-only), show W-NOLOCAL or
  W-MAILDIR beside that profile as for W-JUNCTION, and never pre-select it.
- **F2 — no way back from the What step to the profile step.** Every refusal that says "choose
  another profile" (W-NOLOCAL, W-MAILDIR, W-OVERLAP, W-NOIDENT) needs Cancel, then the button again.
  *Proposal P2:* a "Change profile" link on the What step when there was a choice.
- **F3 — after a crash the page says nothing.** When MailArchive Desktop stops while a restore runs
  (killed, crashed, the computer sleeping and the process gone), the run step keeps the last
  progress line forever; the page script retries `api/activity` every 3 seconds in silence.
  *Proposal P3:* after a few failed polls, say "MailArchive Desktop is not answering. If it has
  stopped, start it again: the top of its page says whether this restore finished."
- **F4 — Shut down while a restore runs.** Its confirmation does not say that it stops the restore,
  and a restore's result step moves the keyboard away from Confirm shut down. *Proposal P4:* when a
  restore runs, the confirmation adds "A restore into Thunderbird is running: shutting down stops it,
  and nothing is added to Thunderbird." (the Lock reply's words); and a step that ends while the
  keyboard is elsewhere on the page does not move it (`showStep` only when the focus is in the card).
- **F5 — where the record goes.** *Fixed:* the encrypted record's note now says "Your browser
  saves it with your downloads (usually your Downloads folder): move it somewhere private, or delete
  it when you no longer need it."; the guide and the README say it is saved with the downloads.
- **F6 — the Windows way to quit Thunderbird.** *Fixed:* W-RUNNING says "on Windows: the menu button
  ≡, then Exit".
- **F7 — nothing restorable is learnt last.** For an Outlook-only (or no-originals) archive the
  person goes through two steps before Check says W-PST or W-NORAW. The builder's note says the page
  load cannot afford to judge every record; but the `folders` route already does (the tree's
  counts). *Proposal P5:* when Restore to Thunderbird… is clicked, ask the folders route first, and
  when every count is 0 say W-PST or W-NORAW at once, beside the button.
- **F21 (graded L, Type I) — any running Thunderbird refuses.** On a Mac where the person's
  Thunderbird is open on another profile, the restore refuses until it is quit. This is §5.1's
  deliberate safe direction; the words say exactly what to do. No change proposed. (Listed here
  beside the mediums for its weight on macOS; counted as low.)

### Low

- **F8** ticking a folder does not tick or grey its subfolders, though they go with it. *P6:* tick
  and disable the children.
- **F9** *fixed:* the tree's step line and the guide say a folder's number counts "messages in it and
  its subfolders".
- **F10** no sidebar entry: 56 Tab presses from the top. *P7:* a sidebar link "Restore to
  Thunderbird" (and "Maintenance").
- **F11** each step is scrolled so its status line touches the top edge and the card heading is out
  of view. *P8:* `scroll-margin-top` on the steps, or scroll the card's heading into view.
- **F12** *fixed:* the locked reason reads "This archive is encrypted and locked: unlock it on its
  Encryption card first."
- **F13** counts are written "15,000" in the plan and progress but "15000" in the result, the tree
  and the record. *P9:* group digits in `wDone` and the tree.
- **F14** in the profile list, W-JUNCTION's words say "so nothing was restored … choose it by that
  path, then click Check again" — nothing was tried yet, and Desktop has no way to give a path.
- **F15** W-NOTAGAIN says "Confirm it again"; the box says "Restore it there anyway".
- **F16** two things on the page stay stale until a reload: the W-UNFINISHED notice after the
  restore that cleaned it up, and the Restore button after a Lock.
- **F17** the plan of a chosen-folders restore gives counts only, not the folders' names.
- **F18** the next name skips: " (4)" was made while " (3)" was being removed first.
- **F19** at a terminal W-NOPROF says "click Restore to Thunderbird again" (seen while checking the
  walk's isolation with `restore-thunderbird -list-profiles`).
- **F20** units: "about 512.0 MiB is kept free", KiB/MiB/GiB for a non-technical reader.

## Fixes made in this commit (lead's words, owner-delegated; test-first)

| Words | Where | Pinned by |
|---|---|---|
| W-NOLOCAL: "If you have another Thunderbird profile, choose that one instead. Otherwise: …" | `internal/tbrestore/refusal.go` | MA-1085 "no Local Folders" (`ma1085_prefs_test.go`) |
| W-RUNNING: "on Windows: the menu button ≡, then Exit" | `internal/tbrestore/refusal.go` | MA-1127 (`ma1127_words_test.go`) |
| The locked button's reason | `internal/desktop/restoretb.go` | MA-1131 (`ma1131_friction_words_test.go`) |
| The encrypted record's note: saved with the browser's downloads | `internal/desktop/restoretb.go` | MA-1131 (same) |
| The tree's step line: "how many messages in it and its subfolders can go back" | `restoretbscript.go`, the page-script harness's pinned words | MA-1131 (same, and `TestTheRestorePage…`) |
| The guide: the count, where the record is saved, a profile with no Local Folders | `internal/desktop/help.go`; README | MA-1131 (same) |

Prove-fail → prove-pass: each test was run red against the old words (MA-1085: "refusal must name
'If you have another Thunderbird profile…'"; MA-1127: the three platforms' W-RUNNING; MA-1131's new
test: seven failures), then green with the new words.

## Behaviour proposals (for the lead)

P1 judge Local Folders and the store kind in the profile list, mark and never pre-select an
unusable profile · P2 "Change profile" from the What step · P3 say when Desktop stops answering
during a run · P4 Shut down's confirmation names a running restore, and a step ending elsewhere does
not take the keyboard · P5 say "nothing restorable" at the button · P6 a ticked folder ticks its
subfolders · P7 sidebar entries · P8 keep the card heading in view · P9 group digits everywhere.
P1, P3 and P4 matter most.

## X-series walk

| X | Seen | Note |
|---|---|---|
| X2 one refusal voice | met | Every refusal seen (W-RUNNING, W-NOLOCAL, W-PST, W-TICK, W-NOTAGAIN, no folder ticked, W-JUNCTION in the list) said what happened, then the remedy, in the step's status line. F14, F15 are wording nits. |
| X7 / X11 no traceback, plain labels | met | No crash text anywhere, including after the forced kill. Labels name outcomes ("Restore to Thunderbird…", "Check", "Cancel the restore", "Save this record"). |
| X9 / X13 guarded routes | met (not re-probed) | The walk used only the page; the R7 adversarial pass (3272fee) probed the routes. |
| X12 locked, not blank | not walked | |
| X14 look before writing | met | Check always shows the plan and writes nothing; Restore writes exactly the plan (one-use token; " (2)" honoured). |
| X15 inert mail text | met for what was seen | Folder names came through as text; no hostile names were tried in this walk. |
| X16, X1, X3–X6, X8, X10, X17 | not in this flow's scope | X1/X3 are the CLI's (R6). |

## Lab evidence this walk gives

- **MA-1133** (Windows): ran in full the same morning, `docs/lab/restore-thunderbird-windows-MA-1133.md`.
- **MA-1093** (Windows, partly): a restored fixture opened in Thunderbird 153.6.0 with every folder
  and count right, the `From ` lines exact, the attachment present, summaries built. Not done: the
  byte comparison through a Thunderbird copy, LF messages, a non-English name, Gmail and Microsoft
  365 messages, read state, the deleted bit, the profile's growth. Stays pending.
- **MA-1091** (Windows, partly): the restore's own dot-named marker files inside its `.sbd` were
  not shown and got no `.msf`. The leftover `.sbd` with no parent file after the kill was seen on
  disk but Thunderbird was not opened on it. Stays pending.
- **MA-1135** (Windows, first half only): MailArchive killed mid-write left a hidden partial
  container, said W-UNFINISHED at the next start, and the next restore removed it. Thunderbird was
  not started on the leftover. Stays pending.
- Speed, for the record: 15,000 messages (178.9 MiB) written and checked in about 4 seconds
  (restores.log 11:18:22.5 → 11:18:26.6 UTC) on the ARM VM running the amd64 build.

## Where things are

- Screenshots (not committed; some show the VM user's desktop and name — keep private):
  `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab-r8/` — `w-01`…`w-23`
  (Windows walk), `m-01-mac-wrunning.jpg` (macOS), `j1-*` (MA-1133).
- Throwaway folders left in place: on the VM `C:\Users\Public\tb-restore-lab2\` (walkhome, the
  walk-* profiles, Dropbox\walk-synced, walk-src, walk-bigsrc, walk-support.pst) and
  `%LOCALAPPDATA%\rtbw\` (programs, the link file, the saved records under `out\downloads`, the
  lab password file); on the Mac `.claude/rtbwalk/` in the main checkout (sources, the macOS
  walk's home and config, the encrypted archives in `enc2/` and `enc3/`, and a partial encrypted
  folder in `enc/` from two runs stopped before they finished).
- Scripts (not committed): `/Users/alex.dimarco/repositories/.winlab/rtbwalk/` and
  `.claude/rtbwalk/` in the main checkout.

---

- Date worked on: 7 November 2026 (the VM's clock and this session's date agree).
- Drawn on: design §3.2, §5.1, §7.3–§7.5 and §13's R7 notes; catalog rows MA-1131, MA-1133,
  MA-1091, MA-1093; `../assurance-kit/process/friction-review.md`; `docs/ux-contract.md`; the code of
  the card and its script; what the VM and the Mac showed. Not drawn on: Thunderbird's source;
  a Linux desktop.
- Register: a review (findings and a verdict), with the fixed words marked as the lead's and the
  proposals left for the lead.
- Smallest check for a reader: on the VM, open
  `%LOCALAPPDATA%\rtbw\out\downloads\MailArchive restore record - Walk Plain - 2026-11-07.txt`;
  it holds the plan with W-RECAPTURE first and W-WAIT last, and the result.
- Counters. Register: review. Borrowed lines: MailArchive's and Thunderbird's own words, quoted.
  Drift: a first draft of this file miscounted the findings (9 medium, 13 low); corrected before
  commit to 7 medium (F1–F7) and 14 low (F8–F21). Touched: this file, the words and tests named above, the design
  and catalog notes; on the VM only the throwaway folders. Persona: none. Source reached: design,
  catalog, process docs, the code — yes; the VM and the Mac — yes; Linux — no.
