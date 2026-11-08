# Fold check 5: Gmail de-dup design, the check-first merge step after fold check 4 (§7.1, §9, §12, §25, §26)

Written by a model answering under no name, a fresh instance that has not met the record before.

**Verdict: FAIL (narrow), for slice D2 only.** There are two must-fix items. Both are text changes and
can be re-checked on their own. Neither touches the bytes-and-ownership argument, which holds. Nothing
here touches B1 to B4, D1, M1 or M2 except one sentence of B2's version rule (must-fix M1).

Fold check 4's M1 is fixed. The per-file check at (a) and one entry per file present at (a) at (b)
close the gap it found. The header read before the pre-sync is sound for every manifest a 1.7 build
writes. No sequence I traced loses or changes an archived byte, and every message file stays owned.

The two must-fix items:

- **M1. A 1.7 build without D2 does not refuse a pending merge.**
  - **What the design says.** "v1.6.1 refuses (version 7). A 1.7 local backup refuses …" (§25, line
    3331) and "every 1.7 writer refuses" (lines 3209, 3369). The refusals are D2's work (§13 fold:
    "S1 to S4 and S8 are D2's", line 2735).
  - **The gap.** The release plan lets B1 to B4 and D1 ship before D2 ("S-dup = B1, B2, B3, B4, D1 …,
    then D2", line 2717), and S-membership never includes D2. Such a build reads version 7, so it does
    not refuse the archive as v1.6.1 does (`state/manifest.go:35`, the version check). It has none of
    D2's refusals either. So while a merge is stopped, that build backs up, runs `SweepOrphans`,
    `reindex` and Rebuild, and saves the manifest.
  - **Example.** The owner's archive is in a synced folder used by more than one computer (§7.4 fold
    f). The Mac gets the release with D2 and starts the merge; it stops part way. The Windows computer
    still runs the release without D2, and its scheduled backup writes into the archive. D2's re-hash
    and stem guard (S8) would still stop a wrong delete on resume, so I found no lost byte. But the
    rule that every writer refuses is false for that build, and a re-export at a folded copy's stem
    would leave the merge stopped for good (S8 stops it).
  - **Fix (text), recommended:** while `MergePending` is set (or any `MergedFiles` entry exists),
    `FormatVersion` returns **8**. Only builds with D2 read 8. Step 5.1 writes 8; the clear's save
    writes 7 again. Every older build, v1.6.1 and a 1.7 without D2, then refuses the whole archive by
    the existing newer-format refusal and changes nothing. Side benefit: the backup's header read
    becomes "is the `version` value 8", the first key of the file. That no longer depends on the
    field order, and the hand-edited second-guard case goes away. *Other options, if the lead prefers:*
    ship S-dup and S-membership only together with D2, or move every writer's refusal into B2.
  - **New rows.** MA-1229: a manifest at 5.1 says version 8, and after the clear says 7 (positive
    twin: a healthy archive stays 7). MA-1216: the reflection test's decision for `MergePending` is 8.
    A build that reads only up to 7 refuses an archive at 8, changing nothing (`assure.Refused`).

