# Adversarial pass — encrypted archives slice 2b-4, the keyless check

**Reviewed commit:** `78c8d8b` (branch `encrypted-archives`)
**Date:** 2026-10-25
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change: the keyless tier-1 check of an encrypted archive (`internal/container/verify.go`),
its wiring into `verify` (`internal/app/verify.go`, `encguard.go`, `cmd/mailarchive/main.go`), the
last-verify record's allow-list (`internal/state/encrypted_verify.go`, `lastrun.go`) and how `status`
judges it (`internal/health/health.go`). The owner approved the pass on 2026-10-25.
**Run:** workflow `phase2b-4-adversarial` (`wf_5c57be98-b2d`, 9 agents). Five read-only finders with
distinct lenses: OUT (someone who can rewrite the archive folder without the key), LEAK (cleartext), CON
(crashes and concurrency), DEC (the decision logic against design §4 and §13) and FID (test strength).
Each proved its claims with failing tests in its own clone of `78c8d8b`. Three refutation-default
skeptics each reproduced or refuted a share against an unmodified clone, and one completeness critic
followed. Tool versions: Go 1.26.0.
**Tallies:** 32 found / **30 confirmed** / 2 refuted. The skeptics refuted three; the critic showed that
one of those (DEC-03) contradicts a confirmed duplicate (OUT-05), and the synthesis sides with the critic.
The 30 merge to 24 distinct defects: OUT-06 = DEC-01, OUT-05 = DEC-03 (its leftover part), OUT-03 = DEC-04,
CON-01 = DEC-02, OUT-02 = CON-02, OUT-07 = DEC-07, LEAK-03 = FID-06. The critic proved 2 more: CRIT-1
(medium) and CRIT-2 (low). By the skeptics' grading: 3 medium (OUT-08, CON-01, FID-05; DEC-01 graded
medium by one skeptic, low by another), the rest low. None high.

## Outcome

Every confirmed finding but one is fixed, each with a regression test (catalog MA-766 and MA-767; the test
gaps are closed under MA-764 and MA-765). The design is amended in
`docs/design-encrypted-archives-phase2b.md` §14. A fresh-eyes re-verification of the fixes followed
(below).

