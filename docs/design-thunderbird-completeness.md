# Thunderbird completeness check: say how much Thunderbird has downloaded

Design note, 2026-11-07, branch `tb-completeness` (off main 0923ffa, v1.6.1). No product code.

**BUILD STATUS: proposed (pre-code).** Owes the 10-lens pre-code design review
(`../assurance-kit/process/design-review.md`), to be filed as
`docs/review-thunderbird-completeness-predesign.md`, before any slice is built. The encrypted
archive allow-list change (§9.2) also owes an adversarial pass, and every shippable slice owes the
friction review (Windows first, on Edge).

*Superseded 2026-11-07 by the status line below. The review was filed as
`docs/review-thunderbird-completeness-10lens.md` (the caller's name), not `-predesign.md` (TC-24).*

**BUILD STATUS (2026-11-07, after the review): reviewed, conditions folded, pre-code.** The 10-lens
pre-code review is `docs/review-thunderbird-completeness-10lens.md` (commit 22330a8, verdict GO WITH
CONDITIONS, TC-1 to TC-24). Every condition is folded below as a dated note beside the text it amends;
the reviewed text above each note is kept as it was. The owner's defaults and the lead's calls are in
§0. Slice C4 (the encrypted yes/no) is withdrawn by owner default O1, and slice C6 (presync) is moved
to its own later design (TC-18). Still owed before code is merged: an independent fold-verify of this
file (the lead arranges it); the TC-14 source re-read at the Thunderbird ESR (slice C0, §13 fold); and
each slice's own gate (§13 fold). No owner ruling is open for C0c, C1a or C1b.

> **Folded 2026-11-07 [foldcheck M1 to M10] (lead's call):** the independent fold check is filed as
> `docs/review-thunderbird-completeness-foldcheck.md` (commit 92b80f8, verdict FAIL (narrow): six
> paths could still print "complete"). The lead accepted its must-fix list M1 to M10 as proposed, and
> each is folded below as a dated note tagged `[foldcheck Mn]` beside the text it amends. Reviewed
> text is kept; sentences it replaces are marked "superseded by [foldcheck Mn]". Also folded: Gmail
> is told by `is_gmail` only, for this design and gmail-dedup alike (lead's call, §3), and the §2.3
> citation slip. §17 has a closing note that re-reads the headline rule against the six paths.
>
> | Item | Folded in | Gate |
> |---|---|---|
> | M1 `prefs.js` through `util.ReadRegularFile` | §4.1 step 1, §12, §13 | before C0c |
> | M2 one key-matching rule | §4.1 step 2, §4.4, §11, §12 | before C1a |
> | M3 a broken `flags` counts as kept | §4.1 step 3, §4.4, §12 | before C1a |
> | M4 the source re-read at ESR 153, plus Release 159 | §0, §4.1 step 3, §6, §12, §13, §16 | before C1a merges |
> | M5 All Mail is the one folder with 0x8000 | §3, §12 | before C1b |
> | M6 `not_applicable` narrowed | §2.2, §4.1 step 1, §4.2, §4.4, §12 | before C1b |
> | M7 "complete" words need zero deleted-copy bytes | §2.4, §4.2, §11, §12, §17 | before C1b |
> | M8 the encrypted Run now note | §7, §12 | before C3 |
> | M9 no "everything" when stale; `stale` and `complete` in `-json` | §2.3, §7, §12 | before C3 |
> | M10 the behind streak's own record | §5, §12 | before C5 |
>
> **Still owed:** the fold checker's re-check of M1 to M10 only (not the whole file), and the
> M4 re-read itself, which is a C0 task the lead owns.
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call):** the one case §17's closing note left
> open is closed. An IMAP-looking store outside every profile and every `ImapMail` folder, with no
> profile found, is `unknown` ("can't tell") when it holds any `.msf` summary file. Folded in §2.2,
> §4.2, §4.4, §12 (MA-1196's custom-folder case) and §17. Gate: before C1b, with M6.
>
> **Folded 2026-11-07 [M4 re-read] (the lead's re-read, done by a delegated agent):** the M4 source
> re-read is done, at ESR 153.6.0esr (comm-esr153 c3fd7a609257) and Release 159.0 (comm-release
> 1e7f8bb744a4). Results: `docs/lab/thunderbird-completeness-source-esr153.md`. Every load-bearing
> claim holds except the account-settings labels: the design's words were wrong at both versions,
> and the new labels arrive in 160. Notes beside §4.1 steps 2 and 3, §6 and §16. The source half
> of C1a's gate is met; the lab rows are still owed.
>
> **Folded 2026-11-07 [completeness foldcheck2] (lead's call):** fold check 2 is filed as
> `docs/review-thunderbird-completeness-foldcheck2.md` (commit edf9e0b, verdict FAIL (narrow), one
> must-fix). The lead accepted its must-fix MF1 and its notes A to D, and added two calls of its own.
> Each is a dated note tagged `[completeness foldcheck2 …]` beside the text it amends. Reviewed text
> is kept; a sentence a note replaces is marked superseded. The tags L1 to L6 below are this fold's
> own; they are not the lead's calls L1 to L5 of §0.
>
> | Tag | What | Folded in | Gate |
> |---|---|---|---|
> | MF1 | the encrypted Run now note's "no Thunderbird IMAP store" escape; MA-1181's "non-IMAP" | §7, §12, §17 | before C3 |
> | L1 (note A) | `EnableOffline`'s own read and write of `prefs.js` | §4.1 step 1, §12, §13 | C0c |
> | L2 (note B) | refuse `.`, `..` and absolute key tails before M2's file check | §4.1 step 2, §12 | C1a (and its adversarial pass) |
> | L3 (note C) | a walked mailbox with no cache key never adds to Gmail's X | §4.1 step 2, §12 | C1b |
> | L4 (note D) | an account with a not-kept WARN may not say complete; the note-only cases stay open for C3 | §4.2, §12 | C3 |
> | L5 | the §6 labels follow the detected Thunderbird version, or quote both sets | §6 | C3 (MA-1191 still settles them) |
> | L6 | an independent narrow check of the M6-residual note and of this fold, before C3 | §13 | before C3 |

**Ids are provisional.** Main's last test-spec row is MA-1150; another agent is adding polish rows
from MA-1151 now. This design takes **MA-1170 to MA-1194**, **R42 to R44** and **S75 to S77**,
and renumbers on merge if any are taken.

> **Folded 2026-11-07 [ids] (lead's call):** this design keeps MA-1170 to MA-1194, R42 to R44 and
> S75 to S77. The Gmail de-duplication design (branch `gmail-dedup`, `docs/design-gmail-dedup.md`)
> takes MA-1210 to MA-1234, R45 and S78 to S84. The folds need 8 more rows, and they take
> **MA-1195 to MA-1202**, in the gap below gmail-dedup's range. On 2026-11-07 no row MA-1195 to
> MA-1209 appeared in main's docs or in the `polish-161`, `gmail-dedup`, `tb-presync-design` or
> `restore-thunderbird` worktrees. (Using MA-1195 and up is the folder's call, so the lead should
> confirm it.) MA-1187 leaves with slice C6 (TC-18) and is not reused here.

## 0. Rulings folded on 2026-11-07

**Owner ruling, 7 November 2026 (late): "ok to pics".** The owner accepted the recommended answers
below (O1–O3 and Q1–Q8 as recorded in this section); they are now the owner's rulings, not defaults.
Lead's calls on the C0c build and its folds (same date): an absolute or `.`/`..` key tail makes the
account "can't tell" (`invalid_entry`), accepted; C0c's stricter server-name fallback (the account's own
folder setting, when it has one, must name a folder of that name) is accepted as built; for C3, a
not-kept Spam or Trash folder, and a not-kept Gmail label folder, give a note only and do not block
"complete" (they are outside Y by §3), while any other not-kept folder blocks it.

**Owner defaults.** The owner has the review's recommendations. These are what gets built, and each
alternative must stay a small change:

- **O1 (TC-6, Q3b), encrypted archives:** completeness is shown on screen only, and only while the
  archive is unlocked here. Nothing is written beside the archive, and no completeness email is sent
  for an encrypted archive. Rule C-16 is unchanged. Slice C4 is withdrawn.
- **O2 (TC-4, Q1), tolerance:** max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), applied once per account.
  Per-folder shortfalls are counted separately (each clamped at 0) before they are added up. Nothing
  on disk is never caught up. Both numbers are shown whenever any mail is missing.
- **O3 (TC-1), Gmail with All Mail hidden:** "Thunderbird lists at least Y; MailArchive cannot tell
  whether that is all of it". Never complete.
- **Q2:** a behind backup is a success with a note. An email follows after 3 days behind, or 3 days
  stalled. (The folder reads this as each kind of email once per streak, per TC-20; §5 fold.)
- **Q3:** WARN in `status` from the first behind backup.
- **Q5:** MailArchive never ticks a Thunderbird setting itself.
- **Q6:** Gmail's near-empty Sent Mail is handled by the gmail-dedup design (membership read from the
  `.msf` summary files). Cross-referenced in §3 and §14.
- **Q7:** deleted-but-not-compacted messages are still archived, but left out of the count, with
  the TC-16 `expungedBytes` estimate.
- **Q8:** no `.msf` reading in this design.

**Lead's calls (owner-delegated):**

- **L1 (TC-18):** C6 (presync) moves out into its own later design.
- **L2:** the email notices (C5) ship right after C3's friction review.
- **L3 (TC-19):** WARN once, then a note, for a setting the person chose.
- **L4 (TC-3):** Panorama is detected by its file **and** by its setting; either one present →
  `unknown`.
- **L5 (TC-13):** the fix-it words are chosen by Thunderbird version, with a version-neutral fallback.
- **TC-14:** a named person re-checks the source claims at ESR 140 before release. This is a lab and
  doc task in slice C0 (§13 fold).
- **C0c:** the existing `FindAccountForStore` bug (`thunderbird.go:97-104`: two accounts on one
  server can give the wrong account) is fixed first, as its own small slice with its own test
  (MA-1195).

> **Folded 2026-11-07 [foldcheck M4] (lead's call):** the TC-14 bullet's "at ESR 140" is
> superseded by [foldcheck M4]. The re-read is at **ESR 153**, the ESR this repo's labs actually ran
> (153.6.0esr, §16 fold), and it also names the current Release seen in the lab (159.0 on the Mac).
> The owner of the re-check is **the lead**, and it is done before C1a merges.

**Where each condition is folded:**

| Id | Folded in |
|---|---|
| TC-1 | §3, §4.2, §4.4 |
| TC-2 | §2.1, §4.1 step 3 |
| TC-3 | §2.3, §4.1 step 2 |
| TC-4 | §4.3 |
| TC-5 | §2.2, §4.1 step 1 |
| TC-6 | §9.2 |
| TC-7 | §6 |
| TC-8 | §6 |
| TC-9 | §4.1 step 4 |
| TC-10 | §4.1 step 1, §7 |
| TC-11 | §2.4 |
| TC-12 | §2.3 |
| TC-13 | §6 |
| TC-14 | §16, §13 |
| TC-15 | §4.1 step 2 |
| TC-16 | §2.4 |
| TC-17 | §7 |
| TC-18 | §8, §13 |
| TC-19 | §5 |
| TC-20 | §5 |
| TC-21 | §2.4 |
| TC-22 | §3 |
| TC-23 | §3 |
| TC-24 | §1, §2.4, §9.2, the status line |

§12 and §13 carry the amended and new rows and the new slice table. §17 records the re-read for
contradictions.

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

  > **Folded 2026-11-07 [TC-24] (review condition):** `source.go:151` is an extension
  > **exclusion** (`isMailboxCandidate` leaves `.msf` out), not an existence check. `:77` is the
  > existence glob. The point stands: nothing reads a `.msf`.
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

> **Folded 2026-11-07 [TC-2, TC-12, TC-16] (review conditions):**
>
> - **Types, as Thunderbird writes them** [proven at trunk; the folder read the review's downloaded
>   copy of `nsMsgDBFolder.cpp:1240-1247` and `nsImapMailFolder.cpp:1932-1949`]:
>   - `flags` is an unsigned 32-bit number (`SetCachedUInt32`), so it is validated as 0 to
>     4,294,967,295. TC-2's "within int32" applies to the next group, not to `flags`.
>   - `totalMsgs`, `pendingMsgs`, `serverTotal` and `lastSyncTimeInSec` are signed 32-bit.
>   - `expungedBytes` and `folderSize` are signed 64-bit, and are written for **every** folder.
>   - Validation rules are in §4.1 step 3's fold.
> - **One more field, `expungedBytes`:** the bytes of messages that are deleted or moved but whose
>   copies stay in the offline file until compaction (`nsImapMailFolder.cpp:1371-1396`). Written at
>   `nsMsgDBFolder.cpp:1246`. Used by §2.4's TC-16 fold.
> - **What `lastSyncTimeInSec` means:** it is set when Thunderbird finishes a pass over the folder's
>   **local** headers, queuing bodies to download (`nsAutoSyncState.cpp:353-358`). It is not a check
>   of the server. The table's wording above fits; the words built on it change in §2.3's fold.

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

> **Folded 2026-11-07 [TC-5] (review condition):** the kind of account is decided by the
> `prefs.js` server `type` of the account the store belongs to (matched as in §4.1 step 1's fold),
> **never by whether the store sits under `ImapMail/`**. Thunderbird's native Exchange stores sit
> under `Mail/` beside Local Folders (`ExchangeProtocolInfo.cpp:34-36`, `nsMailDirProvider.cpp:17,43-44`),
> and an IMAP account can be given a custom folder outside `ImapMail/`.
>
> | `type` | Treated as |
> |---|---|
> | `imap` | checked (this section's IMAP rules), wherever its folder is |
> | `pop3`, `none` (Local Folders), `rss` | `not_applicable` (only these three) |
> | `ews`, any other type, an empty type | `unknown`, with neutral words |
> | no matched account, or `prefs.js` unreadable, for a store that has a folder-cache entry | `unknown`, with neutral words |
> | no matched account and no folder-cache entry (a plain mail folder that is not a Thunderbird store) | `not_applicable` |
>
> `IsImapStore` (`thunderbird.go:22-25`) is no longer used for this decision. Its comment that
> everything under `Mail/` "is always fully local" (`:19-21`, review side finding B) is wrong for
> Exchange accounts. C0c corrects the comment and leaves the function's other callers as they are.
> Rows: MA-1196 (U) and MA-1197 (lab, a real EWS profile's fields).
>
> **Folded 2026-11-07 [foldcheck M6] (lead's call):** the table's last two rows are **superseded by
> [foldcheck M6]**. As written, an IMAP store whose account could not be matched, and whose cache
> was missing or unreadable, reached `not_applicable` and kept "Export complete.". Whether a store
> "has a folder-cache entry" cannot be known when the cache cannot be read. The rule is now:
>
> | Case | Treated as |
> |---|---|
> | a **matched** account whose `type` is `pop3`, `none` (Local Folders) or `rss` | `not_applicable` |
> | no `prefs.js` is found above the input (`FindProfileDir` finds no profile) **and** no part of the input's path is `ImapMail` (compared as in §4.1 step 2's fold) | `not_applicable` (a plain mail folder, not a Thunderbird store) |
> | everything else that is not a matched `imap` account | `unknown`, with the words "MailArchive does not yet know how this kind of Thunderbird account keeps its mail", whether or not the cache could be read |
>
> "Everything else" includes: an ambiguous account (C0c returns none); a `prefs.js` that exists but
> cannot be read or is refused by the opener (M1); an old `panacea.dat` profile; a cache refused as a
> link, a pipe or over 16 MB; and a copied `ImapMail/<host>` with no profile above it. Each is
> `unknown`, never "Export complete.". MA-1196 gains these cases (§12 fold).
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call):** the M6 table's second row is
> **narrowed by [foldcheck M6 residual]**. As written, an IMAP store outside every profile and
> outside every `ImapMail` folder had no `prefs.js` above it and no `ImapMail` path part. Examples:
> an `-input` of an IMAP account's custom local folder kept outside its profile, or a copy of
> `ImapMail/<host>` renamed into another folder. So it reached `not_applicable` and kept "Export
> complete." (the case left open in §17's closing note). The second row is replaced by these two:
>
> | Case | Treated as |
> |---|---|
> | no profile is found above the store (`FindProfileDir` finds no `prefs.js`), **and** no part of the store's path is `ImapMail`, **and** the store holds **no** `.msf` summary file | `not_applicable` (a plain mail folder) |
> | no profile is found and no part of the store's path is `ImapMail`, but the store holds **any** `.msf` summary file | **`unknown`** ("can't tell"), never `not_applicable` |
>
> - **Why a `.msf` file is the sign.** Thunderbird finds an IMAP account's folders at start-up by
>   listing the store's `.msf` files, not its mailbox files (`nsImapMailFolder::CreateSubFolders`,
>   `nsImapMailFolder.cpp:404-508` at ESR 153.6.0esr, read in the M4 re-read,
>   `docs/lab/thunderbird-completeness-source-esr153.md` claim 9). So every folder Thunderbird knows
>   in an IMAP store has one, even a folder with no mailbox file yet (the owner's two label folders,
>   §1). A store with none is not one Thunderbird could be keeping.
> - **"Holds any `.msf`", precisely.** The store folder, or any `.sbd` folder the walk enters under
>   it, has a directory entry whose name ends in `.msf` in any letter case. Thunderbird compares the
>   ending case-blind (`:430-432`). The entry counts whatever kind it is (file, link or other). It is
>   seen by name only, from the listing the walk already makes, and is never opened or followed.
> - **"The store's path"** means the folder the walk treats as one store, not only the `-input`.
>   An `-input` of a parent folder can hold a store whose own path has the `ImapMail` part. M6's
>   "the input's path" is read the same way. (The folder's reading.)
> - **Words and reason.** The M6 row's words: "…because MailArchive does not yet know how this kind
>   of Thunderbird account keeps its mail". The reason is `no_match`, because no account was matched.
>   Words that name the missing profile instead would be a wording change only, for C3's friction
>   review.
> - **Cost.** A Local Folders or POP3 copy kept outside its profile also holds `.msf` files. It now
>   leads with "Backup finished." instead of "Export complete.", which fails safe. A folder of mbox
>   files with no `.msf`, no profile and no `ImapMail` part keeps today's words.
> - **Limit, stated.** An IMAP folder copied without its `.msf` files, and outside any `ImapMail`
>   folder, cannot be told from a plain mail folder, so it stays `not_applicable`. Nothing on disk
>   then says it came from Thunderbird.
> - Rows: MA-1196 (§12 fold).

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

> **Folded 2026-11-07 [foldcheck §6 slip] (citation):** `SyncStaleAfter` is defined at
> `internal/state/syncstreak.go:33`, not `:32` (`:31-32` is its comment).

The verdict still stands. A server that listed Y messages then still had them, unless the person
has since deleted mail on the server, and the tolerance and the word "about" cover that.

**When the file is missing, unreadable, larger than 16 MB, not a JSON object, or has no key
under this account's folder:** the verdict for that account is `unknown` (§4.4). This covers
three known causes:

- an old Thunderbird that still uses `panacea.dat`;
- a profile Thunderbird has never opened;
- a future Thunderbird with its new "Panorama" storage, which refuses to create the folder
  cache at all (`nsMsgFolderCache.cpp:184-187`, `nsMsgDBFolder.cpp:1152-1155`).

> **Folded 2026-11-07 [TC-12] (review condition):** the "as of" changes.
>
> - It is the **oldest** `lastSyncTimeInSec` among the account's kept folders that list mail, not
>   the newest. One fresh INBOX can then no longer hide folders that are weeks stale.
> - The words call it **"Thunderbird's count from DATE"**, never "a check of the server" (§2.1 fold).
>   The stale sentence becomes: "These numbers are Thunderbird's count from DATE. Open Thunderbird
>   and leave it open so it can count again."
> - **Stale** means older than 3 days, or **later than now** (the clock moved back).
> - A value ≤ 0 means never checked (§4.4). That includes the year-2038 wrap of Thunderbird's 32-bit
>   seconds (`nsImapMailFolder.cpp:1948`), which fails safe.
> - **When stale, the run's lead is "Backup finished.", never "Export complete."** A caught-up
>   account reads "As of DATE, Thunderbird had downloaded everything it listed for ACCOUNT". The
>   paragraph above ("the verdict still stands") stays true for the verdict, but not for the lead.
>   Row: MA-1179.
>
> **Folded 2026-11-07 [foldcheck M9] (lead's call):** the sentence "As of DATE, Thunderbird had
> downloaded everything it listed for ACCOUNT" is **superseded by [foldcheck M9]**. It broke R42 as
> amended, which allows "everything" only when the count is not stale. A stale `caught_up` account
> with missing = 0 now reads:
>
> > As of DATE, Thunderbird's count matched what is on this computer for ACCOUNT (N messages). It has
> > not counted since, so newer mail may not be here yet.
>
> R42 is **not** amended to allow a dated "everything"; the word is simply not used when stale. The
> lead stays "Backup finished.". `status -json` gains `stale` and `complete` per account (§7 fold).
>
> **Folded 2026-11-07 [TC-3] (lead's call L4 + review condition):** a profile that uses Panorama
> keeps its last `folderCache.json`. Thunderbird stops writing that file, but nothing removes it
> (review §2). The account is therefore **`unknown`** when **either** of these is true, whatever the
> file says:
>
> - a file named `panorama.sqlite` exists in the profile folder (checked with `Lstat`, never opened);
> - `prefs.js` sets `mail.panorama.enabled` to true.
>
> Words: "This Thunderbird uses its newer storage, which MailArchive cannot read yet, so it cannot
> tell whether Thunderbird has downloaded all of ACCOUNT." This check runs **before** the staleness
> words, so a Panorama profile is never told to "open Thunderbird so it can count again".
> MA-1171 gains both signals, each on its own. Lab row MA-1200 converts a throwaway profile and
> records whether the old file stays. Matching keys after a moved profile is in §4.1 step 2's fold.

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

> **Folded 2026-11-07 [TC-16] (review condition; owner default Q7):** deleted copies, counted
> safely.
>
> **The problem.** Adjustment 1 assumes that a deleted copy still in the offline file carries the
> Expunged bit 0x0008. That is **[unverified]**: nothing read shows Thunderbird sets it on those
> copies. If they do not carry it, they count as on disk and can cover the same number of messages
> never downloaded. That would give a false `caught_up`, which breaks R42.
>
> **The fix: subtract an estimate from the folder cache, as well as excluding the bit.**
>
> 1. Read `expungedBytes` per kept folder (§2.1 fold).
> 2. When it is above 0, estimate the stale copies as ⌈`expungedBytes` ÷ (`folderSize` ÷ listed)⌉.
> 3. Subtract that estimate from the folder's on-disk count. For Gmail, subtract the sum across the
>    account from X. In either case the result is clamped at 0.
> 4. If `expungedBytes` > 0 but there is nothing to divide by (listed ≤ 0 or `folderSize` ≤ 0), the
>    account is `unknown`. A folder carrying copies of unknown size can never support "caught up".
> 5. The walk still leaves out copies that do carry 0x0008. Both together can only lower X, so they
>    fail toward "behind", never toward "complete". The cost is a possible false "behind" until lab
>    row MA-1199 runs.
> 6. Once MA-1199 proves the copies carry 0x0008 and the walk excludes them, the subtraction may be
>    dropped, by amending this note.
>
> **Q7 (owner default):** these copies are still **archived**, as today. They are left out of the
> **count** only, and nothing changes in what a backup saves.
>
> **Release notes** must not claim the count is exact until MA-1199 has run.
>
> MA-1173 gains a fixture: `expungedBytes` > 0 plus extra unflagged copies → not `caught_up`.
>
> **Folded 2026-11-07 [foldcheck M7] (lead's call):** the estimate is not an upper bound.
>
> - **Why.** It divides by the folder's **average** message size, so deleted copies smaller than
>   average beat it. Example: 100 listed, about 1 MB each on average, 50 deleted copies of 10 KB
>   each (500 KB `expungedBytes`). The estimate is 1, the real figure 50; with 49 messages never
>   downloaded, X = 51 + 50 − 1 = 100 and missing = 0. Also, `expungedBytes` counts offline (on-disk)
>   bytes (`nsImapMailFolder.cpp:1388-1395`), while `folderSize` counts server bytes.
> - **The rule.** "Complete" words ("Export complete.", "everything it lists", and the missing = 0
>   claim) also need **`expungedBytes` = 0 on every kept folder of the account**, until lab row
>   MA-1199 proves that the deleted copies carry 0x0008 and the walk leaves them out. The verdict may
>   still use the estimate. With `expungedBytes` > 0 on any kept folder, the lead is "Backup
>   finished." and the words show both numbers ("about X of the Y messages it lists"), never
>   "everything".
> - **Item 5 above is corrected.** Its "both together can only lower X, so they fail toward
>   'behind'" is **superseded by [foldcheck M7]**: it is true only of the 0x0008 half. The
>   subtraction can undercount, so it can fail toward "complete"; the rule above is what stops the
>   words from doing so.
> - **Item 6 above** (drop the subtraction once MA-1199 passes) also lifts this rule, by amending this
>   note.
> - MA-1173 gains the skewed-size fixture above → never complete words (§12 fold).
>
> **Folded 2026-11-07 [TC-24] (review condition):** `mozillaReadFlag` is at `mbox.go:283`; `:280` is
> its comment.
>
> **Folded 2026-11-08 [C2 pass, ADV-C2-02] (lead's call): one identity per email, never one per
> copy.** Adjustment 2's sentence "A message with no Message-ID counts once each" is **superseded**.
> The builder read it as once per copy, so one ID-less email kept in INBOX and in All Mail counted
> twice and could cover an All Mail message never downloaded (a false `caught_up`, against R42).
> The rule now:
>
> - **Identity = the Message-ID** when the message has one: the first `Message-ID` field of the
>   header block, its name read in any letter case with optional white space before the colon
>   (RFC 5322's obsolete syntax, ADV-C2-14), unfolded, trimmed of white space and angle brackets.
> - **Otherwise, the content identity** of the gmail-dedup design (§4.4 step 1 there): a checksum
>   of the message's bytes with the `X-Mozilla-Status`, `X-Mozilla-Status2` and `X-Mozilla-Keys`
>   lines of its top header block (and their continuation lines) taken out, so two copies that
>   differ only in what Thunderbird marks per copy have one identity. Its bounds are that design's:
>   at most 8 such lines and 1 KiB, and the header block's end within 64 KiB (a message shorter
>   than 64 KiB that is all header has an identity).
> - **X counts each identity once** across the account's kept folders.
> - **A message that yields neither** (none is expected: no Message-ID and a split out of bounds) is
>   left out of X, counted in the record as `no_identity`, and the account **may not say complete**
>   (`MaySayComplete` is false while any is there). Leaving it out can only lower X, toward
>   "behind".
> - The 16-byte keys are the first 16 bytes of SHA-256: of the Message-ID as before, and of
>   "content", a NUL and the bytes for the content identity. Still about 4 MB for 238,000 messages.
>
> **Folded 2026-11-08 [C2 pass, ADV-C2-03] (lead's call): only Thunderbird's own separators are
> counted.** The walk's mbox reader (go-mbox v1.0.4) starts a new piece at every line beginning
> "From ", so a body line its writer did not escape inflated X. The **count** now takes a piece only
> when it begins at a separator Thunderbird writes: a line starting "From " that is at the file's
> start or follows an empty line, and has the From_ shape: "From ", a sender word, then the time as C's
> `ctime` gives it (Thunderbird writes "From - " and `ctime`, `nsImapMailFolder.cpp`'s
> `SetupMsgWriteStream` in the ESR 153 copy), a time zone allowed beside the year (the repo's
> fixtures' "From sender Mon Mar  3 09:00:00 2025" form). Every other piece is noted as
> unseparated and never counted.
>
> - A kept mailbox with any unseparated piece is **split differently**: the account is `unknown`
>   with its own reason, `split_differently`, unless the folders already miss more than the
>   tolerance, when it is `behind` (as M2's can't-tell folders are: a certain gap is still told).
>   It never says complete. The record keeps `split_mailboxes`.
> - **What is archived is unchanged** in this slice (Q7). The on/off byte-equality test and the
>   P14 golden still pass.
> - **Not checked from the source copies:** whether every Thunderbird version puts an empty line
>   before each separator. `nsMsgBrkMBoxStore.cpp` wraps the offline store in `MboxMsgOutputStream`
>   ("handle mbox 'From ' separator, escaping etc."), whose own source is not in the copies under
>   `.claude/`. If a real store had separators with no empty line before them, its messages would
>   count low and read `behind` or `split_differently`: it fails safe, but lab row MA-1199 (or
>   MA-1190 on the owner's profile) should count real separators before the release notes claim
>   the count is exact.
> - **Residual:** a body line of exactly Thunderbird's separator shape after an empty line is still
>   a separator to the count, since nothing in the file tells it from one. Current Thunderbird
>   escapes such lines when it writes (the wrapper above); an older store written unescaped is the
>   case left.
>
> **Follow-up, not this slice: archive splitting of unescaped From lines (pre-existing, check
> separately).** The archive's own walk splits a message at an unescaped "From " body line today:
> a probe on 2026-11-08 (a throwaway test, not committed) showed the message cut at the first such
> line, the "From …" lines themselves dropped (each is taken as a separator), and the rest archived
> as an extra "(unparseable message)" and an empty one. That is a fidelity question for the reader,
> outside this design; it needs its own design and review.
>
> **Folded 2026-11-08 [C2 pass, ADV-C2-12] (lead's call):** a maildir folder's count leaves out the
> files Thunderbird's maildir store would not hold as messages: a name starting with "." (a Finder
> `.DS_Store`, a sync tool's file) and an empty file. They are archived as before; `tmp/` is never
> read. The maildir store's own source is not in the copies under `.claude/`, so the list is the
> lead's, not read from Thunderbird.
>
> **Lab evidence, 2026-11-08: the lead's count-only scan of the owner's real macOS profile, with
> the owner's permission (numbers only; no name, subject, address or content was read into this
> document).** 22 mailboxes, 48.9 GB. 318,856 lines start "From ": **every one** is a Thunderbird
> separator of the exact shape "From - " and a ctime date, after an empty line or at the file's
> start; **no other** line starts "From " anywhere. 5,655 lines start ">From " (Thunderbird escapes
> such body lines). Of the 318,856 messages, 318,854 carry an `X-Mozilla-Status` header; 2 lack it,
> both in one mailbox. These are the constraints the next fold is built to.
>
> **Folded 2026-11-08 [C2 pass round 2, R2-04 and R2-05] (lead's call): only Thunderbird's own
> separator, and two counts.** Supersedes the [ADV-C2-03] fold's "From_ shape" and its residual.
>
> - **A separator is only "From - " and Thunderbird's ctime date** (`nsImapMailFolder.cpp`'s
>   `SetupMsgWriteStream`: `ct[24] = 0; result = "From - "; result += ct;`), at the file's start or
>   after an empty line, a CR before the LF tolerated: `From - Www Mmm dd hh:mm:ss yyyy`, the day
>   space-padded (`Mar  3`) or two digits (`Mar 03`). No other sender word (a git patch's first
>   line, "From <sha> Mon Sep 17 00:00:00 2001", is a body line now), no time zone, nothing after
>   the year. **Builder's reading, not checked:** the Windows C library's `ctime` pads the day with
>   a zero, so both paddings are taken; no Windows store has been scanned (lab row MA-1199).
> - **Two counts per mailbox.** STRICT: the pieces at a separator whose header holds
>   `X-Mozilla-Status` (Thunderbird writes it at the top of every offline copy). LENIENT: every
>   piece at a separator. "Caught up" and "complete" use only the strict count; "behind" is told
>   only if the account is still short by the lenient count; between the two the account is
>   `unknown`, reason `count_uncertain` (the builder's code). So a header without
>   `X-Mozilla-Status` can neither fake complete nor fake behind. For Gmail the lenient X adds the
>   loose pieces' identities not already counted **and every message with no identity key**
>   (strict or loose: each may be one more email; the builder's reading, so the lenient count is
>   never below what is on disk). A maildir file is one message whatever its header (there is no
>   separator to mistake), so it is in both counts. The owner-shaped case: 2 of about 170 without
>   the line is strict short by 2, within the tolerance (17): still `caught_up`, never complete
>   (missing 2). The record keeps `no_status`.
> - **A separator glued to the line before it** (the shape, no empty line before it) is never
>   counted and its account is **never `behind`**: `unknown`, `split_differently` (R2-05). The
>   owner's profile has none. A "From " body line of any other shape is still "split differently"
>   (never complete) but a certain gap by the lenient count still says behind, as before: such a
>   piece cannot hold a message Thunderbird wrote, which would begin at a separator. The record
>   keeps `glued_mailboxes`.
> - **Residual:** mail crafted to carry both a "From - <ctime>" line after an empty line and an
>   `X-Mozilla-Status` line, in a store whose writer did not escape it. Current Thunderbird escapes
>   (the owner's 5,655 ">From " lines and no unescaped one); release notes still wait for MA-1199.
>
> **Folded 2026-11-08 [C2 pass round 2, R2-01 and R2-03] (lead's calls): the identity's two
> edges.** The Message-ID is taken only when the header block ends within the 64 KiB bound (an
> empty line inside it, or a message no longer than the bound with none); otherwise the content
> identity is used, which has no key either when the block runs past the bound (`no_identity`).
> A Message-ID line cut at the bound is never hashed. For the **counting** identity only, every CRLF
> is read as LF before the content checksum, so an LF and a CRLF copy of one ID-less email have
> one key (a lone CR stays content). **The gmail-dedup design's stricter identity is intentional:**
> it merges only byte-identical copies, which stays safe, and it is not changed.
>
> **Folded 2026-11-08 [C2 pass round 3, R3-01] (lead's call): the count never depends on the
> reader's chunks.** The round-2 fold's sentence "such a piece cannot hold a message Thunderbird
> wrote" did not hold for a "From " line longer than go-mbox's 4,096-byte line buffer: the reader
> can end one more piece at a later chunk of it, and matching pieces to separators by their order
> then hands each later piece the next one's separator, so the last real message is counted
> nowhere (a false "behind"). Now each piece's reading waits for the file's end, and pieces are
> matched to the scan's separators only when the reader handed on exactly as many pieces as the
> scan saw "From " lines. go-mbox ends a piece at every line the scan notes and may end more, never
> fewer (read from `reader.go` v1.0.4: a fresh piece reader forgets it was in the middle of a line,
> never the other way), so equal numbers are equal splits. Otherwise the mailbox is **misaligned**:
> no piece is counted, it is split differently, and its account is `unknown`, `split_differently`,
> never behind (as a glued separator, R2-05). The record keeps `misaligned_mailboxes`. Only a file
> Thunderbird did not write can hold such a line (it escapes body "From " lines). Memory: a
> mailbox's readings are held until its end, about 20 bytes a message.
>
> **Folded 2026-11-08 [owner ruling: header check] (owner's ruling, "go"; the lead's reason).
> Supersedes the round-2 fold's STRICT rule "the pieces at a separator whose header holds
> `X-Mozilla-Status`".** A piece at a separator now counts STRICT when its header holds
> `X-Mozilla-Status` **or** its header is well-formed. The lead's reason: `X-Mozilla-Status` is as
> forgeable as any header line, so a well-formed header is an equally strong sign that the piece
> is no accidental body "From - <date>" line; and it lets the owner's 2 real messages without the
> line count, so that account may say complete. Everything else in the round-2 and round-3 folds
> stands (what a separator is, glued, unseparated, misaligned, the lenient count, `behind` only by
> the lenient count).
>
> - **Well-formed, exactly** (`source.wellFormedHeader`): the header block is the lines after the
>   separator up to the first empty line, and it must end within the 64 KiB bound, read as the
>   identity's Message-ID is (`headerWithin`, R2-01): an empty line inside the bound, or a piece no
>   longer than the bound that is all header. A header that does not end within the bound is not
>   well-formed. Every line of the block is either a field, `<name>:` with a name of one or more
>   bytes 33 to 126 but 58 (RFC 5322 ftext: printable ASCII, no space, no colon), or a continuation
>   line starting with a space or tab, never the first line. CRLF is tolerated. The block holds a
>   `Date:` field and at least one of `From:` or `Message-ID:`, names compared case-blind. Anything
>   else is not well-formed, and the piece is LENIENT only (`Loose`), as before.
> - **Builder's readings (each fails toward lenient or matches the existing bound; the lead may
>   change them):** "ends within the bound" includes a short piece that is all header, because
>   go-mbox drops the empty line before the next separator, so a message with no body reaches the
>   count with no empty line (the same reading `headerWithin` already gives the identity). A field
>   with an empty value (`Date:` and nothing after it) counts as present: the ruling names fields,
>   not values. The obsolete spelling with white space before the colon (`Date :`) is not
>   well-formed (space is not ftext), though `headerValue` reads it for the identity. A piece that
>   begins with an empty line has an empty block, so no `Date:`: not well-formed (the round-3
>   R3-02 empty-header variant stays lenient unless it carries `X-Mozilla-Status`, as before).
> - **The residual widens, by the ruling.** The round-2 residual (a body line of exactly
>   Thunderbird's separator after an empty line, unescaped, followed by `X-Mozilla-Status`) now
>   also covers such a line followed by a well-formed header (e.g. a quoted mbox excerpt with
>   `From:` and `Date:` lines). Thunderbird escapes such body lines (the owner's 5,655 ">From "
>   lines, 0 bare), so only a store another writer made can carry it; release notes still wait for
>   MA-1199. The git patch's "From <sha> …" line is still no separator, whatever header follows it.
> - **The record's `no_status`** keeps its name and now means pieces at a separator with neither
>   `X-Mozilla-Status` nor a well-formed header.
> - **Owner-shaped, under the ruling** (counts only, the lead's scan): 2 of about 170 without the
>   line, with well-formed headers, are strict: missing 0, `caught_up`, and the account may say
>   complete when everything else holds. **Not checked:** the lead's scan counted the 2 messages
>   without the line but did not test their headers against this definition; that they are
>   well-formed is the lead's expectation, to be confirmed by a count-only re-scan (MA-1190).

Maildir stores (`storeContractID` maildirstore, `internal/tbrestore/storekind.go:26-28`) count
one file per message the same way. A `-copy-first` run counts its copy, but maps it back to the
original store's profile for the folder cache.

> **Folded 2026-11-07 [TC-11] (review condition):** the `-copy-first` sentence above is wrong.
> A store folder is never copied; `-copy-first` snapshots single files only, and "mail-store
> directories are read in place" (`run.go:1631-1634`). Read this rule instead:
>
> - Completeness is computed only when the walk covered a **whole store**. A whole store means the
>   input was the account's own store folder (the folder `prefs.js` names for it, which is under
>   `ImapMail/` unless the person chose another place, §2.2 fold), or a profile or parent folder
>   from which store discovery took the whole store.
> - The store is identified by the **original input `path`**, as the identity seed already is
>   (`run.go:1672-1683`), never by `openPath`.
> - An `-input` that is one mbox file, or a subfolder inside a store, is a **partial** account →
>   `unknown`. It gets the neutral words and **no WARN**: the person chose to back up part of it.
>
> Row: MA-1202.
>
> **Folded 2026-11-07 [TC-21] (review condition):** `folderCache.json` is read **once per run, after
> the walk** has finished every store. A long walk gives Thunderbird time to list new mail, and
> reading afterwards takes the larger Y, which is the cautious order. The cache is read for every
> store of a profile from the same read (one read per profile per run, kept in memory, §4.1 step 2's
> fold).

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

> **Folded 2026-11-07 [TC-1] (owner default O3 + review condition):** a floor can never reach
> "caught up".
>
> **Detecting Gmail: by the `is_gmail` pref only.** Thunderbird's single-copy rule runs only when
> `GetIsGMailServer` is true (`nsImapMailFolder.cpp:8928-8935`). So the "or any folder carries the
> AllMail flag" half of the detection rule above is **dropped**. A non-Gmail server with an
> AllMail-flagged folder keeps the per-folder rule. If `prefs.js` cannot be read, the account is
> `unknown` (§2.2 fold).
>
> **When All Mail sets Y.** All Mail sets Y (and the account may reach `caught_up`) only when it is
> all of these:
>
> - present in the folder cache;
> - kept on this computer;
> - checked (`lastSyncTimeInSec` > 0, and not in the future);
> - listing at least the largest other kept folder's count, less the tolerance (§4.3).
>
> Y is then max(All Mail, the largest other kept folder other than Spam and Trash), plus Spam and
> Trash if they are kept, so they are never counted twice.
>
> All Mail is found by the AllMail flag 0x8000 **or** the Archive flag 0x4000. Thunderbird's own
> label lookup uses Archive (`nsImapMailFolder.cpp:8954-8956`), and the owner's All Mail carries
> both. If more than one folder of the account carries either flag, All Mail is treated as absent,
> so Y is a floor.
>
> **Folded 2026-11-07 [foldcheck M5] (lead's call):** the paragraph above is **superseded by
> [foldcheck M5]**. Thunderbird gives Gmail's All Mail **both** flags, AllMail 0x8000 and Archive
> 0x4000 (`nsImapMailFolder.cpp:1230-1231`). Archive alone comes from a server `\Archive` folder or
> the identity's archive folder (`:1232`; `nsMsgAccountManager.cpp:1541-1542`), so with All Mail
> hidden, Thunderbird's own "Archives" label could stand in for it and reach "Export complete.".
>
> - **All Mail is the one folder of the account carrying 0x8000.** A folder carrying only 0x4000
>   never stands in for it.
> - If no folder carries 0x8000, or more than one does, All Mail is treated as absent and Y is a
>   floor (never `caught_up`).
> - The shared Gmail function (below) owns this "which folder is All Mail" rule as well as the
>   detection.
> - MA-1176 gains the fixture: All Mail hidden, an "Archives" label with 0x4000 only and all its
>   mail on disk, Sent with nothing on disk → never `caught_up` (§12 fold).
> - [proven at trunk, ab60cc5e; re-read at ESR 153 under M4, §16 fold]
>
> **Otherwise Y is a floor.** The floor is the largest single kept, checked folder other than Spam
> and Trash, plus Spam and Trash if they are kept. Spam and Trash are never the "largest folder", so
> they cannot be counted twice. With a floor:
>
> - X < floor − tolerance → `behind`, worded "about X of **at least** Y". X under a floor is
>   certainly short.
> - Otherwise → **`unknown`**, never `caught_up`. Words (O3): "About X messages are on this
>   computer. Thunderbird lists at least Y for ACCOUNT. MailArchive cannot tell whether that is all
>   of it, because Gmail's All Mail is not kept on this computer." Where it fits, the last clause
>   instead reads "…is not shown to Thunderbird" or "…has not been checked by Thunderbird yet". The
>   lead is "Backup finished."
>
> MA-1176 gains a fixture where X is above the floor and a large label is empty: never `caught_up`.
>
> **Seam with the gmail-dedup design.** The gmail-dedup design (`docs/design-gmail-dedup.md` §6, on
> branch `gmail-dedup` at 9203aae) still detects Gmail by "`is_gmail` **or** the All Mail flag",
> written as "the same rule as the completeness design §3". After this fold the two rules differ.
> **Not resolved here.** Its review should adopt `is_gmail` only, or the two must agree on one rule.
> Both designs say one shared detection function in `internal/thunderbird`, owned by whichever lands
> first.
>
> **Folded 2026-11-07 [Gmail detection] (lead's call, after foldcheck §5):** "Not resolved here" is
> **superseded**. **Gmail is told by `is_gmail` only**, in this design and in gmail-dedup; the lead is
> telling the gmail-dedup design the same. Why (the fold check's reasoning): the single-copy rule runs
> only when `GetIsGMailServer` is true (`nsImapMailFolder.cpp:8928-8935`), so a non-Gmail server's
> `\All` folder still gets one copy per folder, and calling it Gmail would let one full folder hide
> gaps in the others. The cost: a profile that has never connected has no `is_gmail` yet
> (`nsImapIncomingServer.cpp:2173`), so it gets the per-folder rule and a false "behind" for Sent,
> which fails safe. The original detection sentence at the top of §3 ("**or** any folder of the
> account carries the AllMail flag") stays superseded, as the TC-1 fold above already says. The one
> shared function in `internal/thunderbird` owns both detection and "which folder is All Mail" (M5).
>
> **Folded 2026-11-08 [B1 split] (lead's call).** "owns both detection and 'which folder is All
> Mail'" above, and the M5 fold's "The shared Gmail function (below) owns this 'which folder is All
> Mail' rule as well as the detection", are **superseded in part by [B1 split]**: the two are split.
>
> - **Detection** is gmail-dedup's slice B1, built on this branch: `thunderbird.GmailForStore(profile,
>   store)` answers yes, no or can't tell, decided only by the matched account's
>   `mail.server.<key>.is_gmail` (the account from `FindAccountForStore`; prefs.js with user.js over
>   it, through C0c's allow-list reader and `util.ReadRegularFile`; a wrong type or a file that
>   cannot be read with certainty is can't tell; no account is can't tell). Can't tell means "do not
>   apply the Gmail rule": here `unknown`. With no line of its own the account takes
>   `mail.server.default.is_gmail`, then Thunderbird's built-in false, as Thunderbird does. `is_gmail`
>   joins the settings MA-1270's type rule checks. Rows: MA-1235 (gmail-dedup's id, limited to what
>   B1 proves) and MA-1175's detection part.
> - **Which folder is All Mail** (the one folder of the store with 0x8000; none or more than one →
>   none, M5) is **not** in the `internal/thunderbird` function. It is read from `folderCache.json` by
>   slice C1a's reader, with that reader's own bounds (`util.ReadRegularFile`, 16 MB) and its own
>   adversarial pass, and C1b's verdict uses it. MA-1235's 0x8000 cases (and the gmail-dedup design's
>   S7 sentence, "read from a `folderCache.json` fixture through the shared function") are proven with
>   C1a, not B1. Why: `folderCache.json` is a new untrusted file whose reader is C1a's, gated by its
>   own pass; a second reader of it in `internal/thunderbird` would be a second parser to review.
> - gmail-dedup's design is not changed on this branch; `docs/notes-b1-split-for-gmail-dedup.md`
>   says what it should fold when the branches meet (it names All Mail in words only, so it needs C1a's
>   answer only for its words).
>
> **Folded 2026-11-07 [TC-22] (review condition, lab):** Gmail may have an IMAP setting that limits
> each folder to the newest 1,000, 2,000, 5,000 or 10,000 messages **(recollection, unverified)**.
> When All Mail's listed count equals one of those exactly:
>
> - add a note: "Gmail may be showing Thunderbird only the newest N messages of each folder. In
>   Gmail's settings, under IMAP access, check the folder size limit."
> - the lead is "Backup finished.", not "Export complete.". This is the folder's addition: it is
>   cheap and fails safe.
>
> Lab row MA-1201 checks whether the setting still exists. If it is gone, this note is dropped by
> amending it.
>
> **Folded 2026-11-07 [TC-23] (review condition, lab):** if Gmail often holds two messages under one
> Message-ID, the distinct count stays below All Mail's listed count by more than the tolerance, and
> the account reads "behind" for good.
>
> - MA-1190 must assert, on the owner's caught-up profile, that X is within the tolerance of All
>   Mail's listed count.
> - If it is not, the distinctness key becomes Message-ID plus Date plus size, by amending this note
>   and §2.4 adjustment 2. That is still a 16-byte hash per message.
> - Gmail's own message id (`X-GM-MSGID`) is in the `.msf` files, which this design does not read
>   (Q8). The gmail-dedup design's summary-file reader could later give an exact key, but not in
>   this build.

**A fidelity side-finding, out of scope.** Because of the single-copy rule, the archive's "Sent
Mail" folder for this Gmail account holds 2 messages; the sent mail is archived under All Mail
(or wherever its copy landed). A person looking for sent mail in the archive would be misled. This
is a label-membership question, not completeness (§14 Q6).

> **Folded 2026-11-07 [Q6] (owner default):** this is handled by the Gmail de-duplication design
> (`docs/design-gmail-dedup.md`, branch `gmail-dedup`). That design reads folder membership from the
> `.msf` summary files and shows each message in every folder it belongs to, Sent Mail included.
> This design neither reads `.msf` files (Q8) nor changes folder membership. Until gmail-dedup ships,
> this build's release notes say that a Gmail archive's Sent Mail folder can look nearly empty, and
> why.

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

> **Folded 2026-11-07, step by step.**
>
> **Step 1 [TC-10] (review condition; lead's call C0c), [TC-5], [TC-11].**
>
> *The account match (fixed first, in C0c).* `FindAccountForStore` loops over a Go map and returns
> the first server whose `hostname` equals the store's folder name (`thunderbird.go:97-104`). With
> two accounts on one server (`ImapMail/imap.gmail.com` and `ImapMail/imap.gmail.com-1`), the
> result is random. C0c changes it to:
>
> 1. match the store folder against each server's `directory` and `directory-rel` first, compared
>    by whole path components after the same normalising as step 2;
> 2. fall back to `hostname` only when exactly one server has it;
> 3. return no account when it is still ambiguous, never a guess.
>
> This also fixes today's wizard and `-enable-offline`, which pass the matched account to
> `EnableOffline` (`wizard.go:1594-1598`, `main.go:2133`). Those can switch on offline download for
> the wrong account and report the right one as done (review side finding A). Row: MA-1195. It is
> built first, before C0b and C1a.
>
> *What follows from the match.*
>
> - The kind of account comes from the matched account's `type` (§2.2 fold).
> - Gmail is the matched account's `is_gmail` (§3 fold).
> - The limits come from the matched account's prefs (step 4).
> - No match → `unknown` if a folder-cache entry exists (§2.2 fold).
> - Completeness is computed only when the walk covered the whole store (§2.4 fold, TC-11).
>
> **Folded 2026-11-07 [foldcheck M1] (lead's call): C0c also reads `prefs.js` safely.** Today
> `parsePrefs` reads it with `os.ReadFile` (`thunderbird.go:63-64`), and `EnableOffline` the same way
> (`:172`). With this design every IMAP store's backup reads it, so a pipe planted at `prefs.js`
> would hang a scheduled backup (the hang TC-15 closed for `folderCache.json`).
>
> - C0c reads `prefs.js` through `util.ReadRegularFile(path, 16 MB)` (`openregular.go:67`), in
>   `parsePrefs` and so for every caller: a link is refused, a pipe or device is not waited on, and a
>   file over the cap is not read.
> - A refusal is treated like an unreadable `prefs.js`: no account, which M6 turns into `unknown`
>   (§2.2 fold), never `not_applicable`.
> - MA-1195 gains a pipe and a link at `prefs.js`: no hang, no account (§12 fold).
>
> **Folded 2026-11-07 [completeness foldcheck2 L1] (lead's call): `EnableOffline` too.** The M1
> bullet "in `parsePrefs` and so for every caller" does not reach `EnableOffline`. It does its own
> `os.ReadFile` of `prefs.js` (`thunderbird.go:172`) and its own `os.WriteFile` of the backup and of
> `prefs.js` (`:195`, `:198`), and never calls `parsePrefs`. Both are in C0c's scope:
>
> - **The read** goes through `util.ReadRegularFile(path, 16 MB)` as well. A link, a pipe, a device
>   or a file over the cap is refused, and nothing is written.
> - **The writes** (the backup `prefs.js.mailarchive-bak` and `prefs.js` itself) never follow a link
>   and never replace a file that is not a regular file. They use the repo's atomic writer
>   (`util.WriteFileAtomic0600Temp`: a temporary file beside the target, then a rename, which
>   replaces the name and never writes through a link), after a fresh `Lstat` shows the target is a
>   regular file or, for the backup only, absent.
> - `EnableOffline` runs attended only (the wizard, `-enable-offline`), so this is no "complete"
>   path. MA-1195 gains the cases (§12 fold).
>
> **Built 2026-11-07 [C0c] (the builder's notes, for the lead).** C0c is built as written above,
> with MA-1195 in the catalog. Where the builder read the text more strictly, or the code changes
> something the text does not name:
>
> - **The hostname fallback, read more strictly (the builder's reading; the lead may widen it).**
>   Item 2 above says "fall back to `hostname` only when exactly one server has it". The build also
>   requires that server's own folder settings, if it has any, to name a folder of the store's name.
>   Otherwise a leftover `ImapMail/imap.gmail.com` (an account removed, its folder left) would be
>   given the one remaining `imap.gmail.com` server, whose folder is `imap.gmail.com-1`. A moved
>   profile's stale `directory` still names a folder of the same name, so it is still found. This
>   fails toward "no account".
> - **Only `[ProfD]` is resolved** in `directory-rel`. Any other prefix is not used for the match.
> - **prefs.js after `EnableOffline`** is a new file put in place by a rename, with mode 0600, and so
>   is its backup. The old in-place write kept the file's own mode and identity (a hard link to it
>   now stops seeing changes). The mode Thunderbird itself gives prefs.js was not checked.
> - **On Windows** the open cannot refuse a link by itself (no `O_NOFOLLOW`), so containment there
>   rests on the `Lstat` just before, as `util.OpenRegularFile` says. The rename waits out a moment
>   of Windows file sharing (up to 2 s, `util.ReplaceRetrying`).
> - **Gate owed: an adversarial pass** on C0c's prefs.js read and write and the account match. The
>   slice table's C0c gate (§13) names none, but foldcheck2 L1 made C0c change how MailArchive
>   writes a file in the person's Thunderbird profile. Under the repo's rule (security-sensitive
>   changes get the adversarial pass), C0c stops at that pass before merge. It is not self-certified
>   here.
>
> **Folded 2026-11-07 [C0c adversarial pass] (the lead's calls on all nine findings,
> `docs/review-thunderbird-completeness-c0c-adversarial.md`; built and tested, MA-1195, MA-1203 to
> MA-1205):**
>
> - **Live accounts only (CON-01).** A server is a candidate only when it is named by
>   `mail.account.<id>.server` for an id in `mail.accountmanager.accounts` (its last line wins). No
>   list → no account.
> - **The hostname fallback (INS-02).** Item 2 above is now read as written: two live servers with
>   the hostname → no account, even if only one passes the builder's stricter folder rule.
> - **What a setting is (CON-02, CON-03, CON-06).** prefs.js, then user.js over it, as Thunderbird
>   applies them; a line in a `/* */` comment or after `//` or `#` never counts; the last line wins.
>   `EnableOffline` changes every live line when it changes the setting, and changes nothing when
>   user.js sets it (on → already on; otherwise a refusal naming user.js, MA-1205).
> - **The backup (CON-04): written once, then kept.** The lead offered "keep the first original" or
>   a dated name; the build keeps the first: `prefs.js.mailarchive-bak` is never overwritten, and the
>   command line says when it kept an older one instead of "backed up". Between the check and the
>   rename a file put there would be replaced; `EnableOffline` runs attended only.
> - **Line endings (CON-05).** The file's own ending (CRLF if its first line has one) is kept.
> - **The profile (INS-01, MA-1203).** `FindProfileDir` takes a folder for the profile only when it
>   holds prefs.js as a regular file and the input is under its own `ImapMail/` or `Mail/` (or is
>   the profile itself, holding one of them and not inside a store). The marker is prefs.js alone,
>   as the restore's and the pre-sync's profile checks accept; the structure rule is what keeps a
>   store out.
> - **The reason reaches the person (OUT-01, MA-1204).** The words: "MailArchive could not tell
>   which Thunderbird account the folder <name> belongs to, so it changed nothing; in Thunderbird,
>   open that account's settings and turn on <label> yourself. Why: <reason>." The label follows §6's
>   version rule (L5). The command line refuses with its refusal code (1) after checking every
>   store first; the wizard shows the words and returns "not prepared", so the export runs on what
>   is on this computer, in its usual mode.
> - **Gate owed:** the wizard's new words owe a light friction look (the friction review this
>   feature owes already). Not self-certified here.
>
> **Folded 2026-11-08 [C0c adversarial pass round 2 + friction W0 to W8] (the lead's calls,
> `docs/review-thunderbird-completeness-c0c-adversarial-r2.md` and
> `docs/review-thunderbird-completeness-c0c-friction.md`; built and tested, MA-1206 to MA-1260):**
>
> - **The profile, by its own files (R2-03, R2-04, R2-06; MA-1206). Supersedes the INS-01 bullet
>   above ("the structure rule is what keeps a store out").** A folder is a Thunderbird profile only
>   when it holds a regular `prefs.js` **and** a regular `compatibility.ini` that reads as
>   Thunderbird's: read through `util.ReadRegularFile` (64 KB), its first line that is not blank is
>   `[Compatibility]` and that section holds a `LastVersion=` line. Folder names around it no longer
>   matter: a profile in `~/Mail/tb-profile` is found. The nearest folder at or above the input that
>   holds both files decides: it is the input's profile when the input is that folder or sits under
>   its own `ImapMail/` or `Mail/`; otherwise the input has no profile (the walk does not go on
>   upward, so a folder outside a copied profile's mail is never the outer profile's store). An mbox
>   file starts with "From ", so a mail folder named `prefs.js` or `compatibility.ini` never makes a
>   store a profile. A copied profile under this profile's `Mail/` that carries its own two files is
>   its own profile, and its store gets its own account.
>   - **Residual (R2-04, not built):** a copy under `Mail/` *without* its own `compatibility.ini` is
>     still read as part of the outer profile, and the hostname fallback can give its store the outer
>     profile's server. A one-line fix (the hostname fallback only for a store directly in the
>     profile's `ImapMail/`) is listed as a follow-up for the lead.
>   - **The other profile checks are not changed.** The restore's `tbrestore.checkProfile` and the
>     pre-sync's `validateProfile` (macOS, Windows) check a profile chosen from `profiles.ini` or
>     named by the person; they never climb from a store, so R2-03 does not reach them. Making them
>     require `compatibility.ini` would change which profiles the restore and the pre-sync accept,
>     with their own lab rows, so it is a follow-up, not part of this fix. `tbrestore.LastVersion`
>     keeps its own reader for the same reason.
> - **Hand-edited lines (R2-01, R2-02; MA-1207).** prefs.js and user.js are read statement by
>   statement, as Thunderbird's parser reads them: a line may hold several statements and each
>   counts; for a name set twice the last statement wins. A line whose text outside comments is not a
>   run of whole statements is one MailArchive cannot read; a setting it names is one MailArchive
>   cannot be sure of, so the match gives no account and EnableOffline changes nothing
>   (`HandEditedError`). EnableOffline rewrites only a line that holds exactly one `user_pref`
>   statement for the setting and nothing else but spaces and the line ending; a statement of the
>   setting sharing its line with anything else (another statement, a comment, a commented copy), a
>   `pref(` line, or a comment left open at the end of prefs.js (an added line would not count) →
>   nothing changed, words telling the person to make sure of it in Thunderbird. Not handled
>   (refused or read conservatively, never guessed): a statement spread over several lines, and
>   `pref()` against `user_pref()` priority in user.js.
> - **Every refusal before any write (R2-05; MA-1208).** `-enable-offline` runs, for every store,
>   the Thunderbird-running check, the account, and `thunderbird.CheckOffline` (every check
>   EnableOffline makes before it writes: user.js, hand-edited lines, prefs.js and its backup path)
>   before it changes any. Only a write that then fails (a full disk, a file swapped in after the
>   check) can stop the run after an earlier store was changed; the log names each change made.
>   The wizard runs the same `CheckOffline` before asking the person to quit Thunderbird.
> - **A closed warning goes on (R2-07; MA-1209).** The wizard's refusal warning, and the dialog for
>   a change that failed after the quit, read OK and a window closed with its close button alike
>   (`noticeClosed`), as after the Full-mode information dialog; the export goes ahead, as the words
>   say.
> - **The words (friction W0 to W8, plus the lead's sentence; MA-1260; MA-1204 and MA-1205 amended).**
>   Each refusal has the command line's words (`Error`: "-enable-offline changed nothing, and
>   nothing was exported: …", the technical reason kept after the plain sentence, what to make sure
>   of for each account on the server and where, "Then run the same command again without
>   -enable-offline.") and the wizard's (`Plain`: no prefs.js, no server key, no raw error; numbered
>   steps; ends with W0, which says mail not yet downloaded is left out and a later run adds it).
>   Where the setting is follows the version: below 160 the account's "Synchronization & Storage"
>   page and "Keep messages in all folders for this account on this computer"; 160 and later
>   "Offline message storage" and "Save copies of all folders and messages on this computer for
>   offline use"; both when the version cannot be read. Where a removed account can cause it (two
>   accounts on one server, or no account), the words add: "If you removed an account from
>   Thunderbird, its leftover mail folder can cause this; if so, you can ignore this message." The
>   user.js words name the server, say "If you or your IT set this on purpose, leave it", and give
>   the file's path last. The log lines name the account by its folder and server and say the backup
>   is in the Thunderbird profile folder. The words still rest on lab row MA-1191 for the labels.
>
> **Folded 2026-11-08 [C0c adversarial pass round 3 + the friction re-look] (the lead's calls,
> `docs/review-thunderbird-completeness-c0c-adversarial-r3.md` and the "Re-look of the words as
> built" section of `docs/review-thunderbird-completeness-c0c-friction.md`; built and tested,
> MA-1261 to MA-1263, MA-1260 and MA-945 amended):**
>
> - **Strict reading (R3-01 to R3-04; MA-1261). Supersedes "as Thunderbird's parser reads them"
>   and the "Not handled" sentence of the round-2 bullet above.** MailArchive does not model
>   Thunderbird's parser. It changes prefs.js, and reports a setting as already on, only when it reads
>   prefs.js **and** user.js with certainty. Either file holding a NUL byte anywhere, any statement it
>   cannot parse (one missing its `;` among them), a string value spanning lines, any `pref(` or
>   `sticky_pref(` statement, or any setting name with a backslash is refused, whichever setting the
>   line is about: "can't tell", nothing changed, the hand-edit words. Thunderbird never writes such a
>   file, so this refuses only files changed outside it. A refused run leaves both files byte for byte.
> - **The hostname fallback (R3-05; MA-1262). Closes the R2-04 residual above.** The hostname gives
>   an account only to a store directly in `<profile>/ImapMail/<dir>`; a deeper store, such as a
>   copied profile's under `Mail/` or `ImapMail/` without its own `compatibility.ini`, gets an account
>   only by its own folder settings.
> - **A stop after a change (the round-3 wording note; MA-1263).** When a later store stops the run
>   after an earlier store was changed (Thunderbird started meanwhile, a failed write), the words say
>   what was changed and that it stays, then why the run stopped, never "changed nothing". This is
>   the simpler of the two true options (moving the running check next to each write would still
>   leave a failed write to word).
> - **The words (the re-look's R1 to R8).** Step 1 is "1. In Thunderbird, click the menu button (≡,
>   top right), then Account Settings." (the command line: "open Account Settings (the menu button ≡,
>   top right)"); the leftover-folder sentence names the server and is split: the wizard's ends
>   "there is nothing to turn on.", the command line's "…: just run the same command again without
>   -enable-offline."; 160's step 2 names no page; step 2 says an account's "Server Settings" page
>   shows its server; user.js says "If you, or whoever looks after this computer, …" and how to open
>   the file in Notepad; the wizard says the file "was changed outside Thunderbird". The Desktop guide
>   names the box by both labels, by version, as the wizard does when it cannot read the version.
>
> **Folded 2026-11-08 [C0c adversarial pass round 4 + the friction final look] (the lead's calls,
> `docs/review-thunderbird-completeness-c0c-adversarial-r4.md` and the "Final look after round 3"
> section of the friction review; built and tested, MA-1264 to MA-1268):**
>
> - **The allow-list reader (R4-01, R4-02; MA-1264). Supersedes the strict-reading bullet above.**
>   A file reads with certainty only when every line is a shape Thunderbird itself writes: empty, a
>   `//` comment, or exactly `user_pref("NAME", VALUE);`, NAME with no quote or backslash, VALUE
>   `true`, `false`, a 32-bit whole number or a double-quoted string with only the `\\`, `\"`, `\n`
>   and `\r` escapes StrEscape writes; all LF or all CRLF, no lone CR, no NUL, no BOM. Anything else
>   anywhere is "can't tell". The name rule is wider than the lead's `[A-Za-z0-9._-]+` on purpose:
>   Thunderbird writes `mailnews.tags.$label1.tag` and `=20` tag keys into every profile whose tags
>   it has set up, and the narrower rule would call those files "changed outside Thunderbird".
> - **Paths by the OS's rules (R4-03; MA-1265).** A backslash separates folders on Windows only.
> - **`-sync-wait` with several stores (R4-04; MA-1266).** Check every store, change every store with
>   Thunderbird closed, say "START Thunderbird" once, then wait for every store.
> - **The wizard after a change (R4-05; MA-1267).** A later refusal in the same run says what was
>   changed and that it is kept, and says "nothing changed" of this mail only.
> - **The words (Q1 to Q5, R10's words; MA-1268).** R10's exit code stays 0 (the lead's call).
>
> **Folded 2026-11-07 [foldcheck M6] (lead's call):** the bullet "No match → `unknown` if a
> folder-cache entry exists" is **superseded by [foldcheck M6]**. No match → `unknown` whether or not
> the cache could be read, unless no `prefs.js` is found above the input **and** no path part is
> `ImapMail` (§2.2 fold's M6 table).
>
> *The account's name.* An account is named by its **store folder name** (`imap.gmail.com`,
> `imap.gmail.com-1`), so two accounts on one server are told apart. That name is no more personal
> than the host. §7's "named by its server" is read as "named by its store folder".
>
> **Step 2 [TC-15] (review condition), [TC-3] (review condition), [TC-7] names.**
>
> *Opening the file.*
>
> - Open with `util.ReadRegularFile(path, 16 MB)` (`internal/util/openregular.go:67`), the opener
>   behind the MA-767 precedent. A link is refused, not followed. A pipe or device is neither waited
>   on nor read. A file over 16 MB is refused, never read whole. Each refusal → `unknown`.
> - The file is kept in memory only, for this run. Nothing writes it, ever.
>
> *Decoding.*
>
> - Decode with a `json.Decoder` using `UseNumber`. The top level must be an object.
> - The top level is walked **token by token**, so a key that appears twice is seen. Go's plain
>   decoding keeps the last one silently. A duplicated key that matches this store → `unknown`.
> - A value that is not an object is skipped and counted. If the skipped key is a kept folder of
>   this store, or is one the walk read, the account is `unknown`.
> - Numbers follow step 3's validation. `1e308`, `1.5` and anything else that is not an integer is
>   invalid. `-0` reads as 0.
>
> *Matching keys to the store (replaces the tail-only matching in step 2 above).*
>
> 1. Normalise both sides: Unicode NFC; `\` and `/` alike; case-folded on Windows and macOS;
>    compared by whole path components (TC-10). So `ImapMail/imap.gmail.com` never matches keys of
>    `ImapMail/imap.gmail.com-1`.
> 2. **Exact first.** Match each key against the store's current absolute path.
> 3. **Tail matching** (the key's path under `ImapMail/<host>/` or the store's own folder name) is
>    used only when **no** key of this store matches exactly, which is the moved or restored profile.
> 4. **Two keys for one folder** (by tail, or one exact plus one tail) → `unknown`. An old-path key
>    left behind after a move (Thunderbird bug 1726660, `nsMsgDBFolder.cpp:1846-1849`) then cannot
>    win.
> 5. Keys are compared as strings only and never opened, as above.
>
> MA-1170 gains a fixture with an old-path and a current-path key for one folder.
>
> **Folded 2026-11-07 [foldcheck M2] (lead's call): one key-matching rule.** Items 3 and 4 above are
> **superseded by [foldcheck M2]**; they contradicted each other. If item 4 won, every moved or copied
> profile was `unknown` for good, because Thunderbird removes old-path keys only when a folder is
> deleted. If item 3 won, a kept folder with only an old-path key was invisible and its gap hidden.
> Items 1, 2 and 5 stay. The rule, per store:
>
> 1. **Exact mode** applies when **any** key of the store matches the store's current path exactly
>    (after item 1's normalising). In exact mode:
>    - an old-path key for a folder that **also** has an exact key is **ignored** (left behind by the
>      move, bug 1726660);
>    - two exact keys for one folder → `unknown`;
>    - an old-path key for a folder with **no** exact key: that **folder** is "can't tell" (next
>      paragraph).
> 2. **Tail mode** applies only when **no** key of the store matches exactly (a moved or restored
>    profile Thunderbird has not opened since). Keys are matched by their tail under the store; two
>    tail keys for one folder → `unknown`.
>
> **A "can't tell" folder, precisely.** It applies only to a folder that has no current-path entry
> but is still in the current store, meaning the walk read its mailbox file, or its `.msf` exists at
> the current path. That is checked with `Lstat` on a path built from the store folder and the key's
> tail, never the key's own path, and nothing is opened.
>
> - Such a folder is left out of X and Y. The account may not be `caught_up` while it has one:
>   - it is `behind` if the account's other folders already show missing above the tolerance (a gap
>     elsewhere is certain, and the tolerance is then taken from the smaller Y, which fails toward
>     "behind");
>   - otherwise it is `unknown`, reason `old_path_only`, with the "could not read Thunderbird's list
>     of folders" words and the number of such folders.
>   - for Gmail, if All Mail is the "can't tell" folder, All Mail is treated as absent and Y is a
>     floor (§3 fold, M5), so the account is `behind` or `unknown`, never `caught_up`.
> - **It is never forever for the whole account.** It clears for a folder as soon as Thunderbird
>   writes that folder's current-path entry, which it does at its next save once it has the folder
>   loaded, at the latest when it closes (`Flush`, §2.3).
> - **A folder that is no longer in the current store** (no mailbox file read and no `.msf` at the
>   current path) has an old-path key left over from before a move or delete. That key is ignored and
>   counted in the record, so a deleted folder cannot hold the account at `unknown`.
> - [conditional: that Thunderbird writes a current-path entry for every folder it has loaded, and
>   that a folder it knows always has a `.msf` in the store. Both are read at trunk only in outline.
>   They are added to the M4 re-read (§16 fold) and to lab row MA-1200's moved profile.]
>
> **Folded 2026-11-07 [M4 re-read] (the lead's re-read, done by a delegated agent):** the
> condition above is **[proven at ESR 153.6.0esr, in source]**, with one limit
> (`docs/lab/thunderbird-completeness-source-esr153.md` claims 2b and 9).
>
> - **Entries.** At shutdown Thunderbird writes every folder of every server's tree at its current
>   key, deep (`nsMsgAccountManager.cpp:226` → `nsMsgIncomingServer.cpp:297-309` →
>   `nsMsgDBFolder.cpp:1186-1216`). Before that, a single folder is written whenever its totals are
>   updated (`:3851`).
> - **`.msf` files.** An IMAP account's folders are found at start-up by listing the store's `.msf`
>   files (`nsImapMailFolder.cpp:404-508`), so every folder Thunderbird loads from disk has one.
> - **The limit.** "At the latest when it closes" above means a **clean** close. A crash or forced
>   quit skips the shutdown write, and the "can't tell" folder then lasts until a later clean close
>   or totals update. That is still never `caught_up`, so it fails safe. A folder first created
>   during a session was not traced.
> - MA-1200 still confirms all of this on a real moved profile.
>
> **Folded 2026-11-07 [completeness foldcheck2 L2] (lead's call): a key's tail is checked before
> the `Lstat`.** The "can't tell" check above builds its path from the store folder and the key's
> tail, and the key is untrusted. Before that `Lstat` is made:
>
> - a tail with any `.` or `..` part, or an absolute tail (a leading `/` or `\`, a drive letter, or
>   a UNC prefix), is **refused**: no `Lstat` is made for it;
> - the refused key is treated as an invalid entry, so the account is `unknown` (reason
>   `invalid_entry`, as M3). That is the cautious reading, since Thunderbird never writes such a key
>   (the folder's reading; C1a's adversarial pass may narrow it to "ignored and counted").
> - MA-1170 gains the cases (§12 fold).
>
> **Folded 2026-11-07 [completeness foldcheck2 L3] (lead's call): a mailbox with no cache key.** M2
> did not say what happens to a mailbox the walk read that has **no** key at all, in either mode
> (for example a folder left on disk after it was unsubscribed, §11).
>
> - Its messages are still archived, as today.
> - It **never adds to X** for a Gmail account (§3). Otherwise its messages could cover a gap in
>   All Mail, as N5's extra separators can.
> - It is **counted instead** as one number in the run's plaintext record: the walked mailboxes with
>   no cache key, and the messages in them. No folder names, and nothing beside an encrypted archive
>   (R44). It changes no verdict.
> - For an account that is not Gmail it changes nothing: only kept folders listed in the cache are
>   summed, so it was never in X.
> - MA-1175 gains the case (§12 fold).
>
> **Fixtures for MA-1170** (replacing "an old-path and a current-path key for one folder → `unknown`"
> in §12's fold):
>
> - a copied profile with old and new keys for every folder → checked, not `unknown`;
> - one folder with an old key only, its mailbox file on disk, the rest caught up → `unknown`
>   (`old_path_only`);
> - the same, with the other folders behind → `behind`;
> - an old key only for a folder gone from the current store → ignored, the account checked;
> - two exact keys for one folder → `unknown`;
> - tail mode with two tail keys for one folder → `unknown`.
>
> *Folder names for the words (TC-7).* A folder's human name is the matched key's path under the
> store, with `.msf` removed and each `.sbd` level shown as "/" (for example `[Gmail]/Sent Mail`).
> That is the same file-safe name the walk and the archive use. It is used even for a folder with no
> mailbox file, which is the usual case for a not-kept folder. `onlineName` is never shown: it is the
> server's raw name, possibly in modified UTF-7 **(recollection, unverified)**. Cleaning and capping
> are in §6's fold.
>
> **Step 3 [TC-2] (review condition): validate before sorting.**
>
> - **Entries that must validate.** Every non-root entry of the store needs `flags` (an unsigned
>   32-bit integer) and `totalMsgs` (a signed 32-bit integer), present as JSON integers.
> - **IMAP folders.** An entry with the ImapBox flag 0x2000 also needs `serverTotal`; `expungedBytes`
>   is needed as a signed 64-bit integer.
> - **Optional fields.** `pendingMsgs` and `folderSize`, when present, must be in range.
>   `lastSyncTimeInSec` may be absent (absent = never checked).
> - **Negative values.** Allowed only on the account root (the owner's roots carry `totalMsgs` = -1;
>   Thunderbird starts the count at -1, `nsMsgDBFolder.cpp:238`). Anywhere else, a negative value
>   makes the entry invalid.
> - **What an invalid entry does.** A kept folder that fails validation → the account is `unknown`.
>   A not-kept or ignored entry that fails is skipped and counted.
> - **Whether `expungedBytes` is required** is written at trunk for every folder (§2.1 fold). Slice
>   C0's ESR re-read (TC-14) confirms it for ESR 140. If ESR 140 does not write it, this rule is
>   decided again and recorded here before C1a is merged. Until then, absent → `unknown`.
> - **An account with no countable kept folder → `unknown`**, never `caught_up`.
> - **The one exception:** every entry validates, every kept folder is checked
>   (`lastSyncTimeInSec` > 0, empty ones included), every folder lists 0, and the walk found no
>   message. It reads "Thunderbird lists no mail for ACCOUNT". The never-connected `fresh-imap`
>   shape has no `lastSyncTimeInSec`, so it stays `unknown` (MA-1178).
> - **A walk that found messages where every folder lists 0 → `unknown`.**
> - MA-1171 gains: a missing `totalMsgs`, a string `totalMsgs`, an out-of-range number, and a
>   negative count on a non-root entry, each → `unknown`.
>
> **Folded 2026-11-07 [foldcheck M3] (lead's call): a broken `flags` counts as kept.** When `flags`
> is the field that fails (missing, a string, out of range, not an integer), "kept or not" cannot be
> told. As written, "a not-kept entry that fails is skipped" let a never-downloaded folder with a
> broken `flags` drop out unseen, and since nothing is on disk the walk never sees it either.
>
> - An entry whose `flags` fails validation is **treated as kept**, so the account is `unknown`
>   (reason `invalid_entry`).
> - A failure in any **other** field of an entry whose `flags` is valid and says not kept (or
>   ignored) may still be skipped and counted, as above.
> - MA-1171 gains a non-root entry with a broken `flags` and nothing on disk → `unknown` (§12 fold).
>
> **Folded 2026-11-07 [foldcheck M4] (lead's call):** "confirms it for ESR 140. If ESR 140 does not
> write it" above is **superseded by [foldcheck M4]**: read "ESR 153" for "ESR 140". Until the re-read
> has recorded it in §16, an absent `expungedBytes` → `unknown`, as written.
>
> **Folded 2026-11-07 [M4 re-read] (the lead's re-read, done by a delegated agent):** settled.
> `expungedBytes` is written as a signed 64-bit number for **every** folder at ESR 153.6.0esr
> (`nsMsgDBFolder.cpp:1232`) and at Release 159.0 (`:1246`). Every field name and type in §2.1's
> fold holds at both versions (`docs/lab/thunderbird-completeness-source-esr153.md` claims 1a and
> 1b). So "absent → `unknown`" stays as the rule, with nothing left to decide. An absent value means
> the entry was not written the way ESR 153, Release 159 or trunk write it, so `unknown` is right.
>
> **Folded 2026-11-08 [C1a pass C1A-01] (lead's call):** the "Negative values" bullet above
> ("allowed only on the account root") is **superseded for `folderSize` and `totalMsgs`**. Its
> "Thunderbird starts the count at -1" is true of **every** folder, not only the root: every folder
> starts at `mFolderSize(kSizeUnknown)` and `mNumTotalMessages(-1)` (`nsMsgDBFolder.cpp` ESR 153
> :243 and :236; `kSizeUnknown` is -1, `MailNewsTypes.h` at the comm-central tip) and writes both as
> they are (:1228, :1233). The owner's real profile held `folderSize` -1 on 4 of its 35 folders,
> which the old rule called invalid, so every such account would have been `unknown`.
>
> - `folderSize` exactly -1 off the root → **missing** ("Thunderbird has not worked out the size
>   yet"). It is optional, so the entry stays valid. Any other negative `folderSize` → invalid.
> - `totalMsgs` exactly -1 off the root → the entry is **not valid** (Thunderbird has no count
>   either), with a reason of its own, **"not counted by Thunderbird yet"**, never `invalid_entry`'s
>   "changed outside Thunderbird". C1a gives it as the field state `NotCounted` and
>   `Entry.NotCountedYet()` (true only when that is the entry's one failure); C1b words it. Any other
>   negative `totalMsgs` → invalid.
> - `pendingMsgs` and `serverTotal`: negative → invalid, as before (the pass's source reading: both
>   start at 0 and only grow or reset to 0).
> - `expungedBytes`: negative → invalid, kept as before. **Unsettled:** it is taken from the folder's
>   database (:669), whose arithmetic (`nsDBFolderInfo`) was not read, so a real negative value is not
>   ruled out. If a real profile shows one, this rule is decided again.
> - The account root is still never validated. MA-1272 proves this.
>
> **Folded 2026-11-08 [C1a pass C1A-07] (lead's call, with the real-profile constraint):** the
> "IMAP folders" bullet above ("an entry with the ImapBox flag 0x2000 also needs `serverTotal`") is
> **superseded**: **every** non-root entry of an IMAP store needs `serverTotal`. Every folder of an
> IMAP account's tree is an IMAP folder (`nsImapMailFolder::AddSubfolderWithPath` refuses a child that
> is not, `nsImapMailFolder.cpp` ESR 153 :353-356), and an IMAP folder writes `serverTotal` whatever
> its flags (:1901; Release 159 :1974). ImapBox is set in one place only, once the server has listed
> the folder (`SetBoxFlags` :1107). So the owner's 2 real folders with no ImapBox and no
> `lastSyncTimeInSec` keep `serverTotal` and stay valid, as does the Windows lab's never-connected
> INBOX. An entry with no `serverTotal` is one Thunderbird did not write. **Residual:** which kind of
> real folder lacks ImapBox (one the server has not listed yet, or a saved search) was not traced; the
> rule does not depend on it. MA-1277 proves this.
>
> **Step 3, Gmail labels [TC-8] (review condition).** For a Gmail account whose All Mail sets Y
> (§3 fold), a label folder that is not kept is **not** a "not kept" WARN. Its mail is backed up
> through All Mail. It is at most one note for all of them (§6 fold). Only a not-kept All Mail is
> reported as not kept, plus Spam and Trash as today's note.
>
> **Step 4 [TC-9] (review condition).** Also read `mail.server.serverN.autosync_offline_stores`
> (default true, `mailnews.js:621`). When it is false, Thunderbird never downloads in the background
> (`nsAutoSyncManager.cpp:819-825`) and never sets `lastSyncTimeInSec`. The account is then
> **`limited`**. It is decided before the never-checked rule, so it does not land on `unknown`
> and its useless "leave it open". Words in §6's fold. The "not logged in this session" skip
> (`:844-859`) needs no verdict of its own; the stopped notice's remedy covers it.
>
> **Step 5 [TC-4, TC-16].** The order is fixed: count on disk; subtract the expunged estimate
> (§2.4 fold); clamp at 0; compute missing per §4.3's fold; then apply the tolerance once per
> account.

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

> **Folded 2026-11-07 [TC-1, TC-2, TC-3, TC-4, TC-5, TC-9, TC-11, TC-12, TC-16] (the verdict table,
> as amended).** The table above is read with these changes.
>
> **`caught_up` needs all of these:**
>
> 1. the account's `type` is `imap`;
> 2. the walk covered the whole store;
> 3. the cache was read and is not Panorama;
> 4. no duplicate key matches the store;
> 5. every kept entry validates;
> 6. at least one kept folder was compared, or the "lists no mail" exception applies (§4.1 step 3
>    fold);
> 7. every kept folder that lists mail is checked, and none in the future;
> 8. for Gmail, All Mail sets Y (not a floor);
> 9. X > 0 whenever Y > 0;
> 10. missing, after the expunged estimate, is within the tolerance (§4.3 fold).
>
> **"Complete" words are narrower than `caught_up`.** "Export complete." and "everything it lists"
> are said only when the verdict is `caught_up`, **missing is 0**, the "as of" is not stale, and no
> TC-22 cap note shows. A `caught_up` account with 0 < missing ≤ tolerance gets the lead "Backup
> finished." and both numbers (§4.3 fold). This is the folder's reading of R42: Thunderbird's own
> counts then still show mail not downloaded.
>
> **The other verdicts:**
>
> - `behind`: missing > tolerance, or a Gmail floor case with X < floor − tolerance.
> - `limited`: behind, and one of these is set: `autosync_max_age_days` > 0, the size limit, or
>   `autosync_offline_stores` false (TC-9). With `autosync_offline_stores` false the account is
>   `limited` even when never checked.
> - `unknown`: every path in §4.4's fold.
> - `not_applicable`: only `type` `pop3`, `none` or `rss`, or a plain mail folder that is no
>   Thunderbird store (§2.2 fold). An Exchange account is no longer `not_applicable`.
>
> **A run with several accounts (the folder's note, found in the §17 re-read).** The run's lead is
> set by its weakest account:
>
> - any `behind` or `limited` → "Backed up what Thunderbird has so far.";
> - otherwise, any `unknown`, any `caught_up` that may not say complete (above), or any stale
>   account → "Backup finished.";
> - otherwise → "Export complete.".
>
> One account's "complete" never speaks for another. MA-1181 gains a two-account run, one caught up
> and one unknown → "Backup finished.".
>
> **Folded 2026-11-07 [foldcheck M2, M3, M5, M6, M7] (lead's call): the verdict rules, as amended
> again.**
>
> - **`caught_up` also needs** (adding to the list above): no "can't tell" folder (M2, §4.1 step 2
>   fold); and for Gmail, All Mail is the one folder carrying 0x8000 (M5, §3 fold). Condition 5
>   "every kept entry validates" now includes every entry whose `flags` is broken (M3).
> - **"Complete" words also need** `expungedBytes` = 0 on every kept folder of the account, until
>   MA-1199 has run (M7, §2.4 fold). The full test for "this account may say complete" is therefore:
>   `caught_up`, missing = 0, not stale, no TC-22 cap note, and `expungedBytes` = 0 on every kept
>   folder. The run's lead rule above uses this test unchanged.
> - **`not_applicable`:** the bullet above ("only `type` `pop3`, `none` or `rss`, or a plain mail
>   folder that is no Thunderbird store") is **superseded by [foldcheck M6]**: `not_applicable` only
>   for a **matched** account of type `pop3`, `none` or `rss`, or when no `prefs.js` is found above the
>   input **and** no path part is `ImapMail` (§2.2 fold). Every other case is `unknown`.
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call):** the second half of the
> `not_applicable` bullet above also needs the store to hold **no** `.msf` summary file (§2.2
> fold). With no profile found and no `ImapMail` part, a store that holds any `.msf` is `unknown`,
> never `not_applicable`.
>
> **Folded 2026-11-07 [completeness foldcheck2 L4] (lead's call): not-kept folders and "complete".**
> As written, a `caught_up` account with not-kept folders still leads "Export complete.", beside a
> not-kept WARN (and, after TC-19, beside a note). The sentence above ("'Not kept' folders are
> reported **alongside** any verdict … never folded into 'behind'") stands for the **verdict**. For
> the **words**, the lead's call is:
>
> - an account that gets the `thunderbird_not_kept` WARN (a not-kept folder that lists mail, §6), or
>   that WARN's TC-19 follow-up note for the same folders, **may not say complete**. Its lead is
>   "Backup finished." at most, and `-json` `complete` is false for it. R42 says never "complete"
>   while Thunderbird's counts show mail not yet downloaded, and a not-kept folder's mail is not
>   downloaded.
> - **Open item for C3:** the cases that get a **note only** (Spam and Trash not kept by default;
>   Gmail labels not kept while All Mail is kept, whose mail is backed up through All Mail). Whether
>   they too block "complete" is not decided here. C3's builder brings it to the lead before C3's
>   friction review. (A note for the lead: Trash usually lists mail, so if default-not-kept blocks,
>   few accounts would ever say complete.)
> - MA-1181 and MA-1183 gain the WARN case (§12 fold).

### 4.3 Tolerance

**Recommendation:** missing ≤ max(20 messages, 0.2% of Y) counts as caught up. For Y = 238,080
that is 476 messages.

It absorbs mail that arrived while the backup ran, deletions on the server since Thunderbird's
last check, and messages with no body (Thunderbird skips zero-byte messages:
`nsAutoSyncState.cpp:271-275`). It is small enough that a real first-download gap (1,670 of
238,080) is never caught up. The figure is an owner question (§14 Q1).

> **Folded 2026-11-07 [TC-4] (owner default O2 + review condition):** the tolerance is fixed, and
> fixed at both ends.
>
> - **Missing, for an account that is not Gmail:** the sum over kept folders of max(0, listed − on
>   disk), where "on disk" is after the expunged estimate. Each folder is clamped at 0 **before** the
>   sum, so a folder holding extra copies cannot hide another folder's gap.
> - **Missing, for Gmail:** max(0, Y − X).
> - **Tolerance:** max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), applied **once per account, never per folder**.
>   It replaces "max(20 messages, 0.2% of Y)" above. Worked values:
>
>   | Y listed | Tolerance |
>   |---|---|
>   | 15 | 1 |
>   | 100 | 10 |
>   | 10,000 | 20 |
>   | 238,169 | 476 |
>
> - **Nothing on disk is never caught up:** X = 0 with Y > 0 is always `behind` (or `unknown` on a
>   path in §4.4's fold).
> - **Both numbers whenever any mail is missing.** When 0 < missing ≤ tolerance, the words say
>   "Thunderbird has downloaded about X of the Y messages it lists for ACCOUNT. The rest is likely
>   mail that arrived or was deleted since Thunderbird's count." The lead is "Backup finished."
>   (§4.2 fold). "Everything" is said only when missing = 0.
> - **The high end:** a 1,000,000-message account may miss up to 2,000 and still be `caught_up`. It
>   still shows both numbers, so the words never hide the gap.
>
> MA-1174 gains: listed 15 with 0 on disk → `behind`; two folders, one +30 and one −30 → `behind`;
> 0 < missing ≤ tolerance → `caught_up`, with both numbers and no "complete".

### 4.4 What MailArchive says when it cannot tell

`unknown` is never `caught_up`. An IMAP account whose state is unknown gets the neutral
"Backup finished", never "complete", and one note in Status & health:

> MailArchive could not read Thunderbird's list of folders for imap.gmail.com, so it cannot tell
> whether Thunderbird has downloaded all of it.

This is a note, not a warning, because the backup itself is fine.

A kept folder never checked gets:

> Thunderbird has not checked the server for this account yet, so MailArchive cannot tell how
> much is still to come. Open Thunderbird and leave it open.

> **Folded 2026-11-07 [TC-1, TC-2, TC-3, TC-5, TC-11, TC-15, TC-16] (every path to `unknown`, in
> one place).** Each path below gives the lead **"Backup finished."** and never "complete",
> "everything" or "Export complete.". Each gets a note in Status & health, not a WARN.
>
> | Path | Words (after "MailArchive cannot tell whether Thunderbird has downloaded all of ACCOUNT") |
> |---|---|
> | file missing, a link, a pipe, over 16 MB, not JSON, not an object (TC-15) | "…because it could not read Thunderbird's list of folders" (the sentence above) |
> | no key matches the store, or two keys match one folder (TC-3) | the same |
> | a kept entry fails validation, a non-object value for a kept or walked folder (TC-2, TC-15) | the same |
> | no countable kept folder; or messages on disk while every folder lists 0 (TC-2) | the same |
> | `panorama.sqlite` present, or `mail.panorama.enabled` true (TC-3) | "…because this Thunderbird uses its newer storage, which MailArchive cannot read yet" |
> | `type` `ews`, other or empty; no matched account; `prefs.js` unreadable (TC-5) | "…because MailArchive does not yet know how this kind of Thunderbird account keeps its mail" |
> | a kept folder with mail never checked, `lastSyncTimeInSec` ≤ 0 (unless `limited` by TC-9) | the "has not checked the server yet" sentence above |
> | Gmail floor with X ≥ floor − tolerance (TC-1, O3) | O3's "lists at least Y…" sentence (§3 fold) |
> | `expungedBytes` > 0 with nothing to divide by (TC-16) | "…because Thunderbird holds deleted copies MailArchive cannot count" |
> | a partial `-input` (TC-11) | "Only part of this Thunderbird account was backed up, so MailArchive did not compare it with Thunderbird's count." (a note in `status`; no WARN) |
>
> **Wording note.** The existing "could not read" sentence above names the account by its store
> folder name (§4.1 step 1 fold). It reads "this account" when the store folder name is not known.
>
> **Folded 2026-11-07 [foldcheck M1, M2, M3, M6, M7] (lead's call): rows for the table above.**
>
> | Path | Words |
> |---|---|
> | a folder with only an old-path key that is still in the current store, the rest not behind (M2) | "…because it could not read Thunderbird's list of folders", plus the number of such folders; reason `old_path_only` |
> | an entry whose `flags` fails validation, kept or not (M3) | the same "could not read" words; reason `invalid_entry` |
> | no matched account (ambiguous, `prefs.js` unreadable or refused by the opener, M1), or a copied `ImapMail/<host>` with no profile above it, **whether or not the cache could be read** (M6) | "…because MailArchive does not yet know how this kind of Thunderbird account keeps its mail"; reason `no_match` or `account_type` |
>
> The TC-3 row above ("two keys match one folder") now means two exact keys, or two tail keys in
> tail mode; an old-path key beside an exact key is ignored (M2). The TC-5 row above ("no matched
> account; `prefs.js` unreadable") now covers the unreadable-cache case too (M6). M7 is **not** an `unknown` path: an account with `expungedBytes` > 0 keeps its
> verdict, but its lead is "Backup finished." and its words show both numbers (§2.4 fold).
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call): one more row.**
>
> | Path | Words |
> |---|---|
> | no profile found above the store, no `ImapMail` part in the store's path, and the store holds any `.msf` summary file (§2.2 fold) | the M6 row's "…does not yet know how this kind of Thunderbird account keeps its mail"; reason `no_match` |

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

> **Folded 2026-11-07 [Q2, Q3] (owner defaults):** both are confirmed as written.
>
> - A behind backup is a success with a note: record `ok`, the alert streak not moved, nothing
>   partial.
> - WARN `thunderbird_behind` shows from the first behind backup, on every behind run.
>
> **Folded 2026-11-07 [TC-19] (lead's call L3):** a setting the person chose gets a WARN once, then
> a note.
>
> - **Which verdicts:** `limited` (WARN `thunderbird_limited`) and not-kept folders (WARN
>   `thunderbird_not_kept`, outside the Gmail case in §4.1 step 3's fold). Each is a WARN on the
>   first run that shows that setting, then a note while the setting is unchanged.
> - **The fingerprint**, in the plaintext last-run record per account:
>   - for limits: a hash of (`autosync_max_age_days`, the size limit and its KB, and
>     `autosync_offline_stores`);
>   - for not-kept folders: a hash of the sorted, normalised folder paths under the store;
>   - each with the time it was first seen.
> - **Carrying it forward.** The run reads the previous last-run record before it writes the new
>   one (`newRunRecord` builds a fresh record, `run.go:1218-1235`), and carries the first-seen time
>   forward when the hash is unchanged.
> - **When the WARN shows.** When the hash is new on this run (no previous hash, or a different one).
>   A changed setting therefore WARNs again once.
> - **What does not change.** `thunderbird_behind` is not a chosen setting: it stays a WARN on every
>   behind run (Q3).
> - **Rows:** MA-1182 and MA-1183 gain "a second run with the same setting → note", and "a changed
>   setting → WARN again".
>
> **Folded 2026-11-07 [TC-20] (review condition; owner default Q2):** "the same email slot" means
> this:
>
> - each streak sends **at most one** still-downloading email and **at most one** stopped email;
> - a stopped email may follow a still-downloading one, never the reverse;
> - a streak that stalls from its start sends only the stopped email.
>
> The owner's "one email after 3 days behind/stalled" is read as one email of each kind per streak
> (the folder's reading). If the owner meant one email in total per streak, that is a small change:
> drop the stopped email after a still-downloading one. MA-1184 gains the sequence.
>
> **Folded 2026-11-07 [the streak and the other verdicts] (the folder's reading of TC-19 and of the
> review's lens 3).** These are in the folder's own words, and the lead should confirm them.
>
> - Only `behind` runs start or extend a behind-streak.
> - A `limited` run ends a streak without an email: the gap is then a chosen setting, said by
>   TC-19's WARN-then-note.
> - An `unknown` or `not_applicable` run neither extends nor ends a streak. The 3 days are measured
>   between the `behind` runs either side of it.
> - Without this, a `limited` account would hold a streak open for good and send a stopped email
>   for a setting the person chose.
>
> **Folded 2026-11-07 [TC-6] (owner default O1):** **an encrypted archive gets no notices and no
> email about completeness**, and no streak is kept for it (§9.2 fold). Its WARNs and notes appear
> only on screen while it is unlocked (§9.2 fold). `status` shows nothing about completeness for an
> encrypted archive, because nothing is written beside it.
>
> **Folded 2026-11-07 [L2] (lead's call):** the notices and their email (slice C5) ship **right
> after C3's friction review**, so the email words are walked once the screen words exist.
>
> **Folded 2026-11-07 [foldcheck M10] (lead's call): the behind streak gets its own small record.**
> "It is computed from the run history's compact per-account `{verdict, x, y}` (§9.1). It needs no
> new streak file for plaintext archives" (above) is **superseded by [foldcheck M10]**. The run
> history keeps 50 entries (`RunHistoryMax`, `internal/state/runhistory.go:26`), which is about 50
> hours on an hourly schedule, so a 3-day notice could never fire. Nothing would record "this
> streak's email was sent" once the streak's first run had dropped out of the history either.
>
> - **Where.** A per-archive dotfile, `.mailarchive-tbstreak.json` (the name is provisional), built
>   like the presync streak file `.mailarchive-syncstreak.json` (`internal/state/syncstreak.go:15-60`):
>   a version, a size cap, a `Serial` that rises only when a new streak starts, and a `RecordID`
>   chosen at random when the record is first written. The once-per-streak emails key on both, as
>   presync's do.
> - **What it holds, per account** (by store folder name): the first behind run's time and X then;
>   the latest behind run's time; the latest time X grew, and X then; the number of behind runs; and
>   two flags, "still-downloading email sent" and "stopped email sent". No folder names and no Y
>   history.
> - **How it is used.** The §5 rules stay as written: only `behind` runs start or extend a streak; a
>   `limited` run ends it silently; an `unknown` or `not_applicable` run neither extends nor ends it;
>   a `caught_up` run ends it with no email. The 3 days are measured **between runs**, never against
>   the clock. A clock that went back restarts the first time and keeps the serial, as presync does.
>   Still-downloading fires at 3 days or more with at least 2 runs; stopped fires when X has not
>   grown across behind runs spanning 3 days; each at most once per streak (TC-20).
> - **Plaintext only.** An encrypted archive keeps no streak (O1). A keyed run removes a stray
>   `.mailarchive-tbstreak.json`, as it does the presync one, so MA-904's checks still pass unchanged.
> - The run history's compact `tb` field stays, for Recent backups only.
> - MA-1184 gains an hourly schedule run for 4 days: the still-downloading email fires once, after
>   the history has wrapped (§12 fold).
>
> **Folded 2026-11-09 [C5 pass: FALSE-01, FALSE-02, MISS-01, PLANT-01] (the lead's calls):** the
> stopped rule above was too easy to trip, and the record trusted any date it held.
>
> - **Stopped needs 3 days and 3 backups (FALSE-01).** X has not grown for 3 days since the last
>   progress, **and** at least 3 behind backups in that span saw no progress. A computer switched off
>   for days and then backed up once trips nothing, as this section's first paragraph says. The
>   still-downloading email keeps MA-891's rule (between runs, a gap counts).
> - **Progress is against the previous backup (FALSE-02).** X above the previous behind backup's X,
>   never above the streak's highest. X that fell (a bulk delete, a folder repaired) and grows again
>   is progress; a fall is not.
> - **A wrong clock (MISS-01).** A plausible backup whose streak has no progress time (its first
>   backup's clock was before MinPlausibleTime: a dead clock battery) or one later than this backup
>   starts the progress time at this backup.
> - **The record's dates (PLANT-01).** A record holding a date before 2020-01-01, a date more than a
>   day after the run's clock (when that clock is plausible), a first time after a last one, or one
>   behind run with two times, is damaged, as R4-01's counters: a fresh streak and a
>   `tbstreak_unreadable` WARN. So a clock put back more than a day also starts the streak again,
>   with that WARN.
> - The record gains `last_x` and `no_progress_runs`.
> - **Not settled, for the confirm round:** on an hourly schedule, after a long switch-off, the 3
>   backups without progress can come within about 2 hours of the computer coming back. The rule is
>   met, but Thunderbird has then had only those hours to download.
>
> **Amended 2026-11-09 [the lead's call on the item above]:** the backups without progress that
> "stopped" counts must also span **at least 24 hours**, from the first of them to the latest
> (`state.TBStoppedSpan`; the record gains `first_no_progress`, the first such backup since the
> latest progress, set only by a backup with a plausible clock, cleared by progress, by a clock that
> went back and by MISS-01's restart). So "stopped" needs all three: 3 days since the last progress,
> 3 backups without progress, and those backups spanning a day.
>
> - Hourly, after 4 days off: the first backup back gets the still-downloading email (MA-891's rule,
>   unchanged); the backups of the next 23 hours (and at 23h59) send nothing; the backup 24 hours
>   after the first one back sends the stopped email, once.
> - Daily: unchanged. The third backup without progress is already 48 hours after the first.
> - A record written before `first_no_progress` sets it at its next backup without progress: the
>   stopped email comes a day later, never falsely.
> - The new date is checked as the others (PLANT-01): before 2020, more than a day ahead, before the
>   latest progress or after the latest behind backup is damaged.
> - **Still open (for the confirm round):** the window starts at the first backup without progress,
>   which can be BEFORE the switch-off. An hourly schedule whose last backups before the switch-off
>   already saw no progress (say Thunderbird was quit an hour or two before the computer was turned
>   off) has a window that began days ago, so the first backup after the computer comes back (with 2
>   such backups before the switch-off) can still send "stopped". The real scheduled-run test
>   (`TestTheScheduledRunSendsTheDownloadEmailsOncePerStreak`) passes through this case. Closing that needs a rule about gaps between backups (for example, a gap longer
>   than the window restarts it), which would hold back a daily or weekly schedule whose backups
>   drift. Not built; a call for the lead.
>
> **Amended 2026-11-09 [C5 pass round 2: CLOCK-R2-A, WARN-R2, PLANT-R2, SPAM-R2-A] (the lead's
> calls on `docs/review-thunderbird-completeness-c5-adversarial-r2.md`):** PLANT-01's "a date more
> than a day ahead is damaged" threw away the sent flags whenever a clock was behind, so a dead
> clock battery sent the still-downloading email at every cold start ("610 days").
>
> - **A clock behind is not counted (CLOCK-R2-A, MA-1290).** A backup whose clock is more than a
>   day earlier than any date in the record records nothing: no reset, the flags kept, no email
>   from that run. It logs one line and leaves the `tbstreak_clock_behind` WARN: "This backup's
>   clock read DATE, earlier than a previous backup of this archive, so MailArchive did not count it
>   toward how long Thunderbird has been downloading. Check this computer's date and time."
> - **Why a date ahead is no longer damage.** A record a day or more ahead of the clock cannot be
>   told from a clock a day or more behind, and the clock behind is the real case. The cost, a
>   documented residual: a planted date ahead (in order; it needs write access to the archive
>   folder) freezes the count until the clock passes it, with the WARN at every backup. A clock
>   that once ran more than a day ahead does the same, and its WARN then names the right date.
> - **A month-long gap restarts the streak's times** (serial and flags kept), as a clock that went
>   back does: a streak whose first backup read a firmware date never says "still going after 610
>   days". The pass's dead battery now sends one email over 6 cold starts.
> - **Damaged is only what is impossible in the record itself** (MA-1281): counters, dates before
>   2020, times out of order, and now pairs no backup writes apart (PLANT-R2): `first_no_progress`
>   without `no_progress_runs` or the reverse, `first_no_progress` without `last_grew`, as many
>   backups without progress as backups, `stopped_sent` without `still_downloading_sent`, a
>   failed-send count past 5. Its WARN no longer mentions clocks or says "Nothing is needed"
>   (WARN-R2): "…because it holds counts or dates that do not fit together, so it started that count
>   again at the last backup. If this shows after every backup, something other than MailArchive
>   is changing that file."
> - **At most 5 tries per email per streak (SPAM-R2-A, MA-1291).** Failed sends are counted beside
>   each sent flag (`still_downloading_tries`, `stopped_tries`); at 5 the email is not tried again
>   and every due run adds `alert_send_failed` to the last-run record: "MailArchive could not send
>   its email about Thunderbird (5 tries). Check the email settings in MailArchive Desktop's
>   Settings, then use Send test email." The shipped sync email has the same cap
>   (design-presync-attended §5.5's amendment).
>
> **Amended 2026-11-09 [C5 pass round 3: CAP-R3-A, FRIC-R3-1, GAP-R3-C, TIE-R3] (the lead's calls
> on `docs/review-thunderbird-completeness-c5-adversarial-r3.md`):** the cap above could silence a
> mail server that worked again, for the rest of the streak, and its WARN's own remedy could not
> clear it.
>
> - **5 failed tries in a row, then at most one a day; never silent for good (CAP-R3-A, MA-1291).**
>   Under 5 failures in a row, every due backup tries. At 5, the email is tried once a day after
>   the latest failure (`state.AlertTryDue`; also when that latest is more than a day after the
>   clock, so a clock that ran ahead cannot freeze it).
> - **Any delivered email starts every count again.** Every email the mail server accepts (a
>   backup alert, the recovery, either email about Thunderbird, and Send test email on the Backup
>   alerts card) writes a new random id into `alert-delivered`, beside the Desktop settings file
>   (only a version and the id). Each failed send is counted with the id current at the time
>   (`tried_since`, and the time in `last_failed_try`, in the streak record and in the sync email's
>   told-file); counts made under another id read 0, and the next failure starts them again at 1.
>   So after Send test email goes, every archive's email about Thunderbird is tried at its next due
>   backup, without writing into any archive folder. Uninstall removes the file with the settings
>   files.
> - **The WARN clears once an email gets through.** `alert_send_failed` is added only by a run that
>   holds an email with 5 failures in a row and does not get an email through; each run writes its
>   last-run record afresh, so the WARN is gone from the first backup after an email gets through.
>   (Not built: removing it from the record at the moment Send test email goes; until the next
>   backup, Status & health still shows it. A call for the confirm round.)
> - **The words (the lead's, FRIC-R3-1):** `alert_send_failed`: "MailArchive tried 5 times to send
>   you an email about Thunderbird and could not. Open MailArchive Desktop, go to Backup alerts,
>   check the mail server details, then click Send test email. Once an email gets through,
>   MailArchive tries this one again at the next backup." `tbstreak_clock_behind`: "When this backup
>   ran, this computer's clock said DATE, which is earlier than a previous backup, so MailArchive
>   did not use this backup to judge how Thunderbird's download is going. Check this computer's
>   date and time. If it is wrong each time the computer starts, its clock battery may need
>   replacing."
> - **A clock equal to a stored time is not "later" (TIE-R3, MA-1290).** A backup whose clock is
>   before the latest behind backup went back, even when it reads exactly the streak's first time.
> - **Documented residual (GAP-R3-C, LOW, MA-1290).** A record with no dates yet, whose first
>   backup's clock reads 3 to 30 days slow, sends the still-downloading email one real hour into the
>   streak, saying "still going after N days" with N the clock's error; once per such streak. A fix
>   needs a sign that the first clock was wrong, and a 2-backup streak spanning 3 to 30 days is also
>   a daily schedule after a week switched off. Kept as a skipped test citing MA-1290.

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

> **Folded 2026-11-07 [TC-7] (review condition): folder names are text the mail server controls.**
>
> - **The name used** is the one in §4.1 step 2's fold: the key path under the store.
> - **Cleaning.** Every name passes `util.StripControl` (`internal/util/paths.go:162`). The Unicode
>   bidirectional controls are removed as well: U+202A to U+202E, U+2066 to U+2069, U+200E, U+200F
>   and U+061C.
> - **Length.** Each name is capped at 80 characters, plus "…".
> - **The not-kept list** shows the first 10 by listed count, then "and N more".
> - **Where it shows.** The Desktop renders names only through `html/template`. Email carries no
>   folder names (§9.3).
> - **Row:** MA-1198 plants control characters, a bidi override and a 10 KB name, and checks all four
>   surfaces: `status` text, the wizard's end summary, the Run now note, and the Desktop.
>
> **Folded 2026-11-07 [TC-8] (review condition): Gmail labels.** For a Gmail account whose All Mail
> sets Y (§3 fold), not-kept labels get one **note**, not a WARN and no "right-click each folder":
>
> > Some of imap.gmail.com's labels are not kept on this computer as folders of their own. Their mail
> > is still backed up through All Mail.
>
> A not-kept **All Mail** gets the WARN and the fix words. Spam and Trash keep the note above.
> MA-1185 gains the Gmail case: 40 not-kept labels with All Mail kept → one note and no WARN.
>
> **Folded 2026-11-07 [TC-9] (review condition):** words for `autosync_offline_stores` false:
>
> > Thunderbird is set not to download ACCOUNT's mail in the background, so MailArchive backs up
> > only what you have opened or downloaded by hand.
>
> The remedy names Thunderbird's own download-now command. Its label is **lab-required**
> (MA-1191). Until that row runs, the remedy reads: "In Thunderbird's Account Settings for ACCOUNT,
> turn on the setting that saves copies of this account's messages on this computer."
>
> **Folded 2026-11-07 [TC-13] (lead's call L5 + review condition): fix-it words by version.**
>
> - **Two sets of labels.** At trunk the account checkbox is "Save copies of all folders and messages
>   on this computer for offline use", under "Offline message storage" (`accountSettings.ftl:72-73`,
>   per the review). The size limit is "Skip downloading messages larger than:". The label "Keep
>   messages for this account on this computer" above appears at trunk only in SeaMonkey's help.
>   ESR 140 probably still shows the older page **(recollection, unverified)**.
> - **Choosing the set.** The words are picked by the Thunderbird version in the profile's
>   `compatibility.ini` `LastVersion`, read with the same opener and a 64 KB cap.
> - **The version where the label changed is not yet known.** Slice C0's ESR re-read (TC-14) and
>   lab row MA-1191 find it. Until both have recorded it here, every version gets the neutral words.
> - **Neutral words, also used for an unknown or unparsable version:**
>   - account level: "In Thunderbird's Account Settings for ACCOUNT, turn on the setting that saves
>     copies of this account's messages on this computer";
>   - size limit: "…turn off the limit on the size of messages it downloads";
>   - age limit: "…set it to keep messages of every age on this computer".
> - **What stays as it is.** The folder-level words ("Properties → Synchronization → Select this
>   folder for offline use", `folderprops.ftl`) are proven at trunk and stay, subject to the same
>   ESR check.
> - **Rows.** MA-1185 gains a version-chosen case and an unknown-version case (neutral words).
>   MA-1191 covers ESR 140 and the current Release, plus the wizard's existing "right-click the
>   account → Download / Sync Now" (`wizard.go:1622`, `:1634`), which is unverified too.
>
> **Folded 2026-11-07 [foldcheck M4] (lead's call):** every "ESR 140" in the TC-13 fold above ("ESR
> 140 probably still shows the older page", "MA-1191 covers ESR 140") is **superseded by [foldcheck
> M4]**: read **ESR 153** (153.6.0esr, the ESR the repo's labs ran) and the current **Release, 159.0**
> as seen on the Mac lab of 2026-11-06. Both version numbers are confirmed on Thunderbird's release
> page at the re-read (§16 fold).
>
> **Folded 2026-11-07 [M4 re-read] (the lead's re-read, done by a delegated agent):** the labels at
> the versions people run. Source: `docs/lab/thunderbird-completeness-source-esr153.md` claim 12,
> read at ESR 153.6.0esr (comm-esr153 c3fd7a609257) and Release 159.0 (comm-release 1e7f8bb744a4).
>
> - **At ESR 153 and Release 159 the account page is identical** (`am-offline.xhtml`,
>   `am-offline.dtd`). For an IMAP account, under Account Settings → "Synchronization & Storage"
>   (`prefs.properties:53`), it shows:
>   - group "Message Synchronizing": checkbox **"Keep messages in all folders for this account on
>     this computer"** (`allFoldersOffline2.label`);
>   - group "Disk Space": **"Synchronize all messages locally regardless of age"**
>     (`allAutosync.label`), or "Synchronize the most recent" N Days/Weeks/Months/Years;
>   - **"Don't download messages larger than" N "KB"** (`autosyncNotDownload.label`).
> - **Superseded by [M4 re-read]:**
>   - §6's "Keep messages for this account on this computer" is in no version read. Read "Keep
>     messages in all folders for this account on this computer".
>   - §6's "The size limit names 'Messages larger than … KB'" is wrong for IMAP. That label
>     (`offlineNotDownload.label`) is hidden for `imap` and `ews` accounts (`am-offline.xhtml:182`).
>     Read "Don't download messages larger than … KB".
>   - The TC-13 fold's "ESR 140 probably still shows the older page" now reads: ESR 153 and Release
>     159 both show the older page.
> - **The version where the labels change is 160.** "Offline message storage", **"Save copies of
>   all folders and messages on this computer for offline use"**, "Download limits" and **"Skip
>   downloading messages larger than:"** (`messenger/accountSettings.ftl:69-88`) are present at
>   160.0 beta 1 (comm-beta 22dd355df013) and in the 160.0 build 1 tag (comm-release f8677769eb91,
>   2026-11-06). They are absent at 159.0. Thunderbird 160 was not yet released on 2026-11-07. So
>   the rule is: `LastVersion` major below 160 → the older labels; 160 or above → the newer ones. No
>   160 label for the age limit was found, so the age words stay neutral at 160 and above.
> - **Folder-level words hold at both versions:** "Synchronization", "Select this folder for
>   offline use", "Download Now". ESR 153 carries them in `chrome/messenger/folderProps.dtd:15,19,21`
>   and Release 159 in `messenger/folderprops.ftl`, with the same text.
> - **What does not change yet.** The TC-13 fold's "until both have recorded it here, every version
>   gets the neutral words" stands. This note records the source half. MA-1191 still owes the lab
>   half (153.6.0esr on the Windows VM, 159.0 on the Mac) before version-chosen words ship.
> - **Not read:** the wizard's "right-click the account → Download / Sync Now", and Thunderbird's
>   download-now command for TC-9. Both stay with MA-1191.
>
> **Folded 2026-11-07 [completeness foldcheck2 L5] (lead's call): the words follow the version.**
>
> - **The labels, by version** (from the M4 re-read above):
>   - **ESR 153 and Release 159** (and so every version below 160): "Keep messages in all folders
>     for this account on this computer", and "Don't download messages larger than … KB".
>   - **160 and above:** "Save copies of all folders and messages on this computer for offline
>     use", and "Skip downloading messages larger than:".
> - **The rule for MailArchive's words:** they follow the version detected from
>   `compatibility.ini` `LastVersion` (the TC-13 fold), or, where no version is detected or it
>   cannot be read, they **quote both sets** ("…'Keep messages in all folders for this account on
>   this computer' (in Thunderbird 160 and later: 'Save copies of all folders and messages on this
>   computer for offline use')"). The age limit has no 160 label (above), so its words quote the
>   older label below 160 and stay neutral at 160 and above.
> - **Superseded by [completeness foldcheck2 L5]:** the TC-13 fold's "until both have recorded it
>   here, every version gets the neutral words", which the M4 re-read note above kept standing, and
>   the TC-13 fold's neutral words for an unknown or unparsable version (account level and size
>   limit). Those neutral words remain only for the age limit at 160 and above.
> - **Still owed:** lab row MA-1191 settles the labels on real installs (153.6.0esr on the Windows
>   VM, 159.0 on the Mac) before C3's words ship. If it finds a label differs, the lab's reading wins
>   and is recorded here. MA-1185 is read with this rule (§12).

## 7. Surfaces and words

The same sentence is used on every surface, built once in `internal/state` beside
`NotDownloadedSentence` (`outcomewords.go:176`), following design-plain-failure-words rule 1. An
account is named by its server (`imap.gmail.com`), never by its email address. **(Superseded
2026-11-08 for the words on screen by the owner's ruling on the C3 friction review's M1: the name
Thunderbird shows for the account, else "the Thunderbird account kept in the folder NAME"; records,
`-json` and email keep the store folder. See §13's C3 friction fold. The completeness lead "Export
complete." is "Backup complete." by the same day's ruling.)**

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

> **Folded 2026-11-07 [TC-17] (review condition): "later backups add the rest" is said only when a
> schedule will make it true.**
>
> - With a schedule for this archive, the second half stays as written.
> - With none (for example a one-off wizard export), it reads: "Run the backup again once
>   Thunderbird has downloaded more, to add the rest."
> - The same choice applies to the still-downloading and stopped notices' "later backups add the
>   rest", wherever they show.
> - MA-1181 gains the no-schedule case.
>
> **Folded 2026-11-07 [TC-10, TC-4, TC-12, TC-1] (the headline sentences, as amended):**
>
> - **Names.** Accounts are named by their store folder name (`imap.gmail.com-1` where that is the
>   folder), not by host alone.
> - **caught up, missing = 0:** as written, plus "as of DATE" when stale (§2.3 fold). When stale,
>   the lead is "Backup finished.".
> - **caught up, 0 < missing ≤ tolerance:** "Thunderbird has downloaded about X of the Y messages
>   it lists for ACCOUNT. The rest is likely mail that arrived or was deleted since Thunderbird's
>   count." Lead "Backup finished.".
> - **stale:** "(Thunderbird's count from Nov 2)", replacing "from its last check of the server on
>   Nov 2".
> - **Gmail floor, `unknown`:** O3's sentence (§3 fold).
> - **Every other `unknown`:** §4.4's fold.
>
> **Folded 2026-11-07 [the wizard and Run now leads] (from §4.2's fold).** The lead rule in the
> table's second row ("Export complete." for `caught_up` or non-IMAP) is narrowed:
>
> - "Export complete." only when every account may say complete (§4.2 fold), and only for
>   `not_applicable` accounts as typed in §2.2's fold;
> - an Exchange account, a partial input and a stale count each give "Backup finished.";
> - the run's lead is set by its weakest account.
>
> **Folded 2026-11-07 [TC-6] (owner default O1): what an encrypted archive shows.**
>
> - **On screen only:** the wizard's end summary, the Run now note, and the card's last result
>   while the archive is unlocked here, held in memory.
> - **Cleared** by Lock or by a switch to another archive. This is the pattern presync's result
>   already uses (C-15, `design-presync-attended.md:945-948`).
> - **Nothing else.** No card banner after a restart, no Recent backups tag, no `status` or `-json`
>   field, no WARN or note in `status`, no email, and no record or history field.
> - The not-kept list is shown on screen with names cleaned as in §6's fold. Those names never
>   leave memory.
>
> **Folded 2026-11-07 [`status -json`] (from TC-2, TC-4, TC-10, TC-16, TC-19).** The additive
> object also carries:
>
> - `account` as the store folder name;
> - `missing` (after the estimate) and `tolerance`;
> - `expunged_estimate`;
> - `as_of` as the **oldest** count time;
> - `limit_since` and `not_kept_since` (TC-19);
> - `verdict`, also allowing `unknown` with a `reason` drawn from §4.4's fold:
>   - `cache_unreadable`, `no_match`, `duplicate_key`, `invalid_entry`, `panorama`;
>   - `account_type`, `never_checked`, `gmail_floor`, `expunged_unsized`, `partial_input`.
>
> Nothing is renamed. Plaintext only.
>
> **Folded 2026-11-07 [foldcheck M8] (lead's call): the encrypted Run now note obeys R42.** The
> surfaces table above names `runNowSummary` only. An encrypted archive's Run now note comes from
> `encryptedSummary` (`internal/wizard/jobrun.go:229-237`), which always begins "Backup complete:".
> Under O1 this is exactly the screen where an encrypted archive's completeness shows, so as written
> its lead would say "complete" above a "behind" or "can't tell" line. Add this row to the table:
>
> | Surface | Code today | Change |
> |---|---|---|
> | Desktop Run now note, encrypted archive | `encryptedSummary`, `internal/wizard/jobrun.go:231-237` ("Backup complete: N message(s) added or updated…") | the lead follows the run's weakest-account rule (§4.2 fold): "Backup complete:" only when every account of the run may say complete, or the run had no Thunderbird IMAP store; "Backed up what Thunderbird has so far:" when any account is `behind` or `limited`; "Backup finished:" otherwise. Then today's count and "sealed" sentence, then the completeness lines (headline, not-kept list with cleaned names), then today's index-behind sentence |
>
> - **Where the verdict comes from.** The run's in-memory result (`app.Result`), never a record.
>   Nothing new is written beside the archive (O1, R44).
> - **What lasts.** The whole note, lead included, is held in memory and cleared by Lock or a switch,
>   with the rest of the on-screen completeness (C-15). After a restart nothing about completeness
>   shows, and no record-based surface says "complete" (the fold check's sweep, its §2).
> - **The wizard's end summary** for an encrypted archive (`exportSummary`, in the table above)
>   follows the same rule.
> - Rows: MA-1181 and MA-1186 gain an encrypted behind run and an encrypted can't-tell run: the note
>   never begins "Backup complete:"; a caught-up, missing = 0 encrypted run still may (§12 fold).
>
> **Folded 2026-11-07 [foldcheck M9] (lead's call): staleness in the words and in `-json`.**
>
> - "**caught up, missing = 0:** as written, plus 'as of DATE' when stale" (the headline fold above)
>   is **superseded by [foldcheck M9]**. When stale, the headline is the dated sentence of §2.3's M9
>   fold, with no "everything", and the lead is "Backup finished.".
> - `status -json` gains two **additive** booleans in each `thunderbird_completeness` entry:
>   - `stale`: the oldest count time is more than 3 days old or later than now (§2.3 fold);
>   - `complete`: this account lets the run say complete, by the exact test of §4.2's fold (`caught_up`,
>     missing = 0, not stale, no cap note, `expungedBytes` = 0 on every kept folder). It is `true` for a
>     `not_applicable` account, so a script that requires every entry's `complete` to be true applies
>     the same rule as the words, without rebuilding it from `verdict`, `missing`, `as_of` and the
>     cap note.
> - `verdict: "caught_up"` stays as it is; nothing is renamed. Plaintext only, as the rest of the
>   object. MA-1183 gains both fields (§12 fold).
>
> **Folded 2026-11-07 [completeness foldcheck2 MF1] (lead's call): no escape by "IMAP store".**
>
> - In the M8 row above, "or the run had no Thunderbird IMAP store" is **superseded by
>   [completeness foldcheck2 MF1]**. Read instead: "or every Thunderbird store of the run is
>   `not_applicable` (a run with no Thunderbird store keeps today's words). This is decided by the
>   accounts' verdicts (§2.2 fold), never by `IsImapStore` or the path."
> - Why: the design never defined "IMAP store" by verdict, and the only code test for it,
>   `IsImapStore`, is a path check the TC-5 fold retired. Read that way, an encrypted run of a store
>   with no matched account, or of an Exchange (`ews`) store, would lead "Backup complete:" above a
>   "can't tell" line.
> - The surfaces table's "Export complete." "only for `caught_up` or non-IMAP" (GUI wizard end
>   summary row) is already narrowed by the wizard-lead fold above to `not_applicable` as typed in
>   §2.2's fold; it is read the same way.
> - MA-1181 and MA-1186 gain the cases (§12 fold).

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

> **Folded 2026-11-07 [TC-18] (lead's call L1):** this section is **taken out of this build**.
>
> - **What moves.** Slice C6 and row MA-1187 leave this design. They go to their own short note,
>   written after C3 has shipped and the lab rows (MA-1189) give real download rates.
> - **Why.** That note changes a reviewed state machine's wait rule, and it owes presync's own
>   re-review. For a download that takes days, it buys at most the existing 20-minute cap.
> - **What stays.** The answer to Q4 (no gate; keep waiting while mail arrives) stands, as that
>   note's starting point.
> - **What is untouched.** Nothing in `internal/presync` changes in this build. `Synced()` and the
>   S1-S9 machine stay as they are.
> - **The wizard's wait.** The wizard's wait text (§7) is not presync. It is still changed in C3.

## 9. Privacy

### 9.1 Plaintext archives

- The record and `status -json` carry per-account counts, the server name, and the names of
  **not kept** folders.
- Only the per-account X and Y go into the run history.
- Folder names already appear in a plaintext archive's own pages, so naming them in its status
  adds nothing. Personal label names are never read for any other purpose.

> **Folded 2026-11-07 [TC-7, TC-19] (review conditions):**
>
> - Not-kept folder names in the record and in `-json` are cleaned and capped as in §6's fold before
>   they are written.
> - TC-19's fingerprints are hashes. They add no name beyond the list above.
> - Accounts are recorded by store folder name (§4.1 step 1 fold).
>
> **Amended 2026-11-08 (§13's note "the account's name on every surface"):** the record, `-json`
> and the run history also keep the name Thunderbird shows for each account (usually the email
> address), beside the store folder, which stays the key. Why that adds no exposure is in that note.

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

> **Folded 2026-11-07 [TC-6] (owner default O1; the proposal above is withdrawn):** nothing about
> completeness is written beside an encrypted archive.
>
> - **Why.** The `tb_behind` field's presence would say "this archive is made from Thunderbird IMAP
>   on this computer". That is exactly the source kind that rule C-16 keeps out
>   (`design-presync-attended.md` §4.4). The sealed record's run-details place (A24) is reserved
>   but not built, so it is not a ready alternative either.
> - **No field.** No `tb_behind` and no other completeness field goes into `encryptedLastRun` or
>   into the minimal history entry. The A18 allow-lists and rule C-16 are **unchanged**.
> - **On screen only, while unlocked.** The numbers, the verdict and the not-kept list are held in
>   memory: the end summary, the Run now note, and the card's last result while unlocked here. They
>   are cleared by Lock or a switch (C-15, `design-presync-attended.md:945-948`).
> - **No streak.** None is counted for an encrypted archive, so there are no notices and no email
>   (§5 fold, §9.3 fold).
> - **The guard is MA-904, not `tree.go`** (TC-24). MA-904 checks that the record key sets equal the
>   A18 allow-list, and scans the whole archive folder for "Thunderbird", "presync" and outcome
>   names. `tree.go:37-44` is only the list of file names allowed at an encrypted archive's root,
>   and it cannot see a field. MA-1186 now asserts that after a behind run of an encrypted archive,
>   MA-904's checks pass **unchanged**: no new key, no count, no server or folder name, no
>   "Thunderbird".
> - **The slice goes.** Slice C4 and its adversarial pass are withdrawn.
> - **Later.** When A24 is designed, completeness may move into the sealed record by its own design.

### 9.3 Email

An email leaves the computer, so alert emails carry the server name and counts, **never folder
names**. An encrypted archive's email carries only the fixed sentence.

> **Folded 2026-11-07 [TC-6] (owner default O1):** an encrypted archive gets **no completeness
> email at all**. The "fixed sentence" above is withdrawn with `tb_behind`, and rule C-16 is
> unchanged. Its other alert emails (failed runs and the like) are as today. MA-1188 now asserts
> two things: a plaintext archive's email carries counts and the store folder name but no folder
> names; and an encrypted archive sends no completeness email.

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

> **Folded 2026-11-07 [TC-2, TC-3, TC-5, TC-11, TC-12, TC-15, TC-16] (more rows for the table
> above):**
>
> | Case | Behaviour | Fails toward |
> |---|---|---|
> | a field missing, renamed, a string, out of range, or negative off the root | `unknown` (§4.1 step 3 fold) | never "complete" |
> | the file is a link, a pipe, a device, or over 16 MB | refused unread, never waited on; `unknown` | never "complete"; never a hang |
> | a key that appears twice; two keys for one folder | `unknown` | never "complete" |
> | Panorama: `panorama.sqlite` present or the pref set | `unknown`, the newer-storage words | never "complete" |
> | an Exchange account, an unknown `type`, or prefs unreadable | `unknown` | never "complete" |
> | a partial `-input` | `unknown`, no WARN | never "complete" |
> | the count's time in the future, or weeks old | stale: "as of DATE", lead "Backup finished." | never "complete" |
> | deleted copies not marked Expunged | the `expungedBytes` estimate subtracted | toward "behind" |
>
> **Row 2 of the table above** (keys naming another place) now reads: exact match first, then tail
> matching only with no exact key; two matches for one folder → `unknown`.
>
> **Row 6 of the table above** ("re-checked at each Thunderbird ESR") has an owner and a trigger in
> §16's TC-14 fold.
>
> **Folded 2026-11-07 [foldcheck M2, M7] (lead's call):**
>
> - **Row 2 of the first table, and its note just above,** are **superseded by [foldcheck M2]**: exact
>   mode when any key matches exactly (old-path keys ignored where an exact key exists; a folder with
>   only an old-path key that is still in the store → "can't tell", which clears at Thunderbird's next
>   save); tail mode only with no exact key (§4.1 step 2 fold). Fails toward: never "complete", and
>   never `unknown` for good.
> - **The row "deleted copies not marked Expunged … toward 'behind'"** is **superseded by [foldcheck
>   M7]**. The estimate can undercount, so it can fail toward "complete"; the words are kept from
>   doing so by requiring `expungedBytes` = 0 for "complete" words (§2.4 fold). Fails toward: "Backup
>   finished." with both numbers.

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

> **Folded 2026-11-07 (the review's conditions applied to §12).**
>
> **Invariants, as amended:**
>
> - **R42** (TC-4, TC-12, §4.2 fold) adds: "it says 'complete' or 'everything' only when nothing
>   Thunderbird lists is missing, the count is not stale, and every account of the run may say so."
> - **R43** (TC-1) adds: "Gmail is told by `is_gmail` alone. A floor is never 'caught up'."
> - **R44** (TC-6, O1) reads instead: "Nothing about completeness (no count, server or folder name,
>   verdict or yes/no) is written beside an encrypted archive. It is shown on screen only, while the
>   archive is unlocked here."
>
> S75 to S77 are unchanged. S77 also covers an Exchange account and Panorama.
>
> **Rows amended** (the row above stands, plus these):
>
> | Row | Gains |
> |---|---|
> | MA-1170 | the TC-15 opener and token decode; exact match before tail; component-wise compare (`imap.gmail.com` vs `imap.gmail.com-1`); NFC; an old-path and a current-path key for one folder → `unknown` (TC-3, TC-10, TC-15) |
> | MA-1171 | a link and a pipe at the file's name (no hang); a duplicated key; a missing, a string and an out-of-range `totalMsgs`; a negative count off the root; `panorama.sqlite` alone and the pref alone, each → `unknown` (TC-2, TC-3, TC-15) |
> | MA-1172 | for Gmail with All Mail setting Y, not-kept labels → one note, never "not kept" (TC-8) |
> | MA-1173 | `expungedBytes` > 0 plus extra unflagged copies → not `caught_up`; `expungedBytes` > 0 with listed 0 → `unknown` (TC-16) |
> | MA-1174 | the O2 formula at 15, 100, 10,000 and 238,169; 15 listed and 0 on disk → `behind`; +30 / −30 → `behind`; 0 < missing ≤ tolerance → `caught_up` with both numbers and no "complete" (TC-4) |
> | MA-1175 | detection by `is_gmail` only; an AllMail-flagged folder on a non-Gmail server keeps the per-folder rule (TC-1) |
> | MA-1176 | All Mail hidden, not kept, never checked, or smaller than a label → floor; X above the floor with a large label empty → `unknown` with O3's words, never `caught_up` (TC-1) |
> | MA-1177 | `autosync_offline_stores` false → `limited` (not `unknown`), with TC-9's words (TC-9) |
> | MA-1178 | `lastSyncTimeInSec` ≤ 0 → never checked; the "lists no mail" exception only with every kept folder checked (TC-2, TC-12) |
> | MA-1179 | the oldest time, not the newest; a future time → stale; stale `caught_up` → lead "Backup finished."; the words say "Thunderbird's count from" (TC-12) |
> | MA-1180 | the cache is read once, after the walk; the TC-19 fingerprint carried forward from the previous record (TC-19, TC-21) |
> | MA-1181 | no "complete" for 0 < missing ≤ tolerance, stale, partial input, Exchange, or Panorama; the weakest account sets the lead; with no schedule, "Run the backup again…" (TC-4, TC-12, TC-17, §4.2 fold) |
> | MA-1182, MA-1183 | the WARN for a chosen setting on the first run, a note on the next run with the same fingerprint, a WARN again when it changes; the `-json` fields of §7's fold (TC-19) |
> | MA-1184 | at most one still-downloading and one stopped email per streak, stopped never before still-downloading; `limited` ends a streak silently; `unknown` neither extends nor ends one (TC-20, §5 fold) |
> | MA-1185 | the Gmail labels note; fix words chosen by `compatibility.ini` version; neutral words for an unknown version (TC-8, TC-13) |
> | MA-1186 | **reworded** (O1): after a behind run of an encrypted archive, MA-904's key-set and folder scan pass unchanged; the numbers appear in the end summary and Run now note only, and are cleared by Lock (TC-6) |
> | MA-1187 | **leaves this design** with C6 (TC-18) |
> | MA-1188 | plaintext email: counts and the store folder name, no folder names; encrypted: no completeness email (TC-6) |
> | MA-1190 | X within the tolerance of All Mail's listed count on the owner's caught-up profile, or the distinctness key changes (TC-23) |
> | MA-1191 | ESR 140 **and** the current Release; the version where the account checkbox label changed; the wizard's "Download / Sync Now" instruction; Thunderbird's download-now command for TC-9 (TC-13) |
>
> **New rows:**
>
> | ID | Tier | Asserts | Covers |
> |---|---|---|---|
> | MA-1195 | U | C0c: `FindAccountForStore` with two servers on one host (`ImapMail/imap.gmail.com`, `ImapMail/imap.gmail.com-1`) gives each store its own account, by `directory`/`directory-rel` compared component-wise; `hostname` is used only when exactly one server has it; still ambiguous → no account, never a guess; `EnableOffline` from the wizard and `-enable-offline` acts on the matched account. Positive twin: one account on one host, found as today | — (an existing bug, review side finding A) |
> | MA-1196 | U | sorting by the account's `type`: an `ews` store under `Mail/` → `unknown`, never "complete"; an `imap` account with a custom folder outside `ImapMail/` → checked; `pop3`/`none`/`rss` → `not_applicable`; prefs unreadable with a cache entry → `unknown` (TC-5) | R42, S77 |
> | MA-1197 | L | **pending** (lab, Windows first): a throwaway Thunderbird profile with a native Exchange account (a throwaway Microsoft 365 test account), on the Parallels VM under `C:\Users\Public\tb-completeness-lab\`; record the fields of its folder-cache entries and where its store lives; then macOS (TC-5) | S77 |
> | MA-1198 | U | hostile folder names: control characters, a bidi override and a 10 KB name are stripped and capped (80 + "…"); the not-kept list capped at 10 plus "and N more"; checked in `status` text, the wizard summary, the Run now note and the Desktop (`html/template`) (TC-7) | S76 |
> | MA-1199 | L | **pending** (lab, Windows first): in a throwaway IMAP profile, delete and move downloaded messages; record, before and after compaction, the mbox separators, how many carry 0x0008, `expungedBytes`, `folderSize`, listed, and the walk's count. Decides whether TC-16's subtraction may be dropped; until it runs, release notes do not claim the count is exact (TC-16) | R42, S75 |
> | MA-1200 | L | **pending** (lab, Windows first): convert a throwaway profile to Panorama (set `mail.panorama.enabled`, run Thunderbird) and record whether `folderCache.json` stays and whether `panorama.sqlite` appears; then move a throwaway profile and record which keys stay (TC-3) | S77 |
> | MA-1201 | L | **pending**: on a throwaway Gmail account, find whether Gmail's IMAP folder size limit still exists; if it does, set it and record All Mail's listed count; if it does not, drop TC-22's note (TC-22) | R43 |
> | MA-1202 | U | a partial `-input` (one mbox file of a store; a subfolder inside `ImapMail/<host>`) → `unknown` with no WARN and neutral words; the whole store → checked; the store taken from the input `path`, never `openPath` (TC-11) | R42 |
>
> Lab story for MA-1197, MA-1199, MA-1200 and MA-1201: the repo has no lab tier, so they stay
> "pending" in the catalog. They run on the Parallels VM (`.claude/winrun.sh`) and the Mac, with
> throwaway profiles and accounts only, never the owner's or the VM user's own.
>
> **Folded 2026-11-07 [foldcheck M1 to M10] (lead's call): rows amended again.** No new ids; each row
> above stands, plus these. Where a gain contradicts an earlier one, the earlier one is superseded as
> named.
>
> | Row | Gains | Item |
> |---|---|---|
> | MA-1170 | the six fixtures of §4.1 step 2's M2 fold. The earlier gain "an old-path and a current-path key for one folder → `unknown`" is **superseded by [foldcheck M2]**: that case is now checked, not `unknown` | M2 |
> | MA-1171 | a non-root entry with a broken `flags` and nothing on disk → `unknown` | M3 |
> | MA-1173 | the skewed-size fixture (100 listed, 1 MB average, 50 deleted copies of 10 KB, 49 never downloaded) → never complete words; `expungedBytes` > 0 on one kept folder with missing = 0 → lead "Backup finished." and both numbers | M7 |
> | MA-1176 | All Mail hidden, an "Archives" label with 0x4000 only and all its mail on disk, Sent with nothing on disk → never `caught_up`; two folders with 0x8000 → floor | M5 |
> | MA-1181 | an encrypted behind run and an encrypted can't-tell run: the `encryptedSummary` note never begins "Backup complete:"; a caught-up, missing = 0 encrypted run may; a stale caught-up run's words carry no "everything"; `expungedBytes` > 0 → "Backup finished." | M7, M8, M9 |
> | MA-1183 | `-json` per-account `stale` and `complete`, `complete` matching the words' rule on every fixture of MA-1174 to MA-1179; `complete` true for `not_applicable` | M9 |
> | MA-1184 | an hourly schedule run for 4 days (history wrapped past 50 entries): the still-downloading email fires once; the streak lives in `.mailarchive-tbstreak.json`; an encrypted archive has none, and a keyed run removes a stray one | M10 |
> | MA-1186 | the encrypted Run now note's lead follows the weakest-account rule, and the note is cleared by Lock; MA-904 still passes unchanged | M8, M10 |
> | MA-1191 | **ESR 153** and **Release 159** (or the Releases current at the lab run), replacing "ESR 140" (superseded by [foldcheck M4]) | M4 |
> | MA-1195 | a pipe and a link at `prefs.js`: no hang, no account; `parsePrefs` uses `util.ReadRegularFile` | M1 |
> | MA-1196 | ambiguous account plus no cache; `prefs.js` unreadable plus no cache; a copied `ImapMail/<host>` with no profile above it: each → `unknown`, never "Export complete."; a plain folder with no profile and no `ImapMail` part → `not_applicable` (positive twin) | M6 |
> | MA-1200 | the moved profile also records whether every loaded folder gets a current-path key at the next save, and whether a folder deleted after the move leaves its old key | M2 |
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call): MA-1196's custom-folder case.** The
> original MA-1196 case "an `imap` account with a custom folder outside `ImapMail/` → checked" is
> **superseded by [foldcheck M6 residual]**. It holds only when the folder sits inside its profile,
> because the profile is found by walking up at most six levels to `prefs.js` (`FindProfileDir`,
> `thunderbird.go:29-42`, §10). The case is replaced by three:
>
> - an `imap` account whose custom folder lies outside `ImapMail/` but **inside its profile**
>   (`prefs.js` found within the six levels) and matched by `directory` → checked;
> - the **same folder outside every profile** (or more than six levels below it), with its `.msf`
>   files → `unknown`, reason `no_match`, never "Export complete." (and in an encrypted archive's
>   Run now note, never "Backup complete:");
> - a folder of mbox files with **no `.msf`**, no profile and no `ImapMail` path part →
>   `not_applicable`. This replaces M6's positive twin above ("a plain folder with no profile and no
>   `ImapMail` part").
>
> MA-1196 also gains a Local Folders copy kept outside its profile (it has `.msf` files) →
> `unknown`, lead "Backup finished.", and an `.MSF` name in capitals counted the same as `.msf`.
>
> **Folded 2026-11-07 [completeness foldcheck2 MF1, L1 to L4] (lead's call): rows amended again.**
> No new ids. MA-1181's original "they do for `caught_up` and non-IMAP" is **superseded by
> [completeness foldcheck2 MF1]**: read "…and for `not_applicable`" (§2.2 fold), never decided by the
> path.
>
> | Row | Gains | Item |
> |---|---|---|
> | MA-1170 | key tails with a `.` part, a `..` part, and an absolute tail (POSIX, drive letter, UNC): no `Lstat` is made outside the store, and the account is `unknown` (`invalid_entry`) | L2 |
> | MA-1175 | a Gmail store with a walked mailbox that has no cache key: its messages are archived, never added to X (a gap in All Mail stays a gap), and counted in the record | L3 |
> | MA-1181 | an encrypted run of a store with no matched account, and of an `ews` store: the `encryptedSummary` note never begins "Backup complete:"; a `caught_up` account with a not-kept WARN → never "Export complete." | MF1, L4 |
> | MA-1183 | `complete` false for an account with a not-kept WARN | L4 |
> | MA-1186 | the same two encrypted runs as MA-1181's MF1 gain | MF1 |
> | MA-1195 | `EnableOffline`: a link, a pipe and an over-cap file at `prefs.js` are refused with nothing written; a link at `prefs.js.mailarchive-bak` is not written through (its target unchanged); the account's setting written for the matched account only | L1 |

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

> **Folded 2026-11-07 [slices] (lead's calls C0c, L1, L2, TC-14; owner default O1).** The table
> above is replaced by this one, in build order. The C3 and C5 names are kept, so the lead's "right
> after C3's friction review" still points at the right slice. C4 and C6 are **retired**, not
> reused.
>
> | Slice | What | Gate before merge |
> |---|---|---|
> | **C0** | this design, the review (filed), these folds; catalog rows MA-1170 to MA-1186, MA-1188 to MA-1202 as pending encoders. **Plus the TC-14 source re-read at ESR 140** (doc and lab task, §16 fold) | an independent fold-verify of this file. The ESR re-read must be recorded in §16 before C1a merges (field names, types and `expungedBytes`), and in any case before release |
> | **C0c** | `FindAccountForStore` fix (MA-1195); `IsImapStore`'s comment corrected. **Built first** | prove-fail → prove-pass; unfiltered `go test ./...`; the test also run on the Windows VM |
> | **C0b** | one shared profile-root list, with Flatpak (MA-1194) | unit + Linux Docker (unchanged) |
> | **C1a** | the `folderCache.json` reader in the new leaf `internal/tbcomplete`: the opener, the 16 MB cap, the token decode with duplicate keys, field validation, key matching, the Panorama signals, the `compatibility.ini` version read (MA-1170, MA-1171) | **adversarial pass** (a new parser of an untrusted file, the lead's call); mutation sweep; a fuzz test of the decoder |
> | **C1b** | the verdict core, pure: sorting by `type`, the Gmail rule with its floor, the tolerance, the limits (including `autosync_offline_stores`), the "as of", the expunged estimate (MA-1172, MA-1174 to MA-1179, MA-1196) | test-first; mutation sweep |
> | **C2** | the walk's per-folder counter and the Gmail distinct set; the whole-store rule; the cache read after the walk; the run calls `tbcomplete` per store; record and history fields with the TC-19 fingerprint (MA-1173, MA-1180, MA-1202) | unfiltered `go test ./...`; the walk tests also run on the Windows VM |
> | **C3** | words and surfaces: wizard, Run now, Desktop, `status`, `-json`, the encrypted on-screen-only path, cleaned names, version-chosen words (MA-1181 to MA-1183, MA-1185, MA-1186, MA-1198) | **friction review** (Windows on Edge first, then Mac); the X-series walk; MA-904 green **unchanged** |
> | ~~C4~~ | retired: the encrypted `tb_behind` field is withdrawn by O1 | — |
> | **C5** | the still-downloading and stopped notices and their email, plaintext only (MA-1184, MA-1188). Ships **right after C3's friction review** (L2) | **friction review** of the email words |
> | ~~C6~~ | retired: presync moves to its own later design (TC-18; MA-1187 goes with it) | that design's own review |
> | **L** | MA-1189 to MA-1193, MA-1197, MA-1199 to MA-1201 | before release notes claim it. The notes name the Thunderbird versions the lab ran on (TC-14), and do not claim the count is exact until MA-1199 has run (TC-16) |
>
> **Order:** C0c → C0b → C1a → C1b → C2 → C3 is the minimum shippable unit. The backup stops
> claiming "complete" at C3. C5 follows C3's friction review. The sentence above about C4 no longer
> applies: encrypted archives get only the on-screen words, which are in C3.
>
> **Gates this design does not self-certify:** the adversarial pass on C1a and the friction reviews
> on C3 and C5. Each is run by a reviewer other than the builder, as the kit's process says.
>
> **Folded 2026-11-07 [foldcheck M1, M4, M10] (lead's call): the slice table, as amended.**
>
> - **C0:** "the TC-14 source re-read at ESR 140" is **superseded by [foldcheck M4]**: the re-read is
>   at ESR 153, names Release 159, and is owned by the lead. It gates C1a's merge (§16 fold).
> - **C0c** also moves `parsePrefs` onto `util.ReadRegularFile` (M1), with MA-1195's pipe and link
>   cases. C0c may start now (the fold check's verdict).
> - **C1a** waits for M2 to M4 (folded here) and for the fold checker's re-check of them. **C1b**
>   waits for M5 to M7 the same way. **C3** carries M8 and M9, and **C5** carries M10's streak record.
> - The fold check's low items fold with their slice, as it says: TC-7's single bidi helper and the
>   cleaned account name (C3); TC-19's read-before-the-start-write and carry-forward (C2); X4 (C3);
>   N4 (C1b); N5 (MA-1190); N7 (C3). They are **not** folded in this pass.
>
> **Folded 2026-11-07 [completeness foldcheck2 L1, L6] (lead's call): the slice table, as amended
> again.**
>
> - **C0c** also moves `EnableOffline`'s own read of `prefs.js` onto `util.ReadRegularFile`, and its
>   two writes (the backup and `prefs.js`) onto the atomic writer after a fresh `Lstat`, so neither
>   follows a link nor replaces a file that is not a regular file (L1, §4.1 step 1 fold).
> - **C3's gate gains (L6):** before C3 starts, an **independent narrow check** of two things fold
>   check 2 could not check independently: the M6-residual note (Job 1, commit 7126111, including
>   MA-1196's rewording in §12's fold) and this foldcheck2 fold (MF1, L1 to L5). Fold check 2's
>   checker wrote the residual note, so its §2 row R is self-checked. The check is narrow: those
>   notes only, read against §2.2, §4.2, §4.4, §7, §12 and §17. It is run by an agent that wrote
>   none of them.
> - **C1a** carries L2 (to its adversarial pass as well); **C1b** carries L3; **C3** carries MF1, L4
>   and L5.
>
> **Built 2026-11-07 [C0c] (the builder's call, for the lead):** C0c's gate before merge gains an
> **adversarial pass** on the prefs.js read and write and the account match (§4.1 step 1's build
> note). The builder stops there; the pass is run by a reviewer other than the builder.
>
> **Built 2026-11-08 [C1a] (the builder's notes, for the lead and for C1a's adversarial pass).**
> C1a is built as the new leaf package `internal/tbcomplete` (`cache.go`, `profile.go`), with
> `thunderbird.PanoramaEnabled` and `thunderbird.LastVersionMajor` added (`panorama.go`); rows
> MA-1170 and MA-1171 in the catalog, and MA-1235 amended with the All Mail cases the [B1 split]
> left for C1a. No new id. No verdict rule is built (C1b), and nothing calls the package yet (C2).
> Where the builder read the text more strictly, or chose where the text is silent:
>
> - **Separators by the OS's rules.** §4.1 step 2 item 1 says "`\` and `/` alike". The build
>   follows MA-1265's later rule (a backslash separates folders on Windows only), as C0c's account
>   match does. So a Windows-shaped key read on macOS or Linux matches no store and the store has no
>   keys (C1b: `unknown`), which fails safe; a POSIX key read on Windows is found by its tail. The
>   lead may widen it.
> - **The tail refusal (L2) is wider than the list.** Each part is read split at `/` and `\` alike
>   on every OS. Refused: an empty piece (a leading, doubled or trailing separator, so absolute and
>   UNC tails), `.` or `..`, a drive letter (`X:` at a piece's start, on every OS), a NUL, a folder
>   **named** `.` or `..` once `.msf` or `.sbd` is taken off (`..msf`, `...msf`: the mailbox-file
>   check would otherwise `Lstat` the store itself or its parent; found by the fuzz test), a part
>   of dots and spaces only (`.. `, `...`: Windows drops trailing dots and spaces, so these name
>   the parent or the folder itself there; the builder's own find on review), and on Windows a
>   colon anywhere (a stream name). A refused key is listed (`Store.Refused`); C1b reads
>   it as `invalid_entry`, as L2 says. The pass may narrow any of these to "ignored and counted".
> - **A key's shape.** A key under the store's current path whose tail is not `X.sbd/…/Y.msf` (in
>   lower case, as Thunderbird writes them) is refused the same way; an old-path key of another
>   shape is no key of the store, which keeps a folder of another account whose path happens to
>   hold the store's name out of it. Tail matching uses the **last** part named as the store.
> - **"Still in the current store" (M2).** C1a has no walk, so "the walk read its mailbox file" is
>   read as "its mailbox file is there". The check is an `Lstat` one part at a time from the store
>   and the tail as written: a link where a `.sbd` folder would be, or any error but "not there",
>   reads as still there ("can't tell", toward `unknown`); a file where the `.sbd` folder would be
>   reads as gone.
> - **The store's current path** is the path made absolute and, when different, the same path with
>   its links resolved: a key written through either spelling is exact (macOS's `/var` is
>   `/private/var`). Two exact keys for one folder through the two spellings would be a duplicate.
> - **Inside an entry.** A field named twice is broken. A value that is not an object is kept as a
>   folder whose flags are not known (M3's rule, so C1b treats it as kept), rather than "skipped
>   and counted": with no flags, "kept or not" cannot be told, which is M3's case. A negative
>   `lastSyncTimeInSec` is read, not broken (≤ 0 is "never checked", §2.3's TC-12 fold).
>   `expungedBytes` is required on every folder entry (the M4 re-read).
> - **Roots and duplicates.** Two keys for the account root are a duplicate too (`""` in
>   `Duplicates`). An old-path root beside an exact one is ignored and counted.
> - **All Mail (M5, the [B1 split]).** One folder with 0x8000: that folder. None: none. Two or
>   more: several, no pick. A folder whose flags are broken, a duplicate, or an old-path-only folder
>   that carries 0x8000 (or whose flags are broken) makes it can't tell. Only "one" names a folder,
>   and the zero value is can't tell.
> - **The Panorama setting** is read through C0c's reader by `thunderbird.PanoramaEnabled`, with
>   its type checked there for this one setting (any statement not true or false → can't tell). It
>   was **not** added to MA-1270's `wrongType`, so the account match reads every profile as before.
>   `panorama.sqlite` is seen by `Lstat` (any kind of entry counts) and never opened.
> - **Gate owed: C1a's adversarial pass** (§13's slice table; "a new parser of an untrusted file").
>   The builder stops there. Its scope should also take in B1's change to the C0c reader (commit
>   5f92b89: `is_gmail` added to `wrongType`'s typed settings, and `findAccount` split out of
>   `FindAccountForStore` so `GmailForStore` reads the files once), and this slice's two small
>   additions to `internal/thunderbird`. Not self-certified here.
>
> **Fixed 2026-11-08 [C1a pass] (the lead's calls on `docs/review-thunderbird-completeness-c1a-adversarial.md`).**
> Every finding is fixed, one catalog row each (MA-1272 to MA-1278). Two are folded into §4.1
> step 3 above (C1A-01, C1A-07). The others change §4.1 step 2's key matching, as follows.
>
> - **C1A-03 (MA-1273).** A file nested deeper than jsoncpp's `stackLimit` of 256 is
>   `CacheMalformed`, since Thunderbird does not read it either.
> - **C1A-04 (MA-1274).** On Windows a `\\server\share` root is a root of its own. A UNC store and a
>   key rooted on a drive never match **exactly**. Such a key may still be found by its tail, as a
>   profile moved from a drive to a share is. That is the builder's reading of the lead's "no match".
> - **C1A-05 (MA-1275).** Item 1's normalising now applies only when no key matches byte for byte.
>   When one does, a key that matches only after case or Unicode folding is an old-path key, under
>   M2's rule.
> - **C1A-06 (MA-1276).** Item 3's tail matching now also needs the part above the store's name to
>   carry the store's parent's name (`ImapMail`), as its own words say. Keys found that way from more
>   than one place make the store the new mode `CantTellKeys`. C1b reads that as `unknown`, like
>   `NoKeys`. **Known limits.** A store with no keys of its own still takes another account's keys
>   if they sit under another `ImapMail` with the same name. A custom-folder store moved to a parent
>   with another name has no keys until Thunderbird saves (`unknown`, never `caught_up`). Fix for the
>   first, if wanted: C2 passes the profile's other store paths, and tail mode skips a key that is
>   exact for one of them.
> - **C1A-08 (MA-1278).** On Windows, `Store` also looks up the store's own long and 8.3 short
>   spellings (`GetLongPathNameW`, `GetShortPathNameW`). It never looks up a key, because a key
>   could name a network share. The spellings are merged part by part. If the path exists and the
>   lookup fails, the store is `CantTellKeys`. Short names *below* the store are not expanded:
>   Thunderbird writes a folder's key from the store's path plus the folder's long name.
>
> **Folded 2026-11-08 [C1a pass] notes for C1b (not findings; the pass's §4, and the C1A-07
> fail-safe).**
>
> - **Keep L2's cautious reading.** Read every `Store.Refused` as `invalid_entry` (`unknown`); do
>   not narrow it to "ignored and counted". Two things rest on it:
>   - a refused exact key does not put the store in exact mode, so a stale old-path key can then be
>     read as the folder's entry (the pass's `TestAdvC1a24`);
>   - `allMail` never looks at a refused key's flags.
> - **`AllMailOne` can name an entry that is not valid.** Check `Valid()` before using that
>   folder's count.
> - **The C1A-07 fail-safe.** A kept folder with **no ImapBox flag** is never counted as caught up
>   on its `serverTotal` alone. Thunderbird has not had the server's count for it (the server has
>   not listed it). Its `serverTotal` is the starting 0, not a count. With no `lastSyncTimeInSec`
>   (never checked, §2.3), it is already not `caught_up`. C1b must also keep that true when
>   `lastSyncTimeInSec` is present.
> - **New facts to word:** `Entry.NotCountedYet()` ("not counted by Thunderbird yet", not
>   `invalid_entry`), `Store.Mode == CantTellKeys` with `CantTellWhy`, and a `folderSize` missing as
>   -1 (no words needed: the folder is valid).
> - **The Panorama setting** is read from prefs.js and user.js only. A setting forced by
>   `policies.json` or an administrator's autoconfig is not seen; `panorama.sqlite` is the second
>   signal for that case. This was not tested on a real install.
>
> **Fixed 2026-11-08 [C1a pass round 2] (the lead's calls on
> `docs/review-thunderbird-completeness-c1a-adversarial-r2.md`).**
>
> - **R2-01 (LOW, MA-1275 amended).** The C1A-05 bullet above ("item 1's normalising now applies
>   only when no key matches byte for byte") is **extended**: the store's path **as given** ranks
>   first. A key byte for byte at the path as given wins; a key byte for byte only at another
>   spelling of it (its link target; on Windows its long or 8.3 short form, C1A-08) is then an
>   old-path key under M2's rule, as a folded key is. Only when no key matches the path as given do
>   the other spellings count as exact, and only when none matches byte for byte at any spelling
>   does a folded key. So a profile reached once through a link and now through its target, or held
>   once short and now long, is never `unknown` for good. The pass's N1 stands: when MailArchive's
>   spelling is the old one byte for byte, the old keys win, with their old `lastSyncTimeInSec`,
>   so TC-12's dated words and the stale rule apply.
> - **R2-02 (LOW), a note for C2 (not fixed in C1).** Tail mode can take another account's keys
>   when the store has no keys of its own and the other account's folder is also
>   `…/ImapMail/<the store's name>`. C2 closes it, as the C1A-06 bullet above already proposes:
>   - **C2 passes the profile's other store paths** to the reader, and tail mode skips a key that
>     is exact for one of them;
>   - **C2 decides "IMAP" by the matched account's `type`** (`findAccount`'s account), never by
>     `thunderbird.IsImapStore` or the folder's path (the pass's Q6; the TC-5 fold);
>   - **a C2 test** shows that a Local Folders store never gets a completeness verdict other than
>     `not_applicable` (C1b's verdict function already gives `not_applicable` for type `none`
>     before it reads the cache; C2's test proves the run never hands it to the IMAP path).
>
> **Built 2026-11-08 [C1b] (the builder's notes, for the lead).** C1b is built as
> `internal/tbcomplete/verdict.go`: pure, no file read, no disk walk, no words. `Assess(Input)
> Result` gives one store's verdict (`caught_up`, `behind`, `limited`, `unknown` with a reason,
> `not_applicable`; zero values are `unknown`), with X, Y, missing, the tolerance, the floor, the
> as-of, staleness, the TC-22 cap note, the deleted-copies estimate and the not-kept lists;
> `Result.MaySayComplete()` is §4.2's full test (`-json`'s `complete`); `RunLead` is the
> weakest-account lead; `Tolerance` and `ExpungedEstimate` are O2's and TC-16's formulas. Rows:
> MA-1172 to MA-1179 (MA-1173 and MA-1175 the verdict's part), MA-1181 (the lead's part, words
> still C3's), MA-1196 (the decision, facts still C2's); MA-1170 and MA-1171 amended. No new id.
> Where the builder read the text more strictly, or chose where it is silent (each fails toward
> "not complete"):
>
> - **A time later than now.** TC-12 makes it stale, and §4.2's condition 7 says caught_up needs
>   "none in the future". Read together: such a folder is not checked, so the account is `unknown`
>   (`never_checked`) and stale, and for All Mail it is a floor (`AllMailNotChecked`). The lead may
>   prefer "stale only, verdict kept"; either is never complete.
> - **A certain gap beats a can't-tell folder.** M2 says an old-path-only folder gives `behind`
>   when the other folders already miss more than the tolerance. The build applies the same to a
>   never-checked folder: left out of X and Y, `behind` on a certain gap, else `never_checked`.
> - **C1A-07, a kept folder without ImapBox, is never checked**, whatever it lists, even with
>   `lastSyncTimeInSec` present (`never_checked`). **For the lead:** if the owner's 2 real
>   folders with no ImapBox (§4.1 step 3's C1A-07 fold) are kept, the owner's account stays
>   `unknown` until the server lists them. Not known from here; MA-1190 can read their flags.
> - **Reason codes.** §7 lists ten; M2 adds `old_path_only`. No key for the store, keys that
>   cannot be told (`NoKeys`, `CantTellKeys`), no countable kept folder, and messages on disk while
>   every folder lists 0 share §4.4's "could not read" words, so they share `cache_unreadable`.
>   C1A-01's "not counted by Thunderbird yet" has words and no code; the build names it
>   `not_counted_yet` (when a broken entry is there too, `invalid_entry` wins). A Gmail answer that
>   cannot be told is `no_match` (GmailForStore can't tell only without an account it can read).
> - **"Lists no mail"** also needs at least one folder entry and every entry valid (a not-kept
>   entry that fails validation, skipped and counted, ends the exception: `cache_unreadable`).
> - **Not-kept folders.** Trash or Junk without Offline is `DefaultNotKept` whatever it lists (a
>   note only). A not-kept folder the walk read is still not kept: its count stays out of Y. With
>   All Mail not kept, the labels are `NotKept` (TC-8's note needs All Mail to set Y).
> - **TC-9.** With background download off, `limited` also covers a Gmail floor whose All Mail was
>   never checked; a hidden All Mail stays `gmail_floor`.
> - **The Gmail floor** counts All Mail itself when it is checked but below the largest label (it
>   is still a true lower bound); a folder never checked never raises it (R43).
> - **For C2.** `Input.Limits`' zero value is Thunderbird's defaults, so C2 must read the account's
>   own settings (a value it cannot read with certainty fails the account match, C0c). `OnDisk.Folders`
>   is keyed by `Entry.Path` exactly as the reader gives it, so C2 maps each walked mailbox to that
>   spelling. `OnDisk.Distinct` must count only mailboxes that have a cache key (L3). The M6 facts
>   are `Input.Unmatched`, all three needed for `not_applicable`.
> - **Gate.** §13 names test-first and a mutation sweep for C1b. Both are done: 12 test functions,
>   all red on a stub; 143 mutants, each red (recorded in the commit). No other gate is named for
>   C1b; C2's unfiltered run and Windows walk tests come next.
>
> **Built 2026-11-08 [C2] (the builder's notes, for the lead).** C2 wires the verdict into the backup
> run without changing what is archived. The walk counts as it reads (`source.MailboxCounts`; the
> Expunged copies left out of the count, still archived); `thunderbird.ReadStoreFacts` reads the
> account, its Gmail answer, its limits and the profile's other stores from one read of prefs.js and
> user.js; once every source is walked, `app.assessThunderbird` reads `folderCache.json` once per
> profile and calls `tbcomplete.Assess` per store; `app.Result.Thunderbird` holds the verdicts in
> memory; a plaintext run that ends ok records them (`thunderbird` in the last-run record, `tb` in the
> history) and folds them into `.mailarchive-tbstreak.json` (M10). An encrypted archive's run keeps them
> in memory only and removes a stray streak record (O1). Rows: MA-1180, MA-1184 (the record's half),
> MA-1202 new; MA-1173, MA-1175, MA-1177, MA-1196 and MA-1276 amended. No id above MA-1278. Where the
> builder read the text more strictly, or chose where it is silent:
>
> - **A not-kept folder's mailbox never adds to Gmail's X**, like L3's unkeyed mailbox: its mail is
>   outside Y (Spam and Trash not kept), so counting it could cover a gap. Fails toward "behind".
> - **Two walked mailboxes that name one folder** (once compared as the OS compares names) are both
>   unkeyed. A mailbox under a container not named `.sbd` (a followed link's own name) is unkeyed.
> - **A limit setting of a type Thunderbird never writes** gives no account (`no_match`), as the C1b
>   note says. It is checked only for imap accounts, in `ReadStoreFacts`, and was **not** added to
>   MA-1270's `wrongType`, so the account match, `-enable-offline` and the wizard read every profile
>   as before (the Panorama precedent).
> - **A not_applicable store gives no result and no record entry** (Local Folders, POP3, Feeds, a
>   plain mail folder). `-json`'s `complete: true` for not_applicable (M9) is then C3's to show or not.
> - **A partial input whose store is also walked whole in the same run** is told once, by the whole.
> - **TC-19 after a failed run.** §11 says a failed run writes no completeness record, so the
>   fingerprints' first-seen times are not in it, and the next ok run sees no previous hash: a
>   chosen-setting WARN shows once more after a failed run. Carrying the fingerprints through a failed
>   record is a small change if the lead wants it.
> - **The streak record is built here**, at the caller's request, though the M10 table above gates it
>   "before C5". Its notices, its email and **the archive README's line for the new file** (a word
>   change) are C5's. The record's field names beyond `thunderbird` and `tb`, and the record bounds
>   (200 kept folders, 10 not kept per list, 64 accounts), are the builder's; the names follow §7's
>   `-json` names where §7 has one.
> - **The R2-02 fix** looks up the other stores as given and with their links resolved, not their
>   Windows long and 8.3 short spellings (C1A-08 looks those up for the store itself only).
> - **The run log says nothing new.** Every word is C3's.
> - **Gate owed: an adversarial pass** (the builder's reading of the repo's rule for security-sensitive
>   changes; §13's C2 row names only the unfiltered run and the Windows walk tests). C2 adds a file
>   written beside every plaintext Thunderbird archive and read back on every run, new fields in two
>   records that `status` and the Desktop will read from a folder anyone who can write the archive can
>   write, a header scan of untrusted mail in the walk of every local store, and a change to C1a's
>   reviewed reader (`StoreAmong`). Not self-certified here.
>
> **Built 2026-11-08 [C2 pass fixes] (the builder's notes, for the lead).** The adversarial pass
> (`docs/review-thunderbird-completeness-c2-adversarial.md`, NOT CLEAN at `18f0b6c`) is answered
> fix by fix; its attack tests are brought in as guards. The two MEDIUM fixes are §2.4's
> [C2 pass, ADV-C2-02] and [ADV-C2-03] folds and the maildir fold beside them. The LOW items:
>
> - **A store with no result (ADV-C2-16 and a failed walk).** A store under a Thunderbird profile
>   that another local reader opened is told `unknown`, `other_reader`; a store whose walk ended
>   with an error is told `unknown`, `walk_failed` (the run's failure words still win, and a failed
>   run records nothing, §11). Neither is ever silently absent from the results, so `RunLead` can
>   never say complete over it. A real Evolution store, under no Thunderbird profile, is still told
>   nothing. **(Amended 2026-11-08 by fold check 3's N1, §13's C3 friction fold: a store with no
>   profile that holds any `.msf` is kept too, as "can't tell".)** **Follow-up, not changed here:**
>   source detection still opens a maildir-format
>   Thunderbird store whose server has folders named `cur`/`new` and one starting with "." with the
>   Evolution reader (`isEvolutionMaildirStore`); the account is now `unknown` instead of counted.
>   Whether detection should prefer the Thunderbird reader under a profile wants its own change.
> - **The streak and the record are keyed by the store (ADV-C2-01).** The key is a hash of the
>   store's full path, profile and store, each compared as the OS compares names (`store_id` in the
>   record, `store` in the streak record); the folder name stays for the words. TC-19's first-seen
>   times are carried by it. **Migration: dropped.** A streak entry with no key (written by C2 at
>   `18f0b6c`, which never shipped) is dropped when read, never matched by its name, so its streak
>   starts again: toward a later notice, never a false one. A record entry with no `store_id`
>   carries no first-seen time, so a chosen-setting WARN shows once more.
> - **A loose-permission streak record (ADV-C2-05) is never silent.** The run logs a line and the
>   last-run record keeps a WARN, `tbstreak_unreadable`, that `status` shows. The record is **not
>   trusted** (a file other users can read may also be one they wrote), so the streak **starts
>   again** from that run and the record is rewritten owner-only. Why fresh: for R42 it can only make
>   the "still downloading" notice later; it can never claim more than a run saw. The cost is that
>   C5 could send a second still-downloading email for a streak already emailed. On a drive that
>   cannot keep files private (exFAT on macOS, not checked) the WARN shows after every backup and
>   the notice cannot count days there; the WARN's words say so. To fold the streak before the
>   record is written, the streak update now runs just before the last-run write (it ran just after).
>   **Follow-up, not changed here:** the presync streak (`app/syncstreak.go:53-56`) still freezes
>   silently on the same refusal and still reads a later format as its own.
> - **A later-format streak record (ADV-C2-10) is never downgraded.** `ReadTBStreak` refuses a
>   version above its own (`ErrTBStreakNewer`); the run moves nothing, writes nothing, logs a line
>   and keeps the same WARN. Any other refusal (a link, a pipe, too large) does the same.
> - **The account's own old `directory` (ADV-C2-07)** is no longer "another store" in `StoreAmong`.
> - **The record's size (ADV-C2-08).** The folders of all three lists across every account are
>   capped at 1,000 together (`TBFoldersTotalMax`): each of n accounts gets 1,000 ÷ n, not-kept
>   folders first (they carry a WARN), then kept, then Trash and Junk; what is cut is counted in the
>   "more" fields (`default_not_kept_more` is new). Measured worst case (64 accounts, 80-character
>   names of "<", 6 bytes each in JSON): see MA-1180's row; it stays under 1 MiB, a quarter of the
>   reader's 4 MiB.
> - **Name cleaning (ADV-C2-11)** also takes out every Unicode format character (category Cf: the
>   zero-width ones U+200B to U+200D and U+2060, the byte-order mark U+FEFF, the soft hyphen, the
>   tag characters, and the bidirectional controls the fold already listed) and the line and
>   paragraph separators U+2028 and U+2029.
> - **New codes and fields, the builder's names:** reasons `split_differently`, `other_reader`,
>   `walk_failed`; record fields `store_id`, `no_identity`, `split_mailboxes`,
>   `default_not_kept_more`; WARN code `tbstreak_unreadable` and its sentence. C3 owns the words of
>   the reasons.
> - **Gate owed:** the pass's own rerun ("Rerun after fixes": the four attack files against the
>   fixed commit). Not self-certified here.
>
> **Built 2026-11-08 [C2 pass round 2 fixes] (the builder's notes, for the lead).** Round 2
> (`docs/review-thunderbird-completeness-c2-adversarial-r2.md`, NOT CLEAN at `1e10b7c`) is answered
> fix by fix, its attack tests brought in as guards (rows MA-1279 to MA-1283). R2-04, R2-05, R2-01
> and R2-03 are §2.4's two round-2 folds. The rest:
>
> - **Stale streak entries (R2-08).** The streak record counts its runs (`run`); each entry keeps
>   the run that last reached its store, any verdict (`seen_run`), and the run that last found it
>   behind (`behind_run`). An entry no run has reached for **72 runs** is pruned, with a log line.
>   Why 72: three days of an hourly schedule, so a store any schedule reaches at least daily is
>   never pruned, with two missed days to spare, while a store dropped from the inputs, a moved
>   profile (a new path is a new key) or a removed account goes within three days of hourly runs.
>   Pruning a live entry only restarts its streak (a later notice, never a false one). Only runs
>   that end ok with Thunderbird results count. A full record (64) never refuses a new behind
>   store: the oldest entry this run did not find behind goes (the one reached longest ago), else
>   the oldest of all, with a log line naming both; the record's streaks are folded before new
>   ones, so an entry extended this run is never the one dropped. With any streak kept, the record
>   is written every such run (so the unseen runs add up). Planted counters are clamped (never
>   ahead of the record's run, never negative, the run capped at 2^40). **For C5:** an entry is
>   never a reason to email about a store the run did not reach.
> - **Accounts past the record's cap (R2-06).** Past 64 the record keeps the weakest first
>   (behind, limited, unknown, caught up but not complete, then complete; each group in the run's
>   order) and `thunderbird_more` says how many were left out; a record read back with more than 64
>   adds the cut ones to it. `tbcomplete.RunLeadWith(results, more)` takes each one left out as
>   unknown, so a lead built from the record is never "complete" over them. **C3 must build the
>   lead with it.**
> - **Name cleaning (R2-07)** removes every Default_Ignorable_Code_Point: Cf as before, and
>   Unicode's Other_Default_Ignorable_Code_Point and Variation_Selector (the combining grapheme
>   joiner, the Hangul fillers U+115F, U+1160, U+3164, U+FFA0, U+17B4 and U+17B5, the variation
>   selectors U+180B to U+180F, U+FE00 to U+FE0F, U+E0100 to U+E01EF), the fillers and selector
>   ranges also named outright. A name of only such characters becomes "". An emoji's U+FE0F goes
>   too (it shows the same).
> - **Fixtures amended, by the lead's call, not weakened:** the repo's classic "From sender <date>"
>   separators are no longer separators, so the shape test now lists them among the refused; the
>   fixtures that stood for Thunderbird's own messages carry `X-Mozilla-Status`, as Thunderbird
>   writes it. The adapted round-1 test `TestR1_ADV_C2_13` in the pass's clone has header-less
>   messages, so its 2,001 pieces now count as loose (0 strict): an expected red under the new
>   rule; this repo's copy (`TestADV_C2_13_HeaderShapesAreBounded`) gives its messages the line.
> - **New codes and fields, the builder's names:** reason `count_uncertain`; record fields
>   `no_status`, `glued_mailboxes`, `thunderbird_more`; streak fields `run`, `seen_run`,
>   `behind_run`. C3 owns the words.
> - **Gate owed:** the pass's own rerun (round 3: the clone's attack files against the fixed
>   commit). Not self-certified here.
>
> **Built 2026-11-08 [C2 pass round 3 fixes] (the builder's notes, for the lead).** Round 3
> (`docs/review-thunderbird-completeness-c2-adversarial-r3.md`, NOT CLEAN at `57d9cd3`, 4 LOW) is
> answered by the lead's four calls, its attack tests brought in as guards (rows MA-1279, MA-1281,
> MA-1283 amended). R3-01 is §2.4's round-3 fold. The rest supersede the round-2 note above where
> they differ:
>
> - **Pruning by time (R3-03).** An entry is pruned only when no run has reached its store for **30
>   days**, by the record's own times (`seen_at` per entry, `last_run_at` for the record), never by
>   a count of runs. A clock that went back never prunes; a run whose clock is before
>   MinPlausibleTime prunes nothing; a planted `seen_at` is clamped to `last_run_at` when read. Why
>   30: the longest schedule is weekly, so a weekly store has three missed weeks to spare; a store
>   no longer backed up goes within a month. The 72-run rule is gone.
> - **Capacity (R3-04).** The streak record keeps **256** stores (`TBStreakAccountsMax`, apart from
>   the last-run record's 64); its reader's bound is 512 KiB (the worst record at 256 is measured
>   by MA-1281's bound test). A full record evicts only an entry this run did not find behind,
>   **never one it found behind**. A new behind store that finds no room is logged by name and
>   leaves a WARN `tbstreak_full` in the last-run record, which `status` shows (the builder's
>   words, naming the record, how many wait and up to three of them; C3 owns the final ones).
>   **Builder's reading, for the lead to confirm:** "currently behind" is "found behind by this
>   run"; an entry reached this run only as unknown, or not reached, may still be evicted (the one
>   reached longest ago first). **For C5:** a store waiting under `tbstreak_full` has no streak, so
>   no "still downloading" email can come for it until room frees up.
> - **Name cleaning (R3-05).** U+2800 is removed; every other Zs space becomes U+0020; runs of
>   spaces become one and the ends are trimmed. Display only: no key is built from a cleaned name.
> - **Tests amended, by the lead's calls, not weakened:** the round-2 streak tests now state 30
>   days, 256 entries and "never evict a streak found behind" (the round-2 line "else the oldest of
>   all goes" is reversed by R3-04); round 3's R3-01 attack test keeps its trigger and asserts the
>   lead's rule (misaligned, nothing counted) as its effect in place of "lenient at least 3";
>   `TestADV_C2_09_PlantedStreakShapes` checks the streak's own cap and adds a record past the byte
>   bound.
> - **New codes and fields, the builder's names:** WARN `tbstreak_full`; record field
>   `misaligned_mailboxes`; streak fields `last_run_at`, `seen_at`.
> - **Gate owed:** the pass's own rerun (round 4: round 3's clone files against the fixed commit).
>   Not self-certified here.
>
> **Folded 2026-11-08 [C2 pass round 4] (the lead's call on
> `docs/review-thunderbird-completeness-c2-adversarial-r4.md`, NOT CLEAN at `6afd74d`, 2 LOW).**
> Neither finding is fixed in C2. Both are about the behind-streak record, which only slice C5's
> notices and email read, so both are **carried to slice C5**, which owns the streak's email and
> its own adversarial pass. C5 must do both of these before it merges:
>
> - **R4-01 (a planted run counter at its cap):** treat an implausible counter as a damaged record.
>   A record whose run counter is at or near its cap, or that is otherwise impossible for the runs
>   the archive has had, is not trusted: the streak **starts fresh** from that run, and the run
>   records a **WARN** (as `tbstreak_unreadable` does for a record other users can read), so it is
>   never silent. Fresh is the fail-safe reading for R42 (a later notice, never a false one).
> - **R4-02 (257 or more behind accounts evicting a weekly-only store silently):** **every
>   eviction raises `tbstreak_full`**, not only a new behind store that finds no room. A store
>   whose streak is dropped to make room for another is named in that WARN, so `status` shows it.
>
> C3 does not change the streak record. Until C5 lands, a planted counter can only delay or stop a
> "still downloading" notice that C5 has not built yet; no surface of C3 reads the streak.
>
> **Built 2026-11-08 [C3] (the builder's notes, for the lead and for C3's friction review).** C3
> words the verdicts on every surface, from one producer: `internal/state/tbwords.go` (beside
> `NotDownloadedSentence`, as §7 says), over the record's shape (`state.TBAccount`). The run's
> lead is `tbcomplete.RunLead(results)` in memory and `tbcomplete.RecordLead(accounts,
> thunderbird_more)` from a record, which goes through `RunLeadWith` (R2-06), so a lead built from a
> record is never "complete" over an account left out. In-memory verdicts reach the screen through
> `app.TBAccounts(result)` (the record's shape, names cleaned and capped, never written). Rows:
> MA-1182, MA-1183, MA-1185, MA-1186 and MA-1198 new; MA-1181, MA-1180, MA-1205, MA-1261, MA-1268
> and MA-1271 amended. No id above MA-1283.
>
> - **Surfaces built:** the wizard's end summary (`exportSummary`: the lead, then each account's
>   lines, then today's counts) and its wait (`syncPausedText`); Run now's note, plaintext
>   (`runNowSummaryFor`: "Backup finished: …" or "Backed up what Thunderbird has so far: …", then the
>   lines) and encrypted (`encryptedSummaryFor`, M8 and MF1: "Backup complete:" only when every
>   account may say complete or the run read none); the command line's summary ("Thunderbird: LEAD",
>   then the lines); Recent backups ("OK — Thunderbird still downloading" for a behind run, the
>   compact words in the detail); the archive card's one line (`TBBanner`); `status` and Status &
>   health (WARN `thunderbird_behind`; `thunderbird_limited` and `thunderbird_not_kept` WARN once,
>   then a note, by TC-19; notes `thunderbird_caught_up`, `thunderbird_unknown`,
>   `thunderbird_default_not_kept`, `thunderbird_labels_not_kept`, `thunderbird_gmail_cap`,
>   `thunderbird_more`; the Last run line ends with the lead); `status -json`
>   (`thunderbird_completeness`, `thunderbird_more`, `thunderbird_lead`).
> - **"Complete" words:** "everything" is said only when the account may say complete, read from
>   the record by `TBAccount.MaySayComplete()` (its complete flag **and** its own numbers), so a
>   planted record never gets it. Every other caught-up case says both numbers and, in one sentence,
>   why not (missing within the tolerance, stale by M9's words, deleted copies, messages not told
>   apart, a not-kept folder, the cap note). Every `unknown` reason has its own sentence, from §4.4's
>   table and the C1b and C2 codes: `invalid_entry`, `duplicate_key` and `cache_unreadable` share
>   "could not read Thunderbird's list of folders"; `not_counted_yet` says "Thunderbird has not
>   counted all of its folders yet"; `old_path_only`, `count_uncertain`, `split_differently`,
>   `other_reader`, `walk_failed`, `expunged_unsized`, `partial_input`, `no_match` and
>   `account_type`, `panorama`, `never_checked` and `gmail_floor` (O3, with its All Mail clause) each
>   their own. A reason this version does not know reads as "could not read".
> - **L4's open item:** settled by §0's lead's call (Spam, Trash and Gmail labels not kept are notes
>   and do not block "complete"; any other not-kept folder that lists mail does). Built so.
> - **L5 (version-chosen labels):** the record now carries `thunderbird_version`
>   (`compatibility.ini`'s major, 0 when it cannot be told). Below 160 the older labels, 160 and
>   above the newer ones, and both quoted when it cannot be told; the age limit stays neutral at 160
>   and above. **Lab row MA-1191 is still owed** before these labels ship (L5's own note); the
>   labels are the M4 re-read's, from source.
> - **TC-17:** "later backups add the rest" only when a schedule other than "only when I click Run
>   now" runs the archive (`app.TBScheduled`, read from the archive's schedule record); a one-off
>   wizard export always asks for the backup to be run again.
> - **TC-19 (WARN once):** a chosen setting WARNs when its first-seen time in the record equals that
>   record's run end (C2 sets it to the run's end when the hash is new) or is missing; otherwise a
>   note.
> - **Record fields changed (additive):** `stale` is always written, as `complete` is (M9:
>   both booleans in every `-json` entry); `thunderbird_version` (above); the history's `tb`
>   entries carry `floor`, so Recent backups says "at least" (R43). `-json` keeps the record's
>   names: `floor`, not §7's `listed_floor`.
> - **The C0c follow-ups:** N2 (user.js's "Delete the line that contains" names the setting in
>   Thunderbird's escaped form, `\"` and `\\`), N3 (the five "what" fragments that said "Thunderbird
>   never writes" or "writes" now speak of Thunderbird's settings format or what it uses; prefs.js
>   keeps "Thunderbird never writes such a line"), and the pre-existing note (a server key's control
>   characters are stripped in the command line's log and in `HandEditedError`'s "for KEY"). Two
>   C0c word pins were moved to the new fragment, nothing dropped.
> - **The streak's two WARNs:** `tbstreak_unreadable`'s three sentences are reworded plainly (they
>   still name the file, which `status` may do); `tbstreak_full`'s are unchanged (R4-02's wider
>   use is C5's).
> - **Not built, by the design or the builder's reading:** BACKUP-NEEDS-ATTENTION.txt is unchanged
>   (the design does not name it for completeness: a behind run is a success, and the notice is for
>   failures). `-json`'s `behind_since` is not added: it comes from the streak record, which C5
>   surfaces. An encrypted archive's card shows no completeness line: its completeness is in Run
>   now's note, which the Desktop already clears at Lock or a switch (C-15); the card's own "last
>   result while unlocked" (§7's TC-6 fold) is not built here. Local Folders and POP accounts never
>   get a line (C2 gives them no result).
> - **For the friction review:** the Gmail cap note avoids the word "IMAP" by naming Gmail's setting
>   "Folder size limits"; the page Gmail shows it on is called "Forwarding and POP/IMAP" (not
>   checked live, and TC-22 is unverified: MA-1201). The Recent backups words for `limited` and
>   `unknown` ("Thunderbird's own settings keep only part of ACCOUNT's mail on this computer",
>   "MailArchive could not tell how much of ACCOUNT Thunderbird has downloaded") are the builder's.
> - **Gate before C3 (L6):** the independent narrow check of the M6-residual note and of foldcheck2
>   is not filed in `docs/` on this branch as far as the builder could find. The lead should say
>   whether it was done elsewhere.
> - **Gate owed: the friction review** (§13's C3 row: Windows on Edge first, then the Mac), with the
>   X-series walk. MA-904's checks are unchanged and green. Not self-certified here.
>
> **Folded 2026-11-08 [C3 friction review; fold check 3's N1 and N2] (the owner's rulings and the
> lead's calls; built, for the friction re-look).** The friction review
> (`docs/review-thunderbird-completeness-c3-friction.md`, SHIPPABLE_WITH_NOTES at e85d840) and fold
> check 3 (`docs/review-thunderbird-completeness-foldcheck3.md`, PASS narrow) are answered:
>
> - **M1 (owner ruling 2026-11-08): the account's name.** On screen an account is said by the name
>   Thunderbird shows for it: `mail.server.<key>.name`, else `mail.server.default.name`, as
>   `nsMsgIncomingServer::GetPrettyName` reads it (ESR 153 `nsMsgIncomingServer.cpp:558-562`,
>   `:589-595`), from the same one read of prefs.js and user.js through C0c's allow-list reader
>   (`thunderbird.ReadStoreFacts`, `StoreFacts.Name`). Only a quoted text counts. An empty, mistyped
>   or absent name is no name (Thunderbird then builds one from a localized string, which is not
>   modelled), and the words say "the Thunderbird account kept in the folder NAME" (the owner's
>   fallback). The name never takes part in the account match. It is cleaned and capped as TC-7.
>   Two accounts with one name (compared without letter case, after cleaning) are both said by
>   their folders (`app.TBAccounts`).
> - **The name is never written** (superseded the same day by the note below: the plaintext
>   record and history now keep it). `state.TBAccount.Name` is `json:"-"`: the last-run record,
>   `-json`, the run history and the alert email keep the store folder (§9.1, §9.3). So every
>   surface that reads a record (`status`, Status & health, the archive card, Recent backups) says
>   the folder words; the wizard, Run now and the command line say the name. §7's "never by its
>   email address" and its TC-10 "Names" bullet are superseded by this note for the words on screen.
>   Row MA-1284.
> - **L4 (owner ruling): "Backup complete."** replaces "Export complete." as the completeness lead
>   (`state.TBLeadComplete`): the wizard's end summary, the command line's "Thunderbird:" line and
>   `status`'s Last run line. The encrypted Run now note's "Backup complete:" is unchanged. Wherever
>   this design names "Export complete." as the lead, read "Backup complete.".
> - **M2 to M9 and L1 to L3 (the lead's calls: as proposed).**
>   - P2: two or more accounts behind say the remedy once, after the last of them (`TBLines`).
>     `status` keeps a remedy in each `thunderbird_behind` WARN (X6).
>   - P3: `TBSay.Surface`. With no schedule, the wizard and `status` say "Run the backup again…",
>     Run now "Click Run now again…", the command line "Run the same command again…". P2's single
>     line follows the same choice.
>   - P4: every Account Settings remedy opens with "click the menu button (≡, top right), then
>     Account Settings". Lab row MA-1191 still owes the Mac's 159.
>   - P5: the cap note names Gmail's path, quoting Gmail's own labels. Recollection, unverified
>     until lab row MA-1201.
>   - P6: the encrypted Run now count in words ("no new messages", "1 message"). The sealed
>     sentence stays when nothing was written ("Your messages are sealed…"), so the Desktop still
>     turns it into its Open archive hint (MA-852's test caught the first build dropping it).
>   - P7: Recent backups' encrypted rows say "backup finished".
>   - P8: the four next steps. "Opening Thunderbird and letting it check your mail usually fixes
>     this" is kept, unverified until a lab note sees which event writes `folderCache.json` again.
>   - P9: the Gmail floor sentence. P11: Thunderbird's usual folders named as the record lists
>     them. P12: "Inbox". P13: "message" after a Y of 1.
> - **M10 (the lead's call: a small design change).** §7's Desktop archive card row gains the
>   limited accounts, after the behind part and before the not-kept count: "Thunderbird keeps only
>   part of ACCOUNT's mail on this computer (about X of Y messages), so the rest is not backed up",
>   or "…of the mail of N accounts…". Row MA-1182.
> - **The builder's choices inside those calls, for the re-look:**
>   - `partial_input` now reads "Only part of ACCOUNT was backed up, …", so the folder words read
>     as a sentence;
>   - a possessive with no name is "the mail of the Thunderbird account kept in the folder NAME";
>   - the Gmail floor's verb follows X ("About 1 message … is");
>   - P11 with more folders than the record lists: "the Trash folder and 2 more folders of
>     ACCOUNT";
>   - P8's "then run the backup again" is P8's words exactly, not named by surface;
>   - the card's "about 0 of 1 message are" keeps "are".
> - **N1 (fold check 3; the lead's call).** `planTBOther`: a store another reader opened, with no
>   profile above it, is kept when its folder holds any `.msf` entry, seen by name in one
>   `os.ReadDir`. It is then `unknown`, `no_match` (the unmatched rule's reason; `Assess` gives
>   `other_reader` only to a matched store), never no result. A folder that cannot be listed shows
>   none: its walk fails too, and the failure words win (§11). A real Evolution store is still told
>   nothing. This amends the ADV-C2-16 bullet of §13's C2-pass note. Row MA-1196.
> - **N2 (fold check 3).** The three tests are added: a store more than six levels below a profile
>   holding a `.msf`; a single mbox file input with and without a `.msf` beside it; real encrypted
>   runs of an unmatched store holding a `.msf` and of an `ews` store, through
>   `encryptedSummaryFor`. Rows MA-1196 and MA-1186.
> - **Backlog, not built (layout; the lead's call):** L5 (Status & health's monospace box hides the
>   longest remedies below its fold on a 1080p-class screen) and L6 (the card line sits under some
>   rows and beside others, by its length).
> - **Owed:** the friction re-look of the changed words. Whether the name lookup owes an
>   adversarial look is the lead's call (the commit gives the builder's reasons).
>
> **Folded 2026-11-08 [the account's name on every surface] (the owner's ruling and the lead's
> call; built).** The owner ruled that accounts are named by the name Thunderbird shows (usually the
> email address). The lead's call: every surface shows it, not only those that see the live run.
> This supersedes the "name is never written" bullet of the fold above.
>
> - **What is written.** Each last-run `thunderbird` entry and each run-history `tb` entry gains an
>   optional `name`: the cleaned, capped name (TC-7), or absent when the lookup fell back or when two
>   accounts of the run share one name (compared without letter case, after cleaning:
>   `state.TBDistinctNames`, applied by `app.tbRecords` before the record is written). Plaintext
>   archives only: an encrypted archive's record and history are their allow-lists, unchanged (O1),
>   and an archive's older plaintext entries lose their names with its first encrypted entry (F19).
> - **What stays.** `account` stays the store folder and is the key everywhere: the streak record,
>   TC-19's carry-forward (by `store_id`), `-json`'s `account` and folder fields. `-json` gains `name`
>   alongside (additive). An older record or entry without `name` reads as before, in the folder words.
> - **What reads it.** `status`, Status & health, the archive card and Recent backups say the name
>   when it is present and still has something visible after cleaning; else "the Thunderbird account
>   kept in the folder NAME". Readers clean it again on read (`saneTB`, `saneTBCompact`: control and
>   format characters, the cap) and apply the shared-name rule again, since anyone who can write the
>   archive folder can write the record. The Desktop renders it as everything else: html/template for
>   the card and Recent backups, JSON and `textContent` for Status & health.
> - **Privacy (§9.1 amended; why there is no new exposure).** The address is written beside the
>   archive only in files that sit next to the mail itself: the last-run record and the run history,
>   in the archive folder. That mail already holds the address (the account's own mail is to and
>   from it), in the plaintext archive's own message files and pages. So whoever can read these
>   files can already read the address. A name that is not an address is a label the person chose
>   in Thunderbird, beside their own mail.
>   The same holds for logs: a scheduled run's `-log` file sits in the archive folder, and the
>   command-line summary has carried the name there since the fold above. A `-log` path the person
>   types by hand can be anywhere; that is the person's choice. §9.1's "only the per-account X and Y
>   go into the run history" now reads "X, Y and the name".
>   **Superseded 2026-11-09 (the lead's call on the name pass's N-04, fail-private; row MA-1288):**
>   the sentence "The same holds for logs…" above was true only for the default path. A `-log` can
>   sit outside the archive (a custom `schedule -log`, a hand run), and a keyed run into an
>   encrypted archive must keep its `-log` outside it (`RefuseLogInside`). So no log file holds the
>   name: the command line's summary says it only when its logger writes to a terminal; a `-log`, a
>   stderr sent to a file and any other writer get the folder words. A scheduled run-job's own log
>   never carried completeness words. Encrypted runs: no address anywhere outside the screen. The
>   owner's earlier "tell me if you'd rather keep addresses out of logs" was not answered; this is
>   the lead's call, and the owner may reverse it.
> - **Not changed.** §9.3: an email leaves the computer. No completeness email is built yet; when one
>   is, it keeps the store folder unless a new ruling says otherwise.
> - **Size.** At the bounds (50 history entries of 64 accounts, each folder and name 80 characters
>   of `<`, 6 bytes each in JSON) the history is 3,561,752 bytes, within `ReadRunHistory`'s 4 MiB
>   (about 85%); the last-run `thunderbird` field is 590,145 bytes, within its 1 MiB share. Measured
>   by MA-1284's and MA-1180's tests. Real names are far shorter.
> - Row MA-1284 (amended).
>
> **Folded 2026-11-09 [the re-look's words and the name pass] (the lead's calls; built).** The C3
> friction re-look (`docs/review-thunderbird-completeness-c3-friction.md`, "Re-look after the fold
> rounds", R1 to R6) and the C3 account-name pass
> (`docs/review-thunderbird-completeness-c3-name-adversarial.md`, N-01 to N-04):
>
> - **R1.** The Gmail floor's "(see below to keep it)" stays where an account's lines are said in
>   order (the wizard, Run now, the command line); `status` and Status & health say every WARN
>   before the notes, so there it reads "(the line that names [Gmail]/All Mail says how to keep
>   it)", the folder as the not-kept line names it (`TBSay.Apart`).
> - **R2.** With two or more accounts behind, the one remedy goes after every account's lines and
>   says whom it is for: "For the 2 accounts still downloading: leave Thunderbird open (it downloads
>   mostly while the computer is left alone), then run the backup again to add the rest." (each
>   surface's own last clause; "…; later backups add the rest." with a schedule). This amends
>   P2's "after the last of them".
> - **R3.** Nothing downloaded yet: "Thunderbird has not downloaded any of the 1,200 messages in
>   ACCOUNT yet." (Recent backups the same; the card "…: none of its 1,200 messages are on this
>   computer yet"). This supersedes the C3 friction fold's "the card's 'about 0 of 1 message are'
>   keeps 'are'": the card now says "its 1 message is not on this computer yet".
> - **R4.** With no name, a limited warning says the full fallback words once (the headline), then
>   "this account's mail"; the fix-it step keeps the full words.
> - **R5.** `status` and Status & health say "Click Run now again" for a Run-now-only archive.
> - **R6.** "all of ACCOUNT's mail" (and "Only part of…", Recent backups' "how much of…"); a name
>   that already ends in "mail" ("Work mail", "AT&T Mail") is said as it is, never "Work mail's
>   mail" (the builder's call, low confidence; the lead may prefer the possessive everywhere).
> - **N-01 (MEDIUM, Linux).** Every zenity list dialog's text passes through `wizard.listText`:
>   on Linux and the BSDs every backslash is doubled and `& < > " '` escaped as Pango markup, since
>   zenity's list sets its text with `gtk_label_set_markup(g_strcompress(text))`; on macOS and
>   Windows the text is unchanged. Names, folder names and paths alike. The list's choices are
>   not escaped (zenity returns the chosen one as given; whether GNOME zenity draws them as markup
>   was not settled from its source here). Not seen on a Linux desktop. Row MA-1286.
> - **N-02, N-03 (LOW).** A name that, compared after cleaning, NFKC and Unicode case folding, is
>   "this account" or starts with "the Thunderbird account kept in the folder" is no name; the
>   shared-name rule compares by the same key. **Documented limit:** letters of another script that
>   look the same (a Cyrillic "а") are not folded. Row MA-1287.
> - **N-04.** See the superseding note above: no log file holds the name. Row MA-1288.

> **Folded 2026-11-09 [C3 name confirm R2-01, R2-02] (the lead's calls; built for R2-01, words only
> for R2-02).** `docs/review-thunderbird-completeness-c3-name-adversarial-r2.md`.
>
> - **R2-01 (LOW, Linux and the BSDs) supersedes the N-01 bullet above.** The ncruces/zenity module
>   runs qarma first when it is installed (then zenity, then matedialog). qarma shows a list's text as
>   plain text, so `listText`'s escapes showed literally there (`AT&amp;T`, and `&quot;` in the
>   copyable `verify -out` command). No one text shows as written in all three lists. So on Linux
>   and the BSDs no list dialog is given text that can vary: the launch health view's text is shown
>   first in a message dialog, which the module always opens with `--no-markup` (plain text, exactly
>   as written, in all three; its button says Next), and the list then says only the fixed prompt
>   "Backup health — what would you like to do next?" (`wizard.textThenList`). macOS and Windows
>   are unchanged. `listText` is removed; every other list prompt is fixed words with no `&`, `<`,
>   `>` or backslash, passed as written. Row MA-1286 (amended). The lab check owed is now on both
>   tools: zenity and qarma, an account named "AT&T Mail", then `mailarchive wizard`.
> - **R2-02 (LOW) widens the documented limit in the N-02, N-03 bullet above.** Characters Unicode
>   lists as confusable, in any script (Cyrillic, Greek, Latin variants such as ɑ ɡ ı, hyphen
>   variants), and wordings that drop the leading "the", are not caught by the same-name and
>   fallback-imitation rules. Folding them needs Unicode's TR39 "skeleton", which
>   `golang.org/x/text` does not provide. Row MA-1287 (amended).

> **Built 2026-11-09 [C5] (the builder's notes, for the lead, the adversarial pass and the friction
> review of the email).** C5 sends the "still downloading" and "stopped" emails for a plaintext
> archive's Thunderbird accounts (§5 with its TC-20 and M10 folds, §9.3), adds `-json`'s
> `behind_since` and the README line for `.mailarchive-tbstreak.json`, and fixes the C2 pass round
> 4's R4-01 and R4-02 carried here. Rows: MA-1188 new; MA-1184, MA-1183 and MA-1281 amended. No id
> above MA-1288.
>
> - **Who decides, who sends.** The run that folds the streak (`app.foldTBStreak`, under the
>   archive's lock) names the streaks due an email in `app.Result.TBNotices`
>   (`state.TBStreakAccount.Due`): only an account this run found behind, whose streak the record
>   holds, and only when the record was written. A store the run did not reach, or found unknown,
>   is never named (C2's "never email about a store the run did not reach"), nor one waiting under
>   `tbstreak_full`. The scheduled local job (`RunHeadless`) sends them through
>   `backupalert.ReportTBBehind`; then `app.MarkTBNoticesSent` sets the flags under the archive's
>   lock (a 5-second wait), matching the record's `record_id`, the store and the streak's serial.
>   A Desktop Run now and a command-line run fold the streak but send nothing (as today: only the
>   scheduled job reports to `backupalert`); a streak that crosses 3 days at such a run is emailed
>   at the next scheduled behind run.
> - **Once per streak (TC-20).** Stopped is due when X has not grown across behind runs spanning 3
>   days and it was not sent; else still-downloading when the behind runs span 3 days (at least 2
>   runs) and neither was sent. The stopped email sets both flags, so a still-downloading email
>   never follows it, and a streak that stalls from its start gets only the stopped one.
> - **The builder's readings, for the lead:**
>   - **One email per run, every due account in it.** Several accounts due in one run share one
>     email (the stopped subject when any stopped; the stopped lines first). The design said "one
>     email of each kind per streak" and nothing about runs.
>   - **One email per run with the other emails.** A run that sent the recovery email or the
>     Thunderbird-not-synced email defers this one to the next run, as the presync design's §5.5
>     does for its own; nothing is marked, so the next due run sends it.
>   - **Alerts off marks at once**, as the Thunderbird-not-synced email does: turning alerts on
>     later in the same streak sends nothing for it. The gate is the failure option (`OnFailure`),
>     as that email's.
>   - **A failed delivery** sets nothing; each later behind run of the streak tries again until it
>     is delivered (the existing "fail loud, retry" style). A mark that fails after delivery (the
>     lock held by another run for 5 seconds, a write refused) is logged, and the next due run may
>     send a second email of that kind: toward a repeat, never silence.
>   - **The email names the account by its folder** ("the Thunderbird account kept in the folder
>     NAME"), never by the name Thunderbird shows (§9.3, MA-1288's fail-private reading; the
>     notice carries no name), with counts only and no mail folder (no "furthest behind" list).
> - **The words** (built in `state.TBNoticeEmail` and `alert.tbBehindSubjectAndBody`; the friction
>   review owes their look). §7's sentences, with the folder wording and the friction review's
>   menu path; the stopped remedy's "Synchronization & Storage" page is the limited fallback's
>   wording (all versions; lab row MA-1191 still owes the labels). Subjects: "MailArchive:
>   Thunderbird is still downloading mail after 3 days — ARCHIVE" and "MailArchive: Thunderbird has
>   stopped downloading mail — ARCHIVE". The days are whole days between the streak's first and
>   latest behind runs, never to now.
> - **`behind_since`** (`status -json`, additive in each `thunderbird_completeness` entry): the
>   streak's first behind run, only for an account the last backup found behind and whose streak
>   that record's latest fold extended; never after the record's run, never before
>   MinPlausibleTime, never from an implausible record.
> - **R4-01:** `state.TBStreak.Implausible` (a run counter, serial or behind-run count past 2^24,
>   one backup a minute for 31 years, far below the 2^40 cap where the counter froze; or an entry
>   whose serial is above the record's) is a damaged record: fresh streak, a log line, a
>   `tbstreak_unreadable` WARN with its own sentence, the record rewritten.
> - **R4-02:** every eviction raises `tbstreak_full`, naming the dropped store, in the same WARN as
>   the stores that found no room. Round 2's test asserting "no WARN" on an eviction is amended to
>   the lead's call (stricter: one `tbstreak_full` naming the dropped store).
> - **Not built:** the archive card's own still-downloading and stopped banner (§5, §7's card row
>   "a one-line banner for behind, still-downloading or stopped"): the card keeps C3's behind line.
>   A planted streak record (owner-only, so written by another hand) can still overstate the days
>   or the since-date in an email for a store this run found behind; for the adversarial pass.
> - **Gates owed (not self-certified):** the **adversarial pass** on the emails and the streak
>   record's read (the caller's brief), and the **friction review** of the email words (§13's C5
>   row).

> **Fixed 2026-11-09 [C5 fix round] (the lead's calls on the adversarial pass and the friction
> review of a385f97).** Both gates' findings are built; a short adversarial confirm is owed.
>
> - **SPAM-01 (the shared sender).** An email the mail server accepted (its reply after the end of
>   the message data) is sent. A failure after that (the goodbye unanswered, the connection
>   dropped) is `alert.ErrAfterAccepted`; `backupalert`'s one sender logs it and counts the email as
>   sent. Every alert email goes through it, so this **also fixes the already-shipped
>   Thunderbird-not-synced email** (design-presync-attended §5.5), which could repeat at every stale
>   backup the same way. A second guard: `ReportTBBehind` drops a notice whose email the record
>   already marks sent. Row MA-1289 (new).
> - **FALSE-01, FALSE-02, MISS-01, PLANT-01:** §5's fold of 2026-11-09. Rows MA-1184, MA-1281 and
>   MA-1183 amended.
> - **The words:** the friction review's P1 to P4 and every low, exactly as proposed: the subjects
>   "MailArchive: Thunderbird still downloading (backups OK) — NAME" and "…stopped downloading
>   (backups OK) — NAME" (NAME the last part of the archive's path; never a day count); the "Your
>   backups are working…" openings; "Archive:" and "Computer:" lines (no Computer line when the name
>   is unknown); one line per account ("The account kept in the folder NAME: …"); one remedy line, or
>   the two numbered stopped steps; the which-account line; the two once-only lines. The folder
>   words stay (never a name or an address, §9.3). The remedy is now a list of lines. Row MA-1188
>   amended. "on this computer" in step 2 is the review's own word, echoing Thunderbird's setting.
> - **Backlog:** a Mac's computer name ends ".local" in every alert email (the review's L7); the
>   older alert emails' subjects keep the full path (L1).
> - **Still owed by lab row MA-1191:** that "Server Settings" shows the folder at the bottom, and
>   that Thunderbird 160 keeps the "Synchronization & Storage" page name.

> **Fixed 2026-11-09 [C5 round-2 fix] (the lead's calls on the round-2 confirm, which was NOT
> CLEAN: 1 MEDIUM, 3 LOW).** CLOCK-R2-A (MA-1290 new), WARN-R2 and PLANT-R2 (MA-1281 amended),
> SPAM-R2-A (MA-1291 new; also the shipped sync email): §5's amendment of the same date.
> - **Gates owed (not self-certified):** a short **adversarial confirm** of this round, and the
>   two new WARNs' words (`tbstreak_clock_behind`, `alert_send_failed`) and the damaged WARN's new
>   words go to the **friction review** (the round-2 pass asked for it: CLOCK-R2-A changes when the
>   WARN shows and what it says).

> **Fixed 2026-11-09 [C5 round-3 fix] (the lead's calls on the round-3 confirm, which was NOT
> CLEAN: 1 MEDIUM, 2 LOW; friction SHIPPABLE_WITH_NOTES).** CAP-R3-A and FRIC-R3-1 (MA-1291
> amended; also the shipped sync email), the clock words, TIE-R3 and GAP-R3-C's residual (MA-1290
> amended): §5's amendment of the same date.
> - **Gate owed (not self-certified):** a TINY **adversarial confirm** of this round: the delivery
>   mark and its id, the daily rule, the WARN's clearing, and the TIE-R3 line.

> **Folded 2026-11-09 [C5 round-4 confirm] (lead's call).** The round-4 confirm
> (`docs/review-thunderbird-completeness-c5-adversarial-r4.md`, 50be038) found no MEDIUM or HIGH:
> CAP-R3-A is fixed (a working server gets each email exactly once after an outage; a dead server
> gets 5 tries, then one a day, per email type). Its two LOW findings are accepted as documented
> residuals, and slice C5's adversarial gate is closed:
> - **QUIET-R4-A:** a server that refuses only the Thunderbird email while delivering the
>   "every successful backup" email (off by default) keeps resetting that email's failed tries, so
>   it is tried at every due backup and `alert_send_failed` never shows. Follow-up for a later
>   polish: count this email's own failures toward the WARN without resetting them on another
>   email's delivery.
> - **DEFER-R4-B:** a run whose "working again" email failed still holds back the Thunderbird email
>   and adds no WARN for that one run; the next backup shows it.
> - The review's two notes (an unwritable settings folder keeps old failure counts for up to a day,
>   logged; a clock that was fast at the last failure can delay one try up to two days) are accepted.

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

> **Folded 2026-11-07 (owner defaults: the answers to §14):**
>
> - **Q1:** O2's formula (§4.3 fold), not the one above: max(min(20, ⌊Y/10⌋), ⌊0.2% × Y⌋), once
>   per account; both numbers whenever any mail is missing.
> - **Q2:** yes, a success with a note. Each email kind at most once per streak (§5 fold, TC-20).
> - **Q3:** yes, WARN from the first behind backup. A chosen setting gets a WARN once, then a note
>   (TC-19).
> - **Q3b:** answered by O1 instead. Nothing is written beside an encrypted archive; the words are
>   on screen only, while unlocked; no email.
> - **Q4:** the answer stands, but the work moves to its own later design (TC-18).
> - **Q5:** no, as recommended. MailArchive never ticks a Thunderbird folder setting itself. The wizard's
>   existing account-level offer stays, and it now acts on the right account (C0c).
> - **Q6:** handled by the gmail-dedup design (§3 fold), with a line in this build's release notes.
> - **Q7:** keep archiving them; leave them out of the count, with the TC-16 estimate (§2.4 fold).
> - **Q8:** no `.msf` reading in this design.

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

> **Folded 2026-11-07 [TC-14] (lead's call + review condition): every Thunderbird claim above is
> [proven at trunk]**, at ab60cc5e, not at the ESR people run. Read each "[proven]" about
> Thunderbird's source as "[proven at trunk]" until the re-read below relabels it.
>
> **What is re-read.** These load-bearing claims, at the ESR 140 branch of the same source (the
> folder expects it to be named `comm-esr140`, but did not check that name):
>
> 1. the folder-cache fields and their types (§2.1 fold), including whether `expungedBytes` is
>    written;
> 2. the save rules (one minute after a change, at close, temp-and-move);
> 3. the Gmail single-copy lookup and its `is_gmail` gate;
> 4. the Panorama gate and the pref's name;
> 5. the defaults of `autosync_offline_stores`, `autosync_max_age_days` and the size limit;
> 6. the account-settings labels (TC-13), and the version where they changed;
> 7. `lastSyncTimeInSec`'s meaning.
>
> Each is relabelled "[proven at ESR 140]" in a dated note beside it, or amended where the ESR
> differs.
>
> - **Who:** the lead, meaning the agent running this build, does it as a C0 doc task. The
>   operator (Alex) sees the dated §16 note before the release call. The lab rows then confirm it on
>   real installs (MA-1189, MA-1191, MA-1192 run on ESR 140 and the current Release).
> - **When:** before C1a merges, and in any case before release.
> - **When again:** at each new Thunderbird ESR. The trigger is that ESR's release, recorded as a
>   revisit note in the project's memory, as the Go toolchain pin is. Whether an ESR newer than 140
>   is already out was **not checked** for this fold; confirm it at the re-read, on Thunderbird's
>   release page.
> - **The release notes** name the Thunderbird versions the lab rows ran on.
>
> **Read for this fold:** `nsMsgDBFolder.cpp:1232-1250` and `nsImapMailFolder.cpp:1889-1953`, at
> trunk ab60cc5e. They come from the review's downloaded copy in the main checkout's untracked
> `.claude/tbsrc-review/`, which the folder did not fetch fresh. The folder also read
> `internal/util/openregular.go:44-79` at 22330a8. Of the gmail-dedup design at 9203aae, it read
> only the id lines, §6's detection rule and §13 to §16.
>
> **Folded 2026-11-07 [foldcheck M4] (lead's call): the re-read targets the ESR the labs ran.** Every
> "ESR 140" in the TC-14 fold above (the branch, the relabelling "[proven at ESR 140]", the lab rows,
> "an ESR newer than 140") is **superseded by [foldcheck M4]**:
>
> - **Target: ESR 153.** This repo's labs ran **Thunderbird 153.6.0esr** on Windows
>   (`docs/lab/presync-attended-start-windows-run1-2026-10-31.md:13`; `thunderbird.go:51`) and on Linux
>   (`docs/lab/restore-thunderbird-macos-2026-11-06.md:81`). The branch is expected to be named
>   `comm-esr153`; that name is **not checked** here. Claims are relabelled "[proven at ESR 153]".
> - **Also named: the current Release, 159.0**, which the Mac lab ran
>   (`docs/lab/restore-thunderbird-macos-2026-11-06.md:12`, `:23`). Where trunk and ESR 153 differ, the
>   re-read says which one Release 159 follows, where its source makes that plain.
> - **Confirmed at the re-read** on Thunderbird's release page: that 153 is the newest ESR and 159
>   (or later) the current Release. Neither is confirmed by this fold.
> - **Added to the list of claims re-read** (items 1 to 7 above stand): 8. the All Mail and Archive
>   flags (`nsImapMailFolder.cpp:1230-1232`; `nsMsgAccountManager.cpp:1541-1542`), for M5; 9. that
>   Thunderbird writes a current-path cache entry for every folder it has loaded, and that a folder it
>   knows always has a `.msf` in the store, for M2; 10. that `expungedBytes` counts offline bytes
>   (`nsImapMailFolder.cpp:1388-1395`), for M7.
> - **Who and when:** the lead (the agent running this build) does it as a C0 task, **before C1a
>   merges**; the operator (Alex) sees the dated note before the release call. Lab rows MA-1189,
>   MA-1191 and MA-1192 run on ESR 153 and the current Release.
> - **When again:** at each new Thunderbird ESR, as written above.
>
> **Folded 2026-11-07 [M4 re-read] (the lead's re-read, done by a delegated agent): done.** The
> results are in `docs/lab/thunderbird-completeness-source-esr153.md`.
>
> - **Versions confirmed** on 2026-11-07 by Mozilla's product-details feed (the data behind the
>   release pages): ESR **153.6.0esr** (no next ESR listed) and Release **159.0**. 160.0 had a build
>   tag but no release.
> - **Revisions read.**
>   - ESR: `hg.mozilla.org/releases/comm-esr153`, tag `THUNDERBIRD_153_6_0esr_RELEASE` =
>     c3fd7a6092572ef295fd562948356e5cea174bbb. The branch name `comm-esr153` is now checked.
>   - Release: `hg.mozilla.org/releases/comm-release`, tag `THUNDERBIRD_159_0_RELEASE` =
>     1e7f8bb744a4a8e407f17b79d3ff59694ef80f54.
>   - The GitHub mirror `mozilla/releases-comm-central` carries only trunk (and 2009-2011 branches),
>     so it cannot serve an ESR re-read.
> - **Relabelled.** Every Thunderbird-source claim above marked "[proven]" or "[proven at trunk]"
>   now reads **"[proven at ESR 153.6.0esr and Release 159.0]"**, with three exceptions:
>   1. the account-settings labels, which differ (§6 fold [M4 re-read]; the switch-over is 160);
>   2. the Exchange store layout (`ExchangeProtocolInfo.cpp`), which was not re-read;
>   3. the Release 159 half of claims 1d, 2b, 5c and 9, where only the same code was located, not
>      read line by line (lab file §2).
>
>   Items 1 to 10 of the lists above all hold: fields and types, save rules, the Gmail lookup and
>   its `is_gmail` gate, the Panorama gate (`panorama.sqlite`; `mail.panorama.enabled`, default
>   false, read once at start-up), the defaults, the meaning of `lastSyncTimeInSec`, the All Mail and
>   Archive flags, M2's condition (with the clean-close limit, §4.1 step 2 fold), and `expungedBytes`
>   counting on-disk bytes. That last one also has a second path: deleting a header whose body is
>   on disk adds its on-disk size (`nsImapMailDatabase.cpp:49-58`). `nsShouldIgnoreFile` also holds,
>   and matches MailArchive's port in `internal/tbrestore/names.go`.
> - **Line numbers** move between versions. The design's citations stay trunk lines; the lab file
>   gives the ESR 153 and Release 159 lines per claim.
> - **The gate.** The source half of the C1a gate (field names, types and `expungedBytes`) is met.
>   The lab rows (MA-1189, MA-1191, MA-1192, MA-1199, MA-1200) are unchanged and still owed before
>   release.
> - **Not re-read:** ESR 140 (no longer the listed ESR).

## 17. The folds re-read for contradictions (2026-11-07)

After the folds, the folder re-read this file end to end. **The test for each path:** can any path
on which MailArchive cannot tell, or on which mail is missing, still print "complete", "Export
complete." or "everything"? Each contradiction found, and how it is now resolved:

1. **Tolerance against R42.** As first folded, a `caught_up` account inside the tolerance kept
   "Export complete." while some mail was missing. **Resolved:** "complete" words need missing = 0,
   a count that is not stale, and every account of the run (§4.2 fold; R42 amended).
2. **Several accounts in one run.** The design never said how the run's lead is chosen when
   accounts differ, so one caught-up account could have spoken for an unknown one. **Resolved:**
   the weakest account sets the lead (§4.2 fold, MA-1181).
3. **TC-2's "lists no mail" exception against MA-1178's never-connected profile.** **Resolved:**
   the exception needs every kept folder checked, so `fresh-imap` stays `unknown`.
4. **TC-2's "within int32" against Thunderbird's types.** `flags` is unsigned 32-bit, and
   `expungedBytes` and `folderSize` are 64-bit. **Resolved** in §2.1's fold and §4.1 step 3's fold.
5. **§2.3's stale advice against Panorama.** "Open Thunderbird so it can count again" would be
   useless on a Panorama profile. **Resolved:** Panorama is decided first.
6. **TC-9 against the never-checked rule.** With background download off, nothing is ever checked,
   so the account would have landed on `unknown` and "leave it open". **Resolved:** `limited` is
   decided first.
7. **The 0x0008 exclusion and the TC-16 subtraction together** may count deleted copies out twice.
   **Accepted:** it can only lower X, so it fails toward "behind", until MA-1199 decides.
8. **§3's "at least" against "caught up".** As written, a floor could reach `caught_up`.
   **Resolved:** a floor is `behind` or `unknown`, never `caught_up` (TC-1, O3).
9. **The Gmail floor and Spam/Trash counted twice.** **Resolved** in §3's fold.
10. **§9.2, MA-1186 and R44 against O1.** **Resolved:** all three now say "nothing beside an
    encrypted archive". The §9.3 fixed sentence is withdrawn, and C4 is retired.
11. **§7's card banner and Recent backups tag for encrypted archives.** **Resolved:** on screen
    only, cleared by Lock (§7 fold).
12. **Q2's "one email" against TC-20's "one of each".** **Read as** one of each kind per streak.
    If the owner meant one in total, it is a small change (§5 fold).
13. **§6's not-kept WARN, TC-8's Gmail labels note, and TC-19's WARN-once.** **Resolved:** Gmail
    labels are a note only; other not-kept folders get a WARN once, then a note; behind stays a
    WARN.
14. **A `limited` account holding a streak open for good.** **Resolved:** `limited` ends a streak
    silently (§5 fold, the folder's reading, for the lead to confirm).
15. **TC-11's "a whole `ImapMail/<host>` store" against TC-5's custom store folders.**
    **Resolved:** a whole store is the folder `prefs.js` names, wherever it is (§2.4 fold).
16. **Gmail detection across designs.** This design now uses `is_gmail` only (TC-1). gmail-dedup §6
    still says "`is_gmail` or the All Mail flag", and claims to match this design. **Not resolved
    here; flagged** for gmail-dedup's review (§3 fold).

**Every path to "can't tell" was checked**: §4.4's fold table, the Gmail floor, a partial
`-input`, Exchange, Panorama, staleness and the TC-22 cap. Each ends at "Backup finished." or
weaker, and none prints "complete". A run that fails for another reason writes no completeness
record, and its failure words win (§11, unchanged). A record without the field shows nothing, never
"complete" (MA-1183).

**Still owed:** an independent fold-verify. This re-read was done by the same agent that wrote the
folds.

> **Folded 2026-11-07 [foldcheck M1 to M10] (lead's call): the re-read after the fold check.**
>
> The independent fold check (92b80f8) found the closing claim above ("none prints 'complete'")
> **not yet true** on six paths. That sentence is **superseded by this note**. Two items in the list
> above change:
>
> - **Item 7** ("Accepted: it can only lower X, so it fails toward 'behind'") is **superseded by
>   [foldcheck M7]**: true only of the 0x0008 half; the estimate can undercount, so the "complete"
>   words now also need `expungedBytes` = 0.
> - **Item 16** ("Not resolved here; flagged") is **superseded**: Gmail is told by `is_gmail` only in
>   both designs (lead's call, §3 fold).
>
> **The headline rule, as it now stands:** a run says "Export complete." (or, for an encrypted
> archive's Run now note, "Backup complete:") only when every account may say complete: `caught_up`,
> no "can't tell" folder, missing = 0, not stale, no cap note, and `expungedBytes` = 0 on every kept
> folder; or the account is `not_applicable` as M6 narrows it. Re-read against each of the six paths:
>
> | # | Path (fold check) | Now | Can it print "complete", "Export complete." or "caught up"? |
> |---|---|---|---|
> | 1 | no matched account and no readable cache → `not_applicable` | M6: `unknown` unless a matched account is `pop3`/`none`/`rss`, or there is no profile above the input and no `ImapMail` path part **(superseded 2026-11-08, fold check 3's §4 wording note: read "…and no `ImapMail` path part and no `.msf`", the M6-residual note below)** | **No**, for every case the fold check named (ambiguous account, `prefs.js` missing above an `ImapMail` path or unreadable or refused, `panacea.dat`, a refused cache, a copied `ImapMail/<host>`). One case outside them remains (below) |
> | 2 | an Archive-only label stands in for a hidden All Mail | M5: All Mail is the one folder with 0x8000; none or several → floor | **No**: a floor is `behind` or `unknown` |
> | 3 | deleted copies smaller than average beat the estimate | M7: complete words need `expungedBytes` = 0 | **No**: with any `expungedBytes` the lead is "Backup finished." with both numbers |
> | 4 | `encryptedSummary` always says "Backup complete:" | M8: its lead follows the weakest-account rule | **No** |
> | 5 | a stale `caught_up` says "everything" | M9: dated words without "everything"; lead "Backup finished."; `-json` `stale` true, `complete` false | **No** |
> | 6 | a broken `flags` lets a folder drop out | M3: treated as kept → `unknown` | **No** |
>
> M2's new "can't tell" folder is `behind` or `unknown`, never complete. No user-facing sentence says
> "caught up"; `-json` keeps `verdict: "caught_up"`, now beside `complete: false` wherever the words
> may not say complete.
>
> **What this re-read did not close (for the lead):**
>
> - **An IMAP store outside every profile and every `ImapMail` folder** (an `-input` of an IMAP
>   account's custom local folder that is not inside its profile, or a copy of `ImapMail/<host>`
>   renamed into another folder) has no `prefs.js` above it and no `ImapMail` part, so M6's rule gives
>   `not_applicable` and today's "Export complete.". From the path alone, MailArchive cannot tell it
>   from a plain mail folder. It is not a new regression: today's words are the same. A one-condition
>   narrowing would close it: a store holding any `.msf` file with no profile found → `unknown`. That
>   costs a Local Folders copy outside its profile "Export complete.", which fails safe. **Not
>   folded**: M6 is the lead's call as written. A related point: MA-1196's "an `imap` account with a
>   custom folder outside `ImapMail/` → checked" holds only when that folder is inside the profile,
>   because the profile is found by walking up (§10).
> - **N5, unexplained extra separators** (the owner's +6 in INBOX with `expungedBytes` = 0) can still
>   cover the same number of never-downloaded messages in one non-Gmail folder, so "missing = 0" can
>   be wrong by that excess. It is a low item owned by MA-1190 and the release-notes rule (§2.4 fold),
>   as the fold check placed it, not one of its six paths.
>
> **Still owed:** the fold checker's re-check of M1 to M10 only. This note was written by the agent
> that folded them.
>
> **Folded 2026-11-07 [foldcheck M6 residual] (lead's call):** the first bullet of "What this
> re-read did not close" is **closed by the lead's call**. An IMAP-looking store outside every
> profile and every `ImapMail` folder is `unknown` ("can't tell") when no profile is found and the
> store holds any `.msf` summary file. Only a store with no `.msf` stays `not_applicable` (§2.2,
> §4.2 and §4.4 folds; MA-1196's custom-folder case is reworded in §12's fold). The headline rule
> above then admits no `not_applicable` for any store Thunderbird keeps as IMAP. The N5 bullet is
> unchanged.
>
> **Folded 2026-11-07 [completeness foldcheck2 MF1, L4] (lead's call):** fold check 2 found one
> more escape from the headline rule above: the encrypted Run now note could say "Backup complete:"
> when "the run had no Thunderbird IMAP store", read by path (its §2, paths 1, 4 and R). MF1 closes
> it: that clause now reads "every Thunderbird store of the run is `not_applicable`", decided by the
> accounts' verdicts (§7 fold). The headline rule also gains L4: an account with a not-kept WARN may
> not say complete (§4.2 fold). Re-read against fold check 2's §2 table, every path, R included,
> now gives "Backup finished." or weaker on every surface. **Still owed:** the independent narrow
> check of L6 (§13 fold); this note was written by the agent that folded MF1.
