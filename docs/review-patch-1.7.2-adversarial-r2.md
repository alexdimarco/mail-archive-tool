# Adversarial re-check, round 2: the MailArchive 1.7.2 patch

**Verdict: SHIP-WITH-NOTES.** Round 1's P1, P5, P6, P7 and P8 are closed: each was reproduced again
on this build and now behaves as the fix round says, on the Mac and (P1) on Windows. What the fix
round added holds under attack: a drive swapped while a backup only reads is caught before the
backup's first write (no message page reached the other archive), the "running" record binds a run
to its folder for plaintext, encrypted and Microsoft 365 archives, and a brand-new archive's first
backup still works on every path tried. No new finding loses data, and in every outage tried the
fix round ends ok wherever 1.7.1 did (the one exception is a lock file another program makes
unreadable for good mid-run, which a small 1.7.1 run never looked at: see the Windows section). The
new findings are smaller: the release notes' "a brief hiccup is not a failure"
is true only while a backup reads or at its checks, not between them while it writes (Q1); a Cancel
pressed during the new up-to-30-second wait is not seen until the wait ends, and the run is then
recorded failed, not cancelled (Q2); "not found" over a network can be taken for a deleted lock
file without the wait (Q3, PLAUSIBLE); a copy of the archive made mid-run is taken for the original
(Q4, as in 1.7.1); and the wait says nothing while it waits (Q5). Recommended before the release:
Q1's one-sentence change to the notes. Q2 and Q5 are small code changes worth taking if the fix
round is reopened; Q3 and Q4 are follow-ups. None blocks the release.

| | |
|---|---|
| Reviewed | branch `patch-1.7.2` at `ff29ee2` (code commit `c8d8657`), this round's diff `c71467d..ff29ee2`; compared with the first 1.7.2 build `c71467d` and shipped 1.7.1 (`main` `1a6d437`) |
| Date | 2026-11-14 |
| Process | assurance-kit `process/adversarial-review.md`, scoped re-check. Deviation, as round 1: one reviewer ran the lenses (integrity/concurrency on the guard; healthy-run regressions; swap closure) and then tried to refute each of its own findings by reproduction. Finder and skeptic were not separate people. |
| Tools | go 1.26.0 darwin/arm64, macOS 26.6.2 (Darwin 25.6.0); Windows 11 VM (ARM64, amd64 test programs emulated), run as SYSTEM through `prlctl exec`, nobody signed in, every lab copy given its own state folders under `C:\ProgramData\p172adv2lab` |
| Count | 5 found (Q1 to Q5): 3 confirmed by reproduction (Q1, Q2, Q4), 1 confirmed from the source (Q5), 1 PLAUSIBLE (Q3); 14 attempts refuted; round 1's P1, P5, P6, P7, P8 closed |
| Files (git blob ids at ff29ee2) | `internal/app/archiveguard.go` 399749a; `internal/app/run.go` e9d74f5; `internal/app/graph.go` 3de8123; `internal/app/graphfaults.go` 5ce945b; `internal/app/localrun.go` 29e3f77; `internal/export/exporter.go` 3dfd3a8; `internal/export/atomic.go` 09def77; `internal/export/errors.go` dda22b7; `internal/lockfile/lockfile.go` f16f4b3; `packaging/release-notes.md` 7cdfb4a |

## Commands and exit statuses

Probes log what happened; they do not assert. They are kept uncommitted in this review's worktree,
`.claude/testdata/probes/` (build tag `advprobe2`; round 1's `zz_p172adv_test.go` copied unchanged,
tag `advprobe`), with the Windows runner and lab scripts beside them, and were copied into
`internal/app/` of each tree to run. The two comparison trees were detached worktrees
(`.claude/worktrees/patch-1.7.2-adv2-base171` at 1a6d437, `.claude/worktrees/patch-1.7.2-adv2-c714`
at c71467d), removed after the runs; `git worktree add --detach` makes them again.

- Mac, comparison probes A, A2, B, C, D1 (`zz_adv2cmp_test.go`): `go test -tags advprobe2 -count=1 -v
  -run TestAdv2_ ./internal/app/` in ff29ee2 → exit 0 (106.0 s); in 1a6d437 → exit 0 (112.8 s); in
  c71467d → exit 0 (105.2 s). B re-run on ff29ee2 with a corrected page count, with B3 → exit 0
  (25.6 s).
