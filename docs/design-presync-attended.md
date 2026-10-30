# Design — the attended Thunderbird sync: Run now really syncs, skips are told, encrypted archives can sync

**Revision:** 4 (2026-10-30), folding the **second review round** (lens 2 scope / proportionality,
lens 9 dependencies and operational cost, the rescue pass, and a fold check of rev 3; recorded in
`docs/review-presync-attended-predesign.md`, section "Second round, 2026-10-30", which reviewed rev 3
sha256 `6a63ef6d…df26`). Its conditions are **C-48 to C-86**, each fold marked `[C-n]`. Rev 3
(2026-10-30) folded the owner's answers OQ-1..OQ-4 and the operator's calls OD-1..OD-4 (marked
`[OQ-n]` / `[OD-n]`). Rev 2 folded the first review (C-1..C-47, reviewed rev 1 sha256
`107c75ba…a012`). **Register:** a draft design. Nothing is built, and no test or catalog row changes
here. Branch `presync-attended`, off `main` at `ea6515a` (v1.5.1 shipped).

**Rev-4 status.** The passes OD-1 asked for have run: lens 2 and lens 9 as their own passes, plus the
rescue pass and a second reader's fold check of every `[C-1..C-47]` fold and of the rev-3 text. All
four returned GO_WITH_CONDITIONS; no finding needed a redesign. The biggest changes in rev 4:
AS4 no longer waits on lab L-A5 (Run now ships "never close" everywhere; closing Thunderbird is a
later slice, **AS-Q**) [C-48]; a second per-platform table, `AttendedStartAllowed`, lets one release
ship the macOS fix while the Windows lab is still owed [C-49]; AS1 is split into the scheduled-run bug
fixes (AS1a) and the attended mode (AS1b) [C-51]; a run where nothing arrived is no longer called
"synced" [C-68]; and the encrypted Lock window now really lasts until mail reading starts [C-76].
**Owed before AS1:** a fold check of C-48..C-86 by a second reader (a check of the folds only; no new
lens is owed). **New owner questions** OQ-5..OQ-8 (§15.3) gate parts of AS5, the waiting note, the
release, and AS8; none gates AS1.

**Status of every id proposed here.** `R38`, `S64`, `S65`, the S54 and X16 amendments and the
per-slice row plan are **accepted by the operator (2026-10-30) [OD-2]**; they land at AS0. `MA-895`
to `MA-928` (MA-922 and MA-923 new in rev 3; MA-924 to MA-928 new in rev 4) stay **provisional**
until each lands with its slice. `MA-880..MA-889` are reserved for the settings-card part B and are not
used. `S61..S63` are claimed by `docs/design-archive-manager.md:508-510`, so the new scenarios start
at `S64`. `R38` was found unused on 2026-10-30 (`grep R38 docs/scenario-catalog.md`, no match;
re-check at AS0). **How the ids land [C-42]:** AS0 adds only R38, S64, S65 and the S54 and X16
amendments; each MA row lands **in the slice that adds its test**, so the covers-map meta-test
(`assureblock/covers_map_test.go:86-97`, which fails any U/S row with no covering test) stays green
throughout. If an R row with no MA row yet fails that meta-test, R38 lands with AS1b's first row
instead (to check at AS0).

**Claim labels.** **proven** = read in the code at the `file:line` given, on this branch (by
reading, not by running: no test was run for this design). **inferred** = reasoning from the code or
from how macOS, Windows and Thunderbird behave, not verified by a run; each inferred behaviour of a
real system is also a **lab item** (L-A1..L-A10, §13). **decision** = a choice this design makes,
with its reason; the owner or a reviewer may overturn it.

**Gates before code.** The pre-code review is filed (GO_WITH_CONDITIONS, two rounds). Before AS1: a
second reader's fold check of C-48..C-86 (§14 gate 1). What gates a **platform** switching attended
Run now on is a lab record cited by its `AttendedStartAllowed` entry (§2.7) [C-49]; what gates a
platform **closing** Thunderbird after an attended run is L-A5 there, in slice AS-Q (§2.8) [C-48].
One release tag builds every platform (`.github/workflows/release.yml`, lines 1-9, proven), so these
are per-platform switches inside one release, not per-platform releases.

---

## 1. Goal and the owner's rulings

### 1.1 What is wrong today (the trace, re-read)

| # | What happens | Proof |
|---|---|---|
| T1 | A Run now click never starts Thunderbird. The session's start check (state S4) skips when the owner touched the keyboard or mouse in the last 2 minutes, and a click on Run now is such a touch. | proven: the gate `session.go:113-116`, the 2 minutes `api.go:43`; that a mouse click resets the idle clock the gate reads is **inferred** (lab L-A1) |
| T2 | There is no way for a caller to say "the owner asked for this". | proven: `presync.Config` has five fields and none is about attendance, `api.go:17-23` |
| T3 | Even if it started, quitting needs 10 minutes with no keyboard or mouse use. | proven: `api.go:47`, `session.go:226-235` |
| T4 | A scheduled run skips when Thunderbird is already open. | proven: `session.go:117-120`, `running.go:23-42` |
| T5 | Every skip that is a "note" is recorded nowhere. The run history has no sync field. | proven: `hook.go:49-54`, `api.go:58-61`, `state/runhistory.go:31-44`; `LastRun.Presync` exists but nothing writes it |
| T6 | The Run now note shows the run log's last line while it runs, then only the result; the result never mentions the sync. | proven: `dashboard.go:2133-2141`; `jobrun.go:49, 92` |
| T7 | Run now's log lives only in memory. | proven: `maintenance.go:44` |
| T8 | The pre-sync's lines in `mailarchive.log` carry no time. | proven: `wizard.go:242, 274`, `jobrun.go:43, 74`, `api.go:147-151` |
| T9 | The Run now tooltip promises "it first opens Thunderbird, waits for its mail, then closes it". | proven: `dashboard.go:1623`; untrue because of T1 and T3 |
| T10 | The Mac note says to answer macOS's permission question from a Run now, which never closes Thunderbird. | proven: `api.go:105` vs `api.go:78` |
| T11 | An encrypted archive cannot turn the sync on. | proven: `settingscard.go:103-104`; `dashboard.go:1202-1206`; `schedule.go:287-291`; `addkeyed.go:71-81` |
| T12 | "Made from Thunderbird" is judged from the archive's manifest, sealed for an encrypted archive. | proven: `localarchive.go:274-295`; the sealed form is **inferred** from phase-2b A21 |
| T13 | An encrypted Run now already goes through the same options builder. | proven: `jobrun.go:74`, `jobrun.go:132-136` |
| T14 | A plaintext Run now cannot be cancelled. | proven: `jobrun.go:45`; `jobcontrol.go:27-35`, `capture.go:231-232` |
| T15 (B6) | An unreadable pre-sync state record's warning is dropped. | proven: `recovery.go:40-43`; `session.go:106-109` |

Found while reading, and relevant here:

- **T16.** Nothing in `app.Run` checks for cancellation between the pre-sync's return and the first
  source read. Proven: no `ctx.Err()` between `app/run.go:588` and `app/run.go:789`; catch-up at
  `app/run.go:715-717`.
- **T17.** The pre-sync's waits use a plain `time.Sleep`. Proven: `env_darwin.go:294`,
  `env_windows.go:85`; the S7 poll is 10 s, `api.go:50`.
- **T18.** Rev 3's S8c, S9 late scan and `presync_stuck` streak were never built. Proven:
  `session.go:189-193, 269-272`; `record.go:52-54`; `health.go:650`.
- **T19.** The manual `mailarchive -thunderbird-sync` meets the same 2-minute check, while several
  remedies send the owner to it (`api.go:72, 74, 76, 77`).
- **T20.** The command line has its own copy of the hook. Proven:
  `cmd/mailarchive/presync_hook.go:19-`, against `presyncjob/hook.go:4-8`.
- **T21 (new in rev 2, from the review) [C-2, C-3].** The watchdog is already too short and leaks.
  Today's limit is `defAwaitUp + defMaxWait + defQuitGrace + 30 s` = 1440 s (`run.go:23`, proven);
  the unattended worst path adds up to about 1444 s before any probe time (90 await + 2 await poll +
  1200 wait + 10 last poll + 20 quit request + 120 grace + 2 exit poll; **inferred** arithmetic from
  `api.go:39-52`, `env_darwin.go:109`). When it fires, `runWatchdog` returns `internal-error` with
  no release (`run.go:47-48`, proven); the abandoned goroutine keeps running and keeps the
  per-profile pid lock, which counts as stale only when its holder process dies (`lockfile.go:83,
  112`, proven). Harmless in a short scheduled process; inside the long-lived Desktop it would block
  every later sync. Fixed by §2.6 for both modes.
- **T22 (new) [C-8].** The write-ahead record is best effort: its save error is ignored and
  Thunderbird is launched anyway (`session.go:62-66, 142-143`, proven).
- **T23 (new) [C-1].** The C3 count drops any Thunderbird process `ps` cannot read
  (`env_darwin.go:364-368`, proven), so it can undercount.

### 1.2 The owner's rulings (2026-10-28, settled)

1. An **attended Run now may start and quit Thunderbird straight away**, skipping the 2-minute and
   10-minute idle rules. It still never touches a Thunderbird that is already open, and closes only
   the copy it can prove it started.
2. Thunderbird **starts hidden** (`open -g -j`, as today, `env_darwin.go:54`), with a progress message
   in the Run now note ("Syncing Thunderbird… waiting for mail to arrive") and the outcome after.
   **On Windows "hidden" cannot be met as written [C-47]:** Thunderbird ignored the hidden-start flag
   in lab L-W4, so today's launch sets **no** hidden flag at all (it was dropped as misleading,
   `quit_windows.go:71-83`, proven) [C-57]: the window appears, and the session tries to minimize it
   once it has proved the process ours. **Corrected [C-56]:** the "about 17 s" of rev 3 bounds the
   **minimize attempt** (a 2 s poll, then up to 15 s `minimizeWait`, `quit_windows.go:62, 106-118`,
   proven), **not how long the window is visible**: if no single main window has appeared by then
   (a slow start, a Primary Password prompt, two windows that both qualify), the attempt gives up
   and the window stays visible, today for the rest of the session (the code's own comment,
   `quit_windows.go:104-105`). Rev 4 keeps retrying (§2.7, A14).
   **Owner ruling 2026-10-30 [OQ-2]: Windows attended now, not held back.** An attended Run now on
   Windows also starts Thunderbird, minimized to the taskbar, and the owner accepts that its window
   may flash on screen for a moment first. This overrides C-47's proposal to hold Windows until a
   lab passed. See §2.7. What the owner accepted was a brief flash; a window that can stay up for the
   whole wait is reported to the owner with the lab's measurement before the Windows entry is
   switched on (§2.7) [C-56].
3. **Scheduled skips:** a WARN after **3 days with no successful sync**, measured as elapsed time like
   the source-unreachable warning; a Status WARN plus an alert email under the existing alert rules,
   once per streak.
4. **Encrypted archive:** a Lock while a Run now **waits for Thunderbird cancels that backup and
   clears the key**. **Widened 2026-10-30 [OQ-3]:** the Lock cancels it at any point **from the click
   until mail reading starts**, not only during the wait; once reading has started, the Lock reply
   says the backup finishes with its own key and Cancel stops it (§3.2).

Owner answers of 2026-10-30, besides OQ-2 and OQ-3 above: **[OQ-1]** the Run now note has a "Keep
Thunderbird open" button while it waits, and if lab L-A5 shows Thunderbird would throw away a message
being written without asking, Run now never closes Thunderbird on that platform — it leaves it open
and says so (§2.8). **[OQ-4]** the two widened encrypted leftovers are accepted (§3.2, §4.4).

Also in scope (proposed by the trace; the owner did not object): the sync outcome carried into the
last-run record and each run-history entry (encrypted: §4.4 finds the short code is **not** safe to
write); a tag on the Recent backups card; timestamps on `mailarchive.log` lines; Run now cancellable
for unencrypted archives; the tooltip and Mac note corrected; B6.

### 1.3 Non-goals

- No change to a **scheduled** run's sync rules: both idle checks, the already-open check and
  everything else stay. The watchdog fix (§2.6) and the record-save rule (§2.4 A9) apply to both
  modes because they are bugs, not rules.
- No quitting of a **leftover** Thunderbird an earlier backup started (rev-3 §3 cut (b)); Q6.
- No window inspection, keystrokes or accessibility scripting (rev-3 §3).
- No scheduled backups of encrypted archives (phase 3 O5, A14).
- No new Thunderbird settings (`check_time` untouched), and no reading of Thunderbird's `prefs.js`
  (rev-3 PC rules) [C-61].
- **Windows scheduled start unchanged** [C-57, C-58]: if the minimized-not-active start is built
  (§2.7, only after lab L-A10), it is used for attended runs only; the scheduled run keeps today's
  launch and today's `SW_MINIMIZE` backstop, which the owner accepted on 2026-10-23. Two Windows
  launch changes do reach scheduled runs, stated here: the launch's working folder becomes
  Thunderbird's own install folder [C-65], and a quit that posts nothing ends at once (A10) [C-60].

---

## 2. The attended mode

### 2.1 The fields

```go
// internal/presync/api.go
type Config struct {
    Inputs       []string
    Profile      string
    ProfileNamed bool
    StateDir     string
    Log          io.Writer
    // Attended: the person started this run and is watching it (§2.2). The S4 and S8
    // owner-activity checks are skipped; every other check stays (§2.3). Never persisted:
    // no job file, descriptor, schedule or record carries it [C-36].
    Attended bool
    // Surface picks the words of the remedies: "desktop" names buttons, "cli" names commands.
    Surface string
    // GOOS picks platform words (Dock or taskbar) [C-24]; "" means runtime.GOOS.
    GOOS string
    // Stop ends the session AT ONCE with no quit request: the person's Lock, or Shut down
    // [C-5]. Separate from ctx, whose cause ErrCancelAndQuit (the Cancel button) asks the
    // started Thunderbird to close first. nil = never.
    Stop context.Context
    // Phase reports "starting", "waiting", "cancelling" and (slice AS-Q) "closing" as the
    // session moves [C-19, C-50]. The Desktop binds it to the Run now's slot id, and the
    // session makes no Phase call once it is abandoned (§4.6) [C-71].
    Phase func(phase string, since time.Time)
    // KeepOpen, if set and true when the quit is due, skips the quit: the person pressed
    // "Keep Thunderbird open" (C-46, OQ-1). Read only where a quit can be sent, so it has no
    // user before slice AS-Q [C-50].
    KeepOpen func() bool
    // NeverQuit: attended runs on this platform never ask Thunderbird to close, at quiet or on
    // Cancel; set from AttendedQuitAllowed(goos) (§2.8) [OQ-1]. Ignored when !Attended.
    NeverQuit bool
}
```

`Result` also gains `Minimized bool` (Windows: whether the backstop minimize ever succeeded, A14),
so every Windows text can say "its window may still be on screen" when it did not [C-56, C-64].

`deps` (`session.go:14-29`) gains `attended`, `stop`, `phase`, `keepOpen`, `neverQuit`. `buildDeps`
(`run.go:57-82`) copies them. **Attended and a named profile [C-18]:** when `Attended` and
`ProfileNamed` are both set, `buildDeps` also requires the profile to be listed in `profiles.ini`
(today a job-named profile skips that check, `hook.go:32`, proven); not listed → `profile-unusable`.
Nothing in the product writes a job's `Profile`, so only a hand-made file is affected (a tightening
of rev-3 PC3 for attended runs only; **accepted by the operator 2026-10-30 [OD-4]**).

### 2.2 Who sets it (decision)

| Caller | Attended? | Why |
|---|---|---|
| Desktop Run now, plaintext (`schedule.go:668-670` → `wizard.RunJobInProcess`) | **yes, from slice AS4** [C-44], on macOS and Windows alike [OQ-2], **on each platform whose `AttendedStartAllowed` entry is true** (§2.7) [C-49] | ruling 1 |
| Desktop Run now, encrypted (`schedule.go:700-715` → `wizard.RunJobInProcessKeyed`) | **yes, from AS8 only** (not AS4) [C-81], macOS and Windows [OQ-2], under the same table | ruling 1 |
| A new encrypted archive's first backup (`encryption_create.go:175`) | yes, but inert | its job is written without the sync (`addkeyed.go:71-81`, `wizard.go:1862-1878`, proven) |
| Scheduled run-job (`wizard.RunHeadless`, `wizard.go:199-330`) | **never** | no one is there |
| Command line export with `-unattended` (every baked schedule line, `cmd/mailarchive/job.go:280`) | **never** | a scheduled run |
| Command line export **without** `-unattended`, standard input a terminal in the foreground, standard error a terminal, **and a local session: no `SSH_CONNECTION` and no `SSH_TTY` in the environment** [C-9] | **yes — slice AS10, owner nod asked (Q3)** | T19. The terminal test is the encrypted prompts' (`cmd/mailarchive/encrypted.go:188-190`). A piped, backgrounded or remote (`ssh -t`) run is not attended. **On Windows** `tty.Foreground` equals `IsTerminal` (`tty_windows.go:22`, proven), so a console run without `-unattended` is attended; every baked scheduled task carries `-unattended`. |

**R38** (§11; accepted by the operator 2026-10-30 [OD-2]): *Thunderbird is started or closed without the owner-activity checks only by a
run the person started and is watching at this computer: Desktop Run now, or the command line at a
local foreground terminal without `-unattended`. No scheduled run, remote session, job file,
descriptor or archive file can turn it on.*

How it travels: `presyncjob.SetHook(opts, HookOptions{Profile, Log, Attended, Surface, GOOS, Stop,
Phase, KeepOpen, OnStart, OnEnd})`; `SetHook` sets `NeverQuit` itself from
`presync.AttendedQuitAllowed(goos)` (§2.8), so no caller can switch the close back on [OQ-1].
**And it reads `presync.AttendedStartAllowed(goos)` [C-49]:** for an attended caller on a platform
whose entry is false, it installs **no** hook — Run now does not sync there, exactly as no hook
behaves today, and the tooltip and the note say so (§7) — rather than run a scheduled-mode session
that would skip on the person's own click. `optsForJob` (`jobrun.go:114-138`) gains a `RunMode` argument and
sets two in-memory `app.Options` fields (no JSON tags): `PresyncWanted` (the job asks for the sync)
and `PresyncAttended` [C-33, C-34]. `RunHeadless` passes the scheduled mode; the two Run nows pass the
attended one. The command line moves onto `presyncjob.SetHook` (T20).

**The test seam [C-41].** `presyncjob` gains an **untagged** file holding
`var runSession = presync.Run` and `var supported = false`; the darwin and windows files set
`supported = true`. `SetHook` itself becomes untagged and installs nothing when `!supported`. Wizard
and desktop tests set `supported = true` and replace `runSession` with a scripted fake that (1)
blocks until its context or `Stop` ends, (2) records `context.Cause(ctx)` and whether `Stop` ended,
(3) calls `Phase`, `OnStart`, `OnEnd`, and (4) returns a chosen outcome. **Rev 4 [C-79]:** it also
(5) records the **whole `Config`** it receives (so MA-898 sees `Attended` and `NeverQuit`), and (6)
calls `KeepOpen()` when its scripted quiet point arrives (MA-919, slice AS-Q). Thunderbird is the
external binary, so this is the mock CLAUDE.md allows. **Quit counts are proven only in `presync`'s
own session tests** (the existing `fakeEnv`); desktop and wizard rows prove the cause delivered and
the side effects. **One test-only override for the two tables [C-79]:** `presync` exposes
`OverrideTablesForTest(quit, start map[string]bool) (restore func())`. It has to be exported from a
non-test file, since the desktop and wizard tests live in other packages; MA-927 adds a structural
check that **no non-`_test.go` file calls it**. MA-915, MA-919 and MA-922 use it to render both
values. The **real** tables stay false unless an entry carries a lab record (MA-927).

