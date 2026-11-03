# Friction review: Windows dialogs in front of the browser (1.5.5)

Walked 3 November 2026, 03:07–03:25 on the VM clock, hands-on on the Parallels VM
"Windows 11" (ARM64, build 10.0.26100; the amd64 program runs emulated), by the friction
agent of workflow wf_f15e48ad-ba9. The fix is branch `windows-dialog-front` at 14dad33
(rule R39, rows MA-1110..MA-1117 in `docs/scenario-catalog.md`).

**Verdict: SHIPPABLE_WITH_NOTES.** Every window a Windows person asked for came up in front
of Edge and took the keyboard, every time (21 windows over 10 walked cells). A window
nobody was waiting for, the "Export complete" summary after the person had gone back to
Edge, did not take a single keystroke: about a minute of typing all went to Edge, and the
summary's taskbar button flashed. When the last window closed, Edge had the keyboard again
with no click. No finding is caused by the fix and blocks shipping it; the notes are below.

## Header

| | |
|---|---|
| Reviewed commit | 14dad33 (worktree clean apart from the untracked `.claude/` helper folder) |
| Program | `mailarchivew.exe`, built for this walk from 14dad33 with `GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui ./cmd/mailarchive` (the release script's line), go1.26.0 |
| Started as | `mailarchivew.exe desktop -config <walk folder>\cfg\graph-config.json`, from the Run box through a small launcher that pointed the user-settings folders (APPDATA, LOCALAPPDATA) at the walk's own folder, so the VM user's real MailArchive settings were not touched |
| Browser | Microsoft Edge, already open with other tabs |
| Actor | the owner (non-technical), using the keyboard only |
| Input | real key events sent to the VM (`prlctl send-key-event`); buttons pressed with Ctrl+F, the button's words, Esc, Enter. No mouse: Parallels' command line has no mouse input |
| Evidence | a screenshot of the VM screen at each step (`prlctl capture`), kept on the Mac with the run's notes, not committed |
| Test mail | a 5-message mbox and a 30,000-message mbox written for this walk |
| Baseline | `docs/scenario-catalog.md` SHA-256 6f8da8b3…34ea12, unchanged; no gate, test or baseline touched (this commit is docs only) |
| Cells walked | 10 (plus 1 checked and found to open no window) |
| Functioning | 10 fully, 0 partly, 0 not |
| High-friction cells | 0 |

## The walk

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| 1. Owner: **Make a new archive…**, end to end with the 5-message mbox, at a normal pace | Yes | None. Each of the 8 windows (What are you exporting?, the mbox picker, the output folder picker, Export mode, the date box, Keep originals, Export complete, Keep this archive current automatically?) came up in front of Edge with the keyboard; Edge's title bar went grey each time. After the last answer, Ctrl+F opened Edge's find bar with no click (`17-edge-has-keys.png`) | — | — |
| 2. Owner, slow: waits 15 s on the mbox picker before answering | Yes | None. The next window (the output folder picker) came forward at once, because it follows the person's answer, not the button press (`05-slow-15s.png`, `06-out-picker.png`) | — | — |
| 3. Owner: Alt+Tab to Edge in the middle of the wizard, then Alt+Tab back | Yes | None. The picker stayed behind Edge and was not pulled forward again; Alt+Tab brought it back unchanged; the next window after the answer came forward (`07`, `08`, `10`) | — | — |
| 4. Owner: **Add another archive** card → **Make a new archive…** with the 30,000-message mbox | Yes | None. Wizard in front; the progress window "Exporting…" in front with its count moving (`19`, `24`) | — | — |
| 5. Owner: switches to Edge during the long export (Alt+Tab, new tab), types for about a minute, then comes back | Yes | 1 extra step to find the summary. No keystroke went to MailArchive: the whole "walk walk walk…" line landed in Edge's address bar (`loop-20.png`). The summary opened behind Edge and its taskbar button flashed. Alt+Tab listed it fifth, after four Edge entries (Edge puts its tabs in Alt+Tab) (`27-alttab-list.png`); the flashing taskbar button is the quicker way back | I | None: the summary not taking the keyboard is the control (R39). See note A |
| 6. Owner: **Shut down** while the wizard's schedule question is open | Yes | None from this fix. The program stopped, the open window went with it, the page said "MailArchive Desktop has stopped." (`32`); no process left. Starting Desktop again showed both archives, the second one complete (`33`). See note C | — | — |
| 7. Owner: **Open an archive folder…**, picks the second archive | Yes | Picker in front with the cursor in its Folder box (`34c`). Choosing the folder took Tab to **Select Folder** and Enter (see note D). The second archive became the current one, and Edge had the keyboard again (`35b`, `36`) | — | — |
| 8. Owner: **Make a new encrypted archive…** → Auto-detect (nothing to find on the VM) → Esc | Yes | None. The list, the "No Outlook, Thunderbird, or Evolution mailboxes were found" warning after the scan, and the list again each came forward; after Esc, Edge had the keyboard (`38`–`41`) | — | — |
| 9. Owner: **Copy MailArchive link** (`desktop -copy-link`) with Desktop running | Yes | None. The "Your browser did not open. Copy this link…" window came forward with the link selected; after Esc, Edge had the keyboard (`42`, `43`) | — | — |
| 10. Owner: starts Desktop again after Shut down | Yes | None (`33`) | — | — |
| (checked) **Archive now** / **Run now** for a local archive | n/a | Archive now is off for a local archive and shows only text; Run now appears only once a schedule is set. Neither opens a MailArchive window (from the page and the code), so the fix does not touch them | — | — |

## Notes

**A. A window that only flashes is found from the taskbar (Type I, low).** This is what
R39 is for: a window that opens after the person has gone back to the browser must not take
their typing. The cost is one step: click the flashing taskbar button (Alt+Tab works too,
but lists Edge's tabs first). The page meanwhile says "Waiting for the wizard…" and keeps
its note "Its windows may be behind this browser window…", which is exactly true for this
case (the lead kept that note; this walk agrees).

**B. The 8 seconds count from the person's last answer, so a slow step before the next
window would make it only flash (Type II, low, not walked).** After "Auto-detect my
mailboxes" the wizard scans for mail stores, and when it finds an Outlook `.ost` it also
checks for the Outlook app before asking "Use the Outlook app to export this account?"
(`internal/wizard/wizard.go`, `localArchiveFlowHooks`). On the VM, with nothing to find, the
scan took under 4 seconds. On a real PC with large mail stores, or with Outlook starting
slowly, this could pass 8 seconds; the next window would then only flash, as in note A, and
the person would click the taskbar button. Nothing would answer itself or be lost. The
8-second rule is the lead's call and stands; the backlog item is a lab check on a real
Windows PC with classic Outlook and a large profile. If it does pass 8 seconds there, one
answer (to be decided then, not now) is an extra ask just before that window. The encrypted
flow does not have this gap: all its wizard windows come before the page step, and its
first backup runs from the page with no window (`internal/wizard/addkeyed.go`, read only).

**C. Shut down closes an open wizard window with no word (older than this fix, low).** In
cell 6 the export had finished and the archive was kept; only the schedule question went
unanswered, so no schedule was set. The page's Shut down confirmation does not mention that
a wizard window is open. The fix does not change this; for a later release.

**D. In Desktop's folder picker, Enter on a typed path does not choose it (Windows'
picker, older than this fix, low).** The first Enter opened the folder, the second left the
path selected in the Folder box; Tab to **Select Folder**, then Enter, chose it. In the
wizard's output picker two Enters were enough. Not about which window is in front; for a
later look.

## The walk's own extra steps (not the product's)

Edge's Ctrl+F sometimes matched the guide text instead of the button, so a few presses were
repeated; the Parallels key for the Windows key did not work, so the Run box was opened from
Start with Ctrl+Esc. Neither is something a person with a mouse meets.

## Not walked

A real mouse click (into Edge mid-wizard, on the flashing taskbar button); a real PC with a
native amd64 processor; Windows 10; more than one monitor; an administrator program in
front; a real Outlook or Thunderbird profile, so the slow scan in note B; Desktop's
**Resume** button (its known gap, MA-1115, is left for a later release by the lead); the
encrypted archive's password step and its restore-key file picker; a Microsoft 365 archive's
Run now.
