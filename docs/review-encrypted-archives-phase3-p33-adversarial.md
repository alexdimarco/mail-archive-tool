# Adversarial pass — encrypted archives Phase 3, slice P3-3 (MailArchive Desktop and encryption, 3a–3e)

**Reviewed commit:** `84c4449` (branch `encrypted-archives`; the pass covers `cd7e46a..84c4449`: the
P3-2b pass's fixes and P3-3 sub-slices 3a–3e). Sub-slices 3f and 3g (`dd984cd`) were built while it
ran; they and this fix round go to a follow-up pass.
**Date:** 2026-10-27
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers the Desktop's encrypted archives: the unlock registry and its
locks (`internal/desktop/keys.go`), the reader, Run now and the keyed pause, the key changes and the
store of prepared ones (`pending.go`), put back the key file, forget, and the keyring calls under
them.

The owner asked for each slice's pass to run without a separate approval (2026-10-25), and to
decide what is clear and raise only what is not (2026-10-26).

**Run:** workflow `wf_71019bd1-3dc`. Five read-only finders with distinct lenses:
- KEY: key lifetime and locking;
- HTTP: the guarded endpoints and what they send back;
- OUT: someone who can write the archive drive;
- REG: regressions in existing callers;
- FID: fidelity to the design and the strength of the tests.

Each proved its claims in its own copy. Two refutation-default skeptics reproduced or refuted each;
one completeness critic followed with its own proofs.

**Tallies:** 29 found / **28 confirmed** / 1 merged as a duplicate (HTTP-02 into KEY-07) / none
refuted. The critic proved 4 more (CRIT-01..04; CRIT-03 is a trap in a proposed fix). By the
skeptics' grading: no high (KEY-02, KEY-03 and KEY-07 were graded down from high); 9 medium
(KEY-02, KEY-03, KEY-07, KEY-01, FID-01, FID-04, FID-06, FID-07, CRIT-04); the rest low.

## The findings and what was done

The central defect (KEY-02, KEY-03, KEY-07, CRIT-01, CRIT-02): the registry's locks did not reach
work already under way. A remembered key's silent re-open read the person's Lock before a keychain
lookup and opened after it; a key change waiting for the derivation turn was kept after its archive
locked; a recovery file was written after its key was discarded. **Fixed** by making each lock
reach what is in flight: a lock counter per archive (an open begun before a lock of the same
archive loses, never one of another archive — REG-02), the remembered-key lookup made part of the
open, a hush while forget-all forgets, a prepared item kept only while its archive is still open
(`archiveKeys.ifOpen`), written to only while it still waits (`pendingSecrets.withItem`), and a store
that keeps nothing after Shut down.

| Id | Finding | Done |
|---|---|---|
| KEY-02 | a remembered re-open looking its key up undid a Lock, and outlived a switch | the lookup is part of the open; the held flag is checked again at its end; a switch makes it lose |
| CRIT-02 | forget-all's window let a remembered key re-open an archive it was forgetting | `archiveKeys.hush` for the whole forget |
| FID-03 | §16's "held until Unlock is clicked (no password asked)" was not built | "Unlock with the key this computer remembers"; a switch away ends the hold (O3); Run now opens a remembered archive by itself |
| KEY-03 | a key change prepared across a Lock, an idle lock, a switch or Shut down was kept | kept only while the archive is open (`ifOpen`); `pendingSecrets.close` at Shut down; a password change watches its archive's lock |
| KEY-07 (HTTP-02) | a recovery file saved after its key was discarded, never voided | written under the store's lock only while the item waits |
| KEY-05, HTTP-04, CRIT-01 | a prepared put-back outlived a switch, forget-all and uninstall; its file was named for the current archive | a switch ends items prepared on another archive's card; forget-all and a confirmed uninstall end all; the item keeps its own folder |
| KEY-04 | a failed confirm left the prepared key live | it is discarded on every failure that says to start again |
| HTTP-03 | one token table, untyped: a forget ate a key change's token | each endpoint takes only its own kind; another kind stays waiting |
| KEY-01 | Shut down did not wait for a reader's scratch index | the entry's open and lease lock first; Shut down then waits on the reader's own Close |
| KEY-06 | a Lock waited on a reader being made | the reader is made outside what a lock takes; one made across a lock is dropped |
| REG-01, CRIT-03 | a refused uninstall left every unlock refused | `lockAll(true)` then `reopen` on refusal; the preview hushes rather than `lockAll(false)` (the critic's trap) |
| REG-02 | a settings save cancelled an unlock of the archive it keeps | per-archive lock counters |
| OUT-A | "Forget the key" forgot whatever archive the folder's key files claimed now | forgets the open archive's own id (`keyring.ForgetKeyOf`); a folder whose key files no longer name it is locked |
| OUT-C | forget left `verb=forget-archive` beside the archive | no verb; keyed extract too. **Accepted:** verify keeps `verb=verify` — status reads it to tell a running check from one that died, and it says only that a check ran |
| OUT-D | Dismiss cleared a lost mark a planted key-file copy named | refused while the two copies disagree |
| FID-01 | forget-all left a lost archive's remembered key | that key goes too, before the lost list |
| CRIT-04 | an amber that opened no message erased a red found with the key, and its notice | the red stays standing beneath and the notice stays, unless every message was opened |
| OUT-B | the recovery file read could wait on a pipe swapped in | `util.ReadRegularFile` |
| REG-03 | the command line lost "or leave out the flag" | restored for a flag's label |
| REG-04 | Open-folder named README.txt, which a broken archive has | names the manifest; tells a manifest that cannot be read |
| HTTP-01 | put back the key file repeated part of what was typed | refused before the dialog, quoting nothing; checked in `keyring.RestoreFromFile` too |
| FID-02 | "Rebuild search index" refused an unlocked encrypted archive | the keyed rebuild (`keyring.Reindex`) on a write lease |
| FID-05 | the record-again question cost two derivations | asked before any derivation on unlock with Remember (`keyring.Forgotten`) |
| FID-04, FID-06, FID-07, FID-08, FID-09, FID-10 | claims with no test | guards added (MA-818) for each, but as listed below |

**Not done in this round, by decision:**
- FID-05 for put back the key file: `keyring.Forgotten` reads the folder's key files, which are
  missing in that case, so the question stays inside the restore (one derivation). The restore's
  questions are tested at the keyring (MA-791 and its pass).
- FID-07: "a forked key file is never healed" is guarded at the keyring (the heal refuses a fork);
  "a failure worded for the archive it started on" has no new test.
- FID-10's three unbuilt plan items — a lock state on each row of Your archives, a plaintext
  golden of the overview and health reply, and K9's "this file is not encrypted" line on reader
  downloads — are deferred to P3-8's friction review, recorded in §16.

## Proofs

- Each fix's guard is the finder's own reproduction, turned to the fixed behaviour: MA-816 (the key
  lifetimes, `internal/desktop/keys_pass_test.go`, with `-race`), MA-817 (forgetting), MA-818 (the
  smaller fixes and the untested claims), MA-819 (the standing red).
- Prove-fail -> prove-pass: scratchpad `mutate3p3pass.py` (36 mutations) then `mutate3p3pass2.py`
  for its survivors with sharper guards. 35 of 36 are killed. One end-check was a duplicate, so it
  was removed rather than kept as untested code. The two layers that end a key file prepared for
  another archive mask each other: the switch's discard, and the confirm's current-archive check.
  Each survives alone; removing both fails the test, as it did before the fix.
- The new race-sensitive tests are on CI's `-race` line for internal/desktop.
- The unfiltered suite is green.

## Owed

The follow-up pass on this fix round together with 3f and 3g.
