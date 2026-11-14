# Adversarial pass, round 2: Gmail de-dup slice B3's security fixes

**Verdict: NOT CLEAN (1 MED, 7 LOW).** A1 to A7 from round 1 are closed: every round-1 probe that
was red is green on macOS and on the Windows VM, and the two PLAUSIBLE ones (A6, A7) are closed in
the code. The new archive id holds against forging, stripping, collisions, older binaries and a
caller that read the answer before the folder changed. One new finding gets around it:

- **G1 (MED).** The run checks the kept yes against the id it read when it wrote its "running"
  record, which is before the Thunderbird pre-sync, and never again. If the folder comes to hold
  another archive while the pre-sync waits (the second drive of a rotation plugged in at the same
  letter, a folder renamed), the run loads that other archive and switches it with the first
  archive's yes. The run's record, and on the version-8 skip path the put-back record, then write
  the first archive's id into the other archive's folder, so the two read as one archive from then
  on. Proved on macOS (the folder renamed) and on the Windows VM (the drive letter moved to another
  folder with `subst`, as when a USB drive is pulled and another takes its letter). Windows refuses
  the rename form (the lock file is open).

The seven LOW findings: Not now on the card leaves an earlier yes waiting, which the next backup
applies (G2); the tidy-up wait is kept by folder, so a second drive's backups reset it and the 3-day
alert never comes (G3); only the scheduled run-job clears the wait, and the U2 skip is told three
ways across the run's callers (G4); a page left open binds a Yes to whatever archive the folder
holds at the click (G5); the encrypted archive's yes is archive-wide, not the stores it was asked
about (G6); the answer file's name does not fold Unicode normal forms (G7); Settle writes the job
file outside Save settings' lock (G8).

No product code was changed by this pass. The probes are kept out of the branch (build tag
`b3adv`, uncommitted), because the ones that prove findings turn the suite red.

## Header

- **Reviewed commit:** `8c9af02` on `gmail-dedup`; this round's diff `25c5bf0..8c9af02` (fixes
  `fa9a93f`, `a1f49c7`, `a53be3e`, `8c9af02`). Round 1: `docs/review-gmail-dedup-b3-adversarial.md`
  (A1 to A7). UX walk: `docs/review-gmail-dedup-b3-ux.md` (U1 to U11). Fix notes: design §13 "[B3
  adversarial and UX fixes]" and "Owner sign-off, 2026-11-13".
- **Blob ids at `8c9af02`:** `internal/state/archiveid.go` b0723e1, `internal/state/lastrun.go`
  e3cf608, `internal/gmailanswer/gmailanswer.go` 5a4f720, `internal/gmailanswer/tidywait.go`
  9b283b0, `internal/desktop/gmailquestion.go` 14ffa4c, `internal/desktop/localschedule.go`
  3317f71, `internal/app/run.go` 8639aa3, `internal/app/gmailswitch.go` 99f3985,
  `internal/wizard/wizard.go` a6e4e7e, `internal/wizard/jobrun.go` 7e86833,
  `internal/health/health.go` fb388e1, `cmd/mailarchive/gmail.go` 5e75198. A finding is stale when
  its cited file changes.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who wrote neither
  the slice nor its fixes, worked the outside, insider and integrity lenses as finder, then as
  skeptic. Every CONFIRMED row has a probe test against the real code; every HELD row cites the
  code that refutes it.
- **Worktree:** `.claude/worktrees/gmail-dedup-b3-adv2`, branch `wip/gmail-dedup-b3-adv2` at
  `8c9af02`. Probe files (build tag `b3adv`, not committed): round 1's five, brought to the
  round-2 API (`internal/app/zz_b3adv_answer_identity_test.go`,
  `internal/archdelete/zz_b3adv_gmail_answer_test.go`, `internal/gmailanswer/zz_b3adv_answer_test.go`,
  `internal/pages/zz_b3adv_member_folder_test.go`, `cmd/mailarchive/zz_b3adv_gmail_test.go`), with
  round 2's probes added to the first and third, and two new files
  `internal/wizard/zz_b3adv_r2_tidywait_test.go` and `internal/desktop/zz_b3adv_r2_gmail_test.go`.
  Tests `TestB3Adv*` (round 1) and `TestB3AdvR2*` (round 2; `TestB3AdvR2DriveLetterSwapDuringPresync`
  runs on Windows only, `TestB3AdvR2AnswerKeyUnicodeForm` on macOS only).
