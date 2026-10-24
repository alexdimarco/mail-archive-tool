# Design — Thunderbird pre-sync: start Thunderbird, let it fetch, ask it to quit, then archive (opt-in, per schedule)

**Revision:** 3 (2026-10-21), amending revision 2 (below) from the macOS pre-sync lab run of
2026-10-21. Changes: **(a) the quiet clock (§4.4.2)** — lab item L-M5 is now answered on a real
Mac, and the answer forces a correction. Thunderbird's startup login refreshes the message list but
defers the offline body/attachment download to its next periodic IMAP check (`check_time`, default
~10 min), so the watched mail files can stay silent for about ten minutes after launch **even when
mail is already waiting on the server** (measured: first INBOX growth at +628 s with the mail
pre-delivered). Revision 2's `settle`(60 s)+`quiet`(3 min) floor would therefore declare quiet at
~4 min, quit before the download, and record `ok-noactivity` every run — a silent miss, the worst
outcome for a backup. The fix: **quiet is not declared until at least one real change has been
observed since `up`**, and `max` is sized above the worst-case first poll plus `quiet`. **(b) id
reassignment** — `MA-549` was taken by the Microsoft-365-setup work after revision 2, so the
pre-sync's `MA-549` row moves to **MA-580**; `MA-550..MA-579`, `R30..R32`, `S53..S58` and `X16` are
re-checked free and unchanged (PC19). **(c)** change (a) is a **material change to a reviewed
section and owes its affected-lens re-review** (correctness + the false-quiet failure lens) before
TP3 ships — it is written up here, not self-certified. The owner chose this wait-for-quiet path
("Option A") over shortening Thunderbird's check interval, accepting the ~10-min wait for an
unattended run.
**Revision:** 2 (2026-10-13), with the conditions PC1..PC22 of the pre-code review folded.
**BUILD STATUS:** reviewed, conditions folded (macOS v1 scope only: slices TP0..TP5). Not built,
not lab-validated. The 10-lens pre-code review is filed as
`docs/review-thunderbird-presync-predesign.md` (verdict APPROVED WITH CONDITIONS, reviewed
against revision 1). Four questions stay OPEN for the owner (§13: Q2, Q4, Q16, Q17) and one
cross-design item for the operator (§15, OPEN-X1); none of the OPEN items may be answered by
weakening R31 or R32. The Windows and Linux sections (§4.7.2, §4.7.3, TP6, TP7) were **not**
reviewed and must go through their own design review before those slices start (PC21). Nothing here is
lab-validated: every Thunderbird, macOS, Windows and Linux behaviour that this text states from
memory of vendor documentation, and not from this repository, carries the words "lab item" and
an id (L-M*, L-W*, L-L*, L-C*), and is verified on a real machine before the slice that depends
on it ships (§9.4). **TP3 may not start until lab items L-M8 and L-M9 (the quit path under
launchd) are answered (§4.7.1, PC4, PC5).**

Base: branch `wip/tb-presync-design`, from `fix/review-2026-10` at `85c4c41`. This document
changes no code, no test and no catalog row. Every id it proposes (R30..R32, S53..S58, X16,
MA-549..MA-579) is provisional until the OPERATOR accepts it into `docs/scenario-catalog.md`
and `docs/ux-contract.md` with its slice's catalog-first PR (`docs/owner-rulings-2026-10-09.md`
§8). **Id allocation (PC19):** revision 1 proposed MA-546..MA-574; the rulings name MA-546 as the
next free overflow id, but the desktop local-origin work has since taken MA-546..MA-548, so every MA id
here is shifted by three (MA-549..MA-577) and the two rows added by the review are MA-578 and
MA-579. R29 / S52 / X15 are the last allocated invariant, scenario and X ids in the catalog
and rulings at the head of this branch; **re-check R30..R32, S53..S58 and X16 against the
operator's log at TP0** (other unbuilt designs may have claimed them, PC19).

**Owner request (2026-10-13), verbatim:** "is there a way to have it start up Thunderbird,
sync, and shut it down?" then "I think it is worth building". The owner wants an opt-in,
per-schedule setting, **Sync Thunderbird before archiving**, default OFF.

Gates before code:
- **TG1** the pre-code design review (`docs/review-thunderbird-presync-predesign.md`), lenses
  per §14, with the rescue pass. **Met for macOS v1 (TP0..TP5): filed, APPROVED WITH
  CONDITIONS, conditions folded here (§15).** A material change to this text re-runs the
  affected lenses; the Windows and Linux sections are outside what was reviewed (PC21).
- **TG2** owner answers to the questions in §13 that block a slice (each has a recommended
  default and a safe interim, so no slice is held by an unanswered question). **Open: Q2
  (now blocks TP3), Q4, Q16, Q17.**
- **TG3** lab items L-M1..L-M10 (macOS) before slice TP3 ships; **L-M8 and L-M9 are viability
  gates, not verification: if neither finds a quit path that works from a launchd-run program,
  TP3's scheduled form does not ship (Q16).** L-W* before TP6. L-L* before TP7.
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
  The default is OFF in every surface. Proven (MA-549, MA-568). Proposes **R30**.
- **P2 — Only what we started, and only by asking.** The tool asks Thunderbird to quit only
  when this session started that process and has proven its identity (pid, process start time,
  executable path, and the `-profile` argument it was given) at launch and again immediately
  before each request. **Conditional (PC1):** on macOS the request is delivered to the
  *application* (by bundle id), not to a pid, so the identity check gates the request but
  cannot bind its delivery: if, between the check and the delivery, our process has gone and
  another Thunderbird of the same bundle has appeared, the request reaches it. The window is
  milliseconds, the request is only a request (Thunderbird still asks about unsent drafts), and
  the owner-activity gate (PC7) and the post-request check (§4.5 S8) bound it; it is a stated
  residual, not a proven absence. It never signals, terminates or kills
  Thunderbird, never edits, deletes or moves any Thunderbird file, sends no keystrokes and
  drives no window beyond the application's own quit request. If Thunderbird was already
  running when the session began, it is neither started nor quit. Proven (MA-554, MA-556,
  MA-557, MA-564). Proposes **R31**.
- **P3 — Never gates the archive, always loud, always bounded.** Every failure of the sync
  session ends in a warning that names what happened, why and what to do (X2), recorded in the
  run record and shown in the run summary, `status` and the GUI health view; and the archive
  run then happens anyway, because it only reads. The one stop is cancellation. Every wait has a
  bound (§4.11): the worst case of a session is about 25 minutes at the fixed defaults.
  **Conditional (PC16):** "the read is safe" means the *archive* writes nothing to the profile;
  whether a read of a Thunderbird that is still writing yields a truncated last message that
  heals on the next run, rather than a stale duplicate, is lab item L-C1 and is not assumed.
  Proven (MA-560, MA-561, MA-555, MA-556). Proposes **R32**. §4.9.1 states exactly which
  failures make the read unsafe: none does by itself, and why.
- **P4 — The sync session lives inside the run.** It runs after the archive lock is held and the
  run's `running` last-run record is written, and before any source is read. So R5 (one run per
  archive) and R18 (every run that begins is recorded) hold for the whole session, and a
  session killed mid-way is visible to `status` as a run that never finished. Proven (MA-559,
  MA-568).
- **P5 — A crash never leaves a hidden Thunderbird forever, and never costs the owner theirs.**
  A write-ahead record in the user's state directory names what was launched. The next run
  recovers from it. In v1 the recovery **announces and does not quit** (Q4, PC22): a
  Thunderbird it can match exactly to the record (pid, start time, path, profile argument) is
  named in a warning every run for as long as that same process stays open, however old the
  record is (PC2); one it cannot match is left alone and treated as the owner's. A reboot
  clears every record (PC10). Conditional on the platform returning a process start time
  (§4.4.3); without one nothing is ever quit.
  Proven (MA-558), lab (MA-576).
- **P6 — Settings come from the schedule, never from the archive.** The profile, application
  path, quiet and maximum wait are read from the program's own arguments or the user-owned job
  file. They are never read from `<out>/.mailarchive-schedule.json` or anything else inside the
  archive folder, which anyone who can write the archive can edit. Proven (MA-562).
- **P7 — Older binaries never skip the sync silently, but they are not all harmless (PC13).**
  The descriptor and job-file fields are additive and ignored by older readers. An older
  *CLI* handed the new flags refuses (existing behaviour for any unknown flag): the unattended
  run is recorded failed and **no backup is made** until the schedule is removed or the binary
  is updated; that is loud, and it is the cost of rolling back. An older *GUI* binary ignores
  the job-file field and backs up without the sync; the new `status` flags that
  (`presync_not_recorded`, §4.9.4), the older one cannot. §7. Proven (MA-551).
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
- **v1 scope cuts from the review (PC22).** Out of v1, to be added only on a stated need:
  (a) the flags `-thunderbird-sync-quiet`, `-thunderbird-sync-max` and `-thunderbird-app`, and
  the job-file and descriptor fields that carried them (quiet 3 min, max 20 min and the
  standard install paths are fixed; a Thunderbird in a non-standard place is refused with a
  message, §4.2.4); (b) quitting a leftover found by the next run (it is announced, Q4);
  (c) the 5-minute wait on a shared profile lock (a held profile ends the session at once with
  the note `presync_profile_busy`); (d) the second quit request (one request, then the wait);
  (e) the escalation of notes to a warning after 7 runs (a single warning streak of 3 stays);
  (f) Windows and Linux (outside the reviewed scope, PC21). Where the body of this document
  still describes a cut item it says "deferred, not v1".

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
the deferred `recordRun` and the `ostAdvisory` block; positions are by name, not line number, because the fault-isolation and
extraction-workspace designs also edit `Run`, PC20). The returned warnings
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
| D1c | A hook, not a new `app.Run` parameter list | Smallest seam; nil means identical behaviour (MA-568) | A `presync` package imported by `app` — couples the run to process control |
| D1d | The hook cannot fail the run | P3: presync is never a gate | Returning an error from the hook would make a Thunderbird dialog stop the backup |

**Consequence the owner should know (conditional):** while a session runs (up to about 25 minutes)
the archive is "in use": `mailarchive status` shows an export in progress, a manual run or
`verify` refuses with the existing "archive is in use by another mailarchive run (held by
mailarchive: pid=… verb=export)" message. This is by design; it is the cost of D1a.

