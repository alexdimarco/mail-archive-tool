# Fold check 6: Gmail de-dup design, the version-8 fold and §26's rules after fold check 5 (§7.1, §7.4, §9, §12, §13, §25 to §27)

Written by a model answering under no name, a fresh instance that has not met the record before.

**Verdict: FAIL (narrow), for slice D2 only.** There are three must-fix items. All are text changes
(two sentences and one test row) and can be re-checked on their own in one short pass. The version-8
rule itself holds: no build that reads at most 7 writes a record, an index row or a message file into
a pending merge, and nothing it runs before its refusal removes a file the merge owns. B1 to B4, D1,
M1 and M2 are not blocked; four should-fix items below touch B2 and fold with it.

The three must-fix items, in short:

- **M1. A damaged survivor found in the last batch is never saved, so the result file cannot list
  it.** `Damaged` is written "by the next save that drops that group's entries (the next fold save,
  or the clear)". When the next save is the clear, the clear removes `MergePending` whole, `Damaged`
  with it, and the result file is written *before* the clear from the saved manifest. Fold check 5's
  M2(c) is still open for that case.
- **M2. Resume step 2 can drop an entry whose file is still on disk.** In the "all copies gone" case
  the design says "that group's entries count as deleted". If an outside program removed a copy's
  `.eml` but not its page, the page's entry is dropped while the page stays: a message file owned by
  nothing. §27 says the opposite ("entries whose files are already gone"); the two must agree.
- **M3. The "build capped at 7" test cannot test what it claims.** In the D2 build the header read
  refuses a version-8 manifest before `SweepOrphans`, with or without the cap, so the row's
  "every file … still on disk after the backup's `SweepOrphans` (which runs before its refusal)"
  never runs a sweep. The assertion is empty.

- **Date worked on:** 7 November 2026, from the session's clock (not inferred).
- **What was checked:** `docs/design-gmail-dedup.md` at 00ce013 (branch `gmail-dedup`), sha256
  `f528e97f030285df80a8913a280b052840c820637ac68758280ed29d937d94a3`.
- **Order of reading:** fold check 5 first, then the code (every citation below was read in the code
  itself), then the design diff `f01bba2..00ce013`, then the sections named in the brief. I had not
  seen the design's argument before reading the code.
- **Is today's code v1.6.1's?** Yes, on every path cited: `git diff 0923ffa HEAD` over
  `internal/app`, `state`, `export`, `index`, `archindex`, `archdelete` and `container` is empty.
- **Procedure:** read-only. No product code was changed or run, and no test was run.
- **Register:** a finding report from reading code and design text. The verdict, the severities and
  the fixes are this checker's opinion.

## 1. Results

| # | Question | Result |
|---|---|---|
| 1 | Version 8 while pending: one function, one atomic save, the clear back to 7, every crash point | **PASS** (should-fix S3, S4) |
| 2 | What v1.6.1 and a 1.7 build without D2 do before they refuse | **PASS** (should-fix S5, S6, S7) |
| 3 | The header read and the hand-edited cases | **PASS** (should-fix S1, S2) |
| 4 | §26's rules: survivor order, the piece check, the kept lines, `Damaged` | **PARTIAL**: order, piece check and kept lines PASS; `Damaged` FAIL narrow (M1, M2) |
| 5 | The test seam (a cap on the highest readable version) | **PARTIAL**: the seam is acceptable; the row built on it is empty (M3); a lab row is needed |
| 6 | Contradictions with older text | **PARTIAL**: two unmarked places (S9) |

### 1.1 Version 8 while pending (PASS)

- **The write sites today** (grep of `manifestVersion`, `ManifestVersion` and `Version:` outside
  tests): `newManifest` (`internal/state/manifest.go:701`), the load bump (`:933-934`), `save`
  (`:2155`, both the plain and the sealed path), and a fresh sealed record
  (`internal/container/store.go:166`). `FromSealed` (`manifest.go:757`) and the sealed header
  (`container/record.go:113`, `:174`) only carry what `save` stamped. The copy path
  (`state/units.go:43-61`, `DetachPlaintext`) saves through `Save`. No other code marshals a
  `Manifest`. The design names all four sites. PASS.
