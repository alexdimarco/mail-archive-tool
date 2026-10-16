# Design — parser fidelity: every MIME part kept, attachments byte-exact, PST attached items, identity frozen

**Revision:** 2 (2026-10-09). **BUILD STATUS:** approved with conditions, not
built; owner rulings folded (`docs/owner-rulings-2026-10-09.md`). The 10-lens pre-code review is filed as
`docs/review-mime-fidelity-predesign.md` (GO_WITH_CONDITIONS: 101 findings, 96
confirmed, 5 refuted; the rescue dissolved all four blockers and found one hidden
blocker). Conditions **MC1–MC43** are folded into this revision, each cited where
it lands. Owner rulings **OR1–OR5** and Q1–Q6 (§11) are answered in
`docs/owner-rulings-2026-10-09.md` §4 and the joint rulings it cites; each change they
make here is marked `[ruling <id>]`. Q6 / OR1 is escalated (ESC-6) and blocks only the
Desktop repair slice MF-SD. What each slice still waits on:

- S0 waits on BodyFallback landing (MC29), which has landed; OR5 is ruled [ruling J1].
- S1 waits on fault-isolation S1a (the landing order of [ruling J5]); OR2 and OR4 are ruled.
- S6 waits on Q2's fixture (MC32): Q2 is ruled yes for a synthetic fixture, which does not
  exist yet [ruling Q2].
- S7 waits on re-capture integrity's S4 and S5 (themselves waiting on ESC-2 and ESC-3), on
  S2–S6, S6 included (MC43), and on a delta re-review of this §4.10 (RR-4). Q1 and OR3 are
  ruled [ruling Q1, J6].

Revision 1 is superseded; the build must not start from it. Nothing in this
document is built, and no MA row below exists in the catalog yet. Catalog ids are
allocated [ruling OR5 = J1]: **R26** (parse fidelity; R-PARSE in revision 2, R22(a) in
revision 1), **R27** (identity freeze and repair; R-FREEZE, R22(b)) and **S49** (S-PARSE,
S42).

**Findings addressed** (verified review findings of 2026-10-08): F24, F25, F26,
C1-1, C1-4, C1-5, C1-8, C1-10. **Parsers in scope:** the one raw-MIME parser
behind mbox, maildir, Evolution, Graph `$value` and `reindex -rebuild` from a
preserved `.eml` (`source.ParseRFC822` → `safeParseMessage` → `parseMessage`,
`internal/source/mbox.go`), and the PST/OST attachment reader
(`internal/source/reader.go`).

**Downstream consumer (MC36).** The owner chose an extraction workspace where you
select a source, choose the export destination and copy (no drag-and-drop). How
much of this design it inherits depends on the path:

- **It re-parses an original** (the Microsoft 365 source, or the archive source
  when a record has a preserved `.eml`). It calls these same parsers, so it
  inherits P2–P10 and needs no parser of its own.
- **It copies the archived files** (the archive source for a record without an
  `.eml`). The copy carries these properties only if the record was captured, or
  repaired, at the current parser scheme. That never happens for with-Message-ID
  Graph mail on MailArchive Desktop (P11, OR1, escalated as ESC-6 [ruling ESC-6]), and
  Desktop keeps no `.eml` by default.

The seam items are listed in §4.11.

**User-visible surfaces (MC39).** This design adds no new screen or flow. It
changes these surfaces:

- the archived page: a visible gaps block and a collapsed notes block;
- the CLI run summary and `Done.` line;
- the GUI summary;
- `status` and `status -json`;
- the report;
- the dashboard capture card and the Desktop health view.

S2 and S4 owe a friction review and an X-series check for them (§10).

**Claim labels used below.** *Proven* = will be established by the named tier-U/S
test once built (nothing is built yet, so no claim here is proven today).
*Conditional* = holds only under the condition written next to it. *Aspirational*
= a goal the mechanism aims at but no test can make total. Line numbers are as of
2026-10-09; `internal/export/*` is being edited concurrently by another slice
(the `BodyFallback` "HTML shown as source" change), so export line numbers may
drift by a few lines.

## 1. Problem

One function, `parseMessage` (`mbox.go:242-325`), built on go-message v0.16.0's
`mail.Reader`, decides what every raw-MIME message becomes. It loses content in
seven verified ways, and the PST reader loses an eighth.

- **F24 — one unknown charset or transfer encoding ends the parse.** The part
  loop treats any `NextPart` error as end of message (`mbox.go:286-288`).
  go-message returns a *usable* part together with an `UnknownCharsetError`
  (`mail/reader.go:92-109`), and `(nil, err)` for an `UnknownEncodingError`
  (`mail/reader.go:92-94`) even though the underlying multipart reader has
  already advanced past that part. Every later part is lost. A single-part
  message with an unknown charset makes `CreateReader` return an error
  (`mail/reader.go:64-71`), and `mbox.go:243-245` then sends the whole message to
  `fallbackParse` (`mbox.go:329-361`), which keeps the subject as raw
  encoded-words and the body still transfer-encoded. Triggers: charset labels
  `unknown-8bit`, `x-unknown`, `charset=""`, `default`, `utf-7`, `cp-850`; CTEs
  `x-uuencode`, `8-bit`, `quoted-printable (encoded)`. Nothing records the lost
  parts: the exporter knows only "no body" (`exporter.go:351-353`). R1, R10.
- **F25 — only the first inline HTML and the first inline plain part survive;
  inline files are named `inline`.** `mbox.go:295-301` keep the first of each;
  go-message classifies every `disposition=inline` part and every `text/*` part
  without `disposition=attachment` as inline (`mail/reader.go:104`), so a *named*
  inline text part (`notes.txt`, a mutt `build.log`) falls into the body branches
  and is dropped when it is not first. Apple Mail's default structure,
  `alternative[plain, mixed[html, file, html, …]]`, archives a page that stops at
  the first attachment (`selectBody`, `html.go:384`, prefers `HTMLBody`).
  `mbox.go:302-311` names every inline non-text part by its Content-ID or
  `inline` (`inline (2)`, …), losing PDF, calendar and forwarded-message names.
  R1, R7.
- **C1-1 — text attachments are re-encoded to UTF-8.** `mbox.go:289` reads
  `p.Body` for attachment parts too (`mbox.go:313-321`); go-message converts
  every `text/*` entity that declares a charset (`entity.go:48-57`) whatever its
  disposition, and `mbox.go:14` registers every IANA charset. A Latin-1 CSV, an
  ISO-8859-1 XML e-invoice that declares `encoding="ISO-8859-1"`, a Shift_JIS
  `.txt` or a cp1252 `.ics` is stored as different bytes; the zip digest is taken
  over the altered bytes (`attach.go:57-59`), so `verify` attests the altered
  file. PST is not affected (its attachments are raw property bytes). R1, R20's
  spirit (faithful bytes).
- **C1-4 — one malformed Content-Type parameter loses the multipart boundary.**
  go-message's `parseHeaderWithParams` (`header.go:9-13`) returns the raw string
  and *no* parameters on any `mime.ParseMediaType` error; an unquoted boundary
  containing `=` (`----=_NextPart_000_0001_01D9`), an unquoted `start=<x@y>`, a
  comment (`charset=us-ascii (Plain text)`) or `name=Invoice 2023.pdf` all
  qualify. The boundary becomes empty, the first `NextPart` fails, and the loop
  breaks with no fallback: no body, no attachments, and a "missing body" gap
  that blames the mail client. Unquoted filenames become `attachment-N`. R1, R10.
- **C1-5 — a `cid:` reference in body TEXT consumes the attachment.**
  `RenderWith` calls `embedInlineImages` before it knows whether the body is HTML
  (`html.go:118`); `embedInlineImages` (`html.go:640-668`) replaces every textual
  `cid:<id>` and marks the attachment consumed (`html.go:666`), which removes it
  from the zip and from the page's attachment list. Outlook's plain-text
  placeholder `[cid:image001.png@01D9ABCD.12345678]` (common in RTF-only PST
  items) and `<a href="cid:…">` links turn an image into megabytes of base64 in a
  `<pre>`, with no zip entry. R7, R1.
- **C1-8 — legacy 8-bit text becomes invalid UTF-8.** A raw Latin-1 `Subject`
  comes back as raw bytes from `h.Subject()` (`mbox.go:250`); `AddressList`
  rejects an 8-bit display name (`mbox.go:258-261`, and `addressList`'s raw
  fallback at `mbox.go:364-367` keeps the bytes); a body with no charset label, or
  one labelled `us-ascii`/`utf-8` that is not, passes through unchanged
  (`charset.go:38-42`). The page shows U+FFFD and full-text search for `Grüße`
  misses it. The only "valid UTF-8, else Windows-1252" fallback is the PST
  reader's `decodeBytes` (`reader.go:517-525`), so README.md:1039's claim for
  "Both" is false for raw-MIME sources. R7, R8.
- **C1-10 — a From header `net/mail` rejects drops the sender.** The sender is
  set only when `AddressList("From")` succeeds (`mbox.go:258-261`; likewise
  `fallbackParse`, `mbox.go:347-350`), unlike To/Cc. `<>` (bounces), `Mueller,
  Hans <h@x>`, `a@b <a@b>`, `Mail Delivery System <MAILER-DAEMON>`, Japanese
  carrier addresses (`taro..yamada@docomo.ne.jp`) and 8-bit names produce no From
  row, `(unknown)` on folder pages, and no sender search. R7, R8.
- **F26 — an Outlook attached item (attach method 5) is archived as junk bytes.**
  `collectAttachments` (`reader.go:527-563`) never reads `PidTagAttachMethod`
  (go-pst exposes `GetAttachMethod`) and wires `WriteTo` (`reader.go:555`) to
  go-pst's `Attachment.WriteTo` (`pkg/attachment.go:314-333`), which reads
  property 0x3701 type-blind; for `PtypObject` the 4-byte value is an HNID that
  resolves to the 8-byte `{subnode NID, ulSize}` heap item (MS-PST 2.3.3.5), so the
  zip gets an 8–16-byte entry, non-empty, hence no gap. (This mechanism comes from
  MS-PST 2.3.3.5 and an in-memory simulation; it has not been observed on an
  Outlook-written item, MC32.) Forward-as-attachment,
  NDR originals, report-phish items and journaling envelopes lose the real
  message. Method 6 (OLE) takes the same path. PST items have no raw bytes, so
  `-raw` cannot help. No bundled fixture contains method 5 (`support.pst` is
  method 1 only). R1.

Four hazards constrain any fix:

- **H1 — identity drift (R2/R3).** Every field these fixes change — `Subject`,
  `SenderEmail`, `To`/`Cc`/`Bcc`, attachment names and count, `HTMLBody`,
  `PlainBody` — is an input to `Identity()` (`message.go:139-144` via
  `contentHash`, `202-212`), `Fingerprint()` (`154-161`) or `ContentDigest()`
  (`179-192`). On a local import an archived Message-ID seen with a different
  fingerprint is filed as a *distinct* message under a `#fp`-qualified key
  (`exporter.go:220-232`): a straight fix would duplicate every improved message
  on the first incremental run after the upgrade. A no-Message-ID message's
  `sha:` identity would move, re-exporting it under a new key and, on the live
  path, later sweeping the original gone (R21). The live path's adopt-never-split
  covers only legacy-*FpScheme* single records (`exporter.go:220-221`).
- **H2 — the nil-part panic.** A naive "keep going" fix that dereferences the
  `nil` part go-message returns for an unknown encoding panics;
  `safeParseMessage`'s recover (`mbox.go:201-206`) then turns the whole message
  into an `(unreadable message)` stub — worse than today, and (per F33, owned by
  the fault-isolation design) every stub shares one identity.
- **H3 — the process-global charset table.** F24's suggested
  `charset.RegisterEncoding("unknown-8bit", …)` mutates go-message's global table,
  which the PST reader also extends (`reader.go:56-60`) and which the identity
  replay of P1 depends on. Rejected (P1, P7).
- **H4 — re-capture regression (F46, owned by the re-capture-integrity design).**
  Any automatic re-capture over a source that has since lost content overwrites a
  good archived copy. The repair in P11 must not be such a re-capture. Only Graph
  is complete-at-fetch (`SourceComplete`, `app/graph.go:113`;
  `exporter.go:77-81`). Every local store can lose content: on-demand caches
  (Thunderbird and Evolution IMAP caches are mbox/Maildir++ files, and PST/OST
  cached mode) evict it, and mail clients rewrite local folders in place
  (Thunderbird's Detach/Delete attachment keeps the part's name and replaces its
  bytes). Counting entries therefore proves nothing, and the repair compares
  content (V1C, §4.10, MC3).

## 2. Properties

- **P1 — Message identity is parser-independent (serves R2, R3; proposes R27).**
  For a raw-MIME message, every input to `Identity()`, `Fingerprint()`,
  `ContentDigest()` and `contentHash()` comes from a frozen, verbatim replay of
  the v1 parser, never from the fidelity parser. The replay is today's
  `parseMessage`/`fallbackParse`/`addressList`, at a named release tag (MC7), run
  under its own recover. When v1 itself panics, the stub's identity terms come from
  the non-frozen `stubIdentityTerms` (§4.2, MC7). For a PST item, the inputs are
  the item's fields as v1 read them (only attachment names can differ, §4.8). File
  stems are also taken from the identity view (MC8). So no change in this design
  moves an archived message's manifest key, fingerprint, content hash or file
  stem, on any path (local, live, full, fill, repair), for changed or unchanged
  mail, and no future parser change routed the same way does either.
  **[ruling J6]** A repair commits through re-capture integrity's write path, so a
  repaired message's files are those its commit gives: the same identity-view stem,
  or that stem's `-vN` successor if the owner answers ESC-3 with (a). The stem is
  still computed from the identity view; only re-capture's suffix can differ.
  **Proven** for this design's own changes, by construction (MA-330).
  **Conditional** for dependency and toolchain changes (MC26). It holds while:
  - (a) `internal/source/parsev1.go` and its vendored dependencies stay
    byte-for-byte as frozen. MA-331 pins their sha256, so any edit goes red and
    needs an operator decision (MC26).
  - (b) The rest of the identity surface behaves as today. That surface is
    everything the replay touches:
    - the vendored go-message v0.16.0 `mail`/`textproto`/`charset` with its label
      table, including go-pst v6.0.2's nameMap, which go-pst registers at init and
      `openPST` registers again;
    - the vendored `net/mail` address and date parser, `mime.WordDecoder`,
      `mime.ParseMediaType` and `mime/quotedprintable` (MC27);
    - go-mbox v1.0.4's framing (CRLF line ends, `>From ` unescaping);
    - `x/net/html/charset` and `x/text`'s charmap and ianaindex tables,
      `encoding/base64`, the unicode tables, and the Go toolchain.
  - (c) Nothing changes go-message's charset table or `message.CharsetReader`
    beyond the init-time registrations of `go-message/charset` and go-pst. Those
    registrations are part of the frozen surface and are snapshotted by MA-331.

  (b) and (c) are **checked only on corpus shapes**, and are aspirational beyond
  them. MA-331 snapshots how every label in IANA, WHATWG, go-message's quirk table
  and go-pst's nameMap resolves, and the corpus holds one item per nameMap label.
  Even so, an upstream change on a message shape the corpus lacks can still move
  identity (MC26). The toolchain floats by policy (`ci.yml:26-33`), so `x/text`,
  `encoding/base64` and the unicode tables are the stated residual until OR4 is
  ruled (MC27). **[ruling OR4]** The toolchain keeps floating, and that residual is an
  open condition, not accepted: MA-330 and MA-331 run on every CI build and catch drift
  on corpus shapes, and S1 is asked to vendor `encoding/base64` into `parsev1` and have
  MA-331 assert `unicode.Version`, so the named residual becomes a tripwire. Drift is
  handled by §5's drift policy (MC28); it is never silently accepted.
- **P2 — No part's failure ends the parse of another (F24, C1-4; R1, R10).** The
  fidelity walker visits every leaf part. A decode problem in one part (unknown
  charset, unknown or malformed transfer encoding, corrupt base64, a panic in a
  decoder) is local to that part; a multipart *framing* error stops only that
  multipart container, which is never read again (no retry loop). There is no
  nil part: every leaf yields a header and a raw-body reader. **Proven** for the
  corpus and the trigger list of §1 by MA-333, MA-334, MA-338, MA-349;
  **aspirational** for all MIME in the wild.
- **P3 — Every inline text part is kept, in order; inline files keep their names
  (F25; R1, R7).** Unnamed inline `text/plain`/`text/html` parts are body
  segments assembled in document order (alternatives never concatenated across
  siblings); a *named* inline text part is an attachment; inline non-text parts
  are named by filename, then Content-ID, then `inline` + an extension from a
  fixed internal table; `message/rfc822` parts are named `<subject>.eml`.
  **Proven** by MA-335, MA-336.
- **P4 — Attachments are archived as the store holds them (C1-1; R1, F3 fixity
  honesty).** Every attachment's zip entry holds its transfer-decoded bytes as the
  source store holds them, never charset-converted, so the zip digest vouches for
  those bytes. Only body text parts are decoded to UTF-8, for the page and the
  index. **Conditional (MC38):**
  - The bytes are exactly as transmitted for base64 and quoted-printable parts.
  - 7bit, 8bit and binary parts read from an mbox are as go-mbox frames them (CRLF
    line ends, one `>` removed from `>From ` lines), which can differ from the
    sender's file.
  - A part whose transfer encoding is truncated or corrupt keeps only its decoded
    prefix. The zip names it `<name>.partial`, never the real name, and the gap
    block records it.

  **Proven** by MA-337.
