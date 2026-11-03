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

## Not covered

- A real PC with a native amd64 processor, Windows 10, more than one monitor, a real
  mouse click (keyboard only here), and an administrator program in front.
- After the folder picker closes, Edge is not made the active window again (its
  title bar stays greyed until clicked); the prototype's run showed the same. Not
  changed here.
