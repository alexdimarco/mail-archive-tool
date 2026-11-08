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

*Superseded 2026-11-07 by the status line below. The review was filed as
`docs/review-gmail-dedup-10lens.md` (the caller's name), not `review-gmail-dedup-predesign.md`
(GD-24).*

**BUILD STATUS (2026-11-07, after the review): reviewed, conditions folded, pre-code.** The 10-lens
pre-code review is `docs/review-gmail-dedup-10lens.md` (commit ff7fc90, verdict GO WITH CONDITIONS,
GD-1 to GD-26). Every HIGH and MED condition (GD-1 to GD-16, GD-25, GD-26) is folded below as a dated
note beside the text it amends; every LOW is folded or recorded with its reason (§0). The reviewed text
above each note is kept as it was. The lead's calls and the owner's defaults are in §0. The largest
change: version 1 merges **only byte-identical copies**, so §4.4's split and rebuild and the per-folder
bookkeeping lines (`Moz`) are dropped (Q-L1), and R20 and R40 are untouched. Still owed before slice
work starts: the review's re-run of lenses 3, 6 and 10 on this folded text (the review, header), and
an independent fold-verify of this file (the lead arranges both). Each slice's own gate is in §13's fold.

> **Folded 2026-11-07 [dedup foldcheck F1 to F4, S1 to S9] (lead's call):** the independent fold check
> is filed as `docs/review-gmail-dedup-foldcheck.md` (commit 92a1173, verdict FAIL (narrow): on an
> encrypted archive the merge step's copies are swept at the next backup without the re-check). The
> lead's calls on its must-fix list are folded below as dated notes tagged `[dedup foldcheck Fn]` or
> `[dedup foldcheck Sn]` beside the text they amend; the text they amend is kept, and a sentence they
> replace is marked *superseded* where it stands. The largest change (F1): **version 1's merge step
> (slice D2) is for plaintext archives only**; an encrypted Gmail archive gets listed memberships and
> capture-time de-duplication but is never offered the merge step, and "the merge step for encrypted
> archives" is a future design with its own review (§9 fold-check note). Still owed: the fold check's
> re-check of F1 to F4 only. **B1 may start now** (the fold check, §7: none of F1 to F4 touches it);
> B2 waits for the F1 and F3(b) folds' re-check, B3 for F3(a), D1 for F4, D2 for F1 and F2. The
> crash-at-every-step re-read of the plaintext merge, with these folds, is §18.

> **Folded 2026-11-07 [dedup foldcheck2 M1 to M3, D2, B3] (lead's call):** the re-check of F1 to F4 is
> filed as `docs/review-gmail-dedup-foldcheck2.md` (commit 082a340, verdict FAIL (narrow), for slice D2
> only; B2, B3 and D1 may proceed). Its must-fix items M1 to M3 and its D2 and B3 should-fix items are
> folded below as dated notes tagged `[dedup foldcheck2 …]`, with every sentence they replace marked
> where it stands. The main note is at the end of §9; the schema is in §7.1; B3 is in §7.4; the rows are
> in §12; §20 re-reads §18 and the new un-merge steps against a crash after each step. **D2 waits for
> a re-check of M1 to M3 only.** B2's text-only item (close the `LoadedVersion` loose end) was not
> among the lead's calls and is not folded here.

**Ids are provisional.** The completeness design takes MA-1170 to MA-1194, R42 to R44 and S75
to S77; another agent takes MA-1151 and up. This design takes **MA-1210 to MA-1234**, **R45**
and **S78 to S84**. Whoever merges second renumbers.

> **Folded 2026-11-07 [ids] (lead's call):** this design keeps MA-1210 to MA-1234, R45 and S78 to
> S84, and the folds add **MA-1235 to MA-1244** (§12 fold). Checked on 2026-11-07: main's catalog
> ends at MA-1150; `polish-161`'s working copy uses MA-1151 to MA-1159; `tb-completeness` uses
> MA-1170 to MA-1202, R42 to R44 and S75 to S77 (its design, commit 31bd018, names this design's
> range as reserved); no file in main's `docs/` or in any worktree uses MA-1203 to MA-1209 or
> MA-1235 and up, and no catalog uses R45 or S78 to S84. Catalog rows are not added to
> `docs/scenario-catalog.md` yet: the covers-map meta-test fails on a U/S row with no covering test,
> so each row is registered with its encoder in the slice that builds it (§13 fold).

## 0. Rulings folded on 2026-11-07

**Lead's calls (2026-11-07):**

- **Q-L1 (GD-4, GD-23):** version 1 merges **only byte-identical copies**. §4.4's split and rebuild
  of Thunderbird's `X-Mozilla-*` lines is dropped, with `NormSHA256`, `Moz` and `CopySHA`. R20 and R40
  are untouched. Owner question Q-A is moot. Before adding the split in a later version, a counts-only
  probe measures copies that differ only in those lines.
- **Q-L2 (GD-7, GD-13, GD-20):** per-folder "gone" and "no longer in this folder since" are
  **deferred** to the later per-folder history design. In version 1 a membership keeps only its
  first-seen time and is never marked gone (§7.2 fold).
- **Q-L3 (GD-11c):** the Desktop copy export writes a message into **every chosen folder it belongs
  to**, like restore, counted per folder written.
- **Q-L4 (GD-17):** an index rebuild does not recover memberships from anything but the manifest;
  a membership the manifest lacks returns at the next backup. The help text says so (§8.2 fold).
- **Q-L5 (GD-6):** the byte-position step **stays**: Thunderbird's `storeToken` is the byte offset of
  the copy's `From ` line, so the about 1,036 Sent Mail and Business Papers rows whose Message-ID
  matches several All Mail copies get placed (§5.3 fold).
- **GD-20:** duplicates (**S-dup**) and missing-folder membership (**S-membership**) are separately
  releasable slices on a shared base (§13 fold).
- **GD-1:** the merge step proves a copy from the **archived file** (checked against its recorded
  fixity), never from the live mbox, and re-checks both files just before each deletion (§9 fold).
- **GD-2:** the merge's first durable act is a manifest save at version 7 carrying a
  `MergePending` field (§9 fold).
- **GD-3:** no plain-text marker file. The pending list is sealed in the manifest of an encrypted
  archive (§9 fold).
- **GD-25:** **no proof, no merge.** Records whose originals cannot prove they are copies stay
  separate and are counted (§9 fold).
- **Gmail detection:** ONE shared function with the completeness design, using **its** rule: the
  account's `is_gmail` setting only, `prefs.js` read through `util.ReadRegularFile`, the account
  matched by its store folder, not its host (completeness design C0c, MA-1195). The All Mail flag is
  **not** used to detect Gmail. Where this design needs All Mail itself, All Mail is the one folder
  with the All Mail flag (0x8000) only, never the Archive flag (0x4000), which Thunderbird's own
  "Archives" folder carries (lead's call, from the completeness fold check
  `docs/review-thunderbird-completeness-foldcheck.md`, branch `tb-completeness`, commit 92b80f8).
  The difference the completeness design flagged (its §3 fold and §17 item 16) is resolved in §6's
  fold.

**Owner defaults** (the owner has the review's recommendations; these are built, and each
alternative must stay a small change):

- **Q-B (GD-12):** at an existing archive's first backup with the new version, MailArchive asks
  **once**, in the Desktop and in a terminal, before switching it to version 7: "Show Sent Mail and
  your other Gmail folders in full, and store each email once? Every computer that backs up into this
  archive then needs MailArchive 1.7 or later." A **new** archive is on by default (§7.4 fold).
- **Q-C (GD-12):** no way back to the older format in version 1. The release notes say so.
- **Q2:** Gmail accounts only.
- **Q3:** a copy's files are deleted only when the archive's own originals prove it is a copy.
- **Q1:** keep the existing archive (`Mail-archive-dimarcotech`) and offer the merge step behind a
  shown plan.

**Where each condition is folded:**

| Id | Sev | Folded in |
|---|---|---|
| GD-1 | HIGH | §9 fold (proof, `MergedFiles` entries, re-check, contained deleter) |
| GD-2 | HIGH | §9 fold (crash order), §7.4 fold |
| GD-3 | HIGH | §9 fold, §5.2 fold, §8.1 fold |
| GD-4 | HIGH | §4.4 fold (Q-L1), §3 fold, §8.5 and §8.6 folds |
| GD-5 | MED | §8.1 fold (alias path) |
| GD-6 | MED | §5.3 fold, §5.4 fold |
| GD-7 | MED | §7.2 fold (dissolved by Q-L2) |
| GD-8 | MED | §5.2 fold |
| GD-9 | MED | §6 fold |
| GD-10 | MED | §3 fold (G7), §7.4 fold |
| GD-11 | MED | §8.5, §8.6 folds, §8.3 fold (copy export, `/goback`) |
| GD-12 | MED | §7.1 and §7.4 folds |
| GD-13 | MED | §7.2 fold (state table) |
| GD-14 | MED | §8.1 fold (carry forward) |
| GD-15 | MED | word-list fold, §1 fold, §3 fold, §7.3 fold, §10 fold, §11 fold, §15 fold |
| GD-16 | MED | §12 fold (R45 clause, R3, R6), §8.7 fold |
| GD-17 | LOW | §3 fold (G9), §8.2 fold (Q-L4), §8.7 fold |
| GD-18 | LOW | §8.3, §8.4, §8.6, §9 folds, §7.4 fold |
| GD-19 | LOW | §5.2 fold (skip unchanged), §8.2 fold, §8.7 fold, §8.8 fold, §9 fold |
| GD-20 | LOW | §13 fold (Q-L2 for the history half) |
| GD-21 | LOW | §8.4 fold |
| GD-22 | LOW | §8.8 fold |
| GD-23 | LOW | recorded, moot: §4.4 fold (Q-L1 drops the split) |
| GD-24 | LOW | the status line, §2.2 fold, §13 fold |
| GD-25 | MED | §9 fold |
| GD-26 | MED | §5.3 fold, §9 fold |

§17 records the re-read of these folds for contradictions.

> **Folded 2026-11-07 [dedup foldcheck] (lead's call): where the fold check's items are folded.**
>
> | Item | Folded in |
> |---|---|
> | F1 (encrypted loss through the start sweep) | §9 fold-check note (plaintext only; the future design's list), §7.1 fold-check note (`Unit` not written), §7.4 fold-check note (the words), §8.9 fold-check note, §12 fold-check note (MA-1229, MA-1230, new MA-1245), §13 fold-check note, §15 fold-check note |
> | F2 (a stale plan) | §9 fold-check note (the yes binds checksums; recompute under the lock), §12 fold-check note (MA-1238) |
> | F3 (a version stamped without a yes) | §7.4 fold-check note (both splits, the stamp sites), §8.2 fold-check note, §12 fold-check note (MA-1216, MA-1243) |
> | F4 (a fill rewriting a home original) | §4.4 fold-check note, §5.3 fold-check note, §9 fold-check note (the survivor), §8.1 pointer, §12 fold-check note (MA-1213) |
> | S1, S2, S3, S4, S8 | §9 fold-check note (slice D2); S2 also §7.1 fold-check note |
> | S5 | §5.2 fold-check note (slice M1) |
> | S6 | §5.3 fold-check note (text) |
> | S7 | §6 fold-check note (slice B1) |
> | S9 | §6 fold-check note, §17 fold-check note (text) |

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

> **Folded 2026-11-07 [GD-15] (review condition) and [Q-L1] (lead's call):**
>
> - "Not part of the email anyone sent" is **false for 4,208 copies** (All Mail 3,947, Important 261)
>   whose header holds a second bookkeeping pair that arrived with the message (review §6). The
>   lines are usually Thunderbird's, not always.
> - "Normalized bytes" are **not used in version 1** (Q-L1): only byte-identical copies merge.
> - New word, **original bytes** (also "the reader's form"): a copy's bytes as MailArchive's mbox
>   reader gives them. The reader drops the `From ` separator line, makes every line ending CRLF,
>   takes one `>` off a body line starting `>From `, and drops the blank line before the next
>   separator (go-mbox v1.0.4 `reader.go:45-75`, per the review §1.1). The archived `.eml` is exactly
>   these bytes (`exporter.go:515`). Everywhere this design says "byte-identical" or "byte for byte",
>   it means in this form, never the mbox file's own bytes. Both copies go through the same
>   transform, so the merge rule is no weaker for it.
> - New word, **class**: the records of one store and one identity whose original bytes have the
>   same SHA-256 (§4.4 fold). The class's **survivor** is chosen by the §9 fold's rule.

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

> **Folded 2026-11-07 [GD-15] (review condition):** two figures are now measured (review §6, read-only,
> counts only):
>
> - "No mail is lost" is **proven for the four folders with one exception**: one Sent Mail row's
>   Message-ID is in no mbox on this computer (a completeness matter, left to that design).
> - "79,200 extra records" counts only copies spread across folders. Counting repeats inside one
>   folder too, there are **79,439**. The 34.5 GB figure is unchanged: it is the 73,915 byte-identical
>   extra records only (§11 fold).
> - Separately releasable (GD-20): "A first, B straight after" stays the order, but each ships on its
>   own (§13 fold).

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

> **Folded 2026-11-07 [GD-24] (review condition): citations corrected** (review §1.2). The reviewed
> text above stands; these replace its loose citations.
>
> - `exporter.go:207-311` is the key-placement part of `Export`, which runs to `:817`; the seen-record
>   path is `:313-376` and the write-folder choice `:404-408`.
> - "only the wizard calls it": `cmd/mailarchive/main.go:2121` calls `FindAccountForStore` too.
> - The `AlsoFiles` field is `manifest.go:136-141`; `:399-404` is `CollapseLoss`'s comment.
> - `internal/app/ws_archive.go:448-499` (§8.3) is `internal/desktop/ws_archive.go:448-499`.
> - The page's `statusLine` starts at `html.go:2104`.
> - Side finding, kept for the build: `ContentHash` is set on every write, local records included
>   (`exporter.go:716`); its comment at `manifest.go:121-123` ("Empty … on folder-scoped local records")
>   is out of date.
> - **Field-name collision found while folding (2026-11-07):** `raw_sha256` is already a record field,
>   `RawDigest` (`manifest.go:190-200`), carried only by an unparsed stub, and Load drops it from every
>   other record. This design's new digest therefore has its own name, `gm_sha256` (§7.1 fold), and
>   never reuses `RawDigest`.
>
> **Folded 2026-11-07 [GD-9e] (review condition): the backup now reads `prefs.js`.** Today the run
> never reads it (above). With this design (and with the completeness design) the backup reads it
> once per run, read-only, through `util.ReadRegularFile` (§6 fold). That read is stated beside R9
> (reading changes nothing) and R25 (contained, no link followed) in §12's fold, so no test that pins
> "the run reads no `prefs.js`" is misread (presync's MA-562 is about the presync run, not the backup).

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

> **Folded 2026-11-07 (properties, as amended).** G1, G2 and G5 stand. The others read as follows in
> version 1.
>
> - **G2 [Q-L1, Q-L2, GD-15b]:** a membership records the folder, its kind (copy or listed) and when
>   it was first seen there. It keeps no bookkeeping lines, no copy checksum, no last-seen time and
>   no "gone" flag. The home folder is not stored as a membership: the record's own `Folder` names it.
> - **G3 [Q-L1, GD-4] replaced:** a copy is merged into a record **only when its original bytes have
>   the same SHA-256 as the record's** (§4.4 fold). The record's stored original is then, byte for byte
>   in the reader's form, that folder's copy as it was when merged. Any other copy is archived as its
>   own record, as today. Nothing is rebuilt.
> - **G4 [GD-6e]:** the bound is restated in §5.4's fold: a wrong listed membership needs a reused
>   Message-ID **and** an empty Gmail id on the row or an unknown or stale byte-position chain for the
>   other email. It still never changes or removes a stored byte.
> - **G6 [GD-4, Q-L1]:** extract and restore write the record's **stored original** into every member
>   folder, copy and listed alike. That is a preserved original, so **R20 and R40 hold as worded**. For
>   a copy member it equals that folder's copy when it was merged. A later Full re-capture replaces
>   the stored original with the home folder's current copy, as Full does today for every record
>   (§8.1 fold, GD-14). A listed member gets the stored original, which carries the home copy's read
>   and tag marks; the plan says so (§8.6 fold).
> - **G7 [GD-10] narrowed:** v1.6.1 and earlier **refuse** a version-7 archive's backup, verify, Desktop
>   folder list and copy export, and change nothing. Their `serve`, `search` and `status` **do not
>   refuse**: they open the search index read-only with no version check (`index.go:233-256`) and never
>   load the manifest, so they show each email in its home folder only (a near-empty Sent Mail again)
>   and write nothing. The release notes say every computer that uses the archive needs 1.7 or later.
> - **G8 [GD-1, GD-25]:** nothing already archived is deleted without a shown plan and a yes, and only
>   when the copy's and the survivor's **archived** originals both match their recorded fixity and each
>   other, checked at plan time and again just before the deletion. A group without that proof is not
>   merged at all (§9 fold).
> - **G9 (new) [GD-17]: one stored copy now serves every folder.** Today a damaged All Mail `.eml` can
>   still be found in INBOX's copy. After a merge, one original serves every folder, and folder
>   membership lives only in the manifest (and the regenerated folder pages). Verify's words name every
>   folder a damaged original belongs to (§8.7 fold).

## 4. Problem A: knowing two copies are the same email

### 4.1 The candidates

| Signal | Verdict |
|---|---|
| Message-ID header | **Lookup only.** In the owner's All Mail, 4,846 Message-IDs appear more than once, and in 253 of them the bodies differ (different emails). It can also be missing. Never enough to merge. |
| Gmail's `X-GM-MSGID` | **Not used to merge** (§4.2). Used only to place listed memberships when the Message-ID is ambiguous (§5.3). |
| SHA-256 of a copy's raw bytes | **Not enough alone**: Thunderbird's lines can differ per folder (§4.3). |
| SHA-256 of the normalized bytes | **The merge test**, with the rebuild check (§4.4). |
| `ContentDigest` (`message.go:376-406`) | **Not enough**: covers no attachment bytes and few headers. |

> **Folded 2026-11-07 [Q-L1] (lead's call):** in version 1 the merge test is the third row's SHA-256
> of the copy's original bytes (the reader's form), **alone**: copies that differ only in Thunderbird's
> lines are not merged. The fourth row (normalized bytes) is not used (§4.4 fold).

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

> **Folded 2026-11-07 [Q-L1] (lead's call) and [GD-4] (review condition): the rule in version 1.**
> Steps 1 to 4 above (split, normalize, rebuild) are **dropped**. GD-23 (the split rule's wording) is
> therefore moot and recorded only; MA-1210 and MA-1211 are re-scoped (§12 fold). The rule is:
>
> For a copy read from folder F of a Gmail store that the person has switched on (§7.4 fold), with
> original bytes `B` (the reader's form, the word-list fold):
>
> 1. **Digest.** `d = SHA-256(B)`.
> 2. **Candidates.** The records of the same store token with the same identity (`KeysForIdentity`,
>    filtered by token), each with a **digest**: its `Fixity.EML` SHA-256 when it has one (the `.eml` is
>    exactly `m.Raw`, `exporter.go:515`), else its `gm_sha256` (§7.1 fold), else none. A record with no
>    digest, an unparsed stub, or a record carrying any gap label is never a candidate.
> 3. **Match.** The candidates whose digest equals `d` form one class (§0 word list). None → step 5.
> 4. **Merge.** Add a copy membership F to the class's survivor (the §9 fold's rule), with `FirstSeen` = this
>    run; write no file. If F already has a listed membership on the survivor, it becomes a copy
>    membership (§7.2 fold). If F is already the survivor's home or a member, nothing changes.
> 5. **No match.** Archive the copy as its own record, as today. A Gmail record written without `-raw`
>    stores `gm_sha256 = d`, so later copies can match it.
>
> Never merged, as before: a copy with a read gap (the KEEP guard runs first, `exporter.go:227-233`),
> an unparsed stub, a copy with no raw bytes. A home record with a gap is never a candidate, so a
> member folder's healthy copy never "heals" a damaged home record across folders: it becomes its own
> record, as today, and `status` counts it (GD-5e).
>
> Cost of a wrong answer (§4.5): a false merge is now impossible short of a SHA-256 collision; a missed
> match costs a second record, as today. On the owner's data, 79,153 of 79,156 live extra copies are
> byte-identical to All Mail's, Thunderbird's lines included (§10.1). The 4,208 copies whose second
> bookkeeping pair came with the message are no special case: they are compared whole.

> **Folded 2026-11-07 [dedup foldcheck F4] (lead's call): a record missing pieces is never a target.**
> Step 2's "a record carrying any gap label is never a candidate" did not cover a **fillable** record:
> `Fillable()` is `len(Missing) > 0` (`internal/state/manifest.go:286`), which is not a label. A
> fillable home record could then take a copy membership, and the alias hit's unchanged fill retry
> would rewrite its `.eml` from a member folder's **current** copy (`exporter.go:443-445`, `:514-519`),
> matched only by envelope fingerprint. Step 2 now reads, in place of its last sentence:
>
> - **A candidate must be complete:** `Complete()` is true (`manifest.go:289`: nothing in `Missing`,
>   the unknown sentinel included, and nothing in `Terminal`), it carries no capture-gap label, it has
>   a digest, and it is not an unparsed stub. A record missing any piece, fillable or not, is never a
>   capture-time target, never a listed-membership target (§5.3 fold-check note) and never the merge
>   step's survivor (§9 fold-check note).
> - So the alias path never meets an incomplete record (its set holds only copy memberships, and those
>   are placed only on complete records), and a member folder's copy can never reach a fill retry.
> - A copy that matches only incomplete records goes to step 5: its own record, as today, and `status`
>   counts it. This is what MA-1213's re-scope already asserts ("a fillable home record is not healed by
>   a member's healthy copy"), so the rule and its test now agree (§12 fold-check note).
> - A record that was complete when it took members and later becomes incomplete (a Full re-capture of
>   its home copy that hits a read gap) keeps its members (add-only), but the KEEP guard (`:227-233`)
>   already stops the degraded re-capture from replacing it; it takes no **new** members while
>   incomplete. **(Not traced in code: whether any path can mark a complete record fillable without a
>   write through the KEEP guard; the D1 adversarial pass checks it.)**

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

> **Folded 2026-11-07 [GD-8] (review condition): the reader's specification**, to be met before
> slice M1 starts. Measured shape it is designed against (review §6, the owner's profile, counts only):
> bare-CR line endings; a dictionary id often followed by a line break and spaces before its `=`
> (453,875 times in All Mail); 162,978 groups, none aborted or open; 584,041 rows opened with `[-` and
> 98,145 tables cleared with `{-`; nesting depth at most 2; 572,356 dictionaries, the largest 519,998
> entries; 1,809,764 values; the longest value 1,083 bytes in a dictionary and 29,016 in a cell; the
> file changing while Thunderbird runs.
>
> - **(a) Grammar subset.** Read: comments, dictionaries (`<…>`, including the column dictionary
>   `(a=c)`), atoms, rows (`[…]`, with `[-` meaning "replace this row's cells"), tables (`{…}`, with
>   `{-` meaning "clear and replace"), cell updates, row removals, and groups (`@$${…{@ … @$$}…}@`). Escapes:
>   `$xx` (two hex digits) and `\` (the next byte literal, and a line continuation at end of line).
>   Line endings CR, LF or CRLF. An aborted group (`@$$}~abort~…`) and an unclosed last group are
>   ignored whole. **Any other form refuses the file**: nothing added, one note; never a guess. The
>   exact meaning of each form is pinned from Thunderbird's Mork parser source when M1 is built and
>   recorded in that slice's commit **(today the forms' meaning is the reviewers' recollection,
>   unverified)**.
> - **(b) Iterative, no recursion.** One explicit stack, nesting depth capped at 8 (measured 2).
> - **(c) Caps, each refusing the file when passed:** file size 1 GiB (largest measured 215 MB); groups
>   2,000,000; row openings 4,000,000; cells per row 512; dictionary entries in all 8,000,000; one value
>   1 MiB; a kept Message-ID or Gmail id 998 bytes (the header line limit); an id 16 hex digits; total
>   time 120 seconds per file and 10 minutes for the whole summary pass.
> - **(d) Memory: two passes.** Pass 1 reads the column dictionary and the rows, and records only the
>   atom ids that the Message-ID, `X-GM-MSGID`, `flags` and `storeToken` columns reference. Pass 2
>   reads the dictionaries and keeps only those atoms' values. Subjects, addresses and every other
>   value are read past and never held. Budget: 256 MiB per file, counted on what is kept; passing it
>   refuses the file.
> - **(e) Deleted rows.** A row whose flags carry Thunderbird's expunged bit (0x8) or IMAP-deleted bit
>   (0x200000) adds nothing **(recollection, unverified; M1 pins both from `nsMsgMessageFlags.idl`)**.
> - **(f) Stable reads only.** The file's size and modification time are taken before pass 1 and after
>   pass 2; if they differ, it is retried once, then "not read this run" (one note). A summary that
>   changed while read never adds anything.
> - **(g) Opening.** Through the contained opener, `util.OpenRegularFileIn` under the profile root
>   (R25): a link is refused, a pipe or device is never waited on. Read-only, sharing read and write so
>   Thunderbird is never blocked; if Thunderbird holds it exclusively on Windows, the open fails and
>   the file is "not read this run". Lab row MA-1232 gains a run with Thunderbird open.
> - **(h) Which files, which folders.** The pass lists `.msf` files itself, building each folder path
>   exactly as `mboxDir` does (`localwalk.go:537-547`, `.sbd` nesting) and applying the walk's leave-out
>   rules (R25 containment, dot names, R41 restore leftovers). Folders with a `.msf` and no mbox
>   (Promotions, Business Papers - Corporation) are included.
> - **(i) What it reports [GD-3].** Counts and the folder name only, through the existing capture-gap
>   path, which is sealed in an encrypted archive (`localwalk.go:114`; `CaptureGaps` in `View`). No
>   Message-ID or Gmail id is ever logged.
> - **(j) Fuzzing.** M1's gate fuzzes the reader with a corpus seeded from the throwaway test
>   account's real summary files (§12 lab story), never the owner's.
>
> **Folded 2026-11-07 [GD-19a] (review condition): skip an unchanged summary.** The manifest keeps,
> per (store, folder), the summary file's size and modification time at its last whole, stable read
> (`SummaryStamps`, §7.1 fold). A file whose size and time both match is not read again; its listed
> memberships are already in the manifest. The owner's account then reads about 285 MB only when
> Thunderbird has changed the files. The skip is **per store, all or nothing**: the byte-position step
> (§5.3 fold) needs the summaries of the folders whose copies made the records (All Mail's above all),
> so when any summary of the store changed, every summary of that store is read this run.
>
> **Folded 2026-11-07 [dedup foldcheck S5] (lead's call: as the fold check proposes; with slice M1).**
> (d)'s "records only the atom ids that the … columns reference" misses a cell that holds its value
> **literally** instead of by an atom reference. Pass 1 also keeps such literal values for the four
> columns (Message-ID, `X-GM-MSGID`, `flags`, `storeToken`), each within (c)'s 998-byte cap for an id
> and inside the same 256 MiB budget. The failure without this is safe (a missed placement), but M1's
> specification and MA-1218's fixtures include a literal cell for each of the four columns.

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

> **Folded 2026-11-07 [GD-6] (review condition), [Q-L5] (lead's call) and [GD-26] (review
> condition): the matching rule in version 1.** Steps 1 to 4 above are replaced by this. It runs only
> for a store the person has switched on (§7.4 fold).
>
> **When a folder's rows are used.** Folder F's summary rows are matched only when F's summary was
> read whole and stable this run (§5.2 fold f) **and** F's mbox walk completed this run or F has no mbox.
> A folder read partly (`folder-partially-read`) adds no listed membership: there, "no copy this run"
> means "not reached", not "absent" (GD-13). Rows flagged deleted, and rows whose Message-ID is
> Thunderbird's synthetic `md5:` form (one each in All Mail and Important), are skipped.
>
> **The byte-position table (Q-L5).** While the walk reads folder G's mbox, a counting reader placed
> **below** go-mbox (which exposes no position, `reader.go:78-87`) records, in order, the byte offset
> of each separator line that starts `From `, counted from the start of the file, before any BOM is
> skipped. The n-th offset belongs to the n-th message the library yields, so each offset maps to the
> record that copy became or merged into. The table is kept for this run only, in memory, never in the
> manifest (the review measured that Thunderbird's `storeToken` is exactly this offset: 6,803 of
> 6,803 on Starred). G's table is used only when (1) the number of offsets equals the number of
> messages the library yielded, and (2) G's mbox has the same size and modification time before the
> walk and after the summary pass. Otherwise G contributes nothing to the table, and is counted.
>
> **A class's known Gmail ids.** For each class (the word-list fold), `K(class)` is the set of
> `X-GM-MSGID` values on summary rows read this run whose (folder G, `storeToken`) maps through G's
> table to a copy that made or merged into a record of that class.
>
> **The rule,** for each usable row of F with Message-ID `M` and Gmail id `g` (possibly empty), where
> the class has neither F as home nor F as a member already:
>
> 1. **Candidates** = the classes of records of the same store with identity `mid:<M>`. The summary's
>    Message-ID has no angle brackets; the reader adds them as the identity spells them and compares
>    the whole string exactly, never case-folded (MA-1225).
> 2. **No candidate**: the body is on no folder of this computer; a completeness matter, counted only.
> 3. **`g` names a class**: some candidate class has `g` in its `K` → listed membership F on **that**
>    class's survivor. This is what places the about 1,036 rows whose Message-ID matches several All
>    Mail copies (review §6).
> 4. **Exactly one candidate class, and `g` is empty or that class's `K` is empty** → listed
>    membership F on its survivor (GD-6a, c). INBOX's 10,796 rows with an empty Gmail id are placed this
>    way or not at all.
> 5. **Anything else** (one candidate whose `K` is known and lacks `g`, which is a different email; or
>    several candidates and `g` names none) → **nothing added, counted**: "N messages Thunderbird lists
>    in Sent Mail could not be placed" (counts and the folder name only, §5.2 fold i).
>
> **On an archive not yet merged (GD-26).** The owner's archive holds two to six records per email
> until the merge step runs. Grouping by class makes "one candidate" mean one email, not one record,
> and the membership goes on the class's **would-be survivor** by the §9 fold's rule (most complete,
> then the record already holding memberships, then earliest `ExportedAt`, then lowest key). Because
> the record holding memberships wins over age, the survivor never changes once a membership is
> placed, and the merge step keeps it, so it moves nothing. *[The sentence "Because the record holding
> memberships wins … so it moves nothing" is superseded 2026-11-07 by [dedup foldcheck S6]: see the
> note at the end of this section.]*
> MA-1220 gains an unmerged fixture with three records per email; MA-1240 is the full case.
>
> A `g` that appears on rows of two different classes is a damaged or hostile summary (Gmail ids are
> unique per account, **recollection, unverified**): every row with that `g` adds nothing and is counted.
>
> **Folded 2026-11-07 [dedup foldcheck S6] (lead's call: as the fold check proposes; text).** "The
> survivor never changes once a membership is placed" is too strong. §17 item 6's wording is kept: the
> record already holding memberships wins over age, so in the usual case the survivor does not change;
> but completeness ranks first, so if a fill later makes another record of the class more complete, that
> record becomes the survivor and the merge step's anchor save moves the memberships to it, metadata
> only, in the same save. No file moves and no byte changes either way: the class's records are
> byte-identical by recorded digest, re-checked at the plan.
>
> **Folded 2026-11-07 [dedup foldcheck F4] (lead's call): listed memberships go only on a complete
> record.** A class's would-be survivor must be complete (§4.4 fold-check note). Because completeness
> ranks first in the survivor rule, the would-be survivor is complete whenever any record of the class
> is. If **no** record of the class is complete, the row adds nothing this run and is counted with the
> unplaced rows ("could not be placed"); it is placed at a later whole read of F's summary once a record
> of the class is complete. The unchanged-summary skip (§5.2 fold) is not changed for this: the row
> waits for Thunderbird's next change to that store's summaries. A listed membership writes no file, so
> this is a choice of consistency with the lead's rule, not a byte-safety need.

### 5.4 What a wrong answer costs

| Case | Cost |
|---|---|
| Wrong listed membership (a reused Message-ID placed on the wrong email) | That email shows in one extra folder. Bounded by step 3: needs a broken `X-GM-MSGID` chain AND a reused Message-ID. |
| Missed listed membership | The message is missing from that folder in the archive, as today. Counted: "N messages Thunderbird lists in Sent Mail could not be placed." |
| Hostile summary file | Contained, capped, fail-closed (§5.2). The worst outcome is wrong folder labels on archived mail, never a write outside the archive or a changed byte. |

> **Folded 2026-11-07 [GD-6e] (review condition): the bound restated.** The first row's bound ("needs
> a broken `X-GM-MSGID` chain AND a reused Message-ID") was false: step 2 attached to a single record
> without looking at the Gmail id. Under §5.3's fold, a wrong listed membership needs **a reused
> Message-ID and either an empty Gmail id on the row or an unknown or stale byte-position chain for the
> other email** (its class's `K` empty). Its cost is unchanged: one email shown in one extra folder.
> If the lead later cuts the byte-position step, the status line says "1,036 messages Thunderbird lists
> in Sent Mail and one other folder could not be placed" (GD-6f); version 1 keeps it (Q-L5).

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

> **Folded 2026-11-07 [GD-9] (review condition) and [detection] (lead's call): one shared rule, the
> completeness design's.** The "Detection" bullet above is replaced. Its "or a folder of the account
> carries Thunderbird's All Mail flag" is **dropped**, and its "the existing contained reader (R25),
> with `thunderbird.FindAccountForStore`" was a wrong premise (`parsePrefs` is a plain `os.ReadFile`,
> `thunderbird.go:63-64`; the account pick is random with two accounts on one host, `:86-104`).
>
> - **The rule:** a store is Gmail when the account it belongs to has `mail.server.serverN.is_gmail`
>   set to true in `prefs.js`, and only then. Reason: Thunderbird's keep-one-copy rule runs only when
>   `is_gmail` is true (`GetIsGMailServer`, `nsImapMailFolder.cpp:8928-8935`, per the completeness
>   design's TC-1 fold); a non-Gmail server with an "all mail"-flagged folder still keeps a copy in
>   every folder. This is the completeness design's rule (its §3 fold, R43, MA-1175), so the two
>   features always agree. **The difference the completeness design flagged (its §17 item 16) is
>   resolved here, in its direction.**
> - **One function, one place.** A single function in `internal/thunderbird` answers both designs. It
>   reads `prefs.js` through `util.ReadRegularFile(path, 16 MB)` (`internal/util/openregular.go:67`: a
>   link is refused, a pipe or device is never waited on, a larger file is refused unread; the 16 MB cap
>   is the one the completeness design gives `folderCache.json`, the folding agent's call), parses
>   `is_gmail` along with the keys the completeness design reads, and resolves the account **by its
>   store folder**: `directory`/`directory-rel` compared by whole path components first, `hostname`
>   only when exactly one server has it, and no account when still ambiguous, never a guess
>   (completeness design C0c, MA-1195). The run reads `prefs.js` once and hands the matched account to
>   both features: no double reading of the profile (§16.10). Whichever design lands first builds the
>   function; the other reuses it. The completeness design names `util.ReadRegularFile` for
>   `folderCache.json` and `compatibility.ini` but not, in so many words, for `prefs.js`; this fold
>   names it, and the completeness design should cite the shared function when it next changes
>   (flagged for the lead). *[The sentence "The completeness design names `util.ReadRegularFile` for
>   `folderCache.json` … but not … for `prefs.js`" is superseded 2026-11-07 by [dedup foldcheck S9]:
>   see the note at the end of this section.]*
> - **Unreadable or unmatched.** `prefs.js` unreadable, refused, or no account matched → not Gmail this
>   run: the store is archived per folder, as today, with one note. Except **sticky**: a store already
>   in `DedupStores` stays switched on whatever this run's detection says; it never flips by itself.
> - **All Mail, where this design needs it** (lead's call, from the completeness fold check,
>   `tb-completeness` commit 92b80f8): the one folder of the account with the All Mail flag (0x8000)
>   only, never the Archive flag (0x4000), which Thunderbird's own "Archives" folder carries. If no
>   folder or more than one folder has 0x8000, All Mail is treated as absent. In version 1 nothing in
>   this design's merging or placing depends on All Mail: the byte-position step uses whichever
>   folders' copies made the records (§5.3 fold). All Mail is named only in words (restore's existing
>   All Mail note, the "This folder" link wording, §8.4 fold). This deliberately differs from the
>   completeness design's own TC-1 fold, which found All Mail by 0x8000 **or** 0x4000; the lead's fold
>   check settles it as 0x8000 only, for both. *[The sentence "This deliberately differs from the
>   completeness design's own TC-1 fold …" is superseded 2026-11-07 by [dedup foldcheck S9]: the two
>   designs now agree; see the note at the end of this section.]*
> - **Over-detection is bounded (area 5 of the review).** With `is_gmail` only, a non-Gmail server is
>   never treated as Gmail by a folder flag. If `is_gmail` were ever set wrongly, Problem A still merges
>   only byte-identical copies (safe) and Problem B places only by the §5.3 fold's rule (an extra folder
>   at worst). The words say "Gmail" because Thunderbird itself treats the account as Gmail.
>
> **Folded 2026-11-07 [dedup foldcheck S9] (lead's call: update the two notes; text).** Both notes
> above that claim a difference from the completeness design are out of date. The completeness design
> **agrees** at commit **58e2542** (branch `tb-completeness`, its fold of its own fold check's M1 to
> M10), read on 2026-11-07:
>
> - **`prefs.js` through `util.ReadRegularFile(path, 16 MB)`**: its `[foldcheck M1]` fold ("C0c also
>   reads `prefs.js` safely", and C0c "moves `parsePrefs` onto `util.ReadRegularFile`", with MA-1195's
>   pipe and link).
> - **All Mail is the one folder with 0x8000 only**: its `[foldcheck M5]` fold marks its "0x8000 **or**
>   0x4000" paragraph superseded; a folder with only 0x4000 never stands in, and none or several
>   0x8000 folders means All Mail is absent (MA-1176).
>
> Small differences that remain, none a contradiction (the fold check's §5): with no matched account
> the completeness design answers `unknown` and this design "not Gmail, per folder, one note" (both
> mean "do not apply the Gmail rule"); MA-1175, MA-1195 and MA-1235 test parts of one function, so
> whoever builds it says in the commit which row owns which part; the completeness design still cites
> this design at 9203aae (its to update).
>
> **Folded 2026-11-07 [dedup foldcheck S7] (lead's call: as the fold check proposes; with slice B1).**
> The All Mail flag (0x8000) is read from **`folderCache.json`**, by the same shared function, opened
> with `util.ReadRegularFile` and the 16 MB cap the completeness design gives that file. Nothing in this
> design's merging or placing uses it in version 1; only the words do ("This folder: All Mail", §8.4
> fold; restore's All Mail note). MA-1235's 0x8000 cases are run from a `folderCache.json` fixture.

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

> **Folded 2026-11-07 (schema, as amended) [Q-L1, Q-L2, GD-1c, GD-2, GD-12, GD-15b, GD-19a].** The
> block above is replaced by this one. The key format still does not change, and every new field is
> `omitempty`, so a manifest with no switched-on Gmail store stays byte-identical to today's (MA-329).
>
> ```go
> // Record
> GmailSHA256 string       `json:"gm_sha256,omitempty"`    // SHA-256 of the original bytes; only on a Gmail record with no Fixity.EML (§4.4 fold)
> Folders     []Membership `json:"folders,omitempty"`      // the folders other than the home one (Record.Folder)
> MergedFiles []MergedFile `json:"merged_files,omitempty"` // the merge step's files awaiting deletion (§9 fold)
>
> type Membership struct {
>     Folder    string    `json:"folder"`     // spelled as Record.Folder is
>     Kind      string    `json:"kind"`       // "copy" (read from F's mbox) or "listed" (F's summary file)
>     FirstSeen time.Time `json:"first_seen"` // set once, never changed (Q-L2)
> }
>
> type MergedFile struct {
>     Path   string `json:"path,omitempty"` // relative to -out (plaintext archive)
>     Unit   string `json:"unit,omitempty"` // the sealed unit (encrypted archive)
>     Role   string `json:"role"`           // "eml", "html" or "zip"
>     Size   int64  `json:"size"`           // copied from the copy's Fixity at step 3
>     SHA256 string `json:"sha256"`         // copied from the copy's Fixity at step 3
>     Proof  string `json:"proof"`          // the survivor's Fixity.EML SHA-256 the copy's .eml equalled
> }
>
> // Manifest
> DedupStores   map[string]string       `json:"dedup_stores,omitempty"`   // token -> "gmail-1": the person's yes, or a new archive
> MergePending  *MergePending           `json:"merge_pending,omitempty"`  // the merge step in progress (§9 fold)
> SummaryStamps map[string]SummaryStamp `json:"summary_stamps,omitempty"` // token\x00folder -> size and mtime at the last whole read
>
> type MergePending struct {
>     PlanDigest string       `json:"plan_digest"` // SHA-256 of the plan the person said yes to
>     Groups     []MergeGroup `json:"groups"`
> }
> type MergeGroup struct {
>     Survivor string   `json:"survivor"` // record key
>     Copies   []string `json:"copies"`   // record keys
> }
> type SummaryStamp struct {
>     Size    int64     `json:"size"`
>     ModTime time.Time `json:"mtime"`
> }
> ```
>
> - **Dropped** from the reviewed block: `NormSHA256`, `Moz`, `CopySHA` (Q-L1); `LastSeen`, `Gone`
>   (Q-L2); `Offset` (kept for the run only, in memory, §5.3 fold). The home folder is not stored as a
>   membership (GD-15b).
> - **`gm_sha256`, not `raw_sha256`:** the name `raw_sha256` is already `RawDigest`, an unparsed stub's
>   field that Load drops from every other record (`manifest.go:190-200`; §2.2 fold).
> - **Alias index (GD-5a):** built at load over **copy** memberships only, mapping
>   `(token, folder, identity)` to a **set** of `{key, fingerprint}`, the shape `byIdent` already has
>   (`manifest.go:383-397`). Listed memberships are not in it: a copy arriving in a listed folder takes
>   the miss path and upgrades the membership (§4.4 fold step 4).
> - **Load validation (MA-1217):** every membership has a clean relative folder path that is not the
>   record's own `Folder`, a known `Kind`, and no duplicate folder on one record; every `MergedFile` has a
>   clean relative path or a well-formed unit id, a known role, a size of 0 or more and well-formed
>   digests; `gm_sha256` is 64 lowercase hex. A bad entry is dropped and logged, never the record. A
>   Microsoft 365 record's `Folders` are dropped (§8.10). *[The `MergedFile` clause of this bullet is
>   superseded 2026-11-07 by [dedup foldcheck F1] and [dedup foldcheck S2]: see the note at the end of
>   this section.]*
> - **Sealed slots (GD-12b, rescue N1).** Today the sealed save commits only
>   `View{Version, Entries, Stores, CaptureGaps, CollapsedTokens}` (`manifest.go:744-750`, `:2156-2157`)
>   and `FromSealed` restores only those (`:755-767`), so any other manifest field is silently lost at
>   every save of an encrypted archive. `DedupStores`, `MergePending` and `SummaryStamps` therefore
>   gain slots in `state.View`, `FromSealed`, `save()` and the sealed header (`container/record.go:39-51`).
>   Record fields (`Folders`, `gm_sha256`, `MergedFiles`) ride inside the sealed record already
>   (`record.go:61-64`). A round trip through an encrypted archive is a named test (MA-1237) and part
>   of slice B2's gate.
> - **Size (GD-15b, estimate).** A membership as above is about 80 bytes. For the owner after the merge:
>   about 79,400 copy and 34,800 listed memberships (about 9 MB) and no `gm_sha256` (the archive has
>   `-raw`), against about 56 MB of removed records: about **175 MB, down from 220.7 MB**. This is an
>   estimate from the review's measured 705.7 bytes per record (review §6); MA-1233 measures it.

> **Folded 2026-11-07 [dedup foldcheck F1] and [dedup foldcheck S2] (lead's calls): `MergedFile` in
> version 1.**
>
> - **Plaintext only (F1).** Version 1 writes `MergedFiles` and `MergePending` only on a plaintext
>   archive (§9 fold-check note). The `Unit` field of `MergedFile` is **not written** in version 1. A
>   unit entry needs the unit's kind, its revision and its sealed digest, copied from the copy's
>   `SealedSet`, because a unit's file name is derived as `units.Path(unit, kind, rev)`; that is the
>   future encrypted merge design's to add (the fold check's F1(a)). That design must also make an
>   archive holding such an entry a **newer format** (a higher `FormatVersion`), so 1.7 refuses it
>   whole rather than dropping the entry at load and leaving the unit for the start sweep.
> - **A sealed manifest holding a merge (F1).** If an encrypted archive's sealed manifest holds a
>   `MergePending` or any `MergedFiles` entry (it cannot in version 1: conversion refuses mid-merge, §9
>   fold-check note S3, and step 6 empties both), 1.7 refuses the archive by name and changes nothing:
>   "This encrypted archive holds an unfinished merge that this MailArchive cannot finish. Nothing was
>   changed." The sealed slot for `MergePending` (MA-1237) stays, so such a manifest is never silently
>   emptied by a save.
> - **Load validation of `MergedFile`, in place of the clause marked above:** a clean relative `Path`
>   (required), a known role, a size of 0 or more, and `proof` 64 lowercase hex. `sha256` is 64
>   lowercase hex for the `eml` role. For the **`html` and `zip` roles only**, an empty `sha256` is
>   allowed (a page or zip with no recorded digest, S2) and the entry is **kept**: dropping it would
>   leave its file owned by nothing (GD-25's failure). A `MergedFile` with a clean `Path` that fails
>   any other check is **not dropped** either: it is moved to the record's `AlsoFiles` at load (owned,
>   never deleted by the merge step, reported by verify as kept) and logged, **together with every other
>   entry of that record that shares its file stem** (the same copy's page, zip and `.eml`), so a page
>   is never deleted while its zip is kept (§9 fold-check note S1, all or none). *[The move to
>   `AlsoFiles` in this sentence is superseded 2026-11-07 by [dedup foldcheck2 M2] whenever
>   `MergePending` holds that copy's pre-merge record: the copy is un-merged instead; see the note
>   below.]* Only an entry whose `Path`
>   is not a clean relative path is dropped and logged; the contained deleter could never have reached
>   it. *(The folding agent's call: stricter than the reviewed "dropped and logged", for `MergedFiles`
>   only; a bad membership is still dropped, since dropping one loses no file.)*

> **Folded 2026-11-07 [dedup foldcheck2 M2, M3, D2] (lead's calls): what un-merging needs, in the
> schema.** The `MergeGroup` above gains three fields, and `Record` gains one. All are `omitempty` and
> plaintext-only in version 1, like `MergePending` itself.
>
> ```go
> type MergeGroup struct {
>     Survivor        string            `json:"survivor"`                   // record key
>     Copies          []string          `json:"copies"`                     // record keys
>     Before          map[string]Record `json:"before,omitempty"`           // each copy's whole record at 5.1, by key (D2)
>     SurvivorFolders []Membership      `json:"survivor_folders,omitempty"` // the survivor's Folders before step 3 (D2)
>     Unmerged        []string          `json:"unmerged,omitempty"`         // copies already put back; never re-merged (D2)
> }
>
> // Record
> RebuildOwed bool `json:"rebuild_owed,omitempty"` // put back by an un-merge whose page or zip could not be rebuilt (M3)
> ```
>
> - **`Before`** is written at step 5.1, before the anchor save, and only step 5.6 clears it (with the
>   rest of `MergePending`). A record in `Before` passes the same checks a loaded record passes
>   (`finish`, `state/manifest.go:775`; for example the clean-path check) before it is put back. One that
>   fails is never put back and never guessed at (§9 fold-check-2 note; left open in §20).
> - **`SurvivorFolders`** lets an un-merge take off only the memberships that copy brought: step 3 moved
>   the copy's home folder **and** the copy's own memberships onto the survivor, so "its own folder"
>   (§19) is not enough.
> - **`RebuildOwed`** is the mark M3 asks for. No existing mark does the job: `reindex`'s redaction
>   keys only on an index row whose page is gone (`app/reindex.go:135-142`, `:174-185`) and reads no
>   `Missing`, `Terminal` or other label. `Missing` is not used (§9 fold-check-2 note, M3, says why).
>   `FormatVersion` counts `RebuildOwed` (7), so the reflection test (§7.4 fold a) has a decision for it.
> - **In place of the move marked above (M2):** a `MergedFile` with a clean `Path` that fails any other
>   check, when `MergePending` holds its copy's record in `Before` (matched by file stem against that
>   record's `Path`), is **kept as it is** in `MergedFiles`, with its stem siblings, marked in memory as
>   never to be deleted, and its **whole group** is marked for un-merge. Load writes nothing; every
>   writer but `merge-copies -resume` refuses while `MergePending` is set (S3), and the resume un-merges
>   the group first, so the files go back to their own restored records. Only when no `Before` record
>   matches does the entry move to `AlsoFiles` as the marked sentence says. 1.7 never writes such a
>   manifest (5.1 writes `Before` for every copy before 5.3 writes any entry), so that means a manifest
>   changed by hand (§20, the one exception).

### 7.2 Per-folder history

`FirstSeen`, `LastSeen` and `Gone` are the per-folder history, written in place; no history log is
added for local archives in version 1 (the Microsoft 365 log, `internal/state/history.go`, is
unchanged). A copy membership is gone when a complete walk of F did not see it; a listed membership
when F's summary was read whole and no longer lists it. The same guards as `SweepGone`
(`manifest.go:1696-1719`): never at a checkpoint, never for a folder not walked or a summary not read,
nothing when the walk did not complete. The record and files always stay. The reader can say "In
Receipts since 1 Mar 2024" or "Not in Receipts since 10 Nov 2026". A date slider is a later design (Q5).

> **Folded 2026-11-07 [Q-L2] (lead's call), [GD-7] and [GD-13] (review conditions): no per-folder
> history in version 1; the membership states.** `LastSeen` and `Gone` are deferred, with "no longer
> in this folder since", to the later per-folder history design (the design's Q5 moves there). A
> membership keeps only `FirstSeen`, written once. This **dissolves GD-7**: a steady run changes no
> membership, so the sealed manifest's rule that a steady run re-seals no bucket (`record.go:53-57`)
> still holds. Version 1 is **add-only**: a membership is never removed because a folder stopped
> showing the message. The reader may still say "In Receipts since 1 Mar 2024" from `FirstSeen`.
>
> Every transition of one membership F of record R (in a switched-on Gmail store):
>
> | Event | What happens to F |
> |---|---|
> | A copy in F is byte-identical to R's class (§4.4 fold) | copy membership added; or a listed F becomes copy |
> | A copy in F differs from every class | that copy becomes its own record in F, as today; a listed F on R stays (F may then list the email twice, as today for differing copies) |
> | F's summary places the message on R (§5.3 fold), F walked whole or with no mbox, summary stable | listed membership added |
> | F read partly (`folder-partially-read`) this run | no listed membership added for F this run |
> | F's summary unreadable, refused, or changed while read (after one retry) | nothing added or removed; one note |
> | F no longer shows the copy, or F's summary no longer lists it | nothing (add-only; deferred with Q-L2) |
> | The merge step folds copy record C into survivor R | C's home folder becomes a copy membership on R (first seen = C's `ExportedAt`), and C's memberships move to R |
> | R is redacted or its archive deleted | R and every membership go (§8.8 fold) |
> | Load finds F malformed | F dropped and logged; R kept |
> | A Full re-capture, a fill or a heal of R | F carried forward unchanged (§8.1 fold, GD-14) |
>
> Since nothing reproduces a folder's copy except the record's stored original (Q-L1), there is no
> per-membership data to lose (GD-13's first rule holds trivially).

### 7.3 Size of the new data

About 250 bytes per extra copy membership and about 120 per listed one. For the owner: about
79,200 copy memberships (20 MB) and 34,800 listed ones (4 MB), replacing 79,200 records of roughly
0.6 to 1 KB each. The manifest gets smaller.

> **Folded 2026-11-07 [GD-15b] (review condition):** as written (a membership on every copy including
> home, with `norm_sha256` and `Moz`), the manifest would **grow** from 220.7 MB to about 280 MB (review
> §6). "The manifest gets smaller" holds only for the amended schema (§7.1 fold): no home membership,
> no `norm_sha256`, no `Moz`, no `LastSeen`; estimated about 175 MB after the merge step. Before the
> merge step (listed memberships only) it grows by about 3 MB.

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

> **Folded 2026-11-07 [GD-12] (review condition), [Q-B] and [Q-C] (owner defaults), [GD-10] and
> [GD-18c] (review conditions): one version function, and the switch asked once.**
>
> **(a) One function decides.** `state.FormatVersion(m) int` returns 7 when the manifest holds any of:
> a `DedupStores` entry, a `MergePending`, a `SummaryStamps` entry, or a record with `Folders`,
> `gm_sha256` or `MergedFiles`; otherwise 6. `save` stamps exactly what it returns, on the plain and
> the sealed path alike. A reflection test (MA-1216) lists every field of `Record` and `Manifest` and
> fails when a field is added without a decision in the function. The lead's call above (write 7 only
> when needed) stands, now over every new field. The search index uses the same pattern: version 4
> only when `doc_folders` holds a row, set in the same transaction as the first such row (MA-1243).
> *[This sentence and "`save` stamps exactly what it returns" are amended 2026-11-07 by [dedup
> foldcheck F3]: today's constants are also stamped elsewhere; see the note at the end of §7.4.]*
> Encrypted records decode strictly (`record.go:171-181`), so a version-7 record stamped 6 would make
> an older binary call the archive damaged rather than newer: one more reason for one function.
>
> **(b) Sealed saves carry the new manifest fields:** §7.1 fold (sealed slots, MA-1237).
>
> **(c) The switch is the person's, asked once (Q-B).** A switched-on store is a `DedupStores` entry,
> and nothing in §4.4, §5.3 or §9 runs for a store without one. So an existing archive stays version 6,
> and behaves exactly as today, until the person says yes.
>
> - **When it asks.** At an existing archive's first backup with this version (an archive with records
>   whose store is detected as Gmail and has no `DedupStores` entry), **once**, in the Desktop (on the
>   archive's card, before or after the run, not blocking it) and in a terminal (only when stdin and
>   stdout are both a terminal):
>
>   > Show Sent Mail and your other Gmail folders in full, and store each email once? Every computer
>   > that backs up into this archive then needs MailArchive 1.7 or later.
>
>   with **Yes** and **Not now**. A scheduled or unattended run never asks and never switches; it backs
>   up as today, and the card asks the next time the person opens it.
> - **Yes** → `DedupStores[token] = "gmail-1"`, saved; the archive is version 7 from that save on.
>   Merging of new copies and filling in folders start at the next backup. The merge step for existing
>   copies is a separate offer behind a shown plan (§9 fold).
> - **Not now** → nothing is written into the archive (a field there would make it version 7, or be
>   dropped by an older binary's save). The answer is kept in this computer's settings for that archive
>   (its job file), so this computer does not ask again by itself; Settings keeps the same question
>   available. Scripted use: `mailarchive gmail-folders -out DIR` asks the same question, and `-yes`
>   answers it (the folding agent's call; the friction review checks the name).
> - **A new archive** (no records yet) is switched on by default at its first run, with the one setup
>   and Settings line of the design's Q6: "Gmail: each email is stored once and shown in all of its
>   folders." An existing archive that gains its first Gmail account later is asked, as above, because
>   the switch would change its version.
> - If only one of the two slices ships (§13 fold), the question names only what that slice does: "Show
>   Sent Mail and your other Gmail folders in full?" or "Store each Gmail email once?", with the same
>   second sentence.
>
> **(d) No way back (Q-C).** Version 1 has no command that writes a version-7 archive back as version 6.
> The release notes say so. (Such a command would drop the new fields: v1.6.1 would then see one record
> per email in its home folder and recapture the other folders' copies as new records; more files,
> nothing lost. Not built.)
>
> **(e) Older readers (GD-10).** v1.6.1's backup, verify, Desktop folder list and copy export refuse a
> version-7 archive by name (`manifest.go:784-786`; `listfolder.go:187-189`), changing nothing. Its
> `serve`, `search` and `status` read a version-4 index with no version check (`index.go:233-256`) and
> show each email in its home folder only, writing nothing. Accepted; no change to old readers (GD-10c
> is not needed). The release notes say every computer that uses the archive needs 1.7 or later.
>
> **(f) The other computer's words (GD-18c).** On a computer still running an older MailArchive the
> refusal and the failing schedule can only use that version's words. From 1.7 on, a refusal of a
> newer format says: "This archive needs a newer MailArchive on this computer." The 1.7 release notes
> and the help page say: "After you say yes, every computer that backs up into this archive needs
> MailArchive 1.7 or later; an older one stops backing up into it (its scheduled backups fail, then
> pause) and changes nothing." This matters for the owner: the archive sits in a Nextcloud-synced
> folder (review §6).

> **Folded 2026-11-07 [dedup foldcheck F3] (lead's call): "highest this build can read" and "version
> to write" are two things, for the manifest and for the search index.** Today one constant means both
> in each place, so bumping it as (a) says would switch every archive on its first 1.7 open, without
> the person's yes. Version 1 separates them:
>
> - **The search index (before slice B3).** Two values: the **highest version this build can read**
>   (4) and the **key format** (3). The refusal of a newer index (`index.go:162-164`) compares with the
>   highest readable (4). *[Amended 2026-11-07 by [dedup foldcheck2 B3]: so do the three other
>   newer-index refusals, `memory.go:76`, `lazy.go:47` and `listfolder.go:187`; see the note below.]*
>   `RepairKeys` compares the stored version with the **key format** (3) at
>   `index.go:804`, so a version-3 index needs no repair. Both stamping sites write **3**, and write
>   **4 only when `doc_folders` holds a row**:
>   - the fresh create, `resolveIndexVersion` (`index.go:218`, and the value it returns at `:221`),
>     which has no `doc_folders` row by definition, so always 3;
>   - `RepairKeys`'s stamp (`index.go:874`, and `ix.version` at `:881`): 4 if the key's repair runs on
>     an index that has a `doc_folders` row, otherwise 3.
>
>   The only other way to version 4 is the transaction that writes the first `doc_folders` row (the
>   fold above, §8.2 fold), which happens only in a switched-on store. So an archive that has not
>   switched keeps a version-3 index through every 1.7 backup, a `reindex -rebuild` and a fresh index.
> - **The manifest (before slice B2).** `ManifestVersion` becomes the **highest version this build can
>   read** (7). It is what the refusals compare with: `manifest.go:784-786`, the sealed header's probe
>   (`container/record.go:168`) and the archive card's reader (`archindex/recognise.go:432`). **Only
>   `FormatVersion(m)` decides what is written.** The three places that write the constant today must
>   not write 7:
>   - `newManifest` (`manifest.go:701`, `Version` and `LoadedVersion`): `FormatVersion` of an empty
>     manifest, which is 6;
>   - the load-time bump (`manifest.go:933-934`): bumps to `FormatVersion(m)`, never to the constant;
>   - a fresh sealed record (`container/store.go:166`, `View{Version: state.ManifestVersion}`):
>     `FormatVersion` of the empty manifest, 6.
>
>   And `save` (`manifest.go:2155`, `m.Version = manifestVersion` today) stamps `FormatVersion(m)`, as
>   (a) says.
> - **What this keeps.** A computer still on 1.6.1 keeps backing up an archive that has not switched:
>   its manifest stays 6 and its index 3 however often 1.7 opens, backs up, verifies or rebuilds it.
>   Only the person's yes (or a new archive's first run) makes it 7, and only the first `doc_folders`
>   row makes the index 4. **(Not traced: every comparison with `LoadedVersion` elsewhere in the code;
>   B2's builder lists them in the commit, and each must keep meaning "the version on disk".)**
> - MA-1216 and MA-1243 gain the tests (§12 fold-check note).
>
> **Folded 2026-11-07 [dedup foldcheck2 B3] (lead's call): all four newer-index refusals compare with
> the highest readable (4), never with the write version.** Read in the code on 2026-11-07: besides
> `index.go:162-164`, three more opens refuse a newer index, and each compares with the same constant
> today (`version > indexVersion`):
>
> - `internal/index/memory.go:76`, the sealed index opened in memory;
> - `internal/index/lazy.go:47`, the sealed index opened lazily (in memory or in a scratch file);
> - `internal/index/listfolder.go:187`, the read-only folder lister.
>
> All four compare with the **highest readable** (4). If any compared with the key format (3), 1.7
> would refuse its own version-4 sealed index, or its own folder list, as soon as the first
> `doc_folders` row lands. Their words ("this build understands %d") print the highest readable. MA-1243
> gains an encrypted case (§12 fold-check-2 note).
>
> **Folded 2026-11-07 [dedup foldcheck F1] (lead's call): the words on an encrypted archive.** An
> encrypted Gmail archive is never offered the merge step (§9 fold-check note), so (c)'s question
> must not promise what the archive will not do to the copies it already holds. On an encrypted
> archive that already has records, the question gains one sentence:
>
> > Show Sent Mail and your other Gmail folders in full, and store each email once? Every computer
> > that backs up into this archive then needs MailArchive 1.7 or later. Emails this archive already
> > holds more than once stay as they are, because it is encrypted.
>
> A new encrypted archive (no records yet) keeps the setup line as written; it has no earlier copies.

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

> **Folded 2026-11-07 [GD-5] (review condition): the alias path.** The first bullet's "(2) alias hit
> … stamp `LastSeen` and skip" was not today's skip: a key hit on a seen record also runs the fill
> retry, the stale-stub and legacy re-examination, `supersedePartial` and `StampParseScheme`, and Full
> mode re-exports it under the **current** folder (`exporter.go:313-376`, `:404-408`). Replaced by:
>
> - **(a)** After a plain key miss in a switched-on Gmail store, look up the alias set for
>   `(token, F, identity)` and keep the entries whose fingerprint matches (`sameFingerprint`).
> - **(b)** Exactly one → set `key` to that record's key and run the **existing seen-record path
>   unchanged** (fill retry, stale-stub and legacy re-examination, `supersedePartial`,
>   `StampParseScheme`). None or several → a miss: go to §4.4's fold.
> - **(c)** A seen Gmail record is always written under its **home** folder, mirroring the Microsoft
>   365 `FirstFolder` rule (`:404-408`). Full mode re-writes a merged record only when it meets the
>   home folder's copy; meeting a member folder's copy does nothing (there is no `LastSeen` to stamp,
>   Q-L2).
> - **(d)** The KEEP guard (`:227-233`) runs on the alias key too, so a degraded member copy never
>   displaces a complete home record.
> - **(e)** No cross-folder heal: a home record with a gap is never a merge candidate (§4.4 fold), so a
>   member folder's healthy copy becomes its own record, as today, counted. MA-1214 gains a Full-mode
>   re-run and this case. *[Amended 2026-11-07 by [dedup foldcheck F4]: "a home record with a gap"
>   means any record that is not complete, fillable records included (§4.4 fold-check note).]*
> - The alias's fingerprint test has the same within-folder limit as today's plain-key test (review §3,
>   B4): the design's §16.4 worry is refuted.
>
> **Folded 2026-11-07 [GD-14] (review condition): carry the new fields forward.** The exporter builds
> a fresh record on every write and carries `AlsoFiles` forward only on the Microsoft 365 path
> (`exporter.go:763-765`). A Full run, a fill or a heal of a Gmail record carries `Folders`,
> `MergedFiles` and `AlsoFiles` forward unchanged, and sets `gm_sha256` from the new bytes when the
> record has no `Fixity.EML`. If a Full re-capture changes the home copy's bytes (Thunderbird rewrote
> its read mark), the stored original becomes the home folder's current copy, as Full does today, and
> copy members keep pointing at it (§3 fold, G6). MA-1242.
>
> **Folded 2026-11-07 [GD-3] (review condition) and the folding agent's call: where the counts go.**
> On a plaintext archive the counts line above goes to `status` and the run summary, counts only, never
> a folder name. On an **encrypted** archive the counts and any folder-naming note go only into the
> sealed `RunDetails` and the capture-gap path, and are shown on screen while the archive is unlocked:
> nothing about Gmail folders is written in plain text beside it. This is stricter than the review's
> "counts only in plain text", to match the completeness design's owner default O1. The numbers in the
> line are the owner's as of 2026-11-07 (79,439 counting repeats in one folder, §1 fold).

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

> **Folded 2026-11-07 [GD-19d, e] (review condition): the table's shape.** `doc_folders(key, store,
> folder)` holds the **non-home** members only (the home folder stays in `docs.folder`; decided once
> here). Indexed on `(store, folder)` and on `key`. A folder filter is `docs.folder = ? OR EXISTS
> (SELECT 1 FROM doc_folders WHERE key = docs.key AND store = ? AND folder = ?)`; facet counts are
> `COUNT(DISTINCT key)` over the union of the two; one message is one hit. Redaction's `DeleteByID`
> (`reindex.go:181`), `Add`'s replace (`index.go:431-458`) and every other row delete also delete the
> key's `doc_folders` rows, in the same transaction. Index version 4 is set in the transaction that
> writes the first `doc_folders` row (§7.4 fold a). MA-1243. *[Amended 2026-11-07 by [dedup foldcheck
> F3]: that transaction, and `RepairKeys` on an index with such a row, are the only ways to version 4;
> a fresh create and every other repair stamp 3 (§7.4 fold-check note).]*
>
> **Folded 2026-11-07 [Q-L4] (lead's call) and [GD-17]:** `reindex -rebuild` takes members from the
> manifest, as the last bullet says, and from nothing else: it does not recover memberships from the
> folder pages. A membership the manifest lacks (a manifest restored from an older copy, a membership
> dropped by load validation) returns at the next backup, from Thunderbird, while the message is still
> in that folder there (as a separate record if that folder's copy has changed since). The help page's
> rebuild entry says so.

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

> **Folded 2026-11-07 [Q-L2], [GD-18b] and [GD-11c, d] (lead's calls and review conditions).**
>
> - The reader's "(no longer in Receipts since 10 Nov 2026)" is **deferred** with Q-L2. The reader shows
>   "In: All Mail, INBOX, Sent Mail, Important" (home first, then members in folder-tree order).
> - **Back link (GD-18b).** A message opened from a folder's list returns to that folder, never to the
>   home folder.
> - **Desktop copy export (Q-L3, GD-11c).** `internal/desktop/ws_archive.go`, `app.OpenArchive` and
>   `PlanCopyExport` read folders too (the design did not list them). A message chosen through two or
>   more folders is copied **into every chosen folder it belongs to**, counted per folder written; for
>   every member, `Fetch` returns the record's stored original (§3 fold, G6). MA-1241.
> - **`/goback` (GD-11d).** It lists local records too, grouped by `rec.Folder` (`goback.go:144-152`,
>   `:237-238`). Unchanged in version 1: it shows each Gmail email under its home folder only; Gmail
>   archives have no go-back timeline (§15 "not claimed"). MA-1241 pins it.
> - **Restore's `KeepsTimeline` (GD-11e)** flips once any record has a non-zero `Record.LastSeen`
>   (`restoreread.go:279-283`). Memberships have no `LastSeen` and never stamp `Record.LastSeen`, so it
>   does not flip. MA-1241 pins it.

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

> **Folded 2026-11-07 [GD-21] (review condition): three limits on the cross-folder links,** which the
> review accepts (the go-back objection does not carry over: folder pages are a regenerated view, R28,
> and the link target never moves, R13):
>
> - **(a)** A link is built only from the record's validated `Path`, always inside the same store
>   directory (R4), never from a membership's folder string.
> - **(b)** The encrypted reader's in-memory pages (`keyring/reader.go:560`) resolve the same relative
>   link.
> - **(c)** A copy export or unencrypted copy of a subset of folders either brings the home files with
>   it (Q-L3: the message is copied into every chosen folder it belongs to) or, if the home folder is
>   not chosen and its files are not brought, drops the link and shows the message's name without one.
>
> **Folded 2026-11-07 [GD-18b] (review condition):** the frozen message page's "This folder" link
> names the home folder in words ("This folder: All Mail"), so a message reached from Sent Mail does
> not silently lead to All Mail.

### 8.5 Extract (mbox and eml)

- Today one output folder per record's `Path` directory; `Record.Folder` unused
  (`internal/app/extract.go:129`, `:273-287`, `:413`, `:452`).
- Change: a record is written into every member's folder: a copy member's rebuilt copy, checked
  against `CopySHA` (a mismatch is skipped and listed, R20); a listed member gets the stored original.
  Without `-raw` there is nothing to extract, as today. `-format eml` writes one file per member folder.

> **Folded 2026-11-07 [GD-4] (dissolved by Q-L1) and [GD-11b] (review condition).** "A copy member's
> rebuilt copy, checked against `CopySHA`" is gone: **every member folder, copy or listed, gets the
> record's stored original**, which is what R20 already allows ("only each record's PRESERVED original
> bytes"). The stored original is checked against its own `Fixity.EML` as today; a mismatch is skipped
> and listed, once per record, naming every folder it would have gone into (R20).
>
> - One walk item per (folder, record), built **before** the sort, so each member folder gets one mbox
>   open as today.
> - A member folder's output directory comes from the **same function** that spells the home folder's,
>   never from the membership's folder string directly.
> - With `-format eml`, two records whose file stems meet in one member folder get a deterministic
>   suffix (`-2`, `-3` in key order), never an overwrite.
> - `Emitted` counts files written, one per (folder, record).
>
> MA-1226 (re-scoped, §12 fold) and MA-1241.

### 8.6 Restore to Thunderbird

- Today `SelectForRestore` keeps a key in its first node only, so `RestoreFolder.Keys` are disjoint
  (`internal/app/restoreread.go:221-231`, `:260`, `:315`); the plan copies them
  (`tbengine/plan.go:336-339`); the run reads each original (`tbengine/run.go:216-245`) and appends it
  (`internal/tbrestore/write.go:387-408`).
- Change: a key goes in each chosen member folder; each gets its rebuilt copy (copy member) or the
  stored original (listed member). Counts and size are per folder written. R40 holds. The All Mail
  note stays (`plan.go:603`, `words.go:73`). A restored Sent Mail now holds the sent mail.

> **Folded 2026-11-07 [GD-4] (dissolved by Q-L1), [GD-11a] and [GD-18d] (review conditions).**
>
> - "Its rebuilt copy (copy member)" is gone: every chosen member folder gets the **stored original**.
>   "R40 holds" is now true as R40 is worded ("it writes only preserved originals"); the design's first
>   claim of it was false (review GD-4).
> - **Counts (GD-11a).** `SelectForRestore` and `RestorableCounts` count per (folder, key), not per key.
>   The promise "the counts add up to what a whole-archive plan restores" (`restoreread.go:403-407`) is
>   restated: they add up to the messages written, one per folder written. `plan.go:372`'s not-restored
>   count is per folder written, so it no longer mixes per-record and per-folder numbers. `res.Damaged`
>   counts a record once, naming its folders. The space check sums the bytes per folder written.
> - **Words (GD-18d).** For a folder that is a listed member, the plan says: "Sent Mail gets the copy
>   MailArchive kept, with the read and tag marks of the folder it was saved from (All Mail)." The home
>   folder's name is filled in.
> - MA-1227 and MA-1241.

### 8.7 verify

- Today `owned` comes from each `Path` stem; other archived-type files are `unexpected`
  (`internal/app/verify.go:194`, `:316-323`, `:436`, `:615`).
- Change: `MergedFiles` are owned while they wait. With an original present, verify rebuilds each
  copy member and compares it with `CopySHA`; a mismatch is `modified` and names the folder.
- Side note: `AlsoFiles` (Microsoft 365 collapse losers) are not owned today and show as
  `unexpected` (`manifest.go:399-404`).

> **Folded 2026-11-07 [Q-L1], [GD-16e], [GD-17] and [GD-19c] (lead's call and review conditions).**
>
> - "Verify rebuilds each copy member and compares it with `CopySHA`" is gone (Q-L1): there is
>   nothing per member to rebuild. Verify checks each record's files once, as today, however many
>   folders it serves (GD-19c).
> - **Ownership (GD-16e).** Verify owns `MergedFiles` entries while they wait, and **`AlsoFiles`**: the
>   existing gap (`verify.go:314-321`; the field is `manifest.go:136-141`) is fixed in the same slice, so
>   a Microsoft 365 collapse loser and a merge-step file kept after a mismatch (§9 fold) are reported as
>   kept, never `unexpected`. This changes verify's output on Microsoft 365 archives that have
>   `AlsoFiles` (fewer `unexpected` lines); MA-1244 has a Microsoft 365 positive twin.
>   *[“a merge-step file kept after a mismatch” is superseded 2026-11-07 by [dedup foldcheck2 M1]: a
>   copy that fails its check is un-merged and its files belong to its own record, never to
>   `AlsoFiles`. The merge step's only `AlsoFiles` file is now one from a copy that passed its check but
>   whose removal failed (§9 fold-check-2 note).]*
> - **Words (GD-17).** A damaged original of a Gmail record names every folder it belongs to: "This
>   email's saved copy is damaged. It is shown in All Mail, INBOX and Sent Mail."

### 8.8 Delete and reindex redaction

- Redaction deletes `Path`'s files and every `AlsoFiles` entry (`reindex.go:158`, `:176-178`); change:
  also `MergedFiles`. Deleting a Gmail message's files removes it from every folder (it is one email);
  the reader and docs say so.
- Archive delete lists `Path` plus `AlsoFiles` (`internal/archdelete/lister.go:205`, `:882`) and folders
  from `rec.Folder` (`:833`); change: add `MergedFiles` and the members' folders.

> **Folded 2026-11-07 [GD-22], [GD-19e] and [GD-1c] (review conditions).**
>
> - **No tombstone (GD-22).** "Deleting a Gmail message's files removes it from every folder" lasts
>   only until the next backup while Thunderbird still holds the message, as today for any message
>   (`manifest.Delete(g.key)`, `reindex.go:184`, keeps no tombstone). The reader and docs say so: "If
>   the email is still in Thunderbird, the next backup saves it again."
> - Redaction also removes the key's `doc_folders` rows (§8.2 fold).
> - `MergedFiles` entries are now `{path or unit, role, size, sha256, proof}` (§7.1 fold); redaction and
>   archive delete read the path or unit from them.

### 8.9 Encrypted archives

The whole record is sealed (`container/record.go:61-64`); files are opaque units with no folder tree
(`internal/export/units.go:23-26`, `:64-73`; `exporter.go:410-433`). Members are inside the sealed
record. A merge or a listed membership writes no unit; the merge step's deletions use the deferred
unit deletion (`exporter.go:155-160`). The summary files are read from the Thunderbird profile, not
the archive, so nothing new is written in plain text.

> **Folded 2026-11-07 [GD-3], [GD-7] and [GD-12b] (review conditions).** "Nothing new is written in
> plain text" was true except for the merge marker and any note naming a folder (GD-3). Both are
> closed: there is no marker file (§9 fold; the pending list is `MergePending`, sealed), and the summary
> pass's notes and the Gmail counts go through the sealed capture-gap path and `RunDetails` (§5.2 fold
> i, §8.1 fold). `DedupStores`, `MergePending` and `SummaryStamps` have sealed slots (§7.1 fold, MA-1237).
> Bucket churn (GD-7) is dissolved by Q-L2 (§7.2 fold). Encrypted originals carry a plaintext
> `Fixity.EML` digest (`exporter.go:503-512`), so the merge step's proof works the same way: the unit is
> opened and its plaintext hashed (§9 fold). MA-1230 asserts that no folder name or Message-ID appears
> in any plain file beside the archive after each merge step and after a summary pass.
> *[In both paragraphs of §8.9, every sentence about the merge step on an encrypted archive ("the merge
> step's deletions use the deferred unit deletion"; "so the merge step's proof works the same way")
> is superseded 2026-11-07 by [dedup foldcheck F1]: see the note below.]*
>
> **Folded 2026-11-07 [dedup foldcheck F1] (lead's call): no merge step on an encrypted archive in
> version 1.** The fold check read why the reviewed plan loses bytes there (its §2): after the anchor
> save the copies' units are named by no record, so the sealed save lists them "retained"
> (`container/store.go:978-989`, the live list built only from records' `Sealed` sets,
> `app/encrypted.go:279-304`), and the next encrypted run's start sweep removes them
> (`app/encrypted.go:58-73`; `export/units.go:313-330`, `:353`) without the re-check. That includes a
> unit kept because it did **not** match its survivor. And the "deferred unit deletion"
> (`exporter.go:155-160`) deletes nothing on a sealed archive (`exporter.go:983-990`). So in version 1:
>
> - **What an encrypted Gmail archive gets:** listed memberships (S-membership) and capture-time
>   de-duplication (a new identical copy becomes a member of an existing complete record). Both live
>   inside the sealed record. **Neither writes a unit nor removes a record**, so the set of units the
>   records name, which is what the live list and the start sweep both read, is exactly what it would
>   have been without this design: the sweep can remove nothing it would not remove today.
> - **What it does not get:** the merge step. Copies archived before the yes stay as their own records
>   with their own units. The card never shows the offer, and `merge-copies` refuses (§9 fold-check
>   note). MA-1245 proves both, and that the sweep deletes nothing new.
> - **The merge step for encrypted archives** is recorded as a future design needing its own 10-lens
>   review (§9 fold-check note lists what it must solve).

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

> **Folded 2026-11-07: the merge step in version 1** [GD-1, GD-2, GD-3, GD-25 (lead's calls); GD-26,
> GD-18a, GD-19b (review conditions); Q1, Q3 (owner defaults)]. Steps 1 to 6 above are replaced by
> these. It runs only on a switched-on store (§7.4 fold c).
>
> **1. No evidence backup (GD-1).** The reviewed step 1 (stamping proof from "the copy it reads
> anyway", the live mbox copy) is **dropped**: Thunderbird rewrites a copy's read mark in place, so the
> live copy can differ from the archived one, and proving from it could delete the very file the
> archive held. The live copy may help group; it never proves.
>
> **2. Listed memberships** start at the first backup after the person's yes (Q-B), not "without
> asking" (the reviewed step 2 and Q4 are amended by Q-B). On an unmerged archive they go on each
> class's would-be survivor (§5.3 fold, GD-26).
>
> **3. The offer (GD-18a).** Once, on the archive's card, after the yes and only when the plan below
> would free space:
>
> > Keep one copy of each Gmail email and free about 34.5 GB? Every folder still shows it.
>
> with **Review** and **Not now**. Terminal: `mailarchive merge-copies -out DIR -plan` (writes nothing),
> then `mailarchive merge-copies -out DIR` (asks, shows the plan's totals, needs a typed yes).
>
> **4. The plan (writes nothing; GD-1a, b, e; GD-25; GD-19b).**
>
> - Groups: the records of one switched-on store and one identity whose `Fixity.EML` SHA-256 is equal.
> - **The proof, per group:** every record's **archived** original (the `.eml`, or the sealed unit's
>   plaintext) is read and hashed, and must equal its own recorded `Fixity.EML`; the digests are equal by
>   grouping; each record also passes its recorded `Fixity` for its page and zip when it has them.
> - **No proof, no merge (GD-25).** A record with no recorded `Fixity.EML` (made without `-raw`, or
>   before fixity), or whose archived original fails its fixity, is **not merged at all**: it stays its
>   own record with its own files, and is counted: "N copies kept apart: the archive has no saved
>   original to prove them." An archive made without `-raw` is offered nothing; the card says why.
> - **The survivor** is the most complete record (no gap label the others lack), then the record that
>   already holds memberships (so listed and copy memberships placed earlier never move, GD-26), then
>   the earliest `ExportedAt`, then the lowest key. Its files never move. *[Amended 2026-11-07 by
>   [dedup foldcheck F4]: the survivor must be complete; see the fold-check note at the end of §9.]*
> - The plan shows, per folder, how many copies merge and how much space is freed, and the total.
>   Reading every kept original of the owner's Gmail records is tens of GB **(time unmeasured on the
>   owner's disk; the plan shows the bytes to read before it starts)**, with progress and Cancel; Cancel
>   writes nothing.
> - The plan's digest (SHA-256 over its groups) is what the yes binds to. *[Superseded 2026-11-07 by
>   [dedup foldcheck F2]: the digest also covers each record's file checksums, and the plan is
>   recomputed under the lock at the yes; see the fold-check note at the end of §9.]*
>
> **5. The yes**, holding the archive lock (a scheduled run meanwhile refuses, as for verify). Order
> (GD-2):
>
> 1. **Pending, first.** Save the manifest with `MergePending{PlanDigest, Groups}`. Read back the saved
>    version: it must be 7 (it already is, from the yes; `FormatVersion` also counts `MergePending`). If
>    not, stop, change nothing more. From here every older binary refuses the archive (§7.4 fold e), with
>    or without an index (`-no-index` included). There is **no `.mailarchive-merge.json`** (GD-3): on an
>    encrypted archive `MergePending` is sealed with the manifest.
> 2. **Index.** In one transaction, made durable: drop the copies' rows; add `doc_folders` rows for each
>    survivor (the copies' home folders and the copies' memberships); version 4 if not already.
> 3. **Records (the anchor save).** Each survivor gains a copy membership for each copy's home folder
>    (none when that folder is the survivor's own, the about 239 repeats inside one folder, §10.2 fold)
>    and every membership the copies held; each copy's record is removed; its files become
>    `MergedFiles` entries on the survivor, `{path or unit, role, size, sha256}` **copied from the copy's
>    `Fixity`** and `proof` = the survivor's `Fixity.EML` SHA-256 (GD-1c). Saved.
> 4. **Pages.** The folder pages regenerate.
> 5. **Deletions (GD-1d, e, f).** For each survivor, **just before** deleting its waiting files:
>    re-hash the survivor's archived original and require it to equal its `Fixity.EML`; re-hash the
>    copy's `.eml` and require it to equal its entry's `sha256`; require the two digests to be equal
>    (`proof`). Only then delete the copy's `.eml`, and its page and zip with it; the page and zip are
>    deleted **as derivable from the survivor**, not as byte-identical (their own `sha256` must still
>    match, or they are kept). Deletion goes through the contained deleter (validated relative path, no
>    link followed), as redaction does (`reindex.go:159-166`); an encrypted archive's units go through the
>    deferred unit deletion (`exporter.go:155-160`). **Any mismatch keeps the file**: it moves from
>    `MergedFiles` to the survivor's `AlsoFiles` (owned by verify, reached by redaction, §8.7 fold) and is
>    reported: "N files were kept because they changed since the plan." *[In step 5, "Deletion goes
>    through the contained deleter …, as redaction does" is superseded by [dedup foldcheck S4], "an
>    encrypted archive's units go through the deferred unit deletion" by [dedup foldcheck F1], and the
>    order of deletion and a file already gone on resume are set by [dedup foldcheck S1] and [dedup
>    foldcheck S2]; see the fold-check note at the end of §9.]* *[Step 5's "it moves from `MergedFiles`
>    to the survivor's `AlsoFiles`" is superseded 2026-11-07 by [dedup foldcheck2 M1] for every copy
>    that is un-merged (a copy that fails its check, and every copy of a group whose survivor fails its
>    re-hash, M2): its files go back to its own restored record only; see the fold-check-2 note at the
>    end of §9.]*
> 6. **Done.** `MergedFiles` emptied and `MergePending` cleared; saved.
>
> **Crash and resume.** A crash at any step leaves `MergePending` in the saved manifest. Every 1.7
> backup, verify and Desktop open then refuses with "Finish the merge first", and `mailarchive
> merge-copies -out DIR -resume` (or Review on the card) finishes **forward from the saved manifest**:
> if any group's copy record still exists, redo step 2 (idempotent: dropping absent rows and adding
> present ones changes nothing) and then step 3; otherwise go on from step 4. Step 5 re-checks every
> file whatever happened before, so a file changed during the crash is kept. A file is deleted only
> after no saved record or index row names it as a record's own file (R28); `MergedFiles` and
> `AlsoFiles` keep it owned until then. MA-1229 gains: a crash at each of steps 5.1 to 5.6, then
> resume; and v1.6.1 opening the archive at each step, refusing (`assure.Refused`, side effect absent).
> *[Amended 2026-11-07 by [dedup foldcheck S3]: more writers than backup, verify and the Desktop open
> refuse while `MergePending` is set; see the fold-check note at the end of §9.]*
> *[Amended 2026-11-07 by [dedup foldcheck2 D2]: "if any group's copy record still exists, redo step 2
> … and then step 3" leaves out every copy whose key is in its group's `Unmerged` list, in both redos;
> see the fold-check-2 note at the end of §9.]*
>
> **6. Encrypted archives:** the same steps on the sealed manifest; units are read and hashed in memory
> and deleted through §8.9. MA-1230. *[Superseded 2026-11-07 by [dedup foldcheck F1]: version 1 has no
> merge step on an encrypted archive; see the fold-check note at the end of §9.]*
>
> **Gates (unchanged in substance):** the adversarial pass, the friction review of the offer and the
> plan (Windows first, on Edge, then Mac), and the owner's sign-off on the words, before slice D2 merges.

> **Folded 2026-11-07 [dedup foldcheck F1, F2, F4, S1, S2, S3, S4, S8] (lead's calls; the S items as
> the fold check proposes, with slice D2): the merge step, as amended.** The fold above stands except
> where these points replace a sentence (each is marked where it stands).
>
> **F1 — plaintext archives only.** Version 1's merge step (slice D2) runs **only on a plaintext
> archive**. On an encrypted archive the start sweep would remove the copies' units after the anchor
> save, without the re-check, including a unit kept because it did not match (§8.9 fold-check note).
>
> - An encrypted Gmail archive gets listed memberships and capture-time de-duplication (a new copy
>   becomes a member; no unit is written and none deleted) and is **never offered** the merge step.
> - **Desktop words.** Where a plaintext archive's card would show the offer, an encrypted archive's
>   card says once, with only **OK**: "This archive is encrypted, so the emails it already holds more
>   than once stay as they are. New copies are stored once from now on."
> - **Terminal words.** `mailarchive merge-copies -out DIR`, with or without `-plan` or `-resume`, on an
>   encrypted archive refuses before it takes any other step and changes nothing: "merge-copies does not
>   run on an encrypted archive yet. Nothing was changed. New copies of Gmail emails are already stored
>   once." (A typed refusal, tested with `assure.Refused`, MA-1245.)
> - **Future design, with its own 10-lens review: "the merge step for encrypted archives".** It must
>   solve the start sweep. At least: (a) waiting and kept units are in the sealed live list (the list a
>   save keeps, `unitFiles`, `app/encrypted.go:279-304`) **and** in the start sweep's named set (the
>   list it spares, `SweepUnits`, `export/units.go:313-330`), so nothing the merge step still owns is
>   ever "retained"; (b) each waiting entry is pinned to its unit's kind, revision and sealed digest,
>   copied from the copy's `SealedSet` (§7.1 fold-check note); (c) a copy that fails its check is
>   **un-merged**: its record put back from `MergePending` and its folder taken off the survivor, so its
>   own folder serves its own bytes; (d) the merge step deletes a unit itself, under the lock, right
>   after its re-check, never a later sweep; and `-resume` and every refused 1.7 run stop **before**
>   `prepareEncrypted`'s start sweep (`app/encrypted.go:58-73`); (e) the fold check's three MA-1230 cases
>   (a crash after the anchor save then a backup attempt then resume; a kept unit surviving two
>   backups; a damaged survivor keeping the copy's unit and folder). Until it is built, nothing in this
>   design writes `MergedFiles` or `MergePending` on an encrypted archive.
>
> **F2 — the yes binds the files, not only the names.**
>
> - The plan's digest is a SHA-256 over every group's **survivor key**, each record's **key**, and each
>   record's recorded **`Fixity.EML`, page and zip digests** (empty where none is recorded), in key order.
> - **At the yes, under the archive lock, before step 5.1,** the groups are recomputed from the saved
>   manifest (recorded digests only, no re-hashing). If the recomputed digest differs from the one shown,
>   the step stops and writes nothing: "The archive changed since you reviewed the plan. Review it
>   again." A backup that ran between the plan and the yes (a Full one rewriting a survivor, a fill, a new
>   record joining a group) is caught here.
> - **Step 3** (the anchor save, and its redo on resume) folds a copy only if its recorded `Fixity.EML`
>   still equals the survivor's; otherwise that copy stays its own record, with its own files, and is
>   counted. No writer can change them between 5.1 and step 3 (S3 below), so this is a second guard.
>
> **F4 — the survivor is complete.** In step 4's survivor rule, "the most complete record" now means:
> the survivor must be **complete** (`Complete()`, `manifest.go:289`, and no capture-gap label; §4.4
> fold-check note). A group with no complete record is **not merged**, and is counted: "N emails kept
> apart: none of their saved copies is complete." A record that is not complete may still be a **copy**
> folded into a complete survivor when the proof holds: its original equals the survivor's byte for
> byte, and its missing pieces are pages or attachments the survivor has. *(The folding agent's call,
> within the lead's rule, which names targets: flagged for the lead.)*
>
> **S1 — the order of deletion, and a file already gone on resume.**
>
> - For each copy, the `.eml` is re-checked first (step 5's three checks), then its **page and zip are
>   deleted before its `.eml`**: the **zip first, then the page, then the `.eml`**.
> - **All or none per copy** *(the folding agent's call, found in the §18 re-read)*. A copy's files are
>   deleted only when every one of its checks passes (its `.eml` proven; its page and zip each matching
>   their recorded digest, or with none recorded, S2). If any one fails, **all** of that copy's files are
>   kept and move to `AlsoFiles`. *[“and move to `AlsoFiles`” is superseded 2026-11-07 by [dedup
>   foldcheck2 M1]: the copy is un-merged, and its files go back to its own restored record only; see
>   the fold-check-2 note below.]* This replaces step 5's "(their own `sha256` must still match, or they
>   are kept)", which allowed a page to go while its zip stayed. Reason: a plaintext run's orphan-zip
>   sweep (`export.SweepOrphanZips`, called at `internal/app/run.go:770`) removes every zip whose sibling
>   `.html` is gone, and it runs **before** the manifest is loaded (`run.go:789`), so before any
>   `MergePending` refusal. A kept zip left without its page would be removed by the next backup
>   attempt. With the zip deleted first and all-or-none per copy, the merge step never leaves a zip
>   without its page. If a removal fails, that copy's remaining files are not removed, and its
>   remaining files move to `AlsoFiles`. *[Per [dedup foldcheck2 M1], 2026-11-07: this sentence stands
>   only for a copy that **passed** its check (its bytes equal the survivor's, which is shown as a
>   mail); for any copy that is un-merged it is superseded, and the files go back to the copy's own
>   record.]* (A zip whose page was already missing before the merge is swept today, with or
>   without this design.)
> - On resume, a waiting `.eml` that is **already gone**, whose entry `sha256` equals its `proof` and
>   whose survivor's original re-hashes to that same digest, counts as **deleted**, not kept: the
>   earlier attempt deleted it after its check. It is not moved to `AlsoFiles` and verify reports no
>   false "missing". If the survivor's re-hash fails, the entry is kept in `AlsoFiles` as missing, as
>   the fold above says. *[This last sentence is superseded 2026-11-07 by [dedup foldcheck2 M2]: a
>   survivor that fails its re-hash un-merges its group; a copy with nothing left on disk is the one
>   case not put back, and is not listed in `AlsoFiles`; see the fold-check-2 note below.]*
> - A waiting page or zip already gone on resume counts as deleted when its copy's `.eml` passes its
>   check or counts as deleted by the rule above.
> - **The start sweeps on a plaintext archive.** `SweepOrphans` (temps and orphan zips) runs at the
>   start of a local run and of `reindex`, before the manifest load, so before the `MergePending`
>   refusal (S3). *[Corrected 2026-11-07 by [dedup foldcheck2 S1 citation]: true of a local run
>   (`run.go:770` before the load at `:789`), not of `reindex`, which loads the manifest first
>   (`app/reindex.go:100`), then repairs keys (`:115`), then sweeps (`:118`); `reindex`'s refusal sits
>   right after `:100`; see the fold-check-2 note below.]* It removes only temp files and zips without a page; the order above means none of
>   them is a file the merge step still owns. D2's builder confirms in the commit that no other
>   start-of-run removal runs before the refusal.
>
> **S2 — a page or zip with no recorded digest.** It is deleted **as derivable** together with a proven
> `.eml` of the same copy, with an empty `sha256` allowed for those two roles only. Load validation
> keeps such an entry (§7.1 fold-check note), so the file is never orphaned.
>
> **S3 — every writer refuses while `MergePending` is set.** Not only backup, verify and the Desktop
> open, but also: `reindex` (redaction and rebuild), encrypt conversion, decrypt copy, archive delete,
> and Add-another-archive. Each refuses with the same words, "Finish the merge first", naming
> `mailarchive merge-copies -out DIR -resume` as the remedy, and changes nothing. Read-only `serve` and
> `search` still open (they write nothing). MA-1229 gains one refusal per writer, each after its
> positive twin. **(Not traced in code: the full list of writers; D2's builder greps every manifest save
> and lists them in the commit.)**
>
> **S4 — the deleter.** The merge step deletes through an `os.Root` opened on `-out` (Go 1.24 and later;
> this repo pins 1.26), or, if that cannot be used on a platform, by checking every path component with
> `Lstat` first. It removes only a regular file and refuses a link anywhere on the path. Redaction's
> string check (`validRelPath`, then `os.Remove`, `reindex.go:159-166`) follows a link in an
> intermediate directory, so the merge step does not reuse it. MA-1238's "refuses a link" covers a link
> as the file **and** as an intermediate directory, each after its positive twin.
>
> **S8 — the offer's words.** The offer (step 3) and the terminal's typed-yes screen add: "Make sure no
> other computer is backing up into this archive while this runs." Two writers on a synced folder were
> already out of scope (`manifest.go:781-783`). Even then no content is lost, since the survivor holds
> the same bytes; but another computer whose backup loaded the manifest before the merge and saves after
> it could bring back records that name deleted copies (the fold check's §2).

> **Folded 2026-11-07 [dedup foldcheck2 M1, M2, M3, D2] (lead's calls; the D2 items as the re-check
> proposes, with slice D2): the un-merge, as amended.** The re-check is
> `docs/review-gmail-dedup-foldcheck2.md` (commit 082a340). The notes above stand except where these
> points replace a sentence (each is marked where it stands). Plaintext archives only, as F1 says.
>
> **The rule (M2).** A file the merge step keeps is always named by a record that is shown as a mail:
> the copy's own restored record, or, only for a copy that **passed** its check, the survivor, whose
> original equals it byte for byte. **No file the merge kept is ever reachable only through a verify
> report.** §20 checks every row of §18 against this rule.
>
> **M1 — an un-merged copy's files belong to its own record only.**
>
> - When a copy is un-merged, its files go back to its **own** restored record (its `Path` and `Fixity`,
>   from `MergeGroup.Before`), and their `MergedFiles` entries come off the survivor. They are
>   **never** added to the survivor's `AlsoFiles`.
> - Why: redaction deletes a record's `Path` files and every `AlsoFiles` file
>   (`app/reindex.go:174-179`). With two owners, deleting the survivor email would delete the
>   un-merged copy too: a different email, with different bytes, that the person did not delete.
> - The merge step puts a file in `AlsoFiles` in one case only: a copy that passed its check but one of
>   whose removals failed (S1). Those bytes equal the survivor's.
>
> **M2 — a damaged survivor, or a damaged merge entry, un-merges the whole group.**
>
> - "A copy fails its check" now includes the **survivor's** original failing its re-hash at step 5.5.
>   Then **every copy of that group** is un-merged, each back to its own record. The damaged survivor
>   stays as it is, for verify to report (§8.7 fold's words name its folders).
> - A `MergedFiles` entry found damaged at load does the same for its group (§7.1 fold-check-2 note):
>   load writes nothing and marks the group; `merge-copies -resume` does the un-merge before anything
>   else.
> - **One edge** *(the folding agent's call, flagged for the lead)*: a copy whose three files an earlier
>   attempt already deleted, after its check passed against a then-good survivor (S1's "already gone,
>   counts as deleted"), has nothing on disk to put back. It is **not** restored: a record naming no
>   file would show a mail that cannot open. Its folder stays a membership on the damaged survivor, so
>   that folder still lists the email, and verify names the damage in that folder. The merge kept no
>   file for it, so the rule holds. The limit is the one any merge accepts (Q3): after the merge there is
>   one saved original, and damage to it afterwards is not undone by the merge step.
>
> **M3 — a missing page or zip is rebuilt before the record is put back.** Before the save that restores
> a copy's record (U2 below), if its page or zip is gone (an earlier attempt deleted them after the
> copy's check passed), the merge step rebuilds what is gone from the copy's own `.eml`, through the
> normal export path, under the archive lock:
>
> - The `.eml` is read as the message (it is already in the reader's form, §1 word list) and exported
>   into the copy's own folder, with the restored record in the working manifest and **`KeepRaw` off**.
>   So the `.eml` is never opened for writing: the carried-original path (`carriedOriginal`,
>   `export/exporter.go:849-861`) keeps it and its recorded `Fixity.EML`.
> - It exports only when the stem the exporter picks (`stemFor`, `exporter.go:1029-1059`) equals the
>   restored record's `Path`. Otherwise it writes nothing, so the re-export rename, which removes the
>   old page and zip (`exporter.go:789-797`), is never reached.
> - **It never overwrites a file that is on disk.** When only the zip is gone (a crash between the zip's
>   and the page's deletion), the page is rendered in memory only, to learn which attachments are
>   inline (`renderMessage`, `exporter.go:493-497`), and only the zip is written (`WriteZip`,
>   `export/attach.go:41`). When both are gone, the page is written before the zip, so no rebuilt zip is
>   ever left without its page for the orphan-zip sweep.
> - The new page and zip digests go into the restored record's `Fixity`. No index row is written here
>   (U3 adds it).
>
> If the rebuild fails or cannot run (a stem mismatch, a parse failure, a full disk), the record is
> restored anyway with **`RebuildOwed`** set (§7.1 fold-check-2 note), a **new field**: no existing mark
> stops `reindex` (its redaction reads only "is the row's page gone", `reindex.go:135-142`, `:174-185`).
> `Missing` is **not** used: it would make the record fillable, and a fill re-capture with `-raw`
> rewrites the home `.eml` from the live mbox (`exporter.go:443-445`, `:514-519`), replacing what is,
> in the M2 case, the only good copy. With `RebuildOwed` set:
>
> - `reindex` keeps the row and the record, deletes nothing, and logs "This email's page could not be
>   rebuilt after the merge; its saved original is kept.";
> - `mailarchive merge-copies -out DIR -resume` retries the rebuild for every `RebuildOwed` record (also
>   when no `MergePending` is left) and clears the mark when it succeeds;
> - verify reports the page or zip as missing and names that command as the fix.
>
> *(The folding agent's calls: the field's name, who retries, and the page-first order.)*
>
> **D2 — what `MergePending` holds, and when.** At 5.1 each `MergeGroup` gains `Before` (each copy's
> whole record), `SurvivorFolders` (the survivor's `Folders` before step 3) and `Unmerged` (copies
> already put back) (§7.1 fold-check-2 note). A record from `Before` is put back only if it passes the
> same checks a loaded record passes (`finish`, `state/manifest.go:775`). One that fails is **not** put
> back: `merge-copies -resume` stops for that copy, deletes none of its files, leaves its `MergedFiles`
> entries as they are, names the file stem, and leaves `MergePending` set (left open, §20).
>
> **The un-merge, step by step.** For each copy to un-merge (the one copy for M1; every copy with a file
> on disk for M2), in this order, all before step 5.6:
>
> - **U1. Rebuild** the copy's page or zip if either is gone (M3). Writes only files that are gone,
>   at the copy's own stem, which its `MergedFiles` entries still name.
> - **U2. Save the manifest**, through the temp-file, fsync, rename path:
>   - the copy's record back from `Before`, with the rebuilt digests, or with `RebuildOwed`;
>   - its `MergedFiles` entries off the survivor;
>   - the survivor's `Folders` set to `SurvivorFolders` plus the memberships brought by the copies of
>     the group that stay merged;
>   - the copy's key added to `Unmerged`.
> - **U3. Index**, one transaction, made durable: add the copy's search row (from its restored record);
>   delete the survivor's `doc_folders` rows for exactly the folders U2 took off it.
> - **U4. Folder pages** regenerate.
>
> **A crash after each step:**
>
> - **After U1.** Only the rebuilt page or zip is new. `MergePending` still holds the copy in `Before`,
>   not in `Unmerged`. Resume runs the check again:
>   - it fails again: U1 finds the files present and writes nothing, then U2 to U4 run;
>   - it now passes (for example, the changed `.eml` was put back by hand): all or none applies (S1,
>     S2), so a rebuilt page or zip whose bytes differ from its entry's recorded digest keeps the copy,
>     and the copy is un-merged. A copy is deleted only when everything matches, which is a proven
>     duplicate.
> - **After U2.** The copy is its own record again and is in `Unmerged`. Every 1.7 writer still refuses
>   (S3). Resume leaves it out of steps 2 and 3 (below) and redoes U3 and U4. A read-only `serve` may
>   meanwhile list the email under the survivor in that folder (the index is behind); it serves the same
>   bytes.
> - **After U3.** Resume redoes U3 (adding a present row and deleting absent rows changes nothing) and
>   U4.
> - **After U4.** Resume goes on with step 5.5 for the next copy, then 5.6.
>
> **Resume never re-merges an un-merged copy.** In the resume rule ("if any group's copy record still
> exists, redo step 2 … and then step 3"), a copy in `Unmerged` is left out of **both** redos. Step 2's
> redo would otherwise drop its restored search row; step 3's would fold it again, because its recorded
> `Fixity.EML` may still equal the survivor's, so F2's guard alone does not stop it.
>
> **F2's left-out records reach the yes.** The plan leaves out every record whose archived original
> fails its re-hash (GD-25).
>
> - The plan hands those keys to the yes, in the memory of the process that showed the plan (the
>   Desktop server, or the same `merge-copies` run). The plan still writes nothing to the archive.
> - The plan's digest also covers the left-out keys, in key order.
> - At the yes, the recompute (recorded digests only) leaves the same keys out before it groups, so one
>   damaged original elsewhere in the archive no longer makes every yes differ.
> - This fails safe. A left-out key only keeps a record out of the merge, so a wrong list can never
>   cause a delete. A record damaged after the plan is caught by step 5.5's re-hash (and un-merged, M2).
>
> **Where the refusals sit.**
>
> - **Encrypted (the re-check's §1.1 note).** The refusal of a sealed manifest holding a `MergePending`
>   or a `MergedFiles` entry (§7.1 fold-check note) sits in `openEncrypted` (`app/encrypted.go:422`),
>   after the sealed load (`container.LoadManifest`, `:432-438`) and before `prepareEncrypted` (`:444`),
>   whose start sweep is `:58-84`. It sits in `openEncrypted` itself, not in a caller's `check`
>   (`:439-443`), so every keyed caller refuses before any sweep (read on 2026-11-07: `run.go:681`,
>   `graph.go:206`, `sealedindex.go:359`, `programsetting.go:49`, `pausenote.go:40`; `prepareEncrypted`
>   has no other caller). `merge-copies`' own encrypted refusal comes earlier still, from the encrypted
>   envelope, before any key is used.
> - **`reindex` (the corrected S1 citation).** Its `MergePending` refusal sits right after the manifest
>   load (`app/reindex.go:100-103`), before `RepairKeys` (`:115`) and `SweepOrphans` (`:118`). The index
>   is opened earlier (`:90`); that is not an archived file. *(Not traced: whether that open writes to
>   `search.db` itself.)*

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

> **Folded 2026-11-07 [GD-15c2] (review condition):** the last bullet's "about as many cells" counted
> empty cells. **INBOX's summary has an empty `X-GM-MSGID` on 10,796 of 10,881 rows** (92 values; review
> §6). All Mail, Important, Sent Mail and Starred are as stated. §5.3's fold places INBOX's empty rows
> by Message-ID alone, or not at all.
>
> **Folded 2026-11-07 [GD-15a]:** "byte for byte" in the second bullet means in the reader's form (the
> word-list fold); the probe compared the mbox copies' bytes, which pass through the same transform.

### 10.2 The owner's archive (read-only, 2026-11-07)

`Mail-archive-dimarcotech`, manifest version 6, made with `-raw` (its job file says so); a backup was
running.

- 312,761 records, 233,322 distinct (store, identity) pairs; 69,498 identities in more than one folder.
- 79,200 extra records: 73,915 whose kept originals are byte-identical to another record's (34.5 GB);
  5,285 in groups where some original differs (this includes the Message-IDs reused by different
  emails, so it is an upper bound on missed matches).
- 90.9 GB of message files in all (pages, zips and originals, from recorded fixity).

> **Folded 2026-11-07 [GD-15c3] (review condition):** counting repeats inside one folder too, there are
> **79,439** extra records (the 79,200 above counts only copies spread across folders). The 5,285 is an
> upper bound that counts a whole group as different if any one copy differs or lacks an `.eml`; grouping
> by digest (§9 fold) recovers part of it with no new rule.

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

> **Folded 2026-11-07 [GD-15e], [Q-L1] and [GD-19] (review conditions).**
>
> - The 34.5 GB saving is proven only for the **73,915 byte-identical** extra records. Groups that
>   cannot be proven are not merged (GD-25) and free nothing.
> - "Each merge costs one SHA-256 of the copy": with Q-L1 that is the SHA-256 of the copy's original
>   bytes, taken once when the copy is first met (the same digest a `-raw` write records as fixity); a
>   steady re-run takes none (the alias path, §8.1 fold).
> - The summary pass reads about 285 MB only when a summary of the store changed (§5.2 fold, GD-19a).
> - The merge plan reads every kept original of the Gmail records it groups, once (§9 fold); its time
>   on the owner's disk is unmeasured, and the plan says the bytes before it starts.

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

> **Folded 2026-11-07 (the conditions applied to §12).**
>
> **Invariants, as amended [GD-16, Q-L1, Q-B]:**
>
> - **R45 reads instead:** "Gmail: one stored copy, every folder shown, no byte lost. In a Gmail store
>   the person has switched on, a copy whose original bytes have the same SHA-256 as an archived record's
>   becomes a member of that record and no file is written; a message Thunderbird lists in a folder
>   without a copy there becomes a listed member, never a new file; extract and restore give every
>   member folder the record's stored original. **A file already archived is deleted only by the
>   person's yes to a shown plan, only after a saved manifest no longer names it as a record's own file,
>   and only when, at the moment of deletion, the survivor's original and the copy's original each match
>   their recorded SHA-256 and the two are equal; otherwise it is kept, owned and reported.** An archive
>   is switched on only by the person's yes, or at its creation."
> - **R3** gains, after its local-import sentence: "except a Gmail store the person has switched on
>   (R45), where the same mail in two folders is stored once and shown in both."
> - **R6** ("Output mirrors the source folder tree") gains: "A Gmail member folder is mirrored by its
>   folder page and the reader's folder tree, not by files."
> - **R20 and R40 are unchanged** (Q-L1): every member folder gets the record's preserved original.
> - **R9 and R25** gain a note, not a change: the backup reads `prefs.js` once, read-only, through the
>   contained opener (§2.2 fold, GD-9e), and the summary files likewise (§5.2 fold g).
> - **Gate:** R3 and R6 change what a promise covers. These edits to `docs/scenario-catalog.md` are
>   **for the operator to accept** when slice B2 registers them; they are not self-granted.
>
> **Scenarios:** S78 to S80, S82 to S84 stand. **S81** ("a label removed in Gmail") is re-scoped: in
> version 1 the membership **stays** (add-only, Q-L2); S81 asserts that, and that nothing is deleted.
> S83 also covers resume after a crash and an older binary at each step.
>
> **Rows re-scoped** (the row in the table above stands as reviewed; this is what it now asserts):
>
> | Row | Now asserts |
> |---|---|
> | MA-1210 | U: the version-1 merge rule: byte-identical originals merge; any one byte different (an `X-Mozilla-Status` digit included) does not; the digest is `Fixity.EML`'s when present, else `gm_sha256` (Q-L1) |
> | MA-1211 | U: a record with no digest, an unparsed stub, or a gap label is never a candidate; several candidates with one digest form one class and the copy goes on its survivor |
> | MA-1213 | U: unchanged, plus: a fillable home record is not healed by a member's healthy copy; that copy becomes its own record and is counted (GD-5e) |
> | MA-1214 | S: gains a Full-mode re-run (home folder's copy re-writes, member copies do nothing, members kept), the KEEP guard on the alias key, and an alias set with two fingerprints (GD-5) |
> | MA-1215 | S: detection by `is_gmail` only through the shared function (MA-1235); an All-Mail-flagged folder on a non-Gmail account stays per folder; sticky `DedupStores` |
> | MA-1216 | U: `FormatVersion` over every new field, with the reflection test; a v6 archive byte-identical (MA-329); v1.6.1's loader refuses v7 |
> | MA-1217 | U: load validation of the §7.1 fold's fields |
> | MA-1218 | U: unchanged, plus: two-pass memory budget; deleted bits; `storeToken` read per row |
> | MA-1219 | U: the §5.2 fold's caps with its numbers; an unknown form refuses the file; a file changed between passes (retry once, then not read) |
> | MA-1220 | S: the §5.3 fold's rule: steps 3, 4 and 5 each; an empty Gmail id; a Message-ID matching three All Mail copies placed by byte position; an unmerged fixture with three records per email (GD-26) |
> | MA-1223 | S: **re-scoped** (Q-L2): the §7.2 fold's state table: add-only; a folder read partly adds no listed member; an unreadable or changing summary adds and removes nothing; never "gone" |
> | MA-1226 | S: extract gives every member folder the stored original; a tampered original is skipped and listed once, naming its folders (R20) |
> | MA-1228 | S: verify owns `MergedFiles`; checks each record's files once; damaged-original words name every folder |
> | MA-1229 | S: the §9 fold's steps; a crash at each of 5.1 to 5.6 then resume; v1.6.1 refuses at each step; a file changed between plan and deletion is kept in `AlsoFiles` |
> | MA-1230 | S: MA-1214, MA-1220 and MA-1229 through sealed units; no folder name or Message-ID in any plain file after each step and after a summary pass (GD-3) *[superseded 2026-11-07 by [dedup foldcheck F1]: MA-1229 is not run through sealed units; see the fold-check note below]* |
> | MA-1232 | L (pending): gains a run with Thunderbird open (the summary read on Windows, GD-8g) and the Q-B question on Edge |
>
> **New rows:**
>
> | Id | Tier | What it proves | Covers |
> |---|---|---|---|
> | MA-1235 | U | the shared detection function: `is_gmail` read through `util.ReadRegularFile` with the 16 MB cap; a link, a pipe and an oversized `prefs.js` refused (no hang) → not Gmail with one note; account matched by store folder (with or after MA-1195); the All Mail flag alone → not Gmail; All Mail found by 0x8000 only, never 0x4000, and absent when two folders carry 0x8000 | R45, S78 |
> | MA-1236 | S | the Q-B switch: an existing v6 archive stays v6, byte-identical, and backs up as today until yes; asked once in the Desktop and in an interactive terminal; never asked or switched by a scheduled run; Not now writes nothing into the archive and is remembered on this computer; yes writes `DedupStores` and v7; a new archive is on by default | R45, S82 |
> | MA-1237 | U | an encrypted archive's save and load keep `DedupStores`, `MergePending` and `SummaryStamps` (the rescue's N1); a v7 sealed record decodes strictly | R45 |
> | MA-1238 | S | the merge proof: from the archived originals against `Fixity.EML`, never the live copy; re-check just before each deletion; a survivor changed after the plan keeps the copy; no `Fixity.EML` → not merged and counted (GD-25); an archive without `-raw` offered nothing; page and zip deleted only with a proven `.eml`; the contained deleter refuses a link (positive twin first) | R45, S83 |
> | MA-1239 | U | the byte-position table: the counting reader's offsets equal Thunderbird's `storeToken` on a fixture (with a BOM, with `>From ` lines); a separator count unequal to the library's → table unused; mbox size or time changed between walk and summary pass → table unused | R45, S80 |
> | MA-1240 | S | an unmerged archive with three records per email: listed memberships go on each class's would-be survivor; a new identical copy after the yes goes there too; the merge step then moves no membership | R45, S80 |
> | MA-1241 | S | readers that assumed one folder per record: restore counts per (folder, key) and the restated "add up" promise; `plan.go:372`'s count; damaged counted once; extract one item per (folder, record), stem-collision suffix, `Emitted` per file; Desktop copy export into every chosen folder (Q-L3); `/goback` lists the home folder only; `KeepsTimeline` does not flip | R45, S84 |
> | MA-1242 | S | carry forward: a Full run, a fill and a heal of a merged record keep `Folders`, `MergedFiles`, `AlsoFiles`; `gm_sha256` recomputed from new bytes when there is no `Fixity.EML` (GD-14) | R45, S78 |
> | MA-1243 | U | the index: version 4 only with a `doc_folders` row, set in the same transaction; redaction and `Add`'s replace remove the key's `doc_folders` rows; a folder filter and facets count each message once | R45 |
> | MA-1244 | S | verify owns `AlsoFiles` (a Microsoft 365 collapse loser and a kept merge file both "kept", not `unexpected`); positive twin on a Microsoft 365 fixture | R45 |
>
> Every refusal test uses `assure.Refused` (exact behaviour, the message names the file and the remedy,
> the side effect absent) after its positive twin on a healthy fixture; every fix ships prove-fail then
> prove-pass, recorded in its commit.

> **Folded 2026-11-07 [dedup foldcheck F1, F2, F3, F4, S1 to S5, S7] (lead's calls): rows, as amended
> again.** Each row below adds to its re-scope above; the re-scope stays as it was.
>
> | Row | Now also asserts |
> |---|---|
> | MA-1213 | **aligned with the rule (F4):** a fillable record (`Missing` not empty, the unknown sentinel included) and a terminal-gap record are never a capture-time candidate; the copy becomes its own record and is counted, and the home record's `.eml` bytes are unchanged after the run (mutation: let a fillable record be a candidate, and the `.eml` comparison goes red) |
> | MA-1216 | **(F3b):** a fresh plaintext manifest (`newManifest`), a fresh sealed record (`container/store.go:166`) and a loaded version-5 archive are each saved at 6 when no version-7 field is set; v1.6.1's loader then accepts each and backs up into it; the refusals compare with 7 |
> | MA-1218 | **(S5):** a literal cell value for each of the four columns is read |
> | MA-1220 | **(F4):** a class with no complete record gets no listed membership and the row is counted |
> | MA-1229 | **plaintext only (F1).** **(S1):** a crash between a copy's page deletion and its `.eml` deletion, and one after the `.eml` deletion before the clear, then resume: the copy counts as deleted, nothing moves to `AlsoFiles`, verify reports no "missing". **(S1, all or none):** a copy whose zip fails its digest keeps its page, zip and `.eml`; then a backup attempt (whose `SweepOrphans` runs before the refusal) and resume leave all three on disk. **(S2):** a page with no recorded digest is deleted with its proven `.eml` and its entry survives a load before that. **(S3):** each writer listed refuses while `MergePending` is set (`assure.Refused`, side effect absent, positive twin first). **(F4):** a group with no complete record is not merged and is counted. **(S8):** the offer's words |
> | MA-1230 | in place of its re-scope's first clause: **MA-1214 and MA-1220 through sealed units** (not MA-1229); the plain-file clause stays |
> | MA-1235 | **(S7):** the 0x8000 cases are read from a `folderCache.json` fixture through the shared function |
> | MA-1238 | **(F2):** a Full backup that rewrites a survivor between the plan and the yes, then the yes: refused with "The archive changed since you reviewed the plan", nothing written (manifest bytes unchanged); a recorded `Fixity.EML` changed before step 3's redo on resume: that copy is not folded. **(S4):** a link as the copy's file and a link as an intermediate directory each refused, nothing outside `-out` removed, after the positive twin |
> | MA-1243 | **(F3a):** on an archive that has not switched, a 1.7 backup, `reindex -rebuild`, a forced `RepairKeys` and a fresh index each leave the index at version 3, and v1.6.1's index open accepts it; a version-3 index is not repaired; the first `doc_folders` row makes it 4 in the same transaction |
>
> **New row (F1):**
>
> | Id | Tier | What it proves | Covers |
> |---|---|---|---|
> | MA-1245 | S | an encrypted Gmail archive is **never offered and never runs** the merge step: after the yes, its card shows the encrypted words and no offer; `merge-copies`, `-plan` and `-resume` each refuse (`assure.Refused`: the exact refusal, the words, and the side effect absent: the sealed record and the `units/` listing unchanged), after the positive twin on a plaintext fixture; a capture-time member (a new identical copy in a second folder) **writes no unit and removes none**: the `units/` listing is equal before the run, after it, and after a second run whose start sweep runs (`prepareEncrypted`), and the live list is unchanged; a sealed manifest holding a `MergePending` or a `MergedFiles` entry is refused by name, changing nothing | R45, S83 |
>
> Id MA-1245 checked free on 2026-11-07: no file in `docs/` or any worktree uses MA-1245 to MA-1249.

> **Folded 2026-11-07 [dedup foldcheck2 M1, M2, M3, D2, B3] (lead's calls): rows, as amended a third
> time.** Each adds to the rows above; they stay as they were. No new id.
>
> | Row | Now also asserts |
> |---|---|
> | MA-1229 | **(M1):** a copy changed after the plan is un-merged; then the survivor email is deleted (its page removed) and `reindex` runs: **none of the restored copy's files is removed** (its page, zip and `.eml` byte-equal before and after) and its record still opens; mutation: put the copy's files in the survivor's `AlsoFiles`, and the check goes red. **(M2):** a survivor damaged after the plan: after the merge, every copy of its group is listed in its own folder and found by search, and verify reports the damaged survivor; a `MergedFiles` entry damaged at load: resume un-merges its whole group; a copy whose files were all deleted at an earlier attempt, then the survivor damaged: it is not restored, its folder still lists the email. **(M3):** a crash after a copy's zip and page are deleted, then the survivor damaged, then resume, then `reindex`: the copy's `.eml` is byte-equal to before, its page exists and its record opens; a crash between the zip's and the page's deletion: the kept page is byte-equal after the rebuild (only the zip is written); the rebuild made to fail: the record carries `RebuildOwed` and `reindex` removes nothing, after the positive twin (the same record without the mark, its page deleted by hand, is redacted as today). **(D2):** a crash after each of U1 to U4, then resume: no un-merged copy is re-merged (its search row survives step 2's redo), and every file's bytes are unchanged; a `Before` record that fails `finish`'s checks is not put back and none of its files is deleted |
> | MA-1238 | **(F2, D2):** an archive with one damaged original outside every group: the yes is accepted (the left-out key reaches the recompute); a key added to the left-out list by hand causes no deletion |
> | MA-1243 | **(B3):** an encrypted archive that has switched on, with a `doc_folders` row, reopens under 1.7 through `memory.go`, `lazy.go` (in memory and in a scratch file) and `listfolder.go`; a version-5 index is refused by each of the four opens, after that positive twin |
> | MA-1245 | **(D2):** the sealed-`MergePending` refusal leaves a planted orphan unit (one the start sweep would remove) on disk, which proves the refusal sits before `prepareEncrypted` |

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

> **Folded 2026-11-07 [slices] (lead's call GD-20; Q-L1; GD-24).** The table above is replaced by this
> one. GD0's review file is `docs/review-gmail-dedup-10lens.md`, not `-predesign.md` (GD-24). The old
> GD1 (split and rebuild core) is **retired** with Q-L1, and its adversarial gate goes with it; the
> false-merge guard is now the capture slice's adversarial pass. Duplicates (**S-dup**) and missing
> folder membership (**S-membership**) are each a releasable unit on a shared base, so either ships if
> the other stalls; each has its own version-7 trigger through the one `FormatVersion` (S-dup: copy
> memberships, `MergedFiles`, `MergePending`, `gm_sha256`; S-membership: listed memberships,
> `SummaryStamps`; both: `DedupStores`).
>
> | Slice | Unit | What | Gate before merge |
> |---|---|---|---|
> | **GD0** | — | this design, the review, these folds | the review's re-run of lenses 3, 6 and 10 on the folded text; an independent fold-verify (the lead arranges both) |
> | **B1** | base | the shared Gmail detection function (`is_gmail`, `util.ReadRegularFile`, the account by store folder; reuses or builds completeness C0c, MA-1195) (MA-1215, MA-1235) | prove-fail → prove-pass; unfiltered `go test ./...`; the test also run on the Windows VM |
> | **B2** | base | manifest version 7: the §7.1 fold's fields, `FormatVersion` and its reflection test, the sealed slots, load validation, the Q-B switch's plumbing (`DedupStores`, the job-file "Not now"), R3/R6/R45 catalog rows registered (MA-1216, MA-1217, MA-1236, MA-1237) | unfiltered suite, the encrypted round trip included; the operator accepts the R3 and R6 edits |
> | **B3** | base | surfaces: `doc_folders` and index version 4, the reader's tree and "In:" line, the back link, search filters and facets, static folder pages with the GD-21 limits, the Q-B question in the Desktop and the terminal, the status counts (MA-1222, MA-1224, MA-1243) | **friction review of the Q-B question and the folder views** (Windows on Edge first, then Mac); the UX contract walk |
> | **B4** | base | per-member readers: extract, restore, verify (owning `AlsoFiles`), redaction, archive delete, Desktop copy export, `/goback` pinned (MA-1226, MA-1227, MA-1228, MA-1231, MA-1241, MA-1244) | **adversarial pass** on restore (R40) and extract (R20); Windows VM run |
> | **D1** | S-dup | capture merging: the §4.4 fold's rule, the alias path, home-only writes, carry forward (MA-1210 to MA-1214, MA-1242) | **adversarial pass** (R1-critical: the false-merge guard and the alias path); mutation sweep; Windows VM run |
> | **D2** | S-dup | the merge step for existing archives: plan, offer, yes, deletions, resume (MA-1229, MA-1230, MA-1238) | **adversarial pass** (it deletes archived files); **friction review of the offer and the merge plan** (Windows on Edge first, then Mac); the owner's sign-off on its words |
> | **M1** | S-membership | the summary-file (`.msf`, Mork) reader, its own leaf package, to the §5.2 fold's specification (MA-1218, MA-1219, MA-1225) | **its own adversarial pass** (a new parser of an untrusted file); fuzzing with the throwaway account's corpus; mutation sweep |
> | **M2** | S-membership | listed memberships wired into the run: the byte-position table, classes on unmerged archives, the state table, the unchanged-summary skip (MA-1220, MA-1221, MA-1223, MA-1239, MA-1240) | unfiltered suite; Windows VM run with Thunderbird open |
> | **L** | both | lab rows MA-1232 to MA-1234; README, help page, release notes (every computer needs 1.7; no way back, Q-C; older `serve`/`search` show home folders only; redaction lasts while Thunderbird holds the mail) | the owner's release call |
>
> **Release units.** S-dup = B1, B2, B3, B4, D1 (new archives and new copies), then D2 (existing
> archives' space). S-membership = B1, B2, B3, B4, M1, M2. If one unit ships alone, the Q-B question
> names only what it does (§7.4 fold c), and the friction review walks the words that ship. The owner's
> archive gets its space back only with D2; its Sent Mail fills in only with M2.
>
> **Gates this design does not self-certify:** every adversarial pass and friction review above is run
> by a reviewer other than the builder, as the kit's process says; the owner signs off D2's words; the
> operator accepts the R3 and R6 edits.

> **Folded 2026-11-07 [dedup foldcheck] (lead's calls): slices after the fold check.**
>
> - **D2 is plaintext only (F1)** and owes MA-1245 besides its rows. On an encrypted archive S-dup is
>   B1 to B4 and D1 only: new copies are stored once; the space already used is not freed.
> - **A future slice, outside version 1: "the merge step for encrypted archives"**, after its own design
>   and 10-lens review (§9 fold-check note). Not scheduled.
> - **Order of the fold check's gates (its §7):** B1 may start now. B2 waits for the re-check of the F1
>   (§7.1 fold-check note) and F3(b) folds; B3 for F3(a); D1 for F4; D2 for F1 and F2. The fold check
>   re-checks only F1 to F4.
> - S5 is M1's, S7 is B1's, S1 to S4 and S8 are D2's; S6 and S9 are text, done here.

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

> **Folded 2026-11-07 (answers, owner defaults and lead's calls):**
>
> - **Q1:** keep the existing archive and offer the merge step behind a shown plan (owner default).
> - **Q2:** Gmail only (owner default).
> - **Q3:** yes, delete a copy only when the archive's own originals prove it (owner default); no proof,
>   no merge (GD-25).
> - **Q4:** amended by **Q-B**: Sent Mail and the other folders fill in from the first backup after the
>   person's yes, asked once; not "without asking".
> - **Q5:** deferred to the later per-folder history design (Q-L2).
> - **Q6:** yes for new archives, with the one setup and Settings line (§7.4 fold c).
> - **Q7:** unchanged.
> - **Q8:** left as is here. The completeness design's owner default Q7 keeps deleted-but-not-compacted
>   copies archived, which matches.
> - **Q-A** (review): moot under Q-L1. **Q-B, Q-C** (review): owner defaults, §0 and §7.4 fold.

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

> **Folded 2026-11-07 [GD-15] (review condition): claims, as amended.**
>
> - **Proven on the owner's data, now with labels:** "No mail is lost" for the four near-empty folders,
>   except one Sent Mail row (§1 fold); 79,439 extra records counting repeats inside a folder (§10.2
>   fold); INBOX's summary mostly lacks Gmail ids (§10.1 fold); `storeToken` is the `From ` line's byte
>   offset (review §6, Starred, 6,803 of 6,803).
> - **"Byte for byte" and "exact originals"** mean in the reader's form, never the mbox file's bytes
>   (word-list fold).
> - **By construction, to be proven by tests:** G3 now rests on SHA-256 equality alone (MA-1210); G4's
>   bound on the §5.3 fold's rule (MA-1220, MA-1239, MA-1240); G8 on the §9 fold's re-check (MA-1238).
> - **Estimates, not measured:** the manifest's size after the merge (about 175 MB, §7.1 fold); the
>   merge plan's reading time.
> - **The 34.5 GB** is proven only for the 73,915 byte-identical groups (§11 fold).
> - **After a merge, one original serves every folder** (G9): a damaged original is no longer backed by
>   another folder's copy.
> - **Not claimed, added:** per-folder "gone" or "since" history (Q-L2); that an older MailArchive's
>   `serve`, `search` or `status` refuse a version-7 archive (they do not, §7.4 fold e).
>
> **Folded 2026-11-07 [dedup foldcheck F1, F3] (lead's calls):**
>
> - **Not claimed, added:** freeing the space of copies already held by an **encrypted** archive. Version
>   1 does not merge them (§9 fold-check note); the 34.5 GB figure is the owner's plaintext archive's.
> - **By construction, to be proven by tests, added:** that an archive which has not switched stays
>   manifest version 6 and index version 3 under 1.7 (§7.4 fold-check note; MA-1216, MA-1243); that an
>   encrypted archive's start sweep removes nothing new because of this design (MA-1245).

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

> **Folded 2026-11-07:** the review attacked all ten (its §2). Items 1 (split) and 5's
> conditional-version worry are changed by Q-L1 and GD-12; item 10's detection seam is resolved (§6
> fold). For the review's re-run of lenses 3, 6 and 10 on the folded text, the places most worth
> attacking are: the §9 fold's crash order and resume rule; the §5.3 fold's byte-position table (its
> alignment of the n-th separator with the n-th message); the §7.4 fold's switch (the job-file "Not
> now", a new account added to an existing archive); and `FormatVersion` over the sealed path.

## 17. The folds re-read for contradictions (2026-11-07)

After the folds, the folding agent re-read this file end to end. Each contradiction found, and how it
is now resolved:

1. **The merge's crash order.** The reviewed step 5 wrote a plain marker first and changed the index
   before the manifest turned version 7, so an older binary could back up into a half-merged archive
   (GD-2) and the marker named folders beside an encrypted archive (GD-3). **Resolved:** the first
   durable act is the `MergePending` save, read back as version 7; then index, anchor, pages, deletions
   with their re-check, then clear (§9 fold). Every point after the first save refuses an older binary
   (the manifest is version 7 from the yes on) and refuses a 1.7 backup until resumed. Between the index
   step and the anchor save a read-only `serve` sees the copies' rows gone and the survivors' members
   added; the manifest still holds the copies, and resume redoes the idempotent index step. No file is
   deleted before both the index and the anchor save stop naming it (R28).
2. **What the merge proves against.** The reviewed step 5.5 checked fixity the step had removed at
   5.3 (GD-1). **Resolved:** `MergedFiles` entries carry the copy's size and digest and the survivor's
   proof digest, copied at the anchor save, and every one is re-checked just before deletion.
3. **v6 or v7, decided in one place.** The reviewed rule named `DedupStores` and `Folders` only.
   **Resolved:** `FormatVersion` covers every new field, `save` stamps only what it returns on the plain
   and the sealed path, and a reflection test fails on a field with no decision (§7.4 fold a). Nothing
   writes a version-7-only field without a `DedupStores` entry first: §4.4's, §5.3's and §9's folds all
   run only for a switched-on store, so a declined archive stays version 6 and byte-identical (MA-1236).
   The "Not now" answer is kept outside the archive for that reason. Q-C: once 7, always 7.
4. **Encrypted saves carrying the new manifest fields.** Today's sealed save keeps only the five `View`
   fields, so `DedupStores` would vanish at the first save (rescue N1), and with it the switch, sticky
   no more. **Resolved:** `DedupStores`, `MergePending` and `SummaryStamps` get slots in `View`,
   `FromSealed`, `save()` and the sealed header (§7.1 fold), with MA-1237. Read in the code on
   2026-11-07: the sealed decode probes the header's version and refuses a newer one
   (`ErrNewerFormat`, `container/record.go:166-169`) **before** its strict decode (`:207`), so v1.6.1
   refuses a version-7 encrypted archive as newer, never as damaged, provided the version is stamped
   right, which item 3 ensures.
5. **"Without asking" against the owner's Q-B.** The design's §9.2 and Q4 started listed memberships at
   the first backup without asking; Q-B asks first. **Resolved** for Q-B (§9 fold step 2, §14 fold).
6. **The survivor's stability.** Memberships placed before the merge (GD-26, and new identical copies
   after the yes) go on a class's would-be survivor; the merge step's survivor must be the same, or
   memberships move. **Resolved:** the survivor order puts the record already holding memberships just
   after completeness (§9 fold). If a fill later makes another record more complete, the merge step's
   anchor save moves the memberships to it (metadata only, in the same save); "step 5.3 needs no move"
   (GD-26) is then true in the usual case, not always.
7. **Repeats inside one folder.** A copy whose home folder is the survivor's own would have made a
   membership equal to `Record.Folder`, which load validation drops. **Resolved:** no membership is made
   for it (§9 fold step 3).
8. **Add-only against "turns into a copy membership".** §5.3's last paragraph upgrades a listed
   membership when a copy arrives; that is a change of kind, never a removal, so it stands with Q-L2.
9. **G6's "byte for byte that folder's copy" against a Full re-capture.** After a Full re-capture that
   changes the home bytes, the stored original is the home folder's current copy, not the member
   folder's copy at merge time. **Resolved** by saying so (§3 fold G6, §8.1 fold GD-14); R20 and R40 hold
   either way, because what is written is the record's preserved original.
10. **The class's digest against the merge's proof.** Capture-time classes use `Fixity.EML` or else
    `gm_sha256`; the merge step groups by `Fixity.EML` only. **Consistent:** a record known only by
    `gm_sha256` (made without `-raw`) can take new identical copies as members (no file is deleted for
    that), but is never merged by the deletion step, which has nothing archived to prove it (GD-25).
11. **Detection across designs.** The design said "`is_gmail` or the All Mail flag" and called it the
    completeness rule; that design had already dropped the flag (its TC-1). **Resolved** in its
    direction (§6 fold). **New, recorded:** the completeness design's TC-1 fold finds All Mail by 0x8000
    **or** 0x4000; the lead's call from its fold check (commit 92b80f8) makes it 0x8000 only. This design
    follows the lead; the completeness design's own text at 31bd018 still reads "or 0x4000" and is that
    design's to amend.
12. **The summary skip against the byte-position table.** Skipping an unchanged All Mail summary would
    leave the Sent Mail rows nothing to chain to. **Resolved:** the skip is all or nothing per store
    (§5.2 fold).
13. **The `raw_sha256` name.** §7.1's reviewed fields would have reused a name an unparsed stub already
    carries and that Load drops from other records. **Resolved:** `gm_sha256` (§2.2 and §7.1 folds).

**Still owed:** an independent fold-verify; this re-read was done by the agent that wrote the folds.

> **Folded 2026-11-07 [dedup foldcheck S9, S6] (lead's calls; text).**
>
> - **Item 11** is out of date from its "**New, recorded:**" sentence on: the completeness design's own
>   text now reads 0x8000 only. At commit **58e2542** its `[foldcheck M5]` fold marks the "0x8000 **or**
>   0x4000" paragraph superseded, and its `[foldcheck M1]` fold reads `prefs.js` through
>   `util.ReadRegularFile`. The two designs agree (§6 fold-check note). *[Item 11's last sentence, "the
>   completeness design's own text at 31bd018 still reads 'or 0x4000' and is that design's to amend", is
>   superseded by this note.]*
> - **Item 6** is the wording kept (S6): §5.3's "the survivor never changes once a membership is placed"
>   is marked superseded in place. With F4 the survivor must also be complete (§9 fold-check note).
> - **Item 4**, a loose citation the fold check found (its §6): the header's strict decode is
>   `container/record.go:171`; `:207` is `DisallowUnknownFields` inside `strictJSON`.
> - **The independent fold-verify** named above as owed is the fold check
>   (`docs/review-gmail-dedup-foldcheck.md`, 92a1173). Its re-check of F1 to F4 is owed next.

## 18. The plaintext merge re-read against the crash trace, with the fold check's folds (2026-11-07)

The folding agent re-read the folds above against the fold check's step-by-step trace (its §2): a crash
just after each step, v1.6.1 opening the archive at each step, and every 1.7 writer. "Archived byte"
means a byte of a record's original, page or zip. Folder pages and index rows are derived (R28) and
regenerated, so they are not counted.

| Point | Crash just after | Another program at this point | Can an archived byte be lost or changed? |
|---|---|---|---|
| The plan (writes nothing) | nothing to finish | a 1.7 backup may run: nothing is bound yet | No: nothing written |
| The yes: plan recomputed under the lock (F2) | nothing written | a backup that ran since the plan makes the digest differ, and the yes is refused | No: refused before any write |
| 5.1 `MergePending` saved, read back as 7 | resume redoes 5.2 and 5.3 | v1.6.1 refuses (v7); every 1.7 writer refuses (S3); `serve`/`search` read only | No: the save changes no file but the manifest, through the temp-file, fsync, rename path |
| 5.2 index (one transaction) | resume redoes it (idempotent) | as 5.1 | No: derived rows only |
| 5.3 anchor save: copies folded, `MergedFiles` written | resume goes on from 5.4; the copies' files are owned by `MergedFiles`; a redo folds a copy only if its recorded `Fixity.EML` still equals the survivor's (F2) | as 5.1; verify (when it next runs) owns `MergedFiles` | No: no file is touched; every copy file stays on disk, named by a `MergedFiles` entry with its size and digest; a plaintext archive has no start sweep of unnamed files (the fold check's §2: the sweep is the encrypted path's) |
| 5.4 pages | resume from 5.4 | as 5.1 | No: derived pages |
| 5.5 for one copy: re-check (survivor original against `Fixity.EML`; copy `.eml` against its entry; both equal `proof`) | resume re-checks from the start | as 5.1 | No: reading only |
| 5.5: zip, then page, deleted (S1 order, all or none per copy) | on resume, the `.eml` is re-checked; the gone zip and page count as deleted (S1) | as 5.1; a 1.7 backup or `reindex` attempt runs `SweepOrphans` before its `MergePending` refusal (`run.go:770` before `:789`), but finds no zip of ours without its page, because the zip goes first | No unique byte: the zip and page are derivable from the survivor, whose original equals the copy's byte for byte (just re-checked) |
| 5.5: `.eml` deleted | on resume, the gone `.eml` counts as deleted only if its entry digest equals `proof` and the survivor re-hashes to it (S1); otherwise it is listed as missing in `AlsoFiles` *[“otherwise … `AlsoFiles`” superseded 2026-11-07 by [dedup foldcheck2 M2]: the group is un-merged; this copy, with nothing on disk, is not put back and not listed; §9 fold-check-2 note]* | as 5.1 | No unique byte: deleted only after the survivor's original was re-hashed equal to it |
| 5.5: a mismatch on any of a copy's files | all of that copy's files stay, their entries move to `AlsoFiles` *[“their entries move to `AlsoFiles`” superseded 2026-11-07 by [dedup foldcheck2 M1]: the copy is un-merged and its files go back to its own record (§19; U1 to U4 in the §9 fold-check-2 note)]* | as 5.1; the orphan-zip sweep finds the kept zip beside its kept page and leaves it | No: the files are kept, owned and reported |
| 5.6 clear, saved | done | v1.6.1 still refuses (v7); 1.7 writers run again | No |

**Paths outside the step, re-read:**

- **The deleter (S4)** removes only a regular file under `-out`, through an `os.Root`, and refuses a
  link anywhere on the path; it cannot remove a byte outside the archive or follow a planted link.
- **A survivor that fails its re-hash** keeps every copy of its group: each copy's file moves to
  `AlsoFiles` (owned, verify reports it as kept). The good bytes stay on disk. Restore and extract then
  skip the damaged survivor and list it (R20), as today for any damaged original. *[This bullet is
  superseded 2026-11-07 by [dedup foldcheck2 M2]: a survivor that fails its re-hash un-merges every
  copy of its group, each back to its own record (its page and zip rebuilt first if gone, M3), so the
  good bytes are shown as mail in their own folders; only the damaged survivor is left for verify. See
  the §9 fold-check-2 note and §20.]*
- **A fillable record (F4)** is never a survivor and never a capture-time target, so no member folder's
  copy can reach the fill retry that rewrites a home `.eml`.
- **A page or zip with no digest (S2)** is kept by load validation and deleted only with its proven
  `.eml`.
- **The plaintext start sweep (found in this re-read).** The fold check's trace did not include
  `SweepOrphanZips`, which a local run and `reindex` call before loading the manifest. As step 5 was
  folded before (a page could be deleted while its zip was kept after a mismatch), the next backup
  attempt would have removed that kept zip. The S1 fold-check note now orders the zip first and keeps a
  copy's files all or none; MA-1229 gains the case. This is the folding agent's finding and fix, for the
  fold check's re-check to confirm.
- **Encrypted archives (F1)** never reach any of the points above: `merge-copies` refuses first.
- **Capture-time membership and listed memberships** write no file and delete none (the fold check's §2).

**Confirmed: on a plaintext archive, no path in the merge step, with these folds, loses or alters an
archived byte.** A file is deleted only when the re-hashed survivor original equals it byte for byte at
that moment, or when it is a page or zip derivable from such a survivor; every other file is kept and
owned. Two limits remain, both outside MailArchive's own writers and both already out of scope:

- **Another program writing into the archive** (a sync client, another computer backing up into the same
  synced folder) between the re-hash and the delete of one file. The archive lock does not reach another
  computer; S8's words tell the person. No MailArchive writer can run in that window (S3).
- **A sync client restoring an older copy of the manifest** after the merge: it would name copies that
  are gone. No content is lost (the survivor holds the same bytes); verify would report them missing.

This re-read is the folding agent's, not independent: the fold check's re-check of F1 to F4 is owed.

## 19. The lead's calls on the fold-check folds (2026-11-07)

Folded 2026-11-07 [dedup foldcheck lead's calls] (lead's call). These settle the four items §18 and the
folding agent left open.

- **A plaintext copy that fails its check is un-merged, not only kept.** Superseding §18's row "5.5: a
  mismatch on any of a copy's files" in this one respect: the copy's files still all stay, and its
  record is also put back as it was before the merge (its own folder, its own search row, its own
  page), so the person sees it exactly where it was. *[Amended 2026-11-07 by [dedup foldcheck2 M1,
  M3]: "its files still all stay" means they belong to the copy's own restored record only, never also
  to the survivor's `AlsoFiles`; a page or zip already gone is rebuilt from the copy's `.eml` first;
  see the §9 fold-check-2 note.]* Only a record that is not in `AlsoFiles` is shown
  as a mail; a file the merge left behind must never be reachable only through a verify report. The
  pre-merge record is saved inside `MergePending` before step 5.3 so un-merging needs no guess. The
  merge step only targets byte-identical copies, so a mismatch means the copy changed after the plan
  and is treated as a different email. MA-1229 gains this case: a copy changed after the plan is still
  listed in its folder and found by search after the merge.
- **A copy missing pieces may fold into a complete survivor**: accepted, because step 5.5 re-hashes the
  copy's own `.eml` against the survivor's before any delete. A copy whose `.eml` is itself missing is
  never folded (nothing proves it).
- **Listed folder entries are held back** while no record of that email is complete: accepted.
- **A damaged `MergedFiles` entry is kept as an owned file** at load, not dropped: accepted (never delete
  on doubt). *[Amended 2026-11-07 by [dedup foldcheck2 M2]: when `MergePending` holds its copy's
  record, "kept as an owned file" means kept in `MergedFiles` and its group un-merged, not moved to
  `AlsoFiles` (§7.1 fold-check-2 note).]*

## 20. §18 and the un-merge re-read against a crash after each step (2026-11-07)

Folded 2026-11-07 [dedup foldcheck2 M1, M2, M3, D2] (lead's call). The folding agent re-read §18's
rows and the new steps U1 to U4 (§9 fold-check-2 note) against two tests:

- **(a)** can an archived byte (a byte of a record's original, page or zip, §18) be lost or changed?
- **(b)** the M2 rule: is any file the merge kept reachable only through a verify report?

| Point | (a) lost or changed? | (b) reachable only through verify? |
|---|---|---|
| The plan; the yes with its recompute and left-out keys | No: nothing written; a wrong left-out list only keeps records out | No: every record is its own |
| 5.1 `MergePending` saved, with `Before` and `SurvivorFolders` | No: the manifest only, through temp-file, fsync, rename | No: every copy is still its own record |
| 5.2 index | No: derived rows | No: the copy's record is still in the manifest; resume redoes 5.2 (leaving out `Unmerged` copies) |
| 5.3 anchor save | No: no file touched; every copy file named by a `MergedFiles` entry with its digest | No: the files are waiting, not kept; a complete re-check comes before any delete, and `MergePending` forces it (S3) |
| 5.4 pages | No: derived | No |
| 5.5 re-check | No: reading only | No |
| 5.5 zip, then page, deleted | No unique byte: derived from a survivor whose original was just re-hashed equal | No: if a later check fails, U1 rebuilds what is gone before U2 puts the record back (M3) |
| 5.5 `.eml` deleted | No unique byte: deleted only after the re-hash proved it equal | No: if the survivor fails later, this copy has no file left and is not put back (M2's edge); its folder stays on the survivor |
| 5.5 a mismatch on a copy (M1) | No: all its files stay | No: un-merged, its files named by its own record, shown in its own folder |
| 5.5 survivor fails its re-hash (M2) | No: no copy file is deleted | No: every copy with a file on disk is un-merged; only the damaged survivor is left for verify |
| 5.5 a removal fails after the check passed | No: the remaining files stay | No: in `AlsoFiles` on the survivor, which is shown as a mail and holds equal bytes |
| U1 rebuild | No: writes only a page or zip that is gone; never overwrites a file on disk; never writes the `.eml` (`KeepRaw` off); never reaches the rename (stem must match) | No: the files are still named by `MergedFiles` entries; resume finds them |
| U2 save | No: the manifest only | No: the record is back; `RebuildOwed` keeps `reindex` from reading a missing page as a delete |
| U3 index | No: derived rows | No |
| U4 pages | No: derived | No |
| 5.6 clear | No | No |
| Load, a damaged `MergedFiles` entry with a `Before` record | No: kept as it is, never deleted | No: its group is un-merged at resume |
| Later: the person deletes the survivor email | No byte of an un-merged copy: none is in the survivor's `AlsoFiles` (M1) | No |
| Later: `reindex` meets a restored record | No: its page was rebuilt (M3), or it carries `RebuildOwed` and nothing is removed | No |

**Confirmed: with these folds, no path in the plaintext merge step or its un-merge loses or alters an
archived byte.** A file is deleted only when it is proven equal to a survivor re-hashed at that moment,
or is a page or zip derived from one. U1 writes only files that are gone, and never the `.eml`.

**The one exception to rule (b), named:** a manifest whose merge entries have no matching `Before`
record, or whose `Before` record fails `finish`'s checks. 1.7 never writes either (5.1 writes and
reads back `Before` for every copy before 5.3 writes any entry), so both mean a manifest changed by hand
or damaged on disk. In the first case the files go to `AlsoFiles` (the §7.1 rule), where verify names
them "kept, not shown as an email". In the second, resume stops for that copy and deletes nothing, so
the archive stays refused until someone looks. No byte is lost in either; the files are shown as mail
in neither. **Left open for the lead:** accept this, or rebuild a record from the copy's `.eml` the way
U1 rebuilds a page.

**Also left open (not byte loss):**

- A file in the survivor's `AlsoFiles` after a failed removal becomes reachable only through verify if
  the survivor is damaged **later**, after 5.6. That is today's position for Microsoft 365 collapse
  losers too.
- A `RebuildOwed` record is listed as a mail but its page does not open until the rebuild succeeds; its
  `.eml` still reaches extract and restore. How Desktop's single-email delete treats such a record was
  not traced.
- The M2 edge (a fully deleted copy is not put back) is the folding agent's call, flagged for the lead.

This re-read is the folding agent's, not independent: a re-check of M1 to M3 is owed before D2.

---

Folded by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method. Date worked on: 7 November 2026, from the session's clock.
Drew on: this design at 9203aae and the review at ff7fc90 (both read in full); the completeness design
at 31bd018 (§0, §2.2, §3, §4.1, §6, §12, §13, §17 read); the lead's calls and the owner's defaults as
given by the coordinator (not checked against the owner directly); the code read while folding
(`state/manifest.go` record fields and `View`, `container/record.go` header and version probe,
`util/openregular.go`, `thunderbird.go`'s reads, `export/exporter.go:496-525`). Not checked: the
completeness fold check file at 92b80f8 (its rule came through the coordinator); Thunderbird's source
for every item marked "recollection, unverified". Register: design folds (decisions given by the lead
and owner, and the folding agent's calls where marked).

Fold-check folds (the notes tagged `[dedup foldcheck …]` and §18), 7 November 2026, from the session's
clock: by a model answering under no name, a fresh instance that has not met the record before, for
Alex DiMarco, under the Limen method. Register: design folds of the lead's calls on
`docs/review-gmail-dedup-foldcheck.md` (92a1173), plus the folding agent's calls where marked (the
all-or-none deletion per copy and the zip-first order, found in the §18 re-read; refusing to drop a
`MergedFile` at load; a fillable copy may still fold into a complete survivor; listed memberships held
back from incomplete classes). Drew on, and reached: this design at 832a02f and the fold check at
92a1173, both read in full; the code at the cited lines (`index/index.go:200-222`, `:795-883`;
`app/encrypted.go:30-85`, `:275-304`; `export/units.go:295-362`; `container/store.go:970-994`;
`export/exporter.go:150-163`, `:438-522`, `:978-993`; `state/manifest.go:255-294` and the version
constant's uses; `export/atomic.go:106-163`; `app/run.go:735-789`); the completeness design at 58e2542
by grep and its M1 and M5 passages only. Not checked: that `index.go:162-164` and
`manifest.go:781-786` still read as the fold check cites (taken from it); every comparison with
`LoadedVersion`; the full list of manifest writers for S3; whether any path marks a complete record
fillable without passing the KEEP guard. No product code was changed or run; no test was run.

Fold-check-2 folds (the notes tagged `[dedup foldcheck2 …]` and §20), 7 November 2026, from the
session's clock: by a model answering under no name, a fresh instance that has not met the record
before, for Alex DiMarco, under the Limen method. Register: design folds of the lead's calls on
`docs/review-gmail-dedup-foldcheck2.md` (082a340), plus the folding agent's calls where marked (the
`RebuildOwed` name and its retry; U1 never overwriting a file on disk, and the page before the zip; a
fully deleted copy not put back under M2; a failing `Before` record stopping resume). Drew on, and
reached: the re-check in full; this design's §0 header notes, §7.1, §7.4's F3 note, §8.2, §8.7 to
§8.9, §9, §12's fold-check rows, §17 to §19; the code at `app/reindex.go:80-194`,
`index/memory.go:71-80`, `index/lazy.go:35-54`, `index/listfolder.go:176-203`,
`app/encrypted.go:25-85`, `:415-449` and the callers of `openEncrypted` by grep,
`state/manifest.go:60-149` and the `finish`/`save` lines by grep, `export/exporter.go:425-529`,
`:770-861`, `:1015-1059`, and the function list of `export/attach.go`. Not checked: whether
`index.Open` writes to `search.db` before `reindex`'s refusal; whether the normal export path can be
asked to write a zip only (the builder wires `WriteZip`); how Desktop's single-email delete reaches
redaction. No product code was changed or run; no test was run.
