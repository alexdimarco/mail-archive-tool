*Filed by the lead, 2026-11-18. The report below is the reviewer's, word for word: an independent
Claude agent that wrote no part of the change, walking commit 9e3feae on branch
`picker-account-names` (diff from cee7259). The lead's fold follows it.*

**Verdict: SHIPPABLE.** The list now names each account the way Thunderbird does, so the owner's three Google accounts show as three addresses. Four medium fixes should land before the public release. Two high-cost problems sit in the steps just before and after the list, which this change did not touch.

# Friction review: the wizard's list of found mailboxes (S85, MA-1462 to MA-1467)

Reviewer: an independent Claude agent (Opus 5.5), not the change's author, new to this change. Walked 18 November 2026 on the Mac, in this review's own worktree at 9e3feae. Process: `../assurance-kit/process/friction-review.md`. Also read:
- the change (`git diff cee7259 9e3feae`): `internal/wizard/picker.go` in full, the removed `pickAutoInputs` and `autoInputLabel`, `state.TBTextKey`, `thunderbird.NoAccountKindOf`, and the three new test files;
- catalog rows S85 and MA-1462 to MA-1467;
- the code around the list: `localArchiveFlowHooks`, `prepareThunderbirdGUI`, `imapPrepQuestion`, `imapPrepAccount`, `guiWaitForSync` and `AddArchive` in `internal/wizard/wizard.go`; `internal/desktop/addarchive.go`; discovery in `internal/app/run.go`; `ReadStoreFacts` and the account match in `internal/thunderbird`; `source.IsMailStoreDir` and Evolution's store names;
- `docs/ux-contract.md`, and `docs/review-thunderbird-completeness-c3-friction.md` for this review's shape.

Where Windows' list differs, it is weighed first (it is the biggest user group).

**Verdict: SHIPPABLE.**

For the owner's own case the change does what was asked:
- Three Google accounts on one server used to read "imap.gmail.com", "imap.gmail-1.com" and "imap.gmail-2.com". They now read as their three addresses, each with how much of it is on this computer.
- The university account reads as its address instead of "outlook.office365.com".
- In all 17 cells walked, every row picks its own mailbox and no row reads like "All of them" or like a dialog option.
- On Linux, clicking OK with nothing selected asks again.

Nothing in the change's design (Type II) blocks shipping. Four findings are medium. All four are buildable (Type III), so the kit puts them in the backlog that is built before ship:
- **F1:** one address kept in two mail programs (Outlook and Thunderbird on Windows) now reads alike. This is the one case the change makes slightly worse.
- **F2:** one address in two Thunderbird profiles is told apart by a folder name no one sees, and not at all when a profile was copied.
- **F3:** a folder the list calls "Not in Thunderbird now" is still asked to prepare a download.
- **F4** (an estimate until the Windows screenshot): Windows cuts rows at about 38 characters. That hides the size and can make two rows look the same.

Two high-cost problems sit just before and after the list. This change did not touch them (F12, F13):
- After "All of them", which is preselected, the wizard asks the same unnamed question once per account: five times for the owner. The person again cannot tell which account is which.
- On Windows, the "Use the Outlook app" question can skip the list altogether.

Neither is this change's doing, but both undo part of its point, and the owner's default path runs straight into F12.

## Header

| | |
|---|---|
| Reviewed commit | 9e3feae (detached), diff from cee7259. Worktree `.claude/worktrees/picker-fric`, used by this review alone |
| Environment | macOS (Darwin 25.6.0), Go 1.26.0. Load 3.83 and 53% memory free before the first run; no other test run going; one run at a time |
| Method | A throwaway test file, `internal/wizard/zz_friction_picker_test.go`, left untracked in the worktree and not committed. It builds made-up Thunderbird, Outlook and Evolution folders in `t.TempDir()`, with sparse files for the sizes. For each it prints `pickList(gatherPickRows(found, time.Now().Add(time.Minute), sizeEntryMax), goos)` for darwin, windows and linux. Every row is mapped back through `pickChoice`. In C1 and C3 every row was also picked through `pickAutoInputs`, and in C10 so was All, with the `listDialog` and `messageDialog` seams replaced by the existing `pickScript` recorder. `imapPrepAccount` was called only to read the words a refusal shows. The windows after the list were read in the code, not run. Invented addresses only (`qz…`, or `.example` domains) |
| Windows widths | **An estimate.** The list is taken as 241 pixels wide (MA-1467 and the brief), in Segoe UI 9 pt at 96 DPI, with character widths recalled, not measured. That leaves about 235 pixels for text, or about 218 with a scroll bar (more than about 10 rows). Rows the estimate cuts are marked "shows …" |
| Commands and exit status | `git status`, `git diff --stat cee7259 9e3feae` and `git diff cee7259 9e3feae -- …`: exit 0. `sysctl -n vm.loadavg`: 0. `memory_pressure -Q`: 0. `pgrep -fl "go test"`: 1 (none running). `go env GOTOOLCHAIN GOVERSION`: 0. `go vet ./internal/wizard/`: 0. `go test -count=1 -run 'TestZZ01' -v ./internal/wizard/`, then the same for `TestZZ02` through `TestZZ14`, one at a time: each 0, PASS. `TestZZ04` and `TestZZ06` were run again after two invented addresses were changed to the `qz` style: 0, PASS |
| Baseline digest | Not taken: the full suite was not run (the brief forbids it) |
| Cells walked | 17 (C1 to C15, plus C3b and C6b) |
| Functioning | 7 fully (C1, C6a, C7, C8, C11, C12, C15). 8 partly (C2, C3, C4, C5, C6b, C9, C10, C13). 2 not (C3b, an estimate; C14) |
| High-friction cells | 5: C3 and C10 (caused by the windows before and after the list, which this change did not touch), C3b (estimate), C5, C14 |
| Not checked | No real screen, on purpose: see §3 |

## 1. The cells

Rows are copied as printed. "➤ All of them (and any added later)" is the first row of every list and is left out below. "Before" is what the list at cee7259 said for the same folders (`autoInputLabel`, rebuilt in the walk file). Sizes are macOS's unless marked Windows.

