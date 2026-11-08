# Design — Gmail label de-duplication: one stored copy per Gmail message, every label kept

**Revision:** 1 (2026-11-07). **BUILD STATUS:** proposed — not built. It faces the 10-lens
pre-code review (`../assurance-kit/process/design-review.md`) before any code, because it
changes what the manifest stores for a Gmail account and adds a step that removes archived
files. It is architecture- and security-relevant. Branch `gmail-dedup`, off `main` 0923ffa
(v1.6.1). The owner asked for it to be built after design and review ("do 3").

Ids in this document are **provisional**. Test-spec ids start at **MA-1190** because other
agents are using MA-1151 and up and MA-1170 and up in other worktrees; whoever merges
second renumbers. The new invariant is **R42 (provisional)** and the scenarios are
**S75 to S80 (provisional)** for the same reason.

Words used here, in plain terms:

- **copy**: one message as Thunderbird keeps it in one folder's mbox file. A Gmail message
  with three labels is three copies on disk.
- **record**: one entry in the archive's manifest (`.mailarchive-manifest.json`). Today
  there is one per copy.
- **membership**: the fact "this message is in folder F", with when it was first and last
  seen there. New in this design.
- **home folder**: the one folder whose directory in the archive holds the message's files.
- **Thunderbird's bookkeeping lines**: the header lines Thunderbird itself writes into each
  copy: `X-Mozilla-Status`, `X-Mozilla-Status2` and `X-Mozilla-Keys`. They hold that copy's
  read, flagged, deleted and tag state. They are not part of the email anyone sent.
- **normalized bytes**: a copy's raw bytes with exactly those lines taken out.

## 1. The problem, with numbers

Gmail stores each message once and shows it under every label. Over IMAP each label is a
folder, and Thunderbird downloads every folder separately, so one Gmail message becomes
several copies on disk: `[Gmail]/All Mail` holds every message, and `INBOX`, `Sent Mail`,
`Important`, `Starred` and each user label hold further copies of some of them.

The owner's counts from Gmail: All Mail 238,080, Important 56,611, Sent 33,816, INBOX
10,799, Starred 6,803, plus labels. That is about 350,000 copies of about 238,000 messages.

What is actually on the owner's Mac (read-only check, 2026-11-07; the profile's
`ImapMail/imap.gmail.com` folder; counts and sizes only; method in §10.1):

| Folder file | Copies on disk | Size | Copies also in All Mail, byte-identical |
|---|---:|---:|---:|
| `[Gmail]/All Mail` | 238,201 | 31.37 GB | (the reference) |
| `[Gmail]/Important` | 56,585 | 12.64 GB | 56,584 (1 has no Message-ID) |
| `INBOX` | 10,887 | 3.51 GB | 10,885 (2 not yet in All Mail's file) |
| `[Gmail]/Starred` | 6,803 | 1.51 GB | 6,803 |
| `SAS` | 2,233 | 1.41 GB | 2,233 |
| `Calendar` | 1,194 | 18 MB | 1,194 |
| `[Gmail]/Drafts` | 686 | 117 MB | 686 |
| seven other labels | 766 | 0.28 GB | 766 |
| `[Gmail]/Sent Mail` | 2 | 0.2 MB | 2 |
| **all Gmail folder files** | **317,357** | **50.86 GB** | **79,153 of 79,156 extra copies** |

Two facts stand out:

- Every extra copy that was found in All Mail was **byte-for-byte identical** to its All
  Mail copy, Thunderbird's bookkeeping lines included, on the day of the check.
- `Sent Mail` holds 2 message bodies on disk although Gmail counts 33,816: Thunderbird is not
  keeping that folder for offline use. Those messages are in All Mail, so they are archived
  from there today and under this design.

MailArchive archives every copy as its own record today (§2), so this account is archived
as 317,357 records and 50.86 GB of source mail where 238,204 records and about 31.4 GB are
distinct. Every search shows the same email up to five times, and every count is inflated.

**Goal.** In an archive of a Gmail account, each email is stored once; the folders it is in
are recorded and shown (on the message page, as search filters, in the folder tree); a
message is reachable from every folder it belongs to; and nothing the archive promises
today is lost.

## 2. What the code does today

- **The key is folder-scoped for every local source.** The exporter builds
  `state.Key(store, folderKey, m.Identity())` unless `DedupMailboxWide` is set
  (`internal/export/exporter.go:219-225`). The key is `token\x00folder\x00identity`
  (`internal/state/manifest.go:413-420`). The local run never sets the flag
  (`internal/app/run.go:795-804`); only the Microsoft 365 path does
  (`internal/app/graph.go:251`). A Thunderbird Gmail account is therefore a "one-shot local
  import" under R3: "the same mail filed in two folders exports to both"
  (`docs/scenario-catalog.md:24-33`; scenario S4 at `:435`). The local run also builds the
  folder-scoped key itself in one place (`internal/app/localrun.go:415`).
- **Store and folder names.** A Thunderbird account directory is one store; its name is the
  directory's name, normally `imap.gmail.com` (`internal/source/mboxreader.go:21`;
  auto-discovery of `ImapMail/*` at `run.go:1881-1901`; token at `run.go:1674`). The folder
  path is built from the mbox file names, so All Mail is `["[Gmail]", "All Mail"]`
  (`internal/source/localwalk.go:506`, `:547`). The run never reads the account's host from
  `prefs.js`; `thunderbird.FindAccountForStore` can (`internal/thunderbird/thunderbird.go:81`),
  but only the wizard calls it (`internal/wizard/wizard.go:1554-1575`).
