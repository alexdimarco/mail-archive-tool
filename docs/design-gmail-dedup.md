# Design — Gmail labels: store each message once, and show it in every folder it is in

**Revision:** 2 (2026-11-07). Revision 1 (commit 4e33bc2) covered only duplicates. Revision 2
answers the parallel completeness design's finding (`docs/design-thunderbird-completeness.md`,
branch `tb-completeness`, commit 552fc98, §1 and §3): for some folders Thunderbird keeps no
copy at all, so the archive also loses folder membership. Both problems are measured here, and
the design now covers both.

**BUILD STATUS:** proposed — not built. Faces the 10-lens pre-code review
(`../assurance-kit/process/design-review.md`) before any code: it changes what the manifest
stores for a Gmail account, adds a reader for Thunderbird's summary files, and adds a step that
removes archived files. Branch `gmail-dedup`, off `main` 0923ffa (v1.6.1). The owner asked for
it to be built after design and review ("do 3").

**Ids are provisional.** The completeness design takes MA-1170 to MA-1194, R42 to R44 and S75
to S77; another agent takes MA-1151 and up. This design takes **MA-1210 to MA-1234**, **R45**
and **S78 to S84**. Whoever merges second renumbers.

Words used here, in plain terms:

- **copy**: one message as Thunderbird keeps it in one folder's mbox file (the file that holds
  that folder's mail).
- **summary file**: the folder's `.msf` file, Thunderbird's list of every message in the
  folder, including those whose body it did not keep in that folder's mbox.
- **record**: one entry in the archive's manifest (`.mailarchive-manifest.json`).
- **membership**: "this message is in folder F", with when it was first and last seen there.
  A **copy membership** comes from a copy read in F. A **listed membership** comes from F's
  summary file when F's mbox has no copy.
- **home folder**: the one folder whose directory in the archive holds the message's files.
- **Thunderbird's bookkeeping lines**: the header lines Thunderbird writes into each copy:
  `X-Mozilla-Status`, `X-Mozilla-Status2`, `X-Mozilla-Keys` (read, flagged, deleted, tag state).
  They are not part of the email anyone sent.
- **normalized bytes**: a copy's raw bytes with exactly those lines taken out.

## 1. The two problems, measured

Gmail stores each message once and shows it under every label. Over IMAP each label is a
folder. Thunderbird syncs folders one by one, and **sometimes** keeps a second copy of a body
it already has in another folder, and **sometimes** does not (§2.1). On the owner's Mac both
happen. Measured 2026-11-07, read-only, counts and sizes only (method in §10).

| Folder | Thunderbird lists | Copies in its mbox | Records in the archive |
|---|---:|---:|---:|
| `[Gmail]/All Mail` | 238,169 | 238,201 | 233,623 |
| `[Gmail]/Important` | 56,625 | 56,585 | 56,575 |
| `[Gmail]/Sent Mail` | 33,822 | **2** | **2** |
| `INBOX` | 10,881 | 10,887 | 10,883 |
| `[Gmail]/Starred` | 6,804 | 6,803 | 6,800 |
| `SAS` | 2,241 | 2,233 | 2,233 |
| `Calendar` | 1,194 | 1,194 | 1,194 |
| `Promotions` | 772 | **no file** | **0** |
| `[Gmail]/Drafts` | 687 | 686 | 686 |
| `TO BE SORTED` | 286 | 286 | 286 |
| `Daily Report` | 191 | **10** | **10** |
| `CAD GOV` | 146 | 145 | 143 |
| `Tax Return` | 127 | 127 | 127 |
| `[Gmail]/Spam` | 126 | not kept offline | 0 |
| `Business Invoice` | 120 | 120 | 120 |
| `Receipts` | 48 | 48 | 48 |
| `Business Papers - Corporation` | 33 | **no file** | **0** |
| `ACTIVE-WORKING` | 30 | 30 | 31 |

(The archive is the owner's `Mail-archive-dimarcotech`, made with `-raw`. Its last backup was
running while it was counted, so its figures trail the mbox files slightly.)

**Problem A — duplicates.** The archive holds **312,761 records for 233,322 distinct
messages**. 79,200 records are extra copies: 60,503 messages are stored twice, 8,319 three
times, 646 four times, 29 five times, 1 six times. For 73,915 of those extra records the kept
original is byte-identical to another folder's. They take **34.5 GB of the archive's 90.9 GB**
of message files (38%). Every search shows such a message up to six times.

**Problem B — missing folder membership.** For four folders Thunderbird kept (almost) no copy,
so the archive does not show their messages there: Sent Mail (33,820 of 33,822 missing),
Promotions (772), Daily Report (181 of 191) and Business Papers - Corporation (33). That is
**about 34,800 folder memberships missing**. No mail is lost: all of those messages are
archived, from All Mail. But a person who opens Sent Mail in the archive sees 2 messages.

**Which is bigger for the owner.** By size and by daily nuisance, **Problem A**: 79,200 extra
copies, 34.5 GB, and repeated search hits on every search. By trust, **Problem B**: an almost
empty Sent folder looks like lost mail, though nothing is lost. The same change fixes both: one
record per message with a list of every folder it is in (§3). Problem A is fixed from what the
archive already reads; Problem B needs one new input, Thunderbird's summary files (§5).
Recommended order: A first, B straight after (§12).

**Goal.** In an archive of a Gmail account, each email is stored once; every folder it is in
is recorded and shown (in the reader, as search filters, in the folder tree, on the static
folder pages); restore and extract put it back in every folder; nothing the archive promises
today is lost.

## 2. What happens today

### 2.1 Thunderbird: when a second copy is kept

- Thunderbird stores Gmail's `X-GM-MSGID`, `X-GM-THRID` and `X-GM-LABELS` on the message's row in
  the folder's summary file, not in the message bytes (`mailnews/imap/src/nsImapMailFolder.cpp`,
  `NormalEndHeaderParseStream`: `newMsgHdr->SetStringProperty("X-GM-MSGID", msgIDValue);`).
