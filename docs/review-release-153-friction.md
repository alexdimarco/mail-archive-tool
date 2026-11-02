# Friction review — release 1.5.3 (archive delete, Remove from list, the switch fix MA-955)

Process: `../assurance-kit/process/friction-review.md`. Contract: `docs/ux-contract.md` (X-series).
Design: `docs/design-archive-delete.md`.

**Verdict: HIGH_FRICTION_NOT_SHIPPABLE.** Every cell but one works, but four findings block ship:
one cell that does not work at all (Linux without the `crontab` program), one server gap that lets a
half-deleted folder be paused or made current, one dead end (an archive made at the command line
cannot be deleted at the command line), and a Desktop page that keeps offering a deleted archive's
controls. A long list of wording fixes (Type III) must also be built before ship.

| Header | |
|---|---|
| Reviewed | branch `switch-keeps-runjob`, commit `c592a70` (builds call themselves "+dirty" only because of untracked files) |
| Date | 2026-11-02 |
| Environments | **Windows:** Parallels "Windows 11" VM (ARM64, amd64 program under emulation), signed-in user, a real Windows pseudo-console for the terminal, Edge 156 driven headless with real clicks. **macOS Desktop:** macOS, the real Desktop in Chrome, a stand-in `launchctl` that only records calls. **Command line:** macOS 26.6 arm64 through `expect`; Linux in a Docker `golang:1.26` container (arm64, root, no cron, no D-Bus). All settings, homes and archives were throwaway; no real scheduled task, keychain entry or user setting was touched |
| Actors | owner (Desktop and terminal), operator at a terminal, command-line-only operator, owner who lost the key, new operator inheriting the system cold |
| Commands and statuses | `delete-archive` (plan only, wrong name, right name, `-plan-out`, `-record-out`), `unlist-archive`, `uninstall -dry-run`, `status`, `forget-key`, `forget-archive`; exits seen: 0, 1, 3, 130, as each verb's help declares |
| Baseline digest | `docs/scenario-catalog.md` sha256 `86cb10d0f2d3cd6904c1502d0ea9c372393220b78d440fdb0a52bff0192bdb80` (unchanged by this review) |
| Cells walked | 34 rows (Windows 17, macOS Desktop 8, command line 9). The Windows walker's summary said 14 cells, 11 fully working; its own table has 17 rows, 14 working fully. This review counts the rows |
| Function | 25 fully, 8 partly, 1 not |
| High-friction findings | 4 (H1–H4 below) |
| Verdict | **HIGH_FRICTION_NOT_SHIPPABLE** until H1–H4 are fixed and the Type III backlog is built |

## The four blockers

- **H1 — Linux without `crontab`: delete never finishes.** Even an archive with no schedule.
  Step 2 fails "Could not stop the scheduled backup mailarchive-4a0c0150: crontab -l failed…",
  exit 3, the row is marked unfinished, a re-run fails the same way, and then `uninstall -dry-run`
  refuses too. The only ways out (install cron, or delete `deletions.log` by hand) are not said.
  With a stand-in `crontab` the same delete completed. **Fix:** treat "crontab not installed" as
  "no cron entry"; give step 2's failure a remedy. (Command line, cell 5.)
