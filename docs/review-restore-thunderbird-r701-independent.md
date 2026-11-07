# Restore to Thunderbird — independent review of the R7-01 patch

Date worked: 2026-11-07. Reviewed: commit `475b0d6` ("every folder and file is private from
its creation", MA-1128) on branch `restore-thunderbird`, at tip `eb65a44` (the reviewed files
are unchanged between the two). Process: `../assurance-kit/process/adversarial-review.md`, the
separate skeptic that `docs/review-restore-thunderbird-adversarial-r7.md` asked for (its
finder and fixer were the same model). This reviewer is also a model (Claude), a fresh
instance that had not seen the patch before; I read the code first, then the R7 document's
R7-01 rows.

## Verdict: NOT CLEAN

One finding must be fixed before this ships (F1, a regression: on macOS a restore into a
Local Folders on a FAT32 or exFAT drive now always fails, plain archives included). The
security mechanism itself held on every probe that reached it.

| ID | Severity | Finding | Probe |
|---|---|---|---|
| F1 | MED (regression, fails closed) | macOS: `mkdir_extended` with an access list returns "invalid argument" on FAT32 and exFAT, so every restore there fails at the container, plain and encrypted; before the patch both finished | P-M2, P-M3 |
| F2 | LOW (defence in depth) | Windows: `createAt` hands the last name part to `NtCreateFile` raw, skipping `os.Root`'s checks: a `\` in it walks through a junction out of Local Folders, a `:` writes a stream into an existing file; not reachable today (`SafeName` strips both) | P-W2 |
| F3 | LOW | macOS: the bind check after `mkdir_extended` compares identity only, by a path look; with an ancestor of Local Folders swapped away for the create and back before the look, plus a folder planted at the name, it passes, and what is made inside is born with the planted folder's "everyone" entry | P-M5 |
| F4 | INFO | The parent file made at the commit (`write.go:524`) never goes through `FS.Private`, so "FS.Private still reads each entry back" is not true of it (it is empty at the commit, and born owner-only on NTFS, APFS, HFS+) | reading |
| F5 | INFO | The new test's "others may read" is a name search (macOS `everyone`; Windows `BUILTIN\Users`, `Everyone`), not "only this user": it would miss `Authenticated Users` (a second drive's real default) or a group entry | reading, P-W1 |
| F6 | INFO | When the bind check does catch a redirect, the entry already made outside Local Folders stays there, and the error words are a raw `statat … no such file or directory`; on the commit's Create, a failed bind is then reported as W-TAKEN (the name "taken by another program") | P-M4, reading |

### F1 — macOS FAT32 and exFAT: every restore now fails

`create_darwin.go` makes the container and the parent file with `mkdir_extended` /
`open_extended` and a `kauth_filesec`. On a volume that cannot hold an access list the kernel
refuses the call with `EINVAL`; there is no fallback (rightly, no fallback to an unprotected
create in general), so the write stops at step 2.

- P-M2 (`r701probe`, `TestProbeVolumes`): disk images made with `hdiutil`, umask 0, Local
  Folders given an inheritable `everyone allow read` where the format takes one. APFS and
  HFS+: every entry born `drwx------` / `-rw-------`, no access list. **MS-DOS FAT32 and
  ExFAT** (attached `-owners off`, as macOS mounts a USB drive): `chmod +a` "Operation not
  supported" (no access lists exist there to inherit), then `mkdir C.sbd: invalid argument`
  and `open C: invalid argument`.
- P-M3 (`tbengine`, a whole restore through `Run`): FAT32 and exFAT, plain archive:
  `done=false`, "The restore could not finish (mkdir Restored — Work mail.sbd: invalid
  argument), so nothing was added to Thunderbird." Encrypted, after the plan asked twice
  (W-READERS) and both answers were given: `done=false`, "The restore could not finish (a
  file error, not named for an encrypted archive …)". **Baseline:** the same probe with
  `rootFS.Mkdir`/`Create` put back to `root.Mkdir(0700)` / `root.OpenFile(0600)`: all four
  restores finish ("Restored 4 messages …").

Fails closed (nothing is written), so it is not an exposure; but it removes a working case
(a profile on a drive shared with Windows) that the plan still offers, and the words give no
remedy. Not run, likely the same: macOS SMB, AFP, WebDAV, NTFS (read-only or a third-party
driver), macFUSE volumes.

Suggested fix: choose the call by what the volume can hold, not by trying. `pathconf(dir,
_PC_EXTENDED_SECURITY_NP)` on Local Folders (or the `EINVAL`/`ENOTSUP` of the first
`*_extended` call); where the volume has no access lists, nothing can be inherited, so
`root.Mkdir(0700)` / `root.OpenFile(0600)` are owner-only at birth as far as that volume can
say, and the plan already asks twice there (W-READERS, FAT and ignore-ownership). Covering
test: a real FAT32 and exFAT image restore finishes on macOS, plain and encrypted-confirmed,
with the APFS twin unchanged.

