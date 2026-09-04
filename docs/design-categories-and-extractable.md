# Design — mailbox categories, and the "extractable" status line

**Revision:** 1 (2026-09-03). **BUILD STATUS:** not built — input to the 10-lens
pre-code design review. It completes the two items the portability design left
open: message **categories/tags** (P6, deferred as "named-MAPI-property work")
and the **`status` extractability line** (PC15, deferred as "needs a manifest
EML-count surface"). Two small, independent slices.

## 1. Problem

- **P6-categories.** `docs/design-archive-portability.md` shipped read/unread,
  importance and sensitivity but deferred categories (a records manager's own
  classification of a message). They are captured from no source today.
- **PC15.** The portability design promised, then deferred, a `status` line that
  tells an operator whether the archive can be migrated out with `extract` —
  i.e. how many records have a preserved original (`.eml`). Today that is only
  learnable by running `extract`.

## 2. Properties (what this design makes true)

- **K1 — Categories are captured, shown, and kept out of identity.**
  `model.Message` gains `Categories []string` (empty when none). The PST reader
  fills it from the named `Keywords` property; the mbox/maildir reader from
  `X-Mozilla-Keys` (Thunderbird) and a `Keywords` header; Graph from the message
  JSON `categories` array (one more field on the already-widened `$select`, no
  extra request). Categories appear in the existing message-page "Status" row,
  HTML-escaped, and are recovered by `reindex -rebuild`'s read-back. They are
  **excluded from `contentHash`/`Fingerprint`** (mutable classification; hashing
  them would break R2/R3) and are not indexed this increment. (additive to
  R7/MA-90 and the message-state scenario; explicitly NOT R2/R3. Scenario S36.)
- **K2 — PST categories are read from the named property, honestly bounded.**
  The reader resolves the property id from go-pst's name-to-ID map
  (`NameToIDMap.StringToID["Keywords"]`, the `Keywords` string name of
  `PidNameKeywords` in `PS_PUBLIC_STRINGS`) and reads the value, parsing the
  multi-valued unicode (PT_MV_UNICODE) blob itself since go-pst has no
  multi-value string reader. A single-valued unicode categories property is also
  handled. Everything is best-effort inside the existing panic-safe convert path:
  an absent property, a foreign type, or an unparseable blob yields no categories
  and never fails the message. go-pst's `StringToID` is a flat string→id map, so
  a (rare) different property set also naming a property `Keywords` could shadow
  it — an acknowledged limit stated in §5. (R10 panic-safety; R1 no silent crash.)
- **K3 — Category text is untrusted and inert.** Category strings are
  sender/source-influenced; they are HTML-escaped in the Status row (like every
  other field), carry no markup, and are stripped of control characters before
  rendering or read-back so they cannot corrupt the page, the rebuild parser, or
  a terminal. (R19; the AGG-1/INT-1 posture.)
- **K4 — `status` reports extractability from the manifest alone.** `status`
  prints "Extractable: N of M records have a preserved original (.eml)" computed
  from the manifest (a record is extractable iff it recorded an `.eml` digest —
  `Fixity.EML != nil`), hashing nothing and reading no message file. When N is 0
  and M > 0 it says "not extractable: no preserved originals — re-archive a
  mbox/maildir/Graph source with `-raw` to migrate out later". Posture is
  unchanged (informational, like the fixity-coverage line). `status -json`
  carries `extractable {records_with_raw, records}`. The count reflects records
  written by the fixity version or later (same framing as "Fixity coverage"),
  stated in §5. (extends R18/S29; no new invariant.)

## 3. Mechanism

### 3.1 Categories (slice K)

- `internal/model/message.go`: add `Categories []string`; a comment that it is
  **not** referenced by `contentHash` (unchanged this slice).
- `internal/source/reader.go` (PST): resolve once per file
  `catID, ok := r.file.NameToIDMap.StringToID["Keywords"]`; per message, when ok,
  `GetPropertyReader(uint16(catID), m.LocalDescriptors)` and, by the reader's
  `Property.Type`, either `GetString` (PT_UNICODE) or `parseMVUnicode(ReadAt…)`
  (PT_MV_UNICODE = 4127). `parseMVUnicode([]byte) []string` is a pure function
  (4-byte little-endian count, then count×4-byte offsets, then UTF-16LE strings)
  with defensive bounds so a truncated/hostile blob returns what it safely can.
  Each value is trimmed and control-stripped; empties dropped.
- `internal/source/mbox.go`: `Categories` from `X-Mozilla-Keys` (space-
  separated) unioned with a `Keywords` header (comma-separated), trimmed,
  control-stripped, de-duplicated, order-stable.
- `internal/graph`: add `categories` to `Messages`' `$select`; `MessageRef`
  carries `Categories []string`; `internal/app/graph.go` sets it on the message.
- `internal/export/html.go` `statusLine`: append "Categories: a, b" when
  non-empty (escaped by the existing `row(...,"status")`). `internal/app/
  htmlheader.go` `applyStatusLine`: recover the `Categories: …` segment (split
  on ", ") so rebuild read-back is faithful. A category value therefore may not
  itself contain the segment separators the line uses; the reader splits
  conservatively and this is noted (the values are short tags).

### 3.2 Extractable line (slice L)

- `internal/state/manifest.go`: `func (m *Manifest) ExtractableCounts()
  (withRaw, total int)` counting records with `Fixity.EML != nil`. No new field,
  no version bump.
- `internal/health`: `Input.Extractable {WithRaw, Total int}` set by `Gather`
  from the manifest; `Summary` prints the K4 line after the fixity-coverage line;
  `status -json` gains `extractable`. Posture untouched. When the manifest has no
  records the line is omitted.

## 4. Build order and seams

Both slices are independent and file-disjoint except `renderHeader`/
`statusLine` (slice K only). Each ships prove-fail → prove-pass and catalog rows.

1. **Slice K — categories.** model field; PST named-property read + the pure
   `parseMVUnicode`; mbox/maildir headers; Graph `$select` + apply; the Status
   row + read-back. Tests: `parseMVUnicode` on a hand-built two-value blob and on
   truncated/empty bytes (no panic, safe subset); a Thunderbird `X-Mozilla-Keys`
   and a `Keywords` header populate `Categories` de-duplicated and order-stable;
   the Graph fake server's `categories` array populates them; a category with a
   `<script>`/control char is escaped and control-stripped in the Status row and
   survives a rebuild read-back inertly; `Fingerprint`/`Identity` unchanged when
   only `Categories` differ (extend MA-145); support.pst (no categories) yields
   none without error. **Real-PST categories end-to-end is lab-pending** (no
   categorized `.pst` fixture without Outlook) — a new lab row.
2. **Slice L — extractable line.** the manifest count, `Gather`/`Summary`/
   `status -json`. Tests: an archive built with `-raw` reports "Extractable: N of
   M" with N == the raw count; a PST-only (no `.eml`) archive reports "not
   extractable"; `status -json` carries the field; posture is unchanged across
   both.

## 5. Honesty of claims

- Categories, like read/importance/sensitivity, are a **capture-time snapshot**,
  shown as captured, excluded from identity, and not searchable this increment.
- PST categories are read from the `Keywords` named property via go-pst's flat
  `StringToID` map; if a tenant's PST defined an unrelated `Keywords` named
  property in another property set, that could shadow the categories one — an
  acknowledged, unlikely limit. The multi-value parse is bounds-checked and
  best-effort; validation against a real categorized PST is lab-pending because
  no such fixture can be built without Outlook.
- The extractable count is manifest-only and reflects records written by the
  fixity version or later (a pre-fixity `-raw` archive undercounts) — stated the
  same way as the fixity-coverage line; it never reads or hashes a message file.
- Scenario **S36** (and a lab row for real-PST categories) is a proposal for the
  OPERATOR to accept into `docs/scenario-catalog.md`.
