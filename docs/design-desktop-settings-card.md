# Design: the Settings card edits this archive's backup options, by kind

**Status: revision 4, 2026-10-28: part A only, after the owner's split of 2026-10-28; folds the
re-review conditions R3C-10 to R3C-17.** Branch `settings-card`; pre-code, nothing built. Review of
record: `docs/review-desktop-settings-card-predesign.md` (GO_WITH_CONDITIONS, and its "Re-review of
revision 3"). Folded conditions are marked "(C-n)" and "(R3C-n)". The capture kind guard and Resume
routing (revision 3's section 4, slice 1, MA-865, MA-873, and R3C-1 to R3C-9) moved to part B,
`docs/design-capture-guard.md`, reviewed on its own (section 4).

**Revision 4.1, 2026-10-28:** the fold check's fixes of revision 4 and two owner rulings of the same
day, each marked "(rev4.1)". Nothing built yet.

**Decision:** Settings becomes the one place to change how this archive is backed up (how often, time,
Thunderbird sync), showing only what applies to its kind; the backup card keeps status plus Run now /
Repair / Remove. Live bugs ship first; every Desktop button shows it was pressed. Part A gives a
local-mail archive its own schedule wherever that is possible today, which is a computer where
Microsoft 365 is **not** set up; elsewhere it says plainly that it can't yet (section 4).

Labels: **proven** = read in the code at the cited line on this branch (read, not run); **conditional**
= holds only under the condition beside it; **aspirational** = what this design makes true once built,
pinned by a row in section 13.

## 1. Owner rulings (settled)

1. Settings is the **one editor** of backup options; the backup card says "Change in Settings".
2. A local-mail archive gets its own scheduled backup and Thunderbird sync **even where Microsoft 365
   is set up**: an archive is judged by its own record, not the computer's. **Delivered by part B**
   (`docs/design-capture-guard.md`); until then part A says so on the card (section 4).
3. Local archives show "keep .eml" **read-only**; the Microsoft 365 boxes show only for Microsoft 365.

- **2026-10-28, the split:** part A (this design: slices A1 to A6, none touching the capture kind guard
  or Resume) is built now; part B (the guard, C-1, OR-1, the §4.6.5(b) and MA-631 amendment, and Resume
  routing) is its own design, reviewed on its own.
- **OR-1 accepted** and the §4.6.5(b) / MA-631 amendment accepted: both now live in part B.
- **OR-2, as answered 2026-10-28 (R3C-15: yes):** Hourly is offered for **every local-mail backup (a
  run-job)**, a mixed archive's included; **Microsoft 365 captures are never hourly**. Failure alerts go
  once per failing streak, plus one recovery email.
- **OR-3 accepted:** amend MA-583, MA-617, MA-523 and the pinned test strings, re-pointed to the new
  card, nothing weakened (section 12). **OR-4** (downgrade guard) not taken up.
- **2026-10-28:** the source-unreachable **warning shows after 3 days** unreachable and **auto-pause at
  30 days**. The owner accepts that a weekly backup failing once, then the computer off for a month,
  auto-pauses at the next failed run. MA-686/687/688 and the archive-manager §5.7 S3-C3 text join
  OR-3's accepted amendments.
- **R3C-9, second half, accepted by the owner 2026-10-28 (rev4.1):** the elapsed-time and
  once-per-streak rulings also change MA-690, MA-693, MA-698 and MA-702 and their tests (section 12);
  the owner accepted amending them, nothing weakened. (Revision 4 listed them as owed.)
- **2026-10-28, a live bug fixed in part A (rev4.1):** a BackupFor already in the settings file that
  names a run-job's folder lets Remove on another archive delete that run-job's scheduler entry
  (`removeTasks`, `internal/desktop/schedule.go:283-299`, proven by reading; the capture install's
  clean-up at `:177-182` does the same). Part A fixes it with a one-time clean-up that clears such a
  BackupFor: **BackupFor is kept only for a capture** (section 8).
- **Queued owner item:** "When pressing buttons on the webpage there still is no obvious indication
  the button is pressed and work is happening" (section 11).

## 2. What is wrong today (proven by reading, except where marked)

- The three Microsoft 365 boxes show for every archive (`internal/desktop/dashboard.go:1379-1381`), are
  **computer-wide** (`settings.go:25-27`, `launch.go:194`) and feed the capture and Archive now
  (`capture.go:81,87,247-250`); "they do nothing for a local archive" is **conditional** on Microsoft 365
  not being set up (C-16).
- The sync box sits on the "Weekly backup" card, only for a "local only" archive on macOS or Windows
  (`dashboard.go:1044,1059,1471-1472`). "Local only" is false whenever a tenant is saved
  (`localarchive.go:93-106`), so a Thunderbird archive goes the capture way (`schedule.go:137-140`),
  where the guard always counts a capture (`kindcorroborate.go:36-44`) and refuses a local install.
  **Part B fixes this**; part A states it (section 4).
- An edit rebuilds the whole job file from the last-run record (`localschedule.go:145,200`), changing
  mode, raw and the start date (`canonical.go:45-49,69-70`, `run.go:1141-1162`); an unhonourable sync
  request is dropped silently (`localschedule.go:153`). The menu lacks Hourly (`dashboard.go:1468`).
- Cadence leaks: a local install writes the computer-wide settings file (`localschedule.go:204`) that
  the capture reads (`schedule.go:80-91`), and the capture writes it before Install can refuse
  (`schedule.go:152` before `:171`).

## 3. The card, per kind

### 3.1 The kind, from the archive's record
A new helper `backupKind(out)` reads the archive's record, never the tenant:

| Kind | When |
|---|---|
| Microsoft 365 | `archiveKind` is `m365`, `mixed` or `empty` (`localarchive.go:30-63`) |
| Local | `local` (`localarchive.go:58-59`); "from Thunderbird" by owner-held evidence (below) |
| Encrypted | `encrypted` (`localarchive.go:36-37`); backstop `refuseEncrypted` (`internal/schedule/encrypted.go:23`) |
| **Unknown** | the record can't be read (`kindNone`, `localarchive.go:39-46`), e.g. its drive is unplugged (C-8) |

**Unknown** shows the location only, plus "MailArchive can't read this archive's record right now (is
its drive connected?); its backup options show when it can." `api/schedule-install` refuses it before
any write (C-8), per the code's rule "nothing is hidden or disabled on a guess" (`localarchive.go:28-29`).

`backupKind` replaces `localOnly()` in install routing (`schedule.go:137`) and the backup card's state
(`dashboard.go:1030-1074`). The **backup tile** (`dashboard.go:628-641`) only gets a new label; the
**Microsoft 365 tile** (`:621`), Archive now (`localarchive.go:141`), the heading (`dashboard.go:1262`),
the connection card (`:1317-1325`) and Status's "New mail" line (`maintenance.go:198`) keep
`localOnly()` (C-15) — **aspirational** (MA-856). Routing by `backupKind` changes which form shows and
which install path a post takes; it does not change the guard (`refuseCollision` then `CheckKind`),
which every install still passes, unchanged in part A.

**The record is untrusted (C-17).** It sits inside the archive and is read with `state.Load`, without an
owner check (`localarchive.go:43`). It only picks the form and the path; every write still passes
`reconstructLocalJob`'s A1/A2 checks (`localschedule.go:75-101`) and today's kind guard, and once a
schedule is installed the owner-side evidence (`BuildKindEvidence`, `kindevidence.go:44-57`) decides.
"From Thunderbird" reads the owner-checked last-run record (`lastRunJobArgs`, `localschedule.go:23-35`).
Network (UNC) and non-local paths are skipped before any file check; drawing the page never waits on
source storage — **aspirational** (MA-869).

### 3.2 Record kind against what is installed (C-7)

"Capture counted" means today's owner-side evidence counts a capture for this archive: Microsoft 365
is set up and the archive is the current folder or BackupFor (`kindcorroborate.go:36-44`), the same
`BuildKindEvidence` the card already reads (`dashboard.go:1029`). The columns come from owner-side
evidence first; a descriptor's kind is used **for display only**, never to route a write.

| Record kind | None installed | Capture | Run-job | Command-line export |
|---|---|---|---|---|
| Microsoft 365, no capture counted (Microsoft 365 since removed) | M365 fields, "No scheduled backup" | status and Remove | Local fields, Hourly offered; run-job edit | read-only: "set up at the command line"; Remove |
| Microsoft 365, capture counted | M365 fields, "No scheduled backup"; Save installs a capture (no Hourly) | M365 fields; capture route | status and Remove, plus the section 4 notice | status and Remove |
| Local, no capture counted | Local fields, "No scheduled backup"; Save installs a run-job | status and Remove | Local fields; run-job edit | read-only: "set up at the command line"; Remove |
| Local, capture counted | the section 4 notice; no schedule inputs | status and Remove, plus the notice | status and Remove, plus the notice | status and Remove, plus the notice |
| Encrypted | location only | as today (status, Remove) | as today | as today |
| Unknown | location only | status and Remove only | status and Remove only | status and Remove only |

Where a schedule is installed, the form follows **its** kind: Hourly is offered whenever the form edits
a run-job, a mixed archive's included (R3C-15), and refused only for a capture (3.7).

The server refuses a posted cadence that differs from what it would install or repair; it never answers
ok:true while the schedule stays unchanged (today's silent path, `schedule.go:146-147,201-226`) —
**aspirational** (MA-860). **Archive now on a Local archive where Microsoft 365 is set up** stays
offered; a confirm first says: "Archive now adds Microsoft 365 mail to this archive. It then counts as
mixed and shows the Microsoft 365 options; a scheduled backup of its mail files keeps running." (C-7)

### 3.3 Fields

| Field | Microsoft 365 | Local | Local from Thunderbird | Stored in (the one source) | Takes effect |
|---|---|---|---|---|---|
| Archive location | edit | edit | edit | settings `out` (`settings.go:24`) | at once (`settings.go:211-216`) |
| How often: No scheduled backup / Hourly / Daily / Weekly (Sunday) | edit, **no Hourly for a capture** (R3C-15) | edit | edit | descriptor + the computer's scheduler | on Save (3.6) |
| At (hourly uses the minutes, `schedule.go:56`) | edit | edit | edit | same | same |
| Sync Thunderbird first | hidden | shown if the job file carries it (C-10) | edit, macOS/Windows | job file `thunderbird_sync` only (`job.go:47`) | on Save |
| Keep original .eml | edit | read-only | read-only | M365: settings `keepRaw`; local: job `raw` (`job.go:38`), else last-run `-raw` (`canonical.go:69-70`) | M365: next capture |
| Include Deleted Items / Junk Email | edit | hidden | hidden | settings `includeDeleted` / `includeJunk` | next capture or Archive now |

The Local columns apply wherever 3.2 gives Local fields, a mixed archive's run-job included.

- **"No scheduled backup"** is How often's starting value for every kind with nothing installed; with
  no descriptor, Settings shows no cadence, never the settings file's copy. Once installed it is not
  offered; Remove stops it (C-2). Choosing it with nothing installed sends no schedule request.
- **The Microsoft 365 boxes show only on a Microsoft 365 archive with the M365 fields** (ruling 3),
  labelled "These apply to every Microsoft 365 capture on this computer, and to Archive now" (C-16).
  **Any archive where Archive now is offered and the boxes are hidden** (a Local archive, or a mixed
  archive showing its run-job's Local fields) gets one line instead (R3C-16): "Archive now uses this
  computer's Microsoft 365 options (Deleted Items, Junk Email, original .eml); change them in Settings
  on a Microsoft 365 archive."
- **Sync is never hidden while installed (C-10).** If the job file carries sync, Settings shows its
  installed value plus a reason line if it can't run now ("Thunderbird's profile isn't on this computer
  right now"). The page never posts presync=false for a box it did not show. Sync reads as on, off or
  **can't read** (with the reason), never "off" for an error (today `localschedule.go:212-221`) (C-9).
- **.eml read-only:** "Keep each message's original .eml too: Yes (set by how this archive was made)",
  or "Not known" with neither job file nor readable record. An edit no longer rebuilds the job (3.5), so
  the value shown is the value kept (C-4).
- **Local, no schedule:** sync is greyed: "Choose how often first: the sync runs before each backup."
- **Read-only schedule (R3C-16).** An **edit** needs only a securely readable job file (3.5), so an
  installed run-job turns read-only only when its job file is not securely readable, never because a
  source drive is away; a **create** needs the rebuild from the last-run record, so "can't be set up
  here" is keyed to that rebuild. **Local on Linux, Encrypted, Unknown, or those read-only cases:** no
  schedule inputs; an installed one shows cadence and sync read-only, plus "To change it, run the
  MailArchive wizard (mailarchive wizard), which offers daily at 2:00 AM or weekly on Sunday at 3:00 AM,
  or use the command line's mailarchive schedule with -at" (`wizard.go:1714-1716`). The wizard note
  (`dashboard.go:1461`) shows only when nothing is installed (C-9). Encrypted keeps "Scheduled backups of
  an encrypted archive aren't available yet" (`dashboard.go:1460`).

### 3.4 Paused and auto-paused archives (C-11, R3C-11)
On a sealed (paused) archive, Save refuses (section 6) with scheduler, descriptor and job file
untouched; today nothing checks the seal. An auto-paused archive shows "Backups stopped: this archive's
source has been unreachable since DATE" and, beside Save, "Saving starts the scheduled backup again. If
the source is still unreachable at the next backup, it stops again." (R3C-11: auto-pause keeps the
streak, `pause.go:84-107`, so the 30-day test is already met at the next failed run.) Auto-pause removes
the entry, keeps the descriptor and never seals (`pause.go:92-107`), so 3.6 step 3 counts it as
**broken** and Save posts even with the cadence unchanged — **aspirational** (MA-860).

### 3.5 One source per value; an edit changes only what was changed (C-4, C-6)
- Sync lives only in the job file; no settings field is added (health reads the job file,
  `health.go:979`) — **proven** for today's reader. The cadence shown is the installed descriptor's
  (`dashboard.go:1066-1069`), so card and Status agree (X10). The local install stops writing the
  settings file's interval and time (drop `localschedule.go:204`); that copy stays only as the capture's
  record of what it last installed, and is **no longer an input to any rebuild** (3.6 step 4, R3C-14).
- **Edit = read-modify-write.** With a securely readable job file, an edit reads it through `job.Read`,
  changes only `thunderbird_sync`, and writes it back through `job.Write` (the one writer); the cadence
  goes to the scheduler entry and descriptor only. The rebuild from the last-run record
  (`localschedule.go:145`, `canonical.go:21-82`) runs on **create only** — **aspirational** (MA-858).
  The create path's start-date refusal is **dropped** (R3C-16): the last-run record never carries a
  start date (`run.go:1141-1162`; `FromCanonicalArgs` refuses one, `canonical.go:12`), so it could never fire.
- **Never overwrite an unreadable job file (C-6).** If the file `scheduleInstallLocal` would overwrite
  exists and `job.Read` refuses it (newer version, symbolic link, wrong owner, bad JSON,
  `job.go:188-193,223-237`), refuse and write nothing (section 6).

### 3.6 How Save works (one button, one archive)
1. **Every post names the archive (C-12):** both carry `drawnFor`, the folder the card was drawn for;
   the server refuses (section 6) when it is not `effectiveOut` (MA-680's binding idea). `api/settings`
   carries `drawnFor` and, separately, `out`, the location asked for.
2. **Location changed:** only the location is sent, with today's confirm; every other field is greyed:
   "Save the new location first; this archive's options then show here." Success reloads the page
   (today it does not, `dashboard.go:1757`) (C-12).
3. **Location unchanged:** the boxes the kind shows go to `api/settings`; `saveSettings` leaves an
   omitted one unchanged (`dashboard.go:459-461,531`). Only after ok:true with no confirm, and only if
   the schedule **changed** (differs from the descriptor; nothing installed and a cadence chosen; or the
   installed state is broken or auto-paused: C-2, C-9, C-11), `{drawnFor, interval, time, presync}` goes
   to `api/schedule-install` (`schedule.go:120`), routed by 3.2.
4. **Captures build from the values they are given, never from the settings file (C-13, R3C-14).**
   `backupSpec()` builds from the settings file's interval and time (`schedule.go:53,80-91`, called at
   `:162`) — proven. An install builds through `backupSpecFor(out, interval, at)` with the posted
   values; an empty-body capture **Repair** builds with the **descriptor's** interval and time, so it
   keeps its own cadence even when the one settings file holds another archive's. `saveScheduleChoice`
   (`:152`) moves to right after a successful Install (`:171`) and **before** the BackupFor clean-up,
   which can return early (`:177-182`), so that early return never skips it (R3C-14).
5. **Sync alone changed:** only the job file is written; the scheduler entry is left alone (C-18).
6. **The note says what happened (C-13, R3C-16):** "Settings saved." only if a settings field changed;
   "Backup schedule updated." if the schedule did; on a refusal, "<what was saved, if anything>. The
   backup schedule was not changed: <reason>." Inside that sentence the reason is given **without** its
   own "Nothing was changed.", so the reply never says "saved" and "nothing changed" together.

### 3.7 Hourly: what it costs, and its bounds (C-14, OR-2)
Hourly is offered wherever the form edits a run-job (3.2), a mixed archive's included (R3C-15); a
capture with `hourly` is refused (section 6). **No dependency on part B:** in part A the form edits a
run-job only where today's guard already allows one, so the re-review's "slice 3 depends on slice 1"
(R3C-15) no longer applies; when part B lands, the same rule reaches more archives. **It does depend on
A3 (rev4.1):** today a mixed archive's install goes the capture way (`schedule.go:137-140`), so Hourly on
a mixed archive's run-job reaches the run-job only through A3's record-against-installed routing (3.2);
A4 therefore builds after A2 **and** A3 (section 12). Before Hourly ships, each cost of 24 runs a day is
bounded:

- **Alert streak file, updated on every run (R3C-10).** Reading the previous history entry fails,
  proven: this run's entry is already appended when the alert decides (`run.go:600,1234` before
  `wizard.go:287`, `capture.go:104`), and early failures write none. Instead the shared layer
  (`backupalert.go:111`, MA-702) keeps an **alert streak file** in the owner's config folder (0600, the
  secure helpers), and:
  - it is updated with **every scheduled run's outcome, before the event gate** (`backupalert.go:112-114`)
    and whether alerts are on or off, so a success clears it even with success emails off (the default,
    `settings.go:54`);
  - a failure sends only when the file says "not failing"; "failing" is written **only after the send
    succeeds**, or at once when no email is due (alerts off); a send error is logged
    (`backupalert.go:133-137`) and the next failure tries again;
  - the first success after a failing streak sends one recovery email ("Backups are working again"),
    gated by the **failure** option, then clears the file;
  - **every failure exit** of `RunHeadless` and `HeadlessCapture` goes through it, except the paused
    refusal: the unreadable job file (`wizard.go:194-197`), the run log that can't open (`:225-228`), an
    absent `-out` (`:201-205`), Outlook PST (`:244-251`), and a capture with Microsoft 365 not set up
    (`capture.go:66-69`);
  - its **key** is `DefaultNameFor` of the archive folder the run backs up into (a run-job's Out, a
    capture's `effectiveOut`), which Install and Remove compute the same way for a Desktop-made schedule;
    create, Remove and auto-pause **clear** it. **Conditional** on the same spelling of that folder;
  - **for the unreadable-job-file exit (rev4.1)** there is no Out to read (`wizard.go:194-197` fails
    before it), so the key is **the job file's own name**: its file name without `.json`
    (`job.PathFor`, `internal/job/job.go:133-138`). For Desktop- and wizard-made jobs that name is
    `DefaultNameFor(out)` (`localschedule.go:150,183`, `wizard.go:1761-1762`, `addkeyed.go:72-73`), so
    it is the same key the run's other exits, Install and Remove use — **proven** by reading. A job file
    under any other name (none of today's writers makes one) keys by that name, alerted once per its own
    streak;
  - an unreadable file sends (fail loud) and is rewritten; if it can't be written, each failure sends and
    the log says why — **conditional** on the config folder being writable. The auto-pause email still goes;
  - saving Hourly while success emails are on warns: "Success emails are on, so this sends one email
    every hour. Turn them off in Backup alerts if that's too many."
- **Warning after 3 days, auto-pause after 30, measured between runs (R3C-11).** The source streak
  gains `last_unreachable`, written only when a run finds the source unreachable (`app/sourceloss.go:90`).
  `SurfaceLoudly` and `AutoPauseDue` (`sourceloss.go:78,82`) move from run counts (`:30,33`) to
  `last_unreachable` minus `FirstUnreachable` (`:47`), with at least two failures, **never compared with
  now**. So the banner, Status and `AutoPauseIfDue` agree; a computer simply off shows nothing on boot;
  an inconclusive run (`app/sourceloss.go:79-81`) moves nothing.
- **Accepted edge (ruling 2026-10-28):** `FirstUnreachable` is set at the first failure
  (`sourceloss.go:66-67`), so a weekly backup that fails once, then the computer is off for a month,
  auto-pauses at the next failed run (and warns at once) — **proven** by reading, pinned by MA-871.
- **The archive's own drive unplugged (R3C-12):** the source streak lives inside the archive
  (`state/sourceloss.go:89`), so it is never written then, and the 3-day warning and auto-pause **never
  trip** for that cause. Stated, not fixed: the alert streak file (owner side) still sends one email per
  failing streak, and Desktop notices follow it.
- **Desktop notices follow the alert streak (R3C-12):** one per failing streak plus a recovery notice,
  not one per failed run (`wizard.go:201-205,317`).
- **Local auto-pause:** `AutoPauseIfDue` is called only from the capture (`capture.go:108`); RunHeadless
  gets the same call. **Failure log:** `jobfail.log` (`wizard.go:311`) is capped at 1 MB, one older copy.
- **Overlap, bounded (R3C-12):** a start while another holder has the archive lock (`run.go:522-525`)
  exits quietly only while that holder **started less than 24 hours ago** (the lock records `started=`,
  `internal/lockfile/lockfile.go:83`); its line names the real holder (`lockfile.HolderVerb`, `:236`),
  such as a verify or a rebuild. Quiet means no failure, alert, streak or history entry. Past 24 hours it
  counts as a failure, alerted once per streak. Second-instance behaviour, **conditional**, not tested:
  launchd does not start a second copy; Task Scheduler follows the installer's policy (checked at build);
  cron starts one, which the lock skip covers.
- **Scheduled verify:** saving Hourly where one exists (`schedule.go:535-544`) warns: "A scheduled
  verify also runs on this archive; backups that start while it runs are skipped."
- **Run history** keeps 50 entries (`runhistory.go:27`), about two days at hourly; stated in the Recent
  backups card, not changed.

## 4. The capture kind guard and Resume: part B, and what part A shows meanwhile

The capture kind guard (C-1, OR-1, the §4.6.5(b) and MA-631 amendment, R3C-1 to R3C-9) and Resume
routing moved to `docs/design-capture-guard.md`, which owes its own pre-code review, adversarial pass and
security re-ratification before it merges. Part A changes neither: every install still passes today's
`refuseCollision` then `CheckKind` (`internal/schedule/schedule.go:641-646`), and Resume is untouched
(`pause.go:72-80`). Meanwhile, on a computer where Microsoft 365 is set up, the current archive always
counts as having a capture (`kindcorroborate.go:36-44`, proven), so where 3.2 says "capture counted" and
the archive or its schedule is local, the card shows, in place of the schedule inputs and not hidden:

> This archive can't get its own scheduled backup yet on a computer where Microsoft 365 is set up:
> MailArchive can't yet tell it apart from a Microsoft 365 capture. A later MailArchive update adds it;
> nothing about this archive is changed meanwhile.

With a run-job already installed there, it adds "Its installed backup keeps running as it is.", shows
**the installed cadence and Thunderbird sync read-only** (as 3.3's read-only schedule does, sync with
its on / off / can't-read value) and offers Remove only (rev4.1, MA-857).

**Where the plain-words refusal comes from (rev4.1).** A crafted post is refused by **A3's routing**, not
by rewording `CheckKind`'s error: before any write and before `schedule.Install`, the route reads
`BuildKindEvidence(out)` (`kindevidence.go:44-57`, the same evidence the card draws from) and, where a
capture is counted and the archive or its schedule is local, refuses with section 6's plain words,
nothing written. `CheckKind` (`kindevidence.go:78`) and `refuseCollision` (`schedule.go:633`) stay
**untouched**, as the backstop inside `Install`; no code matches on their error text and neither is
edited. If the route and the guard ever disagree, the guard's own (untranslated) refusal still stops the
install, with nothing written, since the A3 order and 3.6 step 4 write nothing before Install —
**aspirational** (MA-857, MA-860).

## 5. The backup card

Renamed **Scheduled backup** (X11). It keeps the kind line (`dashboard.go:1462`), the installed state,
Run now, Repair and Remove, and gains "How often, time and Thunderbird sync: Change in Settings" (to
`#sec-settings`), **only where Settings has schedule fields** (C-9). The interval, time and sync inputs
and Install / update leave it (`dashboard.go:1466-1473`). Where 3.2 shows the section 4 notice, the kind
line is today's (it may call a run-job a capture there; part B fixes the label).
- **Repair never rewrites the job file (C-5):** an empty-body Repair of a corroborated run-job goes to
  `repairRunJob` (descriptor plus `wizard.RepairSpec`, `schedule.go:201-226`) whatever the kind; it keeps
  sync and the start date and works with a source missing.
- **Capture Repair stays (C-2) and keeps its own cadence (R3C-14):** empty body, builds from the
  descriptor's interval and time (3.6 step 4), re-points BackupFor (`schedule.go:177-182`). Until built,
  "keeps the cadence" is **aspirational**: today it builds from the settings file (`:53,80-91`).
- **Broken run-job (C-9), where no capture is counted:** descriptor says run-job and the job file is
  **missing**: plain words and a Repair running the **create** path, whose reply says "Thunderbird sync
  is now off; switch it on in Settings" when the old file had it (sync can't be read from a missing file,
  `localschedule.go:138-143,212-221`); **unreadable**: no Repair (C-6 would refuse it), section 6's
  unreadable-file message and its remedy, ahead of any other label. Part B's decision table covers the
  same states where a capture is counted.
- Every health remedy names a button that renders, and can succeed, for that kind (the rule at
  `health.go:943-944`) — **aspirational** (MA-866, MA-867).

## 6. Refusals (plain words, nothing changed on disk)

Each is `{ok:false,error}`, checked before any write: scheduler, descriptor, job file and settings file
untouched — **aspirational** (MA-860). Today the first two are dropped silently (`localschedule.go:153`).
When a reason is quoted inside 3.6 step 6's sentence, its "Nothing was changed." is left off (R3C-16).

| Case | Wording |
|---|---|
| Sync off macOS/Windows | Alone: "Thunderbird sync works on macOS and Windows only, so nothing was changed." (`cmd/mailarchive/job.go:276-277`). Inside 3.6 step 6's combined reply (rev4.1): "Thunderbird sync works on macOS and Windows only.", without ", so nothing was changed." |
| Sync asked, archive not from Thunderbird | "This archive wasn't made from a Thunderbird profile on this computer, so there is no Thunderbird to sync. Nothing was changed." |
| Sync turned on while it can't run (C-10) | "Thunderbird sync can't be switched on: <reason>. Nothing was changed." |
| Encrypted | today's text (`schedule.go:128-130`) |
| Unknown kind (C-8) | "MailArchive can't read this archive's record right now (is its drive connected?). Nothing was changed." |
| Unreadable job file (C-6) | "This archive's backup settings file <path> can't be read (<reason>). Re-create the backup in the MailArchive wizard, or update MailArchive if a newer version made it. Nothing was changed." |
| Local backup where a capture is counted (section 4) | Refused by A3's routing from `BuildKindEvidence`, read before `Install`, with `CheckKind` untouched as the backstop; no error-text matching, no second detector (rev4.1): "This archive can't get its own scheduled backup yet on a computer where Microsoft 365 is set up. Nothing was changed." |
| Paused (C-11) | "This archive is paused; Resume it first. Nothing was changed." |
| Page out of date (C-12) | "This page is out of date; reload it. Nothing was changed." |
| Backup running (C-18) | "A backup is running now; save again when it finishes. Nothing was changed." (archive lock, `run.go:522-525`) |
| Cadence it would not apply (C-7) | "This schedule was set up at the command line; change it there. Nothing was changed." |
| Hourly capture (OR-2) | "Hourly backups are for local mail backups; choose Daily or Weekly for a Microsoft 365 capture. Nothing was changed." |

(R3C-16) Revision 3's "Start date, create only" row is dropped (3.5).

## 7. X16 notice and the macOS note

Wherever the sync box shows, the full `presync.SyncNotice` (`internal/presync/api.go:100`) is visible
text under it, never hover-only (X16, `docs/ux-contract.md:26`; MA-583); on macOS the Automation note
too. That note lives only in the wizard today (`wizard.go:1707-1709`); it moves into `internal/presync`
as one constant both use — **aspirational** (MA-861).

## 8. Location change with a local schedule (C-3)

`saveSettings` asks before a location change whenever a backup is installed (`dashboard.go:491`,
counting a local job, `signin.go:130-136`), in Microsoft 365 wording (`dashboard.go:508`) that is false
for a run-job, whose folder is fixed in its job file (`localschedule.go:84`) — **proven**. For a
corroborated run-job it reads:

> This archive's scheduled backup keeps backing up <old folder>: its folder is fixed in its own settings
> file, so changing the Archive location doesn't move it. Save and keep the backup to leave it running
> there, or Save and remove the backup to stop it.

- **"Save and keep"** sets **no BackupFor** for a run-job; BackupFor feeds `backupTask`, `removeTasks`
  and the capture re-point only when it belongs to a capture.
- **A BackupFor an earlier version already set for a run-job is cleared in part A (rev4.1, owner
  2026-10-28).** Today it is a live bug: `removeTasks` adds BackupFor's folder to the folders it removes
  (`internal/desktop/schedule.go:283-299`), so Remove on archive B deletes the scheduler entry of A's
  run-job (and the capture install's clean-up at `:177-182` does the same), leaving A's job file behind;
  `backupTask` (`:44-47`) also shows A's run-job as B's backup. **BackupFor is kept only for a capture.**
  - **The test**, the same one part B uses (its R3C-5): BackupFor names a run-job when
    `job.PathFor(DefaultNameFor(BackupFor))` reads securely through `job.Read` and its Out is the same
    archive as BackupFor (`SameArchivePath`). Owner-side evidence only; nothing inside the archive is read.
  - **The one-time clean-up:** when the Desktop starts, if the test holds, it clears BackupFor in the
    settings file (`UpdateSettings`, as `schedule.go:183` does) and writes nothing else: no scheduler
    entry, descriptor or job file is touched. Once cleared there is nothing left to clear, so it is
    one-time in effect; it runs at start, never on page load (section 10, MA-864).
  - **Until the next start** (a BackupFor written later by an older version, section 10): `backupTask`,
    `removeTasks` and the capture install's clean-up apply the same test and skip such a BackupFor;
    `backupTask` writes nothing (it is drawn with the page), the two write paths clear it before acting.
  - **Residual, stated:** an unreadable job file at that name fails the test, so the BackupFor is kept
    and Remove on B can still delete A's entry; A's run-job fails every run then anyway (section 5's
    unreadable state). Part B's guard applies the same test.
  - Pinned by MA-862 (the clean-up and the skip) and MA-864 (the start clean-up is the only write
    before Save) — **aspirational**.
- **"Save and remove"** deletes the corroborated job file as `scheduleRemove` does (`schedule.go:242-253`).
- The capture wording and choices (MA-523) keep their behaviour; "weekly backup" becomes "scheduled
  backup" (OR-3). MA-862 pins this — **aspirational**.

## 9. Crash and failure ordering of a local install (C-18, C-9, R3C-13)

In part A a local create happens only where no capture is counted (section 4).
1. Refusals (section 6): nothing written.
2. `schedule.Install` (`schedule.go:586-615`): **2a** the scheduler entry, **2b** the descriptor —
   **proven**. Install returns a distinct error, `ErrNotRecorded`, when 2a succeeded and 2b failed (today
   the error is untyped, `:612-613`) (R3C-13).
3. `job.Write` (create, or the sync edit of 3.5): a temporary file in the same folder, **flushed** (new,
   as `schedule.go:950` does), then renamed over the old (`job.go:159-177`). The rename is **proven**;
   surviving power loss stays **conditional** (no folder flush).

The page asks the scheduler before step 2 whether an entry already existed under the name; a rollback
runs **only when none existed**, so a create running as a Repair over an old entry never removes it and
then claims nothing changed (R3C-13).
- **2b fails, create:** the new entry is removed (`schedule.RemoveIfInstalled`); reply "The backup could
  not be recorded in the archive (<reason>), so nothing was scheduled." If that removal fails, the reply
  says so and the card shows the entry with Remove.
- **2b fails, edit (R3C-13):** the descriptor still holds the old cadence, so the edit restores the
  previous entry from it (the rebuild run-job Repair uses, `schedule.go:202-212`, `wizard.go:1092-1097`)
  and replies "The backup schedule was not changed: it could not be recorded in the archive (<reason>)."
  If the restore fails too, the reply says the new cadence is running but not recorded, with the reason,
  and the card offers Repair, which puts the recorded schedule back. No reply advises Remove.
- **3 fails, create (R3C-13):** what step 2 installed is removed (entry and descriptor, only when none
  existed before); reply "The backup could not be set up (<reason>), so nothing was scheduled."
- **3 fails, edit (R3C-13):** the old job file stays whole; reply "The new schedule (<cadence>) is in
  effect; Thunderbird sync was not changed: <reason>." Settings then shows the old sync, so Save again
  retries it. Today's "Remove the schedule on this card and try again" (`localschedule.go:200-202`) goes.
- **A crash between 2 and 3:** edit as "3 fails, edit" once reloaded (**conditional** on the reload);
  create leaves an entry pointing at a missing job file; the run fails loudly
  (`wizard.go:194-196,308-317`) and the card shows section 5's missing-file state.
- **Residual:** the macOS plist is written in place, then reloaded (`schedule.go:1052-1058`); whether
  unload stops a running job is untested. Same as today's Install / update; not changed here.

Scheduler first, job file last, on purpose: a refused install leaves no decoy job file (A3,
`localschedule.go:119-122`). MA-863 pins this section — **aspirational**.

## 10. Existing installs, rollback and mixed versions (C-19)

Nothing changes on disk until the owner clicks Save; no write on page load — **aspirational** (MA-864).
The one exception (rev4.1): section 8's start-up clean-up of a BackupFor that names a run-job, which
clears that one settings field and nothing else.
- **Local schedule:** Settings shows the installed cadence, time and sync as the card did
  (`dashboard.go:1060-1069`); hourly shows "Hourly".
- **Local archive where Microsoft 365 is set up:** the section 4 notice; a capture or run-job already
  there is shown with Remove, never converted.
- **Command-line export that syncs Thunderbird** (`cmd/mailarchive/job.go:181,316`): read-only (C-7).
- **Local becomes mixed** (Archive now adds a `mailbox:` store, `localarchive.go:56-57`): the kind flips
  to Microsoft 365; a run-job keeps running and keeps the Local fields, Hourly included, where no capture
  is counted (3.2) (C-7).
- **Settings file:** `interval` / `weeklyTime` stay as the capture's record; `weeklyDay` stays unread.

**Rollback.** New on disk: the alert streak file (3.7), which older versions never read, and the source
streak's `last_unreachable` field (R3C-11), which an older version ignores: the streak is read with plain
`json.Unmarshal`, which skips unknown fields (`internal/state/sourceloss.go:97`, proven by reading). Hourly
is already accepted (`internal/schedule/schedule.go:32`) and `thunderbird_sync` exists (`job.go:47`). An
older MailArchive (v1.4.0 on) shows an hourly schedule with no matching menu option, shows sync as on,
and counts thresholds by runs again — **conditional**, read from today's code. Before v1.4.0 the job file
is read leniently (`job.go:182`). MA-864 asserts this version writes no field an older one misreads. No
downgrade guard (OR-4).

## 11. Button feedback on every Desktop page (owner item)

The style has `.btn:disabled` (`dashboard.go:1207`, `pages.go:47`) but **no `.btn:active` anywhere**,
and `.btn` sets its own background (`dashboard.go:1204`, `pages.go:43`); commit 7c4e013 found these
rules beat browser defaults. Some handlers disable their button (Run now `dashboard.go:1848`, alerts
`:1619,1621`, Shut down `:1590`), but Save settings (`doSave`, `:1636-1649`) and Install / Repair /
Remove (`schedPost`, `:1830-1839`) show nothing until the reply — **proven**.
- One helper per page script, `busy(button, label)`: sets `disabled`, `aria-busy="true"` and a label
  ("Saving…", "Working…"), puts what is happening in the card's note, and returns a restore that runs
  in `finally`. A confirm comes first; busy starts after the yes. Long jobs (Run now, verify, rebuild)
  stay disabled until their progress note takes over.
- Each style block in scope gains `.btn:active` (pressed: darker, 1px down) and `[aria-busy="true"]`
  (progress cursor and a small spinner, still under `prefers-reduced-motion`).
- **Scope:** every Desktop page (`internal/desktop/dashboard.go`, `pages.go` with `pages_scripts.go`,
  `encryptionscript.go`) **plus the archive reader's Prev / Next** (`internal/server/page.go:62`), which
  the Desktop serves (`internal/desktop/reader.go:93-95` through `server.Serve`) **and so does `mailarchive
  serve`** (`cmd/mailarchive/surfaces.go:30`) (R3C-17); those carry no `.btn` class, so the reader's style
  block gets its own pressed and busy rules. **Not** the Microsoft 365 setup page
  (`internal/server/setup_page.go`): the separate `setup` surface serves it (`surfaces.go:33-36`).
- MA-872 is structural; a live Chrome check is part of its friction note — **aspirational**.

## 12. Build slices (part A only, dependency order)

**Before any slice, slices A1 and A2 included: a fold check of this revision** (R3C-17). Each slice lands
its rows with covering tests, prove-fail then prove-pass. No slice changes the capture kind guard
(`refuseCollision`, `CheckKind`, `CaptureCorroborates`) or Resume.
- **A1, button feedback (section 11, MA-872; revision 3's slice 6):** no dependency; first (small,
  awaited). Its **own friction note and a live Chrome check before it ships alone** (R3C-17), including
  the reader under `mailarchive serve`.
- **A2, live fixes (revision 3's slice 0, C-20):** in-place edit and create-only rebuild, start-date row
  dropped (C-4, R3C-16, MA-858), unreadable-file refusal (C-6), refuse instead of drop for sync (C-10,
  MA-860's sync rows), captures install the posted cadence and Repair from the descriptor, recorded after
  success and before the BackupFor clean-up (C-13, R3C-14, MA-866's capture rows), run-job location
  wording and no BackupFor for a kept run-job (C-3, MA-862), and the start-up clean-up of an existing
  BackupFor that names a run-job, with the readers' skip (section 8, owner 2026-10-28, MA-862's clean-up
  cases; MA-864's "the only write before Save" clause lands with A6's migration check) (rev4.1). Own
  friction note.
- **A3, kind and server guards (after A2):** `backupKind` with Unknown (C-8, MA-856), untrusted record
  (C-17, MA-869), record-against-installed routing with the section 4 notice (C-7, MA-857's routing
  cases, MA-860's notice refusal), Repair never rewrites (C-5, MA-866), broken run-job states (C-9),
  paused and auto-paused (C-11), one archive per Save (C-12), the section 4 refusal from the route's
  `BuildKindEvidence` read before Install with `CheckKind` untouched (rev4.1), running-backup refusal, failure replies,
  `ErrNotRecorded` and the job-file flush (C-18, R3C-13, MA-863). Then the **adversarial pass** (section 14).
- **A4, Hourly and its bounds (after A2 and A3, rev4.1: Hourly on a mixed archive's run-job needs A3's
  routing, 3.7; OR-2, C-14, R3C-10 to R3C-12, R3C-15):** alert streak file (MA-870), `last_unreachable`
  and the 3-day / 30-day thresholds, local auto-pause (MA-871), notices by streak, log cap, bounded lock
  skip, verify and success-email warnings, the option on every run-job form (MA-859). The owner accepted
  amending MA-690/693/698/702 on 2026-10-28 (section 1, rev4.1).
- **A5, card per kind and text sweep (after A3 and A4; OR-3, C-2, C-15, C-16, R3C-16):** fields,
  read-only keying, labels, the Archive now line, two-step Save and its note, renamed card, remedies
  (MA-857, MA-867), shared macOS note (MA-861), and every string naming the old card or button
  (`health.go:952,959,961,996,1017`, `dashboard.go:501,1248,1388,1421,1473,1646` and `:628-641`,
  `pages.go:119`, `localarchive.go:123,223`, `schedule.go:406`, README.md), re-worded under X11 with
  "set up afresh" pointing at Settings (MA-868).
- **A6:** migration and rollback check (MA-864), then the full friction review with a live Chrome check.

Part B (`docs/design-capture-guard.md`) builds after A3 and A5.

**Amended rows, tests and text (accepted by the owner unless marked; nothing weakened).**
- OR-3: MA-583, MA-617 (`docs/scenario-catalog.md:1180,924`) re-pointed to the new cards, MA-523
  (`:897`) relabelled; strings in `ma633_repairhow_test.go:45,75,143`, `ma616_texts_test.go:52`,
  `ws_archive_fric_test.go:887,896`.
- Elapsed time and per-streak emails, accepted: MA-686, MA-687, MA-688 (`:981-983`; MA-686 says "NEVER
  elapsed time"); tests `sourceloss_test.go:62-65`, `pause_test.go:128-137`, `sourceloss_health_test.go:21`,
  `sourceloss_surface_test.go:30`; the comments at `internal/state/sourceloss.go:25,77`.
- **R3C-9, accepted by the owner 2026-10-28 (rev4.1):** MA-690, MA-693, MA-698, MA-702 and their
  tests, amended for the elapsed-time thresholds and once-per-streak emails, nothing weakened.
- Archive-manager §5.7 S3-C3: §5.7 itself reads "N failed runs or T days"
  (`docs/design-archive-manager.md:325`), so it already allows elapsed time; A4 adds a dated note of the
  3 / 30-day rule, measured between runs.
- MA-631 and the §4.6.5(b) text are part B's.

## 13. Test-spec rows (part A: MA-856 to MA-879; the catalog ends at MA-855, `docs/scenario-catalog.md:1095`)

All unit rows (U); none needs real infrastructure. **MA-865 and MA-873 are retired, never reused**: their
content moved to part B as MA-880 and MA-881; part B holds the reserved range MA-880 to MA-889. MA-874 to
MA-879 are free for part A.

| Row | What it proves |
|---|---|
| MA-856 | Kind from the record, never the tenant; an unreadable record is Unknown (location only, install refused, nothing written); mixed / M365 / empty go the capture way; Archive now unchanged; the guard is called unchanged on every install |
| MA-857 | Each kind's field set (3.3) and the record-against-installed table (3.2); where a capture is counted and the archive or its schedule is local, the section 4 notice shows, no schedule inputs, Remove only for an installed one, and with a run-job installed its cadence and sync shown read-only (rev4.1); the Microsoft 365 boxes only with the M365 fields, and the "Archive now uses" line on every archive where Archive now is offered and the boxes are hidden (a mixed archive's run-job included); read-only keyed to "no securely readable job file" for an edit, with the wizard's real choices in the remedy; "No scheduled backup" for every kind with nothing installed, sending no request; no schedule inputs on the backup card, "Change in Settings" only where Settings has fields; a location save greys the rest and reloads |
| MA-858 | One source per value; after an edit the job file is byte-equal except `thunderbird_sync` (on a wizard job whose last run was full with no `-raw`); no sync field in settings; a local install leaves settings' interval/time alone; omitted boxes unchanged; no start-date refusal on create |
| MA-859 | Hourly offered wherever the form edits a run-job (a mixed archive's included), refused for a capture; reloads as Hourly; hourly with sync installs (Q11 reversed, `docs/design-thunderbird-presync.md:1324`); a start while a holder started under 24 hours ago exits quietly naming that holder, no alert or streak, and past 24 hours counts as a failure alerted once; the failure log stays under its cap; Hourly with a scheduled verify warns; Hourly with success emails on warns |
| MA-860 | Each section 6 refusal states its reason and leaves scheduler, descriptor, job file and settings file unchanged (`assure.Refused`, positive twin first), including a crafted local install where a capture is counted, refused by the route's `BuildKindEvidence` read before Install with `CheckKind` and `refuseCollision` unchanged (rev4.1); a capture install installs exactly the cadence posted and records it only after success; an auto-paused archive's Save with the cadence unchanged re-installs; a hidden sync box is never posted as off; a combined reply never says "saved" and "Nothing was changed" together |
| MA-861 | Wherever the sync box renders, `presync.SyncNotice` is visible beside it, and on macOS the Automation note from the one constant the wizard also uses |
| MA-862 | A run-job location change names the old folder; "keep" sets no BackupFor, and B's card, tile and Settings show no backup while B's Remove or capture install leaves A's entry and job file; "remove" leaves no job file for A; a capture keeps MA-523's behaviour; (rev4.1) a BackupFor already set for A's run-job (its job file reads securely with Out = A) is cleared at Desktop start, after which B's Remove and B's capture install leave A's entry and job file, while a capture's BackupFor is kept (positive twin first); one written after start is skipped by `backupTask`, `removeTasks` and the capture install's clean-up |
| MA-863 | A refusal leaves no job file; an injected 2b failure on an edit restores the old entry and says the schedule was not changed, with the reason; on a create it removes the new entry and says nothing was scheduled; a create over an existing entry never removes it; a job-file write failure on a create removes what was installed, and on an edit says the new cadence is in effect and sync unchanged, never advising Remove; a missing job file shows the broken state whose Repair creates it and says sync is off; an unreadable one shows no Repair |
| MA-864 | Loading the page writes nothing; the start-up BackupFor clean-up (section 8) is the only write before Save and changes only that field (rev4.1); sync on or off shows the same values in Settings; a capture on a local archive is shown, not converted; no field an older version misreads, `last_unreachable` included |
| MA-866 | Run-job Repair goes to `repairRunJob` whatever the kind: with a source missing it succeeds and the job file's bytes are unchanged; with Microsoft 365 set up (a capture counted) it is refused by today's guard and nothing is written: scheduler entry, descriptor and job file unchanged (rev4.1); capture Repair keeps the descriptor's cadence when the settings file holds another, and re-points BackupFor |
| MA-867 | Every health remedy names a button that renders and can succeed for that archive's kind (`health.go:943-944`), the section 4 notice's archives included |
| MA-868 | Structural: no Desktop text, tooltip, health remedy or README line names the "Weekly backup card", "Install / update" or "set up on this card" |
| MA-869 | An M365 archive whose record is rewritten to local store ids installs nothing and refuses in plain words; a planted network path causes no file check; drawing the page never waits on source storage |
| MA-870 | With the alert streak file, for both the capture and the local run: two failed runs in a row send one email; with success emails off, fail, succeed, fail sends two failure emails; the first success after sends one recovery email; a send failure on the first failed run, then a delivered one on the second; each early failure exit goes through it, the unreadable job file keyed by the job file's own name, the same key as the archive's other exits for a Desktop- or wizard-made job (rev4.1); failures with the archive's drive unplugged send once, not hourly; remove, re-install, then fail sends; an unreadable streak file sends and is rewritten; the auto-pause email still goes; Desktop notices follow the streak |
| MA-871 | Warning at 3 days and auto-pause at 30 days of `last_unreachable` minus `FirstUnreachable`, with at least two failures; a computer off 3 days shows no banner on boot; a computer off 30 days trips nothing; an inconclusive run after an old streak does not auto-pause; a weekly backup failing once, then off a month, auto-pauses at the next failed run; RunHeadless auto-pauses like the capture |
| MA-872 | Every Desktop action button and the reader's Prev / Next (under the Desktop and `mailarchive serve`) go through `busy` (disabled, aria-busy, working label, note), restored after the reply; each style block in scope carries the pressed and busy rules; the setup surface is untouched |

## 14. Gates owed

- **Fold check of revision 4 before any slice**, A1 and A2 included (R3C-17).
- **Adversarial pass: owed** (`../assurance-kit/process/adversarial-review.md`), covering A2 to A4,
  before ship. **Part A adds writers of the job file:** the in-place edit (a new read-modify-write caller
  of `job.Write`), Repair's create path for a missing file, the failure rollbacks of section 9, and "Save
  and remove" deleting a job file. All go through the existing writer and remover, but they are new
  write paths. Part A also routes by a record inside the archive (`localarchive.go:43`) and adds an
  owner-side streak file. Starting targets: C-5, C-6, C-12, C-17, section 9's rollbacks, the alert streak
  key, and the section 4 notice's server refusal; (rev4.1) the start-up BackupFor clean-up, a new writer
  of the settings file, and its test.
- **Security pass:** none owed by part A for the guard (that is part B's re-ratification); the adversarial
  pass above covers part A's new write paths.
- **Friction review:** A1 and A2 each carry their own note, A1 with a live Chrome check before it ships
  alone (R3C-17); the full review before ship of A5 and A6, with a live Chrome check, including the
  section 4 notice on a computer with Microsoft 365 set up.
- **UX contract rows:** X2 (one refusal voice), X10 (installed values shown), X11 ("Scheduled backup",
  "Change in Settings"), X16 (notice wherever the box shows).

**Not changed here:** a Desktop-made local schedule copies the last run's mode on create, so a full last
run makes every scheduled run full (`canonical.go:45-49`; read, not tested). Day of week stays Sunday.
Backup alerts and the three Microsoft 365 boxes stay one set per computer (C-16). The capture kind guard
and Resume (part B).

---
Revision 4, 2026-10-28. Re-read for it: `internal/desktop/kindcorroborate.go`,
`internal/schedule/kindevidence.go`, `internal/lockfile/lockfile.go` (by grep: `:83`, `:236`),
`internal/state/sourceloss.go` (by grep: `:47`, `:97`). Other citations are revision 3's, the fold
check's, the rescue check's and the re-review's, not re-read. Nothing run.

Revision 4.1, 2026-10-28 (the fold check's fixes and the owner's two rulings of that day). Re-read for
it: `internal/desktop/schedule.go:20-317`, `internal/wizard/wizard.go:186-235` and by grep `:1761-1766`,
`internal/wizard/addkeyed.go:72-77` (by grep), `internal/job/job.go:133-138`,
`internal/schedule/kindevidence.go:60-104`, `internal/desktop/localschedule.go` (by grep: `:150,183`),
`cmd/mailarchive/job.go:560-577`. Nothing run.