- **Real primitives only.** Real archives made by real backup runs of the package's
  Thunderbird-shaped Gmail profiles, the real answer file, job file, last-run record, tidy-up wait,
  delete step 3, Desktop route (through its HTTP handler with the CSRF token), scheduled run-job
  with the alert sender and notice replaced by recorders. Throw-away `HOME`, `XDG_CONFIG_HOME`,
  `APPDATA` per test. No real email, no browser, no Desktop link, no dialog; nothing outside
  `repositories/` touched.
- **Windows:** the Parallels VM "Windows 11". Nobody was signed in, so the probes ran as SYSTEM
  through this round's own copy of `winrun-at.sh` (`.claude/winrun-b3adv2.sh` in the worktree: its
  own `src-b3adv2` and `wintest-b3adv2` lab folders, its own `C:\matest-b3adv2` program folder and
  `r2probe*` program names, the share reached as `\\Mac\winlab`; only `-test.run B3Adv` runs). The
  VM was not restarted, suspended or signed into.
- **Commands and results** (Go 1.26.0 darwin/arm64; Windows binaries cross-built by the same
  toolchain for windows/amd64, 2026-11-14):
  - `go test -tags b3adv -run B3Adv -count=1 -v ./internal/app/ ./internal/archdelete/ ./internal/gmailanswer/ ./internal/pages/ ./cmd/mailarchive/`
    (macOS): every round-1 probe passes; `TestB3AdvR2SwapDuringPresyncSwitchesAnother`,
    `TestB3AdvR2TidyPutBackGivesAnotherArchiveTheId`, `TestB3AdvR2NotNowBesideAYesStillSwitches`
    and `TestB3AdvR2AnswerKeyUnicodeForm` are red.
  - `go test -tags b3adv -run B3AdvR2 -count=1 -v ./internal/wizard/ ./internal/desktop/` (macOS):
    `TestB3AdvR2RotationResetsTheTidyWait`, `TestB3AdvR2StalePageYesBindsToTheArchiveThereNow` and
    `TestB3AdvR2NotNowKeepsAWaitingYes` are red.
  - `zsh .claude/winrun-b3adv2.sh … internal/pages internal/gmailanswer internal/archdelete internal/app cmd/mailarchive internal/wizard internal/desktop`
    (Windows, as SYSTEM): every round-1 probe passes, A2's two Windows-only probes included;
    `TestB3AdvR2NotNowBesideAYesStillSwitches`, `TestB3AdvR2RotationResetsTheTidyWait`,
    `TestB3AdvR2StalePageYesBindsToTheArchiveThereNow` and `TestB3AdvR2NotNowKeepsAWaitingYes` are
    red as on macOS; `TestB3AdvR2AnswerKeyUnicodeForm` skips (macOS only); the two rename probes
    stop at "Access is denied" (Windows will not rename a folder whose lock file is open).
  - `zsh .claude/winrun-b3adv2.sh … internal/app` again with `TestB3AdvR2DriveLetterSwapDuringPresync`
    (Windows only: `subst Q:` names drive 1's folder, and is moved to drive 2's inside the pre-sync
    hook): red, the run ends ok, drive 2's archive is version 7 with `imap.gmail.com` on and carries
    A's id. The probe removes its `subst` mapping (checked afterwards: none left).
- **Count:** 31 attacks; 8 confirmed or plausible (1 MED, 7 LOW); 23 held or refuted.

## Round-1 findings: verdicts

