*Filed by the lead, 2026-11-20 (clock 21:13 EST). The report below is the reviewer's, word for word:
an independent Claude agent that wrote no part of 1.7.5, walking commit 43d4e4c on branch
`combined-archive-gone-mailbox` (diff from 304871c). The lead's fold comes first.*

## The lead's fold

All Type III findings are built before release (the kit's rule), and the new words go to a
re-verify:

- **F1 to F4, F6, F7:** fixed by builder 3, with the reviewer's words as the starting point.
- **F5:** the words fixed (Type III). Re-arming "Delete when the check finishes" after a backup in
  between (Type II) is owner question Q9; the lead recommends one re-arm.
- **F8, F9, F10, F12, F13, F14:** fixed by builder 3.
- **F11:** the words name the button now. The "Stop looking for it" button stays in design §5 for
  the next release.

---

**Verdict: SHIPPABLE.** Both of the owner's complaints are answered. An "All of them" archive keeps backing up when one account's folder is gone. A delete plan now waits up to a day, and **Delete when the check finishes** removes the wait. Nothing in the design blocks shipping. But seven medium fixes, all buildable (Type III), should land before the release. Two smaller design points (Type II, F5 and F11) are the lead's and the owner's call.

# Friction review: MailArchive 1.7.5 (the gone-mailbox skip, the schedule answer, Delete waits)

Reviewer: an independent Claude agent (Opus 5.5). I wrote no part of 1.7.5 and am new to it. Process: `../assurance-kit/process/friction-review.md`.

## Verdict and why

**SHIPPABLE.**
- The combined archive works as the owner asked. One account folder gone: the scheduled run, Run now and the command line skip it and back up the rest. Status warns for 14 days of backups, then keeps a quiet note. A missing group folder, a link, or a place outside the usual ones still stops the run, as before.
- Delete works as asked. The panel opens at once and the name can be typed during the check. **Delete when the check finishes** deletes with no second click when nothing needs a decision. Once sent, the panel says the delete can no longer be stopped.
- No cell is blocked. The costliest problems are words that send the person the wrong way:
  - the encrypted note's advice is refused (F1);
  - after Repair, the page still says "click Repair" (F2);
  - a removed account is named only by its folder (F3);
  - "add the account back" is said even when Thunderbird still has it (F4).
- One design point cuts into the delete fix (F5). Any backup, even one that saves nothing, makes a waiting plan stale. With Hourly backups, a plan waits under an hour.

**The builder's two points:**
1. **The guide's delete section does not mention "Delete when the check finishes".** Agreed, and it is wider than that (F6):
   - the "This archive" card still says Delete works "after showing what it would remove";
   - the README says the same;
   - X14's own amendment names only "Your archives".
   - Fix before release.
2. **All of the panel's words are drafts.** Weighed one by one (F7):
   - keep as they are: "Deleting this archive. It can no longer be stopped…", the tooltip, and the other-row refusal;
   - add one sentence to the checking line;
   - rewrite the armed line, the "found something" line, and the page's "changed" words.

## Findings, most costly first

Grades: **medium** means fix before the public release; **low** is the lead's call. None is high.

**F1 (medium, Type III). An encrypted archive's note tells the person to do something MailArchive refuses.**
- Cell: W3 (an encrypted "All of them" archive).
- The note, after 14 days of backups: "a mailbox this archive backed up has not been found since Nov 20, 2026; its mail already saved stays in the archive. Click Run now in MailArchive Desktop to see which one. This line stays while the archive's backup lists that mailbox; to end it, set the archive up again in the wizard, with the same archive folder, choose All of them, and answer Yes to keeping it current."
- What goes wrong:
  - The wizard refuses that folder: "…is an encrypted archive — it holds an encrypted archive's key file (.mailarchive-vault.json); refusing to archive into it without its key: open it in MailArchive with its password or recovery key".
  - "Make a new encrypted archive" needs an empty folder.
  - Settings cannot rebuild an encrypted job, because its record keeps no job.
  - So the note can never be ended, and its advice is a dead end. The design called this "recalled, not read; the friction walk checks it". It does not hold.
  - Also, "click Run now … to see which one": an encrypted archive's Run now is greyed out until it is unlocked, and nothing says so.
- Fix:
  - For encrypted archives, drop the wizard sentence. Say: "Unlock it on its Encryption card, then click Run now to see which one. This line stays while the archive's backup still lists that mailbox."
  - Backlog: the "Stop looking for it" button that design §5 deferred.

**F2 (medium, Type III). After Repair, the banner and status still say "click Repair" until the next backup runs.**
- Cell: W4 (an archive 1.7.4 auto-paused).
- Before Repair, the banner says: "This archive's source hasn't been reachable since Oct 12, 2026. If a mail account was removed, click Repair (or Run now) on its Scheduled backup card: this version skips a mailbox that is gone and backs up the others. Repair starts the schedule again; Run now backs up once. Otherwise reconnect the source (its mailbox or mail folder) to resume backups."
- Repair answers "Repaired.". The Settings card's "Backups stopped" line goes away. But the banner and status keep the same words word for word, because Repair does not clear the source-loss record.
- They stay until the next scheduled run: up to a day for Daily, a week for Weekly. The person thinks Repair failed and clicks it again.
- Fix: when this archive is repaired, the reply says: "Repaired. The next backup skips the missing mailbox; to back up now, click Run now." The banner should say the same until the next run, for example by showing "Repaired on DATE: the next backup skips it" when the scheduler entry is back.

**F3 (medium, Type III). An account removed on purpose is named only by its folder.**
- Cell: W1 (the owner, three Gmail accounts).
- Status says: "a mailbox this archive backs up is no longer on this computer: C:\Users\…\Thunderbird\Profiles\abcd1234.default-release\ImapMail\imap.gmail.com, not found since Nov 20, 2026. …"
- The Run now note and the wizard's summary show the same path.
- Removing the account also removes its name from Thunderbird's settings, so the lookup finds none. With three Google accounts in imap.gmail.com, imap.gmail-1.com and imap.gmail-2.com, the owner cannot tell which address this was. That is the problem 1.7.3 fixed in the list of mailboxes.
- The address is already on disk. The previous run's record keeps each account's name (`thunderbird[].name`), keyed by `store_id`. `store_id` is a hash of the profile and folder path, which can still be computed for a gone folder (`storeID`, `internal/app/tbcompleteness.go:99`).
- Fix: when the profile's settings no longer name the folder, take the name from the previous record's Thunderbird entry with the same `store_id`. Example: "qzsam.walker@gmail.com (C:\…\ImapMail\imap.gmail.com)".

**F4 (medium, Type III). "Add the account back" is said even when Thunderbird still has the account.**
- Cell: W2 (the folder vanished by accident).
- Status says "…qz-imap-qzb-example@example.com (…/ImapMail/imap.qzb.example), not found since Nov 20, 2026. … If you didn't remove it, add the account back in Thunderbird and the next backup finds it again." The summaries say "If you didn't remove it on purpose, add its account back in Thunderbird."
- A name is shown only when Thunderbird's settings still list an account for that folder, so the account is still there. The person looks in Thunderbird, finds the account, and is stuck.
- From memory, not checked here: Thunderbird rebuilds a listed IMAP account's folder when it starts.
- Fix: when NAME is known, say "Thunderbird still lists this account: open Thunderbird and let it download the account's mail; the next backup finds it again." Keep "add the account back" for a folder no account names.

**F5 (medium, Type II for the waiting time; Type III for the words). Any backup makes a waiting plan stale, even one that saves nothing.**
- Cells: W7, W6.
- Walked: a plan, then a backup that saved nothing (exported 0), then Delete permanently. The result was refused: "This archive changed since you clicked Delete. Click Delete again." That is followed by "Save this record now: MailArchive keeps no copy after you close this result."
- So with Hourly backups, a plan waits under an hour, not a day. With Daily, a plan left overnight goes stale.
- Three things go wrong:
  - The person sees "changed", but no mail came in.
  - "Click Delete again" shows while the page is already checking again by itself.
  - When an armed delete is refused this way (a backup ran during a long check), the page asks for the plan again un-armed. The person who walked away comes back to a plan waiting for a click.
- Fix:
  - Words (Type III): on the page, for this refusal, say "A backup ran after the check, so MailArchive is checking again below. Nothing was deleted; your typed name is kept."
  - Re-arming (Type II): when the refused delete was armed, re-arm the new panel. The server still checks `armable` and the typed name.
  - The deeper choice is the owner's, after an adversarial pass: either the re-check ignores manifest changes that are only run bookkeeping, or a backup skips an archive whose delete plan is waiting.

**F6 (medium, Type III; the builder's point 1). The guide and other words still describe the old delete.**
- Cell: M2 (a new person reading only the words).
- Guide step 1: "Click Delete…: nothing is deleted yet. Beside the archive, MailArchive shows what Delete would remove…". Step 2: "Type the folder's name exactly as it is shown, then click Delete permanently."
- Neither mentions:
  - that the check can take minutes;
  - that the name can be typed during it;
  - **Delete when the check finishes**;
  - that a plan waits a day;
  - that a delete can no longer be stopped once sent.
- The "This archive" card says "Delete… erases its files, after showing what it would remove". The README says the same. X14's amendment says "Delete… in Your archives", but the same button is on the This archive card.
- Fix. Guide steps:
  1. "Click Delete…: nothing is deleted yet. A panel opens beside the archive while MailArchive checks what Delete would remove; for a large archive this can take several minutes, and the panel counts them."
  2. "While it checks, type the folder's name. Then either wait and read the plan, or click Delete when the check finishes: MailArchive deletes as soon as the check ends, unless the plan holds something you should decide first, which it then shows you. Until the delete starts, Cancel or closing the page deletes nothing."
  3. "After reading a plan, type the folder's name and click Delete permanently. A plan waits up to a day, but a backup in the meantime makes MailArchive check again."
  4. "Once the delete starts it cannot be stopped: the panel says so and Cancel goes away."
  - The This archive card: "Delete… erases its files; you see what it would remove first unless you choose Delete when the check finishes."
  - X14's line: "Delete… on an archive's row (Your archives or This archive)".
  - Update the README to match.

**F7 (medium, Type III; the builder's point 2). The panel's draft words, weighed.**
- Cells: W6, W8.
- Checking line: "Checking what Delete would remove… This can take a few minutes for a large archive. Nothing is deleted yet." Keep it, and add "You can type the folder's name below meanwhile." Without that, a person waits for the check before typing, which loses the point.
- Armed line: "When the check finishes, MailArchive checks once more, then deletes this archive, unless the check finds something you should see first. Keep this page open."
  - "Checks once more" hides that the second check takes about as long as the first. A person who walks away for the check's length comes back to it still running.
  - It also does not say how to stop.
  - New words: "When the check finishes, MailArchive deletes this archive, unless the check finds something you should decide first. Just before deleting it checks the folder once more, which takes about as long again. Keep this page open; until the delete starts, Cancel or closing this page deletes nothing."
- "Found" line: "The check found something to look at before deleting. Read it, then click Delete permanently." It does not say what was found, and not every such line is highlighted (unrecorded copies and the key line are plain text).
  - New words: "Nothing was deleted: the plan holds something to decide first (REASON). Read it, then click Delete permanently."
  - REASON comes from a closed list the server sends beside `armable`: "mail copies this archive did not record", "a synced folder", "scheduled backups to check yourself", "files another program has open", "an earlier delete to finish", "an encrypted archive's key". MA-1476's key check then covers the new key.
  - When only the typed name did not match the folder's name as it is on disk (another case), say so.
- Keep as they are: "Deleting this archive. It can no longer be stopped. Keep this page open until its result shows here…", the tooltip, and "This delete was asked from another row's panel, so nothing was deleted. Click Delete… on the archive's own row." That last one is reached only through a slip in the page.

**F8 (low-medium, Type III). An encrypted archive's row offers "Delete when the check finishes", which can never apply.**
- Cell: W8.
- `armable` is always false for an encrypted archive, which is right. But the button is drawn for every row. An encrypted archive's owner who clicks it and walks away always comes back to the plan.
- Fix: the row already knows it is encrypted. Leave the button off, or show it greyed out with the tooltip "An encrypted archive's plan is always shown first, because it names its key."

**F9 (low-medium, Type III). Windows, Outlook: the wrong remedy for a moved data file.**
- Cell: W5.
- For "Documents\Outlook Files\archive 2019.pst" moved away: "…If you didn't remove it, add the account back in Outlook and the next backup finds it again."
- A .pst there is often an archive file the person moved to another drive. There is no account to add back. The mail in it is no longer backed up, and after 14 days only a quiet note says so.
- Fix: for a .pst, say "If you moved this file, put it back in FOLDER, or set the archive up again with its new place."

**F10 (low, Type III). Small problems with the Repair words (archives 1.7.4 stopped).**
- Cell: W4.
- The Settings card says "click Repair" without saying where. Repair is on the Scheduled backup card, and Save settings here also restarts the schedule.
- "This version skips…" reads like a release note. Better: "MailArchive now skips…".
- Status shows the old RED line "…FAILED: input PATH does not exist (check the path, or that the drive holding it is mounted)" right above the WARN that says a removed account is fine.
- The banner does not name the missing mailbox.
- Fix: "click Repair on the Scheduled backup card"; "MailArchive now skips a mailbox that is gone"; name the mailbox in the WARN and the banner. Where the helper says yes, drop the drive advice.

**F11 (low, Type II). Ending the note costs about ten windows and changes the schedule.**
- Cell: W1.
- "Set the archive up again in the wizard … answer Yes to keeping it current" means **Make a new archive…** (a name that does not suggest "set up again"). Then come the source, every IMAP question, the folder, the mode, the dates, the options, a full run, and the schedule question.
- That question offers only "every day at 2:00 AM" or "every week, Sunday at 3:00 AM". An Hourly or custom time set in Settings is replaced.
- Fix (backlog, design §5): a "Stop looking for it" button on the note. Meanwhile, name the button: "click Make a new archive…, choose All of them and this same archive folder…".

**F12 (low, Type III). The owner's ruled case says "other mailboxes" when only Local Folders is left.**
- Cell: M1.
- With one account plus Local Folders, the WARN says "…and the other mailboxes are still backed up." Only Local Folders remains, and it is often empty.
- Fix: when the only remaining listed input is Local Folders, say "and Local Folders is still backed up".

**F13 (low, Type III; read, not walked). A lost connection during an armed check ends in jargon.**
- Cell: W6.
- The page puts "Error: " plus the browser's own text (for example "TypeError: Failed to fetch") in the note, and closes the panel.
- This is likely when a laptop sleeps while the person walks away. The plan then waits a day with no page to show it.
- Fix: "The page lost touch with MailArchive during the check, so nothing was deleted. Click Delete… again."

**F14 (low, Type III). The command line's `-list` says "skipped" in a preview.**
- Cell: L2.
- `-list` prints "Not found, so skipped: PATH", but nothing ran.
- Fix: "Not found, would be skipped: PATH".

## The cells

The owner reads on a phone, so paths are shortened with "…". Every quoted line is copied from the walk's output or the code.

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| **W1. The owner on Windows.** Three Thunderbird accounts and Local Folders in one "All of them" archive (Windows-shaped place, APPDATA\Thunderbird\Profiles); imap.gmail.com removed on purpose. Run log: "Not found, so skipped: …\ImapMail\imap.gmail.com. It is no longer on this computer; the mail already saved from it stays in the archive." Status day 0 and day 13: the WARN with "from the first backup on or after Dec 4, 2026 this is only a note"; day 14: the note | Yes | 0 extra steps. The label is a folder path only, and the owner cannot tell which Gmail account it was. Ending the note costs about 10 windows and resets the schedule's time | III; II | F3; F11 |
| **W2. Windows, account folder vanished by accident**, the account still in Thunderbird. The WARN names "qz-imap-qzb-example@example.com (…)", then "add the account back in Thunderbird". A second backup keeps the first date | Yes | The remedy contradicts what the person sees in Thunderbird | III | F4 |
| **W3. Windows, ENCRYPTED "All of them" archive**, one account folder gone. The record holds only `mailbox_gone`, `mailbox_gone_since`, `mailbox_gone_count`. The WARN: "a mailbox this archive backs up has not been found since Nov 20, 2026, so it was skipped and the others were backed up; click Run now in MailArchive Desktop to see which one". After a failed run, the short form. Day 15: the note. The Run now note names the account | Partly | The note's remedy is refused by the wizard: a dead end, and the note can never be ended. "Click Run now" while Run now is greyed out (locked) | III | F1 |
| **W4. Windows, archive 1.7.4 auto-paused**, then 1.7.5 installed. Status, the banner (current and not current, scheduled) and the Settings card say Repair. Repair answers "Repaired." | Yes | After Repair, the banner and status still say "click Repair (or Run now)" until the next run. The Settings card does not say where Repair is. The RED line above gives drive advice | III | F2, F10 |
| **W5. Windows, an Outlook .pst** in Documents\Outlook Files moved away | Yes | "add the account back in Outlook" for a moved file | III | F9 |
| **W6. Windows, deleting a large archive and walking away during the check.** The panel opens at once with "Checking what Delete would remove…" and "Under a minute so far."; the name is typed; **Delete when the check finishes** shows "…checks once more, then deletes…"; at the end it sends, the line becomes "Deleting this archive. It can no longer be stopped…" with no Cancel, and the result shows "Deleted. 19 files (83.7 KB) removed. The folder is gone…" | Yes | The armed line does not say the second check takes as long again, or how to stop. A sleep that cuts the connection ends in "Error: …" (not walked) | III | F7, F13 |
| **W7. Plan, walk away, come back within 24 hours, Delete permanently.** With no backup in between: deletes. With a backup in between, even one that saved nothing: refused "This archive changed since you clicked Delete. Click Delete again.", a refusal record to save, and the page checks again with the name kept | Partly | One more full check, often two walks. "Changed" with no new mail. "Click Delete again" while it is already checking. With Hourly, a plan waits under an hour | II; III | F5 |
| **W8. Armed, but the plan needs a decision** (unrecorded copies, synced folder, files held, an encrypted archive) | Yes | "The check found something to look at before deleting…" does not say what. Encrypted rows offer a button that can never apply | III | F7, F8 |
| **W9. Cancel during the check; a panel closed for another; the page closed.** "Nothing was deleted."; the plan is discarded; after a discard, "That delete is no longer waiting — click Delete… again; nothing was deleted." | Yes | None | – | – |
| **M1. The owner on macOS, one account plus Local Folders (the owner's ruled case).** The account removed: backups go on, the WARN shows, no email. With ImapMail gone too: refused as before, "input … does not exist (check the path, or that the drive holding it is mounted)" | Yes | "the other mailboxes are still backed up" when only Local Folders is left | III | F12 |
| **M2. A new person reading only the words:** tooltip, This archive card, guide, README | Partly | The guide, the This archive card, the README and X14's line still describe the old delete | III | F6 |
| **L1. A Linux user answering "Keep this archive current automatically?"** OK with nothing selected gives "Nothing was chosen. Choose one line in the list, then click OK." and the question again; the daily row installs. Ten empty answers end with "No scheduled backup was set up: the list did not say which line was chosen. To set one up, open Settings in MailArchive Desktop, or run the wizard again." A transcoded "Yes ? every day at 2:00 AM" installs the daily schedule | Yes | One extra window, plain words | – | – |
| **L2. The command line on macOS/Linux:** `export -auto -input` with a gone input; the summary on screen names it, a log file has the path only; `-list` prints "Not found, so skipped: PATH" | Yes | "skipped" in a preview | III | F14 |

## Header

| | |
|---|---|
| Date | 2026-11-20, 20:40 to about 20:55 EST (clock read with `date`) |
| Reviewed commit | 43d4e4c (detached), diff from 304871c. Worktree `.claude/worktrees/r175-fric`, used by this review alone |
| Environment | macOS (Darwin 25.6.0), Go 1.26.0. Memory free 66 to 67% (`memory_pressure -Q`) before each run. No other `go test` running. One run at a time, each `-p 2 -count=1` with a `-run` filter |
| Method | Three throwaway test files, left untracked and never committed: `internal/app/zz_friction_175_test.go`, `internal/desktop/zz_friction_175_test.go`, `internal/wizard/zz_friction_175_test.go`. They drive the real run (`app.Run` on made-up Thunderbird profiles in temp folders, HOME set to a temp folder), real records, status (`health.Assess`), the Desktop's real routes and templates (`newDeleteEnv`, `newA3Rig`, MA-1479's `stoppedArchive` and `bannerOf`), and the wizard's question and summaries with the dialogs stood in. Invented addresses only (`qz…`, `.example`). The page script's behaviour was read from `deletescript.go` and MA-1477's Node scenarios in `harness.js`, not run in a browser. 14 or 13 days were made by rewriting the record's start time |
| Commands and exit status | `git log --oneline -3`, `git diff --stat 304871c 43d4e4c`, `git status --short` (twice), several `git diff 304871c 43d4e4c -- …`: 0. `date`: 0. `memory_pressure -Q` (five times): 0. `pgrep -fl "go test"`: 1 (none running). `go env GOTOOLCHAIN GOVERSION`: 0. `go test -p 2 -count=1 -run 'TestZZ175A' -v ./internal/app/`: 0, PASS. `go test -p 2 -count=1 -run 'TestZZ175D' -v ./internal/desktop/`: 0, PASS. `go test … -run 'TestZZ175W' -v ./internal/wizard/`: 1 (build failed: an unused import in my file), then fixed and run again: 0, PASS. `go test … -run 'TestZZ175D03' -v ./internal/desktop/`: 0, PASS. Read-only `grep`, `awk` and `cat`: 0, except one `grep` whose `--include=*.go` zsh would not expand (1, run again without it: 0) and two `grep`s naming files that do not exist (2) |
| Baseline digest | Not taken: the full suite was not run (the brief forbids it) |
| Read | `docs/design-1.7.5-fixes.md` (§0 to §5), X14 in `docs/ux-contract.md`, the whole diff's code: `autoplace`, `app/run.go` and `sourceloss.go`, `health/mailboxgone.go` and `health.go`, `state/lastrun.go` and `encrypted_record.go`, `desktop/archivedelete.go`, `deletescript.go`, `pending.go`, `dashboard.go`, `archives.go`, `settingscard.go`, `localschedule.go`, `help.go`, `wizard/wizard.go`, `picker.go`, `jobrun.go`, `addkeyed.go`, `cmd/mailarchive/main.go`, `app/encguard.go`, `third_party/zenity/list_unix.go`; the README's delete section; `docs/review-picker-names-friction.md` for this report's shape |
| Cells walked | 13 (W1 to W9, M1, M2, L1, L2) |
| Functioning | 10 fully, 3 partly (W3, W7, M2), 0 not |
| High-friction cells | 3: W3, W4, W7 |
| Not walked | A real browser: the panel's timing, a laptop sleeping during the check, `pagehide` sending its keepalive. Real dialogs: the wizard's set-up-again path in F11 was read in the code. Windows itself: real `C:\` paths, case folding, junctions, Outlook removing an account, and how long paths wrap in the wizard's summary window; the rule's Windows behaviour is the Windows lab's (design §4 step 4). Whether Thunderbird rebuilds a listed account's folder (F4) and whether it removes ImapMail with its last IMAP account: recalled, not checked. The `-list` output: read, not run |

## Method notes (Limen counters)

Worked on 2026-11-20 (clock read). Register: a finding, the review's own judgement; quoted product words are copied from the walk's printed output or the code.

What I did not check, per claim:
- F4's Thunderbird behaviour is from memory.
- F3's fix (taking the name from the last record) was read in the code, not built.
- F13 was read, not run in a browser.
- Windows behaviour was not run; paths were made Windows-shaped on the Mac.

The smallest checks that would settle these: the Windows lab's "All of them" item, plus one Thunderbird start after deleting a listed account's folder.

Counters:
- Borrowed lines: only the product's quoted words.
- Drift: none noticed.
- Touched: three untracked test files in the worktree, nothing else.
- Persona: none.
- Sources reached: all files listed under "Read".
