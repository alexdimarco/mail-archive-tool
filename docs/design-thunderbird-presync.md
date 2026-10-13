# Design — Thunderbird pre-sync: start Thunderbird, let it fetch, ask it to quit, then archive (opt-in, per schedule)

**Revision:** 1 (2026-10-13, draft). **BUILD STATUS:** proposed. Not built. The 10-lens
pre-code review is not yet filed (`docs/review-thunderbird-presync-predesign.md` does not
exist), so no code may start from this text. Nothing here is lab-validated: every Thunderbird,
macOS, Windows and Linux behaviour that this text states from memory of vendor documentation,
and not from this repository, carries the words "lab item" and an id (L-M*, L-W*, L-L*, L-C*),
and is verified on a real machine before the slice that depends on it ships (§9.4).

Base: branch `wip/tb-presync-design`, from `fix/review-2026-10` at `85c4c41`. This document
changes no code, no test and no catalog row. Every id it proposes (R30..R32, S53..S58, X16,
MA-546..MA-574) is provisional until the OPERATOR accepts it into `docs/scenario-catalog.md`
and `docs/ux-contract.md` with its slice's catalog-first PR (`docs/owner-rulings-2026-10-09.md`
§8; the rulings name MA-546 as the next free overflow id and R29 / S52 / X15 as the last
allocated invariant, scenario and X ids).

**Owner request (2026-10-13), verbatim:** "is there a way to have it start up Thunderbird,
sync, and shut it down?" then "I think it is worth building". The owner wants an opt-in,
per-schedule setting, **Sync Thunderbird before archiving**, default OFF.

Gates before code (none is met):
- **TG1** the pre-code design review (`docs/review-thunderbird-presync-predesign.md`), lenses
  per §14, with the rescue pass. A material change to this text re-runs the affected lenses.
- **TG2** owner answers to the questions in §13 that block a slice (each has a recommended
  default and a safe interim, so no slice is held by an unanswered question).
- **TG3** lab items L-M1..L-M8 (macOS) before slice TP3 ships. L-W* before TP6. L-L* before TP7.
- **TG4** the extraction-workspace and fault-isolation designs are not built either. This design
  touches none of their surfaces except two seams it names (§4.1): `app.Run`'s run record and
  `state.RunWarning`.

**Claim labels used throughout.** **proven** = a tier-U/S row in §10 asserts it on every commit
once its slice is built (nothing is built, so read "proven" as "will be proven by MA-5xx").
**conditional** = holds only under the condition written beside the claim. **aspirational** =
intended, not mechanically assured. **lab item** = depends on how a real Thunderbird or OS
behaves, and is not assumed.

**Vocabulary.** *Sync session*: the whole pre-sync stage of one run, from the decision to look
at Thunderbird to the end of the quit wait. *Ours*: a Thunderbird process this session
started and has positively identified (§4.4.3). *Quiet*: no size, mtime or name change anywhere
in the watched mail folders for the configured quiet period (§4.4.2). *Profile*: the
Thunderbird profile directory (the one holding `prefs.js`, `thunderbird.exe`'s `-profile`
argument).

## 1. Problem

1. **The tool only reads.** `internal/thunderbird` reads `prefs.js`, probes the profile lock
   (`Running`, `internal/thunderbird/thunderbird.go:117`) and, for the one-time
   `-enable-offline` / `-sync-wait`, edits `prefs.js` with Thunderbird closed and then waits
   while the owner syncs by hand (`cmd/mailarchive/main.go:1388-1503`). It never starts,
   controls or quits Thunderbird.
2. **So a schedule archives what Thunderbird happened to download.** For an IMAP account, a
   message body is on disk only after Thunderbird downloaded it. Otherwise the message is
   archived as *still missing content* (`missing-body`, fillable) and filled by a later run
   (README, "IMAP: get everything downloaded first", lines 1190-1210, and the recipe table row
   at line 56). For POP and Local Folders, new mail reaches disk only when Thunderbird has
   checked the server. A laptop on which Thunderbird is not open at 02:00 archives yesterday's
   mail at 02:00 and today's tomorrow.
3. **`-enable-offline` / `-sync-wait` cannot be scheduled.** They are interactive and refused
   by `parseJob` (`cmd/mailarchive/job.go:211-213`), by design (design-schedule-v2 P2).
4. **The scheduled process has no say in any of this.** It runs from cron, a launchd
   LaunchAgent or Task Scheduler (internal/schedule), under a scheduler environment with no
   terminal, a minimal environment, and on Linux no display.

What the owner asks for is a stage in front of the run that does, unattended, what the owner
does by hand: open Thunderbird, let it fetch, quit it. The hazards are the inverse of the
tool's current safety: it would start and stop another program that may be in the owner's
hands, on a machine that may be asleep, locked or offline, and it must not turn a harmless
read into a damaged mail store.

## 2. Properties

Each property: the claim, the invariant it serves or proposes, its label, and the MA rows that
will prove it (§10).

- **P1 — Opt-in, and inert when off.** With the setting absent or false, no code of this feature
  runs: no process is listed, no profile is read, no file is written, `app.Run`'s hook is nil.
  The default is OFF in every surface. Proven (MA-546, MA-565). Proposes **R30**.
- **P2 — Only what we started, and only by asking.** The tool asks Thunderbird to quit only
  when this session started that process and has proven its identity (pid, process start time,
  executable path) at launch and again at quit. It never signals, terminates or kills
  Thunderbird, never edits, deletes or moves any Thunderbird file, sends no keystrokes and
  drives no window beyond the application's own quit request. If Thunderbird was already
  running when the session began, it is neither started nor quit. Proven (MA-551, MA-553,
  MA-554, MA-561). Proposes **R31**.
- **P3 — Never gates the archive, always loud, always bounded.** Every failure of the sync
  session ends in a warning that names what happened, why and what to do (X2), recorded in the
  run record and shown in the run summary, `status` and the GUI health view; and the archive
  run then happens anyway, because it only reads. The one stop is cancellation. Every wait has a
  bound (§4.11): the worst case of a session is about 24 minutes at the suggested defaults.
  Proven (MA-557, MA-558, MA-552, MA-553). Proposes **R32**. §4.9.1 states exactly which
  failures make the read unsafe: none does by itself, and why.
- **P4 — The sync session lives inside the run.** It runs after the archive lock is held and the
  run's `running` last-run record is written, and before any source is read. So R5 (one run per
  archive) and R18 (every run that begins is recorded) hold for the whole session, and a
  session killed mid-way is visible to `status` as a run that never finished. Proven (MA-556,
  MA-565).
- **P5 — A crash never leaves a hidden Thunderbird forever, and never costs the owner theirs.**
  A write-ahead record in the user's state directory names what was launched. The next run
  recovers from it: a Thunderbird it can still prove it started, inside a bounded window, is
  asked to quit; one it cannot prove is left alone and named in a warning. Conditional on the
  platform returning a process start time (§4.4.3); without one nothing is ever quit.
  Proven (MA-555), lab (MA-573).
- **P6 — Settings come from the schedule, never from the archive.** The profile, application
  path, quiet and maximum wait are read from the program's own arguments or the user-owned job
  file. They are never read from `<out>/.mailarchive-schedule.json` or anything else inside the
  archive folder, which anyone who can write the archive can edit. Proven (MA-559).
- **P7 — Older binaries stay harmless.** The descriptor and job-file fields are additive and
  ignored by older readers; an older binary handed the new CLI flags refuses loudly (existing
  behaviour for any unknown flag) instead of silently skipping the sync. §7. Proven (MA-548).
- **P8 — Honest about what a sync is.** "Sync" means what Thunderbird itself fetches in the
  background after it starts, in the folders it checks. It is not a guarantee that every folder
  is complete (§4.4.4, claim label **conditional**). The setting's text, `status` and the README
  say so.
- **P9 — The owner is told what Thunderbird will do.** Starting a mail client starts its own
  network activity (every account, calendars, feeds, chat, update checks) and may show
  notifications or play a sound. The text that offers the setting says so (§6).

## 3. Non-goals (explicit scope cuts)

- **Not a Thunderbird controller.** No opening of folders, no "Get messages" click, no menu
  automation, no keystrokes, no accessibility scripting, no window moving. Thunderbird fetches
  on its own at start; we wait and watch files.
- **Not a guarantee of a complete download.** No flag is flipped. `-enable-offline` stays the
  one-time prep, unchanged, and is mutually exclusive with this feature (§4.2.4). Thunderbird's
  own settings decide which folders it checks and which bodies it keeps (§4.4.4).
- **Not a way to read a Thunderbird that is open.** If the owner has Thunderbird open the run is
  today's run (§4.5 S4; D9 in §4.9.1).
- **Not a kill switch, not a watchdog, not a supervisor.** The tool never restarts, never
  re-launches a Thunderbird it started that has since closed, never holds Thunderbird open
  beyond the maximum wait.
- **Not unattended GUI automation of any kind.** In particular no `xdotool`, `wmctrl`,
  `SendKeys`, `System Events` keystroke or accessibility call.
- **No new module dependency.** macOS and Linux exec fixed system tools; Windows uses `syscall`
  / `NewLazyDLL` for `user32` and `kernel32`. `go.mod` is unchanged.
- **No second Thunderbird build.** Snap and Flatpak Thunderbird (a different process and
  filesystem namespace) are refused at schedule time. Thunderbird Beta, Daily and ESR channels
  are not recognised unless the owner names the application with `-thunderbird-app` (§4.7).
- **No Linux in v1, no cron on Linux ever (display).** §4.7.3.
- **No MailArchive Desktop control.** The desktop dashboard (`internal/desktop`) archives
  Microsoft 365 through Graph and has no Thunderbird source, so its weekly-backup schedule
  (`internal/desktop/schedule.go`) gains nothing. The only GUI surface is `mailarchive-gui`'s
  schedule step (§4.2.6).
- **No push channel beyond what exists.** `status`, the run summary and the GUI headless
  notification (§4.9). No email, no webhook.
- **No refactor of `app.Run`'s lock.** The session runs inside the lock `app.Run` already takes
  (§4.1); the hook is one optional field.
- **Not an `-input` rewriter.** The set of sources a run archives is exactly what the job names.

## 4. Mechanism

### 4.1 Where the session sits: a hook in `app.Run`, after the lock and the running record

**D1.** `app.Options` gains one optional field:

```go
// BeforeRead, when non-nil, runs once, after the archive lock is held and the "running"
// last-run record is written, and before any source is read. It returns warnings to record
// and an optional presync summary. It never fails the run; only ctx cancellation stops it.
BeforeRead func(ctx context.Context, files []string) (BeforeReadResult, error)
```

`app.Run` calls it after `beginRun(opts)` and before the walk (`internal/app/run.go`, between
the deferred `recordRun` at line 391 and the `ostAdvisory` block at 397). The returned warnings
are appended to the run record's `Warnings` and logged; the summary is stored in a new additive
`LastRun.Presync` field (§4.9.2). When `opts.Auto` is set, `DiscoverInputs` is run again after
the hook, because a Thunderbird that has just started can create an account directory the first
discovery did not see. The hook is wired by `runExport` (`cmd/mailarchive/main.go`) and
`runHeadless` (`cmd/mailarchive-gui/main.go`); `RunGraph`, `reindex`, `verify` and `extract` never
set it.

Why inside the run, not before it (decision rows):

| ID | Decision | Why | Alternative rejected |
|---|---|---|---|
| D1a | The session holds the archive lock for its whole length | R5: a second run (a manual one, a verify) cannot interleave while Thunderbird is writing and being watched; a verify that fires at the same moment refuses instead of racing | Before the lock: a scheduled run and a manual run could both start Thunderbird and then the second one's archive step refuses after 20 minutes of waiting |
| D1b | The `running` last-run record exists during the session | R18/P5 of design-schedule-v2: "every run that begins is recorded, as its FIRST action". A session before `app.Run` would leave no record if the tool were killed during it | Writing a separate record: a second source of truth `status` would have to merge |
| D1c | A hook, not a new `app.Run` parameter list | Smallest seam; nil means identical behaviour (MA-565) | A `presync` package imported by `app` — couples the run to process control |
| D1d | The hook cannot fail the run | P3: presync is never a gate | Returning an error from the hook would make a Thunderbird dialog stop the backup |

**Consequence the owner should know (conditional):** while a session runs (up to about 24 minutes)
the archive is "in use": `mailarchive status` shows an export in progress, a manual run or
`verify` refuses with the existing "archive is in use by another mailarchive run (held by
mailarchive: pid=… verb=export)" message. This is by design; it is the cost of D1a.

### 4.2 The setting: storage, flags, controls, compatibility

#### 4.2.1 The values

| Setting | Meaning | Default | Range | Why |
|---|---|---|---|---|
| sync | run the session | **off** | on / off | owner request |
| quiet | no change in the watched mail folders for this long means done | **3 minutes** | 30 s .. 30 min | owner suggestion; Q1 |
| max wait | the session gives up waiting for quiet this long after Thunderbird was launched | **20 minutes** | 2 min .. 120 min, and at least settle + quiet + 60 s | owner suggestion; Q1 |
| profile | the Thunderbird profile directory | derived (§4.3) | an absolute path | §4.3 |
| app | the Thunderbird application | the platform's fixed candidates (§4.7) | an absolute path | non-standard installs only |

