# Design — the attended Thunderbird sync: Run now really syncs, skips are told, encrypted archives can sync

**Revision:** 2 (2026-10-30), folding the pre-code review
(`docs/review-presync-attended-predesign.md`, reviewed rev 1 sha256 `107c75ba…a012`). Every fold is
marked `[C-n]` with the review's condition id. **Register:** a draft design. Nothing is built, and
no test or catalog row changes here. Branch `presync-attended`, off `main` at `ea6515a` (v1.5.1
shipped).

**Rev-2 status.** The folds have **not** been re-verified by a second reader; the review's
verdicts on the changed sections are stale until they are (process step 5). Four owner rulings
(OQ-1..OQ-4) and four operator acceptances (OD-1..OD-4) are open; §15 lists them, and each section
they touch says which default it builds and what changes if the answer is no.

**Status of every id proposed here.** `MA-895` to `MA-921`, `R38`, `S64`, `S65` and the X16
amendment are **provisional**. `MA-880..MA-889` are reserved for the settings-card part B and are not
used. `S61..S63` are claimed by `docs/design-archive-manager.md:508-510`, so the new scenarios start
at `S64`. `R38` was found unused on 2026-10-30 (`grep R38 docs/scenario-catalog.md`, no match;
re-check at AS0). **How the ids land [C-42]:** AS0 adds only R38, S64, S65 and the S54 and X16
amendments; each MA row lands **in the slice that adds its test**, so the covers-map meta-test
(`assureblock/covers_map_test.go:86-97`, which fails any U/S row with no covering test) stays green
throughout. If an R row with no MA row yet fails that meta-test, R38 lands with AS1's first row
instead (to check at AS0).

**Claim labels.** **proven** = read in the code at the `file:line` given, on this branch (by
reading, not by running: no test was run for this design). **inferred** = reasoning from the code or
from how macOS, Windows and Thunderbird behave, not verified by a run; each inferred behaviour of a
real system is also a **lab item** (L-A1..L-A7, §13). **decision** = a choice this design makes,
with its reason; the owner or a reviewer may overturn it.

**Gates before code.** The 10-lens pre-code review is filed (GO_WITH_CONDITIONS). Before AS1: the
fold re-verification, OD-1 (the review ran five finders, not ten lenses, and no rescue pass), and
the owner answers that AS1 depends on (none of OQ-1..OQ-4 blocks AS1; OQ-1 and OQ-2 block AS4's
ship, OQ-3 and OQ-4 block AS8).

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
   **On Windows this cannot be met as written [C-47]:** Thunderbird ignores the hidden-start flag, so
   its window appears and is minimized only once the session has proved it ours (up to about 17 s:
   a 2 s poll then up to 15 s `minimizeWait`; `quit_windows.go:62, 71-76, 106-118`, proven). See
   §2.7 and owner question OQ-2.
3. **Scheduled skips:** a WARN after **3 days with no successful sync**, measured as elapsed time like
   the source-unreachable warning; a Status WARN plus an alert email under the existing alert rules,
   once per streak.