- **Identity** is `mid:<Message-ID>`, or `sha:<content hash>` when there is none
  (`internal/model/message.go:318-327`). A different message that reuses a Message-ID in the
  same folder is told apart by the 16-hex envelope `Fingerprint` (`message.go:340-356`) and
  filed under a qualified key (`exporter.go:298-311`; `manifest.go:437-448`).
- **A store-wide identity index already exists**: `byIdent` maps an identity to every key
  that carries it (`manifest.go:383-388`; `KeysForIdentity` at `:1721-1736`; the per-store
  count at `:1645-1659`). This design reuses it.
- **Microsoft 365 already stores a message once per mailbox** (`LiveKey`,
  `manifest.go:422-435`), but with ONE current folder (`Record.Folder`, `:150-165`): a move,
  not membership of several folders at once. Microsoft 365 has no labels. That path is out
  of scope and unchanged.
- **Correction to the brief.** Per-folder history ("go back in time") and deletion detection
  exist only for Microsoft 365 archives. The local run says: "A one-shot local import keeps
  no go-back timeline (its dedup is folder-scoped, §3.6), so there is no history writer to
  sync here" (`run.go:927-929`). The restore design records that `Present`/`LastSeen` are
  "kept only for mailbox-wide archives (Microsoft 365), so for a Thunderbird or local archive
  `LastSeen` is zero" (`docs/design-restore-thunderbird.md` §6.4, note RT-9). So a
  Gmail-through-Thunderbird archive has no go-back or deletion detection to lose. This design
  adds per-folder first-seen and last-seen data (§6.2), which is new, and leaves the
  Microsoft 365 timeline as it is.
- **Restore already meets the Gmail case.** It restores each record into its `Folder`, so a
  two-label message comes back in both, and it warns when All Mail is chosen with other
  folders (`design-restore-thunderbird.md` §6.5; `internal/tbengine/plan.go:664-688`; words at
  `internal/tbengine/words.go:73`). Lab row MA-1093 includes "a Gmail two-label message".
- **The page shows that copy's state.** The message page renders "Unread" and "Categories"
  from the copy's own `X-Mozilla-Status` and `X-Mozilla-Keys`
  (`internal/source/mbox.go:132-142`; `internal/export/html.go:2099-2107`, `:2237-2238`).
- **Deleted-but-not-compacted copies are archived.** The mbox reader does not skip a copy
  whose `X-Mozilla-Status` has Thunderbird's deleted bit (no such check in `source/mbox.go`,
  `localwalk.go` or `mime.go`; the read flag is the only bit read, `mbox.go:280-293`). The
  owner's All Mail file holds 2,676 such copies. Unchanged by this design; noted as Q7.

## 3. Properties (what this design makes true)

- **G1 — One stored copy per Gmail message.** In a store marked as a Gmail store (§5), two
  copies of the same message in different folders are one record with one set of files.
- **G2 — Every folder is recorded.** The record lists each folder the message was seen in,
  with first-seen and last-seen times and that copy's Thunderbird bookkeeping lines.
- **G3 — A merge loses no byte (R42, provisional).** A copy is merged into an existing record
  only when its normalized bytes have the same SHA-256 as the record's, and the copy's exact
  bytes are then rebuilt from its normalized bytes plus its own bookkeeping lines and checked
  against the SHA-256 of the copy as read. When either check fails the copy is archived as its
  own record, exactly as today. So even a merge of two "different" emails cannot lose
  anything: they differ only in Thunderbird's lines, which are kept per folder (§4.4).
- **G4 — Reachable from every folder.** The Desktop reader, search filters and facets, the
  folder tree, and the static folder pages list the message under every folder it is a member
  of, all pointing at its one set of files.
- **G5 — Fidelity is unchanged.** With `-raw` the original is kept once (the home copy's
  bytes). Extract and restore write the message into every folder it belongs to, each folder's
  output being that folder's copy rebuilt byte for byte and checked against its recorded
  SHA-256 before it is written; a mismatch is skipped and listed, as R20 does today (§7.4,
  §7.5).
- **G6 — Older versions refuse; they never damage.** An archive that holds memberships is
  manifest version 7, which v1.6.1 and earlier refuse by name (§6.4).
- **G7 — Nothing already archived is deleted without a plan and a yes.** Existing archives
  are merged only through a shown plan the person confirms, and a file is deleted only when
  the archive itself proves it is a copy (§8).

## 4. Identity: how to know two copies are the same email

### 4.1 The candidates

| Signal | What it is | Verdict |
|---|---|---|
| Message-ID header | the sender's id for the email | **Lookup only.** In the owner's All Mail, 4,846 Message-IDs appear more than once; in 253 of them the bodies differ, so they are different emails (§10.1). It can also be missing. Never enough to merge. |
| Gmail's `X-GM-MSGID` | Gmail's own per-message number | **Not used in version 1** (§4.2). |
| SHA-256 of the copy's raw bytes | the exact bytes in the mbox | **Not enough alone**: Thunderbird's lines can differ per folder (§4.3). |
| SHA-256 of the normalized bytes | the email as Gmail served it | **The merge test**, with the rebuild check (§4.4). |
| `ContentDigest` (`message.go:376-406`) | parsed subject, people, date, bodies, attachment names and sizes | **Not enough**: it does not cover attachment bytes or most headers. |

### 4.2 Why not Gmail's `X-GM-MSGID`

