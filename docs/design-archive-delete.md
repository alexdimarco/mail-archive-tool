# Design: delete an archive, and remove one from the list

Status: ACCEPTED FOR BUILD, revision 5a (2026-11-01). Revision 5's fold
verification: 12 of 14 folded, 3 LOW items (wording/test) folded here as
[R5:<item>]; outcome "build may start (slices per §13; each slice owes its
adversarial pass)". Revision 5 (eb05d23): Revision 4 (e01fe01) was verified
("Fold verification of revision 4", new findings scoped to revision-4 text): 14
items, each folded here with its fix text, marked [R4:<item>]. Deeper issues in
unchanged text go to each build slice's adversarial pass (§13).
- Revision 1 (3250479) was reviewed pre-code: `docs/review-archive-delete-predesign.md`,
  GO_WITH_CONDITIONS, conditions C1–C20.
- Revision 2 (7bba813) folded them and the owner's rulings [O1], [O2]; its fold
  verification (same report, "Fold verification of revision 2") found 36 items to
  fix. Revision 3 (ec7e11c) folded every one, using the report's own fix text, each
  marked where it lands (e.g. [C2], [NF-1], [O2-03]).
- Revision 3's fold verification ("Fold verification of revision 3") found 29;
  revision 4 folds each with that report's fix text and its "Conflicts … settled
  here" paragraph, marked [R3:<item>].
- Owes: the fold verification of revision 4, then build, the adversarial pass and
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
   exactly, and then only Remove from list, or Delete's remaining steps when that
   row has an unfinished delete (§5.4) [R4:NF-R4-CLI-STOP-DEADEND],
   are allowed. Read-only, it runs §7's refusals, the schedule discovery (§5.1), the
   file list (§4), the scratch listing [C7, R3:NF-C7-SCRATCH-NAME] — the archive's
   scratch folders are `app.archiveScratch(out, id)` (`app/scratch.go:33-39`, a hash
   of `PathSourceID(out)` and the id) for the row's spelling and for every spelling
   discovery found (each job file's `Out`, each entry's `-out`, the descriptor's
   recorded folder), each listed with the single-archive `ScratchTargets` rule; a
   held reader lock or an unexpected entry refuses here, except a reader lock held
   by this Desktop process (the pid in `reader-<pid>-<n>.db` is this process's and
   this page has the archive open), which step 1 closes [R3:NF-C7-OWN-READER] — and
   the protected-folder check (§7) [C15]; it counts files and bytes and notes a
   synced folder (§9). It records: the folder's identity (volume + file id); the
   manifest's or vault's size, modification time and SHA-256; the discovered
   entries and spellings; §4.3's list of unrecorded mail copies [O1]; for an
   encrypted archive, whether this computer recorded it and, if so, the record's ID
   and Spelling, found while the folder exists [NF-2]; and the message count, the
   mail date range, the first recorded run, the last backup (§8.1), and whether an
   encrypted archive's key was open [R3:NEW-FINDING-record-capture-time]. The plan is held
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
- **After a Delete** [C19]: the page does not reload (timed reloads are held) until
  the person dismisses the result. Each delete's result and the deletion record's
  exact bytes (§8.1) are held server-side, separately, keyed by a server-made id
  (not a token) for that delete, until the person dismisses that result or shuts
  MailArchive down; they arrive in the delete response, and every dashboard load
  shows each held result where "Where the result sits" places it, each with its own
  **Save deletion record** (a Blob download of those bytes, any number of times)
  and **Dismiss** (one POST through `d.api`, `/api/archive/delete/dismiss {id}`, in
  §11's route test); no one-use token is used [R3:NEW-FINDING-record-held-vs-sent,
  R4:NF-R4-HELD-RESULTS-MANY]. A new Delete… on a row whose held record was not
  saved first asks "Close without saving the deletion record?" Every
  reload the page starts itself is held while a result is held
  [R3:NF-R3-RESULT-RELOAD].
- **Where the result sits** [R3:NF-R3-ROW-KEPT]: when step 9 removed the row, the
  result moves to the top of the Your archives card and the row's buttons go at
  once. When the row stays (step 8 skipped, or the delete stopped or was not fully
  done), the result stays beside the row, the row keeps Delete… (a new plan) and
  Forget, and at once shows §5.4's "Delete not finished: click Delete again" in
  place of Switch, Run now and Archive now whenever §5.4 would mark it
  [R4:NF-R4-ROW-KEPT-BUTTONS]; the page still does not reload until the result is
  dismissed.

### 3.2 Remove from this list

One request (`POST /api/archive/unlist {path}`, through `d.api`, same row rule
including the unreachable-folder rule) after an in-row question: "Remove from list?
The folder and its files are kept." — or, when §5.2 finds schedules, "Its scheduled
backup NAME will also be stopped. Remove from list? The folder and its files are
kept." Yes / No.

### 3.3 The command line [C17]

`mailarchive delete-archive -out DIR` and `mailarchive unlist-archive -out DIR`:
- at a terminal only: no `-yes` flag, no stdin pipe; no job file, schedule or
  unattended run can reach them. `keyVerbTerminal`'s closing text becomes per verb:
  for these two it reads "Run it in a terminal window (or use Delete… / Remove from
  list on the Your archives card in MailArchive Desktop)", never the Encryption
  card [R3:C17];
- refused while a MailArchive Desktop instance is running in any logon session of
  this account (`instance.LockIsHeld` on `desktop.lock` and every
  `desktop-s<N>.lock` in `instance.Dir()` — `%LOCALAPPDATA%\MailArchiveDesktop` on
  Windows, not the settings folder) [R3:C17]: "Use the Desktop, or shut it down
  first". Checked at start and again right before steps 4, 8 and 9; a Desktop found
  by the re-check before step 4, 8 or 9 stops the delete as `finished partial`
  (exit 3), naming the steps done, and the Desktop then shows the row as "Delete not
  finished" (§5.4);
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
each name Delete removes or walks is checked with `Lstat` and, on Windows, its
reparse tag (`util.IsLinkLike`, `util/linklike_windows.go`: the name-surrogate
bit), because Go gives a junction and a cloud placeholder the same mode bits
(ModeIrregular); the manifest and descriptor reads keep `util.OpenRegularFileIn`,
which already accepts a placeholder (`openregular.go:142-147`)
[R3:NEW-reparse-mechanism]. **Links** [C11,
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
  store token set: at every manifest version, the store tokens are the manifest's
  `Stores` values together with the first segments of the records' Paths that pass
  `util.ValidRecordedPath` and `state.SafeToken` — `Stores` holds only the stores a
  run has assigned since version 3 (`app/run.go:1596`, `app/graph.go:744`), a
  pre-v3 manifest has none (`state/manifest.go:313-317`), and Load drops or
  collapses some (`state/manifest.go:877-890`); a recorded Path is as untrusted as a
  Stores token, and C11's stem-only rule bounds both. §4.1's run leftovers and
  §4.3's "store tree" use the same token set [R3:NEW-prev3-stores,
  R4:NEW-R4-stores-gate]. And it ends in `.html`; then exactly its stem's `.html`,
  `-attachments.zip` and `.eml` are removed (as `reindex` derives them), never a
  bare `Path`. The whole delete is refused if a store tree holds another archive's
  marker (`.mailarchive-manifest.json`, an encrypted marker).