4. **Encrypted archive:** a Lock while a Run now **waits for Thunderbird cancels that backup and
   clears the key**.

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
- No new Thunderbird settings (`check_time` untouched).

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
    // Phase reports "starting", "waiting", "closing", "cancelling" as the session moves [C-19].
    Phase func(phase string, since time.Time)
    // KeepOpen, if set and true when the quit is due, skips the quit (C-46, pending OQ-1).
    KeepOpen func() bool
}
```

`deps` (`session.go:14-29`) gains `attended`, `stop`, `phase`, `keepOpen`. `buildDeps`
(`run.go:57-82`) copies them. **Attended and a named profile [C-18]:** when `Attended` and
`ProfileNamed` are both set, `buildDeps` also requires the profile to be listed in `profiles.ini`
(today a job-named profile skips that check, `hook.go:32`, proven); not listed → `profile-unusable`.
Nothing in the product writes a job's `Profile`, so only a hand-made file is affected (a tightening
of rev-3 PC3 for attended runs only; operator acceptance OD-4).

### 2.2 Who sets it (decision)

| Caller | Attended? | Why |
|---|---|---|
| Desktop Run now, plaintext (`schedule.go:668-670` → `wizard.RunJobInProcess`) | **yes, from slice AS4** [C-44]; on Windows only after lab L-A7 passes [C-47] | ruling 1 |
| Desktop Run now, encrypted (`schedule.go:700-715` → `wizard.RunJobInProcessKeyed`) | **yes, from AS8**; same Windows gate | ruling 1 |
| A new encrypted archive's first backup (`encryption_create.go:175`) | yes, but inert | its job is written without the sync (`addkeyed.go:71-81`, `wizard.go:1862-1878`, proven) |
| Scheduled run-job (`wizard.RunHeadless`, `wizard.go:199-330`) | **never** | no one is there |
| Command line export with `-unattended` (every baked schedule line, `cmd/mailarchive/job.go:280`) | **never** | a scheduled run |
| Command line export **without** `-unattended`, standard input a terminal in the foreground, standard error a terminal, **and a local session: no `SSH_CONNECTION` and no `SSH_TTY` in the environment** [C-9] | **yes — slice AS10, owner nod asked (Q3)** | T19. The terminal test is the encrypted prompts' (`cmd/mailarchive/encrypted.go:188-190`). A piped, backgrounded or remote (`ssh -t`) run is not attended. **On Windows** `tty.Foreground` equals `IsTerminal` (`tty_windows.go:22`, proven), so a console run without `-unattended` is attended; every baked scheduled task carries `-unattended`. |

**Proposed R38** (§11): *Thunderbird is started or closed without the owner-activity checks only by a
run the person started and is watching at this computer: Desktop Run now, or the command line at a
local foreground terminal without `-unattended`. No scheduled run, remote session, job file,
descriptor or archive file can turn it on.*

How it travels: `presyncjob.SetHook(opts, HookOptions{Profile, Log, Attended, Surface, GOOS, Stop,
Phase, KeepOpen, OnStart, OnEnd})`. `optsForJob` (`jobrun.go:114-138`) gains a `RunMode` argument and
sets two in-memory `app.Options` fields (no JSON tags): `PresyncWanted` (the job asks for the sync)
and `PresyncAttended` [C-33, C-34]. `RunHeadless` passes the scheduled mode; the two Run nows pass the
attended one. The command line moves onto `presyncjob.SetHook` (T20).

**The test seam [C-41].** `presyncjob` gains an **untagged** file holding
`var runSession = presync.Run` and `var supported = false`; the darwin and windows files set
`supported = true`. `SetHook` itself becomes untagged and installs nothing when `!supported`. Wizard
and desktop tests set `supported = true` and replace `runSession` with a scripted fake that (1)
blocks until its context or `Stop` ends, (2) records `context.Cause(ctx)` and whether `Stop` ended,
(3) calls `Phase`, `OnStart`, `OnEnd`, and (4) returns a chosen outcome. Thunderbird is the external
binary, so this is the mock CLAUDE.md allows. **Quit counts are proven only in `presync`'s own
session tests** (the existing `fakeEnv`); desktop and wizard rows prove the cause delivered and the
side effects.

### 2.3 What attended skips and what it keeps

| Check | Where | Attended | Why |
|---|---|---|---|
| Owner idle ≥ 2 min before the start | S4, `session.go:113-116` | **skipped** | ruling 1 |
| Owner idle ≥ 10 min before the quit | S8, `session.go:226-235` | **skipped** | ruling 1 |
| Profile lock, one try, no wait | S2, `session.go:73-77` | kept | |
| Recovery from the state record (a leftover is announced, never quit) | S3, `session.go:99-109` | kept | R31 |
| Thunderbird already running | S4, `session.go:117-120` | kept | ruling 1 |
| Profile path re-check, running re-check, late snapshot before launch | S5, `session.go:124-139` | kept | swap guard, C5 |
| Cancellation check right before the write-ahead record and the launch | S5 (new) [C-10] | **new, both modes** | a cancel that came first starts nothing |
| Write-ahead record **saved** before the start | S5, `session.go:140-142` | kept, **now required** [C-8] | P5 |
| Ownership proven after launch | S6, `session.go:153-171` | kept | R31 |
| Await-up 90 s, maximum wait 20 min, quiet 3 min | S6, S7 | kept | R32 |
| The shared pre-quit function (ownership re-check, C3 count, record `quitting`, one request) | S8 and every cancel quit [C-1] | kept, **one function for every quit** | R31 |
| One quit request **bounded by 20 s** (`quitTimeout`), then the 2-minute exit grace | S8 | kept unchanged in attended mode [C-6] | R31, R32 |
| Elevated refusal, app and profile resolution | S1, adapters | kept; attended also requires `profiles.ini` membership for a named profile [C-18] | |
| The whole-session watchdog | `run.go:23-50` | kept, **computed and leak-free** (§2.6) [C-2, C-3] | R32 |

### 2.4 State machine changes against rev 3 (each stated)

| # | State | Rev 3 / as built | Rev 2 here |
|---|---|---|---|
| A1 | S4 | idle under 2 min → END `owner-active-skipped` (a note) | **unless `attended`**: the idle check is not read at all (no `idleSeconds` call). |
| A2 | S8 | idle under 10 min → leave open, END `owner-active` | **unless `attended`**: then straight to the shared pre-quit function (A10). |
| A3 | S8 | request bounded by `quitTimeout` 20 s (`env_darwin.go:109, 428`), exit grace 2 min (`api.go:51`) | **unchanged in both modes [C-6].** Rev 1's 2-minute attended request is withdrawn: while a quit request waits on macOS's permission question, the person can quit our Thunderbird and open their own, and a quit addressed by bundle id could then reach theirs (**inferred**; lab L-A2 adds the delayed-delivery case). A request that times out is mapped as today (quit not answered) and leaves Thunderbird open, recorded `left-running`; its desktop remedy (§4.6): "macOS asked whether Mail Archive may control Thunderbird. Choose Allow, quit Thunderbird from the Dock, then click Run now again." Revisit only if L-A2 proves a delayed request fails with error -600 once its process has gone. |
| A4 | S6, S7 | cancel → record `launching` (S6) or `left-running` (S7), END `cancelled` | By cause: **(i) `Stop` ended (a Lock, Shut down) or any cause other than `ErrCancelAndQuit` (Ctrl-C, a scheduled stop):** quit nothing; record as built; `Left` = `left-open` (S7) or `may-be-starting` (S6). **(ii) cause `ErrCancelAndQuit` (the Cancel button), attended only:** in S7, take the shared pre-quit function (A10) and wait at most 15 s for the exit; gone → S9 clears the record, `Left` = `closed`; still there → `left-running`, `Left` = `left-open`. In S6 [C-7], keep polling for the proven child up to the 90 s await limit on a short context of its own (which `Stop` also ends), then A10 as in S7; no child proven → record that, `Left` = `may-be-starting`, line "Thunderbird may still be starting; quit it from the Dock/taskbar". **Scheduled runs: (i) only, unchanged.** |
| A5 | all waits | `e.sleep(d)` blocks | `sleepCtx(ctx, stop, d)` in an **untagged** file, used by both adapters [C-2/T4]; returns at once when `ctx`, `Stop` or the session context ends (T17). The fake's `sleep` returns without moving the clock when its context is already done, so a session test cannot pass by accident. |
| A6 | S3 | an unreadable record → warn, continue | built (B6): `sessionResult` gains `side []Outcome`; a proceeding recovery decision appends its outcome there **only when it is warn-class** [C-45] (so `matchOther`'s proceeding `already-running`, `recovery.go:72-75`, adds nothing); `presyncjob` maps every side outcome to a run warning. |
| A7 | S7 | progress line — not built | built for both modes: once a minute, "Thunderbird sync: still waiting for mail to arrive (N min so far, up to 20)", N measured from the start of the wait for mail. |
| A8 | END | outcome only | `Result` gains `Side []Outcome`, `Waited` (from "up" to the quit or the verdict), `Started`, and `Left` (`none`, `closed`, `left-open`, `left-open-by-choice`, `may-be-starting`) [C-23]; `Outcome` gains `Synced() bool` (§5.2). |
| A9 | S5 | the `launching` record save is best effort (T22) | **both modes [C-8]:** launch only after the record is saved to disk; a failed save ends `cannot-start` (a warning) and launches nothing. A failed save of `quitting` or `left-running` is a side warning `presync_state_unsaved`. |
| A10 | S8, A4 | the pre-quit steps live inline in S8 | **one function, `preQuit(sctx)`, used by S8, A4(ii) and the S6 cancel path [C-1]:** (1) if the session is abandoned (§2.6), stop: quit nothing; (2) re-verify ownership; (3) C3 on macOS: count the **raw pids `pgrep` lists**, and count any pid whose details cannot be read as a second Thunderbird [C-1/SAF-9]; more than one → no quit, `quit-skipped-others`, `left-running`; (4) if `KeepOpen()` is true (C-46, pending OQ-1): no quit, clear the record, `Left` = `left-open-by-choice`; (5) save `quitting`; (6) one request, `env.quit(qctx, target)`, where `qctx` derives from the session context `sctx` (§2.6) with the 20 s deadline [C-5]. |
| A11 | S5 | no cancellation check | **both modes [C-10]:** `ctx.Err()` and `Stop` are checked right before saving `launching` and again right before `startHidden`; either done → END `cancelled`, nothing launched, no `launching` record. |

**The quit seam [C-5].** `env.quit(target proc)` (`env.go:78`) becomes `env.quit(ctx, target)`. The
session builds the deadline; `macEnv.quit` runs `osascript` under the context it is given, not
`context.Background()` (`env_darwin.go:428` today). `winEnv.quit` sends `WM_CLOSE`, which returns at
once, so the context bounds nothing there (stated; not read in full, see §16). The fake records
`ctx.Err()` at the call and the deadline.

### 2.5 The invariants, amendment by amendment

- **R30: unchanged.** `Attended` does nothing without the job's `thunderbird_sync`.
- **R31: unchanged in its text.** **Stated residual, widened by ruling 1:** rev 3 used the S8 idle
  check to soften (a) the person opening our hidden Thunderbird during the wait and working in it,
  and (b) macOS addressing the quit by bundle id. In attended mode, at quiet, our Thunderbird is
  asked to quit. Mitigations in rev 2: the shared pre-quit function (C3 by raw pids) for (b) [C-1];
  the 20 s request bound [C-6]; the proposed "Keep Thunderbird open" button and lab L-A5 as a ship
  gate for (a) [C-46, OQ-1]. Whether Thunderbird asks before discarding a message being written is
  **inferred** and is not promised in any text until L-A5 passes [C-30].
- **R32: unchanged; one stop added inside it.** A cancellation during or before the hook stops the
  run before any source is read (T16, MA-902). Every wait is bounded, and the bound is now computed
  and tested (§2.6).
- **S54:** "starts Thunderbird only when none is running and the owner is away" → "… and the owner
  is away, **or the owner asked for it at this computer (an attended run, R38)**".
- **New R38, S64, S65** — §11.

### 2.6 The watchdog: computed, and an abandoned session can do no harm [C-2, C-3]

- **Computed.** `sessionBudget()` becomes a pure function in an untagged file:
  `awaitUp + awaitPoll + maxWait + pollEvery + probeTimeout × probesOnPath + quitTimeout + quitGrace
  + exitPoll + slack`, with `slack` = 60 s and `probesOnPath` counted from the code at AS1 (estimated
  9: four before S5, three in S5, two in the pre-quit function). With today's constants that is about
  90 + 2 + 1200 + 10 + 45 + 20 + 120 + 2 + 60 ≈ 1549 s (**inferred** arithmetic; the S7 folder walk's
  own time is what the watchdog exists to bound, so the slack covers a normal walk only). It is the
  same in both modes now that the quit request keeps its 20 s bound (A3). MA-921 asserts that the
  longest path, the S7 Cancel-quit path (cancel at the last S7 poll, then A10 and the 15 s exit wait)
  and the S6 Cancel path (90 s await, then A10, then 15 s) each fit, from the same constants, so a
  constant changed later re-checks itself. **This also fixes T21 for scheduled runs.**
- **The session context.** The session runs under `sctx`, derived from `Stop` (or `Background`), which
  the watchdog cancels when it abandons the session. Every wait, probe and quit request in the session
  watches `sctx`.
- **When the watchdog returns.** It returns (1) when the session returns; (2) at once when `Stop` ends
  or `ctx` ends with a cause other than `ErrCancelAndQuit` — result `cancelled`, `Left` from the phase;
  (3) when the budget runs out — `internal-error`. On (2) and (3) it **marks the session abandoned and
  cancels `sctx`**. With cause `ErrCancelAndQuit` it waits for the session's bounded Cancel quit (A4(ii),
  at most about 90 + 10 + 20 + 15 s in S6) unless `Stop` ends first.
- **What an abandoned session may still do.** It checks the abandoned flag in `preQuit` before saving
  `quitting` and before any request, and any request already in flight dies with `sctx` (`osascript`
  is killed). So **no quit is ever sent after the watchdog returns**, and Thunderbird's on-exit Trash
  emptying cannot start under the reader by our hand. When the goroutine finally returns, it saves
  `left-running` (or leaves `launching`) and **releases its own profile lock**, so the long-lived
  Desktop does not keep it.
- **A stuck lock cannot hide the streak [C-4].** Three `profile-busy` results in a row on scheduled
  runs count as one miss (§5.2).

### 2.7 Windows [C-47, pending OQ-2]

Ruling 2 is met on macOS only. On Windows the window shows and is minimized after adoption (§1.2).
Before this design the 2-minute idle check meant nobody was typing then; in attended mode the person
is typing by definition, and if Thunderbird took the keyboard, its one-key shortcuts (Delete, A, J)
would act on the selected message (**inferred**). So:

- Lab **L-A7** tries a start that shows minimized without taking focus (the `SW_SHOWMINNOACTIVE`
  start flag) and checks, with the person typing in another app, that Thunderbird does not take the
  keyboard. MA-918 and L-A1 assert the same.
- **Until L-A7 passes, a Windows Run now passes `Attended: false`** (today's behaviour: it skips while
  the person is at the computer), and its note says so plainly in the outcome line. Attended mode on
  Windows ships only after L-A7 passes and the owner nods (OQ-2).

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
- The cancel line is chosen by phase [C-23]: still starting → "Cancelled. Thunderbird may still be
  starting; quit it from the Dock/taskbar."; waiting → "Cancelled. Thunderbird, started by this backup,
  is being asked to close; the archive is unchanged."; reading → today's words (`jobcontrol.go:39`).
  The end line then comes from `OutcomeLine`, which reads `Left`, so it says whether Thunderbird
  closed.

### 3.2 Lock while an encrypted Run now waits (ruling 4)

Today a Run now takes its own write lease at the click, so a Lock does not stop it
(`schedule.go:677-684`; phase 3 §3.2). This design **amends that rule for one window** (operator
acceptance OD-2).

- **The window [C-10, pending OQ-3].** For a job that carries `thunderbird_sync` only: **proposed
  default**, from the moment `runBackupKeyed` takes the lease until the hook returns — so it covers the
  heal, the preflight, discovery and `openEncrypted` (which decrypts the whole manifest, `app/run.go:548`,
  and can take minutes on a large archive). Without it, a Lock in that span (the natural "click Run
  now, then Lock, then walk away") finds no window, and Thunderbird then starts with the key held. If
  the owner answers no to OQ-3, the window is the hook only (rev 1), and A11 still guarantees that a
  run cancelled before S5 starts nothing.
- **Atomic open and close [C-11].** The Desktop records `syncWait{key, stop}` under `c.mu`.
  `stopSyncWait(out)` checks the window **and** calls the stops while holding `c.mu`. The hook wrapper
  closes the window, and resets `syncing`, in a `defer` under `c.mu`, so a hook panic (recovered by
  `app.Run`, `app/run.go:582-587`) closes it too.
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
  any write). No quit wait, poll or folder walk is on that path any more. **Before the hook** (only
  with the OQ-3 default): the run stops at the next step that reads its context, and at the latest
  does not call the hook (A11, §4.1); the key goes when the current step (a heal or the decrypting
  open) ends, which on a large archive can be minutes (**inferred**, stated).
- **After the window closes (the read) [C-13].** The run goes on with its own key, as phase 3 says;
  the Lock reply now says so (§9).
- **The race with the hook's return.** Either `stopSyncWait` finds the window open (cancelled, nothing
  read) or closed (the read goes on, and the reply says so). MA-913 drives both through a seam that
  pauses between the window's close and the hook's return.
- **Thunderbird after a Lock.** Left open, recorded `left-running` (or `launching`). The next sync's S3
  announces it. The Lock reply says how to close it, and stays on screen (§9) [C-21]. Q2 (quit first?)
  is settled by ruling 4 and the review: the key goes first.
- **Widened residuals, for the owner's acceptance (OQ-4) [C-17].** (1) The decrypted manifest, units
  and store, opened before the hook, now sit in memory through the wait (up to about 25 minutes), and
  are left to the garbage collector on a cancel; rev 1 called this unchanged, it is widened. The reply
  says "its key is being cleared", never that the decrypted record is. (2) §4.4's timing residual.

### 3.3 What the locks and the lease hold during the wait

- The archive lock is held for the whole wait (rev 3 P4; `app/run.go:571-600`, proven).
- The per-profile lock from S2 through the read (C2); an abandoned session releases it itself (§2.6).
- An encrypted Run now holds its lease, and so the key, through the wait: up to about 25 minutes
  longer than a Run now with no sync. The person's Lock ends it (§3.2). Said in the Settings card's
  encrypted line (§8.1).

### 3.4 Command line at a terminal (slice AS10)

- Attended as §2.2 says. Ctrl-C: A4(i), no quit, and the line "Thunderbird sync: cancelled —
  Thunderbird, started by this run, was left open; quit it from the Dock."

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
    Synced   bool              // §5.2's table
    Attended bool              // read only by recordRun's streak rule (§5.3); never written
    Left     string            // none | closed | left-open | left-open-by-choice | may-be-starting
    Line     string            // the owner-facing line, surface and platform words; no path
    Started  time.Time
    Waited   time.Duration
}
type BeforeReadResult struct { Warnings []state.RunWarning; Release func(); Outcome *PresyncOutcome }
type Result struct { …; Presync *PresyncOutcome }
type Options struct { …; PresyncWanted, PresyncAttended bool } // set by optsForJob; never written
```

