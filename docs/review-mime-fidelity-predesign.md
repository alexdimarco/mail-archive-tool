# Pre-code design review — parser fidelity (`mime-fidelity`)

**Design reviewed:** `docs/design-mime-fidelity.md` revision 1 (2026-10-09).
**Procedure:** assurance-kit `process/design-review.md`, run as a workflow on
2026-10-09. All ten lenses ran as independent finder passes (L1 purpose/threat-model
fit, L2 focus/proportionality, L3 durability, L4 usability, L5 integration/seams,
L6 security/adversarial, L7 failure domains, L8 migration/coexistence, L9 dependency
and operational cost, L10 honesty of claims). A refutation-default skeptic then checked
every finding against the design text, the code and the sibling designs of the same
date (fault-isolation, recapture-integrity, local-confidentiality, extraction-workspace).
Last, one rescue agent, separate from every finder and the skeptic, worked on every
surviving blocker and major finding.

**Findings: 101 found · 96 confirmed · 5 refuted.** The skeptic graded the 96 confirmed
findings as 4 blocker, 36 major and 56 minor. **Rescue:** the 40 blocker and major
findings went to rescue. 30 were DISSOLVED by a named change, 10 were PARTIAL, and none
SURVIVES. The rescue also found **one hidden blocker made of two defects** that no lens
had raised (below).

**Verdict: GO_WITH_CONDITIONS.** No blocker survives the rescue, so no lens fails.

- **The blockers.** All four confirmed blockers (L1-1, L3-1, L6-1, L7-1) have one root:
  the count-based "never to less" precondition. **MC3** dissolves them.
- **The hidden blocker.** Its two defects become **MC1** and **MC2**.
- **Conditions.** **MC1–MC43** below must be fixed before code. All of them are folded
  into design revision 2, and each is cited where it lands.
- **Owner rulings.** Five changes would amend another design, the project's locked CI
  policy, or a product choice the owner has not made. The review does not impose them.
  They are listed as **OWNER RULINGS OR1–OR5**, and each blocks the slice named next to
  it.

**What may start, and what waits:**

| Slice | Waits on |
|---|---|
| S0 | OR5 (catalog id allocation) and the `BodyFallback` landing (MC29) |
| S1 | OR2 (fault-isolation joint ruling) and OR4 (toolchain) |
| S6 | Q2, which needs a tier-U fixture (MC32) |
| S7 | Q1, OR3 (recapture-integrity joint ruling) and a delta re-review of rev-2 §4.10 (MC43) |
| S2–S5 | Start once S1 has merged |

## Hidden blocker (rescue pass) — the repair can undo an owner's removal, and a withheld repair can record a present message as gone

No lens raised either defect. The rescue found both while it was taking the count
blocker apart.

**(H-1) Repair brings back messages the owner removed.** README.md:428-433 and MA-211
define a redaction as deleting a message's exported files. The README says "deleting
the files alone already hides it everywhere", and `reindex` compacts later. Until that
`reindex` runs, the record stays in the manifest. Today an incremental run never writes
a seen record that is not fillable (`exporter.go:236-264`), so that hidden state is
stable even while the source still holds the message.

Revision 1's precondition looks only at the old zip ("0 if the record had no zip"). It
never checks that the archived page still exists or still matches `Fixity.HTML`.

- **Who is exposed.** Any message with no attachments (most mail) that carries a common
  note (`legacy-8bit`, `charset-fallback`, `header-8bit`, `multi-segment-body`,
  `sender-fallback`) and whose page the owner deleted.
- **What happens.** old = 0 ≥ 0, so the repair runs. The first scheduled run after the
  upgrade (automatic under Q1) rewrites the page at the kept stem.
- **Result.** The message reappears in `serve` at every date, and in the owner's
  extraction workspace (select a source, choose the export destination, copy). The run
  reports it as "repaired N … nothing to do".

**(H-2) A withheld repair turns a present message into a false "gone".** A withheld
repair does "write nothing, change nothing", and it sits in place of the seen-skip
branch. On `DedupMailboxWide`, that branch is the only place where a no-Message-ID
record gets its `LastSeen`/`Present` stamped and its move recorded
(`exporter.go:236-262`).

`SweepGone` (`state/manifest.go:956-971`) marks gone every Present record in a walked
folder whose `LastSeen` is older than the run. So each withheld repair of a Graph
no-Message-ID message writes a durable, false `{k,gone}` history event. Those messages
are exactly the population P11 repairs on incremental runs. The go-back view then shows
a present message as deleted at every later date, which breaks R21. The extra withhold
reasons and the non-fatal write failures that the rescue introduces elsewhere make this
more frequent.

**Resolution.**

- **MC1.** The old page must exist as a regular file reached through the gate and must
  match its recorded fixity. Otherwise the repair is withheld as
  `archive-file-missing-or-modified`, whose remedy names `reindex`.
- **MC2.** Every outcome that does not commit (withheld, failed, unchanged-restamp) first
  runs the seen-skip branch's `MergeFields` and `OnManifestSkip`.

Both get MA-345 twins.

## Rescue outcomes in brief

- **DISSOLVED (30).** The four count blockers are dissolved by **V1C**. V1C is an
  item-level check: it compares archive entries by size and SHA-256 against the frozen v1
  replay of today's source bytes, so it proves the source is unchanged. Two further parts
  of V1C prove "fidelity ⊇ v1-now" and "same message". The other 26 are dissolved by the
  changes named in MC1–MC43.
- **PARTIAL (10).** Each one leaves a residual that only an owner can rule on, or a
  residual that is stated inline:
  - L1-2, L3-4: dependency and toolchain drift. Vendoring (MC27) closes the module
    bumps. The toolchain pin goes to **OR4**.
  - L1-4, L4-1: MailArchive Desktop cannot repair with-Message-ID mail. MC37 discloses
    this. The product choice is **OR1**.
  - L2-2, L5-5, L8-3: the repair's commit primitive versus recapture-integrity's
    write-beside rule. This is **OR3**.
  - L5-4, L8-4: one gap channel and stub ownership shared with fault-isolation. This is
    **OR2**.
  - L7-2: a decode defect of the mojibake class, on records whose source is later lost,
    can be repaired only from an `.eml`. This is stated inline (MC18).
- **One rescue change was not imposed.** For L1-2 the rescue proposed pinning
  `toolchain go1.26.9` and turning `check-latest` off. That contradicts the deliberate
  policy in `ci.yml:26-33`: "Track the current patch of the supported line instead",
  because a pinned patch misses every later security fix. It is therefore **OR4**, not a
  condition.

## Conditions (must fix before code; all folded into design revision 2)

- **MC1 (hidden blocker H-1). Repair never brings back a removed message.** The archive
  side of V1C runs through the archive-file gate (MC35):
  - the old page at `prev.Path` must exist and must match `Fixity.HTML` when that is
    recorded;
  - a recorded zip must exist and must match `Fixity.Zip`;
  - a recorded `.eml` must exist.

  Failing any of these withholds the repair as `archive-file-missing-or-modified`
  ("if you removed it on purpose, run reindex"). Rotted files are reported, not silently
  re-baselined. MA-345 gets a twin: delete the page, run, and assert that nothing is
  written. Closes H-1, L3-9, L3-10.
- **MC2 (hidden blocker H-2). An outcome that does not commit still stamps the timeline.**
  Withheld, failed and unchanged-restamp outcomes first run the seen-skip branch's
  `MergeFields(key, folderKey, seenAt, true)` and `OnManifestSkip`. MA-345 gets a
  live-path (fake Graph) twin asserting that `Present` stays true, `LastSeen` equals
  `runAt`, and `SweepGone` does not mark the message gone. Closes H-2.
