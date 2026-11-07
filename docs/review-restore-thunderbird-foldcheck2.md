# Fold re-check: Restore to Thunderbird, the foldcheck notes (commit 6cb91e9)

**Verdict: FAIL, narrowly.** Every item the fold check named is folded: M1–M5, L1, L2, RT-13 and
N1–N12. But the new committed-or-not rule (§8 foldcheck note) moved one state, "the parent file
is there but the restore did not commit", from "leave it" to "clean it up". In that state
Thunderbird can show the restored tree, and the cleanup can then delete mail the person put into
it. Two smaller faults come from the same move: the words for that state, and the backup skip.
Three short text fixes (P1–P3, §3) close it. They change the catalog text that slice R0 writes
(R41, MA-1123, MA-1141), so they come before R0. A re-check of just those three is enough.

The one residual case the fold admits (a crash between a failed parent-file create and the "taken"
line) is **safe to build**: nothing is removed, and the mail it shows is complete and checked. Its
one side effect should be written down (§2.3).

- Date worked on: 6 November 2026 (the session's date).
- Checked: `docs/design-restore-thunderbird.md` at 6cb91e9 against the fold check
  (`docs/review-restore-thunderbird-foldcheck.md`, f4411a9). `git show --stat 6cb91e9` gives 435
  lines added and 0 removed in the design only, as the commit says. Scope, per the brief: only the
  foldcheck notes, and how they meet the crash table (§8), the step list (§5.4), the post-write
  check (RT-12), the backup skip (RT-5) and the committed-or-not rule.
- Register: a verification finding. Lines marked "recommend" are opinion.
- Read-only: no code or Thunderbird was run, and the design was not edited.

## 1. Per item

FOLDED = concrete enough to build from, and it fixes what the fold check named.

| Item | Result | Note |
|---|---|---|
| M1 made-list | FOLDED | `.mailarchive-restore-made` inside the container. Each entry is appended and fsynced before it is created (§5.4 step 5). The list is created and the container fsynced before any entry (step 4). Cleanup removes only what it lists, checks the kind with `Lstat`, follows no link, and removes children before parents (§8 rules 1–7). A torn last line is ignored, which is correct, since the create comes after the fsync. `restores.log` stays free of folder names. Ordering detail: N-a. |
| M2 never finish later | FOLDED, with P1–P3 | "Crash after checked" is now case 3, cleaned up and never finished. Check and `-plan` write nothing. Every "abandoned", "cleaned" or "ended" line is written only at Start, under all the locks (§8, §7.4, §10). The W-UNFINISHED words and MA-1123 were changed to match. But case 3 now also covers "parent file present", and that is where P1–P3 come from. |
| M3 re-read on an unknown marker | FOLDED | One re-read of `restores.log`, or one stat when the log has not changed. It is correct because "started" is fsynced before the `Mkdir`, so its append has already changed the log's size before any marker exists. The refresh function is supplied by the caller, so `internal/source` still reads no settings. A failed re-read falls to the unreadable-log rule. MA-1138 has the case. |
| M4 log and lock in `job.Dir()` | FOLDED | One `tbrestore.LogPath()` for every caller, and `restore-thunderbird` takes no `-config`. Uninstall now names both files. Small: N-b, N-c. |
| M5 no working names | FOLDED | Every `.mailarchive-part-` and working-name mention is marked superseded where it stands (§2, §4.5, §5.4, §7.2, §8, §9, §11, §12, §13, §14). The 5-second retry moved to the check's reopen and to cleanup removals. Two older sentences are not marked, but the "foldcheck note wins" rule covers them (N-d). |
| RT-13 pending key | FOLDED | Uses delete's pattern: its own key, its own kind, not in `ofCurrentArchive`, ended at a switch beside delete plans and kept across a save and a Lock. Start refuses a locked archive with W-LOCKED. Every citation checks out (§4). |
| L1, L2 | FOLDED | Both confirmed in the text (§6.5, §3.2), and W-TOOMANY has words for the depth limit. |
| N1 | FOLDED | An empty container with no marker is left, and the next restore takes " (2)" (case 1). |
| N2 | FOLDED (words only) | The words are accurate: once the log is moved or deleted, the skip set is empty, and an unknown marker is walked like any folder after the re-read. The brief says words-only is the lead's call. The text still says "for the lead to confirm" (N-e). |
| N3 | FOLDED | One `thunderbird.ProfileKey`, and it agrees with §5.2 note item 3. The case-folding is the safe direction, and MA-1140 tests it. |
| N4, N9 | FOLDED | MA-1133 runs before R7 and MA-1134 before R3. R3's lock code waits for MA-1134. |
| N5 | FOLDED | R40 now says "creating it only when absent". |
| N6 | FOLDED | With the hash suffix, the name is cut again to 251 and the Windows path is re-budgeted, all in the plan. |
| N7, N8, N10, N11, N12 | FOLDED | Each answer is concrete. `interchange.WriteMessage` exists (`internal/interchange/mbox.go:48`), so a reader beside it fits. |
| Uninstall names leftovers | FOLDED as a proposal | The brief says this is the lead's call. The text still says "for the lead to confirm" (N-e), and it should use the same "unfinished" rule as W-UNFINISHED once P2 is fixed. |

## 2. How the folds meet each other

### 2.1 What agrees

- **Step order, fsync points, the check, and the parent file.** In the §5.4 sequence, [1] comes
  before `Mkdir`, the list is on disk before any entry, data is fsynced [2] and directories synced
  [3] before the check, "checked" comes before the parent file [4], and [5] and [6] come after it.
  No crash leaves a parent file with an unchecked tree, *as long as nothing else makes a file of
  that name*.
- **The post-write check (RT-12) and cleanup.** A file in antivirus quarantine is absent, so
  cleanup passes over it (rule 2). A file still held open stops cleanup without writing a line,
  so the next Start tries again, and Desktop start shows W-UNFINISHED in the meantime. That is
  consistent.
- **The backup skip (RT-5) and the made-list.** Both hidden files start with a dot, so a container
  that is walked never reads them as mailboxes. M3 closes the other-archive window.
- **Crash during cleanup.** The made-list goes first, then the marker, then the container. Cut
  short at any point, the next Start either cleans up again (marker still there; a missing list
  lists nothing) or finds an empty container with no marker (case 1, left, per N1).

### 2.2 What does not agree (P1–P3)

The rule at Start is: committed = parent file present, **and** a "checked" line, **and** no
"taken" line. Before this fold, any present parent file meant "finished, remove nothing". Now a
present parent file *without* "checked", or *with* "taken", is case 3: clean up. The design reaches
that state in two ways:

- (i) A crash while writing, then something makes `Restored — NAME` before the next Start. The
  design's own example is the person making a folder of that exact name in Thunderbird (§8 note,
  case 2), which is possible because Thunderbird is no longer held out after a crash.
