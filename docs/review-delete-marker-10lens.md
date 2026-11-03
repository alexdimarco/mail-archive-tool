# Review: the delete marker (design-archive-delete §15), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** No finding is HIGH. 19 MED and 33 LOW findings across the five
reviewers (counted as each reviewer filed them, before merging duplicates) collapse into 31 conditions (DM-1 to DM-31 below), each folded into §15 of
`docs/design-archive-delete.md` beside the text it changes, marked "Folded from the 10-lens
review [DM-n]". Build may start with slices 1 and 2 of §15.10 (the readers and the refusals).
The delete's own write of the marker (slice 3) waits for owner question 1 on *where* it is
written; 1.5.5 does not ship until the owner answers question 2 (the planted-marker security
acceptance).

- Date worked on: 2 November 2026 (the session's date).
- Design reviewed: `docs/design-archive-delete.md` §15 at commit 10b6e65 (branch
  `switch-keeps-runjob`), sha256 `1e835cd8e59ef3104aa07b48286a9913aa1f63e6157778b608fd1594ffdddafb`.
- Procedure: `../assurance-kit/process/design-review.md`. Five lens reviewers, two adjacent
  lenses each (1+2, 3+4, 5+6, 7+8, 9+10), read-only, no code run.
- Synthesis (this file): one owner. It worked as the skeptic for the load-bearing code claims
  by reading the code at 10b6e65 (list below). No separate skeptic agent and no rescue agent
  were run: the rescue pass is given the surviving blockers, and no finding was a blocker (no
  HIGH, no reviewer said NO GO); every MED already came with a modest mechanism change, which
  is what a rescue would have looked for.
- Register: review synthesis (findings, the synthesiser's decisions, and opinion where marked).

## Claims checked against the code (the skeptic's part)

Each was read directly; none was run.

| Claim | Where read | Result |
|---|---|---|
| No released version can delete an archive | `git ls-tree v1.5.3 internal/archdelete` (absent); `git ls-tree origin/windows-installer internal/archdelete` (absent) | Confirmed |
| Step 6 removes the anchor only when every other recorded name is gone | `internal/archdelete/execute.go` lines 594-608 (`if len(res.NotRemoved) == 0`) | Confirmed (ticked §4.3 copies left go to `TickedLeft` and do not block it) |
| The no-anchor re-run refuses on any identity mismatch | `internal/archdelete/rerun.go` line 127 (`if id != u.Identity { return nil, orig }`) | Confirmed |
| `sameFolderStat` falls back to spelling when either path cannot be read | `internal/archdelete/archdelete.go` lines 827-841 | Confirmed |
| The two skipped tests assert `len(ma1044Names(half)) == 1` | `internal/archdelete/ma1045_backup_guard_settle_test.go` lines 262 and 278 | Confirmed |
| The Desktop's "Open an archive folder…" goes through the settings save | `internal/desktop/dashboard.go` lines 2548-2556 (`doSave('')`) | Confirmed |
| The settings save asks about the folder that stays current | `dashboard.go` line 546 (`named := orElse(out, …cfg.Out)`), lines 556-563 | Confirmed |
| The delete check runs before the seal check and the "is the drive there" check | `internal/app/run.go` lines 541, 546, 568; `graph.go` lines 90, 95, 117 | Confirmed |
| Under the lock, the emptied-folder guard is asked before the delete check | `run.go` lines 648 then 655; `graph.go` 177 then 183 | Confirmed |
| `RecordRefusedRun` skips a paused folder but not a marker folder | `run.go` lines 1243-1262 (`seal.IsSealed`) | Confirmed |
| An alert email goes once per failing streak, and only when alerts are set up | `internal/backupalert/streak.go` lines 19-24, 118-128 | Confirmed |
| Auto-pause counts only an unreachable mail source | `internal/wizard/pause.go` lines 118-121 | Confirmed |
| The encrypted alert's fixed sentence says "click Delete again" | `internal/state/encrypted_record.go` line 80; pinned by `internal/backupalert/ma1045_delete_alert_words_test.go` and `internal/app/ma1045_delete_refusal_kinds_test.go` | Confirmed |
| A Windows cloud placeholder is "file-like" to the lister, not regular | `internal/archdelete/lister.go` line 82 → `util.DescriptorReadable` (`internal/util/openregular.go` line 142) | Confirmed |
| The seal is read by presence and shows its reason on older versions | `git show v1.5.3:internal/seal/seal.go` (`RefuseWrite`, `IsSealed`, `Unseal` lock-free) | Confirmed |

Not checked by anyone (each needs a lab run): which error Go reports on Windows for an unplugged
USB drive letter or a dropped mapped drive; how FAT and exFAT order metadata writes when a drive
is pulled; whether macOS or Linux hand a replugged drive the same device number or a new mount
name; how a OneDrive placeholder at the marker's name is reported.

## Verdicts by lens

| Lenses | Reviewer's verdict | MED | LOW |
|---|---|---|---|
| 1 purpose, 2 proportionality | GO WITH CONDITIONS | 5 | 3 |
| 3 durability, 4 usability | GO WITH CONDITIONS | 3 | 6 |
| 5 integration, 6 security | GO WITH CONDITIONS | 4 | 9 |
| 7 failure domains, 8 migration | GO WITH CONDITIONS | 4 | 6 |
| 9 operational cost, 10 honesty | GO WITH CONDITIONS | 3 | 9 |

The overall verdict is the worst of these: GO WITH CONDITIONS. No finding was shown wrong. One
finding's *remedy* is not adopted as written (L7/8 MED-A, the marker's position), because it
would narrow the owner's 2 November ruling; it goes to the owner instead (DM-3).

## Findings, each with its condition

Reviewer IDs are the reviewers' own (for example "L5/6 MED-C" is the lens 5-and-6 reviewer's
MED-C). Where several reviewers found the same thing, the highest severity is kept.

