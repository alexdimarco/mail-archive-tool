# Dialogs in front of the browser on Windows — lab walk, 3 November 2026 (MA-1110)

Owner report (2 November 2026, Windows, MailArchive Desktop): "starting the wizard
(in windows using the desktop app) - the wizard window hides behind the browser and
is not seen".

## Set-up

- Parallels VM "Windows 11" (ARM64, build 10.0.26100; the amd64 program runs under
  Windows' x64 emulation), signed in as the VM's user, Microsoft Edge open.
- The real program, built from branch `windows-dialog-front`:
  `GOOS=windows GOARCH=amd64 go build -ldflags=-H=windowsgui ./cmd/mailarchive`,
  started as `mailarchivew.exe desktop -config <scratch>\graph-config.json` from a
  scheduled task in the signed-in session (no archive chosen yet). Desktop opened
  its dashboard in Edge.
- Every key was a real key event sent to the VM (`prlctl send-key-event`); buttons
  were pressed from the keyboard (Ctrl+F, the button's words, Esc, Enter). Each
  step's evidence is a screenshot of the VM screen (`prlctl capture`), taken on the
  Mac (not committed; kept with the run's notes).

## What was seen (VM clock 00:18–00:21)

| Step | Result |
|---|---|
| Dashboard open in Edge (`walk-1-dashboard.png`) | Edge active |
| Press **Make a new archive…** (`walk-2-wizard-front.png`) | the wizard's first window, "What are you exporting?", is in front of Edge; Edge's title bar is greyed (not the active window); the dialog has its own taskbar button |
| Type `s` (`walk-3-wizard-keys.png`) | the list jumps to "Single mbox file": the key went to the dialog, not to Edge |
| Esc (`walk-4-after-esc.png`) | the dialog closes; the page says "No archive was added." |
| Press **Open an archive folder…** (`walk-5-picker-front.png`) | Desktop's own folder picker, "Choose the archive's folder", is in front of Edge with the cursor in its Folder box; Edge greyed |
| Type `x` (`walk-6-picker-typed.png`) | the x's land in the picker's Folder box (repeated: the key was held between two separate press and release commands) |
| Esc (`walk-7-picker-cancelled.png`) | the picker closes, back to the dashboard |

Before the fix (prototype stage, same VM, 2 November 2026): the plain build's
dialogs opened behind Edge and a typed key went to Edge
(`dfshots/base-01-list-typed.png`).

## After the adversarial review (the settle step, VM clock 01:35–01:51)

The review found that the first version brought forward every dialog, even one
that opened with no click: keys typed into Edge answered "Export complete" and the
schedule question (finding 1). The fix now gives the keyboard only to a dialog the
person asked for (R39) and gives it back to the browser when the dialogs close.

| Step | Result |
|---|---|
| The review's focus-steal probe built from the reviewed commit 285375b, started by a scheduled task (no console); 8 s later, with no click, "Export complete" and the schedule list show while "hi", Enter, Enter are typed into a new Edge tab (`steal-old-1-shown.png`) | the dialog came up active in front of Edge; its log: summary OK, schedule "No" — the keys answered both |
| The same probe built from the fix (`steal-new-2-after-enter.png`) | Edge kept the keyboard: a Bing search for "hi"; both dialogs timed out unanswered; the probe's taskbar button flashed |
| The real `mailarchivew.exe desktop` from the fix, dashboard in Edge (`final-1-dashboard.png`) | Edge active |
| Press **Make a new archive…** (`final-2-wizard-front.png`) | the wizard's "What are you exporting?" in front of Edge, active |
| Type `s` (`final-3-wizard-keys.png`) | the list moves to "Single mbox file" |
| Esc (`final-4-after-esc.png`) | "No archive was added."; Edge is the active window again |
| Press **Open an archive folder…** (`final-5-picker-front.png`) | "Choose the archive's folder" in front of Edge, the cursor in its Folder box |
| Esc, then Ctrl+F with no click (`final-7-edge-has-keys.png`) | Edge's find bar opens: the keyboard went back to Edge (finding 3) |

The lab test MA-1113 (`MAILARCHIVE_DESKTOP_LAB=1`) ran green 5 of 5 on the VM, and
red 2 of 2 for each of: every dialog taking the keyboard, `Install` doing nothing,
the give-back removed. Windows lets a just-started program's first window forward
by itself (seen here with no watch at all), so the test uses that up with a plain
window first; its "another program" in these runs was the Windows Terminal window
that `prlctl exec` opens, not Edge.

## Not covered

- A real PC with a native amd64 processor, Windows 10, more than one monitor, a real
  mouse click (keyboard only here), and an administrator program in front.
- A real long export (the summary after it was a probe's stand-in), Desktop's
  Resume button and the encrypted-archive pickers were not walked.
- (Fixed in the settle step: after the folder picker closes, Edge is made the
  active window again.)
