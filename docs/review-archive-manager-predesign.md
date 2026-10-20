# Pre-code design review — the archive manager

**Design under review:** `docs/design-archive-manager.md` rev 1 (2026-10-19),
sha256 `248b66398f572dd8fe543978cbef92c9b13b3d325faaba2191ac3fcffa9e1312`.
**Repository:** `5ef77250816c2c9e2cc4459a4e7f1154d8c80968` (branch `release-n-merge`).
**Toolchain:** go1.26.0 darwin/arm64. **Review date:** 2026-10-19.
**Process:** `../assurance-kit/process/design-review.md` — the 10-lens battery, a
refutation-default skeptic, and a separate rescue pass. **Register:** a finding
(pre-code adversarial design review). Static, read-only; **no build or test was
run** (a design-phase "test" is a structured interrogation). The only commands
were read-only: `shasum -a 256` (digest), `git rev-parse` (SHA), and greps.

## Verdict

**GO_WITH_CONDITIONS — adopt the finders' slice into three sequential pre-code gates.**

- **Slice 1** (unify the pointer + the registry index + the active-archive
  pointer) is **GO_WITH_CONDITIONS now**; it carries none of the unproven
  mechanism and fixes all three §1 friction points.
- **Slice 2** (disconnect / hard read-only seal) and **Slice 3** (source-loss)
  **return as their own pre-code mini-reviews** — they hold almost all the
  unproven mechanism and every open question.

The registry concept is sound and is the right spine; **no finding requires
re-designing it.** Both apparent blockers dissolved to named, modest mechanism
changes (rescue pass). Clearing them surfaced the **true blocker the ten lenses
missed** (condition **C2**, below), which is the one decision that gates the
whole design and is surfaced to the OPERATOR.

The dependency graph is acyclic: **Slice 1 → (Slice 2, Slice 3)**; Slice 3's
"Disconnect from the source-loss banner" is a soft dependency on Slice 2,
matching §5.7 ("source-loss and Disconnect share machinery").

## Reviewers (independent passes)

| Role | Lenses | Identity |
|---|---|---|
| Finder A | 1 Purpose/threat-fit, 2 Focus/proportionality | subagent (read-only) |
| Finder B | 3 Durability, 7 Failure domains | subagent (read-only) |
| Finder C | 4 Usability/friction, 5 Integration/seams | subagent (read-only) |
| Finder D | 6 Security/adversarial, 9 Dependency/cost | subagent (read-only) |
| Finder E | 8 Migration/coexistence, 10 Honesty-of-claims | subagent (read-only) |
| Skeptic + synthesis owner | refutation-default; firsthand re-check of the PM2 binding and the write chokepoint | Claude (Opus 4.8) |
| Rescue (separate from finders + skeptic) | save the design; dissolve or refute the blockers | subagent (read-only) |

The skeptic/synthesis owner firsthand-verified the two load-bearing facts the
whole verdict turns on, rather than trusting the finders: (1) the PM2 binding in
`internal/schedule/bound.go` re-derives the entry name from the folder's own live
descriptor, so the registry grants no scheduler authority; (2) every in-place
archive write funnels through one chokepoint, `lockfile.AcquireAs(filepath.Join(
out, lockfile.Name), verb)`, at seven production sites (`run.go:379`/`:784`,
`graph.go:101`, `reindex.go:41`/`:262`, `extract.go:124`, `verify.go:137`).

## Findings

Severity: **BLOCKER** (must resolve/re-design before any code) · **CONDITION**
(a named modest change, must-fix-before-code, becomes a tracked edit to the
design) · **MINOR** / **BACKLOG** (operability; ships with the feature).
A finding is CONFIRMED when the design AS WRITTEN permits the failure; REFUTED
when a section already handles it (quoted).