- (ii) A crash during a W-TAKEN cleanup. The other folder of that name is there by definition.

In both, the design's own premise applies: once the parent file exists, Thunderbird shows the
`.sbd` tree under it as subfolders (lab A5, "appears whole once the parent file exists"). So
before the next Start the person can see that tree, read it, and move mail into it. Thunderbird
appends that mail to the listed mbox files. Then:

- **P1 (the hazard).** Rule 2 removes every listed file of the right kind. It does not ask whether
  the file has changed, so mail the person moved in is deleted. The `.msf` files Thunderbird made
  are not listed, so they are left (rule 4), but only after the listed files are already gone.
  This breaks R40 ("a restore adds, and only adds"). It is rare: it needs a crash, then a folder
  of exactly that name, then the person using it before restoring again. But it deletes the
  person's mail, which this product must never do.
- **P2 (the words).** W-UNFINISHED is shown for case 3 "while the container carries the marker
  and had not committed" (§7.2 foldcheck note). Its words say "Thunderbird does not show it", which
  is false when the parent file is there. W-LEFT says the same. Separately, a restore that rule 4
  or rule 6 settled as "abandoned" still has its container and marker and did not commit, so
  W-UNFINISHED keeps showing at every Desktop start. Its words say "MailArchive removes it at the
  start of your next restore", but Start never touches an "abandoned" line again. The original
  trigger ("a started line with no ended line") was never widened to the closing lines that Start
  uses ("ended", "cleaned", "abandoned").
