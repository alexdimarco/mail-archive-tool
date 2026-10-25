# Encrypted archives — Phase 2a design

Status: **reviewed 2026-10-24 — pre-code review GO_WITH_CONDITIONS
(`docs/review-encrypted-archives-phase2a.md`); all 23 conditions (P2a-C1..C23) and the three
owner rulings are folded in below.** Parent: `docs/design-encrypted-archives.md` (A1–A15 and
its Phase-1 sections govern; the parent changes this doc forces are recorded there as A16–A20).
Grounded in a read-only map of Engine A (`wf_c8dbaadd-8ce`) at `3859f3d`.

## 0. Scope, and why 2a is unreachable

Phase 2a makes Engine A (`export.Exporter` under `app.Run`/`app.RunGraph`, the path of every
scheduled backup) write an encrypted archive's message content as sealed units under opaque
names in a flat layout, keeps every plaintext archive byte-identical, and makes every consumer
fail closed on an encrypted archive. It does not encrypt the manifest, index or history (2b),
move the golden test (2c), or add UX (3) or the unattended path (4).

2a alone protects nothing (the manifest, index and history stay cleartext beside the units), so
it must be **unreachable by users, structurally** (P2a-C20): an `assureblock` go/ast meta-test
fails if any non-test file outside `internal/vault` references a key-producing vault function
(`Create`, `PrepareCreate`, `Commit`, `Open`, `RequireRetrievableKey`) or assigns
`app.Options.Key` / `Exporter.Units`, unless the file is on an allow-list that starts EMPTY and
that Phases 3–4 extend as an operator-visible gate change. Tests reach the encrypted path through
`Options.Key`. D5 is enforced by that test, not by discipline.

## 1. Logical names stay; units are placements

Everything downstream speaks the logical `Record.Path` (index rows, reader URLs, page links,
go-back, report, `KeyForPath`, Rebuild). **2a-D1:** `Record.Path` is unchanged and byte-identical;
the on-disk placement comes from new manifest fields, `omitempty` so plaintext manifests are
byte-identical (MA-329):

- **`Record.Unit`** — 32 lowercase hex characters (128 random bits); Load refuses any other form
  and refuses two records sharing one (P2a-C3, C5c). **Rule (P2a-C3):** before an item's first
  write, if the FINAL key already has a record, take its Unit; otherwise draw a new one (retry if
  taken). A seen record in an encrypted archive with an empty Unit is a typed refusal (corruption),
  never a silent draw. `Qualify` and `placeByPhysID` case 3 create keys for DIFFERENT messages and
  always get a new Unit. Whole-record moves (`CollapseByIdentity`, the v2→v3 re-scope) carry it.
- **`Record.Sealed`** — per kind present: `{Rev, SHA256, Size}` of the sealed unit on disk. The
  keyed per-unit rollback check (A2); every kind a record holds has an entry, and a missing one is
  a refusal (P2a-C2, C6).
- **Unit reference** `ref = "<unit>.<rev>"` (rev decimal, starting at 1) is the AAD `unit_id`, so
  an older revision never opens as the current one (P2a-C1).
- **Opaque name** `units/<h[0:2]>/<h>`, `h = hex(HMAC-SHA256(K_names, ref ‖ kind))[0:32]` with
  `K_names = HKDF-SHA256(master, "mailarchive unit names v1")` — a new additive method
  `(*vault.Key).UnitName(kind, ref)` (P2a-C5e, Q1). Logs, errors and alert text name the on-disk
  name or the role, never the unit id; the decrypt-a-plaintext-copy escape hatch strips Unit and
  Sealed.
- **Kinds (2a-D2):** `UnitMessage` (page), `UnitOriginal` (NEW, 3, the preserved `.eml`),
  `UnitAttachment` (zip). `SealUnit`/`OpenUnit` refuse any other kind (P2a-C5d).

