# Restore into Thunderbird — RT-3 spike on Windows, 6 November 2026

Gate RT-3 of the 10-lens review (`docs/review-restore-thunderbird-10lens.md`): a hand spike on
the Windows VM before any slice, to settle claims T1–T5 (§2 of the review) and the Windows legs
of RT-1 and RT-2. Run by an agent on the VM, VM clock 19:03–19:13 (UTC−5), 6 November 2026.

**Verdict: RT-3 holds.** Thunderbird 153 finds mbox files placed in a closed profile's Local
Folders and builds their summaries (T1, T2). It ignores dot-named entries (T3) and a lone
`NAME.sbd` with no `NAME` file (T4). Holding `parent.lock` keeps it out, and nothing goes stale
(T5, Windows). Folder renames fail with a file open inside (RT-1 confirmed), and exclusive create
behaves as escape (c) needs. Four findings change details, not the mechanism (see "What this
changes").

## Set-up

- Parallels VM "Windows 11" (ARM64, build 10.0.26100), signed-in user, driven with
  `prlctl exec --current-user` running PowerShell scripts from the Mac share (`P:\restorelab\`).
  Mouse and keys were real input events in the signed-in session; evidence is `prlctl capture`
  screenshots (listed at the end, not committed) and file listings and hashes taken on the VM.
- **Thunderbird 153.6.0** (file and product version of the exe), the ordinary install at
  `C:\Program Files\Mozilla Thunderbird\thunderbird.exe`. No Microsoft Store package is installed
  (`Get-AppxPackage *thunderbird*` empty, no `Packages\*hunderbird*` folder).
- No Thunderbird was running at the start. The user's own profiles (four, in
  `%APPDATA%\Thunderbird\profiles.ini`) were not opened or changed. The test ran a separate
  instance: `thunderbird.exe -no-remote -profile C:\Users\Public\tb-restore-lab\profile`. Using
  `-profile` with a path instead of `-CreateProfile` kept the user's `profiles.ini` untouched:
  its SHA-256 (and `installs.ini`'s) was the same before and after the run.
- The test mailboxes were made on the Mac by a script with exact bytes (CRLF lines, mboxrd
  quoting, separator `From - Thu Nov 05 10:00:00 2026`). They were copied in with
  `FileMode.CreateNew`, and each placed file's SHA-256 matched its source.

### First finding, before the cases: no Local Folders on a profile with no account

On first start of the empty profile, Thunderbird 153 showed its account setup ("Welcome to
Thunderbird — Add your email address"; Escape did not close it, its Minimize button did) and
created **no** `Mail\Local Folders` and no Local Folders account (`prefs.js` had no
`mail.server.*`). The brief's assumption that it is created automatically on first start did
not hold. To get one without any mail account, the lab profile's `user.js` was given only the
Local Folders server keys (`mail.accountmanager.localfoldersserver`, `type "none"`,
`hostname "Local Folders"`). On the next start Thunderbird made the rest itself: `Trash`,
`Unsent Messages` (shown as Outbox) and their `.msf` files, and it wrote
`directory`, `directory-rel` (`[ProfD]Mail/Local Folders`) and
`storeContractID "@mozilla.org/msgstore/berkeleystore;1"` into `prefs.js`.

## A — what Thunderbird shows for files placed while it was closed

Placed with Thunderbird closed, then Thunderbird started (`07`–`09` screenshots):

| Case | Placed | Seen in Thunderbird | Files Thunderbird made |
|---|---|---|---|
| A1 | `R1`, one mbox file, 3 messages | **R1** shown; opening it: "3 Messages", status bar "Building summary file for R1…" | `R1.msf` when opened |
| A2 | `P` (empty) + `P.sbd\C` | **P** with an expand arrow; **P › C** shown, "1 Message" | `P.msf` at start; `P.sbd\C.msf` when opened |
| A3 | `Q.sbd\C`, no `Q` file | **nothing**: no Q, no C. Not listed in `folderCache.json`, no `.msf`, folder left exactly as placed | none |
| A4 | `.R2` + `.R2.sbd\C` | **nothing**: hidden. No cache entry, no `.msf`, left as placed | none |
| A5 | `S.sbd\` first, then `S.sbd\C`, then the empty `S` last (RT-1 order) | **S › C** shown, "1 Message" | `S.msf` at start; `S.sbd\C.msf` when opened |

Two controls added during the run (see A1 below): `R3` (CRLF, two messages whose first header
is `Return-Path:` and `X-Lab-First:`) and `R4` (LF-only lines). Both shown with their counts.

**A1 messages.** All three opened with the right content:
- m1, whose original body line `From here…` was stored as `>From here…`: shown as
  **`From here…`** (one `>` removed).
- m2, whose original line `>From there…` was stored as `>>From there…`: shown as
  **`>From there…`**. So Thunderbird 153 un-quotes mboxrd correctly (supports T6).
- m2 carried `X-Mozilla-Status: 0001` and was shown as already read; m1 and m3 had no status
  header and were shown unread (supports §4.6 for the read flag).
- m3: "1 attachment: lab-bytes.bin 2.0 KB" (2048 bytes in the original).

**Byte comparison (`.eml` saved with Ctrl+S, compared by SHA-256 and `fc /b`).** No saved file
was byte-identical to its original. Each one was the original **minus its first header line**,
and byte-identical after that line, attachment included:

| Saved | Original | Lost | Rest identical |
|---|---|---|---|
| m1 (R1, first in file) | 377 → 334 bytes | `From: Lab Sender <sender@example.invalid>` + CRLF | yes |
| m3 (R1, last, with attachment) | 3433 → 3390 | same line | yes |
| m4 (R3) | 333 → 294 | `Return-Path: <sender@example.invalid>` + CRLF | yes |
| m6 (R4, LF-only mbox) | 287 → 253 | the `From:` line; and the saved file uses CRLF throughout | — |

To find out whether the loss happens when Thunderbird *reads* a placed mbox (which would matter
for a restore) or only in Save As, m5 (first header `X-Lab-First: 1`) was deleted from R3, so
that Thunderbird itself copied it into `Trash`. The raw `Trash` file then held Thunderbird's own
separator `From - Sat Nov 07 00:10:13 2026`, then **exactly m5's bytes**, then one CRLF
(checked byte for byte). Saving that Trash copy with Ctrl+S again lost `X-Lab-First: 1`.
**So Thunderbird 153's Save As drops the first header line of any local-folder message, even one
it wrote itself; reading a placed mbox loses nothing.** It is a Thunderbird behaviour, not
something the restore causes, but it means Save As is not a usable byte check (see "What this
changes"). Save As also writes CRLF lines whatever the mbox uses.

Other things seen: Thunderbird did not add `X-Mozilla-Status` headers to the message it copied
into Trash. Deleting m5 did not rewrite `R3` (still 707 bytes; the folder cache recorded
`expungedBytes 304`), so removal waits for compaction (relevant to T12). Thunderbird keeps a
`folderCache.json` at the profile root listing each folder it knows; ignored entries (A3, A4) had
no line in it. No new folder database was seen in the profile (the "Panorama" worry in RT-3):
discovery in 153.6.0 is still by scanning files.

## B — RT-2: Thunderbird started while `parent.lock` is held

A hidden PowerShell process opened `profile\parent.lock` with
`[IO.File]::Open(path,'OpenOrCreate','ReadWrite','None')` and held it for 60 seconds; the lab
Thunderbird was started 3 seconds later.

- Thunderbird did **not** open the profile. It showed a dialog titled **"Close Thunderbird"**:
  *"Thunderbird is already running, but is not responding. The old Thunderbird process must be
  closed to open a new window."* with the buttons **Close Thunderbird** (default) and **Cancel**
  (`23-locked-start.png`).
- Pressing **Close Thunderbird** (Enter): the new Thunderbird exited. The lock holder was **not**
  killed and kept the lock until its 60 seconds ran out. (The holder was `powershell.exe`, not
  `mailarchive.exe`; neither is a Thunderbird process, so the result is expected to carry over,
  but it was not run with MailArchive.)
- After the holder closed the handle: Thunderbird started normally, profile open, no stale-lock
  message (`25-after-release.png`).
- Extra, for T5's "a dead holder leaves nothing stale": a second holder was **killed**
  (`Stop-Process -Force`) instead of closing. `parent.lock` could be opened exclusively at once,
  and Thunderbird started normally.

## C — RT-1: Windows file-system behaviour (NTFS, `C:\Users\Public\tb-restore-lab\rt1`)

A folder with one file inside; another process held that file open (opened for reading), with
four sharing modes. `[IO.Directory]::Move` on the folder:

| Sharing the holder allowed | Rename while held | After the holder ended |
|---|---|---|
| none | **fails**, "Access to the path … is denied" | succeeds |
| read | **fails**, same | succeeds |
| read + write | **fails**, same | succeeds |
| read + write + delete | **fails**, same | succeeds |

So a folder rename fails while anything holds a file inside open, even a holder that allows
delete (as virus scanners usually do). This confirms RT-1 (b).

Exclusive create:
- `FileMode.CreateNew` on an existing name fails: "The file … already exists" (0x80070050).
- It also fails for a name differing only in case (`EXISTS` against `exists`), and where a folder
  has the name ("Access … denied").
- Making a folder where a folder or a file already has the name fails ("already exists").
- Caution seen: .NET's `Directory.CreateDirectory` returns success on an existing folder, so it is
  **not** exclusive. Go's `os.Mkdir` / `os.Root.Mkdir` are expected to fail on an existing name
  (they call `CreateDirectoryW`), but that was not run here; MA-1127's or R1's tests should prove it.

## What this changes, per condition

| Condition | Evidence | Effect on the design |
|---|---|---|
| RT-3 (T1, T2) | Placed mbox files found, counted, summaries built, content right | **Holds.** No NO-GO. |
| T3 | `.R2` and `.R2.sbd` hidden, untouched | Holds: dot names are ignored (Windows; macOS still MA-1091). |
| T4 / RT-1 escape (c) | Lone `Q.sbd\C` not shown, not adopted, not removed; S built in the escape's order showed fully once `S` existed | **Supports escape (c).** The "Thunderbird adopts a lone `.sbd`" fallback in RT-1 is not needed on Windows. Not tested: Thunderbird *running* while the tree is written (the design keeps it closed, so this is out of scope). |
| RT-1 (b) | Folder rename fails with any open file inside | **Confirmed**; escape (c), with no folder rename, is the right choice. |
| RT-1 exclusive create | `CreateNew` and folder create fail on existing and case-variant names | Supports steps 1 and 3 of escape (c). Use a call that is exclusive (not .NET-style `CreateDirectory`). |
| RT-2 (Windows) / T5 | Held lock keeps Thunderbird out; clean release and kill both leave nothing stale | **Holds.** New: Thunderbird's own words are "already running, but is not responding" with a **Close Thunderbird** button. Pressing it closed only the new Thunderbird, not the holder. §7.5's words for a person who opens Thunderbird during a restore should expect this dialog (for example: "If Thunderbird says it is already running, choose Cancel and open it again when the restore finishes"). Lead's call on the exact words; this is a wording change, not a mechanism change. |
| MA-1093 method | Save As always drops the first header and rewrites line endings | **Changes the lab method, not the design.** "Each message saved from Thunderbird as `.eml` matches the original's SHA-256" cannot pass on 153.6.0. Replace with: compare the restored mbox's own bytes, re-parsed, with the originals (the RT-12 check), and use a Thunderbird copy into another folder as the "Thunderbird read it faithfully" check (shown byte-exact here). |
| Profile with no Local Folders (new) | A profile with no account has no Local Folders account and no folder | §3.2 must handle `mail.accountmanager.localfoldersserver` being unset: refuse with words, or name the step ("open Thunderbird once and set it up"). Not known: whether setting up only an IMAP account creates Local Folders (not run). |
| T8 / T9 | Thunderbird wrote both `directory` and `directory-rel`, and `storeContractID` explicitly | Names confirmed as §3.2/§3.3 use them; which one wins when they disagree is still MA-1095. |
| RT-11 | No Store Thunderbird on this VM | Not advanced; MA-1097 still open. |

Not done in this spike: the macOS `fcntl` lock leg named in RT-3 (outside this brief), any run
with `mailarchive.exe` itself, a Thunderbird with real accounts, compaction, and messages over
4 GiB.

## Where things are

- Throwaway folder left in place on the VM: `C:\Users\Public\tb-restore-lab\` (the lab profile in
  `profile\`, saved `.eml` files, `holdlock.log`, the RT-1 folders in `rt1\`).
- Lab scripts (not committed): `/Users/alex.dimarco/repositories/.winlab/restorelab/*.ps1` and
  the mailbox maker `.claude/restore-lab/make_mboxes.py` in the main checkout; source hashes in
  `.winlab/restorelab/src/sha256.txt`.
- Screenshots (not committed), `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab/`:
  `02` account setup on first start; `06` Local Folders baseline; `08`–`09` the tree with P, R1,
  S and their C folders; `10` R1's list; `11` m1 body; `12` m2 body; `13` m3 with attachment;
  `16` tree with R3/R4; `17` R3; `18` R4; `20` Trash copy; `21` P › C; `22` S › C;
  `23` the lock dialog; `25` normal start after release. `14-save-dialog.png` shows the names of
  folders in the VM user's Documents folder and `01`, `15`, `23` show the VM desktop: keep these
  private.

---

- Date worked on: 6 November 2026 (VM clock, UTC−5; the Mac session's date agrees).
- Drawn on: the review's RT-1, RT-2, RT-3, RT-11 and "smallest check"; the design's lab-row table;
  what the VM showed and the files it held. Not drawn on: Thunderbird's source or bug tracker
  (why Save As drops the first line is not known from here; it was observed, not explained).
- Register: a report of a lab run (findings), with recommendations marked as the lead's call.
- Smallest check for a reader: open `C:\Users\Public\tb-restore-lab\saved-m4.eml` on the VM — its
  first line is `From:`, while `.winlab/restorelab/src/orig/m4.eml` starts with `Return-Path:`.
- Counters. Register: report. Borrowed lines: Thunderbird's dialog and error texts, quoted.
  Drift: none noticed. Touched: this file (new) and one commit; on the VM only the throwaway
  folder. Persona: none. Source reached: review and design — yes; the VM — yes; Thunderbird
  source — no.