| ID | Lens | Design §/OMISSION | Claimed failure | Verdict | Condition |
|---|---|---|---|---|---|
| B1 (F-A1,F-D1,F-E5) | 1/6/10 | §5.3, PM3, R35, OQ2 | "Hard read-only" is delivered by `ArchiveOptions{Other:true}`, a per-open flag on the copyexport **source reader** (`copyexport_archive.go:664 Immutable: a.other`), which gates **none** of the 7 write sites. A disconnected archive is still written by a scheduled capture, a manual `-out` incremental, or reindex. R35 "every write refused" is false as written. | CONFIRMED (design concedes via §5.3 to-confirm + PM3 "conditional") | **S2-C1** |
| B1′ (F-B1,F-E3) | 7/8 | §9, §5.1, R35 | The `disconnected` seal lives **only** in `known-archives.json`. §9's "rebuilt by re-import" + §5.1 "import only adds entries" re-marks a lost/rolled-back registry's archives `connected` → writable: **fail-OPEN** on a safety invariant; §9 claims the opposite. A disconnected non-current archive whose descriptor+job-file were deleted is unrecoverable by re-import. | CONFIRMED | **S2-C1** |
| B2 (F-C1,F-E1,F-A4) | 4/5/8 | §1, §5.2, §8, S60 | The registry holds **many** `connected` archives, but every surface keys off a single `effectiveOut()` + a global cadence + a single `Settings.BackupFor`, and **no §5 op writes `out` or selects which archive is current**. "Switch between … more than one archive" (§1) ships with no switch; scheduling a 2nd archive reopens the §1 drift. | CONFIRMED | **S1-C1**, **C2** |
| B2′ (F-C2) | 5 | §5.5 | "schedule.Install … unchanged; the manager only chooses the archive and cadence" is false: cadence is global (`schedule.go:75` reads `cfg.settings()`), and `BackupFor` tracks one task. A 2nd archive's cadence clobbers the first's record. | CONFIRMED (depends on B2) | **S1-C1** |
| C-RESUME (F-A5,F-B3,F-C4,F-E6/OQ1) | 3/4/7/10 | §4, §5.6, OQ1 | Disconnect deletes the run-job job file and stashes only `{interval,at}`; the run-job's `Inputs/Mode/Auto/Since/FollowLinks` (`job.go:27`) are destroyed, so "Connect restores the cadence" cannot rebuild a run-job. §5.6 asserts resume is "in scope." | CONFIRMED | **S2-C3** |
| C-NAME (F-D2) | 6 | §6, R34, OQ1 | PM2 holds only because the entry name is re-derived from the live descriptor; the invariant "the registry stores/consumes no entry name or bind verdict" is **never stated**, and OQ1 proposes stashing a full `Spec`. If adopted and later consumed by Remove/Repair, a planted name skips `bound.go`. | CONFIRMED (latent; PM2 holds *conditional on this*) | **S1-C4** |
| C-READ (F-D3) | 6 | §4, §6 | "owner-only 0600" is asserted but the **read path and dir perms are unspecified**: `WriteFileAtomic0600` does `MkdirAll(dir,0700)` on create only (never tightens a pre-existing loose dir), and nothing applies `ReadSecureFile`'s symlink-refusal/mode check to the registry read. A symlinked path or a group-writable config dir lets a non-owner plant entries / flip `disconnected`→`connected`. §6 names these but **defers** them. | CONFIRMED | **S1-C4** |
| C-SRCLOSS (F-B7,F-C8) | 3/7/4 | §5.7 | Transient-vs-permanent "judged by duration/run-count" and the "since DATE / after N runs" banner need **persistent streak state** (first-failed-at + consecutive count) that no record holds (`.mailarchive-lastrun.json` is overwritten; health is stateless). And the `reachable` signal is **archive-side** (`health.go:339`), not source-side; a local source has no unreachable signal at all. The feature's core judgement is uncomputable as written. | CONFIRMED | **S3-C1** |
| C-SEAM (F-C5,F-E4,F-E8) | 5/8/10 | §5.3 | "`RemoveIfInstalled` … removes the descriptor **and, for a run-job, its job file**" is **false**: `Remove`→`removeOn` deletes only the OS entry + archive descriptors; the job file is a separate desktop-layer `os.Remove` (`desktop/schedule.go:232`). And disconnecting a **non-current** archive needs run-job-vs-capture kind-routing today keyed to `effectiveOut()`. | CONFIRMED | **S2-C3** |
| C-CRASH (F-B2) | 3 | §9, §5.3 | §9 mislabels the crash residual: a crash after the scheduler entry is removed but before `disconnected` is written leaves a **connected archive whose backups have silently stopped** (not "connected with its schedule, retryable"); no op auto-repairs it. | CONFIRMED | **S2-C4** |
| C-OUT (F-E2) | 8 | §5.3 | Disconnecting the archive `out` points at never clears/repoints `out`; `offer()` stays gated off (`localarchive.go:182`), so the Desktop pins to a sealed, read-only archive with no surfaced replacement. | CONFIRMED | **S2-C4** |
| C-VER (F-B4) | 3 | §4 (OMISSION) | `known-archives.json` has **no `version` field** and no unknown-key / refuse-newer policy — unlike `Settings.Version`+`keepUnknownSettings` and `job.Read`. A downgrade across a schema change has undefined behaviour (drop keys, or discard-as-corrupt → B1′ seal loss). | CONFIRMED | **S1-C3** |
| C-HEAL (F-B5) | 3 | §4 | Self-heal uses `validArchiveFolder()`, which is **Stat-only**; a dehydrated cloud placeholder Stats successfully while offline, so it is wrongly marked **available** — contradicting §4's own "offline cloud placeholder … marked unavailable" example. | CONFIRMED | **S1-C5** |
| C-FORGET (F-B6) | 3 | §4 | §4 claims the file "only shrinks by owner action," but **no §5 op removes an entry**; permanently-unavailable entries accumulate forever with no "Forget" path — and §5.3 invites creating them ("delete the folder yourself"). (Not the archive-deletion non-goal: forgetting a pointer ≠ deleting data.) | CONFIRMED | **S1-C5** |
| C-HEADLESS (F-C7) | 4 | §3 | A cold/headless operator has **no surface**: the wizard refuses with no DISPLAY (`wizard.go:161`), the Desktop is a loopback browser panel, and the design adds **no CLI verb**. The registry — and connect/disconnect/repair — is unreachable exactly where stale cron jobs fester. A CLI verb honours R33 and was not considered. | CONFIRMED | **BACKLOG-1** |
| C-WIZUX (F-C6, OQ4) | 4 | §3, §4, OQ4 | The primary entry ceremony is undesigned: the wizard first step is a flat native `zenity.List` that cannot render an archive list or greyed/unavailable entries; OQ4 concedes the MA-651 reconciliation is unresolved, yet §3 lists the wizard as a shipping entry point. | CONFIRMED | **BACKLOG-2** |
| C-LEGIBLE (F-C9) | 4 | §5.7 | Source-loss surfaces only in a Desktop banner; the macOS `.app` double-click opens only the wizard, whose health covers one archive. A managed non-current archive fails nightly with no legible surface for that user. | CONFIRMED | **S3-C2** |
| C-PM1 (F-A3,F-E7) | 1/10 | PM1 vs §6/§9 | PM1 "one source of truth" over-claims and contradicts §6/§9 ("the registry is an index, not a system of record"); the design gives no convergence path by which surfaces switch to reading the registry, and the installed scheduler structurally cannot (PM2). PM1 is unreachable as designed, not merely "not-yet-true." | CONFIRMED | **C2** |
| M-SYMLINK (F-D4) | 6 | §6 | TOCTOU/symlink on the entry `path`: `validArchiveFolder()` Stats (follows symlinks), check-then-use. Benign **by construction** (every op re-derives the bound name from the descriptor at the acted path), but the reduction is unstated. | CONFIRMED (minor) | state in C2 |
| M-AUTOPAUSE (F-D5, OQ3) | 9 | §5.7 | "auto-pause after ~30 days" is the one source-loss action that mutates a scheduler entry unattended; the design never says where the clock/actor runs (headless self-removal? Desktop-only?). | CONFIRMED (minor) | **S3-C2** |
| M-FSYNC (F-B8) | 3 | §4/§5.3 | `WriteFileAtomic0600` fsyncs the temp file but **not the parent dir** (`securefile.go`); §5.3's "stash first, then remove the scheduler entry" newly relies on the stash rename being durable. Low blast radius. | CONFIRMED (minor) | note in S2-C3 |