**Never rewrite a unit in place (P2a-C1).** Every write goes to a NEW revision's name. If the new
plaintext digest equals `prev.Fixity` for that kind, the write is skipped and the old unit and its
Sealed entry are kept. Superseded and dropped units go on a pending-deletes list that runs only
after the manifest save succeeds, in the same keyed run under the lock; if the save fails, nothing
is deleted. The commit order is **units → history → index → manifest → deletions**.

**Per-kind rules on the encrypted path (P2a-C2)** — the plaintext branch at exporter.go:626 is
untouched (the guard is `e.Units != nil && prev.Unit != ""`):

| Kind | Written this run | Not written this run |
|---|---|---|
| zip | new rev; old rev queued for delete | nothing archivable (exporter.go:486, attach.go:111): drop the unit, clear its Sealed entry, queue the delete |
| eml | new rev; old rev queued | prev holds one: carry it BY UNIT with `prev.Fixity.EML` and `prev.Sealed`, whatever the logical stem |
| html | always written | — |

`carriedOriginal` on the encrypted path keys on the unit's Sealed entry and presence, never on
`prev.Path == relSlash`.

**Identity collapse (P2a-C7).** `CollapseByIdentity` is refused on an encrypted archive; the A12
conversion completes any owed collapse before it encrypts, and Load checks no token still owes it.

## 2. Three seams (parent amendment A16)

The parent's "one Engine-A FS seam" (§4.2) is replaced by three seams at the choke points the map
found (P2a-C23):

1. **`export.Units`** (2a-1) — `{Root, Key}` with `Write(unit, kind, plaintext)`, `Read`,
   `Remove`, `Present`, used at the content points only when `Exporter.Units != nil`: `writeDigest`
   (html, eml), `WriteZip` (zip), the per-kind table above, `carriedOriginal`/`sameArchivedEML`.
   Hardened (P2a-C5): `units/` and shard directories must be real directories judged without
   following links; each unit file must pass `util.DescriptorReadable` before it is opened; reads
   are capped at `Sealed.Size`; a linked shard directory is refused; after creating a shard
   directory `units/` is fsynced, and `out/` the first time `units/` is created — before any manifest
   save names a unit inside. Writes reuse `createTemp`/`commitTemp`. After each rename the unit's
   mtime is set to the run's start (P2a-C19).
2. **Containers** (2b) — manifest, index, history behind `commitInOrder`.
3. **Minimisation** (2a-2) — inside the state, seal and schedule writers.

`BeforeWrite` and the fault tests keep receiving the LOGICAL path.

**The replacement guarantee for "one seam"** is the structural tree test (§7 item 4): after every
encrypted run, refusal and failure path, every path under `out` matches an allow-list grammar.

**Zips in memory (2a-D5, owner ruling).** For an encrypted archive the zip is built in memory and
sealed before any byte reaches disk. A message whose attachments total more than **1 GiB** (owner:
higher than the 256 MiB proposed) is archived without them and recorded as a typed per-item
terminal gap, "attachments too large for an encrypted archive", disclosed in the catalog. Peak
memory for one message is about 2–3× its attachment set (zip buffer, sealed copy, the reader's
whole-unit open). Per-attachment units (unit id + index) are the follow-up that restores the
one-attachment-at-a-time bound.

**Digests (P2a-C6).** `Record.Fixity` stays the plaintext digest (reproducibility; 2c).
`Record.Sealed` is the KEYED check. The keyless tier-1 inventory A5 needs is **a cleartext file
2b writes beside the pointer** (parent amendment A17): a flat list of `(opaque name, ciphertext
SHA-256, size)` sorted by name, with no unit id, kind or grouping, regenerated at each commit; its
leak budget equals a directory listing.

## 3. No cleartext tree

