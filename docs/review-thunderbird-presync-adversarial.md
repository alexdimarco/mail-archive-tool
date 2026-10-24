# Adversarial security pass — Thunderbird pre-sync (branch thunderbird-presync)

Run 2026-10-24, 6-agent adversarial workflow over the whole pre-sync change (wizard +
Desktop + CLI + adapter). Verdict **FIX-FIRST**. 13 confirmed findings, all MEDIUM or
LOW — none HIGH, none remotely exploitable (the MEDIUM confidentiality ones need a
shared-login computer where another account can write the archive directory). Grouped
by owner/scope below; each is a traced finding, not a hunch.

## Group A — the NEW Desktop/CLI create path (introduced this release; fix before ship)

- **A1 (MED) Untrusted last-run record read with no owner check on Windows.**
  `internal/desktop/localschedule.go` `lastRunJobArgs` uses `util.ReadSecureFile`, whose
  owner check (`Perm()&0o077`) is Unix-only — on Windows it accepts a record another
  account owns/wrote. Fix: route the install-path read through the job package's
  owner-checked reader (export a `job.ReadSecure` over `ownerOnly`/`winsec.PathWritableByOthers`).
- **A2 (MED) Crafted `-input` aims the scheduled backup at any mail store.**
  `reconstructLocalJob` validates inputs only by `os.Stat` (follows symlinks) + a lexical
  `_outlook-pst` check — not membership in the archive's own sources. A tampered record
  (A1) could point the victim's run at another store, written into the attacker-readable
  pinned archive. Fix: cross-check each `-input` against the manifest's local store roots;
  `Lstat`-refuse symlinks/non-regular; `EvalSymlinks` before the `_outlook-pst` compare.
- **A3 (MED) Job file written BEFORE the install guard.** `scheduleInstallLocal` does
  `job.Write` then `install`; on a refusal the job file is a decoy (`corroboratedRunJob`
  reports a schedule that was never installed), and the pre-written file defeats
  `CheckKind` trigger-2. Contradicts the method's own "nothing written on refusal."
  Fix: `install` first, `job.Write` only on success; add a real-install-failure refusal
  test asserting the job file is ABSENT.
- **A4 (LOW) `saveScheduleChoice` persists cadence on refusing paths + stores an invalid
  interval verbatim.** Fix: persist after install succeeds; `ParseInterval`-validate up front.
- **A5 (MED) CLI `-thunderbird-sync` setup failure fails the whole archive (R32).**
  `cmd/mailarchive/main.go` `return err` on `setPresyncHook` failure, unlike the GUI's
  `optsForJob` (swallow + proceed). Fix: route the CLI through `internal/presyncjob.SetHook`
  (one seam) and log-and-proceed; keep the parse-time unsupported-platform refusal.

## Group B — clear correctness/coherence (fix before ship)

- **B1 (MED) Windows idle-time mixes 64-bit uptime with 32-bit last-input.**
  `internal/presync/env_windows.go:324` `uint64(tick) - uint64(li.time)` overcounts after
  ~49.7 days uptime → the owner-active gate fails OPEN (quits/starts while the owner is
  active). Fix: `uint32(tick) - li.time` (or `GetTickCount`), + a realWin32 test.
- **B2 (LOW) Design doc still records hourly pre-sync as refused.**
  `docs/design-thunderbird-presync.md` §4.2.4/Q11 contradicts the 2026-10-23 owner ruling
  and the shipped code (MA-550). Fix: dated correction note; strike the hourly-refusal row.

## Group C — pre-existing pre-sync-adapter safety nuances (owner's call: fix now vs. track)

- **C1 (MED) S8 quits on a 2-minute physical-idle proxy** (`session.go:194`, `api.go:38`):
  a Thunderbird the owner is actively *reading* (no input 2 min) can be quit; hourly cadence
  makes this frequent — it softens the owner's hourly-safety rationale. Fix: raise the
  quit-gate idle threshold (~10 min), or skip the quit if our window became frontmost.
- **C2 (MED) Two schedules on one profile, different `-out`, not serialized**: one run's
  pre-sync can compact the store another is mid-read. Needs the deferred multi-schedule
  case. Fix: extend the per-profile lock over the read, or default `-copy-first` for TB.
- **C3 (MED) macOS quit is bundle-wide** (`env_darwin.go:51/427`): quits ALL Thunderbird
  though S8 proved only one pid is ours. Fix: skip the quit if >1 TB pid is present, or
  record a STATED reduction beside R31/S54/MA-570.
- **C4 (LOW) macOS profile path containing a space** can't be identified → started TB left
  running every run (`ownership.go`, `resolve_darwin.go`). Fix: reject whitespace or match robustly.
- **C5 (LOW) S4→S5 has no running-recheck + the −2s slack** can adopt an owner-launched
  `thunderbird -profile P` as ours and later quit it. Fix: recheck before `startHidden`;
  tighten the start-time slack.
- **C6 (LOW) `caffeinate` wake-lock is dead code** (defined + pinned, never invoked). Fix:
  wire it for the session, or delete it + unpin `/usr/bin/caffeinate`.

Full per-agent detail was in the workflow run `wf_1d0b025f-93c` (scratch, ephemeral).