### 2.3 What attended skips and what it keeps

| Check | Where | Attended | Why |
|---|---|---|---|
| Owner idle ≥ 2 min before the start | S4, `session.go:113-116` | **skipped** | ruling 1 |
| Owner idle ≥ 10 min before the quit | S8, `session.go:226-235` | **skipped** | ruling 1 |
| The attended start itself | caller (`SetHook`) | **only where `AttendedStartAllowed(goos)`** [C-49]; elsewhere no hook for Run now | L-A1, L-A9 (macOS); MA-918 (Windows) |
| The quit itself, at quiet and on Cancel | S8, A4(ii) | **only where `AttendedQuitAllowed(goos)`** [OQ-1], and only from slice AS-Q [C-48]; elsewhere left open (§2.8) | L-A5 |
| Profile lock, one try, no wait | S2, `session.go:73-77` | kept | |
| Recovery from the state record (a leftover is announced, never quit) | S3, `session.go:99-109` | kept | R31 |
| Thunderbird already running | S4, `session.go:117-120` | kept | ruling 1 |
| Profile path re-check, running re-check, late snapshot before launch | S5, `session.go:124-139` | kept | swap guard, C5 |
| Cancellation check right before the write-ahead record and the launch | S5 (new) [C-10] | **new, both modes** | a cancel that came first starts nothing |
| Write-ahead record **saved** before the start | S5, `session.go:140-142` | kept, **now required** [C-8] | P5 |
| Ownership proven after launch | S6, `session.go:153-171` | kept | R31 |
| Await-up 90 s, maximum wait 20 min, quiet 3 min | S6, S7 | kept; whether the first-change gate can be tripped by a start-up touch before Thunderbird's first fetch is lab L-A9 [C-70] | R32 |
| Windows backstop minimize after adoption | S6, S7 | kept, **retried on each S7 poll until it succeeds once** [C-56]; the show mode chosen by a pure function (A14) [C-58] | ruling 2 |
| The shared pre-quit function (ownership re-check, C3 count, record `quitting`, one request) | S8 and every cancel quit [C-1] | kept, **one function for every quit** | R31 |
| One quit request **bounded by 20 s** (`quitTimeout`), then the 2-minute exit grace | S8 | kept unchanged in attended mode [C-6] | R31, R32 |
| Elevated refusal, app and profile resolution | S1, adapters | kept; attended also requires `profiles.ini` membership for a named profile [C-18] | |
| The whole-session watchdog | `run.go:23-50` | kept, **computed and leak-free** (§2.6) [C-2, C-3] | R32 |

### 2.4 State machine changes against rev 3 (each stated)

| # | State | Rev 3 / as built | Rev 2 here |
|---|---|---|---|
| A1 | S4 | idle under 2 min → END `owner-active-skipped` (a note) | **unless `attended`**: the idle check is not read at all (no `idleSeconds` call). |
| A2 | S8 | idle under 10 min → leave open, END `owner-active` | **unless `attended`**: then straight to the shared pre-quit function (A10), or, when `NeverQuit`, to A12 [OQ-1]. |
| A3 | S8 | request bounded by `quitTimeout` 20 s (`env_darwin.go:109, 428`), exit grace 2 min (`api.go:51`) | **unchanged in both modes [C-6].** Rev 1's 2-minute attended request is withdrawn: while a quit request waits on macOS's permission question, the person can quit our Thunderbird and open their own, and a quit addressed by bundle id could then reach theirs (**inferred**; lab L-A2 adds the delayed-delivery case). A request that times out is mapped as today (quit not answered) and leaves Thunderbird open, recorded `left-running`; its desktop remedy is an `OutcomeLine` entry (§4.6, listed there since rev 4 [C-86c]), worded without naming the program macOS asks about [C-62]: "macOS asked for permission to control Thunderbird. Choose Allow, quit Thunderbird from the Dock, then click Run now again." Revisit only if L-A2 proves a delayed request fails with error -600 once its process has gone. **Reached by attended runs only from AS-Q** (before it, attended runs never quit) [C-48]. |
| A4 | S6, S7 | cancel → record `launching` (S6) or `left-running` (S7), END `cancelled` | By cause: **(i) `Stop` ended (a Lock, Shut down) or any cause other than `ErrCancelAndQuit` (Ctrl-C, a scheduled stop):** quit nothing; record as built; `Left` = `left-open` (S7) or `may-be-starting` (S6). **(ii) cause `ErrCancelAndQuit` (the Cancel button), attended only.** **With `NeverQuit` (every platform until AS-Q) [OQ-1, C-74]:** in S7 nothing is asked to close; ownership is re-verified and the record cleared as in A12; `Left` = `left-open-by-rule`. In S6, **the C-7 poll still runs**: keep polling for the proven child up to the 90 s await limit on a short context of its own (which `Stop` also ends), and phase `cancelling` is reported; once the child is proven, re-verify ownership and clear the record as A12 does (`Left` = `left-open-by-rule`); with no child proven, `Left` = `may-be-starting` and the `launching` record stays for S3 (C-7's line). This keeps SAF-5 fixed under never-close: without the poll, the `launching` record with no child pid would make S3's weak match (`session.go:296-302`, proven) treat the person's own later Thunderbird as the leftover. **Without `NeverQuit` (slice AS-Q only) [C-48, C-50]:** in S7, take the shared pre-quit function (A10) and wait at most 15 s for the exit; gone → S9 clears the record, `Left` = `closed`; still there → `left-running`, `Left` = `left-open`. In S6 [C-7], the same poll, then A10 as in S7. **Scheduled runs: (i) only, unchanged.** |
| A5 | all waits | `e.sleep(d)` blocks | `sleepCtx(ctx, stop, d)` in an **untagged** file, used by both adapters [C-2/T4]; returns at once when `ctx`, `Stop` or the session context ends (T17). The fake's `sleep` returns without moving the clock when its context is already done, so a session test cannot pass by accident. |
| A6 | S3 | an unreadable record → warn, continue | built (B6): `sessionResult` gains `side []Outcome`; a proceeding recovery decision appends its outcome there **only when it is warn-class** [C-45] (so `matchOther`'s proceeding `already-running`, `recovery.go:72-75`, adds nothing); `presyncjob` maps every side outcome to a run warning. |
| A7 | S7 | progress line — not built | built for both modes: once a minute, "Thunderbird sync: still waiting for mail to arrive (N min so far, up to 20)", N measured from the start of the wait for mail. |
| A8 | END | outcome only | `Result` gains `Side []Outcome`, `Waited` (from "up" to the quit or the verdict), `Started`, `Minimized` (A14) [C-56], and `Left` (`none`, `closed`, `left-open`, `left-open-by-choice`, `left-open-by-rule` [OQ-1], `may-be-starting`) [C-23]; `Outcome` gains `Synced() bool` (§5.2), **which reads the outcome alone, never `Left`** [C-72]. |
| A9 | S5 | the `launching` record save is best effort (T22) | **both modes [C-8]:** launch only after the record is saved to disk; a failed save ends `cannot-start` (a warning) and launches nothing. A failed save of `quitting` or `left-running` is a side warning `presync_state_unsaved`. |
| A10 | S8, A4 | the pre-quit steps live inline in S8 | **one function, `preQuit(sctx)`, used by S8, A4(ii) and the S6 cancel path [C-1]; built in AS1a for S8 (scheduled runs), reached by attended runs only from AS-Q [C-51, C-48]:** (1) if the session is abandoned (§2.6), stop: quit nothing; (2) re-verify ownership; (3) C3 on macOS: count the **raw pids `pgrep` lists**, and count any pid whose details cannot be read as a second Thunderbird [C-1/SAF-9]; more than one → no quit, `quit-skipped-others`, `left-running`; (4) **(AS-Q, attended)** publish phase `closing` **under the Desktop's `c.mu`** (through `Phase`), **then** read `KeepOpen()`, both **before** any quit [C-73]; if it is true (C-46, OQ-1): no quit, clear the record, `Left` = `left-open-by-choice`, and the session **keeps its S7 verdict as the outcome** (`ok`, `ok-noactivity`, `watch-truncated`, `not-quiet`), or `cancelled` on a Cancel path [C-72]; (5) save `quitting`; (6) one request, `env.quit(qctx, target)`, where `qctx` derives from the session context `sctx` (§2.6) with the 20 s deadline [C-5]; (7) **a quit that posted nothing [C-60]:** on Windows, `winEnv.quit` returns the new sentinel `errQuitNotPosted` on every "cannot safely close" path (no handle, a creation-time mismatch, zero or several main-like windows, the last-instant re-check failing; today each returns nil, `quit_windows.go:129-161`, proven); `preQuit` maps it at once, with **no exit-grace wait** and no "being asked to close" text, to the new outcome **`quit-no-window`** (warn, counted synced), `left-running`, `Left` = `left-open`. This reaches scheduled runs too, which today wait out the 2-minute grace and then say Thunderbird "did not close within 2 minutes after it was asked to quit" when nothing was asked. |
| A11 | S5 | no cancellation check | **both modes [C-10]:** `ctx.Err()` and `Stop` are checked right before saving `launching` and again right before `startHidden`; either done → END `cancelled`, nothing launched, no `launching` record. |
| A12 | S8 | — | **new, attended with `NeverQuit` only [OQ-1]:** at quiet (or at the 20-minute limit), re-verify ownership, then clear the state record and END with **the S7 verdict as the outcome** (`ok`, `ok-noactivity`, `watch-truncated`, `watch-truncated-noactivity`, `not-quiet`) [C-72], `Left` = `left-open-by-rule`; no quit request is ever built. The record is cleared because the person is told Thunderbird was left open (§4.6), so the next run treats it as an open Thunderbird and leaves it alone (S4), instead of nagging about a leftover. |
| A13 | S7 | `watch-truncated` replaces both `ok` and `ok-noactivity` when the walk was truncated (`session.go:201-205`, proven) | **[C-68]** a truncated walk with no change becomes the new outcome **`watch-truncated-noactivity`** (note), so "nothing arrived" is never hidden behind a truncated "done". `ok-noactivity` and `watch-truncated-noactivity` get their own words everywhere (§4.5, §4.6): nothing arrived in 20 minutes — either no new mail, or Thunderbird is waiting for a password or sign-in nobody can see. **Decision (AS-Q):** once a platform's attended close exists, an attended run that ends with either of these two outcomes **leaves Thunderbird open** (`Left` = `left-open-by-rule`), so the person can see a window it may be waiting on; scheduled runs keep today's quit. This is the R31-safe direction, and "may quit" in ruling 1 allows it. |
| A14 | S6, S7 (Windows) | `minimizeMain` tries for up to 15 s once, then gives up for good, with `ShowWindow(SW_MINIMIZE)`, which also activates the next top-level window (`quit_windows.go:35-37, 106-118, 218`, proven) | **[C-56, C-58, C-64]** (1) the 15 s attempt waits on `sleepCtx(sctx)`, not a plain sleep, so a Lock or an abandonment ends it; (2) if it did not succeed, **one more try on each S7 poll** (every 10 s) until it **succeeds once**, never again after that (a person who restores our window to work in it is never fought); `Result.Minimized` records whether it ever did; (3) in **attended** runs the show mode comes from a pure function `minimizeMode(iconic, foreground bool)`: already minimized → no call; Thunderbird is **not** the foreground window → `SW_SHOWMINNOACTIVE` (7: minimize, activate nothing, so the person's typing stays where it was); Thunderbird **is** the foreground window (it took the keyboard) → `SW_MINIMIZE` (6), which hands activation back to the next window, most likely the one the person was typing in (**inferred**; MA-918 samples the foreground window before and after). Scheduled runs keep `SW_MINIMIZE` (§1.3). |
| A15 | all phases | — | **[C-71]** the session makes **no `Phase` call** after it is marked abandoned (§2.6); the Desktop ignores any `Phase` or `KeepOpen` call whose slot id is not the running Run now's. |

**The quit seam [C-5].** `env.quit(target proc)` (`env.go:78`) becomes `env.quit(ctx, target)`. The
session builds the deadline; `macEnv.quit` runs `osascript` under the context it is given, not
`context.Background()` (`env_darwin.go:428` today). `winEnv.quit` posts `WM_CLOSE` with
`PostMessageW`, which returns at once (`quit_windows.go:226-232`, proven, read in rev 4), so the
context bounds nothing there; it now returns `errQuitNotPosted` when it posts nothing (A10 step 7)
[C-60]. The fake records `ctx.Err()` at the call and the deadline.

### 2.5 The invariants, amendment by amendment

- **R30: unchanged.** `Attended` does nothing without the job's `thunderbird_sync`.
- **R31: unchanged in its text.** **Stated residual, widened by ruling 1:** rev 3 used the S8 idle
  check to soften (a) the person opening our hidden Thunderbird during the wait and working in it,
  and (b) macOS addressing the quit by bundle id. In attended mode, at quiet, our Thunderbird is
  asked to quit. Mitigations: the shared pre-quit function (C3 by raw pids) for (b) [C-1]; the 20 s
  request bound [C-6]; for (a), "never close" on every platform until that platform's L-A5 passes,
  and from then the "Keep Thunderbird open" button (§2.8, slice AS-Q) [C-46, OQ-1, C-48].
  Whether Thunderbird asks before discarding a message being written is **inferred** and is not
  promised in any text until L-A5 passes [C-30].
- **R31, Windows residual [OQ-2]:** an attended start on Windows can show Thunderbird's window for a
  moment while the person is typing elsewhere (accepted by the owner). **Corrected [C-56]:** on a
  slow start it can stay on screen longer; A14 keeps retrying the minimize, and the lab measures how
  long. Whether that window also takes the keyboard, so that a typed key acts on a selected message,
  is **inferred** possible and was **not** part of what the owner accepted; the Windows lab test
  checks it (§2.7, MA-918). **Tray [C-61]:** if Thunderbird's "minimize to the notification area"
  option is on, the backstop minimize hides the main window, the quit then finds no visible main
  window and posts nothing (A10 step 7), and the copy is left running (§2.7).
- **R32: unchanged; two stops added inside it.** A cancellation during or before the hook stops the
  run before any source is read (T16, MA-902), and a second check right before the source loop
  catches a cancel during the sweep, the index open and the sealed catch-up (§4.1) [C-76]. Every wait
  is bounded, and the bound is now computed and tested (§2.6).
- **S54:** "starts Thunderbird only when none is running and the owner is away" → "… and the owner
  is away, **or the owner asked for it at this computer (an attended run, R38)**". Wording accepted
  by the operator 2026-10-30 [OD-2].
- **New R38, S64, S65** — §11; ids accepted by the operator 2026-10-30 [OD-2].

### 2.6 The watchdog: computed, and an abandoned session can do no harm [C-2, C-3]

- **Computed.** `sessionBudget()` becomes a pure function in an untagged file:
  `awaitUp + awaitPoll + maxWait + pollEvery + probeTimeout × probesOnPath + quitTimeout + quitGrace
  + exitPoll + slack`, with `slack` = 60 s and `probesOnPath` counted from the code at AS1a (estimated
  9: four before S5, three in S5, two in the pre-quit function). With today's constants that is about
  90 + 2 + 1200 + 10 + 45 + 20 + 120 + 2 + 60 ≈ 1549 s (**inferred** arithmetic; the S7 folder walk's
  own time is what the watchdog exists to bound, so the slack covers a normal walk only). It is the
  same in both modes now that the quit request keeps its 20 s bound (A3). MA-921 asserts that the
  longest path (AS1a), the S6 Cancel child poll under never-close (AS1b), and, from AS-Q, the S7
  Cancel-quit path (cancel at the last S7 poll, then A10 and the 15 s exit wait) and the S6 Cancel
  path (90 s await, then A10, then 15 s) each fit, from the same constants, so a constant changed
  later re-checks itself [C-51, C-50]. **This also fixes T21 for scheduled runs (AS1a).**
- **The session context.** The session runs under `sctx`, derived from `Stop` (or `Background`), which
  the watchdog cancels when it abandons the session. Every wait and quit request in the session
  watches `sctx`, **including `minimizeMain`'s 500 ms loop** (A14) [C-64]. **Corrected [C-86e]:** the
  read-only probes (`pgrep`, `ps`, `sysctl`, `ioreg`) keep their own 5 s limit on
  `context.Background()` (`env_darwin.go:303-357`, proven) and are not changed: they only read, and
  the budget already counts them (`probeTimeout × probesOnPath`).
- **When the watchdog returns.** It returns (1) when the session returns; (2) at once when `Stop` ends
  or `ctx` ends with a cause other than `ErrCancelAndQuit` — result `cancelled`, `Left` from the phase;
  (3) when the budget runs out — `internal-error`. On (2) and (3) it **marks the session abandoned and
  cancels `sctx`**. With cause `ErrCancelAndQuit` it waits for the session's bounded Cancel path unless
  `Stop` ends first: from AS1b that is the S6 child poll under never-close (at most 90 s + one poll,
  A4(ii)) [C-74, C-50]; from AS-Q, also the Cancel quit (at most about 90 + 10 + 20 + 15 s in S6).
- **What an abandoned session may still do.** It checks the abandoned flag in `preQuit` before saving
  `quitting` and before any request, and any request already in flight has its `osascript` killed
  with `sctx`. **Labelled inferred [C-78]:** killing `osascript` does not take back an Apple Event it
  has already sent, and whether a quit held by macOS's permission question still arrives after a later
  Allow is lab L-A2's new case. So the claim is: **no quit request is started after the watchdog
  returns or after a Lock**; one already sent may still be delivered (an R31/R32 residual if L-A2
  shows it, then said in the Lock reply and the record). This applies only where a quit can be sent:
  scheduled runs, and attended runs from AS-Q. When the goroutine finally returns, it saves
  `left-running` (or leaves `launching`). **Its profile lock [C-77]:** an abandoned session must not
  release the lock while `app.Run` is still reading the sources (C2, `run.go:593-599`, proven). The
  release becomes a **once-both latch**: the lock is released once, when both the abandoned session
  has returned **and** `app.Run`'s `Release` has been called after the read (a counter under a
  mutex). So the long-lived Desktop never keeps it, and no other sync can compact the profile during
  the read.
- **A stuck lock cannot hide the streak [C-4].** Three `profile-busy` results in a row on scheduled
  runs count as one miss (§5.2).

### 2.7 Windows: attended now, minimized to the taskbar [OQ-2; replaces C-47's hold]

**The owner's ruling (2026-10-30).** Attended Run now on Windows ships with macOS: it starts
Thunderbird minimized to the taskbar, and the owner accepts that its window may flash on screen for
a moment first. C-47's proposal (Windows keeps `Attended: false` until a lab passes) is **withdrawn**;
no Windows-only branch in the callers (§2.2) remains. What rev 4 adds is a per-platform **switch**
read from a lab-cited table, the same for every platform, so the one cross-platform release need
not wait on one platform's lab [C-49].

**How it starts (decision, rewritten in rev 4).**

- **Today's launch, stated correctly [C-57].** `buildLaunchCmd` (`quit_windows.go:77-83`, proven)
  sets only `CREATE_BREAKAWAY_FROM_JOB`; there is no hidden flag to replace (it was dropped after L-W4,
  and MA-571's `TestBuildLaunchCmdFlags` pins it unset). Go 1.26's `syscall.SysProcAttr` can ask
  only for `SW_HIDE`, through `HideWindow` (`src/syscall/exec_windows.go:293, 393-395` in the go1.26.0
  toolchain, proven), so "show minimized, do not activate" (`SW_SHOWMINNOACTIVE`) cannot be asked for
  through `exec.Cmd`.
- **A lab spike first (L-A10) [C-57].** Before AS4 writes any Windows launch code, one short
  Parallels run starts Thunderbird once with `SW_SHOWMINNOACTIVE` and records whether it starts
  minimized. L-W4 showed Thunderbird ignores `SW_HIDE`, so it may ignore this too.
  - **If Thunderbird ignores it:** keep `exec.Command`; the minimized-not-active start is not built,
    MA-923 is dropped, and the backstop minimize (A14) is the whole mechanism.
  - **If it honours it:** build it **for attended runs only** (§1.3) as a direct
    `windows.CreateProcess` with a `STARTUPINFO` whose `Flags` carry `STARTF_USESHOWWINDOW` and
    `ShowWindow = SW_SHOWMINNOACTIVE`, the command line built with `windows.ComposeCommandLine` (a
    profile path with spaces), `inheritHandles` false, `CREATE_BREAKAWAY_FROM_JOB` kept, and **both
    returned handles (process and thread) closed** after every launch, so the long-lived Desktop does
    not leak two handles per Run now. The startup-info and flags come from a **pure builder** tested
    by behaviour on the Linux gate (MA-923, rewritten). MA-612's process-start walk
    (`cmd/mailarchive/noselfexec_test.go:149-163`, which knows `exec.Command`, `os.StartProcess` and
    the `syscall` start calls only, proven) adds `windows.CreateProcess`, `CreateProcessAsUser` and
    `ShellExecute`, so the new launch site is not invisible to that gate. A fake-seam test asserts
    both handles are closed. This is a reviewed change for the AS4 adversarial pass (lens 9 cost
    stated: new raw Windows code, one more launch path).
- **The backstop minimize stays, and is retried (A14) [C-56, C-58, C-64].** It no longer gives up
  for good after 15 s: one more try on each S7 poll until it succeeds once. In attended runs its show
  mode is chosen so it does not move the person's keyboard focus. If it never succeeds,
  `Result.Minimized` is false and every Windows text says "its window may still be on screen".
- **Working folder and how the Desktop was started [C-65].** The launch's working folder is set to
  Thunderbird's own install folder (both modes), so a Thunderbird left open for hours does not pin
  the Desktop's folder (which may be an archive or a removable drive). The launch now comes from the
  long-lived Desktop, not Task Scheduler: if the Desktop runs inside a Windows job object that forbids
  breakaway, `CreateProcess` with `CREATE_BREAKAWAY_FROM_JOB` fails with "access denied" (documented
  Windows behaviour; which terminal hosts do this is **inferred**). That error maps to the new outcome
  **`start-blocked`** (warn) with its own line: "Mail Archive Desktop was started in a way that cannot
  start Thunderbird; start Mail Archive Desktop from its shortcut, then click Run now again." There is
  **no retry without breakaway**: a job teardown could then kill Thunderbird in the middle of a write.
  A Desktop started "as administrator" gets `refused-elevated` with a Desktop line: "Start Mail
  Archive Desktop normally, not as administrator, then click Run now again." (§5.4's Repair remedy is
  for the scheduled task.)
- **Minimize to the notification area [C-61].** Thunderbird on Windows has an option to move itself
  to the notification area when minimized (from memory of Thunderbird's settings; **inferred**, not
  checked in this repo). With it on, the backstop minimize hides the main window, so
  `IsWindowVisible` is false, `isMainWindow` rejects it (`quit_windows.go:184-199`, proven), the quit
  posts nothing (A10 step 7: `quit-no-window`, left open at once), and the next run reports
  `leftover-found` (missed, §5.2). Scheduled runs are affected the same way. Until MA-918 confirms
  the behaviour, every Windows left-open and leftover text names **both places**: "in the taskbar or
  the notification area". `prefs.js` is not read to detect the option (§1.3).
- **Not accepted, so checked:** before this design the 2-minute idle check meant nobody was typing
  at the start; in attended mode the person may be typing, and if Thunderbird took the keyboard, its
  one-key shortcuts (Delete, A, J) would act on the selected message (**inferred**). The owner
  accepted the flash, not that. The lab test checks it; a focus steal is a ship-blocking finding that
  goes back to the owner, not a residual this design accepts on the owner's behalf.

**One release, per-platform switches [C-49].** One version tag builds every platform and one GitHub
Release (`.github/workflows/release.yml`, lines 1-9, proven), so "a release carrying Windows
attended ships only after MA-918 passes" would also hold the macOS fix. So:

- `presync.AttendedStartAllowed(goos) bool` is an untagged table, one entry per platform, **false by
  default**, built like `AttendedQuitAllowed` (§2.8). While a platform's entry is false, Run now there
  installs no sync hook and says so (§2.2, §7); it never runs a half-attended session.
- **macOS** is set true in a commit that cites the dev-Mac lab record for L-A1 and L-A9 (MA-917, test
  profile) [C-67, C-70]. **Windows** is set true in a commit that cites an MA-918 pass of its
  **first-release scope** (below).
- Each entry names the path of its lab record under `docs/lab/`, and MA-927 checks the file exists
  and names the platform (a tripwire, so the rule does not rest on memory) [C-66].
- **OQ-2 stands:** the Windows feature is built and tested on all paths from AS4; only the switch
  waits for the lab. **If MA-918 has not passed when AS4 is otherwise ready, whether the release
  waits or ships with Windows' entry false is the owner's call (OQ-7, §15.3), not this design's.**

**The Windows lab (MA-918) [OQ-2, C-49, C-59].** A **ship-gate test**, not a feature gate.

- **Start the lab set-up now, in parallel from AS1**, so it is off the critical path. **Re-check the
  VM before planning [C-59]:** rev 3's "Thunderbird is not installed in the VM" came from older lab
  notes; the catalog row MA-570 records L-W4 "CONFIRMED on real Windows 2026-10-23" with the existing
  harness `TestLabHiddenLaunchAndClose` (`internal/presync/lab_windows_test.go:90`, with
  `labLogWindows` at line 56), so Thunderbird was installed and driven after those notes. Build on
  that harness and that run.
- **Method [C-59].** Thunderbird is launched by the **real** `mailarchivew.exe desktop` through a Run
  now click in a browser inside the signed-in VM session. A human, or a **separate** input process,
  types into a third app (Notepad). A **separate observer** process records, every 100 ms, the
  foreground window (`GetForegroundWindow`) and whether Thunderbird's window is minimized. **Input is
  never injected from the process that launches Thunderbird** (Windows' foreground rules depend on
  who received the last input and who started whom, **inferred**, so a single test process would not
  reflect the real case). The VM is Windows on ARM running the amd64 program under emulation, so the
  record states that **timings are emulated**, and the report to the owner gives the flash duration
  as an emulated measurement.
- **First-release scope** (what sets `AttendedStartAllowed[windows]`): the start (the
  minimized-not-active start if L-A10 built it; else today's start), **how long the window shows**,
  **whether it takes the keyboard** (L-A7), the backstop minimize to the taskbar and the foreground
  window before and after it, **Cancel leaving Thunderbird open**, the **slow-start cases** (a profile
  with a Primary Password; Thunderbird not the default mail app) with the visible time and where typed
  keys went [C-56], the **notification-area option** on, **Thunderbird already running** from an
  earlier launch [C-61], and the Desktop started **from its Startup shortcut and from a terminal**
  [C-65]. It also logs the list of files changed before the first fetch (L-A9's check on Windows).
- **AS-Q scope** (what may later set `AttendedQuitAllowed[windows]`): the `WM_CLOSE` close at quiet,
  and L-A5 on Windows (§2.8, A10 step 7).
- **Re-run trigger [C-66].** MA-927 pins a hash of the Windows start, minimize and close functions
  (`buildLaunchCmd`, the CreateProcess builder if built, `startHidden`, `minimizeMain`,
  `minimizeMode`, `quit`) together with the date of the latest MA-918 record. Changing any of them
  turns the test red until a new MA-918 record is added **in the same commit**: the "re-run the lab"
  prompt. The pinned hash is changed only together with a new lab record, never alone (CLAUDE.md:
  never edit a baseline to make a test pass).

**Wording names the taskbar [OQ-2, C-24, C-61].** Every Windows text says "minimized to the taskbar"
where macOS says "out of sight", and "quit it from the taskbar or the notification area" where macOS
says "from the Dock" (until MA-918 settles the tray, §above); where a Windows start is described, it
adds "its window may appear for a moment" (§4.6, §7, §9).

### 2.8 Closing Thunderbird after an attended run: the button and the never-close rule [OQ-1]

**Rev 4: the close is its own slice, AS-Q [C-48, C-50].** AS4 ships with every
`AttendedQuitAllowed` entry false and **no L-A5 gate**: never-close is exactly the safe outcome the
owner chose (OQ-1) for when L-A5 fails, so holding AS4 for L-A5 protected nothing and delayed the
owner's main result. **Slice AS-Q ("attended close")** is the only commit that flips a platform's
entry to true, citing that platform's L-A5 record, and it ships, together with that first flip, all
the parts that only a close can reach: the "Keep Thunderbird open" button and its route (MA-919), the
attended Cancel quit (A4(ii) without `NeverQuit`, MA-924), the `closing` phase and the C-73 ordering,
the close-naming text variants (§7), and A13's leave-open rule for no-change runs. So the OQ-1 button
arrives the moment any close does, and the owner's ruling is still met: no platform closes
Thunderbird until its L-A5 has passed.

- **"Keep Thunderbird open" (C-46, accepted; slice AS-Q).** While the note is in the `waiting` phase it shows a
  "Keep Thunderbird open" button. It posts to `api/runnow-keepopen`, registered through `d.api` (POST
  only, body limit, `no-store`) behind `GuardLocalPOST` and CSRF, body `{run}` naming the running Run
  now's slot id; it sets a flag under `c.mu` that `KeepOpen()` reads. Refused, changing nothing, when
  no Run now is running, the id is stale, or the phase is already `closing` or `cancelling`
  ("Thunderbird is already being closed."). The button uses `busy()` with "Keeping open…"; on success
  the note reads "Thunderbird will be left open {minimized to the taskbar / out of sight} when its
  mail settles." The only effect of a forged or replayed press is a quit **not** sent, the R31-safe
  direction. The result is `Left` = `left-open-by-choice`; **the outcome stays the S7 verdict** (A10
  step 4), so whether it counts as synced comes from that verdict alone [C-72]; the archive still runs.
  **Order [C-73]:** `preQuit` publishes phase `closing` under `c.mu` before it reads `KeepOpen()`, and
  reads `KeepOpen()` before any quit, so a press the route accepted (phase still `waiting`) is always
  seen, and a press after `closing` is refused. **A Cancel after a Keep-open press** leaves
  Thunderbird open (`Left` = `left-open-by-choice`, outcome `cancelled`), and the cancel line says
  so: "Cancelled. Thunderbird was left open, as you asked; the archive is unchanged."
- **Never close where L-A5 fails (accepted).** `presync.AttendedQuitAllowed(goos) bool` is an untagged
  table, one entry per platform, **false by default** for both. A platform's entry is set true only
  in slice AS-Q's commit that cites lab L-A5 passing on that platform: Thunderbird asks before
  throwing away a message being written, after an AppleScript quit (macOS) or `WM_CLOSE` (Windows).
  **Windows, stricter [C-60]:** writing a message opens a separate compose window, so in L-A5's
  scenario `winEnv.quit` probably finds several main-like windows and posts nothing (**inferred**);
  Thunderbird then stays open with the draft intact, which could be misread as "it asked". The
  Windows L-A5 run logs the window set at quit time (`labLogWindows`) and whether `WM_CLOSE` was
  posted; the Windows entry may be set true **only if `WM_CLOSE` was actually posted while a draft
  existed and Thunderbird then asked**. If the post never happens while a draft is open, the record
  says so and the entry is set from that finding, not from a "pass". While an entry is false, an
  attended Run now there never asks Thunderbird to close — not at quiet (A12), not on Cancel (A4(ii))
  — and leaves it open, saying so: "Thunderbird synced: its mail settled after N min. It was left
  open {out of sight / minimized to the taskbar}; quit it from the {Dock / taskbar or the
  notification area} when you are done." With `NeverQuit` the "Keep Thunderbird open" button is not
  shown (there is nothing to keep).
- **Tripwire [C-66].** Each `AttendedQuitAllowed` entry carries the path of its L-A5 record under
  `docs/lab/`; MA-927 checks that the file exists and names the platform.
- **No L-A5 gate on AS4 [C-48].** A scheduled run is unaffected: it quits only after 10 minutes with
  no keyboard or mouse use, as today.
- **Texts follow the table.** AS4 builds **only the never-close texts** [C-50]; every text that says
  Thunderbird is closed (the tooltip, the first-attended-sync line, the Cancel tooltip, the release
  note) gets its close variant in AS-Q, chosen by `AttendedQuitAllowed(goos)`; MA-915's golden in AS4
  pins the never-close texts, and AS-Q adds the close variants.

---

## 3. Runs, cancellation and Lock

### 3.1 Run now, plaintext, cancellable

- `RunJobInProcess(jobPath, logger)` becomes `RunJobInProcess(ctx, stop, jobPath, logger, rn RunNow)`;
  `context.Background()` (`jobrun.go:45`) goes.
- `runKeyedJobOn` (`jobcontrol.go:27-47`) is generalised to `runCancellableJobOn`, used by both Run
  nows; `c.cancel` becomes a `context.CancelCauseFunc`. The Cancel button shows for a plaintext Run
  now too (`dashboard.go:2081`).
- Cancel calls `cancel(presync.ErrCancelAndQuit)` (today `cancel()`, `jobcontrol.go:101`). During the
  wait that runs A4(ii); during the read it is today's cancel.
- Shut down ends the run's `Stop` (the Desktop's own context, `d.life.Context()`): no quit, the
  session is abandoned at once (§2.6), so Shut down is never held by a quit.
- The cancel line is chosen by phase and `Left` [C-23, C-73]: still starting with no child proven →
  "Cancelled. Thunderbird may still be starting; quit it from the {Dock / taskbar or the notification
  area}."; waiting, or starting with the child proven, with `NeverQuit` (every platform from AS4)
  [OQ-1, C-74] → "Cancelled. Thunderbird, started by this backup, was left open {out of sight /
  minimized to the taskbar}; quit it from the {Dock / taskbar or the notification area}. The archive is
  unchanged." (Windows with `Minimized` false: "…was left open; its window may still be on screen…"
  [C-64]); **slice AS-Q only:** waiting without `NeverQuit` → "Cancelled. Thunderbird, started by this
  backup, is being asked to close; the archive is unchanged.", and after a Keep-open press → §2.8's
  line; reading → today's words (`jobcontrol.go:39`).
  The end line then comes from `OutcomeLine`, which reads `Left`, so it says whether Thunderbird
  closed.

