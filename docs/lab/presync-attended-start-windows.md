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