- **H2 — the server lets a half-deleted folder be paused or made current.** A page opened before
  the delete still shows Pause and Switch. Their requests were accepted: Pause sealed the
  half-deleted folder, and Settings' save made it the current archive. The catalog checks this:
  MA-979 refuses Pause, Resume and Switch only while a delete is *running*; MA-987 refuses only
  Archive now and Run now for a delete that *did not finish*. So this is a real gap, not intended.
  **Fix:** refuse Pause, Resume and Switch (Settings' save) for a folder whose delete did not
  finish, in MA-987's words ("Delete not finished: click Delete again…"); a new catalog row,
  test-first. It is a guard in the delete design, so it likely owes the adversarial pass: the
  operator decides. (macOS Desktop, cell 5, finding B.)
- **H3 — an archive made at the command line cannot be deleted at the command line.** It is
  refused as "not in Your archives… can be added with Add another archive in MailArchive
  Desktop", and no command adds a row. On a headless Linux box there is no Desktop, so it can
  never be deleted by MailArchive. The refusal also names the wrong button (the button that adds
  an existing folder is "Open an archive folder…"). **Fix:** let `delete-archive` and
  `unlist-archive` offer to list a folder that holds an archive, or give the command line a way to
  add a row. (Windows 1b, command line 1 and 5e.) Type II.
- **H4 — after deleting the current archive, the page keeps its controls.** Until the result is
  dismissed, Status & health, Settings, How often, Archive location and (for an encrypted
  archive) the Encryption card's "Unlocked…", Lock and "Make an unencrypted copy…" still show and
  work for the deleted folder. After dismissing, the page says "MailArchive Desktop has no archive
  yet" even with other archives listed. **Fix:** while that result is held, grey the rest of the
  page or say "This archive was deleted: Dismiss to refresh"; clear switch notes about it; write
  the no-current-archive text for "you have archives: switch to one". (macOS Desktop 2 and 4,
  finding A; Windows 6c.)

## The walk

Type is the process's: I the control itself, II a design choice, III buildable away (words,
layout, a missing hint). W = Windows, M = macOS Desktop, C = command line.