- **Where Thunderbird keeps it.** Thunderbird reads `X-GM-MSGID`, `X-GM-THRID` and
  `X-GM-LABELS` from Gmail and stores them as properties of the message's row in the
  folder's summary database (the `.msf` file), not in the message bytes. Source:
  `mailnews/imap/src/nsImapMailFolder.cpp`, function `NormalEndHeaderParseStream`:
  `newMsgHdr->SetStringProperty("X-GM-MSGID", msgIDValue);` (read from the
  `mozilla/releases-comm-central` mirror on GitHub, 2026-11-07; line number not recorded).
- **The owner's profile agrees** (§10.1). Each `.msf` file has `X-GM-MSGID`, `X-GM-THRID`
  and `X-GM-LABELS` columns. No mbox copy in the sample of 3,000 per folder has an
  `X-GM-MSGID` header line. (The mbox copies do carry `X-GM-Message-State`, `X-GM-Features`
  and `X-GM-GG` on some messages: Google's sending servers put those into the email itself.)
- **Why that rules it out for version 1:**
  - MailArchive reads no `.msf` file today (nothing in `internal/source` opens one). Using
    it means a parser for Mork, Thunderbird's old database format, which is a new attack
    surface on untrusted local files (R25), plus a mapping from each database row to the
    right message in the mbox.
  - The `.msf` can be missing or rebuilt. Thunderbird rebuilds a missing one, and
    MailArchive's own restore writes none (`design-restore-thunderbird.md` §3.4).
  - The owner ruled on 2026-09-12 that identity must be portable across sources and must not
    depend on one vendor's private feature (`docs/note-goback-identity-options.md`,
    "Vendor-robust").
  - It is not needed: the rule in §4.4 is safe without it, and on the owner's data it matches
    all but 3 of 79,156 extra copies.
- It may come back later as an optional hint (for example, to recognise a partial offline
  copy), never as the thing correctness rests on.

### 4.3 Thunderbird's bytes per folder

Each copy has its own `X-Mozilla-Status` and `X-Mozilla-Status2` lines (and `X-Mozilla-Keys`
when tags are used). Thunderbird updates them in place when the person reads, flags, tags or
deletes that copy, so two copies of one message can differ there. The archive keeps those
lines inside the raw bytes and reads read-state and tags from them (`mbox.go:132-142`; the
raw bytes are what the reader returns, `internal/source/rawstub.go:51-61`).

On the owner's profile on 2026-11-07, every extra copy found in All Mail was identical to it
including those lines (§1). The design does not rely on that: the lines are set aside before
comparing and kept per folder, so a copy read in INBOX but not in Important still merges and
still rebuilds exactly.

### 4.4 The rule

For a copy read from folder F of a Gmail store, with raw bytes `B`:

1. **Split.** `T` is every line of the top header block (before the first empty line) whose
   header name is exactly `X-Mozilla-Status`, `X-Mozilla-Status2` or `X-Mozilla-Keys` (any
   letter case), with its continuation lines, each kept verbatim with its position among the
   header lines. `N` is `B` with exactly those lines removed. Nothing after the first empty
   line is looked at or changed. The split is bounded: at most 8 such lines and 1 KiB in all,
   and the empty line must come within the existing 64 KiB header bound; otherwise the copy
   is never merged.
2. **Look up.** Candidates are records of the same store token with the same identity
   (`KeysForIdentity`, filtered by token) that carry a stored `NormSHA256`.
3. **Compare.** A candidate matches when its `NormSHA256` equals `SHA-256(N)`.
4. **Prove.** Rebuild `B'` by putting `T` back into `N` at the recorded positions, and check
   `SHA-256(B') == SHA-256(B)`. This proves the split is reversible for this copy. Because the
   stored record's normalized bytes equal `N` (same SHA-256), any later rebuild of folder F's
   copy — the stored original with its own lines removed, then F's lines put back — gives `B`.
5. **Merge or not.** On a match, add (or refresh) membership F with `T` and `SHA-256(B)`; no
   file is written. On no match, archive the copy as its own record, as today, and store its
   `NormSHA256` so later copies can match it.

Never merged, whatever the digests say: a copy with a read gap (the existing KEEP guard runs
first, `exporter.go:227-233`), an unparsed stub, a copy with no raw bytes, and a copy whose
split is out of bounds.

### 4.5 What a wrong answer costs

