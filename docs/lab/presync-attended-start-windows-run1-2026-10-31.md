# Windows attended start lab, run 1 — 2026-10-31 (MA-918, first-release scope, partial)

**Verdict: NOT PASSED. A keyboard steal was found, so the Windows `AttendedStartAllowed`
entry stays false (design-presync-attended §2.7: "a focus steal blocks the Windows entry
and goes to the owner").** This file is a run record, deliberately not named
`presync-attended-start-windows.md`: that name is the Windows start entry's own record,
whose existence ends the pin's "no lab record" period (MA-927), and this run does not pass.

## Set-up

- Windows 11 on ARM (Parallels VM "Windows 11", host the owner's Mac). MailArchive is an
  amd64 build running under emulation, so **every timing here is an emulated timing**.
- Thunderbird 153.6.0esr x64, `Thunderbird Setup 153.6.0esr.exe` from
  download-installer.cdn.mozilla.net, SHA-256
  `76ef8cfd592b9ee8461fae321794250b49ad5f3290337486aa29c1b3a3304d4e` matching Mozilla's
  SHA256SUMS, Authenticode "Valid", signer CN=Mozilla Corporation. Installed silently with
  the owner's OK ("yes - download", 2026-10-31).
- Profile `default-esr` with one IMAP account (Gmail) the owner added by hand.
- The program: a **lab build** of commit e873077 with only the Windows
  `AttendedStartAllowed` entry set true (never committed; worktree
  `.claude/worktrees/lab-win`). Windows code pin of that build:
  `92a98b8c88e92dc61e8c1cc7ff003df777419ab2c28e090e099908bde7994b52`.
- Archive `C:\Users\alex.dimarco\MA918-archive`, its schedule made in the Desktop's
  Settings (daily, "Sync Thunderbird first" on), so Run now runs that job.
- Observer: a separate process (`observer.exe`, source beside the lab kit) sampling every
  100 ms the foreground window (process, title) and every visible Thunderbird top-level
  window with its minimized state; it sends no input. Typer: a separate process typing a
  middle dot (no Thunderbird shortcut) only while Notepad has the keyboard, logging each
  time the keyboard leaves or returns. Neither is the process that starts Thunderbird.
- Runs 3–6 were driven by a separate PowerShell process through Windows UI Automation
  (Invoke on the page's buttons in Edge), not by the person; it sends no keystrokes.

## Runs

| # | How the Desktop was started | Who pressed Run now | Thunderbird window on screen | Keyboard | Outcome |
|---|---|---|---|---|---|
| 1 | remotely (`prlctl exec`, current user) | the owner, in Edge | the owner saw "a quick flash and close (too quick to identify)"; observer was not running (lost) | not measured | Stop waiting at 7 min (the owner); 20,351 archived |
| 2 | same | the owner | — Thunderbird already open | — | `already-running`, left alone; 141 archived; note "Thunderbird was already open, so it was not started or closed" |
| 3 | same | the owner, then clicked into Notepad | shown 09:02:47.23, minimized by 09:02:47.64: **about 0.4 s** | **stayed in Notepad**; typer never interrupted | Stop waiting (the owner); 135 archived |
| 4 | same | UI Automation | never seen shown (minimized at first sample): **under 0.1 s** | stayed in Edge | **Cancel** pressed during the wait: run `cancelled` 27 ms later, nothing archived, **Thunderbird left open** |
| 5 | **from a terminal** (`wt.exe -- cmd /k mailarchivew.exe desktop`) | UI Automation | shown 09:12:37.70, minimized by 09:12:37.91 (~0.2 s) | **Thunderbird took the keyboard** at 09:12:37.70; back to Edge at 09:12:37.91 | Stop waiting; 36,440 archived |
| 5b | from the same terminal (repeat) | UI Automation | shown 09:15:58.77, minimized by 09:15:58.88 | **Thunderbird took the keyboard and kept it ~20 s while minimized** (09:15:58.77 → 09:16:18.53, when the driver pressed a button in Edge) | Stop waiting; 1,857 archived |
| 6 | from a shortcut opened through Explorer (parent: explorer.exe) | UI Automation | shown 09:18:03.70, minimized by 09:18:03.81: **≤ 0.1 s** | stayed in Edge | Stop waiting |

## Findings

1. **Keyboard steal when the Desktop is started from a terminal (runs 5, 5b): blocks the
   Windows entry.** Inferred cause (not proven): a process started by the foreground
   process may itself take the foreground, and the Desktop, a windowless process, passes
   that right on to the Thunderbird it starts; started remotely or through Explorer it
   had none. While Thunderbird holds the keyboard, keys typed act on its selected message.
   **Run 6 is weaker evidence than it looks:** a script opened the shortcut, not a
   person's click; a real Start-menu click gives Explorer the foreground right and may
   behave like the terminal. To be tested with a person.
2. Cancel leaves Thunderbird open (run 4): as designed. The cancelled run's record carries
   no outcome `code` (other outcomes do); minor.
3. "Stop waiting and back up now" works on Windows (runs 1, 3, 5, 5b, 6).
4. The flash, when it shows: 0.1–0.4 s emulated.

## Not covered by this run (first-release scope still owed)

Primary Password and "not the default mail app" slow starts; the notification-area option
on; the Desktop started from its Startup shortcut at sign-in; a real person's Start-menu
click; the list of files changed before the first fetch (L-A9 on Windows). AS-Q scope
(close, L-A5) not started.

## Next

A fix for finding 1 before any Windows flip — candidates for the owner: start Thunderbird
so it cannot take the foreground (the L-A10 `SW_SHOWMINNOACTIVE` start, or a launch
through a process without the foreground right), or have the Desktop give up its own
foreground right before it starts Thunderbird. Then re-run this lab, including a real
Start-menu click.

## Lab note (appended 2026-10-31): Thunderbird's default for a new IMAP account

Not part of the MA-918 verdict above. The same set-up answers a question the guide's truth
review held (T8 in `docs/review-help-page-friction.md`): does Thunderbird keep a new IMAP
account's messages on the computer without being told to?

- Thunderbird 153.6.0esr, the Gmail IMAP account the owner added by hand, with no settings
  changed.
- Its `prefs.js` has **no** `mail.server.serverN.offline_download` line for the account,
  so Thunderbird's own default applies. (Read in the VM by the lead; this note's author did
  not read the file again.)
- Thunderbird saved over 36,000 messages to disk in the first half hour (the lead's
  observation). The runs above agree: MailArchive reads only the files Thunderbird saved,
  and run 1 archived 20,351 messages and run 5 36,440 from this profile.

So "Keep messages for this account on this computer" is on by default for a new IMAP
account. Downloading still takes time, and happens only while Thunderbird is open: a
message not downloaded yet is not in the archive yet. The guide (MA-945) and the wizard's
IMAP question (MA-947) now say this. One account, one Thunderbird version, one provider
(Gmail); other versions or providers were not tried.
