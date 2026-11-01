# Design: delete an archive, and remove one from the list

Status: PROPOSED, revision 3 (2026-11-01).
- Revision 1 (3250479) was reviewed pre-code: `docs/review-archive-delete-predesign.md`,
  GO_WITH_CONDITIONS, conditions C1–C20.
- Revision 2 (7bba813) folded them and the owner's rulings [O1], [O2]; its fold
  verification (same report, "Fold verification of revision 2") found 36 items to
  fix. Revision 3 folds every one, using the report's own fix text, each marked
  where it lands (e.g. [C2], [NF-1], [O2-03]).
- Owes: the fold verification of revision 3, then build, the adversarial pass and
  the friction review before release. Target: 1.5.3 (owner, 2026-11-01: "bundle
  into 1.5.3"). Note on proportionality: this is the third pre-code cycle; the build
  is split into slices (§13) so each is reviewable alone.

## 1. Why

The owner asked what the interface offers "to completely delete and purge an
archive". Nothing: no action deletes an archive, and no action takes a row off
Your archives (`archindex.Forget` exists and nothing calls it). A folder deleted by
hand stays on the list forever, greyed.

Owner's rulings, 2026-11-01, quoted:
- "the never delete rule should be adjusted to never delete without your explicit
  decision to do so" (R36 in `docs/scenario-catalog.md` §2).
- "Cleanup is an essential part of any archive process and even necessary from a
  policy and security standpoint (retention rules etc). It is already focused on
  the side of caution to move it to a manual process."
- "windows is the highest potential group of users": Windows behaviour is designed
  and tested first.
- [O1] On mail-shaped files no record names (the review's owner question): "an
  extra, unticked option to delete them too, for retention." (§4.3)
- [O2] "a safety item to consider is an export logs (journal/record/final action) -
  assess?" Assessed by the lead, 2026-11-01: `deletions.log` is a journal for
  recovery, but it is removed by uninstall, is not shareable, and names only a path.
  Added: a saved plan before the decision and a deletion record after it (§8.1).
  The shape is the lead's; the owner may change it.

## 2. What is built

Two row actions on the Your archives card, each with a command-line verb (R33):

| Action | What it changes | What it never changes |
|---|---|---|
| **Remove from this list** (`mailarchive unlist-archive -out DIR`) | the row in `known-archives.json`; `last-archive.txt` when it names the folder; `Out` when the folder is the current one and unreachable or no longer an archive; and, only after saying so in its question, this folder's own schedules (and, for the current archive, its `BackupFor` capture) with their per-name config files (§5.2) [C3, NF-REMOVE-CURRENT-BACKUPFOR] | the folder and everything in it; keys and this computer's record of encrypted archives; every other archive's rows, schedules and files |
| **Delete this archive…** (`mailarchive delete-archive -out DIR`) | the archive's own files (§4), the schedules the plan lists (§5.1), their per-name config files, `Out` / `BackupFor` per §5.3 step 4, the folder's scratch copy, the row, this computer's record and remembered key for an encrypted archive (§6), and lines in `deletions.log` | any file the archive did not write unless the person ticks §4.3's box; any other archive; the shared settings (`Interval`, `WeeklyTime`, alerts, Microsoft sign-in) [C14] |

Delete is permanent: no Trash or Recycle Bin (retention). The caution is in the
plan and the typed confirmation (§3), the refusals (§7) and the honest result (§8).

**What Delete does not reach** (a fixed line in the panel, the result, the record
and the guide) [C14]: a sync service's online recycle bin (OneDrive keeps deleted
files about 30 days on a personal account, up to 93 on a work account) and version
history; Windows File History and Previous Versions; macOS Time Machine; copies,
exports or decrypted copies made elsewhere; sync tools other than those detected
(§9).

The panel carries one line telling the three apart [C19]: "Remove from list forgets
the row (and, after asking, stops its schedules) and keeps the folder. Delete
erases the archive's files. Forget (encrypted archives) only stops this computer
protecting the folder."

## 3. The decision and the requests

### 3.1 Delete: plan, then the typed name

1. **Plan** (`POST /api/archive/delete/plan {path}`) [C20]: POST-only, through
   `d.api` (`GuardLocalPOST`, CSRF token, epoch, body cap). The path must be a row
   in `known-archives.json`: matched by file identity (`util.ResolveExisting` +
   `os.SameFile`) when the folder can be read; when it cannot (unplugged, or gone
   after an unfinished delete) matched only by the row's own recorded spelling,
   exactly, and then only Remove from list, or Delete's remaining steps when
   `deletions.log` has a `started` line with no `finished` line for that row (§5.4),
   are allowed. Read-only, it runs §7's refusals, the schedule discovery (§5.1), the
   file list (§4), the scratch listing (this archive's `scratch/<id>/` with the
   single-archive `ScratchTargets` rule; a held reader lock or an unexpected entry
   refuses here) [C7], and the protected-folder check (§7) [C15]; it counts files
   and bytes and notes a synced folder (§9). It records: the folder's identity
   (volume + file id); the manifest's or vault's size, modification time and
   SHA-256; the discovered entries; §4.3's list of unrecorded mail copies [O1]; and,
   for an encrypted archive, whether this computer recorded it and, if so, the
   record's ID and Spelling, found while the folder exists [NF-2]. The plan is held
   server-side for 10 minutes under a one-use, kind-filtered token
   (`pendingItem.deletePlan`, `isDeletePlan`; `discard` and `ofCurrentArchive`
   cover the new kind).
2. **Delete** (`POST /api/archive/delete {token, typed, unrecorded}`, through `d.api`
   like the plan) [C20]: `typed` must equal the folder's own name (last path
   element), compared after Unicode NFC normalisation on both sides, with no case
   folding [C19]. `unrecorded` is §4.3's box (default false). Under the archive lock
   (§5.3 step 1) the plan is re-checked: the folder opened by `os.Root` must be the
   same folder (identity) as the plan's [C15], the manifest/vault unchanged, the
   entries unchanged, the unrecorded list unchanged [O1], the scratch listing still
   allowed [C7], and none of §7's refusals new. Any difference refuses with "This
   archive changed since you clicked Delete. Click Delete again." and nothing is
   removed (step 0's `started` line is followed by `finished refused`).

The page shows the plan in a panel beside the row (`noteBeside`, MA-955), never a
browser `confirm()`/`prompt()` [C19]:
- the full folder path with its drive, files and size, the schedules that will be
  stopped (and any "not removed: check it yourself"), "This archive is paused."
  when it is, the synced-folder warning when it applies, the "kept: not the
  archive's" count, §4.3's box when there are any, the encrypted-key line (§6),
  "What Delete does not reach", **Save this plan** (§8.1), and "A line is added to
  this computer's deletion log (deletions.log), which keeps the date, the folder and
  what was removed.";
- "Type the folder's name, NAME, to delete it" with a text box (paste allowed), a
  live "doesn't match yet" until it matches, **Delete permanently** (danger style,
  disabled until it matches) and **Cancel**;
- opening a delete panel closes any other one.
- **After a Delete** [C19]: the result moves to the top of the Your archives card
  (not the row, which is gone); the row's buttons are removed at once without a
  reload; the page does not reload (timed reloads are held) until the person
  dismisses the result. The deletion record (§8.1) is held server-side under the
  result's one-use token until dismissal or Shut down, so **Save deletion record**
  works until then.

### 3.2 Remove from this list

One request (`POST /api/archive/unlist {path}`, through `d.api`, same row rule
including the unreachable-folder rule) after an in-row question: "Remove from list?
The folder and its files are kept." — or, when §5.2 finds schedules, "Its scheduled
backup NAME will also be stopped. Remove from list? The folder and its files are
kept." Yes / No.

### 3.3 The command line [C17]

`mailarchive delete-archive -out DIR` and `mailarchive unlist-archive -out DIR`:
- at a terminal only: no `-yes` flag, no stdin pipe; no job file, schedule or
  unattended run can reach them. `keyVerbTerminal` gets its own case for these
  verbs: "Run this in a terminal window: you type the folder's name there to
  confirm.";
- refused while a MailArchive Desktop instance is running in any logon session of
  this account (`instance.LockIsHeld` on every desktop lock in the config folder):
  "Use the Desktop, or shut it down first". Checked at start and again right before
  steps 4, 8 and 9;
- delete prints the plan, asks §4.3's question (default no) when it applies, then
  asks for the folder's name;
- `-plan-out FILE` (with no deletion: a plan-only run) and `-record-out FILE`: §8.1;
- exit codes: 0 deleted (or removed from the list, or a plan saved), 3 not fully
  deleted, 1 refused (including a name that does not match), 130/143 stopped by a
  signal. They are added to `docs/ux-contract.md` X1 beside decrypt's.

## 4. What Delete removes inside the folder

A plain archive can share a folder with the person's own files (no empty-folder
check on the plain create path, `app/run.go:596`, `wizard/wizard.go:563`) and
mirrors the source's folder names, so the folder is never removed wholesale.
Removal is by name, through an `os.Root` opened on the folder; every element of
each name Delete removes or walks is checked with `Lstat`. **Links** [C11,
NEW-reparse-onedrive]: a symbolic link, junction or mount point (a name-surrogate
reparse point) on the path of any name Delete removes or walks, or as the archive
folder itself, refuses the whole delete before anything is removed; other reparse
points (OneDrive cloud placeholders, deduplication) are ordinary files; links among
files Delete does not touch are ignored. Folders are removed with `os.Remove`
(never `RemoveAll`), deepest first.

