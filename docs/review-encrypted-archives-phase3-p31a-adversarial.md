# Adversarial pass — encrypted archives Phase 3, slice P3-1a (the unlock package and the command line)

**Reviewed commit:** `2ad8d12` (branch `encrypted-archives`)
**Date:** 2026-10-26
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers:
- `internal/keyring`, the one unlock package (opens, adoption, K14, leases and locks, create, the
  key changes, forgetting, secret files);
- the hidden prompt (`tty.ReadSecret`, `ReadLine`);
- the Windows access-list predicate and the owner-only write (`internal/winsec`);
- this computer's record: repair, the lost list, recording by the resolved location;
- the command line: `-encrypt`, keyed `export` and `graph`, the five key verbs, the scheduler's
  refusals, the no-secret-flag scan.

The owner asked for each slice's pass to run without a separate approval ("Lets keep going and get
this done", 2026-10-25).

**Run:** workflow `wf_4c9f8705-682`. Its first run ended with every agent failing on a lost
connection to the model service ("Can't reach the API server"); nothing was reviewed, and it was
started again unchanged. Five read-only finders with distinct lenses:
- SEC: secrets and trust;
- OUT: someone who can write the archive drive or a folder on the way to it;
- CON: concurrency, crashes and ordering;
- REG: regressions for everyone else;
- FID: fidelity to the design and test strength.

Each proved its claims in its own copy of `2ad8d12`. Two refutation-default skeptics reproduced or
refuted each claim; one completeness critic followed with its own proofs.

**Tallies:** 40 found / **36 confirmed** / 4 merged as duplicates (OUT-05 and FID-09 into SEC-01,
CON-09 into OUT-04, FID-06 into REG-03). The critic proved 10 more (CRIT-01..10, four of them
traps in proposed fixes). By the skeptics' grading: no high; 5 medium (SEC-01, CON-02, REG-02,
FID-03, FID-04); the rest low. The critic graded CRIT-01..04 medium.

## Outcome

Every confirmed finding is fixed with a regression test, except three stated as residuals and one
fix that is an implementer's call the owner may veto. The fixes are under catalog MA-790 (with
amendments to MA-726, MA-727 and MA-778's gate); the design is amended in place, marked *Amended
after the P3-1a adversarial pass*.

**The traps the critic found, and how the fixes avoid them.**
- **CRIT-04:** removing `-recovery-key-out` whenever `Commit` errs would have deleted the only saved
  recovery key of an archive that WAS created (its key file written, its record not). `Commit` now
  returns `keyring.ErrCreatedNotRecorded` for that case, and the file is kept and named.
- **CRIT-03:** letting a backup unlock an unproven path (REG-02) inside the shared
  `encryptedArchiveAt` would have let `verify` check it as intact. The change is a separate
  `app.UnlockableAt`, used only by the CLI's keyed decision; `verify` still refuses, now tested.
- **CRIT-10:** the prompt's fallback from a recovery-key reading to a password (SEC-03) is a whole
  second `keyring.Open`, so its own K14 question is asked; it never calls the vault directly.

## Findings and what was done

