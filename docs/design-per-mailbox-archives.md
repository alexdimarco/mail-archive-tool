# One archive for each mailbox: the choice after "All of them"

**Status:** DRAFT, revision 2, 2026-11-24. Revision 1 (commit `9962aff`, sha256
`b404466c…ae303`) was reviewed in `docs/review-per-mailbox-archives-predesign.md`: GO WITH
CONDITIONS, 32 conditions (C1 to C32) and two from the rescue (R1, R2), no owner question left.
This revision folds every one of them; §17 maps each condition to the text that carries it. Each
changed passage is marked *Rev 2* with its condition. Revision 1's words are kept in git at
`9962aff` rather than beside each change, to keep this document readable; the review quotes every
passage it faulted. A fold check (the procedure's step 4) is owed before any code. Branch
`per-mailbox-archives`, off main `10fcc2e` (release 1.7.5). Nothing is built yet. The release
number is decided at release time (1.8 was earmarked for Gmail de-duplication, not merged yet).

**Revision 2.1, 2026-11-24:** the fold check of revision 2 (filed in the review, "Fold check of
revision 2") said CLEARED AFTER THESE FIXES: F-01 (HIGH), F-02 to F-05 (MEDIUM), citations 1 and 4,
and the LOW items F-06 to F-12. This revision applies all of them; each is marked *Rev 2.1* with its
F-number beside the text it changes, and §17 lists them. The lead checked each fix against the fold
check's wording (F-01's code claim read at `internal/thunderbird/thunderbird.go:840-890`). With
that, the design review's conditions are met and the build may start, in §16's order.

Sources this revision drew on, read on 2026-11-23 and 2026-11-24 unless said otherwise: the code at
`10fcc2e` (the lines cited); the suggestion workflow of 2026-11-19
(`.claude/notes/archive-per-account-workflow-2026-11-19.json`, untracked; its checker read the code
at `ed491c4`, release 1.7.4); `docs/design-1.7.5-fixes.md`; the scenario catalog; and the review's
five reports.

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
  4. Ship per-mailbox archives before the shared Thunderbird sync. **This design, §11: no
     Thunderbird sync is offered for a set.**
  The notes also say the release of per-mailbox archives is covered by these words.
- 2026-11-20, the owner's answer to 1.7.5's Q7 (a Thunderbird account moved, not removed): "Stop
  the backup (Recommended)". §11's "When Thunderbird stops using an archive's folder" extends that
  stop (*Rev 2*, C2).
- 2026-11-23: "ok - lets get that fixed" (after the session reported that parts 2 to 4 were not
  built).
- 2026-11-23, the owner's answer to this design's **QA**: "Yes, name it (Recommended)". The IMAP
  download question names the account, on both paths (§8, MA-1502).

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
gets its own job file and scheduled task, so each can be paused, repaired or deleted alone. The
archive format and existing archives do not change. The engine changes in one place: a run with
auto-discovery off stops when Thunderbird has moved its account to another folder (§11, *Rev 2*,
C2), and a removed account's stop gets its own words (C13).

## 3. The question

**Where.** In `pickAutoInputs`, after the answer "All of them", when all of these hold:
- the flow is the plaintext one (`keyed == nil`): never in **Make a new encrypted archive…** (the
  encrypted flow reaches the list, so the gate must be explicit);
- the Outlook-app route is not taken (that route never shows the list, `wizard.go:808-820`;
  unchanged);
- two or more rows would get an archive by §5's rule. One account plus an empty feeds folder stays
  one archive, as today, and the question is not shown.

**Words** (fixed text, none of `& < > \`, as MA-1286 requires of a list's prompt; Windows' list
shows the prompt in a scrolling box since 1.7.4). *Rev 2, C8 and C20:* the second paragraph no
longer says "moved" (a moved archive's schedule still points at its old place until Repair) and
the first says when a new mailbox joins:

> Keep these mailboxes together in one archive, or give each one its own archive?
>
> One archive: one place to search all your mail. A mailbox you add to Thunderbird later joins it
> at its next backup.
>
> One archive for each mailbox: each can be searched, paused or deleted on its own, and a problem
> with one does not stop the others. A mailbox you add to Thunderbird later is not backed up until
> you run this again, choose One archive for each mailbox, and choose the same folder.

Rows: **"One archive for all of them"** and **"One archive for each mailbox"**.

**Default.** "One archive for all of them" is preselected on Windows and macOS, so a click-through
gives today's result. Linux preselects nothing (`third_party/zenity/list.go:61-69`); an answer that
names no row shows `pickAgain` and the list again, up to `pickTries`, as `askSchedule` does
(`wizard.go:2136-2167`, the shared `rowChoice`). Cancel ends the flow, as Cancel on the list does
today.

**"One archive for all of them"** continues exactly as today: the same paths and `auto` on.

**A set's parent chosen for one archive** (*Rev 2*, C8). On the one-archive path and the
single-row path, when the output folder chosen directly holds two or more archive folders (read by
§7's child read), the wizard asks before anything is written (*Rev 2.1, F-10a*: the words no
longer say how those archives were made, which the wizard cannot tell): "D:\Mail archives already
holds 3 archives. An archive made here would hold them inside it, and making one archive for each
mailbox in this folder again would then be refused. Choose another folder?" Buttons
**"Choose another folder"** (the folder question again) and **"Use this folder anyway"**.

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
- a mailbox with no usable name: "Folder with no name" or "File with no name", as the list says.

**Made safe** (*Rev 2*, C16 and C17: the order is fixed and checked). The base name is cut to
`60 − len(suffix)` characters on a character boundary, the suffix is added (*Rev 2.1, F-09*: so a
name is at most 60 characters before any " (2)" number, and at most 65 with it), and the result is
cleaned by the copy export's `suggestFolderName` (`internal/app/copyexport_folder.go:29-47`:
control characters dropped, `/ \ : * ? " < > |` turned to spaces, whitespace tidied, leading and
trailing dots dropped). The result must then pass `folderNameWhy` (`:50-62`, which includes
`util.SanitizeSegment`'s check) and must not be a device name Windows reserves, with or without an
extension, compared without regard to case: `CON`, `PRN`, `AUX`, `NUL`, `COM1` to `COM9`, `LPT1`
to `LPT9`, `COM¹` to `COM³`, `LPT¹` to `LPT³`, `CONIN$`, `CONOUT$`. A name that fails, is empty,
or is "untitled" or "New folder" becomes "Mailbox". No name ever holds `SanitizeSegment`'s
"untitled" or its `~` hash ending. Text from `prefs.js` is never used as a path in any other way:
never joined with a separator, never resolved.

**Told apart.** Two names that are the same after NFKC and case folding (`state.TBTextKey`), or,
where answers may be transcoded, after `asciiForm` too, get " (2)", " (3)" and so on, in the
plan's order. Case folding matters because Windows and macOS folders ignore case. A name already
used in the parent by anything that is not this mailbox's archive (§7) takes the next free number.
*Rev 2, C18:* a name found taken only when its folder is made (something appeared in between) also
takes the next free number; the summary says the folder's final name, never a refusal.

**What the owner's ruling costs, stated** (*Rev 2*, C19: corrected). The address then appears in
the archive's path, so also in: its job file (`out`); its log paths; the macOS LaunchAgent's
error-log path; the Windows wrapper's error-log redirect (`internal/schedule/wrapper.go:73-74`); the
run log's line "Scheduled job …: exporting to <out>" (`wizard.go:338`); and alert email subjects
and bodies, which name the archive by its path (`internal/alert/alert.go:184-209`; `header` strips
CR and LF). *Rev 2.1, F-10e:* also the settings files that hold archive paths: the Desktop's list
(`known-archives.json`), `last-archive.txt`, `desktop-settings.json`, and the new made-list (§10),
which holds the name twice (in the path and after its tab). Scheduled task and LaunchAgent names do
not hold it: they are hashes of the path (`schedule.DefaultNameFor`). S85 ("the words are written
nowhere") and MA-1288 ("no log file holds the name") are narrowed, by the owner's 2026-11-19
ruling (answer 3), for this case only: the account's name may appear where the archive's own
folder path appears. "One archive for all of them" and single-mailbox archives are unchanged: their
jobs still hold no account name (the `ma1462_picker_test.go` assertion stays).

**Alert subjects with letters outside ASCII** (*Rev 2*, C31). An address or name with such letters
(for example "Søren") now reaches an email subject. The subject is encoded as RFC 2047 words
(`mime.QEncoding`) when it holds any byte outside ASCII. This also fixes today's archives whose
path holds such letters.

## 5. Which mailboxes get an archive

After "One archive for each mailbox", each row the list showed gets an archive, in the list's
order, except:
- an **empty** row (counted in full, size 0) that is a feeds account, a folder no account uses
  now, or an older copy: nothing to keep, and nothing will arrive. The plan says "Not made:
  <name>, which is empty."

Kept on purpose:
- **Local Folders always gets an archive, even when empty**: POP mail, filters and mail a person
  moves off the server land there.
- An account whose folder says "nothing downloaded yet" gets an archive: its mail arrives later.
- A row whose size was not counted (the 3-second budget ran out, MA-1466) gets an archive.

**Saved once, never scheduled:** a folder no account uses now and an older copy. No account writes
to them, and a schedule on one would only fail, then pause itself, the day the leftover is cleaned
up. The plan and the summary say "saved once". *Rev 2, R2:* a saved-once row whose folder an
archive in the parent already backs up (§7's store-id match) is not copied again; that archive is
left as it is (§7).

Each archive is an ordinary one: `Inputs` holds that one path, `Auto` is off.

## 6. The parent folder

Asked once, in place of "Select the output folder", titled **"Select the folder to keep the
archives in (a folder is made inside it for each mailbox)"**.

*Rev 2, C9, C14 and C15: the check is the decrypt copy's, and each refusal has wizard words.* The
parent is judged as the decrypt copy judges "a new folder inside a chosen parent"
(`internal/app/decryptplan.go:262-302`, `checkDecryptPaths`). Refused, with the folder question
asked again and nothing created:
- the parent is an archive or is inside one, plaintext or encrypted, found by walking up from it
  (`app.ArchivesAbove`, `internal/app/copyexport_archive.go:206-230`) or among the archives this
  computer knows: the Desktop's list (`archindex.Read`), the wizard's made-list (§10), its last
  archive and the current one (`knownArchives`, `decryptplan.go:307-327`, which gains the
  made-list);
- the parent is one of MailArchive's own folders, or inside one (`mailarchiveOwnDirs`,
  `decryptplan.go:332`: its settings folder and the others).

Each refusal the reused check can return (its codes R-e, R-q, R-w, R-f, R-f2, R-set, the
own-folders refusal at `decryptplan.go:286`, and R-enc) gets the wizard's own words. *Rev 2.1,
citation 1:* every one of them is worded today for the decrypt copy ("the unencrypted copy"), and
`copyWording` (`decryptplan.go:368`) rewords only R-f and R-f2, so the wizard needs words of its own
for each code, not only a call to `copyWording`. The commonest: "<folder> is an archive, or is
inside one, so the new archives cannot go there. Choose a folder outside every archive."

*Rev 2.1, F-04: how each folder is made safely without walking the known archives again.* The
known archives are walked once per set, giving the checked result (`CopyOutside`). Each archive
folder is then made through a handle on the parent: the handle is opened first, the set's checked
result is re-proved against the folder that handle holds (its `Recheck`, after the handle is open,
never before), and the folder is made through that handle at 0700, as the copy export's
`CreateFolder` does (`copyexport_folder.go:71-112`) but taking the set's checked result in place of
a new walk. So a parent swapped for a link after the walk is caught by the re-proof against the
handle, and a new folder can never land inside an archive. The parent may hold other things (a person may pick
Documents). MailArchive writes nothing in the parent except the archive folders.

## 7. A folder that is already there

Running the same choice into the same parent again is how a person adds mailboxes found since, and
brings the others up to date.

*Rev 2, C3, C4, C5, C12 and R2: an archive is found by what it holds, not by its name.* Before
naming anything, the wizard reads each folder directly inside the parent (at most 500 are looked
at; more are left out and the plan says so). A child is read only when it is a real directory: not
a link, junction or other reparse point, opened through the parent's `os.Root`, owned by the person
running the wizard and not writable by others. For each such child, a new exported
`archindex.Stores(folder)` gives one of three answers from the bounded, safe walk `inspect` already
does (store ids read lexically inside an `os.Root`, never `state.Load` by path; SD-1, SD-2):
**its store ids**, **not an archive**, or **cannot read now** (a read that failed and may pass
later).

Then, for each mailbox of the plan, in this order (*Rev 2.1, F-05*: the saved-once rule comes
first):
- **A saved-once row already kept** (*R2*; *Rev 2.1, F-05*). For a folder no account uses now, or
  an older copy, whose store id an archive in the parent already holds: that archive is left as it
  is, and nothing new is made. When that archive's store ids are exactly this one id, the plan says
  "<name>: kept as it is in <folder>; Thunderbird no longer uses this folder. Click Pause on it to
  stop its backups." When that archive holds other mailboxes too (a combined archive), the plan
  says only "<name>: kept as it is in <folder>", with no Pause advice.
- **Its archive.** A child whose store ids are exactly this one mailbox's id (`path:` plus its
  folder, as `app` writes it), and whose job file, when there is one, has `Auto` off and this one
  input, is this mailbox's archive, whatever its name (an account renamed since keeps its first
  folder). It is continued with the answers given now; the plan says "brought up to date". A
  combined archive, or any archive holding other stores, never matches. When two children match,
  the first by name is continued and the plan says "<other> also holds this mailbox's archive:
  left as it is". *Rev 2.1, F-11:* a child whose store ids match but whose job file exists and
  cannot be read is continued too; a Yes never installs over that job, and the end note says
  "<name> kept its own schedule (its settings could not be read)".
- **A folder the wizard made but never finished** (*C5*). A child with no manifest that the
  made-list (§10) records for this mailbox's name in this parent is continued (a crash or failure
  before the first save).
