*Filed by the lead, 2026-11-21 (clock 01:32 EST). The report below is the reviewer's, word for word:
an independent Claude agent that wrote no part of 1.7.5 and took part in none of its reviews,
re-verifying the 4 commits 8fc15fe..78c360d (round 3). The lead's fold comes first.*

## The lead's fold

| ID | Severity | Decision |
|---|---|---|
| RV3-01 | MEDIUM | Fix: every record a plaintext run writes (running, refused, failed, cancelled, ok) keeps each Thunderbird folder's store id and account name, carried from the previous record when the run has none of its own; `RecordedName` reads that list. MA-1288 holds (names stay in the record, never in a log). Tombstone: a moved account stops two scheduled runs in a row, and Run now after it |
| RV3-02 | LOW | Fix, the reviewer's choice (2), in the owner's Q7 direction (a loud stop beats a quiet loss): when no live account has the recorded name, the server-name rule is asked. The traded case it brings back is written in MA-1481 |
| RV3-03 | LOW | Fix: `prepareThunderbird` and the Desktop's rebuild ask with the record's names |
| RV3-04 | INFO | Known limit, written in MA-1481, design §5 and the release notes: an encrypted archive's record keeps no names (the owner's Q3, Q5, Q6), so the server-name rule always decides for it |
| RV3-05 | INFO | No change (fails closed once, then heals) |
| RF3-01 | low | Superseded by the RV3-01 fix; the README gains RV3-04's encrypted limit instead |
| RF3-02, RF3-03 | info | Build the reviewer's words |

The review rounds close after this fix if the next check of it finds nothing MEDIUM or above.

---

**Verdict: not ready as is. One MEDIUM is left (RV3-01).** The new rule decides right on the first run. But after one stopped scheduled backup, the next one skips a moved account quietly in some setups. RV3-02 and RV3-03 are LOW. Everything else is INFO or fixed.

# MailArchive 1.7.5: re-verify, round 3 (commit 78c360d)

## Header

- **Date (clock):** 2026-11-21, 01:20 to 01:30 EST.
- **Reviewed:** worktree `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv3`, detached at 78c360d. Scope: the 4 commits in `8fc15fe..78c360d`.
- **Reviewer:** a fresh Claude session (Opus 5.5). It wrote no part of 1.7.5 and took part in none of its reviews.
- **Memory:** I ran `memory_pressure -Q` 4 times, once before each `go test`. It showed 63 to 64% free. I ran one test at a time, each with `-p 2 -count=1` and a `-run` filter.
- **Commands and exit status** (WT is the worktree):
  - `date`, twice: 0.
  - `git -C WT log --stat 8fc15fe..78c360d`: 0.
  - `git -C WT status --short`, twice: 0. It was clean at the start; at the end it holds only my test file.
  - `git -C WT show`, 4 times (45e12aa twice, d50d67a, 8c8e915): 0.
  - `git -C WT diff 8fc15fe 78c360d -- docs/scenario-catalog.md`: 0. The output was too long to show, so I read the amendments with `grep -o`.
  - 11 `git grep` searches and 4 plain `grep` searches: all 0.
  - Tests:
    1. `go -C WT test … -run "TestZZRV3175_" -v ./internal/app/`: exit 1. All 4 failed as intended (RV3-01 a, b, c and RV3-02).
    2. app, the committed tests (`-run "TestTheRecordedName|TestTheServerName|TestAnAccountMovedInThunderbird|TestAMoveUnderAnotherName|TestSettingsThatCannotBeRead|TestARunReadsAtMostSoManyGoneWarnings|TestARunNeverLooksAtANetworkPath"`): exit 0.
    3. desktop (`-run "TestTheIdleLock|TestAnyLockEnds|TestLockEnds|TestTheThisArchiveCardAndTooltip|TestHowToUseMailArchiveExplainsDelete|TestAnArmedDeleteMadeStale"`): exit 0.
    4. app, `-run TestZZRV3175_TheFallbackOnlyCallers -v`: exit 1, as intended (RV3-03).
  - I ran no mutations. Nothing could show a dialog, notification or browser.
