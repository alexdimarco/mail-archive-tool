# Design — the archive manager: discover, connect, disconnect, repair and schedule archives across all three schedulers

**Revision:** 2 (2026-10-20). **BUILD STATUS:** reviewed — pre-code, conditions
open. Two pre-code reviews are filed in `docs/review-archive-manager-predesign.md`:
the 10-lens review (verdict **GO_WITH_CONDITIONS**, sliced into sequential gates)
and the scan / folder-only-index delta-review (§"Delta-review 2"), which this rev 2
integrates. Rev 2 records the operator's **C2 decision** (2026-10-20): the program
keeps a **folder-only** index; each **archive owns its own state** in its own
folder; a lost index is recovered by import plus a **user-initiated,
install-type-gated filesystem scan**. No code until (a) the operator ratifies the
revised design and the two conceded items — **both ratified by the operator
2026-10-20**: rollback un-seals a disconnected archive (SD-10, accepted as a
one-release-back limitation); the index is a **Desktop-maintained cache**, so the
headless/CLI create path writes no index entry (SD-6) — and (b) each must-fix
condition (SD-1..SD-11) is applied here as a tracked edit and re-verified.
The recovery scan is its **own slice** with its own §4.6 adversarial pass. The
**UX / friction review** of the multi-archive surface is filed (§13: 7 Type-II
conditions, two of them operator rulings — UX-2, UX-4, both ruled 2026-10-20).
A material change to this doc re-runs the affected lenses. **Author:** Claude
(Opus 4.8) with Alex DiMarco.

Terms are defined where they first appear. IDs (R34+, MA-653+, S60+) are
**proposed** and provisional until the operator allocates them in
`docs/scenario-catalog.md`; the current maxima are R33, MA-652, S59.

---

## 1. The problem

Three friction points a tester hit on macOS (2026-10-19) trace to one cause:
"the archive" is an **implicit single pointer, stored two different ways that
have already drifted apart**.

- The Desktop uses `effectiveOut()` = `desktop-settings.json`'s `out`
  (`internal/desktop/settings.go`).
- The wizard writes the last archive it touched to `last-archive.txt`
  (`internal/job/job.go` `LastArchiveFile`), which the Desktop reads **only as a
  fallback when no `out` is saved** (`localarchive.go` `offer()`).

So after the wizard makes a new archive, the Desktop stays pinned to the old
one; there is no way to see, switch between, or manage more than one archive;
and a scheduled job for an archive the owner has moved on from keeps running
(or failing) with no clean way to remove or repair it from the GUI — the sharper
because on two of three host schedulers we never read entries back (§7).

This design promotes the archive from an implicit pointer to a **first-class,
named, enumerable set with its schedule lifecycle attached** — one registry, the
three host schedulers (cron / launchd / Task Scheduler) behind it.

## 2. Property / threat-model fit

The design asserts these properties (status labelled per lens 10):

- **PM1 — one index; the archive owns its state.** Every surface (wizard, Desktop,
  CLI) reads the same per-user index of archive **folder locations**; each archive
  owns its own state (source identity, sealed/disconnected flag, schedule
  descriptor) in its own folder. The index is a convenience list of *where* archives
  are — never a system of record, never authoritative for any per-archive fact; the
  installed scheduler, by PM2, never reads it. *(conditional — the index must carry
  no active / sealed / schedule field; §4.)*
- **PM2 — a registry entry never grants a scheduler action.** Discovering or
  listing an archive never lets the tool remove/repair a scheduler entry unless
  the archive's own descriptor **binds** that entry under the existing rules
  (`internal/schedule/bound.go`, MA-455, the UJ-6 read-back limit). The registry
  is a convenience index, never an authority over the OS scheduler. *(conditional
  on reusing `bound.go` unchanged — §6.)*
- **PM3 — disconnect is hard read-only and reversible.** A disconnected archive
  runs no schedule and refuses every content-mutating MailArchive operation until
  reconnected, enforced by an **archive-local sentinel** and an **intent-aware
  write-gate** on the mutating operations plus the two pre-lock entry points — NOT a
  blanket check inside `lockfile.AcquireAs` (that lock is taken by reads too), and
  not the per-open `Other` flag (which gates no write path). *(resolved by the
  Slice-2 pre-code pass — §5.3.1, conditions S2-C1..C7, ratified 2026-10-20.)*
