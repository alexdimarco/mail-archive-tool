# MA-918 — Windows attended start, first-release scope — PASSED 2026-10-31 (run 2)

This is the Windows `AttendedStartAllowed` entry's own lab record (design-presync-attended
§2.7; MA-927 checks it). It records MA-918 run 2, on the build that ships the start, and
names the code it measured.

- Lab: MA-918, platform: **windows**.
- Pinned hash (MA-927, `docs/lab/presync-windows-code-pin.txt`):
  `10d7ba8f901d205ea85ac79fbead84463cb2948fd1b7ddf235764813f4501d4a`
- Date: 2026-10-31
- Build: commit cc3b86c with only the Windows `AttendedStartAllowed` entry set true (a lab
  build, never committed), which carries the minimized-not-active start of MA-923 (commit
  ab22115, from L-A10).
- Earlier runs: run 1 (`presync-attended-start-windows-run1-2026-10-31.md`, not passed: the
  old start let Thunderbird take the keyboard) and L-A10
  (`presync-windows-start-spike-L-A10-2026-10-31.md`, why, and the start that fixes it).

## Set-up

Windows 11 on ARM (Parallels VM; MailArchive and Thunderbird are amd64 programs running
under emulation, so **all timings are emulated**). Thunderbird 153.6.0esr x64 (installer
SHA-256 checked against Mozilla's list, Authenticode signer Mozilla Corporation), profile
`default-esr` with one Gmail IMAP account added by the owner. Archive
`C:\Users\alex.dimarco\MA918-archive`, its backup set up in MailArchive Desktop's Settings
with "Sync Thunderbird before each backup" on. Observer: a separate process sampling every
100 ms the foreground window and every visible Thunderbird window (minimized or shown).
Typer: a separate process typing in Notepad only while Notepad has the keyboard. Runs A–E
pressed the page's buttons through Windows UI Automation from a separate process (no
keystrokes; pressing a button brings Edge to the front); run F was the owner's own clicks.

## Runs

| Case | How the Desktop was started | Input when Thunderbird started | Thunderbird's window | Keyboard | Outcome |
|---|---|---|---|---|---|
| A | from Windows Terminal | the typer typing in Notepad | **minimized from the first sample, never shown** | **stayed in Notepad** (580 typed, no break) | Stop waiting; ok |
| B | from Windows Terminal | **none for 230 s** (past the 200 s foreground lock) | minimized from the first sample | stayed with Edge | Stop waiting; ok |
| C | same | — Thunderbird already open | left alone | — | `already-running`; ok |
| D | same | none | minimized from the first sample | stayed with Edge | **Cancel**: `cancelled`, nothing archived, **Thunderbird left open** |
| E | same | the typer typing | minimized, then **into the notification area** (option on; main window handle 0) | stayed in Notepad until the driver pressed Stop waiting | ok; a second Run now saw it as `already-running` |
| F | **the owner, from the Start menu** (parent: explorer.exe) | the owner typing | minimized from the first sample (owner: "No thunderbird window (but I can see the icon in the bottom task bar)") | owner: "typing continued without interruption"; observer: never Thunderbird | running at the time of writing |