In an encrypted archive Engine A never creates store/folder directories (exporter.go:386-390,
pages.go:167). One placement function is shared by the exporter, `localRun.probe` and the fault
classifier (localrun.go:144-159, :377, :410) and the Graph folder-fault key (graph.go:773); for an
encrypted archive the probe targets `units/` and a unit write fault is an item fault. Opaque names
never pass `util.ValidRecordedPath`; `export.Units` has its own validator.

**The keyed inventory sweep (P2a-C4, A5).** In a keyed run, under the lock, after a SUCCESSFUL
manifest load and before the first write, files under `units/` that neither the durable manifest's
Sealed entries nor the pending-deletes list name, and that are older than the run's start, are
removed. It never runs mid-run. Before any write, a keyed run refuses (typed) when the archive is
encrypted, `units/` is non-empty, and the manifest is missing, empty or has no records — the sweep
never runs against such a manifest. 2b carries the same rule for the pointer.

**Search rows ahead of the manifest (P2a-C22).** The index is flushed before the manifest; in an
encrypted archive the reader drops hits with no manifest record as "not yet committed".

## 4. Encrypted-run hygiene (2a-2)

Every row applies ONLY when the recognition predicate (§5) says encrypted; plaintext output is
byte-identical (P2a-C11).

| Leak | Ruling |
|---|---|
| `-copy-first` snapshot, `_outlook-pst` scratch in `-out` | **owner ruling (P2a-C12):** an app-owned scratch directory in the config dir, a free-space precheck with a typed refusal naming the remedy (drop `-copy-first`, or free space), swept at run start, removed on every exit, owned by uninstall; `snapshot()` takes a destination and a fault there is classified by its volume (MA-100/MA-320 amended for the encrypted case only) |
| `-manifest` outside `-out` | refused |
| folder pages, root `index.html`, `attachments-report.tsv` | not written; the Phase-3 reader renders them from the decrypted data; `StampedGaps` moves into the manifest — *2a-2 as built (MA-737): nothing moves; the stamp only caches when to regenerate pages and is a pure function of the manifest (`gapBannerCount`), so the Phase-3 reader recomputes it and an encrypted archive keeps no stamp* |
| `README.txt` | an encrypted variant ("encrypted — open with MailArchive"), no layout |
| run logs | location fixed where each logger is created (wizard, job runner, schedule descriptor, Desktop `-log`): the config dir; a `-log` at or under an encrypted `out` is refused; off-archive logs are a disclosed machine-local residual under R-E2 (P2a-C18) — no "redacting logger" |
| error text → sinks | a CLOSED list of run-failure kinds covering every return path of Run/RunGraph and the pre-run refusals (source file failed, source unreachable, fatal disk, lost lock, manifest save failed, partial run, Graph outage, index refusal, `encrypted_needs_key`, ...); for an encrypted archive the alert email, stderr and every sidecar are built from the kind alone, never from `err.Error()` (P2a-C16) |
| lastrun, runhistory, sourcestreak, lastverify, both attention `.txt` | explicit per-file field allow-lists no wider than frozen A6: time; ok/failed/running/cancelled; error kind; source-missing streak + since-date; auto-paused flag. Counts and durations move into the manifest (P2a-C16) |
| schedule descriptors, the Desktop capture's arguments | per-shape allow-lists (local job, Desktop M365 capture, verify): `-out`/`-job`/`-mode` and the fixed verbs; no Log, Exe, Wrapper, Host or profile paths |
| pause file | a fixed marker only; the typed reason moves to the config-dir archindex entry |
| `.mailarchive.lock` `host=`, descriptor Host | disclosed in the leak budget (A6 amendment A18) |
| config dir, secret store, token cache, `graph-config.json`, client-secret file at or under `out` | a REFUSAL on every path, scheduled included, surfaced through the A14 channel; `vault.Remember` refuses a store path inside an encrypted archive (P2a-C14) |
| a decrypted or working search index | never written under `out` (parent amendment A19, extending A13 to writers): in-memory (SQLite deserialize/memdb) or a config-dir scratch file with `journal_mode=MEMORY|OFF`, `temp_store=MEMORY`, named for the scratch sweep; 2b implements, the row lands now (P2a-C13) |
| unit size and write timing | disclosed beside R-E2: grouping, role and per-folder counts are inferable from size and order; mtimes normalised to the run start reduce the timing part (P2a-C19) |

