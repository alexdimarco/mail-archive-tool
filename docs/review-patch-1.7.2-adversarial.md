# Adversarial pass: the MailArchive 1.7.2 patch

**Verdict: SHIP-WITH-NOTES.** The patch does what it says for the cases it names: a backup whose
drive is swapped, or whose folder is renamed, stops and writes no manifest, record, page or README
into the other archive, on Mac and on Windows, and a sandboxed (Low-integrity) program can no longer
read the Desktop's instance file once 1.7.2's Desktop has started. No finding is a data loss and none
is a regression that breaks a healthy local backup. But one finding (P1) is a new way for a healthy
backup to fail: a network folder that cannot be reached for more than about a second while the
backup reads its mail now fails the backup and sends the failed-backup alert, where 1.7.1 finished
it. The fix is small; I recommend making it in this patch (P1's fix below). The release notes
over-claim in four places (P2, P4, P6, P8) and need narrower words before the release.

| | |
|---|---|
| Reviewed | branch `patch-1.7.2` at `c71467d`, against `main` `1a6d437` (shipped 1.7.1); 7 commits |
| Date | 2026-11-14 |
| Process | assurance-kit `process/adversarial-review.md`. Deviation: one reviewer ran three lenses (healthy-run regressions; swap closure; Low-integrity insider) and then tried to refute each of its own findings by reproduction. Finder and skeptic were not separate people; an independent skeptic on P1 and P2 is advised. |
| Tools | go 1.26.0 darwin/arm64 (Darwin 25.6.0); Windows 11 VM (ARM64, amd64 programs emulated), run as SYSTEM through `prlctl exec`, every lab copy given its own state folders under `C:\ProgramData\p172advlab` |
| Count | 9 found, 9 confirmed (P1, P2, P4 to P8 by reproduction; P3 in the lab's SYSTEM form; P9 measured), 12 attempts refuted, 1 judged small |
| Files (git blob ids at c71467d) | `internal/app/archiveguard.go` b10b471; `internal/app/run.go` 4199fa1; `internal/app/graph.go` 1282a66; `internal/export/exporter.go` febb958; `internal/pages/pages.go` 1a094d7; `internal/state/manifest.go` 98234f9; `internal/container/store.go` 1e995fd; `internal/util/securefile.go` c472ef5; `internal/util/secretfile_windows.go` 9bbcea1; `internal/instance/instance.go` 5f3d0bd; `packaging/release-notes.md` 2627e37 |

## Commands and exit statuses

- Mac, the patch's own guard tests: `go test -count=1 -run '<the guard tests>' ./internal/app/ ./internal/export/ ./internal/pages/ ./internal/instance/ ./internal/util/` → exit 0 (app 147.9 s).
- Mac probes (build tag `advprobe`, never committed; kept in the review worktree at `.claude/testdata/probes/zz_p172adv_test.go`): `go test -tags advprobe -run 'TestP172Adv_' -v ./internal/app/` → exit 0 (probes log, they do not assert).
- Mac baseline probes against 1.7.1's code (`git archive 1a6d437` into `.claude/testdata/base171`, probe `internal/app/zz_p172adv_base_test.go`): `go test -tags advprobe -run 'TestP172AdvBase_' -v ./internal/app/` → exit 0.
- Windows, the patch's own tests built from c71467d (`.winlab/p172adv/tests.ps1`): `TestADriveLetterMovedDuringTheWalkGetsNoManifest`, `TestALockLostDuringASmallRunSavesNoManifest`, `TestAHealthyRunIsNeverStoppedByItsProofs`, `TestADriveLetterMovedInTheRunsTailGetsNoPages`, `TestADriveLetterMovedBeforeAReplaceGetsNothingReplaced`, `TestEveryFolderPageIsProvedBeforeItIsWritten`, `TestALockLostInItsOwnArchiveIsRecordedThere` PASS; the folder-rename forms SKIP by design on Windows; `TestMA1427_ALowerIntegrityProcessCannotReadTheInstanceFile` and `TestMA1427_TheInstanceFileIsWrittenAsASecret` PASS; `internal/util` PASS. The `V:` drive letter the tests use was gone after.
- Windows labs (`.winlab/p172adv/c1adv.ps1`, `netadv.ps1`, `netadv2.ps1`; probe `advprobe.exe` from `.claude/testdata/winprobe/main.go.txt`): exit 0 each. SYSTEM's own `MailArchiveDesktop` folder did not exist before or after any run.

## Findings

| ID | Severity | Lens | Claim | Verdict | Evidence | Fix |
|---|---|---|---|---|---|---|
| P1 | MED | healthy runs | A network folder that cannot be reached for more than ~1.25 s while a backup only reads (an incremental run with nothing new, a Microsoft 365 capture listing) now fails the backup | CONFIRMED (mechanism); real-world frequency PLAUSIBLE | Mac probe, 1.7.1 vs 1.7.2 | below |
| P2 | MED | Low insider | The Desktop's one-time link still reaches a sandbox: a Low process can read another process's command line, and the link travels on rundll32's command line | CONFIRMED (read by pid, VM); the race to use the link first PLAUSIBLE | VM probe | below |
| P3 | HIGH, pre-existing, not a 1.7.2 regression | Low insider | Credential Manager, where MailArchive keeps the Microsoft 365 sign-in, the app secret, the alert email password and remembered encryption keys on Windows, is readable by a Low process | CONFIRMED in the lab's SYSTEM form; user-session form PLAUSIBLE | VM probe | below |
| P4 | LOW | Low insider | After updating, a 1.7.1 Desktop still running keeps its unlabelled instance file readable until it quits; the MSI does not close it | CONFIRMED (VM, lab binaries; MSI part from source) | VM probe + Product.wxs | below |
| P5 | LOW | swap closure | The crash-leftover sweep removes files in the other archive after a swap (no proof before each removal) | CONFIRMED (Mac) | Mac probe P6 | below |
| P6 | LOW | healthy runs | A brand-new archive's first backup whose lock file is removed before its first save records nothing: its record stays "running" | CONFIRMED (Mac, both versions) | Mac probe P4 | below |
| P7 | LOW | swap closure | Two archives whose manifests hold the same bytes are taken for one: the failed record goes into the other | CONFIRMED (Mac) | Mac probe P5 | accept |
| P8 | LOW | healthy runs | A linked archive folder whose drive is pulled says "lock … was removed … run again", not "location … unavailable" | CONFIRMED (Mac, both versions) | Mac probe P3 | below |
| P9 | LOW | healthy runs | Proofs before every overwrite cost ~15% on a full re-run over SMB | MEASURED (VM, SMB loopback) | VM lab | accept |

### P1 (MED, all platforms): a moment's network outage during the read-only part of a backup now fails it

What happens. 1.7.1 did not touch the archive folder while an incremental backup walked mail it
already had (it only reads the source then). 1.7.2 proves the folder every 10 seconds during the
walk (`archiveGuard.dueAfter` → `prove`, `internal/app/run.go` checkpoint, `internal/app/graph.go`
checkpoint, which a Microsoft 365 capture calls on every skipped message too). A proof that fails
three times over 1.25 seconds (`archiveProofRetries`, 250 ms then 1 s) stops the backup as "lock …
was removed during the run … run again" or "the archive location … is unavailable … reconnect the
drive or share", and a scheduled run sends the failed-backup alert. So on a network folder (a NAS,
a mapped drive over Wi-Fi), an outage that returns errors for more than ~1.25 s and overlaps one of
those proofs fails a backup that would have finished. Roughly, an outage of D seconds overlaps a
proof with chance (D − 1.25)/10, and always beyond ~11 s. Most scheduled runs are exactly this kind
of mostly-read walk.

Proof (Mac, `TestP172Adv_P1_BlipDuringReadOnlyWalk` vs `TestP172AdvBase_P1_BlipDuringReadOnlyWalk`;
an incremental run with nothing new; the lock file, or the folder's parent, made unreadable for 2 s
at the first message; the 1.7.2 proof interval set to 0 so a proof lands inside the 2 s):

- 1.7.1, lock unreadable: ends ok, record "ok". Folder unreachable (the outage over before the
  walk ends): ends ok, record "ok".
- 1.7.2, lock unreadable: fails, `lock_lost`, "lock … was removed during the run (… permission
  denied) … run again", record "failed".
