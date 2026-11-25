# Friction review: per-mailbox archives

- **Under review:** branch `per-mailbox-archives` at `443c2777`, against main `10fcc2e`.
- **Process:** `../assurance-kit/process/friction-review.md`. One independent walker, 2026-11-25
  03:01 to 03:17 EST, 17 cells (Windows weighed first), real words driven from the code by
  throwaway tests with every dialog replaced.
- **Verdict:** SHIPPABLE WITH EDITS. 12 cells work, 5 partly (W4, W5, W8, W11, N1), none fail;
  F1 to F14, all Type III words except F1's optional button.

## The lead's fold (2026-11-25)

All of F1 to F14 are built in the adversarial pass's round B
(`docs/review-per-mailbox-archives-adversarial.md`), with the walker's proposed words unless a
note below says otherwise:
- **F1:** the words (the summary's and end note's last line, and the "already holds" question's
  added sentence). The third button ("One archive for each mailbox here") is not added now; the
  words carry the way.
- **F3 (lead's call on the open point):** an existing combined archive whose job lists a folder no
  set archive covers (for example a removed account's) gets no Pause advice; its line names that
  folder and says its mail stays in that archive.
- **F8:** the "already holds" question is not shown in "Make a new encrypted archive…".
- **F9:** the "could not confirm" refusal names the archive and gives each cause its own remedy.
- The README and the Desktop guide gain the feature (F5).

## Appendix (verbatim; home-folder paths written as "~" for a public repository)

### Appendix A. The walker's report

I'm filing this as **SHIPPABLE WITH EDITS**. The owner's complaint is answered, nothing in the design blocks shipping, and 14 word fixes should land before release. The costliest problem is that an account added later stays unprotected until the owner remembers a choice that isn't preselected (F1).

### Friction review: One archive for each mailbox

Reviewer: an independent Claude agent (Opus 5.5). I wrote no part of this. I worked on 2026-11-25, from 03:01 to 03:17 EST (clock read with `date`). Commit reviewed: 443c2777, compared with 10fcc2e.

#### Verdict: SHIPPABLE WITH EDITS

- **The complaint is answered.** After "All of them", the owner can choose "One archive for each mailbox":
  - Each Gmail account and Local Folders gets its own archive and its own schedule, 10 minutes apart.
  - An empty feeds account is left out, and the plan says so.
  - The plan shows everything before anything is written.
  - Running it again into the same folder adds a new account and brings the others up to date. Their schedules are kept.
- **Every cell works, five only partly.** What goes wrong is mostly words that send the person the wrong way.
- **F1 is the one silent gap.** It only goes away if the person remembers a choice that is not preselected.
- **Almost every finding is a missing or wrong word (Type III), so they get built before release.** The other points are the lead's or the owner's call.
- **A correction.** I first suspected the parent-folder check would refuse any folder near a large archive. I measured it:
  - An archive keeps one folder per mail folder, not one per message.
  - The check's limit is 100,000 folders, so it is not reached in practice.
  - What is left is a wording problem (F9).

#### Findings, most costly first

**Medium** means fix before release. **Low** means the lead's call. Paths are shortened with "…".

**F1 (medium; Type III for the words, Type II for a button). Adding an account later depends on memory, and the default path leads to a dead end.**
- Cells: W3, W4.
- The set's question says it once: "A mailbox you add to Thunderbird later is not backed up until you run this again, choose One archive for each mailbox, and choose the same folder." Nothing repeats it: not the summary, the end note, the README or the guide.
- A month later the owner keeps the preselected "One archive for all of them" and picks the set's folder.
  - They answer four download questions first.
  - Then they see: "D:\Mail archives already holds 3 archives. An archive made here would hold them inside it, and making one archive for each mailbox in this folder again would then be refused. Choose another folder?"
  - "Choose another folder" reopens the folder window. Cancel ends the wizard. Nothing says how to reach the set's path.
  - If they never try again, the new account is never backed up, and nothing warns them.
- Fix:
  - Last line of the summary and of the end note: "To back up a mailbox you add to Thunderbird later: click Make a new archive…, choose All of them, then One archive for each mailbox, and this same folder, D:\Mail archives."
  - Add to the "already holds" question: "To add a new mailbox to these archives instead, click Choose another folder, then Cancel, and start again: All of them, One archive for each mailbox, and this folder."
  - Lead's call (Type II): a third button, "One archive for each mailbox here".

**F2 (medium, Type III). When the archives' folder can't be written to, the summary says mail was kept, and the remedy is lost.**
- Cell: W8. This is likely on Windows when Controlled folder access protects Documents. Walked on the Mac with the folder made read-only.
- The folder check writes nothing, so it passes. After nine windows, making the first archive folder fails.
- The summary says: "qzfamily.walker@gmail.com: Did not finish: MailArchive could not make a folder in D:\Mail archives: this computer did not let it write there. The mail copied so far is kept; run this again with the same folder to finish it."
- What's wrong:
  - Nothing was copied.
  - The same folder will fail again.
  - The wizard's own refusal had the fix ("…allow MailArchive to write there (Windows: Windows Security, Controlled folder access; …)"). The summary cuts it off.
  - The Desktop note also says "choose the same folder".
- Fix, when the folder was never made: "Not made: this computer did not let MailArchive make a folder in D:\Mail archives, so nothing was copied. Allow MailArchive there (Windows Security, Controlled folder access), or run this again and choose another folder." Keep "the mail copied so far is kept" only when the folder was made.

**F3 (medium, Type III). The end note says to keep an old combined archive running when every mailbox already has its own backup.**
- Cell: W5. This is the owner's own case: the old "All of them" job still lists the folder of an account removed before 1.7.5.
- After Yes: "Your archive …\Mail before also backs up these mailboxes, all together. Keep it running until each of these mailboxes has its own scheduled backup."
- All four had just been scheduled. The real reason is the missing folder, which no set can ever cover. So the owner backs up twice, forever, with two lots of failure emails.
- The same problem after another archive failed: "…\qzfamily.walker@gmail.com also backs up qzfamily.walker@gmail.com. Keep it running until each of these mailboxes has its own scheduled backup." qzfamily was scheduled; a different archive had failed.
- Fix: name the real reason.
  - Missing folder: "It also lists FOLDER, which is not on this computer now; its mail already saved stays in it. To stop backing up the others twice, click Pause (make read-only) on its row in MailArchive Desktop."
  - Whether a missing folder may get the Pause advice is for the security review.
  - A member that failed: "Keep it running until NAME finishes; then run this again into the same folder."

**F4 (medium, Type III). "Run this again with the same folder" doesn't say how.**
- Cells: W6, W7.
- The summary says "Did not finish: the export was cancelled. The mail copied so far is kept; run this again with the same folder to finish it." and "Not started. Run this again with the same folder to back it up."
- Running again with the default answers leads into F1's dead end. The Desktop's note already says it right: "choose One archive for each mailbox and choose the same folder, D:\Mail archives."
- Fix: one closing sentence: "To finish them, run this again: choose All of them, then One archive for each mailbox, and this same folder." Then each line can just say "Did not finish: the export was cancelled. The mail copied so far is kept." or "Not started."

**F5 (medium, Type III). The README and the Desktop guide don't describe the feature, and the guide is wrong for a set.**
- Cell: N1.
- The README's wizard steps (lines 153 to 179) have no question, no archives folder, no plan and no set summary.
- The guide (help.go line 190) says:
  - "For Thunderbird mail the wizard then asks Sync Thunderbird before each scheduled backup?" A set is never asked this.
  - "this page offers the new archive at the top". A set makes several.
- Fix:
  - Add a step to the README and a sentence to the guide: the choice, the one folder that holds them, the plan, one summary, one schedule question, and no Thunderbird sync for a set.
  - Also add F1's line about accounts added later.

**F6 (low to medium, Type III). Desktop: "they are now in Your archives", but the list on screen doesn't show them.**
- Cell: D1/W6.
- The note says "1 archive added — it is now in Your archives. qzsam.walker@gmail.com did not finish: …"
- The page only reloads when everything finished. So the new rows appear only after a reload. Today's one-archive note says "…once you reload this page".
- Fix: in every set note that does not reload, say "listed under Your archives once you reload this page".

**F7 (low to medium, Type III). A row gives the reason, then sends the person to Switch to find it.**
- Cell: W10.
- The row says: "Thunderbird no longer has the account this archive backs up (since Nov 20, 2026). The mail saved so far stays here. To stop its backups, click Pause (make read-only)."
- Under it: "Last backup did not finish (Nov 20): switch to it to see why." The same happens for a moved account.
- Fix: drop the "did not finish" line when the source-loss words are shown.

**F8 (low, Type III). Make a new encrypted archive… shows the "already holds" question, which doesn't apply to it.**
- Cell: W11.
- After three download questions and choosing the set's folder, the question talks about "one archive for each mailbox", which this flow never offers.
- "Use this folder anyway" then fails: "Could not make an encrypted archive: an encrypted archive is made in a new or empty folder: … holds qzsam.walker@gmail.com — choose a new or empty folder …". The wizard ends and the answers are lost.
- Fix: don't show this question in the encrypted flow. Its empty-folder check comes right after and refuses any non-empty folder anyway.

**F9 (low, Type III). The words for "couldn't confirm the folder is outside every archive".**
- Cell: P1.
- The wizard says: "…it, or an archive this computer knows, is on a network share or another kind of mount, or an archive is too large or partly unreadable to check. Nothing was made. Choose a folder on this computer's own drive."
- I tested it with the walk limit lowered to 20 folders. Two cases refused:
  - a known archive beside the chosen folder;
  - on a re-run, one of the set's own archives inside it.
- In both cases the folder already is on this computer's drive, so the advice is wrong. The decrypted copy's own words name the archive and say "Choose a folder on another drive; if that archive is no longer used, remove it from Your archives".
- It's rare: 20,000 folders took 0.48 s to check on this Mac.
- Fix: name the archive and give each cause its own remedy.

**F10 (low, Type III).** After Cancel, the end note says "Not scheduled: qzsam.work@gmail.com, which did not finish." That archive was never started. Fix: "…, which was not started."

**F11 (low, Type III). The plan's words.**
- "Asked about a full offline download: a, b, c" sounds like it already happened. Better: "Next you are asked whether to prepare a full offline download of: …".
- The button says "Make the archives" even when the plan says "No new archive: 2 brought up to date". Better: "Bring them up to date".
- When nothing runs at all, the summary still says "No archive was finished in …" plus the closing sentences. Better: skip it.
- "In this folder, not found now: NAME" gives no advice. Add: "its mail stays in its archive; click Pause on its row to stop its backups."

**F12 (low, Type III). Words for an account that moved or was removed.**
- The status and the row's banner for a moved account drop the second option the run's own error gives ("…or choose One archive for each mailbox and the same folder as before"). Choosing the account on its own makes an archive outside the set.
- The emails say "Its mail stays here." and "click Pause (make read-only) on its row." In an email, "here" and "its row" point nowhere. Say "in the archive" and "on its row under Your archives in MailArchive Desktop".
- The moved-account stop is about 700 characters on Windows, because it repeats four long paths. It works, but it's long.

**F13 (low, Type III). The Settings card's sync refusal doesn't say where to go.**
- It says: "Only one archive can sync a profile for now: switch it off there first." Better: "switch it off on that archive's Settings card first (Switch to this archive on its row)."
- The unreadable-settings refusal says "Repair that archive" without saying where.

**F14 (low, Type III). The download questions' order and count.**
- On the one-archive path the questions come in folder order (qzsam.work, then qzfamily, then qzsam.walker). The list shows A to Z.
- Picking one account gives "Account 1 of 1:".
- Fix: ask in the list's order, and drop the count when there is only one.

**Design points noted, not counted against the design (Type II; the owner's rulings or the design's own later work):**
- "One archive for all of them" stays preselected (the owner's answer 2), so clicking through still gives one combined archive.
- A set has no Thunderbird sync, and each "Yes, prepare" quits and restarts Thunderbird once per account.
- "Switch to it to see why" changes which archive is current.
- The progress window doesn't name the account.

#### The cells

Cells are ordered Windows first. Windows cells were walked on macOS with Windows-style paths passed in.

| Cell (actor × scenario) | Functions? | Friction | Type | Fix |
|---|---|---|---|---|
| **W1** The owner (3 Gmail on one server, Local Folders, empty feeds): Make a new archive…, All of them, One archive for each mailbox, No to the downloads, Yes daily | Yes | 20 windows, about 6 more than one combined archive. Plan, summary, schedule (02:00 to 02:30) and end note are clear | III | F11 |
| **W2** Someone who clicks OK through everything | Yes | Gets today's one combined archive. In the "already holds" question, Enter picks the safe choice | – | the owner's ruling |
| **W3** The owner a month later, 4th account, remembers, same folder | Yes | 18 windows. "will make 1 archive", 4 "brought up to date", "Account 1 of 1", re-run schedule words, 4 "kept its own schedule" | III | F1, F14 |
| **W4** The same owner, forgets (keeps the default) | Partly | 4 download questions, then the "already holds" question with no way forward named. The new account is never backed up | III; II | F1 |
| **W5** Has an existing "All of them" archive whose job lists a removed account's folder | Partly | "Keep it running…" although every mailbox is scheduled | III | F3 |
| **W6** Cancel on the window "Exporting 2 of 4…" | Yes | "run this again" with no how; "did not finish" for an archive never started; Desktop note with no reload | III | F4, F6, F10 |
| **W7** One account's folder gone after the plan | Yes | The others go on; its line gives the reason and the log | III | F4 |
| **W8** The archives' folder can't be written to (Controlled folder access) | Partly | 9 windows answered, then a false "mail copied so far is kept"; the remedy is cut | III | F2 |
| **W9** Account removed (folder kept), then added again | Yes | The old archive stops with long words. The re-run makes "qzsam.work@gmail.com (2)" and advises pausing the old one | III | F12 |
| **W10** Account removed for good, 3 days or more | Yes | The row gives the reason and also "switch to it to see why"; the emails say "here" | III | F7, F12 |
| **W11** Someone with an encrypted archive: Make a new encrypted archive…, picks the set's folder | Partly | Correctly never asked the new question. Meets the "already holds" question, then is refused and loses the answers | III | F8 |
| **L1** Linux user: empty answers to the new question and to the schedule | Yes | 10 lists and 9 notes, then a plain ending. Missed times are said to be skipped | – | – |
| **M1** macOS: the same flow, with the wake sentence | Yes | Nothing new seen | – | – |
| **N1** A new person reading only the words (question, README, guide) | Partly | README and guide say nothing; the guide's sync line is wrong for a set | III | F5 |
| **D1** Desktop notes: all finished (page reloads), one, error afterwards, list not writable | Yes | The notes that don't reload don't say to reload | III | F6 |
| **D2** Settings card: switching sync on for a second archive of one profile | Yes | "switch it off there": where? | III | F13 |
| **P1** Archives folder refusals (an archive, an encrypted archive, the settings folder), plus the "couldn't confirm" test | Yes | Each refusal asks again; the "couldn't confirm" remedy is wrong | III | F9 |

Totals: 17 cells. 12 fully work, 5 partly (W4, W5, W8, W11, N1), none fail. High-friction cells: W4 and W8.

#### Header

| | |
|---|---|
| Date | 2026-11-25, 03:01 to 03:17 EST |
| Commit | 443c2777 (detached), diff from 10fcc2e. Worktree `~/repositories/mail-archive-tool/.claude/worktrees/pma-fric`, used by this review alone |
| Environment | macOS (Darwin 25.6.0), Go 1.26.0. Free memory was 62–63% before each run (`memory_pressure -Q`, 10 times). One `go test` at a time, each `-p 2 -count=1` with `-run` limited to my own tests. No Docker, no real dialog, no browser, no notification |
| Method | Three throwaway test files, untracked and never committed: `~/repositories/mail-archive-tool/.claude/worktrees/pma-fric/internal/wizard/zz_friction_pma_test.go`, `~/repositories/mail-archive-tool/.claude/worktrees/pma-fric/internal/app/zz_friction_pma_test.go`, `~/repositories/mail-archive-tool/.claude/worktrees/pma-fric/internal/desktop/zz_friction_pma_test.go`. They drive the real code with every window stood in (a recorder modelled on the branch's own test helper; the branch's scheduler stand-in; the Desktop's add seam). They use made-up Thunderbird profiles under a temporary home folder and invented addresses only (`qz…@gmail.com`, `.example`). The exports really ran |
| Commands and exit status | `git status`, `diff`, `grep` (many): 0. Three `git grep … -A` calls: 128 (my wrong syntax), redone with `grep`: 0. One `grep --include`: 1 (zsh would not expand it). One pointless `git diff --no-index /dev/null /dev/null` sent by mistake: 0. `date` (twice), `go env`: 0. Wizard tests: `-run 'TestZZPMA0[1-48]'`, `TestZZPMA05`, `'TestZZPMA0[67]'`, `TestZZPMA09`: all 0, PASS. App tests: `-run TestZZPMA_A0`: 0, but the folder-check part proved nothing, because the app tests' setup empties the list of known archives; then `TestZZPMA_A01` three times (to debug, with the known list stood in, with timing): 0. Desktop tests: `-run TestZZPMA_D0`: 0 |
| Read | The kit's `process/friction-review.md`; `docs/review-1.7.5-friction.md`; the design, sections 0 to 17; the code: the wizard's `set*.go`, `addkeyed.go`, and its `picker.go`, `wizard.go` and `listtext.go` changes; the app's `setparent.go`, parts of `decryptplan.go` and `copyexport_dest.go`, and its `run.go` and `sourceloss.go` changes; the `autoplace.go` changes; health's `sourcereason.go` and `health.go` changes; the Desktop's `addarchive.go`, `archives.go` changes, `syncshared.go`, the page's add-archive script, `help.go` lines 190 and 205; alert email words; archindex `Stores`; the export's folder layout; README lines 145 to 179; X6 and X14 in `docs/ux-contract.md`; the branch's tests MA-1490, 1493, 1494, 1496, 1497, 1499, 1505, 1506. Catalog rows MA-1490 to 1508: skimmed (with grep and the design's §15), not read one by one |
| Not walked | Windows itself: real `C:\` paths, how wide the windows are (the plan at 150 %, the 87-character folder-window title, the 475-character question), button order, Controlled folder access actually blocking, Task Scheduler entries, the batch wrapper with address-named folders. Real dialogs. A real browser: the reload behaviour was read from the page's script, not run. A real Thunderbird: "Yes, prepare", and whether removing an account keeps its folder. The Background Items notices on macOS. The real wording of the plan's "a delete did not finish" line |

#### Method notes (Limen counters)

Worked on 2026-11-25 (clock read). Register: a finding, the review's own judgement. Product wording is quoted from the walk's output or the code.

What I did not check, claim by claim:
- **F2:** that Controlled folder access refuses the folder is inferred from the code and the macOS read-only walk, not seen on Windows.
- **F9:** I lowered the walk limit to make it happen. How rare it is comes from the archive layout and one timing on this Mac.
- **F6:** read from the page's script, not run.
- **W9:** what Thunderbird does with a removed account's folder is from memory.

The smallest checks that would settle these: in the Windows lab (MA-1503), one set with Controlled folder access on, and one re-run into the set's folder keeping the default answer.

Counters:
- Borrowed lines: only the product's quoted words.
- Drift: one correction kept on the record (F9 lowered after measuring).
- Touched: the three untracked test files, nothing else.
- Persona: none.
- Sources reached: everything listed under "Read".

