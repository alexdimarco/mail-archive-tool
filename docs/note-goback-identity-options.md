# Note — choosing the go-back live-dedup identity (Graph now, IMAP later)

**Purpose.** Decide, before drafting rev 4, what identity the live/repeat path
deduplicates on. The 10-lens gate (DC6) recommended Graph ImmutableId; the
operator flagged two risks that this note weighs as first-class criteria: (1) the
archiver is **source-agnostic** and a live **IMAP** source is planned, so a
Graph-only identity may be a mistake; (2) Microsoft can **change** ImmutableId,
breaking a design that depends on it. No code changes; this is a decision aid.

## What "identity" must do (requirements)

- **R17** — recognise an already-archived message pre-download, INCLUDING after a
  folder move, so re-runs don't re-download bodies.
- **R1** — never silently drop a genuinely distinct message that reuses a
  Message-ID.
- **#7-clean** — not spuriously re-download because a pre/post-download value
  disagrees (the inline-attachment bug).
- **Migratable** — upgrade existing v2/v3/empty-fp archives without duplication or
  phantom deletions.
- **Portable** (operator) — the SAME identity model should carry to a future live
  IMAP source and to the local IMAP/Evolution/Thunderbird caches already ingested,
  not fork per provider.
- **Vendor-robust** (operator) — must not break if one provider changes or removes
  a proprietary feature.

## What each source actually gives us (the substrate)

