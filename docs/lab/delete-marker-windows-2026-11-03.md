# Lab: the delete marker on Windows (MA-1055, Windows subset), 3 November 2026

Run 3 November 2026, 17:17–17:40 on the VM clock (-05:00), on the Parallels VM "Windows 11"
(ARM64, the amd64 program runs emulated), by the lab agent of the lead's workflow. Owner approval:
"lab subtest and release" (3 November 2026). Catalog row: MA-1055. Design: `docs/design-archive-delete.md`
§15.5, §15.7, §16.

**Result: every item asked for ran on the VM. Nothing found is HIGH or MED in the delete feature.**
Items (a) to (e) passed. Four LOW findings are recorded below for the lead. One of them, LOW-D, is
older than this feature: it is the same in the v1.5.5 release build. No product file was changed.

## Set-up

| | |
|---|---|
| Programs | `mailarchive.exe` and `mailarchivew.exe` built from 4106f8b with the two Windows lines in `packaging/build-release.sh` (`GOOS=windows GOARCH=amd64 go build`, the second with `-ldflags -H=windowsgui`), go1.26.0. They report `v1.5.6-0.20261103191721-4106f8b4ed9a+dirty`. The dirty mark comes from untracked helper files |
| Two pretend computers | A and B. One Windows account; each has its own `APPDATA` and `LOCALAPPDATA` under `%LOCALAPPDATA%\ml55\A` or `\B`. A's Desktop and verbs were given `-config %LOCALAPPDATA%\ml55\A\home\Roaming\mailarchive\graph-config.json`. That is also the folder a command without `-config` reads under A's `APPDATA`, so `status` (which takes no `-config`) read A's own `deletions.log`. B only ran `status`, to compare. The VM user's real MailArchive settings and data were not touched |
| A real drive | a 64 MB VHDX (`diskpart create vdisk`, NTFS, label ML55LAB) at `C:\ml55vhd\lab1.vhdx`. It was attached and detached as SYSTEM with `Mount-DiskImage` / `Dismount-DiskImage`; the letter was set with `Set-Partition` |
| A delete stopped part-way | a hidden PowerShell script held one mail file open (others may read it, no one may delete it) while the delete ran, as in the friction review |
| Input | Edge with the keyboard only (`prlctl send-key-event`); a Windows Terminal window for `delete-archive` (it needs a real console). Other verbs ran through `prlctl exec --current-user` |
| Evidence | 46 VM screenshots (`prlctl capture`) in `.claude/ml55-shots/` on the Mac (untracked, not committed, like the friction review's). Each step's terminal output was copied into this record. A's `deletions.log` was read after each step |
| Clean-up | the VHDX was detached and deleted, along with `C:\ml55vhd`. The test folders `C:\Users\Public\ml55` and `%LOCALAPPDATA%\ml55` were deleted. The one scheduled task was removed with `schedule -remove`, and `schtasks` then listed no `mailarchive-*` task. The three Desktop processes were stopped by PID (928, 12312, 6616). The lab terminal was closed. R: and S: are gone. The friction review's earlier folders (`delwalk`) were left as found |

## (a) Detach and reattach a drive at the same letter: passed

1. A made `R:\Lab-Archive` (24 messages) and listed it. At A's terminal, `delete-archive -config … -out R:\Lab-Archive` ran with one mail file held open. Result: `not fully deleted`, 29 files removed, 1 left (`02`, `03`). On disk: the marker `.mailarchive-deleting.json` (run `3dbccbab84d2ccf5`), the manifest, the lock and the held file. A's log: `started` (identity volume 2324449907, file id 281474976710695), `marked`, then `finished partial unfinished`.
2. The hold ended, and **the virtual disk was detached**. R: was gone.
   - `status -out R:\Lab-Archive` gave: "no archive at R:\Lab-Archive: a delete of this archive did not finish — run mailarchive delete-archive -out "R:\Lab-Archive" again, or click Delete… on its row in MailArchive Desktop, to finish it.", exit 1.
   - A backup, and a scheduled-style backup (`-unattended`), were both refused: "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed." They named the terminal's Delete again.
   - `uninstall -dry-run` showed its plan, then said it will refuse until the delete of R:\Lab-Archive is finished.
   - A's row (`05`): "Delete not finished: click Delete again". Remove from list was greyed and Delete… was offered.
   - Delete… on that row (`06`): "**Reconnect the drive or folder first: nothing can be checked.**" No "It was finished elsewhere" question came up, so nothing was changed or forgotten without the person's answer. The drive letter read as absent, not as "could not check" (DM-22, on a virtual disk).
3. **The disk was reattached; it came back as R: by itself.** The marker was still there, unchanged. `status` gave RED: "a delete of this archive did not finish — run … again…". A backup and `pause -config` were refused; pause's message carried the `-config` in Delete again.
4. Delete… in the Desktop (`07`): "**This finishes the delete begun 2026-11-03 17:22 -05:00.**", 2 files. This computer's log found the delete; there was no "another computer" line. After the typed name (`08`): "Deleted. 2 files (13.8 KB) removed. The folder is gone. This finishes the delete begun 2026-11-03 17:22 -05:00: 31 files (141.6 KB) removed in all. Removed from Your archives." The log gained `started` (with `first_run` 3dbccbab…, `earlier_files` 29), `marked`, `folder removed` and `finished complete`. The folder is gone from R:.

## (b) Drive letter change: passed (LOW-B and LOW-C recorded)

1. A made and listed `R:\Move-Archive`. A Desktop delete, with one file held, stopped part-way (`20`): run `39562ab83b2e3145`, 29 removed, 1 left, marker written. The row said "Delete not finished".
2. **The disk was detached and reattached at S:** (R: absent).
   - `status -out S:\Move-Archive` gave RED: "a delete of this archive did not finish — run mailarchive delete-archive -out "**R:**\Move-Archive" again, or click Delete… on its row…". The identity route matched (the volume serial and file id were unchanged). The words were this computer's log's own, and **not "another computer"**. The hint names the old letter (LOW-C).
   - `status -out R:\Move-Archive` said "no archive at R:\Move-Archive: a delete of this archive did not finish…", exit 1.
   - A backup into S: was refused in the log's words, with "At this terminal, Delete again is: mailarchive delete-archive -out "S:\Move-Archive"". `pause -config` into S: was refused in the same way, carrying `-config`.
   - `uninstall -dry-run` gave its plan, then said it will refuse until the delete of R:\Move-Archive is finished.
   - For comparison, B's `status -out S:\Move-Archive` (B's log never saw the delete) said "…begun 2026-11-03 17:28 -05:00 did not finish, and this computer's deletion log has no record of it (it was begun on another computer or user account)…". That is the right sentence for B.
