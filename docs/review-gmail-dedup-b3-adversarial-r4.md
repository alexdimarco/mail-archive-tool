# Adversarial pass, round 4: Gmail de-dup slice B3's round-3 fixes

**Verdict: NOT CLEAN (5 LOW, no MED or higher).** H1 to H6 from round 3 are closed for what they
named: every round-3 probe that was red is green on macOS and on the Windows VM, H5's probe brought
to the new fingerprint API is green, and H2, H4 and H6 hold in the code. The new archive guard
(`archiveguard.go`) does what its note says between the start of the walk and the after-walk
proof, and its new id check gave no false "lock lost" on a healthy archive in any probe (a lock
file made unreadable still ends a run, as it always has: K2). Five LOW findings:

- **J1 (LOW).** The last proof comes before the run's tail, not after it. After it, a local run
  reads Thunderbird's folder cache, writes every folder page, the README and the report, by path; a
  Microsoft 365 capture writes its footer, its last manifest save and then the same. A swap in that
  tail puts the first archive's folder pages, root `index.html`, README and report into the other
  archive, and B's own next backup with nothing new does not put them back. Proved on macOS (folder
  renamed) and on Windows (drive letter moved with `subst`).
- **J2 (LOW).** The guard is plaintext only, so an encrypted run swapped mid-walk keeps writing its
  sealed units and chunks into the other encrypted archive until the store's save, and then writes
  its "failed, lock lost" record, run history entry and attention notice into it: B's status reads
  failed though B's own last backup was fine. Proved on macOS.
- **J3 (LOW, PLAUSIBLE).** `RunGraph` has no post-load check, and its one-time identity-collapse
  save (`graph.go:428`) is a manifest save with no proof before it.
- **J4 (LOW).** H3's fix has one gap left: a real failure whose email could not be sent, then the
  tidy-up wait's alert. The wait's email (worded "Nothing is lost") is delivered as the real
  streak's, and every later real failure is "told already": the real failure's email never goes.
  Proved on macOS and Windows.
- **J5 (LOW).** H4's syntax checks accept a guard call in dead code or a closure never called, and
  `ComputesUserFolder` does not see `os/user`, `os.ExpandEnv`, a `Getenv` of a named constant, or
  `windows.KnownFolderPath`. No package is missed today.

No product code was changed by this pass. The probes are kept out of the branch (build tag
`b3adv`, uncommitted, and kept between runs under the worktree's `.claude/testdata/`, which the
covers scan skips), because the ones that prove findings turn the suite red.

## Header

- **Reviewed commit:** `1b84d10` on `gmail-dedup`; this round's diff `a7dac7e..1b84d10` (MA-1328 to
  MA-1335). Round 3: `docs/review-gmail-dedup-b3-adversarial-r3.md`. Fix notes: design §13 "Folded
  2026-11-14 [B3 adversarial round 3 fixes]".
- **Blob ids at `1b84d10`:** `internal/app/archiveguard.go` 94698cb, `internal/app/run.go` ea8c3df,
  `internal/app/graph.go` 3117d6c, `internal/app/gmailswitch.go` 8dceb70,
  `internal/backupalert/streak.go` a89aceb, `internal/desktop/gmailquestion.go` 4b3373c,
  `internal/testhome/reach/reach.go` 572f525, `assureblock/testhome_meta_test.go` 23ff84f,
  `internal/state/dedup.go` d5093b7, `internal/pages/pages.go` c3e8efd. A finding is stale when its
  cited file changes.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who wrote neither the
  slice nor its fixes, worked the integrity (time and concurrency), insider and outside lenses as
  finder, then as skeptic. Every CONFIRMED row has a probe against the real code; every HELD row
  cites the probe or the code that refutes it.
