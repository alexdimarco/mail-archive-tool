# Adversarial pass — go-back live-dedup feature (2026-09-12)

**Scope:** the merged go-back feature (chain to 264088d) plus the v3→v4 collapse
wiring (e9f9022, now reverted at 94483ac). Five attack surfaces
(collapse-dataloss, envelope-dedup, serve-untrusted-history, redaction,
crash-durability), each finding adversarially verified against the real code.

**Result:** 13 confirmed, 0 refuted, 0 plausible-only. **The live-dedup identity
scheme (envelope signature + v3→v4 collapse) is not shippable as designed.** Its
core mechanism — dedup a message pre-download by an *envelope signature* and use
that same value as the "distinct-reuse" discriminator — is (a) incompatible with
every pre-existing archive's stored fingerprint, (b) self-inconsistent for inline
attachments, and (c) too weak to be a safe identity. This owes a **10-lens
pre-code design review + a re-run adversarial pass** before any rebuild.

Cited lines are against HEAD 9317b49 unless noted. e9f9022's wiring is reverted;
findings #1/#6/#9 describe that wiring for the record and for the eventual rebuild.

---

## Cluster A — the envelope-signature identity scheme (DESIGN; owes the gate)

The root problem. `EnvelopeSignature` (exporter.go:570) = sha256[:16] over
subject / lowered-sender / sorted-deduped-recipient-set / received-to-the-second
/ hasAttachments. It is used two ways that both fail:

- **#1 (CRITICAL) — upgrade fingerprint mismatch → whole-archive re-download +
  duplication + phantom "gone".** A real pre-go-back v3 Graph archive (c3e20b0)
  stored `fp = m.Fingerprint()` (MODEL fp: subject/sender/raw To+Cc/date-NANOS/
  attachment-NAMES) under a folder-scoped key. `CollapseByIdentity` keeps the
  survivor's stored fp verbatim (manifest.go:973) and `state` cannot recompute an
  envelope sig (it does not import `export`). The fast-path skips only when
  `sib.Fingerprint == candSig` (graph.go:413) where candSig is an envelope sig —
  a digest over disjoint inputs, so it never matches a model fp. Per message on
  first upgrade: re-download (R17), Qualify into a 2nd physical copy (R3,
  exporter.go:172), survivor never re-observed → SweepGone → Present=false =
  phantom deletion. **The e9f9022 test was vacuous** — its "v3" fixture was a v4
  record whose fp was already an envelope sig.
- **#2 (HIGH) — empty-fp legacy records re-download forever.** Records from the
  pre-fingerprint binary (a553452) have `fp=""`. `"" != candSig` always, so every
  message re-downloads every run; MergeFields (manifest.go:750) never backfills
  the fingerprint, so it never heals.
- **#7 (MEDIUM) — inline-only attachments re-download forever (fresh v4 too).**
  Graph listing `hasAttachments=false` for a message whose only attachment is an
  inline cid part; the parser puts that part in `m.Attachments` (mbox.go:308), so
  the post-download sig has `hasAttachments=true`. Pre- and post-download sigs
  disagree, the fast-path never matches, and the message re-downloads on every
  incremental run — this hits ordinary signature-image/newsletter mail on a
  brand-new archive, not just upgrades.
- **#8 (MEDIUM) — silent drop of a distinct message (R1).** The sig excludes
  body, attachment bytes, and attachment NAMES. Two genuinely different messages
  that reuse a Message-ID and share subject/sender/recipient-set/second/
  hasAttachments collide; the second is treated as a resend and RETURNS WITHOUT
  DOWNLOADING (graph.go:418). An insider can send benign A then sensitive B
  reusing A's Message-ID + envelope, and only A is archived.

