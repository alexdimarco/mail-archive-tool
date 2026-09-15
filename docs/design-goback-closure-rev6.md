# Design — go-back #8 closure via Graph immutable ids (rev 6, buildable)

**Revision:** 6.1 (2026-09-14). **Status:** buildable — folds the 10-lens gate's
HC1–HC14 (`docs/review-goback-closure-rev5-gate.md`, in the workflow record) onto
rev-5, then CORRECTS a §3/§4 contradiction found while building H3 (operator-
authorized). Ships as **v0.5.0**. Supersedes rev-5 §3/§4/§5. The PhysID model
(rev-5 §2) and the EC6-confirmed Graph capability stand.

**rev-6.1 correction (the §3/§4 contradiction).** rev-6 §3 and §4 prescribed
OPPOSITE actions for the SAME observation — *same Message-ID, same envelope
fingerprint, DIFFERENT immutable id*: §3 SPLIT it ("the closure's raison d'être"),
§4 ADOPTED it as a churn. The envelope `Fingerprint` deliberately EXCLUDES the body
(so a fill is not mistaken for a new message), so `prev.Fingerprint == fp` CANNOT
tell a genuinely-distinct identical-envelope reuse (two invoices, same headers,
`$100` vs `$250`) from a churn (one message whose id was reissued by a migration).
Adopting on a fingerprint match therefore DROPS a distinct message — an R1 loss that
`MA-235`/`R1` already forbid on the local path. rev-6.1 makes the **message bytes**
(the archived `.eml`, via the existing `sameArchivedEML` primitive) the
churn-vs-distinct arbiter: identical bytes ⇒ churn (adopt + note); differing bytes,
or an envelope-fp mismatch, ⇒ distinct (split by the PhysID-hash key of §3). This
unifies §3/§4/§5 under one rule and NEVER drops a distinct message. When the bytes
cannot be compared (no incoming `Raw`, or no archived `.eml`), the closure does not
assert sameness: it defers to the shipped floor or splits (a bounded duplicate is a
safe error; a dropped message is not).

## 0. One-line shape
`Record.PhysID` = the Graph immutable id, a **capability-gated hint** for
Message-ID (`mid:`) identities only. The immutable id is the **distinctness
signal** (a distinct physical message has a distinct id — EC6/Q3) and the **message
bytes are the churn-vs-distinct arbiter** (rev-6.1). PhysID is a pre-download skip
hint and a distinctness tie-breaker, never folded into identity or fingerprint.
Withheld/absent PhysID, or bytes that cannot be compared ⇒ the shipped
Message-ID-membership floor (no regression, no wrong drop).

## 1. Identity inertness + plumbing (HC8, HC9, HC10, HC5)
- **HC9:** `model.Message.PhysID` and `Record.PhysID` are non-identity capture
  fields (like Importance/Categories). NEVER folded into `Identity()`,
  `Fingerprint()`, or `contentHash()`. A covers test asserts two messages
  differing only in PhysID hash identically under all three.
- **HC8:** the entire PhysID skip/adopt/split/backfill decision applies to `mid:`
  identities ONLY. A `sha:` (no-Message-ID) identity already folds the body into
  `contentHash`, so the floor keeps one copy of byte-identical no-mid items —
  PhysID must not touch that path.
- **HC5:** `Export` writes `rec.PhysID = m.PhysID` on the fresh-record path
  (mailbox-wide), and carries `prev.PhysID` forward on an adopt. `applyGraphState`
  stamps `m.PhysID` from the listing.
- **HC10:** `IdentRef` gains `PhysID`; `identUpsertLocked`/`buildIdentIndexLocked`
  set it; `BackfillPhys`/the update path refresh `byIdent` under the same lock, so
  the fast-path compares PhysID without a per-sibling `Get`.
- **Format v6 (HC13):** `manifestVersion` 5→6; `Record.PhysID` additive/omitempty;
  MA-201 flips to "refuses above 6 (version 7)" with a v7 fixture in the SAME change.

## 2. Graph client (HC11)
- Send `Prefer: IdType="ImmutableId"` on BOTH the message listing AND the `/$value`
  fetch; when honored `ref.ID` IS the immutable id (`PhysID := ref.ID`).
