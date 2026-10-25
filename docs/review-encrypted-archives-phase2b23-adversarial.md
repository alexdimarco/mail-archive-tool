# Adversarial pass — encrypted archives slices 2b-2 and 2b-3

**Reviewed commit:** `3571519` (branch `encrypted-archives`)
**Date:** 2026-10-25
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change: the sealed go-back history (`internal/container/history.go`), the sealed search
index and its lazy, in-memory and scratch-file forms (`internal/container/index.go`, `store.go`,
`record.go`; `internal/index/lazy.go`, `memory.go`), and their wiring (`internal/app/sealedindex.go`,
`encrypted.go`, `run.go`, `graph.go`, `scratch.go`; `internal/uninstall/scratch.go`). The owner approved
the pass on 2026-10-25.
**Run:** workflow `phase2b-23-adversarial` (`wf_363505d8-6a0`, 9 agents). It had five read-only finders with
distinct lenses: OUT (someone who can rewrite the archive folder), LOC (another process or user on the
machine, and the scratch file), CON (crashes and concurrency), LEAK (cleartext) and FID (fidelity and test
strength). Each finder proved its claims with failing tests in its own scratch copy. Three
refutation-default skeptics each reproduced or refuted a share of the findings against an unmodified copy
of `3571519`, and one completeness critic followed. Tool versions: Go 1.26.0.
**Tallies:** 21 found / **17 confirmed** (5 medium, 12 low) / 4 refuted. The 17 merge to 12 distinct
defects: OUT-01 = CON-05, OUT-02 = CON-04 = FID-01, LOC-02 = FID-03, LEAK-01 = LOC-03. The critic proved 3
more: CRIT-1 (medium), CRIT-3 (medium), and CRIT-2 (low, a trap in the obvious fix for OUT-01).

## Outcome

All confirmed findings and the critic's three are fixed. Each fix has a regression test (catalog MA-760,
MA-761, MA-762, plus MA-758 and MA-759 for the two test gaps). Each test was shown red against its fix
disabled by a mutation sweep (below). The design is amended in `docs/design-encrypted-archives-phase2b.md`
§12. With the fixes, the owner's ruling of 2026-10-25 ("keep and warn") is built (MA-763). An encrypted
backup that finishes with its search index still behind records a successful run with the kind
`index_behind`. `status` and the Desktop then warn about it without needing the key.

One new defect was found while fixing CON-01. When a batch is lost because the scratch drive filled
during its commit, the index can be left inconsistent: the test's next run failed with a SQLite
primary-key constraint error. Such an index is now treated as unsound. It takes no more messages and is
not sealed again that run. The record keeps the last sound image, and the next run catches it up.

Residuals, stated:
- The late-write guard's RunGraph line, and a failed scratch load's own cleanup, are not separately
  provable.
- A crash-left scratch file now lasts until the archive's next keyed run that reaches its lock, at the
  same location. A run refused before the lock (archive paused, source gone) does not remove it, and a
  file left at an old path of the same archive waits for uninstall. The design's R-E2 text is narrowed to
  say so.
- The index's re-seal bytes per run and its peak memory are still estimates; no measurement was made.
- Windows behaviour was not exercised: unlinking an open file, and a virus scanner holding the scratch
  file.

## Findings

