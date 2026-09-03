# Design — archive portability and message state: rebuild the index, extract to mbox/eml, capture read/flag state

**Revision:** 1 (2026-09-03). **BUILD STATUS:** not built — this revision is the
input to the 10-lens pre-code design review (architecture-relevant: a new
index-population path that runs without the source, a new interchange
subcommand with its own path-containment surface, and new `model.Message`
fields that must never enter the identity hash). Three independent slices; each
may ship on its own and carries its own adversarial or friction pass as noted.

## 1. Problem (the three items deferred by `docs/design-archive-fidelity.md` §1)

- **P4a — the index cannot be rebuilt from the archive.** `reindex` only prunes
  (`internal/app/reindex.go`); the sole path that *adds* rows is the exporter's
  `OnExported` callback. So `search.db` is reconstructable only by re-reading the
  source with `-mode full`. The README invites deleting the source after
  verifying, so "source gone + `search.db` lost, corrupted, or left out of a
  copy" leaves the archive's own full-text search unrecoverable. (nas-04.)
- **P4b — there is no supported way out.** The archive's formats are
  self-contained HTML, attachment zips, and SQLite. A records manager moving to
  another mail or records system years later has no command to emit standard
  interchange (`.eml` / `.mbox`); `-raw` keeps per-message `.eml` only at capture
  time, only for mbox/maildir/Graph, never for PST, and never aggregated. (P4.)
- **P6 — read/flag/importance state is captured from no source.** `model.Message`
  has no field for read/unread, importance, or sensitivity, and no reader reads
  them. For a records manager a message's sensitivity marking or unread state can
  be part of the record; today they are dropped with no notice. (P6-archivist.)

Scope, from the product review's verified reductions:

- P4b is **faithful-only**: `extract` emits a message only when its original
  bytes are preserved on disk (`.eml`). It never fabricates an original from the
  rendered model. Records with no preserved bytes are counted and named, not
  synthesized.
- P6 ships **Importance, Sensitivity, and Unread** (cheap, cross-source, high
  fidelity). **Categories/tags are deferred** (named-MAPI-property work, one
  source). The new fields are **never** mixed into `contentHash`/`Fingerprint`.

## 2. Properties (what this design makes true)

- **G1 — Rebuild reconstructs the index and pages from the archive alone.**
  `mailarchive reindex -out DIR -rebuild` repopulates `search.db` and regenerates
  the folder pages from the manifest and the on-disk per-message files, touching
  no source and no network. For a record whose sibling `.eml` exists, the index
  fields are parsed from those original bytes (full fidelity). Otherwise they are
  re-derived from the archived `.html` the tool itself wrote (subject, sender,
  recipients, date from the header block; a body snippet from the body div;
  attachment names from the sibling zip). A record whose file is missing is
  pruned exactly as a normal reindex prunes it. The rebuild runs under the
  archive lock and refuses a directory with no manifest. (extends R13, R8; R5,
  R12. Proposed scenario S33.)
- **G2 — Rebuild is honest about re-derivation.** The summary states how many
  records were rebuilt from preserved `.eml` bytes versus re-derived from the
  rendered HTML, so an operator knows which rows are byte-faithful. Re-derivation
  recovers searchable text, not the original wire bytes; it never changes a file
  on disk. (R1 spirit: never silent.)
- **G3 — Extract emits standard mail, faithfully or not at all.**
  `mailarchive extract -out ARCHIVE -format mbox|eml -dest OUTDIR` writes, for
  every manifest record whose `<stem>.eml` exists, either one `<folder>.mbox` per
  mirrored folder (`-format mbox`) or one `.eml` per message mirroring the folder
  tree (`-format eml`), under `-dest`. It **never** synthesizes an original from
  the model: a record with no preserved bytes (every PST item; any archive built
  without `-raw`) is counted and listed, never written. It reads the archive
  under the archive lock and writes only inside `-dest`; every output path is
  contained (no `..`, no symlink followed out of `-dest`), mirroring R4. The exit
  code says whether the emitted set is the whole requested set. (Proposed new
  invariant R20; R4, R5, R12. Proposed scenario S34.)
- **G4 — mbox output is well-formed.** Each message in an `.mbox` is preceded by
  a synthesized `From ` separator line, and any body line beginning with `From `
  (or `>+From `) is `>`-quoted (mboxrd), so the file round-trips through a
  standard mbox reader without message-boundary corruption. (part of R20.)
