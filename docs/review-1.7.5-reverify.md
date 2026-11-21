*Filed by the lead, 2026-11-20 (clock 23:19 EST). The report below is the reviewer's, word for word:
an independent Claude agent that wrote no part of 1.7.5 and took no part in its first reviews,
re-verifying the fixes at commit 1ba95c8 (the 15 commits from ea42de3). Part A is the adversarial
re-verify (`assurance-kit/process/adversarial-review.md` §3); Part B is the friction re-check of the
new words. The lead's fold comes first.*

## The lead's fold

| ID | Severity | Decision |
|---|---|---|
| RV-01 | MEDIUM | Fix: on Windows a path is local only when its volume is a drive letter; the Repair helper checks the auto-discovery places before any look on disk. Tests with the three spellings; Windows CI and the lab run them |
| RV-02 | MEDIUM | Fix, as the fuller form of the owner's Q7 ("Stop the backup"): an IMAP account is moved when exactly one live account has the folder's server name and none of its folder settings lies in this profile's ImapMail/ or Mail/; a POP account under Mail/ is matched by its server name the same way. The traded case (a removed account whose only same-server sibling was moved off the profile) stops the backup, which is Q7's direction. The lead's call |
| RV-03 | LOW | Fix: the server's "changed" reply says whether it is still the same archive in the same folder; the page re-arms only then (within the owner's Q9, which named a backup in between) |
| RV-04 | INFO | Words fix: status says when Thunderbird's settings could not be read for certain |
| RV-05 | INFO | No change. Planting a name needs write access to the archive folder (the same class as INS-07, refuted); the name is cleaned and capped, and no log holds it |
| RV-06 | LOW | Fix: any Lock ends every encrypted archive's waiting delete plan |
| RV-07 | INFO | No change in 1.7.5 (not a regression); noted for later |
| RV-08 | INFO | No change: the owner's Q8 |
| RF-01, RF-02 | medium | Build the reviewer's words |
| RF-03 to RF-10 | low, info | Build the reviewer's words (RF-03's walk is the lab's) |
| RF-11 | info | Covered by RV-02 and RV-04 |

After the build: a focused re-verify of these fixes only.

---

# MailArchive 1.7.5: re-verify of the fixes (commit 1ba95c8)

**Bottom line:** 1.7.5 is not ready to ship as it is. Two medium-severity gaps need a fix or your ruling first:
- **RV-01:** on Windows, three ways of writing a network path get past the new "is this path on another computer?" rule.
- **RV-02:** a Thunderbird account moved to a folder with a different name is still skipped without any warning that stops the backup.

The other fixes hold. I re-ran five of the tests that prove a fix (the "tombstone" tests). Each passed with its fix in place and failed with the fix taken out, as it should. The new wording is fine to ship once two delete sentences are fixed (RF-01 and RF-02).

## Header

- **Date (clock):** 2026-11-20, 22:59 to 23:16 EST.
- **Reviewed:** worktree `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv`, detached at 1ba95c8 (1ba95c838b528dea90f8556818d5e0329d1eff37). The fixes are the 15 commits from ea42de3 to 1ba95c8.
- **Reviewer:** a fresh Claude session (Opus 5.5). It wrote no part of 1.7.5 and took no part in its first reviews.
- **Tools:** Go 1.26.0 on macOS (Darwin 25.6.0). The page tests ran in Node, with no browser.
- **Memory:** `memory_pressure -Q` before every `go test`: 61 to 65% free. One test run at a time.
- **Commands and exit status:**
  - `date`, twice: 0.
  - `git -C WT`: `log` twice, `diff --stat` twice, `diff -- <files>` 7 times, `status --short` 3 times, `rev-parse HEAD`: all 0. `git grep` twice: one 0; one gave no output because the file did not exist at ea42de3.
  - `git -C WT checkout -- <file>`, 4 times, to undo my own test changes: 0.
  - `go env GOTOOLCHAIN GOVERSION GOFLAGS`: 0. `go env GOROOT`: 0. I only looked at where Go is installed. I did not open it, because it is outside the folders I may read.
  - `memory_pressure -Q`, 9 times: 0. `which node`: 0. `wc -l`: 0.
  - About 30 read-only `grep` searches: 0. Three failed harmlessly: two zsh "no matches" errors and one search with no match (each exit 1).
  - Tests, each `go -C WT test -p 2 -count=1 -run <filter> ./internal/<package>/`:
    1. app, my new tests: exit 1. Three failed as intended; one passed.
    2. app, the dangling-link fix (INS-03) taken out: exit 1, failed as wanted.
    3. desktop, the Lock fix (Q10) taken out: exit 1, failed as wanted.
    4. desktop, my new test: exit 1, failed as intended.
    5. desktop, the delete re-check (Q9) set to re-arm every time: exit 1, failed as wanted.
    6. health, the network-path check (OUT-01) taken out: exit 1, failed as wanted.
    7. health, my new test that prints the real words: exit 0.
    8. to 10. The tombstone tests with every fix in place (6 in app, the health set, 9 in desktop): all exit 0.
  - Every change I made to test a fix was undone with `git checkout`. None of them touched a dialog, notification or browser guard, so nothing is owed on CI.
  - I also read `/etc/auto_master`, to see whether macOS's automatic network folder (`/net`) is on. It is turned off, so I dropped that line of attack.