- **MC3. V1C replaces the count precondition.** It has six steps:
  1. While `replayV1` runs, record each v1-now attachment's index, label, size and
     SHA-256, plus the v1-now `ContentDigest`. This needs no extra I/O.
  2. Archive side, as in MC1. Decompress the old zip entries within the extraction
     bounds and compute each entry's size and SHA-256.
  3. Every old zip entry must be matched one-to-one by (size, SHA-256), ignoring names,
     by a v1-now or fidelity-now attachment. Every v1-now Content-ID attachment that is
     not listed in Missing, Terminal or Unresolved must still have bytes.
  4. Fidelity ⊇ v1-now (MC4).
  5. Same message (MC5).
  6. Never repair a stub (MC6).

  For PST, v1-now is go-pst `WriteTo`'s bytes, junk included. An old junk entry is
  replaced by a gap only when 0x3701 is `PtypObject` (MC31). Delete "new ≥ old holds by
  construction" and cite `SourceComplete`. Rewrite §5 W2's "⊇" to cite V1C. MA-345 gains
  the masking cases: a cid image that is no longer consumed hiding a 0-byte attachment, a
  Thunderbird-detached part, the F25 named-inline offset, and the S6 offset. Each must be
  withheld. Closes L1-1, L3-1, L6-1, L7-1, L10-9.
- **MC4. Fidelity is never worse than v1, panic or not.** Apply this to every fidelity
  capture, not only to repairs:
  - every v1-now attachment that has bytes needs a fidelity counterpart with identical
    bytes;
  - a `text/*` part that v1 converted must convert, through the same go-message
    `charset.Reader`, to v1's bytes;
  - every non-empty v1 body field needs a non-empty fidelity body;
  - a v1 body that is valid UTF-8 and untouched by any body-decoding note must appear
    verbatim in the fidelity field.

  On failure, use v1 content (re-run `replayV1`, as for a panic), note
  `fidelity-regression`, and withhold any repair. Closes L6-2 and L7-2(b).
- **MC5. Prove it is the same message before repairing.** From S1 on, stamp
  `ContentHash` (the v1-view `ContentDigest` via `idv()`) on every write, local records
  included. It is not an identity input, so no key moves. A repair requires one of these:
  - a `sha:` key;
  - `prev.ContentHash` equal to v1-now's `ContentDigest`;
  - for legacy local `mid:` records, the archived page's normalized `.mailarchive-body`
    text (via `readArchivedHTML`) equal to v1-now's.

  Otherwise withhold as `cannot-confirm-same-message`. §8 states the overwrite
  consequence. Closes L6-3 and L7-1(c).
- **MC6. Records with stub identity.** A record whose v1 replay panicked is never
  repaired. It carries a visible terminal `identity-stub` parse gap (page, report,
  status) until the F33 stub change (OR2) gives stubs distinct identities. Reword P13 to
  "never worse than v1: a stub only where v1 itself produced one". Split §5's both-panic
  row into two rows. Closes L1-8, L7-4, L10-8.
- **MC7. Take the panic branch out of the freeze, and record provenance.** `parsev1.go`
  freezes only `parseMessage`, `fallbackParse`, `addressList` and `bytesWriterTo`, at a
  named release tag and commit. S1 diffs `mbox.go` against that tag and records the
  result. `replayV1` returns `(msg, panicked)`. Stub identity comes from a non-frozen
  `stubIdentityTerms(data)`, which reproduces the stub that the build which wrote the
  archive produced. An MA-330 corpus item that panics v1 pins it. Correct
  "none is known" so that it cites fault-isolation §4.5. Q5 names both designs. Ownership
  of `stubIdentityTerms` is OR2. Closes L1-6 and L10-11, plus the identity halves of
  L5-4 and L8-4.
- **MC8. Every write takes its stem from the identity view.** From S1,
  `stemFor(dir, date, m.idv().Subject, key)` applies to new captures, fills, full runs
  and repairs. Stems then never depend on the parser, and older binaries compute the same
  stems. The ordinary path removes a previous `.eml` only when this run wrote a new one.
  Otherwise it carries the `.eml` and `Fixity.EML` forward. MA-346 is extended to a fill
  and to a `-mode full` run on a legacy-8bit subject in a `-raw` archive, run without
  `-raw`. Closes L3-2, L5-6, L8-1.
- **MC9. Full mode routes noted records through the repair branch.** In `-mode full`, a
  seen record whose predicate fires takes the repair branch: V1C, kept stem, `.eml`
  carried forward, counted in `Repaired`. Every other full-mode record keeps today's
  behaviour, and next to P11 the design states that this behaviour is F46's unguarded
  re-capture. This gives MA-344's "in place" a real mechanism. Closes the mechanism half
  of L1-4.
- **MC10. The stem gate requires ownership and has no fallback.** The gate is: a clean
  single segment ending `.html`, under the computed directory, with
  `KeyForPath(prev.Path) == key`. Failing it withholds as `unsafe-path`. There is no
  fallback to `stemFor`, so a repair never renames and never deletes. In S7, also guard
  the removals at `exporter.go:485-489` with the `validRelPath` containment gate (this is
  a pre-existing hole in fill and full). MA-346 asserts that nothing outside `-out` is
  removed and that a sibling record's stem is refused. Closes L6-7 and L7-8.
- **MC11. A repair never writes an existing `.eml`.** It always carries the `.eml` and
  `Fixity.EML` forward. It writes a new one only where none existed and the run has
  `-raw`. Delete "with -raw the identical bytes are rewritten". Closes L6-4.
- **MC12. Unchanged output only restamps, and every re-read stamps.**
  - Build the fidelity page and zip in memory. If both digests equal the recorded fixity,
    restamp in memory: no write, no history event, counted `Restamped`.
  - A seen record that is re-read and whose predicate is false is stamped current in
    memory.
  - The R2 clarification becomes "at most one rewrite per record per scheme and source
    content".
  - P12 and §6 state that an older binary's save, of any kind, drops `parse_scheme` from
    every record, so the cost is one in-memory re-examination per alternation.
  - MA-347 runs 3 alternations and expects at most 1 rewrite per record.

  Closes L3-3, L6-12, L8-2.
- **MC13. Repair write discipline.** Stage the zip and html temps, then commit the zip,
  then the html. Any write failure keeps the old record and files, does not stamp, counts
  `RepairFailed` with the path and the OS error, and is never an `Export` error, so the
  walk, the Graph mailbox and the gone sweep all continue. After 20 consecutive failures,
  stop repairing for that run. A stale zip is removed only after the html commit. §5
  lists that removal, and states that convergence depends on the source. MA-345 injects a
  rename refusal on the zip and on the html. Closes L3-6, L3-8, L7-7.
- **MC14. Recovers around new export-side code.**
  - Each embedded-item render in `WriteZip` has its own recover; a panic becomes an
    `embedded-item` gap.
  - The S4 tree embedding and the gaps/notes blocks have a recover that re-renders with
    those features off and adds a `render-fallback` note.
  - The whole repair attempt runs under a recover; a panic becomes withheld
    `repair-error`.

  MA-349 injects each of these panics. Closes L7-5.
- **MC15. Withheld repairs are legible and persisted.**
  - Reasons and counters: `source-changed`, `fidelity-regression`,
    `cannot-confirm-same-message`, `archive-file-missing-or-modified`, `unsafe-path`,
    `over-bound`, `repair-error`, plus `RepairFailed` for write failures. Each has its own
    what/why/remedy.
  - The archive-side reasons are integrity findings, routed to `verify` and `status`.
  - `Record.RepairWithheld {Scheme, Reason, SourceDigest}` (omitempty). While
    `SourceDigest` is unchanged, the re-check and the warning are skipped.
  - The report lists withheld records. `status`, `status -json`, the GUI and the Desktop
    health view show the counts. The warning prints only when the count changes.
  - A missing file is never restored except by an explicit operator action.

  Closes L4-2, L4-3, L7-6.
- **MC16. Show migration progress.** A `not_reexamined` count in `status` and
  `status -json` (records with `ParseScheme <` current, which MC12 makes exact), with a
  remedy per source: local, the next run; Graph, `-mode full`; Desktop, OR1. The
  workspace's caveat for older records is keyed on it. §6's index statement is qualified
  to "for records written at the current scheme". Closes L8-6, L8-9, and the status half
  of L4-4.