- Mac, this round's probes E to J and round 1's probes (`zz_adv2_test.go`, `zz_p172adv_test.go`):
  `go test -tags advprobe,advprobe2 -count=1 -v -run 'TestAdv2_A2_|TestAdv2X_|TestP172Adv_'
  ./internal/app/` in ff29ee2 → exit 0 (92.9 s).
- Windows (this review's copy of `winrun-at.sh`, `.claude/testdata/winrun-adv2.sh`: test programs
  `adv2app.test.exe` and `adv2base.test.exe` cross-built with `GOOS=windows GOARCH=amd64 go test -c
  -tags advprobe2`, run by `prlctl exec "Windows 11" powershell -File
  \\Mac\winlab\p172adv2\tests.ps1` as SYSTEM, state folders under `C:\ProgramData\p172adv2lab`):
  exit 0 (12:42 to 12:46 VM time). The guard tests listed in the Windows section PASS or SKIP by
  design; probes W1 to W3 on ff29ee2 and W1, W2 on 1a6d437 PASS (they log). W3's SMB form re-run
  alone (`tests-w3.ps1`) → exit 0.
- Mac, the unfiltered suite on ff29ee2 with the probe copies taken out of `internal/app/`:
  `go test -p 4 -count=1 ./...` → exit 1: 52 packages ok; `internal/app` and `internal/desktop`
  stopped at go's default 10-minute limit (`TestCopyExportScalesToSixtyThousandMessages` at 5m40s,
  `TestNoStaleLockWindow` at 17 s, while four packages ran at once), no test failed. Those two
  re-run with CI's longer limit: `go test -count=1 -timeout 30m ./internal/app/ ./internal/desktop/`
  → exit 0 (`internal/app` 1194.7 s, `internal/desktop` 606.1 s). So every package passes, and the
  committed guard tests among them (the fix round's MA-1444 to MA-1452 included) pass on this Mac.

## Round 1's findings, re-checked

| Round 1 | Re-check on ff29ee2 | Verdict |
|---|---|---|
| P1 (MED): a moment's network outage while a backup only reads fails it | Probe A, real interval and waits: an incremental backup walks 40 messages it already has, 350 ms each (about 14 s); 7.7 s in, its lock file, or its folder's parent, cannot be read for 5 s. ff29ee2: lock ok, folder ok, about 14.1 s each, recorded "ok". The same with 5 new messages after the walk and the outage from 13 s, over the backup's first write: lock ok and folder ok (22.0 s: the check before that write waited the outage out), 45 records. c71467d: all four failed (`lock_lost` "lock … was removed during the run (… permission denied)", or `archive_unwritable`). 1.7.1: ok, ok, ok, and folder-over-first-write failed (its write met the outage). Round 1's own probe (2 s, a check due at every message): ok and ok. Windows: see the Windows section. | CLOSED |
| P5 (LOW): the crash sweep removes files in the other archive | Round 1's probe P6: the other archive kept its stale temp and its zip with no page; the run stopped in plain words ("the archive folder changed"). The committed `TestTheCrashSweepProvesBeforeEachRemoval` and its capture form pass. | CLOSED |
| P6 (LOW): a new archive's first backup that loses its lock leaves no record | Round 1's probe P4: recorded "failed", BACKUP-NEEDS-ATTENTION.txt written, one run-history entry in the plain "lock file … was deleted" words. Also new here: a brand-new encrypted archive (probe E: failed, kind `lock_lost`, notice, one history entry) and a Microsoft 365 capture into a new folder (probe F: failed, notice, one history entry). | CLOSED |
| P7 (LOW): two archives whose manifests hold the same bytes are taken for one | Round 1's probe P5: the two manifests are equal (133 bytes); after the swap the run stopped as "changed", and only the two message files in flight were added there (as the notes allow). The committed `TestTheRunningRecordBindsANewArchiveAndTellsSameBytesApart` asserts the other archive's record, history and notice unchanged, and passes. | CLOSED |
| P8 (LOW): a linked archive folder whose drive is pulled reads as a lost lock | Round 1's probe P3: full mode "the archive location … is unavailable (create output dir …: stat …: no such file or directory)" at once; incremental "… is unavailable (stat …: no such file or directory)" after the 30 s wait; both `archive_unwritable`. | CLOSED |

## New findings

| ID | Severity | Lens | Claim | Verdict | Evidence |
|---|---|---|---|---|---|
| Q1 | LOW (words) | healthy runs | "A brief hiccup is not a failure" is not true between checks while a backup writes new mail: an outage that meets a message write ends the backup at once, with no 30-second wait | CONFIRMED (Mac, all three builds; Windows, patch and 1.7.1) | probes A2, W2 |
| Q2 | LOW | integrity | A Cancel pressed during the up-to-30-second wait is not seen until the wait ends; the backup is then recorded failed, not cancelled; a Desktop shutdown (15 s budget) during the wait leaves it unfinished | CONFIRMED (Mac, engine); the shutdown part from the source | probe H, `lifecycle.go` |
| Q3 | LOW | healthy runs | On a network folder, "not found" can be a symptom of the outage, yet it is taken as a definite "lock file deleted" and stops a writing backup at once | PLAUSIBLE (mechanism in source; not reproduced) | Go's error mapping, `ask` |
| Q4 | LOW, pre-existing | swap closure | A copy of the archive made during a run and put in its place is taken for the run's own | CONFIRMED (Mac); same in 1.7.1 and c71467d by construction | probe G |
| Q5 | LOW (friction) | healthy runs | The wait says nothing: no log line, the progress stops | CONFIRMED by source | `settle` |

### Q1 (LOW, words; all platforms): "A brief hiccup is not a failure" holds only while a backup reads, or at a check

What happens. The fix round made a check wait out an outage (up to 30 s) and stopped checking while
a backup only reads. A backup that writes checks before its first message write once 10 s have
passed since its last check, and before every save, page and overwrite; the new-message writes in
between are not checked first (`writeGuard`, `internal/app/archiveguard.go:328-335`). When one of
them meets the outage, the write fails, `lr.probe` (`internal/app/localrun.go:178-186`) finds the
folder unreadable and ends the run at once as "the archive location … is unavailable", with no
wait, and `proof.gone` (`archiveguard.go:173-180`) keeps that answer, so the run's record cannot be
written either and stays "running" (`status`: "did not finish"). 1.7.1 did the same, so this is not
a regression. It is the release-notes bullet that says more than is true: its second sentence is
right ("when it does check and the folder can't be read … it keeps trying for about 30 seconds"),
but its title, "A brief hiccup is not a failure", is not, for a backup that is writing new mail.

Proof (Mac, probe A2: 40 new messages, 350 ms each; the archive folder's parent made unreadable for
3 s from the tenth message, 3.5 s in, when the last check, after the manifest's load, is under 10 s
old):

- ff29ee2: ended after 3.67 s, `archive_unwritable`, "the archive location … is unavailable (lstat
  …: permission denied) — reconnect the drive or share and re-run", record left "running".