### F2 — Windows: the last name part is not checked

`createAt` opens the parent through the root (checked) but passes `leaf` straight to
`NtCreateFile` as a relative name. NT treats `\` as a separator and `:` as a stream. P-W2
(`TestProbeLeafNames`, on the VM, a junction `C.sbd\J` to a folder outside Local Folders):
`Create("C.sbd/J\escaped")` succeeded and the file `escaped` appeared **outside** Local
Folders; `Create("C.sbd/Trash:stream")` succeeded, a stream on an existing file this restore
did not make. The same names through `os.Root` (the code before the patch): "path escapes
from parent" for both. Today every name the writer passes comes from `SafeName`
(`util.SanitizeSegment` replaces `<>:"/\|?*` and controls) or is a constant, so neither is
reachable; but `FS`'s own contract ("no name, link or '..' can reach outside it",
`fsys.go:15-17`) no longer holds in the FS layer. Fix: refuse a leaf containing `\` or `:`
(or one `filepath.IsLocal` refuses) before the create; a probe like P-W2 as the test.

### F3 — macOS: the bind check can be satisfied by a planted folder

`mkdirPrivate` makes the container by path, then compares `os.Lstat(path)` (a path look
again) with `root.Lstat(name)` (through the root). P-M5 (`tbrestore`, a seam added in the
throwaway clone only, between the create and the look): Local Folders reached through
`link → A`; `link` re-pointed to `B` for the create, back to `A` in the seam, and a folder
with an inheritable `everyone` entry planted at `A/LF/C.sbd`. `Mkdir("C.sbd")` returned nil,
and the marker then made inside showed `0: group:everyone inherited allow read,execute`.
Preconditions are heavy: another account able to rename an ancestor of Local Folders and to
create in Local Folders itself. In that real case the planted folder is the other account's,
so `FS.Private`'s owner check stops an encrypted restore before the marker; a plain restore
goes on (Private's failure is ignored there). Fix (one line): `bindCreated` also requires the
entry be this user's (`Stat_t.Uid == os.Getuid()`); a planted folder cannot be.

### F4–F6

- F4: either call `w.private(top, false)` after the commit's create (it is cheap and the
  file is empty), or say in `FS`'s comment that the parent file is checked only by its birth.
- F5: Windows could compare the SDDL (protected, one ACE, this user's SID), as P-W1 does;
  macOS could require `ls -le` show no ACL line at all.
- F6: P-M4 (`TestProbeSymlinkedAncestorRedirect`): with Local Folders' ancestor re-pointed
  after the root opened, both creates were refused (good), and `C` and `C.sbd` were left in
  the other folder. Low value to fix; worth a sentence in the design.

## What held

- **Windows descriptor at birth** (P-W1, `TestProbeBirthDescriptor`, on the VM as its
  signed-in user, Local Folders granting `BUILTIN\Users` read and `Authenticated Users`
  modify, both inheritable): container, marker, subfolder, nested file and parent file each
  read back `O:<user SID>…D:P(A;OICI;FA;;;<user SID>)` (folders) or `D:P(A;;FA;;;<user SID>)`
  (files): owner the token's user (not Administrators, not the group), protected, one entry,
  nothing inherited (`ID` absent). 0 of 5 not owner-only.
- **Windows construction**: the SDDL is built from the process token's user SID;
  `SecurityDescriptorFromString` returns a Go copy (no `LocalFree` lifetime to manage); the
  folder handle is closed, the file handle is owned by the `*os.File`, the parent handle is
  closed by `defer`. The only retry (`ERROR_INVALID_OWNER`) keeps the protected owner-only
  access list and drops only the owner. No error path falls back to an unprotected create.
- **Windows links**: a junction standing at the leaf's name: `FILE_CREATE` fails ("already
  exists") and nothing is made through it; a junction in the parent path is refused by
  `os.Root` (P-W3). An existing name gives `fs.ErrExist`.
- **Every create in the write path** goes through `FS.Mkdir`/`FS.Create`
  (`write.go:182, 223, 255, 348, 385, 524`); no `os.Mkdir`/`os.OpenFile`/`os.Create` is left
  in it. The other creates in the packages are outside Local Folders and outside R7-01: the
  restore log and its lock (MailArchive's config folder, `restorelock.go:50`) and
  Thunderbird's empty profile lock file (`tblock_unix.go:85`).
- **macOS mechanism**: the `kauth_filesec` layout matches XNU (magic at 0, owner and group
  GUIDs at 4 and 20, entry count at 36, flags at 40, 44 bytes); `KAUTH_UID_NONE` is
  `0xFFFFFF9B`; `kauth_acl_inherit` skips inheritance when the initial list carries
  `KAUTH_ACL_NO_INHERIT`. On APFS and HFS+ (P-M2), with umask 0 and an inheritable
  `everyone` entry on Local Folders, every entry was born 0700/0600 with no access list.
  Entries inside the container (made through the root) inherit nothing because the
  container has nothing to give. `open_extended` uses `O_EXCL|O_NOFOLLOW`, and its bind
  check compares the open descriptor (true identity), which a swap cannot fool.
- **Ignore ownership**: `OwnersIgnored` reported true for FAT32 and exFAT attached
  `-owners off` and false for APFS/HFS+ attached `-owners on`.
- **The branch's own suites**: macOS `go test ./internal/tbrestore/ ./internal/tbengine/`
  green; Windows VM (as its signed-in user) `internal/tbrestore`, `internal/tbengine` PASS.

## Not checked

- A second macOS account (no admin rights here); macOS is by mechanism and by reading
  each entry the instant it is made.
- Windows on FAT32/exFAT (making a FAT volume needs admin) and an SMB share (the VM's `P:`
  share refused a folder: "Access is denied"). By reasoning FAT ignores the descriptor and
  the create succeeds; not run.
- macOS SMB/AFP/NTFS/macFUSE (see F1).
- The Windows lab's 0-of-20 race was not re-run; no temporary Windows account was made for
  this review.

## Probes

Kept out of the tree (a throwaway clone, deleted after): `internal/r701probe`
(`probe_darwin_test.go`: P-M2, P-M4; `probe_windows_test.go`: P-W1, P-W2, P-W3, a share
probe that skipped), `internal/tbengine/zz_r701probe_darwin_test.go` (P-M3, and its
baseline with the old create calls), `internal/tbrestore/zz_r701probe_darwin_test.go` with a
seam in `create_darwin.go` (P-M5). No product code changed.

## Settled 2026-11-07

Fixed by a model (Claude, the fixer, not this review's author), on the lead's calls of
2026-11-07. Each finding's test was red on the code reviewed and is green after; the design
notes are `docs/design-restore-thunderbird.md` §6.2, "Settled 2026-11-07 [R7-01 independent Fn]".

| ID | Outcome | Covering test (MA-1128) |
|---|---|---|
| F1 | Fixed. macOS asks the volume (`pathconf _PC_EXTENDED_SECURITY_NP` on the root's open folder) before choosing the create: no access lists → the plain exclusive create (0700 / 0600). Query failure → plain create only on a volume also detected as not private, else W-NOTPRIVATE in plain words (the drive's format and what to do); the same words when a volume refuses the call. Real disk images: FAT32 and exFAT whole restores finish (plain; encrypted refused without the second yes, finished with it); APFS and HFS+ still born owner-only, no access list. | `tbengine/ma1128_r701_volumes_darwin_test.go`, `tbrestore/ma1128_r701_independent_darwin_test.go` |
| F2 | Fixed. `createAt` refuses a last part holding `\`, `/`, `:` or NUL, or one `filepath.IsLocal` refuses; through a junction and as a stream on the VM, nothing made. | `tbrestore/ma1128_r701_leaf_windows_test.go` |
| F3 | Fixed. The bind also requires this user's own entry (not where ownership is ignored: there it says nothing, and the plan asks twice). Proven with a real root-owned folder (`/private/var`) found through a re-pointed link. | `tbrestore/ma1128_r701_independent_darwin_test.go` |
| F4 | Fixed. The parent file is read back by `FS.Private`; on failure with MustBePrivate it is removed only as the very file its handle names, then the one cleanup; if it cannot be named so, it stays and nothing is removed. | `tbengine/ma1128_r701_parentfile_test.go` |
| F5 | Fixed. Windows: entries allowing Everyone, Authenticated Users, Users or Guests, by SID; Local Folders given Users read and Authenticated Users modify. macOS: any access-list entry. On the VM the old name search let a create granting Authenticated Users read pass; the new one is red. | `tbengine/othersread_windows_test.go` (helper), the R6-01 and R7-01 tests |
| F6 | Fixed. A failed bind is W-CHANGED in plain words (what changed, that what was made may be elsewhere and was left, Click Check again), never a raw `statat`; at the commit it is no longer W-TAKEN. Nothing is removed: what the path made cannot be shown to be the entry a path now names. | `tbrestore/ma1128_r701_independent_darwin_test.go`, `tbrestore/ma1128_r701_commitbind_test.go` |

Not settled here: macOS SMB, AFP, WebDAV, NTFS and macFUSE volumes were not run (by the
code, the query decides there too); Windows on FAT32 or exFAT and a second macOS account,
as before.