### Refuted / sound (recorded with refuting evidence)

- **PM2 — "a registry entry never grants a scheduler action" HOLDS.** Firsthand +
  finders A/C/D: the registry contributes only a folder `path`; Disconnect builds
  the Spec via `backupSpecFor(path)` → `DefaultNameFor(path)` =
  `mailarchive-<hash(path)>`, and CLI `-remove`/Repair derive the name via
  `RecordedBackupName`/`NameBound`, which enforce `bound.go`. A fully
  attacker-controlled registry can at most act on *that path's own* derived
  entry. The sole residual is the pre-existing conceded launchd `-out`-only
  (UJ-6) limit, identical to `-out X -remove` today. **Conditional on C-NAME.**
- **Q20 world-writable-program-dir guard not weakened** (finder D): Install still
  routes through `ProgramForSchedule(true)`→`guardLocation`; removal/disconnect
  never executes the program.
- **Crash-ordering *direction* is safe** (A/B/D/E): writing `disconnected` after
  the entry is gone never yields "disconnected archive + live schedule writing to
  a read-only folder." (Its residual *labeling* is wrong — see C-CRASH.)
- **§8 import is monotone/flag-day-free** (C/E): §5.1/§8 forbid Install/Remove
  during import; a running schedule is untouched.
- **Proposed IDs collision-free, maxima accurate, BUILD STATUS honest** (E):
  current maxima R33 / MA-652 / S59; R34–R35, MA-653–659, S60–S63 undefined;
  PM2/PM4 carry honest inline conditions; "proposed/pre-code" is truthful.