## 5. Fail closed (2a-3)

**One recognition predicate (P2a-C8):** `archive.EncryptedMarker(out) (bool, why)` reports
encrypted if ANY of: either vault name present in any form (Lstat, not parse); `vault.Detect`
errored; the envelope says encrypted or version ≥ `EncryptedManifestVersion`; `units/` exists; any
record has a Unit; `.mailarchive-convert.json` present; (Phases 3–4) a recorded archive id exists for
the folder. Only the absence of every signal means plaintext. `ok=true` and the vault errors
(`ErrCorrupt`, `ErrNewerVault`, `ErrTampered`) map to the encrypted refusal; other I/O errors keep
today's handling. It is applied to every writable target and its parents: `-out` (including an
`-out` nested inside an encrypted archive), the Engine-B destination, `extract -dest`, the
`-manifest` folder. A meta-test fails if code outside the helper calls `vault.Detect`.

**Choke points, not verbs (P2a-C9).**
- A keyless guard `app.RefuseKeylessWrite(out, manifestPath)` beside `seal.RefuseWrite` and BEFORE
  input discovery, any run log, the Outlook `CreatePSTs`, `MkdirAll` or the lock, at: CLI
  `runExport`/`runGraph`, `RunHeadless`, `RunJobInProcess`, the wizard `runExport` (before its
  logger), `HeadlessCapture` (before `recordStartFailure`), Desktop `startCapture`, the CLI
  reindex/verify wrappers.
- The same check again inside `app.Run`, `RunGraph`, `Reindex`, `Rebuild`, `Verify`, `Extract`, and
  AGAIN right after `lockfile.AcquireAs` (closing the race with `PendingCreate.Commit` and the A12
  swap).
- With a key, under the lock: `vault.Open(out, recordedID or key.ArchiveID(), KeySecret(key))` must
  succeed; refuse on `ErrNoVault`, `ErrVaultMissing`, `ErrTampered`, `ErrAuth` or another archive's
  id, and refuse a manifest holding content records with no Unit (an existing plaintext archive —
  encrypting one goes only through A12).
- Consumers: reindex and rebuild (would mass-prune), verify, go-back, serve, search (and `-paths`),
  the desktop reader, copyexport archive-as-source, extract → refuse before any stat/lock/sweep/index
  open/manifest load; archindex → `IsArchive, kind="encrypted"`; health/status/Desktop maintenance →
  an encrypted branch reading only the minimised sidecars; Desktop Maintenance (verify, rebuild),
  the wizard health view (read-only lines only), Resume (unseal, but refuse to reinstall the schedule,
  saying why), Desktop/wizard/CLI schedule install and repair (refuse until Phase 4), scheduled
  reindex/verify jobs.

