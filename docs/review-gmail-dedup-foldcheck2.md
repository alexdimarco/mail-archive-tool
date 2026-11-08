# Fold check 2: Gmail de-dup design, the F1 to F4 folds and §18 and §19

Written by a model answering under no name, a fresh instance that has not met the record before.

**Verdict: FAIL (narrow), for slice D2 only. B2, B3 and D1 may proceed now.**

F1 to F4 are each folded as asked (all four PASS). The failure is new: it comes from §19, the lead's
call to **un-merge** a copy that fails its check (put its record back). That call is right, but three
older sentences were not brought in line with it, and together they leave two ways to lose a kept file
and one way to leave good bytes reachable only through a verify report:

- **M1.** The text still sends a failed copy's files to the survivor's `AlsoFiles` (the list of extra
  files a record owns) **and** §19 puts the copy's own record back. Then the same files have two owners.
  If the person later deletes the survivor email, redaction (the reindex step that removes a deleted
  email's files) deletes every `AlsoFiles` file too (`internal/app/reindex.go:174-179`). That removes
  the un-merged copy's files, whose bytes differ from the survivor's: a different email the person did
  not delete.
- **M2.** When the **survivor's** original fails its re-hash, §18 still keeps the copies only in
  `AlsoFiles` (design line 2120). §19 supersedes only the "mismatch on a copy's files" row, so this
  case is left out. The survivor is damaged, so restore and extract skip it, and the good bytes are
  reachable only through verify: exactly what §19 forbids.
- **M3.** A copy un-merged on **resume**, after an earlier attempt already deleted its zip and page,
  comes back as a record with no page. The next `reindex` treats a record whose page is gone as
  deleted by the person and removes its `.eml` (`reindex.go:135-142`, `:168-176`). In the M2 case that
  `.eml` is the only good copy.

Each is a design-text change to §9/§18/§19 before D2. None touches B2, B3 or D1.

- **Date worked on:** 7 November 2026 (the session's date, not inferred).
- **What was checked:** `docs/design-gmail-dedup.md` at ef91b41 (branch `gmail-dedup`), sha256
  `83a4c28a98bdcbd0196ddca4e93f7ec6d53fbf87d330afc5a7f270e39ccb23c2`; the additions read as
  `git diff 832a02f..ef91b41`.
- **Order of reading:** the first fold check (`docs/review-gmail-dedup-foldcheck.md`, 92a1173) first,
  then the code it and the folds rely on, then the design's fold-check notes, §18 and §19. The
  caller's brief named the questions but not the answers.
- **Procedure:** read-only. No product code was changed or run; no test was run.
- **Register:** a finding report from reading code and design text. Where a recommended fix is given,
  that is this checker's opinion.

## 1. Results

| # | Question | Result |
|---|---|---|
| 1 | F1: encrypted archives never reach the merge step; capture-time membership writes and deletes no unit | **PASS** (one note) |
| 2 | F2: the yes binds names, checksums and survivor; recomputed under the lock | **PASS** (one should-fix, fails safe) |
| 3 | F3: read-max vs write version, manifest and index; unswitched archive stays writable by 1.6.1 | **PASS** (one should-fix for B3) |
| 4 | F4: an incomplete record is never a target or survivor; consistent with §19 | **PASS** |
| 5 | S1 to S9 | **PASS**, all nine folded (one citation slip in S1) |
| 6 | §18's orphan-zip finding | **PASS**: the hazard is real, and zip-first plus all-or-none closes it |
| 7 | §19's un-merge | **FAIL**: M1, M2, M3 |
| 8 | Contradictions | M1 and M2 are contradictions; others listed in §3 |

### 1.1 F1 (PASS)

- **Entry points.** The Desktop card never shows the offer on an encrypted archive (design §9
  fold-check note, "Desktop words"). `merge-copies`, with or without `-plan` or `-resume`, refuses
  "before it takes any other step" (line 1548). That can be decided without the key, from the
  encrypted envelope. A backup's "Finish the merge first" refusal points to `merge-copies -resume`, which
  refuses too. A sealed manifest holding `MergePending` or any `MergedFiles` entry is refused by name
  (line 926). Encrypt conversion and decrypt copy refuse while a plaintext archive's merge is pending
  (S3). No path found reaches step 5 on an encrypted archive.
- **Nothing new is swept.** Both lists the start sweep uses are built only from the records' sealed
  sets: the kept-alive list (`app/encrypted.go:279-304`, used by `container/store.go:978-989`) and the
  spared set (`export/units.go:315-330`). So the sweep can remove a unit only when a record stops naming
  it. Version 1 on an encrypted archive never removes a record, and never changes a record's sealed set
  for this design. It only adds memberships, which are metadata inside the sealed record. A new
  identical copy becomes a membership instead of a new record, so its units are never written. Traced:
  the sweep's removal set is a subset of today's (`units.go:353`, `store.go:730-735`).
- **Note (no fix needed for byte safety).** The sealed-`MergePending` refusal says "changes nothing".
  For that to be literal, it must sit after the sealed load and **before** `prepareEncrypted`'s sweep
  (`app/encrypted.go:58-84`). In version 1 the order cannot lose bytes, because no record is ever
  removed. Say the place anyway, so the future encrypted design inherits it.

### 1.2 F2 (PASS)

- The digest covers every group's survivor key, each record's key, and each record's recorded
  `Fixity.EML`, page and zip digests (line 1567). It is recomputed under the lock before 5.1
  (line 1569). Step 3's redo folds a copy only if its recorded `Fixity.EML` still equals the
  survivor's. File paths are not in the digest. That is safe: step 3 runs under the same lock and reads
  the paths from the saved manifest at that moment.
- **Should-fix (D2), fails safe.** The plan leaves out any record whose archived original fails its
  re-hash ("no proof, no merge"). The recompute at the yes uses "recorded digests only, no re-hashing",
  so it cannot know which records the plan left out. On any archive with one damaged original, the two
  digests would differ and **every** yes would be refused. The plan should hand its left-out keys to
  the yes, the recompute should apply them, and the digest should cover them.

### 1.3 F3 (PASS)

What 1.6.1 checks when it opens an archive (it is today's code, main 0923ffa):

- plaintext manifest: `state/manifest.go:784` (refuses above 6);
- sealed header: `container/record.go:168` (refuses above 6), then the strict decode at `:171`;
- archive card: `archindex/recognise.go:432`;
- index: `index/index.go:162`, `index/memory.go:76`, `index/lazy.go:47`, `index/listfolder.go:187`
  (each refuses above 3).

With the fold, an archive that has not switched keeps manifest 6 and index 3:

- **Manifest.** Every site that writes the version is named: `newManifest` (`:701`), the load-time
  bump (`:933-934`), the fresh sealed record (`container/store.go:166`) and `save` (`:2155`). A grep
  finds no other `state.View{Version:` builder.
- **Index.** Both places that stamp it are named: `resolveIndexVersion` (`index.go:218`, `:221`) and
  `RepairKeys` (`:874`, `:881`). `RepairKeys`'s guard (`:804`) compares with the key format. The sealed
  index records `Version()` in the pointer (`app/sealedindex.go:66`, `container/store.go:937`), so it
  follows.
- **Loose end closed.** The fold leaves "every comparison with `LoadedVersion`" untraced
  (line 1105). There are only two, `manifest.go:817` (`< 3`) and `:851` (`< 4`), and neither changes
  meaning between 6 and 7.
- **Should-fix (B3).** The fold names only `index.go:162-164` as the refusal that must compare with 4.
  `memory.go:76`, `lazy.go:47` and `listfolder.go:187` compare with the same constant. If the builder
  turns that constant into the key format (3), 1.7 would refuse its own version-4 sealed index and
  folder list as soon as the first `doc_folders` row lands. Name all four.
- **Note.** The sealed header is decoded strictly (`record.go:205-215`). The new header slots must be
  left out of the JSON when empty, as the §7.1 schema says for every new field ("every new field is
  `omitempty`"). MA-1216's F3b case (1.6.1 accepts a 1.7-saved sealed record) proves it.

### 1.4 F4 (PASS)

- A candidate must pass `Complete()` (`manifest.go:289`), carry no capture-gap label, have a digest,
  and not be a stub (§4.4 note). Listed memberships go only on a complete would-be survivor (§5.3
  note). The merge step's survivor must be complete (§9 note). The fill retry that could rewrite a home
  `.eml` (`export/exporter.go:443-445`, `:514-519`) is now never reached from a member folder.
- **§19's "a copy missing pieces may fold into a complete survivor" is safe, not a contradiction.**
  F4 is about **receivers** (targets and survivors). §19 is about **givers**: a copy whose record is
  removed and whose folder becomes a membership on the survivor. The only bytes a copy can hold
  uniquely are its original, and 5.5 deletes nothing until that original re-hashes equal to the
  survivor's. Its page and zip are derived from that original, and a complete survivor has its own. A
  copy with no `.eml` is never folded (§19). One edge, safe either way: a page or zip that is
  **recorded but missing** on the first pass fails all-or-none, so the copy is kept (and, with §19,
  un-merged). Nothing is deleted.
- The open item ("whether any path marks a complete record fillable without passing the KEEP guard")
  is fairly left to D1's adversarial pass. The one path seen, the version-1 sentinel at
  `manifest.go:796-799`, cannot meet a record with members: a manifest that old has none.

### 1.5 S1 to S9 (PASS)

| Item | Folded? | Note |
|---|---|---|
| S1 | yes | Zip, then page, then `.eml`; a file already gone on resume counts as deleted. It goes further than asked (all or none per copy), which is right. **Citation slip:** line 1610 says `SweepOrphans` runs "at the start of … `reindex`, before the manifest load". In `reindex` it runs **after** the load (`reindex.go:100` load, `:115` `RepairKeys`, `:118` sweep). That helps: `reindex`'s `MergePending` refusal can sit right after `:100`, before `RepairKeys` and the sweep. Say so. |
| S2 | yes | Empty digest allowed for page and zip only; the entry is kept at load. |
| S3 | yes | Writers listed; the full grep is left to the builder, as recorded. |
| S4 | yes | `os.Root` (Go 1.24 and later; the repo pins 1.26), with `Lstat` as the fallback; MA-1238 covers a link as the file and as a directory on the path. |
| S5 | yes | — |
| S6 | yes | The old sentence is marked superseded in place. |
| S7 | yes | — |
| S8 | yes | — |
| S9 | yes | — |

## 2. The orphan-zip sweep and other cleanups (question 6)

**The hazard is real.** `export.SweepOrphanZips` (`export/atomic.go:132-140`) removes every
`-attachments.zip` whose sibling `.html` is missing. It does not check the file's age or the manifest.
A local run calls it at `app/run.go:770`, before the manifest load at `:789`, so before any
`MergePending` refusal. Under the old step-5 wording (a page deleted while its zip stays after a
mismatch), the next backup attempt would have removed a kept zip. **1.6.1 and older builds run the same
order**, so the hazard was there for "1.6.1 opens the archive" as well.

**Zip-first plus all-or-none closes it**, for 1.7 and older builds alike. A copy's zip goes before its
page, so the merge step never leaves a zip without its page:

- a failed zip removal stops that copy's removals;
- a failed page removal leaves a page with no zip;
- an un-merge keeps every file the copy still has.

**Other removals checked** (a grep for `os.Remove`, `Sweep` and `Orphan` in `internal/app`,
`internal/export` and `internal/container`, test files excluded):

| Removal | Runs | Can it reach a kept merge file? |
|---|---|---|
| `SweepTemps` (`atomic.go:119-127`; also `graph.go:225`, `run.go:783`) | before load | No: only temporary files and snapshot files |
| `reindex` redaction (`reindex.go:135-185`) | after load | **Yes, through M1 and M3** |
| Re-export under a new name (`exporter.go:789-797`) | during a backup | No while the merge is pending (backups refuse). Afterwards it removes only that record's own old page, zip and `.eml` |
| Stale zip on re-capture (`exporter.go:627`, `attach.go:77`) | during a backup | No: that record's own zip only |
| Encrypted unit sweep and write (`units.go:135`, `:356`; `store.go:778`, `:809`) | encrypted only | No: there is no merge on encrypted archives in version 1 (§1.1) |
| Search-database, scratch, report and copy-export files (`reindex.go:453`, `:534`; `scratch.go`; `report.go:54`; `copyexport_*`) | various | No: none of these are mail files |

Outside those three packages, `archdelete` (deleting a whole archive) refuses while a merge is pending
(S3). The other packages that remove files were not read (see §5).

## 3. §19's un-merge, traced (question 7), and contradictions (question 8)

**Is the pre-merge record, saved before 5.3, enough?** For the copy's own record, yes. It is written
at 5.1, before the anchor save, and only 5.6 clears it. The only un-merge decisions happen in 5.5. So
at every crash point from 5.3 to 5.6, the record to put back is on disk. It is **not** enough as
written, for four reasons.

1. **M1: two owners.** Step 5 (line 1506), S1 (lines 1593 and 1600) and §18's row (line 2113) still
   move a failed copy's entries into the survivor's `AlsoFiles`. §19 ("its record is **also** put
   back") does not say they stop going there. Redaction removes `Path`, every `AlsoFiles` file and
   `MergedFiles` (design §8.8, `reindex.go:176-179`). So deleting the survivor would delete the
   un-merged copy's different bytes.
2. **M2: a damaged survivor.** §18's bullet (line 2120) keeps every copy only in `AlsoFiles` and is not
   superseded. The survivor is damaged, restore and extract skip it (R20), and the good copies are
   shown nowhere as mail. This contradicts §19's "a file the merge left behind must never be reachable
   only through a verify report" (line 2157). The same applies to §7.1's load rule (line 935): a
   damaged `MergedFiles` entry moves to `AlsoFiles` even though `MergePending` still holds the copy's
   record.
3. **M3: a restored record with no page.** On resume, a copy's zip and page may already be gone, and
   its check can fail this time (its `.eml` changed, or the survivor now fails its re-hash). §19 then
   puts back a record whose `.html` does not exist. `reindex` reads "page missing" as "the person
   deleted it" and removes the `.eml` (`reindex.go:141-142`, `:168-176`).
4. **Gaps, not losses (should-fix):**
   - The `MergePending` schema (lines 871-878) has no field for the pre-merge records. It needs:
     - each copy's whole record;
     - each survivor's pre-merge `Folders`, so un-merge removes only the memberships that copy added.
       Today §19 says "its own folder", but step 3 also moved the copy's own memberships to the
       survivor.
     - a mark for each copy already un-merged.
   - Records put back must pass the same load checks as `Entries` (`finish`, `manifest.go:775`), for
     example the clean-path check.
   - **The resume rule re-merges un-merged copies.** "If any group's copy record still exists, redo
     step 2 … then step 3" (line 1518) is also true of a copy that was just un-merged. Its recorded
     `Fixity.EML` still equals the survivor's, so F2's step-3 guard lets it through. 5.5 then un-merges
     it again. This repeats on every resume but deletes nothing, because the re-check always comes
     first. The un-merged mark should stop it.
   - **Order of the un-merge.** Each un-merge should run in this order, all before 5.6's save:
     1. save the manifest: the record back, its `MergedFiles` entries out, the survivor's memberships
        trimmed, the copy marked un-merged;
     2. in one index transaction: put back the copy's search row and delete the survivor's
        `doc_folders` rows for those folders;
     3. regenerate the folder pages.

**Other contradictions checked, none found:** F4's survivor rule against §19's copy rule (§1.4);
§18's 5.3 row against §9's resume rule (they agree); 5.1's "already 7 from the yes" against F3 (they
agree, since `DedupStores` makes it 7); the §7.4 encrypted words, §9's Desktop words and MA-1245
(they agree); §13's slice gates against §7 (they agree).

## 4. Must-fix list (before D2 only)

Each is a design-text change, folded beside the text it amends, then re-checked (only these).

- **M1.** An un-merged copy's files go back to **its own** restored record (its `Path` and `Fixity`),
  never into the survivor's `AlsoFiles`. Mark step 5's "it moves from `MergedFiles` to the survivor's
  `AlsoFiles`" (line 1506), S1's two "move to `AlsoFiles`" sentences (lines 1593 and 1600), and
  §18's row (line 2113) as superseded **for every copy that is un-merged**. `AlsoFiles` stays only for
  files of a copy that passed its check but whose removal failed. Those bytes equal the survivor's,
  which is shown as mail. MA-1229 gains: un-merge a copy, delete the survivor email, run `reindex`; the
  copy's three files stay and its record still opens.
- **M2.** "Fails its check" includes **the survivor's original failing its re-hash**. Then every copy
  of that group is un-merged, and the damaged survivor stays as it is for verify to report. Supersede
  §18's bullet (line 2120). Likewise for §7.1's load rule (line 935): when `MergePending` holds the
  copy's record, the damaged entry's copy is un-merged at resume. `AlsoFiles` is used only when no
  pre-merge record exists, and verify then names the file as "kept, not shown as an email". MA-1229
  gains: a survivor damaged after the plan; after the merge, each copy is listed in its folder and
  found by search.
- **M3.** A copy is put back only with its page present. If an earlier attempt already deleted its
  page or zip, the merge step rebuilds them from the copy's own `.eml`, under the lock, before the save
  that restores the record, and records their new digests. If they cannot be rebuilt, the copy is
  restored with its `Missing` set, so `status` and fill see it. Either way, `reindex` must never find
  a restored record with no page. MA-1229 gains: a crash after a copy's zip and page are deleted; then
  the survivor is damaged; then resume and `reindex`; the copy's `.eml` is still on disk and opens as
  a mail.

## 5. Should-fix (fold with the slice named)

- **D2:**
  - the `MergePending` fields and the un-merged mark (§3, item 4);
  - restored records pass `finish`'s checks;
  - resume skips un-merged copies;
  - the un-merge's order;
  - F2's left-out keys carried into the recompute (§1.2);
  - the sealed-`MergePending` refusal placed before `prepareEncrypted` (§1.1 note);
  - S1's `reindex` citation corrected, with the refusal placed right after `reindex.go:100` (§1.5).
- **B3:** all four newer-index refusals compare with 4 (`index.go:162`, `memory.go:76`, `lazy.go:47`,
  `listfolder.go:187`). MA-1243 gains an encrypted case: an archive switched on, with a `doc_folders`
  row, reopens under 1.7.
- **B2 (text only):** close the `LoadedVersion` item (only `manifest.go:817` and `:851`).

## 6. Counters

- **Date worked on:** 7 November 2026, from the session's clock.
- **Register:** a fold-check finding report from reading code and design text. The verdict, the
  severity of each item, and the suggested fixes are this checker's opinion.
- **Drew on, and reached:**
  - the first fold check (92a1173): yes, in full;
  - the design at ef91b41: yes, the fold-check notes, §9 (lines 1404-1640), §18 and §19 in full, the
    §7.1 schema, §8.8, and the diff from 832a02f;
  - the code, at the lines cited above: `container/store.go:150-175`, `:635-765`, `:940-995`;
    `app/encrypted.go:1-120`, `:260-304`; `export/units.go:280-362`; `export/atomic.go:90-167`;
    `app/run.go:700-830`; `app/reindex.go:95-200`; `export/exporter.go:770-805`;
    `index/index.go:150-225`, `:790-884`; `state/manifest.go:255-296`, `:740-800`, `:920-940`,
    `:2140-2165`. The rest came from grep (the version constants, `View{` builders,
    `DisallowUnknownFields`, `AlsoFiles`, `os.Remove`).
- **Not checked, per claim:**
  - M1 to M3 come from reading, not from a run. No test exercised `reindex` against a restored record.
  - How Desktop's single-email delete reaches redaction: taken from design §8.8, not traced in code.
  - Whether the Desktop and `merge-copies` code (which does not exist yet) puts each refusal where
    the design says.
  - The first fold check's citations not re-read here: `exporter.go:443-445`, `:514-519`, `:983-990`;
    `record.go:113`.
  - Removals outside `internal/app`, `internal/export` and `internal/container` (for example
    `internal/health`, `internal/desktop`), beyond `archdelete` and `reindex`.
  - The completeness design: not read.
- **Smallest checks that would settle M1 and M3:**
  - M1: a plaintext fixture where one record lists a second record's page in its `AlsoFiles`. Delete
    the first record's page and run `reindex`; see whether the second record's `.eml` is gone.
  - M3: delete one record's `.html`, run `reindex`, and see whether its `.eml` is gone.
- **Borrowed lines:** short quotes of the design and the first fold check, each attributed where it
  appears.
- **Drift:** none noticed; the check stayed on the design as it stands at ef91b41.
- **Touched:** this file (new) and one commit; a commit-message file under the repo's `.claude/`
  folder, removed after use. No design text and no product code changed.
- **Persona:** none.
- **Source reached, per source:** first fold check yes; design yes (the parts listed); repo code yes
  (as listed); completeness design no; Thunderbird source no; the owner's profile and archive no.

Written by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method.