- **One atomic write.** `save` stamps `Version`, then marshals the whole struct under the same lock
  (`manifest.go:2149-2162`), then temp file, fsync, rename (`:2172-2199`). `MergePending` and the
  stamp are in the same bytes; no reader can see one without the other. PASS.
- **The clear returns to 7.** A merge needs a `DedupStores` entry, so with `MergePending` gone
  `FormatVersion` is 7. PASS.
- **Every crash point.** Before 5.1's rename: 7, no merge. Every fold save, the result file and the
  history entry: 8 with the merge. Before the clear's rename: 8; after: 7. A failed rename leaves the
  old file. Encrypted archives never hold `MergePending` (F1), so a sealed record is never 8. PASS.
- **`LoadedVersion`.** Only two comparisons exist, `< 3` (`manifest.go:817`) and `< 4` (`:851`);
  8 reads as later in both. The design's "every other comparison is still B2's to list" can be closed:
  there are no others.

### 1.2 What an older build does before it refuses (PASS)

Traced step by step in today's code, which is v1.6.1's on these paths.

**A scheduled backup** (`internal/wizard/wizard.go:211-338`, then `app.Run`):

1. Reads its job file; refuses an unfinished delete, a missing folder, a paused archive, an
   encrypted one (`RefuseKeyless`: it reads the first two keys and scans for `"unit":"`,
   `vault/marker.go:97-154`; a version-8 plaintext manifest passes) and an emptied folder.
2. Opens and appends to `mailarchive.log` in the archive folder (`wizard.go:283-298`).
3. For an Outlook job only, recreates PSTs under `_outlook-pst` in the archive (`:307-327`).
4. In `Run`: the same refusals again, input discovery, then the archive lock (`run.go:640`), the
   under-lock re-checks (`:650-667`), the "running" record (`beginRun`, `:704`), the Thunderbird
   pre-sync (`:715-737`), `SweepOrphans` (`:770`), and only then `state.Load` (`:789`), which refuses
   with `NewerFormatError` (`manifest.go:784-786`).
5. The run is recorded failed (`manifest_unreadable`), the alert streak counts it, and after the
   streak the schedule stops; nothing is sealed.

- **Does any of this remove or change a file the merge owns?** No.
  - The run log, the lock, the run records and the PST exports are the run's own files.
  - The pre-sync touches only Thunderbird's profile.
  - `SweepTemps` removes only `.mailarchive-*.tmp` files and snapshots older than the run
    (`export/atomic.go:119-127`): leftovers, never a merge's live file. On one computer the older
    build takes the lock before its sweep (`run.go:640` before `:770`), so it cannot sweep while
    `merge-copies` runs.
  - `SweepOrphanZips` removes a zip only when its sibling page is gone (`atomic.go:132-139`).
- **Can that sweep find an owned zip without its page at any moment of (d)?** No. (d) removes the
  zip first. A zip removal that fails keeps the page and `.eml` too (S11). After the zip is gone,
  nothing of that copy is a zip.
  - Two computers on a synced folder: the other computer's sync client may apply the Mac's deletes
    in another order (the page before the zip). It then sees a zip with no page and its sweep removes
    it, but the Mac had already deleted that zip. Nothing is lost.
  - One edge, not a defect: a zip whose page was gone *before* the merge (only an old record with no
    page digest). Every older build's backup now sweeps it before refusing, where fold check 5 had
    this only on the hand-edited path. Any backup before the merge would have swept it too, and the
    zip comes from the `.eml`. No byte is lost.
- **"No message file, manifest or index changes": true for the backup.** Nothing before the load
  opens the index or saves the manifest.
