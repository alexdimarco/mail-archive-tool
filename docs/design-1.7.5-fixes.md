# 1.7.5: a combined archive keeps going, an empty answer installs nothing, and Delete waits for you

**Status:** DRAFT, revision 3, 2026-11-20, for the lead to confirm the fold check's fixes
(`../assurance-kit/process/design-review.md`, procedure step 4). Revision 1 (commit `47f9efd`) was
reviewed in `docs/review-1.7.5-predesign.md` (commit `34e3baa`): GO WITH CONDITIONS, 30 confirmed
findings (1 HIGH, 10 MEDIUM, 19 LOW), 4 owner questions. Revision 2 (commit `9978a26`) folded
every confirmed finding and recorded the owner's four answers. Its fold check (filed in
`docs/review-1.7.5-predesign.md`, "Fold check of revision 2") said CLEARED AFTER THESE FIXES: the
owner citation in MA-1472 (L1-02), R1 to R3 (must fix) and R4 to R9 (text). This revision applies
them and records the owner's fifth answer (§0, Q5), which closes O1. §8 maps every finding and
every fix. Open items are in §7. Branch `combined-archive-gone-mailbox`, off main `ed491c4`
(release 1.7.4). Nothing is built yet.

**Revision 3.1, 2026-11-20:** revision 3 with the owner's sixth answer recorded (§0, Q6): an
encrypted archive's record also keeps how many mailboxes are gone, a number only, so a new loss
restarts its warning. It closes O5 (§7). Folded in §1.3 ("Encrypted archives"), §1.6 (MA-734, A6
and phase 2a's allow-list line, MA-1478), §5, P5 and §8. Revision 3's text is kept; each change is
written beside what it changes.

**Revision 4, 2026-11-20:** the post-build reviews folded and built: the adversarial pass
(`docs/review-1.7.5-adversarial.md`: OUT-01 with INS-01, INS-03, INS-06, CON-01, CON-03, OUT-02,
OUT-03, OUT-04's guard; CON-04's correction to R9; INS-05 a known limit; INS-04 the Windows lab's)
and the friction review (`docs/review-1.7.5-friction.md`: F1 to F14), and the owner's four answers
to Q7 to Q10 (§0). Each change is written beside what it changes, marked *Revision 4*; §9 lists every
fix, its commit's subject and its rows. Revision 3.1's text is kept.

**Revision 4.1, 2026-11-20:** the re-verify of revision 4's fixes (`docs/review-1.7.5-reverify.md`,
the lead's fold) built: RV-01, RV-02 (the lead's call, the fuller form of Q7), RV-03, RV-04, RV-06,
RF-01 to RF-10, and Windows CI's untyped refusal at 1ba95c8. Each is marked *Revision 4.1* beside the
text it changes; §10 lists them. RV-05, RV-07 and RV-08 are not built (the fold says why).

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
  else in the folder, but take the wait away (part 3.2). That narrower form had no recorded answer
  in revision 1; question 1 below is its answer.
- 2026-11-20, the owner's answers to the review's four questions (`docs/review-1.7.5-predesign.md`,
  "OWNER QUESTIONS"). Each was answered "Yes (Recommended)":
  - **Q1.** May **Delete when the check finishes** delete without first showing the list of what
    it removes, when nothing in that list needs a decision? It still runs the full check, checks
    again just before deleting, and the server refuses in any other case: a narrow, recorded
    exception to X14 ("you always see what will be deleted first"). Answer: "Yes (Recommended)".
    Folded in §3.2 ("X14, amended").
  - **Q2.** Does a gone mailbox's WARN turn into a note 14 days after it was first found gone,
    counted between backups, not by the calendar? Answer: "Yes (Recommended)". Folded in §1.3 and
    MA-1472.
  - **Q3.** May the encrypted archive's record carry one fixed sentence with no folder name in it
    ("a mailbox this archive backs up was not found"), so encrypted combined archives get the same
    skip and still warn? Answer: "Yes (Recommended)". Folded in §1.3, MA-734 (amended) and MA-1478.
  - **Q4.** With one email account plus Thunderbird's Local Folders, losing that one account keeps
    backups "succeeding" on Local Folders with a status WARN and no email; the WARN adds "if you
    didn't remove it, add the account back in Thunderbird". Answer: "Yes (Recommended)". Folded in
    §1.2 ("The commonest case"), §1.3 and P2.
- 2026-11-20, the owner's answer to revision 2's open item O1, "Yes (Recommended)":
  - **Q5.** "1.7.5, encrypted combined archives: when one of their mailboxes is gone, status warns.
    To turn that warning into a quiet note after 14 days (as you ruled for unencrypted archives),
    the encrypted archive must also keep the date the mailbox was first found gone: just a date,
    with no folder or account name. May it keep that date?" Answer: "Yes (Recommended)". Folded in
    §1.3 ("Encrypted archives"), §1.6 (MA-734, A6 and phase 2a's allow-list line, amended;
    MA-1478), P5, and §7 (O1 closed). What one date cannot tell apart is O5.
- 2026-11-20, the owner's answer to revision 3's open item O5, "Yes (Recommended)":
  - **Q6.** "Encrypted combined archives keep one "first found gone" date for the whole archive
    (your earlier yes). Rare case: a second mailbox goes missing while the first is still gone. It
    then inherits the first one's old date, so status may show only the quiet note instead of a
    warning for the new loss. May the encrypted archive also keep a count of how many mailboxes are
    gone (a number only, no names), so a new loss restarts the warning?" Answer: "Yes
    (Recommended)". Folded in §1.3 ("Encrypted archives": `mailbox_gone_count`), §1.6 (MA-734, A6
    and phase 2a's allow-list line, amended again; MA-1478), §5, P5, §7 (O5 closed) and §8.
- *Revision 4.* 2026-11-20, the owner's answers to the post-build reviews' four questions
  (`docs/review-1.7.5-adversarial.md` and `docs/review-1.7.5-friction.md`, the leads' folds), each
  the recommended answer:
  - **Q7** (the adversarial pass's INS-02): a Thunderbird account moved with its Local Directory
    setting, not removed, is skipped quietly; refuse, or skip with true words? Answer: "Stop the
    backup (Recommended)". Folded in §1.2 (*Revision 4*, rule (e)) and MA-1481.
  - **Q8** (CON-02): keep a first-found date later than this run's start, which changes one MA-1472
    test case? Answer: "Yes, change it (Recommended)". Folded in §1.3 (*Revision 4*, beside the
    clamp) and MA-1472, MA-1478.
  - **Q9** (the friction review's F5): when an armed delete is refused because a backup ran in
    between, check again and delete, once? Answer: "Re-check, then delete (Recommended)". Folded in
    §3.1 (*Revision 4*) and MA-1477.
  - **Q10** (OUT-05 (b), CON-06): should Lock end a waiting delete plan? Answer: "Yes, Lock ends it
    (Recommended)". Folded in §3.1 (*Revision 4*) and MA-1475.

The three parts are independent: each can be reviewed, built and reverted alone. They ship
together as 1.7.5 because each is small. The per-mailbox archives the owner also approved
(questions 2 to 4 of 2026-11-19) are the next release's, with their own design.

## 1. A missing mailbox no longer stops a combined archive

### 1.1 Today

- "All of them" in the wizard's mailbox list saves every mailbox found AND auto-discovery
  (`internal/wizard/picker.go:85-86`; `jobFor`, `internal/wizard/wizard.go:2235-2241`, writes
  `Inputs` and `Auto`). This has been so since `bc6e9d0` (2026-09-02). The list, and so "All of
  them", is shown only when two or more mailboxes are found (`picker.go:72-75`).
- Every run calls `DiscoverInputs(opts.Inputs, opts.Auto)` (`internal/app/run.go:631`). Its loop
  over the listed inputs returns `sourceUnreachableError` for the first one `os.Stat` says does not
  exist (`run.go:1981-1987`), before auto-discovery runs (`run.go:2006-2010`). The run is refused
  before any mailbox is read (`run.go:632-642`); an unattended run records it as failed
  (`RecordRefusedRun`). Run now does the same (`internal/wizard/jobrun.go:368-370`). The command
  line's `export` calls the same function up front (`cmd/mailarchive/main.go:275`).
- That error moves the source-loss streak (`internal/app/sourceloss.go:60-63`). Status warns
  after 3 days, and the scheduled run stops the schedule after 30 (`internal/state/sourceloss.go:32-35`;
  `AutoPauseIfDue`, `wizard.go:387`).
- So when one account's folder is gone (an account removed in Thunderbird together with its mail,
  or a leftover folder deleted), every account in that archive stops being backed up. Each run
  sends its failure notice and email, and after 30 days the schedule pauses itself.
- MailArchive Desktop rebuilds a local schedule from the last-run record in two places only: when
  Settings creates a schedule for an archive with no job file yet (`localschedule.go:193-197`), and
  when it checks whether a command-line schedule can be turned into a Desktop one
  (`internal/desktop/dashboard.go:1659-1675`). That rebuild refuses when any listed `-input` is gone
  (`reconstructLocalJob`, `internal/desktop/localschedule.go:77-109`), so Settings cannot create
  that archive's schedule again. An edit, Repair included, keeps the job file as it is and never
  rebuilds (`localschedule.go:188-192`), so it is not stopped. The rebuild's comment
  (`localschedule.go:74-75`) and `internal/desktop/localarchive.go:280-283` say an `-auto` job
  records no `-input`; since `bc6e9d0` that is not so for the wizard's jobs.

### 1.2 The rule

With auto-discovery on (a scheduled run, Run now, the wizard, or the command line's `-auto`
together with `-input`), a listed input is **skipped instead of refused** only when all four of
these hold:

- **(a) It is gone, not unreachable.** `os.Lstat` says it does not exist (`fs.ErrNotExist`).
  Today's check is `os.Stat` (`run.go:1982-1987`), which follows links, so a link whose target is
  on an unplugged drive reads as gone. For `os.Lstat` a dangling link exists, so it is not skipped:
  it refuses as today.
- **(b) It lies where auto-discovery looks.** Its path, cleaned, matches one of the patterns
  `autoDiscover` globs (`outlookGlobs`, `thunderbirdGlobs`, `evolutionGlobs`; `run.go:2069-2118`)
  by `filepath.Match`, the pattern cleaned too. Both sides are lower-cased on Windows only. A macOS
  volume can be case-sensitive APFS, where `filepath.Glob` would never find a path spelled in
  another case (`run.go:2049`, `:2060`), so on macOS and Linux the comparison is as spelled.
  (`state.PathSourceID` still folds case on macOS, `internal/state/manifest.go:490-503`: that
  answers another question, whether two spellings are one store already recorded.)
- **(c) Its parent folder is still there, as a real folder, in the same place.** `os.Stat` of the
  parent says it is a folder, and the parent resolved with `filepath.EvalSymlinks` matches the
  pattern's folder part (the pattern without its last element) rebuilt on the pattern's root (its
  leading part with no wildcard) resolved the same way. For a Thunderbird account
  `PROFILE/ImapMail/ACCOUNT`, the profile's `ImapMail` (or `Mail`) folder must be there. A link at or
  above the root (a home folder, or a whole `.thunderbird` folder, kept elsewhere) resolves alike on
  both sides and still counts. A link below the root (a profile folder or an `ImapMail` folder that
  is a link or a junction) takes the resolved parent outside the place, and the input refuses as
  today. A parent that cannot be read (an unplugged drive, an offline network share under a
  redirected `APPDATA`) refuses as today.
- **(d) At least one mailbox remains** after the skips: a listed input that exists, or a store
  auto-discovery finds this run.

*Revision 4:*

- **(a), amended (the adversarial pass's INS-03).** `os.Lstat` runs on the path as listed and on the
  path cleaned, and the input is gone only when both say it does not exist: a trailing separator,
  "/." or "link/.." make the system follow the last link, so a dangling link spelled so would
  otherwise read as gone.
- **(e), added (INS-02; the owner's Q7: "Stop the backup (Recommended)").** It was not moved: no live
  account in the profile's settings owns the folder by its hostname while every folder setting of
  that account (`directory`, `directory-rel`) names another place (`thunderbird.MovedTo`, through
  `autoplace.MovedTo` inside `autoplace.GoneSkippable`, so the run, the rebuild and the Repair helper
  agree). A moved account's run refuses as 1.7.4 did, with `sourceUnreachableError`, whose words
  (`autoplace.MovedText`) name the old folder, say Thunderbird still lists its account and where it
  now keeps its mail, and give the two ways to back that mail up; they name folders only, never the
  account's name (MA-1288). An account removed from the list, one whose folder setting still names
  the folder (deleted by hand: Q4) and one with no folder setting are skipped as before; a prefs.js
  or user.js that cannot be read with certainty tells no move. `autoplace` now also imports
  `internal/thunderbird`, a leaf of its own (MA-1481).
  *Revision 4.1 (RV-02, the lead's call):* "moved" is now: the gone folder lies directly under this
  profile's ImapMail/ or Mail/, exactly one live account has its server name (its name, or that name
  without a "-N" suffix), and none of that account's folder settings lies inside this profile's
  ImapMail/ or Mail/. So a move to a folder of another name, and a POP account's move from Mail/, stop
  the backup. Traded, and stated in MA-1481: an account removed on purpose whose only same-server
  sibling was moved off the profile stops the backup too (fail closed, Q7's direction). *(RV-04)*
  Settings that cannot be read for certain still tell no move (Q4), and the warning now says so.
  *(RF-03)* The refusal's words also say where the setting is and how to stop the archive looking
  for the old folder.
  *Revision 4.2 (the re-verify, round 2's RV2-01; the lead's call, for the owner's setup of several
  accounts on one mail server):* the archive's previous record decides first. It names each
  Thunderbird folder's account (`autoplace.RecordedName`: its entry by `store_id`, else its
  gone-mailbox warning). Named, and exactly one live account has that name with a folder setting
  that is not the gone folder: moved, stop. Named, and no live account has it: removed, skip. Two or
  more share it, or no name is recorded: the server-name rule, now the fallback, with Thunderbird's
  "-N" before the last dot ("imap.gmail-1.com") and at the end, and "exactly one same-server account
  wholly outside the profile". The traded case shrinks to records that name no account. The run, the
  command line's up-front check and the Repair helper ask it with the same record. *(RF2-01)* The
  words differ by which rule decided and never name the account (MA-1288 holds: a refusal reaches log
  files; the brief's "Thunderbird still has NAME" form is flagged for the lead). *(RV2-06)* A run's
  rule 1 also requires the warning's path to lie in an auto-discovery place.
  *Revision 4.3 (the re-verify, round 3's RV3-01, MEDIUM, and RV3-02, LOW; the lead's calls):* the
  names live in every plaintext record, not only an ok run's: a small list beside the completeness
  entries (`thunderbird_names`: store id and name), carried from the previous record into the
  running record and so into a refused, failed or cancelled run's, so a moved account stops every
  scheduled run and Run now, not only the first after the move. An ok run with entries of its own
  keeps them there, written as before. Encrypted records keep none (Q3, Q5, Q6). And "named, and no
  live account has it" now goes to the server-name rule instead of "removed", so an account moved
  and renamed at once stops; the traded case returns (a removed account beside exactly one
  same-server account wholly off the profile stops). *(RV3-04)* An encrypted archive's record names
  no account, so the server-name rule always decides for it (§5's known limit). *(RF3-03)* The
  same-server words say "…a Thunderbird account on the same mail server, or Local Folders, …".
  *Revision 4.1 (Windows CI at 1ba95c8):* a listed input that cannot be examined (`os.Stat` fails
  with anything but "does not exist") is refused with the same typed refusal, so it is recorded
  failed and the streak moves; it was an untyped refusal (Windows gives ERROR_DIRECTORY for a dangling
  link spelled with a trailing "\").
- **One rule for a non-local path (OUT-01).** `autoplace.NonLocal` holds the Desktop's
  `isRemotePath` rule (a network or device path, or one not absolute), which the Desktop, status
  and a run now share (§1.3, *Revision 4*). `GoneSkippable` itself does not ask it: a run's inputs
  come from the person's own job, not the record.
  *Revision 4.1 (RV-01):* on Windows a path is local only when its volume is a drive letter, so
  "\/host/…", "/\host\…" and "\??\UNC\host\…" are non-local too (`autoplace.nonLocal`, the rule as a
  pure function of the volume part); and the Repair helper refuses, before any look on disk, a record
  whose inputs lie outside the places auto-discovery looks (`autoplace.InPlace`).

Otherwise the run is refused exactly as today, with `sourceUnreachableError`
(`app/sourceloss.go:33-37`) naming the first listed input that does not meet (a) to (c), or, when
every gone input meets them and only (d) fails, the first gone one:

- A listed input outside those places (a `.pst` on a USB drive, a mail folder elsewhere) still
  refuses when it is gone, and so does one reached through a link below the place's root, or one
  that is a link to something gone. This is the "unplugged drive" case the owner was told stays
  caught.
- When a gone input's own group folder is gone too (the profile's `ImapMail` or `Mail` folder, the
  whole profile, or the user folder), the run refuses as today, whatever else remains: rule (c)
  fails. When no listed or discovered mailbox remains, it refuses too: rule (d). In both, the
  source-loss streak, the 3-day warning and the 30-day pause work as today.
- Without auto-discovery (an archive of one chosen mailbox, or `-input` alone), nothing changes.
- A listed input that exists but cannot be read stays a plain error, as today (`run.go:1987`).

**The commonest case, ruled by the owner (Q4).** A Thunderbird profile's Local Folders is a store
auto-discovery finds (`run.go:2097`), and "All of them" needs two stores found (§1.1). So the
commonest combined archive is one mail account plus Local Folders. When that account's folder is
gone and the profile's `ImapMail` folder is still there, rule (d) is met by Local Folders alone:
the backups keep succeeding on Local Folders, status WARNs (§1.3) with the words the owner asked
for, and no email is sent. The owner accepted this on 2026-11-20 (§0, Q4). Whether Thunderbird also
removes `ImapMail` when it removes its last IMAP account is recalled, not read; if it does, rule (c)
refuses that case as today.

**Why (b) and (c):** a gone input whose group folder is still there, in a place auto-discovery
looks, is one the same run would back up by itself if it came back. Skipping it loses nothing the
job's "(and any added later)" did not already promise to find. A gone group folder, a link, or a
place outside auto-discovery can mean something larger is missing, so it refuses.

**Where:** one function holds the rule, and every caller sees the skips. `DiscoverInputs` gains a
return, `DiscoverInputs(inputs, auto) (files, skipped []string, err error)`, with no wrapper that
drops the list, so a later caller (the next release's per-mailbox archives) cannot lose it
without saying so:

- `app.Run` records and reports the skips (§1.3) and returns them in its `Result` (`Skipped`, each
  with its path, the account's name when it can be read, and its `Since`).
- The command line's up-front call (`main.go:275`) uses them only for `-list` and `-follow-links`
  (§1.5).
- `prepareThunderbird` (`main.go:2088`) and the wizard's list (`wizard.go:787`, which passes no
  inputs) discard them on purpose (`files, _, err :=`, with a comment saying why): they prepare or
  list stores that exist, and the run reports the skip. With `auto` on, they now skip a gone input
  silently, where today they refuse; revision 1 wrongly said `DiscoverInputs` kept its meaning.
- Rules (a) to (c) are one exported test, `autoplace.GoneSkippable(path)`, in a small new package,
  `internal/autoplace`, that also holds the patterns auto-discovery globs: `outlookGlobs`,
  `thunderbirdGlobs` and `evolutionGlobs` (`run.go:2069-2118`) move there unchanged, and app's
  `autoDiscover` (`run.go:2034`) globs them from there. Nothing else calls them today. So the run
  (`DiscoverInputs`), the Desktop's rebuild (§1.4) and status (§1.3, "Archives 1.7.4 already
  stopped") all call one function and cannot disagree. Revision 2 put it in `internal/app`; it
  cannot stay there, because status's code in `internal/health` needs it (fold check R1 and R3)
  and `internal/health` cannot import `internal/app`: app's own tests, in package `app`, import
  health (`internal/app/p38_test.go:1`, `:13`). `autoplace` imports only the standard library and
  `internal/state`. `autoplace.GoneSourceID(path)` is the id of a gone input (§1.3), and
  `autoplace.GoneText(path)` is the words of a gone input's refusal, which
  `sourceUnreachableError.Error()` (`app/sourceloss.go:33-37`) returns unchanged, so whatever
  compares a record's error with an input's refusal compares one text.
- The tests that call `DiscoverInputs` today take the new return; none of their assertions is
  dropped.

### 1.3 What the person is told

- **The run log.** Straight after discovery, one line per skipped input, in the same log as the
  run's "Reading PATH (store: …)" lines (`run.go:1856`), so it says nothing that log does not
  already say: "Not found, so skipped: NAME (PATH). It is no longer on this computer; the mail
  already saved from it stays in the archive." It no longer says the other mailboxes were backed
  up: at discovery that has not happened yet. That is said only at the end, in the summaries, of a
  run that finished. NAME is the account's name when the profile's settings still name an account
  for that folder, read as the picker reads it (`thunderbird.FindProfileDir` and `ReadStoreFacts`,
  `picker.go:185-201`), and is left out otherwise (an account removed on purpose). Both read the
  profile's settings and the path (`internal/thunderbird/thunderbird.go:52-70`,
  `storefacts.go:82-110`); `matchAccountIn` was not read, so the build confirms that they answer for
  a folder that is gone, and leaves NAME out where they do not.
- **The last-run record** (plaintext). One `RunWarning` per skipped input, code `mailbox_gone`.
  Three additive, optional fields on `RunWarning` (`internal/state/lastrun.go:213-216`): `Path`
  (the input as listed) and `Name` (NAME above, or empty), each `omitempty`, and `Since` (when it
  was first found gone), `omitzero`, as the record's other times are (`lastrun.go:192-193`):
  `omitempty` does not leave out a zero time, so it would add a `since` to every other warning.
  So a record without them is written byte for byte as before (MA-734's plaintext twin).
  `ReadLastRun`'s single cleaning point (`lastrun.go:374-380`, INS2-2) strips control characters
  from `Path` and `Name` too, beside `Code` and `Message`, so every reader gets them clean, not
  only `internal/health`. `Message` keeps a whole sentence; no reader shows it for this
  code (below).
- **How `Since` is kept: in the run's own warnings, not in a new file.** The skip warnings go into
  `opts.Warnings` straight after discovery (`run.go:631`), before the out-guard's second check, the
  lock (`run.go:676`) and `beginRun` (`run.go:743`). `opts.Warnings` already reaches every record
  the run writes: the "running" record (`newRunRecord`, `run.go:1390`), a refused one
  (`writeRefusedRun`, `run.go:1445-1459`) and the final one (`recordRun` starts from the running
  record, `run.go:807`, `:1533`). So a run that is cancelled, killed part way (its running record
  stays), or refused after discovery keeps every `Since`. Each `Since` is resolved there, once,
  from what is on disk before this run writes anything:
  1. the previous last-run record's `mailbox_gone` warning for the same input, compared by
     `autoplace.GoneSourceID`;
  2. else, when the source-loss streak's last kind is `local-source-unreachable` (read now, before
     this run's end resets it, `app/sourceloss.go:86-102`, `state/sourceloss.go:64-73`) and the
     previous record is a failed run whose error is this input's own refusal
     (`autoplace.GoneText`, the words of `app/sourceloss.go:35-37`; 1.7.4 names the first gone
     input in list order), the streak's `FirstUnreachable`: when the archive that was failing first
     found it gone;
  3. else this run's start.

  **What rule 2 cannot tell (fold check R9).** 1.7.4 names only the first gone input in list order,
  and the streak keeps one first date for the whole archive. If another input's refusal started
  the streak days earlier (another input went first, and this one, which comes before it in the
  list, went later), this input gets that earlier date, so its WARN is shorter by those days. It
  is bounded by the streak's own start, and it arises on the way up from 1.7.4: a 1.7.5 record
  carries each skipped input's own `Since` (rule 1), so rule 2 is reached only when the previous
  record has none for this input.
  *Revision 4, corrected (the adversarial pass's CON-04):* "on the way up from 1.7.4" is not the
  only way. 1.7.5's own refusals at discovery (a gone group folder, a link, a place outside, nothing
  left; and, since Q7, a moved account) record no skip warnings, so the next run that skips reaches
  rule 2 too, or rule 3. Its date is then the streak's first date or a later one: a mailbox already
  a note can WARN again for 14 days from a later date. That is the direction this design accepts
  (longer, never shorter); no code changes.

  A `Since` later than this run's start becomes this run's start (a clock that was ahead). One
  before `state.MinPlausibleTime` (2024-01-01, `state/sourceloss.go:93-95`; a clock that was not
  set) becomes this run's start and is never carried, as the streak does (MA-891). The previous
  record is read before the lock. That is safe: it is written whole, by a rename
  (`lastrun.go:231-260`), and a run that holds the lock meanwhile makes this one refuse at the lock,
  writing nothing (`lockRefusal`, `internal/app/emptied.go:213-220`).
  *Revision 4 (CON-02; the owner's Q8: "Yes, change it (Recommended)"):* a `Since` later than this
  run's start is now kept as recorded. From the records alone a clock that was ahead cannot be told
  from this run's clock being behind, and moving the date to the start in the second case moved it
  earlier for good and cut the WARN short. Kept, it can only make the WARN last longer: status counts
  a date after the run's start as no time, so it WARNs, and says its dates from `Since` as recorded.
  The encrypted record's time shares the clamp. The zero and pre-2024 rules are unchanged.
  *Revision 4 (OUT-01, with INS-01):* rule 1 passes over a previous warning whose path is not a
  plain local one (`autoplace.NonLocal`) before `autoplace.GoneSourceID` looks at it, and reads at
  most `state.MaxGoneWarnings` (64) of a record's warnings (INS-06). *Revision 4 (the friction
  review's F3):* when the profile's settings no longer name the account (removed on purpose), its
  name is taken from the previous record's Thunderbird entry with the same `store_id`, then carried
  from the previous warning; it is said on screen and in status, never in a log file (MA-1288). The
  warning gains `listed` (omitempty) when the name came from the settings (F4).
  **The id of a gone input.** `state.PathSourceID` of a gone path cannot resolve links
  (`manifest.go:490-503`: `EvalSymlinks` fails, so the path is only cleaned), while a recorded
  store's id was resolved when the store existed. `autoplace.GoneSourceID(path)` is `state.PathSourceID`
  of the parent resolved (rule (c) says it is there) joined with the input's last name, so a link
  above it (on macOS, `/var` is one) resolves as it did when the store was recorded.
  **Why not a small file beside the source streak**, the review's other choice:
  `state.PathSourceID` is the path itself (`"path:" + abs`, `manifest.go:490-503`), so such a file
  would name the archive's mailboxes beside it. It could never be written beside an encrypted
  archive (A6, P2a-C16), so it would serve plaintext archives only, as the record does. It would
  also need its own bounded reader, its cleaning, a version, a rule for dropping inputs no longer
  listed, and its name added to MailArchive's own files for the emptied-folder guard
  (`emptied.go:34`) and for the encrypted tree's check (`internal/assure/leak/tree.go:39`). What it
  would add is a `Since` that survives a run whose record lacks the warning: a one-off run into the
  archive without `-auto`, a record that cannot be read, a run refused before discovery, a
  downgrade to 1.7.4. Each of those restarts `Since` at a later date, so the WARN lasts longer,
  never shorter. That direction is accepted.
- **status and the Desktop's health view** (one status truth, X10). `internal/health` adds
  `mailbox_gone` to the codes it knows (`internal/health/health.go:778-805`) and builds the words
  itself from `Path`, `Name` and `Since`. It never shows the recorded `Message` for this code.
  - **The 14 days (owner, Q2) are counted between backups.** The WARN shows while the last run's
    start (the record's own `Started`) minus `Since` is under 14 days. It is never compared with
    now, like the 3 and 30 days (`state/sourceloss.go:23-38`). So a computer that was off for two
    weeks after the run that found the mailbox gone shows the WARN until its next backup; that
    backup starts 14 days or more after `Since`, so from then on it is a note (MA-1472's own
    example). Keeping the WARN through a computer being off would be a new rule, the owner's to
    make; this design does not add it. A `Since` that is zero, before `MinPlausibleTime`, or after
    `Started` (a planted or damaged record) counts as no time at all, so the WARN shows. The length
    is a new constant beside the other two, `state.MailboxGoneNoteAfter = 14 * 24 * time.Hour`,
    whose comment cites the owner's ruling of 2026-11-20.
  - The WARN (the friction review owns the final words): "a mailbox this archive backs up is no
    longer on this computer: NAME (PATH), not found since DATE. Its mail already saved stays in the
    archive, and the other mailboxes are still backed up. If you removed it on purpose, nothing
    needs doing: from the first backup on or after DATE+14 this is only a note. If you didn't remove
    it, add the account back in Thunderbird and the next backup finds it again." For a path in
    Outlook's or Evolution's place, the last sentence names that program instead.
  - **"The other mailboxes are still backed up" only after a run that ended ok** (fold check R7).
    The warning now reaches the running, cancelled and refused records too (above), and health
    shows record warnings whatever the run's status (`health.go:778`). So the clause ", and the
    other mailboxes are still backed up" is said only when the record's status is `ok`; for any
    other status the sentence ends at "stays in the archive.", and the run's own outcome is said
    beside it as today (a cancelled run's WARN, a failed run's RED).
  - After that, a note (GREEN): "a mailbox this archive backed up is no longer on this computer:
    NAME (PATH), not found since DATE. Its mail already saved stays in the archive. This line stays
    while the archive's backup lists that mailbox; to end it, set the archive up again in the
    wizard, with the same archive folder, choose All of them, and answer Yes to keeping it
    current." The last part is needed (fold check R8): only that answer rewrites the job file
    (`installOffered`, `internal/wizard/wizard.go:2185-2198`, writes it at `:2195`), and a job file
    the wizard does not rewrite keeps listing the gone mailbox.
  - *Revision 4, the words as built after the friction review and the adversarial pass:* "on or
    after DAY" names the first local day every backup of which is 14 days on (CON-01: since plus
    14 days at local midnight names that day, else the next); a warning with no path says "a mailbox
    this archive backs up was not found, so it was skipped; its record does not say which. Click Run
    now in MailArchive Desktop to see which one." (CON-03); "and Local Folders is still backed up"
    when it alone is left (F12); the last sentence is "Thunderbird still lists this account: open
    Thunderbird and let it download the account's mail; the next backup finds it again." for a
    listed account (F4) and "If you moved this file, put it back in FOLDER, or click Make a new
    archive… in MailArchive Desktop and choose it in its new place." for an Outlook .pst (F9); the
    note's remedy names the button: "to end it, click Make a new archive… in MailArchive Desktop,
    choose All of them and this same archive folder, and answer Yes to keeping it current." (F11's
    words; the "Stop looking for it" button stays in §5). Status says at most 64 gone mailboxes,
    then one count line (INS-06), and reads Name and Path cleaned (`CleanFolderName`, no bidi).
  - **It is permanent until then.** A gone input never leaves the job by itself: an edit never
    rebuilds (`localschedule.go:188-192`), and §1.4 keeps it. So the run log line, the summaries'
    lines and the note repeat on every run. The note's last sentence says how to end it, including
    the Yes to keeping it current; there is no new button (§5).
  - **A downgraded status** (1.7.4 or older) shows nothing for this warning: health shows only the
    codes it knows (`health.go:778-805`), and nothing reads `Message` for a code it does not know.
    Revision 1's reason for keeping `Message` ("for any reader that does not know the new fields")
    is dropped; the release notes say it (§4).
- **The summaries** (X6: the same count as status). The wizard's summary (`exportSummaryFor`,
  `wizard.go:1937`), the command line's end-of-run summary (`printSummaryFor`, `main.go:2404`,
  printed only for a run that reached its end, `main.go:444`), the plaintext Run now note
  (`runNowSummaryFor`, `jobrun.go:132`) and the encrypted one (`encryptedSummaryFor`,
  `jobrun.go:298`) each add, from `Result.Skipped`: "Not found, so skipped: N mailbox(es). They are
  no longer on this computer, and the mail already saved from them stays in the archive; the other
  mailboxes were backed up:", then one "NAME (PATH)" per line, then "If you didn't remove one on
  purpose, add its account back in Thunderbird." These are said at the end of a run that finished,
  so "were backed up" is true when it is said.
- **Notices and emails.** None for a skip: the run succeeded (owner, Q4: "no email"). An archive
  that was failing because of a gone mailbox recovers on its first 1.7.5 run and gets the existing
  recovery notice and email if alerts are on; status then says which mailbox is gone. An archive
  1.7.4 already stopped makes no run by itself (next item).
- **Archives 1.7.4 already stopped.** An archive 1.7.4 auto-paused has no live scheduler entry
  (`AutoPauseIfDue`, `internal/wizard/pause.go:124-147`), so no 1.7.5 run comes by itself. Today
  status and the Your-archives banner ask to "reconnect the source" (`health.go:511`,
  `dashboard.go:2392`), and the Settings card says "Backups stopped: this archive's source has been
  unreachable since DATE." (`internal/desktop/settingscard.go:205-206`). A person who removed the
  account on purpose cannot reconnect it.
  - **When the new words show (fold check R1).** Only when a Repair or a Run now would really get
    past the gone mailbox now. Revision 2 showed them for every `-auto` archive whose streak's last
    kind is `local-source-unreachable`; but 1.7.5's own refusals set that kind too (MA-688,
    amended): a gone group folder, a link, a place outside, nothing left. There Repair would fail
    again, and saying it works would be untrue. So one helper,
    `health.GoneMailboxRepairable(out) bool`, says yes only when all of these hold:
    - the archive is plaintext: its record is not marked `Encrypted` and `encmark.MustMinimise(out)`
      is false, as `tbBanner` checks (`internal/desktop/archives.go:248-256`);
    - the source streak's last kind is `local-source-unreachable` (`state.ReadSourceStreak`, read
      as `health.go:505-512` and `sourceLostSince`, `archives.go:263`, read it);
    - the last-run record, read bounded, never through a link and cleaned (`state.ReadLastRun`,
      `lastrun.go:354`), is a failed run with `-auto` in its job (`canonicalJob`,
      `run.go:1493-1500`);
    - its `Error` is exactly `autoplace.GoneText(in)` for one of the job's listed `-input`s: the
      input the failed record names (the refused run records `cause.Error()` whole,
      `run.go:1447`; `withKind` keeps the words, `internal/app/failurekind.go:22`);
    - `autoplace.GoneSkippable` accepts that input now (rules (a) to (c), §1.2);
    - and no other listed input would refuse the run: each is there (`os.Stat`, as the run's own
      check) or `GoneSkippable` accepts it, and at least one listed input is there (rule (d), as
      far as the job's own list shows; a store only auto-discovery would find is not looked for,
      so then the helper says no). This last condition is added to the fold check's fix, in the
      same direction: the new words show in fewer cases, never more.

    Otherwise every surface keeps today's words exactly.

    *Revision 4 (the adversarial pass's OUT-01 with INS-01, MEDIUM; MA-869):* the record is
    untrusted (C-17), so the helper also says no, before any file check, when any `-input` its job
    lists is not a plain local path (`autoplace.NonLocal`); and the Desktop asks it inside
    `withinSourceWait` (the banner, the Settings card, and the health view through
    `health.Input.GoneRepair`), so a stalled look draws the page within `sourceWait` with today's
    words. A profile under a redirected network `APPDATA` therefore keeps today's words, the safe
    direction. The helper says no for a moved account too (Q7, through `GoneSkippable`).
    *Revision 4 (the friction review's F2 and F10):* the words name the missing mailbox ("(the
    missing mailbox: PATH)", `health.GoneMailboxRepairableInput`), say "MailArchive now skips" for
    "this version skips", and the Settings card says "click Repair on the Scheduled backup card";
    status's RED line drops the drive advice where the helper says yes; with the current archive's
    scheduled entry there again (a Repair done, or not yet paused), the banner and status say "Its
    scheduled backup is set, and MailArchive now skips a mailbox that is gone, so the next backup
    backs up the others; to back up now, click Run now …", and Repair's reply says "Repaired. The next
    backup skips the missing mailbox; to back up now, click Run now." No repair date is kept, so F2's
    "Repaired on DATE" is said that way.
  - **One helper, in `internal/health` (fold check R3).** Revision 2 put it beside
    `sourceLostSince` in `internal/desktop`; status's code (`health.go:505-512`) could not call it
    there, since desktop imports health (`internal/desktop/maintenance.go:14`), so status would
    have needed its own copy, the two truths X10 forbids. `internal/health` already reads the
    streak and the record. Status calls it in `Assess`; the Desktop's `archiveRows`
    (`archives.go:185-206`) sets a new `ArchiveRow.SourceLostRepair` from it beside `RunNowOnly`;
    and the Settings card calls it beside its auto-paused line. The rule it uses,
    `autoplace.GoneSkippable`, is the run's own (§1.2 "Where").
  - **The words** (the friction review owns the final words). In status, for a scheduled archive:
    "this archive's source has not been reachable since DATE — if a mail account was removed,
    click Repair (or Run now) on the archive's Scheduled backup card in MailArchive Desktop: this
    version skips a mailbox that is gone and backs up the others; otherwise reconnect the source
    (its mailbox or mail folder) to resume backups"; for an archive whose backup runs only on Run
    now (`manualBackup`, `internal/health/manual.go:17`), "click Run now" in place of "click Repair
    (or Run now)".
    On the banner, MA-949's two forms are kept (`dashboard.go:2392`, pinned at
    `internal/desktop/ma949_manual_backup_test.go:551-553`): "This archive's source hasn't been
    reachable since DATE. If a mail account was removed, click Repair (or Run now) on its Scheduled
    backup card: this version skips a mailbox that is gone and backs up the others. Repair starts
    the schedule again; Run now backs up once. Otherwise reconnect the source (its mailbox or mail
    folder) to resume backups."; for a Run-now-only archive, "click Run now on its Scheduled backup
    card: this version skips a mailbox that is gone and backs up the others; its backup runs only
    when you click Run now. Otherwise reconnect the source (its mailbox or mail folder)."; and for a
    row that is not the current archive, in either form, "click Switch to this archive, then
    click …", since the Scheduled backup card shows only the current archive (the reason the test's
    comment gives, `ma949_manual_backup_test.go:530-534`). The Settings card's line, drawn only for
    the current archive and never for a Run-now-only one (`settingscard.go:202-206`), adds: "If a
    mail account was removed, click Repair: this version skips a mailbox that is gone and backs up
    the others."
  - **What stays as today.** Every archive the helper says no to keeps today's words, which their
    tests pin: the banner's three forms (`ma949_manual_backup_test.go:551-553`, MA-949 and MA-692;
    `internal/desktop/sourceloss_surface_test.go:64`, MA-692) and status's
    (`internal/health/sourceloss_health_test.go:35`, MA-693). Those tests draw a streak with no
    kind and no record, so the helper says no and they hold unchanged. MA-692, MA-693 and MA-949
    are amended (§1.6). The 1.7.5 auto-pause email and log line (`wizard.go:392`, `:461`, pinned
    at `internal/wizard/wording_v152_test.go:31`) keep "reconnect the source": 1.7.5 pauses an
    `-auto` archive only after runs the skip did not apply to (a gone group folder, a link, a place
    outside, nothing left), where Repair alone would fail again (open item O2, accepted by the fold
    check).
  - When a plaintext archive 1.7.4 stopped runs again, its first `Since` comes from the streak
    (rule 2 above), not from the day of the upgrade.
  - An encrypted archive's record has no job and its streak no kind (A6; `app/sourceloss.go:91-93`),
    so the helper says no and its words stay as today; the release notes tell those owners to click
    Repair (§4). Its first-found time starts at its first 1.7.5 run that skips (next item).
- **Encrypted archives** (owner, Q3 and Q5). The skip applies to them too. Their record stays the
  frozen allow-list (`encryptedLastRun`, `internal/state/encrypted_record.go:141-149`) plus the two
  fields the owner allowed:
  - `mailbox_gone` (Q3), a boolean written only when it is true, meaning this run skipped a
    mailbox;
  - `mailbox_gone_since` (Q5), written only beside `mailbox_gone`: the time a mailbox was first
    found gone. A time only: no path, no name, and no count of mailboxes beyond what the boolean
    already says.
  - *Amended by revision 3.1 (owner, Q6):* `mailbox_gone_count`, written only beside
    `mailbox_gone`: how many mailboxes this run skipped, a number only (no path, no name). The
    "no count" above is replaced by this one count, at the owner's word.

  `LastRun` gains `MailboxGone` (`omitempty`) and `MailboxGoneSince` (`omitzero`), which a
  plaintext record never sets, so a plaintext record is written byte for byte as before (MA-734's
  plaintext twin). Both are set by `newRunRecord`'s encrypted branch (`run.go:1374-1384`) from the
  run's own skip warnings, which stay in memory and are never written beside the archive.
  `marshalLastRun` (`encrypted_record.go:153-165`) copies them into `encryptedLastRun` only when
  `MailboxGone` is true, and MA-734's key check (`internal/state/encrypted_record_test.go:71`)
  gains these two keys, with the owner's rulings (MA-734, A6 and phase 2a's allow-list line,
  amended, §1.6). *Revision 3.1 (owner, Q6):* `LastRun` also gains `MailboxGoneCount`
  (`omitempty`), set the same way (the number of this run's skip warnings), copied only when
  `MailboxGone` is true, and the key check gains `mailbox_gone_count` too; a plaintext record never
  sets it.
  - **How the time is kept** (owner, Q5): by the plaintext `Since`'s rules, resolved once,
    straight after discovery, from what is on disk before this run writes anything, and kept in
    the skip warnings in memory, so the running, cancelled, killed and refused-after-discovery
    records carry it as they carry the plaintext one (above):
    1. the previous last-run record says `mailbox_gone` with a `mailbox_gone_since`: that time;
    2. plaintext rule 2 does not apply: an encrypted record keeps no error text and its streak no
       kind (`writeRefusedRun`, `run.go:1448-1450`; `app/sourceloss.go:91-93`), so no earlier
       refusal can be tied to a mailbox. Leaving it out makes a WARN last longer, never shorter,
       the direction accepted for `Since`;
    3. else this run's start.

    *Revision 3.1 (owner, Q6), before rule 1:* when this run skips more mailboxes than the previous
    record's `mailbox_gone_count` says were gone, a mailbox went since that record, so
    `mailbox_gone_since` restarts at this run's start and rule 1 does not carry the older time. A
    previous record with `mailbox_gone` and no count, or a count below 1 (a planted or damaged
    record), counts as 1, the least its `mailbox_gone` says; a count past what any run could hold
    (above 1,000,000) counts as unreadable, so as 1 too. A count that falls or stays the same keeps
    rule 1's time.

    The same clamps: a time later than this run's start becomes this run's start, and one before
    `state.MinPlausibleTime` becomes this run's start and is never carried. Counted the same way:
    the WARN shows while the record's `Started` minus `mailbox_gone_since` is under
    `state.MailboxGoneNoteAfter` (14 days), never compared with now; a time that is zero, before
    `MinPlausibleTime` or after `Started` counts as no time, so the WARN shows. After that it is a
    note, as a plaintext archive's is.
  - status and the Desktop's health view say it beside the program line (`health.go:560-571`), in
    fixed sentences whose only variable is the date the record already holds (the friction review
    owns the final words). The WARN, after a run that ended ok, `state.MailboxGoneText`: "a mailbox
    this archive backs up has not been found since DATE, so it was skipped and the others were
    backed up; click Run now in MailArchive Desktop to see which one". After any other run (fold
    check R7), `state.MailboxGoneShortText`: "a mailbox this archive backs up has not been found
    since DATE, so it was skipped; click Run now in MailArchive Desktop to see which one". The
    note (GREEN), `state.MailboxGoneNoteText`: "a mailbox this archive backed up has not been found
    since DATE; its mail already saved stays in the archive. Click Run now in MailArchive Desktop
    to see which one. This line stays while the archive's backup lists that mailbox; to end it, set
    the archive up again in the wizard, with the same archive folder, choose All of them, and
    answer Yes to keeping it current." That the wizard's set-up-again rewrites an encrypted
    archive's job file as it does a plaintext one is recalled, not read; the friction walk
    checks it.
    *Revision 4 (the friction review's F1):* the walk found it does not: the wizard refuses an
    encrypted archive's folder without its key, and Run now is greyed out while it is locked. The
    three sentences now say "unlock it on its Encryption card in MailArchive Desktop, then click Run
    now to see which one", and the note ends "This line stays while the archive's backup still lists
    that mailbox." with no wizard sentence. Ending it is §5's "Stop looking for it" button.
  - **What one date cannot tell apart (open item O5).** The record keeps one time for the whole
    archive. When a second mailbox goes while the first is still gone, rule 1 carries the first
    one's time, so once that is 14 days old the second loss shows only as the note, where a
    plaintext archive WARNs for it for 14 days of its own. The note still says a mailbox is gone
    and how to see which. Telling the two apart needs more than a date beside the archive, which
    Q5's wording does not allow; this design does not add it.
    *Revision 3.1 (owner, Q6): settled by the count.* A second loss while the first is still gone
    raises the count, so the date restarts and the second loss WARNs for 14 days of its own, as a
    plaintext archive's does (the whole archive's date restarts, so the first loss's note becomes a
    WARN again for those days; the WARN names no mailbox either way). What one count cannot tell
    apart: one mailbox coming back in the same run that another goes (the count stays), or the run
    that finds the second gone being one whose record is not kept (a refusal before discovery). The
    first carries the older date, so the new loss may show only as the note, which still says a
    mailbox is gone and how to see which; the second restarts the date at a later run, the
    direction accepted for `Since`.
  - The attended surfaces name the mailbox, in memory only. Revision 1 left Desktop's Run now as a
    condition; it is settled. The wizard's summary and the Desktop's Run now note for an encrypted
    archive both come from `encryptedSummaryFor` (`jobrun.go:279`, `:298-321`), which already
    carries lines held only in memory: the Thunderbird lines, "from the run's result in memory only
    (O1): the Desktop clears the whole note at Lock or a switch (C-15), and nothing of it is written
    beside the archive" (`jobrun.go:123-129`, `:291-297`; that "O1" is the completeness design's,
    not this note's). The skip lines join them the same way.
  - Revision 2's "the encrypted WARN does not turn into a note after 14 days" (its open item O1) is
    replaced by the owner's Q5 above.

### 1.4 The Desktop's schedule rebuild

The rebuild runs only for a Settings create (no job file yet) and for the command-line-schedule
conversion check (§1.1). In `reconstructLocalJob` (`localschedule.go:77-109`), for a job with
`-auto`, a reconstructed `-input` that `lstatSource` (`localschedule.go:113`) says does not exist
and that `autoplace.GoneSkippable` accepts (§1.2 (a) to (c)) is **kept** in the rebuilt job, not
dropped. It must still be one of the archive's own recorded stores: `ids[autoplace.GoneSourceID(in)]`
against `manifestStoreIDs` (`localschedule.go:54`; adversarial finding A2). The run-time rule then
skips it and warns on every run (§1.3).

- Every other check stays for every input that exists: not a link, not a special file, not under
  the archive's `_outlook-pst`, and one of the archive's recorded stores (A2).
- The rebuild refuses, as today, when no listed input exists. Dropping gone inputs, as revision 1
  had it, could rebuild a pure `-auto` job whose runs then fail as "no input mail sources found"
  (`run.go:632-633`), a failure that leaves the streak (`app/sourceloss.go:49-66`): no 3-day warning
  and no 30-day pause. Keeping the inputs, and refusing when none exists, keeps the streak working
  and keeps every gone mailbox warned.
- It also refuses, as today, for a gone input that `autoplace.GoneSkippable` does not accept
  (outside the place, a link, a gone group folder) and for any gone input of a job without
  `-auto`.
- The two stale comments (`localschedule.go:74-75`, `localarchive.go:280-283`) are corrected: since
  `bc6e9d0`, the wizard's `-auto` jobs list their inputs.
- *Revision 4.3 (the re-verify, round 3's RV3-03):* `autoplace.GoneSkippable` is asked with the
  account names the archive's record keeps (`GoneSkippableAs` with `RecordedName`), as the run asks
  it, so the rebuild keeps what the run skips and refuses what it stops for.

### 1.5 The command line

- **One place reports the skips:** `app.Run`'s `Result.Skipped`, through `printSummaryFor` (§1.3).
  The up-front discovery (`main.go:275`) reports none of its own, so the terminal never prints two
  lists that could disagree if a folder goes between the two discoveries.
- **`-list`** (`main.go:280-282`, `listStores`, `main.go:555`) prints the stores found, then one
  "Not found, so skipped: PATH" line per skipped input. *Revision 4 (the friction review's F14):*
  "Not found, would be skipped: PATH", since `-list` is a preview and nothing runs.
- **`-follow-links`** (`main.go:276-279`): an opt-in is checked against the inputs found plus the
  skipped ones. So an opt-in that names a skipped input is no longer refused with words that do not
  fit ("is not one of the inputs being archived", `FollowLinksRefusal`, `run.go:240-254`) and a
  kind that leaves the streak. It reads nothing this run, since the input is not read.
  Command-line schedules keep their `-follow-links` (`cmd/mailarchive/job.go:295-297`), so this
  matters on every scheduled run. The schedule's own check when it is made (`job.go:266`, against
  the listed inputs) is unchanged. The review's words were "the listed inputs minus the skipped
  ones"; this design checks "found plus skipped" so that an opt-in for a store found only by
  auto-discovery, which works today, is not newly refused (open item O3).
- `export -auto -input X`, with X skipped under §1.2, now exits 0 where it refused; the release
  notes say so (§4).
- *Revision 4.3 (the re-verify, round 3's RV3-03):* `-enable-offline` and `-sync-wait`'s discovery
  (`prepareThunderbird`) is given the archive folder and asks with its record's names
  (`app.DiscoverInputsFor` with `app.RecordedNamesAt`), as the run and the up-front check do.

### 1.6 Rows

- **MA-103, amended** (owner, 2026-11-19; nothing weakened for a plain `-input`): "… a missing
  `-input` path is refused …, except with `-auto`, where a listed input that is gone (`Lstat`),
  lies where auto-discovery looks, and whose parent folder is still there as a folder in that place
  is skipped, and warned, while another mailbox remains (MA-1471)".
- **MA-688, amended:** the typed error is returned for a gone input §1.2 does not skip, and when
  nothing remains. A skipped input is not an error, so its run reaches the source and resets the
  streak (owner, Q4).
- **MA-734, amended** (owner, 2026-11-20, Q3 and Q5; nothing else widened): the encrypted
  record's allow-list is "version, status, started, finished, error kind, program, the encrypted
  flag, `mailbox_gone` (a boolean, written only when true: a mailbox this archive backs up was not
  found) and, only beside it, `mailbox_gone_since` (the time a mailbox was first found gone: a time
  only)". The existing key check (`internal/state/encrypted_record_test.go:71`) gains those two
  keys; MA-1478 proves the record names no path, account name or count.
  *Amended again by revision 3.1 (owner, 2026-11-20, Q6):* the allow-list also holds, only beside
  `mailbox_gone`, `mailbox_gone_count` (how many mailboxes the run skipped: a number only); the key
  check gains it; MA-1478 proves the record names no path or account name, and that its only number
  about mailboxes is that count.
- **A6 (`docs/design-encrypted-archives.md`) and phase 2a's allow-list line, amended** (owner,
  2026-11-20, Q3 and Q5; fold check R6). A6 (`design-encrypted-archives.md:410-416`) lists what
  an encrypted archive's cleartext status files carry; it gains "and, in the last-run record
  only, whether the last run skipped a gone mailbox and the time one was first found gone: no
  path, name or count". Phase 2a's allow-list line for lastrun, runhistory, sourcestreak,
  lastverify and the attention files (`design-encrypted-archives-phase2a.md:145`) gains "; in
  lastrun only, `mailbox_gone` and `mailbox_gone_since` (owner, Q3 and Q5)". Both amendments are
  written beside the original text, with the owner's rulings named; the original is not rewritten.
  The run history and the attention notice are unchanged. *Revision 3.1 (owner, Q6):* A6's line
  also says "and how many mailboxes it skipped, a number only", and phase 2a's says "`mailbox_gone`,
  `mailbox_gone_since` and `mailbox_gone_count` (owner, Q3, Q5 and Q6)", each written beside the
  revision 3 amendment.
- **MA-692, amended** (fold check R1; nothing weakened: today's words, and the test that pins them,
  stay for every archive the helper says no to): the Your-archives banner for a source-lost
  archive says Repair or Run now only when `health.GoneMailboxRepairable` says a run would now get
  past the gone mailbox (MA-1479).
- **MA-693, amended** (fold check R1; nothing weakened, as MA-692): the same for status's
  `source_lost` WARN, through the same helper.
- **MA-949, amended** (fold check R1; nothing weakened): the banner's Run-now-only form and its
  "click Switch to this archive" form for a row that is not current stay as pinned
  (`ma949_manual_backup_test.go:551-553`) for every archive the helper says no to; for one it says
  yes to, the new words keep both forms (§1.3).
- **MA-1471** (new): the rule in §1.2, through `DiscoverInputs`, for `app.Run`, Run now, the
  scheduled run and the command line. Positive twin first: an "All of them" archive with one
  account folder gone and its `ImapMail` there is skipped while another remains, including the
  Local Folders case of Q4. Each refusal through `assure.Refused` (the typed error, the input
  named, nothing read, the streak moved): a dangling link; a gone group folder (a whole profile);
  a profile reached through a link below the place's root; an input outside the places; on macOS
  and Linux an input spelled in another case; nothing remaining. It also covers the run log's line
  and the four summaries' lines from `Result.Skipped`, the plaintext Run now note among them.
- **MA-1472** (new; the 14 days are the owner's, 2026-11-20, Q2): the plaintext record's
  `mailbox_gone` warning (`Path`, `Name`, `Since`), cleaned at `ReadLastRun`; a record without it
  written byte for byte as before (`Since` is `omitzero`); `Since` resolved by the three rules,
  clamped to this run's start, never before `MinPlausibleTime`; kept by a refused run after
  discovery, a cancelled run and a killed run's running record; health's WARN while the last run's
  start minus `Since` is under 14 days (owner, 2026-11-20, Q2) and a note after, never compared
  with now (a record whose last run is 15 days after `Since` but read "today" a year later is a
  note, one 13 days after is a WARN whatever today is); "the other mailboxes are still backed up"
  only when the record's status is `ok` (positive twin first: an ok record says it; a cancelled,
  a failed and a running record do not); the note's remedy names the Yes to keeping it current;
  the same in the Desktop's health view.
- **MA-1473** (new): the rebuild keeps a gone input that `autoplace.GoneSkippable` accepts, under
  `-auto` only and only when it is a recorded store by `autoplace.GoneSourceID`; refuses when no
  listed input exists; keeps A2's checks for every input that exists. Run for a Settings create
  and for the conversion check.
- **MA-1478** (new): encrypted archives (owner, Q3 and Q5): the skip applies; the record carries
  `mailbox_gone: true` and `mailbox_gone_since` and nothing else new (its bytes hold no path, no
  account name, no count); `mailbox_gone_since` resolved by rules 1 and 3, clamped to this run's
  start, never before `MinPlausibleTime`, kept by a refused run after discovery, a cancelled run
  and a killed run's running record; status and the health view WARN in the fixed sentence while
  the record's `Started` minus that time is under 14 days and say the note after, never compared
  with now (the same 13- and 15-day cases as MA-1472); "the others were backed up" only after a run
  that ended ok; the wizard's summary and the Run now note name the mailbox from the result in
  memory, and nothing of it is written beside the archive. *Amended by revision 3.1 (owner, Q6):*
  the record also carries `mailbox_gone_count`, and its bytes hold no path and no account name; a
  run that skips more mailboxes than the previous record's count restarts `mailbox_gone_since` at
  its own start (positive twin first: the same count, or a lower one, carries the earlier time), and
  a previous record with no count, or one below 1, counts as 1.
- **MA-1479** (new): the words for a source-lost or auto-paused archive in status, the
  Your-archives banner and the Settings card, all from `health.GoneMailboxRepairable`
  (`internal/health`, one truth, X10). Positive twin first: a plaintext record with `-auto`, failed
  with the refusal of a listed input that `autoplace.GoneSkippable` accepts now, another listed
  input there, and streak kind `local-source-unreachable`, says Repair or Run now, on the banner in
  all four of MA-949's forms (scheduled or Run now only, current or not). Each of these keeps
  today's words exactly: the refused input now outside the places, a link, or with its group
  folder gone; no listed input there; the record's error naming no listed input; a record without
  `-auto`; another streak kind; an encrypted archive. Status and the banner give the same answer
  for every case.
- **MA-1480** (new): the command line: one report of the skips (from `Result.Skipped`); `-list`
  prints them; a `-follow-links` opt-in naming a skipped input is not refused, while one naming
  nothing found or skipped still is (positive twin first).

## 2. An empty answer to "Keep this archive current?" installs nothing

- `offerSchedule` (`wizard.go:2111-2134`) installs a daily backup for any answer except the exact
  "No — just this once". On Linux, zenity can answer a list with nothing selected: it exits 0 with
  empty text (`third_party/zenity/list_unix.go:7-27`, `util.go:93-108`), and no row is selected to
  begin with, since `list_unix.go` passes no default row. So OK with nothing chosen is a natural
  click there. MA-1465 already handles that case for the mailbox list. This is read in the code and
  not run.
- **Change:** only an answer that is the daily row or the weekly row installs. The question is
  asked through the `listDialog` seam (`internal/wizard/listtext.go:13`) with
  `zenity.DisallowEmpty()`, as the mailbox list is (`picker.go:81`; Windows and macOS keep OK off
  with nothing selected).
- **Which row an answer is: the mailbox list's rule, not exact text** (fold check R5). On Linux and
  the BSDs the dialog program may print the answer in plain ASCII, with "?" for each character
  outside it (`answerMayBeTranscoded` and `asciiForm`, `picker.go:758-780`; the adversarial pass
  round 2's R2-02). All three rows hold "—", so "Yes — every day at 2:00 AM" can come back as
  "Yes ? every day at 2:00 AM". With exact text, that person would be told "Nothing was chosen" ten
  times and get no schedule. (Today it is worse: a mangled "No — just this once" is not the exact
  "No" row, so it installs a daily backup, `wizard.go:2128-2134`.) So the three rows are matched
  by `pickChoice`'s rule (`picker.go:709-756`): on those systems, an answer holding "?" is the row
  whose `asciiForm` it equals, only when exactly one row fits (two that fit give no row, never a
  guess); else the row with exactly that text; else the one row whose whole text is the same after
  NFKC and case folding (`state.TBTextKey`). The row-matching part of `pickChoice` becomes one
  function both lists call, `pickChoice` keeping its All row on top, so the two lists cannot match
  differently. The three rows' ASCII forms all differ, so a mangled answer is always read as its
  own row.
- Any answer that is not one of the three rows by that rule, the empty one included, shows
  MA-1465's note ("Nothing was chosen. Choose one line in the list, then click OK.", `pickAgain`,
  `picker.go:38`) and asks the question again, as the mailbox list does (`picker.go:80-98`), up to
  `pickTries` (10) times. After the tenth
  it ends with a line and installs nothing: "No scheduled backup was set up: the list did not say
  which line was chosen. To set one up, open Settings in MailArchive Desktop, or run the wizard
  again." (the friction review owns the words). Cancel and "No — just this once" install nothing,
  as today (R14: schedules are opt-in).
- The wizard's other lists already fall to their safe default on an empty answer: export mode goes
  to Incremental (`wizard.go:903`), mail app open goes to No (`:925`), keep `.eml` goes to No
  (`:939`), follow links goes to none (`followLinksFor`, `:1090-1095`), and the Thunderbird sync
  goes to No (`:2147`). The build re-checks the full list.
- **MA-1474** (new): positive twin first (the daily row installs). An empty answer, and any text
  that is not a row, show the note and the question again; ten of them end with the line and no
  schedule; Cancel and No install nothing. A mangled answer (fold check R5), on Linux and the BSDs:
  "Yes ? every day at 2:00 AM" installs the daily schedule and "Yes ? every week, Sunday at
  3:00 AM" the weekly one, while "No ? just this once" installs nothing; on Windows and macOS,
  where answers are not transcoded, "Yes ? every day at 2:00 AM" is not a row and shows the note.
  Through the `listDialog` seam, with `assure.Refused` (no scheduler entry, no job file).

## 3. Delete waits for you

### 3.1 A plan waits until you are done with it

Today a delete plan is held for 10 minutes from when it is ready (`pendingLife`,
`internal/desktop/pending.go:24`, `:161`; MA-972). A large archive's check can take minutes. A
person who steps away comes back to "That delete is no longer waiting — click Delete… again;
nothing was deleted.", and then waits through the check again.

**Change:** a delete plan waits up to 24 hours (a new `deletePlanLife`) while a panel shows it.
Every other kind of held item keeps `pendingLife`'s 10 minutes: a new or changed key, a recovery
key to put back, a forget, an unencrypted copy, a restore to Thunderbird, and a new encrypted
archive. Several of those hold keys. The store's single `life` field (`pending.go:131-140`, used by
`put`'s timer at `:161`) becomes one life per kind: `put` takes the delete plans' field for a delete
plan and the other field for every other kind, each set from its constant. So MA-972's expiry check
(`ma972_delete_token_test.go:73-76`) keeps its short override, now on the delete plans' field, and
still proves that a delete plan expires and is refused "no longer waiting".

A delete plan ends at once:

- when it is used, when a new plan of the same row replaces it (`deletePlanKey`,
  `internal/desktop/archivedelete.go:75-78`; `put`, `pending.go:154-160`), when the current archive
  is switched (by its own kind), and at Shut down, as today;
- **new:** when the person clicks Cancel on its panel, or another Delete… closes its panel. The page
  posts its token to a new guarded route, `/api/archive/delete/discard {token}`, which calls
  `pending.discard(token, isDeletePlan)` (`pending.go:211-217`): only a delete plan's token is
  taken, and any other kind's is left where it is (del-s3a-A4-02);
- **new:** when a check's reply reaches a panel that was cancelled or closed during the check: the
  page opens nothing and posts that reply's token to the same route;
- **new, best effort:** when the page goes away (a reload, a closed tab), it posts the discard of
  every delete plan it holds, from a `pagehide` handler, as a keepalive request through the same
  guarded route.

So the 24 hours apply only to a plan a panel still shows. A plan whose page went away without
saying so (a browser that was killed, a lost connection, a computer that slept and was shut down)
waits out its 24 hours. That is bounded: at most one plan per row, since a new plan of the row
replaces it, and every plan ends at a switch and at Shut down. Two things here are recalled, not
read: that browsers send a keepalive request from `pagehide`, and whether Go's timers count a
computer's sleep. If either does not hold, a plan is held longer; it can never delete more (below).

**A backup in between refuses the plan.** The re-check under the lock compares the manifest's size,
time and SHA-256 and every name to remove (`samePlan`, `internal/archdelete/execute.go:372-377`).
Any backup that saves mail between the check and Delete changes those. So a plan held across the
archive's own scheduled backup is refused with "This archive changed since you clicked Delete.
Click Delete again.", and its token is spent; with an hourly schedule a held plan in practice lasts
under an hour. The refusal is found only after the second walk (below), and writes its started and
refused lines to `deletions.log` and a held record, as today (MA-964, MA-984). The page then asks for
the plan again by itself: the held result shows as today, and the row's panel opens again with
"Checking what Delete would remove…" and the typed name put back, so the person needs one more
click when the new plan shows. The delete's reply marks this case (`changed: true`, from
`archdelete.CaseChanged`), so the page asks again only then.
*Revision 4 (the friction review's F5; the owner's Q9: "Re-check, then delete (Recommended)"):*
under the held result the page says "A backup ran after the check, so MailArchive is checking again
below. Nothing was deleted; your typed name is kept."; and when the refused delete was armed, the
plan is asked for again armed, once: its delete is sent armed with the new plan's token and the
panel's row, which the server checks as any armed delete; a re-armed delete refused as changed
again asks once more un-armed, so it cannot loop. Whether a bookkeeping-only backup should leave a
plan alone, or a backup skip an archive whose delete plan waits, stays the owner's deeper choice
(F5), not built.

*Revision 4 (OUT-05 (b), CON-06; the owner's Q10: "Yes, Lock ends it (Recommended)"):* a plan
made while an encrypted archive was open holds values read with its key, so any lock of that
archive (Lock, the idle lock, a switch's, forget-all) also ends its waiting delete plan
(`pendingSecrets.discardFor` matches `deletePlanKeyOf(key)`); its delete is then refused "no longer
waiting". A delete's own lock finds its plan already taken. Matched by the archive's key spelling, so
a row listed under another spelling of the same folder (a link) keeps its plan until its 24 hours, a
switch or Shut down.
*Revision 4.1 (RV-06):* that limit is closed: any lock ends every encrypted archive's waiting delete
plan, whatever spelling; a plain archive's plan is kept. *(RV-03)* The "changed" reply also says
whether the plan's folder is still the same folder on disk (its recorded file identity), and the page
re-arms only then, so a folder replaced by another of the same name is never deleted unseen. *(RF-02)*
Its words now differ for a re-armed and an un-armed re-check ("No need to click Delete again. …"),
and an earlier result says when a later delete of the folder ended.
*Revision 4.2 (RV2-02):* RV-06 narrowed: a lock ends only an encrypted plan that read values with
its key open (`Captured.KeyOpen`); a plan made while its archive was locked waits through another
archive's idle lock. The guide and the README say "A plan waits up to a day (for an encrypted
archive, until it is locked), …".
*Revision 4.3 (RF3-02):* those words now say "A plan waits up to a day, but a backup in the meantime
makes MailArchive check again. For an encrypted archive that was unlocked when it was checked,
locking any archive ends the plan.", since any archive's lock ends such a plan, not only its own.

**Every delete walks the folder twice.** At Delete, the plan is made again in full under the lock
(`execute.go:340`): the same walk, and the same SHA-256 of the manifest, as the check. So a large
archive's delete pays for the check twice, and **Delete when the check finishes** pays twice back
to back. This change does not add the second walk; the second walk is what makes the delete safe
(next paragraph). The panel's words say it (§3.2), and the lab sizes its archive for it (§4).

**Why holding a plan longer is safe**, restated from `samePlan` (`execute.go:372-377`). Under the
archive lock the plan is made again (`execute.go:340`), and it must equal the held one in: the
folder's identity, the kind, the re-run state, this computer's record of an encrypted archive, the
gone-note, the anchor file (the manifest or the vault: its name, size, time and SHA-256), every
name to remove, every unrecorded copy's name, the schedules to stop, and those it would not remove.
Any difference is refused and removes nothing. It does not compare the synced line, the paused
flag, the kept files, files another program holds, the litter, the folders or the captured values.
Delete acts only on what the held plan names (`internal/archdelete/archdelete.go:148`). So the
claim is narrower than revision 1's "can never delete more": **Delete never removes a name the plan
did not list, however long the plan was held** (P12). A plan kept longer can only be refused more
often. The token stays one-use and kind-filtered, behind the page's CSRF token and the typed name,
which the server checks (`archivedelete.go:579`).

**The deletion record.** Revision 1 left open whether the record says when the check was made. It
does not: it says the delete's own time (`internal/archdelete/record.go:120`), and the plan carries
no time (`archdelete.go:149`). No line is added: every name it lists was checked again under the
lock at that time (above), so a record of a delete made hours after its first check does not
mislead about what was removed.

- **MA-972, amended:** "… a one-use, kind-filtered token … gone after 24 hours for a delete plan
  (ten minutes for every other kind), at once when its panel's Cancel, a panel closed for another,
  a check's reply to a cancelled panel, or the page going away posts its discard (MA-971) …". The
  expiry check still runs, on the delete plans' own life field; nothing weakened.
- **MA-971, amended:** five delete routes, not four: `/api/archive/delete/discard {token}` is
  registered through `d.api` like the others (POST only, `GuardLocalPOST`, the page's epoch, the
  64 KiB cap, `no-store` and `nosniff`). It takes only a delete plan's token, answers ok whether or
  not the plan was still waiting, and changes nothing else. `/api/archive/delete` gains `armed` and
  `row` (MA-1476). Nothing weakened: the four routes' guards are unchanged and the fifth has the
  same ones.
- **MA-1475** (new): a delete plan's life, and every other kind's (each from its own field);
  the discard route ends a delete plan and leaves any other kind's token waiting; a plan held across
  a backup that saved mail is refused "This archive changed since you clicked Delete. Click Delete
  again.", removes nothing, and its reply carries `changed: true`. Positive twin first: a plan
  held past ten minutes (by the field) and unchanged deletes.

### 3.2 Delete without waiting

The owner asked for a way not to wait for the check. The check itself stays: it is what tells the
archive's own files from the person's, finds the schedules to stop, and notes a synced folder.
Only the waiting goes, by the owner's ruling of 2026-11-20 (§0, Q1).

**What the person sees.** Clicking Delete… on a row opens the panel at once, before the check
ends:

- "Delete this archive permanently"
- "Checking what Delete would remove… This can take a few minutes for a large archive. Nothing is
  deleted yet." While the check runs, the minutes so far are shown.
- "Folder: PATH"
- "Type the folder's name, NAME, to delete it (capital letters count)", with the text box ready.
- **Delete when the check finishes** (danger style; enabled once the typed name matches the row's
  name, below) and **Cancel**.

Clicking **Delete when the check finishes** changes the panel's line to: "When the check finishes,
MailArchive checks once more, then deletes this archive, unless the check finds something you
should see first. Keep this page open." The minutes keep counting through both checks. When the
check ends:

- **If the plan is armable** (below) and the typed name matches the plan's name by
  `archdelete.NameMatches`, the page sends the delete at once with `armed: true` and the panel's
  row. The server decides whether it runs (below). The page shows the result and **Save deletion
  record** as today (MA-984).
- **Otherwise** the full plan is shown as today, with a first line: "The check found something to
  look at before deleting. Read it, then click Delete permanently." When only the name did not
  match, the plan is shown with the box's "doesn't match yet" hint. The typed name is kept. Nothing
  is deleted until **Delete permanently** is clicked.

If the person does not click it, the panel fills with the plan when the check ends, as today, with
the typed name kept.

**Once the delete is sent, it cannot be stopped, and the panel says so** (fold check R2). The delete
is sent when an armed panel's check ends with an armable plan and a matching name, or when
**Delete permanently** is clicked. From then on the server runs it to its end: `runDelete` is not
tied to the request (`archivedelete.go:614`), so closing the page does not stop it, and the second
walk under the lock can take minutes. Today Cancel stays live in that time: `busy()` disables only
the Delete permanently button (`dashboard.go:2434-2441`), and Cancel removes the panel and says
"Nothing was deleted." (`deletescript.go:251`), which may be untrue. That is in the shipped page
already; the armed flow makes the long wait the normal case. So, in both flows, when the delete is
sent the panel removes its **Cancel** button, and its line says (the friction review owns the
final words): "Deleting this archive. It can no longer be stopped. Keep this page open until its
result shows here…" (today's words, `deletescript.go:253`, with the middle sentence added). In the
armed flow the minutes keep counting. When the reply says the delete was refused before it began (no result to
show, `deletescript.go:261`), nothing was deleted, and Cancel comes back with the refusal's words.
When the page loses its connection while it waits (`deletescript.go:262`), Cancel does not come
back: the delete may still be running, and its result shows when the page is drawn again
(MA-986).

**The name, by one rule.** The row's Delete… button gains `data-delete-name`: the last element of
the row's path, as the page is drawn. No folder is read to draw the page, so an unplugged drive
cannot slow it. That name only enables the button early. What decides is one rule,
`NameMatches(typed, the plan's name)` (`execute.go:33-39`): on the page when the check ends (the
page's `delNameMatches` mirrors it), and again on the server (`archivedelete.go:579`). Revision 1's
second rule ("does not fire when the names differ in letter case or form") is dropped:
`NameMatches` already folds Unicode form and never folds case, so a name that differs only in form
fires and one that differs in case does not, by the one rule.

**Armable, computed and enforced on the server.** `armable(plan)` is one function in
`internal/desktop`, beside `planView` (`archivedelete.go:438-505`). It is true only when none of
these is in the plan:

- mail copies the archive did not record (the "Also delete these files" box needs an answer);
- a synced-folder line;
- "Not removed, check it yourself" schedules, or the unconfirmed and other-computer schedule notes;
- files another program holds (`heldLine`);
- the finishing of an earlier delete that stopped part way (`finishesLine`, `finishesNotes`,
  `rerun`);
- an encrypted archive (its key line).

Files that are not the archive's (`kept`) and a paused archive do not stop it: Delete keeps such
files, and the result names them. The field is named `armable`, not `plain`: `planView` already
sends `kind: "plain"`, meaning unencrypted (`archivedelete.go:469`;
`internal/archdelete/archdelete.go:36-46`).

`planView` sends `armable: armable(plan)`. The delete route (`deleteAction`,
`archivedelete.go:563-640`) gains two fields: `armed` (true only from **Delete when the check
finishes**) and `row` (the row the panel was opened for: the panel's own Delete… button's
`data-path`, kept when the panel opened, never a value taken from the reply). After the token and
the typed name are checked (`archivedelete.go:573-582`) and before anything begins (`tryBegin`,
`:586`), an armed request is refused, **keeping the token and deleting nothing** (no started line,
no lock, no row changed), unless both hold:

- the row it names, matched by the same `d.cfg.matchRow` the plan route used
  (`archivedelete.go:114-117`, `:384`), is the held plan's own row (`heldPlan.row`, `:68-71`); and
- `armable` of the held plan, the same function `planView` used, is true.

The refusal words (the friction review owns the final words): "The check found something to look at
before deleting, so nothing was deleted. Read the plan, then click Delete permanently."; for the
other row: "This delete was asked from another row's panel, so nothing was deleted. Click Delete…
on the archive's own row." The page then shows the plan. So a slip in the page script, a stale
cached page, or a check's reply routed to the wrong panel (two archives whose folders share a name)
can at most show a refusal. It cannot delete, unseen, a plan that needed a decision, or another
row's archive. **Delete permanently** sends no `armed`, so it is checked exactly as today. MA-1476
proves both refusals in Go with `assure.Refused`.

*Revision 4, the panel and the route as built after the reviews:*

- **The words (the friction review's F7).** The checking line adds "You can type the folder's name
  below meanwhile."; the armed line is "When the check finishes, MailArchive deletes this archive,
  unless the check finds something you should decide first. Just before deleting it checks the
  folder once more, which takes about as long again. Keep this page open; until the delete starts,
  Cancel or closing this page deletes nothing."; an armed check that finds something says "Nothing
  was deleted: the plan holds something to decide first (REASONS). Read it, then click Delete
  permanently.", REASONS from a closed list the server sends as `decide` (`decideReasons`, from
  which `armable` is now made); one whose only miss is the typed name says so. Kept: the deleting
  line, the tooltip, the other-row refusal.
- **An encrypted archive (F8).** Its Delete… carries `data-delete-encrypted`; its panel offers no
  Delete when the check finishes and says "An encrypted archive's plan is always shown first,
  because it names its key."
- **A lost connection during the check (F13)** says "The page lost touch with MailArchive during the
  check, so nothing was deleted. Click Delete… again."
- **A row removed from the list during an armed check (OUT-02)** is refused "This archive was removed
  from Your archives while its check ran, so nothing was deleted.", not as another row's panel.
- **A sent delete's token (OUT-03)** leaves the page's `delTokens` when it is sent, so `pagehide`
  cannot discard a delete the panel says can no longer be stopped; a refusal that kept it gives it
  back.
- **OUT-04's guard (refuted, kept):** the page holds exactly two posts to the delete route, the armed
  one (`armed:true`, the panel's row) sent only from the plan's reply, and Delete permanently's sent
  only by its click; a structural test says so.
- **The guide, the This archive card, the README and X14 (F6)** describe the panel as built.

**MA-1476's key check.** `armable` must not drift from `planView` as lines are added. The test lists
every key the plan route's reply carries (`planView`'s, and the route's own: `ok`, `token`, `row`,
`logLine`, `planText`, `planName`, `armable`) and classifies each as armable-blocking or
armable-neutral. A plan with any blocking key set is not armable, one with only neutral keys set
is, and the test fails on any key it has not classified. So a later line that needs a decision
cannot leave `armable` true without the test saying so.

**Why the armed request stays on the page, not on the server.** The review suggested, as better
still, that the server hold the typed name with the plan request and delete when the check ends,
so that a reload or a sleep would not lose it. It is not chosen, for two reasons. (1) With the
page-held form, closing the page or losing its connection during the check deletes nothing: that is
the person's way to stop it, and a server-held request would delete with no page open to show the
result or to be stopped. (2) It needs a held item for a plan not yet made, a second kind of
waiting the store does not have. What the page-held form loses on a reload or a sleep is one more
click and one more check, never a wrong delete, and with the server's refusal above it fails
closed.

**What it never does:**

- It never deletes on a plan the person did not type the name for: the server checks it.
- It never deletes on a plan that is not armable, or for another row: the server refuses it.
- It never deletes on a refusal, or on the "finished elsewhere" question (MA-1057): those are shown
  as today.
- Until the delete is sent, it never deletes after Cancel. Cancel during the check closes the
  panel; the check's reply, when it comes, opens nothing, and its plan is discarded (§3.1). Once
  the delete is sent there is no Cancel to click (above).
- A reload the page starts itself is held while an armed panel waits for its check (as for a
  delete waiting for its reply, MA-986). The armed panel is marked `data-delete-waiting` from the
  click, so another row's Delete… does not close it, and its check's reply fills its own panel, never
  another (today a later click drops an earlier reply, `delSeq`, `deletescript.go:283-289`).
  An un-armed panel behaves as today: another Delete… closes it, and its reply opens nothing and
  discards its plan (MA-986, amended).
- When the page is closed or loses its connection before the delete is sent (during the check
  included), nothing is deleted. Once it is sent, the delete runs to its end on the server, and
  its result is held for the page (MA-984, MA-986).

**R36** ("An archive is deleted only by the person's explicit decision … for one named archive,
which the person starts and confirms at that moment"): the person starts it (Delete… on one row),
types its name and clicks **Delete when the check finishes**. The delete follows the check in the
same open page, and the owner accepted that on 2026-11-20 (§0, Q1). Nothing handed to the program
and no unattended run can start it, and, until the delete is sent, closing the page ends it. Once
the delete is sent it runs to its end, as today's Delete permanently does, and the panel says so.

**Look before deleting (X14), amended by the owner's word.** The Delete… button's tooltip
(`dashboard.go:2396`) says "You see what would be removed first", which an armed delete makes
untrue. It becomes (the friction review owns the final words): "Permanently delete this archive's
files. MailArchive first checks what would be removed, and you type the folder's name to confirm.
You see that list before anything is deleted, unless you choose Delete when the check finishes and
nothing in the list needs your decision."

- **X14, amended** (owner, 2026-11-20, question 1: "Yes (Recommended)"): "Delete… in Your archives
  may delete without first showing its plan, only when the person typed the folder's name and
  clicked Delete when the check finishes, and only when the plan is armable: nothing in it needs a
  decision (no unrecorded copies, synced folder, schedules to check by hand, files another program
  holds, unfinished earlier delete or encrypted key), computed on the server by the same function
  the panel's reply uses and refused by the delete route otherwise. The full check still runs, the
  plan is made again and compared under the archive's lock before anything is removed, and the
  deletion record says everything removed and kept. Every other dashboard action keeps X14 as
  written."
- **MA-986, amended** (nothing weakened: one panel opens per click, and every other panel still
  closes as today): an armed panel is marked `data-delete-waiting` from the click until its
  delete's reply, so another row's Delete… or Remove from list leaves it, and its check's reply
  fills that panel, never one opened meanwhile; an un-armed panel closed for another posts its
  plan's discard, and its check's late reply opens nothing and posts the discard of the plan it
  names; reloads the page starts itself are held while an armed panel waits.
- **MA-1476** (new): the server's half. `armable` is false for each blocking line and true with only
  kept files and a paused archive; `planView` sends it as `armable`; the key check above.
  `deleteAction` refuses an armed request whose held plan is not armable, and one whose row is not
  the held plan's row, through `assure.Refused`: the words, nothing removed, no `deletions.log`
  line, the row still listed, the token still waiting. Positive twins first: an armable plan armed
  from its own row deletes; **Delete permanently** (no `armed`) of a plan that is not armable still
  deletes after its name is typed, as today.
- **MA-1477** (new): the page's half, run under Node against the page-script harness
  (`internal/desktop/testdata/pagescript/harness.js`), as MA-986's tests are. The panel opens at once
  with the checking line and its minutes; the name can be typed during the check; **Delete when the
  check finishes** is enabled by `data-delete-name` and changes the line to "checks once more, then
  deletes"; at the reply, the delete is sent with `armed: true` and the panel's own row only when
  the reply says armable and the name matches the plan's; otherwise the plan is shown with its first
  line and the typed name kept; a server refusal of an armed delete shows the plan and its words; an
  armed panel survives another Delete… and receives only its own reply; reloads are held while it
  waits; Cancel, a closed un-armed panel, a cancelled check's late reply and `pagehide` post the
  discard; a `changed` refusal shows the held result and asks for the plan again with the typed
  name. Once the delete is sent (fold check R2), armed or by **Delete permanently**, the panel has
  no Cancel and its line says the delete can no longer be stopped (positive twin first: before it
  is sent, Cancel is there and closes the panel with "Nothing was deleted."); a reply refusing the
  delete before it began gives Cancel back; a lost connection does not. The live walk is the
  friction review's and the lab's.

## 4. Reviews owed and the order of work

1. This note: revision 3. The fold check of revision 2 cleared it after its fixes, all of them
   design-text edits that need no lens re-run; the lead confirms them (`design-review.md`,
   procedure step 4). The owner's five answers are recorded (§0). O5 (§7) goes to the owner; it
   does not hold up the build, since the design takes Q5 as worded. *Revision 3.1:* the owner
   answered O5 (§0, Q6), and the build includes the count.
2. Build, test first. Every row gets a prove-fail → prove-pass by mutation, recorded in its commit.
3. After the build: the adversarial pass (one pass over all three parts) and the friction review
   (every new word: the WARN and note, the encrypted sentences and note, the Repair words in all
   their forms, MA-1474's repeat, the panel, the "can no longer be stopped" line, the refusals and
   the tooltip; and the waiting delete walked). The adversarial pass
   also carries the half of the review's rescue step that was not run: the search for a deeper
   weakness the lenses missed (review header).
4. Windows lab on the office test PC (its brief and self-test first; the builds in `C:\Tests`). A
   test Thunderbird profile made of folders only (no Thunderbird installed) in the test account's
   own AppData, with two IMAP account folders and Local Folders:
   - "All of them", then one account folder removed: Run now and a scheduled run skip it and say
     so (the log, the Run now note, status's WARN and its DATE). Then the profile's `ImapMail`
     folder removed: refused as today. Then the profile folder replaced by a junction to a folder
     elsewhere, with an account folder removed in it: what the rule does on Windows is written down
     (P1's condition), and with the junction's target removed it refuses.
     *Revision 4 (the adversarial pass's INS-04, left to this lab):* also an `ImapMail` folder that
     is a junction (below the root), with an account folder removed in it: MA-1471 says it refuses;
     if Go's `filepath.EvalSymlinks` does not follow the junction, the rule counts it in place and
     skips, and the lab writes down which. A moved account (Q7, MA-1481) is walked too: its Local
     Directory pointed at another folder, its old folder removed: the run refuses in its words.
   - Settings creating the schedule on an archive whose schedule was removed first (no job file),
     so it reaches the rebuild (on 1.7.4 it refuses; on 1.7.5 it saves, the gone input kept). An
     archive that still has its job file proves nothing here.
   - An archive auto-paused by 1.7.4 (its streak record set to span 30 days, then one 1.7.4
     scheduled run with the account folder gone pauses it), then 1.7.5 installed: status, the
     banner and the Settings card say Repair; Repair, then a run, skips and warns with DATE from
     the streak. Before Repair, the profile's `ImapMail` folder is moved away for a moment: all
     three go back to today's words (R1), and come back to Repair when it is put back.
   - An encrypted "All of them" archive with an account folder removed: a scheduled run succeeds,
     status WARNs in the fixed sentence with its date, Run now's note names the mailbox, and the
     record holds no path or name: only `mailbox_gone` and `mailbox_gone_since` beside the
     allow-list (*revision 3.1:* and `mailbox_gone_count`; a second account folder removed while
     the first is still gone raises it and restarts the date).
   - A Delete with **Delete when the check finishes** of an archive sized by one timed check first
     so that its check takes at least two minutes on the test PC (the file count and the minutes
     go in the lab's log); the minutes keep counting through both checks, and once the delete is
     sent the panel has no Cancel and says it can no longer be stopped. Then a second archive
     planned, a backup run into it, then **Delete permanently**: refused as changed, and the page
     asks for the plan again with the name kept.
   - Clean-up: the deleted archive confirmed gone (removed by hand if a step stopped), the second
     archive, the test profile folders and the junction, every schedule the lab installed, and the
     builds in `C:\Tests` removed; the lab's own log kept.

   A local macOS walk of the first, second and fifth items. Linux's empty answer cannot be run
   here, which is said; MA-1474 proves the wizard's handling through the seam.
5. **Release notes** (new step), saying:
   - an "All of them" archive keeps backing up when one mailbox's folder is gone, and status warns
     which one for 14 days of backups, then keeps a note (an encrypted archive's status says that
     one was skipped, not which; Run now names it);
   - the command line's `export -auto -input X`, with X gone in a place auto-discovery looks, now
     exits 0 and says it skipped X, where it refused;
   - an archive 1.7.4 stopped (auto-paused) because a mail account was removed: click Repair on
     its Scheduled backup card (an encrypted archive's words still say reconnect; Repair works);
   - delete plans wait up to 24 hours, a backup in between still refuses the plan, **Delete
     when the check finishes** is new, and once a delete is sent its panel says it can no longer
     be stopped and offers no Cancel;
   - going back to 1.7.4: its status shows nothing for a skipped mailbox and its runs refuse again;
     a 1.7.4 Desktop beside 1.7.5 scheduled runs, or the other way round, can show different
     signals until both are 1.7.5; an encrypted archive's record gains two fields, which an older
     MailArchive ignores (`ReadLastRun` is a plain `json.Unmarshal`, `lastrun.go:366`)
     (*revision 3.1:* three fields, the count among them).
6. CI on all three systems, then release 1.7.5 (owner, 2026-11-19: "make it so").

## 5. Not in this change

- Per-mailbox archives: the next release, with their own design.
- An email or system notice when a mailbox is first found gone; a "Stop looking for it" button.
- A first-found date per mailbox for an encrypted archive, so a second gone mailbox gets a WARN of
  its own (open item O5). The one date the owner allowed (Q5) is in this change.
  *Revision 3.1 (owner, Q6):* the count is in this change, so a second gone mailbox does get a WARN
  of its own (§1.3); a date per mailbox is still not.
- A delete armed and held on the server (§3.2 says why not).
- Making the delete check itself faster, or doing without the second walk.
- *Revision 4, a known limit (the adversarial pass's INS-05, LOW):* a disk mounted at a profile's
  `ImapMail` (or `Mail`) folder that is missing at boot leaves an empty mount-point folder, which
  rule (c) takes as the real parent, so every account folder on that disk is skipped as gone while
  Local Folders remains: the backups go on with a WARN, then a note, as for an account removed. Rule
  (c) has no device check; telling a mount point from a folder is not in this change. Rare (a
  second disk mounted inside a Thunderbird profile, with `nofail`).
- *Revision 4:* the "Stop looking for it" button (the friction review's F11 and F1) stays here; the
  note now names the button that ends a plaintext one (Make a new archive…), and an encrypted note
  says only how long it stays. Whether a backup that saves nothing should leave a waiting delete plan
  alone (F5's deeper choice) is the owner's, after its own adversarial pass.
- *Revision 4.3, a known limit (the re-verify, round 3's RV3-04, INFO):* an encrypted archive's
  record names no account (the owner's Q3, Q5 and Q6), so for it MailArchive goes by the mail
  server's name alone: with several accounts on one mail server, a moved account can be skipped
  instead of stopping the backup. Check status after moving one. (The same words are in MA-1481 and
  the README.)

## 6. Claims, each with its condition

- **P1.** With `-auto`, a gone listed input in a place auto-discovery looks, whose parent folder is
  still there as a folder in that place, is skipped while another mailbox remains. *Proven by
  MA-1471 once built. Conditional, on Windows only, on how `filepath.EvalSymlinks` treats a junction
  (recalled, not read: Go changed how it resolves Windows mount points). If it does not resolve
  one, a profile folder that is a junction counts as inside the place, so an account gone in it is
  skipped while the junction's target is there; with the target gone the parent cannot be read and
  the run refuses. The lab's junction case settles which.*
- **P2.** When the gone input is outside those places, reached through a link below the place's
  root, a link itself, or its group folder is gone; when nothing remains; or without `-auto`; the
  run is refused as today and the streak moves as today. *Proven by MA-1471 and MA-688.* The
  owner's ruled case (Q4): one account plus Local Folders, the account's folder gone and `ImapMail`
  there, keeps succeeding on Local Folders with a WARN and no email. *Proven by MA-1471's Local
  Folders case; conditional on Thunderbird keeping `ImapMail` when it removes an account (recalled,
  not read): if it removes it, that case refuses as today.*
- **P3.** A plaintext archive's skip is a WARN while the last run's start minus `Since` is under 14
  days, then a note, in status and the Desktop's health view alike, never measured against now. It
  says the other mailboxes are still backed up only when the last run ended ok, and the note's
  remedy names everything that ends it. *Proven by MA-1472.*
- **P4.** `Since` survives a cancelled run, a killed run's running record and a refusal after
  discovery; a clock that was ahead or not set never moves it earlier. Any other record without the
  warning (a one-off run without `-auto`, an unreadable record, a refusal before discovery, a
  downgrade) restarts it at a later date, never an earlier one. *Proven by MA-1472.*
  *Revision 4 (Q8):* nor does a run whose own clock is behind: a recorded date later than its start
  is kept as recorded. *Proven by MA-1472's and MA-1478's amended cases and
  `internal/app/ma1472_clock_behind_test.go`.*
- **P5.** An encrypted archive skips the same way; its record carries only `mailbox_gone: true` and
  `mailbox_gone_since`, a time (no path, name or count); status WARNs in a fixed sentence while the
  record's start minus that time is under 14 days, then says a fixed note, never measured against
  now, with "the others were backed up" only after a run that ended ok; the wizard's summary and Run
  now's note name the mailbox in memory only. *Proven by MA-1478 and MA-734 (amended).*
  *Conditional on one gone mailbox at a time (O5): a second one lost while the first is still gone
  carries the first one's time, so it may show only the note.*
  *Revision 3.1 (owner, Q6), replacing that condition:* the record also carries
  `mailbox_gone_count`, a number only, and a run that skips more mailboxes than the previous record
  says restarts the time, so a second loss WARNs for 14 days of its own. *Proven by MA-1478 and
  MA-734 (amended again). Conditional on the count rising: a mailbox that comes back in the same run
  another goes keeps the count, so that new loss carries the older time.*
- **P6.** A plaintext `-auto` archive that 1.7.4 auto-paused is told that Repair or Run now works
  only when it does: when the input its failed record names would be skipped now, and no other
  listed input would refuse the run. Every other archive keeps today's words, MA-949's
  Run-now-only and Switch-first forms included, and status, the banner and the Settings card
  decide it with one helper in `internal/health`. *Proven by MA-1479, with MA-692, MA-693 and
  MA-949 (amended) still pinning today's words.* *Conditional on the record's job and the run's
  error spelling the input the same way: the job records it made absolute (`absOrSame`,
  `run.go:1502`, `:1518`), the error as the job file lists it. The wizard's inputs come from
  auto-discovery's patterns, which are absolute; that every other job file lists absolute paths is
  not read here. Where the two differ, the helper says no and today's words stay.* An encrypted one
  is told only by the release notes.
- **P7.** A Settings create (no job file yet) and the command-line-schedule conversion can rebuild
  such an archive's job, keeping a gone input only when it is a recorded store and refusing when no
  listed input exists; A2's checks hold for every input that exists. *Proven by MA-1473.* An edit,
  Repair included, was never stopped (§1.1).
- **P8.** The command line reports the skips once, `-list` shows them, and a `-follow-links` opt-in
  for a skipped input is not refused. *Proven by MA-1480.*
- **P9.** An answer to the schedule question that is not the daily or weekly row, by the mailbox
  list's `pickChoice` rule, installs nothing, and an empty one asks again; a row that comes back in
  plain ASCII on Linux and the BSDs is still read as its own row, so a mangled No installs nothing.
  *Proven by MA-1474 for the wizard's handling of the answer; conditional on Linux zenity giving
  exit 0 and empty text for OK with nothing selected, which is read in the code and not run.*
- **P10.** A delete plan waits 24 hours while a panel shows it, and ends at Cancel, at a panel
  closed for another, at a cancelled check's late reply, and when its page goes away; every other
  kind waits 10 minutes. *Proven by MA-1475 (the server) and MA-1477 (the page). Conditional on
  browsers sending a keepalive request from `pagehide`, and on Go's timers counting sleep (both
  recalled, not read): where they do not, a plan is held longer, at most one per row.*
- **P11.** A backup between the check and Delete refuses the held plan, and the page asks for it
  again by itself, keeping the typed name. *Proven by MA-1475 (the refusal and its `changed` mark)
  and MA-1477 (the page asking again).*
- **P12.** Delete never removes a name the plan did not list, however long the plan was held.
  *Proven by MA-964 (the re-check under the lock refuses a plan after a backup recorded another
  message, `internal/archdelete/ma964_execute_test.go:229-258`, and Delete keeps files that are not
  the archive's), MA-981 (the encrypted archive's record compared) and MA-1475's case of a plan that
  outlives a backup.*
- **P13.** **Delete when the check finishes** deletes only when the server finds the held plan
  armable, by the same function the panel's reply used, and belonging to the row the panel was
  opened for, and the typed name matches; otherwise it is refused, with the token kept and nothing
  deleted. *Proven by MA-1476, in Go with `assure.Refused`.* The page's live behaviour: MA-1477
  under the Node harness, the friction walk and the Windows lab.
- **P14.** Cancel and closing the page stop a delete only until it is sent; once it is sent, armed
  or by **Delete permanently**, the panel offers no Cancel and says the delete can no longer be
  stopped, so it never says "Nothing was deleted." of a delete that runs. *Proven by MA-1477 under
  the Node harness (the page's half; the server runs a sent delete to its end as today,
  `archivedelete.go:614`).*

## 7. Open items

- **O1, CLOSED (owner, 2026-11-20, Q5: "Yes (Recommended)"): the encrypted WARN fades.** Revision 2
  left an encrypted archive's WARN a WARN on every backup that skips, because a fade needs the
  first-found date beside the archive. The owner allowed that date (§0, Q5). It is designed in
  §1.3 ("Encrypted archives"), with MA-734, A6 and phase 2a's allow-list line amended and MA-1478
  and P5 changed. What one date cannot tell apart is O5.
- **O2, accepted by the fold check: the 1.7.5 auto-pause email and log keep "reconnect the
  source".** The review's condition named "the auto-pause words" with the source-lost words. The
  words a person with a 1.7.4-paused archive sees in 1.7.5 (status, the banner, the Settings card)
  change (§1.3). The auto-pause email and log line that 1.7.5 itself sends come only after runs
  the skip did not apply to, where Repair alone would fail again, so changing them would tell that
  person something untrue. The fold check accepted this, and the same reasoning made R1: status, the banner and the
  card now say Repair only where it would work (§1.3).
- **O3, accepted by the fold check: `-follow-links` is checked against "found plus skipped".** The
  review's words were "the listed inputs minus the skipped ones". Both stop a skipped opt-in from
  refusing; the review's form would also newly refuse an opt-in for a store found only by
  auto-discovery, which works today (§1.5).
- **O4, accepted by the fold check: MA-986 is amended.** Its "an earlier one's slower reply opening
  nothing" now has one exception, an armed panel's own reply (§3.2). Nothing is weakened, but a
  pinned page behaviour changes, so it is listed.
- **O5 (for the owner, not blocking): one date for every gone mailbox of an encrypted archive.**
  The owner's Q5 allows "just a date", so the record keeps one time for the archive, not one per
  mailbox. When a second mailbox of an encrypted archive goes while the first is still gone, it
  carries the first one's time; once that is 14 days old, the second loss shows only as the note
  (which still says a mailbox is gone and how to see which), where a plaintext archive WARNs for it
  for 14 days. Giving the second loss its own WARN needs more beside the archive than a date (a
  mark per mailbox, or how many are gone), or a field in the sealed manifest, whose schema is
  frozen (A1 to A15). This design takes Q5 as worded and states the limit (§1.3, P5). Nothing
  shipped is weakened (1.7.4 refuses such a run outright); against revision 2, whose encrypted WARN
  never faded, the change is the owner's own ruling, and this is its edge. The question,
  if the owner wants it: may the encrypted record also keep how many mailboxes are gone (a count,
  which Q5's wording left out), so that when the count grows the date starts again and the second
  loss WARNs for 14 days?
  *CLOSED by revision 3.1 (owner, 2026-11-20, Q6: "Yes (Recommended)").* The record keeps
  `mailbox_gone_count`; when it grows the date starts again (§1.3, "Encrypted archives"). Its edge,
  a mailbox that comes back in the same run another goes, is stated there and in P5.

## 8. Fold table

Each finding of `docs/review-1.7.5-predesign.md`, by id: confirmed ones with where they are
folded, duplicates with the group that folds them, refuted ones as refuted.

| Finding | Verdict | Folded in |
|---|---|---|
| L1-01 | CONFIRMED, MEDIUM, owner | owner ruling Q4; §1.2 "The commonest case", §1.3 WARN words, P2 |
| L1-02 | CONFIRMED, MEDIUM, owner | owner ruling Q2; §1.3 "The 14 days", `MailboxGoneNoteAfter`, MA-1472 (citing "owner, 2026-11-20, Q2" since revision 3), P3 |
| L1-03 | CONFIRMED, MEDIUM, owner | owner ruling Q1; §0, §3.2 "X14, amended" and the tooltip |
| L1-04 | CONFIRMED, LOW | §1.1 last bullet, §1.4 first sentence, P7, §4 lab (no job file) |
| L2-01 | REFUTED | not folded |
| L2-02 | CONFIRMED, HIGH | §3.2 "Armable, computed and enforced on the server" and "Why the armed request stays on the page", MA-1476, P13 |
| L2-03 | CONFIRMED, LOW | §3.2: the field is `armable` |
| L2-04 | CONFIRMED, LOW | §1.2 "Where", §1.5 "One place reports", MA-1480 |
| L3-01 | CONFIRMED, LOW | §1.3 "The 14 days … counted between backups", P3 |
| L3-02 | CONFIRMED, MEDIUM | §1.3 "How `Since` is kept", MA-1472, P4 |
| L3-03 | CONFIRMED, LOW | §3.1 (the discard route, the late reply, `pagehide`), MA-971 and MA-972 amended, MA-1475, P10 |
| L3-04 | CONFIRMED, LOW | §1.3 "It is permanent until then" and the note's last sentence |
| L4-01 | CONFIRMED, MEDIUM | §1.3 "Archives 1.7.4 already stopped" and `Since` rule 2, MA-1479, §4 lab and release notes, P6, O2 |
| L4-02 | CONFIRMED, MEDIUM, owner | owner rulings Q3 and Q5; §1.3 "Encrypted archives", MA-734, A6 and phase 2a's allow-list line amended, MA-1478, P5, O1 (closed), O5 |
| L4-03 | CONFIRMED, MEDIUM | §3.1 "A backup in between refuses the plan", MA-1475, MA-1477, P11 |
| L4-04 | CONFIRMED, LOW | §2 (MA-1465's note and the question again), MA-1474, P9 |
| L4-05 | CONFIRMED, LOW | §1.3 the run log (no claim at discovery), NAME, the summaries, the WARN's last sentence |
| L4-06 | CONFIRMED, LOW | §3.2 "The name, by one rule" |
| L5-01 | DUPLICATE of L3-02 | group L3-02: §1.3 "How `Since` is kept" (read before `beginRun`) |
| L5-02 | CONFIRMED, MEDIUM | §1.4, MA-1473, P7 |
| L5-03 | CONFIRMED, LOW | §1.5 `-follow-links` and `-list`, MA-1480, O3 |
| L5-04 | CONFIRMED, LOW | §1.3 the record (`ReadLastRun` cleans `Path` and `Name`) |
| L5-05 | REFUTED | not folded |
| L5-06 | DUPLICATE of L3-03 | group L3-03: §3.1 (and MA-972's expiry check kept, on its own field) |
| L5-07 | DUPLICATE of L1-03 | group L1-03: §3.2 the tooltip, "X14, amended" |
| L5-08 | CONFIRMED, LOW | §3.2 "MA-1476's key check" |
| L6-01 | CONFIRMED, MEDIUM | §1.2 rules (a) and (c); §1.4 (one test, `autoplace.GoneSkippable`; `app.GoneSkippable` in revision 2) |
| L6-02 | CONFIRMED, MEDIUM | §1.2 rule (c) and the reworded "group folder gone" bullet |
| L6-03 | DUPLICATE of L2-02 | group L2-02: §3.2, MA-1476 |
| L6-04 | CONFIRMED, LOW | §1.3 (`MinPlausibleTime`), MA-1472 |
| L6-05 | CONFIRMED, LOW | §1.2 rule (b): case folded on Windows only |
| L7-01 | DUPLICATE of L5-02 | group L5-02: §1.4 |
| L7-02 | DUPLICATE of L3-02 | group L3-02: §1.3 (`opts.Warnings` straight after discovery) |
| L7-03 | DUPLICATE of L6-04 | group L6-04: §1.3 |
| L7-04 | DUPLICATE of L2-02 | group L2-02: §3.2 (the row binding) |
| L7-05 | DUPLICATE of L5-03 | group L5-03: §1.5 |
| L7-06 | DUPLICATE of L6-05 | group L6-05: §1.2 rule (b) |
| L7-07 | DUPLICATE of L5-04 | group L5-04: §1.3 the record |
| L7-08 | CONFIRMED, LOW | §1.3 the summaries (`runNowSummaryFor`), MA-1471 |
| L8-01 | DUPLICATE of L4-01 | group L4-01: §1.3 "Archives 1.7.4 already stopped" |
| L8-02 | DUPLICATE of L4-01 | group L4-01: §1.3 `Since` rule 2 |
| L8-03 | CONFIRMED, LOW | §4 step 5 (release notes) |
| L9-01 | DUPLICATE of L3-03 | group L3-03: §3.1 |
| L9-02 | CONFIRMED, LOW | §3.1 "Every delete walks the folder twice", §3.2 panel words, §4 lab (size and clean-up) |
| L10-01 | DUPLICATE of L1-04 | group L1-04: §4 lab (an archive with no job file) |
| L10-02 | DUPLICATE of L2-04 | group L2-04: §1.2 "Where" (they now skip silently) |
| L10-03 | CONFIRMED, LOW | §3.1 "Why holding a plan longer is safe", P12, MA-1475 |
| L10-04 | DUPLICATE of L9-02 | group L9-02: §3.2 "checks once more, then deletes" |
| L10-05 | DUPLICATE of L1-03 | group L1-03: §0, §3.2 |
| L10-06 | REFUTED | not folded |
| L10-07 | REFUTED | not folded |
| L10-08 | DUPLICATE of L8-03 | group L8-03: §1.3 "A downgraded status", §4 step 5 |
| L10-09 | CONFIRMED, LOW | §1.3 "Encrypted archives" (Run now settled), P5 |
| L10-10 | DUPLICATE of L3-01 | group L3-01: §1.3 |
| L10-11 | DUPLICATE of L6-02 | group L6-02: §1.2 |

Tally: 30 confirmed folded (4 of them by the owner's rulings Q1 to Q4 as well as in the text), 21
duplicates folded through their groups, 4 refuted and not folded.

### Fold check of revision 2

The fold check (`docs/review-1.7.5-predesign.md`, "Fold check of revision 2 (2026-11-20)"):
CLEARED AFTER THESE FIXES. Each fix, and where revision 3 applies it:

| Fix | Severity | Applied in |
|---|---|---|
| L1-02 (PARTIAL) | owner citation | §1.6 MA-1472 cites "owner, 2026-11-20, Q2" |
| R1 | MEDIUM, must fix | §1.3 "Archives 1.7.4 already stopped" (when the new words show; the words, with MA-949's Run-now-only and Switch-first forms kept; what stays as today); §1.6 MA-692, MA-693 and MA-949 amended, MA-1479; §4 lab; P6 |
| R2 | MEDIUM, must fix | §3.2 "Once the delete is sent, it cannot be stopped", "What it never does" (both claims narrowed to before the delete is sent), the R36 paragraph; MA-1477; §4 lab and release notes; P14 |
| R3 | LOW, must fix | §1.3 "One helper, in `internal/health`" (`health.GoneMailboxRepairable`); §1.2 "Where" (the rule moved to `internal/autoplace` so health can call it); MA-1479 |
| R4 | LOW, text | §1.3 "The 14 days": "shows the WARN until its next backup" |
| R5 | LOW, text | §2 "Which row an answer is" (`pickChoice`'s rule); MA-1474 (mangled answers); P9 |
| R6 | LOW, text | §1.6 "A6 and phase 2a's allow-list line, amended" |
| R7 | LOW, text | §1.3 "only after a run that ended ok" and the encrypted short sentence; MA-1472, MA-1478; P3, P5 |
| R8 | LOW, text | §1.3 the note's remedy ("and answer Yes to keeping it current"); MA-1472 |
| R9 | LOW, text | §1.3 "What rule 2 cannot tell" |
| O1 (owner, Q5) | closed | §0 Q5; §1.3 "Encrypted archives" (`mailbox_gone_since`); §1.6 MA-734, A6 and phase 2a amended, MA-1478; §4; §5; P5; §7 (O1 closed, O5 opened) |
| O5 (owner, Q6; revision 3.1) | closed | §0 Q6; §1.3 "Encrypted archives" (`mailbox_gone_count`, the restart rule, what one count cannot tell apart); §1.6 MA-734, A6 and phase 2a amended again, MA-1478; §5; P5; §7 (O5 closed) |

Also corrected while applying them: `RunWarning.Since` is `omitzero`, not `omitempty`
(`omitempty` does not leave out a zero time, so revision 2's "byte for byte as before" would not
have held, §1.3); the two citations the fold check called imprecise (`settingscard.go:205-206` says
"Backups stopped … since DATE"; `internal/wizard/wording_v152_test.go:31` pins the pause email's
words, not the banner) are now cited for what they say (§1.3).

## 9. Revision 4: the post-build reviews, built

Every finding of `docs/review-1.7.5-adversarial.md` and `docs/review-1.7.5-friction.md` the leads'
folds sent to the build, where it is written here and the commit that built it (branch
`combined-archive-gone-mailbox`, after `ea42de3`).

| Finding | Severity | Written in | Built in | Rows |
|---|---|---|---|---|
| OUT-01 (with INS-01) | MEDIUM | §1.2, §1.3 *Revision 4* | 63b3092 | MA-869, MA-1479, MA-1472 amended |
| INS-02 (owner, Q7) | MEDIUM | §0, §1.2 rule (e) | 2c830a6 | MA-1481 added, MA-1471 amended |
| CON-02 (owner, Q8) | LOW | §0, §1.3 beside the clamp | 4e5f091 | MA-1472, MA-1478 amended |
| OUT-05 (b), CON-06 (owner, Q10) | refuted as defects | §0, §3.1 | a64673a | MA-1475 amended |
| F5 (owner, Q9) | medium | §0, §3.1 | 94dd0f3 | MA-1477 amended |
| OUT-02 | LOW | §3.2 | 81308a7 | MA-1476 amended |
| INS-03 | LOW | §1.2 rule (a) | 7fcac54 | MA-1471 amended |
| INS-06 | LOW | §1.3 | 7fcac54 | MA-1472 amended |
| CON-01, CON-03 | LOW | §1.3 the words | ab85cab | MA-1472 amended |
| F1, F3, F4, F9, F11, F12 | medium, low | §1.3 | ab85cab | MA-1471, MA-1472, MA-1478 amended |
| F14 | low | §1.5 | 628a00a | MA-1480 amended |
| F2, F10 | medium, low | §1.3 "Archives 1.7.4 already stopped" | f15e040 | MA-1479 amended |
| F7, F8, F13, OUT-03, OUT-04 (guard) | medium, low, INFO | §3.2 | 3837e55 | MA-1476, MA-1477 amended |
| F6 | medium | §3.2 | 7457c4e | MA-1004 amended; X14's line |
| CON-04 | refuted as a defect | §1.3, R9 corrected | (text only) | none |
| INS-04 | LOW, not run | §4 step 4 | the Windows lab | MA-1471's junction case |
| INS-05 | LOW, not run | §5, a known limit | not built | none |
| OUT-05 (a), CON-05, INS-07 | refuted | not folded (§3.1 accepts (a)) | none | none |

Not built, and why: F5's deeper choice and the "Stop looking for it" button (§5); INS-05 (§5).
Owed: the friction re-verify of every new word above; the Windows lab (§4 step 4, with INS-04 and a
moved account); the whole `./internal/app` and `./internal/desktop` runs on CI.

## 10. Revision 4.1: the re-verify's fixes, built

| Finding | Severity | Written in | Built in | Rows |
|---|---|---|---|---|
| RV-01 | MEDIUM | §1.2 *Revision 4.1* | fcfe8db | MA-869 amended |
| RV-02 (the lead's call, Q7's fuller form), RV-04 | MEDIUM, INFO | §1.2 rule (e) *Revision 4.1* | b757401 | MA-1481 amended |
| Windows CI at 1ba95c8 (untyped refusal) | — | §1.2 *Revision 4.1* | 61da953 | MA-1471 amended |
| RV-06 | LOW | §3.1 *Revision 4.1* | 6918973 | MA-1475 amended |
| RV-03 | LOW | §3.1 *Revision 4.1* | e5a0725 | MA-1477 amended |
| RF-03, RF-04, RF-05, RF-08, RF-09 | low, info | §1.2, §1.3 (words) | 93f1212 | MA-1481 amended |
| RF-01, RF-02, RF-06, RF-07, RF-10 | medium, low, info | §3.1, §3.2 (words) | 41ea335 | MA-1477, MA-1004 amended |
| RV-05, RV-07, RV-08 | INFO | not built (the lead's fold) | none | none |
| RF-11 | info | covered by RV-02 and RV-04 | b757401 | MA-1481 |

Owed: a focused re-verify of these fixes; RV-01's Windows-only tests and RF-03's walk (Windows CI and
the lab).

## 11. Revision 4.2: the re-verify, round 2

Built from `docs/review-1.7.5-reverify-r2.md` (the lead's fold), each marked *Revision 4.2* beside its
text: RV2-01 and RF2-01 (the recorded name first; the server-name fallback with both "-N" spellings
and the one-outside rule; MA-1481 amended, the traded case as it now stands), RV2-03 (a tombstone
per "-N" spelling), RV2-06 (rule 1's places check), RV2-02 (only a plan read with its key open ends
at a lock; MA-1475 and MA-1004 amended), RF2-02 to RF2-06 (words; RF-07's card words now pinned by a
committed test). Not built: RV2-04, RV2-05 (the fold says why). Flagged: RF2-01's "Thunderbird still
has NAME" form conflicts with MA-1288 and is built without the name.

## 12. Revision 4.3: the re-verify, round 3

Built from `docs/review-1.7.5-reverify-r3.md` (the lead's fold), each marked *Revision 4.3* beside its
text: RV3-01 (every plaintext record keeps the folders' names, a small list beside the completeness
entries; MA-1481 amended), RV3-02 (no live account with the recorded name asks the server-name rule;
the traded case it brings back written in MA-1481), RV3-03 (the command line's prep and the
Desktop's rebuild ask with the record's names; MA-1473 amended), RV3-04 (an encrypted archive's
known limit, in §5, MA-1481 and the README), RF3-02 and RF3-03 (words; MA-1004's pin restated).
Not built: RV3-05 (no change, the fold's call); RF3-01 is superseded by the RV3-01 fix.
