# Thunderbird pre-sync — the MailArchive Desktop surface (TP-D)

Status: REVIEWED 2026-10-24 — verdict GO-WITH-CHANGES (6-agent pre-code review).
**The "Pre-code review corrections (2026-10-24)" section at the END is AUTHORITATIVE
where it differs from §§1-9 below; the originals are kept per the correction rule.**
Owner, 2026-10-24: the
pre-sync must be **created, shown, changed and (built to) expand** in MailArchive
Desktop — and in the wizard and CLI — before v1.4.0 ships.

## 0. What this reverses, and why it is here

`design-thunderbird-presync.md` §4.2.6 states: "The desktop dashboard has no
Thunderbird surface (non-goal)." **The owner reversed that on 2026-10-24** (the
reason: many people archive Thunderbird mail on a schedule and never touch the
command line, so the one surface they live in must offer it). The original
sentence stands in §4.2.6 with a dated note pointing here; this doc is the new
design for the Desktop surface. The wizard (§4.2.6) and CLI (§4.2.3/§4.2.4) parts
are already built (MA-574, MA-550/569/580); this doc is **only** the Desktop.

## 1. The gap (what the Desktop can and cannot do today)

The Desktop already, for a LOCAL archive that *has* a run-job schedule, DISPLAYS
it ("a run-job backup", `dashboard.go` KindRunJob), and REPAIRS, RUNS and REMOVES
it (`repairRunJob`, `runBackup`, `scheduleRemove`). What it cannot do:

- **Create** one. `scheduleInstall` builds a Microsoft-365 `-capture` spec and
  refuses unless M365 is set up (`m365Ready`), and `ShowInstall` is false for a
  `LocalOnly` archive — so a local archive shows only "the weekly backup … is off"
  (`ShowBackupOff`), pointing at the wizard/CLI.
- **Toggle the pre-sync.** There is no control for it.

So the Desktop work is: a local-archive **create** path, a **pre-sync toggle**, a
**display** of the pre-sync state, and **edit** (change cadence / toggle) — reusing
the existing display/repair/run/remove machinery.

## 2. The create mechanism — reconstruct from LastRun.Job (no picker, no new data)

