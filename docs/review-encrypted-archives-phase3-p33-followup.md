# Follow-up adversarial pass — encrypted archives Phase 3, slice P3-3 (the fix round, 3f and 3g)

**Reviewed commit:** `a6cec3f` (branch `encrypted-archives`): the first P3-3 pass's fix round
(`dd984cd..a6cec3f`) and sub-slices 3f and 3g (`84c4449..dd984cd`), which the first pass did not
cover.
**Date:** 2026-10-27
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`), owed by
the first pass's review (docs/review-encrypted-archives-phase3-p33-adversarial.md).

**Run:** workflow `wf_f1deb082-1f0`. Four read-only finders with distinct lenses:
- LIFE: the fix round's key lifetimes and new traps;
- MAKE: making an encrypted archive in the Desktop (3f, 3g);
- DRIVE: someone who can write the archive drive, against the fixed code;
- FIX: whether each fix did what the review claims.

Each proved its claims in its own copy. Two refutation-default skeptics reproduced or refuted each;
one completeness critic followed with its own proofs.

**Tallies:** 17 found / **17 confirmed**, none refuted; the critic proved 4 more (CRIT-01..04, CRIT-03
a trap in a proposed fix). No high; medium by the skeptics: MAKE-01, LIFE-01, FIX-02, FIX-03, and the
critic's CRIT-02; the rest low.

## The findings and what was done

| Id | Finding | Done |
|---|---|---|
| LIFE-01, CRIT-01 | a reader being made outlived a Lock and Shut down; a Lock during it answered "could not be opened" | the build's lease is on the entry and locked with it; Shut down waits (bounded) for the build to give up; a lock seen by the build is the locked reply |
| LIFE-02 | a replaced item's recovery file voided under the registry's lock | replaced items end off the caller's locks |
| LIFE-03 | a recovery file written and voided under the store's lock | written first, attached only if its key still waits, the old one voided off the lock |
| LIFE-04 | a refused uninstall's reopen undid Shut down | Shut down and an uninstall under way are separate reasons; reopen undoes only the uninstall's |
| LIFE-06, CRIT-02 | forget-all missed a put-back and an unlock with Remember still under way | a forget count checked as a put-back is kept, under the registry's lock; while forget-all runs every open is refused and those under way finish before the forget |
| FIX-01 | the password change's lock watch did not stop a change waiting for its turn | the turn is taken before the key; the derivation itself is not stopped (accepted, §16) |
| MAKE-01 | a new archive whose first backup could not start had no run job | the run job is written as the archive is made (`PendingAdd.WriteRunJob`) |
| MAKE-02 | its failure was worded for the current archive | `runJobOn` words a job's failure for its own archive |
| MAKE-03, LIFE-05 | a second password replaced a key, leaving a recovery file for the first | refused once the key is made |
| DRIVE-01 | Dismiss could still clear a planted id with both copies replaced, or one copy left | only the open archive's own id is cleared; locked, it is refused |
| FIX-02 | three of the standing red's five cases had no guard | guarded |
| FIX-03 | several of the round's wirings had no guard | guarded where reachable through the handlers (forget-all under way, a refused uninstall, Shut down's store, the password change); the rest as listed below |
| FIX-05 | status called the messages intact of an amber that kept a red standing | it says the check could not open every message |
| FIX-06 | 3f claims untested | the cancel test now proves a confirm after Cancel is refused; the recovery file's name is its own archive's |
| FIX-07 | the pipe-swap test raced its cleanup | it waits for its swapper |
| CRIT-04 | CI's `-race` line missed the new guards | widened to match them |
| FIX-04 | a no-verb lock masks a dead backup while a keyed extract or forget runs | **accepted** (§16): the critic proved the proposed fix would mark every live keyed backup red; a verb would say what ran |

**Not done in this round, by decision:** FIX-06's Outlook refusal and "the first backup's key is
destroyed when it ends" need a wizard-side seam for an Outlook pending add and a view of the create's
key; FIX-03's put-back and create failed-commit discards are guarded for the new recovery key only.
These are for P3-8's test pass.

## Proofs

- The guards are the finders' reproductions turned to the fixed behaviour (MA-825,
  `internal/desktop/keys_pass2_test.go`, with `-race`), and the standing red's cases in
  `internal/app/verifyfull_standing_test.go`.
- The unfiltered suite is green.