- **Paused.** If its archive is paused (sealed read-only, `seal.IsSealed`; a paused archive takes
  no write, no lock and no record, `internal/app/run.go:1483-1488`), it is left as it is, gets no
  schedule, and the plan says "paused, so <name> is not being backed up now; click Resume (make
  writable) on its row to back it up again".
- **Cannot read now** (*Rev 2.1, F-07*: any child, not only the one named for this mailbox). The
  plan lists each child that could not be read now: "<folder>: could not be read just now". While
  any such child is there, a mailbox with no matching archive gets no new archive this run (that
  child may be its archive under another name); the plan says "<name>: not made this time, since a
  folder here could not be read; run this again later".
- **Otherwise** a new folder is made, named by §4. Anything already at that name (another
  mailbox's archive, an encrypted archive, a link, a folder owned by someone else, a folder or file
  that is not an archive) is never written into: the new folder takes the next free number.
- **A folder whose delete did not finish** (`app.RefuseUnfinishedDelete`): that archive is not
  made; the plan says why and how to finish the delete.

The plan also lists, for a re-run, each archive in the parent whose mailbox is no longer found:
"In this folder, not found now: <name>" (*C13*).

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
> Asked about a full offline download: qzsam.walker@gmail.com, sam.walker@contoso.example

The text carries account names and paths. It goes through `messageDialog`/`zenity.Question` as the
summary does; on Linux those pass `--no-markup` (`third_party/zenity/msg_unix.go:8`), so a name is
shown as written. Singular and zero forms are written out ("1 archive", "No new archive: 2 brought
up to date").

**The IMAP download question** (*Rev 2*, QA answered; C7). It names the account, on both paths (one
archive and one per mailbox):

> Account 1 of 3: qzsam.walker@gmail.com
>
> <today's explanation, unchanged>
>
> Prepare a full offline download of qzsam.walker@gmail.com first?

Buttons unchanged ("Yes, prepare" and "No, export what's cached"). The name is the one the list
showed. On a set's re-run it is asked only for accounts that get a new archive; an archive brought
up to date is not asked again (its default answer would re-export it in full). The plan's last line
says which accounts will be asked. The count ("1 of 3") counts the accounts asked this run.

**Asked once for all the archives**, as today and with today's defaults: "Export mode:", the date
window, "Is your mail app currently open?" (only when an Outlook file is among them), the `.eml`
question and the folder-links question. An archive whose account was just prepared runs in Full
mode, as today; the others take the mode chosen. The mode question is skipped only when every
archive's account was prepared.

## 9. Making the archives

One after another, in the plan's order, each through today's `runExport` steps (the unfinished-
delete and keyless refusals, the run log, `app.Run`) with `Inputs` its one path and `Auto` off.

- *Rev 2, C5:* each new folder is made just before its own export, and recorded in the made-list
  (§10) right after it is made, before the export starts.
- **Progress:** one progress window per archive, titled "Exporting 2 of 4…", with today's counts
  line. No account name in the progress text.
- **Cancel** on a progress window stops that archive, keeps what it copied (as today), and starts
  no more. The summary lists which finished, which stopped and which were not started.
- **An error** in one archive does not stop the others. Its summary line is the error's first line
  and its log's path. This differs from a single archive, whose error ends the wizard
  (`wizard.go:1900-1906`). *Rev 2, C29:* an error about the parent itself (it cannot be written, is
  full, or is gone) stops the set, as Cancel does, since every later archive would meet it too.
- `rememberArchive` is called once, after the schedule step (*C22*): `last-archive.txt` names the
  set's first scheduled archive, or, with no schedule, its first finished archive that is not saved
  once.

**The summary** is one window, buttons **"Open the folder"** (opens the parent) and **"Close"**:

> 5 archives are ready in D:\Mail archives
>
> qzsam.walker@gmail.com: Backup complete. 48,210 messages.
> sam.walker@contoso.example: Backup complete. 9,870 messages. Not fully downloaded yet: 12 —
> open these in your mail app so it downloads them, then run this again to add them.
> Local Folders: Backup complete. 1,204 messages.
> imap.gmail.com (not in Thunderbird now): saved once. 3,311 messages.
> sam@old.example (brought up to date): Backup complete. 2,045 messages.
>
> Each archive is its own folder: you can hand over or delete one without touching the others.
> Open index.html in any of them to browse. MailArchive Desktop lists them all under Your
> archives; a row whose last backup did not finish says so.

(*Rev 2.1, F-10c*: the example now counts the archive brought up to date, as C21's rule says, and
carries R1's sentence, as §10 says.)

*Rev 2, C21:* the lead counts finished archives only, with singular and zero forms ("1 archive is
ready", "No archive was finished"). Each archive's line keeps the not-done, empty-at-source,
kept-as-received and index-error counts that `exportSummaryFor` shows for one archive today, each
with its remedy (X6), and its Thunderbird account lines (`state.TBLines`) or their one remedy
sentence. Only lines with a count above zero appear. The lead per archive is that archive's
`tbcomplete.RunLead`. One line appears only when it applies: "<name>: Did not finish: <reason>. The
mail copied so far is kept; run this again with the same folder to finish it." *Rev 2, C1:* the
line about an existing combined archive is no longer here; it moved to the schedule's end note
(§11).

## 10. MailArchive Desktop and the stand-alone wizard

**Make a new archive…** (*Rev 2*, C11). `AddArchive` is replaced by
`AddArchives() ([]Made, error)`. Each `Made` is a folder the flow made or continued, in the plan's
order, with its outcome (finished, stopped part way, failed with a reason, not started) and its
schedule outcome. One archive gives one `Made`. A person who backed out before any folder was made
gets none ("nothing added"). The Desktop handler registers every folder that holds an archive, in
one `archindex.UpdateFile` with `Reconcile` (which already takes a list,
`internal/archindex/index.go:342`), makes none of them current (as today, MA-696), and names each
failure. Its notes:
- all finished: "4 archives added — they are now in Your archives."
- one: "Archive added — it is now in your list." (today's words)
- some did not finish: "3 archives added — they are now in Your archives. sam@old.example did not
  finish: <reason>. To finish it, click Make a new archive… again, choose One archive for each
  mailbox and choose the same folder, D:\Mail archives." (*C8*: the parent is named.)
- stopped part way (today's `ErrStoppedEarly` case): the same, naming the parent.

**The stand-alone wizard's archives** (*Rev 2*, C10, C25 and C32). Today the Desktop learns of a
wizard-made archive only through `last-archive.txt`, which holds one path
(`internal/desktop/archives.go:130-143`, `wizard.go:1171-1176`). A set would show only one. The
wizard therefore also records each archive folder it makes in a **made-list**, `wizard-made.txt` in
the settings folder:
- one absolute path per line, with the mailbox's folder name after a tab (for §7's unfinished-folder
  rule); no duplicate lines; at most 200 lines and 64 KB, the oldest dropped;
- written 0600 under `encmark.RefuseSettingsWrite`, by writing a new file and renaming it over the
  old one, under a lock file beside it, `wizard-made.lock`;
- read with the secure regular-file reader (`util.OpenRegularFileIn` inside an `os.Root` on the
  settings folder), bounded to 64 KB; a line with a control character other than its one tab, or
  whose path is not absolute, is skipped (*Rev 2.1, F-06*).

The Desktop folds the made-list's paths into Your archives (with `Reconcile`, under the list's
lock), then removes from the made-list, under its lock, each line whose folder now holds an archive
or is gone. *Rev 2.1, F-02:* a line whose folder is there with no manifest yet (a copy that failed
before its first save) is kept, so §7's unfinished-folder rule still finds it on the re-run the
Desktop's note asks for; the 200-line cap bounds such lines. So a row the person removes (Unlist)
stays removed. Unlist and Delete also strike their folder from the made-list, and uninstall removes
`wizard-made.txt` and `wizard-made.lock` (both join `fixedConfigNames`,
`internal/uninstall/config.go:30`).
`last-archive.txt` keeps one path (§9, C22), so an older Desktop keeps working: it lists one archive
of the set, and the others' schedules still run.

**How the set's archives are doing** (*Rev 2*, R1). The Desktop's health view covers only the
current archive (`internal/desktop/maintenance.go:253-259`), and a set makes none current. So each
row in Your archives now also says, from the last-run record the row already reads for its
Thunderbird line (`internal/desktop/archives.go:272-281`), one line when its last backup did not
end ok: "Last backup did not finish (Nov 24): switch to it to see why." *Rev 2.1, F-10b:* a record
whose status is "running" (`state/lastrun.go:64`, a backup in progress) gives no such line. The end note's and the
summary's sentences say: "MailArchive Desktop lists them all under Your archives; a row whose last
backup did not finish says so."

**The launch health view** of the stand-alone wizard still covers one archive (`wizard.go:682-686`,
`:1223`). A health view of every archive is later work (§13).

## 11. Schedules, the Thunderbird sync, and existing archives

**The schedule question** is asked once for the set, when at least one archive finished and may be
scheduled (not saved once, not paused, not stopped part way, not failed):

> Keep these archives current automatically?
>
> Each archive gets its own scheduled backup. They start 10 minutes apart, from the time you
> choose, so they do not all run at once. A scheduled run repeats exactly this export
> (incremental: only new mail and any content that was still missing). It runs only while you
> are signed in. <today's sentence for this system>

*Rev 2, C27:* today's sentence for each system already says what happens to a missed time
(`scheduleQuestionFor`, `wizard.go:2098-2104`). On Windows and macOS the set's question adds:
"After a night the computer was asleep, they may all start together when it wakes." On Linux a
missed time is skipped (cron), as today's sentence says.

The rows and the rule are today's (MA-1474): only an exact Yes row installs anything.

*Rev 2, C6 and C11: what a Yes installs.* A schedule is installed, through `installOffered`, only
for an archive made in this run, or one brought up to date that has no job file yet. An archive
brought up to date that has a readable job keeps its job file and its scheduler entry exactly as
they are (its Hourly or Run-now-only choice, its Thunderbird sync, its time); the end note says
"kept its own schedule". Each install writes its own job file and scheduler entry, named from its
own path (`schedule.DefaultNameFor`), so nothing clashes. The first new one starts at the chosen
time (02:00 daily or Sunday 03:00); each next new one 10 minutes later. *Rev 2.1, F-10d:* on a
re-run, the archives that keep their own schedule keep their times, so a new one may share a start
time with a kept one; the set's question then says "New archives start 10 minutes apart from the
time you choose" rather than "they do not all run at once". An archive whose job file exists but
cannot be read is never installed over (§7, F-11). An install that fails does not stop the others;
the end note says "Not scheduled: <name> (<why>)". End note:

> Scheduled: daily from 02:00, one archive every 10 minutes, for 3 archives in D:\Mail archives.
> sam@old.example kept its own schedule.
> Each archive keeps its own log, mailarchive.log, in its own folder. These backups save the mail
> Thunderbird has already downloaded to this computer; they do not start Thunderbird first.
> Not scheduled: imap.gmail.com (not in Thunderbird now), saved once.
> MailArchive Desktop lists them all under Your archives; a row whose last backup did not finish
> says so.

**Existing archives that back up the same mail** (*Rev 2*, C1, in the rescue's form). They are
never split, moved, renamed or rewritten. When the schedule question is answered with a Yes row,
the end note may add one line per known archive. That line is never in §9's summary, which comes
before the question; with No or Cancel there is no line. Each known archive's job file is read with
`job.Read`; a job it cannot read gives no line. These are passed over: a folder in this run's plan
(made, brought up to date or left paused) or directly inside this parent, each compared by identity
(`os.SameFile`; a folder that cannot be looked at now gives no line); a paused (sealed) archive; an
encrypted archive (`encmark.MustMinimise`); and an Outlook-app job. Of the rest:
- a job with `Auto` on (with or without listed inputs), or with two or more inputs of which at
  least one is a mailbox of this plan: "Your archive PATH also backs up these mailboxes, all
  together.";
- a job with one input and `Auto` off, where that input is a mailbox of this plan: "PATH also backs
  up NAME."

The line goes on "To stop backing them up twice, click Pause (make read-only) on its row in
MailArchive Desktop; its mail stays readable." only when all of these hold:
- every input the job lists is a mailbox of this plan, compared by identity;
- every mailbox of this plan that may be scheduled (not saved once) has its archive's schedule
  installed and not paused, whether made now or brought up to date keeping its own schedule;
- no archive of the set failed or stopped.

Otherwise the line goes on "Keep it running until each of these mailboxes has its own scheduled
backup." The check reads job files only, never a manifest.

**The Thunderbird sync** is not offered for a set (the owner's answer 4). Today's code would start
Thunderbird once per archive, or leave all but one archive "profile busy": the profile lock is tried
once with no wait (`internal/presync/session.go`, the busy outcome at `:210`). Archives without the
sync never take that lock (`internal/wizard/jobrun.go:379-412`). A shared sync is its own design.

**When Thunderbird stops using an archive's folder** (*Rev 2*, C2, in the rescue's form). An
archive with auto-discovery off reads one folder. Suppose its account is removed in Thunderbird,
the folder is left on disk, and the account is added again. Thunderbird then keeps the account in a
new folder (the old name with "-1": its `GetLocalPath` makes a unique folder when the old one is
there; the repository's saved Thunderbird source, `.claude/tbsrc/nsMsgIncomingServer.cpp:744-782`),
and the old folder stops changing. So a run with `Auto` off and exactly one listed input that is a
folder stops, as a moved account's run has stopped since 1.7.5 (MA-1481, the owner's Q7), when all
of these hold:
- the archive's previous record names the folder's account (`autoplace.RecordedName`);
- this run's read of the profile's settings finds, for certain, that no live account uses the
  folder (*Rev 2.1, F-01*, corrected): no live account's folder setting (`directory`,
  `directory-rel`) names it, and every live account whose server name matches the folder's name
  has a folder setting that names another folder. `thunderbird.ReadStoreFacts` alone is not
  enough: with two or more accounts on one mail server (several Gmail addresses, all on
  imap.gmail.com), it answers `NoAccountAmbiguous`, not `NoAccountNone`, for a folder that none of
  them uses any more (`internal/thunderbird/thunderbird.go:860-874`, `:886-889`). A new function in
  `internal/thunderbird`, `NoLiveOwner(profile, store)`, gives this answer from the same strict
  read, and leaves `ReadStoreFacts` and every caller of it unchanged;
- exactly one live account has that name, with its folder elsewhere (`autoplace.MovedHowAs`,
  decided by the name).

The stop is the typed `sourceUnreachableError` (in `DiscoverInputsFor`,
`internal/app/run.go:2070-2126`), so the failure email, the source-loss banner and the 30-day pause
follow, as for any stopped source. Its words name folders only (MA-1288): "Thunderbird no longer
uses OLD: the account that used it now keeps its mail in NEW, so this backup stopped. The mail
saved so far stays in this archive. To back up NEW, click Make a new archive… and back that account
up again (choose it in the list, or choose One archive for each mailbox and the same folder as
before); then click Pause (make read-only) on this archive. If Thunderbird should use OLD again,
set the account's Local Directory back to OLD (in Thunderbird: Account Settings, then Server
Settings)." The Repair helper still says no (`internal/health/mailboxgone.go:163-166`).

Nothing changes for: a folder the record never named; Local Folders; a run with auto-discovery on;
a job that lists several inputs; settings that cannot be read for certain. The rule applies to
single-mailbox archives made today too.

*Rev 2.1, F-08: the same account moved, with its old folder gone.* A run with `Auto` off and one
listed folder that is gone, whose record names its account, and whose named account now keeps its
mail elsewhere (`autoplace.MovedHowAs`), stops with the same moved words, as an `Auto` run has since
1.7.5 (`run.go:2101-2107` runs that check only when `Auto` is on; it now also runs for `Auto` off
with one listed input).

**When the account was removed for good** (*Rev 2*, C13). A run with `Auto` off and one listed
folder that is gone, whose previous record names its account, and whose profile's settings name no
live account with that name, stops as today (the 30-day rule unchanged), with its own words in
place of "reconnect the source": "Thunderbird no longer has the account that used <folder>. This
archive keeps the mail saved so far. To stop its backups, click Pause (make read-only) on its row."
The words travel with the typed stop as C2's do (the run's error, status, Run now's note, the
failure email).

**The later words for both stops** (*Rev 2.1, F-03*). Both stops are the typed source refusal, so
after 3 days status and the row's banner would say "reconnect the source" (`health.go:553`), and
at 30 days the pause would say "reconnect the source, then … click Repair" (`wizard.go:461`), which
for these archives is a dead end: the folder may be there, and Repair says no
(`mailboxgone.go:163-166`). So the refused record keeps a fixed reason code with no name in it,
`source_reason`: `moved` (C2, F-08) or `removed` (C13); the 1.7.5 moved stop of an `Auto` run gets
`moved` too. Status, the row's banner and the 30-day pause read it and use these words in place of
"reconnect the source":
- `moved`, status and banner: "Thunderbird stopped using this archive's mail folder on DATE: its
  account now keeps its mail in another folder. The mail saved so far stays here. To back that
  account up, click Make a new archive… and choose it again; then click Pause (make read-only) on
  this archive."
- `moved`, the 30-day pause: "MailArchive paused this archive's scheduled backup: Thunderbird has
  not used its mail folder since DATE. Its mail stays here. To back that account up, click Make a
  new archive… and choose it again."
- `removed`, status and banner: "Thunderbird no longer has the account this archive backs up
  (since DATE). The mail saved so far stays here. To stop its backups, click Pause (make
  read-only)."
- `removed`, the 30-day pause: "MailArchive paused this archive's scheduled backup: Thunderbird has
  not had its account since DATE. Its mail stays here."

A record with no `source_reason` (every record written before this release, and every other stop)
keeps today's words. `source_reason` is written in plaintext records only: an encrypted archive's
record keeps its allow-listed fields unchanged (MA-734), so an encrypted archive's stops keep
today's words. The words above are drafts for the friction review.

**The Settings card's sync switch** (*Rev 2*, C24). It refuses to switch the sync on when another
archive already syncs the same Thunderbird profile, naming that archive: "Thunderbird sync is
already on for D:\Mail archives\qzsam.walker@gmail.com, which backs up the same Thunderbird profile.
Only one archive can sync a profile for now: switch it off there first." How it decides:
- the other archives are the Desktop's known archives whose schedule is installed and not paused;
- each one's job file is read with `job.Read`; a job file it cannot read stops the switch, naming
  the file ("Could not read the settings of <archive>: <file>. Try again, or Repair that archive.");
- a job's profile is `ThunderbirdSync.Profile` when set, else the profile of its inputs
  (`thunderbird.FindProfileDir`); a job with `Auto` on counts as covering every profile auto-detect
  finds;
- an input on a network path is not looked at (MA-869's rule), and every look has a bounded wait.

The guard is on the Settings card only. Archives already set up both ways are left as they are.

**The command line (R33).** Every archive in a set is an ordinary one that `mailarchive export
-input PATH -out DIR` makes; `mailarchive export -auto -list` lists the mailboxes found. No new verb
is added. A single command that makes a whole set is later work (§13).

## 12. Question for the owner

**QA**, answered 2026-11-23: "Yes, name it (Recommended)". Folded in §8 and MA-1502; it amends the
pinned wiring checks MA-947 and MA-1267 (the 1.7.3 friction review's F12). No question is open.

## 13. Not in this change (later, each with its own design or review)

- A Thunderbird sync shared by a set (start Thunderbird once per profile; a bounded wait on the
  profile lock).
- *Rev 2, C23:* one Thunderbird quit and one sync for several IMAP accounts prepared in one run
  (today each "Yes, prepare" starts and quits Thunderbird for its own account).
- A "Mailboxes not backed up" card for accounts added after a set was made, which would also cover
  §14's new-profile case.
- Searching across archives; a search narrowed to one account inside a combined archive.
- A tool that splits an existing combined archive.
- Encrypted sets (one password for several archives is a new vault design).
- A command-line flag that makes a set.
- The stand-alone wizard's health view of every archive in a set.
- One-at-a-time scheduled runs (a machine-wide lock), for backups a sleeping computer starts
  together at wake.
- The Outlook-app route's question that comes before the list (the 1.7.3 friction review's F13).

## 14. Risks kept

- A mailbox added later is not backed up by a set until the person runs the same choice into the
  same folder again. The question says so; nothing reminds them later (the card in §13 would).
- *Rev 2, C2:* a new Thunderbird profile, with the old one left on disk and still listed: the old
  profile's settings still name every account, so nothing tells MailArchive that the folder is no
  longer used. The archive keeps backing up the old folder with no warning. The same holds for an
  account the record never named: a POP account, an account with no name line in Thunderbird's
  settings, or any encrypted archive's account. *Rev 2.1, F-01:* also an account on a shared mail
  server whose settings carry no folder setting at all: the folder's owner then cannot be told for
  certain, and nothing stops. Thunderbird appears to store the folder it picks for every account
  it makes (the saved source's `GetLocalPath`, not checked on every version), so this should need
  a hand-edited `prefs.js`; the lab (MA-1503) reads a real profile's settings to check.
- *Rev 2, C28:* notices multiply. One shared cause, such as an unplugged backup drive, sends one
  failure email per archive and, when it is fixed, one recovery email per archive: 2 × N emails for
  a set of N.
- No one search across all mail on the per-mailbox path.
- A set has no Thunderbird sync, so an IMAP account Thunderbird rarely opens falls behind.
- The address in folder names, logs, job files and alert subjects (§4), as the owner accepted.
- An older Desktop lists only one archive of a set made by the stand-alone wizard.
- Path length: the archive folder adds up to 60 characters above every message path. Not measured.
- *Rev 2, C26:* the schedule question comes after every archive's export, so a person who walks
  away during a long first backup finds the question waiting, and nothing is scheduled until they
  answer it.
- *Rev 2, C27:* after a night asleep, a set's missed backups may all start together at wake
  (Windows and macOS); the question says so.

## 15. Rows

New scenario **S90**: "Auto-detect finds several mailboxes, the person picks All of them, then One
archive for each mailbox". New rows (MA-1482 to MA-1489 are left free for 1.7.5's follow-ups):

| Row | Tier | What it proves |
|---|---|---|
| MA-1490 | U | The question appears only after All of them, only with `keyed == nil`, only when two or more rows get an archive; One archive is preselected on Windows and macOS; an empty answer asks again; One archive gives today's paths and `auto` on. |
| MA-1491 | U | Which rows get an archive (§5), from `pickRow`s: empty feeds and leftovers are not made, Local Folders always is, leftovers are saved once. |
| MA-1492 | U | Folder names (§4): the account's name; suffixes kept after the cut; " (2)" after case folding and NFKC; hostile names (`..`, `/`, `\`, `CON`, `CONIN$`, `COM¹`, trailing dot, control and right-to-left characters, 300 characters) come out as one safe segment of at most 60 characters before any number and 65 with it, passing `folderNameWhy`; never "untitled" nor a `~` hash. Adversarial. |
| MA-1493 | U | The parent folder: an archive or inside one (plaintext and encrypted, known or not), MailArchive's own folders: refused with the wizard's words and asked again, nothing created; positive twin first. A parent swapped for a link between the check and the make is refused. |
| MA-1494 | U | A folder already there (§7): the same mailbox's archive is continued whatever its name (renamed account, numbered twin); a combined archive at the name is not; a link, a junction, a folder owned by someone else or writable by others is never written into; an unfinished folder the made-list names is continued, also after a Desktop page load; a paused one is left alone with its words; any child that cannot be read now is listed, and a mailbox with no match then gets no new archive; a matching archive whose job cannot be read is continued and never installed over; a saved-once row (a folder no account uses, an older copy) already kept is left as it is, checked before "its archive", with Pause advice only when that archive holds exactly that one store and none for a combined one; store ids are read the safe way (a planted UNC id opens nothing). |
| MA-1495 | U | The plan lists every archive, every "brought up to date", "paused", "not made", "not found now" and "asked about a download" line, with singular and zero forms; Cancel writes nothing (side effect absent). |
| MA-1496 | U | Making the set: one input each, `auto` off, one at a time; each folder made just before its export and in the made-list first; Cancel stops the current one and starts no more; an error in one goes on to the next; an error about the parent stops the set; a name found taken only at make time takes the next free number and the summary says the final name (C18); `last-archive.txt` names the first scheduled archive, else the first finished one not saved once (C22); the summary keeps each archive's counts and account lines, with singular and zero forms. |
| MA-1497 | U | The schedule once for the set: only an exact Yes row installs; one job and entry per new archive, 10 minutes apart; an archive brought up to date with a readable job keeps its job and entry; none for saved-once, paused, stopped or failed archives; an install that fails does not stop the others; no `thunderbird_sync` in any job written; the end note carries §11's lines. |
| MA-1498 | U | No sync question for a set; the Settings guard refuses a second archive's sync for one profile and names the other (by `ThunderbirdSync.Profile`, by inputs, by `Auto`); refuses on an unreadable job naming it; skips network inputs; its positive twin switches on where no other archive syncs. |
| MA-1499 | U | `AddArchives` returns every folder made with its outcome; the Desktop registers every folder holding an archive, none current, and names each failure and the parent. |
| MA-1500 | U | The made-list: 0600, settings folder only, written by rename under `wizard-made.lock`, no duplicates, 200 lines and 64 KB kept; read bounded through the secure reader, a line with a control character other than its tab skipped; the Desktop folds it and strikes each line whose folder holds an archive or is gone, keeping a line whose folder has no manifest yet; a removed row stays removed; Unlist, Delete and uninstall remove its entries, the file and its lock file. |
| MA-1501 | U | The existing-archive lines (§11), in the Yes end note only. Positive twin first: an `Auto` archive elsewhere whose inputs are all plan mailboxes, with every member scheduled, gives the "all together" line and the Pause advice. Negative twins: a re-run into the same parent gives no line for any member; No or Cancel gives no line; a member that failed, stopped or was not installed gives no Pause advice; an `Auto`-off job with two inputs, one outside the plan, gives no Pause advice; an `Auto` job with no inputs gives the "all together" line; a one-input archive of a plan mailbox elsewhere gives "also backs up NAME"; a paused, encrypted or Outlook-app archive, an unreadable job, or a folder that cannot be looked at gives no line. Mutations: the plan exclusion dropped; the line moved back into the summary; the Pause condition dropped. |
| MA-1502 | U | The download question names the account and its number, on both paths; on a re-run only accounts getting a new archive are asked. Amends MA-947 and MA-1267. |
| MA-1503 | L | Lab, pending: Windows test PC as an ordinary user (the question and Enter; the plan at 150 %; several Task Scheduler entries 10 minutes apart, each started directly with `schtasks /Run`; address-named folders through the batch wrapper, forced with a long program path; a scheduled run of each; clean-up written down), macOS (counting the Background Items notices) and Linux walks in MA-1467's style; and, with the owner, C2's trigger on a real Thunderbird with **two accounts on one mail server** (remove one IMAP account keeping its mail, add it again, look for a new folder ending "-1" or "-2", read the profile's settings for each account's folder setting, click Run now on its one-mailbox archive). A folder name with `!` is run in the test PC's default setting only; changing its Command Processor setting needs the owner's word (*Rev 2.1*, citation 4). |
| MA-1504 | U | C2's stop: an archive with one listed folder and `Auto` off, whose record names its account, refuses through `assure.Refused` with the words when that account's settings now point at another folder and the old folder is still there; the refusal is typed, so the source-loss streak moves and the record keeps `source_reason: moved`; the next run refuses again; the Repair helper says no. Cases: one account on the server; **two accounts on one server, one re-added at "-2"** (F-01: `NoLiveOwner` says no live owner where `ReadStoreFacts` says ambiguous); the folder gone with its named account elsewhere, `Auto` off (F-08). Positive twins first: the account still owns the folder; a folder the record never named; Local Folders; `Auto` on (the new folder is found); two listed inputs; settings that cannot be read for certain; no live account with the name; two accounts on one server where one still names the folder. Mutations: each of the three conditions dropped; `NoLiveOwner` replaced by `ReadStoreFacts`'s `NoAccountNone`. Amends MA-1481 (points here; its moved stop of an `Auto` run also records `source_reason: moved`; nothing weakened). |
| MA-1505 | U | R1: a row in Your archives whose last backup did not end ok says "Last backup did not finish (DATE): switch to it to see why."; a row whose last backup ended ok, or whose record says a backup is running, says nothing new. |
| MA-1506 | U | C13: a removed account's stop (folder gone, record names the account, no live account with that name) gives the removed-account words in the run's error, status, Run now's note and the failure email, and records `source_reason: removed`; after 3 days status and the banner, and at 30 days the pause, use the reason's words (F-03), for `moved` too. Positive twins first: a gone folder whose record names no account keeps today's "reconnect the source" words; an encrypted archive's record gains no field. |
| MA-1507 | U | C8: on the one-archive and single-row paths, an output folder that directly holds two or more archive folders asks first; "Choose another folder" writes nothing; its positive twin (an empty folder) asks nothing. |
| MA-1508 | U | C31: an alert subject holding letters outside ASCII is sent as RFC 2047 encoded words and reads back as written; an ASCII subject is unchanged. |

Narrowed by the owner's ruling: S85 and MA-1288 ("written nowhere" holds except where an archive's
own folder path is named by its mailbox, the owner's 2026-11-19 answer 3). Amended (nothing
weakened): MA-696 (Make a new archive… may add several); MA-1474 (the set's schedule question uses
the same rows and rule); MA-1481 (points at MA-1504; its stop records `source_reason: moved`);
MA-693 and the 30-day pause's words (*Rev 2.1, F-03*: a record with a `source_reason` gets that
reason's words; every other record keeps today's); MA-947 and MA-1267 (QA).

## 16. Reviews owed, and the build order

- This design: the fold check of revision 2 (the procedure's step 4), before any code. *Done
  2026-11-24: CLEARED AFTER THESE FIXES, all applied in revision 2.1.*
- After the build: an adversarial pass (account names into paths; the parent-folder check; the
  child read of §7; the made-list; the Settings guard; C2's stop), a friction review of every new
  window's words, and the Windows lab (MA-1503) on the office test PC, as an ordinary user.
- UX contract: X14 (the plan), X6 (the summary's counts), MA-1286's scan of the new prompt.

**Build order** (*Rev 2*, C32), each step test-first with its rows:
1. The engine and words that stand alone: `thunderbird.NoLiveOwner`, C2's stop with F-08's case,
   C13's words, the record's `source_reason` and the later words it chooses (MA-1504, MA-1506); the
   alert subject encoding (MA-1508).
2. The made-list's writer and reader, together with Unlist, Delete and uninstall learning about it
   (MA-1500); `archindex.Stores`.
3. The wizard: the question, names, rows, parent check, §7, the plan, the download question, the
   set's export, summary and schedule (MA-1490 to MA-1497, MA-1501, MA-1502, MA-1507).
4. The Desktop: `AddArchives`, the handler, the rows' last-backup line, the Settings guard
   (MA-1498, MA-1499, MA-1505).

## 17. Revision 2: each condition and where it is carried

| Condition | Where |
|---|---|
| C1 (A-01, B-01) | §9 (line removed from the summary), §11 "Existing archives that back up the same mail", MA-1501 |
| C2 (C-01) | §2, §11 "When Thunderbird stops using an archive's folder", §14, MA-1504, MA-1503, MA-1481 amended |
| C3 (A-02, B-02, C-02) | §7 (found by store ids, whatever its name), MA-1494 |
| C4 (A-03, B-04) | §7 ("exactly this one mailbox's id", the job check), MA-1494 |
| C5 (B-03, C-03) | §7 (unfinished folder; "cannot read now"), §9 (folder made just before its export), §10 (made-list keeps the name), MA-1494, MA-1496 |
| C6 (A-04, B-10, C-05) | §11 "What a Yes installs", MA-1497 |
| C7 (A-05) | §8 (asked only for new archives; the plan's line), MA-1502 |
| C8 (A-06, C-13) | §3 (words; the set's-parent question), §10 (notes name the parent), MA-1507 |
| C9 (A-08, B-07, C-14 part) | §6 (`ArchivesAbove`, `knownArchives` with the made-list), MA-1493 |
| C10 (B-09, A-10, C-04, C-12) | §10 (fold once and strike; Unlist, Delete, uninstall), MA-1500 |
| C11 (B-11, A-20) | §10 (`AddArchives` outcomes), §11 (install goes on past a failure), MA-1497, MA-1499 |
| C12 (B-05) | §7 (real directory, owner, not writable by others), MA-1494 |
| C13 (C-06) | §11 "When the account was removed for good", §7 ("not found now"), MA-1506 |
| C14 (B-08, A-09, C-14) | §6 (wizard words per refusal; one walk; `Recheck`) |
| C15 (A-12, B-06) | §6 (`checkDecryptPaths`, `mailarchiveOwnDirs`) |
| C16 (A-13, B-14) | §4 "Made safe", MA-1492 |
| C17 (B-15) | §4 "Made safe" (the extended device names), MA-1492. The wrapper's `setlocal DisableDelayedExpansion` is not added in this change: delayed expansion is off in a batch file unless the computer's Command Processor setting turns it on for everyone, which is rare. Known limit, stated here; the lab (MA-1503) runs a name with `!` |
| C18 (A-19, B-16) | §4 "Told apart" |
| C19 (A-11, B-21, C-19) | §4 "What the owner's ruling costs", S85 and MA-1288 amendments |
| C20 (A-14, C-10) | §3 words |
| C21 (A-15) | §8 (forms), §9 summary, MA-1495, MA-1496 |
| C22 (A-16) | §9 (`rememberArchive` once), §10 |
| C23 (A-18) | §13 |
| C24 (B-12, B-13, C-17) | §11 "The Settings card's sync switch", MA-1498 |
| C25 (B-19, C-11) | §10 (made-list writer and reader), MA-1500 |
| C26 (C-07) | §14 |
| C27 (C-08) | §11 (the set's question), §14 |
| C28 (C-09) | §14 |
| C29 (C-15) | §9 |
| C30 (C-18, C-20) | MA-1503 |
| C31 (B-17) | §4 "Alert subjects", MA-1508 |
| C32 (B-20) | §16 build order |
| R1 (rescue §3.1) | §10 "How the set's archives are doing", §11 end note, MA-1505 |
| R2 (rescue §3.2) | §5, §7 ("a saved-once row already kept"), MA-1494 |
| QA (owner) | §0, §8, §12, MA-1502 |
| A-21, C-21 (INFO) | A-21: MA-1286 is cited by name; the "per the checker" hedges were removed where the code was read. C-21: the release notes say that going back to 1.7.5 keeps every archive working (each is ordinary) and that its Desktop lists only one of a set made by the stand-alone wizard |

**Revision 2.1: the fold check's fixes.**

| Fix | Where |
|---|---|
| F-01 (HIGH): C2 never fired with two accounts on one mail server | §11 C2's second condition (`NoLiveOwner`), §14, MA-1504 (the two-account case and mutation), MA-1503 (two accounts in the lab), §16 step 1 |
| F-02: the Desktop struck the made-list line the unfinished-folder rule needs | §10 (keep a line whose folder has no manifest), MA-1500, MA-1494 |
| F-03: after 3 and 30 days, both stops still said "reconnect the source" | §11 "The later words for both stops" (`source_reason`), MA-1504, MA-1506, §15 amendments |
| F-04: one walk plus `Recheck` against `CreateFolder`'s own walk | §6 (the set's checked result re-proved after the handle opens) |
| F-05: R2's Pause advice could point at a combined archive; rule order | §7 (saved-once rule first; Pause advice only for an exact match), MA-1494 |
| Citation 1: R-set left out; every code needs wizard words | §6 |
| Citation 4: MA-1503 had no `!` step | MA-1503 (default setting only), §17 C17 |
| F-06: a tab is a control character; the lock file's name | §10, MA-1500 |
| F-07: an unreadable child of another name | §7 "Cannot read now", MA-1494 |
| F-08: a moved account whose old folder is gone, `Auto` off | §11, MA-1504 |
| F-09: 60 characters plus " (2)" | §4, MA-1492 |
| F-10: words (a) to (e) | §3 (a), §10 (b), §9 example (c), §11 (d), §4 and §15 (e) |
| F-11: a job file that exists but cannot be read | §7, §11, MA-1494 |
| F-12: rows | MA-1504 (typed, streak, Repair says no), MA-1506 (positive twin), MA-1496 (C18, C22) |
