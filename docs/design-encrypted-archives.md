# Encrypted archives — design (DRAFT)

Status: **draft — 10-lens design review FILED 2026-10-24
(`docs/review-encrypted-archives-predesign.md`), verdict GO_WITH_CONDITIONS.** Not built.
The review's 12 conditions (C1–C12) and the owner rulings (D1 remember-key default, D2
keep search, D5 full-build-before-ship) owe a fold-in revision of this doc; C1/C2/C5/C6
are material schema changes whose amended §2/§4 are re-verified before code, and **C2
(encrypt filenames + folder structure vs. scope the claim to contents-only and warn) is a
pending OWNER RULING.** Then the adversarial + friction gates per slice before any ship. This draft records what is settled, the
hard problems the codebase imposes, a recommended approach for each, and the handful of
decisions that are the OWNER's to make. It is grounded in three read-only code-mappings
of this repo (reader/search, write engine/scheduling, wizard/config/keychain) done
2026-10-24, and modelled on the client-side-encryption method in the sibling repo
`open-seavault-rclone` (its `DESIGN.md`/`SECURITY.md`).

## 0. Goal

Let a user OPTIONALLY encrypt an archive, so a copy of it that leaves this machine
(a cloud backup, a handed-over drive, a stolen folder) is unreadable without the
password. Owner's requirements (2026-10-24):

- **G1** Offered as an option in the **setup wizard**.
- **G2** Selectable on the **Desktop configuration page**.
- **G3** The user can **change the password**.
- **G4** A clear **warning to save the recovery key in a password manager**.

## 1. First ruling — encrypted archives are opened by MailArchive (R-E1)

Owner ruling 2026-10-24: **one solution, not two.** An encrypted archive is opened
with MailArchive; there is no second in-browser decryptor.

- **Unencrypted archive** — opens in any browser, no software (UNCHANGED, still the
  default). This is the existing "self-contained HTML" property.
- **Encrypted archive** — opens with MailArchive: it asks for the password, unlocks,
  and shows the same reader (the existing local loopback reader server). A scheduled
  backup unlocks from the stored key.
- **"Open anywhere" escape hatch** — a **Decrypt-a-plaintext-copy** command exports a
  normal unencrypted archive from an encrypted one (with the password). From there it
  is the usual double-click-in-any-browser. This gives the openness without a second
  crypto implementation.

**Why this is the SAFER choice, not only the simpler one.** The alternative (a
browser/WebCrypto decryptor) is a *second, independent crypto implementation* that must
stay in lockstep with the Go one — double the surface to get right and audit, for a
feature whose entire value is security. It also only ever worked for small archives
(whole corpus in browser memory) and could not do search (search is a server+SQLite
design; browsers cannot `fetch()` local files on `file://`). One crypto path is the
correct engineering call.

## 2. Crypto method (modelled on open-seavault-rclone)

Greenfield: there is **no content encryption anywhere in the product today**, and
`golang.org/x/crypto` is not yet a dependency. This is a new, well-scoped dependency
(Argon2id + AES-256-GCM), no cgo (keeps the pure-Go / reproducible-build posture).

**Key hierarchy — a random data key, wrapped by the password.** This is what makes
"change the password" cheap and "recovery key" natural:

1. On "encrypt this archive", generate a random **master key** (the key the archive
   data is actually encrypted with) and a random KDF salt.
2. Derive a **key-encryption key (KEK)** from the password with **Argon2id** (params
   stored, non-secret).
3. **Wrap** (encrypt) the master key with the KEK using AES-256-GCM → the "password
   entry".
4. Generate a high-entropy **recovery key** (a word phrase), derive a second KEK from
   it, and wrap the SAME master key again → the "recovery entry".

- **Change password (G3) = re-wrap, nothing re-encrypted.** Unwrap the master key with
  the old password, derive a new KEK from the new password, re-wrap. Instant, regardless
  of archive size. The archive bytes are untouched.
- **Recovery key (G4) = the recovery entry.** If the password is forgotten, the recovery
  key unwraps the master key (and can set a new password). The warning writes itself and
  must be shown at generation, in plain words: **"Save this recovery key in your password
  manager now. If you lose BOTH your password and this key, the archive can never be
  opened — not by you, not by us. There is no reset."**