- 1.7.1: the same (3.63 s), plus its "could not save the manifest" sentence.
- c71467d: the same, after its 1.25 s of retries (6.1 s).
- Windows (W2, the drive letter taken away for 3 s from the tenth new message): ff29ee2 failed at
  once, "the archive location X:\MailArchive is unavailable (create output dir X:\MailArchive\Fresh:
  GetFileAttributesEx X:\MailArchive: The system cannot find the path specified.)", record left
  "running"; 1.7.1 the same.

By contrast the same kind of outage over the first write after a 13 s read-only stretch (probe A
and W2, over-first-write) is waited out and ends ok on ff29ee2, where 1.7.1 failed it (folder form)
and c71467d failed both forms.

Fix (either). Words: "A brief hiccup while a backup checks its folder, or while it only reads mail
it already has, is not a failure." Or code: when a message write fails and the probe finds the
folder unreadable, ask the run's proof (which waits up to 30 s) and, if the folder comes back as the
run's own, write that message again once.

### Q2 (LOW; all platforms): a Cancel during the wait waits it out, and the backup is recorded failed

`settle` pauses with `time.Sleep` (`archiveguard.go:140`) and never looks at the run's context.
Probe H (a full run whose lock file stays unreadable from its first message; the run cancelled 1 s
later): the run returned 29.0 s after the cancel (30.1 s in all) with `archive_unwritable`, "the
archive location … is unavailable (open …/.mailarchive.lock: permission denied) — reconnect the
drive or share and re-run", recorded "failed" with BACKUP-NEEDS-ATTENTION.txt. A person who presses
Cancel in MailArchive Desktop, or Ctrl-C at the terminal, sees nothing happen for up to 30 s and
then a failure instead of "cancelled" (1.7.1 has no wait). The run keeps the archive's lock that
long. And MailArchive Desktop's shutdown (its Shutdown button, Ctrl-C, signing out) cancels the
running backup and gives it 15 s to finish (`DrainBudget`, `internal/desktop/lifecycle.go:10`),
less than the wait, so a shutdown during a wait leaves the backup unfinished and its record
"running" ("did not finish" in `status`), by the source. Not data loss: nothing is written while a
check waits, and an unfinished run is what a crash leaves.

