# Thunderbird completeness check: say how much Thunderbird has downloaded

Design note, 2026-11-07, branch `tb-completeness` (off main 0923ffa, v1.6.1). No product code.

**BUILD STATUS: proposed (pre-code).** Owes the 10-lens pre-code design review
(`../assurance-kit/process/design-review.md`), to be filed as
`docs/review-thunderbird-completeness-predesign.md`, before any slice is built. The encrypted
archive allow-list change (§9.2) also owes an adversarial pass, and every shippable slice owes the
friction review (Windows first, on Edge).

**Ids are provisional.** Main's last test-spec row is MA-1150; another agent is adding polish rows
from MA-1151 now. This design takes **MA-1170 to MA-1194**, **R42 to R44** and **S75 to S77**,
and renumbers on merge if any are taken.

Every claim below is labelled **[proven]** (read in code, Thunderbird's source or a real
profile), **[conditional: …]** (the condition is stated next to it) or **[unverified]** (to be
settled by a lab row in §12).

---

## 1. Why

On 2026-11-05 the owner's first Gmail backup through Thunderbird saved about 1,670 messages, and
MailArchive called the run complete. At the same time, Thunderbird's own folder list
(`folderCache.json`) showed these totals:

- All Mail 238,080
- Important 56,611
- Sent 33,816
- INBOX 10,799
- Starred 6,803

Every folder was set to keep mail on this computer (flag 0x08000000), but only 3 folders had a
mailbox file yet. Thunderbird was still downloading.

What the code does today [proven]:

- **The backup reads only what is on disk.** It walks the local mbox files:
  `internal/source/localwalk.go:457-500` is the mbox reader and `:506-609` the folder walk. It
  never asks what Thunderbird says the server holds. Nothing in the Go code reads
  `folderCache.json`, `totalMsgs` or the `.msf` summary files. A `.msf` file is only checked for
  existence, at `internal/source/source.go:77` and `:151` and at `internal/presync/quiet.go:321`
  and `:362`.
- **It counts per run, not per folder.** The reader counts messages in each folder (local `n`,
  `localwalk.go:472-493`), but only for the "partly read" error text (`:474`), and records
  nothing. The run records one total: `internal/app/run.go:1421` (`lr.Exported`).
- **Mail never downloaded is invisible.** The existing "not on this computer in full yet"
  sentence (`internal/state/outcomewords.go:176-179`) covers only messages that are in a
  mailbox file but cut short. A message with no copy on disk at all is seen by nothing.
- **The words claim "complete":**
  - The GUI wizard says "Download complete — starting export…" after the store stops growing for
    45 seconds (`internal/wizard/wizard.go:1617`, `:1636-1637`).
  - It then says "Export complete." (`wizard.go:1894`).
  - Run now says "Backup finished: …" (`internal/wizard/jobrun.go:102-153`).
- **The pre-backup sync counts a pause as done.** It waits until the mail files have been
  unchanged for 3 minutes (`defQuiet`, `internal/presync/api.go:99`; rule at
  `internal/presync/quiet.go:221-232`). `Outcome.Synced()` counts "still fetching at 20 minutes"
  (`OutcomeNotQuiet`) and "the person's own Thunderbird was open" as synced
  (`internal/presync/attended.go:55-65`).
- **Thunderbird itself pauses for long stretches.** Its background download runs mostly while
  the computer is idle (10 seconds without input). When the person is using the computer, it
  only fetches new messages of up to 60 KB at a time. See `nsAutoSyncManager.h:29-41` and
  `:127-128`, and `nsAutoSyncManager.cpp:1264-1278`, all at comm-central revision ab60cc5e
  (§16) [proven, in Thunderbird's source]. So a pause in the files growing means nothing on its
  own. Gmail's throttling pauses are a second cause the lead observed [unverified: no Gmail
  limit figure is asserted here].

**A second finding changes the method.** On 2026-11-07 the owner's profile was read again
(read-only, counts and flags only; Thunderbird was running). All Mail was now nearly all on disk:
238,201 message separators against Thunderbird's total of 238,169. But **Sent Mail's mailbox
file held 2 messages against a total of 33,822**, and two label folders that are set to keep mail
had no mailbox file at all (33 and 772 messages).

These folders are not behind. Gmail stores one message under many labels, and Thunderbird
downloads each Gmail message's body **once**, into whichever folder reaches it first. Every other
label folder then skips it:

- `nsImapMailFolder::GetOfflineMsgFolder` looks up the message's X-GM-LABELS and X-GM-MSGID in
  the other folders (`nsImapMailFolder.cpp:8906-9004`).
- The download queue skips any message `HasMsgOffline` finds anywhere
  (`nsAutoSyncState.cpp:254-259`, `:335-339`).

[proven, in source; matches the profile]

So a folder-by-folder check would have told the owner, falsely, "Sent Mail: 2 of 33,822
downloaded". **For Gmail, completeness can only be judged per account, never per folder** (§3).

## 2. The data source

### 2.1 Decision: read `folderCache.json`, not the `.msf` files

Each profile has one `folderCache.json` at its top level. It is a JSON object.

- **Each key** is the full path of one folder's `.msf` summary file. For an account's own top
  level, the key is the account folder itself. The key comes from `GetFolderCacheKey`
  (`nsMsgDBFolder.cpp:1170-1186`): the "persistent descriptor", which is the native absolute
  path. On Windows that means backslashes, for example
  `C:\Users\Public\tb-restore-lab2\fresh-imap\ImapMail\imap.example.invalid\INBOX.msf` in the
  VM lab profile. On macOS it is a POSIX path. [proven]
