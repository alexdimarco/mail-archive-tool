# Re-run gate — go-back live-dedup rev 4 (option D) (2026-09-12)

**Verdict: GO_WITH_CONDITIONS.** 6 lenses → verify (26 confirmed, 11 refuted) →
rescue. Full agent record: task w4ynk7z7e.

## The headline finding (why this is conditions, not rejection)

**Option D's FLOOR is a complete, correct design on its own:** Message-ID
membership skip + post-download content-fingerprint #fp-split + token-scoped
collapse. Option D genuinely shrank rev-3's risk surface — EnvSig/#7 dissolved, no
listing widening, migration heals by LiveKey membership with no fingerprint
dependency. **All four remaining R1/R3-class blockers, and the one unverified
external dependency, are concentrated in a single layer: the "#8 closed on Graph"
closure via the ImmutableId/PhysID hint (build slice G2).** The floor slices (G1
scheme-tag-for-retry, G3 collapse, G4 redaction, G5) may proceed under conditions;
**G2 is NO-GO until EC1/EC2/EC3/EC6 land.**

## Hidden blocker — the missing cross-layer signal

The split-vs-adopt decision is made in the EXPORTER (`Export(store, folderPath,
m)` — sees only fp/scheme; a distinct message B reusing A's Message-ID yields an
identical `m.Identity()`, so it recomputes the SAME base LiveKey), while the
same-vs-distinct verdict is made in the app/graph pre-download layer (which alone
holds the PhysID/ImmutableId). Rev-4 never threads that verdict into `Export()`,
yet every Graph #8-closure claim requires it. Without this channel §3
(adopt-never-split) and §5 (distinct → split) cannot be reconciled, and a faithful
literal build silently DROPS (R1) the original migrated message on the normal
migrated Graph state — or re-duplicates (#1). No precedence text alone fixes it;
the design must specify the pre-download→exporter data path AND capture-time,
mailbox-wide PhysID storage, or "#8 closed on Graph" is unbuildable and must
downgrade to the logged floor.

## Build conditions

**G2 (#8-on-Graph) — NO-GO until these land (all blocker/high):**
- **EC1** Specify the cross-layer channel: thread the §5 "PhysID-distinct" verdict
  into `Export()` as an explicit input, AND store PhysID (ImmutableId) on the
  Record at capture on the fresh export path, mailbox-wide.
- **EC2** Write §3/§5 precedence: adopt-never-split applies ONLY to fillable-retry
  and first-observation downloads; a §5-proven-distinct PhysID forces the #fp-split
  REGARDLESS of the matched sibling's FpScheme, under a v5-qualified key that does
  not merge into the legacy base. Define disposition for 0 or 2+ legacy siblings
  (forbid blind adopt; download-compare / log-as-distinct). Restate §1/§8/§10 so
  #8-on-Graph explicitly depends on EC1.
- **EC3** Replace §5 bullet-2 "skip + backfill assume-same" with
  download-and-compare whenever a listed non-matching PhysID meets an empty-PhysID
  sibling (single OR multiple): the recomputed content fp decides (same → adopt;
  different → split, B survives). Emit the mandatory id-reuse Issue at THIS site.
  On a content-fp match against a CHANGED non-empty PhysID (restore/re-id), ADOPT
  the new PhysID rather than storm. Re-scope the §5.3 gate to fire on hint-LOSS.
- **EC6** Correct the substrate: the immutable id is NOT `$select`-able — it comes
  only via `Prefer: IdType="ImmutableId"` on EVERY per-mailbox request (listing AND
  the `/$value` MIME fetch), which makes `ref.ID` itself the immutable id
  (PhysID==ref.ID). Prove the MIME fetch still resolves under the header. **Confirm
  app-only Mail.Read actually returns immutable ids before relying on the closure;
  if not, downgrade "#8 closed on Graph" to the logged floor.**
- **EC8** Name the seams: an in-place `Manifest.BackfillPhys(key, physID)` (OUT of
  MergeFields, empty-only / never-overwrite + mismatch log); an `id-reuse` Issue
  kind + a sink reachable from the pre-download skip site; `IdentRef` extended with
  PhysID in `buildIdentIndexLocked` so the skip compares without a per-sibling Get.
- **EC9** Re-scope the id-reuse Issue to fire ONLY on genuine ambiguity (>1
  distinct #fp sibling) and hint-loss (EC3), never on bare PhysID-absence (else
  O(mailbox) Issues/run, capped at 10000, drowning real completeness findings);
  dedupe per-identity across runs. Resolve the §2-vs-§8/§10 IMAP-PhysID
  contradiction.

**Floor slices — GO under these:**
- **EC4 (blocker, G3)** Fix collapse: discriminate a genuine v3 folder-scoped key
  by testing whether `parts[1]` is a `mid:`/`sha:` identity (a qualified LiveKey's
  2nd component IS the identity) — NEVER by NUL/component count; keep qualified
  LiveKeys verbatim. Gate "owes collapse" on per-token 3-component-key presence
  scoped to this run's token(s) (per-token marker, not a manifest-wide scalar);
  reindex/verify never collapse. Prove-fail: (a) a #fp-qualified LiveKey survives a
  re-run unchanged; (b) a multi-mailbox v3 archive collapsing mailbox B in a later
  run writes no third copy. (rev-4 dropped the `LoadedVersion<4` gate that enforced
  "pure v3 input", so the trigger would corrupt organic #fp LiveKeys — MA-203 — a
  regression vs today.)
- **EC5 (high, G3)** Fold loser `DeleteByKey` INTO the same
  `wal_checkpoint(TRUNCATE)`+fsync'd `idx.Rekey` transaction (survivor-rekey +
  loser-delete atomic, durable before the manifest advances). Advance
  LastSeen/Present on every same-identity legacy sibling so none is left un-stamped
  and swept phantom-gone. Close the index-leads-manifest window (immediate
  post-Rekey `manifest.Save`, or a verify/status warning on LiveKey-index /
  3-component-manifest divergence).
- **EC7 (high, G4)** Redaction must delete the loser html+zip+eml TRIPLE together
  (carry the loser STEM, not just `.html`), ordering-independent. Prove-fail must
  assert 404 on `.html`, `.eml` AND `-attachments.zip` after reindex, prove-failing
  by reverting the SIBLING deletion, positive twin served-before on a real v3 -raw
  attachment-bearing move-duplicate fixture. (§6/CollapseLoss.LoserPath is the
  `.html` only; the more-sensitive `.eml`/`.zip` would stay served — reopening
  #4/#10.)
- **EC10 (medium, all)** Catalog leads code: add a real loser-folder audit FIELD +
  test OR delete the "audit-only note" language from §4/R21 and state plainly the
  pre-upgrade location is not retained (consistent with §7). Enumerate the new MA
  rows (adopt/scheme; PhysID skip/backfill/split + mismatch anomaly; id-reuse
  Issue; AlsoFiles 404 triple; reindex-only .eml sweep; checkpoint-on-processed;
  idx.Rekey) and REWRITE MA-202/203/204 + S38 to option-D semantics; bump MA-201
  ">4"→">5". (MA-206 and S39 do NOT encode EnvSig — exclude.)

## Residual risks
- #8 on IMAP/local (no reliable PhysID): distinct-reuse skipped; bounded + logged
  once EC3/EC9 land. Full closure abandons R17. Operator-accepted.
- Graph ImmutableId is MS-controlled: genuine withdrawal degrades the Graph path to
  the floor (correctness preserved); a CHANGED id must heal (EC3), not storm.
- **CONTINGENT (EC6): if app-only Mail.Read does not reliably return immutable ids,
  "#8 closed on Graph" (operator decision #2) is not deliverable and must downgrade
  to the logged floor — the operator must re-accept the #8 residual on Graph.**
- Legacy PhysID first-observation window: even with EC3, the very first observation
  of a lone legacy sibling assumes same-message before content is seen — a one-run
  miss, acceptable only once genuinely logged (§10 corrected).
- Collapse crash-window (EC5): an interrupted collapse must be reopened by a graph
  run before reindex/serve are fully trusted; documented graph-only-healable.

## Read
The floor (dedup + migration + collapse + redaction + timeline) is correct and
buildable now under EC4/EC5/EC7/EC10. The "#8 closed on Graph" closure (G2) is a
distinct, higher-risk layer that needs a rev-5 design-delta (EC1/EC2/EC3/EC8/EC9)
AND a live-tenant confirmation that immutable ids are returned app-only (EC6) — if
that check fails, the closure is undeliverable and #8 on Graph becomes the logged
floor. This is the natural seam to decide: ship the correct floor now and treat
the ImmutableId closure as a separate gated enhancement, or fold the full G2 into a
rev-5 before building.