- **Import-cycle claim accurate** (C): `internal/desktop` imports `internal/wizard`
  and not the reverse, so a leaf below both is sound. (But the reuse helpers
  `archiveKind`/`validArchiveFolder` live *in* `internal/desktop` — see C2's
  seam note: they must be relocated to the leaf, not "reused as drawn".)

## Rescue outcomes (per blocker)

- **B1 read-only enforcement — DISSOLVED-BY** an archive-local
  `.mailarchive-disconnected` sentinel inside `out` (the atomic temp+rename
  pattern `state/lastrun.go:202` already uses for `BACKUP-NEEDS-ATTENTION.txt`),
  written at Disconnect and checked by a shared guard at the `AcquireAs` verb
  boundary; the registry `state` becomes **display-only**. This also dissolves
  **B1′** (the sentinel lives with the archive, so re-import re-learns the seal;
  it fails closed on registry loss). Sealed verb set **{export, graph, reindex}**;
  **{verify, extract, serve} stay readable** — `extract` writes to `-dest`
  *outside* the archive (overlap refusal `extract.go:143`), so a parked archive
  stays extractable. Flag folded into S2-C1: `reindex` is also the §5.4 Repair
  primitive, so Repair must re-connect first.
- **B2 active-archive — PARTIAL.** The active-pointer write lands cleanly: the
  sole `out` writer, `saveSettings` (`dashboard.go:356`), is a locked field-scoped
  read-modify-write (`UpdateSettings`, XC34), so it does not race the Settings
  POST. What remains (→ S1-C1): the write must route through the XC22/MA-523
  weekly-backup keep/remove reconciliation (`BackupFor`), not a bare write, or a
  switch silently repoints/orphans the scheduled capture (MA-279).
- **C-RESUME — DISSOLVED-BY** making Disconnect ≠ Remove: remove the OS entry but
  **retain the job file** as the full resume record (`job.Job` already holds the
  complete payload); job-file deletion stays for Remove only.
- **C-SEAM — DISSOLVED-BY** driving Disconnect from the already path-parameterized
  primitives `BuildKindEvidence(path)`/`removeTaskAt(p)`/`backupSpecFor(p)` keyed
  to the entry's own path, not `effectiveOut()`.
- **C-SRCLOSS — PARTIAL.** The streak dissolves with two registry fields
  (`firstFailedAt`, `consecutiveFailures`). The per-run **local-source-unreachable
  signal** is genuinely new (a new `ErrKind*` on the run path, distinct from the
  archive-side `reachable`/PM4 guard; M365 already has
  `ErrKindServiceUnavailable`). **This is the heaviest unproven mechanism** —
  isolated in Slice 3.
- **Slice — clean and acyclic** (1 → 2, 3). Recommended.

## The true blocker (rescue, Closing b) — must-fix **C2**

> There is no single owner of "archive state," and no reconciliation rule between
> the records that encode it.

