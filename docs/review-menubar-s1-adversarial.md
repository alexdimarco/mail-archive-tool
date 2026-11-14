# Adversarial pass: menu-bar slice S1 (one running copy, the launcher choice, stops that wait)

Date: 2026-11-13. Reviewed: branch `menubar`, commit **c1df91b** (S1 = 74a6633..c1df91b on top
of 1a6d437), read in the worktree `.claude/worktrees/menubar-s1-adv` (branch
`wip/menubar-s1-adv`). Procedure: `assurance-kit/process/adversarial-review.md` (finder lenses
outside / insider / concurrency, then a skeptic step that keeps only what was reproduced or
shown in the source). One agent played both roles in sequence; every finding below says how it
was checked. This pass attacks and reports; it fixes no product code.

**Verdict: NOT CLEAN.** 0 critical, 0 high, **4 medium, 5 low**. 14 suspected attacks refuted
(listed at the end). Nothing found lets another account, a web page or a sandboxed program use
the hand-off channel: without the key K every request is refused before anything happens. The
medium findings are about stops (one Windows message silently switches off the sign-out wait;
the upgrade script force-kills the copy that now hosts the wizard; an open wizard is not counted
as work) and one broken promise of rule R50 (a launch from another sign-in can still open the
browser on the other screen).

Header facts:
- Toolchain: go1.26.0 darwin/arm64 (Mac probes); Windows probes on the lab VM "Windows 11"
  (ARM64, amd64 emulated), signed-in user `alex.dimarco`, session 3, through
  `prlctl exec --current-user`, in throw-away `%TEMP%\advs1-*` folders with their own
  `LOCALAPPDATA`, `APPDATA`, `-config` and `-addr 127.0.0.1:0` (so the former-address probe and
  its "end the older copy" step never ran). Only processes the probe started were touched; a
  leftover check afterwards found none.
- Blob ids at c1df91b: `internal/instance/handoff.go` ffad27d, `internal/desktop/front.go`
  c224a62, `handoffact.go` 4b0d98d, `apploop_windows.go` a0884fb, `apploop_darwin.go` 475312e,
  `stopwait.go` cfc6fa0, `launch.go` 1c09796, `internal/wizard/ininstance.go` 3b06c2e,
  `packaging/mailarchive-desktop/Install-RMM.ps1` d1062fb.