- **Each value** is an object of numbers and strings, written by
  `nsMsgDBFolder::WriteToFolderCacheElem` (`nsMsgDBFolder.cpp:1232-1250`) and, for IMAP,
  `nsImapMailFolder::WriteToFolderCacheElem` (`nsImapMailFolder.cpp:1925-1953`). [proven]

The fields this design uses:

| Field | Meaning | Source |
|---|---|---|
| `flags` | folder flags. Offline (keep on this computer) = 0x08000000; Virtual (saved search) = 0x20; ImapNoselect = 0x01000000; Trash = 0x100; Junk = 0x40000000; AllMail = 0x8000; Inbox = 0x1000; SentMail = 0x200 | `nsMsgFolderFlags.idl:41,64,66,72,78,96,102,108` |
| `totalMsgs` | messages in the folder's local summary: one header row per message Thunderbird knows about | `nsMsgDBFolder.cpp:1242`; read from the summary at `:681` |
| `pendingMsgs` | messages the server reported whose headers are not fetched yet | `:1245`; `ChangeNumPendingTotalMessages` `:4062-4078` |
| `serverTotal` | the server's own count at Thunderbird's last STATUS/SELECT (IMAP only) | `nsImapMailFolder.cpp:1936`; set at `:2691-2699` |
| `lastSyncTimeInSec` | when Thunderbird's background download last checked this folder for messages without a body (Unix seconds; IMAP only) | `nsImapMailFolder.cpp:1943-1950` |
| `folderSize` | the summary's sum of message sizes, which for IMAP means the **server** sizes. It is **not** the size of the file on disk (Sent Mail: folderSize 12.2 GB, file 228 KB) | `nsMsgDBFolder.cpp:1247`; owner profile |
| `onlineName` | the folder's name on the server, for example `[Gmail]/All Mail` | `nsImapMailFolder.cpp:1934` |

The total Thunderbird shows a person is `totalMsgs + pendingMsgs`
(`nsMsgDBFolder::GetTotalMessages`, `nsMsgDBFolder.cpp:4009`) [proven]. This design calls that
figure **"listed"**: what Thunderbird lists for the folder.

**Why not the `.msf` files.** A `.msf` is a Mork database. Each message's "body kept on this
computer" bit (`nsMsgMessageFlags::Offline` = 0x80, `nsMsgMessageFlags.idl:47`) lives there. That
bit would give an exact per-message answer, even for Gmail. But reading it means writing a parser
for a quirky, undocumented format, and reading files Thunderbird rewrites while it runs. The
folder cache already carries the totals, as a small JSON file written atomically. Rejected for
this build; §14 Q8 keeps it open as a later precision step.

### 2.2 What "listed" means for each kind of account

- **IMAP** (including Gmail and an Outlook/Microsoft 365 account added to Thunderbird over IMAP):
  - Headers sync quickly and fully. Bodies lag behind.
  - In the owner's profile, `totalMsgs` equals `serverTotal` on every IMAP folder, including
    Sent Mail, whose bodies are elsewhere [proven, owner profile 2026-11-07].
  - So "listed" means the server's count as of Thunderbird's last check, not what is on disk.
  - Deleted messages: under Thunderbird's "mark as deleted" model they stay on the server and in
    the summary until expunged, so both sides count them alike. Gmail accounts move deleted mail
    to Trash. [conditional: the delete model is not read; the tolerance in §4.3 absorbs small
    differences]
  - Rule: listed = max(`totalMsgs` + `pendingMsgs`, `serverTotal`). The max covers headers still
    being fetched. One Outlook folder never opened showed `totalMsgs` 5 with `serverTotal` 0, and
    the max keeps 5.
- **POP3, Local Folders, Feeds:** all of it already lives on this computer. There is no server
  count, and `totalMsgs` describes the local file. **Not checked.** The words say nothing for
  these accounts; the record notes `not_applicable`.
- **Newsgroups:** not archived; not checked.
- **Thunderbird's native Exchange (EWS) accounts:** [unverified: their store and folder-cache
  fields are not read]. They are treated as `unknown` (§4.4) until a lab row reads one.

### 2.3 How fresh `folderCache.json` is

- **While Thunderbird runs**, any change schedules a save one minute later: `kSaveDelayMs`
  (`nsMsgFolderCache.h:42`), `SetModified` (`nsMsgFolderCache.cpp:349-361`). Thunderbird also
  saves when it closes: `~nsMsgFolderCache` → `Flush` (`:243-262`). [proven, in source]
- **The write is atomic.** It goes to a temporary file, which is then moved into place
  (`SaveFolderCache`, `:306-327`). MailArchive never reads half a file. [proven, in source]
- **The file's modified time says nothing.** An idle Thunderbird does not rewrite it. The
  owner's file was 2 hours old with Thunderbird running and nothing changing [proven, owner
  profile].
- **What MailArchive uses instead:** the newest `lastSyncTimeInSec` across the account's kept
  folders. The words call this "Thunderbird's last check" ("as of").

When Thunderbird's last check is more than 3 days old (the same 3 days as
`SyncStaleAfter`, `internal/state/syncstreak.go:32`), the words add: "These numbers are from
Thunderbird's last check of the server on DATE; open Thunderbird so it can check again."

The verdict still stands. A server that listed Y messages then still had them, unless the person
has since deleted mail on the server, and the tolerance and the word "about" cover that.

**When the file is missing, unreadable, larger than 16 MB, not a JSON object, or has no key
under this account's folder:** the verdict for that account is `unknown` (§4.4). This covers
three known causes:

