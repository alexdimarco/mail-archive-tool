# Adversarial pass: Restore to Thunderbird, slice R7 (the Desktop flow) and the R6 settle

**Verdict: NOT CLEAN.** 3 confirmed findings: 1 MED, 2 LOW, no HIGH. The Desktop flow held
everything it was attacked with: the four routes' guards, the one-use token, the server-side tick
and second yes, the plan kept across saves and ended at switches, the folder-id check, and INS-07
(no raw error, no folder name of an encrypted archive on the wire, in Activity or in the record).
A real second Windows account could read **none** of the restored mail. The MED finding is in the
R6 settle's owner-only step: for an instant after the restore's container folder is made, it
still carries Local Folders' "others may read" entry. Another account that opens it in that
instant keeps the right to list it, and so reads the **names** of an encrypted archive's
top-level folders (not their mail). On the Windows VM a second account won that race 5 times out
of 5 (R7-01).

## Header

- Reviewed: branch `restore-thunderbird` at `82d2852` (R7), with the R6 settle `f376772` under it.
  Report written 2026-11-07.
- File blobs at that commit (a finding is stale when its cited file changes):
  `desktop/restoretb.go` d32ee5c, `restoretbscript.go` bf1c62b, `pending.go` 5a12bdb,
  `capture.go` 75793c1, `encryption.go` a96e59e, `jobcontrol.go` 2df4b3a; `tbengine/run.go`
  f1e841e, `plan.go` f95492f, `inputs.go` 3595cf4, `tree.go` 424e806; `tbrestore/write.go`
  4b4856b, `fsys.go` e3c0e7e, `private_windows.go` b5dbc1e, `private_darwin.go` 015dc05,
  `restorelock.go` 9d8a200; `winsec/owneronly_windows.go` af086bb; `lockfile/lockfile.go` 674ed50.
- Who: one agent (Claude, Opus 5.5) as finder and skeptic both, so the finder/skeptic separation
  of the process did **not** hold for this pass: every claim below is a reproduction against the
  running code, not a reading, and each refutation is recorded with its evidence. A second,
  independent finder would still add value on R7-01's fix.
- Order of reading: the sources first (`desktop/restoretb.go`, `restoretbscript.go`, the
  `pending.go` / `capture.go` / `encryption.go` / `jobcontrol.go` / `dashboard.go` changes,
  `maintenance.go`'s job slot, `lifecycle.go`'s drain order, `keys.go`'s remembered open;
  `tbengine/run.go` `Confirmed` and `recheck`, `plan.go` `archiveLockState` and `readerReasons`,
  `inputs.go`; the R6 settle's `tbrestore/private_*.go`, `write.go`'s container steps,
  `winsec.RestrictFolderToOwner`, `restorelock.go`, `lockfile.Held`), then the design (§6.2, §7.3,
  §7.4, §10, §13), then `docs/review-restore-thunderbird-adversarial-r6.md` and its settled
  table, then the builders' notes in the two commits.
- Probes: Go tests in a throwaway clone, `.claude/attack-restore-r7` (not committed):
  `internal/tbengine/zz_r7_attack_test.go`, `zz_r7_attack_windows_test.go`,
  `zz_r7_attack_darwin_test.go`; `internal/tbrestore/zz_r7_attack_test.go`;
  `internal/desktop/zz_r7_attack_test.go`. Each logs `PROBE <id>: …` rather than failing. Fake
  Thunderbird profiles in temp folders only; no real profile on the Mac or the VM was read or
  touched. The real worktree was not changed except for this file.
- Commands and results (go1.26.0):
  - macOS (darwin/arm64): `go -C <clone> test -count=1 -v -run AttackR7 ./internal/tbengine/
    ./internal/tbrestore/ ./internal/desktop/` — exit 0 (probes log); R6's
    `TestRestoredFilesAreReadableOnlyByThisAccount` re-run there — PASS.
  - Windows 11 VM (amd64 test binaries, ARM64 emulated, NTFS temp folder), command line only:
    `zsh .claude/attack-r6-win.sh <clone> .claude/attack-r7-out/win{1,2,3} AttackR7 …`
    (`internal/tbengine`, `internal/tbrestore`, `internal/desktop`) — every probe ran. The second
    account: none existed, so a temporary standard account (`Users` only) was made with `net user
    /add`, used through `LogonUser` + `ImpersonateLoggedOnUser` inside the probe, and deleted
    after (`net user /delete`; no profile folder or profile entry was left, checked). The exFAT
    volume: a 1.6 GB VHDX made and formatted exFAT as SYSTEM (`diskpart create vdisk`,
    `Mount-DiskImage -NoDriveLetter`, `Format-Volume -FileSystem exFAT`, letter R), the probe run
    as SYSTEM from a local copy because that volume lets a standard token add folders but not
    files; then detached and deleted, the probe folders removed (checked). No GUI app or Edge was
    opened or closed; no process not started here was touched.
  - Linux: `docker run golang:1.26` as user 1000, one folder mounted read-write at `/rw` and
    read-only at `/ro` (`.claude/attack-r7-out/linux.sh`) — exit 0 (probes log).
