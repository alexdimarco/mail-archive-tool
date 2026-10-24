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

(Correction, 2026-10-24, Claude: C2 was since ruled by the owner — encrypt names +
structure — and the fold-in is done; see the design doc's "Post-review resolution". The
paragraph above is kept as written.)

## Focused re-review (2026-10-24)

**Reviewed:** the "Post-review resolution" section of `docs/design-encrypted-archives.md`.
**Digest after amendments (sha256):** `2153192cd568494cfc35cc761fa6ff700f2dda129498112c3ed1100615f0bfde`
**Repository SHA:** `2e7827c`
**Reviewers:** three independent read-only adversarial checks, one per changed surface —
(1) crypto construction, (2) keyless-facade data model + leak budget, (3) durability /
crash-safety / concurrency. Load-bearing code claims were re-confirmed by the main session
before folding in (`manifest.go:693-704`, `lastrun.go:71`, `securefile.go:18-48`,
`presync/session.go:21` vs `app/run.go:414,847`).

**Verdict: GO, with the schema frozen as amendments A1–A14** (in the design doc). The shape
held; it was not yet freezable because it named mechanisms without pinning bytes.

| Finding | Check | Severity | Resolution |
|---|---|---|---|
| Re-sealed chunk could reuse a nonce; random 96-bit GCM budget unbounded over archive life | crypto | blocker | A1 — XChaCha20-Poly1305, fresh random nonce every seal |
| AAD "binds to manifest identity" not byte-specified; archive-id not in unit AAD; per-unit replay undecided | crypto | blocker | A2 — exact AAD layouts; per-unit rollback detected via manifest hashes |
| Argon2id "calibrated" could fall on a weak host; recovery-key entropy unpinned | crypto | major | A3 — params are floors; 128-bit recovery key, own salt |
| Refuse-newer unreachable: parse fails at `manifest.go:693` before the version check at 704, with a "delete it to re-export" message | facade | blocker | A4 — cleartext manifest envelope |
| Scheduler/freshness consumer had no ruling; facade field list dropped it | facade | blocker | A5 — per-consumer table |
| `lastrun.json` (source path, counts) and `sourcestreak.json` (source kind) leak past the budget | facade | blocker | A6 — minimised cleartext status; rest moves into the encrypted manifest |
| Facade's keyless fields unauthenticated (recovery entry strippable, id swappable) | facade | major | A7 — header MAC under an HKDF sub-key |
| No generation counter → heal has no winner | durability | high | A7 — monotonic generation; vault rewritten only on key events |
| `WriteFileAtomic0600` skips the directory fsync | durability | med | A8 |
| Change-password named the Thunderbird pre-sync lock, not the archive lock | durability | high | A9 |
| Two key stores with no joint atomicity on password change | durability | high | A10 — secret store holds the master key; password change touches the vault only |
| Manifest chunk persist could leave a mixed old/new state (manifest is not rebuildable) | durability | high | A11 — immutable chunks, single-rename pointer, ordered |
| Directory swap is two renames; conversion could race a scheduled run | durability | med-high | A12 — staged marker under the archive lock |
| `SweepOrphans` can reap a long-held reader temp under `out` (Unix) | durability | med | A13 — reader temps in the instance dir |
| Keyless `verify` can't catch deliberate substitution | durability | med | A5 — two-tier verify, claim scoped |
| Schedule-disable write could fail and leave the schedule live | durability | low | A14 — job reads the marker and refuses |

**Refuted:** the envelope/key hierarchy itself (sound); KDF-param/salt downgrade (unwrap
fails closed); fixity anchor leaking count (count is already a disclosed residual);
plaintext-layer golden leaving an authenticated-determinism gap (AEAD catches corruption on
decrypt).

**Closure:** A1–A14 are the checks' own recommended fixes, not new design; they are frozen
as Phase-1 code + tests, and Phase 1's own adversarial pass re-checks the schema as built.
No further pre-code round.

## Phase-1 adversarial pass (2026-10-24)

**Reviewed:** `internal/vault` and the `util.WriteFileAtomic0600` change at commit `ebeb8a3`.
**Run:** workflow `vault-phase1-assurance` (`wf_62c576e2-5e8`): 44 single-mutation prove-fail
runs (one worktree each), five security lenses (crypto, format/hostile input, durability,
fail-closed, conformance/test honesty) with three skeptics per finding (kept on 2 of 3), and a
completeness critic. 164 agents.