The §1 problem is literally "two representations of the archive drifted apart."
Every escape above dissolves its blocker by choosing or adding a *different*
persistent record — the registry entry, the archive-local sentinel, the retained
job file, `settings.out`, the live scheduler descriptor, `last-archive.txt`, the
manifest `Stores`. That is up to **seven** persistent encodings of "where/what is
this archive, is it managed, is it sealed, is its source alive," and §9 reasons
about the crash-ordering of exactly one pair. The one consumer whose drift opens
the problem — the installed scheduler, which bakes its `-out` into the descriptor
at install time and, by PM2's deliberate design, **never consults the registry** —
cannot be governed by the registry at all. So the fix as sketched **re-creates the
original drift**, now between `known-archives.json.active` and the still-authoritative
`settings.out`, with more writers. This is the class of defect a lens battery
misses: each lens checks one record in isolation; none asks "how many records now
encode archive-state, and what makes them agree?" It is **not a re-design** — the
registry is the right spine — but the design cannot be built until it states a
consistency contract.

## Must-fix-before-code conditions

Each becomes a tracked edit to `docs/design-archive-manager.md`, re-verified.

### Slice 1 — registry index + pointer-unify + active archive (GO_WITH_CONDITIONS now)

1. **S1-C1 (from B2/B2′).** Connect/Switch writes `out` **only** through
   `saveSettings`' XC22/MA-523 keep/remove reconciliation (`BackupFor`), never a
   bare `UpdateSettings`. v1 scopes to **one active archive at a time** (global
   cadence = the active archive's schedule); per-archive concurrent schedules are
   deferred.
2. **C2 — GATING, OPERATOR DECISION (the true blocker).** Add a
   **consistency-model** section that names, per consumer (display · write-guard ·
   scheduler · resume), the **authoritative record** and the **on-load
   reconciliation** when records disagree — centrally
   `known-archives.json.active` vs `desktop-settings.json.out`, and the
   per-machine-registry vs a synced/moved archive. **Downgrade PM1** from "every
   surface reads the registry" to the honest claim (the installed scheduler
   structurally cannot — PM2). State the symlink reduction (M-SYMLINK) here. Also
   relocate `archiveKind`/`validArchiveFolder` to the new leaf (they live in
   `internal/desktop` today — a leaf below it cannot import them back).
3. **S1-C3 (from C-VER).** Add a `version` field + mirror the repo's
   unknown-key-preserve / refuse-newer policy (`Settings.Version`,
   `job.Read`).
4. **S1-C4 (from C-NAME, C-READ).** State the invariant: **the scheduler entry
   name is authored only by `bound.go`, never persisted in or consumed from the
   registry** (bounds OQ1). Read `known-archives.json` with symlink-refusal +
   regular-file + mode checks (as `ReadSecureFile` does for secrets); do not rely
   on `MkdirAll`-on-create for config-dir perms.
5. **S1-C5 (from C-FORGET, C-HEAL).** Name a **Forget-entry** operation (distinct
   from the archive-deletion non-goal). Make self-heal probe reachability with an
   actual open/read, not Stat-only, so an offline cloud placeholder is correctly
   greyed.

### Slice 2 — disconnect / hard read-only seal (returns as its own pre-code review)

6. **S2-C1 (from B1/B1′).** PM3 enforcement = an archive-local
   `.mailarchive-disconnected` sentinel checked by a shared guard at the
   `AcquireAs` verb boundary; registry `state` is display-only. Restate R35 as
   "every write **by MailArchive** refused," sealed set **{export, graph,
   reindex}**. Resolve reindex-as-Repair: Repair re-connects first (or is
   connected-only).
7. **S2-C2 (from OQ2 / B1 to-confirm).** Prove the negative: grep that **no
   archive-content write bypasses `AcquireAs`** (a bare `os.Create`/`WriteFile`
   under an `out`-derived path outside the 7 verbs). If clean, the chokepoint is
   non-leaky; if not, every bypass needs the guard.
8. **S2-C3 (from C-RESUME, C-SEAM).** Disconnect drives
   `BuildKindEvidence(path)`/`removeTaskAt(p)` by the entry's path and **retains
   the job file** (Disconnect ≠ Remove). (Note M-FSYNC: if the stash is the
   resume record, fsync the parent dir on its write.)
