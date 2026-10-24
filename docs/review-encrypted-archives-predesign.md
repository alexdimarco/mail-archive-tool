# Pre-code design review — encrypted archives

**Design reviewed:** `docs/design-encrypted-archives.md`
**Design digest (sha256):** `7fb74c028d8481d0a522d63b97a3060a6ef56e50dde1b62ee8a5fb0627efa9eb`
**Repository SHA:** `4b1a1e2823964eb500671f9beea85a9fd1ec73eb`
**Date:** 2026-10-24
**Gate:** `../assurance-kit/process/design-review.md` (the 10-lens pre-code adversarial gate)
**Reviewers:** 5 lens-pair finders (lenses 1–10), 1 skeptic (refutation-default), 1 rescue —
run as the `encrypted-archives-design-review` workflow (`wf_c0bce187-f15`).
**Tallies:** 39 findings raised, **27 CONFIRMED**, 12 refuted/merged.

## Verdict: **GO_WITH_CONDITIONS**

The feature is sound and rescuable — no finding kills it. The rescue pass dissolved 24
of 27 confirmed findings with modest, reuse-heavy mechanism changes (existing repo
primitives), and left 3 as disclose-and-raise-the-bar (inherently conditional). But it
also surfaced a **true blocker** the lenses only partly saw (below), which is a Phase-1
data-model decision that must be made before the vault/manifest schema is frozen. Build
may not start until the conditions below are folded into the design doc and the amended
schema sections are re-verified; one condition (C2) is an **OWNER RULING** the reviewer
cannot impose.

## The true blocker (rescue, highest-value find)

This product's architecture is **"every subsystem derives archive facts LIVE by reading
the archive's on-disk bytes"** — archindex classification (`inspect()`→`state.LoadFrom`),
the scheduler's freshness / source-loss streak, `SweepOrphans`, `verify`/fixity (recorded
over on-disk bytes), `reindex`/`Rebuild`, and the golden/`SHA256SUMS` invariants.
**Encryption severs live-derivation for every consumer that does not hold the key**,
forcing a per-subsystem choice the draft never makes: hand that subsystem the key
(expanding exactly the at-rest-exposed / unattended-key surface §3 tries to shrink) or
give it a cleartext projection. The vault sidecar IS the design's implicit cleartext
projection, but §2 scopes it to one question ("is this archive encrypted?"); it never
faces that the sidecar must **grow** to carry every fact a keyless consumer needs
(is-archive, kind, freshness/lastrun, a fixity anchor, a stable archive-id) — while every
fact it carries is a metadata leak (finding 1) and every fact it withholds breaks a
keyless consumer (finding 17). **Resolution (not abandonment):** design the sidecar as a
first-class, **minimised, authenticated "keyless facade"** with an explicit, disclosed
leak budget, and rule per keyless consumer whether it reads the facade or is handed the
key. **Secondary:** the encrypted **manifest** inherits every search.db pathology
(whole-file re-encrypt O(total) cost, crash-atomicity, temp exposure) EXCEPT it is NOT
rebuildable — it is the system of record — so it needs the same chunked-container +
atomic-write + authenticated treatment, and its loss has no reindex-style recovery.

## Conditions (must-fix-before-code; each becomes a tracked change to the design doc)

- **C1 — the keyless facade (the true blocker).** Specify the vault sidecar as a
  first-class, minimised, authenticated data model: enumerate each keyless consumer
  (archindex, scheduler/freshness, SweepOrphans, verify, reindex) and rule, per consumer,
  whether it reads the facade or is handed the key; declare the facade's leak budget
  explicitly. Freeze this schema in Phase 1 before any vault/manifest code.
- **C2 — metadata scope (OWNER RULING).** Filenames today are
  `YYYY-MM-DD_HHMM_<subject-slug>_<hash>` + `<stem>-attachments.zip`, and the folder tree
  mirrors mailbox folders ("Legal", "HR-complaints"); encrypt-on-write (contents only)
  leaves all of that cleartext in an off-machine copy. **Decide: encrypt names + structure
  (opaque keyed names on disk, reader rebuilds the tree from the decrypted manifest — the
  rescue's recommendation, matching §1's "entire value is security") OR scope R-E2 to
  "contents only" and warn.** Either way, disclose the residual (ciphertext length,
  message count, filesystem mtimes are not hidden) inline at R-E2.