| Signal | Graph (app-only) | Live IMAP (RFC 3501) | Local cache / PST |
|---|---|---|---|
| RFC 5322 **Message-ID** | listing field | `ENVELOPE` / header fetch | parsed |
| Stable **per-physical-message server id** across moves | **ImmutableId** (opt-in `Prefer: IdType`, MS-controlled) | **none** — `UID` is folder-scoped, a MOVE assigns a NEW uid, and `UIDVALIDITY` can reset → full re-sync | none |
| Cheap pre-download **envelope** (subject/from/to/date) | listing `$select` | `FETCH ENVELOPE` | parsed |
| Cheap pre-download **exact byte size** | ✗ (not in listing) | **`RFC822.SIZE`** | n/a (have full msg) |
| Cheap pre-download **structure/attachments** | `hasAttachments` (bool; wrong for inline — #7) | **`BODYSTRUCTURE`** (full parts tree) | n/a |
| **Content** (body + attachment bytes) | post-download | post-download | already present |

**The load-bearing fact:** the ONLY identity common to all three — and stable
across a folder move — is the **Message-ID** (and, absent one, a content hash).
A stable server-assigned per-message id exists on Graph *only* (ImmutableId), is
*absent* on IMAP (UID is folder-scoped and move-unstable), and is proprietary.

## Candidate mechanisms

- **A — Graph ImmutableId as the identity.** Solves R17-on-move and #8 perfectly
  ON GRAPH (distinct id per physical message, zero content inspection). But it is
  Graph-only (no IMAP analogue), opt-in and MS-controlled (vendor-fragile), and
  **cannot retrofit** existing v3 archives (they hold no ImmutableId), so DC1's
  scheme-tagged migration is still required. Fails Portable and Vendor-robust.
- **B — Envelope signature (rev-3's EnvSig), portable form.** A pre-download hash
  of listing/ENVELOPE scalars. Portable in principle (Graph listing, IMAP
  ENVELOPE). But #7 unless computed from one source on both sides; #8 residual
  (sender-authored scalars); bodyPreview drift; and on Graph the strongest
  discriminators (size, structure) aren't in the listing. Heavy machinery
  (DC2/DC3/DC7/DC8) for a bounded, provider-uneven guarantee.
- **C — Portable floor: Message-ID skip + post-download content fingerprint.**
  Pre-download, skip a listed message whose **Message-ID** is already archived
  mailbox-wide (works everywhere; the move is recorded FROM THE LISTING's folder,
  no download). Post-download, the **content fingerprint** (scheme-tagged) is the
  sole distinct-reuse discriminator (#fp-split). #7 cannot occur (no pre/post
  envelope compare). #8 residual: a distinct reuse of an archived Message-ID is
  skipped pre-download (never downloaded, so never split). Uniform across all
  sources; depends on no vendor id.
- **D — C plus an OPTIONAL, per-source pre-download discriminator.** The portable
  floor of C, but when a source offers a cheap, reliable extra signal, use it to
  decide *whether to download before skipping* and thereby shrink the #8 window —
  never as the identity:
  - IMAP: `RFC822.SIZE` + `BODYSTRUCTURE` (strong, non-authored-ish) → if the
    already-archived record's stored size/structure differ from the listing, force
    a download-and-compare instead of skipping.
  - Graph: `ImmutableId` when present → if the listed ImmutableId differs from the
    stored one for the same Message-ID, it is a DISTINCT physical message → force
    download-and-split (closes #8 on Graph) — used as a *hint behind a capability*,
    with graceful fallback to the floor if MS changes or withholds it.
  - Local: full content already available; dedup by content fingerprint directly.

## Recommendation

**Adopt D: a portable Message-ID + content-fingerprint floor (C), with per-source
pre-download discriminators as optional enhancements behind a capability
interface.** Rationale against the operator's two concerns:

- **Portable.** The core identity is `m.Identity()` — which ALREADY exists and is
  source-agnostic (`mid:` / `sha:`). Live IMAP and the local caches reuse it
  unchanged. No fork per provider.
- **Vendor-robust.** Correctness never depends on ImmutableId (or any UID). If MS
  changes ImmutableId, the Graph path degrades from "closes #8 + no re-download on
  move" to the floor (still correct; a distinct-reuse residual returns, a move is
  still recorded from the listing). Nothing breaks.
- **#8, honestly.** Fully closing the distinct-reuse drop requires a per-physical
  server id (Graph ImmutableId) OR downloading every already-archived message
  (abandons R17). So #8 is closed **on Graph when ImmutableId is available**, and
  is a **bounded, logged, documented residual** on IMAP/local (mitigated by the
  IMAP size/structure discriminator, which is stronger than Graph's listing). This
  is an honest, source-dependent posture — not a false "closed everywhere" claim.
- **#7** disappears (no pre/post envelope comparison in the floor).
- **Migration (DC1)** is unchanged and still required: a scheme-tagged
  Message-ID hit on upgrade is the SAME message → adopt, never split.

**Net effect on the gate conditions.** D keeps the Message-ID/content core the
whole system already uses, so it *retires* most of the EnvSig-specific conditions
(DC2/DC3/DC7/DC8 collapse to "optional per-source discriminator + a mandatory
id-reuse log") and keeps the structural ones that apply to any scheme: DC1
(scheme tag + adopt-on-match), DC4/DC5 (collapse marker + full index rekey),
DC9/DC14 (redaction reaches loser copies + reindex-only .eml sweep), DC10 (v5
format), DC11 (checkpoint on processed), DC12 (honest #9), DC13 (three-state
migration test), DC15 (scale/$delta). The design gets SMALLER and more portable,
and the ImmutableId benefit is captured as a Graph-only optimisation rather than a
foundation.

## The decision this asks for

1. **Identity model for rev 4:** adopt **D** (portable Message-ID + content
   fingerprint floor, optional per-source discriminators) — recommended — or
   **A** (Graph ImmutableId as identity, accepting Graph-lock + a separate IMAP
   design later), or **B** (hardened EnvSig everywhere)?
2. **#8 posture:** accept the bounded, logged distinct-reuse residual on
   IMAP/local (closed on Graph via the ImmutableId hint), or require the residual
   closed on every source (which forbids the pre-download skip and gives up R17)?

Once chosen, rev 4 folds the decision + DC1–DC15 and re-runs the adversarial pass
before any build.
