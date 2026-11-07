# Fold check: Restore to Thunderbird, revision 2 (commit 99c526c)

**Verdict: FAIL, narrowly.** Nothing needs re-designing. The fold answers 20 of the 24 conditions
in full, and it applies the lab's findings, except the one noted below. But two MED conditions are
only partly folded (RT-4 crash recovery, RT-5 the backup skip). The review's own rule says build may
not start, slice R0 included, until every HIGH and MED condition is folded
(`review-restore-thunderbird-10lens.md:8-9`). Five short text fixes (M1–M5, §3) close it. A re-check
of just those notes is enough; a full re-verify is not needed.

- Date worked on: 6 November 2026 (the session's date).
- Checked: `docs/design-restore-thunderbird.md` at 99c526c, against the review (bd9a8ac) and the
  Windows lab note (28c19a4). `git diff --numstat bd9a8ac 99c526c` gives 909 lines added and 0
  removed, so the reviewed revision 1 text is intact under the notes, as the status line says.
- Register: a verification finding. The judgements marked "recommend" are opinion.
- Read-only: no code was run, no Thunderbird was run, the design was not edited.

## 1. Per condition

FOLDED = concrete enough to build from, and it fixes the failure the review named.

| Id | Sev | Result | Note |
|---|---|---|---|
| RT-1 | HIGH | FOLDED, with a contradiction (M5) | No folder rename: `Mkdir` of the final `.sbd` first, the parent file `O_EXCL` last, W-TAKEN (§3.5, §5.4 note steps 2 and 8, §14). That fixes the Windows failure (lab C). But step 4 still writes each mbox under a hidden working name and renames it into place with the file helper. That is exactly RT-1's "lone `.sbd`" fallback, which the same note says is "dropped". |
| RT-2 | HIGH | FOLDED | Windows `CreateFileW` share 0, `OPEN_ALWAYS` (lab B). Unix: a POSIX record lock only (OFD on Linux, `F_SETLK` plus a sole-opener rule on macOS), no symlink. Checks run before the take, a failed take counts as running, and only B is re-checked after. MailArchive restore lock read by presync (`lockfile.Held` exists, `lockfile.go:176`). Release on every path (MA-1127). Its settings-folder location is affected by M4. |
| RT-3 | HIGH | FOLDED | Spike ran on Windows: T1–T5 hold (lab note). Results folded into §3.4, §5.2, §5.4. The spike's macOS `fcntl` leg did not run; it is now lab row MA-1134 at R8. The gate required only the Windows VM, so this is allowed (see N9). |
| RT-4 | MED | **PARTIAL** (M1, M2) | Order and fsync points are right and consistent: [1] step 1, [2] step 4, [3] step 5, [4] step 8, [5] step 9, [6] step 10. All five missing crash rows were added. Missing: (a) what a crash-recovery cleanup removes. Cleanup rule (b) is "the entries the plan recorded", but no record of those entries outlives the process: the started line has no entry list, and for an encrypted archive it may hold no folder names at all. (b) The "crash after checked" row re-checks the tree "against the archive", but nothing records which records went into which file, and the archive may have changed since. |
| RT-5 | MED | **PARTIAL** (M3, M4) | Marker plus skip set in `localWalk.sbd`: right place, since the orphan loop reaches it through `sub` (`localwalk.go:521-524`, `:583-595`). W-UNFINISHED is shown at Desktop start and in `status`. Skip-only (no cleanup) is a stated lead's call. Gap 1: a backup of another archive builds its skip set before a restore starts, then walks into that restore's marked container, whose ID it does not know. It backs that folder up. So "the one instant it can meet" (§7.2 note) is wrong. Gap 2: see M4. |
| RT-6 | MED | FOLDED | `SafeName` steps in order, the last one de-duplicating NFC- and case-blind. Budget 246 = 255 − 4 − 5. "Restored — " = 13 bytes / 11 UTF-16 units (checked). Windows 259 for a file with `.msf`, 247 for a folder. Computed in the plan. Small edge: N6. |
| RT-7 | MED | FOLDED, lead to confirm | Own caps, 10,000 folders and 300 levels, refused in the plan (W-TOOMANY). These match the tree's own constants (`copyexport_archive.go:415-418`). The note says the numbers are "the folding agent's proposal, for the lead to confirm at re-verify". A verifier cannot make that call, so the lead owes one line (L1). |
| RT-8 | MED | FOLDED | Tick checked on the server (W-TICK). Ask twice through `copyLeaves` (`decryptplan.go:123-147`) and `confirmAgain` (`encryption_copy.go:98`). Words say what removal cannot undo. Counts only. Save this record warned. Minor: N7. |
| RT-9 | MED | FOLDED | Uses `!(rec.Present \|\| rec.LastSeen.IsZero())` (`copyexport_archive.go:857`, read). Hidden when the archive keeps no timeline. The rule for "keeps that timeline" is left to the builder (N8). |
| RT-10 | MED | FOLDED | Prefers a profile the archive does not back up. W-RECAPTURE comes first. O1 is open, built as (b), with (a) a one-condition change. |
| RT-11 | MED | FOLDED | Store detection warns with W-STORE, first in the plan and in Done. W-NOIDENT for a drive with no file identity (`copyexport_place_windows.go:164-166`, read). W-NETWORK warns rather than refuses; the note marks that as the folding agent's proposal (L2). |
| RT-12 | MED | FOLDED | Full re-read before the parent file, with SHA-256 per message, then W-CHECKFAIL. The fixity-failed messages are left out of the comparison. Crash-after-check is covered under RT-4 (M2). The reader MA-1089 "ports" becomes production code (N10). |
| RT-13 | LOW | **PARTIAL** (fix before R7) | "Own key" plus "add the kind to `ofCurrentArchive`" is a trap. `discardExcept` ends every `ofCurrentArchive` item whose key is not the current archive's (`pending.go:217-219`). It runs after **every** settings save (`dashboard.go:687`). So a restore plan with its own key would be dropped at every save. `pending.go:63-67` says exactly this about delete plans. Fix: do it delete's way. Own key, not in `ofCurrentArchive`, and `discardWhere(isRestorePlan)` beside `isDeletePlan` at a switch (`dashboard.go:691`, `:710`). Also, with its own key, a Lock (`discardFor(key)`) no longer discards it. Start's lease check still refuses a locked archive, so that is safe, but §7.3's "a Lock discards the plan" should say so. |
| RT-14 | LOW | FOLDED | Linux pieces are marked as new code, in R3; the macOS raw count is used (`env_darwin.go:373`). |
| RT-15 | LOW | FOLDED | Escapes, unknown escape refused, `..` in `directory-rel`, `user.js` (now read, not just a lab case), `profiles.ini` over the cap refused, absolute `Default=`, list id bound to the position. Minor: `user.js` has no stated cap; `StripBidi` strips bidi characters but not controls (`paths.go:177-181`). |
| RT-16 | LOW | PARTIAL (with M1) | Reuses the delete log's discipline. Note: `appendLog` is unexported and typed to delete lines (`dellog.go:151`), so "reused" means lifting it into a shared helper with delete's rows kept green. Cleanup rules are given, but rule (b) depends on M1. W-LOGBAD and W-LOGFULL exist, but their remedies (move or delete the log) also wipe the skip set (N2). |
| RT-17 | LOW | FOLDED | Every missing set of words is added: W-NOIDENT, W-OVERLAP, W-PREFS, W-PROFMODE, W-LOGBAD, W-LOGFULL, W-LEFT, W-RESTORING, plus the W-RUNNING sentence. |
| RT-18 | LOW | FOLDED | Verb `restore-thunderbird` in `holderWords` (`busy.go:72-91`), the quiet skip (`busy.go:54`), the read lease rather than `ExtractSealed` (`extract.go:418-419`), and the Lock reply. |
| RT-19 | LOW | FOLDED | Claims are labelled where they stand. Small over-claim: N5. |
| RT-20 | LOW | FOLDED | FAT32 refused instead of split; the maildir signal widened. |
| RT-21 | LOW | FOLDED | Windows-only legs run on the VM at R8; version recorded; re-check trigger stated. |
| RT-22 | LOW | FOLDED | Margin ×1.30 + 512 MiB, stated as a choice and revisited from MA-1093. |
| RT-23 | LOW | FOLDED | R2 is its own slice, with presync's rows unchanged. |
| RT-24 | MED | FOLDED | Every lab and unit item maps to a row (MA-1127, MA-1134..1139, MA-1099, MA-1091). Status line cites the review. |

### Lab findings

| Finding | Applied? |
|---|---|
| Save As is not the check | Yes. MA-1093's method was changed (§12.3 note). The old row text is kept under the note, and the note wins. |
| No lone-`.sbd` fallback | **Stated, not applied.** §5.4 note says it is dropped, yet step 4 builds it (working names plus per-file rename). This is M5. |
| No-Local-Folders refusal | Yes. It refuses, never edits `prefs.js` or `user.js`, and W-NOLOCAL's menu path is blocked from shipping until MA-1133. Note: MA-1133 sits at R8, after R7's friction review (N4). |
| Cancel words | Yes. W-WAIT, shown for the whole run, says to choose Cancel (lab B's dialog). |
| `os.Root.Mkdir` exclusive on Windows | Yes: to be proven, not assumed (MA-1120's Windows leg). |

### Do the folds agree with each other?

- **Commit order (RT-1), fsync order (RT-4), check (RT-12), crash table:** they agree. Data is
  fsynced and directories synced before the check, the check comes before the parent file, and the
  parent file before "ended". Every crash state leaves no parent file unless the data was already
  checked. The exceptions are M1 and M2 above.
- **Backup skip (RT-5) against the crash table:** they agree for the same archive, since the archive
  lock keeps the two apart. For another archive they do not (M3).
- **Lock order (RT-2) against the re-check (§5.3 note):** they agree. The order is restore lock,
  then checks A and B, then Thunderbird's lock, then B again. Confirmed in the code: on macOS and
  Linux, `thunderbird.Running` with no `lock` symlink falls through to a Windows-only probe
  (`thunderbird.go:127-134`, `:154-158`), so A would not see the restore's own lock there. Presync
  needs the MailArchive restore lock, as folded.
- **Pending store (RT-13) against §7.3:** they conflict (see RT-13's row).

## 2. Citations the folds added (26 checked, all correct)

`copyexport_place_windows.go:100-102` (`FILE_NON_DIRECTORY_FILE`), `:164-166`;
`copyexport_place_unix.go:27-33`, `:27-60` (FAT window); `copyexport_fs.go:139-141` (Windows
directory sync is a no-op); `copyexport_archive.go:358-364`, `:415-431` (300 / 1,024 / 10,000 /
8 MiB), `:508-511`, `:857`; `pending.go:39-42`, `:63-70`, `:68`, `:144-150`, `:209-227`;
`busy.go:54`, `:72-91`; `decryptplan.go:61`, `:123-147`; `encryption_copy.go:98`, `:170-180`,
`:190`; `cmd/mailarchive/decrypt.go:58-86`; `source/source.go:149-155`; `source/localwalk.go:488-494`,
`:582-595`, `:601-612`; `extract.go:418-419`; `presync/env_darwin.go:351`, `:373`;
`presync/env_windows.go:268-292`; `desktop/runnow_sync.go:130`; `presync/running.go:23`;
`presync/resolve.go:50-52`, `:99-100`; `util/paths.go:53-80`, `ShortHash` = 8 hex (`:141`),
`StripBidi` (`:177`); `archdelete/dellog.go:180-219`; `source/mbox.go:112`, `:208`;
`thunderbird.go:127-165`. Every one says what the fold says it does.

Id claims: MA-1084..1099, MA-1119..1143, R40, R41 and S66..S74 appear in no `scenario-catalog.md`
on `main`, `origin/main`, `cloud-sync-note`, `switch-keeps-runjob`, `cid-not-utf8` or this branch
(git grep). The last S id on main is S65.

## 3. Must fix before slice R0 (text only)

- **M1 (RT-4, RT-16): the list of what was made must outlive a crash.** Recommend: keep it *inside*
  the container, in a second dot-named file (for example `.mailarchive-restore-entries`). Append
  each entry's relative name and fsync it before that entry is created. Its names already sit in
  that folder as plain text, so an encrypted archive's `restores.log` stays free of names. Crash
  cleanup then removes exactly the listed entries, the list file, the marker last, then the
  container; anything else is W-LEFT. Keep the marker to its one line, so the backup's 4 KiB read
  still parses it.
- **M2 (RT-4, RT-12): say what "crash after checked" re-checks against.** Recommend: at step 7
  (the "checked" line), record each file's size and SHA-256 in M1's list file. The next attempt
  compares the files with that, which needs neither the archive nor its key; the archive may have
  changed since, or be locked. The other choice is to drop the "finish it" path and clean up as for
  a crash while writing. Also: the row says "the plan says … MailArchive finished it", but `-plan`
  and Check write nothing (§10 note). The finishing happens at Start, so word it there.
- **M3 (RT-5): close the skip-set window.** When the walk meets a well-formed marker whose ID is
  not in the skip set, it reads `restores.log` once more. A real restore's started line is fsynced
  before its `Mkdir` (§5.4 note, step 1), so it is always there by then. Only an ID still unknown is
  walked as an ordinary folder. Correct the "one instant" sentence and add the case to MA-1138.
- **M4 (RT-2 part 4, RT-5, RT-16): where `restores.log` and the restore lock live.** "MailArchive's
  settings folder" is not one fixed place. A Desktop started with `-config` keeps its log beside
  that config file (`archivedelete.go:267-293`), and a scheduled job "does not take" `-config`
  (`main.go:1296`). So a restore from such a Desktop would be invisible to scheduled backups (no
  skip) and to their pre-sync (no restore lock). Recommend: always MailArchive's own folder
  (`job.Dir()`), whatever `-config` is, and say so.
- **M5 (RT-1, lab A3 and A5): working names, keep or drop.** Recommend: drop them. Write each mbox
  directly under its final name with `O_EXCL`, inside a container that Thunderbird does not show
  (lab A3). That removes the per-file rename, which can meet antivirus on Windows, and the FAT/exFAT
  race window (§9.2 note), and none of the folds needs working names. If they stay, give the reason
  and strike "the lone-`.sbd` fallback … is dropped too".

**Lead's one-line confirmations, also before R0:** L1, the 10,000-folder and 300-level caps
(RT-7, MED). L2, W-NETWORK warns rather than refuses (RT-11).

## 4. Fix with the named slice (not before R0)

- RT-13's pending-key trap (above): before R7.
- N2: W-LOGFULL's and W-LOGBAD's remedies (delete or move the log) wipe every ID in the skip set.
  After that, finished restored folders are backed up again (O1(b) lost without a word), and
  unfinished leftovers are walked. Before R5: either MailArchive trims the log itself and keeps the
  IDs whose folders still exist, or the words say what is lost.

## 5. What a builder would still have to invent (notes)

- N1. An empty container with no marker, left by a crash between `Mkdir` and the marker: it is never
  removed (it cannot be proven ours) and it takes the name. Say it is left, and that the next restore
  takes " (2)".
- N2. See §4.
- N3. The marker's `profile` field: say it is the same hash as the restore lock's name (first 16 hex
  of SHA-256 of the canonical path), and give the one function both use. Presync must compute it
  identically.
- N4. W-NOLOCAL's menu path comes from MA-1133, at R8, which is after R7's friction review. Run
  MA-1133 before R7, or let the friction review see a placeholder.
- N5. R40 says the restore "opens but never writes" Thunderbird's lock file, yet it also *creates*
  `parent.lock` or `.parentlock` when absent (§1 note, §5.2 note). Say so in R40.
- N6. Past ` (99)` the dedupe uses the 9-character hash suffix, but the budget reserved only 5. Cut
  again in that case.
- N7. W-CHECKFAIL's FILE is a folder name. For an encrypted archive, leave it out as FOLDER is left
  out. The same goes for W-LEFT's FIRST NAMES.
- N8. RT-9: give the rule for "the archive keeps that timeline" (for example: any record with a
  non-zero `LastSeen`).
- N9. Recommend running MA-1134's macOS leg (Thunderbird held out by an `fcntl` lock) before R3,
  where the Unix lock is written, not at R8. If it fails, R3's design changes.
- N10. The post-write check runs MA-1089's port of Thunderbird's read rules in the product. Name
  the package that owns it, not a test helper.
- N11. `user.js`: state its size cap (same 16 MiB as `prefs.js`, W-PREFS).
- N12. `copyLeaves` is an unexported method of `CopyExportEnv` with the copy's own words ("the
  readable copy"). The restore needs it exported, or a shared core with its own words.

---

- Drew on, and reached: the design at 99c526c; the review at bd9a8ac; the lab note at 28c19a4; the
  repo code at the lines listed in §2, plus `pending.go:15-243`, `dashboard.go:675-711`,
  `archivedelete.go:264-293`, `lockfile.go` (function list), `cmd/mailarchive` (`-config` flags);
  git grep of the catalog on six branches.
- Not checked, per claim: Thunderbird's source (`nsProfileLock.cpp`, `nsShouldIgnoreFile`), and
  whether an OFD lock on Linux or `F_SETLK` on macOS keeps Thunderbird 153 out. Both are still lab
  rows. Not checked: whether Go's `os.Root.Mkdir` is exclusive on Windows (MA-1120), or whether a
  file rename inside a just-written folder meets Defender (MA-1136). No test or program was run.
- Smallest check for the reader: open `internal/desktop/pending.go` lines 63-70 and 215-219, then
  `dashboard.go` line 687. Those settle the RT-13 finding in under a minute.
- Counters. Register: verification finding. Borrowed lines: short quotes from the design, the review
  and the code. Drift: none noticed. Touched: this file (new) and one commit; the design and product
  code are unchanged. Persona: none. Source reached: design, review, lab note and code, yes;
  Thunderbird source and the VM, no.
