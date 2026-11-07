# Friction review: plain words for how a backup ended

Walked 7 November 2026, 17:08–17:33 on the VM clock, on the Parallels VM "Windows 11" (ARM64; the
amd64 program runs emulated) and then on the Mac. Process: `../assurance-kit/process/friction-review.md`.
Feature: `docs/design-plain-failure-words.md` (catalog rows MA-1144 to MA-1150), its §7 list of
what to walk, and the UX contract's X10 as amended for it.

**Verdict: SHIPPABLE_WITH_NOTES.** All 14 walked cells worked. Each one now says what happened
before anything else, and none says "Failed" for a backup that saved everything else or was skipped.
The walk found three wording faults, all now fixed test-first in this commit (findings 1 to 3):

- the stuck-holder email said FAILED over words that say the backup was skipped;
- `status`, Status & health and the report said "the original is untouched" twice on each mistake line;
- BACKUP-NEEDS-ATTENTION.txt showed its command with every Windows backslash doubled.

Nine behaviour proposals follow. None blocks the release.

## Header

| | |
|---|---|
| Reviewed commit | a978971 (branch `plain-failure-words`, worktree clean) |
| Programs | `mailarchive.exe` and `mailarchivew.exe`, built from a978971 with the two `GOOS=windows GOARCH=amd64 go build` lines of `packaging/build-release.sh` (the second with `-ldflags -H=windowsgui`). There was also a **lab-only hook build** of the same tip, `mailarchive-hook.exe` / `mailarchivew-hook.exe` (`-tags pfwlab`). It came from a copy of the source with one extra file. That file sets the exporter seam the builder's tests use (`exporterSetup`), so a message whose page path holds `PFWLAB_PANIC`'s text panics while it is saved. No real message triggers the old picture-name panic since 1.5.9, so this was the only way to get a real program to hit a mistake in MailArchive. The hook file never entered the worktree. A second lab-only file installed the wizard's own run-job schedule (`installOffered`) without its dialogs |
| Own settings | Every program ran with `APPDATA` and `LOCALAPPDATA` pointed at `%LOCALAPPDATA%\pfwlab\home` and the Desktop with `-config C:\Users\Public\pfwlab\cfg\config.json`. The archive and test mail were in `C:\Users\Public\pfwlab`. The VM user has no MailArchive settings of their own (`%APPDATA%\mailarchive` does not exist), and nothing was written there. The one Task Scheduler entry (`mailarchive-5072e71b`, daily at 03:00, so it never fired) was removed at the end |
| Browser and input | Microsoft Edge. Keyboard first: buttons were reached by the card's address (`#sec-backup`) then Tab, and pressed with Enter. Where the keyboard focus had moved to another window, a real mouse click in the session was used (`ui.ps1`) |
| Mail | Made-up mbox files: an Inbox of 12 messages plus one with headers and no body (not downloaded), a 9,000-message folder, and a 150,000-message folder for the long backup. Later, a Receipts folder whose archive folder was blocked by a plain file (a real "could not be written"), and an Old pictures folder of two messages the hook build panics on |
| Holding the archive | A real long backup from a terminal (cell 1). For a holder that started a day ago, a PowerShell script took the same lock a backup takes (an exclusive byte-range lock at 2^62 on `.mailarchive.lock`) with the holder line `verb=export started=` 30 hours back (cell 8) |
| Email | A local SMTP sink on the VM (`127.0.0.1:2525`, no relay) kept each message as a file. No real mail was sent |
| Mac | The tip built natively. MailArchive Desktop ran in Chrome with a throwaway `HOME` and `-config` under `.claude/pfw-lab/mac` (never `~/Library/Application Support/mailarchive`). One archive got a partial run from the hook build with all three kinds |
| Evidence | 56 VM screenshots `00-vm-before.png` … `56-runnow-stuck.png` in `.claude/pfw-lab/shots/`, kept out of git. Also the sink's three emails, the job log, and the Mac page text |
| Baseline | `docs/scenario-catalog.md` SHA-256 828fa5fa…78227764, unchanged. No catalog row, gate, covers marker or baseline was changed. The new tests cite existing rows (MA-1144, MA-1146, MA-1147) |
| Cells walked | 14 (12 Windows, 2 Mac) |
| Functioning | 14 fully, 0 partly, 0 not |
| High-friction cells | 0 |

