# Adversarial pass: the delete marker's slice 6 (f284689) and G3 (358931f)

Filed 3 November 2026. The pass's report is kept below as it was returned (process:
`../assurance-kit/process/adversarial-review.md`). How each finding was settled is in the section
"Settled" at the end, and in `docs/design-archive-delete.md` §14, "Slice 6 and G3's adversarial
pass".

---

Working under the Limen method. (Fresh instance; no name taken.)

VERDICT: NOT CLEAN. One medium finding (MED-1), no high. Three low findings and three notes for the
lead. Nothing I found needs an owner ruling. MED-1's fix has one effect a person could notice; it is
in the text below so the lead can decide whether to ask the owner.

The main problem in one line: the 30-day auto-pause count starts over every time the backup drive or
share is missing. So on a USB drive or share that is not always there (the setup the delete marker
was built for), the auto-pause almost never happens.

## Header (process template)
- Commit reviewed: 358931f, on branch switch-keeps-runjob. It covers slice 6 (commit f284689,
  MA-1058) and G3 (commit 358931f, MA-1060 to MA-1062).
- Short file IDs at that commit:
  - wizard.go: 7584243f70
  - wizard/pause.go: 06e8035309
  - capture.go: 3eb553e664
  - deleterefused.go: 023e926bf1
  - markerguard.go: 987022840d
  - cmd pause.go: 7be1110956
  - cmd main.go: b3a7918e1c
  - archivedelete.go: 03cbe2a924
- Who did what: one finder and one skeptic, both me, in separate passes. I read the design (sections
  15.8, 15.10 and 16) and the code before the builders' commit messages and tests.
- Tools: go 1.26.0 on darwin/arm64; Windows lab VM "Windows 11" (amd64 test programs).
- Totals: 15 attacks tried. 1 MED and 3 LOW confirmed, 11 refuted.
- Probes live only in the throwaway clone,
  /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/attack-s67.
  Nothing was changed or committed in the real repo, so this pass has no commit.