A local re-archive schedule needs the SOURCE input paths. The Desktop (a loopback
browser dashboard) cannot run a native folder picker for a server-side path, and
the archive does not persist its inputs anywhere else. But `state.LastRun.Job`
already records the archive's **canonical export flags** (`-out … -input … / -auto
…`, `lastrun.go:66`) — exactly what `status` parses to print its "keep it current"
command (`health.scheduleCommand`). The Desktop reconstructs the run-job from that:

1. Read `LastRun.Job` for the current archive (`out`). If absent (older record, or
   a Graph/M365 archive), there is nothing to reconstruct → the create control is
   **not shown**; the `ShowBackupOff` message still points at the wizard. This is
   the honest fallback; the Desktop never guesses inputs.
2. Parse those flags into a `job.Job` (reuse the schedule package's existing flag
   parsing — the same canonical job `status` and the installed scheduler entry are
   built from, MA-569). The job carries the archive's real inputs/auto/out/mode/
   since/copy-first/follow-links/raw.
3. Add the cadence + time from the Weekly-backup form, and — when the owner ticks
   the toggle — `ThunderbirdSync{}` (no profile: derived + membership-checked at
   run time, §4.3/PC3), exactly as the wizard's `jobFor` does (MA-574).
4. `job.Write` the file (owner-only 0600) + install a **run-job** spec (the same
   `installSpec` path the wizard uses), NOT a `-capture` spec.

The run itself already honours the setting (MA-580: `RunHeadless` → `optsForJob` →
`presyncjob.SetHook`). So create reuses two proven paths (the wizard's job write +
the run-job install) and the already-wired run.

## 3. The toggle — shown only for a Thunderbird-sourced archive

The pre-sync checkbox appears in the Weekly-backup form only when (a) the platform
has an adapter (macOS/Windows) and (b) the archive's source is a Thunderbird store
— determined from the reconstructed job's inputs via `thunderbird.FindProfileDir`
(the wizard's `presyncOfferable` rule, MA-574), never shown for an M365 capture or
a non-Thunderbird local archive. Default OFF (R30). Hover text draws from
`presync.SyncNotice` (MA-567 coherence) + the running-Thunderbird defer (R31).

## 4. Display

For a local run-job schedule, the card names the cadence/time (already) and, when
the job carries `ThunderbirdSync`, adds one line: "Thunderbird is synced before
each run (if it is already open, it is left alone)." The last run's pre-sync
result already surfaces through the health view (MA-561); the card does not
duplicate it.

## 5. Edit / change

Changing cadence/time or flipping the toggle rewrites the job file and reinstalls
the same-named run-job (an idempotent re-create — the install path already
replaces a same-named entry). "Remove" is unchanged. No separate edit endpoint:
Install/update IS the edit for a run-job the Desktop created, with the toggle
reflecting the current job's state on load.

## 6. Expandable (built to grow, not built now)

v1.4.0 ships ONE local schedule per archive (the existing one-descriptor-per-
archive model). The design keeps the door open for multiple named schedules later
(the deferred scheduling-flexibility work): the create path already goes through
named job files (`job.PathFor(name)`), so a future multi-schedule UI adds rows
keyed by name without changing the record or the run. Nothing here hard-codes
"exactly one".

## 7. Security / invariants (the lenses to review)

- **R30/R31/R32** hold unchanged: the toggle defaults off; the run (not this UI)
  does the sync; a running Thunderbird is never started/quit; the sync never gates
  the archive. This UI only writes a job file + installs a schedule.
- **LastRun.Job trust.** It is owner-written at 0600 and already trusted by
  `status` to build a command. The Desktop parses it into a `job.Job` whose fields
  are then re-serialised by `job.Write` and re-validated by `job.Read` at run time
  (symlink/owner/size guards, R12). The Desktop must NOT exec `LastRun.Job`
  verbatim; it reconstructs a `job.Job` and installs via the normal spec, whose
  program is `schedule.ProgramForSchedule` (never a path from the record) with the
  volatile-dir refusal (§4.6.1).
- **CSRF / loopback.** The create/edit POST reuses `server.GuardLocalPOST(r,
  d.csrf)` — same-origin, loopback, tokened; the positive twin installs, the
  untokened/cross-origin/foreign-Host twin is 403 with nothing installed.
- **No M365 bypass.** The local-create path runs ONLY for a `LocalOnly` archive
  with a reconstructable local job; an M365 archive still takes the `-capture`
  path and its `m365Ready` refusal — the two are chosen by the archive, never by
  the request.
- **No new secret, no new roaming state.** The job file is the only thing written,
  where job files already live.
- **MA-621 / hygiene.** No new per-user-state literal; the comment/token hygiene
  walk stays green.

## 8. Catalog (rows to define as the slices land)

- MA-581 (U): the Desktop creates a local run-job schedule for a `LocalOnly`
  archive by reconstructing `job.Job` from `LastRun.Job`, installing a run-job
  (not a capture); absent/undeterminable `LastRun.Job` → no create control, the
  wizard pointer stands; CSRF positive + refusal twins.
- MA-582 (U): the Desktop pre-sync toggle — shown only for a Thunderbird-sourced
  local archive on an adapter platform (default off); a ticked toggle writes
  `job.ThunderbirdSync{}` (no profile) into the created/edited job; unticking
  rewrites without it; hover draws from `presync.SyncNotice`.
- MA-583 (U): display + edit — the card shows the pre-sync line when the job
  carries it, and Install/update rewrites+reinstalls the same-named run-job with
  the current form (cadence/time/toggle).

## 9. Non-goals (this release)

Multiple named schedules per archive; a Desktop folder-picker for new sources (the
wizard owns first-time setup); scheduling an archive the Desktop has no last-run
record for; any change to the M365 capture path.

## Pre-code review corrections (2026-10-24) — AUTHORITATIVE

A 6-agent pre-code review (verdict GO-WITH-CHANGES) found §§1-9 rest on mechanisms
that do not exist and a false trust premise. These corrections win where they differ
from the sections above (originals kept per the correction-on-the-record rule).

1. **No flags→job parser exists (was §2).** Build `internal/job.FromCanonicalArgs(args
   []string) (Job, bool)` accepting ONLY the `app.canonicalJob` subset — `-out`,
   `-mode`∈{incremental,full}, `-auto`, repeated `-input`, `-copy-first`, repeated
   `-follow-links`, `-raw` — FAIL-CLOSED (ok=false) on a missing/empty `-out`, a
   malformed value, or ANY other token (so `-since`,`-outlook`,`-manifest`,`-log`,
   `-graph`,`-verify` all make it refuse). Do NOT hoist cmd/mailarchive's parseJob.
   One mapper, called by Desktop, wizard and the CLI schedule path.
2. **LastRun.Job is UNTRUSTED (was §7).** It lives in the archive dir, writable by
   anyone who can write that dir (lastrun.go:257) — not owner-0600; `status` only
   RENDERS a paste command, never installs. So: (a) read it with a
   `job.readSecure`-class guard (refuse symlink/non-regular, bound size) before using
   it to install; (b) PIN `-out` to `cfg.effectiveOut()`, never the record's `-out`;
   (c) gate the create branch on `cfg.localOnly()` (owner-side state); (d) only the
   reconstructed, whitelisted job the Desktop writes to the owner's 0600 config is
   trusted. OWES the adversarial pass — not self-certified.
3. **-since/-outlook absent from the record (was §2).** canonicalJob emits neither.
   Show create only when the reconstructed `-input` paths EXIST and are NOT under
   `<out>/_outlook-pst`; else the wizard pointer stands (as for Graph/nil).
4. **Toggle uses the MANIFEST, not LastRun.Job (was §3).** No `-input` is recorded for
   an `-auto` archive, so detect the Thunderbird source from the archive manifest's
   local `path:` store roots (archiveKind/state.Load) via `thunderbird.FindProfileDir`;
   use LastRun.Job only for the job SHAPE. Prove with a manifest fixture (TB→shown,
   M365/non-TB→hidden), incl. the -auto case.
5. **Explicit create/edit handler (was §4/§5).** New handler, NOT via repairRunJob:
   `job.Write`s from the form (ThunderbirdSync presence = toggle) + installs a run-job
   spec with the chosen interval/at. Reuse the wizard via thin EXPORTED wrappers
   (`wizard.InstallRunJob(j, iv, at, exe)` + a shared job-builder mirroring jobFor) —
   do NOT copy installSpec/jobFor (MA-641 single-install-path). Display + toggle-on-load
   read the job FILE (`job.Read`→ThunderbirdSync), not the descriptor. repairRunJob
   stays repair-only.
6. **"also cli" is a real deliverable (new, MA-584).** `status`/`schedule` preview shows
   "this schedule syncs Thunderbird first," reading `-thunderbird-sync` from the argv for
   a flat KindExport schedule AND from the job FILE for a run-job. Fix the stale `-h`
   text (job.go:66 "macOS only"→"macOS and Windows") and comments (job.go:241-242,
   main.go:256).
7. **Handler hygiene (new).** Call `schedule.CheckKind` before installing (refuse a kind
   collision with the wizard's message); guard `r.Method==POST && server.GuardLocalPOST`
   with a NAMED 403 body; three `assure.Refused`+NoSideEffect twins (untokened/
   cross-origin/foreign-Host→403; M365/mixed→capture m365Ready refusal; LocalOnly-but-
   undeterminable→named refusal); positive twin first (run-job installed, Args `-job
   <path>`, job.Read round-trips ThunderbirdSync).
8. **UX (corrects §3/§4).** SyncNotice + the running-Thunderbird defer as VISIBLE inline
   text under the checkbox (X16 — not hover-only); local-path copy drops "capture"/
   "Microsoft sign-in", retitles "Scheduled backup"; the off fallback states the real
   reason ("no record of how this archive was built; open the wizard"). Exactly one of:
   `ShowLocalCreate = LocalOnly && !HasSchedule && reconstructable && adapterPlatform`;
   `ShowBackupOff = LocalOnly && !HasSchedule && !ShowLocalCreate`; has-schedule.
9. **Expandability is job-file-only (corrects §6).** Only job files are name-keyed; the
   descriptor, name derivation, corroboration and collision guard are one-per-archive.
   Thread a `name` param (default `DefaultNameFor(out)`) through the shared create helper
   now, so future multi-schedule adds callers, not a signature change.
10. **Catalog rows cite real tests (corrects §8).** MA-581 (create writes a job + run-job
    install, never -capture; refusal twins), MA-582 (toggle from manifest TB source incl.
    -auto), MA-583 (display/edit read+write the job FILE), MA-584 (CLI display). Each
    `Proven by` a real `_test.go` with prove-fail→pass; route all job-file paths through
    internal/job (MA-621 stays enforcer); add no exec site.
