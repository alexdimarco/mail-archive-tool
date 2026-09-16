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

**rev-6.2 correction (the arbiter must not depend on `-raw`; operator-authorized
2026-09-16 after the closure adversarial pass found a default-mode R1 drop).** The
rev-6.1 `.eml` byte-compare is INERT by default: `-raw` (preserved `.eml`) is
opt-in, so a default archive has nothing to compare, and the H4 *backfill-from-
listing* shortcut then bound a listed id onto a lone record with NO content check
and skipped — permanently dropping a distinct reuse (R1). rev-6.2 makes a
**body-inclusive content hash** (`model.Message.ContentDigest` = `contentHash`,
recorded as `Record.ContentHash` at capture) the churn-vs-distinct arbiter — it
needs no `.eml`, and excludes transport headers so a migration that rewrites
`Received`/etc. is still recognized as the same message. The `backfill-from-listing`
shortcut is **removed**: when a listed id matches no sibling the exporter DOWNLOADS
and arbitrates by content hash. The closure engages only where every same-token
sibling carries a content hash (a v6-managed identity); a pre-v6 / floor record
(none) stays the shipped Message-ID floor — no re-download storm, no unsafe split,
no drop beyond the documented floor. Fresh v6 archives close #8 for everyone;
existing archives close it for messages captured fresh under v6. The sibling scan is
also **token-scoped** (R6) so it never adopts, backfills, or deletes across store
boundaries.

**rev-6.2 id-set (2026-09-16, after the re-verification pass).** A record holds a
SET of content-equal immutable ids (`PhysID` primary + `AltPhysIDs`), not one slot.
A content-hash match on a NEW id ADDS it to the set (`AddPhysID`) instead of flipping
a single stored id; the fast-path and exporter skip a listing entry whose id is ANY
member (`HasPhysID`). This handles a message COPIED into several folders — same
Message-ID, identical content, distinct Graph ids — which otherwise re-downloaded and
flapped the stored id every run; and it subsumes a genuine migration reissue
uniformly. The separate `phys-churn` anomaly is REMOVED: a second content-equal id is
absorbed silently (it is neither a distinct message nor an error).

## 0. One-line shape
`Record.PhysID` = the Graph immutable id, a **capability-gated hint** for
Message-ID (`mid:`) identities only. The immutable id is the **distinctness
signal** (a distinct physical message has a distinct id — EC6/Q3) and the recorded
**body-inclusive content hash is the churn-vs-distinct arbiter** (rev-6.2 — no
`.eml`/`-raw` needed). PhysID is never folded into identity or fingerprint.
Withheld/absent PhysID, or an identity without a comparable content hash (a pre-v6 /
floor record) ⇒ the shipped Message-ID-membership floor (no regression, no wrong
drop, no re-download storm).

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
  set it; `AddPhysID` (rev-6.2, replacing `BackfillPhys`) records content-equal ids under the same lock, so
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

## 4. Exporter placement — the content hash is the arbiter (HC3, HC7, HC1; rev-6.2)
Live path, `mid:` identity, this message carries an immutable id. `placeByPhysID`
scans the identity's **same-token** siblings (`KeysForIdentity` filtered to this
store — R6) and returns `(key, ok)`:
1. **A sibling already stores THIS immutable id** → that sibling: the same physical
   message re-observed → adopt/skip. `ok=true`.
2. **Every same-token sibling carries a content hash** (a v6-managed identity): a
   sibling whose stored `ContentHash` EQUALS this message's `ContentDigest` is the
   SAME message under a new immutable id (a copy filed elsewhere, or a reissue) →
   **`AddPhysID`** the id to that record's content-equal set and adopt (no split, no
   anomaly, so a later run skips every copy — rev-6.2); no content match → a
   genuinely **DISTINCT** reuse → **split** under a key qualified by a HASH OF THE
   IMMUTABLE ID (§3, HC1 — the fingerprints COLLIDE on an identical envelope, so an
   fp-qualified key would overwrite; an R1 drop). `ok=true`, both survive (**#8
   closed**). The content hash (`ContentDigest`) is at least as discriminating as
   `Fingerprint` — it folds Cc/Bcc and attachment name+size as well as the bodies —
   so the arbiter never adopts two messages the fingerprint would split.
