# Fold check: Gmail labels design, after the 10-lens review

**Verdict: FAIL (narrow).** On a plaintext archive no path found loses or alters an archived byte.
The merge step's order, its proof from the archived files, and the re-check just before each deletion
hold, with a crash at any point and with v1.6.1 opening the archive at any point. **On an encrypted
archive one path does lose bytes.** After the merge step's anchor save, the copies' encrypted files
are named by no record. The sealed save then lists them as "retained", and the **next backup's start
sweep deletes them**, without the re-check. That includes a file the merge step kept because its
bytes did **not** match the survivor's. The design sends those deletions through the "deferred unit
deletion", but on an encrypted archive that path deletes nothing during a run; the start sweep does
the deleting (F1).

Three more items must be folded before the slice they name:

- the plan can go stale between the review and the yes (F2);
- the search index would turn version 4 on every archive at its first 1.7 open, without the
  person's yes (F3);
- a member folder's copy can rewrite a "fillable" home record's original, although the design says
  that never happens (F4).

Each is a design-text change, not a re-design. Re-check only F1 to F4. **B1 (the shared Gmail
detection function) may start now**: none of the four touches it, and it agrees with the
completeness design at its latest commit (§5). B2 waits for F1(a, b) and F3(b), B3 for F3(a), D1 for
F4, and D2 for F1 and F2.