| ID | Lens | Design section | Finding (short) | Verdict | Condition |
|---|---|---|---|---|---|
| L1/2 MED-1, L3/4 LOW-1, L5/6 LOW-6, L7/8 LOW-3, L9/10 LOW-1 | 1, 3, 5, 8, 10 | §15.6, §15.7 | "A 1.5.4 delete" does not exist: no release before 1.5.5 can delete | CONFIRMED (MED) | DM-1 |
| L1/2 MED-2, L7/8 C11-C13, L9/10 MED-2 | 1, 2, 7, 10 | §15.7, §15.9 | Changing the two skipped tests' setup: not a weakening in substance, but "word for word" cannot hold, the remount test stops testing the log, and the no-marker case leaves as a test | CONFIRMED (MED) | DM-2 |
| L7/8 MED-A | 7 | §15.2 | The marker at step 1 (d) blocks another computer even when the delete never touched the folder | CONFIRMED (MED); remedy goes to the owner | DM-3, owner Q1 |
| L1/2 MED-3 | 1, 2 | §15 intro, §15.6 | The pause seal, which every version since 1.1.0 honours, is not weighed as a belt for older versions | CONFIRMED (MED) | DM-4, owner Q3 |
| L1/2 MED-4, L5/6 LOW-1 | 2, 5 | OMISSION | No build slices; readers must come before the writer | CONFIRMED (MED) | DM-5 |
| L1/2 MED-5 | 1 | §15.5 | After a remount, the no-anchor re-run refuses on identity and Remove from list is refused too: a dead end | CONFIRMED (MED) | DM-6 |
| L3/4 MED-A | 3, 4 | §15.2 | The no-manifest re-run can create a marker that never goes away when the person's files stay | CONFIRMED (MED) | DM-9 |
| L3/4 MED-B, L7/8 MED-C | 3, 4, 7 | §15.5 | A drive-letter or mount-name change defeats the run-id route; the old row and uninstall stay blocked | CONFIRMED (MED) | DM-10 |
| L3/4 MED-C | 4 | §15.3 | The other computer's person, who deleted nothing, is told "click Delete again"; the alert too | CONFIRMED (MED) | DM-11 |
| L5/6 MED-A | 5 | §15.4 | "Open an archive folder…" goes through the save, which the marker refuses: no row is ever added | CONFIRMED (MED) | DM-17 |
| L5/6 MED-B | 5 | §15.4 | On the other computer every settings save is refused while the marker folder is current | CONFIRMED (MED) | DM-18 |
| L5/6 MED-C, L7/8 MED-B | 6, 7 | §15.4 | A planted marker alone lets Delete remove README.txt, index.html and *.log in a folder of the person's own files | CONFIRMED (MED) | DM-19 |
| L5/6 MED-D | 5, 6 | §15.2, §15.3 | A cloud placeholder is not "regular": wrong words and the other computer cannot finish | CONFIRMED (MED) | DM-20 |
| L7/8 MED-D | 7 | OMISSION | Nothing says what happens when this computer's log and the folder's marker disagree (lost marker, rotation drive) | CONFIRMED (MED) | DM-27 |
| L7/8 LOW-1, L9/10 MED-1 | 7, 9 | §15.3 | A dead share or empty card reader gets "could not check … for a delete" instead of MA-689's "is the drive connected" | CONFIRMED (MED) | DM-22 |
| L9/10 MED-3 | 10 | §15.6, §15.8 Q3 | "The refusal is loud" and "they can already delete it" overstate | CONFIRMED (MED) | DM-31 |
| L1/2 LOW-1, L9/10 LOW-4, LOW-5, LOW-6, LOW-7 | 1, 10 | §15.1, §15.2, §15.5 | Over-claims: "first thing anywhere", "file dates already show", "could not be deleted either", "only that delete ever wrote" | CONFIRMED (LOW) | DM-7 |
| L1/2 LOW-2, L5/6 LOW-7 | 1, 6 | §15 intro, §15.3, §15.5 | "Content is only for words" is contradicted: `run` feeds the log match and the record | CONFIRMED (LOW) | DM-8 |
| L1/2 LOW-3 | 1, 10 | §15.9 | "MED-1 and MED-2 are closed" without its condition | CONFIRMED (LOW) | DM-2 |
| L3/4 LOW-2, L7/8 LOW-2, L9/10 LOW-3 | 3, 7, 10 | §15.2 | Power loss on FAT or exFAT can keep later removals and lose the marker's name | CONFIRMED (LOW) | DM-12 |
| L3/4 LOW-3 | 3 | §15.6 | Synced folders: order, not only timing | CONFIRMED (LOW) | DM-13 |
| L3/4 LOW-4, L5/6 LOW-8 | 3, 6 | §15.3 | `RecordRefusedRun`, `reindex` and the program-copies setting still write into a marker folder | CONFIRMED (LOW) | DM-14 |
| L3/4 LOW-5 | 3 | §15.4 | The row's stat memo follows links; a dangling link reads as "not there" | CONFIRMED (LOW) | DM-15 |
| L3/4 LOW-6 | 3 | §15.6 | Years of refused runs: `delete_unfinished` never auto-pauses | CONFIRMED (LOW) | DM-16, owner Q5 |
| L5/6 LOW-2 | 5 | §15.3 | "Every place asks `UnfinishedAt`" contradicts §15.4's log-only places | CONFIRMED (LOW) | DM-21 |
| L5/6 LOW-3 | 5 | §15.3 | The "could not check" refusal has words only for backups | CONFIRMED (LOW) | DM-23 |
| L5/6 LOW-4, L9/10 LOW-8, L7/8 LOW-5 | 5, 8, 9 | §15.1, §15.3 | Two copies of the refusal words; the name is a contract between versions forever | CONFIRMED (LOW) | DM-24 |
| L5/6 LOW-5 | 5 | §15.3 | Under the lock, a run gets the emptied-folder words, not "Delete not finished" | CONFIRMED (LOW) | DM-25 |
| L5/6 LOW-9 | 6 | §15.2 | An empty marker after a power cut is never filled in, so the run-id route cannot match | CONFIRMED (LOW) | DM-26 |
| L7/8 LOW-4 | 8 | §15.4 | An older version's backups after the delete began make the plan's "This finishes…" misleading | CONFIRMED (LOW) | DM-28 |
| L7/8 LOW-6 | 8 | OMISSION | No rollback for the rollout but installing a release without Delete | CONFIRMED (LOW) | DM-29 |
| L9/10 LOW-2 | 10 | §15.2, §15.5 | "Proven by §15.7's tests" before any test exists | CONFIRMED (LOW) | DM-30 |
| L9/10 LOW-4 (second half) | 9, 10 | §15.2, §15.6 | A completely full drive: unchecked claim, no words, one lab row | CONFIRMED (LOW) | DM-7 |
| L9/10 LOW-9 | 10 | §15.7 | The SMB lab row must be a real SMB share | CONFIRMED (LOW) | DM-30 |
| L9/10 wording note | 4 | §15.3, §15.4 | `status` drops "again" for the other computer, the backup refusal keeps it | CONFIRMED (LOW) | DM-11 |

