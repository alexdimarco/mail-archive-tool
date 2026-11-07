# Adversarial pass: Restore to Thunderbird, slice R6 (and a fresh look at the R3–R5 settle)

**Verdict: NOT CLEAN.** 16 confirmed findings: 3 MED, 13 LOW, no HIGH. Nothing found deletes
the person's mail or writes outside the new folder. The three MED findings are about what the
person is told: decrypted mail that other accounts on the computer can read, with nothing in the
plan saying so (R6-01); a restore whose "this archive backs up this profile" warning is missing,
so mail later filed into the restored folder is silently left out of backups (R6-02); and a
restore that is still running being reported as "did not finish", with advice to delete its
folder (R6-03). The R3–R5 fixes held, including ADV-02's by-handle delete on Windows with real
symbolic links.

## Header

- Reviewed: branch `restore-thunderbird` at `408e6bc` (R6), with the settle `c75ebee` under it.
  Report written 2026-11-07.
- File blobs at that commit (a finding is stale when its cited file changes):
  `tbengine/run.go` 64c5a5f, `tbengine/plan.go` 02a1c04, `tbengine/inputs.go` 353d0cd,
  `tbengine/words.go` a2cac21, `tbengine/elevated_windows.go` d05ce31,
  `cmd/mailarchive/restoretb.go` a144d0c, `keyring/extract.go` db75762, `lockfile/lockfile.go`
  674ed50, `app/busy.go` 1cd88ec, `tbrestore/write.go` 590a97d, `fsys.go` f4a39f9,
  `fsys_windows.go` a66d9ab, `localfolders.go` 596b9a9, `leftovers.go` 8eeb9f8, `skipset.go`
  a897970, `tblock.go` 5e3fc43, `profiles.go` 4b26519, `thunderbird/restorekey.go` 5d1378d.
- Who: three finders, each a separate agent (Claude, Opus 5.5), read-only, each with one lens and
  not seeing the others' reports: **CON** (integrity and races), **INS** (insider: another program
  or account on the same computer, planted files, a careless or scripted caller), **PLT**
  (platform and file system). One skeptic (Claude, Opus 5.5, the synthesis owner) reproduced or
  refuted every claim with a Go test probe. The skeptic also raised six claims of its own (SKP-…)
  before the finders reported; those were judged by the same agent, so for them the
  finder/skeptic separation did not hold — each is a reproduction, not a reading.
- Order of reading: the sources first (`internal/tbengine`, the lock order, the re-check,
  `keyring.LeaseOriginals`, the `restore-thunderbird` verb, `lockfile`'s verb cap, the ADV-01..07
  fixes in `internal/tbrestore` and `internal/source`), then `docs/design-restore-thunderbird.md`
  (§3.1, §5.2–§5.4, §6.2, §7.1, §8, §9, §10, §11, §13) and the R3–R5 review with its settled
  table, then the builders' notes. The builder's attack list was read with the task.
- Probes: Go tests in a throwaway clone, `.claude/attack-restore-r6` (not committed):
  `internal/tbengine/zz_attack_test.go`, `zz_attack2_test.go`, `zz_attack_darwin_test.go`,
  `zz_attack_windows_test.go`; `internal/source/zz_attack_test.go`;
  `cmd/mailarchive/zz_attack_cli_test.go`, `zz_attack_sig_test.go` (Linux only). Fake profiles in
  temp folders only; no real Thunderbird profile touched. The real worktree was not changed except
  for this file. Each probe logs `PROBE <id>: …` rather than failing, so its result is read from
  `-v` output.