| ID | Round 1 | Round 2 verdict | Evidence |
|---|---|---|---|
| A1 | HIGH, CONFIRMED | **CLOSED** (with G1 as a new, narrower way around the binding) | `TestB3AdvAYesKeptForOneArchiveSwitchesAnother` (round-2 API: Keep with A's id, B moved into A's folder, Read the way `optsForJob` reads it) passes on macOS and Windows: B stays version 6. `TestB3AdvAnswerHasNoArchiveIdentity` passes: another id, no id, a non-hex, a 33-character and an upper-case id all read no answer. `TestB3AdvDeleteStep3KeepsTheGmailAnswer` passes: step 3 removes `.gmail-answer`. Settle spends the yes (product test MA-1302) |
| A2 | LOW, CONFIRMED (Windows) | **CLOSED** | `validMemberFolder` and the page check now require `util.SanitizeSegment(s) == s`; `TestB3AdvTrailingDotMemberFolderAliasesAnotherPage` and `TestB3AdvDotDotSpaceMemberFolder` pass on Windows (round 1: red there) and on macOS |
| A3 | LOW, CONFIRMED | **CLOSED** for letter case and links; residual G7 (Unicode normal form, re-ask only) | `TestB3AdvAnswerKeyFollowsSpellingNotFolder` passes on macOS and Windows |
| A4 | LOW, CONFIRMED | **CLOSED** | `TestB3AdvEndOfInputIsARememberedNotNow` passes (end of input is "No answer", nothing kept; its positive twin, a typed Enter, is kept). `TestB3AdvInterruptDoesNotEndTheWait` now cancels the context after 100 ms and the question ends at once, unanswered. The Windows console's Ctrl-C stays unproved (an interactive console cannot be driven through `prlctl`); both ways it can arrive end the question as no answer (`askGmailAnswerCtx`) |
| A5 | LOW, CONFIRMED | **CLOSED** | `TestB3AdvStatusWaitingLinePrintsControls` passes for U+009B, U+202E, U+0085 and U+2066 in `status`'s text lines, `status -json`'s reasons and the terminal summary (`health.shownStore`, `gmailStoreShown`); its positive twin checks the Gmail reason is there |
| A6 | LOW, PLAUSIBLE | **CLOSED** for the Desktop route; a new writer outside the lock is G8 | `gmailquestion.go:316` takes `holdSettings`, `:324` `unfinishedActionRefusal`, `:330` `jobFileMu`, which `scheduleInstallLocal` (`localschedule.go:153`) also takes; uninstall removes `TempPattern` (MA-1307). No deadlock: `holdSettings` never waits (`deleting.go:65-94`), and only these two take `jobFileMu`, both first |
| A7 | LOW, PLAUSIBLE | **CLOSED** | `installOffered` calls `gmailanswer.CarryInto` before `job.Write` (`wizard.go`); MA-1308 |

