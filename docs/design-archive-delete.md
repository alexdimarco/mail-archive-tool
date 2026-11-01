# Design: delete an archive, and remove one from the list

Status: PROPOSED (2026-11-01). Owes the 10-lens pre-code design review
(`docs/review-archive-delete-predesign.md`) before any code, then the adversarial
pass and the friction review before release. Target: 1.5.3 (owner, 2026-11-01:
"bundle into 1.5.3").

## 1. Why

The owner asked what the interface offers "to completely delete and purge an
archive". Nothing: no action deletes an archive, and no action takes a row off
Your archives (`archindex.Forget` exists and nothing calls it). A folder deleted by
hand stays on the list forever, greyed.

Owner's rulings, 2026-11-01, quoted:
- "the never delete rule should be adjusted to never delete without your explicit
  decision to do so" (recorded as R36 in `docs/scenario-catalog.md` §2).
- "Cleanup is an essential part of any archive process and even necessary from a
  policy and security standpoint (retention rules etc). It is already focused on
  the side of caution to move it to a manual process."
- "windows is the highest potential group of users": Windows behaviour is designed
  and tested first.

## 2. What is built

Two row actions on the Your archives card, each with a command-line verb (R33):

| Action | What it changes | What it never changes |
|---|---|---|
| **Remove from this list** (`mailarchive unlist-archive -out DIR`) | the row in `known-archives.json`, and the pointers that would add it back (§5) | the folder and everything in it, schedules, keys |
| **Delete this archive…** (`mailarchive delete-archive -out DIR`) | the archive's own files (§4), its schedules, its per-archive files in the config folder, its row, and for an encrypted archive this computer's record and remembered key (§6) | any file the archive did not write; any other archive; any shared setting |

Delete is permanent: no Trash or Recycle Bin. Retention rules require the mail to
be gone; a Trash copy defeats them. The caution is in the confirmation (§3), the
refusals (§7) and the honest result (§8).

## 3. The confirmation (the person's explicit decision, R36)

Delete is two requests, as Forget is (`encryption_forget.go`, `pending.go`):

1. **Plan** (`POST /api/archive/delete/plan {path}`): read-only. Checks §7's
   refusals, counts what would be deleted (files, bytes, schedules, config files),
   notes whether the folder is inside a synced folder (§9), and holds the plan
   server-side for 10 minutes under a one-use token (`pendingItem.deletePlan`,
   kind-filtered `take`). Changes nothing.
2. **Delete** (`POST /api/archive/delete {token, typed}`): `typed` must equal the
   archive folder's own name (its last path element, exactly as the row shows it).
   The plan is re-checked under the archive's lock before anything is removed; any
   change since the plan (new files, a different manifest, the archive now current,
   paused, or busy) refuses with "click Delete again".

The page shows the plan beside the row (`noteBeside`, MA-955), not in a browser
`confirm()`/`prompt()` dialog (a browser may block those, the owner's Forget report
of 2026-10-31): a panel with what will be deleted, the sync warning when it
applies, a text box "Type the folder's name, NAME, to delete it", **Delete
permanently** (disabled until the name matches) and **Cancel**.

The command line asks the same, at a terminal only (`keyVerbTerminal`): no `-yes`
flag, no stdin pipe, no job file or schedule can run it. Its plan prints first; the
person types the folder name.

Remove from this list is one request with an in-row "Remove from list? Yes / No",
since it deletes nothing.

## 4. What Delete removes inside the folder

Plain archives can share a folder with the person's own files (no empty-folder check
on the plain create path, `app/run.go:596`, `wizard/wizard.go:563`) and mirror the
source's folder names, so the folder is never removed wholesale. Removal is by
**name the archive recorded**, files first, then each folder with `os.Remove` (never
`RemoveAll`), deepest first, so a folder holding anything else stops there and stays.

