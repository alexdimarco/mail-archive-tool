# Design: the capture kind guard and Resume routing (part B of the Settings card work)

**Status: revision 1, 2026-10-28; pre-code, nothing built.** Branch `settings-card`. Split out of
`docs/design-desktop-settings-card.md` revision 3 (its section 4, 4.1 to 4.3, slice 1, MA-865 and
MA-873) by the owner's ruling of 2026-10-28. Folds the re-review conditions R3C-1 to R3C-9 from the
"Re-review of revision 3" section of `docs/review-desktop-settings-card-predesign.md`, each marked
"(R3C-n)". Part A (the Settings card design, revision 4) is built first and does not depend on this.

**Decision:** a Microsoft 365 capture counts against another kind's install only when one is actually
installed for this archive, or recorded and paused; Resume brings back the kind that was paused. This
is what lets a local-mail archive get its own scheduled backup on a computer where Microsoft 365 is
set up (ruling 2 below).

Labels: **proven** = read in the code at the cited line on this branch (read, not run);
**conditional** = holds only under the condition beside it; **aspirational** = what this design makes
true once built, pinned by a row in section 11.

## 1. Owner rulings this rests on

- **Ruling 2 (settings card design, section 1):** a local-mail archive gets its own scheduled backup
  and Thunderbird sync even where Microsoft 365 is set up; an archive is judged by its own record.
- **OR-1, accepted:** a capture counts only when one is actually installed.
- **2026-10-28:** the owner accepts amending the single-executable design's §4.6.5(b) ("no launchd or
  Task Scheduler entry is ever read back", `docs/design-single-executable.md:1266-1268`) and MA-631
  (`TestMA631_CaptureCorroborates`) so the install guard asks the scheduler whether the entry exists.
  **To be re-ratified at the security pass**, which this design owes before it merges (section 12).
- **2026-10-28, the split:** this part is reviewed on its own; until it lands, part A tells the owner
  plainly on the card that such an archive can't get its own schedule yet.
- **2026-10-28, R3C-15 answered yes:** Hourly is offered for every local-mail backup (a run-job), a
  mixed archive's included; Microsoft 365 captures are never hourly. Here that means Resume never
  re-installs an hourly capture (section 6).

## 2. What is wrong today (proven by reading)

- `CaptureCorroborates` (`internal/desktop/kindcorroborate.go:26-45`) counts a capture whenever
  Microsoft 365 is set up (a tenant and client id, `:36-38`) and `out` is the current folder or
  BackupFor (`:39-44`). It never asks whether a capture is installed. `BuildKindEvidence`
  (`internal/schedule/kindevidence.go:44-57`) passes that to `CheckKind` (`:78-91`), which refuses any
  other kind (`:82-86`). So on such a computer the current archive can never get a run-job.
- One seam, `CaptureCorroborator` (`kindevidence.go:38`), feeds both the guard (`schedule.go:646`)
  and eight readers (`health.go:1666,1848`, `dashboard.go:1029,1095`, `localarchive.go:112`,
  `desktop/schedule.go:146,243,333`) (R3C-7).
- The wizard writes the job file (`wizard.go:1761-1768`) before it installs (`:1771`); today's
  always-true rule is what refuses it beside a live capture (R3C-1).
- Pause and auto-pause remove the entry and keep the descriptor (`pause.go:27-38,92-107`); Resume
  (`pause.go:72-80`, `wizard.go:1083-1101,1118-1136`) always rebuilds a run-job `-job <name>.json`.

## 3. The rule

### 3.1 Two predicates, two seams (R3C-7)
- **Capture configured** = today's `CaptureCorroborates`, minus `out` having a securely readable
  default-named job file whose Out is `out` (the same job-file test as 3.3, R3C-6). It stays on the
  existing `CaptureCorroborator` seam and keeps feeding the readers above: `RepairRoute`
  (`internal/schedule/repairroute.go:25-36`), the card's kind line, Status's remedy
  (`health.go:811,947-966`) and the wizard's `repairable` (`wizard.go:1034-1044`).
- **Capture installed** = 3.3's rule, on a **second seam** `CaptureInstalled`, set in the same `init`
  (`kindcorroborate.go:18`) and read **only** by the guard (`refuseCollision` then `CheckKind`,
  `internal/schedule/schedule.go:641-646`). `BuildKindEvidence` is unchanged for every reader.