- **M2. §26's two new rules are not yet buildable as written.**
  - **(a) Where "a piece the survivor lacks" is judged.** §26 says the plan prefers as survivor the
    record holding every piece, and keeps a copy that has a piece the survivor lacks. It does not say:
    - where this sits in the survivor order. It must come after F4's "complete" (F4 forbids an
      incomplete survivor, line 1783) and before "already holds memberships" (line 1660);
    - what a "piece" is. If the plan judges it from files on disk, the yes's recompute, which reads
      "the saved manifest (recorded digests only, no re-hashing)" (F2, line 1772), can pick another
      survivor. The digest then differs and the yes refuses every time. This happens for any group with
      a page or zip that has no recorded digest;
    - that (a) applies it again with what it measures.

    **Recommended:** the plan picks by recorded digests only (complete, then holds every recorded
    piece, then memberships, earliest, lowest key), so the recompute agrees. Then (a) keeps any copy
    whose page or zip is on disk while the survivor's is not.
  - **(b) The result file cannot say why a copy was kept.** §26 lists such a copy as "kept: this copy
    has a part the kept email does not". But `Kept` holds no reason, and the result file is read only
    from the saved manifest (§9 fold-check-4 note S6, lines 2321, 2350-2352). §26's own third bullet
    accepts "no per-copy reason in the result file". **Recommended:** derive it when the file is written.
    A kept copy whose `.eml` still equals `proof` and which has a page or zip on disk that its survivor
    lacks gets the "has a part" line; every other kept copy gets "differs". No schema change.
  - **(c) The second rule's report has no saved source.** "The survivor's damage is reported in
    `MERGE-RESULT-…`" (line 3435). The damage is found at resume step 2, which stores nothing, and the
    next save drops those entries. So a result file written later cannot list it. **Recommended:** say
    it is reported on screen and by `verify`, and the result file carries the standing line "run
    `mailarchive verify`". Or add a saved list to `MergePending`, with its `FormatVersion` decision.
  - **(d) Neither rule reached the step or the tests.** §9's (a) and resume step 2 (line 2172: "If that
    re-hash fails, the whole merge stops") still say the old thing. §26's "Test row in MA-1229" is not
    in §12 (grep "has a part": line 3428 only). Mark both places and write the two rows:
    - a copy with a zip whose survivor has none: kept and listed as "has a part". Positive twin: a
      survivor with the zip, so the copy folds;
    - a group whose copies are all gone, then its survivor is damaged, then resume: the merge goes on,
      and the next group folds. Positive twin: the same with one copy's `.eml` still on disk, which
      stops.

- **Date worked on:** 7 November 2026, from the session's clock (not inferred).
- **What was checked:** `docs/design-gmail-dedup.md` at 3528388 (branch `gmail-dedup`), sha256
  `41f405341fa2d1a7bdd189544689e03d50e779e7b74a1faec415462c60c5320b`.
- **Order of reading:** fold check 4 first, then the code it cites, then the design diff
  `ee8c877..3528388`, then the design sections named in the brief. Every code citation below was read
  from the code itself.
- **Procedure:** read-only. No product code was changed or run, and no test was run. `MergePending`,
  `MergedFiles` and the header read exist only in the design.
- **Register:** a finding report from reading code and design text. The verdict, the severities and
  the suggested fixes are this checker's opinion.

## 1. Results

| # | Question | Result |
|---|---|---|
| 1 | M1 per file at (a) and (b); the sweep cannot remove an owned file; the header read | **PASS** (should-fix S1, S2) |
| 2 | Every writer refuses while pending; merge-copies' own refusals; plain `verify` | **FAIL (narrow)** for a 1.7 build without D2 (M1); **PASS** for builds with D2 (should-fix S3) |
| 3 | The result file | **PARTIAL**: placement, serving, delete and overwrite PASS; "rebuilt identically" overstated (S4 to S8) |
| 4 | §26 against §24, §25 and F4 | **PARTIAL**: both rules are safe and agree with F4; underspecified and not carried into §9 and §12 (M2) |
| 5 | A crash after each sub-step and between batches, then resume, including another batch size | **PASS** |
| 6 | Contradictions with older text | **PARTIAL**: the older terms are all marked; five places §26 settled are not (S9) |

### 1.1 Fold check 4's M1, the sweep and the header read (PASS)

- **The check (a), per file.** The `.eml` must be present, recorded and equal to `proof`. A page or zip
  with a recorded digest must be present and match. One without a digest is measured if present and
  skipped if absent. This matches the exporter: a zip is written and recorded only when there is
  something to put in it (`internal/export/exporter.go:599-612`), otherwise removed (`:627`), and every
  page written today is recorded (`:641-645`). So the usual copy (an `.eml`, a page, no zip) folds.