| # | Cell (actor × scenario) | Functions? | Friction found | Type | Fix |
|---|---|---|---|---|---|
| W1 | Operator, `delete-archive` of a listed archive: save plan, wrong-case name, right name, save record | yes | Plan lists schedules that do not exist, by hash name; record lists none, so plan and record disagree. Plan-only run says a log line "is added". Wrong case ends the run. Time-zone offset shown twice. "First run: not known". | III | B1, B5, B9, B10 |
| W1b | Command-line-only operator deletes an archive made by a command-line export | partly | Refused: not in Your archives; only the Desktop can add it. | II | H3 |
| W2a | Operator, a file opened with no sharing by another program | yes (safe refusal) | Says "click Delete" at a terminal; the next line gives the command. | III (minor) | none needed |
| W2b/3 | Operator, a file held for reading (like antivirus) → part-way delete, then finish | yes | "them" for 1 file. Re-run plan does not say it is finishing an earlier delete. Two records, neither covers the whole delete. | III; II for the split record | B3, D1 |
| W3 | Operator, delete process ended mid-way (12,012 files) | partly | `status` on the half-deleted folder warns only "no schedule". Re-run finishes with no "finishing" context. | III | B2, D1 |
| W4 | Operator, `unlist-archive` (current, normal, not listed) | yes | none | — | none |
| W4b | Desktop, Remove from list → Yes / No | yes | "Removed from Your archives." shows for 1.5 seconds, then the row is just gone. | III (minor) | B14 |
| W5 | Operator, `uninstall -dry-run` after deletes | yes | `deletions.log` listed twice; "KEEP your 0 archive(s)" with an empty list; "your mail is never deleted" now sits beside Delete. | III | B11 |
| W5b | Operator, `uninstall -dry-run` while a delete is not finished | yes (refuses) | Dry run shows no plan, says "removed nothing", uses the placeholder FOLDER. | III | B11 |
| W6 | Owner, Desktop in Edge: Delete… → save plan → wrong case → right name → delete → dismiss → save record | yes | About 7 clicks. "1 files"; kept file not named; kept count reads as 3 instead of 2; no "capital letters count"; Forget mentioned on a plain archive; "macOS Time Machine" on Windows. | III | B4, B6, B7, B8 |
| W6b | Owner, row "Delete not finished", file still held → partial → release → Delete… again | yes | Path squeezed to about 6 characters wide. One extra "Close without saving…?" click. Plan does not say it is finishing. | III | B14, D1 |
| W6c | Owner, delete the current encrypted archive (key open on the page) | yes | "This computer forgot the archive's key" when no key had been remembered. Stale switch note stays. | III | B3, H4 |
| W7 | Owner who lost the key, `delete-archive` of an encrypted archive | yes | Same key wording. | III | B3 |
| W8 | Owner, MA-955: switch away from a Run-now-only backup | yes | No question; switch happened; job file kept, no scheduler entry. Note beside the button not readable (page reloads). | — | none |
| W9 | Owner, Delete… on that Run-now-only archive | partly | Plan lists the backup under "stops" **and** "cannot confirm is this archive's… check it yourself". The result never mentions it, though the job file was removed. Possibly a bug in how the plan matches a Run-now-only record on Windows (not traced in code). | III, maybe a bug | B1, B15 |
| W10 | Operator, either verb while the Desktop runs | yes (refuses) | Does not say where Shut down is. | III (minor) | B12 |
| W11 | Cold new operator, guide + `--help` only | yes | Guide never mentions the command line or where `deletions.log` is. | III | B16 |
| M1 | Owner deletes a plain archive with a file of their own beside it | yes | Phantom schedules (plan and record disagree); kept file not named; "1 file that are"; Save this plan shows nothing on screen; no case hint. On disk all correct; record digest matches the log. | III | B1, B6, B7, B13 |
| M2 | Lone current archive: the "This archive" card | partly | Remove from list always refused, button enabled, tooltip promises a question. H4. Result out of view on a narrow layout. | II (Remove); III | B14, H4 |
| M3 | Remove from list with the schedule question (run-job) | yes | Backup named only by hash; result missing its period and repeated on the next line; button enabled on the current archive. | III | B1, B12, B14 |
| M4 | Encrypted archive: key open (current) and key closed (after `forget-key`) | yes | Schedule line shown though encrypted archives cannot have schedules. "Forgot the archive's key" when none was remembered. Stale "Unlocked" card after delete. Six row buttons squeeze the path to ~4 characters. | III | B1, B3, B14, H4 |
| M5 | A delete that did not finish (read-only mail folder), then Pause / Switch on it, then finish | partly | Result names files, not the read-only folder that caused it. A stale tab's Pause and Switch were accepted (H2). Re-run plan does not say it is finishing; two records needed. | II (H2); III | H2, B6, D1 |
| M6 | MA-955: switch away from a run-job archive | yes | Asked nothing; "Settings saved." beside the pressed row; job file and plist untouched. | — | none |
| M7 | Cold new operator, in-app guide only | partly | Guide does not mention "Delete not finished", nor that deleting the current archive leaves none chosen. "never deleted" claims elsewhere on the page. | III | B11, B16 |
| M8 | Command line: delete (plan, wrong, right), unlist, `uninstall -dry-run` | yes | No `-config` on the new verbs. Wrong button named. Wrong name ends the run. Missing periods. Log listed twice. | III; I (exact name) | H3, B10, B11, B12 |
| C1 | New operator, cold, macOS: find how to delete from help and README | partly | 4 lookups, 1 dead end. README and 1.5.3 release notes say nothing about delete. Then H3. | III (docs); II (H3) | B16, H3 |
| C2 | Owner, macOS: save plan, then delete | yes | Phantom schedules; "Recorded in deletions.log" with no path; "First run: not known". Plan and record share the same manifest fingerprint (good). | III | B1, B9, B16 |
| C3 | Owner, macOS: Ctrl-C at the prompt; Ctrl-C signal during delete; run again | yes | Re-run plan does not say it finishes an earlier delete; final record covers only the second run and loses "Last backup". `status` silent about it and blames missing `-raw`. A stop before any change prints a 25-line "refused after start" record. | II (split record); III | D1, B2, B17 |
| C4 | Owner, macOS: lost key | yes | Key wording. Wrong case ends the run. | III; I (exact name) | B3 |
| C5 | Linux container: encrypted delete with no secret store | **no** | H1. The no-store path (MA-999) is never reached: the key store on Linux is a file. Failed record says "record and key: not applicable" while the record exists. `forget-archive` ends naming only the ID. H3 again (no Desktop at all). | III (H1 is a function failure); II (H3) | H1, H3, B3, B18 |
| C6 | Owner: `uninstall -dry-run` with `deletions.log`; read what `-yes` prints | yes; partly with an unfinished delete | Log listed twice; dry run refuses with no plan and a FOLDER placeholder. Under `-yes` the "copy it first" advice comes in the same run that removes it. | III; II (`-yes`) | B11, D2 |
| C7 | Delete while a backup holds the archive | yes | none | — | none |
| C8 | `unlist-archive` happy path | yes | Folder path printed with no label. | III | B12 |
| C9 | Delete with no terminal | yes | Program name doubled; delete's message also names Remove from list. | III | B12 |