### 3.2 Names (R3C-8)
The names N are `DefaultNameFor(out)`, plus `DefaultNameFor(effectiveOut)` and
`DefaultNameFor(BackupFor)` whenever `out` is the same archive as either under `SameArchivePath`.
`DefaultNameFor` hashes the path as spelled (`descriptor.go:324-330`), so a case alias or a symbolic
link to the current folder is asked under every spelling the program knows.

### 3.3 Capture installed, for the guard only
> A capture is installed for `out` when Microsoft 365 is set up, **`out` is the current folder or
> BackupFor under `SameArchivePath` (R3C-2)**, and for some N: (1) `schedule.Query(N)` reports
> Installed or SchedulerUnavailable (fail closed); (2) `job.PathFor(N)` is not a securely readable
> job file whose Out is `out`; and (3) on cron hosts, N's managed block says capture; there the block
> decides and (2) is not asked (`kindevidence.go:142-162`).

- **An old BackupFor that names a run-job is not a capture (R3C-5).** For the BackupFor name, if
  `job.PathFor(N_BF)` reads securely with Out = BackupFor, that name is the old folder's run-job; the
  guard, `backupTask` and `removeTasks` ignore it. (Part A's "Save and keep" stops creating these; this
  covers settings files that already hold one.)
- **A paused or auto-paused capture still blocks (R3C-3).** Add-only refusal, like trigger (2)
  (`kindevidence.go:87`): Microsoft 365 set up, `out` the current folder or BackupFor, and the
  descriptor records a capture, so an install of another kind is refused even with no live entry.
- **"Can't tell" refuses everywhere (R3C-1).** A descriptor recording a capture beside a securely
  readable default-named job file is "can't tell" in the guard as well as in every reader: an add-only
  refusal of any install other than a same-name remove. The record never picks a kind; it only adds doubt.
- **Why BackupFor:** a capture kept through "Save and keep" is named for the old folder
  (`internal/desktop/schedule.go:38-50`) but writes into the current one (`capture.go:46-47`); asking
  only `DefaultNameFor(out)` would allow two schedules on one archive (proven by reading).
- **Tie-breaker = the default-named job file for N only**, not `corroboratedRunJob`'s folder scan
  (`kindevidence.go:112-136`), so a job file under another name never hides a live capture.

### 3.4 How the scheduler is asked, and what is read (R3C-7, R3C-9)
- **macOS:** whether the plist file exists; nothing inside it is read. **Windows:** `schtasks /Query`
  (`installed.go:40`), with no hidden-window flag today (`schedule.go:1011-1013`), so a console window
  may flash in the windowed build (**conditional**; the no-window flag is added if seen at build).
  **Cron:** `crontab -l`, and the managed block's **command line is read** to learn its kind
  (`kindevidence.go:142-162`), so an export block no longer counts as a capture.
- **Fail closed, with limits:** a macOS stat error other than "does not exist" counts as unavailable.
  Windows uses one exit for "not found" and other errors (`installed.go:83-94`), and any `crontab -l`
  exit other than "no crontab" reads as NotInstalled (`installed.go:61-62`): **both stay fail-open**
  (section 7).
- **Testable on every OS (R3C-7):** `Query` picks its branch from the machine it runs on
  (`installed.go:34-45`). The guard calls a new `queryOn(name, goos)` over the existing `launchdState`
  and `schtasksState`, so MA-880 drives the macOS and Windows branches on every CI leg, faking only the
  scheduler (a plist under a temporary home, the `schtasksRun` / `crontabList` stand-ins), never the
  `InstallBackup` seam.

### 3.5 What holds (proven by reading, from the revision-3 rescue check)
A fresh capture install is unaffected (`CheckKind` refuses only another kind, `kindevidence.go:82-86`);
a first command-line export or run-job away from the current folder is no longer wrongly refused;
planting a job file needs the owner's write access; scheduled captures never ask the corroborator
(`capture.go:46-113`), so the exposure is Install, Resume and Repair.