- **Worktree:** `.claude/worktrees/gmail-dedup-b3-adv4`, branch `wip/gmail-dedup-b3-adv4` at
  `1b84d10`. Probe files (build tag `b3adv`, not committed): rounds 1 to 3's nine, copied from
  `gmail-dedup-b3-adv3`, with round 2's Desktop probe brought to the round-4 API (it now sends the
  fingerprint of the stores the page drew, so a Yes is refused or kept for the archive id alone);
  and four new files: `internal/app/zz_b3adv_r4_test.go`,
  `internal/desktop/zz_b3adv_r4_gmail_test.go`, `internal/backupalert/zz_b3adv_r4_streak_test.go`,
  `internal/testhome/reach/zz_b3adv_r4_reach_test.go`. Tests `TestB3Adv*`, `TestB3AdvR2*`,
  `TestB3AdvR3*`, `TestB3AdvR4*`. Between runs the probes live in the worktree's
  `.claude/testdata/probes/` (a `testdata` folder: the covers scan skips it; a builder's covers scan
  hit the probes when kept elsewhere in a worktree), and they were taken out of the package
  folders before the unfiltered run.
- **Real primitives only.** Real archives made by real backup runs of the package's
  Thunderbird-shaped profiles and its PST fixture, real encrypted archives (`vault.PrepareCreate`),
  the real lock, last-run record, alert streak (sender replaced by a recorder), answer file, and the
  Desktop route through its HTTP handler. The one seam used to place a swap is the package's own
  `readTBCache` variable (the folder-cache read, the first step after the last proof) and the
  progress callback. Throw-away `HOME`, `XDG_CONFIG_HOME`, `APPDATA` per test. No real email, no
  browser, no Desktop link, no dialog; nothing outside `repositories/` touched; the real settings
  folder never read.
- **Windows:** the Parallels VM "Windows 11", nobody signed in, so the probes ran as SYSTEM through
  this round's own copy of `winrun-at.sh` (`.claude/winrun-b3adv4.sh` in the worktree: its own
  `src-b3adv4` and `wintest-b3adv4` lab folders, its own `C:\matest-b3adv4` program folder and
  `r4probe*` program names, the share reached as `\\Mac\winlab`; only `-test.run B3Adv` runs; test
  programs cross-built on the Mac with `GOTOOLCHAIN=local`). The VM was not restarted, suspended or
  signed into. The `subst` probes remove their mapping; `subst` with no arguments on the VM
  afterwards lists none.