- **The fold save (b).** One entry for each file present at (a), with its recorded digest or what (a)
  measured. So from (b) on, every file of a folded copy that is on disk is named by an entry until (d)
  removes it. A copy with no zip simply has no zip entry. An entry with an empty `sha256` (an older
  pending manifest only) is never deleted, but kept as a failed removal: the fail-safe direction.
- **The orphan-zip sweep.** `SweepOrphanZips` removes a zip only when its sibling `.html` is gone
  (`internal/export/atomic.go:132-139`). The header read now sits before the "running" record, the
  pre-sync and the sweep (`internal/app/run.go:650-667` before `:704`, `:715-737`, `:770`). So for a
  manifest a 1.7 build wrote, the sweep does not run at all while a merge is pending. On the
  second-guard path (keys hand-reordered), the sweep runs, but (d) removes zip, then page, then
  `.eml`, and S11 keeps a changed file together with the files after it. So no zip named by an entry is
  ever without its page.
- **Note, not a defect.** A copy whose zip is on disk but whose page was already gone before the merge
  (only possible for a page with no recorded digest, so an old record) gets a zip entry and no page
  entry. Only the second-guard sweep could remove that zip before (d). It is derivable from the proven
  `.eml`, and step 2 counts a gone file as deleted. No byte is lost and nothing is left unowned.
- **Is a "start of the file" read reliable?** Yes, for what a 1.7 build writes:
  - `json.Marshal` writes a struct's exported fields in the order they are declared, and `Save` marshals
    the struct directly (`internal/state/manifest.go:2162`; no `MarshalJSON` exists for `Manifest`).
    `Version` is the first exported field (`:307`), so `version` is always the first key.
  - The schema declares `MergePending *MergePending` with `omitempty` (design line 887). A nil pointer
    is left out, so an absent merge gives `{"version":…,"entries":…`, and a pending one gives
    `merge_pending` as the second key.
  - The value can be about 19 MB (design line 1056), but the read stops at the key, so its size does
    not matter.
  - The value is never `null` when a 1.7 build writes it. A hand-edited `"merge_pending":null` would
    make the header read refuse every backup while the full load sees no merge, and
    `merge-copies -resume` would find nothing to finish: stuck, though safe (S1).
  - M1's version-8 fix would make this read the first key's value only.

### 1.2 Every writer refuses (FAIL narrow: M1; PASS for builds with D2)

