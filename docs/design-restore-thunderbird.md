# Design: Restore to Thunderbird…

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
2. **List them.** Each `[ProfileN]` section gives a name (`Name=`) and a folder
   (`Path=`, relative when `IsRelative=1`). Each `[Install…]` section's `Default=` names
   the profile that Thunderbird opens (`resolve.go:96-101`). The new parser returns
   name, canonical folder and "opens by default" per profile.
3. **Keep only usable profiles.** A listed folder is offered only if it passes presync's
   profile check: it exists, is a folder, holds `prefs.js` as a regular file; on macOS
   and Linux it is owned by this user and not group- or world-writable
   (`resolve_darwin.go:113-150`; the Windows reduction is stated at
   `resolve_windows.go:116-121`). Presync's macOS refusal of a path with a space
   (`resolve_darwin.go:121-127`) is about matching process arguments and does NOT apply
   to restore.
4. **Pick the default.** In order:
   1. the profile the archive's own Thunderbird source came from: the job Run now
      would run (`runnow_sync.go:108-122`), its first input that sits in a listed
      profile (`runnow_sync.go:129-142`, `thunderbird.go:27-42`);
   2. the profile Thunderbird opens by default (`[Install…] Default=`);
   3. the profile marked `Default=1`;
   4. none: the person chooses.
5. **One profile:** shown, not asked. **Several:** the person chooses from the list
   ("NAME — FOLDER"), the default pre-selected. **None:** refused (§10, words W-NOPROF).
6. The page sends the chosen profile's position in the list the server made, never a
   path. The command line takes `-profile FOLDER` (typed by the person, so it is
   accepted without being listed, as presync accepts a named profile,
   `resolve_windows.go:137-139`) or `-profile-name NAME`; either still has to pass step 3.

### 3.2 The Local Folders directory

Read `prefs.js` (bounded to 16 MiB; a larger file is refused as "not a Thunderbird
profile"). String values are decoded as JavaScript string literals (`\\`, `\"`,
`\uXXXX`), because the existing parser only trims quotes
(`internal/thunderbird/thunderbird.go:60-77`) and a Windows path in prefs.js reads
`"C:\\Users\\…"`.

1. The Local Folders account is the server named by
   `mail.accountmanager.localfoldersserver` (for example `server1`). If that is absent,
   the one server whose `mail.server.serverN.type` is `"none"`. Zero or several → refused
   (W-NOLOCAL).
2. Its folder is `mail.server.serverN.directory-rel` when that begins with `[ProfD]`
   (joined under the profile), else `mail.server.serverN.directory` (an absolute path:
   some people move Local Folders to another drive, and Thunderbird honours it). This
   order — the relative setting first — is what I understand Thunderbird to use; not
   checked in Thunderbird's source; lab row MA-1095 includes a profile whose two settings
   disagree.
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

### 4.5 Path length on Windows

A Windows program that does not opt into long paths cannot open a path of 260 characters
or more. Whether Thunderbird opts in is not checked. **Decision:** every path the restore
writes — the deepest file, with its `.sbd` parts, under the longer of the staging name and
the final name — stays under 260 characters on Windows. Over budget, the names on that
branch are shortened (each cut to 30 characters plus the hash suffix), deepest first;
still over, that folder is not restored and is named in the plan with why (W-LONGPATH).
macOS and Linux have no such limit at these lengths. Lab row MA-1092 (a deep tree under a
long Windows user name).

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

### 4.7 Large folders

- Messages are streamed one at a time; a message larger than extract's 2 GiB cap is not
  restored and is named (`internal/app/extract.go:34-39`).
- One file per folder, however big. Thunderbird reads mbox files over 4 GiB on NTFS, APFS
  and ext4 (my understanding, since about Thunderbird 12; not checked; lab row MA-1121).
- A FAT32 drive cannot hold a file of 4 GiB or more. When the Local Folders volume is
  FAT32 (the volume's file system name, `readIdentity`) and a folder's planned size
  reaches 4 GiB − 1 MiB, the folder is split into `NAME`, `NAME (part 2)` … at message
  boundaries, and the plan says so.

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

### 5.3 Checked again right before writing

At "Restore", under the archive's lock and the profile lock, before the first byte:
Thunderbird not running (§5.1), the Local Folders directory the same one (identity, §3.2
step 4), the chosen name still free (§3.5), free space still enough (§6.3), the archive's
manifest unchanged since the plan (size, modification time, SHA-256, as Delete's plan does,
`docs/design-archive-delete.md` §3.1). Any difference: "This changed since you clicked
Check: WHAT. Click Check again." and nothing is written.

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

### 6.3 Size and free space

Needed space = the originals' sizes (recorded fixity size, else the file size; sealed size
for an encrypted archive, which slightly overstates) + 120 bytes per message for framing.
Refused when free space on the Local Folders volume (`util.FreeBytes`) is less than needed
× 1.05 + 256 MiB, room left for Thunderbird's own summaries and for the system (W-SPACE).
These margins are the lead's choice.

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

### 7.4 The restore record

- `restores.log` in MailArchive's settings folder (beside `deletions.log`,
  `internal/desktop/archivedelete.go:267`), one JSON line when a restore starts (id, date,
  archive folder, profile, Local Folders path, staging names, planned counts) and one when it
  ends (result, counts, the final folder name). It is how the next attempt finds an unfinished
  one (§8). For an encrypted archive it holds no folder names (§6.2).
- **Save this record** saves the plan and result as a text file where the person chooses, as
  Delete's record does (`archivedelete.go:63`).
- The command line prints the record and takes `-record-out FILE`.

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
7. **Encrypted archives.** Plaintext only inside the destination, under the staging names
   until the end, and removed on cancel or failure (§6.2, §8). Memory is not wiped beyond
   what the vault already does.
8. **Requests.** Unchanged guards (§7.3); the start is a one-use token of its own kind.

This design owes the adversarial pass on the writer and the profile lock (§13).

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

## 11. New invariant and scenarios (provisional ids)

- **R40 — A restore adds, and only adds.** A restore to Thunderbird writes only new entries
  inside the chosen profile's Local Folders directory (and holds Thunderbird's profile lock
  while it writes), never changes, renames, replaces or removes anything that was there,
  never writes under another account's directory, only while Thunderbird is closed, and
  makes the new folder visible in one step at the end. It writes only preserved originals.
- Scenarios S66 Thunderbird open at Check; S67 Thunderbird started during a restore; S68 a
  crash part way; S69 a hostile folder name in the archive; S70 restoring into the profile
  the archive backs up.

## 12. Tests

### 12.1 Ids

On main the catalog's last rows are MA-1082 (`docs/scenario-catalog.md:1450`) and MA-1100..
MA-1118 (`:1431-1444`); MA-1083 is taken on branch `cloud-sync-note` (not merged). So this
design takes **MA-1084..MA-1099**, then **MA-1119** on. Provisional until slice R0 writes them
into the catalog (structural id allocation, which the owner lets the lead assume).

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
