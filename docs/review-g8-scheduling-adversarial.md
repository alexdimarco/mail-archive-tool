# g8 scheduling: adversarial pass

**Counts:** 13 found / 11 confirmed / 0 refuted (2 duplicates, 0 non-goals); 6 known items, all closed or escalated below.

**Verdict: FINDINGS OPEN.** Confirmed and not fixed, each with a skip encoder and an owner decision named in "Escalations":
G8-AO1 (the launchd custom-name part only; the rest is fixed), G8-AI4, G8-AC4, K4.
Everything else confirmed is fixed on `wip/g8-scheduling` with its tombstone, shown red before the fix.
Fixed but not run on the real OS: the Windows branches (compiled with `gofor windows`, exercised on this host only through the fake Task Scheduler and the crontab seams).

## g8 adversarial pass

**Process:** `../assurance-kit/process/adversarial-review.md`. **Base:** `main` 066f86d. **Head attacked:** `wip/g8-scheduling` a69228d (28 commits: the wrapper's trailing backslash and UTF-8 code page, cron's `%` escape, `-remove` reversing only the entry it names, the device job's sign-in check, the verify name guard, FB-4's verify descriptor and `verify_stale` on its own cadence, and the review fixes 5090706..a69228d).
**Fixes:** 3ba9d35, ee10d29, 67c14e3 (this file's commit follows).

**Lenses (three finders, one skeptic):**
1. Outside aggressor: hostile paths, names and handed-over archives reaching the scheduler (G8-AO*).
2. Insider: a co-account that can write the archive, and mixed versions (G8-AI*).
3. Integrity and concurrency: install, remove and rename ordering (G8-AC*).

Threat model: an archive folder can be written by whoever handed it over, so its descriptors are untrusted input. Same-account malware and administrators are out of scope. Rulings that stand and were not touched: UJ-6, FB-4, g8-R2, g8-Q6, g8-Q7, g8-Q8, ESC-10.

### Findings

Severities are the skeptic's (AO1 high to medium; AC1, AC2, AC5 medium to low).

| ID | Lens | Claim (file:line, at a69228d) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| G8-AO1 | outside | `-out A -remove` and the GUI's Remove delete the entry a planted descriptor names: any root-folder Task Scheduler task, another archive's cron block or launchd plist (`cmd/mailarchive/main.go:838-851`, `schedule.go:713-726,835-840`, `cmd/mailarchive-gui/main.go:798-819`) | CONFIRMED (medium). Fixed except launchd custom names | Probes on a fake Task Scheduler (`/Delete /TN GoogleUpdateTaskMachineCore /F`), a fake crontab (archive B's block gone) and a launchd sandbox (B's plist gone). Now: `RecordedBackupName` binds the recorded name to the archive (own derived names; another archive's derived name refused; a job for a folder that still exists is a copied record; cron: the block's `-out`; Windows: a wrapper of ours). Refused with the descriptor, name and `-name` remedy. A custom name on launchd cannot be told from another archive's without reading the plist back (UJ-6): open | 3ba9d35 | `TestScheduleRemoveRefusesADescriptorNameThatIsNotThisArchives` (cmd), `TestSchedulerEntryNameIsBoundToItsArchive` (schedule); residual: `TestPlantedCustomNameOfAnotherArchiveIsRefusedOnLaunchd` (skip) |
| G8-AO2 | outside | The pasteable `moved_archive` remedy is double-quoted for cmd but carries descriptor tokens that PowerShell (`$(...)`, `$env:`) and cmd (`%VAR%`) expand (`health.go:289-294,600-652`) | CONFIRMED (medium), not in the g8 diff (5ac6a50) | Probe printed `-input "$(Start-Process calc)"`. Now a Windows token with `%`, `^`, a backtick or a non-trailing `$` is replaced by `<edit-this-value-by-hand>` (neither shell runs it); benign tokens keep MA-157's double quotes; POSIX unchanged | 67c14e3 | `TestMovedArchiveRemedyNeverPastesAShellExpansion` |
| G8-AO3 | outside | Name guards compare names case-sensitively; on macOS/Windows a case variant replaces the other entry and its wrapper (`schedule.go:608,615,720,796`) | CONFIRMED (low) | Probe: verify `nightly` installed over backup `Nightly`; the wrapper file was overwritten. Now `sameName` compares case-insensitively on darwin and windows, exactly on cron, in the guards and in the descriptor-goes-with decisions | ee10d29 | `TestNameGuardComparesCaseWhereNamesAreCaseInsensitive` |
| G8-AI1 | insider | A copied or planted backup descriptor makes `-out Y -remove` and the GUI Remove delete archive X's entry (`main.go:829-862`) | DUPLICATE of G8-AO1 | Same root; AO1's launchd and cron probes reproduce it; the AO1 tombstones cover the copied-descriptor case ("X's descriptor copied into Y") | 3ba9d35 | same as AO1 |
| G8-AI2 | insider | A planted verify descriptor naming another archive's entry feeds `SupersededVerify`'s note and `verifyRemoveHint` (`descriptor.go:124-149`, `schedule.go:811-825`, `main.go:947-948,1220-1227`) | DUPLICATE of G8-AO1 | Reproduced (`SupersededVerify` returned X's entry; the hint named it). `SupersededVerify` and the hint now take only a name that binds (`RecordedVerifyName`) | 3ba9d35 | same as AO1 (planted verify subtests; the CLI hint check) |
| G8-AI3 | insider | A backup descriptor that records a verify makes the backup install advise `re-install with -name <verify's name>`, which the guard then allows to replace the verify (`schedule.go:612-617`) | CONFIRMED (low) | Probe on cron: refused with that advice, then accepted. Now never advised; a backup under the verify's name is refused as the clash; "remove it first" stays | ee10d29 | `TestBackupInstallNeverAdvisesTheLegacyVerifysName` |
| G8-AI4 | insider | `status` stats a descriptor-supplied program path (now two: backup and verify), so a UNC or hung-mount path stalls it or leaks a Windows sign-in hash (`health.go:728,744,752-759`) | CONFIRMED (low), pre-existing; read from source, not run | Not fixed: refusing UNC paths narrows what `status` reports for network-share installs and a hung mount cannot be detected without a deadline; owner decision | none | `TestStatusDoesNotStatAUNCProgramPath` (skip) |
| G8-AC1 | integrity | Windows `-remove` clears a task it could not delete: any non-zero `/Query` exit counts as "not installed" (`schedule.go:1043-1052`, `installed.go:83-95`) | CONFIRMED (low) | Fake `schtasks` with `/Delete` and `/Query` both "Access is denied": returned nil and cleared wrapper, XML and record. Now only a `/Query` that says the task is missing clears it; otherwise the `/Delete` error is kept and names the by-hand delete. On a localised Windows nothing matches the English wording, so the removal errs toward keeping the error | ee10d29 | `TestWindowsRemovalKeepsTheDeleteErrorWhenTheTaskMayStillBeThere` |
| G8-AC2 | integrity | Install applies the entry first and the descriptor second, so a failed descriptor write leaves an unrecorded entry (`schedule.go:553-585`) | CONFIRMED (low). Partly fixed | The ordering stays (6a453b1: no descriptor claims an entry that never loaded). A directory planted at the descriptor path is now refused before the scheduler is touched; any other write failure now says the entry is installed, names it and gives `-name N -remove`. A missing `-out` folder (allowed for export jobs) still installs first | ee10d29 | `TestDescriptorWriteFailureNamesTheEntryAndLeavesNoTemp` |
| G8-AC3 | integrity | A failed rename leaves the descriptor's temp file in the archive (`descriptor.go:101`) | CONFIRMED (low) | Two failed installs left two temps. The writer now removes it | ee10d29 | `TestDescriptorWriteFailureNamesTheEntryAndLeavesNoTemp` (writer leg) |
| G8-AC4 | integrity | Nothing serialises schedule mutations; concurrent installs defeat the one-schedule-per-archive guard (`schedule.go:553-585,874-884`) | CONFIRMED (low). Not fixed | Probe: both `-name nightly-a` and `-name nightly-b` pass the guard before either writes; one is named nowhere. A fix needs a lock spanning the guard, the scheduler edit and the descriptor write (the archive lock is held for an export's whole run): owner decision | none | `TestConcurrentInstallsForOneArchiveCannotBothPassTheGuard` (skip) |
| G8-AC5 | integrity | cron install and remove wipe the user's crontab when `crontab -l` fails for any reason but "no crontab" (`schedule.go:914-920`, same on main) | CONFIRMED (low), not new in g8 | Probe with `crontab -l` failing "Permission denied": install wrote only our block, remove wrote an empty table. Now both fail naming `crontab -l` and its output, writing nothing; "no crontab yet" still installs | ee10d29 | `TestCronEditsNeverRewriteACrontabTheyCouldNotRead` |
| G8-AC6 | integrity | The cron name guard compares only `-out`, so with a descriptor missing the other kind takes over the entry (`schedule.go:603-626`) | CONFIRMED (low) | Probe: backup `nightly-c`, descriptor deleted, verify `-name nightly-c` accepted and the block replaced. The guard now also reads the block's kind | ee10d29 | `TestCronNameGuardReadsTheBlocksKindWhenDescriptorsAreGone` |

### Known items

| ID | Item | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|
| K1 | a69228d's `-out` check hides a moved archive's scheduled verify | FIXED | A verify recorded for a folder that no longer exists is the renamed archive's own (a folder that still exists is a copy). Red: "the moved archive's verify is hidden"; the install now names the entry still scheduled against the old path | 3ba9d35 | `TestSchedulerEntryNameIsBoundToItsArchive/a_moved_archive's_verify` |
| K2 | README's upgrade example over-promises; `SupersededVerify`'s legacy branch untested | FIXED | README says the install names an earlier verify only when the archive's records name it. Legacy branch tested; mutation (branch removed) red: `got "", want "mailarchive-verify"` | ee10d29 (test), 67c14e3 (README) | `TestSupersededVerifyReadsAnOlderReleasesBackupDescriptor` |
| K3 | A stale verify that still holds the lock gets "stopped running ... re-install" | FIXED | Red: "a verify that holds the archive lock is told it stopped running and to re-install". Now says a verify holds the lock and is still running, with its log | 67c14e3 | `TestStaleVerifyThatStillHoldsTheLockIsNotToldToReinstall` |
| K4 | A planted verify descriptor that `-out X -remove` keeps becomes trusted once the backup's descriptor is gone and blocks the default-named backup | OPEN, escalated | Closing it adds a third cross-kind refusal beyond g8-Q6's two | none | `TestPlantedVerifyDescriptorDoesNotBlockTheDefaultNamedBackup` (skip) |
| K5 | No test covers Gather recording the verify program's identity | TEST ADDED | Mutation (Gather no longer records it) red: `Gather recorded exists=false isThis=false, want true true` | 67c14e3 | `TestGatherRecordsTheScheduledVerifysProgramIdentity` |
| K6 | No test covers `removeSchtasks` keeping the `/Delete` error when Task Scheduler cannot be queried | TEST ADDED | Mutation (an unavailable scheduler read as "gone") red in the no-schtasks leg | ee10d29 | `TestWindowsRemovalKeepsTheDeleteErrorWhenTheTaskMayStillBeThere` |

### Escalations (owner decisions; skip encoders in the tree)

- **G8-AO1, launchd custom names (UJ-6).** Another archive's custom launchd entry name cannot be told from this archive's without reading the plist back, which UJ-6 rules out, or namespacing custom names by archive. Today a descriptor claiming B's custom name for its own folder still binds on launchd. Encoder: `TestPlantedCustomNameOfAnotherArchiveIsRefusedOnLaunchd`.
- **G8-AC4 (locking).** Needs a lock design across the guard, the scheduler edit and the descriptor write. Encoder: `TestConcurrentInstallsForOneArchiveCannotBothPassTheGuard`.
- **K4 (g8-Q6).** Rejecting a verify descriptor under the backup's derived name, and refusing a verify install under it, is a third refusal. Encoder: `TestPlantedVerifyDescriptorDoesNotBlockTheDefaultNamedBackup`.
- **G8-AI4 (status).** Refusing to stat a UNC program path narrows `status` for network-share installs; a hung mount needs a deadline. Encoder: `TestStatusDoesNotStatAUNCProgramPath`.

Decisions made inside the fixes, for the owner to veto: a Windows removal that cannot confirm a task is gone now errors rather than clearing (English `schtasks` wording only; a localised Windows keeps the error and the by-hand command); a descriptor naming an entry that is not this archive's makes `-out X -remove` fail with exit 1 (it never falls back to the default name); a Windows token no quoting makes inert is replaced by a placeholder, not omitted.

### What held (tried by the finders and not broken)

- Cron `%` escape (F62): 20 hostile tokens round-tripped through vixie-style unescaping and a real sh.
- Line injection into cron, launchd and Task Scheduler: `Validate` rejects control characters; `xmlEscape` and `encoding/xml` cover the plist and the XML; the name grammar keeps Label, Description and wrapper headers free of metacharacters.
- The Windows wrapper (read, not run under cmd.exe): every token quoted, `%` doubled, trailing-backslash runs doubled, `chcp 65001` ASCII and before any non-ASCII byte, no name-derived traversal.
- Descriptor strings reaching a terminal: stripped of C0, DEL and C1 at read time. Not stripped: Unicode bidi overrides (U+202E) and U+2028 in Exe, Log and Job text (display reordering only; a low observation).
- Verify descriptor trust (FB-4): a backup-shaped job, a job for another folder on this host and the backup's own name are refused; `-out A -remove -- verify -out B` is refused as a stray flag combination; F12's `CheckSignIn` adds no path or secret exposure.
- A planted verify descriptor cannot make `verify_stale` lie beyond what deleting the file already does; copying a backup descriptor over the verify descriptor is rejected; a symlink at the descriptor path is replaced, not followed; the run-log symlink is refused (a TOCTOU window remains).
- Remove deletes the entry before the descriptors, so a crash leaves a recorded-but-gone entry that the retry clears; each kind's descriptor goes only with its own entry; an unreadable backup descriptor survives a custom-named removal; descriptor writes are temp plus rename at 0600.
- `-race` on `internal/schedule`, `internal/health` and `cmd/mailarchive` passes (the finders' run on a69228d, and mine at the fix head).

### What this round did not cover

- Real OS schedulers: no launchctl, crontab or schtasks write was made; Windows and launchd paths were read from code, compiled with `gofor`, and exercised only through the fakes.
- Windows reserved device names (`-name NUL`, `CON`) in wrapper and side-file paths: not exercised.
- Task Scheduler's expansion of `%VAR%` in the Arguments element of the GUI's non-wrapper tasks: unproven.
- The pasteable remedies still carry the recorded job's other flags (`-input`, `-copy-first`, `-manifest`) into a `schedule ... -install` line aimed at this archive; they are now inert to the shell, but an operator who pastes one installs a job the folder's writer chose. Unchanged by design (friction #14); the operator reads the line.
- The `-race` runs cover the three packages named; timing-sensitive suites were not stressed.

### Verification at the fix head

Unfiltered `go test ./...` through `inhome`, `-race` on `internal/schedule`, `internal/health` and `cmd/mailarchive`, `vet3` (darwin, linux, windows), a Windows test build of `internal/schedule` and `cmd/mailarchive` through `gofor`, and `gofmt -l`: all clean at the commit that precedes this file.