**Mutation record.** 41 of 42 mutations that ran were killed by a named failing test.
M19 (drop the recovery key's canonical-body check) survived — a real gap, now covered by
`TestANonCanonicalLastCharacterIsRefused`. M06 (drop the chunk index from the AAD) and M44
(write the mirror before the primary) did not run in the sweep (their worktrees started on
`main`); M06 was run by hand and killed by `TestAADLayoutsAreTheFrozenBytes` +
`TestSealedBytesCannotMove`; M44 is now caught by
`TestSaveWritesThePrimaryThenTheMirrorWithSweepableTemps`. M12 (drop the ceiling check) was
reported killed by inference, not execution; the refusal-before-derivation is now observed
directly through the Argon2id seam (`TestUnlockRefusesACostOutsideTheBoundsBeforeDeriving`).

**Confirmed findings and resolution** (severities are the skeptics' consensus):

| Finding | Severity | Resolution |
|---|---|---|
| Deleting the vault files makes an encrypted archive read as plaintext; nothing outside the folder records otherwise | medium (design gap) | **A15** — the gate takes the machine-recorded archive id; missing/swapped/damaged vault refuses (MA-717) |
| A failed mirror write after the primary landed drops a new recovery key that is already in force (old one then refused) | medium | **A7** commit point + two-step create/regenerate; `ErrPartiallySaved` returns the key (MA-714) |
| A failed or interrupted create leaves an unshown-recovery-key vault that blocks every retry | low | two-step create; a failed create removes what it wrote (MA-714) |
| Copies of two different archives are never compared; Heal can adopt or spread a foreign vault | low–medium | refused with `ErrTampered` naming both ids, never healed (MA-715) |
| A newer-format copy is skipped and then healed over by an older build | low–medium | any newer copy refuses the whole open (MA-715) |
| Same-generation fork: the winner may not be the copy the secret opened, and Heal spreads it | low | tie → the opened copy; Heal refuses a fork; a deliberate change settles it (MA-715) |
| A planted copy forces a ceiling-cost (4 GiB) derivation before the real copy | low | ceiling lowered to 1 GiB/16/4; cheapest copy tried first; identical copies derived once (MA-715, MA-705) |
| Chunk AAD binds the container generation, conflicting with O(delta) re-seal | medium (design) | **A2 refined** — a chunk's own seal generation, recorded per chunk in the pointer (MA-704) |
| No known-answer tests for HKDF sub-keys, MAC header, Argon2id, recovery rendering | low–medium | MA-716 (HKDF + base32 cross-checked against Python) + a checked-in vault fixture |
| Vault rename has no Windows sharing-violation retry | low | `util.ReplaceRetrying`, shared with `internal/state` (MA-713) |
| Windows readCopy follows a symlink (no Lstat) | low | Lstat before open on every OS (MA-709) |
| Rewrites don't re-check the lock before writing | low | `StillHeld` before every save (MA-710) |
| Recovery keys pasted with Unicode spaces/dashes are refused; error counts bytes | low | NFKC + Unicode separators skipped; rune counts (MA-706) |
| When every copy fails, a planted copy's error can mask "wrong password" | low | wrong-password is reported if any copy said so (MA-715) |
| Wrap nonce/MAW1 AAD, "before any derivation", threads 0, NFC count, more MAC fields, save order/temp name untested | low (test gaps) | added to MA-703/704/705/707/714 |

**Refuted** (majority of skeptics): Destroy leaving copies (struct copies are bounded and
documented); header MAC/AAD binding build constants (both copies share the format; a newer
format is refused); envelope having no reader yet (Phase 2 scope); CreateAt check-then-rename
race (serialised by the archive lock); typed errors flattened by the gate (they now wrap);
a planted different-id copy blocking the gate (now a named refusal per A15).

**Also checked:** `go test -race` on the touched packages, `go vet` for Windows and Linux
from macOS, and `govulncheck` (v1.1.4): `golang.org/x/crypto` v0.57.0 has one advisory
(GO-2026-5932, the unmaintained `openpgp` package) that this module does not import. The
standard-library advisories it lists are for the LOCAL go1.26.0 toolchain; CI and release
build with the newest 1.26.x patch (`go-version: '1.26.x'`), so they are an operator
question about the pinned local toolchain, not a Phase-1 finding.

**Verdict:** Phase 1 passes its adversarial gate with the fixes above. No friction review is
owed for Phase 1 (no user-facing surface); the UX gates fall on Phase 3.

## Fix-verification round (2026-10-24)

**Reviewed:** commit `cc61a23` (the fixes above). **Run:** workflow
`vault-phase1-fix-verification` (`wf_4b0cec13-c64`, 113 agents): 26 mutations aimed at the
fixes, one adversarial verifier per fix, two fresh-eyes lenses (new-code correctness;
hazards for the Phase 2–4 callers) with three skeptics per finding, a critic.

**Mutations:** 24 of 26 killed by named tests. N14 (drop the pre-open Lstat) survived as
equivalent on macOS (O_NOFOLLOW refuses a link there) — the Windows-only judgment is now a
pure rule pinned for both OSes (`TestTheVaultFileTypeRuleOnEveryOS`), and its USE in
`readCopy` is proven on every OS through an `lstat` seam that presents a readable vault as a
link (`TestAVaultPathJudgedALinkIsNeverOpened`; N14 re-planted by hand: killed).
(Correction, 2026-10-24, Claude: commit `4f6bb11`'s message credited the rule test alone with
catching N14; re-planting N14 showed it did not — the rule test pins the rule, not its use —
so the seam test was added in the next commit.) N17 (drop the empty-id
refusal) survived — now asserted by name (`TestTheGateNamesTheFixForEachCause`), and a vault
claiming the all-zero id no longer parses.

**Confirmed, and fixed** (consensus severities):

| Finding | Severity | Resolution |
|---|---|---|
| A `Discard` after a successful `Commit` destroyed the returned key, and a destroyed/zero key still sealed under an all-zero key | medium | keys carry a lifetime (`ErrKeyDestroyed` on every use); `Commit` hands the key over (MA-718) |
| A new-recovery-key `Commit` destroyed its key on every failure: a retry reported a false "altered" | medium | key destroyed only when the change is spent; retryable after a no-write failure; `ErrPendingUsed` after use (MA-714) |
| A foreign copy beside a missing or damaged own copy was opened and healed over | medium | the recorded id on every keyed call; a damaged copy's claimed id compared (MA-715) |
| A newer-format copy with trailing data read as damaged and was healed over | low–medium | the format is read first, from the first object (MA-707, MA-715) |
| A fork could never be settled by a new recovery key from the mirror side | low | `Commit` accepts the base on either side of a fork (MA-715) |
| The fork test compared bytes: a re-spaced copy was a "fork" | low | a fork is decided by generation + MAC (MA-715) |
| A cheaper older copy tried first could make a current secret "superseded" (latent) | low | the newest authentic copy wins if the secret still opens it (MA-715) |
| The gate's refusal for a deleted vault matched `ErrNoVault` | low | `ErrVaultMissing`, never `ErrNoVault` (MA-717) |
| The gate gave one fix for every cause, and checked no folder first | low | per-cause fix; folder checked first (MA-711) |
| The unattended path could neither see nor heal a damaged copy | low | the gate returns `*Opened`; `Heal(…, KeySecret(key))` (MA-711) |
| Keys, secrets and pending changes printed their secret bytes | low | redacting `Format` (MA-718) |
| Tampering found by the right password was reported as "wrong password" | low | tampering after a successful unwrap ranks first (MA-715) |
| Two error paths dropped the typed cause | low | `%w` on both (create cleanup, `ErrPartiallySaved`) |
| An interrupted create made the retry show a key that never opens | low | `PrepareCreate` refuses an existing vault before any key is shown (MA-714) |
| Lost-lock re-check proven for one of four writers; create cleanup unguarded | low (test gap) | all four writers proven through the lock seam; cleanup re-proves the lock (MA-710) |
| Production writer, Windows-only retry rule, U+2028/U+0085, NFC as a format, recovery-path cost bounds, archive id under the MAC untested | low (test gaps) | MA-713, MA-714, MA-706, MA-716, MA-705, MA-707 |

**Refuted** (majority): `ErrPartiallySaved` as an error on a succeeded path (documented,
and the key is returned with it); the writers taking their own lock (a caller holding the
lock is a Phase-2 design point, not a defect); the A15 id having no API to hang on (it does,
now on every keyed call); `Detect` "no vault" outside the lock (no write path uses it as
plaintext); `PendingCreate.Commit` over a plaintext archive (encrypt-existing is the C4
cutover, Phase 3); exporting the one-step helpers (now test-only regardless).

**Open by design (carried to later phases):** the A15 record and every write path that must
refuse are Phase 3–4 wiring — R37 now says so; a fork is settled silently by the next change
(the Phase-3 UX must name both sides first); A2's per-chunk generation exists in the AAD and
the design, and its container reader/writer is Phase 2b work with its own rows.

**Verdict:** the fixes hold; Phase 1's API is frozen for Phases 2–4.
