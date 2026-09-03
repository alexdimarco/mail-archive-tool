# Design — repeat archiving v2: schedule any job, Windows-safe, with a status surface

**Revision:** 2 (2026-09-02). **BUILD STATUS:** approved with conditions — pre-code review
filed as `docs/review-schedule-v2-predesign.md` (GO_WITH_CONDITIONS, S1–S16 folded into
this revision). Built as the slices of §4; the secret-file slice additionally owes the
adversarial pass and the GUI slices the friction review (S1).

## 1. Problem (from the 2026-09-02 flow review)

- `schedule` can only bake plain export flags. `-outlook` (the reliable path for
  a live Exchange `.ost`) and the whole `graph` subcommand cannot be scheduled,
  although README and `docs/graph-app-setup.md` say they can.
- On Windows the Task Scheduler run string (`/TR`) is limited to **261
  characters** (confirmed: `ERROR: Value for '/TR' option cannot be more than 261
  character(s)`); a typical exe + OneDrive `-out` + `-input` exceeds it. Windows
  jobs also have **no log** (no shell redirect in `/TR`).
- A scheduled `graph` job has no environment, so the env-var-only secret cannot
  reach it.
- There is no status surface: nothing says whether a schedule is installed,
  when it last ran, whether it succeeded, or whether the archive is complete.
- The GUI has no repeat story at all.
- `-enable-offline`/`-sync-wait` are interactive; scheduling them would hang.

## 2. Properties

- **P1 — Any backup job, one grammar.** `schedule` accepts either the existing
  export flags (now including `-outlook`, `-outlook-sync-wait`) *or* `-- <job>`
  where the job is the default export, `graph`, or `reindex` (S2). The two forms
  are mutually exclusive: with `--`, any schedule-level export flag is refused
  naming it, and `-out`, the log path and all validation derive from the job (S3).
  The scheduled command is exactly that job. (MA-72)
- **P2 — Fail at schedule time, not at 02:00.** The job is dry-parsed with the
  real subcommand flag sets: an unknown flag, a missing `-out`, an interactive
  flag (`-enable-offline`, `-sync-wait`), `-outlook` off Windows, a
  non-backup verb (`serve`, `search`, `status`, `schedule`), or a `graph` job
  whose secret file fails the full run-time check (S10) is refused now, naming
  the problem and the remedy. (MA-72)
- **P3 — Windows-safe.** Install writes the wrapper `%LOCALAPPDATA%\mailarchive\
  <name>.cmd` FIRST (S13), holding the command with every token quoted and every
  `%` doubled (S4), then creates the task whose `/TR` is the quoted wrapper path.
  `-name` is sanitized and capped at 40 characters, so the `/TR` string is bounded
  at ~120 characters on any Windows profile (S12). Remove deletes both. The
  wrapper generator is pure and unit-tested. (MA-73)
- **P4 — A secret file, never a secret argument.** `graph -client-secret-file
  PATH` reads the secret from a regular file (Lstat; a symlink/FIFO/device is
  refused), bounded to 4 KB, non-empty; on Unix a group/world-readable file is
  refused naming `chmod 600`; the secret never appears in argv, logs, or the
  scheduled command. The same reader runs at schedule time. (MA-74)
- **P5 — Every run that begins is recorded.** `app.Run`/`RunGraph` write
  `<out>/.mailarchive-lastrun.json` {version, started, pid, status:"running"} as
  their FIRST action (before any early return) and finalize it atomically at the
  end {finished, mode, exported, filled, fillable, terminal, error, exe}. Under
  SIGKILL/power loss the `running` record stays and is itself a signal (S6). (MA-76)
- **P6 — `status` is the legibility surface (X6).** `mailarchive status -out DIR`
  prints messages, indexed, fillable/terminal/unknown counts (+report path), the
  last run (+error), the schedule from the archive-local descriptor (S5) — name,
  cadence, host, whether the real executable still exists and whether it is this
  binary (S8) — and a GREEN/WARN/RED posture that fails closed on missing
  evidence (S6), with every WARN/RED naming its remedy. It exits 0 whenever it
  reports (S15). (MA-75, MA-78)
