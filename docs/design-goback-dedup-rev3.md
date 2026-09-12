# Design — go-back live-dedup, rev 3 (post-adversarial revision)

**Revision:** 3 (2026-09-12). **Status:** GATED — GO_WITH_CONDITIONS (15 conditions,
see `docs/review-goback-dedup-rev3-gate.md`). NOT buildable as written: the gate
found this doc's §2/§4 migration-comparability premise PROVABLY FALSE (DC1 — the
go-back work changed the fingerprint's date term, so a legacy fp is not comparable)
and flagged a materially simpler alternative — Graph ImmutableId — that this doc did
not evaluate (DC6). A rev 4 must incorporate the conditions (and likely pivot the
steady-state identity to ImmutableId) and re-run the adversarial pass before build.
Revises `docs/design-goback.md` §3.1/§3.2 (the live-dedup identity, the v3→v4
collapse, the move fast-path) and folds the 13 confirmed findings in
`docs/review-goback-adversarial.md`. It is architecture- and security-relevant
(it changes the dedup *identity*, touches R1/R3/R17/R21/R13), so it owes this
gate and a re-run adversarial pass BEFORE any build. Rev 2's serve projection,
history log, gone/present-again, and trash policy are unchanged and not restated.

## 1. Root cause (why rev 2's dedup is not shippable)

Rev 2 made ONE value — an *envelope signature* (subject/sender/recipient-set/
received-second/hasAttachments, sha256[:16]) — do two incompatible jobs, and
computed it from two different sources:

- as the **pre-download skip key** (the fast-path recomputes it from the Graph
  *listing* and skips a message whose sibling's stored fingerprint equals it), and
- as the **distinct-reuse discriminator** (the exporter #fp-splits a message whose
  fingerprint differs from an already-archived same-identity sibling).

The stored value was computed POST-download from the parsed message
(`len(m.Attachments)>0`); the fast-path's candidate was computed PRE-download from
the listing (`ref.HasAttachments`). Consequences (adversarial findings):

- **#1/#2** A real pre-go-back archive stores the CONTENT fingerprint
  (`m.Fingerprint()` — over body-defining fields + attachment NAMES) or nothing.
  Neither equals an envelope signature, so on the first upgraded run every message
  re-downloads (R17), the exporter #fp-splits each into a DUPLICATE (R3), and the
  un-re-observed originals are swept to a phantom "gone" (R21).
- **#7** For inline-only attachments the listing says `hasAttachments=false` and
  the parser says true, so the stored envelope sig (post) never equals the
  candidate (pre): ordinary signature/newsletter mail re-downloads every run.
- **#8** The envelope sig omits body/attachment content, so two DISTINCT messages
  that reuse one Message-ID and share the envelope collapse to one — a silent drop
  (R1).

The fix is to **split the two jobs into two stored values, each computed from the
right source**, and to make the pre-download value a function of the LISTING
alone so it is consistent across runs.

## 2. The revised identity model

Each live-path record carries TWO independent values:

- **`Fingerprint` (content).** `m.Fingerprint()` as today — post-download, over
  the body-defining envelope + attachment names (a body-hash term is added below).
  It is the **distinct-reuse discriminator**: the exporter #fp-splits only when a
  downloaded message's content fingerprint differs from an already-archived
  same-identity sibling's. It is what every legacy v3 archive already stores, so
  it is directly comparable across the upgrade (no scheme mismatch). This is the
  R1 guardian and it is only ever compared POST-download.
- **`EnvSig` (NEW field, listing-derived).** A signature over the GRAPH LISTING
  scalars only — subject, from, recipient set, receivedDateTime (to the second),
  sentDateTime, and a hash of `bodyPreview` (the ~255-char preview the listing
  already can return). Computed by the graph layer from the `MessageRef` at
  archive time and stored on the record; recomputed by the fast-path from the same
  listing fields on a re-run. Because BOTH sides read the listing, they are
  apples-to-apples — **#7 cannot occur**. `hasAttachments` is DROPPED from the
  signature (it was the #7 culprit and adds little); `bodyPreview` replaces it as
  a far stronger discriminator. EnvSig is ONLY a pre-download skip heuristic; it is
  never the R1 guardian.

`$select` gains `bodyPreview,sentDateTime` (both standard Graph message fields);
`MessageRef` gains `BodyPreview` and `Sent`. `EnvelopeSignature` is replaced by
`ListingSignature(subject, from, recipients, received, sent, bodyPreview)`.

## 3. Pre-download skip (R17), fixed and bounded (#7, #8)

On an incremental listing of a message whose `LiveKey(token, "mid:"+id)` is
already in the manifest:

1. Compute `candEnv = ListingSignature(ref…)`.
2. Look up the same-identity siblings. For each sibling with a NON-EMPTY stored
   `EnvSig`: if `candEnv == EnvSig` → **skip the download** (R17); stamp LastSeen,
   record a move/present-again if the folder changed or it was gone (§3.2 rev 2).
3. If no sibling matches (candEnv differs, or the sibling has no EnvSig yet —
   legacy or a genuinely changed listing) → **download**. Post-download, the
   exporter compares the CONTENT fingerprint against the siblings:
   - content fp equals a sibling → it is the SAME message (a false EnvSig
     mismatch, e.g. an edited preview, or a legacy record with no EnvSig): update
     that record in place (folder/LastSeen/Present), do NOT create a copy, and
     **backfill its `EnvSig`** from this listing so the next run skips it.
   - content fp differs from every sibling → a genuinely DISTINCT reuse: #fp-split
     into a new sibling keyed by the content fp (both survive — R1).

**#8 threat model (explicit decision for the gate).** A distinct message can be
skipped only if it reuses an archived Message-ID AND matches subject, from,
recipient set, received-to-second, sent-to-second, AND the first ~255 chars of the
body. For genuine mail this is effectively impossible; it is reachable only by an
insider deliberately crafting a near-twin. The design ACCEPTS this bounded
residual, and mitigates it two ways: (a) the strengthened listing signature makes
accidental collision astronomically unlikely; (b) OPTIONAL follow-up — when a
download DOES occur and the content fp reveals a distinct reuse, log a
"Message-ID reused with differing content" line to the attachments/issues report
so an auditor sees id reuse. Full R1 safety (never skip an already-archived id)
would forbid the pre-download skip entirely and abandon R17; the gate/operator
must choose. **Recommendation: keep the skip with the strengthened signature and
the reuse-logging mitigation; document the residual in the README/goback.md.**

## 4. Legacy migration (#1/#2): download-once, update-in-place, never duplicate

The first run of the rev-3 binary over a v2/v3/empty-fp archive:

- **Collapse first (§5).** `CollapseByIdentity` unifies per-folder move-duplicates
  into one identity record BEFORE the walk (else the walk creates a third copy).
- **No EnvSig yet.** Collapsed/legacy records have a content `Fingerprint` (or "")
  but no `EnvSig`, so step 3.2 cannot skip — the message is DOWNLOADED once.
- **Update in place, backfill EnvSig.** Post-download, the content fp matches the
  record (same message) → update in place (§3.3), no duplicate, no phantom-gone
  (LastSeen advances because the message was observed), and `EnvSig` is stored.
- **Steady state.** Every subsequent run skips via the stored `EnvSig` (R17).

So the honest guarantee is: **the first upgraded run re-downloads each message
exactly once (unavoidable — no comparable pre-download value existed in the old
format), creates no duplicate, emits no phantom deletion, and thereafter never
re-downloads.** (Rev 2 / e9f9022 falsely claimed zero re-download; that is what
the vacuous test hid.) An empty content fingerprint (pre-fingerprint binary) is
backfilled on this same download, so #2's "re-download forever" is closed.

The exporter change that makes this safe: on a mailbox-wide LiveKey hit where the
stored `Fingerprint` is EMPTY, treat the download as the SAME message and backfill
(never #fp-split an empty-fp record); where it is non-empty, #fp-split only on a
genuine content-fp difference (unchanged R1 behaviour).

## 5. Collapse, fixed (#6, #9)

`CollapseByIdentity` stays (it is the only way to unify v3-era move-duplicates),
but the caller (RunGraph) must, per loss:

- **#6 — re-key the SURVIVOR's index row.** The survivor's manifest key changes
  from `token\x00folder\x00mid` to `LiveKey(token,mid)`; the search index must
  follow (an `idx.Rekey(old,new)` for the survivor, plus `DeleteByKey` for each
  loser). Today only losers are pruned, so a survivor's browse facet keeps a stale
  folder-scoped key and later moves never update it. RepairKeys/MigrateKey handle
  only v2→v3 and must not be relied on for LiveKey.
- **#9 — record each loser's folder on the timeline.** Emit
  `hist.WriteFolder(survivorKey, loss.Folder)` per loss so a message that moved
  Inbox→Archive in the v3 era still folds to its then-folder at past dates.
  (Bounded honesty: the collapse can only assert the folders it can see in the two
  records; the pre-upgrade *dates* are unknown and are stamped at the migration
  run, stated as a limit.)

## 6. Redaction must reach every copy (#4, #10)

- **#4 — collapse loser FILES.** Collapse leaves the loser's files on disk (R13)
  but drops its manifest row, and serve's on-disk intersection checks only the
  survivor path, so a "redacted" survivor can leave a loser copy served at
  `/files/`. Fix: the survivor record records the loser file paths
  (`AlsoFiles []string`), so (a) serve's redaction intersection hides the message
  only when ALL its files are gone, and (b) `reindex` and the documented redaction
  recipe delete every path. The operator is given all paths (status/verify list
  them).
- **#10 — orphan `.eml`.** `reindex` (redaction ONLY, never a normal run) sweeps an
  html-less `.eml` symmetrically with the html-less `-attachments.zip`, so a
  `-raw` original cannot survive a partial redaction. A normal run still never
  deletes a message file (R13 unchanged for capture; redaction is the sole
  deletion path, as documented).

## 7. Invariants

- **R1 (never silently drop):** the CONTENT fingerprint (post-download) is the
  sole distinct-reuse discriminator; the pre-download skip's residual is bounded,
  documented, and mitigated (§3).
- **R3 (one copy per message, mailbox-wide live path):** unchanged; collapse +
  in-place update keep one physical copy; distinct reuse #fp-splits.
- **R17 (cheap re-runs, no re-download of archived bodies):** held in steady state
  via the listing-derived EnvSig; honestly relaxed to exactly-one re-download per
  message on the first upgraded run.
- **R13 (append-only; normal runs never delete):** unchanged; loser files are kept
  and only ever removed by an operator redaction (now reaching every copy).
- **R21 (point-in-time truth):** collapse now records loser folders (#9); cluster-D
  fixes (fold bad-header, present-again, reindex ordering) already landed.

## 8. Open decisions for the gate

1. **#8 residual (§3):** accept the bounded pre-download-skip drop risk with the
   strengthened signature + reuse logging (keeps R17), or forbid skipping an
   already-archived id (full R1, abandons R17)? Recommendation: the former.
2. **Body-hash in the content Fingerprint:** add a body digest so a same-envelope
   distinct message is split even post-download (strengthens R1 beyond attachment
   names). Cost: a fingerprint scheme bump; legacy content fps stay comparable only
   for the skip-vs-split decision if we treat a legacy fp as "unknown scheme →
   download-and-compare-raw." Decision needed.
3. **`AlsoFiles` on the record (#4):** store loser paths on the survivor vs. a
   separate redaction index. Recommendation: `AlsoFiles` (local, additive).
4. **EnvSig storage:** a new manifest field bumps the format; confirm v5 and the
   version-gated load path.