*Note on case F, added 2026-10-31 after the record was written (by Claude, a model, for the
lead; the row above is kept as written):* case F finished. Its run record on the lab VM
(`C:\Users\alex.dimarco\MA918-archive\.mailarchive-lastrun.json`, also the last entry of
`.mailarchive-runhistory.json` beside it): started 17:58:45Z (13:58:45 on the VM's clock),
finished 18:19:17Z, status `ok`, sync outcome `ok-noactivity` (Thunderbird was started and the
wait ran its full 20 minutes with no new mail arriving, so Stop waiting was not pressed), 2
messages exported and 2 filled. The observer log (`C:\Users\alex.dimarco\ma918\r2f-startmenu-observer.log`)
shows Thunderbird minimized from its first sample (13:58:45.981, VM clock) and never in front
through the end of the run and after it (the last line read, 15:25:14, still minimized, with
Notepad in front). Timings are emulated. So F is a completed run, and its outcome is the full
20-minute wait, which cases A–E did not reach (they ended with Stop waiting, Cancel, or an open
Thunderbird).

Slow start, Primary Password (L-A10 trial pp1, the same start): the "Password Required"
prompt is shown and takes the keyboard; **owner ruling 2026-10-31: "accept the password
window"**.

Files Thunderbird changed in its first minute (case A, L-A9 on Windows): `parent.lock`,
`sessionCheckpoints.json`, SQLite `-shm`/`-wal` files (cookies, places, favicons, abook,
history, storage), `global-messages-db.sqlite` and its journal, `AlternateServices.bin`,
`prefs.js` (+44 s), `crashes\store.json.mozlz4`; no mail folder or `.msf` file in that minute.

## Verdict

First-release scope **passed**: the start shows no window and never takes the keyboard,
from a terminal, from the Start menu, with someone typing and after the foreground lock has
expired; Cancel leaves Thunderbird open; an open Thunderbird (taskbar or notification area)
is left alone; the Primary Password prompt is accepted by the owner. The Windows
`AttendedStartAllowed` entry may be set true.

## Not covered

Thunderbird not the default mail app; the Desktop started by its Startup shortcut at sign-in
(the Start-menu and Explorer launches are covered, and with this start the launcher's
foreground right no longer matters, L-A10); a real (non-emulated) Windows PC. AS-Q's scope
(`WM_CLOSE` at quiet, L-A5) is not started: `AttendedQuitAllowed[windows]` stays false.
Seen in passing, not part of this scope: when MailArchive Desktop starts on Windows, a few
Windows Terminal windows flash for about 0.1 s each (cases 4–7 of run 1 and case F); to be
looked into separately.

*Added 2026-10-31 (MA-951), beside the note above, which stays as written:* read in the code,
the likeliest source is `schtasks.exe /Query`, a console program the Desktop runs for each
archive's scheduled backup as it starts (the health check asks up to three times per
archive), started with no console-window flag. Every program the Desktop and the scheduled
run start now goes through `nowindow.Hide` (CREATE_NO_WINDOW); proven on the lab VM as
SYSTEM through the start flags and a windowless stand-in parent. Not yet re-seen in the
signed-in session with an observer: that the flashes are gone is to be confirmed there.

*Added 2026-10-31 (MA-951 re-check, by Claude, a model, for the lead), beside the note above,
which stays as written:* re-seen in the owner's signed-in session (`prlctl exec
--current-user`), the observer running, each Desktop started by `Start-Process … desktop`,
kept 25 s, then shut down from its page. Before the fix (the run-2 build): Windows Terminal
was in front twice, at +3.8 s ("Terminal") and +7.9 s (titled
`C:\WINDOWS\system32\schtasks.exe`), which confirms the source. After (a build of the release branch at
`e9367f8`, which carries the fix, `908214e`): only Edge was in
front, no console window in any sample. The observer samples the foreground window every
100 ms, so a window that flashed without taking the front would not be seen; launched from
the Start menu and at sign-in, not re-run. Kit: `flash-check.ps1`, `flash-run.ps1`
(`~/repositories/.winlab/ma918`).

*Added 2026-10-31 (MA-918 add-on, by Claude, a model, for the lead), beside the "Not covered"
list above, which stays as written: two of its cases run.* Same set-up, the run-3 build
(release branch at `e9367f8`; the start code unchanged since, pin `10d7ba8f…`).

- **G, Thunderbird not the default mail app** (the owner set Outlook as the default mail app;
  `prefs.js` holds no `mail.shell.checkDefaultClient`, so Thunderbird's own check is on). Desktop
  started by `Start-Process`, the typer in Notepad. Thunderbird minimized from the first sample;
  no other Thunderbird window (no "make Thunderbird the default" question) in about 100 s; the
  keyboard stayed in Notepad (398 letters) except for the driver's own Run now press; ok, 2
  exported. (A first try typed nothing: Notepad was closed by the sign-out; re-run.) Whether
  Thunderbird judged itself not the default was not read; only that no question appeared.
- **H, the Desktop started by its Startup shortcut at sign-in** (a test shortcut like the
  installer's, `desktop -no-browser`; the owner signed out and in). The Desktop started at
  18:11:13 with parent `explorer`; a Start-menu-style start reconnected to it (same process).
  Run now from that process: Thunderbird minimized from the first sample, never in front (the
  foreground was Edge, where the driver's press put it, the typer not regaining it); ok, 2
  exported.

Still not covered: a real (non-emulated) Windows PC.