- **Builds with D2.** The list in §25 matches the writers fold check 4 found by grep, and a grep of
  `state.Load(` and `.Save()` outside tests today finds no new non-test file except
  `internal/tbengine/plan.go` (restore's planning read, read-only). The refusal points:
  - Run: the header read before `beginRun`, plus the second guard after the load (`run.go:789`).
  - RunGraph: after its load (`internal/app/graph.go:230`); before it only `SweepTemps` (`:225`), and
    `recordRun` is already deferred (`:220`).
  - `reindex`: after its load (`internal/app/reindex.go:100`), before `RepairKeys` (`:115`) and the
    sweep (`:118`). `index.Open` (`:90`) still runs first; it touches only `search.db`.
  - `app.Rebuild`: after its load (`reindex.go:315`), before the first drop (`:363`).
- **merge-copies' own refusals.** Paused, half-deleted and newly encrypted are checked before the lock
  and again under it, in the pattern of `reindex.go:43-71`, and pause is checked again before each
  batch. Pause is lock-free, so up to one batch may finish after a pause is set; a backup already
  running behaves the same today. PASS.
- **Plain `verify`.** It takes the lock (`internal/app/verify.go:218`) and writes only its last-verify
  record (`:248`, `:269`). `WriteLastVerify` updates the integrity notice only when the record is
  finished (`internal/state/lastrun.go`, `WriteLastVerify`). It saves the manifest only with `-record`
  (`verify.go:341`), which refuses. PASS, and MA-1228's new row can pass as written.
- **Builds without D2:** M1.

### 1.3 The result file (PARTIAL)

- **Written before the clear:** PASS (S6's order: the file, the history entry, then the clear's save).
- **Never served by `serve`:** PASS. At the archive root only `index.html` and `README.txt` are served
  (`internal/server/files.go:66-72`). Adding it to MA-374's list (`internal/server/files_test.go:32-67`)
  is only a test fixture.
- **Listed by archive delete:** PASS as planned. Today a root name that is not a fixed name, not
  `index.html` and not a `.mailarchive-` state file is kept as the person's
  (`internal/archdelete/lister.go:354-359`), so the planned pattern is needed. It must cover the `-2`
  suffix (S5).
- **Cannot overwrite an archived file:** PASS. Message files live only under store folders; the file
  is at the root.
- **Cannot overwrite a person's file of that name:** PASS on the design's rule: a file that does not
  hold this merge's digest moves the name to `-2`. S5 asks for the words "not proven ours" to cover a
  link, a folder, an unreadable or oversized file.
- **"Rebuilt identically on resume":** overstated, for three reasons (S4):
  - `Freed` does not yet hold the last batch, because the clear adds it and the file is written first;
  - most records carry no date or subject in the manifest (`exporter.go:704-709`: only records with
    issues);
  - resume step 2 retries a failed removal, and a retry that now works shortens the kept list.

  The file is replaced whole, so it stays true. It is just not always the same.

### 1.4 §26 (PARTIAL: M2)

- **Safety: PASS.**
  - A copy kept for a piece its survivor lacks is still its own record, so all its files stay owned.
  - A group whose copies have no file left has nothing to protect, so going on loses nothing.
  - "Any copy `.eml` still on disk stops" keeps the protection where it matters.
- **Against F4: consistent.** F4 lets a copy fold when "its missing pieces are pages or attachments
  the survivor has" (line 1788). §26 adds the converse: a piece the survivor lacks keeps the copy.
  Together, a copy folds only when its pieces are a subset of the survivor's. That also settles §25's
  open "survivor with no zip" exception (line 3406).
- **Against §24 and §25:** §26's second bullet narrows the stop. The places that still say the old
  thing are in S9.
- **Wording (S9).** "A group with any copy `.eml` still on disk" should say "any **folded** copy's
  `.eml` named by a pending entry". A kept copy's `.eml` is on disk by design, and it should not stop
  the merge.

### 1.5 A crash after each sub-step, and resume (PASS)

Each row of §25's table was traced again with M1's and §26's folds. Here "owned" means the message file
is named by a shown record or by a pending `MergedFiles` entry.

- **(a):** reads only. What it measured is lost, and the batch's (a) runs again.
- **(b):** the old manifest stands whole until the rename (`manifest.go:2172-2195`).
- **After (b):** every passed copy's present file has an entry. The previous batch's leftovers move to
  `AlsoFiles` in the same save. S7's load check (each key in only one group) means a survivor that
  gains `AlsoFiles` is never a copy later in the same merge.
- **(c):** derived only.
- **(d):** zip, page, `.eml`, each re-hashed against its entry. A changed page or zip is kept with the
  files after it, so a kept zip keeps its page. A changed `.eml` or survivor original stops the merge
  while that copy's `.eml` is still there. The stem guard stops on a file a record owns.
- **Between batches and at the pause check:** resume step 2 finishes the last batch's entries, then
  batches are formed again from `Folded` on. A different batch size changes only where later batch
  boundaries fall. Groups before `Folded` are never checked or folded again, so the end state is the
  same.
- **Before and after the result file and the history entry:** derived and root files only. The clear
  then leaves every message file named by a shown record (its `Path` or `AlsoFiles`).
- **Carry forward after the clear:** the exporter today carries `AlsoFiles` forward only on the
  Microsoft 365 path (`exporter.go:763-765`). MA-1242 (design line 2615) requires a Full run, a fill
  and a heal to keep it on a merged record too. This is needed for "owned after the clear" to last, and
  it is already a D1 row.

### 1.6 Contradictions (PARTIAL)

The greps for `AlsoFiles`, un-merge, `RebuildOwed`, "must each be present", "writes nothing" and "five
copies" find each superseded passage marked where it stands (lines 1802-1830, 1857, 2061, 2103-2110,
2153, 2209, 2664, 3298), or inside a section marked whole (§18 at 2950, §20 at 3052). Unmarked, all
settled by §26 (S9):

