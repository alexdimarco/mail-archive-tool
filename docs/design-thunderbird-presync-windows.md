# Thunderbird pre-sync — Windows adapter (slice TP6), v1 design

Status: **DRAFT revision 2 — the pre-code review (PC21) is folded; owes owner
sign-off and the lab.** The 12-lens pre-code review (10 lenses + the user32/
WM_CLOSE adversarial addendum) raised 4 critical and 30 high concerns against
revision 1. **Revision 1's central claim was wrong** and is retracted here:
rev-1 said the pure core is "reused unchanged" and that on Windows identity is
"the CreateProcess handle, no scan, no command-line match at runtime." The built
core refutes that — `candidateIsOurs` (`ownership.go`) and `matchRecord`
(`session.go`) BOTH require `argsHaveProfile`, i.e. the process's `-profile`
command line, at S6 and S8 and in recovery. With no command line, Windows would
match nothing and the feature would silently fail every run. Rev-1 also rejected
reading the command line on a false premise (it assumed `ReadProcessMemory`/PEB,
`PROCESS_VM_READ`). This revision corrects all of that. Nothing is built until
this is signed off and then only behind the Windows lab, with L-W3/L-W4/L-W6 as
viability gates (as L-M8/L-M9 gated macOS).

## 0. Honest scope (correcting rev-1's "adds exactly one thing")

TP6 is **not** one `env` file. It is:

1. `internal/presync/env_windows.go` — the Windows `env` implementation.
2. `internal/presync/resolve_windows.go` — S1 resolution/refusals.
3. `internal/presync/run_windows.go` — only if the watchdog is not first
   factored out (see §9 / PC-W9).
4. `cmd/mailarchive/presync_windows.go` — the CLI wiring (hook, state dir,
   install note), mirroring `presync_darwin.go`.
5. **Shared core/seam changes submitted to THIS review**, because they are not
   inside `env_windows.go`: the `quit` seam gains the proven target (§4, PC-W2);
   the whole-session watchdog is factored build-tag-free (§9, PC-W9); the MA-557
   hygiene walk gains the Windows kill tokens (§7, PC-W7). R31/R32 are re-asserted
   on the macOS path where the seam changes touch it.

Each file carries its `// covers:` markers; the quit and the command-line read
owe U-tier tests, not only the pending lab row MA-574.

## 1. What is genuinely reused unchanged

The platform-neutral core is reused and its invariants re-derived for Windows
below, NOT assumed: the S1–S9 state machine, the quiet detector and bounded watch
digest, the write-ahead record, the recovery table, **the ownership predicate
`candidateIsOurs`/`matchRecord` — which keeps working on Windows ONLY because §3
now reads the `-profile` command line** — the outcomes, the `BeforeRead` hook and
its recover guard (R32), `ctx` cancellation (it lives in `session.go`, so it
ports for free), the schedule baking/descriptor/narration. The watchdog lives in
`run_darwin.go` today and must be factored out first (§9).

## 2. The `env` seam mapped to Windows (pure Go, no cgo)

Win32 is reached through `golang.org/x/sys/windows`. System DLLs are loaded by
**absolute System32 path** (or via `windows.NewLazySystemDLL`, which forces the
System32 directory), never a bare `NewLazyDLL("user32.dll")` that could load a
planted DLL from the application or current directory (PC-W8b). Claim labels
follow the parent design (`lab-pending` + an L-W id for anything not pure-Go
stdlib or verified in-repo); the per-row "doc/memory" of rev-1 is dropped.