**Who is blocked, and what each is told (PC8).** The lock holder line only names a verb, so a
refused person would read "export in progress" and not know that the wait is Thunderbird. So:
(1) at S1 the hook rewrites the run's `running` last-run record with
`presync: {state: "syncing", since, expected_until}` (the same atomic write the run already
makes; it is the only extra write before S5), and clears the state to the final outcome at END;
(2) `app.Run`'s "archive is in use" refusal, `status` (`run_in_progress`), `verify`'s refusal and
the GUI health view read that field and add: "it is syncing Thunderbird first (started 02:00,
up to 25 minutes); try again after about 02:25"; a record without the field reads as today;
(3) the extraction workspace's archive-source export takes no archive lock
(design-extraction-workspace MA-431), so it is not blocked and reads the archive while the sync
runs (it sees the previous run's data, as it would between runs); (4) the desktop dashboard has
no Thunderbird source and its Microsoft 365 backup uses a different archive; (5) **a scheduled
`verify` whose slot falls inside the 25 minutes after the backup's refuses and records nothing**
(README, line 1091), so the README's "run it outside the backup window" sentence gains "and, with
Thunderbird sync on, leave at least 30 minutes after the backup's time".

### 4.2 The setting: storage, flags, controls, compatibility

#### 4.2.1 The values

| Setting | Meaning | Default | Range | Why |
|---|---|---|---|---|
| sync | run the session | **off** | on / off | owner request |
| quiet | no change in the watched mail folders for this long means done | **3 minutes, fixed in v1** (configurable range 30 s .. 30 min is deferred, not v1, PC22) | fixed | owner suggestion; Q1 |
| max wait | the session gives up waiting for quiet this long after Thunderbird was launched | **20 minutes, fixed in v1** (range 2 min .. 120 min, at least settle + quiet + 60 s, deferred, not v1) | fixed | owner suggestion; Q1 |
| profile | the Thunderbird profile directory | derived (§4.3), must be one Thunderbird itself lists unless named explicitly (PC3) | an absolute path | §4.3 |
| app | the Thunderbird application | the platform's fixed candidates (§4.7); **no override in v1** (`-thunderbird-app` deferred, not v1, PC22) | — | non-standard installs are refused with a message |

Fixed, not configurable in v1: poll interval 10 s, settle 60 s (the earliest the quiet clock may
start counting after Thunderbird is up), start timeout 90 s, quit grace 120 s (§4.11).

#### 4.2.2 CLI flags (export verb and `schedule`'s flat form)

Kebab-case, `-thunderbird-` prefix as `-outlook` / `-outlook-sync-wait` set the precedent
(X4/X5). `-sync-wait` is taken by the one-time prep, so the new family does not reuse it.

| Flag | Meaning |
|---|---|
| `-thunderbird-sync` | Before the archive run, if Thunderbird is not already open, start it out of sight, wait until its mail files stop changing, ask it to quit, then archive. Off by default. |
| `-thunderbird-sync-quiet DURATION` | **Deferred, not v1 (PC22).** With `-thunderbird-sync`: how long the mail files must stay unchanged (default `3m`, 30s to 30m). |
| `-thunderbird-sync-max DURATION` | **Deferred, not v1 (PC22).** With `-thunderbird-sync`: the longest to wait for that after Thunderbird starts (default `20m`, 2m to 120m). |
| `-thunderbird-profile PATH` | With `-thunderbird-sync`: the Thunderbird profile to sync (default: the profile that holds the Thunderbird `-input` stores and that Thunderbird lists in its `profiles.ini`, §4.3). Required when the inputs span more than one profile, or when the derived profile is not one Thunderbird lists (naming it is the owner's consent to start Thunderbird on it). |
| `-thunderbird-app PATH` | **Deferred, not v1 (PC22).** With `-thunderbird-sync`: the Thunderbird application (macOS: the `.app`; Windows: `thunderbird.exe`). Only for a non-standard install. |

`-h` text for `-thunderbird-sync` (exact, X3):

> Before the run, if Thunderbird is not already open, start it out of sight, wait until its mail
> files stop changing, ask it to quit, then archive. Thunderbird will check all of its accounts
> (mail, calendars, feeds, update checks), run your filters, send anything waiting in its Outbox,
> do its usual on-quit clean-up, and may show notifications or play a sound. If Thunderbird is
> already open, or you are using the computer, it is left alone. Off by default.

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
Presync *PresyncDesc `json:"presync,omitempty"` // v1: {Kind:"thunderbird", Profile string}; Quiet/MaxWait are deferred, not v1
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
| ~~`interval hourly`~~ **(REVERSED 2026-10-23)** | **No longer refused** (owner ruling): the pre-sync is allowed on ANY cadence including hourly — the runtime `detectRunning` defer never starts or quits a Thunderbird the owner is using (`OutcomeAlreadyRunning`), so a frequent sync is safe (shipped in code; MA-550). Original text was: "an hourly schedule is too frequent to start and quit a mail program each time…; use daily or weekly." |
| no Thunderbird store among the inputs (including after `-auto` discovery now) | "none of the inputs is a Thunderbird mail folder, so there is nothing to sync." |
| inputs span more than one profile and `-thunderbird-profile` not given | lists the profiles; "one scheduled sync syncs one profile; name it with `-thunderbird-profile`, or schedule each profile separately." |
| Snap or Flatpak profile path | "Thunderbird installed as a Snap or Flatpak cannot be started and closed from here yet." |
| the profile is not a directory you own, or is group- or world-writable (Unix) | names the path and the mode. |
| application not found or not acceptable (§5.2) | names the candidate paths tried and says a Thunderbird in a non-standard place cannot be synced in this version (`-thunderbird-app` is deferred, not v1). |
| the derived profile is not listed in Thunderbird's own `profiles.ini` and `-thunderbird-profile` was not given (PC3) | "`<path>` is not a profile Thunderbird knows about (a backup or copy?). Starting Thunderbird on it would connect to its accounts and send anything in its Outbox. If you do want that, name it with `-thunderbird-profile`." |
| `-thunderbird-sync-quiet` / `-max` given (deferred, not v1) | "this version uses fixed 3 minute and 20 minute waits." (Range checks return with the flags.) |
| the tool runs as root or elevated | "do not run this as root or an administrator: Thunderbird would create profile files you cannot then open." |

Install-time advisory (read-only, bounded, interactive context only): for an IMAP store whose
account in `prefs.js` has `offline_download` off, the install prints "account `<host>` is not set
to keep messages on this computer: Thunderbird will not download message bodies, so syncing will
not make them available. Run `-enable-offline -sync-wait` once first (README, IMAP)." This reads
`prefs.js` once through the existing `parsePrefs` (to be capped at 8 MiB, §5.2), echoes control-
stripped values only, and uses no path from it. The run-time session reads no `prefs.js` at all
(§5.3). **Second advisory (PC9):** for an account whose `prefs.js` shows an exit action on
(`empty_trash_on_exit`, or expunge/compact on exit), the install prints "account `<host>` is set
to empty its Trash (or clean up) when Thunderbird quits: the scheduled sync quits Thunderbird before
every archive, so mail you deleted since the last run may be removed before it is archived."
These keys are not in `parsePrefs`' current set; the slice adds them, reading values only.

#### 4.2.5 Job file (GUI headless) field

`internal/job.Job` gains, additively (`omitempty`, `Version` stays 1):

```go
ThunderbirdSync *ThunderbirdSync `json:"thunderbird_sync,omitempty"` // v1: {Profile string}; Quiet, Max, App are deferred, not v1
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
> feeds, check for updates, run your filters, send anything waiting in its Outbox, show
> notifications or play a sound, and it does its usual clean-up when it closes. If Thunderbird is
> already open, or you are using the computer, when the backup starts, it is left alone.
>
> On a Mac, macOS will ask your permission, once, to let Mail Archive control Thunderbird. The
> next screen shows how to approve it now, while you are here.
>
> Items: **No — archive what is already on disk** (default) · **Yes — sync Thunderbird first**

Yes writes `thunderbird_sync` into the job file (fixed 3 min / 20 min waits, profile derived and
checked against `profiles.ini` at that moment and shown in the confirmation: "Thunderbird will be
synced from `<profile>`"; a derived profile Thunderbird does not list is refused with the §4.2.4
text, the wizard offering no way to override it in v1).
The final confirmation (`zenity.Info` in `offerSchedule`) gains "Thunderbird: synced before each
run (up to 25 minutes)" and the first-approval instruction of §4.7.1 (PC4): "To approve the macOS
permission now, while you are here, choose *Run the scheduled backup now* in the next dialog",
which starts the installed job through `launchctl kickstart gui/<uid>/<label>` (so the prompt is
attributed to the program the scheduler runs, not to the wizard). Whether the wizard may start
the job itself is checked by L-M8. The health view (§4.9.4) shows the setting and its last result beside
*Remove the scheduled backup*, and a *Turn off Thunderbird sync* action rewrites the job file
without the field and the descriptor without `presync`.

The desktop dashboard has no Thunderbird surface (non-goal). **REVERSED 2026-10-24 (owner ruling):** the Desktop now creates/shows/edits a local Thunderbird schedule with the pre-sync toggle — see `docs/design-thunderbird-presync-desktop.md` (reviewed, GO-WITH-CHANGES).

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

**Membership rule (PC3).** Starting Thunderbird on a profile connects to every account in it,
sends its Outbox, runs its add-ons and filters, and writes to it. A profile *derived* from an
`-input` store is therefore accepted only if Thunderbird itself lists it: its canonical path is the
canonical form of a `Path=` entry (resolved against Thunderbird's own application-support directory
for `IsRelative=1`) in `profiles.ini` or an `Install*` section's `Default=`. `profiles.ini` is read
once, at install time and again at S1, capped at 256 KiB, as untrusted text (values control-stripped,
no path from it used except to compare with the derived profile). An `-input` that points into a
backup, an old copy, a mounted disk image or an inherited profile is thereby **not** started: the
owner sees the §4.2.4 refusal (or at run time `presync_profile_unusable`). `-thunderbird-profile`
bypasses the membership test, and only it: the owner who types the path has consented, the preview
prints it, and whether a profile absent from `profiles.ini` starts normally is lab item L-M6.

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

The quiet clock (**amended, revision 3 — L-M5**): `lastChange` starts at `up + settle` (`up` is the
moment Thunderbird was proven running). Quiet requires **two** conditions, not one: at least one
real change must have been observed in the watched mail folders since `up`, **and**
`now - lastChange >= quiet`. The first condition is the revision-3 correction: the macOS lab
(2026-10-21) measured that with mail already waiting on the server before launch, the offline
download did not reach the mail files until +628 s (~10.5 min), because Thunderbird's startup login
refreshes the list but defers the body/attachment write to its next periodic IMAP check
(`check_time`, default ~10 min). A one-condition clock (`settle + quiet` with no observed change)
would declare quiet at ~4 min, during that opening silence, and quit before anything arrived. So a
session in which nothing has yet changed keeps waiting to `max`; it never concludes `ok` or
`ok-noactivity` at `settle + quiet`, and `ok-noactivity` is reached only at `max` with no change
seen. Equivalently `settle` is a floor that must not by itself end the session; requiring a first
observed change is the form used, because it also serves the fast case (a prompt download ends the
session as soon as it settles). `max` must clear the worst-case first poll plus `quiet`; revision
2's fixed 20 min is tight against a ~10 min poll + 3 min quiet, so revision 3 raises the `max`
ceiling (the exact value folded into §4.2.1/§4.11 when TP3 is built and the lab fixes the real
worst case on a large, multi-message, attachment-heavy mailbox).

Suspend: if the wall-clock gap between two polls exceeds 30 s (the machine slept, or a
laptop's lid closed), `lastChange` is reset to `now` and a counter is kept. The clock used is the
injected clock's wall time (`Now()`); the design needs the gap, not a monotonic reading, which on
macOS does not advance during sleep (lab item L-C3).

| Error | Cause | Consequence |
|---|---|---|
| False "quiet" | Thunderbird was waiting on a network, a server or a modal dialog (Primary Password, an OAuth sign-in, "Thunderbird crashed", "a newer version created this profile", "set as default") so nothing changed; or one very large message streams slowly with no write for longer than `quiet` | **Revision 3 (L-M5):** the opening silence no longer ends the session early — quiet needs an observed change first, and the ~10-min first-poll latency is exactly that silence, so a session with no change waits to `max` and only then records `ok-noactivity` (§4.9.3, a note; the escalation to a warning after 7 in a row is deferred, not v1): "Thunderbird started but nothing changed before the max wait: nothing new, or it could not reach the server". A mid-fetch stall longer than `quiet` *after* real activity still ends with what has arrived. |
| False "not quiet" | Thunderbird touches a watched file periodically for its own reasons (a summary-file rewrite on a timer), or a first sync of a very large mailbox that takes hours | the session stops at max wait with `presync_not_quiet` (a warning that says the archive used what had arrived and that the rest continues next run) |

Whether file silence tracks the end of an IMAP fetch (do offline stores grow as messages
arrive, are `.msf` files updated per folder) was **lab item L-M5**, now **answered on macOS
(2026-10-21)**: the offline store (the INBOX mbox) does grow as bodies arrive and the `.msf`
summary is rewritten, so file growth is a sound "it is fetching" signal — but the *start* of the
fetch lags launch by Thunderbird's periodic-check interval (~10 min at the default), so silence
early in a session does **not** mean "done". `quiet` is therefore a time-based heuristic, guarded by
the revision-3 "an observed change is required" rule above; §12 carries the residual (the Windows
and Linux lab items L-W*/L-L* remain, to confirm the same file-growth behaviour on those platforms).

#### 4.4.3 Is a running Thunderbird "ours"

After the launch the session looks for a Thunderbird process that did not exist before: pid from
the profile lock (if it exists) plus the process list (B above) are the candidates. A candidate is
**ours** only if all hold:

1. its executable path equals the resolved application executable the session launched;
2. **(PC6)** its command line carries `-profile <the canonical profile>` (macOS `/bin/ps -p <pid>
   -o args=`; the same two arguments we passed to `open`). A Thunderbird on another profile, or
   one the owner opened plainly, never matches;
3. its pid is **not** in the process snapshot taken at S4, immediately before the launch (the
   snapshot is the list of every Thunderbird-named process the user ran; it is the strong
   test), **and** its process start time is at or after `launchAt - 2 s`. The start time is
   derived from the elapsed time the platform reports (macOS `ps -o etime=` subtracted from the
   injected `Now()`, stored as UTC unix seconds), not from `ps -o lstart=`, which prints local
   time with no zone and is ambiguous in the repeated hour of a DST change (PC11); Windows
   `GetProcessTimes`, Linux `/proc/<pid>/stat` (deferred, not v1); lab items L-M4, L-W2, L-L3;
4. it is the only such candidate;
5. the launch record was written before the launch (§4.6).

**Successors and late starts (PC6).** A launched Thunderbird can be replaced by another process
that is not the one we proved: an update applied at start makes the application relaunch itself
(a new pid, possibly a visible updater window), and a first start that takes more than 90 s
appears only after S6 gave up. So (a) when the proven process disappears in S7, one more scan
runs for any Thunderbird-named process absent from the S4 snapshot; (b) at S9 the same scan runs
once more. A process found by either scan is **not** proven ours and is never quit; it is
recorded as `left-running` for announcement only (§4.6) and the session ends with the warning
`presync_successor_found`: "Thunderbird restarted itself (an update?) and is still open; quit it
from the Dock". Nothing the tool does depends on guessing that it is the same program.

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
| **S2 profile lock** | planned | try once (no wait in v1, PC22) `lockfile.AcquireAs(<state-dir>/presync-<profilehash>.lock, "tbsync")` | acquired → S3; held by another schedule → END `presync_profile_busy` (a note). A bounded 5-minute wait is deferred, not v1 | 1 s |
| **S3 recover** | lock held | read the state record for this profile (§4.6); apply the recovery table, which in v1 **announces and never quits** (Q4 interim, PC22) | clean → S4; a leftover Thunderbird matched exactly to the record → END `presync_leftover_found` (warning, every run while the same process is open, PC2); unreadable record → warn `presync_state_unreadable`, continue as if absent | 5 s |
| **S4 detect** | recovered | evaluate "running" (§4.4.1) and **take the process snapshot** (§4.4.3); **owner-activity gate (PC7):** if the owner's idle time is under 2 minutes (macOS `HIDIdleTime` through `/usr/sbin/ioreg`, lab L-M10; unreadable counts as active) → END `owner-active` (a note, nothing started) | running → END `already-running` (note) or `other-profile-running` (note); not running and owner idle → S5 | 5 s |
| **S5 launch** | not running | write the **write-ahead record** (state `launching`, launch time, application path, profile, our pid and start time), fsync, then start Thunderbird (§4.7) | start call failed → clear the record, END `cannot-start` (or `no-desktop-session`); started → S6 | the start call 20 s |
| **S6 await-up** | launched | poll every 2 s for a candidate (§4.4.3) | candidate proven ours → write record `syncing` with pid, start time and args → S7. A candidate that started before `launchAt`, or one in the S4 snapshot → END `already-running` (race; never quit; record cleared). Nothing in 90 s → S9 with outcome `did-not-start` (no pid, so nothing to quit; the S9 late scan of §4.4.3 may find one, which is announced, never quit; otherwise the record stays `launching` and the next run's S3 announces it only if a matching process exists) | 90 s |
| **S7 wait quiet** | up | poll every 10 s; fold digest; apply the quiet rules (§4.4.2); watch the process | quiet reached → S8 `ok` (or `ok-noactivity`); `now - launchAt >= max` → S8 `not-quiet`; the process disappeared → the successor scan of §4.4.3, then END `exited-early` (record cleared, or `left-running` if a successor was found; it was closed by Thunderbird itself or by the owner); ctx cancelled → S8c | until `launchAt + max` |
| **S8 quit** | outcome decided | re-verify ownership (§4.4.3) **immediately before the request**, and re-check owner activity (a started-hidden Thunderbird the owner has since opened is theirs: idle under 2 minutes → END `presync_owner_active`, Thunderbird left open, Q2); write record `quitting`; send **one** quit request (§4.7; a second request at +60 s is deferred, not v1); poll every 2 s. After the request, if our pid is gone, done; if a *different* Thunderbird-named process is now present, it is announced, never asked again | process gone → S9; still there at +120 s → END `quit-timeout`: record becomes `left-running` (kept, with identity, announced every run, PC2), the warning names the remedy | 120 s, run on its own context (PC14) |
| **S8c cancel quit** | run cancelled while ours is running | as S8 with a **15 s** grace (launchd's default ExitTimeOut is 20 s after SIGTERM, PC14); then END | 15 s |
| **S9 post** | process gone | wait 3 s, re-read the lock (must be released), run the late scan (§4.4.3), clear the record unless something is announced, update the streak, release the profile lock | END | 10 s |
| **END** | — | return warnings and the presync summary to `app.Run`; the archive reads | — | — |

Every state returns to `app.Run` through END; a panic anywhere is recovered at the hook's top
(`presync_internal_error`), the profile lock is released by the deferred release, and if S5 had
been reached the record stays so the next run recovers.

**Cancellation (Ctrl-C, SIGTERM, `launchctl` stop):** the context is cancelled; S7 goes to S8c.
If the process is killed outright (SIGKILL, power loss) nothing runs: §4.6.

**Worst-case time at the fixed defaults** (profile lock free, v1 with no leftover quit): 5 s plan
+ 1 s lock + 5 s recover + 5 s detect + 20 s start call + 20 min max wait counted from launch (the
90 s await-up is inside it) + 120 s quit + 10 s post = about 23 minutes before the archive reads;
**25 minutes is the figure the owner is told** and the figure the `expected_until` field uses. Revision 1
said 24 minutes while its own phases summed to 25.2 with a recovery quit, and set a hard deadline
of `max + 5 min` that the quit phase could overrun. Now (PC14): the wait phases (S1..S7) run under
one context deadline of `launchAt + max + 30 s`; **S8 and S9 run on their own context
derived without the session deadline** (a fresh 150 s, still cancelled only by the run's own
cancellation, which switches to S8c), so a session deadline can never be the reason a quit
request on a process of ours is not sent or is killed at once.

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

**One state-directory convention (PC12, OPEN-X1).** Three designs name a per-user state
directory differently: local-confidentiality uses `os.UserConfigDir()/mailarchive` (macOS, Linux)
and `%LOCALAPPDATA%\MailArchiveDesktop` (Windows desktop), and applies its P3 owner-only DACL to
every state directory the tool creates; the extraction workspace's run record (XC1) uses
`~/Library/Application Support/MailArchive`, `$XDG_STATE_HOME/mailarchive` and
`%LOCALAPPDATA%\MailArchive`. This design takes the one that already exists in code and in the job
file, `os.UserConfigDir()/mailarchive/presync/` on macOS, and mints no third one: the presync
directory is a **subdirectory of the product's config directory**, so local-confidentiality's P3
(the tool's own state directory) covers it unchanged on Windows when TP6 comes. The macOS path is
`~/Library/Application Support/mailarchive/presync/` (the case differs from the workspace's
`MailArchive` only in spelling; APFS is case-insensitive by default and the two would share one
directory). Reconciling the workspace's name with this one is a cross-design item for the operator
(§15 OPEN-X1); this design needs only that the presync subdirectory stays under whatever root
the product chooses, through one shared helper rather than a local `os.UserConfigDir` call.

State record fields (size bound 4 KiB; unknown fields tolerated; a newer `version` is read as
unreadable): `version`, `boot` (the boot time, macOS `sysctl -n kern.boottime` through
`/usr/sbin/sysctl`, PC10), `profile`, `app`, `state` (`launching` | `syncing` | `quitting` |
`left-running` | `idle`), `tool_pid`, `tool_start`, `launch_at`, `child_pid`, `child_start` (UTC
unix seconds, PC11), `child_args` (the `-profile` argument), `warn_streak` and `note_streak`
(consecutive sessions ending in a warning outcome, and in a note outcome other than
`already-running`; PC18), `streak_since`. **No `host` field and no `deadline`** (revision 1 had
both): the directory is local, so a hostname that macOS changes with the network would only
strand a record (PC10), and a wall-clock deadline is the thing a clock step or a DST change
breaks (PC11). The record's authority to *quit* is gone in v1 (Q4 interim); what remains of
it is the right to *announce*.

**Recovery table (S3), v1 (PC2, PC10, PC11).** The previous session's tool pid is dead (the
profile lock was free). Two separate questions are asked of a record, and they are kept apart
because revision 1 merged them and thereby made a failure silent after one night: *may the tool
quit this process?* (never, in v1, Q4 interim) and *must the tool keep telling the owner about
it?* (yes, for as long as the **same process** is open).

| Record state | Boot / Thunderbird now | Match against the record | Action |
|---|---|---|---|
| none or `idle` | any | — | normal |
| any | `boot` differs from this boot | — | the computer restarted, so nothing the record names can be running; clear silently |
| `launching` | no Thunderbird-named process | — | clear; note "an earlier run ended before Thunderbird started or while it was starting; nothing is open" |
| `syncing`, `quitting` or `left-running` | running | **exact match**: `child_pid` and `child_start` (±1 s) and executable path equals `app` and the process carries `child_args` | **a leftover of ours**: never quit in v1. Warn `presync_leftover_found` on this run **and on every later run while the same process stays open, with no expiry** ("a Thunderbird started by the scheduled sync on <date> is still open and was not closed: <why it was left, from the record>; quit it from the Dock"). The session then ends (S4 would only say "already running"). The recommended later rung (Q4: quit once, then `presync_leftover_closed`) is deferred, not v1 |
| `launching` | running | no `child_pid` was recorded, so no exact match is possible | **not matched**: announce only, as `presync_leftover_found` with the weaker text "a Thunderbird that may have been started by the scheduled sync on <date> is open", **only if** its start time is at or after `launch_at - 2 s` and its path equals `app`; otherwise it is the owner's, and the record is cleared |
| `syncing`, `quitting` or `left-running` | running | no exact match (the pid is reused, the start differs, the path or arguments differ, or no start time is available) | **not ours**: never touched, treated as "already running" (note); the record is cleared |
| `left-running` | no Thunderbird-named process matches | — | clear (the owner or the system closed it) |
| `quitting` | not running | — | clear |
| unparseable, newer version | any | — | treat as absent, warn `presync_state_unreadable` once |

PID reuse is defeated by comparing start time, path and arguments, never the pid alone (MA-558).
There is **no expiry on the announcement** and no wall-clock `deadline`: a record's only
time-dependent rule is the ±1 s match of `child_start`, computed from elapsed time and stored in
UTC (PC11), so a DST change, a manual clock change or an NTP step after sleep cannot turn a
still-open hidden Thunderbird into "the owner's" and silence the warning, and cannot make an
unrelated process look like ours. A stale record that never matches anything is bounded by its
4 KiB size and replaced by the next session. Whether the owner has since opened the process makes
no difference to the announcement; it only ever *speaks*.

**If the tool is killed while it started Thunderbird:**

| Killed at | Left behind | Next run |
|---|---|---|
| S5, before the exec | record `launching`, no Thunderbird | clear (row 2) |
| S5 after the exec / S6 / S7 | record `launching` or `syncing`, a hidden Thunderbird | the leftover row: announced, not quit in v1 (a `launching` record with no child pid is announced only with the weaker text); the lastrun is `running` with a dead pid, so `status` also shows `run_never_finished` until the next run replaces it. After a reboot or power loss the `boot` row clears it |
| S8, after the quit request | record `quitting`, Thunderbird probably closed itself | clear (row `quitting`, not running) |
| anywhere, the tool survives but Thunderbird crashes | record `syncing` | `exited-early` in the same run, or clear on the next |

The archive lock is released by the OS on any exit (`internal/lockfile`), so a killed session
never blocks the next run.

### 4.7 Per-OS mechanics

The rules for all platforms: exec with an argument list, no shell, no `sh -c`, no `cmd /c`; the
executable is an absolute, resolved path from a fixed table (v1; a validated `-thunderbird-app` is deferred, not v1);
**no `PATH` lookup of any name** (`exec.LookPath` is not called; a lookup seam is asserted never
used, MA-563); the child's environment is built from a short allowlist copied from our own
environment, never from job, profile or mail content (§5.5); helper children (`open`, `osascript`,
`ps`, `pgrep`, `caffeinate`) run under a context deadline and may be terminated by that deadline,
because they are ours; Thunderbird itself is never signalled.

Confidence column: **doc** = I am confident from vendor documentation or this repo; **memory** =
I believe it but have not verified it here; **unknown** = I do not know. Anything not **doc** is
a lab item that gates its slice.

#### 4.7.1 macOS (slice 1)

| Step | Exact command (argument list) | Confidence | Lab |
|---|---|---|---|
| Application | candidates in order: `/Applications/Thunderbird.app`, `$HOME/Applications/Thunderbird.app` (the home from `os.UserHomeDir`, never `$HOME` text from elsewhere); no `-thunderbird-app` in v1 (PC22). Accept only a directory ending `.app` whose `CFBundleIdentifier` is `org.mozilla.thunderbird`, read with `/usr/bin/plutil -extract CFBundleIdentifier raw -o - <app>/Contents/Info.plist` (a fixed helper under the §4.7 rules; a byte search of the file would wrongly refuse a binary plist, PC17), and that is not world-writable | bundle id: memory; `plutil -extract … raw`: doc (plutil(1)) | L-M1 |
| Is it running | profile lock (§4.4.1 A) and `/usr/bin/pgrep -x -u <uid> thunderbird` (B) | `pgrep -x -u` flags: doc; process name `thunderbird`: memory | L-M3 |
| Launch hidden | `/usr/bin/open -g -j <app> --args -profile <profile>`. `-g` does not bring the app to the foreground, `-j` launches it hidden, `--args` passes the rest to the app | `open -g -j --args`: doc (open(1)); that Thunderbird stays hidden and shows no window, dock bounce or focus theft: unknown | L-M2 |
| Find the pid | the profile `lock` symlink target (`host:+pid`) parsed by the existing `parseLockPID`; then `/bin/ps -p <pid> -o etime= -o comm=` and `/bin/ps -p <pid> -o args=` (two calls, parsed strictly) with `LC_ALL=C` in the allowlisted environment, for elapsed time (start = `Now()` minus `etime`, stored in UTC, PC11), executable path and the `-profile` argument (§4.4.3). `lstart` is **not** used: it is local time without a zone and is ambiguous in the repeated hour of a DST change | `ps -o etime,args`: doc; macOS `comm` giving the full path, `args` showing the `--args` we passed, and the lock symlink existing on macOS (it may not: then the process list alone finds the pid): memory | L-M4, L-M3 |
| Keep awake | `/usr/bin/caffeinate -i -w <our pid>` started as a helper at S5, ends when we do | flags `-i`, `-w`: doc (caffeinate(8)) | L-M7 |
| Quit (the app's own request) | `/usr/bin/osascript -e 'if application id "org.mozilla.thunderbird" is running then tell application id "org.mozilla.thunderbird" to quit'`. The script text is a constant; nothing is interpolated into it. The `is running` guard is there so that a quit request can never *launch* Thunderbird (whether `tell … to quit` launches an application that is not running is unknown, and whether the guard itself needs an Automation grant is lab item L-M8). **The request is delivered to the application, not to our pid (PC1):** the identity check of §4.4.3 runs immediately before it and our pid is looked at again immediately after; the residual window is stated in P2. Deadline 20 s | `osascript -e`: doc; `quit` as a standard Apple event: doc; the bundle id: memory; behaviour with two instances of the bundle: unknown | L-M1, L-M8, L-M8c |
| Quit denied | exit status non-zero with `-1743` ("not authorized to send Apple events") → outcome `quit-not-permitted`, Thunderbird left running, remedy (X2) "System Settings, Privacy and Security, Automation: allow <the program> to control Thunderbird; the permission is asked per program, so approve it from a run the scheduler starts (`launchctl kickstart gui/<uid>/<label>`), not only from Terminal". `-600` ("application isn't running") → treat as gone. **`-1712` (Apple event timed out) and our own 20 s deadline** are `quit-not-permitted` too (an unanswered permission prompt looks like a hang): the helper is terminated at its deadline, Thunderbird is left open, and the warning text names the prompt as the likely cause | `-1743`, `-1712` and `-600`: memory | L-M8 |
| Owner idle | `/usr/sbin/ioreg -c IOHIDSystem -d 4` (fixed helper, parsed strictly for `HIDIdleTime`, nanoseconds) → the owner-activity gate of S4 and S8 (PC7). Unparseable or helper failure counts as active | `ioreg` and `HIDIdleTime`: memory | L-M10 |
| Boot identity | `/usr/sbin/sysctl -n kern.boottime` → the `boot` field of the state record (PC10) | `kern.boottime`: doc (sysctl(8)) | L-M3 |
| Locked screen, no session | a launchd LaunchAgent in `~/Library/LaunchAgents` is loaded in the user's GUI session at login, so a locked screen still has the session; if `open` reports it cannot reach the window server, outcome `no-desktop-session` | unknown | L-M2 |
| Asleep | `StartCalendarInterval` jobs missed while asleep run on wake; the network may not be up yet, which the settle (§4.4.2) and `ok-noactivity` note absorb | memory | L-M7 |

The macOS Automation consent is the main risk of this platform, and it **decides whether the
headline feature is viable at all.** A permission to send Apple events to Thunderbird is
granted per requesting program. A manual run from Terminal is *not* a test of the scheduled run
(PC4): the request there is attributed to Terminal (or to whichever program is the responsible
process), a grant made there says nothing about the same binary started by launchd at 02:00, and
at 02:00 no one is there to answer a prompt. Revision 1's operator walk (§8) and its Q9 reason
("it is where macOS asks permission") rested on the opposite assumption. Therefore:

1. The **first approval must come from a run the scheduler starts.** `schedule -install` prints,
   after the preview and the §6 notice, the exact command to do it now while the owner is at the
   computer: `launchctl kickstart gui/<uid>/<label>` (L-M8 checks that this works for an agent
   loaded with `launchctl load`), and says what to expect ("Thunderbird opens and closes; if macOS
   asks whether Mail Archive may control Thunderbird, choose Allow"). `status` shows
   `presync_quit_not_permitted` until a scheduler-started run quits Thunderbird successfully.
2. **L-M8 and L-M9 are viability gates** (PC5). L-M8: does a grant to the launchd-run binary
   exist, persist and cover the 02:00 run? L-M9: is there a quit path that needs **no**
   Automation grant and is still the application's own request, for example a second invocation
   of `<app>/Contents/MacOS/thunderbird -profile <profile> -quit` forwarded to the running instance
   (the option's existence and its behaviour on Thunderbird are unknown; a direct exec of the
   bundle's binary would also need a rule for its identity, and is not designed here)? If both
   fail, **TP3's scheduled form does not ship** (Q16): a sync that opens Thunderbird every night
   and cannot close it leaves a hidden mail client permanently running, which is a worse state
   than no feature.
3. If consent is simply absent at run time the design degrades safely and loudly: the session
   starts Thunderbird, waits, fails to quit with a specific warning, leaves it open (hidden), and
   the next run **announces** it every night until it is closed (§4.6, PC2). It does not become
   silent, and it never kills anything. This is why §4.7.4 and Q3b matter.

#### 4.7.2 Windows (slice TP6, lab-gated; informative, deferred, not v1, NOT covered by the review, PC21)

*This section and §4.7.3 are kept as the author's record of intent. The pre-code review read them
only for consistency with macOS v1; none of their mechanisms (a `WM_CLOSE` to another process's
windows, Toolhelp, `SetThreadExecutionState`, a display-less Linux start) was attacked. Revision 1's
numbers and identity rules in them predate PC6, PC10 and PC11. Slice TP6 or TP7 starts only after
this section is rewritten against the v1 text and gets its own pre-code review.*

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
time with no crash-recovery or compaction prompt. The structural test (MA-557) forbids the
identifiers `syscall.Kill`, `Process.Kill`, `Process.Signal` and `os.Interrupt` sends in the
package, with a single named exception file added only by that future slice.

### 4.8 Concurrency

| With | Behaviour |
|---|---|
| another run of the tool on the same archive | the archive lock (`.mailarchive.lock`) is held for the whole session (D1a): the second run refuses with the existing message, before any process is touched |
| a `verify` or `reindex` on the archive | same lock; they refuse while a session runs, which is by design (D1a); the verify-vs-backup clash note in the README (line 1091) gains a sentence for sync |
| another schedule (another archive) that shares this Thunderbird profile | S2's profile lock serialises them; the second ends at once with the note `presync_profile_busy` and archives as today (a 5-minute wait is deferred, not v1, PC22). Two sessions never start or quit Thunderbird at once |
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
// WarnStreak, NoteStreak int (PC18); State string ("syncing" while the session runs, with
// Since, ExpectedUntil: the in-flight form the refusal and status texts read, PC8)
```

Warnings use the existing `Warnings []RunWarning` (`internal/state/lastrun.go:81`, set from
`app.Options.Warnings`), with codes prefixed `presync_`. The allowlist block in
`internal/health/health.go` (the "Problems the last run found with its own setup" block of
`assessRun`; cited by name, not line, PC20) lists the warning codes it will show (the record is writable by anyone who can write the
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
| `ok-noactivity` | note `presync_no_activity` (the escalation to WARN after 7 consecutive is deferred, not v1, PC22) | note | as `ok` but nothing changed during the whole session |
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
| `owner-active-skipped` | note `presync_owner_active_skipped` | note | the owner was at the computer at S4, so Thunderbird was not started (PC7); the run is today's run |
| `owner-active` | `presync_owner_active` | WARN | quit skipped at S8 because the owner seems to be using the Thunderbird we started (Q2); it is left open and announced |
| `successor-found` | `presync_successor_found` | WARN | our process went away and another Thunderbird is open (an update relaunch, a late start); never quit (§4.4.3, PC6) |
| `leftover-found` | `presync_leftover_found` | WARN | an earlier session's Thunderbird is still open (§4.6), announced every run while the same process stays open; `presync_leftover_closed` (the quit-it-once rung) is deferred, not v1 |
| `profile-unusable` | `presync_profile_unusable` | WARN | no or ambiguous or unsafe profile |
| `app-not-found` | `presync_app_not_found` | WARN | no acceptable application |
| `refused-elevated` | `presync_refused_elevated` | WARN | running as root or an administrator |
| `state-unreadable` | `presync_state_unreadable` | WARN | the state record could not be used |
| `watch-truncated` | note `presync_watch_truncated` | note | the watch set exceeded its cap |
| `internal-error` | `presync_internal_error` | WARN | a recovered panic |
| `cancelled` | — (the run's own cancelled status) | — | the run was cancelled |

**Escalation without latching (PC18):** two counters, because one cannot tell a warning night
from a note night. `warn_streak` counts consecutive sessions that ended in a WARN outcome;
`note_streak` counts consecutive sessions that ended in a note other than `already-running`
(which counts as neither, and does not reset either: an always-open Thunderbird is a fine state,
and is also a sync). An `ok` resets both; a WARN outcome resets `note_streak` and vice versa. At
`warn_streak` 3, `status` adds `presync_stuck` (WARN, never RED, mirroring FC18's "stuck is never
RED"): "Thunderbird has not synced on the last N runs". `note_streak` is displayed (`status` shows
"nothing changed for N runs") but escalates to nothing in v1 (deferred, not v1). RED stays
reserved for run-level failure and corruption.

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
schedule that has the setting on but whose last **completed** run started after the descriptor's
`InstalledAt` and has no `presync` record (an older binary ran it) gives WARN
`presync_not_recorded` (PC15). A schedule with no run since the install says "Thunderbird sync:
on, no run yet" and no warning; a run refused before the hook (the archive was in use) is not a
completed run, so it neither raises nor clears the warning. The text: "this schedule is set to sync
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
| F3 | cannot start (exec error, bad app) | "Thunderbird could not be started: <reason>" | archives | check that Thunderbird is installed in `/Applications` or `~/Applications`; reinstall it |
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
| F24 | the owner is at the computer when the run starts (a laptop that was asleep at 02:00 and runs the missed job when the lid opens) | note "you were using the computer, so Thunderbird was not started" (`presync_owner_active_skipped`) | archives what is on disk | none; the next night's run syncs (PC7) |
| F25 | Thunderbird relaunched itself (an update) or came up after the 90 s wait | "Thunderbird restarted itself and is still open" (`presync_successor_found`) | archives | quit Thunderbird from the Dock; nothing was closed for you (PC6) |
| F26 | the quit made Thunderbird run its exit actions (empty Trash, clean up) | not detectable by the tool; the install advisory and §6 say so beforehand | archives (after the exit actions ran) | change the account's setting, or do not schedule the sync (PC9) |
| F27 | a Thunderbird we could not close is still open on a later night | "a Thunderbird started by the scheduled sync on <date> is still open" every run, no expiry (`presync_leftover_found`, PC2) | archives | quit it; allow the permission (F10) |

### 4.11 Resource and time bounds

| Resource | Bound |
|---|---|
| Wall time | worst case about 23 min at the fixed defaults (told to the owner as 25): 20 min max wait from launch (the 90 s await-up is inside it), 120 s quit, a few seconds either side; S1..S7 under a deadline of `launchAt + max + 30 s`, S8 and S9 on their own 150 s context (§4.5, PC14); no profile-lock wait in v1 |
| Processes started | Thunderbird once; helpers: `open` once, `osascript` once, `plutil`, `ioreg`, `sysctl`, `ps`/`pgrep` a few per session, `caffeinate` once; none retried in a loop |
| Quit requests | at most 1 per session in v1 (a second at +60 s is deferred, not v1) |
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
| Application path | the fixed candidates (`-thunderbird-app` deferred, not v1) | absolute, resolved, regular file (Windows) or `.app` directory (macOS) with the bundle id; not world-writable; basename `thunderbird.exe` (Windows) |
| `prefs.js` (install-time advisory only) | the profile | read capped at 8 MiB through the existing `parsePrefs`; only `hostname` and `offline_download` values used, echoed control-stripped; never a path |
| `lock` symlink target | the profile | parsed as a pid with `parseLockPID`; the pid is never acted on until its executable path and start time match the launch (§4.4.3); never followed as a path |
| Folder and file names under the watch roots | mail, Thunderbird, any writer of the profile | never executed, never passed to a process, never interpolated; hashed; logged control-stripped and bounded; links not followed; entry and depth caps |
| The state record | the user's state dir | bounded to 4 KiB, schema-checked, host-checked, newer version = unreadable; control-stripped; confers no authority beyond "identify a process by start time and path" |
| Process list and `ps` output | the OS | parsed with strict layouts; anything unparseable = identity unavailable = nothing is quit |
| Environment | the scheduler | an allowlist is copied (§5.5); everything else dropped |

### 5.3 Attacker personas

| Persona | Attack | Mitigation | Residual / label |
|---|---|---|---|
| **Hostile profile or `prefs.js`** (a profile copied from elsewhere, an inherited store, a hostile add-on) | `directory` prefs pointing at `/` or a share to make the watcher walk or hang; a huge `prefs.js`; control characters to forge log lines; a `lock` symlink naming pid 1 or our own pid to get an innocent process "quit" | the run-time session reads no `prefs.js` and takes no path from the profile (watch roots are fixed: the named stores and `<profile>/ImapMail`, `<profile>/Mail`); the install advisory is capped and never uses a path; a lock pid is only a hint checked against executable path and start time; a quit goes by bundle id (macOS) or by windows of the proven pid (Windows), never by a pid from the lock alone. Proven (MA-562) |
| **Planted executable path** (another local account, malware without the user's rights, a writer of the archive folder) | a world-writable app (or, once deferred `-thunderbird-app` exists, that flag pointing at a script); the schedule or descriptor edited to name a different binary | settings never come from the archive (P6); in v1 the app is an absolute resolved path from the fixed table only (so there is no owner-supplied execution path at all), not world-writable, with the bundle id (macOS) / basename (Windows); no `PATH` lookup; the launch passes only `-profile <path>`. A same-user attacker who can edit the user's launchd plist already controls what runs (DC4) | Type I residual: a same-user attacker. On macOS the check proves the bundle id, **not** that it is Mozilla's: signature pinning (codesign Team ID) is not in v1 (Q12) |
| **Malicious mail-folder name** | a folder named `$(…)`, with a newline, `..`, RTL controls, 4,000 characters or a name that looks like a flag; deep or looping nesting to exhaust the walker | names are never used as a command, a flag or a path component beyond `Lstat`/`ReadDir` of what the OS lists; control-stripped and bounded in logs; no link following; 200,000-entry and depth-16 caps; the digest stores no names. Proven (MA-562) |
| **Co-resident process, another user on the machine** | read or tamper the state record or lock to make the tool quit another process; hold the profile lock; race the launch | state dir 0700 under the user's config dir; the profile must be owned by the user and not group- or world-writable; a quit needs the identity match; lock held by another account cannot be created in our 0700 dir. Windows: the same-user and ACL limits of design-schedule-v2 apply |
| **Co-resident process, same user** | impersonate Thunderbird by naming a process `thunderbird`; plant a fake `lock` | the identity check needs path and start time; a fake process cannot make its image path the resolved application. Same-user malware can do everything the user can (DC4) | Type I |
| **Archive writer** (a NAS or cloud folder others can edit) | edit the descriptor so `status` shows a false sync or a path; edit `.mailarchive-lastrun.json` so its `presync` block reads "ok" for a night that never synced (PC16) | `status` is display only and strips control characters; the run ignores the descriptor; the tool never executes anything named in it. Proven (MA-562) | the forged `ok` is the same trust the whole last-run record already has (anyone who can write the archive can write a false `ok`, DC4-class and Type I); the run record is a *report*, never an input, and the state record that holds any authority lives outside the archive |
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
   or send more than the one bounded quit request of v1.
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
the allowlist still applies to `open` itself. Proven (MA-563).

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
> goes online, and your message filters will run on whatever it fetches. When the backup closes
> Thunderbird, Thunderbird does what it does whenever you quit it: if an account is set to empty
> its Trash or clean up on exit, that happens before the archive is made, so mail you deleted
> since the last backup may not be archived. It runs out of sight, but its icon may appear in the
> Dock or the taskbar. If you are using the computer when the backup starts, or Thunderbird is
> already open, Thunderbird is left alone.

This is **conditional** on how Thunderbird is configured and is a lab observation to record
(L-C5: with a real profile that has a calendar, an Outbox item and a chat account, what leaves
the machine and what is sent; L-C6: what the exit actions remove, PC9). The text above is the
fixed source constant of MA-567, and the `-h` text of §4.2.2 is a short form of it that carries the
same four facts (all accounts, notifications, Outbox and filters, exit actions). The tool itself opens no new connection and handles no credential.

## 7. Migration and coexistence

| Direction | Behaviour |
|---|---|
| New binary reads an older descriptor, job file or last-run record | all lack the new fields; setting off; nothing changes. A schedule that was off stays off. No migration |
| Older binary reads the new descriptor | `json.Unmarshal` ignores the unknown field (`readDescriptorFile`); `status` of the older binary shows what it always showed (it cannot show the sync line or `presync_not_recorded`). Harmless to the archive |
| Older GUI binary reads a job file with `thunderbird_sync` | ignores it (`job.Read`): the backup runs **without** the sync and without saying so. The new `status`/health view then shows `presync_not_recorded` (§4.9.4) because the run record carries no `presync` field. `Version` is not bumped: an older GUI would otherwise refuse the whole backup (`internal/job/job.go:99`) |
| Older CLI binary runs a schedule that has `-thunderbird-sync` | `flag provided but not defined`: the run fails loudly, is recorded failed (an unattended refusal is a run, `RecordRefusedRun`) and `status` is RED, **and no backup is made on any night until the entry is removed or the binary is updated** (PC13): loud, but not harmless. The README rollback note already says: `schedule … -remove` first, because entries written by a new version carry flags older releases do not know (the rollback note in the README's schedule section, cited by content, not line). That note gains this feature and this consequence |
| Mixed versions sharing a profile | both read and write the same versioned state record; a newer record is read as unreadable by an older writer, which then treats the profile as having no record: it will never quit a process (it has no code to) |
| Rolling the feature back | `schedule … -install` without the flag (or *Turn off Thunderbird sync*); the state files stay (≤ 4 KiB each) and are inert; deleting `<UserConfigDir>/mailarchive/presync/` removes them |

Order of introduction (no flag day): fields and the hook land first, dormant (TP1, TP2); the
setting appears only with the macOS adapter (TP3 onward). The setting is per schedule, so no
existing schedule changes behaviour on upgrade.

## 8. Operator walk (the owner, on a Mac, CLI)

1. `mailarchive -thunderbird-sync -input ~/Library/Thunderbird/Profiles/x.default/ImapMail/mail.example.com -out ~/Archive` once by hand, with Thunderbird closed and the owner away from the keyboard for 2 minutes. The log says what it will do, Thunderbird starts out of sight, the run waits, Thunderbird closes, the archive runs. **This run proves the start, the wait and the profile; it does not prove the quit permission for the scheduled run** (revision 1 said it did, PC4): macOS may ask on this run "Terminal wants to control Thunderbird", and the answer is attributed to Terminal.
2. `mailarchive schedule -out ~/Archive -input … -thunderbird-sync -install`. The preview shows the flags and the notice of §6 first, then the exact command to start the installed job once now (`launchctl kickstart gui/<uid>/<label>`) and what to expect. **The owner runs it while at the computer and answers the macOS prompt for the program the scheduler runs.** `status` says "Thunderbird sync: on, no run yet" until that run, then `ok`, or `presync_quit_not_permitted` (with the remedy) until a scheduler-started run has closed Thunderbird.
3. Each night, 02:00: nothing visible unless Thunderbird shows a notification. In the morning `mailarchive status -out ~/Archive` shows "Thunderbird sync: … last run 02:00 → ok".
4. A night the owner is up: Thunderbird is open: "Thunderbird was already open, so it was not started or closed." A laptop that was asleep at 02:00 and runs the missed job when the lid opens at 07:00 with the owner at the keyboard: "you were using the computer, so Thunderbird was not started" (PC7).
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
| `Procs` (`List`, `Elapsed`, `ExePath`, `Args`, `Alive`, `BootTime`, `IdleFor`) | per-OS (`ps`, `sysctl`, `ioreg`; `/proc`, Toolhelp deferred, not v1) | a table of processes the fake Thunderbird creates; a settable boot time and owner idle time |
| `Lock` | `lockfile` | in-memory with a held-by-other mode |
| `Notify`, `Elevated` | per-OS | recorders (`Wake` is the `caffeinate` helper through `Runner`; no seam of its own) |

A **fake Thunderbird** is a scripted actor on the fake clock: it appears after `Start` (or never,
or only after 100 s), changes watched files on a schedule (bursts, a long silence, a periodic
touch, none), goes quiet, ignores the quit request or honours it, exits and **relaunches itself as
a new pid** (an update), crashes mid-way, or hands over. The clock also steps: DST fold, an NTP
step back and forward, a suspend jump (PC11).
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

### 9.4 The lab script (lab rows MA-573..MA-577; results recorded in the review file, not in this design)

Run on a real Mac with a real Thunderbird profile that has one IMAP account with offline storage
and one POP or Local Folders account, an Outbox with nothing in it for the first pass. The human
runs the commands; each step records what was observed. A step whose expectation is false is a
finding for the design.

- **L-M1 identity.** `plutil -p /Applications/Thunderbird.app/Contents/Info.plist` (expect `CFBundleIdentifier` `org.mozilla.thunderbird`).
- **L-M2 hidden start.** With Thunderbird quit: `open -g -j /Applications/Thunderbird.app --args -profile "<profile>"`. Observe: no window, Dock icon, focus change; whether a window appears after some seconds; with the screen locked and the display asleep (start it from `ssh` into the logged-in user's session with `launchctl asuser <uid> /usr/bin/open …` or from a launchd test job), and with no one logged in. **Also (rank 3 overall):** from a real LaunchAgent run of the built binary, record that Thunderbird is started in the user's GUI session, that it **survives the agent's exit and a SIGKILL of the agent** (launchd cleans up a job's process group; an app started through Launch Services should not be in it: confirm, because the kill-drill rows assume it), and which process is its parent.
- **L-M3 detection.** While running: `ls -l "<profile>/lock"` (expect a symlink `…:+<pid>`), `/usr/bin/pgrep -x -u $(id -u) thunderbird`, `pgrep -x thunderbird-bin`; record the process name.
- **L-M4 identity data.** `/bin/ps -p <pid> -o pid=,etime=,comm=` and `-o args=` with `LC_ALL=C`: record the exact `etime`, `comm` and `args` strings (does `args` show `-profile <path>`, and with what quoting); do it while a second `open` of the same app is made (expect the pid and start unchanged). Repeat across a DST change or with the clock set an hour back, and record that the derived start time does not move.
- **L-M5 quiet.** Add a message on the server out of band (send the account an email), start hidden, and log `stat` (size and mtime) of every file under `ImapMail/<acct>` every 2 s into a file; record when the first change, the last change and the visible new message appear, whether `.msf` changes with no mail, and how long the digest was quiet before and after. Repeat with the network off (expect no change). Repeat with a folder that is not selected for offline use.
- **L-M6 profile.** Start with `-profile` pointing at a profile not in `profiles.ini`; start twice; start with another profile already running.
- **L-M7 sleep.** Schedule a test launchd job at a time the Mac is asleep; record whether it runs on wake, how long until the network is up, and whether `caffeinate -i -w` holds the Mac awake for 25 minutes on battery and on power.
- **L-M8 quit (VIABILITY GATE, rank 1).** Use the exact constant script of §4.7.1. (a) From Terminal: record the prompt and **which program name it names**. (b) **From a launchd test job running the built binary** (a LaunchAgent in the real install form, started with `launchctl kickstart gui/<uid>/<label>` while you sit there, then again from its calendar slot at 02:00 with the screen locked): record the exit status, whether a prompt appeared and waited, whether the 20 s deadline killed `osascript` and what the prompt did afterwards, and whether the grant from (a) covers (b) (expected: it does not; record it). (c) Revoke the grant (`tccutil reset AppleEvents`) and rerun from the scheduler: record `-1743` vs `-1712` vs a hang. (d) Does the grant persist across a macOS update, a binary rebuild, a move of the binary? (e) Repeat with an unsent draft open (expect a dialog and no quit) and with a Primary Password prompt open. **L-M8b**: on a disposable copy of the profile with a large folder, record the effect of `kill -TERM <pid>`: does Thunderbird flush, remove `lock`, and start next time without a recovery prompt? (informs Q3b only; never used in v1). **L-M8c**: with two Thunderbird instances (two profiles, `-no-remote`) record which one the script quits, and whether `if … is running` avoids launching the app when it is not running.
- **L-M9 quit without Automation (VIABILITY GATE, rank 1b).** Is there any request to a running Thunderbird to quit that needs no Automation grant and is the application's own (a `-quit` command-line option forwarded to the running instance, a documented remote command)? Record the command, whether it needs a direct exec of `Contents/MacOS/thunderbird` (and so its own identity rule), and whether it works from a launchd job. Result decides Q16.
- **L-M10 owner idle.** `ioreg -c IOHIDSystem -d 4`: does `HIDIdleTime` appear for an agent started by launchd, with the screen locked and unlocked, and does it reset when you move the mouse? Is it the owner's idle or a system value?
- **L-C6 exit actions.** With an account whose "Empty Trash on Exit" and "Clean up on exit" are on, move a message to Trash, run one scheduled session: record whether it is gone from the server and from the archive afterwards, and whether Thunderbird compacts local folders on quit.
- **L-C1 truncated last message.** With a script appending to an mbox in Thunderbird's format, run `mailarchive` repeatedly while it appends; record whether a truncated message is archived, how it is classified, and whether the next run replaces it or leaves a duplicate.
- **L-C2 kill drills (MA-576).** Start a session; `kill -9` the `mailarchive` pid at each state (during up, syncing, after the quit request); rerun; record the state record, the process list, the warning shown. Power-cycle drill at "syncing" on a spare machine.
- **L-C3 suspend.** Close the lid during S7 for 10 minutes; record that the quiet clock reset and the session ended.
- **L-C4 modals.** Start with a Primary Password set; with an expired OAuth token; with a profile created by a newer Thunderbird: record no activity and whether quit is honoured under the modal.
- **L-C5 privacy (MA-577).** With a calendar, a chat account and an Outbox message: record what Thunderbird sends and what it shows; write the measured list into §6's text.
- **L-W1..L-W5 and L-L1..L-L3:** the same shape, using Task Scheduler (interactive) with `thunderbird.exe` hidden launch, Toolhelp enumeration, `PostMessage(WM_CLOSE)` to the hidden window, `SetThreadExecutionState`; and on Linux a `systemd --user` timer with the display variables, the `-headless`/`-quit` existence question and SIGTERM.

## 10. Catalog rows proposed (nothing is added to the catalog here)

Ids are provisional (MA-549..MA-579: the rulings' next free overflow id was MA-546, but
MA-546..MA-548 are now taken by the desktop local-origin work, PC19; R30..R32, S53..S58, X16
follow R29, S52, X15 and are to be re-checked against the operator's log at TP0). Each lands with
its slice's catalog-first PR.

### 10.1 Invariants

- **R30 — Thunderbird pre-sync is opt-in and inert when off.** With the setting absent or false
  no process, profile or state-directory operation of this feature occurs, on any surface,
  default OFF. The setting is read only from the program's own arguments or the user-owned job
  file, never from the archive directory.
- **R31 — The tool starts and quits only what it can prove it started, only by asking.** A
  Thunderbird that was running when a session began is never started or quit. A quit request is
  sent only when the process is identified by executable path, start time and `-profile` argument
  at launch and again immediately before the request, at most once in v1; **where the platform
  delivers the request to the application and not to a pid (macOS), the check gates the request
  and the residual delivery window is stated and bounded, not eliminated (PC1)**; Thunderbird is
  never signalled or terminated, its files are never written, and no keystroke or window
  automation occurs. Nothing is quit when identity cannot be proven, and a leftover found by a later run is
  announced and not quit.
- **R32 — A sync never gates the archive, is always loud, and is always bounded.** Every outcome
  other than success is recorded as a specific warning (what, why, remedy) in the run record and
  shown by the summary, `status` and the GUI health view; the archive runs after every outcome
  except cancellation; every wait is bounded; a session killed at any point leaves a state the
  next run recovers from without quitting a process it cannot prove it started.

### 10.2 Scenarios

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S53 The owner schedules a Thunderbird archive with "Sync Thunderbird before archiving", and at 02:00 Thunderbird is closed | R30, R31, R32, R18 | Thunderbird is started hidden, left until its mail files are quiet, asked to quit, and the archive then reads; the summary and `status` say so | MA-549, MA-550, MA-552, MA-555, MA-556, MA-561, MA-569 |
| S54 The owner has Thunderbird open when the run starts, or opens it during the session | R31, R32, R5 | already open: not started, not quit, the run is today's run and says so; opened during the session: the quit is still only a request and unsaved work blocks it | MA-553, MA-554, MA-559, MA-561 |
| S55 The tool is killed, crashes or loses power while it started Thunderbird | R31, R32, R18 | the next run reads the write-ahead record: a Thunderbird provably ours inside the window is asked to quit, anything else is left alone and named; no hidden Thunderbird is left unannounced | MA-558, MA-565, MA-576 |
| S56 The sync cannot complete: cannot start, no session, never quiet, a modal, Automation denied, will not quit, profile locked, machine asleep or screen locked | R32, R31, X2 | a specific warning with its remedy, the archive still runs, Thunderbird is left running if it would not quit; nothing is forced | MA-560, MA-561, MA-570, MA-571, MA-573, MA-574 |
| S57 A hostile profile, `prefs.js`, lock, mail-folder name, application path, environment or archive-resident descriptor | R31, R30 | nothing outside the fixed watch roots is walked, no path is taken from the profile or the archive, no PATH lookup, a pid from a lock is never acted on alone, no shell | MA-557, MA-562, MA-563, MA-564, MA-572 |
| S58 An older binary meets the schedule, job file, descriptor or run record | R32, R12 | an older CLI refuses the unknown flag loudly; an older GUI or descriptor reader ignores the field harmlessly; the new `status` says the sync was not reported | MA-551, MA-561 |

### 10.3 UX rule

- **X16 — A setting that makes the tool act on another application says so, in owner words, at
  the point of choosing it.** The `-h` text, the schedule preview, the GUI question and the README
  state what that application will do (network, notifications, sounds, its other accounts),
  that it is off by default, and what is left alone. Proven structurally (MA-567), Partial until
  its friction review closes.

### 10.4 Test-spec rows

Tier U = every commit; S = structural; L = lab, pending. The last column is the mutation that must
turn the row red before it is reverted.

| ID | Tier | Asserts | Covers | Prove-fail |
|---|---|---|---|---|
| MA-549 | U | with the setting absent or false (every surface: flags, job file, descriptor), `app.Options.BeforeRead` is nil, the fake `Env` records zero calls (no exec, no `Lstat` of a profile, no state-directory write), and the run's output equals a golden run without the feature; positive twin with the setting on | R30, S53 | default the flag to true → the zero-calls assertion fails |
| MA-550 | U | `parseJob` and the GUI step refuse each §4.2.4 condition naming what, why and remedy with the side effect absent (nothing installed); the baked argv is canonical and absolute; the deferred flags (`-thunderbird-sync-quiet`, `-sync-max`, `-thunderbird-app`) are refused with the fixed-waits text; a derived profile absent from `profiles.ini` is refused unless `-thunderbird-profile` names it (PC3); `schedule -install` prints the §4.7.1 first-approval command (PC4); positive twin | R30, S53, X16 | drop the hourly refusal → the hourly case installs |
| MA-551 | U | an older-shaped `Descriptor`, `job.Job` and `LastRun` (structs without the new fields) round-trip against current files carrying them, other fields intact; the current reader of an old file reports "not configured"; health shows `presync_not_recorded` for a configured schedule whose last completed run started after `InstalledAt` and has no `presync`, and shows "on, no run yet" (no warning) before the first run after the install and ignores a refused run (PC15); an older-flag CLI argv refuses with "flag provided but not defined" and records the run failed | R32, S58 | switch the decoders to `DisallowUnknownFields` → the older-reader case fails |
| MA-552 | U | the happy path on the fake clock: the exact transition sequence S1..S9, the exact argv of each helper and of the launch, the write-ahead record is on the fake disk before the first `Start`, the record is cleared at the end, both streaks are reset, worst-case time at the fixed defaults is within 23 min and the `expected_until` the running record carries is `since + 25 min`; the running record carries `presync.state = syncing` from S1 and the "in use" refusal, `status` and `verify`'s refusal quote it (PC8) | R30, R31, R32, S53 | write the record after the launch → the ordering assertion fails |
| MA-553 | U | Thunderbird running by signal A, by signal B, by both, and on another profile: zero `Start` and zero quit requests, the outcome and note are exact, the archive runs and says so; stale lock with a recycled non-Thunderbird pid reports "not Thunderbird" and does not start | R31, S54 | skip the running check → a launch is recorded |
| MA-554 | U | ownership: a candidate that started before `launchAt - 2 s`, one in the S4 snapshot, one with another path, one without our `-profile` argument, two candidates, and no start time each produce zero quit requests and the right outcome; a single matching candidate is quit once; the check is repeated immediately before the request and our pid is looked at again after it (a different Thunderbird-named process present afterwards is announced, never asked again, PC1); a pid reused with another start time is not ours; the proven process vanishing and a new pid appearing (an update relaunch) gives `presync_successor_found` and no quit; a candidate appearing only after the 90 s wait is found by the S9 late scan and announced, never quit (PC6); the derived start time is identical across a DST fold, an NTP step and a suspend jump (PC11) | R31, S54, S55 | compare the pid only → the reused-pid case quits |
| MA-555 | U | quiet decision on the fake clock: growth, an mtime-only touch, a new file, a rename and a deletion each reset it; no change at all gives `ok-noactivity` at exactly settle + quiet; a 30 s+ gap resets (suspend); max wait ends the wait at exactly `launchAt + max` with `not-quiet`; entry and depth caps give `watch-truncated`; links are not entered; no file is opened (the fake counts opens: zero) | R32, S53, S56 | ignore mtime → the touch case is not seen |
| MA-556 | U | the quit procedure: exactly one request, none after +120 s, the exact argv (including the `is running` guard), a quit that works, one that never works leaves `left-running` with identity, the outcome is `quit-timeout`; the cancel path uses the 15 s grace; S8 runs on its own context, so an expired session deadline still sends the request (PC14); a request is never sent when the pre-request identity re-check fails or when the idle gate says the owner is active | R31, R32, S53, S56 | add a second request → the count assertion fails |
| MA-557 | S | an AST and import walk of `internal/presync`: every exec site goes through `Runner`; no `sh`, `bash`, `cmd`, `-c`, `/c`; no `exec.LookPath`; paths passed to `Runner` are built only from the resolved candidates; none of `syscall.Kill`, `Process.Kill`, `Process.Signal`, `taskkill`, `-9`, `SIGKILL`, `SIGTERM` appears (a single named exception file is allowed only when slice TP8 is accepted); no write function is called on a profile-derived path; the argv catalog (per OS, every helper and its argument shapes) equals the golden list | R31, S57 | add a `Process.Kill()` call → the walk fails |
| MA-558 | U | recovery table: every row of §4.6 as a table test (state × boot × running × exact-match × age): clean, a different boot clears silently, leftover announced and never quit, the announcement repeats on every run for as long as the same process is open **including one year later and across a DST change, a clock step back and an NTP step** (no expiry, PC2, PC11), not ours (pid reused, other start, other path, other arguments), a `launching` record with no child pid announced only with the weaker text, torn or newer record = unreadable and warned; no `Runner` call that quits is ever made from S3 | R31, R32, S55 | match on pid only → the reused-pid row quits |
| MA-559 | U | `app.Run` calls the hook once, after the archive lock and the `running` record, before any read; a second concurrent `Run` refuses with the existing in-use message and starts nothing; two sessions on one profile serialise through the profile lock (the second ends at once with `profile-busy`, zero starts); a manual `verify` and a manual run during a session refuse with the "in use" text **that names the Thunderbird sync and the expected end** (PC8), a record without the field reads as today | R30, R32, S54 | call the hook before `lockfile.AcquireAs` → the order assertion fails |
| MA-560 | U | for each outcome in §4.9.3 (including a panic in the session and a state-dir write failure) `app.Run` still archives the fixture mbox store, returns nil, writes `status: ok` with the warning recorded, and the exit is unchanged; cancellation is the only early stop and still sends the cancel-path quit for a process of ours | R32, S56 | return the session error from `Run` → the run fails |
| MA-561 | U | surfaces: each warning code's summary text, `status` text, `status -json` additive keys (`JSONVersion` 2), run-record field and GUI health text contain what, why and remedy substrings; no path secret, no `panic`, no control characters; unknown codes in a hostile run record are ignored by `health`; the two counters (`warn_streak` to 3 gives `presync_stuck`; notes escalate to nothing in v1; `already-running` counts as neither and resets neither, PC18) and `presync_stuck` are WARN, never RED; the pairing line with `missing-body` appears | R32, S53, S54, S56, S58, X2 | drop the remedy from one text → the substring check fails |
| MA-562 | U | hostile inputs: a `prefs.js` with a `directory` pref at `/`, a 1 GiB file and control characters (the run reads none; the install advisory caps at 8 MiB and uses no path); a `lock` naming pid 1, our pid and a non-Thunderbird pid (never quit); folder names with newline, `..`, RTL, 4,000 characters and flag-like names (watched, never executed, logged stripped, no escape); symlinked mail directories (not entered); a world-writable `.app`, one whose `plutil` bundle id differs (including a binary plist, accepted when right) and a symlink to elsewhere (refused); a profile that is a backup copy, an old copy or a disk-image mount, and so absent from `profiles.ini`, refused unless `-thunderbird-profile` names it, and a hostile `profiles.ini` (huge, control characters, a path entry to `/`) read capped and used only for comparison (PC3); an archive-resident descriptor naming a different profile (ignored by the run) | R31, R30, S57 | read the profile from the descriptor → the planted-descriptor case runs it |
| MA-563 | U | environment: with `DYLD_INSERT_LIBRARIES`, `LD_PRELOAD`, `MOZ_*`, a hostile `PATH` and `TMPDIR` set, every recorded `Cmd.Env` equals the allowlist exactly; the lookup seam is never called | R31, S57 | pass `os.Environ()` → the equality fails |
| MA-564 | U | no writes to Thunderbird: a whole-tree hash (names, sizes, modes, bytes) of a fixture profile and application before and after every scenario of MA-552..MA-560 is identical; the tool's own files exist only in the state directory, with modes 0600 and 0700 under umask 022 and 0 (Unix) | R31, S57 | touch a file under the profile → the hash differs |
| MA-565 | U | persistence: the state record is atomic (a crash hook between temp write and rename leaves the old or the new file), bounded to 4 KiB, tolerant of unknown fields, newer version = unreadable, the streaks survive restarts and reset on ok, no field holds a hostname or a wall-clock deadline (PC10, PC11); at most one record and one lock file per profile; no growth over 1,000 simulated sessions | R32, S55 | write in place, not by rename → the crash hook leaves a torn file |
| MA-566 | U | GUI and job: the second question appears only on macOS with a Thunderbird store among the inputs and defaults to No; Yes writes `thunderbird_sync` with the derived profile (checked against `profiles.ini`); the golden wording equals §4.2.6; `runHeadless` builds the hook from the job file only; a WARN outcome calls the notifier once, a note or ok never; *Turn off Thunderbird sync* removes the field and the descriptor entry | R30, X16, S53 | default the dialog's selection to Yes → the default assertion fails |
| MA-567 | S | owner-facing text: one source constant holds the §6 paragraph; the `-h` text, the schedule preview notice, the GUI question and the README section contain it (or its fixed sentences); the README documents the flags, the one-profile limit, the §4.4.4 limits and the rollback note | X16, R30 | remove the notification sentence from the GUI text → the coherence check fails |
| MA-568 | S | the seam: `BeforeRead` is read in exactly one place in `internal/app/run.go`, after `beginRun`; `RunGraph`, `reindex`, `verify`, `extract` never set or call it; with it nil, `Run`'s observable behaviour is unchanged (golden of the existing run-record fields) | R30, S53 | call it at the top of `Run` → the position assertion fails |
| MA-569 | U | schedule integration: the cron line, the plist, the Task Scheduler XML and the wrapper carry the flags exactly (quoting and `%` cases included); `Preview` prints the notice; the descriptor gains `presync` with `Version` 1 and the install's collision rules are unchanged; `-remove` leaves the state directory | R30, S53 | drop the flags from the plist → the argv assertion fails |
| MA-570 | U | macOS consent: the fake `osascript` returns `-1743` → `quit-not-permitted` with the System Settings remedy that names the scheduler-started first approval, Thunderbird left running, the record becomes `left-running`; `-1712` and a 20 s timeout are the same outcome, kill only the helper, and the text names an unanswered prompt as the likely cause; `-600` is treated as gone; the next night's run announces the leftover again (MA-558) | R31, R32, S56 | map `-1743` to success → the outcome assertion fails |
| MA-571 | U | no session: the fake `open` failing (window server unreachable) or a missing display gives `no-desktop-session` with no retry loop and no launch record left; a locked screen with a working `open` proceeds | R32, S56 | retry the launch in a loop → the call-count assertion fails |
| MA-572 | U | elevation: euid 0 or an elevated token (faked) refuses at S1 with `presync_refused_elevated`, zero helpers, the archive runs | R31, R32, S57 | skip the check → a launch is recorded |
| MA-573 | L | macOS end to end, the §9.4 L-M1..L-M10 script: hidden start and survival of the agent's exit, detection, identity data, quiet, profile, sleep, quit under launchd with the screen locked (L-M8, L-M9: the viability gates), owner idle — **pending** (a real Mac and a real Thunderbird profile) | R31, R32, S53, S56 | — |
| MA-574 | L | Windows end to end (L-W1..L-W5) — **pending** (a Windows machine) | R31, R32, S53, S56 | — |
| MA-575 | L | Linux (L-L1..L-L3, a display, the quit options) — **pending** (a Linux desktop); may close as "not supported" | R31, S56 | — |
| MA-576 | L | crash drills on a real Thunderbird (L-C2, L-C3, L-C4): kill the tool at each state, power-cycle, suspend, modals — **pending** | R31, R32, S55 | — |
| MA-577 | L | privacy observation (L-C5), the exit-actions observation (L-C6) and the truncated-last-message heal (L-C1): measured, and §6's text and §4.9.1's claim updated from it — **pending** | R32, X16 | — |
| MA-578 | U | profile membership and the first-approval path (PC3, PC4): the `profiles.ini` parser on a table of files (`Path` relative and absolute, `Install*` defaults, a trailing slash, symlinked paths, a huge file, control characters) accepts only a listed canonical path; the install preview prints the `launchctl kickstart` command and the `status` line `presync_quit_not_permitted` persists until a scheduler-started run quits successfully; positive twin | R30, R31, S53, S56 | accept an unlisted derived profile → the backup-copy case starts Thunderbird |
| MA-579 | U | owner-activity and exit-action advisories (PC7, PC9): owner idle under 2 minutes (and an unparseable `ioreg` result) at S4 → `owner-active-skipped`, zero starts; idle at S4 and active at S8 → no quit request, Thunderbird left open and announced; the install advisory text for `empty_trash_on_exit` accounts, values control-stripped | R31, R32, S54, S56, X16 | treat an unparseable idle reading as idle → the start is recorded |

## 11. Build slices (small, in dependency order)

Each slice adds its rows to the catalog first (operator-accepted), ships prove-fail then
prove-pass, leaves the covers map green, and says what is shippable. "Nothing" means the slice
adds dead, tested code that no user-visible path reaches.

| Slice | Contents | Depends on | Rows | Shippable? | Gates owed after build |
|---|---|---|---|---|---|
| **TP0** | catalog-first PR: R30..R32, S53..S58, X16, the MA rows as pending/real per slice; README and ux-contract text for X16 | the pre-code review (TG1, **met**) and Q-answers that block (§13: Q2, Q4, Q16, Q17) | all | nothing | none |
| **TP1** | `internal/presync` core, pure: `Env` seams and fakes, the quiet detector and the watch digest, the state machine S0..S9 against fakes, the state record and lock, the recovery table (announce-only), outcomes and warning codes; no OS code, no wiring | TP0 | 549 (core), 552, 553 (the running-detection half), 554, 555, 556, 558, 564, 565 | nothing | none (internal) |
| **TP2** | the seam and the record: `app.Options.BeforeRead`, `LastRun.Presync` (including the in-flight `syncing` state), `Warnings` plumbing, health codes, `status` text and `-json` keys, `presync_not_recorded`, the "in use" refusal text for run and `verify`, the summary lines, descriptor field and job-file field (additive, unused) | TP1 | 549 (surfaces), 551, 559, 560, 561, 568 | nothing user-visible (no source sets the hook); the new `status` reading is inert | none |
| **TP3** | macOS adapter (`open`, `osascript`, `ps`, `pgrep`, `plutil`, `ioreg`, `sysctl`, `caffeinate`), the exec seam's real implementation, the env allowlist, the consent outcome, the owner-activity gate, `profiles.ini` membership, elevation refusal, and the CLI flags on the **manual** export run | TP2; **lab L-M1..L-M10 answered, with L-M8 or L-M9 showing a quit path that works from a scheduler-started program (TG3, Q16)** | 557, 562, 563, 570, 571, 572, 573, 576, 577, 578, 579 | **first shippable:** `mailarchive -thunderbird-sync …` on macOS, manual | **adversarial pass** (new process control, untrusted profile reads, a quit request to another application) and **friction review** (the §8 walk, every F-row) |
| **TP4** | `schedule` integration: `parseJob` validation, the baked flags, the descriptor `presync`, the preview notice with the first-approval command, README (including the verify-window sentence) and the install-time `offline_download` and exit-action advisories | TP3 | 550, 567, 569 | **shippable: scheduled sync on macOS (CLI)** | short adversarial note on the descriptor and the advisory's `prefs.js` read; friction review of the install walk |
| **TP5** | `mailarchive-gui`: the schedule-step question, the job-file field, headless wiring, health-view line, *Turn off*, notification | TP4 | 566 | **shippable: GUI schedule on macOS** | friction review (wizard flows are lab-tier like design-schedule-v2's) |
| **TP6** | **Deferred, not v1, needs its own design review (PC21).** Windows adapter: Toolhelp, `GetProcessTimes`, `PostMessageW`, `SetThreadExecutionState`, hidden launch | TP5; **lab L-W1..L-W5** | 574 (and MA-557's Windows argv catalog entries) | shippable: Windows | a pre-code review of §4.7.2 rewritten against v1, then an adversarial addendum (user32 calls, a `WM_CLOSE` to another process's windows) + friction |
| **TP7** | **Deferred, not v1, needs its own design review (PC21).** Linux adapter, only if L-L1..L-L3 find a documented quit and a usable display story | TP6; lab | 575 | shippable only if the lab allows | as TP6 |
| **TP8** | optional rungs decided by Q3b and Q4 (SIGTERM rung, the leftover quit-once rung, the second quit request, the 5-minute profile-lock wait, configurable quiet and maximum waits, `-thunderbird-app`, the note escalation) | the lab and the owner's rulings | extends 556, 558 | shippable | adversarial note per rung |

TP1..TP2 are the smallest independently reviewable unit; TP3 is the first slice an owner can use.
TP0 is not an implementation slice. **The v1 scope is TP0..TP5 on macOS.**

## 12. Honesty of claims

- P1 (inert when off), P3's "archive runs after every outcome" and P4 (inside the lock and the
  record): **proven** by MA-549, 560, 559, 568 once TP1..TP2 are built.
- P2 (never kills, only ours): the *code-level* claim (nothing is signalled, killed or written;
  every quit request is preceded by the identity check) is **proven** by MA-554, 556, 557, 564 once
  built. The claim that the macOS request reaches *only* our process is **conditional** and not
  provable: it is delivered by bundle id (PC1), with a stated millisecond window. The
  *behavioural* claim that Thunderbird exits cleanly on the quit request, on each OS, is
  **lab-pending** (MA-573..MA-575): nothing in this document is lab-validated.
- P5 (a crash never leaves a hidden Thunderbird forever): **not assured, announced** in v1: a
  leftover is named in a warning on every run while the same process stays open (PC2), and
  cleared by a reboot (PC10). It is **conditional** on the platform giving a process start time
  and arguments (§4.4.3).
- P7 (older binaries): an older CLI stops the backups loudly; an older GUI skips the sync and only
  a newer `status` says so (PC13).
- The sync's usefulness (P8): **conditional** on Thunderbird's own folder-check and offline
  settings (§4.4.4) and, as to whether file silence marks the end of a fetch, **aspirational** until
  L-M5. The "quiet" claim is a heuristic until then.
- "Hidden": **aspirational** (L-M2, L-W3, L-L1). The text promises "out of sight where the system
  allows" and says a window or Dock icon may appear.
- macOS quit under launchd: **aspirational** until L-M8 (or L-M9) shows a quit path that works
  from a scheduler-started program; it decides whether the feature is viable at all (Q16). Until
  then the feature may start Thunderbird and fail to close it on every night (F10, F27); that
  failure is loud every night, not silent.
- "The owner is not at the computer": **conditional** on `HIDIdleTime` meaning what §4.7.1 says
  (L-M10); an unreadable value counts as "at the computer".
- Privacy and side effects (§6): **conditional** on the profile's configuration, to be measured
  (L-C5, L-C6).
- Status: reviewed, conditions folded (macOS v1); not built, not lab-validated.
  `docs/review-thunderbird-presync-predesign.md` is filed; the Windows and Linux text is not covered by it.

## 13. Owner questions

Each has a recommended default and a safe interim (what happens if the owner does not answer; it
must never weaken R31 or R32). A question marked *blocks* holds only the slice named.

| # | Question | Recommended default | Safe interim | Blocks |
|---|---|---|---|---|
| **Q1** | Default quiet period and maximum wait? | quiet **3 min**, max **20 min** (your suggestion), settle 60 s fixed | the same | nothing |
| **Q2** **OPEN** | When should the tool leave Thunderbird alone because you are at the computer? A laptop that was asleep at 02:00 runs its missed backup when you open the lid, so "you are present" is the common case on a laptop, not a rare one (PC7). | **Check before starting and again before quitting:** no keyboard or mouse use for 2 minutes (macOS `HIDIdleTime`) is "away"; if you are present, do not start Thunderbird (a note, "you were using the computer, so Thunderbird was not started"), and if you opened the Thunderbird we started, do not quit it (a warning that it was left open). Thunderbird's own quit still asks about unsaved drafts | **the same gate is in v1 as written**; with the gate unanswered the 2-minute threshold stands. Without the lab result (L-M10) an unreadable idle time counts as present (nothing is started) | **TP3** (revision 1 deferred it to TP8) |
| **Q3** | A locked screen: attempt the sync? | **Attempt** (a locked Mac or PC is the normal 02:00 state and the user's session still exists); if Thunderbird cannot start, warn (F4/F16) | attempt | TP3 (L-M2) |
| **Q3b** | May a SIGTERM rung ever be used to close Thunderbird if the platform's polite request is unavailable? | **No, not in v1.** Only if the lab (L-M8b, L-L3) proves a clean shutdown on a large profile | no | TP8 |
| **Q4** **OPEN** | If a run was killed (or could not close Thunderbird) and the next run finds that same Thunderbird (matched exactly: pid, start time, path and `-profile` argument) still open: ask it to quit? Revision 1 used a 32-minute window; the review removed the window (PC2, PC11). | The review recommends **No for v1: leave it and warn on every run** (`presync_leftover_found`, no expiry), so a hidden Thunderbird is announced and never closed by a record that may be stale; the quit-once rung (`presync_leftover_closed`) is TP8 after the lab shows the exact match is reliable. A reboot clears it either way | **No: announce only** (v1 as written) | TP3 (needs L-M4 identity) |
| **Q5** | Ship Windows and Linux in the first slice, or macOS first? | **macOS first** (TP3..TP5), Windows after its lab (TP6), Linux only if the lab finds a usable quit (TP7) | macOS only; other platforms refuse at schedule time with the sentence in §4.2.4 | TP6, TP7 |
| **Q6** | A repeated warning: when does it become loud? | WARN `presync_stuck` after **3** consecutive warning outcomes; never RED | the same | nothing |
| **Q7** | Notify on a sync warning from a GUI scheduled run? | **Yes**, one best-effort notification, warnings only | yes | TP5 |
| **Q8** | If Thunderbird will not quit (or is still writing) when the archive is due, read anyway or skip the night? | **Read anyway** (a read of a live Thunderbird is today's behaviour; a skipped night loses mail) | read anyway | nothing; reconsidered if L-C1 shows a bad truncated-message result |
| **Q9** | Allow `-thunderbird-sync` on a manual run, not only `schedule`? | **Yes**: it is the test of the start, the wait and the profile. It is **not** where the scheduled run's macOS permission is given (PC4): that comes from a scheduler-started run | yes | TP3 |
| **Q10** | A session in which no file changed at all: note or warning? | a **note** ("nothing new, or Thunderbird could not reach the server"); the escalation to a warning after 7 nights is deferred, not v1 (PC22) | note only | nothing |
| **Q11** | Refuse an `hourly` schedule with sync? | ~~**Yes**~~ → **No (REVERSED 2026-10-23, owner ruling)**: allowed on any cadence; the `detectRunning` defer makes a frequent sync safe | no | TP4 (reversed) |
| **Q12** | Pin the macOS Thunderbird signature (Mozilla's code-signing Team ID) as well as the bundle id? | **Not in v1**; record as residual | bundle id only | nothing |
| **Q13** | One profile per schedule, or sync several? | **One profile per schedule**; a second profile gets its own schedule | one | nothing |
| **Q14** | Accept that while the sync runs (up to about 25 minutes) the archive is "in use": a manual run or `verify` refuses, with a message that says it is syncing Thunderbird and until when (PC8)? | **Yes** (D1a) | yes | TP2 |
| **Q15** | Accept that "sync" means what Thunderbird itself fetches in the background (Inbox by default), not every folder? | **Yes**, with the README paragraph and the account-settings advice | yes | nothing |
| **Q16** **OPEN** | If the lab shows no way to close Thunderbird from a scheduler-started program (L-M8 and L-M9 both fail), what then? | **Do not ship the scheduled form** (TP4, TP5). Keep only the manual `-thunderbird-sync` (TP3), where you are present, or drop the feature. A sync that starts Thunderbird every night and cannot close it leaves a hidden mail client permanently open, which is worse than the feature being absent | the same: TP4 and TP5 stay unbuilt until a quit path is shown | TP3's scheduled use, TP4, TP5 |
| **Q17** **OPEN** | Accept that quitting Thunderbird runs whatever the account does on exit (empty Trash, clean up), before the archive reads, so mail you deleted since the last run may be removed before it is archived (PC9)? | **Yes**, with the install advisory (§4.2.4), the §6 text, and lab L-C6 to measure it. The alternative (archive first, then quit) would archive a Thunderbird that is still writing, which is the state the quit exists to avoid | yes, with the advisory | TP4 |

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
   for a Primary Password prompt, the 25-minute archive lock, the hourly schedule (now allowed, 2026-10-23).
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
   `app.Run` hook, the right shape?** It buys R5 and R18 for free but holds the archive for about 25
   minutes, makes `verify` and a manual run refuse meanwhile, and adds a field to `app.Options`
   that fault-isolation's and the extraction workspace's unbuilt changes also touch (D1a, Q14).
5. **Is process identity (executable path plus start time) sufficient and obtainable?** The macOS
   `ps -o lstart=` and `comm=` strings, Windows launcher re-execution (the child pid may not be
   the real process), the 2 s tolerance, and the 10-minute margin of the leftover window are my reconstruction;
   a mistake here either quits something that is not ours (the worst failure) or never cleans up
   a hidden Thunderbird (L-M4, L-W2, Q4).

*Review note: the five questions above were answered by the filed review. (1) macOS quit under
launchd is the viability gate, with no safe assumption either way: L-M8, L-M9, Q16. (2) the
hidden start, the lock symlink and modals stay lab items: L-M2, L-M3, L-C4. (3) file silence is
unproven and ranks second among the lab items: L-M5. (4) the hook inside the lock is accepted,
with the blocked parties told what they wait for (PC8). (5) path plus start time is not enough:
the `-profile` argument, the S4 snapshot and an elapsed-time start are required (PC6, PC11), and
v1 quits only what that proves and never quits a leftover (Q4).*

## 15. Review record: conditions folded, items still open

Filed review: `docs/review-thunderbird-presync-predesign.md` (verdict APPROVED WITH
CONDITIONS, against revision 1). Revision 2 folds every condition that needed no owner decision.

| Condition | Folded where | Status |
|---|---|---|
| PC1 the quit is delivered to the application, not the pid; residual stated | P2, R31, §4.7.1 quit row, §12, MA-554, MA-556 | folded |
| PC2 a leftover is announced on every run with no expiry; authority and announcement kept apart | §4.5 S3, §4.6 recovery table, §4.9.3, F27, MA-558 | folded |
| PC3 a derived profile must be listed by Thunderbird (`profiles.ini`) or named by the owner | §4.2.4, §4.3, §4.2.6, MA-562, MA-578 | folded |
| PC4 a manual run is not a test of the scheduled quit; first approval from a scheduler-started run | §4.7.1, §4.2.6, §8, Q9, MA-550, MA-570, MA-578 | folded |
| PC5 L-M8 and L-M9 are viability gates; no quit path means no scheduled form | header, TG3, §4.7.1, §9.4, TP3, §12 | folded; **Q16 OPEN** |
| PC6 identity needs the `-profile` argument and the S4 snapshot; successor and late-start scans | §4.4.3, S6, S7, S9, F25, MA-554 | folded |
| PC7 the owner-activity gate is in v1, before start and before quit | S4, S8, §4.7.1, F24, Q2, MA-579 | folded; **Q2 OPEN** (threshold) |
| PC8 who is blocked during the session is told what for; the verify window | §4.1, §4.9.2, MA-552, MA-559, TP2 | folded |
| PC9 exit actions and filters stated; install advisory; lab L-C6 | §4.2.4, §6, F26, L-C6, MA-579 | folded; **Q17 OPEN** (acceptance) |
| PC10 a boot identity replaces the hostname | §4.6 record, recovery table, §4.7.1, MA-565 | folded |
| PC11 start time from elapsed time in UTC; no wall-clock deadline | §4.4.3, §4.6, §4.7.1, §9.1, MA-554, MA-558 | folded |
| PC12 one state-directory convention | §4.6 | folded in this design; **OPEN-X1** (operator: reconcile with the workspace's name) |
| PC13 P7 reworded: an older CLI stops the backups | P7, §7, §12 | folded |
| PC14 session deadline cannot cut the quit; cancel grace fits launchd's exit timeout; time arithmetic | §4.5, §4.11, MA-556 | folded |
| PC15 `presync_not_recorded` judged on completed runs after install | §4.9.4, MA-551 | folded |
| PC16 forged archive-resident run record is display-only; P3's read-safety claim labelled conditional | §2 P3, §5.3 | folded |
| PC17 bundle id read with `plutil`, not a byte search | §4.7.1 | folded |
| PC18 two counters instead of one `streak` | §4.9.3, MA-561 | folded |
| PC19 MA ids re-allocated from MA-549; R/S/X to be re-checked at TP0 | header, §10 | folded; re-check at TP0 |
| PC20 hook and allowlist cited by function, not line | §4.1, §4.9.2 | folded |
| PC21 Windows and Linux not reviewed; own review before TP6/TP7 | header, §4.7.2, TP6, TP7 | folded |
| PC22 v1 scope cuts | §3, §4.2, §4.5, §4.9.3, TP8 | folded |

**OPEN items** (none may be closed by weakening R31 or R32): **Q2** (owner-activity threshold and
the skip-start rule), **Q4** (leftover: the review recommends announce-only for v1), **Q16** (what
to do if the lab finds no quit path from a scheduler-started program), **Q17** (accept the on-quit
exit actions running before the archive). **OPEN-X1 (operator):** the state-directory root shared
by this design, local-confidentiality and the extraction workspace's run record.

**Build status after this revision:** reviewed, conditions folded, macOS v1. TP3 additionally
requires L-M8 or L-M9 to show a working quit path (Q16). Nothing is built.