- Probe test files:
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/attack-s67/internal/wizard/zz_probe_s67_test.go
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/attack-s67/internal/desktop/zz_probe_s67_test.go
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/attack-s67/internal/desktop/zz_probe_s67_g3_test.go
- Probe scripts:
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/win-probe-s67.sh
    (Windows; runs from the VM's own folder and does not sync the shared lab source tree)
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/g3-probe.sh
    (command line, in a throwaway home folder; schedule is only previewed, nothing installed)
- Windows output folders:
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/win-s67
  - /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/win-s67b
- I did not write docs/review-delete-marker-slice6-g3-adversarial.md. The single synthesis owner
  should file it.

## MED-1 (integrity): the count starts over whenever the folder is missing, so the auto-pause rarely fires on a drive or share that comes and goes
- Where:
  - internal/wizard/wizard.go line 240: `autoPauseForDelete(path, j.Out, key, nil)` runs right after
    the delete check passes. That is before the "is the folder there" check at line 243.
  - internal/desktop/capture.go line 83: the same, for the Microsoft 365 capture.
  - When the folder is missing, the marker reads as absent (rule DM-22), so the delete check passes.
    NoteDeleteRefused(refused=false) then deletes the count file.
- Why it breaks the stated rule: slice 6 says it counts "by the same rule as an unreachable mail
  source" (the correction beside section 15.10 item 6, and the commit message). That rule leaves its
  count alone when a run says nothing either way (internal/app/sourceloss.go line 88: inconclusive
  means return; a run that stops because the folder is missing never reaches it). Slice 6 instead
  ends the count on any run that passes the check.
- Who it hits: the delete marker exists for a drive or share used by another computer or account. On
  a backup drive that is plugged in only some days, or a share another computer sometimes holds,
  each run while it is away restarts the count. So 30 days never pass, and the backup fails at every
  plugged-in run indefinitely. Owner ruling Q5 was meant to end exactly this.
- Reproduced on Mac and Windows, on both scheduled paths:
  - Setup: a marker this computer never began, and a count whose first refused run was 40 days ago.
  - One run with the folder renamed away: the count file is deleted.
  - The folder is put back and the next run is refused: count = 1 run, not due, nothing stopped.
  - Probe tests TestProbeS67_W1_AnAbsentFolderEndsTheCount (the local job) and
    TestProbeS67_C1_CaptureAbsentFolderEndsTheCount (the capture) fail with "PROBE ... CONFIRMED".
  - Control test TestProbeS67_W1twin_WithoutTheAbsentRunItPauses (same 40 days, no missing run
    between) passes: the job is stopped. This shows the test setup works.
- Suggested fix (a technical call, matching the "same rule"):
  - End the count only when the run found the folder there with no delete pending.
  - In RunHeadless (wizard.go), move the "ends the count" call below the "folder present" check. In
    HeadlessCapture (capture.go), end it only when the folder is there.
  - A missing or unreachable folder leaves the count alone. That is how the source-loss count treats
    a run that says nothing either way.
  - Tombstone tests: the two probes above, made into real tests with covers markers, red before the
    fix and green after.
- What a person would notice after the fix: a drive away for 30 days or more pauses on its first
  refused run after it comes back. The source-loss rule already does this (a weekly test pauses after
  2 runs 35 days apart). I judge it is inside the owner's "same 30 refused runs as an unreachable mail
  source". If the lead reads it as new behaviour, it is a question for the owner.

## LOW-1 (G3, paths): a -config ending in a slash makes the Desktop and the terminal read different deletion logs
- Where: the Desktop calls DeletionsLogFor(cfg.ConfigPath) with the path as typed (archivedelete.go
  line 276). The terminal calls DeletionsLogFor(abspath(FILE)) (cmd pause.go configLog;
  archivedelete.go lines 199 and 1170).
- Why: `X/` stays X in the Desktop's form, but making it absolute cleans it to X, and taking its
  folder then gives X's parent.
- Confirmed:
  - Mac: `/…/cfgdir/` → the Desktop reads `…/cfgdir/deletions.log`; the terminal reads
    `…/deletions.log`.
  - Windows: `C:\…\cfgdir\` → the same split.
  - Probe test: TestProbeS67_G1_TrailingSeparatorDrifts.
  - Command line: `pause -config "…/cfgdir/"` paused an archive whose delete is in
    cfgdir/deletions.log (probe P6). The hint it printed dropped the slash.
- Impact: only when a person gives the settings folder with a trailing slash. Then pause, resume and
  schedule miss a delete recorded only in a log, which per ruling Q1 means the archive is still whole
  or the folder is gone. A folder with the marker is still refused. delete-archive has drifted the
  same way since MA-1018. Section 16.5's "the verbs never read a different log from the delete that
  wrote it" is false for this spelling.

## LOW-2 (slice 6): a delete begun on this computer can still count, and the email then says "another computer"
- Where: wizard.go line 457. The local job always asks MailArchive's own log (""). NeverBegunHere
  only knows "begun here" from that log (markerguard.go lines 109 and 207).
- When it happens:
  - A delete begun from a Desktop started with -config, or by `delete-archive -config`, is not in
    that log. If a run-job (the local scheduled job) entry for the folder survives the delete's
    schedule stop, it counts.
  - The same happens after the person has removed deletions.log by hand (MA-1007's way out).
- Result: after 30 days that run-job's schedule is paused and the email says "begun on another
  computer or user account". Pausing does no harm (the backups were refused anyway). The words are
  wrong.
- This follows from section 16.6's stated limit ("runs ask MailArchive's own log"). But slice 6's
  claim "a delete this computer began never counts" does not hold here. Confirmed from the code only;
  no probe.

## LOW-3 (slice 6): an old count can survive and make a later pause come early
- Where: only the Desktop's schedule create and Remove clear the count (desktop/schedule.go line
  500). The wizard, `schedule -install`/`-remove` and removal in Task Scheduler or launchd do not.
- Scenario:
  - A count is running.
  - The schedule is removed outside the Desktop.
  - The marker is removed and the schedule is re-made, but no run gets past the check before a new
    marker appears.
  - The first refused run then sees the old start date and pauses at once.
- Narrow (several steps must line up), and it fails toward a pause that only stops the entry.
  Confirmed from the code only; no probe.

## Notes for the lead (not findings)
- N1: only two scheduled paths get the auto-pause (scope stated in section 15.10).
  - A command-line `schedule -install` entry (it runs `mailarchive -out … -unattended`) never
    auto-pauses, for source loss or for a delete.
  - A verify schedule is not stopped either (the builder's call).
- N2: "30 days, at least two runs" means a weekly schedule pauses after about 5 refused runs, or
  after 2 runs with a 30-day gap between them. This is the builder's call (b), already flagged.
- N3: the email's remedy, "click Delete… on the archive's row in Your archives", assumes the row is
  listed in the Desktop that is opened. For a wizard-made job, or a Desktop started with -config, the
  person may first need Open an archive folder… or that particular Desktop. The wording is the lead's
  to judge.

## Refuted (attack, and the evidence against it)
- R1. A planted marker pauses a schedule early, or stops another person's entry: no.
  - The count file sits beside this computer's settings, readable only by this user (mode 0600, read
    with ReadSecureFile). It is never in the folder.
  - A pause needs 2 or more refused runs at least 30 days apart. Removing the marker ends the count.
  - Only the refused run's own entry is stopped: the job file's name, or DefaultNameFor of the
    capture's -out. The name must come out of SanitizeName unchanged (wizard/pause.go line 190).
  - The planted marker blocking backups is the accepted ruling Q2.
- R2. The auto-pause writes into the folder or seals the archive: no.
  - stopLiveBackup only removes the scheduler entry: launchd's plist, the crontab block, or schtasks
    plus the side files beside the wrapper (schedule.go lines 1025, 1125, 1228 and 1254). None of
    these touch Out.
  - Nothing calls seal. FireForRun only sends mail.
  - The builders' tests check the folder is unchanged. Preview runs left ~/Library/LaunchAgents
    untouched (checked).
- R3. A delete MailArchive's own log knows (by folder or by run ID) counts: no. Tested in
  TestMA1058_ADeleteThisComputersLogKnowsNeverCounts.
- R4. A delete still running here counts: no. The archive lock means the run skips quietly before
  any count.
- R5. An unreadable log, a failed marker lookup, or a non-file at the marker's name counts: no.
  NeverBegunHere is false and the count ends.
- R6. The count is inflated: no.
  - Two jobs on the same folder share one key, and the rule counts by time, not by runs.
  - A clock set wrong is handled by the existing rule (MA-891).
- R7. -config reads both logs: no. configLog returns exactly one path. Probes P3 and P4.
- R8. -config misses a delete only the marker knows: no. The marker is asked first (UnfinishedAt).
  MA-1062 test.
- R9. -config leaks into the installed job, or is accepted after `--`: no.
  - Probe P1: the flat-form preview's job command has no -config.
  - Probe P2: after `--` it is refused: "job: flag provided but not defined: -config".
- R10. Relative or linked paths: no problem.
  - A relative -config is made absolute in the hint (P5).
  - A -config through a linked folder reads the same log (P7).
  - A missing folder is refused in MA-1024's words (P8).
  - Control characters in -config reach the terminal output: 0 escape bytes (P9).
  - Resume under -config is refused for a delete in that log (P10).
- R11. Windows case and drive spellings: the log is opened, not compared, so `C:\` and `c:\` name the
  same file. The trailing-separator split (LOW-1) is the only spelling drift found.

## Platforms
- Mac: the slice-6 probes and the G3 path probe were run alone (filtered with `-run TestProbeS67`).
  The command-line probes ran in a throwaway home folder.
- Windows lab VM: the same probe tests were built for Windows and run in the VM (outputs in the
  win-s67 and win-s67b folders). All findings reproduced the same way as on Mac.
- Linux: not run. These are path and logic findings; Linux uses the same code as Mac.
- No unfiltered suite was run: nothing in the real repo changed. None pending.

---
- Date worked on: 2026-11-03 (from the session's date).
- Drew on:
  - the design (docs/design-archive-delete.md sections 15.8, 15.10 and 16); the process file
    (../assurance-kit/process/adversarial-review.md);
  - the code at 358931f, listed above;
  - the builders' tests and commit messages, read after the code;
  - my own probe runs on Mac and the Windows VM. All of these I reached.
- Not checked:
  - LOW-2 and LOW-3 only from the code, no probe.
  - Linux not run.
  - No real scheduler entry installed or removed (a stand-in scheduler and preview only).
  - Whether a Desktop started with a folder-and-slash -config is used by anyone in practice.
  - The builder's MA-1062 Windows run, not repeated.
- Register: a finding report (adversarial review). The severity ratings are my judgement.
- To settle it yourself:
  - Smallest check for MED-1: run the two probe tests named above in the throwaway clone
    (`go test -run TestProbeS67 ./internal/wizard ./internal/desktop`). Each fails with "CONFIRMED".
  - The lead then decides whether "a missing folder leaves the count" is the same rule (my view) or a
    question for the owner.
- Counters: register = finding report. Borrowed lines = the design's and code's own quoted words,
  marked as quotes. Drift = none noticed. Touched = only the throwaway clone and scratchpad (no repo
  changes, no commits, no push). Persona = none. Sources reached: design yes; process file yes; code
  yes; Mac runs yes; Windows VM yes; Linux no.
- Under this file: I added the dated footer and the per-source "reached" counters, and marked my
  severity ratings as judgement. Nothing else changed in the work.

---

## Settled (3 November 2026)

Every finding was settled without a product ruling. MED-1 and LOW-1 were fixed test-first: each new
test was seen red at 358931f's code before its fix, then green, and every fix was proven by mutation
(M1 to M11, each alone, each red). The pass's probes became real tests with covers markers (no `zz_`
file is in the repository). Full detail: `docs/design-archive-delete.md` §14, "Slice 6 and G3's
adversarial pass".

On MED-1's one noticeable effect (a drive away 30 days or more pauses on its first refused run after
it comes back): read as inside the owner's ruling Q5, "after the same 30 refused runs as an
unreachable mail source". The source-loss rule leaves its streak on a run that says nothing about
the source (`app.updateSourceStreak`, inconclusive returns), and pauses the same way after a long
gap. Not sent to the owner as a new question.

| ID | Settled | Test |
|---|---|---|
| MED-1 | Fixed (MA-1063; MA-1058 amended). The local job ends the count only after its "folder present" check; the capture only when the folder is there at its early check, and after the engine only for the engine's `delete_unfinished` refusal. A run with the folder absent leaves the count. The pass also showed (mutation M4) that the capture's count after the engine had no covering test; the new test covers it. | `TestMA1063_AnAbsentFolderLeavesTheRunJobsDeleteCount`, `TestMA1063_AnAbsentFolderLeavesTheCapturesDeleteCount` |
| LOW-1 | Fixed (MA-1064; MA-1062 amended). `configAbs` keeps a typed trailing separator at every terminal `-config` site (`pause`, `resume`, `schedule`, `delete-archive`, `unlist-archive`, `add-archive`, the folder check, the hints). §16.5 corrected beside its third bullet. | `TestMA1064_AConfigWithATrailingSeparatorReadsTheDesktopsLog` |
| LOW-2 | A stated limit (§15.6, and §14's note). The pause does no harm; the email's "another computer" is wrong for a delete begun from a `-config` Desktop or after `deletions.log` was removed by hand. Telling them apart needs the local job to read another log, which §16.6 and the ruling on G3 leave out. | none (no code change) |
| LOW-3 | A stated limit (§15.6, and §14's note). Narrow; it fails toward a pause that only stops this computer's entry, with the email. Clearing the count from every schedule path touches three systems' schedule code for that case. | none (no code change) |
| N1 to N3 | Notes for the lead, recorded in §14's note; no change. | none |