| `env` method | Windows mechanism | Status | Lab |
|---|---|---|---|
| `now`, `sleep` | `time` | proven | — |
| `bootTime` | an ABSOLUTE boot instant via `NtQuerySystemInformation(SystemTimeOfDayInformation).BootTime` (adjusted on clock steps), NOT `Now − GetTickCount64` — the recovery boot guard compares by exact equality, so a wall-clock/NTP step must not look like a reboot (PC-W4) | lab-pending | L-W2 |
| `idleSeconds` | `GetLastInputInfo` → seconds; unreadable = active | lab-pending | L-W5 |
| `thunderbirdProcs` | `CreateToolhelp32Snapshot`+`Process32FirstW/NextW` (this user's `thunderbird.exe`); per pid `QueryFullProcessImageNameW` (exe), `GetProcessTimes` (creation FILETIME → start UTC), **and the command line via `NtQueryInformationProcess(ProcessCommandLineInformation)` → `proc.args`, so `argsHaveProfile` works (§3, PC-W1, PC-W11)** | lab-pending | L-W2 |
| `lockPID` | the `parent.lock` held-open probe (`windowsLockHeld`); reports present/held with no pid, which `detectRunning` treats as running | repo | L-W2 |
| `pidAlive` | `WaitForSingleObject(h,0)==WAIT_TIMEOUT` on a held handle (NOT `GetExitCodeProcess`, whose 259/`STILL_ACTIVE` is ambiguous; NOT a fresh `OpenProcess`-by-pid) | lab-pending | L-W2 |
| `procExe` | `QueryFullProcessImageNameW` | lab-pending | L-W2 |
| `startHidden` | `exec.Cmd{…, SysProcAttr:{HideWindow:true, CreationFlags: CREATE_NO_WINDOW | CREATE_BREAKAWAY_FROM_JOB}}` — breakaway so a Task-Scheduler job-object teardown cannot hard-kill Thunderbird (§6, PC-W6). HideWindow only hints the first window | lab-pending | L-W3, L-W6 |
| `quit(target)` | the proven child's pid+held handle: `EnumWindows`+`GetWindowThreadProcessId` to the child's MAIN top-level window, `PostMessageW(WM_CLOSE)` with the last-instant re-checks of §4 | lab-pending | L-W4 |
| `digest` | `digestWatch` (reused) | proven | — |
| `acquireLock`, `releaseLock` | `internal/lockfile` (LockFileEx on Windows) | repo | — |
| `recheckProfile` | `filepath.EvalSymlinks` — its behaviour on NTFS junctions/reparse points (a §5.2 TOCTOU guard) is not assumed | lab-pending | L-W(new) |

Keep-awake: `SetThreadExecutionState(ES_CONTINUOUS|ES_SYSTEM_REQUIRED)`.

## 3. Identity (PC-W1) — read the command line, keep the core

Windows `thunderbirdProcs` reads each candidate's `-profile` argument via
`NtQueryInformationProcess(ProcessCommandLineInformation)`, which needs only
`PROCESS_QUERY_LIMITED_INFORMATION` — far lighter than the PEB/`ReadProcessMemory`
(`PROCESS_VM_READ`) path rev-1 wrongly weighed and rejected, and it works on a
process we did not create. So `proc.args` carries the command line, and the
reused `candidateIsOurs`/`matchRecord` (the `-profile` match, PC6) work on Windows
**identically to macOS** — identity is as strong, not "stronger," and there is NO
handle-only identity model. This also makes identity robust to a launcher
re-exec (L-W3): if `thunderbird.exe` re-execs to a new pid, the real mail process
is found by the same scan+`-profile` match macOS uses, not by a dead launcher
handle. PC-W11: this cross-process command-line read is a new surface — bounded
to this user's `thunderbird.exe`, `PROCESS_QUERY_LIMITED_INFORMATION` only, a size
cap, unreadable ⇒ not-ours — and is attacked in the TP6 adversarial addendum.

## 4. The quit, R31, and pid reuse (PC-W2, PC-W3) — the crux

`env.quit()` is parameterless on macOS (an Apple event to the bundle id). Windows
`WM_CLOSE` is pid-addressed, so the seam changes to **`quit(target)`** carrying
the session's proven child (its pid); macOS ignores it and still quits by bundle
id (R31/R32 re-asserted on that path). The Windows quit then:

1. holds an `OpenProcess` handle to the proven pid open across the whole quit —
   Windows will not recycle a pid while a handle to it is open, which defeats the
   pid-reuse race rev-1 left open;
2. selects the child's **single MAIN top-level window** (a visible
   `WS_OVERLAPPEDWINDOW` owned by the pid with no owner window; not message-only,
   not tool windows) — not every top-level window — the nearest analogue of the
   macOS single app-quit;
