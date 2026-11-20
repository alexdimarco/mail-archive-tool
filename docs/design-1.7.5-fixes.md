# 1.7.5: a combined archive keeps going, an empty answer installs nothing, and Delete waits for you

**Status:** DRAFT, revision 1, 2026-11-19, for the pre-code design review
(`../assurance-kit/process/design-review.md`). Branch `combined-archive-gone-mailbox`, off main
`ed491c4` (release 1.7.4). Nothing is built yet.

## 0. The owner's words

- 2026-11-19, after the report that a combined archive stops when one account's folder is gone:
  "yes on all 4 - make it so". The first of the four questions was: "Fix the combined-archive bug
  first, as 1.7.5? I recommend yes. It relaxes one rule ("a missing mailbox stops the backup") for
  "All of them" archives only. An unplugged backup drive still stops the backup."
- 2026-11-19: "also - the delete archive setting - after it runs the "what will be deleted" which
  can take a long time, it times out too early making you do the process over if you miss the
  window. Irritating"
- 2026-11-19: "maybe add an option to forgo the check and delete immediately?" The session
  answered: keep the check, because it is how MailArchive tells the archive's files from anything
  else in the folder, but take the wait away (part 3.2).

The three parts are independent: each can be reviewed, built and reverted alone. They ship
together as 1.7.5 because each is small. The per-mailbox archives the owner also approved
(questions 2 to 4) are the next release's, with their own design.

## 1. A missing mailbox no longer stops a combined archive

### 1.1 Today

- "All of them" in the wizard's mailbox list saves every mailbox found AND auto-discovery
  (`internal/wizard/picker.go:85-86`; `jobFor`, `internal/wizard/wizard.go:2235-2241`, writes
  `Inputs` and `Auto`). This has been so since `bc6e9d0` (2026-09-02).
- Every run calls `DiscoverInputs(opts.Inputs, opts.Auto)` (`internal/app/run.go:631`). Its loop
  over the listed inputs returns `sourceUnreachableError` for the first one that does not exist
  (`run.go:1981-1987`), before auto-discovery runs (`run.go:2006-2010`). The run is refused before
  any mailbox is read (`run.go:632-642`); an unattended run records it as failed
  (`RecordRefusedRun`). Run now does the same (`internal/wizard/jobrun.go:368-370`). The command
  line's `export` calls the same function up front (`cmd/mailarchive/main.go:275`).
- That error moves the source-loss streak (`internal/app/sourceloss.go:60-63`). Status warns
  after 3 days, and the scheduled run stops the schedule after 30 (`internal/state/sourceloss.go:32-35`;
  `AutoPauseIfDue`, `wizard.go:387`).
- So when one account's folder is gone (an account removed in Thunderbird together with its mail,
  or a leftover folder deleted), every account in that archive stops being backed up. Each run
  sends its failure notice and email, and after 30 days the schedule pauses itself.
- MailArchive Desktop's rebuild of a local schedule from the last-run record refuses when any
  listed `-input` is gone (`reconstructLocalJob`, `internal/desktop/localschedule.go:77-109`), so
  Settings cannot save that archive's schedule again either. Its comment (`localschedule.go:74-75`)
  and `internal/desktop/localarchive.go:280-283` say an `-auto` job records no `-input`; since
  `bc6e9d0` that is not so for the wizard's jobs.

### 1.2 The rule

With auto-discovery on (a scheduled run, Run now, the wizard, or the command line's `-auto`
together with `-input`), a listed input that does not exist (`fs.ErrNotExist`, as today) is
**skipped instead of refused** when both of these hold:

- **(a) It lies where auto-discovery looks.** Its path matches one of the patterns
  `autoDiscover` globs (`outlookGlobs`, `thunderbirdGlobs`, `evolutionGlobs`;
  `run.go:2069-2117`). The comparison uses `filepath.Match` with both sides cleaned, and lower-cased
  on Windows and macOS, whose file systems ignore case.
- **(b) At least one mailbox remains** after the skip: a listed input that exists, or one that
  auto-discovery finds this run.

Otherwise the run is refused exactly as today, with the same `sourceUnreachableError` naming the
first missing listed input:

- A listed input outside those places (a `.pst` on a USB drive, a mail folder elsewhere) still
  refuses when it is gone. This is the "unplugged drive" case the owner was told stays caught.
- When nothing remains (a whole Thunderbird profile gone, the user folder unavailable), the run
  refuses as today. The source-loss streak, the 3-day warning and the 30-day pause work as today.