Candidates the reviewers tried and refuted (kept as they wrote them): the encmark prefix check
vs the reserved names is not a contradiction (L1/2); the marker name matches no sweep pattern
(L1/2); the run-id route is not disproportionate, since without it Delete again here would break
R3:NF-NF2-RERUN (L1/2); the volume UUID was rightly rejected (L1/2); another account that cannot
read the marker is still refused, by `Lstat` (L1/2); write races, hostile file types, cross-folder
use of the run id and information disclosure (L5/6); no new standing service, bounded alert
volume, no faster log growth, no new hang (L9/10).

## The conditions, and how each is folded

Each is folded into §15 beside the text it changes; the original text stays as it was.

- **DM-1 (versions as they are).** §15.6's first two bullets get a correction beside them: no
  release before 1.5.5 can delete (v1.5.3 and the installer branch hold no `internal/archdelete`).
  A half-deleted folder without the marker comes only from: a marker removed by hand, a share
  hiding dot names, a power cut on FAT/exFAT (DM-12), or a development build of this branch from
  before §15. Older versions matter only as backup takers. New rule in §15.10: the delete feature
  ships in no release without §15's slices 1 to 5 together.
- **DM-2 (the two skipped tests).** Setup changes, checks get stricter: (a) "nothing written" is
  the folder's sorted name list before = after (as `TestABackupWhileADeleteRunsIsToldTheDeleteIsRunning`
  does), with preconditions that the anchor and the marker are both there; (b) the remount test
  uses this computer's own log, asserts as a precondition that the device-and-file-number match
  alone finds no line, and Forbids the "begun on another computer" sentence, so it proves the
  run-id route (MA-1050 stays the main guard); (c) the other-computer test uses a brand-new log
  and asserts the marker-only words in full (DM-11); (d) **the no-marker case stays a test:** the
  two original bodies are kept, still skipped, under new names
  (`TestAnotherComputersBackupIntoAHalfDeletedFolderWithoutItsMarkerIsRefused`,
  `TestABackupIntoAHalfDeletedFolderWithoutItsMarkerOnARemountedDriveIsRefused`), each citing
  §15.6's corrected limit. So nothing is narrowed and no gate reduction needs the operator; the
  reviewers' "operator accepts the narrowing" questions (L1/2 B, L7/8 Q2, L9/10 Q2) fall away.
  §15.9's "closed" gains its condition inline: closed for deletes begun by 1.5.5 or later, seen
  from a computer running 1.5.5 or later.
