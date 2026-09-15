# Design — go-back #8 closure via Graph immutable ids (rev 6, buildable)

**Revision:** 6 (2026-09-14). **Status:** buildable — folds the 10-lens gate's
HC1–HC14 (`docs/review-goback-closure-rev5-gate.md`, in the workflow record) onto
rev-5. GO_WITH_CONDITIONS → this is the build contract. Ships as **v0.5.0**.
Supersedes rev-5 §3/§4/§5. The PhysID model (rev-5 §2) and the EC6-confirmed Graph
capability stand.

## 0. One-line shape
`Record.PhysID` = the Graph immutable id, a **capability-gated hint** for
Message-ID (`mid:`) identities only. The **content fingerprint stays the sole
adopt-vs-split arbiter**; PhysID is a pre-download skip hint and a distinctness
**tie-breaker**, never an override. Withheld/absent PhysID ⇒ the shipped
Message-ID-membership floor (no regression).

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

## 4. Exporter adopt-vs-split — three-way, fp is the arbiter (HC3, HC7, HC1)
On a same-`mid:`-identity match where BOTH PhysIDs are non-empty:
- **PhysID equal** → same message → adopt/skip.
- **PhysID differs, content fp EQUAL** → the SAME message re-identified (Microsoft
  restore / cross-tenant migration reissues immutable ids in bulk) → **ADOPT and
  UPDATE the stored PhysID** (HC7), emit a `phys-churn` anomaly to the report, do
  NOT split, do NOT storm.
- **PhysID differs, content fp DIFFERENT** → genuinely distinct → **#fp-split**,
  keyed by PhysID-hash (§3), both survive.
- **Either PhysID empty** → the floor rules (adopt-never-split / content-fp split),
  unchanged.
`R17 restated honestly (HC7):` a message whose immutable id persistently CHANGES
between runs is re-downloaded each run (bounded, logged, never duplicated) — not
move-stable. The only NEW downloads the closure adds are genuine distinct reuses
and these re-id churns.

## 5. Fast-path skip / backfill / split (HC6, HC4, HC12)
Incremental, `mid:` identity, same-token siblings present:
1. **listed physID == ""** (withheld) → floor: skip by membership; fire the
   id-reuse note if the identity already carries >1 distinct fingerprint (HC12).
2. **physID matches a sibling's stored PhysID** → skip; **matchKey = THAT sibling**
   (HC6, chosen by PhysID, not the base); `MergeFields(present)` on it; `TouchSeen`
   the other same-token siblings (EC5). If the matched sibling is one of >1
   distinct-fp siblings, still fire the id-reuse note (HC12).
3. **physID matches no sibling, every sibling has a non-empty PhysID** → DISTINCT →
   download + #fp-split (§3 key) → captured (**#8 closed**).
4. **physID matches no sibling, ≥1 sibling has an EMPTY PhysID** (legacy /
   first-observation, incl. a collapsed group) → **DOWNLOAD-AND-COMPARE (HC4, EC3)**:
   recompute the content fp; fp equals an empty-PhysID sibling → adopt + backfill
   that sibling's PhysID; else → #fp-split (§3 key). Emit the id-reuse Issue at THIS
   site. NEVER bind a listed id to an on-disk file chosen by anything but content.

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

## 9. Operator-accepted residuals (carried)
IMAP/local #8 (floor); a persistently-changing immutable id (re-download/run,
logged, never duplicated); a withheld id (degrade to floor); an adversarial tenant
assigning ONE immutable id to two distinct reused-id messages (skip-match; the
id-reuse note still fires — HC12); a pre-v6 collapse-dropped loser gone before the
first PhysID run (= floor merge-drop).
