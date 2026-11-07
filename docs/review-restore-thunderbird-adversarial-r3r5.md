# Adversarial pass: Restore to Thunderbird, slices R3, R4 and R5

**Verdict: NOT CLEAN.** Seven confirmed findings, all LOW: each needs either another
program writing inside the restore's hidden folder at the wrong instant, an unusual spelling
or hard link of a profile, a crash that leaves zero bytes in the log, or an archive folder
named like a Windows short name. None was found that a normal person, with Thunderbird and
MailArchive behaving as designed, would hit. Four attacks were refuted, one race is
described but not reproduced.

## Header

- Reviewed: branch `restore-thunderbird` at `4c93e06` (R3 `d7c8012`, R4 `a9ec850`, R5
  `4c93e06`). Report written 2026-11-07.
- File blobs at that commit: `tbrestore/write.go` ba4b55a, `cleanup.go` 1e4b58d,
  `tblock.go` aac8626, `tblock_unix.go` b56a31f, `rlog.go` 0e7ad77, `skipset.go` f8673d6,
  `settle.go` 201f601, `marker.go` ce2c158, `source/restoreskip.go` f536f73,
  `util/appendlog.go` ee358e3. A finding is stale when its cited file changes.
- Who: one agent (Claude, Opus 5.5) acted as finder and skeptic. The process asks for at
  least three separate finders and a separate skeptic; that fan-out was not done. To make up
  for it partly, every finding below is a reproduction, not a reading: a Go test probe run
  against the real code, and anything not reproduced is labelled so.
