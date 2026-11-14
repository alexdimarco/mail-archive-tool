# Friction review: menu-bar slice S1 (one running copy, the launcher choice, stops that wait)

Walked 13 November 2026, 18:58–19:15 on the Windows VM clock and 19:09–19:12 on the Mac, by
the S1 friction agent. Branch `menubar` at c1df91b; design `docs/design-menubar.md`; process
`../assurance-kit/process/friction-review.md`.

**Verdict: HIGH_FRICTION_NOT_SHIPPABLE.** The mechanism works: one copy runs, a second click
reaches it, the choice window comes up in front of Edge, Desktop opens in the default browser,
Cancel on a first start ends the program, and the Quit question asks before a running copy is
stopped. Three findings block shipping because they defeat the owner's request itself
("If it is running it opens up the existing running application (with the same choices)"):

- **F1** On Windows the choice window cuts both lines off, so the person never sees "step by
  step" or "in your browser"; the newer-version and "Now:" lines would hide the question.
- **F2** After the wizard, MailArchive keeps running with nothing on screen (on the Mac no Dock
  icon and no menu-bar icon), and the choice offers no way to quit it.
- **F3** While the wizard runs from the choice, clicking MailArchive only says "already open"
  and never offers Desktop, for as long as the wizard lasts (5 minutes in this walk).

Each has a small fix (below). None needs a design change except F2, which needs one line of
decision from the lead.

## Header

| | |
|---|---|
| Reviewed commit | c1df91b (worktree `wip/menubar-s1-fric`, clean apart from the untracked `.claude/` scratch folder) |
| Programs | Windows: `GOOS=windows GOARCH=amd64 go build -ldflags -H=windowsgui ./cmd/mailarchive`, named `s1fricw.exe` (and a console `s1fric.exe`) so other jobs' "stop every mailarchivew" never reached it; go1.26.0, stamp `v1.5.6+dirty` (a worktree build). Mac: `CGO_ENABLED=0 go build ./cmd/mailarchive` (arm64) in a hand-made bundle shaped like the product's (`mailarchive-gui` symlink, `LSUIElement` true), bundle id `com.dimarcotech.mailarchive.s1fric`. Linux: `GOOS=linux GOARCH=arm64` in Docker `golang:1.26` |
| Started as | Windows: `s1fricw.exe start|desktop|wizard -config C:\Users\Public\s1fric\cfg\config.json` through a small launcher that pointed APPDATA and LOCALAPPDATA at the walk's own folder. Mac: `open --env HOME=<lab> --env PATH=<lab bin>:…` on the lab bundle; terminal launches with the same HOME and PATH |
| Browser | Windows: Edge, already open with another job's tabs. Mac: none (no browser was opened on this Mac) |
| Actor | the owner (non-technical) and a new user with no archive |
| Input | Windows: real input in the signed-in session, sent only when the front window belonged to the lab program (or, for Edge, only when Edge's address bar showed the lab's port); mouse clicks where keys did not work. Mac: a lab-only `osascript` stand-in first on PATH answered every dialog from a queue, logged each dialog's words, and recorded the browser link instead of opening it; no real dialog was shown on the owner's screen |
| Test mail | synthetic mboxes from the 1.5.5 walk: 5 messages and 30,000 messages (`walker@example.org`) |
| Evidence | VM screenshots (`prlctl capture`) and the Mac stand-in's dialog log, kept in the worktree's `.claude/`, not committed |
| Baseline | `docs/scenario-catalog.md` SHA-256 d3edcffc…731afc, unchanged; no gate, test, marker or baseline touched (this commit is docs only) |
| Cells walked | 17 (10 Windows, 6 Mac, 1 Linux) |
| Functioning | 11 fully, 5 partly, 1 not |
| High-friction cells | 4 (W1, W5, M2, M6) |