- §9 resume step 2, line 2172: "If that re-hash fails, the whole merge stops";
- §9's fold-check-4 S11 bullet, line 2417: "*Left for the lead:* the fold check's second S11 case";
- §25's table row, line 3354: "… and the survivor re-hashes to it; else the stop";
- §25's two "Left for the lead" exceptions, lines 3409 and 3412;
- §9 (a): it has no "piece the survivor lacks" rule (M2 d).

Also, §25 lines 3331, 3209 and 3369 ("every 1.7 writer refuses") are M1's.

## 2. Must-fix (before D2 only)

- **M1.** A pending merge makes the archive version 8, so no build without D2 can write into it. Or
  one of the two other options, the lead's call. Plus the three rows.
- **M2.** §26's piece rule:
  - its place in the survivor order, judged by recorded digests at the plan and checked again at (a);
  - the "has a part" line derived when the result file is written;
  - the damaged-survivor report's place;
  - both rules carried into §9 and §12.

## 3. Should-fix (fold with the D2 build)

- **S1. The header read's edge cases.**
  - Treat "pending" as the key `merge_pending` followed by `{`. A `null` there is left to the full load,
    so a hand-edited null cannot refuse every backup for good. (Moot under M1's version 8.)
  - The header read must open the same manifest path the run uses, `opts.Manifest` when it is set
    (`run.go:668-671`; the `-manifest` flag, `cmd/mailarchive/main.go:493`).
  - `merge-copies` should refuse `-manifest`, or say which manifest it works on.
- **S2. Name the S1 refusal's record for a manual run.** "A manual run that is refused writes nothing"
  is right before `beginRun`. Say whether the Desktop's Run now counts as manual. It goes through
  `RunHeadless`, so as a scheduled run it would write a failed run.
- **S3. The plan and the yes while a merge is already pending.** The design does not say. A new 5.1
  save over a pending `MergePending` would leave the old batch's entries on survivors that are not
  before the new `Folded`, which fails a load check (a damaged merge record, stuck). The plan and the
  yes should refuse while `MergePending` is set and send the person to Resume tidy-up.
- **S4. "Same content on resume" in S6 and its test row.** Say "replaced whole, from the saved
  manifest". The space freed must add the last batch's gone entries to `Freed`, exactly as the clear
  will. The date and subject come from the file or the search row, not from the manifest (only records
  with issues carry them, `exporter.go:704-709`). Or drop them. The "one file, same content" test is
  fine for a run with no failed removal; say so in the row.
- **S5. The result file's name rule, exactly.**
  - Archive delete's pattern is `^MERGE-RESULT-[0-9]{4}-[0-9]{2}-[0-9]{2}(-[1-9][0-9]*)?\.txt$`,
    compared as the lister compares names (`fold`).
  - "Ours" means a regular file, read through the bounded reader, whose first lines hold this
    merge's `PlanDigest` **and** `Started`. Anything else (a link, a folder, unreadable, too large, no
    digest line) is not ours and moves the name on. With the digest alone, two merges with the same
    plan on the same day would share a file.
