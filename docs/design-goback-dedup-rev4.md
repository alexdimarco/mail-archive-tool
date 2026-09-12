# Design — go-back live-dedup, rev 4 (option D: portable identity)

**Revision:** 4 (2026-09-12). **Status:** for a re-run 10-lens/adversarial gate,
then build. Supersedes rev 3's EnvSig apparatus. Operator decisions (2026-09-12):
**(1) identity = option D** — a portable Message-ID + post-download content
fingerprint floor, with per-source pre-download discriminators as OPTIONAL,
capability-gated hints (never a dependency); **(2) #8 posture** — the distinct
Message-ID-reuse drop is closed on Graph (via the ImmutableId hint) and a bounded,
logged residual on IMAP/local. Folds gate conditions DC1–DC15
(`docs/review-goback-dedup-rev3-gate.md`). Rev-2 serve/history/gone/trash are
unchanged. See `docs/note-goback-identity-options.md` for the option analysis.

## 1. Why D dissolves rev-3's worst problems

The pre-download skip is by **Message-ID membership**, not by any signature, so:

- **#7 is structurally impossible** — there is no pre/post envelope comparison to
  disagree.
- **The migration download-storm and DC1's fp-incomparability trap dissolve.**
  After collapse (§4) an upgraded archive's message sits at its `LiveKey`; the walk
  lists it, finds `LiveKey` present, and **skips — no download, no re-computed
  fingerprint, no duplicate, no phantom-gone.** The legacy fingerprint's
  incomparability never triggers a split because the skip does not consult the
  fingerprint. The scheme tag (§3) matters only on the rarer DOWNLOAD paths.
- The EnvSig machinery (rev-3 DC2/DC3/DC7/DC8) is retired; a per-source hint (§5)
  replaces it and degrades to the floor if the hint is absent.

## 2. Identity model (D)

- **Dedup key (unchanged, portable):** `LiveKey(token, m.Identity())` on the
  live/repeat path, where `m.Identity()` is `mid:<Message-ID>` or, absent one,
  `sha:<content-hash>` — already source-agnostic. One physical copy per identity
  per mailbox (R3); a one-shot local import keeps the folder-scoped key (§3.6).
- **Content fingerprint (the R1 guardian):** `m.Fingerprint()` post-download, now
  carrying a **scheme tag** (§3). It is the SOLE distinct-reuse discriminator, and
  it is compared only POST-download, in the exporter #fp-split.
- **Per-source physical discriminator (`PhysID`, optional):** an opaque per-source
  string a source capability can produce from a LISTING entry AND store on the
  record, identifying the physical message independent of its Message-ID:
  - Graph → `ImmutableId` (`Prefer: IdType="ImmutableId"`), stable across moves,
    distinct per physical message.
  - Live IMAP (later) → a hash of `RFC822.SIZE` + `BODYSTRUCTURE` (both cheap in a
    `FETCH`), a strong non-move-stable discriminator (a move keeps size/structure).
  - Local one-shot import → none (full content in hand; dedup by content fp).
  `PhysID` is a HINT for the pre-download skip only; correctness never depends on
  it, so a provider change degrades gracefully to the Message-ID floor.

## 3. Fingerprint scheme tag + migration (DC1)

`Record` gains `FpScheme int` (0 = legacy/unknown: any v1–v4 or empty fp, whose
value is NOT comparable to a current fp — the go-back work changed the fp's date
term, a8f241d). The current scheme is `fpSchemeV5`.

Migration is now trivial under D because the skip does not use the fp: an upgraded
message is recognised by `LiveKey` membership and skipped. The scheme tag governs
only the DOWNLOAD paths (a fillable retry, or a distinct-reuse forced by §5):