- **G5 — Message state is captured, shown, and kept out of identity.**
  `model.Message` gains `Importance`, `Sensitivity` (strings, empty when not
  set) and `Unread` (bool). The PST reader fills them from `PidTagImportance`,
  `PidTagSensitivity`, and `PidTagMessageFlags`; the mbox/maildir reader from
  `Importance`/`X-Priority`, `Sensitivity`, and the read flag
  (`X-Mozilla-Status` bit, or the maildir `S` info flag); Graph from the message
  JSON already fetched during listing (best-effort). When any is set, the message
  page shows a "Status" row in its header, HTML-escaped. The three fields are
  **excluded from `contentHash`** (they are mutable state; hashing them would
  break R2 idempotence and R3 identity — a message marked read after capture must
  not become a "different" message), and they are not part of the manifest
  fingerprint. (additive to R7/MA-90; explicitly NOT R2/R3. Proposed scenario
  S35.)

## 3. Mechanism

### 3.1 `reindex -rebuild` (slice A)

`internal/app/reindex.go` gains a `rebuild bool` parameter (wired through
`reindexFlags`/`runReindex` as `-rebuild`). When set:

1. Acquire the archive lock (as reindex already does). Load the manifest; refuse
   with a typed non-zero naming the archive if none exists ("nothing to rebuild
   from").
2. Open `search.db` **creating it if absent** — the `-rebuild` branch bypasses
   reindex's normal "no index → refuse" guard, because a lost index is exactly
   what rebuild repairs. Begin a fresh transaction and delete all rows
   (`DELETE FROM docs`), so a partially-populated or stale index is replaced, not
   merged.
3. For each manifest entry whose file exists on disk (`store` = first path
   segment, `folderPath` = the middle segments, `key` = the manifest key):
   - if `<stem>.eml` exists, read it and `source.ParseRFC822` it → a full
     `model.Message`; `Rebuilt.FromRaw++`;
   - else parse `<stem>.html` with `golang.org/x/net/html`: read the header
     `<dl>` (the renderer's fixed labels — Subject, From, To, Cc, Date (UTC),
     Message-ID) into a `model.Message`, the `.mailarchive-body` text into
     `PlainBody`, and the sibling `<stem>-attachments.zip` central directory for
     attachment names; `Rebuilt.FromHTML++`.
   - `idx.Add(store, folderPath, m, relPath, key)`.
   A record whose file is missing is collected and pruned from the manifest
   (identical to the normal reindex prune), `pruned++`.
4. Regenerate the folder + root pages from the reconciled index, save the
   manifest, print `rebuilt=<n> (from-eml=<a> re-derived=<b>) pruned=<p>`.

To make the HTML fallback robust rather than dependent on visible label strings,
the renderer (`renderHeader`) adds a stable `data-mailarchive-field` attribute to
each header `<dd>` (e.g. `data-mailarchive-field="sender-email"`); rebuild reads
those. Archives written before this change carry no attributes: rebuild then
falls back to matching the fixed `<dt>` label text, and whatever it cannot
recover it leaves empty (the record is still indexed by whatever it did recover;
never dropped). The attribute is inert, adds a few bytes, and changes no visible
rendering (MA-80/MA-144 stay green).

### 3.2 `extract` (slice B)

New verb `extract` (a distinct verb, not the no-verb default export, so the
grammar stays unambiguous — the gate may rule on the name). `extractFlags`:
`-out` (the archive, required), `-format` (`mbox`|`eml`, required),
`-dest` (output directory, required), `-log`.

`app.Extract(archive, format, dest, logger, onProgress) (ExtractReport, error)`:

1. Acquire the archive lock; load the manifest (refuse: no manifest).
2. `os.MkdirAll(dest)`; every output path is built with `util.SanitizeSegment`
   per component and validated to stay within `dest` (the serve/verify
   containment rule), never following a symlink out.
3. Walk the manifest in a stable order. For each record whose `<stem>.eml`
   exists on disk (validated the same containment way inside the archive):
   - `-format eml`: copy the bytes to `dest/<store>/<folder…>/<stem>.eml`
     (atomic write, contained).
   - `-format mbox`: append to `dest/<store>/<folder…>.mbox` a From_ separator
     (`From mailarchive@localhost <asctime>`) then the message bytes with mboxrd
     `>`-quoting of `>*From ` lines, then a blank line. One open handle per
     folder file.
   `Emitted++`.
   A record with no `<stem>.eml` on disk: `Skipped++`, its folder/subject added
   to a capped list (like the verify report cap).
4. Print `emitted=<n> skipped=<s> (no preserved original bytes)` and, when
   `s > 0`, the note that those messages were archived without `-raw` or are PST
   items that carry no wire bytes, so they cannot be re-emitted faithfully. Exit
   **0** when `emitted > 0` and `skipped == 0`; **exit code TBD by the gate**
   when `skipped > 0` (proposal: a distinct non-zero "incomplete extract" code so
   a script can tell a whole export from a partial one); **1** on refusal/error.
   `extract` writes nothing into the archive.

`extract` is read-only against the archive and is a candidate schedulable verb,
but that is out of scope here (it is an operator-driven migration step).

### 3.3 Message state (slice C)

- `internal/model/message.go`: add `Importance string`, `Sensitivity string`,
  `Unread bool`. A comment states they are mutable state and are deliberately
  **not** referenced by `contentHash` (whose body is unchanged by this slice).
- `internal/source/reader.go` (PST): constants `pidTagImportance = 23`
  (0x0017), `pidTagSensitivity = 54` (0x0036), `pidTagMessageFlags = 3591`
  (0x0E07); map importance 0/1/2 → `low`/``/`high`, sensitivity 0/1/2/3 →
  ``/`personal`/`private`/`confidential`, and `Unread = flags & 0x1 == 0`
  (mfRead). All inside the existing panic-safe convert path.
- `internal/source/mbox.go`: read the `Importance` / `X-Priority` header and the
  `Sensitivity` header; `Unread` from `X-Mozilla-Status` (bit 0x1 = read) for a
  Thunderbird store; maildir sets `Unread` from the absence of the `S` info flag
  in the message filename (threaded in where the maildir path already has the
  name).
- `internal/app/graph.go` / `internal/graph`: set the fields from the message
  JSON (`importance`, `isRead`; sensitivity if present) already retrieved when
  listing a mailbox, best-effort; if the listing does not carry them, leave them
  empty and say so in the design note (no extra request).
- `internal/export/html.go` `renderHeader`: when any of the three is set, add a
  "Status" `<dd>` (e.g. "Unread · Importance: high · Sensitivity: confidential"),
  HTML-escaped like every other field, carrying a `data-mailarchive-field`
  attribute so a future rebuild can recover it. Not indexed in this slice.

## 4. Build order and seams

The three slices are independent and file-disjoint enough to build in parallel;
each ships prove-fail → prove-pass and catalog rows.

1. **Slice A — `reindex -rebuild`.** `reindex.go`, `reindexFlags`/`runReindex`,
   the renderer `data-mailarchive-field` attributes, an HTML-header reader in
   `internal/index` or `internal/app`. Tests: build an archive (with and without
   `-raw`), delete `search.db`, `reindex -rebuild`, assert the index is
   repopulated and search returns the same hits as before; a from-eml record and
   a from-html record both indexed; a record whose file was deleted is pruned;
   `-rebuild` on a directory with no manifest refuses; the summary reports the
   from-eml vs re-derived split. Owes a light integrity check, not a full
   adversarial pass.
2. **Slice B — `extract`.** New `extract` verb + `app.Extract` + an
   `internal/interchange` (or in-package) mbox writer, root usage + `-h` (X3/MA-39),
   README "Migrating the archive out". Tests: an archive built with `-raw` emits
   one `.mbox` per folder that round-trips through `net/mail`/a standard reader
   with correct boundaries and `>From ` quoting; `-format eml` mirrors the tree;
   a PST-only or no-`-raw` archive emits nothing and names the skipped count and
   the reason; a `..`/absolute/symlinked `-dest` or a tampered manifest path is
   contained/refused (assure.Refused, NoSideEffect); `extract` writes nothing
   into the archive. **Owes an adversarial pass** (containment of `-dest` and of
   manifest-supplied paths, mbox injection via a crafted body/From line).
3. **Slice C — message state.** model fields, PST + mbox/maildir + Graph readers,
   renderer Status row. Tests: a PST fixture message's importance/sensitivity/
   read state is captured and shown escaped; a Thunderbird `X-Mozilla-Status`
   read bit and a maildir `S` flag set `Unread` correctly; the Status row is
   absent when nothing is set; `Fingerprint`/`Identity` are unchanged when only
   these fields differ (the identity-stability guard — extend the MA-86/MA-145
   fingerprint test). **Owes a friction check** of the page wording.

## 5. Honesty of claims

- Rebuild from the rendered HTML recovers **searchable text**, not the original
  message: entities, whitespace and non-Latin normalization can differ from a
  from-`.eml` rebuild. The summary names the split so this is visible, and the
  README says plainly that a byte-faithful index needs either the sibling `.eml`
  (archive with `-raw`) or a re-export from the source.
- `extract` is **faithful-only by construction**. It can only emit messages
  whose original bytes were preserved, so an archive built without `-raw`, or any
  PST/OST archive, yields nothing to extract. That is stated at the point of use
  and in the README; the remedy (archive mbox/maildir/Graph sources with `-raw`,
  or keep the source for PST) is named. No re-serialized "original" is ever
  written, so nothing `extract` emits is a forgery.
- Message state is a **snapshot at capture time**: read/unread and flags are
  whatever the source held when the archive ran, and they are not re-checked.
  They are shown as captured, excluded from identity, and (this slice) not
  searchable. Categories/tags are out of scope and named as deferred.
- New invariant **R20** (extract faithful-or-absent) and scenarios **S33–S35**
  are proposals for the OPERATOR to accept into `docs/scenario-catalog.md`; the
  build does not self-grant them.