**Whether a folder is an archive** [NEW-isarchive-cap] is decided everywhere in this
design (§3, §5.2, §7, §10) by §4.1's manifest reader (no 1 MiB cap) or the encrypted
marker, never by `archindex.IsArchive` or the row's Available flag.

### 4.1 Plain archive

The manifest is read through `os.Root` + `util.OpenRegularFileIn` +
`state.LoadFrom`, with a cap sized for real archives (not `archindex.inspect`'s
1 MiB) [C10]. Removed:
- **Records** [C11]: a record's `Path` (and each `AlsoFiles` entry) counts only if
  it passes `util.ValidRecordedPath`, its first segment is a token in the
  manifest's `Stores`, and it ends in `.html`; then exactly its stem's `.html`,
  `-attachments.zip` and `.eml` are removed (as `reindex` derives them), never a
  bare `Path`. The whole delete is refused if a store tree holds another archive's
  marker (`.mailarchive-manifest.json`, an encrypted marker).
- **Index pages**: `index.html` / `index-N.html` only in folders a removable record
  passes through, and the root `index.html`; never by walking a tree.
- **Run leftovers at any depth in the store trees and the root** [C9]:
  `.mailarchive-*.tmp`, `.mailarchive-snapshot-*` (the `SweepTemps` rule, no age
  cut-off). An `-attachments.zip` is removed only as a recorded stem's sibling
  (Records above); any other `-attachments.zip` is a §4.3 file (listed, kept unless
  ticked). The `SweepOrphanZips` rule is not used [NEW-orphan-zip].
