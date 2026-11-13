# Lab: menu-bar slice S1, Mac smoke (L-M1's S1 run, part) — 2026-11-13

Run by the S1 builder (an agent) on the owner's Mac (macOS 26.6.2, Apple silicon), branch
`menubar`, against the S1 build. The owner's own MailArchive (a 1.7.0 Desktop may be
running) was not touched: the lab used a **copy** of the built app under the worktree's
`.claude/s1runs/lab/`, with its bundle id changed to `com.dimarcotech.mailarchive.s1lab`
(so Launch Services never routes it to the owner's app) and its own `HOME` (passed with
`open --env HOME=…`), so its lock and instance file were in a lab folder. No Desktop link
was opened in the owner's browser: a lab helper redeemed one over loopback with `GET`.

## What was built
`packaging/macos/build-app.sh 1.8.0` (universal), unzipped, renamed `Mail Archive
S1Lab.app`, bundle id and name changed with `plutil`. Later rebuilds replaced only the
arm64 program (`go build ./cmd/mailarchive`) to add log lines. The bundle shape is the
product's: `Contents/MacOS/mailarchive-gui` a symlink to `mailarchive`, `LSUIElement` true.
The program's own version stamp read `v1.5.6+dirty` (a dev build), the bundle's `1.8.0`.

## Steps and results
1. `open` the lab app. `open` exit 0; one process; its stderr: "the macOS event loop runs";
   `sample` shows `-[NSApplication run]` on the main thread; the first choice showed (an
   `osascript` child). Instance file: `acts=[open choose wizard] icon=not-yet`.
2. Opened a Desktop session over loopback (so a Cancel would not stop it, MB-16), then
   dismissed the first choice by ending its `osascript` (read as Cancel): the instance kept
   running, as designed.
3. **`open` the app again while it runs: `open` exit 0 (no error -600), still one process,
   stderr "macOS opened the app again; showing the choice", and a new `osascript` child of
   the running instance: the choice.** `lsappinfo front` named the lab app as the front
   application while the choice showed. **Screenshot: not taken** — `screencapture` fails
   ("could not create image from display"; the terminal has no Screen Recording
   permission), and the choice is drawn by `osascript`, not by MailArchive, so the app
   cannot render it itself.
4. **Found and fixed in this run:** the first try of step 3 showed nothing. The log said
   "the choice window could not be shown: unsupported option: extra button": on macOS the
   dialog library's list has no extra button, which MB-14's Quit used (the dev stamp made
   the bundle look newer). Quit is now a third list item, "Quit MailArchive — then open it
   again to use the newer version", on every platform; the run in step 3 is after the fix.
5. Two copies: a second copy at another path, opened while the first ran. Launch Services
   started a second process (as §7.2 expects for a launch it does not route); it found the
   lock held and handed off `choose`; the first copy's choice was still on screen, so the
   second launch said "MailArchive is already open, and its window is on your screen. Look
   for it there or in the taskbar." (Friction note: "taskbar" is the Windows word.)
6. SIGTERM to the instance: it left the event loop, removed its instance file and let go
   of the lock (only `desktop.lock` left, unheld).

## Observations, not product findings
- Ending a dialog's `osascript` from outside twice left two `osascript` processes stuck
  "exiting" (state `?E`, even after `kill -9`), and the second copy's launcher, blocked in
  its notice dialog, ignored SIGTERM until `kill -9`. This is the lab's way of dismissing a
  dialog, not a person's click; a launcher blocked in a notice dialog not ending on SIGTERM
  is noted for the adversarial pass.

7. MB-19, part: a backup of a one-message mbox (lab `HOME`) with the purego build, arm64
   (the app's program) and amd64 under Rosetta (`GOARCH=amd64 go build`): both
   "exported=1 … indexed=1".

## Second run: the finishing pass on b0a9fbe (2026-11-13, evening)

Run by the finishing agent on the same Mac, against `b0a9fbe` as pushed (no rebuild with
log lines). `packaging/macos/build-app.sh 1.8.0`, unzipped under the worktree's
`.claude/s1lab2/`, renamed `Mail Archive S1Lab2.app`, bundle id
`com.dimarcotech.mailarchive.s1lab2`. Its `CFBundleShortVersionString` was set to `1.5.6`
to match the program's own stamp (`v1.5.6+dirty`: Go stamps a worktree build from the
parent repo's `.git`, so a lab build reads as main's version; a release build has the two
equal), so the choice showed without MB-14's newer line. Own `HOME` and `PATH` through
`open --env`. A lab-only `osascript` stand-in first on that `PATH` (never committed)
wrote the browser opener's `open location` link to a lab file instead of opening any
browser, passed every dialog to the real `/usr/bin/osascript`, and, only when a one-shot
lab file said so, answered the choice list with "Open MailArchive Desktop" (the person's
click, at the dialog seam). Links were redeemed over loopback with `curl` and a cookie jar.

1. **First `open`:** exit 0; one `mailarchive` process; stderr "the macOS event loop
   runs". The first choice listed the two items with Run the wizard the default (the lab
   has no archive, §6.1). Answered Desktop: the instance's own `/_open?t=` link came to
   the opener, and with that link's cookie the dashboard served (200, 63,866 bytes).
   Instance file: `acts=[open choose wizard] icon=not-yet`.
2. **`open` again while it runs:** `open` exit 0 with no output (no error −600); still
   one `mailarchive` process (pid 56875); stderr "macOS opened the app again; showing the
   choice"; the real `osascript` choice ran as its child, and `lsappinfo front` named
   "Mail Archive S1Lab2", pid 56875, as the front app. Dismissed by ending that
   `osascript` once (SIGTERM, read as no pick): the instance kept running, no process
   left stuck.
3. **`open` a third time, the choice answered "Open MailArchive Desktop":** a new
   one-time link to the same running copy (same port); redeemed with a fresh cookie jar,
   the dashboard served (200, 63,866 bytes); the same page with no cookie: 401. Still
   one process.
4. **Quit with no work running:** macOS's own quit request
   (`tell application id "com.dimarcotech.mailarchive.s1lab2" to quit`, the
   `applicationShouldTerminate:` path): `osascript` exit 0 at once, no question asked
   (nothing ran), the process gone, its instance file removed, only `desktop.lock` left.
5. **Screenshot: not taken.** `screencapture` again failed ("could not create image from
   display"); the choice is drawn by `osascript`, so the app has no render of its own.

Beside the lab, a second prove-fail sweep (9 mutations of S1 pieces the first sweep did
not touch; 9 of 9 red) found one unchecked line: nothing tested that a launch is told
"MailArchive is already open, and its window is on your screen…"; TestMA1427 now does.

Not shown by this run: a person's own click on the list (the pick came through the dialog
seam), Quit while work runs (the Quit question), the product's own bundle id.

## Not done here (still owed for MA-1436)
- The product's own bundle id (only a copy with another id was safe to run beside the
  owner's app), a real person's click on the app, and a screenshot (needs Screen Recording
  for the terminal, or a person).
- MB-19's `run-job` with the purego build on both slices; a sign-out with a backup running
  (the `applicationShouldTerminate:` path).