- Without auto-discovery (an archive of one chosen mailbox, or `-input` alone), nothing changes.
- A listed input that exists but cannot be read stays a plain error, as today (`run.go:1987`).

**Why (a):** a gone input in a place auto-discovery looks is one the same run would back up by
itself if it came back. Skipping it loses nothing the job's "(and any added later)" did not
already promise to find.

**Where:** one function, used by every path. `DiscoverInputs` keeps its signature and meaning for
callers that do not report skips (the wizard's list, `prepareThunderbird`). A new
`DiscoverInputsSkipping(inputs, auto) (files, skipped []string, err error)` holds the rule.
`DiscoverInputs` calls it and drops `skipped`. `app.Run` and the command line's `export`
(`main.go:275`) call it and report what they skipped (§1.3).

### 1.3 What the person is told

- **The run log** (a plaintext archive's `mailarchive.log`, or the command line's `-log`): one line
  per skipped input. "Skipped PATH: it is no longer on this computer. The mail already saved from it
  stays in the archive; the other mailboxes were backed up."
- **The last-run record** (plaintext only): one `RunWarning` per skipped input, code
  `mailbox_gone`. Two additive, optional fields on `RunWarning` carry it: `Path` (the skipped input,
  as listed) and `Since` (when a run first found it gone). A run copies `Since` from the previous
  last-run record's `mailbox_gone` warning for the same input, compared by `state.PathSourceID`;
  otherwise `Since` is the run's start. `Message` keeps a whole sentence, for any reader that does
  not know the new fields.
- **status and the Desktop's health view** (one status truth, X10). `internal/health` adds
  `mailbox_gone` to its list of known warning codes and builds the words itself from `Path` and
  `Since`, cleaned of control characters. It never shows the recorded `Message` for this code.
  - For 14 days from `Since` it is a WARN: "a mailbox this archive backs up is no longer on this
    computer: PATH (not found since DATE). Its mail already saved stays in the archive, and the
    other mailboxes are still backed up. If you removed that account on purpose, nothing needs
    doing: this becomes a note on DATE+14."
  - After that it is a note (GREEN): "a mailbox this archive backed up is no longer on this
    computer: PATH (not found since DATE). Its mail already saved stays in the archive."
  - A `Since` in the future (a clock that went back) counts as the run's start: the WARN shows,
    and it is never hidden by a wrong date.
- **The wizard's summary and the command line's end-of-run summary.** These carry the same count
  as status (X6): "Not found, so skipped: N mailbox(es). They are no longer on this computer, and
  the mail already saved from them stays in the archive:", then one path per line.
- **Notices and emails.** None for a skip: the run succeeded. An archive that was failing because
  of a gone mailbox recovers on its first 1.7.5 run, and gets the existing recovery notice and
  email if alerts are on. Status then says which mailbox is gone.
- **Encrypted archives.** The skip applies to them too. Nothing new is written beside them: their
  record's frozen allow-list keeps no warnings (`internal/state/encrypted_record.go:136-149`), so
  status cannot show the skip. The person is told on screen in the attended run's own result (the
  wizard's summary, or the command line's output). *Conditional:* whether Desktop's Run now result
  for an encrypted archive can carry the line without writing it anywhere is to be checked in the
  build. If it cannot, that surface says nothing, and this note says so.

### 1.4 The Desktop's schedule rebuild

In `reconstructLocalJob` (`localschedule.go:77-109`), for a job with `-auto`, a reconstructed
`-input` that is gone (its `Lstat` says it does not exist) and that lies where auto-discovery looks
(§1.2 (a)) is dropped from the rebuilt job's inputs instead of refusing the rebuild. Every other
check stays for every input that exists: not a link, not a special file, not under the archive's
`_outlook-pst`, and one of the archive's own manifest stores (adversarial finding A2). A gone input
outside those places, or any gone input of a job without `-auto`, refuses as today. The two stale
comments are corrected.

### 1.5 Rows

- **MA-103, amended** (owner, 2026-11-19; nothing weakened for a plain `-input`): "… a missing
  `-input` path is refused …, except with `-auto`, where a missing listed input in a place
  auto-discovery looks is skipped, and warned, while another mailbox remains (MA-1471)".
- **MA-688, amended:** the typed error is returned for such an input only when nothing remains.
- **MA-1471** (new): the rule in §1.2, through `DiscoverInputsSkipping`, for `app.Run`, Run now,
  the scheduled run and the command line. It also covers the run log's line and both summaries.
- **MA-1472** (new): the record's `mailbox_gone` warning, `Since` carried from run to run, and
  status's WARN turning into a note after 14 days, the same in the Desktop's health view.