## New findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| G1 | integrity / insider (time) | `run.go:757-758` snapshot and "running" record (with `ArchiveIDOrNew`, `run.go:1321`) are taken before the pre-sync (`:769-791`); the manifest loads at `:856`; `newGmailRun` (`gmailswitch.go:64-73`) compares `GmailYesFor` with `begun.Archive`, never with the archive loaded; the lock is first re-checked at a checkpoint (`run.go:1000`), and a small store is committed after its file with no check (`:1062`); `recordRun` and the U2 put-back (`:800-808`) write to the path whatever it now holds | CONFIRMED, MED (macOS, Windows) | `TestB3AdvR2SwapDuringPresyncSwitchesAnother` (macOS, folder renamed) and `TestB3AdvR2DriveLetterSwapDuringPresync` (Windows, drive letter moved with `subst`): the run ends ok, B is version 7 with `imap.gmail.com` on, and B's record carries A's id. `TestB3AdvR2TidyPutBackGivesAnotherArchiveTheId`: the version-8 skip writes A's snapshotted record into B's folder; once B's tidy-up ends, B's next scheduled run reads A's yes for B and switches it | `15513d7` | MA-1316, MA-1317: `TestAYesIsNotAppliedWhenTheFolderChangesDuringThePresync`, `TestAYesIsNotAppliedWhenTheDriveLetterChangesDuringThePresync` (Windows), `TestATidyUpSkipNeverPutsOneArchivesRecordIntoAnother` |
| G2 | integrity | `gmailquestion.go:345-367`: Not now sets `NotNow` and keeps `kept.Yes`; `gmailswitch.go:109` applies `yesFor` before the Not now is consulted | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR2NotNowKeepsAWaitingYes` (Desktop route): after Yes for one store and Not now when the question comes back for a second, the yes for the first is still kept, and the note says "Nothing changed". `TestB3AdvR2NotNowBesideAYesStillSwitches`: the next scheduled run switches the archive | `59b91ca` | MA-1318: `TestNotNowTakesBackAWaitingYes`, `TestNotNowOnTheCardTakesBackAWaitingYes` |
| G3 | integrity / availability | `tidywait.go:45-55` keys the wait by the folder (`folderKey`), not the archive id; `wizard.go:357` clears it on any run-job that is not a tidy-up skip | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR2RotationResetsTheTidyWait`: two drives in turn at one path, drive 1 stuck at version 8; each drive-2 backup clears the wait; after 8 days of drive 1 not being backed up, no email and no notice were sent | `15513d7` | MA-1319: `TestTheTidyUpWaitIsKeptForTheArchiveNotTheFolder` |
| G4 | integrity (words) | `gmailanswer.ClearTidyWait` is called only by `RunHeadless` (`wizard.go:357`); the terminal backup (`main.go:441-446`), the Desktop's Run now (`jobrun.go:99-100`) and the Microsoft 365 capture (`capture.go:159`) never clear it; `health.Gather` (`health.go:2052`) shows any kept wait's date | PLAUSIBLE, LOW | Mechanism in source. Also: the U2 put-back is in `app.Run`, so it applies to every caller, but only run-job keeps a wait and alerts: a `mailarchive schedule`-installed backup (`export -unattended`, `cmd/mailarchive/job.go:282`) is now WARN with no date and no alert where it was RED; the Microsoft 365 capture (`graph.go:219-231`, no put-back) still records a failure and sends the "waiting" email on its first run | `15513d7` | MA-1320, MA-1321, MA-1322: `TestABackupPastTheManifestEndsTheTidyUpWaitWhateverStartedIt`, `TestAScheduledExportMeetingATidyUpIsASkip`, `TestTheScheduledCaptureMeetingATidyUpIsASkip` |
| G5 | integrity (stale page) | `gmailquestion.go:303`, `:343-349`: the request carries only the path; the answer is bound to the record the folder holds when the button is clicked | CONFIRMED, LOW (macOS, Windows) | `TestB3AdvR2StalePageYesBindsToTheArchiveThereNow`: A's card drawn, the folder then holds B (same store off), Yes is kept for B's id | `59b91ca` | MA-1323: `TestAStalePagesAnswerIsRefused` |
| G6 | integrity | `runnow_keyed.go:213-214` and `jobrun.go:278` hand the encrypted Run now `opts.GmailYes`, a yes for the whole run; `gmailswitch.go:109` switches every Gmail store the run reads, not only `keyEntry.gmailOff` | PLAUSIBLE, LOW | Mechanism in source: a Gmail account added to Thunderbird between the Run now that asked and the one that applies is switched too. The plaintext card's yes names its stores (`GmailYesStores`) | `59b91ca` | MA-1324: `TestAnEncryptedArchivesYesCoversOnlyTheStoresItNamed`, `TestTheDesktopsEncryptedYesNamesItsStores`, the amended `TestAnEncryptedRunNowCarriesTheDesktopsYes` |
| G7 | integrity | `gmailanswer.go:80-90` folds letter case but not the Unicode normal form; `util.ResolveExisting` keeps the spelling given | CONFIRMED, LOW (macOS; re-ask only) | `TestB3AdvR2AnswerKeyUnicodeForm`: a Not now kept for `Archivé` (NFC) is not found for the same folder spelled in NFD | `59b91ca` | MA-1325: `TestTheAnswerKeyFoldsTheUnicodeFormOnMacOS` |
| G8 | integrity / concurrency | `gmailanswer.Settle` (`gmailanswer.go:234-272`) reads and rewrites the job file; the Desktop's Run now calls it in the Desktop's own process (`jobrun.go:100`) without `jobFileMu`, as run-job does in its own | PLAUSIBLE, LOW | Mechanism in source (a race, not reproduced) | `59b91ca` | MA-1326: `TestTheRunNowSpendsAYesUnderTheJobFileLock`, `TestTheRunNowsSettleTakesTheJobFileLock` |

