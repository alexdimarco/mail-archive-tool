# Adversarial pass — encrypted archives Phase 3, slice P3-2b (verify -full)

**Reviewed commit:** `cd7e46a` (branch `encrypted-archives`; the slice is `dad1b7e..cd7e46a`)
**Date:** 2026-10-26
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers the check with the key: `app.VerifySealed` and
`app.SealedCheck`, `container.CheckChunks` and `CrossCheck`, `keyring.VerifyFull`, the record's
tier and the standing red (`state`, `health`), and `verify -full` at the command line.

The owner asked for each slice's pass to run without a separate approval ("Lets keep going and get
this done", 2026-10-25), and on 2026-10-26 to decide what is clear and raise only what is not.

**Run:** workflow `wf_a9de05c3-3ee`. Five read-only finders with distinct lenses:
- VERD: verdict correctness;
- REC: the record and status rules;
- OUT: someone who can write the archive drive during the check;
- KEY: key lifetime and locking;
- FID: fidelity, regressions and test strength.

Each proved its claims in its own copy. Two refutation-default skeptics reproduced or refuted each;
one completeness critic followed with its own proofs.

**Tallies:** 27 found / **24 confirmed** / 2 merged as duplicates (KEY-01 into REC-02, FID-03 into
REC-07) / 1 refuted (FID-05: the truncation lines' order and the result line's `tier=` are human
output in a fixed order, read by nothing that parses them). The critic proved 5 more
(CRIT-01..05, two of them traps in proposed fixes). By the skeptics' grading: no high; 4 medium
(VERD-01, FID-01, FID-02, REC-05 — the critic regraded REC-05 to low); the rest low.

## The findings and what was done

The central defect (FID-01, VERD-01, with VERD-03 and CRIT-01): tier 2 started from the keyless
verdict and never lowered it, so a keyless red that is not lost mail — a missing copy of the record
pointer — became a tier-2 red, worded with the tier-2 "messages are damaged" sentence, and stood.
**Fixed** per reason, never by "the key saw past it" (the critic's trap): the keyless verdict keeps
its own remedy; a pointer-copy red (`Check.PointerCopyOnly`) becomes amber with "run a backup"
once the keyed load authenticated the record; every other keyless red keeps its outcome — a link
where a folder should be stops every backup, so it stays red.

| Id | Finding | Done |
|---|---|---|
| VERD-02 | a refused load showed tier-2 wording and file counts as "message files" | the summary says no message was opened; counts labelled as the keyless check's files |
| VERD-04 | too-large units uncounted; advice "a computer with more memory" untrue (fixed limit) | `Report.TooLarge` counted and shown; advice names MailArchive's own limit |
| VERD-05 | a lost go-back stretch or an index behind, kept only in the header, read intact | `SealedCheck.Marks` (`Store.RecordMarks`): amber with each remedy |
| FID-06 | two ambers kept the first reason; merged advice read wrongly | equal-rank findings join reasons and remedies; the history remedy names the timeline |
| OUT-1 | a moved archive under a lock-free check was hidden by a red | the change replaces any verdict: not attested, "check it again" |
| OUT-2 | both copies of a record part damaged after load read "the other copy is good" | both copies of one position is red with the restore |
| OUT-3 | every refused load got "restore the whole folder" | each refused load's remedy is its own outcome's; the restore only for damage |
| OUT-4 | EROFS alone sent the check lock-free beside a live holder | `lockfile.HeldReadOnly` probe; held is refused |
| CRIT-04 | a Chunks error other than stop/lock was dropped, attesting unopened chunks | amber "the record's files could not all be read" |
| CRIT-05 | a permission or I/O error on a unit was a standing red | amber "could not be read here"; red only for gone, not its record's bytes (`export.ErrUnitStore`), or does not open (`app.ErrUnitDoesNotOpen`) |
| FID-04 | a gone unit listed twice; the cap started from zero | one entry per file, the key's finding and part; the cap counts keyless problems |
| KEY-02 | Chunks held the key across all disk reads | `CheckChunks` takes a per-chunk hold; the key is held only to open each chunk |
| REC-02 (KEY-01) | a lock with no earlier record left a running record | `state.SnapshotLastVerify` puts the record back byte for byte, or none |
| REC-01 | keep-red froze the record's clock: a daily verify read as stopped | `LastChecked` beside the standing red; status judges staleness by `CheckedAt` |
| REC-06 | keep-red hid a newer keyless red | the newer red is recorded with the standing one beneath (`Standing`); both told; the standing returns to the top after |
| CRIT-03 | (trap) a tier-1 top record would delete the red notice and lose the red | the standing red stays at the top unless a newer red replaces it; the notice is judged from the top, always red |
| REC-03 | a tier-2 amber after a tier-2 red left the red notice | a check with the key that finds only amber removes it |
| REC-05 (CRIT-02) | status and the notice never said only -full clears a tier-2 red | both name `verify -full` (and the restore) |
| REC-04 | "a check without the key cannot see that" said of reds it can see | worded by outcome |
| REC-07 (KEY-03, FID-03) | the stop line and status named plain verify for a check with the key | `-full` named |
| FID-02 | the real sealed side's reads could be switched off with every test green | real-archive keyring tests whose findings only the key's reads produce |
| FID-07 | a busy archive asked for the key first; key flags on plaintext said "add -full" | the busy refusal comes first; plaintext says not encrypted |
| FID-08 | help: "nothing recorded", "only -full clears what -full found", no -remember | corrected |

All are covered by MA-803 (and MA-799 for the original rows), each proved failing first.

## Residuals, stated

- A lost stretch of the go-back timeline stays amber at every later `verify -full`: the owner's
  ruling is that it stays partial for good, and nothing but a backup copy of the folder restores
  it.
- The read-only lock probe sees a holder on the same host or a share that passes locks through; a
  writer on another host behind a share that does not pass locks is not seen, and is caught only by
  the pointer peek before and after.
