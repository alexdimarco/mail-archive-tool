# Pre-code design review — categories and the extractable line (10 lenses)

**Design under review:** `docs/design-categories-and-extractable.md`, revision 1
(2026-09-03). **Verdict:** GO_WITH_CONDITIONS — conditions **QC1–QC8** below are
folded into revision 2; the build must not start from revision 1. Per-slice:
slice K (categories) — GO with QC2–QC8; slice L (extractable line) — GO with QC1.

**Method.** The assurance-kit design gate as a workflow on 2026-09-03: five
finders (two lenses each), a refutation-default skeptic per finding, a rescue
reviewer on the survivors. 21 findings; 18 confirmed or partial, 3 refuted; the
rescue named the single premise the lenses had circled.

## Hidden blocker (rescue reviewer)

**Extractability is a filesystem fact — is the `.eml` present and intact? — not a
manifest fact.** Revision 1's "manifest EML-count surface" (the PC15 deferral
note) was itself the wrong idea. Counting `Fixity.EML != nil` is wrong both ways:
a pre-fixity `-raw` archive has `.eml` files on disk but no recorded digest, so
the count reads 0 and `status` would tell the operator "not extractable —
re-archive" about an archive `extract` fully drains (and `verify` would disagree);
and a fixity-era archive whose `.eml` was later deleted or bit-rotted keeps the
recorded digest, so the count says "M of M have a preserved original" while
`extract` skips them. The count must come from the same `.eml` file-presence
signal `extract` and `verify` use, so the three surfaces agree.

## Findings and disposition

| Finding | Sev | Verdict | Failure the design permits | Condition |
|---|---|---|---|---|
| F1 extractable false-negative (pre-fixity `-raw`) | high | CONFIRMED | Fixity-based count says "not extractable — re-archive" about an archive extract drains | QC1 |
| F2 extractable predicate divergence | high | CONFIRMED | status uses a different extractability gate than extract/verify; they contradict | QC1 |
| F3 extractable false-positive (dropped `.eml`) | high | CONFIRMED | recorded digest survives a deleted/rotted `.eml`; "M of M have a preserved original" is false | QC1 |
| F1/F3 parseMVUnicode OOM | high | CONFIRMED | a 4-byte count from an untrusted blob sizes allocations/loops; OOM is outside R10's recover | QC2 |
| F1 category-status-line forgery | high | CONFIRMED | a category value containing the Status separator " · " forges Unread/Importance/Sensitivity on read-back | QC3 |
| F2 extractable N==0 wording | medium | CONFIRMED | the categorical "not extractable — re-archive" verdict is wrong for pre-fixity and unhelpful for PST-only | QC1 |
| F3 read-back consumes nothing | medium | CONFIRMED | categories aren't indexed and rebuild changes no `.html`, so a `, `-split read-back adds risk for no gain | QC3 |
| F3 "have a preserved original" over-claim | medium | CONFIRMED | manifest-only "have (.eml)" ignores a since-deleted/rotted file | QC1 |
| F2 PST-only extractable wording | low | PARTIAL | for a PST/OST archive N is always 0; the `-raw` remedy is impossible for it | QC1 (source-aware) |
| F3 localized recover | low | PARTIAL | a corrupt Keywords node fails the whole message to an "(unreadable message)" stub | QC5 |
| F4 sender-set Keywords header | low | PARTIAL | an RFC `Keywords` header is sender-chosen, not the operator's classification | QC6 |
| F4 unbounded categories | low | PARTIAL | pathological/many/long categories bloat the page and the rebuild parse | QC4 |
| F4/F5 read-back lossy on separators | low | PARTIAL | a category value with `, `/` · ` round-trips wrong | QC3 (own field) |
| F5 categories mislabelled "Status" | low | CONFIRMED | operator classification shown under the system-state "Status" label | QC3 (own row) |

Refuted: the proportionality "un-bundle the PST parser" (F2 L1-2 — the panic-safe
path and lab-pending fixture are accepted process, not a defect), and two others
that faulted stated deferrals.

## Conditions (folded into revision 2)

- **QC1 — Extractability is `.eml` file presence, and the three surfaces agree.**
  The count comes from Lstat-ing each record's `<stem>.eml` under `-out` (regular
  file present), the same signal `extract` emits on and `verify` counts
  (`WithEML`); a shared helper computes it so `status`, `verify` and `extract`
  cannot diverge. `status` drops the "reads no message file" claim (it Lstats,
  never hashes). The line reads "Extractable: N of M records have a preserved
  original (.eml) on disk". It never prints a categorical "not extractable —
  re-archive": for records without an `.eml` it says a mbox/maildir/Microsoft 365
  source archived with `-raw` keeps one, while Outlook `.pst`/`.ost` items never
  carry one (keep the `.pst` itself to migrate). It cites R20/MA-186; `status
  -json` carries `extractable {records_with_eml, records}`; a slice-L test asserts
  the number equals what `verify` and `extract` see on the same archive.
- **QC2 — parseMVUnicode is bounded before it allocates.** It clamps the declared
  4-byte count to `(len(blob)-4)/4` (each entry needs a 4-byte offset) and to a
  small absolute ceiling BEFORE any allocation; never pre-sizes a slice from the
  untrusted count (appends as offsets validate); uses 64-bit offset arithmetic;
  validates every offset in `[header_end, len(blob)]` and non-decreasing, stopping
  on the first violation. A slice-K test feeds a hostile large-count and
  out-of-range-offset blob and asserts a bounded, safe result (no OOM, no panic).
- **QC3 — Categories are their own page field, not part of the Status line.** The
  renderer emits a dedicated `<dd data-mailarchive-field="categories">`
  (a "Categories" row, parallel to the other header rows), HTML-escaped and
  control-stripped, NEVER appended to the ` · `-joined Status line. `reindex
  -rebuild` reads them back from that dedicated field (lossless — its own field,
  no separator ambiguity), so a category value can never be tokenised as a Status
  segment to forge Unread/Importance/Sensitivity. (MA-187's anchor/neutralize
  already block mail injection of the field.)
- **QC4 — Categories are capped.** The number and total rendered length of
  categories are bounded (mirroring the 64 KiB transport-header cap) with a
  visible truncation note, so a pathological set cannot bloat the page or the
  rebuild parse.
- **QC5 — The PST Keywords read is under a localized recover.** A
  `readCategories(m)` helper with its own defer/recover returns nil on a panic in
  the named-property node, so a corrupt/hostile Keywords property costs only the
  categories, never the whole message (no "(unreadable message)" stub).
- **QC6 — mbox categories are the local tag source only.** Categories come from
  `X-Mozilla-Keys` (Thunderbird's local tagging); the sender-settable RFC
  `Keywords` header is not used, so a sender cannot inject "categories".
- **QC7 — Categories stay out of identity.** `Categories` is excluded from
  `contentHash`/`Fingerprint`; a slice-K test proves `Identity`/`Fingerprint` are
  unchanged when only `Categories` differ.
- **QC8 — Honest lab boundary.** The `parseMVUnicode` parser and the graceful-
  absence path (support.pst yields none) are U-tested; real-PST categories
  end-to-end is a lab row (no categorized `.pst` fixture without Outlook). The
  go-pst flat `StringToID["Keywords"]` collision (an unrelated property set also
  naming a `Keywords` property) is an acknowledged limit in §5.
