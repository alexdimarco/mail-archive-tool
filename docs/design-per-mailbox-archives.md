# One archive for each mailbox: the choice after "All of them"

**Status:** DRAFT, revision 1, 2026-11-23, for the pre-code design review
(`../assurance-kit/process/design-review.md`; the review is owed because one add now makes several
archives, `AddArchive`'s contract changes, and text from Thunderbird's settings becomes a folder
name). Branch `per-mailbox-archives`, off main `10fcc2e` (release 1.7.5). Nothing is built yet.
The release number is decided at release time (1.8 was earmarked for Gmail de-duplication, which
is not merged yet).

Sources this revision drew on, each read on 2026-11-23 unless said otherwise: the code at `10fcc2e`
(the lines cited below); the suggestion workflow of 2026-11-19
(`.claude/notes/archive-per-account-workflow-2026-11-19.json`, untracked: its synthesis and its
checker, which read the code at `ed491c4`, release 1.7.4); `docs/design-1.7.5-fixes.md`, which
shipped the first of the owner's four answers; and the scenario catalog. Where this document leans
on the 2026-11-19 checker without re-reading the code, it says so.

## 0. The owner's words

- 2026-11-19, the owner's report (verbatim, from the session's notes): "if you start fresh with
  mail-archive have it autodetect all the thunderbird accounts and select all - it will create a
  giant archive of all the email accounts blended into one archive. … Suggestions?"
- 2026-11-19, the owner's answer to the four suggestions: "yes on all 4 - make it so". The four,
  as the session's notes record them (the questions' exact wording is not in the repository):
  1. Fix the combined-archive bug first, as 1.7.5. **Shipped in 1.7.5 on 2026-11-21.**
  2. After "All of them", ask "One archive for all of them" (preselected) or "One archive for each
     mailbox". **This design.**
  3. Per-mailbox archive folders are named by the email address. The owner accepts the address in
     job files, logs and alert email subjects; this overrides S85's and MA-1288's "written nowhere"
     for folder names only. **This design, §4.**
  4. Ship per-mailbox archives before the shared Thunderbird sync. **This design, §8: no
     Thunderbird sync is offered for a set.**
  The notes also say the release of per-mailbox archives is covered by these words.
- 2026-11-23: "ok - lets get that fixed" (after the session reported that parts 2 to 4 were not
  built).

One question in this design is not covered by those words and is put to the owner: **QA** (§12).

## 1. Today

- In the wizard, "What are you exporting?" preselects "Auto-detect my mailboxes"
  (`internal/wizard/wizard.go:780`). Auto-detect runs `app.DiscoverInputs(nil, true)` (`:789`).
- When two or more mailboxes are found, the list "Which mailbox do you want to export?" opens with
  "➤ All of them (and any added later)" preselected, then one row per mailbox, worded by
  `pickList` (`internal/wizard/picker.go:72-99`, `:661-707`). One mailbox found: no list (`:73-75`).
- "All of them" returns every found path with `auto` on; one row returns its own path with `auto`
  off (`picker.go:85-88`).
- Then one folder question, "Select the output folder" (`wizard.go:880`), one set of answers
  (`:890-969`), one export (`runExport`, `:1794-1927`), one summary (`exportSummaryFor`,
  `:1939-2016`) and one schedule question (`offerSchedule`, `:2169-2228`).
- So "All of them" makes one archive holding every account's mail. Since 1.7.5, one missing
  mailbox no longer stops it (MA-1471). The person is never asked whether they wanted that.
- MailArchive Desktop's **Make a new archive…** runs the same flow in process through
  `AddArchive` (`wizard.go:701-710`), which returns one folder; the Desktop registers that one
  folder (`internal/desktop/addarchive.go:41-69`).
- **Make a new encrypted archive…** runs the same flow with a `keyed` hook, which takes the answers
  in place of the export (`wizard.go:974-977`; `internal/wizard/addkeyed.go:47`). It also reaches
  the mailbox list.

## 2. What changes, in one paragraph