9. **S2-C4 (from C-CRASH, C-OUT).** Add reconcile-on-load (a `connected` entry
   with a stash but no live scheduler entry → re-offer/restore). Disconnect must
   clear/repoint `out` when it points at the archive being sealed.

### Slice 3 — source-loss (returns as its own pre-code review)

10. **S3-C1 (from C-SRCLOSS).** Persist `firstFailedAt` + `consecutiveFailures`
    in the registry entry **and** add a structured local-source-unreachable
    signal on the run path (a new `ErrKind*`, distinct from the archive-side
    `reachable`/PM4 guard). Prove this signal before Slice 3 code — it is the
    heaviest unproven mechanism.
11. **S3-C2 (from C-LEGIBLE, M-AUTOPAUSE).** Source-loss must surface where the
    wizard-only (macOS `.app`) user sees it, not only a Desktop banner. Specify
    the auto-pause clock's actor/location (or defer auto-pause, OQ3).

### Operability backlog (Type-III; ships with the feature, not pre-code blockers)

- **BACKLOG-1 (C-HEADLESS).** Add a CLI verb for connect/disconnect/repair/list
  (honours R33) so a headless/cold operator can reach the registry where stale
  cron jobs fester.
- **BACKLOG-2 (C-WIZUX).** Specify how the archive list / greyed entries render
  in the wizard first step (OQ4), or route the manager to the Desktop only and
  drop the wizard from §3's entry points.
- Rename to avoid the "disconnected" overload with the existing folder-moved
  refusals (R-l2/R-o2).

## Mechanical check

`docs/design-archive-manager.md` now cites this file in its BUILD STATUS; the
design-review gate's resolve-by-name requirement is satisfied. The verdict is
staged by slice; a changed design digest makes the affected slice's verdict stale
and reruns those lenses.

---

# Delta-review 2 — folder-only index + the recovery scan (operator C2 decision)

**Reviews the operator C2 decision of 2026-10-20** (design-archive-manager rev 2):
the program keeps a **folder-only** index; each **archive owns its state**; a lost
index is recovered by import plus a **user-initiated, install-type-gated filesystem
scan**. The scan **reverses non-goal #3** ("not a filesystem scan"), so it re-opened
lenses 3/6/8/2/10. **Date:** 2026-10-20. **Repository:** `5ef7725`. **Register:** a
finding. Static, read-only.

**Method:** a background workflow — a judge panel (privacy-first / recovery-first /
bounds-first → scored + synthesized) scoped the scan; then lens finders (security,
durability, migration, proportionality-honesty) attacked the revised model and each
finding was verified by a 3-refuter adversarial panel (refute-by-default). **71
agents, 0 errors.** Counts: 21 findings → 11 confirmed (survived ≥2/3), 10 refuted.
The refuted set was read by the synthesis owner, not dropped blindly: most were
refuted *because the recommended scan design already fixes them* (the `guardLocation`
crux, slice-placement, both-gates-required); those fixes are carried forward below.

## Verdict (delta)

**GO_WITH_CONDITIONS.** The folder-only + archive-owns-state model **dissolves the
C2 blocker** (the index stores no per-archive fact, so nothing can drift against the
archive) **and dissolves the first review's B1′ un-seal** (the disconnected seal now
lives in the archive, so re-import cannot re-mark it connected). The recovery scan
is sound **as its own slice** with its own §4.6 adversarial pass. Eleven must-fix
conditions (SD-1..SD-11) and two operator concessions.

## Recommended recovery-scan design (judge-panel synthesis)

- **Trigger:** only when `known-archives.json` is absent, at a hosting surface;
  **import-first** (settings `out`, `last-archive.txt`, readable cron descriptors),
  which **pre-populates but never suppresses** the brand-new-vs-existing question.
- **Install-type gate:** offered only on a **user-directory install**, via a
  purpose-built home-containment predicate (+ a `geteuid()==0`/privilege check) —
  **not** `guardLocation`. Shared/system install → manual folder-naming fallback.
- **Roots:** the invoking user's home, resolved once (`util.ResolveExisting`); never
  `/`, never auto-enumerated volumes; one owner-named extra folder allowed; skip
  OS/app-data/cache trees and cloud-sync roots.