- **Read:**
  - both process files;
  - both review reports, with the lead's notes at the top;
  - the design: §0, §1.1 and §1.2, plus the "Revision 4" lines found by search;
  - all 15 commit messages;
  - the code changes in 21 files;
  - the account-matching and settings-reading code in `thunderbird.go`;
  - the delete route (`deleteAction`), the delete's re-check (`samePlan`), the encrypted plan's key line (`KeyLine`) and how a delete result is shown (`delShowHeld`);
  - the new tombstone tests and page scenarios;
  - catalog rows MA-1288 and MA-1471;
  - the wizard's start-up health check.
- **Could not check:**
  - Windows itself. RV-01 rests on how Go and Windows read paths, which I recalled and did not run. The Windows junction case (INS-04) stays with the Windows lab.
  - A real browser: the delete panel's timing, what happens when the page is closed, and how the earlier delete result looks after an automatic re-check. I read these in the code; the page tests ran in Node.
  - Thunderbird itself: how often a moved account's new folder gets a new name.
- **Left in the worktree** (untracked, not committed, no covers marker):
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv/internal/app/zz_rv_175_app_test.go`
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv/internal/desktop/zz_rv_175_desktop_test.go`
  - `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv/internal/health/zz_rv_175_health_test.go`

---

## Part A — Adversarial re-verify

**Verdict: NOT READY AS IS.** New findings: 2 MEDIUM, 2 LOW, 4 INFO; nothing HIGH. Every fix I re-tested holds. RV-01 and RV-02 need a fix or your ruling before release.

| ID | Fixed? | Evidence |
|---|---|---|
| OUT-01 | partly | With the helper's network-path check taken out, the test failed: "\\evil-host\share\Mail as the refused input: the planted path was looked at". The fix holds for paths starting `\\` or `//` on every system. On Windows, three other spellings get through: see RV-01. |
| INS-02 (Q7) | partly | MA-1481's tombstone passes. My new test fails: "an account Thunderbird still lists, moved to …/QZUSB/QZWork (another folder name), is skipped as gone". See RV-02. |
| INS-03 | yes | With the second look (at the cleaned path) taken out, the test failed: `GoneSkippable(".../imap.qzb.example/") is true for a dangling link` and "expected typed refusal exit 1, got 0". |
| INS-06 | yes | 70 planted missing mailboxes show as 65 lines, the last "6 more mailboxes … (not listed here)". A planted right-to-left character is removed. The builder's own fail runs were not repeated. |
| CON-01 | yes | First found Nov 19 at 15:00: status names "on or after Dec 4", the next full local day. |
| CON-02 (Q8) | yes | `TestAClockBehindNeverMovesTheFirstFoundDateEarlier` passes. The builder's fail runs were not repeated. See RV-08. |
| CON-03 | yes | Printed: "a mailbox this archive backs up was not found, so it was skipped; its record does not say which. …" |
| CON-04 | yes (text) | The design's §1.3 corrects R9 beside its text. |
| OUT-02 | yes | `TestAnArmedDeleteOfARowRemovedFromTheListSaysSo` passes. |
| OUT-03 | yes | The page scenario where a sent delete holds no token (`sentHoldsNoToken`) passes. |
| OUT-04 guard | yes | `TestEveryAutomaticDeletePostIsArmedWithItsRow` passes. There are still exactly two delete sends in the page, with Q9's re-arm. |
| OUT-05 (b), CON-06 (Q10) | yes, for the same spelling | With the Lock fix taken out, the test failed: "expected typed refusal exit 200, got 0" (the archive was deleted after Lock). Another spelling of the folder: see RV-06. |
| F5 (Q9) | yes | Set to re-arm every time, the test failed: `{"dels":5,"asked":6}`. So it cannot loop. It never re-arms after **Delete permanently** (`deletescript.go:389`). The server still refuses anything that needs a decision (`archivedelete.go:666`). The trigger is wider than the ruling: see RV-03. |
| INS-04 | open | Left to the Windows lab (design §4, step 4). |
| INS-05 | written as a known limit | Design §5. |