## The walk

Actor: the owner, a non-technical person using MailArchive Desktop. In cells 10 and 12 the actor is a
person at a console window. Grade is how well the words serve that person: **high** (clear, no change
needed), **med** (works, with a note), or **low** (misleads until fixed).

| # | Cell (actor × scenario) | Functions? | Grade | What the person saw, and friction | Type | Fix / backlog |
|---|---|---|---|---|---|---|
| 1 | Owner: **Run now** while a backup of the same archive runs from a terminal (150,000 messages) | Yes | high | 3 Tabs from the card, then Enter. The note said: "Another backup of this archive is already running (started 17:18 on 2026-11-07) — this backup was skipped; nothing is wrong. Try again when it finishes: until then it keeps this archive in use (its lock file, .mailarchive.lock)." (`25`). It leads with what is happening and does not say "failed". The lock file's name is the only technical word, and S25 requires it | — | — |
| 2 | Owner: **Recent backups** after the skip | Yes | high | No row for the skip, only the two earlier OK rows (`26`). Later, the stopped long backup shows as "Cancelled — stopped before it finished" (`34`) | — | — |
| 3 | Owner: **Run now** that finishes with one message not downloaded | Yes | med | "Backup finished: no new messages." The not-downloaded sentence follows, then "Which ones: the file attachments-report.tsv in the archive's folder (it opens in a spreadsheet program)." (`32`). Every OK row in Recent backups repeats the same 2-line sentence, 4 times in a row (`34`). The test message is a plain mbox message with headers and no body, yet the words say "your mail app (such as Thunderbird) has not downloaded it": that is only true for a mail app's own store | II | P6, P7 |
| 4 | Owner: **Run now** when one folder could not be written (a file sits where its archive folder must go) | Yes | high | "Not everything was saved. 1 folder could not be written to the archive — check that the archive location is writable and has space, then run again. Details: attachments-report.tsv in the archive folder." (`33`). There is no "Backup failed:" and no bare code | — | — |
| 5 | Owner: **Recent backups** and **Status & health** after cell 4 | Yes | med | The row shows "Incomplete" in amber, followed by the same words (`34`). Status & health shows "Last run: … → incomplete: …" and RED "the last backup (…) did not save everything: …" (`34`, `38`). The lines around it still use the older words: "Not captured: 1 folder(s) …" and "WARN: 1 folder(s) could not be captured …". The design kept them (§6), but they sit next to the new words | III | P4 |
| 6 | Owner: opens **BACKUP-NEEDS-ATTENTION.txt** in Notepad | Yes | low → fixed | "This email backup did not save everything." Then the archive, the kinds with their remedies, and the details line (`36`). Two problems. **`mailarchive status -out "C:\\Users\\Public\\pfwlab\\Archives\\Walk Archive"`**: every backslash doubled, because the path went through Go's `%q`. That is wrong to read, and a `\\nas\share` path would not work if pasted. The outright-failure notice has the same line. Also, "When: 2026-11-07T22:25:10Z" is UTC in ISO form, not the reader's time | III | **Finding 3, fixed.** P5 |
| 7 | Person at a console: **`mailarchive status`** after cell 4 | Yes | med | Exit 0. "Last run: … → incomplete: 1 folder could not be written …". "RED: the last backup (…) did not save everything: …" (`38`). Long lines wrap, but they read. The gap line says "(the original is untouched) (3 item(s) not read)" | — | P4 |
| 8 | Owner: **Run now** that meets a mistake in MailArchive (hook build) while the folder is still blocked: two kinds at once | Yes | med | 3 lines: "Not everything was saved. 2 messages could not be saved because of a mistake in MailArchive — your mail is fine (the original is untouched); please report it at https://github.com/alexdimarco/mail-archive-tool/issues. 1 folder could not be written to the archive — … Details: …" (`52`). Clear. The address is plain text: on the page it is not a link | II | P1 |
| 9 | Owner: **Recent backups** with several kinds, run twice | Yes | med | Each Incomplete row is 3 lines at 2000 px wide. Two runs in a row with the same causes give two identical 3-line rows (`54`). With hourly backups that becomes a page of the same text. Still readable: the date and "Incomplete" lead each row | II | P2 |
| 10 | Owner reads the **alert email** for that partial run (the scheduled job, `run-job`, as Task Scheduler runs it) | Yes | high | Subject "MailArchive backup did not save everything: C:\Users\Public\pfwlab\Archives\Walk Archive". Result line "DID NOT SAVE EVERYTHING", then the same words under "Reason:". After the cause was fixed: "MailArchive backups are working again" (sink files 1 and 2). The job's log says "backup alert email sent (failed)" for the partial run | III | P9 |
| 11 | Owner: **Run now** when the holder started 30 hours ago | Yes | med | "Another backup of this archive is already running (started 11:31 on 2026-11-06) — this backup was skipped. It started more than a day ago and still keeps this archive in use (its lock file, .mailarchive.lock), so it may be stuck: if it should have finished by now, restart the computer to end it; the next backup then runs as usual." (`56`). It says what to do. "Restart the computer" assumes the holder runs on this computer. For an archive on a shared drive, the holder may be another computer, and its name is in the lock | II | P3 |
| 12 | Owner reads the **alert email** for the stuck holder (the scheduled job) | Yes | low → fixed | Subject "MailArchive backup FAILED: …" and "MailArchive backup result: FAILED", over a Reason that begins "… this backup was skipped." The email contradicts itself, and it is the only place a scheduled stuck-skip is told (no history row is written for it) | III | **Finding 1, fixed** |
| 13 | Owner, Mac (Chrome): **Recent backups** and **Status & health** after a partial run with all three kinds | Yes | med | Same words as on Windows. Status & health printed "a mistake in MailArchive stopped it from saving this message (the original is untouched); your mail is fine (the original is untouched); please report it at …". The terminal WARN ended "… /issues (the original is untouched)". The same doubling is in the report row | III | **Finding 2, fixed** |
| 14 | Person at a Mac terminal: the command line's own result for that run | Yes | high | Exit 1, then "mailarchive: Not everything was saved. 2 messages … 1 folder … Details: …" after the unchanged "Not captured" block | — | — |