- Order of reading: the sources first (`internal/tbrestore`, the writer, the backup skip,
  uninstall, `util.AppendLogLine`, presync's restore-lock check), then the design
  (`docs/design-restore-thunderbird.md` §5.2, §5.4, §7.2, §8, §9, §11), then the builder's
  notes (§13 "Built" notes). The builder's attack list was read with the task, before the
  sources.
- Probes: Go tests in a throwaway clone, `.claude/attack-restore-r3r5` (not committed):
  `internal/tbrestore/zz_attack_test.go`, `zz_attack_unix_test.go`,
  `zz_attack_darwin_test.go`, `zz_attack_windows_test.go`,
  `internal/source/zz_attack_test.go`. Fake profiles in temp folders only; no real profile
  touched. The real worktree was not modified except for this file.
- Commands and results:
  - macOS: `go -C <clone> test -count=1 -v -run Attack ./internal/tbrestore/ ./internal/source/`
    (go1.26.0 darwin/arm64) — exit 1 (the confirmed probes fail by design).
  - Windows 11 VM (amd64 binary, ARM64 emulated, NTFS temp folder, 8.3 names on):
    `zsh .claude/attack-restore-r3r5-win.sh <clone> .claude/attack-r3r5-out/win internal/tbrestore internal/source`
    (the repo's `winrun-at.sh` with `-test.run=Attack -test.v`).
  - Linux: `docker run golang:1.26 … go test -count=1 -v -run Attack …` — exit 1.
- Count: 12 claims tested; **7 confirmed, 4 refuted, 1 not reproduced**.

## Findings, ranked

| ID | Sev | What breaks | Probe, result per platform | Where |
|---|---|---|---|---|
| ADV-01 | LOW | Thunderbird can open the profile mid-write (goal c) | P1: mac CONFIRMED (both forms); Linux not affected (OFD lock); Windows not affected | `tblock.go:47-60`, `tblock_unix.go:99-125` |
| ADV-02 | LOW | Cleanup deletes the person's mail outside the container (goal a) | P2: mac CONFIRMED, Linux CONFIRMED; Windows junction REFUTED, Windows symlink not run (no privilege) | `cleanup.go:90-103` |
| ADV-03 | LOW | Cleanup removes an entry the restore did not create (goal a, R40 wording) | P3: CONFIRMED on mac, Linux, Windows | `write.go:203-206`, `:289-292`; `cleanup.go:90-103` |
| ADV-04 | LOW | A crash leaves restores.log unreadable for good (goal f) | P8: CONFIRMED on mac, Linux, Windows | `rlog.go:121-131`, `:141-144` |
| ADV-05 | LOW | A plan that says OK fails at Start on Windows (robustness) | P5: Windows CONFIRMED | `names.go:170-206` |
| ADV-06 | LOW | A copied profile's restored folder, and mail filed into it, is not backed up (goal d) | P6: CONFIRMED on mac, Linux, Windows | `restoreskip.go:78`, `skipset.go:86` |
| ADV-07 | LOW | The person's mail in a kept leftover is not backed up (goal d) | P7: CONFIRMED on mac, Linux, Windows | `restoreskip.go:82-83` |

### ADV-01 — The "only one opener" guard is keyed by the path's spelling, not the file

**Scenario.** On macOS the restore holds a classic POSIX lock on `.parentlock`, and closing
*any* other handle to that file in the same process silently drops it (the lead's
sole-opener rule). The guard is a registry keyed by `thunderbird.ProfileKey(profile)`, a hash
of the resolved, case-folded path. Two spellings that are the same folder but resolve
differently get different keys, so a probe in the same process misses the registry, opens
`.parentlock`, closes it, and the lock is gone:
- the macOS "firmlink" spelling `/System/Volumes/Data/...` of a path under `/Users` or
  `/private/var` (the profile list would need two entries for one folder, one spelled that
  way — Thunderbird does not write that, so this is a configuration oddity, not an attack);
- another profile folder whose `.parentlock` is a hard link to this one's.

**Probe P1** (`zz_attack_darwin_test.go`): take the lock; a child process standing in for
Thunderbird is refused; call `CheckRunning(alias)` once in-process; the child then **takes
the lock**. Both forms CONFIRMED on macOS. Linux uses the per-handle (OFD) lock, which
another handle's close does not drop; Windows uses a no-sharing handle — neither affected.

**Fix.** Key the registry by the lock file's identity (device and inode from `Lstat` of
`.parentlock`), recorded in `openProfileLock`; `parentLockProbe` and `probeLockFile` look up
by that identity before any open, and answer from the registry on a match. P1 becomes the
tombstone test.

### ADV-02 — Swapping a scanned folder for a link between the scan and the removal

**Scenario.** The cleanup scans the container (step 3), then removes listed entries by path
(step 4, `cleanup.go:90-103`). Each removal `Lstat`s only the last name; `os.Root` follows a
symlink in the middle of a path as long as it stays inside Local Folders. If, after the scan,
the listed folder `A.sbd` is replaced by a symlink `..`, then the listed file `A.sbd/B`
resolves to `Local Folders/B` — the person's own folder file — which is a regular file, so it
is removed. This disproves §9's note "a planted entry can only make the cleanup remove less,
never more" under a race. It needs a program of the same user writing in the hidden folder
during the cleanup (Thunderbird is held out then), so it is LOW.

**Probe P2** (`zz_attack_test.go`): a restore crashed before its commit; the person has
`Local Folders/B`; a spy file system makes the swap at the first removal-phase `Lstat`.
macOS and Linux: **`Local Folders/B` removed**, CONFIRMED. Windows with a junction: `os.Root`
will not traverse it, removal stops, B kept — REFUTED. Windows with a symlink: not run (the
VM user cannot make symlinks).

**Fix.** In step 3 record each scanned entry's identity (`os.FileInfo` per relative path).
In step 4, before removing an entry, `Lstat` it and its parent folder and require
`os.SameFile` with what the scan recorded; any difference stops removal (W-LEFT's step-4
form). A microsecond window between that `Lstat` and the removal remains; say so in §9.

### ADV-03 — A listed name whose exclusive create failed is still removed

**Scenario.** The made-list line is fsynced *before* the create (the M1 rule). If the create
then fails because something already made that name, the line stays, the run goes to the one
cleanup, the scan sees an entry of the listed name and kind, and removes it — though
MailArchive did not create it. R40 says the cleanup "removes only the entries its made-list
names"; that holds, but the list then names an entry the restore never made. Needs another
program writing inside the new hidden folder within milliseconds; LOW.

**Probe P3**: a fault hook makes `<container>/B` just before the writer's create of it. The
write fails `file exists`, cleanup kind `CleanRemoved`, and **the other program's file is
gone**. CONFIRMED on all three.

**Fix.** The writer keeps the paths whose `Mkdir`/`Create` failed and passes them to its own
cleanup, which treats each as not the list's (remove nothing: W-LEFT). The crash-between-line-
and-create case cannot be told apart later; leave it and name it in §8.

### ADV-04 — Zero bytes left by a power cut make restores.log unreadable

**Scenario.** A crash can leave a file longer than what reached the disk, the tail filled
with zero bytes (NTFS and XFS can; APFS was not checked). `Log.Read` passes over a cut-short
line only when JSON says "unexpected end of input" (`rlog.go:141-144`); a line ending in, or
made of, zero bytes is a different JSON error, so the whole log is W-LOGBAD. The next append
starts a new line after it, so it is now in the middle and never recovers by itself. Then
every restore is refused, and backups fall to the unreadable-log rule. The words tell the
person to move the file away, which also loses the record of committed restores.

**Probe P8**: a valid line, then `…"res` + four zero bytes, or zero bytes only. Read:
W-LOGBAD before and after the next append; the skip set reports unreadable. A plain cut
(no zero bytes) is passed over correctly. CONFIRMED on all three.

**Fix.** Treat a line holding a zero byte (after an optional cut JSON prefix, or zeros only)
as torn wherever it is, like a cut line. Check the deletion log's reader (`archdelete`), which
shares the append discipline; it was not probed here.

### ADV-05 — An archive folder named like a Windows short name fails the restore

**Scenario.** Windows gives a long name a short alias (`Long folder name one` →
`LONGFO~1`). An archive with sibling folders `Long folder name one` and `LONGFO~1` passes the
plan, but at Start the second exclusive create finds the alias of the first and fails. Nothing
of the person's is touched (cleanup removed only the restore's own files; Local Folders
unchanged), but the person is told "could not finish (… file exists)". The repo already handles
this for extract (`util.IsShortNameAlias`, XC13, `copyexport_names.go:81,197`); the restore's
names do not.