- **C3 — no single point of total loss (dissolves blocker finding 8 + finding 14).** Write
  the vault sidecar with `util.WriteFileAtomic0600` (temp+rename+fsync); keep a
  byte-identical `.mailarchive-vault.bak.json` healed from on open; and have the G4 step
  escrow the recovery-entry blob + salt + params (ciphertext/public) into the password
  manager alongside the recovery phrase. Serialise the change-password rewrite against
  scheduled runs with the existing per-profile lock. Crash leaves old-or-new, never
  truncated.
- **C4 — encrypt-existing = copy-and-cutover (dissolves blocker finding 18 + rollout
  rollback finding 23).** Re-encode the whole archive into a sibling workspace through the
  Engine-B `ceFS` seam, verify a decrypt round-trip, atomic rename-swap, secure-delete the
  plaintext tree, under an "encryption-in-progress" marker with copyexport's progress/
  cancel UI. A crash before the swap leaves the original plaintext archive intact
  (fail-closed); never mixed state. Provide decrypt-in-place as the same run reversed (the
  rollout's reversal path). Disclose that secure-delete is best-effort on SSD/COW.
- **C5 — structural authentication (scopes the §2 "detects tampering" over-claim, finding
  6).** Bind each unit's AAD to its manifest identity; the authenticated manifest is the
  archive's inventory; the reader/`verify` cross-check that every message the decrypted
  manifest lists is present and authenticates — a deleted/truncated/reordered unit is then
  detected, not silently dropped. Whole-archive rollback/freshness stays the §8-conceded
  non-goal. Scope the §2 claim to "authenticates each message's bytes."
- **C6 — chunked AEAD container for the index AND the manifest (dissolves findings 9, 22,
  23, search-cost).** Replace whole-file encryption with fixed-size AEAD chunks + a
  top-level authenticated chunk manifest (count+order), so an incremental run re-seals only
  changed chunks → O(delta) cost and O(delta) cloud re-upload. Persist atomically
  (temp+rename of changed chunks + atomic pointer swap); a crash mid-persist fails CLOSED
  to "needs rebuild." Name any decrypted temp `.mailarchive-*.tmp` **under `out`** so the
  existing `SweepOrphans(out,…)` reaps it after a crash; prefer in-memory where it fits.
- **C7 — key binding + fail-closed unattended (dissolves findings 15, 20, 25).** Generate a
  random **stable archive-id** at encrypt time, store it in the (travelling) vault sidecar,
  and file the wrapped key under `archive:<vault-id>` (not the output-path hash — removes
  the move/rename loss and the 32-bit collision). Add a `RequireRetrievableKey` guard
  (mirroring MA-689 `RequireExistingOut`): an unattended run that cannot unwrap the master
  key writes NOTHING (no append, no decoy) and raises `BACKUP-NEEDS-ATTENTION.txt` + the
  health WARN / Desktop banner / email alert. Drill DPAPI-reset and XDG/HOME-change as
  "key not retrievable."
- **C8 — refuse-newer for old binaries (dissolves finding 24).** Reverse §2's rejection:
  bump the manifest `format_version` for encrypted archives (crypto params still live in
  the sidecar — only one integer moves) so an old binary hits the existing refuse-newer
  guard (`state.NewerFormatError` / `index.ErrNewerFormat`) and fails CLOSED with
  "written by a newer MailArchive — upgrade," never serving ciphertext as HTML or appending
  plaintext into an encrypted archive.
- **C9 — reach the recovery key (dissolves findings 13, 14).** Accept the recovery key
  everywhere the password is accepted (reader unlock, a Desktop "reset password with
  recovery key", `mailarchive reset-password` / a `--recovery-key` flag). Add "generate a
  new recovery key" (a cheap re-wrap replacing the recovery entry, shown once).
- **C10 — archindex recognises encrypted archives (dissolves finding 17).** Teach
  `inspect()` the vault sidecar as a second recognition marker: a valid vault with an
  unparseable (ciphertext) manifest returns `IsArchive=true`, `kind="encrypted"`, so the
  Desktop card shows a labelled encrypted entry, not greyed-as-broken.
- **C11 — build order + slice split (dissolves findings 7, 21).** Phase-gate: until the
  Phase-4 unattended-key path exists, encrypting an archive that has a schedule disables
  the schedule (with the standing surfacing) or is refused. Split Phase 2 into (2a) FS seam
  + content/name encrypt-on-write, (2b) index/manifest chunked crypto, (2c) reproducibility/
  golden relocation (MA-329 → plaintext layer) — each independently reviewable with its own
  covers markers and prove-fail/prove-pass.
- **C12 — honesty-of-claims (lens 10; findings 2, 3, 26, and the §4.1 recommendation).**
  Inline next to the claims: a calibrated Argon2id cost (sized for a small always-on host,
  e.g. ~256 MB / ~0.5–1 s) + a password strength floor/meter, with R-E2's conditionality
  on password strength stated; the "changing the password does not re-secure copies already
  made" caveat at G3/G4; the decrypted-temp while-open exposure named at §5.4; the escape-
  hatch plaintext warning + a refuse/double-confirm on a sync-root/removable destination; a
  documented crypto-dependency security-patch fast-path (a CVE in `golang.org/x/crypto`/
  stdlib is a pre-authorised, operator-gated exception to the version pin).

## Owner rulings already recorded (fold into the doc with the conditions)

- **D1** — both key modes, **default "remember the key"** (unattended backups on by default).
- **D2** — **keep full-text search** when encrypted (now realised via C6's chunked container,
  not a whole-`search.db` temp).
- **D5** — **full build before ship**: the complete feature ships as one release, through
  every gate; no partial/phased ship.

## Confirmed findings (condensed; full text in the workflow result)

| # | Lens | Section / OMISSION | Severity | Rescue outcome |
|---|------|--------------------|----------|----------------|
| 1 | 1/6 | §3 R-E2 — names/sizes/tree cleartext (metadata leak) | blocker | PARTIAL → C2 (owner) |
| 8 | 3/7 | §2 — single vault sidecar, no backup, bricks archive | blocker | DISSOLVED → C3 |
| 18 | 3/4 | §5.2/§4.2 — encrypt-existing has no conversion mechanism | blocker | DISSOLVED → C4 |
| 6 | 1/10 | §2 — GCM "detects tampering" over-claim (structural) | major | DISSOLVED → C5 |
| 2 | 6/10 | §2/§5.1 — no password floor → offline brute-force | major | PARTIAL → C12 |
| search-cost | 3/9 | §4.1(a) — whole-index re-encrypt O(total)/run | major | DISSOLVED → C6 |
| 9,10 | 3 | §4.1(a) — index crash ordering / no atomicity | major | DISSOLVED → C6 |
| 22 | 3/6/7/10 | §4.1(a) — plaintext index temp survives a crash | major | DISSOLVED → C6 |
| 23 | 7 | §4.1/§5.4 — reindex has no decrypt path | major | DISSOLVED → C6 |
| 15 | 5 | §5.2 — key filed under mutable path-hash | major | DISSOLVED → C7 |
| 20 | 7 | OMISSION — fail-direction on key-retrieval failure | major | DISSOLVED → C7 |
| 25 | 9 | OMISSION — dead-secret-store drill (DPAPI reset) | major | DISSOLVED → C7 |
| 24 | 8 | §2 — version bump rejected → old binary serves/appends | major | DISSOLVED → C8 |
| 13 | 4 | OMISSION — no surface accepts the recovery key | major | DISSOLVED → C9 |
| 14 | 7 | §2/§8 — change-password sidecar rewrite not atomic | major | DISSOLVED → C3 |
| 17 | 5 | §4 — encrypted manifest → archindex greys it out | major | DISSOLVED → C10 |
| 21 | 5 | §7 — Phase 3 UX before Phase 4 unattended key | major | DISSOLVED → C11 |
| ask-each | 4 | §3 D1 — "ask each time" silent scheduled failure | major | DISSOLVED → C7/C11 |
| 3 | 10 | §2/G3 — change-password doesn't re-secure copies | minor | DISSOLVED → C12 |
| 11 | 3/8 | §2 — "format version" not wired to refuse-newer | minor | DISSOLVED → C8 |
| escape | 1 | §1/§5.3 — decrypt writes plaintext to any dest | minor | DISSOLVED → C12 |
| 12 | 2 | §7 — Phase 2 bundles three hard changes | minor | DISSOLVED → C11 |
| 26 | 9 | §2 — Argon2id params unstated → unreviewable/OOM | minor | DISSOLVED → C12 |
| patch | 9 | §2 — no crypto-dependency patch plan | minor | DISSOLVED → C12 |
| regen | 4 | §5.2 — no regenerate-recovery-key | minor | DISSOLVED → C9 |
| 4.1(c) | 2 | §4.1 — recommends the at-rest-regressing option | minor | DISSOLVED → C6/D2 |

(Refuted/merged: 12, incl. the D1 "wrapped==raw on-machine" claim — refuted because §3
already concedes device-compromise inline.)

## Status

This review is **filed**. The design doc owes a revision folding C1–C12 + the owner
rulings; because C1/C2/C5/C6 are material schema changes, the amended data-model sections
(§2, §4) are re-verified before code (a focused re-review of the changed sections, not a
fresh 10-lens). C2 blocks on the owner.