- an old Thunderbird that still uses `panacea.dat`;
- a profile Thunderbird has never opened;
- a future Thunderbird with its new "Panorama" storage, which refuses to create the folder
  cache at all (`nsMsgFolderCache.cpp:184-187`, `nsMsgDBFolder.cpp:1152-1155`).

### 2.4 Counting what is on disk

**Decision:** the backup's own walk counts each folder's messages as it reads them. This costs
nothing extra, because every run already reads every message of every mbox file. The count is
taken at `localwalk.go:493` (after `n++`), **before** any manifest skip ("already done") or date
filter, so an incremental run counts the same as a first run.

Two adjustments:

1. **Deleted-but-not-compacted messages are left out of the count.** These are messages whose
   `X-Mozilla-Status` has the Expunged bit (0x0008, `nsMsgMessageFlags.idl:29`). The mbox file
   keeps them until Thunderbird compacts it. For example, the owner's INBOX has 10,887 separators
   against 10,881 listed. Today the reader only looks at the Read bit (`mozillaReadFlag`,
   `internal/source/mbox.go:280`). This build changes only the count, not what is archived
   (§14 Q7).
2. **For Gmail, the count is distinct messages across the whole account** (§3), by Message-ID.
   A message with no Message-ID counts once each. This needs a set of 16-byte hashes: about 4 MB
   for 238,000 messages.

Maildir stores (`storeContractID` maildirstore, `internal/tbrestore/storekind.go:26-28`) count
one file per message the same way. A `-copy-first` run counts its copy, but maps it back to the
original store's profile for the folder cache.

`status`, the Desktop and alerts show **the last backup's numbers**, from its record. They do not
re-read Thunderbird's files. This keeps "on disk" and "in the backup" the same figure, and makes
`status` cost nothing. Each figure carries its "as of".

### 2.5 What the real profiles showed

All reads were read-only and limited to counts and flags. The owner's Mac profile has two IMAP
accounts plus Local Folders. Personal label names are not reproduced.

| Folder (owner, Gmail) | kept on this computer | listed | in its own mbox | Reading |
|---|---|---|---|---|
| All Mail | yes | 238,169 | 238,201 separators | caught up (its count includes deleted messages not yet compacted) |
| Important | yes | 56,625 | 56,585 | — |
| Sent Mail | yes | 33,822 | 2 | **bodies are in All Mail (single-copy rule)** |
| INBOX | yes | 10,881 | 10,887 separators | — |
| two labels | yes | 33; 772 | no file | single-copy rule |
| Spam | **no** (Junk) | 126 | no file | Thunderbird's default |
| Trash | **no** | 0 | — | Thunderbird's default |

- **Outlook account:** every normal folder is kept, and on-disk counts are within a few messages
  of listed. Junk Email (234) is not kept, by default.