### G1 (MED): the archive changes while the pre-sync waits, and the first archive's yes switches it

**What happens.** A run takes the archive's lock (a file inside the folder), keeps the last-run
record as it is, and writes its "running" record, which carries the archive's id. It then runs the
Thunderbird pre-sync, which waits for Thunderbird to finish downloading (about ten minutes in the
field, design note on wait-for-quiet). Only then does it load the manifest. The yes check compares
the id the yes was given for with the id read at the start. Nothing compares it with the archive
it loaded. The lock does not pin the folder: when the folder is renamed or the drive is swapped,
the lock file goes with the old folder, and the run is first told at a checkpoint, which a small
store never reaches before its manifest is saved.

The probe follows the rotation the round-1 A1 named: two drives backed up in turn, each holding its
own archive of the same Thunderbird profile, at one path. Archive A (drive 1) has a yes kept. While
A's scheduled run waits in its pre-sync, drive 1 is replaced by drive 2 (archive B, its own id,
never answered). Result on macOS: the run ends ok, B is version 7 with `imap.gmail.com` switched on,
and B's last-run record now carries A's id, so from then on the two drives read as one archive and
any answer kept for one is applied to the other.

The same window on the U2 skip path: if B is at version 8, the run's put-back writes A's
snapshotted record into B's folder. Once B's tidy-up finishes, B's next scheduled run reads A's
kept yes as B's and switches it (`TestB3AdvR2TidyPutBackGivesAnotherArchiveTheId`).

**Why MED and not HIGH.** It needs the folder's archive to change during one run's pre-sync (opt-in,
minutes long) or the sweeps (seconds), with a yes waiting. On Windows a folder holding the open lock
file cannot be renamed; the drive swap at one letter is the Windows form (proved with `subst`), and
Windows lets a USB drive be pulled during a run. The harm is the one this slice exists to prevent (an irreversible
switch nobody said yes for) plus a lasting merge of two archives' ids.

**Fix, in plain words.**

1. Check the yes against the archive the run loaded: read the folder's record id again right after
   the manifest loads (under the lock), and use the yes only when it equals both `GmailYesFor` and
   the id the run began with.
2. Re-check the lock (`lock.StillHeld`) right after the manifest load, before any switch, and
   before `recordRun` or the U2 put-back writes the record; when it is lost, write nothing.
3. Optionally take the snapshot and the "running" record only once the manifest has loaded, so a
   pre-sync's wait is not inside the window.

### G2 (LOW): Not now leaves an earlier yes waiting

Yes for store S1 is kept. A later backup that did not read S1 (its account not reachable that run,
or another computer's backup) finds a second store S2 off, so the card asks again. The person clicks
Not now. The route sets Not now and keeps the yes for S1; the note says "Nothing changed, and
MailArchive will not ask again". The next backup switches S1, which makes the archive 1.8-only, the
step the person's latest answer declined. The encrypted route does take its yes back on Not now
(`gmailquestion.go:140`).

**Fix:** Not now clears the kept yes (or the question for S2 says a yes for S1 still waits and
offers to take it back).

### G3 (LOW): a second drive's backups reset the tidy-up wait

The wait is kept by folder. With two drives used in turn at one path (daily, or any turn shorter
than 3 days), drive 2's backups get past the manifest and clear the wait drive 1 began, so drive 1's
3-day alert never comes, however long it has not been backed up. Its `status` says WARN with a
recent "since" date; RED never comes (U2). A plain failure on drive 1 still alerts every time (its
own failure streak starts again after drive 2's recovery), so only the tidy-up wait is silenced.

**Fix:** bind the wait to the archive id the version-8 archive's last-run record carries (it is
readable, since the record is put back as it was), and clear it only when that archive gets past
the manifest.

### G4 (LOW): the tidy-up wait is cleared by one caller, and the skip is told three ways

The design note says "Any run that gets past the load removes the file"; only run-job does. A wait
left behind by a tidy-up that ended while backups ran from the terminal or Run now stays, so the
next tidy-up of that folder is told "waited since" the old date and its alert is due on its first
skip. Separately, the skip's posture differs by caller: run-job skips quietly and alerts after 3
days; a schedule installed by `mailarchive schedule` runs `export -unattended`, which now leaves the
old record (WARN, no date, no alert, exit 1) where it was RED; the Microsoft 365 capture, which has
no put-back, records a failure (RED) and sends the "waiting" email on day 1.

**Fix:** clear the wait in `app.Run` (or in each caller) once the manifest loads; give the
`export -unattended` and capture paths the same skip rule, or say in the design that they differ.

### G5 (LOW): a page left open binds Yes to whatever archive the folder holds at the click

The card's Yes sends only the folder's path. A Desktop page drawn while drive 1 was plugged in,
clicked after drive 2 took the same letter, keeps the yes for drive 2's archive, which was never
shown. This is close to the design's conceded case (two archives sharing one folder's answer), but
here the yes lands on an archive the person never saw asked about.

