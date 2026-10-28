# Design: the Settings card edits this archive's backup options, by kind

Branch `settings-card`, 2026-10-28. Pre-code design; nothing is built yet.

**Decision:** Settings becomes the one place to change how this archive is backed up (how
often, time, Thunderbird sync), showing only what applies to its kind; the backup card
keeps status and Run now / Repair / Remove.

Labels: **proven** = read in the code at the cited line on this branch (read, not run);
**conditional** = holds only under the condition beside it; **aspirational** = what this
design makes true once built, pinned by a test-spec row in section 11.

## 1. Owner rulings (2026-10-28, settled)

1. Settings is the **one editor** of backup options; the backup card keeps status plus
   Run now / Repair / Remove and says "Change in Settings".
2. A local-mail archive gets its own scheduled backup and Thunderbird sync **even where
   Microsoft 365 is set up**: an archive is judged by its own record, not the computer's.
3. Local archives show "keep .eml" **read-only** ("set by how this archive was made"); the
   three Microsoft 365 tick-boxes show only for a Microsoft 365 archive.

## 2. What is wrong today

- The Settings card shows the three Microsoft 365 tick-boxes for every archive, with no
  condition (`internal/desktop/dashboard.go:1379-1381`). Only the Microsoft 365 capture
  reads them (`internal/desktop/capture.go:81,87,247,250`). For a local archive they do
  nothing — **proven**.
- The Thunderbird sync box lives on the "Weekly backup" card (`dashboard.go:1471-1472`),
  inside a form shown only when the archive is "local only" and on macOS or Windows
  (`dashboard.go:1044,1059`) — **proven**.
- "Local only" is false whenever a Microsoft 365 tenant is saved on the computer, even for
  a pure Thunderbird archive (`internal/desktop/localarchive.go:93-106`). The card then
  offers the Microsoft 365 capture installer (`dashboard.go:1040`) and the install request
  goes the Microsoft 365 way (`internal/desktop/schedule.go:137-140`) — **proven**.
- The menu offers only Weekly and Daily (`dashboard.go:1468`); the scheduler accepts
  hourly (`internal/schedule/schedule.go:32,38-47`). An hourly local schedule therefore
  opens with no matching option — **proven**; that saving it then turns it weekly is
  **conditional** on the browser selecting the first option, not tested.
- A Settings save writes all three tick-boxes from the request (`dashboard.go:459-461,531`)
  into one file for the whole computer (`desktop-settings.json`, `internal/desktop/launch.go:194`),
  so a save that left them out would clear them — **proven**.
- A local install copies its cadence into that file (`internal/desktop/localschedule.go:204`),
  which the Microsoft 365 install reads (`schedule.go:80-91`): one archive's cadence leaks
  into another's default — **proven**.

## 3. The card, per kind

The kind comes from the archive's own record (ruling 2). A new helper, named here
`backupKind(out)`, returns:

| Kind | When (from the record) |
|---|---|
| Microsoft 365 | `archiveKind` is `m365`, `mixed`, `empty` or none (`localarchive.go:30-63`) |
| Local | `archiveKind` is `local` (`localarchive.go:58-59`) |
| Local from Thunderbird | Local, **and** a store sits in a Thunderbird profile on this computer (`localArchiveThunderbirdSourced`, `localarchive.go:274-291`) |
| Encrypted | `archiveKind` is `encrypted` (`localarchive.go:36-37`) |

`backupKind` never reads the tenant. It replaces `localOnly()` in the backup decisions
only: install routing (`schedule.go:137`), the backup card's state (`dashboard.go:1040-1074`)
and the overview's backup tile (`dashboard.go:621-642`). `localOnly()` stays as it is for
Archive now (`localarchive.go:141`), the page heading (`dashboard.go:1262`) and the
Microsoft 365 connection card (`dashboard.go:1317-1325`), which this design does not
touch — **aspirational** (MA-856).

