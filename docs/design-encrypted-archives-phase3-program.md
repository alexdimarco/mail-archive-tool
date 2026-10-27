# Design — the program copies kept in an encrypted archive (Phase 3, P3-5)

**Status:** revision 3, 2026-10-27 — the conditions of its pre-code review (C1–C19) and of
condition 20's targeted re-review of §4 and §5 (R1–R22, §11) folded in; both reviews are filed in
docs/review-encrypted-archives-phase3-program-predesign.md (GO_WITH_CONDITIONS each). Where §§1–9
and §11 differ, **§11 governs**. Not built, except PG5 (MA-827).
**Parent:** docs/design-encrypted-archives-phase3.md §13.2 (settled points, requirements 1–12),
§13.1 (the note, built as MA-826), §14 (leak budget); owner rulings O1 (all four platforms,
downloaded and checked; offline, this computer's own program) and O7 (published releases become
unchangeable).
**Why:** an encrypted archive opens only with MailArchive. Years from now the program may be hard
to find. A copy of it kept in the archive folder — the break glass — lets the person open their
mail with what is on the drive, under the conditions stated beside each claim below.

## 1. What is kept

A folder `program/` at the archive's root holds the release copies of one MailArchive version and,
when needed, self-copies:

| File | What it is | Size (v1.4) |
|---|---|---|
| `MailArchive-macos.zip` | the macOS app (universal) | 34.5 MB |
| `mailarchive-windows-amd64.zip` | the Windows programs | 35.6 MB |
| `mailarchive-linux-amd64` | the Linux program (x86-64) | 33.0 MB |
| `mailarchive-linux-arm64` | the Linux program (ARM) | 31.1 MB |
| `SHA256SUMS-<version>` | each kept version's own checksum file, byte for byte (R16) | <1 KB |

Release copies are the release's assets, byte-identical, under the same names (about 134 MB, O1).