- **Fixed root names**: `search.db` (+ `-wal`, `-shm`, `-journal`),
  `search.db.rebuild` (+ `-wal`, `-shm`), `README.txt`, `attachments-report.tsv`,
  `attachments-report-legacy.tsv`, `BACKUP-NEEDS-ATTENTION.txt`,
  `ARCHIVE-INTEGRITY-ATTENTION.txt`, `_COPY-PROBLEMS.txt`, `_COPY-NOT-FINISHED.txt`,
  `mailarchive.log`, `mailarchive.stderr.log` [C9], each discovered name's
  `<name>.log`, `<name>.stderr.log` and verify log [C6, C9], `_outlook-pst/` (its
  `.pst` files only), and every `.mailarchive-*` state file and descriptor except
  `.mailarchive.lock`, which step 7 removes after unlocking [C1].
- **The history file** `.mailarchive-history.jsonl` is removed as a state file; it
  names no files, so it is not a source of names [C9].
- **The manifest last** (§5.3's order), so a delete stopped part way is still
  recognised as an archive and can be finished.

### 4.2 Encrypted archive

The fixed root names (`encmark.reserved`, `tempRe`) except `.mailarchive.lock`,
which step 7 removes after unlocking [C1], with `units/`, `chunks/`, `inventory/`
only in their validated layouts (`vault/marker.go`), `program/` only in the
break-glass copy layout; **the vault files last**. The archive's scratch folder by
§5.3 step 5 [C7].

### 4.3 Mail copies the archive did not record [O1, C9]

The plan lists any `.html`, `.eml` or `-attachments.zip` in a store tree (a folder
under a token in `Stores`) that no record names as a stem's sibling: copies from
older formats, a crashed run, or files the person put there — they cannot be told
apart. The panel shows their count and the first 10 names under "Mail copies this
archive did not record", with an **unticked** box: "Also delete these N files (for
retention)". The command line asks the same, default no.
- Unticked (default): they are kept and listed in the result as "kept: not the
  archive's" (one label, §8 (c)). The result never says only "Deleted" while they
  remain ("Deleted. N mail copies the archive did not record were kept.").
- Ticked: only the files listed in the plan are removed, under every rule of §4
  (same `os.Root`, same link rule, only those three file kinds, only inside store
  trees, never any other file). One that cannot be removed is reported as "Not
  fully deleted" with §8 (a)'s remedy (the manifest is not kept for it); on Windows
  a ticked read-only file has its read-only flag cleared under §9's rules. The
  box's choice is recorded in `deletions.log` and the record.

### 4.4 Litter and the folder itself [C5]

An operating-system file (`util.IsLitter`: `.DS_Store`, `desktop.ini`, `Thumbs.db`)
is removed from every folder a removable record or run leftover passes through,
whether or not that folder is then removed. A litter *folder* is never cleared and
keeps its parent. The archive folder itself is removed last (§5.3 step 7), only
when empty but for litter files.

## 5. Discovery and the order of each action

### 5.1 Discovery (read-only, in the plan) [C6]

Every scheduler entry for the folder:
- `DefaultNameFor(path)` and its `-verify`;
- `RecordedBackupName` / `RecordedVerifyName` from the folder's descriptors, read
  before anything is removed;
- every job file in `job.Dir()` whose `Out` is the same folder under
  `SameArchivePath` — every match, not `corroboratedRunJob`'s first;
- every `OurInstalled` entry whose `-out` is the same folder. New code reads `-out`
  on Windows from the entry's XML side-file or wrapper and on Linux from the cron
  block (`OurInstalled` fills `Out` only for cron, `sweep.go:102`); macOS cannot
  read it back (UJ-6), so every macOS custom-named entry no descriptor or job file
  binds is listed as "not removed: check it yourself".

Each scheduler call has a time limit. The plan lists them all. Refused before
anything changes: a descriptor that cannot be read (ESC-10), or one recording a
same-computer name `bound.go` refuses, with the wording of §7's descriptor row. A
descriptor recording another computer (`bound.go`'s other-host case) is not a
refusal: it is read with `bindDescriptorName` (no host check), listed in the plan as
"Computer B also backs up into this folder; remove its schedule there", and deleted
with the folder [NF-OTHER-HOST-DESCRIPTOR].

**BackupFor** [C4]: when `captureBackupFor()` (`desktop/schedule.go:85-91`; a
`BackupFor` that names a run-job counts as unset) names folder X, the capture
registered under X's name is the current archive Y's backup.
- (a) Delete of X (X not current) is refused, nothing changed: "The scheduled
  backup of your current archive Y is still registered under this folder's name.
  In Settings, remove the scheduled backup and set it up again (it is then
  registered under Y), then delete this one."
- (b) Remove from list of X is refused with the same reason, and the remedy ends
  "…then remove this one from the list." [C3]
- (c) Delete of the current archive Y removes both entries `removeTasks` removes
  (the one named for `Out` and the one named for `BackupFor`) and clears
  `BackupFor`. Removing both entries is what stops a capture falling back to its
  own `-out` (`capture.go:57`, `launch.go:199`).

### 5.2 Remove from this list [C3]

Discovery as §5.1 but from this computer's records only (job files under
`SameArchivePath`, `DefaultNameFor` and `-verify` entries, recorded names when the
folder is reachable), so it works with the drive unplugged. When the folder is the
current archive, discovery also includes `captureBackupFor()`'s entry, as §5.1 (c)
does for Delete [NF-REMOVE-CURRENT-BACKUPFOR]. If any are found the question names
them; Yes stops each with `schedule.RemoveLiveEntry` (never touches the folder),
removes that name's job file and per-name config files (§5.3 step 3's list), and
clears `BackupFor` when its entry was removed. Then `last-archive.txt` is cleared
when it names the folder, `Out` is cleared when the folder is current (allowed only
when it is unreachable or no longer an archive), and `archindex.Forget` removes the
row; nothing re-adds it (`archivePointers` reads `Out`, `last-archive.txt`,
`BackupFor`, each now clear of it).