- 1.7.2, folder unreachable: fails, `archive_unwritable`, "the archive location … is unavailable
  (lstat …: permission denied) — reconnect the drive or share and re-run", record "failed".

Whether a real SMB or NFS client returns errors (rather than waiting) during a short outage depends
on the client and the cause; Windows fails at once when the network adapter is down (Wi-Fi roaming,
a cable, sleep), so this is PLAUSIBLE for laptops with network archives. The patch's own
`TestAMomentsUnreadableLockIsAskedAgain` covers only a 20 ms outage.

Healthy runs with no outage are not affected: on Windows, 1.7.1 and 1.7.2 backed up 1,500 messages
into a local folder and into the same folder over SMB (`\\localhost\C$`), first run, nothing-new,
full re-run, nothing-new: every run exit 0, record "ok".

Fix (plain words): while nothing has been written since the last proof, do not prove on a timer;
prove right before the next write instead (the overwrite guard already proves before every
overwrite; extend that to "the first new file after a quiet stretch"). And let a proof whose only
answer is "cannot read" keep asking for longer (for example 30 s) while nothing is written, keeping
"the lock holds another run's mark" an immediate stop. Release-note line "tries again for about a
second" then changes with it.

### P2 (MED, Windows): the one-time Desktop link still reaches a sandbox through command lines

