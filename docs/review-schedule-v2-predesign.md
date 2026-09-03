# Pre-code design review — repeat archiving v2 (schedule any job, Windows-safe, status)

**Design reviewed:** `docs/design-schedule-v2.md` revision 1 (2026-09-02).
**Procedure:** as for `review-incremental-completeness-predesign.md` (same workflow run).
**40 findings raised · 38 survived · 2 refuted.** Rescue: 11 of 12 surviving blocker/high
findings DISSOLVED, 1 PARTIAL (routed to the sibling design as B3/B4); one hidden
blocker surfaced.

**Verdict: GO_WITH_CONDITIONS.** Conditions SC1–SC16 are must-fix-before-code and are folded
into design revision 2. Per SC1 the work is split into slices that each owe their own gate.

## Hidden blocker (rescue pass)

`status` is pull-only. The personas the feature exists for (the unattended laptop, the
double-click user) do not go and ask. A backup that silently stops is *discoverable*, not
*disclosed*. Conceded as a non-goal for this revision with two mitigations that need no new
standing dependency: the GUI shows backup health on every launch, and on Unix a failed
scheduled run prints its one-line failure to stderr so cron's mail (MAILTO) carries it.
An OS-notification channel is recorded as backlog, not promised.

## Findings → conditions

| ID(s) | Lens | Section | Claimed failure (verified) | Verdict | Condition |
|---|---|---|---|---|---|
| F1 (lens 2) | 2 | §3.5, §4 | Seven independent units and a grab-bag of GUI flow fixes bundled into one design | PARTIAL (high) | **SC1** |
| F2 (lens 1), F3 (lens 5), L5-5 | 1, 5 | §3.1 | `-- serve` (or search/status) is accepted and installs a never-terminating job | CONFIRMED (high) | **SC2** |
| L5-7, F7 (lens 5) | 5 | P1 | The flat form and the `--` form can both carry `-out`/`-mode` with no precedence | CONFIRMED (medium) | **SC3** |
| L6-1, F1 (lens 6) | 5, 6 | §3.2 | Batch quoting ignores cmd.exe metacharacters (`&` is legal in NTFS names) | CONFIRMED (high) | **SC4** |
| L3-1, F6 (lens 1), F6 (lens 8), F5 (lens 5), F3 (lens 7) | 3, 4, 5, 7, 8 | §3.4 | Nothing links a schedule to its archive; status can only probe the default name; on Windows it stats the wrapper, not the real exe | CONFIRMED (high) | **SC5** |
| F4 (lens 7), F1 (lens 10), F5 (lens 1) | 1, 7, 10 | P5, §3.4 | No posture for "no last run"; a killed run leaves the prior "ok"; "every run leaves a record" is false under SIGKILL | CONFIRMED (high) | **SC6** |
| F4 (lens 5), F7 (lens 8) | 5, 8 | §3.5 | GUI and CLI both default to one name; a second archive silently overwrites the first's schedule | CONFIRMED (medium) | **SC7** |
| L3-2, F3 (lens 9), F5 (lens 8) | 3, 4, 8, 9 | P7, §3.4 | Exe moved/upgraded → job silently dead; no rollback story | CONFIRMED (high/medium) | **SC8** |
| L3-3, F2 (lens 9), F8 (lens 7) | 3, 7, 9 | P3, §3.5 | Logs append forever inside the (synced) archive | CONFIRMED (medium) | **SC9** |
| L6-4, F8 (lens 6), L3-7 | 3, 6 | §3.1, §3.3 | Secret-file mode check only at 02:00; FIFO/symlink accepted; secret expiry has no story | CONFIRMED/PARTIAL | **SC10** |
| L3-6, L3-4 | 3, 4 | §3.5 | Job file lacks `auto` (new stores never picked up) and a version field | CONFIRMED (medium/low) | **SC11** |
| F8 (lens 1), L3-5, F8 (lens 10) | 1, 3, 10 | P3, P7 | The GUI's direct `/TR` is unbounded; `-name` is unbounded | CONFIRMED/PARTIAL | **SC12** |
| L3-8 | 3 | §3.2 | Install order unstated (task before wrapper leaves a task pointing at nothing) | CONFIRMED (low) | **SC13** |
| F4 (lens 9) | 9 | §3.4 | "No crontab" and "no cron at all" collapse to "not installed" | PARTIAL (low) | **SC14** |
| L5-8, F6 (lens 5) | 5 | §3.4 | `status` exit code undefined | CONFIRMED (low) | **SC15** |
| F2 (lens 6) | 6 | OMISSION | No inter-process lock on `-out` | CONFIRMED (high) | B9 (sibling) |
| F1 (lens 8) | 8 | sibling §3.1 | Old binary downgrades the manifest | PARTIAL (high) | B3/B4 (sibling) |
| F2 (lens 7) | 7 | §3.5 | Headless job-file failure has no sink independent of `out` | PARTIAL (medium) | **SC11** |
| F7 (lens 2) | 2 | §4 | R18 restates R14 | PARTIAL (low) | **SC16** |
| F5, F7 (lens 10) | 10 | §6 | Wizard flows and real-host status detection labelled "proven" | PARTIAL (low) | **SC16** |
| (2 refuted) | — | — | recorded in the journal with refuting quotes | REFUTED | — |

