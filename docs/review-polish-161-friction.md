# Friction review: the 1.6.1 polish pass

Walked 7 November 2026, 21:12–21:20 on the VM clock, on the Parallels VM "Windows 11" (ARM64; the
amd64 program runs emulated), in Microsoft Edge. Process: `../assurance-kit/process/friction-review.md`.
Feature: the nine proposals of `docs/review-plain-failure-words-friction.md` (P1 to P9), built as
catalog rows MA-1151 to MA-1159. The walk covers items 1, 2, 3, 6 and 7 (the brief); items 4, 5 and 8
were read on the same run's `status` output and notice; item 9 (the job log's alert line) needs a
mail server and was left to its test.

**Verdict: SHIPPABLE_WITH_NOTES.** All 9 walked cells worked. The report address is a real link that
opens in a new tab; repeated runs are one row; a stuck holder on another computer is named by it; a
partial Run now says what is not downloaded; a plain mail file is no longer said as "your mail app
has not downloaded it". The walk found no fault that blocks the release. It left four notes:
N1 is fixed in this commit, N2 to N4 are not built.

## Header

| | |
|---|---|
| Reviewed tree | branch `polish-161` (off main 0923ffa, v1.6.1), the working tree of this commit before two later changes in it: N1's fix (below) and a rewrite of Run now's end-of-run line in the page script that keeps the form its older tests pin (`n.textContent=d.note||…` first, then `sayText`); the words and the link it shows are the same |
| Programs | `mailarchive.exe`, `mailarchivew.exe` (`-ldflags -H=windowsgui`) and a **lab-only hook build** `mailarchivew-hook.exe` (`-tags pfwlab`), built with `GOOS=windows GOARCH=amd64` from a copy of the tree plus the lab-only files the 1.6.1 walk used (`zz_pfwlab_hook.go`: a message whose page path holds `PFWLAB_PANIC`'s text panics while it is saved; `zz_pfwlab_job.go` and `cmd/pfwlabjob`: install the wizard's own run-job schedule without its dialogs). The lab files never entered the worktree |
| Own settings | Every program ran with `APPDATA` and `LOCALAPPDATA` pointed at `%LOCALAPPDATA%\polishlab\home`, and the Desktop with `-config C:\Users\Public\polishlab\cfg\config.json`. The archive and the test mail were in `C:\Users\Public\polishlab`. Nothing was written to the VM user's own MailArchive settings. The one Task Scheduler entry (`mailarchive-4a1d9f1e`, daily at 03:00, so it never fired) was removed at the end, and the walk's Desktop stopped |
| Browser and input | Microsoft Edge, already open in the VM user's session (its other tabs left alone). Real mouse clicks and keys in the session (`P:\pfw\ui.ps1`) |
| Mail | Made-up mbox files: an Inbox of 8 messages plus one with headers and no body (a plain mail file that holds only part of it); later a Receipts folder whose archive folder a plain file blocks (a real "could not be written"), and an Old pictures folder of two messages the hook build panics on |
| Holding the archive | A PowerShell script took the same lock a backup takes (an exclusive byte-range lock at 2^62 on `.mailarchive.lock`) with the holder line `verb=export started=` 30 hours back and `host=OFFICE-PC` (another computer), then `host=alexdimarco414d.home` (this computer, by a DNS name in another case) |
| Evidence | 24 VM screenshots `01-desktop-open.png` … `24-runnow-stuck-here.png` in `.claude/polish-lab/`, kept out of git; the archive's `.mailarchive-runhistory.json`, BACKUP-NEEDS-ATTENTION.txt and `mailarchive status` output read in the VM |
| Cells walked | 9 (Windows) |
| Functioning | 9 fully, 0 partly, 0 not |
| High-friction cells | 0 |

## The walk

Actor: the owner, a non-technical person using MailArchive Desktop; in cell 9 a person at a
console. Grade: **high** (clear, no change needed), **med** (works, with a note), **low** (misleads
until fixed).