Fixed, not configurable in v1: poll interval 10 s, settle 60 s (the earliest the quiet clock may
start counting after Thunderbird is up), start timeout 90 s, quit grace 120 s (§4.11).

#### 4.2.2 CLI flags (export verb and `schedule`'s flat form)

Kebab-case, `-thunderbird-` prefix as `-outlook` / `-outlook-sync-wait` set the precedent
(X4/X5). `-sync-wait` is taken by the one-time prep, so the new family does not reuse it.

| Flag | Meaning |
|---|---|
| `-thunderbird-sync` | Before the archive run, if Thunderbird is not already open, start it out of sight, wait until its mail files stop changing, ask it to quit, then archive. Off by default. |
| `-thunderbird-sync-quiet DURATION` | With `-thunderbird-sync`: how long the mail files must stay unchanged (default `3m`, 30s to 30m). |
| `-thunderbird-sync-max DURATION` | With `-thunderbird-sync`: the longest to wait for that after Thunderbird starts (default `20m`, 2m to 120m). |
| `-thunderbird-profile PATH` | With `-thunderbird-sync`: the Thunderbird profile to sync (default: the profile that holds the Thunderbird `-input` stores). Required when the inputs span more than one profile. |
| `-thunderbird-app PATH` | With `-thunderbird-sync`: the Thunderbird application (macOS: the `.app`; Windows: `thunderbird.exe`). Only for a non-standard install. |

`-h` text for `-thunderbird-sync` (exact, X3):

> Before the run, if Thunderbird is not already open, start it out of sight, wait until its mail
> files stop changing, ask it to quit, then archive. Thunderbird will check all of its accounts
> (mail, calendars, feeds, update checks) and may show notifications or play a sound. If
> Thunderbird is already open it is left alone. Off by default.

The flags work on a manual `mailarchive -input … -out …` run as well as on `schedule`
(owner question Q9): a manual run is how the owner tests the setting, and how the first-run
permission prompt on macOS is answered (§4.7.1).

`schedule` bakes them into the job like any export flag (`parseJob`, `cmd/mailarchive/job.go`
adds them to the canonical argv with `-thunderbird-profile` and `-thunderbird-app` made
absolute). They appear in the printed cron line, the plist's `ProgramArguments`, the Task
Scheduler XML and the Windows wrapper, so `schedule … ` (print only) shows the owner exactly what
will run, plus the notice of §6.

#### 4.2.3 Descriptor field (status only)

`schedule.Descriptor` (`internal/schedule/descriptor.go`) gains one additive field:

```go
Presync *PresyncDesc `json:"presync,omitempty"` // {Kind:"thunderbird", Quiet, MaxWait string, Profile string}
```

`Version` stays 1. It records what the schedule *says* it does so `status` can show the setting
and judge the last run against it, including for the GUI job whose descriptor `Job` is just
`["-job", path]`. It is **display and judgement only**: the run never reads its settings from the
descriptor (P6). The reader strips control characters from every string as it does for the other
fields (`readDescriptorFile`).

#### 4.2.4 Refusals at schedule time (P2 of design-schedule-v2: fail now, not at 02:00)

`parseJob` and the GUI schedule step refuse, naming what / why / remedy (X2):

| Condition | Refusal says (shape) |
|---|---|
| `-thunderbird-sync` with `-enable-offline` or `-sync-wait` | "`-thunderbird-sync` and `-enable-offline` cannot be combined: the one-time prep needs Thunderbird closed and you present. Do the prep by hand once, then schedule with `-thunderbird-sync`." |
| with `-outlook` | "`-thunderbird-sync` is for Thunderbird mail; `-outlook` archives Outlook." |
| platform not supported yet (§13 Q5) | "Sync Thunderbird before archiving is available on macOS in this version. On <OS> run Thunderbird yourself before the scheduled time." |
| `interval hourly` | "an hourly schedule is too frequent to start and quit a mail program each time (a sync can take up to <max> minutes); use daily or weekly." |
| no Thunderbird store among the inputs (including after `-auto` discovery now) | "none of the inputs is a Thunderbird mail folder, so there is nothing to sync." |
| inputs span more than one profile and `-thunderbird-profile` not given | lists the profiles; "one scheduled sync syncs one profile; name it with `-thunderbird-profile`, or schedule each profile separately." |
| Snap or Flatpak profile path | "Thunderbird installed as a Snap or Flatpak cannot be started and closed from here yet." |
| the profile is not a directory you own, or is group- or world-writable (Unix) | names the path and the mode. |
| application not found or not acceptable (§5.2) | names the candidate paths tried and `-thunderbird-app`. |
| `-thunderbird-sync-quiet` / `-max` out of range or `max < settle + quiet + 60 s` | names the range. |
| the tool runs as root or elevated | "do not run this as root or an administrator: Thunderbird would create profile files you cannot then open." |

Install-time advisory (read-only, bounded, interactive context only): for an IMAP store whose
account in `prefs.js` has `offline_download` off, the install prints "account `<host>` is not set
to keep messages on this computer: Thunderbird will not download message bodies, so syncing will
not make them available. Run `-enable-offline -sync-wait` once first (README, IMAP)." This reads
`prefs.js` once through the existing `parsePrefs` (to be capped at 8 MiB, §5.2), echoes control-
stripped values only, and uses no path from it. The run-time session reads no `prefs.js` at all
(§5.3).

#### 4.2.5 Job file (GUI headless) field

`internal/job.Job` gains, additively (`omitempty`, `Version` stays 1):

```go
ThunderbirdSync *ThunderbirdSync `json:"thunderbird_sync,omitempty"` // {Quiet, Max, Profile, App string}
```

Presence means ON. An older `mailarchive-gui -job` reads the file with `encoding/json`, which
ignores the unknown field (`job.Read`, `internal/job/job.go:96`), runs without the sync and
records nothing about it. That is harmless to the archive and **silent to the owner**; §7 states
the status check that makes it visible, and why the version is deliberately not bumped (bumping
to 2 would make an older GUI *refuse the whole backup*, `job.go:99`, which is worse).

#### 4.2.6 GUI wording (`mailarchive-gui` schedule step)

`offerSchedule` (`cmd/mailarchive-gui/main.go:1386`) asks its existing first question. Only when
the owner picks a daily or weekly schedule **and** the wizard's inputs include a Thunderbird
store (macOS only in v1), a second `zenity.List` question follows, default the first item:

> **Sync Thunderbird before archiving?**
>
> Before each scheduled run, Mail Archive can start Thunderbird out of sight, wait until it has
> finished fetching your mail, close it, and then archive.
>
> Thunderbird checks all of its accounts when it starts, so it may also fetch calendars and
> feeds, check for updates, show notifications or play a sound. If Thunderbird is already open
> when the backup starts, it is left alone.
>
> On a Mac, the first run may ask your permission to let Mail Archive control Thunderbird.
>
> Items: **No — archive what is already on disk** (default) · **Yes — sync Thunderbird first**

Yes writes `thunderbird_sync` into the job file (defaults 3 min / 20 min, profile and app derived
at that moment and shown in the confirmation: "Thunderbird will be synced from `<profile>`").
The final confirmation line (`zenity.Info`, line 1456) gains "Thunderbird: synced before each
run (up to 20 minutes)". The health view (§4.9.4) shows the setting and its last result beside
*Remove the scheduled backup*, and a *Turn off Thunderbird sync* action rewrites the job file
without the field and the descriptor without `presync`.

The desktop dashboard has no Thunderbird surface (non-goal).

### 4.3 The profile choice

| Case | Choice |
|---|---|
| `-thunderbird-profile PATH` given | that path, canonicalised with `filepath.EvalSymlinks`, must contain `prefs.js`. Inputs that are not inside it are not watched for quiet (they are archived as named). |
| not given | every Thunderbird store among the inputs (after `-auto` discovery, using `thunderbird.FindProfileDir`, `thunderbird.go:29`, which ascends at most six levels from the store to the directory holding `prefs.js`) is mapped to its profile. Exactly one distinct canonical profile: that one. |
| more than one distinct profile | refused at schedule time (§4.2.4). At run time, with `-auto`, if more than one appears and none was named at install, no sync runs: warning `presync_profile_unusable`, the archive runs. |
| a store with no profile above it (a store configured in a custom directory outside the profile) | no profile can be derived: warning `presync_profile_unusable` naming the store and `-thunderbird-profile`. |
| Snap, Flatpak | refused (non-goal). |