- **P7 — The GUI can keep an archive current.** After a successful wizard run it
  offers *Keep this archive current? (No / Daily at 02:00 / Weekly, Sunday
  03:00)*. Yes writes a job file and installs a schedule (same per-archive name
  as the CLI, S7) whose program is the GUI executable in headless mode
  (`mailarchive-gui -job FILE`): no dialogs, no console, a rotating log, lastrun
  written. On every launch the first screen shows the health of any schedule for
  the last-used archive (last run, posture, "the scheduled program is not this
  binary") beside *Remove the scheduled backup* (S8). The wizard dialog flows are
  lab-tier; the job-file codec, headless wiring and health logic are tier U. (MA-77)
- **P8 — Bounded logs (S9).** `-log FILE` on export and graph writes the operator
  log to FILE with size-capped rotation (8 MB, one `.1`) instead of stderr; every
  scheduled entry uses it. Crash output the product cannot capture goes to cron's
  mail on Linux and to `<name>.stderr.log` under launchd and the Windows wrapper.
  A failed scheduled run also prints its one-line failure to stderr so cron mail
  (MAILTO) carries it — the only push channel that needs no new dependency.

Conceded non-goals:

- A Windows CLI-scheduled job runs through `cmd.exe`, so a console window
  appears briefly at run time (Type I for a console program; the GUI job has
  none). Documented.
- Task Scheduler jobs created without credentials run only when that user is
  logged on and are skipped if the machine is off/asleep (same as cron on a
  laptop). Documented; `status` shows the last run so a silently skipped night is
  visible.
- `-outlook` unattended requires an interactive session and a configured
  Outlook profile; Outlook's one-time "allow programmatic access" prompt cannot
  be answered by a scheduled job. Documented: run it once interactively first.
- Windows file ACLs are not checked for the secret file (no portable API); the
  README says to keep it under the user's profile.
- **No push notification.** A backup that stops is *discoverable* (`status`, the
  GUI health view, cron mail on Linux), not *disclosed* by an OS notification;
  that channel is backlog, not promised (hidden blocker of the review).
- Entra client secrets expire (≤24 months). README/graph-app-setup carry the
  calendar note; `status` maps an authentication failure to a "rotate the secret
  and rewrite the file" remedy (S10).

## 3. Mechanism

### 3.1 Job parsing (CLI refactor, no behaviour change)

Extract the flag-set builders from `runExport`/`runGraph` into
`exportFlags(fs) *exportOpts` and `graphFlags(fs) *graphOpts` so `schedule` can
dry-parse a job through the same definitions. `parseJob(args) (job, error)`:
subcommand = `args[0]` if it is one of the known verbs, else the default export;
returns the subcommand name, the parsed options (for `-out`, `-input` paths to
absolutize, the interactive/platform checks) and the canonical argv. Positional
inputs are kept. Paths in `-input`, `-out`, `-client-secret-file` are made
absolute (a scheduled job has an unknown cwd), as today.

Refusals (all `mailarchive: …`, exit 1, R12): unknown flag (from `flag`);
`-enable-offline`/`-sync-wait` → *"is interactive: run the one-time prep by hand
(see README → IMAP), then schedule without it"*; `-outlook` off Windows (reuse
`outlookcom.ErrUnsupported`); a non-backup verb → *"serve is a long-running
server and cannot be a scheduled backup job; schedule an export, graph or
reindex job"* (S2); a schedule-level export flag combined with `--` → names the
flag (S3); `graph` without `-client-secret-file` → *"a scheduled job has no
environment; pass -client-secret-file"*; the secret file failing `readSecret`
(S10).

### 3.2 `schedule.Spec` and the Windows wrapper

