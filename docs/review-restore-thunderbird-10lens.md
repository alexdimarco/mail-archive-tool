# Review: Restore to Thunderbird (design-restore-thunderbird, revision 1), 10-lens pre-code review

**Verdict: GO WITH CONDITIONS.** Three findings are HIGH (RT-1 the Windows commit step, RT-2
Thunderbird's profile lock, RT-3 the one Thunderbird behaviour the whole mechanism rests on). The
rescue pass dissolved RT-1 and RT-2 by named mechanism changes and turned RT-3 into a lab spike that
must run before slice R1; none needs a re-design. 24 conditions in all (RT-1 to RT-24), each to be
folded into `docs/design-restore-thunderbird.md` beside the text it changes, marked "Folded from the
10-lens review [RT-n]", and re-verified. **Build may not start (slice R0 included) until the HIGH
and MED conditions (RT-1 to RT-12, RT-24) are folded and the RT-3 spike has run on the Windows VM.**
The LOW ones (RT-13 to RT-23) may be folded with the slice they name. If the spike shows Thunderbird
does not pick up mbox files placed in a closed profile (T1/T2, §2), the verdict becomes NO GO.

| Id | Sev | Lens | Section | In one line | Verdict |
|---|---|---|---|---|---|
| RT-1 | HIGH | 3, 5, 7 | §5.4, §8 | Windows commit: the reused rename refuses directories; open files inside block a folder rename | CONFIRMED; dissolved by exclusive create, no rename |
| RT-2 | HIGH | 6, 7 | §5.1–5.3 | Profile lock: flock misses Thunderbird's fcntl on Linux; symlink can stop Thunderbird starting; the re-check sees its own lock; presync blind | CONFIRMED; dissolved by four changes |
| RT-3 | HIGH | 10, 1, 2 | §3.4, §12.3, §13 | The mechanism rests on unrun forum claims; lab sits after all code | CONFIRMED; spike before R0 |
| RT-4 | MED | 3 | §5.4, §8 | No write/fsync order; crash table misses five states | CONFIRMED |
| RT-5 | MED | 5, 7 | §5.4, §8 | Backups walk restore leftovers (`localwalk.go:583-595`) | CONFIRMED (also by rescue) |
| RT-6 | MED | 6 | §4.4, §4.5 | Name length in runes not bytes; Windows budget misses `.msf` and the 248 limit; SanitizeSegment merges | CONFIRMED |
| RT-7 | MED | 6 | §9.1 | Reused tree builder skips caps for the current archive | CONFIRMED |
| RT-8 | MED | 6, 4 | §6.2, §9.7 | Encrypted into a synced folder asked once; tick not server-enforced; "removed" over-claims; names in progress | CONFIRMED |
| RT-9 | MED | 10, 4 | §6.4 | "Deleted after backup" count would call every local message deleted | CONFIRMED |
| RT-10 | MED | 4, 1 | §3.1, §7.2 | The default path is the double-capture case | CONFIRMED; owner O1 |
| RT-11 | MED | 5, 7, 10 | §3.1, §3.2 | Store Thunderbird: false "Restored"; redirected AppData refuses with no words | CONFIRMED |
| RT-12 | MED | 7, 10 | §6.1, §7.5 | No check of written files before commit (antivirus quarantine, truncation) | CONFIRMED |
| RT-13 | LOW | 5 | §7.3 | Plan token's key discards a pending copy plan | CONFIRMED |
| RT-14 | LOW | 5 | §2, §3.1 | Linux pieces are new code; macOS probe must use the raw count | CONFIRMED |
| RT-15 | LOW | 6 | §3.1, §3.2 | prefs.js escapes, `directory-rel` `..`, profiles.ini cap, list position binding | CONFIRMED |
| RT-16 | LOW | 3 | §7.4, §8 | restores.log must reuse the delete log's torn-tail writer; cleanup limits | CONFIRMED |
| RT-17 | LOW | 4 | §7.5 | Missing words; two restores read as "Thunderbird is open" | CONFIRMED |
| RT-18 | LOW | 5 | §7.1, §6.2 | Restore verb in skip words; encrypted path on the lease; Lock reply | CONFIRMED |
| RT-19 | LOW | 10 | §1, §4.6, §6.1, §9.2, R40 | Claims to label where they stand | CONFIRMED |
| RT-20 | LOW | 2 | §4.7, §3.3 | FAT32 split is gold-plating; maildir signal too narrow | CONFIRMED |
| RT-21 | LOW | 9, 10 | §12.3, §13 | Windows legs not CI-gated; Thunderbird version drift trigger | CONFIRMED |
| RT-22 | LOW | 7 | §6.3 | Space margin ignores Thunderbird's summaries and search index | CONFIRMED |
| RT-23 | LOW | 8 | §2, §13 | Presync parser move as its own commit | CONFIRMED |
| RT-24 | MED | 10, 7 | §12 | Tests and lab rows miss stale-lock, commit-under-antivirus, presync-during-restore | CONFIRMED |

- Date worked on: 6 November 2026 (the session's date).
- Design reviewed: `docs/design-restore-thunderbird.md` at commit fbc1a11 (branch
  `restore-thunderbird`, off main 794b86e = v1.5.9), sha256
  `18a7eb7c2bb701d46c909b5693a3792374310f87704723fbe9003d40a3b0f6ff`.
- File name: the caller asked for `docs/review-restore-thunderbird-10lens.md` (the shape of
  `review-delete-marker-10lens.md`). The design's status line names
  `docs/review-restore-thunderbird-predesign.md` (the kit's convention, `design-review.md:7`). No
  meta-test enforces either name (none scans `docs/design-*.md`). Condition RT-24 fixes the status
  line to cite this file.
- Procedure: `../assurance-kit/process/design-review.md`. Read-only; no code was run, no
  Thunderbird was run.
  - Lens passes 1–10 and the skeptic's code check: the synthesiser (this file's author) read the
    load-bearing code directly (list below).
  - Citation sweep: one read-only agent checked every other file:line in the design and the go-mbox
    v1.0.4 module source; its load-bearing results were re-read by the synthesiser (marked
    "re-read").
  - Rescue pass: one separate agent, given RT-1, RT-2, RT-3 and the W-RECAPTURE default (RT-10) and
    told to save the design. Its outcomes are recorded per blocker below.
- Register: review synthesis (findings, the synthesiser's decisions, and opinion where marked).
  Statements about Thunderbird's internals that are the reviewers' recollection, not read, are
  marked **(recollection, unverified)**.

## 1. Claims checked against the code (the skeptic's part)

### 1.1 Read by the synthesiser

| Design claim | Where read | Result |
|---|---|---|
| The copy export's no-replace rename can be reused for the commit (§2 row "Writing without following links", §5.4) | `internal/app/copyexport_place_windows.go:100-102` | **WRONG for rename 1 on Windows.** The source is opened with `FILE_NON_DIRECTORY_FILE`; rename 1 renames a *directory* (`….sbd`), which that open refuses. Linux/macOS (`renameat2`, `renameatx_np`) take directories. → RT-1 |
| Every write is through `*os.Root`, exclusive create, 0o700 folders, no-replace placement (`copyexport_fs.go:15-31`, `:78`, `:108-117`) | `internal/app/copyexport_fs.go` | Confirmed. `Replace` (`:117-119`) is a plain `Root.Rename`, as the design says. |
| Other Unix fallback is a hard link (`copyexport_place_other.go:11-14`) | `copyexport_place_other.go`, `copyexport_place_unix.go:27-60` | Confirmed, and wider: Linux and macOS *also* fall back (`noFlagSupport`) on a filesystem that rejects the flag (FAT/exFAT): a link, then a look-up-then-rename with a stated race window. A directory cannot be hard-linked, so a directory rename always takes the windowed path there. → RT-19 |
| `readIdentity` gives volume + file id, and the file system name (§3.2 step 4, §4.7) | `copyexport_place_windows.go:142-168`, `_darwin.go:36-55`, `_linux.go:44-71` | Confirmed. On Windows it **fails** ("the volume reported no file identity") when the volume gives none — relevant to a redirected (network) AppData. → RT-11 |
| `thunderbird.Running`: live `lock` pid on Unix, `parent.lock` held on Windows (`thunderbird.go:118-165`) | `internal/thunderbird/thunderbird.go:127-165` | Confirmed. Consequence the design misses: it would see MailArchive's *own* lock (its own live pid; its own no-share handle) → RT-2. |
| prefs.js parser trims quotes only (`thunderbird.go:60-77`) | same file `:60-77` | Confirmed (`strings.Trim(v, "\"")`). |
| The archive lock: exclusive OS lock, refusal "archive is in use by another mailarchive run" (`lockfile.go:38-91`) | `internal/lockfile/lockfile.go` | Confirmed. Note: it is **flock** on Unix and LockFileEx on Windows (`:4`), and `AcquireAs` truncates and writes a holder line into the file (`:88-89`). Neither may be used on Thunderbird's lock files. → RT-2 |
| Extract holds the lock for its whole run, no verb for an encrypted archive (`extract.go:158-172`) | `internal/app/extract.go:158-175` | Confirmed. |
| `-dest` overlap and keyless refusals (`extract.go:154`, `:193-199`) | same | Confirmed. |
| The unencrypted copy's shape: plan, token, lease, progress every 3 s (`encryption_copy.go`) | `internal/desktop/encryption_copy.go` | Confirmed, plus two things the design does not carry over: the copy asks **twice** for a folder that may leave the computer, checked server-side (`:98`, `plan.AskTwice()`), and its progress lines are **counts only, never a path or a subject** (`:170-180`). → RT-8 |
| A scheduled backup that meets the lock skips (§7.1) | `internal/app/busy.go:40-91` | Confirmed and better than the design says: it skips **quietly** (no failure, alert, streak or history) for a holder under 24 h old. `holderWords` has no restore verb, so it would say "another MailArchive task". → RT-18 |
| Backups would never see the hidden staging pair (implied by §5.4) | `internal/source/localwalk.go:525-595`, `internal/source/source.go:149-155` | **WRONG.** A dot-named *file* is skipped (`isMailboxCandidate`), but the orphan-container loop (`localwalk.go:583-595`) walks `.mailarchive-restore-ID.sbd` as a folder named `.mailarchive-restore-ID`. → RT-5 |
| restores.log follows Delete's rule for an unreadable log (§8) | `internal/archdelete/dellog.go:180-219` | The delete log's writer already handles a torn last line, a size cap (`errLogFull`), fsync and 0o600. The design cites only the "unreadable refuses" half. → RT-16 |
| `pendingSecrets` reuse with a new kind (§7.3) | `internal/desktop/pending.go:132-159` (re-read) | Confirmed, with a trap: `put` discards every other item **with the same key**. → RT-13 |
| Tree bounds apply to the person's own archive too (§9.1) | `internal/app/copyexport_archive.go:408-419`, `:508-511` (re-read) | **WRONG by reuse.** Node and id-byte caps apply only to *another* archive folder (`if !a.other { return nil }`). → RT-7 |
| "Deleted from the mailbox after backed up" = records the timeline marks not present (§6.4) | `copyexport_archive.go:857` (re-read); agent: `export/exporter.go:718-724`, `manifest.go:851-865` | **WRONG.** `Present` is set only for mailbox-wide archives; the workspace's own test is `Gone: !(rec.Present \|\| rec.LastSeen.IsZero())`. As written, every Thunderbird or local record would be counted as deleted. → RT-9 |

### 1.2 From the citation sweep (agent), not re-read unless marked

Every other citation is CONFIRMED except these:

| Citation | Finding |
|---|---|
| `util/paths.go:44-81` SanitizeSegment (§2 "so two names never merge") | **WRONG.** The hash suffix is added only when a character was replaced or the name cut (re-read, `:53-80`). Trimming, the `_` device prefix and "untitled" add none: ".Inbox", "Inbox." and "Inbox" all become "Inbox"; "CON" and "_CON" both become "_CON"; ".", ".." and "untitled" all become "untitled". Device names missing: COM0, LPT0, COM¹²³/LPT¹²³, CONIN$, CONOUT$, CLOCK$, "CON .txt". The cap is 120 **runes** (re-read, `:77`), not bytes. Replacement is a space, not `_`. DEL, C1 controls and U+200E/U+200F are kept. → RT-6 |
| §3.1 step 3, §9.4 "on macOS and Linux it is owned by this user…" | **No Linux code.** presync has no `resolve_linux.go`; these checks are new code. `runnow_sync.go:129-142` returns only true/false and is false on Linux (`:130`). → RT-14 |
| `env_darwin.go:351` (macOS process probe) | Drops any Thunderbird process it cannot fully read; the safe count for a refusal is `rawThunderbirdCount` (`:373`). → RT-14 |
| `env_windows.go:268-292` | Matches `thunderbird.exe` for **every user** on the machine. Safe direction; friction on a shared PC. → RT-17 |
| `presync/resolve.go:50-52`, `:77`, `:99-100` | profiles.ini over 256 KiB is cut silently, not refused; bidirectional characters not stripped; `Name=` not parsed; `[Install…] Default=` always joined as relative. → RT-15 |
| `localstore.go:129` ("holds cur and new") | The code's test is `cur` **or** `new`. → RT-20 |
| `localwalk.go:535` ("the source's own file names") | Archived folder names already went through SanitizeSegment at backup (`localwalk.go:86-88`). Round trip still keeps names Thunderbird itself wrote; the claim's wording is loose. → RT-19 |
| `rawstub.go:63-67` | Off: that is `isPanicStub`; Raw is kept at `localwalk.go:488-494`, `source/mbox.go:112`, `:208`. |
| `copyexport_archive.go:30-35`, `:62-68`; `pending.go:15-21`; `scenario-catalog.md:1431-1444` | Off-by or minor (the plaintext-only load is at `:358-364`; a store's label is `token — mailbox`; the discard paths are `pending.go:63-70`, `:209-227`; MA-1105..1109 do not exist). Ids MA-1084..1099 and MA-1119..1132 are free on main and nine named branches. |
| go-mbox v1.0.4 `reader.go:62-64` | Removes one `>` only from a line beginning exactly `>From ` (mboxo style). `>>From ` is left. Thunderbird 122+ writes strict mboxrd (§4.1), so a body line that was `>From ` in a Thunderbird 122+ mbox is archived as `>>From ` — one `>` too many — and a restore faithfully carries that. Existing behaviour, outside this design. → side finding A (§6) and RT-19 |
| §6.1 "said before starting, with the counts" for damaged records | Cannot be: planning reads no message (`emlPresent` only Lstats). Fixity mismatches are found only while restoring. → RT-19 |

## 2. Thunderbird-behaviour claims: all lab-required, and which would break the design

Nothing about Thunderbird has been run. Every row below is an assumption until its lab row passes.
"Breaks" means the mechanism itself fails and a different mechanism is needed; "fix" means a
bounded change inside this design.

| # | Claim (design section) | Source the design gives | If wrong |
|---|---|---|---|
| T1 | Thunderbird, started after a bare mbox file was put in Local Folders while it was closed, **discovers it as a folder** (§3.4) | Support forum threads 1457240, 1075104, 1272388 (summarised) | **BREAKS.** Writing files directly would not restore anything; a different mechanism (Thunderbird's own import, an add-on) would be needed. |
| T2 | It **builds the missing `.msf`** from the mbox and shows every message (§3.4) | same | **BREAKS** (same reason). |
| T3 | It ignores dot-named entries (§5.4) | not checked | Fix: the design already moves staging out of Local Folders. The synthesiser recalls `nsShouldIgnoreFile` skipping a leading `.` **(recollection, unverified)**. |
| T4 | What it does with `NAME.sbd` with no `NAME` file (§5.4) | not checked | Fix (RT-1 picks the commit order around it). |
| T5 | Holding its profile lock keeps a starting Thunderbird out, and a dead holder leaves nothing stale (§5.2) | "my understanding" | Fix (RT-2; the design already says it drops the lock if the lab disagrees). Getting the *format* wrong is the dangerous case, not the lock failing to hold. |
| T6 | Its current reader un-quotes mboxrd (`>+From `) and accepts LF or CRLF framing (§4.1–4.3) | Thunderbird source on searchfox, read 2026-11-06, not run; bugs 1719121, 121947 | Fix (quoting choice or W-OLDTB wording). Read in source, so the strongest of these. |
| T7 | Version ≤115 shows `>From` lines; `compatibility.ini` `LastVersion=` names the last version (§4.1) | bug 1134690; "not checked in source" | Fix (warning only). |
| T8 | `directory-rel` (`[ProfD]…`) wins over `directory` (§3.2) | "what I understand" | Fix, but a silent one: a restore into a folder Thunderbird does not use says "Restored" and shows nothing. The synthesiser recalls `NS_GetPersistentFile` reading the relative pref first **(recollection, unverified)**. |
| T9 | `storeContractID` names and default (§3.3) | "my knowledge" | Fix (detection). |
| T10 | A file with no extension shows; ".mbox" does not (§3.4) | forum thread 1457240 | Fix (naming). |
| T11 | The reserved endings list (§4.4) | "my reading of Thunderbird's habits" | Fix. The synthesiser recalls more skip rules: a leading `#`, a trailing `~`, and names such as `msgFilterRules.dat`, `filterlog.html`, `junklog.html`, `popstate.dat`, `sort.dat`, `mailfilt.log`, `filters.js`, `feeds.json`, `feeditems.json` **(recollection, unverified)**. → RT-6 |
| T12 | Read state comes from the preserved `X-Mozilla-Status`; a deleted-flag message stays hidden (§4.6) | not checked | Fix. Also likely: Thunderbird's automatic compaction then **removes** such messages from the restored file **(recollection, unverified)** — the archive still has them. → RT-19 |
| T13 | A folder shows its file name when no summary names it (§4.4) | not checked | Fix (wording). |
| T14 | Thunderbird reads mbox files over 4 GiB (§4.7) | "my understanding" | Fix. |
| T15 | A Microsoft Store Thunderbird keeps its profiles elsewhere (§3.1) | not checked | Fix, but silent if ignored (RT-11). |
| T16 | Thunderbird does not opt into long Windows paths (§4.5) | not checked | Fix (budget already assumes it does not). |
| T17 | The 2026-10-31 lab saw 153.6.0esr on the Windows VM (§12.3) | `thunderbird.go:49-52` comment | Context only. |

Only T1 and T2 would break the design. They are also the claims most likely to be true (copying mbox
files into a closed profile's Local Folders is the long-standing manual method), which is why RT-3 is
a cheap spike before code, not a re-design.

## 3. Rescue pass outcomes

The rescue agent was given RT-1, RT-2, RT-3 and RT-10 with candidate escapes, and told to save the
design. It re-read `copyexport_place_windows.go`, `thunderbird.go:118-165`, `presync/running.go`,
`env_windows.go:110-189`, `lockfile.go`, `lock_unix.go`, `localwalk.go:500-612`, `source.go:149`. It
did not read Mozilla's source, run Thunderbird, or check Windows file-system behaviour; its
Thunderbird and Windows statements are recollection, as marked in the conditions.

| Blocker | Outcome | The change |
|---|---|---|
| RT-1 Windows commit | **DISSOLVED-BY** escape (c) | Exclusive `Mkdir` of the final `.sbd` at start, tree written inside, the empty parent file created exclusively last; no rename, so neither the directory flag nor open files inside can block the commit. Costs nothing in R40 beyond the one unknown (T4) the design already had. |
| RT-2 profile lock | **DISSOLVED-BY** four changes | Windows unchanged; Unix a POSIX record lock only (OFD on Linux), no symlink; checks before the take, a failed take = running; a MailArchive restore lock presync can see; release proven on every path. |
| RT-3 load-bearing claims | **PARTIAL** | Not a design flaw; a one-hour spike before R0 settles T1–T5. Survives as a gate: if T1 or T2 fails, NO GO. |
| RT-10 double capture | **SURVIVES** | A modest default rule helps little; a backup-side skip needs an owner ruling (O1). |
| (new) backup walks the residue | found by the rescue, same as RT-5 | Clean up (or skip) restore leftovers before a backup reads the profile. |

No finding was refuted outright. The rescue pass did not find a blocker underneath the three.

## 4. Conditions

Severity is the failure's impact if built as written. "Lens" uses the kit's numbering
(1 purpose, 2 proportionality, 3 durability, 4 usability, 5 integration, 6 security, 7 failure
domains, 8 migration, 9 operational cost, 10 honesty).

### RT-1 — HIGH — lenses 3, 5, 7 (Windows) — §5.4, §8 "Commit" rows, §14 decision 4

**Failure.** On Windows, rename 1 (`.mailarchive-restore-ID.sbd` → `Restored — NAME.sbd`) is a
directory rename. (a) The helper the design reuses opens its source with `FILE_NON_DIRECTORY_FILE`
(`copyexport_place_windows.go:100-102`), so as written rename 1 fails on every Windows restore.
(b) Even with that fixed, NTFS refuses to rename a directory while any process holds a file open
inside it — Explorer's "a file in it is open in another program". Defender scans each file just after
it is closed; OneDrive (a Local Folders moved into Documents) holds files while uploading; the search
indexer may hold them. The design's 5-second retry covers only writes (§8 "antivirus" row), not the
commit. Result on the biggest user group: the restore writes everything and then fails at its last
step. (c) "If rename 2 fails, rename 1 is undone" is a second directory rename that can fail the
same way, leaving a final-named `.sbd` with no parent file and no row in §8 for it.
(The NTFS rule is the synthesiser's knowledge, widely reported in npm and Git-for-Windows "EPERM on
rename" issues; lab-required.)

**Fix to fold (rescue escape (c), adopted).** No directory rename at all:
1. At start, after the §5.3 re-check, exclusively create `Restored — NAME.sbd` through the Local
   Folders root (`Root.Mkdir`, which fails on an existing name on every platform and file system).
2. Write the whole tree inside it under final names (files still created exclusively).
3. Last, exclusively create the empty parent file `Restored — NAME` (`O_CREATE|O_EXCL`). The parent
   file is always empty (§3.4), so creating it does what renaming an empty staging file into place
   did, without a rename.
4. If step 3 meets an existing name (someone made it during the restore), remove the tree that
   `restores.log` names and refuse: "The name was taken while restoring; nothing was added." Never
   attach the tree to someone else's folder.

This leaves one Thunderbird unknown instead of two: whether Thunderbird ignores a `NAME.sbd` with no
`NAME` file (T4, MA-1091 second half) — the state the design's own "crash between the renames" row
already leaves. Hidden names are not needed while writing, because Thunderbird is checked closed and
kept out by RT-2's lock; they mattered only for crash residue. **If MA-1091 shows Thunderbird adopts a
lone `.sbd`:** keep the final folder name, write each mbox file under a hidden name, and place files
one by one with the existing *file* helper plus the §8 retry. The dot-name question (T3) then returns
for files only. §5.4, §8's commit rows, §14 decision 4 and R40's wording change accordingly.

### RT-2 — HIGH — lenses 6, 7 — §5.1, §5.2, §5.3, §14 decision 3

**Failure.** Four ways the profile lock as written goes wrong:
1. *It does not keep Thunderbird out on Linux.* Mozilla takes a POSIX `fcntl` write lock on
   `.parentlock` **(recollection, unverified)**. The repo's lock primitive is `flock`
   (`lockfile.go:4`); on Linux `flock` and `fcntl` locks do not see each other. "Take an OS lock on
   `.parentlock`" built with the existing package would let Thunderbird start mid-restore. A POSIX
   lock has its own trap: closing *any* descriptor of that file anywhere in the process drops it.
2. *It can stop Thunderbird starting.* Mozilla's symlink is `IP:+PID`, the `+` meaning "this holder
   also has the fcntl lock" **(recollection, unverified)**. A symlink in another format, with a host
   part Thunderbird computes differently, or naming the pid of a process that is still alive, can read
   to Thunderbird as a live holder, so it says "Thunderbird is already running" until someone deletes
   `lock`. The design's stale argument (§5.2, "a crash leaves the symlink naming a dead pid") assumes
   the holder died. MailArchive Desktop is a long-lived server: a restore that fails without a crash
   (an error on the removal, a panic recovered by the job runner) leaves the symlink naming a **live**
   pid for as long as Desktop runs. Presync and `thunderbird.Running` would also read it as running.
3. *It refuses every restore.* §5.3 re-checks "Thunderbird not running (§5.1)" **under** the profile
   lock. Signal A is `thunderbird.Running`, which reads MailArchive's own symlink (its own pid, alive)
   on Unix and fails to open MailArchive's own no-share `parent.lock` on Windows
   (`thunderbird.go:127-165`). As written, the check sees its own lock.
4. *Presync cannot see it.* §5.2 says a pre-sync "sees the lock held". Presync's signals are the
   symlink and the process list (`running.go`, `env_windows.go:161-175`); an fcntl-only lock is
   invisible to both, and a scheduled run is a separate process.

**Fix to fold (rescue, adopted).**
1. **Windows:** as designed — open `parent.lock` with no sharing; the handle closes on every exit path
   and when the process ends, so nothing goes stale.
2. **macOS and Linux:** no `lock` symlink. Only a POSIX record lock on `.parentlock`, the kind Mozilla
   takes (to confirm by reading `nsProfileLock.cpp` in R2 before writing it). Linux: the per-handle
   kind (`F_OFD_SETLK`), which conflicts with Mozilla's `F_SETLK` and is not dropped when another
   descriptor closes. macOS has no per-handle kind: `F_SETLK`, plus a test that nothing else in the
   process opens `.parentlock` while it is held. Never the `lockfile` package on Thunderbird's files
   (flock, and it writes a holder line).
3. **Order:** run §5.1's checks A and B, *then* take the lock; a failed take counts as running. After
   the take, re-check only B (the process list), since A would see the restore's own lock. Words:
   "Thunderbird, or another restore, is using this profile…" (and RT-17).
4. **Presync and other MailArchive runs:** the restore also holds a MailArchive lock,
   `restore-<hash of the profile's canonical path>.lock` in the settings folder, taken with
   `lockfile.Acquire` (never stale: an OS lock released when the process ends). Presync's
   `detectRunning` (`running.go:23`) and a second restore treat `lockfile.Held` on it as "in use".
5. **Release on every path** of a long-lived Desktop: success, error, Cancel, a recovered panic.
   MA-1127 proves each by a child process taking the lock afterwards.

Dropping the profile lock instead was tested and rejected: it would lose "only while Thunderbird is
closed" (a Thunderbird started mid-restore could index a half-written mbox and show missing messages
until the folder is repaired), and keeping it is cheap once the format question is removed.

### RT-3 — HIGH — lenses 10, 1, 2 — §3.4, §5.4, §12.3, §13

**Failure.** The mechanism rests on T1 and T2 (§2 above): that Thunderbird discovers mbox files
placed in a closed profile's Local Folders and builds their summaries. The design's evidence is three
summarised forum threads; no lab row has run, and the lab rows sit in slice R6, after five slices of
code. If T1/T2 are false for Thunderbird 153 the work of R1–R5 is wasted. T3, T4 and T5 decide the
shape of RT-1 and RT-2.

**Fix to fold (rescue, adopted: a gate before R0, not at R6).** A hand spike of about an hour on the
Windows VM with its installed Thunderbird (153.6.0esr), recorded as lab notes, before any slice:
- Thunderbird closed, put in Local Folders: a hand-made mbox (CRLF, a `From ` and a `>From ` body
  line, one message with `X-Mozilla-Status`, one without); `P` (empty) plus `P.sbd/C`; a lone
  `Q.sbd/C` with no `Q`; a dot-named file and a dot-named `.sbd`. Start Thunderbird: which folders
  appear, their counts, which `.msf` files it made, and whether a message saved as `.eml` matches.
- Hold `parent.lock` from PowerShell (`[IO.File]::Open(path,'OpenOrCreate','ReadWrite','None')`) and
  start Thunderbird: what it says; close the handle: it opens normally.
- On this Mac, the same with a small program holding an `fcntl` lock on `.parentlock`.
- Why now: the forum threads are years old, and Thunderbird has been moving folder data into a
  global database (the "Panorama" work) **(recollection, unverified)**. If 153 finds folders that way,
  file-scan discovery could fail (T1), which would break the design.

The spike's results are folded into §3.4, §5.2 and §5.4 before R0; if T1 or T2 fails, the design
returns for re-design (NO GO). MA-1090, MA-1091 and MA-1097 are equally load-bearing and their Windows
legs run first in R6 (RT-11 covers MA-1097's interim).

### RT-4 — MED — lens 3 — §5.4, §7.4, §8

**Failure.** No write ordering is stated. A power cut after the commit renames but before the files'
data reach the disk can leave a visible, final-named folder whose mbox files are short or zero-filled,
with a "started" line and no "ended" line; the next attempt does not know whether to clean or keep.
The crash table also lacks rows for: both renames done, "ended" line missing; a crash during cleanup
(after Cancel or failure); Thunderbird opened between a crash and the next attempt (it may have made
`.msf` files inside the staging or the half-committed tree, so "removes exactly the entries its line
names" can no longer empty the folder); the Local Folders volume missing at the next attempt; the
person never trying again.

**Fix to fold.** State the order in §5.4 and §8: (1) append and fsync the "started" line (with the
staging names *and* the final name chosen) before the first create; (2) fsync each mbox file when it
is complete; (3) fsync each staging directory (no-op on Windows, as `copyexport_fs.go:139-141`
states); (4) the commit (RT-1); (5) fsync Local Folders; (6) append and fsync the "ended" line. Add
the five missing rows, each with what is on disk, what is shown, and the next attempt's action. A
next attempt that finds the final name present and the staging names gone treats the restore as
finished (writes the "ended" line, removes nothing). A cleanup that meets entries it did not make
leaves them and says so in words (RT-17).

### RT-5 — MED — lenses 5, 7 — §5.4, §7.2, §8 "crash" row

**Failure.** A backup walks the staging directory. `isMailboxCandidate` skips a dot-named *file*
(`source.go:149-155`) but the orphan-container loop (`localwalk.go:583-595`) reads
`.mailarchive-restore-ID.sbd` as a folder. So (a) after a crash the hidden pair stays until the
person happens to run Restore again for that profile — possibly never — and every backup of this
profile (the W-RECAPTURE case, which RT-10 shows is the default) copies the half-written folders,
including a torn last message, into the archive under `.mailarchive-restore-ID/…`; (b) a backup of a
*different* archive that reads the same profile (not stopped by this archive's lock) can do the same
during a restore.

The rescue pass found this independently. With RT-1's commit the residue is a final-named
`Restored — NAME.sbd` with no parent file, which the same loop reads too.

**Fix to fold.** (1) Before a backup reads a Thunderbird profile (`RunHeadless` and the Desktop's own
runs), it finishes the cleanup of any unfinished restore that `restores.log` names for that profile,
under RT-2's MailArchive restore lock, or skips those exact entries when it cannot (Thunderbird open)
— a fixture test for each. (2) An unfinished restore is surfaced without waiting for a new Check:
Desktop's start and `status` read `restores.log` and say "An earlier restore into Thunderbird did not
finish (DATE)…". Skipping MailArchive's own leftover entries is not a change to what a backup captures
of the person's mail, so this is the lead's call, not an owner ruling.

### RT-6 — MED — lens 6 (Windows first) — §4.4, §4.5, §3.5

**Failure.**
1. *Units.* SanitizeSegment caps at 120 **runes**. ext4 and APFS allow 255 **bytes** per name, NTFS
   255 UTF-16 units. 120 CJK characters are 360 bytes; 120 emoji are 240 UTF-16 units before
   "Restored — ", " (99)" and `.sbd`. Thunderbird then adds `.msf` beside every file. A long
   Microsoft 365 folder name fails the restore mid-write on macOS and Linux, and can leave a file
   whose `.msf` Thunderbird cannot create.
2. *The Windows budget.* "Under 260" must be ≤ 259 characters for a file and ≤ 247 for a directory
   (CreateDirectory's limit), counting Thunderbird's own `.msf` beside the deepest file.
3. *Merges.* SanitizeSegment does not always add its hash (§1.2): "Inbox." / ".Inbox" / "Inbox",
   "CON" / "_CON", "." / ".." / "untitled" collide. §4.4 step 4 de-duplicates siblings, which saves
   it only if step 4 runs **after** every other step, on the exact bytes written, NFC- and
   case-blind.
4. *Gaps.* Device names COM0, LPT0, COM¹²³, LPT¹²³, CONIN$, CONOUT$, CLOCK$, "CON .txt"; DEL, C1
   controls, U+200E/U+200F; Thunderbird's own skip rules beyond the design's list (T11: a leading
   `#`, a trailing `~`, the named files).

**Fix to fold.** A restore-specific name step after SanitizeSegment: budget each component in UTF-8
bytes and UTF-16 units at ≤ 255 − len(".msf") − len(" (99)"), cut with the hash when over; the
Windows path budget as in (2) including `.msf`; the extra device names and controls; the full skip
list, taken from Thunderbird's `nsShouldIgnoreFile` source in slice R3 (as §4.4 step 3 already
promises), each tried in MA-1095; step 4 last. The plan (not the write) finds every over-budget name,
so nothing fails mid-write for a name.

### RT-7 — MED — lens 6 — §9.1, §2 row "Folder tree with counts"

**Failure.** §9.1 says the tree's bounds (10,000 folders, 8 MiB of ids) apply "for the person's own
archive too, because a restore writes files for each". The builder the design reuses skips them for
the current archive (`copyexport_archive.go:508-511`). "Open an archive folder…" can make any folder
the current archive, including one someone else wrote. As built by reuse, a manifest naming a million
folders creates a million entries in Local Folders, and Thunderbird scans them at every start.

**Fix to fold.** The restore applies its own caps to what it will create (folders and files), whatever
the tree builder allows, refused in the plan with words; MA-1088 gains the 10,001-folder case.

### RT-8 — MED — lenses 6, 4 — §6.2, §6.4 W-CLOUD, §7.3, §9.7

**Failure.**
1. An encrypted archive restored into a Local Folders that OneDrive, Dropbox or iCloud sync (or on a
   removable drive) sends plaintext off the computer, including the staging files, which are uploaded
   while they are written and stay in the service's history after Cancel removes them. The
   unencrypted copy asks **twice** for a folder that may leave the computer and checks that on the
   server (`encryption_copy.go:98`); the restore asks once.
2. §7.3 lists `notEncryptedOK` in the start request but does not say the server refuses an
   encrypted plan without it.
3. "Removed on cancel or failure" (§9.7, the "Stopped" words) reads as erased. Removal does not undo a
   sync upload, a Time Machine or shadow-copy snapshot, or an SSD's copy.
4. The progress line "Restoring: 1,200 of 4,310 messages (Inbox)" names a folder; for an encrypted
   archive the copy's rule is counts only (`encryption_copy.go:170-180`). The result's damaged-record
   names and Save this record carry names too.

**Fix to fold.** Reuse the copy's ask-twice rule and server-side check for an encrypted restore into a
cloud-synced or removable Local Folders (or refuse it — owner question O2). The server refuses an
encrypted start without `notEncryptedOK`. The words for Stopped and for the not-encrypted line say
what removal does not undo. Encrypted progress and Activity lines are counts only; Save this record
says it contains folder names.

### RT-9 — MED — lenses 10, 4 — §6.4 last bullet, MA-1124, Q3

**Failure.** The count "N of these messages were deleted from the mailbox after they were backed up"
uses records the timeline marks not present. `Present` is set only for mailbox-wide archives, so for
every Thunderbird or local archive (most Windows users) the plan would call every message deleted.

**Fix to fold.** Use the workspace's own rule, `!(rec.Present || rec.LastSeen.IsZero())`
(`copyexport_archive.go:857`), and say where it does not apply. MA-1124 tests a local archive and a
Microsoft 365 archive.

### RT-10 — MED — lenses 4, 1 — §3.1 step 4.1, §7.2, Q7

**Failure.** The default profile is the one the archive backs up (§3.1 step 4.1), and in that case the
design only warns (§7.2). Most people have one Thunderbird profile, which is the one they back up. So
the *default* path restores into the backed-up profile and the next backup copies every restored
message into the archive again; the next restore then offers "Restored — NAME" inside the archive,
and so on. The design treats this as an edge (a warning); on the default path it is the common case.

**Fix to fold.** (1) Lead, now: §3.1 step 4 prefers a profile the archive does *not* back up when one
exists, and the plan's W-RECAPTURE line moves to the top of the plan when it applies. (2) Owner: the
rescue pass confirmed (1) rarely helps (one profile is usual) and that anything stronger — a backup
that skips folders `restores.log` names — changes what a backup captures. That is owner question O1,
which replaces Q7's "warn only" with an informed choice.

### RT-11 — MED — lenses 5, 7, 10 (Windows) — §3.1 Store bullet, §3.2 step 4, MA-1097

**Failure.** (a) For a Microsoft Store Thunderbird whose profile is virtualized under
`%LOCALAPPDATA%\Packages\…`, the design "says nothing" until lab MA-1097: if the person also has an old
`%APPDATA%\Thunderbird`, the restore writes there, says "Restored N messages… Open Thunderbird to see
it", and nothing appears. A success claim that is false is the worst outcome in the friction rules.
(b) An AppData redirected by company policy to a network share: `readIdentity` may fail ("the volume
reported no file identity", `copyexport_place_windows.go:164-166`) and the restore refuses with no
words in §7.5; `UnderCloudSync` does not flag it.

**Fix to fold.** Detect a Thunderbird package folder under `%LOCALAPPDATA%\Packages` (name match on
"Thunderbird") and, until MA-1097 passes, show "If you use Thunderbird from the Microsoft Store, this
restore may not show in it" (or refuse — the lead's call; owner question O3 if refuse). Words for an
unreadable identity and a network Local Folders. Lab row MA-1097 gains the redirected-AppData case.

### RT-12 — MED — lenses 7, 10 — §6.1, §7.5 "Done", §4.7

**Failure.** Nothing checks the written files before the commit. Defender can quarantine a whole mbox
because one archived message carries a known-bad attachment (the archive keeps originals, so it can
hold such mail); a full or failing disk can truncate. The result would still say "Restored N
messages", and the folder would be missing or short.

**Fix to fold.** Before the commit, re-open each staged mbox file through the root and check it exists
with the size written (cheap, required). Recommended (lead's call): re-read each file with the same
parser MA-1089 ports and compare each message's SHA-256 with what was written, so "each message the
archive's preserved original bytes" is checked at run time, not only in tests. A vanished or changed
file is named in the result, with the antivirus remedy.

### RT-13 — LOW — lens 5 — §7.3

`pendingSecrets.put` discards every item with the same key (`pending.go:144-150`): a restore plan
keyed by the archive cancels a pending unencrypted-copy plan or key change for that archive. **Fix:**
its own key (as delete has, `pending.go:39-42`), and add the kind to `ofCurrentArchive` so a switch
discards it.

### RT-14 — LOW — lens 5 — §2 rows "Thunderbird profiles", "Is Thunderbird running", §3.1 step 4.1

Linux pieces the table calls reuse are new code: the profile ownership and mode checks, the Run-now
profile lookup (false on Linux, `runnow_sync.go:130`), the process probe. macOS must refuse on the
raw process count (`env_darwin.go:373`), not the filtered list (`:351`). **Fix:** say "new" in the
table, put the Linux checks in R2 with their own rows, and name the raw count.

### RT-15 — LOW — lens 6 — §3.1, §3.2

prefs.js decoding must also take `\n`, `\r` and refuse (not guess) an escape it does not know;
`directory-rel` is resolved as Mozilla does (`..` allowed, then the §3.2 step 3 checks), not
"joined under the profile"; a `user.js` that sets the Local Folders directory is noted as a lab case.
profiles.ini over the cap is refused for a write path, not cut; `[Install…] Default=` may be
absolute; profile names are stripped of bidirectional characters for display; the page's "position
in the list" is bound to the list the server made (kept with the plan or re-listed and compared), so
a profiles.ini rewritten between two clicks cannot point the position at another profile.

### RT-16 — LOW — lens 3 — §7.4, §8

`restores.log` reuses the delete log's append discipline (`dellog.go:180-219`: torn last line kept
apart, size cap, fsync, 0o600) rather than only its "unreadable refuses" rule — otherwise a power cut
mid-append refuses every later restore. Cleanup removes only names matching
`.mailarchive-restore-<the recorded ID>` whose identity matches what the line recorded, never a
`Restored — ` name; recursive removal goes through the root and follows no link. Words for "log full"
and "log unreadable", each with a remedy. The CLI and Desktop use the same log (one settings folder).

### RT-17 — LOW — lens 4 — §7.5, §8

Words missing from §7.5: Local Folders identity unreadable; Local Folders overlaps the archive or
another account; prefs.js too large; a Unix profile with group/world-writable modes; restores.log
unreadable or full; cleanup met entries it did not make; another MailArchive restore into this
profile is running (a terminal and Desktop at once now read as W-RUNNING, which tells the person to
close a Thunderbird that is not open — take a MailArchive-owned restore lock first so the two are
told apart). W-RUNNING when no Thunderbird window shows: "If you don't see it, look for its icon by
the clock, or another person on this computer may have it open" (the Windows probe sees every user's
Thunderbird; Thunderbird's background update task may also match — lab note).

### RT-18 — LOW — lens 5 — §7.1, §6.2, Q6

Add a restore verb for an unencrypted archive to the lock and to `holderWords` (`busy.go:72-91`), so a
skipped scheduled backup says "a restore of this archive into Thunderbird" rather than "another
MailArchive task"; the plan's line matches `ScheduledLockSkip`'s behaviour (quiet for 24 hours). The
encrypted path runs on the lease, not on `ExtractSealed`, which stops when the archive is locked
(`extract.go:418-419`). Lock while a restore runs replies as the copy does (`copyGoesOnText`).

### RT-19 — LOW — lens 10 — §1, §2, §4.6, §6.1, §9.2, R40

Label the claims where they stand: R40's "visible in one step" depends on T3/T4 and the commit RT-1
picks; "cannot replace" holds except on FAT/exFAT under macOS and Linux (the look-up-then-rename
window, `copyexport_place_unix.go:27-33`); §9.2 "nothing existing is opened for writing" excludes the
lock files; §6.1 cannot count damaged records before starting (planning reads no message) — the plan
says they are found during the restore; §4.6 "keeps them hidden" adds "and may remove them from the
restored folder at its next compaction; the archive keeps them"; MA-1089 proves the writer against a
port of Thunderbird's reader (a model), MA-1093 against Thunderbird; "byte for byte" means the
archive's preserved bytes, which for an mbox source are go-mbox's output (CRLF, and side finding A);
the §1.2 off-by citations corrected.

### RT-20 — LOW — lens 2 — §4.7, §3.3

The FAT32 split into "NAME (part 2)…" is machinery for a profile on FAT32, which nobody has reported:
refuse a folder of 4 GiB or more on FAT32 with words instead. The maildir second signal is `cur` or
`new` or `tmp` in a folder directory (Thunderbird's maildir may not make `new` — recollection), plus
Inbox/Trash being directories; needed because a policy-locked store setting may not be in prefs.js.

### RT-21 — LOW — lenses 9, 10 — §12.3, §13 R6

Windows-only unit legs (the `parent.lock` probe, the Windows directory commit, MA-1087's Windows leg)
are not gated by CI (the CI test gate is Linux-only): R6 runs them on the Windows VM and the release
notes say so. Each lab row records the Thunderbird version it ran; a re-check trigger is written down
(each new Thunderbird ESR, or a Thunderbird change to its mbox store, folder discovery or profile
lock).

### RT-22 — LOW — lens 7 — §6.3

The space margin (5 % + 256 MiB) ignores what Thunderbird adds after the restore: a summary per
folder and its global search index of every restored message, both on the same (often system) drive.
Raise the margin (lead's number, stated as a choice) and say in the plan that Thunderbird will use more
space as it indexes.

### RT-23 — LOW — lens 8 — §2, §13 R2

Moving presync onto the shared parser touches shipped code that has had regressions handled with
care. Do it as its own commit in R2, presync's existing rows unchanged and green before and after, and
the new parser's extra outputs (names, default) unused by presync.

### RT-24 — MED — lenses 10, 7 — §12

The tests and lab rows miss the properties most likely to fail:
- Lab: kill MailArchive mid-write (Task Manager on Windows, `kill -9` on macOS/Linux), then start
  Thunderbird: it opens normally (stale lock). A non-crash failure while Desktop keeps running, then
  start Thunderbird: it opens normally.
- Lab: Defender on and OneDrive syncing Local Folders during the *commit* (MA-1121 covers writes only).
- Lab: MA-1091 on Windows, macOS and Linux, with a Thunderbird 115 and the current release.
- Lab: a scheduled backup with pre-sync fires during a restore: pre-sync does not start Thunderbird
  on the profile.
- Lab: a Flatpak Thunderbird's lock (its pid is in another pid namespace) is read correctly after it
  crashes (MA-1099).
- Unit: the Unix profile lock is a POSIX record lock that another process sees (`F_GETLK` from a
  child), and stays held after the process opens and closes another descriptor of `.parentlock`.
- Unit: the walker skips the staging names (RT-5); the plan finds every over-budget name (RT-6); the
  restore's own folder cap (RT-7); the ask-twice rule (RT-8); the deleted count on a local archive
  (RT-9); the post-write check (RT-12).
- Status line: cite this file as the 10-lens review, and record that RT-1 to RT-24 were folded.

## 5. Questions

### 5.1 For the owner (product choices)

The design's Q1–Q6, Q8 and Q9 stand as written; this review has no objection to their
recommendations. Q7 is replaced by O1. Two are new.

- **O1 (replaces Q7) — restoring into the profile the archive backs up.** This is the default path,
  not an edge (RT-10): the next backup copies every restored message into the archive again.
  Choices: (a) warn only, as the design says; (b) backups skip the folders a restore made (named in
  `restores.log`), which changes what a backup captures. Reviewer's recommendation: (b), because the
  person did not ask to back up a second copy, and (a) leaves the archive growing every time someone
  restores; (a) is acceptable for 1.6.0 only if the plan puts the warning first.
- **O2 (new) — an encrypted archive restored into a synced or removable Local Folders** (RT-8).
  Choices: ask twice, as "Make an unencrypted copy" does; or refuse. Recommendation: ask twice, the
  same rule the person already met in the copy.
- **O3 (new, only if the lead chooses to refuse) — Microsoft Store Thunderbird before lab row MA-1097
  has run** (RT-11). Recommendation: warn, not refuse, until the lab says where the Store version
  keeps its profiles.

### 5.2 For the lead (technical; the owner has delegated these)

- The commit mechanism (RT-1, escape (c)) and its fallback if MA-1091 disagrees.
- The Unix lock call per platform, after reading `nsProfileLock.cpp` (RT-2).
- Whether the post-write check is size-only or a full re-read with SHA-256 (RT-12; recommended:
  full).
- Clean-up-before-backup versus skip-only for restore leftovers, and whether Desktop cleans at start
  (RT-5).
- The Store-install interim: warn (recommended) or refuse (RT-11).
- The space margin (RT-22); the FAT32 refusal replacing the split (RT-20); the restore verb's words
  (RT-18); the restore's own folder cap number (RT-7).
- The order and fsync points (RT-4) and the restores.log writer (RT-16).

## 6. Side findings (existing code, outside this design)

A. **go-mbox un-quotes only one level** (`reader.go:62-64`, mboxo style). Thunderbird 122 and later
   write strict mboxrd (design §4.1), so a body line that was `>From ` in such a Thunderbird mbox is
   archived as `>>From `, one `>` too many, and every export of it (extract, copy, restore) carries
   that. This extends the design's own side finding 4; it is a fidelity question for the owner of the
   archive's "preserved original" claims, not for this design.
B. **SanitizeSegment can merge names** (§1.2): its hash suffix is added only when a character was
   replaced or the name was cut. Every caller that relies on "two names never merge" (its own comment,
   R6) should be checked; backups' folder paths go through it (`localwalk.go:86-88`).
C. **The Flatpak and Snap lock pid.** A Flatpak Thunderbird writes its pid as seen inside its own pid
   namespace **(recollection, unverified)**; `thunderbird.Running` checks that number on the host, so
   after such a Thunderbird crashes it may read as running for good (refusing, the safe direction).
D. **`run.go:1860-1881` reads the `APPDATA` environment variable**, while presync uses the Windows
   known-folder lookup (`resolve_windows.go:88-96`). Two ways to find the same folder.

## 7. Counters

- Date worked on: 6 November 2026. Clock read from the session; not inferred.
- Register: review synthesis — findings (code read), the synthesiser's decisions, and opinion where
  marked; Thunderbird and Windows behaviour statements marked "recollection, unverified" are opinion.
- Drew on, and reached:
  - the design at fbc1a11 (digest above), and `../assurance-kit/process/design-review.md`;
  - the code listed in §1.1, read directly;
  - the citation agent's report (all its other rows, §1.2), with its five load-bearing results re-read:
    `copyexport_archive.go:408-419`, `:508-511`, `:857`, `pending.go:132-159`, `paths.go:18-90`;
  - the rescue agent's report (§3);
  - `internal/app/busy.go`, `internal/archdelete/dellog.go:180-219`,
    `internal/source/localwalk.go:500-609`, `internal/source/source.go:149-155`,
    `internal/presync/env_windows.go:110-199`.
- Not reached or not checked, per claim:
  - Thunderbird's source (`nsProfileLock.cpp`, `nsShouldIgnoreFile`, `NS_GetPersistentFile`,
    `MboxMsgInputStream.cpp`), Mozilla's bug tracker and the support threads: not read by this
    review; every statement about them is recollection or the design's.
  - No Thunderbird was run; no lab row has run; no test was run.
  - The NTFS rule on renaming a folder with open files: knowledge, not tested here.
  - Whether a scheduled run's pre-sync runs before or after the archive lock: not checked (it matters
    to RT-2 change 4, which covers both orders).
- The smallest check that would settle the most: the RT-3 spike on the Windows VM — put a hand-made
  mbox, a `P` + `P.sbd/C`, a lone `Q.sbd/C` and a dot-named pair into a closed profile's Local Folders,
  start Thunderbird, and see which appear; then start it while PowerShell holds `parent.lock`.
- Borrowed lines: short code quotes from the files cited; the rescue escapes are the rescue agent's,
  adopted with attribution.
- Drift: none noticed; the review stayed on the design as frozen.
- Touched: this file only (new), and one commit. The design text and product code are unchanged.
- Persona: none.
- Source reached, per source: design — yes; repo code — yes (as listed); go-mbox module — yes (by the
  citation agent); assurance-kit process — yes; Thunderbird source and Mozilla sites — no.