- **DM-3 (where the marker is written).** Not decided here: it narrows the owner's ruling. §15.2
  keeps step 1 (d) as written and states the alternative beside it (write it just before step 6's
  first removal, under the same lock and root; the crash table's "steps 2 to 5" row then becomes
  "another computer backs up as before"; MA-1046's mutation moves). Owner question 1.
- **DM-4 (the pause seal as a belt).** Recorded and decided for 1.5.5: **not adopted**, reason
  written in §15.6: no shipped version deletes, so the window is the update window; the belt adds
  a second file to the delete's steps (more crash states, a seal the person set before the
  delete, a "keep" path that must know which seal it wrote) and would need these lenses again;
  and on older versions the person sees "Paused" with a Resume button that removes it without a
  lock, so it guards only until someone presses Resume. The owner may overrule in question 3; it
  would then be its own slice with its own review.
- **DM-5 (build slices).** New §15.10: (1) the `delmark` leaf and every name list; (2) the
  refusals (backups, every action, rows, `status`, the writes that skip the check); (3) the delete
  writes and removes the marker, the two tests become guards; (4) finishing from another computer
  (planning from the marker, add-archive, Open, the settings save, Remove from list); (5) the log's
  routes after a remount and the disagreement rule. Readers before the writer; each slice keeps the
  unfiltered `go test ./...` green and has its own adversarial pass; Delete ships only with all
  five.