- **MC17. Accounting and audit trail.** `Exported` never counts repairs; every surface
  reads "exported 0 new · repaired N". A per-run `repairs-<run-id>.tsv` (key, path, old
  fixity, new fixity, note codes), written only when something was repaired, is named in
  the summary and the Activity log. The wording "nothing to do" is replaced. §7 and §12
  say "once per parser scheme; every future scheme repeats it". Closes L4-5, L4-6, L5-9,
  L9-6.
- **MC18. Revoked schemes, and the true blast radius.** Add `state.RevokedSchemes`: the
  predicate also fires when `prev.ParseScheme` is in that list. §5 and §6 state the real
  blast radius, which is every record whose message triggers a non-panic defect that MC4
  cannot see, and the real rollback: an older binary plus `-mode full`. They also state
  the residual: a mojibake-class defect on a record whose source is gone can be repaired
  only from an `.eml`. Closes L7-2 (PARTIAL).
- **MC19. Fidelity fallback is visible and does not loop.**
  - A fallback write stamps the current scheme and sets
    `Record.FidelityFallback = <scheme>`. Repair is suppressed while the current parse
    falls back. A later scheme repairs the record.
  - The record gets a visible terminal `fidelity-fallback` gap (page, report, status) and
    a `LastRun.FidelityFallback` count.
  - The fallback's display fields go through `decodeText`.
  - MA-340's property runs on `safeParseMessage`. MA-349 asserts that a second run
    repairs 0 records for a fallback message with a cid text reference.

  Closes L1-9, L1-11, L6-6, L7-3.
- **MC20. Gap classes follow R1's source rule.** `undecoded-part`, `structure-limit`,
  `embedded-item`, `ole-object`, `identity-stub` and `fidelity-fallback` are terminal
  everywhere. `corrupt-part` and `unsplit-multipart` are fillable when `!SourceComplete`
  (the probe: a re-parse no longer yields the gap) and terminal on Graph. MA-334 gets a
  twin: truncated on the first run, complete on the next, on an on-demand source, and the
  record is filled. Closes L1-7 and L10-6.
- **MC21. Bound the gap labels.** At most 32 parse-gap labels per record, plus one
  summary label "and N more undecodable parts". Report rows are capped per record the
  same way. MA-349 asserts the manifest size. Closes L3-5.
- **MC22. Explicit header caps.** An explicit `LimitedReader` (1 MiB) on the top-level
  header and on every part header, listed in P13. RFC 2231 continuation indices are at
  most 64 and are joined by a sparse sort. MA-349: an oversized part header gives
  `structure-limit` and leaves the other parts intact. Closes L6-13 and L10-7.
- **MC23. Harden data URIs.** Embed only an allowlisted image type that is lowercased and
  matches the RFC 2045 token grammar (png, jpeg, gif, webp, bmp, x-icon). Anything else is
  not embedded and stays in the zip. The URI is inserted quoted. MA-301's re-parse check
  is extended to fail closed when a reserved `mailarchive-*` class or a
  `data-mailarchive-*` attribute appears outside tool-owned markup. The `</style>`
  breakout case goes into MA-339 and MA-349. Closes L6-5.
- **MC24. Close the cid prefix false negative.** `renderNotes` also flags a `cid:`
  occurrence whose attachment CID is a strict prefix of a longer token, which is v1's
  over-consumption. §4.10 names MA-332 as the guard that catches a missed note
  registration, over the corpus. Closes L2-3.
- **MC25. The notes block names its property.** The collapsed notes block serves P7's
  condition: where a guess (Windows-1252, an unknown label) shaped the displayed text, the
  page says so. It lists only notes that qualify displayed text. Closes L2-5.
- **MC26. List the full identity dependency surface, with honest labels.**
  - P1(b) lists: go-message, including its charset table; go-pst's nameMap, registered at
    init and again in `openPST`; `x/net/html/charset`; `x/text`; go-mbox framing; stdlib
    `net/mail`, `mime`, `mime/quotedprintable` and `encoding/base64`; the Go toolchain.
  - (c) is reworded to "no mutation beyond the init-time registrations, which are part of
    the frozen surface".
  - (a)–(c) are relabelled: by construction for this design's changes; checked only on
    corpus shapes for dependency changes.
  - MA-331 snapshots how every label resolves (IANA, WHATWG, go-message quirks, go-pst
    nameMap) and pins the sha256 of `parsev1.go` and the `go.sum` lines.
  - The corpus gets one item per nameMap label.
  - §12 is made to match.

  Closes L1-2 (PARTIAL), L5-13, L9-2, L10-1, and the residual of L8-7.
- **MC27. Vendor the replay's dependencies.** Vendor these into `internal/source/parsev1/`:
  go-message v0.16.0's `mail`, `textproto` and `charset` (with a snapshot of the
  effective label table, go-pst's nameMap included); stdlib `net/mail`'s address and date
  parser; `mime.WordDecoder`, `ParseMediaType` and `mime/quotedprintable`. All are
  BSD-licensed. MA-331 asserts via `go list -deps` that `parsev1` imports none of
  `net/mail`, `mime` or go-message. The residual is stated inline: `x/text` charmap,
  `encoding/base64` and the unicode tables still float with the toolchain (OR4). Closes
  L3-4 (PARTIAL) and part of L9-1.
- **MC28. Drift policy and the 3am drill in §5.**
  - A reachable CVE in the frozen surface gets a minimal patch to the frozen copy. MA-330
    must stay green, and a corpus-seeded differential fuzz must show v1 output changing
    only on inputs that the patch now rejects or bounds.
  - If MA-330 goes red, an owner ruling either accepts named drift (with a README note on
    the duplicate consequence) or keeps the old behaviour.
  - govulncheck reachability through `parsev1` goes in the release notes.
  - A toolchain end-of-life move follows the same procedure.

  Closes L9-1.
- **MC29. Split the goldens and order S0.**
  - The identity and content goldens stay frozen from the unmodified build, at a named
    tag (MC7).
  - MA-332 becomes parser-scoped: a message with no note has a `model.Message` equal to
    the content golden, and its render equals the current renderer applied to the v1
    message, computed inside the test.
  - Each golden states its regeneration rule.
  - S0 runs after `BodyFallback` lands, or excludes the messages it affects.

  Closes L9-4 and the S0 half of L5-2.
- **MC30. Make the build order sound.**
  - The nil-`WriteTo` guard (in `WriteZip`, `drain` and `hasArchivable`), the
    GapKind→Terminal routing and the report kind move into S5. MA-342 asserts that the
    outer message is exported.
  - MA-333's page assertion and MA-348's page blocks go green at S4.
  - MA-347 is split: the identity half at S1, the repair half at S7.

  Closes L2-4, L5-1, L5-2.
- **MC31. Harden the PST path.**
  - S5 decides on the property type of 0x3701: `PtypObject` means an embedded item;
    `PtypBinary` keeps today's bytes.
  - The visited set is keyed on block ids (`LocalDescriptorsIdentifier`,
    `DataIdentifier`), not NIDs. `ulSize` is advisory and never sizes an allocation.
  - Each top-level message has a budget: at most 10,000 embedded entries, decoded bytes
    at most 4× the PST file size, and a per-embed step budget. Past the budget, record
    `structure-limit`.
  - Each go-pst error class maps to a named `embedded-item` detail, and is counted.
  - §8 gets a dependency paragraph (go-pst upstream status, owner, fork/patch plan via a
    `replace` directive and THIRD-PARTY-NOTICES).
  - MA-343 adds a fixture where the inner and outer NIDs coincide.

  Closes L6-8, L6-9, L6-10, L9-3.
