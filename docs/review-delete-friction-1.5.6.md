# Friction review: deleting an archive, and the delete marker (1.5.6)

Walked 3 November 2026, 12:23–12:54 on the VM clock, hands-on on the Parallels VM "Windows 11"
(ARM64, build 10.0.26200; the amd64 program runs emulated), by the friction agent of the lead's
workflow. Process: `../assurance-kit/process/friction-review.md`. Feature: `docs/design-archive-delete.md`
(§3 to §10, the marker §15, its rulings §15.8, the `-config` verbs §16).

**Verdict: SHIPPABLE_WITH_NOTES.** All 12 walked cells worked, and each refusal said what
happened and what to do next. The disk and `deletions.log` agreed with every result shown. No
finding blocks 1.5.6. Ten notes follow (none high; one low-to-medium). Their proposed wording
changes are proposals only: this commit is docs only, and nothing in the product was changed.

## Header

| | |
|---|---|
| Reviewed commit | 666148b (branch `switch-keeps-runjob`; worktree clean apart from the untracked `.claude/` helper folder and four untracked helper scripts at the root) |
| Programs | `mailarchive.exe` and `mailarchivew.exe`, built from 666148b with the two lines `packaging/build-release.sh` uses (`GOOS=windows GOARCH=amd64 go build`, the second with `-ldflags -H=windowsgui`), go1.26.0. They report `v1.5.6-0.20261103165557-666148b8c9b2+dirty` (the dirty mark is the untracked files) |
| Two "computers" | A and B: one Windows account, each with its own settings folders (`APPDATA` and `LOCALAPPDATA` pointed into `%LOCALAPPDATA%\delwalk\A` or `\B`) and its own `-config` file, so each had its own list of archives and its own `deletions.log`. The VM user's real MailArchive settings and data were not touched. The archives sat in `C:\Users\Public\delwalk`, which both "computers" could reach, as a shared drive would |
| Browser and input | Microsoft Edge, keyboard only: real key events sent with `prlctl send-key-event`, buttons reached with Ctrl+F, Tab, Shift+Tab and Enter. Parallels' command line has no mouse input |
| Terminal | Windows Terminal (cmd) opened on the VM from Start, typed into by key events, so `delete-archive` saw a real console |
| Test mail | one 24-message mbox written for this walk; five plain archives made from it at the terminal, plus one more during the walk |
| A file held open | a hidden PowerShell script held one mail file open (reading allowed, deleting not), as Windows Search or an antivirus can, to make a delete stop part-way. This is a real Windows sharing stop, not a simulated one |
| Evidence | a VM screenshot at each step (`prlctl capture`), 70 files `00-start.png` … `68-term-unlisted.png`, kept on the Mac in `.claude/delwalk-shots/` (untracked, not committed); the saved plan and deletion record files; computer A's `deletions.log` (read at the end, matching every result) |
| Baseline | `docs/scenario-catalog.md` SHA-256 d46d2e9d…bebe9c7, unchanged; no gate, test, covers marker or baseline touched (this commit adds only this file) |
| Cells walked | 12 (Windows). Mac: not walked (the brief made it optional; time went to Windows first). Linux: not walked |
| Functioning | 12 fully, 0 partly, 0 not |
| High-friction cells | 0 |

## The walk

