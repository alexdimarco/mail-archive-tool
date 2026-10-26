# Adversarial pass — encrypted archives Phase 3, slice P3-0 (groundwork)

**Reviewed commit:** `87891cb` (branch `encrypted-archives`)
**Date:** 2026-10-25 to 2026-10-26
**Gate:** the finder/skeptic procedure (`../assurance-kit/process/adversarial-review.md`) for a
security-sensitive change. It covers:
- this computer's record of encrypted archives (`internal/encrecord`);
- the one answer to "is this encrypted" (`internal/encmark`) and its callers;
- the settings guard (E5);
- the guarded last-run read (E8);
- the stoppable keyless check (`container.CheckKeylessContext`, `VerifyStopped`, the CLI's 130/143);
- the Windows cleanup script.

The owner asked for each slice's pass to run without a separate approval ("Lets keep going and get
this done", 2026-10-25).
**Run:** workflow `wf_8b561d60-f39`. Five read-only finders with distinct lenses:
- OUT: someone who can rewrite folders on this computer's drives, without the key;
- CON: crashes and concurrency;
- REG: what plaintext users lose;
- SEC: the record as an attack surface;
- FID: test strength.

Each proved its claims with tests in its own copy of `87891cb`. Two refutation-default skeptics
reproduced or refuted each claim against an unmodified copy. One completeness critic followed,
with its own proofs, including two runs on the Windows 11 lab VM. Tool versions: Go 1.26.0.
**Tallies:** 33 found / **31 confirmed** / 2 merged as duplicates (REG-01 into CON-01, FID-03 into
CON-01). The critic proved 5 more (CRIT-01..05). By the skeptics' grading: 1 high (CON-01); 6
medium (OUT-02, CON-02, SEC-01, SEC-02, FID-01, FID-02); the rest low. The critic graded CRIT-01..04
medium and CRIT-05 low.

## Outcome

Every confirmed finding is fixed, with a regression test, except three that are deferred to P3-1a
for a stated reason (below). The fixes are under catalog MA-775; the design is amended in place in
`docs/design-encrypted-archives-phase3.md` §4.3, §4.5, §6 (E5) and §8, each change marked
*Amended after the P3-0 adversarial pass*.

**The top finding (CON-01, with REG-01, REG-02, FID-03 and the critic's trap CRIT-03).** While the
record could not be used, `verify` on a healthy plaintext archive ran the encrypted archive's
keyless check. It wrote a false "damaged" verdict and an attention file telling the person to
restore from a backup copy, and it erased the real verdict. The root cause was one answer serving
three questions. They are now separate:
- **Refuse?** A keyless write or check into a folder holding MailArchive files refuses with the
  record's own problem and remedy (`encmark.ErrRecordUnusable`, failure kind
  `encrypted_record_unusable`).
- **Label?** Status, Desktop and the archive list answer from the folder's own markers only.
- **Minimise?** A refused run's record stays minimised (`encmark.MustMinimise`, fail-closed), so
  no source path lands in a folder that may be an encrypted archive. This honours the critic's
  trap: the obvious fix to REG-02 would have leaked source paths.

**Deferred to P3-1a, when recording starts** (nothing records a path in P3-0, so none can be met
today):
- SEC-04: a Windows junction or folder-mounted volume is not resolved by Go's `EvalSymlinks`, so a
  recorded one would read "case L, may have been tampered with" from the moment it is recorded.
  Fix: record its final path (`GetFinalPathNameByHandle`).
- OUT-04 and OUT-05: a link recorded by its target, re-pointed later, reads as plaintext; a
  recorded path whose parent is swapped for a link reads case 4 (gone) instead of L. The fix needs
  §4.2's rule "a recorded path is never itself a link" revisited.

The Windows rows are pending in MA-775, to run on the Windows lab VM.

**Residuals, stated:**
- The CLI tests' child processes now get an isolated home folder. Tests that start the binary
  their own way (the pty and uninstall tests) already set their own.
- SEC-01's file-identity match stats every recorded path that exists on each lookup. A recorded
  path on an offline network share may make that slow. This was not measured.
- Desktop has no Cancel for a running check yet. It is not in P3-0's slice.
- The cleanup script's two new behaviours (stop on a failed key delete, remove the emptied folder)
  are pinned by reading the script, not by running it. `pwsh` is not on the build machine, and
  running the script in the lab VM deletes credentials.

## Findings

| ID | Lens | Claim | Verdict | Fix | Tombstone test |
|---|---|---|---|---|---|
| CON-01, REG-01, FID-03 | concurrency, regression, fidelity | With the record unusable, `verify` on a plaintext archive runs the keyless check, writes a false damaged verdict and an attention file, and drops the real verdict; status and Desktop call the archive encrypted. | CONFIRMED (high) | The three questions split (see Outcome). | TestVerifyRefusesWhileTheRecordCannotBeUsed; TestAnUnusableRecordRefusesFoldersHoldingArchiveFiles |
| REG-02 | regression | The stopped plaintext backups are reported everywhere as "encrypted, needs its key". | CONFIRMED (low) | Its own failure kind and text, naming the record and its remedy; the label does not flip. | TestARefusedRunKeepsItsRecordMinimisedWhileTheRecordCannotBeUsed; TestNoScheduleIsInstalledWhileTheRecordCannotBeUsed |
| CRIT-03 | critic | REG-02's obvious fix writes a full last-run record, with source paths, into a possibly-encrypted folder. | PROVED (medium) | `MustMinimise` stays fail-closed. | TestARefusedRunKeepsItsRecordMinimisedWhileTheRecordCannotBeUsed |
| REG-03 | regression | No home folder, or a settings folder that cannot be searched, makes every plaintext archive read encrypted, with "damaged — needs repair". | CONFIRMED (low) | A fourth case, `unreadable`, naming the folder and the error. | TestARecordThatCannotBeLookedForIsItsOwnCase |
| CRIT-01 | critic | A future header with an extra field reads as damaged, and the next write overwrites the newer record. | PROVED (medium) | "Newer" from the magic word and format number alone. | TestARecordThatCannotBeUsedIsNeverRewritten (a longer newer header) |
| CRIT-02 | critic | `verify` on a recorded folder that is gone says "no backup yet", and on one replaced by a link writes through it. | PROVED (medium) | Verify refuses every case but intact, before any lock or write. | TestVerifyOfATamperedRecordedFolderRefuses |
| CRIT-04 | critic | Common drive-root litter makes a healthy archive read as tampered (RED, "restore from a backup"). | PROVED (medium) | Litter list extended; design amended. | TestTheCasesHoldAtTheirEdges |
| CRIT-05 | critic | A failed key delete in the cleanup script does not stop it before the record is removed. | PROVED (low) | `$LASTEXITCODE` checked; throws. | TestTheCleanupScriptKeepsTheRecordOfEncryptedArchives |
| OUT-01 | outside | A link into a recorded archive's subfolder is not seen through. | CONFIRMED (low) | Lookup also walks the resolved path's parents. | TestTheRecordMatchesEverySpellingOfARecordedFolder |
| OUT-02, SEC-01 | outside, security | Another spelling of the same folder (Unicode NFC/NFD, σ/ς, a macOS firmlink) reads as plaintext. | CONFIRMED (medium) | NFC and full case folding where the system folds; file identity for recorded paths that exist. | TestTheRecordMatchesEveryMacSpellingOfOneFolder; TestTheRecordMatchesEverySpellingOfARecordedFolder |
| OUT-03, SEC-02 | outside, security | The settings guard misses the job files, the instance file (its key), the breadcrumb, the pre-sync record, the `.unreadable` copy and the record. | CONFIRMED (medium) | Each guarded; hooks for packages beneath `encmark`. | TestNoSettingsAreWrittenInsideAnEncryptedArchive; TestEveryProgramLinksTheSettingsGuard; TestThePreSyncStateIsNeverWrittenInsideAnEncryptedArchive; TestTheLastArchiveNoteIsNeverWrittenInsideAnEncryptedArchive |
| OUT-04 | outside | A recorded link re-pointed later reads as plaintext. | CONFIRMED (low) | **Deferred to P3-1a** (see Outcome). | pending in MA-775 |
| OUT-05 | outside | A swapped parent reads case 4, not L. | CONFIRMED (low) | **Deferred to P3-1a.** | pending in MA-775 |
| OUT-06 | outside | A plaintext manifest at a recorded path reads case 4. | CONFIRMED (low) | A plaintext manifest is case 2. | TestTheCasesHoldAtTheirEdges |
| CON-02 | concurrency | A stopped check resets verify staleness. | CONFIRMED (medium) | Staleness from the carried verdict, or none. | TestStatusAtTheEdgesOfP30 |
| CON-03 | concurrency | The "may have been lost" notice is lost when a write fails. | CONFIRMED (low) | Move-aside only after every check passes. | TestADamagedCopyIsSetAsideOnlyByAWrite |
| CON-04 | concurrency | Status's summary shows a stopped check as finished with a blank verdict. | CONFIRMED (low) | Shown as stopped, with the carried verdict. | TestStatusAtTheEdgesOfP30 |
| CON-05 | concurrency | A folder MailArchive cannot write is reported as "busy". | CONFIRMED (low) | "Busy" only for another holder. | TestARecordThatCannotBeLookedForIsItsOwnCase |
| CON-06 | concurrency | A stopped check still walks the whole tree. | CONFIRMED (low) | Returns right after the listed files. | TestAStoppedCheckWalksNoFolder |
| REG-04 | regression | `-RemoveLocalAppState` leaves the emptied folder. | CONFIRMED (low) | Removed once empty. | TestTheCleanupScriptKeepsTheRecordOfEncryptedArchives |
| SEC-03 | security | The record's owner is never checked (Windows has no permission check at all). | CONFIRMED (low) | The instance files' owner check, on every system. | TestARecordThatCannotBeUsedIsNeverRewritten (a foreign owner) |
| SEC-04 | security | A Windows junction would read as case L from the moment it is recorded. | CONFIRMED (low), reproduced on the lab VM | **Deferred to P3-1a.** | pending in MA-775 |
| FID-01 | fidelity | An archive made through a linked output folder reads case L, RED. | CONFIRMED (medium) | Case L judges the recorded spelling. | TestTheCasesHoldAtTheirEdges |
| FID-02 | fidelity | Newer key files of the same id read "missing or damaged". | CONFIRMED (medium) | Newer is not damaged. | TestTheCasesHoldAtTheirEdges |
| FID-04 | fidelity | The cleanup-script test reads a comment and a pattern list of its own. | CONFIRMED (low) | Parses the script's list; anchors the order inside the block. | TestTheCleanupScriptKeepsTheRecordOfEncryptedArchives |
| FID-05 | fidelity | The CLI's stop wiring is untested. | CONFIRMED (low) | Factored and tested on a real encrypted archive. | TestTheCLIWiresAnEncryptedChecksStop |
| FID-06 | fidelity | No Desktop or archive-list test of a recorded folder. | CONFIRMED (low) | Tests added. | TestDesktopLabelsARecordedFolderEncrypted; TestARecordedFolderIsListedAsEncrypted |
| FID-07 | fidelity | The code scan matches literal text only. | CONFIRMED (low) | Parses the Go files and resolves import names; fails if nothing is scanned. | TestEveryCallerAsksEncmark |
| FID-08 | fidelity | The settings test does not check the file is named, nor a recorded-only archive. | CONFIRMED (low) | Both asserted. | TestNoSettingsAreWrittenInsideAnEncryptedArchive |
| FID-09 | fidelity | "Unproven" WARN and the case order are not pinned. | CONFIRMED (low) | Tests added. | TestStatusAtTheEdgesOfP30; TestTheCasesHoldAtTheirEdges |
| FID-10 | fidelity | "Status notes the stop" is not pinned over an intact verdict. | CONFIRMED (low) | Test added. | TestStatusAtTheEdgesOfP30 |
| FID-11 | fidelity | Record branches untested (damaged wording, notice absence, the id check, the sequence step). | CONFIRMED (low) | Tests added. | TestARecordWithAForeignKeyIsDamaged; TestADamagedCopyIsSetAsideOnlyByAWrite |
| FID-12 | fidelity | The Windows tests the design owes are neither written nor pending. | CONFIRMED (low) | Marked pending in MA-775 with the lab story. | — |
| FID-13 | fidelity | The CLI tests read the real record. | CONFIRMED (low) | Their test setup and every child process use a temporary folder. | (setup change) |

## Mutation sweep (prove-fail)

52 mutations of the fixes, each run against its covering tests in a fresh copy of the tree, one
at a time and never beside the full suite. **All 52 killed**, in two rounds.

- **Round 1:** 40 killed. 10 survived and 1 did not build; one more was killed only by a 10-minute
  hang. Each survivor was a fix whose test never reached it:
  - a record copy MailArchive may not read (only an unreadable folder was tested);
  - the "busy" wording (the lock file already existed, so taking the lock succeeded);
  - full case folding (the folder still existed, so its identity matched first);
  - the pre-sync state writer and its guard hook (no test wrote it);
  - schedule install while the record is unusable (refused anyway, under the wrong error);
  - a stopped check with no earlier verdict, for staleness;
  - the Desktop settings' kept copy (the test never planted the unreadable file inside the
    archive);
  - both writers of the wizard's last-archive note.
