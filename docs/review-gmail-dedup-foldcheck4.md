# Fold check 4: Gmail de-dup design, the check-first merge step (§7.1, §9, §12, §13, §22 to §24)

Written by a model answering under no name, a fresh instance that has not met the record before.

**Verdict: FAIL (narrow), for slice D2 only.** There is one must-fix item. It is a text change to the
check-first note's steps (a) and (b), and it can be re-checked on its own. Nothing here touches B1 to
B4 or D1.

Check first, then fold, then delete is sound. No sequence I found loses a byte of a survivor's
original or of a kept copy's file. No resume folds a group twice or deletes a file that now differs.
The fourteen should-fix items below are wording, named refusal points, or small product calls.

The one must-fix:

- **M1. A copy's page and zip, per file, at the check (a) and the fold save (b).**
  - **The check.** Step (a) says a copy's "page and zip must each be present and match their recorded
    digests (or have no digest recorded, S2)". Most emails have no attachments, and for those the
    exporter writes **no zip at all** (`internal/export/exporter.go:599-627`: a zip only when
    `hasArchivable`, otherwise `os.Remove(zipPath)`; `fx.Zip` stays empty, `:612`). Read literally,
    every copy without attachments fails the check and is kept. That also contradicts F4 and §19, which
    say a copy that is missing pieces may still fold into a complete survivor (design lines 1737-1739
    and 2697-2699).
  - **The fold save.** Step (b) makes the copy's entries "copied from the copy's `Fixity`". A page or
    zip with no recorded digest (S2's case: `Fixity.Zip` is empty, `manifest.go:273-277`, while the
    file is on disk) then gets no entry.
  - **Example.** A copy has a zip but no recorded zip digest. Its record is removed at (b), so from
    that save on its zip is owned by nothing. Step (d) deletes only files that entries name, so the zip
    stays while the page goes. That leaves a zip with no page. The next backup attempt's
    `SweepOrphans` (`internal/app/run.go:770`, which runs before that backup's refusal) then removes it.
    No unique byte is lost, because the zip can be rebuilt from the proven `.eml`. But test (2) of §23
    fails ("every file owned"), and so does §23's "the merge never leaves a zip without its page".
  - **Fix (text).** At (a), check each of the copy's three files by its file stem (the file name
    without its ending):
    - the `.eml` must be present, have a recorded digest, and match it;
    - for the page and the zip: if a digest is recorded, the file must be present and match it; if no
      digest is recorded and the file is present, record its size from disk (S2); if no digest is
      recorded and the file is absent, there is nothing to check.

    At (b), make one entry for each file that was present at (a), with an empty `sha256` for an S2 page
    or zip.
  - **New MA-1229 cases.**
    - A copy with no zip folds and its page and `.eml` are deleted. Positive twin first: a copy with a
      zip.
    - A copy whose zip has no recorded digest: a crash after (b), then a backup attempt. The zip is
      still on disk and is named by a `MergedFiles` entry. After resume it is deleted before the page.

- **Date worked on:** 7 November 2026, from the session's clock (not inferred).
- **What was checked:** `docs/design-gmail-dedup.md` at 9dc5df7 (branch `gmail-dedup`), sha256
  `179d73e2e3c88994817875cad958a5189a5b4f38b489e5950d55a9125e12cca2`.
- **Order of reading:** fold check 3 first, then `internal/app/reindex.go` in full. Then, out of the
  order the brief asked for, the design diff `git diff affab63..9dc5df7`, then the rest of the code.
  So I had seen the design's argument before most of the code. Every code citation below was read
  from the code itself, not from the design.
- **Procedure:** read-only. No product code was changed or run, and no test was run. The design
  changes nothing in code yet: `MergePending`, `MergedFiles` and `RebuildOwed` appear nowhere under
  `internal/` or `cmd/` (grep).
- **Register:** a finding report from reading code and design text. The verdict, each item's
  severity and the suggested fixes are this checker's opinion.

## 1. Results