**A self-copy** is this computer's own program, written when §4.2 says so: on macOS the running
`.app` bundle zipped as `mailarchive-self-darwin-universal.zip` when the program runs from inside
one, else the bare program; on Windows the console `mailarchive.exe` found beside the running
program and checked to be the same build (`SiblingQualifies`, `SameBuild`; C17 — when it does not
qualify, no self-copy is written and the run warns with the program note's words); on Linux the
bare program. A bare self-copy is written with mode 0755 (C8). Self-copies are named
`mailarchive-self-<os>-<arch>[.exe|.zip]`, sealed `origin: self`, and marked "cannot be checked"
in the note.

**The program's own temporary files** — `program/.mailarchive-program-<n>.tmp`, the staging names
of §4.3 — exist only while a run writes; anything else in `program/` is not MailArchive's (C7).

## 2. What the record seals

The sealed header gains a program set, **its own sub-object with its own `format` number**,
decoded leniently while the rest of the header stays strict, never tied to the manifest's version
(C3):

```
program: {
  format:  1
  keep:    bool          // the person's choice; absent set = not chosen (the card offers it, ticked, O1)
  version: "v1.5.0"      // the high-water mark: the highest release version ever sealed here (C4)
  state:   "intent"|"landed"
  files:   [{ name, sha256, size, origin: "release"|"self", version, platform, reads }]
  incoming:[ same shape ]   // during intent only: what is about to replace or join (C7)
  note:    { current, incoming }  // the note digests, during intent (C7)
}
```

- **`reads`** is the record-format revision the copy's program can open: a program-side constant,
  bumped whenever an older program could no longer open what this one seals (C4). It needs no new
  header field.
- **Validated when read** (C2): names match §3 and are not temporary or reserved names; `version`
  and each file's `version` are an exact `vMAJOR.MINOR.PATCH` or empty; `origin`, `state`,
  `platform` are known values. A set that fails, or an unknown `format`, leaves `program/`
  untouched with one warning, and the backup goes ahead (C3). A pin test fixes the fields per
  format.

## 3. The keyless check (C9)

`container.CheckKeyless` walks `program/` with the same no-follow rules as the rest of the
archive and gains an informational field, `ProgramOdd` (a count, and up to ten names), that
**never changes the outcome**. It counts: a link, a folder, a special file, a name outside
`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$` or ending in `-attachments.zip`, a file over 256 MB. The
program's own temporary files and encmark's litter list (`.DS_Store`, `Thumbs.db`, …) are not
counted. Regular files matching the pattern are accepted without the key — a newer MailArchive may
keep others (forward compatibility). MA-743's grammar wording moves to its test row.

## 4. At each keyed backup (C10)

**One hook**, in `Run` (local and Microsoft 365, the first backup at creation included), after the
backup's final save and before its finalizer, **under the archive lock until the landed commit**,
with the lock re-checked before staging and before the renames (R2). Reindex and pause do not
run it. It **never fails the backup**:
its errors are warnings naming their class, cancelling it ends it with a warning and leaves the
backup's exit code unchanged, and a keyed run whose own executable lies inside this archive's
`program/` skips the step and says so (C15). Its outcome class is recorded in the last-run record
(§8's leak budget) and shown as one line in `status` and on the Encryption card; a file missing on
two runs in a row becomes a lasting "incomplete" line.

### 4.1 Confine (C1)

`program/` is checked without following links (the `realDir` pattern, §4.5's reparse-tag rule on
Windows). Not a real folder: the step is skipped with a warning and nothing is touched. Otherwise
an `os.Root` is opened on it, confirmed to be the same file as the check, and every list, read,
remove and rename goes through it. Entries are removed one at a time — never a whole tree — and
only regular files or links whose names match §3; folders and other entries are reported, never
deleted (the person may have unzipped a copy in place).

### 4.2 Judge, then decide per platform (C6, C14, C5)

**Judge** each entry against the sealed set with `judgeFile`'s shape: size first against the
sealed size, `OpenRegularFile`, `SameFile`, a bounded streaming hash — never a whole file in
memory (C14). An online-only placeholder is "not checked this run" and is not fetched. While
`state` is `intent`, a file matching either its current or its incoming entry is accepted silently;
the program's own temporary files are swept silently (C7). Otherwise:
- an entry with no sealed record: removed (if a file matching §3) or reported, with a warning;
- a sealed file whose bytes changed: written again from the cache, or removed with its entry kept
  marked missing, with a warning naming its platform;
- a sealed file missing: written again when the cache has it, else a warning naming its platform.

**Decide**, per platform, from what is present and verified on disk (C6):
- `keep` off: nothing — sealed files are removed; the folder only when empty.
- Release copies are written only from a verified cached version **at or above the high-water
  mark** (equal, for a repair). A development build writes no release copy and removes none.
- A platform the newer release does not publish keeps its older copy.
- **A self-copy is written for this computer's platform when that platform has no present,
  verified copy that opens the archive** — including when the kept copies' `reads` is below this
  program's (C5: "the kept copies cannot open this archive", warned on every run, in `status` and
  on the card; the note names the copies that open the archive). Another computer's self-copy
  stays until a release copy for its platform arrives (an upgrade: no warning).

### 4.3 Write: one staged protocol (C7)

For repairs, upgrades, self-copies and turning `keep` off alike:
1. **Stage** each incoming file as `program/.mailarchive-program-<n>.tmp`, synced and re-hashed
   against the verified cache (requirement 9). Skipped, with a warning, when the free space is
   below the total plus 256 MB.
2. **Render** the new note from the planned set.
3. **Intent commit:** the set with, per name, its current and incoming entry and the removals,
   and both note digests.
4. **Renames, single-entry removals, the note.**
5. **Landed commit:** the set as it now stands, and the note's digest.

A run stopped anywhere between 3 and 5 is judged by the intent at the next run (§4.2); never from a
cache lacking the expected version (requirement 3).

## 5. The cache and the download (C11, C12, C13)

**The cache:** `<user cache dir>/mailarchive/program-cache/<version>/`, outside the scratch folder,
local, with a closed shape: the four assets, `SHA256SUMS`, `verified`, the partial-download name,
the attempt record; beside the versions, `expects` and one per-user lock (C13). The lock
(`lockfile.HoldFile`) is held by the download, by copies from the cache and by pruning; uninstall
lists the cache (in PG2) and refuses while the lock is held. Each asset is synced before
`verified` is written atomically; a failed re-hash deletes the asset and `verified`. `expects`
(the newest version any archive opened here expects, learned at each keyed run, never in the
record) is written atomically; unreadable, nothing is pruned. Pruning keeps this computer's
version, the `expects` version, and any version used within 90 days.

**What counts as a release:** this program's build information names an exact `vMAJOR.MINOR.PATCH`
and `vcs.modified=false` (C12: the release build refuses unless every shipped binary says so).

**When it downloads** (C11): at the **beginning** of a keyed backup — the first one at creation
included — and only when `keep` is on and the decide step would want this version or the `expects`
version that the cache lacks. It is a separate, cancellable task outside any archive lock. The
program step waits for it **up to 10 minutes**, showing progress; the CLI process stays alive for
it; Ctrl-C or the Desktop's cancel skips the wait. When the wait runs out, the trigger "the
download did not finish within the wait" applies (a self-copy, §4.2). An attempt cut off by an
exit or a cancel resumes next time and does not count toward the once-a-day limit. Nothing is
fetched at Desktop start-up.

**How:**
- the built-in repository and fixed URLs:
  `https://github.com/alexdimarco/mail-archive-tool/releases/download/<version>/<asset>`;
- HTTPS only; redirects only to `github.com` and hosts under `.githubusercontent.com`, at most 5;
- a 200 MB ceiling per file; the proxy from the environment; resume with a range request;
- `SHA256SUMS` first, then each asset against it; a mismatch deletes the asset and the version is
  not `verified`; an asset the release does not have is "not published" (requirement 8).

**What it proves** — *conditional, stated here as everywhere it is claimed:* the checksums prove a
copy is the file the release published, not who built it, and only **for a release published after
GitHub's immutable releases were switched on and after PG5** (C12, O7). For an earlier release they
prove agreement with what the release held when it was downloaded.

**Self-copy triggers** (requirement 6; parent §14 gains the last two): no network; a download that
failed (refused redirect, too large, checksum mismatch, not published, server error); a development
build; the download did not finish within the wait; the kept copies cannot open this archive.

## 6. Front ends

- **Making an encrypted archive** (Desktop, CLI `-encrypt`): "Keep a copy of MailArchive for Mac,
  Windows and Linux in the archive so it can be opened years from now (about 134 MB each time a new
  version updates it)" — on by default.
- **The setting** (C16): the card's toggle is an immediate keyed commit under the archive lock, as
  the pause note is; refused with its reason when the archive is locked, its drive is missing or a
  run is under way; the card shows a pending removal until the next backup carries it out. The
  CLI's keyed backup takes `-program-copies=on|off`.
- **Scheduling from a kept copy is refused at the chokepoint** (C15): a typed refusal in
  `schedule.ProgramForSchedule`, like `ErrTranslocated`, when the program's path has an ancestor
  folder named `program` whose parent encmark judges an encrypted archive, or its name begins
  `mailarchive-self-`. Every caller (CLI, wizard, Repair, Desktop) is tested; a release program
  outside any archive still schedules. A copy moved elsewhere is not detected and needs no refusal:
  no refresh replaces it.
- **The note** (C8) is written from the sealed landed set: each copy by platform and version, no file
  names (MA-826); which copies can be checked, and against which release page; this computer's own
  copy marked "cannot be checked"; on every platform, copy the file out of the archive folder before
  opening or unzipping it; a bare self-copy's Terminal steps (copy to the home folder, `chmod +x`,
  run); Gatekeeper and SmartScreen; an offline paragraph — without the release page a copy cannot be
  checked, anyone who had the drive could have swapped it, and changing the password afterwards does
  not undo what a swapped copy took, because the master key is never re-keyed (parent §14); when
  `keep` is on and nothing has landed yet, it says so.
