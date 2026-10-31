# Lab record — macOS attended start (L-A1, L-A9), 2026-10-30

Owner's own Thunderbird profile (the profile folder name and user name are left out), with the
owner's OK in chat that night: "start Thunderbird on your real profile" and "flip the Mac or Windows
switch that turns the feature on for real… the risk is low". The design asks for a test profile
[C-67]; the owner chose the real one.

Two runs of `docs/lab/presync-lab-mac.py quiet`. Both use the shipped launch `open -g -j` and quit
with an AppleScript addressed by bundle id. A focus watcher read `lsappinfo` every 0.5 s.

## Run 1 — 14:14–14:23, screen locked

Full record: `presync-quiet-darwin-2026-10-30.md`.
- Front app: loginwindow throughout, so L-A1 was not answered.
- First file change at +4 s: the folder index `INBOX.msf`.
- First mail data at +6 s: the mailbox file `INBOX` grew.
- All activity settled by +60 s.

## Run 2 — 22:08:48–22:20:52, screen unlocked, the computer in use

Focus (L-A1):

| Time after start | App in front |
|---|---|
| 0 s to +132 s | Terminal. Thunderbird started at +1 s; Terminal kept the keyboard. |
| +132 s to +725 s | The person switched between Debut Ops, Terminal, Obsidian, ONLYOFFICE, Chromium and Firefox. |

- Thunderbird was never the front app, at the start or during its mail fetch.
- Thunderbird exited at +725 s; Firefox was in front.

File changes (L-A9). The watch covered sizes and times of every file under `ImapMail` and `Mail`,
which is a superset of the product's `digestWatch`:
- No file changed for 573 s.
- +573 s: `INBOX.msf`. +575 s: `INBOX.msf` and `INBOX`; the mail data grew.
- +607 s: `INBOX.msf`.
- Quiet from then on. The quit was asked at +722 s; it was accepted in 0.5 s, and Thunderbird
  exited 1 s later with no prompt.

## Findings

1. **L-A1:** the hidden start did not take the keyboard. The app in front stayed the one the person
   was using, through the start and through the fetch, across seven app switches. The owner confirmed
   in chat: "yes I was using the mac". Asked whether anything appeared, the owner answered: "no popup at all". It was
   not checked whether a key was pressed in the exact second of the start.
2. **L-A9:** Thunderbird's first mail fetch after a hidden start varies.
   - Run 1 fetched at +6 s.
   - Run 2 waited for the periodic check at about +9.5 minutes, which matches the 2026-10-21 finding.
   - In both runs an index (`.msf`) touch came about 2 s before the mail data.
   - Neither run showed an index touch followed by a long silence. However, the gate as built counts
     any file change, so a lone index touch would start the 3-minute quiet clock early.
   - Following the design's L-A9 rule and the owner's "go ahead with the wait fix", the first-change
     gate is changed to count only message-store data before the macOS start entry is set.
3. **What Run now waits for:** expect about 13 minutes on a profile like run 2's (the periodic fetch
   plus 3 minutes of quiet), and up to 20 minutes when no mail comes. "Stop waiting and back up now"
   ends the wait sooner.

## What this record set (added 2026-10-31)

Platform: darwin (macOS). This record, with the wait fix of finding 2 (commit 4f090e8, MA-932: the
first-change gate counts only mail data), sets the darwin entry of `AttendedStartAllowed` true
(`internal/presync/attended.go`). It does not set the macOS `AttendedQuitAllowed` entry: that needs
L-A5, not run. Still open from this record: the profile was the owner's, not a test profile [C-67];
a key pressed in the very second of the start was not checked; the pre-sync design's quiet-rule
review of the wait fix is not held.
