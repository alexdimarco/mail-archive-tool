# Design: delete an archive, and remove one from the list

Status: PROPOSED, revision 2 (2026-11-01). Revision 1 (commit 3250479, sha256
f9efbf99…) was reviewed pre-code: `docs/review-archive-delete-predesign.md`,
verdict GO_WITH_CONDITIONS, conditions C1–C20, all folded in here (each marked
[Cn] where it lands). The owner's ruling on the review's one question is folded in
as [O1]. Owes, after a fold-verification of this revision: the adversarial pass and
the friction review before release. Target: 1.5.3 (owner, 2026-11-01: "bundle into
1.5.3").

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
  The shape below is the lead's; the owner may change it.

## 2. What is built

Two row actions on the Your archives card, each with a command-line verb (R33):

| Action | What it changes | What it never changes |
|---|---|---|
| **Remove from this list** (`mailarchive unlist-archive -out DIR`) | the row in `known-archives.json`; `last-archive.txt` when it names the folder; `Out` when the folder is the current one and unreachable or no longer an archive; and, only after saying so in its question, this folder's own schedules and their per-name config files (§5.2) [C3] | the folder and everything in it; keys and this computer's record of encrypted archives; every other archive's rows, schedules and files |
| **Delete this archive…** (`mailarchive delete-archive -out DIR`) | the archive's own files (§4), the schedules the plan lists (§5.1), their per-name config files, `Out` / `BackupFor` when they name the folder, the folder's scratch copy, the row, this computer's record and remembered key for an encrypted archive (§6), and one pair of lines in `deletions.log` | any file the archive did not write unless the person ticks §4.3's box; any other archive; the shared settings (`Interval`, `WeeklyTime`, alerts, Microsoft sign-in) [C14] |

Delete is permanent: no Trash or Recycle Bin (retention). The caution is in the
plan and the typed confirmation (§3), the refusals (§7) and the honest result (§8).

**What Delete does not reach** (a fixed line in the panel, the result and the
guide) [C14]: OneDrive's online recycle bin (it keeps deleted files about 30 days
on a personal account, up to 93 on a work account) and its version history; Windows
File History and Previous Versions; macOS Time Machine; copies, exports or
decrypted copies made elsewhere; sync tools other than those detected (§9).

The panel carries one line telling the three apart [C19]: "Remove from list forgets
the row and keeps the folder. Delete erases the archive's files. Forget (encrypted
archives) only stops this computer protecting the folder."

## 3. The decision and the requests

### 3.1 Delete: plan, then the typed name

1. **Plan** (`POST /api/archive/delete/plan {path}`) [C20]: POST-only, through
   `d.api` (`GuardLocalPOST`, CSRF token, epoch, body cap). The path must be a row
   in `known-archives.json`, matched by file identity (`util.ResolveExisting` +
   `os.SameFile`), not text. Read-only: it runs §7's refusals, the schedule
   discovery (§5.1), the file list (§4), and the protected-folder check (§7, [C15]);
   it counts files and bytes and notes a synced folder (§9). The plan is held
   server-side for 10 minutes under a one-use, kind-filtered token
   (`pendingItem.deletePlan`, `isDeletePlan`; `discard` and `ofCurrentArchive`
   cover the new kind). It records the folder's identity (volume + file id), the
   manifest's or vault's size and modification time, and the discovered entries.
2. **Delete** (`POST /api/archive/delete {token, typed, unrecorded}`): `typed` must
   equal the folder's own name (last path element), compared after Unicode NFC
   normalisation on both sides, with no case folding [C19]. `unrecorded` is §4.3's
   box (default false). Under the archive lock the plan is re-checked: the folder
   opened by `os.Root` must be the same folder (identity) as the plan's [C15], the
   manifest/vault unchanged, the entries unchanged, and none of §7's refusals new.
   Any difference refuses with "This archive changed since you clicked Delete. Click
   Delete again." and nothing removed.

The page shows the plan in a panel beside the row (`noteBeside`, MA-955), never a
browser `confirm()`/`prompt()` [C19]:
- the full folder path with its drive, files and size, the schedules that will be
  stopped, "This archive is paused." when it is, the synced-folder warning when it
  applies, the "kept: not the archive's" count, §4.3's box when there are any,
  the encrypted-key line (§6), "What Delete does not reach", and "A line is added
  to this computer's deletion log (deletions.log), which keeps the date, the folder
  and what was removed.";