- **The keyed check** (C18): `verify -full` compares `program/` and the note against the sealed set,
  read-only, and reports a warning-class finding that never changes the mail verdict.

## 7. The release process — first (C12)

PG5 lands before PG2 and is a release gate:
- `--clobber` is removed; `release.yml` creates a **draft**, uploads every asset and `SHA256SUMS`,
  checks them, then publishes. A re-run against a published release fails.
- `build-release.sh` refuses unless every shipped binary (both Linux programs, both macOS slices,
  the Windows pair) has `Main.Version` equal to the tag and `vcs.modified=false` (closes SC33, with
  a catalog row).
- **No tag carrying PG2 ships until the operator has confirmed GitHub's immutable releases are on**,
  recorded in the release checklist (an owner step).

## 8. Leak budget and threats (adds to parent §14; C19)

- The copies reveal the version that last refreshed them; a self-copy also this computer's
  operating system and processor type. The last-run record names the program step's outcome class.
- **The download contacts GitHub once a day per version until verified, plus the newer version an
  archive here expects, through the environment's proxy**, sending this computer's address and the
  versions. (Parent §14's sentence is corrected to these words: an accuracy fix.)
- **A swapped copy** can steal a password from someone who runs it. *The check depends on reaching
  the release page*: offline, a copy cannot be told from a swapped one, and anyone who had the drive
  could have swapped it; changing the password afterwards does not undo it, because the master key
  is never re-keyed. The next keyed run replaces a changed copy and warns — after the fact.
- **A planted file** in `program/` is removed at the next keyed run, with a warning; a planted
  folder or link is reported, never followed or deleted (§4.1).
- **Network attacker:** HTTPS to GitHub's hosts only; the checksums come from the same release over
  the same channel, so they prove agreement with the release, under §5's condition.
- **Disk exhaustion:** the free-space margin, the per-file ceilings, the cache's pruning.

## 9. Slices and tests

- **PG5 first** (built, MA-827): the release process (§7).
- **PG1**: the sealed program set held as raw bytes — format, validation, pin test (§2, R6, R7);
  `Options.ProgramCopies` and its sealing (R21, R22); the keyless `ProgramOdd` (§3); the
  scheduling refusal (§6, C15).
- **PG2**: the cache and the fetcher (§5, R17–R19), against a local HTTPS test server through seams
  for the base URL, the trusted hosts, the cache root and this program's version (R22); uninstall's
  listing and lock.
- **PG3**: the keyed hook `programStep` at its two seams (R1) — confine, judge, decide, the staged
  write with intent and landed commits (§4, R2–R15) — the note renderer shared with `refreshNote`
  (R5), and the download's start-and-wait wiring (R17, R20); fault injection for each failure
  class, a stop between each pair of steps, a link or junction at and inside `program/`.
- **PG4**: the front ends, the setting's commit, the note from the landed set, `verify -full`'s
  comparison (§6). No tag carries PG3 without PG4 (R22).

Each slice test-first with its catalog rows; an adversarial pass after PG3 and after PG4.

## 10. The review's operability backlog

Three minors partly dissolved (DUR-06: comparing in the keyless check against the `SHA256SUMS` on
the drive; SEC-11: a GitHub host change beyond `.githubusercontent.com`; SEC-12: a refresh policy
for old copies, a product call accepted under O1) are kept here, with no code owed now.

## 11. The re-review's conditions (revision 3; these govern)

Condition 20's targeted re-review of §4 and §5 (lenses 3, 5, 6) confirmed 38 of 40 findings and dissolved every one into the conditions below (GO_WITH_CONDITIONS). They are part of the design: where they differ from §§1–9, they govern.

- **R1.** (RINT-01, RDUR-14). Write one shared function, programStep, and call it at two seams: Run after guardLateIndexWrite (run.go about line 829) and RunGraph after guardLateIndexWrite (graph.go about line 553). Creation's first backup reaches one of these through RunJobInProcessKeyed. It runs only when the archive is encrypted, the lock was not lost, there was no fatal volume fault, saveErr is nil and the context was not cancelled; failed sources do not skip it. Its commits are plain manifest saves and it writes no index. ErrInventoryWrite on either commit counts as landed, with a warning. Its outcome travels in Result, because recordRun receives `begun` by value. Tests: the local path, Microsoft 365 and creation each run it; a failed last save, a fatal stop and a lost lock each skip it.

- **R2.** (RINT-10, RSEC-04, RSEC-08). Replace 'with the store's generation check' with what exists: StillHeld inside each commit, plus an explicit lock.StillHeld before staging and before step 4. A failure ends the step with a warning and no further writes. Name RecordedGen as P3-7's future work. Beside the claim in §2 and §8, state that the high-water mark is monotone only while the record is not rolled back. Versions compare numerically by MAJOR, MINOR and PATCH; a test pins v1.10.0 > v1.9.0.

- **R3.** (RINT-03). Name the mechanism for each operation in program/. The folder is confined as now (Lstat, realDir, IsLinkLike). Listing, creating temporary files, renames and removals go through the os.Root. Content reads of existing kept files are path-based in judgeFile's shape (Root.Lstat, then OpenRegularFile, then SameFile), with a check that program/ is still the same folder before each open, because Root.OpenFile on Windows refuses every reparse point, cloud files included. Add a Windows OneDrive lab row (pending) and a unit test that injects a refused open.

- **R4.** (RINT-04, RDUR-06, RSEC-02). Detect a placeholder before any open. On Windows: a cloud reparse tag together with RECALL_ON_DATA_ACCESS or RECALL_ON_OPEN from Lstat's attribute data. On macOS: SF_DATALESS. Never decide it from Go's mode bits or the OFFLINE attribute. A placeholder counts as present but unchecked: decide counts it present, and it is never opened, rewritten or removed. Any other irregular or reparse entry at a sealed name is judged 'changed'. A read error is never grounds for a rewrite or a removal. After two runs unchecked, a lasting line reads 'not checked since DATE (kept online only)', and verify -full reports it too. Tests: an injected placeholder predicate, and a non-cloud reparse file at a sealed name.

- **R5.** (RDUR-01). The header's note_sha256 is the only digest that governs. The intent commit records program.note {current, incoming}. While state is intent, refreshNote silently accepts either one and renders the note from the last landed set. The landed commit sets the header digest and clears program.note. Tests: a stop between steps 3 and 4, and between 4 and 5, gives no tamper warning at the next run.

- **R6.** (RDUR-02). The header holds `program` as raw JSON (json.RawMessage), decoded separately and strictly within its format. A program that does not know the format, or fails to decode it, writes the bytes back unchanged and leaves program/ alone, with one warning. Any new field raises program.format. Test: an older program reads a newer set, commits, and the bytes stay identical.

- **R7.** (RDUR-07, RDUR-08, RDUR-15, RINT-09, RSEC-03). Before the pin test, format 1 gains `removing` in the intent, and `missing_runs` and `unchecked_since` on each entry. A hook that finds state=intent resolves it first: it applies the accept-either rule only to that judgement, seals a landed set, then plans afresh. The high-water mark rises only at a landed commit, to the highest version among the release entries that landed. A lasting line, 'an update of the program copies did not finish (since DATE)', is driven by state=intent. The keep toggle and pause commits keep the intent unchanged. The 'incomplete' line clears when the file is present and verified again.

- **R8.** (RSEC-03). Before staging, drop from the plan any name held by an entry the step may neither replace nor remove (a folder, a special file, a link). Report that platform as 'blocked' with a lasting line, and never name it in an intent. Tests: a folder at a sealed name; an intent left standing, then the old version swapped back in.

- **R9.** (RDUR-03). Sync program/ (open '.' through the Root and call Sync; best effort on Windows) after step 1's temporary files and before the intent commit, and after step 4 and before the landed commit. Fault-injection test: the renames are dropped but the landed commit is kept, and no good copy is removed.

- **R10.** (RSEC-01, RINT-15). Sealed hashes come only from the plan: a release entry's hash is its line in the verified SHA256SUMS; a self-copy's hash is computed over the bytes as they are written. The read-back through the same open handle must match, or the entry leaves the plan. After each rename, Root.Lstat the final name: it must be the same regular file as the staged handle. If not, warn and still seal the planned hash. The landed commit never seals a hash computed from disk.

- **R11.** (RSEC-04, RINT-14). <n> is a random 64-bit decimal number. Each temporary file is created through the Root with O_CREATE|O_EXCL, after removing any entry already at that name. export.SweepTemps and SweepOrphanZips skip <out>/program, so the hook alone sweeps its own temporary files, through the Root, and only those older than the run's start.

- **R12.** (RSEC-05). Match listed names to sealed names ignoring case (ASCII). A match is judged as that entry and renamed back to its sealed spelling; it is never removed as unknown. Before landing, Root.Lstat each landed name. Test: a sealed name with its case changed.

- **R13.** (RINT-13). Judge ignores entries for which encmark.IsLitter is true: they are never reported and never removed. Tests: `._MailArchive-macos.zip` and Thumbs.db present.

- **R14.** (RDUR-13, RINT-15). Record the identity of os.Executable's file at process start. Write a self-copy only if the file on disk is still that same file (on every platform; Windows also keeps SiblingQualifies and SameBuild); otherwise write none and warn. Name a bundle-zip function that records Unix modes (0755 for executables) and stores links as links. Test it by unzipping and checking the executable bit; codesign --verify goes in the macOS lab row (pending).

- **R15.** (RDUR-16). State the mapping from os-arch to release platform: darwin/* to macos; windows/amd64, linux/amd64 and linux/arm64 to their own assets; anything else has no asset. Condition 5's every-run warning applies only to this computer's platform. A self-copy whose `reads` is below what the archive needs is removed by any computer, with a warning.

- **R16.** (RDUR-09). Keep each version's checksum file byte-identical as `SHA256SUMS-<version>`. Write it with that version's copies, and remove it when no release copy of that version remains. The note says which file covers each copy.

- **R17.** (RDUR-04, RINT-06, RINT-08, RSEC-06). The download has a 60-second read-idle deadline and an overall limit. A partial file is protected by HoldFile on that partial file itself, never by the cache lock. The cache lock is taken only for short steps (moving a file into place, writing `verified`, pruning, copying out), with a bounded wait of up to 30 seconds, after which the step warns 'cache busy' and takes the self-copy trigger. One download manager per process, so a second request joins the running task. A lock-busy attempt is not counted toward the daily limit. A wait that runs out counts as that day's attempt, and later runs that day do not wait. Ctrl-C means cancel: the step ends 'cancelled' and writes nothing new. Tests: a throttled server, a stalled server, and a copy while another version downloads.

- **R18.** (RDUR-05). SHA256SUMS is written atomically, and its digest is recorded in `verified`. A checksum mismatch deletes SHA256SUMS together with the asset. SHA256SUMS is re-checked against `verified` before it is copied into program/. Two mismatches in a row against a freshly fetched SHA256SUMS end in the terminal class 'repeated checksum mismatch'.

- **R19.** (RDUR-10, RDUR-11, RDUR-12, RSEC-07). 'Used' means a successful copy out of the cache, recorded in the attempt record. Prune at every download and every keyed run. Keep at most this computer's version, the `expects` version and the two most recently used versions. An attempt record dated in the future counts as expired. `verified` is kept per asset. The terminal classes 'not published' and 'repeated checksum mismatch' stop retries until this program's version or `expects` changes. Add a program-cache/format file: a program that finds a newer format leaves the cache alone and warns, and uninstall lists it as one folder. Create the cache folders with mode 0700, check each part of the path without following links, and on Unix refuse a folder another user owns or that group or others can write. Validate `expects` on every read, before any path is built from it.

- **R20.** (RINT-05). In Desktop, the job slot gets a context per job. 'Skip the program download' ends only the wait; Cancel ends the job. A Shutdown is stated to be a crash that the intent recovers from. State that the archive lock can be held for up to 10 minutes during the wait. Tested in PG4.

- **R21.** (RINT-07). Add app.Options.ProgramCopies (unset, on or off). Desktop's create sets it through RunJobInProcessKeyed's options callback; the CLI sets it with -encrypt and -program-copies. The download decision uses the effective value, and the first commit seals it. Unset leaves the sealed value. Tests: creation with the default seals keep=true and starts the download at the first run; unticked seals keep=false.

- **R22.** (RINT-11, RINT-12). ProgramCopies and its sealing move to PG1. The note renderer (shared with refreshNote) and the download's start-and-wait wiring move to PG3. PG2 is the cache and the fetcher. No tag carries PG3 without PG4. Add Options seams for the cache root and for 'this program's version and release flag'. A TestMain in each affected package points the cache root at a temporary folder and fails if the real one is touched.