**Type I, counted not faulted:** typing the folder's exact name (capitals count) is the
confirmation itself. A wrong name ending the run is part of that control; a retry is optional
(see B10).

## Design choices (Type II) beyond H2 and H3

- **D1 — finishing a part-way delete.** The re-run's plan never says "this finishes the delete
  started DATE", and each run makes its own record, so an approver comparing plan and record sees
  a mismatch with no explanation. **Fix:** head the re-run plan with "Finishing the delete begun
  DATE"; have the record name the first run (its id or digest) and carry its totals. (W2b, W3,
  W6b, M5, C3.)
- **D2 — `uninstall -yes` removes `deletions.log` in the same run that tells you to copy it.**
  **Fix:** when `deletions.log` exists, pause once or refuse with "copy it first". (C6.)
- **D3 — Remove from list on a lone current archive.** Always refused, button still enabled.
  **Fix:** disable it with the reason as its tooltip. (M2, M3; also seen on Windows.)

## Wording for the operator

Every new message the walkers saw, once each, verbatim. "Fine" means leave it. A rewrite is
suggested only where the words are unclear or wrong.

### Terminal (`delete-archive`, `unlist-archive`)

1. "mailarchive: mailarchive delete-archive needs you at a terminal: it shows the plan there, and you type the folder's name to confirm. Run it in a terminal window (or use Delete… / Remove from list on the Your archives card in MailArchive Desktop)" — no terminal. Program name doubled; give each verb its own ending (delete names only Delete…).
2. "DIR is not in Your archives (MailArchive Desktop's list for the account this terminal runs as), so nothing was deleted. Give the folder as its row in Your archives shows it, in a terminal of your own account (not one run as administrator or as another user); a folder not listed can be added with Add another archive in MailArchive Desktop" — dead end (H3); names the wrong button: say "Open an archive folder…". Missing period.
3. "DIR is not in Your archives, so there is nothing to remove from the list; nothing was changed" — fine (add a period).
4. "Your archives (PATH) cannot be read (archive index file PATH is readable by other users (mode 0644): run `chmod 600 PATH`: readable by other users), so MailArchive cannot tell whether DIR is in it; nothing was changed" — quote the path (macOS "Application Support" has a space) and say "readable by other users" once.
5. "MailArchive will permanently delete the plain archive in this folder (no Recycle Bin or Trash):" — fine.
6. "files: 10 (67.6 KB)" — fine. "store 1: Inbox" — "store" is jargon: "mail folder 1: Inbox".
7. "Schedules to stop: mailarchive-f518d68f, mailarchive-f518d68f-verify" — wrong when nothing is installed; hash names. "Scheduled backups it stops: none" or "the daily backup at 02:00".
8. "What Delete does not reach: a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; Windows File History and Previous Versions; macOS Time Machine; copies, exports or decrypted copies made elsewhere; sync tools other than those detected" — fine but long; show only this computer's items; add the final period on the Desktop.
9. "A line is added to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed." — in a plan-only run: "If you delete it, a line is added…". Give the full path.
10. "Kept, not the archive's: 1" — name the file(s): "Kept (not the archive's): my-notes.txt".
11. "Mail copies this archive did not record: 1 (kept unless you choose to delete them)" — fine.
12. "Also delete these 1 files (for retention)? [y/N]" — "Also delete this 1 file (for retention)?"
13. "If the folder is removed, this computer will forget this archive's key: this is the last copy of it this computer recorded. Any other copy of this archive will then need its password or recovery key to open." — fine when a key is remembered; when none is: "…this computer forgets its record of this archive (no key is remembered here)."
14. "This computer has not recorded this archive folder, so Delete forgets no key here." — fine.
15. "The plan was saved to FILE. Nothing was deleted." — fine.
16. "Type the folder's name, Archive-One, to delete it: " — fine.
17. "mailarchive: The name you typed does not match this folder's name, Archive-One, so nothing was deleted. Type it exactly as shown. (Folder: …)" — add "(capital letters count)".
18. "Deleted. 10 files (67.6 KB) removed. The folder is gone." — fine.
19. "Removed from Your archives" / "Recorded in deletions.log" — add periods; give the log's full path.
20. "The deletion record was saved to FILE." — fine.
21. "Deletion record (copy it now: MailArchive does not keep a copy)" — fine.
22. "mailarchive: MailArchive cannot read …\Inbox\2025-03-04_1030_Lunch-on-Friday_90398d21.html (The process cannot access the file because it is being used by another process.). Close any program using it, such as a backup or an antivirus scan, and make sure your account can open it, then click Delete again. (Folder: …)" then "At this terminal, Delete again is: mailarchive delete-archive -out "…\Archive-Two"" — fine (the second line rescues "click").
23. "Not fully deleted. 1 file could not be removed: Inbox/2025-03-04_1030_Lunch-on-Friday_90398d21.html. Close Outlook or any program using them, then click Delete again." — "them" → "it" for one file.
24. "Not fully deleted. 7 files could not be removed: src/Inbox/…. The system did not allow it (a locked file, a folder you may not change, or a read-only drive): unlock it and click Delete again, or delete these by hand." — clear; name the folder that blocked it.
25. "mailarchive: interrupted; nothing was changed" — fine.
26. "mailarchive: stopping: the delete stops before it changes anything." then "Stopped by Ctrl-C before anything was deleted, so nothing was changed." — fine (but see record line 41).
27. "mailarchive: delete-archive was stopped by Ctrl-C and did not finish: the folder may be partly deleted. Run `mailarchive delete-archive -out "…/Big"` again (or Delete… in MailArchive Desktop) to finish it." — fine.
28. "mailarchive: A backup of this archive is running. Try again when it ends. (Folder: …)" — fine.
29. "Could not stop the scheduled backup mailarchive-4a0c0150: crontab -l failed, so the crontab was left alone rather than overwritten: exec: "crontab": executable file not found in $PATH:. Nothing else was changed." — H1: wrong premise, no remedy, ":." garbled.
30. "mailarchive: This is the archive in use. Switch to another archive, or add one (Add another archive), then remove this one from the list. (Folder: …)" + "Switching archives, Add another archive and Settings are in MailArchive Desktop." — fine.
31. "Remove from list? The folder and its files are kept. [y/N]" → "Removed from Your archives. The folder and its files are kept." — fine; label the folder line before it "Folder:".
32. "mailarchive: MailArchive Desktop is running for your account (in this or another sign-in to this computer), so nothing was changed. Use the Desktop, or shut it down first" — "…or click Shut down at the top of MailArchive Desktop." Add the period.
33. `delete-archive -h`: "…and, for an encrypted archive, make this computer forget its key…" — "…when this is the last copy it recorded".
34. `delete-archive -h` / `unlist-archive -h` exit lines ("Exit: 0 deleted (or the plan saved); 3 not fully deleted…; 130 or 143…") — fine.
35. Root help: "mailarchive unlist-archive -out DIR … remove an archive from Your archives; the folder is kept" — a command-line-only user does not know "Your archives"; say "MailArchive Desktop's list of archives".
36. "flag provided but not defined: -config" — fine as an error; the new verbs have no `-config` (backlog B19).

