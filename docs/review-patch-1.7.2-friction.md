# Friction review: the 1.7.2 patch (a backup whose folder changes while it runs)

Walked 14 November 2026, 10:25 to 10:50 Mac time, on the Mac (native), in a throw-away Linux
container (`golang:1.26`), and on the Parallels VM "Windows 11" as SYSTEM (nobody is signed in to
it). Process: `../assurance-kit/process/friction-review.md`. Scope: branch `patch-1.7.2` at
`c71467d` against shipped v1.7.1 (`1a6d437`), `git diff 1a6d437..c71467d`; catalog rows MA-1328
to MA-1330, MA-1336 to MA-1338, MA-1440 to MA-1443 and MA-1427; the UX contract's X1, X2 and X8;
the owner's standing rule that every backup outcome says why and how to fix it in plain words.

**Verdict: HIGH_FRICTION_NOT_SHIPPABLE as worded; SHIPPABLE once F1 and F2 are folded.** The
guard works: in every swap cell the run left the other archive's records, run history, pages,
README and index exactly as they were (only new message files landed there, as the notes say),
and in every Mac and Linux cell where the archive's own lock file was deleted or written over, the
failure was recorded in that archive with its run history entry, BACKUP-NEEDS-ATTENTION.txt and
email. What fails the review is the words. The sentence a person reads about this failure, on
every surface and in the email, is the lock's internal sentence: "lock …/.mailarchive.lock was
replaced during the run: stopping so two runs cannot overlap; run again". It names a hidden file,
blames a second backup, and does not say to put the right drive back. The one plain sentence the
patch adds (the "no longer holds this backup's lock" warning) reaches only the terminal, the run
log and Run now's live log, never the email or Status. And the release notes overstate two things
and quote the less common words (F2). Both are text changes (Type III); no gate or behaviour needs
to change. F1 keeps the lock's sentence after "Details:", so a test that looks for it still finds
it; a test that pins the whole sentence changes with it.

## Header