**Side effects (P2a-C10).** An interactive or manual refusal writes NOTHING (no log, lock, lastrun).
The unattended Engine-A refusal (A14) writes exactly: an A6-minimised lastrun (failed,
`encrypted_needs_key`, no Job/Error), one run-history entry, and the attention sidecar with
code-based wording — and its test asserts exactly that change. The wording is final-state, by case
(remembered key; ask-me mode; vault damaged or forked; vault missing with an id recorded — reusing
the gate's remedies; conversion marker present); it is first seen by users in the D5 build, and its
friction review is Phase 3's. Owner-facing text pointing at files an encrypted archive lacks
(capture.go:259, wizard.go:1517, refusals.go:142-144, health.go:1389) gets encrypted variants.

**Uninstall (owner ruling, P2a-C15, parent amendment A20).** Uninstall keeps an encrypted archive's
remembered key and its recorded-id home by default — archives are always kept, and the key is what
opens them unattended; removing the key is offered only as a separate, explicitly confirmed choice
that names the consequence.

## 6. Plaintext-visible changes (slice 2a-P, P2a-C11)

Each with prove-fail/prove-pass, its own adversarial pass and catalog amendments:
- **Snapshot sweep gap:** the snapshot KEEPS its source extension (the reader is chosen by
  extension, run.go:1213); `SweepOrphans` also reaps `.mailarchive-snapshot-*` older than the run's
  start (`snapshotPrefix` moves into `internal/export`). Twins: `-copy-first` over `testdata/support.pst`
  archives every message and leaves no snapshot; a stale snapshot is swept, a fresh one is not.
- **Sweep split:** `SweepOrphans` → `SweepTemps` + `SweepOrphanZips`; `RunGraph` calls only
  `SweepTemps`; an html-less zip on a Graph archive survives a run.
- **Manifest temp** into the sweep pattern via one shared constant, with MA-399's `leftoverTemps`
  and loadfrom globs updated (prove-fail recorded); the `-manifest`-outside-`out` temp is a disclosed
  plaintext residual.
- **Gate position:** the recognition gate in plaintext Run/RunGraph sits after `seal.RefuseWrite`,
  `RecordRefusedRun` and `RequireExistingOut`; "no signal" means today's bytes exactly — twins assert
  byte-identical lastrun, attention, runhistory and descriptor output and the MA-689 wording.

Amended rows: MA-69, MA-100, MA-320, MA-399, MA-689 and the N13 rows.

## 7. Tests

1. Every unit round-trips through `OpenUnit` to the plaintext a plaintext export writes; MA-329
   unchanged.
2. **The C2 property test (P2a-C17):** one fixture with a unique marker in subject, body, folder,
   sender, recipients, mailbox address, Message-ID, attachment name and bytes, store name and source
   path; scan file NAMES and contents under `out`, raw and as HTML entities, RFC 2047, UTF-16, JSON
   `\u` escapes and NFD.
3. The same scan over the captured alert body, stderr, log files and descriptors.
4. **The structural tree test:** after every encrypted run, refusal and failure path, every path
   under `out` matches the allow-list grammar (vault + `.bak`, envelope, lock, minimised sidecars,
   README, `units/[0-9a-f]{2}/[0-9a-f]{32}`, container files, swept temps).
5. The 2b-owned cleartext files (manifest, `search.db` + `-wal`/`-shm`, history) are an explicit
   named xfail encoder citing C2/R37 in items 2 and 4; a meta-test fails if the list grows or
   outlives 2b, and each 2b slice removes its entry.
6. Crash proofs: kill between the html write and the checkpoint → the next reader passes the digest
   check on every record; crash after N unit writes → the next start leaves exactly the recorded
   inventory; missing-manifest refusal (`assure.Refused`, bytes unchanged).
7. One test per per-kind table row, including "stem change without `-raw` keeps the original, and a
   sweep afterwards leaves it"; a local-path fill and a Full re-run keep Unit and every unit name; a
   `#fp`-split of a reused Message-ID gives two Units.

## 8. Slices

- **2a-0** vault: `UnitOriginal`, the kind guard, `(*Key).UnitName`, known-answer rows for kind 3's
  AAD and the name derivation.
- **2a-P** the plaintext-visible changes (§6).
- **2a-1** `export.Units`, `Record.Unit`/`Sealed`, revisions, per-kind rules, placement, in-memory
  zip with the 1 GiB ceiling, the keyed inventory sweep, the reachability meta-test.
- **2a-2** hygiene (§4).
- **2a-3** fail-closed (§5): the predicate, the guard and its call sites, consumer refusals and labels.

Each slice owes its adversarial pass; 2a-2/2a-3 wording owes the Phase-3 friction review.