## The walk

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| W1. New user, Windows: first launch, nothing running, no archive | Yes | The choice came up **in front of Edge** with Edge greyed, "Run the wizard" selected (right: no archive). Both lines cut off at the window edge: "Run the wizard — add or back up an archive" and "Open MailArchive Desktop — your archives," (F1) | III | F1 |
| W2. Same, closes the choice (Esc) | Yes | The program ended and Edge had the keyboard again. No word that MailArchive stopped; fine for a first start | — | — |
| W3. Owner, Windows: launch, Enter on the choice | Yes | Enter took the selected wizard item. The wizard's first list has 3 items (Desktop item dropped, Q2) and asks "What would you like to do?" again, the same question as the window before it (F10). Arrow keys sent by my input tool did not move either list's selection, while Enter, Esc and clicks did (F12, unconfirmed) | III | F10, F12 |
| W4. Owner: wizard → Single mbox file → 30,000 messages → copy | Yes | Each wizard window came forward. The copy took 5 min 2 s and was recorded ok | — | — |
| W5. Owner: clicks MailArchive again during the copy, to see the archive | **Partial** | A box titled "MailArchive Desktop" said "MailArchive is already open, and its window is on your screen. Look for it there or in the taskbar." The window on screen was the wizard's progress, not the choice; there was **no way to Desktop by clicking the app** (F3). The heading says "MailArchive Desktop", the window it points at says "MailArchive" (F6) | II | F3, F6 |
| W6. Owner: the older `desktop` route during the copy | Yes | A new Edge tab opened on the running copy's Desktop. (The person has no such route after S1: the shortcut that used to do this now shows the choice) | — | F7 |
| W7. Owner: **Shut down** during the copy | Partial | Three clicks: Shut down → "Confirm shut down" with the note "your archives and schedules are untouched" (no word about the copy) → only then "MailArchive is busy (the wizard is copying mail). Quit anyway? …" with Keep running (blue, first) and Quit (F4) | III | F4 |
| W8. Owner: presses Quit 6 minutes later | Partial | The copy had finished at 19:10; the page still said "busy … copying mail" at 19:12 (stale). Quit closed the wizard's still-open end windows ("Export complete", "Keep this archive current automatically?") unanswered: `status` then said "Schedule: none recorded" (F9). The page said "has stopped" at once; the process ended 15–30 s later (F15) | III | F4, F9, F15 |
| W9. Owner, Windows: sign-in launch (`start -background`), then a click | Yes | Nothing showed at sign-in (as designed). The click's choice came **in front of Edge**; "Open MailArchive Desktop" opened a new tab in the default browser (Edge). The default item was still "Run the wizard" though the wizard had just made an archive (F8) | III | F8 |
| W10. Owner: Start menu "MailArchive wizard" route twice while MailArchive runs | Yes | The running copy showed the wizard; the second said "The wizard is already open. Look for its window on your screen or in the taskbar." under the heading "MailArchive Desktop" (F6). After OK, no window had the keyboard (F13) | III | F6, F13 |
| M1. New user, Mac: first open, closes the choice | Yes | The program ended and removed its instance file | — | — |
| M2. Owner, Mac: open → Run the wizard → closes the wizard | **Partial** | The wizard asked "What would you like to do?" right after the choice asked it (F10). After the wizard closed, **the program kept running** as a background element (`lsappinfo`: type UIElement): no Dock icon, no menu-bar icon (that is S3/S4), no window, no Quit anywhere but Desktop's Shut down. Before S1 the Mac app ended with the wizard (F2) | II | F2 |
| M3. Owner, Mac: opens the app again while it runs, picks Desktop | Yes | One process; "macOS opened the app again; showing the choice"; the Desktop link went to `open location` (the default browser) | — | — |
| M4. Owner, Mac: a newer copy installed (bundle 9.9.9), opens the app | Yes | The prompt read "A newer MailArchive is installed. Quit and open MailArchive again to use it." then the question; a third item "Quit MailArchive — then open it again to use the newer version". Quit with nothing running ended the program. The line and the item say the same thing twice (F16) | III | F16 |
| M5. Owner, Mac: a second launch while the choice is on screen | Partial | Alert headed "MailArchive Desktop": "… Look for it there or in the taskbar." A Mac has no taskbar (F6) | III | F6 |
| M6. Owner, Mac: starts the wizard (the `wizard` launch) while the first choice is open, then closes the choice | **No** | Closing the choice ended MailArchive although the wizard was open: the instance counts only a copy as work, not an open wizard. The wizard's dialog was left with no program behind it (F5) | III | F5 |
| L1. Linux, no desktop session (Docker): `start -h`, `start`, `start -background`, `start foo`, `wizard` | Yes | `start` with no dialogs fell back to serving Desktop and printing its address (as §6.1 says). Small words: `start -h` has no line saying what it does; "mailarchive: mailarchive start takes no arguments but its flags…" doubles the prefix; the log says "mailarchive desktop: a dialog shows a link" where no dialog can show (F14) | III | F14 |