- Commands and results:
  - macOS (go1.26.0 darwin/arm64): `go -C <clone> test -count=1 -v -run Attack
    ./internal/tbengine/ ./internal/source/ ./cmd/mailarchive/` — exit 0 (probes log).
  - Windows 11 VM (amd64 test binaries, ARM64 emulated, NTFS temp folder):
    `zsh .claude/attack-r6-win.sh <clone> .claude/attack-r6-out/win2 Attack internal/tbengine
    internal/source` — both PASS (probes log). `cmd/mailarchive` cannot run on the VM.
  - Windows, ADV-02 with real symbolic links: Developer Mode switched on for the run (registry
    value `AllowDevelopmentWithoutDevLicense` = 1) and removed again after;
    `… win1 NeverFollowsASwappedLink internal/tbrestore` — PASS.
  - Linux: `docker run golang:1.26` as user 1000, `go test -count=1 -v -run Attack
    ./internal/tbengine/ ./internal/source/ ./cmd/mailarchive/` — exit 0.
- Count: 27 claims after merging duplicates (CON 9, INS 7, PLT 10, SKP 6, with 5 overlaps):
  **16 confirmed, 5 refuted, 6 not reproduced or not run.**

## Findings, ranked

| ID | Sev | What goes wrong | From | Mac | Win | Linux | Where |
|---|---|---|---|---|---|---|---|
| R6-01 | MED | An encrypted archive's decrypted mail can be readable by other accounts, unsaid | INS-02, PLT-02 | CONFIRMED | CONFIRMED | not affected (mode masks) | `plan.go:326-331`, `run.go:258-264` |
| R6-02 | MED | "This archive backs up this profile" missed when no job file names the archive | SKP-A2 | CONFIRMED | CONFIRMED | CONFIRMED | `inputs.go:16-41`, `plan.go:349` |
| R6-03 | MED | A running restore is reported as "did not finish", with advice to delete it | CON-02 | CONFIRMED | CONFIRMED | CONFIRMED | `leftovers.go:54-70` |
| R6-04 | LOW | A backup that started during a restore backs up the finished restored folder | CON-01 | CONFIRMED | CONFIRMED | CONFIRMED | `skipset.go:120-124` |
| R6-05 | LOW | The profile folder swapped after the re-check: the real profile is not locked | CON-04, PLT-07 | CONFIRMED | CONFIRMED | CONFIRMED | `run.go:132-140` |
| R6-06 | LOW | Two profiles sharing one Local Folders: only one is held out | CON-05, PLT-06 | CONFIRMED | CONFIRMED | CONFIRMED | `tblock.go:80-101` |
| R6-07 | LOW | Local Folders' own owner and mode are never checked | INS-01, PLT-01 | CONFIRMED | no check exists | CONFIRMED | `localfolders.go:56-99` |
| R6-08 | LOW | `-record-out` can write a plaintext record inside the encrypted archive | INS-03 | CONFIRMED | not run | CONFIRMED | `restoretb.go:129-135, 239-249` |
| R6-09 | LOW | A second Ctrl-C mid-write exits 130 saying nothing, leaving the hidden folder | INS-06, CON-03 | CONFIRMED | not run | CONFIRMED | `restoretb.go:174`, `main.go:1104-1106` |
| R6-10 | LOW | Ctrl-C is not heard during the read-back check and the commit | CON-03 | CONFIRMED | CONFIRMED | CONFIRMED | `write.go:214, 317, 349-488` |
| R6-11 | LOW | The store kind (mbox or maildir) is not checked again at Start | CON-06 | CONFIRMED | CONFIRMED | CONFIRMED | `run.go:224-266` |
| R6-12 | LOW | An encrypted archive's folder names in the plan's W-FAT32 and W-LONGPATH words | INS-04 | CONFIRMED | CONFIRMED | CONFIRMED | `plan.go:308-309, 370` |
| R6-13 | LOW | The plan says yes, Start fails with a raw error, when the lock file cannot be opened for writing | SKP-C | CONFIRMED | CONFIRMED | CONFIRMED | `plan.go:194`, `run.go:119-125` |
| R6-14 | LOW | A junction in the profile's path refuses every restore, with wrong words | PLT-03 | n/a | CONFIRMED | n/a | `profiles.go:189-191`, `localfolders.go:56-58` |
| R6-15 | LOW | One profile, two spellings, two keys (restore lock, settle scope, presync) | CON-08, PLT-04 | CONFIRMED | CONFIRMED | not run | `thunderbird/restorekey.go:30-41` |
| R6-16 | LOW | A parent name taken after the re-check is found only after the whole write | SKP-B | CONFIRMED | CONFIRMED | CONFIRMED | `write.go:164-186, 434-456` |