- **Before the hook [C-10]:** `app.Run` does not call `BeforeRead` when `ctx` is done; it returns
  cancelled through the normal record path.
- **In the hook block:** the outcome is set on the **record value**, `begun.Presync = br.Outcome`, the
  way `begun.Warnings` is set today, **before** `defer recordRun` is registered (`app/run.go:600`).
  `recordRun` and the streak read it from there, so the later `result = Result{…}` at
  `app/run.go:782` and the early `return Result{}, …` at `app/run.go:622, 635, 695, 705` (all proven)
  can no longer lose it. `Result.Presync` is set again for the caller after line 782 and on every
  early return.
- **The T16 check [C-11]:** placed **after** `defer recordRun` and `defer presyncRelease` are
  registered and **before** the sweep: `ctx.Err() != nil` → return the current result with
  `context.Canceled`, so the record ends `cancelled`, never left `running` (which `health.go:626-627`
  would show RED).
- **Keyed runs [C-16]:** `recordRun` leaves the record's `Presync` nil when `opts.Key != nil`, so no
  refactor of the encrypted marshaller can leak it.
- A recovered panic in the hook sets `Presync{Name: "internal-error", Class: "warn"}` beside today's
  warning.
- `presyncjob` builds the outcome from `presync.Result`: `Line` is
  `presync.OutcomeLine(o, surface, goos, left, waited)` (§4.6).

