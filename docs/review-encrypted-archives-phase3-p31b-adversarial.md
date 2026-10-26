# Adversarial pass — encrypted archives Phase 3, slice P3-1b (the recovery file and putting key files back)

**Reviewed commit:** `8ebb51c` (branch `encrypted-archives`)
**Date:** 2026-10-26
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers:
- the one planned change to Phase 1's frozen vault API: `Vault.RecoveryFile`,
  `ParseRecoveryFile`, `PrepareRestore`/`PendingRestore` with its two sources, and `OpenWithin`;
- the keyring's restore (`RestoreFromFile`, `RestoreFromRemembered`) and its K14 asking;
- the command line: `restore-key-file`, `-recovery-file-out`, and case 3's offer.

The owner asked for each slice's pass to run without a separate approval ("Lets keep going and get
this done", 2026-10-25).

**Run:** workflow `wf_faf586fa-427`. Five read-only finders with distinct lenses:
- CRY: the cryptographic proof and the generation;
- OUT: someone who can write the archive drive;
- CON: concurrency, crashes and ordering;
- REG: regressions for everyone else;
- FID: fidelity to the design and test strength.

Each proved its claims in its own copy of `8ebb51c`. Two refutation-default skeptics reproduced or
refuted each claim; one completeness critic followed with its own proofs.

**Tallies:** 25 found / **22 confirmed** / 3 merged as duplicates (CON-03 into OUT-01, REG-01 into
CRY-01, FID-04 into CRY-03). The critic proved 6 more (CRIT-01..06, two of them traps in proposed
fixes). By the skeptics' grading: no high; 6 medium (CRY-01, OUT-01, CON-02, FID-01, FID-02,
FID-03); the rest low. The critic graded CRIT-01 and CRIT-02 medium.

## Outcome

Every confirmed finding is fixed with a regression test, except one open gap left to the owner
(FID-01/CRY-04) and two stated residuals. The fixes are under catalog MA-793, with amendments to
MA-789 (REG-05) and MA-791 (the open gap). The design is amended in place in §9.2 and §9.4, marked
*Amended after the P3-1b adversarial pass*.

**The traps the critic found, and how the fixes avoid them.**
- **CRIT-02:** OUT-03's "refuse a copy claiming a generation far ahead of now" would have removed
  the only floor that keeps a second restore above a first one made on a fast clock. The fix
  refuses only claims within 2^32 of the 2^62 cap, far past any clock. A claim far ahead of the
  clock is still outranked, and a test pins that. No clock ceiling was added; it would refuse a
  correct clock years from now.
- **CRIT-03:** CRY-02's "refuse any copy naming this id that does not authenticate" would have
  refused the person's own restore over a merely damaged copy. The fix accepts such a copy when its
  recovery entry is the recovery file's own, byte for byte; a forged recovery file's entry differs.

## Findings and what was done