- **Recognition:** `.mailarchive-manifest.json` marker during the walk (no follow /
  no hydrate); at the Add gate parse via `state.LoadFrom` over size-capped bytes.
- **Bounds:** install-gate AND per-walk caps (both required); mount pre-filter from
  the mount table; depth/entries/time caps each logging a visible outcome; the time
  cap is a UI watchdog, not a cooperative poll.
- **Cloud:** skip cloud roots, never hydrate; a cloud-hosted archive is recovered by
  the manual named-folder path.

## Confirmed conditions (must-fix-before-code; the scan is its own slice)

| ID | Sev | Concern | Fix |
|---|---|---|---|
| **SD-1** | BLOCKER | Recognition via `archiveKind`/`state.Load`-by-path resolves attacker-chosen manifest store-ids through `EvalSymlinks` → a planted manifest with a `\\\\attacker\\share` id makes the victim open an SMB connection and leak NetNTLM creds; symlink ids followed out of the folder. Runs for *every* entry incl. import, not just the consented scan. | Recognize every scanned/imported manifest with `state.LoadFrom(name, data)` (lexical store-ids, no on-disk resolution) over bytes from an `OpenRegularFile` descriptor — never `archiveKind`/`state.Load`-by-path. |
| **SD-2** | CONDITION | Manifest read at recognition/Add is unbounded (`state.Load` → `os.ReadFile`, no cap; `OpenRegularFile` imposes none) → oversized planted marker = OOM/DoS. | `io.LimitReader(cap)` over the descriptor + `state.LoadFrom`; state the byte cap as a design constant. |
| **SD-3** | MINOR | Attacker-chosen scanned folder names/paths flow unsanitized into dialogs, the end-of-scan report, and the CLI terminal → ANSI/newline/bidi forging/spoofing. | `StripControl`+`StripBidi` every scanned path/label at the read boundary before storing or showing it. |
| **SD-4** | CONDITION | The index's read-hardening is not applied to the import-first pointer files it trusts *in preference to* the scan (`last-archive.txt` = bare `ReadFile`, no symlink refusal; `validArchiveFolder` follows symlinks). The preferred fast path is the unhardened one. | Apply the owner-only config-dir precondition + `readSecure` to *all* config-dir readers; validate imported entries no less strictly than scanned (capped `LoadFrom`, no-follow). |
| **SD-5** | CONDITION | The scan's time cap and Cancel cannot interrupt a blocking syscall (dead NFS/SMB mount, asleep drive, macOS placeholder) → the scan hangs past the deadline and Cancel freezes; reused `walkArchive` was only ever aimed at a small local folder. | Time cap = UI watchdog (walk on its own goroutine, surface returns at the deadline, abandon the goroutine); pre-filter mounts from the mount table to `SkipDir` before opening; non-blocking open flag. |
| **SD-6** | CONDITION | "Written at install and kept updated" names no writer on any create path (only point-in-time import + manual Add) → an archive created outside import is never listed, defeating multi-archive. CLI/engine write no config-dir trace today and `internal/app` doesn't import `internal/job`. | Name the single upsert chokepoint on every create/open seam (wizard, CLI, headless), keyed by folder path, ordered manifest-first then index-upsert; import becomes a reconciler — **or** honestly declare the index a GUI-maintained cache over the authoritative filesystem. |
| **SD-7** | CONDITION | Lost index does not fail safe for completeness: rebuild-by-import recovers only pointer-named archives, import-first then suppresses the rescue that would find the rest, and §9's "re-Adds folders" hides that the owner is never told what dropped. | fsync the parent dir after the index rename; keep "Look for archives again" always reachable (ties BACKLOG-1 CLI verb); rewrite §9 to the honest residual. |
| **SD-8** | CONDITION | Index size/entry bound unspecified; reusing the 64 KiB job-file cap means a scan + "add all" can exceed it → the whole index is refused as oversize exactly on the many-archive systems the scan exists for. | Explicit max-entries bound + a read size cap sized to it (not 64 KiB); refuse/prune legibly. Pairs with Forget (S1-C5). |
| **SD-9** | CONDITION | Import-first silently defeats recovery on macOS/Windows: §7 reads schedulers back only on cron, so launchd/Task Scheduler import sees only `out`≈`last-archive.txt` = one archive; the "≥1 → stop, never ask" rule then never asks a user who in fact has several. | Import pre-populates but never suppresses: trigger on "index absent" → always surface the choice showing what import found; suppress the scan offer only when coverage is provably complete (cron + readable crontab). |
| **SD-10** | CONDITION (concession) | Rollback un-seals: the disconnected seal is an archive-local sentinel enforced by a *new* write-lock guard; a pre-feature build has none, so a one-release downgrade makes a sealed archive writable again (fail-open on R35 via rollback). | Concede and document (no pre-feature enforcement to inherit); bound exposure to one release back. Add a `version` + refuse-newer field to the index. |
| **SD-11** | CONDITION (honesty) | Non-goal #3 ("not a filesystem scan") still stood and would immunize the riskiest new mechanism from the §4.6 pass; PM1 is contradicted by the folder-only decision. | Rewrite non-goal #3 to the scoped/consented/install-gated recovery scan (done, rev 2 §2); downgrade PM1 (done, rev 2 §2). |
| **SD-crux** | CONDITION | The owner's "reuse `guardLocation`" instruction is the wrong test — it answers Q20 write-safety, so it would *offer* the scan on a non-world-writable system install (`/Applications`, `C:\Program Files`), incl. to an elevated invoker. (Refuted vs the synthesis only because the synthesis already replaced it.) | Build a purpose-built home-containment predicate + a privilege (`geteuid()==0`) gate; never `guardLocation`. |