- **Other programs:**
  - `verify` takes the lock, then loads (`app/verify.go:218`, `:231`), and refuses before its
    last-verify record (`:248`). It writes nothing.
  - `app.Rebuild` loads (`app/reindex.go:315`) before it touches any index file (`:332`). It writes
    nothing.
  - `archive delete` refuses in its archive test (`archdelete/classify.go:95-96`, words at `:133`).
  - The Desktop card refuses (`archindex/recognise.go:432`). Note: it reports the newer format only
    after scanning the whole file; unknown keys are skipped (`:505-508`, depth limit 64), so
    `merge_pending` scans.
  - `RunGraph` runs only `SweepTemps` before its load.
  - **`reindex` opens the index before it loads the manifest** (`reindex.go:90` before `:100`).
    - v1.6.1 reads index version 3, so it refuses a version-4 index at `index/index.go:162-165`,
      after its pragmas and `CREATE TABLE IF NOT EXISTS` statements (no-ops on an existing file) and
      before any write. It refuses with the index's newer-format words, not the manifest's (§7.4's
      note says "refuse at their loads").
    - A 1.7 build without D2 reads version 4, so `index.Open` passes and writes the header with its
      own value (`index.go:172-182`, the read-only probe) before the manifest refusal.
    - No row changes. Whether `search.db`'s bytes are unchanged depends on WAL checkpointing; the
      design's byte-compare row will settle it. This closes the "not traced" item carried since fold
      check 3 (S6).

### 1.3 The header read and the hand-edited cases (PASS)

- **Reliable for every manifest the code writes.** `Version` is the first exported field
  (`manifest.go:307`), `save` marshals the struct directly (`:2162`), and older pretty-printed files
  also start with `version`. The repo already reads the first key this way:
  `vault.ManifestIsPlaintext` (`vault/claims.go:32-55`) and `manifestSaysEncrypted`
  (`vault/marker.go:116-140`). D2 should reuse that bounded first-key reader (S2).
- **Hand-edited files:**
  - `version` missing, not first, or a string: the header read passes, and the full load decides.
    - With a `MergePending`, the D2 build's second guard refuses after the load (no save).
    - A missing `version` decodes as 0, and the load then runs the version-1 migration in memory;
      nothing is saved before the refusal.
    - A string makes `json.Unmarshal` fail with today's "corrupt" words (`manifest.go:727`). That is
      existing behaviour, for a hand edit only.
  - Above 8: refused by the header read with the newer-format words.
    - An encrypted archive's file holds version **100** (`vault/envelope.go:13`). It never reaches
      the header read: the read runs only when `opts.Key == nil` (fold-check-4 S1's place), after
      `RefuseKeyless` (`run.go:664`), which refuses version 100 or above (`vault/marker.go:133`).
    - The fold-check-5 note restates the read but not that condition, and no row tests it (S2).
- **The lead's calls:**
  - **B2 refuses a decoded `MergePending` whatever the stamp:** sound. A `null` decodes to nil and
    is not refused, which matches "null as absent".
  - **Stamp 8 with no `MergePending`:** sound for the backup (the header read refuses) and for
    `merge-copies -resume`. But D2's other writers refuse only "while `MergePending` is set" (S3 of
    fold check 4). A D2 `reindex`, `app.Rebuild`, `verify -record` or Microsoft 365 run would load
    such a file, see no merge, and save it at 7 (S1). Nothing is deleted, and the leftover
    `MergedFiles` stay owned. But "a damaged merge record; every writer refuses" is then not true.
  - **Above 8 refuses with the newer-format words:** sound, given the place above.

### 1.4 §26's rules (PARTIAL: M1, M2)

- **The survivor order: PASS.**
  - Complete, then "records every piece any record of the group records" (by `Fixity.HTML` and
    `Fixity.Zip`), then memberships, earliest, lowest key.
  - Every input is recorded data: F4's completeness, the digests, `ExportedAt`, the key. The plan and
    the yes's recompute (F2: recorded digests only) therefore pick the same survivor and reach the
    same digest.
  - When no complete record records every piece, step 2 is skipped. The text says "the best survivor
    by this order"; it reads that way, and both sides apply it alike.
  - I found no case where the plan and the yes disagree for good.
- **The piece check at (a): PASS.** Judged from the disk, and it can only keep a copy, never delete
  one. A copy folds only when every piece it has on disk the survivor also has on disk. Every piece
  but the `.eml` comes from the `.eml`, which equals `proof`, so no unique byte can go. Two notes:
  - The survivor's piece is checked only for presence, so a broken unrecorded survivor zip still lets
    a copy fold. The copy's zip is rebuilt from its `.eml`.
  - The plan's "space freed" cannot know which copies (a) will keep, so it is an upper bound (S8).