- Its background download skips a message that `HasMsgOffline` reports as already kept
  (`mailnews/imap/src/nsAutoSyncState.cpp`, `GetNextGroupOfMessages` and
  `ProcessExistingHeaders`). For an IMAP folder `HasMsgOffline` is true when
  `GetOfflineMsgFolder` finds a folder holding the body. For a Gmail account that search also
  looks in OTHER folders, but only in the folders named by the message's own `X-GM-LABELS`
  (special labels such as `\Sent` and `\Inbox` map to those folders by their flags), and only
  where that folder's summary has a row with the same `X-GM-MSGID` flagged as kept offline. (All
  read on the `mozilla/releases-comm-central` mirror on GitHub, 2026-11-07; line numbers not
  recorded. The completeness design cites `nsImapMailFolder.cpp:8906-9004` and
  `nsAutoSyncState.cpp:254-259`, `:335-339` at comm-central ab60cc5e.)
- So whether a folder gets its own copy depends on which folders were synced first and on each
  message's label list. The rule is not "one copy per message": on the owner's Mac, Important,
  INBOX, Starred and most labels have full copies of bodies that All Mail also holds, while
  Sent Mail, Promotions, Daily Report and Business Papers do not. **[unverified: the exact order
  of events that produced this split; the design does not depend on it — it handles both a
  folder with copies and a folder without.]**

### 2.2 MailArchive

- **The key is folder-scoped for every local source.** The exporter builds
  `state.Key(store, folderKey, m.Identity())` unless `DedupMailboxWide` is set
  (`internal/export/exporter.go:219-225`); the key is `token\x00folder\x00identity`
  (`internal/state/manifest.go:413-420`). The local run never sets the flag
  (`internal/app/run.go:795-804`); only Microsoft 365 does (`internal/app/graph.go:251`). So a
  Thunderbird Gmail account is a "one-shot local import" under R3: "the same mail filed in two
  folders exports to both" (`docs/scenario-catalog.md:24-33`; scenario S4 at `:435`). The local
  run also builds that key itself once (`internal/app/localrun.go:415`).
- **The archive reads only mbox files.** It never reads a summary file; one is only checked for
  existence (`internal/source/source.go:77`, `:151`, per the completeness design §1). A folder
  with no mbox copy of a message therefore never shows it: Problem B.
- **Store and folder names.** A Thunderbird account directory is one store named after the
  directory, normally `imap.gmail.com` (`internal/source/mboxreader.go:21`; auto-discovery at
  `run.go:1881-1901`; token at `run.go:1674`). All Mail's folder path is `["[Gmail]", "All Mail"]`
  (`internal/source/localwalk.go:506`, `:547`). The run never reads the account's host from
  `prefs.js`; `thunderbird.FindAccountForStore` can (`internal/thunderbird/thunderbird.go:81`), but
  only the wizard calls it (`internal/wizard/wizard.go:1554-1575`).
- **Identity** is `mid:<Message-ID>`, or `sha:<content hash>` without one
  (`internal/model/message.go:318-327`). A different message reusing a Message-ID in the same folder
  is told apart by the envelope `Fingerprint` (`message.go:340-356`) and filed under a qualified key
  (`exporter.go:298-311`; `manifest.go:437-448`).
- **A store-wide identity index exists**: `byIdent` (`manifest.go:383-388`; `KeysForIdentity` at
  `:1721-1736`; the per-store count at `:1645-1659`). This design reuses it.
- **Microsoft 365 already stores a message once per mailbox** (`LiveKey`, `manifest.go:422-435`),
  with ONE current folder (`:150-165`): a move model, not membership of several folders. No labels
  there. Out of scope and unchanged.
- **No per-folder history for local archives today.** "A one-shot local import keeps no go-back
  timeline … so there is no history writer to sync here" (`run.go:927-929`); `Present`/`LastSeen`
  are "kept only for mailbox-wide archives (Microsoft 365)" (`docs/design-restore-thunderbird.md`
  §6.4, note RT-9). So there is no go-back or deletion detection to lose for a Gmail-through-
  Thunderbird archive. This design adds per-folder first-seen and last-seen data, which is new.
- **Restore already meets the Gmail case.** It restores each record into its `Folder` and warns
  when All Mail is chosen with other folders (`design-restore-thunderbird.md` §6.5;
  `internal/tbengine/plan.go:664-688`; words at `internal/tbengine/words.go:73`). Lab row MA-1093
  includes "a Gmail two-label message".
- **The page shows that copy's state**: "Unread" and "Categories" come from the copy's own
  bookkeeping lines (`internal/source/mbox.go:132-142`; `internal/export/html.go:2099-2107`,
  `:2237-2238`).
- **Copies marked deleted are archived.** The reader reads only the read bit of
  `X-Mozilla-Status` (`mbox.go:280-293`), so a copy with Thunderbird's deleted bit is archived.
  The owner's files hold 2,709 such copies. Unchanged here; the completeness design leaves them out
  of its count. Noted as Q8.

## 3. Properties (what this design makes true)

- **G1 — One stored copy per Gmail message.** In a Gmail store (§6), copies of the same message in
  different folders are one record with one set of files.
- **G2 — Every folder is recorded.** The record lists each folder the message is in, with first-
  and last-seen times. A copy membership also keeps that copy's bookkeeping lines and checksum.
- **G3 — A merge loses no byte (R45, provisional).** A copy is merged into an existing record only
  when its normalized bytes have the same SHA-256 as the record's, and its exact bytes then rebuild
  from its normalized bytes plus its own bookkeeping lines (checked by SHA-256). Otherwise it is
  archived as its own record, as today. Even a merge of two "different" emails loses nothing: they
  differ only in Thunderbird's lines, which are kept per folder (§4.4).