- On a download whose `LiveKey`/identity matches a sibling with `FpScheme==0`
  (legacy) → treat as the **SAME message**: update in place, recompute and store
  the current-scheme fp, set `FpScheme=fpSchemeV5`, and **NEVER #fp-split against a
  legacy-scheme sibling**. (Adopt, don't split.)
- When both are current-scheme → #fp-split only on a genuine current fp
  difference (unchanged R1 behaviour).

No "download-once per message" storm: only messages that are downloaded anyway
(fillable retries, or §5-forced distinct-reuse checks) recompute the fp.

## 4. Collapse (DC4, DC5, DC9) — unify v3 move-duplicates before the walk

Still required: a v3 archive holds a moved message as two folder-scoped records;
without collapse the walk's `LiveKey` probe misses both and writes a third copy.

- **Trigger (DC4):** a durable, re-derivable signal, NOT the shared version int (a
  reindex/verify Save or a checkpoint Save stamps the version and would defeat a
  version gate). Use a manifest `CollapsedAt`/marker AND the presence of any
  3-component (folder-scoped) key of a live token as the "owes collapse" signal;
  idempotent, so a crash mid-walk re-runs it. **Scope to the token(s) archived this
  run** (never re-key another mailbox's keys — R3).
- **Full index rekey (DC5):** `CollapseByIdentity` returns the COMPLETE old→new
  remap for every KEPT record whose key changes — survivors included (a no-move v3
  survivor's key still changes `token\x00folder\x00mid → LiveKey`, yielding no
  `CollapseLoss` today). `idx.Rekey(remap)` applies it in ONE batched transaction,
  `wal_checkpoint(TRUNCATE)`+fsync'd and durable BEFORE the manifest advances / the
  marker is set; drop-the-occupant collision semantics (reuse RepairKeys).
- **Loser files (DC9, #4):** the survivor record records loser file paths in
  `AlsoFiles []string`; loser folders are recorded as an **audit-only** note, not a
  point-in-time assertion (see DC12).

## 5. Pre-download skip + the #8 posture (DC7, DC8)

On a live incremental listing entry whose `LiveKey` is already archived:

1. Ask the source capability for the entry's `PhysID` (may be empty).
2. **PhysID present:**
   - equals a sibling's stored `PhysID` → **skip** (same physical message); record
     move/present-again from the listing.
   - matches none, but exactly one sibling has an EMPTY `PhysID` (legacy / not yet
     stamped) → **skip + backfill** that `PhysID` onto the sibling (no download):
     establishes the baseline; a later divergent `PhysID` is then caught.
   - matches none and every sibling has a non-empty `PhysID` → a **DISTINCT
     physical message** → **download + #fp-split** (both survive — R1; **this closes
     #8 on Graph**).
3. **PhysID empty (the floor — no source hint):** skip by Message-ID membership.
   If the identity already has >1 distinct #fp sibling, OR a `PhysID` has never been
   available for it, emit a MANDATORY `id-reuse` Issue at the SKIP DECISION site
   (DC8d) — the bounded, logged residual on IMAP/local.

Guards (DC8): skip only on an exactly-one match/So baseline case above (else
download-compare); backfill `PhysID` ONLY when empty, never overwrite a non-empty
one, and log a `content/phys mismatch` anomaly if a non-empty stored `PhysID`
would change; force download-compare when the listing is too sparse to identify.
No bodyPreview is used anywhere, so there is no served-page injection surface and
no preview-drift re-download (a rev-3 residual, now gone).

## 6. Redaction reaches every copy (DC9 #4, DC14 #10)

- **AlsoFiles carried + deleted.** `AlsoFiles` rides the DedupMailboxWide write
  path (the fresh Record build) and `MergeFields`; collapse populates it.
  `reindex` DELETES every `AlsoFiles` path when the survivor's own `Path` is
  redacted/off-disk (a loser has no index row for the row-walk to reach; `/files/`
  is a bare FileServer). Prove-fail: `GET /files/<loserPath>` → 404 after reindex.
- **reindex-only `.eml` sweep.** A separate `reindex`-path sweep removes an
  html-less `.eml` (never `SweepOrphans`, which also runs on NORMAL runs and would
  delete a legitimately-orphaned `.eml` from a crash between the `.eml` and `.html`
  writes — R13). The zip-vs-eml asymmetry is documented.

## 7. Durability, format, scale

- **Format v5 (DC10).** Manifest → v5; update MA-201 ("refuses > 4" → "> 5") in
  the same change. New fields `FpScheme`, `PhysID`, `AlsoFiles` are `omitempty`
  with load defaults; version-gated load. (encoding/json drops unknown fields, so
  an older equal-version binary on a shared -out would strip them — the version
  bump prevents the silent round-trip.)
- **Checkpoint on PROCESSED (DC11).** Count downloads + skips + moves, not
  `exp.Stats.Exported` (a migration/steady run is nearly all skips), and persist
  backfilled `PhysID`/fp/LastSeen at each checkpoint so an interrupted large run
  converges.
- **#9 honesty (DC12).** History has no per-event timestamp and FoldEvents is
  last-writer-wins within a run header, so a collapse's loser-folder assertion
  cannot be a point-in-time guarantee under the same header. Record loser folders
  as an **audit note on the record**, not a history assertion; drop any at-date
  loser-folder test. The pre-upgrade timeline is genuinely unknown and is stated
  as such.
- **Scale (DC15).** D adds NO listing widening (no bodyPreview/size on Graph), so
  the ~⅓ page-width cost rev-3 incurred is gone. Evaluate Graph `$delta` (native
  move/removal signalling, changed-items-only) as the scale-correct replacement for
  re-listing every run; keep `$top=1000`. The `Prefer: IdType="ImmutableId"` header
  rides the existing listing — no extra request.

## 8. Invariants

- **R1** — content fp (post-download, scheme-tagged) is the sole split
  discriminator; #8 closed on Graph via `PhysID`, bounded+logged residual on
  IMAP/local; adopt-never-split protects legacy records.
- **R3** — one copy per identity per mailbox (live path); collapse token-scoped;
  local imports keep folder-scoped keys.
- **R17** — Message-ID membership skip holds across moves with NO download; the
  first upgraded run skips (no storm).
- **R13** — normal runs never delete; redaction is the sole deletion path, now
  reaching loser copies + orphan `.eml`.
- **R21** — collapse loser folders are audit-only (not a false point-in-time
  claim); cluster-D fixes already landed.

## 9. Build slices (proposed) + test plan (DC13)

Slices, each with prove-fail: **G1** scheme tag + adopt-never-split + v5 format +
MA-201 update; **G2** `PhysID` capability, Graph ImmutableId producer, pre-download
skip logic + id-reuse Issue; **G3** collapse marker + `CollapseByIdentity` full
remap + `idx.Rekey` (wired into RunGraph before the walk); **G4** `AlsoFiles`
carry + reindex delete + reindex-only `.eml` sweep; **G5** checkpoint-on-processed
+ `$delta` evaluation + docs/README/goback.md.

Migration tests (DC13) use GENUINE fixtures, never a v4 record reshaped as v3:
(1) a real Version-3 archive (folder-scoped keys, real files, fp over the
pre-adoption MIME-Date instant) → run1 skips by membership after collapse: zero
fetches, zero duplicates, zero `{k,gone}`, one LiveKey record, index rekeyed;
(2) an fp="" fixture; (3) a v4 envelope-sig fixture; (4) an `id-reuse` test — a
distinct message reusing a Message-ID with a distinct Graph ImmutableId → downloaded
+ #fp-split, both survive; (5) redaction reaches an AlsoFiles loser (404 after
reindex) and an orphan `.eml`.

## 10. Residual risks (operator-accepted)

- #8 on IMAP/local (no reliable `PhysID`): a distinct message reusing a Message-ID
  is skipped; bounded and MANDATORY-logged; closed on Graph via ImmutableId. Full
  closure would forbid the skip (abandons R17) — rejected.
- `PhysID` first-observation window on legacy records: the very first post-upgrade
  observation backfills `PhysID` assuming same-message; a distinct reuse arriving
  in exactly that window is the only miss — one-run, logged.
- Graph ImmutableId is MS-controlled; if withdrawn, the Graph path degrades to the
  floor (still correct; #8 residual returns). No correctness dependency.