### R6-01 (MED) — Decrypted mail inherits who may read Local Folders, and the plan does not say

**Scenario.** An encrypted archive is restored into a Local Folders whose folder lets other
accounts read what is made in it. On Windows, new files take the folder's inherited permissions
(design §9.6 says so); a Local Folders moved to a second drive (`D:\Mail`) gets, by Windows'
default for such a drive, read and change rights for every signed-in account. On macOS an
inheritable access entry ("everyone may read") applies whatever the 0600/0700 modes say. The copy
export, which makes the same kind of decrypted output, works this out before writing
(`app/copyexport_posture.go:293`, `:307`) and asks or refuses; the restore's only extra question
is "may it leave this computer" (synced, removable, network share: `plan.go:326-331`). The plan's
line says "anyone who can open this Thunderbird profile can read it", not "every account on this
computer can".

**Probe** (`zz_attack_windows_test.go`, `zz_attack_darwin_test.go`): Local Folders given an
inheritable read entry (Windows: `icacls … /grant *S-1-5-32-545:(OI)(CI)R`; macOS: `chmod +a
"everyone allow read,…,file_inherit,directory_inherit"`), an encrypted restore, the restored
file's access list read back.
- Windows: the decrypted `Sentbox` file carries `BUILTIN\Users:(I)(R)`; the plan names nothing.
  **CONFIRMED.**
- macOS: the file carries `group:everyone inherited allow read`, the folder `list,search`.
  **CONFIRMED.**
- Linux: not run; mode 0600 limits a default access list there (PLT-02's reading).

**Fix.** Run the copy export's posture check (`destinationPosture`, then `realizedPosture` on the
first file made) on the Local Folders of an encrypted plan. When others could read it, add it to
the ask-twice reasons ("every account on this computer can read files made there") so Start needs
the second confirmation, or refuse as the copy does. Owner question if the choice is between the
two. §9.6 is worth a dated note: it was written before the encrypted path.

### R6-02 (MED) — The W-RECAPTURE line is missing for archives backed up without a job file

**Scenario.** Whether "this archive backs up this profile" is decided only from job files in
MailArchive's settings folder (`ArchiveInputs`, `inputs.go:16-41`, then `BacksUp`,
`plan.go:349`). An archive backed up by hand at the command line, or scheduled with
`mailarchive schedule` (which keeps its job in the system scheduler, not in a job file:
`cmd/mailarchive/main.go:1278-1300`), has none. So the plan leaves out W-RECAPTURE, whose words
are the only place the person learns "Mail you later move into that folder in Thunderbird is not
backed up while it is there". Backups then do leave that folder out (owner's O1, as built), so
mail the person files into it is silently not backed up.

**Probe** (`TestAttackRecaptureMissed`): a backup of the profile's Local Folders with no job file;
the plan's codes; a message filed into the restored folder; the same backup again.
- Plan codes `[DEST WHAT DAMAGED-LATER SPACE CLOSED SCHEDULED CHECKS W-WAIT]` — no W-RECAPTURE.
  Twin: with a job file naming the same input, W-RECAPTURE is there.
- After the second backup the archive still holds 2 records: the newly filed message is not in it.
- **CONFIRMED** on macOS, Windows and Linux.

