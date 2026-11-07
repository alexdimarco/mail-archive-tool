# Plain words for how a backup ended

Design note, 2026-11-07, branch `plain-failure-words` (off main 9073b1f, v1.6.0). The lead's words
under the owner's delegation of 2026-11-02: say what happened first, then name the remedy. Catalog
rows MA-1144..MA-1150. Owes the friction review (§7) before it ships.

## 1. Why

The owner, 2026-11-05: "It seems backups are failing but it isnt providing information as to why or
how to fix". Their screen showed:

- Recent backups: `Nov 5, 2026 5:04 PM — Failed — partial: 4 item(s) not captured — see
  attachments-report.tsv — Thunderbird was already open`.
- The Scheduled backup card, after Run now: `Backup failed: another backup or check is using this
  archive (started 20:00 on 2026-11-05) — it is in use until that finishes, holding its lock file
  .mailarchive.lock; try again when it finishes` while the 20:00 backup was simply still running.

The 4 items were `export-panic` (a MailArchive bug, fixed in 1.5.9). The only explanation ("this is a
mistake in mailarchive, not in your mail … please report it") was in a row of the TSV file.

Rules for the new words:

1. Say what happened first; name the remedy; the remedy text is the same on every surface.
2. A partial run gives its counts by kind in plain words. The TSV is named only as "Details:",
   never as the only explanation. No bare code ("item(s)", "partial:", a reason name).
3. A backup refused because another run holds the archive is a **skip**, not a failure.
4. Machine-readable codes stay as they are: add fields, rename nothing.

## 2. Kinds of what a partial run could not save

A run is partial when it observed a fillable or terminal capture gap, or wrote a partly-read
record (§4.1 of design-fault-isolation; unchanged). Each gap the run observed gets one kind, by its
reason and class (`state.PartialKindOf`):

| Kind (`partial` key) | Gaps | Words (one sentence each, most serious first) | Remedy (same everywhere) |
|---|---|---|---|
| `mailarchive_mistake` | `export-panic` | "N messages could not be saved because of a mistake in MailArchive — …" | "your mail is fine (the original is untouched); please report it at https://github.com/alexdimarco/mail-archive-tool/issues" (also the gap table's remedy, so the TSV row and `status`'s gap line say it) |
| `archive_write` | `export-failed`, `folder-unwritable` | "N messages could not be written to the archive — …" | the gap table's own: "check that the archive location is writable and has space, then run again" |
| `source_unreadable` | any other terminal gap | "N messages cannot be read from your mail source as it stands (for example a damaged file) — …" | "Status & health in MailArchive Desktop, or `mailarchive status`, says what to do for each" |
| `retried` | any other fillable gap | "N messages could not be read from your mail this time — …" | "each backup tries them again; if they keep failing, Status & health … says what to do" |
| `other` | a gap a summary counted under no kind | "N messages could not be saved — …" | as `source_unreadable` |
| `partly_read` | partly-read records | "N messages could only be partly read from your mail — …" | "each backup tries them again" |
| (none) | info gaps | not said: not a problem | — |

Counts say messages and folders ("2 messages and 1 folder"); an overflow entry counts the items it
stands for. The whole text leads with "Not everything was saved." and ends with "Details:
attachments-report.tsv in the archive folder." (an encrypted archive: "Open the archive in
MailArchive to see which.").

Messages that are in the archive but not downloaded in full yet (fillable records such as
`missing-body`, `corrupt-part`, `empty-attachment` not yet delivered — Thunderbird still
downloading) do **not** make a run partial (unchanged). They get one sentence
(`state.NotDownloadedSentence`): "420 messages are not on this computer in full yet: your mail app
(such as Thunderbird) has not downloaded them. Later backups add them once it has; opening them in
your mail app makes it download them sooner."

## 3. Inventory: before → after

| Surface | Partial run | Busy (another run holds the archive) | Thunderbird sync | Real failure |
|---|---|---|---|---|
| Recent backups row | `Failed — partial: 4 item(s) not captured — see attachments-report.tsv` → `Incomplete — 4 messages could not be saved because of a mistake in MailArchive — your mail is fine (the original is untouched); please report it at …. Details: attachments-report.tsv in the archive folder.` A finished run's row adds the not-downloaded sentence. An older entry keeps its own text. | not a row (no record is written under another run's lock) — unchanged | `Thunderbird was already open` → `Thunderbird was already open, so the backup left it alone`; the other tags unchanged (friction-reviewed 2026-10-31) | `Failed — <reason>` unchanged |
| Scheduled backup card (Run now's note) | `Backup failed: partial: …` → `Not everything was saved. 4 messages could not be saved because of a mistake in MailArchive — …` then the sync's line (the Desktop ends a partial job with its words as the result, not as an error) | `Backup failed: another backup or check is using this archive …` → `Another backup of this archive is already running (started 20:00 on 2026-11-05) — this backup was skipped; nothing is wrong. Try again when it finishes: until then it keeps this archive in use (its lock file, .mailarchive.lock).` | the sync's line, unchanged | unchanged |
| Archive now / a new archive's first backup (Desktop) | `It didn't finish: partial: …` / `Its first backup did not finish: partial: …` → the kinds' words (`Its first backup finished: Not everything was saved. …`) | `It didn't finish: …` / `Its first backup did not finish: …` → the skip's words | — | unchanged |
| Status & health and `mailarchive status` | `the last run (…) FAILED: partial: …` → `the last backup (…) did not save everything: <kinds' words>`; Last run line `→ failed: …` → `→ incomplete: …`; reason code `run_failed` and RED kept | the busy refusal is never recorded — unchanged | the presync WARNs (what/why/do, X16) unchanged | `FAILED: <reason>` unchanged |
| `status -json` | text in `last_run.error` changes as above; **added** `last_run.partial` (`{mailarchive_mistake:{messages,folders}, …, partly_read}`); `reason_codes`, `status`, the `gaps_*` counts unchanged | — | unchanged | unchanged |
| Alert email | subject `MailArchive backup FAILED: …` → `MailArchive backup did not save everything: …`; result line `FAILED` → `DID NOT SAVE EVERYTHING`; reason = the kinds' words. Sent exactly as before. | a scheduled skip under a day is quiet (no email, unchanged); a holder of a day or more is alerted as before, now with the "may be stuck … restart the computer" words | the 3-day email unchanged | unchanged |
| BACKUP-NEEDS-ATTENTION.txt | `This email backup FAILED and needs your attention. … Reason: partial: …` → `This email backup did not save everything.` … `Everything else it found was saved …` then each kind with its remedy, then Details | not written (no record) | — | unchanged |
| Command line (stderr) | `mailarchive: partial: … see attachments-report.tsv` → `mailarchive: Not everything was saved. …` (the "Not captured" block above it unchanged); exit 1 unchanged | the skip's words; exit 1 unchanged (X1) | — | unchanged |
| `mailarchive.log` of a scheduled run-job | `FAILED: partial: …` → the kinds' words, no `FAILED:` (as the command line's own log already did) | the quiet skip line, unchanged | — | unchanged |
| Run-history record (`.mailarchive-runhistory.json`) | `error` text as above; **added** `partial` and `fillable` | — | `presync` unchanged | unchanged |
| Last-run record (`.mailarchive-lastrun.json`) | `error` text as above; **added** `partial`; `status: failed` and every count kept | — | unchanged | unchanged |
| attachments-report.tsv | export-panic row's why `mailarchive failed while archiving this message` → `a mistake in MailArchive stopped it from saving this message`; what to do → the mistake remedy above | — | — | — |