- Count: 15 claims probed: **3 confirmed, 3 refuted, 9 held** (listed under What held).

## Findings, ranked

| ID | Sev | What goes wrong | Mac | Win | Linux | Where |
|---|---|---|---|---|---|---|
| R7-01 | MED | Another account that opens the new container in the instant before it is made owner-only keeps listing it: an encrypted archive's folder names | mechanism shown (no 2nd account) | CONFIRMED 5/5 | not affected | `tbrestore/write.go:182, 213-215`; `private_windows.go`, `private_darwin.go` |
| R7-02 | LOW | An archive reached through a read-only spelling of a writable folder is read with no lock, so a backup through the writable spelling runs during the restore | not run | not run | CONFIRMED | `tbengine/plan.go:468-487`, `run.go:120-129` |
| R7-03 | LOW | A schedule descriptor or last-run planted in an encrypted archive's folder changes the recapture warning | CONFIRMED | CONFIRMED | CONFIRMED | `tbengine/inputs.go:53-61` |

### R7-01 (MED) — The container is born with Local Folders' "others may read" entry

**Scenario.** A shared computer; Local Folders lets other accounts read what is made in it (a
folder on a second Windows drive does by default, R6-01's case). Another account runs a small
program that keeps trying to open `Restored — NAME.sbd`. The writer makes the container with
`Mkdir` (`write.go:182`), which takes Local Folders' inherited entry, and only then makes it
owner-only (`w.private(container, true)`, `write.go:215`). A handle opened in between keeps the
rights it was opened with: Windows checks access when a handle is opened, and a later change to
the folder's access list does not take them back. So that account goes on listing the container
for as long as it holds the handle, and sees every name the restore makes in it: the encrypted
archive's top-level folder names (`Sentbox`, `Zebrafolder`, `Zebrafolder.sbd`) and the marker
names. It cannot open any of them (each is made after the owner-only list, which they inherit),
and names one level further down (`Quokkasub`) stay hidden. The plan told the person the files
are readable only by their account.

**Probes.**
- `TestAttackR7RaceTheContainer` (Windows VM, a real second standard account): five encrypted
  restores, the other account polling the container's path. **Won 5 of 5**; each time its handle
  then listed `.mailarchive-restore .mailarchive-restore-made Sentbox Zebrafolder Zebrafolder.sbd`.
  **CONFIRMED.**
- `TestAttackR7HandleOpenedInTheWindow` (Windows): the mechanism step by step. The other account
  opens the new folder; `winsec.RestrictFolderToOwner` is applied; a file is made in it. A fresh
  open by the other account is "Access is denied" (the twin), reading the file is denied, and
  the handle opened before lists `[Secret Project Name Zebrafolder.sbd]`.
- `TestAttackR7MacContainerBornListable` (macOS): the new folder is born with `group:everyone
  inherited allow list,search,…`; after the entry is removed, a handle opened before still lists
  the name made afterwards. No second macOS account could be made here (no admin rights), so the
  cross-account step is shown by mechanism only.
- Linux: not affected. `Mkdir` is 0700 and a default POSIX access list's mask is then 0, so no
  other account can open it in the window.

**Fix.** Make the container owner-only from the moment it exists, rather than just after:
- Windows: create it with its security descriptor (`CreateDirectory` with the protected
  owner-only `D:P(A;OICI;FA;;;<user>)` in its `SECURITY_ATTRIBUTES`), then check it as now
  (`os.Root` cannot do this; the writer can make this one call by path and then `Lstat` it
  through the root to bind it, as `makePrivate` already restricts by path).
- macOS: make it with its access list given at creation (`mkdirx_np` with an empty `filesec`),
  or make it under a temporary name inside a folder already owner-only and rename it in.
- Simpler, and on every platform: when Local Folders carries an inheritable entry that lets
  others list or read, treat it as a W-READERS reason for an encrypted plan (the copy export's
  `destinationPosture` reads exactly this), so the plan says so and asks twice.
Tombstone: the race probe with a real second account must lose every time. Whichever is
chosen, it is a change to R6-01's settle, so it owes a fresh look of its own.

### R7-02 (LOW) — "Read-only" through one spelling, writable through another

**Scenario.** R6-13 reads an archive on a read-only medium without its lock, because "nothing
can write it there, a backup included". That is true of a read-only medium, not of a read-only
*path*. On Linux a bind mount can show a writable folder read-only at a second place (containers,
sandboxes, `mount --bind -o ro`); a read-only network share whose server lets another account or
computer write is the same case. The plan sees `ST_RDONLY` and takes no lock; a backup through the
writable spelling takes the lock freely while the restore reads.

**Probe** (`TestAttackR7ReadOnlyBindMount`, Linux Docker, `-v dir:/rw -v dir:/ro:ro`):
`readOnlyVolume(/ro)=true`, `readOnlyVolume(/rw)=false`; the plan through `/ro` is `ReadOnly` with
its READONLY line. A backup already holding the lock at Start: refused W-BUSY (held: the check at
Start sees it). A backup's lock taken through `/rw` **while the restore runs: took**, and it
rewrote the manifest; the restore still finished and its check passed (it had loaded the record
at Start). **CONFIRMED.** macOS and Windows: not run (no second spelling of one folder there
without other software).

**Fix.** Hold the lock for the whole run through the read-only handle instead of probing it for
an instant: `flock` and `LockFileEx` both lock a read-only handle, and a bind mount shares the
lock with the writable path, so a backup there is then refused. `lockfile.HeldReadOnly` already
takes it that way; keep it until `Run` returns. The READONLY line can then say a backup waits.

### R7-03 (LOW) — A planted record in an encrypted archive's folder changes the recapture line

**Scenario.** R6-02 reads the archive's own last-run job and its schedule descriptor
(`inputs.go:53-61`). Both are plain files in the archive folder. An encrypted archive keeps no
inputs in its last-run (checked: its file has no `job`) and is never scheduled (O5), so a
`job` there, or a descriptor at all, was put there by someone else — the party an encrypted
archive's storage is meant to be kept from. `ReadDescriptor` does not check that the descriptor's
`-out` or host is this archive's, as `ReadVerifyDescriptor` does.

**Probe** (`TestAttackR7PlantedSourcesInEncryptedArchive`, all three platforms): before, the plan
starts with "MailArchive cannot tell whether this archive backs up this Thunderbird profile…". A
planted descriptor naming another input: the line is **gone** (`Known` true). A planted last-run
with `-auto`: W-RECAPTURE for every profile, and Desktop pre-selects none. **CONFIRMED.**
Effect: a warning missing or one too many; no mail is touched (backups skip the restored folder
anyway, §7.2).

**Fix.** For an encrypted archive, read sources from Desktop job files only (this computer's,
owner-only). For a plain archive, take a descriptor only when its `-out` is this archive and its
host is this computer, as the verify descriptor's reader does.

## Refuted (each with the evidence)

| Claim | Evidence |
|---|---|
| R7-C1 first run: on Linux a held restore lock reads as not running | Probe error: the test package's helper takes Thunderbird's `fcntl` lock, while the restore lock is `flock` (`lockfile/lock_unix.go:16-17`); on Linux the two do not see each other, on macOS they do. Re-run with a child that calls `AcquireRestoreLock` itself: running=true while held, on all three platforms. |
| After a Lock, the restore's record stays readable on `/api/activity` (as C-15 clears a Run now's) | Not a finding: the copy, the same kind of job, keeps its result after a Lock by design (`runnow.go:251-260`), and the record of an encrypted restore names no folder of the archive (`TestAttackR7EncryptedWire`: plan lines, running and ended Activity, and the record after the Lock carry none of the three names; macOS, Windows, Linux). |
| A restore plan kept across a switch starts on the archive left | Only two places change the current archive (`dashboard.go:696` the settings save, `:722` `afterSwitch`, also called from the delete paths `archivedelete.go:620, 1139`); both end every restore plan, and MA-1130's test drives both. |