| Field | Microsoft 365 | Local | Local from Thunderbird | Encrypted | Stored in (the one source) | A save takes effect |
|---|---|---|---|---|---|---|
| Archive location | edit | edit | edit | edit | settings file `out` (`internal/desktop/settings.go:24`) | at once: it picks the current archive (`settings.go:211-216`) |
| How often: Hourly / Daily / Weekly (Sunday) | edit | edit | edit | hidden | installed schedule record (descriptor) + the computer's scheduler | on Save, by re-installing (below) |
| At (local time; hourly uses the minutes only, `schedule.go:56`) | edit | edit | edit | hidden | same | same |
| Sync Thunderbird before each backup | hidden | hidden | edit, macOS and Windows only | hidden | job file `thunderbird_sync` only (`internal/job/job.go:47`) | on Save, via `api/schedule-install` |
| Keep original .eml | edit | **read-only** | **read-only** | hidden | M365: settings `keepRaw` (`settings.go:25`); local: job file `raw` (`job.go:38`), else the last-run record's `-raw` (`internal/job/canonical.go:69-70`) | M365: next capture reads it (`capture.go:87`); local: not editable here |
| Include Deleted Items / Junk Email | edit | hidden | hidden | hidden | settings `includeDeleted` / `includeJunk` (`settings.go:26-27`) | next capture reads them (`capture.go:81,247`) |

Notes per kind:
- **Microsoft 365:** today's fields plus How often / At moved in from the backup card.
  Hourly here is **conditional** on Microsoft 365 accepting an hourly capture without
  throttling; not tested against a live tenant (no lab tier, so this stays "pending").
- **Local:** the read-only line reads "Keep each message's original .eml too: Yes (set by
  how this archive was made)". With neither a job file nor a readable record it says
  "Not known — set by how this archive was made".
- **Local, no schedule yet:** How often starts at "No scheduled backup". Picking a cadence
  and saving installs one. The sync box is greyed with "Choose how often first: the sync
  runs before each scheduled backup." Once a schedule exists, "No scheduled backup" is not
  offered: Remove on the backup card is the one way to stop it.
- **Local on Linux:** no schedule fields, as today (`dashboard.go:1044,1059`); the card
  shows the existing wizard note (`dashboard.go:1461`).
- **Encrypted:** location only, plus today's sentence "Scheduled backups of an encrypted
  archive aren't available yet" (`dashboard.go:1460`). Key, password and program-copy
  settings stay on the Encryption card.

**One source per value.** Sync lives only in the job file; no settings field is added
(the health view already reads the job file, `internal/health/health.go:979`) — **proven**
for today's reader. The cadence shown is the **installed descriptor's** whenever a
schedule exists (as the edit form already does, `dashboard.go:1066-1069`), so the card and
Status agree (X10). The local install stops writing the settings file's interval and time
(drop `localschedule.go:204`); the settings file's copy stays only as the Microsoft 365
install's input (`schedule.go:80-91`) — **aspirational** (MA-858).

### How Save works (one button)
1. **Location changed:** Save sends only the location to `api/settings`, with today's
   confirm step. The schedule fields are greyed: "Save the new location first; this
   archive's backup options then show here." This keeps one archive's options from being
   applied to another.
2. **Location unchanged:** Save sends the tick-boxes that the kind shows to `api/settings`;
   `saveSettings` takes them as optional and leaves an omitted box unchanged (change at
   `dashboard.go:459-461,531`). Then, only if How often, At or Sync changed, it posts
   `{interval, time, presync}` to the existing `api/schedule-install`
   (`schedule.go:120`), which routes by `backupKind`: local to `scheduleInstallLocal`
   (`localschedule.go:123`), Microsoft 365 to the capture install (`schedule.go:150-193`).
3. If step 2's second post fails, the note says "Settings saved. The backup schedule was
   not changed: <reason>." No new endpoint and no new writer of the job file —
   **aspirational** (MA-857, MA-858).

## 4. The backup card

Renamed **Scheduled backup** (the old title is wrong for daily and hourly). It keeps the
kind line (`dashboard.go:1462`), the installed state, Run now, Repair and Remove
(`dashboard.go:1473`), and gains "How often, time and Thunderbird sync: Change in
Settings" linking to `#sec-settings`. The interval, time and sync inputs and the
Install / update button leave it (`dashboard.go:1466-1473`). Repair keeps posting an empty
body, which keeps the cadence and sync (`localschedule.go:138-143`) — **aspirational**
(MA-857).

## 5. Refusals (plain words, nothing changed on disk)

Each is a JSON `{ok:false,error}` from `api/schedule-install`, checked before the install,
so the scheduler, descriptor, job file and settings file are untouched — **aspirational**
(MA-860). Today the first two are silently dropped (`localschedule.go:153`), against X2.