3. `add-archive -config … S:\Move-Archive` said: "Added to Your archives: S:\Move-Archive. A delete of this archive did not finish: to finish it, click Delete… on its row…". Both rows (`24`), R: and S:, said "Delete not finished: click Delete again"; neither said "another computer".
4. With the Desktop stopped, at A's terminal, `delete-archive -config … -out S:\Move-Archive` (`26`, `27`) began "**This finishes the delete begun 2026-11-03 17:28 -05:00.**" and ended `Status: complete … Files removed earlier: 29 … Files removed in all: 31`. The log gained `started` (`first_run` 39562ab8…), `marked`, `folder removed` and `finished complete`.
5. **The old row's delete is ended, and uninstall is no longer blocked.** `uninstall -dry-run` gave its plan with exit 0 and no refusal. The old row no longer said "Delete not finished". In the Desktop (`28`) it said "Not reachable right now — reconnect the drive or folder and it comes back. Nothing was removed." (LOW-B). Remove from list took it off (`29`, `30`).
6. The words "begun on this computer" (MA-1050 (d)) did not come up. They are not reachable by a letter change on Windows: the identity route matches first, so the surfaces used this computer's log's own words. They never said "another computer" on A.

## (c) The terminal `-config` walk on Windows: passed for the delete feature (LOW-D recorded)

