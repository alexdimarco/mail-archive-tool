# Pre-code design review: 1.7.5 fixes (docs/design-1.7.5-fixes.md, revision 1)

**Date:** 2026-11-20. **Process:** `../assurance-kit/process/design-review.md` (the 10-lens battery),
with the finder/skeptic split of `adversarial-review.md`. **Design under review:** revision 1 at
commit 47f9efd, branch `combined-archive-gone-mailbox`, off main ed491c4 (release 1.7.4).

**Verdict: GO WITH CONDITIONS.** 55 findings, 30 confirmed (1 HIGH, 10 MEDIUM, 19 LOW), 21
duplicates, 4 refuted. 26 confirmed findings are the design author's to fold; 4 need the owner's
ruling (owner questions 1 to 4 below). No code is written until revision 2 folds every confirmed
condition and the owner has answered.

**How it was run (lean, for the weekly usage cap; see the notes' token-budget-care):** all ten
lenses, given to three read-only reviewers instead of five lens pairs: lenses 1-4, lenses 5-6,
lenses 7-10. Their briefs are the stopped review's (wf_fa9876b8-f68) word for word. One skeptic
then checked every finding against the code, with refutation as the default. The rescue step was
done by the lead, not by an agent: the one HIGH (L2-02, the armed delete guarded only by the page)
dissolves with a modest change the skeptic names, a server-side refusal of an armed delete unless
the held plan is armable and belongs to the row the panel was opened for. The lead did not run
the rescue's second half, the search for a deeper weakness the lenses missed; that is owed to
the post-build adversarial pass.

**Sources:** each reviewer's own SOURCES line below says what it read and what it could not reach.
Recalled, not read, and flagged by the reviewers: whether Thunderbird gives back a deleted folder of
an account it still has, makes a new default profile when its profile folder is gone, and keeps a
Local Folders store in every profile; whether Go timers count system sleep.

Below, in order: the skeptic's verdicts, groups and owner questions; then the three finders' reports
as they returned them.

---

## Skeptic: verdicts, groups, owner questions

L1-01 | CONFIRMED | MEDIUM | - | OWNER | Rule (b) counts any store that is still there, and the commonest "All of them" archive is one account plus Local Folders (picker.go:73-86 shows the list only for 2+ stores; run.go:2097 includes Local Folders), so losing that one account still "succeeds" on Local Folders with only a 14-day warning, a case §1.2 and P2 never state. Lowered from HIGH because the warning exists, and Thunderbird gives back the folder of an account it still has (recalled, not read).
L1-02 | CONFIRMED | MEDIUM | - | OWNER | The 14 days are a new product limit beside the owner-ruled 3 and 30 days (state/sourceloss.go:23-35), and §0 records no ruling on them; put the length and the fade to GREEN to the owner, and record the answer in §0 and MA-1472.
L1-03 | CONFIRMED | MEDIUM | - | OWNER | §0 records only the owner's question ("forgo the check") and "The session answered", yet §3.2 calls the narrower form "a scoped exception the owner asked for", and no change to the X14 row (ux-contract.md:24) is listed; record the owner's word on the narrower form and add "X14, amended".
L1-04 | CONFIRMED | LOW | - | DESIGN | An edit never rebuilds the job (localschedule.go:188-197, "An edit needs only the securely readable job file"); narrow §1.1 and P4 to a Settings create with no job file yet, and to the command-line schedule conversion.
L2-01 | REFUTED | NONE | - | - | §0 keeps "first" against the other approved questions ("The per-mailbox archives … are the next release's") and says "each can be reviewed, built and reverted alone"; what rides in 1.7.5 is a sequencing call, and the only harm named is a possible delay.
L2-02 | CONFIRMED | HIGH | - | DESIGN | deleteAction checks only the token and NameMatches (archivedelete.go:563-582), so "plain", the panel-to-archive routing and P7 rest wholly on page script that the design makes more complex (it replaces delSeq); raised because a destructive action guarded only on the client is the kit's standing anti-pattern (adversarial-review.md, worked example).
L2-03 | CONFIRMED | LOW | - | DESIGN | planView already sends kind:"plain", meaning unencrypted (archivedelete.go:469; archdelete.go:36-46); give the new field another name, for example `armable`.
L2-04 | CONFIRMED | LOW | - | DESIGN | export discovers at main.go:275 and again inside app.Run (run.go:631), and §1.2 has both report skips; name app.Run's result as the one place the command line reports them, and have callers that do not report discard the list on purpose.
L3-01 | CONFIRMED | LOW | - | DESIGN | §1.3 never says what the 14 days are counted against, while the existing limits are "NEVER compared with now" (state/sourceloss.go:23-28); state it as the last run's start minus Since.
L3-02 | CONFIRMED | MEDIUM | - | DESIGN | Since lives only in the one-slot last-run record, which every run rewrites (beginRun run.go:743 and :1364-1390, writeRefusedRun :1445-1451) and whose warnings "never latch" (lastrun.go:208-212); so a refused or cancelled run in between restarts it, and a Since in the future is copied forward. Raised to MEDIUM because P3 is then false in ordinary sequences.
L3-03 | CONFIRMED | LOW | - | DESIGN | No route discards a delete plan (dashboard.go:414-417), and §3.2 lets a cancelled plan "wait unused", now for 24 h with its full file list; Cancel posts a discard limited to delete plans. Lowered because there is at most one plan per row (deletePlanKey), and all end at a switch and at Shut down.
L3-04 | CONFIRMED | LOW | - | DESIGN | Only a Settings create ever drops a gone input, so the skip line and the note repeat for the archive's life; say so in §1.3 and add one line on how to end it (run the wizard again and choose All of them).
L4-01 | CONFIRMED | MEDIUM | - | DESIGN | An auto-paused 1.7.4 schedule makes no runs, so "recovers on its first 1.7.5 run" never happens, and the words still say "reconnect the source" (health.go:511, dashboard.go:2392, wizard.go:392 and :461).
L4-02 | CONFIRMED | MEDIUM | - | OWNER | The encrypted record's frozen allow-list keeps no warnings (encrypted_record.go:136-149), so a scheduled encrypted "All of them" archive goes from failing loudly to succeeding with no unattended signal at all; widening that frozen list, or leaving encrypted archives out of the change, is the owner's call.
L4-03 | CONFIRMED | MEDIUM | - | DESIGN | samePlan compares the manifest's size, time and SHA-256 and every name (execute.go:340-376), so any scheduled backup that saves mail between the plan and Delete refuses it, after a second slow walk and with the token spent; state this limit in §3.1 and P6, and have the page ask for the plan again by itself, keeping the typed name.
L4-04 | CONFIRMED | LOW | - | DESIGN | On Linux no row is selected by default (list_unix.go passes no default row), so OK with nothing chosen now installs nothing and says nothing; show MA-1465's note and ask again (Cancel still installs nothing), or end with a line saying no schedule was set up.
L4-05 | CONFIRMED | LOW | - | DESIGN | "the other mailboxes were backed up" is written at discovery, before the backup, and the warning speaks only to the person who removed the account on purpose; write the line at the end of the run, add "if you did not remove it, add the account back in Thunderbird", and name the account when it can be read.
L4-06 | CONFIRMED | LOW | - | DESIGN | NameMatches folds Unicode form but not case (execute.go:33-38), so "differs in letter case or form → does not fire" contradicts the firing rule when only the form differs; keep one rule, NameMatches against the plan's name.
L5-01 | DUPLICATE | NONE | L3-02 | - | The same Since-carry failure; it adds that the previous record must be read before beginRun.
L5-02 | CONFIRMED | MEDIUM | - | DESIGN | §1.4 drops gone inputs with no version of (b). All gone gives a pure -auto job that only ever fails with "no sources" (run.go:632-633), which leaves the streak unchanged (app/sourceloss.go:49-66), so no source-loss warning and no auto-pause; some gone means those mailboxes are never warned as gone. Its "any Settings save erases the warning" part is refuted, because an edit does not rebuild (localschedule.go:188-197).
L5-03 | CONFIRMED | LOW | - | DESIGN | main.go:278 checks -follow-links against the discovered list, so a skipped input that was opted in now refuses with wrong words and a kind that leaves the streak, on command-line schedules that keep -follow-links (job.go:295-297); -list also stays silent about the skip.
L5-04 | CONFIRMED | LOW | - | DESIGN | ReadLastRun's single cleaning point strips only Code and Message on each warning (lastrun.go:378-380); strip the new Path there too.
L5-05 | REFUTED | NONE | - | - | §4 item 5 releases only after the build and CI on all three systems; branch commits are not shipped, so no released build skips mailboxes without health's entry for them.
L5-06 | DUPLICATE | NONE | L3-03 | - | The Cancel half repeats L3-03. The test-seam half is refuted: MA-1475 covers "a delete plan's life, and every other kind's", and CLAUDE.md already forbids weakening MA-972's expiry test.
L5-07 | DUPLICATE | NONE | L1-03 | - | The same X14 failure; it adds the Delete… tooltip "You see what would be removed first" (dashboard.go:2396), folded into L1-03.
L5-08 | CONFIRMED | LOW | - | DESIGN | `plain` is a hand-kept deny-list beside planView (archivedelete.go:441-505), so a later line that needs a decision would leave it true; MA-1476 classifies every key planView sends and fails on any it has not classified.
L6-01 | CONFIRMED | MEDIUM | - | DESIGN | "Gone" is decided by os.Stat, which follows links (run.go:1982-1985), while "where auto-discovery looks" is a name match, so a profile or account folder linked to an unplugged drive is skipped instead of refused, which is the case the owner was told stays caught; §1.4 uses Lstat, so the two parts disagree.
L6-02 | CONFIRMED | MEDIUM | - | DESIGN | §1.2 says "a whole Thunderbird profile gone … refuses as today", but (b) counts any store found this run (another program's store, or a new default profile's Local Folders; that Thunderbird makes one is recalled), so a whole profile can be skipped one mailbox at a time.
L6-03 | DUPLICATE | NONE | L2-02 | - | The server cannot tell an armed send from Delete permanently.
L6-04 | CONFIRMED | LOW | - | DESIGN | Only a Since in the future is guarded; a clock that was unset at the first gone-run gives a Since years back, which is carried forward and shows a GREEN note at once. The streak already guards this with MinPlausibleTime (state/sourceloss.go:73-95).
L6-05 | CONFIRMED | LOW | - | DESIGN | Lower-casing on macOS widens (a) on a case-sensitive APFS volume, where auto-discovery would never find that path; fold case on Windows only.
L7-01 | DUPLICATE | NONE | L5-02 | - | The rebuild drops inputs without (b).
L7-02 | DUPLICATE | NONE | L3-02 | - | The same Since carry; its fix (put the skip warnings into opts.Warnings before beginRun) is folded in.
L7-03 | DUPLICATE | NONE | L6-04 | - | A Since from a clock that was behind.
L7-04 | DUPLICATE | NONE | L2-02 | - | It adds the row binding (two archives with the same folder name) to L2-02's server check.
L7-05 | DUPLICATE | NONE | L5-03 | - | The -follow-links refusal.
L7-06 | DUPLICATE | NONE | L6-05 | - | macOS case folding.
L7-07 | DUPLICATE | NONE | L5-04 | - | Path cleaning.
L7-08 | CONFIRMED | LOW | - | DESIGN | §1.3's list of places omits the plaintext Desktop Run now note (runNowSummaryFor, jobrun.go:100), while X6 asks the GUI summary to carry the same counts; add it to §1.3 and MA-1471.
L8-01 | DUPLICATE | NONE | L4-01 | - | The auto-paused archive.
L8-02 | DUPLICATE | NONE | L4-01 | - | Taking the first Since from the streak's first-unreachable time is folded into L4-01.
L8-03 | CONFIRMED | LOW | - | DESIGN | §4 has no release-notes step, though command-line -auto -input now exits 0 where it refused, auto-paused archives need Repair, delete plans last 24 h, and a downgraded status shows nothing for mailbox_gone (health.go:778-805 shows known codes only).
L9-01 | DUPLICATE | NONE | L3-03 | - | A cancelled plan held for 24 h.
L9-02 | CONFIRMED | LOW | - | DESIGN | Delete plans again in full under the lock (execute.go:340), so every delete walks the folder twice, and an armed delete does it back to back; state it, size the lab archive so its check takes minutes, and add its removal to the lab clean-up.
L10-01 | DUPLICATE | NONE | L1-04 | - | It adds that the lab's "Settings saving the schedule" must run on an archive with no job file, or it already passes on 1.7.4.
L10-02 | DUPLICATE | NONE | L2-04 | - | "keeps its meaning" is false: prepareThunderbird (main.go:2088) now skips silently.
L10-03 | CONFIRMED | LOW | - | DESIGN | "Why this is safe" lists checks that differ from samePlan (execute.go:369-376) and leaves out the comparison of names to remove, which is what makes "can never delete more" true; restate the list from samePlan, narrow the claim to "never removes a name the plan did not list", and put it in §6 as proven by the existing re-check rows plus an MA-1475 case of a plan that outlives a backup.
L10-04 | DUPLICATE | NONE | L9-02 | - | The panel's "deletes … as soon as the check finishes" hides the second walk; the wording is folded into L9-02.
L10-05 | DUPLICATE | NONE | L1-03 | - | X14.
L10-06 | REFUTED | NONE | - | - | The list describes ends this design does not change: the forget's discard stays in the code (encryption_forget.go:151-155), and the switch is already in the list ("when the current archive is switched"); no failure follows.
L10-07 | REFUTED | NONE | - | - | §2 says "The build re-checks the full list", and the one list that misses its default ("What are you exporting?", wizard.go:780-830) lands on a file chooser that can be cancelled and installs nothing.
L10-08 | DUPLICATE | NONE | L8-03 | - | No older reader shows Message for an unknown code, so a downgraded status shows nothing; folded into the release note.
L10-09 | CONFIRMED | LOW | - | DESIGN | P3's encrypted Run now condition can be settled now: the encrypted Run now result already carries lines held only in memory (encryptedSummaryFor, jobrun.go:279 and :298-321, reached through runNowKeyed); settle it in §1.3 and P3.
L10-10 | DUPLICATE | NONE | L3-01 | - | The same omission.
L10-11 | DUPLICATE | NONE | L6-02 | - | "a whole profile gone refuses" says more than (b) delivers.

Tally: 55 found, 30 confirmed (1 HIGH, 10 MEDIUM, 19 LOW; 4 need the owner), 21 duplicates, 4 refuted.

GROUPS:

L2-02, L6-03, L7-04 (HIGH, DESIGN). Two rules guard "Delete when the check finishes": only a plain plan, and only the archive whose panel was opened. Both live only in the page script. The server checks just the token and the typed name. So a script slip, or a check result routed to the wrong panel when two archives share a folder name, deletes without the plan being shown, or deletes the wrong archive. Condition: the armed request carries armed:true and the row its panel was opened for. deleteAction refuses it, keeping the token and deleting nothing, unless the held plan's row is that row and the plan is armable by the same function planView uses. MA-1476 proves that refusal in Go with assure.Refused. Better still, keep the armed request on the server (the typed name sent with it, Cancel ending it), so a reload or a sleep does not lose it; otherwise say in §3.2 why not.

L1-03, L5-07, L10-05 (MEDIUM, OWNER). The design grants itself an exception to X14 (look before deleting). The owner's recorded words asked something else, and the narrower answer has no recorded reply. Condition: the owner's word in §0; an "X14, amended" line with the exact scope (armed, armable plan only, full check still run and checked again under the lock); new Delete… tooltip words.

L6-01, L6-02, L10-11 (MEDIUM, DESIGN). "Gone" follows links, "where auto-discovery looks" is only a name match, and (b) is met by any store found this run. So an unplugged linked drive is skipped, and a whole lost profile can be skipped one mailbox at a time. Condition, one rule for both: skip only when os.Lstat says the input does not exist and its parent folder exists now as a real folder inside the same auto-discovery place; otherwise refuse as today. Reword the "whole Thunderbird profile gone" sentence to match.

L1-01 (MEDIUM, OWNER). In the commonest "All of them" archive, the only real account can vanish while backups keep "succeeding" on Local Folders. Condition: the owner's answer to question 4, and this case stated in §1.2 and P2, with the words from L4-05.

L3-02, L5-01, L7-02 (MEDIUM, DESIGN). The "not found since" date lives in a record that every run rewrites, including refused and cancelled runs. Condition: keep each gone input's first-seen date in a small file beside the source streak, keyed by PathSourceID, read before beginRun and written under the archive lock. At minimum: put the skip warnings into opts.Warnings straight after discovery, so the running, refused and final records all carry them, and change a carried Since that is later than this run's start to this run's start. Related, LOW: L6-04 and L7-03 (treat a Since before MinPlausibleTime as this run's start and never carry it); L3-01 and L10-10 (count the 14 days between runs, never against now).