3. **Any same-token sibling LACKS a content hash** (a pre-v6 / floor record), or the
   incoming message has none → `ok=FALSE`: the closure cannot tell a distinct reuse
   from the archived message without a comparable hash, so it defers to the shipped
   Message-ID floor — no unsafe split, no drop, no re-download storm to backfill
   legacy records. Such an archive closes #8 only for messages captured fresh under
   v6.
`R17 restated (rev-6.2):` a NEW content-equal id (a copy, or a reissue) is
downloaded ONCE — the run that first sees it — then added to the record's id-set, so
later runs skip it by set membership (no per-run re-download, no id flap). A capture
is a distinct new record only when the content differs (correctly split). The only
NEW downloads the closure adds are genuine distinct reuses and first-observations of
a new content-equal id.

## 5. Fast-path skip decision (HC6, HC4, HC12; rev-6.2)
The fast-path decides only SKIP-vs-DOWNLOAD; all placement (adopt/churn/split) is
the exporter's `placeByPhysID` (§4). Incremental, `mid:` identity, same-token
siblings present:
1. **listed physID == ""** (withheld) → floor: skip by membership; fire the id-reuse
   note if the identity already carries >1 distinct fingerprint (HC12).
2. **physID matches a same-token sibling's stored PhysID** → SKIP (no download);
   **matchKey = THAT sibling** (HC6); `MergeFields(present)`; stamp ONLY that sibling
   (each physical message has its own listing entry, so a departed distinct reuse is
   gone-detected — no EC5 fan-out on an id match).
3. **physID matches NO sibling, and EVERY same-token sibling carries a content
   hash** → DOWNLOAD and hand to the exporter, which arbitrates by content hash
   (§4): a content-hash match ⇒ adopt + AddPhysID (add the id to the record's set, no anomaly); no match ⇒ #fp-split
   by the §3 key (**#8 closed**).
4. **physID matches NO sibling, but ANY sibling LACKS a content hash** (a pre-v6 /
   floor record) → FLOOR membership skip: never bind a listed id to a record chosen
   by anything but content (HC4), and never re-download to backfill legacy records
   (no storm). Fan out `TouchSeen` over all same-token siblings (a floor skip cannot
   say which one this entry is — R21). The closure engages for this identity only
   once its records are captured fresh under v6.

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
  `IdentRef.PhysID` + byIdent wiring + `AddPhysID`/`HasPhysID` id-set (HC10, rev-6.2).
- **H3** — exporter three-way adopt/re-id/split with the PhysID-hash split key
  (HC1, HC3, HC7); `phys-churn` anomaly; scoped to `mid:` (HC8).
- **H4** — graph fast-path skip-by-matched-sibling + download-and-compare +
  id-reuse-note-preserved (HC6, HC4, HC12).
- **H5** — collapse survivor-PhysID backfill + still-live-loser re-separation (HC2);
  docs/README/goback.md + catalog rewrites (HC13, HC14); the re-run adversarial pass.

## 9. Operator-accepted residuals (carried; rev-6.2)
IMAP/local #8 (floor); a withheld id (degrade to floor); an adversarial tenant
assigning ONE immutable id to two distinct reused-id messages (skip-match; the
id-reuse note still fires — HC12); a pre-v6 collapse-dropped loser gone before it is
re-captured (= floor merge-drop). **rev-6.2:** an EXISTING (pre-v6 / floor) archive
keeps the Message-ID floor for its already-captured messages — the closure does not
re-download to backfill immutable ids or content hashes, so #8 is closed only for
messages captured fresh under v6 (a distinct reuse of a pre-v6 record's Message-ID
is the same bounded floor residual as before, never a NEW drop). A reissued or
copied id is added to the record's content-equal id-set (one record, no duplicate,
skipped on later runs); it becomes a distinct capture only if the CONTENT also
changed — then it is genuinely a different message and the split is correct. The
content hash excludes transport headers (so a header-only migration is still the same
message) and folds Cc/Bcc + attachment name/size (so an attachment- or Cc-only
distinct reuse is split, not dropped — adversarial re-check 2026-09-16). A message COPIED into several
folders is kept as ONE record and (with the id-set) not re-downloaded, but its single
"current folder" and the go-back timeline can flap between those folders each run — a
PRE-EXISTING mailbox-wide-dedup property (present on the floor), cosmetic (no
loss/duplicate/re-fetch); a per-copy folder model is deferred.