On `C:\Users\Public\ml55\Note-Archive`, a delete was stopped part-way at the terminal (`32`). Before
that, while A's Desktop ran, `delete-archive -config …` was refused (`31`): "MailArchive Desktop is
running for your account … so nothing was changed. … click Shut down at the top of MailArchive
Desktop, then run this again."

| Command (as A) | Half-deleted Note-Archive | Healthy-Archive |
|---|---|---|
| `schedule -config A -install` | refused: "Delete not finished: click Delete again. This archive's delete did not finish, so its schedule was not installed; nothing was changed." plus `delete-archive -config "…A…" -out "…Note-Archive"` | installed `mailarchive-53924596` (daily 02:00); `schtasks` listed it |
| `pause -config A` | refused in the same words ("…so it was not paused…"), with the `-config` Delete again | paused; the line named `resume -config "…A…graph-config.json" -out …` to undo |
| `resume -config A` | refused ("…so it was not resumed…"), `-config` Delete again | the archive was made writable, but its schedule was **not restored** (LOW-D) |
| `pause -config "C:\Users\Public\ml55\A config dir\graph-config.json"` (a path with spaces; that log never saw the delete) | refused by the marker alone: "…begun on another computer or user account, or this computer's deletion log has no record of it…", with "To finish it at this terminal: mailarchive delete-archive -config "C:\Users\Public\ml55\A config dir\graph-config.json" …" | paused; the undo line quoted the spaced path; `resume` with it unsealed (same LOW-D on the schedule) |
| `status` | RED "a delete of this archive did not finish — run … again…" | WARN after the failed resume: the schedule is recorded but not present |
| a backup | refused, nothing changed | not run |

**Not run: `-config` on a network path.** The Parallels shared folder P: cannot be written from the
VM (`mkdir` failed there), so `pause` refused with "There are no MailArchive Desktop settings beside
"P:\ml55\netcfg\graph-config.json": its folder "P:\ml55\netcfg" does not exist, so nothing was
changed." That refusal is correct for a folder that does not exist. The case of a real network
`-config` stays pending.

## (d) Friction fix B in Edge (keyboard only): passed, with LOW-A

- **After Remove from list** (no current archive; `14b` to `17`): the page was drawn again at Your archives with **the heading "Your archives" focused** (outlined), and the result note was below it. One Tab went to the next row's first button.
- **After Dismiss, with a current archive** (Healthy-Archive current; `21b` to `23`): Dismiss → "Close without saving the deletion record?" → Shift+Tab → Close without saving. The page was drawn again at Your archives with **its heading focused**.
- **After Remove from list on the old R: row** (`29`, `30`): the page landed at Your archives with the heading focused.
- **After Dismiss with no archive chosen** (`12`, `13`, `13b`): the page was drawn again **at its top** ("Overview", "No archive chosen yet"), not at Your archives (LOW-A).

## (e) The new LOW-4 words and the "Keep it" default: passed

- **Keep it** (`10`, `22`): Dismiss on an unsaved result asked "Close without saving the deletion record?" with **the cursor on Keep it** (outlined). Enter on Keep it moved the cursor to **Save deletion record** (`11`). The record was not saved during the lab, so nothing was written to the VM user's folders.
- **LOW-4's words (MA-1068)**: Note-Archive's note was removed by hand after its part-way delete (a copy was kept outside the folder). The log still had the delete and its `marked` line, and the anchor was still in the folder (DM-27).
  - `status`: "RED: a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left — if this is a different drive, plug in the drive the delete began on and finish the delete there; if it is the same drive and the note was removed, run mailarchive delete-archive -out "…Note-Archive", or click Delete… on its row in MailArchive Desktop, to finish it."
  - A backup, `pause -config` and `schedule -config -install` were refused: "Delete not finished: a delete this computer began in this folder did not finish, but the folder does not hold the note that delete left, so it was not backed up / paused / its schedule was not installed; nothing was changed. If this is a different drive, …; if it is the same drive and the note was removed, click Delete… on its row in MailArchive Desktop to finish it." `pause` and `schedule` added the `-config` terminal hint; the backup added the hint without it. Nothing on disk changed.
  - The row (`33`) said the same, with "click Delete… to finish it." Following that remedy, Delete… planned 2 files under a warning ending "…may be another drive's archive with the same name. Delete it only if you mean to delete this archive." (`35`). It finished: "Deleted. 2 files (13.8 KB) removed. The folder is gone." (`36`). It ran as a new delete (its own `first_run`; no "This finishes…" line), as DM-27's plan rule intends. Afterwards `uninstall -dry-run` gave its plan with no refusal.