| ID | Lens | Claim | Verdict | Evidence / refutation | Fix | Tombstone test |
|---|---|---|---|---|---|---|
| OUT-01, CON-05 | outside, concurrency | The `history_gap` recorded for an unreadable open last history chunk names the current commit's generation, not the generations whose events were lost (container/history.go:199, 237-242). | CONFIRMED (low) | Reproduced by skeptic 1 (TestSK1_OUT01): lost chunk sealed at gen 3, recorded gap {4,4}. | The gap reaches back to the generation after the previous chunk's seal. | TestAnUnreadableLastHistoryChunksGapCoversTheGenerationsItHeld |
| CRIT-2 | critic | The obvious fix (start the gap at the lost chunk's own generation) is still wrong: an open chunk is re-sealed every commit, so it holds events from many earlier generations. | PROVED (low) | TestCRIT2_LostChunkSpansEarlierGenerations: one open chunk at Entry.Gen 4 held runs 1, 2 and 3. | As above: the previous chunk's generation + 1, never the lost chunk's own. | the same test, with a chunk started after a full one |
| OUT-02, CON-04, FID-01 | outside, concurrency, fidelity | A real history loss is erased when a later failed history step's events land: `apply` cleared the whole gap whenever a staging hold landed (history.go:155-158). | CONFIRMED (low) | Reproduced by skeptics 1 and 2: gap {3,3} becomes nil after a later landing, while History() still shows the barrier. | The loss and the hold are kept apart in memory; only the hold is cleared. | TestAHeldHistoryLandingKeepsARealLossGap |
| FID-02 | fidelity | A landed hold is cleared only in memory after the landing commit already sealed it, so the run's last commit seals a false gap for good. | CONFIRMED (low) | TestSK2_FID02: reopened, gap {2,2} with all events present. | The landing commit seals the header without the hold. | TestAHeldHistoryGapIsClearedOnDiskWhenItLands |
| CON-06 | concurrency | A failed commit leaves its index row count in the in-memory header; a next commit that cannot capture seals that count beside the old image. | CONFIRMED (low) | Reproduced (sk3_con_test.go): rows 50 recorded beside the old image. | A commit that does not land restores the header's row count (and a cleared hold). | TestTheRowCountOfAFailedCommitIsNeverSealedBesideTheOldImage |
| LOC-02, FID-03 | local machine, fidelity | A catch-up whose add fails (scratch drive full, or a refusing row) counts the message unreadable and clears the behind mark, so the message is out of search for good. | CONFIRMED (medium) | Reproduced by skeptic 3 with RLIMIT_FSIZE and with a refusing trigger. | Add failures are counted apart; any failed catch-up keeps the mark, and sets it for an index that was only absent. | TestACatchUpThatCannotAddAMessageKeepsTheIndexBehind |
| CON-01 | concurrency | Rows whose adds succeeded but whose batch commit failed are sealed as a whole index, not marked behind. | CONFIRMED (medium) | Reproduced by skeptic 1 with RLIMIT_FSIZE after the run's first add. | The capture marks the index behind when a batch was lost, and does not seal that unsound index again that run. | TestAnIndexBatchLostAtItsCommitMarksTheSealedIndexBehind |
| CRIT-3 | critic | A new sealed index that cannot be opened (scratch folder unwritable) stops the whole backup in RepairKeys. | PROVED (medium) | TestCRIT3: run error `migrate search index keys ... permission denied`; the new message was not backed up. | On the sealed path a RepairKeys failure drops the index for the run and marks it behind. | TestAFreshIndexThatCannotBeOpenedStillBacksUp; TestAGraphCaptureKeepsTheSealedIndexWhole |
| CRIT-1 | critic | The memory budget is checked only at open; a first backup always starts in memory and grows without bound. | PROVED (medium) | TestCRIT1: a 3.6 MB image held in memory against a 256 KB stand-in budget. | Every 256 adds the in-memory size is checked; past the budget the index moves to its scratch file, or with no room takes no more messages and is marked behind. | TestAFirstBackupThatOutgrowsMemoryMovesItsIndexToTheScratchFile; TestAnInMemoryIndexMovesToAScratchFile |
| LOC-01 | local machine | A `?` in the cache path makes SQLite open a different file outside the scratch folder (mode 0644, never removed); the scratch file was opened by raw path. | CONFIRMED (medium) | Reproduced with the production scratch root under HOME `.../we?ird`. | Opened through the escaped `file:` URI, as `index.Open` is. | TestAScratchIndexUnderAPathWithAQuestionMarkStaysInItsFolder |
| LEAK-01, LOC-03 | leakage, local machine | A crash-left plaintext scratch index was removed only by a later run with indexing on that reached the index. | CONFIRMED (medium) | Reproduced: still present after an index-off run. | Every keyed run removes it once under the lock (`openEncrypted`), whatever its `-index`. R-E2's text narrowed for runs refused before the lock. | TestACrashLeftScratchIndexIsRemovedByARunWithIndexingOff |
| LEAK-03 | leakage | A scratch index that cannot be removed at a run's end is dropped in silence (bare `defer idx.Close()`). | CONFIRMED (low) | Reproduced with the index folder at 0500. | The sealed path logs a failed close. | TestAScratchIndexThatDoesNotCloseCleanlyIsSaid |
| LOC-04 | local machine | Two archives whose folders differ only in case share one scratch folder (the source id folds case on macOS and Windows). | CONFIRMED (low) | Collision reproduced; mixing mail between them was not shown (critic). | The scratch folder hashes the location and the archive id. | TestTwoArchivesNeverShareAScratchFolder |
| FID-05 | fidelity | No test exercises the sealed-index wiring in RunGraph; commenting out all five pieces kept every app test green. | CONFIRMED (low) | Reproduced by mutation in a separate copy. | Tests added: indexing off marks behind, catch-up, failed add, late damage healed, scratch route, a new index that cannot open. The late-write guard's graph line is not separately provable. | TestAGraphCaptureKeepsTheSealedIndexWhole |
| FID-06 | fidelity | No test proves the records estimate alone moves the index to scratch. | CONFIRMED (low) | Mutation `indexScratchNeed(image, 0)` survived every MA-759 test. | Test added. | TestTheRecordEstimateAloneMovesTheIndexToScratch |
| CON-02 | concurrency | The catch-up compares keys only, so a message rewritten while the index was behind keeps its old content in search. | REFUTED | Design-stated (§6: the catch-up adds what the index lacks); it matches the plaintext path. Noted for Phase 3. | — | — |
| CON-03 | concurrency | A unit that fails to read once during a catch-up is left out for good. | REFUTED | Design-stated (§6: a record that cannot be read is logged and left out). The critic agreed, and asked that the owner be told a one-off read error is never retried; recorded in §12 for Phase 3. | — | — |
| FID-04 | fidelity | Folders moved while the index was behind stay stale in search after the catch-up. | REFUTED | Design-stated (§6), same as CON-02. | — | — |
| LEAK-02 | leakage | A scratch file at an old path of the same archive (a remounted drive) is never removed by a run. | REFUTED | The scratch folder is per location by design; uninstall removes it. R-E2's window now says "at the same location". | — | — |

## Mutation sweep (prove-fail)

Each fix was disabled in turn in a scratch copy, and the covering tests were run
(`scratchpad/mutatepass2b3.py`). 34 mutations: **33 killed, 1 survived.** On the first round three
mutations did not build, because they left a variable unused; they were rewritten to keep it used, and
all three were then killed.

- **MA-760, history and the row count (7, all killed):** the gap started at the lost chunk's own
  generation (CRIT-2's wrong fix); the gap started at the current generation; the loss not kept apart; a
  reloaded gap not treated as a loss; the hold not cleared before sealing; a failed commit keeping its row
  count; the header restored after a landed commit.
- **MA-761, an honest index (11, 10 killed):**
  - Killed: a catch-up add failure counted as unreadable; a failed catch-up not an error; a failed
    catch-up not marked behind; a lost batch not marked behind; an unsound index still sealed; a
    fresh-index fault stopping the backup (local run and Microsoft 365, each); the budget never checked;
    no room not marked behind; the budget check never consulted.
  - **Survived:** removing `drop`'s own `MarkIndexBehind`. On every path a test can produce, the
    load's `onBad` has already marked the index behind before `drop` runs. `drop`'s mark matters only
    when RepairKeys fails on an index that loaded cleanly, which no test reaches. It is kept as a defence
    and recorded as not separately provable.
- **MA-762, the scratch file (6, all killed):** opened by raw path; the move resetting the change count;
  a moved index still sized as memory; a crash leftover kept by an unindexed run; the scratch folder
  named by location only; a failed close not said.
- **MA-763, keep and warn (5, all killed):** the kind not recorded; the local run's result never behind;
  the Microsoft 365 run's result never behind; status not warning; the Recent-backups card not saying it.
- **FID-05 and FID-06, the wiring (5, all killed):** in Microsoft 365 captures, indexing off not marked,
  no start catch-up, a failed add not marked, late damage not healed; the records estimate ignored.

The unfiltered `go test ./...` passed after the fixes, and again after the last test change.
