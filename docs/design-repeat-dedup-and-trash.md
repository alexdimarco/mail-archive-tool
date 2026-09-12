# Design — repeat capture: cross-folder dedup and Deleted Items / Junk policy

**Revision:** 1 (2026-09-11). **BUILD STATUS:** not built — input to the 10-lens
pre-code design review. It is architecture-relevant: it changes what the manifest
dedup key means for a *repeat* capture from a live source (Graph today; IMAP if
built), touches invariant R3, and must not break R13 (append-only) or R8
(search↔export parity).

## 1. Problem (raised 2026-09-11)

On a scheduled `graph` run against a live Microsoft 365 mailbox:

1. **Within a folder, dedup is correct.** Each message is recorded under
   `state.Key(token, folderKey, "mid:"+InternetMessageID)` (internal/app/graph.go
   ~274). A re-run lists each folder's refs (id, Message-ID, date — no bodies,
   internal/graph/graph.go `Messages`) and skips a ref whose key is already in the
   manifest, **without downloading the MIME** (`SkippedManifest++`). A message
   that stays put is captured once and never re-fetched. Good, keep it.
2. **The key is folder-scoped, and the folder walk has no exclusions.**
   `client.Folders` enumerates *every* mail folder recursively — Inbox, Sent,
   Drafts, **Deleted Items**, **Junk Email**, Archive, and every custom/child
   folder — with no skip list (internal/graph/graph.go `Folders`). folderKey is
   the folder's display-name path (internal/app/graph.go ~253).
3. **Therefore a folder MOVE re-captures the message.** The Graph item `id`
   changes on a move, but the RFC 5322 `InternetMessageID` is stable, so the
   dedup would recognise the message — except the key also carries the folder, so
   the moved copy gets a *new* key and is archived again under the new folder's
   tree. The commonest move is delete-to-trash: a message captured in `Inbox/` is
   deleted, reappears in `Deleted Items/` next run, and is written a second time.
   Because the archive never deletes (R13), the `Inbox/` copy stays, so the
   archive now holds two files for one message, and the search index counts two.
   Over a mailbox's life this accumulates ≈ one extra copy per distinct folder a
   message ever visits — bounded, but for a delete-everything mailbox it roughly
   doubles the store and inflates every count.

### The tension (why this is a review, not a patch)

- **R3** ("the same mail filed in two folders exports to both") is deliberate —
  for a message genuinely in two folders at once (a copy, a category-linked
  item). A *move* is semantically different (the message is no longer in the
  source) but looks identical to a folder-scoped key on a later run.
- **R13** (append-only; nothing on disk is deleted) means the source-folder copy
  captured *before* a move cannot be retracted afterwards. So "one copy per
  message" can only be enforced *going forward* from first capture, never
  retroactively.
- **Capturing Deleted Items has value**: it is how mail the user deleted is
  preserved in the archive. Excluding trash outright would lose deleted-only
  mail. So the fix must not simply drop trash.

## 2. Properties (what this design would make true)

- **D1 — Repeat capture dedupes a message mailbox-wide by Message-ID.** On a run
  over a live source, a message whose `InternetMessageID` is *already archived
  anywhere in that mailbox* is NOT downloaded or written a second time when it
  later appears in another folder. The first-captured location keeps the single
  archived file (R13: the original is never moved or deleted). The message's
  current/most-recent folder membership is recorded as **metadata** on the
  existing record (e.g. `AlsoIn []string` or a "current folder" field), so the
  move is *recorded*, not *duplicated*. (changes R3 for repeat capture; preserves
  R13, R8, R17.)
- **D2 — Deleted Items and Junk are EXCLUDED by default; the operator is asked.**
  OPERATOR RULING (2026-09-11): the two well-known folders **Deleted Items** and
  **Junk Email** are **not captured by default**. The configuration flow (the GUI
  wizard step, and the CLI/schedule via an explicit `-include-deleted`/
  `-include-junk` pair or an `-include-folders` choice) **asks** the operator to
  decide one way or the other, so it is a conscious choice, not a silent one. A
  fast click-through with no choice takes the **excluded** default. When an
  operator opts to include them, D1 still applies: a message already archived
  from another folder that later appears in Deleted Items/Junk is recorded as a
  move (metadata), never written a second time; a message that only ever lived in
  Deleted Items is archived once, there. Exclusion uses the stable well-known-
  folder ids (`deletedItems`, `junkemail`), so it is locale-independent.
- **D3 — Search and pages stay consistent (R8).** One archived file ⇒ one index
  row. A message's folder page shows it under its first-captured folder; its
  recorded also-in/current-folder metadata is shown on the message page and,
  optionally, the folder listing, so a reader can see it "moved to Deleted Items"
  without a second row.
- **D4 — Honest about what a move can and cannot do.** A message captured in
  Inbox then moved to Deleted Items keeps its `Inbox/` file and gains a "later in
  Deleted Items" note; it is not relocated (R13). A message moved *before* its
  first capture is archived once, in wherever it is first seen. Dedup is by
  Message-ID; a message with no Message-ID (rare) still dedupes on content hash,
  mailbox-wide.

## 3. Mechanism (sketch — the gate refines this)

- **Manifest**: add a mailbox-wide identity index, `midIndex map[token+"\x00"+mid]
  key`, built at load and maintained on `Add`. Before writing a message, the
  Graph/repeat path asks "is this Message-ID already archived in this mailbox?"
  If yes, it updates the existing record's folder metadata (a new `AlsoIn`/
  `LastFolder` field, additive, manifest stays backward-compatible) and skips the
  download+write. If no, capture as today.
- **Fast-path**: the existing folder-scoped `Get(key)` skip stays as the cheap
  same-folder case; the new mailbox-wide check catches the moved case (different
  folderKey, same mid). Both happen before the MIME fetch, so R17's "cheap
  re-runs" holds.
- **Folder policy**: `Folders` gains an optional exclude set (well-known folder
  ids `deletedItems`, `junkemail`, or display-name match), default empty. The
  well-known-folder ids are stable in Graph, so the skip is locale-independent.
- **Scope of the invariant change**: D1 applies to *repeat capture from a live
  source* (Graph; IMAP later). A single-pass import of a local store (PST,
  mbox) where a message legitimately appears in two folders at once is the R3
  case — the design must decide whether mailbox-wide dedup also applies there
  (losing the two-folder copy) or only to the live/repeat path. This is the
  central open decision for the review.

## 4. Honesty / open decisions for the review

- **The R3 change is an OPERATOR decision.** Adopting D1 re-words R3 from "same
  mail in two folders exports to both" to (roughly) "on repeat capture a message
  is archived once per mailbox; simultaneous multi-folder membership is recorded,
  not duplicated." The review should confirm whether that is desired, and whether
  it applies to one-shot local imports too or only to live/repeat sources.
- **Retroactive duplicates already on disk.** Archives captured before this
  change already hold the move-duplicates. A `reindex`-time or one-off
  reconciliation to collapse existing cross-folder duplicates is out of scope
  here (append-only makes it delicate); state it as a known limit or a follow-up.
- **Default trash policy — RESOLVED (operator, 2026-09-11).** Deleted Items and
  Junk are **excluded by default**; the setup flow asks the operator to include
  or exclude (a conscious decision), and a click-through defaults to excluded. So
  the common scheduled run does not archive trash/junk at all, which also removes
  the biggest move-duplicate source outright; including them is an explicit
  opt-in, and even then deduped per D1. The review should treat "exclude by
  default, asked at config, opt-in to include" as fixed, not open.