- **P5 — A malformed parameter never costs the boundary or the filename (C1-4;
  R1, R10).** When strict parsing of Content-Type or Content-Disposition fails, a
  bounded lenient tokenizer recovers `boundary`, `filename`, `name`, `charset`,
  `type`, `start`; a multipart whose boundary still cannot be found keeps its body
  as text with a terminal `unsplit-multipart` gap, never a "missing body".
  **Proven** by MA-338.
- **P6 — `cid:` embedding happens only for HTML image contexts (C1-5; R7, R1).**
  Plain-text and RTF bodies are never embedded into; in HTML only `<img src>`,
  `background=` attributes and CSS `url(…)` (style attributes and `<style>`
  elements) are rewritten, by exact token; links and text keep their attachment
  in the zip. MA-19 (img-context images embedded and kept out of the zip) and
  MA-21 (unresolved image refs reported) keep holding. **Proven** by MA-339.
- **P7 — Displayed and indexed text is valid UTF-8 (C1-8; R7, R8).** Subject,
  display names, address fields and body segments are decoded by one local
  chain: declared charset if go-message/charset knows it; otherwise, or when the
  bytes are unlabelled or mislabelled, "valid UTF-8 as is, else Windows-1252" —
  the PST reader's rule — implemented without touching any global table.
  The fidelity-fallback path (P13) also runs v1's display fields (Subject, names,
  addresses, body) through this chain; identity is unaffected, because
  `IdentityTerms` keeps the v1 values (MC19). **Proven** by MA-340, including a
  fuzz-seeded property over all text fields of `safeParseMessage`'s output, so the
  fallback path is covered too. **Conditional** on Windows-1252 being the right
  guess for unlabelled legacy bytes: a Cyrillic KOI8 message without a label is
  shown as Windows-1252 mojibake, as PST already does. Where a guess shaped the
  displayed text, the page's notes block says so (MC25).
- **P8 — The sender is never silently dropped (C1-10; R7, R8).** When
  `AddressList("From")` fails or is empty, the decoded raw From text becomes the
  display name and a lenient `<…>` (or bare `x@y`) extraction the address, both
  control-stripped and capped at 1,000 runes. **Proven** by MA-341.
- **P9 — A PST attached item is archived as the real message (F26; R1, R19).**
  Method 5 is opened as a nested message, rendered by the same renderer into the
  outer zip as `<subject> (attached message).html` (CSP meta first, transport
  headers, its own inline images), with its own attachments as byte-exact
  entries `<subject> - <name>`. Recursion is bounded: depth 4, 256 items, a
  visited set keyed on block ids, and a per-message entry, byte and step budget
  (MC31). Each of these is a terminal gap and writes no junk bytes: method 6
  (OLE), an unopenable method 5, a non-mail embedded item, and a stop at the
  depth, cycle or budget bound. The method is decided by the type of property
  0x3701, not by `PidTagAttachMethod` alone (MC31). The interim slice (S5) is
  **proven** by MA-342 on an in-memory retyped `support.pst` and is the shipped
  floor. Full extraction is **aspirational** until MA-343 is tier U on a
  committed, Outlook-written method-5 fixture (Q2), and S6 may not merge before
  then (MC32). **[ruling Q2]** The fixture may be committed if it is synthetic: generated
  on a Windows Outlook host from made-up messages, or a third-party sample whose licence
  is recorded in THIRD-PARTY-NOTICES. No real mail enters the repo.
- **P10 — Undecodable content is a visible, recorded gap, with its bytes kept (R1;
  proposes R26).** A part that cannot be decoded is archived as received
  (still-encoded) where bytes exist. It is recorded as a gap in the manifest, the
  regenerated report (its own kinds), the run summary, `status`, and a visible
  block on the page. The class follows R1's source rule (MC20):
  - **Terminal on every source** for the deterministic kinds, because re-reading
    the same bytes gives the same answer: `undecoded-part`, `structure-limit`,
    `embedded-item`, `ole-object`, `identity-stub`, `fidelity-fallback`. The only
    way out is a later parser scheme's repair (P11).
  - **Fillable on an on-demand source** (`!SourceComplete`) for the kinds a later
    complete copy can cure: `corrupt-part` and `unsplit-multipart`. The probe asks
    whether a re-parse no longer yields the gap. On Graph these are terminal.

  **Proven** by MA-334, MA-342, MA-348.
- **P11 — Damaged archived messages are repaired in place, never to less (R2, R5,
  R13; proposes the R2 clarification of §6).** A record qualifies when it was
  written by an older parser scheme (or a revoked one, MC18) and its message,
  re-read now, triggers a fidelity note that a later scheme introduced. It is
  re-captured on the next run with the same key, fingerprint and file stem and new
  fixity. **[ruling J6]** "In place" is replaced: the repair is a re-capture through
  re-capture integrity's verdict and write path (W1–W5), taking only UNCHANGED, COMMIT
  or KEEP, never MERGE. Its key and fingerprint never move; its files are those that
  commit gives (the same stem, or its `-vN` successor if ESC-3 is answered (a)). "Never
  to less" is re-capture's R28, which R27 cites. That happens at most once per record per scheme and source content: if
  the output is unchanged, the record is restamped and not rewritten (MC12). The
  repair runs only if the V1C precondition proves four things (§4.10, MC1, MC3–MC6):
  - the archived files are present and as recorded;
  - today's source still delivers every archived item, matched by content
    (size and SHA-256), not by count;
  - the fidelity capture holds at least what v1 does;
  - it is the same message.

  Otherwise the repair is withheld with a named reason, persisted, and shown in
  `status` (MC15). The timeline is still stamped (MC2). **Proven** by MA-344,
  MA-345, MA-346.

  **Conditional** on the source still holding the message:
  - Local sources re-read every message each run.
  - On the Graph live path, incremental runs re-download only no-Message-ID
    messages. With-Message-ID Graph mail is repaired only by an explicit
    `-mode full` run, whose noted records take this same repair branch (MC9).
    R17 is unchanged: an incremental Graph run downloads no already-archived
    message.
  - MailArchive Desktop captures Graph incrementally only (`capture.go:43, :141`),
    so it **never repairs with-Message-ID mail**. Desktop says so in its summary
    (MC37), and a Desktop repair route is **OR1**. **[ruling ESC-6]** OR1 is escalated to
    the owner: until answered, MC16/MC37's disclosure stands and the gap stays open, not
    accepted.
  - In `-mode full`, records that carry no note keep today's behaviour. That is
    F46's unguarded re-capture, which this design does not change (MC9).

  Automatic or opt-in is **Q1**. **[ruling Q1]** Automatic, on the next ordinary run;
  `state.RevokedSchemes` is the rollback lever.
- **P12 — Mixed versions and rollback create no duplicate (R2, R3).** There is no
  manifest version bump in this design. Every new field is additive and
  `omitempty` (the precedent is the concurrent `BodyFallback` field). If OR3 adopts
  recapture-integrity's v7, see §6 (MC41). **[ruling J2]** It does not: the manifest stays
  version 6 for every in-flight design. Because identity and stems are P1, an
  older binary and this one compute the same keys, fingerprints and stems for
  every message, so alternating them, or rolling back, never `#fp`-splits,
  re-exports or renames. **[ruling J6]** A repair's `-vN` file name (ESC-3 (a)) is named
  by the manifest record, which an older binary reads, so rollback still renames
  nothing. **Proven** by MA-347. Residuals:
  - Any save by an older binary drops `parse_scheme` (and every other new field)
    from every record. Each alternation then costs this build one in-memory
    re-examination of the noted subset, and no rewrite, because unchanged output
    is restamped (MC12).
  - An older binary's `-mode full` re-renders with the v1 parser (un-repairs).
    The fidelity content comes back only while the source still holds the message
    when this build next runs, so "lose nothing" is **conditional** on that
    (MC39).
- **P13 — Hostile structure is bounded (R10).** The bounds are:
  - multipart depth ≤ 32 and leaf parts ≤ 10,000;
  - lenient parameters ≤ 64 per header, and RFC 2231 continuation indices ≤ 64;
  - an explicit 1 MiB cap on the top-level header and on every part header (MC22);
  - parse-gap labels ≤ 32 per record, plus one summary label (MC21);
  - PST embedding depth ≤ 4, ≤ 256 embedded items, and a per-message entry, byte
    and step budget (MC31).

  Past a bound, the remainder is kept as one still-encoded blob with a terminal
  `structure-limit` gap. Two things make the fidelity parser **never worse than
  v1**, panic or not:
  - A panic anywhere in it degrades that message to exactly what v1 produces (P1's
    replay is re-run for content), with a visible terminal `fidelity-fallback`
    gap (MC19).
  - A capture that fails the "fidelity ⊇ v1" check takes the same path, noted
    `fidelity-regression` (MC4).

  A stub results only where v1 itself produced one; in that case
  `stubIdentityTerms` gives the identity and an `identity-stub` gap makes it
  visible (MC6). New export-side code (embedded renders, cid embedding, gaps and
  notes blocks, the repair attempt) runs under its own recovers (MC14). Fatal
  runtime errors that are not panics (stack exhaustion, out of memory) are not
  recovered. **Proven** by MA-349.

## 3. Non-goals (explicit scope cuts)

- Changing the identity scheme to fidelity-parser terms. Rejected (§4.11): it
  would re-key or duplicate archived mail. v1 identity quirks therefore persist
  for new captures too — notably two no-Message-ID messages that differ only in
  parts v1 dropped still share one `sha:` identity (an existing limit, now
  written down).
- The `(unreadable message)` stub identity collision (F33) and PST row-loop
  isolation (F23) are owned by the fault-isolation design. This design does not
  change stub identity. It moves the stub's identity terms out of the frozen file,
  into `stubIdentityTerms` (§4.2, MC7), so F33's change never has to edit frozen
  code. Until F33 lands, a stub-identity record carries a visible `identity-stub`
  gap and is never repaired (MC6). Who owns which part is **OR2**. **[ruling J5]**
  Fault-isolation owns `stubIdentityTerms`, which S1 creates reproducing today's constant
  stub exactly. F33's identity fold is escalated (ESC-1), so it stays out until the owner
  answers.
- General re-capture monotonicity for `-mode full` and fills (F46), and re-capture
  commit ordering (N2, N3), are owned by the re-capture-integrity design. This
  design does make two changes on those paths:
  - every re-capture of a seen record keeps its stem and carries its `.eml`
    forward (MC8);
  - noted records in full mode go through the repair branch (MC9).

  The repair's commit primitive is a joint decision with that design (**OR3**,
  MC41). S7 does not merge before it. **[ruling J6]** The repair commits through that
  design's verdict and write path (W1–W5); there is no in-place exemption.
- Repair from a preserved `.eml` without the source (the only cheap repair for
  with-Message-ID Graph mail): deferred, **Q3**. **[ruling Q3]** It gets its own design and
  its own 10-lens review, and does not block S0–S7.
- Decoding UTF-7, uuencode/`x-uuencode`, BinHex, TNEF (`winmail.dat`),
  `message/partial` reassembly, S/MIME or PGP; `multipart/digest`'s implicit
  `message/rfc822` default type (parts keep go-message's `text/plain` default).
  Undecodable parts are kept as received (P10). (uuencode: **Q4**; **[ruling Q4]** kept
  still-encoded with a terminal `undecoded-part` gap; a bounded decoder can be a later
  slice.)
- Rendering MIME `message/rfc822` attachments as nested HTML (they are kept as
  the faithful `.eml`); indexing the text of attached/embedded messages; indexing
  HTML-only segments of a message that also has a plain track (the index keeps
  "plain body preferred", `index/htmltext.go:14-22`).
- PST attach methods 2/3/4/7 (by-reference/web links): unchanged.
- No change to Message-ID or Date extraction, to `-raw`'s default, to the
  transport-header panel (shown as stored, MA-143/144), to MA-19's semantics, or
  to the missing-body gap's fillable classification (MA-67).
- No new CLI flag, verb, refusal or exit code (if Q1 is ruled "automatic"; **[ruling Q1]**
  it is). None of this design's counts (undecodable, fidelity-fallback, repair withheld,
  repair failed) makes a run `partial` under fault-isolation's partition (MC40); they
  register there as informational [ruling J5]. (Fault-isolation's exit 3 is not adopted,
  [ruling fault-isolation OR2].)

## 4. Mechanism

### 4.1 Model (`internal/model/message.go`)

```go
type IdentityTerms struct { // the v1 parser's values of every identity input
    InternetMessageID, Subject, SenderEmail, To, Cc, Bcc string
    IdentityDate        time.Time
    HTMLBody, PlainBody, RTFBody string
    Attachments         []IdentityAttachment // {Filename string; Size int64}, v1 order
}
type Message struct { …
    IdentityTerms *IdentityTerms // nil → the content fields are the identity inputs (as today)
    ParseNotes    []ParseNote    // {Code string; Since int; Detail string} fidelity events
    ParseGaps     []ParseGap     // {Kind, Detail string} message-level parse gaps (channel per OR2, MC40; [ruling J5]: carried as fault-isolation's ReadGaps labels)
    V1Inventory   []V1Item       // {Index int; Label string; Size int64; SHA256 [32]byte}, from the replay (V1C, MC3)
    FellBack      bool           // this parse used v1 content (P13; MC4, MC19)
}
type Attachment struct { …
    Embedded *Message // PST method 5, opened (§4.8); WriteZip renders it
    GapKind, GapDetail string // non-empty: a parse gap for this part;
                              // WriteTo nil → nothing archived; non-nil → bytes as received
    ZipName  string   // final zip entry name, computed once before rendering (MC33)
}
```

The record (`internal/state/manifest.go`) gains these fields, all `omitempty`:

- `ParseScheme int` (§4.10);
- `FidelityFallback int`, the scheme a fallback write was made under (MC19);
- `RepairWithheld *{Scheme int; Reason string; SourceDigest string}` (MC15).

From S1 on, `ContentHash` (the v1-view `ContentDigest` via `idv()`) is written on
every record, local ones included. It is not an identity input, so no key moves
(MC5).

`Identity`, `Fingerprint`, `ContentDigest` and `contentHash` read their inputs
through one accessor, `m.idv()`, which returns `IdentityTerms` when set and the
content fields otherwise; `identityTime()` uses `idv().IdentityDate`, falling back
to `m.Date()` exactly as today (so Graph's `applyGraphState` override of
`Received` still feeds a Date-less identity, MA-233). `PhysID`, state, categories
and transport headers stay excluded (MA-145, MA-239). Hand-built messages
(tests, PST without method 5/6) have `IdentityTerms == nil` and behave exactly as
today.

### 4.2 The frozen v1 replay — P1 (`internal/source/parsev1.go`, new)

- **Frozen code (MC7).** `parsev1.go` is a verbatim copy of `parseMessage`,
  `fallbackParse`, `addressList` and `bytesWriterTo`, renamed `…V1`. The copy is
  taken at the release tag and commit named in the file's header and in the
  goldens' provenance: the code that wrote users' archives, not the working tree.
  S1 diffs `mbox.go` against that tag and records the result in the S1 commit. Any
  identity-affecting change found there is reconciled in S1. One such change is
  already known: fault-isolation §4.5 rewrites `safeParseMessage`'s recover and
  stub (OR2). The panic branch is therefore **not** frozen:
  - `replayV1(data) (*model.Message, panicked bool)` runs the frozen functions
    under a recover.
  - When `panicked` is true, the identity terms come from `stubIdentityTerms(data)`
    in a separate file that is not frozen and is owned by fault-isolation (OR2).
    It reproduces the stub that the build which wrote the archive produced. Today
    that is the constant `{Subject:"(unreadable message)"}`. After F33 it becomes
    fault-isolation's raw-preserving `{Subject, PlainBody: string(data)}`, with
    that design's own legacy-stub migration. **[ruling J5, J8]** S1 creates it with
    today's constant stub exactly, so S1 changes no identity. The F33 fold that would
    change it moves MA-330's golden and is escalated (ESC-1); it lands only in
    fault-isolation's S2b-id, after the owner answers.
  - An MA-330 corpus item that panics v1 pins it.

  Fault-isolation's `ReadGaps:[unparsed]` addition to `fallbackParse` is content,
  not identity, so it lands in the fidelity walker's header-fallback branch, not
  in the frozen file. The header comment reads: "FROZEN — identity inputs. Never
  edit except under §5's drift policy (MC28); MA-330/MA-331 pin it (sha256)."
- **Vendored dependencies (MC27).** The replay's dependencies are vendored into
  `internal/source/parsev1/`: go-message v0.16.0's `mail`, `textproto` and
  `charset` packages, with an explicit snapshot of the effective charset label
  table, including go-pst's nameMap (the vendored table no longer sees go-pst's
  init); and the stdlib `net/mail` address and date parser, `mime.WordDecoder`,
  `mime.ParseMediaType` and `mime/quotedprintable`, at the toolchain version named
  in the header. All are BSD-licensed, and THIRD-PARTY-NOTICES lists them. MA-331
  asserts via `go list -deps` that `parsev1` imports none of `net/mail`, `mime` or
  `github.com/emersion/go-message`, so the fidelity walker and the rest of the
  module can take go-message, `x/net` and Go security updates without moving
  identity. **Residual, stated in P1:** `x/text`'s charmap tables, `encoding/base64`
  and the unicode tables are still shared with the toolchain (OR4). **[ruling OR4]** An
  open condition, not accepted: S1 vendors `encoding/base64` too, and MA-331 asserts
  `unicode.Version`, so a toolchain move that touches them goes red.