| Id | Grade | Finding | Done |
|---|---|---|---|
| SEC-01 (+OUT-05, FID-09) | medium | `-recovery-key-out` written before the re-entry, kept when nothing took effect | written after the re-entry, just before the commit; removed (overwritten) when the key does not take effect, and said; kept for `ErrCreatedNotRecorded` |
| SEC-02 | low | MailArchive's own key file not readable by `-recovery-key-file` | header is a `#` comment; the reader takes the one non-comment line |
| SEC-03 (+CRIT-10) | low | a password shaped like a recovery key could not be typed | refused when set; the prompt tries a failed recovery reading as a password through a whole open |
| SEC-04 / FID-07 | low | callback (`XA`, `ZA`) and unknown access entries judged safe | every type but deny and audit counts as a grant; entries split at balanced parentheses |
| SEC-05 | low | the pending recovery key not destroyed when its save failed | `Discard` deferred on every path |
| SEC-06 | low | access lists beyond the mode ignored on Unix | refused on Linux; **residual on macOS** (not reachable without cgo) |
| SEC-07 | low | parse detail quoted part of a secret | fixed words for a non-key and a typing mistake |
| SEC-08 | low | the scan did not see the key users §3.1 reserves to keyring | `ChangePasswordAt`, `Heal`, `Remember`, `Forget`, `KeySecret`, and `VerifyKey` outside app, scanned |
| SEC-09 / FID-08 | low | the Windows key file written before its access list was set | made empty, restricted, then written; refused (not just warned) if it cannot be restricted |
| OUT-01 | low | a recorded folder linked to another recorded archive matched as the other | the folder's own spelling (parents resolved) matches first: case L |
| OUT-02 | low | the folded spelling used as a real path on a case-sensitive volume | **residual**: writers still refuse; only the case wording differs |
| OUT-03 | low | Commit did not judge the folder again | `stillCreatable`: empty, not encrypted, no overlap, same resolved place |
| OUT-04 (+CON-09) | low | K14 judged one read, the vault derived from another | **residual**: bounded by the A3 ceiling; closing it needs a cost limit inside the frozen vault open |
| CON-01 | low | Ctrl-Z at a prompt nothing could continue stopped it for good | ignored there, as the system ignores it; typing stays hidden |
| CON-02 | medium | a repair cut short left an empty, usable record that could not be repaired | damaged copies are copied aside and replaced by rename; retry tested |
| CON-03 | low | a failed new recovery key not told as not in effect | said, and its file removed |
| CON-04 | low | a forget during an open overridden without the question | the record is re-checked against what was asked |
| CON-05 / FID-01 | low | a refused forget had already forgotten the key | the key is forgotten inside the record update, after the re-check |
| CON-06 | low | a stale lost mark | not made, nor shown, for an id the record lists |
| CON-07 | low | finishing set-up never offered Remember again | offered, on by default |
| CON-08 | low | Ctrl-C at a question inside an open was a "no" | the questions return the interruption: exit 130 |
| CON-10 | low | Windows `-outlook` PSTs left after Ctrl-C | the interrupt is held until the export returns, then the cleanup runs |
| REG-01 | low | the scheduler's key-flag scan read values as flags | judged on the parsed flag set |
| REG-02 (+CRIT-03) | medium | a backup could not prove an unproven path | `app.UnlockableAt` for the CLI only |
| REG-03 (+FID-06) | low | `-encrypt` asked for a password before judging the folder | judged first, before discovery and before any question |
| FID-02 | low | a forget went ahead while a backup held the folder | refused, "try again when it is free"; the folder's case re-checked |
| FID-03 | medium | five keyring rules had no test that a regression would fail | tests for each (K14 by the recovery entry, remembered writes, the tombstone race, Commit's re-check) |
| FID-04 | medium | the CLI's §5.1/§5.2/§9.1/K2 words untested | tests for each, including the key never entered to the limit |
| FID-05 | low | the allow-list also exempts keyring's `vault.Detect` | MA-727 amended so the operator's record says so |
| FID-10 | low | `-remember` missing from the key verbs; secret flags ignored by the forget verbs | added where they unlock; removed where they do not |
| FID-11 | low | the R37 rows' terminal tests were weak | full plaintext walk after both backups; key files deleted then a keyless backup refused, tree unchanged; the remember question and Ctrl-C at it on a real terminal |
| FID-12 | low | cases L and 2 called an altered key file; the busy refusal repeated itself | `keyring.ErrNotTheArchive`; told once |
| FID-13 | low | coverage gaps (repair race, ForgetKey unrecorded, locked key changes, a marker) | tested; marker added |
| CRIT-01 | medium | the leases' race tests never ran under the race detector | a CI step on Ubuntu runs `go test -race` on keyring, vault and tty |
| CRIT-02 | medium | an unproven path whose archive is gone could never be cleared | **implementer's call, vetoable**: forgettable with the strong confirmation when its key files are gone, and its refusal says so |
| CRIT-05 | low | "finish setting up" status not built | a WARN for an archive with no backup yet, and for a key-file-only copy |
| CRIT-06 | low | a removed archive typed through a linked parent was "inside itself" | judged by the recorded spelling too |
| CRIT-07 | low | a fork also called damage | a fork is not healed |
| CRIT-08 | low | repair joined tombstones by OR | the later copy decides; a path recorded again is not forgotten |
| CRIT-09 | low | byte-order marks | UTF-8's dropped; UTF-16 refused with the remedy |

## Residuals, stated

- **OUT-04:** the K14 question and the derivation read the key files separately. A drive that
  serves different bytes to each read can cost one derivation up to the A3 ceiling (1 GiB, time 16)
  without the question. Closing it needs the cost check inside the vault's open, a change to the
  frozen API; left for the owner's ruling with P3-1b's `PrepareRestore` change.
- **OUT-02:** on a case-sensitive volume, a recorded path whose letters fold differently is read
  through its folded spelling. Keyless writers still refuse it; the refusal's case and its forget
  confirmation may be the weaker one.
- **SEC-06 on macOS:** an access list on a 0600 password file is not seen (macOS keeps it out of
  reach without cgo). It is the person's own file.

## Owner's call

- **CRIT-02** is a design gap: §4.5 offers no forget for an unproven path. The fix lets one be
  forgotten, with the strong confirmation, only when its key files are gone, since nothing could
  ever prove it. It can be reverted on the owner's word.

## Prove-fail

42 mutations of the fixes, each run against its covering tests in a fresh copy, never beside the
full suite (scratchpad `mutate3p1afix.py`). Round 1: 33 killed, 3 did not build (rewritten), and 6
survived:
- two were weaker than the fix they named (the UTF-16 refusal also sits in the NUL check; the
  bracket mutation did not undo the nesting) and were rewritten to remove the whole rule;
- one was duplicate code: the record's lookup already matches a folder by its own spelling with
  its parents resolved, so the extra check in `atRecordedFolder` was removed;
- three showed tests that never reached the fix: the re-entry's wording, the repair's tombstone
  merge (two cases), and status ignoring a stale lost mark. Tests were added.

Round 2: every one killed, 42/42. On Linux (Docker `golang:1.26`), the access-list check's
mutation is killed too, and the changed packages pass with the race detector. The full
`go test ./...` passed before round 2's small changes; the packages they touch pass again after.
