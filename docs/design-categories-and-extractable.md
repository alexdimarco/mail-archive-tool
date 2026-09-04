# Design — mailbox categories, and the "extractable" status line

**Revision:** 2 (2026-09-03). **BUILD STATUS:** approved with conditions, not
built — the 10-lens pre-code review is filed as
`docs/review-categories-predesign.md` (GO_WITH_CONDITIONS; QC1–QC8 folded in). It completes the two items the portability design left
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
  extra request). Categories appear in their OWN message-page field (a "Categories" row with
  `data-mailarchive-field="categories"`, never inside the " · "-joined Status
  line — QC3), HTML-escaped and capped (QC4), and are recovered losslessly by
  `reindex -rebuild` from that dedicated field. They are
  **excluded from `contentHash`/`Fingerprint`** (mutable classification; hashing
  them would break R2/R3) and are not indexed this increment. (additive to
  R7/MA-90 and the message-state scenario; explicitly NOT R2/R3. Scenario S36.)
- **K2 — PST categories are read from the named property, honestly bounded.**
  The reader resolves the property id from go-pst's name-to-ID map
  (`NameToIDMap.StringToID["Keywords"]`, the `Keywords` string name of
  `PidNameKeywords` in `PS_PUBLIC_STRINGS`) and reads the value, parsing the
  multi-valued unicode (PT_MV_UNICODE) blob itself since go-pst has no
  multi-value string reader — with hard bounds BEFORE any allocation (QC2), and
  under a localized recover so a corrupt property costs only the categories, not
  the message (QC5). A single-valued unicode categories property is also
  handled. Everything is best-effort inside the existing panic-safe convert path:
  an absent property, a foreign type, or an unparseable blob yields no categories
  and never fails the message. go-pst's `StringToID` is a flat string→id map, so
  a (rare) different property set also naming a property `Keywords` could shadow
  it — an acknowledged limit stated in §5. (R10 panic-safety; R1 no silent crash.)
- **K3 — Category text is untrusted and inert.** Category strings are
  sender/source-influenced; they are HTML-escaped in their own field (like every
  other header field), carry no markup, are stripped of control characters, and —
  because they live in a dedicated field, never the Status line — a category
  value can never be tokenised as a Status segment to forge Unread/Importance/
  Sensitivity on read-back (QC3). (R19; the AGG-1/INT-1 posture.)
- **K4 — `status` reports extractability from the same signal `extract` uses.**
  Extractability is a filesystem fact: a record is extractable iff its
  `<stem>.eml` is present on disk (a regular file). `status` Lstats each record's
  `.eml` (never hashes, never reads bytes) — the same presence signal `extract`
  emits on and `verify` counts (`WithEML`), via one shared helper so the three
  surfaces cannot disagree (QC1). It prints "Extractable: N of M records have a
  preserved original (.eml) on disk". It never prints a categorical "not
  extractable — re-archive": for the M−N records without one it says a
  mbox/maildir/Microsoft 365 source archived with `-raw` keeps an `.eml`, while
  Outlook `.pst`/`.ost` items never carry one (keep the `.pst` itself to migrate).
  Posture is unchanged (informational). `status -json` carries
  `extractable {records_with_eml, records}`. (governed by R20/MA-186, the
  file-presence definition; extends S29.)

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
- `internal/source/mbox.go`: `Categories` from `X-Mozilla-Keys` only
  (Thunderbird's local tagging; space-separated), trimmed, control-stripped,
  de-duplicated, order-stable. The sender-settable RFC `Keywords` header is NOT
  used (QC6).
- `internal/graph`: add `categories` to `Messages`' `$select`; `MessageRef`
  carries `Categories []string`; `internal/app/graph.go` sets it on the message.
- `internal/export/html.go` `renderHeader`: emit a dedicated "Categories" `<dd
  data-mailarchive-field="categories">` (its own row, NOT part of `statusLine`),
  the values joined for display, HTML-escaped, capped in number and total length
  with a visible truncation note (QC3, QC4). `internal/app/htmlheader.go`: read
  the categories back from the `data-mailarchive-field="categories"` field
  (its own field — lossless, no Status-line separator ambiguity). Categories are
  never appended to the Status line.

### 3.2 Extractable line (slice L)

- One shared helper — `func ExtractableCount(out string, m *state.Manifest)
  (withEML, total int)` (in package app, beside verify/extract) — Lstats each
  record's `<stem>.eml` under `out` (via the same `validRelPath` gate) and counts
  the regular files present. `verify`'s `WithEML` and this helper are the same
  code, and `extract` emits on the identical presence check, so the three cannot
  diverge (QC1).
- `internal/health`: `Input.Extractable {WithEML, Total int}` (set by the caller,
  since health must not import app — `cmd/mailarchive/status.go` computes it via
  the shared helper and passes it in); `Summary` prints the K4 line after the
  fixity-coverage line, with the source-aware M−N wording; `status -json` gains
  `extractable {records_with_eml, records}`. Posture untouched; the line is
  omitted when the manifest has no records.

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
   none without error; a hostile `parseMVUnicode` blob (huge declared count,
   out-of-range offsets) returns a bounded safe result with no OOM/panic (QC2);
   a category value containing " · "/", " renders in its own field and cannot
   forge a Status segment on read-back (QC3). **Real-PST categories end-to-end is
   lab-pending** (no categorized `.pst` fixture without Outlook) — a new lab row.
2. **Slice L — extractable line.** the shared `ExtractableCount` (.eml file
   presence), `status.go` wiring, `Summary`, `status -json`. Tests: on a `-raw`
   archive the count equals what `verify` reports (`WithEML`) and what `extract`
   emits (agreement, QC1); a PST-only archive reports N=0 with the source-aware
   "keep the .pst" wording, never a categorical "re-archive"; deleting a
   preserved `.eml` drops the count by one (file-presence, not a stale digest);
   `status -json` carries `extractable`; posture is unchanged.

## 5. Honesty of claims

- Categories, like read/importance/sensitivity, are a **capture-time snapshot**,
  shown as captured, excluded from identity, and not searchable this increment.
- PST categories are read from the `Keywords` named property via go-pst's flat
  `StringToID` map; if a tenant's PST defined an unrelated `Keywords` named
  property in another property set, that could shadow the categories one — an
  acknowledged, unlikely limit. The multi-value parse is bounds-checked and
  best-effort; validation against a real categorized PST is lab-pending because
  no such fixture can be built without Outlook.
- The extractable count is `.eml` file presence (an Lstat per record, never a
  hash), so it agrees with `extract` and `verify` on the same archive; it is
  current (a since-deleted `.eml` is not counted). It Lstats each record, which
  is O(records) on a `status` call — cheap next to a hash, and bounded by the
  manifest size.
- Scenario **S36** (and a lab row for real-PST categories) is a proposal for the
  OPERATOR to accept into `docs/scenario-catalog.md`.