### 4.2 The last-run record (plaintext)

`state.PresyncRecord` (`lastrun.go:135-142`) is written for the first time: `code`, new `name`, new
`result` (`"synced"`, `"not_synced"`, `"cancelled"`), `since`. **No `attended` field [C-36].**
`warn_streak`, `note_streak`, `state`, `expected_until`: not written (kept so older records read).
**`ReadLastRun` strips control characters from `Presync`'s strings** as it does for Job, Error and
Warnings (`lastrun.go:280-286`, which today skips Presync) [C-37].

### 4.3 The run history (plaintext)

`RunHistoryEntry` gains `Presync string json:"presync,omitempty"` (the name), copied in
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
- **Widened timing residual (OQ-4) [C-17].** The encrypted last-run keeps `Started` and `Finished` in
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
| `ok`, `ok-noactivity`, `watch-truncated` | "Thunderbird synced first" |
| `not-quiet` | "Thunderbird synced (still fetching at 20 min)" |
| `quit-*`, `owner-active`, `identity-unavailable` | "Thunderbird synced, but was left open" |
| `already-running` | "Thunderbird was already open" |
| `owner-active-skipped` | "Thunderbird not synced: you were using the computer" |
| `other-profile-running` | "Thunderbird not synced: open with another profile" |
| `profile-busy` | "Thunderbird not synced: another backup was syncing it" |
| `leftover-found` | "Thunderbird not synced: an earlier copy is still open" |
| `app-not-found` | "Thunderbird not synced: Thunderbird was not found" |
| `refused-elevated` | "Thunderbird not synced: the backup ran as administrator" |
| `profile-unusable` | "Thunderbird not synced: its profile could not be used" |
| `cannot-start`, `did-not-start`, `exited-early`, `successor-found` | "Thunderbird not synced: it could not be started" |
| `no-desktop-session` | "Thunderbird not synced: no one was logged in" |
| `not-set-up` | "Thunderbird not synced: the sync could not be prepared" |
| `internal-error` | "Thunderbird not synced" |
| `cancelled`, absent | no tag |
| any name not in this map | "Thunderbird not synced" — the name itself is **never echoed** |

No "(see Status)" anywhere. Encrypted rows: no tag, judged by the archive kind, not by the field.
MA-905 asserts every name in `allOutcomes` is in the map or deliberately has no tag.

### 4.6 The Run now note: phases, then the outcome [C-19, C-22, C-23, C-24, C-30]

- **Phases.** `Config.Phase` reports `starting` (S5-S6), `waiting` (S7; `since` = the start of the
  wait for mail), `closing` (the pre-quit function and the exit grace), `cancelling` (A4(ii)). The
  Desktop keeps, under `c.mu`, `syncPhase`, `syncSince`, and at the end `syncLine`.
  `runJobOn` resets them **together with** `lines` and `result` (`maintenance.go:34`) [C-15].
- **The note is built on the server** (CI has no JavaScript runtime, so a script-built note cannot be
  tested) [C-19/T6]. `/api/activity` gains `syncPhase` and `syncNote`; while `syncPhase` is set,
  `pollBackup` shows `syncNote` **in place of** the last log line. The texts (desktop words; the
  platform word in braces is "Dock"/"out of sight" on macOS, "taskbar"/"minimized" on Windows) [C-24]:
  - starting: "Starting Thunderbird {out of sight}…" On the first attended sync on this computer (a
    marker in the pre-sync state folder) it adds: "Run now starts Thunderbird even while you are
    using the computer, and closes it when its mail settles, which runs Thunderbird's on-exit actions
    (for example emptying Trash)." [C-32]
  - waiting: "Syncing Thunderbird… waiting for mail to arrive (N min so far, up to 20). Thunderbird is
    running {out of sight}; it is closed when its mail settles." N is whole minutes from the wait's
    start and never shown above 20. The clause about Thunderbird asking before discarding a message
    being written is added **only after lab L-A5 passes** [C-30]. With OQ-1's default, a "Keep
    Thunderbird open" button shows here [C-46].
  - closing, macOS: "Closing Thunderbird… If macOS asks whether Mail Archive may control Thunderbird,
    click Allow." Windows: "Closing Thunderbird…"
  - cancelling: "Cancelling… Thunderbird, started by this backup, is being asked to close."
  - end: `syncLine` then the result, or `syncLine` then "Backup failed: …", or `syncLine` then the
    cancel words of §3.1. A failed run no longer hides the sync's line.
- **Reload [C-22].** On page load, if the running job is a Run now, the page calls `pollBackup` with
  Run now disabled, so the phase note and Cancel show beside Run now again (today only the capture
  poll restarts, `dashboard.go:2083`, and labels errors "Capture failed:", `dashboard.go:2073`).
- **Lock or switch [C-15].** A Lock of an archive, and a switch away from it, clear that encrypted
  job's `lines`, `result` and sync fields, and mark the slot so later lines of that run are not kept.
- **`OutcomeLine(o, surface, goos, left, waited)`**, new, past tense, every outcome has a line (a new
  outcome fails MA-897 until it has one). Examples, desktop, macOS: "Thunderbird synced: it was
  started out of sight, its mail settled after 6 min, and it was closed." · "Thunderbird was already
  open, so it was not started or closed; the backup read the mail already on this computer." ·
  `Left=left-open`: "Thunderbird sync: cancelled — Thunderbird, started by this backup, was left open
  out of sight. Quit it from the Dock before the next Run now." · `Left=may-be-starting`: "…
  Thunderbird may still be starting; quit it from the Dock." · Windows Run now before L-A7 (§2.7):
  "Thunderbird was not synced: you were using the computer. On Windows, Run now does not yet start
  Thunderbird while you are using it."
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
  archives". That last clause is an X16 exception for the operator (OD-3).
- `status -json` gains an additive `thunderbird_sync.last` (plaintext) and `thunderbird_sync.enabled`;
  `JSONVersion` stays 2.

### 4.8 `mailarchive.log` timestamps, and a lasting Run now log [C-14]

- `optsForJob` takes the run's `*log.Logger`; the pre-sync's `Log` is a line writer calling
  `logger.Print` per line, so every line is dated (T8). The invalid-since warning (`jobrun.go:124`)
  uses `logger.Printf`.
