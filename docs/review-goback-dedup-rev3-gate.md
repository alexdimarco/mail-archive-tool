# 10-lens gate — go-back live-dedup rev 3 (2026-09-12)

**Verdict: GO_WITH_CONDITIONS.** 10 lenses → skeptic (44 confirmed, 18 duplicate)
→ rescue. The direction (split rev-2's one overloaded envelope signature into a
listing-derived pre-download skip key + a post-download content fingerprint as the
R1 guardian; collapse-then-walk; honest download-once migration) is sound and
worth building — but rev 3 is NOT yet buildable: two load-bearing premises are
provably false against the code, five blocker seams have no named mechanism, and a
materially simpler alternative (Graph ImmutableId) was not evaluated. Build only
after the conditions are met AND a re-run adversarial pass clears the rebuilt
identity scheme. Full agent record: task wa1s2zgs0.

## HIDDEN BLOCKER (DC1)
§2/§4's "the content fingerprint is what every legacy v3 archive already stores,
so it is directly comparable across the upgrade — no scheme mismatch" is FALSE.
The go-back work itself (a8f241d, app/graph.go `m.Received = ref.Received`) changed
the fingerprint's date term from the MIME Date-header instant (what v3 stored) to
Graph's `receivedDateTime`; `model.Fingerprint()` folds `Date().UnixNano()`. So a
v3 record's fp differs from what rev3 recomputes for essentially all received
mail → every migrated message fails the content-fp match, #fp-splits into a
duplicate (R3), and its un-re-observed survivor is swept phantom-gone (R21) — the
precise #1 the revision exists to kill, silently un-fixed. Nothing downstream
matters if migration re-drops/re-duplicates the whole archive.

## Conditions (DC1–DC15) — the build contract

- **DC1 (blocker) — migration must not rely on fp equality.** Add an explicit
  fingerprint SCHEME tag on the record. On the first upgraded run treat a
  Message-ID (LiveKey/folder-scoped) hit as the SAME message regardless of stored
  fp value; re-derive/update in place, backfill, and NEVER #fp-split against a
  legacy-scheme / empty / v4-envelope-sig / v1-fillable record. Delete the
  "directly comparable" claim.
- **DC2 (blocker) — name the backfill mutator.** MergeFields writes only
  Folder/LastSeen/Present. Add e.g. `Manifest.BackfillIdentity(key, envSig, fp,
  scheme)`, called on the exporter skip branch and the fillable Add path. Prove:
  run1 over a legacy fixture backfills; run2 does zero fetches.
