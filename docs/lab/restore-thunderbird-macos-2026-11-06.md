# Restore into Thunderbird — lab row MA-1134 on macOS (and Linux), 6 November 2026

Lab row MA-1134 (`docs/scenario-catalog.md`), the macOS and Linux leg of RT-2: does the lock that
design §5.2 item 2 (as folded) names keep a starting Thunderbird out of the profile, and does
Thunderbird start normally once the holder releases it or is killed? It also covers the review's
second-holder test: does that lock block another holder? Run by an agent on this Mac between
22:00 and 22:08 EST (UTC−5) on 6 November 2026. The Linux leg ran in Docker at 03:07 UTC on
7 November, the same evening. No product code was changed or run. The holder was a small lab
program, not `mailarchive`, so the R8 re-run with `mailarchive` as the holder is still owed.

**Verdict: the Mac lock rule holds, with three changes to details around it.** With
`.parentlock` held by `F_SETLK` and no `lock` symlink, Thunderbird 159.0 did not open the
profile. After a release, and after the holder was killed, it opened normally with no stale-lock
message. A second holder was refused. The "sole opener" rule in item 2 is load-bearing: opening
and closing a second handle in the holding process silently dropped the lock, and Thunderbird
then got in. **The Linux rule (`F_OFD_SETLK`) also holds** (Thunderbird 153.6.0esr, headless,
Docker), and it survives the second-handle close that the classic lock does not. The changes are
listed under "What this changes": check A is blind on macOS; the words a person sees differ by
platform; and a refused Thunderbird writes one small file into the profile.

## Set-up

- macOS on this Mac (Darwin 25.6.0, arm64). **Thunderbird 159.0**
  (`CFBundleShortVersionString` of `/Applications/Thunderbird.app`, build id 20261026161902).
- The owner's own Thunderbird was already running (pid 90413 on their own profile under
  `~/Library/Thunderbird/Profiles/`). It was never opened, locked or signalled. Every signal went
  to a pid first confirmed by `pgrep -f restore-lab-mac/profile`. `profiles.ini` and
  `installs.ini` had the same modification time before and after the run (13 Oct 00:07:49), and
  pid 90413 was still running at the end.
- Test instance: `thunderbird -no-remote -profile <repo>/.claude/restore-lab-mac/profile`, which
  started as an empty folder. Its `user.js` only turns off first-run screens (account
  provisioner, default-client check, start page, telemetry). No account was configured.
- Holder and probe: `labtool` (`.claude/restore-lab-mac/tool/main.go`, a lab file not in git). It
  uses the exact call from item 2: `open(".parentlock", O_RDWR|O_CREAT)`, never written, then
  `fcntl(F_SETLK)` with `F_WRLCK` over the whole file (`l_start 0`, `l_len 0`), non-blocking. It
  makes no `lock` symlink. `probe` is the second holder: it reads `F_GETLK`, then tries `F_SETLK`,
  then `flock(LOCK_EX|LOCK_NB)`. With `LABLOCK=ofd` (Linux only), the same calls use
  `F_OFD_GETLK`/`F_OFD_SETLK`.
- **No screenshots.** `screencapture -x` failed every time with "could not create image from
  display", most likely because the display was asleep or locked, or because this shell lacks
  screen-recording permission. A System Events query for window titles timed out, and was not
  retried so it could not raise a permission prompt on the owner's screen. In their place:
  - a window list by pid (`winlist`, built from `tool/windows.swift` with CoreGraphics
    `CGWindowListCopyWindowInfo`). It shows owner, layer and size, but not titles without that
    permission;
  - Thunderbird's own text in `--headless` mode;
  - its child processes, and the profile files' modification times.

## What Thunderbird 159 does on macOS (baseline, no holder)

- It made **`.parentlock` only** (an empty regular file). **No `lock` symlink** existed while it
  ran, or after it quit.
- It holds `.parentlock` with a POSIX record lock. The probe saw `F_GETLK held by pid 61238` (the
  Thunderbird pid), and both `F_SETLK` and `flock` were refused with "resource temporarily
  unavailable". So item 2's lock kind is the one Thunderbird uses, and the two conflict both ways.
- A normal start looks like this: the main process plus a GPU helper, a "tab" content process and
  an "rdd" process; three windows at layer 0; `.startup-incomplete` during start.
- `kill -TERM` ended it (exit 143). `.parentlock` stays behind as an empty file, as the design
  says.

## Results on macOS

| Case | What was done | What was seen |
|---|---|---|
| Second holder | holder pid 61377 holds; `labtool probe` | `GETLK held by pid 61377`; `F_SETLK` **refused** (EAGAIN); `flock` **refused** |
| Held, headless start | `thunderbird -no-remote --headless -profile …` | refused at once, exit 0, printed: **"A copy of Thunderbird is already open. Only one copy of Thunderbird can be open at a time."** |
| Held, normal start | `thunderbird -no-remote -profile …` (pid 61387) | did **not** open the profile. One on-screen window of 458×160 at **layer 8** (the macOS modal-panel layer), only a GPU helper, no tab or rdd process. Text and buttons **not read** (no screen capture). Closed by `kill -TERM 61387` |
| Release, holder alive | holder unlocks (`F_UNLCK`) and keeps running; start | probe saw no holder. Thunderbird (pid 61422) took the lock and started like the baseline (tab and rdd processes, layer-0 windows, no layer-8 dialog): **opened normally** |
| Holder killed | new holder pid 61447, `kill -9`; start | lock free at once (probe took it). Thunderbird (pid 61456) **opened normally**, as in the baseline. Nothing stale |
| Thunderbird holds | probe against pid 61456 | `F_SETLK` and `flock` refused: a restore that starts while Thunderbird runs fails its take, so "a failed take counts as running" (§5.1 note) works on macOS |
| Second handle closed (the macOS caveat) | holder takes the lock, then opens and closes another fd of `.parentlock` in the same process | **lock gone** while the holder still ran (`GETLK no holder`, probe took it). A headless Thunderbird (pid 61483) then **opened the profile** |