**Fix.** Also read the archive's own record of how it was last backed up: its last-run record
keeps the run's inputs (`state.LastRun.Job`, written from `canonicalJob`, `app/run.go:1234`,
`:1337`). For an encrypted archive, read what the lease allows. When the inputs cannot be known,
say so in the plan instead of saying nothing ("MailArchive cannot tell whether this archive backs
up this profile; if it does, …"). The design's §3.1 step 4 rule ("no input of any job for this
archive") needs a dated note, since it is the rule that misses these.

### R6-03 (MED) — A restore that is still running is reported as one that "did not finish"

**Scenario.** `tbrestore.Leftovers` names every restore with no closing line as W-UNFINISHED
(`leftovers.go:54-70`) and never asks whether that profile's restore lock is held. Its callers are
Desktop on every page load (`desktop/restorenotices.go:16`), `status` (`cmd/mailarchive/status.go:113`)
and uninstall's plan (`uninstall/uninstall.go:59`). So while a terminal restore is writing, Desktop
and `status` say it did not finish and tell the person to "close Thunderbird and delete the folder
'Restored — … .sbd'" — the folder being written. Uninstall's plan says the same, and uninstall does
not refuse: it removes `restores.log` while the restore still appends to it.

**Probe** (`TestAttackCON02LiveRestoreReportedUnfinished`): `Leftovers` called from inside the
write (at the first folder). It returns `kind=unfinished` with the delete advice, and for
uninstall adds "Nothing will clean it up after MailArchive is removed". The run then finishes
`done`. **CONFIRMED** on macOS, Windows and Linux.

**Fix.** In `Leftovers`, pass over a restore with no closing line while
`RestoreLockHeld(r.Started.ProfileDir)` is true (or name it as "running now"). Uninstall refuses
while any profile's restore lock is held, as delete refuses while Desktop runs.

### R6-04 (LOW) — A backup whose skip list was read during a restore backs up its finished folder

**Scenario.** Another archive's backup of the same profile (the restore's archive lock does not
stop it) loads its skip list from `restores.log` once, at its start (`app/localrun.go:154`). The
list re-reads the log only for a restore id it has never seen (`skipset.go:120-124`). A restore it
saw while open stays "open, not committed" for the rest of that backup. If the restore commits
before the walk reaches Local Folders, its folder now has its parent file beside it, so neither
skip rule applies and the restored mail is copied into that other archive (R41: "never backs up
its own restore leftovers" and O1's committed skip).

**Probe** (`internal/source`, `TestAttackCON01StaleSkipSet`): the list loaded while the restore
was open (walk: left out, correct); then the "checked" and "ended" lines and the parent file
appear; the same list walks again: `Restored — Live/Inbox: restored mail` is backed up; a fresh
list leaves it out. **CONFIRMED** on macOS, Windows and Linux. Whether real backup timing hits it
was not measured.

**Fix.** In `Lookup`, for a known entry that is open or not committed, re-read the log when it has
changed (`s.changed()`), as for an unknown id.

### R6-05 (LOW) — The profile folder swapped after the re-check leaves the real profile unlocked

**Scenario.** The re-check pins the profile by identity (`run.go:232`), then Thunderbird's lock is
taken by path (`run.go:135`, `tblock.go:80-101`) and only Local Folders is pinned again
(`OpenLocalRoot`, `run.go:140`). When Local Folders is an absolute folder outside the profile, a
program of the same user that renames the profile folder and puts a decoy at its path in that
window gets the decoy locked and the real profile left open to Thunderbird during the write.

**Probe** (`TestAttackCON04ProfileSwappedAfterRecheck`): the swap made inside the re-check's last
step (the network probe seam); mid-write a child process tries both locks: real profile's
**took**, decoy's refused; the run finishes `done`. **CONFIRMED** on macOS, Windows and Linux.

**Fix.** After `TakeProfileLock`, read the profile folder's identity again (and on macOS/Linux that
the locked file's identity is inside it) and refuse W-CHANGED on a difference, releasing the lock.

### R6-06 (LOW) — Two profiles that share one Local Folders are not both held out

**Scenario.** Only the chosen profile's lock is taken; "no Thunderbird runs" is checked once, at
the take (`tblock.go:96`). A second profile whose prefs point at the same Local Folders can be
opened by Thunderbird during the write. R40's "writes only while Thunderbird is closed and held
out" does not hold for that Thunderbird.

**Probe** (`TestAttackCON05SharedLocalFolders`): profiles A and B with one absolute Local Folders;
mid-write, `CheckRunning(B)` says not running and another process **takes** B's lock.
**CONFIRMED** on macOS, Windows and Linux.

**Fix.** Rare configuration; cheapest is a plan line, or a refusal, when another listed profile's
Local Folders is the same folder by identity; or take every such profile's lock.

### R6-07 (LOW) — Local Folders' own owner and mode are not checked

**Scenario.** The profile folder must be this user's and not group- or world-writable on macOS
and Linux (`profiles.go:170`, `profiles_unix.go:24-30`), but the folder actually written, Local
Folders, is not checked when prefs point it elsewhere (`localfolders.go:56-99`). Another account
that can write there can swap the restored folder for its own after the commit, and Thunderbird
shows that as "Restored — …". (Such an account could already plant any folder there; the restore
adds a trusted-looking name.) On Windows there is no owner check at all, as the design concedes for
the profile (§9.4).

**Probe** (`TestAttackINS01WorldWritableLocalFolders`): Local Folders at a 0777 folder outside the
profile: the plan and the run go ahead (`done`). **CONFIRMED** on macOS and Linux; on Windows the
same code path has no check.

**Fix.** Apply the profile's W-PROFMODE test to the resolved Local Folders too (macOS, Linux); on
Windows, R6-01's posture check covers the read side.

### R6-08 (LOW) — `-record-out` writes a plaintext record into the encrypted archive

**Scenario.** `saveTBRecord` (`restoretb.go:239-249`) only creates a new file, 0600. `delete-archive`
checks the same kind of flag with `checkSavedFile` → `keyring.CheckNewFileOut` (not inside an
encrypted archive, not over a link; `archivedelete.go:693-696`). So `restore-thunderbird -out ENC
-plan -record-out ENC/rec.txt` puts a plain file inside the encrypted archive naming the profile,
the Local Folders path, the restored folder's name and the counts (plus R6-12's folder names when
they apply).