## What held

- **The four routes' guards** (`TestAttackR7RouteTampering`, three platforms): a 70 KiB body 413,
  GET 405, no CSRF token 403, a foreign Origin 403, a foreign Host 403, bad JSON 400, on each of
  `/profiles`, `/folders`, `/plan`, `/start`.
- **List, position and folder tampering:** position -1, 2 (past the end), 1e9, and a forged list
  id are W-PROFCHANGED, at `/plan` and `/folders`; folder ids `../../../etc`,
  `Mail/Zebrafolder/../Sentbox` and `""` are "no longer in this archive"; no folder is "nothing
  ticked". Ids are only looked up, never joined into a path.
- **Duplicate and overlapping folder ids** (`TestAttackR7DuplicateFolderIDs`): parent, parent,
  child, child, parent plans 3 messages, the same as parent alone; a run of a list with repeats
  wrote each message once (4 of 4).
- **The one-use token raced** (`TestAttackR7TokenRace`): 32 concurrent starts of one token, exactly
  one ok, one restored folder.
- **Local Folders moved after the plan** (`TestAttackR7LocalFoldersMovedAfterPlan`, prefs.js
  pointed at another folder before Restore): "This changed since you clicked Check …", nothing in
  either folder. An encrypted plan's re-check also repeats the may-leave and other-readers tests
  (`run.go:328-339`), so a folder that became synced is caught at Start.
