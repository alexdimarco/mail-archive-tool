# Design: Restore to Thunderbird…

Status (2026-11-06, revision 2): REVIEWED, FOLDED, NOT BUILT. Nothing in this document
exists in the code yet.
- Reviewed by the 10-lens pre-code design review, filed as
  `docs/review-restore-thunderbird-10lens.md` (verdict GO WITH CONDITIONS, commit bd9a8ac).
  That file, not the `…-predesign.md` name below, is the review of record [RT-24].
- Its gate RT-3 ran on the Windows VM: `docs/lab/restore-thunderbird-windows-2026-11-06.md`
  (commit 28c19a4, Thunderbird 153.6.0). Verdict there: RT-3 holds, no NO GO.
- Folded 2026-11-06: all 24 conditions RT-1 to RT-24 (HIGH and MED folded in full; every LOW
  folded too, none deferred) and the lab's four findings, each as a dated note beside the text it
  amends ("Folded 2026-11-06 [RT-n]: …"). The lead's technical calls are labelled "lead's call
  2026-11-06". The reviewed text of revision 1 is kept unchanged under the notes; where a note and
  the text it sits beside disagree, the note wins.
- Still owed before slice R0: a re-verify of this fold (that each note answers its condition), and
  the owner's answers to §15 (Q1–Q6, Q8, Q9, O1, O2). The design is written so each recommended
  answer is what gets built if the owner agrees, and each alternative is a small change, named
  beside its question.
- Index of the folds (condition → where): RT-1 §5.4, §3.5, §8, §14; RT-2 §5.1–§5.3, §14; RT-3 §3.4,
  §5.2; RT-4 §5.4, §8; RT-5 §7.2, §8; RT-6 §4.4, §4.5; RT-7 §6.5, §9; RT-8 §6.2, §7.3, §7.5, §10; RT-9 §6.4;
  RT-10 §3.1, §6.4, §7.2; RT-11 §3.1, §3.2, §7.5; RT-12 §5.4, §6.1; RT-13 §7.3; RT-14 §2, §5.1;
  RT-15 §3.1, §3.2; RT-16 §7.4, §8; RT-17 §7.5; RT-18 §6.2, §7.1; RT-19 §1, §2, §4.6, §6.1, §9, §11;
  RT-20 §3.3, §4.7; RT-21 §12.3, §13; RT-22 §6.3; RT-23 §13; RT-24 this line, §12.2, §12.3. Lab
  findings: no Local Folders §3.2; Save As is not the check §12.3 (MA-1093); the lock dialog's
  words §5.2, §7.5.

> Folded 2026-11-06 [foldcheck]: the fold check `docs/review-restore-thunderbird-foldcheck.md`
> (commit f4411a9, verdict FAIL, narrowly) named five must-fix items M1–M5, the lead's two
> confirmations L1 and L2, the RT-13 pending-key trap, and twelve smaller notes N1–N12. Each is
> folded as a dated note beside the text it amends, tagged "[foldcheck M1]" and so on. Earlier fold
> notes these replace are kept, and a note beside each says "superseded by [foldcheck …]". The
> lead's calls are labelled "(lead's call)"; answers the folding agent chose for the smaller notes
> are labelled "(folding agent's answer)", and the two it could only propose are labelled "for the
> lead to confirm". Where a foldcheck note and an earlier note disagree, the foldcheck note wins.
> Still owed before slice R0: a re-check of these notes only (the fold check's §3 says a full
> re-verify is not needed), and the owner's answers to §15.
> - Index (item → where): M1 §3.4, §5.4, §8, §9; M2 §5.4, §7.2, §7.4, §7.5, §8, §10, §14; M3 §7.2,
>   §11; M4 §1, §5.2, §7.4; M5 §2, §4.5, §5.4, §7.2, §8, §9, §11, §12.3, §13, §14; L1 §6.5, §7.5;
>   L2 §3.2; RT-13 §7.3, §7.5; N1 §8; N2 §7.5; N3 §3.4; N4 §12.3, §13; N5 §11; N6 §4.4; N7 §7.5;
>   N8 §6.4; N9 §5.2, §13; N10 §5.4; N11 §3.2, §7.5; N12 §6.2; the RT-15 and RT-16 minor notes
>   §3.1, §7.4; every changed test row §12.2.

> Folded 2026-11-06 [foldcheck2] (lead's calls): the second fold check
> `docs/review-restore-thunderbird-foldcheck2.md` (commit 547b00f, verdict FAIL, narrowly) named
> three must-fix items P1–P3, one residual case owing two sentences, and seven smaller notes
> N-a..N-g. Each is folded as a dated note beside the text it amends, tagged "[foldcheck2 P1]" and
> so on; text it replaces is kept and marked "superseded by [foldcheck2 …]". Every item is a lead's
> call except where a note says "(folding agent's answer)". Where a foldcheck2 note and an earlier
> note disagree, the foldcheck2 note wins. In the paragraph above, "the two it could only propose
> are labelled 'for the lead to confirm'" is superseded by [foldcheck2 N-e]: both (N2's words-only
> answer and uninstall naming leftovers) are now the lead's calls.
> Still owed before slice R0: a re-check of P1–P3 and these notes only (the check's §1 says that is
> enough), and the owner's answers to §15.
> - Index (item → where): P1 §3.4, §5.4, §7.4, §7.5, §8, §9, §10, §11, §12.2, §12.3, §13, §14; P2
>   §7.2, §7.4, §7.5, §12.2; P3 §7.2, §11, §12.2, §14; the residual case §7.2, §8, §11, §12.2; N-a §8,
>   §12.2; N-b §7.4, §12.2, §13; N-c §5.4, §7.5, §8, §10, §12.2; N-d §8, §13; N-e this note, §7.4,
>   §7.5; N-f §7.5, §8, §12.2; N-g §7.5, §8, §10, §12.2; P1's lab check §12.3, §13. RT-12's check
>   failure §5.4.

Revision 1 status, as reviewed (kept):

Status: PROPOSED, revision 1 (2026-11-06). Not reviewed, not built. Nothing in this
document exists in the code yet.
- Owes, before any code: the 10-lens pre-code design review
  (`../assurance-kit/process/design-review.md`), filed as
  `docs/review-restore-thunderbird-predesign.md`, and the owner's answers to §15.
- Base: main at 794b86e (v1.5.9). Every "file:line" below is at that commit.
- Owner's ask, 2026-11-06: "lets do restore item one". Item one was the lead's
  recommendation: a Desktop button "Restore to Thunderbird…" that writes an archive (or
  some of its folders) into a NEW folder under Thunderbird's Local Folders, asks the
  person to close Thunderbird first, and never touches live accounts or existing
  folders. Windows is the biggest user group (owner, 2026-11-01): Windows is designed
  and tested first, then macOS, then Linux.

How to read the claims here: each claim about MailArchive's own code cites file:line.
Each claim about Thunderbird says where it comes from — Thunderbird's source or bug
tracker (read 2026-11-06, not run), Mozilla's support forum, or "not checked" — and
names the lab row (§12.3) that settles it. Nothing about Thunderbird is called proven
until a lab row has run.

## 1. What is built, in one paragraph

A person picks an archive (the current one) and, optionally, some of its folders, and a
Thunderbird profile on this computer. MailArchive checks Thunderbird is closed, shows a
plan (what will go back, what cannot and why, where, how big, free space), and on
"Restore" writes one new folder, "Restored — ARCHIVE NAME", under that profile's Local
Folders, with the archive's folder tree below it: one mbox file per folder, each
message the archive's preserved original bytes. It never changes, renames or replaces
anything already in the profile, never writes under ImapMail or any other account's
directory, and never starts or quits Thunderbird. The same engine runs from the
command line as `mailarchive restore-thunderbird` (R33, "One program",
`docs/scenario-catalog.md:275`).