- **Read:**
  - the process file;
  - the round-2 report and the lead's fold;
  - the design: §0, §1.2 with the Revision 4.2 lines;
  - the catalog's 2026-11-21 amendments;
  - all 4 commit messages and their code changes;
  - `thunderbird.go`: `movedHow`, `matchWith`, `liveServers`, `samePath`;
  - `storefacts.go`: `accountNameOf`, `ReadStoreFacts`;
  - `autoplace.go`: the changes;
  - `run.go`: `Run` up to the lock, `newRunRecord`, `RecordRefusedRun`, `writeRefusedRun`, the ok-record branch;
  - `sourceloss.go` and `tbcompleteness.go`: `storeID`, `fingerprint`, `tbRecords`, `assessThunderbird`;
  - `state.CleanFolderName`;
  - `mailboxgone.go`: the Repair helper;
  - `localschedule.go`, `wizard.go:770-809`, `jobrun.go:250-384`, `main.go` (`runExport`, `prepareThunderbird`);
  - `archdelete`: `lister.go:575-650` and `Captured`;
  - the MA-1481 test file and its helpers.
- **Could not check:**
  - **Thunderbird itself** (recalled, not checked):
    - that a new account's name is its email address, so three Gmail accounts have three different names;
    - that an account added again gets a new "-N" folder while its old folder is still there (RV3-05).
  - **Windows:** none of the new rule ran on Windows.
  - **The command line from end to end** for RV3-03. I showed the function it calls; I did not run the command.
  - RV3-04, RV3-05 and RF3-03 come from reading the code, not from runs.
  - I did not repeat the builder's mutation runs (N1 to N4, M1, KeyOpen).
- **Left in the worktree** (untracked, never committed, no covers marker): `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv3/internal/app/zz_rv3_175_app_test.go`

## Fixed items

| Item | Fixed? | Evidence |
|---|---|---|
| RV2-01 (the record decides) | Partly | Owner's setup, all three accounts in the profile: one removed is skipped, one moved stops. The committed tests pass. My positive twins pass too, including the name read back by store id from a real ok record. See RV3-01 (it holds for one scheduled run only) and RV3-02 (renamed). |
| RV2-03 ("-N" tests) | Yes | `TestTheServerNameFallback…` covers "-N" at the end, before the last dot, and with no dot. It passes. |
| RV2-06 | Yes | `sourceloss.go:217` now checks `InPlace` before any look on disk. |
| RF2-01 (two forms, name kept out) | Yes | The test forbids the name, so MA-1288 holds. My runs printed the same-server form with no name. I agree with the gate held: the error reaches log files. |
| One store id | Yes | `autoplace.StoreID` is the old hash exactly: the same prefix, the same normalising, and the first 16 bytes of SHA-256 in hex. So names in older 1.7.x records still match. |
| RV2-02 (d50d67a) | Yes | `KeyOpen` is set only when the sealed values were read with the key (`lister.go:619-623`), so the test is exactly "read with its key open". The tests pass. See RF3-02 for the words. |
| RF2-02 to RF2-06 (8c8e915) | Yes | The card test passes. The words match the code, and the harness pins match `deletescript.go`. RF2-05 overclaims again: see RF3-01. |
| The three fallback-only callers | Partly | `wizard.go:789` cannot disagree: it lists no inputs. The other two can: see RV3-03. |

## New findings

### RV3-01: MEDIUM. After one stopped scheduled backup, the next one skips the moved account quietly

- **Where:**
  - `run.go:1504-1518`: `writeRefusedRun` keeps no account names.
  - `run.go:1614-1617`: names are written only by a run that ended ok.
  - `autoplace.go:219`: `RecordedName` reads only those names and the gone-mailbox warnings.