- **Commands and results** (Go 1.26.0 darwin/arm64; Windows programs cross-built by it for
  windows/amd64; 2026-11-14):
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/app/ ./internal/archdelete/ ./internal/gmailanswer/ ./internal/pages/ ./cmd/mailarchive/ ./internal/wizard/ ./internal/desktop/`
    (macOS, round 1 to 3 probes): every round-1, round-2 and round-3 probe passes, but round 2's
    `TestB3AdvR2NotNowKeepsAWaitingYes`, red only because it posts a Yes without the fingerprint
    (H5's refusal); brought to the round-4 API, it passes.
  - `go test -tags b3adv -run "B3AdvR4|B3AdvR2NotNow|B3AdvR2StalePage" -count=1 -v ./internal/app/ ./internal/desktop/ ./internal/backupalert/`
    and `… ./internal/testhome/reach/` (macOS): red `TestB3AdvR4SwapAfterTheLastProofWritesIntoAnother`
    (J1), `TestB3AdvR4EncryptedSwapMidWalk` (J2), `TestB3AdvR4UndeliveredRealFailureIsToldByTheWaitsEmail`
    (J4); every other round-4 probe passes; the reach probe logs J5's rows.
  - `zsh .claude/winrun-b3adv4.sh … B3Adv internal/app internal/desktop internal/wizard internal/backupalert cmd/mailarchive internal/gmailanswer internal/archdelete internal/pages`
    (Windows, as SYSTEM): desktop, wizard, cmd/mailarchive, gmailanswer, archdelete and pages pass
    (H5's round-4 probe and the CSRF probe included); app: `TestB3AdvR3DriveLetterSwapDuringTheWalk`
    and `TestB3AdvR2DriveLetterSwapDuringPresync` pass, red `TestB3AdvR4DriveLetterSwapAfterTheLastProof`
    (J1), and the two round-2 rename probes stop at "Access is denied" as in rounds 2 and 3
    (Windows will not rename a folder whose lock file is open); backupalert: red J4's probe.
  - Unfiltered, probes out: `go test -count=1 -p 4 -timeout 60m ./...` (macOS): 60 of 62 packages pass (the throw-away-home meta-test and the covers scan included). Red: `internal/app` on `TestArchiveExportTakesNoLockBesideACapture` (a copy export beside a capture; 3 of 3 alone, the copy-export load class the builder also saw), and `internal/presync`, which hung in `TestWatchdogAbandonedSessionStartsNoQuitAndLatchesTheLock/the_session_returns_first` until the 60-minute timeout (5 of 5 alone, and the whole package alone in 10 s). Neither touches the guard; the presync hang is a test that waits with no deadline under load, worth a deadline of its own, not a finding of this round
  - The reach graph by system: `go list -test` of the module with `GOOS` darwin, linux and windows
    (a scratch script, `.claude/scratch/reachdiff.py`): the same 32 test packages reach a per-user
    folder root on all three, so a Linux-only CI run of the meta-test misses none.
- **Count:** 27 attacks; 5 confirmed or plausible (all LOW); 22 held or refuted.

## Round-3 findings: verdicts

| ID | Round 3 | Round 4 verdict | Evidence |
|---|---|---|---|
| H1 | MED, CONFIRMED | **CLOSED** for the window it named (the walk, and the save after it); J1 is the tail after the last proof, J2 the encrypted walk, J3 Graph's pre-walk save | `TestB3AdvR3SwapAfterTheRecheckWritesIntoAnother` and `TestB3AdvR3SwapDuringTheWalkSwitchesAnother` (incremental and full) pass on macOS: run ends "now holds another archive … recorded nothing in it", B stays version 6, its manifest unchanged. `TestB3AdvR3DriveLetterSwapDuringTheWalk` passes on Windows. New: `TestB3AdvR4SameIDCopySwappedMidWalk` (a rotation drive seeded as a copy of A, same id) is stopped by the lock's nonce ("lock … was replaced"); `TestB3AdvR4NoIDArchiveSwappedMidWalk` (an id-less B with its own stale lock) is stopped and B keeps no id. Proof sites: `run.go:1040-1042` (on time), `:1050-1056` (checkpoint), `:1096-1098` (cancel), `:1122-1127` (each source), `:1161-1166` (after the walk); `graph.go:498-500`, `:504-511`, `:541-547`, `:618-623` |
| H2 | LOW, CONFIRMED | **CLOSED** | `TestB3AdvR3NoIDWaitOutlivesItsArchive` passes on macOS and Windows: `endTidyWait` (`gmailswitch.go:153-159`) ends the "" wait at every backup past the manifest. Note on the stated cost: the design says an id-less drive's 3-day alert "can come later, never sooner"; when the other drive of the rotation is backed up more often than every 3 days, every one of its backups restarts the "" wait, so the alert can never come. `status` still says the tidy-up at each skip (the design says so), so this is a wording point for the note, not a finding |
| H3 | LOW, CONFIRMED | **CLOSED** for what it named; J4 is the undelivered-email case | `TestB3AdvR3TidyAlertMasksTheNextRealFailure` passes on macOS (skips on Windows: its premise needs `chmod`). Both callers keep the tidy-up error in the chain (`wizard.go:445`, `capture.go:179`, `%w`), so `state.TidyingElsewhere` (`dedup.go:254-257`) sees it. The streak's `Waiting` is set only by a wait that opens a streak (`streak.go:150-160`) |
| H4 | LOW, PLAUSIBLE | **CLOSED** for the packages; J5 is what the syntax checks accept | `TestEveryPackageReachingTheSettingsRunsInAThrowAwayHome` passes (unfiltered run). The reach sets under `GOOS` darwin, linux and windows are the same 32 packages, so the Linux-only CI misses no Windows-only route. Every one of the 32 reaching packages has a TestMain calling `testhome.Run` (31, plus `internal/testhome`'s own unqualified `Run`), none `Check` alone |
| H5 | LOW, CONFIRMED | **CLOSED** | Round 3's probe passes, but only because it sends no fingerprint. Brought to the round-4 API, `TestB3AdvR4StalePageFingerprintRefused` (macOS and Windows) reads `data-gmail-asked` from the page as drawn: after a later backup finds `imap.gmail.com-1` off, that Yes is refused ("This archive's Gmail question changed since this page was drawn. Reload the page.") and nothing is kept; the page drawn again is accepted and keeps both; a store removed since is refused too; Not now needs no fingerprint. `TestB3AdvR4FingerprintBehindCSRF`: a Yes with the right id and fingerprint but no CSRF token is refused 403 before anything is read. Card and Settings draw the digest from the same set the route checks (`archives.go:215-221`, `gmailquestion.go:334-341`, `:395-409`, all `anyOff()`). The encrypted card: `gmailquestion.go:152-156` (MA-1334's product test covers it) |
| H6 | LOW, CONFIRMED | **CLOSED** | `TestB3AdvR3TerminalNotNowStillSwitchesInTheSameRun` passes on macOS and Windows: asked once, Not now, the run ends version 6. `yesFor` is cleared only in the branch where the person was asked (`gmailswitch.go:121-131`), so a scheduled run (nobody to ask) still applies a kept yes to every store it names, whatever the order |

## New findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| J1 | integrity (time) | The after-walk proof (`run.go:1161-1166`) is the last. After it: the Thunderbird folder-cache read (`:1170`), `pages.Generate` over the whole index (`:1182-1186`, every folder page and the root `index.html`, by path through `export.WriteFileAtomic`), `finish`'s README (`:374-376`) and report (`:407`). The capture's last proof (`graph.go:618-623`) is followed by the footer, the last manifest save (`:629`), the pages (`:635-639`) and `finish` (`:641`). Page generation reads every row of the index, so the tail grows with the archive and is not bounded by `archiveRecheckEvery` | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR4SwapAfterTheLastProofWritesIntoAnother` (macOS, folder renamed at the folder-cache read): A's run ends "now holds another archive"; B's manifest and record are unchanged, but B gains `imap.a.example/AlphaOnly/index.html` and `imap.a.example/INBOX/index.html`, and its `index.html` and `README.txt` are rewritten: B's root page now lists A's folders and not B's own. B's own next backup with nothing new keeps A's root page (`pagesNeeded`, `run.go:1290-1296`: nothing exported and the same gap count). `TestB3AdvR4DriveLetterSwapAfterTheLastProof` (Windows, `subst Q:` moved to drive 2 at the same point): the same four files | | |
| J2 | integrity (time) | The guard is plaintext only (`archiveguard.go:68-70`, `:93`; `archiveChangedFor` `:35-37`). An encrypted run's only proof is the sealed save's (`container/store.go:932-934`), at a checkpoint (1,000 or more messages) or a store's end; its deferred record has no "another archive" case (`run.go:817` is false for a keyed run), so `recordRun` writes the run's failure into the folder | CONFIRMED, LOW (macOS) | `TestB3AdvR4EncryptedSwapMidWalk`: encrypted A (its key) and encrypted B (its own key); A's folder renamed at the walk's first message. A's run ends "the archive lock was lost before the record was saved". B gains 39 to 41 sealed files (two runs) under `units/` and `chunks/` (A's, under A's key) and `BACKUP-NEEDS-ATTENTION.txt`, and its `.mailarchive-lastrun.json` and `.mailarchive-runhistory.json` are rewritten: B's record says `failed`, kind `lock_lost`, so B's status reads failed though B's own last backup was fine. B's keyless check is still intact ("39 file(s) in the archive are not in its file list — they do not change the outcome"), and B's own next backup succeeds and clears the record. B's own next backup also removes A's files: its check after it lists none outside its file list | | |
| J3 | integrity (time) | `RunGraph` has no post-load check (Run has `run.go:888-893`). Between its lock and its walk it opens the history log and writes its header (`graph.go:303-313`), opens the index and repairs its keys (`:341-355`), and for an archive that still owes the one-time v3 to v5 collapse saves the manifest (`:428`) with no proof before it | PLAUSIBLE, LOW (not probed) | The window is short: the sign-in is before the lock (`:143`), so no wait sits in it. The collapse save is the one manifest save of the capture with no proof; it runs only for an archive written before the v5 identity change, and the collapse reads `.eml` files of every duplicate pair (`:410-411`), so on a large old archive it is not instant | | |
| J4 | integrity (alerts) | `streak.go:150-160`: a real failure whose email was not delivered leaves `Failing` false, `Noticed` true, `Waiting` false. A tidy-up wait's alert after it is "the same streak" (`!prev.Waiting`), keeps `Waiting` false (`:156-158`), and delivers the wait's email; the streak is now `Failing`. Every later real failure is "told already" (`:151-153`) | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR4UndeliveredRealFailureIsToldByTheWaitsEmail`: the sender refuses once. Real failure: notice raised, email not delivered. Wait alert: delivered, worded "Another computer started tidying this archive … Nothing is lost …". Real failure again: nothing sent. The real failure's email never goes; the person's only email says nothing is lost. Needs the mail server to be unreachable at the first failure and a tidy-up to begin elsewhere within that streak; the notice for the real failure was raised | | |
| J5 | integrity (test hygiene) | `reach.CallsGuard` (`reach.go:80-115`) accepts any call to `testhome.Run` or `Check` inside the body of TestMain, dead code and closures included, and `Check` alone, which only reports after the tests ran. `ComputesUserFolder` (`:36-74`) sees only `os.User…Dir` and a literal `os.Getenv`/`LookupEnv` of a per-user variable | CONFIRMED, LOW (macOS, syntax probe) | `TestB3AdvR4ReachChecksEdges`: `CallsGuard` is true for `if false { testhome.Run(…) }`, for `_ = func() int { return testhome.Run(…) }`, and for `testhome.Check(…, m.Run())` with no throw-away home set; `ComputesUserFolder` is false for `os.Getenv(home)` with `const home = "HOME"`, `os.ExpandEnv("$HOME/…")`, `os/user.Current().HomeDir`, `syscall.Getenv("HOME")` and `windows.KnownFolderPath(FOLDERID_RoamingAppData, 0)`. No package is missed today: every current TestMain calls `Run`; the packages using `os/user` (`app`, `archdelete`, `desktop`) and `KnownFolderPath` (`tbrestore`, `presync`) are roots or reach one anyway. Note: `KnownFolderPath` does not follow a throw-away `APPDATA` at all, so on Windows `presync`'s and `tbrestore`'s Thunderbird lookups read the real (or SYSTEM's) roaming folder whatever `testhome` sets; MailArchive's own settings folder (`job.Dir`, `os.UserConfigDir`) does follow it | | |

### J1 (LOW): the tail after the last proof

**What happens.** Round 3's fix proves, before every save and every 10 seconds of the walk, that the
folder still holds the run's archive. The last proof is right after the walk, before "the pages and
the README". But the pages are not one write: `pages.Generate` reads every row of the search index
and writes one page per folder (more for a large folder), then the root `index.html`; then `finish`
writes the README and the report. All by path. A drive swapped (or a folder renamed) in that tail
receives the first archive's pages, README and report. The record, the manifest and the Gmail switch
are not written (the deferred record checks again, and they come before the last proof), and the run
ends as a lost lock, as the fix intends.

**Why it matters.** The other archive's browsable root page now lists the first archive's folders,
subjects and senders, with links to message files it does not hold; its report is the first
archive's (or removed, when the first has none). It stays so until the other archive's next backup
that brings something new: a backup with nothing new keeps the pages. Nothing is lost and `reindex`
redraws the pages. In a rotation of one mailbox the two listings mostly agree.

**Why LOW.** No message, manifest or switch is wrong; the pages are a view that a later backup or a
`reindex` redraws. The window grows with the archive, though, and the fix note says "nothing more is
written there"; the design's "the after-walk proof covers the swap after the last save" holds for
the record and the manifest only.

**Fix, in plain words.** Prove again inside the tail: between folder pages (on time, as the walk
does), and once more before the README and the report; or write the pages into a scratch folder in
the archive and move them in after a last proof. The capture's tail the same.

### J2 (LOW): the encrypted walk has no proof on time

**What happens.** The guard returns at once for an encrypted run, on the reasoning that a sealed
save re-proves the lock at its commit point and that the key opens only its own archive. Both hold
for the record. But between saves an encrypted run writes its sealed units and chunks by path, and
saves come only at a checkpoint or a store's end: a whole large store's first backup can be one
stretch. A swap there puts the first archive's sealed files into the other encrypted archive until
that save. Then the save stops the run, and the deferred record, which has no "another archive"
case for a keyed run, writes the run's failure into the other archive's folder.

**Why LOW.** The sealed files are unreadable there and do not change the other archive's check, and
its next backup clears the record. What the person sees in between is the other archive marked
failed, with `BACKUP-NEEDS-ATTENTION.txt`, for a backup that was not its own.

**Fix, in plain words.** Prove the lock on time between saves for an encrypted run too (the lock
nonce is the proof there, as the clone probe shows it is for a same-id plaintext copy), and let the
deferred record skip a keyed run whose lock is no longer its own.

### J3 (LOW, PLAUSIBLE): the capture's pre-walk save

`RunGraph` writes its history header, opens and repairs the index, and (once, for an old archive)
saves the collapsed manifest before its walk, with no proof after the load. There is no wait in that
window (the sign-in comes before the lock), so it is narrow; the collapse save is the one save of a
capture the H1 fix does not prove. **Fix:** the post-load check `Run` has, and a proof before the
collapse save.

### J4 (LOW): an undelivered failure email is answered by the wait's

H3's fix makes the tidy-up wait's alert a streak of its own unless it falls inside a real failure's
streak, where it "is told already". A real failure whose email could not be sent is such a streak
(its notice was raised), so the wait's alert keeps it real and sends its own email, worded as the
wait. Once that email is delivered the streak is "failing", and the real failure is never emailed.
**Fix:** inside a real streak whose email is still owed, send the real failure's email (or nothing)
rather than the wait's; that is, a wait alert never marks a real streak delivered.

### J5 (LOW): what the throw-away-home checks accept

The checks are against an accidental omission, and none exists today. They would pass a TestMain
whose guard call is dead, and a TestMain that calls only `Check` after `m.Run()`, which reports a
leak after the tests made it. **Fix, if wanted:** require the call to be `os.Exit(testhome.Run(…))`
or a top-level statement of TestMain, and accept `Check` only beside a `Run`; add `os/user` and
`os.ExpandEnv` to the roots. `KnownFolderPath` is a different matter (it ignores the environment by
design); the note above is for the record.

## Held and refuted attempts

| ID | Attempt | Verdict | Evidence |
|---|---|---|---|
| K1 | False "lock lost": the last-run record unreadable for a moment (torn by another writer, or gone) during a healthy run, with a proof at every message | HELD | `TestB3AdvR4RecordBrieflyUnreadableIsNoFailure` (macOS and Windows, both forms): the run ends ok. An unreadable record reads as no id, and the proof falls to the lock (`archiveguard.go:43`), which holds |
| K2 | False "lock lost": the lock file unreadable for a moment | HELD (old semantics, no realistic trigger found) | `TestB3AdvR4LockBrieflyUnreadableEndsAHealthyRun` (macOS, `chmod 000` for one message): the run ends "lock … was removed during the run (permission denied)", as `StillHeld` always has (`lockfile.go:142-145`); the guard asks it every 10 s and at every source's end instead of every 1,000 messages. On Windows no other program can deny read sharing on the lock while the run's own read-write handle is open (it would have to allow read and write sharing to open it at all, `lockfile.go:59`), and no MailArchive writer replaces the lock of a held archive |
| K3 | False "another archive": a writer changes the record's id mid-run | HELD | The writers of the record during a run: `RecordRefusedRun` takes the lock and skips when held (`run.go:1435-1438`); the Desktop's `recordStartFailure` (`capture.go:262`) and every other `WriteLastRun` carry the folder's id when given none (`lastrun.go:258-260`); `RecordRunWarning` takes the lock and refuses a running record (`tbcompleteness.go:701-718`). Only `beginRun`, under the lock, gives an id |
| K4 | A rotation drive seeded as a copy of the first (same id) swapped in mid-walk | HELD | `TestB3AdvR4SameIDCopySwappedMidWalk` (macOS): stopped by the lock's nonce ("lock … was replaced during the run"); the copy's manifest unchanged |
| K5 | An id-less archive with its own stale lock swapped in mid-walk (only the lock guards) | HELD | `TestB3AdvR4NoIDArchiveSwappedMidWalk` (macOS): stopped ("now holds another archive"), B's manifest unchanged, B given no id |
| K6 | A user renames the archive folder mid-run; a case-only rename; an NFC/NFD path | HELD | A rename leaves nothing at the path: `Lstat` fails, the run ends run-fatal "unavailable" (`archiveguard.go:72-74`), better than before H1, when the walk's `MkdirAll` recreated a decoy at the old path. A case-only or normalization-only rename on APFS keeps the path valid, the id and lock the same. Windows refuses the rename while the lock is open (the round-2 rename probes, "Access is denied") |
| K7 | A clock jump (or sleep) makes the 10 s proof fire wrongly | HELD | `due()` is `time.Since` of a `time.Now()` (`archiveguard.go:71`, `:93`), which uses Go's monotonic clock; a jump changes only when the next proof runs, never its answer |
| K8 | The proof's cost on a very large archive | HELD | `due()` is one monotonic compare per progress call; a proof is one `Lstat`, one bounded record read (`util.ReadRegularFile`, `maxLastRunSize`) and one lock read, at most every 10 s and once per source before a save that already writes the whole manifest. On a network share that is three round trips per 10 s |
| K9 | H4: a package that reaches a root only on Windows is missed by the Linux-only CI run of the meta-test | HELD | `.claude/scratch/reachdiff.py`: `go list -test` under darwin, linux and windows gives the same 32 reaching packages |
| K10 | H5: the fingerprint leaks the store names, or weakens CSRF | HELD | 12 bytes of SHA-256 of the sorted names (`gmailquestion.go:203-208`); checked after `GuardLocalPOST` (`TestB3AdvR4FingerprintBehindCSRF`: 403, nothing kept); not needed for Not now |
| K11 | H6: a scheduled run (nobody to ask) loses a kept yes when an unanswered store is read first | HELD | `yesFor` is cleared only after `g.ask` was called (`gmailswitch.go:121-131`); with nobody to ask `GmailDecision` never returns `GmailAskPerson` |
| K12 | SQLite's path-based cleanup on close: after a rename swap, the run's index close unlinks `search.db-wal`/`-shm` at the old path, now the other archive's | NOT PURSUED | Harmful only when the other archive has a live write-ahead log (it crashed mid-write); the index is rebuilt by `reindex`. Mentioned for completeness |
| K13 | Rounds 1 to 3's attempts come back with this round's changes | HELD | Every round-1, 2 and 3 probe passes on macOS and Windows (header), round 2's Desktop probe after it was brought to the fingerprint API |

## Probe locations (uncommitted, build tag `b3adv`; kept in `.claude/testdata/probes/`)

- `internal/app/zz_b3adv_r4_test.go`: J1 (macOS and Windows forms), J2, K1, K2, K4, K5.
- `internal/desktop/zz_b3adv_r4_gmail_test.go`: H5 (round-4 API), K10.
- `internal/backupalert/zz_b3adv_r4_streak_test.go`: J4.
- `internal/testhome/reach/zz_b3adv_r4_reach_test.go`: J5.
- `internal/desktop/zz_b3adv_r2_gmail_test.go`: brought to the fingerprint API.
- Rounds 1 to 3's other files, unchanged.