- **G4 — Folders Thunderbird kept no copy for are recovered.** A message listed in folder F's
  summary file and archived from another folder gains a listed membership F (§5). A wrong listed
  membership can only show a message in a folder it is not in; it never changes or removes a stored
  byte.
- **G5 — Reachable from every folder.** The reader, search filters and facets, the folder tree and
  the static folder pages list the message under every membership, pointing at its one set of files.
- **G6 — Fidelity unchanged.** With `-raw` the original is kept once. Extract and restore write the
  message into every member folder: for a copy membership that folder's copy rebuilt byte for byte
  and checked against its recorded SHA-256; for a listed membership the stored original, said so in
  the plan (there was never a copy of that folder to reproduce). A mismatch is skipped and listed, as
  R20 does today.
- **G7 — Older versions refuse; they never damage.** An archive with memberships is manifest
  version 7, which v1.6.1 and earlier refuse by name (§7.4).
- **G8 — Nothing already archived is deleted without a plan and a yes**, and only when the archive's
  own originals prove the file is a copy (§9).

## 4. Problem A: knowing two copies are the same email

### 4.1 The candidates

| Signal | Verdict |
|---|---|
| Message-ID header | **Lookup only.** In the owner's All Mail, 4,846 Message-IDs appear more than once, and in 253 of them the bodies differ (different emails). It can also be missing. Never enough to merge. |
| Gmail's `X-GM-MSGID` | **Not used to merge** (§4.2). Used only to place listed memberships when the Message-ID is ambiguous (§5.3). |
| SHA-256 of a copy's raw bytes | **Not enough alone**: Thunderbird's lines can differ per folder (§4.3). |
| SHA-256 of the normalized bytes | **The merge test**, with the rebuild check (§4.4). |
| `ContentDigest` (`message.go:376-406`) | **Not enough**: covers no attachment bytes and few headers. |

### 4.2 Why `X-GM-MSGID` is not the merge test

- It lives only in summary files (§2.1); a summary can be missing, stale or rebuilt, and
  MailArchive's own restore writes none (`design-restore-thunderbird.md` §3.4).
- A merge that rests on it would rest on a file Thunderbird rewrites while running, and on one
  vendor's private feature; the owner ruled identity must not (2026-09-12,
  `docs/note-goback-identity-options.md`, "Vendor-robust").
- It is not needed for Problem A: on the owner's data the byte rule matches 79,153 of 79,156 extra
  mbox copies (§10.1).
- Problem B is different: there is no copy to compare, so the summary file is the only evidence,
  and a mistake there costs a wrong folder label, not a byte (§5).

### 4.3 Thunderbird's bytes per folder

Each copy has its own bookkeeping lines, updated in place when that copy is read, flagged, tagged
or deleted. The archive keeps them in the raw bytes (`mbox.go:132-142`;
`internal/source/rawstub.go:51-61`). On 2026-11-07 every extra mbox copy that matched All Mail was
identical even including those lines; the design does not rely on that.

### 4.4 The rule

For a copy read from folder F of a Gmail store, with raw bytes `B`:

1. **Split.** `T` is every line of the top header block (before the first empty line) whose name is
   exactly `X-Mozilla-Status`, `X-Mozilla-Status2` or `X-Mozilla-Keys` (any letter case), with its
   continuation lines, each kept verbatim with its position among the header lines. `N` is `B` with
   exactly those lines removed. Nothing after the first empty line is looked at. Bounded: at most 8
   such lines and 1 KiB, and the empty line within the existing 64 KiB header bound; otherwise never
   merged.
2. **Look up.** Records of the same store token with the same identity (`KeysForIdentity`, filtered by
   token) that carry a `NormSHA256`.
3. **Compare.** A candidate matches when its `NormSHA256` equals `SHA-256(N)`.
4. **Prove.** Put `T` back into `N` at its positions and check the SHA-256 equals `SHA-256(B)`. Since
   the record's normalized bytes equal `N`, every later rebuild of F's copy gives `B`.
5. **Merge or not.** Match: add or refresh copy membership F (`T`, `SHA-256(B)`), write no file. No
   match: archive as its own record, as today, storing its `NormSHA256`.

Never merged: a copy with a read gap (the KEEP guard runs first, `exporter.go:227-233`), an unparsed
stub, a copy with no raw bytes, a split out of bounds.

### 4.5 What a wrong answer costs

| Case | When | Cost |
|---|---|---|
| False merge | Only when every byte but Thunderbird's three lines is identical. | Nothing lost: each folder's copy rebuilds (G3). One search hit where Thunderbird showed two identical emails. |
| Missed match | The copies differ in more than those lines (partial or damaged copy, re-delivery with new `Received` lines). | A second record, as today; `status` counts them. On the owner's archive at most 5,285 of 79,200 extra records (§10.2). |
| Different email reusing a Message-ID | Normalized digests differ. | Kept apart, as today (R1). |

## 5. Problem B: recovering the folders Thunderbird kept no copy for

### 5.1 The source

Each folder's summary file lists every message Thunderbird knows is in that folder, with its
Message-ID and `X-GM-MSGID`, whether or not the body is kept in that folder. On the owner's profile,
Sent Mail's summary has about 33,824 `X-GM-MSGID` cells against Thunderbird's count of 33,822; INBOX,
Important, Starred and All Mail likewise (§10.1). Promotions, Daily Report and Business Papers have
summary files and no (or a near-empty) mbox.

The completeness design rejected summary files for counting (it reads `folderCache.json`, which has
counts but no per-message rows: completeness design §2). For membership, per-message rows are
needed, so the summary file is the only source on disk.

### 5.2 The reader

- A new leaf package reads Thunderbird's summary format (Mork, a text format of tables, rows and
  value dictionaries). It extracts, per row, only the Message-ID, `X-GM-MSGID` and the row's flags
  (to skip rows Thunderbird marks deleted). Nothing else is kept.