- **The two kept lines: PASS.** They are worked out from the disk when the file is written, need no
  schema change, and are true when written.
- **`Damaged`: FAIL narrow.**
  - **M1.** The order after the last batch is "the result file, the run-history entry, then the
    clear's save" (design line 3651). `Damaged` is written only "by the next save that drops that
    group's entries (the next fold save, or the clear)" (§7.1 fold-check-5 note, lines 1140-1144;
    §9, lines 2676-2681).
    - When resume step 2 finds the damage in the **last** batch's entries, no fold save follows. The
      clear's save removes `MergePending`, `Damaged` with it. The result file, written just before
      from the saved manifest, does not list the damage.
    - A crash between the result file and the clear repeats the same miss.
    - The row (line 2947) tests only the middle-batch case ("the next group folds").
    - Fold check 5's S4 found the same shape for `Freed`.
  - **M2.** Step 2's new rule says "that group's entries count as deleted (their sizes added to
    `Freed`)" (line 2676) whenever no folded copy's `.eml` named by an entry is on disk.
    - Normal running cannot reach this: (d) removes the `.eml` last, and a failed or changed page
      keeps the `.eml`.
    - An outside program can (the design guards outside writers elsewhere: S11, the stop). It removes
      a folded copy's `.eml` after the fold save and leaves its page. The page's entry is then dropped
      while the page is on disk: a message file owned by nothing, and `Freed` counts bytes that were
      not freed. That fails test (2) of §23.
    - §25 also says a copy's page is deleted only while its `.eml` is on disk, so the page cannot be
      deleted either.
    - §27 (line 3857) states the right rule ("entries whose files are already gone"); §9 must say
      the same.

### 1.5 The test seam (PARTIAL: M3; lab row needed)

- **Is a version cap fault injection?** In my reading, yes, close enough to sit inside the repo rule.
  - It mocks no core logic. The real loader, the real refusals and the real files run; only the
    highest version the build accepts is lowered, to stand in for an older reader.
  - The repo already has seams of this kind, for example `manifestStat` (`archdelete/classify.go:40`).
  - Keep it to one unexported variable in package `state`, read by all three comparisons
    (`manifest.go:784`, `container/record.go:168`, `archindex/recognise.go:432`), set only by a test
    helper that restores it.
  - Whether it counts as fault injection is finally the operator's call under the repo rule.
- **What it does not simulate (M3).** A capped D2 build is not a build without D2:
  - its header read still refuses 8 before the running record, the pre-sync and `SweepOrphans`
    (8 is above the cap, so it refuses with the newer-format words);
  - its S3 refusals and its version-8 words are still there.
  So the row's sweep clause (line 2946) cannot run, and "the only files the backup writes are its
  own last-run record and run-history entry" describes a different order from a real older build's.
- **A lab row with the real binaries is needed.** The claim is about shipped programs on another
  computer, and the sweep-before-refusal order exists only in them (and in B2's own build, before D2
  lands). The local lab can run it: a stamped-8 fixture, the v1.6.1 binary, and any shipped 1.7 build
  without D2, on the Windows VM and the Mac. Run a scheduled `run-job` with the Thunderbird pre-sync
  on, `reindex`, `verify`, the Desktop card and archive delete. Before and after each, compare the
  bytes of the manifest, `search.db` and every message file, and check that every file a
  `MergedFiles` entry names is still there.

### 1.6 Contradictions with older text (PARTIAL)

Grepped: "version 7", "at version 7", "read back as", "every 1.7 writer", "writes nothing",
`AlsoFiles`, "five copies". Everything fold check 5 listed is now marked. Still unmarked (S9):

- GD-2, line 107: "the merge's first durable act is a manifest save at version 7";
- §7.4's first bullet, line 1233: "`manifestVersion` becomes 7" (already replaced by the F3 note at
  line 1335, which is marked, but the bullet itself is not).

§18 is marked whole (line 3270). The remaining "writes nothing" lines are about the plan or a refused
run, and are still true.

