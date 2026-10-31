# Friction review — the attended Thunderbird sync (design-presync-attended §14 gate 3)

Process: `../assurance-kit/process/friction-review.md`. Contract: `docs/ux-contract.md` (X11, X16, X17).

| Header | |
|---|---|
| Reviewed | branch `presync-attended`, base commit `71f2245` (AS0..AS8 built; macOS `AttendedStartAllowed` true, Windows false; every `AttendedQuitAllowed` false, so every Run now leaves Thunderbird open) |
| Date | 2026-10-31 |
| Environment | macOS (the reviewer's machine), Go 1.26.0; Windows texts read through the tests' table override and run on the Windows 11 lab VM (`internal/presync` text, outcome and start tests: PASS) |
| Actors | the owner on a Mac (plaintext and encrypted archives), the owner on a Windows PC, the owner reading the alert email on a phone, an operator reading `mailarchive status`, a new person reading the release notes cold |
| How the words were read | from the code that builds them and from the server-built notes the tests drive through the scripted Thunderbird (`internal/desktop/runnow_attended_test.go`, `runnow_keyed_test.go`); **not a live drill** — the task forbids starting a real Thunderbird. The live timing comes from the lab record `docs/lab/presync-attended-start-darwin.md` |
| Commands and statuses | `go test` of `internal/presync`, `internal/health`, `internal/alert`, `internal/backupalert`, `internal/desktop`, `internal/wizard`, `internal/presyncjob`, `assureblock`: ok. `go vet ./...` and `GOOS=windows go vet ./...`: clean |
| Baseline digest | `docs/scenario-catalog.md` sha256 `c38143e1bd3536fe1954130be176228c2297c2d4d9099129ed714e469da5cd52` (after this review's amendments) |
| Cells walked | 16 |
| Function | 12 fully, 4 partly (before the fixes); 16 fully after them |
| High-friction cells | 0 after the fixes (2 before: cells 8 and 10) |
| Verdict | **SHIPPABLE** for the macOS start and the encrypted texts, with the fixes below applied. The "for the owner" list is open and does not block. AS9 and AS-Q owe their own pass. |

## The walk

"Functions" is whether the text tells the person the truth and lets them act. Type is the process's:
I security-inherent, II design choice, III buildable away (wording, a missing hint).

| # | Cell (actor × scenario) | Functions? | Friction found | Type | Fix |
|---|---|---|---|---|---|
| 1 | Owner, Mac, first Run now with the sync on: reads the Run now tooltip and the one-time "New:" line | yes | The tooltip said "usually 13 to 20 minutes", but the lab measured one first fetch at +6 s, so 13 is not a floor. The "New:" line gave no length at all, so a 13-minute wait comes as a surprise. | III | F1, F3 |
| 2 | Owner, Mac, watching the waiting note for ~13 minutes | yes | "Thunderbird usually starts fetching about 10 minutes after it opens" claims more than the lab shows (once at +6 s, once at ~9.5 min). "It is left open when its mail settles" uses "settles", a technical word. | III | F2 |
| 3 | Owner, Mac, presses Stop waiting and back up now | yes | Clear. A press in the moment Thunderbird is still starting (a race: the button is hidden then) was refused with "No backup is waiting for Thunderbird", which is false — one is. | III | F5 |
| 4 | Owner, Mac, Cancel while starting, then while waiting | yes | Clear. The end line after a cancel that left Thunderbird open said "quit it … before the next Run now", but a scheduled backup would also find it and count a missed sync. | III | F8 |
| 5 | Owner, Mac, no new mail in 20 minutes | yes | Clear: says either no mail or a waiting password, and where to look. | — | none |
| 6 | Owner, Mac, encrypted archive: ticks Sync Thunderbird before each Run now, runs it, presses Lock while it waits | yes | "Until it starts reading mail, Lock cancels it" follows a sentence about the archive, so "it" reads as the archive. | III | F7 |
| 7 | Owner, Mac, encrypted, presses Lock after the backup started reading | yes | "Locked for new use." is not plain English; a person cannot tell what was locked. | III | F6 |
| 8 | Owner reads Status after a Run now that hit the 20-minute limit while mail was still arriving | **partly** | The saved run warning said Thunderbird "was asked to quit". A Run now never asks it to quit (never-close), so Status said something the code did not do. | III | F9 |
| 9 | Owner, a Thunderbird left open by a cancelled Run now; the next backup finds it | yes | The warning said it was started "by an earlier scheduled sync" (it was a Run now) and said "switch to that Thunderbird window", which is out of sight. | III | F9 |
| 10 | Owner, phone, reads the "Thunderbird not synced for 3 days" email | **partly** | "The last time: Thunderbird not synced: …" reads as "the last time it synced". "Do this: Click Run now on the Scheduled backup card" does not say on which computer or in which program. | III | F10, F12 |
| 11 | Owner reads the Status WARN for 3 days of missed syncs | yes | Same "the last time" reading as cell 10. Remedies are per reason and name the right button. | III | F10 |
| 12 | Owner reads the softer "nothing arrived" warning | yes | Word order ("Before this archive's scheduled backups since DATE, Thunderbird was started…") is hard to parse. "a window nobody can see" overclaims on Windows, where a minimized window shows in the taskbar. | III | F11 |
| 13 | Owner, Mac, reads the Settings card's sync notice | **partly** | The notice says that when the sync "asks Thunderbird to quit" on-exit actions run. Read on a Mac, where Run now never closes Thunderbird, it can be taken to mean Run now closes it. | III | F4 |
| 14 | Owner, Windows PC, Run now with the sync on (start not allowed yet) | yes | Tooltip, note and encrypted reason all say Run now does not start Thunderbird yet, and scheduled backups still do. No promise is made. | — | none. *Note 2026-10-31: Windows' start is now on (its `AttendedStartAllowed` entry was set true after lab MA-918 run 2), so this cell describes the build before that flip; the Windows texts with the start on were walked in "Windows texts pass (§14 gate 3), 2026-10-31" below.* |
| 15 | New person reads the release notes before upgrading | **partly** | "Run now now syncs" reads as a typo. The notes said nothing about the 3-day warning, its email, the softer warning or encrypted archives syncing. | III | F13 |
| 16 | Operator reads `mailarchive status` WARN at a terminal | yes | Remedies name `mailarchive -thunderbird-sync` without the archive folder, which is not a complete command on its own. | III | for the owner (O2); applied by the lead 2026-10-31, owner may revert |

No Type I or Type II friction was found in the words. The ~13-minute wait itself is Thunderbird's own
periodic mail check; the owner chose accuracy over speed (2026-10-21), and Stop waiting is the way
out. It is counted, not faulted.

## Findings and fixes applied

Each fix changes words only; no behaviour. Each was test-first: the test was updated to the new words
(same strength or stronger), run red against the old words, then the code was changed and the test
run green.

**F1 — the wait length overclaimed a floor.** Where: Run now tooltip (macOS, Windows), the encrypted
notice, the release line. A person would read "usually 13 to 20 minutes" as "never under 13". Fix:
"up to 20 minutes, often about 13", one constant (`syncWaitWords`, `internal/desktop/runnow.go`).

**F2 — the waiting note.** Where: `runNowSyncNote`, phase waiting. Now: "Thunderbird may fetch at once,
or wait about 10 minutes for its regular mail check, so this often takes about 13 minutes; with no
new mail it takes the full 20. Thunderbird is running out of sight and stays open after the wait."

**F3 — the one-time "New:" line.** Now: "New: Run now starts Thunderbird out of sight, even while you
are using the computer, waits up to 20 minutes for its mail, and leaves it open for you to quit from
the Dock when you are done." (Windows: minimized to the taskbar; the taskbar or the notification area.)

**F4 — the Settings notice on a platform where Run now starts Thunderbird.** `SyncNoticeFor` now says
"…a backup you start with Run now does not wait for that, and leaves Thunderbird open for you to
quit." Every X16 disclosure is kept. Where the start is off (Windows today) the notice is unchanged.

**F5 — Stop waiting pressed while Thunderbird is still starting.** Now refused with "Thunderbird is
still starting: Stop waiting works once it is waiting for mail, and until then Cancel is the way to
stop. Nothing was changed." The other refusals keep their words.

**F6 — Lock after reading started.** Now: "Locked: nothing new can open the archive. The backup that
is running had already started reading mail, so it finishes with its own key, then the key is
cleared. Click Cancel to stop it now."

**F7 — the encrypted Lock sentence.** Now: "Until the backup starts reading mail, Lock cancels it;
after that, Cancel stops it." (tooltip and Settings box).

**F8 — where to quit a Thunderbird a cancelled Run now left open.** The end line now says "before the
next backup", not "before the next Run now". The encrypted Lock reply keeps "before the next Run now":
an encrypted archive has no scheduled backup, so there it is exact.

**F9 — run warnings a Run now can now record.** The not-quiet warning no longer says Thunderbird "was
asked to quit": "…still changing at the maximum wait (20 minutes), so the archive ran with the mail
that had arrived by then. … Do this: the next backup will pick it up, …". The leftover warning: "A
Thunderbird an earlier backup started is still open, so this backup did not start another. Do this:
quit that Thunderbird; it may be hidden or minimized, so quit it from its icon where your open
programs are shown." It stays platform-neutral, as the 2026-10-24 friction review requires (it is
shown as written on every platform). New test `TestWarnTextTrueForARunNow`.

**F10 — "the last time".** Status WARN and email now say "most recently: PHRASE".

**F11 — the softer warning.** Now: "This is not a failed backup: the backups still ran. Since DATE,
Thunderbird was started before each of this archive's scheduled backups, but no new mail arrived (N
backups in a row). That can mean there was no new mail, or that Thunderbird is waiting for a password
or sign-in that nobody has answered: it was started out of sight, so its window is easy to miss. Do
this: …" (Windows: "started minimized").