- **`safeParseMessage(data)` becomes:**
  1. `v1, panicked := replayV1(data)`.
  2. `terms := identityTermsOf(v1)` (or `stubIdentityTerms(data)` when panicked);
     `m.V1Inventory` = each v1 attachment's index, label, size and SHA-256 (V1C,
     §4.10). The v1 attachment byte slices are then dropped, so the peak is up to
     two decoded copies of a large message, transiently, plus v1's body text held
     in `terms` (MC39).
  3. `m := parseFidelity(data)` under its own recover.
     - (3a) **Fidelity ⊇ v1 (MC4).** On success, every v1 attachment with bytes
       needs a fidelity attachment with identical bytes, compared by size and
       SHA-256 against `V1Inventory`. A `text/*` attachment whose declared charset
       v1 converted must convert, through v1's own (vendored) `charset.Reader`, to
       v1's bytes. Every non-empty v1 body field
       needs a non-empty fidelity body. When the v1 field is valid UTF-8 and no
       body-decoding note (`charset-fallback`, `legacy-8bit`, `header-fallback`,
       `lenient-cte`) touches it, the fidelity HTML or plain field must contain it
       verbatim. P3 keeps a single segment verbatim and HTML segments as is, so
       legitimate improvements pass (Apple Mail, Mailman and nested alternatives
       were checked).
     - (3b) **Fallback.** On a panic, or when (3a) fails, re-run `replayV1` and use
       its message as the content (P13), and set `m.FellBack`. Its display fields
       (Subject, names, addresses, body) are run through §4.4's `decodeText`, so
       P7 holds on this path too (MC19). The record gets a terminal
       `fidelity-fallback` gap. Its detail names the bounded panic site, or
       `fidelity-regression` plus the failed check for (3a), which is also noted
       `fidelity-regression` (MC4). The write stamps the current
       scheme and sets `Record.FidelityFallback` to it. A later scheme repairs the
       record, and the same build never loops (MC19, §4.10). `LastRun.FidelityFallback`
       and `status -json` `fidelity_fallback` count these records.
  4. `m.IdentityTerms = &terms`, **always** for raw MIME.
  5. `Raw` and `TransportHeaders` are set as today.

  `ParseRFC822` keeps its signature.
- **Cost (estimates, MC39):** one extra v1 parse per raw-MIME message per run, on
  every run, because local sources re-parse every message. The replay drains
  attachment bodies through the same decoders, so it can reproduce a v1 decoder
  panic exactly. In a review-time scratch measurement, a v1-style drain on Apple
  Silicon ran at 400–670 MB/s, which is about 15–25 CPU-seconds per 10 GB of mbox
  for the replay alone. Low-end hardware has not been measured. There is no extra
  I/O. `BenchmarkParseCorpus` (v1 replay + fidelity
  parse, at three message sizes) ships with S2, and its numbers are recorded in
  the S2 commit.
- **Never** call `charset.RegisterEncoding` or assign `message.CharsetReader`
  anywhere (H3). MA-331 snapshots the effective label table of both the vendored
  copy and the shared one, and asserts they are unchanged after the corpus is
  parsed (MC26).

### 4.3 The fidelity walker — P2, P4, P5, P13 (`internal/source/mime.go`, new)

Built on `go-message/textproto` (`ReadHeader`, `NewMultipartReader`), not on
`mail.Reader`, so no part is ever returned nil and no charset is applied
implicitly.