What happens. 1.7.2 labels the instance file so a Low-integrity process cannot read the hand-off
key (C1). But the Desktop opens the browser with `rundll32 url.dll,FileProtocolHandler <link>`
(`internal/browser/browser.go:29`), so the one-time opener link (60 s, single use,
`localweb.OpenerTTL`) is on rundll32's command line and then the browser's. On the VM, a Low process
given a Medium process's pid read its whole command line, link included
(`advprobe cmdpid` at S-1-16-4096: `"…advprobe.exe hold 20 http://127.0.0.1:50999/s/ONETIMELINK-…"`,
no error). The Low process could not list processes with the snapshot call (it saw 4), but pids can
be tried in turn. A sandbox that watches for the browser launch can redeem the link before the
browser does and get a Desktop session; the person sees "link already used". The "Copy link"
shortcut (`desktop -copy-link`) puts a 10-minute link (`LinkTTL`) on the clipboard, which Low
processes can generally read: PLAUSIBLE, user-session-only (the VM has nobody signed in).

Fix: release notes say "a sandboxed program can no longer read MailArchive Desktop's private key
file" rather than "can no longer open MailArchive Desktop". Follow-up (not for 1.7.2): open the
browser without the secret in any command line (for example, a short-lived local page that the
running Desktop serves only to the process that holds the key), and say that "Copy link" puts a
working link on the clipboard.

### P3 (HIGH by impact, pre-existing, not a 1.7.2 regression; Windows): Credential Manager is readable from Low integrity

What happens. On Windows, MailArchive keeps its secrets in Credential Manager
(`internal/graphconfig/secretstore_windows.go`): the Microsoft 365 app secret, the delegated sign-in
(refresh token, `DefaultTokenStore`), the alert email password (`internal/backupalert/backupalert.go:81`)
and remembered encryption keys (`cmd/mailarchive/encrypted.go:427`). On the VM a Medium process wrote
a generic credential and a Low process of the same account read it back whole
(`CredRead ok: 20 bytes` at S-1-16-4096). The lab used SYSTEM's token lowered to Medium and Low and a
session-only credential (MailArchive uses "local machine" persistence; not tried, to keep SYSTEM's
stored vault untouched). The same check in a person's session is PLAUSIBLE.

Why it matters here. 1.7.2's C1 accepts "a Low-integrity process of this account" as an attacker.
Against that attacker, the Credential Manager entries give far more than a Desktop session: read
access to the mailbox, the alert account, and keys to encrypted archives. 1.7.2 neither causes nor
changes this, so it does not block the release; it needs an operator call on the threat model and a
design of its own (for example, sealing the entries with a key the Desktop keeps in a labelled file,
or DPAPI with an entropy value held the same way).

### P4 (LOW, Windows): the update leaves a running 1.7.1 Desktop's key readable until it quits