**Plain archive** — from the manifest, read through the confined reader
(`archindex.inspect`'s path):
- each record's `Path` and its siblings (`.html`, `-attachments.zip`, `.eml`), the
  per-folder `index.html` / `index-N.html` pages under each store token's tree, and
  each store token's folders;
- the files named by `.mailarchive-history.jsonl` (superseded copies kept for
  point-in-time views, R21);
- the fixed root names: `index.html`, `search.db` (+ `-wal`, `-shm`, `-journal`),
  `README.txt`, `attachments-report.tsv`, `attachments-report-legacy.tsv`,
  `BACKUP-NEEDS-ATTENTION.txt`, `ARCHIVE-INTEGRITY-ATTENTION.txt`,
  `mailarchive.log`, the capture log `<name>.log`, `_outlook-pst/` (its `.pst`
  files only), and every `.mailarchive-*` state file and temp (`state`, `seal`,
  `schedule` descriptors, `lockfile`);
- **the manifest last** (after everything else), so a delete stopped part way is
  still recognised as an archive and can be run again to finish.

**Encrypted archive** — the fixed root names (`encmark.reserved`, `tempRe`), with
`units/`, `chunks/`, `inventory/` only in their validated layouts
(`vault/marker.go`), `program/` only in the break-glass copy layout; **the vault
files last**. The archive's scratch folder (`<UserCacheDir>/mailarchive/scratch/<id>/`)
by `uninstall.ScratchTargets`'s rules.

Every removal goes through an `os.Root` opened on the archive folder (no path
leaves it, a link inside it is removed as a link and never followed). The folder
itself is removed last, only when empty but for system litter (`util.IsLitter`:
`.DS_Store`, `desktop.ini`, `Thumbs.db`), which is then removed with it.

## 5. What both actions change outside the folder

Remove from this list: `archindex.Forget`, then clear what would re-add it on the
next page load (`archivePointers`): `last-archive.txt` when it names this folder.
`Out` cannot name it (refused, §7). `BackupFor` naming it refuses (§7).

Delete, in this order (each step only after the one before succeeded):
1. **Schedules first**, so nothing re-creates the archive: `removeTaskAt(path)`
   (the run-job or capture entry, its job file, the alert streak, `BackupFor`), the
   derived `-verify` entry, and any custom-named entry `bound.go` binds to this
   folder. The schedule descriptors go with the folder in step 2.
2. **The folder's contents** (§4).
3. **Config-folder files keyed to this archive** (`DefaultNameFor(path)`): `<name>.json`
   via `schedule.RemoveJobFile`, `<name>.jobfail.log`, `<name>.older.jobfail.log`,
   `<name>.alert-streak`, `<name>.sync-alert`; `last-failure.json` only when its
   `Name` is this archive's; `last-archive.txt` when it names this folder.
4. **Settings**: if this was the current archive, `Out` is cleared (the page shows
   "No archive chosen yet"); `BackupFor` cleared if it names this folder.
5. **The row**: `archindex.Forget`.
6. **Encrypted only**: `keyring.Forget(plan, Whole, Keys)` — this computer's record
   and the remembered key — **only when step 2 left nothing of the archive**; if
   anything sealed remains, the record is kept so the folder stays protected, and the
   result says so.
7. **A line in `deletions.log`** (config folder, owner-only, append): date, folder,
   files and bytes removed, what was left. Kept for retention evidence; uninstall
   removes it with the other config files.

## 6. Encrypted archives

No password is asked: a person who has lost it must still be able to delete
(retention), and the typed name is the decision. The archive must be locked
(not open in the page) when deleted: the plan refuses while it is unlocked, "Lock
it first", so no key is in memory for a folder being removed.

## 7. Refusals (each names the cause and the remedy; nothing changes)

| Case | Delete | Remove from list |
|---|---|---|
| a backup, Run now or verify of this archive is running (its lock is held) | refused: "A backup of this archive is running. Try again when it ends." | allowed |
| the folder is not reachable (drive unplugged, cloud offline) | refused: "Reconnect the drive or folder first: nothing can be checked." | allowed (the main use) |
| the folder is no longer an archive (no manifest, no encrypted marker) | refused, offering Remove from list | allowed |
| paused (read-only) | refused: "Resume it first: a paused archive is protected from every change." | allowed |
| current archive | allowed (Out cleared, §5.4) | refused: "Switch to another archive first." |
| `BackupFor` names it (a capture still runs under its name) | allowed (removed in step 1) | refused: "Its scheduled backup still uses this folder. Delete it, or remove the backup first." |
| encrypted and unlocked in this page | refused: "Lock it first." | allowed |
| the folder is, contains, or is inside the config folder, the scratch folder, another known archive, the home folder, a drive root, or a system folder | refused | allowed |
| the folder is a link, or reached through one that resolves elsewhere | refused (the real folder must be named) | allowed |
| a plain manifest beside encrypted files, or any other mixed state `encmark.classify` calls Other | refused: "This folder holds files of more than one kind. Delete it by hand." | allowed |

## 8. The result says exactly what happened

- Everything removed: "Deleted. N files (SIZE) removed. The folder is gone." (or
  "The folder is kept: it holds your other files.")
- Something left (a file open in another program, access denied, a file the archive
  did not write in its folders): "Not fully deleted. N files could not be removed:"
  with up to 10 names and the remedy ("Close Outlook or any program using them and
  click Delete again"). The manifest/vault stays (§4), so the row stays and Delete
  can be run again. Never "Deleted" while anything of the archive remains.

## 9. Windows first, then macOS and Linux

- **Read-only files**: Windows refuses to delete a file with the read-only
  attribute. A file the archive wrote is made writable (`os.Chmod 0o600`) before its
  removal, inside the root only; a read-only file the archive did not write is never
  touched.
- **Open files**: Outlook, antivirus or the search indexer may hold a file. The
  removal of that file fails; the rest continues; §8's partial result applies.
  `search.db` is closed by the page first (the reader server's handle for this
  archive, if any).
- **Long paths**: paths over 260 characters are handled by Go's `os` package on
  Windows (`\\?\` prefixing); tested with a deep tree.
- **OneDrive / iCloud Drive / Dropbox**: deleting inside a synced folder deletes the
  cloud copy too within minutes, and files that are only in the cloud
  (placeholders) are deleted without being downloaded. The plan detects a folder
  under `%OneDrive%`, `%OneDriveCommercial%`, `%OneDriveConsumer%` or
  `~/Library/Mobile Documents` / `~/Library/CloudStorage` and the panel says: "This
  folder is in OneDrive: deleting it here also deletes it from OneDrive." Dropbox and
  others are not detected (named in the guide).
- **Case-insensitive names**: the typed name is compared exactly as the row shows it
  (no case folding), so the person types what they see.

## 10. Surfaces and words

- Row buttons (all rows, beside Switch/Pause): **Remove from list** (secondary) and
  **Delete…** (danger style). Unreachable rows get Remove from list only, with
  Delete shown disabled and its title saying why.
- The guide (How to use MailArchive) gains "Deleting an archive" and "Removing an
  archive from the list", Windows first.
- `status` and the Overview never mention deleted archives.

## 11. Tests (each prove-fail → prove-pass; Windows run first in the lab)

New MA rows from MA-956. Proven with real files in temp folders (no mocks of the
removal), each refusal with `assure.Refused` (cause, remedy, side effect absent),
each positive twin first:
- a plain archive beside the person's own files: every archive file gone, every
  other file byte-identical, the folder kept; a same-named store folder holding a
  person's file keeps that file;
- an encrypted archive: the folder gone, record and key forgotten; a partial delete
  (a file held open) keeps the vault and the record, and a second Delete finishes;
- the manifest/vault removed last (a fault injected before it leaves a recognised
  archive);
- schedules removed first (a fake scheduler records the order);
- every refusal in §7, nothing changed;
- the token is one-use, expires, is kind-filtered, and a change after the plan
  refuses;
- links: a link inside the archive is removed as a link, its target untouched; a
  linked archive folder is refused;
- Windows: a read-only archive file is deleted; a read-only foreign file is kept; a
  file held open is reported, the rest deleted; a path over 260 characters;
- Remove from this list: the row gone and not re-added on the next page load; the
  folder byte-identical;
- the CLI verbs refuse without a terminal; uninstall removes `deletions.log`.
- the page: the panel beside the row, the button disabled until the name matches,
  no `confirm(`/`prompt(` in either handler.

## 12. Open questions for the review

1. Is the manifest + history the complete list of files a plain archive wrote
   under store folders (orphans `verify` flags, files from older format versions)?
   If not, the leftovers are reported (§8), which an owner on a retention schedule
   may find not enough.
2. Should Delete of an encrypted archive require the password after all?
3. Is "Lock it first" needed, or can the delete lock it itself?
4. Does deleting the current archive (Out cleared) leave any card or schedule
   reading the old path?