L1-02 (MEDIUM, OWNER). Whether, and when, the gone-mailbox warning fades to a note.

L5-02, L7-01 (MEDIUM, DESIGN). Condition: Settings' rebuild keeps a gone input that lies where auto-discovery looks, still requiring it to be one of the archive's own recorded stores (PathSourceID works on a gone path), and refuses when no listed input exists. The run-time rule then skips it and warns.

L4-01, L8-01, L8-02 (MEDIUM, DESIGN). Condition: for an -auto archive, the auto-pause words and the source-lost words say "click Repair (or Run now): this version skips a mailbox that is gone". Add a release note. Take the first Since from the streak's first-unreachable time when its last kind is local-source-unreachable, read before this run resets the streak. Add an auto-paused archive to the §4 lab.

L4-02 (MEDIUM, OWNER) and L10-09 (LOW, DESIGN). Encrypted "All of them" archives turn silent on every unattended surface. Settle the Run now part from the code now; the rest is owner question 3.

L4-03 (MEDIUM, DESIGN), with L9-02 / L10-04 and L10-03 (LOW). The 24-hour plan rarely survives the archive's own scheduled backups, and every delete walks the folder twice. Condition: state the limit; re-request the plan automatically, keeping the typed name, after a "changed" refusal; panel words "checks once more, then deletes"; restate the safety re-check from samePlan; size the lab archive and add its clean-up.