- **VM lab profiles** (`C:\Users\Public\tb-restore-lab*\`, 19 folder caches): same format.
  - Windows keys use backslashes, and non-ASCII names are JSON-escaped (for example `\u2014`).
  - A Local Folders entry has no `serverTotal` or `lastSyncTimeInSec`.
  - A profile set up but never connected (`fresh-imap`) shows INBOX with listed 0 and no
    `lastSyncTimeInSec`. That is the "never checked" case (§4.4).

[proven]

## 3. Gmail

**Detecting Gmail:** prefs.js `mail.server.serverN.is_gmail` is true
(`nsImapIncomingServer.cpp:399`; the owner's account has it), **or** any folder of the account
carries the AllMail flag 0x8000. Folder names are never used, because Gmail localises them
("[Google Mail]", "Alle Nachrichten").

**The "of Y" figure must not overstate.** The lead's brief suggested "the largest of All Mail vs
the sum". This design keeps the "largest" half and drops the sum, because the sum overstates.
INBOX, Important, Sent, Starred and every label all hold messages that are also in All Mail. In
the owner's account, all folders added up come to 352,176, which is 1.48 times All Mail's
238,169. Gmail keeps everything except Spam and Trash in All Mail, so All Mail is already the
union of the labels.

**Rule:** Y = max(All Mail's listed, the largest single other kept folder's listed), plus Spam and
Trash only if they are kept (they sit outside All Mail).

- The max guards against an All Mail whose count is stale or smaller than one label's.
- **If All Mail is not visible over IMAP** (a Gmail setting), or not kept on this computer: Y is
  the largest single kept folder, and the words say **"at least"**. The true distinct count is
  then unknowable from these files, and "at least" never overstates.
- **X** = distinct messages on disk across every mailbox file of the account (§2.4). Never one
  folder's count, because of the single-copy rule.
- The record also stores X and Y per folder. **For Gmail they are never shown as "behind"**, only
  used for the account figure.

**A fidelity side-finding, out of scope.** Because of the single-copy rule, the archive's "Sent
Mail" folder for this Gmail account holds 2 messages; the sent mail is archived under All Mail
(or wherever its copy landed). A person looking for sent mail in the archive would be misled. This
is a label-membership question, not completeness (§14 Q6).

## 4. The check

### 4.1 Per account (one Thunderbird store = one account, `ImapMail/<host>`)

1. Find the profile (`thunderbird.FindProfileDir`, `internal/thunderbird/thunderbird.go:29`) and
   the account (`FindAccountForStore`, `:81-116`). For IMAP stores only
   (`IsImapStore`, `:22-25`).
2. Read `folderCache.json`:
   - It is untrusted input: size-capped (16 MB), parsed once, never written.
   - Its keys are compared as strings only, never opened.
   - Match each key to a folder the walk read by its path **relative to the store folder**,
     comparing `\` and `/` alike and case-insensitively on Windows and macOS. A profile that was
     moved or restored (the keys still name the old place) therefore still matches, as long as
     the keys share the store's tail (`ImapMail/<host>/…`).
   - Keys outside the store are ignored.
3. Sort the account's folders into groups:
   - **kept:** the Offline flag is set, and the folder is not Virtual and not ImapNoselect.
   - **not kept, by default:** Trash or Junk without the Offline flag. Thunderbird never sets the
     flag for these (`nsImapMailFolder.cpp:1005-1018`).
   - **not kept:** any other folder without the Offline flag whose listed count is above 0.
   - **ignored:** virtual, noselect, the account's root, and listed 0.
4. Read the limits that make a gap deliberate, from prefs.js with the defaults of
   `mailnews.js:547-548,625`:
   - `autosync_max_age_days` > 0: keep only recent mail
     (`nsAutoSyncManager.cpp:107-125`; pref name `nsMsgIncomingServer.cpp:1429`);
   - `limit_offline_message_size` true with `max_size` KB
     (`nsMsgIncomingServer.cpp:1413-1415`).
5. Compute X and Y: per kept folder (non-Gmail), or per account (§3).
6. Give the verdict (§4.2).

### 4.2 Verdicts

| Verdict | When | Says "complete"? |
|---|---|---|
| `caught_up` | every kept folder has been checked (`lastSyncTimeInSec` > 0) and the missing count is within the tolerance (§4.3) | it may say "Thunderbird has downloaded everything it lists for this account" — never "all your mail" |
| `behind` | missing > tolerance | **no** |
| `limited` | behind, but a limit from §4.1 step 4 is on (so the gap may be by design) | no; says which setting |
| `unknown` | no usable folder cache (§2.3), or a kept folder never checked (no `lastSyncTimeInSec`) | no; neutral words ("Backup finished") |
| `not_applicable` | POP3 / Local Folders / anything not under `ImapMail/` | the wording used today |

"Not kept" folders are reported **alongside** any verdict (§6), never folded into "behind". A
folder that is not kept will never download, so calling it "behind" would make a backup look
unfinished forever.

### 4.3 Tolerance

**Recommendation:** missing ≤ max(20 messages, 0.2% of Y) counts as caught up. For Y = 238,080
that is 476 messages.

It absorbs mail that arrived while the backup ran, deletions on the server since Thunderbird's
last check, and messages with no body (Thunderbird skips zero-byte messages:
`nsAutoSyncState.cpp:271-275`). It is small enough that a real first-download gap (1,670 of
238,080) is never caught up. The figure is an owner question (§14 Q1).

### 4.4 What MailArchive says when it cannot tell

`unknown` is never `caught_up`. An IMAP account whose state is unknown gets the neutral
"Backup finished", never "complete", and one note in Status & health:

> MailArchive could not read Thunderbird's list of folders for imap.gmail.com, so it cannot tell
> whether Thunderbird has downloaded all of it.

This is a note, not a warning, because the backup itself is fine.

A kept folder never checked gets:

> Thunderbird has not checked the server for this account yet, so MailArchive cannot tell how
> much is still to come. Open Thunderbird and leave it open.

## 5. Outcomes, alerts and the streak

**Recommendation (owner question Q2): a behind backup is a success with a note.**

- The record status stays `ok`. The alert streak (`internal/backupalert/streak.go:132-187`) is
  not moved, and nothing is marked partial (`internal/app/gaps.go:225`, `:276-285` unchanged).
- The backup did save everything that is on this computer. Calling it failed would teach people
  to ignore failures for the days a first download takes.
- **The words change everywhere** (§7): the lead becomes "Backed up what Thunderbird has so far".
- `status` posture: **WARN** `thunderbird_behind` from the first behind run (§14 Q3), with its
  remedy. A `limited` account gets WARN `thunderbird_limited`. A not-kept folder gets WARN
  `thunderbird_not_kept`. A default-not-kept folder gets only a note. All of this sits under the
  existing `warn`/`note` helpers (`internal/health/health.go:366-382`).

**The "still downloading" notice.** It fires when the account has been `behind` across runs
spanning **3 days or more, with at least 2 runs**. This reuses `SyncStaleAfter`/`SyncMinRuns`
(`internal/state/syncstreak.go:30-38`), measured between runs and never compared with the clock,
so a computer that was switched off trips nothing. It is sent **once per behind-streak** as an
attention email through `backupalert` (the existing alert settings decide whether email is on),
and shown as a banner on the archive card. The streak ends at the first `caught_up` run, which
sends no recovery email: nothing failed.

**The "stopped downloading" notice.** It fires when X has not grown across behind runs spanning 3
days. It takes the same email slot, with stronger words and a remedy (§7). It is computed from the
run history's compact per-account `{verdict, x, y}` (§9.1). It needs no new streak file for
plaintext archives; encrypted archives use §9.2.

## 6. Folders not kept on this computer

These words are shown in the end summary, Status & health, `status` and the archive card (the card
shows a count and "see Status & health"):

> These folders are not kept on this computer, so MailArchive cannot back them up: Projects (772
> messages), Receipts (33 messages). To keep them: in Thunderbird, right-click each folder →
> Properties → Synchronization → tick "Select this folder for offline use", then click Download
> Now.

The labels "Synchronization", "Select this folder for offline use" and "Download Now" are
Thunderbird's own words (`mail/locales/en-US/messenger/folderprops.ftl:63-71`) [proven].

When every folder of an account is not kept:

> Thunderbird is not set to keep this account's mail on this computer. In Thunderbird: Account
> Settings → this account → Synchronization & Storage → tick "Keep messages for this account on
> this computer".

[unverified: the checkbox label and section name are not yet read in Thunderbird's source; lab
row MA-1191]

Today's wizard already offers to turn the account setting on (`thunderbird.EnableOffline`,
`thunderbird.go:170`, called at `wizard.go:1594-1598` and `cmd/mailarchive/main.go:2133`). Keep
that. **Do not** edit per-folder flags. They live in Thunderbird's own databases, and MailArchive
has no business writing those (§14 Q5).

A `limited` account:

> Thunderbird is set to keep only the most recent 30 days of mail for imap.example.com on this
> computer, so older mail is not backed up. To back up all of it: in Thunderbird, Account
> Settings → Synchronization & Storage → "Synchronize all messages locally regardless of age".

The size limit names "Messages larger than … KB". Both labels are Thunderbird's own
(`am-offline.dtd`, `allAutosync.label`, `offlineNotDownload.label`) [proven].

Spam and Trash (by default), shown as a note only:

> Thunderbird does not keep Spam and Trash on this computer (its usual setting), so they are not
> backed up.

## 7. Surfaces and words

The same sentence is used on every surface, built once in `internal/state` beside
`NotDownloadedSentence` (`outcomewords.go:176`), following design-plain-failure-words rule 1. An
account is named by its server (`imap.gmail.com`), never by its email address.

**Headline sentences:**

- **behind (Gmail):**
  > Thunderbird has downloaded about 1,670 of 238,080 messages in imap.gmail.com so far. Leave
  > Thunderbird open (it downloads mostly while the computer is left alone); later backups add the
  > rest.
- **behind (other IMAP):** the same, then the folders furthest behind: "INBOX 1,200 of 10,799;
  Sent 0 of 3,402".
- **at least:**
  > … about 1,670 of at least 56,611 messages …
- **stale:** add "(Thunderbird's count from its last check of the server on Nov 2)".
- **caught up:**
  > Thunderbird has downloaded everything it lists for imap.gmail.com (238,169 messages).
- **still downloading:**
  > Thunderbird has been downloading imap.gmail.com for 3 days and has about 120,000 of 238,080
  > messages so far. Leave it open; later backups add the rest.
- **stopped:**
  > Thunderbird has not downloaded any more of imap.gmail.com since Nov 4 (about 1,670 of 238,080).
  > Open Thunderbird and leave it open with the computer idle for a while. If nothing changes,
  > check Account Settings → Synchronization & Storage.

**Where each surface changes:**

| Surface | Code today | Change |
|---|---|---|
| GUI wizard wait | `wizard.go:1617-1641` ("Download complete — starting export…") | when it stops growing: "The download has paused — starting the backup of what Thunderbird has so far…" (the end summary then says how much) |
| GUI wizard end summary | `exportSummary`, `wizard.go:1890-1952` ("Export complete.") | lead becomes "Backup finished." when unknown, "Backed up what Thunderbird has so far." when behind or limited, and "Export complete." only for `caught_up` or non-IMAP; then the headline, not-kept and limited lines |
| Desktop Run now note | `runNowSummary`, `internal/wizard/jobrun.go:102-153` | the same lead and lines |
| Desktop archive card | `ArchiveRow`, `internal/desktop/archives.go:31-76`, banner like `sourceLostSince` `:241-250` | a one-line banner for behind, still-downloading or stopped; a not-kept count |
| Recent backups | `recentRunRows`, `internal/desktop/runhistory.go:26-85` | a behind run's status is "OK — Thunderbird still downloading", detail = headline |
| Status & health / `mailarchive status` | `health.Assess` `health.go:415`; Last run line `:2348-2383` | WARN `thunderbird_behind` / `thunderbird_limited` / `thunderbird_not_kept`; notes `thunderbird_unknown`, `thunderbird_default_not_kept`; each WARN names its remedy (X6) |
| `status -json` | `JSONReport` `health.go:2514-2573` | **added** `thunderbird_completeness: [{account, verdict, on_disk, listed, listed_floor, as_of, behind_since, not_kept:[{folder, listed}], default_not_kept, limit:{max_age_days, max_size_kb}}]`; new `reason_codes` above; nothing renamed |
| Alert email | `backupalert`, `internal/alert/alert.go:164-165` | still-downloading and stopped notices only (once per streak); counts and server name, **no folder names** (§9.3) |
| Last-run / run-history records | `LastRun` `internal/state/lastrun.go:71-148`; `RunHistoryEntry` `internal/state/runhistory.go:35-58` | **added** `thunderbird` (full, last run) and `tb` (compact `{account, verdict, x, y}`, history) |
| Command line (stderr) | end of run | the headline lines; exit 0 unchanged for behind (X1) |

## 8. Pre-backup sync (presync)

**Should "quiet" need the gap closed too?** **Recommendation (Q4): not as a gate, yes as a
reason to keep waiting.**

- A first Gmail download can take days. Holding a nightly backup until it finishes would mean no
  backup at all. What is on disk is still worth saving.
- **Change:** at the end of the quiet window, presync reads the folder cache. If the account is
  `behind`, **and** mail arrived during this session, the session does not end at quiet. It keeps
  waiting up to the existing cap (`defMaxWait` 20 minutes, `api.go:100`), then ends as today's
  `not-quiet`.
- If the account is `caught_up` or `unknown`, the quiet rule is unchanged.
- The cap, the S1-S9 state machine (`internal/presync/session.go:131`), the ownership rules (it
  quits only a Thunderbird it started) and `Synced()` are all unchanged. Whether Thunderbird
  synced and whether it is caught up are separate questions with separate words.
- A presync that finds the person's own Thunderbird open leaves it alone, as today. That is
  exactly what "Leave Thunderbird open" asks for.

## 9. Privacy

### 9.1 Plaintext archives

- The record and `status -json` carry per-account counts, the server name, and the names of
  **not kept** folders.
- Only the per-account X and Y go into the run history.
- Folder names already appear in a plaintext archive's own pages, so naming them in its status
  adds nothing. Personal label names are never read for any other purpose.

### 9.2 Encrypted archives

Today an encrypted archive's plaintext last-run record holds only version, status, times, error
kind, program and encrypted (`encryptedLastRun`, `internal/state/encrypted_record.go:141-149`).
The run history keeps only when, status and kind (`minimal()`, `internal/state/runhistory.go:61-63`).
Presync data is dropped entirely (`internal/app/run.go:1379-1385`; `internal/app/syncstreak.go:38-41`).

**Proposal (Q3b, owes the adversarial pass):**

- Add **one allow-listed boolean**, `tb_behind`, to `encryptedLastRun` and to the minimal history
  entry. Its fixed sentence: "Thunderbird has not downloaded all of this mail to this computer
  yet; later backups add the rest."
- There is a precedent: `NoteIndexBehind`, the one kind a successful run records
  (`encrypted_record.go:45-48`). A separate field is proposed rather than reusing `error_kind`,
  because a run can be both index-behind and Thunderbird-behind.
- The days-behind streak is counted from the minimal history (`when` + `tb_behind`). No counts,
  no server name and no folder names are written beside an encrypted archive.
- The numbers appear only on screen: the end-of-run summary and the Run now note, held in memory
  like the "in-memory error text" of design-plain-failure-words §3.
- The leak test (`internal/assure/leak/tree.go:31-44`) gains this field and asserts nothing else.

### 9.3 Email

An email leaves the computer, so alert emails carry the server name and counts, **never folder
names**. An encrypted archive's email carries only the fixed sentence.

## 10. Profile paths per OS

The design reuses the backup's own store discovery. The account is the store folder, and the
profile is `FindProfileDir(store)`, which walks up to `prefs.js` (`thunderbird.go:29-42`). The
folder cache is `<profile>/folderCache.json` on every OS. Store discovery today
(`internal/app/run.go:1881-1902`):

| OS | Profiles folder |
|---|---|
| Windows | `%APPDATA%\Thunderbird\Profiles\*\` (restore uses `FOLDERID_RoamingAppData`, `internal/tbrestore/profiles_windows.go:14-19`) |
| macOS | `~/Library/Thunderbird/Profiles/*/` |
| Linux | `~/.thunderbird/*/`, Snap `~/snap/thunderbird/common/.thunderbird/*/`, `~/.mozilla-thunderbird/*/` |

**Finding:** the backup's list has **no Flatpak path**, while restore's has one
(`~/.var/app/org.mozilla.Thunderbird/.thunderbird`, `internal/tbrestore/profiles.go:428-437`). A
Flatpak user's backup does not find Thunderbird automatically today. Recommend one shared list,
as a small separate fix (§13 C0b), so the two lists stop disagreeing.

An explicit `-input` store works on every OS, because the profile is found by walking up.

## 11. Failure behaviour and limits

| Case | Behaviour | Fails toward |
|---|---|---|
| folder cache missing, unreadable, too large, malformed | `unknown`, a note, neutral words | never "complete" |
| keys name another place (a moved or restored profile) | matched by the tail under the store; none match → `unknown` | never "complete" |
| Thunderbird still running mid-read | counts are "about"; the next run catches up | — |
| a folder not subscribed in Thunderbird | not in the folder cache; invisible (a stated limit) | silent [limit] |
| `-since` date filter | the count is taken before the filter (§2.4) | correct |
| new Thunderbird storage ("Panorama") or a field renamed | `unknown` | never "complete"; a lab re-check per Thunderbird ESR (§12) |
| a run that fails for another reason | the completeness record is not written; the failure words win | the existing precedence |

No new standing service is added, and no network use. The new dependency is a private Thunderbird
file format. It is pinned by fixtures and re-checked in the lab at each Thunderbird ESR.

## 12. Tests, catalog rows, lab rows (provisional ids)

**Invariants:**

- **R42**: A Thunderbird IMAP backup never says it is complete while Thunderbird's own counts show
  mail not yet downloaded; when MailArchive cannot tell, it does not say complete.
- **R43**: The "of Y" figure never overstates what Thunderbird lists. No sum over Gmail labels; a
  floor is worded "at least".
- **R44**: Completeness counts, server names and folder names never appear beside an encrypted
  archive; only the `tb_behind` boolean.

**Scenarios:**

- **S75**: Thunderbird still downloading at backup time.
- **S76**: a folder not kept on this computer.
- **S77**: the folder cache missing, or written by a different Thunderbird.

**Fixtures** (synthetic, under `internal/tbcomplete/testdata/`): `folderCache.json` files in Mac
and Windows key shapes, plus small mbox files. Never the owner's file. The owner-shaped Gmail
fixture repeats only the numbers' *shape*, with invented folder names.

| ID | Tier | Asserts | Covers |
|---|---|---|---|
| MA-1170 | U | the reader parses flags, listed (`totalMsgs`+`pendingMsgs`, max with `serverTotal`), `lastSyncTimeInSec` per folder; matches keys by the tail under the store for POSIX and backslash keys and escaped names; never opens a key path | R42, S75 |
| MA-1171 | U | missing / unreadable / over 16 MB / non-object / no matching key → `unknown`, never `caught_up`; positive twin on a healthy fixture | R42, S77 |
| MA-1172 | U | sorting: kept = Offline and not Virtual and not Noselect; Trash/Junk without Offline → default-not-kept; others with listed > 0 → not kept | S76 |
| MA-1173 | U | the walk's per-folder count excludes Expunged (0x0008) and is taken before the manifest skip and the date filter (an mbox with one expunged and one already-archived message) | R42 |
| MA-1174 | U | non-Gmail verdict: exactly at the tolerance → `caught_up`; one over → `behind` | R42 |
| MA-1175 | U | Gmail: Y = max(All Mail, largest kept folder) + kept Spam/Trash, never the sum; X distinct by Message-ID; Sent Mail 2 of 33,822 on disk with All Mail full → `caught_up`, no folder called behind | R43 |
| MA-1176 | U | Gmail with All Mail hidden or not kept → the floor, the words "at least" | R43 |
| MA-1177 | U | `autosync_max_age_days` > 0 or the size limit → `limited`, with the setting's own words | R42 |
| MA-1178 | U | a kept folder with no `lastSyncTimeInSec` (the fresh-imap shape) → `unknown`, "has not checked the server yet" | R42 |
| MA-1179 | U | newest check older than 3 days → the "as of DATE" words; verdict unchanged | R42 |
| MA-1180 | U | a behind run: record `ok`, alert streak not moved, not partial; record carries `thunderbird`; history carries `tb` | R18, R42 |
| MA-1181 | U | `exportSummary`, `runNowSummary` and the GUI wait text never say "complete" for behind or unknown IMAP; they do for `caught_up` and non-IMAP | R42 |
| MA-1182 | U | Desktop: card banner, Recent backups "OK — Thunderbird still downloading", Status & health WARN with remedy | R42, X6 |
| MA-1183 | U | `status` text and `-json` additive `thunderbird_completeness`; a record without it shows nothing (not unknown); reason codes added, none renamed | X5, X8 |
| MA-1184 | U | still-downloading notice at ≥3 days between runs with ≥2 runs, once per streak; stopped notice when X has not grown in 3 days; a caught-up run ends the streak with no recovery email | R42 |
| MA-1185 | U | not-kept folders named with the fix words (plaintext); counted only (encrypted, on screen) | S76 |
| MA-1186 | U | encrypted: the plaintext record and history carry only `tb_behind`; leak tree test finds no count, server or folder name | R44 |
| MA-1187 | U | presync: behind and arrivals → waits to the cap; caught up → ends at quiet; `Synced()` unchanged | R42 |
| MA-1188 | U | alert email: counts and server, no folder names; encrypted: the fixed sentence | R44 |
| MA-1189 | L | **pending** (lab, Windows first): the Parallels VM, a fresh throwaway profile under `C:\Users\Public\tb-completeness-lab\` with a throwaway IMAP account that holds enough mail to take an hour; back up mid-download (behind, numbers within tolerance of Thunderbird's own), again after (caught up); then macOS | S75 |
| MA-1190 | L | **pending**: the owner's Mac profile, read-only: `mailarchive status` after a backup shows imap.gmail.com caught up and no false Sent Mail gap; reports only counts | R43 |
| MA-1191 | L | **pending**: Thunderbird ESR on Windows and macOS: the fix words' menu paths and labels match (§6, including the unverified "Keep messages for this account on this computer") | S76 |
| MA-1192 | L | **pending**: the folder cache is rewritten within about a minute of a change while Thunderbird runs, and at quit (Windows, macOS) | S75 |
| MA-1193 | L | **pending**: Linux: no real-Thunderbird lab (headless Docker has no GUI); covered by U rows; lab story: a desktop Linux VM when one exists | S75 |
| MA-1194 | U | one shared Thunderbird profile-root list used by backup and restore, including Flatpak (C0b) | — |

**Process:**

- Every fix ships prove-fail then prove-pass (mutate, watch red, revert).
- Refusal-shaped tests use `assure.Refused`/`assure.Reached` (`internal/assure/`).
- The covers-map meta-test (`assureblock/`) must stay green on an unfiltered `go test ./...`.

## 13. Build slices and their gates

| Slice | What | Gate before merge |
|---|---|---|
| C0 | this design + catalog rows as pending encoders | **10-lens pre-code review** filed; owner answers to §14 |
| C0b | shared profile-root list with Flatpak (MA-1194) | unit + Linux Docker |
| C1 | new leaf package `internal/tbcomplete`: reader, sorting, Gmail math, tolerance, verdict (MA-1170–1179); pure, no I/O but one file read | test-first; mutation sweep |
| C2 | the walk's per-folder counter + Gmail distinct set (MA-1173); the run calls `tbcomplete` per IMAP store; record and history fields (MA-1180) | unfiltered suite |
| C3 | words and surfaces: wizard, Run now, Desktop, status, json (MA-1181–1183, 1185) | **friction review** (Windows on Edge first, then Mac) |
| C4 | encrypted `tb_behind` allow-list (MA-1186, 1188) | **adversarial pass** (allow-list change) |
| C5 | still-downloading and stopped notices + email (MA-1184) | friction review of the email |
| C6 | presync keeps waiting while behind (MA-1187) | presync's own re-review (it touches a reviewed state machine's wait rule) + the macOS and Windows presync lab rows |
| L | MA-1189–1193 | before release notes claim it |

Order: C1 → C2 → C3 is the minimum shippable unit; the backup stops claiming "complete" at C3.
C4 must land with or before C3 if encrypted archives get any words beyond today's.

## 14. Owner questions (each with a recommendation)

- **Q1. How close counts as "caught up"?** Recommend: within 20 messages or 0.2% of the total,
  whichever is larger (476 for 238,080). It covers mail arriving mid-backup and recent
  deletions; a real first download is never mistaken for done.
- **Q2. Does a behind backup count as a success for alerts and streaks?** Recommend: **yes, a
  success with a note.** Everything on this computer was saved. Separately, a "still
  downloading" email after 3 days behind, and a "stopped downloading" email if nothing new
  arrives for 3 days, each once per streak.
- **Q3. Status & health while behind: WARN or just a note?** Recommend: **WARN from the first
  behind backup.** The backup does not have the person's mail yet; WARN is truthful and still
  not red.
  - **Q3b.** For encrypted archives, allow the single "still downloading" yes/no beside the
    archive? Recommend **yes.** It tells nothing about the mail and follows the "index behind"
    precedent. Numbers stay on screen only.
- **Q4. Should the pre-backup sync wait for Thunderbird to catch up?** Recommend: **no gate, but
  keep waiting up to the existing 20-minute cap while mail is still arriving.** A first Gmail
  download can take days; a backup of what is there beats no backup.
- **Q5. Should MailArchive tick "keep on this computer" for a folder itself?** Recommend: **no.**
  Folder settings live in Thunderbird's own databases. Tell the person the 3 clicks instead.
  The existing account-level offer in the wizard stays.
- **Q6. Gmail's "Sent Mail" in the archive is nearly empty because of Thunderbird's single-copy
  rule.** Recommend: a **separate follow-up design** (label membership), not this build. Say it
  in this build's release notes.
- **Q7. Messages deleted in Thunderbird but not yet compacted are archived today.** Recommend:
  **leave as is for now** (the archive is a record of what was in the mail) and only leave them
  out of the count. Decide separately if it matters.
- **Q8. Later precision: read the `.msf` per-message "kept" bit for an exact Gmail answer?**
  Recommend: **not now.** The folder cache answers the question; revisit only if lab rows show
  the approximation misleads.

## 15. What the 10-lens review should attack

1. **Purpose:** does any word over-claim? Especially "downloaded everything it lists" when
   Thunderbird's own list could be stale or partial (unsubscribed folders, §11).
2. **Focus:** is C6 (presync) worth bundling? Should C5 (notices) wait for real-world data?
3. **Durability:** the record and history fields growing (50-entry history × accounts); the
   behind-streak never ending for an account that is `limited`; the format changing at a
   Thunderbird ESR; Panorama.
4. **Usability:** walk a first-time Gmail user on Windows through a week of hourly backups: how
   many WARNs and emails, and do the words stay true each day? The not-kept wording for a person
   with 40 labels.
5. **Seams:** the counter's placement before the manifest skip and the date filter (§2.4);
   `-copy-first` mapping; reuse of `SyncStaleAfter`, `backupalert`, `health.warn`; the build
   order C1 → C2 → C3 → C4.
6. **Security:** folder-cache parsing as untrusted input (size cap, keys never opened, path
   matching that cannot be steered outside the store); the encrypted allow-list addition; folder
   names in plaintext outputs and email.
7. **Failure domains:** every cause of `unknown`; does anything fail toward "complete"? A crash
   between the walk and the record.
8. **Migration:** old records without the fields; an older MailArchive reading newer records
   (additive JSON); turning the feature off if it misleads (one switch in `tbcomplete`?).
9. **Dependency:** a private Thunderbird file is now load-bearing. What happens when a field
   moves? Who re-checks, and when?
10. **Honesty:** every [unverified] here (EWS accounts, the account-level checkbox label, Gmail
    throttling, the Linux lab) carries its condition next to the claim, and the release notes
    must not outrun the lab rows.

Specific claims to try to break:

- the Gmail rule (§3) when All Mail is hidden, localised, or smaller than a label;
- the tolerance at both ends;
- the "as of" logic when Thunderbird has not run for weeks;
- whether `serverTotal` can exceed listed for long and make a caught-up account look behind.

## 16. Sources

- **Thunderbird source:** comm-central via the GitHub mirror `mozilla/releases-comm-central`,
  revision **ab60cc5ea7300498991dfaaa84c33204bdb482e4** (committed 2026-11-07). Files and lines
  as cited:
  - `mailnews/base/src/nsMsgFolderCache.{cpp,h}`
  - `mailnews/base/src/nsMsgDBFolder.cpp`
  - `mailnews/base/src/nsMsgIncomingServer.cpp`
  - `mailnews/base/public/nsMsgFolderFlags.idl`
  - `mailnews/base/public/nsMsgMessageFlags.idl`
  - `mailnews/imap/src/nsImapMailFolder.cpp`
  - `mailnews/imap/src/nsImapIncomingServer.cpp`
  - `mailnews/imap/src/nsAutoSyncManager.{cpp,h}`
  - `mailnews/imap/src/nsAutoSyncState.cpp`
  - `mailnews/mailnews.js`
  - `mail/locales/en-US/messenger/folderprops.ftl`
  - `mail/locales/en-US/chrome/messenger/am-offline.dtd`
- **The owner's Mac profile:** read-only on 2026-11-07, counts and flags only, through a
  throwaway script that printed standard folder names and replaced personal labels with numbers.
  Read: `folderCache.json`; the offline, Gmail and store keys of `prefs.js`; mbox file sizes and
  separator counts. No message content, subject or address was read into this document, and the
  file was not copied.
- **VM lab profiles:** `C:\Users\Public\tb-restore-lab*\` (throwaway; never the VM user's own
  profile), read 2026-11-07.
- **MailArchive code:** main 0923ffa, at the lines cited.
