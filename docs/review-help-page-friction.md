# Friction review — the "How to use MailArchive" guide (MA-945) and the Windows-lab wording fixes (MA-941..944)

Process: `../assurance-kit/process/friction-review.md`. Contract: `docs/ux-contract.md` (X11, X16, X17).
Format follows `docs/review-presync-attended-friction.md`.

| Header | |
|---|---|
| Reviewed | branch `presync-attended`, commit `b454d64` (the guide, MA-945) on top of `464fddb` (MA-941, Archive now card points at Run now, sidebar Run now link), `3388963` (MA-942, a command-line schedule says why there is no Run now), `5892c86` (MA-943 Run now's end note, MA-944 guard) |
| Date | 2026-10-31 |
| Environment | macOS (the reviewer's machine), Go 1.26.0. Windows and Linux pages drawn by the Desktop's own handler with `GOOS` set to `windows` / `linux` (the tests' way); no Windows VM run |
| Actors | a brand-new person on a Mac and on Windows with no archive yet (Thunderbird mail, or a Microsoft 365 mailbox); the owner with a local Thunderbird archive (no schedule, a Desktop-made schedule, a command-line schedule); the owner with a Microsoft 365 archive; the owner with an encrypted archive; a Linux user (secondary) |
| How the pages were read | the real Overview HTML from `DashboardHandler` / `getOverview` in 18 states (3 platforms × no archive, local without schedule, local with a Desktop schedule, local with a command-line schedule, Microsoft 365, encrypted), turned to visible text (hidden parts and closed disclosures marked). The harness was a temporary test file, deleted before this commit; nothing of it is kept. The claims in the guide were then checked against the code that does the thing (`internal/schedule`, `internal/wizard`, `internal/desktop`, `packaging/`). **Not a live drill**: no browser, no Thunderbird, no wizard window, no real scheduler |
| Commands and statuses | `go test -count=1 -run 'TestHowToUseMailArchive|TestArchiveNowCardPointsAtRunNow|TestACommandLineScheduleSaysWhyNoRunNow|TestRunNowNoteLeadsWithTheBackup|TestRunNowSummary' ./internal/desktop ./internal/wizard`: ok. `go test -count=1 ./assureblock`: ok. No code changed by this review, so no package was re-run whole |
| Baseline digest | `docs/scenario-catalog.md` sha256 `d07fbd11a5a176524e2c6df1017d89dfa8ccda7e21e94882a4200ea026c19c2b` (unchanged by this review) |
| Cells walked | 15 |
| Function | 11 fully, 4 partly (cells 1, 8, 11, 12), 0 not |
| High-friction cells | 1, conditional: cell 1 (a new Mac user with no archive may have no way to reach the wizard), pending the live check C1 |
| Verdict | **SHIPPABLE once M1–M3 are applied and check C1 passes.** If C1 shows the dead end, cell 1 is **HIGH_FRICTION_NOT_SHIPPABLE** and fix H1 is owed before 1.5.2. No fix is applied in this commit (review only) |

## The walk

"Functions" is whether the words tell the person the truth and let them act. Type is the process's:
I security-inherent, II design choice, III buildable away (wording, a missing hint, a missing button).

| # | Cell (actor × scenario) | Functions? | Friction found | Type | Fix |
|---|---|---|---|---|---|
| 1 | New person, Mac, no archive, Thunderbird mail: reads "New to MailArchive?…", opens the guide, follows "Your first archive" | **partly (unverified)** | The guide says "open Mail Archive.app and choose Archive a mail file…". The Desktop this person is looking at was most likely started from that same app (the app's first window hands off to the Desktop in the same process, MA-612). Double-clicking a Mac app that is already running normally just brings it forward; it does not start a second copy, so the wizard's first window may never appear. The only in-Desktop way into the wizard (Add another archive…) is hidden until one archive is known (`CanAddArchive`). Right above the guide, the "No archive chosen yet" card says "use the MailArchive wizard (mailarchive wizard)", a terminal command a Mac app user does not have. Two different instructions, and possibly neither works. | III | H1, M3; check C1 |
| 2 | New person, Windows (installed), no archive, Thunderbird | yes | Start menu "MailArchive wizard" exists (`Product.wxs`, WizardShortcut) and runs as its own program, so it works. After the wizard, the Desktop tab is not refreshed, so "click Use this archive if this page offers it" is not offered until the person reloads. The unzipped-copy line ("open a Command Prompt in the MailArchive folder and type mailarchivew.exe wizard") is a technical step. | III | L1, L2 |
| 3 | New person, Mac or Windows, no archive, Microsoft 365 | yes | The Entra app registration (Step 1) is long and needs an admin; counted, not faulted (each organisation registers its own app: a design choice already ruled). The connection card still offers "App-only (admin)", which the Desktop never uses; the guide explains it, the card does not. | II | O3 |
| 4 | Owner, Mac, local Thunderbird archive, no schedule, wants a backup now | yes | MA-941's card is clear: "Archive now is off… It is for Microsoft 365 archives…", then "first give it a scheduled backup… Run now then appears". A one-off backup needs a schedule first. | II | O2 |
| 5 | Owner, Mac, local archive with a Desktop schedule, Run now with the sync on | yes | Sidebar Run now link works (MA-941). The guide's Run now text is true on a Mac. "Run now backs up straight away" undersells a wait of up to 20 minutes with the sync on. The sidebar shows both "Archive now" (off for this archive) and "Run now", next to each other. | III / II | L3, O1 |
| 6 | Owner, Windows, local archive with a Desktop schedule, Run now | yes | The guide says Run now does not start Thunderbird yet, and to open Thunderbird first; true (`AttendedStartAllowed` windows false). | — | none |
| 7 | Owner, Mac or Windows, local archive with a command-line schedule (MA-942) | yes | Clear why there is no Run now and how to get it; Save settings alone is refused, as the test drives. The old cadence ("weekly on Sunday at 03:00") and any `-thunderbird-sync` are not carried over: after Remove, the person must re-pick How often and re-tick the sync, and the words do not say so. | III | L4 |
| 8 | Owner, local archive, wants to add a Microsoft 365 mailbox | **partly** | The Microsoft 365 connection card on a local archive says "To set it up, run mailarchive setup in a terminal" (`dashboard.go` line 1549). The guide says: type a new folder in Settings → Archive location, Save settings, then fill in the card. Opposite instructions; the card's one needs a command a Mac app user does not have. | III | M2 |
| 9 | Owner, Microsoft 365 archive, sets a schedule | yes | Settings offers exactly "Daily, Weekly (Sunday)", as the guide says. | — | none |
| 10 | Owner, Mac, encrypted archive made in the Desktop | yes | Unlock, Run now while unlocked, the Mac encrypted sync line: true. | — | none |
| 11 | Owner, encrypted archive made at the command line | **partly** | The guide says "back it up with Run now while it is unlocked"; this archive has no Run now (no Desktop job). The Archive now card gives the right (terminal) steps. | III | L5 |
| 12 | Owner with a laptop reads when scheduled backups run | **partly** | The guide says "A time when the computer is off or asleep is skipped." Not true for asleep: Windows tasks are written with `StartWhenAvailable` true, so a run missed while asleep runs on waking (`internal/schedule/xml.go`, MA-164, README line 69 "a night the laptop merely sleeps is caught up when it wakes"). macOS's launchd also runs a missed calendar job on wake (launchd's documented behaviour, not lab-checked here). | III | M1 |
| 13 | Owner reads Run now's end note (MA-943) | yes | The first line is plain ("Backup finished: N new messages archived."). "Which ones: /path/attachments-report.tsv" is a path the person cannot click or easily open. "N messages already in the archive were saved again" may read as duplicates. | III | L6, O4 |
| 14 | Owner on Extraction workspace / Export activity clicks How to use MailArchive | yes | Goes to the Overview's card (`./#sec-help`). | — | none |
| 15 | Linux user, local archive (secondary) | yes | "MailArchive does not sync Thunderbird", the wizard can schedule, hand steps open on the Archive now card: consistent. | — | none |

## Findings and proposed fixes

None is applied in this commit.

**H1 — a new Mac user may have no way to start the wizard (cell 1). High if C1 confirms it.**
Proposed fix: with no archive known, show a button on the "No archive chosen yet" card,
"Make an archive from mail files…", that runs the same in-process flow as Add another archive…
(`wizard.AddArchive`). Then point the guide's Mac and Windows lines at that button first ("click
Make an archive from mail files… above"), keeping the app / Start menu route as the second way. This
adds a control, so it needs its own test (it works with no archive known; a cancel adds nothing),
a catalog row, and an owner yes.

**M1 — "off or asleep is skipped" is untrue for asleep (cell 12). Medium.**
Proposed words: "If the computer is asleep at that time, the backup runs when it wakes. If it is
turned off, that time is skipped." Check C2 confirms the Mac half first. The wizard says the same
untrue thing (`wizard.go` lines 1800 and 1866); fix it in the same pass.

**M2 — the Microsoft 365 card on a local archive sends people to a terminal (cell 8). Medium.**
Proposed words for `dashboard.go` line 1549: "Not used by this archive: it was made from mail files on
this computer. To back up a Microsoft 365 mailbox, keep it in an archive of its own: in Settings →
Archive location, type a new folder and click Save settings, then fill in this card. How to use
MailArchive → Microsoft 365 has the steps." (Same path the guide gives; pin it in the MA-945 test so
the two cannot drift.)

**M3 — the "No archive chosen yet" card names a terminal command (cells 1, 2). Medium.**
Its paragraph says "use the MailArchive wizard (mailarchive wizard)". Proposed: draw that sentence
from the guide's own per-platform words (`helpDataFor(goos).WizardHow`), so the card and the guide say
the same thing on each platform. The "Other ways to add new mail" disclosure on the Archive now card
(MA-546) has the same command; it is now behind a disclosure, so leave it, or add the same
per-platform sentence in front of it.

**L1 — "Use this archive" needs a reload (cell 2).** Proposed: "Then come back to this page,
reload it, and click Use this archive…".

**L2 — the unzipped Windows copy (cell 2).** Proposed: "…there is no Start menu entry: open the
MailArchive folder, click in the address bar, type cmd and press Enter, then type mailarchivew.exe
wizard and press Enter." (Spells out how to get the Command Prompt there.)

**L3 — "Run now backs up straight away" (cell 5).** Proposed: "Run now starts a backup now (with
the Thunderbird sync on, it first waits for Thunderbird's mail; see Thunderbird below)."

**L4 — command-line schedule replaced (cell 7).** Proposed addition to MA-942's line: "Note its
time first: How often and Sync Thunderbird are not carried over."

**L5 — encrypted archive made at the command line (cell 11).** Proposed: "…back it up with Run
now while it is unlocked (an encrypted archive made at the command line is backed up there; its
Archive now card says how)."

**L6 — the report path in Run now's note (cell 13).** Proposed: "Which ones: the file
attachments-report.tsv in the archive folder (it opens in a spreadsheet program)." Or link it.

**L7 — small wording in the guide.**
- "…this archive was made from (Thunderbird keeps its profiles in ~/Library/Thunderbird/Profiles.)"
  has no full stop before the bracket. Proposed: "…was made from. (Thunderbird keeps its profiles in
  ….)" On a Mac, Library is hidden in Finder; consider "the Library folder inside your home folder".
- "Hourly: With Hourly and the Thunderbird sync, …" says Hourly twice. Proposed: "Hourly: with the
  Thunderbird sync on, Thunderbird stays open…".
- "It is off until you turn it on: in Settings…": the wizard also asks about the sync when it makes
  the archive. Proposed: "The wizard asks about it when it makes the archive; you can change it later
  in Settings: …".
- "Backup alerts" in the parts list has no link, while every other part shown on the page does.
  Proposed: link it to `#sec-alerts` except on an encrypted archive (the card is absent there).

## For the owner (matters of taste, with a recommendation)

- **O1. Two sidebar entries, "Archive now" and "Run now", on a local archive.** Archive now is off
  there. Recommendation: on a local archive, hide the sidebar's Archive now entry when Run now is
  shown. Small, and it removes the wrong first click the Windows lab hit.
- **O2. A one-off backup needs a schedule first.** Recommendation: keep it for 1.5.2 (Run now runs
  the scheduled job by design); revisit after the release.
- **O3. The connection card offers App-only, which the Desktop never uses.** Recommendation: keep
  the choice, but add one line under it on the card: "MailArchive Desktop always signs in as you;
  App-only is for the command line." The guide already says this.
- **O4. "saved again" in Run now's note.** Recommendation: add "(no copies are made)" if that is
  true of the exporter; not checked here.
- **O5. Getting started is long when it opens by itself.** About 600 words and ten parts, most of
  which are not on a page with no archive yet. Recommendation: keep "Your first archive" in Getting
  started and move "The parts of this page" into a fourth part, closed. Not needed for 1.5.2.

## Checks the owner can do (smallest first)

- **C1 (decides H1).** On a Mac, open Mail Archive.app, choose the MailArchive Desktop item so the
  page opens. Then double-click Mail Archive.app again in Applications. Does a "What would you like to
  do?" window appear? If not, H1 is needed.
- **C2 (confirms M1 on a Mac).** Schedule a backup two minutes ahead, put the Mac to sleep past that
  time, wake it, and look at Recent backups: a run right after waking means "asleep is skipped" is
  wrong on the Mac too.

## Not checked

- No live drill: no browser, no Thunderbird, no wizard window, no real scheduler. Pages were drawn by
  the Desktop's own handler in a test harness, Windows and Linux through `GOOS`.
- The Mac relaunch behaviour (C1) and launchd's catch-up on wake (C2) are from general macOS behaviour,
  not observed.
- A command-line schedule made with its own `-name`: the Desktop's Remove finds the schedule by the
  default name for the folder (`removeTaskAt` → `DefaultNameFor`), so Remove may report success and
  leave such a schedule in place. Not driven; worth a test before MA-942's words send people to Remove.
- Whether the Settings save reloads the page so Run now appears without a manual reload (cell 4).
- The Windows Start menu layout (the wizard shortcut sits in a "MailArchive Desktop" folder under All
  apps) was read from `Product.wxs`, not seen.

## Disposition (added 2026-10-31, after the review; the review above is unchanged)

Applied test-first in the commits that follow this review, together with a truth review of the
guide against the code (its findings are named T-n here). Catalog: MA-945, MA-938, MA-942 and
MA-943 amended; MA-946 new.

| Finding | Outcome |
|---|---|
| M1 / T2 asleep is skipped | Applied. Guide: Mac and Windows "switched off is skipped; if it was asleep, the backup runs when it wakes up"; Linux (cron) keeps "off or asleep". The wizard's question and the Mac end note say the same (MA-946). The Mac half rests on launchd.plist(5) as read on this Mac; C2 would observe it |
| M2 / T6 Microsoft 365 card | Applied: the card gives the guide's steps; the test drives Save settings with a new folder and sees the form |
| M3 no-archive card command | Applied: the card uses the guide's per-platform wizard words |
| L1 / T4 reload, wizard sync question | Applied |
| L2 unzipped Windows | Applied (address bar, cmd, Enter) |
| L3 Run now "straight away" | Applied: "starts a backup now", with the wait named only where Run now starts Thunderbird |
| L4 command-line schedule not carried over | Applied: the Scheduled backup card says how it runs now and that Remove does not keep it (and the sync, when on) |
| L5 encrypted archive made at the command line | Applied |
| L6 report path | Applied: "the file attachments-report.tsv in the archive's folder (it opens in a spreadsheet program)" |
| L7 full stop, Hourly twice, sync "off until", alerts link | Applied. The "Library is hidden in Finder" rewording was a "consider" and is not taken |
| T1 alerts boxes and test email | Applied, in Getting started and the Thunderbird 3-day line |
| T3 Shut down, come back | Applied per platform |
| T5 sync turned on in the wizard | Applied (same as L7's third point) |
| T7 not-downloaded messages | Applied |
| T8 Thunderbird's default | The unchecked claim "Thunderbird ticks it for a new account" is removed; the wizard's offline-download offer is named and counted as an exception to read-only. Held: whether Thunderbird ticks it by default (a lab check; the wizard's own IMAP dialog says the opposite) |
| T9 recovery key | Applied |
| H1 / T10 / C1 Mac wizard after the Desktop is open | Held for the owner: C1 decides it, and H1 adds a button, which needs the owner's yes |
| O1–O5 | Held for the owner (taste) |
| Remove and a custom-named command-line schedule | Held, open. Read in the code, not driven: Remove reaches only the entry named for the folder (`removeTaskAt` → `DefaultNameFor`), and the page says "Removed." for any `ok:true` reply, including `removed:false`. A schedule set up with its own `-name` would stay, and MA-942's words would send its owner to a Remove that does not reach it. A fix changes what Remove removes, so it needs its own test and design |
| Security & privacy's "one exception" | Noticed, not changed: that card names only the Thunderbird sync as the exception to read-only; the guide now also names the wizard's offline-download offer (a Thunderbird setting, not mail). Whether the card should too is the owner's wording call |

### Later the same day (2026-10-31), added beside the rows above; those rows are unchanged

| Finding | Outcome |
|---|---|
| T8 Thunderbird's default (was held) | Settled by the Windows lab (lab note in `docs/lab/presync-attended-start-windows-run1-2026-10-31.md`): Thunderbird 153.6.0esr, a Gmail IMAP account added with no settings changed, no `offline_download` line in prefs.js, over 36,000 messages saved to disk in the first half hour. The guide now says Thunderbird turns the setting on for a new account, and that downloading takes time and happens only while Thunderbird is open (MA-945 amended). The wizard's IMAP question no longer says Thunderbird keeps mail "only on demand" (MA-947). README's two IMAP lines say the same. Noted, not changed: the code reads an account with no such line as "off", so Yes, prepare writes the setting out anyway; harmless |
| Remove and a custom-named command-line schedule (was held, open) | Real, now driven (MA-948): Remove's reply was `{"ok":true,"removed":false}` and the page said "Removed.". Remove now says when it removed nothing; for a schedule under its own name it names it and gives the command that removes it (only when the name binds to this archive; otherwise generic words). The Scheduled backup card and Settings no longer send that owner to Remove, and Remove is not shown for it (MA-942 amended). Remove still never acts on the record's name: making it do so would widen what it deletes from an untrusted record, so it is left for its own security pass, not done here |
| O1 sidebar Archive now beside Run now | Applied by the lead, 2026-10-31, owner may revert: on an archive of mail files with Run now on the page, the sidebar no longer shows Archive now; its card stays (MA-941 amended) |
| O2 a one-off backup needs a schedule first | Owner ruling, 2026-10-31 ("It may be fine to have a manual archive where someone updates at their own schedule when they desire to do so", then "yes, 1.5.2"): How often gains "Only when I click Run now" in 1.5.2 — the backup is saved with no timer, as an encrypted archive's already is, so Run now works without a schedule. Built separately |
| O3 App-only on the connection card | Applied by the lead, 2026-10-31, owner may revert, after checking it is true (Archive now and the scheduled capture use `Auth: "device"`, the sign-in is `graph.NewDelegated`): under App-only the card says "MailArchive Desktop always signs in as you; App-only is for the command line." (MA-945 amended) |
| O4 "saved again" | Applied by the lead, 2026-10-31, owner may revert, after checking it is true for the archives Run now backs up (a message written again replaces its record and page; the one keep-the-old-file path is Microsoft 365's only), and driving it on a real mbox folder: "…were saved again (no copies are made)." (MA-943 amended) |
| O5 Getting started is long | Applied by the lead, 2026-10-31, owner may revert: "Your first archive" stays in Getting started; "The parts of this page" is a part of its own, closed (MA-945 amended) |
