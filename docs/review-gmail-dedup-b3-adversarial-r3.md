# Adversarial pass, round 3: Gmail de-dup slice B3's round-2 fixes

**Verdict: NOT CLEAN (1 MED, 5 LOW).** G1 to G8 from round 2 are closed: every round-2 probe that
was red is green on macOS and on the Windows VM (the drive-letter form of G1 included), and the
three PLAUSIBLE ones (G4, G6, G8) are closed in the code. One new finding gets around G1's fix:

- **H1 (MED).** The new check runs once, right after the manifest loads. A drive swapped (or a
  folder renamed) after that check, while the run walks its sources, is not seen again: the walk
  goes on with the first archive's manifest, the Gmail store is switched in memory with the first
  archive's yes, and the store's save writes that manifest into the folder, which now holds the
  other archive. Nothing re-checks the lock before that save (a checkpoint re-checks it only after
  1,000 or more new messages, so an incremental run that brings little or nothing new never does).
  The run then ends "ok". Proved on macOS (folder renamed at the index open, and mid-walk in an
  incremental and a full run) and on the Windows VM (drive letter moved with `subst` mid-walk): the
  other archive is version 7 with `imap.gmail.com` on, and its manifest is the first archive's.

The five LOW findings: a tidy-up wait kept under no id outlives its archive, so a later id-less
archive at that folder is "waited since" an old date and alerts on its first skip (H2); the tidy-up
wait's 3-day alert opens the failure streak, so a real failure right after the tidy-up ends sends
no email and no notice (H3); the throw-away-home guard covers only the packages that reach
`internal/app` or `internal/gmailanswer`, not every package whose tests can reach the settings
folder (H4, PLAUSIBLE); the card's Yes covers a Gmail store found off after the page was drawn (H5);
a Not now typed at the terminal does not stop the same run from applying an earlier yes (H6).

No product code was changed by this pass. The probes are kept out of the branch (build tag
`b3adv`, uncommitted), because the ones that prove findings turn the suite red.

## Header

- **Reviewed commit:** `a7dac7e` on `gmail-dedup`; this round's diff `8c9af02..a7dac7e` (the fixes
  for G1 to G8 and the throw-away-home guard, MA-1316 to MA-1327). Round 2:
  `docs/review-gmail-dedup-b3-adversarial-r2.md`. Fix notes: design §13 "Folded 2026-11-14 [B3
  adversarial round 2 fixes]".
- **Blob ids at `a7dac7e`:** `internal/app/run.go` a4d4a52, `internal/app/gmailswitch.go` 95468fb,
  `internal/app/graph.go` 0795dd9, `internal/gmailanswer/gmailanswer.go` b257c47,
  `internal/gmailanswer/tidywait.go` 0d0f1ba, `internal/desktop/gmailquestion.go` ea25904,
  `internal/desktop/capture.go` 080942a, `internal/wizard/wizard.go` fd27d80,
  `internal/wizard/jobrun.go` f58789c, `cmd/mailarchive/main.go` 074037a, `cmd/mailarchive/gmail.go`
  b3035a2, `internal/backupalert/streak.go` 0eb8076, `internal/testhome/testhome.go` c013295,
  `assureblock/testhome_meta_test.go` 006ea13. A finding is stale when its cited file changes.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who wrote neither the
  slice nor its fixes, worked the outside, insider and integrity lenses as finder, then as skeptic.
  Every CONFIRMED row has a probe test against the real code; every HELD row cites the code or the
  probe that refutes it.
- **Worktree:** `.claude/worktrees/gmail-dedup-b3-adv3`, branch `wip/gmail-dedup-b3-adv3` at
  `a7dac7e`. Probe files (build tag `b3adv`, not committed): rounds 1 and 2's seven, copied from
  `gmail-dedup-b3-adv2`, with the two round-2 Desktop probes brought to the round-3 API (the page now
  sends the archive id, so a probe that sent none would pass for the wrong reason) and the round-2
  tidy-wait probe brought to the per-id wait; and two new files,
  `internal/app/zz_b3adv_r3_test.go` and `internal/desktop/zz_b3adv_r3_gmail_test.go`, with one
  round-3 probe added to `internal/wizard/zz_b3adv_r2_tidywait_test.go`. Tests `TestB3Adv*`,
  `TestB3AdvR2*`, `TestB3AdvR3*`.