Profile writes by a refused Thunderbird: the two refused starts (headless and normal) wrote
`Telemetry.FailedProfileLocks.txt` (1 byte, content `2`, last written 22:04:23). `prefs.js`,
`times.json` and `sessionCheckpoints.json` were last written before the holder's take at
22:04:00.

## Results on Linux (cheap leg: Docker, headless)

`docker run golang:1.26` (Debian 13.7, arm64, kernel 7.0.14-linuxkit), with `thunderbird`
installed from Debian: **Thunderbird 153.6.0esr**, not 159. It ran as a non-root user,
`--headless`, on a profile in the container's `/tmp`. Scripts: `.claude/restore-lab-mac/linux/`.
Transcript: `linux/linux-run-output.txt`. **This is the Docker `--headless` variant, not a
desktop VM**, as the row asks to record. No GUI dialog was seen.

- Baseline: Thunderbird holds `.parentlock` with a classic `fcntl` lock **and** makes the `lock`
  symlink (`lock -> 172.17.0.2:+2619`). A clean quit removed the symlink. `flock` did **not**
  conflict with Thunderbird's lock: Linux keeps the two apart, as item 2 says.
- An `F_OFD_SETLK` holder blocked a second `F_OFD_SETLK` holder and a classic `F_SETLK` holder,
  and Thunderbird's lock blocked an `F_OFD_SETLK` probe. They conflict both ways.
- Held, Thunderbird started: refused, printed **"Thunderbird is already running, but is not
  responding. To use Thunderbird, you must first close the existing Thunderbird process, restart
  your device, or use a different profile."** The only profile write was
  `Telemetry.FailedProfileLocks.txt`.
- After release (holder alive), and after `kill -9` of the holder: Thunderbird took the lock and
  started with no lock message. The only matching output line was "You are running in headless
  mode".
- The OFD holder opened and closed a second handle: the **lock stayed held**, and Thunderbird
  was still refused. In contrast, a classic `F_SETLK` holder doing the same **lost** its lock.
  This confirms why item 2 uses OFD on Linux.

## What this changes

1. **Check A (§5.1) is blind on macOS with Thunderbird 159.** `thunderbird.Running`
   (`internal/thunderbird/thunderbird.go:127-135`) and presync's `macEnv.lockPID`
   (`internal/presync/env_darwin.go:310-316`) look only for the `lock` symlink, then for
   Windows' `parent.lock`. Neither exists on this Mac while Thunderbird runs, so A says "not
   running". The restore is still safe on macOS: check B (the process list) and the failed
   `F_SETLK` take both catch a running Thunderbird. But the design's description of A, and
   `thunderbird.go`'s comment ("on Unix/macOS Mozilla writes a `lock` symlink"), are wrong for
   macOS. A fix for the lead to weigh: on macOS and Linux, A also asks `F_GETLK` on `.parentlock`.
   That is a read-only question that takes no lock, and it reports the holder's pid on macOS
   (Linux reports −1 for an OFD holder). The same blind spot exists in shipped presync. It fails
   safe there, because B fires. This is a design note, not a code change in this run.
2. **The words a person sees differ by platform.** W-WAIT (§5.2 item 5) says "If you open it and
   it says Thunderbird is already running, choose Cancel". That matches Windows (lab B) and the
   Linux text above. On macOS, the headless text is "A copy of Thunderbird is already open. Only
   one copy of Thunderbird can be open at a time.", and the normal dialog's text and buttons were
   not seen. Whether it has a Cancel button is unknown. W-WAIT's macOS wording waits on someone
   looking at that dialog.
3. **A refused Thunderbird writes one file into the profile:** `Telemetry.FailedProfileLocks.txt`
   (a counter of failed lock attempts), on both macOS and Linux. It is outside Local Folders and
   not mail. Any test or lab row that expects the profile to be byte-for-byte unchanged after
   Thunderbird was refused must allow this file. The restore itself never writes it.
4. The "sole opener" requirement on macOS (item 2) is confirmed necessary, not just careful: the
   per-process `F_SETLK` lock is lost the moment any other descriptor of `.parentlock` in the
   same process closes, and Thunderbird then gets in. The MA-1127 test that "nothing else opens
   it while held" is the guard for this.

## Still pending in MA-1134

- **macOS normal-start dialog text and buttons** (W-WAIT wording). The smallest check: with the
  holder running, start the test Thunderbird with the screen awake, and read the dialog. The lab
  folder has the tools; or grant this terminal screen recording and re-run `screencapture`.
- **Linux on a desktop with a GUI**, and on Thunderbird 159 rather than 153.6.0esr: not run.
- **The R8 re-run with `mailarchive` as the holder**, on both platforms (as the row says).

## Evidence (in `.claude/restore-lab-mac/`, outside git)

- `tool/main.go` (holder and probe), `tool/windows.swift` (window list), built as `labtool` and
  `winlist`;
- `linux/setup.sh`, `linux/tests.sh`, `linux/linux-run-output.txt`;
- `profile/` (the throwaway profile, including `Telemetry.FailedProfileLocks.txt`);
- `shots/` is empty: screen capture was not available (see Set-up).
- The macOS command outputs quoted above come from the agent's session transcript. They were not
  saved separately.
