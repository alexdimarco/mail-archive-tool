# Design — archive portability and message state: rebuild the index, extract to mbox/eml, capture read/flag state

**Revision:** 2 (2026-09-03). **BUILD STATUS:** approved with conditions, not
built — the 10-lens pre-code review is filed as
`docs/review-archive-portability-predesign.md` (GO_WITH_CONDITIONS; conditions
PC1–PC17 are folded into this revision). Three slices: A (`reindex -rebuild`)
and C (message state) both touch `renderHeader`, so **C builds on A** (PC17); B
(`extract`) is independent. Each ships prove-fail → prove-pass; B owes an
adversarial pass.

## 1. Problem (the three items deferred by `docs/design-archive-fidelity.md` §1)

- **P4a — the index cannot be rebuilt from the archive.** `reindex` only prunes;
  the sole path that *adds* rows is the exporter. So `search.db` is
  reconstructable only by re-reading the source with `-mode full`. The README
  invites deleting the source after verifying, so "source gone + `search.db`
  lost, corrupted, or left out of a copy" leaves the archive's own full-text
  search unrecoverable. (nas-04.)
- **P4b — there is no supported way out.** No command emits standard interchange
  (`.eml`/`.mbox`) for migrating the archive into another system. (P4.)
- **P6 — read/flag/importance state is captured from no source.** (P6-archivist.)