After "All of them", and only when two or more mailboxes would each get an archive, the wizard asks
one more question: one archive for all of them (today's result, preselected) or one archive for
each mailbox. On the second answer it asks for one parent folder, shows a plan of the archives it
will make, asks today's other questions once, makes ordinary archives one after another (each one
mailbox, auto-discovery off), shows one summary, and asks the schedule question once. Each archive
gets its own job file and scheduled task, so each can be paused, moved, repaired or deleted alone.
Nothing about the archive format, the engine or existing archives changes.

## 3. The question

**Where.** In `pickAutoInputs`, after the answer "All of them", when all of these hold:
- the flow is the plaintext one (`keyed == nil`): never in **Make a new encrypted archive…** (the
  2026-11-19 checker's flow problem 2: the encrypted flow reaches the list, so the gate must be
  explicit);
- the Outlook-app route is not taken (that route never shows the list, `wizard.go:808-820`;
  unchanged);
- two or more rows would get an archive by §5's rule. One account plus an empty feeds folder stays
  one archive, as today, and the question is not shown.

**Words** (fixed text, none of `& < > \`, as MA-1286 requires of a list's prompt; Windows' list
shows the prompt in a scrolling box since 1.7.4):

> Keep these mailboxes together in one archive, or give each one its own archive?
>
> One archive: one place to search all your mail. A mailbox you add to Thunderbird later joins it
> by itself.
>
> One archive for each mailbox: each can be searched, paused, moved or deleted on its own, and a
> problem with one does not stop the others. A mailbox you add later is not backed up until you
> run this again with the same folder.

Rows: **"One archive for all of them"** and **"One archive for each mailbox"**.

**Default.** "One archive for all of them" is preselected on Windows and macOS, so a click-through
gives today's result. Linux preselects nothing (`third_party/zenity/list.go:61-69`); an answer that
names no row shows `pickAgain` and the list again, up to `pickTries`, as `askSchedule` does
(`wizard.go:2136-2167`, the shared `rowChoice`). Cancel ends the flow, as Cancel on the list does
today.

**"One archive for all of them"** continues exactly as today: the same paths and `auto` on.

## 4. Folder names

Each archive's folder is made inside the parent folder the person picks (§6) and is named for its
mailbox, as the owner ruled (§0, answer 3).

**The name** is the row's own identity text as `pickIdents` gives it (`picker.go:402-564`), without
the row's details in brackets (size, last changed, profile) and without its lead word:
- a Thunderbird account: the name Thunderbird shows for it, usually the address
  (`state.CleanFolderName`, already made distinct within a profile by `TBDistinctNames`);
- Local Folders and feeds: their names as shown ("Local Folders");
- an Outlook data file: its file name without `.pst` or `.ost`, then " (Outlook)";
- an Evolution store: its store name, then " (Evolution)";
- a folder no account uses now: its name, then " (not in Thunderbird now)";
- an older copy: its name, then " (older copy)";
- a mailbox with no usable name: MailArchive's own words, "Folder with no name" or "File with no
  name", as the list says.

**Made safe.** The name then passes `util.StripControl`, `state.CleanFolderName` and
`util.SanitizeSegment` (`internal/util/paths.go:52-59`: illegal characters, reserved device names
such as `CON`, trailing dots and spaces, length), and is cut to 60 characters on a character
boundary. A name that comes out empty, `.` or `..` takes "Mailbox". Text from `prefs.js` is never
used as a path in any other way: never joined with a separator, never resolved.

**Told apart.** Two names that are the same after NFKC and case folding (`state.TBTextKey`), or,
where answers may be transcoded, after `asciiForm` too, get " (2)", " (3)" and so on, in the
plan's order. Case folding matters because Windows and macOS folders ignore case (the checker's
"missed" item on `SanitizeSegment` keeping case). A name that matches an existing entry in the
parent folder is handled by §7's rule before any number is added.

**What the owner's ruling costs, stated:** the address then appears in the archive's path, so also
in its job file, its scheduled task's name and log path, the run log's line "Scheduled job …:
exporting to <out>" (`wizard.go:338`), the Windows wrapper's error-log path
(`internal/schedule/wrapper.go:73-74`), and alert email subjects, which name the archive by its
path (`internal/alert/alert.go:184-206`; `header` strips CR and LF from it). S85 ("the words
are written nowhere") and MA-1288 ("no log file holds the name") are amended for this case only:
the account's name may appear where the archive's own folder path appears. "One archive for all of
them" and single-mailbox archives are unchanged: their jobs still hold no account name (the
`ma1462_picker_test.go` assertion stays).

## 5. Which mailboxes get an archive

After "One archive for each mailbox", each row the list showed gets an archive, in the list's
order, except:
- an **empty** row (counted in full, size 0) that is a feeds account, a folder no account uses
  now, or an older copy: nothing to keep, and nothing will arrive. The plan says "Not made:
  <name>, which is empty."

Kept on purpose:
- **Local Folders always gets an archive, even when empty**: POP mail, filters and mail a person
  moves off the server land there (the checker's "missed" item 1).
- An account whose folder says "nothing downloaded yet" gets an archive: its mail arrives later.
- A row whose size was not counted (the 3-second budget ran out, MA-1466) gets an archive.

**Saved once, never scheduled:** a folder no account uses now and an older copy. No account writes
to them, and a schedule on one would only fail, then pause itself, the day the leftover is cleaned
up. The plan and the summary say "saved once".

Each archive is an ordinary one: `Inputs` holds that one path, `Auto` is off.

## 6. The parent folder

Asked once, in place of "Select the output folder", titled **"Select the folder to keep the
archives in (a folder is made inside it for each mailbox)"**.

Refused, with the folder question asked again (nothing is created):
- a folder that is an archive, or is inside one, plaintext or encrypted: "<folder> is an archive,
  or is inside one, so the new archives cannot go there. Choose a folder outside every archive.";
- MailArchive's settings folder, or a folder inside it (`encmark.RefuseSettingsWrite`'s rule).

The check reuses the copy export's destination check, which judges "outside every archive" by path
and by identity (`internal/app/copyexport_folder.go:64-112`, `CheckDestination`, given the archives
the wizard knows: the Desktop's list read with `archindex.Read`, the wizard's own last archive, and
the current one). Each archive folder is then made with the same primitive, `CreateFolder`: through
a handle on the parent opened before the check, so a parent swapped for a link after the check
cannot move a new folder into an archive.

The parent may hold other things (a person may pick Documents). MailArchive writes nothing in the
parent except the archive folders.

## 7. A folder that is already there

Running the same choice into the same parent again is how a person adds mailboxes found since, and
brings the others up to date. For each archive, when `<parent>/<name>` already exists:
- **an archive of this same mailbox** (its manifest's store ids include this mailbox's id,
  `path:` plus its folder as `app` writes it): continued, with the answers given now. The plan
  says "brought up to date". The store ids are read the safe way: `archindex`'s bounded streaming
  read inside an `os.Root`, ids compared lexically, never `state.Load` by path, which resolves an
  untrusted manifest's ids on disk (SD-1, SD-2; the checker's "missed" item 4). A new exported
  `archindex.Stores(folder) ([]string, bool)` returns them from the walk `inspect` already does.
- **that archive, paused** (sealed read-only): left as it is. The plan says "paused: left as it
  is", and it gets no schedule (a paused archive takes no write, no lock and no record,
  `internal/app/run.go:1483-1488`, `seal.IsSealed`).
- **anything else** (another mailbox's archive, an encrypted archive, a folder or file that is not
  an archive): the new archive takes the next free name with " (2)", " (3)".
- **a folder whose delete did not finish** (`app.RefuseUnfinishedDelete`): that archive is not
  made; the plan says why and how to finish the delete.

## 8. The plan, then one set of answers

**The plan** comes right after the parent folder, before any other question and before anything is
written (UX contract X14: look before writing). A question box, buttons **"Make the archives"** and
**"Cancel"**. Cancel writes nothing. Example (addresses invented):

> MailArchive will make 4 archives in D:\Mail archives:
>
> qzsam.walker@gmail.com
> sam.walker@contoso.example
> Local Folders
> imap.gmail.com (not in Thunderbird now): saved once and not kept current, since no account uses it
>
> Brought up to date: sam@old.example (already in this folder)
> Not made: Feeds, which is empty.

The text carries account names and paths. It goes through `messageDialog`/`zenity.Question` as the
summary does; on Linux those pass `--no-markup` (`third_party/zenity/msg_unix.go:8`), so a name is
shown as written.

**The IMAP download question** is asked for each Thunderbird IMAP account that gets an archive, as
today (`wizard.go:859-877`), in the plan's order. Today it does not name the account; with three
accounts it is asked three times in the same words. Whether to name it is **QA** (§12).

**Asked once for all the archives**, as today and with today's defaults: "Export mode:", the date
window, "Is your mail app currently open?" (only when an Outlook file is among them), the `.eml`
question and the folder-links question. An archive whose account was just prepared runs in Full
mode, as today; the others take the mode chosen. The mode question is skipped only when every
archive's account was prepared.

## 9. Making the archives

One after another, in the plan's order, each through today's `runExport` steps (the unfinished-
delete and keyless refusals, the run log, `app.Run`) with `Inputs` its one path and `Auto` off.

- **Progress:** one progress window per archive, titled "Exporting 2 of 4…", with today's counts
  line. No account name in the progress text.
- **Cancel** on a progress window stops that archive, keeps what it copied (as today), and starts
  no more. The summary lists which finished, which stopped and which were not started.
- **An error** in one archive (not a cancel) does not stop the others. Its summary line is the
  error's first line and its log's path. This differs from a single archive, whose error ends the
  wizard (`wizard.go:1900-1906`).
- `rememberArchive` is called for each finished archive, as today, and the made-list (§10) gets
  each folder that holds an archive.

**The summary** is one window, buttons **"Open the folder"** (opens the parent) and **"Close"**:

> 4 archives are ready in D:\Mail archives
>
> qzsam.walker@gmail.com: Backup complete. 48,210 messages.
> sam.walker@contoso.example: Backup complete. 9,870 messages. Not fully downloaded yet: 12 —
> open these in your mail app so it downloads them, then run this again to add them.
> Local Folders: Backup complete. 1,204 messages.
> imap.gmail.com (not in Thunderbird now): saved once. 3,311 messages.
>
> Each archive is its own folder: you can move, hand over or delete one without touching the
> others. Open index.html in any of them to browse.

Each archive's line keeps the not-done, empty-at-source, kept-as-received and index-error counts
that `exportSummaryFor` shows for one archive today, each with its remedy, so the GUI says the same
counts as `status` (X6; the checker's flow problem 4). Only lines with a count above zero appear.
The lead per archive is that archive's `tbcomplete.RunLead`. Two lines appear only when they apply:
"<name>: Did not finish: <reason>. The mail copied so far is kept; run this again with the same
folder to finish it." and §11's line about an existing combined archive.

## 10. MailArchive Desktop and the stand-alone wizard

**Make a new archive…** `AddArchive` is replaced by `AddArchives() ([]string, error)`: every folder
the flow made, in the plan's order; one folder for a single archive. `addOutcome`'s rules apply per
set: nothing made and Cancel is "nothing added"; a set where any archive stopped part way returns
`ErrStoppedEarly` with every folder that holds an archive. The Desktop handler registers them all in
one `archindex.UpdateFile` with `Reconcile` (which already takes a list,
`internal/archindex/index.go:342`) and makes none of them current (as today, MA-696). Its note:
"4 archives added — they are now in Your archives." A set stopped part way: "The wizard was stopped
before it finished. The archives it made are listed under Your archives once you reload this page;
to finish, click Make a new archive… again and choose the same mailboxes and the same folder."

**The stand-alone wizard's archives.** Today the Desktop learns of a wizard-made archive only through
`last-archive.txt`, which holds one path (`internal/desktop/archives.go:130-143`,
`wizard.go:1171-1176`). A set would show only its last archive. The wizard therefore also appends
each archive it makes to a **made-list**, `wizard-made.txt` in the settings folder: one absolute
path per line, written 0600 under `encmark.RefuseSettingsWrite`, kept to its last 200 lines and
64 KB. The Desktop's `archivePointers` adds each line it reads (read bounded: at most 64 KB, lines
that are not absolute paths ignored). `Reconcile` only adds and skips duplicates, so reading it on
each page load is safe. `last-archive.txt` keeps one path, so an older Desktop keeps working (it
lists one archive of the set; the others' schedules still run).

**The launch health view** of the stand-alone wizard still covers one archive (`wizard.go:682-686`,
`:1223`). The set's end note says: "To see how all of them are doing, open MailArchive Desktop." A
health view of every archive is later work (§13).

**Rows in Your archives** are unchanged. Each row shows its folder's path, which now ends with the
mailbox's name.

## 11. Schedules, the Thunderbird sync, and existing archives

**The schedule question** is asked once for the set, when at least one archive finished and may be
scheduled (not saved-once, not paused, not stopped part way, not failed):

> Keep these archives current automatically?
>
> Each archive gets its own scheduled backup. They start 10 minutes apart, from the time you
> choose, so they do not all run at once. A scheduled run repeats exactly this export
> (incremental: only new mail and any content that was still missing). It runs only while you
> are signed in. <today's sentence for this system>

The rows and the rule are today's (MA-1474): only an exact Yes row installs anything. Each archive
then gets `installOffered`, its own job file and its own scheduler entry, named from its own path
(`schedule.DefaultNameFor`), so nothing clashes. The first starts at the chosen time (02:00 daily or
Sunday 03:00); each next one 10 minutes later. Why: several first-time or large backups at once
compete for disk, processor and memory. A computer that slept through the time runs every missed
one at wake, together; that is a known limit (§13). End note:

> Scheduled: daily from 02:00, one archive every 10 minutes, for 3 archives in D:\Mail archives.
> Each archive keeps its own log, mailarchive.log, in its own folder. These backups save the mail
> Thunderbird has already downloaded to this computer; they do not start Thunderbird first.
> Not scheduled: imap.gmail.com (not in Thunderbird now), saved once.
> To see how all of them are doing, open MailArchive Desktop.

**The Thunderbird sync** is not offered for a set (the owner's answer 4). Today's code would start
Thunderbird once per archive, or leave all but one archive "profile busy": the profile lock is tried
once with no wait (`internal/presync/session.go:207-212`, per the checker). Archives without the
sync never take that lock (`internal/wizard/jobrun.go:379-412`). A shared sync is its own design.

**The Settings card's sync switch** gains a guard, for any archive (a set's or not): it refuses to
switch the sync on when another known archive's job already syncs the same Thunderbird profile,
naming that archive: "Thunderbird sync is already on for D:\Mail archives\qzsam.walker@gmail.com,
which backs up the same Thunderbird profile. Only one archive can sync a profile for now: switch it
off there first." The profile of each job comes from its inputs (`thunderbird.FindProfileDir`), the
known archives from the Desktop's list, and each job file is read with `job.Read` from the settings
folder. Archives already set up both ways are left as they are.

**Existing combined archives** are never split, moved, renamed or rewritten. When a known archive's
job has `Auto` on, or lists one of the set's mailboxes, the summary adds: "Your archive <path>
already backs up these mailboxes together. To stop backing them up twice, click Pause (make
read-only) on its row in MailArchive Desktop; its mail stays readable." It reads job files only,
never a manifest.

**The command line (R33).** Every archive in a set is an ordinary one that `mailarchive export
-input PATH -out DIR` makes; `mailarchive export -auto -list` lists the mailboxes found. No new verb
is added. A single command that makes a whole set is later work (§13).

## 12. Question for the owner

**QA.** With one archive for each mailbox, the "prepare a full offline download?" question is asked
once per IMAP account in the same words, so a person with three Gmail accounts cannot tell which one
each question is about. May it name the account? It would start "Account 1 of 3:
qzsam.walker@gmail.com" and end "Prepare a full offline download of qzsam.walker@gmail.com first?",
on both paths (one archive and one per mailbox). It changes the pinned wiring checks MA-947 and
MA-1267, and the 1.7.3 friction review filed it as needing your word (its F12). **Recommended: yes.**

## 13. Not in this change (later, each with its own design or review)

- A Thunderbird sync shared by a set (start Thunderbird once per profile; a bounded wait on the
  profile lock).
- A "Mailboxes not backed up" card for accounts added after a set was made.
- Searching across archives; a search narrowed to one account inside a combined archive.
- A tool that splits an existing combined archive.
- Encrypted sets (one password for several archives is a new vault design).
- A command-line flag that makes a set.
- The stand-alone wizard's health view of every archive in a set.
- Backups that a sleeping computer missed all start together at wake (each scheduler runs a missed
  time once at wake); one-at-a-time scheduled runs would need a machine-wide lock.
- The Outlook-app route's question that comes before the list (the 1.7.3 friction review's F13).

## 14. Risks kept

- A mailbox added later is not backed up by a set until the person runs the same choice into the
  same folder again. The question says so; nothing reminds them later (the card in §13 would).
- Notices multiply: one shared cause, such as an unplugged backup drive, sends one failure email per
  archive.
- No one search across all mail on the per-mailbox path.
- A set has no Thunderbird sync, so an IMAP account Thunderbird rarely opens falls behind.
- The address in folder names, task names, logs and alert subjects (§4), as the owner accepted.
- An older Desktop lists only one archive of a set made by the stand-alone wizard.
- Path length: the archive folder adds up to 60 characters above every message path. Not measured.

## 15. Rows

New scenario **S90**: "Auto-detect finds several mailboxes, the person picks All of them, then One
archive for each mailbox". New rows (MA-1482 to MA-1489 are left free for 1.7.5's follow-ups):

| Row | Tier | What it proves |
|---|---|---|
| MA-1490 | U | The question appears only after All of them, only with `keyed == nil`, only when two or more rows get an archive; One archive is preselected on Windows and macOS; an empty answer asks again; One archive gives today's paths and `auto` on. |
| MA-1491 | U | Which rows get an archive (§5), from `pickRow`s: empty feeds and leftovers are not made, Local Folders always is, leftovers are saved once. |
| MA-1492 | U | Folder names (§4): the account's name; suffixes; " (2)" after case folding and NFKC; hostile names (`..`, `/`, `\`, `CON`, trailing dot, control and right-to-left characters, 300 characters) come out as one safe segment of at most 60 characters. Adversarial. |
| MA-1493 | U | The parent folder: inside or an archive (plaintext and encrypted), the settings folder: refused with the words and asked again, nothing created; positive twin first. A parent swapped for a link between the check and the make is refused. |
| MA-1494 | U | A folder already there (§7): the same mailbox's archive is continued; a paused one is left alone; anything else gets " (2)"; store ids are read the safe way (a planted UNC id opens nothing). |
| MA-1495 | U | The plan lists every archive, every "brought up to date", "paused" and "not made" line; Cancel writes nothing (side effect absent). |
| MA-1496 | U | Making the set: one input each, `auto` off, one at a time; Cancel stops the current one and starts no more; an error in one goes on to the next; the summary keeps each archive's counts. |
| MA-1497 | U | The schedule once for the set: only an exact Yes row installs; one job and entry per scheduled archive, 10 minutes apart; none for saved-once, paused, stopped or failed archives; no `thunderbird_sync` in any job. |
| MA-1498 | U | No sync question for a set; the Settings guard refuses a second archive's sync for one profile and names the other; its positive twin switches on where no other archive syncs. |
| MA-1499 | U | `AddArchives` returns every folder made; the Desktop registers them all, none current; the stopped-part-way note. |
| MA-1500 | U | The made-list: 0600, settings folder only, 200 lines and 64 KB kept; the Desktop reads it bounded and lists each archive. |
| MA-1501 | U | The existing-combined-archive line appears when a known job has `Auto` or a shared input, and not otherwise. |
| MA-1502 | U | (If QA is yes.) The download question names the account, on both paths. |
| MA-1503 | L | Lab, pending: Windows test PC (the question and Enter, the plan at 150 %, several Task Scheduler entries 10 minutes apart, address-named folders through the batch wrapper, a scheduled run of each), macOS and Linux walks in MA-1467's style. |

Amended (nothing weakened): S85 and MA-1288 ("written nowhere" holds except where an archive's own
folder path is named by its mailbox, the owner's 2026-11-19 ruling); MA-696 (Make a new archive…
may add several); MA-1474 (the set's schedule question uses the same rows and rule).

## 16. Reviews owed

- This design: the 10-lens pre-code review, before any code.
- After the build: an adversarial pass (account names into paths; the parent-folder check; the safe
  read of an existing folder; the made-list; the Settings guard), a friction review of every new
  window's words, and the Windows lab (MA-1503) on the office test PC, as an ordinary user.
- UX contract: X14 (the plan), X6 (the summary's counts), MA-1286's scan of the new prompt.