- **PM4 — fail closed on a missing archive or a missing source.** Neither a lost
  archive folder nor a lost source is ever "fixed" by writing elsewhere; it is
  recorded and surfaced, never silently worked around. Extends the engine's
  existing guard ("…is not present — is the backup drive mounted? refusing to
  create a new archive elsewhere"). *(proven precedent; new surfacing is
  aspirational — §5.7.)*

Non-goals (conceded; a finding that faults these is invalid — lens 1/2):

- **Not** a general scheduler editor. The manager acts only on archives in its
  registry and only on entries the `bound.go` rules prove are ours.
- **Not** archive deletion. Once disconnected, the owner deletes the folder by
  hand if they want it gone (owner ruling, 2026-10-19).
- **Not** an always-on crawl. Discovery imports known pointers and accepts a folder
  the owner names. When the per-user index is **missing**, a one-time,
  user-initiated, home-scoped **recovery scan** is offered — only on a
  user-directory install, skipping cloud and other drives, always paired with
  manual folder-naming — to rebuild the list (§5.1). This is a consented recovery,
  not a background crawl, and it **is in scope for the §4.6 adversarial pass** (it is
  not immunized as a non-goal).

## 3. Preserved constraints

- **R33 — one program.** No new executable; every surface is an in-process verb
  (MA-612 AST walk stays green). The manager is reached through the existing
  wizard first-step menu (MA-651) and the Desktop, not a new binary.
- **MA-455 / the "ours" binding** (`bound.go`) is reused unchanged for every
  Remove/Repair/Continue. Disconnect's schedule removal goes through
  `schedule.RemoveIfInstalled` exactly as the GUI remove does today.
- **Import-cycle constraint.** `internal/desktop` imports `internal/wizard`;
  the registry package must sit **below** both (a new leaf package, e.g.
  `internal/archindex`, imported by wizard, desktop and the cmd layer), so both
  surfaces share it without a cycle.

## 4. The model — a folder-only index

**Operator C2 decision (2026-10-20):** the program's file lists archive **folder
locations only**; the **archive owns its own details**. That is the single owner
per fact, and it is what dissolves the C2 drift — the index holds no fact that
could disagree with the archive.