### Saved plan and deletion record

37. "Status: planned, nothing deleted" / "Status: complete" / "Status: not fully deleted" — fine.
38. "Date: 2026-11-02 01:08:50 -05 (-05:00)" — offset twice; also "Last backup: … UTC" beside "Date: … EST". Use one form.
39. "First run: not known" — appears for every archive made at the command line; take it from the run history or say why.
40. "Kept: …" — says nothing; name the file or "1 file outside the mail folders".
41. "Status: refused after start" (a Ctrl-C before any change) — "stopped before anything was deleted"; a short line, not a full record.
42. "Schedule to stop: mailarchive-…" (twice) — the phantom of item 7.
43. "This computer's record and key: to be forgotten / forgotten / not applicable" — fine, except "not applicable" on a not-fully-deleted record while the record still exists.
44. "Messages: not known: encrypted, key not open" (also Mail dates, First run) — fine.
45. "Left: store 1/2025-03-04_1030_._90398d21.html" (Windows terminal) — the "." may be the capture turning "…" into "."; not checked.

### Desktop page

46. Card intro: "The mail archives MailArchive Desktop knows about on this computer. The one marked current is the one this page shows and changes. Each archive can have its own scheduled backup; if switching would affect one, you are asked first." — fine.
47. Tooltips: Delete… "Permanently delete this archive's files. You see what would be removed first, and type the folder's name to confirm."; Save this plan "Save this plan as a text file, for example to have it approved first. Nothing is deleted." — fine. Remove from list "Take this archive off this list. The folder and its files are kept; you are asked first, and told wh[at…]" — misleading on the current archive (D3).
48. "Delete this archive permanently" — fine.
49. "Remove from list forgets the row (and, after asking, stops its schedules) and keeps the folder. Delete erases the archive's files. Forget (encrypted archives) only stops this computer protecting the folder." — "the row" and "protecting" are jargon; drop the Forget sentence on plain archives. Suggest: "Remove from list takes it off this list and keeps the folder. Delete erases its files. Forget (encrypted archives only) makes this computer stop remembering its key."
50. "Folder: PATH", "Delete removes 10 files (67.6 KB)." — fine. "Stores: store 1 Inbox" — "Mail folders: Inbox".
51. "Scheduled backups it stops: mailarchive-9b7c006f, mailarchive-9b7c006f-verify" — as item 7; leave out entirely for encrypted archives.
52. "Kept: not the archive's: 1" — as item 10.
53. "Mail copies this archive did not record: 1" + "Also delete these 1 files (for retention)" — as item 12.
54. "This archive is paused." — fine.
55. "Type the folder's name, Archive-Four, to delete it" / "doesn't match yet" — add "(capital letters count)"; keep the name on one line (it broke at the hyphen at phone width).
56. "…\.mailarchive-schedule.json names the schedule mailarchive-98c4d940, which MailArchive cannot confirm is this archive's: not removed, check it yourself" — contradicts "it stops" for the same name; the Desktop made that backup itself (B15).
57. "Delete of PATH" (result heading) — "Deleted: PATH" or "Result for PATH".
58. "Deleted. 10 files (67.6 KB) removed. The folder is kept: it holds 2 files that are not the archive's. 1 mail copy the archive did not record was kept." — reads as 3; "The folder is kept: 2 files are left in it: your 1 file and 1 mail copy the archive did not record."
59. "Deleted. 13 files (69.7 KB) removed. The folder is kept: it holds 1 file that are not the archive's." — "1 file that is not".
60. "This computer forgot the archive's key" — as item 13 when no key was remembered; add a period.
61. "Save this record now: MailArchive keeps no copy after you close this result." — fine.
62. "Close without saving the deletion record?" [Close without saving] [Keep it] — fine.
63. Row: "Removed from Your archives." — fine (but shown only 1.5 s on Windows).
64. Row: "Delete not finished: click Delete again" (and on the Archive now card) — fine words; put it on its own line.
65. Row question: "Its scheduled backup mailarchive-d7a1fe3b will also be stopped. Remove from list? The folder and its files are kept." — name the backup by when it runs.
66. Row result: "Removed from Your archives. The folder and its files are kept. Stopped the scheduled backup mailarchive-d7a1fe3b" — add the period, name by when, drop the repeated "Removed from Your archives." below.
67. Row refusal: "This is the archive in use. Switch to another archive, or add one (Add another archive), then remove this one from the list." — fine, but a dead end on a lone archive (D3).
68. Lone card: "This archive" / "The archive this page shows. Delete… erases its files, after showing what it would remove; Remove from list takes it off MailArchive's list and keeps its files." — promises a Remove that is always refused (D3).
69. "Settings saved." beside the pressed Switch row — fine (MA-955).
70. After deleting the current archive: "MailArchive Desktop has no archive yet. To use one you already have, click Open an archive folder… above…" — wrong when archives are listed (H4).
71. Stale after delete: "Unlocked: you can read this archive's messages, search it and go back in time. This computer remembers its key." and "Switching to another archive locks PATH. This computer remembers its key, so it opens again by itself when you switch back." / "…When you switch back, unlock it again on its Encryption card." — false after the delete; clear them (H4).
72. Security & privacy: "Reversible. You can revoke access any time, and uninstalling keeps your archive — your saved mail is never deleted." and the Uninstall card's "Your archives are kept — your saved mail is never deleted." — now contradict Delete: "Uninstalling never deletes your saved mail; only Delete… does."
73. Shut down: "Confirm shut down" / "Click again to stop MailArchive Desktop. You can reopen it any time; your archives and schedules are untouched." — fine.
74. Guide, "Deleting an archive" and "Removing an archive from the list" — clear and Windows-first; missing the command line, `deletions.log`'s location, "Delete not finished", and what happens after deleting the current archive (B16).