## 4. The wizard cannot slip past the guard (R3C-1)
Before the guard change merges, `offerSchedule` installs first and writes the job file last, the
Desktop's A3 order (`localschedule.go:188-203`), so its own new file never counts as "no capture". A
job file left behind by an earlier refused wizard install hits 3.3's "can't tell" refusal when the
descriptor records a capture, and otherwise passes test (2) only if no entry exists under N.

## 5. One decision table for broken and doubtful states (R3C-6)

Both rules use the same job-file test: securely readable, Out = `out`. "M365" = Microsoft 365 set up
and `out` the current folder or BackupFor. A descriptor recording a run-job never takes the capture route.

| Descriptor | Default-named job file | Entry | Card label / buttons | Repair route | Save route | Status remedy |
|---|---|---|---|---|---|---|
| run-job | readable | any | scheduled backup of mail files; Run now, Repair, Remove | `repairRunJob` (job file untouched) | run-job edit | Repair on the card |
| run-job | missing | any | "its settings file is missing"; Repair, Remove | create path; reply says Thunderbird sync is now off, switch it on in Settings | create | Repair on the card |
| any | unreadable | any | the unreadable-file message (ahead of "can't tell"); Remove | none | refused | re-create it in the MailArchive wizard |
| capture | readable | any | "can't tell which kind: Remove it, then set it up again in Settings"; Remove | none | refused | Remove, then Settings |
| capture | missing | present, M365 | Microsoft 365 capture; Repair, Remove | capture Repair (descriptor's cadence) | capture edit | Repair on the card |
| capture | missing | absent, M365 | paused: Resume; otherwise "not installed"; Repair | capture Repair | refused if paused | Resume or Repair |
| capture | missing | any, not M365 | "a Microsoft 365 capture, but Microsoft 365 isn't set up here"; Remove | none | refused | Remove |
| none | any | present | "a scheduled entry with no record"; Remove | none | refused | Remove |

Remove renders whenever the scheduler has the entry, even with no descriptor (R3C-6). Rows pinned by MA-882.

## 6. Resume routes by kind, in the same slice (R3C-4)
Resume installs the rebuilt run-job only when `job.PathFor(name)` reads securely with Out = this archive.
It re-installs a **capture** only when all hold: the descriptor records a capture, `backupKind` is
Microsoft 365, the archive is the **current folder** (not BackupFor), and the cadence is not hourly. It
goes through the card's install path, `backupSpecFor` with the descriptor's interval and time, including
the BackupFor clean-up (`desktop/schedule.go:177-183`). Every other case makes the archive writable,
keeps the record, installs nothing and says so; for a BackupFor folder: "The archive is writable again;
set its backup up again in Settings."

**Hook:** `ResumeArchive` (`pause.go:72-82`) is in `internal/wizard`, which cannot import
`internal/desktop`. So `ResumeArchive` itself installs only a run-job whose job file reads securely,
and otherwise installs nothing and says so; the command line (`cmd/mailarchive/pause.go:48`) therefore
never installs a capture. The Desktop's Resume handler (`dashboard.go:724`) routes first: for a recorded
capture meeting the test above it makes the archive writable, then installs through the card's path;
otherwise it calls `ResumeArchive` — **aspirational** (MA-881).

## 7. Residuals (for the adversarial pass)
- **Export re-install in the current folder (R3C-2):** off cron, with Microsoft 365 set up, a
  command-line export's own entry has no job file and counts as a capture, so re-installing that export
  is refused (fail closed, nothing replaced). Not fixed by trusting a descriptor that says export.
- **Fail-open:** Windows (one exit code) and cron (any `crontab -l` failure) read errors as NotInstalled.
- **Not built:** §4.6.5(d)'s delete of a name's job file on a capture or export install
  (`docs/scenario-catalog.md:937`), and the wizard pre-check that MA-631 and
  `design-single-executable.md:1302-1303` promise (R3C-1, R3C-9); section 4's order replaces the
  pre-check, and the text is struck (section 8).
- **Pre-v1.0 "both surfaces scheduled" archives** (`design-single-executable.md:1316-1320`) resolve to
  run-job when the default-named job file exists, unless the descriptor says capture ("can't tell").

## 8. Text and tests that change (R3C-7, R3C-9)
- **§4.6.5(b)** (`design-single-executable.md:1266-1268`) gains: "For the install guard only, the
  scheduler is asked whether an entry exists under each name: a plist file's presence on macOS (nothing
  inside it read), `schtasks /Query` on Windows, and on cron the managed block, whose command line is
  read for its kind. Windows and cron read a query error as not installed." "Before any scheduler call"
  is dropped, and the unbuilt wizard pre-check claim is struck. Re-ratified at the security pass.