## Findings (all LOW; none blocks 1.5.6)

**LOW-A. Dismiss with no archive chosen lands at the top of the page, not at Your archives.**
`delDismiss` (`internal/desktop/deletescript.go`, the `delTopKey` branch) sends the page to its top
when `$('archiveGone')||$('noArchive')`. `noArchive` is drawn whenever no archive is chosen, not
only when the page's own archive was just deleted (N12's case). So a person with no current archive
who deletes a row's archive, then dismisses, starts again from the top. The top card does point
down to Your archives. Fix B holds whenever an archive is chosen.

**LOW-B. After a delete finished at a new letter, the old row is no longer "Delete not finished".
It now says "Not reachable right now — reconnect the drive or folder and it comes back. Nothing was
removed."** Its Remove from list asks "The folder and its files are kept." But the archive was
deleted at S:. Nothing is harmed, and Remove from list clears the row. Still, the words suggest the
archive will come back. MA-1050 (c) ends the delete in the log; it does not take the row away.

**LOW-C. `status` at the new letter names the old letter in its remedy.** At `S:\Move-Archive` it
said "run mailarchive delete-archive -out "R:\Move-Archive" again". R: no longer existed. These are
the log line's words, by design (MA-1050 (b)). Not tried: what `delete-archive -out R:\Move-Archive`
does while R: is gone; by step (a) 2, it most likely refuses with "Reconnect the drive or folder
first". The backup refusal at S: names S: correctly.

**LOW-D (older than this feature; v1.5.5 behaves the same). `resume` does not bring back a schedule
installed at the terminal.** `schedule -install` records an *export* schedule. `pause` removes its
task. `resume` unseals the archive, then `repairSchedule` → `RepairSpec` always rebuilds a *run-job*
schedule, so the cross-kind guard (`CheckKind` trigger 2, `internal/schedule/kindevidence.go`)
refuses it: "the archive is writable again, but its backup schedule could not be restored … already
has an export schedule named "mailarchive-53924596" with no owner-side record to compare … Remove it
first (mailarchive schedule -out … -remove)…", exit 1. This was the same with and without `-config`,
and the same with the v1.5.5 release program built from tag v1.5.5 (a separate worktree, removed
afterwards). It is loud, it names the remedy, and no mail or file is touched. But `pause` says
"Run `mailarchive resume …` to undo", and the scheduled backups stay off until the person
re-installs them. Rated LOW for this lab because it is not a 1.5.6 regression and does not concern
the delete. The lead may weigh it higher. Schedules made in the Desktop's Settings are run-job and
were not tried here.

## Not run here (MA-1055 stays pending for these)

- A real USB drive unplugged and plugged in again on macOS and Linux, and a remount under another name there.
- An exFAT drive pulled right after the marker and the first removal (DM-12).
- A completely full drive on macOS, Linux and Windows (DM-7).
- A real SMB share used by the Mac and the VM (DM-30). The Parallels shared folder is not one, and cannot be written from the VM.
- On Windows: a dropped mapped drive; a junction at the marker's name; a OneDrive placeholder at the name, and LOW-5's download-on-every-check timing; another Windows account removing the marker. Only the absent drive letter was run here, with a virtual disk standing in for USB.
- The `-config` walk with a network path (item 7, partly).
- MA-1050 (d) "begun on this computer" words. A letter change on Windows reaches the identity route instead (see (b) 6).