1. **Top level.** `textproto.ReadHeader` behind the walker's own explicit cap: an
   `io.LimitedReader` of 1 MiB wraps the top-level header and **every part
   header** (MC22). `textproto.ReadHeader` has no cap of its own (its
   documentation leaves the bound to the caller), go-message's 1 MiB limit applies
   only inside `message.Read`, and `NewMultipartReader` reads part headers
   uncapped. A part header that exceeds the cap gives that part a
   `structure-limit` gap and leaves its siblings intact. If the top-level header
   cannot be read, the walker switches to *header fallback* mode (same trigger as
   v1's `fallbackParse`): headers via `net/mail`, decoded by §4.4; the body decoded
   as text; note `header-fallback`. If `net/mail` also fails, the result is
   `(unparseable message)` with the whole input decoded as text (v1's behaviour,
   now valid UTF-8). That case also records fault-isolation's `unparsed` gap, in
   the gap channel OR2 rules on (**[ruling J5]**: `ReadGaps` + `GapClass`; until this
   walker exists the label is added in `safeParseMessage`'s routing). It sits here because
   it is content, not identity (MC7, MC40).
2. **Leaf vs container.** Content-Type parsed strict (go-message's own
   `Header.ContentType()`, so params are RFC 2047-decoded exactly as v1); on
   error, the lenient tokenizer (`mimeparams.go`, below). `multipart/*` →
   container; `message/rfc822` → leaf (not descended, as v1). Depth > 32 or
   leaves > 10,000 → the rest of that container's raw bytes become one
   `remaining-parts.undecoded` attachment + `structure-limit` gap.
3. **Container read.** `NewMultipartReader(body, boundary)`. Per child:
   `NextPart` error that is not `io.EOF` = framing error → stop this container
   (never call `NextPart` on it again), keep what was read, record
   `corrupt-part` ("multipart ended early") if any bytes followed, continue with
   the parent. A container with no recoverable boundary, or one that yields zero
   parts over a non-blank body → its body as a text segment + `unsplit-multipart`
   gap.
4. **Transfer decoding (P4).** Normalize the CTE token: lowercase, trim, strip
   quotes and RFC 822 comments, `7-bit`/`8-bit` → `7bit`/`8bit` (note
   `lenient-cte` when normalization changed it). Decode by building a synthetic
   header `Content-Type: application/octet-stream` + the normalized CTE and
   calling `message.New` on the raw part body: this is go-message's own
   `encodingReader` (quoted-printable; base64 with its whitespace tolerance;
   7bit/8bit/binary pass-through), so bytes are identical to v1 for every
   non-text attachment, and **no charset is applied**. Unknown CTE →
   `message.New` returns the raw body with `UnknownEncodingError`: the bytes are
   kept still-encoded (`<name or part-N>.undecoded`) + `undecoded-part` gap. A
   decode error mid-stream keeps the decoded prefix as `<name>.partial` + a
   `corrupt-part` gap. It is never stored under the real name, so nobody mistakes
   a truncated file for the original (MC38). Each leaf's decode runs under a
   per-part recover: a panic costs that part only (`corrupt-part`).
5. **Classification (§4.5)** decides body segment vs attachment.
6. **Attachment bytes** are the transfer-decoded bytes from step 4, whatever the
   type (`text/*` included). When a `text/*` attachment declared a charset that
   v1 would have converted (anything but `utf-8`/`us-ascii`), note
   `text-attachment-bytes`.

**Lenient parameter tokenizer** (`mimeparams.go`, used only after a strict
failure; note `lenient-params`): split on `;` outside double quotes; key up to
the first `=`, lowercased and trimmed; a quoted value runs to its closing quote
(backslash escapes honoured, the rest of the segment ignored); an unquoted value
runs to the next `;`, may contain `=` and spaces, is trimmed, and loses a
trailing `(comment)`; RFC 2231 `key*=charset'lang'pct` and `key*N`/`key*N*`
continuations are joined by a sparse sort of their indices (an index above 64
drops that continuation; no slice is ever sized by an index) and decoded by
§4.4; first occurrence of a key wins; at most 64 parameters. The input is
bounded by the walker's explicit per-header cap of step 1 (MC22).

### 4.4 Text decoding — P7, P8 (`internal/source/text.go`, new)

- `decodeText(label string, b []byte) (string, []ParseNote)`: normalize the label
  (lowercase, trim quotes/comments). `""`, `utf-8`, `us-ascii`, `ascii`: valid
  UTF-8 → as is; else Windows-1252 (note `legacy-8bit`). A label
  `go-message/charset.Reader` resolves → decode with it (identical to v1's
  conversion). Any other label (`unknown-8bit`, `x-unknown`, `default`, `utf-7`,
  `cp-850`, …): valid UTF-8 → as is, else Windows-1252 (note
  `charset-fallback`, naming the label; for `utf-7` the note says "shown as
  stored, not decoded"). Never fails; never registers anything.
- **Headers** (`Subject`, display names, address fields): if the raw value is
  not valid UTF-8, decode its bytes as Windows-1252 first (encoded-words are
  ASCII and survive); then RFC 2047-decode with a `mime.WordDecoder` whose
  `CharsetReader` is `decodeText` (note `header-8bit` /
  `encoded-word-fallback`). When the raw value is valid and go-message's own
  decoder succeeds, its result is used unchanged (identical to v1).
- **Addresses.** To/Cc/Bcc/Reply-To: strict `AddressList` on the decoded value,
  formatted as v1; on failure, the decoded text (note `address-fallback`). From:
  strict `AddressList`; on failure or an empty list, `SenderName` = decoded raw
  text with the `<…>` part removed and quotes trimmed, `SenderEmail` = the
  contents of the last `<…>`, or the whole value if it has an `@` and no
  whitespace; `<>` gives name `<>`, email empty; both control-stripped
  (`util.StripControl`) and capped at 1,000 runes (note `sender-fallback`).
- **One primitive each (MC35).** These rules live in `internal/util` and every
  site calls them, so copies cannot drift:
  - The Windows-1252 step is `util.DecodeLegacy`, moved from the PST reader's
    `decodeBytes` (`reader.go:517-525`); `decodeBytes` calls it.
  - The lenient From extraction is `util.LenientAddress`. It reconciles
    `interchange.headerAddress` (`interchange/mbox.go:183-201`), which has no `@`
    test and returns `""` on a control character, with the rule above. mbox
    `extract`'s postmark address calls the same function, so it and the page's
    sender always agree.
  - The archive-file gate (regular file, no symlinked component, under the
    computed directory) is `util.ArchiveFileGate`, shared with `extract`,
    `verify` and recapture-integrity's old-zip inventory reader.

### 4.5 Body assembly and naming — P3

**Leaf classification.** A leaf is a *body segment* iff its type is `text/plain`
or `text/html`, its disposition is not `attachment`, and it has no filename
(disposition `filename`, else Content-Type `name`, strict then lenient, RFC
2047/2231-decoded). Every other leaf is an *attachment*: named inline text,
`text/*` other than plain/html (calendar, csv, vcard), inline non-text,
`message/rfc822`, `disposition=attachment`. Exception kept for v1 parity: if a
message has no body segment at all, its first named inline `text/plain`/`html`
leaf is also used as the body (and still archived as an attachment).

**Tracks.** Two segment lists are built recursively, the HTML track and the plain
track. A leaf contributes its segment (body text) or an attachment reference. A
`multipart/alternative` contributes, for the HTML track, its *first* child whose
HTML track holds an HTML segment, and for the plain track its first child whose
plain track holds a plain segment (v1 took the first HTML and first plain leaf in
document order, so single-body mail is unchanged); alternatives are never
concatenated. Every other multipart contributes all children in order. Blank
segments are dropped.

- `HTMLBody` = the HTML track when it holds ≥ 1 HTML segment. One segment →
  that HTML verbatim (identical to v1). Several → concatenated in order: HTML
  segments as is (x/net/html merges repeated `<html>`/`<body>` wrappers, so the
  result renders as one document — verified by MA-335's render assertion), plain
  segments as `<pre style="white-space:pre-wrap">escaped</pre>`, and an
  attachment reference that sits *between* two text segments — and is not an
  image whose Content-ID an HTML segment references — as
  `<p>[Attachment: NAME]</p>` (NAME escaped). Note `multi-segment-body`.
- `PlainBody` = the plain track's plain segments joined by a blank line, with
  between-segment references as `[Attachment: NAME]` lines; HTML segments in the
  plain track are not converted. One segment → verbatim.
- The page shows `HTMLBody` when non-empty, else `PlainBody` (`selectBody`
  unchanged); the index keeps `PlainBody`-preferred (`index/htmltext.go`).
- Apple Mail `alternative[plain, mixed[html1, pdf, html2, notes.txt, ""]]` →
  page `html1 [Attachment: Quarterly Report.pdf] html2`, zip holds both files,
  index holds the full plain alternative. Mailman `mixed[alternative[plain,
  html], plain-footer]` → page shows the HTML then the footer in a `<pre>`.

**Names** (note `inline-name` when the result differs from v1's): filename →
Content-ID (v1's rule for CID-bearing parts, unchanged) → for `message/rfc822`,
`<decoded Subject of the embedded header block, ≤ 100 runes>.eml`, else
`attached-message.eml` → otherwise `inline` (inline parts) or `attachment-N`
(others) + an extension from a fixed internal type→extension table (pdf, png,
jpg, gif, ics, eml, txt, html, csv, xml, zip, doc/docx, xls/xlsx, ppt/pptx, …),
never `mime.ExtensionsByType` (host-dependent, so names would differ between
machines). Names are control-stripped. A named inline text part becoming an
attachment notes `named-inline-text`.

**Final names, once (MC33).** `SanitizeFilename` + `uniqueName` still make the
zip entry (R4). They now run once, before rendering, and the result is stored in
`Attachment.ZipName`. The page's Attachments line, the `[Attachment: NAME]`
placeholders and the gaps block all print `ZipName`, so the page never names a
file the zip lacks.

- **Nested entries.** For `<subject> - <name>` the subject prefix is capped at 60
  runes, so the attachment's own name and extension always survive the 120-rune
  segment cap.
- **Reserved suffixes.** The suffixes `(attached message).html`, `.undecoded` and
  `.partial` are reserved for tool-made entries. A sender's file whose name ends
  in one gets ` (sender file)` inserted before its extension, so an inheritor can
  trust these suffixes.
- **Listing.** The outer page lists the entries the tool rendered.

### 4.6 What is archived, and where

| Content | Archived as | Where |
|---|---|---|
| Any attachment (incl. `text/*`, named inline text, inline non-text, `message/rfc822`) | transfer-decoded bytes exactly as sent; never charset-converted | zip entry, real name |
| Body text parts (unnamed inline `text/plain`/`text/html`) | decoded to valid UTF-8 (§4.4), assembled in order (§4.5) | the page body; the index |
| The whole original message | its raw bytes, unchanged | `<stem>.eml`, only with `-raw` (unchanged) |
| Part with an unknown transfer encoding | its still-encoded bytes as received | zip `<name or part-N>.undecoded`; a text part is also shown escaped in the body; `undecoded-part` gap |
| Part whose transfer encoding is corrupt or truncated | the decoded prefix | zip `<name>.partial` (MC38); `corrupt-part` gap (fillable on on-demand sources, MC20) |
| Multipart whose boundary cannot be recovered | its body bytes, decoded as text | the page body; `unsplit-multipart` gap |
| Past a structure bound | the remaining raw bytes | zip `remaining-parts.undecoded`; `structure-limit` gap |
| PST attach method 5 (attached item) | the item rendered by the same renderer + its attachments' bytes | zip `<subject> (attached message).html` and `<subject> - <name>` |
| PST method 6 (OLE), unopenable/non-mail method 5, depth/cycle/budget stop | nothing (no junk bytes) | `ole-object` / `embedded-item` / `structure-limit` gap |
| A message the fidelity parser could not handle (panic, or "fidelity ⊇ v1" failed) | v1's content, display text decoded per §4.4 | the page and zip as v1 wrote them; terminal `fidelity-fallback` gap (MC4, MC19) |
| Headers | decoded display text (§4.4); the raw block as stored | the page header; the transport panel (unchanged) |

### 4.7 Renderer — P6, P10 (`internal/export/html.go`)

- `RenderWith` stops calling `embedInlineImages` on the selected body. For HTML,
  embedding moves onto the parsed tree inside `sanitizeHTML`'s walk: `img@src`,
  `input[type=image]@src`, any element's `background` attribute, `url(…)` inside
  `style` attributes and `<style>` text. A reference matches when the value,
  trimmed and with `cid:` matched case-insensitively (as v1), equals
  `cid:<Content-ID>` (angle brackets stripped) **as a whole token** — v1's regex
  matched prefixes (`cid:abc` inside `cid:abcdef`). Consumed = embedded in an image
  context; everything else stays in the zip. `<a href="cid:…">` is left as is
  (inert). Plain and RTF bodies are never embedded into. `unresolvedInlineRefs`
  counts only image-context references. The concurrent `BodyFallback` rule
  (fallback resets `consumed`) is kept.
- **Data URIs (MC23).** The embedded value is built only for an allowlisted
  image type: the lowercased `MimeType` must match the RFC 2045 token grammar and
  be one of `image/png`, `image/jpeg`, `image/gif`, `image/webp`, `image/bmp`,
  `image/x-icon`. Anything else is not embedded; it stays in the zip and counts
  as unresolved. For PST, `MimeType` is an arbitrary `PidTagAttachMimeTag`
  string, and for raw MIME, go-message returns the raw header on a parse error, so
  an unvalidated type could carry `</style>…`. The URI is inserted quoted, and its
  alphabet (`[A-Za-z0-9+/=;:,.-]`) needs no CSS escape. MA-301's re-parse of the
  serialized page is extended to **fail closed** (to the escaped-source body) when
  a reserved `mailarchive-*` class or `data-mailarchive-*` attribute appears
  outside tool-owned markup. This backstops any future tree-embedding bug, since
  insertion now happens after `neutralize`.
- **Isolation (MC14).** The tree embedding and the two blocks below run under a
  recover. A panic re-renders the page with those features off (v1-style regex
  embedding is **not** restored; images stay in the zip) and adds a
  `render-fallback` note.
- `renderHeader` gains two blocks after the attachments line. Both are
  tool-owned: reserved `mailarchive-*` classes, all text escaped, and no
  `data-mailarchive-field`, so `reindex -rebuild`'s from-HTML reader ignores them.
  - A **visible** `mailarchive-gaps` block: "Not fully decoded:" plus one line per
    parse gap, naming files by their final `ZipName` (MC33). Example: "report.pdf
    — transfer encoding "x-uuencode" is not supported; kept as received in the
    zip as report.pdf.undecoded". It also covers the `fidelity-fallback` and
    `identity-stub` gaps (MC6, MC19).
  - A **collapsed** `<details class="mailarchive-notes">` "How this message was
    decoded" block. It serves P7's condition (MC25): wherever a guess or a
    fallback shaped the displayed text, the page says so. It lists only notes that
    qualify displayed text: `charset-fallback`, `legacy-8bit`, `header-8bit`,
    `encoded-word-fallback`, `sender-fallback`, `header-fallback`,
    `render-fallback`. Notes about structure (`lenient-params`, `lenient-cte`)
    appear only in the report.
- `WriteZip` (`attach.go`) handles parse gaps and attached items. The nil
  guards land in S5, before any producer of nil `WriteTo` (MC30):
  - It skips an attachment with `GapKind` and nil `WriteTo`; `drain()`
    (`html.go:673`) and `hasArchivable` (`exporter.go:773-780`) skip it too.
  - It writes still-encoded bytes for one with `GapKind` and non-nil `WriteTo`.
  - For `Embedded`, it writes `RenderWith(embedded, RenderContext{})` as the
    entry, then the embedded message's non-consumed attachments as
    `<subject> - <name>` entries (recursively, bounded by §4.8). Each embedded
    render runs under its own recover, and a panic becomes an `embedded-item` gap
    on that attachment, never a lost outer message (MC14).

### 4.8 PST attach methods — P9 (`internal/source/reader.go`)

`collectAttachments` reads `GetAttachMethod()` and the **property type of 0x3701**
per attachment (MC31). The file's writer controls the method value, so the type
decides: `PtypObject` is an embedded item (below), and `PtypBinary` is archived as
bytes exactly as today, whatever the method says.

- **1 (by value)** — unchanged.
- **5 (embedded message, 0x3701 `PtypObject`)** — never call go-pst `WriteTo`.
  Interim (slice S5):
  `GapKind=embedded-item`, detail = `PidTagDisplayName` + "attached Outlook item
  not exported by this version", `WriteTo=nil`. Full (slice S6): read the 0x3701
  heap item's 8-byte `{subnode NID, ulSize}`; `pst.FindLocalDescriptor(nid,
  a.LocalDescriptors)`; `file.GetHeapOnNodeFromLocalDescriptor`;
  `file.GetPropertyContext`; `file.GetLocalDescriptorsFromIdentifier(ld.LocalDescriptorsIdentifier)`;
  assemble a `pst.Message`, populate `properties.Message`, and run it through
  `convertMessage` under its own recover. On success,
  `Embedded = <converted message>` and
  `Filename = "<subject or display name> (attached message).html"`. When
  `convertMessage` returns nil (appointment, contact, task), the attachment gets
  an `embedded-item` gap naming the message class.

  Bounds (MC31):
  - depth ≤ 4, and ≤ 256 embedded items per top-level message;
  - a visited set per top-level message keyed on the **block ids**
    (`LocalDescriptorsIdentifier`, `DataIdentifier`), not on NIDs. Subnode NIDs
    are local to each subnode tree and recur legitimately at every nesting level;
    a real cycle re-enters through a block id;
  - `ulSize` is advisory and never sizes an allocation;
  - a per-top-level-message budget of ≤ 10,000 embedded zip entries, decoded
    bytes ≤ 4 × the PST file's size, and a per-embed step budget, because a hang
    inside go-pst is not caught by recover.

  A cycle, depth or budget stop gives `structure-limit`. Each go-pst error class
  maps to a named, counted `embedded-item` detail: `ErrLocalDescriptorBranchNode`
  ("large attached item: go-pst cannot read branch sub-node trees"), not found,
  and invalid signature.
- **6 (OLE)** — `GapKind=ole-object`, detail = display name; `WriteTo=nil`.
- **2/3/4/7** — unchanged (non-goal).

**S6 gate (MC32).** S6 may not merge until MA-343 is tier U on a committed,
Outlook-written method-5 fixture (Q2). Until then S5's terminal gap is the
shipped floor, and P9's full form is aspirational.

Identity (P1): the PST content fields other than these attachments' `Filename`
are unchanged. When any method-5/6 attachment's new name differs from v1's
(`v1AttachmentName`, the frozen long-name/short-name/`attachment-N`+ext rule),
`convertMessage` sets `IdentityTerms` to the content fields with v1's
attachment names; otherwise it leaves it nil. Note `embedded-gap` (S5) or
`embedded-message` (S6) on such messages. For V1C (§4.10), the PST v1-now
inventory is go-pst `WriteTo`'s bytes under `v1AttachmentName`, junk method-5/6
bytes included, so an old junk entry matches v1-now and is then deliberately
replaced by a gap.

**Dependency (MC31).** S6 calls go-pst v6.0.2 internals that no current code
reaches, on attacker-controlled NIDs. v6.0.2 was tagged 2023-06-13 and is the
latest release. The owner of this design owns the go-pst dependency. If a
reachable go-pst CVE has no upstream fix, the plan is a minimal patch in a fork
pinned by a `replace` directive, listed in THIRD-PARTY-NOTICES, with MA-330
(nameMap) and MA-342/343 kept green. S5's gap remains the floor if the fork
cannot be made safe.

### 4.9 Gap classification and recording — P10

- **One gap channel (MC40, OR2).** Fault-isolation also creates a message-level
  gap channel: `model.Message.ReadGaps` with bare labels classified through
  `state.GapClass`. The two designs adopt **one** primitive, chosen by OR2, and
  S1 does not merge before that ruling. The proposed ruling is fault-isolation's
  `ReadGaps` + `GapClass`, with this design's kinds registered as class
  `undecodable`, informational (never `partial`, never exit 3).
  `Attachment.GapKind` stays only as the per-attachment pointer. F23's
  attachment-table error uses fault-isolation's fillable `unreadable:attachments`,
  not `GapKind`. The rest of this section describes the encoding this design
  proposed (`ParseGaps`, NUL-prefixed `Terminal` labels); OR2 replaces it if it
  rules otherwise. The classes, bounds, counts and wording below hold either way.
  **[ruling J5]** OR2 rules for the proposal: fault-isolation's `ReadGaps` + `GapClass`,
  together with `probeImproves`, are the single owner of record gap labels, and this
  design's kinds register there as informational, never `partial`. S1 reconciles the
  label encoding below with `GapClass`'s grammar, so there is one grammar; the classes,
  bounds, counts and wording hold unchanged. Fault-isolation S1a owns the
  `Counts()`/status rewrite, and S2 adds this design's kinds to it as new cases.
- **Class per kind, by R1's source rule (MC20).**
  - `undecoded-part`, `structure-limit`, `embedded-item`, `ole-object`,
    `identity-stub` (MC6) and `fidelity-fallback` (MC19) are deterministic. They
    go to `rec.Terminal` on every source, regardless of `SourceComplete`
    (`exporter.go:405-410`).
  - `corrupt-part` and `unsplit-multipart` can be truncation in an on-demand cache
    that a later complete copy cures. They follow `SourceComplete` like every
    other gap: `rec.Missing` (fillable) on on-demand sources, `rec.Terminal` on
    Graph. Their probe in `probeImproves` is "a re-parse no longer yields this
    gap".

  The label is `"\x00" + kind + "\x00" + detail` (detail control-stripped, ≤ 300
  runes).
- **Bounded (MC21).** At most 32 parse-gap labels per record, plus one summary
  label `"\x00structure-limit\x00and N more undecodable parts"`. The page's gaps
  block lists them all, because it is bounded by the message itself. The leading
  NUL cannot start a label this build produces otherwise (names are
  control-stripped, §4.5). A v1-era attachment label beginning with NUL (only via a
  crafted RFC 2231 `%00`) would be mis-kinded in the report: cosmetic, and only
  for the attacker's own message. Storing in `Terminal`/`Missing` (not a new field)
  means an older binary keeps, counts and reports the gap (as an
  `empty-attachment` row whose detail reads ` undecoded-part …`), and
  `Complete()`/`HasIssues()` hold unchanged.
- `probeImproves` never sees the terminal kinds (it reads `Missing` only), so a
  terminal parse gap is never retried. It is left only by a repair under a later
  scheme (P11).
- **Report** (`app/report.go`): `gapKind`/`gapDetail` decode the prefix; rows
  carry the kind and its class. Rows are capped per record the same way (32 +
  one summary row) (MC21).
- **Counts:** `Manifest.Counts()` stops counting a record as `terminal` when all
  its terminal labels are parse gaps; a new `Manifest.UndecodableCount()` counts
  records with ≥ 1 parse gap. `Result.Undecodable`, `LastRun.Undecodable`
  (`json:"undecodable,omitempty"`), `health.Input.Undecodable`,
  `status -json` `undecodable`. Posture is unchanged: terminal and undecodable are
  informational, as `terminal` is today, and the fillable parse kinds count with
  the other fillable gaps. This matches the classification OR2 is asked to adopt
  (MC40).
- **Wording** (X2: what / why / remedy). Run summary:
  `Verification: … · M message(s) with parts this version cannot decode (kept as
  received; see the report)`. `status`: `Incomplete: X still missing content · Y
  source-empty (never fillable) · M with parts this version cannot decode · W not
  yet re-examined`. W keeps its existing meaning, the legacy Unknown count
  (`health.go:573`). The parser-migration count is a separate line, §4.10's
  `not_reexamined` (MC16). The GUI summary
  (`cmd/mailarchive-gui/main.go:1164-1166`) gets the same split, so "Empty at the
  source" no longer counts parse gaps. These are user-visible surfaces, and S2
  owes their friction review and X-series check (MC39).

### 4.10 Repair — P11 (`internal/export/exporter.go`, `internal/state/manifest.go`)

*Gate (MC43):* S7 builds only after Q1 and OR3 are ruled and this section's
revision-2 text has passed a delta re-review (lenses 3, 6, 7, 8). The commit
primitive below (rename-over, staged) holds only if OR3 keeps repair in place. If
OR3 adopts recapture-integrity's write-beside commit, repair commits through it
instead. Either way, repair uses only recapture's UNCHANGED, COMMIT or KEEP
outcomes, never MERGE, and hands it V1C's v1↔fidelity correspondence as its
inventory (MC41).

**[ruling Q1, J6]** Q1: repair is automatic. OR3 (= J6): a repair has no commit path of
its own. It is a re-capture through recapture-integrity's verdict (its §4.3) and write
path (W1–W5), with no in-place exemption in that design's R5 amendment. Its file names
are whatever that commit gives (the stem, or its `-vN` successor if ESC-3 is answered
(a)), and "never to less" is that design's R28, which R27 cites while keeping V1C's own
preconditions (the archive files as recorded; the same message). So the stem gate's
**no-fallback** rule stands (a repair never takes a stem from the decoded subject), but
the "In place" promise, the staged rename-over of "Write discipline", the W2 row of §5
and the "rewritten in place" wording of "Accounting" below are superseded. They are
rewritten through W1–W5 before RR-4, the delta re-review this gate names. The manifest-v7
clause is moot: the version stays 6 [ruling J2].

**Scheme**
- `Record.ParseScheme int` (`json:"parse_scheme,omitempty"`). 0 means the record
  was written by a build before this design and has not been re-examined since.
- **Stamping.** Every write stamps `state.ParseSchemeCurrent`. A fidelity-fallback
  write also stamps the current scheme, and sets `Record.FidelityFallback` to that
  scheme (MC19).
- **Stamp on re-read (MC12, MC16).** A seen record re-read this run whose
  predicate is false gets `ParseScheme = current` in memory. The existing
  checkpoint saves it; no file is written. A withheld record is not stamped.
  `ParseScheme < current` therefore means exactly one of two things: not
  re-examined by this scheme since the upgrade, or withheld.
- **Bumps.** Each slice that changes what is written bumps the scheme by one, and
  registers its note codes with that integer as their `Since` value (the table
  below gives the slice that introduces each code). Notes are computed on every
  parse, with no extra I/O.
- **Revocation (MC18).** `state.RevokedSchemes []int` is a compiled-in list, empty
  at S7. A later build adds a scheme to it when it declares that scheme defective.

**Predicate**
- **Where it runs.** On the seen path when the record is not being filled: in
  incremental mode (`exporter.go:235`) **and in `-mode full` (MC9)**.
- **When it fires.** `repair` is true if any of these holds:
  - `prev.ParseScheme < ParseSchemeCurrent` and some note `n` in
    `m.ParseNotes ∪ renderNotes(m)` has `n.Since > prev.ParseScheme`;
  - `prev.ParseScheme ∈ RevokedSchemes`;
  - `0 < prev.FidelityFallback < ParseSchemeCurrent` (a later scheme may now
    parse the message).
- **When it never fires.** When this run's own parse fell back (`m.FellBack`;
  no rewrite loop, MC19), or when this run's v1 replay panicked (MC6).
- **`renderNotes(m)`** adds `cid-text-ref` (Since = S4's scheme) in three cases:
  - the selected body is not HTML and contains `cid:<CID>` of an attachment;
  - the body is HTML and contains a `cid:<CID>` occurrence not immediately
    preceded (after optional quote or space) by `src=`, `url(` or `background=`;
  - **the body contains `cid:<CID>` as a strict prefix of a longer token** (v1's
    prefix match consumed an attachment whose CID is `abc` for `src="cid:abcdef"`,
    MC24).

  This is an over-approximation: a false positive costs a V1C check and, when the
  output is unchanged, only a restamp.
- **Guard against missed notes (MC24).** MA-332 (parser-scoped, MC29) catches a
  missed note registration over the corpus: a message with no note must parse to
  its content golden.
- **Full mode (MC9).** A seen record whose predicate fires takes this repair
  branch (V1C, kept stem, `.eml` carried forward, `Repaired` count) instead of
  the ordinary re-capture. Every other full-mode record keeps today's
  behaviour, which is F46's unguarded re-capture. Like every write, it uses the
  identity-view stem (MC8).
- **Fills.** A fillable record whose probe improves is filled as today (and
  stamped), with the identity-view stem (MC8). The probe matches old Missing
  labels against v1-now labels through the replay: a v1 label (`inline`,
  `attachment-3`) counts as improved when its v1-now attachment has bytes, and the
  record then carries fidelity labels (MC34).
- **Neither.** A record that is neither filled nor repaired is skipped as today,
  including the timeline stamp (MC2).
- Repair ignores `-since`, like a fill.

**Precondition V1C (never to less; H4).** Every step must pass. A failure
withholds the repair with the step's reason (MC15).

1. **Replay inventory (MC3).** `m.V1Inventory` (§4.2) gives each v1-now
   attachment's index, `attachmentLabel`, size and SHA-256, and the v1-now
   `ContentDigest` comes with it. replayV1 already drains every v1 attachment, so
   this costs no extra I/O. For PST, v1-now is go-pst `WriteTo` under
   `v1AttachmentName`, junk method-5/6 bytes included (§4.8).
2. **Archive side (MC1).** Through `util.ArchiveFileGate` (MC35: regular file, no
   symlinked component, under the computed directory):
   - the old page at `prev.Path` must exist and match `Fixity.HTML` when that is
     recorded;
   - when `Fixity.Zip` is recorded, the zip must exist and match it;
   - a recorded `.eml` must exist.

   Read the zip's central directory, then decompress each entry within the
   extraction bounds (≤ 65,535 entries; per-entry, total and ratio caps) to get its
   size and SHA-256. A missing, unreadable or altered file →
   `archive-file-missing-or-modified`; a bound → `over-bound`. Deleting a
   message's page is how an owner redacts it (README.md:428-433, MA-211), so a
   repair never brings a removed page back. A rotted file is reported, never
   silently re-baselined (MC1).
3. **Source unchanged (MC3).** Two checks:
   - Every old zip entry is matched one-to-one, by (size, SHA-256) with names
     ignored, by a v1-now attachment or a fidelity-now attachment. Matching
     fidelity-now as well avoids false withholds on records an older binary
     re-stamped.
   - Every v1-now attachment with a Content-ID that `prev` does not list in
     Missing, Terminal or Unresolved still has bytes. This covers images v1
     embedded in the old page.

   An old PST junk entry matches v1-now's identical junk bytes. It is replaced by
   a gap only when 0x3701 is `PtypObject` (MC31). A failure → `source-changed`.
   This is what makes P11's "holds at least what the archive holds" true. A
   recovered item (a cid image no longer consumed, a named inline text part, an
   F24 later part, an S6 nested entry) can no longer mask a lost one, and a
   Thunderbird-detached part, whose name is the same but whose bytes are not,
   fails the match.
4. **Fidelity ⊇ v1-now (MC4).** This is checked at parse time on every capture
   (§4.2 step 3a). A capture that failed it fell back, and the predicate never
   fires on a fallback, so a repair never writes a capture holding less than v1.
   The reason, when it applies, is `fidelity-regression`.
5. **Same message (MC5).** Fingerprint deliberately excludes bodies and bytes
   (`message.go:146-161`), so an equal key and fingerprint do not prove the
   archived message is this one. One of these must hold:
   - a `sha:` key proves it (the body is in `contentHash`);
   - otherwise, when `prev.ContentHash` is present (written on every record from
     S1, §4.1), it must equal v1-now's `ContentDigest`;
   - otherwise (a legacy local `mid:` record), the normalized `.mailarchive-body`
     text of the archived page, read by `readArchivedHTML` (the reader
     `reindex -rebuild` uses), must equal the same extraction from v1-now's
     selected body.

   A failure → `cannot-confirm-same-message`. This also catches a truncated body
   that is still present.
6. **Never a stub (MC6).** A record whose v1 replay panicked is never repaired;
   it carries the `identity-stub` gap.

The rule "on a deterministic source `new ≥ old` holds by construction" in
revision 1 is withdrawn (MC3).

**Unchanged means restamp (MC12)**
- After V1C passes, the fidelity page and zip are built in memory (the staged
  temps of the write discipline) and digested.
- If both digests equal `prev.Fixity.HTML` and `Fixity.Zip`, the temps are
  discarded and `ParseScheme` is set to current in memory. There is no file write,
  no history event and no `Repaired` count; the record is counted `Restamped`.
- Render and zip are byte-deterministic within one toolchain: `html.go` reads no
  clock, `zw.Create` writes zero mtimes, and `uniqueName` is deterministic.
- So an older binary that drops `parse_scheme` costs this build CPU only: zero
  rewrites, zero verify windows, zero folder-assertions.

**In place (MC8, MC10, MC11)** — **[ruling J6]** superseded as a commit mode: the kept
fields, the stem gate and the `.eml` carry-forward below still hold, but `Path` is the one
re-capture's commit gives (the stem, or its `-vN` successor under ESC-3 (a)), so "a repair
never renames … a file" becomes "a repair never takes a new stem from the decoded subject".
- **Kept fields.** Key, `Fingerprint`, `FpScheme`, `ContentHash`, `FirstFolder`,
  `FirstSeen`, `AlsoFiles` and `Path` are kept: key, fingerprint and hash by P1;
  the others carried as on any re-capture.
- **The stem gate.** The file stem is the basename of `prev.Path` when it passes
  the gate:
  - a clean single segment ending `.html`;
  - under the directory the exporter computes from the store token and folder;
  - `KeyForPath(prev.Path) == key`, the same ownership test `stemFor` applies
    (`exporter.go:541`), so a Path that names a sibling record's stem is refused.
- **Gate failure.** The repair is withheld as `unsafe-path`. There is **no**
  fallback to `stemFor`, so a repair never renames and never deletes a file
  (MC10). Since S1, every write takes its stem from the identity view (MC8), so a
  record written by this build or by a v1 build already has the stem a repair
  keeps. A decoded subject never renames a file: there is no N2 window, and links
  and index rows stay valid.
- **The preserved original (MC11).** A repair never writes an existing `.eml`. The
  `.eml` and its `Fixity.EML` are always carried forward. A new one is written
  only where none existed and the run has `-raw` (R20). Thunderbird rewrites
  `X-Mozilla-*` headers and detached parts in place, so today's source bytes are
  not assumed equal to the preserved original.

**Write discipline (MC13, MC14)**
1. **Stage.** Write the zip and html temps first and digest them. A staging
   failure discards the temps, keeps the old record and files, does not stamp,
   counts `RepairFailed` (with the path and the OS error), and returns nil. It is
   never an `Export` error, so the store walk and the Graph mailbox continue and
   the gone sweep still runs.
2. **Commit.** Rename the zip, then the html. When the repaired capture has
   nothing archivable, the stale zip is removed only **after** the html commit.
   Every residual is then old-or-new, never missing (§5).
3. **Rename failure.** This includes the Windows read-only attribute or a handle
   held by the reader or an AV scanner. The old record is kept (W2's known
   residual, §5), the repair is counted `RepairFailed`, and the walk continues. A
   repair never enters the capture path's write-error handling
   (`exporter.go:383-391`), so it never invents Failed/Terminal gaps over an
   intact old zip.
4. **Back-off.** After 20 consecutive `RepairFailed` in one run, no further repair
   is attempted in that run, and one warning is printed.
5. **Recover.** The whole attempt (V1C reads, render, staging, commit) runs under
   its own recover. A panic → withheld `repair-error` + count (MC14).

**Timeline on every non-commit outcome (MC2).** Withheld, failed and restamped
outcomes first run the seen-skip branch's bookkeeping exactly as a skip does
today (`exporter.go:236-262`). On `DedupMailboxWide` that means
`MergeFields(key, folderKey, seenAt, true)` and
`OnManifestSkip(key, folderPath, assert)`. `LastSeen` and `Present` advance and a
move is followed, so `SweepGone` (`state/manifest.go:956-971`) never marks a
present message gone (R21).

**Withheld, legibly (MC15).** Each reason has its own counter and its own
what / why / remedy:

| Reason | From | Operator line |
|---|---|---|
| `source-changed` | V1C 3 | "kept N archived message(s) unrepaired because the mail source no longer holds what the archive holds (for example an attachment no longer downloaded, or detached in the mail app); the archived copies are unchanged — make the mail available offline in your mail app and re-run" |
| `fidelity-regression`, `repair-error` | V1C 4, recover | "N message(s) were not repaired because this version could not reproduce what the archive already holds; the archived copies are unchanged; the report lists them — please report it" |
| `cannot-confirm-same-message` | V1C 5 | "N archived message(s) share their envelope with a different message in the source and were left as archived" |
| `archive-file-missing-or-modified` | V1C 2 | integrity finding, routed to `verify` and `status`: "the archived page or attachments file for N message(s) is missing, unreadable or not as recorded (path); run verify and restore it from backup; if you removed it on purpose, run reindex" |
| `unsafe-path` | stem gate | integrity finding, as above, naming the path: "a manifest path does not belong to its record (path); run verify" |
| `over-bound` | V1C 2 | "N archived message(s) are too large to check safely and were left as archived" |
| `RepairFailed` | write discipline | "could not rewrite N archived message(s) (path: OS error); the archived copies are unchanged; check the permissions of the archive folder" |

- **Persistence.** `Record.RepairWithheld {Scheme, Reason, SourceDigest}`
  (omitempty). `SourceDigest` hashes the v1-now (size, SHA-256) multiset and the
  v1-now `ContentDigest`. The replay recomputes it for free on later runs, and
  while it is unchanged the archive read and the warning are skipped. If an older
  binary drops the marker, the cost is one re-check.
- **Where it shows.** The report lists withheld records (subject, date, path,
  reason). `status`, `status -json` (`repair_withheld` by reason), the GUI summary
  and the Desktop health view show the counts. A warning prints only when a count
  or the scheme changes.
- **Never restored silently.** A missing archive file is never restored from the
  source except by an explicit operator action, so a file the owner removed on
  purpose is never brought back.

**Accounting (MC16, MC17)**
- **Counters.** A repair increments `Repaired`, **never `Exported`**. Every
  surface reads "exported 0 new · repaired N": the `Done.` line, the GUI
  progress, the Desktop card ("Archived — 0 new message(s) · N repaired") and the
  Activity log. On the live path a repair appends one folder-assertion via
  `OnExported` (same folder; inert in the fold).
- **Run summary.** `repaired=N restamped=R` in the `Done.` line, and once per run:
  "repaired N message(s) archived by an earlier version (they lost split bodies,
  attachment names, text-attachment bytes, legacy characters, cid placeholders or
  Outlook attached items). Their files were rewritten in place with fresh
  fixity; the list with old and new digests is in `repairs-<run-id>.tsv`. A
  checksum list you keep outside the archive needs refreshing for these files".
- **Repair log.** `repairs-<run-id>.tsv` (key, path, old `Fixity`, new `Fixity`,
  note codes) is written beside the report, only for runs that repaired
  something, and named in the summary and the Activity log. So a custodian with
  external baselines, offsite copies or workspace hand-overs (`SHA256SUMS`) can
  attribute every re-baselined file.
- **Last run.** `LastRun.Repaired`, `LastRun.Restamped`,
  `LastRun.RepairWithheld` (by reason), `LastRun.RepairFailed`,
  `LastRun.FidelityFallback` (all omitempty).
- **Migration progress (MC16).** `status` and `status -json` gain
  `not_reexamined`: the records with `ParseScheme < current` that are not
  withheld, which stamp-on-re-read makes exact. Each source gets its own remedy:
  - local sources: "the next run re-examines them";
  - Graph: "N live records were captured by an earlier parser; only `-mode full`
    re-examines them (it re-downloads the mailbox)";
  - MailArchive Desktop: "Desktop cannot re-check them without a full
    re-download" (OR1; escalated as ESC-6 [ruling ESC-6], so this line stays until the
    owner answers).

  The extraction workspace's caveat for older records is keyed on the same field
  (MC36).
- These are user-visible surfaces. S7 owes their friction review (§10).

**Fidelity notes (repair triggers):**

| Code | Introduced by (its `Since` is the scheme integer that slice gets when it lands) | Fires when (the archived v1 form differs) | On page |
|---|---|---|---|
| `charset-fallback` | S2 | a part's charset label is unknown to go-message | yes |
| `legacy-8bit` | S2 | unlabelled / `utf-8` / `us-ascii` text is not valid UTF-8 | yes |
| `lenient-cte` | S2 | a CTE needed normalization | report only (MC25) |
| `lenient-params` | S2 | strict Content-Type/Disposition parsing failed | report only (MC25) |
| `continued-after-error` | S2 | v1 would have stopped at this part | no |
| `text-attachment-bytes` | S2 | a `text/*` attachment declared a converting charset | no |
| `header-fallback` | S2 | v1 would have used `fallbackParse` | yes |
| (any parse gap) | S2 | `undecoded-part`, `corrupt-part`, `unsplit-multipart`, `structure-limit` (and `fidelity-fallback`/`identity-stub`, which never trigger a repair themselves, §4.10 predicate) | gap block |
| `multi-segment-body` | S3 | a track holds ≥ 2 segments | no |
| `named-inline-text` | S3 | a named inline text part is now an attachment | no |
| `inline-name` | S3 | an attachment name differs from v1's | no |
| `header-8bit`, `encoded-word-fallback` | S3 | §4.4 header rules changed a value | yes |
| `sender-fallback`, `address-fallback` | S3 | §4.4 address rules changed a value | `sender-fallback` only |
| `cid-text-ref` | S4 | render-level, see predicate (incl. the prefix case, MC24) | no |
| `embedded-gap` | S5 | a PST method 5/6 attachment (interim) | gap block |
| `embedded-message` | S6 | a PST method 5 attachment opened | no |

(S1 bumps the scheme to 1 and registers no note. It routes identity, stems (MC8) and
`ContentHash` (MC5) only, and stamps re-read records (MC12).)

### 4.11 Seams and alternatives considered

- **Fault isolation (F33, F23) — OR2, MC7, MC40.** Fault-isolation §4.5
  (design-fault-isolation.md:523-527) rewrites `safeParseMessage`'s recover and
  stub as `{Subject:"(unreadable message)", PlainBody: string(data), Raw: data,
  ReadGaps:[unparsed]}`. Revision 1 froze exactly that recover, so the landing
  order would have decided whether F33 is undone or blocked. Revision 2 removes
  that dependence:
  - The panic branch is outside the freeze. Stub identity comes from the
    non-frozen `stubIdentityTerms(data)`, which fault-isolation owns, and which
    reproduces the stub of the build that wrote the archive. F33's identity change
    is an edit to that one function, with fault-isolation's own legacy-stub
    migration, never an edit to `parsev1.go`.
  - Fault-isolation's `ReadGaps:[unparsed]` on `fallbackParse` lands in the
    fidelity walker's header-fallback branch (§4.3 step 1).
  - Merged content rule: if the fidelity parse succeeded, use fidelity content
    and record no `unparsed` gap (when v1 panicked, the record carries
    `identity-stub`, MC6). If both panicked, use fault-isolation's raw-preserving
    stub plus `unparsed`.
  - F23's attachment-table error uses fault-isolation's fillable
    `unreadable:attachments`, not `Attachment.GapKind`.
  - Both designs adopt **one** gap channel and one label grammar, and this
    design's counts are informational under fault-isolation's partial/exit-3
    partition (§4.9).

  What remains is a joint ruling across the two designs (**OR2**: gap primitive,
  ownership of `stubIdentityTerms` and of the `Counts()`/status rewrite, landing
  order, Q5 restated). S1 does not merge before it. **[ruling J5]** Ruled: the gap
  primitive is fault-isolation's `ReadGaps` + `GapClass`; fault-isolation owns
  `stubIdentityTerms` (S1 creates it with today's stub) and the `Counts()`/status rewrite
  (its S1a); the `unparsed` label lives outside `parsev1.go`; the order is fault-isolation
  S1a → this design's S0 → S1 → fault-isolation S2b. The F33 identity fold is ESC-1
  [ruling J8].
- **Re-capture integrity (F46, N2, N3) — OR3, MC41.** Revision 1 said P11's
  precondition was "stricter than, and independent of, F46's general rule". That
  was false: a count is weaker than recapture-integrity's per-item KEEP/MERGE, and
  rename-over contradicts that design's R5 amendment ("new files … beside the old
  ones"). Revision 2 states the actual relationship:
  - V1C compares a SHA-256 per entry. That is stronger than recapture's T2/T3
    (labels and names, no sizes) for this case, so it is offered as the
    T1-equivalent inventory. The correspondence it carries (old entry ↔ fidelity
    attachment, by SHA-256 or charset equivalence; method-5/6 junk entries marked
    superseded-by-gap) means S3's renames and S5/S6's junk replacements are not
    read as regressions.
  - Repair takes only UNCHANGED, COMMIT or KEEP, never MERGE. MERGE would carry
    v1's renamed and junk entries back.
  - If OR3 adopts write-beside, repair commits through recapture's primitive,
    and MA-346 becomes "same stem, or its `-vN` successor". If OR3 grants repair
    an in-place exemption, §4.10's staged rename-over stands, with the N3 window
    stated in §5. **[ruling J6]** OR3 adopts recapture's commit (no in-place
    exemption), and MA-346 reads "the stem re-capture's commit gives it".
  - One old-zip inventory reader is shared by both designs (`util.ArchiveFileGate`
    plus this design's bounds, MC35).
  - Which invariant owns "never to less" is part of OR3 (MC42). **[ruling J6]** It is
    recapture's R28; R27 cites it.

  S7 does not merge before OR3 (ruled, [ruling J6]).
- **Index/status (F53).** If a manifest save fails (F53 logs only), the repair
  stamp is lost. The next run re-examines and, because the output is unchanged,
  only restamps (MC12): idempotent, no loss, no rewrite.
- **Extraction workspace (MC36).** This is the owner's chosen tool: select a
  source, choose the export destination, copy, with no drag-and-drop. Where it
  re-parses an original (the Microsoft 365 source, or the archive source with an
  `.eml`) it calls the same `ParseRFC822`/`convertMessage` and inherits P2–P10.
  Where it copies archived files (the archive source without an `.eml`), the
  copies carry these properties only for records at the current scheme. Seam
  items for that design:
  - (a) its Q9 ("Extract attachments" once C1-1 lands) depends on S2, and its
    README caveat says S2 fixes new captures and repairs only;
  - (b) an attachment with a `GapKind` gets an entry note naming the kind, and
    `.undecoded`/`.partial` names are kept;
  - (c) message-level parse gaps become entry notes, so P10's "visible, recorded"
    reaches the export manifest;
  - (d) nil `WriteTo` is guarded (defensive; the workspace has no PST source);
  - (e) a per-entry note "captured by an earlier version (attachments may be
    re-encoded)" when `parse_scheme` is below the S2 scheme (§4.10
    `not_reexamined`);
  - (f) its concurrency assumption "an incremental capture … does not rewrite
    existing files (R13)" no longer holds during a repair burst. An archive-source
    export running alongside the first post-upgrade capture may report records
    "changed in the archive during export" (§6).
- **Alternatives rejected.** (A) Identity from fidelity terms plus scheme-tagged
  dual lookup (current chain, then a legacy chain) — needs two scheme tags, a
  legacy chain walk in the dedup core, and either re-keying (which breaks the
  go-back history, whose events are keyed by manifest key) or "a record keeps its
  first key forever" with a dual-acceptance heuristic; an older binary would then
  `#fp`-split every repaired message on rollback. (B) Re-key archives on upgrade
  — mass index re-key and history loss. (C) Register charset fallbacks globally
  (F24's text) — H3. (D) Repair everything by `-mode full` only. A full run
  re-downloads and, today, rewrites unchanged mail. Once recapture-integrity's
  "unchanged writes nothing" (its P8) or this design's restamp (MC12) exists, (D)
  differs from P11 only by its cost (a full re-download) and by needing an
  operator to start it (MC41). The chosen freeze (P1) keeps the exporter's dedup
  untouched.

**Exit codes:** unchanged (X1). No refusal is added or changed. Gaps, fallbacks,
withheld repairs and failed repairs never change the exit status, and they are
informational under fault-isolation's partial/exit-3 partition (MC40, OR2). (**[ruling
J5]** they register as informational; fault-isolation's exit 3 is not adopted, its OR2.)

## 5. Crash ordering and failure domains

**Writes.** This design adds one new file kind, the per-run `repairs-<run-id>.tsv`
beside the report (MC17). Repair stages its own temps (MC13).

**[ruling J6]** The W2 repair row below, and the paragraph after the table, describe the
superseded rename-over. Under J6 a repair commits through recapture-integrity's W1–W5,
whose crash table (its §5.1) is the repair's: the persisted manifest always names a
complete set of files matching their recorded fixity, so the W2 "new zip, old html, old
fixity" window does not arise. This table's W2 rows are rewritten before RR-4.

| Write | Steps | Killed after… | Residual | Direction | Operator sees |
|---|---|---|---|---|---|
| W1 new capture | as today: `.eml`, zip temp→rename, html temp→rename, manifest Add (memory), index Add, checkpoint (history → index → manifest) | any step | as today (R5) | as today | as today |
| W2 repair (V1C passed, output changed) | (0) zip and html temps staged and digested; (1) zip temp→rename over the old zip; (2) html temp→rename over the old html; (2b) only when nothing is archivable: the stale zip is removed, **after** (2) (MC13); (3) `.eml` never written (MC11); (4) record replaced in memory (fixity, `parse_scheme`, gaps, `Unresolved`, `ContentHash`); (5) index row replaced (+ live folder-assertion); (6) `repairs-<run-id>.tsv` row; (7) checkpoint | (0) | temps only (swept at run start, as today); old trio, old record | none | nothing; the next run tries again |
| | | (1) | new zip, old html, old record | loud: `verify` reports the zip `modified`; no content lost. V1C (MC3) proved the new zip holds every archived entry plus what v1 dropped | the next run re-examines: on an unchanged source V1C passes again (it matches old entries against v1-now **or fidelity-now**), the repair recommits and records fixity, and the state converges. **Conditional (MC13):** if the source lost content in between, the next run withholds (`source-changed`), and the new-zip/old-html/old-fixity residue persists as a `modified` finding until the source returns or the operator runs `verify` and restores |
| | | (2) or (5) | new zip+html, old record; index old or new | loud, as above (two `modified`) | converges under the same condition |
| | | (2b) | new html (no zip link), stale zip still present | loud: the stale zip is reported as an orphan; nothing is missing | converges on the next run |
| | | (7) | done | — | `repaired=N` |
| W2′ restamp (output unchanged, MC12) | record stamped in memory; checkpoint | any | stamp lost at worst | none | re-examined next run, again no write |
| W3 parse-gap labels | part of the record | — | atomic with the manifest save | — | report/status after the save |
| W4 last-run counts | existing atomic record | — | as today | — | — |

A repair **never deletes** a file, apart from the stale-zip removal of (2b) that
runs after the html commit. It never renames a file to a new path (MC10), never
writes a record ahead of its file (R5), never writes an existing `.eml` (MC11),
and never creates a second record (P1). The W2 window is the same class that every
fill and full-mode re-capture has today (N3). This design makes it more frequent
once per scheme, at the first post-upgrade run. If `verify` runs inside the
window, it reports `modified` and suggests restoring from backup. On an unchanged
source that is harmless, because the next run repairs again. If OR3 adopts
recapture-integrity's staged write-beside commit, the window closes for repair too
(MC41). **[ruling J6]** It does, so the window closes; and "never renames a file to a new
path" becomes "never takes a new stem from the decoded subject" (the commit may give the
stem's `-vN` successor under ESC-3 (a)). The v1 replay and the fidelity parse are pure
functions of the input bytes.

**Failure domains.**

| Failure | Outcome | Ref |
|---|---|---|
| Fidelity-parser panic in one part | That part's gap (per-part recover) | — |
| Fidelity-parser panic for the message, or the "fidelity ⊇ v1" check fails | v1 content, a visible terminal `fidelity-fallback` gap, and a count in `LastRun` and `status`. Never worse than today (P13). | MC4, MC19 |
| v1 replay panics, fidelity succeeds | Stub identity (`stubIdentityTerms`), fidelity content, a visible terminal `identity-stub` gap. Never repaired. | MC6 |
| v1 replay panics, fidelity panics too | The stub, as today, plus `unparsed` (OR2) | — |
| A panic in the new export-side code: an embedded render, the cid tree embedding, the gaps/notes blocks | That attachment's `embedded-item` gap, or a `render-fallback` re-render. Never the whole message. | MC14 |
| A panic during a repair attempt | Withheld `repair-error` + count | MC14 |
| PST embed failure | That attachment's gap, named by go-pst error class | MC31 |
| Old zip or page missing, unreadable, or not as recorded | Repair withheld (`archive-file-missing-or-modified`), reported as an integrity finding | MC1, MC15 |
| Disk full, a read-only file, or a held handle during a repair | `RepairFailed`; the old trio and record are unchanged; the walk continues; after 20 failures, no more repairs that run | MC13 |
| `parsev1.go` or a vendored file edited, or an identity dependency bumped | MA-330/331 red (the sha256 and `go.sum` pins, MC26); handled by the drift policy below | — |

There is no new standing service and no new single point of loss.

**Blast radius (MC18).** A panic costs one part, or at worst one message's
content reverting to v1. A non-panic logic defect is wider. In S3's track
assembly or in `text.go`, it reaches **every record whose message triggers it**
on the first run of an automatic repair (Q1). MC4 stops the defects that drop or
change an attachment, or drop a body segment: those fall back to v1 instead of
spreading. What MC4 cannot see is a text-decoding defect of the mojibake class,
or a reordering of segments. The remedies for those:

- A fixed build lists the defective scheme in `state.RevokedSchemes`, and the
  predicate repairs those records again on their next re-read.
- The content rollback is an older binary plus `-mode full`. On Graph that is a
  full re-download under throttling. On on-demand sources it carries F46.

**Residual:** a record damaged that way whose source is later lost can be
repaired only from a preserved `.eml` (Q3). Q1 is the canary lever: an opt-in
first release limits a defect to the operators who asked for repair. **[ruling Q1]**
Repair is automatic, so `state.RevokedSchemes` is the rollback lever, and V1C (never to
less) and MC4 bound the blast radius. **[ruling Q3]** The `.eml` repair is its own design.

**Drift policy — the 3am drill (MC28)**
- **A reachable CVE in the frozen surface** (`parsev1.go`, its vendored copies, or
  a shared identity dependency, P1(b)). Fix it with a minimal patch to the frozen
  copy. MA-330 must stay green, and a corpus-seeded differential fuzz of old v1
  against patched v1 must show output changing only on inputs the patch now
  rejects or bounds. The patch is an operator-accepted change to a pinned file.
  It is recorded in its commit, with govulncheck's reachability through
  `parsev1` in the release notes.
- **MA-330 goes red** (the fix moves identity on a corpus shape). Stop and get an
  **OWNER RULING**: either accept the named drift (a README note naming the
  affected shape and its consequence: a `#fp`-qualified duplicate on local
  imports, a re-keyed no-Message-ID message), or keep the old behaviour and
  ship the CVE fix elsewhere.
- **A deadlock** between govulncheck (a reachable vulnerability blocks the
  release) and MA-330 (identity moved). The owner decides; neither gate is
  weakened.
- **A toolchain end-of-life move** follows the same procedure (OR4).
- **What users see:** nothing, when the patch keeps identity. When drift is
  accepted, a README and release-note line naming the shape.

## 6. Migration and coexistence

- **Existing archives.** No load-time migration and no manifest version change
  (stays 6, unless OR3 adopts recapture-integrity's v7; see Rollback; **[ruling J2]** it
  stays 6). `parse_scheme` appears on records as they are re-read or written (MC12). Parse
  gaps live in the gap channel OR2 rules on (`ReadGaps` + `GapClass`, [ruling J5]). The
  first run after upgrade exports zero new items for an unchanged source (P1) and repairs
  the records whose messages carry notes and pass V1C (P11). A seen record that needs
  nothing is stamped in memory without a write.
- **R2 clarification (catalog text, S0; MC12):** "…exports zero new items. A
  repair of a record written by an older parser scheme is a re-capture in place,
  not a new item, and rewrites a record at most once per record per scheme and
  source content." **[ruling J7]** Not in S0: this clarification changes R2's text, so it
  lands with recapture-integrity's R2 amendment as one joint paragraph once the owner
  answers ESC-2. "In place" there follows ESC-3 [ruling J6].
- **Schedules.** No job flag changes; MA-72's dry-parse is unaffected. The first
  scheduled run after each scheme upgrade carries the repair burst. It is longer
  once per scheme, every future scheme repeats it, and it is logged (MC17). S2–S6
  ship before S7, or together with it, so this design causes one burst (MC43).
- **Graph.** Incremental runs repair no-Message-ID messages only. With-Message-ID
  messages keep their archived form until a `-mode full` run or Q3's `.eml`
  repair. A full run re-downloads every message; its noted records take the
  repair branch, and its other records are re-captured as today (MC9).
  `status` shows `not_reexamined` with that remedy (MC16). MailArchive Desktop
  cannot run full (OR1; escalated as ESC-6 [ruling ESC-6]). Stated in the README.
- **Mixed versions (MC12).** Identity and stems are equal across builds (P1,
  MC8), so alternating binaries never duplicates or renames. (**[ruling J6]** a
  repair's `-vN` name under ESC-3 (a) is recorded in the manifest, so an older binary
  reads it as it reads any record path.) Any save by an older
  binary (incremental runs, `verify -record`, `reindex`) drops `parse_scheme`,
  `FidelityFallback` and `RepairWithheld` from every record. This happens
  routinely when a scheduled job still points at an older executable
  (`health.go:535-538` detects it). Each alternation then costs this build one
  in-memory re-examination of the noted subset and **no rewrite**, because
  unchanged output is restamped. An older binary's `-mode full` re-renders with
  the v1 parser (un-repairs). This build repairs again on its next run only
  while the source still holds the message (P12).
- **Rollback.** Replace the binary. The older binary reads the manifest
  (version 6), skips every record by identity, keeps repaired files, and shows
  parse gaps as `empty-attachment` rows. **Two limits:**
  - It cannot fill a Missing label this build wrote under a fidelity name
    (`Quarterly Report.pdf` where v1 says `inline`). Its `probeImproves` matches
    v1 names only (`exporter.go:583-586`), so such a record stays "still missing
    content" until this build returns (MC34).
  - If OR3 adopts recapture-integrity's manifest v7, rollback and alternation
    hold only among v7-capable binaries; a pre-v7 binary refuses the archive
    ("Load refuses a stored version above 6"). §6 is then re-walked under OR3
    (MC41). **[ruling J2]** Moot: no design bumps the manifest, so rollback stays a
    binary swap.
- **Index.** No version change; repaired rows are replaced by key.
  `reindex -rebuild` from `.eml` yields fidelity content under the same keys. For
  records **not yet written at the current scheme** (with-Message-ID Graph mail
  before a full run, withheld records), the rebuilt index holds fidelity text
  while the page still shows v1 content, so a search hit can open a page without
  the matched text. `reindex -rebuild` reports how many such records it indexed
  and points at `not_reexamined` (MC16).
- **Extraction workspace concurrency (MC36).** A repair burst rewrites existing
  files during an ordinary capture, so an archive-source export running at the
  same time may report records "changed in the archive during export". The
  workspace design's concurrency note is updated (§4.11 seam item f).
- **README.** The Windows-1252 claim becomes true for raw MIME. New lines cover
  repair and the repair log; `.undecoded` and `.partial` files;
  `(attached message).html`; and the Graph full-run and Desktop conditions.
  README.txt (archive root) explains `.undecoded`, `.partial` and attached-item
  pages for an inheritor, and says that these suffixes are reserved for
  tool-made entries (MC33).

## 7. Operator walk

- **Home user, local source** (Thunderbird or Apple Mail export, GUI or CLI):
  1. Install the update.
  2. Wait for the next "Archive now" or scheduled run.

  **0 extra steps, 0 secrets.** They see "exported 0 new · repaired N" in the
  summary and the Activity log (MC17). The first run after each parser scheme is
  longer: Type I, inherent to rewriting, mitigated by the progress counts, and
  Type II fixed here by showing `repaired` in the progress stats. If the mail app
  has since dropped or detached content, they see the `source-changed` line:
  - what: kept N unrepaired;
  - why: the source no longer holds what the archive holds;
  - remedy: make mail available offline, then re-run.

  The line prints once, not on every run. If an archived file is missing or not
  as recorded, they see an integrity finding pointing at `verify`, not a mail-app
  remedy (MC15).
- **Home user, MailArchive Desktop** (Microsoft 365, MC37):
  1. Install the update.
  2. Wait for the next "Archive now" or scheduled run.

  **0 extra steps.** No-Message-ID mail is repaired automatically. With-Message-ID
  mail, which is nearly all of it, is **not repaired**: Desktop captures
  incrementally only (`capture.go:43, :141`) and has no full-run control. The
  Desktop summary and health view say so: "N messages were archived by an
  earlier version; Desktop cannot re-check them without a full re-download"
  (from `not_reexamined`, MC16). Whether Desktop gains a repair action is
  **OR1**, escalated to the owner as ESC-6 [ruling ESC-6]. Until then, the workspace's
  Microsoft 365 source (a fresh parse) gives fidelity output for a copy.
- **Admin** (scheduled CLI, PST fleet, or Graph app-only/device):
  1. Deploy the binary. Nothing else is needed for local sources (**0 steps**).
  2. **1 optional step** for Graph: a deliberate `-mode full` run to repair
     with-Message-ID mail. Its true cost (MC37): a full re-download under
     throttling, and a re-export and rewrite of **every** archived record (only
     noted records take the repair branch), so the whole archive churns through
     backup and sync.

  `status` shows `not_reexamined` beforehand, so the admin can judge the benefit
  first. A repair-only write policy for that full walk is part of OR1 (ESC-6: it would
  amend R2's "`full` re-exports all", which only the owner can accept [ruling ESC-6]).
  `status` keeps its posture. The new "cannot decode", withheld and fallback counts name
  the report. If Q1 rules opt-in, the CLI flag, the GUI and Desktop control, the
  settings key and the job-descriptor field that carry it are named in S7 under
  X5's shared builders (MC37). **[ruling Q1]** Q1 rules automatic, so there is no flag.
- **Custodian holding external copies** (offsite backup, checksum baseline,
  workspace hand-overs with `SHA256SUMS`). Each run that repaired anything writes
  `repairs-<run-id>.tsv` with old and new digests, named in the summary (MC17).
  The custodian refreshes external baselines for exactly those files.
- **Inheritor opening the archive cold** (no tool): opens README.txt, then a page.
  **0 steps** to read:
  - The body is complete and in order, and attachments carry real names that
    match the zip entries (MC33).
  - An attached Outlook item is an `.html` in the zip that opens in any browser
    (same offline-safe policy). The tool-made suffixes are reserved, so a
    sender's file cannot pose as one (MC33).
  - Any part that could not be decoded, or was cut short, is listed visibly on
    the page with its `.undecoded`/`.partial` file name and the reason. So they
    know what is missing and where its bytes are.
- **Extraction-tool user** (the owner's chosen tool: select source, choose
  destination, copy; MC36). The ceremony is unchanged. Two outcomes:
  - From the Microsoft 365 source, or from archive records with an `.eml`, the
    output carries the fidelity properties.
  - From archive records without an `.eml`, the copy carries what the archive
    holds: fidelity only for records at the current scheme, which is never the
    case for with-Message-ID Desktop mail (OR1; ESC-6 [ruling ESC-6]). Such entries carry
    the "captured by an earlier version" note.
- **Refusals:** none added. The new operator messages are §4.10's repair,
  restamp, withheld (per reason) and failed lines; §4.9's "cannot decode" counts;
  and the `not_reexamined` line. All are worded what / why / remedy. S2 and S4
  owe a friction review and an X-series check for them, and S7 owes one for the
  upgrade walk (MC39).

## 8. Security

- **Trust boundaries.**
  - Mail bytes are untrusted: structure, headers, parameters, charset labels,
    names, CIDs, media types, HTML.
  - PST files are untrusted: attach methods, property types, NIDs, block ids,
    `ulSize`, recursion.
  - The manifest is semi-trusted: a tampered `Path`, `parse_scheme`,
    `ContentHash` or `RepairWithheld`.
  - Archive files on disk are semi-trusted: the old page and zip that V1C reads.
- **Outside aggressor (a sender).**
  - *Crafted MIME aimed at CPU or memory:* bounded by P13, including the
    explicit per-header caps and the RFC 2231 index bound (MC22).
  - *Forged placeholders* (`[Attachment: x]` in their own body): cosmetic; the
    tool-owned Attachments line is authoritative.
  - *Forged gap or notes markup:* mail cannot wear `mailarchive-*` (MA-187).
  - *A forged media type that breaks out of `<style>`* (`image/png</style><div
    class="mailarchive-header">…`): refused by the image-type allowlist, with
    MA-301's re-parse failing closed on reserved markup outside tool-owned
    blocks (MC23).
  - *A `utf-7` body to smuggle markup:* shown as stored, escaped, under the
    archive CSP.
  - *Filenames with separators or reserved names:* `SanitizeFilename` (R4); zip
    entries never carry paths, so there is no zip-slip for an extracting
    inheritor.
  - *A sender file named `… (attached message).html` or `….undecoded`:* renamed,
    because those suffixes are reserved for tool-made entries (MC33).
  - *A crafted `%00` RFC 2231 name:* mis-kinds only their own report row.
  - *A same-envelope forgery:* someone who knows A's Message-ID, envelope and
    attachment names sends B into the same folder with a note-triggering part.
    Fingerprint deliberately ignores bodies and bytes, so revision 1 would have
    written B over archived A at the first post-upgrade run. Revision 2 repairs
    only after the same-message proof (V1C 5, MC5) and the per-entry SHA-256
    match (V1C 3), and an attacker cannot meet SHA-256 with a CRC-32 collision.
    The same check withholds the benign twin: a Mailman list copy whose footer
    fires `multi-segment-body` next to the archived direct copy.
- **Crafted PST.**
  - Embed cycles, depth, block-id aliasing: bounded by the block-id visited set,
    depth, and the entry, byte and step budget (MC31).
  - Per-embed recover, including a recover around each embedded render (MC14).
  - The property type decides an embed, not the writer's method value (MC31).
  - `ulSize` never sizes an allocation.
  - An embed pointing at another message in the same file discloses nothing
    outside the archive.
- **Insider with write access to the manifest but not the target.**
  - A tampered `Path` can neither redirect nor delete through a repair. The stem
    gate requires `KeyForPath(prev.Path) == key`, a failure withholds with no
    fallback (MC10), and S7 guards the pre-existing `os.Remove` calls of
    fill/full re-captures (`exporter.go:485-489`) with the `validRelPath`
    containment gate.
  - A tampered `parse_scheme` can only cause or suppress a V1C-gated repair of
    that record.
  - A tampered `ContentHash` can only withhold.
  - A swapped old zip or page fails its fixity and withholds as an integrity
    finding (MC1). Such an actor can already rewrite files.
- **Integrity and concurrency:** mixed versions and crashes are covered in §5,
  §6, P12 and MC12.
- **Identity surface.** P1 adds no new collision surface for keys: identity
  inputs are exactly today's. There is one new consequence of a collision, and
  revision 2 closes it. Two messages that share a key and fingerprint used to mean
  the second was skipped. Under P11 it could have been written over the first. V1C
  steps 3, 5 and 6 refuse that overwrite (MC5, MC6).
- **New surfaces.**
  - The lenient tokenizer and address extraction: bounded and linear.
  - The nested attached-item page: same renderer, CSP meta first (R19).
  - The old-file read of V1C: bounded decompression, `util.ArchiveFileGate`. It
    uses the same directory the write uses, so it adds nothing to the
    symlinked-folder exposure that the confidentiality design owns (C2-2).
  - There is no new listener, credential or network call.
- **Dependencies (MC27, MC31).** The vendored replay code (`internal/source/parsev1/`)
  is a frozen parser of untrusted input. It is patched only under §5's drift
  policy (MC28), and THIRD-PARTY-NOTICES lists it. go-pst is reached further by S6.
  §4.8 states who owns it and the plan for forking and patching it.
- **Adversarial pass owed** on S1, S2, S4, S6 and S7 (§10).

## 9. Test plan (proposed rows MA-330..MA-349)

A new **fidelity corpus** lives in `testdata/mime/`. The tests lay it out as mbox,
maildir and Evolution trees. It holds:
- one `.eml` per trigger in §1;
- Apple Mail, mutt, Mailman, Outlook and bounce shapes;
- one item per go-pst nameMap label (MC26);
- one item that panics v1 (MC7);
- `support.pst`.

**Goldens (MC7, MC29).** S0 records two goldens from the unmodified build, at the
release tag and commit named in `parsev1.go`'s header:
- **identity**: key, `Identity`, `Fingerprint`, `ContentDigest`, and local and
  live keying;
- **content**: all `model.Message` fields, attachment names and byte digests.

These are baselines in the CLAUDE.md sense: regenerating one is an
operator-accepted act under §5's drift policy, never a fix. There is no stored
page-byte golden. MA-332 computes the expected render inside the test, by applying
the current renderer to the v1 message, so renderer work in other slices never
touches a parser baseline. S0 runs after `BodyFallback` lands, or excludes the
messages it affects.

No row is a refusal. Positive twins are named per row. Covers cells use the
working ids of the header (MC42).

| ID | Tier | Asserts | Covers | Prove-fail |
|---|---|---|---|---|
| MA-330 | U | For every corpus message, `Identity()`, `Fingerprint()`, `ContentDigest()` and the local and live keys equal the identity golden, for messages the fidelity parser changes as well as unchanged ones. This includes the v1-panicking item, whose stub identity comes from `stubIdentityTerms`, and one item per go-pst nameMap label. PST twin on `support.pst` | R3, R2, R27, S49 | make `idv()` return content `Subject` → red |
| MA-331 | U | (1) After parsing the corpus, the effective charset label table matches the snapshot: resolution of every label in IANA ∪ WHATWG ∪ go-message's quirk table ∪ go-pst's nameMap, for both the vendored and the shared table. `message.CharsetReader` is the same function. (2) The sha256 of `parsev1.go` and of every vendored file, and the `go.sum` lines of every identity dependency (P1(b)), equal the pinned values. (3) `go list -deps` of `parsev1` lists none of `net/mail`, `mime` or `github.com/emersion/go-message` (MC27). (4) `parsev1`'s v1 message for every corpus item equals the content golden (MC26) | R27, R3 | add `charset.RegisterEncoding("unknown-8bit", …)` in `text.go` → red; edit one byte of `parsev1.go` → red |
| MA-332 | U | Parser-scoped (MC29). Every corpus message with no fidelity note has content fields equal to the content golden, renders identically to the current renderer applied to `replayV1`'s message (computed in the test), and carries `IdentityTerms` equal to its content fields. It is also the guard that catches a missed note registration over the corpus (MC24) | R2, R7, R27 | concatenate with a separator even for one segment → red |
| MA-333 | U | F24 charsets. Each label is tested both first and after the body. Every later part (incl. `r.pdf`) is archived, text is decoded (valid UTF-8 / Windows-1252) and no gap is recorded. A top-level `unknown-8bit` QP message gets a decoded Subject and body (no `fallbackParse`). Its "`charset-fallback` on the page" assertion turns green at S4 (MC30). Twin: `iso-8859-1` control | R1, R10, R26 | restore `break` on error → red |
| MA-334 | U | F24 CTEs. `8-bit` and `quoted-printable (encoded)` decode with `lenient-cte`. `x-uuencode`/`x-foo` keep still-encoded bytes as `<name>.undecoded` and record a **terminal** `undecoded-part` gap, even on a local source; the report row has kind `undecoded-part`, class `terminal`; a re-run neither retries nor rewrites; later parts are archived; no stub. **On-demand twin (MC20):** a base64 part truncated on the first run records a **fillable** `corrupt-part`, and when the complete message lands, the next run fills it. The same truncation on a Graph fake stays terminal. Twin: 7bit/base64/qp | R1, R10, R26 | route `undecoded-part` to `Missing` → red (re-run retries); route `corrupt-part` to `Terminal` on a local source → red (never filled) |
| MA-335 | U | F25 bodies. The Apple Mail fixture's page has html1, the placeholder and html2 in order, and renders as one document. The zip holds the PDF and `notes.txt` byte-exact. The index holds the plain alternative. mutt `build.log` and `name=notes.txt` are archived. The Mailman footer is shown after the HTML. `alternative[plain, html]` shows the HTML only | R1, R7, R26 | keep first html only → red |
| MA-336 | U | F25 names. Filename → CID → `inline`+ext; `message/rfc822` → `<subject>.eml`; inline `text/calendar` → `inline.ics`. Names are identical with `mime.ExtensionsByType` stubbed to return nothing (no host dependency). **Names match the zip (MC33):** the page's Attachments line, the placeholders and the gaps block print the zip entry names (duplicates `a (2).pdf`, a 130-rune name). A nested `<subject> - <name>` with a 200-rune subject keeps the attachment's own name and extension. A sender file named `Invoice (attached message).html` or `x.undecoded` is renamed | R1, R4, R26 | use `mime.ExtensionsByType` → red under the stub; print the raw filename on the page → red (duplicate case) |
| MA-337 | U | C1-1. Latin-1 `text/csv`, ISO-8859-1 XML, UTF-16 and Shift_JIS `.txt`, cp1252 `.ics` and a `text/html` attachment: each zip entry's sha256 equals the transfer-decoded source bytes, zip fixity is recorded over them, and `verify` exits 0. An 8bit, LF-only text attachment in an mbox has the bytes as go-mbox frames them (stated in the row's fixture comment, MC38). A truncated base64 attachment is archived as `<name>.partial` with a `corrupt-part` gap, never under its real name. The displayed Latin-1 body part is valid UTF-8 on the page. Twin: utf-8 attachment | R1, R5, R26 | decode attachments through the charset path → red |
| MA-338 | U | C1-4. Unquoted `=` boundary, unquoted `start=<x@y>`/`type=text/html`, `charset=us-ascii (Plain text)`, unquoted `filename=Invoice 2023.pdf` → body, attachments and names are recovered and `lenient-params` is noted. A multipart with no recoverable boundary → body as text + `unsplit-multipart` (fillable on an on-demand source, terminal on Graph; MC20), never a `body` gap. Twin: strict-valid headers unchanged | R1, R10, R26 | disable the lenient tokenizer → red |
| MA-339 | U | C1-5. A plain body with `[cid:image001.png@…]` keeps `chart.png` in the zip and on the Attachments line, and the page has no base64. `<a href="cid:x">` is not consumed. `<img src="cid:x">` and CSS `url(cid:x)` are embedded and kept out of the zip (MA-19 twin). Unresolved refs are counted only in image contexts. `cid:abc` vs `cid:abcdef` is matched by whole token, and `renderNotes` flags the prefix case (MC24). **Data URIs (MC23):** an attachment whose `MimeType` is `image/png</style><div class="mailarchive-header">…`, referenced from the mail's own `<style>`, is not embedded; the page's first `.mailarchive-header` is the tool's; and MA-301's re-parse fails closed on reserved markup injected outside tool-owned blocks | R7, R1, R19 | re-enable regex embedding on plain bodies → red; drop the allowlist → red (forged header) |
| MA-340 | U | C1-8. A raw Latin-1 Subject/From name/To, and unlabelled and `us-ascii`-labelled 8-bit bodies → valid UTF-8 `Grüße aus Köln` on the page, the FTS query `Grüße` hits, and there is no U+FFFD. Identity equals the golden. Fuzz-seeded property: every text field of `safeParseMessage(any bytes)` is valid UTF-8, **including fault-injected fidelity fallbacks** (MC19) | R7, R8, R26, R3 | drop the Windows-1252 step → red; skip `decodeText` on the fallback path → red |
| MA-341 | U | C1-10. `<>`, `Mueller, Hans <hans@example.de>`, `Mail Delivery System <MAILER-DAEMON>`, `hans@example.de <hans@example.de>`, `<john..smith@…>`, `<.john@…>`, `Undisclosed sender:;` and an 8-bit name → the page's `data-mailarchive-field="from"` has the expected name/email, the folder page does not show `(unknown)`, and sender search hits. Control chars are stripped, with a 1,000-rune cap. The fingerprint equals the golden. mbox `extract`'s postmark address agrees with the page's sender (one primitive, MC35) | R7, R8, R3 | remove the From fallback → red |
| MA-342 | U | F26 interim on `support.pst` with an attachment's 0x3701 retyped in memory to `PtypObject` (method 5 and 6). There is no junk entry in the zip. `embedded-item`/`ole-object` terminal gaps name `PidTagDisplayName`, although PST is `SourceComplete=false`. A re-run neither retries nor rewrites. **The outer message is exported**, with its ordinary attachments (MC30). Method 5 with a `PtypBinary` 0x3701 keeps its bytes as today (MC31). Identity equals the golden | R1, R10, R26 | fall through to go-pst `WriteTo` → red; remove the nil-`WriteTo` guard → red (outer message missing) |
| MA-343 | L, pending, until Q2's fixture lands; then U, in S6 [ruling Q2, J1] (S6 may not merge while L) | F26 full on a committed, Outlook-written method-5 fixture (Q2, MC32; ruled: a synthetic fixture, [ruling Q2]): forward-as-attachment, NDR, nested depth 2, a nested item whose subnode NIDs equal the outer item's, a block-id cycle, an embedded appointment. The outer zip has `<subject> (attached message).html` (CSP meta first, transport panel, inline images embedded) and byte-exact `<subject> - <name>` entries. Depth > 4, the cycle and an exhausted budget → `structure-limit`. The coinciding NIDs are **not** a cycle. The appointment → `embedded-item` naming the class. A go-pst `ErrLocalDescriptorBranchNode` → the named `embedded-item` detail, counted (MC31). Until Q2 yields the fixture, the row stays **L pending** ("export from Outlook on Windows a PST holding a forward-as-attachment, an NDR and a report-phish item; run; inspect the zip"), and S5's gap is the shipped floor | R1, R19, R4, R26 | skip the visited set → cycle test hangs past its deadline → red; key the set on NIDs → coinciding-NID item reports a false cycle → red |
| MA-344 | U | Repair. Start from an archive written through the frozen v1 path (v1 messages exported with `parse_scheme` 0) and upgrade it. The first incremental run rewrites exactly the records whose notes fire and whose output changes (`Repaired`) and restamps the rest without a write (`Restamped`, MC12). Keys and fingerprints are unchanged, paths are those re-capture's commit gives (the same stem, or its `-vN` successor under ESC-3 (a), [ruling J6]), fixity is fresh (`verify` exits 0) and index text is updated. Untouched records' files keep their bytes and mtimes. `repairs-<run-id>.tsv` lists old and new digests, and every surface shows `exported 0 new` (MC17). The second run repairs 0, restamps 0 and exports 0. A scheme listed in `RevokedSchemes` makes its records repair again (MC18). Graph fake: an incremental run downloads no with-Message-ID message and repairs a no-Message-ID one; `-mode full` routes noted with-Message-ID records through the repair branch (V1C runs, same stem, `.eml` carried forward) with no `#fp` split (MC9) | R2, R3, R5, R13, R17, R27 | stamp `parse_scheme` 0 on write → second run repairs again → red |
| MA-345 | U | Repair never to less, and withheld legibly. Each case is withheld with old page, zip, record and fixity unchanged (assure NoSideEffect), the named reason and its own counter: (a) an on-demand source whose message lost an attachment's bytes since capture → `source-changed`; (b) the L1-1 masking case (a cid-placeholder image no longer consumed hides report.pdf now streaming 0 bytes); (c) a Thunderbird-detached part (`X-Mozilla-Altered: AttachmentDetached`, same name, placeholder bytes); (d) the F25 named-inline and S6 nested-entry offsets; (e) a same-envelope message with a different body or attachments, and the Mailman list-copy twin → `cannot-confirm-same-message`; (f) a v1-panicking record → never repaired, `identity-stub` gap; (g) **the H-1 twin (MC1):** a record with no zip whose page the owner deleted → nothing written, `archive-file-missing-or-modified`. The same for a page or zip that is missing-but-recorded, unreadable, altered against fixity, symlinked or over the bound, never followed or opened; (h) **the H-2 twin (MC2):** a withheld no-Message-ID record on the live path (fake Graph) keeps `Present` true, gets `LastSeen` = `runAt`, and is not marked gone by `SweepGone`; (i) an injected rename refusal on the zip and on the html → old trio unchanged, `RepairFailed` names the path, and a later new message in the walk is still captured (MC13); (j) `RepairWithheld` is persisted with its `SourceDigest`, and the next run skips the re-check and does not re-warn (MC15). Positive twin: the source unchanged → repaired | R1, R13, R4, R21, R27 | replace V1C with revision 1's count → red (cases b–d replace the old zip); drop the archive-side check → red (case g re-creates the page) |
| MA-346 | U | Repair and re-capture keep paths and originals. (a) A `-raw` record repaired by a run without `-raw` keeps its `.eml` and `Fixity.EML`, and a `-raw` repair never rewrites an existing `.eml` (MC11). (b) The html/zip are rewritten at the stem re-capture's commit gives it (the identity-view stem, or its `-vN` successor under ESC-3 (a)), never a stem from the decoded subject, although the decoded subject's slug differs [ruling J6]. (c) A **fill** and a **`-mode full` run** on a legacy-8bit-subject record in a `-raw` archive, run without `-raw`, keep the same path, `.eml` and `Fixity.EML` (MC8). (d) A tampered `Path` (`../x.html`, absolute, another store's directory, a sibling record's stem) is never written through, nothing outside `-out` is removed, and the repair is withheld as `unsafe-path` (MC10). Cases (a)/(c) cover S1's write path; (b)/(d) turn green at S7 | R4, R20, R13, R5 | use `stemFor(m.Subject)` → red (stem changes) |
| MA-347 | U | Mixed versions. Identity half (S1): a run by v1 semantics (manifest round-trip dropping `parse_scheme`; exporter fed `replayV1` messages) → 0 new exports, 0 `#fp`-qualified keys, the same stems. Repair half (S7): **3 alternations** → ≤ 1 rewrite per record in total, 0 writes after the first, 0 new exports, and no new history folder-assertions after the first (MC12) | R2, R3, R27 | compute identity from content terms → red (`#fp` splits); drop the restamp → red (rewrites per alternation) |
| MA-348 | U | Legibility. The summary shows `repaired=N`, `restamped`, "exported 0 new · repaired N" (`Done.` line, GUI, Desktop card), and the "cannot decode" counts. The `status` Incomplete line splits source-empty from cannot-decode. The `not_reexamined` line has its per-source remedy, and `status -json` carries `undecodable`, `not_reexamined`, `repair_withheld` by reason and `fidelity_fallback` (MC15–MC17). The GUI summary and Desktop health view show the split. Report kinds and classes are as specified. The page's visible gaps block (incl. `fidelity-fallback`, `identity-stub`) and collapsed notes block are escaped, and the notes block lists only display-qualifying notes (MC25). `reindex -rebuild` from HTML ignores both blocks. The page-block assertions turn green at S4 (MC30) | R1, R18, R7, X6 | count parse gaps as source-empty → red; count repairs as `Exported` → red |
| MA-349 | U | Bounds and isolation. Each of these completes within a generous deadline, records `structure-limit`/`corrupt-part` (one part only), or reverts that message to v1 content with a visible `fidelity-fallback` gap, and never yields a stub; the next mbox message parses normally: multipart depth 33; 10,001 leaves; 64+ lenient params; `filename*4294967295*=` (bounded, no allocation by index); an oversized part header (MC22); an encoded-word bomb; a fault-injected panic in one part's decoder; a fault-injected panic in the fidelity parser; an injected "fidelity ⊇ v1" failure (MC4); more than 32 parse gaps → 32 labels + one summary, with a manifest-size assertion (MC21). A fault-injected panic in an embedded render, in the cid tree embedding and in a repair attempt → an `embedded-item` gap / `render-fallback` / withheld `repair-error`, and the outer message is exported (MC14). The `</style>` breakout (MC23). A fallback message with a cid text reference → the second run repairs 0 (MC19) | R10, R1, R19, R26 | remove the depth bound → red (deadline); stamp a fallback write 0 and drop the `FellBack` exclusion → red (rewrite every run) |

Proposed catalog additions (S0, ids as allocated by the operator, OR5, MC42; **[ruling
OR5 = J1]** allocated: R26, R27, S49):

- **R26 — Parse fidelity.** "Every MIME leaf part and every PST attachment the
  reader meets is either archived or recorded as a parse gap, and no part's failure
  ends the parse of another.
  - *Archived:* an attachment as its transfer-decoded bytes as the source store
    holds them, never charset-converted, under its own name (a truncated part
    under `<name>.partial`); a body text part decoded to valid UTF-8 for the page
    and the index, with every inline text part kept in document order.
  - *Recorded:* a parse gap naming the part, in the manifest, the report and on
    the page, with its bytes kept as received where they exist. The gap is
    terminal where re-reading cannot change the answer, and fillable on an
    on-demand source where a later complete copy can."
- **R27 — Frozen identity, guarded repair.** **[ruling J6]** This is the text that lands in
  S0. It replaces revision 2's draft, which promised that no parser change moves an
  archived message's files and that a repair is "re-captured in place"; under J6 a repair
  commits through R28's write path, whose answer to ESC-3 decides its file names.
  "A raw-MIME message's dedup identity, fingerprint, content digest and file stem are
  computed from the frozen v1 parser's view of it, and a PST item's from its v1
  attachment names. No parser change therefore moves an archived message's key or
  fingerprint. A record written by an older or revoked parser scheme whose message now
  triggers a later scheme's fix is re-captured through R28's commit, rewriting it at
  most once per scheme and source content; its files are those that commit gives it
  (its stem, or that stem's `-vN` successor). That happens only when:
  - the archived files are present and as recorded;
  - it is provably the same message;
  - the re-capture is never to less (R28): the source still delivers every archived
    item, matched by content, and the new capture holds at least what v1 does.

  Otherwise the record is left as archived, its timeline is still stamped, and the
  reason is shown." OR3 may assign the "holds at least" clause to
  recapture-integrity's invariant instead; this text then cites it. **[ruling J6]** It
  does: the clause cites R28, and V1C's own preconditions stay.
- **S49** — "A legacy, Apple Mail, mailing-list or Outlook message is archived,
  and an archive written by an earlier version is upgraded. The message may have
  split bodies, inline named files, legacy or unknown charsets, unknown
  encodings, malformed parameters, cid text placeholders, a bounce sender, or an
  attached Outlook item." → R26, R27, R1, R2, R3, R7, R8, R21 →
  MA-330..349.

The R2 clarification is as in §6. **[ruling J7]** It does not land in S0: it changes R2's
text, so it lands with recapture-integrity's R2 amendment as one joint paragraph once the
owner answers ESC-2. S0 checks that the catalog text of every id
cited in a Covers cell matches the wording above. This catches the case where
another design's catalog edit took the id first (MC42).

## 10. Build slices and order

The order is dependency-sound:
- the goldens precede any change;
- identity routing and stems precede any content change;
- the model fields and their sinks precede their producers;
- repair, the sink of every note, comes last, behind its own gate.

| Slice | Waits on | Content | Rows turned green | Gate owed after build |
|---|---|---|---|---|
| **S0** | `BodyFallback` landed (MC29; it has); OR5 ruled [ruling J1] | Catalog first: R26, R27 (the [ruling J6] text of §9), S49 (allocated ids), MA-330..349 (U rows as `t.Skip` encoders citing R26/R27; MA-343 as L, pending). The R2 clarification is **not** in S0: it waits for ESC-2 [ruling J7]. The fidelity corpus, incl. nameMap and v1-panic items. The identity and content goldens at the named release tag (provenance recorded) | — | none (tests/catalog only) |
| **S1** | OR2, OR4 (ruled [ruling J5, OR4]); fault-isolation S1a merged first (the landing order of J5) | Model (`IdentityTerms`, `ParseNotes`, gap fields per OR2, `V1Inventory`, `FellBack`, `Attachment.GapKind/Embedded/ZipName`, `idv()`). `parsev1.go` at the named tag, plus the vendored dependencies (MC27), and `stubIdentityTerms` (MC7). `safeParseMessage` routing. `ContentHash` on every write (MC5). Identity-view stems and `.eml` carry-forward on every write (MC8). `Record.ParseScheme` stamping (=1), incl. stamp on re-read (MC12). Content still from v1. `stubIdentityTerms` reproduces today's constant stub [ruling J5]. Vendor `encoding/base64` into `parsev1` and have MA-331 assert `unicode.Version` (recommended strengthening, [ruling OR4]) | MA-330, MA-331, MA-347 (identity half), MA-346 (a, c) | **adversarial pass** (integrity persona: identity drift, mixed versions, stems) |
| **S2** | S1 | Fidelity walker, `mimeparams.go`, `text.go` for bodies, transfer decoding. P2, P4 (`.partial`), P5, P10 (gap classes, MC20), P13 (bounds, MC21/MC22). The "fidelity ⊇ v1" check and the visible fallback (MC4, MC19). Shared `internal/util` primitives (MC35). Report kinds, counts and wording (CLI, GUI, status). `BenchmarkParseCorpus` (MC39) | MA-332, 333 (non-page), 334, 337, 338, 340 (bodies), 349 | **adversarial pass** (hostile MIME) + **friction review and X-series check** (CLI/GUI/status wording, MC39) |
| **S3** | S2 | Body assembly, classification, naming (final zip names once, reserved suffixes, MC33), header and address decoding (P3, P7 headers, P8) | MA-335, 336, 340, 341 | **friction review** (inheritor reads pages; names) |
| **S4** | S3 | Renderer: tree-based cid embedding with the data-URI allowlist and the MA-301 fail-closed extension (MC23); `renderNotes` prefix case (MC24); gaps and notes blocks (MC25); render recovers (MC14) (P6) | MA-339, 333 (page), 348 | **adversarial pass** (R19 renderer surface) + **friction review and X-series check** (page blocks, MC39) |
| **S5** | S1 | PST interim: 0x3701 property-type decision (MC31), method 5/6 gaps, the nil-`WriteTo` guard in `WriteZip`/`drain`/`hasArchivable`, GapKind routing and the report kind (MC30), PST `IdentityTerms` | MA-342 | **adversarial pass** (crafted PST, interim scope); S6's pass covers it only if they ship together |
| **S6** | Q2: MA-343 tier U on an Outlook-written fixture (MC32); Q2 ruled yes for a synthetic fixture, which must be committed first [ruling Q2] | PST full embedded extraction (P9), block-id visited set, budgets, go-pst error mapping (MC31) | MA-343 | **adversarial pass** (crafted PST) |
| **S7** | Q1, OR3 (ruled [ruling Q1, J6]), a delta re-review of §4.10 rev 2 (lenses 3, 6, 7, 8) (MC43; RR-4, on the text that writes J6 in); S2–S6 in the same release or earlier, **S6 included, unconditionally**; recapture-integrity S4 and S5 (waiting on ESC-2 and ESC-3) | Repair: the predicate (incl. full mode MC9, revocation MC18, fallback MC19), V1C (MC1, MC3–MC6), restamp (MC12), write discipline (MC13), timeline stamping (MC2), withheld legibility and persistence (MC15), accounting, repair log, `not_reexamined` (MC16, MC17), the containment guard on removals (MC10), stats, summary, last-run, dashboard card, Desktop summary line (MC37), README/README.txt | MA-344, 345, 346 (b, d), 347 (repair half) | **friction review** (upgrade walk: local home user, Desktop user, admin, custodian) + **adversarial pass** (tampered manifest, old-file read, same-envelope forgery) |

Each slice ships prove-fail → prove-pass per row (§9).
- S5 may ship before S2–S4: it needs only S1, now that it carries its own sinks
  (MC30).
- S2–S6 ship before S7, or in the same release, so users see one repair burst
  (MC43). **[ruling]** That includes S6: while Q2's fixture does not exist, S7 waits.
  Shipping S7 before S6 relaxes MC43, which is the owner's decision
  (`docs/owner-rulings-2026-10-09.md` §4).
- If the owner rules repair opt-in (Q1), S7 adds the flag to the shared flag
  builders (X4/X5, MA-72) and to the help walk (MA-39). It also names the GUI
  and Desktop control, the settings key and the job-descriptor field that carry
  the flag (MC37). **[ruling Q1]** Repair is automatic, so no flag is added.
- **[ruling ESC-6]** A Desktop repair action (Q6 / OR1) would be a further slice, MF-SD,
  after S7 with its own friction review; it is escalated, and does not start until the
  owner answers.

## 11. Open questions (OWNER RULING)

The review (`docs/review-mime-fidelity-predesign.md`) did not impose these. Each
blocks the slice named in §10. **All are answered** in `docs/owner-rulings-2026-10-09.md`
§4 (and the joint rulings of its §1): each keeps its question for context, followed by its
ruling.

- **Q1 — Automatic or opt-in repair?** Should a record damaged by an earlier
  version be re-captured automatically on the next ordinary run (proposed; it
  mirrors the v1 "unknown" re-examination, MA-70)? Or only when the operator asks
  (a `-repair` flag, plus the GUI/Desktop carriers of MC37), accepting that
  damaged pages stay damaged until then? Automatic costs a longer first run per
  scheme, and rewritten files (fresh fixity, listed in the repair log) for the
  repaired subset. Opt-in is also the canary lever of MC18. **Blocks S7.**
  **RULED (delegated-recommended): automatic,** on the next ordinary run;
  `state.RevokedSchemes` is the rollback lever.
- **Q2 — PST fixture provenance.** May we commit a PST containing method-5
  attached items, generated on a Windows Outlook host (or a third-party sample
  under a compatible licence)? Without it, MA-343 stays tier L, lab-pending, and
  **S6 does not merge** (MC32). S5's gap is the shipped floor.
  **RULED (delegated-judgement): yes, if it is synthetic** — generated on a Windows Outlook
  host from made-up messages, or a third-party sample whose licence is recorded in
  THIRD-PARTY-NOTICES. Until it exists, MA-343 stays L and pending, and S6 does not merge.
- **Q3 — Repair from preserved `.eml`.** Should a follow-up design add a
  source-free repair that re-parses each record's preserved `.eml`? It is the
  only cheap repair for with-Message-ID Graph mail and for archives whose source
  is gone, and the only repair for a defect of the mojibake class on such records
  (MC18). Or is `-mode full` enough? Does not block.
  **RULED (delegated-judgement): yes,** as its own design with its own 10-lens review; it
  does not block S0–S7.
- **Q4 — uuencode.** Keep `x-uuencode` parts still-encoded with a terminal gap
  (proposed), or add a small bounded uudecoder now (legacy Usenet/mail archives)?
  Does not block.
  **RULED (delegated-recommended): keep them still-encoded,** with a terminal
  `undecoded-part` gap and the bytes kept (R26); a bounded decoder can be a later slice.
- **Q5 — Identity freeze as house rule (restated, part of OR2).** Do you confirm
  P1 as the rule for every future parser change? Identity would always come from
  the frozen v1 replay, so v1's identity quirks persist unless a dedicated,
  migrated identity change is designed. **Both** designs are bound by this:
  - **fault-isolation:** its §4.5 stub change becomes an edit to
    `stubIdentityTerms`, never to `parsev1.go`, with its own legacy-stub
    migration (MC7);
  - **this design:** its drift policy (MC28) is the only way the frozen surface
    changes.
  **RULED (delegated-judgement): confirmed.** Identity comes from the frozen v1 replay for
  every future parser change, and the frozen surface changes only through §5's drift
  policy, which takes any MA-330 move to the owner. Fault-isolation's stub change is an
  edit to `stubIdentityTerms`, never to `parsev1.go` (J5, J8).
- **Q6 / OR1 — Repair route for with-Message-ID Graph mail, especially on
  MailArchive Desktop.** Desktop captures incrementally only and cannot run full.
  Choose one:
  - (a) a Desktop action, "Repair messages archived by an earlier version
    (re-downloads your mailbox)", with a size and time estimate, whose full walk
    writes only records where the repair fires. This amends full mode's
    "re-export everything" for that walk;
  - (b) rely on Q3 (Desktop keeps no `.eml` by default);
  - (c) accept the gap and disclose it, which MC16/MC37 already do.

  Does not block S0–S7. A ruling for (a) adds a Desktop slice with its own
  friction review.
  **ESCALATED (ESC-6).** (a) amends R2's "`full` re-exports all" for that walk, (b) cannot
  reach a default Desktop archive, and (c) accepts the gap, so every option needs the
  owner. Recommended answer: (a); if the owner would rather leave R2 untouched, MF-SD can
  instead be designed as a new, separately named repair walk, which is a pure addition.
  Interim: MC16/MC37's disclosure stands, the gap stays open, not accepted, and MF-SD does
  not start.
- **OR2 — Joint ruling with fault-isolation.** Decide:
  - one gap primitive (proposed: its `ReadGaps` + `GapClass`, with this design's
    kinds registered as informational);
  - ownership of `stubIdentityTerms`;
  - moving its `fallbackParse` `unparsed` edit into the fidelity walker;
  - which design owns the `Counts()`/status rewrite;
  - the landing order.

  Both design documents are amended to match. **Blocks S1.**
  **RULED = J5:** the proposed gap primitive; fault-isolation owns `stubIdentityTerms` (S1
  creates it with today's constant stub) and the `Counts()`/status rewrite (its S1a); the
  `unparsed` label lives outside `parsev1.go`; the order is fault-isolation S1a → S0 → S1 →
  fault-isolation S2b. The F33 identity fold is escalated (ESC-1, J8).
- **OR3 — Joint ruling with recapture-integrity.** Decide:
  - the repair's commit mode: write-beside with `-vN` naming (MA-346 then reads
    "same stem or its -vN successor"), or an explicit in-place exemption for
    repair in that design's R5 amendment;
  - MERGE disabled for repair;
  - the v1↔fidelity correspondence handed to its comparator;
  - which invariant owns "never to less";
  - the manifest v7 interplay with rollback and alternation (§6).

  **Blocks S7, and F46's write-path slices.**
  **RULED (delegated-judgement) = J6:** repair commits through recapture-integrity's
  verdict and W1–W5, with no in-place exemption; it takes only UNCHANGED, COMMIT or KEEP;
  V1C's correspondence is the T1-equivalent inventory; file names are what that commit
  gives (ESC-3), so MA-346 reads "the stem re-capture's commit gives it"; "never to less"
  is R28, which R27 cites; the v7 clause is moot (J2). R27 lands without a file promise
  for repairs (§9).
- **OR4 — Pin the Go toolchain?** The review's rescue proposed `toolchain` in
  go.mod and `go-version-file` with `check-latest` off. That would amend the
  deliberate CI/release policy (`ci.yml:26-33`: track the current patch for
  security fixes). Choose one:
  - pin, and make every Go patch an MA-330-gated bump;
  - keep floating, and accept P1's stated residual (`x/text`, `encoding/base64`
    and the unicode tables move with the toolchain).

  **Blocks S1.**
  **RULED (delegated-judgement): keep floating;** the CI policy is unchanged. P1's toolchain
  residual stays an open condition, not accepted (accepting it is not delegable). MA-330 and
  MA-331 run on every CI build, and S1 is asked to vendor `encoding/base64` into `parsev1`
  and have MA-331 assert `unicode.Version`, so the residual becomes a tripwire.
- **OR5 — Catalog id allocation.** R22 and S42 are claimed, with different
  meanings, by this design, fault-isolation, recapture-integrity,
  local-confidentiality and extraction-workspace. The operator allocates R and S
  ids across all five before any of their S0 slices lands. Until then this
  document uses R-PARSE, R-FREEZE and S-PARSE. **Blocks S0.**
  **RULED = J1:** R26 (R-PARSE), R27 (R-FREEZE) and S49 (S-PARSE); the rows keep
  MA-330..349, with MA-343 entering as tier L.

## 12. Honesty of claims (summary)

- Nothing here is built. Every "proven" is a commitment to a named tier-U/S row.
- **P1** is proven by construction for this design's own changes. For dependency
  and toolchain changes it is checked only on corpus shapes:
  - the identity surface is listed in full (MC26);
  - the replay's dependencies are vendored (MC27);
  - MA-331 pins the frozen files and `go.sum` lines;
  - the residual is `x/text`, `encoding/base64`, the unicode tables and the
    floating toolchain (OR4; an open condition, not accepted, [ruling OR4]);
  - drift is handled by §5's drift policy, never silently.
- **P2** is proven for the corpus and the listed triggers only. **P7**'s
  Windows-1252 guess is a guess for unlabelled bytes, and the page says so where it
  applied (MC25).
- **P4** holds for bytes as the store holds them. It is exact only for base64 and
  quoted-printable parts, and a truncated part is a `.partial` prefix (MC38).
- **P9** full is aspirational until a committed Outlook-written fixture makes
  MA-343 tier U (Q2). S6 does not merge before then, and the interim gap is the
  shipped floor (MC32). §1's F26 mechanism is from MS-PST and a simulation, not
  observed on an Outlook-written item.
- **P10**'s terminal class holds only for the deterministic kinds. Truncation on
  an on-demand source is fillable (MC20).
- **P11** is conditional on the source still holding the message. Graph
  with-Message-ID mail is repaired only by `-mode full` (or Q3), and **never on
  MailArchive Desktop** (OR1; escalated, ESC-6 [ruling ESC-6]). Its "never to less" is the
  content-level V1C check (MC3), not a count, together with recapture-integrity's R28,
  through whose commit a repair writes [ruling J6].
- **P12**: no duplicates, unconditionally. "Lose nothing" holds only while the
  source still holds the message when this build next runs (MC39).
- **P13**: "never worse than v1", panic or not (MC4). A stub appears only where
  v1 produced one (MC6). Fatal runtime errors that are not panics are not
  recovered.
- **Costs** (estimates, MC39):
  - one extra v1 parse per raw-MIME message per run, measured once at
    400–670 MB/s on fast hardware and guarded by a Benchmark;
  - transiently up to two decoded copies of a large message;
  - a rewrite of the repaired subset **once per parser scheme**, which every
    future scheme repeats (MC17), with each run's re-baselined files listed in
    its repair log;
  - a `verify` false-`modified` window if a repair is hard-killed. The next run
    heals it while the source still holds the message.