What was **not** walked:

- A real Thunderbird profile still downloading. The not-downloaded case used a plain mbox message with no body.
- The "already open" tag (MA-1150), because Thunderbird is not set up in this lab VM. Its words are covered by `TestTheAlreadyOpenTagSaysThunderbirdWasLeftAlone`.
- A Microsoft 365 skip, and Archive now on a Microsoft 365 archive (no lab mailbox).
- On the Mac, Run now and the notice in TextEdit. Run now needs an installed schedule, and on a Mac that installs a real LaunchAgent for the owner. The Mac notice was read as a file instead.
- An encrypted archive's partial run.

## Findings

1. **The stuck-holder email said FAILED over words that say the backup was skipped** (cell 12, low → fixed).
   The design keeps the behaviour: a holder of a day or more counts as a failure and is emailed once
   per streak. But the email is the only surface that tells a scheduled stuck-skip, and its subject
   and result line contradicted its reason. **Fixed:** the alert event carries `Busy` when the run's
   error is a busy refusal (`app.BusySkipped`). The email then reads, as subject, "MailArchive backup
   skipped, the archive is still in use: <archive>", and as result line, "SKIPPED (THE ARCHIVE IS STILL
   IN USE)". The reason keeps the run's own words. When it is sent is unchanged.
   Tests: `TestASkippedBackupEmailSaysItWasSkipped` (alert) and `TestASkippedRunsAlertIsMarkedBusy`
   (backupalert, a real busy refusal from `app.Run`).
2. **"The original is untouched" said twice on every mistake line** (cell 13, med → fixed). This is in
   `status`, in Status & health (the gap line and the WARN line) and in attachments-report.tsv. The
   builder made the mistake's remedy say "your mail is fine (the original is untouched)", but
   `GapOriginal` still added "(the original is untouched)" for `export-panic`. **Fixed:**
   `GapOriginal("export-panic", …)` is now empty, because its remedy already says it. `export-failed`
   and `folder-unwritable` keep their note. Tests: `TestAMistakeLineSaysTheOriginalIsUntouchedOnce`
   (health, with a positive twin for a write gap) and `TestTheReportsMistakeRowSaysTheOriginalIsUntouchedOnce`
   (app, a real run).
