# Pre-code design review — encrypted archives Phase 2a

**Design reviewed:** `docs/design-encrypted-archives-phase2a.md` (first draft, uncommitted at review
time; the folded revision is the committed file).
**Repository SHA:** `3859f3d`
**Date:** 2026-10-24
**Gate:** the pre-code adversarial design review (`../assurance-kit/process/design-review.md`),
focused on the Phase-2a slice of the already-reviewed parent design.
**Run:** workflow `phase2a-design-review` (`wf_8454eed2-51e`, 226 agents): six lenses (C2 leaks,
crypto/format, plaintext regression, durability, fail-closed completeness, architecture/staging),
three skeptics per finding (kept on 2 of 3), a verdict synthesis. Grounded in the read-only
Engine-A map `wf_c8dbaadd-8ce`.
**Tallies:** 73 findings raised, **62 confirmed**, 11 refuted.

## Verdict: **GO_WITH_CONDITIONS**

The central idea holds — logical `Record.Path` everywhere in the app, sealed units under opaque
names in a flat `units/` tree — but five blockers and a set of gaps needed design changes before
engine code. All are folded into the design (its sections cite the condition ids).

**Blockers:**
1. Rewriting a sealed unit in place under a fixed name: with a fresh nonce per seal and the
   manifest saved only at checkpoints, an ordinary crash would leave disk and `Record.Sealed`
   disagreeing — a false tamper verdict never repaired (→ P2a-C1: revisions, delete after commit).
2. The unit id carried "like FirstFolder": FirstFolder is carried only on the mailbox-wide M365
   path (exporter.go:563-600), so every local re-export would draw a new unit (→ P2a-C3).
3. Renaming the `-copy-first` snapshot to end in `.tmp`: the reader is chosen by extension
   (run.go:1213), so every `-copy-first` PST/OST run would break (→ P2a-C11: keep the extension,
   sweep by prefix). This was the draft author's error.
4. Outlook scratch PSTs and run logs written into `-out` before any refusal could run, and kept on
   refusal (→ P2a-C9: a keyless guard at the front-end choke points).
5. 2b's working search index as a full cleartext copy inside the archive, its `-wal`/`-shm` never
   swept (→ P2a-C13, parent A19).

## Conditions (folded)

P2a-C1 revisions + delete-after-commit · C2 per-kind table · C3 Unit rule independent of
DedupMailboxWide, uniqueness invariant · C4 keyed inventory sweep + missing-manifest refusal ·
C5 unit-store hardening, pinned encoding, kind guard, keyed names · C6 Sealed is keyed only; keyless
inventory in 2b (A17) · C7 collapse refused on encrypted archives · C8 one recognition predicate ·
C9 choke-point guard, re-check under the lock, key-to-archive binding, more consumers · C10
side-effect split interactive/unattended, final wording · C11 plaintext-visible changes in their own
slice · **C12 owner ruling** · C13 decrypted index never under `out` (A19) · C14 key/identity
material under `out` is a refusal · **C15 owner ruling** · C16 field allow-lists, closed error kinds
· C17 rebuilt C2 property test + structural tree test · C18 logs: fix the location, no redacting
logger · C19 disclose size/timing, normalise mtimes · C20 structural reachability meta-test · **C21
owner ruling** · C22 reader drops uncommitted hits · C23 parent amendments recorded (A16–A20).

## Owner rulings (2026-10-24)

- **C12** — an encrypted archive's `-copy-first` snapshot and Outlook scratch go to **an app-owned
  folder on this computer** (config dir), with a free-space check and a clear refusal.
- **C15** — uninstall **keeps** an encrypted archive's remembered key by default; removing it is a
  separate, confirmed choice.
- **C21** — attachments too large for in-memory sealing are **skipped and reported, at a higher
  limit than the 256 MiB proposed**; the design sets **1 GiB** per message (peak memory ~2–3×), a
  single constant the owner may change.

## Open questions answered

Q1 keyed name derivation (HMAC under an HKDF sub-key; unit ids never leave the sealed manifest) ·
Q2 an explicit ceiling (owner: 1 GiB) · Q3 refuse in 2a, pin type + ordering + no-side-effect,
final wording in Phase 3 · Q4 neither — fix log locations at logger creation.

## Refuted (majority)

Engine A's refusal reading in-folder markers (covered by the recorded id, A15); an Engine-B
encrypt-existing plan file (Phase 3, C4 cutover); unkeyed names + sinks (superseded by C5e); §5's
"Today" column assuming the envelope (wording only); the shared placement function's input drift;
`omitempty` on new fields (adopted anyway); the in-memory zip disturbing plaintext WriteZip;
a plaintext redacting logger; Run/RunGraph setup repetition; Unit as 2b's chunk-bucketing key;
a leaf package for the unit format.