- **MC32. S6 is gated on a real fixture.** S6 may not merge until MA-343 is tier U on a
  committed, Outlook-written method-5 fixture (Q2). Until then S5's gap is the shipped
  floor and P9-full is labelled aspirational inline. §1's F26 mechanism is relabelled
  "per MS-PST 2.3.3.5 and an in-memory simulation; not observed on an Outlook-written
  item". Closes L10-5.
- **MC33. The page names match the zip.** Final zip entry names are computed once, before
  rendering, and the page, placeholders and gaps block all use them. The subject prefix
  of nested entries is capped at 60 runes. The suffixes `(attached message).html`,
  `.undecoded` and `.partial` are reserved: a sender's file ending in one is renamed. The
  outer page lists the entries the tool rendered. Closes L4-8 and L6-11.
- **MC34. Migrate the labels.** `probeImproves` matches an old Missing label against
  v1-now labels through the replay: the v1 label counts as improved when its v1-now
  attachment has bytes. The written record carries fidelity labels. §6's rollback
  paragraph states that an older binary cannot fill gaps labelled with fidelity names.
  Closes the label half of L5-5, and L8-8.
- **MC35. One shared primitive each.** Move the legacy decode (`decodeBytes`), the lenient
  address (reconciled with `interchange.headerAddress`) and the archive-file gate
  (shared with `extract`, `verify` and recapture-integrity's inventory reader) into
  `internal/util`, and call them from every site. Closes L5-10 and the reader half of
  L5-5.
- **MC36. The extraction workspace inherits the properties only under a condition.** This
  is the owner's chosen tool: select a source, choose the export destination, copy, with
  no drag-and-drop.
  - It inherits P2–P10 where it re-parses an original: the Microsoft 365 source, and the
    archive source when an `.eml` is present.
  - For the archive source without an `.eml`, it copies the archived files. Those carry
    the properties only if the record was captured or repaired at the current scheme.
  - Seam items for the workspace design:
    - Q9 depends on S2;
    - entry notes for GapKind parts and ParseGaps;
    - an entry note for records written by an earlier version (MC16);
    - nil-`WriteTo` safety;
    - the repair burst running alongside an archive-source export (§6).

  Closes L1-5, L5-7, L10-3, L5-8.
- **MC37. Make the operator walk truthful.**
  - §7 splits Home user into a local-source actor (0 steps) and a MailArchive Desktop
    actor. For the Desktop actor, the P11 Graph condition is written inline, and a summary
    line reads "N messages were archived by an earlier version; Desktop cannot re-check
    them without a full re-download".
  - The Admin step's true cost is stated: a full run re-exports and rewrites every
    record.
  - If Q1 rules opt-in, the GUI or Desktop control, the settings key and the
    job-descriptor field are named under X5.

  Closes L1-4 (the disclosure half), L4-1 (PARTIAL), L4-4, L5-11, L10-4.
- **MC38. P4 is honest about bytes.** P4 now reads: transfer-decoded bytes as the store
  holds them, never charset-converted. This is exact for base64 and quoted-printable
  parts. 7bit and 8bit parts from mbox are as go-mbox frames them. A truncated part is
  kept as `<name>.partial`. MA-337 adds an 8bit text attachment with LF-only line ends in
  an mbox. Closes L1-10 and L10-12.
- **MC39. Claim sweep.**
  - "Adds no UI" is replaced by the list of surfaces it changes. A friction review and an
    X-series check are owed on S2 (wording) and on S4 (page blocks).
  - P12's "lose nothing" gets its condition (the source still holds the message).
  - The costs are labelled estimates. A corpus Benchmark ships with S2.
  - The memory claim becomes "up to two decoded copies, transiently".

  Closes L1-12, L1-15, L9-5, L10-10.
- **MC40. Fault-isolation seam (this design's side).** Delete "F23 … reuses
  `Attachment.GapKind`". §4.1 and §4.9 adopt whichever single gap primitive OR2 rules.
  `fidelity-fallback`, `RepairWithheld` and `RepairFailed` are informational counts
  under fault-isolation's partial/exit-3 partition: never partial, never exit 3. S1 does
  not merge before OR2. Closes L2-6, and L5-4 and L8-4 (PARTIAL).
- **MC41. Recapture-integrity seam (this design's side).**
  - Delete "stricter than, and independent of, F46 … calls it in addition".
  - Rewrite rejected alternative (D).
  - Repair offers recapture-integrity only UNCHANGED, COMMIT or KEEP, never MERGE, and
    supplies V1C's v1↔fidelity correspondence as its inventory.
  - §6's rollback is written for both manifest outcomes, v6 and v7.
  - S7 does not merge before OR3.

  Closes L2-2, L5-5 and L8-3 (PARTIAL).
- **MC42. Working catalog ids.** Until the operator allocates ids (OR5), use working
  names: R-PARSE (parse fidelity, revision 1's R22(a)), R-FREEZE (identity freeze and
  repair, R22(b)) and S-PARSE (S42). The "never to less" clause follows OR3. S0 checks
  that the catalog text of each cited id matches this design's wording. Closes L1-14,
  L3-X1, L5-3, L8-5, L10-2.
- **MC43. S7 gets its own gate.** S7's build waits on Q1, on OR3, and on a delta
  re-review of rev-2 §4.10 (lenses 3, 6, 7, 8). S2–S6 ship before S7, or in the same
  release, so users see one repair burst. Closes L2-1, and part of L7-2(d) and L9-6.

## OWNER RULINGS (surfaced, not imposed)

- **OR1 (design Q6, new). How does with-Message-ID Graph mail get repaired, especially on
  MailArchive Desktop?** Desktop captures incrementally only (`capture.go:43, :141`).
  Changing that is a product choice. The options:
  - (a) A Desktop action, "Repair messages archived by an earlier version (re-downloads
    your mailbox)", whose full walk writes only records where the repair fires. This
    amends full mode's "re-export everything" for that walk.
  - (b) Q3's repair from the preserved `.eml` (Desktop keeps no `.eml` by default).
  - (c) Accept the gap and disclose it.

  Does not block S0–S7, because MC16 and MC37 disclose the state. A ruling for (a) adds a
  Desktop slice with its own friction review. (L1-4, L4-1, L4-4, L10-4)
- **OR2. Joint ruling with fault-isolation.** It covers:
  - one gap primitive: that design's `ReadGaps` and `GapClass`, with this design's kinds
    registered as informational, not partial;
  - ownership of `stubIdentityTerms` (MC7);
  - moving fault-isolation §4.5's `fallbackParse` `ReadGaps:[unparsed]` edit into the
    fidelity walker's header-fallback branch;
  - which of the two designs owns the `Counts()`/status rewrite;
  - the landing order;
  - Q5 restated so it names both designs.

  This amends design-fault-isolation.md, which this review cannot do. **Blocks S1.**
  (L1-6, L2-6, L5-4, L8-4)
- **OR3. Joint ruling with recapture-integrity.** It covers:
  - the repair's commit mode: write-beside with `-vN` naming (MA-346 then amended to
    "same stem or its -vN successor"), or an explicit in-place exemption for repair in
    that design's R5 amendment;
  - MERGE disabled for repair;
  - the v1↔fidelity correspondence handed to its comparator;
  - which invariant owns "never to less";
  - what the manifest moving to v7 does to rollback and to alternating binaries.

  **Blocks S7, and F46's write-path slices.** (L2-2, L5-5, L8-3)
- **OR4. Pin the Go toolchain?** Pinning (`toolchain` in go.mod, `go-version-file`,
  `check-latest` off) would amend the deliberate CI and release policy (`ci.yml:26-33`).
  The choice:
  - pin, and make every Go patch an MA-330-gated bump;
  - keep floating, and accept the residual inline (`x/text`, `encoding/base64` and the
    unicode tables move with the toolchain; MC27).

  **Blocks S1** (the freeze becomes house rule under Q5). (L1-2, L3-4, L8-7, L9-2)
- **OR5. Allocate catalog ids across the five concurrent designs.** R22 and S42 are
  claimed with different meanings by mime-fidelity, fault-isolation, recapture-integrity,
  local-confidentiality and extraction-workspace. The covers-map meta-test checks only
  that a cited id exists. Allocation is an operator act. **Blocks S0.** (L1-14, L3-X1,
  L5-3, L8-5, L10-2)
- **Still open from revision 1:** Q1, automatic or opt-in repair (blocks S7, and is the
  canary lever of MC18). Q2, fixture provenance (blocks S6 via MC32). Q3 and Q4 do not
  block. Q5 is restated as part of OR2.

## Findings table

Verdict cells give the skeptic's verdict and severity, and for blockers and majors the
rescue outcome (DISSOLVED, PARTIAL or SURVIVES). "Rescue" in the Lens column marks a
defect the rescue pass found.

| ID | Lens | Design section (or OMISSION) | Claimed failure | Verdict | Evidence / refuting quote | Condition # |
|---|---|---|---|---|---|---|
| H-1 | Rescue | §4.10 Precondition (OMISSION: the old page) | A record the owner redacted (page deleted, README.md:428-433, MA-211) with no zip and a common note gives old = 0 ≥ 0 and is re-created by the next automatic run; it reappears in `serve` and the workspace | CONFIRMED · blocker (hidden) · DISSOLVED | §4.10 "0 if the record had no zip"; today a non-fillable seen record is never written (`exporter.go:236-264`) | MC1 |
| H-2 | Rescue | §4.10 Withheld ("write nothing, change nothing") | On the live path a withheld repair skips `MergeFields`/`OnManifestSkip`; `SweepGone` (`manifest.go:956-971`) writes a false `{k,gone}` for a present no-Message-ID message (R21) | CONFIRMED · blocker (hidden) · DISSOLVED | `exporter.go:236-262`: the seen-skip branch is the only place such a record's `LastSeen`/`Present` are stamped | MC2 |
| L1-1 | 1 | P11; §4.10 Precondition; §5 W2 | Count check: a cid-placeholder image that P6 no longer consumes masks report.pdf's lost bytes on a Thunderbird IMAP cache (new=1 ≥ old=1); the repair renames a zip without report.pdf over the only copy | CONFIRMED · blocker · DISSOLVED | §4.10 "Let `old` = its entry count … Repair only if `new ≥ old`"; only Graph is `SourceComplete` (`graph.go:113`); WriteZip commits by rename (`attach.go:57-101`) | MC3 |
| L1-2 | 1 | P1 (a)–(c); H3; §12 | The identity dependency set is incomplete (go-pst's init charset registrations, the toolchain), and the conditions are labelled proven from a corpus | CONFIRMED · major · PARTIAL | go-pst `pkg/charsets.go:70-72` `init(){ ExtendCharsets(…) }`; `reader.go:56-60`; H3 itself says "which the PST reader also extends"; go.mod has no toolchain line | MC26, MC27; OR4 |
| L1-3 | 1 | §4.3 step 6 | Text attachments labelled `us-ascii`/`utf-8` were U+FFFD-destroyed by v1 and are never noted | REFUTED | go-message v0.16.0 `charset.go:38-41`: `if charset == "utf-8" \|\| charset == "us-ascii" { return input, nil }`: v1 passed them through byte for byte. The design cites this in §1 C1-8 ("passes through unchanged (`charset.go:38-42`)") | — |
| L1-4 | 1 | P11 Graph; §6; MA-344; §7 | `-mode full` "repairs in place" has no mechanism (the predicate is on the incremental seen path only; full mode re-stems from the decoded subject); Desktop is incremental-only | CONFIRMED · major · PARTIAL | `exporter.go:235` `if e.Mode == Incremental && seen`; `:306` `stemFor(…, m.Subject, …)`; `desktop/capture.go:43, :141` `Mode: export.Incremental` | MC8, MC9, MC37; OR1 |
| L1-5 | 1 | Header "inherits every property"; §4.11; §7 | False for archive-source records without an `.eml` in the owner's chosen select-source → choose-destination → copy tool (the Desktop default) | CONFIRMED · major · DISSOLVED | design-extraction-workspace.md §4.11-4.12 copies the page and zip byte-exact; `desktop/settings.go:17` KeepRaw defaults to false | MC36 |
| L1-6 | 1 | §4.2 "none is known"; §4.11; Q5 | Fault-isolation §4.5 rewrites the recover that replayV1 freezes; landing order decides whether F33 is undone or blocked; the two designs give different outcomes for a panic | CONFIRMED · major · DISSOLVED | design-fault-isolation.md:523-527 `{Subject:"(unreadable message)", PlainBody: string(data), Raw: data, ReadGaps:[unparsed]}`; its seam note calls the overlap "textual only" | MC7; OR2 |
| L1-7 | 1 | P10; §4.9 "Terminal, always" | A truncated part on an on-demand cache is terminal, so the complete copy that arrives later is never filled; this contradicts R1 | CONFIRMED · minor | R1 (`scenario-catalog.md:14-20`) "fillable (an on-demand source may still deliver it)"; not a regression (`mbox.go:289` ignores the read error) | MC20 |
| L1-8 | 1 | §5 "Both → stub identity, fidelity content"; §4.2 `panicked` | v1-panicking messages share a stub key; the first is archived complete, every later one is silently `SkippedManifest` | CONFIRMED · minor | `exporter.go:236-259` `!prev.Fillable()` → `SkippedManifest++`; today the stub carries Missing [body] | MC6 |
| L1-9 | 1 | §4.2(3); §4.10 predicate | A fallback write stamps 0, and `renderNotes` on v1 content (`cid-text-ref`) fires again: the record is rewritten every run | CONFIRMED · minor | §4.2 "stamped `parse_scheme` 0"; the predicate unions `renderNotes(m)` with no fallback exclusion | MC19 |
| L1-10 | 1 | P4 | "The bytes the sender attached" is unconditional, yet a corrupt part keeps its prefix under the real name and go-mbox rewrites line ends and `>From ` | CONFIRMED · minor | §4.3 step 4 "keeps the decoded prefix under the real name"; go-mbox v1.0.4 `reader.go:55-70` | MC38 |
| L1-11 | 1 | P7 vs P13 / §4.2(3) | Fallback content is v1's invalid UTF-8; MA-340 tests `parseFidelity`, not `safeParseMessage` | CONFIRMED · minor | P13 "degrades that message to exactly what v1 produces"; MA-340 "every text field of `parseFidelity(any bytes)`" | MC19 |
| L1-12 | 1 | P12 "lose nothing" | An older binary's full run un-repairs; once the source drops the message, the fidelity content is gone for good | CONFIRMED · minor | P12's residual concedes the un-repair but not the source condition | MC39 |
| L1-13 | 1 | P1; §3 first non-goal | ContentDigest frozen to v1 terms cannot split reuses that differ only in parts v1 dropped | REFUTED | Conceded non-goal: §3 "v1 identity quirks therefore persist for new captures too — notably …" ("notably" is not the full list). ContentDigest is already v1-derived today (`message.go:179-192`), so `placeByPhysID` behaves as now | — |
| L1-14 | 1 | §9 "Proposed catalog additions (S0): R22 … S42" | R22/S42 collide with four sibling designs, so covers markers silently cite another invariant | CONFIRMED · minor | recapture-integrity.md:190/217, fault-isolation.md:188/228, local-confidentiality.md:7/153, extraction-workspace.md:5; the catalog ends at R21/S41 | MC42; OR5 |
| L1-15 | 1 | Header "This design adds no UI" | Page blocks, GUI split, status lines and a dashboard figure ship with no friction or X-series check on S2 | CONFIRMED · minor | §4.7 gaps/notes blocks; §4.9 GUI/status wording; §4.10 dashboard `repaired`; §10 owes friction only at S3/S7 | MC39 |
| L2-1 | 2 | Whole doc; §10; Q1 | One gate covers 13 properties and 8 slices; the only slice that mutates the archive depends on Q1 and on a sibling primitive | CONFIRMED · minor | §10 "If the owner rules repair opt-in (Q1), S7 adds the flag …"; the slicing already lets S0–S6 land independently | MC43 |
| L2-2 | 2 | §3 F46; §4.10; §4.11; §5 W2 | A second never-degrade primitive, weaker (a count) than recapture-integrity's per-item KEEP/MERGE, whose rename-over breaks that design's R5 amendment | CONFIRMED · major · PARTIAL | §4.11 "P11's precondition is stricter than, and independent of, F46's general rule … Repair never renames"; recapture-integrity P5 "beside the old ones … never over a recorded file" | MC3, MC41; OR3 |
| L2-3 | 2 | §4.10 predicate / notes table | Hand-maintained notes have false negatives, e.g. CID `abc` inside `src="cid:abcdef"`, which v1 consumed | CONFIRMED · minor | `renderNotes` skips occurrences "preceded … by `src=`"; v1's regex `(?i)cid:`+QuoteMeta(cid) matches prefixes (`html.go:649`) | MC24 |
| L2-4 | 2 | §10 "S5 may ship before S2–S4" | S5's nil `WriteTo` panics `WriteZip`/`drain`; the whole message is recovered as skipped | CONFIRMED · minor | `attach.go:70`, `html.go:673` call WriteTo unconditionally; `hasArchivable` (`exporter.go:773-780`); MA-342 would stay red, so prove-pass blocks shipping | MC30 |
| L2-5 | 2 | §4.7 collapsed notes block | The block traces to no property and adds a renderer surface mail can influence | CONFIRMED · minor | P10 covers gaps only; P7 says nothing about showing notes | MC25 |
| L2-6 | 2 | §4.1 ParseGaps; §4.9 NUL-prefixed labels | A second gap channel next to fault-isolation's `ReadGaps`/`GapClass` | CONFIRMED · minor | fault-isolation.md:323-333, 390; §4.11 "F23 … reuses `Attachment.GapKind`" vs fault-isolation §4.4 `unreadable:attachments` | MC40; OR2 |
| L3-1 | 3 | §4.10 Precondition; P11 | Thunderbird Detach keeps the part's name and replaces its bytes; the counts are equal; the repair overwrites the real PDF with the placeholder | CONFIRMED · blocker · DISSOLVED | Fingerprint hashes names only (`message.go:154-161`); the count check compares no bytes | MC3 |
| L3-2 | 3 | §4.10 In place; MA-344; §6 Graph | Fill and full runs re-stem from the decoded subject; the old html, zip and `.eml` are deleted even without `-raw` | CONFIRMED · major · DISSOLVED | `exporter.go:306`, `:485-490`; `util.Slug` (`paths.go:112-128`); rawName only with KeepRaw (`:307-309`) | MC8, MC9 |
| L3-3 | 3 | P12; §6 Mixed versions; R2 clarification | Any save by an older binary drops `parse_scheme` from every record, so the noted subset is re-repaired once per alternation | CONFIRMED · major · DISSOLVED | `manifest.go:524` Unmarshal / `:1391` Marshal of a fixed struct; `health.go:535-538` ExeIsThis | MC12 |
| L3-4 | 3 | P1(b); §4.2 | The freeze rests on stdlib and toolchain code that go.mod cannot pin; a security update and identity conflict | CONFIRMED · major · PARTIAL | go-message `mail/address.go:4-5`, `mail/header.go:8` import `net/mail`, `mime`; `release.yml:28-30` `'1.26.x'`, check-latest | MC27, MC28; OR4 |
| L3-5 | 3 | P10 / §4.9; P13 | No per-record bound on parse-gap labels (10,000 leaves × 300 runes) | CONFIRMED · minor | not a regression (v1 has no leaf bound) | MC21 |
| L3-6 | 3 | §5 "Disk full … exactly like a fill's write error" | A zip commit failure invents Failed/Terminal gaps over an intact zip; an html failure aborts the walk at the same record every run | CONFIRMED · major · DISSOLVED | `exporter.go:383-391`, `:406-407`, `:387`; `run.go:529-531`; `graph.go:636-642` | MC13 |
| L3-7 | 3 | §4.10 Scheme / notes "Since (slice)"; §10 | Since is given as a slice label; out-of-order landing breaks the predicate | REFUTED | §4.10 "Each slice that changes what is written bumps it by one and registers its note codes with that `Since` value"; "(S1 bumps the scheme to 1 …)". Since is the integer assigned at landing | — |
| L3-8 | 3 | §5 "No step deletes a file" | The reused write path removes the old zip when nothing is archivable | CONFIRMED · minor | `exporter.go:382` `os.Remove(zipPath)`; `attach.go:98-101` | MC13 |
| L3-9 | 3 | §4.10 Precondition | No check that the page exists; a redacted message is re-created before `reindex` | CONFIRMED · minor (raised by the rescue to hidden blocker H-1) | README.md:428-432 | MC1 |
| L3-10 | 3 | §4.10 Precondition / In place | Old files are not checked against recorded fixity before being overwritten; the storage-decay signal is lost | CONFIRMED · minor | §4.10 reads only the central directory | MC1 |
| L3-X1 | 3 (×5) | §9 R22/S42 | Five designs claim R22 and S41–S45 | CONFIRMED · minor | as L1-14 | MC42; OR5 |
| L4-1 | 4 | §7 Home user / Extraction-tool user | The Desktop user never gets repairs for with-Message-ID mail; archive-source copies hand over v1 files (Type II) | CONFIRMED · major · PARTIAL | `capture.go:43, :141`; `settings.go:17`; nothing marks pre-fidelity records | MC16, MC36, MC37; OR1 |
| L4-2 | 4 | §4.10 Withheld; X6 | Withheld is not persisted, itemized or in status; it is re-checked and re-warned every run forever | CONFIRMED · major · DISSOLVED | §4.10 "write nothing, change nothing, count `Stats.RepairWithheld`"; X6 | MC15 |
| L4-3 | 4 | §4.10 Withheld wording; MA-345 | One counter and one "source holds less" remedy for four different causes | CONFIRMED · major · DISSOLVED | MA-345 "missing-but-recorded, unreadable, symlinked or over the entry cap → withheld"; one warning text | MC15 |
| L4-4 | 4 | §7 Admin; §6 Graph | The full-run cost is understated: it rewrites every record, which is rejected alternative (D) | CONFIRMED · minor | `exporter.go:235` skip is Incremental-only; §4.11 (D) | MC16, MC37; OR1 |
| L4-5 | 4 | §4.10 Accounting "nothing to do" | Repaired files are not listed and the prior fixity is discarded | CONFIRMED · minor | §4.10 accounting text | MC17 |
| L4-6 | 4 | §4.10 Accounting | The exported headline includes repairs | CONFIRMED · minor | `capture.go:146` `res.Stats.Exported`; `cmd/mailarchive/main.go:1361` | MC17 |
| L4-7 | 4 | §5 W2 residual | An interrupted repair looks like tampering (verify RED) | REFUTED | §3 "re-capture commit ordering (N2, N3): owned by the re-capture-integrity design"; §5 "If `verify` runs inside that window it reports `modified` … doing so is harmless (the next run repairs again)" | — (MC13 also narrows the window) |
| L4-8 | 4 | §4.7; §4.5; §4.8 | The page names files differently from the zip (uniqueName/SanitizeFilename run after render; 120-rune cap) | CONFIRMED · minor | `attach.go:82`; `paths.go:26, 71-72` | MC33 |
| L5-1 | 5 | §10 S5 order; §4.7 WriteZip in no slice | Same root as L2-4 | CONFIRMED · minor | `run.go:520-526` recovers with err=nil; MA-342 red blocks | MC30 |
| L5-2 | 5 | §10 "Rows turned green"; S0 goldens | MA-333's page check and MA-348 are listed at S2 but their blocks are S4; MA-347 is vacuous at S1; the S0 golden races BodyFallback | CONFIRMED · minor | §10 table; header notes the concurrent BodyFallback edit | MC29, MC30 |
| L5-3 | 5 | §9 | R22/S42 collision | CONFIRMED · minor | as L1-14 | MC42; OR5 |
| L5-4 | 5 | §4.11; §4.2; §4.9 | The frozen recover conflicts with fault-isolation's edit; two gap taxonomies; disagreement over the exit-3 partition | CONFIRMED · major · PARTIAL | fault-isolation.md:523-527, line 29 (exit 3); §4.11 "never change the exit status" | MC7, MC40; OR2 |
| L5-5 | 5 | §4.11; P12 | Recapture's write-beside rule, v7 and T2/T3 name comparisons vs rename-over, v6 and a count; label shifts break `probeImproves` | CONFIRMED · major · PARTIAL | recapture-integrity.md 468-471, ~567; `exporter.go:571-590` | MC34, MC35, MC41; OR3 |
| L5-6 | 5 | §4.10 In place (repair only); §6/§7; MA-344 | Same mechanism as L3-2, plus rollback renames | CONFIRMED · major · DISSOLVED | `exporter.go:306`, `:485-489`; `paths.go:112-128` | MC8 |
| L5-7 | 5 | Header; §4.11; §7 | Workspace inheritance; GapKind parts leave as ordinary files; ParseGaps never reach the workspace manifest | CONFIRMED · major · DISSOLVED | extraction §4.12 writes "every model.Attachment … via WriteTo" | MC36 |
| L5-8 | 5 | OMISSION (workspace concurrency) | The repair burst makes a concurrent archive-source export report changed files and finish partial | CONFIRMED · minor | extraction §4.14 "does not rewrite existing files (R13)" | MC36 |
| L5-9 | 5 | §4.10 Accounting | `Exported` is reused for repairs | CONFIRMED · minor | `capture.go:146` | MC17 |
| L5-10 | 5 | §4.4; §4.10 old-zip gate | Three primitives are rebuilt (decodeBytes, headerAddress, the archive-file gate) | CONFIRMED · minor | `interchange/mbox.go:183-201` has no `@` test; `reader.go:517-525` | MC35 |
| L5-11 | 5 | §10 Q1 opt-in | The opt-in variant names only CLI flags; GUI and Desktop users could not ask for a repair | CONFIRMED · minor | `capture.go:43/141` fixed Options | MC37 |
| L5-12 | 5 | §4.10 notes "Since (slice)"; §10 | Slice labels vs scheme integers | REFUTED | same quote as L3-7: "registers its note codes with that `Since` value" | — |
| L5-13 | 5 | P1(b)/(c); MA-331 | go-pst's init registers ~45 labels into go-message's table; go-pst is not in (b) | CONFIRMED · minor | go-pst `pkg/charsets.go:70-72` | MC26 |
| L6-1 | 6 | §4.10 Precondition; P11 | Recovered items mask lost ones one for one (Apple Mail `notes.txt` vs a detached PDF); verify attests the loss | CONFIRMED · blocker · DISSOLVED | §4.10 "On a deterministic source (mbox/maildir, Graph) `new ≥ old` holds by construction" vs `graph.go:113`, `exporter.go:76-81` | MC3 |
| L6-2 | 6 | §4.10 ("a deliberate gap replaces v1's junk entry"); P13; P2 | Any GapKind counts as ≥ a v1 entry: a decoded PDF at depth 33 is replaced by an undecoded blob | CONFIRMED · major · DISSOLVED | §4.10 quoted; P2 "aspirational for all MIME in the wild" | MC4 |
| L6-3 | 6 | §4.10 predicate; §8 "no new collision surface" | Same key and fingerprint ≠ same message; a same-envelope B overwrites the archived A in place | CONFIRMED · major · DISSOLVED | `message.go:146-161` "differ only in body are treated as one (a resend)"; ContentHash written only with DedupMailboxWide (`exporter.go:430-445`) | MC5, MC6 |
| L6-4 | 6 | §4.10 "with -raw the identical bytes are rewritten" | A `-raw` repair overwrites the preserved `.eml` with today's source bytes, which may be detached (R20) | CONFIRMED · major · DISSOLVED | `mbox.go:269-276` X-Mozilla-*; `exporter.go:354-361` | MC11 |
| L6-5 | 6 | §4.7 tree embedding incl. `<style>` | An unvalidated MimeType inserted after `neutralize`; a `</style>` breakout forges the tool header that `reindex -rebuild` anchors on | CONFIRMED · major · DISSOLVED | `html.go:654-662`; `hostileSurvives` (`html.go:250-282`) checks only metas; `htmlheader.go:66-72` | MC23 |
| L6-6 | 6 | §4.2(3); §4.10 predicate | A rewrite loop every run via fallback plus cid (as L1-9) | CONFIRMED · minor | as L1-9 | MC19 |
| L6-7 | 6 | §4.10 stem fallback; §8; MA-346 | Falling back to stemFor makes `os.Remove` delete prev.Path's siblings with no containment check; a sibling record's stem passes the gate | CONFIRMED · major · DISSOLVED | `exporter.go:485-489`; stemFor's `KeyForPath` test (`exporter.go:541`) is absent from the gate | MC10 |
| L6-8 | 6 | §4.8 visited-NID set | NIDs are local to each subnode tree: false cycles and missed BID cycles; `ulSize` is unbounded | CONFIRMED · minor | go-pst `local_descriptors.go:131-139` | MC31 |
| L6-9 | 6 | P13 / §4.8 (≤ 256 embedded items) | No byte or entry budget; BID aliasing multiplies output through recursion | CONFIRMED · minor | `attach.go:65-70` buffers each entry whole | MC31 |
| L6-10 | 6 | §4.8 (method alone) | Method 5 with real PtypBinary bytes becomes a gap, and the repair counts it as replacing junk | CONFIRMED · minor | §4.8 "5 — never call go-pst `WriteTo`" | MC31, MC3 |
| L6-11 | 6 | §7 Inheritor; §6 README.txt | A sender-chosen `… (attached message).html` passes as tool-rendered; the segment cap can cut the tool's own suffix | CONFIRMED · minor | `paths.go:71-73` | MC33 |
| L6-12 | 6 | P12; §6 | Per-alternation re-repair (as L3-3) | CONFIRMED · major · DISSOLVED | `manifest.go:524/:1391`; `health.go:539` | MC12 |
| L6-13 | 6 | §4.3 step 1; tokenizer | The 1 MiB cap does not exist on the textproto path; part headers are uncapped; the RFC 2231 index is unbounded | CONFIRMED · minor | `textproto/header.go:516-520`; `textproto/multipart.go:87` | MC22 |
| L7-1 | 7 | §4.10; §5 W2 "new zip ⊇ old"; P11 | Equal counts ≠ superset: (a) cid un-consumption offsets a loss, (b) a Detach placeholder, (c) a truncated body passes | CONFIRMED · blocker · DISSOLVED | §5 W2's "⊇" does not follow from `new ≥ old`; the body check is presence only | MC3, MC5 |
| L7-2 | 7 | §5 blast radius; §6 Rollback | A logic defect that does not panic spreads to every noted record, with no prior copy and no revocation | CONFIRMED · major · PARTIAL | §5 "blast radius … one part, or at worst one message's content reverting to v1"; §6 "nothing to undo" | MC4, MC18, MC43 |
| L7-3 | 7 | §4.2(3); P13; P10 | Fallback is invisible (a log line and a Stats count); the record looks complete | CONFIRMED · minor | §4.2 "a `fidelity-fallback` log line + `Stats` counter" | MC19 |
| L7-4 | 7 | §4.2; §5 | The stub collision is hidden (as L1-8) | CONFIRMED · minor | `exporter.go:571-580` probeImproves | MC6 |
| L7-5 | 7 | §5; §4.7; §4.8 | New render-side code has no recover; a panic drops the whole outer message, logged with err=nil | CONFIRMED · major · DISSOLVED | `run.go:520-526`; `graph.go:631` | MC14 |
| L7-6 | 7 | §4.10; MA-345 | The wrong remedy for archive-side causes, with no end state (as L4-3) | CONFIRMED · major · DISSOLVED | a single `Stats.RepairWithheld` | MC15 |
| L7-7 | 7 | §5 W2 "converges"; "No step deletes" | Convergence depends on the source; the zip removal contradicts the table | CONFIRMED · minor | `attach.go:98-101`; `exporter.go:382` | MC13 |
| L7-8 | 7 | §4.10 stem-gate failure branch | The precondition reads stemFor's absent zip (old=0); prev's files are deleted or orphaned; `Fixity.EML` points at the wrong sibling | CONFIRMED · minor | `exporter.go:485-490` | MC10 |
| L8-1 | 8 | §4.10 In place; §6; MA-344 | As L3-2, with the R20 original lost on the first post-upgrade fill | CONFIRMED · major · DISSOLVED | `exporter.go:306`, `:308-309`, `:485-490` | MC8 |
| L8-2 | 8 | P12; §6; R2 clarification; §12 | Unbounded re-repair bursts (as L3-3) | CONFIRMED · major · DISSOLVED | `manifest.go:524/:1391` | MC12 |
| L8-3 | 8 | §4.11 "calls it in addition"; §6 Rollback "version 6" | Recapture's write-beside W1, T2/T3 name-based regressions and the manifest move to v7 | CONFIRMED · major · PARTIAL | recapture-integrity.md 424, 468-471, ~567, 931 | MC41; OR3 |
| L8-4 | 8 | §4.2; §4.11 | Fault-isolation edits frozen code; the rollout order is undefined; gap encodings and posture differ | CONFIRMED · major · PARTIAL | fault-isolation.md:523-527; §4.9 "Posture unchanged" vs terminal gaps feeding `partial` there | MC7, MC40; OR2 |
| L8-5 | 8 | §9 | id collision | CONFIRMED · minor | as L1-14 | MC42; OR5 |
| L8-6 | 8 | §6; §7 Admin; §4.10 Scheme; X6 | No progress or completion signal: scheme 0 means both "written before" and "not re-read since" | CONFIRMED · major · DISSOLVED | §4.10 "Every write stamps"; "W not yet re-examined" is the legacy Unknown count (`health.go:573`) | MC12, MC16 |
| L8-7 | 8 | P1(b) | The toolchain is not pinned; MA-330 sees corpus shapes only | CONFIRMED · minor | go.mod `go 1.26.0` with no toolchain; `ci.yml:26-33` floats deliberately | MC26; OR4 |
| L8-8 | 8 | §6 Rollback | An older binary cannot fill Missing labels written under fidelity names | CONFIRMED · minor | `exporter.go:583-586`; `attach.go:121-129` | MC34 |
| L8-9 | 8 | §6 Index; §7 | Index (fidelity) and page (v1) diverge for unrepaired records; the workspace copies v1 files | CONFIRMED · minor | §6 "`reindex -rebuild` from `.eml` now yields fidelity content" (unconditional) | MC16, MC36 |
| L9-1 | 9 | P1(b); §5 "go-message bumped → … blocks the change"; §12 | A CVE fix in a frozen dependency deadlocks govulncheck against MA-330, or drifts identity silently off-corpus | CONFIRMED · major (finder: blocker) · DISSOLVED | `release.yml:33-36`; no owner or procedure designed | MC27, MC28 |
| L9-2 | 9 | P1(b)/(c); H3; MA-331 | A wider identity surface: go-pst's nameMap (big5hk, iso_1, cp874, iso15 resolve only when linked), go-mbox framing, quotedprintable/base64, the floating toolchain | CONFIRMED · major · DISSOLVED | go-pst `charsets.go:71-73`; go-mbox `reader.go:55-70`; `ci.yml:32-33` | MC26 |
| L9-3 | 9 | §4.8 S6; §8 "no new dependency" | S6 leans on go-pst internals (v6.0.2, 2023) with no dead-dependency drill; branch-node errors; hangs escape recover | CONFIRMED · minor | `local_descriptors.go:83-85` ErrLocalDescriptorBranchNode; P9 already makes error returns gaps | MC31, MC32 |
| L9-4 | 9 | §9 render golden; MA-332 | A page-byte golden goes red on every unrelated renderer change, an operator ceremony forever | CONFIRMED · minor | the header's concurrent BodyFallback note | MC29 |
| L9-5 | 9 | §4.2 Cost; §12 | The cost is unlabelled and unbenchmarked; the memory claim ignores v1's attachment bytes | CONFIRMED · minor | `mbox.go:289`, `:508` | MC39 |
| L9-6 | 9 | §4.10; §7; §12; Q5 | "One-time" really means once per scheme; superseded digests are not recorded | CONFIRMED · minor | P11 "at most once per scheme" vs §12 "a one-time rewrite" | MC17, MC43 |
| L10-1 | 10 | P1; §12 | "Proven over the corpus" misapplies the doc's own labels; (c) is false today | CONFIRMED · major · DISSOLVED | P1 "(a)–(c) are **proven** over the fidelity corpus"; P2 uses "aspirational for all MIME in the wild" | MC26 |
| L10-2 | 10 | §9; every Covers cell | id collision; extraction's MA-425 xfail is already bound to its own R22 | CONFIRMED · minor | as L1-14 | MC42; OR5 |
| L10-3 | 10 | Header; §4.11; §7 Extraction-tool user | The inheritance over-claim (as L1-5) | CONFIRMED · major · DISSOLVED | as L1-5 | MC36 |
| L10-4 | 10 | §7 Home user | The Desktop "0 steps … sees repaired N" claim is false for with-Message-ID mail | CONFIRMED · major · DISSOLVED | `capture.go:43, :141` | MC37; OR1 |
| L10-5 | 10 | P9; §10 S6; §12; §1 F26 | S6 could ship with MA-343 L-pending and replace an honest gap; the F26 mechanism is stated as fact from a simulation | CONFIRMED · major · DISSOLVED | Q2 "S6 ships with that claim conditional"; §1 "No bundled fixture contains method 5" | MC32 |
| L10-6 | 10 | P10; §4.9; MA-334 | Terminal-always reasoning (as L1-7) | CONFIRMED · minor | as L1-7 | MC20 |
| L10-7 | 10 | §4.3 step 1; tokenizer; P13 | The stated header cap does not exist (as L6-13) | CONFIRMED · minor | as L6-13 | MC22 |
| L10-8 | 10 | P13; §4.2; §5 | "Never to a stub" over-claims when v1 also panicked | CONFIRMED · minor | `mbox.go:202-206`; §5 "Both → stub identity, fidelity content if it succeeded" | MC6 |
| L10-9 | 10 | §4.10 "holds by construction" | False on mbox (structure-limit fold, entry cap) and for Graph `$value`; the warning always blames offline copies | CONFIRMED · minor | P13's 10,000-leaf fold | MC3, MC15 |
| L10-10 | 10 | Header "adds no UI" | As L1-15 | CONFIRMED · minor | as L1-15 | MC39 |
| L10-11 | 10 | §4.2 provenance; §4.11 F33 hook | The release is never named; the F33 exception to "Never edit" is unstated | CONFIRMED · minor | §4.2 "as they stand in the last shipped release"; "none is known" | MC7 |
| L10-12 | 10 | P4 | As L1-10 | CONFIRMED · minor | go-mbox `reader.go:55-70` | MC38 |

## Mechanical check

`docs/design-mime-fidelity.md` revision 2 has BUILD STATUS "approved with conditions,
not built" and cites this review, which exists on disk. This repo has no meta-test that
scans `docs/design-*.md` for the build-status marker. The check was done by hand:
`grep -n "review-mime-fidelity-predesign" docs/design-mime-fidelity.md`. As each slice
lands, it flips its own status and cites its gate artifact: S1, S2, S4, S6 and S7
adversarial passes; S2, S3, S4 and S7 friction reviews (MC39, MC43).