Not walked (stated, not implied): R50 (another sign-in of the same account; needs two graphical
sessions); the Quit question's "a backup is running" form (Run now; the words share one template
with the walked "wizard is copying mail" form); a real click on a Start-menu shortcut (my launches
came through a script, so Windows' "the program the person clicked" foreground right was not
exercised, though the choice still came in front every time); a person's own keyboard; any Mac
screenshot (the terminal has no Screen Recording permission); the product's own bundle id; a Mac
sign-out with work running.

## Findings

### High

**F1. The Windows choice window cuts its words off (Windows, walked; the two-line case read in
the dialog library).** Windows' list is a fixed 241-unit-wide box with a one-line prompt
(`zenity` `list_windows.go` layout; `Width` is "Unix only"). Seen: "Run the wizard — add or back
up an archive" and "Open MailArchive Desktop — your archives," — the explaining half, the part the
owner asked for ("a better choice"), never shows. The Quit item would read "Quit MailArchive —
then open it again…". The prompt is one line with an ellipsis (seen on the wizard's "Is your mail
app (Outlook / Thunderbird) cu…"), so the "A newer MailArchive is installed…" and "Now: …" lines,
which the code puts above the question with blank lines (`front.go` `choiceView.Text`), would show
only their first line and hide "What would you like to do?". MA-1286's rule (show the text in the
list on Windows) assumed Windows shows it whole; it does not.
*Plain fix:* keep every item under about 40 characters and every prompt to one short line (wording
below). No explanation after a dash.