3. immediately before the `PostMessageW(WM_CLOSE)` re-checks that the held handle
   still reports alive (`WaitForSingleObject(h,0)`) and `GetWindowThreadProcessId(hwnd)`
   still equals the pid (the Windows analogue of S8's pre-quit re-verify);
4. states the irreducible residual — an HWND destroyed and its value recycled
   between enumerate and post — explicitly, exactly as macOS states PC1.

R31 argument: `WM_CLOSE` is the window's own close request (the X button's
message), handled by Thunderbird's shutdown path; it is NOT `TerminateProcess`,
`WM_QUIT`, `EndTask` or a signal (all forbidden, §7). On a HIDDEN launch an
unsaved-draft or password modal is invisible and blocks the close → the honest
outcome is `presync_quit_timeout`, left-running, announced — a hang path, not a
safety prompt. **Viability L-W4** must prove `WM_CLOSE` to that one window closes
a hidden Thunderbird cleanly (flush, lock removed, next start with no
crash-recovery/compaction prompt, no surviving tray process). If it cannot, the
scheduled form does not ship (the macOS Q16 rule).

## 5. Resolution and refusals (§5.2) (PC-W8)

- **App:** resolve Program Files from `SHGetKnownFolderPath(FOLDERID_ProgramFiles
  / FOLDERID_ProgramFilesX86)`, NEVER the attacker-influenceable `%ProgramFiles%`
  environment variable; accept only a regular file named `thunderbird.exe`. There
  is no portable Go per-file writability/ACL test on Windows (the parent skips
  profile ownership on Windows for the same reason), so v1 does NOT perform a
  "not user-writable" check — it relies on the machine-scope, admin-owned
  known-folder location under the same-user-out-of-scope threat model (DC4). This
  is a genuine authenticity REDUCTION versus macOS (which keeps the bundle-id
  check): v1 verifies LOCATION only, no file content/publisher. Q-W3 reopens
  whether an Authenticode publisher check (chains to Mozilla) is wanted — it, not
  "no check," is the true macOS-bundle-id analogue. A per-user install (under
  `%LOCALAPPDATA%`, user-writable) is either accepted via an added per-user
  candidate (same-user already out of scope) or refused machine-wide-only with a
  legible message (the §4.2.4 non-standard-install analogue). `-thunderbird-app`
  stays deferred (PC22).
- **Profile:** `EvalSymlinks`, contains `prefs.js`; ownership/mode NOT checked on
  Windows (no portable API, stated). `profiles.ini` membership (PC3) reused,
  reading `%APPDATA%\Thunderbird\profiles.ini`.
- **Elevation:** refuse an elevated token — `OpenProcessToken` +
  `GetTokenInformation(TokenElevation)`; `presync_refused_elevated`, zero helpers.

## 6. Job-object teardown (PC-W6) — an R31 breach our code never causes

A `CreateProcess` child sits inside the Task-Scheduler task's job object, so a
task End (timeout, logoff, shutdown inside the ~25-minute window) makes Windows
`TerminateProcess` the whole tree — hard-killing Thunderbird mid-write, violating
R31 even though our code calls no kill (so MA-557 passing is necessary, not
sufficient). macOS avoids this by launching detached via `open`. Windows launches
with `CREATE_BREAKAWAY_FROM_JOB` (§2) so the child leaves the task's job object;
this forfeits the direct child handle, which is consistent with §3's scan+match
identity (we re-find the real process by `-profile`, not by a launch handle). The
graceful cancel path (S8c) has no Windows trigger for a console-less process, so
the design does NOT claim `ctx`-cancellation parity on Windows for a task stop;
it relies on breakaway (so the leftover survives to be announced, P5) + the
watchdog. **Viability L-W6**: confirm on a real task whether breakaway holds and
the child survives a task End/logoff/shutdown.

## 7. MA-557 on Windows (PC-W7)

The hygiene walk's token list does NOT currently forbid the Windows kill
primitive, and the repo already calls `windows.TerminateProcess` elsewhere, so a
TP6 author could kill and the test would stay green. Before TP6 the list gains:
`TerminateProcess`, `OpenProcess`+`PROCESS_TERMINATE`, `WM_QUIT`, `WM_DESTROY`,
`WM_ENDSESSION`, `EndTask`, `ExitWindowsEx`, `PostQuitMessage`, `HWND_BROADCAST`,
`PostThreadMessage`. A Windows constant-golden (the analogue of macOS's
`TestMacCommandArgvGolden`/`quitScript`) asserts the ONLY window message the
adapter posts is `WM_CLOSE` (0x0010) and the DLL/proc set is a closed allowlist;
each is prove-failed by planting `TerminateProcess` and `WM_QUIT`.