| Case | When it happens | Cost |
|---|---|---|
| **False merge** (two different emails become one record) | Only when every byte except Thunderbird's three lines is identical, every header and attachment byte included. | Nothing is lost: each folder's copy rebuilds byte for byte (G3). Search shows one hit where Thunderbird showed two identical emails. |
| **Missed match** (a duplicate is kept) | The copies differ in more than those lines: a partial or headers-only offline copy, a damaged copy, a re-delivery with new `Received` lines. | A second record, exactly as today. `status` counts them: "N copies share a Message-ID with another folder's copy but differ in content; kept separately." |
| **Different email reusing a Message-ID** | Normalized digests differ (253 such Message-IDs in the owner's All Mail). | Kept apart, as today (R1). |

A merge needs a byte-identity of everything that came from Gmail, so no sender-controlled
field is trusted.

## 5. Scope: which accounts

**Recommendation: Gmail accounts read through Thunderbird only, in version 1.**

- **Detection.** A store is a Gmail store when its source is a Thunderbird IMAP account
  directory and either (a) the profile's `prefs.js` gives that account the host
  `imap.gmail.com` or `imap.googlemail.com`, or sets `mail.server.serverN.is_gmail` to true
  (the owner's profile: `mail.server.server4.is_gmail` true, host `imap.gmail.com`), read
  with the existing `thunderbird.FindAccountForStore`; or (b) when `prefs.js` cannot be read,
  the account directory has a top-level `[Gmail].sbd` or `[Google Mail].sbd` (the German and
  UK spelling). Detection reads names only, through the existing contained reader (R25).
- **Sticky per store.** The decision is recorded per store token in the manifest
  (`DedupStores`, §6.1) on the store's first archived run with dedup, and never flips by
  itself. A store already archived without dedup changes only through the merge step (§8).
- **Wrong detection is harmless.** A non-Gmail account wrongly detected (say, one with a
  folder named `[Gmail]`) only merges byte-identical copies, which loses nothing (G3).
- **Why not every account.** On other IMAP servers, two copies in two folders are two real
  server copies made by a copy or a filter; there are few of them, and the person may think
  of them as separate. The rule would still be lossless, so widening later is one detection
  change plus a review (Q2).
- **Out of scope:** Microsoft 365 through Graph (no labels; already once per mailbox);
  Outlook PST/OST (no raw bytes, so a merge cannot be proved); Evolution's cache of a Gmail
  account (a different layout; a follow-up once this is proven); a future live IMAP source
  (could use `X-GM-MSGID` from the server directly; its own design).

## 6. Storage

### 6.1 Manifest schema (version 7)

The key format does not change. A merged message keeps the key of its home copy,
`Key(token, homeFolder, identity)` or its qualified form. New fields, all `omitempty`, so a
manifest with no Gmail store is byte-identical to today's (the MA-329 golden):

```go
// Record
NormSHA256  string       `json:"norm_sha256,omitempty"`  // SHA-256 of the normalized bytes (§4.4); Gmail stores only
Folders     []Membership `json:"folders,omitempty"`      // every folder, the home one included
MergedFiles []string     `json:"merged_files,omitempty"` // the merge step's files awaiting deletion (§8)

type Membership struct {
    Folder    string    `json:"folder"`         // spelled as Record.Folder is
    FirstSeen time.Time `json:"first_seen"`
    LastSeen  time.Time `json:"last_seen"`
    Gone      bool      `json:"gone,omitempty"` // missing from a complete walk of this folder (§7.3)
    Moz       string    `json:"moz,omitempty"`  // this copy's bookkeeping lines and positions, encoded
    CopySHA   string    `json:"copy_sha256"`    // SHA-256 of this copy's raw bytes as read
}

// Manifest
DedupStores map[string]string `json:"dedup_stores,omitempty"` // token -> "gmail-1"
```

- `Record.Folder` keeps meaning the home folder for these records, so any reader not yet
  changed still sees one valid folder.
- At load an **alias index** is built: `(token, folder, identity) -> home key` for every
  membership. After a plain key miss the exporter consults it, so a re-run finds a merged
  copy without hashing.
- Load validates every membership: a folder that is not a clean relative path, a `Moz` that
  does not decode within the bounds, or a malformed digest drops that membership (logged),
  never the record.

### 6.2 Per-folder history

A membership's `FirstSeen`, `LastSeen` and `Gone` are the per-folder history, written in place
in the manifest. No history log is added for local archives in version 1 (the Microsoft 365
log in `internal/state/history.go` is unchanged). The reader can say "In Receipts since
2024-03-01" and "Not in Receipts since 2026-11-10". A date slider for Gmail archives is a later
design (Q5).

### 6.3 Size of the new data

About 250 bytes of manifest JSON per extra membership. For the owner's 79,153 merged copies
that is about 20 MB, replacing 79,153 records that each cost more (a record with fixity is
about 0.6 to 1 KB). The manifest gets smaller.

### 6.4 Version and older MailArchive versions

- `manifestVersion` becomes 7. Load refuses a stored version above what it understands
  (`manifest.go:779-786`), so v1.6.1 and earlier refuse a version-7 archive with
  `NewerFormatError` and change nothing. Without the bump an older binary would load the
  file, drop the unknown `folders` field on its next save (Go's JSON decoder ignores unknown
  fields), and silently lose every membership.
- **Lead's call, for the review to attack:** write 7 only when the manifest holds a
  `DedupStores` entry or any record with `Folders`; otherwise keep writing 6, so an archive
  with no Gmail store stays usable by v1.6.1. Today `save` always stamps the current constant
  (`manifest.go:2148-2160`); this changes that line and its tests.
- The search index (`internal/index/index.go:27-34`, `indexVersion = 3`; newer refused at
  `:162-164`) gains one table (§7.1). Same rule: stamp 4 only when the table has rows.
- An encrypted archive's sealed manifest carries the same fields inside the sealed record
  (`internal/container/record.go:61-64`; `manifest.go:744-768`), so the same version rule
  applies to its inner version.

## 7. Every reader and writer of the manifest

Each item: where it is today, and what changes. "Members" means the record's `Folders`.

### 7.1 Capture (the exporter and the local run)

- `Export` (`exporter.go:207-311`) gains a Gmail-store branch after the plain key lookup:
  1. Key hit in this folder: today's path (skip, retry, or `#fp` split).
  2. Alias hit `(token, F, identity)`: the same message when its `Fingerprint` matches the
     home record's (today's skip test, `exporter.go:298`); then stamp F's `LastSeen` and skip.
     No hashing on a steady re-run (R2 stays cheap). A mismatch is a different email and
     goes on as today (new record, qualified key).
  3. Miss: §4.4 against same-token siblings; merge (membership, no write) or write a new
     record with `NormSHA256` and its own home membership.
- `localrun.go:415` (the unwritable-folder shortcut) uses the same lookup, so a merged copy in
  a folder the archive cannot create is not counted as a gap.
- The capture-gap ledger's per-folder identity locator (`localrun.go:244-254`) is unchanged: a
  copy that fails to write is still its folder's own gap.
- A `full` run rewrites the home record's files once and refreshes the other members; it never
  writes a second set of files.
- `status` and the run summary say: "Gmail: N messages, M extra folder copies recorded without
  storing them again."

### 7.2 Search index

- Today: `docs(key UNIQUE, store, folder, …)` with one folder per row
  (`internal/index/index.go:41-63`); `Add` replaces any earlier row of the key
  (`index.go:431-458`).
- Change: a new table `doc_folders(key, store, folder)` with an index on `(store, folder)`.
  `Add` for a Gmail record writes the `docs` row as today (folder = home) and one
  `doc_folders` row per member. Merging a copy adds one `doc_folders` row, body-free.
- Filters (`internal/index/query.go:141-142`), facets (`Folders()`, `query.go:303`) and
  folder listing (`ListFolder`, `internal/index/listfolder.go:227-262`) match a folder when
  either `docs.folder` or a `doc_folders` row matches. One message is one search hit.
- Rebuild paths take folders from the manifest's members, not from `Path`: `Rebuild`
  (`internal/app/reindex.go:396-399`), `catchUpIndex` (`internal/app/sealedindex.go:263`),
  `indexCopied` (`internal/app/decryptcopy.go:457`).

### 7.3 Per-folder "no longer here"

At the end of a walk of a Gmail store that completed with no folder fault and an intact lock,
a member whose folder was walked to the end and whose `LastSeen` is older than this run is
marked `Gone`. This mirrors `SweepGone`'s guards (`manifest.go:1696-1719`): never at a
checkpoint, never for a folder not walked, nothing when the walk did not complete. The record
and files stay. A member seen again loses `Gone`. When every member is gone the record stays
and is shown as "no longer in any folder".

### 7.4 Desktop reader, folder tree and search page

- The tree puts each key in exactly one node from `rec.Folder`
  (`internal/app/copyexport_archive.go:489`, `:530-542`); messages come from the index
  (`internal/app/ws_archive.go:448-499`). Change: a Gmail record's key goes in every member's
  node. Folder counts are "messages in this folder"; archive totals count records, worded
  "238,204 messages in 15 folders" (distinct records, and the folders that hold any), never a sum of folder counts.
- `resolveRecord` (`copyexport_archive.go:855-862`) gives a copied message one folder chain;
  for a Gmail record it uses the folder the person opened it from.
- The message view in the reader shows a line under the header: "In: All Mail, INBOX,
  Important, Starred", with "(no longer in Receipts since 10 Nov 2026)" for a gone member.
- Search (`internal/server/page.go:57`, `:123`, `:135`; `internal/server/server.go:61`, `:79-86`)
  gets folder filters from the facets above; a result lists its folders.
- The Microsoft 365 go-back views (`internal/server/goback.go:137-180`) are untouched.

### 7.5 Static pages

- Today folder pages group by the file's directory (`internal/pages/pages.go:122`,
  `:144-146`, from `EachDoc` ordered by path, `internal/index/pagequery.go:19`).