`Spec` gains `Wrapper bool` and `WrapperPath` (default
`%LOCALAPPDATA%\mailarchive\<name>.cmd`). The CLI always sets `Wrapper` on
Windows (it needs the stderr redirect); the GUI sets it only when the direct
`/TR` string would exceed 261 characters (S12), accepting the console flash in
that case. Pure generators: `CmdWrapper(s) string` (content: `@echo off`, a
comment naming the managing command, the run line with EVERY token wrapped in
`"` and every `%` doubled — inside cmd.exe quotes `& | < > ^ ( )` are inert and
delayed expansion is never enabled, S4 — redirecting `2>> "<name>.stderr.log"`),
and `SchtasksCreateArgv`, which uses `winQuote(WrapperPath)` as `/TR` when
`Wrapper`. Install writes and fsyncs the wrapper before creating the task
(S13); `Remove` deletes the task, then the wrapper. `Preview` prints both.

Every install (all OSes) also writes the archive-local descriptor
`<out>/.mailarchive-schedule.json` {version, name, interval, at, exe, exe_size,
exe_mtime, wrapper, job, installed_at, host} and `Remove` deletes it (S5/S8).
`schedule -out DIR -remove` reads the name from it. The default name is
`mailarchive-` + the first 8 hex of sha1(absolute out) (S7); `DefaultName`
stays the managed-marker prefix so existing entries are still recognized.

### 3.3 `-client-secret-file`

`readSecret(path)`: `os.Lstat`; not a regular file (symlink, FIFO, device) →
refuse naming the requirement; on non-Windows `mode&0o077 != 0` → refuse naming
`chmod 600 <path>`; read at most 4 KB, `strings.TrimSpace`; empty → refuse.
Precedence: flag file over env var. The same function runs at schedule time
(S10). The `graph` help and `docs/graph-app-setup.md` show the file form for
scheduled use and the secret-expiry note.

### 3.4 Last-run record and `status`

`internal/state/lastrun.go`: `LastRun{Version int; Started, Finished time.Time;
PID int; Status string /* running|ok|failed|cancelled */; Mode string; Exported,
Filled, Fillable, Terminal, IndexErrors int; Error, Exe string}` with atomic
`Write(out)`/`Read(out)` (Read distinguishes absent / unreadable / present).
`app.Run`/`RunGraph` write the `running` record as their first action and
finalize in a deferred step (S6).

`schedule.Installed(name) (State, error)` returns Installed / NotInstalled /
SchedulerUnavailable (S14): cron → `crontab -l` contains `CronMarker(name)`
(exec error ≠ empty crontab); launchd → plist exists; Windows → `schtasks
/Query /TN <name>` exit 0 vs. command missing. Each is a pure predicate over
injected command output/errors so it is unit-testable on any OS (MA-78); the
real-host behaviour is a lab row. `status` reads the descriptor for the name,
job and installed exe and stats the REAL exe (never the wrapper), comparing
size/mtime with the running binary.

`runStatus` prints to stdout (the answer, X8):

```
Archive:    /path/to/export
Messages:   1843 in manifest · 1843 indexed
Incomplete: 12 — see /path/attachments-report.tsv (download in your mail app, then re-run; incremental fills them)
Last run:   2026-09-01 02:00 → ok · exported 5 · filled 2 · 41s   (or: FAILED: <error>)
Schedule:   "mailarchive-backup" installed · daily 02:00 · runs /usr/local/bin/mailarchive   (or: not installed → mailarchive schedule -out … -install)
Posture:    GREEN | WARN (reasons…) | RED (reasons…)
```

