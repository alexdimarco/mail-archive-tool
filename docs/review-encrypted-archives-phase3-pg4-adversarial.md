# Review — PG4's adversarial pass (program copies, the front ends)

**Reviewed:** commit 99f46a4 (PG4, MA-847) against 9aeaf43, on a frozen copy, 2026-10-27.
**Design:** docs/design-encrypted-archives-phase3-program.md (§5, §6, §8, §11: C16, C18, R20, R21).
**How:** five lenses — SEC (Desktop security), DUR (durability and crash), LEAK (keyless leak
budget), COR (correctness), CONC (concurrency in the job slot) — then one skeptic told to refute
each finding, then a completeness critic (workflow run wf_23ae73a9-480; 7 agents).
**Tallies:** 17 found, **17 confirmed** by the skeptic (several regraded), none refuted; the critic
found one more (CMP-1). Many are the same defect seen through different lenses: ten distinct.
**Fixed in:** MA-848.

| Finding(s) | Graded | What | Disposition |
|---|---|---|---|
| DUR-1, COR-2, CONC-1 (first half) | MED | "Skip the program download" killed the download it said carries on: the download ran under the job's own context, cancelled when the job returned | **Fixed.** `Options.ProgramDownload`: the Desktop passes its own life context, so a skipped download carries on; Cancel ends the job, Shut down ends both. Test: the stalled request is not cut off when the run and its context end |
| CONC-1 (second half) | — | a wait that ran out is not retried that day in the Desktop | **Kept as designed:** R17 counts a run-out wait as that day's attempt, as the CLI and the schedule do |
| DUR-2, LEAK-2, COR-1 | MED | verify -full during an unfinished update called the program's own new note and copies tampered ("do not follow", "do not run") | **Fixed.** While the intent stands, the note before or after it, each copy as it was or as it is coming, and a copy whose removal was under way, gone, are the program's own (R5, R7) |
| COR-3 | MED | verify -full never looked at files the set does not name, nor at sealed copies with keeping switched off | **Fixed.** A file no entry names is told by name; sealed copies are compared whether keeping is on or off |
| DUR-3 | LOW | the card switch (and the pause note, before PG4) rewrote and sealed a changed README.txt, its warning thrown away | **Fixed.** A setting's keyed change leaves the note to the next backup, which tells it |
| SEC-1, DUR-4, COR-4 | LOW | the create box's choice was lost if the first backup could not start or was cancelled before its first save; Run now never carried it | **Fixed.** The run job carries it (`job.Job.ProgramCopies`); a run seals it only while no choice is sealed, never over a later change on the card |
| LEAK-1, COR-5 | LOW | with copies switched off, status said copies were "as they should be", then "updated" when they were removed | **Fixed.** New outcomes: removed (said once), not removed (a warning), none kept (nothing said) |
| LEAK-3 | LOW | a run whose program step did not run wiped the last program line | **Fixed.** The running record carries the last line; only a step that ran replaces it |
| LEAK-4, COR-7 (first part) | LOW | the reader's note for a set a newer MailArchive wrote lacked the "copies may be kept" section the note on the drive has | **Fixed.** One function (`EncryptedNoteOf`) writes both |
| COR-7 (second part) | LOW | an invalid set is called "set up by a newer MailArchive" | **Kept:** the set is sealed inside the authenticated header, so in practice it is a newer writer or a bug; the wording is the safe reading |
| COR-6 | LOW | the card reassured while an update was unfinished or a copy unchecked; R4's lasting "not checked since" line was never shown; a self-copy was never marked unchecked | **Fixed.** Status: a lasting "kept online only, not checked" line after a second backup finds it so; the card: the unfinished update and the day each copy was last checkable; self-copies are marked like release copies |
| CMP-1 (critic) | LOW | the Desktop's Cancel was told as "Capture failed" | **Fixed.** The person's Cancel is told as a cancel |

**Not killable from the Desktop's tests** (recorded in PG4's commit, unchanged): the line that
hands the skip, the wait callback and the download context to Run now's and the create's options
— a test build is never a release, so no download starts there.