- **Cipher:** AES-256-GCM (AEAD — authenticated; detects tampering). Data is encrypted
  with the master key; each encrypted unit carries a fresh random nonce + AAD.

**Non-secret parameters live in a sidecar, not in a secret store.** Store the KDF
algorithm/salt/cost, the wrapped master key (password entry + recovery entry), the cipher
id, and a format version in a new **`.mailarchive-vault.json`** at the archive root
(parallel to `.mailarchive-manifest.json`). Everything in it is ciphertext or public
parameters, so a `0600` file is fine. The sidecar is readable up front to answer "is this
archive encrypted?" before touching the manifest. (Alternative considered: an additive
field on the manifest, bumping manifestVersion 6→7. Rejected for separation of concerns —
crypto metadata should not live in the message ledger.)

## 3. Where the keys live, and the honest threat model

**This is the most important thing for the owner to understand and rule on.**

**The secret store is a real OS vault only on Windows.** `graphconfig.SecretStore`:
- **Windows** → Windows Credential Manager (DPAPI-backed), ~2560-byte blob cap (a
  passphrase or a 32-byte wrapped key fits easily).
- **macOS and Linux** → **NOT the system Keychain.** It is a `0600` plaintext file per
  account under `~/Library/Application Support/mailarchive` / `$XDG_CONFIG_HOME/mailarchive`.
  (This is already how the M365 secret, the OAuth token, and the SMTP alert password are
  stored.) A future improvement could add the real macOS Keychain / libsecret, but today
  it is a protected file.

**Threat model (what encryption here does and does not protect) — R-E2:**
- **PROTECTS:** a copy of the archive *separated from this machine* — synced to a cloud
  folder, copied to a handed-over USB drive, or a stolen archive folder — is unreadable
  without the password/recovery key. This is the seavault model and the stated goal (G0).
- **DOES NOT PROTECT:** someone with access to your *logged-in computer*. If the key is
  stored for unattended backups, on macOS/Linux it sits in a `0600` file on the same
  machine as the archive, so an attacker with your account gets both. (Same residual as
  seavault's "device compromise not protected", and why G4's password-manager warning —
  an off-machine copy of the recovery key — matters.)

**The unattended-key tension (DECISION D1 — owner).** A *scheduled* backup must append
new mail to the encrypted archive with no human present, which means it must decrypt the
manifest and the search index — so the key must be retrievable non-interactively, i.e.
stored. Two postures:
- **"Remember the key" (recommended default):** store the master key *wrapped* (never the
  raw passphrase) in the secret store so scheduled backups run unattended. Usable; the
  at-rest protection is against an *off-machine* copy (as above).
- **"Ask me each time" (opt-in, maximal):** store nothing; the archive only opens/updates
  when the user types the password. **Scheduled unattended backups cannot run** in this
  mode (state it plainly at the toggle). For the user who wants the key never on disk.

Recommendation: offer both, default to "remember", store only a *wrapped* key, and name
the trade-off at the control.

## 4. The hard problems on the write side

**Two engines — and the simple one is not the one scheduled backups use.**
- **Engine A — `export.Exporter`** (`internal/app/run.go` / `graph.go`): the incremental
  archiver. **All scheduled backups use this.** It has **no single file-writing choke
  point** — writing is spread across ~5 mechanisms: `export/atomic.go` (html/eml +
  `WriteFileAtomic` pages/readme), `export/attach.go` (attachment zips), `internal/index`
  (the SQLite search index), `internal/state` (manifest/history/lastrun), and
  `util.WriteFileAtomic0600` (small state).
- **Engine B — `copyexport`** (`internal/app/copyexport*.go`): the Desktop interactive
  "extraction workspace" / portable export. Routes **every** write through one `ceFS`
  abstraction with an existing `wrapFS`/`fsWrap` hook (a clean single encrypt-on-write
  seam) and writes no SQLite — but it is NOT the incremental/scheduled path.

