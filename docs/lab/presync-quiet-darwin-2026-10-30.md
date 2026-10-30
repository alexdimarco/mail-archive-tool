# Lab record — macOS quiet test, 2026-10-30 14:14–14:23

Owner's own Thunderbird profile, with the owner's OK in chat ("thunderbird is closed so go ahead").
The profile's folder name and the user name are left out of this record.

Harness: `docs/lab/presync-lab-mac.py quiet --minutes 8 --profile <the owner's default profile>`.
Launch as shipped in v1.5.1: `open -g -j`; quit by an AppleScript addressed by bundle id.
Observer: a focus watcher reading `lsappinfo` every 0.5 s.
The screen was locked throughout (frontmost app = loginwindow), so lab L-A1 (does the hidden
start take the keyboard while the owner types?) is NOT answered by this run.

## What happened (seconds after the start)

- Start: `open -g -j` returned 0; the Thunderbird process was seen after 1 s.
- +4 s: first file change, `INBOX.msf` (the folder index), before any mail data.
- +6 s: first mail data, `INBOX` (the mbox) grew; again at +14 s.
- +16 s `Sent Items`; +18 s `Deleted Items.msf`.
- +38 s to +50 s: `INBOX.msf` touched every 2 s; +54 s `Sent Items.msf`;
  +60 s `Local Folders/Unsent Messages.msf` and `Trash.msf`.
- Then quiet until +365 s: one `INBOX.msf` touch (the periodic check, about 5–6 minutes).
- Quit: the AppleScript request returned 0 in 0.5 s, with no permission prompt (already granted
  to the calling chain); Thunderbird exited 1 s later.

## Findings

1. The hidden start works, and on this profile new mail arrived within seconds of the start.
   This differs from the 2026-10-21 run, where offline download waited for the ~10-minute check.
2. All mail activity settled by +60 s; a 3-minute quiet rule would end the wait at about +240 s.
3. An index (`.msf`) touch came 2 s before the first mbox write. A "first change" gate that counts
   any file would trip on it (lab L-A9's question). Harmless here because data followed 2 s later;
   not proven in general.
4. The later periodic touch at +365 s would not extend a wait that already ended at +240 s.

## Still open

L-A1 (focus while typing), L-A2 (the first quit-permission prompt raised by MailArchive itself),
L-A5 (does closing discard a draft?), L-A8.