- **Index pages**: `index.html` / `index-N.html` only in folders a removable record
  passes through, and the root `index.html`; never by walking a tree.
- **Run leftovers at any depth in the store trees and the root** [C9]:
  `.mailarchive-*.tmp`, `.mailarchive-snapshot-*` (the `SweepTemps` rule, no age
  cut-off). An `-attachments.zip` is removed only as a recorded stem's sibling
  (Records above); any other `-attachments.zip` in a store tree is a §4.3 file
  (listed, kept unless ticked); one outside the store trees is kept (§8 (c)). The `SweepOrphanZips` rule is not used [NEW-orphan-zip].
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
break-glass copy layout; **the vault files last**. The archive's scratch folders
(every spelling, §3.1) by §5.3 step 5 [C7, R3:NF-C7-SCRATCH-NAME].

### 4.3 Mail copies the archive did not record [O1, C9]

The plan lists any `.html`, `.eml` or `-attachments.zip` in a store tree (a folder
under a store token, §4.1) that no record names as a stem's sibling and that is not
litter (`util.IsLitter`, §4.4) [R3:NEW-litter-scope]: copies from
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
  fully deleted. N files you chose to delete could not be removed: NAMES.
  MailArchive no longer recognises this folder as an archive: delete them by hand."
  (the manifest is not kept for them, so "click Delete again" is never offered;
  `finished partial`, exit 3) [R3:NEW-ticked-remedy]; on Windows
  a ticked read-only file has its read-only flag cleared under §9's rules. The
  box's choice is recorded in `deletions.log` and the record.

### 4.4 Litter and the folder itself [C5]

An operating-system file (`util.IsLitter`: `.DS_Store`, `desktop.ini`, `Thumbs.db`,
`._*`) is removed from every folder a removable record, run leftover, or a §4.3
file removed because the box was ticked passes through, whether or not that folder
is then removed [R3:NEW-litter-scope]. A litter *folder* is never cleared and
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