**Probe** (`TestAttackINS03RecordInsideEncryptedArchive`): exit 0, a 1.5 KB plaintext record in
the archive folder. **CONFIRMED** on macOS and Linux (the verb cannot run on the VM). That the 0600
does nothing on Windows (the file takes its folder's permissions) is a reading, not run.

**Fix.** Use `checkSavedFile`'s rules (or `keyring.WriteNewFile`, which also restricts the file to
its owner on Windows) for `-record-out`.

### R6-09 (LOW) — A second Ctrl-C mid-write exits 130 and says nothing

**Scenario.** The verb's signal watcher exits the process at once on a second signal
(`restoretb.go:174` passes `os.Exit` to `watchVerifyStop`, `main.go:1104-1106`). No deferred
release runs (the system frees the locks, which is fine) and no words are printed. The hidden
`Restored — … .sbd` folder stays until the next Start cleans it; for an encrypted archive it holds
decrypted mail (whatever had left the 256 KiB write buffer), on a drive that may be the removable
one the person confirmed twice for. Design §10 says for 130/143 "the words name anything left
behind".

**Probe.** `TestAttackINS06SecondCtrlC`: a child process stopped inside a progress callback mid-write,
two interrupts 100 ms apart: exit 130, no "The restore was stopped" words, `Restored — Locked
mail.sbd` with its marker, made-list and `Inbox` left in Local Folders. **CONFIRMED** on macOS and
Linux. Through the real program (`zz_attack_sig_test.go`, Linux, 40 messages of 8 MiB, two SIGINTs
0 ms and 30 ms apart) the first stop and its cleanup finished before the second signal mattered:
exit 130, the words, nothing left — so the window is real but narrow.