## 2. Must-fix (before D2 only)

- **M1. Give `Damaged`, and the last batch's `Freed`, a saved source before the result file.**
  Recommended, one sentence in §9's fold-check-5 note and §25's step line: *after the last batch's
  (d), and after resume step 2 whenever it drops or moves an entry, one more save (still version 8)
  drops the gone entries (adding to `Freed` and `Damaged`) and moves kept ones to `AlsoFiles`; then
  the result file, the history entry, the clear.* This also settles fold check 5's S4 for `Freed`.
  Row: the damaged-survivor case in the **last** batch, and a crash between the result file and the
  clear; the result file lists the survivor both times.
- **M2. Resume step 2's "none on disk" rule drops only entries whose files are gone.** A page or zip
  still on disk is kept, moves to the survivor's `AlsoFiles` at that save (as a failed removal does),
  and is listed in the result file; its size is not added to `Freed`. Row: an outside removal of a
  folded copy's `.eml` with its page left, then a changed survivor original, then resume. The merge
  goes on, the page is on disk and in `AlsoFiles`, and ownership check (2) holds.
- **M3. Rewrite MA-1229's "build capped at 7" row so it tests something real:**
  - B2 carries the rows against its own build, a real build without D2, with a version-8 fixture
    written by the test: backup, `reindex`, `app.Rebuild`, `verify -record`, archive delete and the
    card each refuse; `SweepOrphans` really runs first; the owned files are still there.
  - After D2 raises the cap, the same rows run with the seam and call `export.SweepOrphans` directly,
    standing in for the older order, as the other rows already do.
  - Add the lab row of §1.5 with the real v1.6.1 binary.

## 3. Should-fix (fold with B2 or the D2 build)

- **S1. S3's refusal rule must also cover a damaged merge record** (B2/D2). That includes a file
  stamped 8 with no `MergePending` while entries wait. Say: every writer refuses when `MergePending`
  is present **or** the load marked the merge record damaged. Otherwise a D2 `reindex` saves such a
  file at 7.
- **S2. The header read's place and parser** (D2):
  - restate in the fold-check-5 note that it runs only when `opts.Key == nil`, after `RefuseKeyless`
    (`run.go:664`);
  - add a positive twin to the header-read row: a keyed backup into an encrypted archive (version
    100) runs, and a keyless one gets `RefuseKeyless`'s words, not the newer-format words;
  - reuse the first-key reader of `vault/claims.go:32-55` rather than a new parser.
- **S3. The sealed-field pin test needs a new entry at each bump** (B2 and D2).
  `TestRecordFieldsArePinnedToTheRecordVersion` (`container/adversarial_test.go:194-210`) looks up
  `pinned[state.ManifestVersion]`. B2's 7 and D2's 8 each turn it red until a list is pinned for that
  number; the 8 list is the same as the 7 list.
  - This is the test's own documented step ("bump … and pin the new list here").
  - It still edits a pinned list, so name it in §13 for the operator's acceptance.
- **S4. `FormatVersion` runs inside `save`, under the manifest's lock** (`manifest.go:2149-2155`).
  It must not take that lock itself; an unexported variant for use under the lock will do (B2).
- **S5. List everything an older build's backup writes before refusing** (§7.4 and §27 notes, and
  MA-1229):
  - the archive lock;
  - `mailarchive.log` (`wizard.go:283-298`);
  - for an Outlook job, the PSTs under `_outlook-pst` (`:307-327`);
  - the pre-sync's own records;
  - the last-run record and run-history entry.
  Or limit the row's "only files" clause to `app.Run` called directly.
- **S6. `reindex` opens the index before its manifest load** (`reindex.go:90` before `:100`).
  - Correct §7.4's note: v1.6.1's `reindex` refuses at its index open, in the index's words.
  - Recommended in B2: move `reindex`'s manifest load, or the header read, before `index.Open`, so a
    build without D2 does not write the index header before refusing. Keep the byte-compare row
    either way.