### 5.3 Delete, in order [C2, C1, C18]

A named file already gone counts as removed. Every step has a success rule and a
result line:

| Step | Success | On failure |
|---|---|---|
| 0. Log `started`: append and fsync a `started` line to `deletions.log` (date, folder, plan counts, §4.3's choice) | written | stop: "Could not write MailArchive's deletion log, so nothing was deleted." |
| 1. Take the archive lock (`lockfile`, the lock a backup takes) and re-check the plan (§3.1). Encrypted: lock this archive in the page whether or not it is open (`d.cfg.Keys.lock(out)`, as Forget does at `encryption_forget.go:119`, so no remembered key re-opens it), and wait for this page's reader of it to close and its scratch copy to go (a waiting close, as `lockAll` uses). `Keys.hush` is not used: it locks every archive [C13]. | lock held, plan unchanged | stop: §7's or §3.1's wording; nothing changed |
| 2. Schedules: each discovered entry is first queried (`schtasksState` from the exit status, the launchd plist, the cron block); one the scheduler reports not installed counts as removed and is not passed to `RemoveLiveEntry` (a paused archive's entries are expected to be absent) [NEW-FINDING-1]. The rest are removed with `RemoveLiveEntry`; for the current archive, §5.1 (c)'s two. | every entry removed or absent | stop: "Could not stop the scheduled backup NAME: SCHEDULER ERROR. Nothing else was changed." |
| 3. Per-name config files, for each discovered name: `<name>.json` (`schedule.RemoveJobFile`), `<name>.jobfail.log`, `<name>.older.jobfail.log`; `<key>.alert-streak` and `<key>.sync-alert` for `backupalert.KeyFor` of each discovered entry's `-out` spelling [C6]; `last-failure.json` only when its `Name` is one of them | every listed file gone or absent | stop: "Could not remove FILE in MailArchive's settings folder." |
| 4. Settings and pointers: `Out` cleared if this is the current archive (the page then shows "No archive chosen yet", or the folder given with `-out` at launch when there is one [C16]); `BackupFor` cleared if it names the folder, and always when this is the current archive [C4]; `last-archive.txt` cleared if it names the folder | the settings write succeeds | stop: "Could not save MailArchive's settings, so this archive is still chosen. Click Delete again." |
| 5. Scratch (encrypted) [C7]: re-list `scratch/<id>/` under the lock with the single-archive `ScratchTargets` rule (only that id, never the root or another id) and remove it | removed or absent | stop, `finished partial`: "MailArchive's working folder for this archive is in use or holds files it did not put there: FOLDER. Steps done: schedules stopped, settings updated. Close MailArchive serve, search or other windows, then click Delete again." (never "nothing changed") |
| 6. The folder's contents (§4), the manifest or vault last, `.mailarchive.lock` kept (it is held) | no recorded name remains (§8 (a)) | continue to 7's release, then stop with §8 (a)'s result |
| 7. Release the lock (the file stays, `lockfile.go:111-114`); remove `.mailarchive.lock` by its exact name, retrying briefly (Windows cannot delete a held file); remove the folder if empty but for litter; append a durable `folder removed` or `folder kept` line to `deletions.log` [C2] | the lock released | — (the lock file or an empty folder left is reported, not a failure) |
| 8. Keyring (encrypted) [C8, NF-1, NF-2]: only when the folder is gone, or `encmark.At` classifies it Gone (only an unheld `.mailarchive.lock` and litter remain). A fresh `PlanForget` on the Spelling the plan recorded (not the row's path), case Gone, no lock taken. `Whole` and `Keys` only when `plan.Last`; otherwise forget only this path, keep the key, and the result names the other recorded copies. "Never recorded" means the plan found no record, not a step-8 lookup failure: then no record is touched. | forgotten, or not applicable | skipped: "Files deleted; this computer still remembers its key: use Forget this encrypted archive here." |
| 9. The row: `archindex.Forget` — only when step 8 is done or does not apply (so a skipped step 8 keeps the row and its Forget button) [C18] | removed | "Could not remove the row from Your archives." |
| 10. The deletion record (§8.1) is built first; then log `finished complete` / `finished partial` / `finished refused` with what was left and the record's digest | written | the result says "This record's digest could not be added to deletions.log." |

Releasing the archive lock (step 7's first part) and step 10 always run, in a
deferred cleanup, also after a refusal or a recovered panic. The delete handler
recovers its own panics and reports the steps done, never "nothing was changed"
after step 0 (`d.api`'s recover, `workspace.go:131-132`, would say so) [C18].

**After step 7** the archive is unlocked. A backup that passed its checks before
the delete and takes the lock after it is refused by the emptied-folder guard,
which is checked again under the run's own lock (§7) [NF-3, C12]: a kept folder
holding the person's files is refused by the guard; a folder that is gone is
refused by MA-689 (no `MkdirAll` for unattended runs). A kept folder that is empty
or holds only litter is not protected: the result then says "The empty folder could
not be removed: delete it by hand", and the plan's "not removed: check it yourself"
entries are repeated.

### 5.4 A delete not finished [C2]

A `started` line with no `finished` line for a folder (a crash, a power cut) shows
its row as "Delete not finished: click Delete again" in place of Switch, Run now
and Archive now. A re-run:
- treats the folder as gone only when the log has its `folder removed` line and
  the folder's parent is reachable; then it skips steps 1 and 5–7, says so, and
  runs the rest (config side, keyring, row) and step 10;
- when the folder is not reachable and there is no `folder removed` line, refuses:
  "Reconnect the drive or folder first" (a drive unplugged after a crash in step 6
  must not make the keyring step forget a vault still on that drive);
- while the manifest/vault exists, is an ordinary Delete (`Out` was cleared in step
  4, so it is no longer current);
- when the folder is present but holds no manifest or vault and the log has no
  `folder removed` line, removes what §4 can still name (fixed root names, run
  leftovers) and writes `finished partial: the folder still holds N files the
  delete can no longer identify`, never `complete`.

`deletions.log` is in the config folder, owner-only (0600, `util.WriteSecure`
rules), append-only, one line per event; uninstall removes it (added to
`ConfigTargets`, `uninstall/config.go:56`). Growth: three or four short lines per
delete.

## 6. Encrypted archives

No password is asked: a person who has lost it must still be able to delete
(retention), and the typed name is the decision. The plan says: "This computer
will forget this archive's key. Any other copy of this archive will need its
password or recovery key to open." [C8]

## 7. Refusals [C15]

Each names the cause and a remedy that can be done from the row; nothing changes.

| Case | Delete | Remove from list |
|---|---|---|
| a backup, Run now, verify, pre-sync session, Desktop capture (`d.cap.running`) or workspace job on this archive is running [C12] | "A backup of this archive is running. Try again when it ends." | allowed |
| MailArchive serve, search or another MailArchive window has this encrypted archive open (reader lock) [C7] | "Close MailArchive serve, search or other MailArchive windows for this archive first." | allowed |
| `scratch/<id>/` holds something MailArchive did not put there [C7] | "MailArchive's working folder for this archive holds files it did not put there: FOLDER. Remove them, then click Delete again." | allowed |
| the folder is not reachable | "Reconnect the drive or folder first: nothing can be checked." — unless §5.4's `folder removed` line exists; then the re-run continues [C2] | allowed; when it is the current archive, `Out` is cleared [C3] |
| the folder is no longer an archive (by §4's test), no unfinished delete | "This folder no longer holds an archive. Use Remove from list." | allowed; `Out` cleared when current [C3] |
| paused (read-only) | allowed: the seal goes with the folder; the plan says "This archive is paused." [C13] | allowed |
| current archive | allowed (§5.3 step 4) | refused while reachable and an archive: "This is the archive in use. Switch to another archive, or add one (Add another archive), then remove this one from the list." [C3] |
| `captureBackupFor()` names it, it is not current | refused, §5.1 (a) [C4] | refused, §5.1 (b) [C4] |
| a descriptor unreadable, or naming an entry `bound.go` refuses (same computer) | "The schedule NAME could not be read. Remove it with: mailarchive schedule -remove -name NAME, then click Delete again." [C15] | allowed (only §5.2's own discovery is acted on) |
| the Desktop was started with `-out` naming this folder [C16] | "Restart MailArchive without -out, then delete." | "Restart MailArchive without -out, then remove it from the list." [C3] |
| protected: decided by file identity (`os.SameFile` up the ancestor chains), not path text — the folder is, contains or is inside the config folder, the scratch folder, the encrypted record folder, or another known archive; or it is or contains the home folder, a drive root, or a system folder (Windows: `%SystemRoot%`, `%ProgramFiles%`, `%ProgramFiles(x86)%`, `%ProgramData%`; macOS: `/System`, `/Library`, `/Applications`, `/usr`; Linux: `/usr`, `/etc`, `/var`, `/bin`, `/opt`) | "MailArchive will not delete this folder: it is (or is inside, or holds) NAME. Delete the archive's files by hand." [C15] | allowed |
| a link, junction or mount point on the path of a name Delete removes or walks, or as the folder itself [C11] | "This archive's folder is reached through a link. Delete it by hand." | allowed |
| mixed state (`encmark.classify` Other; another archive's marker in a store tree) | "This folder holds files of more than one kind. Delete it by hand." | allowed |

**The emptied-folder guard [C12, NF-3]** (a change to the shipped engine, its own MA
row, in the adversarial pass's scope): an unattended run (scheduled backup, capture,
run-job) refuses an existing `-out` that holds no manifest, no encrypted marker and
no backup descriptor, and holds anything beyond litter. The guard and
`RequireExistingOut` are checked again under the run's own lock (after
`lockfile.AcquireAs`, ignoring `.mailarchive.lock` itself), and an unattended run
never creates `-out` (no `MkdirAll`): a run that checked before a delete and got the
lock after it is refused. A first run after an install, and "Save and keep" into a
new empty folder, still work (empty, or holding the descriptor).

## 8. The result says exactly what happened [C5, C18]

Three kinds of leftover:
- (a) A name the archive recorded or owns (or a §4.3 file the person ticked) that
  could not be removed. It keeps the manifest/vault for a recorded name (so Delete
  can be run again) and the result is "Not fully deleted. N files could not be
  removed:" with up to 10 names and a remedy by error type: a sharing violation,
  "Close Outlook or any program using them, then click Delete again"; access
  denied, "Windows security or antivirus blocked it: allow MailArchive or delete
  these by hand"; a device gone, "Reconnect the drive, then click Delete again". A
  briefly busy empty folder is retried before it is reported.
- (b) Litter: removed (§4.4).
- (c) Anything else (the person's files, §4.3's files when unticked): kept, listed as
  "kept: not the archive's", never blocking completion.

Complete means no name the archive recorded remains. Result lines: "Deleted. N
files (SIZE) removed. The folder is gone." / "…The folder is kept: it holds N
files that are not the archive's." plus one line per step that changed something
outside the folder ("Stopped the scheduled backup NAME", "This computer forgot the
archive's key", "Removed from Your archives"), §2's "What Delete does not reach",
"Recorded in deletions.log", and "Save this record now: MailArchive does not keep
a copy." [O2-07]

### 8.1 The plan and the deletion record, saved by the person [O2]

- **Before the decision**: the panel's **Save this plan** and `delete-archive
  -plan-out FILE` (a plan-only run, deleting nothing) save the plan as plain text,
  for a person who must get approval first: the record below with "Status: planned,
  nothing deleted". The plan's text is sent in the plan response (no new route);
  saving it does not use or extend the token [O2-08].
- **After it**: the result's **Save deletion record** and `-record-out FILE` save
  the record. The Desktop builds the text server-side once and sends exactly those
  bytes in the delete response; the page saves them through the browser (a Blob
  download named "MailArchive deletion record - NAME - DATE.txt", as the
  fingerprint receipt is saved, `pages_scripts.go:615-618`); nothing is sent
  anywhere else. Saving is offered, never required. Dismissing a result whose
  record was not saved asks once: "Close without saving the deletion record?"
  [O2-07]
- **Files on the command line** [O2-02]: `-plan-out` and `-record-out` are checked
  before the plan is shown and before step 0, with `keyring.CheckNewFileOut`'s
  rules plus: not inside the folder being deleted or any known archive. Each file is
  created new (`O_EXCL`, never over an existing name or link) only when its text is
  final; it is not owner-only (it is meant to be shared). If writing the record
  fails after the deletion, the full record is printed to the terminal, the result
  says "The deletion record could not be saved to FILE: it is shown above", and the
  exit code stays the deletion's. A plan-only run follows §3.3's terminal and
  Desktop-running rules and exits 0, or 1 when refused.
- **Contents** (UTF-8, one "Key: value" per line; `Status` first and only once;
  every value through `util.StripControl` with newlines replaced; a key with several
  values repeats the key on one line per value [O2-06]):
  - Status: planned / complete / not fully deleted / refused after start;
  - the date and time with its time zone; the computer name and the account;
    MailArchive's version;
  - the folder as shown and its identity (volume + file id); plain or encrypted
    (and the archive id for an encrypted one);
  - the message count and the date range of the mail; the first recorded run (the
    first run header in `.mailarchive-history.jsonl`, `state/history.go:9`) and the
    last backup (the run history's newest entry), each labelled "as recorded in the
    archive", or "first run: not known" when the history is missing or unreadable
    [O2-04]. For an encrypted archive whose key is not open here, the message count,
    the date range and the first run read "not known: encrypted, key not open";
    never left out, never estimated [O2-05];
  - files and bytes removed; the SHA-256 of the manifest (or vault file) read at the
    plan; the schedules stopped and those "not removed: check it yourself"; whether
    this computer's record and key were forgotten; §4.3's choice and count;
  - each leftover kind with its count; a leftover is named only by its store token
    and the date-and-hash parts of its stem (for example
    `Store1/…/2024-03-01_0900_…_ab12cd34.html`), never a folder name below the token
    or a subject slug — the full names are shown only on screen [O2-01];
  - "What Delete does not reach";
  - a record made after a saved plan whose manifest digest differs says "the archive
    changed since a plan saved earlier", so an approver can match the two [O2-08];
  - last line `Record-SHA256: <hex>`: the SHA-256 of every byte before that line.
    The text is built once, before step 10, and never rebuilt; step 10 writes the
    same hex to the `finished` line [O2-03].
- **Never in it**: subjects, senders, recipients, addresses, folder names below a
  store token, or any mail content.
- The record is not signed: it is evidence of what this program did, not proof
  against a person who edits it; the digest in `deletions.log` is the cross-check
  while that file exists. Uninstall's plan names `deletions.log` among what it
  removes and says "Copy deletions.log first if you need it to check saved deletion
  records." [O2-07]

## 9. Windows first, then macOS and Linux

- **Read-only files** [C11, O1]: Windows refuses to delete a read-only file. The
  read-only attribute is cleared only on Windows, only through a handle opened
  without following links, and only when the file has a single link; a file the
  archive did not record is never touched unless §4.3's box is ticked and it is in
  the plan's list; macOS and Linux never get a chmod.
- **Open files**: Outlook, antivirus or the search indexer may hold a file; §8 (a).
  `search.db` is closed first (this page's reader for the archive).
- **Long paths**: over 260 characters, handled by Go's `os` on Windows; tested with
  a deep tree in the lab.
- **Synced folders** [C14]: a folder under `%OneDrive%`, `%OneDriveCommercial%`,
  `%OneDriveConsumer%`, a synced SharePoint library (detected through the OneDrive
  registry `SyncEngines\Providers` mount points, or named as not detected), macOS
  `~/Library/Mobile Documents` (iCloud Drive) or `~/Library/CloudStorage/<name>`:
  "This folder is synced with PROVIDER (OneDrive, SharePoint, iCloud Drive, or the
  CloudStorage folder's name). Deleting it here also deletes it there, and PROVIDER
  may keep it in its own recycle bin: empty that too." Claims about cloud-only
  placeholders, timing and paused sync are **conditional** until §11's synced-folder
  lab row proves them (pending: lab).
- **Names**: the typed name is compared after NFC normalisation, exactly as shown,
  no case folding [C19].

## 10. Surfaces and words

- Row buttons (every row, beside Switch / Pause): **Remove from list** (secondary)
  and **Delete…** (danger style). Delete's enabled state comes from §4's archive
  test and §7, not from the row's greyed state [NEW-isarchive-cap]; when disabled,
  its title says why (§7's words).
- The guide (How to use MailArchive) gains "Deleting an archive" and "Removing an
  archive from the list", Windows first, with "What Delete does not reach".
- `status` and the Overview never mention deleted archives.

## 11. Tests (each prove-fail → prove-pass; Windows run first in the lab)

New MA rows from MA-956. Real files in temp folders (no mocks of the removal); each
refusal with `assure.Refused` (cause, remedy, side effect absent); each positive
twin first.
- Plain beside the person's own files: every archive file gone, every other file
  byte-identical, folder kept; a person's file in a same-named store folder is kept
  and the result is "Deleted… The folder is kept" (never "Not fully deleted");
  `.DS_Store` / `desktop.ini` in a store subfolder, ends "Deleted" [C5].
- Q1 fixtures: an archive written at manifest versions 1, 2, 3, 4, 5 and 6, and
  one left by a crashed run (temps, snapshot, orphan zip, `search.db.rebuild`):
  every file the archive wrote is removed or listed [C9].
- §4.3: unrecorded copies (including a person's own `x-attachments.zip`) kept by
  default and listed; ticked, exactly the listed ones removed; a file added after
  the plan refuses; never any other kind of file; the choice in `deletions.log` and
  the record [O1, NEW-orphan-zip].
- Encrypted: folder gone, record and key forgotten (`plan.Last`); another recorded
  copy keeps the key [C8]; a folder kept only for an unheld lock file or litter
  still forgets the record [NF-1]; the record found by the plan's Spelling after the
  folder is gone [NF-2]; scratch only `scratch/<id>/` [C7]; a remembered key does
  not re-open the archive mid-delete [C13].
- Order: the manifest/vault removed last (a fault before it leaves a recognised
  archive, re-run finishes); schedules first (a recording fake scheduler);
  `started` before the first removal; each step's stop wording (§5.3's table); the
  lock released and `finished` written after a panic [C18].
- §5.4: a crash after `folder removed` → re-run with the folder gone finishes; a
  crash in step 6 then the drive unplugged → re-run refused, keyring untouched; a
  folder with no manifest and mail files left → `finished partial`, never complete
  [C2].
- The lock: deleted under the lock, then the lock file and folder gone (Windows
  lab) [C1].
- Discovery: a custom `-name`, two `-name` installs (Windows, Linux; macOS: the
  earlier one is listed as not removed), another spelling, a legacy verify, a
  denied or hung scheduler; an unreadable descriptor refuses; another computer's
  descriptor is listed, not refused [C6, NF-OTHER-HOST-DESCRIPTOR]; delete of a
  paused archive on a scheduler whose "missing" message is not English completes
  [NEW-FINDING-1].
- BackupFor (a), (b), (c) [C4]; Remove from list of a current, unreachable archive
  with BackupFor set: the BackupFor-named capture is named and removed, BackupFor
  cleared [NF-REMOVE-CURRENT-BACKUPFOR]; `-out` launch [C16].
- Manifest over 1 MiB: Delete is enabled and Remove of the current archive is
  refused for a reachable archive whose manifest is over 1 MiB [C10,
  NEW-isarchive-cap] (and a separate MA row: today's list greys such archives,
  `index.go:50, 283`).
- Shape rule and links: a planted record naming a person's file; a planted Stores
  token naming a person's folder removes only `<stem>.html` / `-attachments.zip` /
  `.eml` for records under it and nothing else in that folder; an in-root link
  mid-path; a Windows junction (lab); a OneDrive Files On-Demand folder (cloud-only
  and locally available placeholders) deletes normally (lab) [C11,
  NEW-reparse-onedrive].
- Protected folders by identity, including another spelling of the config folder
  [C15].
- Every §7 refusal, nothing changed; tokens one-use, expiring, kind-filtered; a
  change after the plan refuses.
- Routes: plan, delete and unlist — no token, wrong token, foreign Origin or Host,
  GET → 405, side effect absent; an unreachable row matched by its spelling only
  [C20].
- Remove from list: the row gone and not re-added; folder byte-identical; schedules
  named and stopped only after Yes; unplugged drive; current + unreachable clears
  Out [C3].
- Emptied-folder guard: an unattended run refuses a non-empty folder with no
  archive; a first run into an empty folder and into one holding only the
  descriptor still works; a run passes the pre-lock check, a delete completes, the
  run takes the lock → refused, folder not recreated [C12, NF-3].
- Windows: read-only archive file deleted; read-only person's file kept (and
  removed when ticked and listed); a file held open reported, the rest deleted,
  re-run finishes; a path over 260 characters; a hard-linked file not chmod-ed
  [C11].
- Synced folder (Windows lab, pending): OneDrive and a synced SharePoint library
  detected; the cloud-only placeholder, timing and paused-sync behaviour observed
  before the conditional wording is lifted [C14].
- CLI: refuses without a terminal and while a Desktop runs (any logon session, and
  one started mid-delete before steps 4, 8, 9); exit codes 0/3/1 and X1 updated
  [C17]; `-plan-out`/`-record-out`: an existing file, a link, a path inside the
  folder, and a path in an unwritable folder each refuse before anything is
  removed; a failed record write after the delete prints the record [O2-02];
  uninstall removes `deletions.log` and its plan names it.
- The page: panel beside the row, button disabled until the NFC name matches, no
  `confirm(`/`prompt(` in either handler, one panel at a time; the result and Save
  deletion record survive the dashboard's timed reloads until dismissed; the
  unsaved-record question [C19, O2-07].
- Plan and deletion record [O2]: the saved plan deletes nothing; the record's fields
  for a complete, a partial, a refused-after-start and a plan; the planted archive
  has a leftover of each kind and an unrecorded copy whose file and folder names
  contain the planted subject — neither the saved plan nor the record contains it
  [O2-01]; a planted leftover whose name contains a newline followed by "Status:
  complete" leaves exactly one Status line, saying "not fully deleted" [O2-06]; a
  record for a locked encrypted archive shows the three sealed values as not known
  and no other value [O2-05]; a saved record re-hashed without its last line equals
  the `finished` line's digest [O2-03]; a record after a changed archive says so
  [O2-08].

## 12. Questions answered by the review

Q1 → §4.1, §4.3 [C9, O1]; Q2 → no password (§6); Q3 → the delete locks the
archive itself with `Keys.lock(out)` and a waiting close, not `hush` [C13]; Q4 →
§5.1 (c), §7 [C4, C16].

## 13. Build slices (each with its tests, prove-fail → prove-pass, pushed alone)

1. **Core** (`internal/archdelete`, new): the file list (§4), discovery (§5.1),
   the refusals (§7), the ordered steps (§5.3) behind small seams for the
   scheduler, settings and keyring; `deletions.log`; the record text (§8.1).
2. **Emptied-folder guard** (engine, §7): the re-check under the run's lock and no
   `MkdirAll` for unattended runs.
3. **Desktop**: the three routes, the row buttons, the panel, the result, Save this
   plan / Save deletion record, "Delete not finished" rows.
4. **Command line**: `delete-archive`, `unlist-archive`, `-plan-out`, `-record-out`,
   X1.
5. **Uninstall and the guide**: `deletions.log` in `ConfigTargets`, the uninstall
   plan's line, the guide's two sections.