**4.1 The SQLite search index is the hardest piece (D2 — technical, with a UX edge).**
`search.db` is a random-access SQLite (FTS5) file with its own WAL/shm; it cannot be
transparently stream-encrypted. Options:
- **(a, recommended) Encrypt `search.db` as a whole unit at rest.** On open, decrypt it
  (to memory, or to a `0600` temp the app owns and deletes), query/append, re-encrypt on
  close. Fits the incremental append (load → modify → save). Caveat: memory/temp sized to
  the index (tens–hundreds of MB on a large mailbox). A decrypted temp on disk partially
  undercuts at-rest protection while the app is open — prefer in-memory where feasible.
- **(b) A crypto VFS / SQLCipher-style page encryption.** Cleanest at rest, but
  `modernc.org/sqlite` has no such VFS out of the box — significant work.
- **(c) No full-text search when encrypted** (keep only the per-page filter). Worst UX;
  mention only as a fallback.

**4.2 Engine A's many seams (D3 — technical).** Recommendation: introduce a single
filesystem seam into Engine A (the way Engine B already has `ceFS`/`wrapFS`) and apply
encrypt-on-write there, rather than wrapping four writers independently. This is the
clean long-term shape and the biggest build cost. The SQLite index (4.1) is handled
separately regardless.

**4.3 Reproducible bytes (D4 — technical).** The archive-bytes golden
`TestHealthyInputsMatchTheGolden` (MA-329, `p14-golden.json`) and the zip-reproducibility
test compare byte-for-byte SHA-256 of output files. **Random-nonce AEAD produces
different ciphertext every run**, which would break MA-329, the zip determinism, the
copyexport `SHA256SUMS`, and `verify` fixity (fixity is recorded over on-disk bytes).
Recommendation: **keep the reproducibility guarantee at the PLAINTEXT layer** — the engine
still produces deterministic plaintext; MA-329 tests plaintext *before* encryption; add a
decrypt-round-trip test; record `verify` fixity over the plaintext (or over ciphertext
accepting it is per-run, decided in the review). **Do NOT** use deterministic/synthetic
nonces to force byte-equality — deterministic AEAD nonces are a crypto footgun. (The two
"frozen baselines" in CLAUDE.md are charset/Unicode pins — unaffected by encryption.)

## 5. UX surfaces

**5.1 Wizard (G1).** `internal/wizard` is zenity-driven. Add an **"Encrypt this archive?"**
step right after the output-folder step (encryption is a property of the output), before
`runExport`. On Yes: a **masked** password entry (`zenity.HideText()` — works on macOS,
Windows, Linux) + a confirm entry; then generate the recovery key and **show it once**
(non-masked so it can be copied) with the G4 warning. Add `encrypt bool` (+ the captured
secret, held in memory only) to `wizardChoice` → `app.Options`.