- **MA-631** (`scenario-catalog.md:937`) says the same, per OS. With the second seam (3.1),
  `CaptureCorroborates` keeps the configured rule, so `ma631_capture_test.go:48` **stays green**
  (**conditional** on that placement), and a new guard test is added; `kindevidence_test.go:54` stays
  green. The ma633 tests build evidence by hand; MA-880 adds the configured-capture Repair case.

## 9. What changes on part A's card when this lands
Part A's notice for a local archive on a computer with Microsoft 365 set up goes away; that archive
gets the Local fields, Hourly included, and Save installs a run-job. Part A's record-against-installed
table gains the rows of section 5, and its "Capture" and "Command-line export" columns merge into
"status and Remove" where they can't be told apart off cron (R3C-2). Part A's §6 refusal "Local archive
where Microsoft 365 is set up" becomes `CheckKind`'s capture refusal in plain words.

## 10. Build slices (after part A's slices A3 and A5)
- **B1, guard, wizard order and Resume, one slice:** sections 3 to 6 and the section 8 text, with
  MA-880 to MA-883, prove-fail then prove-pass. Resume ships with the guard: under the new rule alone, a
  resumed capture would become a run-job pointing at a missing job file.
- **B2, part A's card switch:** section 9 and part A's MA-857 cases extended.

## 11. Test-spec rows (reserved range MA-880 to MA-889)
Part A owns MA-856 to MA-879. Revision 3's MA-865 and MA-873 are **retired, never reused**; their
content is MA-880 and MA-881. MA-884 to MA-889 are reserved. All unit rows (U).

| Row | What it proves |
|---|---|
| MA-880 | (was MA-865) The real `refuseCollision` then `CheckKind`, Microsoft 365 configured, only the scheduler faked per OS through `queryOn`: no entry, so a local archive installs a run-job (positive twin); an entry and no default-named job file refuses a run-job install with nothing written; SchedulerUnavailable refuses; entry plus default-named job file is a run-job, a job file under another name is not; a capture named for BackupFor blocks a run-job into the current folder; a BackupFor whose name holds the old folder's run-job does not; a paused and an auto-paused capture block another kind; a case alias or symbolic link to the current folder is asked; an export re-install away from the current folder passes; on cron the block decides; a configured capture with its entry missing still routes Repair to the card |
| MA-881 | (was MA-873) Resume by kind: a paused run-job with its job file resumes as a run-job (positive twin); a recorded capture in the current folder resumes as a capture, writes no `-job` entry, keeps the record's kind; a run-job descriptor with a missing job file, or an unreadable one, an hourly capture descriptor, a Local archive with Microsoft 365 set up, a BackupFor folder, and the command line each make the archive writable, install nothing and say so |
| MA-882 | Every row of section 5's table: label, buttons, Repair and Save routes and Status remedy; Remove with no descriptor; unreadable ahead of "can't tell"; a missing-file Repair says sync is off |
| MA-883 | Wizard order: a run-job install into a folder with a live capture is refused, entry and jobs folder unchanged (positive twin first); a leftover job file already on disk beside a recorded capture is refused |

## 12. Gates owed before this merges
- **Pre-code review of its own** (`../assurance-kit/process/design-review.md`), then a fold check;
  a rescue-style check of the folded rule is advised (the re-review's own note).
- **Adversarial pass** (`../assurance-kit/process/adversarial-review.md`) on B1 before ship. Targets:
  section 7, R3C-1 to R3C-5, the Resume hook, the "can't tell" refusals.
- **Security pass** that re-ratifies the §4.6.5(b) and MA-631 amendment (section 8) **before B1 merges** (R3C-17).
- **Friction review** of B2, with a live Chrome check of the card on a computer with Microsoft 365 set up.

---
Revision 1, 2026-10-28. Re-read for it: `internal/desktop/kindcorroborate.go`,
`internal/schedule/kindevidence.go`. Other citations are carried from revision 3 of the settings card
design and from the re-review, not re-read. Nothing run.
