# Adversarial pass — encrypted archives Phase 3, slice P3-2a (reading an encrypted archive)

**Reviewed commit:** `6bd9e2a` (branch `encrypted-archives`; the slice is `f8ac0cc..6bd9e2a`)
**Date:** 2026-10-26
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers:
- the memory guard (`internal/memguard`);
- the read-only load: `LoadOptions.ReadOnly`, `PeekFile`, `HistoryChecked`, the split unit read;
- the reader: `server.Source`, `server.SealedFiles`, `keyring.Reader`, `pages.Render`, GET-only;
- the read verbs: `serve`, `search`, `extract`, the read unlock; keyed `reindex -rebuild`;
- the reader's scratch index and uninstall.

The owner asked for each slice's pass to run without a separate approval ("Lets keep going and get
this done", 2026-10-25).

**Run:** workflow `wf_9d7aed2f-572`. Five read-only finders with distinct lenses:
- LEAK: confidentiality of decrypted content;
- KEY: key lifetime and locking;
- OUT: someone who can write the archive drive while it is read lock-free;
- REG: regressions for plaintext users and existing callers;
- FID: fidelity to §7 and test strength.

Each proved its claims in its own copy. Two refutation-default skeptics reproduced or refuted
each; one completeness critic followed with its own proofs.

**Tallies:** 28 found / **23 confirmed** / 4 merged as duplicates (KEY-01 into LEAK-01, KEY-02
into LEAK-02, OUT-E into LEAK-03, FID-09 into KEY-05) / 1 refuted (LEAK-04: a request already
holding data decrypted before a lock finishes with it; §3.2 promises the locked page to new
requests, and nothing is decrypted after the lock). The critic proved 4 more (CRIT-01..04, one a
trap in a proposed fix). By the skeptics' grading: no high; 5 medium (LEAK-01, OUT-A, FID-01,
FID-02, and KEY-05 before its downgrade); the rest low. The critic graded CRIT-01 and CRIT-02
medium.

## Outcome

Every confirmed finding is fixed with a regression test. The fixes are under catalog MA-798,
with corrections to MA-733 (REG-01) and MA-759; the design is amended in place under §7.5,
marked *Amended after the P3-2a adversarial pass*.

**The traps the critic found, and how the fixes avoid them.**
- **CRIT-02:** closing the view synchronously inside `ensure` when the open is seen locked
  (LEAK-02's proposed fix) deadlocks against `/api/facets`, which read the index notice while
  holding the index's read lock. The fix locks the reader and closes the view asynchronously, as
  `Lock` does; the facets handler now reads the notice before it takes the index; and no lock is
  held across a load (KEY-03).
- **CRIT-03:** a synchronous close at exit would wait on a client that stopped reading an API
  answer written under the index's read lock. The API now builds its answer under the lock and
  writes it after, and `Close` waits at most ten seconds.

## Findings and what was done

| Id | Grade | Finding | Done |
|---|---|---|---|
| LEAK-01 (+KEY-01) | medium | a clean exit of serve or search left the plaintext scratch index on disk | `Reader.Close`, synchronous within a bound; serve and search defer it |
| LEAK-02 (+KEY-02) | low | locking the open kept the reader's view, pages and scratch file | seeing the open locked locks the reader, dropping them (CRIT-02's trap avoided) |
| LEAK-03 (+OUT-E, FID-10) | low | the sweep trusted the pid in a scratch file's name | an OS lock on a lock file beside the index; the sweep removes any whose lock is free |
| LEAK-04 | — | refuted | — |
| KEY-03 | low | Lock waited behind a load | the locked flag is atomic; loads run under their own mutex; a late load is closed |
| KEY-04 | low | a folder link after a lock was 404 | the probe's lock is the locked page |
| KEY-05 (+FID-09) | low | a stalled download held the budget for five minutes, holding back an unlock | the write deadline renews with each write (15 s); the owed test against a real reader |
| OUT-A / FID-01 | medium | one index chunk gone refused the whole reader as "damaged" | search "unavailable — part of the index is missing"; the reader stays open |
| OUT-B | low | a history chunk gone made go-back a 500 | a partial timeline |
| OUT-D | low | each request for a bad unit reloaded the whole record | reload only when a peek shows the record moved; never for bad bytes |
| OUT-F | low | a planted primary pointer stopped the reader though the mirror loads | the peek covers both copies and never fails on one |
| REG-01 | low | MA-733's amendment was wrong for reindex and unenforced | row corrected; the test checks each verb's names |
| REG-02 | low | reindex's no-terminal refusal offered Desktop and spoke of backups | its own wording |
| FID-02 | medium | with no usable index the folder pages failed as "could not be read" | they and search say why, naming `reindex -rebuild`; messages open by link |
| FID-03 | low | the failed-load retry was untested | a test fails a load as the record changes |
| FID-04 | low | two lock windows were untested | hooks and tests for each |
| FID-05 | low | the index-behind note was untested | tested on the search page |
| FID-06 | low | go-back's sealed history was untested | a sealed-history fixture: available, a past date, damaged and gone chunks |
| FID-07 | low | the attachment zip path was untested | served from its own unit, tested |
| FID-08 | low | the ordering claims of MA-797 were unproven | tested with no secret at all; serve's no-key refusal tested |
| FID-11 | low | read-only loads and folders untested | the file-list rule read-only, and a folder the reader cannot write |
| FID-12 | low | "too large" named twice the message's size | named by the message's own size |
| CRIT-01 | medium | Ctrl-C did not stop keyed `reindex -rebuild` | the walk checks between messages; 130/143; a second signal exits |
| CRIT-03 | low | API answers were written under the index's read lock | built under it, written after |
| CRIT-04 | low | test sandboxes left behind by killed runs | stated below |

## Residuals, stated

- **CRIT-04:** test runs killed mid-way (the reviewers' timed-out mutation runs) leave their
  `mailarchive-keyring-*` sandbox folders in the system temp folder: test archives made with test
  keys only, nothing from the real home folder. The system's temp cleaning removes them.
- **The stop of `reindex -rebuild` at the command line** is wired through the same signal watcher
  `verify` uses and is tested at the engine (a stop between messages saves nothing); the CLI's
  exit code on a real signal needs a terminal, so it is left to P3-8's terminal tests.

## Prove-fail

22 mutations of the fixes, each run against its covering tests in a fresh copy, never beside the
full suite (scratchpad `mutate3p2afix.py`). Round 1: 20 killed, 2 did not build (a variable left
unused; rewritten). Round 2: 22/22. Two fixes have no mutation of their own: the scratch lock
taken before the index is made (an ordering a test cannot race reliably), and the CLI's stop code
for `reindex -rebuild` (above).