**5.2 Desktop config (G2/G3).** Mirror the Backup-alerts card's proven secret-capture
pattern (form `type=password` → guarded POST → OS secret store; the page only ever
receives a boolean, never the secret). Add a **`sec-encryption` card** (+ a nav anchor):
an "Encrypt this archive" toggle, a set/**change-password** control (old + new), a
**"Show recovery key" once** reveal with the G4 warning, and a status line. New guarded
endpoint `POST /api/encryption` (`server.GuardLocalPOST` + CSRF, exactly like
`saveAlertSettings`); secrets to `cfg.Store` under a per-archive account key
(`"archive:"+ShortHash(out)`); non-secret parameters to the `.mailarchive-vault.json`
sidecar.

**5.3 CLI.** `-encrypt` at create; password via a prompt or `MAILARCHIVE_PASSWORD` env
(**never** on argv); `mailarchive decrypt <archive> <dest>` (the escape hatch);
`mailarchive change-password <archive>`. Hidden terminal entry like seavault's.

**5.4 Reader open.** The local reader server (`internal/desktop/reader.go` /
`internal/server`) prompts for the password (or reads the stored wrapped key), unlocks
the master key, and serves — decrypting message files on demand and the search index per
4.1. No plaintext is written back to the archive directory while reading.

## 6. Decisions for the owner

- **D1 (threat model / unattended key)** — offer both "remember the key" (default,
  enables unattended scheduled backups; key stored *wrapped*; at-rest protection is
  against an off-machine copy) and "ask me each time" (no stored key, no unattended
  backups)? Recommended: yes, both, default remember.
- **D2 (search when encrypted)** — accept approach 4.1(a) (whole-`search.db` encrypt,
  decrypt-on-open), with the memory/temp caveat? Or is "encrypted ⇒ no full-text search"
  acceptable as a first cut? Recommended: 4.1(a).
- **D5 (scope / phasing)** — see §7. What ships first?

(D3 Engine-A seam and D4 reproducibility are technical; recommended approaches above,
to be confirmed in the 10-lens review.)

## 7. Recommended phasing

- **Phase 1 — crypto core (pure, no engine changes).** The vault sidecar, key hierarchy,
  Argon2id + AES-256-GCM, wrap/unwrap, password change, recovery key, the secret-store
  account key. Fully unit-testable in isolation (like the pre-sync TP1 core). Owes its own
  10-lens + adversarial pass.
- **Phase 2 — encrypt-on-write in Engine A** (the FS seam, §4.2) **+ the SQLite story
  (§4.1)** + the reproducibility test move (§4.3). The heaviest slice.
- **Phase 3 — the UX:** wizard step, Desktop `sec-encryption` card, CLI flags, the reader
  unlock, and the Decrypt-a-plaintext-copy escape hatch.
- **Phase 4 — unattended scheduled backups** against an encrypted archive (stored wrapped
  key), with the health/status surfacing ("this archive is encrypted").

## 8. Open questions / risks (for the 10-lens review)

- Decrypted `search.db` temp vs in-memory, and the at-rest exposure while the reader is open.
- Nonce management and the AAD scheme across thousands of per-message files.
- Atomic password change / vault-sidecar rewrite under a concurrent scheduled run (the
  per-profile-lock pattern from pre-sync may apply).
- Rollback/freshness of the vault sidecar (seavault's TOFU anchor — likely out of scope v1).
- Partial-encryption failure (power loss mid-encrypt): never a half-encrypted archive.
- `verify`/fixity semantics over ciphertext vs plaintext.
- Key rotation beyond password change (re-keying the master key) — likely out of scope v1.

---

## Post-review resolution — AUTHORITATIVE (2026-10-24)

The 10-lens review (`docs/review-encrypted-archives-predesign.md`, verdict
GO_WITH_CONDITIONS) and the owner rulings settle the design. Where this section conflicts
with the draft above, this section governs; the draft is kept for traceability. Condition
numbers (C1–C12) reference the review.

**Owner rulings.** D1 — both key modes, default "remember the key". D2 — keep full-text
search (realised via the chunked container below, not a whole-`search.db` temp). D5 — full
build before ship (one release, every gate). **C2 — encrypt names AND structure.**

**The data model — the vault is a "keyless facade" (C1, the true blocker).** The design's
backbone is that some subsystems must learn facts about an archive WITHOUT the key
(archindex classification, the scheduler's freshness/source-loss, `SweepOrphans`, `verify`,
`reindex`). The `.mailarchive-vault.json` sidecar is promoted from "is this encrypted?" to a
**first-class, minimised, authenticated facade** carrying EXACTLY the keyless-consumer facts
and no more: a format version (C8), a random **stable archive-id** (C7), an "encrypted" kind
marker archindex reads (C10), the wrapped master-key entries (password + recovery), the KDF
salt/params, and a fixity/inventory anchor. Its **leak budget is explicit and disclosed**:
it reveals that the archive exists, is encrypted, and its id/params — nothing about contents,
names, or structure. Every keyless consumer is ruled, per consumer, to read the facade or be
handed the key; this schema is frozen in Phase 1 before any code.

**What is encrypted (C2).** Message bodies, attachments, the search index, the manifest, AND
all names + the folder tree. On disk, each unit is stored under an **opaque keyed name**
(flattened — no mailbox tree); the reader rebuilds the logical names and folder tree from the
**decrypted manifest** (the existing message ledger — no new source of truth). Residual,
disclosed inline at R-E2: ciphertext length, message count, and filesystem timestamps are not
hidden (confidentiality of names/structure/contents is; traffic-analysis of size/count/time
is not).

**Crypto (C5, C6).** AES-256-GCM AEAD; Argon2id KDF with a **stated, calibrated cost**
(~256 MB / ~0.5–1 s, sized for a small always-on host; stated inline per C12). Random master
key wrapped by a password-KEK and (second wrap) a recovery-KEK — password change and recovery
are re-wraps, never data re-encryption. The index AND the manifest are **chunked AEAD
containers** (fixed-size chunks, per-chunk nonce, a top-level authenticated chunk manifest
binding count+order), so an incremental run re-seals only changed chunks → O(delta) cost and
O(delta) cloud re-upload. Each message's AAD binds it to its manifest identity; the
authenticated manifest is the inventory, and the reader/`verify` cross-check presence so a
deleted/truncated/reordered unit is detected (the §2 "detects tampering" claim is scoped to
per-unit bytes; whole-archive rollback stays a conceded non-goal).

**Durability & no single point of total loss (C3).** The vault sidecar is written with
`util.WriteFileAtomic0600` and mirrored to a byte-identical `.bak`, healed on open; the G4
step escrows the recovery-entry blob+params (not just the phrase) into the password manager.
Password change rewrites the sidecar atomically under the existing per-profile lock. Index/
manifest containers persist atomically and fail CLOSED to "needs rebuild"; `reindex`/`Rebuild`
is given the unlock (C6). Decrypted temps (only when memory won't hold the index) are named
`.mailarchive-*.tmp` under `out` so the existing `SweepOrphans` reaps them after a crash.

**Encrypt-existing / decrypt-in-place = copy-and-cutover (C4).** Re-encode the whole archive
into a sibling workspace through the Engine-B `ceFS` seam, verify a decrypt round-trip, atomic
rename-swap, secure-delete the plaintext tree, under an "encryption-in-progress" marker with
copyexport's progress/cancel UI. A crash before the swap leaves the original intact
(fail-closed); never mixed state. Decrypt-in-place is the same run reversed (also the rollout
reversal path). Secure-delete is best-effort on SSD/COW — disclosed.

**Unattended safety (C7, C8, C11).** The wrapped key is filed under `archive:<vault-id>`
(stable, travels with the archive) with a `RequireRetrievableKey` guard mirroring MA-689: an
unattended run that cannot unwrap writes NOTHING and raises `BACKUP-NEEDS-ATTENTION.txt` + the
health WARN / Desktop banner / email alert (fail-closed + surfaced, reusing the Slice-3
machinery). The manifest `format_version` is bumped for encrypted archives so an old binary
hits the existing refuse-newer guard and fails closed rather than serving ciphertext as HTML
or appending plaintext. Until the Phase-4 unattended path exists, encrypting a scheduled
archive disables the schedule (surfaced) or is refused.

**UX (C9, C12).** The recovery key is accepted everywhere the password is (reader unlock,
Desktop reset-password, `mailarchive reset-password` / `--recovery-key`), plus a "generate a
new recovery key" re-wrap. Inline disclosures: the password-strength floor+meter and R-E2's
conditionality on it; "changing the password does not re-secure copies already made"; the
escape-hatch plaintext warning + refuse/double-confirm on a sync-root/removable destination;
the crypto-dependency security-patch fast-path (a CVE in the crypto deps is a pre-authorised,
operator-gated exception to the version pin).

**Revised phasing (C11).** Phase 1 — crypto core + the frozen keyless-facade schema + vault
durability + password change + recovery + stable-id key store (pure, unit-testable). Phase 2a
— the Engine-A single FS seam + content/name encrypt-on-write. Phase 2b — the chunked AEAD
container for the index + manifest. Phase 2c — reproducibility/golden relocation (MA-329 →
plaintext layer) with its own prove-fail/prove-pass. Phase 3 — UX (wizard, Desktop card, CLI,
reader unlock, recovery surfaces, escape hatch, encrypt-existing cutover). Phase 4 —
unattended scheduled backups + the RequireRetrievableKey surfacing. Each slice owes its own
adversarial + friction passes; nothing ships until all are built (D5).

**Re-verification.** This resolution materially changes the data model (§2/§4). The amended
schema (the keyless facade, the chunked containers, the AAD/manifest-inventory scheme) owes a
**focused re-review before code** — the affected lenses re-run against this section, not a
fresh 10-lens from scratch.