- Decide the namespace once per run from the listing's `Preference-Applied`.
- A `/$value` that does NOT honor the header is a **non-fatal per-message failure**
  that degrades THAT message to the floor (empty PhysID) — never abort the mailbox
  walk. `WellKnownFolderID`/listing paths unchanged otherwise.

## 3. The #fp-split KEY is PhysID-derived (HC1 — the hidden blocker)
The split MUST have an fp-independent key. When two same-identity messages are
distinct by PhysID but their content fingerprints are IDENTICAL (identical-envelope
reuse — the closure's raison d'être), `Qualify(key, fp)` collides and the write
overwrites (R1 drop). So the exporter, when it splits a PhysID-distinct message,
qualifies by **`util.HashHex(physID, 8)`** (mirroring `CollapseByIdentity`'s
existing fp-collision fallback), and keeps the existing "slot taken → qualify
again" loop so a hash collision still never becomes a silent skip.

## 4. Exporter placement — bytes are the churn-vs-distinct arbiter (HC3, HC7, HC1; rev-6.1)
Live path, `mid:` identity, this message carries an immutable id AND its `Raw` bytes
are present. `placeByPhysID` scans the identity's siblings (`KeysForIdentity`) and
chooses the key:
1. **A sibling already stores THIS immutable id** → that sibling: the same physical
   message re-observed → adopt/skip. (No byte compare needed.)
2. **No id match, but a same-fingerprint sibling's archived `.eml` is
   BYTE-IDENTICAL** to this message → adopt THAT sibling and **backfill its
   PhysID**. If the sibling carried a DIFFERENT non-empty id, its id was REISSUED (a
   restore / cross-tenant migration): emit a `phys-churn` anomaly (HC7); if the
   sibling's id was EMPTY (a pre-v6 / withheld record), this is the ordinary first
   PhysID-walk backfill (§5) — no anomaly. Never split.