- **Tests added or strengthened for them:**
  - TestARecordThatCannotBeLookedForIsItsOwnCase (an unreadable copy; the lock with no lock file);
  - TestTheRecordMatchesEveryMacSpellingOfOneFolder (each spelling again once the folder is gone);
  - TestThePreSyncStateIsNeverWrittenInsideAnEncryptedArchive (new; reached through a test-only
    entry point);
  - TestNoScheduleIsInstalledWhileTheRecordCannotBeUsed (new);
  - TestStatusAtTheEdgesOfP30 (a stopped check with nothing before it);
  - TestNoSettingsAreWrittenInsideAnEncryptedArchive (the kept copy really planted);
  - TestTheLastArchiveNoteIsNeverWrittenInsideAnEncryptedArchive (new);
  - TestTheCLIWiresAnEncryptedChecksStop (fails in 10 seconds instead of hanging).

  The mutation that did not build was rewritten.
- **Round 2:** those 12 re-run, all killed.

The mutations cover:
- the record: header, unreadable cases, owner, write order, folding, lookup chains and identity;
- encmark: the record-error flag, the case target, the plaintext manifest, newer keys, litter,
  the four hooks, the label and the minimising;
- the app's refusal, verify routing and failure kind;
- the schedule guard;
- status staleness and its summary;
- the stop after cancel;
- the CLI wiring;
- each settings writer;
- the four cleanup-script promises.

## The full suite's time margin (found while running the gate, not by the pass)

The first unfiltered `go test ./...` after these fixes failed. The app package ran past Go's
10-minute per-package limit while every package ran at once. Alone it takes 405 seconds, the
same as before these fixes, and a re-run of the whole suite passed (app 419 seconds). Its 584
tests take 373 seconds one after another. About 130 places in them create or unlock a key at
the full Argon2id cost (256 MiB, 3 passes), by design. CI runs the same command with the same
limit. The branch is not under CI yet, but it will be at merge. The fix that would restore the
margin is a test-only cheaper cost inside `internal/vault`. Its API is frozen and the change
touches the cryptography, so it is left for the owner to rule on, not done here.