Fix: give the guard the run's context and wait with a `select` on `ctx.Done()` and a timer; a wait
ended by a cancel returns the cancel (not "unavailable") and is not kept in `lost`.

### Q3 (LOW, PLAUSIBLE; network folders, mainly Windows): "not found" during an outage is taken as "your lock file was deleted"

`ask` (`archiveguard.go:148-168`) treats the lock file "not found" as a definite answer when a second
`os.Stat` of the folder succeeds. Go maps Windows' ERROR_BAD_NETPATH (53, "the network path was not
found") to not-found (`src/syscall/syscall_windows.go:201-205`, go1.26.0), so `LostError.Removed`
(`internal/lockfile/lockfile.go:172`) is true for it, and Go's `os.Stat` on Windows asks
`GetFileAttributesEx` first (`src/os/stat_windows.go:34-37`), which the Windows SMB client can answer
from its file-information cache for some seconds after the server stops answering. A check in a
writing backup during a NAS or Wi-Fi drop could then stop at once with "MailArchive's lock file …
was deleted during the backup, by a person or by a cleaning or sync app" (or, when its record cannot
be read, "the archive folder changed during the backup"), not wait 30 s as the notes say. Not
reproduced: it needs a real share outage, which this lab cannot make without touching the VM's
network. Not a regression (1.7.1 failed such a backup too), and a backup that only reads checks
nothing, so P1's case is not affected. A Mac or Linux network client that reports ENOENT while it
reconnects would do the same.

Fix: count "not found" as definite only when a fresh look at the folder succeeds — for example open
the folder and read one name (`os.Open(out)` then `Readdirnames(1)`, which goes to the server) — or
ask once more after a short pause before stopping.

### Q4 (LOW, pre-existing, accept): a copy of the archive made during a run is taken for the run's own

Probe G: at the run's third message the archive folder was copied (`cp -Rp`: lock file, "running"
record and manifest included) and the copy put at the archive's path. The run ended ok and wrote
its new messages, its manifest (10 records) and its "ok" record into the copy; the original kept 6
records and a record that says "running". The lock's nonce, the running record and the manifest are
all in the copy, so nothing in 1.7 can tell it from the original; 1.7.1 and c71467d behave the same.
It needs a clone of the archive made while a backup runs and then put in its place. Nothing is lost
(the copy holds everything). Accept; it closes with Gmail de-dup's archive id plus a check of the
folder's own identity (volume and file id).

### Q5 (LOW, friction; all platforms): the wait says nothing