**F2. After the wizard, MailArchive keeps running with nothing on screen and no way to quit (Mac,
walked; Windows by the same code).** `showChoice` ends after the wizard with the instance still up
(`front.go:206-212`); only a first-start Cancel stops it (MB-16). On the Mac the app is a
background element (`LSUIElement`), so there is no Dock icon, and S1 has no menu-bar icon, so
nothing shows it runs; the choice has a Quit item only when a newer copy is installed. Before S1
the Mac app ended with the wizard. This is the "silent, invisible background server" X11 rules out,
introduced by S1 ahead of the icon that would explain it.
*Plain fix (pick one, lead's call):* (a) recommended: until the icon ships (S3/S4), when the choice
or the wizard ends and nothing runs and no Desktop page was opened, end the program, the same
rule MB-16 already applies to the first Cancel; or (b) always offer "Quit MailArchive" in the
choice.

**F3. While the wizard runs, a click on MailArchive cannot reach Desktop (Windows, walked).**
`showChoice` runs the wizard inside itself (`front.go:212`), so `choosing` stays true for the
whole wizard and every later click gets `ReasonAlreadyOpen` → "MailArchive is already open, and
its window is on your screen…". The window on screen is the wizard's, not the choice. A first
backup of real mail can run for hours; for all that time clicking the app gives no choice, which
is the owner's request ("it opens up the existing running application (with the same choices)").
*Plain fix:* clear `choosing` before the wizard starts (run the picked wizard as the hand-off's
`wizard` already does, in its own goroutine), so a click shows the choice again; its "Run the
wizard" then answers "The wizard is already open", and "Open MailArchive Desktop" works.

### Medium

**F4. Shut down while work runs takes three clicks, and the middle one is wrong; the question goes
stale (Windows, walked).** Shut down → "Click again to stop MailArchive Desktop … your archives
and schedules are untouched" → Confirm → the busy question. The middle note does not say a copy
will stop. The question stayed "busy … copying mail" 2 minutes after the copy had ended.
*Plain fix:* when work runs, the first click shows the busy question in place of the generic
confirm; when nothing runs, keep today's two clicks. Check again when Quit is pressed and word the
result ("Nothing was running; MailArchive stopped." when the work had ended).

**F5. Closing the first choice ends MailArchive under an open wizard (Mac, walked).** The idle test
(`launch.go` `idle`: no Desktop page opened and `quitQuestion() == ""`) does not count an open
wizard, only its copy. A person who clicks the app, then the "MailArchive wizard" shortcut, then
closes the first window, loses the wizard (on the Mac its dialog is left with nothing behind it).
*Plain fix:* count a held wizard lock as "not idle".

**F6. The notices name the wrong place and the wrong window (Windows and Mac, walked).** "…or in the
taskbar" shows on the Mac, which has no taskbar (and the lab app has no Dock icon either). Both
notices are headed "MailArchive Desktop" (the launcher's `ShowNotice` title) while the window they
point to is titled "MailArchive" or "MailArchive wizard".
*Plain fix:* head them "MailArchive"; word per platform (below).

**F7. The shortcuts are still named "MailArchive Desktop" but now open a choice (Windows, MSI
table).** The desktop and Start-menu shortcuts that run `start` keep the name "MailArchive Desktop"
and the description "Open MailArchive Desktop"; beside them sits "MailArchive wizard". A person
clicking "MailArchive Desktop" gets asked whether they want Desktop.
*Plain fix:* name the `start` shortcuts "MailArchive" (description "Open MailArchive"); keep
"MailArchive wizard"; this is a packaging change the lead or owner should confirm.

**F9. Quit closes the wizard's end windows unanswered (Windows, walked).** Older than S1 (the
1.5.5 review's note C), but S1 makes it common: the wizard now lives in the instance, so Desktop's
Shut down ends it; before S1 a separate wizard survived Shut down. In W8 "Keep this archive current
automatically?" was closed with no answer and no schedule was set. The busy question does not count
an open wizard window.
*Plain fix:* count "the wizard is open" as work in the busy question ("MailArchive is busy (the
wizard is open). Quit anyway? Its unanswered questions are dropped.").

### Low

- **F8. Default item.** After the wizard made an archive, the default stayed "Run the wizard"
  until the person clicked "Use this archive" in Desktop (`hasArchives` reads Desktop's current
  archive only). *Fix:* also count the archives list and the wizard's last archive.
- **F10. The same question twice.** The choice asks "What would you like to do?"; the wizard it
  opens asks it again. *Fix:* when the wizard comes from the choice, its first list asks "What
  mail do you want to add or back up?"
- **F11. Mac: OK with nothing selected acts as Cancel** (the list allows an empty pick:
  `emptySelectionAllowed` in the dialog's script), and on a first start that quietly ends
  MailArchive. *Fix:* `zenity.DisallowEmpty()` on the choice.
- **F12. Arrow keys (unconfirmed).** In this walk, arrow keys sent from inside the VM did not move
  the selection in any list (the choice or the wizard's), while Enter, Esc and clicks did. The
  1.5.5 walk drove the wizard by keyboard with real key events, so this may be my tool. *Backlog:*
  one keyboard-only pass with a real keyboard on the choice (Tab, arrows, Enter).
- **F13. Focus after a notice.** After OK on "The wizard is already open…", no window had the
  keyboard; the wizard under it did not get it back.
- **F14. Terminal words.** `start -h` has no line saying what `start` is; `mailarchive: mailarchive
  start takes no arguments but its flags (got "foo")` doubles the prefix; a `start` with no display
  logs "mailarchive desktop: a dialog shows a link" where no dialog can show.
- **F15. Slow exit.** "MailArchive Desktop has stopped." showed at once; the process ended 15–30 s
  later (the drain, with wizard windows open). Not seen by the person; a quick relaunch in that
  window would hand off to a stopping copy (not walked).
- **F16. The newer line repeats the Quit item.** Covered by the wording below.

## The S1 words, judged as a phone reader

| Text | Verdict | Proposed words |
|---|---|---|
| Choice title "MailArchive" | Keep | — |
| "What would you like to do?" | Keep alone; but as the only prompt line, and not repeated by the wizard (F10) | — |
| "Run the wizard — add or back up an archive step by step" | **Fails on Windows** (cut at "archive"); "wizard" is the owner's word and the next window's title, so it may stay, but the item must say what it does in under 40 characters | **"Add or back up mail, step by step"** (the next window's title, "MailArchive wizard", keeps the word) |
| "Open MailArchive Desktop — your archives, schedules and settings, in your browser" | **Fails on Windows** (cut after "your archives,") | **"Open MailArchive Desktop in your browser"** |
| "Quit MailArchive — then open it again to use the newer version" (with the newer line) | Fails on Windows (cut); says the same as the line above it | Item **"Quit, to use the new version"**; prompt (one line) **"A newer MailArchive is installed."** |
| "Now: <task> (since …)" line (code: "Now: a backup is running.") | Would hide the question on Windows (F1) | Prompt (one line) **"Now: a backup is running."** — the list itself is the question |
| Quit question: "MailArchive is busy (a backup is running). Quit anyway? The backup stops and is recorded as interrupted." Keep running (default) / Quit | Mostly passes: Keep running is the default and first. "Recorded as interrupted" leaves the reader asking what happens to the mail | **"A backup is running. Quit anyway? The backup stops; the next backup picks up where it left off."** (true per `interruptedText`). Buttons unchanged. Same shape for the others, e.g. **"The wizard is copying mail. Quit anyway? The copy stops; run the wizard again to finish it."** |
| R50: "MailArchive is already open in another sign-in of this account. Use it there, or quit it there to open it here." | Passes, one unclear word ("sign-in" as a noun). Not walked | **"MailArchive is already open for this account on another screen (another sign-in). Use it there, or quit it there to open it here."** |
| Launch notice: "MailArchive is already open, and its window is on your screen. Look for it there or in the taskbar." (heading "MailArchive Desktop") | **Fails**: wrong on the Mac ("taskbar"), wrong heading, and while the wizard runs the window it means is not the one on screen (F3) | Heading **"MailArchive"**. Windows: **"MailArchive is already asking what you'd like to do. Look for its window, or click MailArchive on the taskbar."** Mac and Linux: **"MailArchive is already asking what you'd like to do. Look for its window on your screen."** |
| "The wizard is already open. Look for its window on your screen or in the taskbar." (`WizardOpenText`, heading "MailArchive Desktop") | Same "taskbar" and heading problem on the Mac | Heading **"MailArchive"**; Mac and Linux drop "or in the taskbar" |

## The questions asked

- **First launch, nothing running:** the choice window, with "Run the wizard" selected when no
  archive is set up. Seen on Windows (in front of Edge) and on the Mac (by its dialog log).
- **In front (the v1.5.5 bug):** yes, every time on Windows, from a cold start, from a background
  sign-in instance and through the hand-off; Edge greyed out each time. Caveat: launched by script,
  not a real Start-menu click.
- **Closing the choice:** on a first start with nothing running, MailArchive ends (no word). On any
  later start it keeps running, unseen (F2). With a wizard open it still ends (F5).
- **"Wizard":** the owner's own word and the next window's title, so not jargon to him; to a new
  user it says nothing about what happens. Keep it in the title, lead the item with the task
  (above).
- **Desktop link:** opens in the default browser on both platforms (Edge on the VM; `open
  location` on the Mac).

## Notes for the lead (not product findings)

- **My input reached another job's window once.** At about 19:00 VM time my first attempt (open
  the Run box, type a command, Enter) did not get the Run box, so the typed command and an Enter
  went to the other job's Edge window (its page scrolled). Every input after that was guarded
  (keys only to a lab window; clicks in Edge only when the address bar showed my port). The other
  job's walk may want to recheck what it saw around 19:00.
- A third job (`P:\advs1\run.ps1`) was also on the VM; its terminal window held the front for
  about 5 minutes.
- Left on the VM (my own lab, harmless): `C:\Users\Public\s1fric`, `C:\Users\Public\s1fric-h`,
  `%LOCALAPPDATA%\s1fric`, and two Edge tabs at 127.0.0.1 ports 57952 and 62264 (the programs behind
  them are stopped). No lab process is left on the VM or the Mac.