- **INS-07** (`TestAttackR7EncryptedWire` and reading): the raw wire of `/folders` carries the
  tree (the person's own choice list, shown while unlocked), and nothing else carries a folder
  name: not the plan lines, not Activity while running or after, not the record. A non-refusal
  error is fixed words (`restoreWords`), the job's own failure text is a `spokenErr`, and a panic
  in the run is recovered in `restoreJob` before the job slot's own recover (which would print
  it) can see it. `/start`'s one raw `err.Error()` (`restoretb.go:339`) is only ever the slot's
  two fixed refusals.
- **The second Windows account** (`TestAttackR7SecondAccountReads`): after an encrypted and a plain
  restore into a Local Folders that lets Users read, the other account reads Thunderbird's own
  Trash and lists Local Folders (twins), and gets "Access is denied" on the container and on every
  file and folder in it, by fresh path.
- **A real exFAT drive** (`TestAttackR7RealExFAT`, VHDX): the encrypted plan reads `exFAT`, adds
  W-READERS and asks twice; Start without the second yes is W-NOTAGAIN, nothing written; confirmed
  twice it restores, and the other account can then read it, as the plan said. A plan that
  misread the drive as NTFS stops at the owner-only step with the R6-01 words and removes what it
  wrote (nothing left) — fail-closed.
- **A crashed restore's lock** (`TestAttackR7StaleRestoreLock`, three platforms): held by a child,
  "running" is true; the child killed (kill -9 / TerminateProcess), the file is left but running is
  false and a new restore takes it, so uninstall is not blocked for ever. A planted folder or an
  unreadable file of a lock's name does not read as running.