- Hard limits, like every source reader: the file is opened through the existing contained reader
  (no links, no special files, R25); size cap (for example 1 GiB; the owner's largest is 215 MB),
  row cap, value-length cap, dictionary-size cap, and a time cap. A file that breaks any limit or does
  not parse adds nothing and is reported once ("Could not read Thunderbird's list for Sent Mail; its
  folder in the archive shows only the copies on disk").
- Thunderbird writes the summary while it runs. A torn last transaction is ignored; Mork's grouped
  writes make that detectable. Reading it changes nothing (R9).

### 5.3 The matching rule

For each row of folder F's summary in a Gmail store, where F's mbox gave no copy of it this run:

1. Find records of the same store with identity `mid:<Message-ID>` (Message-ID spelled the way the
   archive's identity spells it; the summary stores it without angle brackets, which the reader
   normalises; MA-1225 tests both spellings).
2. **Exactly one record** (after Problem A's merging): add listed membership F.
3. **More than one** (a Message-ID reused by different emails): use `X-GM-MSGID`. The message's row in
   All Mail's summary (or any folder whose copy produced a record) carries the same `X-GM-MSGID` and
   that copy's position in its mbox. The capture records, for each copy membership, the copy's
   position in its mbox file (`Offset`), so the `X-GM-MSGID` names exactly one copy and therefore one
   record. If that chain breaks anywhere, add nothing and count it.
4. **None**: the message's body is nowhere on disk. That is a completeness gap, not membership; it is
   left to the completeness design and only counted here.

A listed membership never creates a record, writes a file, or changes a record's bytes. When F's
mbox later gains a copy, it turns into a copy membership through §4.4.

### 5.4 What a wrong answer costs

| Case | Cost |
|---|---|
| Wrong listed membership (a reused Message-ID placed on the wrong email) | That email shows in one extra folder. Bounded by step 3: needs a broken `X-GM-MSGID` chain AND a reused Message-ID. |
| Missed listed membership | The message is missing from that folder in the archive, as today. Counted: "N messages Thunderbird lists in Sent Mail could not be placed." |
| Hostile summary file | Contained, capped, fail-closed (§5.2). The worst outcome is wrong folder labels on archived mail, never a write outside the archive or a changed byte. |

## 6. Scope: which accounts

**Recommendation: Gmail accounts read through Thunderbird only, in version 1.**

- **Detection**, the same rule as the completeness design §3 so both features agree: the profile's
  `prefs.js` sets `mail.server.serverN.is_gmail` to true for that account (the owner's:
  `mail.server.server4.is_gmail`, host `imap.gmail.com`), or a folder of the account carries
  Thunderbird's All Mail flag. Folder names are never used (Gmail localises them). Read through the
  existing contained reader (R25), with `thunderbird.FindAccountForStore`.
- **Sticky per store**, recorded in the manifest (`DedupStores`, §7.1) on the store's first run with
  the feature on; never flips by itself. A store already archived per folder changes only through
  the merge step (§9).
- **Wrong detection is harmless for Problem A** (only byte-identical copies merge) and bounded for
  Problem B (§5.4).
- **Why not every account.** Elsewhere two copies in two folders are two real server copies, there
  are few of them, and the person may think of them as separate. Widening is one detection change
  plus a review (Q2).
- **Out of scope:** Microsoft 365 (no labels; already once per mailbox), Outlook PST/OST (no raw
  bytes), Evolution's cache of a Gmail account (a follow-up), a future live IMAP source (could read
  `X-GM-LABELS` from the server directly; its own design).

## 7. Storage

### 7.1 Manifest schema (version 7)

The key format does not change. A merged message keeps its home copy's key,
`Key(token, homeFolder, identity)` or its qualified form. New fields, all `omitempty`, so a
manifest with no Gmail store is byte-identical to today's (the MA-329 golden):

```go
// Record
NormSHA256  string       `json:"norm_sha256,omitempty"`  // SHA-256 of the normalized bytes (§4.4)
Folders     []Membership `json:"folders,omitempty"`      // every folder, the home one included
MergedFiles []string     `json:"merged_files,omitempty"` // the merge step's files awaiting deletion (§9)

type Membership struct {
    Folder    string    `json:"folder"`          // spelled as Record.Folder is
    Kind      string    `json:"kind"`            // "copy" (read from F's mbox) or "listed" (F's summary file)
    FirstSeen time.Time `json:"first_seen"`
    LastSeen  time.Time `json:"last_seen"`
    Gone      bool      `json:"gone,omitempty"`  // no longer in F (§7.2)
    Moz       string    `json:"moz,omitempty"`   // copy only: this copy's bookkeeping lines and positions
    CopySHA   string    `json:"copy_sha256,omitempty"` // copy only: SHA-256 of this copy's raw bytes
    Offset    int64     `json:"offset,omitempty"`      // copy only: the copy's position in F's mbox (§5.3)
}

// Manifest
DedupStores map[string]string `json:"dedup_stores,omitempty"` // token -> "gmail-1"
```

- `Record.Folder` keeps meaning the home folder, so any reader not yet changed sees one valid folder.
- At load an **alias index** `(token, folder, identity) -> home key` is built over copy memberships;
  the exporter consults it after a plain key miss, so a re-run finds a merged copy without hashing.
- Load validates every membership (clean relative folder path, `Moz` within bounds, digests well
  formed, `Kind` known); a bad one is dropped and logged, never the record.

### 7.2 Per-folder history

`FirstSeen`, `LastSeen` and `Gone` are the per-folder history, written in place; no history log is
added for local archives in version 1 (the Microsoft 365 log, `internal/state/history.go`, is
unchanged). A copy membership is gone when a complete walk of F did not see it; a listed membership
when F's summary was read whole and no longer lists it. The same guards as `SweepGone`
(`manifest.go:1696-1719`): never at a checkpoint, never for a folder not walked or a summary not read,
nothing when the walk did not complete. The record and files always stay. The reader can say "In
Receipts since 1 Mar 2024" or "Not in Receipts since 10 Nov 2026". A date slider is a later design (Q5).

### 7.3 Size of the new data

About 250 bytes per extra copy membership and about 120 per listed one. For the owner: about
79,200 copy memberships (20 MB) and 34,800 listed ones (4 MB), replacing 79,200 records of roughly
0.6 to 1 KB each. The manifest gets smaller.

### 7.4 Version and older MailArchive versions

- `manifestVersion` becomes 7. Load refuses a stored version it does not understand
  (`manifest.go:779-786`), so v1.6.1 and earlier refuse a version-7 archive with `NewerFormatError`
  and change nothing. Without the bump an older binary would drop the unknown `folders` field on its
  next save and silently lose every membership.
- **Lead's call, for the review to attack:** write 7 only when the manifest holds a `DedupStores`
  entry or a record with `Folders`; otherwise keep 6, so an archive with no Gmail store stays usable
  by v1.6.1. Today `save` always stamps the current constant (`manifest.go:2148-2160`).
- The search index (`internal/index/index.go:27-34`, `indexVersion = 3`, newer refused at
  `:162-164`) gains one table (§8.2); same rule, 4 only when the table has rows.
- An encrypted archive's sealed manifest carries the same fields inside the sealed record
  (`internal/container/record.go:61-64`; `manifest.go:744-768`); the same version rule applies.

## 8. Every reader and writer of the manifest

"Members" means the record's `Folders`.

### 8.1 Capture

- `Export` (`exporter.go:207-311`) gains a Gmail-store branch after the plain key lookup: (1) key hit
  in this folder: today's path; (2) alias hit: the same message when its `Fingerprint` matches
  (today's skip test, `exporter.go:298`), so stamp `LastSeen` and skip, with no hashing on a steady
  re-run (R2); a mismatch is a different email and goes on as today; (3) miss: §4.4, merge or write a
  new record with its home membership.
- `localrun.go:415` uses the same lookup. The capture-gap ledger's per-folder locator
  (`localrun.go:244-254`) is unchanged.
- After the mbox walk of a Gmail store, the summary pass (§5) runs for every folder whose summary is
  readable, adding and refreshing listed memberships. It writes no message file.
- A `full` run rewrites the home record's files once and never writes a second set.
- `status` and the run summary say: "Gmail: 233,322 messages. 79,200 extra folder copies recorded
  without storing them again. 34,800 folder entries taken from Thunderbird's folder lists."

### 8.2 Search index

- Today `docs(key UNIQUE, store, folder, …)`, one folder per row (`internal/index/index.go:41-63`);
  `Add` replaces an earlier row of the key (`index.go:431-458`).
- Change: a table `doc_folders(key, store, folder)`, indexed on `(store, folder)`, one row per member.
  Filters (`internal/index/query.go:141-142`), facets (`query.go:303`) and folder listing
  (`internal/index/listfolder.go:227-262`) match either `docs.folder` or a `doc_folders` row. One
  message is one search hit.
- Rebuilds take folders from the manifest's members, not from `Path`: `Rebuild`
  (`internal/app/reindex.go:396-399`), `catchUpIndex` (`internal/app/sealedindex.go:263`),
  `indexCopied` (`internal/app/decryptcopy.go:457`).

### 8.3 Desktop reader, folder tree and search page

- The tree puts each key in one node from `rec.Folder` (`internal/app/copyexport_archive.go:489`,
  `:530-542`); lists come from the index (`internal/app/ws_archive.go:448-499`). Change: a Gmail
  record's key goes in every member's node. Folder counts are "messages in this folder"; the archive
  total counts records ("233,322 messages in 17 folders"), never a sum of folder counts.
- `resolveRecord` (`copyexport_archive.go:855-862`) uses the folder the person opened the message from.
- The reader shows under the header: "In: All Mail, INBOX, Sent Mail, Important", and "(no longer in
  Receipts since 10 Nov 2026)" for a gone member.
- Search (`internal/server/page.go:57`, `:123`, `:135`; `internal/server/server.go:61`, `:79-86`) gets
  folder filters from the facets; a result lists its folders.
- The Microsoft 365 go-back views (`internal/server/goback.go:137-180`) are untouched.

### 8.4 Static pages

- Folder pages group by the file's directory (`internal/pages/pages.go:122`, `:144-146`, from
  `EachDoc` ordered by path, `internal/index/pagequery.go:19`).
- Change: each folder page also lists records that are members of it but live elsewhere, linking to
  the home file by a relative path inside the same store directory, built from the validated `Path`
  (R4). No script (R19). The go-back review rejected cross-directory links for Microsoft 365 moves
  (`docs/review-goback-predesign.md`, F5/GB-6) because folders there change every run; Gmail membership
  is mostly add-only, which the review should test.
- The message page stays frozen at capture: its "This folder" link goes to the home folder
  (`html.go:2176-2185`), its Unread and Categories lines are the home copy's. The full folder list is
  on the folder pages and in the reader.
- Encrypted archives render pages in memory with the same code (`internal/keyring/reader.go:560`).

### 8.5 Extract (mbox and eml)

- Today one output folder per record's `Path` directory; `Record.Folder` unused
  (`internal/app/extract.go:129`, `:273-287`, `:413`, `:452`).
- Change: a record is written into every member's folder: a copy member's rebuilt copy, checked
  against `CopySHA` (a mismatch is skipped and listed, R20); a listed member gets the stored original.
  Without `-raw` there is nothing to extract, as today. `-format eml` writes one file per member folder.

### 8.6 Restore to Thunderbird

- Today `SelectForRestore` keeps a key in its first node only, so `RestoreFolder.Keys` are disjoint
  (`internal/app/restoreread.go:221-231`, `:260`, `:315`); the plan copies them
  (`tbengine/plan.go:336-339`); the run reads each original (`tbengine/run.go:216-245`) and appends it
  (`internal/tbrestore/write.go:387-408`).
- Change: a key goes in each chosen member folder; each gets its rebuilt copy (copy member) or the
  stored original (listed member). Counts and size are per folder written. R40 holds. The All Mail
  note stays (`plan.go:603`, `words.go:73`). A restored Sent Mail now holds the sent mail.

### 8.7 verify

- Today `owned` comes from each `Path` stem; other archived-type files are `unexpected`
  (`internal/app/verify.go:194`, `:316-323`, `:436`, `:615`).
- Change: `MergedFiles` are owned while they wait. With an original present, verify rebuilds each
  copy member and compares it with `CopySHA`; a mismatch is `modified` and names the folder.
- Side note: `AlsoFiles` (Microsoft 365 collapse losers) are not owned today and show as
  `unexpected` (`manifest.go:399-404`).

### 8.8 Delete and reindex redaction

- Redaction deletes `Path`'s files and every `AlsoFiles` entry (`reindex.go:158`, `:176-178`); change:
  also `MergedFiles`. Deleting a Gmail message's files removes it from every folder (it is one email);
  the reader and docs say so.
- Archive delete lists `Path` plus `AlsoFiles` (`internal/archdelete/lister.go:205`, `:882`) and folders
  from `rec.Folder` (`:833`); change: add `MergedFiles` and the members' folders.

### 8.9 Encrypted archives

The whole record is sealed (`container/record.go:61-64`); files are opaque units with no folder tree
(`internal/export/units.go:23-26`, `:64-73`; `exporter.go:410-433`). Members are inside the sealed
record. A merge or a listed membership writes no unit; the merge step's deletions use the deferred
unit deletion (`exporter.go:155-160`). The summary files are read from the Thunderbird profile, not
the archive, so nothing new is written in plain text.

### 8.10 Microsoft 365

Unchanged. A Graph token is never in `DedupStores`; Load drops `Folders` on a Graph record. Side note
for the review: `CollapseByIdentity`'s caller writes no history event for collapse losers
(`graph.go:341-401`), although `CollapseLoss`'s comment says it does (`manifest.go:399-404`).

## 9. Existing archives: the merge step

Upgrading changes nothing in an existing archive. Its captures stay per folder until the person merges.

1. **Evidence, at the next backup.** For records of a detected Gmail store without `NormSHA256`, the
   backup stamps `NormSHA256`, `CopySHA`, `Offset` and the lines from the copy it reads anyway.
   Metadata only.
2. **Listed memberships need no merge step.** They add folders and delete nothing, so they start at the
   first backup after the upgrade, for old and new archives alike (Q4). The owner's Sent Mail fills in
   then.
3. **The offer.** The archive's card says once: "This archive keeps 79,200 extra copies of Gmail
   messages (about 34.5 GB). Merge them?" with Review and Not now. CLI:
   `mailarchive merge-copies -out DIR -plan`.
4. **The plan** (writes nothing) groups records with the same token, identity and `NormSHA256`, and shows
   per folder how many merge and how much space is freed. A file is offered for deletion only when the
   archive proves it is a copy: survivor and copy both have a kept original with equal normalized
   SHA-256, both pass their recorded fixity, and the survivor is at least as complete (no gap label the
   copy lacks). Groups without that proof are merged in the manifest only, their files kept and counted.
   The survivor is the most complete, then earliest captured, then lowest key, so its files never move.
5. **The yes**, holding the archive lock (a scheduled run meanwhile refuses, as for verify):
   1. write a marker `.mailarchive-merge.json` listing the groups, fsynced;
   2. index: drop the copies' rows, add `doc_folders` rows, one transaction, made durable;
   3. manifest: the survivor gains members from each copy, the copies' records go, their paths go into
      `MergedFiles`, the store goes into `DedupStores`; saved (the anchor);
   4. folder pages regenerate;
   5. each `MergedFiles` file is deleted only if its bytes still match its recorded fixity (else kept and
      reported); then `MergedFiles` is cleared and saved;
   6. the marker is removed.
   A crash at any step leaves the marker; the step (or a backup, which refuses until it is done)
   finishes forward from the saved manifest. A file is deleted only after no saved record or index row
   names it (R28).
6. **Encrypted archives**: the same steps on the sealed manifest, units deleted through §8.9.

This step deletes archived files: it owes the adversarial pass, the friction review and the owner's
sign-off on its words (R36's "only by the person's explicit decision", in spirit).

## 10. Evidence

### 10.1 The owner's Thunderbird profile (read-only, 2026-11-07)

Scripts kept outside the repository (`mail-archive-tool/.claude/gmail-dedup-probe/`, not committed).
They opened files read-only, hashed Message-IDs in memory only to pair copies, and printed counts, sizes,
flag words, folder names and header names only. No subject, address, body or Message-ID was printed.

- Listed counts are from `folderCache.json` (`totalMsgs`); copies are message separators in each mbox.
  Table in §1.
- 79,153 of the 79,156 mbox copies outside All Mail match an All Mail copy byte for byte (Thunderbird's
  lines included). The other 3: 2 INBOX copies newer than All Mail's file, 1 copy with no Message-ID.
- All Mail: 4,846 Message-IDs appear more than once: 32 with identical copies, 4,561 with equal bodies
  and different other headers, 253 with different bodies.
- 2,676 All Mail copies and 33 Important copies carry Thunderbird's deleted bit; none its partial bit.
- No mbox copy in a sample of 3,000 per folder carries an `X-GM-MSGID` line. Every summary checked (All
  Mail, INBOX, Important, Sent Mail, Starred) has `X-GM-MSGID`, `X-GM-THRID` and `X-GM-LABELS` columns
  with about as many cells as Message-ID cells (Sent Mail: 33,824).

### 10.2 The owner's archive (read-only, 2026-11-07)

`Mail-archive-dimarcotech`, manifest version 6, made with `-raw` (its job file says so); a backup was
running.

- 312,761 records, 233,322 distinct (store, identity) pairs; 69,498 identities in more than one folder.
- 79,200 extra records: 73,915 whose kept originals are byte-identical to another record's (34.5 GB);
  5,285 in groups where some original differs (this includes the Message-IDs reused by different
  emails, so it is an upper bound on missed matches).
- 90.9 GB of message files in all (pages, zips and originals, from recorded fixity).

### 10.3 Thunderbird's source

`nsImapMailFolder.cpp` (`NormalEndHeaderParseStream`, `HasMsgOffline`, `GetOfflineMsgFolder`) and
`nsAutoSyncState.cpp` (`GetNextGroupOfMessages`, `ProcessExistingHeaders`), as §2.1 describes.

## 11. Disk and speed

- **Records:** 312,761 become 233,322 (25% fewer), plus about 34,800 listed memberships that cost no
  file.
- **Bytes:** 34.5 GB of the owner's 90.9 GB of archived message files are byte-identical copies:
  **about 38% smaller** after the merge step. A new Gmail archive is that much smaller from the start.
- **First backup with the feature:** about 79,000 fewer pages and zips written. Each merge costs one
  SHA-256 of the copy (about 19.5 GB of source hashed once). The summary pass reads the summary files
  (about 285 MB for the owner's account) once per backup.
- **Later backups:** the same mbox reading as today; no hashing for copies already recorded.
- **Search:** one hit per email instead of up to six.

## 12. Tests, catalog rows and lab rows

All provisional (see the top). Every test cites its rows with `// covers:` markers.

**R45 (provisional) — Gmail: one stored copy, every folder shown, no byte lost.** In a Gmail store, a copy
whose normalized bytes match an archived record's becomes a member of that record, merged only after its
exact bytes rebuild and check; a message Thunderbird lists in a folder without a copy there becomes a
listed member, never a new file; every copy member's bytes are reproducible by extract and restore, or
skipped and listed. **R3 reworded:** its local-import sentence gains "except a Gmail store (R45)".

**Scenarios:** S78 one message in four Gmail folders; S79 a Message-ID reused by different emails; S80 a
folder whose bodies Thunderbird keeps elsewhere (Sent Mail); S81 a label removed in Gmail; S82 an older
version opens a version-7 archive; S83 the merge step with a crash mid-way; S84 restore and extract of a
multi-folder message.

| Id | Tier | What it proves |
|---|---|---|
| MA-1210 | U | split/rebuild: rebuild(split(B)) == B for any header block; nothing after the first empty line changes; out-of-bounds refused (property test plus folded, duplicated, LF/CRLF, `X-Mozilla-Status-Fake`) |
| MA-1211 | U | copies differing only in the three lines merge; one byte different elsewhere does not (R1) |
| MA-1212 | U | a Message-ID reused by a different email stays a separate record |
| MA-1213 | U | read-gap copy, unparsed stub, no-raw copy never merged |
| MA-1214 | S | fixture Gmail account, one message in four folders: one record, four members; re-run exports zero, hashes nothing |
| MA-1215 | S | detection via `is_gmail` and the All Mail flag; a non-Gmail account stays per folder; sticky |
| MA-1216 | U | manifest v7 only with a Gmail store; a v6 archive byte-identical (MA-329); v1.6.1's loader refuses v7 |
| MA-1217 | U | load drops a hostile member, keeps the record |
| MA-1218 | U | summary reader: real Mork fixtures from Thunderbird (Windows, macOS); Message-ID and `X-GM-MSGID` per row; deleted rows skipped |
| MA-1219 | U | summary reader limits: oversized file, rows, values, dictionary, deep nesting, torn last transaction, a link or a pipe in place of the file: nothing added, one note, no hang |
| MA-1220 | S | listed membership: Sent Mail's summary lists 3 messages archived from All Mail → three listed members; no file written; ambiguous Message-ID resolved by `X-GM-MSGID` and offset, or counted |
| MA-1221 | S | a listed member becomes a copy member when F's mbox gains the copy |
| MA-1222 | S | index: one hit per message; filter, facets, folder listing find it under every member; rebuild from the manifest equal |
| MA-1223 | S | per-folder gone (copy and listed): only after a complete walk or a whole summary read; cleared when seen again |
| MA-1224 | S | static folder pages list members living elsewhere with working relative links; script-free (R19); inside the store tree (R4) |
| MA-1225 | U | Message-ID spelling: with and without angle brackets match the same identity |
| MA-1226 | S | extract mbox and eml into every member folder; copy members byte-equal to their folder's copy; tampered `CopySHA` skipped and listed (R20) |
| MA-1227 | S | restore writes every chosen member folder; counts match; R40 holds |
| MA-1228 | S | verify reports a copy member whose rebuild mismatches; owns `MergedFiles` |
| MA-1229 | S | merge step: plan writes nothing; only proven copies deleted; fixity-mismatched kept; a crash at each of steps 5.1 to 5.6 finishes forward with nothing lost |
| MA-1230 | S | encrypted archive: MA-1214, MA-1220 and MA-1229 through sealed units |
| MA-1231 | S | redaction and archive delete reach `MergedFiles` and every member's folder |
| MA-1232 | L (pending) | Windows VM: a throwaway Gmail account in Thunderbird, one message under three labels plus sent mail; archived, restored, extracted; each folder shows in Thunderbird; per-folder `.eml` SHA-256 equal for copy members |
| MA-1233 | L (pending) | macOS: the owner's profile read-only: counts of §10.1 against a real archive run into a scratch folder; Sent Mail shows about 33,800 messages (counts only) |
| MA-1234 | L (pending) | Linux: MA-1232's fixture through Docker (no GUI; archive, extract, verify) |

Lab story: the Windows row needs a Gmail account the owner creates for testing (MailArchive's agents
never sign in to a real account), synced by Thunderbird on the Parallels VM. Windows first (the owner's
2026-11-01 rule). The owner's Mac is used read-only, for counts. Real Mork fixtures come from that
throwaway account's Thunderbird, never from the owner's profile.

## 13. Build slices and gates

| Slice | What | Gate before the next |
|---|---|---|
| GD0 | This design; catalog rows registered | **10-lens pre-code review** (`docs/review-gmail-dedup-predesign.md`); owner answers to §14 |
| GD1 | Split/rebuild core, own leaf package, no callers (MA-1210, MA-1211) | **Adversarial pass** (the false-merge guard) and a mutation sweep |
| GD2 | Manifest v7 fields, alias index, conditional version, load validation, sealed record (MA-1216, MA-1217) | unfiltered `go test ./...` |
| GD3 | Detection, capture merge, per-folder seen/gone, `status` line (MA-1212 to MA-1215, MA-1223) | **Adversarial pass** (R1-critical); Windows run on the VM |
| GD4 | Index table, reader tree, search, static pages (MA-1222, MA-1224) | **Friction review** (Windows first, then Mac); UX contract walk |
| GD5 | Summary-file reader, own leaf package (MA-1218, MA-1219, MA-1225) | **Adversarial pass** (hostile summary files; new parser) and fuzzing |
| GD6 | Listed memberships wired into the run (MA-1220, MA-1221) | unfiltered suite; Windows run |
| GD7 | Extract, restore, verify per member (MA-1226 to MA-1228, MA-1231) | **Adversarial pass** on restore (R40) and extract (R20) |
| GD8 | Merge step for existing archives (MA-1229, MA-1230) | **Adversarial pass**, **friction review**, owner sign-off on the words |
| GD9 | Lab rows (MA-1232 to MA-1234), README, help page, release notes | owner's release call |

Every fix ships prove-fail then prove-pass, recorded in its commit. GD1 to GD4 alone fix Problem A for new
archives; GD5 and GD6 fix Problem B for every archive; GD8 frees the space in existing ones. The detection
rule is shared with the completeness design; whichever lands first owns it and the other reuses it.

## 14. Owner questions, each with a recommendation

- **Q1 — Keep `Mail-archive-dimarcotech` or start a new Gmail archive?** Recommended: keep it and use the
  merge step. It already holds 233,322 messages, and the step frees about 34.5 GB.
- **Q2 — Gmail only, or every Thunderbird account?** Recommended: Gmail only for now; other accounts have
  few copies.
- **Q3 — Delete a copy's files only when both copies have kept originals that prove it?** Recommended:
  yes. Your archive was made with `-raw`, so this covers about 73,900 of the 79,200 extra copies.
- **Q4 — Fill in Sent Mail and the other near-empty folders from Thunderbird's folder lists, from the
  next backup, without asking?** Recommended: yes. It only adds folder entries and never deletes.
- **Q5 — Show "no longer in this folder since DATE"?** Recommended: yes, in the reader; a date slider
  later, as its own design.
- **Q6 — Merge copies by default in new Gmail archives?** Recommended: yes, with one line in setup and
  Settings: "Gmail: each email is stored once and shown in all of its folders."
- **Q7 — Restore: keep All Mail ticked with the note?** Recommended: yes, unchanged.
- **Q8 — Copies Thunderbird marked deleted (2,709) are archived today.** Recommended: leave as is here;
  decide with the completeness design, which already leaves them out of its count.

## 15. Honesty of claims

- **Proven on the owner's data (2026-11-07, counts only):** §1's table; the duplicate counts and bytes in
  the archive; that Gmail ids are in summary files, not mbox copies.
- **Proven in source:** where Thunderbird stores Gmail's ids, and that a folder may skip a body another
  labelled folder already keeps (§2.1); every code citation here, at 0923ffa.
- **Unverified:** why Thunderbird kept copies for Important, INBOX and Starred but not Sent Mail on this
  machine (§2.1). The design handles both outcomes.
- **By construction, to be proven by tests:** G3 rests on §4.4's rebuild check (MA-1210, MA-1211); G4's
  bound rests on §5.3 step 3 (MA-1220).
- **Conditional:** extract and restore reproduce each folder's copy only when the archive kept originals
  (`-raw`); a listed member gets the stored original, not a copy that never existed. The space saving is
  measured on the archive as it stood during a running backup.
- **Not claimed:** go-back for Gmail archives; any change to Microsoft 365 archives; recovering messages
  whose body is on no folder of this computer (that is the completeness design's job).

## 16. What the 10-lens review should attack

1. **Split and rebuild (lenses 6, 10).** Any header block where rebuild differs from the original, or two
   different emails normalize equal: folded lines, repeated bookkeeping lines, mixed line endings, a
   bookkeeping name inside another header's continuation, no empty line.
2. **The summary-file reader (lenses 6, 7, 9).** A new parser of an untrusted, Thunderbird-written format:
   limits, torn writes while Thunderbird runs, reading a file being rewritten, Windows file locks, and
   whether its cost is proportionate (or whether `X-GM-LABELS` on All Mail's summary alone would do).
3. **Listed-membership matching (lens 6).** Reused Message-IDs, the `X-GM-MSGID`-and-offset chain, a
   compacted mbox whose offsets changed since the capture, a summary rebuilt by Thunderbird.
4. **The alias skip (lenses 3, 6).** A later different email with the same Message-ID in a folder where a
   member exists: is the `Fingerprint` test enough?
5. **Conditional version writing (lens 8).** v1.6.1 on a synced folder; a v7 binary mis-stamping 6.
6. **Static cross-folder links (lenses 5, 6)**, against the go-back review's F5/GB-6.
7. **Restore's disjoint-keys assumption (lens 5)**: what else in `tbengine` assumes one folder per key.
8. **The merge step (lenses 3, 7)**: crash order, the marker, deletion after the anchor save, the fixity
   re-check, a backup started meanwhile, encrypted archives.
9. **Desktop counts and words (lens 4)**: folder counts adding up to more than the total; the merge offer;
   a gone member; a folder filled from a summary list.
10. **Seams with the completeness design (lens 5)**: one detection rule, one place that reads `prefs.js`,
    no double reading of the profile, consistent numbers between its "of Y" and this design's counts.
