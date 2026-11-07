# Restore to Thunderbird — slice R8 Windows lab, 7 November 2026

The Windows lab rows of slice R8 (design §13, §12.3; `docs/scenario-catalog.md`), run by an agent
on the Parallels VM "Windows 11", VM clock 08:26–09:26 (UTC−5), 7 November 2026, against the
branch tip **eb65a44**, built as `packaging/build-release.sh` builds the Windows pair
(`GOOS=windows GOARCH=amd64 go build ./cmd/mailarchive`, and `mailarchivew.exe` with
`-ldflags -H=windowsgui`). Both the command line (`mailarchive restore-thunderbird`) and Desktop
(through the lab's own Edge, real key and mouse events over its DevTools port) were used.

**Thunderbird: 153.6.0** (the exe's product and file version, `C:\Program Files\Mozilla
Thunderbird\`) for every row. No Microsoft Store Thunderbird is installed. Windows Defender
real-time protection was **on** for the whole session (`Get-MpComputerStatus`:
`RealTimeProtectionEnabled True`, tamper protection on). OneDrive is not signed in on the VM.

**Verdict.** Ten rows ran. Seven pass on Windows: MA-1090, MA-1092, MA-1093, MA-1121, MA-1135,
MA-1137, and MA-1134's Windows re-run with MailArchive as the holder. **MA-1095 fails** on a
product bug (BUG-1, high): Thunderbird does not read a restored folder whose name holds `#` or
`;` or is longer than 55 characters, so that folder shows as empty, and a whole restore is hidden
when the archive's own name is longer than 44 characters. MA-1091 now has its foldcheck2 P1
answers, but it still waits on macOS, Linux and 115 ESR. MA-1097 and MA-1136 ran only in part.
One more bug was found while running MA-1097 (BUG-2, medium): a profile folder that cannot take
the lock file is refused with "Thunderbird is open".

*Addendum, same day:* both bugs are fixed in 2b229ad, and **MA-1095 re-ran and passes on Windows**.
See "Addendum … MA-1095 re-run after the fix" below.

## Set-up

- Throwaway world only, under `C:\Users\Public\tb-restore-lab3\`. The lab's programs ran with
  `USERPROFILE`, `APPDATA` and `LOCALAPPDATA` pointed at `…\tb-restore-lab3\home`, as the R7
  friction review did. They had their own `-config`, their own Edge profile (`--inprivate`,
  DevTools port 9342), a `rundll32` stand-in, and their own `profiles.ini`. The VM user's
  profiles and MailArchive settings were never opened.
- Every lab profile is a copy of MA-1133's `tb-restore-lab2\fresh-news`. Its Local Folders was
  made by Thunderbird through a made-up newsgroup account (`news.example.invalid`). No real mail
  account was set up. Thunderbird always ran as `thunderbird.exe -no-remote -profile <lab path>`.
- Archives (all with originals kept):
  - **R8 Fixture**: a Thunderbird-style Local Folders plus a Gmail IMAP cache (`imap.gmail.com`:
    INBOX, Work, `[Gmail]/All Mail`, with g01 in all three, as Thunderbird syncs a two-label
    message). It was written by `mkfix.py` with exact bytes: CRLF and LF files; `From `, `>From `
    and `>>From ` body lines; a 2 KiB attachment; X-Mozilla-Status 0001, 0000 and none, and 0009
    (deleted); nested `Inbox/Receipts/2026`; `Входящие`, `受信トレイ`, `Déjà vu` (NFC). 19 records.
  - **R8 M365, R8 Names, R8 Names2, R8 Deep**: Microsoft 365 archives made by the real
    `app.RunGraph` against a local fake Graph server (lab tool `r8graph`; no real mailbox).
    - R8 M365: Inbox; Archive and `Archive/2025`; and "Line endings", which holds an LF
      message, a CRLF message with no final newline, and an LF one with none. 7 records.
    - R8 Names: 40 hostile folder names.
    - R8 Names2: names with `;`, `#`, and lengths 54, 55, 56 and 60.
    - R8 Deep: 10 nested levels of 40-character names.
  - **R8 Big**: one folder of 170 messages, each with a 19 MiB random attachment: **4,634,791,260
    bytes**. It was made on the VM's own NTFS disk by lab tool `r8big`, which recorded each
    message's SHA-256. The archive took 182 s to make and is 8.0 GB.
- The byte comparisons were done on the VM by lab tools `r8cmp` (mboxrd parse, matched by
  Message-ID) and `r8sum` (streams a file of any size).

## Rows

### MA-1090 — Thunderbird started during a restore — **PASS (Windows)**

A command-line restore of R8 Big into `p-big` was started. Ten seconds after `restores.log` said
"started", a lab Thunderbird was started on `p-big`. It did not open the profile. It showed
"**Close Thunderbird** — Thunderbird is already running, but is not responding. The old
Thunderbird process must be closed to open a new window." (`r8l-40`). Cancel was pressed. The
restore went on and finished: 170 of 170 restored and checked, 09:11:45–09:12:47. Thunderbird
started afterwards opened normally and showed **Restored — R8 Big (2) › Big, 170 messages**
(`r8l-41`, `r8l-42`). W-WAIT's words ("If you open it and it says Thunderbird is already running,
choose Cancel…") match what Thunderbird said.

### MA-1134, Windows half, with `mailarchive.exe` as the holder — **PASS (Windows)**

The run above is the release case: `mailarchive.exe` held `parent.lock`, Thunderbird stayed out,
and after the release it opened normally. The killed-holder case is MA-1135's first half below:
`mailarchive.exe` was force-ended mid-write, and Thunderbird started at once opened normally, with
no stale-lock message. The macOS and Linux legs keep their own record (they ran before slice R3).

### MA-1135 — killed mid-write, and a non-crash failure with Desktop running — **PASS (Windows)**

- **Killed.** The command-line restore of R8 Big was ended 8 s in with `Stop-Process -Force`
  (TerminateProcess, as Task Manager's End task does). Its last progress line was "39 of 170".
  On disk it left `Restored — R8 Big (3).sbd` holding the marker, the made-list and a partial
  `Big` of 1,293,156,352 bytes, with **no parent file**. A lab Thunderbird started straight after
  opened normally and **showed nothing of (3)**: only `Restored — R8 Big` and `(2)` (`r8l-44`).
  Afterwards the folder held no `.msf` for (3), inside the container or beside it.
- The next plan said "An earlier restore into this profile did not finish (7 November 2026,
  09:15). Its partial folder, which Thunderbird does not show, will be removed first." The next
  restore, run from Desktop, logged `cleaned … settled (case 3b)` and then made `(4)`.
- **Non-crash failures with Desktop still running** (lab Desktop and Edge):
  - (a) Between Check and Restore, PowerShell made an empty file under the planned name
    `Restored — R8 Big (5)`. Restore answered "This changed since you clicked Check: the name
    'Restored — R8 Big (5)' is now taken in Local Folders. Click Check again." Nothing was
    written.
  - (b) Restore was pressed, then **Cancel the restore** 10 s in, at "71 of 170". The result was
    "The restore was stopped, so nothing was added to Thunderbird. The part already written was
    removed. …", and `restores.log` said `ended … stopped`. Desktop was still running when a lab
    Thunderbird was started at once. It opened normally and showed no `(5)` (`r8l-45`).
- Not induced: a real I/O error part way through a write (for example a full disk).

### MA-1137 — a scheduled backup with pre-sync fires during a restore — **PASS (Windows)**

Two saved jobs with Thunderbird pre-sync on `p-big` (`thunderbird_sync.profile`), in the lab
config: `sched-same` backs up into R8 Big (the archive being restored from) and `sched-other`
into another archive. Both were started as a schedule starts them (`mailarchive run-job FILE`,
hidden, no terminal) 0.1 s after the restore's "started" line.

- `sched-same`: "This scheduled backup was skipped: a restore of this archive into Thunderbird
  started at 09:11 on 2026-11-07 and is still using this archive. The next scheduled backup runs
  as usual." No Thunderbird was started.
- `sched-other`: it **did not start Thunderbird**. Its run record says `presync: already-running`,
  and status says "Thunderbird sync at the last backup: Thunderbird was already open". Its backup
  ran in 0 s, left the restore's folders out, and ended `ok`.
- Control, with no restore running: the same `sched-same` job did start Thunderbird on `p-big`,
  waited its 20 minutes, quit it, and recorded `ok-noactivity`. So the pre-sync path itself works
  in this world, and only the restore stopped it.
- Low wording note (L3 below): the status line says Thunderbird "was already open" when it was a
  MailArchive restore holding the profile.

### MA-1093 — the restored fixture against Thunderbird — **PASS (Windows)**

- **Folders and counts.** R8 Fixture (19 messages, 13 folders) and R8 M365 (7 messages, 4 folders)
  were restored into `p-main` from the command line. Thunderbird showed every folder, including
  `Входящие`, `受信トレイ`, `Déjà vu` and `[Gmail] › All Mail`, and built a summary for each one
  opened (`r8l-10`, `r8l-11`, `r8l-13`). Its `folderCache.json` records:

  | Folder | Total | Unread |
  |---|---|---|
  | Inbox | 5 | 3 |
  | Receipts | 1 | 0 |
  | 2026 | 1 | 1 |
  | LF Folder | 3 | 2 |
  | Входящие | 1 | 1 |
  | 受信トレイ | 1 | 0 |
  | Déjà vu | 1 | 1 |
  | Gmail INBOX | 2 | 1 |
  | Work | 1 | 0 |
  | All Mail | 2 | 1 |
  | M365 Inbox | 2 | 2 |
  | Archive | 1 | 1 |
  | 2025 | 1 | 1 |
  | Line endings | 3 | 3 |

- **Read state (§4.6) holds.** Status 0001 is shown read; none or 0000 is shown unread; every
  Microsoft 365 message is unread, as the result line says. The **deleted-bit message** (f05,
  0009) is hidden: Inbox holds 6 messages and shows 5, with `expungedBytes 341`, f05's size. The
  result said "1 of them were deleted in Thunderbird before they were backed up, so Thunderbird
  keeps them hidden…".
- **The restored mbox's own bytes**, re-parsed by the mboxrd rule, equal the archive's kept
  originals: **26 of 26** messages (19 + 7). The two Microsoft 365 originals with no final newline
  are equal once the one added line ending is taken off, as the design says. The "Line endings"
  file frames each message with its own line ending (LF, CRLF, LF).
- **Thunderbird read it faithfully (the oracle).** Each restored top folder was copied whole with
  Thunderbird's own **folder context menu → Copy To → Local Folders → Oracle**. Thunderbird wrote
  every message again under its own `From - Sat Nov 07 13:40:23 2026` separators. Every copied
  message equals the archive's original byte for byte: **23 equal, plus the 2 no-final-newline
  ones equal after the added ending**. f05 was not copied, because Thunderbird keeps it hidden.
  Save As was not used.
- **What the archive's originals are.** For an mbox source, the archive kept CRLF for the LF
  file's messages, and it kept `>>From there` where the mbox meant `>From there`. This confirms
  the design's §16 side findings 4 and A on a real run. These are backup-side and outside the
  restore. LF messages therefore came only from the Microsoft 365 fixture, which keeps LF, and
  those were restored and copied byte-exact.
- **Profile growth (§6.3).** `p-main` was 8,449,111 bytes before, 8,459,912 after the Fixture
  restore, and 11,148,160 after Thunderbird's first session. That session also built its search
  database and made the oracle copies, so the 2.7 MB is an upper bound for indexing ~26 small
  messages. For the large case see MA-1121.

### MA-1092 — a deep tree under a long user name — **PASS (Windows)**

- **The setup.** R8 Deep (10 levels × 40 characters) was restored into a profile at
  `C:\Users\Public\tb-restore-lab3\LongUserNameAbcdefgh\AppData\Roaming\Thunderbird\Profiles\x7k2m9qa.default-release`.
  That is a 20-character "user" folder one level deeper than `C:\Users\<name>`, so it is harsher
  than a real one. Its Local Folders path is 133 characters.
- **The plan.** It shortened level 1 to `Level 01 folder with a long-is~e78d0883`. It named the other
  9 levels as "nested too deep for Windows to hold under this profile, so they are not restored",
  and the run exited 3. The deepest path written is 200 characters.
- **Thunderbird.** It opened the deepest restored folder and its message ("r8 deep level 01",
  `r8l-50`).
- **A shorter profile.** Into `p-main` (Local Folders 54 characters), 3 levels went back, each
  shortened. Thunderbird opened level 3 and its message (`r8l-52`).
- **The bound.** Within the 259/247 budget everything Thunderbird was given opened. Whether
  Thunderbird could open paths beyond 260 was not tried, because the restore never writes them.

### MA-1095 — reserved endings and names; `directory` against `directory-rel` — **FAIL (Windows), BUG-1**

- **The order of the settings: pass.** In profile `p-dirs`, `prefs.js` `directory` named
  `Mail\Abs LF`, `prefs.js` `directory-rel` named `[ProfD]Mail/Local Folders`, and `user.js`
  `directory-rel` named `[ProfD]Mail/UserJS LF`.
  - Thunderbird showed only `Seen-UserJS`, so it used `user.js`'s `directory-rel` (`r8l-55`).
  - MailArchive's plan chose `…\p-dirs\Mail\UserJS LF`. They agree.
  - Every copied profile also kept a stale absolute `directory`, which pointed at
    `tb-restore-lab2\fresh-news`, beside a `[ProfD]` `directory-rel`. Thunderbird and MailArchive
    both used the profile's own folder, and Thunderbird did not rewrite `directory`.
- **Thunderbird's skip list: pass.** In R8 Names (40 names), every name that Thunderbird would
  skip got its `_`, and Thunderbird showed each one with its 1 message:
  - the endings: `x.msf_`, `y.sbd_`, `z.snm_`, `w.toc_`, `v.mozmsgs_`;
  - the exact names: `msgFilterRules.dat_`, `rules.dat_`, `rulesbackup.dat_`, `popstate.dat_`,
    `sort.dat_`, `filterlog.html_`, `junklog.html_`, `mailfilt.log_`, `filters.js_`, the
    `feeds.json*_` and `feeditems.*_` names, and `FEEDS.RDF_`;
  - the patterns: `tilde~_`, `_feeditems_error1`;
  - devices: `_CON`, `_COM1`, `_COM¹`, `_CONIN$`, `_LPT9`, `_nul.txt`;
  - others: the leading dot as `dotted`; the trailing dot as `Trailing`; `Q1: Reports` as
    `Q1 Reports~612dd4a6`; the bidi override removed (`abtxt.exe`); `same` and `SAME` as `SAME`
    and `same (2)`; `LONGFO~1` as `LONGFO~1~ed5d0774` beside `Long folder name one`.

  See `r8l-31` to `r8l-33` and `folderCache.json`.
- **BUG-1 (HIGH): restored mail hidden.** Thunderbird shows these folders **empty** (0
  messages), though the restore said "Restored … MailArchive checked every message it wrote":
  - `_#hash` (from `#hash`; `r8l-34`), `mid#dle`, `semi;colon` (`r8l-35`);
  - a 56-character and a 60-character name. Names of 54 and 55 characters are shown with their
    message.

  For each one, Thunderbird made **its own** summary and folder under a hashed name beside the
  restored file: `mid3085bdd1.msf` with a directory `mid3085bdd1`, `semi04771469`,
  `ccc…cac643999`, `ddd…d245b4b29`, `_d4e62f63`. It reads the mail from that name, not from the
  file MailArchive wrote.

  The **top folder** is hit too. `-name "This is a long archive folder name for lab tests"` made
  `Restored — This is a long archive folder name for lab tests` (59 characters). Thunderbird showed
  it with **no subfolders**, and made `Restored — This is a long archive folder name f49c53148.msf`
  and a directory of that name beside it. So any archive folder whose name is over 44 characters
  restores into a folder Thunderbird shows empty.

  This matches Mozilla's folder-name hashing as I recall it: `NS_MsgHashIfNecessary`, which
  hashes a name holding `;` or `#` or longer than 55 characters. I recall this and did **not**
  read it in the source today, so it is the next thing to check. The design's §4.4 budget (246
  units) and `SafeName` do not cover it.
  - **Severity: high.** The mail is on disk, not lost, but Thunderbird does not show it, while the
    result says it went back.
  - Mail the person later files into such an "empty" folder would go into Thunderbird's hashed
    file. That was not tried.
  - The hashed entries Thunderbird adds inside a container are unlisted. Under the cleanup rule
    that means "remove nothing", which is the safe direction.

  **The row stops here; the code was not changed.**

### MA-1091 — dot names, the foldcheck2 P1 step, renames — **Windows answers recorded; row stays pending (macOS, Linux, 115 ESR)**

**The P1 set-up.** A finished restore `Restored — P1` (R8 M365) was made into throwaway profile
`p-p1`. With Thunderbird closed, its parent file was removed, and an empty `Restored — P1` was
made by PowerShell `New-Item`. Then Thunderbird was started. The readings:

1. **Does it show the tree?** Yes. `Restored — P1` showed with Archive (and 2025), Inbox and
   Line endings (`r8l-21`). So W-VISIBLE's and W-TAKEN's "shows" holds.
2. **Is mail moved into a shown subfolder appended to its mbox?** Yes. A message dragged from Inbox
   to Archive was appended to `Restored — P1.sbd\Archive`, which grew from 305 to 620 bytes. The
   hazard P1 guards against is real.
3. **Which `.msf` files does it make?** It made `Restored — P1.msf` beside the container, and,
   inside it, **only `Inbox.msf`, for the one folder opened**. There was none for Archive, even
   after a message was moved into it, and none for Line endings or 2025. Shown is not enough to
   leave the scan's sign; only opening a folder does. This answers the load-bearing reading:
   Thunderbird can show a tree and leave no `.msf` inside it. Per the design's §12.3 foldcheck3
   note, this goes to the lead as an open hazard for the scan's backstop.

**With a folder of that name.** In `p-p1b`, a directory named `Restored — P1` was made in place
of the file. Thunderbird showed **nothing** of the restore (`r8l-23`) and made no `.msf`.

**Dot-named entries.** The marker and the made-list were never shown and never got a `.msf`, in
any profile of this session. That repeats the friction review's reading.

**Rename and delete.**
- Renaming `Restored — P1` to `Renamed P1` in Thunderbird moved the container whole, with the
  marker and the made-list inside `Renamed P1.sbd`. A backup of `p-p1` after that still left it
  out: "Left out 1 folder that MailArchive is restoring, or has restored…".
- **Deleting** it in Thunderbird moved its mail to `Trash.sbd\Renamed P1…`, **without** the marker
  and made-list, which were left nowhere. The next backup took the 8 messages from Trash
  normally. That is expected, and it means a restored folder the person deletes is backed up from
  Trash like any other mail.

**The leftover `.sbd` with no parent file.** Thunderbird was opened on it: see MA-1135.

### MA-1121 — a folder over 4 GiB on NTFS, with Defender on — **PASS (Windows)**

R8 Big (one folder of 4.32 GiB, 170 messages) was restored four times into `p-big` with
real-time protection on:
- twice from the command line: 48 s, then 62 s, the second while two scheduled jobs and a
  Thunderbird start ran beside it;
- twice from Desktop: 42 s, then 40 s.

Each finished "done", and each logged `checked`. No retry or W-CHECKFAIL was seen.

Thunderbird opened the 4,634,794,830-byte folder and counted **170 messages**. It opened the last
one, which starts at byte 4,607,531,331, past 4 GiB: "Big message 170", "1 attachment:
big-170.bin 19.0 MB" (`r8l-43`). Streaming the restored file and hashing each message gave
**170 of 170 equal** to the SHA-256 recorded when the source was made.

Thunderbird's summary for it (`Big.msf`) records `folderSize 4634794830`. Profile growth from
indexing this folder could not be separated from the restores' own bytes, so it is not reported.

### MA-1136 — Defender, OneDrive, a taken file — **partly run; row stays pending**

- **Defender: clean results.** Defender real-time scanning was on through every placement, check
  reopen, parent create and cleanup of the session: 4 big restores, 9 small ones, a Cancel and a
  settle of a killed restore. Every result was clean. No sharing violation was seen, so the
  retry was not exercised.
- **A held file.** A hidden PowerShell "holder" polled every 5 ms to open the new folder's mbox
  with no sharing, as an antivirus might. It failed 2,277 times over 40 s, because the restore
  keeps its own handle through the write and the check. It got in 14 ms **after** `checked`, by
  which time the parent file was made.
  - So a file taken between the write and the check could not be staged from outside.
  - W-CHECKFAIL was **not** reached.
  - A hold that starts after the commit did not change the result, and did not disturb the next
    plan.
- **OneDrive: not run.** OneDrive is not signed in on the VM, and signing in needs a real account.
- **A real quarantine: not tried.** Putting an EICAR test file in a message would raise Defender
  alerts on the VM. That needs the owner's yes first, so it was left for the owner to decide.

### MA-1097 — Store Thunderbird, network AppData — **partly run; row stays pending**

- **Store Thunderbird: not run.** No Microsoft Store Thunderbird is installed, and installing one
  is outside this run's brief.
- **The plan on a network path: pass.** A throwaway profile on the Parallels share was planned as
  `P:\restorelab3\netprof` (a mapped network drive) and as `\\Mac\winlab\restorelab3\netprof`
  (UNC). Both plans said "Thunderbird's Local Folders (…) is on a network drive. The restore
  writes there over the network, so it may be slow." That is **W-NETWORK**. No W-NOIDENT
  appeared, so the identity could be read.
- **The restore itself: could not be run.** The share is read-only to the VM user ("Access is
  denied"), so no restore could be written there. That attempt found **BUG-2**.

### MA-1133 — **done earlier**

See `docs/lab/restore-thunderbird-windows-MA-1133.md`. Its Windows leg ran on 153.6.0, and the
macOS and Linux legs stay pending.

## Product bugs found (not fixed in this run)

**BUG-1, HIGH: names Thunderbird hashes.** A restored folder whose final name holds `;` or `#`, or
is longer than 55 characters, shows **empty** in Thunderbird 153.6.0 on Windows. Thunderbird
makes its own hashed-name summary and directory beside the file and does not read the file. The
same happens to the top folder `Restored — NAME` when NAME is longer than 44 characters, which
hides the whole restore. The result still says every message was restored and checked.

- Evidence: `p-names` `_#hash`; `p-names2` `semi;colon`, `mid#dle`, the 56- and 60-character
  names, and the top folder; `folderCache.json` entries with `total=0`; screenshots `r8l-34`,
  `r8l-35`, `r8l-36`.
- Where it sits: design §4.4 and §4.5 budgets; `tbrestore.SafeName` and `TopName`; MA-1088 and
  MA-1095.
- A suggested direction for the lead, **not tried**: treat `;` and `#` anywhere as needing a
  change, and cap each name at 55 characters (as Thunderbird counts them), keeping the hash
  suffix. First check the cap's unit and the character list against the Mozilla source.

**BUG-2, MEDIUM (friction; safe direction): the wrong refusal when the lock file cannot be made.**
On a profile folder where `parent.lock` cannot be created (here, a read-only network share), the
plan says "Thunderbird is closed." and Start then refuses with W-RUNNING: "Thunderbird is open,
so nothing was restored. Close Thunderbird …, then click Check again." No Thunderbird was
running, so Check again loops.

- Cause: `TakeProfileLock` (`internal/tbrestore/tblock.go`, the `openProfileLock` error branch)
  maps every open error to W-RUNNING.
- Nothing is written, so it is safe, but the words send the person to a dead end.
- Evidence: `rt-net-run.txt` and `rt-net-run2.txt`, with the process list showing no Thunderbird.

**Low wording notes.**
- L1: at a terminal, W-WAIT is printed twice, at the end of the plan and again as the run starts.
- L2: "1 of them were deleted in Thunderbird…"; this should be "was" for one.
- L3: after a pre-sync stopped by a restore, `status` says "Thunderbird was already open"
  (`already-running`), but it was MailArchive's restore holding the profile.

## Addendum, 7 November 2026 (VM clock 11:00–11:03, UTC−5): MA-1095 re-run after the fix — **PASS (Windows)**

BUG-1, BUG-2 and the wording notes L1–L3 were fixed in commit **2b229ad** (design §4.4 and §7.5
notes "Settled 2026-11-07 [MA-1095 lab]"). Thunderbird's rule was read in its source this time:
`NS_MsgHashIfNecessary` in comm-central `mailnews/base/src/nsMsgUtils.cpp` at releases-comm-central
`9ff8c8b16fba2d9f9668b416eb6f2d708494fe78`. It hashes a name holding `;`, `#`, `/:*?"<>|`, the path
separator or a control character, a name that starts with `.` or ends with `.`, `~` or a space, and
a name longer than `MAX_LEN = 55` UTF-16 units. That matches the run above.

**Set-up.** A new throwaway world, `C:\Users\Public\tb-restore-lab4\`, with `USERPROFILE`,
`APPDATA` and `LOCALAPPDATA` pointed at its `home`, as before.
- Profile `p-names3` is a copy of MA-1133's `fresh-news` (Local Folders made through a made-up
  newsgroup account; no real account). The VM user's own profiles were not opened.
- `mailarchive.exe` was built from 2b229ad as `packaging/build-release.sh` builds it
  (`GOOS=windows GOARCH=amd64 go build ./cmd/mailarchive`; SHA-256 `5097c500…37b9`).
- The archive was made by lab tool `r8graph4` (real `app.RunGraph`, fake Graph server). Its own
  folder name is **80 characters**: "R8 Names3 - an archive whose own folder name is far longer than
  sixty characters". It holds 20 folders, one message each:
  - `semi;colon`, `#hash`, `mid#dle`, `a;b#c`;
  - names of 54, 55 and 56 `a`/`b`/`c`, a 60-character and a 120-character name, and two
    60-character `y` names that differ only in the last character;
  - `Входящие`, `受信トレイ`, `Déjà vu`, 56 × `漢`, 27 × 📨 (54 units) and 28 × 📨 (56 units),
    `plain control`;
  - `#parent;one` with a 70-character child.

**The plan and the restore** (command line, `rt-plan.txt`, `rt-yes.txt`).
- The new folder is **'Restored — R8 Names3 - an archive whose o~eb6f9369'** (50 units, so 55 with
  " (99)").
- The plan said "14 folder names had to be changed to be safe as file names". That is the 13
  changed folders plus the top name.
- The result was "Restored 20 messages … MailArchive checked every message it wrote", exit 0.
- On disk, every name is at most 55 units and none holds `;` or `#`. For example: `_hash`,
  `semi_colon`, `mid_dle`, `a_b_c`, `cccc…c~ee59278c` (55), `Sixty character folder name for the
  MA-1095 re~279b48d1` (55), the two `y` names `…~1d01c1b7` and `…~1ee3e0c2`, `漢…~ee132b2f` (55),
  28 × 📨 as 23 × 📨`~f0f56c22` (55), `_parent_one` / `kkk…k~93d56084`. The 54- and 55-unit names
  and the Unicode ones under 55 were kept as they were.

**Thunderbird 153.6.0** (`thunderbird.exe -no-remote -profile …\p-names3`, pid 15280).
- It showed the top folder, which itself holds no mail ("0 Messages", as designed), and all 20
  folders under it.
- Each folder was opened in turn, by keyboard in the folder pane. **Every one showed "1 Message"**
  with its own message, including `_hash`, `semi_colon`, `mid_dle`, the 56-, 60-, 70- and
  120-character ones, CJK and both emoji names (`r8l4-03` to `r8l4-07`).
- After a graceful close (pid 15280 exited), each restored file had Thunderbird's summary **under
  its own name** (`NAME.msf` beside `NAME`). There was **no hashed-name `.msf` or folder anywhere**
  (the BUG-1 sign; `check.ps1` found no extra entry).
- `folderCache.json` gives `total=1 unread=1` for all 20, and `total=0` for the top folder's
  own file.
- So no restored folder is shown empty, and the whole restore is not hidden.

**BUG-2, the read-only share's lock words** (the Parallels share, read-only to the VM user).
- **No `parent.lock` in the profile** (`P:\restorelab4\netprof`). The plan went ahead ("Thunderbird
  is closed.", W-NETWORK). The restore then refused with W-LOCKPERM, exit 1: "MailArchive is not
  allowed to make or open Thunderbird's lock file (P:\restorelab4\netprof\parent.lock) in this
  profile's folder, so nothing was restored: … This happens when the profile is on a read-only
  network share, or its folder belongs to another account. Choose a profile on this computer's own
  drive that your account can change, then click Check again." Before the fix this was W-RUNNING.
- **A `parent.lock` left in the profile**, as a real profile has it (`netprof-lock`). The **plan**
  refused with the same W-LOCKPERM words. Before the fix, the plan's check counted any failure to
  open an existing `parent.lock` as held, so by the old code it would have said "Thunderbird is
  open". That is read from the code; the old build was not run on this case.
- Nothing was written in either profile. No Thunderbird or MailArchive process was left on the VM.

**Not re-run here:** L1–L3 have unit tests (MA-1129, MA-1124, MA-1140) and were not walked on the
VM. The macOS and Linux legs of MA-1095 were not run.

**Where things are.** The lab4 world is `C:\Users\Public\tb-restore-lab4\` (profiles `p-names3`,
`p-names3b` (unused), `home`, `Archives`, `src`). Programs and outputs are in `%LOCALAPPDATA%\rl4\`.
On the Mac:
- the scripts and the tool's source are in `/Users/alex.dimarco/repositories/.winlab/restorelab4/`;
- the screenshots are `r8l4-01` to `r8l4-07` in
  `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab-r8/` (not committed;
  `r8l4-01` shows the VM user's desktop, so keep it private).

## Not done here

- **Other platforms.** The macOS legs (MA-1091, MA-1098, MA-1134's re-run, MA-1135) wait,
  because the owner's own Thunderbird is running on the Mac and the restore correctly refuses
  while any Thunderbird runs. The Linux legs (MA-1099, MA-1134, MA-1135) wait for a Linux
  desktop, which this lab does not have. MA-1094 and MA-1096 were not in this brief.
- **The Windows-only unit legs** of RT-21 (MA-1120, MA-1087, MA-1143) were not run in this
  session. Another agent was running command-line tests on the VM.
- **Other Thunderbird versions.** Only 153.6.0 was used, not 115 ESR.

## Where things are

- **Throwaway folders left on the VM (29.1 GB):**
  - `C:\Users\Public\tb-restore-lab3\`: profiles `p-main`, `p-p1`, `p-p1b`, `p-names`,
    `p-names2`, `p-big` (four 4.3 GiB restores), `p-dirs` and
    `LongUserNameAbcdefgh\…\x7k2m9qa.default-release`; `home` (the lab's settings, jobs and
    `restores.log`); `Archives` (R8 Fixture, M365, Names, Names2, Deep, Big, SchedOther,
    BackupOfP1); `src`; `bigsrc` (the 4.3 GiB source and its `sums.txt`); `holder.log`.
  - `%LOCALAPPDATA%\rl3\`: programs and outputs. The `rt-*.txt`, `cmp-*.txt` and `bg-*.txt`
    files hold every command's words quoted here.
- **On the Mac:** `/Users/alex.dimarco/repositories/.winlab/restorelab3/` holds the lab scripts,
  the lab tools' source in `labfix-src/`, and `netprof/`. None of it is committed.
- **Screenshots** (not committed; some show the VM user's desktop, so keep them private):
  `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab-r8/r8l-*.png`.
  - `r8l-10` to `r8l-16`: the fixture tree and the oracle copies.
  - `r8l-20` to `r8l-26`: P1, rename and delete.
  - `r8l-30` to `r8l-36`: names.
  - `r8l-40` to `r8l-45`: the big folder, Thunderbird during a restore, after a kill and after a
    Cancel.
  - `r8l-50` to `r8l-52`: the deep tree.
  - `r8l-55`: directory against directory-rel.

---

- Date worked on: 7 November 2026 (the VM clock, UTC−5, and this session's date agree).
- **Drawn on:**
  - Design §3.2, §4.3–§4.7, §5, §8, §12.3 and §13's R8 notes.
  - The catalog rows named above.
  - The earlier labs (2026-11-06, MA-1133) and the R7 friction review.
  - `tblock.go`, `layout.go` and `inputs.go`, read for BUG-2 and the budgets.
  - What the VM showed and held.
- **Not drawn on:** Thunderbird's source. BUG-1's hashing rule is my recollection of Mozilla's
  code, consistent with every name tried here but **not checked in the source today**.
- Register: a report of a lab run (findings), with suggestions marked as such.
- **Smallest check for a reader:** on the VM, start Thunderbird with `-no-remote -profile
  C:\Users\Public\tb-restore-lab3\p-names2` and click `semi;colon`. It shows "0 Messages",
  though `…\Restored — R8 Names2.sbd\semi;colon` holds a 313-byte message.
- **Counters.**
  - Register: report.
  - Borrowed lines: MailArchive's and Thunderbird's own words, quoted.
  - Drift: once, a lab script was pointed at a profile that did not exist yet, and Thunderbird
    made a fresh one there. It was ended by its pid and the folder was removed and remade before
    use.
  - Touched: this file and the catalog rows; on the VM, only the throwaway folders.
  - Persona: none.
  - Source reached: design, catalog and code — yes; the VM — yes; Thunderbird source — no; macOS
    and Linux — no.

**For the addendum (MA-1095 re-run):**
- Date worked on: 7 November 2026 (the VM clock, UTC−5, and this session's date agree).
- **Drawn on:**
  - Thunderbird's source: `nsMsgUtils.cpp` at releases-comm-central 9ff8c8b16fba, and
    `nsCRTGlue.h` at mozilla-firefox/firefox 6149f492996. These were read; earlier the rule was
    only recalled.
  - The program built from 2b229ad.
  - What Thunderbird 153.6.0 showed, and what it wrote: `.msf` files and `folderCache.json`.
- Register: a report of a lab run (findings).
- **Smallest check for a reader:** on the VM, start Thunderbird with `-no-remote -profile
  C:\Users\Public\tb-restore-lab4\p-names3` and click `semi_colon` under "Restored — R8 Names3 …".
  It shows 1 message.
- **Counters.**
  - Register: report.
  - Borrowed lines: MailArchive's words and Thunderbird's source, quoted.
  - Drift: none noticed.
  - Touched: this file and the MA-1095 catalog row; on the VM, only `tb-restore-lab4` and
    `%LOCALAPPDATA%\rl4`; on the share, nothing.
  - Persona: none.
  - Source reached: Thunderbird source — yes; the VM — yes; macOS and Linux Thunderbird — no.

## Addendum 2, 7 November 2026 (VM clock 12:20–12:21, UTC−5): MA-1095 — names with `%` (ADV-N1) — **PASS (Windows)**

The names adversarial pass (`docs/review-restore-thunderbird-adversarial-names.md`, ADV-N1, high)
found from Thunderbird's source that it percent-decodes each file name before it works out the
folder's file, so a folder written as `a%41b` would be read from `aAb` and shown empty. The fix,
commit **bc29f75**, writes every `%` as `_`. This run checks it on a running Thunderbird.

**Set-up.** A new throwaway world, `C:\Users\Public\tb-restore-lab5\`, with `USERPROFILE`,
`APPDATA` and `LOCALAPPDATA` pointed at its `home`, as before.
- Profile `p-pct` is a copy of MA-1133's `fresh-news` (Local Folders made through a made-up
  newsgroup account; no real account). The VM user's own profiles were not opened.
- `mailarchive.exe` was built from bc29f75 as `packaging/build-release.sh` builds it
  (`GOOS=windows GOARCH=amd64 go build ./cmd/mailarchive`; SHA-256 `a6f600b7…86ea`).
- The source is a folder of four mbox files, written by the lab script with exact bytes:
  `a%41b` (2 messages), `Budget%2026`, `100%` and `Normal` (1 each). The archive, named
  **`Mail%20backup`**, was made from it by that program with `-raw` (originals kept): 5 records.

**The plan** (command line, `-plan -record-out`; `rt-plan.txt`, `record-plan.txt`). Its NAMES line,
word for word:

```
4 folder names had to be changed to be safe as file names; Thunderbird shows the changed names:
"Restored — Mail%20backup" → "Restored — Mail_20backup"
"100%" → "100_"
"Budget%2026" → "Budget_2026"
"a%41b" → "a_41b"
```

The saved record holds the same lines. `Normal` is not listed (not changed).

**The restore** (`-yes`): "Restored 5 messages into Thunderbird as the folder 'Restored —
Mail_20backup' under Local Folders. … MailArchive checked every message it wrote.", exit 0.

**Thunderbird 153.6.0** (`thunderbird.exe -no-remote -profile …\p-pct`, pid 9496).
- It showed `Restored — Mail_20backup` (0 messages, as designed) with `100_`, `a_41b`,
  `Budget_2026` and `Normal` under it (`r9-02-tree`).
- Each was opened in turn: `100_` **1 Message** ("pct hundred"), `a_41b` **2 Messages** ("pct
  a41b one", "pct a41b two"), `Budget_2026` **1 Message** ("pct budget"), `Normal` **1 Message**
  ("pct normal") (`r9-03` to `r9-06`).
- After a graceful close (pid 9496 exited), each restored file had Thunderbird's summary under
  its own name, and there was **no other `.msf` or folder** (no `aAb.msf`, no `Budget 26.msf`: the
  ADV-N1 sign; `check.ps1`). `folderCache.json`: `100_` 1, `a_41b` 2, `Budget_2026` 1, `Normal` 1,
  and 0 for the top folder's own file.
- No Thunderbird or MailArchive process was left on the VM.

**Noticed, not a failure:** the progress line names the folder as the archive spells it, so a
folder named `100%` reads "Restoring: 1 of 5 messages (100%)", which looks like a percentage.
For the lead (a wording question, not changed here).

**Not run here:** the same names before the fix (the old build was not run; ADV-N1's red is the
unit test against Thunderbird's decode, seen red on bb93bf4's code). macOS and Linux legs, and a
Mac OS Extended (HFS+) Local Folders for ADV-N5, were not run.

**Where things are.** The lab5 world is `C:\Users\Public\tb-restore-lab5\` (profile `p-pct`, `home`,
`Archives\Mail%20backup`, `src\mbox`). Programs and outputs are in `%LOCALAPPDATA%\rl5\`. On the
Mac, the scripts are in `/Users/alex.dimarco/repositories/.winlab/restorelab5/`; the screenshots
are `r9-01` to `r9-06` in `/Users/alex.dimarco/repositories/mail-archive-tool/.claude/restore-lab-r8/`
(not committed; they show the VM's desktop only through Thunderbird's window and the taskbar).

**For addendum 2:**
- Date worked on: 7 November 2026 (the VM clock, UTC−5, and this session's date agree).
- **Drawn on:** the program built from bc29f75; what Thunderbird 153.6.0 showed and wrote (`.msf`
  files, `folderCache.json`); Thunderbird's source as cited in design §4.4 "Settled 2026-11-07
  [ADV-N1]".
- Register: a report of a lab run (findings).
- **Smallest check for a reader:** on the VM, start Thunderbird with `-no-remote -profile
  C:\Users\Public\tb-restore-lab5\p-pct` and click `a_41b` under "Restored — Mail_20backup". It
  shows 2 messages.
- **Counters.** Register: report. Borrowed lines: MailArchive's words, quoted. Drift: the first
  two clicks missed because the lab's bring-to-front script pressed Alt and showed the menu bar;
  the folder was then opened by keyboard. Touched: this file and the MA-1095 catalog row; on the VM,
  only `tb-restore-lab5` and `%LOCALAPPDATA%\rl5`. Persona: none. Source reached: the VM — yes;
  macOS and Linux Thunderbird — no.