- **Plaintext Run now** appends to `<out>/mailarchive.log` through `runlog.Open` with the first line
  "Run now from MailArchive Desktop" — **opened only after** `job.Read`, `RequireExistingOut`,
  `seal.RefuseWrite` and `app.RefuseKeyless` have passed, in `RunHeadless`'s order
  (`wizard.go:222-235`), so a plaintext-path Run now on an archive that is really encrypted (a
  misjudged kind at the click, `schedule.go:664`) writes nothing (`runlog.Open` creates 0644 and has no
  encrypted check, `runlog.go:32`, proven).
- **Stated residuals (M5):** the log opens before `app.Run` takes the archive lock, so a Run now then
  refused by that lock leaves a header and its refusal in the log (accepted: true and harmless); a
  size rotation (a rename) can race a scheduled run's append, so a few lines may land in `.1`.
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
    Busy        int       // scheduled profile-busy results in a row [C-4]
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
| `ok`, `ok-noactivity`, `watch-truncated`, `not-quiet`, `quit-not-permitted`, `quit-timeout`, `quit-skipped-others`, `owner-active`, `identity-unavailable`, and `Left=left-open-by-choice` | **synced** |
| `already-running` | **synced** (decision; Q1) |
| `profile-busy` | **neither**, except that the third in a row on scheduled runs counts as one **missed** and resets `Busy` [C-4] |
| `cancelled`; no session because the sync is off, the run was refused or failed before the hook, or the archive is paused | **neither** |
| `state-unreadable` | never the main outcome (side only, A6), so never counted [C-45] |
| `leftover-found` | **missed** (decision [C-20/M3]): that copy may be fetching mail, but it is hidden and stuck; counting it missed is what makes the owner hear of it, and its remedy says to quit it |
| everything else (`owner-active-skipped`, `other-profile-running`, `exited-early`, `successor-found`, `cannot-start`, `no-desktop-session`, `did-not-start`, `profile-unusable`, `app-not-found`, `refused-elevated`, `internal-error`, and `not-set-up` [C-34]) | **missed** |

`Outcome.Synced()` implements the synced rows; MA-903 checks it for every entry of `allOutcomes`.

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
| `leftover-found` | "A Thunderbird an earlier backup started is still open {out of sight}. Quit it from the {Dock}, then click Run now." |
| `other-profile-running` | "Thunderbird was open with another profile at backup time. Quit it before the backup time, or leave this profile's Thunderbird open." |
| `app-not-found` | "Thunderbird was not found in its usual place. Install it there, then click Run now." |
| `refused-elevated` | "The backup ran as administrator, and the sync never runs that way. Click Repair on the Scheduled backup card." |
| `profile-unusable` | "This Thunderbird profile could not be used. Open Thunderbird once, then click Run now." |
| `no-desktop-session` | "No one was logged in at backup time, so Thunderbird could not be started. Stay logged in at the backup time." |
| `not-set-up` | "The sync could not be prepared on this computer. Click Repair on the Scheduled backup card." |
| `profile-busy` (after 3) | "Another backup kept this Thunderbird profile busy. Shut down Mail Archive, open it again, then click Run now." |
| any other | "Click Run now on the Scheduled backup card to sync while you watch." |

Never RED. `presync_stuck` stays on the allow-list for older records and is never produced. MA-909
asserts the remedy for each reason and a hostile `LastName` (shown as the generic phrase).

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
  removes `<key>.sync-alert`. Uninstall also removes the pre-sync state folder `<config>/presync/`'s
  known files (the state record, the per-profile locks, the C-32 marker) and then the empty folder —
  named files only, no recursion, the same overlap guards (`ConfigTargets` skips directories today,
  `uninstall/config.go:91-94`). The archive's own `.mailarchive-syncstreak.json` stays with the
  archive (R36).

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
   (`jobrun.go:27-42`). **Then** the Run now log opens (§4.8) [C-14].
3. `app.Run`: the archive lock; discovery; the "running" record (`app/run.go:571`).
4. `ctx` checked; the hook (S1..S9, phases, `OnSyncEnd`), skipped if already cancelled [C-10].
5. `defer recordRun` registered with `begun.Presync` set; then the T16 check → cancelled, nothing read
   [C-11, C-12].
6. The read; `recordRun`: last-run (with `presync`), history, source streak, sync streak.

An encrypted Run now: the lease at the click (the OQ-3 window opens here for a sync job), the heal,
then 2.–6. with the key applied before step 3. The hook-only window (if OQ-3 is no) is step 4.

Crash points and the direction they fail (the "Desktop killed" rows are new [C-8]):

| Crash after | State left | What the next run decides |
|---|---|---|
| a failed save of the write-ahead record | nothing launched (A9) | nothing to recover |
| the write-ahead record, before the start | `launching` | S3 clears it or announces a match (as rev 3) |
| Desktop killed during attended S7 | `launching` with the child pid recorded at adoption, Thunderbird running hidden | S3 matches the pid and start time → `leftover-found`, announced, never quit; counts missed if scheduled (§5.2) |
| Desktop killed during the Cancel quit, after `quitting` was saved | `quitting` | S3: the process gone → clear; still there → `leftover-found` |
| Desktop killed during the S8 request | `quitting` | as the row above |
| the final last-run, before the sync streak | one session not counted | towards one fewer miss |
| the sync streak, before the told-file | the email is sent again at the next stale run | towards one more email |
| the Settings sync save (§8.2) | `job.Write` renames a whole file | either job is valid |

---

## 7. Corrected texts

| Where | Today | Proposed |
|---|---|---|
| Run now tooltip, plaintext, sync on (`dashboard.go:1623`) | "…it first opens Thunderbird, waits for its mail, then closes it…" | macOS: "Run this archive's backup now. Because Thunderbird sync is on, it first starts Thunderbird out of sight — even while you are using the computer — waits for its mail to arrive (up to 20 minutes), then closes it, which runs Thunderbird's on-exit actions (for example emptying Trash). A Thunderbird that is already open is left alone. You can cancel while it waits." Windows after L-A7: the same with "minimized". Windows before L-A7: "…Thunderbird sync is on, but on Windows Run now starts it only when you have been away from the computer for 2 minutes…". Linux: no sync sentence (the sync never runs there) [C-24]. |
| Run now tooltip, encrypted, sync on (EncRunNow, `dashboard.go:1623`) [C-25] | "Back up this encrypted archive now, while it is unlocked here." | the plaintext sync text, plus: "While it waits for Thunderbird, the archive stays unlocked for this backup; Lock cancels it." |
| Cancel tooltip, sync-window variant (`dashboard.go:1502`) [C-23] | "Stop this backup now…" | while the sync waits: "Stop this backup now. Thunderbird, started by this backup, is asked to close, which runs its on-exit actions (for example emptying Trash). Nothing is added to the archive." |
| `SyncNotice` (`api.go:100`), shared by every surface [C-31] | "…A Thunderbird you already have open, or one you are using, is left alone…" | keep every X16 disclosure, and replace that sentence with: "A Thunderbird you already have open is left alone — never started or closed. A scheduled backup also waits until you have been away from the computer; a backup you start yourself and watch does not wait for that." |
| `SyncNotice`, encrypted card variant [C-25] | — | the X16 disclosures with no scheduled-backup sentence: "Before each Run now, Mail Archive starts Thunderbird out of sight, waits for its mail to arrive (up to 20 minutes), then closes it, which runs Thunderbird's on-exit actions (for example emptying Trash). A Thunderbird you already have open is left alone — never started or closed." |
| `MacAutomationNote` (`api.go:105`) | "…run one backup yourself first (the Run-now button…)…" | **gated on L-A2.** If one Allow covers both: "On a Mac, the first time Mail Archive closes Thunderbird, macOS asks whether it may. Click Run now once while you are here and choose Allow; the scheduled backup then needs no answer." If not: "…a scheduled backup needs its own answer: [the kickstart step, desktop words]". The second ships until the lab answers. Encrypted card variant [C-25]: "The first time Run now closes Thunderbird, macOS asks whether Mail Archive may control it; stay until it finishes and choose Allow." |
| `warnText` remedies naming commands (`api.go:72-84`) | command lines | rendered per surface by code when read (§4.6) [C-38] |
| `noteText` / `SummaryLine` (`api.go:116-144`) | present tense | kept for the live log; `OutcomeLine` for end-of-run surfaces |
| Run now's cancel (`jobcontrol.go:102`) | "…stops at its next safe point…" | by phase (§3.1) [C-23] |
| Release notes [C-32] | — | "Run now now syncs Thunderbird even while you are at the computer: it starts Thunderbird out of sight and closes it when its mail settles, which runs Thunderbird's on-exit actions such as emptying Trash. Scheduled backups still wait until you are away." |