3. **BACKUP-NEEDS-ATTENTION.txt showed its command with doubled backslashes on Windows** (cell 6,
   low → fixed). Both notices, partial and outright failure, printed the folder through `%q`.
   **Fixed:** both name it as a person types it: on Windows in double quotes as it is, elsewhere in
   single quotes (`typedPath`). Test: `TestTheAttentionNoticeNamesTheFolderAsItIsTyped`, which checks
   both notices and the Windows, UNC and POSIX forms.

The owner's questions, as the walk sees them:

- **Q1 (is a busy skip a failure?)** The recommendation reads right on every surface. The Desktop
  never says "failed". With finding 1 fixed, the stuck holder's email no longer does either.
- **Q2 (where to report)** https://github.com/alexdimarco/mail-archive-tool/issues is the right address:
  on 2026-11-07 `gh repo view` showed the repository PUBLIC with issues enabled. It is not clickable
  on the Desktop (P1).
- **Q3 ("Incomplete")** It reads right. The amber "Incomplete" sits well apart from a red "Failed" and
  a green "OK", and it matches status's "→ incomplete". Keep it.

## Proposals (behaviour; not built)

- **P1.** Make the report address a link on the Desktop: the card's note and the Recent backups row.
  The email and the notice stay plain text, because mail apps link it themselves.
- **P2.** Recent backups: when consecutive runs ended the same way, show the words once with a count
  ("Incomplete ×3 since 5:25 PM"), or give the remedy only on the newest row. Hourly backups with a
  standing cause otherwise fill the card with the same 3 lines.
- **P3.** A holder that may be stuck: when the lock's `host=` is not this computer, name that computer
  ("restart ALEX-PC, where it runs") instead of "restart the computer". The holder line is untrusted,
  so show it sanitized, as `HolderVerb` already does.
- **P4.** Carry the plain words into status's gap lines: "Not captured: 2 item(s) and 1 folder(s)" and
  "WARN: 1 folder(s) could not be captured …". These now sit next to "did not save everything" and
  still use the old style.
- **P5.** The notices' "When:" line: give local time with its offset, as the email does
  ("2026-11-07 17:25 -05"), not UTC in ISO form.
- **P6.** Run now's note for a partial run leaves out the not-downloaded sentence that an OK run's note
  gives. Add it after the kinds' words.
- **P7.** The not-downloaded sentence for a source that is a plain mbox or export file, not a mail
  app's own store. There "your mail app has not downloaded it" may be untrue (the message may simply
  have no body). Word it by the source's kind, or say it only for a Thunderbird or IMAP-cache store.
- **P8.** The other commands `status` and Status & health print with `%q` (verify, reindex, schedule,
  serve) have the same doubled backslashes on Windows (seen in the Fixity coverage line, `38`). This
  was there before this branch. Use the same `typedPath`-style quoting there.
- **P9.** The job log's "backup alert email sent (failed)" for a partial or skipped run: name what
  was sent ("did not save everything", "skipped").

A lab note, not a product finding. A Desktop started with `-config` keeps its alert settings beside
that file, but a scheduled `run-job` reads them from the default settings folder
(`backupalert.DefaultSettingsPath`). So the lab wrote the run-job's alert settings into the lab's
`APPDATA` by hand. In normal use (no `-config`) the two are the same file.

## Fixes made in this commit (prove-fail → prove-pass)

Each mutation was applied alone; its covering test went red; the file was restored; the test went green
(`.claude/pfw-lab/tools/mutate.py`):

| | Mutation | Red |
|---|---|---|
| F1a | `GapOriginal("export-panic")` says "the original is untouched" again | `TestAMistakeLineSaysTheOriginalIsUntouchedOnce` |
| F1b | the same | `TestTheReportsMistakeRowSaysTheOriginalIsUntouchedOnce` |
| F1c | the `export-panic` case removed (another note about the original returns) | `TestAMistakeLineSaysTheOriginalIsUntouchedOnce` |
| F2a | the email ignores `Busy` | `TestASkippedBackupEmailSaysItWasSkipped` |
| F2b | the alert event never marked `Busy` | `TestASkippedRunsAlertIsMarkedBusy` |
| F3a | `typedPath` doubles Windows backslashes | `TestTheAttentionNoticeNamesTheFolderAsItIsTyped` |
| F3b | the partial notice back to `%q` | the same |
| F3c | the failure notice back to `%q` | the same |

No existing test was changed. The runs and their results are in the commit message.