3. **No id match and byte-differs from every same-fingerprint sibling** → a
   genuinely DISTINCT reuse → **split** under a key qualified by a HASH OF THE
   IMMUTABLE ID (§3, HC1 — the fingerprints COLLIDE on an identical envelope, so an
   fp-qualified key would overwrite; an R1 drop). Both survive (**#8 closed**).
- **Cannot byte-compare** (a same-fp sibling has no archived `.eml`) → do NOT assert
  distinctness on nothing: fall back rather than risk an R1 drop — the incoming
  message splits (keep both), a bounded duplicate.
- **No incoming `Raw`, PhysID empty, non-`mid:`, or non-live** → the shipped floor
  (adopt-never-split / content-fp split), unchanged.
`R17 restated honestly (HC7):` a message whose immutable id changes between runs is
re-downloaded that run and adopted in place by the byte compare (bounded, logged);
duplicated ONLY if its stored MIME is not byte-identical across the reissue (§9).
The only NEW downloads the closure adds are genuine distinct reuses and these re-id
churns.

## 5. Fast-path skip decision (HC6, HC4, HC12; rev-6.1)
The fast-path decides only SKIP-vs-DOWNLOAD before it has bytes; all placement
(adopt/churn/split) is the exporter's `placeByPhysID` (§4), which owns the key and
does the byte compare AFTER download. Incremental, `mid:` identity, same-token
siblings present:
1. **listed physID == ""** (withheld) → floor: skip by membership; fire the
   id-reuse note if the identity already carries >1 distinct fingerprint (HC12).
2. **physID matches a sibling's stored PhysID** → SKIP (no download); **matchKey =
   THAT sibling** (HC6, chosen by PhysID, not the base); `MergeFields(present)` on
   it; `TouchSeen` the other same-token siblings (EC5). If the matched sibling is
   one of >1 distinct-fp siblings, still fire the id-reuse note (HC12).
3. **physID matches NO sibling** → DOWNLOAD and hand to the exporter, which
   byte-compares against the siblings (§4): a byte-identical sibling ⇒ adopt +
   backfill / churn-note (this subsumes the old §5.4 empty-sibling *and* the all-v6
   churn cases — one arbiter, the bytes); no byte match ⇒ #fp-split by the §3 key
   (**#8 closed**). Fire the id-reuse Issue when the identity carries >1 distinct fp.
   NEVER bind a listed id to an on-disk file chosen by anything but content (HC4).

## 6. Collapse (HC2 — reject defer-to-walk; keep the floor merge)
- Keep the SHIPPED collapse merging every same-(token,identity) group to one
  LiveKey survivor; do NOT invert `sameArchivedEML`'s no-`.eml`→merge fallback; do
  NOT leave members at v3 folder-scoped keys.
- On the first PhysID-bearing walk the survivor's PhysID is backfilled (§5.4). A
  still-live loser that is genuinely distinct is then re-separated by the
  distinct-PhysID split (§4/§5.3) against the survivor's byIdent-visible LiveKey —
  no new mechanism, no walk-merge needed.
- **HC14 scope:** #5 is closed for archives whose collapse runs under v6, and for
  the still-live-loser case above. A pre-v6 archive already `MarkCollapsed` that
  merged-and-dropped a loser whose message is GONE from the mailbox cannot be
  re-separated (no live counterpart) — that equals the floor's already-accepted
  merge-drop and stays documented, not claimed closed.

## 7. Catalog / covers in lockstep (HC13) + honest docs (HC14)
- Bump v6 + MA-201 ceiling in the same change; **REMOVE the `t.Skip` on
  `TestGraphDistinctIDMailboxWideSplit`** — the gap-encoder becomes a permanent
  guard asserting the reuse IS now downloaded + split on Graph. Rewrite
  MA-203/S38/S39 from "reuse SKIPPED" to "closed on Graph via PhysID". Add covering
  rows: PhysID skip-by-match, download-and-compare backfill, distinct-PhysID split
  (PhysID-hash key), three-way re-id adopt + phys-churn, withheld-PhysID degrade,
  mid:-only scope, PhysID inertness.
- `goback.md`/README: the reused-Message-ID residuals are **closed on Graph** for
  archives captured/migrated under v6 (with the honest caveats: #1 self-heals from
  the 2nd run once a PhysID baseline exists; a persistently-changing id
  re-downloads; a withheld id degrades to the floor; IMAP/local remain the floor
  residual).

## 8. Build slices (each prove-fail; H1 first, catalog-first per slice)
- **H1** — v6 format + `Record.PhysID`/`model.Message.PhysID`/`MessageRef.PhysID`
  (inert: HC9 test); Graph client `Prefer: IdType` on listing + `/$value`,
  namespace-once, per-message degrade (HC11) + header-aware fake server; MA-201→v7.
- **H2** — `Export` capture-store `rec.PhysID` + carry-on-adopt (HC5);
  `IdentRef.PhysID` + byIdent wiring + `BackfillPhys` (HC10).
- **H3** — exporter three-way adopt/re-id/split with the PhysID-hash split key
  (HC1, HC3, HC7); `phys-churn` anomaly; scoped to `mid:` (HC8).
- **H4** — graph fast-path skip-by-matched-sibling + download-and-compare +
  id-reuse-note-preserved (HC6, HC4, HC12).
- **H5** — collapse survivor-PhysID backfill + still-live-loser re-separation (HC2);
  docs/README/goback.md + catalog rewrites (HC13, HC14); the re-run adversarial pass.

## 9. Operator-accepted residuals (carried; rev-6.1)
IMAP/local #8 (floor); a withheld id (degrade to floor); an adversarial tenant
assigning ONE immutable id to two distinct reused-id messages (skip-match; the
id-reuse note still fires — HC12); a pre-v6 collapse-dropped loser gone before the
first PhysID run (= floor merge-drop). **New with the byte-compare arbiter
(rev-6.1):** a churn (reissued id) whose stored MIME is NOT byte-identical across
the reissue is re-captured as a bounded, logged DUPLICATE rather than adopted — the
`.eml` compare cannot recognize it (a stronger, body-only arbiter is possible later
if a real migration exhibits this); and a same-fingerprint reuse whose sibling has
no archived `.eml` (KeepRaw was off) splits to a bounded duplicate rather than risk
an R1 drop. Both are DUPLICATES (safe), never drops.
