# Adversarial pass: per-mailbox archives

- **Under review:** branch `per-mailbox-archives` at `443c2777` (all four build steps), against main
  `10fcc2e` (release 1.7.5). Design: `docs/design-per-mailbox-archives.md`, revision 2.2.
- **Process:** `../assurance-kit/process/adversarial-review.md`. Three read-only finders (OUT: the
  outside aggressor and names into paths; INS: the insider and local file tricks; CON: integrity,
  time, crashes, concurrency), then one skeptic who reproduced what it could on macOS with
  throwaway tests. Run 2026-11-25, 03:10 to 03:31 EST, by the lead session's subagents.
- **Skeptic's verdict:** NOT READY. 24 findings (OUT-01..05, INS-01..08, CON-01..11): 16 confirmed
  by a red test, 7 by the code or the branch's own tests, 1 refuted (OUT-05); 1 HIGH, 3 MEDIUM.
- The friction review ran beside it and is filed as `docs/review-per-mailbox-archives-friction.md`.

## The lead's fold (2026-11-25)

Every confirmed finding is fixed before release, in two builder rounds, then re-verified by a fresh
agent. No owner question arises: each fix keeps the owner's rulings (§0 of the design), and the two
lead calls below are recorded.

**Round B (wizard, Desktop, words; first):**
- **CON-01 (HIGH).** A continued archive counts as scheduled only when the scheduler says its own
  entry is installed and its record is not "Run now only". Otherwise its end-note line says it has
  its own settings but no scheduled backup now ("click Repair on its row"), it is never installed
  over (C6 kept), and no Pause advice is given for any archive it covers. Twins: install failed,
  auto-paused, Run now only.
- **INS-01 (with CON-02) and INS-02.** On Windows each new set folder is restricted to its owner
  right after it is made (`winsec.RestrictFolderToOwner`, as other MailArchive-made folders are).
  The plan names every child it did not look at, so a "(2)" is never unexplained. Each child's
  identity from the plan is checked again through the parent's handle right before the log opens
  and the export starts; a changed child fails that archive, naming the folder, and nothing is
  written.
- **CON-03.** A lock beside the made-list (never in the parent) is held for the whole set run; a
  second window says another window is making archives there.
- **OUT-03 (= CON-07), OUT-04.** Each made-list line also keeps the mailbox's store id; an
  unfinished folder is continued only for that store. Names and the parent are compared in
  composed form (NFC) and the parent by identity.
- **CON-04, CON-08, CON-10, INS-07, INS-08.** The row's line shows for a "running" record whose
  archive lock is free; the Desktop's lead counts registered archives; "the mail copied so far is
  kept" only when the folder was made; a made-list line is struck as gone only for a folder that
  was once listed; an unreadable made-list names its file and the remedy.
- **OUT-01.** The encoded Subject is folded into lines of at most 78 octets.
- **OUT-02 (lead's call).** A folder name ending in a macOS package extension (`.app`, `.bundle`,
  `.pkg`, `.rtfd`, `.framework`, `.plugin`, `.kext`) or in `.{GUID}` gets " (mail)" added, so the
  folder still reads as the address and opens as a folder. This keeps the owner's ruling 3.
- **CI's two test-only failures at `443c2777`:** MA-1498's test restores a seam while a stalled look
  still reads it (Linux race step); MA-1499's test searches its own source for "\n}\n", which a
  Windows checkout writes with "\r\n".
- **The friction review's F1 to F14** (its own record says what each changes).