| Case | Wording |
|---|---|
| Sync asked for off macOS/Windows | "Thunderbird sync works on macOS and Windows only, so nothing was changed." (matches the command line, `cmd/mailarchive/job.go:276-277`) |
| Sync asked for, archive not from Thunderbird | "This archive wasn't made from a Thunderbird profile on this computer, so there is no Thunderbird to sync. Nothing was changed." |
| Encrypted archive | today's text (`schedule.go:128-130`); the card offers no schedule fields |
| Job cannot be rebuilt | today's text (`localschedule.go:145-148`); the card shows the wizard note (`dashboard.go:1461`) instead of fields |
| Existing job file has a start date (`since`) the rebuild would drop | "This archive's backup was set up in the MailArchive wizard with a start date, which this page can't keep. Change it in the MailArchive wizard (mailarchive wizard). Nothing was changed." |
| A Microsoft 365 capture is already scheduled into a local archive | "This archive has a Microsoft 365 capture scheduled. Remove it on the Scheduled backup card, then set up a backup of its mail files here." |

- The start-date guard exists because the wizard writes `since` into the job
  (`internal/wizard/wizard.go:1799-1803`) under the same name the Desktop uses
  (`wizard.go:1761`, `localschedule.go:150`), while the last-run record never carries it
  (`internal/app/run.go:1141-1162`) — **proven**. Without the guard, a Settings save would
  quietly widen a wizard backup to all dates.
- **Repair, not a request:** Repair inherits sync from the job file (`localschedule.go:142`).
  If sync can no longer be honoured (the profile is gone from this computer), Repair keeps
  today's behaviour of dropping it, and its reply now says "Thunderbird sync was switched
  off: <reason>" — **aspirational** (MA-860).

## 6. X16 notice and the macOS note

Wherever the sync box shows, the full `presync.SyncNotice` (`internal/presync/api.go:100`)
is visible text under it, never hover-only (X16, `docs/ux-contract.md:26`; MA-583). On
macOS the Automation note shows too. Today it exists only in the wizard
(`wizard.go:1707-1709`); it moves into `internal/presync` as one constant both use, so the
two cannot drift — **aspirational** (MA-861).

## 7. Location change with a local schedule

`saveSettings` asks before a location change whenever a backup is installed
(`dashboard.go:491`), and the check counts a local job too (`internal/desktop/signin.go:130-136`)
— **proven**. The question uses Microsoft 365 wording: "Your weekly backup will now archive
<account> into <folder>" (`dashboard.go:508`). That is false for a local job: its folder is
pinned in its job file (`localschedule.go:84`) — **proven**. When the installed schedule is
a run-job (`schedule.BuildKindEvidence(...).Has(schedule.KindRunJob)`), the question reads:

> This archive's scheduled backup keeps backing up <old folder>: its folder is fixed in its
> own settings file, so changing the Archive location doesn't move it. Save and keep the
> backup to leave it running there, or Save and remove the backup to stop it.

The Microsoft 365 wording and choices (MA-523) are unchanged — **aspirational** (MA-862).

## 8. Crash ordering of a local install

Order inside one Save, unchanged from today:
1. Refusals (section 5) — nothing written.
2. `schedule.Install`: the computer's scheduler entry, then the descriptor in the archive
   (`schedule.go:586-614`) — **proven**.
3. `job.Write` (`localschedule.go:200`): written to a temporary file in the same folder,
   then renamed over the old one (`internal/job/job.go:159-177`), so a reader sees the old
   file or the new, never half — **proven** for the rename; that it survives power loss is
   **conditional** (no folder flush is done).
4. The settings-file cadence copy is removed (section 3), so nothing follows the job file.

Crash between 2 and 3:
- **Edit:** the old job file stays whole; the new cadence runs with the old sync value.
  Settings reads sync from the job file, so it shows the old value — the screen tells the
  truth and saving again fixes it — **conditional** on the reload after the crash.
- **Create:** the entry points at a missing job file. The run fails loudly: a failure log,
  a recorded failure and a notification (`wizard.go:194-196,308-317`) — **proven** by
  reading. Repair or Save fixes it.

Scheduler first and job file last is kept on purpose: a refused install leaves no decoy
job file (adversarial finding A3, `localschedule.go:119-122`).

## 9. Existing installs (migration)

Nothing changes on disk until the owner clicks Save; this design adds no write on page
load — **aspirational** (MA-864).
- **Local schedule, sync on or off:** Settings shows the installed cadence, time and sync
  as the backup card did (descriptor + job file, `dashboard.go:1060-1069`); hourly now
  shows "Hourly".
