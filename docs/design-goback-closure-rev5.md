# Design — go-back #8 closure via Graph immutable ids (rev 5)

**Revision:** 5 (2026-09-14). **Status:** for the 10-lens pre-code design review,
then build. Builds the per-source **PhysID** hint the option-D floor
(`docs/design-goback-dedup-rev4.md` §2/§5) left as a capability, now that the
live-tenant check (gate EC6, `docs/review-goback-dedup-rev4-gate.md`) PASSED:
Graph immutable ids are returned under app-only `Mail.Read`, stable across a move,
**distinct per physical message even when the Message-ID is reused**, and
`/$value`-fetchable. This closes the floor's documented reused-Message-ID
residuals (`docs/goback.md` Limits) at the root. It ships as **v0.5.0**.

## 1. What the floor left open (and this closes)

The floor deduplicates the live path by **Message-ID membership** — correct and
vendor-neutral, but it cannot tell a *genuinely different* message that reuses an
archived Message-ID from the same message, so three bounded residuals remain (all
file-preserving, all reused-id-only): a live run **skips** a distinct reuse (#8);
a `-mode full` re-export can **overwrite** a gone copy's search record (#1); the
v3→v5 collapse can **merge** two identical-envelope copies in a non-`-raw` archive
(#5). Each needs a *content-comparable, per-physical-message* identity the floor
lacks. Graph's immutable id is exactly that.

## 2. The PhysID model (a capability-gated hint, never a dependency)

- **`Record.PhysID` (new, additive, `omitempty`)** — an opaque per-source
  physical-message discriminator. On Graph it is the **immutable id**; empty for
  local imports and for any record captured before this version. It is a HINT for
  the pre-download skip/split decision — **correctness never depends on it**: if a
  provider stops returning it, the path degrades to the Message-ID-membership
  floor (the residuals return, nothing breaks). This is the option-D principle,
  now instantiated for Graph (the operator's portability/vendor-robustness
  requirement).
- **Graph client** — request immutable ids with `Prefer: IdType="ImmutableId"` on
  BOTH the message listing AND the `/$value` MIME fetch (EC6: the header makes
  `ref.ID` itself the immutable id, and the fetch resolves under it). `PhysID :=
  ref.ID`. Confirm `Preference-Applied` per response; if a request is not honored,
  treat PhysID as empty for that message (degrade to floor), never guess.

## 3. Closing #8 — the pre-download skip/split (graph.go fast-path)

On an incremental listing whose `LiveKey` identity has same-token sibling(s)
(the floor's membership hit), consult PhysID:

1. Listed `physID == ""` (Graph withheld it) → **floor behaviour**: skip by
   membership (the bounded #8 residual, logged as today).
2. Listed `physID` equals a sibling's stored PhysID → **skip** (same physical
   message); record move/present-again; TouchSeen every same-token sibling (EC5).
3. Listed `physID` matches no sibling, and exactly one sibling has an EMPTY PhysID
   (legacy / first observation) → **skip + backfill** that PhysID onto it (no
   download); establishes the baseline so a later divergence is caught.
4. Listed `physID` matches no sibling and every sibling has a non-empty PhysID →
   a genuinely **DISTINCT physical message** → **download and #fp-split**, so it
   is CAPTURED as its own sibling (both survive — **#8 closed**, R1). This is the
   only new download the closure adds, and only for a real distinct reuse.

## 4. Closing #1 and the adopt-vs-split knot — the cross-layer signal (EC1)

The floor's adopt-never-split guard is blind to same-vs-distinct against a legacy
record, which is why a `-mode full` reuse could overwrite a gone original (#1).
Fix with the cross-layer channel EC1 demanded:

- **`applyGraphState` stamps the listed PhysID onto the `model.Message`** (a new
  `model.Message.PhysID` field, MIME-independent, set by the source), so the
  exporter sees the physical identity of the message it is about to write.
- **The exporter's split/adopt decision consults PhysID first:** when the
  incoming message's PhysID and a same-identity record's stored PhysID are BOTH
  non-empty and DIFFER → the incoming is a distinct physical message → **#fp-split**
  (never adopt-overwrite), regardless of fingerprint scheme. When they match →
  same message → adopt. When either PhysID is empty → fall back to the floor's
  content-fingerprint / adopt-never-split rules (unchanged). This resolves #1 in
  both full and incremental modes, and removes the need for the round-3 "never
  delete a legacy file" guard to bear the whole R1 weight (it stays as
  defence-in-depth).

## 5. The v3→v5 collapse (#5) — download-to-disambiguate, do not guess

The one-time collapse migrates PRE-PhysID v3 records, so it has no PhysID to
compare and its file-content check (the `.eml`) is only present under `-raw`. Rev-5
does NOT try to resolve an ambiguous same-fingerprint group at collapse time.
Instead:

- Collapse merges only the **unambiguous** move-duplicates (byte-identical `.eml`,
  or a single member per fingerprint) as today.
- A same-(token,identity) fingerprint group that is **ambiguous** (>1 member,
  files not confirmably identical) is **left un-merged** — the members keep their
  distinct records — and the subsequent walk resolves them by PhysID (§3/§4):
  each is observed with its own immutable id, so they are recognised as distinct
  and both retained. This turns #5 from a silent collapse-time merge into a
  walk-time correct split. (A departed ambiguous member with no live counterpart
  stays as its own record — no drop.)

## 6. Format, backfill, invariants

- **Format v6** (additive `PhysID` on Record + `model.Message.PhysID`): bump so an
  older v5 binary that would silently drop PhysID on a shared `-out` is refused
  (version-gated Load); update MA-201's ceiling. Namespace nothing else.
- **Backfill mutator (EC8):** `Manifest.BackfillPhys(key, physID)` — set only when
  empty, never overwrite a non-empty PhysID; a content-verified change (restore /
  cross-tenant re-id) is logged, not stormed on. IdentRef carries PhysID so the
  fast-path compares without a per-sibling Get.
- **id-reuse (EC9) superseded on Graph:** the distinct reuse is now CAPTURED (§3.4),
  so the "id-reuse skipped" note fires only on the floor path (§3.1, PhysID
  absent). Keep it there.
- **Invariants:** R1 — #8 distinct reuse now archived, not skipped; R17 — PhysID
  is move-stable (Q2), so no new re-download on a move; the only added downloads
  are genuine distinct reuses; R3 — one copy per physical message; R13/R21 —
  unchanged; vendor-robustness — PhysID optional, degrades to the floor.

## 7. Build slices (proposed) + tests

**H1** Graph client: `Prefer: IdType="ImmutableId"` on listing + `/$value`;
`MessageRef.PhysID`/`model.Message.PhysID`; `Record.PhysID`; format v6 + MA-201
ceiling. **H2** the fast-path skip/backfill/split on PhysID (§3) + `BackfillPhys` +
IdentRef.PhysID + id-reuse re-scope. **H3** the exporter cross-layer split/adopt on
PhysID (§4). **H4** the collapse ambiguous-group defer-to-walk (§5). **H5**
docs/README/goback.md (residuals now closed on Graph) + catalog + `verify` note.

Tests (each prove-fail): a distinct reuse with a distinct immutable id is
downloaded + #fp-split, both survive (#8 closed); a moved message keeps its PhysID
→ no re-download (R17); a full-mode reuse over a gone original splits, original
intact (#1 closed); a v3 archive with two same-envelope distinct copies collapses
to un-merged then the walk splits by PhysID (#5 closed); a withheld PhysID
degrades to the floor (skip + log); PhysID backfill never overwrites. Fixtures use
a fake Graph server that serves an immutable id per message (distinct for a reused
Message-ID) — the kit's Q3 shape.

## 8. Open decisions for the gate

1. **Format v6 vs additive-on-v5** — a shared/synced `-out` written by a mixed
   fleet: is the version bump + refusal the right call, or is PhysID safe to drop
   silently (it re-backfills)? Recommendation: bump.
2. **The cross-layer signal shape** (§4) — stamp PhysID on `model.Message` (chosen)
   vs a separate Export parameter. Confirm no layering violation (model gains a
   source-set field it does not interpret).
3. **§5 collapse defer** — is leaving an ambiguous group un-merged for the walk
   acceptable (the archive briefly holds the un-collapsed folder-scoped copies
   until the walk re-keys them), or should collapse download-to-disambiguate?
4. **PhysID for a gone message** never re-observed — it keeps its last PhysID; a
   distinct reuse of its id is caught (different PhysID). Confirm no edge where a
   gone record's PhysID causes a false skip.