- Change: after that pass, each folder page also lists the Gmail records that are members of
  it but live elsewhere, linking to the home file by a relative path inside the same store
  directory, computed from the validated `Path` (R4). Pages stay script-free (R19). The
  go-back review rejected cross-directory links for Microsoft 365 moves
  (`docs/review-goback-predesign.md`, F5/GB-6) because folders there change every run; Gmail
  membership is mostly add-only, which the review should confirm or refute.
- The message page itself is frozen at capture as today: its "This folder" link goes to the
  home folder (`html.go:2176-2185`), and its Unread and Categories lines are the home copy's.
  The full folder list is on the folder pages and in the reader.
- Encrypted archives render pages in memory with the same code (`internal/keyring/reader.go:560`).

### 7.6 Extract (mbox and eml)

- Today one output folder per record's `Path` directory; `Record.Folder` is unused
  (`internal/app/extract.go:129`, `:273-287`, `:413`, `:452`).
- Change: a Gmail record is written into every member's folder. Each folder's message is the
  stored original with its own bookkeeping lines removed and that folder's lines put back,
  checked against the member's `CopySHA` before it is written; a mismatch is skipped and
  listed like a missing original (R20). Without `-raw` there is no original to extract, as
  today.
- `-format eml` writes one file per member folder, so the tree mirrors Thunderbird's view.

### 7.7 Restore to Thunderbird

- Today `SelectForRestore` keeps a key in the first node only, so `RestoreFolder.Keys` are
  disjoint (`internal/app/restoreread.go:221-231`, `:260`, `:315`); the plan copies them
  (`tbengine/plan.go:336-339`); the run reads each key's original (`tbengine/run.go:216-245`)
  and appends it (`internal/tbrestore/write.go:387-408`).
- Change: a Gmail record's key is placed in each chosen member folder, and the run writes that
  folder's rebuilt copy (checked against `CopySHA`, as in §7.6). The plan's counts and size are
  per folder written, so they match what lands on disk. R40 ("a restore adds, and only adds")
  is unaffected. The All Mail note stays (`plan.go:603`, `words.go:73`).
- MA-1093's "Gmail two-label message" now comes from one record with two members.

### 7.8 verify