| Cell (actor × scenario) and the rows printed | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| **C1. The owner on macOS.** Three Google accounts on imap.gmail.com (folders imap.gmail.com, imap.gmail-1.com, imap.gmail-2.com; one is a Workspace address on its own domain), one account on their own domain, a university Microsoft 365 account, Local Folders. Rows: `qzsam.walker@gmail.com (14 GB)` · `qzwalker.family@gmail.com (nothing downloaded yet)` · `sam@qzname.example (480 MB)` · `sam@qzwalkerdesign.example (6.3 GB)` · `samantha.walker@mail.qzuniversity.example (12 GB)` · `Local Folders (empty)`. Before: `imap.gmail-1.com` · `imap.gmail-2.com` · `imap.gmail.com` · `mail.qzname.example` · `outlook.office365.com` · `Local Folders` | Yes | One list, one click. The person only needs to know their own addresses, as Thunderbird's folder pane shows them. No dead end. Each row picked through `pickAutoInputs` gave its own folder. On Windows the same folders lose the end of 2 of 6 rows (estimate): "qzwalker.family@gmail.com (nothing downl" and "samantha.walker@mail.qzuniversity.exampl" | III | F4 |
| **C2. The owner renamed accounts in Thunderbird**, two of them "Gmail" and "gmail". Rows: `Design studio (6.3 GB)` · `imap.gmail-1.com (14 GB)` · `imap.gmail-2.com (nothing downloaded yet)` · `Sam (own domain) (480 MB)` · `University (12 GB)` · `Local Folders (empty)` | Partly | The two Gmail rows are back to the old folder words. MA-1287's rule says two accounts named alike are shown by their folders. Only the sizes tell them apart, though each account's login (its address) is in prefs.js. Rare: Thunderbird itself shows both as "Gmail" | III | F5 |
| **C3. A Windows user** with Outlook (a work .ost, Outlook.pst, archive.pst, a Gmail .pst) and Thunderbird (the same work address, the same Gmail, Local Folders). Windows rows: `archive.pst (3.9 GB)` · `Outlook.pst (763 MB)` · `qzsamantha.walker@gmail.com.pst (1.0 GB)` (shows "…gmail.com.pst (1.0 GB") · `samantha.walker@qzcontoso.example.ost (2.7 GB)` (shows "…example.ost (") · `qzsamantha.walker@gmail.com (7.2 GB)` · `samantha.walker@qzcontoso.example (1.8 GB)` (shows "…example (1.8 ") · `Local Folders (empty)`. Before: `samantha.walker@qzcontoso.example.ost` · `Outlook.pst` · `archive.pst` · `qzsamantha.walker@gmail.com.pst` · `imap.gmail.com` · `outlook.office365.com` · `Local Folders` | Partly | The same address in Outlook and in Thunderbird now differs only by ".ost" or ".pst". No row says "Outlook" or "Thunderbird", so the person must know that a name ending .ost or .pst is Outlook's. Before this change the Thunderbird row showed a server name, so the two at least looked different. Before the list (not changed here): with classic Outlook installed, "Use the Outlook app to export this account?" comes first. Its default button skips the list, so the Thunderbird accounts are never offered | III; II (F13) | F1, F4; F13 |
| **C3b. The same, with longer addresses.** Windows rows: `qzsamantha.walker.consulting@gmail.com.pst (1.0 GB)` (shows "qzsamantha.walker.consulting@gmail.com.") · `samantha.walker@mail.qzuniversity.example.ost (2.7 GB)` and `samantha.walker@mail.qzuniversity.example (1.8 GB)` (both show only "samantha.walker@mail.qzuniversity.exampl") · `qzsamantha.walker.consulting@gmail.com (7.2 GB)` (shows "qzsamantha.walker.consulting@gmail.com ") | No (estimate) | Two pairs of rows look the same on screen. One pair is identical; the other differs only by a final dot. The only clue is their position (Outlook files come first), and nothing tells the person that | III | F1, F4 |
| **C4. A Linux user** with Thunderbird and Evolution, one address in both. Rows: `qzsam.walker@gmail.com (3.0 GB)` · `sam@qzname.example (300 MB)` · `Local Folders (empty)` · `On This Computer (4.0 MB)` · `sam@qzname.example (200 MB)`. Before: `imap.gmail.com` · `mail.qzname.example` · `Local Folders` · `On This Computer` · `sam@qzname.example`. Nothing is preselected on Linux. Clicking OK with nothing selected showed "Nothing was chosen. Choose one line in the list, then click OK." and then the list again (2 lists, 1 note); the row picked next gave its own folder | Partly | Thunderbird's and Evolution's copies of the same address differ only by size, and nothing says which program is which. Before this change, the Thunderbird row showed its server name. The empty-answer note is plain and costs one extra window | III | F1 |
| **C5. One address in two Thunderbird profiles.** (A profile is the folder where Thunderbird keeps one set of accounts and mail.) An old "x7y8z9w0.default" and the "a1b2c3d4.default-release" in use. Rows: `qzsam.walker@gmail.com (profile default-release, 14 GB)` · `qzsam.walker@gmail.com (profile default, 6.3 GB)` · `sam@qzname.example (480 MB)` · `Local Folders (profile default-release, empty)` · `Local Folders (profile default, 300 MB)`. Before: `imap.gmail.com` · `mail.qzname.example` · `imap.gmail.com (2)` · `Local Folders` · `Local Folders (2)`. On Windows, with a longer address, both rows show only "qzsamantha.walker.consulting@gmail.com " (estimate) | Partly | Better than before, but the person must know which profile Thunderbird opens. "default-release" is a folder name Thunderbird's own window never shows. A wrong pick is not caught as one: the archive backs up the copy Thunderbird no longer uses (see F2) | III | F2, F4 |
| **C6a. An account removed and added again.** Thunderbird keeps the new one in imap.gmail-1.com and leaves the old imap.gmail.com behind. Rows: `qzsam.walker@gmail.com (nothing downloaded yet)` · `Local Folders (empty)` · `Not in Thunderbird now: imap.gmail.com (6.3 GB)`. Before: `imap.gmail-1.com` · `imap.gmail.com` · `Local Folders` | Yes | Plain, true, and listed last. But picking it, or All, brings "This looks like an IMAP account. … Prepare a full offline download first?" for that folder. "Yes, prepare" then shows four paragraphs and three numbered steps, beginning "MailArchive could not find a Thunderbird account for the mail in imap.gmail.com, so it did not change any Thunderbird setting." That is two windows that cannot help | III | F3 |
| **C6b. The same, with three Google accounts live** (imap.gmail-1.com, -2 and -3) and the old imap.gmail.com. Rows: `imap.gmail.com (6.3 GB)` · `qzsam.walker@gmail.com (14 GB)` · `qzwalker.family@gmail.com (nothing downloaded yet)` · `sam@qzwalkerdesign.example (1.2 GB)` · `Local Folders (empty)` | Partly | The old folder sits among the addresses (first, by its letters) under the old folder words, with nothing to say why. MailArchive cannot say whose it is: when two or more live accounts use that server, it could be either's (C0c's rule, `internal/thunderbird/thunderbird.go:667-674`). The rule is the control; the missing reason is the friction | III (the rule itself I) | F6 |
| **C7. Thunderbird in French, German and Russian.** Thunderbird names Local Folders in its own language when the profile is made. French: `qzsam.walker@gmail.com (3.0 GB)` · `Account École (enseignants) (700 MB)` · `Blogs et actualités (3.0 MB)` · `Dossiers locaux (2.1 GB)`. German: `qzsam.walker@gmail.com (3.0 GB)` · `Account Übersee-Büro (700 MB)` · `Blogs & News-Feeds (3.0 MB)` · `Lokale Ordner (2.1 GB)`. Russian: `qzsam.walker@gmail.com (3.0 GB)` · `Account Работа (700 MB)` · `Account Блоги и новости (3.0 MB)` · `Account Локальные папки (2.1 GB)` | Yes | Local Folders and the feeds read as that Thunderbird shows them. A name starting with a letter outside A to Z gets the English "Account " in front and sorts after Z. In Russian that is every row except Gmail, including Local Folders and the feeds. Cosmetic | I | F8 |
| **C8. New accounts with nothing downloaded yet:** IMAP, Thunderbird's own Exchange account (kept under Mail/), POP. Rows: `qzsam.walker@gmail.com (nothing downloaded yet)` · `sam.walker@qzisp.example (empty)` · `samantha.walker@qzcontoso.example (nothing downloaded yet)` · `Local Folders (empty)` | Yes | Plain and true. The IMAP row leads to the offer to prepare a download. The Exchange row gets no offer, because the wizard asks only for folders under ImapMail/ (`wizard.go:861`, not changed here). A person who picks it gets an empty export and no next step | III | F14 |
| **C9. A POP account** whose mail goes to Local Folders (Thunderbird's "Global Inbox"), and one that keeps its own. Global Inbox: `qzsam.walker@gmail.com (3.0 GB)` · `sam.walker@qzisp.example (empty)` · `Local Folders (3.5 GB)`. Own Inbox: `qzsam.walker@gmail.com (3.0 GB)` · `sam.walker@qzisp.example (3.5 GB)` · `Local Folders (empty)` | Partly | With Global Inbox, the person's own address says "empty" and nothing says its mail is in Local Folders, so picking the address gives an empty archive. Not checked: whether Thunderbird leaves summary files in such a folder. If it does not, the address has no row at all, and the person must know that POP mail lives in Local Folders | III | F7 |
| **C10. The owner picks "All of them"** (preselected) with five IMAP accounts; rows as in C1. Then come five questions, word for word the same ("This looks like an IMAP account. …"), in the folders' order: imap.gmail-1.com, imap.gmail-2.com, imap.gmail.com, mail.qzname.example, outlook.office365.com. Those are rows 1, 2, 4, 3 and 5 of the list | Partly (the list: yes; the next step: no) | The person cannot tell which account each question is about, and counting rows gives the wrong answer for questions 3 and 4. Each "Yes, prepare" can add "Please QUIT Thunderbird completely…" and a sync window, "Start Thunderbird, then right-click the account → Download / Sync Now.", which names no account and closes at least 45 seconds after the download stops. That is up to 15 windows. Not changed by this change | III | F12 |
| **C11. Names a person chose:** "Gmail" and "gmail", "💼 Work", "Élise Walker", "Почта", and a 127-character name. Rows: `imap.qz1.example (700 MB)` · `imap.qz2.example (1.4 GB)` · `Walker family shared mailbox Walker family shared mailbox Walker family shared m… (4.2 GB)` · `Account Élise Walker (2.8 GB)` · `Account Почта (3.5 GB)` · `Account 💼 Work (2.1 GB)` · `Local Folders (empty)` | Yes | Every row is one mailbox and maps back to it. The two Gmails show as folder words, as in C2; the "Account " lead words are as in C7 | III; I | F5, F8 |
| **C12. Twelve accounts on Windows** (14 rows). About 10 show at once, with a scroll bar for the rest (estimate). With the scroll bar, 5 of the 12 address rows lose their end: the whole size in one (`samantha.walker@mail.qzuniversity.example (6.1 GB)` shows "samantha.walker@mail.qzuniversity.exa"), the unit in three (`qz.billing@qzwalkerdesign.example (7.3 GB)` shows "…(7.3 ", `qz.treasurer@qzhockeyclub.example (15 GB)` shows "…(15", `qzsam.walker@qzoutlook.example (8.5 GB)` shows "…(8.5 "), and the closing bracket in one (`qzsam.walker@qzyahoo.example (11 GB)`) | Yes (estimate) | The start of every address shows, so each row can still be told apart. The person scrolls to reach the last rows; Local Folders is last | III | F4 |
| **C13. The owner's profile with a user.js line Thunderbird never writes** (a hand-edited settings file). Rows: `imap.gmail-1.com (14 GB)` · `imap.gmail-2.com (nothing downloaded yet)` · `imap.gmail.com (6.3 GB)` · `Local Folders (empty)` · `mail.qzname.example (480 MB)` · `outlook.office365.com (12 GB)` | Partly | The old list again, with sizes, and nothing says why. A settings file MailArchive cannot read with certainty gives no account, so no name. That is C0c's rule, and right as a rule. Local Folders now sorts among the accounts | III (the rule itself I) | F6 |
| **C14. Ubuntu's move to the Thunderbird snap copied the profile:** the same profile folder under ~/.thunderbird (the old copy) and ~/snap/thunderbird/common/.thunderbird (the one in use). Rows: `qzsam.walker@gmail.com (profile default-release, 6.3 GB)` · `qzsam.walker@gmail.com (profile default-release, 6.3 GB) (2)` · `Local Folders (profile default-release, empty)` · `Local Folders (profile default-release, empty) (2)`. Before: `imap.gmail.com` · `Local Folders` · `imap.gmail.com (2)` · `Local Folders (2)` | No | Both copies have the same profile name, so the rows differ only by "(2)". The first row is the old copy. As in C5, a wrong pick is not caught as one | III | F2 |
| **C15. MailArchive Desktop, "Make a new archive…"**: the same list from the same code (`AddArchive`, `wizard.go:701-709`, and the encrypted add's flow, `internal/wizard/addkeyed.go:47`, both reach `pickAutoInputs` at `wizard.go:843`). Rows as in C1 | Yes | Cancel at the list gives "No archive was added." Nothing marks a mailbox that already has an archive. Your archives shows each archive by its folder, not by the mail in it, so it is easy to make a second archive of the same mail. On Linux, ten empty answers end with "Could not add an archive: The list of mailboxes did not say which one was chosen, so nothing was exported. Start MailArchive again and choose one line in the list.", though MailArchive is already running | III | F9, F11 |

## 2. Findings

Grades:
- **High:** on a common path, the person cannot get what they came for, or loses track of which account is which.
- **Medium:** fix before the public release.
- **Low:** the lead's call.

### In this change

**F1 (medium, Type III). One address kept in two mail programs reads alike.**
- Where: `internal/wizard/picker.go:422-441` (`pickRowText`), `:101` (`leadWord`), `:398` (`reservedStarts`). Cells C3, C3b, C4.
- What the person sees: on Windows, a person moving from Outlook to Thunderbird has both. "samantha.walker@qzcontoso.example.ost (2.7 GB)" and "samantha.walker@qzcontoso.example (1.8 GB)" differ only by ".ost". With a longer address, both show only "samantha.walker@mail.qzuniversity.exampl" (estimate). On Linux, Thunderbird's and Evolution's "sam@qzname.example" differ only by size. No row says which program it belongs to.
- Why it matters: before this change the Thunderbird row showed its server name, so the two looked different by accident. This is the one case the change makes harder.
- Fix: when the list holds mailboxes from more than one program, start each Outlook row with "Outlook: " and each Evolution row with "Evolution: ". Leave Thunderbird rows as they are. Add both words to `reservedStarts`, so a Thunderbird account named "Outlook: x" still starts "Account ". The rows then read:
  - `Outlook: samantha.walker@qzcontoso.example.ost (2.7 GB)`
  - `samantha.walker@qzcontoso.example (1.8 GB)`
  - `Evolution: sam@qzname.example (200 MB)`

  The program is then at the start of the row, which Windows never cuts.

**F2 (medium, Type III). Two Thunderbird profiles are told apart by a folder name no one sees.**
- Where: `internal/wizard/picker.go:365-379`, `:385-394` (`profileHint`), `:431-433`. Cells C5, C14.
- What the person sees: "(profile default-release, 14 GB)" next to "(profile default, 6.3 GB)" asks the person which profile Thunderbird opens, which Thunderbird's own window never shows. When Ubuntu's snap copied a profile (C14), both copies have the same folder name. The hint is then the same word twice, and the rows differ only by "(2)".
- Why it matters: a wrong pick backs up the copy Thunderbird no longer uses. At most, once that copy's count is over 3 days old, the end of the export says "…It has not counted since, so newer mail may not be here yet. Open Thunderbird and leave it open so it can count again." (`internal/state/tbwords.go:251`). Opening Thunderbird opens the other copy, so that advice does not help.
- Fix: instead of the profile's folder name, say when that copy's mail last changed on this computer. `storeSize` already visits every file it counts, so it can keep the newest modification time of those same files (no .msf, no .dat). The words go right after the size:
  - `qzsam.walker@gmail.com (14 GB, last changed today)`
  - `qzsam.walker@gmail.com (6.3 GB, last changed Mar 2, 2024)`

  Dates read "today", "yesterday", "Nov 3" within this year, and "Mar 2, 2024" before it. Keep the profile's name only as a tie-breaker, when both dates fall on the same day.

**F3 (medium, Type III). A folder the list says is not in Thunderbird is still asked to prepare a download.**
- Where: `internal/wizard/wizard.go:857-866` (the loop asks for every folder under ImapMail/), `internal/wizard/picker.go:181-182`. Cell C6a, and every "All of them" that includes such a folder.
- What the person sees: the list knows for certain that no account uses the folder (MA-1463). Yet the next window says "This looks like an IMAP account. … Thunderbird may not have downloaded every message to this computer yet… Prepare a full offline download first?" "Yes, prepare" then ends in four paragraphs that say this may be a leftover folder with nothing to turn on.
- Fix: skip the question for a folder the strict match says no live account uses. Use the same test the list makes: `thunderbird.NoAccountKindOf` on `ReadStoreFacts`' error gives `NoAccountNone`. No new words are needed.

**F4 (medium, Type III; an estimate until MA-1467's Windows screenshot). Windows cuts rows at about 38 characters, with no sign that it did.**
- Where: `internal/wizard/picker.go:422-441`. Cells C1, C3, C3b, C5, C12.
- What the person sees: the size is at the end of each row, so it is the first thing lost. "samantha.walker@mail.qzuniversity.example (12 GB)" shows "samantha.walker@mail.qzuniversity.exampl". "qzwalker.family@gmail.com (nothing downloaded yet)" shows "qzwalker.family@gmail.com (nothing downl". With more than about 10 rows, a scroll bar takes about 3 more characters (C12: 5 of 12 rows cut). Rows that differ only at their end look the same (C3b, and C5 with a long address). The prompt itself fits on one line (about 210 of 241 pixels).
- Fix, once the screenshot gives the real width: on Windows only, cut a long row's name at its end with "…", but never its brackets, so the row fits. By this estimate that is about 36 characters. Keep at least the first 20 characters of the name, then make the rows unique as today. Example: `samantha.walker@mail.qzuniv… (12 GB)`. With F1 and F2 in place, the program and the date then stay on screen too.

**F5 (low, Type III). When the account's name cannot be used, the row falls back to the folder, though the login is the address.**
- Where: `internal/wizard/picker.go:318-347`, `:355`. Cells C2, C11.
- What the person sees: two accounts the person named alike ("Gmail" and "gmail") show as "imap.gmail-1.com (14 GB)" and "imap.gmail-2.com (nothing downloaded yet)", following MA-1287's rule. For those two rows, that is the owner's original complaint. An account with no name line reads the same way.
- Why it is fixable: prefs.js also holds each account's login (`mail.server.<key>.userName`). For Gmail, Microsoft 365 and most providers, the login is the address.
- Fix: for such a Thunderbird row, show the login when it contains "@". Put it after the name when there was one:
  - `Gmail (qzsam.walker@gmail.com, 14 GB)`
  - `gmail (qzwalker.family@gmail.com, nothing downloaded yet)`
  - with no name line: `qzsam.walker@gmail.com (14 GB)`

  Otherwise show the folder, as today. `ReadStoreFacts` would carry the login beside the name, from the same strict read, so a file that is not read with certainty still gives no login.

**F6 (low, Type III). A Thunderbird folder whose account cannot be told gives no reason.**
- Where: `internal/wizard/picker.go:183-185`, `:355-356`. Cells C6b, C13.
- What the person sees: the bare folder words, "imap.gmail.com (6.3 GB)", sorted among the addresses. In C13 the whole list is the old one again.
- Fix: such a row says so first in its brackets: `imap.gmail.com (account not known, 6.3 GB)`. In C13 every row then says it, which is true.

**F7 (low, Type III). Nothing says when a POP account's mail is kept in Local Folders.**
- Where: `internal/wizard/picker.go:173-180`. Cell C9.
- Fix: read the account's `mail.server.<key>.deferred_to_account` setting (same read). The POP row then says where its mail is, and Local Folders' row says whose mail it holds:
  - `sam.walker@qzisp.example (its mail is in Local Folders)`
  - `Local Folders, with sam.walker@qzisp.example's mail (3.5 GB)`

**F8 (low, Type I: counted, not faulted). "Account " in front of names that start outside A to Z.**
- Where: `internal/wizard/picker.go:405-417`; the sort order at `:454-463`. Cells C7, C11.
- Why it is there: this is MA-1464's guard. A row whose name starts with anything but an ordinary letter or digit could look like the All row or a dialog option, or flip the line's direction, so it gets a lead word.
- What it costs: cosmetic only. "Account École (enseignants) (700 MB)", and in a Russian Thunderbird, "Account Локальные папки (2.1 GB)" for Local Folders. Such rows also sort after Z.
- No change needed. If the lead wants it lighter, let accented Latin letters through (É, Ü, Ø) and keep the lead word for every other script. The adversarial reviewer should see that first, since an accented capital A can pass for A.

**F9 (low, Type III). The give-up words name the wrong next step on the Desktop.**
- Where: `internal/wizard/picker.go:56`. Cell C15 (Linux only, after ten empty answers).
- Fix: `The list of mailboxes did not say which one was chosen, so nothing was exported. Try again, and click one line in the list before you click OK.` These words are true in both the wizard and the Desktop.

**F10 (low, Type II; recorded, no change proposed). After the pick, the archive uses the folder name.**
- Where: `internal/app/run.go:1843-1856`; catalog MA-1462's last sentence, and MA-1288.
- What the person sees: they pick "qzsam.walker@gmail.com", but the archive's folder for that mail and the run's log line say "imap.gmail-1.com". The release notes say so.
- This follows the owner's earlier rule that the list's words are shown, never written. Whether the archive's pages should show the account's name too is the owner's call.

**F11 (low, Type III, backlog). On the Desktop, nothing marks a mailbox that already has an archive.**
- Cell C15. Your archives shows each archive by its folder, so a person adding another archive cannot see which mail the existing ones hold, and can make a second archive of the same mail.
- Later: a row detail such as `(already in the archive ~/Archives/Mail)`, worked out from the known archives' job files.

### Before and after the list: not changed by this change, but costed for the person

**F12 (high, Type III). After "All of them", the next windows do not say which account they are about.**
- Where: `internal/wizard/wizard.go:857-873`, `:1551-1555`, `:1568-1573`, `:1725`, `:1737`. Cell C10.
- Why it matters: All is preselected, so this is the owner's default path. Five IMAP accounts give five identical questions, asked in the folders' order rather than the list's. Each "Yes, prepare" can add a request to quit Thunderbird and a sync window that says "right-click the account" without naming it. That is up to 15 windows, and the person may sync the wrong account while the wizard watches another. The change already gives the wizard the words it needs to fix this.
- Fix: ask in the list's order, and name the account (the row's words without the size) in both the question and the sync window. For several accounts:

  > Account 2 of 5: qzwalker.family@gmail.com
  >
  > Thunderbird may not have downloaded all of this account's mail to this computer yet: it downloads mail over time, and only while it is open. A message or attachment it has not downloaded yet is not in this export; a later run adds it once Thunderbird has it.
  >
  > Yes, prepare makes sure Thunderbird keeps all of this account's mail on this computer, then waits while it downloads.
  >
  > Prepare a full offline download of qzwalker.family@gmail.com first?

  With one account there is no "Account 1 of 1" line. In the sync window: "Start Thunderbird, then right-click qzwalker.family@gmail.com → Download / Sync Now." (That menu path comes from the old words and was not checked here.) The larger fix, later: one question and one Thunderbird quit for every account that needs it.

**F13 (high, Type II). On Windows, the Outlook-app question can skip the list.**
- Where: `internal/wizard/wizard.go:806-818`, `:820-822`, `:840-841`. Cell C3.
- What happens: when a .ost is found and classic Outlook is installed, "A live Outlook (.ost) account was found. … Use the Outlook app to export this account?" comes before the list. Its default button, "Use the Outlook app", switches to the Outlook-app export, and the list never opens. The Thunderbird accounts that were found are dropped without a word. The only way to reach them is "Read the files directly", and nothing says so. The question also says "this account" before the person has seen which one.
- Fix (a design change for the lead): ask it after the list, only when the person picked a .ost row, or All of them with a .ost in it, and name the file: "samantha.walker@qzcontoso.example.ost is Outlook's copy of a live account. Reading it directly can miss mail Outlook hasn't fully downloaded to this computer; having the Outlook app export it first is more reliable.\n\nUse the Outlook app to export this account?"
- Open design question: what the Outlook-app export should mean next to Thunderbird mailboxes picked with All, since it exports Outlook's whole profile.

**F14 (low, Type III). Thunderbird's own Exchange account gets no next step.**
- Where: `internal/wizard/wizard.go:861`. This is a path test: only folders under ImapMail/ get the question. Cell C8.
- What the person sees: "samantha.walker@qzcontoso.example (nothing downloaded yet)" is a Thunderbird Exchange account kept under Mail/. Picking it gives an empty export with no offer and no explanation.
- Later: after the pick, for an Exchange account with nothing on this computer, one line: "Thunderbird has not downloaded any of this account's mail yet. Open Thunderbird and leave it open until it has, then run the backup again."

### Against the UX contract

- **X11 (plain, action-first labels):** the rows meet it, except "profile default-release" (F2) and the folder words used where a name is missing (F5, F6). The next window's "This looks like an IMAP account." (F12) is jargon, outside this change.
- **X7 (no crash reaches the person):** no crash path seen. The list's give-up message and its note are plain words.
- **X17 (one program name):** the give-up message says "MailArchive".
- **MA-1288 (the list's words are written nowhere):** read in `TestThePickersWordsAreWrittenNowhere`, not re-run here.

### Noted, no change

- The owner's case now reads as Thunderbird's folder pane does, and the university account shows its address.
- "Not in Thunderbird now:" is plain and listed last, and "All of them" still includes that folder (R1).
- "nothing downloaded yet" and "empty" are plain and true. Sizes use each system's own units, so the same folder is "14 GB" on the Mac and "13 GB" on Windows, matching what Finder and File Explorer say.
- "Which mailbox do you want to export?" fits on one line on Windows (about 210 of 241 pixels, estimate).
- Every row in all 17 cells maps back to its own folder. In C1 and C3 every row also did through `pickAutoInputs`, and in C10 All gave every folder found, with "and any added later" switched on.
- On Linux, an empty answer costs one plain note, and ten in a row end the step with plain words.
- Counting the sizes can hold the list back by up to 3 seconds (bounded). Every walked cell counted in about 0.01 seconds or less (made-up folders with few files).
- A Gmail account's size on this computer includes its All Mail and label folders, so it can be larger than the storage figure Gmail shows. The row does not claim otherwise.

## 3. Not checked

- **No real screen, on purpose.** No dialog was opened and nothing was built or run. The Windows cuts are an estimate (see the header). I did not know the Mac list's width, so no Mac cut is estimated. Whether "➤" draws on Windows, and how qarma and zenity draw the rows on Linux, were not seen. MA-1467 still owes all of these.
- **The dialog module's own code** (ncruces/zenity, kept outside the repo) was not read, per the brief's rule. Linux's "nothing preselected" rests on MA-1467 and on the argument test (`ma1464_picker_argv_unix_test.go`: the rows follow `--column=`).
- **The owner's real profile** was not read, on purpose. Two things decide whether the owner sees C1 or C13: whether each account there has a name line, and whether prefs.js and user.js read with certainty. The smallest check: the owner opens the list once on the Mac (MA-1467's macOS step).
- What Thunderbird leaves in a POP account's folder once it uses Global Inbox, and whether Global Inbox is the default for new POP accounts (C9).
- Thunderbird's Unified Folders account as a row: not built.
- The windows after the list were read in the code, not run, because `prepareThunderbirdGUI` opens real dialogs.
- The full test suite, which the brief forbids.

Two mistakes of my own, kept here:
- The walk's folder column prints only the last three parts of each path, so C14's two copies both print "m4n5b6v7.default-release/ImapMail/imap.gmail.com". They are two different folders; the sort by path puts the ~/.thunderbird copy first. Not a product finding.
- Two invented addresses I first used were on gmail.com without the repo's `qz` prefix, so a real person could own them. They were renamed, and C3 and C5 were run again before the rows above were copied.

## About this review

- **Date worked on:** 18 November 2026 (the brief's date, and this session's clock).
- **Register:** a finding (a friction review) with proposals. The severities and the Windows widths are my judgement and an estimate, and are labelled so where they appear.
- **Drew on:** the code and catalog listed at the top, read in this worktree. Every quoted row or sentence is the program's own words, either printed by the walk or copied from the code. Not reached: the dialog module's code, the owner's profile, a real screen.
- **The checks that settle what this review could not:** the Windows screenshot already being taken (it settles F4 and the estimates in C3b, C5 and C12), and the owner opening the new list once on the Mac.
- **Counters:**
  - Register: finding.
  - Borrowed lines: the program's words only.
  - Drift: none from the brief.
  - Touched: one untracked test file in this review's own worktree; nothing committed.
  - Persona: none.
  - Source reached: the change and its tests, yes; wizard.go around the list, yes; the module's code, no; the owner's profile, no; a real screen, no.

---

## Fold (the lead, 2026-11-18)

The kit asks for Type III findings to be built before ship. Those that this change caused, or that
its list makes worse, are built below, each with its own test and mutation in the round-2 sweep
(`export/mutate/sweep2.py`, not committed; results in the fold commit's message). The rest are
older than this change and are listed with what each waits on.

| Finding | Verdict | Change, or what it waits on | Test | Mutation |
|---|---|---|---|---|
| F1 | Fixed | Outlook's rows start "Outlook: " and Evolution's "Evolution: " when the mailboxes found belong to more than one of Outlook, Thunderbird and Evolution (the adversarial pass's SPF-03 too). The program's name sits at the row's start, which Windows never cuts. | `TestTwoProgramsSayWhichIsWhich` | N1 |
| F2 | Fixed, as proposed | The newest file counted gives each copy's "last changed" words (`storeSize`); the profile's short folder name only as a tie-breaker, its whole name when those match (SPF-02). | `TestOneAddressInTwoProfilesSaysWhichChangedLast` | N2, N3, N16 |
| F3 | Fixed, as proposed | The loop over the chosen mailboxes skips the IMAP question for a folder the strict match says no live account uses (`storeUnused`: the same `NoAccountKindOf` test the list makes). Anything less certain is asked about as before. MA-947's and MA-1267's pinned lines are unchanged. | `TestAFolderNoAccountUsesIsNotAskedToPrepare` (with a structural check of the loop) | N4, N5 |
| F4 | Pending MA-1467's screenshot | Not built: the width is an estimate. On 2026-11-18 neither the office Windows test PC (its claude-test account still refused the key) nor the Parallels VM (a cold start waits at the owner's PIN, which was not used) could show the list. The release notes say a very long address may be cut at its right end. F1 keeps the program's name in view, and F2's words help where the cut leaves the start of each row. | — | — |
| F5 | Later | Needs the login (`mail.server.<key>.userName`) as a new field of the reviewed reader, with its own adversarial look. | — | — |
| F6 | Not taken | The folder words are what the list said before; words saying the account is not known would worry the person with nothing to do about it. Backlog. | — | — |
| F7 | Later | Needs `deferred_to_account` from the reviewed reader. | — | — |
| F8 | No change (Type I) | MA-1464's guard. | — | — |
| F9 | Fixed, as proposed | "… so nothing was exported. Try again, and click one line in the list before you click OK." | `TestAnAnswerTheListDoesNotHoldIsAskedAgain` | N14 |
| F10 | The owner's call; recorded | Archive folders and logs keep the folder (MA-1288's rule). | — | — |
| F11 | Backlog | — | — | — |
| F12 | Needs the owner's word | Naming the account in the IMAP question (and asking in the list's order) changes two pinned wiring checks: MA-947's `choice := zenity.Question(` with `imapPrepQuestion` and MA-1267's `p, err := prepareThunderbirdGUI(in, &changed)`. The repo's rule is that a pinned test changes only with a human's word, so it is proposed for the next release with the reviewer's words. F3 already removes the question for folders no account uses. | — | — |
| F13 | The lead's design call, later | Moving the Outlook-app question after the list changes what "All of them" means beside Outlook's profile export. | — | — |
| F14 | Backlog | — | — | — |

---

## Re-walk after the fold (the reviewer, 2026-11-18)

*The same reviewer re-walked the cells the fold changed, on commit 2217a76. Its report, word for
word:*

**Verdict: SHIPPABLE.** The fold fixes F1, F3 and F9 on every system, and F2 on the Mac and Linux. Two new medium findings are both Windows estimates that belong with F4's pending screenshot: on Windows the "last changed" date is cut off the row, and the new "Outlook: " start can make two Outlook files look the same.

# Friction re-walk after the fold (2217a76)

Reviewer: the same independent Claude agent as the first walk, not the fold's author. Walked 18 November 2026 on the Mac, in this review's own worktree, checked out detached at 2217a76.

Read for this round:
- `git diff 9e3feae 2217a76`: `picker.go`, the question loop in `wizard.go`, `source.IsEvolutionStore`, the fold's tests, the catalog and the release notes;
- the fold table in `docs/review-picker-names-friction.md`;
- items SPF-01 to SPF-05, MAP-01 and FS-01 in `docs/review-picker-names-adversarial.md`.

**Summary.** On the Mac and Linux, the person can now tell the copy they use apart from an old one. The leftover folder of a removed account no longer gets a pointless question. The Desktop's give-up words are true.

On Windows, the date that tells the copies apart comes after the size, which is the part of the row Windows cuts (estimate). So F2 does not yet reach the biggest user group. The fold table's sentence "F2's words help where the cut leaves the start of each row" does not hold: in every two-profile row walked on Windows, the date was cut.

The fold also added a few new costs, all low or estimates: see R2-F1 to R2-F7.

## Header

| | |
|---|---|
| Reviewed commit | 2217a76 (detached), on top of 9e3feae; worktree `.claude/worktrees/picker-fric` |
| Method | The same walk file, `internal/wizard/zz_friction_picker_test.go` (untracked, not committed), updated as follows. `pickChoice` gets its third argument; on Linux each row is also tried as a non-UTF-8 locale would print it (`asciiForm`, transcoded). The list's clock `pickNow` is fixed at 18 Nov 2026 12:00 local time. File dates are set with `os.Chtimes` so the "last changed" words are realistic. The All-of-them count skips `storeUnused` folders, as the loop now does. Five new cells. No dialog opened, nothing built or run, invented addresses only. Windows widths are the same estimate as before (241-pixel list, recalled Segoe UI widths) |
| Commands and exit status | `git checkout --detach 2217a76`, `git diff --stat 9e3feae 2217a76`, `git diff 9e3feae 2217a76 -- …`, `git status --short`: exit 0. Another session's full suite (`go test -p 4 ./...`, process 42258) was running when I came to compile, so nothing of mine ran until it ended: I waited with a background wait and `caffeinate -w 42258` (both exit 0). Then `pgrep -l -x go`: 1 (none running). `sysctl -n vm.loadavg`: 0 (5.44). `memory_pressure -Q`: 0 (56% free). `go vet ./internal/wizard/`: 0. `go test -count=1 -run 'TestZZ<NN>' -v ./internal/wizard/` for NN = 04, 05, 06, 07, 14, 19, 15, 16, 17, 18, 20, 01, 02, 03, 08, 09, 10, 11, 12, 13: 20 runs, one at a time, each exit 0, PASS |
| Cells walked | 22: all 17 from the first walk, plus 5 new (R2-a to R2-e) |
| Functioning | 12 fully: C1, C3, C3b (estimate), C4, C6a, C7, C8, C11, C12, C14 (its usual state), C15, R2-b. 10 partly: C2, C5 (Windows, estimate), C6b, C9, C10, C13, R2-a (estimate), R2-c, R2-d, R2-e. 0 not. (First walk: 7 fully, 8 partly, 2 not) |
| High-friction cells | 6. C3 and C10 come from the windows before and after the list (F13, F12), unchanged by this change. C5 on Windows and R2-a are estimates. R2-d and R2-e are rare setups. (First walk: 5) |

## 1. The re-walked cells

Rows are copied as printed; "➤ All of them (and any added later)" is first in every list and left out below. Linux's words equal the Mac's in every cell, so they are given once. Windows rows marked "shows …" are cut there (estimate).

| Cell and the rows printed | Functions? | Finding | New friction |
|---|---|---|---|
| **C3. Windows, Outlook and Thunderbird.** Windows: `Outlook: archive.pst (3.9 GB)` · `Outlook: Outlook.pst (763 MB)` · `Outlook: qzsamantha.walker@gmail.com.pst (1.0 GB)` (shows "Outlook: qzsamantha.walker@gmail.com.ps") · `Outlook: samantha.walker@qzcontoso.example.ost (2.7 GB)` (shows "Outlook: samantha.walker@qzcontoso.exa") · `qzsamantha.walker@gmail.com (7.2 GB)` · `samantha.walker@qzcontoso.example (1.8 GB)` (shows "…example (1.8 ") · `Local Folders (empty)`. Mac and Linux: the same words, with 4.2 GB, 800 MB, 1.1 GB, 2.9 GB, 7.7 GB and 1.9 GB. Each row picked through `pickAutoInputs` gave its own folder | Yes (estimate on Windows) | **F1 fixed**: each Outlook row says it is Outlook's, at the start of the row | Long Outlook rows now lose their ".ost"/".pst" and their size on Windows (R2-F2). F13's question still comes first (not changed) |
| **C3b. The same, with longer addresses.** Windows: `Outlook: qzsamantha.walker.consulting@gmail.com.pst (1.0 GB)` (shows "Outlook: qzsamantha.walker.consulting@g") · `Outlook: samantha.walker@mail.qzuniversity.example.ost (2.7 GB)` (shows "Outlook: samantha.walker@mail.qzuniversit") · `qzsamantha.walker.consulting@gmail.com (7.2 GB)` (shows "qzsamantha.walker.consulting@gmail.com ") · `samantha.walker@mail.qzuniversity.example (1.8 GB)` (shows "samantha.walker@mail.qzuniversity.exampl"), plus archive.pst, Outlook.pst and Local Folders as in C3 | Yes (estimate) | **F1 fixed**: the two pairs that looked the same in the first walk now differ at the start | — |
| **C4. Linux, Thunderbird and Evolution.** `qzsam.walker@gmail.com (3.0 GB)` · `sam@qzname.example (300 MB)` · `Local Folders (empty)` · `Evolution: On This Computer (4.0 MB)` · `Evolution: sam@qzname.example (200 MB)`. Clicking OK with nothing selected: 2 lists and 1 note, then the row gave its own folder | Yes | **F1 fixed**. "Evolution: On This Computer" is also clearer than before | — |
| **C5. One address in two profiles, the old copy untouched since 2024.** Mac and Linux: `qzsam.walker@gmail.com (14 GB, last changed today)` · `qzsam.walker@gmail.com (6.3 GB, last changed Mar 2, 2024)` · `sam@qzname.example (480 MB)` · `Local Folders (empty, last changed today)` · `Local Folders (300 MB, last changed Mar 2, 2024)`. Windows: "qzsam.walker@gmail.com (13 GB, last chan" and "qzsam.walker@gmail.com (5.9 GB, last cha"; with a longer address, both rows show only "qzsamantha.walker.consulting@gmail.com ". Both changed today (Mac): `qzsam.walker@gmail.com (14 GB, last changed today, profile default-release)` · `qzsam.walker@gmail.com (6.3 GB, last changed today, profile default)` · `Local Folders (empty, last changed today, profile default-release)` · `Local Folders (300 MB, last changed today, profile default)` | Mac and Linux yes; Windows partly (estimate) | **F2 fixed on the Mac and Linux.** Not on Windows: the date is cut on every two-profile row walked there | R2-F1 (Windows). The same-day rows are long, but are told apart |
| **C6a. An account removed and added again.** All three systems: `qzsam.walker@gmail.com (nothing downloaded yet)` · `Local Folders (empty)` · `Not in Thunderbird now: imap.gmail.com (6.3 GB)` (Windows says 5.9 GB). `storeUnused` is true for the leftover. "All of them" now asks 1 IMAP question (for imap.gmail-1.com), not 2 | Yes | **F3 fixed** | — |
| **C6b. The same with three live Google accounts.** Unchanged: `imap.gmail.com (6.3 GB)` · `qzsam.walker@gmail.com (14 GB)` · `qzwalker.family@gmail.com (nothing downloaded yet)` · `sam@qzwalkerdesign.example (1.2 GB)` · `Local Folders (empty)`. `storeUnused` is false, so the leftover is still asked, as the fold intends | Partly | F6: "not taken" in the fold | — |
| **C14. Ubuntu's snap copy of one profile.** Usual state (Linux): `qzsam.walker@gmail.com (6.3 GB, last changed Apr 20, 2024)` (the old ~/.thunderbird copy, listed first) · `qzsam.walker@gmail.com (6.4 GB, last changed today)` (the snap's) · `Local Folders (empty, last changed Apr 20, 2024)` · `Local Folders (empty, last changed today)`. Day of the move: `qzsam.walker@gmail.com (6.3 GB, last changed today, profile m4n5b6v7.default-release)` · `qzsam.walker@gmail.com (6.4 GB, last changed today, profile m4n5b6v7.default-release)` · `Local Folders (empty, last changed today, profile m4n5b6v7.default-release)` · `Local Folders (empty, last changed today, profile m4n5b6v7.default-release) (2)` | Yes in the usual state; partly on the day of the move | **F2 fixed** where this case lives (Linux) | R2-F5 (the day of the move). The old copy is listed first (R2-F1) |
| **C15. MailArchive Desktop, "Make a new archive…".** Rows as in C1 (unchanged). A list that keeps answering nothing: 10 lists, 9 notes, and the Desktop line "Could not add an archive: The list of mailboxes did not say which one was chosen, so nothing was exported. Try again, and click one line in the list before you click OK." | Yes | **F9 fixed** | — |
| **R2-a (new). Windows: one address in an .ost, an older .pst and Thunderbird.** Short address, all rows fit: `Outlook: qzsam@qzco.example.ost (2.7 GB)` · `Outlook: qzsam@qzco.example.pst (1.5 GB)` · `qzsam@qzco.example (1.8 GB)` · `Local Folders (empty)`. Real-length address: `Outlook: samantha.walker@qzcontoso.example.ost (2.7 GB)` and `Outlook: samantha.walker@qzcontoso.example.pst (1.5 GB)` both show only "Outlook: samantha.walker@qzcontoso.exa". By the same estimate, before the fold they showed "…example.ost (" and "…example.pst (" | Partly (estimate) | — | R2-F2 |
| **R2-b (new). Windows: Outlook, and a Thunderbird never set up (only an empty Local Folders).** `Outlook: archive.pst (3.9 GB)` · `Outlook: qzsamantha.walker@gmail.com.pst (1.0 GB)` (shows "…gmail.com.ps") · `Outlook: samantha.walker@qzcontoso.example.ost (2.7 GB)` (shows "…qzcontoso.exa") · `Local Folders (empty)`. With Outlook alone, there is no "Outlook: " | Yes | — | R2-F3 |
| **R2-c (new). A hand-edited profile beside a normal one** (Mac): `imap.gmail.com (6.3 GB)` · `Folder Local Folders (300 MB)` · `qzsam.walker@gmail.com (3.0 GB)` · `Local Folders (empty)` | Partly | — | R2-F6 |
| **R2-d (new). Linux with a locale that does not name UTF-8:** `Account Élise Walker (900 MB)` · `Account Письма (700 MB)` · `Account Работа (700 MB)` · `Local Folders (empty)`. Both Cyrillic rows come back as "Account ?????? (700 MB)", which maps to no row. The All row ("? All of them (and any added later)") and the other rows still map | Partly | — | R2-F7 |
| **R2-e (new). The copy in use keeps no mail on this computer** (Thunderbird set not to keep messages; its summary files rewritten today) beside an old copy with mail (Mac): `qzsam.walker@gmail.com (nothing downloaded yet, last changed Jan 5, 2023)` (the copy in use) · `qzsam.walker@gmail.com (6.3 GB, last changed Mar 2, 2024)` (the old copy) · `Local Folders (empty, last changed Jan 5, 2023)` · `Local Folders (300 MB, last changed Mar 2, 2024)` | Partly | — | R2-F4 |
| **Unchanged:** C1, C2, C7, C8, C9, C10, C11, C12 and C13, row for row as in the first walk, on all three systems. C10 still gives five identical IMAP questions (F12, waiting on the owner) | as before | — | None. The look-alike folding (SPF-05) and the right-to-left check (SPF-01) added a lead word to no ordinary name walked (C7, C11) |

