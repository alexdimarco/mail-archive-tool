*Filed by the lead, 2026-11-21 (clock 02:38 EST). The report below is the reviewer's, word for word:
an independent Claude agent that wrote no part of 1.7.5 and took part in none of its reviews,
re-verifying the 4 commits 22dc749..0a450d2 (round 4, the closing check). The lead's fold comes
first.*

## The lead's fold

**Nothing MEDIUM or above is left, so the review rounds close.** The adversarial pass, the friction
review and their re-verifies (rounds 1 to 4) are done. The items below are built, and the lead
reviews that small diff instead of running a fifth round.

| ID | Severity | Decision |
|---|---|---|
| RV4-01 | LOW | Fix: `recordStartFailure` carries the previous plaintext record's names (`state.TBNamesOf`). The reviewer's desktop red test is the tombstone. MA-1481's "never skipped quietly after one stop" then holds |
| RV4-02 | INFO | Fix (narrows toward Q4): before the server-name rule, skip when a live account's folder setting still names the gone folder |
| RF4-01, RF4-02, RF4-03 | info, low | Build the reviewer's words (RF4-03's README line included) |

---

**Verdict: nothing MEDIUM or above is left.** One LOW (RV4-01), one INFO (RV4-02) and three word findings remain. Only LOW and INFO are left, so the lead can close the review rounds.

# MailArchive 1.7.5: re-verify, round 4 (commit 0a450d2)

## Header

- **Date (clock):** 2026-11-21, 02:24 to 02:36 EST.
- **Reviewed:** worktree `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv4`, detached at 0a450d2. Scope: the 4 commits in `22dc749..0a450d2`.
- **Reviewer:** a fresh Claude session (Opus 5.5). It wrote no part of 1.7.5 and took part in none of its reviews.
- **Memory:** I ran `memory_pressure -Q` 6 times. Each showed 61 to 64% free. I ran one `go test` at a time, each with `-p 2 -count=1` and a `-run` filter.
- **Commands and exit status** (WT is the worktree):
  - `date`, twice: 0.
  - `git -C WT log --stat 22dc749..0a450d2`: 0.
  - `git -C WT status --short`, twice: 0. Clean at the start. At the end it holds only my two test files.
  - `git -C WT show` on c7935df (twice), 9286bf0 and aadc4b8 (twice): 0.
  - About 45 `git grep` searches, one of them at v1.7.4:
    - The ones that found something exited 0. Five found nothing.
    - Five failed on my own pattern mistakes and were rerun in a working form: two zsh quoting errors, one stray `-A` flag (exit 128), and two over git's 255-repeat limit (exit 128).
  - Tests:
    1. app (`-run "TestZZRV4175_|TestAMovedAccountStopsEveryRunNotOnlyTheFirst|TestEveryPlaintextRecordCarriesTheFolderNames|TestAMovedAndRenamedAccountStops|TestTheRecordedNameDecidesAMoveAmongAccountsOnOneServer" -v`): exit 1. My 3 tests failed as intended. The 4 committed tests passed.
    2. desktop (`-run "TestZZRV4175_|TestTheRebuildAsksWithTheRecordsNames|TestTheRebuildKeepsAGoneMailboxTheRunWouldSkip|TestHowToUseMailArchiveExplainsDelete" -v`): exit 1. My 1 test failed as intended. The 3 committed tests passed.
    3. cmd/mailarchive (`-run TestPrepareAsksWithTheRecordsNames -v`): exit 0.
    4. state (`-run TestTheCarriedNamesReadBackCleanAndBounded -v`): exit 0.
  - I ran no mutations. Every test ran in temp folders with HOME pointed at a temp folder. Nothing could show a dialog, notification or browser.
- **Read:**
  - the process file; the round-3 report and the lead's fold;
  - the design: §0 (Q1 to Q10), and every *Revision 4.3* line (§1.2, §1.4, §1.5, §3.1, §5, §12);
  - the catalog amendments to MA-1481 and MA-1473;
  - all 4 commit messages and their diffs;
  - `run.go`: `Run` up to the running record, `newRunRecord`, `RecordRefusedRun`, `writeRefusedRun`, `recordRun`, `RecordedNamesAt`, `DiscoverInputsFor`;
  - `sourceloss.go`; app `tbcompleteness.go` (the assessment, `tbRecords`, `RecordRunWarning`);
  - `autoplace.go`; `thunderbird.go` (`movedHow`);
  - state: `lastrun.go`, `encrypted_record.go`, `runhistory.go`, `tbcompleteness.go`;
  - desktop: `capture.go`, `localschedule.go`, `launch.go`, `keys.go`, `settings.go` (`effectiveOut`), `dashboard.go:1640-1670`;
  - `wizard.go:230-360`; `main.go:220-445` and `prepareThunderbird`; `status.go`;
  - the committed MA-1481 tests, the rebuild test, the lock-ends-plan tests, and their helpers.