- **MA-1473** (new): the rebuild drops a gone input that lies where auto-discovery looks, under
  `-auto` only, and keeps A2's checks for every input that exists.

## 2. An empty answer to "Keep this archive current?" installs nothing

- `offerSchedule` (`wizard.go:2120-2134`) installs a daily backup for any answer except the exact
  "No — just this once". On Linux, zenity can answer a list with nothing selected: it exits 0 with
  empty text (`third_party/zenity/list_unix.go:7-27`, `util.go:93-108`). MA-1465 already handles
  that case for the mailbox list. This is read in the code and not run.
- **Change:** only the exact daily row or weekly row installs. Any other answer, the empty one
  included, installs nothing (R14: schedules are opt-in).
- The wizard's other lists already fall to their safe default on an empty answer: export mode goes
  to Incremental (`wizard.go:903`), mail app open goes to No (`:925`), keep `.eml` goes to No
  (`:939`), follow links goes to none (`followLinksFor`, `:1090-1095`), and the Thunderbird sync
  goes to No (`:2147`). The build re-checks the full list.
- **MA-1474** (new).

## 3. Delete waits for you

### 3.1 A plan waits until you are done with it

Today a delete plan is held for 10 minutes from when it is ready (`pendingLife`,
`internal/desktop/pending.go:24`, `:161`; MA-972). A large archive's check can take minutes. A
person who steps away comes back to "That delete is no longer waiting — click Delete… again;
nothing was deleted.", and then waits through the check again.

**Change:** a delete plan waits up to 24 hours (a new `deletePlanLife`). Every other kind of held
item keeps `pendingLife`'s 10 minutes: a new or changed key, a recovery key to put back, a forget,
an unencrypted copy, a restore to Thunderbird, and a new encrypted archive. Several of those hold
keys. A delete plan still ends at once when it is used, when a new plan of the same folder replaces
it, when the current archive is switched (by its own kind), and at Shut down.

**Why this is safe:** the 10 minutes never guarded what is deleted. At Delete the plan is checked
again under the archive lock (`docs/design-archive-delete.md` §3.1 step 2). The folder must be the
same by identity. The manifest or vault must be unchanged in size, time and SHA-256. The schedule
entries and the unrecorded list must be unchanged, the scratch listing must still be allowed, and
none of §7's refusals may be new. Any difference refuses with "This archive changed since you
clicked Delete. Click Delete again." and removes nothing. A plan kept longer can only be refused
more often; it can never delete more. The token stays one-use and kind-filtered, behind the page's
CSRF token and the typed name.

*To check in the build:* that the deletion record (§8.1) says when the check was made, not only
when the delete ran, so a record of a delete hours after its check is not misleading. If it does
not, the record gains the check's time.

- **MA-972, amended:** "… a one-use, kind-filtered token … gone after 24 hours for a delete plan
  (ten minutes for the other kinds) …".
- **MA-1475** (new): a delete plan's life, and every other kind's.

### 3.2 Delete without waiting

The owner asked for a way not to wait for the check. The check itself stays: it is what tells the
archive's own files from the person's, finds the schedules to stop, and notes a synced folder.
Only the waiting goes.

**What the person sees.** Clicking Delete… on a row opens the panel at once, before the check
ends:

- "Delete this archive permanently"
- "Checking what Delete would remove… This can take a few minutes for a large archive. Nothing is
  deleted yet." While the check runs, the minutes so far are shown.
- "Folder: PATH"
- "Type the folder's name, NAME, to delete it (capital letters count)", with the text box ready.
- **Delete when the check finishes** (danger style; enabled once the typed name matches) and
  **Cancel**.

Clicking **Delete when the check finishes** changes the panel's line to: "MailArchive deletes this
archive as soon as the check finishes, unless the check finds something you should see first.
Keep this page open." When the check ends:

- **If the plan is plain** (below) and the typed name matches the plan's name
  (`archdelete.NameMatches`), the page sends the delete at once, exactly as **Delete permanently**
  does. It shows the result and **Save deletion record** as today (MA-984).
- **Otherwise** the full plan is shown as today, with a first line: "The check found something to
  look at before deleting. Read it, then click Delete permanently." The typed name is kept. Nothing
  is deleted until **Delete permanently** is clicked.

If the person does not click it, the panel fills with the plan when the check ends, as today, with
the typed name kept.

**Plain** is computed on the server, in `planView`, as a new field `plain`. It is true only when
none of these is in the plan:

- mail copies the archive did not record (the "Also delete these files" box needs an answer);
- a synced-folder line;
- "Not removed, check it yourself" schedules, or the unconfirmed and other-computer schedule notes;
- files another program holds (`heldLine`);
- the finishing of an earlier delete that stopped part way (`finishesLine`, `finishesNotes`,
  `rerun`);
- an encrypted archive (its key line).

Files that are not the archive's (`kept`) and a paused archive do not stop it: Delete keeps such
files, and the result names them.

**The name before the check.** The row's Delete… button gains `data-delete-name`: the last element
of the row's path, as the page is drawn. No folder is read to draw the page, so an unplugged drive
cannot slow it. The plan's own name (`onDiskName`) can differ in letter case or form. When it does,
the armed delete does not fire: the plan is shown, and the box is checked again against the plan's
name.

**What it never does:**

- It never deletes on a plan the person did not type the name for.
- It never deletes on a refusal, or on the "finished elsewhere" question (MA-1057): those are shown
  as today.
- It never deletes after Cancel. Cancel during the check closes the panel. The check's reply, when
  it comes, opens nothing, and its plan waits unused until it ends (§3.1).
- A reload the page starts itself is held while a delete waits for its check (as for a delete
  waiting for its reply, MA-986). The waiting panel is marked `data-delete-waiting`, so another
  row's Delete… does not close it. Its check's reply is handled by its own panel even after a later
  Delete… or Remove from list (today, a later click drops an earlier reply, `delSeq`).
- When the page is closed or loses its connection during the check, nothing is deleted.

**Look before writing (X14).** This is a scoped exception the owner asked for. The check still runs
in full. The delete still carries out exactly the checked plan, checked again under the lock. Only
showing the plan first is skipped, only when the person asks, and only when nothing in the plan
needs their decision. The deletion record still says everything that was removed and kept.

- **MA-1476** (new): `plain`, and what makes a plan not plain.
- **MA-1477** (new): the page's half. The panel opens at once; the name can be typed during the
  check; **Delete when the check finishes** fires only on a plain plan whose name matches;
  otherwise the plan is shown. A waiting panel survives another Delete…, reloads are held, and
  Cancel during the check deletes nothing. These are structural tests on the page script, plus the
  friction review's walk.

## 4. Reviews owed and the order of work

1. This note: the 10-lens pre-code design review. It is security-relevant: it changes what feeds
   source loss and auto-pause, and the confirmation of a destructive action.
2. Build, test first. Every row gets a prove-fail → prove-pass by mutation, recorded in its commit.
3. After the build: the adversarial pass (one pass over all three parts) and the friction review
   (every new word, and the waiting delete walked).
4. Windows lab on the office test PC. A combined archive made with "All of them" over a test
   Thunderbird profile made of folders only (no Thunderbird installed), with one account folder
   removed: Run now, a scheduled run, and Settings saving the schedule. Then a Delete of a large
   archive with **Delete when the check finishes**. A local macOS walk. Linux's empty answer
   cannot be run here, which is said.
5. CI on all three systems, then release 1.7.5 (owner, 2026-11-19: "make it so").

## 5. Not in this change

- Per-mailbox archives: the next release, with their own design.
- An email or system notice when a mailbox is first found gone; a "Stop looking for it" button.
- Making the delete check itself faster.

## 6. Claims, each with its condition

- **P1.** With `-auto`, one gone mailbox where auto-discovery looks no longer refuses the run
  while another remains. *Proven by MA-1471 once built.*
- **P2.** When nothing remains, when the input is outside those places, or without `-auto`, the
  run is refused as today and the streak moves as today. *Proven by MA-1471 and MA-688.*
- **P3.** The person is told: a WARN for 14 days, then a note, the same in status and the
  Desktop's health view. *Proven by MA-1472.* For encrypted archives, only on screen in the
  attended run's result. *Conditional on the build's check (§1.3).*
- **P4.** Settings can save such an archive's schedule again, and A2's checks hold for every input
  that exists. *Proven by MA-1473.*
- **P5.** An empty answer to the schedule question installs nothing. *Proven by MA-1474 for the
  wizard's handling of the answer; conditional on Linux zenity giving exit 0 and empty text for OK
  with nothing selected, which is read in the code and not run.*
- **P6.** A delete plan waits 24 hours; every other kind waits 10 minutes. *Proven by MA-1475.*
- **P7.** **Delete when the check finishes** deletes only on a plain plan whose name the typed
  text matches; otherwise the plan is shown. *Proven structurally by MA-1476 and MA-1477; the
  page's live behaviour by the friction walk and the Windows lab.*