**Probe P5** (Windows; 8.3 names confirmed on): outcome `failed`, Local Folders unchanged.
CONFIRMED.

**Fix.** In `FinalNames`, give a name that `util.IsShortNameAlias` matches a suffix, as
copyexport does; add it to MA-1088's hostile names.

### ADV-06 — The profile check compares the marker with itself

**Scenario.** The walk calls `Lookup(m.ID, m.Profile)` with the profile key *written in the
marker* (`restoreskip.go:78`), and the log's started line holds the same key. Nothing compares
it with the profile being backed up. So a committed restore's folder copied with its marker
into another profile, or onto a USB disk the person backs up, is left out there too, with any
mail later filed into it. The builder's note (§13, "a marker copied into another profile does
nothing") and R41's "for that profile" are not what the code does.

**Probe P6**: a committed restore copied into a second Local Folders, a message filed into the
copy's top folder; that walk backs up **nothing** from it. CONFIRMED on all three.

**Fix.** Bind the skip to where the restore wrote: the skip set keeps each started line's
Local Folders identity; the walk passes the identity of the folder holding the container;
skip only on a match, else back it up as ordinary mail (the safe direction). Correct the §13
note.

### ADV-07 — A kept leftover that holds the person's mail is never backed up

**Scenario.** A leftover the cleanup kept because something not MailArchive's is inside
(W-LEFT: typically Thunderbird's `.msf` after it showed the tree) is "abandoned", and its
container has no parent name beside it, so §7.2 rule 1 leaves it out of every backup. But
that is exactly the leftover that can hold the person's mail: mail they moved into the
restored folder while Thunderbird showed it is appended to the listed files. The S5 words
promised "Until then MailArchive backs them up like your other mail"; once the parent name
goes, that stops. This is the design's own rule working as written; it is rare (row 5 of §8:
another program makes, then removes, the parent name).

**Probe P7**: a restore whose listed `Inbox` gained the person's message and an `Inbox.msf`,
parent gone, record "abandoned". The walk leaves it out; **the person's message is not backed
up**. CONFIRMED on all three.

**Fix (lead's call, not self-granted).** Recommended: leave out only restores with no closing
line (the half-written case RT-5 was for) and committed ones; back up "abandoned" containers
as ordinary mail. Cost: a cut-short last message of a failed restore may be copied into the
archive once.

## Refuted (each with the evidence)

| Claim | Evidence |
|---|---|
| A made-list path escapes the container by `..`, `/`, `\`, `C:`, `./`, `//` | P9b: the whole list is refused (CleanListBad), nothing removed — mac, Linux, Windows |
| Win32's trimming of trailing spaces and dots turns `.. `, `.. .`, `...`, `..` + ideographic space into `..` | P9: under `os.Root` none resolves upward; the person's file kept, the restore's own files removed — mac, Linux, **Windows** |
| A junction planted (no race) at a listed folder's name walks the cleanup into Local Folders | P11 (Windows): Go reports the junction as not a folder, `kindMatches("d")` false, W-LEFT, nothing of the person's removed |
| A junction swapped in between scan and removal (ADV-02 on Windows) | P2/junction: `os.Root` refuses to traverse it; removal stops (CleanLeftPart) |

## Not reproduced (mechanism in the source, timing not shown)

- **N1, the walk's marker check and its descent are not one step** (`localwalk.go:521-529`
  then `sbd`). A backup that checks a container in the instant after the restore's `Mkdir`
  and before its marker is written walks it as ordinary; if the restore then writes its first
  folder file before the walk lists the container (it must first fsync the marker, the
  made-list and the container), the walk can read a half-written file. Microseconds against
  milliseconds; not reproduced. Cheap narrowing: after listing a container, if the marker
  name is among its entries, run the skip rule again.

