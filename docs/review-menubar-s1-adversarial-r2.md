# Adversarial re-check: menu-bar slice S1, the fix round

Date: 2026-11-13. Reviewed: branch `menubar` at **c990a79**; the fix round is
`git diff 4528ebe..c990a79` (4528ebe merged the two reviews onto c1df91b). Read in the
worktree `.claude/worktrees/menubar-s1-adv2` (branch `wip/menubar-s1-adv2`). First pass:
`docs/review-menubar-s1-adversarial.md` (A1–A9). Fix notes: `docs/design-menubar.md`, the
"S1 fix round" build note. Procedure: `assurance-kit/process/adversarial-review.md` (finder
lenses outside / insider / concurrency, then a skeptic step that keeps only what was
reproduced or shown in the source). One agent played both roles in sequence. This pass
attacks and reports; it changes no product code.

**Verdict: NOT CLEAN.** Of the first pass's findings, A1, A3, A4, A6, A7 and A9 are closed for
the attacks they named; A2 is closed for the plain case but its edges are open (B1, B2); A5
is narrowed, not closed (B3); A8 stays open (lab). New: **0 critical, 0 high, 3 medium,
7 low**. 11 suspected attacks refuted (listed at the end). Nothing found lets another account,
a web page or a sandboxed program use the hand-off channel.