| # | Question | Result |
|---|---|---|
| 1 | A crash after each of (a) to (d) and between batches, then resume: no byte lost or changed; every file owned | **PARTIAL**: bytes PASS; ownership fails for a page or zip with no recorded digest (M1) |
| 2 | Every writer refuses while `MergePending` is set, before any sweep or delete | **PARTIAL**: the routing is right and `app.Rebuild` is covered; `SweepOrphans`'s safety depends on M1; Run's pre-sync comes before the refusal (S1); merge-copies' own entry refusals are not named (S2) |
| 3 | A differing copy and a damaged survivor's group are untouched | **PASS** (one should-fix: S10) |
| 4 | The stop: whole merge, before the clear, nothing more deleted, files owned; resume cannot delete a changed file | **PASS** (two should-fix: S11, S12) |
| 5 | Resume: `Folded`, `Kept`, `SurvivorChanged`; a damaged `MergePending` stops it | **PASS** (should-fix: S5, S6, S7) |
| 6 | Batches of whole groups, about 5,000 copies each | **PASS** (the "five copies" bound has no source: S5) |
| 7 | Encrypted archives never reach any of this | **PASS** (one should-fix: re-check under the lock, S2) |
| 8 | Contradictions left in superseded text | **PARTIAL**: M1 contradicts F4 and §19; three unmarked leftovers (S13) |

### 1.1 Crash after each sub-step (PARTIAL)

Each row of §23's table was traced against the code.

- **Manifest saves.** Each save is a temp file, fsync, rename (`internal/state/manifest.go:2172-2195`),
  so a half-written (b) leaves the old manifest whole. Note: `Save` always stamps the current version
  (`:2155`).
- **(a)** reads only. **(c)** writes only derived rows and pages.
- **(d)**, for one copy, in order: zip, then page, then `.eml`, each re-hashed just before it is
  removed. A crash between any two leaves no zip without its page, but only for files that have an
  entry (see M1).
- **Between batches.** Batch N's entries stay on its survivors until batch N+1's fold save. So a crash
  anywhere before that save brings resume's step 2 back to batch N's pending entries, and a file already
  gone is counted by S1's rule. A crash after batch N+1's (b) finds N's entries already dropped or moved
  to `AlsoFiles` in that same save.
- **Bytes: PASS.** A copy's `.eml` is removed only after it and the survivor's original were both
  re-hashed equal to `proof`. A survivor's files and a kept copy's files are never named by an entry.
  (d) refuses any record's own `Path` and any `AlsoFiles` file.
- **Ownership: FAIL for S2 files (M1).** The rest holds: the fold save names a passed copy's files in
  the same save that removes its record.
- **Note (not new to this design).** The directory fsync after the rename is best effort
  (`manifest.go:2196-2198`). If power is lost after (d)'s unlinks were persisted but before (b)'s
  rename was, the old manifest's copy records would name deleted files. On resume, (a) then puts those
  copies in `Kept` (their `.eml` is missing). No unique byte is lost; verify reports them missing. This
  is the repo's standing durability position, not a hole in the step.

### 1.2 Every writer refuses (PARTIAL)

Every writer was found by grep for `state.Load`, `state.LoadFrom` and `.Save()` outside tests.

| Writer | Where it loads | What runs before the refusal point | Result |
|---|---|---|---|
| Local backup: CLI, Desktop Run now, `RunHeadless` (`wizard/wizard.go:211`, through `wizard/jobrun.go:90,222` and `:1839`), wizard | `app/run.go:789` | the lock (`:640`), the "running" record (`:704`), the Thunderbird pre-sync (`:715-737`), `SweepOrphans` (`:770`) | safe for bytes given M1; the pre-sync is S1; the refusal line is not named in "Where the refusals sit" (S1) |
| Microsoft 365: `HeadlessCapture` (`desktop/capture.go:159,464`), `graph` (`main.go:1866`) | `app/graph.go:230` | `SweepTemps` only (`:225`): temp files | PASS; refusal line not named (S1) |
| `reindex` | `app/reindex.go:100` | `index.Open` (`:90`); the refusal is placed before `RepairKeys` (`:115`) and `SweepOrphans` (`:118`) | PASS |
| `app.Rebuild`: `reindex -rebuild` (`cmd/mailarchive/main.go:852`), Desktop button (`desktop/maintenance.go:236`), wizard (`wizard/wizard.go:1292`) | `reindex.go:315` | refusals and the lock only; the first drop is `:363`, `removeDBFiles` is `:332` | PASS: one refusal covers all three callers |
| `verify -record` | `app/verify.go:231` | the lock (`:218`) | PASS |
| plain `verify` | `verify.go:231` | — | read-only for archive files, but it writes its last-verify record (`:248`, `state/lastrun.go:486-491`); S3 |
| Encrypted keyed runs | `openEncrypted` (`app/encrypted.go:432-444`) | key check and scratch removal (`:426-430`), no archived file | PASS (as fold check 3) |
| Archive delete | `archdelete/classify.go:186` | — | listed in S3 |
| Encrypt conversion, decrypt copy (`app/decryptcopy.go:335`) | — | — | listed in S3 |
| Program setting, pause note, sealed index (`app/programsetting.go:56`, `pausenote.go:45`, `sealedindex.go:174-386`) | through `openEncrypted` | — | encrypted only: never reached |
| Restore to Thunderbird (`tbengine/run.go:144,201`), extract (`app/extract.go:168,188`) | under the archive lock | — | read records only; §24 leaves the trace to D2; fine |
| Desktop copy export / extraction workspace (`app/copyexport_archive.go:369,1229`) | `LoadFrom`, a snapshot, no lock | — | reads only; not in §24's trace (S14) |
| `pause` / `resume` (`seal/seal.go:72-96`) | no manifest | writes only the sentinel, lock-free | needs no refusal; merge-copies must refuse a paused archive (S2) |
| `uninstall`, `unlist-archive`, `add-archive`, key verbs | no archive manifest write | — | none needed |
| `status`, `serve`, Desktop card reads (`health/health.go:1905`, `cmd/mailarchive/status.go:84`, `server/source.go:98`, `desktop/localarchive.go:43,292`, `desktop/localschedule.go:56,490`) | read-only | — | none needed |

