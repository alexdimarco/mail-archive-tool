# Adversarial pass — encrypted archives Phase 3, slice P3-4 (the unencrypted copy)

**Reviewed commit:** `98234b0` (branch `encrypted-archives`; the pass covers P3-4 steps 0–7, from
`03830b9`: the plan, the copy engine, `keyring.DecryptCopy`, `mailarchive decrypt` and the
Encryption card's "Make an unencrypted copy").
**Date:** 2026-10-27
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. The owner asked for each slice's pass to run without a separate approval
(2026-10-25), and to decide what is clear and raise only what is not (2026-10-26).

**Run:** workflow `wf_501604f6-c01`. Five read-only finders with distinct lenses:
- KEY: key lifetime, Lock and Shut down during a copy;
- DEST: where the copy lands, and whether that place can be swapped;
- LEAK: what leaves this computer or the chosen folder;
- FAIL: failure, stop and partial states;
- FID: fidelity to the design and the strength of the tests.

Each proved its claims in its own copy. Two refutation-default skeptics reproduced or refuted each;
one completeness critic followed with its own proofs.

**Tallies:** 29 found / **24 confirmed** / 4 merged as duplicates (KEY-01 into FAIL-03, DEST-04 into
LEAK-02, FID-01 into FAIL-01, FID-04 into FAIL-05) / 1 refuted (FID-11). The critic proved 6 more:
CRIT-01..03 are defects or gaps; CRIT-04..06 are traps in proposed fixes. By the skeptics' grading:
no high; medium — DEST-01, DEST-02, LEAK-01, LEAK-03, FAIL-01, FID-03, FID-05, and the critic's
CRIT-01; the rest low.

## The findings and what was done

The two central defects:
- **Where the copy lands (DEST-01, DEST-02, LEAK-01, LEAK-02).** The copy's folder could be
  swapped after the last check, and the "may leave this computer" check (K5) read only the path as
  typed. **Fixed:** the copy holds the folder first and compares it with its path before and after
  the last checks. The plan records the folder's identity, and the start refuses another. K5 is
  judged where links lead, and judged again at the start.
- **What the Desktop said when a copy failed (LEAK-03, FAIL-01).** It said "the backup failed" and
  never named the folder of readable mail left behind. **Fixed:** the copy words its own failures
  and names that folder.

| Id | Finding | Done |
|---|---|---|
| DEST-01 | the parent swapped for a link after the recheck received the copy | the folder is held first; held and path compared before and after the checks; the plan's identity compared at the start (MA-828) |
| DEST-02, LEAK-01 | K5 read the path as typed: a link into a synced folder, onto a removable drive, or to Desktop/Documents asked once | judged on the spelling and where links lead; Desktop/Documents also by identity (MA-828) |
| LEAK-02 (DEST-04) | the start did not judge K5 again | Recheck runs it again; a new reason makes the plan stale (MA-828) |
| DEST-03 | the index, pages, report and README are written by path | the held folder is compared with its path before and after those steps, and the manifest checked through the handle; writing them through the handle is not done this slice (§14) |
| DEST-05 | the CLI checked no other archive by identity | the plan reads the archives this computer knows (the Desktop's list, its current archive, the wizard's last), for both front ends (MA-828) |
| LEAK-03, FAIL-01 (FID-01) | the Desktop said "the backup failed" and never named the unfinished folder | the copy's own words, naming the folder as READABLE, to delete; a stale plan says plan again; the wrapped cause is never shown (MA-830) |
| LEAK-05 | the pages' sort could spill subjects to the system's temporary folder | a small index (≤ 256 MiB) keeps temporary work in memory; a larger one's spill is in §14 (MA-829; CRIT-04 heeded) |
| FAIL-02, FID-10 | lock-free, the record was compared with a "before" read too late | compared with the plan's own record (MA-829) |
| FAIL-03 (KEY-01) | a stop after the last message was ignored, exit 0 | a stop is heard before the history, before the index, inside a rebuild and before the pages (MA-829); CLI exits 130 (MA-830); no CLI backstop (CRIT-06 heeded) |
| FAIL-04 | reserved names were case-sensitive | compared in lower case, every segment checked against the page names; the README must be a regular file (MA-829) |
| FAIL-05 (FID-04) | a copy failed at its last checks held a manifest | the manifest is written last, and taken away whenever the copy is left unfinished (MA-829) |
| FAIL-06 | a kept index listed messages left out | the index is rebuilt from the copied files when anything was left out (MA-829) |
| CRIT-01 | a sealed index marked behind was copied as is | the copy rebuilds it from the copied files (`SealedCopy.IndexBehind`) (MA-829) |
| FID-05 | the copy's go-back view never said it was partial | a gap the record marks is written as a barrier; the plaintext reader treats a barrier as partial (MA-829) |
| KEY-02 | a Lock does not stop a running copy, and nothing said so | by design; said where the person acts — the plan, the start reply, and the Lock reply while a copy runs (MA-830) |
| KEY-03 | the docs said a lock mid-copy stops the copy | DecryptCopy's comment and §16 step 4 corrected; the mid-copy branch kept as defence |
| CRIT-02 | the card said the copy "shows in Activity as it goes"; it showed nothing until the end | a counts-only progress line to Activity, at most every few seconds (MA-830) |
| CRIT-03 | a stopped copy is not remembered after a restart | **owner's call, pending.** Built as option B: the plan, the CLI and §14 say so; no tracker |
| FID-02 | the start's recheck was unguarded | MA-828's tests reach it through DecryptSealed |
| FID-03 | the lock held during the copy was untested | a backup's lock taken mid-copy is refused (TestTheCopyHoldsTheLockAndItsFilesModes) |
| FID-06 | the report and history content were untested | compared with the archive's (TestTheCopysReportAndHistoryAreTheArchives) |
| FID-07 | the CLI's exit 3 and 130 were untested | tests for both, and for no index → 3 (MA-830) |
| FID-08 | Desktop claims untested | Shut down mid-copy, K5 confirmed twice, Lock while copying (MA-830) |
| FID-09 | smaller untested claims | graph refuses an unfinished copy; file and folder modes; the iCloud reason in the plan |
| FID-11 | the room check skipped records without fixity | refuted |

## Traps heeded

- **CRIT-04:** temp_store=MEMORY for every size would grow without bound. It is used only below the
  reader's budget.
- **CRIT-05:** compare-before-and-after does not stop a swap there and back. It is said in §14
  rather than claimed closed. A handle-path check (F_GETPATH, /proc/self/fd,
  GetFinalPathNameByHandle) is a later hardening.
- **CRIT-06:** no CLI backstop. The engine stops, and a copy that finished reports success.

## The sweep

The mutation sweep (`mutate3p4pass.py`) ran 38 mutants: **37 killed, 1 survived**.
- The survivor removes the stop check straight after the history. It is caught later, with the
  same outcome: the check before the pages, inside a rebuild, and before the mark comes off.
- Two mutants were first written so they did not build. They were rewritten and killed.

The fixes owe the P3-4 follow-up pass, run with P3-5's pass after PG3.