Actor: the owner, a non-technical person using the Desktop with the keyboard; for cells 9 to 11, a
person at a terminal. "Steps" counts the key actions a mouse user would also need.

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| 1. Owner: **Delete…** a plain archive that has a nightly schedule and one file of the person's own in its folder | Yes | 4 steps: Delete…, type the name, Tab, Enter. The plan opened beside the row with the cursor already in the name box (`05`). "capital letters count" is in the label; a wrong-case name showed "doesn't match yet" and kept the button off (`06`). Enter in the name box does nothing: the person must Tab to **Delete permanently** (`08`, `09`). The result said what was removed, that the folder was kept for one file not the archive's, that the schedule was stopped, and that the row was removed (`12`). On disk: only `My notes.txt` left; the scheduled task gone | I (the Enter) | None: a typed name that also submits on Enter would make the decision one keystroke; keep it |
| 2. Owner: **Save this plan**, then **Save deletion record**, then **Dismiss** | Yes | Both files were saved with plain names ("MailArchive deletion plan - Plain-Archive - 2026-11-03.txt"); the plan says "planned, nothing deleted". Their folder identity and manifest SHA-256 lines match, as an approver needs. Dismiss before saving asked "Close without saving the deletion record?" (`14`), but **the focus sits on "Close without saving"**, so a second Enter loses the record. After Dismiss the page reloaded and the view jumped down to the Microsoft 365 card, away from Your archives (`17`) | III | Note A, note B |
| 3. Owner: a delete stopped part-way (one mail file held open) | Yes | The result named the file and the remedy: "Not fully deleted. 1 file could not be removed: walk/2025-01-04_…html. Close Outlook or any program using it, then click Delete again." (`30`). The row at once said "Delete not finished: click Delete again" with Switch, Pause, Run now gone (`33`). The folder kept the anchor, the held file and `.mailarchive-deleting.json` | — | — |
| 4. Owner: **Remove from list** on that unfinished row, before reloading | Yes (refused, nothing changed) | Right after the result, **Remove from list** still looked live (`33`); pressing it gave "Delete not finished: click Delete again. … so it was not removed from the list; nothing was changed. Reload this page to see its row as it is now." (`34`). After the reload it is greyed (`38`). One wasted step and a "reload" instruction | III | Note C |
| 5. Owner: **Delete…** again on the unfinished row (the file no longer held) | Yes | The plan began "This finishes the delete begun 2026-11-03 12:34 -05:00." with the 2 files left (`39`); the result: "Deleted. 2 files (13.8 KB) removed. The folder is gone. This finishes the delete begun …: 31 files (141.7 KB) removed in all." (`40`) | — | — |
| 6. Owner on computer B: **Open an archive folder…** on a half-deleted folder B never listed | Yes | The picker came up in front of Edge with the cursor in its Folder box (`45`). Typing the path, Tab, Enter chose it. B said "Added to Your archives: … A delete of this archive did not finish: to finish it, click Delete… on its row." and did not make it the current archive (`48`) | — | — |
| 7. Owner on B: reads the row | Yes | "Delete not finished: a delete of this archive begun 2026-11-03 12:40 -05:00 did not finish. This computer's deletion log has no record of it (it was begun on another computer or user account). Click Delete… to finish it." (`49`). It leads with what happened, does not say "again" to a person who began nothing, and names the remedy (the owner's wording rule). Remove from list stays offered here, as §15.4 rules | — | — |
| 8. Owner on B: backup refused into the half-deleted folder, then **Delete…** to finish | Yes | At B's terminal, `mailarchive -out Shared-Archive -input walk.mbox` was refused: "Delete not finished: a delete of this archive was begun on another computer or user account … so it was not backed up; nothing was changed. To finish it, click Delete… on its row in MailArchive Desktop." plus the terminal command (`50`). `status` gave the RED line in the same words. B's plan: "This finishes the delete begun … This computer's deletion log has no record of when it began: it was begun on another computer or user account." (`52`). The second sentence contradicts the first, which gives the date. B's result named what it removed and that earlier counts are not known (`53`) | III | Note D |
| 9. Owner back on computer A: the delete B finished elsewhere | Yes | A's row still said "Delete not finished: click Delete again" (A's log has no word of B's finish). Delete… asked: "This archive's folder is not there. If its delete was finished from another computer or user account, MailArchive can finish it here too: it stops this computer's schedules for it and removes its row. If the folder is on a drive that is not plugged in, plug it in instead." with **It was finished elsewhere** / **Cancel** (`59`). The plan then said "The folder was not found here; you said this delete was finished elsewhere." and still asked for the typed name to "Delete permanently" 0 files (`60`). The result: "Finished here. … 29 files (127.9 KB) removed in all." (`61`). B removed 2 more, so "in all" undercounts. Neither the guide nor the README mentions this question | I (typed name); III (count, docs) | Note E, note F |
| 10. Person at A's terminal: refusals for an unfinished delete | Yes | `delete-archive` while A's Desktop ran: "MailArchive Desktop is running for your account … so nothing was changed. … click Shut down at the top of MailArchive Desktop, then run this again." (`29`). `pause -config` and `schedule -config … -install` were refused in MA-1015's words with the `-config` hint (`36`). A plain backup was refused. `status` worked (`35`) | — | Note G (a stated limit, seen) |
| 11. Person at the terminal: `pause`/`resume -config`, `delete-archive -plan-out`, a wrong name, then the right name | Yes | `pause`'s line named `resume -config "…" -out …` to undo (`63`). The plan-only run saved the file and said "Nothing was deleted." (`64`). A wrong-case name got "That is not the folder's name, Terminal-Archive (capital letters count). Nothing was deleted; 2 tries left." (`65`). The right name gave the result and the full record, exit 0 (`66`). On Windows the record's "What Delete does not reach" names "macOS Time Machine" (the Desktop's line on Windows does not). The record says "First run: not known" for an archive whose first run is the one shown as its last backup | III | Note H, note I |
| 12. Owner: **Remove from list** (Desktop), then `add-archive`/`unlist-archive` (terminal) | Yes | Desktop: "Remove from list? The folder and its files are kept." Yes/No, with focus left on the row button: Tab, Tab to Yes (`23`, `25`). The row went and the folder stayed (8 entries); again the page jumped down after the reload (`26`). Terminal: `unlist-archive` asked the same question as `[y/N]` and said "Removed from Your archives. The folder and its files are kept.", exit 0 (`67`, `68`) | III | Note B |

## Notes (fixes proposed; none applied)

**A. The "Close without saving the deletion record?" question puts focus on the losing choice
(Type III, low).** A keyboard user who presses Enter twice on Dismiss closes the result without
the record. Proposal: focus **Keep it** when the question opens, so Enter keeps the result. One line
in the page script; a test can check which button has focus.

**B. After Dismiss and Remove from list, the page reloads and leaves the view at the Microsoft 365
card (Type III, low to medium).** The person loses their place in Your archives and, on the
keyboard, starts again from the top. This happened three times in the walk. Proposal: after these
reloads, scroll Your archives into view and put focus on the card's heading or on the next row's
first button. This is about this feature's flow, not new behaviour, so it is a builder's fix with
no ruling needed.

**C. Right after a part-way result, Remove from list on that row still looks live (Type III,
low).** The server refuses it correctly, but tells the person to reload. Proposal: when the result
marks the row "Delete not finished", grey out Remove from list on that row at the same moment, with
the title the reload gives it.

**D. On the other computer, the plan's second line contradicts its first (Type III, low).**
"This finishes the delete begun 2026-11-03 12:40 -05:00." is followed by "This computer's deletion
log has no record of when it began: …" (`MsgMarkerNoRecord`, `internal/archdelete/result.go:203`).
Proposed words (lead's words, owner-delegated 2026-11-02, if the lead takes them): "This
computer's deletion log has no record of this delete: it was begun on another computer or user
account." These are the design's own words in §15.4.

**E. "removed in all" after "It was finished elsewhere" counts only this computer's runs (Type
III, low).** Computer A said 29 files in all, but B removed 2 more. Proposed words (lead's words,
owner-delegated 2026-11-02, if taken): "This finishes the delete begun DATE: this computer's runs
of it removed N files (SIZE); the rest were removed elsewhere." The record's and the log's counts
are right as they are.

**F. The guide and README do not mention the "It was finished elsewhere" question, or what the
note file is (Type III, low to medium).** The question explains itself well, but a person who
reads the guide first will not expect it. Also, the in-app guide never names
`.mailarchive-deleting.json` (only the README and the release notes do). It does not say that
removing the file by hand lets backups write into a folder a delete may have half emptied. §15.8's
first question recommended saying this "in the guide", and the owner's Q1 ruling keeps "finish it,
or remove the marker by hand" as the way out. Proposal: two sentences in "Deleting an archive",
"A delete begun on another computer" (README too). These are doc words, not behaviour.

**G. Stated limit, seen: `status` without `-config` calls a delete "begun on another computer"
when it was begun by a Desktop started with `-config` (§16.6).** At A's terminal, `status` (which
takes no `-config`) read MailArchive's own log, not A's. For most people, whose Desktop runs
without `-config`, this does not arise. The remedy it names works. Nothing to do for 1.5.6.