**The tension the design must resolve:** a *pre-download* skip cannot see content,
so any pre-download identity is either too weak to be reuse-safe (#8) or
disagrees with the post-download fingerprint (#7) and with legacy fingerprints
(#1/#2). Candidate directions for the design review: (i) skip by (folder-or-mid)
membership pre-download and dedup by real content fingerprint post-download,
accepting a re-download on move; (ii) store a fingerprint *scheme* tag and, on a
LiveKey hit whose stored scheme differs, treat as SAME and re-derive rather than
Qualify; (iii) drop the envelope-sig fast-path for messages whose envelope
collides and fall back to download. All change the dedup identity → security-
sensitive, architecture-relevant.

## Cluster B — collapse wiring (reverted; rebuild must include)

- **#6 (MEDIUM) — collapse re-keys the manifest but not the search index.** The
  wiring only `DeleteByKey`'d loser rows; a single-folder survivor's docs row
  keeps its old folder-scoped key (RepairKeys/MigrateKey only handle v2→v3, never
  LiveKey), so a later `UpdateFolder(LiveKey)` matches zero rows and the browse
  facet never follows post-upgrade moves.
- **#9 (MEDIUM) — collapse loser's folder never written to the timeline.** Design
  §3.1 requires `hist.WriteFolder(survivor, loss.Folder)` so no location is lost;
  no such call exists. A message that moved Inbox→Archive in the v3 era shows
  nowhere at past dates after upgrade. (state/goback_test.go only asserts the loss
  is *returned*, never recorded — the gap was untested.)

## Cluster C — redaction completeness (owes the gate; overlaps friction B2)

- **#4 (HIGH) — a collapse LOSER copy survives redaction and is served.** The
  loser's files are left on disk (R13) with no manifest/index row; the on-disk
  intersection (goback.go:166) gates only the survivor's `rec.Path`, so the loser
  is neither hidden nor swept, yet `server.go:78` serves it at `/files/`. The
  operator, who only ever sees the survivor path, redacts the survivor and the
  loser copy remains readable.
- **#10 (MEDIUM) — orphan `.eml` never swept (= friction B2).** `SweepOrphans`
  (atomic.go:119) reclaims an html-less `-attachments.zip` but has no `.eml`
  branch, so a `-raw` original (headers+body+attachments, more sensitive than the
  `.html`) survives a partial redaction and is served in full. Changing this
  touches SweepOrphans's "nothing else is ever deleted" contract and brushes R13.

## Cluster D — history / crash robustness (mostly BOUNDED; fixable without a design gate)

- **#3 (HIGH) — stale run timestamp on an unparseable header falsifies the view.**
  `FoldEvents` (history.go:270) updates curAt/haveAt only when `time.Parse`
  succeeds; on failure there is no `else`, so the run's per-message events inherit
  the *previous* run's date. A header with a valid-JSON-but-bad date can show a
  gone message as present at `?at=D` (or hide a present one). RunDates already
  skips such a header (history.go:314) — FoldEvents should match. **Bounded fix.**
- **#5 (HIGH) — present-again in the same folder emits no event for a no-mid
  message.** The exporter skip path guards `recordMove` on `moved := prev.Folder
  != folderKey` only (exporter.go:200), omitting the `!prev.Present` twin the
  graph fast-path has (graph.go:432). A no-Message-ID message that goes present →
  gone → present-again in one folder gets no assertion, so the fold reports it
  Present=false forever after. **Bounded fix** (add the `|| !prev.Present` twin).
- **#11 (MEDIUM) — reindex makes the compacted log durable before the manifest.**
  Order is index-flush → history-rename → manifest.Save (reindex.go:132/145/158);
  a crash between the rename and Save leaves the manifest asserting a message the
  log no longer explains, and the next reindex can't re-prune it (its index row is
  already gone) → permanent orphan. The run path forbids exactly this inversion
  (§3.5). **Bounded fix** (save the manifest before compacting the log, or make
  compaction re-derivable).
- **#12 (LOW) — unbounded history read, parsed up to 3× per request.**
  `ReadHistory` has only a per-line 16 MB cap; `serveGoback` reads the file
  independently at goback.go:74/112/147. Loopback-only, so low severity. **Bounded
  fix** (read once, reuse the slice; cap total events).
- **#13 (LOW) — a failed history append is logged but the manifest mutation is
  not rolled back.** A targeted write/fsync error on the log lets `commit()` save a
  manifest recording a move/gone with no log line — the manifest leads the log.
  **Bounded fix / accept-and-document.**

---

## Recommendation

1. **Cluster A is a design decision (owes the 10-lens gate).** The live-dedup
   identity must be redesigned so it is reuse-safe (no #8 drop), self-consistent
   pre/post-download (no #7), and migrates model-fp/empty-fp archives without
   re-download-storms, duplication, or phantom deletions (no #1/#2). Until then,
   the go-back live-dedup path is not shippable, and the v3→v4 upgrade is unsolved.
2. **Clusters B and C fold into that same design** (the collapse rebuild must
   re-key the index and record loser folders; redaction must reach loser copies
   and orphan `.eml`s — the latter also owes a gate on the R13/SweepOrphans
   contract).
3. **Cluster D is bounded** — #3, #5, #11, #12, #13 can be fixed independently
   with prove-fail tests now, if the operator wants the robustness fixes decoupled
   from the design revision.

The friction review (docs, same day) is a separate gate; its ship-now doc-honesty
fixes are committed (9317b49). Its two durable items (redaction re-capture
tombstone; orphan `.eml`) coincide with #1's tension and #10 respectively.