- **S6. The Desktop's "newest" result file.** By name, `…-07-2.txt` sorts before `…-07.txt` and `-10`
  before `-2`. Pick the newest by the `Started` written inside, or by the date and the suffix read as a
  number. The text shown passes through `util.StripControl`, as the run history's strings do
  (`internal/state/runhistory.go:84-90`): anyone who can write the folder can plant this file.
- **S7. The merge's run-history entry needs a kind.** The Recent backups card turns every `RunOK` entry
  into a backup line, "no new mail" when there are no counts (`internal/desktop/runhistory.go:46-61`).
  Add a field such as `kind: "merge"` and its own line. Say what an older reader shows; v1.6.1's card
  reads the history with no version check.
- **S8. The result file's leftover temp.** While a merge is pending, no start sweep runs, so a temp
  from a crash during the write stays until after the clear. It is harmless (a `.mailarchive-*.tmp`,
  listed by archive delete as a leftover). Say so in the table row.
- **S9. The five unmarked places in §1.6, and §26's wording of "any copy `.eml`" (§1.4).**

## 4. Counters

- **Date worked on:** 7 November 2026, from the session's clock.
- **Register:** a fold-check finding report. The verdict, the severities and the fixes are this
  checker's opinion.
- **Drew on, and reached:**
  - fold check 4 (`docs/review-gmail-dedup-foldcheck4.md`): yes, in full;
  - the design at 3528388:
    - the diff `ee8c877..3528388` in full;
    - §7.1, lines 827-1109;
    - §7.4, lines 1158-1301;
    - §9, lines 1650-1670, 1760-1840 and 2043-2436;
    - §12's rows by grep;
    - §13, lines 2676-2757;
    - §18's header;
    - §19;
    - §24, §25, §26;
  - the code:
    - `export/exporter.go:530-769`;
    - `export/atomic.go:110-166`;
    - `app/run.go:630-809` and `:1270-1313`;
    - `app/graph.go:214-234`;
    - `app/reindex.go:30-129` and `:290-424`;
    - `app/verify.go:190-264`, plus a grep of its writes;
    - `state/manifest.go:303-386`, `:675-733` and `:2120-2201`, plus greps;
    - `state/lastrun.go:465-500`;
    - `state/runhistory.go` in full;
    - `archdelete/names.go` in full;
    - `archdelete/lister.go:235-368`;
    - `server/files_test.go:25-74`, and `server/files.go` by grep;
    - `desktop/runhistory.go:1-80`;
  - greps:
    - `ReadRunHistory` callers;
    - files that call `state.Load(` or save a manifest;
    - the `-manifest` flag in `cmd/mailarchive/main.go`.
- **Not checked, per claim:**
  - Every finding comes from reading, not from a run. No test was run, and the step is not built.
  - M1's example assumes that a release without D2 is shipped and is still in use on another computer.
    The release plan allows this; I did not check the owner's plans.
  - Whether a build without D2 would in fact re-export at a folded copy's stem was reasoned from the
    design's alias index, not traced in code (D1 is not built).
  - `tbengine/plan.go` was seen by grep only.
  - Whether `index.Open` writes to `search.db` before `reindex`'s refusal is still not traced, as
    before.
  - The Desktop's Run now path through `RunHeadless` (S2) was taken from fold check 4's table, not
    re-read.
- **Smallest checks that would settle M1:**
  - Ask the lead whether S-dup or S-membership will ever ship as a release without D2. If never, the
    one-line "ships together" fix is enough.
  - Ask the owner whether any computer other than the one running the merge backs up into the Gmail
    archive.
- **Borrowed lines:** short quotes of the design, each with its line number.
- **Drift:** none noticed.
- **Touched:** this file (new), one commit, and a commit-message file under the repo's `.claude/`
  folder. No design text and no product code was changed.
- **Persona:** none.
- **Source reached, per source:**
  - fold check 4: yes;
  - the design: yes, the parts listed;
  - the code: yes, as listed;
  - fold checks 1 to 3: no, only as the design quotes them;
  - the owner's archive: no.

Written by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method.