Header facts:
- Toolchains: Mac probes and the Windows cross-build with the repo's pinned go1.26.0
  (`go env GOTOOLCHAIN` = go1.26.0; no 1.26.11 toolchain is installed on this Mac, so the
  lead's "1.26.11" was not used); Linux probes in Docker `golang:1.26` = go1.26.9
  linux/arm64 with `GOTOOLCHAIN=local`.
- Windows: the lab VM "Windows 11" (ARM64, amd64 emulated), user `alex.dimarco`, session 3.
  User-side scripts through `prlctl exec --current-user`; the upgrade script's close step as
  `nt authority\system` (session 0) through plain `prlctl exec`. Own program name
  `maadv2w.exe` (built from c990a79 with `-H=windowsgui`), own folders `%TEMP%\adv2-*` with
  their own `LOCALAPPDATA`, `APPDATA`, `USERPROFILE`, `-config`, `-addr 127.0.0.1:0`. Only
  processes these scripts started were messaged or ended (each script checks the target's
  path is `\adv2-N\maadv2w.exe`); the real `Install-RMM.ps1` was never run whole (it would
  touch every `mailarchivew.exe` on the VM and run msiexec): its `$closeHelper` and
  `Close-InItsSession` were cut out of the file and run against one lab pid. A final check
  found no lab copy and no task named `MailArchive upgrade close *` left on the VM.
- Mac: only the `internal/instance` probe ran here (no dialog, no browser). Every probe that
  could reach a dialog seam ran in Docker.
- Blob ids at c990a79: `Install-RMM.ps1` 29ebaf0, `apploop_windows.go` cbfa8af, `front.go`
  5395dba, `launch.go` f3e7c7e, `handoff.go` a00fa4a, `guisession_other.go` 9ec0391,
  `ininstance.go` 3c9ed34.
- Probes (not committed): `.claude/adv2/` in the worktree (`instance_zz_adv2probe_test.go.txt`,
  `desktop_zz_adv2probe_test.go.txt`, build tag `adv2probe`; copy one into its package as
  `zz_adv2probe_test.go` to rerun; `wmprobe/`, the window-message sender) and the lab share
  `.winlab/adv2/` (`p1.ps1` window messages; `p2-user-start.ps1`, `p2-system.ps1`,
  `p2-sd.ps1`, `p2-collide-*.ps1` the upgrade close; `p3-clm.ps1`; `p4-low.ps1`;
  `p2-user-clean.ps1`).
- Count: **21 suspected / 10 confirmed / 11 refuted**, plus the 9 re-checks. Of the 10, B8
  is PLAUSIBLE as a whole; B1's real-world triggers and B5's cross-account case are PLAUSIBLE
  on top of a proven mechanism.

## The first pass's findings, re-checked

| ID | Verdict | Check |
|---|---|---|
| A1 | **closed** | VM `p1.ps1`: an idle copy and `taskkill` without /F → it stopped in 158 ms; a copy with its wizard open and a WM_CLOSE to its instance window → alive, instance window still there (count 1), and a later foreign WM_ENDSESSION still reached it (and was ignored, A5). The private loop-end message from outside is ignored (W5: alive). New door this fix opened: B3. |
| A2 | **closed for the plain case; edges open (B1, B2, B5–B7)** | VM, the real `Close-InItsSession` as SYSTEM: idle copy → returned 0 after 2 s, copy gone, task removed; copy with its wizard open → returned 1 after 35 s, copy alive, task removed. MA-614's text is amended. |
| A3 | **closed for today's launchers; residuals B8, B9** | Docker `TestAdv2Probe_A3DesktopVerb`: `desktop` from sign-in 200 to a copy in 100 → instance browser opens 0, launcher told the R50 line; from an unknown sign-in → opens 0, told the "cannot tell" line. |
| A4 | **closed** | Docker `TestAdv2Probe_A4OpenWizard`: with the wizard open the Quit question is "The wizard is open. Quit anyway? …"; the first choice's Cancel left the copy running; a stop with the wizard still waiting for an answer ended the copy after 5 ms (the wizard does not hold a stop). Residual already in the build note: orphaned dialogs on macOS/Linux. See B4 for `taskkill`. |
| A5 | **narrowed, open** | VM W2: a foreign WM_ENDSESSION(TRUE, 0) while the session is not ending → ignored (alive). But the same reach still stops a busy copy two other ways: B3. |
| A6 | **closed** | Mac `TestAdv2Probe_Replay`: a captured `choose` → 200 then 403, shown once; a v2 open with `quiet` flipped → 403; sent to a restarted copy (new K) → 403. A capture works again only after 1024 later *proven* requests (each needs K), so the bound does not help an outsider. No clock enters the record; the rate limit uses Go's monotonic clock. |
| A7 | **closed** | Docker `TestAdv2Probe_A7*`: the instance file reads `closing=true` 0.5 s after Stop; a `start` during a drain held by a job that never ends became the next copy after 14.6 s, with no "not responding" advice; `reopen` after Stop showed nothing; a `wizard` act answered "closing". |
| A8 | **open (lab L-W1)** | Not attempted: a real sign-out would end other jobs' sessions on the shared VM. MA-1424 and R52 name it. |
| A9 | **closed, with B10** | Docker: the token is in the environment while the choice shows and removed after. |

## New findings

| ID | Sev | Platform | Claim | Check |
|---|---|---|---|---|
| B1 | MED | Windows | The upgrade script reads "no instance window found or could not post" (2) as "a copy from before S1" and force-kills it; an S1 copy, busy, is forced whenever the helper cannot reach its window | PROVEN (mechanism, VM); triggers PLAUSIBLE |
| B2 | MED | Windows | Any failure of the helper is reported as "busy" and exits 1618 for ever: under PowerShell's Constrained Language mode (AppLocker/WDAC) an idle copy is never asked, the upgrade never runs, and the stated reason is false | PROVEN (VM) |
| B8 | MED | Linux | The sign-in fallback's identity is unproven and can fail both ways: one id for every sign-in of the account (R50 not kept) or none (every second click says "cannot tell") | PLAUSIBLE (needs MA-1439) |
| B3 | LOW | Windows | Two messages any program on the same desktop can send still stop a busy copy with no question: a posted WM_QUIT (new: the A1 fix made a lost loop a stop that ignores Busy) and a lone WM_ENDSESSION(TRUE, ENDSESSION_CLOSEAPP) | PROVEN (VM) |
| B4 | LOW | Windows | `taskkill` without /F still ends an open wizard (its dialog takes the close as Cancel) and then the copy | PROVEN (VM) |
| B5 | LOW | Windows | The task name is predictable in a folder every user can write; `-Force` updates a task someone made first and keeps their full access to it | PROVEN (same account, VM); cross-account PLAUSIBLE |
| B6 | LOW | Windows | A script ended mid-wait (RMM timeout) leaves its task behind, still running | PROVEN (VM) |
| B7 | LOW | Windows | Several signed-in users: idle copies are closed even when a later copy refuses and the upgrade is not run — again on every retry | PROVEN in the source |
| B9 | LOW | macOS, Linux | A first-form open is not gated by sign-in, and a 403 to the second form falls back to it: an older launcher from another sign-in still opens the browser on the copy's screen | PROVEN (Mac + Docker) |
| B10 | LOW | Linux | The activation token stays in the whole process's environment for as long as the choice shows, so everything the copy starts meanwhile inherits it | PROVEN (Docker) |

### B1 (MEDIUM, Windows) — "no window to ask" is not proof of an old copy

**What happens.** The helper's `Ask` returns `found != IntPtr.Zero && PostMessage(found,
WM_CLOSE, …)`, and `exit 2` on false. The script then runs `elseif ($closed -eq 2) {
Stop-Process -Id $proc.ProcessId -Force }`: TerminateProcess, no drain, no interrupted
record. Code 2 means "the helper, in the session and at the integrity the task gave it, did
not find the window or could not post to it", which is not the same as "a copy from before
S1". An S1 copy, busy, is forced when:
- it runs at a higher integrity than the task (the task runs with the user's filtered token:
  `RunLevel` is left at Limited): a copy started "as administrator" or from an elevated
  console. UIPI then blocks the post;
- its user is signed in twice (an RDS host with more than one session per user): the
  interactive task runs in one of those sessions, and `EnumWindows` sees only that session's
  desktop;
- its event loop already ended (a posted WM_QUIT, B3): the loop's thread exits locked, so Go
  ends the thread and Windows destroys its windows, while the copy is still draining.

**Proof (VM `p4-low.ps1`, the mechanism).** A sender at a lower integrity than the copy (the
copy had its wizard open, so it was busy):

```
lower-integrity sender: instance windows found=1
lower-integrity sender: posted WM_CLOSE to 0x7d0340: 0
```

`found=1` and a post of 0 is exactly `Ask` → false → `exit 2` → forced. The triggers above
were not staged on the shared VM (no second account, no elevation prompt).

**Fix, in plain words.** Decide "before S1" from the program, not from a window: the helper
runs as the user and can read the user's instance file (the S1 file lists `Acts`), or the
script can compare the file version of `$proc.ExecutablePath` with the S1 release. Give "found
but could not post" its own code, and never force on it: report it like a refusal.

### B2 (MEDIUM, Windows) — a helper that cannot work is called "busy", for ever

**What happens.** `Close-InItsSession` returns 3 when the task cannot be registered or run,
and the helper returns 1 when it waited 30 s without the copy ending. The script lumps 1 and 3
into `$busy` and prints "MailArchive is busy … it is running a backup or another task, or its
wizard is open", exit 1618. When the helper itself cannot work, that is false and never
changes, so the RMM tool retries for ever and the upgrade never runs:
- **Constrained Language mode** (AppLocker or WDAC script rules, common on managed fleets,
  put `-EncodedCommand` in it): `Add-Type` is refused, the `if (-not [MailArchiveClose]::Ask…)`
  statement fails and is skipped under `SilentlyContinue`, the loop waits 30 s and exits 1.
  An idle copy is never asked.
- Code 3, PLAUSIBLE: an owner name Task Scheduler cannot resolve as `Domain\User` (Entra ID
  accounts report `AzureAD\…`), or an endpoint product that blocks a hidden
  `powershell -EncodedCommand` started by a task (a pattern many of them flag).

**Proof (VM `p3-clm.ps1`, the helper cut from the real script, idle lab copy).**

```
helper (ConstrainedLanguage) exit=1 after 32 s; copy alive=True
helper (FullLanguage) exit=0 after 1 s; copy alive=False      (the twin, same copy)
```

**Fix, in plain words.** Do the close with the program itself, not with compiled-on-the-fly C#:
a small verb of the signed `mailarchivew.exe` (run by the task as the user) that asks the copy
to close and says how it answered; an allow-listed program passes AppLocker. Keep three
answers apart in what the script prints and returns: "busy" (the copy said no), "could not
ask" (with the reason), "closed". Consider an explicit admin override for a copy that stays
busy for days (for example a wizard left open), stated in README-RMM.

### B8 (MEDIUM, PLAUSIBLE, Linux) — the fallback sign-in may be one for all, or none

**What happens.** `GUISession` prefers `XDG_SESSION_ID`, else `/proc/self/sessionid`. On
desktops that run the shell under the systemd user manager (GNOME on Wayland and others), apps
the shell starts are understood not to carry `XDG_SESSION_ID`, and their audit session is the
user manager's own: one per account, shared by every sign-in of that account (a local one and
an xrdp one alike). Then:
- if that audit id is set, the copy and a launch from the other sign-in compare equal and the
  choice or the browser opens on the copy's screen (R50 not kept);
- if it is unset (4294967295, as in this Docker container), every launch, the person's own
  second click included, gets "it cannot tell whether that is on this screen" instead of the
  choice.

**Check.** Docker: `/proc/self/sessionid` = `4294967295`; the A3 probe shows the second path's
words. Which path a real GNOME session takes is not proven here; the build note already names
the lab row (MA-1439, pending). Raised to MED because either outcome is the common Linux case,
not an edge.

**Fix, in plain words.** Settle MA-1439 before release. Where a desktop gives no per-sign-in
id, a robust one is logind's own answer for the process (`sd_pid_get_session` /
`GetSessionByPID` over D-Bus, which the Linux build already links), or the display the launch
belongs to (`WAYLAND_DISPLAY`/`DISPLAY` plus `XDG_RUNTIME_DIR`), not the audit session.

### B3 (LOW, Windows) — two messages still stop a busy copy with no question

**What happens.**
1. **WM_QUIT.** `windowsLoop` now treats a loop that ends before the serving as a stop:
   `if !serveEnded.Load() && ev.Close != nil { ev.Close() }`. Unlike the WM_CLOSE path it does
   not look at `Busy`. Any program on the same desktop at the same or higher integrity can
   `PostMessage(hwnd, WM_QUIT)`. With a Run now the backup is cancelled.
2. **WM_ENDSESSION(TRUE, ENDSESSION_CLOSEAPP) alone.** The fix note says this "narrows, not
   closes" the gap. But the builder's own VM finding is that Restart Manager cannot ask this
   copy at all (an "unknown app"; RmShutdown without force returns 350), and Restart Manager
   never sends WM_ENDSESSION(TRUE) after a WM_QUERYENDSESSION was refused. So the
   CLOSEAPP branch is reached only by a sender that skips the query: it serves a forger and
   nothing else.

**Proof (VM `p1.ps1`, copy with its wizard open, window class `WC_…` "MailArchive wizard" in
the copy's pid).**

```
sent WM_QUERYENDSESSION(ENDSESSION_CLOSEAPP) to 0xe80502: ok=1 answer=0
sent WM_ENDSESSION(TRUE, ENDSESSION_CLOSEAPP) alone to 0xe80502: ok=1
W2 after a forged WM_ENDSESSION(TRUE, ENDSESSION_CLOSEAPP) alone, while the wizard is open: alive=False
posted WM_QUIT to 0x1e01f0: 1
W3 after a posted WM_QUIT while the wizard is open: alive=False
```

**Fix, in plain words.** On a lost loop, build a new hidden window and loop and keep serving
(stop only if that fails, and then only when idle). Ignore WM_ENDSESSION with CLOSEAPP while
`Busy` (it matches the refused query), or drop the CLOSEAPP branch, since nothing genuine
sends it to this copy.

### B4 (LOW, Windows) — `taskkill` still closes an open wizard, then the copy

**What happens.** `taskkill` without /F posts WM_CLOSE to every top-level window of the
process. The instance window refuses while the wizard is open, but the wizard's own dialog
takes it as Cancel; the wizard ends; nothing runs and no Desktop page was opened, so
`endIfIdle` ends the copy (F2). The build note says the dialogs "close as a person's Cancel
would", but MA-1435's wording and the note's "one that runs … an open wizard refuses it"
read as if the wizard survives. PLAUSIBLE extension: the wizard's export progress window takes
the same close as Cancel, so "the wizard is copying mail" is cut by `taskkill`.

**Proof (VM `p1.ps1` W4).** Copy with its wizard open, `taskkill /PID` (no /F): `SUCCESS …`,
then `alive=False`.

**Fix, in plain words.** Say it in the words (MA-1435 and the build note): `taskkill` without
/F closes an open wizard like Cancel, then an idle copy ends. Or only end as idle after a
wizard the *person* cancelled (not a close from outside), if the dialog library can tell.

### B5 (LOW; cross-account PLAUSIBLE, Windows) — a task someone made first keeps their rights

**What happens.** The task is `\MailArchive upgrade close <pid>`, in the root folder, whose
DACL lets every authenticated user create tasks (`(A;CI;FW;;;AU)`); pids are visible to every
user. `Register-ScheduledTask -Force` *updates* an existing task of that name and keeps its
security descriptor, so whoever made it first keeps Full Access to the task SYSTEM then fills
with the copy owner's principal and starts.

**Proof (VM `p2-collide-*.ps1`).** The user pre-created the name for their lab copy's pid
(action `cmd /c exit 7`). During SYSTEM's run of the real `Close-InItsSession`:

```
before: DACL …(A;ID;FA;;;S-1-5-21-…-1000)… action cmd.exe
during: DACL …(A;ID;FA;;;S-1-5-21-…-1000)… action powershell.exe principal alex.dimarco
```

(A task SYSTEM makes fresh gives that user only `FR`: `p2-sd.ps1`.) On the VM the creator and
the owner are the same account, so nothing is gained. With two accounts, user A can at least
delete or disable user B's task in the window (B's upgrade then reports "busy", B2); whether A
can change what runs as B is not tested (registering a definition for another account needs
that account's credentials or an administrator, so it is expected to fail).

**Fix, in plain words.** Make the task under a folder the script creates with an
administrators-and-SYSTEM-only descriptor (for example `\MailArchive\`), with a random name;
remove any task of that name first; after registering, check the principal, the action and
the descriptor before starting it.

### B6 (LOW, Windows) — a script ended mid-wait leaves its task behind

**What happens.** The `finally` that unregisters the task does not run when the script's
process is ended, which is how an RMM tool enforces its timeout.

**Proof (VM `p2-system.ps1 -KillAfter 12`).** `LEFT BEHIND: \MailArchive upgrade close 3888
state=Running principal=alex.dimarco logon=Interactive` (removed afterwards by `p2-sd.ps1`).
No trigger, so it does not run again; the helper inside it still ran its 30 s.

**Fix, in plain words.** At the start of the script, remove any task the script made earlier
(its folder, B5); set an execution time limit and a delete-after-expiry on the task itself.

### B7 (LOW, Windows) — several users: idle copies closed for an upgrade that does not run

**What happens.** The `foreach` closes each copy as it comes and only at the end decides
whether to upgrade. When user C's copy refuses, users A and B have already had their idle
copies closed, the script exits 1618, and nothing tells them. Every retry repeats it.

**Check.** Source: `Install-RMM.ps1` lines 91–109. A non-destructive question already exists:
the copy answers WM_QUERYENDSESSION with ENDSESSION_CLOSEAPP with 0 while busy (VM W2:
`answer=0`) and 1 while idle, without stopping.

**Fix, in plain words.** First ask every copy whether it would close (that query, from the
same per-session task); close them only when all say yes; otherwise close none and exit 1618.

### B9 (LOW, macOS and Linux) — the first form still opens on any screen

**What happens.** `front.gate` returns "" for a first-form request (`Act == ""`): it carries no
sign-in, so only "closing" applies. A non-quiet first-form open makes the copy open its own
browser, whatever screen the sender is at. Today's launcher sends the first form only to an
older copy, or after a 403 to the second form (`launch.go`: `case errors.Is(cerr,
instance.ErrRefused): rep, cerr = instance.Reach(…)`), but an older launcher (a second,
older install, or a copied binary) still sends it.

**Proof.** Mac `TestAdv2Probe_FirstFormIsNotGated` (the gate's rule for Act "") and Docker
`TestAdv2Probe_A3DesktopVerb` end to end: `PROBE A3 first form (no sign-in): opened=true
err=<nil>; instance browser opens now=1`.

**Fix, in plain words.** Where sign-ins are not separate (macOS, Linux), answer a non-quiet
first-form open as if it were quiet: hand back a link, which every launcher shows in its own
session.

### B10 (LOW, Linux) — the activation token lingers

**What happens.** `front.choose` sets `XDG_ACTIVATION_TOKEN` in the process environment and
unsets it in a `defer` that runs when the dialog *returns*, so it stays set while the choice is
on the screen. Anything the copy starts meanwhile inherits a one-time token meant for the
choice: the wizard's dialogs (from a hand-off), `xdg-open` for Desktop, Thunderbird for a Run
now's sync. The build note says "set … while it starts, then removed".

**Proof (Docker `TestAdv2Probe_A9TokenLingers`).** `300 ms into the choice, the process
environment has XDG_ACTIVATION_TOKEN="adv2-token"`; after the choice, `""`.

**Fix, in plain words.** Give the token to the dialog process only (its own environment), or
unset it as soon as that process has started.

## Refuted (the skeptic's record)

| # | Suspected attack | Why it fails |
|---|---|---|
| R1 | A local user hijacks what the SYSTEM-made task runs (PATH, a writable folder, the command line) | The task runs as the copy's own user with a filtered token: a hijack runs code as that user, which they can already do. The SYSTEM side reads back only an integer (`LastTaskResult`) and the pid is cast to `[int]` before it enters the encoded command. A task SYSTEM makes fresh gives the user only `FR` (VM `p2-sd.ps1`). (Pre-created name: B5.) |
| R2 | The copy's command line (user-controlled) injects into the script | Used only in `-match` tests; never executed or interpolated into the task. |
| R3 | The 30 s helper wait or the 60 s outer bound cut an idle copy's stop | Idle copy closed in 2 s (SYSTEM run) and 1 s (helper alone); a busy refusal took 35 s, within the 60 s bound. |
| R4 | A copy that is busy for ever goes unreported | Each run prints the busy pid and exits 1618 before msiexec; nothing is forced (VM: busy copy alive after the run). The open questions are the false "busy" (B2) and the missing admin override (in B2's fix). |
| R5 | A copy owned by another account is asked as the wrong user | `GetOwner` names the process's own account and the task runs as it, interactive, in its session; a pid outside that session is not reachable, and that case is B1's "two sessions" trigger, not a wrong-user one. |
| R6 | A wizard waiting for an answer blocks sign-out or shut-down for ever | Windows: WM_QUERYENDSESSION without CLOSEAPP is answered 1 while the wizard is open (VM W2), then a 5 s bound; macOS: terminate asks nothing and is bounded (`terminateBound`); every stop: the drain waits for the wizard's writes, not its questions (Docker: the copy ended 5 ms after Stop with the wizard waiting). |
| R7 | The replay record is evicted or reset by an outsider, or beaten with the clock | Only proven requests enter it; a replay is not re-recorded; a restart brings a new K (403); no wall clock is involved (Mac probe). A nonce's hex case cannot be changed without breaking its MAC. |
| R8 | `quiet` flipped on a captured request | v2: inside the MAC (403). v1: not covered, but reading or altering loopback traffic needs an administrator, and the replay gate refuses the copy of a request already used. |
| R9 | The private loop-end message (WM_APP+0x4D) from outside takes the window down | Honoured only after the serving ended (VM W5: alive, window kept). |
| R10 | A forged "closing" mark in the instance file keeps launches waiting | Writing that file needs this account (R23); the wait is bounded (25 s) and then the launch says so. |
| R11 | MarkClosing races Close and leaves a stale file | Both hold the holder's lock and `closed` is checked; Watch skips a closed holder. A copy killed during its drain leaves the lock free, and the next launch takes it and removes the stale file. |

## Notes for the lead

- Highest value first: B2 and B1 (the upgrade path: one blocks fleets that lock down
  PowerShell, the other can still force a busy copy), then B8 (settle MA-1439 before the
  Linux release), then B3.
- A note on PLAUSIBLE, not raised as a finding: during a graceful upgrade close, a launch that
  was waiting on the closing copy (A7) becomes the next copy, from the old files, just before
  msiexec runs; the MSI then meets files in use and asks for a restart (3010). The window is
  the drain (about 20 s).
- Catalog effects: B1/B2/B5–B7 touch MA-614 (the script's promises); B3 and B4 touch
  MA-1435's wording; B9 touches MA-1427 (first form); B8 is MA-1439.
- No probe code is committed. Toolchain caveat: the lead's 1.26.11 was not available on this
  Mac; the VM binary was built with the repo's pinned go1.26.0.