## 2. New findings

Grades as before: medium = fix before the public release; low = the lead's call. All of these are Type III (buildable).

**R2-F1 (medium; Windows, estimate). On Windows the "last changed" date is cut, so F2 does not reach the biggest user group.**
- Where: `internal/wizard/picker.go:573-589` (`pickRowText`).
- What the person sees: the date sits after the size, which is the part Windows cuts first: "qzsam.walker@gmail.com (13 GB, last chan" next to "qzsam.walker@gmail.com (5.9 GB, last cha". With a real-length address the two rows show the same text. The copies are also listed by folder path, so the old one can come first (C14).
- Fix: start every copy except the most recently changed one with "Older copy: " (add "older copy" to `reservedStarts`), and list the newest copy first. On the same day, keep today's profile words.
  - `qzsam.walker@gmail.com (14 GB, last changed today)`
  - `Older copy: qzsam.walker@gmail.com (6.3 GB, last changed Mar 2, 2024)`

  On Windows the second shows "Older copy: qzsam.walker@gmail.com (5.9", so the word that matters is at the start. This relies on R2-F4, so that "newest" really is the copy in use.

**R2-F2 (medium; Windows, estimate; caused by the fold). "Outlook: " can make two Outlook files of one address look the same.**
- Where: `internal/wizard/picker.go:449-458`.
- What the person sees: the start costs 9 characters. An .ost and an older .pst of one real-length address both show "Outlook: samantha.walker@qzcontoso.exa", where before the fold ".ost (" and ".pst (" showed. More Outlook rows also lose their size (C3, R2-b).
- Fix, with F4 once the screenshot gives the real width: cut an Outlook file's name before its extension, keeping ".ost" or ".pst" and the brackets. At this estimate's 38 characters:
  - `Outlook: samantha.walke….ost (2.7 GB)`
  - `Outlook: samantha.walke….pst (1.5 GB)`