- **S7. The version-8 words are written in more than one place.** The words live in four places:
  - the loader (`manifest.go:712`);
  - archive delete (`archdelete/classify.go:133`);
  - the Desktop card's rendering;
  - the backup's failure record.
  "One comparison in the refusal's text" undercounts. The row should name which places show the
  tidy-up words in B2.
- **S8. The plan's "space freed" is an upper bound** once the piece check can keep copies at (a).
  Say so in the plan's words.
- **S9. Mark the two places in §1.6** (GD-2 at line 107; §7.4's first bullet at line 1233), and close
  "every other `LoadedVersion` comparison is B2's to list": there are only `:817` and `:851`.

Fold check 5's S1 (other bullets) to S8 stand as the design files them in §13; none is repeated here.

## 4. Counters

- **Date worked on:** 7 November 2026, from the session's clock.
- **Register:** a fold-check finding report. The verdict, the severities and the fixes are this
  checker's opinion.
- **Drew on, and reached:**
  - fold check 5 (`docs/review-gmail-dedup-foldcheck5.md`, f01bba2): yes, in full;
  - the design at 00ce013:
    - the diff `f01bba2..00ce013` in full;
    - §7.1's notes (lines 1030-1180);
    - §7.4 (lines 1231-1460);
    - §9's fold-check-4 note on the header read and S2 (lines 2355-2454) and the fold-check-5 note
      (lines 2611-2694);
    - §12's MA-1229 rows by grep;
    - §13's fold-check-5 note;
    - §18's header and table;
    - §25's step and table (lines 3516-3700);
    - §26 and §27;
  - the code:
    - `state/manifest.go:296-365`, `:695-794`, `:920-941` and `:2120-2201`;
    - `state/units.go:30-62`;
    - `app/run.go:535-809`;
    - `wizard/wizard.go:211-340`;
    - `app/reindex.go:30-129` and `:285-374`;
    - `app/verify.go:185-269`;
    - `app/encguard.go:88-113`;
    - `index/index.go:101-186`;
    - `export/atomic.go:80-167`;
    - `archindex/recognise.go:150-170`, `:395-436` and `:461-515`;
    - `archdelete/classify.go:40-199`;
    - `vault/envelope.go`, `vault/claims.go:30-55` and `vault/marker.go:95-154`;
    - `container/adversarial_test.go:140-211`;
  - greps:
    - every write and comparison of the manifest version;
    - `LoadedVersion`;
    - `json.Marshal(m)` outside tests;
    - `View{` constructions;
    - the design's older phrases;
  - `git diff 0923ffa HEAD` over the cited packages (empty) and `git merge-base`.
- **Not checked, per claim:**
  - Every finding comes from reading, not from a run. No test was run, and D2 is not built.
  - That `search.db`'s bytes stay unchanged after `index.Open`'s header write in a build without D2
    was reasoned from SQLite's WAL mode, not measured (S6).
  - The shipped v1.6.1 binary was not run. Its behaviour is inferred from source identical to
    0923ffa (the reason for M3's lab row).
  - Archive delete's order after its archive test (whether any marker is written before the
    classify refusal) was not traced beyond `classify.go`.
  - The Desktop card's words for `NewerFormatError` were not traced past `recognise.go:432`.
  - `lockfile`'s stale-lock rule was not read. I assumed a live merge's lock is never taken over.
  - Whether the owner's archive will ever be backed up by a build without D2 while a merge is pending
    was not asked.
- **Smallest checks that would settle what I could not:**
  - Run MA-1229's byte-compare for `reindex` in B2's build (S6).
  - Ask the operator whether the version cap counts as fault injection under the repo rule (§1.5),
    and whether the pinned-list addition (S3) is accepted.
- **Borrowed lines:** short quotes of the design and of code comments, each with its line number.
- **Drift:** none noticed.
- **Touched:** this file (new), one commit, and a commit-message file under the repo's `.claude/`
  folder. No design text and no product code was changed.
- **Persona:** none.
- **Source reached, per source:**
  - fold check 5: yes;
  - the design: yes, the parts listed;
  - the code: yes, as listed;
  - fold checks 1 to 4: no, only as the design and fold check 5 quote them;
  - the v1.6.1 binary and the owner's archive: no.

Written by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method.