- Probes (not committed; build tag `advprobe`, or files under the worktree's `.claude/`):
  `go test -tags advprobe -run AdvProbe -v ./internal/instance/` (exit 0) and
  `./internal/desktop/` (exit 0); the VM scripts `run.ps1` and `low.ps1` (exit 0; the first
  `low.ps1` call failed inside `prlctl` with "PrlJob_GetResult: Invalid argument" and was rerun).
- Count: **23 suspected / 9 confirmed / 14 refuted.**

## Findings

| ID | Sev | Platform | Claim | Check |
|---|---|---|---|---|
| A1 | MED | Windows | A `WM_CLOSE` (what `taskkill` without `/F` sends) destroys the hidden window; the copy keeps running but no longer hears sign-out or an installer, so a later sign-out kills it with no wait | PROVEN on the VM |
| A2 | MED | Windows | Install-RMM.ps1 force-kills the `start` copy, which now hosts the wizard and Run now: no wait, no "interrupted" record, an open wizard's copy cut | PROVEN in the source |
| A3 | MED | macOS, Linux | R50 broken on the "open" path: a `desktop` launch from another sign-in opens the browser on the other screen and the launch is told nothing; an unknown sign-in ("") on either side also shows the choice there | PROVEN (Mac probe) |
| A4 | MED | all | An open wizard inside the copy is not counted as work: Shut down, Quit, sign-out and the first choice's Cancel end it with no question | PROVEN (Mac probe) |
| A5 | LOW | Windows | Any normal program on the same desktop can stop the copy at once, mid-backup, with no question, by sending `WM_ENDSESSION` | PROVEN on the VM |
| A6 | LOW | all | A captured hand-off request is accepted again (no record of used nonces) | PROVEN (Mac probe) |
| A7 | LOW | all (reopen: macOS) | A copy that is already stopping still shows the choice and starts the wizard; the launch's wait during the stop has zero margin | PROVEN (Mac probe) |
| A8 | LOW | Windows | Sign-out waits only 5 s, the drain needs up to 15 s: a Run now that has not reached its safe point is killed and left "running", not "cancelled" | PLAUSIBLE |
| A9 | LOW | Linux | The Wayland activation token is sent and signed but never used | PROVEN in the source |

### A1 (MEDIUM, Windows) — `taskkill` quietly switches off the sign-out wait

**What happens.** `wndProc` in `internal/desktop/apploop_windows.go:118` answers `WM_CLOSE` with
`DestroyWindow`, and `WM_DESTROY` posts `WM_QUIT`, which ends the message loop. Nothing tells the
instance; it keeps serving. From then on Windows has no window to send `WM_QUERYENDSESSION` /
`WM_ENDSESSION` to, so at sign-out, shut-down or a Restart Manager close the process is simply
ended: no drain, a running backup left as a crash leaves it. R52 ("no stop silently cuts work
short") is lost without a sign.

**Proof (VM, `run.ps1` case 1).** A real `mailarchivew.exe start -background` from this commit:

```
case1 pid=2936 alive=True instance windows before=1
SUCCESS: Sent termination signal to the process with PID 2936.
case1 after taskkill (no /F): alive=True instance windows=0
case1 now a foreign WM_ENDSESSION: no MailArchiveInstanceWindow for that pid
```

`taskkill` also reports SUCCESS for a process that did not stop, which misleads the person or
admin who ran it.

**Fix, in plain words.** Do not let a close request from outside take the window down. Either
ignore `WM_CLOSE`, or treat it as the one stop (`life.Stop`, which waits). The loop's own end
(after serving returns) should destroy the window through its own path (for example a private
message only this process posts, checked against a flag set when serving ended), never through
`WM_CLOSE`.

### A2 (MEDIUM, Windows) — the RMM upgrade force-kills the copy that now hosts the wizard

**What happens.** `packaging/mailarchive-desktop/Install-RMM.ps1:28-31` now counts a first
argument `start` as a dashboard and ends it with `Stop-Process -Force` (TerminateProcess: no
drain). Since S1 that process is where:
- the wizard runs when the "MailArchive wizard" shortcut is clicked while MailArchive runs
  (`wizard.Run` asks `HandOff` first, `internal/wizard/wizard.go` S1 hunk; `handOffWizard` in
  `internal/desktop/handoffact.go:100`, and the `wizard` process exits 0), and when the person
  picks "Run the wizard" in the choice (`front.showChoice`);
- Run now, a restore, an encryption copy and the Extraction export run.

So the script's own promise in the same file ("an open wizard is left to finish (MA-614, SC34)")
no longer holds: there is no separate `wizard` process left to spare. The wizard's export (hours
for a large PST) and a Run now are cut mid-write with no "interrupted" record, against R52
("every stop … an installer … waits … then records it as interrupted").

**Check.** Source: the regex `'^start(\s|$)'` matches both `start` and `start -background`; the
hand-off path above puts the wizard in that process (proven live by MA-1430's own test and this
pass's A4 probe, where the `wizard` hand-off ran the wizard inside the `start` copy).

**Fix, in plain words.** Ask the copy to stop the graceful way first and wait for it (for
example `taskkill` without `/F`, once A1 makes `WM_CLOSE` the one stop, or a small
`mailarchive stop` verb that uses the hand-off key and waits), give it the drain's bound, and
force-kill only after that. Or leave a busy copy running and let it meet the MSI's own close
request. Update MA-614's text, which still says a wizard is never stopped.

### A3 (MEDIUM, macOS and Linux) — a launch from another sign-in can still open on the other screen

**What happens.** R50 now says: when the running copy belongs to another sign-in of this account,
"the launch says so in its own session … and nothing shows on the other screen". Only the
`choose` and `wizard` actions check the sign-in (`front.act`, `internal/desktop/front.go:106`).
- The "open" path never checks. `mailarchive desktop` (and `start` when the copy answers "can't
  show here") uses the first request form (`instance.Reach`, `launch.go:428`), which carries no
  sign-in at all, so the copy opens its browser, with a fresh signed-in Desktop page, on its own
  screen, and the launch ends with nothing said. Realistic ways in: a Linux user who copied the
  pre-S1 desktop entry (`Exec=mailarchive desktop`; the READ ME has them copy it, and an upgrade
  does not replace that copy) and signs in a second time through xrdp; `mailarchive desktop` over
  SSH on a Mac.
- The check is skipped when either side's sign-in is "" (`r.Session != "" && f.session != ""`).
  On Linux the sign-in is `XDG_SESSION_ID` from the environment; a launcher or a copy started
  without it (some autostart and systemd-user paths, `su`, a container) shows the choice on the
  other screen. Whether `XDG_SESSION_ID` is reliably set and distinct for apps that GNOME starts
  through the systemd user manager is PLAUSIBLE-unreliable and needs the Linux lab.

**Proof (Mac, `TestAdvProbe_DesktopVerbFromAnotherSignIn`).** Copy in sign-in "100", `desktop`
launch from sign-in "200":

```
PROBE R50/desktop: err=<nil>; the instance (sign-in 100) opened its browser 1 times
  ["http://127.0.0.1:64142/_open?t=…"]; the launcher (sign-in 200) was told []
PROBE R50/blank session: a start whose sign-in is unknown showed the choice in sign-in 100
```

MA-1427's test covers only `start`, so it stays green. Windows is not affected (its lock and
file are per logon session).

**Fix, in plain words.** When the copy's file lists the actions, every launch (also `desktop`)
uses the second request form with its sign-in, including for "open", and the copy applies the
same check to "open": a launch from another sign-in gets "other-session", says R50's line in its
own session, and nothing opens on the other screen. When a sign-in cannot be told (""), do not
open or show anything on the copy's screen for that launch: hand the launch a link to show in its
own session instead.

### A4 (MEDIUM, all) — an open wizard inside the copy is not counted as work

**What happens.** `busyTask` (`stopwait.go:64`) names a wizard only while its export copies mail
(`wizard.ExportsRunning`). While the wizard is open at any other step, `quitQuestion` is "", so:
- Shut down (and the choice's Quit) stop at once with no question;
- the first choice's Cancel stops the copy when `idle()` is true (`launch.go:610`), and `idle`
  does not look at the wizard lock, so a wizard handed off while that first choice was still on
  the screen dies with it;
- sign-out and installers stop it as well (expected for sign-out, but not waited for).

On Windows the wizard's dialogs vanish with the process; on macOS (osascript) and Linux (zenity)
the dialog processes are left orphaned on screen, and the person's answers go nowhere. A stop
between the wizard's own writes after the export (job file, then schedule install) can leave half
a setup.

**Proof (Mac, `TestAdvProbe_OpenWizardIsNotWork`).** First choice held on screen, the `wizard`
shortcut's hand-off runs the wizard in the copy, then the person dismisses the first choice:

```
PROBE wizard: quitQuestion while the wizard is open = "" (empty means Shut down/Quit ask nothing)
PROBE wizard: the instance STOPPED on the first choice's Cancel while the wizard was still open
```

**Fix, in plain words.** While this process holds the wizard lock, `busyTask` says "the wizard is
open" (so Shut down and Quit ask), `idle()` is false (so the first choice's Cancel never stops the
copy), and the stop waits for the wizard's current write to end within the bound.

### A5 (LOW, Windows) — a window message stops the copy with no question

**What happens.** `wndProc` acts on `WM_ENDSESSION` with `wParam` TRUE from anyone: it runs the
stop that never asks. Any program on the same desktop at the same or higher integrity can send
it, including a program of another account started on this desktop with "Run as different user"
(window messages are not separated by account, only by integrity level). Design §10.1 says the
window procedure "acts only on its own callback message and `TaskbarCreated`", and MB-17 says
another process "cannot post Quit": this message is a Quit that skips the question.

**Proof (VM, `run.ps1` case 2 and `low.ps1`).**

```
sent WM_QUERYENDSESSION to 0x8c024c: ok=1 answer=1
sent WM_ENDSESSION(TRUE) to 0x8c024c: ok=1
case2 after a foreign WM_ENDSESSION: alive=False
```

A low-integrity sender (the probe marked `Low Mandatory Level`, like a browser sandbox) was
blocked by Windows (`ok=0`, the copy stayed alive), and `WM_COMMAND` was ignored as MB-17 says
(case 3: alive). So the reach is "normal programs on this desktop", which under R23 can mostly do
worse already; the finding is the gap between the design's words and the code, plus the
other-account-on-this-desktop case.

**Fix, in plain words.** Act on `WM_ENDSESSION` only when the session is really ending: Windows
reports that through `GetSystemMetrics(SM_SHUTTINGDOWN)` for sign-out and shut-down, and through
the `ENDSESSION_CLOSEAPP` flag for Restart Manager (that flag can be forged, so it only narrows
the gap). Otherwise ignore it. And correct §10.1's sentence.

### A6 (LOW, all) — a captured request works again

**What happens.** The copy checks the request's MAC but keeps no record of the nonces it has
already seen (`Handoff.ServeHTTP`, `handoff.go:192`). A request read off the wire can be sent
again: a `choose` shows the window again, and a "quiet" request mints a new one-time Desktop link
in the clear each time.

**Proof (Mac, `TestAdvProbe_ReplayIsAccepted`).** "one captured choose shown 3 times; one captured
quiet request minted 3 links". Reading loopback traffic needs an administrator (or, on a Mac with
Wireshark's ChmodBPF installed, membership of its `access_bpf` group), and such a reader can
already see the session cookies on the wire, so the gain is small. Design §10.1 asked for this
case.

**Fix, in plain words.** Remember the nonces seen in the last few minutes (a small, bounded set)
and refuse a repeat with 403.

### A7 (LOW, all; reopen on macOS) — a copy that is stopping still shows windows

**What happens.** After a stop begins, the drain can take up to 15 s. `front.reopen` and
`front.act` never look at whether the copy is stopping: a click on the app (macOS reopen) shows
the choice, and picking "Run the wizard" starts a wizard that the exit then cuts, or "Open
MailArchive Desktop" opens a link to a listener that is already closed. Separately, a launch
during the drain finds the lock held and the listener closed and waits silently. Its wait (15 s,
`instance.DefaultWait`) equals the drain budget (15 s, `DrainBudget`), so today it nearly always
outlasts the drain and then becomes the new copy; but the margin is zero. If a drain ever overruns,
the launch says "is not responding. Do this: quit it in Task Manager … or `kill`", the one advice
that cuts the work the drain is protecting.

**Proof (Mac, `TestAdvProbe_ReopenDuringDrain`, `TestAdvProbe_LaunchDuringDrain`).** "after Stop,
a reopen showed 1 choice"; "after Stop, a wizard act answered shown=true". With the test rig's
shorter 2 s launch window and a check that does not end: "after 2.214s the launch returned
MailArchive Desktop is already running for your account but is not responding. Do this: quit it in
Task Manager (pid 22353; or `kill 22353`)".

**Fix, in plain words.** Once the copy's life has ended, `reopen` and `act` show nothing and
answer "closing". Keep `/_handoff` answering "MailArchive is finishing its work and closing" until
the drain ends, and have the launch wait for the lock with that line instead of the "not
responding" advice.

### A8 (LOW, PLAUSIBLE, Windows) — sign-out waits 5 s, the drain needs up to 15 s

**What happens.** `WM_ENDSESSION` waits `endSessionBound` = 5 s, then returns, and Windows may end
the process at once. A cancellable Run now that needs longer to reach its safe point (a Graph
request in flight, the Thunderbird sync) is killed and left "running" (a crash record), not the
whole "cancelled" record that R52 promises for cancellable work. Also, `ShutdownBlockReasonCreate`
is called while the message is answered TRUE (allow); as far as is known here, a reason only shows
when the program blocks, so the words may never appear. Both need the L-W1 lab with a real
sign-out.

**Fix, in plain words.** Either say in R52 and MA-1424 that a Windows sign-out allows about 5 s and
can leave a cancellable run "running", or block the sign-out (answer FALSE with the reason) while
work runs, so Windows shows "MailArchive is finishing its work" and the person decides.

### A9 (LOW, Linux) — the activation token is never used

`activationToken()` sends `XDG_ACTIVATION_TOKEN` inside the signed request (design §7.4, MB-3
item 5), but `front.act` ignores `ActRequest.Token`; no dialog receives it (searched
`internal/desktop`, `internal/wizard`, `internal/dialogfront`). On Wayland the choice may then
not come to the front. Not an attack; a gap between the design and the code. Fix: pass it to the
zenity process's environment, or drop it from the request until slice S5.

## Refuted (the skeptic's record)

| # | Suspected attack | Why it fails |
|---|---|---|
| R1 | A squatter on the recorded port answers `choose` | It cannot sign its reply; a 403 makes the launch try the first form once, which also needs the key, so the launch opens nothing and ends with the existing refusal. Nothing opened, no link. |
| R2 | A forged or altered `act`, version, sign-in or token | All are inside the request MAC; an unknown action or an out-of-bounds field is 400 before the MAC (`handoff.go:206-214`). |
| R3 | A flood of `choose` | Unsigned requests get 403 before the once-a-second limit, so they cannot use it up; signed ones show one choice at a time (`choosing`), one wizard per account (wizard lock). |
| R4 | Another local account, a sandboxed app, or a web page talks to the channel | None can read K (0600 file in a 0700 folder, owner-checked; `%LOCALAPPDATA%` on Windows). A web page can send a cross-site POST but gets 403. No new listening socket. |
| R5 | Foreground right given to a pid named only in the instance file | Refused unless the lock's holder line names the same pid (`handoffact.go:26`); both files are writable only by this account (R23). |
| R6 | A posted `WM_COMMAND` "Quit" | Ignored (VM case 3: still alive). |
| R7 | A low-integrity program sends the end-of-session messages | Blocked by Windows (VM `low.ps1`: `ok=0`, still alive). |
| R8 | Older copy meets newer launcher, and the reverse | A launch sends the second form only for actions the copy's file lists; an older copy gets today's "open". |
| R9 | Two launches in the same second | The second gets a signed "busy" and counts it as done; a `choose` that showed nothing gives its second back (`handoff.go:252-260`). |
| R10 | Desktop link issuance | Every hand-off and every "Open MailArchive Desktop" mints a fresh one-time link; `choose`/`wizard` replies carry none; no link is logged. (Replay: A6.) |
| R11 | `getaudit_addr` read at the wrong offset (purego) | `auditinfo_addr` is 4 + 8 + 24 bytes before `ai_asid` (offset 36), 48 bytes in all; matches `guisession_darwin.go`. |
| R12 | A launch lies about its sign-in to get the window shown | The sign-in is inside the MAC; only a holder of K (this account) can set it. Same-account limit (R23). (Unknown sign-ins: A3.) |
| R13 | A forged, symlinked or stale instance file | Read refuses links, loose permissions and another owner; the holder removes a stale one before binding. Forgery needs this account. |
| R14 | Quit or a stop that hangs for ever | Every stop is bounded: the drain 15 s (all drains honour the deadline), server shutdown 5 s, macOS terminate reply at most 17 s, Windows end of session 5 s. The Quit question blocks only its own goroutine. |

## Notes for the lead

- Highest value first: A1 and A2 (Windows, sign-out and upgrade paths), then A4, then A3.
- A3 and A4 each need a catalog change of their covering rows (MA-1427 covers only `start`;
  MA-1424 and MA-1422 do not name the open wizard). A2 needs MA-614's text amended.
- No probe code is committed. The probes are kept untracked in the worktree's
  `.claude/advprobe/` (the two Go test files, saved as `*_test.go.txt` so no build or meta-test
  sees them; copy one into its package as `zz_advprobe_s1_test.go` and run with
  `-tags advprobe` to repeat it; and `wmprobe/`, the Windows message sender) and on the lab share
  under `.winlab/advs1/` (`run.ps1`, `low.ps1`, `leftover.ps1`).