### 3.2 Lock while an encrypted Run now waits (ruling 4)

Today a Run now takes its own write lease at the click, so a Lock does not stop it
(`schedule.go:677-684`; phase 3 §3.2, `design-encrypted-archives-phase3.md:210-224`: "a lease is an
independent key for one job"). This design **amends that rule for one window**, accepted by the
operator 2026-10-30 [OD-2].

- **The amendment, as it is to be written into phase 3 §3.2** (at AS8, the slice that builds it;
  this rev does not edit that file): "Exception (attended Thunderbird sync): for a Run now whose job
  carries `thunderbird_sync`, the person's Lock of that archive also cancels the Run now at any
  point from the click until mail reading starts, and its key is cleared; once reading has started,
  the Run now goes on with its own key, as above, and the Lock reply says so and that Cancel stops
  it. The idle lock and a switch never cancel it."
- **The window [C-10, OQ-3: owner said yes, 2026-10-30; rewritten in rev 4, C-75, C-76].** For a
  job that carries `thunderbird_sync`: from the moment `runBackupKeyed` takes the lease **until mail
  reading starts**, which is defined as the point right before the source loop (`app/run.go:788`).
  **Corrected [C-76]:** rev 3 said the hook's return is "the last step before mail reading (the T16
  check follows with no work between)". That is false: after the hook (`app/run.go:588`) and the
  deferred record (`:600`) come the orphan sweep (`:615`), the scratch-folder sweep (`:628`), the
  manifest load (`:633-637`, plaintext only), the index open including the sealed index (`:685-697`),
  the key repair (`:703`) and the sealed catch-up (`:715-716`), which decrypts archive units with the
  key, takes no context and can take minutes (all proven). OQ-3 says "any time before mail reading
  starts", so the window now covers all of those too. The window therefore covers the heal, the
  preflight, discovery, `openEncrypted` (which decrypts the whole manifest, `app/run.go:548`), the
  Thunderbird wait, and everything up to the source loop. This closes the natural "click Run now,
  then Lock, then walk away" case, in which Thunderbird would otherwise start with the key held. A11
  also guarantees that a run cancelled before S5 starts nothing.