L3-03, L5-06, L9-01 (LOW, DESIGN). Cancel, and a cancelled check's late result, discard the plan through a discard limited to delete plans; the 24 h applies only to a plan that a panel still shows.

L2-04 / L10-02 and L5-03 / L7-05 (LOW, DESIGN). On the command line: one place reports skips (app.Run's result); -follow-links is checked against the listed inputs minus the skipped ones; -list says what it skipped; the line about DiscoverInputs keeping its meaning is corrected.

Small single items (LOW, DESIGN): L1-04 / L10-01 (narrow the Settings claim; lab on an archive with no job file), L2-03 (rename the field), L3-04, L4-04, L4-05, L4-06, L5-04 / L7-07, L5-08, L6-05 / L7-06, L7-08, L8-03 / L10-08.

OWNER QUESTIONS:

1. Do you accept, as a recorded exception to the "look before deleting" rule (X14), a "Delete when the check finishes" button that still runs the full check, and checks again before deleting, but skips showing the plan when nothing in it needs your decision? Recommend yes, but only with group L2-02's server-side check.
2. Should a gone mailbox's warning turn into a plain note 14 days after it was first found gone, counted between backups and not against today's date? Recommend yes: a warning that can never clear teaches people to ignore status.
3. May an encrypted archive's record carry one fixed line with no path in it ("a mailbox this archive backs up was not found"), like the existing program-copies line, so encrypted "All of them" archives can skip a gone mailbox and still warn? Recommend yes; if no, encrypted archives keep refusing as today in 1.7.5.
4. In the commonest "All of them" archive (one mail account plus Thunderbird's Local Folders), is it right that losing that one account lets backups keep "succeeding" on Local Folders alone, with a warning and no email? Recommend yes: it is the removed-account case you already ruled on. The warning should add "if you did not remove it, add the account back in Thunderbird".

SOURCES: Date worked 2026-11-20. Register: findings (skeptic verdicts), read-only, nothing run. Read in full: the design (revision 1; worktree HEAD confirmed as 47f9efd, sha256 not computed), design-review.md, adversarial-review.md, and the three findings files. Read in part: ux-contract.md rows X6, X10 and X14. Code read: internal/app/run.go (DiscoverInputs, autoDiscover and its patterns, Run's discovery, beginRun and newRunRecord, RecordRefusedRun and writeRefusedRun, the end of recordRun, FollowLinksRefusal), app/sourceloss.go, state/sourceloss.go, state/lastrun.go (RunWarning, the ReadLastRun cleaning point), state/encrypted_record.go, health.go (source_lost and the warnings switch), wizard picker.go, wizard.go (offerSchedule, the list dialogs, the auto-pause words), jobrun.go, desktop localschedule.go, pending.go, archivedelete.go (plan route, planView, deleteAction), dashboard.go (routes, tooltip, banner, afterSwitch), encryption_forget.go, runnow_keyed.go, ma972_delete_token_test.go, archdelete.go and execute.go (NameMatches, lockAndRecheck, samePlan), source.go (IsMailStoreDir), zenity list_unix.go, cmd main.go (:262-300 and prepareThunderbird), and job.go. Not read: deletescript.go (the delSeq claims are the finders'), design-archive-delete.md, pause.go, alert.go, catalog rows. Recalled, not read: that Thunderbird always keeps a Local Folders store, gives back the folder of an account it still has, and makes a new default profile when its profile folder is gone. The smallest check for you: open a test Thunderbird profile, delete one still-configured account's folder, start Thunderbird, and see whether the folder comes back; that settles how much of L1-01 is real. Counters: Register finding; Borrowed lines none; Drift none noticed; Touched nothing; Persona none; Source reached: design and findings yes, code citations yes as listed, page script no.

---

## Finder report: lenses 1 to 4 (purpose, focus, durability, usability)

L1-01 | 1 | HIGH | §1.2 (b), P2
failure: Rule (b) counts any store that is still there. A Thunderbird profile that still exists nearly always keeps its Local Folders, plus any feed or unified-folder stores, and auto-discovery finds them as stores. So take the commonest "All of them" archive: one real account plus Local Folders. That is two rows, so the list is shown with All of them as the default. If its only account folder vanishes by accident, the run is not refused. It "succeeds" backing up an almost empty Local Folders, and the run log says "the other mailboxes were backed up". The source-loss streak resets, so there is no 3-day banner, no 30-day pause and no failure email. An archive that was already failing gets "MailArchive backups are working again". The only signal left is a WARN that turns GREEN after 14 days. The owner's question described a combined archive whose other accounts keep being backed up; this case has none.
evidence: §1.2 "(b) At least one mailbox remains after the skip: a listed input that exists, or one that auto-discovery finds this run"; §1.3 "recovers on its first 1.7.5 run, and gets the existing recovery notice and email". internal/app/run.go:2097 "Each account under ImapMail/ and Mail/ (incl. "Local Folders") is a store"; internal/source/source.go:77-78 (any folder with a *.msf file is a store); internal/wizard/picker.go:123 (groupTBLocal "Local Folders, feeds, unified folders") and :456-457 ("Thunderbird counts for its mail accounts only"); picker.go:73, :81 (two stores found → the list, All of them as default); internal/app/sourceloss.go:50-52 (a clean run resets the streak); internal/alert/alert.go:203 "MailArchive backups are working again". That Local Folders exists in nearly every profile is recalled Thunderbird behaviour; the picker's own group backs it up.
condition: In (b), count only a mail account's store, an Outlook or Evolution store, or a store that holds mail; otherwise refuse as today. Recalled: POP3 accounts can deliver into Local Folders, so "holds mail" is safer than excluding Local Folders by name. Or state this case in §1.2 and P2 and put it to the owner as a ruling.

L1-02 | 1 | MEDIUM | §1.3 status (the 14-day WARN, then a note)
failure: Nothing ties the 14-day expiry to the owner's words or to a stated property. The other limits of this kind, the 3-day warning and the 30-day pause, are owner rulings. Emails and notices for a skip are a conceded non-goal, so the WARN is the only signal on runs nobody watches. The design lets it fade to GREEN on its own.
evidence: §0 records nothing on how long a gone mailbox warns; §1.3 "For 14 days from Since it is a WARN … After that it is a note (GREEN)"; §5 (no email or notice); internal/state/sourceloss.go:33-35 ("owner policy"); docs/scenario-catalog.md:1210 MA-871 ("owner ruling 2026-10-28").
condition: Put the expiry and its length to the owner as a ruling, recorded in §0 and in MA-1472, naming the alternative: a WARN for as long as the input stays listed.

L1-03 | 1 | MEDIUM | §3.2 "Look before writing (X14)", §0
failure: The design calls the X14 exception "a scoped exception the owner asked for". The owner's recorded words ask a question about something else: skipping the check. §0 records only the session's counter-proposal, with no owner reply. No change to the X14 row is listed, although the earlier changes to X14's scope sit in that row with the owner's date. So an exception to an X-row is granted by the design itself.
evidence: §0 "maybe add an option to forgo the check and delete immediately?" … "The session answered"; §3.2 "This is a scoped exception the owner asked for"; §3.2 lists only MA-1476 and MA-1477; docs/ux-contract.md:24 (the X14 row with its "Owner, 2026-10-27" scope note); the repo's CLAUDE.md: "accepted by the OPERATOR, never self-granted".
condition: Add "X14, amended" to §3.2's rows with the exact scope (armed, plain plan only), and record the owner's ruling on that narrower form in §0 before build.

L1-04 | 1 | LOW | §1.1 last bullet, P4
failure: The claim "Settings cannot save that archive's schedule again" holds only when Settings creates a schedule, because no job file exists yet. An edit, including Repair of an auto-paused schedule (its job file and descriptor are kept), never reads the record. P4 claims a general fix where the change only fixes the create form and the conversion of a command-line schedule.
evidence: internal/desktop/localschedule.go:188-196 ("An edit needs only the securely readable job file … a source drive away or a record gone does not stop it"); internal/desktop/dashboard.go:1665-1675 (the rebuild only decides ShowLocalCreate and exportToRunJob).
condition: Restate §1.1 and P4 as "a Settings create (no job file yet), and the command-line-schedule conversion".

L2-01 | 2 | MEDIUM | §0 "ship together", §4
failure: The owner said "Fix the combined-archive bug first, as 1.7.5". The design puts it behind one adversarial pass over all three parts, one friction review, one Windows lab that includes a large-archive armed delete, and one CI round. So the bug fix waits for part 3.2's page work and its walk. "Each can be … reverted alone" is true of the code, not of the gates.
evidence: §0 "Fix the combined-archive bug first, as 1.7.5?"; "They ship together as 1.7.5 because each is small"; §4 items 3-5.
condition: Release parts 1 and 2 as 1.7.5 on their own gates, and move part 3 to the next release; or say in §4 that 1.7.5 ships without part 3 if its gates lag.

L2-02 | 2 | MEDIUM | §3.2 (the armed delete is held only by the page)
failure:
(i) It needs five new page behaviours plus a guess at the name before the check.
(ii) A reload, a tab the browser throws away, or a sleep that drops the connection loses it with no word. The person who stepped away comes back to no panel, must click Delete… again and wait through the check again.
(iii) `plain` is computed on the server but enforced only by the page. The delete route checks only the token and the typed name, so it cannot tell an armed send from a send after the plan was shown. A page-script bug or a stale cached page could fire on a synced or unrecorded plan.
evidence: §3.2 "the page sends the delete at once", "Keep this page open", "When the page is closed … nothing is deleted"; internal/desktop/archivedelete.go:563-582 (deleteAction checks the token and NameMatches only); archivedelete.go:417 (the server already holds the plan).
condition: Keep the armed request on the server. The typed name is posted with the plan request, or tied to a plan-request id. When the check ends, the server deletes only if `plain` is true and NameMatches(typed, plan.Name). Cancel ends it on the server, and the held results already survive a reload (MA-972). Or say in §3.2 why the page-held form was chosen over this.

L2-03 | 2 | LOW | §3.2 "a new field plain"
failure: "plain" already means "unencrypted": archdelete's Kind Plain, which planView already sends as kind:"plain" in the same reply. A second `plain` meaning "nothing needs a decision" invites mix-ups in the page script and its tests. An unencrypted plan with a synced line would be kind "plain" with plain=false.
evidence: internal/archdelete/archdelete.go:36-37; internal/desktop/archivedelete.go:469.
condition: Name the field for what it means, for example `noDecision` or `armable`.

L2-04 | 2 | LOW | §1.2 "Where"
failure: DiscoverInputs keeps its name but now silently drops gone inputs, so a future caller (the next release's per-mailbox archives) can lose the skip report without noticing. The command line's export runs discovery twice, at cmd/mailarchive/main.go:275 and again inside app.Run, and the design says both "report what they skipped". The command line may then print the list twice, or two lists that disagree if an input goes in between.
evidence: §1.2 "DiscoverInputs calls it and drops skipped. app.Run and the command line's export (main.go:275) call it and report"; main.go:275, then app.Run at internal/app/run.go:631; main.go:2088.
condition: Use one function that returns the skipped list; a caller that does not report discards it on purpose (`files, _, err :=`). Name one place where the command line reports skips: app.Run's result.

L3-01 | 3 | MEDIUM | §1.3 (OMISSION: what the 14 days are measured against)
failure: The design never says whether the 14 days count against the clock now or between runs. The rule this code already follows is "never compared with now", so a computer that was simply off trips nothing. If the count is against now, an archive whose one run found the account gone, on a computer then off or asleep for two weeks, starts straight at a GREEN note. Nobody ever saw the WARN.
evidence: §1.3 "For 14 days from Since"; "A Since in the future … counts as the run's start" (only meaningful against now); internal/state/sourceloss.go:25-28 ("NEVER compared with now"); MA-871.
condition: State that the WARN lasts while (the last run's start − Since) is under 14 days, never measured against now.

L3-02 | 3 | LOW | §1.3 (Since carried from run to run)
failure:
(i) A Since written while the clock ran ahead is copied by every later run. "A future Since counts as the run's start" keeps it a WARN until real time passes that date plus 14 days, while the text promises a note on that future date.
(ii) Any record in between without the warning restarts Since and moves the promised date: an unreadable record, a run refused before discovery, a one-off command-line run into the archive, a rollback to 1.7.4.
Both fail toward more warning, but the promise in the text breaks.
evidence: §1.3 "copies Since from the previous last-run record … otherwise Since is the run's start"; internal/app/run.go:1390 (a run's record carries only that run's warnings); run.go:1445-1451.
condition: The run fixes it when writing: a carried Since later than this run's start becomes this run's start. The WARN drops the exact date, or says "about two weeks after it was first found gone".

L3-03 | 3 | MEDIUM | §3.1, §3.2 Cancel
failure: A held delete plan lists every file the delete would remove. Cancel and closing the panel release nothing, and plans for different rows never replace one another. A person who opens and cancels Delete… on several large archives keeps several full file lists in Desktop's memory for 24 hours; today it is 10 minutes. On a laptop that sleeps, the 24-hour timer may not count sleep (recalled, not read), so it can be days.
evidence: §3.2 "its plan waits unused until it ends (§3.1)"; internal/archdelete/archdelete.go:161-163 ("Remove lists every file Delete removes"); internal/desktop/archivedelete.go:417 (one held plan per row); internal/desktop/pending.go:156-161 (put replaces only the same row's plan; one timer); no delete route discards a plan (archivedelete.go:374, :563, :1071, unlistAction).
condition: Cancel, and a panel closed for another one, sends a discard of its plan. Keep at most one held delete plan, or a small fixed number with the latest kept, and state the limit in §3.1 and MA-1475.

L3-04 | 3 | LOW | §1.3, §1.4, §5
failure: A gone input never leaves the job, because §1.4's drop happens only when Settings creates a schedule. So the record's warning, the GREEN note, and the wizard and command-line "Not found, so skipped" lines repeat on every run for the life of the archive, one more per removed account over the years. The design neither says this is permanent nor tells the person how to end it.
evidence: §1.4 (the drop lives inside reconstructLocalJob); internal/desktop/localschedule.go:188-196 (an edit never rebuilds); §1.3 "one path per line"; §5 (no "Stop looking for it" button).
condition: State in §1.3 that it is permanent, and add one line to the note saying how to end it: run the wizard again and choose All of them. No new mechanism is needed.

L4-01 | 4 | MEDIUM | OMISSION (the person whose archive 1.7.4 already auto-paused)
failure: The recovery story is "recovers on its first 1.7.5 run", but a schedule that auto-paused makes no runs. This person still reads "reconnect the source (its mailbox or mail folder) to resume backups", and the pause email says "reconnect the source, then … click Repair". Someone who removed the account on purpose cannot reconnect it, and nothing tells them Repair or Run now would now work. The schedule can stay stopped indefinitely (Type II friction: a design choice that can be fixed now). When they do recover, Since starts at that run, though the streak already knows when the folder first went missing.
evidence: §1.3 "recovers on its first 1.7.5 run"; internal/wizard/wizard.go:461 (the pause message); internal/health/health.go:511 and internal/desktop/dashboard.go:2392 (the "reconnect the source" words); wizard.go:385-392.
condition: Add this person to §1.3. For an -auto archive where another mailbox remains, the source-loss words say "click Repair (or Run now); a mailbox that is gone is now skipped" instead of asking to reconnect. Take the first Since from the streak's first-unreachable time when there is one, and add this case to the §4 lab.

L4-02 | 4 | MEDIUM | §1.3 "Encrypted archives", P3
failure: Take an encrypted "All of them" archive whose account folder vanished by accident. Before 1.7.5: a failed run, a failure email, the streak, the pause. After: every scheduled run succeeds, no warning can be recorded, status shows nothing, and the streak resets. The only place it shows is an attended run's screen, and even Desktop's Run now is uncertain. So the loss stays invisible to every surface that runs on its own, indefinitely. The owner's question did not describe this case.
evidence: §1.3 "Nothing new is written beside them … If it cannot, that surface says nothing"; internal/state/encrypted_record.go:136-149 (no warnings allowed in the encrypted record); internal/wizard/jobrun.go:367-371 (Run now runs with Unattended: true).
condition: Keep encrypted archives refusing as today until there is a signal the encrypted record is allowed to carry (for example a count in the streak that names no source). Or put "encrypted archives skip silently on scheduled runs" to the owner as its own ruling.

L4-03 | 4 | MEDIUM | §3.1 (the person who steps away without arming, on an archive with a schedule)
failure: The 24 hours do not survive the archive's own backups. Any scheduled run in between that saves mail changes the manifest and adds file names. At Delete permanently the plan is made again under the lock, a second full check that takes minutes on a large archive. It is found changed and refused with "This archive changed since you clicked Delete. Click Delete again.", and the token is spent. The person then has to click Delete… again, wait through the check, type the name, click again, and wait through the re-check: the owner's complaint again. With an hourly schedule the plan really lasts under an hour. The design never mentions scheduled runs; "can only be refused more often" covers safety, not this person.
evidence: §3.1; internal/archdelete/execute.go:340, :349, :372-376 (the plan made again and compared: manifest SHA-256 and time, every file name); internal/desktop/archivedelete.go:637-640.
condition: State this limit in §3.1 and P6. When a held plan is refused as changed, the page asks for the plan again by itself and keeps the typed name, so the person clicks only once more.

L4-04 | 4 | MEDIUM | §2 (a Linux person clicking OK with nothing selected)
failure: On Linux no row of the schedule list is selected: the default row is not passed to zenity (the dialog program on Linux), and the "no empty answer" option works only on Windows and macOS. So clicking OK with nothing chosen is a natural move. After the change it installs nothing and says nothing, at the wizard's last step. A person who meant Yes ends with no backup and no word (Type II friction). The repo's own fix for the same empty answer in the mailbox list, MA-1465, shows "Nothing was chosen…" and asks again; §2 cites MA-1465 without adopting it.
evidence: §2 "Any other answer, the empty one included, installs nothing"; third_party/zenity/list_unix.go:7-27; third_party/zenity/list.go:66; internal/wizard/wizard.go:2120-2128, :1924; docs/scenario-catalog.md:1651.
condition: On an empty answer, show MA-1465's note and ask the schedule question again, up to a fixed number of times; Cancel still installs nothing, so R14 holds. Or end with a line saying no scheduled backup was set up and how to add one.

L4-05 | 4 | LOW | §1.3 wording (Type III: wording or tooling that can be built before ship)
failure:
(i) The WARN and the log line give only a raw path such as ".../ImapMail/imap.gmail-2.com", while the picker names accounts as Thunderbird shows them.
(ii) The WARN tells the person who removed the account on purpose that nothing needs doing, and tells the person who lost it by accident nothing.
(iii) "the other mailboxes were backed up" is written at discovery, before the backup, so a run that then fails leaves a false line.
evidence: §1.3 sentences; internal/wizard/picker.go:105.
condition: Name the account when it can still be read; add "if you did not remove it, add the account back in Thunderbird and it will be found again"; write "are being backed up", or write the line at the end of the run.

L4-06 | 4 | LOW | §3.2 "The name before the check"
failure: Two rules overlap. One says the armed delete fires if NameMatches(typed, the plan's name). The other says it does not fire when the folder's name on disk differs from the row's name "in letter case or form". NameMatches already ignores Unicode form, so for a name that differs only in form (a macOS folder name stored one way, the row's path the other), the design says it both fires and does not fire. Either the person is stopped for nothing, or one rule is dead text.
evidence: §3.2; internal/archdelete/execute.go:37-38 (NameMatches folds Unicode form, not case).
condition: Keep one rule: fire only when NameMatches(typed, the plan's name). Drop the second rule, or limit it to case.

SOURCES: Read the design document in full (revision 1 at commit 47f9efd; I did not recompute its sha256), assurance-kit process/design-review.md (the lenses), friction-review.md §1-2, docs/ux-contract.md X14, and the catalog rows MA-103, MA-688, MA-871, MA-890, MA-972, MA-986 and MA-1465. In the code I read: internal/app/run.go (discovery, autoDiscover, the refused-run record), internal/app/sourceloss.go, internal/state/sourceloss.go, internal/state/lastrun.go (RunWarning), internal/state/manifest.go (PathSourceID), internal/state/encrypted_record.go, internal/source/source.go (IsMailStoreDir), internal/wizard/picker.go, wizard.go (offerSchedule, jobFor, AutoPauseIfDue, the pause message), jobrun.go, internal/desktop/localschedule.go, dashboard.go (the rebuild check and the banner), pending.go, archivedelete.go, internal/archdelete/archdelete.go and execute.go (NameMatches, the re-plan and samePlan), internal/health/health.go (run warnings, the source_lost words), internal/alert/alert.go, internal/runlog/runlog.go (log size cap), third_party/zenity/list.go and list_unix.go, and cmd/mailarchive/main.go (:275, prepareThunderbird). From memory, not read: Thunderbird's usual Local Folders layout with *.msf files and POP3 mail going into it, and whether Go timers count system sleep. Not reached: docs/design-archive-delete.md beyond §3.1, deletescript.go beyond its delSeq and data-delete-waiting lines, and whether a backup run that finds no new mail rewrites the manifest (L4-03 relies only on runs that save mail).

---

## Finder report: lenses 5 and 6 (integration, security)

L5-01 | 5 Integration | MEDIUM | §1.3 (record's `Since`, "copied from the previous last-run record")
failure: The design never says when the previous record is read, and the code overwrites it. `beginRun` writes a fresh "running" record at the start of every run, carrying only `opts.Warnings`. `writeRefusedRun` also writes a fresh record. If `Since` is read where warnings are appended today (the end of the run), the "previous" record is the run's own running record. Then `Since` always equals this run's start, the WARN never turns into a note, and P3 is false. Even if `Since` is read early, any refused, crashed or cancelled run in between drops it. Status then shows "not found since DATE" with a later date than the truth, and the 14 days start again.
evidence: Design: "A run copies `Since` from the previous last-run record's `mailbox_gone` warning … otherwise `Since` is the run's start." Code: internal/app/run.go:1364-1367 and :1390 (`Warnings: opts.Warnings`); run.go:1445-1451 (`writeRefusedRun`: new record); run.go:1601-1605 (warnings appended at the end). internal/state/lastrun.go:205-209 documents that a RunWarning "never latches, because the next run … records none". Cross-run memory elsewhere lives in sidecars: internal/app/sourceloss.go:77-80 (source streak) and internal/app/tbcompleteness.go:505-592 (Thunderbird behind-streak record).
condition: Keep each gone input's first-seen date in a sidecar next to the source streak. Key it by `PathSourceID`, write it under the archive lock, and read it before `beginRun`. The RunWarning then carries `Path`/`Since` for display only. At minimum, the design must say `Since` is read before `beginRun` and that the running and refused records carry `mailbox_gone` forward.

L5-02 | 5 Integration | MEDIUM | §1.4 (the Desktop's schedule rebuild drops a gone input)
failure: §1.4 has no version of §1.2(b). When every listed input is gone (a whole profile), the rebuild drops all of them and saves a pure `-auto` job. That job can never again raise `sourceUnreachableError`, only "no input mail sources found", which the streak treats as inconclusive. So there is no 3-day source-loss WARN and no 30-day auto-pause, and a failure email goes out on every run forever. If auto-discovery finds anything else, the runs are silently green with no `mailbox_gone` at all. In the partial case, dropping the input means the next run no longer lists it. Any Settings save (for example, changing the time) then quietly erases the WARN or note.
evidence: Design: "a reconstructed `-input` that is gone … is dropped from the rebuilt job's inputs instead of refusing the rebuild". Code: internal/desktop/localschedule.go:77-109 (today any vanished input refuses); internal/app/run.go:632-634 (`FailNoSources`); internal/app/sourceloss.go:49-66 (only `sourceUnreachableError` and outages count as unreachable).
condition: Keep, rather than drop, a gone input that lies where auto-discovery looks. Still require A2's manifest-store membership (`state.PathSourceID` works on a gone path, internal/state/manifest.go:490-499). §1.2 then governs it at run time. Refuse the rebuild when no listed input exists.

L5-03 | 5 Integration | LOW | §1.2 "Where" (callers of the export's up-front discovery at main.go:275)
failure: On the command line, `discovered` feeds two more checks the design does not mention:
- (i) `FollowLinksRefusal(o.followLinks, discovered)` refuses when a `-follow-links` opt-in names a skipped input. The words are wrong ("is not one of the inputs being archived"), the refusal kind is not source-unreachable (so no streak), and the skip never takes effect for jobs scheduled from the command line, which keep `-follow-links`.
- (ii) `-list` prints the remaining stores and says nothing about the skipped one.

Separately, `export` discovers twice (main.go:275, then app.Run at run.go:631). A command-line summary built from the first list can differ from the record's count, which breaks X6's "same count".

The design's claim that `DiscoverInputs` "keeps its … meaning" is also untrue: with `auto` on, it now drops gone inputs silently for every caller (`prepareThunderbird`, main.go:2088).
evidence: cmd/mailarchive/main.go:275-282; cmd/mailarchive/job.go:266 and :295-297 (`-follow-links` kept in scheduled args); internal/app/run.go:240-254 (`FollowLinksRefusal`).
condition:
- Check `-follow-links` against the listed inputs (or discovered plus skipped), and drop opt-ins for skipped inputs.
- Make `-list` print the "Not found, so skipped" lines.
- Take the command-line summary from app.Run's result.
- Change the design's line about `DiscoverInputs` to say it skips silently when `auto` is on.

L5-04 | 5 Integration | LOW | §1.3 (new `RunWarning.Path` field)
failure: The design cleans `Path` only inside internal/health. The existing choke point that cleans every string read from the record (INS2-2) does not clean the new field. Any other reader of the record (present or future) gets raw control characters from a planted record.
evidence: Design: "builds the words itself from `Path` and `Since`, cleaned of control characters". Code: internal/state/lastrun.go:369-380 (strips only `Code`/`Message` per warning).
condition: Strip `Path` at `ReadLastRun`'s choke point as well.

L5-05 | 5 Integration | LOW | §4 build order (part 1)
failure: The producer of a loss (the skip in `DiscoverInputsSkipping`) can land before its sink (health's `mailbox_gone` entry). Health shows only codes on its allow-list, so in any build or release in between, a skipped mailbox is entirely silent.
evidence: Design §4 step 2 states no order within part 1. Code: internal/health/health.go:778-805 (unknown codes are dropped).
condition: State the order. The record fields and the health reader go in first, or in the same commit as the skip; never a commit where runs skip and status cannot say so.

L5-06 | 5 Integration | LOW | §3.1 (`deletePlanLife`) and §3.2 Cancel
failure: The store has one lifetime field, `pendingSecrets.life`, used by `put`'s timer. That field is the seam MA-972's expiry test uses: it sets `life` to 30 ms and expects a delete plan to expire. A constant `deletePlanLife` that bypasses the field breaks that test or forces it to be rewritten. The design says "MA-972, amended" but does not name the seam, which invites dropping the expiry check (a weakened gate). Also, Cancel only removes the panel and never discards the token, so a cancelled plan (including one whose check ends after Cancel) is held for 24 hours. An existing primitive could end it.
evidence: internal/desktop/pending.go:135-139, :161, :213-217 (`discard(token, is)`); internal/desktop/ma972_delete_token_test.go:73-76; internal/desktop/deletescript.go:251 (Cancel removes the panel only). Design: "its plan waits unused until it ends (§3.1)".
condition: Make each kind's lifetime a store field set from the constants, so MA-972's expiry check keeps its short override for delete plans and MA-1475 drives both. Have Cancel (and a cancelled waiting panel) post to a route that calls `pending.discard(token, isDeletePlan)`.

L5-07 | 5 Integration | LOW | §3.2 "Look before writing (X14)" and §3.2 rows
failure: The design calls this "a scoped exception", but its rows amend only MA-rows. The X14 row in docs/ux-contract.md still says the write follows a shown dry run. The Delete… button's tooltip still promises "You see what would be removed first", which is false for an armed plain delete.
evidence: docs/ux-contract.md:24 (X14); internal/desktop/dashboard.go:2396 (tooltip). Design §3.2's rows list only MA-1476 and MA-1477.
condition: Add an X14 amendment that states the exception's scope with the owner's words, and change the tooltip's text (the friction review owns the wording).

L5-08 | 5 Integration | LOW | §3.2 "Plain" (the list of things that make a plan not plain)
failure: `plain` is a deny-list kept by hand next to `planView`. A later line added to the plan that needs the person's decision would leave `plain` true, and the armed path would delete without showing it.
evidence: Design: "true only when none of these is in the plan: …". Code: internal/desktop/archivedelete.go:441-505 (`planView` keys).
condition: MA-1476 lists every key `planView` emits and classifies each as plain-neutral or plain-blocking. The test fails on any key it has not classified.

L6-01 | 6 Security | MEDIUM | §1.2 (a) and "fs.ErrNotExist, as today"
failure: The place check is lexical, but "gone" uses `os.Stat`, which follows links. Examples: a Thunderbird profile, `~/.thunderbird`, or an account folder that is a link or junction to an external drive; or Windows AppData redirected to a network share. When the drive or share is unplugged or offline, the path reads as gone yet still matches the pattern, so it is skipped while any other mailbox remains. The person gets no notice or email, a WARN for 14 days, then GREEN. The design says this "unplugged drive" case stays caught (§1.2, P2). §1.4 decides the same question with Lstat, so the run skips a dangling link that the rebuild refuses.
evidence: internal/app/run.go:1982-1985 (`os.Stat`, `ErrNotExist`); run.go:2061 (auto-discovery follows links). Design §1.4: "its `Lstat` says it does not exist". Design §1.3: "None for a skip".
condition: Skip only when (i) `os.Lstat(input)` says it does not exist (a dangling link counts as unreachable and refuses), and (ii) the input's parent folder exists now as a folder and, resolved, lies in the same auto-discovery place resolved the same way. Otherwise refuse as today.

L6-02 | 6 Security | MEDIUM | §1.2 (b) ("or one that auto-discovery finds this run")
failure: The design says a whole Thunderbird profile gone still refuses. But rule (b) counts any store auto-discovery finds this run. Every listed input can be skipped while the run "succeeds" on something unrelated:
- a leftover readable Outlook `.ost`, which auto-discovery keeps;
- an Evolution cache folder;
- a freshly created default Thunderbird profile with an empty Local Folders (Thunderbird creating one is recalled, not read).

The streak resets, no email is sent, and after 14 days status is GREEN.
evidence: Design: "When nothing remains (a whole Thunderbird profile gone …), the run refuses as today". Code: internal/app/run.go:2049-2064 (stub `.ost` files kept if readable; any mail-store folder kept).
condition: (b) requires at least one listed input to exist, and stores found only by auto-discovery do not count. Alternatively, L6-01's parent-folder rule, which refuses when the account's profile folder itself is gone.

L6-03 | 6 Security | MEDIUM | §3.2 ("Plain is computed on the server")
failure: `plain` is computed on the server but enforced only by the page. The delete route receives the same token, typed name, unrecorded flag and `drawnFor` for both buttons, so it cannot tell an armed delete from Delete permanently. P7 therefore rests entirely on page JavaScript, which is about to drop the `delSeq` guard on stale replies, and is "proven structurally" only. One page-script slip on a non-plain plan deletes without showing it. That plan could be an encrypted archive whose key line says the key will be forgotten, a folder in a cloud-synced folder, or a re-run finishing an earlier delete. The person typed the name; there is no path without the name, because the server checks it.
evidence: internal/desktop/archivedelete.go:563-640 (`deleteAction` checks only `NameMatches` at :579, then the re-check under the lock); internal/desktop/deletescript.go:282-295 (`delSeq` drop today). Design P7: "Proven structurally by MA-1476 and MA-1477".
condition: The armed request carries `armed: true`. `deleteAction` refuses it, keeping the token and deleting nothing, unless the held plan is plain by the same function `planView` uses. MA-1476 proves that refusal in Go with `assure.Refused`.

L6-04 | 6 Security | LOW | §1.3 (14-day WARN, then a note, from `Since`)
failure: Two ways a loss turns GREEN at once:
- A clock that is behind when the gone mailbox is first seen (for example, after a CMOS battery reset) sets `Since` years in the past. It is copied forward, so the first run after the clock is fixed shows a GREEN note. The design handles only a `Since` in the future.
- A planted record's old `Since` now carries over from run to run. Every other record field is rewritten by each run.
evidence: Design: "A `Since` in the future … counts as the run's start". Precedent for refusing clock-behind counts: internal/app/tbcompleteness.go:530-533 (`tbstreak_clock_behind`, CLOCK-R2-A). internal/state/lastrun.go:205-209.
condition: The note also requires a minimum count of runs that found the input gone, kept beside `Since` in L5-01's sidecar. A `Since` earlier than the archive's first recorded run counts as unknown and keeps the WARN.

L6-05 | 6 Security | LOW | §1.2 (a) ("lower-cased on Windows and macOS")
failure: macOS volumes can be case-sensitive APFS. Lower-casing then widens (a) beyond what `filepath.Glob` would ever find there. A gone input at a differently-cased path is skipped, which contradicts the design's "Why (a)" (it "would back up by itself if it came back").
evidence: internal/app/run.go:2049 and :2060 (`Glob` follows the volume's case rules).
condition: Lower-case only on Windows. On macOS, compare as spelled (both sides come from the same HOME), or check the volume's case sensitivity.

SOURCES: Read assurance-kit/process/design-review.md (lens battery, procedure). Read the design (revision 1) in the picker-review worktree. In that worktree, read: internal/app/run.go (DiscoverInputs, autoDiscover, globs, Run's discovery, beginRun/newRunRecord, RecordRefusedRun/writeRefusedRun, canonicalJob, FollowLinksRefusal, end-of-run warnings); internal/app/sourceloss.go; internal/app/tbcompleteness.go (RecordRunWarning, streak warnings); internal/state/lastrun.go (RunWarning, ReadLastRun choke point); internal/state/encrypted_record.go (allow-list, marshalLastRun); state.PathSourceID; internal/health/health.go:740-805; internal/desktop/pending.go; internal/desktop/archivedelete.go (plan route, planView, deleteAction, deletePlanKey); internal/desktop/deletescript.go (whole delete flow); internal/desktop/dashboard.go (switch discards, button template); internal/desktop/localschedule.go:60-125; internal/desktop/ma972_delete_token_test.go:60-90; the X15 ban at internal/desktop/ma983_delete_panel_test.go:66; internal/archdelete/execute.go (lockAndRecheck, samePlan, key step); the archdelete Kind, Item and DeletePlan types; cmd/mailarchive/main.go:240-330 and :2085-2095; cmd/mailarchive/job.go:240-300; internal/wizard/jobrun.go:340-400 and offerSchedule; internal/presyncjob/hook.go:150-175; docs/ux-contract.md X6, X10, X14, X15. Checked with no finding: the 24-hour token does not widen what is deleted, because the delete re-plans under the lock and compares the removed files, unrecorded copies, schedules and manifest or vault, the key step re-plans at execute, and the typed name is checked on the server. X15 for the new texts is already enforced structurally (no innerHTML in the delete script; attributes go through html/template). Reloads go through `reloadPage`. Recalled, not read: that Thunderbird creates a fresh default profile when its profile folder is gone (L6-02). Not reached: docs/design-archive-delete.md §3.1 and §8.1 text (I relied on the code's re-check instead), the encrypted record allow-list at encrypted_record.go:136-149 beyond the struct shown, and how `status -json` renders warnings.

---

## Finder report: lenses 7 to 10 (failure, migration, cost, honesty)

L7-01 | 7 | MEDIUM | §1.4
failure: When Settings rebuilds a schedule, any gone input inside an auto-discovery folder is dropped. Rule (b) ("at least one mailbox remains") is not applied there. If every listed input is gone, the rebuilt job keeps -auto but lists no inputs. Each run then fails as "no input mail sources found" (FailNoSources). That failure is not the typed source-unreachable error, so the streak treats it as inconclusive: no 3-day source-loss warning and no 30-day auto-pause. This breaks P2 for rebuilt jobs. If only some inputs are gone, the dropped ones are no longer listed, so no mailbox_gone warning is ever recorded for them. The person is never told (P3 fails), and Since is lost.
evidence: §1.4 "a reconstructed -input that is gone … and that lies where auto-discovery looks (§1.2 (a)) is dropped from the rebuilt job's inputs". run.go:632-633 (zero files → FailNoSources). app/sourceloss.go:49-66 (only sourceUnreachableError or an outage counts as unreachable). localschedule.go:87-106 is the loop this changes.
condition: Keep the gone input in the rebuilt job instead of dropping it, still requiring it to be one of the archive's own manifest stores (the ids check works on a missing path). The run-time rule then skips it and warns. Alternatively, apply (b) to the rebuild and refuse when nothing remains.

L7-02 | 7 | MEDIUM | §1.3 (Since carry) – OMISSION of when the previous record is read
failure: "A run copies Since from the previous last-run record" does not say when that record is read. beginRun overwrites last-run with a "running" record at run start. That record carries only opts.Warnings. A naive read at record time therefore sees this run's own record, so Since is always "this run's start" and the WARN never turns into a note. A run that crashes, is cancelled, or is refused after discovery also leaves a record without the warning unless the warning is in opts.Warnings, so Since starts over and the "not found since DATE" date moves forward.
evidence: run.go:743 `begun := beginRun(opts)`. run.go:1364-1367 (writes the running record). run.go:1390 (`Warnings: opts.Warnings`). run.go:1445-1458 (refused record built from newRunRecord). Discovery runs at run.go:631, before both.
condition: State that the skip warnings, with Since resolved from the record read before beginRun, go into opts.Warnings right after discovery, so the running, refused and final records all carry them. MA-1472 then covers a killed run and an intervening refused run both keeping Since.

L7-03 | 7 | LOW | §1.3 ("never hidden by a wrong date")
failure: Only a Since in the future is guarded. If the clock was unset at the first-gone run (a date before 2024), that Since is carried forward on every run. Status shows a GREEN note at once, and the WARN is hidden by a wrong date, which the section says cannot happen.
evidence: §1.3 "A Since in the future … counts as the run's start: … it is never hidden by a wrong date". The codebase already guards this case for the streak: state/sourceloss.go:95 (MinPlausibleTime) and :78 (MA-891).
condition: Treat a Since before state.MinPlausibleTime the same as a future one: use the run's start, and never carry it forward.

L7-04 | 7 | MEDIUM | §3.2 (armed delete), fail-closed OMISSION
failure: Both safety conditions of the armed delete live only in the page script: "plain" and "the reply goes to its own panel". The server's delete route checks only the token and the typed name. Folder names often repeat across drives (two archives both called "MailArchive", for example). If a reply reaches the wrong panel, which is exactly the routing this design changes (delSeq), the server deletes an archive whose plan was never shown. The same happens if a script bug fires on a plan that is not plain. Nothing on the server fails closed.
evidence: §3.2 "Its check's reply is handled by its own panel even after a later Delete…"; P7 is proven only structurally. archivedelete.go:563-610: the route takes Token, Typed, Unrecorded and DrawnFor (the archive the page was drawn for, not the row), and the only check is NameMatches at :579.
condition: The armed request sends `armed:true` and the row its panel was opened for. The server refuses, keeping the plan, unless it.deletePlan.row equals that row and `plain` recomputed from the held plan is true.

L7-05 | 7 | LOW | §1.2 / §1.3 (command line)
failure: Command-line export with `-auto -input X -follow-links X`, where X is gone from an auto-discovery folder: X is skipped, so FollowLinksRefusal refuses the whole run with "-follow-links X is not one of the inputs being archived". The message is misleading, and it is not a source error, so the streak treats it as inconclusive.
evidence: main.go:275-279 (the check runs against the discovered list). run.go:240-254.
condition: An opted-in input that was skipped is not a follow-links refusal: check against the listed inputs minus the skipped ones.

L7-06 | 7 | LOW | §1.2 (a)
failure: Folding case on macOS makes a path that only differs in case match the auto-discovery pattern on a case-sensitive APFS volume. Auto-discovery never looks at that path, so it is skipped instead of refused. This contradicts the stated reason for (a).
evidence: §1.2 "lower-cased on Windows and macOS, whose file systems ignore case". The patterns come from os.UserHomeDir() plus fixed folder names (run.go:2081-2117).
condition: Fold case only on Windows, or only when the volume reports it ignores case.

L7-07 | 7 | LOW | §1.3 (record fields)
failure: The new Path field is cleaned only in internal/health. The record's single cleaning point (ReadLastRun, INS2-2) cleans Code and Message only, so any other reader of the record gets Path with control characters intact.
evidence: §1.3 "builds the words itself from Path and Since, cleaned of control characters". lastrun.go:371-380.
condition: Clean Warnings[i].Path in ReadLastRun beside Code and Message.

L7-08 | 7 | LOW | OMISSION in §1.3 surfaces
failure: The result note of a plaintext archive's Desktop Run now is not on the list of places that report a skip. The person who clicks Run now sees a normal result and is told nothing on that screen. This breaks X6 ("the GUI summary … carry the same counts").
evidence: §1.3 lists the run log, record, status and health view, the wizard summary, the command-line summary and encrypted results. RunJobInProcess returns runNowSummary (jobrun.go:72-100).
condition: Add the plaintext Run now note to §1.3 and to MA-1471.

L8-01 | 8 | MEDIUM | §1.3 ("recovers on its first 1.7.5 run")
failure: An archive that 1.7.4 already auto-paused has no scheduler entry, so no 1.7.5 run ever comes. Its auto-pause words tell the person to "reconnect the source, then click Repair", which they cannot do for an account they removed. Nothing in 1.7.5 tells them that Repair alone now works.
evidence: §1.3 "An archive that was failing because of a gone mailbox recovers on its first 1.7.5 run". pause.go:124-147 (removes the live entry, keeps the job file). wizard.go:392 (the auto-pause words).
condition: Say that an auto-paused archive needs Repair. Put it in the release notes, and have the auto-pause words for an -auto job say that this version backs up the remaining mailboxes and Repair restarts the schedule.

L8-02 | 8 | LOW | §1.3 / OMISSION (streak under way at upgrade)
failure: The first 1.7.5 run succeeds with a skip, which resets the source-loss streak and erases its first-unreachable date. Since then starts at the upgrade run. "Not found since DATE" is later than the truth, and a mailbox gone for 25 days gets a fresh 14-day WARN.
evidence: state/sourceloss.go:64-69 (a reached run clears FirstUnreachable). app/sourceloss.go:86-104.
condition: When the streak's last kind is local-source-unreachable, start Since from its FirstUnreachable, read before updateSourceStreak runs. Or word the date as "first noticed".

L8-03 | 8 | LOW | OMISSION (release notes and mixed versions)
failure: §4 has no release-note step, but several changes need one:
- Command-line scripts using `-auto -input` now exit 0 where they used to refuse.
- Auto-paused archives need Repair (L8-01).
- After a downgrade, 1.7.4's status shows nothing for mailbox_gone (only codes it knows are shown) while its runs refuse again.
- A 1.7.4 Desktop next to a 1.7.5 scheduled run (or the other way round) shows disagreeing signals.
- Delete plans now last 24 hours.
evidence: health.go:778-805 (unknown codes are ignored). §4 items 1-5.
condition: Add a release-notes item to §4 that names these.

L9-01 | 9 | MEDIUM | §3.1 / §3.2 ("its plan waits unused until it ends")
failure: A held plan contains one entry per file to remove, plus the kept list, litter, folders and store folders. For a large archive that is the archive's whole file list held in Desktop memory. By design, Cancel during the check leaves the plan for 24 hours instead of 10 minutes, because no route discards it. Several rows multiply this in a process that runs for a long time.
evidence: archdelete.go:149-209 (DeletePlan.Remove []Item and the other lists). dashboard.go:414-416 (only plan, delete and dismiss routes). pending.go:161 (only the timer ends it).
condition: Cancel, and the reply of a cancelled check, discards its plan through a kind-filtered discard route. The 24 hours applies only to plans a panel still shows.

L9-02 | 9 | LOW | §3.1 / §4 item 4
failure: Delete runs a full new plan under the lock, the same slow walk of the folder and checksum of the manifest. Every large delete therefore pays for the check twice, and an armed delete pays twice in a row. A plan held across the 2 AM backup learns it is refused only after that second walk, and adds started and refused lines to deletions.log. The lab's "large archive" has no size and no clean-up step.
evidence: execute.go:340 (`again, err := Plan(p.Folder, deps)`). archivedelete.go:777-783.
condition: State the double cost. Size the lab archive so the check takes minutes, and add its removal to the lab clean-up.

L10-01 | 10 | LOW | §1.1 last bullet, §4 item 4
failure: "So Settings cannot save that archive's schedule again" is too broad. An edit uses the job file and never calls the rebuild. The rebuild runs only for a new schedule, a Repair whose job file is missing, or drawing the create form. As a result, the lab step "Settings saving the schedule" passes on 1.7.4 for an archive that has its job file, and proves nothing about MA-1473.
evidence: localschedule.go:186-197 ("An edit needs only the securely readable job file … a source drive away … does not stop it"; the rebuild runs only `if !exists`). dashboard.go:1661-1670.
condition: Narrow the sentence. Run the lab step on an archive whose schedule was removed (no job file).

L10-02 | 10 | LOW | §1.2 "Where"
failure: "DiscoverInputs keeps its signature and meaning" contradicts "DiscoverInputs calls it and drops skipped". Its meaning does change: prepareThunderbird and the command line now skip silently.
evidence: main.go:2087-2091.
condition: Say that DiscoverInputs now skips silently.

L10-03 | 10 | MEDIUM | §3.1 "Why this is safe" and §6 (OMISSION)
failure: The re-check list does not match the code.
- The code re-plans in full and compares (samePlan): folder identity, kind, rerun, the encrypted-archive record, the gone-note, the anchor file (manifest or vault), the names to remove, the unrecorded names, and the schedules.
- It does not compare: synced, paused, kept, held files, litter, folders, or the captured values.
- The design's list leaves out the names-to-remove comparison, which is the check that actually backs "it can never delete more", and does not say the re-check repeats the slow walk.
- That safety claim is not in §6, is not labelled proven or conditional, and no row proves it. MA-1475 covers only the plan's life.
evidence: execute.go:318-352 and :372-378. design-archive-delete.md §3.1 step 2.
condition: Restate the re-check from samePlan. Narrow the claim to "never removes a name the plan did not list". Add it to §6 as proven by the named existing re-check rows plus an MA-1475 case where a plan outlives a backup and is refused.

L10-04 | 10 | LOW | §3.2 panel words
failure: "MailArchive deletes this archive as soon as the check finishes" is not what happens: after the check, Delete runs the full check again under the lock before removing anything.
evidence: archivedelete.go:563-610 and :777-783. execute.go:340.
condition: Word it as "checks once more, then deletes", and keep the minutes counter running.

L10-05 | 10 | MEDIUM | §3.2 "Look before writing (X14)" and §0
failure: "This is a scoped exception the owner asked for" says more than the record shows. The owner's words are a question about skipping the check ("maybe add an option to forgo the check and delete immediately?"). The session's counter-proposal (keep the check, skip showing the plan) has no recorded owner acceptance. The X14 row is not listed for amendment.
evidence: §0 bullets 2-3. ux-contract.md:24 (X14).
condition: Record the owner's ruling on the scoped X14 exception word for word, or mark it as an owner ruling still pending. Amend the X14 row.

L10-06 | 10 | LOW | §3.1 (when a delete plan ends)
failure: The list is incomplete. A plan also ends when this computer forgets an encrypted archive's record, and when a delete or Remove from list clears the current archive (afterSwitch ends every delete plan).
evidence: encryption_forget.go:151-155. dashboard.go:719-727.
condition: Complete the list.

L10-07 | 10 | LOW | §2
failure: "The wizard's other lists already fall to their safe default" cites 5 of the 8 list dialogs. Not cited:
- "What would you like to do?" (wizard.go:675)
- "What are you exporting?" (wizard.go:780); an empty answer reaches the default branch, a file chooser (wizard.go:829-830)
- Deleted Items and Junk (wizard.go:1017)
"The build re-checks" puts off something that can be read now.
evidence: grep of zenity.List in internal/wizard.
condition: List all eight dialogs with what an empty answer does in each.

L10-08 | 10 | LOW | §1.3 ("for any reader that does not know the new fields")
failure: No such reader exists. The only reader of LastRun.Warnings shows defined codes only, so 1.7.4 and older show nothing for mailbox_gone, and the Message is never seen.
evidence: health.go:778-805 (the only non-test reader).
condition: Drop that reason, and say that a downgraded status shows nothing for this warning.

L10-09 | 10 | LOW | §6 P3
failure: P3's encrypted-archive condition is a reference to §1.3, not stated beside the claim. It can also be settled now: the encrypted Run now result already carries extra lines held only in memory, and "nothing of it is written beside the archive".
evidence: jobrun.go:291-321 (encryptedSummaryFor, tbNoteLines).
condition: State the condition inline, or settle it citing this precedent.

L10-10 | 10 | LOW | §1.3 (14-day WARN)
failure: The design does not say what the 14 days are counted against: the time status runs, or the recorded run. The existing source-loss thresholds are, by owner ruling, never compared with the current time.
evidence: state/sourceloss.go:23-28.
condition: Say which.

L10-11 | 10 | LOW | §1.2 ("When nothing remains (a whole Thunderbird profile gone …)")
failure: The example holds only when the archive has no other listed or discoverable mailbox. A combined archive that also lists another program's store (Evolution's local store, or a PST in Documents\Outlook Files) skips a whole lost Thunderbird profile one mailbox at a time and never moves the streak.
evidence: run.go:2069-2117 (Outlook, Thunderbird and Evolution patterns together).
condition: Word it as "when no listed or discovered mailbox remains".

SOURCES: Read the design at worktree HEAD 47f9efd (I did not compute the sha256), design-review.md, design-archive-delete.md §3.1, and the ux-contract X6, X10 and X14 rows. In the code I read:
- internal/app: run.go, sourceloss.go
- internal/state: sourceloss.go, lastrun.go, encrypted_record.go, runhistory.go
- internal/wizard: wizard.go, jobrun.go, pause.go, picker.go, listtext.go
- internal/desktop: localschedule.go, localarchive.go, dashboard.go, pending.go, archivedelete.go, runnow_keyed.go, schedule.go, encryption_forget.go, deletescript.go, pages_scripts.go
- internal/archdelete: archdelete.go, execute.go, record.go
- also: internal/health/health.go, internal/runlog/runlog.go, third_party/zenity list_unix.go and util.go, cmd/mailarchive/main.go, catalog rows MA-103, MA-397, MA-688, MA-972, MA-1465, and commit bc6e9d0
I checked every code citation in the design and none is wrong. jobrun.go:368-370 points at optsForJob's `Unattended: true`, which is a fair basis for "Run now does the same". pending.go:161 uses the store-wide `p.life`, so a per-kind life needs a change to put(); that is not a citation error.
I found nothing for:
- run-log growth (rotated at 8 MiB)
- the per-run read of the previous record (capped at 4 MiB)
- 1.7.4 reading 1.7.5 records (plain json.Unmarshal ignores the new fields)
- the job-file format (unchanged)
- the encrypted run log (Run now logs to the Desktop's activity log; the command line's -log stays outside the archive)
Not reached: the page-script test harness and how the page routes check replies (only the existing delSeq and delClose code), whether the Desktop server has a write timeout on long plan requests, and how the Thunderbird sync works out its profile when an input is skipped. Nothing was run.