## What held

- **Only adds.** Every entry is made by exclusive create (`Root.Mkdir`, `O_CREATE|O_EXCL`),
  nothing existing is opened for writing, the commit is an exclusive create, and nothing after
  it removes anything (read in `write.go`, `fsys.go`).
- **Names.** Archive names are cleaned (`..`, devices, trailing dot or space, `.sbd`/`.msf`
  endings, leading `.`/`#`, bidi controls), deduplicated case-blind and NFC on every platform,
  and every write goes through `os.Root` on Local Folders; path budgets apply on Windows. JSON
  manifests cannot carry invalid UTF-8, so the macOS/Windows invalid-name failure is not
  reachable.
- **The cleanup's order.** The "taken" line, the parent-name check, the unreadable list, the
  whole-container scan (links never matched; NFC matched once), reverse removal, the list and
  marker removed last only from an empty container — all as §8 describes, on every crash point
  the existing MA-1122/1123 tests drive. The made-list's cut last line is passed over.
- **Locks.** Thunderbird's lock is opened without truncating or writing, with no link
  followed (`O_NOFOLLOW`, `FILE_FLAG_OPEN_REPARSE_POINT`); Linux's OFD lock survives other
  handles; the kernel releases every lock and handle when the process is killed, so nothing
  stale stops Thunderbird starting; the restore lock is `flock`, so an in-process `Held` probe
  cannot drop it; presync checks the restore lock first.
- **The post-write check.** Each file reopened by its final name (same-file checked), its
  size compared, and each message's length and SHA-256 compared after re-parsing, with the
  added line ending checked and removed; edge messages (empty, no final newline, CRLF, `From `
  and `>From ` lines) round-trip by reading the code. An antivirus that removes or changes a
  file before the commit fails the check.
- **The skip set.** An unknown marker costs one `stat` unless the log changed; a re-read
  happens only when its size or time changed, so planted markers cannot force a re-read storm.
- **Settling with a moved Local Folders.** When the recorded folder is not the profile's now
  (path or identity), nothing is removed.
- **Uninstall** removes only `restores.log` and names matching `IsRestoreLockName`, and names
  each leftover.

## Notes (not findings)

- **Out of scope here:** there is no production caller yet that runs restore lock → profile
  lock → settle → write → release (that is a later slice). The order and the release on every
  path are owed to that slice's adversarial pass.
- **External and network drives:** macOS and Linux identity is device + inode; a drive
  plugged back in, or a network share mounted again, can get a new device number. A leftover
  there is then "elsewhere" for good: nothing removed (safe), but Desktop start does not name
  it and the words say the folder "cannot be reached". Worth a lab row.
- **Uninstall while a terminal restore runs** deletes the restore lock's file; presync then no
  longer sees it, but Thunderbird's own lock still keeps Thunderbird out. No harm found.
- **Side finding, outside R3–R5:** presync's macOS lock check reads only the `lock` symlink,
  which Thunderbird on macOS does not make (as `tbrestore/running.go`'s comment records), so on
  macOS presync relies on its process check alone.

## Gates

The fixes above are not made here (this pass writes docs only). Each fix needs its probe as the
tombstone test, shown red then green, and a fresh look at the patch. ADV-07 is a product rule
change and needs the lead's (or owner's) ruling before it is built.

## Settled 2026-11-07

Each finding's probe became a real test with a `covers:` marker in the worktree (no `zz_` files),
each fix proven by a mutation that turned its test red, then reverted. The design's dated
"Settled 2026-11-07 [ADV-0n]" notes stand beside the text they amend; the catalog rows carry
"Amended 2026-11-07" notes. Fix blobs (a later change to one of these files makes the line stale):
`write.go` e2aa332, `cleanup.go` d81ac3b, `fsys.go` c08314a, `fsys_unix.go` d56258f, `fsys_windows.go`
a66d9ab, `tblock.go` 5e3fc43, `tblock_unix.go` 1169876, `tblock_windows.go` debd364, `rlog.go`
f4f8b5e, `names.go` b0d01a4, `skipset.go` a897970, `source/restoreskip.go` b753283.