- **DC3 (blocker) — one producer→record channel for EnvSig.** The graph layer
  computes it once from `MessageRef` and carries it into the record on EVERY
  export path (incl. full mode and the download fall-through) via the Export input
  or OnExported. The exporter must NOT recompute a listing signature from the
  parsed message (bodyPreview isn't reproducible from `m` → #7 reincarnated). Add
  EnvSig to IdentRef (or Get the sibling in the fast-path). Pin bodyPreview's
  exact hash input byte-for-byte; write EnvSig onto the key the exporter returns.
- **DC4 (blocker) — gate collapse on a real signal, not the version int.** A
  reindex/verify Load→Save or any checkpoint Save stamps `manifestVersion`, so a
  version gate is defeated and collapse silently never runs (re-triggers #1). Use
  a durable collapse-done marker (or the presence of 3-component keys); make
  collapse re-derivable/idempotent across a mid-walk crash; scope it to the
  token(s) archived this run (else it re-keys other mailboxes → R3).
- **DC5 (blocker) — `idx.Rekey` full old→new remap for EVERY kept record**, not
  just losers (a no-move v3 survivor's key still changes to LiveKey and yields no
  CollapseLoss today), as ONE batched, `wal_checkpoint(TRUNCATE)`+fsync'd index
  transaction durable BEFORE the manifest advances / marker is set. Define
  drop-the-occupant collision semantics (reuse RepairKeys). CollapseByIdentity
  returns the complete remap.
- **DC6 (blocker/strategic) — evaluate Graph ImmutableId first.** `Prefer:
  IdType="ImmutableId"` (or singleValueExtendedProperties) as the steady-state
  live identity BEFORE committing to the EnvSig machinery: stable across moves
  (clean R17, no preview drift), per PHYSICAL message (distinct id for a reused
  Message-ID → closes #8 at the root with zero content inspection), no steady-state
  backfill. If rejected, document why (it cannot retrofit the v3 migration — DC1
  still applies — and is Graph-specific). It dissolves the pre/post tension EnvSig
  contorts around and would retire much of DC2/DC3/DC7/DC8.
- **DC7 — close §8 decisions together with the scheme tag.** State plainly that NO
  listing-only signature binds a deliberate insider — every EnvSig input but
  received-to-the-second is sender-authored — so a kept skip's security reduces to
  the received-second barrier (not a dichotomy bodyPreview closes). A body-hash, if
  adopted, MUST NOT apply to legacy-scheme fps and MUST NOT turn an edited-preview
  in-place update into a #fp-split. Name the single non-adversarial case EnvSig
  must catch and pick the cheapest mechanism for exactly that.
- **DC8 — if the EnvSig skip is kept:** (a) skip only on EXACTLY-ONE non-empty
  EnvSig sibling match (else download-compare); (b) backfill EnvSig ONLY when
  empty, never overwrite a non-empty one, and log 'content-fp match with differing
  non-empty EnvSig' as an anomaly; (c) force download-compare when listing scalars
  are sparse (empty bodyPreview AND (empty from OR recipients)); (d) MANDATORY
  reuse-logging emitted at the skip DECISION site as a new 'id-reuse' Issue kind;
  (e) hash bodyPreview (served-page injection); (f) name attachments and
  body-past-preview inside the skip blind spot in the threat model.
- **DC9 — redaction must DELETE collapse-loser copies (#4).** Carry `AlsoFiles`
  on the DedupMailboxWide write path and MergeFields; CollapseByIdentity populates
  survivor.AlsoFiles with loser paths. reindex DELETES every AlsoFiles path when
  the survivor's own Path is redacted/off-disk (a loser has no index row for the
  row-walk to reach; `/files/` is a bare FileServer). Prove-fail: GET
  `/files/<loserPath>` → 404 after reindex. Update the R13/reindex contract text.
- **DC10 — bump manifest format to v5** and update MA-201 ('refuses > 4') in the
  same change, before the test. Namespace/version the stored EnvSig so a later
  ListingSignature scheme change is a documented one-time re-download.
  (encoding/json drops unknown fields → an older equal-version binary silently
  strips EnvSig/AlsoFiles on a shared/synced -out, reopening #4 and the re-download
  storm.)
- **DC11 — checkpoint on messages PROCESSED** (downloads + skips + moves), not
  `exp.Stats.Exported` (a migration follows the skip path, so Exported stays ~0 and
  checkpoint never fires → an interrupted large-mailbox migration never
  converges). Each checkpoint durably persists the backfilled EnvSig/fp/LastSeen.
  Reword §4/§7: "exactly once across clean runs; a crash re-downloads at most
  messages since the last checkpoint."
- **DC12 — restate #9 honestly (R21).** HistoryEvent has no per-event timestamp;
  FoldEvents is last-writer-wins within a run header's curAt, so the collapse's
  `WriteFolder(survivor, loserFolder)` and the walk's current-folder assertion
  under the SAME run header make the current folder win — the loser folder never
  surfaces at a past date. Either give loser assertions orderable past timestamps
  or restate #9 as audit-only and drop any at-date loser-folder test.
- **DC13 — genuine THREE-STATE migration test before code:** (1) a REAL version-3
  fixture (Version 3, folder-scoped keys, real files, fp over the pre-adoption
  MIME-Date instant) → LoadedVersion==3, run1 = exactly one fetch/msg + zero dup +
  zero {k,gone} + EnvSig backfilled, run2 = zero fetches; (2) a fp="" fixture; (3)
  a v4 envelope-sig fixture. NO v4-record-masquerading-as-v3 (the e9f9022 vacuous
  trap). Plus an 'id-reuse' Issue-kind test and a real bodyPreview-drift recovery
  test. Update MA/R catalog rows first.
- **DC14 — reindex-ONLY .eml sweep**, separate from SweepOrphans (which also runs
  on normal runs from run.go and would delete a legitimately-orphaned .eml from a
  crash between the .eml and .html writes — brushes R13). Justify the zip-vs-eml
  asymmetry.
- **DC15 — quantify the first-run download storm** (GB/hours for ~500k vs Graph
  throttling; get() retries 5× with no global pacing), state resumability +
  operator guidance, record the rejected cheaper alternative, and EVALUATE Graph
  `$delta` as the scale-correct alternative to re-listing every run (adding
  bodyPreview widens every listing row ~⅓ forever for marginal #8 gain; confirm
  `$top=1000` survives the heavier $select).

## Residual risks the operator must accept (if EnvSig skip kept)
- #8: a deliberate insider reusing a Message-ID and matching subject/from/
  recipients/received-second/sent-second/first-255-body can hide a payload in an
  attachment or past char 255 → B silently skipped (R1 drop). Only non-authored
  barrier is received-to-the-second. ImmutableId (DC6) removes this entirely.
- bodyPreview is not guaranteed byte-stable; a server-side format change
  invalidates every EnvSig at once → whole-mailbox one-time re-download
  (self-healing, but looks like a regression).
- A crash before the next checkpoint loses backfills → bounded re-download.
- serve /goback O(n) manifest reload per request; AlsoFiles adds a stat/loser.

## Direction
The gate strongly points at **Graph ImmutableId for steady-state identity + a
scheme-tagged download-once migration (DC1) for the legacy upgrade** as the
materially simpler, safer design — it closes #7 and #8 at the root and retires the
EnvSig apparatus (DC2/DC3/DC7/DC8) for steady state. Rev 4 should evaluate and,
if adopted, pivot to it; the migration, collapse (DC4/DC5), redaction reach
(DC9/DC14), honesty (DC12), format (DC10), resumability (DC11), and test plan
(DC13) conditions apply either way.
