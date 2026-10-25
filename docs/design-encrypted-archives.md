# Encrypted archives — design (DRAFT)

Status: **schema FROZEN 2026-10-24 — 10-lens review (GO_WITH_CONDITIONS) folded in as the
"Post-review resolution"; the focused re-review of the amended schema folded in as the
"Re-review amendments" (A1–A14); the Phase-1 adversarial pass refined A2/A3/A7 and added A15
("Phase-1 adversarial pass — amendments"), which govern.** Phase 1 (`internal/vault`) built. All owner rulings settled (D1
remember-key default, D2 keep search, D5 full-build-before-ship, C2 encrypt names +
structure). Next: Phase 1, then the adversarial + friction gates per slice before any ship. This draft records what is settled, the
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
fresh 10-lens from scratch. (Done 2026-10-24 — see the amendments below.)

---

## Re-review amendments — AUTHORITATIVE, FROZEN SCHEMA (2026-10-24)

The focused re-review (three adversarial checks: crypto construction, keyless-facade model,
durability/crash-safety; record in `docs/review-encrypted-archives-predesign.md` §"Focused
re-review") found the post-review resolution sound in shape but NOT yet freezable: it named
mechanisms without pinning the bytes. The points below are the frozen Phase-1 schema. Where
they conflict with anything above, these govern. Amendment ids A1–A14 are cited by Phase-1
tests.

**A1 — Cipher and nonce discipline.** All sealing (message/attachment units, container
chunks, container pointers, key wraps) uses **XChaCha20-Poly1305** (`golang.org/x/crypto/
chacha20poly1305`, 192-bit nonce) instead of AES-256-GCM. Every seal AND every re-seal draws
a **fresh random 192-bit nonce** from `crypto/rand`; nothing derives a nonce from a chunk
index, a counter, or content. Why: a re-sealed mutable chunk that reused its nonce would be a
total break, and 96-bit random GCM nonces cap a single key at ~2^32 seals (NIST SP 800-38D)
— within budget today but resting on an unbounded lifetime re-seal count. 192-bit random
nonces remove the budget question. Same dependency (`x/crypto`) the design already adds; no
cgo.

**A2 — AAD layout (exact bytes).** Every AAD starts with a 4-byte domain tag, then fixed
fields, length-prefixed where variable, so no two kinds of sealed object can be confused:

- unit (message body, attachment): `"MAU1" ‖ format_version u16 ‖ archive_id [16]byte ‖
  unit_kind u8 ‖ len u16 ‖ unit_id` — `unit_id` is the unit's stable manifest identity.
- container chunk: `"MAC1" ‖ format_version u16 ‖ archive_id ‖ container_kind u8 ‖
  generation u64 ‖ chunk_index u32`.
- container pointer: `"MAP1" ‖ format_version u16 ‖ archive_id ‖ container_kind u8 ‖
  generation u64`; the pointer's plaintext lists, in order, each chunk's file name and
  ciphertext SHA-256, and the chunk count.
- key wrap: `"MAW1" ‖ format_version u16 ‖ archive_id ‖ entry_kind u8` (password | recovery).

The stable `archive_id` is in **every** AAD. **Rollback scope, stated:** the authenticated
manifest records each unit's ciphertext SHA-256, so swapping in an older blob for one message
(per-unit rollback — possible from a cloud backup, which shares the master key) is
**detected**. Rolling the WHOLE archive back to an older consistent state stays the conceded
non-goal.

**A3 — Argon2id as a floor.** Password wrap: Argon2id, memory ≥ 256 MiB, time ≥ 3,
parallelism 1, 16-byte random salt. These are MINIMUMS: a slow host never lowers them; a fast
host may raise time. The recovery key is **128 bits from `crypto/rand`**, shown as
26 base32 characters plus 1 check character, in dash-separated groups, and gets its own
independent salt. Both wraps record their own params; unwrap accepts only params at or above the floor
(a downgraded params block is refused, not tried).

**A4 — Cleartext manifest envelope (fixes the refuse-newer path).** For an encrypted
archive, `.mailarchive-manifest.json` is a small cleartext JSON envelope:
`{"version": <encrypted-format>, "encrypted": true, "archive_id": "…", "pointer": "…"}`.
The real manifest lives in the chunked container that `pointer` names. An older binary
unmarshals the envelope successfully (unknown fields are ignored), reaches the existing
`m.Version > manifestVersion` check (`internal/state/manifest.go:704`), and fails CLOSED
with "written by a newer MailArchive — upgrade". Without the envelope it would fail at the
parse step (`manifest.go:693-694`) with "delete it to re-export everything", inviting a
plaintext re-export into an encrypted archive. Disclosed limit: the shipped v2 binary has
no refuse-newer guard at all (`manifest.go:701`) and is not protected by any format choice.

**A5 — Every keyless consumer, ruled.**

| Consumer | Without the key it reads… | Needs the key for… |
|---|---|---|
| archindex `inspect()` | vault marker → `IsArchive=true, kind="encrypted"` (C10) | nothing |
| scheduler / `status` / health / Desktop banner | the minimised status file (A6) | schedule "Repair" (rebuilds the job line — interactive) |
| `SweepOrphans` temp reaping | `.mailarchive-*.tmp` names (unchanged) | the orphan-zip half — replaced in Phase 2a by "file not in the decrypted inventory", run only in keyed runs |
| `verify` | tier 1: each inventory file exists + its ciphertext SHA-256 matches (catches bitrot, loss, truncation — NOT deliberate substitution) | tier 2: full decrypt + manifest cross-check (catches tampering) |
| `reindex` / `Rebuild` | — | always handed the key |

**A6 — Leak budget, enforced on the status files.** Today `.mailarchive-lastrun.json`
records the job's full flags including `-input <source path>` (`internal/state/lastrun.go:71`)
and per-run counts, and `.mailarchive-sourcestreak.json` records the source kind. For an
encrypted archive those cleartext files carry ONLY: last-run time, ok/failed, consecutive
source-missing count + since-date, auto-paused flag. The job flags, source path, source kind
and per-run counts move into the encrypted manifest. Disclosed residual (unchanged from the
resolution): file sizes, file count, timestamps, and the minimised status above.

**A7 — Vault split + generation + header MAC.** The vault (`.mailarchive-vault.json` +
`.mailarchive-vault.bak.json`) holds only: format version, archive_id, a monotonic
`generation u64`, the two wrap entries (each with its own salt + params), and a
**header MAC** = HMAC-SHA256 under `HKDF-SHA256(master, info="mailarchive vault header v1")`
over a canonical encoding of every other field. The fixity/inventory anchor does NOT live in
the vault — it lives in the manifest envelope/container — so routine backups never rewrite
the vault; it changes only on create, password change, recovery regeneration. Heal on keyed
open: both copies must pass the header MAC; the highest generation that does wins and the
other is rewritten. Keyless readers (archindex) only need *a* parseable copy and trust nothing
in it. Write order on change: primary (gen+1) then `.bak`. A stripped recovery entry, a
swapped archive_id or a changed marker fails the MAC → refuse + surface.

**A8 — Durable rename.** Every vault, pointer and chunk write fsyncs the file, renames, then
fsyncs the parent directory (the pattern `manifest.Save` and `export.commitTemp` already use).
`util.WriteFileAtomic0600` today skips the directory fsync (`internal/util/securefile.go:37-47`);
Phase 1 adds it (strictly more durable for its existing callers too). Vault/pointer temps are
named `.mailarchive-*.tmp` so `SweepOrphans` reaps crash leftovers; those writes happen under
the archive lock (A9), so a sweep cannot race them.

**A9 — The right lock.** Change-password, recovery regeneration, encrypt-existing and
decrypt-in-place all take the **archive-folder lock** `out/.mailarchive.lock`
(`lockfile.AcquireAs`, the lock the export already takes at `internal/app/run.go:414, 847`).
The resolution's "per-profile lock" was wrong: that lock (`internal/presync/session.go:21`)
guards the Thunderbird profile during pre-sync only.

**A10 — What "remember the key" stores.** The remembered secret is the **master key itself**,
held by the OS secret store under `archive:<archive_id>` — Windows Credential Manager (DPAPI)
on Windows; a `0600` file on macOS/Linux, exactly like today's M365 secret. It is not wrapped
by the password (that would need the password, defeating unattended backups). So a password
change touches only the vault, never the secret store — one store, one atomic write. This
replaces the draft's "store the master key wrapped" wording, which overstated the protection
on macOS/Linux; R-E2 (device compromise not protected) already covers it honestly.

**A11 — Container persist order (the manifest has no rebuild).** Chunks are immutable: a
changed chunk is written to a NEW unique file (never rewritten in place), fsynced, dir-synced.
Only after every new chunk is durable is the pointer (one file, inside the A4 envelope)
replaced by one atomic rename + dir fsync. Superseded chunks are deleted only after the new
pointer is durable, and only by a keyed run holding the archive lock. A crash at any point
leaves the old pointer naming old, intact, authenticated chunks.

**A12 — Encrypt-existing / decrypt-in-place, staged.** Runs under the archive lock for its
whole duration. A marker `.mailarchive-convert.json` (deliberately NOT the sweep pattern)
records a stage: `copying → verified → swapping → swapped → cleaning`. The swap is two
renames (original → `<name>.mailarchive-old`, new → `<name>`), so the marker's stage says
exactly which state a crash left; on next start the run resumes forward from `verified` or
later and rolls back (deletes the workspace) before it. Reader and scheduler refuse an archive
with a marker present.

**A13 — Reader temps off the archive.** If the decrypted search index doesn't fit in memory,
the reader's temp goes in the per-account instance directory (`internal/instance`), not
under `out`: a long-held reader temp under `out` could be reaped by a concurrent export's
`SweepOrphans` on Unix. This reverses C6's "temps under `out`" for the reader only; writer
temps stay under `out`.

**A14 — Phase gate by reading, not by a one-shot write.** Until Phase 4 ships, the scheduled
job reads the keyless encrypted marker at start and refuses (surfaced: banner, health WARN,
`BACKUP-NEEDS-ATTENTION.txt`) — instead of relying on a schedule-disable write that could
fail and leave the schedule live.

**Re-review closure.** These amendments are the re-review's own recommended fixes, not new
design. They are frozen as Phase-1 code + tests; Phase 1's own adversarial pass re-checks the
schema as built. No further pre-code review round.

---

## Phase-1 adversarial pass — amendments (2026-10-24)

The Phase-1 adversarial pass (five security lenses, three skeptics per finding, a 44-mutation
prove-fail sweep and a completeness critic; record in `docs/review-encrypted-archives-predesign.md`
§"Phase-1 adversarial pass") confirmed findings that refine A2, A3 and A7 and add A15. These
govern where they conflict with A1–A14.

**A2 (refined) — a chunk's generation is its own.** A11 re-seals only changed chunks, so a
container at generation G holds chunks sealed at earlier generations. The chunk AAD's
`generation` is the generation at which THAT chunk was sealed; the authenticated pointer
records it per chunk (name, seal generation, ciphertext SHA-256), and a reader opens each
chunk at its recorded generation. A superseded version of a chunk never opens as the current
one.

**A3 (refined) — the ceiling is headroom, and the cheapest copy is tried first.** The cost
ceiling is memory 1 GiB, time 16, threads 4 (this build writes only the floor; the ceiling
bounds what a hostile vault can make an unlock spend). An unlock tries the copy with the
lowest cost first and never derives twice for byte-identical copies, so a planted
expensive copy costs a derivation only if the real one fails, and a typo costs one
derivation. When no copy unlocks, "wrong password" is reported if any copy said so — a
planted copy cannot mask it.

**A7 (refined) — the two copies, a commit point, and two-step key events.**
- The copies must name the same archive: copies of two archives are refused (`ErrTampered`,
  naming both ids) and never healed across. A copy from a newer MailArchive is never fallen
  back past or healed over (`ErrNewerVault` for the whole open). Symlinks and special files
  at a vault name are refused on every OS (judged before opening).
- On a generation tie, the copy the secret opened wins. Two different authentic copies at the
  newest generation (two machines changed the vault at once) are a **fork**: Heal refuses to
  choose (`ErrConflict`); the next deliberate change from one side settles it.
- The primary is the **commit point**: if the primary was written and the mirror was not,
  the change is in effect and the result says so (`ErrPartiallySaved`, with a new recovery
  key returned alongside it, never dropped).
- **A recovery key nobody has seen is never committed** (the seavault rule): creating a vault
  and replacing the recovery key are two steps — prepare in memory, show the key, the person
  confirms, then commit (`PrepareCreate`/`Commit`, `PrepareNewRecovery`/`Commit`). A failed
  create removes what it wrote so a retry starts clean; a commit over a vault that changed
  since it was prepared is refused (`ErrConflict`).
- Every rewrite re-proves the archive lock immediately before writing (the export's R5
  `StillHeld` rule) and writes nothing if it was lost.
- The vault write waits out a Windows sharing violation with the same bounded retry the
  manifest uses (`util.ReplaceRetrying`, shared with `internal/state`).

**A15 — an archive recorded as encrypted never reads as plaintext.** The vault, its mirror
and the manifest envelope live in the archive folder, which the threat model lets an attacker
rewrite — deleting all three would otherwise make an encrypted archive look like a fresh
plaintext one, and the next backup would export the mailbox unencrypted into it. So this
machine records, when an archive is encrypted, its archive id OUTSIDE the archive (the
schedule/job descriptor and the archindex entry in the config directory; wired in Phases 3–4),
and every path that writes to it is handed that id. `RequireRetrievableKey(out, want, store)`
takes the recorded id — never one read from the folder — and refuses (`ErrKeyNotRetrievable`)
when the vault is missing, damaged, from a newer MailArchive, or names another archive; a
missing vault is never "not encrypted". `Detect`'s "no vault" is documented as unauthenticated
absence, not proof of plaintext. Phase 2+ write paths that hold a recorded id must refuse the
same way and never fall back to a plaintext export.

**Fix-verification round (2026-10-24) — the API frozen for Phases 2–4.** A second
assurance round (26 mutations on the fixes, a verifier per fix, two fresh-eyes reviewers,
three skeptics per finding; record in the review doc) refined the API before any caller
exists:
- **Every keyed entry point takes the recorded id**: `Open(out, want, s)`,
  `ChangePasswordAt(out, want, …)`, `PrepareNewRecovery(out, want, …)`, `Heal(out, want, …)`;
  the zero ID means "no record" (a handed-over copy). A copy naming another archive than
  `want` is refused (`ErrTampered`), and a DAMAGED copy that still names another archive is
  refused even without a record.
- **The gate returns the opened vault** (`*Opened`: key, `NeedsHeal`, `Forked`), and an
  unattended run heals under its own lock with `Heal(out, want, KeySecret(key))`. The gate
  checks the folder is present first and names the fix for each cause (connect the drive /
  restore the key files / upgrade / open once with the password); a deleted vault is
  `ErrVaultMissing`, which never matches `ErrNoVault`.
- **Keys have a lifetime**: a destroyed, zero or nil `Key` refuses every use
  (`ErrKeyDestroyed`) instead of sealing under an all-zero key; `PendingCreate.Commit`
  hands its key over (a later `Discard` is a no-op); a prepared change is single-use
  (`ErrPendingUsed`); a new-recovery-key `Commit` that failed without writing (lock busy,
  folder absent, lock lost, primary write failed) can be retried with the same key. Keys,
  secrets and pending changes print redacted.
- **Two-step only**: the one-step create and regenerate helpers are test-only; `PrepareCreate`
  refuses an existing vault before any key is made or shown.
- **The winner rule**: after a secret proves the master key, the newest authentic generation
  wins if the secret still opens it (unchanged entry, or one more derivation) — a cheaper
  older copy tried first never makes a current secret "superseded"; a fork is decided by
  content (generation + header MAC), so a re-spaced copy is a heal, not a fork; a new
  recovery key commits from either side of a fork; tampering found after a successful unwrap
  outranks "wrong password" in the refusal shown.
- **Forward-compat contract**: every vault format is one JSON object whose `format` is a whole
  number, read first from the first object alone, so a newer vault is recognised as newer
  whatever it appends.
- **The vault file-type rule** is `util.DescriptorReadable` judged on the path before opening:
  a Windows cloud placeholder of a synced archive is accepted; a link anywhere is refused.

**A2 note for Phase 2b.** Fixed-size POSITIONAL chunks would re-seal every chunk after an
insertion; chunk boundaries must stay stable across runs (e.g. records bucketed by a hash of
their identity), or O(delta) is lost.

---

## Phase-2a review — parent amendments (2026-10-24)

The Phase-2a pre-code review (`docs/review-encrypted-archives-phase2a.md`, GO_WITH_CONDITIONS)
forces these changes to this design; `docs/design-encrypted-archives-phase2a.md` holds the detail.

- **A16 — three seams, not one.** The single Engine-A FS seam (§4.2, revised phasing) is replaced by
  the unit store (`export.Units`, content), the containers (2b: manifest, index, history) and
  minimisation inside the state/seal/schedule writers. The replacement guarantee is a structural test:
  after every encrypted run, refusal and failure, every path under `out` matches an allow-list
  grammar.
- **A17 — the keyless tier-1 inventory (A5).** `Record.Sealed` lives in the sealed manifest and is
  the KEYED rollback check. Keyless tier-1 `verify` reads a cleartext inventory 2b writes beside the
  pointer: `(opaque name, ciphertext SHA-256, size)` sorted by name, no unit id, kind or grouping;
  its leak budget equals a directory listing.
- **A18 — A6 as explicit allow-lists.** Each cleartext sidecar (lastrun, runhistory, sourcestreak,
  lastverify, both attention files, both schedule descriptors, the pause file) gets a field
  allow-list no wider than A6; reasons come from a closed list of run-failure kinds, never from error
  text; the lock's `host=` and the descriptor Host are disclosed in the leak budget.
- **A19 — A13 extends to writers.** A decrypted or working search index (including Rebuild and
  reindex output) is never written under `out`: in memory, or a config-dir scratch file with
  in-memory journal and temp store, swept.
- **A20 — uninstall keeps the remembered key (owner ruling).** Archives are always kept, so is the
  key that opens them unattended; removing it is a separate, explicitly confirmed choice.
- **Owner rulings recorded with them:** an encrypted archive's `-copy-first` snapshot and Outlook
  scratch go to an app-owned config-dir folder with a free-space check; attachments over 1 GiB per
  message are skipped and reported as a gap (owner: a higher limit than 256 MiB).