| ID | Settled by | Test (row) | Mutation that turned it red | Design note |
|---|---|---|---|---|
| ADV-01 | Registry keyed by the lock file's identity (device+inode; Windows volume+file index, read with attribute-only access); a probe or open that lands on the held file by a race keeps its descriptor until Release | `tbrestore/ma1127_adv01_identity_test.go` (MA-1127): hard link everywhere, `/System/Volumes/Data` on macOS; macOS and Windows green (Windows: hard link; firmlink skipped off macOS) | M1 registry answering only for the spelling it was taken under (3 second handles opened); M1b race guard off (probe answers not-self and drops the lock) | §5.2, after the Mac-lab note |
| ADV-02 | Scan records what it saw; `FS.RemoveSeen` walks by handle with no link followed (`openat` `O_NOFOLLOW`; `NtCreateFile` + `FILE_OPEN_REPARSE_POINT`), compares identity and kind at every level, removes relative to the checked parent (`unlinkat`; Windows by handle, POSIX-semantics disposition) | `tbrestore/ma1122_adv02_swap_test.go` (MA-1122): swap at the Lstat (P2), at the removal call, and after the removal's check (`beforeUnlinkSeam`); macOS green; Windows junction cases green, symlink cases skipped (no privilege) | M2 removal by path through the root (person's file removed); M2b the walk without `O_NOFOLLOW` and the identity compare (removed) | §8 cleanup step 6 note; §9 ADV-02 note (the window left: `fstatat`→`unlinkat`, inside the container only, Unix) |
| ADV-03 | Writer keeps names whose exclusive create met `ErrExist`; its cleanup's scan never takes them for the list's (W-LEFT, remove nothing) | `tbrestore/ma1122_adv03_notmine_test.go` (MA-1122): file at a listed file's name, folder at a listed folder's name; macOS and Windows green | M3 the writer not keeping the names (the other program's entry removed) | §8 cleanup step 6 note (the crash-between-line-and-create case left, named) |
| ADV-04 | `Log.Read` reads a zero-holding line as what precedes its trailing zeros: torn, or a whole line | `tbrestore/ma1142_adv04_zeros_test.go` (MA-1142); macOS and Windows green | M4 the zero rule off (W-LOGBAD) | §7.4 ADV-04 note (archdelete's reader checked: passes over any unparsable line) |
| ADV-05 | `FinalNames` gives an 8.3-alias-shaped name (`util.IsShortNameAlias`) the hash suffix, counted as changed; the dedupe's hash suffix too. **Rename, not refusal** — the builder's reading of the lead's "refuse at plan time with words" (see the §4.4 note; a refusal is one condition) | `tbrestore/ma1088_adv05_shortname_test.go`, `ma1088_adv05_shortname_windows_test.go` (MA-1088); macOS green; Windows VM (8.3 on) both siblings restored | M5 alias-shaped names kept | §4.4 ADV-05 note |
| ADV-06 | `SkipSet.Lookup(id, profileKey, holder)`: known only when the folder the walk met the container in is, by identity, the started line's Local Folders | `source/ma1138_adv06_adv07_test.go`, `tbrestore/ma1138_skipset_test.go` (MA-1138); macOS and Windows green | M6 the Local Folders not compared (copy left out) | §7.2 ADV-06 note; §9 ADV-06 note; §13 R5 note corrected |
| ADV-07 | Lead's call: a restore closed "abandoned" is never left out; leftovers with no parent left out only while open or committed; committed excludes "abandoned" | the same two files (MA-1138); macOS and Windows green | M7 every parentless leftover left out; M7b committed ignoring "abandoned" | §7.2 ADV-07 note; §11 R41 note; catalog R41 amended |

Not reproduced N1 (the walk's marker check and its descent not one step) is unchanged and still
open. Refuted items need nothing. Platforms: macOS (darwin/arm64) package runs and the full suite,
Windows 11 VM (`internal/tbrestore` PASS; `internal/source` only its known baseline
`TestFrozenReplayPinsTheCharsetTableAndItsSources`, no `go` on the VM), Linux in Docker — as the
commit records. These fixes have not had their own fresh adversarial look; R6's pass should include
`RemoveSeen` on Windows with a privileged symlink (not run: the VM user cannot make symlinks).