| Id | Grade | Finding | Done |
|---|---|---|---|
| CRY-01 (+REG-01) | medium | `OpenWithin` re-derived on a newer copy without the limit | every copy judged before any derivation |
| CRY-02 (+CRIT-03) | low | with no record, a forged recovery file could replace a healthy key file | a copy naming the archive must be the source's own (`vouches`) |
| CRY-03 (+FID-04) | low | a planted generation claim wrapped round, or blocked unnamed | claims within the headroom refused by file name; no wrap possible |
| CRY-04 | low | a restore on a corrected clock outranked by an earlier fast-clock one | **open, owner's decision** (with FID-01) |
| CRY-05 | low | a recovery file derived at any cost up to the ceiling, unasked | K14's limit and question at unrecorded folders |
| CRY-06 | low | the code blocks on a damaged pointer; §9.4 and the wording said otherwise | strict rule kept; §9.4 and the refusal now say so |
| OUT-01 (+CON-03) | medium | a failed read-back after the write said "NOT in effect" | read back only the primary; failures are `ErrUnconfirmed`, in effect |
| OUT-02 | low | a folder swapped for another between prepare and Commit got the key file | the same directory required before the lock and before the write; **narrowed, not closed** |
| OUT-03 | low | a planted claim near the cap silently used up the generation space | refused by name; every restore keeps 2^32 rewrites |
| OUT-04 | low | the mirror-failed path skipped the read-back | the primary is read back there too |
| CON-01 | low | a forget between prepare and Commit was undone unasked | the record is re-read at Commit and re-checked in the update |
| CON-02 / FID-02 | medium | a record update failing after the write voided the recovery key in force | `keyring.ErrRestoredNotRecorded`; the CLI keeps the copies |
| CON-04 | low | a remembered restore held the derivation lock across the keychain | the lock is let go while the store is read |
| REG-02 | low | the cost question replaced the limit, then let a raw `CostError` out | the limit raised setting by setting; never a raw `CostError` |
| REG-03 | low | `CostError` could follow a derivation | the pre-scan (CRY-01) makes it mean "nothing derived" |
| REG-04 | low | help, the subcommand list and the stray-flag refusal missed the new verb and flag | added; the refusal names the flags given |
| REG-05 | low | the no-secret-flag allow-list grew without the catalog saying so | MA-789 amended |
| FID-01 | medium | the outranking claim fails with no copy left on disk | **open, owner's decision**; a skipped test encodes it |
| FID-03 | medium | Commit's re-judge, read-back, cap and newer-pointer refusal untested | tests for each, and the clock's positive twin |
| FID-05 | low | damaged key files at a recorded folder were not offered the restore | offered at every open; (b) **design gap, stated** |
| FID-06 | low | parts of MA-792 untested | tests for each |
| CRIT-01 | medium | without a new recovery file, the person was told to destroy the only one | the file is offered at the terminal; without one, keep the earlier pair |
| CRIT-04 | low | `RecoveryEntrySource` printed the recovery key | prints redacted; MA-718's test extended |
| CRIT-05 | low | Commit did not judge the folder's case again | judged again; nothing written |
| CRIT-06 | low | a mistyped recovery key found only after the password | checked first; asked again, the password kept |

## Open gap — the owner's decision

**FID-01 / CRY-04.** With no copy of the key file left on disk, a restore's generation is the
clock. Suppose a first restore ran on a clock set ahead, both copies were lost again, and a second
restore ran on the corrected clock. If the first restore's key file then comes back (a cloud sync,
a backup), it outranks the second. The password the second restore withdrew opens again, and a heal
writes the old key file over the new one.

It needs all three: a clock running ahead at the first restore, a second loss of both copies, and
the stale file coming back.

The options:
1. **Accept it as a stated limit** (recommended). §9.4 and MA-791 now say the outranking holds
   while a copy claiming the earlier generation survives, or the clocks were right. The skipped
   test stays as the record.
2. **Add a floor kept on this computer** (the highest generation seen per archive). It helps only
   on the same computer.

No clock ceiling is offered: it would refuse a correct clock years from now (CRIT-02).

## Residuals, stated

- **OUT-02:** the folder is checked to be the same directory before the lock and again before the
  write. A swap between that check and the rename is still possible. Closing it needs every read,
  lock and write through one open directory (`os.Root`), a larger change to the frozen vault's
  save path. It needs someone who can write the drive, racing the person's own prompts, and the
  bytes written are not theirs to choose.
- **FID-05 (b):** a recorded folder whose first backup never ran and whose key files are gone is
  §4.5 case 4, offered only forget-archive, though the vault would accept a restore there. It is a
  design gap, not a deviation.
- **PendingCreate.Commit** shares OUT-02's path re-resolution. Not new in this slice; its keyring
  Commit already judges the folder again (the P3-1a pass, OUT-03).

## Prove-fail

36 mutations of the fixes, each run against its covering tests in a fresh copy, never beside the
full suite (scratchpad `mutate3p1bfix.py`).

Round 1 (35 mutations): 33 killed, 1 did not build, and 1 survived:
- the read-back mutation did not build; it was rewritten, and a sibling added that drops only the
  byte comparison. Both showed that a damaged primary is also caught by the parse, so a test now
  puts the archive's earlier key file in the new one's place: it parses and authenticates, and
  only the byte comparison catches it;
- the cost question's "replace the limit" mutation survived because the test never asked for less
  in any setting. The test now does.

Round 2: every one killed, 36/36.