MA-915's golden covers darwin, windows and linux for the tooltips, the phase notes and `OutcomeLine`
[C-24], every surface of `SyncNotice` [C-31], and a job file under a non-default name [C-18/M2].

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
  unlocked for that backup; Lock cancels it."
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

## 9. The Lock reply and the leftover after a Lock [C-13, C-21, C-24]

- **Window open, a child proven:** `{"ok":true, "note":"Locked. The backup was cancelled before it read
  any mail, and its key is being cleared; nothing was added. Thunderbird, which it started out of
  sight, was left open — quit it from the Dock before the next Run now."}`
- **Window open, still starting, or before the hook (OQ-3):** the same first sentence, then "Thunderbird
  may still be starting; if it appears in the Dock, quit it." (or nothing about Thunderbird before the
  hook started).
- **Window closed, an encrypted Run now on this archive reading:** "Locked for new use. The backup that
  is running goes on with its own key until it ends, then the key is cleared. Click Cancel to stop it
  now."
- **The note stays on screen [C-21].** When the reply carries a note, the Lock script does not reload
  after 6 s (`encryptionscript.go:89`); it shows the note with a Done button, which reloads.
- **The next Run now with that Thunderbird still open:** `leftover-found`, desktop text: "A Thunderbird
  an earlier backup started is still open out of sight, so it was not synced again. Do this: click
  Thunderbird in the Dock and quit it, then click Run now again." Windows: "taskbar", "minimized".

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
Windows: minimized, after L-A7), the note shows each phase and the outcome, Cancel asks the
Thunderbird it started to close, Lock on an encrypted archive cancels and clears the key.

**S65** Scheduled syncs keep being skipped, and the owner must hear of it — R32, X6, X10 — the outcome
is recorded per run (plaintext), the Recent backups card tags it, Status warns after 3 days of
scheduled runs with no sync with a remedy for the reason, one email per streak; encrypted archives
record nothing beside the archive.

(The MA ids each scenario cites are added as their rows land, slice by slice [C-42].)

**X16 amendment:** "an already-open Thunderbird, or an owner at the computer, is left alone" → "an
already-open Thunderbird is left alone; a scheduled backup also leaves Thunderbird alone while the
owner is at the computer, and a backup the owner starts and watches says it does not wait for that.
For an encrypted archive, status shows the setting but keeps no last result (OD-3)."

