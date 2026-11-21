*Filed by the lead, 2026-11-21 (clock 00:37 EST). The report below is the reviewer's, word for word:
an independent Claude agent that wrote no part of 1.7.5 and took part in none of its reviews,
re-verifying the 10 commits 1b7b3df..8a92441 (round 2). The lead's fold comes first.*

## The lead's fold

| ID | Severity | Decision |
|---|---|---|
| RV2-01 | MEDIUM | Fix, with a sounder rule than the server-name guess. The previous record already names each folder's account (by `store_id`, the source F3 uses). When it names the gone folder's account, and exactly one live account has that same name with a folder setting that is not the gone folder, the account was moved: stop (the owner's Q7). When it names an account no live account has, it was removed: skip. The server-name rule stays only as the fallback for a record that names no account, with the reviewer's (a) ("-N" before the last dot, as Thunderbird spells `imap.gmail-1.com`) and (b) (exactly one same-server account wholly outside the profile). This matters for the owner's own setup: three Gmail accounts on one server |
| RV2-02 | LOW | Fix: Lock ends only an encrypted plan that read values with its key open (the reviewer's tried rule); the README and guide say "for an encrypted archive, until it is locked" |
| RV2-03 | LOW | Fix: tombstones for each "-N" spelling |
| RV2-04 | INFO | No change in 1.7.5 (hand-typed only; the run fails either way, "check the path" is right) |
| RV2-05 | INFO | No change |
| RV2-06 | INFO | Fix (one line): `resolveGoneSince` also requires the path to be in an auto-discovery place |
| RF2-01 to RF2-06 | low, info | Build the reviewer's words; RF2-01's words for the traded case follow the new rule (the account named in the record, when known) |

After the build: round 3 of the focused re-verify, on these commits only.

---

# MailArchive 1.7.5: re-verify, round 2 (commit 8a92441)

**Verdict: NOT READY AS IS.** The fixes hold, but one MEDIUM gap is left in RV-02's rule (RV2-01). Fix it, or rule it a known limit, before release. RV2-02 and RF2-01 should be fixed at the same time. Everything else is LOW or INFO.

- **RV2-01 (MEDIUM):** a moved Thunderbird account is still skipped without stopping the backup when another account uses the same mail server. Two Gmail accounts are an example.
- **RV2-02 (LOW):** RV-06's new Lock rule ends too much. After 30 minutes unused, the automatic lock of any other archive ends a waiting delete plan for an encrypted archive. The person starts the check again, which is the owner's "times out too early" complaint coming back for encrypted archives.
- **New words:** they match the code, with six fixes below.

## Header

- **Date (clock):** 2026-11-21, 00:19 to 00:34 EST.
- **Reviewed:**
  - Worktree `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv2`, detached at 8a92441 (8a92441d62f6587bc260fb34f0a9e12b20e7931b).
  - Scope: the 10 commits in `1b7b3df..8a92441`.
- **Reviewer:** a fresh Claude session (Opus 5.5). It wrote no part of 1.7.5 and took no part in its reviews.
- **Tools:**
  - The repo's pinned Go, on macOS (Darwin 25.6.0).
  - The delete page's tests ran in Node, with no browser.
- **Memory:** I ran `memory_pressure -Q` before each `go test`, 8 times. It showed 63 to 64% free. One run at a time, each with `-p 2 -count=1` and a `-run` filter.
- **Commands and exit status** (WT is the worktree):
  - `date`, twice: 0.
  - `git -C WT log --stat 1b7b3df..8a92441`: 0.
  - `git -C WT show` of six commits' files: 0.
  - `git -C WT diff 1b7b3df 8a92441 -- <files>`, 3 times: 0.
  - `git -C WT status --short`, 5 times: 0.
  - About 22 read-only `git grep` searches and 5 plain `grep` searches, all 0, except:
    - two found nothing (no output);
    - one zsh "no matches found" error from a `--include` pattern (exit 1, harmless).
  - `wc -l`: 0.
  - Tests, each `go -C WT test -p 2 -count=1 -run <filter> ./internal/<package>/`:
    1. app, my 4 tests: exit 1. Two failed as intended (RV2-01 a and b). The "-N" test and the over-reach check passed.
    2. app, with the "-N" branch switched off: exit 1. My "-N" test failed. The two committed tests in the MA-1481 test file still passed.
    3. desktop, my 2 tests: exit 1. The idle-lock test failed. The card test failed on the tooltip.
    4. desktop, with RV-06's new clause taken out: exit 1. RV-06's tombstone test failed. My idle-lock test passed.
    5. desktop, with my proposed rule from RV2-02 put in: exit 0. RV-06's tombstone, the `TestLockEnds` tests and my test all passed.
    6. desktop, the card test with its two checks split: exit 1, on the tooltip only.
    7. app, run with `-v`: my trailing-slash test plus 5 committed tombstone tests. Exit 1: my test failed and the 5 passed.
    8. desktop, with the page's same-folder check removed: exit 1. The page scenario for a replaced folder (`rearmReplaced`) failed.
  - `GOOS=windows go -C WT vet ./internal/app/ ./internal/autoplace/`: 0.
  - `git -C WT checkout -- <file>`, 3 times, to undo my 3 temporary code changes: 0.
  - The worktree now holds only my two untracked test files.
  - None of my changes touched a guard against dialogs, notifications or browsers.
- **Read:**
  - both process files;
  - the first re-verify report, with the lead's fold;
  - the design: its header, §0, §1.2, §3.1 with the Revision 4.1 lines, and §10;
  - all 10 commit messages and their code changes;
  - `thunderbird.go`: `MovedTo`, `matchWith`, `liveServers`, `within`, `SettingsUncertain`;
  - `autoplace.go`, whole;
  - `mailboxgone.go`: the Repair helper and `goneRemedy`;
  - `run.go` and `sourceloss.go`: `DiscoverInputs`, `classifySourceReach`, `resolveGoneSince`;
  - `archivedelete.go`: `deleteAction`, `armable`, `decideReasons`;
  - `archdelete`: `StillSameFolder`, `samePlan`, `lockOnPage`;
  - `pending.go` and `keys.go`: `discardFor` and every lock path, including the 30-minute idle lock (`reap`);
  - the archive row and the This archive card templates;
  - the catalog amendments to MA-1471 and MA-1481;
  - the MA-1471, MA-1475 and MA-1481 tests.
- **Could not check:**
  - **Windows itself:** RV-01's Windows-only tests, the "directory name is invalid" case, and what Windows reports for other bad paths (RV2-04). Windows CI and the lab owe these.
  - **Thunderbird itself:**
    - how it names a second account's folder on the same server (RV2-01 a);
    - where Local Folders keeps its Local Directory setting (RF2-01).
    Both are recalled from Mozilla's code and support threads.
  - **A real browser:** the tooltip and the delete panel's words, on a phone and in Edge.
  - **File ids on Linux and on USB drives:** whether Linux ext4 or FAT/exFAT drives reuse a folder's file id (RV2-05).
- **Left in the worktree** (untracked, never committed, no covers marker):
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv2/internal/app/zz_rv2_175_app_test.go`
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv2/internal/desktop/zz_rv2_175_desktop_test.go`

## Fixed items

| Item | Fixed? | Evidence |
|---|---|---|
| RV-01 (network paths on Windows) | Yes, for the part this Mac can run | The test of the rule itself passes here. The Repair helper checks the auto-discovery places before any look on disk (`mailboxgone.go:171`). The builder's two mutation runs (V1, V2) are recorded clearly; I did not repeat them. The Windows-only tests skip here, so Windows CI owes them. |
| RV-02 (moved account) | Partly | The renamed-folder and POP cases (RV-02a, RV-02b) stop the backup. The held case is still skipped. The "-N" branch works: my test passes, and fails with the branch switched off. But see RV2-01, and RV2-03: no committed test guards that branch. |
| RV-03 (re-arm only for the same folder) | Yes | On the tip, the page scenario for a replaced folder (`rearmReplaced`) still fails when the check is removed (`{"dels":2}`). That matters because commit 41ea335 rewrote that same line. The server-side mutation (Z2) is recorded. Limits: RV2-05. |
| RV-04 (settings that cannot be read) | Yes; see RF2-03 for the words | `TestSettingsThatCannotBeReadForCertainSaySo` passes. |
| RV-06 (Lock ends encrypted plans) | Yes, but too wide | Its tombstone test fails with the fix taken out and passes with it. It also ends plans it need not end: see RV2-02. |
| Windows CI item (61da953) | Yes, as narrowed | `TestADanglingLinkSpelledAnotherWayIsRefused` passes here. The Windows vet is clean. The Windows case itself runs only on Windows CI. |
| The lead's narrowing (8a92441) | Yes | A permission error stays inconclusive: the lead's test passes. Both mutation runs are recorded clearly. One rare side effect: RV2-04. |
| 8c4f70f (restores MA-984's pinned line) | Yes | Read. The pinned line is unchanged, and the new call sits on the line before it. |
| RF-01 to RF-10 | Yes. The words match the code | The This archive card's sentence is right for both kinds of archive (my card test). Remaining word issues: RF2-01 to RF2-06. |

## New findings

### RV2-01: MEDIUM. A moved account is still skipped quietly when another account uses the same mail server

- **Where:** `internal/thunderbird/thunderbird.go:635-649`, in `MovedTo`. The "-N" strip is at :635-638. The "exactly one live account" test is at :647.
- **Case (b), red test:**
  1. Two accounts use one server name. Two Gmail accounts do, and so do a personal Outlook.com account and a work Microsoft 365 account.
  2. The person moves one account's mail to another folder (its Local Directory setting).
  3. Two live accounts have the gone folder's server name, so the rule says "cannot tell" and skips it.
  4. That account's mail is never backed up again. The warning fades to a note after 14 days. The words say "add the account back in Thunderbird", but the account is still there.
  - Red test: `TestZZRV2175_MovedBesideASameServerAccountStops`. It printed: "GoneSkippable(…/ImapMail/imap.qzd.example-1) is true for an account moved to …/QZUSB/QZWork3" and "expected typed refusal exit 1, got 0".
  - Positive twin first: the same move, with no other account on that server, stops the backup.
- **Case (a), recalled, not checked:**
  - Thunderbird names a second account's folder on the same server "imap.gmail-1.com". Its naming routine puts "-1" before the last dot, not at the end.
  - The new "-N" branch strips only a "-N" at the end. So it never fires for a server name with a dot in it.
  - Result: a second account that moves, while it is the only account left on that server, is skipped quietly.
  - Red test: `TestZZRV2175_ThunderbirdsOwnSecondFolderNameMovedStops`. It printed: "GoneSkippable(…/imap.qzd-1.example) is true…". The positive twin, using the builder's spelling `…example-1`, stops the backup.
- **Does the wider rule now stop plain removed accounts?** I found no such case beyond the traded case and its mirror image (a removed "X-1" account whose only same-server sibling was moved).
  - `TestZZRV2175_RemovedBesideSameServerInProfileIsSkipped` passes. It covers:
    - a removed POP account beside an IMAP account on the same server, kept in the profile, with an out-of-date absolute folder setting;
    - the reverse: a removed IMAP account beside a POP account on the same server.
  - One change that is right: a Local Folders folder that was moved now stops the backup too.
- **Fix (the lead's call; both go in Q7's direction):**
  - (a) Also strip a "-N" just before the last dot. Keep stripping one at the end when the name has no dot, as in "Feeds-1".
  - (b) Among the live accounts with that server name, count the account as moved when exactly one of them has every folder setting outside this profile's ImapMail/ and Mail/. The held case (removed beside a same-server account kept in the profile, nothing moved) stays skipped.
  - The trade grows a little: a removed account stops the backup whenever a same-server sibling was moved out of the profile.
  - Or: rule it a known limit, and change the README (RF2-05).

### RV2-02: LOW (extra work for the person; nothing unsafe). RV-06 also ends plans made while their archive was locked

- **Where:** `internal/desktop/pending.go:251-256` (`discardFor`). Every lock calls it, including the idle lock at `keys.go:777-789`, which fires after 30 minutes unused (`keys.go:25`).
- **Sequence:**
  1. The current archive is encrypted and was opened by its remembered key (the default).
  2. The person clicks Delete… on another encrypted archive that is locked. The check of a large archive takes many minutes, and they step away.
  3. After 30 minutes unused, the idle lock locks the current archive. The new rule then ends every encrypted archive's waiting delete plan.
  4. They come back, type the folder's name and click Delete permanently. The page says the delete is no longer waiting, and the whole check runs again.
  - That plan held nothing read with a key, because it was made while its archive was locked. Clicking Lock on the current archive does the same.
- **Red test:** `TestZZRV2175_IdleLockOfAnotherArchiveKeepsALockedArchivesPlan`. It printed: "the idle lock of another archive ended a delete plan made while its own archive was locked".
  - With 6918973's new clause taken out, this test passes. So 6918973 brought the over-reach in.
- **Fix (tried, then undone):** end an encrypted plan only when it read values with its key open:
  `… && it.deletePlan.plan.Kind == archdelete.Encrypted && it.deletePlan.plan.Captured.KeyOpen`
  With it, RV-06's tombstone, the `TestLockEnds` tests and my test all pass (exit 0).
- **Words:**
  - The README and the guide's step 3 say "A plan waits up to a day". That is not true for an encrypted archive that is open, because any lock ends its plan (the owner's Q10).
  - Proposed: "A plan waits up to a day (for an encrypted archive, until it is locked), but a backup in the meantime makes MailArchive check again."

### RV2-03: LOW (a missing test). No committed test guards the "-N" branch

- With the branch switched off (`base` never set, `thunderbird.go:637`), MA-1481's two tests still pass.
- My test `TestZZRV2175_DashNBranchStopsAMovedAccount` fails then: "GoneSkippable(…/imap.qzd.example-1) is true for an account moved to …/QZUSB/QZMoved".
- With the branch on, my test passes, positive twin first.
- So the branch works but needs its own tombstone test. If RV2-01 (a) is built, it needs one for each spelling.

### RV2-04: INFO. A file that is there is called "gone" when spelled with a trailing "/"

- **Where:** `sourceloss.go:48` (`pathLeadsNowhere`) and `run.go:2079`.
- On macOS and Linux, a listed file spelled `…/qz-inbox.mbox/` gives the system error ENOTDIR ("not a directory").
  - It is now refused as "does not exist (check the path, or that the drive holding it is mounted)".
  - The source streak moves toward the 3-day warning and the 30-day pause.
  - Before 1.7.5 it was a plain error. The run fails either way, and "check the path" is the right advice.
- **Red test:** `TestZZRV2175_APresentFileWithATrailingSlashIsNotGone`.
- **How common:** rare. The wizard never writes such a path; only a hand-typed command-line input could.
- **Windows (recalled, not run):**
  - Only Windows error 267 ("The directory name is invalid") is treated as a path that leads nowhere. That is the narrow, safe choice.
  - These still give the plain error, as before 1.7.5:
    - a link that points to itself in a loop (1921);
    - a badly formed name (123);
    - a drive with no disk in it (21);
    - a network share name that is gone (67).
  - None of these is a mail store that is there but cannot be read.
  - Not checked: whether a .pst that is there, spelled with a trailing "\", gives error 267.
- No change needed for 1.7.5.

### RV2-05: INFO. RV-03 checks the folder, not the archive

- **Where:** `StillSameFolder` (`archdelete.go:95`) compares the folder's file id on disk. The reply calls the result `sameArchive`.
- **It can be fooled** if a folder's whole contents are replaced in place during the check: the same folder now holds another archive.
- **Recalled, not checked:** on Linux ext4 and on FAT/exFAT USB drives, a folder that is removed and made again might get back the same file id.
- **It cannot refuse a real backup in between,** because a backup writes inside the same folder. The builder's server test shows this.
- The server still refuses an armed delete of any plan that needs a decision, and of every encrypted plan (`archivedelete.go:666`, `:563`).
- No change needed.

### RV2-06: INFO. RV-01's places check is in the Repair helper only

- **Where:** a run's `resolveGoneSince` (`sourceloss.go:210`) still looks on disk at a planted local path outside the places auto-discovery looks.
- To reach another computer from there, someone would have to plant a link on a local disk. That needs more than write access to the archive (the same class as INS-07, refuted before).
- **Optional one-line hardening:** add `autoplace.InPlace(w.Path)` beside `!autoplace.NonLocal(w.Path)` at that line.
- **Side effect of the helper's new check:**
  - It also says no for a command-line `-auto` job that lists a mailbox outside those places, such as a .pst on another drive.
  - Repair would actually work there; status just keeps its old words.
  - Safe direction. No change needed.

## Word findings

**RF2-01 (low): the moved-account refusal is not true in the traded case.**
- **Why:** after RV-02's trade, the stop also fires when the gone folder belonged to a removed account, and NEW is another account's folder.
  - The words then say "Thunderbird still has this account".
  - They advise setting that other account's Local Directory back to OLD, which would change the wrong account.
- **Also (recalled):** Local Folders keeps its Local Directory on its own page in Account Settings, not under Server Settings. RV-02 now stops a moved Local Folders, so these words reach it.
- **Proposed** (`autoplace.MovedText`): "input OLD does not exist, so this backup stopped: a Thunderbird account on the same mail server now keeps its mail in NEW (its Local Directory setting). If you moved that account there, set its Local Directory back to OLD (in Thunderbird: Account Settings, then Server Settings, or Local Folders' own page), or back up NEW as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder). If you removed the account that used OLD, or you now back up NEW on its own, stop this archive looking for OLD (Make a new archive…, then All of them and this same archive folder)."
- The pinned words in MA-1481's tests ("Thunderbird still has this account") must be restated with it.

**RF2-02 (low; Windows matters most here): the encrypted row's Delete… tooltip contradicts the card above it.**
- **Where:** the Delete… button's tooltip at `dashboard.go:2410`, on the same This archive card that RF-07 fixed.
- It still says "unless you choose Delete when the check finishes". An encrypted archive's panel never offers that button.
- **Red test:** `TestZZRV2175_ThisArchiveCardWords`. The card's sentences are right for both kinds of archive; only the tooltip fails.
- **Proposed,** for a row marked `data-delete-encrypted`: "Permanently delete this archive's files. MailArchive first checks what would be removed, and you type the folder's name to confirm. For an encrypted archive, you always see that list before anything is deleted."
- Note: no committed test pins RF-07's card words. Mine does, but it is untracked.

**RF2-03 (low): RV-04's sentence says what MailArchive could not tell, but not what to do** (`mailboxgone.go:75-80`).
- **Proposed:** "If you moved this account to another folder in Thunderbird, MailArchive could not tell from Thunderbird's settings, so that folder is not backed up: back it up as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder)."

**RF2-04 (low): RF-09's sentence reads as if checking How often resets the schedule** (`mailboxgone.go:245`).
- Today: "Then check How often in Settings: this sets the schedule again."
- **Proposed:** "Making it again sets its schedule afresh, so then check How often in Settings."

**RF2-05 (info): the README overclaims for moved accounts.**
- RF-10's README text under `-auto` says a moved account "still stops the backup". It does not always (RV2-01, and RV-04's settings that cannot be read).
- **Proposed:** "…a link, or a mailbox outside the usual places still stops the backup, and so does a Thunderbird account moved to another folder when Thunderbird's settings show the move."

**RF2-06 (info): RF-02's line can read as a contradiction on a phone.**
- **Where:** `deletescript.go:81-82`. It says "No need to click Delete again" and later "click Delete permanently".
- **Proposed:** start both lines with "No need to click Delete… again." (the row button's own name, with its dots).

Kept as written: RF-01, RF-04, RF-05, RF-06, RF-07's card sentences, RF-08, and RF-02's later-result line.

**Smallest check for you:**
- Run `go -C /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv2 test -p 2 -count=1 -run ZZRV2175 ./internal/app/`.
  - Expect 3 failing tests: RV2-01 (a), RV2-01 (b) and RV2-04.
  - Expect 2 passing: the "-N" branch test and the over-reach check.
- The same command with `./internal/desktop/` should show 2 failing tests: RV2-02 and RF2-02.

Working under the Limen method. Clock read: 2026-11-21, 00:19 to 00:34 EST. Register: a finding report; the verdicts are my own. Sources: the worktree's code, tests, design, catalog and the first re-verify report, all reached, plus the test runs listed above. Not checked, per claim: RV2-01 (a), Thunderbird's folder naming (recalled); RF2-01, where Local Folders keeps its setting (recalled); RV2-04's Windows error codes (recalled); RV2-05's file-id reuse (recalled); RV-01 on Windows (not run). Borrowed lines: test failure lines and product words, quoted as printed. Drift: none noticed. Touched: two untracked test files, and three temporary code changes, each undone with `git checkout`; worktree confirmed clean. Persona: none.