Posture (fail-closed, S6): RED if the last run failed, or its record is
`running` with a dead pid or older than one interval ("previous run did not
complete"), or the scheduled executable is missing; WARN if fillable>0 or
unknown>0, no schedule is installed, the scheduler is unavailable, a schedule is
installed but no run was ever recorded, the record is unreadable (named), the
last run is older than 2× the interval, or the scheduled executable is not this
binary; else GREEN. A directory with neither manifest nor descriptor is refused
naming it. An authentication error in the last run adds the secret-rotation
remedy. `status` exits 0 whenever it reports (S15); a machine-readable health
signal is ux-contract condition C5 (backlog).

### 3.5 GUI

- `mailarchive-gui -job FILE`: headless. The job file (`internal/job`, JSON:
  version, inputs, auto, out, mode, since, copy_first, outlook; S11) is written by
  the wizard to `os.UserConfigDir()/mailarchive/<name>.json`. Headless mode
  re-runs discovery when `auto` is set, then runs `app.Run` (or the COM path) with
  the rotating log opener on `<out>/mailarchive.log`; no dialog is ever shown. A
  job-file load failure is appended to `<config>/mailarchive/<name>.jobfail.log`
  and exits 1 (a sink that does not depend on `out`).
- Schedule step after a successful run (P7): `schedule.Install` with
  `Exe=os.Executable()`, `Args=["-job", path]`, the per-archive name, `Wrapper`
  per S12. Before installing, if the executable lives under a Downloads/Temp
  path the GUI warns ("move it somewhere permanent first") with *Continue anyway*.
- First screen (health view, S8): for the last-used archive (remembered in the
  config dir) read the descriptor + lastrun and show last run, posture, and
  whether the scheduled program is this binary, beside *Remove the scheduled
  backup*.
- The GUI flow fixes from the flow review (mode question skipped when prep
  forces Full; prep for auto-detected inputs; "nothing found" re-asks; "mail app
  open?" only for a data file; Evolution in the manual list; verification counts
  in the summary; unreadable `.ost` points at the Outlook-app option) are a
  SEPARATE friction-owned change with their own catalog rows (S1e), not part of
  this design.

### 3.6 Docs

README: a per-mail-program recipe table (prep once → first archive → keep it
current) at the top of Usage; the schedule section rewritten around `--`, the
Windows wrapper and log, the console-window and logged-on notes, `status`; the
Graph section and `graph-app-setup.md` fixed to the secret-file form.

## 4. Build order and seams

Slices (S1), each its own commit naming its gate:

1. **(a)** CLI flag-builder refactor + `parseJob` + `--` form + verb whitelist +
   form exclusivity + `-log` rotation (MA-72, MA-93; MA-39/50 stay green).
2. **(a)** Windows wrapper (quote-all, stderr redirect), descriptor, install
   order, `Remove` via `-out`, per-archive default name, `-name` bound (MA-73;
   MA-65 stays).
3. **(b)** `-client-secret-file` with the full reader at both times (MA-74) —
   adversarial pass filed as `docs/review-schedule-v2-adversarial.md`.
4. **(c)** lastrun start/finish + `Installed` three-state + `status` (MA-75,
   MA-76, MA-78; MA-39 gains `status -h`).
5. **(d)** `internal/job` + GUI headless + schedule step + health view (MA-77
   tier U core; wizard flows lab-pending) — friction review.
6. Docs (README recipe table, schedule/status sections, graph secret file +
   expiry, upgrade/rollback notes).

Depends on design-incremental-completeness for the counts (built first).
Invariants: **R14** extended ("…carrying exactly the operator's job, refusing at
schedule time what cannot run unattended, correct for any host path") and new
**R18 — Every scheduled run is recorded and `status` reports completeness,
last run, and schedule posture, failing closed on missing evidence.**

## 5. Security notes for the reviewers

- The wrapper `.cmd` and the job file live in per-user directories with the same
  trust as the executable path already baked into the entry. They contain no
  secrets (the secret file path only).
- The secret file is read by the `graph` process only; its content is never
  logged (the logger prints tenant/client id/mailboxes, never the secret — MA-74
  asserts the secret string is absent from stderr and the log).
- `status` runs only read-only host commands (`crontab -l`, `launchctl list`,
  `schtasks /Query`).
- Nothing here changes what the archive contains or how sources are read (R9).

## 6. Honesty of claims

- P1–P6 and P8, plus the tier-U core of P7 (job codec, headless wiring, health
  logic): **proven** by MA-72..MA-78 and MA-93 once built. The real `schtasks`
  apply, the real `.cmd` execution under Task Scheduler, real-host `Installed`
  detection on each OS, and the wizard dialog flows are **lab-pending** (MA-79,
  MA-95).
- "No console for the GUI job": **conditional** on the direct `/TR` fitting 261
  characters; otherwise the wrapper is used and a console flashes (S12).
- "Every run that begins is recorded": **best-effort** under power loss (the
  `running` record survives and is itself the signal).
