## The real blocker (rescue pass and critic): a schedule does not know its own kind

The design's upgrade story is "click Repair". Revision 1 never says *which* repair, and the code it builds
on cannot tell:

- Every surface installs an archive's backup under the one name the archive derives
  (`schedule.DefaultNameFor(out)`: the wizard in `offerSchedule`, the desktop in `backupSpecFor`) into the
  one descriptor beside the archive. A same-name install replaces the entry (`schtasks /F`, the cron upsert,
  the plist rewrite) and the descriptor. `refuseCollision` refuses only a verify over a backup and the
  reverse.
- Each surface's repair rebuilds *its own* kind. The wizard's `repairSpec` always writes `-job <job file>`
  (revision 1: `run-job`), whatever the descriptor records. Revision 1's dashboard Repair (§4.6.3) always
  re-installs `backupSpecFor` (`desktop -capture …`).
- After SE2, every pre-SE2 descriptor carries the `exe_former_name` note, both surfaces offer Repair on it,
  and §8.2 tells the owner to click it.
- So the prescribed walk converts schedules. A local Thunderbird archive's wizard schedule, repaired from
  the dashboard, becomes a Microsoft 365 capture that refuses "not configured" at 02:00. A Microsoft 365
  weekly backup, repaired from the wizard, becomes `run-job` on a job file that does not exist. The
  capture's start refusal writes no last-run record, so in the first case `status` stays GREEN until the
  run is twice the period late (`stale`): mail stops being archived and nothing says so for up to two
  periods. In the second case the run's failure toast fires every week, naming a job file the owner never
  made, and the breadcrumb behind it is read only by the wizard (SE-20).

Five findings are faces of this one defect (SE-01, SE-18, SE-21, SE-26, SE-39). Each rescue proposed a kind
check in a different place with a different taxonomy, and the critic's SE-C04 showed that the obvious fix,
reading the kind from the descriptor's `Job[0]`, hands an archive writer a lever: the descriptor is
archive-held and untrusted, so a forged kind could steer the owner's Repair into a schedule that refuses
every night. The mechanism has to be stated once, in one place, with the kind corroborated by state the
owner holds.

**Part of it ships today.** At `b1a16bc` the wizard's health check offers Repair whenever the recorded
program is not this one (`repairable`: `!in.ExeIsThis`), and `repairSpec` rebuilds `Args: ["-job",
jobPath]` whatever kind the descriptor records. On an archive the owner has scheduled from both the wizard
and MailArchive Desktop, today's wizard reads the desktop's `mailarchive-desktop` as "not this binary" and
offers to convert the weekly backup into a job-file schedule. This review verified that path in the code
(`cmd/mailarchive-gui/main.go`: `healthCheck`, `repairable`, `repairSpec`; both surfaces name the schedule
`DefaultNameFor(out)`). It is a confirmed-open gap of R18, independent of this design, and it needs an
operator-accepted catalog row and an xfail encoder now (OP-1).

Resolution: **SC1** (one schedule-kind mechanism in `refuseCollision`, one taxonomy, a repair that rebuilds
the kind it finds or refuses naming the surface that can, the kind corroborated by owner-side state) on top
of **SC2** (one composed identity predicate that says what the size and mtime fallback actually decides).
These two conditions carry the verdict.