**Fix:** put the archive id on the page with the question and send it with the answer; refuse
("There is no Gmail question for this archive now. Reload the page.") when it differs.

### G6 (LOW): the encrypted archive's yes covers stores it was not asked about

The encrypted Run now carries the Desktop's in-memory yes as `opts.GmailYes`, the terminal-style yes
for the whole run. A Gmail account added to Thunderbird after the Run now that asked is switched by
the Run now that applies the yes. The plaintext card's yes names its stores. The key binding itself
held (see H10 below).

**Fix:** carry the stores (`keyEntry.gmailOff` at the answer) as `GmailYesStores` with
`GmailYesFor` set to the entry's id check, or say in the design that an encrypted yes is
archive-wide.

### G7 (LOW): the answer file's name does not fold the Unicode normal form

macOS file systems treat `Archivé` typed in NFC (a terminal) and NFD (some dialogs) as one name;
`folderKey` does not. A Not now kept under one spelling is not found under the other, so the person
is asked again. Never a wrong apply (the id binds). Windows does not normalise, so it is macOS only.

**Fix:** normalise to NFC in `folderKey` on darwin, or key by the folder's file identity.

### G8 (LOW): Settle writes the job file outside Save settings' lock

`Settle` reads the job file and writes it back without its yes. The Desktop's Run now calls it in
the Desktop's own process, without `jobFileMu`; a Save settings that reads, then writes the same
file meanwhile can lose its change (the job file keeps the old schedule or sync setting while the
scheduler entry has the new one), or Settle's change is lost (harmless: `Read` leaves spent stores
out). Run-job is another process, as the A6 note concedes for the wizard.

**Fix:** take `jobFileMu` around Settle in the Desktop's Run now (a seam the Desktop passes in), as
the answer route does.

## Held and refuted attempts