### Uninstall

75. "MailArchive uninstall will REMOVE: … settings/log files: 3 … - the deletion log: PATH / Copy deletions.log first if you need it to check saved deletion records." — list the log once; say where to copy it.
76. "It will KEEP your 0 archive(s) — your mail is never deleted:" (then nothing) — "No archives are listed." and "uninstalling never deletes your mail".
77. "mailarchive: A delete of DIR is running or did not finish, so uninstall removed nothing: uninstalling would remove MailArchive's deletion log (…), which that delete needs to finish. Wait for its result, or finish it (Delete… on its row in MailArchive Desktop, or mailarchive delete-archive -out FOLDER), then uninstall again. If you no longer want to finish it, copy deletions.log somewhere safe, delete it, then uninstall again; …" — fill in the real folder; in a dry run, show the plan and say "uninstall will refuse until…", not "removed nothing".
78. Under `-yes` (read, not run): "This run removes it. A copy had to be made before running uninstall -yes (uninstall -dry-run shows this list and removes nothing)." then "Done — MailArchive's schedules, settings and sign-in are removed; your archives are untouched." and "The deletion log was removed: PATH" — the advice comes too late (D2).

### Status and other commands

79. `status` on a half-deleted archive: "Posture: WARN … no schedule is recorded…", "Extractable: 5234 of 6003 records…", "the remaining 1926 have no preserved original — an mbox/maildir/Microsoft 365 source keeps one only when archived with -raw…" — misleading by silence: add "A delete of this archive did not finish: run delete-archive again (or Delete… on its row)." (B2.)
80. `forget-key`: "This computer no longer remembers the key of the encrypted archive DIR; backups you start and opening it will ask for the password." — fine.
81. `forget-archive`: "Another copy of this archive that used this same folder path, such as another drive mounted at the same place, loses this computer's protection at this path too." — "protection" is unclear. Ends "This computer forgot the encrypted archive b6acf138273229a3b341f9c7f6c62f94." — name the folder too.