- "Type the folder's name, NAME, to delete it" with a text box (paste allowed), a
  live "doesn't match yet" until it matches, **Delete permanently** (danger style,
  disabled until it matches) and **Cancel**;
- opening a delete panel closes any other one; the result stays until the person
  dismisses it.

### 3.2 Remove from this list

One request (`POST /api/archive/unlist {path}`, same guard and row-identity rule)
after an in-row question: "Remove from list? The folder and its files are kept." —
or, when §5.2 finds schedules, "Its scheduled backup NAME will also be stopped.
Remove from list? The folder and its files are kept." Yes / No.

### 3.3 The command line [C17]

`mailarchive delete-archive -out DIR` and `mailarchive unlist-archive -out DIR`:
- at a terminal only (`keyVerbTerminal`): no `-yes` flag, no stdin pipe; no job
  file, schedule or unattended run can reach them;
- refused while a MailArchive Desktop instance is running ("Use the Desktop, or
  shut it down first"), so one process changes the list at a time;
- delete prints the plan, asks §4.3's question (default no) when it applies, then
  asks for the folder's name;
- exit codes: 0 deleted (or removed from the list), 3 not fully deleted, 1 refused
  (including a name that does not match), 130/143 stopped by a signal.

## 4. What Delete removes inside the folder

A plain archive can share a folder with the person's own files (no empty-folder
check on the plain create path, `app/run.go:596`, `wizard/wizard.go:563`) and
mirrors the source's folder names, so the folder is never removed wholesale.
Removal is by name, through an `os.Root` opened on the folder; every path element
is checked with `Lstat`, and a link, junction or other reparse point **at any
level** is refused (the whole delete, before anything is removed) [C11]. Folders
are removed with `os.Remove` (never `RemoveAll`), deepest first.

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
  cut-off), an orphan `-attachments.zip` beside a recorded stem's folder (the
  `SweepOrphanZips` rule).
- **Fixed root names**: `search.db` (+ `-wal`, `-shm`, `-journal`),
  `search.db.rebuild` (+ `-wal`, `-shm`), `README.txt`, `attachments-report.tsv`,
  `attachments-report-legacy.tsv`, `BACKUP-NEEDS-ATTENTION.txt`,
  `ARCHIVE-INTEGRITY-ATTENTION.txt`, `_COPY-PROBLEMS.txt`, `_COPY-NOT-FINISHED.txt`,
  `mailarchive.log`, each discovered name's `<name>.log`, `<name>.stderr.log` and
  verify log [C6, C9], `_outlook-pst/` (its `.pst` files only), and every
  `.mailarchive-*` state file and descriptor.
- **The history file** `.mailarchive-history.jsonl` is removed as a state file; it
  names no files, so it is not a source of names [C9].
- **The manifest last** (§5's order), so a delete stopped part way is still
  recognised as an archive and can be finished.

### 4.2 Encrypted archive

The fixed root names (`encmark.reserved`, `tempRe`), with `units/`, `chunks/`,
`inventory/` only in their validated layouts (`vault/marker.go`), `program/` only
in the break-glass copy layout; **the vault files last**. The archive's scratch
folder by §5 step 5 [C7].

### 4.3 Mail copies the archive did not record [O1, C9]

The plan lists any `.html`, `.eml` or `-attachments.zip` in a store tree (a folder
under a token in `Stores`) that no record names: copies from older formats, a
crashed run, or files the person put there — they cannot be told apart. The panel
shows their count and the first 10 names under "Mail copies this archive did not
record", with an **unticked** box: "Also delete these N files (for retention)". The
command line asks the same, default no.
- Unticked (default): they are kept and listed in the result as "kept: not
  recorded by the archive". The result never says only "Deleted" while they remain
  ("Deleted. N mail copies the archive did not record were kept.").
- Ticked: they are removed with the archive's files, under every rule of §4 (same
  `os.Root`, same link refusal, only those three file kinds, only inside store
  trees, never any other file). The box's choice is recorded in `deletions.log`.

### 4.4 Litter and the folder itself [C5]

An operating-system file (`util.IsLitter`: `.DS_Store`, `desktop.ini`, `Thumbs.db`)
in any folder the archive owns is removed with that folder. A litter *folder* is
never cleared and keeps its parent. The archive folder itself is removed last
(§5 step 7), only when empty but for litter files.