## 8. Boot, state dir, honesty

- **Boot (PC-W4):** absolute boot instant (§2), or a drift-tolerant boot
  comparison + an explicit max record age; a test pins that two reads across a
  simulated clock step compare equal. Rev-1's "no wall-clock dependence" is struck.
- **State dir (PC-W5):** the Windows presync state/record/lock live under
  `%LOCALAPPDATA%` (reuse the in-repo non-roaming helper, e.g. `internal/instance`
  / `internal/desktop/ws_util.go`), NEVER `jobfile.Dir()`/`os.UserConfigDir()`
  (which is `%AppData%\Roaming` on Windows and would carry a pid to another
  machine, the exact §4.6 hazard). A `presync_windows.go` wiring is in scope; a
  test proves the path is under `%LOCALAPPDATA%`, not Roaming.
- **Honesty (PC-W10):** a §12-style roll-up labels every Windows-behaviour claim
  (hidden start, WM_CLOSE-closes-cleanly, launcher-no-re-exec, recovery identity,
  junction resolution) as lab-pending with its L-W id; `pidAlive` corrected to
  `WaitForSingleObject`.

## 9. Factor the watchdog (PC-W9)

Factor the whole-session watchdog (`sessionBudget`, `Run`), `watchRoots`,
`underDir`, `profileHash` out of `run_darwin.go` into a build-tag-free `run.go`,
so the R32 safety watchdog is shared and reviewed once, not copied into
`run_windows.go` where it could drift.

## 10. Lab (pending a real Windows machine + a signed-in Thunderbird)

L-W1 app resolution (known-folder; per-user install). L-W2 detection + identity +
the command-line read + absolute boot (stable across an NTP step and sleep/resume)
+ `pidAlive` via handle. **L-W3 (viability)** hidden launch, no window/flash, and
whether `thunderbird.exe` re-execs to a new pid (the scan+match must find the real
one). **L-W4 (viability)** `WM_CLOSE` to the main window closes a hidden
Thunderbird cleanly, no tray survivor. L-W5 owner idle + keep-awake. **L-W6
(viability)** `CREATE_BREAKAWAY_FROM_JOB` holds; the child survives a task
End/logoff/shutdown. If L-W3, L-W4 or L-W6 fail, the scheduled form does not ship.

## 11. Open questions for the owner (sign-off gates)

- **Q-W1:** accept `WM_CLOSE` to the proven child's single main window as the
  Windows "app's own quit" under R31 (§4), subject to L-W4?
- **Q-W2 — RESOLVED by the review:** read the `-profile` command line on Windows
  via the light `NtQueryInformationProcess(ProcessCommandLineInformation)` (§3),
  so the core is genuinely reused. Confirm.
- **Q-W3:** v1 Windows verifies application LOCATION only, no Authenticode
  publisher check — a genuine reduction vs. the macOS bundle-id check. Accept, or
  require an Authenticode check?
- **Q-W4 — scope:** TP6 is a bigger slice than rev-1 implied (core/seam changes +
  a cross-process command-line read + job-object breakaway + three viability lab
  gates L-W3/L-W4/L-W6, any of which can sink the scheduled form). Build it after
  TP5, and accept that scope — or reconsider whether Windows is worth it in v1?

## 12. Invariants on Windows

- **R30** opt-in/inert: same descriptor/flag/hook; off ⇒ nothing runs.
- **R31** only quits what we started, never kills/signals: identity by the
  `-profile` match (§3, as macOS); quit by `WM_CLOSE` to the proven pid's main
  window with a held handle + last-instant re-checks (§4); job-object breakaway so
  a task teardown cannot `TerminateProcess` it (§6); MA-557 extended to forbid the
  Windows kill primitives (§7). No `TerminateProcess`, `taskkill`, `Kill`,
  `Signal`, `WM_QUIT`.
- **R32** never gates the archive: the same `BeforeRead` recover guard and the
  shared (factored) whole-session watchdog; every outcome recorded, the archive
  reads anyway.