No writer of a manifest file was missed. The gaps are where refusals sit and what runs first (S1,
S2), plus M1, on which `SweepOrphans`'s safety rests.

### 1.3 Untouched groups (PASS)

- **A copy in `Kept`.** Its record is not touched at (b). Its search row is not dropped at (c). No
  entry names its files, so (d) cannot reach them. Its folder page is regenerated from an index that
  still lists it (derived).
- **A `SurvivorChanged` group.** Every copy keeps its record, and the survivor gains no membership.
- **Should-fix S10.** The survivor's page and zip are never re-hashed, at (a) or at (d).

### 1.4 The stop (PASS)

- **Nothing more is deleted** after a differing re-hash in (d). `MergePending` stays set, and the copy's
  remaining files stay named by their entries.
- **No writer can remove those files while the merge is stopped**:
  - `reindex` refuses before its sweep;
  - `Rebuild` refuses before its first drop;
  - archive delete refuses;
  - a backup's `SweepOrphans` finds no zip without its page (given M1).
- **Resume cannot delete a file that now differs.** Step 2 runs (d) again on the pending entries, and
  (d) re-hashes each file just before removing it. The window between that re-hash and the delete is
  open only to an outside program (§23's named exception).
- **Two should-fix items:** S11 and S12.

### 1.5 Resume logic (PASS)

- **No double fold.** Only fold saves write `Folded`, `Kept` and `SurvivorChanged`, in the same save
  as the fold, and resume never runs (a) or (b) on a group before `Folded`. So no group is folded twice.
- **No skipped check.** A passed copy was checked at (a) and is re-checked at (d). A kept copy is never
  deleted, so it needs no second check.
- **No lost `Kept` entry while pending.** Every later save carries `MergePending` forward.
- **The load checks catch a reorder.** A group moved across the `Folded` line still has copies that
  are records but not in `Kept`.
- **Gaps:** S5 (where batches start counting), S6 (the result is lost at the clear), S7 (a key in two
  groups).

### 1.6 Batches (PASS)

"Whole groups in order until at least 5,000 copies" can never split a group: a group larger than
5,000 would be a batch of its own. The arithmetic holds:

- 73,915 copies is 14 full batches and a last of about 3,915;
- 17 saves in all: step 5.1, 15 fold saves, the clear.

"The largest has five copies" has no source in §10.2 (it gives counts, not group sizes). It is a
claim, not a measurement. It changes only the upper size of a batch, not correctness (S5).

### 1.7 Encrypted (PASS)

- F1's refusal comes from the encrypted envelope, before any key is used.
- Conversion refuses while `MergePending` is set, and the sealed-merge refusal sits before
  `prepareEncrypted` (`encrypted.go:439-444`).
- A conversion that finishes between the plan and the yes is caught by accident: the plaintext
  manifest is gone, so `state.Load` returns an empty manifest (`manifest.go:681-682`), and the
  recomputed digest differs. This should be a named check under the lock, as `run.go:661-667` does
  (S2).

### 1.8 Contradictions (PARTIAL)

Grep run for `AlsoFiles`, un-merge, `RebuildOwed`, `Before`, survivor, 5.5, 5.6 and the anchor save.
The superseded blocks (§7.1 fold-check-2, the §9 fold-check-2 note, §20, §21) are marked where they
stand. Left over:

- **M1's clash** with F4 (lines 1737-1739) and §19 (lines 2697-2699).
- **§12's base MA-1229 row** (line 2236) and the re-scoped row (line 2292) still say "a crash at each of
  steps 5.1 to 5.6". There are no 5.2 to 5.5 steps now. Mark both and point to the check-first rows.
- **§18's "Paths outside the step", the survivor bullet** (lines 2642-2648), carries the M2 un-merge
  bracket. The section's check-first note (line 2620) names only the table's step order. Mark the
  bracket superseded.
- **§19's "A copy missing pieces" bullet** (line 2697) says "step 5.5 re-hashes". It should say
  (a) and (d).

## 2. Must-fix (before D2 only)

- **M1.** As in the verdict: per-file presence and digest rules at (a); one entry per file present at
  (a), with an empty `sha256` for an S2 page or zip, at (b); the two MA-1229 cases.

## 3. Should-fix (fold with D2)

- **S1. Name the backup refusals, and put them before the pre-sync.**
  - Name Run's refusal right after `run.go:789` and RunGraph's right after `graph.go:230`.
  - In Run, the Thunderbird pre-sync (`:715-737`) runs before the load. While a merge is stopped, every
    scheduled backup would start or quit the person's Thunderbird and then refuse. Recommended: a
    cheap read of `MergePending` before `BeforeRead`. The "running" record (`:704`) still lets a
    refused run count toward the alert streak, as §24 wants.
- **S2. merge-copies' own entry refusals.** The plan, the yes and resume should refuse, before the lock
  and again under it:
  - a paused archive (`seal.RefuseWrite`; Pause is lock-free, `seal/seal.go:72`);
  - a half-deleted archive (`RefuseDeleteMarker`, as `reindex.go:47,66` does);
  - an archive that became encrypted while waiting (as `run.go:661-667` does).
  Note the two "Resume"s: `mailarchive resume` lifts a pause; "Resume tidy-up" finishes a merge. The
  words should not let one be taken for the other.
- **S3. Plain `verify` "writes nothing" is not literally true.** It writes its last-verify record and
  integrity file (`verify.go:248`; `state/lastrun.go:486-491`), and it takes the archive lock
  (`verify.go:218`), so it waits while a merge runs. In §9's S3 amendment and MA-1228, say "writes no
  manifest, no index and no message file". Otherwise MA-1228's assertion cannot pass as written.
- **S4. MA-1229's ownership check (2) needs a defined set.** "Every file under `-out` other than the
  manifest, the index and the folder pages" also takes in other files that live in an archive: the
  lock, the history log, the last-run and last-verify records, the report, the README,
  `search.db-wal`/`-shm` and the pause sentinel. Define the set as message files: `.html`,
  `-attachments.zip` and `.eml` under a store folder.
- **S5. Batches start counting at group `Folded`.**
  - Say that batches are formed from group `Folded` on. The batch size is not stored, so a later
    build with a different size must not re-form batches from group 0.
  - Add a test: resume with a different batch size, after its positive twin.
  - Mark "the largest has five copies" as unmeasured, or measure it on the owner's manifest.
- **S6. The result is lost at the clear.** The clear removes `MergePending`, and with it the only
  durable copy of `Kept` and `SurvivorChanged`. A crash after the clear's save, or a closed window,
  loses the "kept: this copy differs" list for good; the copies stay safe as their own records.
  Persist the result (counts, kept copies, kept-apart groups) in the clear's own save or in a run
  history entry.
- **S7. Load checks.** Add these:
  - every key appears once across all groups, as a survivor or as a copy;
  - a copy in `Kept` is still a record;
  - entries sit only on the survivors of the last folded batch.
- **S8. (d)'s guard works by stem, against every record.** "Never a record's own `Path`, never an
  `AlsoFiles` file" should compare file stems, because a live record's zip and `.eml` share its page's
  stem. It should check against every record (`KeyForPath`, `manifest.go:1222`), not only the
  survivor.
- **S9. A record that carries `AlsoFiles` or `MergedFiles` is never a copy.** Leave it out, as a record
  with no proof is left out. Otherwise (b) removes it and leaves those files owned by nothing.
  - Today only Microsoft 365 writes `AlsoFiles` (`graph.go:381`, `manifest.go:2026`).
  - But an earlier merge's survivor that holds a failed removal's `AlsoFiles` could become a copy
    later, if F4's completeness rule picks another survivor.
- **S10. Re-hash the survivor's page and zip at (a).** If they fail, keep the group apart, as for a
  changed original. Today a damaged survivor page or zip can be left as the only page or zip, while
  the copies' good ones are deleted. No unique byte is lost, because both can be rebuilt from the
  `.eml`, but the person sees a broken page in every folder.
- **S11. When the stop protects nothing (lead's call).**
  - **A changed page or zip.** If a passed copy's page or zip differs at (d), it never "matches again",
    so the archive is refused for good in version 1. Recommended: treat it like a failed removal (keep
    it; it moves to the survivor's `AlsoFiles` at the next save). Stop only on an `.eml` or
    survivor-original mismatch while the copy's `.eml` is still on disk.
  - **A gone `.eml` and a survivor damaged later.** At resume step 2, this stops the merge with nothing
    left to protect. Count it as deleted, and report the damaged survivor.
- **S12. §24's words say "re-checks from the start".** Resume re-checks only the pending entries and
  the batches not yet folded. A kept copy is never re-checked. The words should say what happens.
- **S13. The three unmarked leftovers in §1.8.**
- **S14. Add the Desktop copy export to D2's trace.** The Desktop copy export / extraction workspace
  (`copyexport_archive.go:369,1229`, a snapshot read with no archive lock) belongs in §24's trace,
  alongside restore and extract.

## 4. Counters

- **Date worked on:** 7 November 2026, from the session's clock.
- **Register:** a fold-check finding report. The verdict, the severities and the fixes are this
  checker's opinion.
- **Drew on, and reached:**
  - fold check 3 (`docs/review-gmail-dedup-foldcheck3.md`): yes, in full;
  - the design at 9dc5df7:
    - the diff `affab63..9dc5df7` in full;
    - §7.1, lines 930-1060;
    - §9, lines 1544-1990;
    - §10.2;
    - §18's "Paths outside the step";
    - §19, lines 2694-2706;
  - the code:
    - `app/reindex.go` in full;
    - `app/run.go:535-840`;
    - `app/encrypted.go:415-455`;
    - `app/verify.go:189-253`;
    - `state/manifest.go:270-298`, `:679-941` and `:2120-2201`;
    - `state/lastrun.go:472-493`;
    - `export/atomic.go:110-166`;
    - `export/exporter.go:592-631`;
    - `desktop/maintenance.go:186-245`;
    - `wizard/wizard.go:1270-1297`;
    - `cmd/mailarchive/main.go:60-129` and `:820-869`;
    - `tbengine/run.go:130-210`;
    - `seal/seal.go`, outline only;
    - `desktop/localarchive.go`, around its two loads;
  - greps:
    - every `state.Load` and `state.LoadFrom`;
    - every `.Save()` outside tests;
    - `MergePending`, `MergedFiles` and `RebuildOwed` in code;
    - `AlsoFiles` writers;
    - archive-lock takers;
    - `app.Rebuild`, `app.Run` and `app.RunGraph` callers.
- **Not checked, per claim:**
  - Every finding comes from reading, not from a run. No test was run, and the step is not built.
  - `archdelete`'s order (whether its `MergePending` refusal can sit before its marker write) was not
    read.
  - The copy export's file reads were seen through `LoadFrom` only.
  - Whether `index.Open` writes to `search.db` before `reindex`'s refusal is still not traced.
  - S6 assumes the result is shown only from memory; the design names no durable place for it.
- **Smallest checks that would settle M1:**
  - Count the owner's Gmail records with an `.eml` digest but no `Fixity.Zip`. That is how many copies
    a literal reading of (a) would keep.
  - Count those whose `-attachments.zip` exists on disk anyway. That is how many S2 zips would be
    owned by nothing after (b).
- **Borrowed lines:** short quotes of the design, each with its line number.
- **Drift:** none noticed.
- **Touched:** this file (new), one commit, and a commit-message file under the repo's `.claude/`
  folder. No design text and no product code was changed.
- **Persona:** none.
- **Source reached, per source:** fold check 3 yes; design yes (the parts listed); code yes (as
  listed); the owner's archive no; fold checks 1 and 2 no (only as fold check 3 and the design quote
  them).

Written by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method.