## 5. The order of a Delete, and of a Remove from this list

### 5.1 Discovery (read-only, in the plan) [C6]

Every scheduler entry for the folder: `DefaultNameFor(path)` and its `-verify`;
`RecordedBackupName` / `RecordedVerifyName` from the folder's descriptors, read
before anything is removed; every job file in `job.Dir()` whose `Out` is the same
folder under `SameArchivePath` (the `corroboratedRunJob` scan, which finds other
spellings); every `OurInstalled` entry whose `-out` (cron block, wrapper, XML
side-file or job file) is the same folder. Each scheduler call has a time limit.
The plan lists them all. Refused before anything changes, with the by-name remedy
(`removereach.go`): a descriptor that cannot be read (ESC-10), or one recording a
name `bound.go` refuses. A custom name that cannot be proven from the scheduler's
own definition is listed as "not removed: check it yourself". A descriptor
recording another computer is named: "Computer B also backs up into this folder;
remove its schedule there." [C12]

**BackupFor** [C4]: when `BackupFor` names folder X, the capture registered under
X's name is the current archive Y's backup.
- (a) Delete of X (X not current) is refused, nothing changed: "The scheduled
  backup of your current archive Y is still registered under this folder's name.
  In Settings, remove the scheduled backup and set it up again (it is then
  registered under Y), then delete this one."
- (b) Remove from list of X gives the same reason and remedy.
- (c) Delete of the current archive Y removes both entries `removeTasks` removes
  (the one named for `Out` and the one named for `BackupFor`) and clears
  `BackupFor`, so no capture falls back to its own `-out` (`capture.go:55`,
  `launch.go:199`).

### 5.2 Remove from this list [C3]

Discovery as §5.1 but from this computer's records only (job files under
`SameArchivePath`, `DefaultNameFor` and `-verify` entries, recorded names when the
folder is reachable), so it works with the drive unplugged. If any are found the
question names them; Yes stops each with `schedule.RemoveLiveEntry` (never touches
the folder) and removes that name's job file and per-name config files (§5.3 step
3's list). Then `last-archive.txt` is cleared when it names the folder, `Out` is
cleared when the folder is current (allowed only when it is unreachable or no
longer an archive), and `archindex.Forget` removes the row; nothing re-adds it
(`archivePointers` reads `Out`, `last-archive.txt`, `BackupFor`, each now clear of
it).

### 5.3 Delete, in order [C2, C1, C18]

Each step runs only after the one before succeeded; a named file already gone
counts as removed. Every step has a result line.