- **How the window opens [C-75].** `runBackupKeyed` takes the lease before any job file is read
  (`schedule.go:686-699`, proven), so whether the job carries the sync is not yet known at the lease.
  Rev 4: right after the lease, `runBackupKeyed` reads the job's `thunderbird_sync` flag **once**
  (through the same `job.Read` path and `SameArchivePath` check Run now uses); that one read decides
  both the window and the hook (it is passed down as `RunNow.SyncWanted`). If the job file changed
  between that read and `RunJobInProcessKeyed`'s own `job.Read` (the flag differs), the run is
  refused before `app.Run` ("The backup's settings changed as it started; click Run now again.
  Nothing was changed."), so the window and the hook can never disagree.
- **How the window closes [C-75, C-76].** At whichever comes first: (1) **read start**: a new
  in-memory `app.Options.OnReadStart func()` (no JSON tag), called by `app.Run` right before the
  source loop; the Desktop's callback closes the window under `c.mu`; then `app.Run` makes its
  **second cancellation check** (`ctx.Err()`; §4.1); or (2) a `defer` in the Run now job function
  (the closure passed to `runKeyedJobOn`), under `c.mu`, which covers every return that never reaches
  read start: a run refused in `RunJobInProcessKeyed`, an `openEncrypted` failure, a cancel that skips
  the hook (C-10), a `not-set-up` job with no hook, an index failure, and a recovered hook panic. So a
  stale window can never let a later Lock call `stop()` on a run that has already ended and reply
  "the backup was cancelled".
- **Scope, read narrowly (flagged; owner question OQ-8, §15.3) [C-85].** The owner's answer was
  recorded as "Lock cancels an encrypted Run now any time before mail reading starts"; the question
  asked was about a Run now **with the sync**. This rev builds it for sync jobs only, the question's
  scope. A Run now with no sync keeps phase 3's rule unchanged. If the owner meant every encrypted
  Run now, the window opens at the lease for every job (the flag read then decides only the hook);
  that is a wider change to phase 3. **§14 gate: the owner confirms the scope before AS8 is built.**
- **Atomic open and close [C-11].** The Desktop records `syncWait{key, stop}` under `c.mu`.
  `stopSyncWait(out)` checks the window **and** calls the stops while holding `c.mu`. The window is
  closed only by the two points above, each under `c.mu`; `syncing` is reset by the hook wrapper's
  `defer` as before, so a hook panic (recovered by `app.Run`, `app/run.go:582-587`) resets it and the
  job function's `defer` closes the window.
- **The trigger: the person's Lock only.** `lockAction` (`encryption.go:128-140`), after
  `Keys.lock(out)`, calls `d.stopSyncWait(out)`. The idle lock and an archive switch do not stop the
  run (phase 3 kept); `watchLock` (`keys.go:87-104`) is not used. **Corrected claim [C-17/M4]:** the
  idle lock **may** fire during a long Run now (the poll routes do not touch the idle clock,
  `keys.go:570-586`); it does not stop the run, as phase 3 says, and it is not a reason for anything
  here.
- **What `stop` does, in order:** (1) end the run's `Stop` and `cancel(errLockedDuringSync)` — a cause
  that is not `ErrCancelAndQuit`, so nothing is asked to quit, and `Stop` ends any quit request
  already in flight, including a Cancel quit that began first [C-5]; (2) `lease.Lock()` — no new use of
  the key can start, and the key is destroyed when the run releases its one reference
  (`keyring.go:241-252`; `apply.go:23-29`); (3) the reply's note (§9).
- **How fast the key goes (rewritten [C-5]).** Inside the hook: `Stop` makes the watchdog return at
  once (§2.6), the hook returns `cancelled`, `app.Run`'s post-hook check returns, `recordRun` writes
  the minimal encrypted record, `RunJobInProcessKeyed`'s `done()` releases the reference
  (`jobrun.go:86`), and the locked lease destroys the key. The bound is the time to write that one
  small record (**inferred**: about a second on a working disk; a stalled disk holds it, as it would
  any write). No quit wait, poll or folder walk is on that path any more. **Before the hook**
  [OQ-3]: the run stops at the next step that reads its context, and at the latest does not call the
  hook (A11, §4.1); the key goes when the current step (a heal or the decrypting open) ends, which on
  a large archive can be minutes (**inferred**, stated). **After the hook, before read start
  [C-76]:** the post-hook T16 check or the second check before the source loop stops it; a Lock during
  the sealed catch-up (which takes no context) takes effect when the catch-up ends. The Lock reply
  says the backup was cancelled and its key is being cleared **once the current step ends**, never
  that it is already gone.
- **After the window closes (the read) [C-13, OQ-3].** The run goes on with its own key, as phase 3
  says; the Lock reply says the backup finishes with its own key, then the key is cleared, and that
  Cancel stops it now (§9).
- **The race with read start.** `OnReadStart` closes the window under `c.mu` **before** the second
  cancellation check. Either `stopSyncWait` finds the window open (it cancels; the second check then
  sees it; nothing is read) or closed (the read goes on, and the reply says so). MA-913 drives both
  through a seam that pauses between the window's close and the check.
- **Thunderbird after a Lock.** Left open, recorded `left-running` (or `launching`). The next sync's S3
  announces it. The Lock reply says how to close it, and stays on screen (§9) [C-21]. Q2 (quit first?)
  is settled by ruling 4 and the review: the key goes first.
- **Widened residuals, accepted by the owner 2026-10-30 [C-17, OQ-4].** (1) The decrypted manifest,
  units and store, opened before the hook, now sit in memory through the wait (up to about 25
  minutes), and are left to the garbage collector on a cancel; rev 1 called this unchanged, it is
  widened. The reply says "its key is being cleared", never that the decrypted record is. (2) §4.4's
  timing residual. Both stay stated here as accepted residuals, not fixed.

### 3.3 What the locks and the lease hold during the wait

- The archive lock is held for the whole wait (rev 3 P4; `app/run.go:571-600`, proven).
- The per-profile lock from S2 through the read (C2); after an abandonment it is released once both
  the abandoned session has returned and the read's `Release` has run (§2.6's once-both latch)
  [C-77].
- An encrypted Run now holds its lease, and so the key, through the wait: up to about 25 minutes
  longer than a Run now with no sync. The person's Lock ends it (§3.2). Said in the Settings card's
  encrypted line (§8.1).

### 3.4 Command line at a terminal (slice AS10)

- Attended as §2.2 says. Ctrl-C: A4(i), no quit, and the line "Thunderbird sync: cancelled —
  Thunderbird, started by this run, was left open; quit it from the {Dock / taskbar or the
  notification area}." [OQ-2, C-61]

### 3.5 What is not changed

- A scheduled run's cancellation still records `left-running` and quits nothing. Rev 3's S8c for
  scheduled runs stays out of scope.

---

## 4. The visible outcome

### 4.1 Data model and where the outcome rides [C-12, C-11]

```go
// internal/app/run.go — in memory only; no JSON tags [C-36]
type PresyncOutcome struct {
    Code, Name, Class string   // Class: "none" | "note" | "warn"
    Synced   bool              // §5.2's table, from the outcome alone [C-72]
    Left     string            // none | closed | left-open | left-open-by-choice |
                               // left-open-by-rule | may-be-starting [C-86a]
    Minimized bool             // Windows: the backstop minimize succeeded (A14) [C-56]
    Line     string            // the owner-facing line, surface and platform words; no path
    Started  time.Time
    Waited   time.Duration
}
type BeforeReadResult struct { Warnings []state.RunWarning; Release func(); Outcome *PresyncOutcome }
type Result struct { …; Presync *PresyncOutcome }
type Options struct {
    …
    PresyncWanted, PresyncAttended bool // set by optsForJob; never written
    OnReadStart func()                  // called right before the source loop (§3.2) [C-76]
}
```

**Rev 4 [C-80]:** `Attended` is dropped from `PresyncOutcome`; the streak reads
`opts.PresyncAttended` (§5.3), so nothing that could reach the persisted type carries it [C-36].

- **Before the hook [C-10]:** `app.Run` does not call `BeforeRead` when `ctx` is done; it returns
  cancelled through the normal record path.
- **In the hook block [C-12, C-80]:** `begun` is a `state.LastRun` (`beginRun`, `app/run.go:571,
  1033`, proven), so `begun.Presync` is the **persisted** `*state.PresyncRecord`, which carries JSON
  tags (`lastrun.go:135-142`, proven). The hook block therefore **converts** `br.Outcome` into a
  `state.PresyncRecord` holding only `code`, `name`, `result` and `since` (§4.2), and sets that on the
  **record value** the way `begun.Warnings` is set today, **before** `defer recordRun` is registered
  (`app/run.go:600`). The full in-memory outcome is kept in a local, `presyncOut`, from which
  `Result.Presync` is set for the caller after line 782 and on every early return. So the later
  `result = Result{…}` at `app/run.go:782` and the early `return Result{}, …` at `app/run.go:622, 635,
  695, 705` (all proven) can no longer lose it. **Slice:** the conversion onto `begun` and the record
  write land in AS7 (§12); AS2 keeps only `presyncOut` and `Result.Presync` in memory [C-53].
- **The T16 check [C-11]:** placed **after** `defer recordRun` and `defer presyncRelease` are
  registered and **before** the sweep: `ctx.Err() != nil` → return the current result with
  `context.Canceled`, so the record ends `cancelled`, never left `running` (which `health.go:626-627`
  would show RED).
- **The second check, at read start [C-76]:** right before the source loop (`app/run.go:788`), call
  `opts.OnReadStart` (if set), then check `ctx.Err()`; done → take **the source loop's own cancel
  path** (`finish(…, true, …)` then `context.Canceled`, `app/run.go:807-814`, proven), so whatever the
  sealed catch-up already did is kept exactly as a cancel during the first source would keep it, and
  the record ends `cancelled`. This catches a cancel (Cancel, Lock, Shut down) during the sweeps, the
  index open, the key repair and the sealed catch-up, for every run, not only sync jobs (a run
  cancelled in that stretch today reads until `runFile` sees the context; the change only stops it
  sooner).
- **Keyed runs [C-16]:** `recordRun` leaves the record's `Presync` nil when `opts.Key != nil`, so no
  refactor of the encrypted marshaller can leak it.
- A recovered panic in the hook sets `Presync{Name: "internal-error", Class: "warn"}` beside today's
  warning.
- `presyncjob` builds the outcome from `presync.Result`: `Line` is
  `presync.OutcomeLine(o, surface, goos, left, waited)` (§4.6).

### 4.2 The last-run record (plaintext)

**Slice AS7, right after AS4, together with its first visible user, the tag (§4.5) [C-53].**
`state.PresyncRecord` (`lastrun.go:135-142`) is written for the first time: `code`, new `name`, new
`result` (`"synced"`, `"not_synced"`, `"cancelled"`), `since`. **No `attended` field [C-36].**
`warn_streak`, `note_streak`, `state`, `expected_until`: not written (kept so older records read).
**`ReadLastRun` strips control characters from `Presync`'s strings** as it does for Job, Error and
Warnings (`lastrun.go:280-286`, which today skips Presync) [C-37].

### 4.3 The run history (plaintext)

(Slice AS7 [C-53].) `RunHistoryEntry` gains `Presync string json:"presync,omitempty"` (the name), copied in
`RunHistoryEntryFrom`. The reader strips control characters; every surface shows names only through
the closed map of §4.5 [C-37].

### 4.4 Encrypted archives: nothing about the sync is written beside the archive

The task proposed "encrypted: the short code only". It **does not pass** the minimisation rules:

- A6 limits an encrypted archive's cleartext status files and moves "the source kind" into the sealed
  record (`design-encrypted-archives.md:410-416`); A18 makes the allow-lists explicit (`:593-596`); the
  code already drops the source kind from the encrypted source streak (`app/sourceloss.go:91-93`).
- Any pre-sync code or name says **this archive is made from Thunderbird on this computer**, a source
  kind. So nothing is written: the encrypted last-run shape has no such field
  (`encrypted_record.go:133-141`), `minimal()` rebuilds history entries from four fields
  (`runhistory.go:47-49`), `recordRun` keeps `Presync` nil for a keyed run (§4.1), and the sync-streak
  writer is gated on `opts.Key == nil && !ArchiveEncrypted(out)`; a keyed run **removes** a stray
  `.mailarchive-syncstreak.json` if one is present [C-16].
- **How it is pinned (MA-904, rewritten) [C-16].** After a synced, a skipped and a Lock-cancelled Run
  now on an encrypted archive: the JSON **key set** of the last-run and of every history entry equals
  the A18 allow-list exactly; `internal/assure/leak.CheckTree(out, AtRest, nil)` plus a scan for every
  outcome name and a planted profile-path marker over the **whole** archive folder find nothing; there
  is no sync-streak file and no `mailarchive.log`. Positive twin: the same scripted run on a plaintext
  archive does write `presync`.
- **Widened timing residual, accepted by the owner 2026-10-30 [C-17, OQ-4].** The encrypted last-run keeps `Started` and `Finished` in
  clear (`encrypted_record.go:133-141`), and `Started` is stamped before the hook (`app/run.go:571`).
  A synced Run now's span now includes a 3- to 20-minute wait, and a Lock-cancelled one is a
  `cancelled` entry whose span is the wait alone; the pattern hints at a Thunderbird source. It sits
  within A6's disclosed timestamps but is wider than before.
- **Where the sync does show:** the Run now note, the in-memory run log, and (C-27) the card's last
  result for this Desktop session — all while the archive is unlocked here; a Lock or a switch clears
  them (§4.6) [C-15]. The sealed header's reserved run-details place (A24) is Q4, not this design.

### 4.5 The Recent backups card's tag (plaintext) [C-28, C-37]

`runRow` gains `Sync string`. One **closed** map from outcome name to a short plain phrase, used by the
tag, the Status line (§4.7) and the WARN reason (§5.4):

| Name(s) | Phrase |
|---|---|
| `ok`, `watch-truncated` | "Thunderbird synced first" |
| `ok-noactivity`, `watch-truncated-noactivity` [C-68] | "Thunderbird started, but nothing arrived in 20 min" |
| `not-quiet` | "Thunderbird synced (still fetching at 20 min)" |
| `quit-*` (including the new `quit-no-window` [C-60]), `owner-active`, `identity-unavailable` | "Thunderbird synced, but was left open" |
| `already-running` | "Thunderbird was already open" |
| `owner-active-skipped` | "Thunderbird not synced: you were using the computer" |
| `other-profile-running` | "Thunderbird not synced: open with another profile" |
| `profile-busy` | "Thunderbird not synced: another backup was syncing it" |
| `leftover-found` | "Thunderbird not synced: an earlier copy is still open" |
| `app-not-found` | "Thunderbird not synced: Thunderbird was not found" |
| `refused-elevated` | "Thunderbird not synced: the backup ran as administrator" |
| `profile-unusable` | "Thunderbird not synced: its profile could not be used" |
| `cannot-start`, `did-not-start`, `exited-early`, `successor-found`, `start-blocked` [C-65] | "Thunderbird not synced: it could not be started" |
| `no-desktop-session` | "Thunderbird not synced: no one was logged in" |
| `not-set-up` | "Thunderbird not synced: the sync could not be prepared" |
| `internal-error` | "Thunderbird not synced" |
| `cancelled`, absent | no tag |
| any name not in this map | "Thunderbird not synced" — the name itself is **never echoed** |

No "(see Status)" anywhere. Encrypted rows: no tag, judged by the archive kind, not by the field.
MA-905 asserts every name in `allOutcomes` is in the map or deliberately has no tag.

### 4.6 The Run now note: phases, then the outcome [C-19, C-22, C-23, C-24, C-30]

- **Phases.** `Config.Phase` reports `starting` (S5-S6), `waiting` (S7; `since` = the start of the
  wait for mail), `cancelling` (A4(ii): from AS4 only the S6 child poll under never-close [C-74]),
  and, **from slice AS-Q only**, `closing` (the pre-quit function and the exit grace) and the
  Cancel-quit `cancelling` [C-50]. A phase is never reported when no quit will be sent [C-73]. The
  Desktop keeps, under `c.mu`, `syncPhase`, `syncSince`, and at the end `syncLine`.
  `runJobOn` resets them **together with** `lines` and `result` (`maintenance.go:34`) [C-15].
- **Bound to the run [C-71].** The `Phase` (and, in AS-Q, `KeepOpen`) closures the Desktop hands to
  `SetHook` capture the Run now's **slot id**; the Desktop ignores any call whose slot id is not the
  running Run now's, so a late call from an abandoned earlier session can never overwrite the current
  run's note or show "Closing Thunderbird" while a new run starts. The session itself makes no
  `Phase` call after it is marked abandoned (A15).
- **The note is built on the server** (CI has no JavaScript runtime, so a script-built note cannot be
  tested) [C-19/T6]. `/api/activity` gains `syncPhase` and `syncNote`; while `syncPhase` is set,
  `pollBackup` shows `syncNote` **in place of** the last log line. The texts (desktop words; the
  platform words in braces are "Dock" / "out of sight" on macOS, "taskbar" / "minimized to the
  taskbar" on Windows) [C-24, OQ-2]:
  - starting: macOS "Starting Thunderbird out of sight…"; Windows "Starting Thunderbird minimized to
    the taskbar… its window may appear for a moment." [OQ-2]
  - **The one-time notice, shown before the start [C-32, C-83].** Rev 3 put it in the `starting`
    note, by which time Thunderbird is already checking accounts and sending the Outbox. Rev 4: for a
    sync-on job on a platform whose `AttendedStartAllowed` entry is true, the Scheduled backup card
    shows one line **beside Run now**, until the first attended Run now on this computer has started
    (a marker file in the pre-sync state folder, written when the session reaches S5): "New: Run now
    starts Thunderbird {out of sight / minimized to the taskbar} even while you are using the
    computer, waits for its mail, and leaves it open for you to quit from the {Dock / taskbar} when
    you are done." The close variant ("…and closes it when its mail settles, which runs Thunderbird's
    on-exit actions (for example emptying Trash).") arrives with AS-Q [C-50]. No extra click: the
    tooltip and the release note say the same.
  - waiting [C-69]: "Syncing Thunderbird… waiting for mail to arrive (N min so far, up to 20).
    Thunderbird usually starts fetching about 10 minutes after it opens, so this often takes 13 to
    20 minutes; with no new mail it takes the full 20. Thunderbird is running {out of sight /
    minimized to the taskbar}; it is left open when its mail settles." N is whole minutes from the
    wait's start and never shown above 20. (The 10-minute figure is the measured first-fetch delay,
    +628 s on the lab Mac, 2026-10-21.) **Slice AS-Q:** without `NeverQuit` the last clause reads
    "…; it is closed when its mail settles.", a "Keep Thunderbird open" button shows here (§2.8)
    [C-46, OQ-1], and once pressed the line reads "…; it will be left open when its mail settles."
    The clause about Thunderbird asking before discarding a message being written is added **only
    after lab L-A5 passes** [C-30].
  - closing (**slice AS-Q only**) [C-50, C-62]: macOS "Closing Thunderbird… If macOS asks for
    permission to control Thunderbird, click Allow." (the asking program is not named until L-A2
    says which it is); Windows "Closing Thunderbird…"
  - cancelling: from AS4, the S6 child poll [C-74]: "Cancelling… Thunderbird is still starting; it
    will be left open." **Slice AS-Q**, the Cancel quit: "Cancelling… Thunderbird, started by this
    backup, is being asked to close."; after a Keep-open press: "Cancelling… Thunderbird will be left
    open, as you asked." [C-73]
  - end: `syncLine` then the result, or `syncLine` then "Backup failed: …", or `syncLine` then the
    cancel words of §3.1. A failed run no longer hides the sync's line.
- **Reload [C-22].** On page load, if the running job is a Run now, the page calls `pollBackup` with
  Run now disabled, so the phase note and Cancel show beside Run now again (today only the capture
  poll restarts, `dashboard.go:2083`, and labels errors "Capture failed:", `dashboard.go:2073`).
- **Lock or switch [C-15].** A Lock of an archive, and a switch away from it, clear that encrypted
  job's `lines`, `result` and sync fields, and mark the slot so later lines of that run are not kept.