What happens. The MSI's `util:CloseApplication` names `mailarchive-desktop.exe`
(`packaging/mailarchive-desktop/Product.wxs:52-58`), but the Start menu, desktop and Startup
shortcuts run `mailarchivew.exe desktop` (`Product.wxs:151-206`), so installing 1.7.2 does not end a
running 1.7.1 Desktop (the RMM script does stop it; the MSI double-click does not). Lab
(`c1adv.ps1`): with a 1.7.1 Desktop running, a 1.7.2 launch handed off to it, the instance file kept
no label, and a Low process read it, asked a quiet hand-off and redeemed the link (status 200, 2
cookies). After the 1.7.1 copy ended and 1.7.2 started, the file was rewritten with
`S:AI(ML;;NWNRNX;;;ME)`, Low was refused ("Access is denied") and the Medium hand-off still worked.
So the fix takes effect at the next sign-in or Desktop restart, not at "update".

Fix: release notes "after updating, quit MailArchive Desktop (or sign out and back in)"; or add
`mailarchivew.exe` dashboards to what the MSI closes, as `Install-RMM.ps1` already does.

### P5 (LOW, Mac/Linux shown; all platforms in principle): the crash-leftover sweep removes files in the other archive

What happens. A local run sweeps a crashed run's leftovers (`export.SweepOrphans`,
`internal/app/run.go:852`) after the proof that follows the pre-sync, walking the whole archive by
path and removing as it goes, with no proof before each removal. If the drive is swapped while it
walks, it walks on into the other archive. In a rotation (one source backed up to two drives) both
archives have the same folder names, so it reaches the other archive's files. Probe
`TestP172Adv_P6_SweepRemovesInTheOtherArchive`: after the swap the other archive's old
`.mailarchive-stale2.tmp` and a `…-attachments.zip` with no page beside it were both removed; the
run then stopped as a lost lock. Harm is small (those are what the other archive's own next sweep
removes), and 1.7.1 did the same, but it contradicts the release note "right before it changes or
removes any file already there".

Fix: give the sweep the run's proof and ask it before each removal (removals are rare, so it costs
nothing), or word the note "before it changes any message file already there".

### P6 (LOW, all platforms): a new archive's first backup that loses its lock leaves no record

What happens. The record is written after a lost lock only when the folder's manifest still holds
the bytes the run read or wrote (`archiveGuard.sameArchive`). A brand-new archive has neither until
its first save (after 1,000 messages, or the end of the first source), so if its lock file is removed
(no swap) before then, nothing is recorded: the record stays "running", with no run-history entry and
no BACKUP-NEEDS-ATTENTION.txt (probe `TestP172Adv_P4_NewArchiveLockRemovedBeforeFirstSave`). 1.7.1
did not notice the removed lock at all and recorded "ok". The backup still ends failed where it ran
(log, terminal, alert). The release note "the backup stops and is recorded as failed in its archive,
as before" is not true here; `status` will say the last backup did not finish.

Fix: words ("…except a brand-new archive's first backup, shown as not finished"), or treat "the
folder holds no manifest and no lock holder's mark but this run's last" as here. Either is fine.

### P7 (LOW, all platforms): two archives whose manifests hold the same bytes are taken for one

Probe `TestP172Adv_P5_SameBytesManifests`: two new archives whose first runs found no mail have
identical 133-byte manifests. Swapped during a run, the other one received the failed record, a
run-history entry and BACKUP-NEEDS-ATTENTION.txt (and, as the notes allow, the two message files
written before the proof). A drive cloned just before a run is the other case. The harm is a wrong
"failed, lock lost" note in an archive that is empty or a copy. 1.7 has no archive id to tell them
apart; accept and leave to Gmail de-dup's archive id.

### P8 (LOW, Mac/Linux): a linked archive folder whose drive is pulled reads as a lost lock

The proof asks `os.Lstat(out)`, which succeeds on a symbolic link whose target is gone (a Windows
junction the same). Probe `TestP172Adv_P3_SymlinkedOutPulled`, incremental, nothing new: 1.7.2 says
"lock … was removed during the run … run again" (`lock_lost`); 1.7.1 said "could not save the
manifest … file exists" (`manifest_save_failed`). Not a regression in words, but the release note
"a drive pulled out with nothing in its place still says 'the archive location … is unavailable'"
is not true for a linked folder. With new mail (a write first), both versions say "unavailable".
Fix: `os.Stat` after `os.Lstat` in `prove` and `endUnheld`, so a link to a gone target is
"unavailable".