- **Sequence:**
  1. An "All of them" archive holds several accounts on one mail server. One of these is also true:
     - two of them are moved at once (the owner's three-Gmail setup);
     - another account on that server has always kept its mail off the profile (the commit's own test setup);
     - the account's server name was changed after Thunderbird made its folder.
  2. The person moves one account's mail to another folder.
  3. Scheduled run 1: the record names the account, so the backup stops with the right words. Its failed record names no account.
  4. Scheduled run 2: no name is recorded, so the fallback decides. It sees two accounts off the profile, or none on that server name, and skips.
  5. Every later run "succeeds" without that mail.
     - Status says "add its account back in Thunderbird", but the account is still there.
     - After 14 days, only a note is left.
  - Run now does the same: plaintext Run now records its refusals too (`jobrun.go:373`).
  - Any other record that is not an ok run (failed, cancelled, or a killed run's running record) drops the names too. A move after such a run is decided by the fallback from the first run.
- **Red tests:** each one's first run stopped by name (positive twin), then:
  - `…_ThirdOffProfile`: "the run after a refusal skipped the moved account quietly: err=<nil>". The log showed the refused record "failed with 0 Thunderbird entries and 0 warnings; RecordedName now """.
  - `…_TwoOfThreeMoved`: "skipped both moved accounts quietly".
  - `…_HostnameChanged`: the same.
- **Fix (the lead's call):**
  - Keep each account's store id and name in every record a plaintext run writes (running, refused, failed, cancelled), carried from the previous record when the run has none of its own. `RecordedName` then reads that list. MA-1288 allows names in the record; it only keeps them out of log files.
  - Tombstone: a moved account stops two scheduled runs in a row.
  - Or: rule it a known limit (see RF3-01).

### RV3-02: LOW. An account moved and renamed at once is skipped quietly

- **Where:** `thunderbird.go:686-688`. When no live account has the recorded name, the rule returns "removed" without asking the server-name rule.
- **Sequence:**
  1. The archive has one account per server, the simple case.
  2. Between two backups, the person moves an account's Local Directory and renames it. Both are on the same settings page in Thunderbird.
  3. The next backup skips it.
  - Round 2's rule stopped this case: my log shows the server-name rule alone refuses it.
- **Red test:** `TestZZRV3175_AMovedAndRenamedAccountStops`: "an account moved to …/QZUSB/QZWork and renamed was skipped quietly". Positive twin: moved with its name unchanged stops.
- **Fix choices:**
  - (1) Keep it as a known limit.
  - (2) When no live account has the name, ask the server-name rule. This brings back the traded case: a removed account beside a same-server account kept off the profile would stop the backup again.
  - I lean to (2): Q7 prefers a loud stop to a quiet loss, and both setups are rare.

### RV3-03: LOW. Two fallback-only callers disagree with the run in the traded setup

- **Setup:** the second account is removed; another account on that server has always kept its mail off the profile.
- The run, asking with the record's names, skips it. Two callers still ask without the names:
  - `prepareThunderbird` (`main.go:2104`, used only by command-line jobs with `-enable-offline` or `-sync-wait`) refuses, with the same-server words.
    - That refusal returns at `main.go:434-436`, so no failed run is recorded.
    - Before 45e12aa, the check that runs first refused it and recorded it.
  - The Desktop's rebuild (`localschedule.go:104`) says no, so Settings offers no create button for a schedule the run would accept.
- **Red test:** `TestZZRV3175_TheFallbackOnlyCallersAgreeWithTheRun`. It printed:
  - "prepareThunderbird's check refuses where the run skips: …";
  - "the Desktop's rebuild refuses where the run skips".
- **Fix:**
  - Pass `app.RecordedNamesAt(*o.out)` through to `prepareThunderbird`, using `DiscoverInputsFor`.
  - In the rebuild, use `GoneSkippableAs(in, autoplace.RecordedName(record, in))`. It already reads that record.

### RV3-04: INFO. Encrypted archives always use the fallback

- `RecordedName` returns "" for an encrypted record (`autoplace.go:220-222`). This follows the rulings Q3, Q5 and Q6: an encrypted record keeps no names.
- So RV3-01's setups skip a moved account quietly from the first run, and RV3-02 does not apply.
- Nothing says so for encrypted archives. Proposed line for MA-1481: "An encrypted archive's record names no account, so the server-name rule always decides for it."
- **Related:** the Repair helper reads only failed records. Today those never hold names, so in practice it uses the fallback. It still agrees with the run, which reads the same record. The committed helper test plants a failed record with a named warning, which no run writes today. The RV3-01 fix would give the helper real names.

### RV3-05: INFO (from code, not run). An account removed and added again can stop the backup once

- **Sequence:**
  1. An account is removed in Thunderbird, keeping its mail data.
  2. It is added again under the same name and gets a new "-N" folder in the profile.
  3. The old folder is deleted before the next backup.
- **Result:**
  - The record's name now matches the new account, so the backup stops, wrongly.
  - The new folder is in the profile, so auto-discovery would back it up anyway.
  - The next run falls back and skips the old folder, so it fixes itself after one stop. It fails closed, and it is rare.

### Other cases tried (no finding)

- **Two accounts with the same name:** the record keeps neither name (`tbRecords` and `TBDistinctNames`), and two live accounts with one name go to the fallback. Safe.
- **A record from an older version:**
  - With names, they match, because the store id hash is the same.
  - Without names, the fallback decides, as in round 2.
- **A planted record:** a path in it is checked as local and in a place auto-discovery looks before any look on disk, and store ids are compared as hashes only. A planted name can only turn a stop into a skip, or the reverse. That needs write access to the archive folder, the class ruled out before (INS-07).
- **Local Folders:** it is not IMAP, so the record has no name for it and the fallback decides.
  - Moved: it stops.
  - Deleted by hand: it is skipped, because its setting still names the folder.

## Word findings

**RF3-01 (low; only if RV3-01 is ruled a known limit). The README's `-auto` passage overclaims.**
- Proposed: "…a link, or a mailbox outside the usual places still stops the backup. A Thunderbird account moved to another folder stops it too when Thunderbird's settings show the move; with several accounts on one mail server it can instead be skipped after the first stopped backup, so check status after moving one."

**RF3-02 (info). The guide's step 3 and the README say "(for an encrypted archive, until it is locked)".**
- **Why:** a plan made while the encrypted archive was unlocked ends when any archive is locked, including another archive's 30-minute idle lock, not only when "it" is locked.
- Proposed: "A plan waits up to a day, but a backup in the meantime makes MailArchive check again. For an encrypted archive that was unlocked when it was checked, locking any archive ends the plan."
- MA-1004's pin would change with it.

**RF3-03 (info; from code, not run). The same-server words for a moved Local Folders (`autoplace.MovedSameServerText`).**
- **Why:** Local Folders has no mail server.
- Proposed opening: "input OLD does not exist, so this backup stopped: a Thunderbird account on the same mail server, or Local Folders, now keeps its mail in NEW (its Local Directory setting)." The rest is unchanged.

Working under the Limen method. Clock read: 2026-11-21, 01:20 to 01:30 EST. Register: a finding report; the verdicts and severities are my own. Sources: the worktree's code, tests, design, catalog and the round-2 report, all reached, plus the test runs above. Not checked, per claim: Thunderbird's account naming and its re-added folder names (recalled); Windows (not run); RV3-03's command-line path (shown at the function it calls); RV3-04, RV3-05 and RF3-03 (code only). Borrowed lines: test output and product words, quoted as printed. Drift: none noticed. Touched: one untracked test file; no code changed; no mutations. Persona: none.
