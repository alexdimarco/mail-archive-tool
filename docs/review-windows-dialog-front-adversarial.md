# Adversarial pass: Windows dialogs in front of the browser (1.5.5)

Date: 2026-11-03, about 03:10–03:25. Branch `windows-dialog-front` (off v1.5.4 = main
6bd3ca0). Reviewed: commit 14dad33 (rule R39, rows MA-1110..MA-1117), read against main in a
throwaway clone, by the adversarial agent of workflow wf_f15e48ad-ba9. Settled in the commit
after it (the release settle step).

**Verdict of the pass: CLEAN.** 0 high, 0 medium, 4 low, 1 note. Nothing found lets a web
page, another program or a background job take the keyboard on its own. The pass ran no probes
on the Windows VM (another workflow had a dialog open there at 03:12); it ran the package's
decision logic on the Mac the way the Windows code calls it, and read the Windows calls.

## Findings and what was done

| # | Finding | Disposition |
|---|---|---|
| L1 | One person's answer can let a different flow's window take the keyboard. `onEvent` calls `Arm` whenever one of our dialogs closes while active, and the ask is not tied to a flow. With an add-archive export running in the background, the person opens **Open an archive folder…**, presses Esc and goes back to the browser; if the export's "Export complete" summary appears within 8 s it takes the keyboard, and Enter keys could answer it and then the schedule question (default "No"). Confirmed with the logic on the Mac. | **Declined, recorded** (MA-1114 note, R39 note). The suggested fix (a 2 s life for an answer's ask) would make the person's own next window only flash after a slow mail scan — the friction review's note B measured up to 4 s on an empty VM, and a real PC can be slower — against the lead's 8-second rule. The watch sees only windows, not flows, and tying an ask to a flow would mean changing every dialog call (not tried here). The case needs two flows at once; the worst outcome is the one R39 exists for, so it stays a known limit to revisit if seen. |
| L2 | The zero-distance mouse move restarts Windows' idle countdown: a person who left our progress window in front and walked away has the screen lock put off when the summary comes forward, though the move is not needed then (our program is already in front). | **Fixed** (new row MA-1118, MA-1113 amended). `bring` sends the move only when the active window is not ours, or when Windows refuses without it. Confirmed on the VM first: with the reviewed rule, the session's last-input tick moved 1547 ms while a dialog came forward from our own active one. |
| L3 | Typing can go back to an old window: the window to return to was set only when a dialog came forward by the watch, so after the person clicked a flashing dialog from another program, the keyboard went back to an earlier flow's browser. Confirmed with the logic on the Mac. | **Fixed** (MA-1116 amended). When one of our dialogs becomes active straight from another program's window, that window becomes the one to return to; one made active from another of ours leaves it as it is. |
| L4 | The 8-second check used the wall clock (`UnixNano`), so setting the clock back between a press and a dialog could let an old press count once. | **Fixed** (MA-1114 amended). The ask is timed on the process's steady clock (Go's monotonic reading, `time.Since(start)`, through a `steady` seam). |
| Note | `onEvent` compares the event number on all 64 bits; junk in the top half would make events be ignored, the safe direction. The retries can bring a dialog forward up to 1.2 s after the 8 s end, within the accepted rule. | **No change**, as the pass said. |

## Prove-fail record

Tests were written first; seams were then added with the reviewed behaviour, and the new and
amended tests ran red on the Mac (`go test ./internal/dialogfront/`):

- MA-1114 (L4): "an ask 1 s old on the steady clock: asked true, 1µs ago" and "an ask 9s old
  … 0s ago"; the amended "older than 8 s" case: "an ask made 0s ago still brings a dialog
  forward".
- MA-1116 (L3): "after the person clicked a flashing dialog from 0x30, the keyboard goes back
  to 0x20, want 0x30".
- MA-1118 (L2): "our window active: bring made "lift nudge set", want "lift set"", and the
  refusal case.

With the fixes: green. Mutations of the fixes, each red then reverted: `bring` never sending
the move (the browser, no-window and refusal cases red); counting a window of ours as the one
to return to (MA-1116 red, "goes back to 0x0"). On the lab VM, MA-1113's new chain case was red
with the reviewed rule ("the session's last input moved from tick 214856656 to 214858203") and
green 2 of 2 with the fix, Edge being the other program.

## What held (from the pass)

- A web page cannot set the ask: `Arm` is called only inside existing Desktop handlers behind
  the session cookie, the anti-forgery token and a local Origin and Host; the branch adds no
  web address.
- Other programs' windows are not touched: the show/hide hook is limited to our process id by
  Windows; the system-wide active-window hook only reads; the only other window acted on is the
  give-back, and only when no window is active.
- Background work cannot bring a window forward: only `desktop.Launch` and `wizard.Run`
  install the watch; scheduled and headless runs never do (MA-1111, MA-1117 walks).
- Structure sizes and handles are right; no lock is held across a Windows call; the hook thread
  lives as long as the process; the fake move carries no button flags.
- macOS and Linux are unchanged.

## Not checked

A native amd64 PC, Windows 10, several monitors, an administrator program or the lock screen in
front, a full-screen game, a real mouse click; `GuardLocalPOST` itself was not re-reviewed.