| ID | Tier | Slice | Asserts | Covers | Prove-fail |
|---|---|---|---|---|---|
| MA-895 | U | AS1 | attended skips S4: owner idle 0 s, and an unreadable idle reading, with `Attended` → a launch and `idleSeconds` never called; twin without → `owner-active-skipped`, zero starts; sync off → nothing installed (R30) | R31, R38, S64 | read the idle gate in attended → the start is skipped |
| MA-896 | U | AS1 | attended keeps every other check (already running A, B, another profile; C5; profile re-check; profile busy; leftover; await-up; max wait) with the same outcome and zero-quit count; attended with a job-named profile not in `profiles.ini` → `profile-unusable` [C-18] | R31, R32, S64 | skip `detectRunning` when attended → a launch beside an open Thunderbird |
| MA-897 | U | AS1 | attended skips the S8 idle re-check and keeps the pre-quit function; the quit context is live at the call with a 20 s deadline in both modes [C-5, C-6]; twin unattended → `owner-active`. **Extended [C-43]:** under the fake clock, at most one progress line per fake minute with the right N in both modes; `OutcomeLine` for every outcome in `allOutcomes`, both surfaces, darwin and windows words, every `Left`; `Waited` measured from "up" to the quit or verdict; `Started` set | R31, R32, S64 | drop the ownership re-check in attended → a non-ours pid is asked |
| MA-898 | S | AS3 (negative), AS4 (positive) [C-44] | AST check matched on the mode field or constant, not a literal; and a **behavioural twin through the seam [C-41/T10]:** `RunHeadless` with a sync job gives `Attended` false; `RunJobInProcess` and `RunJobInProcessKeyed` give true (AS4); a job file and a descriptor carrying `"attended": true` (and other spellings) give false; no field named `Attended` has a JSON tag | R38 | set it in `RunHeadless` → the twin fails |
| MA-899 | U | AS10 | the CLI: no `-unattended`, local foreground terminal (faked) → attended; `-unattended`, piped stderr, background job, or `SSH_CONNECTION`/`SSH_TTY` set → not [C-9]; Windows console without `-unattended` → attended, every baked argv carries `-unattended` | R38 | drop the SSH test → the remote case is attended |
| MA-900 | U | AS1 | attended cancel: cause `ErrCancelAndQuit` in S7 → the pre-quit function, exactly one request, `quitting` then cleared or `left-running`, `Left` right; **twins [C-1]:** a second Thunderbird present on macOS → zero requests, `left-running`; one pid unreadable → zero requests; any other cause or `Stop` → zero requests; S6 Cancel → polls to the 90 s limit then one request, or none and `may-be-starting` [C-7]; **cancel before S5 → zero launches, no `launching` record [C-10]**; a failed `launching` save → zero launches, `cannot-start` [C-8]; `sleepCtx` returns well before d on a real short cancel (untagged, runs on Linux) [C-2/T4]; scheduled runs unchanged | R31, R32, S64 | quit on every cancel cause → the Lock case sends a request |
| MA-901 | U | AS1 | B6: an unreadable state record's warning reaches the run's warnings while the session syncs; twin with a readable record → none; a proceeding note-class recovery outcome adds no side outcome [C-45] | R32 | drop the side outcome → the warning is absent |
| MA-902 | U | AS2 | a real `app.Run`: a cancel during the hook stops before any source read, index open or sealed catch-up; a cancel before the hook never calls it [C-10]; the last-run **file on disk** reads `cancelled`, never `running` [C-11, C-12]; the hook's `Release` still runs | R32, R18, S64 | move the check before `defer recordRun` → the file reads `running` |
| MA-903 | U | AS2 | the outcome travels through a real `app.Run` on two paths — a full success and a failure after the hook (an index refusal) — to the last-run file (`name`, `result`, `since`, no `attended`) and the history's `presync` [C-12, C-36]; `Synced()` equals §5.2 for every entry of `allOutcomes`; `state-unreadable` never the main outcome [C-45] | R32, S65 | drop `begun.Presync` → the failure path's file has no presync |
| MA-904 | U | AS2 (record parts), AS5 (streak part) [C-16] | §4.4's pin: exact key sets against the A18 allow-list; `leak.CheckTree` plus outcome-name and profile-marker scans over the whole folder; after synced, skipped and Lock-cancelled Run nows; no sync-streak file, no `mailarchive.log`; `Presync` nil for keyed runs; a stray sync-streak file removed by a keyed run; positive plaintext twin writes `presync` | R37 (A6, A18), S65 | (AS5) drop the plaintext-only guard on the reset path → a sync-streak file appears |
| MA-905 | U | AS7 | the tag: §4.5's phrases per name; every `allOutcomes` name mapped or deliberately untagged; unknown names never echoed; a `presync` name **planted in an encrypted history** still shows no tag [C-16/T12]; control characters stripped | X10, S65 | show unknown names raw → the hostile case renders them |
| MA-906 | U | AS4 | `/api/activity` carries `syncPhase` and the **server-built** `syncNote` for each phase, the counter only in `waiting` and never above 20, then `syncLine` with the result, the failure and the cancel words [C-19]; `runJobOn` resets the sync fields with `lines` and `result`; a Lock or switch clears an encrypted job's activity [C-15]; a **panicking** hook and a hook cancelled before `OnSyncEnd` leave `syncPhase` empty and the window closed [C-11]; a reload during the wait re-attaches the Run now poll (structural) [C-22] | X11, S64 | build the failure note without `syncLine` → the Go test fails |
| MA-907 | U | AS3 (scheduled), AS4 (Run now) | every line a scheduled run writes to `mailarchive.log` starts with a date and time; a plaintext Run now appends under its own header **after** the refusals; a plaintext-path Run now on an encrypted archive is refused and leaves no `mailarchive.log` and no `.1` [C-14]; an encrypted Run now writes nothing under the archive | R18, S65 | open the log before `RefuseWrite` → the encrypted case has a log |
| MA-908 | U | AS5 | the streak: reset by any synced session; extended only when `Unattended && !PresyncAttended && PresyncWanted` and missed; **a plaintext Run now (Unattended true) that missed does not move it [C-33]**; a hook-setup failure on a sync job extends it as `not-set-up` and never deletes it; a sync-off scheduled job deletes it; a manual run without `-thunderbird-sync` does not [C-34]; three `profile-busy` in a row count one miss [C-4]; `leftover-found` counts missed; stale needs ≥ 2 misses over ≥ 3 days, never compared with now; implausible time counts `Missed` without moving times; a clock that went back restarts `FirstMissed` and keeps `Serial` [C-35]; never written for an encrypted archive; secure, bounded, stripped read | R32, S65 | compare with now → the computer-off case warns |
| MA-909 | U | AS5 | Status WARN `presync_not_synced` when configured and stale, naming date, count, phrase and the per-reason remedy for every reason in §5.4 [C-20], in each surface's words [C-38]; a hostile `LastName` shows the generic phrase [C-37]; never RED; nothing when not configured; the health view says the same | R32, X6, X10, S65 | always use the Run now remedy → the leftover case fails |
| MA-910 | U | AS6 | the email: once per `Serial`; `ToldFor` written only after delivery, or at once with alerts off; a failed delivery retried; never on a failed run; deferred when the run sends the recovery email; a clock restart inside one streak sends no second email [C-35]; the words come from the closed maps (hostile streak) [C-37]; uninstall removes `*.sync-alert` and the presync folder's named files then the folder, with the suffix check; `ClearStreak` removes `<key>.sync-alert` [C-40] | S65 | drop the `ToldFor` check → every stale run sends |
| MA-911 | U | AS8 | the encrypted Settings box: shown (by `goos`) for macOS and Windows for a readable job made from Thunderbird or carrying the sync, with the encrypted `SyncNotice` and Mac note variants and its own Save with `busy()` [C-25, C-26]; absent on Linux, for an Outlook job, for an unreadable job (reason shown); the card and health view show the setting and the session's last result while unlocked [C-27] | X16, S64 | show it for an Outlook job → the case renders a box |
| MA-912 | U | AS8 | `api/runnow-sync`: each §8.2 refusal, including **nil `drawnFor`, missing `presync`, and a post naming another archive's job** [C-18], leaves the job file, the archive, the scheduler and the settings unchanged (`assure.Refused`, positive twin first); success edits exactly the job Run now would run (a non-default name included), changes only `thunderbird_sync`, writes no `Profile` | R30, R37, S64 | edit the default-named file → the non-default case misses |
| MA-913 | U | AS8 | Lock while an encrypted sync Run now waits: cancelled and recorded cancelled; the `runLease` seam (`schedule.go:684`) shows the lease locked at once and the key **not** destroyed while the hook holds it, destroyed before `RunJobInProcessKeyed` returns [C-5/T8]; zero quit requests; the reply's note, still visible after the reply (no reload) [C-21]; **Lock during the S8 request and during a Cancel quit** end the run within §3.2's bound; **Lock then Cancel** → zero quits; **Cancel then Lock** → the quit request dies, bounded [C-5]; **Lock before the hook** → zero starts, recorded cancelled (OQ-3 default) [C-10]; **Lock in the read phase** → the run goes on and the reply says so [C-13]; idle lock or switch during the wait does not stop it; the race through the pause seam lands on one side [C-11]; a panicking hook leaves no stale window | R37, R32, S64 | use `watchLock` → the idle-lock case cancels |
| MA-914 | U | AS4 | plaintext Run now: `cancellable` true; through the seam, Cancel delivers `context.Cause = presync.ErrCancelAndQuit`, Shut down and Lock do not and end `Stop` [C-41/T15]; Cancel while reading → cancelled, archive intact; the cancel line by phase [C-23] | S64 | call `cancel(nil)` → the cause assertion fails |
| MA-915 | S | AS9 | the texts: tooltips (plaintext and encrypted, sync on, darwin/windows/linux, non-default job name), the Cancel sync variant, `SyncNotice` (every X16 disclosure, every surface) and its encrypted variant, the Mac note per L-A2 and its encrypted variant, desktop words name no command, `OutcomeLine` names no path, no draft-prompt clause before L-A5; no Desktop text promises a behaviour the code lacks (golden) | X16, X11 | restore the old tooltip → the golden fails |
| MA-916 | U | AS2 | migration of records: frozen v1.5.1 bytes (job, last-run, history) read by the new code; new records read by the copied v1.5.1 structs; an encrypted job with the sync keeps its other fields; a v1.5.1 history rewrite drops `presync` (residual pinned) [C-39] | S58, S65 | rename the `presync` history key → the fixture case fails |
| MA-917 | L | lab | macOS lab with the owner's real Thunderbird, **only with the owner's OK**: L-A1..L-A6 — **pending** | R31, R32, R38, S64 | — |
| MA-918 | L | lab | Windows lab (Parallels, ARM-emulated): L-A7 (minimized, not active; no keyboard focus taken while the person types); then attended start, `WM_CLOSE` quit, Cancel — **pending**; attended on Windows ships only after it passes [C-47] | R31, R32, S64 | — |
| MA-919 | U | AS4, **only if OQ-1 is yes** | "Keep Thunderbird open": pressed during the wait → no quit request, the record cleared, `Left=left-open-by-choice`, counted synced, the archive still runs [C-46] | R31, S64 | ignore `KeepOpen` → a request is sent |
| MA-920 | U | AS5 | migration of the streak: a v1.5.1-era archive with no streak file starts clean; the downgrade residual of §10 (1) pinned as stated [C-39] | S65 | — (pins a stated residual; prove-fail named at AS5) |
| MA-921 | U | AS1 | the watchdog [C-2, C-3]: `sessionBudget()` exceeds the longest path and both Cancel paths computed from the same constants; on the budget, on `Stop` and on a non-quit cause the watchdog returns, marks the session abandoned and cancels `sctx`; an abandoned session sends **no** quit request even when it reaches S8 afterwards, an in-flight request's context ends, and it releases its profile lock when it returns (the next session gets the lock, not `profile-busy`) | R31, R32 | remove the abandoned check in `preQuit` → a late request is counted |