- Today `owned` comes from each record's `Path` stem only, and other archived-type files are
  `unexpected` (`internal/app/verify.go:194`, `:316-323`, `:436`, `:615`).
- Change: `MergedFiles` are owned while they wait (§8). With an original present, verify also
  rebuilds each member's copy and compares it with `CopySHA`; a mismatch is reported as
  `modified` and names the folder. Counted in coverage like fixity.
- Side note for the review: `AlsoFiles` (the Microsoft 365 collapse losers) are not owned
  today and show as `unexpected`, as the comment at `manifest.go:399-404` says.

### 7.9 Delete and reindex redaction

- Redaction deletes `Path`'s files and every `AlsoFiles` entry (`reindex.go:158`, `:176-178`).
  Change: also every `MergedFiles` entry. Deleting a Gmail message's files removes it from
  every folder, because it is one email; the reader and the docs say so.
- Archive delete lists `Path` plus `AlsoFiles` (`internal/archdelete/lister.go:205`, `:882`) and
  folders from `rec.Folder` (`:833`). Change: add `MergedFiles` and the members' folders.

### 7.10 Encrypted archives

- The whole record is sealed (`container/record.go:61-64`); files are opaque units with no
  folder tree (`internal/export/units.go:23-26`, `:64-73`; `exporter.go:410-433`). Members and
  their lines are inside the sealed record, so nothing about labels is readable without the
  key. A merge writes no unit. The merge step's deletions use the existing deferred unit
  deletion (`exporter.go:155-160`).

### 7.11 Microsoft 365 path

Unchanged. A Graph token is never in `DedupStores`, and Load refuses `Folders` on a record
whose token is a Graph mailbox (`stores` maps it from a mailbox address). Side note for the
review: `CollapseByIdentity`'s caller writes no history event for collapse losers
(`graph.go:341-401`), although `CollapseLoss`'s comment says it does (`manifest.go:399-404`).

## 8. Existing archives: the merge step

An archive that already holds a Gmail account per folder is not changed by upgrading.
Captures stay per folder until the person merges.

**Recommendation:** if the account is not yet archived, or the archive is young, start a new
archive (Q1); dedup is on from its first backup. Otherwise use this step.

1. **Evidence, automatically, at the next backup.** For records of a detected Gmail store
   that lack `NormSHA256`, the backup reads each copy anyway; it stamps `NormSHA256`, `CopySHA`
   and the lines on the record. Metadata only; no file is written or deleted.
2. **The offer.** The archive's card says once: "This archive keeps 79,153 extra copies of
   Gmail messages (about 19.5 GB). Merge them?" with Review and Not now. CLI:
   `mailarchive merge-copies -out DIR -plan`.
3. **The plan** (read-only) groups records with the same token, identity and `NormSHA256`
   and shows per folder how many copies merge and how much space is freed. A file is offered
   for deletion only when the archive itself proves it is a copy: both the survivor and the
   copy have a preserved original whose normalized SHA-256 is equal, both pass their recorded
   fixity, and the survivor is at least as complete (no gap label the copy lacks). Groups
   without that proof are merged in the manifest only (one search hit) and their files are
   kept and counted: "N copies could not be proven identical from the archive alone; kept."
   The survivor is the most complete record, then the earliest captured, then the lowest key,
   so its files never move.
4. **The yes.** On confirm, holding the archive lock (a scheduled run that fires meanwhile
   refuses, as for verify):
   1. write a marker `.mailarchive-merge.json` listing the groups, fsynced;
   2. index: remove the copies' rows, add `doc_folders` rows, in one transaction, made durable;
   3. manifest: the survivor gains members (from each copy: folder, first seen = its capture
      time, its lines and `CopySHA`), the copies' records go, their file paths go into the
      survivor's `MergedFiles`, the store goes into `DedupStores`; saved (the anchor);
   4. folder pages regenerate;
   5. each file in `MergedFiles` is deleted only if its bytes still match its recorded fixity
      (else kept and reported), then `MergedFiles` is cleared and saved;
   6. the marker is removed.
   A crash at any step leaves the marker; the next run of the step (or of a backup, which
   refuses until it is done) finishes forward from the saved manifest. A file is deleted only
   after no saved record or index row names it (R28).
5. **Encrypted archives**: the same steps on the sealed manifest; units are deleted through
   the deferred deletion of §7.10.

This step deletes archived files, so it owes the adversarial pass, the friction review and the
owner's sign-off on its words (R36's "only by the person's explicit decision" in spirit).

## 9. Disk and speed

Measured source sizes, estimated archive effect (an estimate: archive size has not been
measured against source size for this account):

- **Copies:** 317,357 records become 238,204 (25% fewer).
- **Bytes:** 19.49 GB of the 50.86 GB of source mail is extra copies (38%). The archive's
  pages, zips and preserved originals scale with the source, so an archive of this account
  should be about **38% smaller, roughly 19 to 20 GB less** with `-raw`. Without `-raw` the
  saving is the pages and zips only.
- **First backup:** about 79,000 fewer pages rendered and zips written. Each merge costs one
  SHA-256 of the copy (about 19.5 GB hashed once, tens of seconds on current machines),
  against rendering and writing that copy today.