One call was the implementer's, put to the owner, who accepted it on 2026-10-25 (CRIT-1): a file missing
or changed only under `chunks/`, with every unit as listed, is now `not_attested`, not `damaged`
("restore the whole archive folder from your newest backup copy"). Its next step is in the owner's
approved words: "What to do: run a backup now. It rewrites the damaged copy from the good one. Then
check the archive again. If the backup stops and names a file, both copies are lost: restore the whole
archive folder from your newest backup copy." Every record chunk has a
second copy that the keyed load reads and the next save rewrites, and the search index is rebuildable;
the old advice would roll back every run newer than the backup copy for a fault the next backup heals.
Without the key the check cannot tell one damaged copy from two; when both are lost, the backup it sends
the user to refuses and names the file. Damage under `units/` stays `damaged`, and so does every chunk
missing or changed (the re-verification's FV-C-01).

Not fixed, stated (OUT-01): a listed file's size has no upper bound, so someone who can write the folder
can list a huge sparse file and keep the check hashing for days under the archive lock. Deliberate
substitution is outside tier 1 (parent design A5). A unit has no size ceiling a bound could use: a
preserved original is sealed whole (the critic's trap). Ctrl-C ends the check and releases the lock. A
cancellable check is left for Phase 3's Desktop work.

Residuals, stated:
- The keyed side of CON-01. A backup that heals a damaged part of the file list, killed after its pointer
  lands and before its file list, leaves a state the keyed load refuses for good. The check now says
  `damaged`, matching it. Making the keyed load accept the state touches the F12 rollback guard (the
  critic's trap), and is left for its own review.
- A damaged search-index or history chunk is not healed by a backup that does not load it, so the check
  stays amber until one does, or until Phase 3's keyed check names the file.
- The plaintext last-run record is still read with a plain read; status would hang on a pipe there too.
  The new guarded reader should replace it.
- Windows was not exercised: the same-file check, a cloud placeholder at a listed path or folder.

## Findings

| ID | Lens | Claim | Verdict | Evidence / refutation | Fix | Tombstone test |
|---|---|---|---|---|---|---|
| OUT-08 | outside | The last-verify record is read with a plain read under the archive lock: a link is followed (an outside file read whole, its `previous.status` copied into the archive's record), a pipe blocks for good (every backup then "in use"). | CONFIRMED (medium) | Reproduced by skeptic 1: link copied `TEXT-FROM-OUTSIDE-THE-ARCHIVE`; pipe held the lock. Pre-existing in plaintext verify and status; this slice added the encrypted caller. | `util.ReadRegularFile`: no link, no pipe, at most 64 KiB; status closed to running/done; a carried verdict must be a finished one. Applies to every reader of the record. | TestTheLastVerifyRecordIsNeverReadThroughALinkOrWhole; TestAPipeAtTheLastVerifyRecordIsNotWaitedOn; TestKeylessVerifyFollowsNoLinkToItsRecord |
| OUT-06, DEC-01 | outside, decision | A folder, link or pipe at the envelope's name reads `intact`, while the keyed load refuses the archive. | CONFIRMED (low / medium) | Reproduced by skeptics 2 and 3 (folder, link, pipe). | A non-file at the envelope's name is `damaged` (its own reason); a missing one stays healable; the too-large case is tested first (the critic's trap). | TestAKeylessCheckFindsDamage (folder, link at the record file's name; an envelope only gone is healed) |
| OUT-05, DEC-03 | outside, decision | Files of an unread shard, and a newer run's files at I < P, are reported as "leftovers the next backup removes; nothing to do" — but nothing is swept then, and some are live. | CONFIRMED (low) for the wording; the outcome `not_attested` for an unread part at I = P is design-stated (MA-764, §3) | Reproduced by skeptic 3 (a live unit moved from checked to leftovers). DEC-03 was refuted as design-stated; the critic showed the design sentence applies only at I = P with every part read. | `LeftoversStay`: where the list is missing, behind or partly unread, the note says the files are outside the readable file list, with no removal claim. | TestAKeylessCheckSaysWhatItCannotAttest; TestAKeylessCheckComparesTheRecordWithItsFileList; TestAKeylessReportSaysEachOutcome |
| OUT-01 | outside | A listed size has no bound: a sparse file listed at 1 PiB is hashed for days under the lock. | CONFIRMED (low) | Reproduced by skeptic 1 (still hashing a 1 TiB sparse file after 5 s). Needs a deliberate writer (A5). | **Not fixed** — see Outcome. | — |
| OUT-02, CON-02 | outside, concurrency | A unit's folder swapped for a link between the per-segment Lstat and the open makes the check hash a file outside the archive, and report a changed unit as matching. | CONFIRMED (low) | Reproduced by skeptic 2 (1 match in 1,639,194 calls). | The opened file must be the one judged (`os.SameFile`). A first fix also opened inside an `os.Root`; the re-verification set it aside (FV-C-07, Windows). | TestAKeylessCheckNeverReadsThroughASwappedFolder (a fault-injection hook swaps the folder in the window, to a good copy outside and inside the archive) |
| OUT-09 | outside | With both pointer copies present but damaged, a newer envelope reads `newer_format`; the keyed load says both copies are damaged. | CONFIRMED (low) | Reproduced by skeptic 3. | A newer envelope decides only where no pointer copy stands. | TestAKeylessCheckAsksForAnUpgrade (beside two damaged copies; beside a current pointer, intact) |
| OUT-07, DEC-07 | outside, decision | With no pointer, the first-commit test accepts a link at a chunk's name, a temp beside the shard folders and a longer folder name; the keyed `checkFresh` refuses each. | CONFIRMED (low) | Reproduced by skeptics 1 and 3. | `firstCommitOwed` applies `walkLeftovers`' exact rules: two-character real folders, regular chunk-shaped files and temps. | TestAKeylessCheckJudgesAFolderWithNoRecord |
| OUT-03, DEC-04 | outside, decision | At a cleartext generation of 2^64 − 1, `recordedGen == P + 1` wraps to 0 == 0: `unfinished_save` instead of damage. | CONFIRMED (low) as OUT-03; DEC-04 REFUTED on its claimed harm (the outcome is RED either way; a forged header never authenticates) | Reproduced by skeptics 2 and 3. | A pointer or top-file generation no writer reaches is damage, so P + 1 never wraps. | TestAKeylessCheckFindsDamage (both pointers, the top file, at the last generation) |
| OUT-04 | outside | With `inventory/` a link, every shard is stat'd through it before being refused. | CONFIRMED (low) | Shown in the source by skeptic 1; metadata only. | `inventory/` that is not a real folder: nothing read or stat'd through it, and — after the re-verification (FV-C-02) — `damaged`. The keyed load's same pattern (store.go) is left as it is. | TestAKeylessCheckSaysWhatItCannotAttest (inventory a link is not read through) |
| LEAK-01 | leakage | A plaintext-shaped record in an encrypted archive is judged by the plaintext rules, its counts and an unchecked outcome shown. | REFUTED | A folder writer can forge any verdict in either shape; nothing of the mail leaks; the plaintext branch predates the slice. Phase 3's conversion should remove an old plaintext record. | — | — |
| LEAK-02 | leakage | An unknown outcome is quoted in full in the unreadable-record error, so a folder writer puts any text into status. | CONFIRMED (low) | Reproduced by skeptic 2 (a 100 KB planted sentence in the WARN). | No text of an unreadable record is echoed. | TestAnEncryptedArchivesVerifyRecordHoldsOnlyTheAllowList |
| LEAK-03, FID-06 | leakage, fidelity | A finished encrypted record with no outcome is accepted and judged by nothing (GREEN, a blank verdict); a carried plaintext-shaped verdict becomes one. | CONFIRMED (low) | Reproduced by skeptic 3. | A finished encrypted verdict must have an outcome of the list, at write and read, at both levels; a carried verdict without one is not carried. | TestAnEncryptedArchivesVerifyRecordHoldsOnlyTheAllowList |
| CON-01, DEC-02 | concurrency, decision | At I < P with a part of the file list unreadable, the check says `not_attested` ("run a backup"), but that backup refuses the archive for good. | CONFIRMED (medium) | Reproduced by skeptic 1 (keyless not_attested, keyed refusal). | At I ≠ P an unread part is `damaged` (reasonListDamaged), matching the keyed refusal. Keyed side: residual. | TestAKeylessCheckComparesTheRecordWithItsFileList (an older file list with a damaged part, checked against the keyed load) |
| DEC-05 | decision | A crash-left temp counts as a merge-restore's file (latent until RecordedGen is persisted). | CONFIRMED (low) | Reproduced by skeptic 1. | The merge signal counts only unit- and chunk-shaped files, as the keyed `walkFiles` does. | TestAKeylessCheckAppliesThisComputersLastSave |
| DEC-06 | decision | With no pointer, an envelope with both records and a newer version reads `newer_format`; the keyed load refuses it as a plaintext manifest. | CONFIRMED (low) | Reproduced by skeptic 2. | A plaintext manifest is decided before any newer format. | TestAKeylessCheckJudgesAFolderWithNoRecord (a plaintext manifest of a newer version) |
| FID-01 | fidelity | No test makes a listed file unreadable; the "could not be read" branches can become matches. | CONFIRMED (low) | Mutant survived (skeptic 1). | Tests added: a unit and a unit's folder without access. | TestAKeylessCheckFindsDamage |
| FID-02 | fidelity | No test proves the check takes the archive lock. | CONFIRMED (low) | Mutant survived (skeptic 1). | Test added, with its positive twin. | TestKeylessVerifyTakesTheArchiveLock |
| FID-03 | fidelity | Seven rules of the no-pointer branch are untested. | CONFIRMED (low) | Combined mutant survived (skeptic 1). | Tests added, each against the keyed load too. | TestAKeylessCheckJudgesAFolderWithNoRecord |
| FID-04 | fidelity | The too-large-for-an-envelope rule is untested. | CONFIRMED (low) | Mutant survived (skeptic 1). | Test added. | TestAKeylessCheckFindsDamage |
| FID-05 | fidelity | `status -json` gives `exit_code` 0 for a damaged keyless check. | CONFIRMED (medium) | Reproduced by skeptic 2. | An encrypted finished check's `exit_code` is 0 only for `intact`, else 2; counts stay 0 (no type change, the critic's trap); README says so. | TestStatusJudgesAKeylessCheckByItsOutcome |
| FID-07 | fidelity | Each half of the closed-list test checks one record level only. | CONFIRMED (low) | By reading. | Both levels tested on read; the carried verdict on write. | TestAnEncryptedArchivesVerifyRecordHoldsOnlyTheAllowList |
| FID-08 | fidelity | The CLI test clamps the exit with `min(code, 1)`, so a refusal that exits 2 passes. | CONFIRMED (low) | By the arithmetic. | The exit is asserted as it is. | TestEveryKeylessCommandRefusesAnEncryptedArchive |
| FID-09 | fidelity | Several branches are untested (a newer copy level with P, a newer envelope beside a whole archive, a changed entry count, the problems bound, a file where a unit's folder belongs); reasons matched by a shared substring. | CONFIRMED (low) | By reading. | Tests added; every case matches its full reason. | TestAKeylessCheckAsksForAnUpgrade; TestAKeylessCheckSaysWhatItCannotAttest; TestAKeylessCheckCountsEveryProblemAndNamesABoundedFew |
| FID-10 | fidelity | Nothing tests a non-intact report's rendering or the run-log line. | CONFIRMED (low) | By grep. | A table test over all six outcomes. | TestAKeylessReportSaysEachOutcome |
| FID-11 | fidelity | "Sealed files are present but its record is missing" is said of folders with no sealed files. | CONFIRMED (low) | Reproduced by skeptic 2. | One reason per cause. | TestAKeylessCheckJudgesAFolderWithNoRecord |
| CRIT-1 | critic | Bit rot in one copy of a doubled record chunk (or a search-index chunk) reads `damaged`, advising a whole-folder restore, while the keyed load reads the good copy and the next backup heals it. | PROVED (medium) | The critic's tests: keyless damaged; keyed load notice "a copy of a record chunk is damaged; the next save writes both copies again"; intact after one backup. | `not_attested` with its own reason and next step for damage only under `chunks/` — the implementer's call, accepted by the owner with the wording (see Outcome). | TestAKeylessCheckSaysWhatItCannotAttest (a record chunk missing / changed); TestAKeylessCheckFindsDamage (a unit and a chunk missing stays damaged) |
| CRIT-2 | critic | `encryptedArchiveAt`'s comment says a link to the archive is not the archive; in fact it is checked (harmless), and the OUT finder's "held" list was wrong. | PROVED (low) | The critic's test: `RefuseVerify(link)=<nil>`, outcome intact. | The comment corrected; the behaviour kept and tested as a positive case. | TestKeylessVerifyFollowsNoLinkToItsRecord |

## Mutation sweep (prove-fail)

56 mutations of the fixes (round 1, round 2 and the owner's CRIT-1 wording), each run
against its covering tests in a fresh copy of the tree, one at a time and never beside the
full suite. **55 killed, 1 survived as a stated backstop.**

- Container (`verify.go`, 31 mutations): every one killed. They cover:
  - the envelope order (a non-file, too large, newer before plain);
  - the last generation for the pointer and the top file;
  - the chunk/unit split, every chunk gone, and the inventory a link;
  - a stray folder and a link at a shard folder;
  - the `Unnamed`/`Leftovers`/`LeftoversStay` fields;
  - the `SameFile` compare;
  - the temp-versus-merge count;
  - each first-commit reason and its leftovers.
- State and util (11): all killed but one. `ReadRegularFile`'s Lstat refusal
  ("reader follows links") **survives** on macOS because `OpenRegularFile` opens with
  `O_NOFOLLOW|O_NONBLOCK`, which refuses the link and the pipe on its own. On Windows the
  Lstat refusal is the only guard. It stays, as the defence in depth the Windows build
  needs. The Windows half is not exercised here (no Windows run).
- Health (3), app (10, two of them the wording's step carried and shown), cmd (1): every
  one killed.
- Three mutations first failed to build, and one ran against the wrong package (scope
  errors in the sweep script, not in the product). Each was corrected and re-run, and
  each is killed.

## Fix re-verification

Workflow `phase2b-4-fix-verify` (`wf_1de254d8-349`, 3 agents, read-only, each in its own copy of the
fixed working tree): FV-C (the check against the keyed load), FV-R (the record and status, plaintext
regressions), FV-T (test strength). It found 15 claims, every one fixed or stated; none high.

| ID | Claim | Severity | Resolution |
|---|---|---|---|
| FV-C-01 | The CRIT-1 rule was too wide: every chunk gone (`chunks/` deleted, a link or a file) read amber, while the keyed load refuses. | medium | Every listed chunk missing or changed is `damaged` (its own reason); `chunks/` a link is `damaged` (FV-C-03's rule). Tested with the keyed refusal. |
| FV-C-02 | `inventory/` a link read amber ("the next backup rewrites it"), but that backup cannot write through it and leaves a state the keyed load refuses for good. | medium | `damaged`, still never read through. |
| FV-C-03 | A link where a shard folder belongs (`units/zz`, `chunks/zz`) read `intact`, while every backup's start sweep refuses it. Pre-existing. | medium | `damaged` whenever the file list is whole (the sweep runs then). A regular file there stays harmless. Tested with the keyed sweep's refusal. |
| FV-C-04 | "The next backup removes" was still said of files a backup keeps (a regular file where a shard folder belongs; unnamed files older than the last commit's run, which only the key can date). | low | Files under `units/` and `chunks/` the file list does not name are reported as unnamed, with nothing said of their removal; only the superseded parts of the file list are leftovers the next backup removes. |
| FV-C-05, FV-T-01 | The same-file check had no test of its own (the root refusal caught the outside swap); an inside-the-archive swap was guarded by it alone. | low / medium | With `os.Root` set aside, the same-file check is the guard; the swap test covers a good copy outside and inside the archive and asserts its detail. |
| FV-C-06 | An archive folder that can be searched but not listed made `os.OpenRoot` fail and every file read "could not be read" — a new false `damaged`. | low | Gone with `os.Root`. |
| FV-C-07 | Windows (unproven, read from Go's sources): an `os.Root` open refuses reparse points, which may include OneDrive placeholders the old path opened — every unit of a OneDrive-kept archive would read `damaged`. | medium | `os.Root` set aside; the old judged-path open plus the same-file check. Windows is still not exercised (residual). |
| FV-R-01, FV-T-02 | Text planted in a time field came back whole in the decoder's error, into status's WARN. | low | Any decode failure is a fixed sentence. Tested with a top-level and a carried time field. |
| FV-R-02 | `status -json` showed counts and an exit code a planted encrypted record carried. | low | For an encrypted record every count is 0 and the exit comes from the outcome alone. |
| FV-R-03 | With no verify schedule, an unreadable record was silent, so an earlier RED behind it vanished — and the fix made more states unreadable. Pre-existing for a torn record. | low | `lastverify_unreadable` WARN with or without a verify schedule. |
| FV-R-04 | The bounded read's "never read whole" half had no test. | low | A 256 MiB sparse file read with a 64 KiB bound must allocate under 4 MiB. |
| FV-T-03 | FID-11 only partly closed: "sealed files are present" still said of folders with none. | low | Own reasons: the file list present without a record; files a backup does not write among the record's files. |
| FV-T-04 | No pipe-at-the-envelope test, no carried-verdict JSON test, no plaintext-newer beside a pointer. | low | Each added. The pipe test calls the check directly: the suite's tree snapshot itself blocks reading a pipe (found while adding it). |

Checked and found sound by the verifiers: older releases' records (before F79, with an unknown future key,
a done record with `finished` null) still read as present; every reader of the record goes through
`ReadLastVerify`; MailArchive's own writers never produce what the new checks refuse; the plaintext
marshal path is unchanged; the envelope order, the 2^64 − 1 guards, the first-commit rules and the
merge signal match the keyed load.