**F12 — the email names where to act.** "Do this on that computer: REMEDY" and a line "The buttons and
cards named above are in MailArchive Desktop."

**F13 — the release notes.** The Mac line reads "On a Mac, Run now syncs Thunderbird first, even while
you are at the computer: … (up to 20 minutes, often about 13; …)". Three bullets added: encrypted
archives on a Mac can sync before Run now (Lock cancels until reading starts); a 3-day "not synced"
warning in Status with one email when "a failed backup" is ticked; the softer no-email warning. The
release-line check now also fails a line that says "Run now now".

Catalog rows amended with the settled words: MA-906, MA-909, MA-910, MA-913, MA-915, MA-926, MA-929.
The design keeps its draft words; §14 gate 3 points here for the words of record.

## For the owner (matters of taste, with a recommendation)

- **O1. One program name.** New texts say "Mail Archive Desktop" or "Mail Archive" (the Windows
  administrator and start-blocked lines, two Status remedies, and the email subject "Mail Archive:
  Thunderbird not synced…"), while the rest of the Desktop and the other alert emails say
  "MailArchive Desktop" and "MailArchive". Recommendation: use "MailArchive Desktop" and "MailArchive"
  everywhere a person reads, which is the accepted name in the UX contract (X17). Not applied here: it
  changes Windows-only tests that need the lab VM, and the Mac app is named "Mail Archive" in the Dock,
  so it is partly a naming choice.
  **Applied by the lead, 2026-10-31, owner may revert.** Every text this branch added that a person
  reads now says "MailArchive Desktop" where the Desktop is meant and "MailArchive" where the program
  is: the email subject ("MailArchive: Thunderbird not synced for 3 days — ARCHIVE", as the other
  alert emails' "MailArchive backup …"), the Desktop's administrator and start-blocked lines, the
  start-blocked, profile-busy ("Shut down MailArchive Desktop, open it again…") Status remedies, and the
  run warnings start-blocked, quit-no-window, state-unsaved and not-set-up. Not renamed: the macOS app
  bundle, the older texts from before this branch (among them the macOS Automation warning, which names
  the entry macOS lists, and the wizard's own notices), and the design's draft words. Tests now fail any
  of these texts that says "Mail Archive"; the Windows-only test was updated by hand and run on the
  Windows 11 lab VM.
- **O2. The command-line remedy is not a whole command.** Status at a terminal says "run the backup by
  hand with `mailarchive -thunderbird-sync`", without the archive folder. Recommendation: print the
  full command with this archive's folder, once someone checks which exact form runs the job (not
  checked in this review).
  **Applied by the lead, 2026-10-31, owner may revert.** Checked by reading `cmd/mailarchive` (the
  export flags, `parseJob`, the run-job verb) and by a dry parse in a test, never by running a backup.
  A backup needs its archive folder and its mail, so the whole command depends on how the schedule was
  made: for a Desktop- or wizard-made schedule it is `mailarchive run-job JOBFILE` — the very job the
  schedule runs, which holds the archive folder, its mail and the sync (the job file is the one this
  computer keeps for the archive, never one the archive itself names); for a command-line schedule it is
  the recorded job with this archive's folder as its `-out`, which already carries `-thunderbird-sync`.
  Both are shell-quoted. When neither is known (or a Windows path would need editing by hand), the
  advice says so: "run this archive's backup by hand, adding `-thunderbird-sync` to the command you back
  it up with; status cannot print that whole command for this archive". The partial
  `mailarchive -thunderbird-sync` is no longer printed by these two warnings. Left as they were: the
  four older run warnings (from the first pre-sync release) that also name `mailarchive
  -thunderbird-sync`; they are written once for every archive and cannot name its folder.
- **O3. The alert checkbox mentions Thunderbird on every archive.** "a failed backup, or Thunderbird
  not synced for 3 days (recommended)" also shows for a Microsoft 365 archive. Recommendation: keep it;
  it is true and short. **Kept as it is, by the lead, 2026-10-31, owner may revert.**
- **O4. The waiting note has four sentences.** Recommendation: keep them; each answers a question a
  person has during a 13-minute wait (how long, why, where Thunderbird is, how to stop early).
  **Kept as it is, by the lead, 2026-10-31, owner may revert.**
- **O5. "its mail settled after 6 min".** "Settled" is a slightly technical word in the end line.
  Recommendation: "its mail stopped arriving after 6 min". Not applied: many pinned lines, and it is
  wording taste. **Applied by the lead, 2026-10-31, owner may revert:** "Thunderbird synced: its mail
  stopped arriving after N min." (also the partly-watched variant). The exited-early line's "before its
  mail settled" is not this end line and was left as it is.
- **O6. "Thunderbird not synced: Thunderbird was not found".** The tag repeats the name.
  Recommendation: "…: it was not found". Tiny. **Applied by the lead, 2026-10-31, owner may revert:**
  "Thunderbird not synced: it was not found".
- **O7. Read the three new release-note bullets** before the release; they are in your voice.

## Not checked

- No live drill: no real Thunderbird was started and no page was opened in a browser; the words were
  read from code and from the notes the tests build. The live timing is the lab record's two runs.
- The email was not delivered to a phone; its body was read from the test's rendered message.
- Windows texts were read through the table override; the Windows lab VM ran the presync tests only,
  with no Thunderbird installed there.
- Slices AS9 (the Mac note's Run-now wording) and AS-Q (closing Thunderbird) are not built; their
  texts were not reviewed.

## Windows texts pass (§14 gate 3), 2026-10-31

A friction pass over the Windows Run now texts, with Windows' start on (its `AttendedStartAllowed`
entry set true after lab MA-918 run 2), on branch `presync-attended` at `8aff705`. The lead ruled
to apply every finding except the wait figure (finding 6). Each fix changes words only, except
finding 3, which also switches which notice the Settings card shows. Each was test-first: the pins
were updated to the new words (whole-string where they were partial, never looser), run red against
the old words, then green; the mutations are listed in each commit. The pinned Windows start code
(`TestWindowsCodePin`) was not touched.

| # | Finding | Severity | What was done | Commit |
|---|---|---|---|---|
| 1 | Run now's tooltip too long | medium | Applied. One short shape on both platforms. Windows: "Run this archive's backup now. With Thunderbird sync on, it first starts Thunderbird minimized (it won't take your typing), waits up to 20 minutes for its mail, then backs up and leaves Thunderbird open. A Thunderbird window that asks you something, such as for its Primary Password, can still appear and take the keyboard. Cancel skips the backup." macOS the same with "out of sight" and no window sentence. The encrypted tooltip keeps "The archive stays unlocked for this backup while it waits." and the Lock sentence in place of "Cancel skips the backup." The details stay in the "New:" line, the notes, the Settings notice and the guide. | `04fd73b` |
| 2 | "notification area" jargon; "minimized to the taskbar" wrong when Thunderbird hides to the tray (lab case E) | medium | Applied everywhere a person reads it: "minimized"; where to quit it, "its icon on the taskbar, or among the small icons by the clock" (one constant, `presync.WinQuitPlace`, with a comma after it before a following clause); the leftover line "still open, minimized (on the taskbar, or among the small icons by the clock)". The end lines, the quit-no-window warning, the Status remedies, the notes, the "New:" line, the cancel words, the Lock replies, the encrypted notice, the guide, the release line, the README, the `-thunderbird-sync` help and the Windows install note (its sentence moved to an untagged constant so the Linux gate pins it). | `53243e9` |
| 3 | Settings notice wrong for "Only when I click Run now" | medium | Applied. That choice shows the encrypted card's Run now notice in place of `SyncNoticeFor` (which speaks of asking Thunderbird to quit and of a scheduled backup's 2-minute idle rule), on a Mac and on Windows, and hides the Mac Automation note, which is about a scheduled backup closing Thunderbird. Where Run now does not start Thunderbird it says so instead. Both notices are drawn; the page switches them as How often changes. | `6431a52` |
| 4 | Start-blocked advice a dead end for unzipped installs, contradicted by the lab | low-medium | Applied on Windows: the guide's words, "start MailArchive Desktop from the Start menu, or, if MailArchive was unzipped, double-click mailarchivew.exe in its folder", and "(not from a terminal)" dropped. The Mac text was checked first: start-blocked comes only from Windows' launch, so a Mac never shows it, and its words are left as they were. The Status remedy, as before, adds no "then click Run now", so it holds where Windows' Run now does not sync (C-20). | `29506b3` |
| 5 | Settings notice silent on the Primary Password | low | Applied: Windows' notices (`SyncNoticeFor`, the encrypted notice, the Run-now-only notice) say "If Thunderbird has a Primary Password, its password window appears and takes the keyboard each time the sync starts Thunderbird." | `6431a52` |
| 6 | "often about 13" not measured on Windows | low | **Kept, not measured on Windows.** The figure rests on Thunderbird's own 10-minute check, which runs on every platform; the Mac lab measured it. | none |
| 7 | "sync while you watch" | low | Applied: "Click Run now on the Scheduled backup card to sync Thunderbird now."; the wizard's health view "To sync Thunderbird now and back up its mail, open MailArchive Desktop and click Run now on this archive's Scheduled backup card." | `29506b3` |
| 8 | Release notes bury the headline | low | Applied: the two Run now lines lead "New in 1.5.2", Mac then Windows, under "**Run now syncs Thunderbird first, on a Mac and on Windows.**"; MA-915's pins now hold each as a whole nested line and the block in order. | `cdcb89d` |

One test check was narrowed, by exactly one phrase, and is recorded here: the checks that a Desktop
text names no command fail any "mailarchive"; the guide's phrase names the file mailarchivew.exe to
double-click, so that exact phrase (`presync.WinStartDesktop`) is taken out before those checks, and
any other mention still fails them (shown by a mutation in `29506b3`).

Stale records from before the Windows flip got dated notes beside them: cell 14 above, and catalog
row MA-906's "may appear for a moment". Catalog rows amended with the new words: MA-861, MA-897,
MA-900, MA-906, MA-909, MA-911, MA-913, MA-914, MA-915, MA-922, MA-923, MA-928, MA-939, MA-945,
MA-949.

Not checked in this pass: no live Windows drill (the words were read from the code and the tests);
the Windows-only `TestPresyncInstallNoteWindows` was compiled (`GOOS=windows go vet`) but not run
here; its sentence is pinned on every platform by `TestTheWindowsScheduledStartNote`.