> Folded 2026-11-06 [RT-19, RT-1, RT-2]: where each claim in this paragraph stands.
> - "Writes one new folder": built without any folder rename (§5.4 as folded): the folder's
>   `.sbd` directory is made first, the empty parent file last, and Thunderbird shows nothing until
>   that last file exists (lab A3 and A5, Windows; macOS and Linux are lab row MA-1091).
> - "Nowhere else": besides Local Folders, the restore touches two things. (1) Thunderbird's own
>   profile lock file, held as Thunderbird holds it (`parent.lock` on Windows, `.parentlock` on
>   macOS and Linux; opened, and created only if absent; never written to). (2) MailArchive's own
>   settings folder: `restores.log` and a restore lock file (§5.2, §7.4).
> - "The archive's preserved original bytes": for a message from an mbox source these are the
>   bytes the archive kept, which are go-mbox's output (CRLF lines, one `>` taken off a `>From `
>   line), not the source mbox's exact bytes (§16 side finding 4, review side finding A).
>
> Folded 2026-11-06 [foldcheck M4, N5] (lead's call for M4): "MailArchive's own settings folder"
> above is always MailArchive's default settings folder, `job.Dir()`, never the folder of a
> `-config` file (§7.4 foldcheck note). The lock file "created only if absent" is now said in R40
> as well (§11 foldcheck note).

## 2. What exists today, and what this reuses

| Need | What exists (read, not assumed) | Reuse or change |
|---|---|---|
| Faithful bytes per record | `extract` reads each record's preserved `.eml` through the same gate `verify` uses, checks recorded fixity, skips and counts what has none (`internal/app/extract.go:328-399`); an encrypted archive's originals come decrypted in memory through `SealedOriginals` (`internal/app/extract.go:110-127`, `internal/keyring/extract.go:18-44`) | Reuse. Slice R1 lifts the per-record "give me the faithful bytes or a skip reason" step out of `extractor.processRecord`/`processSealed` into one shared function, so extract and restore cannot disagree. Extract's own output stays byte-identical (its tests MA-181..MA-185 must stay green). |
| mbox writing | `interchange.WriteMessage` writes mboxrd: a synthesized "From " line, every `>*From ` line quoted, a blank line after (`internal/interchange/mbox.go:41-81`); `Postmark` gives "From ADDRESS Mon Jan _2 15:04:05 2006" (`internal/interchange/mbox.go:90-103`) | Reuse, with one option added (§4.3: the two framing line endings follow the message's own). |
| Folder tree with counts | The Extraction workspace builds the archive's tree from the manifest: store → folder path from each record's `Folder`, bounded depth, name length and node count (`internal/app/copyexport_archive.go:415-431`, `:437-472`, `:489-600`); nodes carry ids that are "validated against the snapshot, never used to build a path" (`:70-78`) | Reuse the tree rules. Slice R1 lets the same builder take a manifest however it was loaded (plain or sealed), since the workspace's `ArchiveSource` reads only a plaintext manifest (`:30-35`). |
| Thunderbird profiles | presync parses `profiles.ini` (paths only, capped, control-stripped) (`internal/presync/resolve.go:44-106`), reads it from `%APPDATA%\Thunderbird` on Windows (`internal/presync/resolve_windows.go:148-162`) and `~/Library/Thunderbird` on macOS (`internal/presync/resolve_darwin.go:157-170`), validates a profile (`resolve_windows.go:116-146`, `resolve_darwin.go:108-150`), and derives "the profile that contains the archive's inputs" (`resolve.go:16-42`) | Reuse, moved to `internal/thunderbird` so both callers share one parser (slice R2). The parser must also return each profile's `Name=` and which one is the default. Its comment says values are "used only to compare … never as a path to open" (`resolve.go:46-48`); restore WILL write under a listed path, so §9 adds the checks that change requires. |
| Which profile an archive came from | Desktop finds the job Run now would run and asks whether an input sits in a Thunderbird profile (`internal/desktop/runnow_sync.go:108-142`, using `thunderbird.FindProfileDir`, `internal/thunderbird/thunderbird.go:27-42`) | Reuse for the default profile (§3.1). |
| Is Thunderbird running | `thunderbird.Running`: the `lock` symlink's pid alive on macOS/Linux, `parent.lock` held on Windows (`internal/thunderbird/thunderbird.go:118-165`); presync adds "any Thunderbird-named process" (`internal/presync/running.go:20-42`), with real probes only on Windows and macOS (`internal/presync/env_windows.go:118-175`, `env_darwin.go:310`, `:351`) | Reuse both signals; add a Linux process probe (§5). |
| prefs.js | A line regex parser that trims quotes but does not decode JavaScript string escapes (`internal/thunderbird/thunderbird.go:60-77`) | Change: a decoder for `\\`, `\"` and `\uXXXX`, needed because Windows paths in prefs.js are written with doubled backslashes (§3.2). |
| The archive's lock | One exclusive OS lock per archive; a second holder is refused with "archive is in use" (`internal/lockfile/lockfile.go:38-91`); extract holds it for its whole run so a scheduled backup that fires meanwhile refuses, and names no verb for an encrypted archive (`internal/app/extract.go:158-172`) | Reuse exactly as extract does (§7.1). |
| Writing without following links or replacing | The copy export's "contained writer": every write through an `*os.Root`, files created exclusively, folders 0o700, a file reaches its final name only by a no-replace rename relative to its folder's handle (`internal/app/copyexport_fs.go:15-31`, `:78`, `:108-117`); the no-replace rename per platform: Windows `FILE_RENAME_INFORMATION(_EX)` without replace (`copyexport_place_windows.go:14-20`, `:75`), Linux `renameat2(RENAME_NOREPLACE)` (`copyexport_place_linux.go:13-15`), macOS `renameatx_np(RENAME_EXCL)` (`copyexport_place_darwin.go:12-15`), other Unix a hard-link fallback (`copyexport_place_other.go:11-14`); a fault-injection seam for tests (`copyexport_fs.go:30-31`) | Reuse. Go's own `os.Rename` (and the writer's own `Replace`, `copyexport_fs.go:117-119`) can replace an existing target, so neither is used for the restore's final step. |
| Safe file names | `util.SanitizeSegment`: NFC, Windows-illegal characters and controls replaced, trailing dots and spaces trimmed, device names (CON, NUL, COM1…) neutralised, 120-rune cap, and a short hash suffix when anything changed so two names never merge (`internal/util/paths.go:18-60`, `:65-81`) | Reuse, plus Thunderbird's own rules (§4.4). |
| Free space | `util.FreeBytes` (`internal/util/diskfree.go:9`) | Reuse. |
| Cloud-sync folders | `util.UnderCloudSync` names OneDrive, Dropbox, iCloud and others from a path (`internal/util/paths.go:186-225`) | Reuse for a warning (§6.4). |
| Desktop requests | `d.api`: POST only, `GuardLocalPOST` (loopback Host, loopback Origin, CSRF token), 1 MiB body, recover (`internal/desktop/workspace.go:104-138`, `internal/server/setup.go:168-176`); the session cookie gate (`internal/localweb/session.go:168-169`, `:265`) | Reuse unchanged. |
| A plan held for a second step | `pendingSecrets`: one-use tokens, kind-filtered, ten-minute life, ended at Shut down (`internal/desktop/pending.go:15-55`, `:132-187`) | Reuse with a new kind (`restorePlan`). |
| A long task with progress and Cancel | `runJob`: one task at a time in the shared slot, refused while a delete runs, log lines and result shown through `/api/activity` (`internal/desktop/maintenance.go:19-84`, `internal/desktop/capture.go:379-404`); the unencrypted copy is the closest pattern — plan, token, confirm, a read lease on the key, progress every 3 seconds (`internal/desktop/encryption_copy.go:26-59`, `:87-143`, `:166-168`) | Reuse the copy's shape step for step. |
| Bringing a step into view | `showStep` (MA-1070, v1.5.7): scrolls the step into view, keyboard to its first field, a status line says what is needed (`internal/desktop/dashboard.go:2362-2380`); `noteBeside` for a note beside a row (MA-955, `dashboard.go:2610`) | Reuse. |

> Folded 2026-11-06 [RT-14, RT-1, RT-13, RT-8, RT-19, RT-23]: corrections to the table above.
> - "Thunderbird profiles" and "Is Thunderbird running": the **Linux** pieces are NEW code, not
>   reuse. Presync has no `resolve_linux.go`, so the Linux profile ownership and mode checks are
>   new; the Run-now profile lookup returns false on Linux (`runnow_sync.go:130`), so the Linux
>   default-profile step is new; and there is no Linux process probe today. All three go in slice
>   R3 with their own test rows (MA-1084, MA-1087). On **macOS** the process check refuses on the
>   raw count of Thunderbird processes (`rawThunderbirdCount`, `env_darwin.go:373`), not on the
>   filtered list (`:351`), which drops any process it cannot fully read.
> - "Thunderbird profiles": moving presync onto the shared parser is its own commit in slice R2,
>   with presync's existing rows unchanged and green before and after, and presync not using the
>   new outputs (names, default) [RT-23].
> - "Writing without following links or replacing": the no-replace *rename* helper is NOT used for
>   the commit any more (§5.4 as folded: no folder rename). It is used only to place each mbox
>   *file* from its hidden working name to its final name; it opens its source with
>   `FILE_NON_DIRECTORY_FILE` on Windows (`copyexport_place_windows.go:100-102`), which is right for
>   a file. On FAT/exFAT under macOS and Linux that helper falls back to a look-up-then-rename with a
>   stated race window (`copyexport_place_unix.go:27-60`), so "cannot replace" holds there only
>   because Thunderbird is closed and held out by the lock (§9.2 as folded).
>   - Folded 2026-11-06 [foldcheck M5] (lead's call): this bullet is superseded by [foldcheck M5].
>     The restore uses no rename helper at all: every entry is created under its final name by an
>     exclusive create (§5.4 foldcheck note). So the FAT/exFAT look-up-then-rename window does not
>     arise for the restore.
> - "A plan held for a second step": `pendingSecrets.put` discards every item with the same key
>   (`pending.go:144-150`), so the restore plan gets its own key (§7.3 as folded) [RT-13].
> - "A long task with progress and Cancel": two more things carried over from the unencrypted copy:
>   ask twice, checked on the server, for a destination that may leave the computer
>   (`encryption_copy.go:98`, `DecryptPlan.AskTwice`, `decryptplan.go:61`); and progress lines that
>   are counts only for an encrypted archive (`encryption_copy.go:170-180`) [RT-8].
> - "Safe file names": SanitizeSegment does NOT always keep two names apart. Its hash suffix is
>   added only when a character was replaced or the name was cut (`paths.go:53-80`), so ".Inbox",
>   "Inbox." and "Inbox" all become "Inbox"; its cap is 120 runes, not bytes (§4.4 as folded)
>   [RT-6, RT-19].
> - Citations corrected [RT-19]: the message's Raw is kept at `localwalk.go:488-494`,
>   `source/mbox.go:112`, `:208` (not `rawstub.go:63-67`, which is `isPanicStub`); the workspace's
>   plaintext-only manifest load is `copyexport_archive.go:358-364`; a store's label is
>   `token — mailbox`; the pending discard paths are `pending.go:63-70`, `:209-227`.

## 3. Where it writes

### 3.1 The profile

1. **Find the profiles.** Read Thunderbird's `profiles.ini`, capped and as untrusted text,
   as presync does (`internal/presync/resolve.go:13-14`, `:49-106`):
   - Windows: `%APPDATA%\Thunderbird\profiles.ini`, `%APPDATA%` from
     `FOLDERID_RoamingAppData`, never the environment variable
     (`internal/presync/resolve_windows.go:88-96`).
   - macOS: `~/Library/Thunderbird/profiles.ini` (`resolve_darwin.go:159-170`).
   - Linux: `~/.thunderbird/profiles.ini`, then the Snap location
     `~/snap/thunderbird/common/.thunderbird/` (both already searched for backups,
     `internal/app/run.go:1860-1881`), then the Flatpak location
     `~/.var/app/org.mozilla.Thunderbird/.thunderbird/` (not searched for backups
     today, see §16 side finding 3).
   - Windows, not checked: a Thunderbird installed from the Microsoft Store may keep
     its profiles under `%LOCALAPPDATA%\Packages\…\LocalCache\Roaming\Thunderbird`
     (Windows redirects a Store app's writes to its own folder). If so, a restore
     written to `%APPDATA%\Thunderbird` would never be seen by it. Lab row MA-1097
     settles whether that is real; until then the plan says nothing about Store installs.

   > Folded 2026-11-06 [RT-11], lead's call 2026-11-06: **warn, do not refuse**, until lab row
   > MA-1097 has run (owner question O3 therefore does not arise; it would arise only if the lead
   > chose to refuse). Detection: any folder directly under `%LOCALAPPDATA%\Packages` whose name
   > contains "Thunderbird" (case-blind), `%LOCALAPPDATA%` from `FOLDERID_LocalAppData`, never the
   > environment variable. When one is found, the plan shows W-STORE (§7.5) as its first warning,
   > and the Done words add the same sentence, so a restore never claims success to a Store
   > Thunderbird that cannot see it. The 2026-11-06 lab VM had no Store package
   > (`Get-AppxPackage *thunderbird*` empty), so MA-1097 is still open; when it runs and shows
   > where the Store version keeps its profiles, that location joins step 1's list and W-STORE is
   > removed (or, if a Store Thunderbird cannot see an ordinary profile, the warning becomes a
   > refusal for that case — a one-line change).
2. **List them.** Each `[ProfileN]` section gives a name (`Name=`) and a folder
   (`Path=`, relative when `IsRelative=1`). Each `[Install…]` section's `Default=` names
   the profile that Thunderbird opens (`resolve.go:96-101`). The new parser returns
   name, canonical folder and "opens by default" per profile.

   > Folded 2026-11-06 [RT-15]: for the restore (a write path) a `profiles.ini` over the 256 KiB cap
   > is **refused** (W-PROFINI, §7.5), never cut short as presync's reader does today
   > (`presync/resolve.go:50-52`); presync keeps its own behaviour (RT-23). An `[Install…] Default=`
   > may be an absolute path and is read as one when it is (today it is always joined as relative,
   > `resolve.go:99-100`). Each `Name=` is shown with bidirectional and control characters removed
   > (`util.StripBidi`), as names in the archive are.
   >
   > Folded 2026-11-06 [foldcheck RT-15 minor] (folding agent's answer): `StripBidi` removes
   > bidirectional characters but not control characters (`paths.go:177-181`). So each `Name=` shown
   > also has C0 controls, DEL and the C1 controls removed, as `SafeName` step 2 does (§4.4 note).
3. **Keep only usable profiles.** A listed folder is offered only if it passes presync's
   profile check: it exists, is a folder, holds `prefs.js` as a regular file; on macOS
   and Linux it is owned by this user and not group- or world-writable
   (`resolve_darwin.go:113-150`; the Windows reduction is stated at
   `resolve_windows.go:116-121`). Presync's macOS refusal of a path with a space
   (`resolve_darwin.go:121-127`) is about matching process arguments and does NOT apply
   to restore.

   > Folded 2026-11-06 [RT-14, RT-17]: the macOS and Linux checks (owned by this user, not group-
   > or world-writable) are new code on Linux (§2 note). A profile that fails them is not dropped
   > silently: it is listed as not usable, with W-PROFMODE (§7.5) beside it.

   > Settled 2026-11-07 [R6-14] (the R6 adversarial pass; the lead's call; Windows): the folder's
   > links were resolved with `filepath.EvalSymlinks`, which since Go 1.23 fails on a junction in
   > the middle of a path, so a profile moved with `mklink /J` (`%APPDATA%\Thunderbird` → `D:\TB`)
   > was refused in the words of a folder that is not a profile. On Windows the profile and its
   > Local Folders are now resolved with the repository's own resolver (`util.ResolveExisting`,
   > through junctions and links alike, as the overlap checks are), and a path reached through a
   > link or junction (`util.ThroughLink`) is allowed when it leads inside the user's own folder
   > (`%USERPROFILE%`), and is then shown and pinned by where it really leads; one that leads
   > outside it is refused W-JUNCTION, naming where it leads and that choosing that folder by its
   > own path works (a listed profile so reached is listed as not usable with those words). macOS
   > and Linux are unchanged (EvalSymlinks follows every link there, and the folder it leads to is
   > held to this step's checks). Test: `tbrestore/ma1084_r614_junction_windows_test.go`
   > (MA-1084, MA-1143).
4. **Pick the default.** In order:
   1. the profile the archive's own Thunderbird source came from: the job Run now
      would run (`runnow_sync.go:108-122`), its first input that sits in a listed
      profile (`runnow_sync.go:129-142`, `thunderbird.go:27-42`);
   2. the profile Thunderbird opens by default (`[Install…] Default=`);
   3. the profile marked `Default=1`;
   4. none: the person chooses.

   > Folded 2026-11-06 [RT-10]: the order above is replaced by this one.
   > 1. When there are several usable profiles: the first, in the order 2 → 3 → listed order,
   >    that the archive does NOT back up (no input of any job for this archive lies inside it).
   > 2. Otherwise (one profile, or every profile is backed up by it): the order above, unchanged.
   > 3. When the chosen profile is one this archive backs up, the plan says so in its FIRST line
   >    (W-RECAPTURE, §6.4, §7.2), above every other line.
   > Most people have one profile, so (1) rarely applies; what keeps the default path from
   > copying the restored mail back into the archive is the backup-side skip of §7.2 (owner
   > question O1). The Linux leg of "does the archive back up this profile" is new code (§2 note).

   > Settled 2026-11-07 [R6-02] (the R6 adversarial pass, `docs/review-restore-thunderbird-adversarial-r6.md`;
   > the lead's call): "no input of any job for this archive lies inside it" read only Desktop job
   > files, so an archive backed up at the command line (by hand, or with `mailarchive schedule`,
   > whose job lives in the system scheduler) never warned, and mail later filed into the restored
   > folder was silently left out of its backups. "Backs up" is now decided from what the archive
   > itself records as its sources — its last run's job (`-input`, `-auto`), which a plain
   > archive's last-run record keeps — and the descriptor of a schedule installed for it (its job,
   > and its pre-sync profile), as well as every Desktop job file that backs up into it
   > (`tbengine.ArchiveSources`). Any source path at or inside the profile, or at or inside its
   > Local Folders when that lies elsewhere, warns; `-auto` (which finds every profile itself)
   > warns for every profile. When nothing names the archive's sources (an encrypted archive,
   > whose record keeps no inputs; an archive older than its record), the plan's first line says
   > MailArchive cannot tell, in W-RECAPTURE's place — never nothing. A Microsoft 365-only archive
   > backs up no profile and says nothing. Test: `tbengine/ma1124_r602_sources_test.go` (MA-1124).
5. **One profile:** shown, not asked. **Several:** the person chooses from the list
   ("NAME — FOLDER"), the default pre-selected. **None:** refused (§10, words W-NOPROF).
6. The page sends the chosen profile's position in the list the server made, never a
   path. The command line takes `-profile FOLDER` (typed by the person, so it is
   accepted without being listed, as presync accepts a named profile,
   `resolve_windows.go:137-139`) or `-profile-name NAME`; either still has to pass step 3.

   > Folded 2026-11-06 [RT-15]: the position is bound to the list the server made. The server keeps
   > the list it sent (each entry's canonical folder and its identity, volume and file id) beside a
   > list id; the page sends the list id with the position. A request whose list id is not the
   > current one is refused ("The list of Thunderbird profiles changed. Choose the profile again.");
   > at Plan and again at Start the server re-reads `profiles.ini` and refuses if the entry at that
   > position no longer names the same folder with the same identity. So a `profiles.ini` rewritten
   > between two clicks cannot point the position at another folder.

### 3.2 The Local Folders directory

Read `prefs.js` (bounded to 16 MiB; a larger file is refused as "not a Thunderbird
profile"). String values are decoded as JavaScript string literals (`\\`, `\"`,
`\uXXXX`), because the existing parser only trims quotes
(`internal/thunderbird/thunderbird.go:60-77`) and a Windows path in prefs.js reads
`"C:\\Users\\…"`.

> Folded 2026-11-06 [RT-15]: the decoder takes `\\`, `\"`, `\'`, `\n`, `\r`, `\t` and `\uXXXX`
> (a surrogate pair joined, a lone surrogate refused), and **refuses** any other escape rather than
> guessing (the value is then unreadable and the profile is refused with W-PREFS, §7.5). A
> `prefs.js` over 16 MiB is refused with W-PREFS too (its own words, not W-NOLOCAL). A `user.js` in
> the profile can also set the Local Folders server or directory; version 1 reads `user.js` after
> `prefs.js` with the same decoder (Thunderbird applies `user.js` last), and the lab checks that
> order (lab row MA-1095).
>
> Folded 2026-11-06 [foldcheck N11] (folding agent's answer): `user.js` has the same cap as
> `prefs.js`, 16 MiB. Over it, the profile is refused with W-PREFS, whose words then name `user.js`
> instead of `prefs.js` (§7.5 foldcheck note).

1. The Local Folders account is the server named by
   `mail.accountmanager.localfoldersserver` (for example `server1`). If that is absent,
   the one server whose `mail.server.serverN.type` is `"none"`. Zero or several → refused
   (W-NOLOCAL).

   > Folded 2026-11-06 [lab finding 1], lead's call 2026-11-06: a profile with **no Local
   > Folders** is a real, ordinary case, not an edge. In the lab, Thunderbird 153 on a profile with
   > no mail account made no `Mail\Local Folders` and no Local Folders server at all. MailArchive
   > **refuses in plain words and never edits `prefs.js` or `user.js`** (the lab added the keys to
   > its own throwaway profile's `user.js`; MailArchive never does that to a person's profile). The
   > words (W-NOLOCAL, replaced in §7.5) say how to get Local Folders in Thunderbird. **Lab-check
   > item (lab row MA-1133):** the exact menu path in Thunderbird 153 that makes Local Folders.
   > Candidate, not run: setting up a mail account (≡ menu → Account Settings → Account Actions →
   > Add Mail Account…), since Thunderbird is understood to make Local Folders with the first
   > account; the lab did not check whether an IMAP account makes it. The words are not shipped
   > until MA-1133 names the path that worked.
2. Its folder is `mail.server.serverN.directory-rel` when that begins with `[ProfD]`
   (joined under the profile), else `mail.server.serverN.directory` (an absolute path:
   some people move Local Folders to another drive, and Thunderbird honours it). This
   order — the relative setting first — is what I understand Thunderbird to use; not
   checked in Thunderbird's source; lab row MA-1095 includes a profile whose two settings
   disagree.

   > Folded 2026-11-06 [RT-15, lab]: `directory-rel` is resolved as Mozilla resolves a
   > `[ProfD]` relative path: the text after `[ProfD]` is joined to the profile folder and `..`
   > parts are allowed (not refused, not "joined under the profile"); the result then goes through
   > every check of step 3, which is what keeps it safe. Lab, 2026-11-06: Thunderbird 153 wrote both
   > settings itself (`directory` and `directory-rel` = `[ProfD]Mail/Local Folders`), so the names
   > are right; which one wins when they disagree is still lab row MA-1095.
3. Checks on that folder, refused with words when any fails (§10):
   - it exists and is a directory, after resolving links once (`EvalSymlinks`); the
     resolved path is what the plan shows and what is opened;
   - it is not inside, and does not contain, ANY account's directory other than Local
     Folders (every `mail.server.*.directory` and `directory-rel`), and no path element
     below the profile is `ImapMail` (the rule `thunderbird.IsImapStore` uses,
     `thunderbird.go:19-25`);
   - it does not overlap the archive folder (`util.Overlap`, as extract checks its
     destination, `internal/app/extract.go:193-199`) and is not inside any encrypted
     archive (`app.RefuseKeyless`, `internal/app/encguard.go:88`; as extract,
     `extract.go:154`).
4. Its identity (volume and file id, the copy export's `readIdentity`,
   `copyexport_place_windows.go:140-142`) is recorded in the plan and compared again at
   start (§5.3).

   > Folded 2026-11-06 [RT-11, RT-17]: on Windows `readIdentity` **fails** when the volume reports
   > no file identity ("the volume reported no file identity", `copyexport_place_windows.go:164-166`),
   > which a network share can do (an AppData redirected by company policy). That is refused with
   > words of its own (W-NOIDENT, §7.5), not a bare error. A Local Folders on a network path (a UNC
   > path `\\server\…`, or a drive whose `GetDriveTypeW` is `DRIVE_REMOTE`) whose identity CAN be
   > read gets the warning W-NETWORK in the plan and goes ahead: refusing would shut out every
   > person whose company redirects AppData, and Thunderbird itself already runs from that share.
   > (Warn rather than refuse is the folding agent's proposal, not a lead's call; for the lead to
   > confirm at re-verify.) Lab row MA-1097 gains the redirected-AppData case. Overlap with the archive or
   > with another account's directory has its own words too (W-OVERLAP, §7.5).
   >
   > Folded 2026-11-06 [foldcheck L2] (lead's call): confirmed. A Local Folders on a network drive
   > whose identity can be read gets W-NETWORK, a warning, and the restore may go ahead; one whose
   > identity cannot be read is refused with W-NOIDENT. This answers the "for the lead to confirm at
   > re-verify" sentence above.

   > Settled 2026-11-07 [R6-07] (the R6 adversarial pass): step 3 checked the profile's owner and
   > mode (§3.1 step 3) but never the folder actually written, when prefs point Local Folders
   > outside the profile. On macOS and Linux the resolved Local Folders is now held to the same
   > rule — this user's, neither group- nor world-writable — else W-LOCALMODE, naming it and the
   > remedy (`chmod go-w`), nothing restored. Windows has no owner check (§9 item 4); there the
   > restored files' own owner-only access lists (§9 item 6, R6-01) cover who can read them.
   > Test: `tbrestore/ma1085_r607_r615_test.go` (MA-1085).
   >
   > Settled 2026-11-07 [R6-14]: the "resolving links once" of step 3 is, on Windows, the
   > junction-aware resolve and rule of the §3.1 step 3 note.

### 3.3 The storage kind: mbox or maildir

Thunderbird keeps a folder either as one file (mbox, its default) or as a directory of
one file per message (maildir). The kind is set per account by
`mail.server.serverN.storeContractID`, else for the profile by
`mail.serverDefaultStoreContractID`, else mbox. The values are
`@mozilla.org/msgstore/berkeleystore;1` (mbox) and
`@mozilla.org/msgstore/maildirstore;1` (maildir). (From my knowledge of Thunderbird's
preferences; not checked against its source; lab row MA-1096.)

As a second signal, a directory directly in Local Folders that holds `cur` and `new`
is a maildir folder (the archive's own reader uses that shape,
`internal/source/localstore.go:129`).

**Decision:** version 1 writes mbox only. A maildir Local Folders, or one where the two
signals disagree, is refused before anything is written (W-MAILDIR). Why: mbox is
Thunderbird's default; a maildir writer is a second writer to review and lab-test; and
nobody has reported a maildir profile. Owner question Q5 (§15).

> Folded 2026-11-06 [RT-20, lab]: the second signal is widened. A folder directory directly in
> Local Folders that holds `cur` OR `new` OR `tmp` (the archive's own reader tests `cur` or `new`,
> `localstore.go:129`; Thunderbird's maildir may not make `new`), OR an `Inbox` or `Trash` that is a
> directory, counts as maildir. Needed because a store setting locked by policy may not appear in
> `prefs.js`. Lab, 2026-11-06: Thunderbird 153 wrote
> `storeContractID "@mozilla.org/msgstore/berkeleystore;1"` explicitly for Local Folders, so the
> setting's name and mbox value are as above; the maildir value is still lab row MA-1096.
> Q5's recommended answer (refuse) is what is built; the alternative (a maildir writer) is a new
> slice of its own, not a small change, which is why refusing is recommended.

### 3.4 What it creates

Inside the Local Folders directory, and nowhere else:

```
Local Folders/
  Restored — Work mail            (an empty mbox file: the parent folder)
  Restored — Work mail.sbd/       (its subfolders)
    Inbox                         (mbox: the archive's "Inbox")
    Inbox.sbd/
      Receipts                    (mbox: "Inbox/Receipts")
    Sent
    (no folder)                   (records whose Folder is empty)
```

> Folded 2026-11-06 [RT-3, RT-1], lead's call 2026-11-06: **RT-3 holds on Windows** (Thunderbird
> 153.6.0, the ordinary Program Files install; `docs/lab/restore-thunderbird-windows-2026-11-06.md`).
> With Thunderbird closed, a bare mbox file placed in Local Folders was found and counted (lab A1),
> `P` (empty) + `P.sbd/C` showed as P › C (A2), and Thunderbird made each `.msf` itself (at start for
> a parent, when opened for the rest). So the layout above stands. macOS and Linux remain lab rows
> MA-1098 and MA-1099.
>
> One entry is added to the layout: the **restore marker**, a file named `.mailarchive-restore`
> directly inside `Restored — NAME.sbd/`. It is created first, exclusively, right after that
> directory (§5.4 as folded), 0o600, holding one JSON line: `{"mailarchive":"restore","id":ID,
> "started":RFC3339 time,"profile":hash of the profile's canonical path}`, where ID is 128 random
> bits in hex, the same ID as the `restores.log` lines. It is what lets a later attempt (§8) and a
> backup (§7.2) recognise a folder MailArchive's restore made: a folder is "carrying the marker" only
> when its marker's ID matches a `restores.log` line for this profile on this computer, so a marker
> copied or planted elsewhere does nothing. Thunderbird ignores it: lab A4 showed dot-named entries
> are ignored in Local Folders (Windows); that this holds inside a `.sbd` too is added to lab row
> MA-1091.
>
> Folded 2026-11-06 [foldcheck M1] (lead's call): a second hidden file joins the marker: the
> **made-list**, `.mailarchive-restore-made`, directly inside `Restored — NAME.sbd/`, created
> exclusively right after the marker, 0o600. Before the restore creates each entry inside the
> container (a folder's mbox file, or a `NAME.sbd` directory), it appends one JSON line naming that
> entry, `{"k":"f","p":"Inbox.sbd/Receipts"}` ("f" a file, "d" a directory; the path relative to the
> container, parts joined by "/"), and fsyncs the list. Only then is the entry created. So after any
> crash the list names every entry the restore made, and nothing it did not try to make. Crash
> cleanup (§8 foldcheck note) removes exactly what the list names and nothing else, never following
> a link. The names in it already sit in that same folder as plain file names, so `restores.log`
> stays free of folder names for an encrypted archive (§6.2). The marker stays one short line, so
> the backup's 4 KiB read still parses it (§7.2 note). The layout above gains:
>
> ```
>   Restored — Work mail.sbd/
>     .mailarchive-restore          (the marker: one line)
>     .mailarchive-restore-made     (the made-list: one line per entry made)
>     Inbox
>     …
> ```
>
> Both hidden files stay after a restore finishes, as the marker always did. MA-1091's last step
> (the person renames or moves the restored folder in Thunderbird) watches the made-list too.
>
> Folded 2026-11-06 [foldcheck2 P1] (lead's call): "Crash cleanup … removes exactly what the list
> names and nothing else" above is narrowed by [foldcheck2 P1]: it removes **at most** what the list
> names. It removes nothing at all when an entry named `Restored — NAME` is beside the container, or
> when the container holds anything the list does not name (§8 foldcheck2 note).
>
> Folded 2026-11-06 [foldcheck N3] (folding agent's answer): the marker's `profile` field is the same
> value as the restore lock's name (§5.2 note, item 3): the first 16 hex characters of the SHA-256 of
> the profile's canonical path. One function computes it, `thunderbird.ProfileKey(dir)`, and the
> restore, presync and the backup skip all call it, so they cannot disagree. It resolves links
> (`EvalSymlinks`), makes the path absolute and clean, and on Windows and macOS folds case (their
> usual file systems ignore case; folding can only make two profiles share one lock, the safe
> direction). MA-1140 tests that presync and the restore get the same key for one profile spelled
> two ways.
>
> Settled 2026-11-07 [R6-15] (the R6 adversarial pass; the lead's call, "key by identity"): the key
> above resolved links but not macOS's `/System/Volumes/Data` firmlink, a Windows junction or a
> `subst` drive, so one profile spelled two ways had two restore locks, two settle scopes, and a
> presync blind to one. `thunderbird.ProfileKey` now keys a profile folder that exists by its
> identity — device and inode (Windows: volume serial number and file index, from a handle) —
> whatever path reaches it: the first 16 hex characters of the SHA-256 of "identity", the volume
> and the file id. The path's key above is kept only for a folder whose identity cannot be read
> (gone, or a volume that reports none), so a gone profile is still named as before. The restore,
> presync, the marker, restores.log and the backup skip all still call the one function.
> Tests: `thunderbird/ma1140_r615_identitykey_test.go`, `tbrestore/ma1085_r607_r615_test.go`
> (MA-1140).

- **One mbox file per archive folder.** A folder with subfolders gets its file (empty
  when it holds no messages) and a sibling `NAME.sbd` directory. This is Thunderbird's
  layout as MailArchive already reads it (`internal/source/localwalk.go:502-597`,
  including "a parent with no messages of its own is an EMPTY file … beside its
  container", `:515-519`). Files have no extension: Mozilla's support forum reports that
  a ".mbox" suffix leaves the folder showing as a different name or empty (support
  thread 1457240; not checked in source).
- **No `.msf` file.** Thunderbird builds a folder's summary (`.msf`) when it finds the
  mbox without one. Evidence: Mozilla's support forum says, for copying mail into Local
  Folders, copy only the mbox files with Thunderbird closed and Thunderbird recreates the
  index on start (support threads 1457240, 1075104, 1272388). Not checked in source; lab
  rows MA-1093 (Windows), MA-1098 (macOS), MA-1099 (Linux).
- **The archive's stores.** When the selected records come from one store, the folders
  sit directly under the parent, as above. When they come from more than one, each store
  gets a level of its own, named by the store's label (the mailbox address, or the
  store's own name, `copyexport_archive.go:62-68`), so two stores' "Inbox" never merge.
- **The folder's name and place** come from each record's `Folder`, "the human-readable
  source folder path", the message's most recently seen folder
  (`internal/state/manifest.go:72-75`, `:150-161`), split on "/" with empty parts dropped,
  exactly as the workspace's tree does (`copyexport_archive.go:433-472`). This is what the
  person sees in the folder chooser. (Extract instead groups by the record's stored
  file path, which is slug-cut, `extract.go:270-287`; that would show the person names
  they never typed.) For a Thunderbird source the folder path is the source's own file
  names joined with "/" (`localwalk.go:535`), so a Thunderbird → archive → Thunderbird
  round trip keeps the names.

### 3.5 The new folder's name and collisions

- Name: `Restored — ` + the archive folder's own name (the last element of its path, the
  name Delete asks the person to type, `docs/design-archive-delete.md` §3.1), made safe by
  §4.4. Owner question Q1 (§15).
- Never overwrite: if `NAME` or `NAME.sbd` already exists in Local Folders — compared
  after NFC normalisation, and without case on Windows and macOS — the next of
  `NAME (2)` … `NAME (99)` that is free on both counts. Past 99: refused (W-NAMES), the
  shape `freeCopyName` uses (`internal/app/decryptplan.go:336-351`).
- The name is chosen in the plan, shown to the person, and checked again at the final
  rename, which cannot replace anything (§5.4).

> Folded 2026-11-06 [RT-1, RT-6], lead's call 2026-11-06: there is no final rename any more. The
> name is taken by an **exclusive create**, twice: `Restored — NAME.sbd` by `os.Root.Mkdir` at the
> start (fails if any entry of that name exists → the next ` (n)` is tried, up to 99), and the
> empty parent file `Restored — NAME` by `OpenFile(O_CREATE|O_EXCL)` last. Lab C (Windows, NTFS):
> an exclusive file create fails on an existing name, on a name that differs only in case, and where
> a folder has the name; making a folder fails where a folder or file has the name. .NET's
> `Directory.CreateDirectory` is NOT exclusive (it succeeds on an existing folder); Go's `os.Mkdir`
> calls `CreateDirectoryW` and is expected to fail, which MA-1120's Windows leg proves rather than
> assumes. The free name is chosen in the plan on BOTH counts (no `NAME` and no `NAME.sbd`, NFC- and
> case-blind), so the `Mkdir` normally succeeds first time. The ` (n)` suffix is part of the name
> budget (§4.4 as folded). Q1's recommended answer ("Restored — ARCHIVE NAME", then " (2)") is what
> is built; a date in the name instead is a change to one format string and to the budget.
>
> Settled 2026-11-07 [R6-16] (the R6 adversarial pass): the `Mkdir` above moved to the next " (n)"
> only when `NAME.sbd` was taken; a parent name `NAME` another program made after the re-check was
> met only at the commit, after the whole write and check (W-TAKEN, the tree left "abandoned").
> The writer now looks at the parent name too, before the `Mkdir` and again right after it: taken
> before, it goes to the next " (n)"; taken between the look and the `Mkdir`, the empty container
> it just made is removed as the cleanup removes (only the very folder seen, never through a link)
> and it goes to the next " (n)" — each with its fsynced "name" line, before anything is written.
> A name taken after that is still met at the commit (W-TAKEN). Left: a crash between that
> `Mkdir` and its removal leaves an empty, unmarked container, which Start settles as not
> MailArchive's (case 1, left; the next restore takes " (n+1)"). Test:
> `tbengine/ma1122_r603_r610_r611_r616_test.go` (MA-1120).

## 4. What exactly is written into each mbox file

### 4.1 Flavour: mboxrd

**Decision:** write mboxrd with the existing `interchange.WriteMessage`
(`internal/interchange/mbox.go:41-81`): any line matching `>*From ` gets one more `>`.

Why: Thunderbird's current reader undoes exactly that. In Thunderbird's source
(`mailnews/base/src/MboxMsgInputStream.cpp`, read on searchfox 2026-11-06): the parser
"Assume[s] well formed mboxrd format", and on a body line that is a run of `>` then
`From ` it removes one `>` ("Yes! We need to remove a '>' to unescape it"). The writer
side "Uses reversible "From " escaping, as per mboxrd" (`MboxMsgOutputStream.h`). This
landed in Mozilla bug 1719121 ("So our mbox writing is very strictly mboxrd") and fixed
bug 121947 ("Need to unescape "From " lines … before displaying them"), target milestone
"122 Branch", not backported to ESR 115.

Conditional, stated next to the claim: for **Thunderbird 122 and later** the message
Thunderbird shows is the preserved original byte for byte (lab rows MA-1093, MA-1098).
For **Thunderbird 115 or older**, a message line that begins with "From " shows with a
leading ">" (the older reader did not unquote, bug 1134690: "historically mboxo"). The
profile's `compatibility.ini` names the Thunderbird version that last opened it
(`LastVersion=`; not checked in source). When it reads below 122 the plan says so
(W-OLDTB) and lets the person go on. Lab row MA-1094.

> Folded 2026-11-06 [lab A1]: Thunderbird 153.6.0 un-quotes mboxrd as the source says: a body
> line stored `>From here…` showed as `From here…`, and `>>From there…` showed as `>From there…`.
> The 115-and-older half is still lab row MA-1094.

### 4.2 The separator line

`From ADDRESS Www Mmm dd hh:mm:ss yyyy`, from the message's own From and Date headers,
falling back to `MAILER-DAEMON` and the Unix epoch (`internal/interchange/mbox.go:83-103`).
Thunderbird's reader (same source file) requires a line beginning `From `, takes the
sender as the text to the next space (any text, "eg MAILER-DAEMON"), accepts a date of
22 to 32 characters, and accepts the line even when those do not parse. The postmark is
24 characters of date. No blank line is required before it, though one is written.

### 4.3 Line endings

- The message bytes are written as preserved; nothing is converted.
- For a message that came from an mbox source the preserved bytes have CRLF line endings
  throughout: the archive's mbox reader is `github.com/emersion/go-mbox` v1.0.4
  (`internal/source/localwalk.go:14`, `go.mod:6`), which writes `\r\n` after every line it
  returns and removes one `>` from a line beginning `>From ` (go-mbox `reader.go:59`,
  `:62-64`, `:69`; `mbox.go:8-9`), and the archive keeps those returned bytes as the
  message's Raw (`internal/source/rawstub.go:63-67`). Messages from Microsoft 365 are as
  Graph delivered them.
- **Decision:** the two lines restore adds — the "From " line's ending and the blank line
  after the message — use the message's own line ending (CRLF when its first line ends in
  CRLF, else LF), so a file never mixes them. `WriteMessage` today always writes LF for
  both (`mbox.go:52`, `:79`); slice R3 adds an option for restore and leaves extract's
  output unchanged. Thunderbird's reader accepts LF or CRLF blank lines
  (`SniffEOLAtStart`, same source file). Lab row MA-1093 checks a CRLF and an LF message
  side by side on Windows.

> Folded 2026-11-06 [lab A1 controls]: on Thunderbird 153.6.0 a CRLF mbox (R3) and an LF-only mbox
> (R4) both showed with their counts and content. MA-1093 still runs the restore's own output.

### 4.4 Folder and file names

Each folder name from the archive (untrusted, §9) goes through, in order:
1. `util.SanitizeSegment` (`internal/util/paths.go:44-81`): Windows-illegal characters
   `<>:"/\|?*` and controls replaced, leading and trailing dots and spaces trimmed (so no
   name can start with "." and be hidden), device names prefixed with `_`, at most 120
   characters, a hash suffix when anything changed.
2. Bidirectional-override characters removed (`util.StripBidi`, `paths.go:171-184`), so a
   folder name cannot read backwards in Thunderbird's folder list.
3. Thunderbird's reserved endings: a name ending `.msf`, `.sbd`, `.dat`, `.json`,
   `.html`, `.htm`, `.snm`, `.toc` or `.mozmsgs` gets `_` appended, so Thunderbird does not
   skip it as one of its own files. (MailArchive's own reader skips `.msf` and `.dat`,
   `thunderbird.go:223-226`. The full list is my reading of Thunderbird's habits, not
   checked against its ignore list; slice R3 checks it against Thunderbird's source and lab
   row MA-1095 tries each ending.)
4. Siblings that now spell the same (NFC, and case-blind on Windows and macOS) get
   ` (2)`, ` (3)` in archive order.

Thunderbird shows a folder by its file name when no summary says otherwise, so a name
that had to change shows changed (for example "Q1 Reports~3fa2b1c0" for "Q1: Reports").
The result names how many folder names changed.

> Folded 2026-11-06 [RT-6]: the steps above are replaced by this restore-specific name step
> (`tbrestore.SafeName`, slice R4), run on every component, in this order:
> 1. `util.SanitizeSegment` (unchanged; its known gaps are closed by the steps below, not by
>    changing it, since backups use it — review side finding B).
> 2. Remove what SanitizeSegment keeps: bidirectional controls (`util.StripBidi`), U+200E and
>    U+200F, DEL (U+007F) and the C1 controls (U+0080–U+009F).
> 3. The device names SanitizeSegment misses, matched case-blind on the part before the first dot
>    with trailing spaces ignored: `COM0`, `LPT0`, `COM¹`, `COM²`, `COM³`, `LPT¹`, `LPT²`, `LPT³`,
>    `CONIN$`, `CONOUT$`, `CLOCK$`, and any device name followed by spaces and an extension
>    ("CON .txt") — each gets a leading `_`.
> 4. Thunderbird's skip rules: the full list is taken from Thunderbird's own source
>    (`nsShouldIgnoreFile`) when slice R4 is written, and each rule is tried in lab row MA-1095.
>    Until then the list is the design's endings above plus the review's recollection (a leading
>    `#`, a trailing `~`, and the names `msgFilterRules.dat`, `filterlog.html`, `junklog.html`,
>    `popstate.dat`, `sort.dat`, `mailfilt.log`, `filters.js`, `feeds.json`, `feeditems.json`): a
>    name that would be skipped gets `_` appended (or, for a leading `#`, `_` put first).
> 5. Length, in the units the file systems count: at most 246 **UTF-8 bytes** (ext4, APFS) and at
>    most 246 **UTF-16 units** (NTFS). 246 = 255 − 4 (Thunderbird's `.msf`, or `.sbd`) − 5 (a
>    ` (99)` suffix). Over either: cut at a character boundary, keeping room for the hash suffix
>    (`~` + 8 hex of the component's original bytes), so two cut names stay distinct. The top
>    folder's budget also counts "Restored — " (13 bytes, 11 UTF-16 units).
> 6. **Last**, on the exact names that will be written: siblings that now spell the same — after
>    NFC and case-blind on every platform (a profile can be copied to Windows) — get ` (2)`,
>    ` (3)` … in archive order, up to ` (99)`; past that, the hash suffix instead. This step is
>    what closes SanitizeSegment's merges (".Inbox" / "Inbox." / "Inbox", "CON" / "_CON",
>    "." / ".." / "untitled").
> All of this runs in the **plan**, not during the write: the plan holds every final name, so no
> name can fail part way through a restore. MA-1088 gains each case above.
>
> Folded 2026-11-06 [foldcheck N6] (folding agent's answer): step 5 keeps 5 units for ` (99)`, but
> past ` (99)` step 6 adds the hash suffix, which is 9 (`~` and 8 hex). So when step 6 uses the hash
> suffix, the name is cut again at a character boundary until the name with its suffix is at most
> 251 UTF-8 bytes and 251 UTF-16 units (255 − 4 for `.msf` or `.sbd`), and the Windows path budget
> (§4.5 note) is computed again for that name. This still happens in the plan.
>
> Settled 2026-11-07 [ADV-05] (the R3–R5 adversarial pass): a step between 5 and 6. Windows gives a
> long name an 8.3 short alias ("Long folder name one" → `LONGFO~1`), and an exclusive create of a
> sibling named like that alias finds the long-named folder and fails, so a plan that said OK failed at
> Start. A name that looks like a short alias (`util.IsShortNameAlias`, extract's XC13 rule: up to six
> characters, `~`, digits, an optional extension of up to three) gets the hash suffix of its original,
> on every platform (a profile can be copied to Windows), and counts as changed, so the plan names it
> among the changed names; step 6's own hash suffix is held to the same rule. The result has two
> tildes and is never an alias, so no name can fail part way through. This is a rename in the plan,
> not a refusal: the person cannot rename an archive's folders, and the design's way with every other
> unsafe name is to change it and say so (the builder's reading of the lead's "refuse at plan time with
> words" as "caught at plan time, named in the plan's words"; a refusal instead is one condition in
> `FinalNames`). Built: `notShortAlias` in `names.go`; MA-1088's ADV-05 tests, and on the Windows VM
> both siblings restored.

### 4.5 Path length on Windows

A Windows program that does not opt into long paths cannot open a path of 260 characters
or more. Whether Thunderbird opts in is not checked. **Decision:** every path the restore
writes — the deepest file, with its `.sbd` parts, under the longer of the staging name and
the final name — stays under 260 characters on Windows. Over budget, the names on that
branch are shortened (each cut to 30 characters plus the hash suffix), deepest first;
still over, that folder is not restored and is named in the plan with why (W-LONGPATH).
macOS and Linux have no such limit at these lengths. Lab row MA-1092 (a deep tree under a
long Windows user name).

> Folded 2026-11-06 [RT-6]: the Windows budget, exactly. Counted in UTF-16 units from the resolved
> Local Folders path, for each entry the restore makes, using the longer of its hidden working name
> (§5.4 as folded) and its final name: a **file** path, plus Thunderbird's own `.msf` beside it
> (4 more), is at most **259**; a **directory** path is at most **247** (`CreateDirectoryW`'s limit
> of 248 with its terminator). Over budget: the shortening above, then W-LONGPATH. The restore
> marker and the parent file are counted too. Computed in the plan (§4.4 note), so nothing fails
> mid-write for a path. MA-1126 tests the exact boundaries (247/248, 259/260, with and without
> `.msf`).
>
> Folded 2026-11-06 [foldcheck M5] (lead's call): "using the longer of its hidden working name (§5.4
> as folded) and its final name" above is superseded by [foldcheck M5]. There are no working names,
> so each entry is counted under its final name only. The marker and the made-list
> (`.mailarchive-restore-made`, §3.4 foldcheck note) are counted like any other file.

### 4.6 Read and unread, X-Mozilla-Status

**Decision:** add nothing. No `X-Mozilla-Status` header is written, because adding one
would change the message's bytes.
- A message that came from Thunderbird carries Thunderbird's own `X-Mozilla-Status` in its
  preserved bytes (the archive reads it for the read flag, `internal/source/mbox.go:280-293`),
  so Thunderbird should show it read or unread as it was when backed up. Not checked; lab
  row MA-1093.
- A message from Microsoft 365 carries none, so Thunderbird should show it unread. The
  result says: "Messages restored from Microsoft 365 show as unread. To mark them read,
  right-click the folder in Thunderbird and choose Mark Folder Read."
- One case to state: a message Thunderbird had deleted from a local folder but not yet
  compacted away is still in the mbox, marked deleted in `X-Mozilla-Status` (bit 0x0008).
  The archive's reader reads only the read bit (`mbox.go:283-293`), so such a message may
  be in the archive. Restored, Thunderbird would keep it hidden. The engine counts those
  (one header read per message, already in memory) and the result says "N of them were
  deleted in Thunderbird before they were backed up, so Thunderbird keeps them hidden."
  (See §16 side finding 2.)

> Folded 2026-11-06 [RT-19, lab A1, Q2]: lab, Thunderbird 153.6.0: a message carrying
> `X-Mozilla-Status: 0001` showed as read and two with none showed unread, as above. Deleting a
> message did not rewrite the mbox at once (the folder cache recorded `expungedBytes`), so removal
> waits for compaction. The deleted-flag sentence therefore becomes: "N of them were deleted in
> Thunderbird before they were backed up, so Thunderbird keeps them hidden, and may remove them
> from the restored folder the next time it compacts it. The archive keeps them." (Whether
> Thunderbird hides a placed message with the deleted bit is not yet run; lab row MA-1093 adds one.)
> Q2's recommended answer (add nothing; tell the person how to mark a folder read) is what is
> built; marking everything read instead would add a header to each message, which breaks "the
> preserved original bytes" and the post-write check (§5.4 note), so it is not a small change and
> would need its own design note.

### 4.7 Large folders

- Messages are streamed one at a time; a message larger than extract's 2 GiB cap is not
  restored and is named (`internal/app/extract.go:34-39`).
- One file per folder, however big. Thunderbird reads mbox files over 4 GiB on NTFS, APFS
  and ext4 (my understanding, since about Thunderbird 12; not checked; lab row MA-1121).
- A FAT32 drive cannot hold a file of 4 GiB or more. When the Local Folders volume is
  FAT32 (the volume's file system name, `readIdentity`) and a folder's planned size
  reaches 4 GiB − 1 MiB, the folder is split into `NAME`, `NAME (part 2)` … at message
  boundaries, and the plan says so.

> Folded 2026-11-06 [RT-20], lead's call 2026-11-06: **no splitting.** On a FAT32 Local Folders
> volume (the file system name `readIdentity` reports), a folder whose planned mbox reaches
> 4 GiB − 1 MiB is **refused in the plan** with W-FAT32 (§7.5), naming the folder and its size;
> the other folders may still be chosen. The split bullet above is dropped. (exFAT has no such
> limit and is not refused.)

## 5. Thunderbird must be closed, and stays closed

### 5.1 Detecting it

Thunderbird counts as running when any of these is true (any one is enough, so a mistake
only refuses):
- A. the profile's lock is held: `thunderbird.Running(profile)` — a `lock` symlink naming a
  live pid on macOS and Linux, `parent.lock` held open on Windows
  (`internal/thunderbird/thunderbird.go:118-165`); a lock naming a pid that was reused by
  another program still counts, as presync does (`internal/presync/running.go:8-36`);
- B. any Thunderbird process runs for this user (presync's process probe on Windows and
  macOS, `env_windows.go:118-135`, `env_darwin.go:351`; Linux has none today, so slice R2
  adds one that reads `/proc/*/exe` and matches the names presync accepts,
  `running.go:44-52`). B refuses even when Thunderbird runs another profile: the words
  say to close Thunderbird, and closing it is the person's whole step.

Refused with W-RUNNING (§10). MailArchive never starts, quits, signals or kills
Thunderbird for a restore.

> Folded 2026-11-06 [RT-2, RT-14, RT-17], lead's call 2026-11-06 (RT-2):
> - **Order.** Checks A and B, and every other check of §5.3, run BEFORE the restore takes
>   Thunderbird's profile lock (§5.2). After the take, only B (the process list) is checked again,
>   because A would now see the restore's own lock. A failed take counts as running.
> - **Another MailArchive restore first.** Before A and B, the restore checks its own MailArchive
>   restore lock for this profile (§5.2 note). Held → W-RESTORING ("Another restore into this
>   Thunderbird profile is running…", §7.5), so a terminal and Desktop restoring at once are not
>   told to close a Thunderbird that is not open.
> - **macOS:** B refuses on `rawThunderbirdCount` (`env_darwin.go:373`), the count before the
>   filter that drops processes it cannot fully read. **Linux:** B is new code (`/proc/*/exe`, as
>   above), in slice R3, with its own leg in MA-1087.
> - **Windows:** B matches `thunderbird.exe` for every user on the machine
>   (`env_windows.go:268-292`). That stays (safe direction); W-RUNNING gains a sentence for when no
>   Thunderbird window shows (§7.5). Thunderbird's background update task may match too; lab row
>   MA-1090 notes whether it does.
> - Error words come from §7.5, not §10 (the "(§10)" above is an off-by reference).

### 5.2 Keeping it closed while writing

**Decision:** for the length of the write, the restore holds Thunderbird's own profile lock,
the way Thunderbird does, so a Thunderbird started meanwhile finds the profile in use and
does not open it:
- Windows: open the profile's `parent.lock` with no sharing (creating it if absent, as
  Thunderbird does); the handle closes when the restore ends, however it ends.
- macOS and Linux: create the `lock` symlink naming this host and this process's pid
  (`thunderbird.go:121-126` describes the format), then take an OS lock on `.parentlock`;
  both are removed or released at the end. A crash leaves the symlink naming a dead pid,
  which Thunderbird and `thunderbird.Running` both treat as stale
  (`thunderbird.go:121-126`).
- A pre-sync that wants to start Thunderbird on this profile meanwhile sees the lock held
  and treats Thunderbird as running (`running.go:23-36`), so it leaves it alone.

These lock files are the ONE place outside Local Folders where the restore writes, and they
are Thunderbird's own lock files, not mail. That the lock keeps Thunderbird out is my
understanding of Mozilla's profile lock, not checked; lab row MA-1090 starts Thunderbird
mid-restore on Windows and checks it says the profile is in use and opens normally
afterwards. If the lab shows otherwise, the belt in §5.4 still keeps Thunderbird from ever
seeing a half folder, and this subsection is dropped.

> Folded 2026-11-06 [RT-2, RT-3], lead's call 2026-11-06. This subsection is replaced by:
> 1. **Windows: hold `parent.lock` exactly as Thunderbird does.** `CreateFileW` on the profile's
>    `parent.lock`, read/write access, share mode 0 (no sharing), `OPEN_ALWAYS` (made if absent,
>    as Thunderbird makes it; never written to; left in place afterwards, as Thunderbird leaves
>    it). The handle is closed on every exit path and by Windows when the process ends.
>    **Lab B, 2026-11-06 (Thunderbird 153.6.0, holder `powershell.exe`):** with the file held,
>    a Thunderbird started did not open the profile; it showed a dialog titled "Close Thunderbird":
>    "Thunderbird is already running, but is not responding. The old Thunderbird process must be
>    closed to open a new window." with **Close Thunderbird** (default) and **Cancel**. Pressing
>    Close Thunderbird closed only the new Thunderbird, not the holder. On release, and when the
>    holder was killed, Thunderbird opened normally at once: nothing stale. Not yet run with
>    `mailarchive.exe` as the holder: lab row MA-1090.
> 2. **macOS and Linux: Thunderbird's own lock type only, and no `lock` symlink.** Open
>    `.parentlock` (create if absent, never written to) and take a POSIX write record lock on the
>    whole file, non-blocking: on Linux the per-handle kind `F_OFD_SETLK` (it conflicts with
>    Mozilla's `F_SETLK` and is not dropped when some other descriptor of the file closes); on
>    macOS `F_SETLK`, the only kind there, with one owner type in `tbrestore` that is the sole
>    opener of `.parentlock` in the process and a test that nothing else opens it while held.
>    Before writing this code, slice R3 reads Mozilla's `nsProfileLock.cpp` and records what lock
>    Thunderbird takes and what its symlink check does when no symlink exists. Never the `lockfile`
>    package on Thunderbird's files (it is `flock`, which Linux keeps apart from `fcntl` locks, and
>    it writes a holder line). Whether this keeps a starting Thunderbird out is **not yet run** on
>    either platform: lab row MA-1134 (macOS and Linux). If MA-1134 shows it does not, the
>    restore still never shows a half folder (the parent file comes last, §5.4 as folded) and
>    the subsection says so; the lock is then kept only as the in-use signal for presync.
> 3. **A MailArchive restore lock.** Before Thunderbird's lock, the restore takes
>    `restore-<first 16 hex of SHA-256 of the profile's canonical path>.lock` in MailArchive's
>    settings folder with `lockfile.Acquire` (an OS lock, released when the process ends, so never
>    stale). Presync's `detectRunning` (`presync/running.go:23`) treats `lockfile.Held` on it as
>    "Thunderbird is in use" and so never starts Thunderbird on that profile during a restore; a
>    second restore into the profile is refused with W-RESTORING; and the backup-side skip (§7.2
>    note) reads it. This is a change to shipped presync code: in slice R3, with a presync row
>    (MA-1140) and inside the R3–R5 adversarial pass.
> 4. **Lock order and release.** Take: archive lock (§7.1) → MailArchive restore lock →
>    Thunderbird's profile lock. Release in reverse, on success, error, Cancel, Shut down and a
>    panic recovered by the job runner (a long-lived Desktop must not keep a lock after a failed
>    restore). MA-1127 proves each path by a child process taking each lock afterwards.
> 5. **Words for a person who opens Thunderbird during a restore** [lab finding 3], lead's words,
>    owner-delegated 2026-11-02: since Thunderbird's own dialog offers "Close Thunderbird" first, the
>    Desktop panel and the terminal say, for the whole time the restore runs: "While this runs,
>    leave Thunderbird closed. If you open it and it says Thunderbird is already running, choose
>    Cancel and wait for the restore to finish." (W-WAIT, §7.5.) MailArchive is not a Thunderbird
>    process, so choosing Close Thunderbird would not stop the restore (lab B), but the person cannot
>    know that, and Cancel is the safe answer.
>
> Folded 2026-11-06 [foldcheck M4, N9] (lead's calls):
> - [M4] Item 3's "MailArchive's settings folder" means MailArchive's default settings folder,
>   `job.Dir()` (`internal/job/job.go:61-67`), always, never the folder of a `-config` file. Presync
>   and scheduled backups never take `-config` (`cmd/mailarchive/main.go:1296`), so they always find
>   the lock this way (§7.4 foldcheck note). The lock's name uses `thunderbird.ProfileKey`
>   (§3.4 foldcheck note).
> - [N9] Lab row MA-1134 (item 2's macOS and Linux lock) runs **before slice R3**, not at R8
>   (§13 foldcheck note). If it shows the lock does not keep Thunderbird out, item 2's fallback is
>   written into this design before any R3 code.

> Folded 2026-11-07 [Mac lab of 2026-11-06, `docs/lab/restore-thunderbird-macos-2026-11-06.md`]
> (lead's calls 2026-11-07, built in slice R3). MA-1134 held on macOS (Thunderbird 159.0) and on
> Linux (153.6.0esr, Docker `--headless`), so item 2 stands. Three changes around it:
> - **(a) Check A gains a non-taking lock probe** (§5.1). Mac Thunderbird 159 makes no `lock`
>   symlink, so the symlink check alone is blind there. On macOS and Linux check A also asks who
>   holds `.parentlock` (`F_GETLK`; `F_OFD_GETLK` on Linux), taking nothing. At the plan the probe
>   opens `.parentlock` read-only, never creating it, and closes it. At Start the restore's one
>   `ProfileLock` opens `.parentlock` **once** (created only when absent, never written to), asks
>   `F_GETLK` through that handle, takes the lock on that same handle, and holds it to the end.
>   **The restoring process never opens `.parentlock` twice**: on macOS closing any second handle
>   silently drops the lock (the lab saw Thunderbird get in). A process-wide registry in
>   `tbrestore` records each profile this process has a `ProfileLock` on; every probe of such a
>   profile answers from the registry ("held by this process") and opens nothing, and the
>   registry's mutex covers each probe's own open and close. MA-1127 proves the lock survives the
>   restore's own probe (another process still sees it held). Built: `tbrestore.CheckRunning`,
>   `tbrestore.TakeProfileLock`, `tblock_*.go`.
>   - Read before the code, as item 2 asks (Mozilla's `toolkit/profile/nsProfileLock.cpp`, gecko-dev
>     mirror, read 2026-11-07, not run): on macOS and Linux Thunderbird opens `.parentlock`
>     `O_WRONLY|O_CREAT|O_TRUNC` and takes a whole-file `F_WRLCK` with `F_SETLK` after an
>     `F_GETLK`; on macOS it falls back to a symlink *at `.parentlock`* only when that fcntl fails for
>     a reason other than access-denied (so a working Mac has no `lock` symlink, as the lab saw); on
>     Linux, after the fcntl succeeds, it also makes the old `lock` symlink. On Windows it opens
>     `parent.lock` with `CreateFileW(GENERIC_READ|GENERIC_WRITE, share 0, CREATE_ALWAYS)`. A held
>     fcntl lock is access-denied, so no fallback is tried and Thunderbird refuses. The restore's
>     symlink check therefore reads both `lock` and a symlink at `.parentlock`.
>   - **Follow-up, not changed in this slice:** presync's macOS running check (`macEnv.lockPID`,
>     `env_darwin.go`) and `thunderbird.Running` (used before `prefs.js` is edited) look only for the
>     `lock` symlink (and Windows' `parent.lock`), so they have the same blind spot on Mac
>     Thunderbird 159. Presync is mitigated by its process-list check (signal B), which fires for any
>     running Thunderbird of this user; `thunderbird.Running`'s comment ("on Unix/macOS Mozilla
>     writes a `lock` symlink") is wrong for macOS. Recorded as a follow-up for presync's owner; the
>     restore does not depend on either.
> - **(b) W-WAIT's words are platform-neutral on macOS and Linux** (lead's words, owner-delegated
>   2026-11-02): the Mac dialog's words and buttons are not yet seen (MA-1134 pending item), and
>   Linux showed a message, not a Cancel. So on macOS and Linux W-WAIT reads: "While this runs,
>   leave Thunderbird closed. If Thunderbird says it is already open or running, close that message
>   and wait for the restore to finish." Windows keeps naming Cancel (lab B). §7.5's W-WAIT row is
>   the Windows form. Built: `tbrestore.WaitWords`.
> - **(c) A refused Thunderbird writes one file into the profile** on macOS and Linux:
>   `Telemetry.FailedProfileLocks.txt` (a counter). It is Thunderbird's, outside Local Folders, and
>   never the restore's. R40's "only adds" is a claim about what MailArchive writes, so it is
>   untouched; any test or lab row that holds the profile byte-identical across a refused
>   Thunderbird allows this one file and no other (`tbrestore.ThunderbirdRefusalFile`).

> Settled 2026-11-07 [ADV-01] (the R3–R5 adversarial pass, `docs/review-restore-thunderbird-adversarial-r3r5.md`):
> (a)'s registry "records each profile this process has a `ProfileLock` on" by **the lock file's
> identity**, not by the profile path's spelling: device and inode on macOS and Linux (`Lstat` of
> `.parentlock`, then `fstat` of the one handle), volume serial number and file index on Windows
> (from a handle opened for its attributes only, which no sharing mode refuses). The same lock file
> reached by another spelling (macOS's `/System/Volumes/Data` firmlink) or through another profile
> whose `.parentlock` is a hard link to it is answered from the registry and never opened. The one
> race left — the name swapped for the held file between that identity check and a probe's open —
> is closed too: a probe or second open that finds, by `fstat`, that it opened a held file keeps
> that descriptor with the lock, never closing it before `Release`, and answers "held by this
> process". Built: `tblock.go` (`lockKey`, `heldHere`), `tblock_unix.go` (`keepIfHeld`),
> `tblock_windows.go` (`lockFileKey`). MA-1127's ADV-01 test
> (`ma1127_adv01_identity_test.go`) proves it with a hard link everywhere and the firmlink on macOS.

### 5.3 Checked again right before writing

At "Restore", under the archive's lock and the profile lock, before the first byte:
Thunderbird not running (§5.1), the Local Folders directory the same one (identity, §3.2
step 4), the chosen name still free (§3.5), free space still enough (§6.3), the archive's
manifest unchanged since the plan (size, modification time, SHA-256, as Delete's plan does,
`docs/design-archive-delete.md` §3.1). Any difference: "This changed since you clicked
Check: WHAT. Click Check again." and nothing is written.

> Folded 2026-11-06 [RT-2, RT-15]: "under … the profile lock" is replaced. The re-check runs under
> the archive lock and the MailArchive restore lock, and BEFORE Thunderbird's profile lock is taken
> (§5.1 note): Thunderbird not running (A and B), the profile list entry still the same folder and
> identity (§3.1 step 6 note), Local Folders the same (identity), the chosen name free on both counts,
> free space, the manifest unchanged, the Store-install and network checks as in the plan. Then
> Thunderbird's lock is taken (a failed take = W-RUNNING, nothing written), then B is checked once
> more. Only then is the first byte written.

> Settled 2026-11-07 [R6-05, R6-06, R6-11, R6-13] (the R6 adversarial pass):
> - **[R6-05]** Thunderbird's lock is taken by path, and only Local Folders was pinned again after
>   it; a profile folder moved aside in that window, with a decoy at its path (Local Folders
>   elsewhere), got the decoy locked and left the real profile open to Thunderbird. After the take,
>   `ProfileLock.Binds` reads the profile folder's identity, the lock file's identity at its lock
>   name (it must be the very file the lock holds), and the folder's identity again; any difference
>   is W-CHANGED, the lock released, nothing written. Left: the window between those reads and the
>   writes is a same-user program's, closed on Windows (a folder holding an open file cannot be
>   renamed) and narrow elsewhere.
> - **[R6-06]** Only the chosen profile's lock was taken, so a second listed profile whose prefs
>   name the same Local Folders could be opened by Thunderbird during the write. The plan now lists
>   every other profile whose Local Folders is this one by identity (W-RUNNING when one is open) and
>   says so ("Another Thunderbird profile, 'NAME' (DIR), uses this same Local Folders…"); Start
>   takes each one's Thunderbird lock too, bound as above, and the re-check compares the list.
>   (Profiles found through profiles.ini only: one typed with -profile and not listed is not known.)
> - **[R6-11]** The re-check reads prefs.js again and repeats the store-kind check (§3.3) and the
>   inside-an-encrypted-archive test (§3.2 step 3); a difference is W-CHANGED, naming it.
> - **[R6-13]** See §7.1's note: an archive whose lock cannot be taken is refused at the plan in
>   words, or — on a read-only medium — read without it; the re-check compares which.
> Tests: `tbengine/ma1127_r605_r606_r613_test.go` (MA-1127), `ma1122_r603_r610_r611_r616_test.go`
> (MA-1125).

### 5.4 Thunderbird never sees a half folder

All writing happens under two hidden names in Local Folders:
`.mailarchive-restore-ID` (the empty parent file) and `.mailarchive-restore-ID.sbd/`
(everything below it, already under final names). At the end, two no-replace renames in
the Local Folders directory, through its handle (`copyexport_place_*.go`):
1. `.mailarchive-restore-ID.sbd` → `Restored — NAME.sbd`
2. `.mailarchive-restore-ID` → `Restored — NAME`

Thunderbird finds a folder by its file, so the folder appears whole at rename 2. If rename 2
fails (the name was taken in that instant), rename 1 is undone and the next free name is
tried. Two things here are not checked and are lab row MA-1091: that Thunderbird ignores
entries whose names begin with "." (so a staging pair left by a crash is never shown), and
what Thunderbird does with a `NAME.sbd` that has no `NAME` file (the instant between the two
renames, or a crash there). If Thunderbird does show dot-named entries, the staging pair
moves to the profile folder (or, for a Local Folders on another drive, beside it) and the
rename helper is widened to two directory handles (`renameat2`, `renameatx_np` and
`FILE_RENAME_INFORMATION` all take a target directory; a modest change to the existing
helper).

> Folded 2026-11-06 [RT-1, RT-4, RT-12], lead's call 2026-11-06. **The two renames above are
> dropped: no folder is ever renamed.** Why: on Windows the reused helper refuses a directory
> (`FILE_NON_DIRECTORY_FILE`, `copyexport_place_windows.go:100-102`), and lab C showed NTFS refuses
> a folder rename while any process holds a file inside open, even one that allows delete (as
> virus scanners do). The **lone-`.sbd` fallback** in RT-1 is dropped too: lab A3 showed
> Thunderbird 153 ignores a `Q.sbd` with no `Q` file (not shown, no `.msf`, no `folderCache.json`
> entry, left as placed), and lab A5 showed a tree built in this order appears whole once the parent
> file exists. A crash before the last step therefore leaves a folder Thunderbird does not show;
> the next attempt cleans it, or finishes it, only when it carries this restore's marker (§3.4
> note, §8 note). The hidden-name questions (T3) now concern files only, and lab A4 answered them
> for Windows.
>
> The sequence, every step through the `*os.Root` opened on Local Folders, every name created
> exclusively (numbers in brackets are RT-4's fsync points):
> 1. **[1]** Append the "started" line to `restores.log` and fsync it: restore ID, date, archive
>    folder, profile, Local Folders path and identity, the final name chosen, the working-name
>    prefix `.mailarchive-part-ID-`, planned counts. (No folder names for an encrypted archive.)
> 2. `Root.Mkdir("Restored — NAME.sbd", 0o700)`. It fails if any entry of that name exists; then the
>    next free ` (n)` is tried, and a "name" line naming it is appended and fsynced before going on.
> 3. Create `.mailarchive-restore` (the marker, §3.4 note) inside it, write its line, fsync it.
> 4. For each folder in plan order, parents before children: `Mkdir` its `NAME.sbd` when it has
>    subfolders; write its messages into `.mailarchive-part-ID-N` (created exclusively in the
>    folder's directory), keeping in memory, per message, the SHA-256 and length of the original
>    bytes handed to the writer; **[2]** fsync and close the file; then place it at its final name
>    with the existing no-replace *file* helper (`copyexport_place_*.go`), retried for about 5
>    seconds on a Windows sharing violation, as §8's antivirus row. A folder with no messages of its
>    own gets an empty file created exclusively under its final name.
> 5. **[3]** fsync every directory made (a no-op on Windows, as `copyexport_fs.go:139-141` states).
> 6. **Post-write check** [RT-12], lead's call: a full re-read of every written mbox, by its final
>    name, through the root: it exists, its size is the bytes written, and re-parsed with the same
>    reader MA-1089 ports from Thunderbird's read rules (separator, mboxrd un-quoting, LF/CRLF) it
>    yields exactly the planned messages in order, each with the SHA-256 and length recorded in
>    step 4 — that is, the archive's originals, which were themselves checked against their recorded
>    fixity as they were read (§6.1). Any file missing, short or different (antivirus quarantine,
>    a failing disk): the restore stops here, nothing is made visible, the tree is removed (§8
>    note), and the result names the file with the remedy (W-CHECKFAIL, §7.5). Cost: one more read
>    of the restored data, stated in the plan ("then checks every message it wrote").
> 7. Append a "checked" line and fsync it.
> 8. **The commit:** create the empty parent file `Restored — NAME` with
>    `OpenFile(O_CREATE|O_EXCL)`, then fsync it. Lab C: this fails on an existing name, a
>    case variant, and a folder of that name. If it fails because the name now exists (made by
>    someone else during the restore), the tree that the started line names is removed and the
>    restore refuses (W-TAKEN, §7.5); it never attaches the tree to someone else's folder.
> 9. **[5]** fsync the Local Folders directory.
> 10. **[6]** Append the "ended" line (result, counts, final name) and fsync it.
>
> Thunderbird is closed and held out by the lock throughout (§5.2), so it reads nothing while this
> runs; the parent file is what makes the folder appear, in one step. RT-4's fsync point [4] is
> step 8.

> Folded 2026-11-06 [foldcheck M5, M1, M2, N10] (lead's calls for M5, M1 and M2). This note amends
> the folded sequence above; where the two disagree, this note wins.
> - **Superseded by [foldcheck M5]:** step 4's hidden working files `.mailarchive-part-ID-N`, their
>   placement by the no-replace file helper and its retry, and step 1's "working-name prefix". Each
>   mbox file is now created **directly under its final name** with `OpenFile(O_CREATE|O_EXCL)`
>   through the root. Why that is enough: the whole container stays invisible to Thunderbird until
>   the parent file is made, last (lab A3 on Windows; macOS and Linux lab row MA-1091), so no name
>   inside it needs hiding. It also removes every per-file rename, which antivirus can block on
>   Windows, and the FAT/exFAT rename window (§9 foldcheck note). The sentence above that the
>   lone-`.sbd` fallback "is dropped too" now holds: nothing in the sequence builds it.
> - **Superseded by [foldcheck M2]:** "the next attempt cleans it, or finishes it". An unfinished
>   restore is **never finished** by a later attempt. It is always cleaned up, and the person
>   restores again (§8 foldcheck note).
> - **The sequence as it now stands.** Every step goes through the `*os.Root` opened on Local
>   Folders; every name is created exclusively. Bracketed numbers are RT-4's fsync points.
>   1. **[1]** Append "started" to `restores.log` and fsync it (as step 1 above, without a
>      working-name prefix).
>   2. `Root.Mkdir("Restored — NAME.sbd", 0o700)`; a taken name gives the next ` (n)` and a fsynced
>      "name" line, as step 2 above.
>   3. Create the marker, write its one line, fsync it.
>   4. Create the made-list `.mailarchive-restore-made` (§3.4 foldcheck note), fsync it, and fsync
>      the container directory, so the list itself is on disk before any entry is made.
>   5. For each folder in plan order, parents before children, and for each entry it needs (its mbox
>      file `NAME`; its `NAME.sbd` directory when it has subfolders): append the entry to the
>      made-list and fsync the list **[M1]**; then create the entry (`Root.Mkdir`, or
>      `OpenFile(O_CREATE|O_EXCL)`). Write the folder's messages straight into its file, keeping in
>      memory, per message, the SHA-256 and length of the bytes handed to the writer; **[2]** fsync
>      and close the file. A folder with no messages of its own gets its empty file the same way.
>   6. **[3]** fsync every directory made (a no-op on Windows).
>   7. The post-write check [RT-12], as step 6 above, reopening each file by its final name. A
>      Windows sharing violation on that reopen (antivirus scanning the new file) is retried for
>      about 5 seconds, then W-CHECKFAIL.
>
>      > Settled 2026-11-07 [R6-10] (the R6 adversarial pass): Cancel, Ctrl-C and Shut down were
>      > heard only before each folder and each message of step 5, so a stop during this check,
>      > which re-reads every byte, was ignored and the restore finished (honestly, but the person
>      > who pressed Ctrl-C saw nothing happen and pressed it again, R6-09). The check now stops
>      > between files and between messages, and once more before step 8; a stop there is a stop
>      > with the one cleanup, as before step 8 (outcome "stopped", exit 130/143). The caller is
>      > told when the check begins (`WriteInput.Checking`; the command line says "Checking what
>      > was written…"), so the pause is explained. Test: `tbengine/ma1122_r603_r610_r611_r616_test.go`
>      > (MA-1122).
>   8. Append "checked" and fsync it. It records that the check passed. A later attempt uses it only
>      to tell a committed restore from an unfinished one (§8 foldcheck note, case 2), never to finish
>      one (M2).
>   9. **[4] The commit:** create the empty parent file `Restored — NAME` with
>      `OpenFile(O_CREATE|O_EXCL)` and fsync it, as step 8 above. If that fails because the name
>      exists, first append a **"taken"** line and fsync it, then clean up (§8 foldcheck note) and
>      refuse with W-TAKEN. The "taken" line is what tells a later attempt that a parent file of that
>      name is not this restore's.
>   10. **[5]** fsync the Local Folders directory, then **[6]** append "ended" and fsync it. An error
>      (not a crash) at [5] or [6] comes after the folder is visible and complete: the result says the
>      restore finished and names the error, and nothing is removed.
> - **Cancel, Shut down, a failure and W-CHECKFAIL** clean up with the same code as a crash, from the
>   made-list (§8 foldcheck note). There is one cleanup rule.
> - **[N10]** (folding agent's answer): the reader the post-write check uses is product code in
>   `internal/interchange`, beside `WriteMessage`, since that package owns the mbox format (for
>   example `interchange.ReadMboxrd`). It is not a test helper. MA-1089 tests it against cases taken
>   from Thunderbird's read rules and as the writer's round trip; lab row MA-1093 is what proves both
>   against Thunderbird.

> Folded 2026-11-06 [foldcheck2 P1, N-c] (lead's calls). This note amends the foldcheck sequence
> above; where the two disagree, this note wins.
> - **Step 9, a taken name [P1].** "then clean up (§8 foldcheck note) and refuse with W-TAKEN" is
>   superseded by [foldcheck2 P1]: an entry named `Restored — NAME` is now there, so the one cleanup
>   removes nothing (§8 foldcheck2 note, case 3a). The run appends "taken", then "abandoned", keeps
>   the marker and the made-list, and gives W-TAKEN's new words (§7.5 foldcheck2 note): the restored
>   tree, complete and checked, stays, and Thunderbird can show it inside that other folder. The
>   older sentence in step 8 above, "the tree that the started line names is removed … it never
>   attaches the tree to someone else's folder", is superseded by [foldcheck2 P1] for the same
>   reason. Why accept the untidy result: whatever made that name may also have written into the
>   container, so removing nothing is the only choice that can never delete mail that is not
>   MailArchive's.
> - **Step 9, the parent file's fsync fails after a successful create [N-c].** Not covered before.
>   The run re-checks once: an `Lstat` through the root must show `Restored — NAME` as a regular,
>   empty file. If it does, the restore counts as committed: it goes on to step 10 ([5], then "ended"
>   [6]), and the result says the restore finished and names the fsync error, as step 10 does for
>   an error at [5] or [6]. If the re-check fails (the file is absent, not a regular file, or cannot
>   be checked), the run appends "abandoned", removes nothing (the parent file is never removed,
>   §8 rule 7, and the tree passed its check), and gives W-COMMITERR (§7.5 foldcheck2 note). A crash
>   in that instant, before either line, is settled at the next Start like any other (§8 foldcheck2
>   note): a regular parent file there is case 2, none is case 3b.
> - **Step 10's "nothing is removed"** stands, and is now the general rule: no step after the
>   parent file's create removes anything.
> - **The post-write check's failure [RT-12].** Step 6 of the RT-1 sequence, "the restore stops here,
>   nothing is made visible, the tree is removed (§8 note)", now means: the run's one cleanup, in §8
>   foldcheck2's decision order. When its scan is clean (the usual case, including a file antivirus
>   took away, which is simply absent), the tree is removed and the result is W-CHECKFAIL as
>   written. When the scan finds an entry the list does not name (for example a file antivirus left
>   in place of the one it took), nothing is removed and W-CHECKFAIL's result gives W-LEFT's words in
>   place of "the tree removed" (§7.5 foldcheck2 note). "Nothing is made visible" stands: the parent
>   file is not made.

## 6. What can be restored, and the plan

### 6.1 Faithful only

A record is restorable only when its preserved original is present and passes the gate
extract uses: a safe recorded path, a regular file with no link on the way, read bounded,
matching its recorded fixity when there is one (`extract.go:321-399`); for an encrypted
archive, a sealed original that decrypts and matches (`extract.go:401-441`). Nothing is
ever made up from the archive's pages.

What cannot be restored, said before starting, with the counts:
- **Outlook archives** (`.pst`/`.ost` stores): Outlook items keep no original message
  (README "Migrating the archive out", `README.md:1208-1209`; the workspace marks such a
  store, `copyexport_archive.go:54-60`, `:499-500`). W-PST.
- **Archives made without keeping originals** (`-raw` off; the job's "keep originals"
  setting, `internal/job/job.go:38`): W-NORAW, with the remedy (turn on keeping originals
  for future backups while the source still exists).
- **A record whose original is damaged or missing**: counted, and the first ones named, as
  extract does (`extract.go:617-627`), with "run Maintenance → Check integrity".
- **A record whose original is over 2 GiB** (§4.7), **a folder over the Windows path
  budget** (§4.5).
- **Records with a recorded fixity mismatch** are never restored; **records with no recorded
  fixity** are restored "as stored" and counted (`extract.go:387-389`, `:722-724`).

Counting before the start costs one Lstat per record for a plain archive (the shared
predicate `emlPresent`, `internal/app/extractable.go:9-31`, the same one `status` and
`verify` use), and a look at `rec.Sealed.EML` for an encrypted one
(`internal/keyring/extract.go:39-41`). No message is read to plan.

> Folded 2026-11-06 [RT-19, RT-12]: because planning reads no message, the plan CANNOT count
> damaged originals or fixity mismatches in advance; "said before starting, with the counts" above
> holds only for Outlook stores, `-raw` off, missing originals, the 2 GiB cap and the path budget.
> The plan says instead: "Originals that turn out to be damaged are found while restoring and
> named in the result." A message whose fixity fails while being read is left out of its mbox and
> counted; it never reaches the post-write check's list, so that check compares only what was
> meant to be written (§5.4 note, step 6).

### 6.2 Encrypted archives

- Restore is offered only while the archive is unlocked on this computer; the start takes a
  read lease on the key, as the unencrypted copy does (`encryption_copy.go:106-115`), so a
  Lock after the start does not stop it, and Shut down does (§8).
- Each original is decrypted in memory and written straight into the staging folder inside
  Local Folders (§5.4). No plaintext is written anywhere else: no temp file in the cache or
  scratch folders, nothing in the archive.
- The folder names, decrypted in memory, become plain file names in Thunderbird.
- The plan says, in its own line: "The restored mail in Thunderbird is NOT encrypted: anyone
  who can open this Thunderbird profile can read it." The Restore button needs a tick in "I
  understand the restored mail will not be encrypted." Owner question Q8.
- The archive's lock names no verb, as extract's does for an encrypted archive
  (`extract.go:161-167`).
- The restore record (§7.4) for an encrypted archive keeps counts, the destination and the
  date, but no folder names.

> Folded 2026-11-06 [RT-8, RT-18, Q8, O2, Q6]:
> - **The tick is enforced on the server.** `POST /api/restore-tb/start` refuses an encrypted plan
>   whose request lacks `notEncryptedOK: true` (W-TICK, nothing written); the page's tick is only
>   how the person says it. Command line: at a terminal it asks "The restored mail will not be
>   encrypted. Restore it? [y/N]"; without a terminal it needs `-yes -not-encrypted-ok`.
> - **Ask twice, checked on the server, when the Local Folders may leave this computer** (owner
>   question O2, recommended answer, what is built): the copy's own test,
>   `CopyExportEnv.copyLeaves` (`internal/app/decryptplan.go:123-147`: synced by a cloud service,
>   possibly iCloud Desktop/Documents, removable or external drive, network share), run on the
>   resolved Local Folders path. Any reason → the plan lists the reasons, and Start needs both
>   `notEncryptedOK` and `confirmAgain: true` (the copy's rule, `encryption_copy.go:98`). Command
>   line: refused without a terminal; at a terminal, after the yes, the person types the Local
>   Folders path exactly as shown (as `decrypt` does, `cmd/mailarchive/decrypt.go:58-86`). The
>   alternative (refuse such a destination) is a one-line change: return W-LEAVES from the plan
>   instead of setting ask-twice.
> - **Removal does not erase.** The not-encrypted line adds: "If you stop the restore or it fails,
>   MailArchive removes what it wrote, but that cannot undo a copy already uploaded by a sync
>   service, kept in a backup or snapshot, or left on the disk." The Stopped words change to match
>   (§7.5).
> - **Counts only** for an encrypted archive: the progress line is "Restoring: 1,200 of 4,310
>   messages" with no folder name, and Activity lines likewise (`encryption_copy.go:170-180`). The
>   result's damaged-record list gives counts, not names, and **Save this record** says beside its
>   button that the record names the restored folders (it is saved only where the person chooses).
> - **The lease, not `ExtractSealed`.** The encrypted path reads each original through the read
>   lease taken at start, not through `ExtractSealed`, which stops when the archive is locked
>   (`extract.go:418-419`). So Q6's recommended answer is what is built: a Lock after the start
>   does not stop the restore, and the Lock reply says so as the copy's does (`copyGoesOnText`,
>   `encryption_copy.go:190`): "Locked. The restore into Thunderbird already started goes on until
>   it finishes — Shut down stops it, and nothing is added to Thunderbird." The alternative (Lock
>   stops it) is a small change: the restore's progress loop checks the lock state between
>   messages and ends as Cancel does.
> - Q8's recommended answer (allowed while unlocked, with the tick) is what is built; refusing
>   encrypted archives instead is a one-line refusal in the plan.
>
> Folded 2026-11-06 [foldcheck N12] (folding agent's answer): `copyLeaves` is an unexported method of
> `CopyExportEnv` and carries the copy's own words ("the readable copy"). Slice R6 lifts its test into
> one exported function in `internal/app` that returns reason codes only (synced by a named service,
> possibly iCloud Desktop/Documents, removable or external drive, network share). The copy and the
> restore each turn those codes into their own words. The copy's existing rows stay green before and
> after the lift, as with the delete log's writer (§7.4 foldcheck note).

> Settled 2026-11-07 [R6-01] (the R6 adversarial pass; the lead's call): decrypted mail took Local
> Folders' inherited permissions (a folder on a second Windows drive lets every signed-in account
> read; a macOS "everyone may read" inheritable entry applies whatever the mode says), and the plan
> said only "anyone who can open this Thunderbird profile". Now every folder and file a restore makes
> is made readable by this account alone as it is made (`FS.Private`): 0600/0700 with no access-list
> entry on macOS (`fchmod_extended` with the ACL removed, as `chmod -N`) and Linux; on Windows the
> recovery-key file's explicit, protected owner-only list (`winsec.RestrictToOwner`; a folder's
> inherited by what is made in it) — each read back (macOS `fgetattrlist`; Windows the recovery-key
> file's own check, `winsec.CheckSecretFile`). For an encrypted archive the plan says so ("readable
> only by your account on this computer"). When it cannot be done — the plan predicts it from the
> drive's file system: FAT32, exFAT, or a network share whose server decides (the copy export's
> list) — the plan says plainly who else can read it ("other accounts on this computer, and on any
> computer the drive is connected to") and that is a reason to ask twice (W-READERS, the same
> second confirmation as O2's; W-NOTAGAIN names it); a restore so confirmed passes an owner-only
> failure over. One the plan expected to hold but that fails while writing stops with the one
> cleanup, in words (`wNotPrivateHead`), nothing added. A plain archive's restore is made owner-only
> too where it can be; a failure is passed over and nothing more is asked. Tests:
> `tbengine/ma1128_r601_r612_test.go` (MA-1128): an inherited "others may read" entry on Local
> Folders (macOS `chmod +a`, Windows `icacls … (OI)(CI)R`), every made entry read back with the
> OS's own tool; the FAT seam; the owner-only step failing.
>
> Settled 2026-11-07 [R6-12] (the R6 adversarial pass): "counts only" now reaches the plan's and
> the refusal's words too: W-FAT32 says how many folders are 4 GB or more, and W-LONGPATH how many
> are nested too deep, never their names, for an encrypted archive (they go to a terminal's log and
> to `-record-out`). A plain archive's keep naming them. Test: `tbengine/ma1128_r601_r612_test.go`.

### 6.3 Size and free space

Needed space = the originals' sizes (recorded fixity size, else the file size; sealed size
for an encrypted archive, which slightly overstates) + 120 bytes per message for framing.
Refused when free space on the Local Folders volume (`util.FreeBytes`) is less than needed
× 1.05 + 256 MiB, room left for Thunderbird's own summaries and for the system (W-SPACE).
These margins are the lead's choice.

> Folded 2026-11-06 [RT-22], lead's call 2026-11-06 (margin includes Thunderbird's indexes): the
> margin is raised to **needed × 1.30 + 512 MiB**. The extra 25 % stands for what Thunderbird adds
> on the same drive after the restore — a summary (`.msf`) per folder and its search index of every
> restored message — and is a stated choice, not a measurement: lab row MA-1093 records how much the
> fixture's profile grew once Thunderbird had indexed it, and the number is revisited from that. The
> plan says: "Thunderbird will use more space as it indexes the restored mail; about X is kept free
> for that." The post-write check (§5.4 note) needs no extra space.

### 6.4 The plan the person sees

- Destination: the profile's name, the full Local Folders path, and the new folder's name.
- What goes back: messages and folders, and the size.
- What does not, by reason (§6.1), each with its remedy.
- Free space.
- Thunderbird: "Thunderbird is closed." or the refusal.
- Warnings, only when they apply:
  - W-RECAPTURE (§7.2) when this archive backs up this profile;
  - W-OLDTB (§4.1);
  - the not-encrypted line and tick (§6.2);
  - W-CLOUD: "This Thunderbird profile is in a folder SERVICE keeps in sync, so the
    restored mail will be uploaded there too." (`util.UnderCloudSync`, `paths.go:186-225`);
  - Gmail's All Mail (§6.5);
  - "N of these messages were deleted from the mailbox after they were backed up; they are
    restored too." (records the go-back timeline marks not present,
    `internal/state/manifest.go:150-165`). Owner question Q3.

The command line prints the same plan with `-plan` and stops.

> Folded 2026-11-06 [RT-9, RT-10, RT-8, RT-11, RT-22, O1, Q3]:
> - **The deleted-after-backup count** [RT-9] uses the workspace's own rule, a record counts as gone
>   when `!(rec.Present || rec.LastSeen.IsZero())` (`copyexport_archive.go:857`), not "not present".
>   `Present` is kept only for mailbox-wide archives (Microsoft 365), so for a Thunderbird or local
>   archive `LastSeen` is zero and nothing is counted. The line is shown only when the archive keeps
>   that timeline; for any other archive the plan says nothing about it (rather than a false "0").
>   Q3's recommended answer (restore such mail too, and say how many) is what is built; leaving it
>   out instead is a filter on the same rule.
> - **Order of the plan** [RT-10]: when this archive backs up the chosen profile, W-RECAPTURE is the
>   plan's FIRST line, above the destination. With O1's recommended answer it reads: "This archive
>   backs up this Thunderbird profile. Its backups will leave the restored folder out, so the
>   restored mail is not copied into the archive again. Mail you later move into that folder in
>   Thunderbird is not backed up while it is there." With O1's alternative (warn only) it is the
>   design's original W-RECAPTURE words (§7.2).
> - **Warnings added**: W-STORE first when a Store Thunderbird is found [RT-11]; W-NETWORK (§3.2
>   note); the ask-twice reasons for an encrypted archive [RT-8]; the space line of §6.3's note
>   [RT-22]; W-WAIT, always (§5.2 note); and, when the archive holds Outlook, no-original or
>   damaged-later records, the §6.1 note's sentence.
> - W-CLOUD stays for an unencrypted archive (a warning only); for an encrypted one it is replaced by
>   the ask-twice reasons.
>
> Folded 2026-11-06 [foldcheck N8] (folding agent's answer): "the archive keeps that timeline" means
> at least one record in the archive's manifest has a non-zero `LastSeen`. When none has, the
> deleted-after-backup line is not shown at all. MA-1124 tests both.

### 6.5 Choosing folders, and Gmail's duplicates

- Whole archive (default), or chosen folders from the tree (§2, workspace's tree rules),
  each with its restorable count. Choosing a folder includes its subfolders. Message by
  message choice is not in version 1 (the Extraction workspace does that for its own
  destinations).
- Same message in two folders: an archive made from a local or Thunderbird source keeps one
  record per folder ("Same email filed in two folders … exported to both",
  `docs/scenario-catalog.md:388`, S4), so a Gmail message with two labels, synced by
  Thunderbird as two folders, is restored into both — as Thunderbird showed it. A Microsoft
  365 archive keeps one record per message, in its latest folder
  (`internal/state/manifest.go:21-34`), so it is restored once.
- Gmail's "All Mail" folder holds every message of the account's other folders again. When a
  folder named `All Mail` under `[Gmail]` (or `[Google Mail]`) is chosen with others, the plan
  says: "Gmail's All Mail folder holds another copy of every message in your other Gmail
  folders. Restoring it too puts those messages in Thunderbird twice. Untick it if you only
  want your folders." Owner question Q4.

> Folded 2026-11-06 [Q4, RT-7]: Q4's recommended answer (All Mail ticked like every other folder,
> with the note) is what is built; unticking it by default instead is one default in the tree's
> initial selection. **The restore's own caps** [RT-7]: at most **10,000 folders** and **300
> levels** in one restore (the tree's own numbers; the review left the number to the lead and the
> lead named none, so these are the folding agent's proposal, for the lead to confirm at
> re-verify), applied to what the restore will create, whatever the tree builder allows — because
> that builder skips its node and id caps for the current archive (`copyexport_archive.go:508-511`),
> and "Open an archive folder…" can make any folder current, including one someone else wrote. Over
> a cap: refused in the plan with W-TOOMANY (§7.5), nothing written; choosing fewer folders is the
> remedy. MA-1088 gains the 10,001-folder case.
>
> Folded 2026-11-06 [foldcheck L1] (lead's call): confirmed: at most **10,000 folders** and **300
> levels** in one restore. This answers the "for the lead to confirm at re-verify" sentence above.
> Over the level cap, W-TOOMANY's words name the depth instead of the count (§7.5 foldcheck note).

## 7. Locks, backups, Desktop flow, words

### 7.1 The archive's lock and scheduled backups

- The restore holds the archive's exclusive lock (`lockfile.AcquireAs`) for its whole run,
  as extract does (`extract.go:158-172`): what it reads cannot change under it.
- Started while a backup runs: refused (W-BUSY).
- A scheduled backup that fires during a restore is refused by the lock ("archive is in use
  by another mailarchive run", `lockfile.go:63-70`) and tries again at its next time. The
  plan says so: "A backup scheduled while this runs is skipped and runs at its next time."
- Desktop: one task at a time in the shared slot, refused while a delete runs
  (`maintenance.go:49-57`).

> Folded 2026-11-06 [RT-18]: for an UNencrypted archive the restore takes the archive lock with a
> verb of its own, `restore-thunderbird`, and `holderWords` (`internal/app/busy.go:72-91`) gains it:
> "a restore of this archive into Thunderbird". So a skipped scheduled backup's line names the
> restore rather than "another MailArchive task". (An encrypted archive's lock still names no verb,
> as extract's does.) A scheduled backup meeting the lock skips **quietly** for a holder under 24
> hours old (`ScheduledLockSkip`, `busy.go:54`): no failure, alert, streak or history entry. The
> plan's line matches that: "A backup scheduled while this runs is skipped without an error and
> runs at its next time."

> Settled 2026-11-07 [R6-13] (the R6 adversarial pass; the lead's call): the plan's busy check
> answered "not held" when the lock file could not be opened, and Start then failed with the raw
> "open lock …: permission denied". Now the plan looks at the lock as Start will take it:
> - On a medium that is genuinely read-only (the volume is mounted read-only: `statfs` MNT_RDONLY
>   on macOS, ST_RDONLY on Linux; Windows FILE_READ_ONLY_VOLUME) the restore is **allowed** without
>   the archive's lock — nothing can write the archive there, a backup included — and the plan
>   says so ("The archive is on a drive that cannot be written (read-only), so MailArchive reads it
>   without taking its lock…"). It is W-BUSY if another computer's backup holds the lock (read
>   through a read-only handle, as verify does, `lockfile.HeldReadOnly`), and W-CHANGED at Start
>   if the drive is no longer read-only.
> - Otherwise a lock this account cannot take (a read-only lock file, another account's archive, a
>   folder it cannot write) is refused at the plan, W-ARCHLOCK, in words that name the folder and
>   say to check it is the person's own and not read-only; Start's own failure, should it come, is
>   W-ARCHLOCK too, never the raw error.
> Test: `tbengine/ma1127_r605_r606_r613_test.go` (MA-1127; the read-only medium through its seam,
> since none can be made in a test).

### 7.2 When the archive backs up this same profile (W-RECAPTURE)

A backup from Thunderbird reads every account folder under `Mail/`, Local Folders included
(`internal/app/run.go:1876-1878`), and a local archive keeps one record per folder (S4). So
after a restore into the profile this archive backs up, its next backup copies every
restored message into the archive again, under "Restored — NAME". Nothing is lost, but the
archive grows and search shows each message twice. The plan says: "This archive backs up
this Thunderbird profile. Its next backup will copy the restored folder into the archive
again, so each restored message will then be in the archive twice. To avoid that, delete the
restored folder in Thunderbird once you have what you need, or restore into another
profile." Version 1 warns only; teaching backups to skip restored folders would change what a
backup captures. Owner question Q7.

> Folded 2026-11-06 [RT-5, RT-10, O1]. Q7 is replaced by owner question O1 (review §5.1).
>
> **Leftovers are never backed up** [RT-5], lead's call 2026-11-06 (not an owner ruling: it skips
> only MailArchive's own entries, not the person's mail). The review found the backup walk reads
> what Thunderbird ignores: a dot-named *file* is skipped (`isMailboxCandidate`,
> `source/source.go:149-155`), but the loop for a container with no folder file of its own
> (`source/localwalk.go:582-595`) walks any `NAME.sbd`, so a half-written `Restored — NAME.sbd`
> would be copied in, torn last message included. The fix, a folder-scan skip in slice R5:
> - The working files `.mailarchive-part-ID-N` are dot-named files, already skipped by
>   `isMailboxCandidate`; a test pins that (MA-1138).
> - Before the walk enters any `.sbd` container (`localWalk.sbd`, `localwalk.go:601-612`, and the
>   orphan loop that calls it), it looks for `.mailarchive-restore` directly inside (one bounded
>   `lstat` + read of at most 4 KiB through the walk's scope, a regular file only). A container
>   "carries the marker" when the marker parses and its ID is in the skip set. The walk then skips
>   that container and the parent file beside it, and the run log says "Left out N folders that
>   MailArchive is restoring into Thunderbird" (not a fault, no alert).
> - The skip set is built by the backup's caller (`RunHeadless`, and Desktop's own runs) from
>   `restores.log` and passed to the source as an option, so `internal/source` does not read
>   MailArchive's settings. If `restores.log` cannot be read, every container with a well-formed
>   marker and no parent file is skipped, and the run log says the log could not be read.
> - The backup does not clean up leftovers (that stays with the next restore attempt, §8 note):
>   cleaning would need Thunderbird's lock inside a backup run.
> - A backup of a *different* archive that reads the same profile during a restore is covered the
>   same way; the one instant it can meet is after step 2 of §5.4's note (the `.sbd` exists, empty)
>   and before step 3 (the marker): an empty container, which yields no mail.
>
> Folded 2026-11-06 [foldcheck M5, M3] (lead's calls):
> - The first bullet above (the working files `.mailarchive-part-ID-N`) is **superseded by
>   [foldcheck M5]**: there are no working files. What MA-1138 pins instead: the marker and the
>   made-list are dot-named files, so `isMailboxCandidate` never takes them for mailboxes, even in a
>   container that is walked.
> - The last bullet above ("the one instant it can meet") is **superseded by [foldcheck M3]**. It
>   was wrong: a backup of another archive builds its skip set before a restore starts, then can walk
>   into that restore's marked container, whose ID it does not know, and back it up. The fix: when
>   the walk meets a well-formed marker whose ID is not in the skip set, it **re-reads `restores.log`
>   once** for that marker before treating the folder as ordinary. The re-read always finds a real
>   restore, because a restore's started line is fsynced before its `Mkdir` (§5.4, step 1). To keep a
>   planted marker cheap, the re-read is skipped when the log's size and modification time are the
>   same as when the skip set was last built (one stat instead of a read). The caller passes the
>   skip set with a refresh function it supplies, so `internal/source` still does not read
>   MailArchive's settings. A re-read that fails is the unreadable-log case of the third bullet.
>   Only an ID still unknown after the re-read is walked as an ordinary folder. The empty-container
>   instant (after `Mkdir`, before the marker) remains, and still yields no mail. `restores.log` is
>   always the one in `job.Dir()` (§7.4 foldcheck note, M4). MA-1138 gains the case.
>
> **Finished restored folders** (owner question O1). Built as the recommended answer (b): the skip
> above covers a finished restore too — a marked container whose ID is in `restores.log`, parent
> file present — so the next backup does not copy the restored mail into the archive again, and the
> plan's first line says so (§6.4 note). Cost, said in that line: mail the person later moves into
> the restored folder is not backed up while it is there. If the owner chooses (a), warn only, the
> change is one condition: the skip applies only when the parent file is absent (unfinished), and
> W-RECAPTURE keeps the original words above, first in the plan. Whether Thunderbird carries the
> marker along when the person renames or moves the restored folder is lab row MA-1091's last
> step.
>
> Surfacing an unfinished restore without waiting for a new Check [RT-5 (2)]: Desktop's start and
> the headless `status` read `restores.log`; a started line with no ended line shows as "An earlier
> restore into Thunderbird did not finish (DATE). Thunderbird does not show it. Click Restore to
> Thunderbird and Check to clean it up." (W-UNFINISHED, §7.5). MA-1141.
>
> Folded 2026-11-06 [foldcheck M2] (lead's call): Check and `-plan` write nothing, so "Click Restore
> to Thunderbird and Check to clean it up" above is superseded by [foldcheck M2]: the cleanup happens
> at the next **Restore** (Start) into that profile, under its locks. The new words are in §7.5's
> foldcheck note. Desktop start and `status` show W-UNFINISHED only while the recorded container
> still exists and carries that restore's marker, and the restore had not committed (§8 foldcheck
> note, case 3; a read, nothing is written). So a person who deletes the leftover by hand is not
> told about it again, and a restore that committed but crashed before its "ended" line (which
> Thunderbird does show) is never called unfinished; the next Start just records its "ended".
>
> Folded 2026-11-06 [foldcheck2 P3, P2, the residual case] (lead's calls). Amends the notes above;
> where they disagree, this note wins.
> - **[P3] The skip's "finished" is Start's "committed".** In the O1 paragraph above, "a marked
>   container whose ID is in `restores.log`, parent file present" is superseded by [foldcheck2 P3].
>   A backup skips a marked container (marker well-formed, ID in the skip set) in exactly two cases:
>   1. no entry named `Restored — NAME` is beside it (an unfinished or abandoned leftover): the
>      container is skipped, as before; or
>   2. (O1's answer (b) only) the restore **committed**, by the same three-part rule Start uses (§8
>      foldcheck note, case 2, with foldcheck2's regular-file check): `Restored — NAME` beside it is
>      a regular file (`Lstat`), and the log has this ID's "checked" line and no "taken" line. Then the
>      container and that parent file are both skipped.
>   Otherwise (an entry of the parent name is there, but the restore did not commit: no "checked"
>   line, a "taken" line, or the entry is a folder or a link) the container and the entry beside it
>   are **an ordinary folder and are backed up**, the hidden marker and made-list inside it still
>   never read as mailboxes. That is the case where Thunderbird can show the tree and the person may
>   have moved mail into it (§8 foldcheck2 note, case 3a), so it must be backed up.
> - **How the skip set carries it.** The caller builds the skip set from `restores.log` as before,
>   and for each ID records one flag, "committed" = a "checked" line and no "taken" line. The refresh
>   function of the M3 note returns the same flag. The parent file's kind is read by the walk itself
>   (`Lstat` through its scope), never taken from the log. With an unreadable log, only containers
>   with no entry of the parent name are skipped (as the third bullet of the RT-5 note already says),
>   since "committed" cannot be told.
> - **[Residual case] its cost under O1 (b).** In the residual case (§8 foldcheck note: a crash
>   between a failed parent-file create and the fsynced "taken" line), the state looks committed, so
>   the parent file beside the restored tree is skipped too. Here that file is the other program's
>   folder, so its own mail is not backed up while the restored tree sits beside it. P3 does not
>   remove this cost; it is the price of the case, and it ends when the person deletes the restored
>   folder in Thunderbird.
> - **[P2] When W-UNFINISHED shows.** The previous note's "only while the recorded container still
>   exists and carries that restore's marker, and the restore had not committed" is superseded by
>   [foldcheck2 P2]. Desktop start, `status` and the plan show W-UNFINISHED for a restore only when
>   all of these hold: its started line has **no closing line** ("ended", "cleaned" or "abandoned");
>   its recorded Local Folders is this profile's and reachable (§8 foldcheck2 note, step 0); the
>   recorded container exists and carries its marker; and **no entry named `Restored — NAME`** is
>   beside it. So W-UNFINISHED's "Thunderbird does not show it" is true whenever it is shown (lab A3
>   on Windows; macOS and Linux lab row MA-1091), and a restore that Start already settled as
>   "abandoned" is never shown as unfinished again.
> - **An abandoned leftover that is still there** (folding agent's answer, per the check's P2): a
>   restore whose closing line is "abandoned", whose container still exists with its marker, and
>   with no entry of the parent name beside it (W-LEFT, made-list unreadable, or W-COMMITERR) is
>   shown at Desktop start and in `status` with that row's words (§7.5 foldcheck2 note), never with
>   a promise to remove it. One with an entry of the parent name beside it is shown nothing: it is
>   an ordinary folder of the person's now, said once in the result that abandoned it (W-VISIBLE or
>   W-TAKEN). All of this is a read; nothing is written. MA-1141.

> Folded 2026-11-07 [foldcheck3 S7, S10] (lead's calls, owed to slice R5; built there; the words
> are the builder's, under the owner-delegated rule):
> - **[S7] The display rule, made whole.** W-COMMITERR at Desktop start uses §7.2's "no **entry** of
>   the parent name", like every other row (not "no file"). An abandoned leftover is shown, while its
>   container is there with its marker and no entry of its parent name beside it, in the words of why
>   it was left: the made-list's words when the list cannot be read; W-COMMITERR's (Desktop form:
>   "An earlier restore into Thunderbird (DATE) wrote and checked every message, but could not make
>   sure the file that makes the folder show in Thunderbird was saved, so it removed nothing.
>   Thunderbird does not show it now. Close Thunderbird, delete the folder 'Restored — NAME.sbd' in
>   PATH, and restore again.") when its abandon was a commit error; the taken name's (§8's
>   foldcheck3 S5 note) when the log has a "taken" line; W-LEFT's sense when something not
>   MailArchive's is in it ("… left the folder 'Restored — NAME.sbd' in PATH: it holds something
>   MailArchive did not make, so MailArchive removed nothing there. Thunderbird does not show that
>   folder. Close Thunderbird and delete it yourself …"); and — S7's case, a 3a-abandoned leftover
>   whose parent entry has since gone — "… left the folder 'Restored — NAME.sbd' in PATH whole,
>   because a folder of its name that MailArchive did not make was in Local Folders. That folder has
>   gone, so Thunderbird does not show the restored one now. Close Thunderbird and delete
>   'Restored — NAME.sbd' yourself when you no longer need it." The reason is read from the disk and
>   the log at display time, so no reason code is added to the log. Uninstall's plan names every one
>   of these and also, S7's third bullet, each open restore whose Local Folders cannot be checked now
>   (not reachable, or no longer its profile's), by its recorded path: "An earlier restore into
>   Thunderbird (DATE) may not have finished. It wrote into PATH, which MailArchive cannot check now. If
>   the folder 'Restored — NAME.sbd' is there, close Thunderbird and delete it yourself." — skipped
>   when that path is reachable and the container is gone. Each uninstall line adds "Nothing will
>   clean it up after MailArchive is removed."
> - **[S10] The function it reuses.** "Reachable and still this profile's Local Folders" re-reads the
>   recorded profile's settings with §3.2's own lookup: `tbrestore.LocalFoldersOf(profileDir)`, which
>   is `ReadPrefs` then `FindLocalFolders` — the same code the plan uses. The recorded Local Folders
>   counts as this profile's only when that lookup gives the same path and the same identity, and as
>   reachable only when the recorded path's identity is the recorded one.

> Settled 2026-11-07 [ADV-06, ADV-07] (the R3–R5 adversarial pass; ADV-07 is the lead's call of
> 2026-11-07). These amend the foldcheck2 P3 note's two cases above; where they disagree, this note
> wins.
> - **[ADV-06] The skip is tied to where the restore wrote.** "ID in the skip set" now also means: the
>   folder the walk met the container in is, by identity, the Local Folders the restore's started line
>   records (its recorded path is still that folder — `DirIdentity` equal to the recorded identity —
>   and the walk's folder is the same folder, `os.SameFile`), and the marker's profile key is the
>   started line's, as before. Anything else — a committed restore's folder copied with its marker into
>   another profile, a copy on a USB disk the person backs up, the mail later filed into either, a
>   copied leftover — is an ordinary folder and is backed up. The §13 R5 builder's note "a marker
>   copied into another profile does nothing" was not what the code did; it is now. A Local Folders on
>   an external drive that comes back with a new device number is then backed up in full, the safe
>   direction. Built: `SkipSet.Lookup(id, profileKey, holder)`, `source.RestoreSkip`.
> - **[ADV-07] "Abandoned" leftovers are backed up like ordinary folders** (lead's call 2026-11-07:
>   never skip a folder that may hold the person's mail). Case 1 above ("no entry named
>   `Restored — NAME` beside it: an unfinished or abandoned leftover") is narrowed: the container is
>   skipped only when its restore is still **open** (no closing line: being written now, or cut off by a
>   crash, so Thunderbird has never shown it) or **committed**. A restore closed "abandoned" — the
>   cleanup kept it whole because something not MailArchive's is in it (W-LEFT: typically Thunderbird's
>   `.msf` beside mail the person moved in while Thunderbird showed it), its made-list could not be
>   read, its commit could not be confirmed (W-COMMITERR), or a taken name — is never skipped, with or
>   without its parent name. "Committed" itself now also excludes a restore closed "abandoned" (so an
>   abandoned W-COMMITERR's parent file is backed up too). **The accepted cost:** an abandoned leftover
>   of a failed restore may hold a cut-off last message, which is copied into the archive once. The S5
>   words' promise ("Until then MailArchive backs them up like your other mail") now holds whether or
>   not the parent name stays. With an unreadable log, the RT-5 fallback is unchanged (containers with
>   no entry of the parent name are skipped, open or abandoned alike, since the log cannot tell them
>   apart); every restore is refused meanwhile (W-LOGBAD), and its words say backups then copy those
>   folders once the file is moved away.

> Settled 2026-11-07 [R6-02, R6-03, R6-04] (the R6 adversarial pass; R6-02, R6-03 and R6-04 are the
> lead's calls). These amend the notes above.
> - **[R6-02]** Whether "this archive backs up this profile" is now read from the archive's own
>   record and its installed schedule as well as Desktop's job files, and the plan says when it
>   cannot tell (§3.1 step 4's R6-02 note). W-RECAPTURE's words are unchanged.
> - **[R6-03] A restore still running is never "did not finish".** `Leftovers` named every restore
>   with no closing line W-UNFINISHED, so while a terminal restore was writing, Desktop and `status`
>   told the person to delete the very folder being written, and uninstall's plan said nothing
>   would clean it up — and uninstall went ahead, removing restores.log under it. Now a restore with
>   no closing line whose profile's restore lock is held is named "running now": "A restore into
>   Thunderbird is running now (it began DATE): it is writing the new folder 'TOP' into PATH. Keep
>   Thunderbird closed until it finishes; Thunderbird shows the folder once it is done." — the same
>   words in Desktop, `status` and uninstall's plan, never the delete advice. Uninstall refuses while
>   any restore lock is held (`uninstall.RestoreRunningError`; a dry run still gives its plan with
>   "Uninstall will refuse until the restore into Thunderbird that is running now finishes…"), and
>   asks again right before the settings files would go, so it never removes restores.log mid-run.
>   Tests: `tbengine/ma1122_r603_r610_r611_r616_test.go`, `uninstall/ma1141_r603_restore_running_test.go`
>   (MA-1141).
> - **[R6-04] The skip holds for a backup that began before the restore finished.** A backup loads
>   its skip set once; the M3 refresh re-read the log only for an ID it had never seen, so a restore
>   it saw open stayed "open, not committed" for the rest of that backup, and once the restore
>   committed (its parent file beside its folder) neither rule applied and the restored mail was
>   copied into that other archive. `Lookup` now reads the log again, when it has changed, for a
>   known restore that is open or not committed too, as for an unknown one (one stat; a committed
>   restore cannot go back). Test: `source/ma1138_r604_staleskip_test.go` (MA-1138).

### 7.3 The Desktop flow

Where: a "Restore to Thunderbird…" button on the current archive's card (beside
Maintenance), disabled with a reason when the archive is locked (encrypted) or has nothing
restorable (W-PST / W-NORAW shown beside it). Name chosen so it is not confused with the
encrypted archive's "restore the key file" (`/api/encryption/restore`,
`internal/desktop/dashboard.go:391`).

Steps, each brought into view with `showStep` (`dashboard.go:2362-2380`), the keyboard on
its first control, its status line saying what is needed:
1. **Profile** (§3.1): skipped when there is one.
2. **What**: "The whole archive" (selected) or "Choose folders" (the tree, with counts).
3. **Check** → the plan (§6.4), in a panel on the card. Refusals are said there, in place.
4. **Restore** (and the not-encrypted tick for an encrypted archive). The panel then follows
   the task: "Restoring: 1,200 of 4,310 messages (Inbox)", every 3 seconds at most
   (`encryption_copy.go:166-168`), with **Cancel** (`/api/job/cancel`,
   `dashboard.go:402`).
5. **Result**, in the panel, with **Save this record** (§7.4).

Requests, each through `d.api` (`workspace.go:104-138`), so POST only, loopback Host and
Origin, CSRF token, body cap, recover; plus the session cookie (`localweb/session.go:168-169`):
- `POST /api/restore-tb/profiles` → the list (names, folders, default). Read-only.
- `POST /api/restore-tb/folders {profile}` → the tree with restorable counts. Read-only.
- `POST /api/restore-tb/plan {profile, folders[], whole}` → the plan, kept server-side for ten
  minutes under a one-use token of a new kind (`pendingItem.restorePlan`, `isRestorePlan`;
  `pending.go:26-55`, `:132-187`). Folder ids are checked against the server's tree, never
  used to build a path (`copyexport_archive.go:72`). Read-only.
- `POST /api/restore-tb/start {token, notEncryptedOK}` → takes the token, re-checks (§5.3),
  starts the task through `runJob` (`maintenance.go:24-84`). Progress and the result come
  through `/api/activity` (`capture.go:379-404`).
A switch of the current archive, a Lock (before start), and Shut down discard the plan, as for
the copy (`pending.go:15-21`).

> Folded 2026-11-06 [RT-13, RT-8, RT-15, lab finding 3]:
> - **Its own key** [RT-13]: the restore plan is kept under a key of its own (as delete's is,
>   `pending.go:39-42`), not the archive's key, because `pendingSecrets.put` discards every item
>   with the same key (`pending.go:144-150`) and would cancel a pending unencrypted-copy plan or key
>   change. The kind is added to `ofCurrentArchive` (`pending.go:68`), so a switch of the current
>   archive still discards it. (The discard paths are `pending.go:63-70`, `:209-227`.)
> - **Start's body** becomes `{token, notEncryptedOK, confirmAgain}`; the server refuses an
>   encrypted plan without `notEncryptedOK`, and a plan whose ask-twice list is not empty without
>   `confirmAgain` (§6.2 note) [RT-8]. The page draws the second question only when the plan says
>   ask-twice, as the copy's page does.
> - **Profiles** carry the list id (§3.1 step 6 note) [RT-15]; `/folders` and `/plan` take
>   `{listID, position}`.
> - **Step 4's progress line** is counts only for an encrypted archive (§6.2 note); for an
>   unencrypted one it may name the folder as written. Under the progress line, for the whole run,
>   W-WAIT (§5.2 note, lab finding 3).
>
> Folded 2026-11-06 [foldcheck RT-13] (lead's call): the first bullet above is **superseded by
> [foldcheck RT-13]** where it adds the kind to `ofCurrentArchive`. That is a trap: `discardExcept`
> ends every `ofCurrentArchive` item whose key is not the current archive's (`pending.go:215-219`),
> and it runs after **every** settings save (`dashboard.go:686-687`), so a plan under a key of its own
> would be dropped at every save. `pending.go:63-67` says exactly this about delete plans. The
> restore plan copies the delete plan's pattern exactly:
> - **Its own key and kind.** A key of its own, `restorePlanKey(profileKey)`, made as
>   `deletePlanKey(row)` is (`internal/desktop/archivedelete.go:73-75`) and never an archive's key;
>   its own field on `pendingItem`; its own kind `isRestorePlan` beside `isDeletePlan`
>   (`pending.go:55`). It is **not** added to `ofCurrentArchive` (`pending.go:68-70`).
> - **How it survives a settings save.** A save that leaves the current archive where it was keeps
>   it: `discardExcept` passes it by (`dashboard.go:687`, since it is not `ofCurrentArchive`), and the
>   save ends restore plans only at a switch, in the same `if` as delete plans:
>   `if !samePath(wasCurrent, d.cfg.effectiveOut()) { discardWhere(isDeletePlan); discardWhere(isRestorePlan) }`
>   (`dashboard.go:690-692`). `afterSwitch` ends it beside delete plans too (`dashboard.go:710`).
>   Shut down ends it (`close`, `pending.go:222-227`), and so do its ten minutes (`put`'s timer,
>   `pending.go:151`).
> - **A Lock no longer discards it.** A Lock calls `discardFor` with the archive's own key
>   (`dashboard.go:300`, `pending.go:211-213`), which a plan under its own key does not match, as for
>   a delete plan. So "a Lock (before start) … discard[s] the plan" above is replaced: after a Lock
>   the plan stays, and Start refuses it, because an encrypted archive's restore needs the read lease
>   (§6.2), which a locked archive cannot give. The words are W-LOCKED (§7.5 foldcheck note); nothing
>   is written.
> - Because the key is per profile, a second plan for the same profile replaces the first (`put`,
>   `pending.go:144-150`), and a pending copy plan or key change is never touched.
> - MA-1130 gains: a restore plan survives a settings save that keeps the location; a switch, by a
>   save and by `afterSwitch`, ends it; a Lock leaves it and Start then refuses with W-LOCKED; a
>   pending copy plan survives a restore plan.

### 7.4 The restore record

- `restores.log` in MailArchive's settings folder (beside `deletions.log`,
  `internal/desktop/archivedelete.go:267`), one JSON line when a restore starts (id, date,
  archive folder, profile, Local Folders path, staging names, planned counts) and one when it
  ends (result, counts, the final folder name). It is how the next attempt finds an unfinished
  one (§8). For an encrypted archive it holds no folder names (§6.2).
- **Save this record** saves the plan and result as a text file where the person chooses, as
  Delete's record does (`archivedelete.go:63`).
- The command line prints the record and takes `-record-out FILE`.

> Folded 2026-11-06 [RT-16, RT-4]:
> - **The writer** is the delete log's append discipline, reused, not re-written
>   (`internal/archdelete/dellog.go:180-219`): a torn last line (a power cut mid-append) is kept
>   apart and does not make the log unreadable, a size cap (`errLogFull`), fsync after every
>   append, 0o600. Only a log that cannot be parsed apart from a torn tail refuses (W-LOGBAD); a full
>   log refuses with W-LOGFULL. Both words in §7.5, each with a remedy.
> - **Lines** per restore, all keyed by the restore ID: "started" (before the first create), "name"
>   (only if the first name was taken at `Mkdir`), "checked" (after the post-write check), "ended"
>   (result, counts, final name); and "abandoned" or "cleaned" written by a later attempt (§8 note).
> - The command line and Desktop use the same `restores.log` (one settings folder), so either finds
>   the other's unfinished restore.
> - Save this record says beside its button that the record names the restored folders (§6.2 note).
>
> Folded 2026-11-06 [foldcheck M4] (lead's call): **where `restores.log` and the restore lock live.**
> Always in MailArchive's default settings folder, `job.Dir()` (`internal/job/job.go:61-67`), whatever
> `-config` says, and never beside a `-config` file. Why: a Desktop started with `-config` keeps its
> `deletions.log` beside that config file (`deletionsLogPath`,
> `internal/desktop/archivedelete.go:267-293`), but a scheduled job "does not take" `-config`
> (`cmd/mailarchive/main.go:1296`). A restore logged beside a `-config` file would therefore be
> invisible to scheduled backups (no skip) and to their pre-sync (no restore lock). So:
> - The restore's two paths are never derived from `ConfigPath`. One function, `tbrestore.LogPath()`
>   (`job.Dir()` joined with `restores.log`), gives the log, and the restore lock sits beside it
>   (§5.2 note, item 3). A Desktop started with `-config` finds them through that function, exactly
>   as the command line does. Scheduled backups (`RunHeadless`), Desktop's own runs, presync,
>   `status` and `restore-thunderbird` all call it. `restore-thunderbird` takes no `-config`.
> - So the earlier note's "one settings folder" now holds for every caller.
> - Uninstall: `restores.log` and the `restore-*.lock` files are added to uninstall's settings files
>   (`fixedConfigNames`, `internal/uninstall/config.go:29-38`), so uninstalling MailArchive removes
>   them from `job.Dir()`. A Desktop started with `-config` plans its uninstall in its own folder, so
>   it does not remove them. (For the lead to confirm [superseded by [foldcheck2 N-e]: now the
>   lead's call; which leftovers, §7.4 foldcheck2 note], the folding agent's proposal: when
>   `restores.log` holds an unfinished restore, uninstall's plan names that leftover folder, since
>   nothing will clean it up afterwards.)
> - MA-1140 gains: a restore from a Desktop started with `-config` is seen by a scheduled backup's
>   skip and by presync's `detectRunning`.
>
> Folded 2026-11-06 [foldcheck RT-16 minor] (folding agent's answer): "reused, not re-written" above
> means lifted. The delete log's `appendLog` is unexported and typed to delete lines
> (`internal/archdelete/dellog.go:151`), so slice R4 moves its append discipline into one shared
> helper that both logs call, with delete's rows green before and after.
>
> Folded 2026-11-06 [foldcheck M2] (lead's call): **lines.** "checked" stays; a later attempt uses it
> only to tell a committed restore from an unfinished one, never to finish one (§8 foldcheck note,
> case 2). A **"taken"** line is added, appended and fsynced before a W-TAKEN cleanup starts (§5.4
> foldcheck sequence, step 9). A later attempt appends "abandoned", "cleaned" or "ended" **only at
> Start**, never at Check or `-plan` (§8 foldcheck note).
>
> Folded 2026-11-06 [foldcheck2 P1, P2, N-b, N-e] (lead's calls):
> - **Closing lines.** A restore's record is closed by exactly one of "ended", "cleaned" or
>   "abandoned"; a started line without one is open. The run itself writes "ended" when it finishes,
>   or when its own cleanup (Cancel, Shut down, a failure, W-CHECKFAIL) removed everything, with the
>   result. It writes **"abandoned"** instead when its cleanup removed nothing (W-TAKEN, W-LEFT, the
>   made-list unreadable, W-COMMITERR; §8 foldcheck2 note), and writes nothing when a removal was
>   blocked, so the next Start tries again. A later attempt still writes its lines only at Start.
>   So the M2 note's "A later attempt appends … only at Start" stands; the run's own "abandoned" is
>   new.
> - **[N-b] Uninstall's rule for the lock files.** `fixedConfigNames` holds exact names
>   (`internal/uninstall/config.go:27-38`), so the M4 note's "the `restore-*.lock` files are added to
>   … `fixedConfigNames`" is superseded by [foldcheck2 N-b] for the lock files. `restores.log` goes
>   into `fixedConfigNames` (an exact name, as `deletions.log` is, `:35-37`). The lock files get a
>   pattern case in `deletableConfigName` (`:114-130`), beside the `.jobfail.log` and `.alert-streak`
>   cases: a name that is `restore-`, then exactly 16 lowercase hex characters, then `.lock`. Its
>   constants come from `tbrestore` (the lock-name function of §5.2 note, item 3), and a test keeps
>   the two in step by behaviour, as MA-965 does for `deletions.log` and MA-910 for `.sync-alert`
>   (MA-1140).
> - **[N-e] Confirmed.** "(For the lead to confirm, the folding agent's proposal: …)" in the M4 note
>   above is superseded by [foldcheck2 N-e]: uninstall's plan naming leftovers is the lead's call.
> - **[P2] Which leftovers uninstall names.** The same rule as Desktop start (§7.2 foldcheck2 note):
>   every restore W-UNFINISHED would show, and every abandoned leftover still there with no entry of
>   the parent name beside it. For each, the plan names the folder `Restored — NAME.sbd` and its
>   path, and says that nothing will clean it up after MailArchive is removed. (The M4 note's
>   "when `restores.log` holds an unfinished restore" is narrowed to this rule.)

> Settled 2026-11-07 [ADV-04] (the R3–R5 adversarial pass): "a torn last line … is kept apart" covered
> only a line cut short. A power cut can also leave the file longer than what reached the disk, its
> tail zero bytes (NTFS and XFS can; APFS not checked). Such a line made every later read W-LOGBAD for
> good, refusing every restore. Now `Log.Read` reads a line holding a zero byte as what precedes its
> trailing zeros (and spaces): nothing, a cut-off object — both torn, passed over wherever they sit —
> or a whole line whose line break alone was lost, which is read. A zero byte with anything after it
> on its line is not such a tail and stays W-LOGBAD. The deletion log's reader (`archdelete.readLog`)
> passes over every line it cannot parse, so it was never affected; the made-list's reader passes over
> its torn last line, and nothing appends to a made-list after a crash (decision 11). Built:
> `rlog.go`; MA-1142's ADV-04 test.

### 7.5 Words (the owner-delegated rule: say what happened first, then the remedy)

| Id | Words |
|---|---|
| W-RUNNING | "Thunderbird is open, so nothing was restored. Close Thunderbird (on Windows: File → Exit; on a Mac: Thunderbird → Quit Thunderbird), then click Check again." |
| W-NOPROF | "No Thunderbird profile was found on this computer, so there is nowhere to restore to. Open Thunderbird once to make one, close it, then click Restore to Thunderbird again." |
| W-NOLOCAL | "This Thunderbird profile has no Local Folders, so nothing was restored. Open Thunderbird once with this profile and close it, then try again." |
| W-MAILDIR | "This Thunderbird profile keeps each message in its own file (the 'maildir' setting). MailArchive can restore only into the usual one-file-per-folder kind, so nothing was written. Choose another profile." |
| W-PST | "This archive was made from Outlook data files, which keep no original messages, so none of it can go back into Thunderbird." |
| W-NORAW | "This archive was made without keeping the original messages, so none of it can go back into Thunderbird. Turn on keeping originals in Settings so that future backups can be restored." |
| W-SPACE | "There is not enough free space on DRIVE for this restore: it needs about X and DRIVE has Y free. Free some space, or choose fewer folders, then click Check again." |
| W-BUSY | "A backup of this archive is running, so the restore did not start. Wait until it finishes (its row in Your archives says when), then click Restore again." |
| W-NAMES | "Local Folders already holds 'Restored — NAME' and 98 more like it, so nothing was restored. Delete or rename the restored folders you no longer need in Thunderbird, then try again." |
| W-OLDTB | "This profile was last opened by Thunderbird VERSION. That version shows a '>' at the start of any message line beginning with 'From '. Update Thunderbird first to see those lines exactly, or go ahead." |
| W-LONGPATH | "N folders are nested too deep for Windows to hold under this profile, so they are not restored: FOLDERS." |
| W-ELEVATED | "MailArchive is running as administrator, so the restore did not start: files it wrote could not be changed by Thunderbird afterwards. Start MailArchive normally and try again." (presync refuses an elevated start for the same reason, `resolve_windows.go:41-46`) |
| Done | "Restored N messages into Thunderbird as the folder 'Restored — NAME' under Local Folders. Open Thunderbird to see it; the first time, it may take a minute to show every message." plus the skipped counts and the notes of §4.6. |
| Stopped | "The restore was stopped, so nothing was added to Thunderbird. The part already written was removed." |

> Folded 2026-11-06 [RT-17, RT-8, RT-11, RT-12, RT-16, RT-20, RT-1, RT-5, RT-7, lab findings 1
> and 3]. Same rule: what happened first, then the remedy. Placeholders in capitals are filled by
> the server. Changed rows (the original rows above stay as reviewed):
> - **W-RUNNING**, add on Windows: "If you don't see Thunderbird, look for its icon by the clock;
>   someone else signed in to this computer may also have it open."
> - **W-NOLOCAL**, replaced (lab finding 1, lead's call): "This Thunderbird profile has no Local
>   Folders yet, so there is nowhere to restore to and nothing was written. In Thunderbird, MENU
>   PATH, then close Thunderbird and click Check again. MailArchive does not change Thunderbird's
>   settings itself." MENU PATH is filled from lab row MA-1133 before this ships.
> - **Stopped**, for every archive, adds: "That cannot undo a copy already uploaded by a sync
>   service, kept in a backup or snapshot, or left on the disk." [RT-8]
> - **Done** adds W-STORE's sentence when it applied, and "MailArchive checked every message it
>   wrote." [RT-11, RT-12]
>
> New rows:
> - **W-WAIT** (lead's words, owner-delegated 2026-11-02; shown for the whole run, Desktop and
>   terminal): "While this runs, leave Thunderbird closed. If you open it and it says Thunderbird is
>   already running, choose Cancel and wait for the restore to finish."
> - **W-RESTORING**: "Another restore into this Thunderbird profile is running (in MailArchive or at
>   a terminal), so this one did not start. Wait for it to finish, then click Check again."
> - **W-STORE** (warning): "If you use Thunderbird from the Microsoft Store, this restore may not
>   show in it: that version may keep its mail in another place. If you installed Thunderbird from
>   thunderbird.net, ignore this."
> - **W-NOIDENT**: "Windows could not identify the drive that holds Thunderbird's Local Folders
>   (PATH), so nothing was restored. This happens on some network drives. Choose a profile on this
>   computer's own drive."
> - **W-NETWORK** (warning): "Thunderbird's Local Folders (PATH) is on a network drive. The restore
>   writes there over the network, so it may be slow."
> - **W-OVERLAP**: "Thunderbird's Local Folders (PATH) overlaps WHAT (the archive folder, or the
>   folder of the mail account ACCOUNT), so nothing was restored. Choose another profile."
> - **W-PREFS**: "MailArchive could not read this profile's settings file (prefs.js: WHY), so nothing
>   was restored. Open Thunderbird once with this profile and close it, then try again; if it
>   repeats, choose another profile."
> - **W-PROFINI**: "Thunderbird's list of profiles (profiles.ini) is larger than MailArchive will
>   read, so nothing was restored. Remove profiles you no longer use in Thunderbird's Profile
>   Manager, then try again."
> - **W-PROFMODE** (macOS, Linux): "This profile's folder (PATH) can be changed by other accounts on
>   this computer, or belongs to another account, so MailArchive will not write into it. Make it
>   yours only (for example `chmod go-w PATH`), or choose another profile."
> - **W-LOGBAD**: "MailArchive's restore record (restores.log in FOLDER) can't be read, so it can't
>   tell whether an earlier restore left anything behind, and nothing was restored. Move that file
>   somewhere you keep records, then try again."
> - **W-LOGFULL**: "MailArchive's restore record (restores.log in FOLDER) is full, so nothing was
>   restored. Copy it somewhere you keep records, delete it, then try again."
> - **W-LEFT**: "MailArchive removed what it wrote, but left N entries it did not make in
>   'Restored — NAME.sbd' (FIRST NAMES). Thunderbird does not show that folder; delete it yourself
>   when you no longer need those files."
> - **W-TAKEN**: "Something made a folder named 'Restored — NAME' in Local Folders while the restore
>   ran, so MailArchive removed what it wrote and added nothing. Click Check again to restore under
>   a new name."
> - **W-CHECKFAIL**: "After writing, MailArchive checked every message, and FILE (the folder FOLDER)
>   was missing or changed — often antivirus moving a file it flagged. Nothing was added to
>   Thunderbird. Look in your antivirus's quarantine, or leave that folder out, then try again."
>   (For an encrypted archive, FOLDER is left out: "one of the folders".)
> - **W-TICK**: "The restored mail will not be encrypted, and the box saying you understand was not
>   ticked, so nothing was restored. Tick it and click Restore again."
> - **W-FAT32**: "FOLDER is SIZE, too big for the drive that holds Local Folders: that drive uses
>   the FAT32 format, which cannot hold a file of 4 GB or more. It is not restored. Choose fewer
>   folders, or move Local Folders to a drive formatted another way."
> - **W-TOOMANY**: "This restore would make N folders in Thunderbird, more than MailArchive makes in
>   one restore (10,000), so nothing was restored. Choose fewer folders."
> - **W-UNFINISHED** (Desktop start, `status`, and the next plan): "An earlier restore into
>   Thunderbird did not finish (DATE). Thunderbird does not show it. Click Restore to Thunderbird,
>   then Check, to clean it up."
> - **W-LEAVES** (only if the owner picks O2's alternative): "Thunderbird's Local Folders may leave
>   this computer (REASONS), and the restored mail would not be encrypted, so nothing was
>   restored. Choose a profile on this computer's own drive."
>
> Folded 2026-11-06 [foldcheck M2, N2, N7, N11, L1, RT-13]. Same rule. Changed rows (the rows above
> stay as written):
> - **W-UNFINISHED**, replaced [M2] (lead's call; Check writes nothing): "An earlier restore into
>   Thunderbird did not finish (DATE). Thunderbird does not show it. MailArchive removes it at the
>   start of your next restore into that Thunderbird profile. To remove it yourself instead, close
>   Thunderbird and delete the folder 'Restored — NAME.sbd' in PATH." Shown only while that folder
>   still carries the restore's marker and the restore had not committed (§7.2 foldcheck note). NAME is the archive folder's name,
>   which the started line keeps for an encrypted archive too (§5.4, step 1).
> - **W-CHECKFAIL** and **W-LEFT** [N7] (folding agent's answer): for an encrypted archive FILE is
>   left out, as FOLDER already is ("one of the restored files"), and W-LEFT's FIRST NAMES are left
>   out ("left N entries it did not make"). The antivirus words of §8's table also leave out FILE
>   for an encrypted archive.
> - **W-LOGBAD** and **W-LOGFULL** [N2] (for the lead to confirm [superseded by [foldcheck2 N-e]:
>   now the lead's call, words only], the folding agent's proposal; the
>   other choice, MailArchive trimming the log itself and keeping the IDs whose folders still exist,
>   is a slice R5 change): moving or deleting the log also forgets which folders MailArchive put into
>   Thunderbird. So each adds, after its remedy: "After that, MailArchive's backups no longer know
>   which folders it restored into Thunderbird, and back them up like your other mail, together with
>   any hidden folder an unfinished restore left. Before you move the file, delete in Thunderbird the
>   restored folders you no longer need."
> - **W-PREFS** [N11]: names `user.js` instead of `prefs.js` when that is the file it could not read.
> - **W-TOOMANY** [L1], over the level cap: "This restore would make folders N levels deep, more than
>   MailArchive makes in one restore (300), so nothing was restored. Leave out the most deeply nested
>   folders."
>
> New rows:
> - **W-LOCKED** [RT-13]: "This archive is locked, so the restore did not start. Unlock it, then
>   click Check again."
> - **Made-list unreadable** [M1] (§8 foldcheck note, rule 6): "MailArchive could not read its list of
>   what an earlier restore wrote, so it removed nothing. Thunderbird does not show that folder
>   ('Restored — NAME.sbd' in PATH); close Thunderbird and delete it yourself when you are ready."

> Folded 2026-11-06 [foldcheck2 P1, P2, N-c, N-f, N-g] (lead's calls; the exact wording of each row
> is the folding agent's, under the owner-delegated rule). Each row below is shown only in the state
> it names, so none says something false. "Thunderbird does not show it" is used only when no entry
> named `Restored — NAME` is beside the container (lab A3 on Windows; macOS and Linux lab row
> MA-1091). Changed rows (the rows above stay as written):
> - **W-UNFINISHED**, its trigger narrowed [P2]: shown only as §7.2's foldcheck2 note says (no
>   closing line, reachable, marker present, no entry of the parent name). Its sentence "MailArchive
>   removes it at the start of your next restore into that Thunderbird profile" is superseded by
>   [foldcheck2 P1], since the cleanup may now leave it: "At the start of your next restore into that
>   Thunderbird profile, MailArchive removes what it wrote there; if anything else has been put in
>   that folder, it leaves the whole folder and tells you." The rest of the replaced row stands.
> - **W-LEFT**, replaced [P1, N-a]: the cleanup now removes nothing when it finds an entry it did not
>   make, so "MailArchive removed what it wrote, but left N entries" is superseded by [foldcheck2
>   P1]. New words: "MailArchive found N entries it did not make in the folder 'Restored — NAME.sbd'
>   (FIRST NAMES), in PATH, so it removed nothing there. Thunderbird does not show that folder. Close
>   Thunderbird and delete it yourself when you no longer need those files." For an encrypted
>   archive FIRST NAMES are left out, as before. Shown in the result that abandoned it, and at
>   Desktop start and in `status` while the folder is still there (§7.2 foldcheck2 note).
> - **W-TAKEN**, replaced [P1]: the old words "MailArchive removed what it wrote and added nothing"
>   are superseded by [foldcheck2 P1]. New words: "Something made a folder named 'Restored — NAME'
>   in Local Folders while the restore ran. MailArchive had already written and checked every
>   message, so it removed nothing: Thunderbird will show the restored folders inside that
>   'Restored — NAME'. They are yours to keep, or to delete in Thunderbird. MailArchive backs them up
>   like your other mail. To restore under a new name as well, click Check again." When the entry of
>   that name is a folder or a link, not a file, "Thunderbird will show" becomes "Thunderbird may
>   show".
> - **Made-list unreadable**: its words stand; they are reached only after the cleanup has found no
>   entry of the parent name (§8 foldcheck2 note, step 2), so "Thunderbird does not show that folder"
>   is true. Also shown at Desktop start while the folder is there.
> - **Stopped** and **W-CHECKFAIL**: when the run's own cleanup removed nothing (W-LEFT, made-list
>   unreadable) or was blocked (the antivirus words), their sentences "The part already written was
>   removed." and "the tree removed" do not apply; the result gives that row's words in their place.
>
> New rows:
> - **W-VISIBLE** [P1], at Start, for an earlier restore settled by case 3a (an entry named
>   `Restored — NAME` beside its container, and it had not committed). Two forms:
>   - when it had a "checked" line (then either a "taken" line, or the entry is a folder or a link):
>     "An earlier restore into Thunderbird
>     (DATE) wrote and checked every message, but a folder named 'Restored — NAME' that MailArchive
>     did not make is now in Local Folders, so MailArchive removed nothing. Thunderbird shows the
>     restored folders inside 'Restored — NAME'. They are yours to keep, or to delete in
>     Thunderbird. MailArchive backs them up like your other mail."
>   - when it had no "checked" line: "An earlier restore into Thunderbird (DATE) stopped before it
>     finished writing, and a folder named 'Restored — NAME' that MailArchive did not make is now in
>     Local Folders, so MailArchive removed nothing. Thunderbird shows those folders inside
>     'Restored — NAME', and some messages in them may be missing or cut short. They are yours: if
>     you moved mail into them, move it out first, then delete them in Thunderbird and restore
>     again. Until then MailArchive backs them up like your other mail."
>   When the entry is a folder or a link, "Thunderbird shows" becomes "Thunderbird may show".
>   Both forms rest on P1's lab check (§12.3 foldcheck2 note); if Thunderbird is found not to show a
>   tree under a parent file another program made, "shows" becomes "may show" before slice R7.
> - **W-COMMITERR** [N-c]: "MailArchive wrote and checked every message, but could not make sure the
>   file that makes the folder show in Thunderbird was saved (WHY), so it stopped there and removed
>   nothing. The restored mail is complete, but Thunderbird may or may not show it as 'Restored —
>   NAME'. If it does not, close Thunderbird, delete the folder 'Restored — NAME.sbd' in PATH, and
>   restore again." Also shown at Desktop start while that folder is there with no file of the parent
>   name beside it.
> - **W-ELSEWHERE** [N-f], in the plan and in Start's result: "An earlier restore into this
>   Thunderbird profile did not finish (DATE). It wrote into PATH, which REASON, so MailArchive left
>   it alone." REASON is "is on a drive that isn't connected" (then: "Connect that drive, and
>   MailArchive deals with it at your next restore into this profile.") or "is no longer this
>   profile's Local Folders" (then: "If you no longer need it, delete the folder 'Restored —
>   NAME.sbd' there yourself."). In Start's result it adds "Your new restore went ahead." [N-g]; the
>   plan says "Your restore will go ahead." The RT-4 note's "Connect it and click Check again" words
>   for the drive-missing state are superseded by this row.
> - **[N-g] At Start, after the new restore's own result**, each earlier restore that Start could not
>   clean up is named once, in the row that applies (W-VISIBLE, W-LEFT, made-list unreadable,
>   W-ELSEWHERE, or the antivirus words when a removal was blocked, then "MailArchive tries again at
>   your next restore.").

## 8. Failures and crashes

The staging pair (§5.4) is the only thing written before the end. "Next attempt" means the
next Check or Restore for that profile, which reads `restores.log` first.

| Step | What can go wrong | Left on disk | Shown | Next attempt |
|---|---|---|---|---|
| Plan | Thunderbird running, no profile, no Local Folders, maildir, elevated, PST, no originals | nothing | the refusal (§7.5) | runs normally |
| Start | anything changed since the plan (§5.3), archive lock held, profile lock held | nothing | "This changed since you clicked Check …" / W-BUSY / W-RUNNING | runs normally |
| Create staging | permission denied on Local Folders | nothing | "Thunderbird's Local Folders (PATH) can't be written by this account (WHY). Check its permissions, then try again." | runs normally |
| Writing | disk full | staging, removed at once | W-SPACE, "nothing was added" | runs normally |
| Writing | a message unreadable or failing fixity | nothing extra | counted and named in the result | — |
| Writing | antivirus holding a new file (Windows sharing violation) | staging | retried for about 5 seconds per file; then "Windows would not let MailArchive write FILE (it was in use — often by antivirus scanning). Nothing was added to Thunderbird. Try again; if it repeats, exclude this Thunderbird profile from real-time scanning." (the hint backups already give, `run.go:1430`) | runs normally (staging removed) |
| Writing | Cancel or Shut down | staging, removed | Stopped | runs normally |
| Writing | crash or power cut | staging pair (hidden names) | — | the plan says "An earlier restore into this profile did not finish (DATE). Its partial files will be removed first." and removes exactly the entries its `restores.log` line names, nothing else |
| Commit | rename 1 done, crash before rename 2 | `Restored — NAME.sbd` and the hidden parent file | — | finishes rename 2 (the started line names both); lab row MA-1091 checks what Thunderbird showed meanwhile |
| Commit | name taken at the instant of rename 2 | — | — | rename 1 undone, next name tried |
| After | Thunderbird opened later, builds summaries | Thunderbird's own `.msf` files | — | — |

A plain `restores.log` that cannot be read refuses the restore, because whether a partial
restore is on disk could not be told (Delete's rule, `archivedelete.go:659-680`).

> Folded 2026-11-06 [RT-1, RT-4, RT-5, RT-16, RT-17], lead's call 2026-11-06 (RT-1). The table
> above assumed the hidden staging pair and two renames; with §5.4's folded sequence there is no
> staging pair, so its "Create staging", "Commit" and "crash or power cut" rows are replaced by the
> states below. "Next attempt" is still the next Check or Restore for the profile, which reads
> `restores.log` first; every state below is also surfaced at Desktop start and in `status`
> (W-UNFINISHED, §7.2 note), and backups skip it meanwhile (§7.2 note). In every state Thunderbird
> shows nothing that is not finished, because the parent file is the last thing made.
> - **Crash after the started line, before `Mkdir`.** On disk: nothing. Next attempt: finds no
>   container of the recorded name (or one without this ID's marker), appends "abandoned", runs
>   normally.
> - **Crash while writing** (container and marker present, no parent file, no "checked" line). On
>   disk: a container Thunderbird does not show, finished mbox files under final names, perhaps one
>   working file. Next attempt: the plan's first line says "An earlier restore into this profile did
>   not finish (DATE). Its partial folder, which Thunderbird does not show, will be removed first.";
>   at Start (under all three locks) it removes it (cleanup rules below), appends "cleaned", then
>   restores.
> - **Crash after "checked", before the parent file.** On disk: a complete, fsynced tree, not shown.
>   Next attempt (lead's "reuses"): re-runs the full post-write check on that tree against the
>   archive (the fingerprints are recomputed from the archive's originals); if it passes, it creates
>   the parent file, appends "ended", and the plan says "An earlier restore (DATE) had written
>   everything; MailArchive finished it: 'Restored — NAME' is now in Thunderbird." If it fails, the
>   tree is removed as above. The new restore then goes on under its own name.
> - **Crash after the parent file, before "ended"** [RT-4]. On disk: the finished folder, its data
>   fsynced before the parent file was made. Next attempt: the final name present and its marker
>   matching → treated as finished: appends "ended", removes nothing.
> - **Crash during cleanup** (after Cancel, a failure, or W-TAKEN) [RT-4]. The marker is removed last,
>   just before the container itself, so a cleanup cut short leaves a container still carrying the
>   marker. Next attempt: finishes the cleanup.
> - **Thunderbird opened between a crash and the next attempt** [RT-4]. It ignores a container with no
>   parent file (lab A3: no `.msf` made, no cache entry), so normally nothing was added inside. If
>   the cleanup meets entries it did not make, it leaves them and says W-LEFT.
> - **The Local Folders drive missing at the next attempt** [RT-4]. The plan for that profile says
>   "An earlier restore into this profile did not finish (DATE); its folder is on DRIVE, which isn't
>   connected. Connect it and click Check again." The restore into that profile cannot run anyway
>   (its Local Folders is missing); other profiles are not affected.
> - **The person never tries again** [RT-4, RT-5]. The leftover is never shown by Thunderbird, never
>   backed up (§7.2 note), and is named at every Desktop start and in `status` until a Check cleans it
>   [superseded by [foldcheck2 N-d]: until a Start settles it, and while it shows as §7.2's
>   foldcheck2 note says].
> - **The parent file's name taken at the commit.** W-TAKEN: the tree removed, nothing added
>   (§5.4 note, step 8).
> - **Antivirus during placement or the commit** [RT-24]: a placement meeting a sharing violation is
>   retried for about 5 seconds; then the restore stops, removes what it wrote, and says the
>   antivirus words of the table above. A quarantine after placement is caught by the post-write
>   check (W-CHECKFAIL).
>
> **Cleanup rules** [RT-16]: it runs through the Local Folders root and follows no link; it removes
> only (a) working files named `.mailarchive-part-<this restore's ID>-N`, (b) the entries the plan
> recorded for this restore, each checked to be a regular file or directory, (c) the marker, last,
> and only when its ID is this restore's, then the container. It never removes a `Restored — ` name
> whose container does not carry this restore's marker, and never anything outside the container.
> Anything else found inside is left, and named (W-LEFT).
>
> `restores.log`: an unreadable log refuses (W-LOGBAD), a full one refuses (W-LOGFULL); a torn last
> line does neither (§7.4 note).

> Folded 2026-11-06 [foldcheck M1, M2, M5, N1] (lead's calls for M1, M2 and M5). This note amends the
> crash states and cleanup rules above; where they disagree, this note wins.
> - **Check (the plan) and `-plan` write nothing.** They read `restores.log` and the disk and report.
>   Every action of a "next attempt", the cleanup and every "abandoned", "cleaned" or "ended" line,
>   happens at **Start**, in this order: archive lock → MailArchive restore lock → the re-check
>   (§5.3 note) → Thunderbird's profile lock → B checked again → the cleanup of each unfinished
>   restore into this profile → the new restore. Cleanup only removes, so it cannot spoil the
>   re-check: the new name was chosen free of the leftover, and free space only grows.
> - **At Start, each started line for this profile with no "ended", "cleaned" or "abandoned" line
>   is settled by the first case that applies:**
>   1. The recorded container (named by the latest "name" line, else the started line) is absent, or
>      does not carry this ID's marker → append "abandoned"; remove nothing. [N1] (folding agent's
>      answer) A container with no marker (a crash between `Mkdir` and the marker) cannot be proven
>      MailArchive's, so it is left. It is empty, Thunderbird does not show it, a backup finds no
>      mail in it, and it keeps its name, so the next restore takes " (2)".
>   2. The container carries this ID's marker, the parent file `Restored — NAME` exists, the log has
>      this ID's "checked" line, and it has no "taken" line → the restore had committed (the parent
>      file is made only after "checked"): append "ended"; remove nothing. Requiring "checked" means
>      a parent file made by someone else after a crash mid-write (for example the person making a
>      folder of exactly that name in Thunderbird) never makes an unchecked tree count as committed.
>   3. Otherwise (no parent file, no "checked" line, or a "taken" line) → **clean up** by the rules
>      below, then append "cleaned" (or "abandoned" with W-LEFT, rule 4). A parent file that is
>      present is never touched (rule 7).
> - **Superseded by [foldcheck M2]:** the state "Crash after 'checked', before the parent file" above.
>   It is now case 3: cleaned up, never finished, and the person's new restore writes everything
>   again. Nothing re-checks the old tree against the archive, and the plan never says "MailArchive
>   finished it".
> - **Superseded by [foldcheck M5]:** "perhaps one working file" in "Crash while writing". There are
>   none. Instead there may be one mbox file under its final name whose last message is torn. The
>   made-list names it, so it is removed like the rest.
> - **A crash during a W-TAKEN cleanup** now finds the "taken" line, so it is case 3, not case 2: a
>   parent file someone else made is never taken as this restore's commit. One case is left: a crash
>   in the instant between a failed parent-file create and the fsynced "taken" line looks like case 2
>   and is recorded as ended. Nothing is removed (the safe direction), and the restored tree then
>   shows under the other folder of that name. It needs another program to make that exact name in
>   Local Folders while Thunderbird is closed and held out; it is in the R3–R5 adversarial pass's
>   scope.
> - **Cleanup rules, replaced** [M1] (rules (a) and (b) above are superseded by [foldcheck M1]). The
>   cleanup runs through the Local Folders root and never follows a link:
>   1. Read the made-list, bounded at 16 MiB. A torn last line is ignored, since its entry was never
>      created (the create comes after the fsync). A missing made-list (a crash between the marker
>      and the list) lists nothing. Each line's path must be relative, with no empty, `.` or `..`
>      part, or the list counts as unreadable (rule 6).
>   2. Remove the listed entries in reverse order, so children go before parents. Each is removed only
>      after an `Lstat` through the root shows the kind the list names: a regular file for "f", a
>      directory for "d", never a link. An entry that is absent is passed over. A directory is removed
>      only when it is empty.
>   3. Then remove the made-list, then the marker (only when its ID is this restore's), then the
>      container.
>   4. Anything not listed, a kind that does not match, or a directory still not empty after its
>      listed children are gone, is **left**. Then the made-list and the marker are kept too, so
>      backups keep leaving the folder out; the log gets "abandoned"; and the result says W-LEFT once.
>   5. A listed entry that cannot be removed (a Windows sharing violation, retried for about 5
>      seconds; a permission error) stops the cleanup with the antivirus words of the table above. No
>      line is appended, so the next Start tries again.
>   6. A made-list that cannot be parsed, apart from a torn last line: nothing is removed, the marker
>      and the list are kept, "abandoned" is appended, and the result gives the made-list-unreadable
>      words (§7.5 foldcheck note).
>   7. It never removes the parent file `Restored — NAME` (it is outside the container), and never
>      anything else outside the container.
> - **One cleanup.** Cancel, Shut down, a failure, W-CHECKFAIL and W-TAKEN use these same rules (§5.4
>   foldcheck note).
> - **Antivirus** (the last state above): "a placement meeting a sharing violation" is superseded by
>   [foldcheck M5]; nothing is placed. The 5-second retry is now on the post-write check's reopen
>   (§5.4 foldcheck sequence, step 7) and on cleanup removals (rule 5).

> Folded 2026-11-06 [foldcheck2 P1, N-a, N-c, N-d, N-f, N-g, the residual case] (lead's calls). This
> note amends the foldcheck note above and the RT-4 states before it; where they disagree, this
> note wins. The one idea: **if Thunderbird could have shown the tree, or anything not MailArchive's
> is in it, remove nothing.**
> - **Which restores Start settles.** Each started line for this profile with no closing line
>   ("ended", "cleaned" or "abandoned"), in the order of the foldcheck note's first bullet (all locks
>   held). Each is settled by the first of these that applies:
>   0. **[N-f] Is its Local Folders this one?** The started line recorded the Local Folders path and
>      identity. If the profile's Local Folders now has another path or identity, or the recorded
>      one cannot be reached (its drive is not connected), the restore is **elsewhere**: nothing is
>      removed, no line is written (so it stays open and is looked at again at every Start), and the
>      result says W-ELSEWHERE. Only after this passes can case 1 decide that a container is absent.
>      This replaces the RT-4 note's "Local Folders drive missing" state, whose words are superseded
>      by W-ELSEWHERE (§7.5 foldcheck2 note).
>   1. Case 1 as the foldcheck note says (container absent, or without this ID's marker → "abandoned",
>      nothing removed), now reached only after step 0.
>   2. Case 2 as the foldcheck note says, with one more condition [residual case]: `Restored — NAME`
>      must be a **regular file** by `Lstat` through the root, not a folder or a link. Then "ended",
>      nothing removed.
>   3. Case 3 of the foldcheck note is superseded by [foldcheck2 P1] and split in two:
>      - **3a. An entry named `Restored — NAME` is beside the container** (a file, folder or link,
>        made by any program), and the restore did not commit: **remove nothing.** The marker and the
>        made-list stay, "abandoned" is appended, and the result gives W-VISIBLE. From then on it is
>        the person's folder: backups take it as ordinary mail (§7.2 foldcheck2 note, P3), and Desktop
>        start names it no more.
>      - **3b. No entry of that name** → the cleanup below, then "cleaned" (or "abandoned" when the
>        cleanup removes nothing).
> - **The cleanup, in decision order** [P1, N-a]. Every cleanup (at Start, and the run's own after
>   Cancel, Shut down, a failure, W-CHECKFAIL or W-TAKEN) goes through these steps in this order, and
>   nothing is removed before step 4. It replaces the order of the foldcheck note's rules 1–4; those
>   rules' checks stand where this list cites them.
>   1. **The parent name.** `Lstat` `Restored — NAME` through the root. Any entry there → case 3a:
>      remove nothing. (In a live W-TAKEN this is always so; §5.4 foldcheck2 note.)
>   2. **The made-list**, read as rule 1 says. A list that cannot be parsed → rule 6: remove nothing,
>      "abandoned", the made-list-unreadable words.
>   3. **Scan the whole container before removing anything.** Through the root, following no link,
>      starting at the container and going down only into entries the list names "d" that `Lstat`
>      shows as directories. Each entry found must be the marker, the made-list, or an entry the list
>      names, of the kind it names ("f" a regular file, "d" a directory). Anything else (a name not on
>      the list, such as a Thunderbird `.msf` file, the usual sign that Thunderbird has shown a
>      folder, or a file the person put there; a link; a kind that does not match) → **remove
>      nothing**: the marker and the list stay, "abandoned" is appended, the result says W-LEFT. The
>      scan reads each directory in batches and stops at the first entry not on the list, so it does
>      no more work than the list's own size (at most 16 MiB) allows. Rule 4's "is **left**. Then the
>      made-list and the marker are kept too" is superseded by [foldcheck2 N-a]: nothing is removed,
>      not only the unlisted entries.
>   4. **Only now, remove**, as rules 2 and 3 say: the listed entries in reverse order, each `Lstat`
>      again for its kind, absent ones passed over, a directory only when empty; then the made-list,
>      the marker, the container. If a directory is not empty at its turn, or a kind has changed
>      (something was added after the scan), removal stops: what is left stays, the list and marker
>      with it, "abandoned" is appended, and the result says W-LEFT.
>   5. **A removal blocked** (rule 5, unchanged): the cleanup stops, no line is written, and the next
>      Start tries again from step 0. [N-g] At Start, this does not stop the new restore.
>   6. Rule 7 stands: the parent name is never removed, nor anything outside the container.
>
>   > Settled 2026-11-07 [ADV-02, ADV-03] (the R3–R5 adversarial pass):
>   > - [ADV-02] Step 3's scan now also records what it saw of the container and of each entry it
>   >   matched, and step 4's "each `Lstat` again for its kind" becomes: each removed only when it,
>   >   and every folder on the way to it, is still exactly the entry the scan saw — walked down from
>   >   Local Folders one folder at a time by handle, following no link, and removed relative to its
>   >   checked parent (§9 ADV-02 note). Any difference stops removal, as a changed kind always did.
>   > - [ADV-03] A listed name whose exclusive create failed because something already stood there
>   >   is not the restore's: the line was fsynced first (M1), so the list names it, but the run
>   >   knows it did not make it. The writer keeps those names and hands them to its own cleanup, whose
>   >   scan never takes them for the list's: finding one there removes nothing (W-LEFT, "abandoned").
>   >   **What is left:** a crash between a list line and its create, when another program made that
>   >   name in that instant, cannot be told apart at a later Start from the restore's own entry, and
>   >   that Start's cleanup would remove it. It needs another program writing inside the restore's new
>   >   hidden folder within milliseconds of a crash; accepted, as the pass recommended.
> - **What the scan cannot see.** A listed file changed in place, by hand, outside Thunderbird, in a
>   folder Thunderbird does not show. Thunderbird itself can change a listed file only once it shows
>   the tree, which needs the parent name, which step 1 catches. The hand-edit case is in the R3–R5
>   adversarial pass's scope.
> - **[N-c] The parent file's fsync fails after a successful create**: §5.4 foldcheck2 note (a
>   regular, empty parent file on re-check → committed; otherwise "abandoned", nothing removed,
>   W-COMMITERR).
> - **[N-g] Start always goes on.** Whatever settling each earlier restore came to (cleaned, ended,
>   abandoned, blocked or elsewhere), the new restore then runs under the name the plan chose. That
>   name was chosen free of every leftover (a " (n)" name when a leftover holds the first), and the
>   re-check (§5.3) confirmed it. After its own result, the result names each earlier restore that
>   was not cleaned up, in its row's words (§7.5 foldcheck2 note). The one exception is the RT-4 note's
>   point that a profile whose own Local Folders is on a missing drive cannot be restored into at all;
>   that refusal is the plan's, as before.
> - **The plan predicts, Start decides.** Check and `-plan` still write nothing. They run steps 0 to
>   3 read-only and say, per open restore, what Start will do: W-UNFINISHED's "will be removed first"
>   (3b, scan clean), W-VISIBLE, W-LEFT, made-list unreadable, or W-ELSEWHERE. Start decides again
>   under its locks and reports what it did.
> - **[Residual case] the two owed sentences.** In the foldcheck note's bullet "A crash during a
>   W-TAKEN cleanup …", the one case left (a crash between a failed parent-file create and the fsynced
>   "taken" line) stays as written, with these two sentences: Under O1's answer (b), backups skip the
>   parent file beside the restored tree too, so the other program's folder, and its own mail, is not
>   backed up while the restored tree sits beside it (§7.2 foldcheck2 note). And case 2 requires that
>   parent to be a regular file (`Lstat`): lab C showed a folder of that name also fails the create,
>   and a folder would not show the tree, so that state is case 3a and is never recorded as ended.
>   (The same bullet's "it is case 3, not case 2" now means case 3a: nothing is removed.)
> - **Superseded in the RT-4 states above** [foldcheck2]:
>   - The intro's "In every state Thunderbird shows nothing that is not finished, because the parent
>     file is the last thing made" holds for every state MailArchive itself makes. It is superseded
>     by [foldcheck2 P1] where another program makes the parent name: then Thunderbird can show an
>     unfinished tree, which is why case 3a removes nothing and W-VISIBLE says so. The same intro's
>     "backups skip it meanwhile" is narrowed by [foldcheck2 P3] (§7.2 foldcheck2 note).
>   - "Crash after the parent file, before 'ended' … the final name present and its marker matching
>     → treated as finished" is superseded by case 2 (foldcheck M2's three-part rule, plus the
>     regular-file check).
>   - "Crash during cleanup … Next attempt: finishes the cleanup." now means: runs the decision
>     order again from step 0 (absent entries passed over).
>   - "Thunderbird opened between a crash and the next attempt … If the cleanup meets entries it
>     did not make, it leaves them and says W-LEFT" is superseded by [foldcheck2 P1]: it removes
>     nothing at all (step 3), and if the parent name is there, step 1 already stopped it.
>   - "The parent file's name taken at the commit. W-TAKEN: the tree removed, nothing added" is
>     superseded by [foldcheck2 P1]: nothing is removed; W-TAKEN's new words.
> - **R40 in every crash case** (re-stated, §11 foldcheck2 note). What the next Start does after a
>   crash at each point, and what it removes:
>
>   | Crash at | Start settles it as | Removes |
>   |---|---|---|
>   | after "started", before `Mkdir` | case 1, "abandoned" (after step 0) | nothing |
>   | after `Mkdir`, before the marker | case 1, left; the next restore takes " (2)" | nothing |
>   | after the marker, before or while making the made-list | 3b: no list, so it lists nothing; the scan finds only the marker | the marker and the empty container, both made by this restore |
>   | while writing, or after "checked" before the parent file; no parent name, nothing unlisted | 3b: scan clean, then remove | only listed entries: made by this restore, never shown by Thunderbird (no parent file; lab A3) |
>   | the same, but another program has since made `Restored — NAME` | 3a, "abandoned", W-VISIBLE | nothing |
>   | the same, but something unlisted is inside (a `.msf`, a person's file) | 3b, step 3: "abandoned", W-LEFT | nothing |
>   | the parent create failed on a taken name, before "taken" (residual) | case 2 if that entry is a regular file, else 3a | nothing |
>   | after "taken" | 3a, "abandoned" | nothing |
>   | after the parent file, before "ended" | case 2, "ended" | nothing |
>   | the parent file's fsync failed, then a crash | a regular parent file: case 2; none: 3b | as the rows above |
>   | during a cleanup's removals | 3b again (the parent name was absent, or removal would not have begun) | only listed entries still there |
>   | any of these, with Local Folders since moved or its drive not connected | step 0, elsewhere, no line | nothing |
>
>   So in every case Start removes only entries this restore's made-list names, and only when no
>   entry of the parent name is beside the container and nothing unlisted is in it: only what
>   MailArchive made, and only what Thunderbird could not have shown. R40 holds.

> Folded 2026-11-06 [foldcheck3 S1, S2, S3] (lead's call). The third fold check
> (`docs/review-restore-thunderbird-foldcheck3.md`, commit b37694e, PASS_WITH_NOTES) named three
> notes for slice R0. This note amends the foldcheck2 note above; where they disagree, this note wins.
> - **[S1] What the cleanup removes, said truly.** The table's closing sentence above ("Start removes
>   only entries this restore's made-list names") leaves out that steps 3 and 4 also remove the
>   made-list itself, the marker and the emptied container, none of which is on the list. It now
>   reads: Start removes only the entries this restore's made-list names, then that made-list, its
>   marker and the emptied container (all made by this restore, by exclusive create); it removes
>   nothing while any entry named `Restored — NAME` is beside the container, nothing when the log has
>   a "taken" line for the restore (S3 below), nothing when anything the list does not name is inside
>   it, and so nothing Thunderbird can show. R40's cleanup clause is re-worded to match (§11
>   foldcheck3 note).
> - **[S2] Row 5 of the table, split.** The row "the same, but another program has since made
>   `Restored — NAME`" (settled as 3a) is true only before "checked", or when that entry is a folder
>   or a link. It is replaced by two rows:
>
>   | Crash at | Start settles it as | Removes |
>   |---|---|---|
>   | while writing (no "checked" line), then another program makes `Restored — NAME` (any kind); or after "checked", and the entry another program made is a folder or a link | 3a, "abandoned", W-VISIBLE | nothing |
>   | after "checked", before the parent file, then another program makes a **regular file** `Restored — NAME` | case 2, "committed (case 2, ended)" | nothing |
>
>   The second row is a second instance of the residual case's cost (the residual bullet above): the
>   restore is recorded as finished although MailArchive did not make the parent file, and under O1's
>   answer (b) backups skip that other program's file beside the restored tree. It removes nothing
>   (the safe direction) and is as rare as the residual case: a crash in the commit's instant, then
>   another program making that exact name while Thunderbird is closed. It is in the R3–R5
>   adversarial pass's scope with the residual case. MA-1123 ("every row of §8's foldcheck2 table")
>   tests this table as split here.
> - **[S3] A "taken" line always means remove nothing.** The table's row "after 'taken' → 3a,
>   'abandoned'" was unconditional, but the cleanup's step 1 looks only at what is beside the
>   container *now*. After a crash after "taken", Thunderbird is no longer held out and can show the
>   tree under the other entry; if a program other than Thunderbird then removes only that entry,
>   step 1 finds nothing and the cleanup would go on to 3b, with only the scan's `.msf` check left
>   before the person's mail. So, lead's call: **a "taken" line for the restore in `restores.log`
>   always settles it as 3a: nothing is removed, "abandoned" is appended, whatever exists beside the
>   container now.** The log already proves a parent name existed at the commit. The cleanup's
>   decision order gains a step before step 1: **0b. A "taken" line?** Then 3a, remove nothing. The
>   live W-TAKEN cleanup (the run's own, after its failed create) is the same: the "taken" line is
>   fsynced before the cleanup, so it removes nothing, whatever the parent name's state at step 1.
>   Two sentences are corrected:
>   - In "What the scan cannot see" above, "Thunderbird itself can change a listed file only once it
>     shows the tree, which needs the parent name, which step 1 catches" is superseded by [foldcheck3
>     S3]: step 1 catches a parent name that is there *now*. One that was there earlier is caught by
>     the "taken" line when MailArchive's own create met it (step 0b). When another program made it
>     and removed it again with no "taken" line (a crash before the commit, row 5's path), only the
>     scan's sign stands: the `.msf` files Thunderbird makes inside a folder it shows. That path stays
>     in the R3–R5 adversarial pass's scope.
>   - §12.3's foldcheck2 note's "Nothing in the cleanup rule rests on the answer (removing nothing is
>     safe either way); only the words do" is corrected beside it (§12.3 foldcheck3 note): reading (3)
>     of that lab step is what the scan's backstop rests on.

> Folded 2026-11-07 [foldcheck3 S4, S5, S6, S8, S9] (lead's calls, owed to slice R4; built there;
> the exact wording of each row is the builder's, under the owner-delegated rule). Each row is shown
> only in the state it names.
> - **[S4] W-LEFT's step-4 form.** When removal began and then met something new (cleanup step 4),
>   the words are: "MailArchive removed part of what it wrote in the folder 'Restored — NAME.sbd', in
>   PATH, then found something there it did not make (FIRST NAMES), so it removed nothing more.
>   Thunderbird does not show that folder. Close Thunderbird and delete it yourself when you no
>   longer need those files." (FIRST NAMES left out for an encrypted archive.) The step-3 form (nothing
>   removed) keeps W-LEFT's foldcheck2 words. Both are "abandoned". One more rule, the builder's: the
>   made-list and the marker are removed only once the container holds nothing else (it is read once
>   more just before), so a cleanup stopped by something new always keeps them and backups keep leaving
>   the folder out.
> - **[S5] The live states that had no exact row.**
>   - A live Cancel, failure or W-CHECKFAIL whose cleanup meets an entry of the parent name (no
>     "taken" line: MailArchive never tried to make it): "While the restore ran, something made an
>     entry named 'Restored — NAME' in Local Folders, so MailArchive stopped and removed nothing.
>     Thunderbird may show the folders MailArchive had written inside it, and some messages in them may
>     be missing or cut short. They are yours: if you moved mail into them, move it out first, then
>     delete them in Thunderbird and restore again. Until then MailArchive backs them up like your other
>     mail." ("abandoned".)
>   - A live W-TAKEN whose entry has gone again by the cleanup's step 1: the "taken" line still means
>     remove nothing (S3), so the old "removed what it wrote" words do not fit either. New words:
>     "Something made, then removed again, a folder named 'Restored — NAME' in Local Folders while the
>     restore ran. MailArchive had already written and checked every message, and removed nothing: the
>     restored folders are in 'Restored — NAME.sbd' in PATH, which Thunderbird does not show. Close
>     Thunderbird and delete that folder yourself when you no longer need it, then click Check again to
>     restore." The same state found by a later Start has its own form ("An earlier restore into
>     Thunderbird (DATE) wrote and checked every message, but its folder's name was taken by something
>     else while it ran, so MailArchive removed nothing: …").
>   - A parent create that fails for a reason other than "exists" but leaves an entry, and the case
>     variant lab C showed failing the create on a volume that ignores case: both are treated as a
>     taken name (an entry of the name is there by `Lstat`, or the create said it exists): "taken",
>     then W-TAKEN. A create that fails with no entry there is a failure: the one cleanup, then the
>     failure words ("The restore could not finish (WHY), so nothing was added to Thunderbird. The part
>     already written was removed. …", W-FAILED).
> - **[S6]** W-VISIBLE's unchecked form says "stopped before it finished" (the crash may have come
>   during the post-write check); both W-ELSEWHERE forms say "may not have finished" (with the drive
>   gone a committed restore cannot be told from an unfinished one).
> - **[S8] The scan matches names as the file system does.** Each made-list path and each name the
>   directory read returns are compared NFC-normalised, so a name an HFS+ volume stored decomposed is
>   still the list's (no false W-LEFT on every cleanup of an accented folder); each listed name is
>   matched once, so a second entry that spells the same (a decomposed twin on a volume that keeps both)
>   is not the list's and the cleanup removes nothing. Removal uses the list's own spelling, which such
>   a volume finds whatever its normalisation. Case is not folded (the volume returns the name as
>   MailArchive made it), which can only make the scan stricter.
> - **[S9]**
>   - An "elsewhere" restore whose recorded Local Folders is reachable, with its recorded identity, and
>     no longer holds the container (the person deleted it) is closed "abandoned" at the next Start and
>     not named again; one whose drive is not connected, or whose container is still there, stays open
>     and is named at each Start (it can still be dealt with when the drive comes back).
>   - Which "regular file" applies where: Start's case 2 wants a **regular file of any size**
>     (Thunderbird may since have filed mail in "Restored — NAME", the top folder); the live run's own
>     re-check after a failed fsync (N-c) wants a **regular empty file** (right after its create nothing
>     else can have written into it, Thunderbird being held out).

## 9. Security

The threat model is the repo's: another local account is out of scope only where the OS
protects the files; the archive and the profile files are read as untrusted text.

1. **Folder names from the archive are untrusted** (someone else may have written the
   manifest, `util.ValidRecordedPath`'s note, `paths.go:307-312`). They are split on "/",
   each part made a single safe name (§4.4), so no part is empty, ".", "..", starts with
   ".", or holds a separator; store tokens must pass `state.SafeToken`
   (`internal/state/manifest.go:455-471`). The tree's bounds apply (300 deep, 1,024-byte names,
   10,000 folders, `copyexport_archive.go:415-431`) — for the person's own archive too,
   because a restore writes files for each.
2. **Only under the chosen Local Folders.** Every write goes through an `*os.Root` opened on
   that directory (`copyexport_fs.go:15-31`), which cannot reach outside it, and every
   created name is new (exclusive create, no-replace rename). Nothing existing is opened for
   writing, so no planted link or junction in Local Folders is ever followed.
3. **The directory itself.** Links on the way to Local Folders are resolved once, the result
   shown in the plan and pinned by identity (§3.2 step 4); a swap between plan and start is
   refused (§5.3). It may not be another account's directory or under ImapMail (§3.2 step 3).
4. **profiles.ini paths.** Used to write only after the profile checks of §3.1 step 3,
   which presync uses to decide what it may start (`resolve_darwin.go:108-150`). On Windows,
   ownership is not checked, as presync states (`resolve_windows.go:116-121`); the profile's
   parent, `%APPDATA%`, is the user's own.
5. **The page never names a path**; it names positions in lists the server built (§3.1 step 6,
   §7.3).
6. **Permissions.** New files 0o600, new folders 0o700 on macOS and Linux
   (`copyexport_fs.go:78`, `:83`); on Windows they take Local Folders' own permissions. Refused when
   running elevated or as root (W-ELEVATED), so Thunderbird can always change them later.

   > Settled 2026-11-07 [R6-01] (the R6 adversarial pass; the lead's call): this item was written
   > before the encrypted path. "On Windows they take Local Folders' own permissions" is replaced:
   > every folder and file a restore makes is made readable by this account alone and read back
   > (Windows: an explicit, protected owner-only access list; macOS: the inherited access list
   > removed too), for a plain archive where it can be and for an encrypted one always — or, where
   > the drive cannot hold it, only after the second confirmation, with the plan saying who else can
   > read (§6.2's R6-01 note). Thunderbird, running as this user, can still change them.
7. **Encrypted archives.** Plaintext only inside the destination, under the staging names
   until the end, and removed on cancel or failure (§6.2, §8). Memory is not wiped beyond
   what the vault already does.
8. **Requests.** Unchanged guards (§7.3); the start is a one-use token of its own kind.

This design owes the adversarial pass on the writer and the profile lock (§13).

> Folded 2026-11-06 [RT-7, RT-19, RT-2, RT-8, RT-5]:
> - **§9.1** [RT-7]: as written, the tree's bounds do NOT apply to the current archive (the builder
>   skips its node and id caps then, `copyexport_archive.go:508-511`). The restore applies its own
>   caps to what it creates (§6.5 note, W-TOOMANY), whoever wrote the manifest.
> - **§9.2** [RT-19, RT-2]: "nothing existing is opened for writing" excludes Thunderbird's lock file
>   (`parent.lock` / `.parentlock`), which is opened (created if absent) and locked but never written
>   to. "Every created name is new" holds by exclusive create (§5.4 note); "cannot replace" also rests
>   on the file-placement helper, which on FAT/exFAT under macOS and Linux falls back to a
>   look-up-then-rename with a stated race window (`copyexport_place_unix.go:27-33`); that window is
>   inside a folder that Thunderbird does not show and nothing else writes to.
> - **§9.7** [RT-8]: "removed on cancel or failure" means deleted from the folder, not erased: a sync
>   upload, a backup or snapshot, or the disk itself may still hold it. Said in the words (§6.2 note,
>   §7.5 Stopped).
> - **Lock files** [RT-2]: the restore never makes a `lock` symlink, and never uses the `lockfile`
>   package on Thunderbird's files.
> - **The marker** [RT-5]: a marker copied or planted elsewhere does nothing, because a container is
>   skipped or cleaned only when its marker's ID is in this computer's `restores.log` for this
>   profile (§3.4 note). The one weaker case is an unreadable `restores.log`, when backups skip every
>   marked container that has no parent file (§7.2 note); that fallback is in the adversarial pass's
>   scope.
> - The adversarial passes owed are now named per slice in §13's note.
>
> Folded 2026-11-06 [foldcheck M5, M1] (lead's calls):
> - In the §9.2 bullet above, "also rests on the file-placement helper, which on FAT/exFAT … falls
>   back to a look-up-then-rename with a stated race window" is **superseded by [foldcheck M5]**.
>   Nothing is placed or renamed, so "cannot replace" rests on exclusive create alone (`O_EXCL` and
>   `Root.Mkdir`).
> - **The made-list** [M1] is read back by a later attempt from a folder that another program could
>   write into. Its paths are checked (relative, no empty, `.` or `..` part), used only through the
>   root, never followed through a link, and act only inside a container whose marker's ID is in
>   this computer's `restores.log` for this profile (§8 foldcheck note). The R3–R5 adversarial pass
>   covers it.
>
> Folded 2026-11-06 [foldcheck2 P1, N-c] (lead's calls):
> - **§9.7, "removed on cancel or failure"**, is narrowed by [foldcheck2 P1]: a cleanup that finds
>   the parent name, or anything unlisted, or cannot read its list, removes nothing (§8 foldcheck2
>   note), and so does W-COMMITERR. For an encrypted archive that leftover is plaintext mail inside
>   Local Folders until the person deletes it. Each of those results names the folder and its path,
>   and Desktop start keeps naming it while it is there and Thunderbird does not show it (§7.2
>   foldcheck2 note). The choice is deliberate: deleting it could delete mail that is not
>   MailArchive's.
> - **The scan and the parent-name check** (§8 foldcheck2 note, cleanup steps 1 and 3) join the made-list
>   in the R3–R5 adversarial pass: a planted entry can only make the cleanup remove less, never more,
>   and the scan is bounded by the list's size.
>
> Settled 2026-11-07 [ADV-02] (the R3–R5 adversarial pass): the bullet just above held only without a
> race. A listed folder swapped for a link to Local Folders between the scan and the removals walked
> the removal (through `os.Root`, which follows a link inside its root) onto the person's file of a
> listed name. Now the scan records what it saw of the container and of every entry it matched, and
> each removal (`FS.RemoveSeen`) walks down from Local Folders **one folder at a time by handle,
> following no link** — `openat(O_DIRECTORY|O_NOFOLLOW)` on macOS and Linux, `NtCreateFile` relative to
> the parent handle with `FILE_OPEN_REPARSE_POINT` on Windows — and requires each folder on the way, and
> the entry itself, to be the very one the scan saw (the same identity and kind; never a link, junction
> or reparse point). Any difference stops removal (W-LEFT's step-4 form, "abandoned"). The entry is
> then removed relative to its checked parent: `unlinkat` on macOS and Linux, which never follows a
> link; on Windows by its own handle (opened with `DELETE`, its disposition set), so exactly the
> checked object is removed. **The window left**, on macOS and Linux only: between the entry's last
> `fstatat` and its `unlinkat`, another program of the same user could put something else at that
> name inside the checked folder — the restore's own hidden folder, which Thunderbird is held out of —
> and that thing (a link itself, never its target) would be removed. It cannot reach anything outside
> the container. MA-1122's ADV-02 test makes the swap at three instants (the removal loop's `Lstat`,
> the removal's call, and inside it after its check, through `beforeUnlinkSeam`).
>
> Settled 2026-11-07 [ADV-06] (the R3–R5 adversarial pass): the RT-5 bullet above ("a marker copied or
> planted elsewhere does nothing, because a container is skipped or cleaned only when its marker's ID
> is in this computer's `restores.log` for this profile") did not hold for the backup skip: the walk
> compared the marker's profile key with the log's started line, both written by the same restore, and
> nothing with the profile being backed up. Now a backup leaves a marked folder out only when the
> folder it met the container in is, **by identity, the Local Folders the restore's started line
> records** (the recorded path still that folder, and the walk's folder that same folder), as well as
> the profile key matching (§7.2 ADV-06 note). A copied profile, or a copy on a disk the person backs
> up, is backed up like any other mail. The cleanup's side was already bound this way (Start compares
> the recorded Local Folders, §8 step 0).

## 10. The command line

```
mailarchive restore-thunderbird -out ARCHIVE [-profile FOLDER | -profile-name NAME]
    [-folder PATH]... [-store LABEL] [-name NAME] [-plan] [-yes] [-record-out FILE]
mailarchive restore-thunderbird -list-profiles
```

- `-folder` takes a folder as the tree spells it ("Inbox/Receipts"); `-store` when two stores
  have one of that name; repeated `-folder`s add up; none means the whole archive.
- `-name` replaces the archive's name after "Restored — ".
- Without `-plan`, prints the plan and asks "Restore? [y/N]" on a terminal; without a terminal
  and without `-yes`, refused.
- An encrypted archive asks its password as `extract` does.
- Exit codes, in the shape `extract` and `decrypt` declare (`docs/ux-contract.md:11`, X1):
  `0` every chosen message restored · `3` partial (some not restorable, each named) · `1`
  refused, nothing written · `130`/`143` stopped by Ctrl-C or terminate, nothing added.
  X1 gains this verb's line.
- Desktop and the command line call one engine (a new package, `internal/tbrestore`:
  `Profiles`, `Plan`, `Run`), as delete's two surfaces do.

> Folded 2026-11-06 [RT-8, RT-2, RT-12, lab finding 3]:
> - New flag `-not-encrypted-ok`: needed with `-yes` for an encrypted archive without a terminal.
>   When the Local Folders may leave the computer (§6.2 note), an encrypted restore is refused without
>   a terminal, whatever the flags, and at a terminal asks for the Local Folders path to be typed,
>   as `decrypt` does (`cmd/mailarchive/decrypt.go:58-86`).
> - The plan goes to stderr and the result to stdout, as `decrypt` does (X8). W-WAIT is printed with
>   the plan and again when writing starts.
> - Exit codes unchanged in shape: W-CHECKFAIL, W-TAKEN, W-RESTORING and a failed lock take are `1`
>   (nothing added); a restore finished by a later attempt (§8 note) is reported in that attempt's
>   output, and its own exit code is that attempt's.
> - `-plan` reads `restores.log` and reports an unfinished restore, but writes nothing (cleanup happens
>   only at Start, under the locks).
>
> Folded 2026-11-06 [foldcheck M2] (lead's call): "a restore finished by a later attempt (§8 note) is
> reported in that attempt's output" above is **superseded by [foldcheck M2]**: no attempt finishes
> an earlier one. A cleanup at Start, or an "ended" recorded for a restore that had committed, is
> reported in that attempt's output, and its exit code is that attempt's own. `-plan` writes nothing,
> as the bullet above says, and neither does Desktop's Check.
>
> Folded 2026-11-06 [foldcheck2 P1, N-c, N-g] (lead's calls; the exit codes are the folding agent's
> answer):
> - "W-CHECKFAIL, W-TAKEN, W-RESTORING and a failed lock take are `1` (nothing added)" is
>   superseded by [foldcheck2 P1] for W-TAKEN: the restored tree stays and Thunderbird can show it.
>   W-TAKEN, W-COMMITERR's "abandoned" outcome, and a W-CHECKFAIL whose cleanup removed nothing
>   still exit `1` (the restore did not finish); a stop by Ctrl-C or terminate whose cleanup removed
>   nothing still exits `130`/`143`. In each, the words printed say what was left and where. X1's
>   line for this verb (R6) reads "`1` not restored, `130`/`143` stopped; the words name anything
>   left behind", not "nothing written" or "nothing added".
> - [N-c] A parent-file fsync error whose re-check passes is a finished restore: `0` (or `3`), with
>   the error named, as step 10's errors are.
> - [N-g] The exit code is the new restore's own; an earlier restore that Start could not clean up
>   does not change it, and is named after the result.

> Settled 2026-11-07 [R6-08, R6-09, R6-10] (the R6 adversarial pass):
> - **[R6-08] `-record-out`** only refused an existing file, so `-plan -record-out` could put a
>   plaintext record (profile, Local Folders, the restored folder's name, the counts) inside the
>   encrypted archive. It now follows the saved file's rules, as `delete-archive`'s does
>   (`keyring.CheckNewFileOut`, checked before anything is read or asked, then `keyring.WriteNewFile`):
>   never inside the archive folder or any encrypted archive, never over a file, made owner-only
>   (on Windows too) before it is written. Test: `cmd/mailarchive/ma1129_r608_r609_r610_test.go`.
> - **[R6-09] A second Ctrl-C** exits at once, as before (130, or 143), before the restore's own
>   cleanup — but once writing has begun it first says what may be left and where: "stopped at once,
>   before the restore could remove what it wrote: its unfinished folder 'Restored — NAME.sbd' (or
>   one numbered after it) may be left in PATH. Thunderbird does not show it; the next restore into
>   this profile removes it, or close Thunderbird and delete it yourself." and, for an encrypted
>   archive, "It may hold decrypted mail." (§10's "the words name anything left behind" now holds
>   for this exit too.)
> - **[R6-10]** "Checking what was written: every message is read back before Thunderbird can show
>   the folder…" is printed when the post-write check begins, and Ctrl-C is heard during it (§5.4
>   step 7 note).
> (All MA-1129.)

## 11. New invariant and scenarios (provisional ids)

- **R40 — A restore adds, and only adds.** A restore to Thunderbird writes only new entries
  inside the chosen profile's Local Folders directory (and holds Thunderbird's profile lock
  while it writes), never changes, renames, replaces or removes anything that was there,
  never writes under another account's directory, only while Thunderbird is closed, and
  makes the new folder visible in one step at the end. It writes only preserved originals.
- Scenarios S66 Thunderbird open at Check; S67 Thunderbird started during a restore; S68 a
  crash part way; S69 a hostile folder name in the archive; S70 restoring into the profile
  the archive backs up.

> Folded 2026-11-06 [RT-19, RT-1, RT-2, RT-5, O1]: R40's wording, as it goes into the catalog in R0:
> **R40 — A restore adds, and only adds.** A restore to Thunderbird creates only new entries, each
> by exclusive create, inside one new folder of the chosen profile's Local Folders directory; it
> never changes, renames, replaces or removes anything it did not create; it never writes under
> another account's directory; it writes only while Thunderbird is closed and held out by
> Thunderbird's own profile lock, which it opens but never writes; it writes only preserved originals
> (for an mbox source, the bytes the archive kept), each checked after writing; and the folder
> becomes visible to Thunderbird in one step, by the creation of its parent file, last. (What it
> rests on: lab A3 and A5 on Windows; macOS and Linux lab rows MA-1091, MA-1098, MA-1099. "Cannot
> replace" on FAT/exFAT under macOS and Linux: §9 note.)
> **R41 (new) — MailArchive never backs up its own restore leftovers.** A backup leaves out every
> folder carrying a marker whose ID is in `restores.log` for that profile and has no parent file,
> and every `.mailarchive-part-` working file; with O1's recommended answer, also the finished
> restored folders it lists.
> New scenarios: **S71** a scheduled backup with pre-sync fires during a restore (pre-sync does not
> start Thunderbird; the backup skips quietly); **S72** an encrypted archive restored into a synced
> or removable Local Folders (asked twice); **S73** a profile with no Local Folders (refused, nothing
> changed); **S74** antivirus quarantines a restored file before the commit (caught by the check,
> nothing shown). Ids R40, R41 and S66–S74 are free on main and `cloud-sync-note` (checked
> 2026-11-06); provisional until R0.
>
> Folded 2026-11-06 [foldcheck N5, M5, M3]:
> - [N5] (folding agent's answer): R40's folded wording says the restore opens Thunderbird's lock
>   "but never writes" it, yet the restore also creates that file when it is absent. So R40, as it
>   goes into the catalog, reads "… held out by Thunderbird's own profile lock file, which it opens,
>   creating it only when absent, and never writes to; …".
> - [M5] (lead's call): R40's clause "'Cannot replace' on FAT/exFAT under macOS and Linux: §9 note" is
>   **superseded by [foldcheck M5]**: nothing is renamed, so the clause is dropped.
> - [M5, M3] (lead's calls): R41's "and every `.mailarchive-part-` working file" is **superseded by
>   [foldcheck M5]**: there are none. R41 instead reads: "A backup leaves out every folder carrying a
>   marker whose ID is in `restores.log` for that profile (re-reading the log once for a marker it
>   does not know) and has no parent file, the hidden marker and made-list inside it included; with
>   O1's recommended answer, also the finished restored folders it lists."
>
> Folded 2026-11-06 [foldcheck2 P1, P3, the residual case] (lead's calls):
> - **R40, re-stated** [P1]. The folded R40 (with N5's lock-file words) gains one clause after "it
>   never changes, renames, replaces or removes anything it did not create;": "its cleanup, after a
>   cancel, a failure or a crash, removes only entries this restore's made-list names, and only when
>   no entry named `Restored — NAME` is beside the restore's folder and nothing the list does not name
>   is inside it; otherwise it removes nothing;". The clause "the folder becomes visible to
>   Thunderbird in one step, by the creation of its parent file, last" stands for what MailArchive
>   makes; when another program makes that name, nothing is removed and the person is told (§8
>   foldcheck2 note, case 3a). §8's foldcheck2 table shows R40 holding at every crash point.
> - **R41, reworded** [P3, residual case]. The foldcheck wording's "and has no parent file, the hidden
>   marker and made-list inside it included; with O1's recommended answer, also the finished restored
>   folders it lists" is superseded by [foldcheck2 P3]. R41 reads: "**R41 — MailArchive never backs up
>   its own restore leftovers.** A backup leaves out every folder carrying a marker whose ID is in
>   `restores.log` for that profile (re-reading the log once for a marker it does not know) and with
>   no entry of its parent name beside it, the hidden marker and made-list inside it included. With
>   O1's recommended answer it also leaves out a restored folder whose restore committed (its parent
>   is a regular file, and the log has that ID's 'checked' line and no 'taken' line), with that parent
>   file. Any other marked folder with an entry of its parent name beside it is backed up like any
>   other folder."
>
> Settled 2026-11-07 [ADV-06, ADV-07] (the R3–R5 adversarial pass; ADV-07 the lead's call): R41's
> wording above gains two narrowings, written beside it in the catalog
> (`docs/scenario-catalog.md` R41, "Amended 2026-11-07"): "for that profile" means for that profile
> **and that Local Folders** — the folder the backup meets the marked folder in must be, by identity,
> the Local Folders the restore's started line records (§7.2 ADV-06 note); and a leftover with no
> entry of its parent name is left out only while its restore is **open or committed** — one closed
> "abandoned" is backed up like any other folder, since it may hold the person's mail, accepting that
> a cut-off last message of a failed restore may be copied into the archive once (§7.2 ADV-07 note);
> "committed" excludes a restore closed "abandoned".

> Folded 2026-11-06 [foldcheck3 S1, S3] (lead's call): R40's cleanup clause, as the foldcheck2 note
> above words it, is false about the design's own cleanup, which also removes the made-list, the
> marker and the emptied folder (none of them on the list). It is superseded by [foldcheck3 S1]. The
> clause after "it never changes, renames, replaces or removes anything it did not create;" reads:
> "its cleanup, after a cancel, a failure or a crash, removes only the entries its made-list names,
> then its own made-list, its marker and the emptied folder; it removes nothing while any entry named
> `Restored — NAME` is beside the folder, nothing for a restore whose `restores.log` has a 'taken'
> line, nothing when the folder holds anything the list does not name, and so nothing Thunderbird can
> show;". The "taken" words are [foldcheck3 S3] (§8 foldcheck3 note). R40 goes into the catalog in
> R0 with this clause.

## 12. Tests

### 12.1 Ids

On main the catalog's last rows are MA-1082 (`docs/scenario-catalog.md:1450`) and MA-1100..
MA-1118 (`:1431-1444`); MA-1083 is taken on branch `cloud-sync-note` (not merged). So this
design takes **MA-1084..MA-1099**, then **MA-1119** on. Provisional until slice R0 writes them
into the catalog (structural id allocation, which the owner lets the lead assume).

> Folded 2026-11-06 [RT-24]: the fold adds **MA-1133..MA-1143** (free on main, `cloud-sync-note`,
> `switch-keeps-runjob` and this branch, checked 2026-11-06). The review's statement stands that
> MA-1084..1099 and MA-1119..1132 are free on main and nine branches. MA-1083 is on branch
> `cloud-sync-note`. All provisional until R0.

### 12.2 Unit and integration rows (U/S, fake profile in a temp folder, real files)

| Id | Proves |
|---|---|
| MA-1084 | profiles.ini: relative and absolute paths, names, the Install default, several profiles, a missing or oversized file; the shared parser gives presync the same paths as before |
| MA-1085 | prefs.js: Local Folders found by `localfoldersserver` and by type "none"; `directory-rel` over `directory`; doubled backslashes and `\uXXXX` decoded; none or two refused (W-NOLOCAL) |
| MA-1086 | store kind: mbox by default, maildir by per-server and by profile-wide setting, and by `cur`/`new` on disk, each refused with W-MAILDIR before any write (positive twin: mbox writes) |
| MA-1087 | running: a live `lock` pid, a reused pid, a dead pid (not running), `parent.lock` held (Windows leg), a Thunderbird process; each refused with W-RUNNING and nothing written (`assure.Refused`) |
| MA-1088 | names: each hostile name (`..`, `CON`, trailing dot, leading dot, NUL, bidi override, `x.msf`, two names equal but for case) becomes a safe distinct file; every created path stays under the root |
| MA-1089 | mbox bytes: a test port of Thunderbird's read rules (separator, mboxrd unquote, LF/CRLF blank lines) recovers every original byte for byte, including `From `, `>From `, `>>From ` lines, a final line with no newline, CRLF and LF messages; the framing follows the message's line ending; extract's output unchanged |
| MA-1119 | layout: nested folders give `NAME` + `NAME.sbd`, empty parent files, no `.msf`, one store vs several, "(no folder)" |
| MA-1120 | collisions: an existing `NAME`, `NAME.sbd`, or a case variant gives `NAME (2)`; 99 taken → W-NAMES; nothing existing changed (directory listing and file hashes before and after) |
| MA-1122 | staging and commit: the hidden pair, the two no-replace renames, the undo when rename 2 meets a new name; fault injection at every write (the ceFS seam) leaves only the staging pair, which the next attempt removes by its record line and nothing else |
| MA-1123 | crash between the renames: the next attempt finishes rename 2 |
| MA-1124 | plan: counts, size, free space, W-SPACE, W-PST, W-NORAW, damaged and fixity-less records, the 2 GiB cap, Present=false count, All Mail note, W-RECAPTURE when the job's input lies in the profile |
| MA-1125 | re-check at start: Thunderbird started, Local Folders swapped for a link, name taken, manifest changed — each refused, nothing written |
| MA-1126 | path budget (Windows rule tested on every leg): shortening, then W-LONGPATH |
| MA-1127 | locks: the archive lock held for the run (a backup started meanwhile is refused), no verb for an encrypted archive; the profile lock taken and released, and stale after a kill |
| MA-1128 | encrypted: restored while unlocked through the lease; no plaintext file outside Local Folders (the cache, scratch and temp folders scanned before and after); locked before start refused; no folder names in `restores.log` |
| MA-1129 | CLI: exit 0, 3, 1, 130/143; `-plan` writes nothing; no terminal without `-yes` refused; `-list-profiles` |
| MA-1130 | Desktop routes: each through `d.api` (no token, wrong Origin, GET → refused); the start token one-use and kind-filtered; profile and folder ids checked against the server's lists |
| MA-1131 | Desktop flow: the words of §7.5; each step's `showStep` focus; progress in Activity; Cancel removes the staging pair; the result and Save this record |
| MA-1132 | elevated or root refused (W-ELEVATED) |

> Folded 2026-11-06 [RT-24, and the conditions named]: changes to the rows above, then new rows.
> - MA-1084 adds: an oversized `profiles.ini` refused for the restore (presync's cut unchanged), an
>   absolute `[Install…] Default=`, bidi characters stripped from `Name=`, and the Linux ownership
>   and mode checks [RT-14, RT-15].
> - MA-1085 adds: `\n`, `\r`, `\t`, `\'`, surrogate pairs, an unknown escape refused (W-PREFS),
>   `directory-rel` with `..`, `user.js` read after `prefs.js`, and a profile with no Local Folders
>   refused with W-NOLOCAL with `prefs.js` and `user.js` byte-identical before and after
>   [RT-15, lab finding 1].
> - MA-1086 adds: `tmp` alone, and `Inbox` or `Trash` as directories [RT-20].
> - MA-1089, labelled where it stands [RT-19]: it proves the writer against a port of Thunderbird's
>   read rules (a model, taken from the source), not against Thunderbird; MA-1093 is the row that
>   proves it against Thunderbird. "Byte for byte" there means the archive's preserved bytes.
> - MA-1087 adds: the Linux `/proc` probe leg; the macOS raw count; checks run before the lock and
>   only B after it [RT-2, RT-14].
> - MA-1088 adds: every name case of §4.4's note (".Inbox"/"Inbox."/"Inbox", "CON"/"_CON",
>   "."/".."/"untitled", COM0, LPT¹, CONIN$, "CON .txt", DEL, C1, U+200E, a leading `#`, a trailing
>   `~`, each named Thunderbird file), the byte and UTF-16 budgets with 120 CJK characters and 120
>   emoji, and a 10,001-folder archive refused with W-TOOMANY [RT-6, RT-7].
> - MA-1119 adds: the marker inside the container, 0o600, its line.
> - MA-1120 adds: `os.Root.Mkdir` and `O_EXCL` proven exclusive on Windows, including a case
>   variant (the Windows leg runs on the VM, RT-21) [RT-1, lab C].
> - MA-1122 is re-scoped to §5.4's folded sequence: fault injection (the ceFS seam) at every create,
>   write, fsync and placement; each leaves no parent file, and the next attempt's action is the one
>   §8's note gives for that state. MA-1123 is re-scoped to "crash after checked, before the parent
>   file: the next attempt re-checks and finishes; a failed re-check removes" [RT-1, RT-4].
> - MA-1124 adds: the deleted count on a local archive (none counted) and a Microsoft 365 archive;
>   W-RECAPTURE first; W-FAT32 [RT-9, RT-10, RT-20].
> - MA-1125 adds: the profile list rewritten between Plan and Start (refused) [RT-15].
> - MA-1126 adds: the exact 247/248 and 259/260 boundaries with `.msf` and working names [RT-6].
> - MA-1127 adds: the Unix lock is a POSIX record lock that a child process sees with `F_GETLK`, and
>   on Linux stays held after the process opens and closes another descriptor of `.parentlock`; no
>   `lock` symlink is made; each lock released on success, error, Cancel, Shut down and a recovered
>   panic, proven by a child taking it afterwards [RT-2].
> - MA-1128 adds: Start refused without `notEncryptedOK`; ask-twice for synced, removable and
>   network Local Folders, refused without `confirmAgain`; progress and Activity lines carry no
>   folder name; the Lock reply [RT-8, RT-18].
> - MA-1129 adds: `-not-encrypted-ok`; the typed-path question; refused without a terminal when the
>   destination may leave the computer [RT-8].
> - MA-1130 adds: the restore plan's own key (a pending copy plan survives a restore plan), its kind
>   discarded by a switch [RT-13].
> - MA-1131 adds: W-WAIT shown for the whole run; W-UNFINISHED [lab finding 3, RT-5].
>
> | Id | Proves |
> |---|---|
> | MA-1138 | backups skip a marked container whose ID is in `restores.log` and its parent file, and every `.mailarchive-part-` file; the run log line; positive twin: a marker with an unknown ID is backed up like any folder; unreadable log → only marked containers without a parent file skipped; O1's finished-folder skip and its one-condition alternative [RT-5, O1] |
> | MA-1139 | post-write check: a file truncated, removed, one byte changed, a message added or missing → W-CHECKFAIL, no parent file, tree removed; positive twin: an untouched tree passes and the parent file is made [RT-12] |
> | MA-1140 | the MailArchive restore lock: held → presync's `detectRunning` reports in use and starts nothing; a second restore gets W-RESTORING; presync's existing rows unchanged [RT-2, RT-17] |
> | MA-1141 | an unfinished restore shown at Desktop start and in `status` (W-UNFINISHED); `-plan` writes nothing [RT-5] |
> | MA-1142 | `restores.log`: torn last line tolerated, full log W-LOGFULL, unparsable W-LOGBAD, fsync before the first create, the line sequence of §7.4's note [RT-16, RT-4] |
> | MA-1143 | Windows legs: a `Packages\*Thunderbird*` folder gives W-STORE in the plan and the Done words; an identity-less volume gives W-NOIDENT; a remote drive gives W-NETWORK (run on the VM, RT-21) [RT-11] |

> Folded 2026-11-06 [foldcheck M1–M5, RT-13, N1, N3, N6, N8, N10]: changes to the rows above. No new
> ids.
> - **MA-1122** [M1, M5, N1]: fault injection also at every made-list append; there is no placement.
>   After a crash at each point, the made-list names every entry on disk that the restore made, and
>   the cleanup removes exactly those. Left, with W-LEFT and the marker and list kept: a foreign entry,
>   a link where a listed entry was, a kind that does not match, a listed directory not empty. Also: a
>   torn last list line; an unreadable list removes nothing; a container without a marker is left and
>   the next restore takes " (2)".
> - **MA-1123**, re-scoped again [M2]: a crash after "checked", before the parent file, is cleaned up
>   at the next Start and never finished; a parent file present with a "checked" line and no "taken"
>   line is recorded as ended with nothing removed; a parent file present with no "checked" line, or
>   with a "taken" line, leads to cleanup of the container, never to "ended", and the parent file is
>   left untouched; Check and `-plan` write nothing in every case (the file listing and the bytes of
>   `restores.log` the same before and after).
> - **MA-1126** [M5]: the working-name cases are dropped; the marker and the made-list are counted.
> - **MA-1088** [N6]: a name past ` (99)` gets the hash suffix and is cut again to fit.
> - **MA-1089** [N10]: tests the product reader in `internal/interchange`.
> - **MA-1124** [N8]: the deleted-after-backup line hidden when no record has a non-zero `LastSeen`,
>   shown when one has.
> - **MA-1130** [RT-13]: the cases listed in §7.3's foldcheck note.
> - **MA-1138** [M3, M5]: a skip set built before a restore starts, then that restore's marked
>   container met mid-walk: skipped after one re-read; an unknown ID with the log unchanged (a stat,
>   no re-read) walked as ordinary; the marker and the made-list never taken as mailboxes. The
>   `.mailarchive-part-` case is dropped.
> - **MA-1139** [M1]: after W-CHECKFAIL the cleanup goes by the made-list.
> - **MA-1140** [M4, N3]: `restores.log` and the restore lock in `job.Dir()` whatever `-config` says;
>   a `-config` Desktop's restore seen by a scheduled backup's skip and by presync;
>   `thunderbird.ProfileKey` the same for one profile spelled two ways.
> - **MA-1141** [M2]: W-UNFINISHED with its new words, shown only while the leftover still carries
>   its marker and had not committed; not shown for a committed restore missing only its "ended".

> Folded 2026-11-06 [foldcheck2 P1–P3, N-b, N-c, N-f, N-g, the residual case]: changes to the rows
> above. No new ids. Each refusal-to-remove case asserts, with `assure`, that the side effect is
> absent: the file listing and every file's bytes in the container and beside it are the same before
> and after.
> - **MA-1122** [P1, N-a, N-c]: the foldcheck bullet's "Left, with W-LEFT and the marker and list
>   kept: a foreign entry, a link …, a kind that does not match, a listed directory not empty" now
>   means **nothing removed** in each, the scan running before any removal (prove-fail: a cleanup
>   that removes listed entries first must go red). Adds: a Thunderbird-style `.msf` inside the
>   container → nothing removed; an entry added between the scan and the removals (fault seam) →
>   removal stops, "abandoned"; an entry named `Restored — NAME` (a file, a folder, a link) beside
>   the container on every live cleanup path (Cancel, failure, W-CHECKFAIL) → nothing removed, W-VISIBLE
>   or W-TAKEN words; fault injection at the parent file's fsync: re-check passes → "ended" with the
>   error named, re-check fails → "abandoned", nothing removed, W-COMMITERR; positive twin for each:
>   the same state without the foreign entry is cleaned and logged "cleaned".
> - **MA-1123** [P1, N-f, N-g, residual case]: the foldcheck bullet's "a parent file present with no
>   'checked' line, or with a 'taken' line, leads to cleanup of the container" is superseded by
>   [foldcheck2 P1]: it leads to **nothing removed**, "abandoned" and W-VISIBLE (both forms). Adds:
>   case 2 with a *folder* (and a link) named `Restored — NAME` → case 3a, never "ended"; a crash
>   between the failed create and "taken" with a regular file there → "ended", nothing removed;
>   the recorded Local Folders path or identity different, or its drive missing → no line, nothing
>   removed, W-ELSEWHERE, and the restore stays open for the next Start; a blocked removal, an
>   abandon and an elsewhere at Start each followed by the new restore running to "ended" under its
>   " (n)" name, with the earlier restore named after the result; every row of §8's foldcheck2
>   table, each removing only what that table says.
> - **MA-1138** [P3, residual case]: a marked container with an entry of its parent name beside it
>   is skipped (with that entry) only when the entry is a regular file and the ID has "checked" and no
>   "taken"; with no "checked", with "taken", or with a folder of that name, the container and the
>   entry are backed up as ordinary mail; the refresh function carries the flag; an unreadable log
>   skips only containers with no entry of the parent name; the residual state's parent file is
>   skipped (pinning the cost the design states).
> - **MA-1139** [P1]: "tree removed" after W-CHECKFAIL holds when the cleanup's scan is clean; with a
>   foreign entry the W-CHECKFAIL result gives W-LEFT's words instead and nothing is removed.
> - **MA-1140** [N-b]: uninstall's targets include `restores.log` and a `restore-` + 16 hex +
>   `.lock` file in `job.Dir()`, and not `restore-x.lock`, `restore-<16 hex>.lock.bak` or a directory
>   of that name; the lock-name constants match `tbrestore`'s by behaviour (a lock `tbrestore` makes is
>   a target).
> - **MA-1141** [P2]: W-UNFINISHED shown only with no closing line, a reachable Local Folders, the
>   marker present and no entry of the parent name; not shown after "abandoned"; an abandoned
>   leftover still there with no entry of the parent name shown with W-LEFT's, the made-list-unreadable
>   or W-COMMITERR's words; nothing shown when an entry of the parent name is there; uninstall's plan
>   names exactly the restores Desktop start names; every check a read (the bytes of `restores.log`
>   and the listing unchanged).

> Folded 2026-11-06 [foldcheck3 S2, S3] (lead's call): changes to the rows above. No new ids.
> - **MA-1123** [S2, S3]: "every row of §8's foldcheck2 table" means that table as split by §8's
>   foldcheck3 note: a crash after "checked" and then a regular file `Restored — NAME` made by
>   another program → "ended", nothing removed (the residual cost pinned, as MA-1138 pins it for
>   backups); the same with a folder or a link → 3a. Adds: a "taken" line in `restores.log` with no
>   entry of the parent name beside the container now → 3a, "abandoned", nothing removed (prove-fail:
>   a settle that looks only at the parent name now must go red), and the same with the entry there.
> - **MA-1122** [S3]: a live W-TAKEN whose parent entry has gone again by the cleanup's step 1 →
>   nothing removed (the "taken" line was fsynced first).

### 12.3 Lab rows (real Thunderbird; all pending until run)

The lab: Windows on the Parallels VM "Windows 11" (ARM64, Thunderbird installed and seen at
version 153.6.0esr in the lab of 2026-10-31, `internal/thunderbird/thunderbird.go:49-52`), macOS
on this Mac, Linux in a desktop VM (a headless Docker container cannot show Thunderbird's
window; a Thunderbird run with `--headless` can still build summaries, which the row reads).
Windows first.

| Id | Platform | What is checked |
|---|---|---|
| MA-1090 | Windows | Thunderbird started during a restore says the profile is in use; after the restore it opens normally and shows the folder (§5.2) |
| MA-1091 | Windows, then macOS | Thunderbird ignores dot-named entries in Local Folders; what it shows for a `NAME.sbd` with no `NAME` file (§5.4) |
| MA-1092 | Windows | a deep tree under a long user name opens to its deepest folder (§4.5) |
| MA-1093 | Windows | a restored fixture (nested folders, CRLF and LF messages, `From ` and `>From ` lines, a non-English folder name, a Gmail two-label message, a Microsoft 365 message): every folder shows with its count; each message saved from Thunderbird as `.eml` matches the original's SHA-256; summaries were built; read state as §4.6 says |
| MA-1094 | any | Thunderbird 115 ESR shows the ">From" lines as §4.1 says, and W-OLDTB is shown |
| MA-1095 | Windows | reserved endings (§4.4) and a profile whose `directory` and `directory-rel` disagree (§3.2) |
| MA-1096 | any | a maildir profile is refused with W-MAILDIR, and the setting names are as §3.3 says |
| MA-1097 | Windows | where a Microsoft Store Thunderbird keeps its profiles (§3.1) |
| MA-1098 | macOS | MA-1093 on macOS |
| MA-1099 | Linux | MA-1093 on Linux, with the plain, Snap and Flatpak profile locations |
| MA-1121 | Windows | a folder over 4 GiB on NTFS opens; Defender real-time scanning on during a 2 GiB restore (the retry of §8) |

> Folded 2026-11-06 [RT-3, RT-21, RT-24, lab findings 1–3]: what the 2026-11-06 Windows spike
> settled, what changes, and new rows.
> - **Ran (hand-placed files, holder `powershell.exe`; not yet with `mailarchive.exe`):** MA-1090's
>   lock behaviour (lab B), MA-1091's Windows half (lab A3, A4), and the discovery part of MA-1093
>   (lab A1, A2, A5). Each row still runs in full with MailArchive's own output in slice R8.
> - **MA-1093, method changed** (lab finding 2, lead's call): Thunderbird 153.6.0's Save As drops the
>   first header line of every local-folder message, even one Thunderbird wrote itself, and rewrites
>   line endings to CRLF, so **Save As is never the check**. The row checks instead: (1) the restored
>   mbox's own bytes, re-parsed, against the archive's originals (the post-write check's comparison,
>   run by hand); and (2) "Thunderbird read it faithfully" by having Thunderbird copy each message to
>   another local folder (lab: a delete into Trash wrote exactly the message's bytes after its own
>   separator line) and comparing the copy's bytes, after its separator, with the original. It also
>   records the profile's growth after indexing (§6.3 note), and a placed message with the deleted
>   bit (§4.6 note).
> - **MA-1091** becomes: Windows (done for hand-placed entries), macOS and Linux, each with a
>   Thunderbird 115 ESR and the current release; plus a dot-named file inside a `.sbd`, and what
>   happens to the marker when the person renames or moves the restored folder in Thunderbird.
> - **MA-1097** adds a redirected (network) AppData.
> - **MA-1099** adds: a Flatpak Thunderbird's lock read correctly after that Thunderbird crashes (its
>   pid is from another pid namespace — review side finding C).
> - **Every lab row records the Thunderbird version it ran** [RT-21]. Re-check trigger, written into
>   the catalog rows in R0: each new Thunderbird ESR, or any Thunderbird change to its mbox store,
>   folder discovery or profile lock, re-runs MA-1090, MA-1091, MA-1093 and MA-1134 on Windows first.
> - **Windows-only unit legs** (the `parent.lock` hold, the exclusive-create proofs in MA-1120, the
>   Windows leg of MA-1087, MA-1143) are not gated by CI, whose test gate is Linux-only; slice R8
>   runs them on the Windows VM and the release notes say so [RT-21].
>
> | Id | Platform | What is checked |
> |---|---|---|
> | MA-1133 | Windows first | the exact Thunderbird 153 menu path that makes Local Folders on a profile with no account, and whether setting up an IMAP account makes it; the path goes into W-NOLOCAL (lab finding 1) |
> | MA-1134 | macOS, Linux | a Thunderbird started while the restore holds its POSIX lock on `.parentlock` does not open the profile, and says what; after release, and after the holder is killed, it opens normally (RT-2; not yet run) |
> | MA-1135 | Windows, macOS, Linux | MailArchive killed mid-write (Task Manager; `kill -9`), then Thunderbird started: it opens normally and shows nothing of the restore; a non-crash failure while Desktop keeps running, then Thunderbird started: it opens normally (RT-24) |
> | MA-1136 | Windows | Defender real-time scanning on and OneDrive syncing Local Folders during the file placements and the parent-file create: the retry, W-CHECKFAIL when a file is taken, and a clean result otherwise (RT-24) |
> | MA-1137 | Windows first | a scheduled backup with pre-sync fires during a restore: pre-sync does not start Thunderbird on that profile, and the backup skips quietly (RT-24, S71) |

> Folded 2026-11-06 [foldcheck M5, N4, N9]: MA-1136's "file placements" are **superseded by
> [foldcheck M5]**; it runs Defender and OneDrive during the file creates, the post-write check's
> reopen, the parent-file create and a cleanup. MA-1134 runs before slice R3 (lead's call, N9), and
> MA-1133 before slice R7 (N4); §13's foldcheck note says how.

> Folded 2026-11-06 [foldcheck2 P1] (lead's call): **P1's assumption becomes a lab check**, added to
> **MA-1091**'s Windows leg, so it sits in the existing Windows lab list (R8). With Thunderbird
> closed: place a `Restored — X.sbd` tree with the marker and made-list, *without* its parent file;
> then make the empty parent file `Restored — X` with another program (PowerShell `New-Item`, as lab
> B used PowerShell); start Thunderbird. Record (1) whether it shows the tree inside `Restored — X`
> (the premise of W-VISIBLE's and W-TAKEN's "shows"); (2) whether a message moved into a shown
> subfolder is appended to that subfolder's mbox file (the hazard P1 guards against); (3) which
> `.msf` files it makes, inside the container and beside it (the scan's usual sign). Also with a
> *folder* named `Restored — X` instead of a file: what Thunderbird shows. Nothing in the cleanup
> rule rests on the answer (removing nothing is safe either way); only the words do. So it runs
> with MA-1133, any time after R0 and before slice R7, and if (1) is "no", W-VISIBLE and W-TAKEN say
> "may show" (§7.5 foldcheck2 note).

> Folded 2026-11-06 [foldcheck3 S3] (lead's call): the note above's "Nothing in the cleanup rule
> rests on the answer (removing nothing is safe either way); only the words do" is superseded by
> [foldcheck3 S3]. With a "taken" line now always meaning remove nothing (§8 foldcheck3 note), one
> path is left where a parent name another program made has since gone with no "taken" line (a crash
> before the commit, §8's table, row 5). There the cleanup's only backstop is the scan's sign that
> Thunderbird has shown the tree: the `.msf` files it makes inside it. So reading (3) of this step
> (which `.msf` files Thunderbird makes, inside the container and beside it) is load-bearing: if
> Thunderbird made none inside the container, that path goes to the R3–R5 adversarial pass as an
> open hazard before slice R4 is built. Readings (1) and (2) still decide only the words.

## 13. Build slices, and the gates each owes

Each slice is small, test-first, with prove-fail → prove-pass recorded in its commit, and runs
the unfiltered `go test ./...` before it is pushed.

| Slice | What | Gate owed |
|---|---|---|
| R0 | Catalog first: R40, S66–S70, the rows of §12 as pending encoders | none beyond this design's 10-lens review, which comes BEFORE R0 |
| R1 | Read side: the shared "faithful bytes or skip reason" step (extract unchanged), restorable counting, the tree from any loaded manifest | — (no new surface) |
| R2 | Thunderbird side: shared profiles.ini parser with names (presync moved onto it), prefs.js decoding, Local Folders and store kind, version, running detection incl. Linux, the profile lock (Windows first) | **adversarial pass** (it writes Thunderbird's lock files) |
| R3 | The writer: names, path budget, staging, mbox with matching line endings, no-replace commit, retries, cleanup, `restores.log` | **adversarial pass** (R2 and R3 may share one pass) |
| R4 | `tbrestore.Plan`/`Run`, the encrypted adapter, the `restore-thunderbird` verb, X1's line | adversarial pass on the encrypted path and the verb |
| R5 | Desktop: routes, token, panel, steps, progress, Cancel, result, help-page text | **friction review**; the UX contract (X-series) walk |
| R6 | Lab: Windows rows first (MA-1090..MA-1093, MA-1095, MA-1097, MA-1121), then macOS (MA-1091, MA-1098), Linux (MA-1099), MA-1094, MA-1096; release notes | owner sign-off to release |

Build order is sound: nothing on a surface (R4, R5) before its engine (R1–R3).

> Folded 2026-11-06 [RT-3, RT-21, RT-23, and the slices the folds add]. The slices above are
> re-numbered to absorb the folds; this list replaces the table. Every slice, as before: small,
> test-first, prove-fail → prove-pass recorded in its commit, the unfiltered `go test ./...` before
> it is pushed. The RT-3 gate ran before R0 (lab note, 2026-11-06). Catalog ids stay provisional
> until R0 writes them.
> - **R0 — Catalog first.** R40 (folded wording), R41, S66–S74, and every row of §12 (MA-1084..1099,
>   MA-1119..1143) as pending encoders, lab rows marked pending with their lab story. Gate: this
>   fold re-verified (each HIGH and MED condition answered) and the owner's answers to §15 written
>   in, before R0 starts.
> - **R1 — Read side.** The shared "faithful bytes or skip reason" step (extract's output unchanged;
>   MA-181..MA-185 green), restorable counting, the tree from any loaded manifest, the deleted count
>   by the workspace's rule (RT-9), the restore's own folder caps (RT-7). Gate: none beyond the
>   slice discipline (no new surface).
> - **R2 — Shared `profiles.ini` parser, presync moved onto it, as its own commit** (RT-23): presync's
>   existing rows unchanged and green before and after; presync does not use the new outputs. Gate:
>   none beyond that, since presync's behaviour must not change (a changed presync row is a stop).
> - **R3 — Thunderbird side.** The restore's use of the parser (names, default, refusal over the
>   cap, list binding), prefs.js and user.js decoding, Local Folders (incl. none, W-NOLOCAL), store
>   kind, version, Store-install and network detection, running detection (Linux probe, macOS raw
>   count), Thunderbird's profile lock per platform (after reading `nsProfileLock.cpp`), the
>   MailArchive restore lock and presync honouring it. Gate: **adversarial pass, shared with R4 and
>   R5 — the profile/lock/writer pass** (it writes Thunderbird's lock files and changes shipped
>   presync).
> - **R4 — The writer.** `SafeName` and the budgets, the marker, exclusive `Mkdir`, working files and
>   placement with retry, the fsync order, the post-write check, the parent file last, cleanup rules,
>   `restores.log` on the delete log's writer, the crash states of §8's note. Gate: **the same
>   profile/lock/writer adversarial pass.**
> - **R5 — Backup side.** The folder-scan skip (marker + working files [superseded by [foldcheck2
>   N-d]: marker and made-list; there are no working files, foldcheck M5], skip set from `restores.log`,
>   the unreadable-log fallback), O1's finished-folder skip as the owner rules, the backup run-log
>   line, W-UNFINISHED at Desktop start and in `status`. Gate: **the same profile/lock/writer
>   adversarial pass** (it changes what a backup reads); and the owner's O1 answer before the
>   finished-folder part is merged.
> - **R6 — Engine, encrypted path and command line.** `tbrestore.Plan`/`Run`, the encrypted adapter
>   on the read lease (not `ExtractSealed`), the server-checked tick and ask-twice, counts-only
>   progress, the `restore-thunderbird` verb and its flags, the archive-lock verb in `holderWords`,
>   X1's line. Gate: **adversarial pass on the encrypted path and the command line.**
> - **R7 — Desktop.** Routes (own plan key, list binding), token, panel, steps, progress, W-WAIT,
>   Cancel, result, Save this record, help-page text. Gate: **friction review of the Desktop flow,
>   Windows first** (Edge on the Windows VM, then macOS, then Linux), and the UX contract (X-series)
>   walk.
> - **R8 — Lab and release.** Windows rows first (MA-1090, MA-1091, MA-1092, MA-1093, MA-1095,
>   MA-1097, MA-1121, MA-1133, MA-1135, MA-1136, MA-1137), with the Windows-only unit legs run on
>   the VM (RT-21); then macOS (MA-1091, MA-1098, MA-1134, MA-1135); Linux (MA-1099, MA-1134,
>   MA-1135); MA-1094, MA-1096. Each row records its Thunderbird version. Release notes say which
>   rows ran where. Gate: **owner sign-off to release** (Q9's recommended answer: 1.6.0, after the
>   Windows lab rows pass; a patch number instead changes only the release notes and the tag).
> - Build order: no surface (R6, R7) before its engine (R1–R5); the backup skip (R5) ships in the
>   same release as the writer, so no release can write a restore that its own backups would copy.
>
> Folded 2026-11-06 [foldcheck N9, N4, M5, M1, M3, M4, RT-13]:
> - **MA-1134 runs before slice R3** (lead's call, N9), not at R8: the macOS leg on this Mac, and the
>   Linux leg in a desktop VM (or Thunderbird `--headless` in the Docker lab, if that shows the
>   profile-in-use refusal; the row records which). The holder is a small script taking the same
>   POSIX lock on `.parentlock`, as lab B used PowerShell. R3's Unix lock code is not written until
>   it has run; if it fails, §5.2 note item 2's fallback goes into this design first. R8 re-runs it
>   with `mailarchive` as the holder, as for MA-1090, so R8's lists keep it for that re-run only.
> - **MA-1133 runs before slice R7** (folding agent's answer, N4), so R7's friction review sees
>   W-NOLOCAL with its real menu path. It needs only the Windows VM and Thunderbird, so it can run
>   any time after R0.
> - **R4** [M5, M1]: "working files and placement with retry" is superseded by [foldcheck M5]. R4
>   builds instead: the made-list, every entry by exclusive create under its final name, the retry on
>   the check's reopen and on cleanup removals, the "taken" line, and the single cleanup.
> - **R5** [M3, M4]: adds the re-read of `restores.log` for an unknown marker, and every caller's skip
>   set from `tbrestore.LogPath()` in `job.Dir()`.
> - **R7** [RT-13]: the restore plan's own key and kind, ended at a switch beside delete plans, kept
>   across a settings save and a Lock.
>
> Folded 2026-11-06 [foldcheck2 P1–P3, N-a..N-g] (lead's calls):
> - **R0** writes R40 and R41 in their foldcheck2 wording (§11 foldcheck2 note) and the changed
>   MA-1122, MA-1123, MA-1138, MA-1139, MA-1140 and MA-1141 rows. Gate before R0: a re-check of
>   foldcheck2's P1–P3 and these notes only.
> - **R4** builds the cleanup in the decision order of §8's foldcheck2 note (parent name, list, scan,
>   then removal), the run's own "abandoned", the N-c re-check, step 0's Local Folders comparison and
>   N-g's "Start always goes on". W-VISIBLE, W-COMMITERR and W-ELSEWHERE are its words. Its gate
>   stays the profile/lock/writer adversarial pass, whose scope adds the scan and the hand-edit case
>   (§8, §9 foldcheck2 notes).
> - **R5** builds P3's committed flag in the skip set and its refresh, and P2's display rule for
>   Desktop start, `status` and uninstall's plan. Uninstall's `restores.log` name and `restore-*.lock`
>   pattern (N-b) go in R5 too, beside the rest of the settings-file work.
> - **P1's lab check** (MA-1091's new Windows step, §12.3 foldcheck2 note) runs with MA-1133, after R0
>   and before R7, on the Windows VM.
>
> Folded 2026-11-06 [foldcheck3 S1–S10] (lead's calls): **R0** writes R40 with the foldcheck3 cleanup
> clause (§11 foldcheck3 note) and MA-1122 and MA-1123 as §12.2's foldcheck3 note changes them; S1,
> S2 and S3 are folded in the design at R0. The other notes are owed to their slices, unfolded until
> then: S4, S5, S6, S8 and S9 to **R4** (the writer's words and the scan's name match); S7 and S10 to
> **R5** (the display rule and the Local Folders read it reuses). **R4** builds step 0b (a "taken"
> line → remove nothing) in the cleanup's decision order.

> Built 2026-11-07, slice R3 (the builder's notes; where a call was the builder's, it says so):
> - **Where it lives.** `internal/tbrestore` holds the Thunderbird side and imports nothing of
>   `internal/app`, so the backup skip (R5, in `internal/app`) can import it. `ProfileKey`,
>   `RestoreLockName`, `IsRestoreLockName` and `RestoreLockPath` are in `internal/thunderbird`, so
>   presync, which `tbrestore` does not import, uses the same names. R6's `Plan`/`Run`, which read
>   an archive through `internal/app`, therefore cannot live in `tbrestore` itself: R6 places them
>   (in `internal/app`, or a package above both) and says so.
> - **`directory-rel` with `..`.** The catalog row MA-1085 (R0) says "`directory-rel` with `..`
>   refused"; §3.2's RT-15 note (and the review's RT-15) say `..` is allowed, resolved as Mozilla
>   resolves it, and then checked. The note wins (it is the later, reviewed text): built as allowed
>   and checked, and refused (W-OVERLAP) when it lands on another account's folder; the catalog row
>   is amended beside its text to say so.
> - **Builder's answers** where the design is silent, each the refusing direction: a Local Folders
>   server with neither `directory-rel` nor `directory` is W-NOLOCAL; a store contract id that is
>   neither mbox nor maildir is W-MAILDIR; any unreadable setting of the Local Folders account
>   refuses (W-PREFS naming the file that set it), even one the lookup would not use; a Local Folders
>   that is missing or not a folder has words of its own, W-LOCALGONE ("Thunderbird's Local Folders
>   for this profile (PATH) is not there or is not a folder, so nothing was restored. Open
>   Thunderbird once with this profile and close it, then click Check again."); a `-profile FOLDER`
>   that is not a profile is W-NOTPROFILE; on a Unix other than macOS and Linux there is no process
>   probe, so Thunderbird counts as running there.
> - **W-NOLOCAL's MENU PATH** stays a literal placeholder in the code until lab row MA-1133 names the
>   path that worked, so no slice can ship a guessed path (§3.2 step 1 note).

> Built 2026-11-07, slice R4 (the builder's notes):
> - **Thunderbird's skip rules, from its source.** `nsMsgLocalStoreUtils::nsShouldIgnoreFile`
>   (comm-central `mailnews/local/src/nsMsgLocalStoreUtils.cpp`, read 2026-11-07 on the
>   releases-comm-central mirror, not run) skips: a leading "." or "#", a trailing "~", the exact
>   names msgFilterRules.dat, rules.dat, filterlog.html, junklog.html, rulesbackup.dat, popstate.dat,
>   sort.dat, mailfilt.log, filters.js, feeds.json (and .tmp, .backup, .corrupt), feeditems.json (the
>   same three), feeds.rdf, feeditems.rdf (case-blind), the endings .snm, .toc, .mozmsgs, .sbd, .msf,
>   the prefix feeditems_error, and hidden or special files. `SafeName` uses exactly this list,
>   case-blind (the safe direction); §4.4's provisional .dat, .json, .html and .htm endings are not in
>   the source and are dropped. Lab row MA-1095 still tries each.
> - **The writer** is `tbrestore.Write` over an `fsys` seam on the `*os.Root` (the ceFS seam's
>   shape), `PlanLayout`/`FinalNames`/`SafeName`/`FreeTopName` for every name in the plan, the
>   cleanup `cleanupContainer` in the decision order, `Settle` (and its read-only prediction for
>   Check and `-plan`), and `restores.log` (`tbrestore.Log`) on `util.AppendLogLine`, the deletion
>   log's append discipline lifted out of `archdelete` (delete's rows green before and after). The
>   post-write check streams each file through `interchange.ScanMboxrd` (the read-rule port beside
>   `WriteMessageMatched`), so a folder of any size is checked without holding it in memory.
> - **A final line with no newline.** The writer adds the message's own line ending to it (as
>   Thunderbird's writer does) and records how many bytes it added; the post-write check takes exactly
>   those off before comparing, so the check holds the archive's original byte for byte.
> - **A torn marker** (a crash after the marker's create, before its line) cannot prove the container
>   MailArchive's, so Start settles it as case 1 and leaves it (N1); the next restore takes " (2)".
> - **Words, the builder's:** W-LONGPATH for a Local Folders so deep that even the container's own
>   entries do not fit ("Thunderbird's Local Folders (PATH) is so deep in its drive that Windows cannot
>   hold a restored folder under it, so nothing was restored. Choose a profile whose Local Folders is
>   nearer the top of its drive."); "1 entry" in W-LEFT when there is one; a blocked removal's words
>   name the file (not for an encrypted archive) and end "MailArchive tries again at your next
>   restore."; the plan's prediction for a clean leftover: "An earlier restore into this profile did
>   not finish (DATE). Its partial folder, which Thunderbird does not show, will be removed first."

> Built 2026-11-07, slice R5 (the builder's notes):
> - **The skip** is in `internal/source` (`restoreskip.go`, `WalkOptions.RestoreSkip` and
>   `RestoreSkipped`): before the mbox walk enters a `.sbd` container (through a folder's entry or the
>   loop for containers with none), and before it reads a folder file whose container is a finished
>   restore's, it reads the marker (a regular file, at most 4 KiB, through the walk's root) and
>   applies §7.2's foldcheck2 rule. The record it asks is `tbrestore.SkipSet` (built from
>   `tbrestore.LogPath()` once per run by `internal/app`'s `localRun`, so every backup — scheduled,
>   Desktop's own, the command line — uses the one record; `internal/source` reads no settings), whose
>   `Lookup(id, profileKey)` re-reads the log once for an unknown ID unless its size and modification
>   time are unchanged (M3). A restore is this record's only when the marker's profile key matches its
>   started line's (a marker copied into another profile does nothing). Owner question O1's
>   recommended answer (b) is what is built (the finished-folder skip); the design's rule that it is
>   not merged before the owner's O1 answer stands.
> - **The run log line**, the builder's words (it covers the finished folders O1 (b) leaves out
>   too): "Left out N folder(s) that MailArchive is restoring, or has restored, into Thunderbird (not
>   backed up; not a fault)." No alert, streak or fault.
> - **Where it is shown:** Desktop start renders `tbrestore.Leftovers` in the page header beside the
>   other notices (read at each page load); `status` prints each as "Restore to Thunderbird: …" after
>   its report, whatever archive it reports on (the JSON document is unchanged); uninstall's plan
>   (`uninstall.Report.RestoreLeftovers`, the command line and Desktop's Uninstall panel) names them.
> - **Uninstall's targets:** `restores.log` (an exact name beside `deletions.log`) and the lock files
>   by `thunderbird.IsRestoreLockName` — the very function that names them, so the two cannot drift.
>
> Settled 2026-11-07 [ADV-06, ADV-07] (the R3–R5 adversarial pass): in the first bullet, "A restore is
> this record's only when the marker's profile key matches its started line's (a marker copied into
> another profile does nothing)" was not what the code did — the walk compared the marker with the
> record, both the restore's own, and nothing with the folder being backed up. It is now: the record
> also requires the folder the walk met the container in to be, by identity, the Local Folders the
> started line records (§7.2 ADV-06 note). And O1 (b)'s skip, and the leftover skip, never apply to a
> restore closed "abandoned" (§7.2 ADV-07 note, the lead's call).

> Built 2026-11-07, slice R6 (the builder's notes; where a call was the builder's, it says so, each the
> refusing direction):
> - **Where it lives.** The engine is a package of its own, `internal/tbengine`, above `internal/app`
>   and `internal/tbrestore` (tbrestore cannot import app; the R3 note above). `tbengine.MakePlan`
>   (named so beside the type `Plan`) writes nothing and settles earlier restores read-only;
>   `tbengine.Run` takes the archive lock (verb `restore-thunderbird`, none for an encrypted archive)
>   → the MailArchive restore lock → the §5.3 re-check → Thunderbird's lock (`TakeProfileLock`, check
>   B again) → `OpenLocalRoot` → `Settle` → `Write`, released in reverse by deferred releases on every
>   path. It does not recover a panic (Desktop's job runner does); the releases unwind with it. The
>   command line (`cmd/mailarchive/restoretb.go`) and Desktop (R7) call it. `ArchiveInputs` reads
>   every job file of `job.Dir()` whose `out` is the archive, for W-RECAPTURE and the default profile.
> - **The encrypted side** is `keyring.LeaseOriginals`: a read lease taken at Start, read through
>   `app.FaithfulOriginal` with that lease as its `SealedOriginals` (never `ExtractSealed`), so a Lock
>   after Start does not stop it (Q6). The plan loads the sealed manifest through a lease of its own,
>   destroyed at once. The §5.3 manifest pin of an encrypted archive is its record pointer
>   (`.mailarchive-manifest.ptr`), which every backup rewrites.
> - **No folder names, encrypted** (§6.2): the writer (`tbrestore.Write`) now says only an error's kind
>   for an encrypted archive, in `restores.log`'s detail and in the words ("the disk is full",
>   "stopped", else "a file error, not named …"), since an error's text can name a restored file; a
>   change to slice R4's writer, found by MA-1128's check-failure case.
> - **The lock's verb.** `lockfile`'s holder-verb cap rose from 16 to 24 characters, so
>   `restore-thunderbird` (19) is kept whole and `holderWords` names it; an older program reading the
>   line cuts it and says "another MailArchive task", the safe direction.
> - **Ask twice** uses the copy's test, lifted as `app.(*CopyExportEnv).LeaveReasons` (reason codes;
>   foldcheck N12), each worded by its caller; the copy's rows unchanged. A missing second yes is
>   **W-NOTAGAIN** (the builder's words: "Thunderbird's Local Folders may leave this computer
>   (REASONS), and the restored mail will not be encrypted. That was not confirmed a second time, so
>   nothing was restored. …").
> - **The plan's order** (§6.4 as folded): W-RECAPTURE, then W-STORE, then the destination, what goes
>   back, what cannot (W-PST, W-NORAW and too-big counts, W-LONGPATH, records with no fixity, "found
>   while restoring", changed names), earlier restores' predictions (W-UNFINISHED's "will be removed
>   first", W-VISIBLE, W-LEFT, W-ELSEWHERE + "Your restore will go ahead."), the space line,
>   "Thunderbird is closed.", W-NETWORK, the not-encrypted line and ask-twice reasons (encrypted) or
>   W-CLOUD (unencrypted), W-OLDTB, the All Mail note, the deleted-after-backup line, the
>   scheduled-backup line, the check, and W-WAIT last. The builder's reading of the two "first"s
>   (RT-10's W-RECAPTURE "above every other line", RT-11's W-STORE "first warning"): W-RECAPTURE leads
>   every plan it is in; W-STORE is the first line otherwise, else the second.
> - **Refusals the design names without words, or not at all** (the builder's): W-BUSY is refused at
>   the plan too, not only at Start; a free space that cannot be read is W-SPACE ("MailArchive could
>   not read how much free space DRIVE has (WHY) …"); W-FAT32 refuses the plan (the person chooses
>   fewer folders); nothing restorable is W-PST when every record is an Outlook one, else W-NORAW;
>   root on macOS and Linux is W-ELEVATED with its own words ("running as root … without sudo");
>   Windows' elevation probe fails closed; a context ended before Run takes its locks is **W-STOPPED**
>   ("The restore was stopped before it began, so nothing was written."); a Local Folders inside an
>   encrypted archive is **W-INSIDEENC**; `-profile-name` naming none or several is **W-NOPROFNAME** /
>   **W-PROFNAMES**; `-folder` naming none, or one in two stores without `-store`, is **W-NOFOLDER** /
>   **W-TWOSTORES**; several usable profiles with no default is **W-CHOOSE** (not reachable with
>   today's default order, kept for safety).
> - **The deleted-after-backup line**, when the archive keeps the timeline and none was deleted, says
>   "None of these messages were deleted from the mailbox after they were backed up." (a true line,
>   not a "0").
> - **Done words** add, after the writer's: the chosen messages that did not go back (originals found
>   damaged named by their archive file, none named for an encrypted archive), §4.6's count of messages
>   Thunderbird had marked deleted (X-Mozilla-Status 0x0008), the Microsoft 365 unread hint when a
>   store is a mailbox, W-STORE when it applied, then each earlier restore not cleaned up (N-g; an
>   elsewhere one adds "Your new restore went ahead."). Exit 3 counts every chosen record that did not
>   go back, whatever the reason.
> - **The command line.** Without a terminal and without `-plan` or `-yes` it is refused before
>   anything is read. At a terminal, one question serves as the confirmation and, for an encrypted
>   archive, the tick ("The restored mail will not be encrypted. Restore it? [y/N]"); `-yes` at a
>   terminal skips it, so an encrypted archive then needs `-not-encrypted-ok` too (else W-TICK).
>   `-not-encrypted-ok` on a plain archive is refused. The typed-back path must equal the resolved
>   Local Folders path the plan showed. `-record-out` saves the plan (with `-plan`) or the plan and the
>   result as a NEW file only; a record that cannot be saved after a restore is a warning on stderr,
>   the exit code the restore's. `-list-profiles` with no profile is W-NOPROF (exit 1). Progress is said
>   on stderr at most every 3 seconds, and at the last message.
> - **Pending to R7:** the Lock reply while a restore runs (§6.2 note, `copyGoesOnText`'s pattern) is
>   Desktop's; MA-1128's encoder is narrowed to it.

> Built 2026-11-07, slice R7 (the builder's notes; where a call was the builder's, it says so; the
> lead's calls of 2026-11-07, "ok to your picks", are followed as written):
> - **Where it lives.** `internal/desktop/restoretb.go` (the four routes, the plan's pending kind, the
>   job, the Activity view, the record) and `restoretbscript.go` (the page, the last part of
>   `dashboard.js`). The card is its own, **Restore to Thunderbird**, drawn right after Maintenance on
>   the current archive's page (§7.3's "beside Maintenance"; the builder's reading: a card of its own,
>   so `showStep`'s one-step-said-at-a-time rule holds within it); a delete of the page's own archive
>   greys it with the archive's other cards. Its button is disabled with its reason beside it on a
>   locked encrypted archive that this computer does not remember the key of. "Nothing restorable"
>   (W-PST / W-NORAW) is said at Check, in place, not beside the button: knowing it means judging every
>   record, which the page load does not do (the builder's call).
> - **The routes** are §7.3's, through `d.api`, with bodies capped at 64 KiB as delete's. `/profiles`
>   answers the server's list (`tbrestore.ReadProfiles` over the bases, its ID, each profile by
>   position, with its name, folder, why it cannot be used, and whether the archive backs it up) and
>   the pre-selected one (`ProfileList.Default` with the archive's sources, §3.1 step 4). `/folders`
>   and `/plan` take `{listID, position}`; `/folders` re-resolves the binding and answers the tree with
>   restorable counts (`tbengine.Folders`, on `app`'s new `(*ArchiveTree).RestorableCounts`, which
>   judges records as `SelectForRestore` does); `/plan` checks every folder id against the archive's
>   own tree before `MakePlan`. The profile bases are a Desktop seam (`restoreTBBases`), so tests use
>   fake profiles in temp folders only.
> - **The start.** `tbengine` gains `(*Plan).Confirmed(Start)`, the first check `Run` makes (W-TICK,
>   W-NOTAGAIN); Desktop calls it before it takes the token, so a missing tick or second yes keeps the
>   plan and the reply names the box (`need`), and `Run` checks again. The run is
>   `runCancellableJobOnClaiming` (Cancel and Shut down through its context); Shut down's drain waits
>   for it to end. `Run` is never handed a raw error to show: a non-refusal error is fixed words, a
>   panic is recovered inside the job into fixed words (INS-07).
> - **Lock.** A restore plan holds the registry's open of an encrypted archive by reference, never a
>   lease of its own, so a Lock leaves the plan and Start is refused W-LOCKED by the engine (RT-13); a
>   remembered key opens the archive at the routes as a job would (O3). While a restore runs, a Lock
>   answers `restoreGoesOnText` (§6.2 note's words), beside the copy's.
> - **Progress.** Activity's log has the progress line at most every 3 seconds and at the last
>   message, the folder named only for a plain archive, and the CLI's "Checking what was written…"
>   line when `Start.Checking` fires; `/api/activity` adds a `restore` view for the slot's job:
>   W-WAIT (`tbrestore.WaitWords`, which names Windows' Cancel) for the whole run, the last line, the
>   check, then the result (`Result.Words`, done or stopped) and the record. Cancel the restore is the
>   panel's own button on `/api/job/cancel`, which now says a restore's own line in Activity.
> - **Save this record** saves, through the browser as Delete's record does, the plan's text and the
>   result's words with the date, computer, account and version; for an encrypted archive the panel
>   says beside the button that the record names the restored folders and is not encrypted.
> - **Words, the builder's:** the step lines (what is needed next), the button's reason on a locked
>   archive, "That restore is no longer planned…", a folder no longer in the tree, no folder ticked,
>   the fixed words for an error that is not a refusal (which point to `restore-thunderbird -plan` at
>   a terminal for the reason), the crash words, and Cancel's Activity line.
> - **Still pending:** W-NOLOCAL's MENU PATH stays the literal placeholder until lab row MA-1133 runs
>   (§13 foldcheck note; the R3 note). Gate owed: the friction review of this Desktop flow, Windows
>   first on Edge, then macOS, then Linux, and the X-series walk; and R6's pass asked that R7's pass
>   include INS-07, the Lock reply and `FS.Private` on Windows with a second account and on exFAT.

## 14. Decisions this design makes, for the review to test

1. mboxrd, unchanged writer, framing line endings matching the message (§4.1–4.3).
2. Version 1 refuses maildir (§3.3).
3. Hold Thunderbird's own profile lock while writing (§5.2) — the one write outside Local
   Folders.
4. Hidden staging pair inside Local Folders, made visible by two no-replace renames (§5.4).
5. Folder names from `Folder` (latest folder), not from the stored file path (§3.4).
6. Hold the archive lock for the whole run, like extract (§7.1).
7. Warn, do not prevent, the re-capture of restored mail (§7.2).
8. Refuse when any Thunderbird runs, not only on this profile (§5.1).

> Folded 2026-11-06 [RT-1, RT-2, RT-10, RT-5, RT-12], as the review tested them:
> - Decision 3 stands on Windows (lab B) and changes on macOS and Linux: Thunderbird's POSIX record
>   lock only, no symlink, checks before the take, plus a MailArchive restore lock (§5.2 note);
>   lead's call 2026-11-06.
> - Decision 4 is replaced: no staging pair and no rename; the final `.sbd` made exclusively first,
>   hidden working files placed one by one, the empty parent file made exclusively last (§5.4
>   note); lead's call 2026-11-06.
> - Decision 7 is replaced: backups leave out MailArchive's own restore leftovers (lead's call), and
>   with O1's recommended answer the finished restored folders too (§7.2 note).
> - New decision 9: a full re-read check of every written mbox before the parent file is made (§5.4
>   note, step 6); lead's call 2026-11-06.
> - New decision 10: the default profile prefers one the archive does not back up, and W-RECAPTURE
>   leads the plan (§3.1 step 4 note).
>
> Folded 2026-11-06 [foldcheck M5, M1, M2] (lead's calls):
> - Decision 4's "hidden working files placed one by one" is **superseded by [foldcheck M5]**: every
>   entry is created under its final name by exclusive create, after being written into the
>   made-list (§5.4 foldcheck note).
> - New decision 11 [M2]: an unfinished restore is never finished by a later attempt. It is cleaned
>   up at the next Start, by the made-list, and the person restores again (§8 foldcheck note).
>
> Folded 2026-11-06 [foldcheck2 P1, P3] (lead's calls):
> - Decision 11 is narrowed by [foldcheck2 P1]: an unfinished restore is cleaned up only when no
>   entry of its parent name is beside it and nothing unlisted is in it. Otherwise it is left
>   whole, recorded "abandoned", and the person is told where it is and whether Thunderbird shows it.
> - New decision 12 [P1]: when Thunderbird could have shown the tree, or anything not MailArchive's
>   is in it, MailArchive removes nothing, in every cleanup, including a live W-TAKEN. It accepts a
>   restored tree left visible inside another program's folder over any chance of deleting mail
>   that is not its own (§8 foldcheck2 note).
> - New decision 13 [P3]: a backup skips a restored folder that has a parent file only under Start's
>   committed rule; otherwise that folder is the person's and is backed up (§7.2 foldcheck2 note).

## 15. Owner questions (product choices only), each with the lead's recommendation

- **Q1 — The new folder's name.** Recommended: "Restored — ARCHIVE NAME", and " (2)", " (3)"
  when that is taken, no date in the name (the date is in the record). It is the name you asked
  for, and it stays short in Thunderbird's folder list.
- **Q2 — Read or unread.** Recommended: leave each message as its own bytes say (mail from
  Thunderbird keeps its read marks; mail from Microsoft 365 shows unread) and tell the person how
  to mark a folder read. Marking all read would mean adding a header to every message.
- **Q3 — Mail deleted from the mailbox after it was backed up.** Recommended: restore it too, and
  say how many. Getting such mail back is often why someone restores.
- **Q4 — Gmail's All Mail.** Recommended: keep it ticked like every other folder and show the
  note about doubles, so nothing is left out without the person seeing it.
- **Q5 — Thunderbird profiles that keep one file per message (maildir).** Recommended: refuse in
  this version with plain words; build it only if a tester has such a profile.
- **Q6 — Locking the archive during a restore.** Recommended: a restore that has started runs to
  its end even if the archive is locked, as the unencrypted copy does; Shut down stops it and
  nothing is added.
- **Q7 — Restoring into the profile the archive backs up.** Recommended: warn in the plan (the
  restored mail would be backed up again) and do nothing more in this version.
- **Q8 — Encrypted archives.** Recommended: allowed while unlocked, with a required tick "I
  understand the restored mail will not be encrypted".
- **Q9 — Release.** Recommended: its own version, 1.6.0, after the Windows lab rows pass.

> Folded 2026-11-06 [owner questions; review §5.1]. Still OPEN — not decided by this fold. The
> owner has the lead's recommendations. For each, what is built if the owner agrees, and the small
> change if not:
> - **Q1 folder name**: built as recommended ("Restored — ARCHIVE NAME", then " (2)"). Alternative
>   (a date in the name): one format string and the name budget (§3.5 note).
> - **Q2 read state**: built as recommended (bytes as kept; the Mark Folder Read hint). Alternative
>   (mark all read): NOT small — it adds a header to every message and breaks the post-write check's
>   byte comparison; it would need its own design note (§4.6 note).
> - **Q3 deleted after backup**: built as recommended (restored and counted, by the workspace's rule,
>   §6.4 note). Alternative (leave it out): a filter on that same rule.
> - **Q4 Gmail All Mail**: built as recommended (ticked, with the note). Alternative (unticked by
>   default): one default in the tree's initial selection (§6.5 note).
> - **Q5 maildir**: built as recommended (refused, W-MAILDIR). Alternative (a maildir writer): a new
>   slice with its own adversarial pass and lab rows — not small, which is why refusing is
>   recommended (§3.3 note).
> - **Q6 Lock mid-restore**: built as recommended (the restore goes on; Shut down stops it; the Lock
>   reply says so). Alternative (Lock stops it): the progress loop checks the lock state between
>   messages and ends as Cancel does (§6.2 note).
> - **Q7** is replaced by **O1 — backups skip restored folders**: built as the review's
>   recommendation (b), backups leave out the finished restored folders too. Alternative (a), warn
>   only: one condition in the skip, and W-RECAPTURE keeps its original words, first in the plan
>   (§7.2 note). Either way, MailArchive's own unfinished leftovers are always skipped (lead's call,
>   RT-5).
> - **Q8 / O2 — encrypted archives**: built as recommended: allowed while unlocked, with the
>   server-checked tick; when the Local Folders is synced, removable or on a network share, asked a
>   second time, checked on the server (§6.2 note). Alternatives: refuse encrypted archives (one
>   refusal in the plan), or refuse such destinations (W-LEAVES instead of ask-twice).
> - **Q9 release**: built toward 1.6.0 after the Windows lab rows (R8). Alternative: a patch number;
>   only the notes and tag change.
> - **O3** (Store Thunderbird) does not arise: the lead chose warn, not refuse (§3.1 note).

## 16. Side findings (existing code, outside this design's scope)

1. `extract -format mbox` writes LF framing lines around CRLF messages (`mbox.go:52`, `:79`), so
   its files mix line endings. Harmless for Thunderbird 122+ by its source; other readers
   unknown.
2. A message Thunderbird had deleted but not compacted away seems to be archived like any other:
   the reader reads only the read bit of `X-Mozilla-Status` (`internal/source/mbox.go:280-293`), and
   no other place reads it for the deleted bit (searched `internal/source`). Not checked with a
   real file.
3. Backups' automatic discovery does not search the Flatpak Thunderbird location
   (`internal/app/run.go:1865-1870`).
4. The archive's mbox reader normalises every line ending to CRLF and removes one `>` from
   `>From ` lines (go-mbox `reader.go:59-69`), so a "preserved original" from an mbox source is not
   the mbox's exact bytes. This design keeps what the archive kept; whether that matters for
   fidelity claims elsewhere is for the owner of those claims.

> Folded 2026-11-06 [review §6]: the review adds four side findings, recorded here and not acted on
> by this design: (A) go-mbox removes only one `>` and only from `>From `, so a Thunderbird 122+
> mbox line `>From ` is archived as `>>From ` — every export carries it, restore included (extends
> finding 4); (B) SanitizeSegment can merge names, so every caller relying on "two names never
> merge" should be checked, backups' folder paths included (`localwalk.go:86-88`); (C) a Flatpak or
> Snap Thunderbird's lock pid may be from another pid namespace (lab row MA-1099 checks it); (D)
> `run.go:1860-1881` reads the `APPDATA` environment variable while presync uses the known-folder
> lookup. Lab side observations, 2026-11-06: Thunderbird 153's Save As drops a message's first
> header line (a Thunderbird behaviour, not MailArchive's); and Thunderbird 153 still discovers
> folders by scanning files (no new folder database was seen).