**Carried from the refuted set (residual guidance, folded into rev 2):** the index
must hold **no `active`/`current` field** — current-archive stays `out`
(authoritative), the switcher writes `out`; `kind`/`label` are **derived, not
stored**; the whole-file last-writer-wins replace needs a concurrency note if a
headless upsert is added (SD-6). All reflected in rev 2 §4.

**Status:** rev 2 integrates SD-11 and SD-crux into §2/§5.1 and the folder-only
model + invariants into §4/§8/§9; SD-1..SD-10 are tracked must-fix items for the
index slice and the scan's own slice. Outstanding gate: the UX/friction review of
the multi-archive surface.

---

# UX / friction review — the multi-archive surface (2026-10-20)

Walked each actor (the wizard-only macOS `.app` user, the Desktop user, the
headless operator) through the multi-archive flows — list / switch / disconnect /
repair / greyed-offline / source-loss banner / recovery-scan — against the X-series
UX contract (`docs/ux-contract.md`), with a 3-refuter adversarial verify per
finding. **97 agents, 0 errors; 31 findings → 7 confirmed, all Type II.**

**Verdict:** the multi-archive UI is **buildable with these conditions**; they land
with the Desktop wiring and Slice 2, and **none touches the Slice-1 index leaf**
already built. The 7 conditions are recorded in
`design-archive-manager.md` §13 (UX-1..UX-6):

- UX-1 (X6) the list IS the switcher — a Desktop Overview archive list, each row a
  "Switch to this archive"; the free-text box demoted to "Add a folder" (resolves
  OQ4).
- **UX-2 (X10) [OPERATOR RULING]** the wizard reads `last-archive.txt`, the Desktop
  `out` → a switch is invisible to the wizard (the §1 drift between surfaces).
  Recommended: the wizard adopts `out`; `last-archive.txt` becomes a reconciled
  breadcrumb.
- UX-3 (X6/X12) greyed/sealed rows state their state and remedy; the switch-click
  never silently pins `out` to an unreadable archive.
- **UX-4 (X11) [OPERATOR RULING]** a distinct verb for the deliberate seal (bare
  "disconnected"/"Reconnect" is already spent 3 ways). Recommended: "Pause (make
  read-only)" / "Resume".
- UX-5 (X14 / MA-652) Disconnect confirms, naming its three effects; the dashboard's
  plain Remove is brought to the same confirm bar.
- UX-6 (X6/X11) the seal refusal routes to where Resume lives, never a dead-end the
  wizard cannot act on.

Two operator rulings (UX-2, UX-4) were surfaced for the morning and **both ruled by
the operator 2026-10-20** — UX-2: the wizard adopts `out`; UX-4: "Pause (make
read-only)" / "Resume" — the rest are design conditions applied with Slice 2 / the
Desktop wiring.