- **Could not check:**
  - **Windows:** nothing ran there.
  - **RV4-01 from end to end.** I did not run a real Microsoft 365 plus Thunderbird archive. I showed the record it writes, then planted that record between real Thunderbird runs.
  - Whether a leftover Microsoft 365 capture can write into a Thunderbird-only archive. Its folder comes from the Desktop's current archive setting (`settings.go:211-216`). I read this; I did not run it.
  - **RF4-01 on a real encrypted run.** I showed it at the rule the run asks, with an encrypted record.
  - **Thunderbird itself** (recalled, not checked): renaming an account, and how long a listed account's folder can stay missing.
  - **1.7.4 reading the new field.** I checked this by `git grep` at v1.7.4: nothing reads the record strictly. I did not run 1.7.4.
  - I did not repeat the builder's mutations (R1 to R5, S1, Q1, Q2, W1, W2).
- **Left in the worktree** (untracked, never committed, no covers marker):
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv4/internal/app/zz_rv4_175_app_test.go`
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv4/internal/desktop/zz_rv4_175_desktop_test.go`

## Fixed items

| Item | Fixed? | Evidence |
|---|---|---|
| RV3-01: a moved account stops every run | Yes, with one gap (RV4-01) | `TestAMovedAccountStopsEveryRunNotOnlyTheFirst` passes: 3 setups, 3 runs each, Run now included. `newRunRecord` carries the names into every plaintext record (`run.go:1455-1459`). Every refusal writer goes through it: `main.go:234,300,1724`, `wizard.go:272`, `launch.go:257`, `capture.go:103`, `graph.go`. Plaintext Run now runs unattended (`jobrun.go:373`), so its refusals carry the names too. |
| RV3-01: a removed account still skips | Yes | The positive twin passes: skipped on two runs, named both times. When no live account has the recorded name, the server-name rule now decides. It skips, except in the traded case. |
| RV3-01: record kinds | All but one | These carry the names: running (so a killed run's), refused, failed, cancelled, and ok with no entries. `RecordRunWarning` rewrites the whole record (`tbcompleteness.go:701-718`). Encrypted records keep none (`run.go:1427-1443`, `encrypted_record.go:146-178`). **Lost:** a Microsoft 365 capture refused at start (RV4-01). **Older 1.7.x:** an ok record's names still match, because the store id is the same. A 1.7.4 failed record has no names, so the first 1.7.5 run uses the server-name rule, as round 3 said. |
| RV3-01: leaks | None found | Log lines say the path only (`run.go:680`). The attention notice uses the error text only (`lastrun.go:320-357`). Alert emails never read names. `status -json` builds its own struct (`status.go:91-93`). The encrypted writer drops the field. The history entry is built field by field and leaves it out (`runhistory.go:140-155`). |
| RV3-01: stale results | None found | Only two functions read the new list: `TBNameOf` and `TBNamesOf`. They are used only to decide a move and to name a skipped mailbox. Status, the wizard, the Desktop, restore and history read only `thunderbird[]`, which a failed record still lacks. |
| RV3-02 | Yes | `TestAMovedAndRenamedAccountStops` passes. The traded case is a little wider than its words: see RV4-02 and RF4-03. |
| RV3-03 | Yes | `TestPrepareAsksWithTheRecordsNames` and `TestTheRebuildAsksWithTheRecordsNames` pass. The prep and the rebuild read the same record as the run (`main.go:280,434,2107`; `localschedule.go:101-109`). The Repair helper already did. `wizard.go:789` lists no inputs, so it cannot disagree. |
| RV3-04 words | True, but incomplete | See RF4-01. |
| RF3-02 words | True, but incomplete | See RF4-02. |
| RF3-03 words | Yes, true and plain | `autoplace.go:271-275`, pinned in the committed test. Only RV4-02's rename case fits neither of the words' two "If…" cases. |

## New findings

### RV4-01: LOW. A Microsoft 365 capture refused at start drops the names

- **Where:** `internal/desktop/capture.go:245-249` (`recordStartFailure`).
  - It writes a bare failed record: only the status, the start time and the error.
  - It runs when Microsoft 365 is not set up (`capture.go:124-125`, `m365Ready` at `:36-42`).
- **Sequence:**
  1. One archive folder is backed up by both a Microsoft 365 capture and a Thunderbird "All of them" backup (a mixed archive, `dashboard.go:1653`). It is one of RV3-01's setups.
  2. Microsoft 365's setup is gone, but its capture schedule still runs.
  3. The person moves a Thunderbird account.
  4. The next Thunderbird run stops, naming the right reason. Its record keeps the names.
  5. The capture runs, is refused at start, and writes a record with no names.
  6. The next Thunderbird run has no names. The server-name rule skips the moved account quietly, and every later run does too.
- **Red tests:**
  - `TestZZRV4175_CaptureStartFailureKeepsTheNames`: "recordStartFailure wrote a record without the folders' names: []".
  - `TestZZRV4175_ABareFailedRecordBetweenRunsKeepsTheStop`: the first run stopped (positive twin). After the same bare record: "the moved account was skipped quietly: err=<nil> skipped=[{… Name: … Listed:false …}]".
- **Why only LOW:** it needs a mixed archive, a missing Microsoft 365 setup whose schedule still runs, and RV3-01's rare setups.
- **Until fixed, a word is off:** the MA-1481 amendment says "never skipped quietly after one stop". That is not yet true.
- **Fix:**
  - In `recordStartFailure`, read the record it replaces. When it is plaintext, set `ThunderbirdNames` to `state.TBNamesOf(prev)`. The desktop red test is the tombstone.
  - Or rule it a known limit.

### RV4-02: INFO. An account renamed while its folder is missing now stops the backup

- **Where:** `thunderbird.go:688-694`. With no live account of the recorded name, the server-name rule now decides (`:721-746`).
- **Sequence:**
  1. Three accounts are on one mail server. A and B are in the archive. C keeps its mail off the profile.
  2. B's folder goes missing while Thunderbird still lists B and B's folder setting still names that folder. This is the "deleted by hand" case Q4 says to skip.
  3. B is renamed in Thunderbird.
  4. The next run: the recorded name matches no live account. The server-name rule finds C as the only account outside the profile, so the backup stops.
- **Red test:** `TestZZRV4175_RenamedFolderMissingBesideOneOffProfile`.
  - Positive twin (not renamed): skipped.
  - Renamed: "stopped the backup: … a Thunderbird account on the same mail server, or Local Folders, now keeps its mail in …/QZGmail3 …".
- **Effect:**
  - It fails closed: the stop is loud.
  - The words fit neither of their two cases (B was not moved, and not removed).
  - It heals once Thunderbird downloads the folder again.
  - Before RV3-02, this case was skipped.
- **Fix:**
  - At least, write it beside the traded case (RF4-03).
  - Option: before the server-name rule, skip when a live account's folder setting still names the gone folder. The one-match branch already does this (`thunderbird.go:700-703`). Two accounts cannot share one folder, so this narrows only toward Q4.

## Word findings

**RF4-01 (info). RV3-04's known limit leaves out one case.**
- **Where:** `README.md:124-127`, design §5, and the MA-1481 amendment.
- **Why:** with one account per mail server, an encrypted archive also skips a moved account whose server name was changed after it was set up. My red test `TestZZRV4175_EncryptedOneAccountPerServerServerNameChanged` shows it.
- **Proposed:** "An encrypted archive keeps no account names, so for it MailArchive goes by the mail server's name alone: a moved account can be skipped instead of stopping the backup when several accounts use one mail server, or when the account's server name was changed after it was set up. Check status after moving one."

**RF4-02 (low). RF3-02's words do not say an archive locks by itself.**
- **Where:** `internal/desktop/help.go:259`, `README.md:367-369`, and MA-1004's pin.
- **Why:**
  - Archives also lock after 30 minutes of no one using the Desktop, and when you switch archives (`keys.go:20-25`). Both end such a plan.
  - "Locking any archive" reads as something the person does.
  - The owner's complaint was a plan that ends too early.
- **Proposed:** "A plan waits up to a day, but a backup in the meantime makes MailArchive check again. For an encrypted archive that was unlocked when it was checked, the plan also ends when any archive locks: when you click Lock, when you switch archives, or by itself after 30 minutes of no one using MailArchive Desktop."

**RF4-03 (info). The traded case's words are narrower than the code.**
- **Where:** the MA-1481 amendment, design §1.2 *Revision 4.3*, and the comment at `thunderbird.go:690-694`.
- **Why:**
  - Accounts on the same server kept in the profile do not prevent the stop. The committed test itself has one.
  - "Beside exactly one other account" can be read as "only one other account".
  - RV4-02 is a second form of the same case.
- **Proposed:** "The traded case it brings back: an account removed in Thunderbird, or renamed there while its folder is missing, stops the backup (the same-server words) instead of being skipped, when exactly one of the other accounts on its mail server keeps all its mail outside the profile; accounts on that server kept in the profile do not change this."
- **Optional README line:** "Rarely, a removed account stops it too: when exactly one other account on its mail server keeps its mail outside the Thunderbird profile. The message says how to go on."

Working under the Limen method.
- **Clock read:** 2026-11-21, 02:24 to 02:36 EST.
- **Register:** a finding report. The verdict and severities are my own.
- **Sources:** the worktree's code, tests, design and catalog, and the round-3 report, all reached, plus the four test runs above.
- **Not checked, per claim:**
  - RV4-01 from end to end with a real Microsoft 365 archive: shown at the function, and by planting its record.
  - RF4-01 on a real encrypted run: shown at the rule.
  - Windows: not run.
  - Thunderbird's own rename behaviour: recalled.
- **Borrowed lines:** test output and product words, quoted as printed.
- **Drift:** none noticed.
- **Touched:** two untracked test files. No code changed. No mutations.
- **Persona:** none.