| ID | Attempt | Verdict | Evidence |
|---|---|---|---|
| H1 | Forge or copy an archive id into the record so a yes applies to another archive | HELD (no new power) | The answer is keyed by this computer's folder key and bound to the id; to make B carry A's id, someone must write B's record on the archive drive. Whoever can write the drive can stamp a version-7 manifest with Gmail switched on directly. G1 is the one way the program itself copies an id |
| H2 | Strip the id, or an older MailArchive drops it | HELD | `ReadLastRun` turns a missing or malformed id into none (`lastrun.go:411-413`); `Read` and `gmailStoresAt` then return nothing; the next 1.8 run gives a new id (`ArchiveIDOrNew`), so the kept answer is asked again, never applied |
| H3 | Malformed ids (upper-case hex, 31 or 33 characters, a space) | HELD | `ValidArchiveID` (`archiveid.go:21-31`) is exact; probe `TestB3AdvAnswerHasNoArchiveIdentity` |
| H4 | Collisions | HELD | 16 bytes from `crypto/rand`; no randomness gives no id, so nothing applies (`archiveid.go:34-40`) |
| H5 | A cloned or restored copy | HELD (conceded by design) | Same id, same archive (design §13 A1 note). G1 is a way the program makes two different archives share an id without a copy |
| H6 | A writer of a later record drops the id | HELD | `WriteLastRun` carries the on-disk id when the record has none (`lastrun.go:258-260`) |
| H7 | A caller that read the answer before the folder changed | HELD | `newGmailRun` checks `GmailYesFor` against the id under the lock (MA-1302; round-1 probe green). G1 is the window after that check |
| H8 | Planted `gmail_on` or `gmail_off` in the archive's record | HELD | `gmail_on` only spends a yes (Settle, `spent`); `gmail_off` only asks; a token is switched only for a store the run detects as Gmail (round-1 H9) |
| H9 | Forge `<key>.tidy-wait` to delay a real failure alert or fake a 3-day alert | HELD (out of scope) | It lives in the owner-only settings folder (round-1 H8). Planted 1970 is due at once; 2999 is refused (`tidywait.go:69`); a file of version 99 is read as version 1 (noted, harmless). A tidy-up wait never covers a failure: only a run refused at the manifest load with exactly the tidy-up words (`TidyingElsewhere`) is skipped |
| H10 | F18: Lock forgets the encrypted answer; another request applies it to a different encrypted archive | HELD | The answer lives on the open entry (`keys.go:153-159`); every lock deletes the entry (`keys.go:448-452`, `closeWhere`), and a re-open makes a new one (`keys.go:352`). A request for another folder reads that folder's entry; two spellings that fold together share one entry, and the Run now opens the folder with that entry's key, which opens only the archive it was made for |
| H11 | F18: the encrypted answer leaks into a file or the plain log | HELD | `gmailAnswerEncrypted` and `keepGmailOutcome` write memory only; `newGmailRun`'s and `switchOn`'s log lines are plaintext-only (`gmailswitch.go:70`, `:98`) |
| H12 | A6: deadlock between the answer route and Save settings | HELD | `holdSettings` never waits; `jobFileMu` is taken by these two only, each after its holds; neither calls the other |
| H13 | Symlink games on the answer file's name | HELD | `folderKey` resolves links; a link retargeted between answer and run gives another key (no answer) or the same folder (same id). The file itself is read without following links (round-1 H5) |
| H14 | Case-sensitive volume: two folders fold to one key | HELD (conceded) | One folder's answer replaces the other's (asked again); the id keeps one archive's yes from the other. The same folding shares a tidy-up wait (G3's mechanism) |
| H15 | Scrubbed waiting lines: other controls (NEL, isolates) | HELD | U+0085 and U+2066 are removed too; the Desktop view escapes HTML |
| H16 | A crafted version-8 manifest disguises a real failure | HELD, with G3 and G4 as residuals | Only a well-formed manifest at 8, or carrying `merge_pending`, is a skip (`manifest.go:849-855`); a damaged one is RED. The skip's alert after 3 days fires on run-job (MA-1310). By the lead's U2 call `status` is WARN, never RED; "Nothing is lost" rests on the stamp alone (as round-1 H16 noted). Whoever can write the manifest can also delete the archive |
| H17 | The put-back removes a damaged or linked record | HELD | A record that cannot be read as a regular file is removed by the put-back (`lastrun.go:607-618`); `os.Remove` removes a link, not its target; the record was unreadable before |
| H18 | Settle removes a yes for another archive, or a Not now | HELD | It acts only when the last ok record's id equals the answer's, and only on stores that record lists as on |
| H19 | `Keep` with no id | HELD | `ErrNoArchiveID` (`gmailanswer.go:179-181`, `:199-201`); the card asks only when the record has an id (`gmailStoresAt`, `:189`) |
| H20 | The terminal's Not now kept for another archive | HELD | `KeepHere` takes `state.ArchiveID(out)` while the run holds the lock, after its "running" record (G1's window aside) |
| H21 | The 1.8 lines on the encrypted card name accounts | HELD | `encryptedGmailCard` returns the fixed question and lines only |
| H22 | U2 skip on an encrypted archive | HELD | The put-back is plaintext-only (`run.go:801`); an encrypted archive cannot be tidied |
| H23 | Ctrl-C at the terminal question leaves a kept answer | HELD | `ctx.Done()` returns unanswered (`gmail.go`); nothing is kept |