Which Thunderbird profile a launch uses is decided by us, never by Thunderbird's own default: the
launch always passes `-profile <canonical path>` (Mozilla's documented command-line option) so a
machine with several profiles never opens Thunderbird's profile chooser, a modal dialog that
would hang a hidden launch. Whether a `-profile` directory that is not listed in `profiles.ini`
starts normally is lab item L-M6.

Validation (§5.2): the profile is owned by the current user and, on Unix, neither group- nor
world-writable.

### 4.4 Deciding "Thunderbird is running", "ours" and "the mail files are quiet"

#### 4.4.1 Is Thunderbird running (the gate in front of the start)

Running = **A** or **B**:

- **A, the profile signal.** The existing `thunderbird.Running(profile)` (`thunderbird.go:117`):
  on Unix and macOS the `lock` symlink's target `host:+pid` names a live pid; on Windows
  `parent.lock` cannot be opened. Where the pid is obtainable its executable is compared with a
  Thunderbird executable; a live pid that is not Thunderbird is reported as "the profile lock
  points at process N, which is not Thunderbird" (a stale lock whose pid was reused), and treated
  as running (Mozilla itself refuses such a profile).
- **B, the process signal.** Any process named `thunderbird` or `thunderbird-bin` (macOS and
  Linux, `pgrep -x -u <uid>`; the exact tool is lab item L-M3) or `thunderbird.exe` (Windows,
  Toolhelp snapshot through `syscall`) run by the current user. A second Thunderbird started
  with a different profile hands over to the first and exits (Mozilla's single-instance remote
  mechanism), so it would not sync our profile.

Consequences (labelled **conditional**):

| Error | Cause | Consequence | Direction |
|---|---|---|---|
| False positive (says running, is not) | a stale lock with a recycled pid; an unrelated process named `thunderbird`; a profile on a network share used from another host (the pid is another host's) | no sync this run; the run is today's run; the summary says Thunderbird looks open and names which signal fired | fails safe: the archive still runs, the owner is told |
| False negative (says not running, is) | Thunderbird in a Snap or Flatpak namespace (refused earlier), the lock removed, a profile on another host | we try to start: macOS `open` activates the existing instance (harmless, `-g` keeps it behind); Windows `thunderbird.exe` hands over and exits. The launch cross-check (§4.4.3) finds no process that started after the launch, so nothing is "ours" and **nothing is ever quit** | fails safe: costs the max-wait (we wait for quiet on the owner's Thunderbird) |

#### 4.4.2 Are the mail files quiet

The watch set is fixed by construction (§5.3): the Thunderbird `-input` stores that belong to the
profile (or, with an explicit `-thunderbird-profile`, all Thunderbird stores among the inputs),
plus `<profile>/ImapMail` and `<profile>/Mail`. No path is taken from `prefs.js`, from the lock,
or from anything Thunderbird writes.

Each poll (10 s) walks the watch set without following links (`Lstat`, `ReadDir`; directories
that are links are not entered) and folds `(relative path, size, mtime in nanoseconds)` of every
regular file, `.msf` summaries included, into one 64-bit FNV-1a digest. The files' contents are
never opened. A different digest than the last poll is a **change**: it records the change time.
Bounds: at most 200,000 entries and depth 16 per walk; beyond that the walk stops, the poll is
marked truncated, and the session records the note `presync_watch_truncated` (quiet is then
judged on the part walked). Renames, new files and deletions change the digest, as do mtime-only
changes.

The quiet clock: `lastChange` starts at `up + settle` (`up` is the moment Thunderbird was proven
running), so quiet can never be declared earlier than `settle + quiet` after it started, which
stops a Thunderbird that has not yet connected from being called quiet. The session is quiet
when `now - lastChange >= quiet`.

Suspend: if the wall-clock gap between two polls exceeds 30 s (the machine slept, or a
laptop's lid closed), `lastChange` is reset to `now` and a counter is kept. The clock used is the
injected clock's wall time (`Now()`); the design needs the gap, not a monotonic reading, which on
macOS does not advance during sleep (lab item L-C3).

| Error | Cause | Consequence |
|---|---|---|
| False "quiet" | Thunderbird was waiting on a network, a server or a modal dialog (Primary Password, an OAuth sign-in, "Thunderbird crashed", "a newer version created this profile", "set as default") so nothing changed; or one very large message streams slowly with no write for longer than `quiet` | the session ends early and the archive runs on what is there. If no file ever changed, the outcome is `ok-noactivity` (§4.9.3), a note, which becomes a warning after 7 in a row (Q10): "Thunderbird started but nothing changed in <quiet>: nothing new, or it could not reach the server" |
| False "not quiet" | Thunderbird touches a watched file periodically for its own reasons (a summary-file rewrite on a timer), or a first sync of a very large mailbox that takes hours | the session stops at max wait with `presync_not_quiet` (a warning that says the archive used what had arrived and that the rest continues next run) |

Whether file silence tracks the end of an IMAP fetch (do offline stores grow as messages
arrive, are `.msf` files updated per folder) is **lab item L-M5**. If it does not, `quiet` is
only a time-based heuristic and §12 says so.

#### 4.4.3 Is a running Thunderbird "ours"

After the launch the session looks for a Thunderbird process that did not exist before: pid from
the profile lock (if it exists) plus the process list (B above) are the candidates. A candidate is
**ours** only if all hold:

1. its executable path equals the resolved application executable the session launched;
2. its process start time is at or after `launchAt - 2 s` (a start time from the platform:
   macOS `ps -o lstart=` through `/bin/ps`, Windows `GetProcessTimes`, Linux `/proc/<pid>/stat`;
   lab items L-M4, L-W2, L-L3);
3. it is the only such candidate;
4. the launch record was written before the launch (§4.6).

The same check is repeated immediately before the quit request. If the platform cannot give a
start time, no candidate is ours, no quit is ever sent, and the outcome is
`presync_identity_unavailable` (a warning; Thunderbird is left running). A Thunderbird that
started before `launchAt` is by definition not ours: it is the "already running" case reached
by a race, and is handled as such (no quit).

#### 4.4.4 What a sync can and cannot cover (P8)

Thunderbird by default checks the Inbox of an IMAP account for new mail in the background, and
keeps message bodies only for folders marked for offline use. Other folders are synchronised when
selected, or if the account's "check all folders for new messages" option is on. So this feature
syncs *what Thunderbird would sync in the background* during the session, not more. The README
says so and says how to widen it (account settings: Synchronisation & Storage, "Keep messages in
all folders on this computer"; Server Settings: check for new messages at start and every N
minutes). Label: **conditional** on those Thunderbird settings.

### 4.5 The state machine

One session per run, in this order. All durations are the §4.2.1 values; `T` is the injected
clock. A transition that touches the machine is written to the state record (§4.6) **before** it
acts, so a crash always leaves a record that over-states what happened.

| State | Entry condition | Action | Exit and next state | Timeout |
|---|---|---|---|---|
| **S0 off** | setting absent or false | none | the run continues; nothing was constructed | — |
| **S1 plan** | setting on, hook called | refuse if elevated (§5.4); resolve profile (§4.3) and application (§4.7); validate ownership and modes (§5.2); build the watch set | ok → S2; any failure → END with the matching warning (`presync_profile_unusable`, `presync_app_not_found`, `presync_refused_elevated`) | 5 s |
| **S2 profile lock** | planned | acquire `lockfile.AcquireAs(<state-dir>/presync-<profilehash>.lock, "tbsync")`, bounded wait (below) | acquired → S3; not acquired in 5 min → END `presync_profile_busy` (a note) | 5 min |
| **S3 recover** | lock held | read the state record for this profile (§4.6); apply the recovery table | clean → S4; a leftover Thunderbird of ours was quit (or left) → S4 / END per Q4; unreadable record → warn `presync_state_unreadable`, continue as if absent | 3 min (the quit grace of S7 if a leftover is quit) |
| **S4 detect** | recovered | evaluate "running" (§4.4.1) | running → END `already-running` (note) or `other-profile-running` (note); not running → S5 | 5 s |
| **S5 launch** | not running | write the **write-ahead record** (state `launching`, launch time, application path, profile, our pid and start time), fsync, then start Thunderbird (§4.7) | start call failed → clear the record, END `cannot-start` (or `no-desktop-session`); started → S6 | the start call 20 s |
| **S6 await-up** | launched | poll every 2 s for a candidate (§4.4.3) | candidate proven ours → write record `syncing` with pid and start time → S7. A candidate that started before `launchAt` → END `already-running` (race; never quit; record cleared). Nothing in 90 s → S9 with outcome `did-not-start` (no pid, so nothing to quit; the record stays `launching` until S3 of the next run decides) | 90 s |
| **S7 wait quiet** | up | poll every 10 s; fold digest; apply the quiet rules (§4.4.2); watch the process | quiet reached → S8 `ok` (or `ok-noactivity`); `now - launchAt >= max` → S8 `not-quiet`; the process disappeared → END `exited-early` (record cleared; it was closed by Thunderbird itself or by the owner); ctx cancelled → S8c | until `launchAt + max` |
| **S8 quit** | outcome decided | re-verify ownership (§4.4.3); if the owner appears active, optionally skip the quit (§13 Q2); write record `quitting`; send the quit request (§4.7); at +60 s send it once more; poll every 2 s | process gone → S9; still there at +120 s → END `quit-timeout`: record becomes `left-running` (kept, with identity), the warning names the remedy | 120 s |
| **S8c cancel quit** | run cancelled while ours is running | as S8 with a 30 s grace; then END | 30 s |
| **S9 post** | process gone | wait 3 s, re-read the lock (must be released), clear the record, reset the failure streak if the outcome was ok, release the profile lock | END | 10 s |
| **END** | — | return warnings and the presync summary to `app.Run`; the archive reads | — | — |

Every state returns to `app.Run` through END; a panic anywhere is recovered at the hook's top
(`presync_internal_error`), the profile lock is released by the deferred release, and if S5 had
been reached the record stays so the next run recovers.

**Cancellation (Ctrl-C, SIGTERM, `launchctl` stop):** the context is cancelled; S7 goes to S8c.
If the process is killed outright (SIGKILL, power loss) nothing runs: §4.6.

**Worst-case time at the defaults** (profile lock free): 5 s plan + 3 min recovery quit (only if a
leftover) + 90 s up + 20 min max wait counted from launch + 120 s quit grace + 10 s = about
24 minutes before the archive reads. The overall hard cap is one context deadline of
`max + 5 min` set at S2.

### 4.6 What is persisted where, and what happens after a crash

| What | Where | Mode | Written when | Trusted for |
|---|---|---|---|---|
| The setting | the schedule's own program arguments (cron line, plist `ProgramArguments`, Task Scheduler XML, wrapper), or the GUI job file in `os.UserConfigDir()/mailarchive/<name>.json` | per OS (job file 0600, dir 0700) | `schedule -install`, wizard | the run's behaviour |
| The descriptor's `presync` | `<out>/.mailarchive-schedule.json` | as today | `schedule -install` | status display only (P6) |
| The state record `presync-<profilehash>.json` | the state directory: `os.UserConfigDir()/mailarchive/presync/` (macOS, Linux); `%LOCALAPPDATA%\mailarchive\presync\` (Windows: the roaming profile would carry a pid to another machine) | file 0600, dir 0700 | atomically (temp + fsync + rename, `util.WriteFileAtomic0600`), before every transition that acts | recovery only; never read from the archive |
| The profile lock `presync-<profilehash>.lock` | same directory | 0600 | S2; released at END or by the OS when the process ends | serialising sessions on one profile |
| The run record's `presync` field and `warnings` | `<out>/.mailarchive-lastrun.json` | as today | with the run's records (`running` at start, finalised at the end) | what `status` shows |

`<profilehash>` is the first 8 hex of the SHA-256 of the canonical profile path (as
`schedule.DefaultNameFor` hashes an archive path). The state directory is outside the archive on
purpose: an archive folder may be on a NAS or a cloud drive and be edited by others, and a pid
means nothing on another host.

State record fields (size bound 4 KiB; unknown fields tolerated; a newer `version` is read as
unreadable): `version`, `host`, `profile`, `app`, `state` (`launching` | `syncing` | `quitting` |
`left-running` | `idle`), `tool_pid`, `tool_start`, `launch_at`, `child_pid`, `child_start`,
`deadline` (= launch_at + max + grace + 10 min), `streak` (consecutive sessions that ended in
a warning or note outcome), `streak_since`.

**Recovery table (S3).** The previous session's tool pid is dead (the profile lock was free), and
`host` equals this host (a record from another host is ignored with a note):

| Record state | Thunderbird now | Identity vs record | Action |
|---|---|---|---|
| none or `idle` | any | — | normal |
| `launching` | not running | — | clear; note "an earlier run ended before Thunderbird started or while it was starting; nothing is open" |
| `launching`, `syncing` or `quitting` | running | process start within `[launch_at - 2 s, deadline]` and executable path equals `app` | **ours (a leftover)**: policy Q4. Recommended: ask it to quit once, now (grace 120 s), warn `presync_leftover_closed` or, if it stays, `presync_quit_timeout`. Safe interim: leave it and warn `presync_leftover_found` ("a Thunderbird started by the scheduled sync on <date> is still open; quit it from the Dock or taskbar"). |
| same | running | start outside the window, or path differs, or start time unavailable | **not ours**: never touched; treated as "already running"; warn `presync_leftover_found` only if the record's `deadline` has not passed, else clear the record silently |
| `left-running` | running | as above | as the leftover row |
| `left-running` | not running | — | clear |
| any | — | `deadline` in the past by more than 24 h | clear the record (bounded memory); Thunderbird, if running, is the owner's |
| unparseable, newer version, wrong host | any | — | treat as absent, warn `presync_state_unreadable` once |

PID reuse is defeated by comparing start time and path, never the pid alone (MA-555). The
`deadline` rule bounds how long the tool will go on regarding a still-open Thunderbird as its own:
after its `deadline` (launch + max wait + grace + 10 minutes, 32 minutes at the defaults) it is the owner's, whatever its history (it may have been opened
by the owner in the meantime, and the tool cannot tell, so it stops claiming it).

**If the tool is killed while it started Thunderbird:**

| Killed at | Left behind | Next run |
|---|---|---|
| S5, before the exec | record `launching`, no Thunderbird | clear (row 2) |
| S5 after the exec / S6 / S7 | record `launching` or `syncing`, a hidden Thunderbird | the leftover row: quit it once (Q4) or warn; the lastrun is `running` with a dead pid, so `status` also shows `run_never_finished` until the next run replaces it |
| S8, after the quit request | record `quitting`, Thunderbird probably closed itself | clear (row `quitting`, not running) |
| anywhere, the tool survives but Thunderbird crashes | record `syncing` | `exited-early` in the same run, or clear on the next |

The archive lock is released by the OS on any exit (`internal/lockfile`), so a killed session
never blocks the next run.

### 4.7 Per-OS mechanics

The rules for all platforms: exec with an argument list, no shell, no `sh -c`, no `cmd /c`; the
executable is an absolute, resolved path from a fixed table or the validated `-thunderbird-app`;
**no `PATH` lookup of any name** (`exec.LookPath` is not called; a lookup seam is asserted never
used, MA-560); the child's environment is built from a short allowlist copied from our own
environment, never from job, profile or mail content (§5.5); helper children (`open`, `osascript`,
`ps`, `pgrep`, `caffeinate`) run under a context deadline and may be terminated by that deadline,
because they are ours; Thunderbird itself is never signalled.

Confidence column: **doc** = I am confident from vendor documentation or this repo; **memory** =
I believe it but have not verified it here; **unknown** = I do not know. Anything not **doc** is
a lab item that gates its slice.

#### 4.7.1 macOS (slice 1)

| Step | Exact command (argument list) | Confidence | Lab |
|---|---|---|---|
| Application | candidates in order: `/Applications/Thunderbird.app`, `$HOME/Applications/Thunderbird.app` (the home from `os.UserHomeDir`, never `$HOME` text from elsewhere); or `-thunderbird-app`. Accept only a directory ending `.app` whose `Contents/Info.plist` has `CFBundleIdentifier` `org.mozilla.thunderbird` (read as bytes, searched for the key; no plist library) and that is not world-writable | bundle id: memory | L-M1 |
| Is it running | profile lock (§4.4.1 A) and `/usr/bin/pgrep -x -u <uid> thunderbird` (B) | `pgrep -x -u` flags: doc; process name `thunderbird`: memory | L-M3 |
| Launch hidden | `/usr/bin/open -g -j <app> --args -profile <profile>`. `-g` does not bring the app to the foreground, `-j` launches it hidden, `--args` passes the rest to the app | `open -g -j --args`: doc (open(1)); that Thunderbird stays hidden and shows no window, dock bounce or focus theft: unknown | L-M2 |
| Find the pid | the profile `lock` symlink target (`host:+pid`) parsed by the existing `parseLockPID`; then `/bin/ps -p <pid> -o lstart= -o comm=` with `LC_ALL=C` in the allowlisted environment for start time and executable path (`lstart` layout `Mon Jan _2 15:04:05 2006`, local zone; `comm` full path) | `ps -o lstart`: doc; macOS `comm` giving the full path and the lock symlink existing on macOS: memory | L-M4, L-M3 |
| Keep awake | `/usr/bin/caffeinate -i -w <our pid>` started as a helper at S5, ends when we do | flags `-i`, `-w`: doc (caffeinate(8)) | L-M7 |
| Quit (the app's own request) | `/usr/bin/osascript -e 'tell application id "org.mozilla.thunderbird" to quit'`. The script text is a constant; nothing is interpolated into it. Deadline 20 s | `osascript -e`: doc; `quit` as a standard Apple event: doc; the bundle id: memory | L-M1, L-M8 |
| Quit denied | exit status non-zero with `-1743` ("not authorized to send Apple events") → outcome `quit-not-permitted`, Thunderbird left running, remedy "System Settings, Privacy and Security, Automation: allow <the program> to control Thunderbird". `-600` ("application isn't running") → treat as gone | `-1743` and `-600`: memory | L-M8 |
| Locked screen, no session | a launchd LaunchAgent in `~/Library/LaunchAgents` is loaded in the user's GUI session at login, so a locked screen still has the session; if `open` reports it cannot reach the window server, outcome `no-desktop-session` | unknown | L-M2 |
| Asleep | `StartCalendarInterval` jobs missed while asleep run on wake; the network may not be up yet, which the settle (§4.4.2) and `ok-noactivity` note absorb | memory | L-M7 |

The macOS Automation consent is the main risk of this platform. A permission to send Apple
events to Thunderbird is granted per requesting program; whether a grant given when the owner ran
`mailarchive -thunderbird-sync` from Terminal also covers the same binary run by launchd at 02:00
(the "responsible process" differs), and whether an unanswered prompt blocks `osascript` until our
20 s deadline, are **lab items L-M8**. The design's answer if consent cannot be given in
advance: the session still starts Thunderbird, waits, fails to quit with a specific warning, and
leaves it open (hidden). The next run finds it (leftover row). This is why Q4 and the SIGTERM
question in §4.7.4 matter. Do not ship TP3 before L-M8 is answered.

#### 4.7.2 Windows (slice TP6, lab-gated)

| Step | Exact mechanism | Confidence | Lab |
|---|---|---|---|
| Application | `%ProgramFiles%\Mozilla Thunderbird\thunderbird.exe`, `%ProgramFiles(x86)%\Mozilla Thunderbird\thunderbird.exe`, or `-thunderbird-app`; accept only a regular file named `thunderbird.exe`, not in a user-writable directory unless the owner named it explicitly | install paths: memory | L-W1 |
| Is it running | `parent.lock` probe (the existing `windowsLockHeld`) and a Toolhelp snapshot (`CreateToolhelp32Snapshot`, `Process32First/Next` from `syscall`) for `thunderbird.exe` of this session's user | `syscall` exports these: memory | L-W2 |
| Launch hidden | `exec.Cmd{Path: <app>, Args: [<app>, "-profile", <profile>], SysProcAttr: &syscall.SysProcAttr{HideWindow: true}}` (`HideWindow` sets `STARTF_USESHOWWINDOW` with `SW_HIDE`). Whether Thunderbird honours the hint for its first window, and whether the launcher process re-executes and exits (so the child pid is not the browser pid), are unknown | `HideWindow` exists: doc; behaviour: unknown | L-W3 |
| Find and identify | Toolhelp enumeration by name, `QueryFullProcessImageNameW` for the path, `GetProcessTimes` for creation time, §4.4.3 rules | memory | L-W2 |
| Quit (the app's own request) | `EnumWindows`, `GetWindowThreadProcessId`, then `PostMessageW(hwnd, WM_CLOSE, 0, 0)` to each top-level window of the proven pid, through `syscall.NewLazyDLL("user32.dll")`. This is the same message the window's close button sends; Thunderbird exits when its last main window closes and still asks about unsaved drafts. No `taskkill` (it would be an exec of a name resolved from the system, and without `/F` its behaviour on a hidden window is unverified) | `WM_CLOSE` closes the app as the X button does: doc (Windows API); that Thunderbird quits when its main window closes and does not stay in a tray: memory | L-W4 |
| Keep awake | `SetThreadExecutionState` with `ES_CONTINUOUS` plus `ES_SYSTEM_REQUIRED`, through `kernel32`, cleared at END | doc | L-W5 |
| Session | the task is registered with an interactive-logon identity and `LeastPrivilege` (`internal/schedule/xml.go:162`), so it runs in the user's desktop session and windows exist; a disconnected or locked session still has them; a logged-off user has no run at all (today's limit) | repo | L-W3 |

#### 4.7.3 Linux (slice TP7, not in v1)

| Step | Position |
|---|---|
| Display | cron and `systemd --user` timers have no `DISPLAY`/`WAYLAND_DISPLAY`/`XAUTHORITY`/`DBUS_SESSION_BUS_ADDRESS`. A hidden Thunderbird cannot start without a display. The session would copy those four variables from our own environment if present and valid (absolute paths, no control characters), and refuse with `presync_no_display` otherwise. Under a plain cron job it will always refuse. A virtual display (`xvfb-run`) is a different design. |
| Application | fixed candidates `/usr/bin/thunderbird`, `/usr/lib/thunderbird/thunderbird`, `/usr/lib64/thunderbird/thunderbird`; Snap (`/snap/bin/thunderbird`) and Flatpak refused |
| Launch | `<app> -profile <profile>`; **no documented hidden-start flag is known to me**. Mozilla's `-headless` is a Firefox flag; whether Thunderbird honours it for mail sync is unknown (L-L1) |
| Quit | **I do not know a documented non-signal quit request for Thunderbird on Linux.** Candidates, none verified: a Mozilla `-quit` command-line option (its existence unknown, L-L2); SIGTERM to the proven pid (graceful only if Gecko traps it, L-L3, §4.7.4); closing the window through `wmctrl -c` or `xdotool` (rejected: it is GUI automation, non-goal). Until L-L2/L-L3 answer, Linux ships nothing. |

#### 4.7.4 The one signal question

Asking an application to quit with SIGTERM is not `kill -9`, and is how a service manager asks
politely. For Thunderbird it is gentle **only if** Gecko turns SIGTERM into a normal shutdown;
if it takes the default action, SIGTERM ends the process immediately, mid-write. That would
violate P2. **The design therefore forbids SIGTERM in v1 on every platform** and treats it as a
future rung (Q3b) that may be enabled for a platform only after lab items L-M8b / L-L3 prove, on
a real profile with a large folder, that Thunderbird flushes, removes its lock and starts next
time with no crash-recovery or compaction prompt. The structural test (MA-554) forbids the
identifiers `syscall.Kill`, `Process.Kill`, `Process.Signal` and `os.Interrupt` sends in the
package, with a single named exception file added only by that future slice.

### 4.8 Concurrency

| With | Behaviour |
|---|---|
| another run of the tool on the same archive | the archive lock (`.mailarchive.lock`) is held for the whole session (D1a): the second run refuses with the existing message, before any process is touched |
| a `verify` or `reindex` on the archive | same lock; they refuse while a session runs, which is by design (D1a); the verify-vs-backup clash note in the README (line 1091) gains a sentence for sync |
| another schedule (another archive) that shares this Thunderbird profile | S2's profile lock serialises them; the second waits up to 5 minutes, then ends with the note `presync_profile_busy` and archives as today. Two sessions never start or quit Thunderbird at once |
| the owner starting Thunderbird just before the session: | S4 sees it (running) → no start, no quit |
| the owner starting Thunderbird during the session (it is hidden; they click the Dock icon, or launch it) | the owner reaches our hidden instance. The tool cannot tell (non-goal: no window inspection). At S8 it asks it to quit unless the optional owner-activity gate (Q2) says the owner is at the computer. Thunderbird's own quit still asks about unsaved drafts, so nothing is lost without the owner's click |
| the owner quitting Thunderbird during the session | `exited-early` (a warning that says the owner or Thunderbird closed it); the archive runs |
| a second Thunderbird the owner starts on another profile during the session | handed to our instance (single-instance) or independent; either way S8 acts only on the process proven ours |
| Thunderbird compacting folders while we read | an existing live-read risk of today's tool (README: Thunderbird need not be closed), narrowed (not removed) by the quit and the quiet period |

### 4.9 What the owner sees, and what is recorded

#### 4.9.1 When the sync fails, does the archive still run, and what makes the read unsafe

**Decision D9 (recommended, Q8):** the archive reads whatever the outcome, except when the run
is cancelled. The read is a read: it opens files and never writes to the profile, so no sync
outcome makes the *archive* unsafe. The cases considered and why none gates the read:

| Case | Why the read still runs |
|---|---|
| Thunderbird still running and still writing at read time (`quit-timeout`, `not-quiet`) | today's tool reads a running Thunderbird every night for an owner who never closes it. A message being appended is read as a truncated last message or skipped by the existing mbox reader's fault isolation (`folder-partially-read`, design-fault-isolation §4.1), and a later run re-reads it. Skipping the night is worse: a missed night of mail. **Lab item L-C1** checks that a truncated last message heals (identity and refill) and does not leave a stale duplicate; if it does not, the answer is a documented residual and the design's Q8 option (defer the archive to the next run when files changed within the last poll) is reconsidered |
| The profile could not be resolved or validated (`profile_unusable`) | nothing was started, so nothing about the read changed |
| A leftover, hidden Thunderbird of ours (found at S3) | it has been syncing; reading is as safe as any live read |
| The state record is damaged | affects only whether we may touch a process |
| The tool is told to run as root | the session is refused (§5.4); the archive then runs (read-only) |

**Stops (the complete list):** a cancelled run (context), and every refusal `app.Run` already
makes (archive in use, no inputs, the unattended guard for a missing `-out`). The session never
produces one.

#### 4.9.2 The run record

`state.LastRun` gains an additive, `omitempty` field (no `Version` change, as FC31(5) of
design-fault-isolation did for its fields):

```go
Presync *PresyncRun `json:"presync,omitempty"` // present only when the setting was on
// PresyncRun: Outcome string (§4.9.3), StartedByUs, QuitByUs bool, WaitedSec, QuietSec int,
// ChangedFiles int (the count of polls that saw a change), GrewBytes int64, Truncated bool,
// Streak int
```

Warnings use the existing `Warnings []RunWarning` (`internal/state/lastrun.go:81`, set from
`app.Options.Warnings`), with codes prefixed `presync_`. `internal/health/health.go:420-425`
allowlists the warning codes it will show (the record is writable by anyone who can write the
archive, so unknown codes are ignored); the `presync_*` codes are added to that list. **A
sync problem is a `RunWarning` and not a capture-gap-ledger entry** (design-fault-isolation
§4.2): the ledger records items and folders the source holds that no record represents, is inert,
and feeds `partial`; a sync outcome says no item is missing, only that the freshness of the
source is uncertain. Mixing them would make a weekend with nothing new "partial". What the
failed sync *causes* is already recorded where it belongs: messages whose bodies had not arrived
are `missing-body` fillable gaps, as today. The run summary pairs the two (§4.9.5). A
failed sync never changes the exit status (X1), never makes the run `partial`, and never
creates `BACKUP-NEEDS-ATTENTION.txt` by itself.

#### 4.9.3 Outcomes and warning codes

| Outcome | Warning code (or note) | Class | Meaning |
|---|---|---|---|
| `ok` | — | — | started by us, quiet reached, quit confirmed |
| `ok-noactivity` | note `presync_no_activity`; WARN after 7 consecutive | note | as `ok` but nothing changed during the whole session |
| `already-running` | note `presync_already_running` | note | Thunderbird was open; not started, not quit |
| `other-profile-running` | note `presync_other_profile_running` | note | Thunderbird is open on another profile; this profile was not synced |
| `profile-busy` | note `presync_profile_busy` | note | another scheduled sync holds this profile |
| `cannot-start` | `presync_cannot_start` | WARN | the start call failed |
| `no-desktop-session` | `presync_no_desktop_session` | WARN | no window server, display or session |
| `did-not-start` | `presync_did_not_start` | WARN | nothing appeared in 90 s |
| `identity-unavailable` | `presync_identity_unavailable` | WARN | no start time, so nothing may be quit |
| `not-quiet` | `presync_not_quiet` | WARN | still changing at max wait; quit was still asked |
| `exited-early` | `presync_exited_early` | WARN | closed before quiet |
| `quit-not-permitted` | `presync_quit_not_permitted` | WARN | macOS Automation denied; left running |
| `quit-timeout` | `presync_quit_timeout` | WARN | still running 120 s after the request; left running |
| `owner-active` | `presync_owner_active` | WARN | quit skipped because the owner seems to be at the computer (Q2) |
| `leftover-found` / `leftover-closed` | `presync_leftover_found` / `presync_leftover_closed` | WARN | an earlier session's Thunderbird (§4.6) |
| `profile-unusable` | `presync_profile_unusable` | WARN | no or ambiguous or unsafe profile |
| `app-not-found` | `presync_app_not_found` | WARN | no acceptable application |
| `refused-elevated` | `presync_refused_elevated` | WARN | running as root or an administrator |
| `state-unreadable` | `presync_state_unreadable` | WARN | the state record could not be used |
| `watch-truncated` | note `presync_watch_truncated` | note | the watch set exceeded its cap |
| `internal-error` | `presync_internal_error` | WARN | a recovered panic |
| `cancelled` | — (the run's own cancelled status) | — | the run was cancelled |

**Escalation without latching:** a note or warning outcome increments `streak` in the state
record, an `ok` resets it. At 3 consecutive warning outcomes `status` adds
`presync_stuck` (WARN, never RED, mirroring FC18's "stuck is never RED"): "Thunderbird has not
synced on the last N runs". Notes escalate to WARN at 7 (`already-running` does not: an always-open
Thunderbird is a fine state, and is also a sync). RED stays reserved for run-level failure and
corruption.

#### 4.9.4 Wording (X2: what / why / remedy; X8: stderr for the conversation)

Run summary, a successful session (stderr, the run log):

```
Thunderbird sync: started Thunderbird out of sight, waited 6 min 40 s until its mail files had been unchanged for 3 min (they grew by 14.2 MiB), and closed it. Thunderbird fetched mail for all its accounts, so it may have shown notifications.
```

Already open:

```
Thunderbird sync: Thunderbird was already open, so it was not started or closed. Archiving what is on disk now.
```

A failure (one warning per line, then the archive's own lines):

```
WARNING (Thunderbird sync): Thunderbird did not close within 2 min after it was asked to quit, so it was left open.
  Why: it may be showing a dialog (an unsent message, a password prompt) or still be working.
  Do this: open Thunderbird and quit it, or run again once it is closed. The archive ran anyway; mail that arrived after the sync may not be in it yet.
```

When the sync failed and the run has still-missing bodies:

```
12 message(s) are still missing content. The Thunderbird sync did not complete this run (see above), which is the usual reason.
```

`status` (X6), with the setting on:

```
Thunderbird sync: on · quiet 3 min, up to 20 min · profile <path>
                  last run 02:00 → ok · started and closed Thunderbird · 6 min 40 s · mail files grew 14.2 MiB
```

or `WARN presync_quit_timeout: …` with the same text as the summary, each naming its remedy. A
schedule that has the setting on but whose last run has no `presync` record (an older binary ran it,
or no run since install) gives WARN `presync_not_recorded`: "this schedule is set to sync
Thunderbird, but the last run did not report a sync: run `mailarchive -thunderbird-sync …` once
by hand (it also lets macOS ask your permission), or re-install the schedule from this
version". `status -json` gains an additive `thunderbird_sync` object (`JSONVersion` stays 2, the
house rule at `internal/health/health.go:1667`).

Report: `attachments-report.tsv` is unchanged (a sync outcome is not an item). GUI: the health view
shows the same lines (X10); `runHeadless` raises one best-effort desktop notification, "Mail
Archive backup ran, but Thunderbird could not be synced (see the log)", for a WARN outcome, never
for a note or an ok.

#### 4.9.5 Order in the summary

The session's lines come first (they happen first), then the archive's `Done.` block; if the
archive then reports missing content, the pairing line above is printed in the "Verification"
group.

### 4.10 Failure table

Every row: the run still archives, exit unchanged, class as §4.9.3. "Owner sees" is the `what`
sentence; the `why` and `do this` follow it in the actual text.

| # | Cause | Owner sees | The run still | Remedy shown |
|---|---|---|---|---|
| F1 | Thunderbird already open | "Thunderbird was already open, so it was not started or closed" (note) | archives what is on disk | none needed; to have it synced anyway, leave it open |
| F2 | Thunderbird open on another profile | "Thunderbird is open with a different profile, so <profile> was not synced" | archives | quit that Thunderbird or open this profile in it |
| F3 | cannot start (exec error, bad app) | "Thunderbird could not be started: <reason>" | archives | check the application path, `-thunderbird-app`; reinstall Thunderbird |
| F4 | no desktop session (login screen, SSH, no display, FileVault pre-login) | "there is no desktop session to open Thunderbird in" | archives | a scheduled run needs you logged in; on Linux cron cannot do this |
| F5 | profile locked by a Thunderbird we cannot see, or a stale lock | "Thunderbird would not start: its profile is locked by process N (not Thunderbird)" | archives | quit Thunderbird fully or restart the computer; the tool never deletes a lock |
| F6 | never reaches "up" in 90 s | "Thunderbird did not start within 90 s" | archives | open Thunderbird once by hand (it may be waiting at a dialog, a profile prompt, an update) |
| F7 | waiting at a modal (Primary Password, OAuth sign-in, crash dialog, newer-profile prompt) | `ok-noactivity` note or `not-quiet`; after 7 nights a warning "Thunderbird started but nothing changed" | archives | open Thunderbird once and sign in, and tick "remember"; clear the dialog |
| F8 | never quiet within max wait (first huge sync, a chatty folder) | "Thunderbird was still fetching after 20 min; the archive used what had arrived" | archives; the rest arrives next run | raise `-thunderbird-sync-max`, or open Thunderbird once to finish the first download |
| F9 | Thunderbird closed during the session | "Thunderbird was closed (by you, or it quit) after N min" | archives | none if you closed it; otherwise check Thunderbird crash reports |
| F10 | macOS Automation permission denied | "Thunderbird could not be asked to quit: macOS has not allowed <program> to control it. Thunderbird was left open." | archives; Thunderbird stays open hidden | System Settings, Privacy and Security, Automation: allow it; or quit Thunderbird yourself |
| F11 | quit request ignored (a dialog, unsent mail) | "Thunderbird did not close within 2 min after it was asked to quit" | archives | quit Thunderbird; check for an unsaved message |
| F12 | owner seems active (optional gate, Q2) | "Thunderbird was left open because you appear to be using the computer" | archives | none |
| F13 | the tool was killed in the last run mid-session | "an earlier run ended while Thunderbird was open; <action taken>" | archives | per row of §4.6 |
| F14 | the machine slept mid-session | the suspend counter resets the quiet clock; if max passes: F8 | archives | none |
| F15 | the machine is asleep or off at the scheduled time | no run (today's limit); `status` shows the last run | — | as today |
| F16 | locked screen | the session proceeds (a locked session is still a session); if Thunderbird cannot start, F3/F4 | archives | lab L-M2 decides whether this works |
| F17 | profile not found, more than one, unsafe mode or owner | "the Thunderbird profile could not be chosen safely: <why>" | archives | `-thunderbird-profile` |
| F18 | running as root or an administrator | "Thunderbird sync was not run as an administrator" | archives | run the schedule as your own user |
| F19 | the state directory is unwritable | "Thunderbird was not started because the tool could not record that it would" | archives | fix the directory; the tool never starts Thunderbird without its write-ahead record |
| F20 | another scheduled sync holds this profile | "another backup is syncing this Thunderbird profile" (note) | archives | none |
| F21 | an unexpected error in the session | "the Thunderbird sync failed unexpectedly: <bounded text>" | archives | report it; turn the setting off |
| F22 | no network (offline laptop, VPN down) | indistinguishable from "nothing new" (L-M5): `ok-noactivity` | archives | the note's text names this possibility |
| F23 | Thunderbird crash-recovery prompt after an earlier forced kill | as F7 | archives | open Thunderbird once |

### 4.11 Resource and time bounds

| Resource | Bound |
|---|---|
| Wall time | worst case about 24 min at the defaults: 90 s up, max wait 20 min from launch, 120 s quit grace, a few seconds either side; a hard context deadline of `max + 5 min`; the profile-lock wait is a further 5 min only when another schedule holds the profile |
| Processes started | Thunderbird once; helpers: `open` once, `osascript` at most twice, `ps`/`pgrep` a few per session, `caffeinate` once; none retried in a loop |
| Quit requests | at most 2 per session |
| Memory | O(1) beyond one `ReadDir` listing at a time; a running digest, no list of files |
| CPU and I/O | one metadata walk of the watch set per 10 s, at most 200,000 entries, no content reads; at most about 150 walks per session |
| Disk | one state file of at most 4 KiB per profile and one empty lock file; nothing grows; no log of its own |
| Log lines | at most one progress line per minute plus the session lines |
| Network | none by the tool; Thunderbird's own (§6) |

## 5. Security

### 5.1 Trust boundaries

1. **Scheduler → program.** The arguments are the owner's (written by `schedule -install` or the
   wizard). Trusted like `-out`.
2. **User config directory → session.** The GUI job file and the state directory are the user's
   (0600/0700). Same-user processes are out of scope (DC4, Type I: they can edit the launchd plist
   or crontab and run anything).
3. **Archive directory → session.** Not trusted for settings or state (P6). The descriptor's
   `presync` is read by `status` only.
4. **Thunderbird profile → session.** Untrusted *content*: `prefs.js`, the `lock` symlink, folder
   names, `.msf` names and sizes may be hostile (a copied or inherited profile, a hostile extension).
   The session `Stat`s `prefs.js` (existence), reads the `lock` symlink target (a pid), and walks
   directory entries. It opens no file in the profile, writes none, and parses no `prefs.js`.
5. **Session → Thunderbird.** One launch, one quit request. No other channel.
6. **Session → OS helpers.** Fixed absolute tools, fixed argument shapes (§4.7).

### 5.2 Untrusted inputs and how each is handled

| Input | From | Handling |
|---|---|---|
| Profile path | the job, `-thunderbird-profile` | canonicalised (`EvalSymlinks`); must be a directory owned by the current user (Unix `Stat_t`), not group- or world-writable, containing `prefs.js` as a regular file; control characters refused; a path whose canonical form changes between S1 and S5 is refused (checked again at S5). Windows ownership is not checked (no portable API; stated, as for the secret file in design-schedule-v2 non-goals) |
| Application path | candidates, `-thunderbird-app` | absolute, resolved, regular file (Windows) or `.app` directory (macOS) with the bundle id; not world-writable; basename `thunderbird.exe` (Windows) |
| `prefs.js` (install-time advisory only) | the profile | read capped at 8 MiB through the existing `parsePrefs`; only `hostname` and `offline_download` values used, echoed control-stripped; never a path |
| `lock` symlink target | the profile | parsed as a pid with `parseLockPID`; the pid is never acted on until its executable path and start time match the launch (§4.4.3); never followed as a path |
| Folder and file names under the watch roots | mail, Thunderbird, any writer of the profile | never executed, never passed to a process, never interpolated; hashed; logged control-stripped and bounded; links not followed; entry and depth caps |
| The state record | the user's state dir | bounded to 4 KiB, schema-checked, host-checked, newer version = unreadable; control-stripped; confers no authority beyond "identify a process by start time and path" |
| Process list and `ps` output | the OS | parsed with strict layouts; anything unparseable = identity unavailable = nothing is quit |
| Environment | the scheduler | an allowlist is copied (§5.5); everything else dropped |

### 5.3 Attacker personas

| Persona | Attack | Mitigation | Residual / label |
|---|---|---|---|
| **Hostile profile or `prefs.js`** (a profile copied from elsewhere, an inherited store, a hostile add-on) | `directory` prefs pointing at `/` or a share to make the watcher walk or hang; a huge `prefs.js`; control characters to forge log lines; a `lock` symlink naming pid 1 or our own pid to get an innocent process "quit" | the run-time session reads no `prefs.js` and takes no path from the profile (watch roots are fixed: the named stores and `<profile>/ImapMail`, `<profile>/Mail`); the install advisory is capped and never uses a path; a lock pid is only a hint checked against executable path and start time; a quit goes by bundle id (macOS) or by windows of the proven pid (Windows), never by a pid from the lock alone. Proven (MA-559) |
| **Planted executable path** (another local account, malware without the user's rights, a writer of the archive folder) | a world-writable app or `-thunderbird-app` pointing at a script; the schedule or descriptor edited to name a different binary | settings never come from the archive (P6); the app must be an absolute resolved path from the fixed table or the owner's explicit `-thunderbird-app`, not world-writable, with the bundle id (macOS) / basename (Windows); no `PATH` lookup; the launch passes only `-profile <path>`. A same-user attacker who can edit the user's launchd plist already controls what runs (DC4) | Type I residual: a same-user attacker. On macOS the check proves the bundle id, **not** that it is Mozilla's: signature pinning (codesign Team ID) is not in v1 (Q12) |
| **Malicious mail-folder name** | a folder named `$(…)`, with a newline, `..`, RTL controls, 4,000 characters or a name that looks like a flag; deep or looping nesting to exhaust the walker | names are never used as a command, a flag or a path component beyond `Lstat`/`ReadDir` of what the OS lists; control-stripped and bounded in logs; no link following; 200,000-entry and depth-16 caps; the digest stores no names. Proven (MA-559) |
| **Co-resident process, another user on the machine** | read or tamper the state record or lock to make the tool quit another process; hold the profile lock; race the launch | state dir 0700 under the user's config dir; the profile must be owned by the user and not group- or world-writable; a quit needs the identity match; lock held by another account cannot be created in our 0700 dir. Windows: the same-user and ACL limits of design-schedule-v2 apply |
| **Co-resident process, same user** | impersonate Thunderbird by naming a process `thunderbird`; plant a fake `lock` | the identity check needs path and start time; a fake process cannot make its image path the resolved application. Same-user malware can do everything the user can (DC4) | Type I |
| **Archive writer** (a NAS or cloud folder others can edit) | edit the descriptor so `status` shows a false sync or a path | `status` is display only and strips control characters; the run ignores the descriptor; the tool never executes anything named in it. Proven (MA-559) |
| **Mail sender** | a message that exploits Thunderbird, now triggered by an unattended start | Thunderbird processes incoming mail whenever it runs; an unattended hidden run does not widen what a message can do but removes the owner's chance to notice. Stated in §6; no mitigation here |
| **A network on the path** | Thunderbird's own traffic | Thunderbird's TLS; nothing by the tool (no new connection, no credential) | Type I |

### 5.4 What the tool must never do (each is a row in §10)

1. Kill, terminate, signal or `taskkill` Thunderbird. It may terminate only its own short-lived
   helpers (`open`, `osascript`, `ps`, `pgrep`, `caffeinate`) at their deadline.
2. Edit, create, delete, rename or touch any file in the Thunderbird profile or application,
   including `lock`, `parent.lock`, `.parentlock`, `prefs.js`, `.msf` files and mail folders.
   (`-enable-offline` keeps its existing, separate, mutually exclusive prefs edit.)
3. Send keystrokes, mouse events or accessibility actions; use `System Events`; click or type in
   a window; move, resize or raise a window; use `xdotool`, `wmctrl`, `SendKeys`, `AutoHotkey`.
4. Ask for or use elevated privilege: no `sudo`, no UAC `runas`, no setuid helper. The session
   refuses when the effective uid is 0 or the Windows token is elevated (`presync_refused_elevated`),
   because a Thunderbird started as root writes root-owned profile files the owner then cannot use.
5. Start Thunderbird without its write-ahead record, quit one that it has not proven it started,
   or quit twice more than the bounded two requests.
6. Read a message body, a subject or any content of the profile (the watch uses metadata only).
7. Run a shell, resolve a command through `PATH`, or interpolate any value into a script text.
8. Take a setting, a path or a process id from the archive directory.

### 5.5 Environment hygiene

The child environment is exactly an allowlist of variables copied from the tool's own environment
(macOS: `HOME`, `USER`, `TMPDIR`, `LANG`; Windows: `SystemRoot`, `SystemDrive`, `USERPROFILE`, `APPDATA`,
`LOCALAPPDATA`, `TEMP`, `TMP`, `USERNAME`, `HOMEDRIVE`, `HOMEPATH`, `ProgramFiles`, `ProgramFiles(x86)`,
`windir`; Linux (TP7): those four display variables plus `HOME`, with validation) plus `LC_ALL=C` for the
`ps` helper only. `DYLD_*`, `LD_*`, `MOZ_*`, `GTK_*`, `PATH` and every other variable are dropped: a
planted `DYLD_INSERT_LIBRARIES` or `MOZ_*` must never reach Thunderbird. On macOS `open` launches
the application through Launch Services, so the child does not inherit our environment at all;
the allowlist still applies to `open` itself. Proven (MA-560).

### 5.6 Explicit non-claims

Not a defence against a same-user attacker (DC4). Not a defence against a Thunderbird that is
itself compromised. Not a guarantee that Thunderbird's own quit completed a clean shutdown (we ask,
we wait for the process to go, we do not inspect its files). Authenticity of the application is not
checked beyond the macOS bundle id (Q12). Windows profile ownership is not checked.

## 6. Privacy (the owner-facing text)

Starting a mail client starts what that client does on start. The text that offers the setting
(`-h`, the schedule preview, the GUI question, the README) says, in these words or the
equivalent:

> Syncing Thunderbird starts Thunderbird on your computer at the scheduled time, with no one
> there. Thunderbird will contact your mail servers for every account in this profile, and may
> also fetch calendars, address books, feeds and chat accounts, check for updates, show
> notifications and play a sound. Any message waiting in Thunderbird's Outbox may be sent when it
> goes online. It runs out of sight, but its icon may appear in the Dock or the taskbar. If you
> are using Thunderbird when the backup starts, it is left alone.

This is **conditional** on how Thunderbird is configured and is a lab observation to record
(L-C5: with a real profile that has a calendar, an Outbox item and a chat account, what leaves
the machine and what is sent). The tool itself opens no new connection and handles no credential.

## 7. Migration and coexistence

| Direction | Behaviour |
|---|---|
| New binary reads an older descriptor, job file or last-run record | all lack the new fields; setting off; nothing changes. A schedule that was off stays off. No migration |
| Older binary reads the new descriptor | `json.Unmarshal` ignores the unknown field (`readDescriptorFile`); `status` of the older binary shows what it always showed. Harmless |
| Older GUI binary reads a job file with `thunderbird_sync` | ignores it (`job.Read`): the backup runs **without** the sync and without saying so. The new `status`/health view then shows `presync_not_recorded` (§4.9.4) because the run record carries no `presync` field. `Version` is not bumped: an older GUI would otherwise refuse the whole backup (`internal/job/job.go:99`) |
| Older CLI binary runs a schedule that has `-thunderbird-sync` | `flag provided but not defined`: the run fails loudly, is recorded failed (an unattended refusal is a run, `RecordRefusedRun`) and `status` is RED. The README rollback note already says: `schedule … -remove` first, because entries written by a new version carry flags older releases do not know (README line 1009). That note gains this feature |
| Mixed versions sharing a profile | both read and write the same versioned state record; a newer record is read as unreadable by an older writer, which then treats the profile as having no record: it will never quit a process (it has no code to) |
| Rolling the feature back | `schedule … -install` without the flag (or *Turn off Thunderbird sync*); the state files stay (≤ 4 KiB each) and are inert; deleting `<UserConfigDir>/mailarchive/presync/` removes them |

Order of introduction (no flag day): fields and the hook land first, dormant (TP1, TP2); the
setting appears only with the macOS adapter (TP3 onward). The setting is per schedule, so no
existing schedule changes behaviour on upgrade.

## 8. Operator walk (the owner, on a Mac, CLI)

1. `mailarchive -thunderbird-sync -input ~/Library/Thunderbird/Profiles/x.default/ImapMail/mail.example.com -out ~/Archive` once by hand. The log says what it will do, Thunderbird appears in the Dock, the run waits, Thunderbird closes, the archive runs. If macOS asks "mailarchive wants to control Thunderbird", the owner allows it.
2. `mailarchive schedule -out ~/Archive -input … -thunderbird-sync -install`. The preview shows the flags and the notice of §6 first.
3. Each night, 02:00: nothing visible unless Thunderbird shows a notification. In the morning `mailarchive status -out ~/Archive` shows "Thunderbird sync: … last run 02:00 → ok".
4. A night the owner is up: Thunderbird is open: "Thunderbird was already open, so it was not started or closed."
5. A night with a Primary Password prompt: a warning in `status` after the first run, and a nightly warning after: "open Thunderbird once and sign in".

## 9. Test strategy

### 9.1 Seams

A package `internal/presync` takes one `Env` struct of small interfaces, so the whole state
machine is deterministic and needs no real Thunderbird, process or wall clock:

| Seam | Real | Fake (in tests) |
|---|---|---|
| `Runner` (`Run(ctx, Cmd{Path, Args, Env})`, `Start`) | `exec.CommandContext` per OS | records every call (path, argv, env); returns scripted results, including `-1743`, timeouts, exec errors |
| `Clock` (`Now`, `Sleep(ctx, d)`) | `time` | a manual clock: tests advance it; a sleep returns immediately and moves time; a "suspend" is a jump |
| `FS` (`Lstat`, `ReadDir`, `Readlink`, `ReadFile` capped, `WriteAtomic`) | `os` | an in-memory tree with scripted mtimes and sizes, call counts per opened file |
| `Procs` (`List`, `StartTime`, `ExePath`, `Alive`) | per-OS (`ps`, `/proc`, Toolhelp) | a table of processes the fake Thunderbird creates |
| `Lock` | `lockfile` | in-memory with a held-by-other mode |
| `Notify`, `Elevated`, `Wake` | per-OS | recorders |

A **fake Thunderbird** is a scripted actor on the fake clock: it appears after `Start` (or never),
changes watched files on a schedule (bursts, a long silence, a periodic touch, none), goes quiet,
ignores the first quit request, exits on the second or never, crashes mid-way, or hands over.
Mutation checks use the real code with one assertion deleted (§10 "Prove-fail").

`app.Run` is tested with a real temp archive and a fixture mbox store, and a fake `BeforeRead` for
the seam rows. No test starts, signals or inspects a real Thunderbird, and none depends on the
developer's profile or processes.

### 9.2 What each tier proves

U rows prove behaviour on every commit; S rows are structural walks (no kill identifiers, every exec
behind the seam, the argv catalog, the hook's position); L rows are lab rows marked pending (this
repo has no lab tier, `CLAUDE.md`), each stating its lab story.

### 9.3 Row-by-row catalog

See §10.

### 9.4 The lab script (lab rows MA-570..MA-574; results recorded in the review file, not in this design)

Run on a real Mac with a real Thunderbird profile that has one IMAP account with offline storage
and one POP or Local Folders account, an Outbox with nothing in it for the first pass. The human
runs the commands; each step records what was observed. A step whose expectation is false is a
finding for the design.

- **L-M1 identity.** `plutil -p /Applications/Thunderbird.app/Contents/Info.plist` (expect `CFBundleIdentifier` `org.mozilla.thunderbird`).
- **L-M2 hidden start.** With Thunderbird quit: `open -g -j /Applications/Thunderbird.app --args -profile "<profile>"`. Observe: no window, Dock icon, focus change; whether a window appears after some seconds; with the screen locked and the display asleep (start it from `ssh` into the logged-in user's session with `launchctl asuser <uid> /usr/bin/open …` or from a launchd test job), and with no one logged in.
- **L-M3 detection.** While running: `ls -l "<profile>/lock"` (expect a symlink `…:+<pid>`), `/usr/bin/pgrep -x -u $(id -u) thunderbird`, `pgrep -x thunderbird-bin`; record the process name.
- **L-M4 identity data.** `/bin/ps -p <pid> -o pid=,lstart=,comm=` with `LC_ALL=C`: record the exact `lstart` and `comm` strings; do it while a second `open` of the same app is made (expect the pid and start unchanged).
- **L-M5 quiet.** Add a message on the server out of band (send the account an email), start hidden, and log `stat` (size and mtime) of every file under `ImapMail/<acct>` every 2 s into a file; record when the first change, the last change and the visible new message appear, whether `.msf` changes with no mail, and how long the digest was quiet before and after. Repeat with the network off (expect no change). Repeat with a folder that is not selected for offline use.
- **L-M6 profile.** Start with `-profile` pointing at a profile not in `profiles.ini`; start twice; start with another profile already running.
- **L-M7 sleep.** Schedule a test launchd job at a time the Mac is asleep; record whether it runs on wake, how long until the network is up, and whether `caffeinate -i -w` holds the Mac awake for 25 minutes on battery and on power.
- **L-M8 quit.** From Terminal: `osascript -e 'tell application id "org.mozilla.thunderbird" to quit'` (first time: record the prompt and which program name it names); then from a launchd test job running the same binary at 02:00 with the screen locked: record the exit status, whether a prompt waited, and whether a grant made from Terminal covers launchd. Repeat with an unsent draft open (expect a dialog and no quit) and with a Primary Password prompt open. **L-M8b**: on a disposable copy of the profile with a large folder, record the effect of `kill -TERM <pid>`: does Thunderbird flush, remove `lock`, and start next time without a recovery prompt?
- **L-C1 truncated last message.** With a script appending to an mbox in Thunderbird's format, run `mailarchive` repeatedly while it appends; record whether a truncated message is archived, how it is classified, and whether the next run replaces it or leaves a duplicate.
- **L-C2 kill drills (MA-573).** Start a session; `kill -9` the `mailarchive` pid at each state (during up, syncing, after the quit request); rerun; record the state record, the process list, the warning shown. Power-cycle drill at "syncing" on a spare machine.
- **L-C3 suspend.** Close the lid during S7 for 10 minutes; record that the quiet clock reset and the session ended.
- **L-C4 modals.** Start with a Primary Password set; with an expired OAuth token; with a profile created by a newer Thunderbird: record no activity and whether quit is honoured under the modal.
- **L-C5 privacy (MA-574).** With a calendar, a chat account and an Outbox message: record what Thunderbird sends and what it shows; write the measured list into §6's text.
- **L-W1..L-W5 and L-L1..L-L3:** the same shape, using Task Scheduler (interactive) with `thunderbird.exe` hidden launch, Toolhelp enumeration, `PostMessage(WM_CLOSE)` to the hidden window, `SetThreadExecutionState`; and on Linux a `systemd --user` timer with the display variables, the `-headless`/`-quit` existence question and SIGTERM.

## 10. Catalog rows proposed (nothing is added to the catalog here)

Ids are provisional (MA-546..MA-574 are the next free overflow ids per the rulings; R30..R32,
S53..S58, X16 follow R29, S52, X15). Each lands with its slice's catalog-first PR.

### 10.1 Invariants

- **R30 — Thunderbird pre-sync is opt-in and inert when off.** With the setting absent or false
  no process, profile or state-directory operation of this feature occurs, on any surface,
  default OFF. The setting is read only from the program's own arguments or the user-owned job
  file, never from the archive directory.
- **R31 — The tool starts and quits only what it can prove it started, only by asking.** A
  Thunderbird that was running when a session began is never started or quit. A quit request is
  sent only to a process identified by executable path and start time at launch and again at the
  request, at most twice; Thunderbird is never signalled or terminated, its files are never
  written, and no keystroke or window automation occurs. Nothing is quit when identity cannot be
  proven.
- **R32 — A sync never gates the archive, is always loud, and is always bounded.** Every outcome
  other than success is recorded as a specific warning (what, why, remedy) in the run record and
  shown by the summary, `status` and the GUI health view; the archive runs after every outcome
  except cancellation; every wait is bounded; a session killed at any point leaves a state the
  next run recovers from without quitting a process it cannot prove it started.

### 10.2 Scenarios

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S53 The owner schedules a Thunderbird archive with "Sync Thunderbird before archiving", and at 02:00 Thunderbird is closed | R30, R31, R32, R18 | Thunderbird is started hidden, left until its mail files are quiet, asked to quit, and the archive then reads; the summary and `status` say so | MA-546, MA-547, MA-549, MA-552, MA-553, MA-558, MA-566 |
| S54 The owner has Thunderbird open when the run starts, or opens it during the session | R31, R32, R5 | already open: not started, not quit, the run is today's run and says so; opened during the session: the quit is still only a request and unsaved work blocks it | MA-550, MA-551, MA-556, MA-558 |
| S55 The tool is killed, crashes or loses power while it started Thunderbird | R31, R32, R18 | the next run reads the write-ahead record: a Thunderbird provably ours inside the window is asked to quit, anything else is left alone and named; no hidden Thunderbird is left unannounced | MA-555, MA-562, MA-573 |
| S56 The sync cannot complete: cannot start, no session, never quiet, a modal, Automation denied, will not quit, profile locked, machine asleep or screen locked | R32, R31, X2 | a specific warning with its remedy, the archive still runs, Thunderbird is left running if it would not quit; nothing is forced | MA-557, MA-558, MA-567, MA-568, MA-570, MA-571 |
| S57 A hostile profile, `prefs.js`, lock, mail-folder name, application path, environment or archive-resident descriptor | R31, R30 | nothing outside the fixed watch roots is walked, no path is taken from the profile or the archive, no PATH lookup, a pid from a lock is never acted on alone, no shell | MA-554, MA-559, MA-560, MA-561, MA-569 |
| S58 An older binary meets the schedule, job file, descriptor or run record | R32, R12 | an older CLI refuses the unknown flag loudly; an older GUI or descriptor reader ignores the field harmlessly; the new `status` says the sync was not reported | MA-548, MA-558 |

### 10.3 UX rule

- **X16 — A setting that makes the tool act on another application says so, in owner words, at
  the point of choosing it.** The `-h` text, the schedule preview, the GUI question and the README
  state what that application will do (network, notifications, sounds, its other accounts),
  that it is off by default, and what is left alone. Proven structurally (MA-564), Partial until
  its friction review closes.

### 10.4 Test-spec rows

Tier U = every commit; S = structural; L = lab, pending. The last column is the mutation that must
turn the row red before it is reverted.

| ID | Tier | Asserts | Covers | Prove-fail |
|---|---|---|---|---|
| MA-546 | U | with the setting absent or false (every surface: flags, job file, descriptor), `app.Options.BeforeRead` is nil, the fake `Env` records zero calls (no exec, no `Lstat` of a profile, no state-directory write), and the run's output equals a golden run without the feature; positive twin with the setting on | R30, S53 | default the flag to true → the zero-calls assertion fails |
| MA-547 | U | `parseJob` and the GUI step refuse each §4.2.4 condition naming what, why and remedy with the side effect absent (nothing installed); the baked argv is canonical and absolute; ranges and `max >= settle + quiet + 60 s`; positive twin | R30, S53, X16 | drop the hourly refusal → the hourly case installs |
| MA-548 | U | an older-shaped `Descriptor`, `job.Job` and `LastRun` (structs without the new fields) round-trip against current files carrying them, other fields intact; the current reader of an old file reports "not configured"; health shows `presync_not_recorded` for a configured schedule whose last run has no `presync`; an older-flag CLI argv refuses with "flag provided but not defined" and records the run failed | R32, S58 | switch the decoders to `DisallowUnknownFields` → the older-reader case fails |
| MA-549 | U | the happy path on the fake clock: the exact transition sequence S1..S9, the exact argv of each helper and of the launch, the write-ahead record is on the fake disk before the first `Start`, the record is cleared at the end, the streak is reset, worst-case time at the defaults is within 24 min | R30, R31, R32, S53 | write the record after the launch → the ordering assertion fails |
| MA-550 | U | Thunderbird running by signal A, by signal B, by both, and on another profile: zero `Start` and zero quit requests, the outcome and note are exact, the archive runs and says so; stale lock with a recycled non-Thunderbird pid reports "not Thunderbird" and does not start | R31, S54 | skip the running check → a launch is recorded |
| MA-551 | U | ownership: a candidate that started before `launchAt - 2 s`, one with another path, two candidates, and no start time each produce zero quit requests and the right outcome; a single matching candidate is quit once; the check is repeated before the request; a pid reused with another start time is not ours | R31, S54, S55 | compare the pid only → the reused-pid case quits |
| MA-552 | U | quiet decision on the fake clock: growth, an mtime-only touch, a new file, a rename and a deletion each reset it; no change at all gives `ok-noactivity` at exactly settle + quiet; a 30 s+ gap resets (suspend); max wait ends the wait at exactly `launchAt + max` with `not-quiet`; entry and depth caps give `watch-truncated`; links are not entered; no file is opened (the fake counts opens: zero) | R32, S53, S56 | ignore mtime → the touch case is not seen |
| MA-553 | U | the quit procedure: one request, one more at +60 s, none after +120 s, the exact argv, a quit that works on the second request, one that never works leaves `left-running` with identity, the outcome is `quit-timeout`; the cancel path uses the 30 s grace | R31, R32, S53, S56 | add a third request → the count assertion fails |
| MA-554 | S | an AST and import walk of `internal/presync`: every exec site goes through `Runner`; no `sh`, `bash`, `cmd`, `-c`, `/c`; no `exec.LookPath`; paths passed to `Runner` are built only from the resolved candidates; none of `syscall.Kill`, `Process.Kill`, `Process.Signal`, `taskkill`, `-9`, `SIGKILL`, `SIGTERM` appears (a single named exception file is allowed only when slice TP8 is accepted); no write function is called on a profile-derived path; the argv catalog (per OS, every helper and its argument shapes) equals the golden list | R31, S57 | add a `Process.Kill()` call → the walk fails |
| MA-555 | U | recovery table: every row of §4.6 as a table test (state × running × identity × age × host): clean, leftover quit once, leftover left and warned, not ours, record cleared past 24 h, other host ignored, torn or newer record = unreadable and warned; a leftover is never quit without identity; after `deadline` the process is the owner's | R31, R32, S55 | match on pid only → the reused-pid row quits |
| MA-556 | U | `app.Run` calls the hook once, after the archive lock and the `running` record, before any read; a second concurrent `Run` refuses with the existing in-use message and starts nothing; two sessions on one profile serialise through the profile lock (the second waits, then `profile-busy` after 5 min, zero starts); a manual `verify` during a session refuses | R30, R32, S54 | call the hook before `lockfile.AcquireAs` → the order assertion fails |
| MA-557 | U | for each outcome in §4.9.3 (including a panic in the session and a state-dir write failure) `app.Run` still archives the fixture mbox store, returns nil, writes `status: ok` with the warning recorded, and the exit is unchanged; cancellation is the only early stop and still sends the cancel-path quit for a process of ours | R32, S56 | return the session error from `Run` → the run fails |
| MA-558 | U | surfaces: each warning code's summary text, `status` text, `status -json` additive keys (`JSONVersion` 2), run-record field and GUI health text contain what, why and remedy substrings; no path secret, no `panic`, no control characters; unknown codes in a hostile run record are ignored by `health`; the streak escalation (3 and 7) and `presync_stuck` are WARN, never RED; the pairing line with `missing-body` appears | R32, S53, S54, S56, S58, X2 | drop the remedy from one text → the substring check fails |
| MA-559 | U | hostile inputs: a `prefs.js` with a `directory` pref at `/`, a 1 GiB file and control characters (the run reads none; the install advisory caps at 8 MiB and uses no path); a `lock` naming pid 1, our pid and a non-Thunderbird pid (never quit); folder names with newline, `..`, RTL, 4,000 characters and flag-like names (watched, never executed, logged stripped, no escape); symlinked mail directories (not entered); a world-writable app path, a relative one, a symlink to elsewhere and a wrong basename (refused); an archive-resident descriptor naming a different app or profile (ignored by the run) | R31, R30, S57 | read the app path from the descriptor → the planted-descriptor case runs it |
| MA-560 | U | environment: with `DYLD_INSERT_LIBRARIES`, `LD_PRELOAD`, `MOZ_*`, a hostile `PATH` and `TMPDIR` set, every recorded `Cmd.Env` equals the allowlist exactly; the lookup seam is never called | R31, S57 | pass `os.Environ()` → the equality fails |
| MA-561 | U | no writes to Thunderbird: a whole-tree hash (names, sizes, modes, bytes) of a fixture profile and application before and after every scenario of MA-549..MA-557 is identical; the tool's own files exist only in the state directory, with modes 0600 and 0700 under umask 022 and 0 (Unix) | R31, S57 | touch a file under the profile → the hash differs |
| MA-562 | U | persistence: the state record is atomic (a crash hook between temp write and rename leaves the old or the new file), bounded to 4 KiB, tolerant of unknown fields, newer version = unreadable, the streak survives restarts and resets on ok; at most one record and one lock file per profile; no growth over 1,000 simulated sessions | R32, S55 | write in place, not by rename → the crash hook leaves a torn file |
| MA-563 | U | GUI and job: the second question appears only on macOS with a Thunderbird store among the inputs and defaults to No; Yes writes `thunderbird_sync` with derived profile and app; the golden wording equals §4.2.6; `runHeadless` builds the hook from the job file only; a WARN outcome calls the notifier once, a note or ok never; *Turn off Thunderbird sync* removes the field and the descriptor entry | R30, X16, S53 | default the dialog's selection to Yes → the default assertion fails |
| MA-564 | S | owner-facing text: one source constant holds the §6 paragraph; the `-h` text, the schedule preview notice, the GUI question and the README section contain it (or its fixed sentences); the README documents the flags, the one-profile limit, the §4.4.4 limits and the rollback note | X16, R30 | remove the notification sentence from the GUI text → the coherence check fails |
| MA-565 | S | the seam: `BeforeRead` is read in exactly one place in `internal/app/run.go`, after `beginRun`; `RunGraph`, `reindex`, `verify`, `extract` never set or call it; with it nil, `Run`'s observable behaviour is unchanged (golden of the existing run-record fields) | R30, S53 | call it at the top of `Run` → the position assertion fails |
| MA-566 | U | schedule integration: the cron line, the plist, the Task Scheduler XML and the wrapper carry the flags exactly (quoting and `%` cases included); `Preview` prints the notice; the descriptor gains `presync` with `Version` 1 and the install's collision rules are unchanged; `-remove` leaves the state directory | R30, S53 | drop the flags from the plist → the argv assertion fails |
| MA-567 | U | macOS consent: the fake `osascript` returns `-1743` → `quit-not-permitted` with the System Settings remedy, Thunderbird left running, the record becomes `left-running`; `-600` is treated as gone; a 20 s timeout kills only the helper | R31, R32, S56 | map `-1743` to success → the outcome assertion fails |
| MA-568 | U | no session: the fake `open` failing (window server unreachable) or a missing display gives `no-desktop-session` with no retry loop and no launch record left; a locked screen with a working `open` proceeds | R32, S56 | retry the launch in a loop → the call-count assertion fails |
| MA-569 | U | elevation: euid 0 or an elevated token (faked) refuses at S1 with `presync_refused_elevated`, zero helpers, the archive runs | R31, R32, S57 | skip the check → a launch is recorded |
| MA-570 | L | macOS end to end, the §9.4 L-M1..L-M8 script: hidden start, detection, identity, quiet, profile, sleep, quit under launchd with the screen locked — **pending** (a real Mac and a real Thunderbird profile) | R31, R32, S53, S56 | — |
| MA-571 | L | Windows end to end (L-W1..L-W5) — **pending** (a Windows machine) | R31, R32, S53, S56 | — |
| MA-572 | L | Linux (L-L1..L-L3, a display, the quit options) — **pending** (a Linux desktop); may close as "not supported" | R31, S56 | — |
| MA-573 | L | crash drills on a real Thunderbird (L-C2, L-C3, L-C4): kill the tool at each state, power-cycle, suspend, modals — **pending** | R31, R32, S55 | — |
| MA-574 | L | privacy observation (L-C5) and the truncated-last-message heal (L-C1): measured, and §6's text and §4.9.1's claim updated from it — **pending** | R32, X16 | — |

## 11. Build slices (small, in dependency order)

Each slice adds its rows to the catalog first (operator-accepted), ships prove-fail then
prove-pass, leaves the covers map green, and says what is shippable. "Nothing" means the slice
adds dead, tested code that no user-visible path reaches.

| Slice | Contents | Depends on | Rows | Shippable? | Gates owed after build |
|---|---|---|---|---|---|
| **TP0** | catalog-first PR: R30..R32, S53..S58, X16, the MA rows as pending/real per slice; README and ux-contract text for X16 | the pre-code review (TG1) and Q-answers that block (§13) | all | nothing | none |
| **TP1** | `internal/presync` core, pure: `Env` seams and fakes, the quiet detector and the watch digest, the state machine S0..S9 against fakes, the state record and lock, the recovery table, outcomes and warning codes; no OS code, no wiring | TP0 | 546 (core), 549, 550, 551, 552, 553, 555, 561, 562 | nothing | none (internal) |
| **TP2** | the seam and the record: `app.Options.BeforeRead`, `LastRun.Presync`, `Warnings` plumbing, health codes, `status` text and `-json` keys, `presync_not_recorded`, the summary lines, descriptor field and job-file field (additive, unused) | TP1 | 546 (surfaces), 548, 556, 557, 558, 565 | nothing user-visible (no source sets the hook); the new `status` reading is inert | none |
| **TP3** | macOS adapter (`open`, `osascript`, `ps`, `pgrep`, `caffeinate`), the exec seam's real implementation, the env allowlist, the consent outcome, elevation refusal, and the CLI flags on the **manual** export run | TP2; **lab L-M1..L-M8 answered** (TG3) | 554, 559, 560, 567, 568, 569, 570, 573, 574 | **first shippable:** `mailarchive -thunderbird-sync …` on macOS, manual | **adversarial pass** (new process control, untrusted profile reads, a quit request to another application) and **friction review** (the §8 walk, every F-row) |
| **TP4** | `schedule` integration: `parseJob` validation, the baked flags, the descriptor `presync`, the preview notice, README and the install-time `offline_download` advisory | TP3 | 547, 564, 566 | **shippable: scheduled sync on macOS (CLI)** | short adversarial note on the descriptor and the advisory's `prefs.js` read; friction review of the install walk |
| **TP5** | `mailarchive-gui`: the schedule-step question, the job-file field, headless wiring, health-view line, *Turn off*, notification | TP4 | 563 | **shippable: GUI schedule on macOS** | friction review (wizard flows are lab-tier like design-schedule-v2's) |
| **TP6** | Windows adapter: Toolhelp, `GetProcessTimes`, `PostMessageW`, `SetThreadExecutionState`, hidden launch | TP5; **lab L-W1..L-W5** | 571 (and MA-554's Windows argv catalog entries) | shippable: Windows | adversarial addendum (user32 calls, a `WM_CLOSE` to another process's windows) + friction |
| **TP7** | Linux adapter, only if L-L1..L-L3 find a documented quit and a usable display story | TP6; lab | 572 | shippable only if the lab allows | adversarial addendum |
| **TP8** | optional rungs decided by Q3b and Q4 (SIGTERM rung, leftover adoption policy flip), the optional owner-activity gate | the lab and the owner's rulings | extends 553, 555 | shippable | adversarial note per rung |

TP1..TP2 are the smallest independently reviewable unit; TP3 is the first slice an owner can use.
TP0 is not an implementation slice.

## 12. Honesty of claims

- P1 (inert when off), P3's "archive runs after every outcome" and P4 (inside the lock and the
  record): **proven** by MA-546, 557, 556, 565 once TP1..TP2 are built.
- P2 (never kills, only ours): the *code-level* claim is **proven** (MA-551, 553, 554, 561). The
  *behavioural* claim that Thunderbird exits cleanly on the quit request, on each OS, is
  **lab-pending** (MA-570..MA-572): nothing in this document is lab-validated.
- P5 (a crash never leaves a hidden Thunderbird forever): **conditional** on the platform giving a
  process start time (§4.4.3) and on Q4's policy; with the safe interim it is *not* assured, it is
  announced nightly.
- The sync's usefulness (P8): **conditional** on Thunderbird's own folder-check and offline
  settings (§4.4.4) and, as to whether file silence marks the end of a fetch, **aspirational** until
  L-M5. The "quiet" claim is a heuristic until then.
- "Hidden": **aspirational** (L-M2, L-W3, L-L1). The text promises "out of sight where the system
  allows" and says a window or Dock icon may appear.
- macOS quit under launchd: **aspirational** until L-M8 shows a grant covers the scheduled
  program. Until then the feature may start Thunderbird and fail to close it on every night
  (F10); that failure is loud, not silent.
- Privacy (§6): **conditional** on the profile's configuration, to be measured (L-C5).
- Status: not built, not lab-validated, pre-code review not filed.

## 13. Owner questions

Each has a recommended default and a safe interim (what happens if the owner does not answer; it
must never weaken R31 or R32). A question marked *blocks* holds only the slice named.

| # | Question | Recommended default | Safe interim | Blocks |
|---|---|---|---|---|
| **Q1** | Default quiet period and maximum wait? | quiet **3 min**, max **20 min** (your suggestion), settle 60 s fixed | the same | nothing |
| **Q2** | If the tool started Thunderbird, but you opened it (it is visible and you are using it) before the scheduled quit, should the tool still ask it to quit? | **Yes, but not when you appear to be at the computer.** Thunderbird's own quit asks about unsaved drafts, so nothing is lost without your click, and a macOS or Windows idle check (no keyboard or mouse for 2 minutes) skips the quit with the warning "left open because you appear to be using the computer" | quit as scheduled without the idle check, and the log line "Thunderbird was closed by the scheduled backup at 02:09" (the idle gate waits for TP8 and its lab item) | TP8 |
| **Q3** | A locked screen: attempt the sync? | **Attempt** (a locked Mac or PC is the normal 02:00 state and the user's session still exists); if Thunderbird cannot start, warn (F4/F16) | attempt | TP3 (L-M2) |
| **Q3b** | May a SIGTERM rung ever be used to close Thunderbird if the platform's polite request is unavailable? | **No, not in v1.** Only if the lab (L-M8b, L-L3) proves a clean shutdown on a large profile | no | TP8 |
| **Q4** | If a run was killed after starting Thunderbird, and the next run finds that same Thunderbird (provably started by the tool, inside its 32-minute window) still open: ask it to quit? | **Yes, once, at the start of the next run**, with `presync_leftover_closed` | **No: leave it and warn nightly** (`presync_leftover_found`), so a hidden Thunderbird is announced, never silently closed | TP3 (default needs L-M4 identity) |
| **Q5** | Ship Windows and Linux in the first slice, or macOS first? | **macOS first** (TP3..TP5), Windows after its lab (TP6), Linux only if the lab finds a usable quit (TP7) | macOS only; other platforms refuse at schedule time with the sentence in §4.2.4 | TP6, TP7 |
| **Q6** | A repeated warning: when does it become loud? | WARN `presync_stuck` after **3** consecutive warning outcomes; never RED | the same | nothing |
| **Q7** | Notify on a sync warning from a GUI scheduled run? | **Yes**, one best-effort notification, warnings only | yes | TP5 |
| **Q8** | If Thunderbird will not quit (or is still writing) when the archive is due, read anyway or skip the night? | **Read anyway** (a read of a live Thunderbird is today's behaviour; a skipped night loses mail) | read anyway | nothing; reconsidered if L-C1 shows a bad truncated-message result |
| **Q9** | Allow `-thunderbird-sync` on a manual run, not only `schedule`? | **Yes**: it is the test, and where macOS asks permission | yes | TP3 |
| **Q10** | A session in which no file changed at all: note or warning? | a **note** ("nothing new, or Thunderbird could not reach the server"), a **warning after 7 nights in a row** | note only | nothing |
| **Q11** | Refuse an `hourly` schedule with sync? | **Yes** | yes | TP4 |
| **Q12** | Pin the macOS Thunderbird signature (Mozilla's code-signing Team ID) as well as the bundle id? | **Not in v1**; record as residual | bundle id only | nothing |
| **Q13** | One profile per schedule, or sync several? | **One profile per schedule**; a second profile gets its own schedule | one | nothing |
| **Q14** | Accept that while the sync runs (up to about 24 minutes) the archive is "in use": a manual run or `verify` refuses? | **Yes** (D1a) | yes | TP2 |
| **Q15** | Accept that "sync" means what Thunderbird itself fetches in the background (Inbox by default), not every folder? | **Yes**, with the README paragraph and the account-settings advice | yes | nothing |

## 14. Pre-code review readiness

The 10 lenses of `assurance-kit/process/design-review.md`, as this design asks them to be applied.

1. **Purpose / threat-model fit.** Does every mechanism trace to a property (P1..P9) or to the
   owner's request? Does "P3: read unsafe in no case" over-claim the safety of reading a live
   Thunderbird? Is the macOS bundle-id check claimed as authenticity (it must not be)?
2. **Focus / proportionality.** Is a process-control feature plus a `app.Run` seam plus three
   OS adapters one design or three? Is TP1..TP2 the smallest reviewable unit? Is there a simpler
   mechanism with the same property (for example, the owner leaving Thunderbird open, or a
   documented `launchd` job of their own)?
3. **Durability / long-term operation.** The state record's bounds, the streak counters, a record
   from a year-old crash, a Thunderbird auto-update changing the app path or bundle contents, a
   renamed profile, `deadline` arithmetic across daylight-saving, macOS purging nothing we keep
   (config dir, not caches), roaming profiles on Windows, the crash order at every write.
4. **Usability / operator friction.** Walk the owner (CLI and GUI), the owner at 02:00 with the
   screen lit, an inheritor who finds the schedule cold, an admin with several profiles; classify
   friction Type I or II: the macOS permission prompt, the visible Dock icon, a nightly warning
   for a Primary Password prompt, the 24-minute archive lock, the refused hourly schedule.
5. **Integration / seams.** Is `BeforeRead` the right seam and does it collide with
   fault-isolation's `recordRun`/`Result` changes and the extraction workspace's run record
   (neither is built)? Does the flag family follow X4/X5? Is the build order producer-before-sink
   (TP2 fields before TP3 writers)? What does deployment touch (a new state directory, a new
   launchd argument)?
6. **Security / adversarial.** The §5.3 personas aimed at the design as written: the lock-pid
   trick, the descriptor in a shared archive, `-thunderbird-app` as an execution primitive, the
   Windows `PostMessage` target, the `osascript` constant, environment allowlists, the root
   refusal. What can an attacker read or cause regardless of what the client filters?
7. **Failure domains / recoverability.** For each new component (hook, state record, profile lock,
   helpers, the hidden Thunderbird): what happens when it is lost or compromised; does each failure
   fail closed in the sense of "never quit what we cannot prove"; what does the owner see (the
   §4.10 table, row by row).
8. **Migration / coexistence.** §7: the older-binary matrix, the silent older GUI, the loud older
   CLI, mixed versions on one profile, the rollback of the feature, the interaction with
   `-enable-offline` and with a verify schedule at the same hour.
9. **Dependency and operational cost.** No new module; new reliance on `open`, `osascript`,
   `ps`, `pgrep`, `caffeinate` (macOS), `user32`/`kernel32` (Windows), and on Thunderbird's
   behaviour across its updates. What happens at 3 am when Thunderbird's quit dialog changes, the
   Automation grant is revoked, or an OS update changes `ps` output? Who patches?
10. **Honesty of claims.** Every property carries a label and its condition beside it (§12); the
    "hidden", "quit cleanly", "quiet means done" and "unaffected read" claims must not read as
    proven; status is "proposed, not built, not lab-validated".

### The five questions I am least sure of

1. **Can the macOS scheduled run ever be allowed to quit Thunderbird?** An Apple event to another
   application needs an Automation grant per requesting program. Whether a grant given from
   Terminal covers the launchd-run binary, whether an unanswered prompt blocks until our 20 s
   deadline, and whether any quit path exists that needs no grant and is still the application's own
   request (SIGTERM is not shown to be graceful), decide whether the headline feature works on
   the owner's Mac at all (L-M8, L-M8b).
2. **Does a hidden Thunderbird actually sync, hidden, unattended?** `open -g -j` may still show a
   window or Dock icon; a Primary Password, an OAuth re-sign-in, a crash-recovery or newer-profile
   dialog would stall it silently; and the profile `lock` symlink I rely on for the pid may not
   exist on macOS (L-M2, L-M3, L-C4).
3. **Is "no file change for 3 minutes" a real signal that the fetch ended?** I do not know that
   Thunderbird appends to the offline mbox as bytes arrive, that `.msf` files move per folder, or
   how it behaves with no network (indistinguishable from "nothing new"); and by default it checks
   only the Inbox of an IMAP account. If file silence does not track the fetch, the sync is a
   timer with extra steps (L-M5).
4. **Is running the session inside the archive lock and the run record, through a new
   `app.Run` hook, the right shape?** It buys R5 and R18 for free but holds the archive for about 24
   minutes, makes `verify` and a manual run refuse meanwhile, and adds a field to `app.Options`
   that fault-isolation's and the extraction workspace's unbuilt changes also touch (D1a, Q14).
5. **Is process identity (executable path plus start time) sufficient and obtainable?** The macOS
   `ps -o lstart=` and `comm=` strings, Windows launcher re-execution (the child pid may not be
   the real process), the 2 s tolerance, and the 10-minute margin of the leftover window are my reconstruction;
   a mistake here either quits something that is not ours (the worst failure) or never cleans up
   a hidden Thunderbird (L-M4, L-W2, Q4).