- **Real primitives only.** Real archives made by real backup runs of the package's
  Thunderbird-shaped Gmail profiles, the real answer file, job file, last-run record, tidy-up wait,
  archive lock, Desktop route (through its HTTP handler with the CSRF token), scheduled run-job with
  the alert sender and notice replaced by recorders. Throw-away `HOME`, `XDG_CONFIG_HOME`, `APPDATA`
  per test. No real email, no browser, no Desktop link, no dialog; nothing outside `repositories/`
  touched; the real settings folder never read.
- **Windows:** the Parallels VM "Windows 11". Nobody was signed in, so the probes ran as SYSTEM
  through this round's own copy of `winrun-at.sh` (`.claude/winrun-b3adv3.sh` in the worktree: its
  own `src-b3adv3` and `wintest-b3adv3` lab folders, its own `C:\matest-b3adv3` program folder and
  `r3probe*` program names, the share reached as `\\Mac\winlab`; only `-test.run B3Adv` runs; test
  programs cross-built on the Mac). The VM was not restarted, suspended or signed into. Both `subst`
  probes remove their mapping; `subst` with no arguments on the VM afterwards lists none.
- **Commands and results** (Go 1.26.0 darwin/arm64; Windows programs cross-built by the same
  toolchain for windows/amd64; 2026-11-14):
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/app/` (macOS): every round-1 and round-2
    probe passes; `TestB3AdvR3NoIDArchiveSwappedIn` passes; red:
    `TestB3AdvR3SwapAfterTheRecheckWritesIntoAnother`, `TestB3AdvR3SwapDuringTheWalkSwitchesAnother`
    (incremental and full), `TestB3AdvR3NoIDWaitOutlivesItsArchive`,
    `TestB3AdvR3TerminalNotNowStillSwitchesInTheSameRun`.
  - `go test -tags b3adv -run B3Adv -count=1 ./internal/archdelete/ ./internal/gmailanswer/ ./internal/pages/ ./cmd/mailarchive/ ./internal/wizard/ ./internal/desktop/`
    (macOS): every round-1 and round-2 probe passes (`TestB3AdvR2AnswerKeyUnicodeForm` included);
    red: `TestB3AdvR3TidyAlertMasksTheNextRealFailure` (wizard) and
    `TestB3AdvR3StalePageYesCoversAStoreAddedSince` (desktop).
  - `zsh .claude/winrun-b3adv3.sh … internal/pages internal/gmailanswer internal/archdelete internal/app cmd/mailarchive internal/wizard internal/desktop`
    (Windows, as SYSTEM): pages, gmailanswer, archdelete, cmd/mailarchive and wizard pass;
    `TestB3AdvR2DriveLetterSwapDuringPresync` passes (round 2: red); the two round-2 rename probes
    stop at "Access is denied" as in round 2 (Windows will not rename a folder whose lock file is
    open); red: `TestB3AdvR3DriveLetterSwapDuringTheWalk`, `TestB3AdvR3NoIDWaitOutlivesItsArchive`,
    `TestB3AdvR3StalePageYesCoversAStoreAddedSince`; `TestB3AdvR3TidyAlertMasksTheNextRealFailure`
    skips (its premise, an unreadable source, cannot be made with `chmod` there).
  - The guard check (H4): `go test -count=1` of `internal/tbrestore`, `presync`, `thunderbird`,
    `tbcomplete`, `delmark`, `job`, `archindex` and `schedule` with `HOME` set to a stand-in folder
    in the worktree (`GOPATH`, `GOCACHE`, `GOMODCACHE` and `GOTOOLCHAIN=go1.26.0` given, so Go's own
    files are found): all pass, and nothing of MailArchive's was written into the stand-in home
    (only Go's telemetry counters).
  - `go test -count=1 -run ThrowAwayHome ./assureblock/` (macOS): passes.
- **Count:** 26 attacks; 6 confirmed or plausible (1 MED, 5 LOW); 20 held or refuted.

## Round-2 findings: verdicts

| ID | Round 2 | Round 3 verdict | Evidence |
|---|---|---|---|
| G1 | MED, CONFIRMED | **CLOSED** for the window it named (the pre-sync); H1 is the window after the new check | `TestB3AdvR2SwapDuringPresyncSwitchesAnother` passes on macOS: the run is refused ("now holds another archive … nothing was written into it"), B stays version 6 and keeps its own id. `TestB3AdvR2DriveLetterSwapDuringPresync` passes on Windows. `TestB3AdvR2TidyPutBackGivesAnotherArchiveTheId` passes on macOS: B's record keeps B's id after the skip, and nothing of A's is read for B. New: `TestB3AdvR3NoIDArchiveSwappedIn` (an id-less B, with and without a lock file) is refused too (`run.go:767-776`) |
| G2 | LOW, CONFIRMED | **CLOSED** on the card and in Settings; H6 is an in-run residual at the terminal | `TestB3AdvR2NotNowKeepsAWaitingYes` (sending the id, as the page now does) and `TestB3AdvR2NotNowBesideAYesStillSwitches` pass on macOS and Windows. `Answer.latest` (`gmailanswer.go:184-189`) drops a yes beside a Not now in `Keep` and `KeepHere`; `NotNowHere` (`:240-253`) takes it out of the job file |
| G3 | LOW, CONFIRMED | **CLOSED** | `TestB3AdvR2RotationResetsTheTidyWait` (per-id API) passes on macOS and Windows: four rounds of drive 2's backups leave drive 1's wait at its first start; once it is 4 days old, drive 1's skip sends the email and the notice. Residual H2 (the wait kept under no id) |
| G4 | LOW, PLAUSIBLE | **CLOSED** | `endTidyWait` is called by `app.Run` (`run.go:897`) and `app.RunGraph` (`graph.go:246`) after the load; `export -unattended`/`graph -unattended` exit 0 with "Skipped, not failed: " (`main.go:447-449`, `:1899-1901`, `scheduledTidySkip` `:1913-1922`); `HeadlessCapture` skips and alerts after 3 days (`capture.go:167-182`); `RunGraph` puts the record back (`graph.go:222-233`). Product tests MA-1320 to MA-1322. The alert side of one shared streak is H3 |
| G5 | LOW, CONFIRMED | **CLOSED** for another archive; H5 is the same page and archive with a store added since | `TestB3AdvR2StalePageYesBindsToTheArchiveThereNow` passes on macOS and Windows: A's id, an empty id and no id are each refused with "There is no Gmail question for this archive now. Reload the page." and nothing is kept; B's own id (spaces around it) is accepted. The check is after `GuardLocalPOST` (`gmailquestion.go:313`, the check at `:356`); the id reaches the page through `html/template` |
| G6 | LOW, PLAUSIBLE | **CLOSED** | `gmailAnswerEncrypted` keeps `e.gmailOff` at the click (`gmailquestion.go:149`); `RunJobInProcessKeyed` carries only `rn.GmailYesStores` (`jobrun.go:295`); `newGmailRun` applies them per store (`gmailswitch.go:72-82`). The id check is skipped only when `opts.Key != nil`, and a plaintext kept answer is never read for an encrypted record (it carries no id, `gmailanswer.go:157-160`). Same at-the-click caveat as H5 |
| G7 | LOW, CONFIRMED | **CLOSED** (macOS) | `TestB3AdvR2AnswerKeyUnicodeForm` passes on macOS (skips on Windows by design). `folderKey` folds NFC on darwin only (`gmailanswer.go:91-93`). Not reported: APFS's own case folding differs from `strings.ToLower` for a few letters; at worst the person is asked again |
| G8 | LOW, PLAUSIBLE | **CLOSED** for the Desktop's Run now | `settleUnder` takes `rn.HoldJobFile` (`jobrun.go:65-72`, `:116`), which the Desktop sets to `jobFileMu` (`runnow.go:406`). No deadlock: the hold is taken after `app.Run` returns, and neither holder of `jobFileMu` (Save settings, the answer route) starts a Run now while holding it. Run-job and the terminal stay outside (MA-1307's concession) |

## New findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| H1 | integrity / insider (time) | The post-load check (`run.go:889-897`) runs once. After it, the index opens (`:953`), the yes is resolved (`:1025-1029`), the Gmail store is switched in memory at its start (`:1883-1893`), and the store is saved after its walk by path (`:1107`, `commit`, no lock check). A checkpoint re-checks the lock only once `effectiveEvery` (at least 1,000) messages were exported (`:1036-1048`). The deferred record then sees the change and writes nothing but the run returns `nil` (`:818-826`) | CONFIRMED, MED (macOS, Windows) | `TestB3AdvR3SwapAfterTheRecheckWritesIntoAnother` (macOS, folder renamed at the index open): the yes is not applied, but B's manifest is replaced by A's. `TestB3AdvR3SwapDuringTheWalkSwitchesAnother` (macOS, renamed at the walk's first message, incremental and full): run ok, B is version 7 with `imap.gmail.com` on, its manifest is A's. `TestB3AdvR3DriveLetterSwapDuringTheWalk` (Windows, `subst R:` moved to drive 2 mid-walk): the same | `b9e91fb`, `6cdaf70` | MA-1328 to MA-1330: `TestAFolderSwappedDuringTheWalkGetsNoManifestAndTheRunFails`, `TestADriveLetterMovedDuringTheWalkGetsNoManifest`, `TestACaptureWhoseFolderIsSwappedWritesNothingIntoTheOther`, `TestALockLostDuringASmallRunSavesNoManifest`, `TestTheWalkStopsSoonAfterItsFolderHoldsAnotherArchive` |
| H2 | integrity / availability | `endTidyWait` (`gmailswitch.go:148-156`) ends the wait under no id only when the record had no id before this run (`idBefore == ""`). When another computer's newer MailArchive gave the record an id during or after the tidy-up, this computer's next backup ends that id's wait only; the wait kept under "" stays, with its old start | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR3NoIDWaitOutlivesItsArchive`: after the skip of an id-less archive (wait under "", 10 days old), the record gains an id, a backup goes through, and the "" wait is still kept and Due. The next id-less archive at that folder (a rotation drive an older MailArchive last wrote) that meets a tidy-up reads "waited since" that date and alerts at its first skip | `b9e91fb` | MA-1331: `TestAWaitKeptUnderNoIDEndsAtAnyBackupPastTheManifest` |
| H3 | integrity (alerts) | The wait's alert goes into the backup alert's failure streak (`wizard.go:435-446`, `capture.go:178-179`); `backupalert.Report` sends nothing for a failure while the streak is failing (`streak.go:142-144`). A backup past the manifest ends the wait (`run.go:897`) but only a success clears the streak | CONFIRMED, LOW (macOS) | `TestB3AdvR3TidyAlertMasksTheNextRealFailure`: wait 4 days, the skip sends "failed" (worded as the wait) and one notice; the tidy-up ends; the next scheduled run gets past the manifest and fails (its source unreadable): no email, no notice. The person's last word is "Nothing is lost". The same holds in a G3 rotation: drive 1's wait alert masks a real failure of drive 2 | `e049744` | MA-1332: `TestATidyUpWaitsAlertNeverHidesTheNextRealFailure`, `TestTheTidyUpWaitsAlertIsAStreakOfItsOwn` |
| H4 | integrity (test hygiene) | `assureblock`'s meta-test names only packages whose tests reach `internal/app` or `internal/gmailanswer` (`testhome_meta_test.go:21`); `internal/tbrestore`, `presync`, `thunderbird`, `tbcomplete`, `delmark` and `job` reach `job.Dir` (the restore lock and `restores.log`, `tbrestore/restorelock.go:20-25`, `:76`; `thunderbird/restorekey.go:93-94`) with no TestMain throw-away home; `archindex` and `schedule` have their own, not checked by `testhome`. `usesTestHome` accepts the text `testhome.Run(` anywhere in a test file, a comment included (`:72-83`). `Contained` checks `job.Dir` only, not the cache folder (`job.ScratchDir`) | PLAUSIBLE, LOW | No current test is proved to open the real folder: the tests that take the restore lock set `HOME` per test (`tbrestore/ma1085_r607_r615_test.go:48`, `presync/ma1140_restore_lock_test.go:22`), the others only compute its path, and the eight packages run under a stand-in `HOME` wrote nothing of MailArchive's. A new test in those packages that takes the restore lock without `t.Setenv` would write into the real settings folder and nothing would fail | `a13d179` | MA-1333: `TestTheThrowAwayHomeCheckReadsCallsNotText`, `TestEveryPackageReachingTheSettingsRunsInAThrowAwayHome` |
| H5 | integrity (stale page) | The route reads the stores off at the click (`gmailquestion.go:352`, `:372-376`); the page carries the archive id only (`dashboard.go`, `data-gmail-archive`) | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR3StalePageYesCoversAStoreAddedSince`: page drawn when `imap.gmail.com` was off; a later backup of the same archive also found `imap.gmail.com-1` off; the page's Yes keeps both. The question names no store, so the person said yes to "this archive's Gmail"; G6's note says an account added since is never switched, which holds from the click, not from the drawing (the encrypted card the same) | `d1535e1` | MA-1334: `TestACardsYesCoversOnlyTheAccountsItWasDrawnFor`, `TestAnEncryptedCardsYesCoversOnlyTheAccountsItWasDrawnFor` |
| H6 | integrity | `newGmailRun` takes the kept yes into `yesFor` at the run's start (`gmailswitch.go:77-82`); a terminal Not now (`gmail.go:85-95`, `NotNowHere`) changes the files, not the run, and `store` applies `yesFor` before asking (`gmailswitch.go:113`) | CONFIRMED, LOW (macOS) | `TestB3AdvR3TerminalNotNowStillSwitchesInTheSameRun`: a yes kept for `imap.gmail.com`; a terminal backup reads `imap.gmail.com-1` first, asks, the person says Not now (the kept yes is taken back on disk); the same run then switches `imap.gmail.com`: version 7. The question had said "This cannot be undone" | `b9e91fb` | MA-1335: `TestANotNowInTheRunHoldsForTheRestOfIt` |

### H1 (MED): the archive changes after the new check, while the run walks

**What happens.** Round 2's fix checks, once the manifest has loaded, that the folder still holds
the archive the run began with and that the lock is still the run's. After that check the run opens
the index, decides the yes, and walks its sources. When the walk reaches a Gmail store, the store is
switched in memory (with the first archive's yes); when the store's walk ends, the manifest is saved
to the folder's path. Nothing between the check and that save asks again whether the folder still
holds the same archive. The lock is re-checked only at a checkpoint, which comes after 1,000 or more
new messages (more for a large archive): a scheduled incremental run, which usually brings a few
new messages, never reaches one. The walk of a large Gmail store (an All Mail of several gigabytes)
reads for minutes even when nothing is new.

So a drive swapped at the same path during the walk receives the first archive's manifest, switched
to version 7: the other archive becomes 1.8-only, and its manifest now lists the first archive's
messages. The run's deferred record sees the change and writes nothing, but the run returns no
error, so the scheduled job reports success, the alert streak records a success, and the first
archive keeps its record "running" (on drive 1) and its yes unspent.

The yes check itself held: when the swap comes before `archiveNow` is read (the index-open probe),
the yes is not applied, but the manifest is still written into the other archive. The overwrite is
the old lost-lock window of every local run (R5); this slice's Gmail switch rides it, and round 2's
fix note says "nothing is written into a folder that now holds another archive", which holds only up
to the check.

**Why MED.** The same harm G1 named (a switch nobody said yes for, for the archive it lands on),
plus the other archive's manifest replaced. The window is the walk, which every run has, not the
opt-in pre-sync. Proved on both systems, with the Windows form being a USB drive pulled and another
given its letter.

**Fix, in plain words.** Re-prove before every manifest save of a plaintext run what the check
proves (the folder's record carries the run's archive id and the lock file still holds the run's
nonce), as a sealed save already re-proves the lock at its commit point (`container.ErrLockLost`),
and stop as a lost lock when it does not. Also re-check the lock on time, not only on the count of
exported messages, and let the deferred record's "another archive" case end the run as `lock_lost`,
not ok.

### H2 (LOW): a wait kept under no id outlives its archive

An archive whose last-run record carries no id (written by an older MailArchive) meets a tidy-up:
the skip puts the record back, so the wait is kept under no id. If the other computer's newer
MailArchive gives the record an id while it finishes, this computer's next backup ends the wait of
that id only. The "" wait stays, with its old start, until the folder's settings are removed. The
next id-less archive at that folder that meets a tidy-up is "waited since" that old date in `status`
and alerts at its first skip.

**Fix:** end the "" wait too whenever the record had no id at the skip that made it (keep, with the
wait, the id the record carried then), or simply whenever a run of the folder gets past the manifest
and the record it began with carried no id or the id is new to the folder's waits.

### H3 (LOW): the tidy-up wait's alert hides the next real failure

The wait's alert is sent as a failure of the schedule's streak. The streak sends one email per run
of failures and clears only on a success. When the tidy-up ends and the next backup fails for its
own reason (a source gone, a full drive), no email and no notice go: the streak is still "failing".
The person's last email said the archive is waiting for another computer and "Nothing is lost".

**Fix:** when a backup gets past the manifest (the wait ends), end a streak the wait opened, so the
next failure is told; or send the wait's alert outside the failure streak.

### H4 (LOW, PLAUSIBLE): the throw-away-home guard covers fewer packages than the rule

The lead's rule is that no test reaches the real settings folder, even to read it. The meta-test
names only packages that reach the engine or the Gmail answer. Six packages reach the settings
folder by another way (the restore lock and the restore log, both in `job.Dir`) and keep no
throw-away home for the whole package; two keep one that `testhome` does not check. Today their
tests either set `HOME` themselves or only compute the path, so nothing is proved to leak; a future
test there is not caught. The meta-test's text match also accepts a comment.

**Fix:** widen `settingsLookups` to `internal/job` (every lookup goes through `job.Dir`), and match
a call in a `TestMain` (parse the file) rather than a substring.

### H5 (LOW): the card's Yes covers a store found off after the page was drawn

G5 binds the answer to the archive, not to the stores the page was drawn for. A page drawn when one
Gmail account was off, clicked after a later backup found a second one (an account added to
Thunderbird since), keeps the yes for both. The question names no account, so this may be the
intended reading; if so, G6's note ("never to an account added since the question was asked")
should say "since the answer".

**Fix:** carry the stores the page drew (or a digest of them) with the answer and refuse ("Reload
the page") when they differ, or amend the design note.

### H6 (LOW): a Not now at the terminal does not stop the run that asked

A yes kept for one store (from the card) and a terminal backup that reads a second Gmail store first:
the second store's question is asked, the person says Not now, and the files are updated (the yes is
taken back). The run, though, already took the yes into memory and switches the first store when it
reaches it. The archive becomes 1.8-only in the run where the person's latest answer was Not now,
after a question that says "This cannot be undone".

**Fix:** a Not now in the run clears `yesFor` (and stores already switched in this run stay, which
the question could say), or the run asks before applying a waiting yes when it can ask.

## Held and refuted attempts

| ID | Attempt | Verdict | Evidence |
|---|---|---|---|
| R1 | An id-less archive (an older MailArchive's record) swapped in during the pre-sync, where "the lock is still ours" is the only guard | HELD | `TestB3AdvR3NoIDArchiveSwappedIn`, macOS: refused with B's lock file there (its nonce is B's last run's) and with none (`StillHeld` reads the path, `lockfile.go:133-151`). Only a copy of A's folder taken during A's run would carry A's nonce; that is the conceded clone |
| R2 | The run's own "running" record could not be written (`beginRun` is best effort), so the folder's record lacks the new id | HELD | `archiveChanged` falls to the lock check (`run.go:775`), which holds; `archiveNow` needs the on-disk id (`:1026`), so the yes is not applied |
| R3 | The swap brings a version-8 archive: the load refuses before the check, so the put-back runs into the other folder | HELD | The deferred record checks `archiveChanged` before the put-back (`run.go:821-826`): nothing is written; the caller's `NoteTidyWait` then keeps the wait for the archive the folder holds (B's id), which is the one being tidied |
| R4 | An encrypted run's swap during the pre-sync (its manifest is loaded before it, `run.go:726-731`; `archiveChanged` is plaintext-only) | HELD (not probed) | Every sealed save re-proves the lock at its commit point (`container.ErrLockLost`, `stopOnLostLock`), and the key opens only its own archive |
| R5 | Forge or merge entries in the format-2 tidy-wait file | HELD | It lives in the owner-only settings folder (round-1 H8). Ids that are not 32 lowercase hex are dropped, starts more than a day ahead are dropped, another version reads as none (`tidywait.go:82-101`) |
| R6 | An older MailArchive reads format 2 | HELD | 1.7 and earlier keep no tidy-up wait; format 1 was only in unreleased builds of this branch, which read format 2 as no wait (design §13 G3 note) |
| R7 | Unbounded growth of the tidy-wait file | HELD | At most 64 ids (`tidywait.go:45-47`, `:145-147`); a wait is ended when its archive gets past the manifest, and Delete, Remove from list and uninstall remove the file. A 65th waiting archive at one folder is refused with a logged warning (no alert for it), reachable only by planting |
| R8 | A run that gets past the manifest and then fails ends a real wait and so suppresses the 3-day alert | HELD (the alert side is H3) | A manifest that loads is not at version 8 and carries no `merge_pending` (`state.Load`), so the tidy-up is over; the failure is the failure streak's |
| R9 | `RunGraph`'s put-back writes one archive's record into another (no `archiveChanged` there) | HELD (not probed) | Its window, from the snapshot (`graph.go:222`) to the load (`:243`), holds only the temp sweep; no pre-sync, no sign-in. The capture's walk re-checks the lock at its checkpoints as before |
| R10 | `export -unattended` / `graph -unattended` exit 0 hides a real failure from the scheduler | HELD (the posture is the U2 call) | Only `TidyingElsewhere` (`gmailswitch.go:137-140`: the load's refusal, its words exactly `TidyUpWords`, a well-formed version-8 or `merge_pending` manifest) exits 0. A stuck tidy-up then shows as success in the scheduler for ever, with `status` WARN (not RED) and no email (such jobs have none); the design note says so |
| R11 | G5: the archive id on the request weakens the CSRF guard, or a page drawn before this build keeps answering | HELD | The id is checked after `GuardLocalPOST` and the body decode (`gmailquestion.go:313-322`, `:356`); a page without the attribute sends `""`, which is refused (probe). The id is drawn through `html/template` (escaped) |
| R12 | G5: an id padded with spaces | HELD | `TrimSpace` then exact compare; a padded copy of the right id is the right id (probe) |
| R13 | G6: the encrypted path's skipped id check reached by a plaintext answer | HELD | `newGmailRun` skips the id check only when `opts.Key != nil`; `gmailanswer.Read` gives nothing for an encrypted record (no id), and a keyed Run now overrides `GmailYesStores` with the Desktop's (`jobrun.go:295`) |
| R14 | G8: the new hold deadlocks Save settings or the answer route | HELD | See G8 above |
| R15 | G7: NFC folding on Windows or Linux merges two different folders | HELD | darwin only (`gmailanswer.go:91`) |
| R16 | testhome: a lookup at package start, before `TestMain` sets the folders | HELD | No `init` function and no package-level variable in product code calls `job.Dir`, `os.UserConfigDir`, `os.UserHomeDir` or `os.UserCacheDir` (searched across `internal/` and `cmd/`) |
| R17 | testhome: a test's subprocess (a built program, a helper re-run of the test binary) finds the real folder | HELD | They inherit the process's environment, which `TestMain` set |
| R18 | testhome: `Contained` passes while `job.Dir` resolves elsewhere through a link | HELD | `filepath.Rel` of the two absolute paths as the process computes them; the folders variables are set to `root` itself |
| R19 | Round 1's A1 to A7 come back with this round's changes | HELD | Every round-1 probe passes on macOS and Windows (header) |
| R20 | The wait's stale page: Not now and Yes posted with B's id from a page drawn for B after the swap | HELD | That is B's own question; accepted as it should be (probe) |

## Probe locations (uncommitted, build tag `b3adv`)

- `internal/app/zz_b3adv_r3_test.go`: H1 (three probes), H2, H6, R1.
- `internal/desktop/zz_b3adv_r3_gmail_test.go`: H5.
- `internal/wizard/zz_b3adv_r2_tidywait_test.go`: G3 (per-id), H3.
- `internal/desktop/zz_b3adv_r2_gmail_test.go`: G5 and G2, sending the archive id.
- Rounds 1 and 2's other five files, unchanged.