- **DM-6 (no-anchor re-run after a remount).** §15.5: `planNoAnchor` also accepts a folder whose
  marker names the line's `run` or `first_run` when the identity differs. MA-1050 gains that case,
  prove-fail: drop the marker route from `planNoAnchor`.
- **DM-7 (over-claims reworded).** "the first change to the archive's own files" (the log line
  and the lock file come first); the privacy line says what the marker reveals (that a delete
  was decided, and when); "such a folder could not have its files deleted either" becomes a
  stated possibility (a full drive, a create-but-not-delete ACL, a quota) that fails safe; a
  full-drive cause gets its own words ("The drive is full: free a little space, then click Delete
  again."); the "64 random bits only that delete wrote" line notes clones and restores; a
  completely full drive is a lab row on all three systems.
- **DM-8 (content that feeds a decision).** Beside "content is only for words": the exception is
  `run`, which the log match (§15.5, DM-6, DM-10) and the record use. Trust argument: it decides
  only together with a second proof (path or identity), and a clone or a restore at the same path
  is the same half-deleted archive. The record labels marker facts "as the folder's note says;
  MailArchive cannot confirm it".
- **DM-9 (the marker that never goes).** §15.2/§5.4: the no-manifest re-run never creates a
  marker; when it finds one, it removes it once every name §4 can name is gone, and reports what
  is left as Kept; with a marker present and no anchor, step 6 is proven finished, so the delete
  is complete with those files kept (amends §5.4's "never complete" for this case only). MA-1047
  test: a person's file, a crash after the marker went, Delete again ends with no marker;
  prove-fail: let the re-run create one.
- **DM-10 (path or letter changed).** §15.5: the run-id route also accepts a started line under
  any path when the marker names its run AND the line's recorded identity equals the folder's now
  (Windows letter change); a complete finished line whose started line carries `first_run` R also
  ends R in this log (finishing at the new path clears the old row and uninstall); when the
  marker's run is in this computer's log, the words say the delete was begun here. Path and
  identity both changed (macOS "/Volumes/Name 1") is a stated limit, cleared by finishing at the
  new path. MA-1050 gains the letter-change case; prove-fail: leave out the `first_run` ending.
  Lab row for a drive mounted again under another name or letter.
- **DM-11 (words for the other computer).** When only the marker knows, the backup refusal leads
  with what happened and never says "again"; the encrypted alert's fixed sentence becomes one
  that fits both (builder's words, for the operator; the two MA-1045 tests that pin it are
  updated to pin the new sentence exactly, not loosened). This computer's log case keeps
  MA-1044's words.
- **DM-12 (power loss).** The claim gains its condition inline (folder metadata flushed in order;
  `util.SyncDir` is best effort). Lab row: an exFAT drive pulled right after the marker and the
  first removal.
- **DM-13 (synced folders).** The limit names ordering: another device can receive "marker
  removed" before "manifest removed".
- **DM-14 (writes that skip the check).** `RecordRefusedRun`, `reindex` and the program-copies
  setting ask `delmark.Present` before writing (one line each; `delmark` is a leaf). Tests in
  MA-1048.
- **DM-15 (links at the marker's name on the row).** The row and `status` use an `Lstat` memo of
  their own; MA-1049 covers the row and `status` as well as backups.
- **DM-16 (years of refused runs).** Stated in §15.6; whether to auto-pause is owner question 5.
- **DM-17 (Open an archive folder…).** For a marker folder, the open action itself adds the row
  (`registerArchive`, as `AddRow`) and returns without switching; the page shows the "Added to
  Your archives…" words and does not call the save. MA-1052 tests the real button flow.
- **DM-18 (the settings save on the other computer).** When only the marker knows and the folder
  is this computer's current archive: a save that keeps the location unchanged and installs or
  repairs nothing is allowed (the engine refuses every backup anyway; the program-copies writer
  asks the marker, DM-14). Install, Repair and a save that newly makes the folder current stay
  refused. A delete this computer's log knows keeps MA-1015's refusal unchanged. MA-1052 test.
- **DM-19 (a planted marker cannot delete the person's files).** When only the marker knows and
  there is no anchor, the plan removes only MailArchive's own names: `.mailarchive-*` files, the
  marker, `.mailarchive.lock`, litter, and the log files of this computer's own discovered
  schedules; every other name is Kept. A real such folder never holds the fixed names (step 6
  removed them before the anchor), so nothing is lost. MA-1051 test: a planted marker beside
  README.txt and index.html keeps both.
- **DM-20 (cloud placeholders).** "Regular" for the marker means `util.DescriptorReadable` and not
  `util.IsLinkLike`, in the presence words, step 1 (d) and step 6. MA-1049 gains a placeholder
  case (lab on Windows).
- **DM-21 (who asks what).** §15.3 lists the log-only places: `UnlistRefusal` (MA-1029),
  uninstall's `PendingDeletes`, and `planOf`'s `Finishes` (which reads the marker separately,
  §15.4). The marker check sits outside `RowsCheck`'s `r.log != nil` guard and `DeleteLog`'s
  `!pending` early return.
- **DM-22 (a dead drive or share).** On a lookup error other than "not there", check the folder
  itself: if it cannot be reached, answer "no marker" so MA-689's archive-absent refusal speaks;
  refuse with "could not check" only when the folder is a reachable directory. The re-ask under
  the lock still catches a marker if the folder comes back. Fault-injection test (a seam returning
  "device not ready" and "network name deleted" asserts `archive_absent`); Windows lab row for an
  unplugged USB letter and a dropped mapped drive.
- **DM-23 ("could not check" for every action).** §15.3 states the kind route
  (`RefuseUnfinishedDelete` picks `archive_unwritable` from `delmark`'s typed error), that
  `RefusedAsNoArchive` takes such a run's earlier writes back, and the words for Pause, Resume,
  Switch, Save, Install and Repair ("…so it was not paused", and so on).
- **DM-24 (one owner of the words; the name frozen).** `internal/delmark` holds the name, the kind
  and the marker-only words as constants; the engine's fallback and archdelete use them; the
  fallback has no busy branch and says so (it runs only where no check is installed). The name
  `.mailarchive-deleting.json` is frozen: never renamed; any content, version or file type counts
  as present; a test pins it as the seal's name is pinned.
- **DM-25 (order under the lock).** `app.Run` and `app.RunGraph` ask the delete check before the
  emptied-folder guard under the lock, so a run that queued before the anchor went gets
  "Delete not finished".
- **DM-26 (an empty marker).** Accepted and stated: a re-run never writes into an existing marker
  (opening an existing name to write would follow a planted hard link to its target). The empty
  marker still refuses backups by presence; only the run-id route is lost for it.
- **DM-27 (log and marker disagree).** After step 1 (d) succeeds, the log gets a durable `marked`
  line with the run id. Rule: backups stay refused whenever either says so (fail closed). Delete
  again does not say "This finishes the delete begun DATE" when the log says `marked` but the
  folder holds the anchor with no marker or with another run's marker: the plan says the folder
  does not hold the note that delete left and may be another drive's archive (builder's words),
  then is an ordinary delete under the typed name with its own record lookup (never the started
  line's ID and Spelling, so step 8 cannot forget another vault's key). New catalog row MA-1056 with a test for each
  cause (marker removed by hand, a crash before 1 (d), a rotation drive with the same numbers).
- **DM-28 (an older version backed up after the delete began).** Words only: when the archive's
  last backup is later than the marker's `began`, the plan adds that a backup ran after the delete
  began, probably from a computer with an older MailArchive (both clocks may differ; no decision
  rests on it).
- **DM-29 (rolling the rollout back).** §15.6 and the release notes say: the only rollback is an
  earlier release, which has no Delete and no marker check, so an unfinished delete cannot then be
  finished there and backups go into it; a misfiring marker check is undone only by removing the
  file by hand.
- **DM-30 (honest status of claims and lab rows).** "Proven by §15.7's tests" becomes "to be proven
  by MA-1046 to MA-1050" until built; MA-1055's SMB row names a real SMB share (macOS File Sharing),
  not Parallels shared folders.
- **DM-31 (the planted-marker facts).** §15.6 and question 2 corrected before the owner rules: an
  alert email goes once per failing streak and only if alerts are set up, the Desktop notice once
  per streak, `status` is red only when someone opens it; planting needs only "create" on the
  folder, which can be less than deleting (a sticky-bit folder, a Windows drop-box ACL).

## Questions only the owner can answer

The reviewers' other questions are settled above as technical calls (DM-2 removes the need for a
gate-reduction acceptance; DM-27's words follow the usual "builder's words" approval; the
release-sequencing rule is DM-1/DM-5).

1. **Where the marker is written, and the way out on another computer** (replaces §15.8 Q1; DM-3).
   Recommendation: write it just before step 6's first removal. Until then the archive is whole,
   so another computer's backup harms nothing (the person who began the delete removes what it
   added when they finish), and it spares that computer's person a block they can clear only by
   deleting. It also shrinks "no way to keep the archive" to a crash inside step 6 before its
   first removal; keep "finish it, or remove the marker by hand" for 1.5.5. Blocks: slice 3's write
   position only.
2. **Security acceptance of a planted marker** (§15.8 Q3, with DM-31's corrected facts; DM-19 means
   it can no longer make Delete remove the person's files). Recommendation: accept. Blocks: shipping
   1.5.5.
3. **Older versions** (§15.8 Q4, with DM-4). Recommendation: no seal belt; the 1.5.5 release notes
   say "update MailArchive on every computer that backs up into a shared folder or drive". Also: is
   any development build of this branch that can delete running on your or a tester's computer?
   If so, the notes say to finish those deletes after updating. Blocks: the release notes only.
4. **The computer that began a delete another one finished** (§15.8 Q2, unchanged). DM-10 now fixes
   the same-computer case (finished at a new path). Recommendation as written there. Does not block.
5. **Auto-pause for a delete this computer never began** (DM-16). Recommendation: yes, after the
   same 30 refused runs as an unreachable mail source, so a forgotten delete does not alert for
   years. Does not block slices 1 to 5; a yes adds a small slice.

## What this synthesis did not do

It did not run any test or build. It did not re-run the lenses on the folded text; the
conditions are folded as text beside the original, and the fold verification is owed before
slice 3 (the first slice that changes the delete's steps). It did not check the lab facts listed
under "Claims checked".