### P9 (LOW, all platforms): the overwrite proofs cost about 15% on a full re-run over SMB

Windows, 1,500 messages kept with `-raw`, archive over SMB loopback: full re-run 5.9 s (1.7.1) → 6.8 s
(1.7.2); on the local disk 1.2 s → 1.2 s; first and nothing-new runs unchanged. Each overwritten
file now costs a proof (a folder stat and a lock-file read), up to three per message. A NAS with real
latency will cost more (PLAUSIBLE). Full runs are chosen by the person, not scheduled by default.
Accept.

## Refuted attempts

- **Closing the stopped run's search index deletes the other archive's `search.db-wal`/`-shm` by
  path** (folder renamed and another put in its place). REFUTED: SQLite skips its checkpoint-and-delete
  on close when the database file has moved (`databaseIsUnmoved`, `SQLITE_FCNTL_HAS_MOVED`). Probe
  `TestP172Adv_P2_IndexCloseRemovesTheOtherArchivesWAL`: the other archive, left with a WAL holding a
  committed row, kept its WAL, its `-shm` and all 5 rows (control without the swap: 5).
- **Windows sharing violations (a scanner, an indexer) fail the proof.** REFUTED: the run holds the
  lock file open for reading and writing, so any other opener must share both, and the run's own read
  cannot be refused; the OS lock sits at byte 2^62, away from the content. The patch's test with the
  file held open by another reader passes on Windows.
- **A network share root breaks the folder check.** REFUTED (VM): `Lstat` of `\\Mac\winlab`,
  `\\Mac\winlab\` and a folder under it all succeed.
- **Healthy runs over SMB end failed.** REFUTED (VM): 16 runs, 1.7.1 and 1.7.2, local and SMB, all
  exit 0, record "ok". (The SMB path reached the same folder as the local runs, so its "first" run
  was in fact a second nothing-new run; its full re-run and nothing-new runs are as labelled.)
- **Clock jumps or sleep make proofs due or not due wrongly.** REFUTED: `due` uses `time.Since` on
  the monotonic clock.
- **Case-insensitive or Unicode-normalised spellings fail the proof.** REFUTED: nothing is compared
  by name; the run uses its own spelling for the folder and the lock (the patch's test runs with the
  path in another letter case).
- **The retries let a swap through.** REFUTED: each retry reads the lock again; a swapped-in folder
  never holds this run's mark; nothing is written while it waits.
- **The run log rotates into the other archive mid-run.** REFUTED: `internal/runlog` rotates only at
  open.
- **The Microsoft 365 history log is written by path after a swap.** REFUTED: `state.OpenHistory`
  keeps a handle.
- **The scheduled run's alert path writes into the other archive.** REFUTED: the alert streak lives
  in the settings folder (`backupalert.ReportDefault`); auto-pause acts only on an unreachable
  source; the record, history, attention file and streaks all go through `endUnheld`.
- **The program-copy download writes into the archive unproved.** REFUTED: it downloads into the
  program cache; the copies are renamed in under `lock.StillHeld`.
- **The label is lost when the instance file is replaced, or an old unlabelled file is kept.**
  REFUTED (VM): a 1.7.2 Desktop start over 1.7.1's unlabelled file leaves `S:AI(ML;;NWNRNX;;;ME)`;
  Low is refused; the Medium hand-off and a second Medium launch work (see P4 for the window before
  that start).
- **The encrypted per-message proof is costly.** Judged small, not measured: two small reads beside
  each message's several sealed writes.

## Not tried

- A real NAS or Wi-Fi outage (P1 is shown with a file made unreadable; how a given SMB or NFS client
  behaves during an outage was not observed).
- Anything in a signed-in Windows user session: the clipboard (P2), Credential Manager with a
  person's own vault (P3), the MSI itself (P4 uses the lab binaries).
- The Microsoft 365 path for P1 and P5 (shown on the local path; the capture calls the same proof on
  every skipped message).