**Round A (the engine's account identity; second):**
- **INS-03, INS-04, INS-05, INS-06 (one cause): a folder's account is known only by its shown name.**
  A plaintext record also keeps, per Thunderbird folder, a fixed identity of its account: a hash of
  the account's type, user name and server, never the name itself. "Moved" (C2, F-08), "removed"
  (C13) and a new third case, "this folder now belongs to a different account" (INS-03: stop,
  with its own words and `source_reason: reused`), are decided by that identity; the shown name is
  kept only for the words. A record from before this release, with no identity, keeps today's
  rules.
- **INS-04 (lead's call):** with the identity, a re-added account under a different shown name is
  found and its old archive stops with the moved words. This changes the expectation of one pinned
  positive twin (`internal/app/ma1504_one_folder_stop_test.go:166-169`: "no live account with the
  recorded name" must not stop). The change is a stop where there was silence, in the direction of
  the owner's Q7 ("Stop the backup") for moved accounts; nothing is weakened, and the twin is
  restated beside its old text with the identity case.
- **CON-06.** Before continuing a child, the plan asks whether its folder has no live owner and its
  account moved; if so it plans "kept as it is … click Pause", never "run this again".

## Re-verify of rounds A and B (2026-11-25), and the lead's fold

A fresh agent re-verified both rounds at `ce5b6fbd` (appendix E): NOT READY. Of the 23 confirmed
findings, 19 closed, 2 partly (OUT-02, INS-01), 2 not folded (CON-09, CON-11, both INFO); of F1 to
F14, 12 closed, 2 partly (F2, F12). New: RV-01 (MEDIUM, by test), RV-02 (MEDIUM, Windows mechanism),
RV-03 and RV-04 (LOW, by test), RV-05 (the lead's call), RV-06 to RV-08 (INFO); RF-01 to RF-09.

The lead's fold, built as round C, then a narrow re-check, then the Windows lab:
- **RV-01:** a set's plan never continues a child whose run would stop as reused (the folder now
  belongs to another account): the row gets a new archive, and the old one is listed "kept as it
  is … another account uses its folder now; click Pause (make read-only) on its row under Your
  archives in MailArchive Desktop".
- **RV-02:** on a Windows volume without persistent access lists (FAT, exFAT), a child is judged by
  "a real folder, not a link", not by its owner, and the plan's "another account owns or can change"
  line is not used there.
- **RV-03:** the summary names each member with no scheduled backup now whenever any has that state.
- **RV-04:** `.download` joins the package endings; the builder checks other candidates with `mdls`
  and adds those macOS treats as packages.
- **RV-05 (the lead's call): keep the owner-only folders on Windows.** An archive readable only by
  the person who made it is the safer default on a shared computer; handing one over works by
  copying it. The release notes say so.
- **RV-06:** no change; a user-name edit or a re-add with other capital letters is a loud stop,
  never a silent one.
- **RV-07:** the design's §14 gains "encrypted one-folder archives keep no account identity, so a
  folder reused by another account is not caught for them" (the owner's privacy rulings).
- **RV-08:** CON-09 and CON-11 stay as INFO: a second tab at the exact moment of a fold, and the
  instant between making a folder and recording it; neither loses mail.
- **F3 / RF-08 (the lead's call, revised):** a folder that is not on this computer is not a live
  mailbox, so pausing the combined archive cannot stop a live backup of it. When every live input
  of the combined archive is covered by a scheduled set archive, its line names the missing folder,
  says its mail stays in that archive, and gives the Pause advice.
- **RF-01 to RF-07, RF-09:** the re-verifier's proposed words.

## Appendices (verbatim; home-folder paths written as "~" for a public repository)

### Appendix A. Finder OUT

**Finder report, lens OUT (outside aggressor)**

- **Date:** 2026-11-25, from the session clock.
- **Commit:** 443c2777, in the read-only worktree `.claude/worktrees/pma-adv`.
- **What I read:**
  - the brief and `assurance-kit/process/adversarial-review.md`;
  - the design, §0 to §11;
  - the full diff list, then these files in the worktree: `internal/wizard/` (`set.go`, `setexport.go`, `setplan.go`, `setwindow.go`, `setparent.go`, `setschedule.go`, `setexisting.go`, `setchild_unix.go`, `setchild_windows.go`, and the diffs of `picker.go`, `listtext.go`, `wizard.go`);
  - `internal/app/` (`setparent.go`, `copyexport_folder.go`, `deleteguard.go`, and the diffs of `run.go`, `sourceloss.go`, `decryptplan.go`);
  - `internal/job/made.go`, `internal/alert/alert.go` and its MA-1508 test, `internal/schedule/wrapper.go`, and `internal/schedule/schedule.go` (Validate, cron, plist, launchd install);
  - `internal/util/paths.go`, `state.CleanFolderName` and `state.TBTextKey`, `internal/health/sourcereason.go`, `internal/archindex/index.go` (Stores, Reconcile);
  - `internal/desktop/` (`addarchive.go`, `syncshared.go`, and the diffs of `archives.go` and `dashboard.go`);
  - the diffs of `internal/thunderbird/thunderbird.go` and `internal/autoplace/autoplace.go`;
  - `third_party/zenity/` message dialogs for Linux, Windows and macOS.
- **What I did not read:**
  - the pre-design review, and the design's §12 to §17;
  - test files, apart from the MA-1508 test;
  - `schedule/xml.go` (the Windows task XML), `openPath`, `tbcomplete`, and the zenity list and progress code.
- **Ran nothing**, as the brief says.

**Verdict:** nothing HIGH or MEDIUM in this lens. Every sink I followed cleans or escapes the name. Four LOW findings and one INFO.

---

**OUT-01 (LOW): the new encoded alert Subject is one long, unfolded line**
- **Where:** `internal/alert/alert.go:365-372`. `subjectHeader` returns `mime.QEncoding.Encode` output. Go joins the encoded words with a plain space and never folds the line.
- **Why it grows:** each byte outside ASCII becomes three characters, and every 63 characters of text add 13 more. A folder name may be 60 four-byte characters, such as emoji, which `CleanFolderName` keeps.
- **Sequence:**
  1. An account name of 60 emoji, set in prefs.js.
  2. A parent folder of about 100 characters.
  3. A failed backup.
- **Result:** the Subject line is about 1,040 characters. The email standard (RFC 5322 §2.1.1) caps a line at 998. Before this change the same subject was raw UTF-8, about 350 bytes.
- **A smaller break every time:** today's "Thunderbird still downloading" subject, which holds a dash outside ASCII, now goes out as one line of about 95 characters. The encoding standard (RFC 2047 §2) caps such a line at 76.
- **For the person:** a strict mail server may refuse the alert, so a failing backup is not reported. I did not check which servers refuse; that needs a real send.
- **Red test:** `Message()` for that event has every header line at or under 998 octets. Ideally each line is at or under 78, folded as CRLF plus a space between `?=` and `=?`.

**OUT-02 (LOW): a folder name can carry an ending the file manager treats specially**
- **Where:** `internal/wizard/set.go:201-235`. `setSafeName` cleans separators, reserved names and dots, but not "shell" endings.
- **Windows sequence:** a Thunderbird account named `Work.{645FF040-5081-101B-9F08-00AA002F954E}`. The braces, dot and dashes all pass, and Thunderbird rows get no suffix. The folder is made with that name.
  - File Explorer shows it as the Recycle Bin and opens the Recycle Bin.
  - The summary's advice, "Open index.html in any of them", fails from Explorer.
- **macOS sequence:** a name ending in `.app`, `.pkg`, `.bundle` or `.rtfd`. Finder shows the archive as an app or document package that will not open.
- **For the person:** the archive looks gone or broken. The mail is still on disk and listed in Desktop. No data is lost.
- **Red test:** `setSafeName` of `x.{645FF040-5081-101B-9F08-00AA002F954E}` and of `x.app` gives a name without a `.{GUID}` ending and without a package ending.

**OUT-03 (LOW): an unfinished folder is matched by the account's name only, and prefs.js controls that name**
- **Where:** `internal/wizard/setplan.go:344` compares `made[c.name] == r.name`. The made-list records only the name: `setparent.go:152` and `setexport.go:307`. This is what §7 (condition C5) says. The finding is that a name is not a stable identity. The store id (`state.PathSourceID`) is known at record time and is not recorded.
- **Sequence:**
  1. A set into `D:\Mail`. Account A ("Work") gets `D:\Mail\Work`.
  2. A's first export dies before its first manifest save. Up to 999 messages can be written before that save (`defaultCheckpointEvery`, `run.go:1306`).
  3. A is renamed "Work (old)" and B is renamed "Work", by the person or by a synced or shared profile.
  4. Run the set again. B matches the unfinished `Work` folder and is exported into it.
- **For the person:** B's archive folder may hold A's partial mail, which is not in B's manifest. The summary says each archive can be handed over on its own, so handing over B's folder also hands over part of A's mail. A reproduction must check whether those files survive B's run.
- **Red test:** a made-list line (`D:\Mail\Work`, "Work"), a child `Work` with no manifest and one planted message file, and a row named "Work" whose store is different. Its outcome must be a new folder, not "unfinished".

**OUT-04 (LOW): on an HFS+ drive, an unfinished folder is never continued when its name has an accented letter**
- **Where:** `internal/wizard/setplan.go:250` keys the made-list by the composed spelling. Names are NFC, enforced by `folderNameWhy` through `SanitizeSegment`. HFS+ ("Mac OS Extended") stores and lists names decomposed, so the byte compare at `:344` misses.
- **Sequence:**
  1. The parent folder is on an HFS+ volume.
  2. An account named `josé@x.com`.
  3. Its first export dies before the manifest save.
  4. Run the set again.
- **Result:** the plan makes a new `josé@x.com (2)`. The unfinished `josé@x.com` is left forever, and its made-list line is never removed (`archives.go` `strikeFolded` keeps a folder with no manifest).
- **For the person:** a duplicate partial folder. No loss.
- **Red test:** a child named in decomposed form plus a made-list line in composed form gives "unfinished". The fix is to compare by identity, or by `state.TBTextKey`.

**OUT-05 (INFO): the macOS scheduled-task file escapes only five characters**
- **Where:** `internal/schedule/schedule.go:1367-1370`.
- **Gap:** `CleanFolderName` does not remove the noncharacters U+FFFE and U+FFFF. An account name holding one puts it into the LaunchAgent's `StandardErrorPath` (line 375), which is not valid XML.
- **Unknown:** I did not check whether launchd accepts such a file. `installLaunchd` trusts `launchctl load`'s exit status (line 1119).
- **Skeptic check:** run `plutil -lint` on the file, then `launchctl list` after install. If the job is not loaded while the install reports success, this is HIGH: an archive said to be scheduled that never runs.

---

**Held up** (attacks tried, and the line that stops each)
- `..`, `/`, `\`, `:`, `*`, `?`, `"`, `<`, `>`, `|`, control characters, and trailing dots and spaces: `app.suggestFolderName` (`copyexport_folder.go:29-47`) and the checks at `set.go:225-233`. For example, `..\..\Windows` becomes `Windows`.
- Windows reserved device names:
  - CON becomes `_CON` through `SanitizeSegment`;
  - `CONIN$`, `CONOUT$`, `COM¹` and `CON .txt` become "Mailbox" (`set.go:239-253`).
- Right-to-left, zero-width, separator and odd-space characters: `state.CleanFolderName` at `set.go:204`. So no name can start a new plan line or forge a log line.
- Names equal after case folding or NFKC: `setToldApart` (`set.go:259-298`). Anything the file system still sees as the same name (Turkish dotless ı, short 8.3 names) fails `Mkdir` with "exists" and takes the next number (`app/setparent.go:111-114`, `setexport.go:304-318`). Nothing is written into another folder.
- Very long names: at most 60 characters plus " (99)", so at most 245 bytes. That is under the 255-byte name limit.
- The Windows batch wrapper (`%`, `!`, `&`, `^`, quotes): `batchQuote` and `batchArg` (`wrapper.go:104-125`), with chcp 65001 for non-ASCII text, so Windows' "best fit" character swapping cannot happen.
- Cron and the Windows task XML: the name never reaches them. The program's arguments are only `-job` and a hashed file path (`wizard.go:1407`).
- Control characters anywhere in a schedule: refused by `schedule.go:96-104`.
- Tab or newline injection into the made-list: refused at `made.go:128` and `:185`.
- Dialogs:
  - Linux: the plan, summary, end note and download question are message or question dialogs, opened with `--no-markup` (`msg_unix.go:8`).
  - Windows: MessageBox; macOS: JXA with the text passed as JSON. Both show plain text.
  - List prompts are fixed words (`set.go:28`, `setschedule.go:28`).
- Alert header injection: `header()` removes CR and LF before encoding (`alert.go:366`). A folder name can never hold an encoded-word, because `?` becomes a space.
- Desktop add route:
  - it is still POST, checked by `GuardLocalPOST` (loopback and CSRF), one add at a time by `addBusy`;
  - the encrypted add shares that lock (`addarchive.go:31-43`, `encryption_create.go:42`);
  - the notes reach the page only through `textContent` (`dashboard.go:2574-2575`);
  - JSON replies carry nosniff (`dashboard.go:787`).
- The rows' "Last backup did not finish" line: html/template escaping, and only the record's date reaches the page (`archives.go:377`).
- The source-lost words: a closed set (`lastrun.go:479`, `sourcereason.go:27-33`).
- The Settings sync guard: it names the other archive and its job file only on the person's own loopback page, which already lists every archive (`syncshared.go:28-29`, `:73`, `:81`). No new viewer sees them.
- Every new `Sprintf` and `Errorf` uses a fixed format string.

---

**Check for the skeptic, in plain terms:**
- OUT-01: build that one message and measure its Subject line.
- OUT-02: make the `.{GUID}` folder on the Windows test PC and open its parent in Explorer.
- OUT-05: `plutil -lint` one plist with U+FFFF in the path.

Counters:
- Register: finder report.
- Borrowed lines: none.
- Drift: none known.
- Touched: nothing written.
- Persona: none.
- Sources reached:
  - the code at 443c2777: yes;
  - mail-server line limits, Explorer's `.{GUID}` behaviour and launchd's plist parser: from memory, not checked.


### Appendix B. Finder INS

**Adversarial pass, lens INS (insider and local file tricks): per-mailbox archives**

- **Date:** 2026-11-25 (from the clock). **Commit:** 443c2777, read-only worktree `.claude/worktrees/pma-adv`.
- **Read:** the brief; `assurance-kit/process/adversarial-review.md`; the design (all of it, §0 to §17); the pre-code review, through searches only.
- **Code read:** `app/setparent.go`, `decryptplan.go`, `copyexport_dest.go`, `copyexport_archive.go`; `wizard/setparent.go`, `setplan.go`, `setchild_unix.go`, `setchild_windows.go`, `setwindow.go`, `setexport.go`, `setexisting.go`, `set.go`; `wizard.go` lines 1640–1810; `job/made.go`.
- **Diffs read:** `desktop/archives.go`, `archivedelete.go`, `addarchive.go`, `syncshared.go`; `archindex` (and `Reconcile`); `app/run.go`; `autoplace.go`; `thunderbird.go` (and `movedHow`, `storefacts.go`); `sourceloss.go`; `health/sourcereason.go`; `health.go`; `lastrun.go`; `tbcompleteness.go`; `alert.go`. Also `winsec/sddl.go`, `secretfile_windows.go`, `state/manifest.go` (store ids) and `export/atomic.go` (sweep).
- **Not read:** `setschedule.go`; the `picker.go` diff; the test files; the catalog rows' text; the review in full; presync; the schedule package.
- **Not checked on a machine:** Windows' default folder permissions (the access lists that say who may write). They come from my own knowledge.
- **Nothing was run.**

**Verdict:** the parent-folder check and the made-list hold. Two parts break: §7's reading of the folders already in the parent, and C2's account check. On Windows, outside the person's profile, a re-run never recognises the set's own archives. One shipped flow can write a person's mail into another account's folder. A third case puts one account's mail under another address's folder name.

---

**INS-01 — MEDIUM. Outside the person's Windows profile, a re-run makes a second copy of every archive, and says nothing.**
- **Claim:** `setChildMine` counts a folder as the person's own only when no account other than the owner, SYSTEM or Administrators may write to it (`wizard/setchild_windows.go:21-36`, `winsec/sddl.go:55,65-76`).
  - Folders made under `C:\` (outside `C:\Users\<name>`), on `D:\`, or on an NTFS drive formatted by Windows inherit "Authenticated Users: Modify". That is an allow entry with write rights for another account.
  - A FAT or exFAT drive has no owner and no access list, so the check fails there too.
  - `MakeSetFolder` makes each folder with that inherited list (`app/setparent.go:111`; on Windows the 0700 mode has no effect).
- **Trigger:**
  1. Windows, ordinary user. Make a set in `D:\Mail archives` (the design's own example) or in `C:\Mail archives`, and answer Yes to the schedule.
  2. Run the same choice again into the same folder. The question's words and the Desktop's "did not finish" note both tell the person to do this.
- **What happens** (followed through the source):
  - No child is "looked" (`setplan.go:150-151`), but every child's name counts as taken (`:256-258`).
  - Every mailbox gets a new folder, "a@x.com (2)", a full export, and on Yes a second schedule. The old schedules keep running.
  - The plan has no line for folders it did not look at (`setwindow.go:34-119`). "Not found now" needs a looked folder (`setplan.go:283`).
  - The end note skips every folder inside this parent (`setexisting.go:105-107`), so nothing says "backed up twice".
  - MA-1507's question counts looked folders only (`setparent.go:126-128`). So "One archive" into the set's folder asks nothing, and that folder then becomes an archive: the next set re-run is refused there.
  - An unfinished folder is never continued, and each later re-run adds "(3)", and so on. This is C3's fold failing ("no duplicate archive or second schedule").
- **Same mechanism elsewhere:**
  - Linux: any group-write bit (`setchild_unix.go:19,26`). Examples: one-mailbox archives made by 1.7.x in a folder the file chooser created under umask 0002, or a folder made group-writable later.
  - A network parent whose owner IDs are not this user's (Linux CIFS mounted as root; NAS accounts mapped on Windows).
  - Possibly exFAT drives on macOS (to check).
- **Red test:**
  - Linux unit test: a plaintext archive of one store with mode 0775 in the parent. `planSet` should give `outContinue`; today it gives `outNew` "(2)".
  - Windows lab (MA-1503): parent `D:\` or `C:\X`, re-run, and the plan says "Brought up to date" for each archive.
  - Also: the plan names every child it did not look at.
- **Reproduction needs:** run `icacls C:\` and `icacls D:\` on the test PC. Expect `Authenticated Users:(OI)(CI)(IO)(M)`.

**INS-02 — MEDIUM. The export writes by path, so a link planted after the plan is followed.**
- **Claim:** C12 checks a child at plan time only (`setplan.go:130-157`). The export then uses `parent/name` as a path:
  - `setFolderThere` uses `os.Stat`, which follows links (`setexport.go:446`);
  - `newLogger(c.out)` opens the log inside (`:390`);
  - `app.Run` gets `Out` as a path (`:413-432`).
- `CheckSetParent` never asks whether other accounts can write to the parent (`app/setparent.go:35-58`).
- **Trigger:**
  1. Linux or macOS. Parent P can be written by another local account B: a group-shared 2775 folder, no sticky bit.
  2. The re-run's plan says "Brought up to date: a@x.com" (0700, the person's own).
  3. While the earlier archives export, B renames `P/a@x.com` and makes a link `P/a@x.com` pointing to `/home/B/drop` (mode 0777).
  4. That archive's whole mailbox is written into B's folder.
  - New folders have the same gap, for the seconds between `Mkdir` and `app.Run`.
- **For the person:** another account reads all of one mailbox's mail. MA-1494 says a link "is never written into". (One-archive outputs are also written by path today; here the plan told the person the folder was checked.)
- **Red test:** through a seam after the plan, swap the child for a link to a temporary folder. Expect `assure.Refused` (MadeFailed naming the folder), and the link's target holds nothing, not even `mailarchive.log`. Positive twin first: with no swap, the archive is continued.

**INS-03 — MEDIUM. An address-named archive silently takes in another account's mail when Thunderbird reuses its folder.**
- **Claim:** C2 asks only whether some live account uses the folder (`run.go:2186-2187`; `thunderbird.go:831`). It never compares that account with the one the record names.
- **Trigger:**
  1. Archive `a@gmail.com` backs up folder F = `ImapMail/imap.gmail.com`; the record names `a@gmail.com`.
  2. In Thunderbird, remove that account with its mail data (F is deleted). Runs meanwhile stop with C13's words and keep the name.
  3. Add `b@gmail.com`. F's name is free, so Thunderbird gives it F (the design's own `GetLocalPath` reading).
  4. The next run finds F, sees that `b`'s setting names it, and backs up `b`'s mail into the folder named `a@gmail.com`.
- **What follows:** the record now names `b`, and the removed banner clears. A set re-run says "Brought up to date: a@gmail.com" for `b`'s row, because the plan shows the folder's name, not the row's (`setwindow.go:55`).
- **For the person:** the summary invites handing over one archive (`setexport.go:518`). Handing over "a@gmail.com" hands over b's mail.
- **Red test:** a settings fixture following the steps above. The second run refuses (or warns) that the account now using F is not the one the archive was made for, and writes nothing.
- **Fix idea:** keep a fixed identity in the record (a hash of the account's user name and server) beside the shown name.

**INS-04 — MEDIUM. A re-added account under a different shown name is not stopped, and its new folder is backed up by nothing.**
- **Claim:** C2 stops only when the record's name matches a live account (`run.go:2188` needs `byName`).
- **Trigger:**
  1. The person had renamed the account "Gmail"; the record names "Gmail".
  2. Remove the account (keeping its mail) and add it again. Thunderbird uses `imap.gmail.com-1` and names it `a@gmail.com` by default.
  3. `NoLiveOwner(F)` is true, but no live account is named "Gmail" (`thunderbird.go:689`). The server-name fallback wants a folder outside the profile, and "-1" is inside it. So nothing stops.
- **For the person:** "Backup complete" every night on a frozen folder, while the account's new mail is in no archive. This is not among §14's kept risks.
- **Red test:** MA-1504's "-1" case with the re-added account's name differing from the record's should refuse with the moved words.
- **Fix idea:** a fallback for exactly one same-server "-N" sibling, or INS-03's identity hash.

**INS-05 — LOW. A false "removed".**
- **Claim:** `RemovedAs` counts names only (`thunderbird.go:848-866`). It skips the check `movedHow` makes first: a live account whose folder setting still names the gone folder (`:718-724`).
- **Trigger:** the account was renamed, then its folder was deleted by hand. The next run stops with C13's words and `source_reason: removed` (`run.go:2155-2160`). The banner says "To stop its backups, click Pause".
- **What actually happens:** Thunderbird would recreate the folder at its next sync. Clicking Pause ends that account's backups for good.
- **Red test:** a live account's `directory-rel` names the gone F under a new name. Expect today's plain "does not exist" words and no reason recorded.

**INS-06 — LOW. A carried name never expires.**
- **Claim:** `oneFolderNameKept` keeps the name forever while no live account uses the folder (`run.go:1608-1629`).
- **Trigger:**
  1. A custom name, "Work", is removed with its folder kept. The archive keeps backing up the frozen folder.
  2. A later, different account is named "Work".
  3. The old archive now stops every night with "the account that used it now keeps its mail in <the new account's folder>". That is false. Failure emails follow, then the 30-day pause in the moved words.
- **Red test:** that scenario gives no moved stop.

**INS-07 — LOW. The Desktop can drop a made-list line before it ever lists the folder.**
- **Claim:** a line counts as "gone" when the folder is missing while its parent exists (`desktop/archives.go:220-227`). It is struck then (`:210`), even though `Reconcile` never listed it (`archindex/index.go:489`). A re-run never records continued folders again: the line is written only for a new folder (`setparent.go:150-153`).
- **Trigger:** the stand-alone wizard makes a set whose parent is either:
  - the root of a drive mounted at a folder that stays when the drive is out (Linux fstab `/mnt/backup`); or
  - on one of two rotated macOS drives with the same name, where the other drive has the parent folder but not that archive.

  The Desktop page then loads.
- **For the person:** the archive never appears in Your archives: no row, no "did not finish" line. Its schedule and alerts still run.
- **Red test:** strike only lines whose folder was once listed, and have the next fold list it.

**INS-08 — INFO. An unreadable made-list stops every new set archive.**
- **Claim:** a link, a folder or a file over 64 KB at `wizard-made.txt` makes `ReadMade` fail (`made.go:95,110`). Then every `RecordMade` fails: each new folder is removed and the archive fails (`setexport.go:233-241`). The reason does not say what to do.
- Only someone who can write the settings folder can cause this.
- **Red test:** the summary names the file and the remedy.

---

**Held up** (attacks that failed, each with the code that stops it):
- **Parent swapped for a link, junction or mount** between the check, the handle and `mkdir`: the handle is opened first, its identity must equal `destChain` and the checked key, and `mkdir` goes through the handle (`app/setparent.go:86-111`).
- **Parent that is, or is inside, an unknown archive:** `ArchivesAbove` includes the parent itself (`copyexport_archive.go:206-230`; `decryptplan.go:290,296-300`). For an encrypted archive, `encmark.At` catches it (`decryptplan.go:266`).
- **MailArchive's own folders:** refused (`decryptplan.go:281-288`). Uninstall never deletes recursively (`uninstall.go:492`).
- **Planted network-path or `..` store id:** read as text and never opened (`state/manifest.go:535-547`; `idsFrom` with `SafeToken`).
- **Folders the set will not touch:**
  - links, junctions and other owners' folders are never read or written (`setplan.go:109-110`, `:130-131`);
  - a combined archive is never matched (`:318`, exactly one id);
  - an encrypted archive takes the next number (`:51`);
  - a paused archive is left alone (`:334`);
  - an oversized or newer-format manifest reads as "not an archive" and is never written into.
- **Made-list:** a link at its name and an oversized file are refused, and control characters and relative paths are skipped (`made.go:95,110,128`). Unlist keeps a row whose line cannot be struck (`archivedelete.go`, `forgetRow`).
- **`source_reason`:** limited to its two codes at read time (`lastrun.go:455,479-486`). Every reader goes through `ReadLastRun` (`health.go:2044`; `sourcereason.go:60`).
- **False "moved" from path spellings:** `directory-rel` is resolved on the profile found from the input itself (`thunderbird.go:813-833`).
- **Local Folders (and POP3, Feeds):** stores that are not IMAP get no completeness result (`app/tbcompleteness.go:173-175`). Named "gone" warnings come only from auto-detect runs. So the record never names them, and C2 and C13 cannot fire. It holds by construction, not by a guard. A test that pins "Local Folders gets no name" would keep it.
- **Privacy:** names are kept only in plaintext records (`run.go:1662`; `writeRefusedRun` `:1518-1524`). The stop words name folders only (`autoplace.go:282-292`). The plan, download question, summary and Desktop notes are dialogs or page replies, never files. I found no account name beside an encrypted archive, and none in a log except through the archive's own path.

**Not re-filed:** C-14. A re-run into a parent whose archives' walk is cut short (a slow drive, a share) is refused as "could not confirm" (`copyexport_dest.go:501-503`). The review rated it LOW and the fold left it.

**Counters:**
- **Register:** findings (adversarial), my own reading of the code.
- **Borrowed lines:** none.
- **Drift:** none noticed.
- **Touched:** read-only; worktree only; `git grep` and `git diff` only.
- **Persona:** none.
- **Sources reached:** code at 443c2777, yes; design, yes; review, by search only; Windows default access lists, not reached (my knowledge).
- **Check for the owner:** on the test PC, `icacls C:\` and `icacls D:\` settle INS-01's trigger.


### Appendix C. Finder CON

**Finder report, CON lens (integrity, time, crashes, concurrency)**

- **Date:** 2026-11-25, from the session clock. The read-only rules allow only `git -C` commands, so no clock command was run.
- **Commit reviewed:** 443c2777 (worktree `pma-adv`, detached). Nothing was run, built or written.
- **Read:**
  - the brief and `assurance-kit/process/adversarial-review.md`
  - the design, revision 2.2, in full
  - in full: `wizard/set.go`, `setexport.go`, `setparent.go`, `setplan.go`, `setwindow.go`, `setschedule.go`, `setexisting.go`, `setchild_unix.go`, `setchild_windows.go`, `app/setparent.go`, `job/made.go`, `desktop/addarchive.go`
  - the diffs of `desktop/archives.go`, `archivedelete.go`, `clidelete.go`, `launch.go`, `localschedule.go`, `archindex`, `app/run.go`, `autoplace`, `state` and `wizard/wizard.go`
  - `prepareThunderbirdGUI`, `askScheduleBy`, `offerSchedule` and `installOffered` in `wizard.go`
  - `wizard/pause.go`, `schedule/descriptor.go`, `schedule.go:599-663`, `sweep.go`, `archdelete/installguard.go`, `winsec/sddl.go`
  - `health.go` `runRunning`, `thunderbird.go` `movedHow`, `localrun.go`
- **Not read:**
  - `docs/review-per-mailbox-archives-predesign.md`, so a finding here may repeat one already folded. The skeptic should check.
  - the catalog rows' text and the test files
  - `syncshared.go`, `sourceloss.go`, `health/sourcereason.go`, the alert changes, the `picker.go` diff
  - the inside of `checkDecryptPaths`, `CheckDestinationOther` and `Recheck`
  - the Desktop page's script (whether it polls)
  - the real behaviour of Windows access lists, or of macOS on exFAT or FAT drives

**Verdict: not ready.** One HIGH, three MEDIUM, five LOW and two INFO.

---

**CON-01 HIGH: a re-run treats any readable job file as a live schedule**

- **Claim:**
  - `offerSetSchedule` (`setschedule.go:99-109`) marks a continued archive "kept" whenever its job file reads (`setJobState`, `:73-84`). It never asks the scheduler whether the entry exists, and never reads the "Run now only" mark.
  - `setAllScheduled` (`setexisting.go:165`) then counts that archive as scheduled.
  - So `existingLines` (`:139-141`) adds the Pause advice for the combined archive.
- **Four ways a job file exists with no live entry:**
  - (a) The install failed after the job was written. `installOffered` writes the job first (`wizard.go:2420`). `GuardedInstall` has no undo when `installEntry` fails (`installguard.go:30-31`). Triggers include a `refuseCollision` refusal or a launchctl, schtasks or crontab error.
  - (b) Power was lost between those two steps.
  - (c) The 30-day auto-pause removes only the scheduler entry and keeps the job file (`pause.go:124-147`).
  - (d) "Run now only" records a descriptor with interval "manual" and installs no entry, on purpose (`descriptor.go:86-88`). Keeping that job is right (design condition C6). Counting it as scheduled is not.
- **Sequence:**
  1. A combined archive C exists: All of them, auto-discovery on, inputs A and B, scheduled daily.
  2. The person makes a set in parent P: folders `P/a` and `P/b`, then answers Yes to the schedule. The install for `P/b` fails. The end note says "Not scheduled: b (…)". The job file for `P/b` stays.
  3. Later the person adds account D and runs the set into P again. `a` and `b` are brought up to date; `b` is marked kept; D gets a new archive and is installed.
  4. The end note says "b kept its own schedule." and "Your archive C also backs up these mailboxes, all together. To stop backing them up twice, click Pause…".
  5. If no new mailbox is found, the question and the note never appear, so the re-run fixes nothing and says nothing.
- **Harm:** B has no scheduled backup. If the person follows the advice and pauses C, nothing backs B up. This is an archive silently not backed up.
- **Red test:**
  - Set up a continued member with a readable job file while the scheduler seam answers "not installed", plus a second case with a manual descriptor.
  - Assert the first is installed on Yes, or reported "Not scheduled".
  - Assert neither one says "kept its own schedule" as if it were running.
  - Assert `existingLines` gives "Keep it running…", not the Pause advice.
  - Mutation: remove the scheduler check.

**CON-02 MEDIUM: on Windows outside the user's own folders, and on drives without owners, a re-run never finds the set's own archives, so each re-run makes "(2)" copies with their own schedules**

- **Claim:**
  - The child read only looks at a folder that is "owned by the person and writable by no one else" (`setplan.go:150-152`, `setchild_windows.go:21-36`, `setchild_unix.go:17-27`).
  - `MakeSetFolder` makes each folder with `root.Mkdir(name, 0o700)` (`app/setparent.go:111`). Windows ignores that mode, so the folder inherits its parent's access list.
  - Folders under `C:\` (for example `C:\Mail archives`) or under a second drive (`D:\…`) inherit "Authenticated Users: Modify" by default. `winsec` counts that group as another account (`sddl.go:65-75`).
  - So the wizard's own folders are never looked at (`setplan.go:104-117`). Their names still count as taken (`:256-258`), so `planOne` makes a new numbered folder (`:357-365`).
  - Nothing reports it: neither "not found now" (`:282-294`) nor the one-archive warning, MA-1507 (`wizard/setparent.go:120-132`). The parent itself is not checked this way (`DACLWritableByOthers` is not used in `app`).
  - Unix behaves the same for any folder with group or other write. macOS shows exFAT and FAT drives that way, as do many SMB and cifs mounts.
- **Sequence:**
  1. On Windows, as an ordinary user, the person makes a set in `D:\Mail archives`: `alice@x` and `bob@x`, both scheduled.
  2. Next month they add carol and run the set into the same folder again.
  3. The plan says "will make 3 archives: alice@x (2), bob@x (2), carol@x". It has no "Brought up to date" line and gives no reason.
  4. After Make the archives and Yes, alice and bob each have two archives and two schedules. Every re-run adds another copy.
- **Harm:** double backups and schedules, full re-exports, and a plan that does not say why. No mail is lost.
- **What a reproduction needs:** the Windows test PC, where `icacls` on the parent should show `AU:(OI)(CI)(M)`; or, on unix, a set folder changed to mode 0777, then a new plan.
- **Red test:**
  - Plan into a parent whose set folders carry the inherited write entry, or mode 0777.
  - Assert each mailbox is brought up to date, or that the plan names each folder it could not check and makes no "(2)" for a mailbox whose name that folder holds.
  - A possible fix on Windows: restrict each new folder to its owner with `winsec.RestrictFolderToOwner`.

**CON-03 MEDIUM: two sets made into one parent at the same time give one mailbox two archives and two schedules**

- **Claim:**
  - When a name turns out to be taken at make time (design condition C18), `makeSetArchiveFolder` (`setexport.go:304-318`) takes the next number (`setFreeName`, `:331-357`).
  - It never asks whether the thing now at that name is this same mailbox's folder, made by the other run.
  - No lock works across programs. The Desktop's `addBusy` (`addarchive.go:39`) only stops two adds inside one Desktop.
- **Sequence:**
  1. The double-click wizard plans a set into P (`a` and `b` both new) and starts `a`'s long first export.
  2. The Desktop's Make a new archive…, with the same choices and the same P, plans `b` as new and exports it first.
  3. When the wizard reaches `b`, the folder exists, so it makes `b (2)` and exports B again.
  4. Both runs answer Yes, so B gets two scheduler entries.
  5. Later re-runs only say "b (2) also holds this mailbox's archive: left as it is" (`setplan.go:328-330`). `existingLines` skips folders inside the parent, so nothing advises Pause.
  - Two double-click wizards at once do the same.
- **Red test:**
  - Make two plans before any folder is made, then make `b` with the first.
  - For the second plan, assert no `b (2)` folder appears, and that its result says another window is making that archive. It can tell from a made-list line for this name in this parent written after its plan, or from a child that now holds this mailbox's store.

**CON-04 MEDIUM: after a crash or power loss mid-backup, the row never says "Last backup did not finish"**

- **Claim:**
  - `rowLastRun` (`archives.go:377-401`) shows nothing for any record whose status is "running" (`:388`).
  - A run killed by power loss leaves "running" until the next run.
  - Health already tells a live run from a dead one by the archive lock (`runRunning`, `health.go:1365-1374`; MA-1448's "did not finish").
- **Sequence:**
  1. The stand-alone wizard is making a set of 4. Power is lost during archive 3, after its first checkpoint save.
  2. The schedule question was never reached, so nothing is scheduled.
  3. The Desktop later folds the made-list and lists rows 1 to 3. Row 3 is partial, with a "running" record, so it shows no line. No row says anything is wrong.
  4. The set's own words promised "a row whose last backup did not finish says so".
  - This also affects any archive whose scheduled run was killed.
- **Red test:** a record with status running and a dead process number, with the lock free, gives the row's line. The same record with the lock held by an export gives none.

**CON-05 LOW: a carried account name lets a different account with the same name trigger the "moved" stop**

- **Claim:**
  - `oneFolderNameKept` (`run.go:1608-1629`) carries the old account name for as long as no live account uses the folder, with no time limit.
  - The folder-moved stop (design condition C2, `run.go:2186-2191`) fires when exactly one live account has that name and keeps its mail elsewhere. That match is by name only; the mail server is never compared (`thunderbird.go:681-710`).
- **Sequence:**
  1. A one-mailbox archive backs up `ImapMail/imap.a.example`, for an account named "Work".
  2. The person removes that account and keeps its folder. The archive keeps running for months with the name carried.
  3. They add a new account, also named "Work", on `imap.b.example`.
  4. The next run stops: "Thunderbird no longer uses …: the account that used it now keeps its mail in …imap.b.example". Failure emails follow, then the 30-day pause.
- **Harm:** wrong words and alerts. The old folder's mail is safe.
- **INFO, the reverse case:** an account with a custom name, removed and then re-added under Thunderbird's default name (its address), never stops. Its old archive keeps backing up the dead folder quietly, because only a name match can stop it (`run.go:2189`).
- **Red test:** carried "Work" for `imap.a.example` plus one live "Work" on `imap.b.example` gives no stop. The same name on `imap.a-1.example` does stop.

**CON-06 LOW: with two accounts on one mail server, a re-run "brings up to date" an archive the moved stop will block, then tells the person to run it again**

- **Claim:**
  - The list marks a folder "not in Thunderbird now" only when it is certain no account uses it (`storeUnused`, `picker.go:274-281`).
  - With two accounts on one server it is only "ambiguous" (the very case fixed by design finding F-01). So the old folder is an ordinary row, and `planOne` continues its archive (`setplan.go:318-339`).
  - `app.Run` then hits the moved stop (`run.go:2186-2191`).
  - The summary adds "run this again with the same folder to finish it" (`setexport.go:285`, `:571`). The Desktop note says "To finish it, click Make a new archive… again" (`addarchive.go:141`, `:165`).
- **Sequence:**
  1. `alice@gmail.com` and `bob@gmail.com` are in one profile, with a set in P.
  2. Alice's account is removed (folder kept) and re-added at `imap.gmail-2.com`.
  3. The re-run's plan says "Brought up to date: alice@gmail.com" and makes `alice@gmail.com (2)`.
  4. The old archive's line says "Did not finish: Thunderbird no longer uses…" followed by "run this again". Every re-run repeats this.
  5. Because a member always fails, `setAllScheduled` stays false, so a combined archive elsewhere never gets its Pause advice.
- **Red test:** the plan does not continue that folder's archive. It says the folder is kept as it is and to click Pause on it, and nothing says "run this again".

**CON-07 LOW: an unfinished folder is matched by mailbox name only, so another mailbox can take it over**

- **Claim:**
  - A made-list line holds a path and a name, not the mailbox's store (`made.go:38-43`).
  - The unfinished-folder rule matches on name alone (`setplan.go:343-350`).
  - Names are numbered in list order (`set.go:132-143`), so a newly found mailbox with the same name, listed first, takes the name the line holds.
- **Sequence:**
  1. Two profiles each have "Local Folders". The first run lists only profile 1 and makes and records the folder `Local Folders`.
  2. Power is lost before the first save, which comes about 1,000 messages in (`run.go:1306`). The folder holds message files and no manifest.
  3. Profile 2 appears and is listed first on the re-run. It becomes "Local Folders" and continues profile 1's folder. Profile 1 becomes "Local Folders (2)".
  4. Profile 1's stray files stay in profile 2's archive, outside its manifest.
- **Red test:** a line for `P/x` named "x", no manifest, and rows B "x" then A "x (2)": B must not continue `P/x` unless the line names B's store. Fix: keep the store's id in the line.

**CON-08 LOW: the Desktop note can say an archive was added when it was not**

- **Claim:**
  - `addedSet` counts finished archives for its lead (`addarchive.go:135-137`, `:152-160`).
  - It registers only folders that pass `validArchiveFolder` at that moment (`:131-133`).
  - Registering no folders returns no error (`:197-199`), so the "could not be added" words never appear.
- **Sequence:**
  1. The Desktop makes a set on USB drive `E:\Mail`. Archive 1 finishes.
  2. The drive is unplugged during archive 2, and the set stops.
  3. The note says "1 archive added — it is now in Your archives." The row is not listed until the drive is back.
- **Red test:** one finished archive whose folder cannot be reached: the note must not claim it is in Your archives.

**CON-09 LOW: a page load running at the same moment can undo Remove from list**

- **Claim:**
  - The Desktop's page-load fold (`reconcileArchives`) reads the made-list before taking the list's lock (`archives.go:244-245`).
  - It then applies those stale pointers to a fresh copy of the list under the lock (`:253-257`).
  - Remove from list strikes the made-list line first, then removes the row (`archivedelete.go`, `forgetRow`).
- **Sequence:**
  1. Right after a Desktop set where something did not finish, the page stays and the lines are not struck yet.
  2. A page load reads F's line and also finds another new folder to add.
  3. Meanwhile the person removes F: its line is struck and its row removed.
  4. The page load takes the lock and adds F back from its stale pointers.
  - This needs two requests at once: a second tab, or a page that polls (not checked).
- **Red test:** run the strike and the row removal between `madeLines` and `UpdateFile`; F must stay removed.

**CON-10 INFO:** the summary says "The mail copied so far is kept" for archives whose folder was never made. This covers a folder that could not be recorded in the made-list, a name with no free number, and a refusal about the parent (`setexport.go:233-250`, `:571`).

**CON-11 INFO:** power loss between making a folder and recording it (`wizard/setparent.go:145-157`) leaves an empty folder under the mailbox's own name. Every later run names the real archive "(2)" (`setplan.go:256-258`). Only the error path removes that empty folder (`setexport.go:233-241`).

---

**Lens point 1: crash windows in short**

1. **After making the folder, before recording it:** see CON-11.
2. **After recording it, before the first save:**
   - Left on disk: a folder with no manifest, a made-list line, no job file.
   - The re-run continues it (`setplan.go:343-350`).
   - The Desktop does not list it and keeps its line (`archives.go:200-227`).
   - This held up.
3. **Mid-export, after a checkpoint save:**
   - Left on disk: a manifest, a "running" record and the made-list line.
   - The re-run brings it up to date.
   - The Desktop lists it but the row is silent; see CON-04.
4. **Between the last export and the schedule question:**
   - Left on disk: the archives and their lines; no jobs, no scheduler entries; `last-archive.txt` unchanged (`setexport.go:185-187`).
   - The re-run asks the schedule question and installs (`setschedule.go:110-112`). This held up.
   - No row says "not scheduled", the same as today for one archive.
5. **Mid-way through the installs:** one archive can be left with a job file and no entry; see CON-01.

**Attacks that held up**

- **Point 4, Full mode only for prepared archives: correct.**
  - `runSet` works out "every archive prepared" over all the archives it will export (`setexport.go:159-168`). Only then does `askAnswers` skip the mode question.
  - `exportSet` forces Full mode per archive only when that archive was prepared (`:262-264`).
  - Only new or unfinished archives are ever asked about a download (`setplan.go:347`, `:364`). For those, Full and Incremental copy the same mail, so no answer the person gave is overridden in effect.
- **Schedule question:**
  - Only an exact daily or weekly row installs. Cancel or closing the window installs nothing. The re-asking is bounded (`wizard.go:2318-2346`).
  - A continued archive with any job file is never installed over (`setschedule.go:100-108`).
  - No set job carries a Thunderbird sync (`:139`).
  - The 10-minute slots advance only after a successful install (`:145`).
- **Loops:** the each-mailbox question and the folder-making loop are both bounded (`set.go:49-64`, `setexport.go:306`).
- **Made-list:**
  - Every change happens under an operating-system lock that is released on a crash, and is written by atomic rename (`made.go:229-262`).
  - The Desktop never strikes the line of a folder with no manifest, or of one whose parent folder is gone (`archives.go:200-227`).
- **Parent unplugged mid-set:**
  - The archive folder is never made again (`setexport.go:381`, `:397`).
  - The set stops (`:472-477`).
  - The summary leaves out a log path it cannot reach (`:572`).
- **Disk full:**
  - The run's fatal error unwraps to "no space", so the set stops (`localrun.go:53`; `setexport.go:481-487`).
  - When the scheduler entry goes in but its descriptor cannot be written, the entry still exists (`schedule.go:645-660`), so that case is not CON-01.
- **A moved-stop archive stays stopped:** refused runs carry the account names forward (`run.go:1450-1458`).
- **Two Make a new archive… clicks in one Desktop:** blocked by `addBusy` (`addarchive.go:39`).
- **A scheduled backup of a member while the wizard continues it:** the archive lock makes one of them fail with a reason. The set goes on to the next archive and its summary says so (`setexport.go:284-286`).

---

Register: findings report, adversarial and read-only. Borrowed lines: code identifiers and the product's own words only. Drift: none noticed. Touched: nothing; reads and `git grep`/`git diff` only. Persona: none. Sources reached: the code at 443c2777, yes; design revision 2.2, yes; the pre-code review, no; real Windows access lists and macOS exFAT modes, no (CON-02 says what a reproduction must confirm). Smallest check for the skeptic: CON-01, done with the scheduler seam.


### Appendix D. Skeptic

**Skeptic report: per-mailbox archives, adversarial pass**

**Verdict: not ready.** One HIGH holds. A member that has a settings file but no live schedule is counted as scheduled. The end note then tells the person to pause the combined archive, which can leave that mailbox with no backup at all. Three MEDIUM findings also hold: two on Windows, and one about Thunderbird reusing a folder.

Of the 24 findings, 16 went red in a test of mine, 7 are settled by reading the code or by the branch's own tests, and 1 is refuted. The brief said 32, but the three reports hold 5 + 8 + 11 = 24.

#### Header
- **Date:** 2026-11-25, 03:20 to 03:31 EST (read with `date`).
- **Commit:** 443c2777, worktree `pma-adv`, compared with 10fcc2e.
- **Read:**
  - the skeptic part of `adversarial-review.md`;
  - the three finder reports, plus the friction review for cross-reference;
  - the design: §0, §6, §7, §10, §11 and §14;
  - the wizard's `setplan`, `setschedule`, `setexport`, `setexisting`, both `setparent` files, `setchild_unix`, `setchild_windows`, `set`, and parts of `wizard.go`, `pause.go` and `picker.go`;
  - `winsec/sddl.go`;
  - the diffs of `run.go`, `thunderbird.go`, `autoplace.go`, `archives.go`, `archivedelete.go` and `alert.go`;
  - `addarchive.go`, `localschedule.go`, `job/made.go`, `archindex` `Reconcile`, the launchd code in `schedule.go`;
  - Go 1.26's Windows `Mkdirat`.
- **Not read:** the catalog rows, the pre-design review in full, the Desktop page's script beyond a search.
- **Commands:**
  - About 40 read-only `git`, `grep` and Read calls, all exit 0. Two were my own syntax slips (a zsh glob: 1; `git grep -A`: 128), each redone with exit 0.
  - `memory_pressure -Q`: 64% free before each of the 3 test runs.
  - `go vet` on wizard and app: 0.
  - `go test -p 2 -count=1 -v -run TestZZSkeptic` on wizard: 1 (6 red, as intended).
  - The same on app: 1 (3 red).
  - The same on alert and schedule together: 1 (alert red; schedule PASS).
- No real dialog, browser, notification, scheduler entry or owner data was touched.

#### Verdict table
| ID | Verdict | Final sev. | Evidence |
|---|---|---|---|
| CON-01 | CONFIRMED-BY-TEST | **HIGH** | `TestZZSkepticCON01`: cy's install failed, but its job file stayed. On the re-run the note said "cy@qz-c.example kept its own schedule." and gave the combined archive "To stop backing them up twice, click Pause". cy had no entry. Cause: `setschedule.go:101-106` reads only the job file. The design's own rule (§11, l.504-505) says "installed and not paused". The same happens after an auto-pause (`pause.go:124-147`) and with "Run now only" (`localschedule.go:243`). |
| INS-01 | MECHANISM-ONLY | MEDIUM | `TestZZSkepticINS01Predicate` red: the access list a folder inherits under `C:\X` or `D:\` counts as writable by others. Go's `Mkdirat` passes no security setting, so the 0700 is ignored (`internal/syscall/windows/at_windows.go:210-228`). `RestrictFolderToOwner` exists but nothing calls it. |
| CON-02 | DUPLICATE of INS-01 | — | — |
| INS-02 | CONFIRMED-BY-TEST | MEDIUM | `TestZZSkepticINS02`: a link put at `ann@…` after the plan. Nine entries were written into its target, including the mailbox folder, `search.db` and the log, and the outcome was "finished". The writes go by path: `setexport.go:381,390,427`. |
| INS-03 | CONFIRMED-BY-TEST | MEDIUM | `TestZZSkepticINS03`: account A removed with its folder, then account B given the same folder. The next run backs up B's mail into A's archive with no stop, and the record now names "qz-other@…". Not new for one-mailbox archives; address-named folders make it misleading. |
| INS-04 | PARTIAL | LOW | The branch itself pins this as wanted: `ma1504_one_folder_stop_test.go:166-169` ("no live account with the recorded name", discovery must not stop). The harm is §14's first kept risk; only the missed re-add under a new name is new. |
| INS-05 | CONFIRMED-BY-TEST | LOW | `TestZZSkepticINS05`: the account renamed and its folder deleted by hand. The run says "removed" (`source_reason` removed) while a live account still names that folder. |
| INS-06 | CONFIRMED-BY-TEST | LOW | `TestZZSkepticINS06`: a carried name "Work" meets a different "Work" on another server, and the old archive stops with the "moved" words. |
| CON-05 | DUPLICATE of INS-06 | — | Its INFO reverse case is INS-04. |
| INS-07 | CONFIRMED (code) | LOW | `archives.go` `goneFolder` strikes a line whose folder is missing while its parent is there, even if it was never listed (`index.go:489` skips non-archives). Hits Linux fstab mount points. |
| INS-08 | CONFIRMED (code) | INFO | `made.go:95,110`. The error names the file but gives no remedy. Only someone who can write the settings folder can cause it. |
| CON-03 | CONFIRMED-BY-TEST | LOW (from MEDIUM) | `TestZZSkepticCON03`: two plans, then both runs. Every mailbox got "(2)", and 8 entries were installed. It needs two set windows at once into one folder. Nothing is lost. |
| CON-04 | CONFIRMED (code) | LOW | `archives.go` `rowLastRun`, `case state.RunRunning`. This is the design's own F-10b, so a crash leaves the row silent. |
| CON-06 | CONFIRMED (code + branch test) | LOW | `picker.go:281` needs `NoAccountNone`; `ma1504…test.go:215` shows such a folder reads as ambiguous. `planOne` continues it, `run.go` stops it, and "run this again" repeats. Related to friction F4. |
| CON-07 | DUPLICATE of OUT-03 | — | — |
| CON-08 | CONFIRMED (code) | LOW | `addarchive.go:130-160`: the lead counts finished archives, not registered ones, so "1 archive added — it is now in Your archives" appears with nothing registered. Related to friction F6. |
| CON-09 | CONFIRMED (code; race not run) | INFO | `archives.go:244-257` reads the made-list before the lock. The page does not poll; it needs a second tab plus a new row at that moment. |
| CON-10 | CONFIRMED (code) | LOW | `setexport.go:571` adds "mail copied so far is kept" for folders never made. Same failure as friction F2. |
| CON-11 | CONFIRMED (code) | INFO | `wizard/setparent.go:146-153`: the window between making the folder and recording it is tiny. |
| OUT-01 | CONFIRMED-BY-TEST | LOW | `TestZZSkepticOUT01`: 1,022 octets only in the extreme case (60 emoji under a 110-character folder). 722 octets for 60 CJK letters. 103 octets for one accent, against the 78 the standards recommend. |
| OUT-02 | CONFIRMED-BY-TEST (name); MECHANISM-ONLY (display) | LOW | `TestZZSkepticNames`: `me@mydomain.app`, `.{GUID}` and `.rtfd` are all kept. `.app` is a real email domain ending, so Finder would show the archive as an app. |
| OUT-03 | CONFIRMED-BY-TEST | LOW | `TestZZSkepticOUT03`: a renamed row continues the unfinished folder made for another mailbox (`setplan.go:344`). The stray file stays inside the finished archive. |
| OUT-04 | CONFIRMED-BY-TEST (simulated) | LOW | `TestZZSkepticOUT04`: a decomposed name planted on APFS stands in for HFS+. The plan says "new josé@qz-a.example (2)" instead of continuing. |
| OUT-05 | REFUTED | — | `TestZZSkepticOUT05`: U+FFFF does reach the plist, but `plutil -lint` says "OK". `launchctl` was not tried, since it would touch the real scheduler. |

**Windows lab steps** (office test PC, account claude-test, inside `C:\Tests` only):
- INS-01: run `icacls C:\` and `icacls C:\Tests`. Make a set into `C:\Tests\set`, run it again, and expect "Brought up to date" (today it should say "(2)"). Then `icacls` one child and expect an inherited `Authenticated Users: (M)`.
- OUT-02: make a folder `x.{645FF040-5081-101B-9F08-00AA002F954E}` in `C:\Tests` and look at it in Explorer.

#### What must be fixed, highest first
1. **CON-01 (HIGH).**
   - Fix: count a continued archive as scheduled only when the scheduler says its own entry is installed (`schedule.Query` on its recorded backup name) and its record is not "Run now only". Otherwise mark it with a new state, say "has its own settings but no scheduled backup now: click Repair on its row", and give no Pause advice. Never install over it (C6 kept).
   - Red tests: `TestZZSkepticCON01`, plus a "Run now only" twin and an auto-paused twin through the scheduler seam.
2. **INS-01, which also closes INS-02 on Windows (MEDIUM).**
   - Fix: right after `root.Mkdir`, restrict each new set folder to its owner (`winsec.RestrictFolderToOwner`, unused today). On Windows this also stops another account renaming the folder or planting a junction there. Also make the plan name each folder it did not look at, so a "(2)" is never unexplained.
   - Red tests: a seam test that the restrict is called on Windows; a unix plan test with a 0775 child that names it; the lab step above.
3. **INS-02 (MEDIUM).**
   - Fix: keep each child's identity from the plan. Right before the log opens and `app.Run` starts, check it again through the parent's handle (a real directory, the same folder). If it changed, the archive fails, naming the folder, and nothing is written.
   - Red test: `TestZZSkepticINS02`. The link's target must stay empty.
4. **INS-03, INS-04, INS-05, INS-06 (MEDIUM, then LOW): one root cause, folder identity by name.**
   - Fix: record a stable identity in the plaintext record (a hash of the account's user name plus server), and decide "moved", "removed" and "now another account's folder" by it.
   - Red tests: `TestZZSkepticINS03`, `INS05`, `INS06`.
   - Gate: INS-04 would flip the branch's positive twin at `ma1504…:166-169`. That changes a pinned test, so it is the lead's call, not the builder's.
5. **OUT-03 (same as CON-07), LOW.** Put the store id in each made-list line and match unfinished folders by it. Red test: `TestZZSkepticOUT03`.
6. **OUT-04, LOW.** Compare child names to made-list names in composed form (NFC), and compare the parent by identity, not by bytes (`setplan.go:249`). Red test: `TestZZSkepticOUT04`.
7. **CON-03, LOW.** Hold a lock beside the made-list for the whole set run (never in the parent, per §6). A second window then says another window is making archives there. Red test: `TestZZSkepticCON03`.
8. **CON-06, LOW.** Before continuing, check `NoLiveOwner` plus a moved name. If so, plan "kept as it is… Click Pause", not "run this again".
9. **Wording, LOW** (the friction review's builder can take these together):
   - CON-10 (= friction F2);
   - CON-08 (with friction F6);
   - CON-04 (show the line when the record says running but the archive lock is free);
   - INS-07 (strike a "gone" line only for a folder that was once listed).
10. **OUT-01, LOW.** Fold the Subject into lines of 78 octets or fewer (CRLF and a space between encoded words). Red test: `TestZZSkepticOUT01`.
11. **OUT-02, LOW.** Change names ending `.app`, `.rtfd` or `.{GUID}`. The exact form is the owner's call, because ruling 3 names folders by address.

#### Missed by the finders
- **On Windows, INS-02's precondition is the default, not an edge case.** The same inherited "Authenticated Users: Modify" lets any other local account rename a set folder and plant a junction. Unprivileged users can make junctions. This is mechanism only, judged from code and Windows' documented defaults. Fix 2 closes it.
- **A sibling of OUT-04:** the made-list's parent is compared by bytes (`setplan.go:249`). A parent chosen with a different spelling (letter case on Windows or macOS) would never continue an unfinished folder. I did not test this.
- **CON-01 also covers auto-paused members.** After a 30-day auto-pause, a re-run never puts the schedule back, yet says "kept its own schedule".

#### Throwaway tests (untracked; delete them before any full `go test ./...` in that worktree, since they are meant to fail)
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-adv/internal/wizard/zz_skeptic_pma_test.go`
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-adv/internal/app/zz_skeptic_pma_test.go`
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-adv/internal/alert/zz_skeptic_pma_test.go`
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-adv/internal/schedule/zz_skeptic_pma_test.go`

**How this was done, and what was not checked**
- This is a finding report: my own judgement of each claim, with test output as evidence.
- Not checked on a real machine:
  - Windows access lists and Explorer's handling of `.{GUID}` folders (from documentation and memory);
  - Finder's handling of `.app` folders;
  - mail servers' line limits;
  - `launchctl`.
- The smallest check you could do: the two Windows lab steps above.

Counters: Register: finding report. Borrowed lines: product words only. Drift: none. Touched: 4 untracked test files. Persona: none. Sources reached: the code at 443c2777, yes; the design, in part; Windows and macOS behaviour, no.


### Appendix E. Re-verify of rounds A and B

### Re-verify: per-mailbox archives, fix rounds A and B

**Verdict: NOT READY.** One new MEDIUM problem is confirmed by a test (RV-01). It is cheap to fix, and the fix touches the plan code the Windows lab would test, so fix it first, then run the lab. Of the 23 confirmed findings, 19 are fully closed, 2 only partly (OUT-02, INS-01), and 2 were never fixed (CON-09, CON-11, both INFO). Of F1 to F14, 12 are closed and 2 partly (F2, F12).

#### Header
- **Date:** 2026-11-25, 06:24 to 06:41 EST, read with `date`.
- **Commit:** ce5b6fbd, worktree `pma-rv`. Compared with 443c2777.
- **What I read:**
  - the lead's fold and Appendix D of `docs/review-per-mailbox-archives-adversarial.md`;
  - the fold and F1 to F14 of `docs/review-per-mailbox-archives-friction.md`;
  - design revision 3;
  - the full diffs of `thunderbird.go`, `storefacts.go`, `state/lastrun.go`, `state/tbcompleteness.go`, `app/run.go`, `app/tbcompleteness.go`, `sourceloss.go`, `autoplace.go`, `health/sourcereason.go`, `alert.go`, `job/made.go`, `archdelete/dellog.go`, `uninstall/config.go`;
  - in the wizard: `setplan.go`, `setexport.go`, `setschedule.go`, `setexisting.go`, `setparent.go`, `setwindow.go`, `set.go`, `picker.go`, `wizard.go`, `setchild_unix.go`, `setchild_windows.go`;
  - in the Desktop: `addarchive.go`, `archives.go`, `syncshared.go`, `capture.go`, `help.go`;
  - also `winsec/owneronly_windows.go`, `lockfile.Probe`, `encryptedLastRun`, `AutoPauseIfDue`, and the README diff;
  - the skeptic's 4 test files and the branch tests named in the evidence below.
- **Not read:** the commit messages' prove-fail notes in full, the catalog rows, the Desktop page script.
- **Commands and exit status:**
  - Read-only `git log`, `diff`, `show` and `status`: all 0.
  - About 25 grep and Read calls: all 0, except one zsh glob slip (exit 1), redone with exit 0.
  - `memory_pressure -Q` before each test run: 62% free every time.
  - `go test -p 2 -count=1 -v -run TestZZRV` on wizard: 1 (2 red, as intended: RV-01 and RF-01).
  - The same on app: 0. On alert: 0.
  - exFAT probe:
    - `hdiutil create`, `attach -nobrowse` and `detach` of a 32 MB exFAT image inside the worktree: 0. The image and its mount point were removed afterwards.
    - The wizard probe run against it: 0.
  - `mdls` on 3 probe folders inside the worktree: 0. Folders removed afterwards.
  - Wizard `-run TestZZRVCON01AnsweredNo`: 1 (red, as intended: RV-03).
  - I started a run of every touched package, then stopped it myself before it finished. My meant-to-fail throwaway tests would have muddied it. No result.
  - The branch's own tests, with `-skip TestZZRV`:
    - identity and stop tests in app, thunderbird, state and health: 0;
    - set, kept-schedule, plan, note, sync and subject tests in wizard, desktop, job, alert and uninstall: 0.
  - `assureblock` with my test files moved aside: 0. They were put back afterwards.
- **Not touched:** no real dialog, browser, notification or scheduler entry; none of the owner's settings, archives or keychain.

#### Findings

| ID | Verdict | Evidence |
|---|---|---|
| CON-01 (HIGH) | CLOSED | `setschedule.go` `setScheduleLive`: a job file alone no longer counts as a schedule. The skeptic's test now passes: cy's note line reads "has its own settings but no scheduled backup now", no Pause advice is given, and the combined archive is told to keep running. A re-run with nothing new to schedule says the same line in its summary. Auto-pause removes the scheduler entry (`pause.go:124-147`), so it also reads as "no scheduled backup now". One gap remains: RV-03. |
| INS-01 (= CON-02) | PARTLY CLOSED, MECHANISM-ONLY | Each new folder is restricted to its owner (`setparent.go:210`; the branch's seam test passes). The plan now names every folder it did not look at. Still open: Windows FAT and exFAT drives (RV-02). |
| INS-02 | CLOSED | `setChildSame` checks each folder again, once before the log opens and once before the export starts (`setexport.go`). The skeptic's test now passes: the link's target stays empty. |
| INS-03 | CLOSED for plaintext one-folder archives | `run.go:2302`, `ReusedByID`. My test with user names in the fixture: the run stops with reason `reused`, the archive's file list is unchanged, and the next run stops again. Not closed inside a set (RV-01). Encrypted archives: RV-07. |
| INS-04 | CLOSED (lead's call) | The branch test `TestAReAddedAccountUnderAnotherNameStopsByItsIdentity` passes. The pinned twin is restated beside its old text (`ma1504_one_folder_stop_test.go:170-200`); the old text is kept. |
| INS-05 | CLOSED | My identity test: a renamed account whose folder was deleted gets "does not exist", not "removed". |
| INS-06 (= CON-05) | CLOSED | My identity test: a different "Work" account on another server no longer stops the old archive as "moved". |
| INS-07 | CLOSED (code) | `archives.go:216-221`: a gone folder's made-list line is struck only once that folder was listed, or after MailArchive deleted it. |
| INS-08 | CLOSED (code) | `made.go`: an unreadable made-list now gets the remedy "move or delete that file … then run this again". |
| CON-03 | CLOSED | `job.HoldSetLock` uses an operating-system lock, which is released if the program crashes. A second window is refused at once, never kept waiting. The branch test passes. |
| CON-04 | CLOSED (code) | `archives.go:401-407`: when a record says "running" but nothing holds the archive's lock, the row shows its line. |
| CON-06 | CLOSED | `setplan.go:442`. The branch test passes, both by identity and by name. |
| CON-08 | CLOSED (code) | `addarchive.go`: the lead counts only archives it could list. A finished archive it could not reach gets its own line. |
| CON-09 | NOT CLOSED (INFO) | Not in the lead's fold. `archives.go:253` still reads the made-list before taking the lock. |
| CON-10 | CLOSED | My write-refused test: a folder never made now reads "Not made, so nothing was copied". For swapped folders, see RF-06. |
| CON-11 | NOT CLOSED (INFO) | Not in the fold. `setparent.go:202-217` makes the folder, reads it back, restricts it, then records it. The gap before it is recorded is slightly longer than before. |
| OUT-01 | CLOSED | `alert.go` `foldEncoded`. My test (4 cases, including 60 emoji): every header line is 78 octets or fewer, and the Subject decodes back to what was written. |
| OUT-02 | PARTLY CLOSED | `.app`, `.rtfd`, `.{GUID}` and `.APP` now get " (mail)" added. `.download` does not (RV-04). How Explorer shows `.{GUID}` folders: MECHANISM-ONLY, lab step below. |
| OUT-03 (= CON-07) | CLOSED | Each made-list line now keeps its store id (`setplan.go:391-396`). My test: bob no longer continues ann's folder; ann still does; a two-field line from before is still read. |
| OUT-04 | CLOSED | Names are compared in composed form (NFC), and the parent by identity (`setSameParent`). The skeptic's test passes on APFS. |
| F1 | CLOSED | The summary's last line, the end note's last line, and the added sentence in the "already holds" question. (The same folder is still named after a refused folder: RF-01.) |
| F2 | PARTLY CLOSED | The remedy is now in the summary and the Desktop note. But the summary then says "run this again … this same folder" (RF-01). |
| F3 | CLOSED | The missing folder is named, and the member that has not finished is named. A combined archive that lists only a missing folder gets no advice at all (lead's call; RF-08). |
| F4, F5, F6, F7, F8, F10, F13, F14 | CLOSED | Read in the code. The branch tests for each pass. |
| F9 | CLOSED | `setUnconfirmedWords` names the archive and gives each cause its own remedy. |
| F11 | CLOSED | The plan's words for "not found" still do not say where to click (RF-04). |
| F12 | PARTLY CLOSED | Fixed for "moved" and "removed". The new "reused" words repeat the same problem (RF-02). |

#### New problems

**RV-01 — MEDIUM, confirmed by a test. Thunderbird gives a removed account's folder to a new account, inside a set: the new account never gets an archive.**
- **Where:** `setplan.go:415-448`.
- **The sequence:**
  1. qzalice's archive is part of a set.
  2. qzalice's account is removed in Thunderbird, with its mail data.
  3. qzcarol is added on the same server and gets the same folder.
  4. qzalice's archive now stops with the "reused" words. That is correct.
  5. The set's re-run plans carol's mailbox into alice's archive: "No new archive: 1 brought up to date … Brought up to date: qzalice@qzr.example". The run stops again. "Run this again" repeats forever.
  6. After the advised Pause, the plan says "Paused, so qzalice@qzr.example is not being backed up now". Carol's address appears nowhere, and every set re-run is silent about her mailbox.
- **Why MEDIUM, not HIGH:** the stop itself is loud and its words give a way that works (choose the account on its own). It turns silent only after the Pause.
- **Smallest fix:** in `planOne`, before continuing and before the paused check, ask whether the matched child's run would stop as reused (`autoplace.ReusedByID` on its recorded identity). If it would, skip that child for this row: plan a new archive for carol, and list alice's archive as "kept as it is … another account uses its folder now; click Pause".
- **Red test:** `TestZZRVReusedFolderInASet`.

**RV-02 — MEDIUM, MECHANISM-ONLY (Windows), probably there before the fixes too. On a FAT or exFAT drive (common on USB sticks), every re-run makes a second copy of every archive.**
- **Where:** `setchild_windows.go:21-36`, `setparent.go:210`.
- **What goes wrong:**
  - These drives keep no access list, so the owner-only restriction fails silently (its error is ignored on purpose).
  - Windows then reports a "world" owner for every folder. `setChildMine` says the set's own folders are not the person's, so the plan never looks at them and makes "NAME (2)" for each mailbox, each with its own schedule.
  - The plan's line "a folder another account owns or can change" is also untrue there.
- **macOS exFAT is fine:** I checked with a real exFAT image on this Mac. The re-run brought all 4 archives up to date.
- **Smallest fix:** on a volume without persistent access lists (`GetVolumeInformationByHandle`, `FILE_PERSISTENT_ACLS`), judge a child by "is a real folder", not by its owner. Or refuse such a parent, with words that say why.
- **Lab step:** make a set on an exFAT USB stick, run it again, and expect "Brought up to date" for each.

**RV-03 — LOW, confirmed by a test. CON-01's leftover when a re-run makes something new and the schedule question is answered No or Cancel.**
- **Where:** `setexport.go:707`.
- **What goes wrong:** the member with no scheduled backup is never mentioned. The summary shows the line only when there is nothing new to schedule, and No shows no end note. No Pause advice is given, so the HIGH harm cannot happen.
- **Fix:** add the "no scheduled backup now" lines to the summary whenever any member has that state.
- **Test:** `TestZZRVCON01AnsweredNo`.

**RV-04 — LOW, confirmed. OUT-02's list misses `.download`.**
- **Where:** `set.go:218`.
- **What goes wrong:** `.download` is a real domain ending. `mdls` on this Mac says a folder named `me@shop.download` is a package (`com.apple.package`), so Finder shows it as a single item, not a folder.
- **Fix:** add `.download` to the list. Consider `.band` too; I did not check `.band`.

**RV-05 — LOW, MECHANISM-ONLY (Windows), the lead's call. The owner-only access list blocks handing an archive over.**
- **Where:** `owneronly_windows.go:28`.
- **What goes wrong:**
  - The restriction gives access to the person alone, and new files inside inherit that.
  - The summary promises "you can hand over … one". On another Windows account or computer, an NTFS drive's set archive opens only after an administrator takes ownership. A single archive has no such limit.
  - Also, the restriction is applied by path, so it follows a junction planted in the instant after the folder is made. The worst case is that the person's own folder becomes owner-only. INFO.
- **Lab step:** run `icacls` on one set child and confirm that only claude-test is listed.

**RV-06 — INFO, MECHANISM-ONLY. How the identity behaves when the user name changes.**
- The identity is built from the fields `userName` and `hostname`.
- My test: editing the account's login (Thunderbird stores it as `realuserName`, `realhostname`, as I understand it) keeps the identity. The backup runs.
- If `userName` itself is rewritten, the archive stops with "a different account" words. My test confirms that stop. It is a false stop, but loud, and nothing is lost.
- The user name is not lower-cased, so an account re-added with different capital letters reads as "removed".
- **Lab step:** change the user name in Thunderbird's Account Settings, then read `prefs.js`.

**RV-07 — INFO.**
- Encrypted one-folder archives keep no identity (the owner's privacy rulings). INS-03 stays open for them. The design's kept risks should say so.
- On upgrade, old records keep the name rules until their first ok run, which then writes the identity of whoever owns the folder at that moment.

**RV-08 — INFO.** The fold says every confirmed finding is fixed, but CON-09 and CON-11 were not folded.

**Checked and fine:**
- The set lock is released if the program crashes. A Desktop window and a wizard window never both wait: the second is refused at once.
- An older two-field made-list line is still read.
- Encrypted records never carry `thunderbird_ids`: their record format allows no such field.
- An ok run while the settings could not be read for certain (a `user.js` present) kept the identity (my test).
- `setChildSame` uses the same same-folder check the plan already relied on. I found no new false refusal on case-folding drives. Network shares: MECHANISM-ONLY.

#### Friction

- **RF-01: the summary contradicts itself after a refused folder (my test).**
  - Now: "Allow MailArchive there (…), or run this again and choose another folder." Then: "To finish them, run this again: … and this same folder." Then the "Each archive is its own folder … Open index.html" lines, with nothing made.
  - Proposed: when a remedy is shown, end with "Then run this again: choose All of them, then One archive for each mailbox, and the folder you allowed or a new one." Leave out the "Each archive…" lines when no archive was made.
- **RF-02: the reused stop's words (`autoplace.go:395`) reach the failure email.** There, "its row" points nowhere, and "that account" is unclear.
  - Proposed: "…To back up the account that uses it now, click Make a new archive… in MailArchive Desktop and choose it; to stop this archive's backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
  - The reused pause email names no next step. Add the first half of the sentence above.
- **RF-03: the CON-06 plan line (`setwindow.go:71`).**
  - Now: "…kept as it is in FOLDER; Thunderbird no longer uses this folder. Click Pause on it…". "This folder" reads as the archive's folder, and it does not say where Pause is.
  - Proposed: "NAME: kept as it is in FOLDER, since Thunderbird no longer uses that mailbox's folder. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
- **RF-04: the plan's "not found" line (`setwindow.go:27`).**
  - Now: "; its mail stays in its archive; click Pause on its row to stop its backups."
  - Proposed: ". Its mail stays in that archive. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
- **RF-05: the "no scheduled backup now" lines (`setschedule.go:259-260`).** Name the buttons.
  - "…: in MailArchive Desktop, click Switch to this archive on its row, then Repair on its Scheduled backup card."
  - "…: it is backed up only when you click Run now on its Scheduled backup card in MailArchive Desktop."
- **RF-06: a swapped folder (INS-02).**
  - A continued archive reads: "…so nothing was written into it. The mail copied so far is kept." Drop the second sentence for this reason.
  - A new folder reads "Not made, so nothing was copied:" although the folder was made. Use "Stopped before copying:".
- **RF-07: the plan's "Brought up to date: FOLDER (already in this folder)".** It reads as done before anything ran (F11's own point), and it never names the mailbox (this hid carol in RV-01).
  - Proposed: "To bring up to date: FOLDER, for MAILBOX."
- **RF-08 (lead's call, F3):** a combined archive that lists only a missing folder now ends with no advice at all, so the person keeps backing up twice with no guidance.
- **RF-09:** the Desktop's "To finish it, click Make a new archive… again, choose One archive for each mailbox…" leaves out "All of them, then". This was there before the fixes.

#### Windows lab steps to add
- Steps for RV-02 (exFAT stick) and RV-05 (`icacls`) above.
- The skeptic's two original steps (INS-01, OUT-02).
- The user-name edit for RV-06.

#### Throwaway tests (untracked)
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-rv/internal/wizard/zz_rv_pma_test.go`
  - 3 tests are meant to fail: ReusedFolderInASet, WriteRefusedSummary, CON01AnsweredNo.
  - The exFAT probe skips unless `ZZRV_EXFAT` is set.
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-rv/internal/app/zz_rv_pma_test.go`
- `~/repositories/mail-archive-tool/.claude/worktrees/pma-rv/internal/alert/zz_rv_pma_test.go`
- Delete all three before any unfiltered `go test ./...`. They have no covers markers, so the covers meta-test would fail.

**How this was done:**
- This is a finding report: my own judgement, with test output as evidence.
- I ran no test at 443c2777. "Red before" comes from the skeptic's record of the same tests.
- Not checked on a real machine: Windows access lists on FAT drives, Explorer, Thunderbird's own prefs writes, mail servers.
- The smallest check you could do: the exFAT re-run on the test PC.

Counters: Register: finding report. Borrowed lines: product words only. Drift: none. Touched: 3 untracked test files; temporary probe folders and an exFAT image, all removed. Persona: none. Sources reached: the code at ce5b6fbd, yes; the design, revision 3 only; Windows behaviour, no; macOS package typing, yes (mdls).

