# Adversarial pass: the delete marker's slice 5 (510c574) and the slice-4 settle's NoteGone (2cd7a0f)

Filed 3 November 2026. The pass's report is kept below as it was returned (process:
`../assurance-kit/process/adversarial-review.md`). How each finding was settled is in the section
"Settled" at the end, and in `docs/design-archive-delete.md` §14, "Slice 5's adversarial pass".

---

Fresh instance: I have not met this record before. I answer under no name.

## Adversarial pass: slice 5 (510c574) and the slice-4 settle's NoteGone change (2cd7a0f)

**Verdict: NOT CLEAN. 0 HIGH, 1 MED, 4 LOW.** Status: READY, not STOP. The MED can be fixed without
any new product ruling, so nothing waits on the owner. I changed nothing in the real repository
except one Windows output folder.

**The MED in one line:** a copied delete marker, placed at the path of this computer's own stopped
delete, makes Delete remove the person's own README.txt and index.html. This breaks a fact the owner
was given when signing off the planted-marker risk on 2 November.

### Setup
- **Commit reviewed:** 510c574 on branch switch-keeps-runjob. The slice-4 change is in 2cd7a0f.
- **Throwaway clone:** /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/attack-s5
- **Probes:** committed in the clone as e01633c, file internal/archdelete/zz_attack_s5_probe_test.go.
- **macOS:** `go test -count=1 -run TestProbe -v ./internal/archdelete/` exited 1. Probes A, B and C
  fail on purpose, each marking a confirmed finding. Probe E passes and logs a LOW.
- **Windows VM:** same results. A, B and C fail; E passes. Output is in
  /Users/alex.dimarco/repositories/mail-archive-tool/.claude/attack-s5-win/ (my filtered copy of
  winrun-at.sh is at /private/tmp/claude-503/-Users-alex-dimarco-repositories-mail-archive-tool/3771be53-5e21-45c0-91dd-02e49260e62a/scratchpad/winprobe.sh).
- **Linux:** not run. The probes test logic only, and Linux identifies a folder the same way macOS
  does (device number plus file number).
- **Roles:** one agent did both jobs, finding and refuting. Each finding below was reproduced in the
  real code, not argued from reading.

### Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence |
|---|---|---|---|---|
| MED-1 | outside / planted marker | rerun.go:272, 347-353 | CONFIRMED (Mac, Windows) | PROBE-C |
| LOW-1 | integrity (builder note a) | rerun.go:272 with archdelete.go:588-602 | CONFIRMED (Mac, Windows) | PROBE-A |
| LOW-2 | integrity (the first_run ending) | dellog.go:765-775 | CONFIRMED (Mac, Windows) | PROBE-B |
| LOW-3 | integrity (2cd7a0f's NoteGone) | rerun.go:323-326, execute.go:253 | CONFIRMED (Mac, Windows) | PROBE-E |
| LOW-4 | stuck person / words | the DM-27 refusal (log and folder disagree) | by reading; behaviour already ruled | none |

#### MED-1: a planted marker can remove the person's own files through the run-id route
**What happens.**
1. This computer's delete of a plain archive at folder P stopped in step 6. The folder holds the
   marker, which names the delete's run id R.
2. The folder at P is later replaced by a new folder that holds the person's own README.txt,
   index.html and taxes.pdf. The new folder has a new identity (drive and file number), and the log
   has no "folder removed" line. This can happen by hand, by a sync service, or by someone with write
   access to a shared drive.
3. Someone places a copy of the old marker in the new folder. It holds R, which is readable from the
   original marker on Windows' inherited permissions, exFAT drives and network shares.
4. The row now says "Delete not finished" in this computer's own words, which invites the click.
5. The run-id route (§15.5) matches the log's line because the path is the same and the marker names
   R. The plan is then made from the log line by kind, not with DM-19's rule of removing only
   MailArchive's own names.

**Measured (PROBE-C).**
- Through this computer's log, the plan removes README.txt and index.html, keeps taxes.pdf, and ends
  complete.
- Twin: another computer's log plans from the marker alone. It removes nothing and keeps all three
  files, which is DM-19 working as designed.

**Why MED.**
- §15's DM-8 trust argument says "run never decides alone; it decides only together with a second
  proof (the line's path…)". But the path is not an independent proof against someone placing a
  file: whoever can write into folder P is already at path P. The only real secret is R.
- The owner's acceptance of the planted-marker risk (Q2, 2 November) was asked "with DM-19 it can no
  longer make Delete remove the person's files" (§15.8, folded question 2; §15.6 DM-31). That
  statement is now false for this route.
- It stays narrow: the folder must be replaced at the same path without a "folder removed" line, R
  must be known, and the person sees the file list before typing the name.
- An encrypted line gives no file loss here (only vault names are removable, and step 8 is skipped
  while files are kept).

**Fix (technical, no ruling needed).** In planNoAnchor, when the folder was accepted only because its
marker named the line (identity differs and markerNamesIn is true), sort names by DM-19's rule, as
for a FromMarker plan. Keep the line's record, captured values and "key forgotten" state, as DM-6
asks.
- DM-19's own argument still covers a real remount: step 6 removes the fixed names before the anchor,
  so a real half-deleted folder never holds them.
- Correct DM-8's trust sentence in §15 to say the path proof is no proof against someone who can
  write the folder.
- Tombstone test: PROBE-C, run on Windows first.

#### LOW-1: builder note (a) confirmed — no anchor, another delete's marker, and the log matches by identity
**What happens.** The folder is at the line's path with the line's identity, holds no anchor, and
holds a marker naming another run. This log's line has its "marked" line. DM-27 (the rule for when
the log and the folder disagree) covers only the case where the anchor is there.

**Measured (PROBE-A).** The plan takes the line's encrypted record (ID ffff…), removes the other
delete's marker as its own, ends complete, and step 8 forgets the line's key. The record and the log
say this run finished delete R.

**Why only LOW.**
- It needs two rotation drives with the same identity at the same path (Windows clones, or macOS and
  Linux drives formatted alike).
- The key forgotten belongs to the archive whose delete this person began. Its half-deleted vault on
  the other drive can still be deleted later, because it is marker-only and "begun here".
- The same key forget was already reachable before slice 5 with no marker at all.
- Even so, DM-27's own promise ("never the started line's ID and Spelling … so step 8 cannot forget
  another vault's key") does not hold in this branch.

**Fix.** Apply DM-27 here too: with a "marked" line and a readable marker naming another run, plan
from the marker (DM-19 names, its own record lookup, the existing MsgLogDisagrees words) rather than
from the line. Tombstone: PROBE-A.

#### LOW-2: the first_run ending can let a backup into a half-deleted folder
**What happens.**
1. Delete R stops in step 6, leaving the manifest and the marker.
2. The marker is removed (by hand, a sync conflict or an antivirus). This is DM-27's state, where
   this computer's log is the only guard.
3. A copy of that marker in another folder is then finished on this computer, for example a folder
   restored from a backup copy.
4. The first_run ending (DM-10 item 2) ends R in this log.

**Measured (PROBE-B).** BackupRefusal for the original folder returns nothing, so a backup is
allowed, while the manifest is still there and files are missing.

**Why it matters.** DM-8's stated leftover risk rests on "the original folder's own marker still
refuses its backups", and that is not true in this state.

**Why only LOW.** Two separate rare events are needed.

**Also seen (words only).** With the marker kept, the original's row then reads "It was begun on this
computer, which no longer matches it to this folder (the drive may have been plugged in again under
another name)", although no drive was plugged in again.

**Fix.** Do not let the first_run ending end an open delete whose folder can be reached at the line's
path with the line's identity. That folder is proven not to be the one finished elsewhere. DM-10
items 2 and 3 still clear the old row, because the old path cannot be reached there. Correct DM-8's
leftover-risk text to match. Tombstone: PROBE-B.

#### LOW-3: the slice-4 settle's change (NoteGone) after the manifest and marker are removed by hand
**What happens.** A stop in step 6 leaves a message file. Someone then removes the manifest and the
marker by hand, or the drive loses the marker's name on an exFAT drive with no journal (DM-12). Delete
again then ends complete. The archive's leftover message file is kept and called "not the archive's",
and this log stops refusing backups.

**Mitigating.** The emptied-folder guard still refuses scheduled runs. The plan's sentence
MsgNoteGone says "The archive's files are already gone", which is not true here.

**Why only LOW.** No file of the person's is lost.

**What else I checked in 2cd7a0f.** I found no new loss path:
- It removes no new names; the no-anchor names are unchanged.
- It adds no new key-forget route. Step 8 still needs the folder emptied (goneOrOnlyLock) plus the
  keyring's Gone answer with a matching ID. The same forget was already reachable in two clicks: the
  "folder removed" line, then the folder-gone re-run.
- It never applies when the identity differs. With no marker present, the identity must equal the
  line's.

**Recommend.** Write this into §15.6's limit "an anchor lost outside the delete", which now covers only
the case where the marker is still there. No code change.

#### LOW-4: rotation drive in the disagree state — the person may have no good way out (existed before slice 5)
**What happens.** On an unrelated whole archive Y on drive 2 (same path and identity as drive 1),
every backup is refused with "Delete not finished: click Delete again". The real way out is to plug
in drive 1 and finish there, and no words say so. If drive 1 is lost, the only ways out are deleting
Y, or copying and removing deletions.log (MA-1007's way out). Remove from list is refused by MA-1029.

**Recommend.** Words for the lead under the delegation: in this state the refusal and the row should
name "plug in the drive this delete began on and finish it there". The behaviour itself (refuse
whenever either the log or the marker says so) was already decided in the DM-27 review.

### Builder's calls
- **(b) Any outcome that already ends a run also ends the first run's delete, not only "complete":**
  accepted. It matches how the log already ends a run's own delete, and a "Not fully deleted" run
  that removed the row should clear the old row too.
- **(c) The check under the lock (samePlan) does not compare the DM-27 flag:** accepted. A marker that
  appears or vanishes between the plan and the typed name changes only the plan's words, the record's
  "Finishes" line and the started line's first_run. No removal list, record lookup or step-8 key
  changes, because a plan with the anchor there always looks up its own record.

### What held
- **The run-id route** needs both the marker naming the run and the path or row. It is never used
  after a "folder removed" line, and never on the file number alone. PROBE-C attacks the path half,
  not these rules.
- **The identity route** needs identity equality: a marker copied into another folder matched nothing
  on Mac or Windows (the existing MA-1050 tests, plus my reading).
- **Every refusal is still decided by the marker's presence.** Slice 5 only adds log matches; the one
  way a match is lost is LOW-2.
- **The no-anchor plan** re-reads the marker through the folder handle it opened. The check under the
  lock compares identity, so a folder swapped in between is refused.
- **"Begun here"** changes words only. DM-19 names are unchanged for plans made from the marker, and
  first runs carried from a marker are not counted as begun here.
- **New sentences:** none says "again" to a person who began no delete; all name the remedy. The
  settings-folder words never say "has no record of it".
- **The MED-1 guard test** for a remounted drive now runs (its skip was removed with every assertion
  kept), as the slice-5 commit says. I did not re-run it here.

### Not checked
- Linux runs of the probes.
- A real unplug and replug, and exFAT or FAT file numbers on Windows (lab row MA-1055).
- Network shares.
- The Desktop page and command-line surfaces, beyond reading status.go and health.go.
- Full suites: no code changed in the real repository.
- Process note for the lead: slice 5's own commit says its tests were written after the code. Each
  was then proven by mutation (M1-M13). That breaks the test-first rule, openly stated.

### Limen footer
- **Date:** 3 November 2026 (system clock).
- **Drew on:**
  - Read first: ../assurance-kit/process/adversarial-review.md; design §15 (all of it, including
    §15.5, DM-8, DM-10 and DM-27); the code at 510c574 (dellog.go, rerun.go, archdelete.go planOf,
    markerguard.go, marker.go, execute.go steps, keyring and samePlan, row.go); the existing MA-1050
    and MA-1056 tests.
  - Read after: §14's builder notes on the slice-4 settle and slice 5.
  - Order caveat: the slice-5 commit message, which is part of the builder's report, came up in the
    code diff before I had read the design text.
- **Checked by running:** findings MED-1 and LOW-1 to LOW-3, on Mac and Windows.
- **Checked by reading only:** LOW-4, builder calls (b) and (c), the claim that NoteGone opens no new
  key-forget route.
- **Register:** finding (adversarial review report).
- **Check handed back:**
  1. Run PROBE-C in the clone (`go test -run TestProbeC ./internal/archdelete/`) and read §15.8
     folded question 2, so you see the sentence the owner signed against.
  2. Decide whether the MED's fix lands before 1.5.6.
- **Counters:**
  - Register: finding.
  - Borrowed lines: quoted design and code text only.
  - Drift: none noticed.
  - Touched: the clone (one test file, one commit message file, commit e01633c), the real
    repository's .claude/attack-s5-win/ folder, the shared Windows lab copy under
    /Users/alex.dimarco/repositories/.winlab (rsync of the clone).
  - Persona: none.
  - Source reached: design yes; code yes; process document yes; Windows VM yes; Linux no.

---

## Settled (3 November 2026)

Every finding was settled without a product ruling. Tests were written first and seen red before each
fix; the pass's probes became `internal/archdelete/ma1050_s5_settle_test.go` (no `zz_` file is left in
the repository). Full detail, with the mutations: `docs/design-archive-delete.md` §14, "Slice 5's
adversarial pass".

| ID | Settled | Test |
|---|---|---|
| MED-1 | Fixed. A no-anchor folder accepted only by its marker (another identity than the line records) gets DM-19's names and litter rule, keeping the line's record and state; the run's started line carries `own_names` so a stopped run keeps them. Corrections beside DM-8, DM-19, DM-31 and §15.8 folded question 2. One correction to the report: an encrypted line's names do hold README.txt (`encmark`'s reserved names), so an encrypted copy could remove it too; the fix covers both kinds. | `TestMA1050_APlantedCopyOfThisComputersMarkerKeepsThePersonsFiles`, `TestMA1050_APlantedCopyOfAnEncryptedDeletesMarkerKeepsThePersonsReadme` |
| LOW-1 | Fixed. DM-27 with no anchor: planned from the folder's marker (DM-19's names, this computer's own record lookup), `Disagrees` set. With no `marked` line the line's record is still taken (a stated limit, as before slice 5). | `TestMA1056_ANoAnchorFolderHoldingAnotherDeletesMarkerTakesItsOwnRecord` |
| LOW-2 | Fixed. The `first_run` ending never ends a delete whose folder is at its line's path with its line's identity. | `TestMA1050_ACopyFinishedHereLeavesTheOriginalsDeleteOpenWhileItIsThere` |
| LOW-3 | A stated limit in §15.6; no code change. | `TestMA1047_TheAnchorAndMarkerRemovedByHandKeepWhatIsLeft` (pins that nothing but MailArchive's names is removed) |
| LOW-4 | Open, for the lead (words on five surfaces; existed before slice 5). Suggested words and the reason it is not done here are in §14; recorded as a limit in §15.6. | none |