### RV-01 — MEDIUM — On Windows, three spellings of a network path count as local

- **Where:** `internal/autoplace/autoplace.go:30-35` (`NonLocal`). It is used by `health/mailboxgone.go` (the Repair helper), `app/sourceloss.go:190` and the Desktop's `isRemotePath`.
- **The rule today:** a path counts as non-local if it starts with `\\` or `//`, or is not absolute.
  - On Windows, Go calls any path that starts with two separators of either kind (`\/`, `/\`) absolute, and also `\??\…`.
  - So `\/qz-host/s/x`, `/\qz-host\s\x` and `\??\UNC\qz-host\s\x` all count as local.
  - Windows reads all three as `\\qz-host\s\x`, a network share.
- **Sequence:** the same as OUT-01, with the path spelled `\/qz-host/s/x`.
  1. Someone who can write to the archive folder plants a loud source-loss streak and a failed record. The record's job lists that path as an input, and its error is the standard "does not exist" text for it.
  2. The person opens MailArchive Desktop, runs `status`, or starts the wizard.
  3. The Repair helper lets the path through, then checks it on disk (`autoplace.go:224`). Windows signs in to qz-host with the person's credentials. That last step is recalled, not run.
  4. A scheduled run that skips a real missing mailbox does the same for a planted path in its previous record (`sourceloss.go:190`).
- **Scope:** the page now limits how long it waits, but not this sign-in. The Desktop's rebuild check already used this rule before 1.7.5. OUT-01 made three more places depend on it. MA-869's tests use only the `\\` and `//` spellings.
- **Fix:**
  - On Windows, count a path as local only when its volume is a drive letter, such as "C:".
  - In the Repair helper, check the path against the places auto-discovery looks before the first look on disk. This needs no disk access.
- **Test for Windows CI or the lab:** `NonLocal` must say non-local for all three spellings, and MA-869's health and Desktop tests, repeated with them, must look at nothing.

### RV-02 — MEDIUM — Q7 stops the backup only when the moved folder keeps its old name

- **Where:** `thunderbird.go:600-638` (`MovedTo`) reuses the existing hostname match (`thunderbird.go:683`, `:699-713`). That match:
  - requires every folder setting of the account to end in the old folder's name;
  - works only for folders directly under ImapMail/.
- **Red test:** `TestZZRV175_Q7_MoveUnderAnotherNameOrUnderMailIsNotSkipped`. The positive twin passes: a move that keeps the folder's name stops the backup. Then:
  - "RV-02a: an account Thunderbird still lists, moved to …/QZUSB/QZWork (another folder name), is skipped as gone (… err <nil>): its mail is never read again". Both folder settings were written as Thunderbird writes them.
  - "RV-02b: a POP account Thunderbird still lists, moved to …/QZUSB2/pop.qzc.example, is skipped as gone". Accounts under Mail/ never match by hostname.
- **What goes wrong:** the same harm as INS-02.
  - The mail in the new folder is never backed up.
  - The warning fades to a quiet note after 14 days.
  - The note says "add the account back in Thunderbird", but the account is still there.
- **Held:** an account removed beside another account on the same server (`imap.qzd.example` and `imap.qzd.example-1`) is still skipped, as it should be. That stays true when the other account's folder is on a USB drive (`TestZZRV175_Q7_RemovedBesideASameHostAccountIsSkipped`, passes).
- **How common:** recalled, not checked. Mozilla's steps for moving an account's folder keep its name. A person who points Thunderbird at a new, empty folder gets a different name.
- **Fix (your call, because it trades one case):**
  - Call an IMAP account moved when exactly one live account has the folder's server name and none of its folder settings is inside this profile's ImapMail/ or Mail/.
  - For a POP account under Mail/, match its server name the same way.
  - The cost: a removed account whose only same-server sibling was moved off the profile would then stop the backup. That fails closed, which is Q7's direction.

### RV-03 — LOW — Q9 re-arms after any change, not only after a backup

- **Where:** the page re-arms whenever the server says "changed" (`deletescript.go:388-389`). The server says "changed" for any difference in its re-check (`archivedelete.go:727-728`; `archdelete/execute.go:349-350` and `:372-378`). That includes a different folder now sitting at the same path.
- **Sequence:**
  1. You click **Delete when the check finishes** on D:\Mail\Busy.
  2. During the check, the folder is replaced by another folder of the same name: a restored copy, or another archive moved in.
  3. The delete is refused as changed, and the page re-arms.
  4. The new check is of the other folder. If nothing in it needs a decision, it is deleted without ever being shown.
- **Scope:** your Q9 answer covered "a backup ran in between". The server still refuses anything that needs a decision. I read this in the code; I did not run it.
- **Fix:** the server's "changed" reply also says whether it is the same folder and the same kind of archive, and the page re-arms only then.

### RV-04 — INFO — A settings file MailArchive cannot read for certain hides a move

- **Red test:** `TestZZRV175_Q7_HandEditedUserJSHidesAMove`. A user.js with a `/* */` comment makes a moved account skip quietly. Hand-made privacy files often hold such comments.
- **Weighed:** the builder's choice is right. Stopping instead would stop every removed account in such profiles for good, against your Q4 ruling.
- **Suggested words-only fix:** the run records that the settings could not be read, and status adds: "If you moved this account to another folder in Thunderbird, MailArchive could not tell from its settings."

### RV-05 — INFO — F3: a name planted in the archive's record is shown and carried forward

- **Red test:** `TestZZRV175_F3_PlantedRecordNameIsShownAndCarried`. The skipped mailbox was named "QZ PLANTED call 555-0100 gnp.exe" on two runs in a row.
- **Held:**
  - The right-to-left character is removed and the name is capped.
  - The run's log never holds the name (MA-1288).
- **Scope:** this is not a new kind of exposure. Status already showed names from the record. What is new: the run copies the name into its own new record and the Run now note, and keeps it while the mailbox stays gone.
- **Optional fix:** take the name only from the record's Thunderbird entries, not from an earlier gone-mailbox warning.

### RV-06 — LOW — Q10: Lock misses a plan made under another spelling of the folder

- **Red test:** `TestZZRV175_Q10_LockUnderAnotherSpellingEndsThePlan`. The archive was opened as `/private/var/…/Enc` and listed as `/var/…/Enc`. After Lock, its delete plan still waits.
- **Where:** `pending.go:246` matches by the exact spelling only. The builder named this limit.
- **Scope:** rare. It still ends after 24 hours, a switch, or Shut down.
- **Fix:** on any Lock, end the delete plans of encrypted archives whose folder is the same folder on disk, as `openAt` already matches it. Or simply end every encrypted archive's delete plan.

### RV-07 — INFO — `status` and the wizard's start still have no time limit

- **Where:** `cmd/mailarchive/status.go:89` and `wizard.go:1233-1235` ask the Repair helper with no time limit. Only the Desktop is bounded.
- **What can happen:** a local path on storage that hangs holds them. Example: a mapped drive for a redirected AppData folder that went offline.
- The Desktop also waits up to 2 seconds for each Your-archives row, one after another.
- **Scope:** not a regression. Both had no limit before.

### RV-08 — INFO — Q8 keeps a later first-found date with no upper limit

- A clock once set years ahead, or a planted date, gives a warning that names a future date: "not found since Jan 1, 2037 … on or after Jan 15, 2037". It stays until that date passes.
- This follows your Q8 ruling. Noted for you only.

---

## Part B — Friction re-check

**Verdict: SHIPPABLE**, as a design. Two medium word fixes, RF-01 and RF-02, should be built before the public release, as the kit requires. The rest are low.

I read each new sentence as four people would: you on Windows (weighed first), an encrypted-archive user, a Linux user, and a person deleting a large archive.

### Kept as written

- **Status, a missing mailbox:**
  - "…from the first backup on or after Dec 4, 2026 this is only a note."
  - "a mailbox this archive backs up was not found, so it was skipped; its record does not say which. Click Run now in MailArchive Desktop to see which one."
  - "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again." The plural form in the summaries is kept too.
  - "…and Local Folders is still backed up."
  - The note's "to end it, click Make a new archive… in MailArchive Desktop, choose All of them and this same archive folder, and answer Yes to keeping it current." RF-09 adds one sentence.
- **Repair, for archives 1.7.4 stopped:**
  - "(the missing mailbox: PATH)" and "MailArchive now skips a mailbox that is gone…"
  - "Its scheduled backup is set, and MailArchive now skips a mailbox that is gone, so the next backup backs up the others; to back up now, click Run now on its Scheduled backup card."
  - Settings card: "If a mail account was removed, click Repair on the Scheduled backup card: MailArchive now skips a mailbox that is gone and backs up the others."
  - Repair's reply: "Repaired. The next backup skips the missing mailbox; to back up now, click Run now."
  - Status's red line: "FAILED: input PATH does not exist".
- **Command line (Linux user):** "Not found, would be skipped: PATH".
- **Delete panel:**
  - The checking line, with "You can type the folder's name below meanwhile."
  - The waiting line after clicking Delete when the check finishes, as written.
  - "Nothing was deleted: the plan holds something to decide first (REASONS). Read it, then click Delete permanently."
  - The line for a typed name that does not match.
  - "The page lost touch with MailArchive during the check, so nothing was deleted. Click Delete… again."
  - "This archive was removed from Your archives while its check ran, so nothing was deleted."
- **Guide** steps 1, 3 and 4, and X14's amendment.

### Reworded (findings RF-01 to RF-11)

**RF-01 (medium, buildable now):** the guide's step 2 (`help.go:258`) and the README (around `README.md:351`) say "MailArchive deletes as soon as the check ends". The panel says the delete checks the folder once more, which takes about as long again. A person deleting a large archive would plan around the wrong time.
- **Guide step 2:** "While it checks, type the folder's name. Then either wait and read the plan, or click **Delete when the check finishes**: when the check ends, MailArchive checks the folder once more (about as long again), then deletes it, unless the plan holds something you should decide first, which it then shows you. An encrypted archive's plan is always shown first. Until the delete starts, **Cancel** or closing the page deletes nothing."
- **README:** the same middle sentence.

**RF-02 (medium, buildable now):** the new line sits under a result that says "Click Delete again". It also says "A backup ran" for any change.
- With the automatic re-check (Q9), a person who walked away comes back to the old "Click Delete again" result above a "Deleted" result. After a page reload, the old result shows without the new line at all.
- **After Delete when the check finishes:** "No need to click Delete again. Something changed after the check (a backup, say), so nothing was deleted yet. MailArchive is checking again below and then deletes the archive, unless it finds something you should decide first."
- **After Delete permanently:** "No need to click Delete again. Something changed after the check (a backup, say), so nothing was deleted. MailArchive is checking again below, with your typed name kept: click Delete permanently when the new plan shows."
- **Also:** when a later delete of the same folder ends, add to the earlier result: "A later delete of this folder ended: see its result."

**RF-03 (low to medium):** the moved-account refusal.
- Its second choice leaves this archive stopped for good.
- It does not say where the Local Directory setting is.
- **Proposed:** "input OLD does not exist: Thunderbird still has this account, but now keeps its mail in NEW (its Local Directory setting was changed), so this backup stopped. To back that mail up again, either set the account's Local Directory back to OLD in Thunderbird (Account Settings, then Server Settings); or back up NEW as an archive of its own (Make a new archive…, then Thunderbird / mbox mail folder) and stop this archive looking for OLD (Make a new archive…, then All of them and this same archive folder)."
- The last part follows the note's own advice (F11). I did not walk it.

**RF-04 (low):** for encrypted archives, "unlock it" reads as "unlock the mailbox".
- **Warning:** "…so it was skipped and the others were backed up; to see which one, unlock this archive on its Encryption card in MailArchive Desktop, then click Run now"
- **Short form:** "…so it was skipped; to see which one, unlock this archive on its Encryption card in MailArchive Desktop, then click Run now"
- **Note:** "…To see which one, unlock this archive on its Encryption card in MailArchive Desktop, then click Run now. This line stays while the archive's backup still lists that mailbox."

**RF-05 (low, Windows):** an Outlook data file (.pst). "Choose it in its new place" makes a separate archive without saying so, and the sentence drops "the next backup finds it again".
- **Proposed:** "If you moved this file, put it back in FOLDER and the next backup finds it again. To back it up from its new place instead, click Make a new archive… in MailArchive Desktop and choose the file there: that makes a separate archive."
- Use the same words in the end-of-run summaries.

**RF-06 (low):** "names its key" is unclear.
- **Proposed:** "For an encrypted archive, the plan is always shown before anything is deleted, because it says what happens to the archive's key on this computer."

**RF-07 (low):** the This archive card (`dashboard.go:2041`) is untrue for an encrypted archive, and it drops "nothing needs your decision".
- **Plain archive:** "Delete… erases its files; you see what it would remove first, unless you choose Delete when the check finishes and nothing in it needs your decision."
- **Encrypted archive:** "Delete… erases its files, after showing what it would remove."

**RF-08 (low):** at exactly 65 missing mailboxes, the cap line says "1 more mailboxes … are" (`health.go:872`).
- **Proposed:** "1 more mailbox this archive backs up is no longer on this computer (not listed here)".

**RF-09 (low, a design choice):** following the note's advice resets an Hourly or custom schedule, and the note does not say so.
- **Add:** "Then check How often in Settings: this sets the schedule again."

**RF-10 (INFO):** the README says nothing about 1.7.5's main change. Across all of 1.7.5 it changed only its delete section.
- **Proposed, under `-auto`:** "With `-auto` (an "All of them" archive), a listed mailbox whose folder is gone is skipped while the others are backed up; `status` warns for 14 days of backups, then keeps a note. A missing profile, a missing ImapMail or Mail folder, a link, a mailbox outside the usual places, or a Thunderbird account moved to another folder still stops the backup."

**RF-11 (INFO):** an account that was moved but is still skipped (RV-02, RV-04) is told "If you didn't remove it, add the account back in Thunderbird". The account is still there. Fixing RV-02 fixes most of this.

### Do the guide, the This archive card and the README match the panel?

- The panel, its tooltip and X14 agree.
- The guide and the README disagree with the panel on how long the delete takes (RF-01).
- The This archive card is wrong for an encrypted archive (RF-07).

**Smallest check for you:** run `go -C /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-rv test -p 2 -count=1 -run ZZRV175 ./internal/app/`. You should see three failing tests: RV-02, RV-04 and RV-05.

Working under the Limen method. Clock read: 2026-11-20, 22:59 to 23:16 EST. Register: a finding report, my own verdicts. Sources: the worktree's code, tests, design and review reports, all reached; the test runs listed above. Not checked, per claim: RV-01's Windows path behaviour (recalled; no Windows here, and Go's source is outside the folders I may read); RV-02's "how common" (recalled Thunderbird behaviour); RV-03 and RF-02's earlier-result display (read in code, not run). Borrowed lines: test failure lines and product words, quoted as printed. Drift: none noticed. Touched: three untracked test files; four temporary code changes, each undone with `git checkout`, worktree confirmed clean. Persona: none.