0. **Log "started"**: append and fsync a `started` line to `deletions.log` (date,
   folder, plan counts, §4.3's choice) before anything is removed.
1. **Take the archive lock** (`lockfile`, the same lock a backup takes) and
   re-check the plan (§3.1). Encrypted: if unlocked in this page, the delete locks
   it itself and holds `Keys.hush` for the whole operation, as Forget does [C13].
   Busy (§7) refuses here.
2. **Schedules**: remove every discovered entry (`RemoveLiveEntry`), C4(c)'s two
   for the current archive. A partial failure stops here, reports the entries left,
   and changes nothing else [C18].
3. **Per-name config files**, for each discovered name: `<name>.json`
   (`schedule.RemoveJobFile`), `<name>.jobfail.log`, `<name>.older.jobfail.log`,
   `<name>.alert-streak`, `<name>.sync-alert`; `last-failure.json` only when its
   `Name` is one of them.
4. **Settings and pointers**: `Out` cleared if this is the current archive (the page
   then shows "No archive chosen yet"); `BackupFor` cleared if it names the folder;
   `last-archive.txt` cleared if it names the folder.
5. **Scratch** (encrypted) [C7]: a single-archive variant of `ScratchTargets`
   touching only `scratch/<id>/`, never the root or another id. When this archive's
   reader lock is held, or anything unexpected is there, the delete is refused
   before step 6 with "Close MailArchive serve or search for this archive first".
6. **The folder's contents** (§4), the manifest or vault last, `.mailarchive.lock`
   kept (it is held).
7. **Unlock, then the lock file, then the folder** [C1]: release the lock (the file
   stays, `lockfile.go:111-114`); remove `.mailarchive.lock` by its exact name,
   retrying briefly (Windows cannot delete a held file); remove the folder if empty
   but for litter. A `.mailarchive.lock` nobody holds is never "something of the
   archive left"; if it cannot be removed the result says "Deleted; one MailArchive
   lock file could not be removed" and the delete is complete. The short gap after
   unlocking is accepted: step 2 removed this computer's schedules, and §7's
   emptied-folder guard [C12] refuses an unattended run into what is left.
8. **Keyring** (encrypted) [C8]: only after step 7 removed the folder. A fresh
   `PlanForget` (case Gone, no lock taken: the folder is gone). `Whole` and `Keys`
   only when `plan.Last`; otherwise forget only this path, keep the key, and the
   result names the other recorded copies. If the folder could not be removed, this
   step is skipped and the result says so. An encrypted folder this computer never
   recorded: files deleted, no record touched.
9. **The row**: `archindex.Forget`.
10. **Log "finished"**: `finished complete` or `finished partial` with what was
    left; written after every attempt, including a refusal after step 0.

The delete handler recovers its own panics and reports the steps done, never
"nothing was changed" after step 0 [C18].

**A delete not finished** [C2]: a `started` line with no `finished` line for a
folder (a crash, a power cut) shows its row as "Delete not finished: click Delete
again" in place of Switch, Run now and Archive now. A re-run is allowed even when
the folder is gone or no longer an archive; it then runs the steps that remain
(config side, keyring, row) and writes `finished`. While the manifest/vault exists
a re-run is an ordinary Delete (Out was cleared in step 4, so it is no longer
current).

`deletions.log` is in the config folder, owner-only (0600, `util.WriteSecure`
rules), append-only, one line per event; uninstall removes it with the other config
files. Growth: two short lines per delete.

## 6. Encrypted archives

No password is asked: a person who has lost it must still be able to delete
(retention), and the typed name is the decision. The plan says: "This computer
will forget this archive's key. Any other copy of this archive will need its
password or recovery key to open." [C8]

## 7. Refusals (each names the cause and a remedy that can be done from the row; nothing changes) [C15]

| Case | Delete | Remove from list |
|---|---|---|
| a backup, Run now, verify, pre-sync session, Desktop capture (`d.cap.running`) or workspace job on this archive is running [C12] | "A backup of this archive is running. Try again when it ends." | allowed |
| MailArchive serve or search has this encrypted archive open (reader lock) [C7] | "Close MailArchive serve or search for this archive first." | allowed |
| the folder is not reachable | "Reconnect the drive or folder first: nothing can be checked." | allowed; when it is the current archive, Out is cleared [C3] |
| the folder is no longer an archive (no manifest, no encrypted marker), no unfinished delete | "This folder no longer holds an archive. Use Remove from list." | allowed; Out cleared when current [C3] |
| paused (read-only) | allowed: the seal goes with the folder; the plan says "This archive is paused." [C13] | allowed |
| current archive | allowed (§5.3 step 4) | refused while reachable and an archive: "Switch to another archive first." |
| `BackupFor` names it, it is not current | refused, §5.1 (a) [C4] | refused, §5.1 (b) [C4] |
| a descriptor unreadable, or naming an entry `bound.go` refuses | refused, the by-name remedy [C6] | allowed (only §5.2's own discovery is acted on) |
| the Desktop was started with `-out` naming this folder [C16] | "Restart MailArchive without -out, then delete." | same |
| protected: decided by file identity (`os.SameFile` up the ancestor chains), not path text — the folder is, contains or is inside the config folder, the scratch folder, the encrypted record folder, or another known archive; or it is or contains the home folder, a drive root, or a system folder (Windows: `%SystemRoot%`, `%ProgramFiles%`, `%ProgramFiles(x86)%`, `%ProgramData%`; macOS: `/System`, `/Library`, `/Applications`, `/usr`; Linux: `/usr`, `/etc`, `/var`, `/bin`, `/opt`) | "MailArchive will not delete this folder: it holds NAME. Delete the archive's files by hand." | allowed |
| a link, junction or reparse point at any level of the folder or inside it [C11] | "This archive's folder is reached through a link. Delete it by hand." | allowed |
| mixed state (`encmark.classify` Other; another archive's marker in a store tree) | "This folder holds files of more than one kind. Delete it by hand." | allowed |

**The emptied-folder guard [C12]** (a change to the shipped engine, its own MA row,
in the adversarial pass's scope): an unattended run (scheduled backup, capture,
run-job) refuses an existing `-out` that holds no manifest, no encrypted marker and
no backup descriptor, and holds anything beyond litter. A first run after an
install, and "Save and keep" into a new empty folder, still work (empty, or holding
the descriptor). A missing `-out` is already refused (MA-689). So a deleted
archive's folder, kept because it holds the person's files, is never refilled by a
schedule this computer missed or another computer runs.

## 8. The result says exactly what happened [C5, C18]

Three kinds of leftover:
- (a) A name the archive recorded or owns that could not be removed. It keeps the
  manifest/vault (so Delete can be run again) and the result is "Not fully deleted.
  N files could not be removed:" with up to 10 names and a remedy by error type:
  a sharing violation, "Close Outlook or any program using them, then click Delete
  again"; access denied, "Windows security or antivirus blocked it: allow
  MailArchive or delete these by hand"; a device gone, "Reconnect the drive, then
  click Delete again". A briefly busy empty folder is retried before it is reported.
- (b) Litter: removed with its folder (§4.4).
- (c) Anything else (the person's files, §4.3's files when unticked): kept, listed as
  "kept: not the archive's", never blocking completion.

Complete means no name the archive recorded remains. Result lines: "Deleted. N
files (SIZE) removed. The folder is gone." / "…The folder is kept: it holds N
files that are not the archive's." plus one line per step that changed something
outside the folder ("Stopped the scheduled backup NAME", "This computer forgot the
archive's key", "Removed from Your archives"), §2's "What Delete does not reach",
and "Recorded in deletions.log". After a partial: "Files deleted; this computer
still remembers its key: use Forget this encrypted archive here" where step 8 was
skipped.

### 8.1 The plan and the deletion record, saved by the person [O2]

- **Before the decision**: the panel's **Save this plan** (and `delete-archive
  -plan-out FILE`, which deletes nothing) saves the plan as a plain-text file, for a
  person who must get approval first. It is the record below with "Status: planned,
  nothing deleted".
- **After it**: the result's **Save deletion record** (and `delete-archive` writing
  `-record-out FILE` when given) saves the record of what happened. The Desktop
  builds the text server-side from the finished operation and the page saves it
  through the browser (a Blob download named "MailArchive deletion record - NAME -
  DATE.txt"); nothing is sent anywhere. Saving is offered, never required; the
  record can be saved until the result is dismissed.
- **Contents** (UTF-8 text, one "Key: value" per line, a person or a script can read
  it): status (planned / complete / not fully deleted / refused after start); the
  date and time with its time zone; the computer name and the account; MailArchive's
  version; the folder as shown; plain or encrypted (and the archive id for an
  encrypted one); message count and the date range of the mail; when the archive was
  made and last backed up (from its run history, read before deletion); files and
  bytes removed; the SHA-256 of the manifest (or vault file) as read at the plan,
  identifying the archive without keeping any mail; the schedules stopped; whether
  this computer's record and key were forgotten; §4.3's choice and count; each
  leftover kind with counts and up to 10 names; "What Delete does not reach"; and
  the SHA-256 of the record text itself, also written to `deletions.log`'s
  `finished` line so a saved record can be matched to this computer's journal.
- **Never in it**: subjects, senders, recipients, addresses or any mail content.
  The record must not keep what the deletion removed.
- The record is not signed: it is evidence of what this program did, not proof
  against a person who edits it; the digest in `deletions.log` is the cross-check
  while that file exists. Uninstall's plan names `deletions.log` among what it
  removes, and suggests saving any records first.

## 9. Windows first, then macOS and Linux

- **Read-only files** [C11]: Windows refuses to delete a read-only file. The
  read-only attribute is cleared only on Windows, only through a handle opened
  without following links, and only when the file has a single link (no hard link
  elsewhere); a file the archive did not record is never touched; macOS and Linux
  never get a chmod.
- **Open files**: Outlook, antivirus or the search indexer may hold a file; §8 (a).
  `search.db` is closed first (this page's reader for the archive).
- **Long paths**: over 260 characters, handled by Go's `os` on Windows; tested with
  a deep tree in the lab.
- **Synced folders** [C14]: a folder under `%OneDrive%`, `%OneDriveCommercial%`,
  `%OneDriveConsumer%`, a synced SharePoint library (under
  `%UserProfile%\<organisation name>`, detected through the OneDrive registry
  `SyncEngines\Providers` mount points, or named as not detected), or macOS
  `~/Library/Mobile Documents` / `~/Library/CloudStorage`: the panel says "This
  folder is synced with OneDrive. Deleting it here also deletes it there, and
  OneDrive keeps it in its recycle bin: empty that too." The claims about
  cloud-only placeholders, timing and paused sync are **conditional** until a
  Windows lab row proves them (pending: lab).
- **Names**: the typed name is compared after NFC normalisation, exactly as shown,
  no case folding [C19].

## 10. Surfaces and words

- Row buttons (every row, beside Switch / Pause): **Remove from list** (secondary)
  and **Delete…** (danger style). A row that cannot be deleted shows Delete
  disabled with its title saying why (§7's words).
- The guide (How to use MailArchive) gains "Deleting an archive" and "Removing an
  archive from the list", Windows first, with "What Delete does not reach".
- `status` and the Overview never mention deleted archives.

## 11. Tests (each prove-fail → prove-pass; Windows run first in the lab)

New MA rows from MA-956. Real files in temp folders (no mocks of the removal); each
refusal with `assure.Refused` (cause, remedy, side effect absent); each positive
twin first.
- Plain beside the person's own files: every archive file gone, every other file
  byte-identical, folder kept; a same-named store folder's person file kept;
  `.DS_Store` / `desktop.ini` in a store subfolder, ends "Deleted" [C5].
- §4.3: unrecorded mail copies kept by default and listed; ticked, removed; never
  any other kind of file; the choice in `deletions.log` [O1].
- Encrypted: folder gone, record and key forgotten (`plan.Last`); another recorded
  copy keeps the key [C8]; scratch only `scratch/<id>/` [C7].
- Order: the manifest/vault removed last (a fault before it leaves a recognised
  archive, re-run finishes); schedules first (a recording fake scheduler);
  `started` before the first removal; a crash after `started` shows "Delete not
  finished" and the re-run finishes when the folder is gone [C2].
- The lock: deleted under the lock, then the lock file and folder gone (Windows lab)
  [C1].
- Discovery: a custom `-name`, two `-name` installs, another spelling, a legacy
  verify, a denied or hung scheduler; an unreadable descriptor refuses [C6].
- BackupFor (a), (b), (c) [C4]; `-out` launch [C16].
- Manifest over 1 MiB [C10] (and a separate MA row: today's list greys such
  archives, `index.go:50, 283`).
- Shape rule and links: a planted record naming a person's file, a planted store
  token, an in-root link mid-path, a Windows junction (lab) [C11].
- Protected folders by identity, including another spelling of the config folder
  [C15].
- Every §7 refusal, nothing changed; tokens one-use, expiring, kind-filtered; a
  change after the plan refuses.
- Routes: no token, wrong token, foreign Origin or Host, GET → 405, side effect
  absent [C20].
- Remove from list: the row gone and not re-added; folder byte-identical; schedules
  named and stopped only after Yes; unplugged drive; current + unreachable clears
  Out [C3].
- Emptied-folder guard: an unattended run refuses a non-empty folder with no
  archive; a first run into an empty folder and into one holding only the
  descriptor still works [C12].
- Windows: read-only archive file deleted; read-only person's file kept; a file
  held open reported, the rest deleted, re-run finishes; a path over 260
  characters; a hard-linked file not chmod-ed [C11].
- CLI: refuses without a terminal and while a Desktop runs; exit codes 0/3/1
  [C17]; uninstall removes `deletions.log` and its plan names it.
- Plan and deletion record [O2]: the saved plan deletes nothing; the record's fields
  for a complete, a partial and a plan; no subject, sender or address from a planted
  archive appears in either; its digest matches `deletions.log`'s `finished` line;
  the manifest digest is the one read at the plan.
- The page: panel beside the row, button disabled until the NFC name matches, no
  `confirm(`/`prompt(` in either handler, one panel at a time [C19].

## 12. Questions answered by the review

Q1 → §4.1, §4.3 [C9, O1]; Q2 → no password (§6); Q3 → the delete locks it itself
[C13]; Q4 → §5.1 (c), §7 [C4, C16].
