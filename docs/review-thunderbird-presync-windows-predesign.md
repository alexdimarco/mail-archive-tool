# Pre-code design review — Thunderbird pre-sync Windows adapter (PC21)

Date: 2026-10-22. Reviewers: a 12-agent workflow pass — the 10 design-review
lenses (`assurance-kit/process/design-review.md`, as §13 of the parent design
applies them) + an adversarial addendum on the user32 / `WM_CLOSE`-to-another-
process calls that PC21/the TP6 row mandate. This is a model-run review, not a
named human reviewer; its findings are weighed against the code, not accepted on
authority. Target: `docs/design-thunderbird-presync-windows.md` revision 1.

Result: **73 concerns — 4 critical, 30 high, the rest medium/low.** Revision 1's
central design was refuted; the conditions below (PC-W1..PC-W11) are folded into
revision 2 of the design. The full per-agent output is the workflow transcript
(`subagents/workflows/wf_55a2e252-bd9/journal.jsonl`).

## The critical findings (all cluster on identity and the quit)

- **C1/C3/C4 — the core is NOT "reused unchanged" as rev-1 claimed.** The built
  core identifies and matches by the `-profile` command line (`ownership.go`
  `candidateIsOurs`, `session.go` `matchRecord` → `argsHaveProfile`) at S6, S8 and
  in recovery. Rev-1 refused to read the command line and claimed a handle-only
  identity, so Windows would match nothing: S6 times out `did-not-start` every
  run and a real leftover is never re-announced (P5 broken) — silently. Rev-1
  rejected the command-line read on a false premise (PEB/`ReadProcessMemory`); the
  review notes `NtQueryInformationProcess(ProcessCommandLineInformation)` needs
  only `PROCESS_QUERY_LIMITED_INFORMATION`.
- **C2 — the quit cannot be delivered through the parameterless `quit()` seam,
  and is open to pid reuse.** `env.quit()` takes no target (macOS quits by bundle
  id); Windows `WM_CLOSE` is pid-addressed, so the seam must change — a core
  change, not inside `env_windows.go`. And a pid recycled between enumerate and
  post could send `WM_CLOSE` to an unrelated app's window (R31 breach); the held
  `CreateProcess` handle that would pin the pid is never used by the core.

## Conditions folded into design revision 2

- **PC-W1** Windows reads the `-profile` command line (light `NtQueryInformation
  Process`), so the reused ownership predicate works identically to macOS; the
  "handle identity, no scan" model is dropped (and is robust to a launcher
  re-exec). The runtime command-line read is a reviewed new surface (PC-W11).
- **PC-W2** the `quit` seam carries the proven target (`quit(target)`); R31/R32
  re-asserted on the macOS path it touches.
- **PC-W3** the quit holds an `OpenProcess` handle on the proven pid across the
  critical section (pins the pid vs. reuse), posts `WM_CLOSE` to the single MAIN
  top-level window only, and re-checks handle-liveness + `GetWindowThreadProcessId`
  immediately before each post; the HWND-reuse residual is stated like macOS PC1.
- **PC-W4** boot time is an absolute OS boot instant (not `Now − GetTickCount64`),
  or a drift-tolerant comparison + max record age; "no wall-clock dependence" is
  struck.
- **PC-W5** the state dir/record/lock live under `%LOCALAPPDATA%` (not
  `jobfile.Dir()`/`os.UserConfigDir()` = Roaming), so a pid never roams; a
  `presync_windows.go` wiring is in scope.
- **PC-W6** launch with `CREATE_BREAKAWAY_FROM_JOB` so a Task-Scheduler job-object
  teardown cannot `TerminateProcess` Thunderbird mid-write (an R31 breach our code
  never causes); viability L-W6.
- **PC-W7** MA-557 gains the Windows kill tokens (`TerminateProcess`, `OpenProcess`
  +`PROCESS_TERMINATE`, `WM_QUIT`, `WM_DESTROY`, `WM_ENDSESSION`, `EndTask`,
  `ExitWindowsEx`, `PostQuitMessage`, `HWND_BROADCAST`, `PostThreadMessage`) + a
  `WM_CLOSE`-only constant-golden; prove-failed.
- **PC-W8** app resolution via `SHGetKnownFolderPath` (not the `%ProgramFiles%`
  env var); no portable per-file writability check on Windows, so v1 verifies
  LOCATION only (a genuine authenticity reduction, Q-W3); per-user-install
  decision; system DLLs loaded by absolute System32 path (no DLL-hijack).
- **PC-W9** factor the whole-session watchdog + `watchRoots`/`profileHash` into a
  build-tag-free `run.go`, shared and reviewed once (not copied per platform).
- **PC-W10** adopt the parent's claim labels; every Windows-behaviour claim gets
  an L-W id; `pidAlive` corrected to `WaitForSingleObject`; a §12-style roll-up.
- **PC-W11** the cross-process command-line read is bounded (this user's
  `thunderbird.exe`, `PROCESS_QUERY_LIMITED_INFORMATION`, a size cap, unreadable ⇒
  not-ours) and owes the TP6 adversarial addendum.

## Owner sign-off gates (carried to the design §11)

Q-W1 (WM_CLOSE to the main window as the app's quit, subject to L-W4); Q-W2
(RESOLVED: read the command line the light way); Q-W3 (location-only authenticity
— accept or require Authenticode); Q-W4 (the real, larger scope — core/seam
changes + three viability lab gates — build after TP5 or reconsider Windows in v1).