**Fix.** On the second signal, print one line before exiting ("Stopped at once: a hidden folder may
be left in PATH; the next restore removes it, or delete 'Restored — NAME.sbd' yourself"). Or ignore
the second signal for the short cleanup and say "Stopping…".

### R6-10 (LOW) — Ctrl-C is not heard during the read-back check and the commit

**Scenario.** The writer checks for a stop before each folder and each message (`write.go:214`,
`:317`), not during the post-write check, which re-reads every byte (`:349-376`), nor the commit
(`:432-488`). A Ctrl-C there is ignored and the restore finishes. The result is honest (exit 0,
"Restored N messages"), but on a large restore the person who pressed Ctrl-C sees nothing happen
for as long as the re-read takes, and so presses it again (R6-09).

**Probe** (`TestAttackCON03CancelDuringCheck`): cancelled at the check's first open: outcome
`done`, exit code 0. **CONFIRMED** on macOS, Windows and Linux.

**Fix.** Check the context between files of the check (and between messages within one); a stop
there is a stop with the one cleanup, as before step 8. Say "Checking what was written…" on stderr
when the check starts, so a pause is explained.

### R6-11 (LOW) — The store kind is not checked again at Start

**Scenario.** The plan refuses a maildir Local Folders (W-MAILDIR, `plan.go:222`), but the re-check
(`run.go:224-266`) does not repeat `CheckStoreKind` (nor W-INSIDEENC). A Thunderbird run between
Check and Start that switches the store is not noticed and mbox files are written into a maildir
store.

**Probe** (`TestAttackCON06StoreKindChangedAfterPlan`): `storeContractID` changed to maildir after
the plan: a fresh plan says W-MAILDIR; the old plan's Start writes (`done`). **CONFIRMED** on macOS,
Windows and Linux.

**Fix.** Re-read prefs in the re-check and run `CheckStoreKind` (and the W-INSIDEENC test) again;
any difference is W-CHANGED.

### R6-12 (LOW) — An encrypted archive's folder names in plan and refusal words

**Scenario.** W-FAT32 (`plan.go:308-309`, every platform) and W-LONGPATH (`plan.go:370`, Windows)
name the archive's folders for an encrypted archive too. They go to stderr (often a log for an
unattended run) and into `-record-out`. Design §6.2's counts-only rule lists the progress, Activity,
`restores.log` and the damaged list, not the plan; and says Desktop's Save tells the person the
record names the restored folders. So this is a gap in the rule's reach more than a break of it.

**Probe** (`TestAttackINS04EncryptedFolderNamesInPlan`, FAT32 via the `fsTypeOf` seam): the
refusal reads "Sentbox is 369 B, too big for the drive…". **CONFIRMED** on macOS, Windows and Linux.