- **The Lock reply and keys** (reading, and MA-1128's test): the reply names the running restore;
  the plan holds the registry's open by reference only, so a Lock destroys it and Start is
  W-LOCKED; the run's own lease is destroyed on every return of `Run`; a remembered key is not
  re-opened silently while the person holds the archive locked (`keys.go:285-303`).
- **Shut down mid-run** (reading, and MA-1131's test): the keys lock first, the run keeps its own
  lease, the Desktop's context stops it, and the drain waits for its cleanup within the 15-second
  budget; past that, what is left is the hidden folder the next restore removes (§8).

## Not run

- A second macOS account (no admin rights here); R7-01's macOS leg is by mechanism.
- R7-02 on macOS and Windows (a read-only SMB share another account writes is the Windows case).
- Linux with a second account (mode 0700 and the access-list mask were read, not raced).

## Gates

This pass writes docs only; no fix is made here. Each confirmed finding needs its probe turned into
a real test with a `covers:` marker (MA-1128 for R7-01, MA-1127 for R7-02, MA-1124 for R7-03),
shown red against `82d2852` and green after the fix, then a fresh look at the patch. R7-01 changes
R6-01's settle and has a choice in it (make the folder owner-only at birth, or ask twice when
Local Folders lets others in): the lead's call. The friction review of the Desktop flow and the
X-series walk are still owed, separately.

## Settled 2026-11-07 (the lead's calls)

Settled by one agent (Claude, Opus 5.5), the same model that ran this pass; the fresh look this
pass asks for on R7-01's patch has **not** been done by an independent finder (see Still owed).

| ID | Lead's call | What was done | Test (covers) | Prove-fail | Commit |
|---|---|---|---|---|---|
| R7-01 | Private from creation; verify after; ask twice where it cannot be guaranteed; prove with a second account, 0 of 20 | Windows: one `NtCreateFile` per entry, relative to its parent through the root, `FILE_CREATE`, a protected owner-only descriptor in the call. macOS: `mkdir_extended` / `open_extended` with an empty access list flagged `KAUTH_ACL_NO_INHERIT` for what is made in Local Folders itself, then bound to the root; the rest through the root inside the non-inheriting container. Linux unchanged (0700/0600 masks a default ACL). `FS.Private` still reads each back. New ask-twice reason: a macOS drive set to ignore ownership (`OwnersIgnored`, `wReadersOwners`) | `tbengine/ma1128_r701_born_private_test.go` (each entry read the instant its create returns, others granted read, umask 0); `ma1128_r701_owners_darwin_test.go` (a real `-owners off` image) — MA-1128 | old folder create: red macOS, red Windows; no-inherit flag off: red macOS; ignore-ownership reason off: red. Survived: the descriptor without "P" (equivalent at creation; `FS.Private` sets it) | 475b0d6 |
| R7-01 lab | — | Windows VM, temporary standard account `r8probe` (made with `net user /add`, deleted after, no profile folder left), 20 encrypted restores raced, about 5,000 opens each: **0 of 20** handles, 0 fresh reads, Thunderbird's own Trash readable 20 of 20 (twin). Against the old create: **19 of 20** won. macOS with a second account: not run (no admin rights) | lab test kept outside the tree (`.claude/r8-lab-src/`) | — | 475b0d6 (message) |
| R7-02 | Keep the archive-lock decision held for the whole restore; test the bind case on Linux | On a read-only path the archive's lock is held through a read-only handle (`lockfile.HoldReadOnly`) from Start until `Run` returns; the read-only check and the record are re-checked under it; an archive with no lock file reads without one, and the plan says which | `tbengine/ma1127_r702_readonly_lock_test.go`: the seam everywhere; a real bind mount on Linux in Docker (`MA_RO_BIND`) — MA-1127 | the lock released right after it is taken: red (seam, macOS), red (real bind mount, Linux) | 0a6b346 |
| R7-03 | For encrypted archives, ignore the folder's plaintext schedule and last-run files | `ArchiveSources(out, encrypted)`: for an encrypted archive only this computer's Desktop job files are read; the plan reads an encrypted archive's sources itself whatever its caller passed. The plain-archive suggestion (bind a descriptor to this archive and host) was not in the call and is not done | `tbengine/ma1124_r703_planted_sources_test.go` — MA-1124 | the files read for an encrypted archive: red; the plan trusting the caller's sources: red | 0c24ed4 |

Runs of the settle (2026-11-07): macOS, the unfiltered `go test -count=1 -p 4 -timeout 60m ./...`
— exit 0. Windows VM (`winrun-at.sh`), `internal/tbrestore` and `internal/tbengine` PASS;
`internal/desktop` only the known baselines (`TestTheRecoveryFileIsReadBounded`,
`TestAWedgedRunIsFinishedAsPartialAfterARestart`, `TestReconciliationIsOffTheRequestPath`). Linux
in Docker (`golang:1.26`, the worktree mounted read-write): `tbrestore`, `tbengine`, `assureblock`
ok; `desktop` with `/tmp` as a tmpfs (the container's own `/tmp` now reads as a FUSE mount to the
folder-identity guard, at the starting commit too) fails only
`TestScheduledBackupCardSaysNotInstalledOnlyForAnArchive` (the baseline),
`TestTheCardNamesAFailedCopysFolder` and `TestARepliesSayWhatThePageCanStillDo` — the same three
at the starting commit 9572721 in the same container (it runs as root, so a permission-based fault
does not bite). The page-script scenarios need Node: they ran on macOS only.

Still owed: an independent finder's look at the R7-01 patch (`create_windows.go`,
`create_darwin.go`), as this pass asked; a macOS run with a second account; R7-02 on macOS and
Windows (a read-only share another account writes). The macOS bind check after
`mkdir_extended` (a folder swapped between the path create and the root's look) has no test: no
seam reaches that window.