- **Later backups:** unchanged reading (Thunderbird's files are read as today); no hashing for
  copies already recorded (the alias lookup, §7.1).
- **Search:** one hit per email instead of up to five.

## 10. Evidence

### 10.1 The read-only check of the owner's profile

Run 2026-11-07 by the design agent with a script kept outside the repository
(`mail-archive-tool/.claude/gmail-dedup-probe/probe.py`, not committed). It opened the mbox
and `.msf` files read-only, hashed Message-IDs in memory only to pair copies, and printed
counts, sizes and header names only. No subject, address, body or Message-ID was printed or
stored. Findings:

- Message counts and byte-identity per folder: §1's table.
- After setting Thunderbird's lines aside, 79,153 of 79,156 extra copies matched an All Mail
  copy; all of those were identical even with the lines. The 3 others: 2 INBOX copies newer
  than All Mail's file, 1 copy with no Message-ID (the probe paired by Message-ID only).
- All Mail: 4,846 Message-IDs appear more than once. In 32 the copies are identical, in 4,561
  the bodies are equal but other headers differ, in 253 the bodies differ. Message-ID alone
  would have merged all 4,846.
- 2,676 All Mail copies and 33 Important copies carry Thunderbird's deleted bit.
- No copy had Thunderbird's partial-download bit.
- Every `.msf` checked (All Mail, INBOX, Important, Sent Mail, Starred) has the `X-GM-MSGID`
  column with about as many cells as Message-ID cells.

### 10.2 Thunderbird source

- `nsImapMailFolder.cpp`, `NormalEndHeaderParseStream`: Gmail ids stored with
  `SetStringProperty` on the database row (§4.2).

## 11. Tests, catalog rows and lab rows

All ids provisional (see the top). Every test cites its rows with `// covers:` markers.

**Invariant R42 (provisional) — A Gmail copy is stored once, and a merge loses no byte.** In a
Gmail store, a copy whose normalized bytes match an archived record's is recorded as a member
of that record, and is merged only after its exact bytes are rebuilt and checked; every
member's copy is reproducible byte for byte by extract and restore, or is skipped and listed.
**R3 reworded:** its local-import sentence gains "except a Gmail store (R42), where a copy in a
second folder is recorded on the same record, not stored again."

**Scenarios (provisional):** S75 one message in four Gmail folders; S76 Message-ID reused by
different emails in a Gmail store; S77 a label removed in Gmail; S78 an older version opens a
version-7 archive; S79 merge step on an existing archive, with a crash mid-way; S80 restore and
extract of a multi-folder message.

| Id | Tier | What it proves |
|---|---|---|
| MA-1190 | U | split/rebuild: for any header block, rebuild(split(B)) == B; nothing after the first empty line changes; out-of-bounds input is refused (property test plus hostile cases: folded lines, duplicated lines, LF and CRLF, a header named `X-Mozilla-Status-Fake`) |
| MA-1191 | U | two copies differing only in the three lines merge; one differing anywhere else (one byte in an attachment) does not (R1) |
| MA-1192 | U | a Message-ID reused by a different email in another folder stays a separate record |
| MA-1193 | U | read-gap copy, unparsed stub and no-raw copy are never merged |
| MA-1194 | S | a fixture Gmail account (four folders, one message in all) archives one record with four members; a re-run exports zero and hashes nothing |
| MA-1195 | S | detection: host from `prefs.js`, `is_gmail`, the `[Gmail]`/`[Google Mail]` fallback; a non-Gmail account stays per folder; the decision is sticky |
| MA-1196 | U | manifest v7: written only with a Gmail store; a v6 archive stays byte-identical (MA-329 golden); v1.6.1's Load refuses v7 (a frozen v6 loader fixture) |
| MA-1197 | U | load drops a hostile member (bad folder, oversized lines, bad digest) and keeps the record |
| MA-1198 | S | index: one hit per message; folder filter, facets and folder listing find it under every member; rebuild from the manifest gives the same rows |
| MA-1199 | S | per-folder gone: marked only after a complete walk; not on a faulted walk; cleared when seen again |
| MA-1200 | S | static folder pages list members living elsewhere with a working relative link; pages stay script-free (R19); links stay inside the store tree (R4) |
| MA-1201 | S | extract mbox and eml write the message into every member folder, each byte-equal to that folder's copy; a tampered `CopySHA` is skipped and listed (R20) |
| MA-1202 | S | restore writes every chosen member folder's copy; counts match; R40 holds |
| MA-1203 | S | verify reports a member whose rebuild mismatches, and owns `MergedFiles` |
| MA-1204 | S | merge step: plan writes nothing; confirm merges; only proven copies are deleted; fixity-mismatched files are kept; a crash at each of steps 4.1 to 4.6 finishes forward with nothing lost |
| MA-1205 | S | encrypted archive: the same as MA-1194 and MA-1204 through sealed units |
| MA-1206 | S | redaction and archive delete reach `MergedFiles` and every member's folder |
| MA-1207 | L (pending) | Windows VM: a throwaway Gmail account in Thunderbird, three labels on one message, archived, restored, extracted; Thunderbird shows each folder; saved `.eml` SHA-256 equal per folder |
| MA-1208 | L (pending) | macOS: the read-only count check of §10.1 on the owner's profile, repeated after the build against a real archive run into a scratch folder (counts and sizes only) |
| MA-1209 | L (pending) | Linux: MA-1207's fixture through Docker (no GUI; archive, extract, verify only) |

Lab story: the Windows row needs a Gmail account the owner creates for testing (MailArchive's
agents never sign in to a real account); Thunderbird on the Parallels VM syncs it. Windows runs
first, per the owner's 2026-11-01 rule. The owner's Mac is used read-only, for counts only.

## 12. Build slices and gates

| Slice | What | Gate before the next |
|---|---|---|
| GD0 | This design; catalog rows registered | **10-lens pre-code review**, filed as `docs/review-gmail-dedup-predesign.md`; owner answers to §13 |
| GD1 | Split/rebuild core in its own leaf package, no callers (MA-1190, MA-1191) | **Adversarial pass** (it is the false-merge guard) plus a mutation sweep |
| GD2 | Manifest v7 fields, alias index, conditional version, load validation, sealed record (MA-1196, MA-1197) | unfiltered `go test ./...` |
| GD3 | Detection, capture merge, per-folder seen and gone, `status` line (MA-1192 to MA-1195, MA-1199) | **Adversarial pass** (R1-critical); Windows run on the VM |
| GD4 | Index table, reader tree, search, static pages (MA-1198, MA-1200) | **Friction review** (Windows first, then Mac) and the UX contract walk |
| GD5 | Extract, restore, verify per member (MA-1201 to MA-1203, MA-1206) | **Adversarial pass** on restore (R40) and extract (R20) |
| GD6 | Merge step for existing archives (MA-1204, MA-1205) | **Adversarial pass**, **friction review**, owner sign-off on the words |
| GD7 | Lab rows (MA-1207 to MA-1209), README, help page, release notes | owner's release call |

Every fix ships prove-fail then prove-pass, recorded in its commit. GD1 to GD3 can ship before
GD6: new Gmail archives get dedup, existing ones wait for the merge step.

## 13. Owner questions, each with a recommendation

- **Q1 — Is this Gmail account already archived?** Recommended: if not, or if the archive is
  young, start a new archive and use the merge step only for an archive worth keeping.
- **Q2 — Gmail only, or every Thunderbird account?** Recommended: Gmail only for now. Other
  accounts rarely have many copies, and keeping the first version small makes it safer.
- **Q3 — In an existing archive, delete a copy's files only when the archive itself proves it
  is a copy (both have kept originals)?** Recommended: yes. Copies without that proof are
  merged in search but their files are kept.
- **Q4 — On by default for new Gmail archives?** Recommended: yes, with one line in setup and
  Settings: "Gmail: each email is stored once and shown in all of its folders."
- **Q5 — Show "no longer in this folder since DATE"?** Recommended: yes, as a line in the
  reader; a date slider for Gmail archives later, as its own design.
- **Q6 — Restore: keep All Mail ticked with the note, as built?** Recommended: yes, unchanged.
- **Q7 — Two side findings, separate from this design.** (a) Thunderbird keeps only 2 of your
  33,816 Sent Mail bodies offline; they are archived from All Mail, so nothing is missing, but
  the Sent folder in the archive will look nearly empty unless Thunderbird syncs it.
  (b) 2,709 copies Thunderbird has marked deleted (not yet compacted) are archived today.
  Recommended: leave both as they are for now and decide in a small follow-up.

## 14. Honesty of claims

- **Proven on the owner's data (2026-11-07, counts only):** the copy counts, sizes and
  byte-identity of §1 and §10.1; that Thunderbird keeps Gmail's ids in `.msf`, not the mbox.
- **Proven in source:** where Thunderbird stores Gmail's ids (§4.2); every code citation here,
  at 0923ffa.
- **By construction, to be proven by tests:** G3's "no byte lost" rests on the rebuild check
  of §4.4, which MA-1190 and MA-1191 must prove, and on the split never touching anything after
  the first empty line.
- **Conditional:** extract and restore reproduce each folder's copy only when the archive kept
  the original (`-raw`); without it there is nothing to extract, as today. The disk saving is
  an estimate from source sizes (§9).
- **Not claimed:** go-back for Gmail archives; any change to Microsoft 365 archives; merging of
  partial offline copies (they stay separate).

## 15. What the 10-lens review should attack

1. **The split and rebuild (lens 6, 10).** Can any header block make `rebuild(split(B))`
   differ from `B`, or make two different emails' normalized bytes equal? Folded lines,
   duplicated bookkeeping lines, mixed line endings, a bookkeeping name inside another
   header's continuation, a block with no empty line.
2. **The alias skip (lens 3, 6).** A later different email with the same Message-ID landing in
   a folder where a member exists: is the `Fingerprint` test enough, or must the alias path
   also hash?
3. **Conditional version writing (lens 8).** Writing 6 or 7 depending on content: can a
   v1.6.1 binary on a synced folder still damage a v7 archive, or a v7 binary mis-stamp 6?
4. **Static cross-folder links (lens 5, 6).** The go-back review's F5/GB-6 rejected them for
   moves; are they safe for add-only membership?
5. **Restore's disjoint-keys assumption (lens 5).** What else in `tbengine` assumes a key is in
   one folder (counts, size, the All Mail note, the plan's binding)?
6. **The merge step (lens 3, 7).** Crash order, the marker, deletion only after the anchor
   save, fixity re-check before each delete, a backup started meanwhile, an encrypted archive.
7. **Detection (lens 1, 6).** Spoofed `[Gmail]` folders, a profile with two Gmail accounts, a
   renamed account directory, a store whose detection changes after archiving.
8. **Desktop counts and words (lens 4).** Folder counts that add up to more than the total; the
   merge offer's wording; what the person sees when a member is gone.
9. **Proportionality (lens 2).** Is a per-member `Moz` field worth it, given every copy on the
   owner's machine was identical including those lines? (The design keeps it because a read in
   one folder changes them.)
10. **Index without a version bump (lens 8).** An older Desktop reader opening an index that has
    `doc_folders` rows it does not read: degraded (one folder per message) but not damaging?