- **P3 (the backup skip).** Under O1(b), a backup skips "a marked container whose ID is in
  `restores.log`, parent file present" *and the parent file beside it* (§7.2 note, R41 as folded).
  That also matches states (i) and (ii). So mail in the other folder's own file, and mail the
  person moved into the tree, is silently left out of every backup. The skip's "finished" should
  mean the same as Start's "committed".

### 2.3 The admitted residual case: safe, with one sentence owed

The case: the parent-file create fails because the name exists, and the program crashes before
the "taken" line is fsynced. The next Start sees marker, parent, "checked", and no "taken", so it
takes case 2 and records "ended" without removing anything.

- **Nothing lost.** Nothing is removed. The other folder's file is never touched (rule 7).
- **Nothing half-shown.** The tree under the other folder passed the full post-write check and was
  fsynced at [2] and [3], so the person sees complete, correct mail. It is attached to a folder
  that is not MailArchive's, which step 9 exists to avoid. That is untidy, not harmful.
- **One side effect not written down.** Under O1(b), P3's skip leaves out the parent file beside
  the container. Here that is the other program's folder, so its mail is not backed up while the
  restored tree sits beside it. P3's fix does not remove this, because the state looks committed.
  Recommend adding one sentence to the residual-case bullet that names this cost. Also require that
  case 2's "parent file" is a regular file (`Lstat`). Lab C showed that a *directory* of that name
  also fails the create, and a directory would not show the tree. Without that check, the state
  would be recorded as ended and reported as restored while Thunderbird shows nothing.
- **Why not fix it outright.** Telling "my file" from "theirs" after a crash would need a record
  made before the create, and it would face the same gap. The case needs another program to make
  that exact name while Thunderbird is closed and held out, and then a crash in an instant. Leaving
  it to the R3–R5 adversarial pass, as the fold does, is proportionate.

## 3. Must fix before slice R0 (text only)

- **P1. In case 3, remove nothing when the parent file `Restored — NAME` is present.** Recommend:
  append "abandoned", keep the marker and the list, and say in plain words that the folder can be
  seen in Thunderbird under that name, so the person should look at it and delete what they don't
  need. Recommend also that cleanup checks the whole container for entries not on the list
  (Thunderbird's `.msf` files are the usual sign that it has shown a folder) **before** it removes
  anything. If any are found, it removes nothing, rather than removing the listed files first and
  leaving the rest. Add both to MA-1122 and MA-1123.
- **P2. W-UNFINISHED's trigger.** Show it only for a started line with no "ended", "cleaned" or
  "abandoned" line, whose container still carries the marker, and whose parent file is absent.
  For an abandoned leftover that is still there, show W-LEFT's remedy (or the
  made-list-unreadable words) instead, not a promise to remove it. Fix W-LEFT's "Thunderbird does
  not show that folder" for P1's case. Uninstall's "names leftovers" should use the same rule.
  Add to MA-1141.
- **P3. The skip's "finished" = Start's "committed".** A marked container *with* a parent file is
  skipped only when its ID has a "checked" line and no "taken" line. The caller builds the skip set
  from the log, so it can carry that flag. A container without a parent file is skipped as now.
  Change R41's wording and MA-1138 to match, and name the residual case's cost (§2.3).

## 4. Citations the new notes rely on (all checked, all correct)