| # | Cell (actor × scenario) | Item | Functions? | Grade | What the person saw, and friction | Fix / note |
|---|---|---|---|---|---|---|
| 1 | Owner: **Run now** on a plain mail file holding one message with no body | 7 | Yes | high | "Backup finished: no new messages. 1 message is not in the archive in full: the mail file holds only part of this message; if it is a copy, copy it again from its mail program. Later backups add it once the file holds it in full. Which ones: …" (`16`). No mail app is blamed. The history records `fillable_from: mail_file` | — |
| 2 | Owner: **Run now** again, then **Recent backups** | 2, 7 | Yes | high | The two finished runs are one row: "Nov 7, 2026 9:16 PM — OK (2 times, last at 9:17 PM) — no new mail · 1 message is not in the archive in full: the mail file holds only part …" (`22`). The first backup (9 messages) keeps its own row | — |
| 3 | Owner: **Run now** when a folder cannot be written and two messages meet a mistake in MailArchive | 1, 6 | Yes | high | "Not everything was saved. 2 messages could not be saved because of a mistake in MailArchive — … please report it at **https://github.com/alexdimarco/mail-archive-tool/issues** [a link]. 1 folder could not be written … Details: …", then on its own line the not-downloaded sentence (`17`). P6's sentence is there | — |
| 4 | Owner: clicks the address in Run now's note | 1 | Yes | high | GitHub's Issues page of the project opened in a **new tab**; the Desktop tab stayed as it was (`18`). The page's opener is not passed (`rel="noopener noreferrer"`) | — |
| 5 | Owner: a second partial **Run now**, then **Recent backups** | 1, 2 | Yes | med | "Nov 7, 2026 9:17 PM — Incomplete (2 times, last at 9:17 PM) — 2 messages … please report it at [link] …" (`22`). One row, the address a link. Both runs ended in the same minute, so "last at 9:17 PM" repeats the row's own time | N1, fixed |
| 6 | Owner: **Status & health** after the partial runs | 1, 4, 8 | Yes | high | Both report addresses are links (`21`). "Not captured: 2 messages and 1 folder from the source" — no "item(s)". The Fixity line names the folder as typed: `mailarchive verify -out "C:\Users\Public\polishlab\Archives\Polish Archive"` | N3 |
| 7 | Owner: **Run now** while a backup started 30 hours ago on **another computer** (OFFICE-PC) holds the archive | 3 | Yes | high | "Another backup of this archive is already running (started 15:18 on 2026-11-06) — this backup was skipped. It started more than a day ago on another computer, OFFICE-PC, and still keeps this archive in use (its lock file, .mailarchive.lock), so it may be stuck: if it should have finished by now, restart the computer OFFICE-PC to end it (restarting this one does not); the next backup then runs as usual." (`23`) | — |
| 8 | Owner: the same, the holder on **this computer** (named by its DNS name, in lower case) | 3 | Yes | high | The words as in 1.6.1: "… so it may be stuck: if it should have finished by now, restart the computer to end it; …" (`24`). No computer named | — |
| 9 | Person at a console: **`mailarchive status`**, and **BACKUP-NEEDS-ATTENTION.txt** read in the VM | 4, 5, 8 | Yes | med | `status`: "WARN: 1 folder could not be captured on the last run; it is retried …", "WARN: 2 messages are NOT in this archive …", the folder's line "(3 messages not read)". The notice: "When:    2026-11-07 21:17:39 -05:00" and `mailarchive status -out "C:\Users\Public\polishlab\Archives\Polish Archive"`. Beside them, `status` still says "WARN: 1 message(s) still missing content — download for offline use in your mail app" for a plain mail file | N2, N3 |

What was **not** walked:

- Item 9 (the job log's alert line): it needs an alert email sent by a scheduled run-job; covered by
  `TestTheJobLogNamesTheAlertThatWasSent` (a real busy refusal), not seen in a real log.
- A real Thunderbird store (the Thunderbird wording) and a Microsoft 365 archive: no Thunderbird or
  lab mailbox in this VM. Their words are covered by `TestTheNotDownloadedWordsSayWhereTheMailCameFrom`
  and `TestARunRecordsWhereItReadItsMail` (a folder under a Thunderbird profile, a real run).
- A drive-root archive (`E:\`): no second drive in the VM; covered by
  `TestStatusNamesAWindowsFolderAsItIsTyped` and `TestANoticeNamesADriveRootSoItReadsBack`.
- The Mac: the Desktop's page and words are the same code; the Mac ran the full test suite.

## Notes

- **N1 (fixed in this commit, test-first).** A repeated row said "(2 times, last at 9:17 PM)" even
  when the last run ended in the same minute as the row's own time (cell 5, seen because the lab ran
  backups seconds apart). It now says "(2 times)" then. Test: the same-minute case of
  `TestRecentBackupsTellsRepeatedRunsOnce` (red before the fix, green after; mutation M2e).
- **N2.** `status`'s and Status & health's `incomplete_content` line still says "N message(s) still
  missing content — download for offline use in your mail app" for a plain mail file (cell 9). It is
  the archive's state, not a backup's outcome, so P7 did not reach it; worded by `fillable_from` it
  would agree with Run now and Recent backups.
- **N3.** The capture-gap lines use the outcome words, but other `status` lines still say "(s)"
  ("message(s)", "file(s)"), and a folder's gap line reads "(the original is untouched) (3 messages
  not read)", two brackets in a row.
- **N4.** An Incomplete row in Recent backups does not repeat the not-downloaded sentence that an OK
  row and Run now's note give (cells 2, 3, 5). The record has it (`fillable`, `fillable_from`).
- Seen beside the walk, not in it: the backup's own command-line summary still prints `Search:
  mailarchive serve -out "C:\\Users\\…"` with doubled backslashes (the run's summary, not `status`;
  pre-existing, `cmd/mailarchive/main.go`), as does the wizard's own summary dialog.