**Fix.** For an encrypted plan, give counts in these two lines ("1 folder is 4 GB or more…", "3
folders are nested too deep…"), or a lead's call that on-screen words may name folders, written in
§6.2, with `-record-out`'s help saying the record may name them.

### R6-13 (LOW) — The plan says yes and Start fails with a raw error when the lock cannot be opened

**Scenario.** The plan's busy check (`lockfile.Held`, `plan.go:194`) answers "not held" when the
lock file cannot be opened; Start's `AcquireAs` then fails and the raw error is returned
(`run.go:119-125`): "open lock …/.mailarchive.lock: permission denied". It happens for an archive
on a read-only medium or one belonging to another account. Extract has the same limit.

**Probe** (`TestAttackReadOnlyLockFile`, the lock file made read-only): plan OK; Run: no refusal
code, "permission denied" (Windows "Access is denied"). **CONFIRMED** on macOS, Windows and Linux.

**Fix.** Use `lockfile.Probe` at the plan (it errors when the file cannot be probed) and refuse in
words ("MailArchive cannot take this archive's lock: the folder is read-only for this account…").
Restoring from a read-only archive is a real case (a backup drive mounted read-only); whether to
support it without the lock is an owner question.

### R6-14 (LOW) — A junction in the profile's path refuses every restore on Windows

**Scenario.** `CheckProfile` and `FindLocalFolders` call `filepath.EvalSymlinks` directly
(`profiles.go:189-191`, `localfolders.go:56-58`). With Go 1.23 and later it fails on a junction in
the middle of a path; the repo has its own resolver for this (`util/finalpath_windows.go`). So a
profile moved with `mklink /J` (for example `%APPDATA%\Thunderbird` → `D:\TB`) is refused, with
words that send the person the wrong way.

**Probe** (`TestAttackPLT03JunctionInProfilePath`): a profile reached through a junction: refused
W-NOTPROFILE, "… is not a Thunderbird profile (it holds no prefs.js)"; through the real path the
plan is fine. **CONFIRMED** on Windows.

**Fix.** Resolve with `util`'s final-path resolver (as the overlap checks do) everywhere
`tbrestore` and `thunderbird.ProfileKey` resolve links; add a junction case to MA-1085/MA-1140.

### R6-15 (LOW) — One profile, two spellings, two keys

**Scenario.** `thunderbird.ProfileKey` resolves links and folds case, but not macOS's
`/System/Volumes/Data` firmlink nor a Windows junction (`restorekey.go:30-41`). One profile spelled
two ways gets two restore locks and two settle scopes. Thunderbird's own lock still keeps two
writes apart (keyed by the file's identity since ADV-01), but presync can miss the restore lock and
start Thunderbird mid-restore (Thunderbird is then refused by the lock), the person is told "close
Thunderbird" instead of "another restore is running", and a leftover under one spelling is not
cleaned by a restore under the other.

**Probe** (`TestAttackCON08FirmlinkKey`, `TestAttackPLT03…`): keys differ for `/Users/…` vs
`/System/Volumes/Data/Users/…` (macOS) and for a junction spelling (Windows); a plan through the
firmlink spelling is accepted. **CONFIRMED** on macOS and Windows. `subst` was not run (the VM's
`subst` refused the drive letter).

**Fix.** Key by the profile folder's identity (device and inode; volume and file index), or resolve
with the final-path resolver before hashing.

### R6-16 (LOW) — A parent name taken after the re-check is found only after the whole write

**Scenario.** The writer makes the container `Restored — NAME.sbd` exclusively and moves to the
next "(n)" name only when that container exists (`write.go:164-186`). It does not look at the parent
file's name `Restored — NAME`. If another program makes that name between the re-check and the
container, the restore writes and checks everything, then meets the name at the commit: W-TAKEN, the
tree left "abandoned" where Thunderbird may show it (`write.go:434-456`), exit 1.

**Probe** (`TestAttackWindowAfterRecheck`, "the name taken" after the re-check): outcome `taken` at
every point after the re-check; at the re-check itself it is W-CHANGED, nothing written.
**CONFIRMED** on macOS, Windows and Linux. Needs another program in a millisecond window; Thunderbird
is held out.

**Fix.** Right after the container's create, if an entry of the parent name is there, log `name`
and move to the next "(n)" as the existing loop does. (A name taken later still needs W-TAKEN.)

## Refuted (each with the evidence)

| Claim | Evidence |
|---|---|
| INS-05: an archive folder name with a terminal escape reaches the terminal | `TestAttackINS05ControlCharsToTerminal`: a folder named `In<ESC>[2Abox`, restored with `-yes`: no ESC on stdout or stderr (macOS, Linux). The names printed are the layout's, cleaned of C0, DEL and C1 (§4.4 note). |
| SKP-A1: a backup whose input is the folder above the profile backs it up without W-RECAPTURE | A backup does not read mbox files from a folder above the profile (`DiscoverInputs` finds no source there: "no input mail sources found"), so there is nothing to warn about. |
| SKP-B: Local Folders swapped for another folder after the re-check | Refused W-CHANGED at every instant, before and inside Thunderbird's lock take (`OpenLocalRoot` pins by the open handle's identity), macOS, Windows, Linux. |
| SKP-B: prefs.js pointed at another folder after the re-check | The restore writes into the folder the plan pinned (`done`); Thunderbird would then not show it, but nothing of the person's is touched, and only another program can edit prefs.js with Thunderbird held out. No harm. |
| PLT-08: the admitted Unix `fstatat`→`unlinkat` window reaches outside the container | A same-user program can move the restore's own checked folder and plant a name in it; the removal then removes that planted name inside the restore's own folder. No link is followed and no name of the person's is reachable. The design's "inside the container only" wants "inside the restore's own folders, wherever they are moved" — a wording note. |

## Not reproduced or not run

- **PLT-05** identity on FAT/exFAT (slot reuse), macOS SMB, ReFS: not run (needs an `hdiutil`
  MS-DOS image or a Dev Drive). The harm, if real, stays inside the restore's own folder.
- **PLT-09** Windows with UAC off: admin tokens likely read as elevated, so every restore would be
  refused (fail-closed). Not run.
- **PLT-10** ADV-02's "inside" instant with a *folder* swapped (not the entry): not run. The entry
  swap at that instant ran with a real symbolic link and held.
- **CON-07** an encrypted restore's verb-less lock makes `health` read a stale "running" backup
  record as live. Same for every verb-less holder before R6 (encrypted extract); not probed.
- **CON-09** the start-up cleanup's words lost when the record then fails to load: no realistic
  trigger found.
- **INS-07** (latent, for slice R7): `Result.Write.Err` and the cleanup's name lists keep raw
  errors and names of an encrypted archive's files. The command line prints only the scrubbed
  words; R7's pass should check that Desktop never serialises or logs the raw result.

## What held

- **The lock order and releases.** Archive lock → restore lock → re-check → Thunderbird's lock →
  `OpenLocalRoot` → settle → write, each released by a deferred release (`run.go:119-145`); the
  existing MA-1127 tests drive every end (success, error, Cancel, Shut down, panic, refusal); the
  probes added none that left a lock taken.
- **Local Folders' identity.** Pinned at the plan, re-read in the re-check and again from the
  opened root after Thunderbird's lock; every write goes through that root (P-B, all platforms).
- **ADV-02 on Windows with real symbolic links** (Developer Mode on for the run): the removal-loop
  swap, the call swap and the swap inside the removal after its check all kept the person's file
  and stopped (W-KEPT); junction cases too. **The R3–R5 settle's ADV-01..07 held** under these
  probes; nothing found re-opens them.
- **The lock's verb cap (16 → 24).** Every reader decides "held" by the OS lock, never by the verb
  (`archdelete/execute.go`, `backupguard.go`, `health.go`, `busy.go:54-68`); the quiet skip ignores
  the verb; an older program cuts it to `restore-thunderb` and says "another MailArchive task"; no
  existing verb is 17–24 characters.
- **A backup of the same archive during a restore** is refused by the archive lock, plain and
  encrypted; a scheduled one skips quietly (MA-1127).
- **Who counts as at a terminal, and the flags.** Stdin and stderr a terminal and in the
  foreground; every combination of `-yes`, `-not-encrypted-ok`, `-plan` and the terminal refuses in
  the safe direction; ask-twice without a terminal is refused whatever the flags; the typed-back
  path is exact and fails closed; `-not-encrypted-ok` on a plain archive is refused (INS finder,
  MA-1129).
- **The key lease.** Taken at Start, destroyed on every return of `Run` and of the plan's own lease;
  the command line destroys its unlock; no temporary or scratch file holds plaintext (MA-1128's scans;
  INS finder). Memory and swap are not wiped beyond what the vault does, as §9.7 states.
- **Planted job files.** Only the same user can write them (owner-only, no links, size-capped); the
  worst a plant does is add W-RECAPTURE or move the default profile, which the plan shows.
- **Elevation.** Root refused on macOS and Linux; Windows fails closed when the token cannot be read.
- **R40 "only adds".** No probe made the restore write outside its new folder (beyond Thunderbird's
  lock file, as R40 says) or remove anything the person made.
- **Exit codes.** A stop before writing is 130/143 with nothing added; a stop mid-write is 130/143
  with the cleanup's words; a partial restore is 3; a finished one 0 (MA-1129, P-D).

## Gates

This pass writes docs only; no fix is made here. Each confirmed finding needs its probe turned into
a real test with a `covers:` marker, shown red against this commit and green after the fix, and a
fresh look at the patch. R6-01 (refuse, or ask twice) and R6-13 (restore from a read-only archive)
carry owner questions; R6-02 changes §3.1 step 4's rule and R6-12 §6.2's reach, which want the
lead's call. Slice R7 (Desktop) owes its own pass, with INS-07 and the Lock reply in it.