One owner-only JSON file, `known-archives.json`, in the per-user config dir
(`job.Dir()` — the same folder as `last-archive.txt` and the job files;
`~/Library/Application Support/mailarchive/` on macOS, `%AppData%\mailarchive\`
on Windows, `$XDG_CONFIG_HOME/mailarchive` on Linux). It is written and read
through the proven `internal/job` pattern — atomic 0600, a `version` field, a file
written by a newer build refused, read via `readSecure` (a symlinked or
non-regular file refused) — not a bare `WriteFile`/`ReadFile` (review SD-4/SD-10).

One entry per archive — **folder location plus provenance, nothing else**:

| field | meaning |
|---|---|
| `path` | the archive folder (absolute) |
| `addedAt`, `lastSeenAt` | provenance and staleness clock (index-owned) |

**Everything about the archive is derived live from the archive, never cached in
the index:** its `kind` from the manifest `Stores`, its label from the folder's
base name, its **sealed/disconnected** state from an archive-local sentinel, its
**schedule** from the archive-local descriptor (`ReadDescriptor(out)`). The manifest
is read with `state.LoadFrom` over size-capped bytes — never `archiveKind()` /
`state.Load`-by-path, which resolves attacker-chosen store-ids and can open a UNC
path or follow a symlink out of the folder (review **SD-1/SD-2**). Nothing cached
means nothing to go stale, and no second owner for any fact.

**The active-archive invariant.** "Which archive is current" is owned by
`desktop-settings.json` `out` (authoritative — `effectiveOut()`), **not** the
index. The index MUST NOT hold an `active`/`current` field; adding one would
re-create the two-pointer drift this feature exists to kill. The switcher writes
`out` through the existing keep/remove reconciliation (review S1-C1); the index
only lists the set to switch among.

**Written at install, kept updated (review SD-6).** A single upsert chokepoint on
the shared archive-create/open seam — reached by the wizard, the CLI and the
headless run alike — adds/updates the entry keyed by the archive's folder path,
ordered manifest-first then index-upsert, so a crash leaves a recoverable
archive-without-entry, never a dangling entry; import (§5.1) is a reconciler, not
the primary writer. **Operator decision (2026-10-20, SD-6):** the index is a **Desktop-maintained
cache** over the authoritative filesystem. The CLI/engine create path
(`internal/app`, `cmd/mailarchive`) writes **no** index entry — it needs no
`internal/job`/`internal/archindex` import, and a headless run as a different user
(with a different `os.UserConfigDir()`) never writes a stray index. The Desktop
reconciles the cache (import + the offered scan) when it opens; a CLI-created
archive appears once the Desktop next reconciles or the owner Adds/finds it. The
archive folder itself is always authoritative.

**Bounds (review SD-8).** An explicit max-entry count, with a read size cap sized
to it — not the 64 KiB job-file cap — so a recovery scan that finds many archives
never produces a file the reader then refuses as oversize. Beyond the cap it prunes
or refuses legibly, never fails the whole file silently.

**Self-healing.** On load each entry's folder is checked for the archive marker;
an entry whose folder is unreachable (unmounted drive, offline cloud placeholder)
is shown greyed, never removed, never recreated, and returns when reachable.
Reachability is probed by an actual bounded open — not a bare `Stat`, which a
dehydrated cloud placeholder passes while offline (review SD-6/self-heal). The file
grows only by create/Add and shrinks only by an explicit **Forget** (review S1-C5);
there is no auto-prune.

## 5. Operations

### 5.1 Discover (import, then a consented recovery scan)
When `known-archives.json` is **absent** on launch:

1. **Import first — pre-populate, never suppress (review SD-9).** Reconcile from the
   known pointers: `desktop-settings.json` `out`, `last-archive.txt`, and any folder
   named by a *readable* schedule descriptor (cron only — launchd and Task Scheduler
   are not read back, §7/UJ-6). Import never calls Install/Remove; a live schedule
   keeps running. On macOS/Windows import can see only `out`≈`last-archive.txt` (one
   archive) even when the user has several, so import pre-populates but **never
   suppresses** the next step.
2. **Always surface the choice.** Show what import found, then ask in plain words:
   *brand-new setup, or does this computer already have archives you made before?*
   "Brand new" writes an empty, versioned index (which stops the re-ask); "I already
   have some — look for them" is the **consent** to scan. The scan offer is withheld
   only when coverage is provably complete (cron with a readable crontab), never on
   launchd/Task Scheduler.
3. **The recovery scan** — its own slice, its own §4.6 adversarial pass (the heaviest
   new mechanism): a one-time, user-initiated, home-scoped walk.
   - **Install-type gate.** Offered only on a **user-directory install**, decided by
     a purpose-built home-containment predicate (is the program within the invoking
     user's home?) — **not** `guardLocation`, which answers Q20 write-safety and
     would wrongly offer the scan on a `/Applications` or `C:\Program Files` system
     install (review **SD-crux**). A shared/system install (admin, or
     `geteuid()==0`) does not offer the scan; it falls back to the owner naming the
     folder.
   - **Roots.** The invoking user's own home, resolved once (`util.ResolveExisting`)
     so the start is not itself a symlink; never `/`, never an auto-enumeration of
     volumes / mounts / removable / network drives. The one widening is owner-named:
     the owner may add exactly one folder they type or pick (an external- or
     cloud-hosted archive), validated identically. Skip the OS / app-data / cache
     trees and the cloud-sync roots.
   - **Recognition.** During the walk, by a `.mailarchive-manifest.json` directory
     entry only — no follow, no hydrate. At the confirm/Add gate, parse it with
     `state.LoadFrom` over bytes read under an explicit size cap from an
     `OpenRegularFile` descriptor (never `archiveKind`/`state.Load`-by-path — review
     **SD-1/SD-2**); reject zero-byte/non-JSON; list a newer-format manifest as
     "upgrade to manage"; dedupe by `os.SameFile`.
   - **Bounds — both gates required (review SD-5).** The install gate is necessary
     but not sufficient: a plain walk can still follow a symlink/junction or cross a
     mount into another user's world-readable files inside the home. Reuse the
     bounded no-follow `walkArchive` pattern; pre-filter mounts from the mount table
     (`classifyMountinfo` / `getmntinfo`, read without touching the mount) to
     `SkipDir` share/removable/stacking mounts before opening them; cap
     depth/entries/time; every cap ends in a logged, owner-visible "stopped/skipped
     because X". The time cap is a **UI watchdog**, not a cooperative poll — the
     surface returns to the owner at the deadline even if the walk goroutine is
     blocked on a dead mount.
   - **Output hygiene (review SD-3).** Every scanned path and derived label is
     `StripControl`+`StripBidi`'d at the read boundary before it is stored or shown
     in any dialog, report or terminal.
4. Every imported / scanned / added entry is validated no less strictly than any
   other (capped `LoadFrom`, no-follow), and the same owner-only config-dir
   precondition and `readSecure` discipline apply to **every** config-dir reader
   (`last-archive.txt`, `desktop-settings.json`, descriptors), not only
   `known-archives.json` (review **SD-4**). Monotone — discovery only adds entries.

### 5.2 Connect / Add
Register an archive folder the owner names (validated as a real archive), or flip
a disconnected entry back to `connected`. A connected archive is writable and
schedulable. Connect offers to restore the stashed `schedule` if one is recorded.

### 5.3 Disconnect (hard read-only, from management)
Flip the entry to `disconnected`:
1. Remove the live scheduler entry across whichever host scheduler holds it
   (`schedule.RemoveIfInstalled`, which already removes the descriptor and, for a
   run-job, its job file), **stashing** `{interval, at}` in the registry first.
2. Mark the archive hard read-only: every later open is immutable
   (`ArchiveOptions{Other:true}`), and any write path (Archive now, a manual
   incremental, a scheduled capture) refuses with a "this archive is
   disconnected — Connect it to make changes" message.
3. Help text notes: *once disconnected you can delete the folder yourself if you
   want it gone.*
**To-confirm (PM3):** enumerate every write entry point (capture, incremental
`-out`, rebuild, extraction-workspace writes) and confirm the disconnected check
sits ahead of each, not only the immutable index open. This is a lens-6/lens-7
condition for the review.

### 5.3.1 Pre-code pass resolution (ratified 2026-10-20; S2-C1..C7)
The Slice-2 adversarial pass (workflow, 19 findings; two spot-verified in code) found
PM3's "one sentinel checked inside `lockfile.AcquireAs`" both **leaky** (writes reach
the archive without that lock) and **over-broad** (reads take that lock too). Resolved:
- **S2-C1 — intent-aware gate, not a blanket `AcquireAs` check.** The seal refuses only
  the content-mutating operations (capture/export, graph, incremental `-out`, `reindex`
  **redaction**, `verify -record`) and allows reads — plain `verify`, `extract` (writes
  only to `-dest`), and the empty-verb health probe. `reindex` **rebuild** of a lost
  index is **allowed** (operator ruling 2026-10-20: it regenerates derived data, not
  mail; cf. Repair §5.4). Each write caller declares write-intent; the seal does not key
  on the ambiguous `AcquireAs` verb string alone (one "verify" = read & `-record`; one
  "reindex" = destructive redaction & derived-index rebuild).
- **S2-C2 — entry-point checks.** `HeadlessCapture` and `RunHeadless` check the seal at
  ENTRY (before `recordStartFailure`/`WriteLastRun`, the run-log open, and the
  `_outlook-pst` recreate — all of which precede the engine lock), not only inside the
  engine.
- **S2-C3 — remove *both* schedules.** Disconnect removes the backup **and** the separate
  scheduled verify (`.mailarchive-verify-schedule.json`, FB-4), stashing both cadences, or
  a live schedule survives a "paused" archive (R35).
- **S2-C4 — un-seal is lock-free and headless-capable.** Connect/Resume clears the sentinel
  WITHOUT passing through the gated write path; a new `mailarchive` CLI un-seal verb (by
  `-out`) ships in Slice 2 (operator ruling 2026-10-20), because the seal bites on the
  CLI/scheduled path while Connect is Desktop/wizard-only (a NAS/traveled archive would
  otherwise lock out).
- **S2-C5 — Connect crash-ordering mirrors Disconnect.** Connect clears the sentinel
  **first**, then restores the schedule, so a crash leaves connected+unscheduled
  (writable, retryable), never scheduled+sealed. Connect twin of MA-659.
- **S2-C6 — sentinel discipline.** Sealed ⇔ a regular file present at the sentinel path
  (**fail-closed**: a FIFO/symlink/unreadable sentinel reads as sealed, never followed or
  opened-to-hang); presence checked by `os.Lstat` (no follow, no open), written/removed
  atomically (`util.WriteFileAtomic0600`). The `ArchiveOptions{Other:true}` flag named in
  §5.3 step 2 is **dropped** — it gates no write path.
- **S2-C7 — detectability + durable stash.** `status` and the Desktop reconcile the
  archive-local descriptor against the sentinel and flag "paused but still scheduled" (a
  refused run writes no last-run, so health would otherwise read stale-green); the stashed
  cadence has a durable, §4-consistent home capturing the fields a run-job needs.
Full findings: Slice-2 pre-code pass output.

### 5.4 Repair
Reuse the existing repair routes (`health` `rebuildHow`/`repairHow`,
`schedule.ReadDescriptor` + `wizard.RepairSpec`, MA-617): rebuild a lost index,
rewrite a damaged descriptor, or re-bind a schedule whose program moved/upgraded
(detected from the descriptor's `ExeSize`/`ExeMTime`, which already exist for
exactly this).

### 5.5 Schedule
Set or replace the connected archive's backup schedule through the existing
`schedule.Install` path and `WrapperNeeded` decision (MA-641) — unchanged; the
manager only chooses the archive and cadence.

### 5.6 Reconnect & continuation
- **Resume on reconnect (#2, in scope).** Connect restores the stashed cadence
  so a parked archive picks back up without re-entering settings.
- **Survive a move (#1, best-effort per scheduler).** The descriptor already
  records `Exe`/`ExeSize`/`ExeMTime` and `bound.go` already handles a *moved
  archive folder* (its `from` path). A *moved/upgraded program* is detected by
  Repair and re-bound. What cannot be done silently on launchd/Task Scheduler is
  re-reading the live entry to confirm it (UJ-6) — so "survive a move" is a
  Repair prompt, not an invisible self-heal, on those two.
- **Resume a killed run (#3, already covered).** Incremental mode only adds
  what is new, so a killed run is continued by the next run with no new
  mechanism. Noted so the review does not treat it as missing.

### 5.7 Source-loss handling
When a connected archive's **source** (the M365 mailbox, or the local
Thunderbird/mbox folder) becomes unreachable:
- **Transient vs permanent** is judged by duration/run-count, not one miss
  (built on the health model, which already carries a `reachable` signal plus
  `LastStartFailure` and the `.mailarchive-lastrun.json` record). A drive
  unplugged overnight, a sleeping laptop, a token needing refresh = transient,
  keep trying.
- **Fail closed** — record the miss, change nothing, never half-capture or
  recreate elsewhere (the existing guard, PM4).
- **Surface loudly** after a threshold (N failed runs or T days): the Desktop
  shows "this archive's source hasn't been reachable since DATE" with two
  actions — **Reconnect source** or **Disconnect (seal read-only, stop the
  failing schedule)**. This is why source-loss and Disconnect share machinery:
  losing the source is the main reason to disconnect.
- **No auto-delete, no auto-seal.** Default: keep trying + show the banner;
  *optionally* auto-pause the schedule after ~30 days only to stop nightly
  failure noise — never seal or forget on its own (owner-approved policy).
- **To-confirm:** the exact fields `.mailarchive-lastrun.json` records and
  whether it already distinguishes source-unreachable from other failures, and
  the health model's current threshold — pin both before coding (lens 3/10).

### 5.8 Uninstall (program + process only — archives remain)
A **full uninstall** removes MailArchive the program and everything it runs or stores
to run itself, and **never touches an archive** (owner ruling 2026-10-20; the product
already promises "uninstalling keeps your archive — your saved mail is never deleted",
the Security & privacy card). Scope to remove:
- **Processes / schedules** — every scheduler entry this install owns, for EVERY known
  archive: the backup AND the verify entry (S2-C3), across launchd / cron / Task
  Scheduler, each via the existing `schedule.RemoveIfInstalled` + `bound.go` "ours"
  rules (PM2 — never remove an entry this install does not own).
- **Config + sign-in** — the per-user config-dir contents: `desktop-settings.json`,
  `graph-config.json`, `last-archive.txt`, `known-archives.json`, the job files, the
  capture / `.jobfail` logs, and the device sign-in (the OS vault entry on Windows, the
  file token cache elsewhere) — the credential removal is the security-sensitive part.
- **The program** — the binary / app bundle / installed files, per platform (the OS
  uninstaller does this on Windows via the MSI; the command can clear the app-managed
  state + schedules + sign-in and then hand off to the OS uninstaller for the binary).
**Hard invariant R36 (NEW, proposed):** uninstall deletes NO archive and nothing inside
one — not the manifest, the mail, the search index, or the pause sentinel — even when an
archive sits under or beside the config dir (the P10 overlap case); it removes only the
enumerated program/config/schedule paths, never an `-out` / known-archive folder.
**Owes its own pre-code adversarial pass** before any code (data-loss on R36, the
credential deletion, the cross-platform scheduler removal), like Slice 2. Open questions
for the pass: remove the binary itself or hand off to the OS uninstaller; enumerate
schedules from `known-archives.json` + the live scheduler or only the configured one; a
dry-run/confirm that lists exactly what will be removed AND names the archives it keeps;
behaviour for a shared/admin install vs a per-user one (cf. the scan install-type gate).

## 6. Security & the "ours" binding (lens 6)

Every Remove/Repair/Continue flows through `schedule.RemoveIfInstalled` /
`ReadDescriptor` and the `bound.go` rules, which already decide whether a
scheduler entry is this archive's (derived name binds; a custom name needs host
proof — cron's block `-out`, Windows' XML/wrapper side-file; launchd stays
bound only by `-out`, UJ-6). **The registry adds no new authority:** it cannot
cause an action on an entry the descriptor does not bind. A registry entry
pointing at a folder whose descriptor records another archive's or another
program's entry is refused exactly as `-out X -remove` is today. The adversarial
pass (§4.6 / `process/adversarial-review.md`) must confirm the registry cannot
be used to smuggle an action past the binding (a planted `known-archives.json`,
a registry entry whose path is a symlink, a disconnected-state flag flipped by a
non-owner).

## 7. Cross-platform

The crux for "all OS versions": **we deliberately never read launchd or Task
Scheduler entries back (UJ-6); only cron is read back.** So the registry — built
from our own on-disk records (archive descriptors, config-dir job files) — is the
cross-OS source of truth; we never ask the OS scheduler to enumerate for us.

| scheduler | name/anchor | can enumerate ours? | remove | source-loss signal |
|---|---|---|---|---|
| cron (Linux/BSD) | `CronMarker(name)` block | yes (read crontab) | `RemoveCronBlock` | cron MAILTO + last-run |
| launchd (macOS) | `com.dimarcotech.mailarchive.<name>.plist` | no — plist existence only | `removeLaunchd` (unload + rm) | StandardErrorPath log + last-run |
| Task Scheduler (Windows) | task `<name>` + XML side-file | existence via `/Query`, not content | `removeSchtasks` + side-files | last-run + `.stderr.log` |

All three already exist and are reused unchanged; the manager chooses *which*
archive/entry, the `schedule` package does the OS work.

## 8. Migration & coexistence (lens 8)

- **Monotone import.** First launch of the new build imports the two existing
  pointers into the registry; nothing is removed, and **any running schedule
  keeps running** — the import never calls Install/Remove.
- **Mixed versions.** An older build ignores `known-archives.json` and uses
  `desktop-settings.json` `out` as before; the current build keeps `out` as the
  authoritative current-archive pointer (the switcher writes it), so a downgrade
  loses the *list* (rebuildable by re-import + rescan) but not the current archive
  or its schedule. The index carries a `version` field and refuses a file written by
  a newer build (review SD-10).
- **Rollback un-seals a disconnected archive (conceded, review SD-10).** The
  disconnected seal is an archive-local sentinel enforced by a *new* write-lock
  guard; a pre-feature build has no such guard, so downgrading one release makes a
  sealed archive writable again — a fail-open on R35 reached by version-rollback.
  There is no pre-feature enforcement to inherit, so the design concedes this and
  bounds the exposure to one release back (**operator accepted, 2026-10-20**).
- **Rollback of the rollout.** Deleting `known-archives.json` returns the Desktop to
  single-`out` behaviour with no schedule disturbed (the index held no per-archive
  state to lose).

## 9. Failure domains (lens 7)

- **Index lost/corrupt** → rebuilt by re-import (pointer-named archives) plus the
  always-reachable "Look for archives again" rescan for the rest; the index write
  fsyncs the parent directory after the rename, so a lost update is not silent.
  Honest residual (review SD-7): import alone recovers only pointer-named archives —
  manually-added and scan-found entries need the rescan. No schedule or archive
  content is affected (the index is not a system of record).
- **Registry entry stale** (archive moved/deleted) → marked unavailable, never
  acted on, never recreated.
- **Disconnect interrupted** (schedule removed, read-only flag not yet written,
  or vice-versa) → state the crash ordering so the residual fails safe: write the
  `disconnected` state **after** the scheduler entry is gone, so a crash leaves a
  still-connected archive with its schedule (retryable), never a "disconnected"
  archive with a live schedule still writing to a folder marked read-only.

## 10. Proposed catalog additions (provisional IDs)

Invariants (R):
- **R34** — the archive registry is the single source of truth for which
  archives an install manages; it grants no authority over any scheduler entry
  (every scheduler action still passes the `bound.go` binding).
- **R35** — a disconnected archive is hard read-only (no live schedule; every
  write refused) and reversible via Connect.

Test-specs (MA) — U unless noted:
- **MA-653** import builds the registry from `out` + `last-archive.txt` +
  descriptor-named folders, monotone, without touching a live schedule.
- **MA-654** Disconnect removes the live entry on each scheduler and refuses
  every write path afterward (A, per-OS where it can be driven).
- **MA-655** Connect restores the stashed cadence and re-enables writes.
- **MA-656** a registry entry whose descriptor does not bind the entry cannot
  drive a Remove/Repair (S — adversarial; the PM2 guard).
- **MA-657** an unavailable (offline/missing) entry is marked, never removed,
  never recreated.
- **MA-658** source-loss is judged transient vs permanent, fails closed, and
  surfaces the Reconnect/Disconnect choice after the threshold.
- **MA-659** crash-ordering of Disconnect leaves a connected+scheduled residual,
  never disconnected+scheduled (S).

Scenarios (S): **S60** discover→connect→schedule a second archive; **S61**
disconnect→delete-by-hand; **S62** source drive lost→banner→reconnect;
**S63** park (disconnect) then resume (connect) with cadence restored.

## 11. Open questions for the review

1. Registry schema stability — is `{interval, at}` enough to restore a schedule,
   or must the full `Spec` (mode, inputs, follow-links) be stashed for a run-job?
2. Does "hard read-only" extend to the extraction workspace's own writes, or only
   to capture/incremental? (§5.3 to-confirm.)
3. Source-loss threshold values (N runs / T days) and whether auto-pause ships in
   v1 or is deferred.
4. Where the archive list lives in the Desktop UI and how it reconciles with the
   wizard first-step menu (MA-651) — **resolved by the UX review (§13 UX-1): the
   list lives on the Desktop Overview, each row a "Switch to this archive"; the
   wizard only hands off to it.**

## 12. Status

Reviewed, pre-code. `docs/review-archive-manager-predesign.md` (10-lens battery +
skeptic + rescue) returned **GO_WITH_CONDITIONS**. Both apparent blockers (the
read-only enforcement gap and the missing active-archive model) dissolved to named
modest changes; clearing them surfaced the true blocker **C2** — the design has no
single owner of "archive state" and no reconciliation rule between the ~7 records
that encode it, so the fix as sketched re-creates the §1 drift. The registry spine
is sound; nothing needs re-designing.

Recommended cut into three sequential pre-code gates:
- **Slice 1** (pointer-unify + registry index + active-archive pointer) —
  GO_WITH_CONDITIONS now; conditions S1-C1..C5 + the gating **C2**.
- **Slice 2** (disconnect / hard read-only seal) — own pre-code mini-review;
  conditions S2-C1..C4.
- **Slice 3** (source-loss) — own pre-code mini-review; conditions S3-C1..C2;
  holds the heaviest unproven mechanism (a new local-source-unreachable signal).

Next: operator light sign-off on the verdict and on **C2** (the gating decision),
then apply the Slice 1 conditions here as tracked edits and re-verify, then code
Slice 1. Operability items BACKLOG-1..2 ship with the feature.

**Rev 2 (2026-10-20) — operator C2 decision + scan delta-review.** C2 is resolved
the folder-only way (§4): the index lists folder locations only, each archive owns
its state, and a lost index is recovered by import plus a consented,
install-type-gated filesystem scan (§5.1; non-goal #3 rewritten). The
scan/folder-only delta-review (`review-archive-manager-predesign.md` §"Delta-review
2", 71 agents) confirmed the model **dissolves C2 and the old B1′ un-seal** and
returned **GO_WITH_CONDITIONS** with 11 conditions SD-1..SD-11 — notably the
credential-egress fix (recognize via `state.LoadFrom`, never
`archiveKind`/`state.Load`-by-path; SD-1/SD-2), the install-type gate as a
home-containment predicate not `guardLocation` (SD-crux), import pre-populates but
never suppresses the recovery offer (SD-9, it would otherwise silently miss
archives on macOS/Windows), and two conceded items (rollback un-seals — SD-10; the
headless-create-path index writer — SD-6). The recovery scan is its **own slice**
with its own §4.6 adversarial pass. The **UX / friction review** of the
multi-archive surface is now filed (`review-archive-manager-predesign.md` §"UX
review"; §13 below): 7 Type-II conditions, two of them operator rulings (UX-2 the
current-archive reconciliation, UX-4 the seal verb).

## 13. UX / friction conditions (the multi-archive surface, 2026-10-20)

The UX/friction review walked each actor (the wizard-only macOS `.app` user, the
Desktop user, the headless operator) through the multi-archive flows against the
X-series UX contract and returned 7 conditions, all Type II (design-choices,
cheapest fixed in the spec now). They land with the Desktop wiring and Slice 2 —
none touches the Slice-1 index leaf already built.

- **UX-1 (X6) — the list IS the switcher.** The Desktop Overview gains an
  archive-list surface reading the index; each row carries an action-first "Switch
  to this archive". The free-text folder box is demoted to "Add an archive folder".
  Resolves OQ4: the list lives on the Desktop Overview; the wizard first-step menu
  only hands off to it, never rendering the list in a native dialog.
- **UX-2 (X10) — one current-archive truth across surfaces. [OPERATOR RULING]** The
  wizard health view reads `last-archive.txt` while the Desktop's active archive is
  `out`, so after a Desktop switch the wizard still shows the old archive — the §1
  drift surviving *between* surfaces. **RULED (operator, 2026-10-20):** the wizard reads the same
  authoritative active pointer (`out`); `last-archive.txt` becomes only the
  wizard's "last touched" breadcrumb, reconciled to `out` on launch.
- **UX-3 (X6/X12) — greyed/sealed rows state their state AND remedy.** A row
  pre-states "this location isn't reachable right now — reconnect the drive and it
  comes back" or "read-only — Resume it to make changes"; the switch-click on a
  greyed/sealed row blocks with the reason or switches read-only, never silently
  pins `out` to an unreadable archive.
- **UX-4 (X11) — a distinct verb for the deliberate seal. [OPERATOR RULING]** The
  bare word "disconnected"/"Reconnect" is already spent on drive-gone (R-l2/R-o2)
  and source-loss ("Reconnect source"). The deliberate read-only seal needs its own
  action-first pair, chosen before code so the sentinel, the write-refusal text, the
  state label and the button all land on one word. **RULED (operator, 2026-10-20): "Pause (make read-only)" / "Resume".**
- **UX-5 (X14 / MA-652 symmetry) — Disconnect confirms, naming its three effects.**
  Disconnect (remove the schedule + seal read-only + stash the cadence) is more
  consequential than the plain Remove, so §5.3 gains an explicit confirm naming the
  three effects and the reverse verb, mirroring MA-652; the dashboard's plain Remove
  (which fires with no confirm today) is brought to the same bar at the same time.
- **UX-6 (X6/X11) — the seal refusal points at a reachable action.** A sealed
  archive's write-refusal routes to where Resume lives — "This archive is paused
  (read-only). Open MailArchive Desktop → Archives to resume it." — never naming an
  action the wizard (the only macOS double-click surface) cannot offer.

UX-2 and UX-4 are **ruled (operator, 2026-10-20)** — the wizard adopts `out`, and
the seal verb is "Pause (make read-only)" / "Resume"; the rest apply with Slice 2 /
the Desktop wiring. None blocks the Slice-1 leaf already built.

**Implementation status (2026-10-20, branch `archive-manager`):** UX-1 (the
Your-archives list + per-row switch), UX-3 (greyed unavailable rows stating state +
remedy; the switch reuses the guarded settings-save) and UX-2 (the wizard reads the
Desktop's `out`, breadcrumb reconciled) are **built** — catalog MA-664/665/666/667,
over the `internal/archindex` leaf (MA-653/657/660..663); full `go test ./...` +
covers-map green. UX-5/UX-6 land with **Slice 2** (the "Pause (make read-only)" /
"Resume" seal), which — with the recovery **scan** and **Slice 3** source-loss — is
still gated on its own §4.6 adversarial pass. Residuals filed for the implementation
friction review: the switch reuses `/api/settings`, which does not re-check
archive-ness, so a folder that goes unreadable between render and click could still be
set as `out` (TOCTOU; pre-existing, low severity — the dashboard then shows the
"isn't an archive" state); a refused or hostile index hides the card silently
(fail-closed, no "couldn't read your archive list" line yet); there is no sidebar nav
entry for the card yet (deferred to keep the MA-408 sidebar-resolve invariant
untouched); and the wizard shows the Desktop's `out` even when it last archived
elsewhere (per the UX-2 ruling that `out` is the one truth).
