# Friction review — MailArchive Desktop weekly-backup schedule (slice S4)

**Reviewed:** the operator ceremony of turning on / changing / turning off the weekly
backup from the dashboard, walked as the actor it exists for — a **non-admin mailbox
owner** who has signed in (S2) and wants an unattended weekly archive of their own M365
mailbox. Built code: `internal/desktop/schedule.go` (`backupSpec`, `scheduleInstall`,
`scheduleRemove`, `scheduleState`) over `internal/schedule` (`Install`/`RemoveIfInstalled`/
`Query`, cross-platform schtasks/cron/launchd). Classification per
`../assurance-kit/process/friction-review.md`: **Type I** (inherent — document it) vs
**Type II** (design-choice — fix it). Builds on `review-graph-delegated-friction.md`
(the scheduled-delegated-run frictions FR1/FR4 there carry).

## The walk

1. Sign in (S2) and choose an archive location + a weekly day/time in Settings.
2. Turn on the weekly backup → `POST /api/schedule-install` (loopback + CSRF guarded):
   it saves the settings, then `schedule.Install(backupSpec())` writes a Task Scheduler
   entry **`mailarchive-<hash>`** (one per archive) that runs *this* binary's
   `mailarchive-desktop.exe -capture -config … -out … -log …`.
3. The Overview **weekly backup** card flips to *Installed* (`schedule.Query`), showing the
   day/time read back from the descriptor.
4. The task fires on schedule **with the dashboard closed**, re-reading config + settings +
   the saved sign-in token at run time, and captures headless (DC1).
5. Turn it off → `POST /api/schedule-remove` → `RemoveIfInstalled`.

Steps handled: **0 out-of-band transfers**, **1 interactive step** (choosing day/time),
the heavy lifting (Windows wrapper quoting, descriptor, log rotation, three-state detection)
reused from `internal/schedule` unchanged. The schedule never holds a credential — it reads
the vault/file token at run time.

## Findings

| # | Type | Point | Disposition |
|---|---|---|---|
| FS1 | II (FIXED pre-code, DC1) | A weekly backup that drove the open dashboard would miss when the dashboard is closed at 3am | Fixed by DC1: the task runs this binary's engine-**headless** `-capture`, independent of the dashboard UI process. Works whether or not the dashboard is open. (MA-278/279) |
| FS2 | II (FIXED, structural) | A later settings change (location / time / Deleted-Junk) silently diverges from what the installed task runs | Fixed: `backupSpec` passes only `-config`/`-out`/`-log`; `-capture` re-reads settings + token at **run time**, so a settings change is honored without reinstalling. Grounded in the `Args` the task carries. |
| FS3 | II (FIXED, S2) | A scheduled run blocks forever on a device prompt no one answers | Fixed in S2/DC1: the headless `-capture` runs `Unattended=true` and **refuses** with the sign-in remedy when there is no usable token — it never reaches a prompt (MA-278; graph-delegated FR2). |
| FS4 | I | The schedule runs as the OS user who signed in, because only that user's vault/`0600` file holds the token | Documented (graph-delegated FR1): sign in as the account the backup runs as. The dashboard installs the task for the current user, whose vault holds the token — consistent by construction. Inherent to per-user delegated tokens. |
| FS5 | I | The sign-in expires (~90-day refresh inactivity, password change, admin revoke) and a later scheduled run fails | Documented: the capture log + the Status & health view surface the auth failure with a "sign in again" remedy (graph-delegated FR4); the run refuses cleanly, does not hang (FS3). Inherent to delegated tokens. |
| FS6 | II (CONFIRMED, LOW — accepted with note) | **Changing the archive location after installing leaves the old task behind.** The task name is `DefaultNameFor(out)` (per-archive hash); install against a new location creates `mailarchive-<newhash>` while `mailarchive-<oldhash>` stays installed, still backing up the old path | Accept with a documented note. It is not data loss — the old archive simply keeps its own weekly backup — but it is surprising. Per-archive naming is deliberate (so a second archive never overwrites the first's schedule). A future nicety: on a location change the dashboard detects and offers to remove the prior-location task. Flagged, not auto-fixed (would silently delete a schedule the user may still want). |
| FS7 | II (LOW, accepted) | The task name `mailarchive-<hash>` is opaque in Task Scheduler | Accept (graph-delegated FR7): derived per archive so two archives don't collide; the dashboard shows the human-legible state, and `README-RMM.md` D.3 names the `mailarchive-` prefix for operators who look. |
| FS8 | II (LOW, accepted) | On a platform with no scheduler, "turn on" could look broken | Accept: `scheduleState` reports `schedulerAvailable:false` / *Scheduler unavailable* (three-state `Query`), a legible state rather than a hard error. |

## Verdict

**PASS (shippable).** The two frictions that would have bitten — the dashboard-closed miss
(FS1) and the settings-drift (FS2) — are resolved structurally (engine-headless task +
run-time re-read), each with a covering test (MA-278/279). FS3/FS4/FS5 are the delegated
frictions already dispositioned, carried unchanged. One substantive Type-II residual remains:
**FS6**, a changed archive location leaves the prior-location task installed — accepted as a
LOW, no-data-loss surprise with a documented note and a suggested future nicety, deliberately
not auto-fixed (silently deleting a user's schedule is worse than the surprise). The remaining
items are LOW niceties or legible-by-design. The schedule ceremony is one choice (day/time)
over an already-completed sign-in.