- **`OutcomeLine(o, surface, goos, left, waited, minimized)`**, new, past tense, every outcome has a
  line (a new outcome fails MA-897 until it has one). AS1b builds the never-close lines; AS-Q adds
  the "it was closed" lines [C-50]. **No-change lines [C-68]:** `ok-noactivity` and
  `watch-truncated-noactivity`: "Thunderbird started, but nothing arrived in 20 minutes: either there
  was no new mail, or Thunderbird is waiting for a password or sign-in. To check, open Thunderbird
  from the {Dock / taskbar}; it was left open." **`quit-not-permitted` (AS-Q) [C-86c, C-62]:**
  "macOS asked for permission to control Thunderbird. Choose Allow, quit Thunderbird from the Dock,
  then click Run now again." **`quit-no-window` [C-60]:** "Thunderbird synced, and was left open:
  its window could not be told apart from its other windows (for example a message being written),
  so it was not asked to close. Quit it from the taskbar or the notification area when you are
  done." **`start-blocked` and Desktop `refused-elevated`:** §2.7 [C-65]. **Windows with `Minimized`
  false:** "…was left open; its window may still be on screen." [C-56, C-64]. Examples, desktop,
  macOS (AS-Q): "Thunderbird synced: it was
  started out of sight, its mail settled after 6 min, and it was closed." · "Thunderbird was already
  open, so it was not started or closed; the backup read the mail already on this computer." ·
  `Left=left-open`: "Thunderbird sync: cancelled — Thunderbird, started by this backup, was left open
  out of sight. Quit it from the Dock before the next Run now." · `Left=may-be-starting`: "…
  Thunderbird may still be starting; quit it from the Dock." · `Left=left-open-by-choice`:
  "Thunderbird synced: its mail settled after 6 min, and it was left open, as you asked." ·
  `Left=left-open-by-rule` (§2.8): "Thunderbird synced: its mail settled after 6 min. It was left open
  out of sight; quit it from the Dock when you are done." · Windows, desktop, never-close (AS4):
  "Thunderbird synced: it was started minimized to the taskbar and its mail settled after 6 min. It
  was left open; quit it from the taskbar or the notification area when you are done." [OQ-2, C-61]
  (C-47's "Windows Run now before L-A7" line is withdrawn: no Windows Run now skips for that reason.)
- **Warnings, worded when read [C-38].** Presync warnings are saved with their code; each surface
  renders the code in its own words when it reads the record (Desktop names buttons, the CLI names
  commands, `desktopWarnText` and `warnText`). The saved `Message` is the fallback for an unknown code
  only, so `mailarchive status` never says "click Run now" and the Desktop never names a command.
- **Encrypted (U16).** The encrypted result gets the same `syncLine` in front; it names Thunderbird and
  no file of the archive.

### 4.7 The Status line

- `status` and the Desktop health view keep `presync_syncs` (`health.go:599-601`) and add, from the
  last-run record's `presync`, one line: "Thunderbird sync at the last backup (DATE): " + the §4.5
  phrase. Plaintext only. Never a WARN by itself.
- **Encrypted [C-27].** The setting is read from the Run now job file in the settings folder (not the
  archive): the encrypted card and the Desktop health view show "Thunderbird sync: on (Run now
  only)", and, while unlocked in this Desktop session, the last Run now's sync phrase (memory only,
  cleared by Lock). `mailarchive status` shows the setting and "last result: not kept for encrypted
  archives". That last clause is an X16 exception, **accepted by the operator 2026-10-30 as a
  stated residual [OD-3]**: for an encrypted archive, `mailarchive status` never shows the last sync
  result, and X16's amendment (§11) says so.
- `status -json` gains an additive `thunderbird_sync.last` (plaintext) and `thunderbird_sync.enabled`;
  `JSONVersion` stays 2. **Named in slice AS5 and asserted by MA-909 [C-55]:** `last` present for a
  plaintext archive with a recorded sync, absent for an encrypted one, which carries the setting only;
  `JSONVersion` still 2. (Kept rather than cut: `status -json` is a status surface X16 covers.)

### 4.8 `mailarchive.log` timestamps, and a lasting Run now log [C-14]

- `optsForJob` takes the run's `*log.Logger`; the pre-sync's `Log` is a line writer calling
  `logger.Print` per line, so every line is dated (T8). The invalid-since warning (`jobrun.go:124`)
  uses `logger.Printf`.
  These timestamps stay in **AS3**: scheduled runs need them.
- **The lasting Run now log is its own optional slice, AS-L, not owner-ruled and cuttable [C-54].**
  It came from the trace (T7), no owner ruling asks for it, and once the outcome is in the last-run
  record and the history (AS7) the owner can see what happened without it. It is out of AS4 so the
  security-relevant ordering below (SEC-5/C-14) is not on the path the owner is waiting on. AS4 keeps
  the cancellable slot and the reload re-attach. If AS-L is built:
- **Plaintext Run now** appends to `<out>/mailarchive.log` through `runlog.Open` with the first line
  "Run now from MailArchive Desktop" — **opened only after** `job.Read`, `RequireExistingOut`,
  `seal.RefuseWrite` and `app.RefuseKeyless` have passed, in `RunHeadless`'s order
  (`wizard.go:222-235`), so a plaintext-path Run now on an archive that is really encrypted (a
  misjudged kind at the click, `schedule.go:664`) writes nothing (`runlog.Open` creates 0644 and has no
  encrypted check, `runlog.go:32`, proven).
- **Stated residuals (M5):** the log opens before `app.Run` takes the archive lock, so a Run now then
  refused by that lock leaves a header and its refusal in the log (accepted: true and harmless); on
  macOS and Linux a size rotation (a rename) can race a scheduled run's append, so a few lines may
  land in `.1`.
- **Windows rotation [C-54].** Go opens files on Windows without delete-sharing (`sharemode :=
  FILE_SHARE_READ | FILE_SHARE_WRITE`, go1.26.0 `src/syscall/syscall_windows.go:395`, proven), so
  `runlog.Open`'s rotation rename (`runlog.go:27-31`, proven) fails while another process holds
  `mailarchive.log` open. A Desktop Run now would hold it for up to about 25 minutes; a scheduled run
  starting then would fail at `runlog.Open`, and `RunHeadless` would report a failed backup with a
  failure email and a "rotate log" reason (`wizard.go:235-239`, proven), instead of the clearer
  "another backup is running"; and the reverse for a Run now during a scheduled run. **Fix:** a failed
  rotation is not fatal: `runlog.Open` appends without rotating and returns a run warning ("the log
  could not be rotated because another backup had it open"), for **both** callers (Run now and
  `RunHeadless`). MA-907 adds an injected rename failure.
- **Encrypted Run now writes no log file** (E3, U16).

---

## 5. The 3-day warning for scheduled skips (ruling 3)

### 5.1 Where it lives

`<out>/.mailarchive-syncstreak.json`, 0600, atomic write, read with `util.ReadSecureFile` and a 4 KiB
bound; strings stripped on read [C-37].

```go
type SyncStreak struct {
    Version     int
    Serial      int       // goes up only when a miss follows a reset: one value per real streak [C-35]
    Missed      int       // scheduled sessions in a row that did not sync
    Busy        int       // scheduled profile-busy results in a row [C-4]; reset by any other outcome [C-86d]
    NoChange    int       // OQ-5 (if yes): scheduled runs in a row ending ok-noactivity [C-68]
    FirstNoChange time.Time // OQ-5: when that run of no-change sessions began
    FirstMissed time.Time
    LastMissed  time.Time
    LastSynced  time.Time
    LastName    string    // the latest miss's outcome name; shown only through §4.5's map
}
```

Written by `app.recordRun`, beside `updateSourceStreak` (`app/run.go:1245-1247`), **plaintext only**
by code, not prose: `opts.Key == nil && !ArchiveEncrypted(out)` [C-16]. P3-6 (encrypting an existing
archive) must remove this file with the other sidecars.

### 5.2 What counts

| Session outcome | Counts as |
|---|---|
| `ok`, `watch-truncated`, `not-quiet`, `quit-not-permitted`, `quit-timeout`, `quit-skipped-others`, `quit-no-window` [C-60], `owner-active`, `identity-unavailable` | **synced** |
| `ok-noactivity`, `watch-truncated-noactivity` [C-68] | **synced** for the missed streak (so a quiet mailbox never reads as "not synced"); **and, if the owner says yes to OQ-5**, counted in the separate `NoChange` run, which raises its own softer warning after 3 days of nothing but no-change scheduled runs (§5.4) |
| `already-running` | **synced** (decision; Q1) |
| `profile-busy` | **neither**, except that the third in a row on scheduled runs counts as one **missed** and resets `Busy` [C-4]; any other scheduled outcome resets `Busy` to 0 [C-86d] |
| `cancelled`, **whatever its `Left`** (a Cancel under never-close leaves Thunderbird open but synced nothing that was read) [C-72]; no session because the sync is off, the run was refused or failed before the hook, the platform's `AttendedStartAllowed` entry is false (§2.7), or the archive is paused | **neither** |
| `state-unreadable` | never the main outcome (side only, A6), so never counted [C-45] |
| `leftover-found` | **missed** (decision [C-20/M3]): that copy may be fetching mail, but it is hidden and stuck; counting it missed is what makes the owner hear of it, and its remedy says to quit it |
| everything else (`owner-active-skipped`, `other-profile-running`, `exited-early`, `successor-found`, `cannot-start`, `start-blocked` [C-65], `no-desktop-session`, `did-not-start`, `profile-unusable`, `app-not-found`, `refused-elevated`, `internal-error`, and `not-set-up` [C-34]) | **missed** |

`Outcome.Synced()` implements the synced rows **from the outcome alone; `Left` never enters it**
[C-72]. The left-open endings keep their S7 verdict as the outcome (A10 step 4, A12), so they are
synced or not by that verdict, and a cancelled run is never synced. MA-903 checks `Synced()` for
every entry of `allOutcomes`; MA-903 and MA-922 also assert that a cancelled run with `Left` =
`left-open-by-rule` or `left-open-by-choice` does not reset the streak.

### 5.3 The rules [C-33, C-34, C-35]

- **Which run is scheduled (defined).** `scheduled = opts.Unattended && !opts.PresyncAttended`.
  A plaintext Desktop Run now runs with `Unattended: true` on purpose (`jobrun.go:130`, proven, for the
  source streak) but is attended, so it is not scheduled. A manual CLI run without `-unattended`
  (before AS10, or piped after it) is neither.
- **Reset:** a synced session from **any** run sets `LastSynced` and clears `Missed`, `Busy`,
  `FirstMissed`, `LastMissed`.
- **Extend:** only a scheduled run with `opts.PresyncWanted` whose session missed. If `PresyncWanted`
  is set but no hook could be installed (`presyncjob.StateDir()` failed, `jobrun.go:133-135`), the run
  counts as the outcome **`not-set-up`** (missed) with a run warning, instead of looking like "sync
  off". When `Missed` goes from 0 to 1, `Serial` goes up.
- **Neither:** every other case, including any attended run that did not sync (the note told the
  person) and any manual run.
- **Removing the file.** Only where the sync is switched off: the Settings save and the schedule edit
  that turn `thunderbird_sync` off, and a **scheduled** run whose own job or schedule has no sync
  (judged from configuration, `PresyncWanted` false, not from whether a hook exists). A manual
  `mailarchive -out X` run without `-thunderbird-sync` never removes it.
- **Threshold:** stale when `Missed ≥ 2` and `LastMissed − FirstMissed ≥ 3 days`, never compared with
  now (a computer that was off trips nothing).
- **Clock rules:** MA-891's rules (`sourceloss.go:76-95`): a time before `MinPlausibleTime` moves no
  time field **but `Missed` still counts up** (as the source streak does); a clock that went back
  restarts `FirstMissed`, and `Serial` does **not** change, so it never starts a new email "streak".

### 5.4 Status WARN, with a remedy per reason [C-20]

`health.assessKeyless`, when `schedulePresyncConfigured(in)` and the archive is plaintext and the
streak is stale: WARN **`presync_not_synced`**: "Thunderbird has not been synced before this archive's
scheduled backups since DATE (N backups in a row; the last time: PHRASE). The backups still ran, but
mail that had only reached the mail server may be missing. Do this: REMEDY." PHRASE from §4.5's map;
REMEDY by `LastName` (desktop words; the CLI names the command):

| Last miss | Remedy (draft words for the friction review) |
|---|---|
| `owner-active-skipped` | "Click Run now on the Scheduled backup card to sync while you are here. To stop this coming back, change the backup's time in Settings to when you are usually away, or leave Thunderbird open at that time." |
| `leftover-found` | "A Thunderbird an earlier backup started is still open {out of sight / minimized}. Quit it from the {Dock / taskbar or the notification area}, then click Run now." [C-61] |
| `other-profile-running` | "Thunderbird was open with another profile at backup time. Quit it before the backup time, or leave this profile's Thunderbird open." |
| `app-not-found` | "Thunderbird was not found in its usual place. Install it there, then click Run now." |
| `refused-elevated` | "The backup ran as administrator, and the sync never runs that way. Click Repair on the Scheduled backup card." (A Desktop Run now started as administrator gets §2.7's own line, not this one [C-65].) |
| `start-blocked` [C-65] | "Thunderbird could not be started from the way Mail Archive was started. Click Repair on the Scheduled backup card; if you use Run now, start Mail Archive Desktop from its shortcut." |
| `profile-unusable` | "This Thunderbird profile could not be used. Open Thunderbird once, then click Run now." |
| `no-desktop-session` | "No one was logged in at backup time, so Thunderbird could not be started. Stay logged in at the backup time." |
| `not-set-up` | "The sync could not be prepared on this computer. Click Repair on the Scheduled backup card." |
| `profile-busy` (after 3) | "Another backup kept this Thunderbird profile busy. Shut down Mail Archive, open it again, then click Run now." |
| any other | "Click Run now on the Scheduled backup card to sync while you watch." |

Never RED. `presync_stuck` stays on the allow-list for older records and is never produced. MA-909
asserts the remedy for each reason and a hostile `LastName` (shown as the generic phrase).

**The no-change warning (only if the owner says yes to OQ-5) [C-68].** A separate WARN
`presync_nothing_arrived`, under the same "configured, plaintext, never compared with now" rules,
when `NoChange ≥ 2` and the no-change run spans ≥ 3 days: "Before this archive's scheduled backups
since DATE, Thunderbird was started but fetched nothing (N backups in a row). That can mean no new
mail, or that Thunderbird is waiting for a password or a sign-in that nobody can see. Do this: open
Thunderbird; if it asks for a password or sign-in, answer it, then click Run now." Any session in
which something arrived (`ok`, `watch-truncated`, `not-quiet`, a left-open ending with one of them)
resets `NoChange`. If the owner says no, `NoChange` is not built and no-change runs count only as
synced. MA-926 (AS5).

### 5.5 The alert email, once per streak [C-29, C-35, C-40]

- **Who sends:** the scheduled run-job (`RunHeadless`), after `app.Run`, for a run that did not fail,
  when the streak (read back) is stale. The CLI flat schedule and Run now send none.
- **Once per streak:** `<key>.sync-alert` beside the alert streak holds `ToldFor` = the streak's
  `Serial` (an integer, so no time comparison). A stale streak whose `Serial` equals `ToldFor` was told.
  **`ToldFor` is written only after the email was delivered**, or at once when alerts are off or the
  failure option is not chosen (as `Report` does, `streak.go:146-151`), so a failed delivery is retried
  at the next stale run. A told-file that cannot be read fails loud: the email sends and the file is
  rewritten.
- **One email per run.** If this run sends the recovery email (a failing streak ended, `wizard.go:326-
  327`), the sync email is deferred to the next stale run (`ToldFor` not written).
- **Under the existing rules:** a new result `alert.ResultSyncStale`, sent under the failure option
  (Q5). The checkbox (`dashboard.go:1633`) is relabelled "a failed backup, or Thunderbird not synced
  for 3 days (recommended)".
- **The words (draft, for the friction review).** Subject: "Mail Archive: Thunderbird not synced for 3
  days — ARCHIVE". Body: "The scheduled backups of ARCHIVE on COMPUTER still ran, but Thunderbird has
  not been synced before them since DATE (N backups in a row). Mail that had only reached the mail
  server, not this computer, may be missing from the archive. The last time: PHRASE. Do this: REMEDY
  (§5.4). You will get this email once, until Thunderbird syncs again." PHRASE and REMEDY come only from
  the closed maps.
- **Uninstall and schedule removal [C-40].** Uninstall removes `*.sync-alert` like `*.alert-streak`
  (`uninstall/config.go:37-39, 105`), with a check that keeps the uninstall suffix and the
  `backupalert` suffix the same. `ClearStreak` (schedule create and Remove, `streak.go:204-206`) also
  removes `<key>.sync-alert`. Uninstall also removes the pre-sync state folder's known files (the
  state record, the per-profile locks, the C-32 marker) and then the empty folder — named files only,
  no recursion, the same overlap guards (`ConfigTargets` skips directories today,
  `uninstall/config.go:85-94`). **Which folder, per platform [C-63]:** the target is
  `presyncjob.StateDir()`, not `<config>/presync/`. On macOS that is the job folder plus `presync`
  (`statedir_darwin.go`, `jobfile.Dir`); on Windows it is `%LOCALAPPDATA%\MailArchiveDesktop\presync`
  (`statedir_windows.go`, through `instance.Dir`, `instance.go:48-57`, all proven), which uninstall
  never touches today. MA-910 checks the Windows location too (the platform passed as a parameter to
  the target-list builder). The archive's own `.mailarchive-syncstreak.json` stays with the archive
  (R36).

### 5.6 With auto-pause and the alert streak

- A run that finds the source unreachable fails before the hook, so it moves neither the sync streak
  nor the email; the source warning and auto-pause carry on unchanged.
- A failing run sends its failure email; the sync email waits for a run that did not fail; and a run
  that sends the recovery email defers the sync email (§5.5). So one run never sends two emails.

---

## 6. Failure ordering

A plaintext Run now, in order:

1. CSRF and the run-job evidence (`schedule.go:648-662`); the job slot is free.
2. `job.Read`; `RequireExistingOut`; not paused; `seal.RefuseWrite`; `app.RefuseKeyless`; not Outlook
   (`jobrun.go:27-42`). **Then**, only if slice AS-L is built, the Run now log opens (§4.8) [C-14,
   C-54].
3. `app.Run`: the archive lock; discovery; the "running" record (`app/run.go:571`).
4. `ctx` checked; the hook (S1..S9, phases, `OnSyncEnd`), skipped if already cancelled [C-10].
5. `defer recordRun` registered with `begun.Presync` set (from AS7); then the T16 check → cancelled,
   nothing read [C-11, C-12].
5b. The sweeps, the manifest load, the index open, the key repair, the sealed catch-up; then
   `OnReadStart` and the second check → cancelled, nothing read [C-76].
6. The read; `recordRun`: last-run (with `presync`), history, source streak, sync streak.

An encrypted Run now: the lease at the click, then one read of the job's sync flag (the Lock window
opens here for a sync job [OQ-3, C-75]), the heal, then 2.–6. with the key applied before step 3. The
window closes at step 5b's `OnReadStart`, before its check, or at the job function's `defer` on any
earlier return [C-75, C-76].

Crash points and the direction they fail (the "Desktop killed" rows are new [C-8]):

| Crash after | State left | What the next run decides |
|---|---|---|
| a failed save of the write-ahead record | nothing launched (A9) | nothing to recover |
| the write-ahead record, before the start | `launching` | S3 clears it or announces a match (as rev 3) |
| Desktop killed during attended S7 | `syncing` (saved at adoption with the child pid, `session.go:172-174`, proven; rev 3 said `launching`) [C-86b], Thunderbird running hidden (Windows: minimized, or on screen if the minimize never succeeded) | S3 matches the pid and start time → `leftover-found`, announced, never quit; counts missed if scheduled (§5.2) |
| Desktop killed during the Cancel quit, after `quitting` was saved | `quitting` | S3: the process gone → clear; still there → `leftover-found` |
| Desktop killed during the S8 request | `quitting` | as the row above |
| the final last-run, before the sync streak | one session not counted | towards one fewer miss |
| the sync streak, before the told-file | the email is sent again at the next stale run | towards one more email |
| the Settings sync save (§8.2) | `job.Write` renames a whole file | either job is valid |

---

## 7. Corrected texts