## Conditions (all folded into revision 2)

- **SC1 — Split.** (a) job parsing + `--` grammar + verb whitelist + Windows wrapper; (b)
  `-client-secret-file` (owes the adversarial pass); (c) last-run record + `status`; (d)
  GUI headless job + schedule step + health view (owes the friction review); (e) the GUI
  flow fixes as their own friction-owned change with their own catalog rows. Separate
  commits; each names its gate.
- **SC2 — Verb whitelist.** A `--` job may be the default export, `graph`, or `reindex`;
  `serve`/`search`/`status`/`schedule` are refused at schedule time naming why.
- **SC3 — One form per invocation.** When `--` is present, any schedule-level export flag
  is refused naming it; `-out`, the log path and all validation derive from the job.
- **SC4 — Batch quoting.** The wrapper quotes every token unconditionally and doubles `%`;
  delayed expansion is never enabled; MA-73 covers `& | < > ^ ( ) !`.
- **SC5 — Archive-local descriptor.** Install writes `<out>/.mailarchive-schedule.json`
  {version, name, interval, at, exe, wrapper, job, installed_at, host}; remove deletes it;
  `status` and the GUI read it (name-correct, host-aware) and stat the *real* exe.
  `schedule -out DIR -remove` works without `-name`.
- **SC6 — Fail-closed posture.** The last-run record is written at START (`running`, pid)
  before any early return and finalized at the end. `status`: installed but no record →
  WARN "never recorded a run"; unreadable record → WARN naming it; `running` with a dead
  pid or older than one interval → RED "previous run did not complete"; no manifest and no
  schedule → refusal naming the missing archive. P5 reads "every run that begins is
  recorded (best-effort under power loss)".
- **SC7 — One schedule per archive.** The default name is `mailarchive-<8 hex of the
  absolute -out>` on both surfaces; `-name` still overrides; `DefaultName` remains the
  managed-marker prefix.
- **SC8 — Exe identity.** The descriptor records the installed exe path + size + mtime;
  `status`/GUI WARN when the running binary differs ("re-run schedule -install") and RED
  when it is missing; the GUI warns before self-scheduling from a Downloads/Temp path.
  README: upgrading → re-install; rolling back → `schedule -remove` first.
- **SC9 — Bounded logs.** A `-log FILE` flag (export and graph) writes the operator log to
  FILE with size-capped rotation (8 MB, one `.1`), instead of stderr; scheduled entries use
  it on every OS. Crash output (not under the product's control) goes to cron mail on
  Linux, `<name>.stderr.log` under launchd and the Windows wrapper.
- **SC10 — Secret file.** Schedule-time validation calls the same reader as run time:
  regular file only (Lstat; no FIFO/device), mode `0o077` clear on Unix, non-empty,
  bounded to 4 KB. README/graph-app-setup carry the secret-expiry calendar note; `status`
  maps an authentication failure to a "rotate the secret" remedy line.
- **SC11 — Job file.** Carries `version` and `auto` (re-discovery at run time); the last-run
  record carries `version`; both follow the manifest's additive-only rule. A job-file
  load failure is written to `<config>/mailarchive/<name>.jobfail.log` and exits 1.
- **SC12 — Length bounds.** `-name` is sanitized and capped at 40 characters; Install
  measures the `/TR` string and uses the wrapper whenever it would exceed 261 (the GUI
  job included, accepting the console flash in that case); P3 states the bound.
- **SC13 — Install order.** Wrapper written (and fsynced) before the task is created.
- **SC14 — Three-state detection.** `Installed` returns installed / not installed /
  scheduler unavailable, and `status` names an unavailable scheduler.
- **SC15 — Exit code.** `status` exits 0 whenever it reports; posture is data on stdout
  (X8). A machine-readable health signal is tracked as ux-contract condition C5.
- **SC16 — Honesty.** R18 is narrowed to the new property (every scheduled run is recorded;
  `status` reports completeness, last run and schedule posture); the "carries the job /
  any host path" clauses extend R14. Wizard dialog flows and real-host detection are
  lab-pending rows, not "proven".