Each scheduler call has a time limit. The plan lists them all. **Descriptors**
[R3:NEW-FINDING-B]: a same-computer descriptor `bound.go` refuses because its name
is not this archive's (a copied descriptor, another archive's derived name, no
-out, or no Windows side-file) is not a refusal: nothing is stopped for it, the plan
lists "FILE names the schedule NAME, which MailArchive cannot confirm is this
archive's: not removed, check it yourself", and the file is deleted with the
folder. Only an unreadable descriptor (ESC-10) refuses, with §7's descriptor-row
wording; `schedule -remove -name NAME` is never printed for a name `bound.go`
refused. A descriptor recording another computer (`bound.go`'s other-host case) is not a
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
them; Yes first checks each with §5.3 step 2's absent-entry rule (an absent one
counts as stopped) and stops the rest with `schedule.RemoveLiveEntry` (never
touches the folder); if `RemoveLiveEntry` fails, nothing else is changed and the
result says "Could not stop the scheduled backup NAME: SCHEDULER ERROR. Already
stopped: NAMES (set them up again in Settings, or click Remove from list again).
The row is kept." ("Already stopped" only when at least one was stopped.) [R3:NEW-FINDING-A, R4:NF-R4-UNLIST-PARTIAL-STOP]. Otherwise it
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
| 0. Log `started`: append and fsync a `started` line to `deletions.log` (date, folder, plan counts, §4.3's choice; the archive's kind, plain or encrypted; for an encrypted archive the plan's record ID and Spelling, or "not recorded" [R3:NF-NF2-RERUN, R4:NF-R4-RERUN-PLAIN]) | written | stop: "Could not write MailArchive's deletion log, so nothing was deleted." |
| 1. In this order [R3 conflict (1)]: (a) take the archive lock (`lockfile`, the lock a backup takes) and re-check the plan (§3.1) except the scratch listing; (b) encrypted: lock this archive in the page whether or not it is open, with one new registry call that, all under `k.mu`, does what `Keys.lock` does (remove the entry, set `held`, bump `gen`) and sets a per-archive "being deleted" mark that `open` checks the way `hushed` is — so an open begun before it loses on `gen`, and every open after it, the person's own Unlock (typed or remembered) included, is refused with "This archive is being deleted." until the deferred cleanup clears the mark [R3:NF-R3-UNLOCK-MIDDELETE, R4:NF-R3-UNLOCK-MIDDELETE] — and closes with `e.close(true)`, as `lockAll` does (`Keys.lock` closes without waiting, `keys.go:404`, `:172`, leaving nothing to wait on); only when that returns does (c) run [R4:NF-R4-LOCK-NO-WAIT]; (c) re-list the scratch folders (every spelling), where any held reader lock now refuses [R3:NF-C7-OWN-READER]. `Keys.hush` is not used: it locks every archive [C13]. | lock held, plan unchanged | (a) fails: §7's or §3.1's wording; nothing changed. From (b) on, every stop adds: "This archive was locked on this page; unlock it again to read it." [R3:C13] |
| 2. Schedules: an entry counts as already absent only when the query says not installed and, on Windows, either schtasks says the task is missing (`schtasksSaysMissing`) or neither of its side-files (`<name>.xml`, `<name>.cmd` beside `DefaultWrapperPath`) exists — Pause and `RemoveLiveEntry` remove both (`wizard/pause.go`, `schedule.go:1225-1234`); an absent entry still has its side-files removed (`removeSchtasksSideFiles`). Any other entry goes to `RemoveLiveEntry` [R3:NEW-FINDING-1]; for the current archive, §5.1 (c)'s two. | every entry removed or absent | stop: "Could not stop the scheduled backup NAME: SCHEDULER ERROR. Already stopped: NAMES (set them up again in Settings, or click Delete again). Nothing else was changed." plus step 1's locked line when it applies; this `finished partial` marks the row "Delete not finished" (§5.4) [R3:NF-C18-NOTHING-CHANGED] |
| 3. Per-name config files, for each discovered name: `<name>.json` (`schedule.RemoveJobFile`), `<name>.jobfail.log`, `<name>.older.jobfail.log`; `<key>.alert-streak` and `<key>.sync-alert` for `backupalert.KeyFor` of each discovered entry's `-out` spelling [C6]; `last-failure.json` only when its `Name` is one of them | every listed file gone or absent | stop: "Could not remove FILE in MailArchive's settings folder." |
| 4. Settings and pointers: `Out` cleared if this is the current archive (the page then shows "No archive chosen yet", or the folder given with `-out` at launch when there is one [C16]); `BackupFor` cleared if it names the folder, and always when this is the current archive [C4]; `last-archive.txt` cleared if it names the folder | the settings write succeeds | stop: "Could not save MailArchive's settings, so this archive is still chosen. Click Delete again." |
| 5. Scratch (encrypted) [C7]: re-list the archive's scratch folders (every spelling, §3.1) under the lock with the single-archive `ScratchTargets` rule (only those folders, never the scratch root or another archive's) and remove them | removed or absent | stop, `finished partial`: "MailArchive's working folder for this archive is in use or holds files it did not put there: FOLDER. Steps done: schedules stopped, their settings files removed, settings updated, archive locked. Close MailArchive serve, search or other windows, then click Delete again." (never "nothing changed") |
| 6. The folder's contents (§4), the manifest or vault last, `.mailarchive.lock` kept (it is held) | no recorded name remains (§8 (a)) | continue to 7's release, then stop with §8 (a)'s result |
| 7. Release the lock (the file stays, `lockfile.go:111-114`); remove `.mailarchive.lock` by its exact name, retrying briefly (Windows cannot delete a held file); remove the folder if empty but for litter; append a durable `folder removed` or `folder kept` line to `deletions.log` [C2] | the lock released | — (an empty folder left is reported, not a failure; a lock file left: "Deleted. One MailArchive lock file (.mailarchive.lock) could not be removed: delete it by hand." and the delete is complete [R3:NF-C1-RESULT]) |
| 8. Keyring (encrypted) [C8, NF-1, NF-2]: only when the folder is gone, or `encmark.At` classifies it Gone (only an unheld `.mailarchive.lock` and litter remain). A fresh `PlanForget` on the Spelling the plan recorded (not the row's path; on a §5.4 re-run, the ID and Spelling from the original `started` line, never a new lookup) [R3:NF-NF2-RERUN], case Gone. When the folder still exists, `keyring.Forget` takes its archive lock (`forget.go:137, 173-189`), which re-creates `.mailarchive.lock`; after Forget returns, step 8 removes that file again by its exact name and retries removing the folder, as step 7 does; a re-created `.mailarchive.lock` that cannot be removed gets step 7's result line ("Deleted. One MailArchive lock file (.mailarchive.lock) could not be removed: delete it by hand.") and the delete is still complete [R4:NF-NF1-LOCK]; when that retry removes the folder, step 8 appends a durable `folder removed` line to `deletions.log` (after step 7's `folder kept`; §5.4 reads the last one) and the result's folder line becomes "The folder is gone", with step 7's lock-file or empty-folder line dropped [R4:NF-R4-STEP8-FOLDER-LOG]; a lock held by a run at that moment skips step 8 with its skipped line [R3:NF-NF1-LOCK]. `Whole` and `Keys` only when `plan.Last`; otherwise forget only this path, keep the key, and the result names the other recorded copies. "Never recorded" means the plan found no record, not a step-8 lookup failure: then no record is touched. | forgotten, or not applicable | skipped: "Files deleted; this computer still remembers its key: use Forget this encrypted archive here." |
| 9. The row: `archindex.Forget` — only when step 8 is done or does not apply (so a skipped step 8 keeps the row and its Forget button) [C18] | removed | "Could not remove the row from Your archives." |
| 10. The deletion record (§8.1) is built first; then log `finished complete` / `finished partial` / `finished refused` with what was left and the record's digest | written | the result says "This record's digest could not be added to deletions.log." |

Releasing the archive lock (step 7's first part), clearing the "being deleted"
mark, and step 10 always run, in a deferred cleanup, also after a refusal or a
recovered panic [R4:NF-R3-UNLOCK-MIDDELETE]. The delete handler
recovers its own panics and reports the steps done, never "nothing was changed"
after step 0 (`d.api`'s recover, `workspace.go:131-132`, would say so) [C18].

**After step 7** the archive is unlocked. A backup that passed its checks before
the delete and takes the lock after it is refused by the emptied-folder guard,
which is checked again under the run's own lock (§7) [NF-3, C12]: a kept folder
holding the person's files is refused by the guard; a folder that is gone is
refused as MA-689's "archive not present", even when it disappears between the
check and the lock (no `MkdirAll` for unattended runs) [R3:NF-R3-GONE-NOT-MA689]. A
kept folder that is empty
or holds only litter is not protected: the result then says "The empty folder could
not be removed: delete it by hand", and the plan's "not removed: check it yourself"
entries are repeated.

### 5.4 A delete not finished [C2]

A `started` line with no `finished` line for a folder (a crash, a power cut) shows
its row as "Delete not finished: click Delete again" in place of Switch, Run now
and Archive now. A re-run:
- treats the folder as gone only when the log has its `folder removed` line and
  the folder's parent is reachable; then it skips steps 1 and 5–7, says so, and
  runs the rest (config side, keyring, row) and step 10. Step 8 uses the ID and
  Spelling from the original `started` line, not a new lookup. For a plain archive,
  or an encrypted one whose line says "not recorded", step 8 does not apply (no
  record is touched; step 9 removes the row); only an encrypted archive's line with
  neither an ID and Spelling nor "not recorded" skips step 8 with its skipped line
  and keeps the row [R3:NF-NF2-RERUN, R4:NF-R4-RERUN-PLAIN];
- a `finished partial` written after step 2 began (any stop from step 2 on: step
  2's, step 5's or step 6's, or a command-line stop when a Desktop appears
  [R5:NF-R4-ROW-KEPT-BUTTONS]; not a §4.3 ticked-file failure, whose row step 9
  removed [R4:NEW-R4-ticked-delete-again]) also marks the row "Delete not finished:
  click Delete again", and for §3.1's unreachable-row rule and §7's not-reachable
  row such a delete counts as unfinished: a re-run with a `folder removed` line and
  a reachable parent continues as the folder-gone re-run [R3:NF-C18-NOTHING-CHANGED,
  R3:C17, R4:NF-R4-CLI-STOP-DEADEND];
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
| a scratch folder of this archive (any spelling, §3.1) holds something MailArchive did not put there [C7] | "MailArchive's working folder for this archive holds files it did not put there: FOLDER. Remove them, then click Delete again." | allowed |
| the folder is not reachable | "Reconnect the drive or folder first: nothing can be checked." — unless §5.4's `folder removed` line exists for an unfinished delete (§5.4) and the folder's parent is reachable; then the re-run continues [C2, R4:NF-R4-CLI-STOP-DEADEND] | allowed; when it is the current archive, `Out` is cleared [C3] |
| the folder is no longer an archive (by §4's test), no unfinished delete | "This folder no longer holds an archive. Use Remove from list." | allowed; `Out` cleared when current [C3] |
| paused (read-only) | allowed: the seal goes with the folder; the plan says "This archive is paused." [C13] | allowed |
| current archive | allowed (§5.3 step 4) | refused while reachable and an archive: "This is the archive in use. Switch to another archive, or add one (Add another archive), then remove this one from the list." [C3] |
| `captureBackupFor()` names it, it is not current | refused, §5.1 (a) [C4] | refused, §5.1 (b) [C4] |
| a descriptor that cannot be read (a descriptor `bound.go` refuses is listed, not refused, §5.1) [R3:NEW-FINDING-B] | "This archive's schedule record FILE cannot be read, so MailArchive cannot tell which schedule it records. If no schedule for this archive is installed, delete FILE, then click Delete again; otherwise reinstall that schedule to rewrite it." (`schedule.UnreadableDescriptorRemedy`, `schedule.go:848`) | allowed (only §5.2's own discovery is acted on) |
| the Desktop was started with `-out` naming this folder [C16] | "Restart MailArchive without -out, then delete." | "Restart MailArchive without -out, then remove it from the list." [C3] |
| protected: decided by file identity (`os.SameFile` up the ancestor chains), not path text — the folder is, contains or is inside the config folder, the scratch folder, the encrypted record folder, or another known archive; or it is or contains the home folder, a drive root, or a system folder (Windows: `%SystemRoot%`, `%ProgramFiles%`, `%ProgramFiles(x86)%`, `%ProgramData%`; macOS: `/System`, `/Library`, `/Applications`, `/usr`; Linux: `/usr`, `/etc`, `/var`, `/bin`, `/opt`) | "MailArchive will not delete this folder: it is (or is inside, or holds) NAME. Delete the archive's files by hand." [C15] | allowed |
| a link, junction or mount point on the path of a name Delete removes or walks, or as the folder itself [C11] | "This archive's folder is, or holds where its files are, a link: LINK. Delete it by hand." [R3:NEW-reparse-mechanism] | allowed |
| mixed state (`encmark.classify` Other; another archive's marker in a store tree) | "This folder holds files of more than one kind. Delete it by hand." | allowed |

**The emptied-folder guard [C12, NF-3]** (a change to the shipped engine, its own MA
row, in the adversarial pass's scope): an unattended run (scheduled backup, capture,
run-job) refuses an existing `-out` that holds no manifest, no encrypted marker and
no backup descriptor, and holds anything beyond litter. The guard and
`RequireExistingOut` are checked again under the run's own lock (after
`lockfile.AcquireAs`, ignoring `.mailarchive.lock` itself), and an unattended run
never creates `-out` (no `MkdirAll`); when `AcquireAs` fails because `-out` no longer
exists, the run reports `RequireExistingOut`'s refusal (`FailArchiveAbsent`), not
"archive unwritable" [R3:NF-R3-GONE-NOT-MA689]: a run that checked before a delete
and got the lock after it is refused. A first run after an install, and "Save and keep" into a
new empty folder, still work (empty, or holding the descriptor).

## 8. The result says exactly what happened [C5, C18]

Three kinds of leftover:
- (a) A name the archive recorded or owns (or a §4.3 file the person ticked) that
  could not be removed. It keeps the manifest/vault for a recorded name (so Delete
  can be run again) and the result is "Not fully deleted. N files could not be
  removed:" with up to 10 names and a remedy by error type (when only §4.3 files
  the person ticked are left, the remedy is §4.3's "delete them by hand", never
  "click Delete again" [R4:NEW-R4-ticked-delete-again]): a sharing violation,
  "Close Outlook or any program using them, then click Delete again"; access
  denied, "Windows security or antivirus blocked it: allow MailArchive or delete
  these by hand"; a device gone, "Reconnect the drive, then click Delete again". A
  briefly busy empty folder is retried before it is reported.
- (b) Litter: removed (§4.4).
- (c) Anything else (the person's files, §4.3's files when unticked): kept, listed as
  "kept: not the archive's", never blocking completion (never the lock file: step 7)
  [R3:NF-C1-RESULT].

Complete means no name the archive recorded remains and every file ticked under
§4.3 is gone; otherwise Status "not fully deleted", `finished partial`, exit 3
[R3:NEW-ticked-remedy]. Result lines: "Deleted. N
files (SIZE) removed. The folder is gone." / "…The folder is kept: it holds N
files that are not the archive's." plus one line per step that changed something
outside the folder ("Stopped the scheduled backup NAME", "This computer forgot the
archive's key", "Removed from Your archives"), §2's "What Delete does not reach",
and "Recorded in deletions.log". On the Desktop only: "Save this record now:
MailArchive keeps no copy after you close this result." [O2-07, R3:O2-07]

### 8.1 The plan and the deletion record, saved by the person [O2]

- **Before the decision**: the panel's **Save this plan** and `delete-archive
  -plan-out FILE` (a plan-only run, deleting nothing) save the plan as plain text,
  for a person who must get approval first: the record below with "Status: planned,
  nothing deleted". The plan's text is sent in the plan response (no new route);
  saving it does not use or extend the token [O2-08].
- **After it**: the result's **Save deletion record** and `-record-out FILE` save
  the record. The Desktop builds the text server-side once, sends exactly those
  bytes in the delete response, and holds them with the result until dismissal or
  Shut down (§3.1); the page saves them through the browser (a Blob download named
  "MailArchive deletion record - NAME - DATE.txt", as the fingerprint receipt is
  saved, `pages_scripts.go:615-618`), any number of times; nothing is sent anywhere
  else. Saving is offered, never required. Without `-record-out`, `delete-archive`
  prints the full record after the result, headed "Deletion record (copy it now:
  MailArchive does not keep a copy)" [R3:O2-07]. Dismissing a result whose
  record was not saved asks once: "Close without saving the deletion record?"
  [O2-07]
- **Files on the command line** [O2-02]: `-plan-out` and `-record-out` are checked
  before the plan is shown and before step 0, with `keyring.CheckNewFileOut`'s
  rules plus: not inside the folder being deleted or any known archive; its folder
  exists and can be written (a probe file created with `O_EXCL` and removed at once)
  [R3:O2-02]. Each file is
  created new (`O_EXCL`, never over an existing name or link) only when its text is
  final; it is not owner-only (it is meant to be shared). If writing the record
  fails after the deletion, the full record is printed to the terminal, the result
  says "The deletion record could not be saved to FILE: it is shown above", and the
  exit code stays the deletion's. A plan-only run follows §3.3's terminal and
  Desktop-running rules and exits 0, or 1 when refused; a plan-only run whose file
  cannot be created or written prints the plan, says "The plan could not be saved to
  FILE: it is shown above", and exits 1 [R3:O2-02].
- **Contents** (UTF-8, one "Key: value" per line; `Status` first and only once;
  in every value each line break (CR, LF, CRLF) is first replaced by a space, then
  the value goes through `util.StripControl` [R5:NF-R4-STRIPCONTROL-PROVEFAIL]; a key with several
  values repeats the key on one line per value [O2-06]):
  - Status: planned / complete / not fully deleted / refused after start;
  - the date and time with its time zone; the computer name and the account;
    MailArchive's version;
  - the folder as shown and its identity (volume + file id); plain or encrypted
    (and the archive id for an encrypted one);
  - as read at the plan (the re-check under the lock, §3.1, confirms the manifest or
    vault is unchanged, so they still hold) [R3:NEW-FINDING-record-capture-time]:
    the message count and the date range of the mail; the first recorded run (the
    first run header in `.mailarchive-history.jsonl`, `state/history.go:9`) and the
    last backup (the run history's newest entry whose status is ok), each labelled
    "as recorded in the archive", or "first run: not known" when the history is
    missing or unreadable [O2-04, R3:O2-04]. For an encrypted archive whose key was
    not open here at the plan, the message count, the date range and the first run
    read "not known: encrypted, key not open"; never left out, never estimated
    [O2-05];
  - files and bytes removed; the SHA-256 of the manifest (or vault file) read at the
    plan; the schedules stopped and those "not removed: check it yourself"; whether
    this computer's record and key were forgotten; §4.3's choice and count;
  - each leftover kind with its count; a leftover is named only by its store's number
    (store 1, store 2, … numbered in the byte order of the store tokens, §4.1's
    token set, so the saved plan, the record and the screen agree; the on-screen
    plan and result show each store's number beside its full name
    [R4:NF-R4-STORE-NUMBER-ORDER]) and the date-and-hash parts of its stem (for example
    `store 1/…/2024-03-01_0900_…_ab12cd34.html`); a store token, which can be a
    mailbox address, is never written, nor a folder name below it or a subject slug
    — the full names are shown only on screen [O2-01, R3:O2-01];
  - "What Delete does not reach";
  - the folder identity and the manifest SHA-256 lines are worded identically in the
    saved plan and the record, so an approver can match the two: the same digest
    means the archive is the one approved; a different digest means it changed (for
    example, a backup ran) after the plan was saved [R3:O2-08];
  - last line `Record-SHA256: <hex>`: the SHA-256 of every byte before that line.
    The text is built once, before step 10, and never rebuilt; step 10 writes the
    same hex to the `finished` line [O2-03].
- **Never in it**: subjects, senders, recipients, addresses (store tokens
  included), folder names below a store, or any mail content.
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
- Q1 fixtures: an archive written at manifest versions 1, 2, 3, 4, 5 and 6 (the
  version-1 and version-2 fixtures have no Stores map [R3:NEW-prev3-stores]; a
  version-6 manifest whose Stores map lacks a store that still has records — a
  version-2 archive of two stores, upgraded by a run of one — has that store's mail
  removed [R4:NEW-R4-stores-gate]), and
  one left by a crashed run (temps, snapshot, orphan zip, `search.db.rebuild`):
  every file the archive wrote is removed or listed [C9].
- §4.3: unrecorded copies (including a person's own `x-attachments.zip`) kept by
  default and listed; ticked, exactly the listed ones removed; a file added after
  the plan refuses; never any other kind of file; the choice in `deletions.log` and
  the record [O1, NEW-orphan-zip]; a ticked file held open → "Not fully deleted",
  remedy "delete them by hand", `finished partial`, exit 3, row removed
  [R3:NEW-ticked-remedy]; ticked copies in a folder holding a `.DS_Store` leave no
  folder kept for litter alone [R3:NEW-litter-scope].
- Encrypted: folder gone, record and key forgotten (`plan.Last`); another recorded
  copy keeps the key [C8]; a folder kept only for an unheld lock file or litter
  still forgets the record, and no `.mailarchive.lock` is left in the kept folder
  [NF-1, R3:NF-NF1-LOCK]; a folder kept at step 7 only for a held lock file and
  removed by step 8's retry → `folder removed` is logged and the result says the
  folder is gone [R4:NF-R4-STEP8-FOLDER-LOG]; the record found by the plan's Spelling after the folder
  is gone [NF-2]; a crashed run's scratch under another spelling of the folder is
  removed [C7, R3:NF-C7-SCRATCH-NAME]; Delete of a large encrypted archive open in
  this page (index in scratch) is not refused and completes [R3:NF-C7-OWN-READER];
  an Unlock of the archive (typed or remembered) sent between step 1 and step 10 is
  refused, and no scratch folder of it exists after the delete
  [C13, R3:NF-R3-UNLOCK-MIDDELETE]; an Unlock started at the moment of step 1 (b)
  either loses or is refused, and no reader of the archive is made after step 5; a
  refusal after step 1 (b) leaves the archive unlockable again [R4:NF-R3-UNLOCK-MIDDELETE].
- Order: the manifest/vault removed last (a fault before it leaves a recognised
  archive, re-run finishes); schedules first (a recording fake scheduler);
  `started` before the first removal; each step's stop wording (§5.3's table); the
  lock released and `finished` written after a panic [C18].
- §5.4: a crash after `folder removed` → re-run with the folder gone finishes; a
  crash after `folder removed` for an encrypted archive reached through another
  spelling → the re-run forgets the record and key [R3:NF-NF2-RERUN]; a crash in
  step 6 then the drive unplugged → re-run refused, keyring untouched; a folder with
  no manifest and mail files left → `finished partial`, never complete [C2]; a step-2
  stop after one entry was removed names it as "Already stopped" and marks the row
  "Delete not finished" [R3:NF-C18-NOTHING-CHANGED]; a crash after `folder removed`
  for a plain archive → the re-run removes the row and never mentions a key
  [R4:NF-R4-RERUN-PLAIN]; a command-line delete stopped before step 8 by a Desktop,
  after the folder was removed → Delete again from the Desktop finishes (keyring,
  row) [R4:NF-R4-CLI-STOP-DEADEND].
- The lock: deleted under the lock, then the lock file and folder gone (Windows
  lab) [C1].
- Discovery: a custom `-name`, two `-name` installs (Windows, Linux; macOS: the
  earlier one is listed as not removed), another spelling, a legacy verify, a
  denied or hung scheduler; an unreadable descriptor refuses, and its remedy,
  followed literally, lets the next Delete go ahead; another computer's descriptor
  is listed, not refused [C6, NF-OTHER-HOST-DESCRIPTOR]; a copied descriptor naming
  another archive's entry is listed, not refused, and that archive's entry is
  untouched [R3:NEW-FINDING-B]; delete of a paused archive on a scheduler whose
  "missing" message is not English completes; a stopped or denied Task Scheduler
  stops the delete at step 2 with the scheduler's error and never reports the
  schedule stopped [R3:NEW-FINDING-1].
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
- Routes: plan, delete, dismiss and unlist — no token, wrong token, foreign Origin
  or Host, GET → 405, side effect absent; an unreachable row matched by its spelling
  only [C20].
- Remove from list: the row gone and not re-added; folder byte-identical; schedules
  named and stopped only after Yes; unplugged drive; current + unreachable clears
  Out [C3]; a paused archive on a scheduler whose "missing" message is not English
  is removed from the list [R3:NEW-FINDING-A]; a second entry that fails after the
  first was stopped names the first as "Already stopped" [R4:NF-R4-UNLIST-PARTIAL-STOP].
- Emptied-folder guard: an unattended run refuses a non-empty folder with no
  archive; a first run into an empty folder and into one holding only the
  descriptor still works; a run passes the pre-lock check, a delete completes, the
  run takes the lock → refused, folder not recreated [C12, NF-3]; a run whose folder
  is removed between its check and the lock is refused with the archive-not-present
  kind [R3:NF-R3-GONE-NOT-MA689].
- Windows: read-only archive file deleted; read-only person's file kept (and
  removed when ticked and listed); a file held open reported, the rest deleted,
  re-run finishes; a path over 260 characters; a hard-linked file not chmod-ed
  [C11].
- Synced folder (Windows lab, pending): OneDrive and a synced SharePoint library
  detected; the cloud-only placeholder, timing and paused-sync behaviour observed
  before the conditional wording is lifted [C14].
- CLI: refuses without a terminal (with the per-verb closing text) and while a
  Desktop runs (any logon session; the lock files in `instance.Dir()`); a Desktop
  started mid-delete gives `finished partial` with the steps done, never "refused"
  [R3:C17]; exit codes 0/3/1 and X1 updated [C17]; `-plan-out`/`-record-out`: an
  existing file, a link, a path inside the folder, and a path in an unwritable
  folder each refuse before anything is removed; a failed record write after the
  delete prints the record [O2-02]; a delete without `-record-out` prints the record
  [R3:O2-07]; a plan-only run that cannot write its file exits 1 [R3:O2-02];
  uninstall removes `deletions.log` and its plan names it.
- The page: panel beside the row, button disabled until the NFC name matches, no
  `confirm(`/`prompt(` in either handler, one panel at a time; the result and Save
  deletion record survive the dashboard's timed reloads, a manual reload, and a
  reload from another action until dismissed; Save works more than once
  [R3:NF-R3-RESULT-RELOAD, R3:NEW-FINDING-record-held-vs-sent]; after a partial
  delete, Delete… and Forget work from the row without dismissing the result
  [R3:NF-R3-ROW-KEPT], and Switch, Run now and Archive now are not offered on the
  row before dismissal [R4:NF-R4-ROW-KEPT-BUTTONS]; two deletes before either result
  is dismissed: both records stay savable, and each is dismissed on its own
  [R4:NF-R4-HELD-RESULTS-MANY]; the unsaved-record question [C19, O2-07].
- Plan and deletion record [O2]: the saved plan deletes nothing; the record's fields
  for a complete, a partial, a refused-after-start and a plan; a complete delete's
  record shows the first run, the last backup, the message count and the date
  range read at the plan (not "not known") [R3:NEW-FINDING-record-capture-time];
  the planted archive has a leftover of each kind and an unrecorded copy whose file
  and folder names contain the planted subject — neither the saved plan nor the
  record contains it [O2-01]; a Microsoft 365 archive whose store token is the
  mailbox address: the address appears in neither [R3:O2-01]; an archive folder
  whose name (Linux/macOS) contains a newline followed by "Status: complete", plus a
  carriage return, an ESC (0x1b) and U+0085, and a schedule name or manifest value
  that reaches the record with the same text, leave exactly one Status line, saying
  "not fully deleted", and the record holds no character below 0x20 except its line
  ends, no 0x7f and none in 0x80–0x9f, and the folder line holds the planted text
  with a space where the newline was ("…x Status: complete") — two prove-fails:
  removing `util.StripControl` (the control-character assertion goes red) and
  removing the line-break replacement (StripControl then joins the words, and the
  space assertion goes red) [R5:NF-R4-STRIPCONTROL-PROVEFAIL, O2-06,
  R3:NEW-FINDING-o206-test-cannot-fail, R4:NF-R4-STRIPCONTROL-PROVEFAIL]; an
  archive with two stores gives the same store numbers in the saved plan and the
  record [R4:NF-R4-STORE-NUMBER-ORDER]; a record for a locked encrypted archive
  shows the three sealed values as not known and no other value [O2-05]; a saved
  record re-hashed without its last line equals the `finished` line's digest
  [O2-03]; a saved plan and the record of the same unchanged archive carry identical
  folder-identity and manifest-digest lines, and after a backup in between the
  digest lines differ [R3:O2-08].

## 12. Questions answered by the review

Q1 → §4.1, §4.3 [C9, O1]; Q2 → no password (§6); Q3 → the delete locks the
archive itself with one new registry call that does `Keys.lock`'s work and sets a
per-archive "being deleted" mark, all under `k.mu`, then a waiting close
(`e.close(true)`) — not `Keys.lock` and not `hush` (§5.3 step 1 (b)) [C13,
R3:NF-R3-UNLOCK-MIDDELETE, R5:NF-R5-Q3-KEYSLOCK]; Q4 →
§5.1 (c), §7 [C4, C16].

## 13. Build slices (each with its tests, prove-fail → prove-pass, pushed alone)

1. **Core** (`internal/archdelete`, new): the file list (§4), discovery (§5.1),
   the refusals (§7), the ordered steps (§5.3) behind small seams for the
   scheduler, settings and keyring; `deletions.log`; the record text (§8.1).
2. **Emptied-folder guard** (engine, §7): the re-check under the run's lock and no
   `MkdirAll` for unattended runs.
3. **Desktop**: the four routes (plan, delete, dismiss, unlist) [R4:NF-R4-THREE-ROUTES], the row buttons, the panel, the result, Save this
   plan / Save deletion record, "Delete not finished" rows.
4. **Command line**: `delete-archive`, `unlist-archive`, `-plan-out`, `-record-out`,
   X1.
5. **Uninstall and the guide**: `deletions.log` in `ConfigTargets`, the uninstall
   plan's line, the guide's two sections.

## 14. Build notes (1 November 2026, from slices 1 to 5)

Rulings the builders' adversarial passes asked for, made in the main session under the
owner's standing instruction to make technical calls and surface only product rulings.
The text above is unchanged; where it and this section differ, this section is what was
built. Each is in its catalog row.

**Slice 2, the emptied-folder guard (MA-956).**
- *MailArchive's own files are not the person's.* Read literally, "anything beyond
  litter" refused a first run after a refused start (MA-646's `.mailarchive-lastrun.json`)
  and a settings folder kept in the archive folder (CC16 warns, not refuses). Built: the
  run records a refused run leaves (`.mailarchive-lastrun.json`,
  `BACKUP-NEEDS-ATTENTION.txt`, `.mailarchive-runhistory.json`,
  `.mailarchive-sourcestreak.json`), the lock file, and the caller's own files directly in
  the folder (`OwnFiles`: its log and the scheduler's stderr file, `_outlook-pst`, the
  Desktop's settings folder, the credential file), matched by file identity, count as
  MailArchive's. Delete removes all of them before the manifest, so a part-way delete
  still leaves its manifest.
- *A run that saw the archive is held to it.* A run that found an archive at any of its
  checks (or its caller's entry check, `Options.ArchiveAtEntry`) is refused under its lock
  when the folder holds none, whatever is left. On Windows a delete cannot remove a file
  the run holds open (its log, its PSTs), so own files excuse only a run that never saw
  an archive. This is stricter than §7 above.
- *"Is there an archive here" opens, it does not parse.* The manifest or descriptor must
  be a regular, non-empty file opened inside the folder without a link; an encrypted
  archive is whatever `encmark.Here` says (MA-770's gate: never `vault.EncryptedMarker`
  outside encmark and vault, here or in §4). A manifest that opens but does not parse
  passes the guard and fails as `manifest_unreadable` under the run's lock, before any
  write.
- *"Save and keep" into a folder that already holds files* (finding A3-02): left to the
  guard for now (the first scheduled backup there is refused with the guard's words).
  Slice 3 refuses it at Save, in the same words, so the person is not told later.

**Slice 1a, the plan (MA-957..MA-963).**
- Three refusals added to §7: the manifest cannot be read (`CaseUnreadable`), is from a
  newer MailArchive (`CaseNewerFormat`), or this computer's record of encrypted archives
  cannot be read (`CaseRecord`). None calls the folder "no longer an archive", so Remove
  from list cannot clear an archive in use because of a read error.
- `desktop.ini` and the macOS folder-icon file at the archive folder's root are kept, not
  litter (they may be the person's folder settings); in store subfolders they stay litter.
- The fixed names gain the rotated run logs (`.log.1`).
- A descriptor whose recorded folder is another spelling or another folder is named,
  not stopped: a schedule copied from an unplugged archive is "check it yourself".

**Slice 1b, the steps (MA-964..MA-970).**
- The `started` line (§5.3 step 0) also records the folder's identity and the delete's
  run id; every later line carries the run id; a `key forgotten` line follows step 8. A
  different folder at the same path is never taken for an unfinished delete, and
  overlapping deletes keep their own state.
- A re-run on a folder with no manifest never finishes "complete" and never runs steps 8
  and 9; "Delete again" finishes them (§5.4 as written).
- On macOS and Linux, step 7 re-takes the archive lock for the instant it removes the
  lock file, so a lock a backup took after step 7's release is never removed.
- Slices 3 and 4 must pass `UnlistDeps.Named` (the names Remove from list's question
  showed), `SettingsDir` (a Desktop started with `-config`) and, in the keyring adapter,
  `keyring.ForgetPlan.ID` into `KeyPlan.ID` (without it step 8 always skips: safe, but
  the key is never forgotten).

**Slices 3 to 5 and their re-verifications (MA-1010 and the rows named).**
- *§6's key line is picked at the plan (MA-1010; slice 5's re-verification, residual 2).*
  "This computer will forget this archive's key" was not always true: step 8 forgets the
  key only when the folder is removed and it is the last copy this computer recorded, and
  otherwise keeps it for the other recorded copies (MA-1004's guide says so). The plan now
  asks the keyring's read-only forget plan (`Keyring.PlanForget`, the record the plan found,
  by its ID) and says one of: "If the folder is removed, this computer will forget this
  archive's key: this is the last copy of it this computer recorded. Any other copy of this
  archive will then need its password or recovery key to open."; "This computer keeps this
  archive's key for its other recorded copies: COPIES. If the folder is removed, it forgets
  only this copy."; for a folder this computer did not record, "This computer has not
  recorded this archive folder, so Delete forgets no key here."; and, when the forget plan
  cannot be had, the line that says both cases. The Desktop's panel and the command line
  show the same line. Without a secret store the command line's added sentence (MA-999)
  still says a remembered key is then kept.
- *Sealed values when the key is open here (MA-1011; slice 3a's conformance D2).* §8.1's
  "not known: encrypted, key not open" is written only when it is true. When the archive
  is open on the Desktop's page, the plan reads its sealed manifest and timeline with that
  open archive's key, for that read only (`keyring.ReadSealedRecord`, a reader's read-only
  load; no new key handling), and states the message count, the mail dates and the first
  run by the plain archive's rules. The command line has no page and always says "not
  known".
- *The scratch listing is at the plan, as §3.1 says (MA-1012; slice 3a's conformance
  D1).* Slice 3a listed the scratch folders only at step 1 (c), after the started line
  and after the page had locked its open archive. The plan now lists them (the Desktop's
  plan route and the command line's plan): a reader another program holds, or a file
  MailArchive did not put there, refuses there, before anything is logged or locked. A
  reader of this process is excused only while this page has the archive open. Step 1
  (a)'s re-check under the lock leaves the listing out; step 1 (c) still lists again.
- *Uninstall and a delete never overlap (MA-1007; slice 5's adversarial pass, a gap in
  §8.1, which says only "uninstall removes it").* Uninstall, its plan and its real run, on
  the Desktop and at the command line, refuses while deletions.log holds a delete that is
  running or did not finish: removing the log would leave that delete with no way to
  finish, since a re-run reads only the log. It names the row and how to finish it
  (Delete… on its row, or `mailarchive delete-archive -out FOLDER`), and checks again
  right before the settings files go. A log it cannot read stops it too, with advice for
  the cause. The way out for a delete that will never be finished: copy deletions.log
  somewhere safe, delete it by hand, then uninstall again (an encrypted archive's key is
  then kept until Forget). On the Desktop, Delete permanently and Remove from list's Yes
  are refused while Remove runs and after it has uninstalled.
- *No secret store (MA-999).* When the secret store cannot be opened (a headless Linux
  session, a locked Keychain or Credential Manager), step 8 does not forget an archive's
  last recorded copy: no one can tell whether its key is remembered, and forgetting the
  record would leave a key nothing points to. The files are deleted; the record, any
  remembered key and the row (with its Forget) are kept, and the result says this
  computer still remembers the key. The command line's plan says so first.
- *A stop held until just before step 2 (MA-1000, `CaseStopped`).* At the command line,
  Ctrl-C or the terminate signal after the started line is held, and the delete refuses
  right before step 2 (`ExecDeps.BeforeStep(2)`): nothing changed, its finished line
  written, so the row is never "Delete not finished". The same check refuses when a
  MailArchive Desktop started meanwhile. From step 2 on a stop is "not fully deleted"
  (§5.4).
- *A name typed in its `\uXXXX` form (MA-1002).* A folder name holding a control
  character (U+0080 to U+009F are allowed in NTFS names) is shown at the terminal with
  that character written out as `\uXXXX`; typing the name as shown deletes the archive,
  as typing the name itself does.
- *The "At this terminal…" lines (MA-1002).* A result line in the Desktop's words that
  names a Desktop control is followed at the terminal by its command: "At this terminal,
  Delete again is: mailarchive delete-archive -out ROW", Remove from list's
  `unlist-archive`, Forget this encrypted archive here's `forget-archive`, and
  "Switching archives, Add another archive and Settings are in MailArchive Desktop."
- *The "This archive" card (MA-982, MA-986).* A lone current archive has no Your
  archives card (MA-666), so its row — Remove from list and Delete… with the rest of a
  row — is drawn on a card of its own, "This archive", and a delete of it that stopped at
  step 2 still shows "Delete not finished: click Delete again" with a Delete… to click.
  This is the builder's placement: the operator may instead rule that Your archives is
  drawn for a lone current archive (amending MA-666).

**The residual fixes and their security pass (1 November 2026; MA-979, MA-996, MA-999,
MA-1000, MA-1010 to MA-1013).**
- *The command line writes the Desktop's row first (MA-996, MA-997).* The Desktop adds
  its current archive, the folder a scheduled backup is for and the wizard's last archive
  to Your archives only when it draws its page, so such a row may be missing from
  `known-archives.json`. `delete-archive` and `unlist-archive` match the list as the
  Desktop would draw it, and before anything changes (the re-check before step 2, after
  the name matched; before Remove from list acts) write that one row into the list as the
  Desktop's tidy-up would (`CommandLineDelete.ListRow`): only the folder the Desktop would
  add itself, nothing for a row already listed, nothing for a plan-only run or a name
  that does not match. A delete stopped part way then still has its row and its "Delete
  not finished". A list that cannot be saved refuses there, nothing changed.
- *deletions.log keeps its last 1 MiB for deletes already started (MA-1013).* A new
  delete's started line is refused at step 0 ("Could not write MailArchive's deletion log,
  so nothing was deleted.") when the log, with that line and 1 MiB of room for the
  delete's later lines, would pass the 32 MiB that MailArchive reads. A delete already
  started is never stopped by the limit. So the log is never written past what MailArchive
  can read back (rows, plans, uninstall).
- *Actions that a delete must not meet hold their folder (MA-979).* Pause and Resume, a
  Switch or Save settings naming a folder, and the Scheduled backup card's Install each
  hold their folder in the record of deletes under way for the whole request, under the
  lock a delete's start takes, so their check and their action are one step. A delete of
  that folder that begins meanwhile is refused in the holder's own words, its plan kept
  (Pause and Resume: "This archive is being paused or resumed…"; the save: "Settings
  naming this archive are being saved…"; the Install: "A scheduled backup of this archive
  is being installed…"). The holds, and the job, preview and export starts, match the
  folder on disk as well as its spelling (builder's words, for the operator).
- *No secret store: the key is kept, and said so everywhere (MA-999, MA-1010).* With no
  secret store, step 8 keeps the last copy's record, any remembered key and the row; the
  plan's key line says so ("This computer's secret store could not be opened, so this
  computer will keep its record of this archive, any key it remembers for it and its row,
  even if the folder is removed. Use Forget this encrypted archive here later."), and the
  saved plan's "This computer's record and key" line now says the same thing ("to be
  kept: this computer's secret store could not be opened"; with other recorded copies,
  "this copy to be forgotten; the key is kept for N other recorded copies"; when the
  keyring's plan is not known, "not known: …"). Forget this encrypted archive here
  refuses the whole archive, `forget-archive` refuses Yes to it, and Forget every
  encrypted archive on this computer forgets nothing (`keyring.ForgetAll` with no store),
  each saying nothing was changed: no one can tell whether a key is remembered, and
  forgetting the record would leave it with nothing pointing to it. Forgetting only a
  folder still works.
- *The archive open here is found by its folder on disk and its archive ID (MA-1011,
  MA-1012).* The plan's sealed values and its "open here" excuse for this page's own
  reader ask for the open archive whose folder is the folder planned on disk (by file
  identity, as step 1 (b) closes it) and, for the sealed values, whose archive ID is the
  one the folder's key files claim. Never by the folded name alone, which merges folders a
  case-sensitive or NTFS drive keeps apart ("Straße" and "Strasse") and misses another
  spelling of the same folder (a link, a mapped drive).
- *A stop never waits on a folder that hangs (MA-1000).* At the command line, the removal
  of a plan file being written when Ctrl-C arrives is bounded by the same two seconds as
  its close; one that has not answered by then is reported as maybe incomplete, and the
  verb exits.