- **Local archive where Microsoft 365 is set up:** the capture installer the backup card
  offered is gone; Settings offers the local schedule (and sync if from Thunderbird).
- **A capture already scheduled into a local archive:** shown with Remove, never
  converted; a local install is refused (section 5).
- **Microsoft 365 archive:** same fields, How often / At now in Settings, Hourly added.
- **Settings file:** old `interval` / `weeklyTime` stay as the Microsoft 365 install's
  input; `weeklyDay` is still unread (`settings.go:28`).

## 10. Build slices (dependency order)

Each slice lands its catalog rows with their covering tests (the covers-map meta-test
fails a row with no test), prove-fail then prove-pass.
1. **Kind by record:** `backupKind`, install routing and card state (MA-856).
2. **Server:** optional tick-boxes in `saveSettings`, drop the local settings copy, refuse
   instead of drop, start-date and capture-clash guards, Repair's reply (MA-858, 860, 863).
3. **Hourly:** menu option, minute-only time, reload keeps it (MA-859).
4. **Card per kind:** Settings fields, read-only .eml, two-step Save, slimmed and renamed
   backup card, shared macOS note (MA-857, MA-861).
5. **Location warning** for a run-job (MA-862).
6. **Migration check** (MA-864), then the friction review.

## 11. Test-spec rows (from MA-856; the catalog ends at MA-855, `docs/scenario-catalog.md:1095`)

| Row | Type | What it proves |
|---|---|---|
| MA-856 | U | The backup kind comes from the archive's record, never the tenant: a local archive on a computer with Microsoft 365 set up installs a run-job, not a capture; mixed / Microsoft 365 / empty go the capture way; Archive now unchanged |
| MA-857 | U | The Settings card shows each kind's field set (section 3); the Microsoft 365 boxes are absent for local and encrypted; local shows .eml read-only with the job file's value; the backup card has no interval, time or sync input and says "Change in Settings" |
| MA-858 | U | One source per value: a sync save changes only the job file's `thunderbird_sync`; the settings file gains no sync field and its interval/time are untouched by a local install; a save that omits the tick-boxes leaves them as they were; the cadence shown is the descriptor's |
| MA-859 | U | Hourly is offered; an hourly schedule reloads as Hourly and a no-change save keeps it hourly; hourly with sync installs (Q11 reversed, `docs/design-thunderbird-presync.md:331,1324`) |
| MA-860 | U | Each refusal in section 5 states its reason and leaves scheduler, descriptor, job file and settings file unchanged (`assure.Refused`, positive twin first); Repair that cannot keep sync says so |
| MA-861 | U | Wherever the sync box renders, `presync.SyncNotice` is visible text beside it, and on macOS the Automation note from the one shared constant; the wizard uses the same constant |
| MA-862 | U | A location change with a local run-job names the old folder and says it is fixed; a capture keeps MA-523's wording |
| MA-863 | U | Crash order: an install refusal leaves no job file; a failed job write after install names the fix; an injected failure between install and write leaves the old job file whole |
| MA-864 | U | Migration: loading the page writes nothing; a schedule with sync on or off shows the same values in Settings; a capture on a local archive is shown, not converted |

All are unit rows; none needs real infrastructure. Lab confirmation of hourly Microsoft
365 captures stays "pending" (no lab tier).

## 12. Gates owed

- **This pre-code design review** (the 10-lens review): owed now, before slice 1, since
  the design moves which screen drives the job file the scheduler runs. Stop here.
- **Friction review:** owed before ship (shippable feature).
- **UX contract rows:** X2 (one refusal voice — the silent drop becomes a refusal), X10
  (the card shows the installed values), X11 (labels: "Scheduled backup", "Change in
  Settings"), X16 (notice wherever the box shows).
- **Adversarial pass:** not owed under the rule — no new endpoint (reuses `api/settings`
  and `api/schedule-install`) and no new writer of the job file (`scheduleInstallLocal`
  stays the only Desktop writer). **Low-confidence call:** ruling 2 makes that writer
  reachable on computers with Microsoft 365 set up, which it was not before; the reviewer
  may decide that widening needs a short pass.

**Not changed here:** a Desktop-made local schedule copies the last run's mode, so a full
last run makes every scheduled run full (`canonical.go:45-49`; read, not tested). Day of
week stays Sunday; backup alerts stay one set per computer.

---
2026-10-28, design draft from the session's research notes and the owner's rulings; every
cited line re-read on `settings-card`; nothing run.
