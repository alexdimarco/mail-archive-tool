# Fold check 3: Gmail de-dup design, the M1 to M3 folds, §20 and §21

Written by a model answering under no name, a fresh instance that has not met the record before.

**Verdict: FAIL (narrow), for slice D2 only.** Nothing here touches B2, B3 or D1; B3's fold passes.

M1, M2 and the un-merge order hold. Three things are still wrong before D2 is built. Two of them are
about the rebuild that M3 added (the step that rebuilds a missing page or zip from the copy's `.eml`):

- **N1. `RebuildOwed` is not honoured by `reindex -rebuild`.** `RebuildOwed` is the new mark that stops
  `reindex` deleting a put-back record whose page is still missing. The design makes plain `reindex`
  honour it, but **the Rebuild command** (`app.Rebuild`) also drops every record whose page is missing
  (`internal/app/reindex.go:367-374`). Three places run it: the command `mailarchive reindex -rebuild`
  (`cmd/mailarchive/main.go:852`), the Desktop's "Rebuild search index" button
  (`internal/desktop/maintenance.go:228-236`), and the wizard's rebuild offer
  (`internal/wizard/wizard.go:1292`). Example: a copy is put back with `RebuildOwed`, and the person then
  clicks Rebuild search index. The record and its mark are gone. Its `.eml` stays on disk but no record
  owns it, so it is not shown as an email: exactly what §19 forbids. The next backup then sees the
  email as new. It exports it at the same file name (`stemFor` finds no owner, `exporter.go:1055`) and,
  with `-raw`, writes over that `.eml` (`exporter.go:513-519`). That is the hazard the design gave as
  its reason for not using `Missing`.
- **N2. "The normal export path" replaces the whole record it puts back.** The exporter builds a fresh
  `Record` and stores it with `e.Manifest.Add` (`internal/export/exporter.go:662-767`). It does not carry
  the copy's memberships, first-seen data or labels from the record it replaces. If the zip write fails,
  the export does not fail: it files every attachment as a gap (`:600-622`), and on a source that is
  not complete those gaps go to `Missing` (`:663-667`). The design says only "the new page and zip
  digests go into the restored record's `Fixity`" (design line 1783). It does not say the rest of the
  export's record is thrown away. Followed literally, a failed zip write puts back a fillable record
  (one a later backup may refill from the mailbox). The next fill with `-raw` then writes over the
  `.eml` (`:443-445`, `:514-519`). This is the same hazard as N1, reached a second way.
- **N3. An old test row still asserts the superseded behaviour.** MA-1229's first row (design line 2032)
  still says "a file changed between plan and deletion is kept in `AlsoFiles`" and is not marked. The
  amended row (line 2083) makes exactly that a mutation that must turn the test red. A builder working
  from the first row would build M1's bug into the test.

Each is a design-text change to §9's fold-check-2 note and §12, re-checked on its own before D2.

- **Date worked on:** 7 November 2026 (the session's date, not inferred).
- **What was checked:** `docs/design-gmail-dedup.md` at 16057ec (branch `gmail-dedup`), sha256
  `ec51a72318b5b8acfaa66154ca5ebfc2855d158d14712211e1c351a82e9de730`. The folds were read as
  `git diff 082a340..16057ec`, plus §9 step 5, §18, §19, §20 and §21.
- **Order of reading:** fold check 2 (`docs/review-gmail-dedup-foldcheck2.md`, 082a340) first, then
  the code, then the design's diff and sections. The caller's brief named the questions, not answers.
- **Procedure:** read-only. No product code was changed or run; no test was run.
- **Register:** a finding report from reading code and design text. The suggested fixes are this
  checker's opinion.

## 1. Results

| # | Question | Result |
|---|---|---|
| 1 | M1: an un-merged copy's files are owned by its own record only; deleting the survivor removes none | **PASS** (one finding: no Desktop single-email delete exists) |
| 2 | M2: a damaged survivor or entry un-merges the whole group; nothing kept is reachable only through verify | **PASS** (two should-fix) |
| 3 | M3: the rebuild is the normal export path, never writes the `.eml`, never overwrites; `RebuildOwed` honoured by every deleter | **FAIL**: N1, N2; partial on "never overwrites" |
| 4 | U1 to U4 and a crash after each; `Before`, `SurvivorFolders`, `Unmerged` | **PASS** (two should-fix, wording) |
| 5 | The yes with the plan's left-out records | **PASS** |
| 6 | The encrypted refusal inside `openEncrypted` | **PASS** |
| 7 | B3's three refusals | **PASS** |
| 8 | §21's calls | **PASS** for byte safety (one should-fix: stop the whole resume) |
| 9 | Contradictions with older text | **N3**, plus two wording clashes (§5) |

### 1.1 M1 (PASS)

- Redaction removes a pruned record's own three files and every `AlsoFiles` file, and nothing else
  (`app/reindex.go:158-179`). With the fold, an un-merged copy's files are named only by its own put-back
  record (design line 1739-1741), so deleting the survivor reaches none of them. The copy's file name
  comes from its own key (`stemFor`, `export/exporter.go:1029-1059`), so it never shares the survivor's.
- Other paths that remove files, re-read: the re-export rename removes only that record's own old files
  (`exporter.go:789-797`); `Rebuild` deletes no file, it only drops manifest entries
  (`reindex.go:358-416`); `SweepOrphans` removes only temp files and zips with no page
  (`export/atomic.go:110-140`); deleting a whole archive is the person's decision on every file, and it
  refuses while a merge is pending (S3). A grep for `Manifest.Delete` finds only `Reindex` and `Rebuild`.
- **Finding (answers §21's third call):** the Desktop has **no single-email delete**. Its routes
  (`internal/desktop/dashboard.go:349-465`) offer no delete or forget of one message. One email is
  deleted only by removing its page by hand and running `reindex` (plain `reindex`, which the Desktop
  never calls). The Desktop's only path that drops records is the Rebuild search index button, which
  runs `app.Rebuild`. That is N1. §21's rule ("removes only that record's own files and never the
  survivor's") holds for plain `reindex`. The item should be re-stated as the Rebuild case.

### 1.2 M2 (PASS)

Every row of §18 and §20 was checked against the rule that a kept file is always named by a record
shown as a mail:

- a survivor that fails its re-hash un-merges every copy that still has a file (line 1750-1752);
- a damaged `MergedFiles` entry with a `Before` record stays in `MergedFiles`, and its group is marked
  for un-merge (line 983-990);
- a copy whose three files are all gone is not put back, and its folder stays on the survivor
  (line 1755-1761, §21);
- a copy that passed but whose removal failed goes to `AlsoFiles` on a survivor that is shown and holds
  equal bytes (line 1745-1746).

Each obeys the rule. The only exception is the one already named: a manifest changed by hand (§20,
§21). Two should-fix items are in §5: U2's membership formula, and stopping the whole resume.

### 1.3 M3 (FAIL)

- **Never writes the `.eml`: PASS.** With `KeepRaw` off, `writeRaw` is false (`exporter.go:443-446`), so
  the `.eml` write (`:513-519`) is not reached. The carried-original path keeps the `.eml` and its
  recorded digest when the record is in the working manifest and the file name matches
  (`:466-470`, `:849-861`). The rename that removes old files needs `prev.Path != relSlash`
  (`:789`). The fold's stem-equality rule rules that out.
- **Is the normal export path: contradiction.** The normal path writes the zip **before** the page
  (`exporter.go:539`, "Attachments first (zip, then html)"; `atomic.go:130-131` names a crash between
  the two). The fold says that when both are gone, "the page is written before the zip" (line 1779).
  Both cannot hold. No byte is at stake, because the zip is rebuilt from the `.eml` that is kept. A
  crash between the two leaves a rebuilt zip with no page. The next run's sweep may remove it, and
  resume redoes U1, because the copy is not yet in `Unmerged`. Should-fix in §5.
- **Never overwrites a file on disk: PARTIAL.** The fold covers "only the zip gone" and "both gone".
  It does not cover **page gone, zip present**, which is what the normal path's own crash (above)
  leaves. In that case the normal path would rewrite the zip (`WriteZip`, `:600`). It would even remove
  it (`os.Remove(zipPath)`, `:625-627`) if a new parse finds nothing to put in a zip. Neither loses a
  unique byte, but the fold's sentence is not literally true. Should-fix in §5.
- **The record the export writes: FAIL (N2).** See the verdict. `rec` is built fresh (`:662`) with
  this export's gaps, scheme, content digest and `ExportedAt`. The first-seen fields are carried only
  when `DedupMailboxWide` is on (`:723`, `:754-760`), and the copy's memberships are not carried at all. It is
  stored with `e.Manifest.Add` (`:767`), replacing the record put back from `Before`. `OnExported`
  (`:771`) would also add a search row, which U1 says it must not.
- **`RebuildOwed` and every deleter: FAIL (N1).**

| Deleter or pruner | Honours `RebuildOwed`? |
|---|---|
| `reindex` (`reindex.go:135-185`) | yes, as folded (line 1791) |
| `reindex -rebuild`, Desktop Rebuild button, wizard rebuild (`reindex.go:367-374`) | **no**: the record is dropped from the manifest, and its mark goes with it |
| Desktop single-email delete | does not exist (§1.1) |
| Archive delete with its marker | not needed: it removes every file at the person's word, and refuses while `MergePending` is set (S3) |
| The orphan-zip sweep (`atomic.go:132-140`) | not needed: a `RebuildOwed` record never has a zip without its page, except a rebuilt zip left by a crash in U1, which is derived and rebuilt again |

### 1.4 U1 to U4 and resume (PASS)

- **After U1:** the copy is still in `Before`, not in `Unmerged`. The re-check runs again, and a
  rebuilt page whose bytes differ from its entry's digest keeps the copy (all or none). No delete
  without proof.
- **After U2:** the copy is in `Unmerged`, and its `MergedFiles` entries are gone. Step 5's deletions
  work only on waiting `MergedFiles` entries (design line 1569-1572, "its waiting files"), so a resume's
  5.5 has nothing of this copy to delete. Steps 2 and 3 leave it out (line 1600-1602, 1840-1843), so its
  search row is not dropped and it is not re-merged. U3 runs again and changes nothing the second
  time (it is idempotent).
- **After U3 or U4:** the next run repeats the step and changes nothing more.
- No sequence found re-merges a put-back copy, drops a search row or deletes a file. Two wording
  should-fixes are in §5: 5.5 skipping `Unmerged` should be said, and so should the resume trigger.

### 1.5 The yes with the left-out records (PASS)

The left-out keys are in the plan's digest, in key order (line 1848). The recompute at the yes leaves
out the same keys before it groups. Results:

- **A recorded digest changes, a record is added, or the survivor changes:** the digest differs, so the
  yes is refused.
- **A different left-out list:** the digest differs, so the yes is refused.
- **A record damaged after the plan:** the recompute cannot see it (it uses recorded digests only), but
  5.5's re-hash catches it and un-merges the group (M2).

A left-out key can only keep a record apart, so no list can cause a delete. The list lives in the
server's memory, not in a browser POST (line 1846).

### 1.6 The encrypted refusal (PASS)

- `prepareEncrypted` has one caller, `openEncrypted` (`app/encrypted.go:444`). The only two start
  sweeps of an encrypted archive are inside it (`SweepUnits` at `:73`, `SweepContainersSplit` at
  `:77`).
- The sealed load, `container.LoadManifest` (`container/store.go:139-206`), removes nothing.
- Before the load, `openEncrypted` removes only the scratch index and the reader's scratch files
  (`encrypted.go:429-430`). Neither is an archived file.
- A refusal placed between `:438` and `:444` therefore covers the backup (`run.go:681`), the Microsoft
  365 path (`graph.go:206`), the keyed index rebuild (`sealedindex.go:359`), the program setting
  (`programsetting.go:49`) and the pause note (`pausenote.go:40`). Every one comes before any sweep.
- **Note:** the read-only keyed readers (verify, extract, decrypt, `keyring/*.go`, loads with
  `ReadOnly: true`) do not pass through it and do not sweep. They need no refusal for byte safety.

### 1.7 B3 (PASS)

All four places that open an index refuse one newer than `indexVersion`:

- `index.go:162`;
- `memory.go:76`, after `resolveIndexVersion` at `:71`;
- `lazy.go:47`;
- `listfolder.go:187`.

The fold names all four and the words they print. When a sealed image cannot be loaded, the lazy
open falls back to `OpenMemory(nil)` (`lazy.go:73`, `:87-88`), which stamps through
`resolveIndexVersion`. That is a stamping site the design already names.

### 1.8 §21's calls (PASS for bytes)

- **Stop when a `Before` record is missing or fails its checks:** nothing is deleted or written, so the
  call is safe. Two notes:
  - The fold's text says resume "stops **for that copy**" (lines 1803 and 2456), while §21 says "the
    resume STOPS … and changes nothing". If only that copy stops and the rest goes on to 5.6, then 5.6
    ("`MergedFiles` emptied") takes away that copy's entries. Its files are not deleted, but no
    record owns them any more. Should-fix in §5.
  - The archive stays refused until someone edits it by hand: there is no command out. The lead
    accepted that.
- **A fully deleted copy is not put back:** safe, since nothing is on disk for it. It relies on U2
  keeping that copy's folder on the survivor. Should-fix in §5.

## 2. Must-fix (before D2 only)

- **N1.** `RebuildOwed` stops `Rebuild` from dropping the record, as it stops `reindex`. Change
  `reindex.go:367-374` as follows: a `RebuildOwed` record whose page is missing stays in the manifest.
  It is indexed from its `.eml` when the `.eml` passes the same file check, otherwise it is kept
  without a search row. It is logged with the same words as `reindex` uses, and never dropped. Name
  all three callers (`main.go:852`, `maintenance.go:236`, `wizard.go:1292`). Restate §21's third call
  as this case, since there is no Desktop single-email delete. MA-1229 gains: a `RebuildOwed` record,
  then `reindex -rebuild` (and the Desktop button). The record is still in the manifest with its mark,
  and its `.eml` is byte-equal. Positive twin first: the same record without the mark is dropped, as
  today.
- **N2.** U1 keeps the record put back from `Before` as the record. From the export it takes only the
  new `Fixity.HTML` and `Fixity.Zip`. Every other field the export writes (`Missing`, `Terminal`,
  scheme, content digest, `ExportedAt`, the first-seen fields, `Folders`) is thrown away. `OnExported`
  stays unset, so no search row is written. An export that records any gap the `Before` record did
  not have, for example a failed zip (`exporter.go:600-622`), counts as a failed rebuild and sets
  `RebuildOwed`. MA-1229 gains: the zip write made to fail in U1. The put-back record has no new
  `Missing` label, it carries `RebuildOwed`, and its `Folders` equal its `Before` record's.
- **N3.** Mark MA-1229's first row (design line 2032), "a file changed between plan and deletion is
  kept in `AlsoFiles`", as superseded by [dedup foldcheck2 M1].

## 3. Should-fix (fold with D2)

- **Page before zip.** Line 1779 contradicts the normal path's zip-first order (`exporter.go:539`).
  Recommended: drop "the page is written before the zip" and accept the normal order. A crash leaves
  only a derived zip, and resume redoes U1 because the copy is not in `Unmerged` yet.
- **Page gone, zip present.** Name this case in U1: render the page and write only the page. Never
  call `WriteZip` and never reach `os.Remove(zipPath)` (`exporter.go:625-627`). Record the present
  zip's digest by reading it from disk.
- **Stop the whole resume.** In §9's D2 note (line 1803) and §20 (line 2456), "stops for that copy"
  should become "the whole resume stops before 5.6". Better still, check every `Before` record once at
  the start of resume, before any write, so §21's "changes nothing" is literally true.
- **U2's memberships** (line 1814-1815). Count a copy that was not put back (M2's fully deleted
  copy) among the copies that "stay merged", so its folder stays on the survivor as §21 says.
- **Resume wording.** Say that 5.5 works only on `MergedFiles` entries and so never meets an
  `Unmerged` copy. Read the resume trigger as "a copy record **not in `Unmerged`** still exists".
- **Note.** Record keys inside `MergePending` (`Survivor`, `Copies`, `Before`, `Unmerged`) do not go
  through `finish`'s key migrations, which work on `Entries`. A version-7 manifest is not re-keyed by
  the same build, so nothing is lost today. Say so, so that a later re-key also covers `MergePending`.

## 4. Counters

- **Date worked on:** 7 November 2026, from the session's clock.
- **Register:** a fold-check finding report from reading code and design text. The verdict, each
  item's severity, and the suggested fixes are this checker's opinion.
- **Drew on, and reached:**
  - fold check 2 (082a340): yes, in full;
  - the design at 16057ec: the diff from 082a340 in full; §9 step 5 (lines 1553-1606); the §9
    fold-check-2 note; §12's amended rows; §18's table; §19; §20; §21;
  - the code:
    - `app/reindex.go` in full;
    - `app/run.go:735-800`;
    - `app/encrypted.go:405-460`, and the callers of `openEncrypted`, `prepareEncrypted` and the
      sweeps, found by grep;
    - `container/store.go:139-206`, and its removal sites by grep;
    - `export/exporter.go:425-870` and `:1015-1065`;
    - `export/atomic.go:110-145`;
    - `index/memory.go:55-80`, `index/lazy.go:1-90`, and `indexVersion`'s uses by grep;
    - `state/manifest.go:760-800`;
    - the Desktop route list (`desktop/dashboard.go:349-465`), `desktop/maintenance.go:180-240`,
      `keyring/reindex.go`, `wizard/wizard.go:1275-1297`;
    - a grep for `Manifest.Delete`.
- **Not checked, per claim:**
  - Every finding comes from reading, not from a run. No test exercised `Rebuild` against a
    `RebuildOwed` record (it does not exist yet). No test made a U1 zip write fail.
  - Whether a Gmail (Thunderbird) export runs with `DedupMailboxWide` or `SourceComplete` set, which
    decides whether N2's gaps land in `Missing` or `Terminal`. N2 stands either way: the record is
    still replaced.
  - `listfolder.go` was read through grep only, at lines 187-189.
  - The completeness design, and the B-slice text beyond §7.4's B3 note.
  - Whether `index.Open` writes to `search.db` before `reindex`'s refusal (left open by the folds too).
- **Smallest checks that would settle N1 and N2:**
  - N1: in a plaintext test archive, delete one record's `.html` and run `mailarchive reindex -rebuild`.
    Then see whether the record is gone from `.mailarchive-manifest.json` while its `.eml` is still
    on disk. Today's code says it is.
  - N2: re-export one existing record through `Exporter` with `KeepRaw` off and a `BeforeWrite` hook
    that fails the zip. Then read the record: its `Missing` list and `Folders`.
- **Borrowed lines:** short quotes of the design and fold check 2, each attributed where it appears.
- **Drift:** none noticed; the check stayed on the folds at 16057ec.
- **Touched:** this file (new) and one commit; a commit-message file under the repo's `.claude/`
  folder. No design text and no product code changed.
- **Persona:** none.
- **Source reached, per source:** fold check 2 yes; design yes (the parts listed); repo code yes (as
  listed); completeness design no; the owner's profile and archive no.

Written by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method.