## Operability backlog (Type III — must be built before ship)

Numbered for reference; the source cells are in the walk table.

- **B1 — scheduled backups in the plan.** List only backups the scheduler reports as installed,
  or say "none"; describe them by when they run, not by hash; leave the line out for encrypted
  archives; make the record repeat the plan's lines ("already absent"). Seen by all three walkers.
- **B2 — `status` and the health view say when a delete did not finish.** A gap against UX rule X6.
- **B3 — key wording when no key was remembered.** Plan, result and record: "forgets its record of
  this archive (no key was remembered)". Fix the record's "not applicable" while the record exists.
- **B4 — show only this computer's items** in "What Delete does not reach" (no Time Machine on Windows).
- **B5 — the plan-only log line:** "If you delete it, a line is added…".
- **B6 — name kept files** that are not the archive's; name the folder that blocked a delete.
- **B7 — plurals and counts:** "1 files", "them" for one file, "1 file that are", the double count in the kept line.
- **B8 — "capital letters count"** beside the typed-name prompt, terminal and Desktop.
- **B9 — dates and first run:** one time-zone form; explain or fill "First run: not known".
- **B10 — optional:** allow a few tries for the typed name at the terminal instead of ending the run (the name itself stays the control).
- **B11 — uninstall:** list `deletions.log` once and say where to copy it; "No archives are listed"; replace every "your mail is never deleted" (CLI, Uninstall card, Security & privacy) with "uninstalling never deletes your mail"; in a dry run with an unfinished delete, show the plan and the real folder.
- **B12 — terminal message polish:** name the Shut down button; final periods; per-verb no-terminal text; label the folder line in `unlist-archive`; "store" → "mail folder".
- **B13 — Save this plan** shows "Plan saved" on screen.
- **B14 — Desktop layout:** row note "Delete not finished" on its own line; row buttons wrap under the path (path was squeezed to 4–6 characters); "Removed from Your archives." stays until the next action; scroll a result into view on narrow layouts; the typed name never wraps.
- **B15 — a Run-now-only backup is this archive's own.** Do not list it as both "stops" and "check it yourself"; say "Removed its Run now backup" in the result. Check whether the plan's match is a bug on Windows (no scheduler side-file).
- **B16 — docs:** README and the 1.5.3 release notes cover delete, Remove from list and `deletions.log` (with its path, e.g. `%APPDATA%\mailarchive\deletions.log`); the guide names both commands, "Delete not finished", and "after deleting the current archive, switch to another".
- **B17 — a Ctrl-C stop before any change** prints a short line, not a 25-line "refused after start" record.
- **B18 — `forget-archive`** names the folder in its last line; replace "protection".
- **B19 — `-config` on the new verbs**, so they reach a Desktop started with `-config`.
- **B20 — catalog wording, MA-999:** on macOS and Linux the remembered-key store is a file, so "no Secret Service in a headless Linux session" is not how the no-store path is reached. Correct the row's wording; the path itself was not walked.