---

## 12. Build slices (each small, test-first, prove-fail then prove-pass)

| Slice | Contents | Rows | Shippable? |
|---|---|---|---|
| **AS0** | catalog: R38, S64, S65, the S54 and X16 amendments only [C-42] | — | nothing (operator gate OD-2) |
| **AS1** | `internal/presync`: `Config` fields; A1, A2, A4..A11; the quit seam; `sessionBudget()`, `sctx`, the abandoned rule; `sleepCtx`; `OutcomeLine`, the desktop and platform texts | 895, 896, 897, 900, 901, 921 | nothing (no caller sets attended) — but the T21 watchdog fix and A9/A11 reach scheduled runs |
| **AS2** | `internal/app`: `PresyncOutcome` on `begun`, the pre-hook and post-hook checks, `Options.PresyncWanted/Attended`; `LastRun.Presync` and history fields; strip on read; the encrypted record pin | 902, 903, 904 (record parts), 916 | nothing user-visible |
| **AS3** | `presyncjob`: untagged `SetHook`, `runSession` seam; `optsForJob` with a logger and a mode; the CLI on the shared hook; timestamps | 898 (negative half), 907 (scheduled part) | timestamps |
| **AS4** | Desktop Run now: **attended from here** [C-44] (Windows: not until L-A7); the cancellable slot and causes; phases and server-built note; reload re-attach; the Run now log file; tooltips | 898 (positive twin), 906, 907 (Run now part), 914, 919 (if OQ-1) | **Run now syncs (plaintext, macOS)** — ships only after L-A5 passes on macOS (OQ-1's gate) |
| **AS5** | the sync streak, the Status line and WARN, remedies | 904 (streak part), 908, 909, 920 | the 3-day WARN |
| **AS6** | the email, the checkbox label, uninstall and `ClearStreak` | 910 | the email |
| **AS7** | the Recent backups tag | 905 | the tag |
| **AS8** | encrypted: the Settings box and its Save, `api/runnow-sync`, the Lock window (OQ-3) | 911, 912, 913 | encrypted archives sync from Run now (after OQ-3, OQ-4) |
| **AS9** | `SyncNotice` and its variants, the Mac note (after L-A2), the remaining texts, release note | 915 | — |
| **AS10** | the CLI at a local foreground terminal — **only after the owner's nod (Q3)** | 899 | the manual CLI sync works |
| lab | MA-917 (macOS), MA-918 (Windows, incl. L-A7) | | |

Order: AS0 → AS1 → AS2 → AS3 → AS4, then AS5..AS9 in any order (AS8 needs AS4's slot; AS9's Mac note
needs L-A2), AS10 last.

---

## 13. Lab items (a real Mac or Windows and a real Thunderbird; the owner's profile only with the owner's OK)

- **L-A1** `open -g -j` while the person is typing in another app: Thunderbird stays out of sight and
  does not take the keyboard; a click on Run now does reset the idle reading (T1's mechanism).
- **L-A2** The first quit from a Desktop Run now: does macOS ask, does `osascript` wait for the answer,
  does one Allow also cover the scheduled run-job — and **[C-6] if the request is held past its 20 s
  bound and our Thunderbird is quit and another opened meanwhile, does the late answer reach the new
  one, or fail with error -600?**
- **L-A3** Lock during the wait: time from Lock to the run's end (against §3.2's bound); Thunderbird
  left open; the next Run now announces it.
- **L-A4** Cancel during the wait: one quit request; Thunderbird closes, or asks about a draft.
- **L-A5** The person opens our hidden Thunderbird from the Dock (Windows: taskbar) during the wait and
  starts a message: at quiet the quit is asked; does Thunderbird ask before discarding it, after an
  AppleScript quit and after `WM_CLOSE`? **A ship gate for AS4 on each platform (OQ-1):** if it does
  not ask, attended quit does not ship.
- **L-A6** `mailarchive.log` after a scheduled run with the sync: every line timed.
- **L-A7 (new) [C-47]** Windows: a start with the minimized-not-active flag (`SW_SHOWMINNOACTIVE`) while
  the person types in another app: the window never takes keyboard focus. Gate for attended mode on
  Windows.

---

## 14. Gates owed

1. **The pre-code review is filed** (`docs/review-presync-attended-predesign.md`,
   GO_WITH_CONDITIONS). **Owed before AS1:** a second reader re-verifies each `[C-n]` fold against its
   condition; the operator answers OD-1 (five finders, no rescue pass).
2. **Adversarial pass** after AS4 (process control, causes, the abandoned session) and after AS8 (Lock,
   lease, the route, the byte pin).
3. **Friction review** of the phase notes, the Cancel and Lock replies, the encrypted box, the Status
   line, WARN remedies, the email and checkbox, and every corrected text (after AS4, AS8, AS9).
4. **Labs**: MA-917 with the owner's OK; MA-918 incl. L-A7. L-A5 gates AS4's ship (OQ-1); L-A7 gates
   attended on Windows (OQ-2).
5. **Operator**: OD-1..OD-4.

---

## 15. Open questions

### 15.1 Owner rulings needed (from the review)

- **OQ-1 (C-46).** A "Keep Thunderbird open" button in the Run now note while it waits, and lab L-A5 as
  a ship gate (no attended quit if Thunderbird does not ask before discarding a message being written).
  Default built: yes to both. If no: no MA-919, and L-A5 stays a lab item only.
- **OQ-2 (C-47).** Windows cannot start Thunderbird hidden; accept ruling 2 as Mac-only and hold
  attended Run now on Windows until L-A7 passes. Default built: yes (a Windows Run now keeps today's
  idle rule until then).
- **OQ-3 (C-10).** Lock cancels a sync Run now at any point before it reads (from the click), not only
  during the Thunderbird wait. Default built: yes. If no: the window is the hook only.
- **OQ-4 (C-17).** Accept the widened encrypted residuals (§3.2, §4.4).

### 15.2 Operator acceptances

- **OD-1** the review's coverage (five finders; lenses 2 and 9 not run; no rescue pass).
- **OD-2** R38, S64, S65, the S54 and X16 amendments, the phase-3 §3.2 Lock amendment, the per-slice
  row plan.
- **OD-3** the X16 exception for encrypted archives in `status` (C-27).
- **OD-4** PC3 tightened for attended runs (C-18).

### 15.3 Earlier questions (each has a default this design builds)

- **Q1** An already-open Thunderbird counts as a sync. Default **yes**.
- **Q2** Lock: key first, Thunderbird left open — **settled** by ruling 4 and the review.
- **Q3** The CLI at a local foreground terminal also attended (AS10). Default **yes, last slice**.
- **Q4** Write the encrypted outcome into the sealed run details (A24) later. Default **not now**.
- **Q5** The sync email under the failure option, with the relabelled checkbox, no "working again"
  email. Default **yes**.
- **Q6** An attended Run now closing a leftover an earlier backup provably started. Default **no**.

## 16. Not checked

- No test was run; every "proven" is by reading.
- macOS's Automation prompt and delayed delivery (L-A2), `open -g -j` while typing (L-A1),
  Thunderbird's quit prompt (L-A5), Windows focus (L-A7): not checked.
- `env_windows.go`'s quit path was not read in full; the claim that `WM_CLOSE` returns at once is
  inferred from `quit_windows.go`.
- `container.Store`'s copies of key material after `vault.Key.Destroy` — not read; §3.2's "key
  destroyed" is about the lease's key.
- `encryption_create.go` beyond its call site; `alert.go` beyond `ShouldSend`; how an archive switch
  treats the capture state today.
- `probesOnPath` in §2.6 is an estimate to be counted at AS1.