**R2-F3 (low). A Thunderbird that was never set up puts "Outlook: " on every Outlook row.**
- Where: `internal/wizard/picker.go:438-448`. Cell R2-b.
- Why it matters: an empty Local Folders makes Thunderbird count as a second program. No Thunderbird row can then be mistaken for an Outlook row, so the start only costs width on Windows.
- Fix: count Thunderbird only for a mail account's row, not for Local Folders or feeds alone (`case r.tb && r.group != groupTBLocal:`). The rows then read as they do for Outlook alone: `archive.pst (3.9 GB)` … `Local Folders (empty)`.

**R2-F4 (low). The date can point at the wrong copy.**
- Where: `internal/wizard/picker.go:263-283` (`storeSize`'s newest time leaves out .msf and .dat files). Cell R2-e.
- What happens: the date comes from mail files only. In a copy still in use, those can be empty and years old: an account set not to keep mail here, or a Local Folders nobody uses. Thunderbird meanwhile rewrites that copy's summary files. So the copy in use reads "nothing downloaded yet, last changed Jan 5, 2023", the old copy reads "6.3 GB, last changed Mar 2, 2024", and the old copy looks newer. The same thing gives "Local Folders (empty, last changed …)", which states a date for mail that is not there.
- Fix: take the date from the newest of all the folder's files, summaries and .dat files included (still left out of the size), and give no date where no file was found. Not checked in a running Thunderbird: that it rewrites a folder's summary whenever it syncs that folder. MA-1467's lab step should look. The rows then read:
  - `qzsam.walker@gmail.com (nothing downloaded yet, last changed today)`
  - `qzsam.walker@gmail.com (6.3 GB, last changed Mar 2, 2024)`

**R2-F5 (low). On the day of a snap move, the tie-breaker adds words that tell nothing.**
- Where: `internal/wizard/picker.go:483-500`. Cell C14.
- What the person sees: both copies have the same whole folder name, so both rows end "profile m4n5b6v7.default-release" (32 characters with no use), and the two Local Folders differ only by " (2)".
- Fix: when even the whole names match, say where each profile is kept: `(6.4 GB, last changed today, in the snap)` and `(6.3 GB, last changed today, outside the snap)`. With R2-F4 the snap's copy should read "today" from its first sync, so this tie should be rare.

**R2-F6 (low). "Folder Local Folders" loses the profile words.**
- Where: `internal/wizard/picker.go:419-433` and `:460-482`. Cell R2-c.
- What the person sees: the SPF-04 lead word makes the two Local Folders rows start differently, so the profile rule never treats them as alike. "Folder Local Folders (300 MB)" sits among the accounts with nothing saying it is another profile's.
- Fix: keep the lead word, but give the date (and, on the same day, the profile) words when two Thunderbird rows of different profiles differ only by that lead word:
  - `Folder Local Folders (300 MB, last changed Mar 2, 2024)`
  - `Local Folders (empty, last changed today)`

  Rare: it needs a second profile that cannot be read with certainty.

**R2-F7 (low; Linux with a locale that is not UTF-8). Two rows can become impossible to choose.**
- Where: `internal/wizard/picker.go:597-625` (the uniqueness pass) and `:635-660`. Cell R2-d.
- What happens: rows that differ only in letters outside ASCII, with the same size, come back alike ("Account ?????? (700 MB)"). Following MAP-01's rule, they map to no row. The person gets "Nothing was chosen…", which is untrue, and after 10 tries the step ends. Neither mailbox can be picked alone.
- Fix: on Linux and the BSDs, also make rows unique by their ASCII form, adding " (2)" as for rows alike: `Account Письма (700 MB)` and `Account Работа (700 MB) (2)`. Every row then comes back as exactly one row.

## 3. Not checked

- **No real screen.** All Windows cuts are the same estimate as the first walk, and F4, R2-F1 and R2-F2 all wait on MA-1467's screenshot.
- **Thunderbird's own behaviour**, which R2-F4's fix assumes: that it rewrites the summary file of a folder it syncs. Also whether Ubuntu's snap move keeps the copied files' times (C14).
- **The dialog module's code** (outside the repo, not read), the owner's profile (not read), and the full suite (another session ran one during this walk; mine was not run).

## About this review

- **Date worked on:** 18 November 2026.
- **Register:** a finding (a friction re-walk), with proposals. The severities and the Windows widths are my judgement and an estimate, and are labelled so where they appear.
- **Drew on:** the fold's diff, its tests, the fold table and the adversarial review, all read in this worktree. Every quoted row is the program's own output from the walk. Not reached: a real screen, the dialog module's code, the owner's profile.
- **Smallest check for the owner:** the Windows screenshot already being taken. Have it show one list with two copies of one address, and an .ost and a .pst of one long address. That settles R2-F1 and R2-F2 together with F4.
- **Counters:**
  - Register: finding.
  - Borrowed lines: the program's words only.
  - Drift: none from the request; one wait for another session's test run, by the one-run-at-a-time rule.
  - Touched: the untracked walk file in this review's own worktree; nothing committed.
  - Persona: none.
  - Source reached: the fold's code and tests, yes; a real screen, no; the module's code, no; the owner's profile, no.

## Fold, round 2 (the lead, 2026-11-18)

Built on top of the adversarial pass's round-2 fold, each with its own test and mutation in the
round-3 sweep (results in the commit's message).

| Finding | Verdict | Change, or what it waits on | Test | Mutation |
|---|---|---|---|---|
| R2-F1 | Fixed, as proposed | A copy changed on an earlier day than another copy of the same words starts "Older copy: ", which Windows never cuts; the newest copy is listed first. Copies changed the same day keep the date and profile words, with no "Older copy". "Older copy" is a reserved row start. | `TestOneAddressInTwoProfilesSaysWhichChangedLast`; `TestEveryRowIsOneSourceNeverTheAllRowNeverAnOption` ("Older copy: qz") | O1, O2, O7, O8 |
| R2-F2 | Waits on MA-1467, with F4 | The cut before the extension needs the real width. | — | — |
| R2-F3 | Fixed, as proposed | Thunderbird counts as a program for its mail accounts only, so Outlook's files beside an empty Local Folders carry no "Outlook: ". | `TestTwoProgramsSayWhichIsWhich` | O3 |
| R2-F4 | Fixed, as proposed | A copy's date is its newest file's, Thunderbird's summaries and .dat files included (they stay out of the size). Not checked in a running Thunderbird, as the reviewer notes: that it rewrites a folder's summary whenever it syncs the folder; MA-1467's lab step should look. | `TestTheSizeCountsTheMailOnThisComputer`; `TestOneAddressInTwoProfilesSaysWhichChangedLast` (the copy in use dated by its summary) | O4 |
| R2-F5 | Fixed, more simply than proposed | Where even the whole profile folder names match, no profile words are said (they would tell nothing); the date and " (2)" are left. Naming where each profile is kept ("in the snap") is later work. | `TestOneAddressInTwoProfilesSaysWhichChangedLast` | O6 |
| R2-F6 | Backlog | Rare: it needs a second profile that cannot be read with certainty. | — | — |
| R2-F7 | Fixed, as proposed | On Linux and the BSDs, rows alike once each character outside ASCII is "?" get " (2)" too, so every row's answer fits it alone. | `TestTheAnswerMapsBackToItsRow` | O5 |