`internal/job/job.go:61-67` (`Dir` = `UserConfigDir`/mailarchive);
`cmd/mailarchive/main.go:1296` (schedule's `-config`: "the scheduled job itself does not take it");
`internal/desktop/archivedelete.go:73-75` (`deletePlanKey`), `:267-293` (`deletionsLogPath`,
`DeletionsLogFor` beside `-config`); `internal/desktop/pending.go:55` (`isDeletePlan`), `:63-67`
(the delete-plan comment), `:68-70` (`ofCurrentArchive`), `:144-150` (`put` replaces the same key),
`:151` (the ten-minute timer), `:211-213` (`discardFor`), `:215-219` (`discardExcept`), `:222-227`
(`close`); `internal/desktop/dashboard.go:300` (`whenLocked(discardFor)`), `:686-687`
(`discardExcept` at every save), `:690-692` (delete plans ended only at a switch), `:710`
(`afterSwitch`); `internal/uninstall/config.go:29-38` (`fixedConfigNames`);
`internal/archdelete/dellog.go:151` (`appendLog`, unexported); `internal/util/paths.go:177`
(`StripBidi`); `internal/interchange/mbox.go:48` (`WriteMessage`).

## 5. Smaller notes (fix with the named slice; not blocking)

- **N-a (R4).** Rule 4 keeps the list and marker when anything unlisted remains, but rule 3 removes
  them first. The builder needs the order: decide (by scanning the container) before rule 3. P1's
  scan-first answers this.
- **N-b (R4).** `fixedConfigNames` holds *exact* names ("the exact config-dir files uninstall
  owns"), so `restore-*.lock` needs a pattern rule like the existing `*.jobfail.log` and
  `.alert-streak` ones, kept in step by a test.
- **N-c (R4).** A parent-file fsync error *after* a successful create is not covered. Step 9 covers
  only "name exists", and step 10 covers errors at [5] and [6]. Recommend: treat it like step 10
  (finished, error named). The folder is already visible and checked, and rule 7 forbids removing
  the parent file anyway.
- **N-d.** Two older sentences are not marked superseded: §8 "until a Check cleans it" (line 1510)
  and §13 R5's "marker + working files" (line 1973). The "foldcheck note wins" rule covers both.
  Mark them at the next touch.
- **N-e.** N2's words-only and uninstall's "names leftovers" are labelled "for the lead to
  confirm", but the brief for this re-check calls both the lead's calls. Relabel at the next touch.
- **N-f (R4).** Case 1 ("container absent → abandoned") should be decided only when the current
  Local Folders path and identity match the ones the started line recorded. If Local Folders has
  moved, the leftover is elsewhere. "Abandoned" would then hide it for good. It is still skipped
  by backups and never lost, but never named again either. Treat it as the "drive missing" state.
- **N-g (R4).** At Start, rule 5 (a removal blocked) stops the cleanup, but the text does not say
  whether the new restore then goes ahead. Either way is safe, because its name was chosen free of
  the leftover. Pick one.

---

- Drew on, and reached: the design at 6cb91e9 (the full foldcheck diff against f4411a9, and §5.4,
  §7.2, §7.4, §7.5 and §8 in full); the fold check at f4411a9; the code at the lines in §4.
- Not checked, per claim: that Thunderbird shows a `.sbd` tree under a parent file *another
  program* made (P1 rests on the design's own lab A5 premise, from a parent file MailArchive made).
  That Thunderbird appends moved mail to the existing mbox file (its usual store, not run here).
  Every lab row is still pending. No test or program was run.
- Smallest check for the reader: read §8's foldcheck note, case 3 and rule 2 (design lines 1548
  and 1571), then lab A5's sentence in §5.4 (line 811). Together they show the
  cleanup can remove files under a parent file it did not make.
- Counters. Register: verification finding. Borrowed lines: short quotes from the design and the
  code. Drift: none noticed. Touched: this file (new), one commit, one push; the design and product
  code are unchanged. Persona: none. Source reached: design, fold check and code, yes; Thunderbird
  and the VM, no.