**H. The terminal record names "macOS Time Machine" on Windows (Type III, low).** The Desktop's
line on Windows leaves it out. Proposal: use the same per-system line in both places.

**I. "First run: not known: the archive's history of its runs begins … (as recorded in the
archive)" for a new archive made at the terminal (Type III, low).** It is honest (the terminal
export writes no history header), but an approver reading a record for an archive made that same
day will find it odd. A later look, not for 1.5.6.

**J. Older than this feature, seen in passing (not counted).** `schedule -install`'s success line
prints `mailarchive status -out "C:\\Users\\…"` with doubled backslashes. `pause` says "its
scheduled backup is stopped" for an archive that has none.

> **Settled 3 November 2026 (the lead's settle step; the notes above stay as written).** Fixed
> test-first, each with its prove-fail: **B** (MA-1065: the page drawn again after Dismiss or
> Remove from list is at Your archives, its heading focused), **D** (MA-1066: "It was begun on
> another computer or user account: this computer's deletion log has no record of its start." — not
> the proposed "…has no record of this delete…", which §15.4 had turned down because a later run
> here does have its own line), **E** (MA-1066: the proposed words, as written above) and **F**
> (MA-1067: the guide and the README name the note file, say what removing it by hand does, and
> describe the "It was finished elsewhere" question). Words: lead's words, owner-delegated
> 2026-11-02. Left for the lead, not changed: A, C, H, I, J; G is a stated limit. Detail in
> `docs/design-archive-delete.md` §14, "The 1.5.6 friction review settled". B's fix is checked
> under Node, not yet on Edge.

> **Settled further, 3 November 2026 (the notes above stay as written).** **A** is fixed test-first
> (MA-1069): the question opens with the cursor on **Keep it**, and Enter twice saves the record;
> checked under Node, not yet on Edge. **H** is answered without a change: the terminal's own plan
> and result lines are already the Desktop's per-computer line (MA-1019); what this walk saw is the
> deletion record the terminal prints to be copied, which keeps the whole line on both surfaces on
> purpose (it may be read on another computer, and `deletions.log` holds its SHA-256), as MA-1019
> pins. Changing it is the lead's call. Detail in `docs/design-archive-delete.md` §14, "LOW-4 of
> slice 5's adversarial pass, and friction notes A and H, settled". C, I and J stay left.

> **Walked on Edge, 3 November 2026 (MA-1055's Windows lab; the notes above stay as written).**
> **A** holds on Edge: the question opens with the cursor on Keep it, and Enter on Keep it moves it
> to Save deletion record. **B** holds on Edge after Remove from list, and after Dismiss while an
> archive is chosen: the page is at Your archives, its heading focused. With no archive chosen,
> Dismiss still lands at the page's top (N12's branch also takes that case; LOW, for the lead).
> Detail in `docs/lab/delete-marker-windows-2026-11-03.md`.

## Type I friction, counted and kept

- The typed folder name, and Tab to the button (no Enter submit): the typed name is the decision
  (§3.1).
- The typed name after "It was finished elsewhere", though 0 files are removed: the answer can make
  this computer forget an encrypted archive's key (§15.10 slice 4), so the typed name stays the
  decision.
- Three tries at the terminal, then refused: the same decision at a terminal.

## The walk's own extra steps (not the product's)

Edge's Ctrl+F often matched the expanded guide before the row, and a long folder name wraps on the
row, so searches were repeated. One Tab too many pressed **Switch to this archive** on another row;
the switch was harmless (no schedule) and is not a finding. A PowerShell window from the first
file-hold took the keyboard once; the hold was then run hidden. Shut down was not pressed from the
page: the walk stopped each "computer's" Desktop with `taskkill` to change computers quickly (Shut
down was walked in 1.5.5's review). None of this is something a person with a mouse meets.

## Not walked

- **macOS (Chrome) and Linux:** not walked in this review.
- **Encrypted archives:** the key line in the plan, "This computer forgot the archive's key", and
  "It was finished elsewhere" for an encrypted archive.
- **Mail copies the archive did not record:** the §4.3 box, ticked and unticked.
- **A synced (OneDrive) folder.**
- **A real second PC or a second Windows account,** and a real network share or USB drive pulled
  mid-delete (lab rows MA-1055).
- **The scheduled run's refusal and its 30-day auto-pause** (slice 6) as a scheduled task actually
  firing; only the attended refusals were walked.
- **Deleting the current archive** from the Desktop (`-out` given at launch, "No archive chosen
  yet" afterwards).
- **A mouse.**