- **Date worked on:** 7 November 2026 (the session's date).
- **What was checked:** `docs/design-gmail-dedup.md` at commit 832a02f (branch `gmail-dedup`),
  sha256 `51493ad8e66a605b2d7a3359018c399f63e6ed16dce58b55719b8858b7ce0c4f`, against
  `docs/review-gmail-dedup-10lens.md` (ff7fc90, GD-1 to GD-26).
- **Independence:** this checker did not write the design, the review or the folds. It read the
  review first, then the design end to end, then the code. The caller's brief summarised the lead's
  calls before the design was read.
- **Procedure:** read-only. No product code was changed or run; no test and no Thunderbird was
  run. MailArchive code was read in the worktree (it is main 0923ffa plus docs). go-mbox v1.0.4 was
  read in the module cache. The completeness design was read at `origin/tb-completeness` 58e2542.
- **Register:** a finding report (code read), plus the checker's opinions where marked.

## 1. Per-condition results

FOLDED: the text can be built from and removes the failure the review named. PARTIAL: one piece is
missing (named). NOT_FOLDED: none.

| Id | Result | Note |
|---|---|---|
| GD-1 | **PARTIAL** | Plaintext: folded. The proof comes from the archived `.eml` checked against `Fixity.EML`, never from the live copy (§9 fold step 1). `MergedFiles` carries size, digest and proof. Both files are re-hashed just before deletion, and deletion goes through the contained deleter. Two gaps. **(1)** On an encrypted archive the deletions are not done by the merge step at all; the start sweep does them later, without the re-check (**F1**). **(2)** The yes binds to a digest "over its groups", that is, over record keys. A backup between the plan and the yes can rewrite a survivor's file, the keys still match, and step 3 then files the copy's folder under different bytes (**F2**) |
| GD-2 | FOLDED | The first durable act is the `MergePending` save, read back as 7. The archive is already 7 from the person's yes (§7.4 fold c), so v1.6.1 refuses from before the offer, `-no-index` runs included (`manifest.go:784-786`). Resume is "forward from the saved manifest", with step 2 idempotent. No plain marker file |
| GD-3 | FOLDED | No marker file. `MergePending` is sealed. Summary-reader notes go through the sealed capture-gap path (`localwalk.go:114` records `Folder` in a fault; `CaptureGaps` is in `View`, `manifest.go:748`), and the Gmail counts go into sealed `RunDetails` (`record.go:47`). MA-1230 asserts no folder name or Message-ID in any plain file |
| GD-4 | FOLDED | Q-L1: only byte-identical copies merge. Every member folder gets the stored original, so R20 and R40 hold as worded. Q-A is moot |
| GD-5 | **PARTIAL** | (a) to (d) are folded: a set of `{key, fingerprint}`, the unchanged seen-record path, home-only writes, and the KEEP guard on the alias key. (e) is not. The candidate rule excludes "a record carrying any gap label", but `Fillable` is `len(Missing) > 0` (`manifest.go:286`), which is not a label. So a fillable home record can take members, and the alias hit's unchanged fill retry then rewrites its `.eml` from a member folder's **current** bytes (`exporter.go:443-445`, `:514-519`), matched only by envelope fingerprint. MA-1213's re-scope says "a fillable home record is not healed", so the rule and its test disagree (**F4**) |
| GD-6 | FOLDED | Steps 1 to 5 match the review's (a) to (e). The byte-position table is counted below go-mbox. In production the only layers between the file and go-mbox are the BOM skip and an identity wrapper (`localwalk.go:670-685`). go-mbox splits on a line starting `From ` at a line start (`reader.go:41-44`, `:99`). The table is used only when the counts agree and the mbox is unchanged between the walk and the summary pass. A colliding Gmail id adds nothing |
| GD-7 | FOLDED | Dissolved by Q-L2: no `LastSeen` per membership, so a steady run changes no bucket |
| GD-8 | FOLDED (note) | Grammar subset with unknown forms refusing the file; iterative, depth 8; caps with numbers; two passes, 256 MiB of kept data; deleted bits named (to be pinned in M1); a stable read only; the contained opener; `.msf` files listed by the walk's own rules; counts only; fuzzing from the throwaway account. Note **S5**: pass 1 must also keep **literal** cell values of the four columns, not only atom ids |
| GD-9 | FOLDED (note) | One shared function: `is_gmail` only; `prefs.js` through `util.ReadRegularFile(path, 16 MB)` (`openregular.go:67`, read: refuses a link, never waits on a pipe, refuses an oversized file unread); the account by store folder, `hostname` only when unique, never a guess. (e) is stated beside R9 and R25. Note **S7**: (c) does not name where the 0x8000 flag is read; it is moot for merging in version 1, but the "This folder: All Mail" words use it |
| GD-10 | FOLDED | G7 is narrowed truthfully. v1.6.1's backup and verify refuse a v7 manifest (`manifest.go:784-786`); the archive card's reader refuses too (`archindex/recognise.go:432-433`). Its `serve`, `search` and `status` open the index read-only without a version check (`index.go:233-256`) and write nothing. Release notes are named. See F3 for the index's own version stamp |
| GD-11 | FOLDED | Restore and extract count per (folder, record); stem-collision suffix; copy export into every chosen folder (Q-L3); `/goback` pinned to home; `KeepsTimeline` (`restoreread.go:279-283`) does not flip |
| GD-12 | **PARTIAL** | (a) for the manifest: one `FormatVersion`, covering all six new fields (`gm_sha256`, `Folders`, `MergedFiles`, `DedupStores`, `MergePending`, `SummaryStamps`), with a reflection test. (b), (c) and (d) are folded. **Missing for the index:** "version 4 only with a `doc_folders` row" is stated, but `index.go` writes its version constant at a fresh create (`:218`) and in `RepairKeys` whenever the stored version is below the constant (`:804`, `:874`). Bump the constant to 4 as written, and every archive's index turns 4 at its first 1.7 open, with no yes (**F3a**). The manifest has the same trap in a milder form (**F3b**) |
| GD-13 | FOLDED | The state table covers every listed event. A folder read partly adds no listed member; version 1 is add-only |
| GD-14 | FOLDED | Carries `Folders`, `MergedFiles` and `AlsoFiles` forward, and recomputes `gm_sha256`. Full re-capture semantics are stated (G6 fold, MA-1242) |
| GD-15 | FOLDED | Every listed claim is labelled: the reader's form, 4,208 copies, the 175 MB estimate marked as an estimate, 79,439, INBOX's empty ids, 34.5 GB only for byte-identical groups |
| GD-16 | FOLDED | The R45 deletion clause is written as an invariant. R3 and R6 wording is left for the operator to accept. `AlsoFiles` is owned by verify (MA-1244). On encrypted archives, see F1(c) |
| GD-17 | FOLDED | G9 added; verify's words name every folder; the rebuild does not recover memberships (Q-L4) |
| GD-18 | FOLDED | The offer, the back link, "This folder: All Mail", the other computer's words, and restore's words are all drafted |
| GD-19 | FOLDED | Unchanged-summary skip (all or nothing per store, so the position chain stays whole); plan cost shown, with progress and cancel; verify once per record; index shape; `doc_folders` deleted with the row |
| GD-20 | FOLDED | Separate units S-dup and S-membership, each with its own version-7 trigger |
| GD-21 | FOLDED | — |
| GD-22 | FOLDED | — |
| GD-23 | FOLDED (moot) | Recorded; the split is dropped |
| GD-24 | FOLDED | Status line and citations corrected. Checked: `main.go:2121` and `html.go:2104` hold |
| GD-25 | **PARTIAL** | Plaintext: "no proof, no merge" is folded, and a file kept after a mismatch goes to `AlsoFiles`, owned and reachable. Encrypted: `AlsoFiles` is a list of paths and cannot name a unit, and nothing names a kept unit to the start sweep, so the "kept" file is deleted at the next backup (**F1c**). Note **S2**: a page or zip with no recorded digest has no defined fate |
| GD-26 | FOLDED (note) | Classes on an unmerged archive; the would-be survivor; MA-1240. Note **S6**: §5.3's fold says the survivor "never changes once a membership is placed", but §17 item 6 says a later fill can change it and the anchor save then moves memberships. Keep §17's wording |

Count: 22 FOLDED (GD-8, GD-9 and GD-26 with notes; GD-23 moot), 4 PARTIAL (GD-1, GD-5, GD-12,
GD-25), 0 NOT_FOLDED.

## 2. Can any path lose or alter an archived byte? The merge step, traced

At each step: a crash just after it, v1.6.1 opening the archive (the owner's Nextcloud-synced
second computer), and the encrypted path. "Saved" means durable. The plaintext save writes a temp
file, fsyncs, renames and fsyncs the directory (`manifest.go:2172-2199`). The sealed save is one
commit point, the pointer rename (`container/store.go:867-869`).

| Step | Crash just after | v1.6.1 opens | Encrypted |
|---|---|---|---|
| The yes to Q-B (`DedupStores` saved) | nothing to finish | refuses: v7 (`manifest.go:784-786`; sealed: version probe before the strict decode, `record.go:166-171`) | `DedupStores` needs its sealed slot (MA-1237). Without it, today's save drops it (`manifest.go:2157`, five `View` fields) |
| 4. Plan (writes nothing) | nothing | refuses | — · **but** a backup may run before the yes (**F2**) |
| 5.1 `MergePending` saved, read back 7 | resume redoes 2 and 3 | refuses | sealed slot (MA-1237) |
| 5.2 index: copies' rows out, `doc_folders` in | one transaction; resume redoes it (idempotent) | refuses to write; old `serve`/`search` show home folders only, write nothing | the sealed index is staged inside the same commit as the manifest (`store.go:901-903`), so 5.2 and 5.3 may land together: fine |
| 5.3 anchor: copies' records out, `MergedFiles` in | resume from 4; files owned by `MergedFiles` | refuses | **fails (F1a)**: the commit drops the copies' units from the live list and lists them "retained" (`store.go:978-989`, live = records' `Sealed` only, `app/encrypted.go:279-304`). The next encrypted run's start sweep removes unnamed retained units (`app/encrypted.go:58-73`; `export/units.go:313-330`, `:353`). No re-check runs, and the design does not say the resume path avoids that sweep |
| 5.4 pages | resume from 4 | refuses | in memory |
| 5.5 re-check, then delete | resume re-checks every file. A file already deleted reads as a mismatch and is moved to `AlsoFiles` as missing (**S1**): a false "missing" in verify, but no byte lost | refuses | **fails (F1b, c)**: "deferred unit deletion" (`exporter.go:155-160`) deletes nothing on a sealed archive (`exporter.go:983-990`). A unit kept after a mismatch has no sealed home and is swept at the next backup. That is a real loss: its bytes differ from the survivor's |
| 5.6 clear | done | refuses (still v7) | — |

**Plaintext: holds.** Every deleted file is first proven equal to a re-hashed survivor original. A
mismatch keeps the file, owned. v1.6.1 refuses from the yes on, with or without an index.

**Encrypted: fails (F1).** The bytes of a correctly merged copy also live in the survivor, so in
the usual case only the proof is skipped. But two cases lose the only copy of some bytes:

- a copy unit kept because the survivor's original failed its re-hash;
- a copy unit kept because it changed after the plan.

The start sweep removes both at the next backup.

**Outside the merge step (checked; holds, with notes):**

- **Capture merging** writes no file and deletes none. It compares the copy's digest with the
  record's recorded `Fixity.EML` (`exporter.go:515` and `:900-901`: the `.eml`, or the sealed
  plaintext, is exactly `m.Raw`).
- **Listed memberships** write no file.
- **A Full re-capture** after a merge replaces the stored original with the home folder's current
  copy, as Full does today. The design says so (G6 fold). Accepted.
- **Two writers on a synced folder** were already out of scope (`manifest.go:781-783`). Even then
  no content is lost, because the survivor holds identical bytes. But an older binary's save could
  bring back records that name deleted copies. **S8**: say so in the offer's words.

## 3. The version rule (GD-12, GD-10)

- **One function: yes, for the manifest.** `FormatVersion(m)` covers every new field of the §7.1
  fold. `save` stamps exactly what it returns on both paths. The sealed header takes its version from
  the `View` (`record.go:113`), so the sealed path follows. A reflection test fails on an undecided
  new field.
- **Older versions refuse v7 cleanly: yes.** Plaintext: `finish` refuses before any migration
  (`manifest.go:784-786`). Encrypted: the header's version is probed leniently and refused as
  `ErrNewerFormat` (`record.go:166-169`) **before** the strict decode (`:171`, using `strictJSON`
  at `:205-215`), so new header slots never make v1.6.1 call the archive damaged. Archive cards:
  `archindex/recognise.go:432-433`. Not refusing: v1.6.1's `serve`, `search` and `status`, as the
  fold now says.
- **F3, the gap.** Two constants today mean "the highest version understood" and also "what to
  stamp":
  - The index stamps `indexVersion` at a fresh create (`index.go:218`) and in `RepairKeys`, which
    runs whenever the stored version is below the constant (`:804`) and stamps the constant (`:874`).
  - The manifest uses `manifestVersion` in `newManifest` (`:701`), in the load-time bump
    (`:933-934`) and in a fresh sealed record (`container/store.go:166`). Save overwrites it, so
    the manifest is safe if `save` is the only writer. The index is not.

## 4. The summary-file (`.msf`) reader spec (GD-8)

- **Memory: bounded.** Two passes, 256 MiB of kept data per file, an 8,000,000 dictionary-entry
  cap, a 4,000,000 row cap, 512 cells per row, a 1 MiB value cap and a 998-byte id cap. The
  byte-position table is in memory for the run only: one offset per message, about 312,000 for the
  owner.
- **Time: bounded.** 120 seconds per file and 10 minutes for the pass. An unchanged store skips the
  pass (all or nothing per store).
- **Worst case: "an extra folder shown", as required.**
  - A refused file adds nothing (that folder shows only its copies, as today).
  - A bad row can place an email in one extra folder.
  - New, and harmless: a listed membership ranks second in choosing the merge step's survivor. A
    class's records are byte-identical by recorded digest and re-hashed at the plan, so the choice
    never changes which bytes are kept.
  - A slow disk can make the time cap refuse All Mail's 215 MB file; that file is then "not read
    this run".
- **S5.** "Records only the atom ids that the columns reference" misses a cell that holds its value
  literally rather than by an atom reference. Keep such literal values for the four columns
  (bounded by the 998-byte cap). The failure is safe either way (a missed placement), but M1's spec
  should say it.

## 5. MA-1215 and the shared detection function, against the completeness design

- **MA-1215 is re-scoped.** The §12 fold reads: "detection by `is_gmail` only through the shared
  function (MA-1235); an All-Mail-flagged folder on a non-Gmail account stays per folder; sticky
  `DedupStores`". The reviewed row ("via `is_gmail` and the All Mail flag") stands above it,
  unchanged, as the fold convention requires. MA-1235 adds the `ReadRegularFile` cap, a link, a pipe,
  an oversized file, the account by store folder, and 0x8000 only (absent when two folders carry it).
- **Completeness design at `origin/tb-completeness` 58e2542 (its latest commit): agrees.**
  - `is_gmail` only (its §3 "Gmail detection" fold).
  - All Mail is the one folder with 0x8000; Archive 0x4000 alone never stands in; none or several
    means absent (its M5 fold).
  - `prefs.js` through `util.ReadRegularFile(path, 16 MB)` (its M1 fold).
  - The account by `directory`/`directory-rel` compared component-wise, `hostname` only when exactly
    one server has it, no guess (its C0c, MA-1195).
- **No 0x8000/0x4000 mismatch remains.** Two notes in this design are now stale, because 58e2542
  resolved them:
  - §17 item 11: "the completeness design's own text at 31bd018 still reads 'or 0x4000'";
  - the §6 fold: the completeness design "does not name `ReadRegularFile` for `prefs.js`".
  Update both when the design is next touched.
- **Small differences, not contradictions:**
  1. With no matched account, completeness answers `unknown` and this design answers "not Gmail,
     per folder, one note". Both mean "do not apply the Gmail rule".
  2. MA-1175, MA-1195 and MA-1235 test parts of one function. Say which row owns which part,
     whoever builds it.
  3. Completeness reads the 0x8000 flag from `folderCache.json`; this design does not name the
     source (S7).
  4. The completeness design still cites this design at 9203aae.

## 6. Code citations sampled (36; the safety argument rests on these)

Hold as cited:

1. `container/record.go:39-51` (sealed header);
2. `:53-57` (the "latest" marker);
3. `:61-64` (`sealedRecord`);
4. `:113` (header version from the `View`);
5. `:166-169` (lenient version probe, `ErrNewerFormat`);
6. `:171-181` (strict decodes);
7. `state/manifest.go:136-141` (`AlsoFiles`);
8. `:190-200` (`RawDigest` is `raw_sha256`, so the new name `gm_sha256` is right);
9. `:273-283` (`Fixity`);
10. `:286` (`Fillable` = `Missing`, F4);
11. `:383-397` (`byIdent`, `IdentRef{Key, Fingerprint}`);
12. `:744-750` (`View`, five fields);
13. `:755-767` (`FromSealed`);
14. `:784-786` (refuse newer);
15. `:2148-2157` (`save` stamps the constant; sealed commit of five fields);
16. `export/exporter.go:227-233` (KEEP guard);
17. `:404-408` (write folder);
18. `:503-515` (sealed digest of the plaintext; `.eml` = `m.Raw`);
19. `:763-765` (`AlsoFiles` carried on a seen record);
20. `:894-923` (`sealKind`: plaintext digest; a superseded revision is queued);
21. `index/index.go:27-34` (`indexVersion = 3`);
22. `:162-164` (refuse newer);
23. `:233-256` (`OpenReadonly`, no version check);
24. `index/listfolder.go:187-189` (folder list refuses a newer index);
25. `app/verify.go:313-323` (only the `Path` stem owned; the review's `:314-321`);
26. `app/reindex.go:158-186` (redaction: `validRelPath`, `AlsoFiles`, `DeleteByID`, no tombstone);
27. `thunderbird/thunderbird.go:63-64` (`os.ReadFile`);
28. `:86-104` (map loop, `HasSuffix`);
29. `util/openregular.go:67-88` (`ReadRegularFile`);
30. `:93-99` (`OpenRegularFileIn`);
31. `source/localwalk.go:114` (folder fault into capture gaps);
32. `:537-547` (`mboxDir` naming and the R41 leave-out);
33. `app/restoreread.go:279-283`, `:403-407`;
34. `cmd/mailarchive/main.go:2121`;
35. `export/html.go:2104`;
36. go-mbox v1.0.4 `reader.go:78-87` (no position exposed) and `:41-63` (the separator rule, the
    blank line, `>From `).

Loose (no effect on safety except the second one):

- §17 item 4 cites the strict decode as `:207`. That line is `DisallowUnknownFields` inside
  `strictJSON`; the header's strict decode is `:171`.
- The §9 fold and §8.9 cite `exporter.go:155-160` as the encrypted deletion path. The citation is
  accurate, but on a sealed archive `flushDeletes` deletes nothing (`:983-990`). This is F1.
- The §9 fold's "contained deleter (… no link followed), as redaction does (`reindex.go:159-166`)":
  redaction checks the path **string** (`validRelPath`) and then calls `os.Remove`, so a link in an
  intermediate directory is followed (**S4**).

Not checked: `nsImapMailFolder.cpp:8928-8935` (Thunderbird source, cited through the completeness
design); every "recollection, unverified" item.

## 7. Must-fix list (before the slice named; B1 is not blocked)

Each item is a design-text change, folded beside the text it amends, then re-checked (only these
items).

**F1 (byte loss on encrypted archives; GD-1, GD-25; before B2 for (a) and (b), before D2 for all).**

- **(a)** A `MergedFile` for a unit carries the unit's kind, its revision and its sealed digest,
  copied from the copy's `SealedSet`. Without the revision the unit's file name cannot be derived
  (`units.Path(unit, kind, rev)`).
- **(b)** The sealed live list (`unitFiles`, `app/encrypted.go:279-304`) and the start sweep's
  named set (`SweepUnits`, `export/units.go:313-330`) include every waiting `MergedFiles` unit and
  every kept unit. Then nothing the merge step still owns is ever "retained".
- **(c)** A kept file needs a home that can name a unit. Recommended (the checker's opinion): on any
  mismatch, **un-merge** that copy. Put its record back (its fields are kept in `MergePending` until
  step 6), and take its folder off the survivor. Then its own folder serves its own bytes, on
  plaintext and encrypted archives alike. The alternative is a sealed "kept units" list on the
  survivor.
- **(d)** Say who deletes an encrypted copy unit: the merge step itself, under the lock, right after
  its re-check. Do not leave it to a later sweep. Say also that `-resume` and every refused 1.7 run
  refuse **before** `prepareEncrypted`'s start sweep.
- **(e)** MA-1230 gains:
  - a crash after 5.3, then a backup attempt and then resume: no copy unit is removed before its
    re-check;
  - a unit kept after a mismatch survives the next two backups;
  - a damaged survivor keeps the copy's unit and its folder.

**F2 (a stale plan; GD-1, GD-2; before D2).**

- The plan digest covers each record's key **and** its `Fixity.EML`, page and zip digests.
- At step 5.1, under the lock, the groups are recomputed from the saved manifest (no re-hashing). If
  the digest differs, the step stops: "The archive changed since you reviewed the plan. Review it
  again."
- Step 3 folds a copy only if its recorded `Fixity.EML` still equals the survivor's.
- MA-1238 gains: a Full backup between the plan and the yes, then the yes is refused.

**F3 (the version stamped without a yes; GD-12, Q-B).**

- **(a) The index, before B3.** Split "highest understood" (4) from "key format" (3). `RepairKeys`
  compares with the key format. A fresh or repaired index stamps 4 only when `doc_folders` has a
  row, otherwise 3. MA-1243 gains: on a declined archive, a 1.7 backup, `reindex -rebuild` and a
  fresh index each leave the index at version 3.
- **(b) The manifest, before B2.** Say that `ManifestVersion` becomes "the highest understood" (7)
  for `record.go:168` and `archindex/recognise.go:432`, and that only `FormatVersion` decides what
  is written. `newManifest`, the load-time bump (`:933-934`) and `container/store.go:166` must not
  write it.

**F4 (a cross-folder fill rewrites a home original; GD-5e; before D1).**

- The candidate rule also excludes `Fillable` records (`Missing` not empty, the unknown sentinel
  included).
- Or the alias path skips the fill retry and only counts the record.
- Either way the rule then matches MA-1213's re-scope.

**Fold with their slice (should-fix):**

- **S1 (D2).** On resume, a waiting file that is already gone, whose entry digest equals `proof`
  and the re-hashed survivor, counts as deleted, not as kept. Delete a copy's page and zip before its
  `.eml`.
- **S2 (D2).** A page or zip with no recorded digest is deleted as derivable together with a proven
  `.eml`, with an empty digest allowed for those two roles only. Load validation must not drop such
  an entry, or the file is orphaned (GD-25's failure).
- **S3 (D2).** List every writer that refuses while `MergePending` is set: `reindex` (redaction),
  encrypt conversion, decrypt copy, archive delete, and Add-another-archive. Not only backup, verify
  and the Desktop open.
- **S4 (D2).** The merge deleter removes through an `os.Root` opened on `-out` (or checks every
  component with `Lstat`), not through redaction's string check. This is what MA-1238's "refuses a
  link" needs.
- **S5 (M1).** Literal cell values (§4).
- **S6 (text).** §5.3's "the survivor never changes" against §17 item 6.
- **S7 (B1).** Name `folderCache.json`, read by the shared function, as the source of 0x8000.
- **S8 (D2 words).** The offer says: "Make sure no other computer is backing up into this archive
  while this runs."
- **S9 (text).** Update §17 item 11 and the §6 fold's flag, both now resolved at 58e2542 (§5).

## 8. Counters

- **Date worked on:** 7 November 2026, from the session's clock, not inferred.
- **Register:** a fold-check finding report from code reading. The verdict, F1(c)'s un-merge
  recommendation and the suggested words are the checker's opinion.
- **Drew on, and reached:**
  - the design at 832a02f (digest above): yes, in full;
  - the review at ff7fc90: yes, in full;
  - MailArchive code at the lines in §6, plus the sealed commit and inventory
    (`container/store.go:639-656`, `:704-746`, `:867-995`, `:1019-1089`), the start sweep
    (`app/encrypted.go:32-85`, `export/units.go:299-362`), and the index stamp sites
    (`index.go:205-221`, `:798-881`): yes;
  - go-mbox v1.0.4 `reader.go`: yes, in full;
  - the completeness design at 58e2542: by grep and the detection and account passages only.
- **Not checked, per claim:**
  - **F1 is from reading the code, not from a run.** No test or encrypted archive was exercised.
    That the start sweep removes a unit listed "retained" is read at `store.go:730-735` and
    `units.go:353`.
  - That a merge-step `-resume` would run `prepareEncrypted`: not traced (the code does not exist
    yet).
  - Thunderbird source: not read.
  - The owner's profile and archive: not read by this check.
- **The smallest check that would settle F1:** a unit test on a scratch encrypted archive. Commit
  a `View` with one record removed, open the store again, run `SweepUnits` with `MayRemove`, and
  see whether that record's units are gone.
- **Borrowed lines:** short quotes of the design, the review and the cited code, each attributed
  where it appears.
- **Drift:** none noticed. This check stayed on the design as frozen at 832a02f.
- **Touched:** this file (new) and one commit. No design text and no product code was changed. An
  empty scratch file this check created by mistake under the repo's `.claude/` was removed.
- **Persona:** none.
- **Source reached, per source:** design yes; review yes; repo code yes (as listed); go-mbox yes;
  completeness design partly; Thunderbird source no; owner profile no.

Written by a model answering under no name, a fresh instance that has not met the record before,
for Alex DiMarco, under the Limen method.