Scope (the product review's verified reductions): P4b is **faithful-only**
(`extract` emits a message only when its original bytes are on disk); P6 ships
**Importance, Sensitivity, Unread** and defers Categories; the new fields are
**never** mixed into `contentHash`/`Fingerprint`.

The headline P4a case is an **already-existing** PST/default archive whose
`search.db` is lost — it has no `.eml`, so rebuild is entirely from the archived
`.html`. Revision 2 therefore specifies the from-HTML parser against **what the
current renderer already emits** (the installed base), with going-forward
attributes as a robustness aid, not the mechanism (PC3; predesign hidden
blocker).

## 2. Properties (what this design makes true)

- **G1 — Rebuild reconstructs the index and pages from the archive alone.**
  `mailarchive reindex -out DIR -rebuild` repopulates `search.db` and regenerates
  the folder pages from the manifest and the on-disk per-message files, touching
  no source and no network. A record whose sibling `.eml` exists is parsed from
  those original bytes (full fidelity); otherwise its index fields are recovered
  from the archived `.html` the tool wrote. It runs under the archive lock and
  requires an intact manifest. It never deletes the live index in place: the
  fresh index is built into `search.db.rebuild` and renamed over `search.db` only
  on full success (PC1). (extends R13, R8; R5, R12, R4. Proposed scenario S33.)
- **G2 — Rebuild is honest and changes no message file.** The summary reports how
  many records were rebuilt from preserved `.eml` bytes versus re-derived from
  the rendered HTML, and how many had a field or date that could not be
  recovered. Re-derivation recovers searchable text, not the original wire bytes.
  Rebuild changes no message file (`.html`/`.zip`/`.eml`); it does regenerate the
  folder and root `index.html`, and a from-HTML record's folder-table columns can
  be less complete than its intact message page (PC5). (R1 spirit.)
- **G3 — Extract emits standard mail, faithfully or not at all.**
  `mailarchive extract -out ARCHIVE -format mbox|eml -dest OUTDIR` writes, for
  every manifest record whose `<stem>.eml` is present, one `<folder>.mbox` per
  mirrored folder (`-format mbox`) or one `.eml` per message mirroring the tree
  (`-format eml`), under `-dest`. It never synthesizes an original: a record with
  no preserved bytes (every PST item; any archive built without `-raw`) is
  counted and listed, never written. Output is atomic and idempotent (temp+rename
  per file; a re-run does not double), and contained within `-dest`, which may
  not overlap `-out` (PC9, PC10). (Proposed new invariant R20; R4, R5, R12.
  Proposed scenario S34.)
- **G4 — mbox output is well-formed mboxrd.** Each message is preceded by a
  synthesized `From ` separator and any `>*From ` body line is `>`-quoted
  (mboxrd), so message boundaries survive a standard reader. A reader that
  unquotes `>From ` recovers the exact original bytes; an mboxo reader does not;
  `-format eml` is the byte-exact format for a consumer of unknown flavour (PC12).
- **G5 — Message state is captured, shown, and kept out of identity.**
  `model.Message` gains `Importance`, `Sensitivity` (strings, empty when unset)
  and `Unread` (bool). The PST reader fills them from `PidTagImportance`,
  `PidTagSensitivity`, `PidTagMessageFlags`; the mbox/maildir reader from
  `Importance`/`X-Priority`, `Sensitivity`, and the read flag (`X-Mozilla-Status`
  bit, or the maildir `S` info flag); Graph from a **widened listing `$select`**
  that already carries `importance,isRead,sensitivity` (one request, no extra
  round-trip — PC16). When any is set, the message page shows a HTML-escaped
  "Status" row. The three fields are **excluded from `contentHash`** (mutable
  state; hashing them would break R2/R3) and are not in the fingerprint.
  (additive to R7/MA-90; explicitly NOT R2/R3. Proposed scenario S35.)

## 3. Mechanism

### 3.1 `reindex -rebuild` (slice A; PC1–PC7)

`internal/app/reindex.go` gains a `rebuild bool` (wired through
`reindexFlags`/`runReindex` as `-rebuild`). When set:

1. Acquire the archive lock. Load the manifest; refuse with a typed non-zero
   naming `.mailarchive-manifest.json` if absent ("…may have been dropped by a
   copy that skips dotfiles; rebuild needs it"). Manifest reconstruction is out
   of scope (PC7).
2. Open a fresh `search.db.rebuild` sibling (delete any leftover first) via
   `index.Open` — both `docs` and `docs_fts` start empty, so no `DELETE` and no
   rowid collision. A present-but-unopenable live `search.db` is moved aside; the
   rebuild replaces it regardless (PC1).
3. For each manifest entry (`store` = first path segment, `folderPath` = middle
   segments, `key` = the manifest key), **validate `rec.Path` with `validRelPath`
   + component-wise `Lstat` (no symlink follow, regular-file-only, size-bounded)
   before opening anything** (PC2); then, fault-isolated per record (PC4):
   - if `<stem>.eml` exists and passes the gate, `source.ParseRFC822` it → a full
     `model.Message`; `fromEML++`;
   - else parse `<stem>.html` (`golang.org/x/net/html`), **anchored to the first
     `.mailarchive-header` element** (PC3): subject from its `.mailarchive-subject`
     div; From by splitting `formatSender`'s "Name <email>"; To/Cc from their
     dds; date from `fmtTime`'s parenthetical "(YYYY-MM-DD HH:MM UTC)" (Received
     else Sent, handling the separate Sent/Received rows); attachment names from
     the sibling `<stem>-attachments.zip` central directory. Markers/labels
     outside the first `.mailarchive-header`/first `<dl>` are ignored (a hostile
     body cannot shadow a field); unrecovered fields are left empty and counted;
     `fromHTML++`.
   - `idx.Add(store, folderPath, m, relPath, key)` into the temp index.
   A record whose file is missing (or fails the path gate) is pruned from the
   manifest, `pruned++`.
4. `idx.Flush`, fsync, close; regenerate folder + root pages **from the temp
   index**; `os.Rename` `search.db.rebuild` over `search.db`; log the
   `StoresMigrated`/`Rekeyed` upgrade lines and stamp the index meta version
   (PC6); save the manifest; print `rebuilt=<n> (from-eml=<a> re-derived=<b>
   unrecovered-fields=<u>) pruned=<p>`.

The renderer (`renderHeader`) additionally tags the subject div and the From/
To/Cc/Date/Sent/Received dds with `data-mailarchive-field` (inert, few bytes, no
visible change — MA-80/MA-144 stay green), so a *future* archive rebuilds by
attribute; the installed base is recovered by the structure match above. Plain
`reindex` on a manifest-present-but-index-missing/unopenable archive now names
`reindex -rebuild` as the recovery instead of "run an export first" (PC7).

### 3.2 `extract` (slice B; PC8–PC15)

New verb `extract` (a distinct verb, not the no-verb default export). `-out`
(archive, required), `-format` (`mbox`|`eml`, required), `-dest` (required),
`--overwrite`, `-log`. Registered in `knownVerbs` and **refused as a scheduled
job** ("an operator-driven migration, not a backup") — PC14. In the root `-h`
and the MA-39 walk.

`app.Extract(archive, format, dest, overwrite, logger, onProgress)
(ExtractReport, error)`:

1. Acquire the archive lock (extract holds it for its whole run; `-h`/README warn
   to run it outside the backup window — PC14). Load the manifest (refuse: none).
2. Refuse a `-dest` equal to, inside, or a parent of `-out` (symlink-resolved,
   component-wise), naming the overlap (PC10). `os.MkdirAll(dest)`; require it
   empty unless `--overwrite`. Every output path is `SanitizeSegment`-per-
   component and contained within `-dest`.
3. Walk the manifest **folder-grouped, sorted by key** (which groups
   folder-adjacent), one temp handle per folder file, closed on leaving the
   folder (PC9). For each record, confirm `<stem>.eml` with `validRelPath` +
   `inspect` (Lstat every component, no follow, regular-file, read bounded to the
   recorded size — PC8) and, when the record carries a recorded `Fixity.EML`,
   verify the bytes against it (PC13):
   - `-format eml`: write the bytes to `dest/<store>/<folder…>/<stem>.eml.tmp`,
     rename on success;
   - `-format mbox`: append to the folder's temp file a `From ` separator then
     the bytes with mboxrd `>`-quoting of `>*From ` lines then a blank line;
     rename the temp over `dest/<store>/<folder…>.mbox` when the folder walk ends
     (truncating any pre-existing file — never appending) — PC9.
   `emitted++`. A record with no `.eml`, a fixity mismatch, or a failed gate:
   `skipped++`, added to a capped list with the reason.
4. Print `emitted=<n> skipped=<s>` with the reason breakdown; exit per PC11:
   **0** when `emitted > 0 && skipped == 0`; the **partial** code (a fresh code
   declared in ux-contract X1, not `2`) when `skipped > 0` (including
   `emitted == 0`, e.g. a PST-only archive); **1** on refusal/error. ENOSPC mid-
   write discards the temp and refuses legibly. `extract` writes nothing into the
   archive.

A run and `verify` warn when a raw-capable source is archived **without** `-raw`,
and `status` reports "not extractable (no preserved originals)" for such an
archive, so the capture-time dependency is visible before the source is deleted
(PC15).

### 3.3 Message state (slice C, builds on A; PC16–PC17)

- `internal/model/message.go`: add `Importance`, `Sensitivity` (strings),
  `Unread` (bool); a comment states they are **not** referenced by `contentHash`.
- `internal/source/reader.go` (PST): `pidTagImportance = 23` (0x0017),
  `pidTagSensitivity = 54` (0x0036), `pidTagMessageFlags = 3591` (0x0E07); map
  importance 0/1/2 → `low`/``/`high`, sensitivity 0/1/2/3 →
  ``/`personal`/`private`/`confidential`, `Unread = flags & 0x1 == 0`.
- `internal/source/mbox.go`: `Importance`/`X-Priority` and `Sensitivity` headers;
  `Unread` from `X-Mozilla-Status` bit 0x1 for Thunderbird, the maildir `S` info
  flag otherwise.
- `internal/graph`: widen `Messages`' `$select` to
  `id,internetMessageId,receivedDateTime,importance,isRead,sensitivity`, carry
  them on `MessageRef`, set them on the message after `ParseRFC822` (PC16); a
  field the tenant omits stays empty.
- `internal/export/html.go` `renderHeader`: when any is set, add a "Status" `<dd>`
  (e.g. "Unread · Importance: high · Sensitivity: confidential"), HTML-escaped,
  tagged `data-mailarchive-field="status"`. Slice A's rebuild reads that
  attribute back into the model when present (PC17). Not indexed this slice.

## 4. Build order and seams

1. **Slice A — `reindex -rebuild`.** `reindex.go`, `reindexFlags`/`runReindex`,
   the renderer `data-mailarchive-field` tags, an HTML-header reader. Tests:
   build an archive with and without `-raw`, delete `search.db`, `reindex
   -rebuild`, assert the index returns the same hits as before; a from-eml and a
   from-html record both indexed with the right fields (fixture with Sent≠
   Received and a non-empty subject); a temp `search.db.rebuild` is renamed and no
   `docs_fts` orphan survives (search a term only in an old row → no hit); a
   deleted file is pruned; a `..`/absolute/symlinked/oversized path is
   skipped-and-reported, never read (assure.Refused/NoSideEffect); a corrupt zip
   on one record does not abort; no-manifest refuses naming the file; plain
   `reindex` on a lost index names `-rebuild`; the summary reports the split.
2. **Slice B — `extract`.** New verb + `app.Extract` + an mbox writer, root usage
   + `-h` (MA-39), README "Migrating the archive out", ux-contract X1 exit codes,
   `knownVerbs` refusal, the capture-time `-raw` warning + `status` line. Tests:
   a `-raw` archive emits one `.mbox` per folder that round-trips through
   `net/mail` with correct boundaries and `>From ` quoting; `-format eml` mirrors
   the tree; a re-run does not double (temp+rename, truncate); a PST-only/no-raw
   archive emits nothing, names the skipped count and reason, exits the partial
   code; a `..`/absolute/symlinked `-dest` or `<stem>.eml`, a `-dest` overlapping
   `-out`, and a fixity mismatch are refused/skipped (assure.Refused,
   NoSideEffect); extract writes nothing into the archive; `schedule -- extract`
   is refused. **Owes an adversarial pass** (dest and manifest-path containment,
   mbox `From `-line injection).
3. **Slice C — message state (on A).** model fields, PST + mbox/maildir + Graph
   readers, the Status row + rebuild read-back. Tests: a PST fixture message's
   importance/sensitivity/read state captured and shown escaped; a Thunderbird
   read bit and a maildir `S` flag set `Unread`; the widened Graph `$select`
   populates the fields against the fake server; the Status row is absent when
   nothing is set; `Fingerprint`/`Identity` unchanged when only these differ
   (extend MA-86/MA-145). **Owes a friction check** of the page wording.

## 5. Honesty of claims

- Rebuild from the rendered HTML recovers **searchable text**, not the original
  message; for a PST/default archive the `.html` is the only on-disk record of the
  content, so re-deriving from it is correct, and the summary names the from-eml
  vs re-derived split so the fidelity of each row is visible. Persisting the index
  fields at capture instead was weighed and rejected: it would duplicate the whole
  index on every record against the manifest's size discipline, and the `.html`/
  `.eml` already hold the data. A byte-faithful index still needs the sibling
  `.eml` (archive with `-raw`) or a re-export from the source; the README says so.
- `extract` is **faithful-only by construction** and copies preserved bytes as
  stored; when a recorded fixity digest exists it is checked first, otherwise the
  output is "as stored — run `verify` first". An archive built without `-raw`, or
  any PST/OST archive, yields nothing to extract; that is stated at capture,
  `verify`, `status`, and use, with the remedy named. No re-serialized original is
  ever written, so nothing `extract` emits is a forgery. mbox is mboxrd; `-format
  eml` is byte-exact.
- Message state is a **snapshot at capture time**, shown as captured, excluded
  from identity, and not searchable this slice. Categories/tags are deferred.
- New invariant **R20** and scenarios **S33–S35** are proposals for the OPERATOR
  to accept into `docs/scenario-catalog.md`; the build does not self-grant them.