| | |
|---|---|
| Reviewed commit | `c71467d` (branch `patch-1.7.2`; worktree `wip/patch-1.7.2-fric`, clean but for its untracked `.claude/`) |
| Programs | `mailarchive` built from `c71467d` for macOS arm64, Linux arm64 (container) and Windows amd64 (VM, emulated). Two lab-only helpers, never committed: `fric172lab` (installs the wizard's run-job as "Only when I click Run now", from a build-tagged file that was deleted from the worktree right after the build) and `swapdirs` (exchanges two folders in one atomic step, macOS `renamex_np RENAME_SWAP`); plus a loopback SMTP sink (`fakesmtp`, 127.0.0.1:2525, no relay) |
| Own settings | Mac: `HOME` and `-config` under `.claude/testdata/fric172/mac/` in the worktree; the Desktop ran with a PATH shim for `osascript` that kept the one-time link, so no browser, dialog or notice opened; it was driven with `curl` only and shut down from its own API. Linux: everything inside the container's `/work`. Windows: `C:\fric172lab` and drive letter `W:` (a `subst`, removed at the end); SYSTEM can read `\\Mac\winlab` but not write it, so results stayed in the VM and were read back with `type` |
| Email | Only in the Linux container, where no desktop notice can show (a scheduled failure raises one on a Mac). The sink kept 2 messages; no real mail was sent |
| Mail | Made-up mbox files: source A grown from 2,000 to 21,000 messages across the cells (40,000 on Windows), source B 30 messages |
| How the folder was changed | Mac command line and Linux: `mv` A away, `mv` B in (a gap of a few milliseconds, like unplugging one drive and plugging another). Mac Desktop: the same, then `swapdirs` (no gap). Windows: `subst W: /d`, then `subst W:` to B's folder, as MA-1328's Windows test does |
| Baseline | `docs/scenario-catalog.md` SHA-256 `137dc9b4…08502c0`, unchanged. No catalog row, gate, covers marker, test or product file was changed |
| Cells walked | 12 (Mac 8, Linux 2, Windows 2) |
| Functioning | 11 fully, 1 partly (cell 10: nothing recorded, but the run made the vanished folder again, F5), 0 not |
| High-friction cells | 9 (every cell that ends on the lock's words or on "never finished") |

## The walk

Actor: a person with two USB drives used in turn, not technical, often reading on a phone. Grade
is how well the words serve that person: **high** (clear), **med** (works, with a note), **low**
(misleads or leaves them stuck). Paths are shortened to `…/drive/Archive` here; the program prints
them in full.

| # | Cell | Functions? | What the person sees | Grade | Findings |
|---|---|---|---|---|---|
| 1 | Mac, terminal: A's backup writing new mail; A moved away, B moved into its path | yes: B's 7 top files byte-for-byte unchanged (38 files before and after); exit 1 | `warning: …/drive/Archive no longer holds this backup's lock (its drive was changed, the folder was moved, or another backup took it over); nothing was recorded in it` then, twice (the log line and the error), `the archive location …/drive/Archive is unavailable (lstat …/drive/Archive: no such file or directory) — reconnect the drive or share and re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.` | med | F4, F2b |
| 2 | Mac, terminal with `-log` (what a scheduled job's log holds) | yes: B unchanged (its lock and index side files had been rewritten earlier by `status`, F10, not by this run) | the run log: the same warning, then `FAILED: the archive location … is unavailable …` | med | F4 |
| 3 | Mac, A put back after cell 1 | yes | `status`: `Last run: … → never finished` and `RED: the run started 2026-11-14 10:28 EST never finished (pid 67050 is gone: crash, kill, or power loss) — check the log and run again`; `status -json`: `run_never_finished`. The next backup: `swept orphan: …` then `Done.`; status ok | low | F3 |
| 4 | Mac Desktop, Run now; A and B exchanged in one step (no gap) mid-walk | yes: B's records, pages, README, index unchanged; **940 of A's new message files** landed in B's folder in the seconds before the run noticed; `verify` on B: `attested`, `unexpected=940` | Run now's note: `Backup failed: lock …/drive/Archive/.mailarchive.lock was replaced during the run: stopping so two runs cannot overlap; run again`; its live log, under the note, shows the warning of cell 1. With B at the path, Status & health and Recent backups show B's own good backup and nothing about the stop. With A back: RED "never finished … crash, kill, or power loss"; Recent backups shows no row for the stopped run | low | F1, F2a, F3, F8 |
| 5 | Linux, the scheduled job (`run-job`) with email alerts on; A moved away, B moved in, no write in flight | yes: B unchanged; B gained a message folder of A's | email subject `MailArchive backup FAILED: /work/drive/Archive`; body `Reason:` `lock /work/drive/Archive/.mailarchive.lock was replaced during the run: stopping so two runs cannot overlap; run again`. A's `mailarchive.log` (it moved with A): the warning, then `FAILED: lock … was replaced …`, then `backup alert email sent (failed)`. The scheduler's stderr: `mailarchive run-job: lock … replaced …` | low | F1, F4 |
| 6 | Mac, terminal: A's own lock file deleted mid-run (K3) | yes: recorded failed in A (last-run record, run history, BACKUP-NEEDS-ATTENTION.txt); exit 1 | terminal, `status`, the notice file: `lock …/drive/Archive/.mailarchive.lock was removed during the run (open …/drive/Archive/.mailarchive.lock: no such file or directory): stopping so two runs cannot overlap; run again` | low | F1, F6 |
| 7 | Mac, terminal: A's own lock file written over mid-run (K3b) | yes: recorded failed in A | `lock … was replaced during the run: stopping so two runs cannot overlap; run again` (terminal, `status`, BACKUP-NEEDS-ATTENTION.txt) | low | F1 |
| 8 | Linux, scheduled job, A's own lock file deleted, a new alert streak | yes: recorded failed in A; `status -json` `run_failed`, `last_run.error` the lock's sentence | email `Reason:` `lock … was removed during the run (open …: no such file or directory): stopping so two runs cannot overlap; run again` | low | F1, F6 |
| 9 | Mac Desktop, Run now; A's own lock file deleted | yes: recorded in A | Run now's note `Backup failed: lock … removed …`; Recent backups `Failed — lock … removed …`; Status & health RED with the same sentence | low | F1, F6 |
| 10 | Mac, terminal: A's folder moved away, nothing put in its place, its parent folder writable | partly: nothing recorded anywhere, but **the run made the folder again** at the old path and wrote 366 message files into it before its proof stopped it; then `status` there said `no archive at …` | `lock …/drive/Archive/.mailarchive.lock was removed during the run (open …: no such file or directory): stopping so two runs cannot overlap; run again` — not the "unavailable … reconnect the drive" the release notes promise | low | F5, F2c |
| 11 | Windows, terminal (SYSTEM): `W:` moved from A's folder to B's mid-run | yes: B's 38 files unchanged; exit 1 after 7.6 s | the warning of cell 1 with `W:\Archive`, then twice `the archive location W:\Archive is unavailable (GetFileAttributesEx W:\Archive: The system cannot find the path specified.) — reconnect the drive or share and re-run. Messages written before this point are kept; …`. With A back at `W:`: `RED: the run started 2026-11-14 10:42 -05 never finished (pid 5512 is gone: crash, kill, or power loss) — check the log and run again`, and `3000 in manifest · 3366 indexed` until its next backup | med, then low | F3, F4, F2 |
| 12 | Windows, terminal (SYSTEM): A's own lock file, a delete tried, then a write, mid Full run of 40,000 messages | yes: Windows refused both (`The process cannot access the file 'W:\Archive\.mailarchive.lock' because it is being used by another process.`), so the run went on and ended ok | nothing: the backup's normal `Done.` | high | F2 (the K3 bullet is about Mac and Linux in practice) |

A lab note on cell 10's twin: the first Desktop Run now (moving A away and B in with two `mv`s)
landed its write in the gap: the run made `…/drive/Archive` again, so the second `mv` put B
*inside* it. The Desktop said `Backup failed: lock … removed …` and Status & health said the
folder held no archive yet. That is cell 10's mechanism again; cell 4 repeats the swap without a
gap.

## Findings

Severity is for the person, not the code. Type is the process's: III is buildable-away wording or
legibility, II a design choice.

### F1 — HIGH (III): the failure reason is the lock's internal sentence, on every surface

**Seen:** cells 4 to 10, Mac and Linux. Terminal, run log, Run now's note, Recent backups,
Status & health, `status`, `status -json` `last_run.error`, the run history,
BACKUP-NEEDS-ATTENTION.txt and the alert email all carry it. On Windows the walk did not reach it:
the letter swap ended on the "unavailable" words (cell 11), and Windows would not let another
program delete or write the held lock file (cell 12). A Windows swap with no write in flight (a
long incremental run with nothing new, Thunderbird's pre-sync wait) reaches the same sentence by
the same code.

**Current words** (`internal/lockfile/lockfile.go`, `StillHeld`):

- `lock <folder>/.mailarchive.lock was replaced during the run: stopping so two runs cannot overlap; run again`
- `lock <folder>/.mailarchive.lock was removed during the run (open <folder>/.mailarchive.lock: no such file or directory): stopping so two runs cannot overlap; run again`

**Why it fails the rule:** "lock" is jargon and the sentence starts with a hidden file's path; it
says the cause is two overlapping runs, when after a swap the cause is the drive; "run again" does
not say "with this archive's drive connected", so a person who runs again at once backs up into
the other drive. The plain explanation the patch did write (F4) never reaches the email, Status or
the note's first line. The email is what the phone reader gets.

**Proposed:** keep the kind (`lock_lost`), the exit code and every machine field; put plain words
first and keep the lock's sentence after "Details:", so every test that looks for "was replaced
during the run" or "was removed during the run" still finds it. The run already knows which case
it is at its end (`archiveGuard.endUnheld`'s `here`), so:

- the folder now holds something else, or another backup took it over (nothing recorded there):
  > **Backup stopped: the archive folder changed during the backup.** Another drive or folder is now at `<folder>` (a drive was swapped or disconnected, or the folder was moved or renamed), or another backup took it over. Nothing more was saved there. Connect the drive that holds this archive, then run the backup again. Details: lock `<folder>/.mailarchive.lock` was replaced during the run.
- its own lock file was deleted (recorded in its own archive):
  > **Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was deleted during the backup**, by a person or by a cleaning or sync app. Your archive is fine. Run the backup again. If this keeps happening, stop that app from cleaning the archive folder. Details: lock `<folder>/.mailarchive.lock` was removed during the run.
- its own lock file was changed (recorded in its own archive):
  > **Backup stopped: MailArchive's lock file in the archive folder (.mailarchive.lock) was changed during the backup**: another backup of this archive may have started, or another app wrote to the file. Your archive is fine. Run the backup again once any other backup of it has finished. Details: lock `<folder>/.mailarchive.lock` was replaced during the run.

Drop the OS error in parentheses (`(open …: no such file or directory)`) from the shown text; the
run log can keep it.

### F2 — MEDIUM (III): the release notes overstate the fix and quote the less common words

Checked claim by claim against the code and the walk:

| Claim (current) | Verdict | Evidence |
|---|---|---|
| Heading: "a backup never writes into another archive" | **untrue as written** | up to 10 s of new message files land in the other archive (940 in cell 4, a message folder in cell 5); the body admits it, the heading does not |
| "could finish as "OK" and leave its own list of saved messages and its "last backup" note in the other drive's archive … or showed a failed backup that was not its own" | true | MA-1328's shipped bug; also its pages, README and report (MA-1336), which the notes leave out |
| "another drive given the same letter or folder, or the archive folder moved or renamed" | true | cells 1, 4, 5, Windows |
| "checks … every few seconds, right before it changes or removes any file already there, before each save, and before each page and the README" | true, loosely | at least every 10 s (`archiveRecheckEvery`); also before the report, after Thunderbird's pre-sync and after the manifest loads; an encrypted run after every message |
| "ends as failed ("lock … was removed during the run … run again", or "replaced")" | **incomplete** | a real swap with a gap usually ends on "the archive location … is unavailable … reconnect the drive" (cells 1, 2, 11); a swap is "replaced", rarely "removed" |
| "with the usual failed-backup alert" | true for scheduled backups | cell 5; Run now and the terminal send none, as always |
| "Put the right drive back and run the backup again" | true; add what they then see | that drive's Status & health says "never finished … crash, kill, or power loss" until its next backup (F3) |
| "except for any new message files written in the few seconds before the backup noticed; that archive does not list them" | true, understated | up to 10 s: hundreds of files on a fast disk; `verify` lists them as unexpected (F8) |
| "A drive pulled out with nothing in its place still says … unavailable" | **untrue in part** | true for a USB drive on a Mac (`/Volumes` is not writable by the user) and a Windows drive letter; untrue when the folder's parent stays writable (a folder moved or renamed, a Linux mount point the user owns): the run makes the folder again and ends on "lock … removed" (cell 10, F5) |
| "This affects Mac, Windows and Linux, plain and encrypted archives, and Microsoft 365 backups" | true | `Run` and `RunGraph` share the guard; Microsoft 365 not walked |
| Bullet 2, "still says so in its own archive … as before" | true, but no change from 1.7.1 | cells 6 to 9; on Windows another program cannot delete or write the held lock file at all (cell 12); can go |
| "the check tries again for about a second" | true | 250 ms then 1 s (`archiveProofRetries`) |
| Windows bullet | true as far as the code and MA-1427's tests show | not walked (needs a signed-in session and a Low-integrity process) |
| "Nothing else changed … nothing to do but update" | true, with one note | people who swap drives may now get a failed-backup email that 1.7.1 never sent |

The proposed text is in full at the end.

### F3 — MEDIUM (III, with a II option): the right drive, put back, says the backup "never finished … crash, kill, or power loss"

**Seen:** cells 3 and 4 (Mac), cell 5 (Linux), cell 11 (Windows). The stopped run cannot write into the
folder it began in (that folder has gone with its drive), so that archive keeps the "running"
record the run began with. `status`, Status & health and `status -json` (`run_never_finished`) read
it as a crash. Recent backups of either archive shows no row for the stopped run, and Run now's
note goes when the page is left. The desktop notice after a scheduled failure ("Open MailArchive
Desktop to see why", code-read; not raised on the Mac by rule) then leads to a page that does not
say why.

**Current** (`internal/health/health.go`): `the run started DATE never finished (pid N is gone: crash, kill, or power loss) — check the log and run again`

**Proposed:**
> the backup started DATE did not finish and could not record how it ended: MailArchive was stopped, the computer shut down or restarted, or this archive's drive was disconnected or swapped during the backup. Run the backup again.

Option (Type II, the lead's call): a scheduled run that ends with its lock lost and nothing
recorded could leave the same breadcrumb beside the job file that a run failing before its start
already leaves (`recordHeadlessFailure`), so the Desktop can say why after a notice.

### F4 — MEDIUM (III): the new warning line is jargon and says "nothing was recorded" where files were written

**Seen:** cells 1, 2, 4, 5, 10 (Mac and Linux) and 11 (Windows). Terminal, run log, Run now's live log.

**Current** (`internal/app/archiveguard.go`, `lockGoneWarning`): `warning: <folder> no longer holds this backup's lock (its drive was changed, the folder was moved, or another backup took it over); nothing was recorded in it`

**Proposed:**
> warning: the folder at `<folder>` changed during this backup (another drive or folder is there now, or another backup took it over), so this backup's result was not written into it.

If F1 is folded, this line repeats the error's first sentence; it can stay as the log's record of
which branch ran.

### F5 — MEDIUM (II, pre-existing, reduced by the patch): a folder that vanishes from a writable parent is made again

**Seen:** cell 10 (Mac, the folder moved away), and the first Desktop attempt. The walk writes each
new message by path and makes its folders as it goes, so it made `…/drive/Archive/A/Inbox` again
and wrote 366 message files there until the 10-second proof stopped it; the stray folder holds no
manifest. 1.7.1 would have written the whole run there and saved a manifest, so 1.7.2 is better.
Not a wording fix: the exporter could refuse to make the archive's own top folder (as the Outlook
step does with `ParentMustExist`), so the run meets the "unavailable … reconnect the drive" fatal
instead. A USB drive on a Mac or a Windows drive letter cannot hit this; a folder the person
renames, or a Linux mount point the user owns, can. For the lead.

### F6 — LOW (III): the OS error inside the lock sentence

`(open …/.mailarchive.lock: no such file or directory)` repeats the path and adds a system call's
words. Folded into F1's proposal.

### F7 — LOW (III, code-read): the encrypted archive's fixed sentence for a lost lock

An encrypted archive's record and email say only the kind's sentence (`state.FailureText`):
`another program took over the archive during the backup`. After a swap it reaches the email when
the folder at the path is encrypted, and it names no fix. **Proposed:**
> the backup stopped because the archive folder changed during the backup (its drive was swapped or disconnected, the folder was moved, or another program changed MailArchive's lock file there) — connect this archive's drive and run the backup again

Not walked: making an encrypted archive needs a typed password at a terminal.

### F8 — LOW (III): `verify` names the wrong causes for the files a swap leaves behind

After cell 4, `mailarchive verify` on B said `attested` and listed 940 `unexpected` files with:
`note: \`unexpected\` files are under a store directory but owned by no record — a leftover from an old-binary excursion, or a file added by hand; compare bytes before deleting anything.`
**Proposed:** add the new cause:
> … a leftover from an old-binary excursion, a file added by hand, or message files another archive's backup wrote here before it noticed its drive had been swapped; compare bytes before deleting anything.

### F9 — LOW (pre-existing): the terminal prints the failure twice

Cells 1, 6, 7, 10: the log line `FAILED: <reason>` and then `mailarchive: <reason>`, the same long
sentence twice. X2 is met (one voice, `mailarchive:`), but a phone-width terminal shows two
screens of the same words. Not new in 1.7.2.

### F10 — INFO (pre-existing): `status` writes the lock's holder line and the index's side files

`health.Gather` probes the lock with `lockfile.Acquire`, which creates `.mailarchive.lock` if it is
absent and rewrites its holder line, and opening `search.db` creates `search.db-shm` and
`search.db-wal`. In the walk this changed B's lock and added two index files between cell 1 and
cell 2 (a `status` of B, not a backup), and it made a lock file in cell 10's stray folder. Harmless
to the archive, but it means "the other archive's lock is unchanged" holds only if nothing probed
it. Not in 1.7.2's diff.

### F11 — LOW (unwalked, Windows): the instance file's new refusal has no plain words

If Windows will not set the integrity label (a profile folder on a file system without labels),
MailArchive Desktop does not start, with `could not record this instance: temp file in <folder>:
createsecret <file>: <Windows error>`. `%LOCALAPPDATA%` is local NTFS on almost every computer,
so this should be rare. If it is kept: "MailArchive Desktop could not start: Windows would not
protect its private start file in `<folder>` (`<Windows error>`). That folder must be on this
computer's own drive."

## Verdict per sentence

| Sentence (where it comes from) | New, or newly reached | Verdict |
|---|---|---|
| `lock … was replaced during the run: stopping so two runs cannot overlap; run again` (`lockfile.StillHeld`) | newly reached by every swap with no write in flight, and by K3b | **rewrite** (F1) |
| `lock … was removed during the run (open …: …): stopping so two runs cannot overlap; run again` | newly reached by K3 and by a folder made again (F5) | **rewrite** (F1, F6) |
| `warning: … no longer holds this backup's lock (…); nothing was recorded in it` (`lockGoneWarning`) | new | **rewrite** (F4) |
| `the archive location … is unavailable (…) — reconnect the drive or share and re-run. Messages written before this point are kept; nothing was recorded as archived that was not fully written.` | newly reached by a swap with a write in flight, and by the guard's proof | **keep**; it says why and what to do. Its `(lstat …: no such file or directory)` is noise, as before |
| `the run started … never finished (pid … is gone: crash, kill, or power loss) — check the log and run again` (health) | newly reached on the right drive after a swap | **rewrite** (F3) |
| `another program took over the archive during the backup` (encrypted kind sentence) | newly reached by an encrypted swap's email | **rewrite** (F7) |
| Email: subject `MailArchive backup FAILED: <folder>`, `MailArchive backup result: FAILED`, `Reason:` | frame unchanged | **keep**; the reason follows F1 |
| Run now: `Backup failed: <reason>` | frame unchanged | **keep**; the reason follows F1 |
| Recent backups: `<when> — Failed — <reason>` | frame unchanged | **keep** |
| `status`: `Last run: … → failed: <reason>`, `RED: the last run (…) FAILED: <reason>` | frame unchanged | **keep** |
| BACKUP-NEEDS-ATTENTION.txt: `This email backup FAILED and needs your attention. … The messages already in this folder are unaffected …` | frame unchanged; true for K3 | **keep** |
| `status -json`: `reason_codes` `run_failed` / `run_never_finished`, `last_run.error`, `last_run.status` | unchanged codes (X8) | **keep** |
| `verify`'s note on `unexpected` files | newly reached | **amend** (F8) |
| `could not record this instance: temp file in …: createsecret …` (Windows) | new, unwalked | **optional** (F11) |
| Exit codes: 1 for every stopped backup, 0 for `status` | unchanged (X1) | **keep** |
| stdout empty on every failure; the conversation on stderr | unchanged (X8) | **keep** |

## Release notes: proposed text

Replaces lines 80 to 85 of `packaging/release-notes.md`. About 300 words where the current section
has about 520. It keeps every true fact the lead listed: what could go wrong, who is affected,
what to do, and that nothing else changed. It drops the "still says unavailable" sentence (not a
change, and not always true), the quoted lock words (they are the less common outcome, and F1
would change them), and the "as before" bullet (no change from 1.7.1).

```markdown
### Fixed in 1.7.2 — a backup stops when its drive is swapped during it, and a sandboxed program cannot open MailArchive Desktop
- **If you swap backup drives, a backup no longer writes its records into the other drive's archive.** Before, if the drive changed while a backup ran (another drive at the same letter or folder, or the archive folder moved or renamed), the backup could end "OK" and leave its list of saved messages and its "last backup" note in the other archive, which then listed mail it did not hold, or showed a failed backup that was not its own. Now the backup checks its folder at least every 10 seconds, and before every save, every page and every change to a file already there. If the folder has changed, the backup stops, writes nothing more there, and ends as failed, with the usual failed-backup email for a scheduled backup. This applies on Mac, Windows and Linux, to plain and encrypted archives, and to Microsoft 365 backups.
- **What to do:** put this archive's drive back and run the backup again. Until you do, its Status & health may say the last backup "never finished". The other drive's archive keeps its own records and pages. Only the new message files written in the seconds before the backup noticed (up to 10 seconds' worth, which can be hundreds) stay in its folder; that archive does not list them.
- **A brief hiccup is not a failure.** If the archive folder can't be read for a moment (a network folder reconnecting, for example), the check tries again for about a second before it stops the backup.
- **Windows: a sandboxed program can no longer open MailArchive Desktop.** MailArchive Desktop keeps a private key in a small file so its own shortcuts can reach it. A program in a restricted sandbox under your account (as a browser runs web pages) could read that key and use it to open a MailArchive Desktop page. Now only programs running normally under your account can read it. Shortcuts work as before. Mac and Linux are not affected.
- **Nothing else changed.** Your archives, settings and scheduled backups stay as they are; there is nothing to do but update.
```

If F1 is folded, add one sentence to the first bullet: `It says "Backup stopped: the archive
folder changed during the backup."` so a reader can match the email to the note.

## Not walked

- **Microsoft 365 capture** (`RunGraph`, MA-1337): needs a tenant. Its guard is the same code; its
  words are the same lock sentence.
- **Encrypted archives** (MA-1338, MA-1443, F7): making one needs a typed password at a terminal.
- **The Windows sandbox fix** (MA-1427): needs a signed-in session and a Low-integrity process;
  the VM has nobody signed in. Covered by `ma1427_integrity_windows_test.go`.
- **Edge and the Windows Desktop**: nobody is signed in to the VM, so no browser walk there; the
  Windows cells are the terminal as SYSTEM.
- **The macOS desktop notice** after a failed scheduled backup: never raised on this Mac by rule;
  its words were read from `internal/wizard/wizard.go` (`failureNotice`).
- **Thunderbird pre-sync swaps** (MA-1443's wait): not walked; the guard's words are the same.

## Lab notes

Everything is under the worktree's `.claude/testdata/fric172/` (untracked, not committed):
`setup.sh`, `walk-mac.sh`, `desk-*.sh`, `linux/walk-linux.sh`, results in `mac/res/` and
`linux/res/`, the email files in `linux/res/mail/`. The Windows script is
`/Users/alex.dimarco/repositories/.winlab/fric172/walk-win.ps1`; its results are in the VM at
`C:\fric172lab\res`. The stray folders of cell 10 and the first Desktop attempt are kept in
`mac/parked/` as evidence.

## Fix round (2026-11-14, the patch builder; added beside the review, nothing above changed)

The lead's calls, one fix round on `patch-1.7.2`. Each finding, what was done, and its catalog row:

| Finding | Outcome |
|---|---|
| F1 | Adopted as proposed: three plain sentences by what the run found at its end, the lock's sentence after "Details:" (MA-1447). The words live in one place (`internal/app/archiveguard.go`). They replace the run's error on every surface; kind `lock_lost` and exit 1 unchanged. |
| F2 | Adopted as the base of the release notes, then made true after the round: the Windows bullet says a sandboxed program can no longer read the key file (the adversarial review's P2: not "can no longer open MailArchive Desktop"); quit MailArchive Desktop or sign out after updating (P4); "at least every 10 seconds while it writes"; the hiccup bullet says about 30 seconds (P1); "did not finish" (F3's new words); the sentence `It says "Backup stopped: the archive folder changed during the backup."`. About 345 words. |
| F3 | Adopted (MA-1448). The Type II option (a breadcrumb beside the job file) was not taken. |
| F4 | Adopted (MA-1447); printed only when the folder is there but not the run's (an unreachable folder ends on "unavailable" and needs no second line). |
| F5 | Fixed (MA-1446): a run's exporter never makes the archive's own folder; a vanished folder ends "the archive location … is unavailable … reconnect the drive or share". Cell 10's stray folder can no longer be made by the walk. |
| F6 | Adopted inside F1: the system's error is not in the shown words. |
| F7 | Adopted (MA-1449). |
| F8 | Adopted (MA-1450). |
| F9 | Pre-existing (the failure printed twice); not in this patch. |
| F10 | Pre-existing (`status` probes the lock with `Acquire`); not in this patch. |
| F11 | Adopted (MA-1451), not walked on a file system without labels. |

Every sentence was re-checked after the P1 and F5 changes: "Nothing more was saved there" holds (the run proves before its next write and never makes the folder again; files written before it noticed stay, as the notes say); "Your archive is fine" holds for the two own-archive branches (the run stopped before any save after its lock was lost).