| Where | Today | Proposed |
|---|---|---|
| Run now tooltip, plaintext, sync on (`dashboard.go:1623`) | "…it first opens Thunderbird, waits for its mail, then closes it…" | **AS4, never-close (every platform until AS-Q) [C-50]:** macOS: "Run this archive's backup now. Because Thunderbird sync is on, it first starts Thunderbird out of sight — even while you are using the computer — and waits for its mail to arrive (usually 13 to 20 minutes), then leaves it open for you to quit from the Dock. A Thunderbird that is already open is left alone. You can cancel while it waits; that skips the backup." [C-69] Windows [OQ-2]: "…it first starts Thunderbird minimized to the taskbar (its window may appear for a moment) — even while you are using the computer — … then leaves it open for you to quit from the taskbar or the notification area…" and the rest the same [C-61]. **Platform whose `AttendedStartAllowed` entry is false [C-49]:** "Run this archive's backup now. On this computer Run now does not start Thunderbird yet, so it backs up the mail already here; scheduled backups still sync Thunderbird first." **AS-Q, close allowed [OQ-1]:** "…then closes it, which runs Thunderbird's on-exit actions (for example emptying Trash)…". Linux: no sync sentence (the sync never runs there) [C-24]. C-47's "Windows before L-A7" variant is withdrawn. |
| Run now tooltip, encrypted, sync on (EncRunNow, `dashboard.go:1623`) [C-25] | "Back up this encrypted archive now, while it is unlocked here." | the plaintext sync text, plus: "While it waits for Thunderbird, the archive stays unlocked for this backup. Until it starts reading mail, Lock cancels it; after that, Cancel stops it." [OQ-3] |
| Cancel tooltip, sync-window variant (`dashboard.go:1502`) [C-23] | "Stop this backup now…" | while the sync waits, **AS4 (never-close)** [OQ-1, C-69]: "Stop this backup now. Nothing is added to the archive: the whole backup is skipped, not only the wait. Thunderbird, started by this backup, is left open for you to quit from the {Dock / taskbar or the notification area}." **AS-Q (close allowed):** "Stop this backup now. Nothing is added to the archive: the whole backup is skipped. Thunderbird, started by this backup, is asked to close, which runs its on-exit actions (for example emptying Trash)." |
| "Keep Thunderbird open" button tooltip (new, **AS-Q**) [OQ-1, C-50] | — | "Leave Thunderbird open when its mail settles, instead of closing it. The backup still runs." |
| `SyncNotice` (`api.go:100`), shared by every surface [C-31] — **corrected in AS4**, the slice that makes today's sentence untrue [C-52] | "…A Thunderbird you already have open, or one you are using, is left alone…" | keep every X16 disclosure, and replace that sentence with: "A Thunderbird you already have open is left alone — never started or closed. A scheduled backup also waits until you have been away from the computer; a backup you start with Run now does not wait for that." [C-84] (widened to name the command line only when AS10 ships). Where a platform's `AttendedStartAllowed` entry is false, the last clause is left out on that platform. |
| `SyncNotice`, encrypted card variant [C-25] — **AS8**, where the encrypted box is born [C-52] | — | the X16 disclosures with no scheduled-backup sentence (Windows: "minimized to the taskbar" [OQ-2]): "Before each Run now, Mail Archive starts Thunderbird out of sight and waits for its mail to arrive (usually 13 to 20 minutes), then leaves it open for you to quit. A Thunderbird you already have open is left alone — never started or closed." AS-Q adds the close variant ("…then closes it, which runs Thunderbird's on-exit actions (for example emptying Trash).") [OQ-1, C-50]. |
| `MacAutomationNote` (`api.go:105`) [C-52, C-62] | "…run one backup yourself first (the Run-now button…)…" (false today, T10, and still false under never-close: a Run now never closes Thunderbird, so it never raises macOS's question) | **AS4:** drop the Run-now instruction and keep a note that is true without any lab, worded without naming the asking program and without promising anything across updates: "On a Mac, the first time a scheduled backup closes Thunderbird, macOS may ask for permission to control Thunderbird; until it is answered, each run leaves Thunderbird open. To answer, open System Settings > Privacy & Security > Automation and allow Mail Archive to control Thunderbird." (draft, friction review). **AS9, after L-A2 and after macOS's `AttendedQuitAllowed` flip (AS-Q):** if one Allow covers both: "…Click Run now once while you are here and choose Allow; the scheduled backup then needs no answer." — and the "after an update" promise only if L-A2 shows the permission survives a new build. **MA-915 asserts the note never names Run now while macOS's `AttendedQuitAllowed` entry is false.** Encrypted card variant [C-25], AS9 after the flip: "The first time Run now closes Thunderbird, macOS may ask for permission to control it; stay until it finishes and choose Allow." (before the flip the encrypted card shows no Mac note). |
| `warnText` remedies naming commands (`api.go:72-84`) | command lines | rendered per surface by code when read (§4.6) [C-38] |
| `noteText` / `SummaryLine` (`api.go:116-144`) | present tense | kept for the live log; `OutcomeLine` for end-of-run surfaces |
| Run now's cancel (`jobcontrol.go:102`) | "…stops at its next safe point…" | by phase (§3.1) [C-23] |
| Release notes [C-32, OQ-2] — **AS4's line written in AS4** [C-52] | — | AS4 (never-close), naming only the platforms whose `AttendedStartAllowed` entry is true in that release: "Run now now syncs Thunderbird even while you are at the computer: it starts Thunderbird out of sight on a Mac, or minimized to the taskbar on Windows (its window may appear for a moment), waits for its mail (usually 13 to 20 minutes), and leaves it open for you to quit. Scheduled backups still wait until you are away." AS-Q, per platform flipped: "…and now closes it when its mail settles, which runs Thunderbird's on-exit actions such as emptying Trash; while it waits, Keep Thunderbird open leaves it running." |

MA-915's golden covers darwin, windows and linux for the tooltips, the phase notes and `OutcomeLine`
[C-24], every surface of `SyncNotice` [C-31], a job file under a non-default name [C-18/M2], every
Windows text naming the taskbar (and the notification area where §2.7 says) [OQ-2, C-61], and both
values of each table [OQ-1, C-49], **slice by slice [C-52]:** AS4 pins the never-close texts, the
plaintext `SyncNotice` correction, the AS4 Mac note and release line, and the start-not-allowed
variants; AS8 the encrypted variants (with MA-911); AS9 the Mac note's Run-now wording; AS-Q the
close variants.

---

## 8. Encrypted archives can sync

### 8.1 The Settings card's sync-only box

- **Shown when** all hold: macOS or Windows (a `goos` parameter, as `settingsInputs.goos` already is
  [C-24]); the archive is encrypted; **the job Run now would run** (§8.2) exists and reads securely; and
  either it already carries `thunderbird_sync` (never hidden while on) or it is **made from
  Thunderbird** (§8.3). Otherwise the card stays location-only, with a one-line reason when the job
  cannot be read.
- `settingsCardFor` returns for the encrypted kind `Sync`, `SyncOn`, `SyncReason` and `SyncOnly`;
  the encrypted variants of `SyncNotice` and the Mac note (§7) [C-25]. `dashboard.go:1204` stops wiping
  `data.Set` when `SyncOnly` is set.
- Label: "Sync Thunderbird before each Run now". Line: "This archive is backed up only when you click
  Run now, so the sync runs then, while you are here. While it waits for Thunderbird, the archive stays
  unlocked for that backup. Until it starts reading mail, Lock cancels it; after that, Cancel stops
  it." [OQ-3]
- **The control [C-26].** A dedicated Save, the only Save on a `SyncOnly` card, posting to
  `api/runnow-sync`; it uses `busy()` with "Saving…" (MA-872); success reply "Saved. Run now will sync
  Thunderbird first." (or "Saved. Run now will not sync Thunderbird.").

### 8.2 The sync-only save path [C-18]

`POST api/runnow-sync`, **registered through `d.api`** (POST only, the body limit, `no-store`) behind
`GuardLocalPOST` and CSRF; body `{drawnFor, presync}`. Every refusal before any write, ending "Nothing
was changed.":

1. `drawnFor` missing or stale — **nil is refused** (no older page ever posts here, unlike
   `stalePage`'s allowance, `schedule.go:208-213`);
2. `presync` missing or not a boolean (never read as off);
3. not an encrypted archive ("use Save in Settings");
4. Linux;
5. paused;
6. a task running;
7. no Run now job here;
8. the job file unreadable;
9. switching on when not made from Thunderbird here.

**Which file.** Exactly the job Run now would run: `BuildKindEvidence`'s `JobPath`, **re-resolved at
request time** (it can fall back to scanning the job folder, `kindevidence.go:116-140`, proven), read
with `readJobToEdit`'s `SameArchivePath` check. The tooltip and the box use the same path [M2].
Switching on writes `ThunderbirdSync{}` and **never carries a `Profile`**; switching off removes the
field. Only that field changes, through `writeJob`. **While locked:** the route works — it needs no key,
and the job file is in the settings folder, not the archive; Run now itself stays disabled while
locked (phase 3), so the change takes effect at the next unlocked Run now.

### 8.3 "Made from Thunderbird", from the job file

`jobThunderbirdSourced(j, goos)`: not an Outlook job, and at least one of `j.Inputs` is a local path
inside a Thunderbird profile on this computer (`findProfileDir`, `localarchive.go:299`) — the wizard's
own test (`presyncOfferable`, `wizard.go:1748-1758`), with the platform as a parameter so it can be
tested on the Linux gate [C-24]. **Corrected [C-18/SEC-9(f)]:** rev 1 said "the run still checks the
profile's `profiles.ini` membership"; that is false for a job-named profile (`hook.go:32`). In attended
mode it now is checked (§2.1); the product never writes a named profile (§8.2).

### 8.4 Lock, lease, leaks

- Lock: §3.2. Lease and locks: §3.3. Leaks: §4.4. U16: §4.6.

---

## 9. The Lock reply and the leftover after a Lock [C-13, C-21, C-24, OQ-2, OQ-3]

Platform words as §4.6: macOS "out of sight" / "Dock"; Windows "minimized to the taskbar" /
"taskbar or the notification area" [OQ-2, C-61]; on Windows with `Minimized` false, "minimized to the
taskbar" becomes "left open; its window may still be on screen" [C-64].

- **Window open, a child proven:** `{"ok":true, "note":"Locked. The backup was cancelled before it read
  any mail, and its key is being cleared once its current step ends; nothing was added. Thunderbird,
  which it started {out of sight}, was left open — quit it from the {Dock} before the next Run
  now."}` [C-76]
- **Window open, still starting, or before the hook [OQ-3]:** the same first sentence, then
  "Thunderbird may still be starting; if it appears in the {Dock}, quit it." (or nothing about
  Thunderbird before the hook started — the heal, the decrypting open or discovery).
- **Window open, after the hook and before read start [C-76]:** the same first sentence (the
  catch-up or index open ends first), then the Thunderbird sentence by `Left` as above.
- **Window closed, an encrypted Run now on this archive reading [OQ-3, C-13]:** "Locked for new use.
  The backup that is running has started reading mail, so it finishes with its own key, then the key
  is cleared. Click Cancel to stop it now."
- **The note stays on screen [C-21].** When the reply carries a note, the Lock script does not reload
  after 6 s (`encryptionscript.go:89`); it shows the note with a Done button, which reloads.
- **The next Run now with that Thunderbird still open:** `leftover-found`, desktop text: "A Thunderbird
  an earlier backup started is still open out of sight, so it was not synced again. Do this: click
  Thunderbird in the Dock and quit it, then click Run now again." Windows: "…is still open,
  minimized to the taskbar or in the notification area, … click Thunderbird there and close it,
  then click Run now again." [OQ-2, C-61]

---

## 10. Migration [C-39]

- **v1.5.1 job files**: no new job field. A plaintext job's Run now now syncs (release note, §7).
- **An encrypted job with the sync read by v1.5.1** (downgrade): v1.5.1 installs the hook unattended;
  it skips while the person is at the computer — harmless.
- **Records**: additive; an older reader ignores new fields. An older record has no `presync`: no tag,
  no Status line, no WARN.
- **Stated residuals of a downgrade then upgrade.** (1) v1.5.1 runs never touch the sync streak, so
  after the upgrade back `FirstMissed` can predate syncs that happened under v1.5.1, and the first miss
  can raise the WARN and the email at once. (2) v1.5.1's `AppendRunHistory` decodes the whole history
  into its older struct and writes it back (`runhistory.go:75-91`, proven), so one v1.5.1 run drops
  every saved `presync` name and the tags disappear. Both are accepted, not handled.
- **The pre-sync state record** (version 1) unchanged; attended adds no field.
- **Three new outcome names [C-60, C-65, C-68]** (`quit-no-window`, `start-blocked`,
  `watch-truncated-noactivity`) appear only in records the new code writes; v1.5.1 never reads
  `presync`, and the new code's closed map (§4.5) covers them, so no surface echoes a raw name.
- **Rollback residue**: v1.5.1's uninstall leaves `*.sync-alert` and the presync folder.
- **Older Desktop pages** never post to the new route; a post with no `drawnFor` is refused.
- **Tests (MA-916, MA-920) [C-39/T16]:** golden v1.5.1 record bytes (last-run, history, job) frozen
  from `ea6515a` as fixtures; the v1.5.1 struct definitions copied word for word for the backward
  direction; the failing test is a product change (for example renaming the new `presync` history
  key, so old fixtures read differently).

---

## 11. Catalog rows proposed (from MA-895)

**R38 — An attended sync is only one the person started, at this computer.** Thunderbird is started or
closed without the owner-activity checks only by a run the person started and is watching at this
computer (Desktop Run now; the command line at a local foreground terminal without `-unattended`, not
over SSH). The mode is never persisted; no scheduled run, remote session, job file, descriptor or
archive file can turn it on. Every other R31 check holds in it, and every quit, in any mode, goes
through the one pre-quit function.

**S64** The owner clicks Run now with Thunderbird sync on (plaintext or encrypted), at the computer, or
cancels, or locks — R30, R31, R32, R38, R37 — Thunderbird starts out of sight unless already open (on
Windows: minimized to the taskbar, its window possibly shown for a moment [OQ-2]), the note shows
each phase and the outcome, the person can keep Thunderbird open, Cancel asks the Thunderbird it
started to close (where the platform's attended close is allowed; otherwise it is left open and the
person told [OQ-1]), Lock on an encrypted archive cancels it at any point before mail reading starts
and clears the key [OQ-3].

**S65** Scheduled syncs keep being skipped, and the owner must hear of it — R32, X6, X10 — the outcome
is recorded per run (plaintext), the Recent backups card tags it, Status warns after 3 days of
scheduled runs with no sync with a remedy for the reason, one email per streak; encrypted archives
record nothing beside the archive.

(The MA ids each scenario cites are added as their rows land, slice by slice [C-42], as the operator
accepted [OD-2].)

**X16 amendment** (wording accepted by the operator 2026-10-30 [OD-2]; the encrypted exception
accepted as a stated residual [OD-3]): "an already-open Thunderbird, or an owner at the computer, is
left alone" → "an already-open Thunderbird is left alone; a scheduled backup also leaves Thunderbird
alone while the owner is at the computer, and a backup the owner starts and watches says it does not
wait for that. For an encrypted archive, status shows the setting but keeps no last result."

| ID | Tier | Slice | Asserts | Covers | Prove-fail |
|---|---|---|---|---|---|
| MA-895 | U | AS1b [C-51] | attended skips S4: owner idle 0 s, and an unreadable idle reading, with `Attended` → a launch and `idleSeconds` never called; twin without → `owner-active-skipped`, zero starts; sync off → nothing installed (R30) | R31, R38, S64 | read the idle gate in attended → the start is skipped |
| MA-896 | U | AS1b | attended keeps every other check (already running A, B, another profile; C5; profile re-check; profile busy; leftover; await-up; max wait) with the same outcome and zero-quit count; attended with a job-named profile not in `profiles.ini` → `profile-unusable` [C-18] | R31, R32, S64 | skip `detectRunning` when attended → a launch beside an open Thunderbird |
| MA-897 | U | AS1b | attended skips the S8 idle re-check: with `NeverQuit` it goes to A12, and with `NeverQuit` false (set directly in the session deps) it keeps the pre-quit function; the quit context is live at the call with a 20 s deadline in both modes [C-5, C-6]; twin unattended → `owner-active`. **Extended [C-43]:** under the fake clock, at most one progress line per fake minute with the right N in both modes; `OutcomeLine` for every outcome in `allOutcomes` (the three new ones included [C-60, C-65, C-68]), both surfaces, darwin and windows words, every `Left`, `Minimized` true and false [C-56]; AS1b pins the never-close lines, AS-Q adds the close lines [C-50]; `Waited` measured from "up" to the quit or verdict; `Started` set | R31, R32, S64 | drop the ownership re-check in attended → a non-ours pid is asked |
| MA-898 | S | AS3 (negative), AS4 (plaintext positive), AS8 (keyed positive) [C-44, C-81] | AST check matched on the mode field or constant, not a literal; and a **behavioural twin through the seam [C-41/T10]**, with the fake recording the whole `Config` [C-79]: `RunHeadless` with a sync job gives `Attended` false; `RunJobInProcess` gives true (AS4) and `RunJobInProcessKeyed` gives true (AS8, never before) for `goos` darwin **and windows** [OQ-2], **only where `AttendedStartAllowed` is true; where it is false no hook is installed for Run now** [C-49]; `NeverQuit` comes only from `AttendedQuitAllowed`, never from a caller or a file [OQ-1]; a job file and a descriptor carrying `"attended": true` (and other spellings) give false; no field named `Attended` has a JSON tag | R38 | set it in `RunHeadless` → the twin fails |
| MA-899 | U | AS10 | the CLI: no `-unattended`, local foreground terminal (faked) → attended; `-unattended`, piped stderr, background job, or `SSH_CONNECTION`/`SSH_TTY` set → not [C-9]; Windows console without `-unattended` → attended, every baked argv carries `-unattended` | R38 | drop the SSH test → the remote case is attended |
| MA-900 | U | AS1a [C-51] | the session fixes that reach scheduled runs: **cancel before S5 → zero launches, no `launching` record [C-10]**; a failed `launching` save → zero launches, `cannot-start` [C-8]; `sleepCtx` returns well before d on a real short cancel (untagged, runs on Linux) [C-2/T4]; `preQuit` at S8 **twins [C-1]:** a second Thunderbird present on macOS → zero requests, `left-running`; one pid unreadable → zero requests; any cancel cause or `Stop` → zero requests; **a quit that returns `errQuitNotPosted` → `quit-no-window` at once, no exit-grace wait, `left-running`, no "asked to close" text [C-60]** (the sentinel's mapping through `fakeEnv` on the Linux gate; `winEnv.quit` returning it on each cannot-close path in `quit_windows_test.go`) | R31, R32 | drop the raw-pid count → the unreadable-pid case sends a request |
| MA-901 | U | AS1a | B6: an unreadable state record's warning reaches the run's warnings while the session syncs; twin with a readable record → none; a proceeding note-class recovery outcome adds no side outcome [C-45] | R32 | drop the side outcome → the warning is absent |
| MA-902 | U | AS2 | a real `app.Run`: a cancel during the hook stops before any source read, index open or sealed catch-up; a cancel before the hook never calls it [C-10]; the last-run **file on disk** reads `cancelled`, never `running` [C-11, C-12]; the hook's `Release` still runs; **the second check [C-76]:** a cancel during the index open and during the sealed catch-up (through a pause seam) → `OnReadStart` called once, no source read, the record `cancelled`, the catch-up's work kept as the loop's own cancel path keeps it | R32, R18, S64 | move the check before `defer recordRun` → the file reads `running` |
| MA-903 | U | AS7 [C-53] | the outcome travels through a real `app.Run` on two paths — a full success and a failure after the hook (an index refusal) — to the last-run file (`code`, `name`, `result`, `since` only: the hook block converts to `state.PresyncRecord`, and no `Attended` exists on either type [C-80]) and the history's `presync` [C-12, C-36]; `Synced()` equals §5.2 for every entry of `allOutcomes`, reading the outcome alone: a `cancelled` outcome with `Left` = `left-open-by-rule` or `left-open-by-choice` is not synced [C-72]; `state-unreadable` never the main outcome [C-45] | R32, S65 | drop `begun.Presync` → the failure path's file has no presync |
| MA-904 | U | AS7 (record parts; must land before AS8), AS5 (streak part) [C-16, C-53] | §4.4's pin: exact key sets against the A18 allow-list; `leak.CheckTree` plus outcome-name and profile-marker scans over the whole folder; after synced, skipped and Lock-cancelled Run nows; no sync-streak file, no `mailarchive.log`; `Presync` nil for keyed runs; a stray sync-streak file removed by a keyed run; positive plaintext twin writes `presync` | R37 (A6, A18), S65 | (AS5) drop the plaintext-only guard on the reset path → a sync-streak file appears |
| MA-905 | U | AS7 | the tag: §4.5's phrases per name, including the no-change phrase, `quit-no-window` and `start-blocked` [C-60, C-65, C-68]; every `allOutcomes` name mapped or deliberately untagged; unknown names never echoed; a `presync` name **planted in an encrypted history** still shows no tag [C-16/T12]; control characters stripped | X10, S65 | show unknown names raw → the hostile case renders them |
| MA-906 | U | AS4 | `/api/activity` carries `syncPhase` and the **server-built** `syncNote` for each phase built at AS4 (`starting`, `waiting`, the S6 `cancelling`; no `closing` before AS-Q) [C-50], the counter only in `waiting` and never above 20, the waiting note's delay sentence [C-69], then `syncLine` with the result, the failure and the cancel words [C-19]; `runJobOn` resets the sync fields with `lines` and `result`; a Lock or switch clears an encrypted job's activity [C-15]; a **panicking** hook and a hook cancelled before `OnSyncEnd` leave `syncPhase` empty [C-11]; **a late `Phase` call from an abandoned earlier session, or one carrying another slot id, does not change the next run's note [C-71]**; the start-not-allowed note on a platform whose entry is false [C-49]; a reload during the wait re-attaches the Run now poll (structural) [C-22] | X11, S64 | build the failure note without `syncLine` → the Go test fails |
| MA-907 | U | AS3 (scheduled), AS-L (Run now and rotation) [C-54] | every line a scheduled run writes to `mailarchive.log` starts with a date and time; (AS-L) a plaintext Run now appends under its own header **after** the refusals; a plaintext-path Run now on an encrypted archive is refused and leaves no `mailarchive.log` and no `.1` [C-14]; an encrypted Run now writes nothing under the archive; **an injected rename failure during rotation → the log is appended without rotating, a run warning is recorded, and neither caller fails the backup** [C-54] | R18, S65 | open the log before `RefuseWrite` → the encrypted case has a log |
| MA-908 | U | AS5 | the streak: reset by any synced session; extended only when `Unattended && !PresyncAttended && PresyncWanted` and missed; **a plaintext Run now (Unattended true) that missed does not move it [C-33]**; a hook-setup failure on a sync job extends it as `not-set-up` and never deletes it; a sync-off scheduled job deletes it; a manual run without `-thunderbird-sync` does not [C-34]; three `profile-busy` in a row count one miss, and any other scheduled outcome resets `Busy` to 0 [C-4, C-86d]; `leftover-found` counts missed; a `cancelled` run with any `Left` moves nothing [C-72]; stale needs ≥ 2 misses over ≥ 3 days, never compared with now; implausible time counts `Missed` without moving times; a clock that went back restarts `FirstMissed` and keeps `Serial` [C-35]; never written for an encrypted archive; secure, bounded, stripped read | R32, S65 | compare with now → the computer-off case warns |
| MA-909 | U | AS5 | Status WARN `presync_not_synced` when configured and stale, naming date, count, phrase and the per-reason remedy for every reason in §5.4 [C-20], in each surface's words [C-38]; a hostile `LastName` shows the generic phrase [C-37]; never RED; nothing when not configured; the health view says the same; **`status -json`: `thunderbird_sync.last` present for a plaintext archive with a recorded sync, absent for an encrypted one (setting only), `JSONVersion` still 2** [C-55] | R32, X6, X10, S65 | always use the Run now remedy → the leftover case fails |
| MA-910 | U | AS6 | the email: once per `Serial`; `ToldFor` written only after delivery, or at once with alerts off; a failed delivery retried; never on a failed run; deferred when the run sends the recovery email; a clock restart inside one streak sends no second email [C-35]; the words come from the closed maps (hostile streak) [C-37]; uninstall removes `*.sync-alert` and the pre-sync state folder's named files then the folder, with the suffix check, **at `presyncjob.StateDir()`'s location for darwin and for windows (`%LOCALAPPDATA%\MailArchiveDesktop\presync`), the platform passed to the target-list builder** [C-40, C-63]; `ClearStreak` removes `<key>.sync-alert` [C-40] | S65 | drop the `ToldFor` check → every stale run sends |
| MA-911 | U | AS8 | the encrypted Settings box: shown (by `goos`) for macOS and Windows for a readable job made from Thunderbird or carrying the sync, with the encrypted `SyncNotice` variant (never-close wording until AS-Q) and no Mac note before macOS's flip, and its own Save with `busy()` [C-25, C-26, C-52]; absent on Linux, for an Outlook job, for an unreadable job (reason shown); the card and health view show the setting and the session's last result while unlocked [C-27] | X16, S64 | show it for an Outlook job → the case renders a box |
| MA-912 | U | AS8 | `api/runnow-sync`: each §8.2 refusal, including **nil `drawnFor`, missing `presync`, and a post naming another archive's job** [C-18], leaves the job file, the archive, the scheduler and the settings unchanged (`assure.Refused`, positive twin first); success edits exactly the job Run now would run (a non-default name included), changes only `thunderbird_sync`, writes no `Profile` | R30, R37, S64 | edit the default-named file → the non-default case misses |
| MA-913 | U | AS8 (the Lock-during-a-quit cases with whichever of AS8 and AS-Q lands second) | Lock while an encrypted sync Run now waits: cancelled and recorded cancelled; the `runLease` seam (`schedule.go:684`) shows the lease locked at once and the key **not** destroyed while the hook holds it, destroyed before `RunJobInProcessKeyed` returns [C-5/T8]; zero quit requests; the reply's note, still visible after the reply (no reload) [C-21]; **(AS-Q) Lock during the S8 request and during a Cancel quit** end the run within §3.2's bound; **Lock then Cancel** → zero quits; **Cancel then Lock** → the quit request dies, bounded [C-5]; **Lock before the hook** — during the heal, the decrypting open and discovery, each through a pause seam — → zero starts, recorded cancelled [C-10, OQ-3]; **the window opens after one read of the job's sync flag, and a flag changed before `job.Read` refuses the run before `app.Run` [C-75]; a run refused in `RunJobInProcessKeyed`, an `openEncrypted` failure and a not-set-up job each leave no open window, and a Lock afterwards claims no cancel [C-75]; a Lock during the sealed catch-up (pause seam) → cancelled at the next check, nothing read, the reply says "once its current step ends" [C-76]**; a Run now **without** the sync keeps phase 3's rule (a Lock does not stop it) unless OQ-8 widens it [OQ-3 scope, §3.2, C-85]; **Lock in the read phase** → the run goes on and the reply says so [C-13]; idle lock or switch during the wait does not stop it; the race through the pause seam between `OnReadStart` and the second check lands on one side [C-11, C-76]; a panicking hook leaves no stale window; Windows reply wording with `Minimized` false [C-64] | R37, R32, S64 | use `watchLock` → the idle-lock case cancels |
| MA-914 | U | AS4 | plaintext Run now: `cancellable` true; through the seam, Cancel delivers `context.Cause = presync.ErrCancelAndQuit`, Shut down and Lock do not and end `Stop` [C-41/T15]; Cancel while reading → cancelled, archive intact; the cancel line by phase and `Left` [C-23] | S64 | call `cancel(nil)` → the cause assertion fails |
| MA-915 | S | AS4, AS8, AS9, AS-Q, part by part [C-52] | the texts: tooltips (plaintext and encrypted, sync on, darwin/windows/linux, non-default job name, a platform whose start entry is false [C-49]), the Cancel sync variant (says the whole backup is skipped [C-69]), `SyncNotice` (every X16 disclosure, every surface, "with Run now" [C-84]) and its encrypted variant, the Mac note (AS4 wording; **never names Run now while macOS's `AttendedQuitAllowed` entry is false**; names no asking program before L-A2 [C-52, C-62]) and its encrypted variant, desktop words name no command, `OutcomeLine` names no path, no draft-prompt clause before L-A5; every Windows text says "taskbar" (and "or the notification area" where §2.7 says), never "Dock" or "out of sight" [OQ-2, C-61]; the close-naming texts exist only as AS-Q variants [C-50]; no Desktop text promises a behaviour the code lacks (golden) | X16, X11 | restore the old tooltip → the golden fails |
| MA-916 | U | AS7 [C-53] | migration of records: frozen v1.5.1 bytes (job, last-run, history) read by the new code; new records read by the copied v1.5.1 structs; an encrypted job with the sync keeps its other fields; a v1.5.1 history rewrite drops `presync` (residual pinned) [C-39] | S58, S65 | rename the `presync` history key → the fixture case fails |
| MA-917 | L | lab | macOS lab on a **test profile on the development Mac**, with the existing harnesses (`docs/lab/presync-lab-mac.py`; `internal/presync/lab_darwin_test.go`, MA-573) [C-67]: **L-A1 and L-A9** (their record sets macOS's `AttendedStartAllowed` entry [C-49, C-70]); L-A2 (with C-62's and C-78's cases), L-A3, L-A4, L-A6, L-A8; **L-A5** (its record sets macOS's `AttendedQuitAllowed` entry, slice AS-Q) — **pending**. The owner's real profile is optional, **never a gate**, and used only with the owner's OK | R31, R32, R38, S64 | — |
| MA-918 | L | lab, **ship-gate test** [OQ-2] | the Windows lab of §2.7, **rewritten [C-59]**: built on `TestLabHiddenLaunchAndClose` and the MA-570 run (re-check what the VM has first); Thunderbird launched by the real `mailarchivew.exe desktop` through a Run now click in a browser in the signed-in VM session; a human or a separate process types into Notepad; a separate observer samples the foreground window and Thunderbird's minimized state every 100 ms; no input from the launching process; timings stated as ARM-emulated. **First-release scope** (sets `AttendedStartAllowed[windows]`): the start, how long the window shows, whether it takes the keyboard (L-A7), the backstop minimize and the foreground window before and after it, Cancel leaving Thunderbird open, the slow starts (Primary Password; not the default mail app) [C-56], the notification-area option on and Thunderbird already running [C-61], the Desktop started from its Startup shortcut and from a terminal [C-65], the files changed before the first fetch (L-A9). **AS-Q scope** (may set `AttendedQuitAllowed[windows]`): `WM_CLOSE` at quiet and L-A5 with the window set logged and whether the post happened [C-60] — **pending**. A focus steal blocks the Windows entry and goes to the owner. Re-run before any release changing the Windows start, minimize or close code (MA-927's hash pin) [C-66] | R31, R32, S64 | — |
| MA-919 | U | AS-Q [OQ-1, C-50] | "Keep Thunderbird open": in `presync`, `KeepOpen` true at quiet → zero quit requests, the record cleared, `Left=left-open-by-choice`, the outcome stays the S7 verdict [C-72], the archive still runs [C-46]; **`closing` is published under `c.mu` before `KeepOpen()` is read, and `KeepOpen()` before any quit; a Cancel after a press → zero quits, `cancelled`, `left-open-by-choice`, the cancel line says so [C-73]**; the route `api/runnow-keepopen`: refused with no change when no Run now runs, the run id is stale (slot-bound [C-71]), or the phase is `closing`/`cancelling` (`assure.Refused`, positive twin first); the press sets the flag the session reads; the button shows only in `waiting` and not with `NeverQuit`; `busy()` while posting | R31, S64 | ignore `KeepOpen` → a request is sent |
| MA-920 | U | AS5 | migration of the streak: a v1.5.1-era archive with no streak file starts clean; the downgrade residual of §10 (1) pinned as stated [C-39] | S65 | read a missing streak file as stale → the "starts clean" case fails [C-82] |
| MA-921 | U | AS1a (budget, abandoned rule, latch), AS1b (S6 child-poll Cancel path), AS-Q (Cancel-quit path) | the watchdog [C-2, C-3]: `sessionBudget()` exceeds the longest path and each Cancel path built so far, computed from the same constants; on the budget, on `Stop` and on a non-quit cause the watchdog returns, marks the session abandoned and cancels `sctx`; an abandoned session starts **no** quit request even when it reaches S8 afterwards, an in-flight request's context ends, and `minimizeMain`'s loop ends [C-64]; **after a budget timeout the profile lock is still held during the read, and is free once both the abandoned session has returned and the read's `Release` has run (the next session then gets the lock, not `profile-busy`) [C-77]** | R31, R32 | remove the abandoned check in `preQuit` → a late request is counted |
| MA-922 | U | AS1b (session), AS4 (Desktop) [OQ-1] | the never-close rule: with `NeverQuit`, at quiet and at the 20-minute limit → zero quit requests, ownership re-verified, the record cleared, `Left=left-open-by-rule`, the outcome the S7 verdict [C-72]; Cancel in S7 → zero requests, `left-open-by-rule`, outcome `cancelled`, not synced [C-72]; **Cancel in S6 → the child poll runs to the 90 s limit; a child proven → re-verified, record cleared, `left-open-by-rule`; none → `may-be-starting`, the `launching` record kept** [C-74]; the next session sees an open Thunderbird and leaves it alone (`already-running`); the Desktop note, tooltip and outcome line use the left-open words and show no "Keep Thunderbird open" | R31, S64 | build the quit when `NeverQuit` → a request is counted |
| MA-923 | S | AS4, **only if lab L-A10 shows Thunderbird honours `SW_SHOWMINNOACTIVE`; dropped otherwise** [C-57] | the attended-only launch builder, a pure function tested by behaviour on the Linux gate: `STARTF_USESHOWWINDOW` with `SW_SHOWMINNOACTIVE`, `CREATE_BREAKAWAY_FROM_JOB` kept, `ComposeCommandLine` quoting a profile path with spaces, no handle inheritance; a fake seam proves both returned handles are closed after each launch; MA-612's walk lists `windows.CreateProcess`, `CreateProcessAsUser` and `ShellExecute`; MA-571's breakaway check kept; the scheduled launch unchanged | S64 | drop `STARTF_USESHOWWINDOW` (or change the show value) → the builder check fails |
| MA-924 | U | AS-Q [C-50] | the attended Cancel quit (A4(ii) without `NeverQuit`): cause `ErrCancelAndQuit` in S7 → the pre-quit function, exactly one request, `quitting` then cleared or `left-running`, `Left` right; a second Thunderbird on macOS → zero requests; S6 Cancel → polls to the 90 s limit then one request, or none and `may-be-starting` [C-7]; any other cause or `Stop` → zero requests; an attended `ok-noactivity` or `watch-truncated-noactivity` run leaves Thunderbird open (A13) [C-68] | R31, R32, S64 | quit on every cancel cause → the Lock case sends a request |
| MA-925 | U | AS1b (session and `OutcomeLine`), AS7 (tag) [C-68] | no-change honesty: a session with no change ends `ok-noactivity`; a truncated walk with no change ends `watch-truncated-noactivity`, never `watch-truncated`; their line and phrase say nothing arrived and name the password or sign-in possibility, and never say "synced first" | S64, S65 | fold the truncated no-change case into `watch-truncated` → the case fails |
| MA-926 | U | AS5, **only if the owner says yes to OQ-5** [C-68] | the no-change count: scheduled `ok-noactivity` / `watch-truncated-noactivity` runs extend `NoChange`; any session where something arrived resets it; attended runs never move it; stale after ≥ 2 over ≥ 3 days, never compared with now; WARN `presync_nothing_arrived` with its remedy; never RED; plaintext only | R32, S65 | count an attended no-change run → the Run now case moves it |
| MA-927 | S | AS1b (the tables), AS4 (the Windows hash pin) [C-49, C-66, C-79] | every entry of `AttendedStartAllowed` and `AttendedQuitAllowed` is false unless it names a lab record under `docs/lab/` that exists and names that platform; `OverrideTablesForTest` is called from no non-`_test.go` file; a hash of the Windows start, minimize and close functions matches the one pinned with the latest MA-918 record's date | R31, R38 | set an entry true with no record → the check fails |
| MA-928 | U | AS1a (the working folder), AS4 (the rest) [C-56, C-58, C-64, C-65] | Windows start details, through the win32 fake layer and untagged pure functions on the Linux gate: `minimizeMode` (iconic → no call; not the foreground window → `SW_SHOWMINNOACTIVE`; the foreground window → `SW_MINIMIZE`); the minimize retried on each S7 poll until it succeeds once, never after; `Minimized` recorded; the launch's working folder is Thunderbird's install folder; a launch "access denied" → `start-blocked`, no second launch without breakaway; a Desktop started elevated → `refused-elevated` with the Desktop line | R31, S64 | retry after a success → the restored-window case is minimized again |

---

## 12. Build slices (each small, test-first, prove-fail then prove-pass)

Rev 4 re-cut [C-48, C-50, C-51, C-52, C-53, C-54]. Every slice keeps its own prove-fail record.

| Slice | Contents | Rows | Shippable? |
|---|---|---|---|
| **AS0** | catalog: R38, S64, S65, the S54 and X16 amendments only [C-42] (accepted [OD-2]) | — | nothing |
| **AS1a** | **scheduled-run bug fixes, both modes [C-51]:** `sessionBudget()` computed; `sctx`; the abandoned rule with the once-both lock latch [C-77]; `sleepCtx` (and `minimizeMain`'s loop on it [C-64]); `quit(ctx)`; A9; A11; `preQuit` at S8 with the raw-pid C3 count; `errQuitNotPosted` → `quit-no-window` [C-60]; the Windows launch's working folder [C-65]; B6 | 900, 901, 921 (budget, abandoned, latch), 928 (working folder) | **can ship on its own as a patch** (the watchdog leak T21, the best-effort record T22, the launch after a cancel, the C3 undercount, B6, the false "asked to quit" text); the release is the owner's call |
| **AS1b** | **attended mode in `presync` [C-51]:** `Config` fields; A1, A2, A12, A13, A15; A4(ii) under never-close with the S6 child poll [C-74]; A7; A8 with `OutcomeLine` (never-close and no-change lines [C-68]); `AttendedQuitAllowed` and `AttendedStartAllowed`, every entry false, with lab-record paths; `OverrideTablesForTest` | 895, 896, 897, 921 (S6 Cancel path), 922 (session), 925 (session and line), 927 (tables) | nothing (no caller sets attended) |
| **AS2** | `internal/app`, **in memory only [C-53]:** `presyncOut` and `Result.Presync`; `Options.PresyncWanted`, `PresyncAttended`, `OnReadStart`; the pre-hook check, the T16 check and the second check at read start [C-76] | 902 | nothing user-visible (a cancel before reading stops sooner) |
| **AS3** | `presyncjob`: untagged `SetHook` (reads both tables), `runSession` seam; `optsForJob` with a logger and a mode; the CLI on the shared hook; timestamps | 898 (negative half), 907 (scheduled part) | timestamps |
| **AS4** | **Desktop Run now, plaintext, attended where `AttendedStartAllowed` allows [C-44, C-49, OQ-2], never-close only [C-48, C-50]:** the cancellable slot and causes; phases `starting`, `waiting`, S6 `cancelling`; the server-built note, slot-bound [C-71]; reload re-attach; the first-attended-sync card line and its marker [C-83]; the tooltip, the plaintext `SyncNotice` correction, the AS4 Mac note and the release line [C-52]; Windows A14, `start-blocked` and the elevated line [C-56, C-58, C-65]; the CreateProcess start **only if L-A10 says so** [C-57]; the Windows hash pin | 898 (plaintext positive), 906, 914, 915 (AS4 part), 922 (Desktop part), 923 (only if L-A10), 927 (hash pin), 928 | **Run now syncs (plaintext)** on each platform whose `AttendedStartAllowed` entry is true: macOS once the L-A1/L-A9 record is in; Windows once MA-918's first-release scope has passed (if it has not by release time: OQ-7). Thunderbird is left open after the sync and the note says so. **No L-A5 gate** [C-48] |
| **AS7** | **records and the tag, moved up and merged [C-53]:** the `begun.Presync` conversion and write [C-80], the history field, strip on read, the Recent backups tag, the encrypted record pin | 903, 904 (record parts), 905, 916, 925 (tag) | the tag; the outcome on the record |
| **AS5** | the sync streak, the Status line and WARN, remedies, `status -json` fields [C-55]; the no-change warning **only if OQ-5 is yes** | 904 (streak part), 908, 909, 920, 926 (if OQ-5) | the 3-day WARN |
| **AS6** | the email, the checkbox label, uninstall (at `presyncjob.StateDir()` per platform [C-63]) and `ClearStreak` | 910 | the email |
| **AS8** | encrypted, **after the owner confirms OQ-8's scope [C-85]:** keyed Run now attended (not before) [C-81]; the Settings box and its Save; `api/runnow-sync`; the Lock window from one read of the sync flag to read start [C-75, C-76]; the encrypted `SyncNotice` and tooltip variants [C-52]; the phase-3 §3.2 amendment written into `design-encrypted-archives-phase3.md` [OD-2] | 898 (keyed half), 911, 912, 913 | encrypted archives sync from Run now (residuals accepted [OQ-4]) |
| **AS-Q** | **attended close, per platform [C-48, C-50]**, each flip in a commit citing that platform's L-A5 record: the `AttendedQuitAllowed` entry set true; the "Keep Thunderbird open" button and `api/runnow-keepopen`; the attended Cancel quit; the `closing` phase with C-73's order; the close-naming text variants; A13's leave-open rule for no-change runs; on Windows, `WM_CLOSE` under MA-918's AS-Q scope | 919, 924, 915 (close variants), 921 (Cancel-quit path), 913 (Lock-during-quit cases, if AS8 has landed) | Run now closes Thunderbird on that platform, with Keep Thunderbird open |
| **AS9** | the Mac note's Run-now wording, **after L-A2 and after macOS's AS-Q flip** [C-52] | 915 (Mac note part) | — |
| **AS-L** | **optional, not owner-ruled, may be cut [C-54]:** the lasting Run now log; non-fatal rotation for both callers | 907 (Run now and rotation) | the Run now log |
| **AS10** | the CLI at a local foreground terminal — **only after the owner's nod (Q3)** | 899 | the manual CLI sync works |
| lab | MA-917 (dev Mac, test profile: L-A1 and L-A9 → the macOS start entry; L-A2..L-A4, L-A6, L-A8; L-A5 → AS-Q) [C-67]; MA-918 (Windows; **set-up starts now, in parallel with AS1a**; first-release scope → the Windows start entry; AS-Q scope) [C-49, C-59]; L-A10 (the spike, before AS4's Windows launch code) [C-57] | | |

Order: AS0 → AS1a → AS1b → AS2 → AS3 → AS4 → AS7 → AS5 → AS6. AS8 after AS7 (its record pin) and
after OQ-8. AS-Q on a platform once its L-A5 record exists (any time after AS4). AS9 after L-A2 and
macOS's AS-Q. AS-L any time after AS3, or never. AS10 last. The labs run in parallel from AS1a.

---

## 13. Lab items (a real Mac or Windows and a real Thunderbird; a test profile, the owner's profile only with the owner's OK and never as a gate [C-67])

- **L-A1** `open -g -j` while the person is typing in another app: Thunderbird stays out of sight and
  does not take the keyboard; a click on Run now does reset the idle reading (T1's mechanism). **With
  L-A9, its record sets macOS's `AttendedStartAllowed` entry** [C-49]. Dev Mac, test profile [C-67].
- **L-A2** The first quit from a Desktop Run now: does macOS ask, does `osascript` wait for the answer,
  does one Allow also cover the scheduled run-job — and **[C-6] if the request is held past its 20 s
  bound and our Thunderbird is quit and another opened meanwhile, does the late answer reach the new
  one, or fail with error -600?** **Added in rev 4:** (a) **[C-62]** which program macOS names in its
  question when the Desktop was started from the `.app` and when it was started from Terminal
  (`cmd/mailarchive/noarg_darwin.go` tells the two apart), and whether the permission survives an
  update to a new, unsigned build (`packaging/macos/build-app.sh:44-48` notes the unsigned-app
  warning; the original L-M8(d) asked this and no answer was found); (b) **[C-78]** kill `osascript`
  while the permission question is on screen, then click Allow: does Thunderbird still quit? If it
  does, that is an R31/R32 residual, stated in §2.6 and said in the Lock reply and the record.
- **L-A3** Lock during the wait: time from Lock to the run's end (against §3.2's bound); Thunderbird
  left open; the next Run now announces it.
- **L-A4** Cancel during the wait: under never-close, Thunderbird left open; from AS-Q, one quit
  request, and Thunderbird closes, or asks about a draft.
- **L-A5** The person opens our hidden Thunderbird from the Dock (Windows: taskbar) during the wait and
  starts a message: at quiet the quit is asked; does Thunderbird ask before discarding it, after an
  AppleScript quit and after `WM_CLOSE`? **The gate for slice AS-Q on each platform, not for AS4
  [OQ-1, C-48]:** if Thunderbird asks, that platform's `AttendedQuitAllowed` entry is set true in
  AS-Q's commit citing the record; if it does not ask, the entry stays false and attended Run now
  there never closes Thunderbird, leaving it open and saying so (§2.8). Also covers a Cancel during
  the wait, which asks for the same close. **Windows [C-60]:** log the window set at quit time
  (`labLogWindows`) and whether `WM_CLOSE` was posted; the entry may be set true only if the post
  happened while a draft existed and Thunderbird then asked.
- **L-A6** `mailarchive.log` after a scheduled run with the sync: every line timed.
- **L-A7 [C-47, reworked by OQ-2]** Windows: the start while the person types in another app: how long
  the window shows (the owner accepted a brief flash), and whether it takes the keyboard (not
  accepted), measured as MA-918's method says [C-59]. Part of MA-918's first-release scope, the
  Windows **ship-gate test**; no longer a gate on building the feature.
- **L-A8 (new) [C-68]** A hidden start with a **Primary Password** set, and (if an account is
  available) with an **expired OAuth sign-in**, on both platforms: does any watched file change, what
  outcome results, and does the Dock icon bounce (macOS) or the window show (Windows)? It informs the
  no-change wording and OQ-5; it is not a gate (the Primary Password case on Windows is also in
  MA-918's slow starts).
- **L-A9 (new) [C-70]** On a profile with Local Folders and at least two accounts (one POP if one is
  available), run **the product's own digest** (`digestWatch`, not an INBOX-size watcher) every 10 s
  from the hidden start and log every file that changes before Thunderbird's first periodic fetch.
  If anything changes before that fetch (an `.msf` summary, `msgFilterRules.dat`, a POP account's
  store, Local Folders' Outbox or Trash summary), the first-change gate can end the wait at about 4
  minutes, before the mail arrives, and report "synced". **Its record, with L-A1's, sets macOS's
  `AttendedStartAllowed` entry**; if it finds early touches, the entry stays false until the
  first-change gate counts only message-store growth (an mbox growing, or a new file in a maildir),
  a change that goes back through the pre-sync design's quiet-rule review first. MA-918 logs the
  same list on Windows.
- **L-A10 (new, a short spike) [C-57]** Parallels: start Thunderbird once with `SW_SHOWMINNOACTIVE`
  (a throwaway `CreateProcess` call in a lab-only test) and record whether it starts minimized and
  without activation. Before AS4 writes any Windows launch code; decides whether MA-923 is built.

---

## 14. Gates owed

1. **The pre-code review is filed** (`docs/review-presync-attended-predesign.md`, GO_WITH_CONDITIONS,
   two rounds). The passes OD-1 asked for ran in the second round (lens 2, lens 9, the rescue pass,
   and the fold check of rev 3). **Owed before AS1 (the review's round-2 verdict):** a
   second reader checks each fold C-48..C-86 against its condition (a fold check only; no new lens is
   owed). Lenses 1, 5 and 10 were covered in part by round 1 and in part by round 2's scope and
   rescue passes; the review records that the operator did not ask for more (OD-1's reach, §15.4).
2. **Adversarial pass** after AS1a (the abandoned session, the once-both latch, the not-posted
   sentinel), after AS4 (process control, causes, the Windows start and minimize, the CreateProcess
   path if built, the slot binding), after AS8 (Lock from the click to read start, lease, the route,
   the byte pin) and after AS-Q (the keep-open route, the attended quit, C-73's ordering).
3. **Friction review** of the phase notes, the waiting note's delay sentence, the Cancel and Lock
   replies, the first-attended-sync card line, the start-not-allowed variants, the no-change lines,
   the encrypted box, the Status line, WARN remedies (and the no-change WARN if OQ-5 is yes), the
   email and checkbox, the Windows taskbar and notification-area wording, the AS4 Mac note, and every
   corrected text (after AS4, AS8, AS9, AS-Q).
4. **Labs, as per-platform switches, not per-platform releases** (one tag builds every platform)
   [C-48, C-49]: the macOS `AttendedStartAllowed` entry after the L-A1/L-A9 record (dev Mac, test
   profile, MA-917) [C-67, C-70]; the Windows `AttendedStartAllowed` entry after MA-918's
   first-release scope; L-A10 before AS4's Windows launch code; L-A5 on a platform before AS-Q flips
   that platform's `AttendedQuitAllowed` entry; MA-918 again before any release changing the Windows
   start, minimize or close code (MA-927's hash pin) [C-66]. **No L-A5 gate on AS4** [C-48]. If
   MA-918 has not passed when AS4 is otherwise ready, the release question goes to the owner (OQ-7).
5. **Owner, before the slices they touch:** OQ-5 before AS5's no-change part; OQ-6 before AS4's
   waiting note is final (AS4 can ship without the button); OQ-7 at release time if the Windows lab
   is late; **OQ-8 before AS8 is built** [C-85].
6. **Operator**: OD-2, OD-3, OD-4 accepted 2026-10-30; OD-1 answered by the round-2 passes (gate 1).

---

## 15. Questions: answered, and what is still open

### 15.1 Owner answers (2026-10-30)

- **OQ-1 (C-46): yes to both.** The Run now note shows a "Keep Thunderbird open" button while it
  waits (§2.8, MA-919); and if lab L-A5 shows Thunderbird would throw away a message being written
  without asking, Run now never closes Thunderbird on that platform — it leaves it open and says so
  (§2.8, A12, MA-922). Folded in §1.2, §2.1, §2.3, §2.4 (A2, A4, A8, A10, A12), §2.5, §2.8, §3.1,
  §4.6, §5.2, §7, §11, §12, §13. **Rev 4 [C-48]:** still met, in a different order: every platform
  starts never-close in AS4, and the button arrives with the first close, in AS-Q.
- **OQ-2 (C-47): Windows too, now.** Attended Run now on Windows also starts Thunderbird, minimized,
  accepting the brief window flash. This **overrides C-47's hold**. The Windows lab check of the
  start, minimize and close becomes a ship-gate test (MA-918), not a feature gate, and the wording
  names the taskbar. Folded in §1.2, §2.2, §2.5, §2.7, §3.1, §3.4, §4.6, §6, §7, §9, §11, §12, §13,
  §14. **Rev 4 [C-49]:** the feature is still built for Windows from AS4; a per-platform switch lets
  one cross-platform release carry the macOS fix even if the Windows lab is late, and whether to wait
  is put to the owner (OQ-7), not decided here. The "brief flash" is corrected: the window can stay
  longer on a slow start (§1.2) [C-56].
- **OQ-3 (C-10): yes.** Lock cancels an encrypted Run now any time before mail reading starts (from
  the click to the read); after reading starts, the Lock reply says the backup finishes with its own
  key and Cancel stops it. Built for a Run now whose job carries the sync, the question's scope; the
  wider reading is OQ-8. Folded in §1.2, §3.2, §6, §7, §8.1, §9, §11, §12. **Rev 4 [C-76]:** "before
  mail reading starts" now really means right before the source loop, not the hook's return.
- **OQ-4 (C-17): accepted.** The plaintext start and finish times beside an encrypted archive
  include the Thunderbird wait, and the unlocked archive's index stays in memory through the wait.
  Both stay written as accepted residuals (§3.2, §4.4).

### 15.2 Operator calls (2026-10-30)

- **OD-1: the gate is not reduced.** Lenses 2 and 9 ran as their own passes in round 2, with the
  rescue pass and a fold check (§14 gate 1).
- **OD-2: accepted.** R38, S64, S65; the S54 and X16 wording; the phase-3 §3.2 "a Lock does not stop
  a Run now" rule amended as OQ-3 says (§3.2 gives the text; it is written into the phase-3 design at
  AS8); catalog rows slice by slice (C-42).
- **OD-3: accepted as a stated residual.** For an encrypted archive, `mailarchive status` shows the
  sync setting but not the last result (§4.7, X16 amendment).
- **OD-4: accepted.** In attended mode a profile named in a job file must also be in `profiles.ini`
  (§2.1).

### 15.3 New owner questions (rev 4) — each with the design's recommendation

- **OQ-5 [C-68]. When Thunderbird starts but nothing arrives in 20 minutes, should scheduled backups
  warn after 3 days of only that?** It can mean no new mail, or that Thunderbird is stuck on a
  password or sign-in window nobody can see (the most likely lasting failure). Today such runs count
  as "synced", so the 3-day warning never fires for it. **Recommended: yes**, as a separate, softer
  warning ("Thunderbird was started but fetched nothing since DATE; open it and check it is not
  waiting for a password"), not as "not synced", so a quiet mailbox is not reported as a failure.
  Until answered, no-change runs count as synced and nothing new is built (AS5's no-change part
  waits). Either way, the note and the tag already stop calling such a run "synced" (no ruling
  needed for that wording).
- **OQ-6 [C-69]. Add a "Stop waiting and back up now" button to the Run now note while it waits?**
  Thunderbird usually fetches about 10 minutes after it starts, so a Run now waits 13 to 20 minutes;
  the only way out today is Cancel, which skips the whole backup. The button would end the wait and
  back up what has arrived (Thunderbird left open; counted neither synced nor missed). **Recommended:
  yes**, in AS4 if answered in time, else a later slice. Either way the note now explains the delay
  and the Cancel tooltip says Cancel skips the backup.
- **OQ-7 [C-49]. If the Windows lab test is not done when the Mac part is ready, should the release
  wait for it, or ship with Run now's Thunderbird start switched on for the Mac only?** One release
  builds both platforms. **Recommended: ship the Mac fix**; Windows Run now keeps today's behaviour
  (no sync, and now says so honestly) until its lab passes, then switches on in the next release. Not
  needed now; asked early so the release does not stall.
- **OQ-8 [C-85]. Should a Lock cancel every encrypted Run now that has not started reading mail, or
  only those that sync Thunderbird first?** Your answer to OQ-3 said "an encrypted Run now"; the
  question had asked about one with the sync, so only those are built. **Recommended: every
  encrypted Run now** — that is what you said, it gives one rule to remember, and the mechanism is
  the same. It widens phase 3's rule for Run nows without the sync. Needed before AS8.

### 15.4 Still open, not owner questions

- **OD-1's reach (operator).** Round 2 covered lens 2 and lens 9 fully and parts of lenses 1, 5 and
  10 through the scope and rescue passes; whether the operator wants full passes for lenses 1, 5 and
  10 is still the operator's call.
- **A Windows focus steal.** The owner accepted a brief window flash, not Thunderbird taking the
  keyboard; if MA-918 finds a focus steal, it goes back to the owner before the Windows entry is set
  (§2.7). So does a measured flash much longer than "brief" [C-56].
- **The phase-3 amendment's wording** follows OQ-8's answer (§3.2).

### 15.5 Earlier questions (each has a default this design builds)

- **Q1** An already-open Thunderbird counts as a sync. Default **yes**.
- **Q2** Lock: key first, Thunderbird left open — **settled** by ruling 4 and the review.
- **Q3** The CLI at a local foreground terminal also attended (AS10). Default **yes, last slice**.
- **Q4** Write the encrypted outcome into the sealed run details (A24) later. Default **not now**.
- **Q5** The sync email under the failure option, with the relabelled checkbox, no "working again"
  email. Default **yes**.
- **Q6** An attended Run now closing a leftover an earlier backup provably started. Default **no**.

## 16. Not checked

- No test was run; every "proven" is by reading.
- macOS's Automation prompt, delayed delivery and the asking program (L-A2), `open -g -j` while
  typing (L-A1), Thunderbird's quit prompt (L-A5), a hidden Primary Password or OAuth window (L-A8),
  start-up file touches before the first fetch (L-A9), Windows focus and how long the window shows
  (L-A7): not checked.
- Whether Thunderbird honours `SW_SHOWMINNOACTIVE` (L-A10): not checked.
- Thunderbird's Windows "minimize to the notification area" option is from memory, not checked
  (§2.7) [C-61]; which Windows terminal hosts run their children in a job that forbids breakaway is
  inferred (§2.7) [C-65].
- What the Parallels VM has installed now (§2.7): MA-570's 2026-10-23 record says Thunderbird was
  driven there; not re-checked for rev 4.
- The rev-4 folds (C-48..C-86) have not been re-read by a second reader (§14 gate 1).
- `env_windows.go` was still not read in full; `winEnv.quit` itself (`quit_windows.go:129-161`) and
  `postClose` (`:226-232`) were read in rev 4.
- `container.Store`'s copies of key material after `vault.Key.Destroy` — not read; §3.2's "key
  destroyed" is about the lease's key.
- `encryption_create.go` beyond its call site; `alert.go` beyond `ShouldSend`; how an archive switch
  treats the capture state today.
- `probesOnPath` in §2.6 is an estimate to be counted at AS1a.