Seen but outside 1.5.3 and not counted: root `--help` and `uninstall -h` exit 1 with "flag: help
requested" (rule X1, predates 1.5.3); the uninstall plan does not list the encrypted-archive record
or the remembered-key file (not checked whether intended).

## Not walked

- **A delete or Remove from list that stops a really installed scheduled backup** — real installs were not allowed on any platform (macOS used a stand-in `launchctl`).
- **The Desktop's Uninstall Remove, and `uninstall -yes`** — dry run only; `-yes` was read in the code's words, not run.
- **MA-955's positive twin** (a Microsoft 365 capture still asks on switch) — needs Microsoft 365.
- **MA-955's note beside the Switch button on Windows** — the page reloads at once; seen on macOS ("Settings saved.").
- **The no-secret-store path (MA-999)** — unreachable on macOS and Linux in this build; not tried on Windows (no key was stored in Credential Manager).
- **Remembered keys on Windows** — never stored, to keep away from the real Credential Manager.
- **A real phone-width layout** — the browser window would not resize; a forced 390-pixel CSS stand-in only.
- **Ctrl-C on Windows** at the prompt or mid-delete — out of time; Windows "End task" covers the mid-delete case.
- **A file held open on macOS or Linux** — those systems do not block deleting open files; a read-only folder and a running backup were used instead.
- **Synced (OneDrive) folders, links or junctions, long paths, read-only files on Windows** — not set up.
- **`-plan-out` / `-record-out` refusals** (existing file, file inside the archive) — not tried.
- **A backup run into a half-deleted folder** — not tried.
- **The Desktop on Linux** — the container has no screen.

## About this report

Date: 2026-11-02, from the session clock and the walkers' program output. Register: a synthesis
of three live walk reports; every result above comes from what a walker saw, and anything they did
not run is listed as not walked. The synthesis owner read MA-955, MA-979, MA-987 and MA-999 in full
to settle H2 and B20; the walkers' other catalog judgements rest on the rows' opening text. Not
checked here: the "." for "…" (item 45), and why Windows calls the Run-now-only record
unconfirmed (B15). Screenshots: Windows `…/scratchpad/friction/windows/` (01–06); saved plans and
records: `…/scratchpad/friction/out/` and `…/scratchpad/friction/linux/out/` under the session
scratchpad. Smallest check for the operator: on any Linux box without cron, run `delete-archive`
on a listed archive (H1).