While a check waits (up to 30 s), the run logs nothing (`settle` has no logger) and MailArchive
Desktop's progress stops. A person watching "Run now" sees a backup frozen for half a minute with no
reason; a scheduled run's log shows an unexplained gap before "unavailable". Fix: one log line when a
check first cannot read ("Cannot reach the archive folder at …; waiting up to 30 seconds for it to
come back"), and one when it can again.

## Windows (VM, as SYSTEM)

The patch's guard tests, built from ff29ee2 (`.winlab/p172adv2/tests.ps1`):
`TestADriveLetterMovedDuringTheWalkGetsNoManifest`, `TestALockLostDuringASmallRunSavesNoManifest`,
`TestAHealthyRunIsNeverStoppedByItsProofs`, `TestADriveLetterMovedInTheRunsTailGetsNoPages`,
`TestADriveLetterMovedBeforeAReplaceGetsNothingReplaced`, `TestEveryFolderPageIsProvedBeforeItIsWritten`,
`TestALockLostInItsOwnArchiveIsRecordedThere` (6 subtests) and `TestTheUnexpectedNoteNamesASwappedDrivesFiles`
PASS; the rest SKIP by design on Windows after their positive twins (folder renames, file modes,
symbolic links), among them `TestTheRunningRecordBindsANewArchiveAndTellsSameBytesApart`, whose
Windows form (P6: a brand-new archive's lock file written over, recorded failed there) runs before
its skip, and `TestAStoppedBackupSaysWhatHappenedInPlainWords`, whose "changed" form runs.

This review's probes (`zz_adv2win_test.go`), patch and 1.7.1:

| Probe | ff29ee2 | 1.7.1 (1a6d437) |
|---|---|---|
| W1 nothing-new: a deny entry for the running account on the lock file (reading it: "Access is denied") for 5 s, 8 s into a 14 s read-only walk | ok, 14.8 s, recorded "ok" | ok, 16.5 s (never reads the lock then) |
| W1 over-first-write: the same from 13 s, over the first write | ok, 20.7 s (waited), recorded "ok" | ok, 17.3 s (its writes do not read the lock) |
| W1 for-good, full run | 30.3 s, `archive_unwritable` "the archive location … is unavailable (open …\.mailarchive.lock: Access is denied.)", recorded "failed" there | ok, 91 ms (a run this small never reads its lock; a larger one fails at its first checkpoint save as a lost lock) |
| W2 nothing-new: the archive's drive letter (subst `X:`) taken away for 5 s and given back, inside a read-only walk | ok, 18.7 s, 40 records | ok, 16.6 s, 40 records |
| W2 over-first-write | ok, 29.2 s (waited), 45 records | failed (16.0 s): "the archive location X:\MailArchive is unavailable (create output dir X:\MailArchive\Fresh: mkdir X:\MailArchive: The system cannot find the path specified.)" plus "could not save the manifest", record left "running", 40 records |
| W2 between checks while writing (3 s away, from the tenth new message) | failed at once (4.9 s): "the archive location X:\MailArchive is unavailable (create output dir X:\MailArchive\Fresh: GetFileAttributesEx X:\MailArchive: The system cannot find the path specified.)", record left "running" (Q1) | the same (4.2 s), plus "could not save the manifest", record left "running" |
| W3 a brand-new archive's first backup at a drive root (`X:\`), then a full re-run | ok, 5 records; ok | not run |
| W3 the same over SMB (`\\localhost\C$\ProgramData\p172adv2lab\w3\smb\first`, a folder not there yet) | ok, 5 records; ok | not run |

So on Windows, as on the Mac, the fix round waits out a 5 s outage that 1.7.1 did not survive (W2
over-first-write) and keeps 1.7.1's behaviour elsewhere, except two cases: an outage between checks
while writing still fails at once (Q1, as 1.7.1), and a lock file that stays unreadable to the run
holding it now ends the run after 30 s where a run as small as W1's never read it in 1.7.1 (a lock
file the run opened for reading and writing at its start does not become unreadable to it unless
another program changes its permissions mid-run; a larger 1.7.1 run fails at its first checkpoint).
After the lab: `X:`, `U:` and `V:` not mapped, `subst` lists nothing, and SYSTEM's own
`MailArchiveDesktop` folder does not exist. The Mac's share (`\\Mac\winlab`) refused SYSTEM's folder
creation, so W3's network form used the VM's own SMB share, as round 1 did.

## Refuted attempts

1. **A swap while a backup only reads, then the backup writes: the first write slips through.**
   REFUTED. Probe B (swap 7.7 s into a 14 s read-only walk; 5 new messages after): ff29ee2 stopped
   in plain words with 0 of A's message pages and no top-level file changed in the other archive
   (c71467d the same). 1.7.1 wrote the other archive's manifest, last-run record, run history,
   README and root page. Each source's end is also proved before its save (`run.go:1116`), so the
   next source never starts on a swapped folder (case 2 stopped at 1.5 s, before any write). The
   accepted bound is unchanged: writes within 10 s of the last check are not checked first. Probe
   B3 (one source; swap at its second message; 40 new messages in 7.5 s) put 40 of A's message
   pages, and nothing else, into the other archive, as the release notes say ("up to 10 seconds'
   worth, which can be hundreds").
2. **A `-copy-first` backup writes its snapshot (a whole copy of a PST) into the other archive after
   a read-only stretch with no check.** REFUTED for ff29ee2 and c71467d: the previous source's end
   is checked before its save, so the next source's snapshot comes milliseconds after a check (probe
   C found none). 1.7.1 wrote a 1,033,216-byte snapshot into the other archive (probe C).
3. **A swapped-in archive whose lock file cannot be read is waited on for 30 s: something is written
   meanwhile.** REFUTED. The check sits on the only writing path (the walk, the sweep, the tail) and
   blocks it; the run then ends "unavailable" and, not finding its running record there, writes
   nothing. The committed `TestALockThatStaysUnreadableEndsUnavailableAfterTheWait` shows the
   manifest unchanged after the real 30 s wait. Its words ("unavailable … reconnect the drive or
   share") are less exact than "changed", and safe.
4. **The classification fooled by error kinds.** Mac: permission denied on the lock or the folder is
   waited out (probe A); "not found" with the folder there stops at once (committed tests); a link
   loop put at the archive's path ends "unavailable" at once through the write's own fault (probe I:
   71 ms), and nothing is made. Windows: see the Windows section (a deny entry on the lock file,
   ERROR_ACCESS_DENIED; a drive letter gone, ERROR_PATH_NOT_FOUND). Windows sharing violations: Go
   opens the lock file without FILE_SHARE_DELETE (`syscall_windows.go:395`), so no cleaner can
   delete it under the run, and round 1's refutation stands. The remaining hole is Q3.
5. **MA-1452's record forged, copied or left behind.** A crash leaves a "running" record that the
   next run's `beginRun` replaces with its own. A 1.7.1 reads the same record format
   (`newRunRecord` and `state.WriteLastRun` are not changed by the patch). Only a cloned folder
   carries the record: Q4.
6. **Two backups at once.** Probe D1: run A's lock file removed at its first message; run B starts,
   runs and ends ok while A still walks. ff29ee2: A stops ("… or another backup took it over") and
   writes nothing; B's "ok" record and its history stand. c71467d: A wrote "failed" over B's "ok"
   (the manifest bytes matched), with a notice. 1.7.1: A ended "ok" and never noticed. MA-1452 makes
   this right.
7. **Encrypted archives and Microsoft 365 captures under MA-1452.** Probes E and F: the failure is
   recorded in the run's own new folder, with no manifest to compare.
8. **F5 breaks a brand-new archive's first backup.** REFUTED. Probe J: attended into a folder not
   there yet, attended into an empty folder, unattended (a scheduled job) into an empty folder the
   person made, a relative path, a path ending in a separator, a Microsoft 365 capture into a folder
   not there yet, and an encrypted archive's first backup: all ok, every record "ok", every message
   there. Windows: see the Windows section (a drive root and the share). The window the builder
   noted stays: the manifest save (`state.Save`, `internal/state/manifest.go:2192`) and the pages
   (`internal/pages/pages.go:109`) make their folder by path right after a check; milliseconds, and
   only where the archive's parent is writable by the person (a Mac's `/Volumes` is root's,
   `drwxr-xr-x root wheel`, so a pulled USB drive cannot be made again there).
9. **A link pointed elsewhere mid-run.** Probe I: incremental stopped at the source's end check with
   nothing written into the other archive; full mode stopped after 4 new message pages there (new
   files keep the 10 s bound; overwrites are checked first); a loop ended "unavailable" at once.
10. **The 30-second wait stacks into a backup that never ends.** REFUTED as stated. Each check waits
    at most 30 s and a failed wait is kept (`g.lost`), so a dead folder costs one wait per run. A
    share that keeps dropping and coming back can make each check wait: a writing backup checks at
    most once per 10 s of new writes plus each save, page and overwrite, so a scheduled
    (incremental) run is stretched by roughly the outages themselves; a full run, which checks
    before every overwrite, could be stretched much more. The next scheduled run then meets the
    held lock and is refused as busy, as for any long run. Bounded. (Cancelling during a wait: Q2.)
11. **Read-only walks are still checked somewhere.** REFUTED: probe A (nothing new) and round 1's P1
    probe with a check due at every message both end ok across the outage.
12. **The search index is opened again by path mid-run (after a swap, the other archive's).**
    REFUTED by source: one pooled connection (`SetMaxOpenConns(1)`, `internal/index/index.go:143`;
    default idle 2, no lifetime), opened before the walk.
13. **Something else rewrites the "running" record mid-run, so the run no longer knows its own
    folder.** Only `beginRun`, `RecordRefusedRun` (before the record, under the lock),
    `RecordRunWarning` (refuses while a run is running) and the Desktop's scheduled-capture start
    failure (`internal/desktop/capture.go:241-250`, written without the lock) write it. The last
    could replace a running capture's record only if a scheduled capture that is not configured is
    pointed at an archive another backup is writing, and only matters if that backup then loses its
    lock: negligible.
14. **Healthy backups regress.** Sleep and resume (`due` uses the monotonic clock), case-insensitive
    paths (nothing compared by name), very large archives (a check is one folder stat and one
    lock-file read, at most one per 10 s of new writes plus each overwrite, as round 1's P9),
    cloud-synced folders (a sync app that removes or rewrites the lock file stops the run as before,
    now in plain words), antivirus (round 1's refutation; the Windows deny-entry runs show an
    unreadable lock file is waited out). Healthy runs over SMB on this code were measured in round
    1's follow-up (P9: every run exit 0, every record "ok"); W3 adds a first backup and a full
    re-run over SMB, both ok. The unfiltered suite passes (with CI's longer time limit for the two
    largest packages).

## Not tried

- A real NAS, SMB or NFS outage, or a Wi-Fi drop (outages are made with file modes and folder
  permissions on the Mac, a deny entry and a drive letter taken away on Windows). Q3 needs one.
- Linux (the release ships it; the guard's code is the same as on the Mac).
- A signed-in Windows session, and MailArchive Desktop's own Cancel and progress for Q2 and Q5
  (shown at the engine, `app.Run`, which the Desktop calls).
- A Microsoft 365 capture's checks during an outage while it writes (its first write and its saves
  share the local run's proof; round 1's and the committed tests cover a capture with nothing new).

## Final round (2026-11-14, the patch builder; added beside the re-check, nothing above changed)

The lead's calls, one final round on `patch-1.7.2`, with the research note's L0
(`docs/research-windows-secret-store.md` on `research-windows-secrets`) as a release blocker:

| Finding | Outcome |
|---|---|
| L0 | Safety net (MA-1453): when Windows will not set the instance file's Medium no-read-up label, the file is written exactly as 1.7.1 wrote it (owner-only, unlabelled) and the Desktop's log says so in one plain line; MailArchive Desktop still starts. Only when that plain write fails too does it refuse to start, as a dialog (G8's words). Shown on every OS with the refusal stood in for, and on Windows with a real one (a Low-integrity child refused the Medium label). Whether a signed-in person's Medium program sets the label (L0 itself) still needs a signed-in person: nobody was signed in to the VM during the round, so it was not run. What the lab did show, as SYSTEM: Windows refuses a label above the writer's own integrity with "A required privilege is not held by the client." (ERROR_PRIVILEGE_NOT_HELD), the error the fallback knows; the Low child's file was written, read back as an instance file, and the warning given. If L0 fails for a person, 1.7.2 now behaves for them as 1.7.1 did. |
| Q1 | Words: the notes say plainly that a dropout meeting a backup while it writes new mail, between checks, still stops it at once, as before. No code change. |
| Q2 | Fixed (MA-1454): the wait listens to the run's context; a Cancel, Ctrl-C or the Desktop's Shut down ends it at once, the cancel is never kept as the folder's answer, and the run is recorded cancelled where the folder still holds its archive. |
| Q3 | Fixed (MA-1455): a lock file "not found" is "deleted" only when the folder's parent can be looked at and a fresh listing of the folder succeeds without it; otherwise it is "could not read" and waited on. Not reproduced on a real share (the lab cannot make one drop). |
| Q4 | Accepted, as recommended: closes with Gmail de-dup's archive id. |
| Q5 | Fixed (MA-1456): one log line as a wait starts ("waiting up to 30 seconds for the archive folder to be reachable again… (SYSTEM'S WORDS)") and one as it ends. |
| CI time | See the round's report: `ci.yml` and `release.yml` run `go test ./... -timeout 20m`; the round measured `internal/app` and `internal/desktop` on this branch against `main`. |