Encrypted archives keep their allow-lists: the record and history hold the failure kind only, so
their words stay the kind's fixed sentence ("some messages could not be backed up completely") and no
`partial` object is written beside them. The in-memory error text an encrypted run returns gets the
kinds' words with "Open the archive in MailArchive to see which."

## 4. The busy skip

`busyRefusal` (the only refusal Run and RunGraph give for a held lock) now says what is running from
the lock's verb, since when, "this backup was skipped; nothing is wrong", "Try again when it
finishes", and names the lock file (S25 and MA-85 require the lock file and the holder named). A
holder that started a day or more ago may be stuck: "It started more than a day ago and still keeps
this archive in use (its lock file, .mailarchive.lock), so it may be stuck: if it should have
finished by now, restart the computer to end it; the next backup then runs as usual." Every code is
unchanged: `lockfile.ErrHeld`, the `archive_busy` kind, exit 1, MA-859's scheduled quiet skip.

MailArchive Desktop ends such a job as a skip: `/api/activity` carries the words as `busy` (added),
with no `error` and no `result`; Run now's note is the words alone; the page script shows `busy`
before any failure text in Archive now, Run now and a new archive's first backup.

## 5. Owner questions (the recommended answer is built)

- **Q1 — does a busy skip count as a failure?** Today, before this branch: a *scheduled* backup that
  meets a holder that started under a day ago is already a quiet skip — no failure, no alert, no
  streak, no history entry (design-desktop-settings-card 3.7, R3C-12, MA-859). A holder of a day or
  more counts as a failure (it may be stuck) and is alerted once per streak. A Run now / Archive now
  that meets the lock writes no record and sends no alert. **Recommendation: keep exactly that — a
  skip is not a failure; only a possibly-stuck holder (a day or more) is.** Built: only the words
  change (the skip is never called "Failed" on the Desktop, and the stuck case says what to do).
- **Q2 — where does a person report a MailArchive mistake?** **Recommendation: the project's GitHub
  issues, https://github.com/alexdimarco/mail-archive-tool/issues**, since the project goes public.
  Built as one constant (`state.ReportAddress`); changing it changes every surface.
- **Q3 — call a partial run "Incomplete" instead of "Failed"?** **Recommendation: yes on the words
  (Recent backups "Incomplete", status "did not save everything", the email subject), no on the
  behaviour**: the record's status stays `failed`, `status` stays RED with `run_failed`, the alert
  streak and the exit code are unchanged (the S1a mapping). Built, including the Desktop's job slot:
  a partial Run now, Archive now or first backup ends with its words as the result, so the card says
  "Not everything was saved. …", never "Backup failed:" or "It didn't finish:".

## 6. Not changed (residuals)

- The OS notice after a failed scheduled run ("the scheduled backup of X failed. Open MailArchive
  Desktop to see why.") is per streak and still says "failed" for a partial run.
- `status`'s completeness lines ("Incomplete: N still missing content …") are the archive's state,
  not a backup's outcome; their words are unchanged.
- The keyed verbs' own busy refusal (`refuseBusy`, encrypted archives' key verbs) is not a backup
  and keeps its words.
- The encrypted archive's fixed failure sentences are unchanged (no counts beside an encrypted
  archive, P2a-C16).

## 7. What the friction review walks

Windows first on Edge, then macOS: a Run now while a scheduled backup runs (the card's note; then
Recent backups shows no row for the skip); a partial run (Recent backups row, the card's note,
Status & health, the email, BACKUP-NEEDS-ATTENTION.txt opened in Notepad/TextEdit); a finished run
with messages Thunderbird has not downloaded; the "already open" tag; whether the report address
reads right; whether "Incomplete" is the right label (Q3); the stuck-holder words (a day old lock)
in the email; the length of a Recent backups row with several kinds.
