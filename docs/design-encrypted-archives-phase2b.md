# Encrypted archives — Phase 2b design

Status: **DRAFT 2026-10-25 — owes the pre-code design review (`../assurance-kit/process/design-review.md`)
before any code.** Parent: `docs/design-encrypted-archives.md` (A1–A20 govern; the parent changes this
doc forces are proposed here as A21–A25 and land in the parent only after the review). Sibling:
`docs/design-encrypted-archives-phase2a.md` (built, `fdb9c15`). Grounded in a read-only map of the
manifest, index, history, inventory, readers and tests (`wf_6af3aaff-e53`, six mappers and a critic)
at `fdb9c15`.

## 0. Scope

Phase 2a sealed message content under opaque names but left three files in cleartext beside it: the
manifest `.mailarchive-manifest.json`, the search index `search.db` (+ `-wal`/`-shm`/`-journal`) and
the go-back history `.mailarchive-history.jsonl` (the "2b list", MA-744). Phase 2b seals all three,
adds the keyless tier-1 inventory (A17), closes the deferred F12 gap (a restored older manifest makes
the unit sweep delete newer mail), and gives keyless `verify` its tier-1 work (A5). It adds no UX
(Phase 3) and no unattended path (Phase 4). Every keyed path stays unreachable by users (P2a-C20,
MA-726): the allow-list stays empty. Plaintext archives stay byte-identical (MA-329).

## 1. One commit point for all three containers (proposed A21)

The map found four incompatible commit models; the critic named this the riskiest choice. **Ruling:
one root pointer commits the manifest, the history and the index together, by one rename.**

- **`.mailarchive-manifest.json`** — the frozen A4 envelope, its `pointer` field always the fixed
  name `.mailarchive-manifest.ptr`. Written once, on the first commit, AFTER the first pointer is
  durable, so a visible envelope always names a durable pointer. Never rewritten by a backup.
- **`.mailarchive-manifest.ptr`** — the root pointer, the ONLY file whose rename commits:
  `"MAPF" ‖ file_format u16 ‖ gen u64 ‖ SealPointer(ContainerManifest, gen, plaintext)`. The
  cleartext `gen` is what keyless readers compare (§4); it is authenticated only through the pointer
  AAD (A2), so a keyed reader trusts it only after `OpenPointer` succeeds at that gen.
- **Pointer plaintext** (JSON, deterministic): `format`, `gen`, `run_start`, and three sections —
  `manifest` (layout: bucket count N, seen-chunk count S), `history`, `index` (page size, image
  length, chunk size) — each an ordered list of `{i, name, gen, sha256, size, psha256}`: chunk
  position, file name, the generation it was sealed at (A2 refined), ciphertext SHA-256 and size, and
  the plaintext SHA-256 used to decide what changed. Each section's chunks are sealed under its own
  `ContainerKind` (manifest 1, index 2, **history 3 — new**), so a chunk can never be read as another
  section's, another position's or another generation's.
- **`chunks/<hh>/<32 hex>`** — every container chunk of every kind, immutable, under a fresh random
  128-bit name the pointer records. ONE shared folder, separate from `units/`: the unit sweep can never
  touch a chunk (the manifest has no rebuild), and the folder does not say which chunks are history
  (only Microsoft 365 archives keep history, so a history folder would reveal the source kind, A6) or
  index (indexing on or off).

Why one pointer: it makes the index and the history unable to roll back or run ahead on their own,
turns today's history-before-manifest ordering (commitInOrder, run.go:902) into structure, removes the
"index ahead of the manifest" window P2a-C22 works around (the reader's hit-dropping stays as defence),
and leaves one F12 check instead of three. Cost: the pointer is rewritten every commit and grows with
the chunk count (about 120 bytes per chunk: ~0.5 MB for a 1 GB index) — O(chunks), not O(delta),
disclosed.

**Commit order** (the encrypted branch of `commitInOrder`; the plaintext branch is untouched): units
(already durable, 2a) → stage history chunks → stage index chunks → stage manifest chunks (each new
chunk: temp, fsync, rename, mtime = run start; then each touched shard folder and `chunks/` fsynced) →
`lock.StillHeld()` → pointer temp `.mailarchive-ptr-*.tmp`, fsync, `util.ReplaceRetrying`, folder
fsync **[COMMIT]** → the inventory (§3) → deletes: superseded units (`flushDeletes`) and superseded
chunks. Any staging failure before the rename leaves the old pointer naming old, intact chunks; the new
files are orphans for the next keyed sweep (§2). A failed folder fsync after the rename counts as a
failed save: nothing is deleted, and the next save retries. Lost lock before the rename: nothing is
renamed.

**When a section cannot be staged.** *Index* (serialize or seal fails): the pointer commits with the
OLD index section and the manifest header records `index_behind: {since: gen}`; the run reports the
typed `index` failure kind; only a keyed Rebuild (§6) clears it — incremental runs would otherwise skip
the messages whose rows were never sealed and the gap would be permanent and invisible. *History*:
buffered events stay buffered for the next commit; if the run ends first they are lost and the header
records `history_gap: {from, to}` so go-back says partial. *Manifest*: the commit fails.

## 2. The sealed manifest (slice 2b-1)

**Seam.** `state` gains a `Persister` interface (`Load`, `Save`, `OnSaved`); the plaintext persister
is today's code, byte-identical. The sealed persister lives in a new package `internal/container`
(`state` must not import `vault`: vault's in-package tests import `state`). `parseManifest` splits into
decode and finish so both persisters share every post-parse check (unit uniqueness, Sealed entries,
P2a-C3/C5). `openEncrypted` (encrypted.go:131) calls `container.LoadManifest(out, key)` and NEVER falls
back to `state.Load`.

**Strict keyed load.** The envelope must parse with unknown fields refused, `version ==
EncryptedManifestVersion`, `encrypted: true`, `archive_id ==` the key's id and `pointer ==` the fixed
name; anything else — including plain manifest JSON, i.e. a 2a-shaped test archive or a planted
downgrade — is a typed refusal, never a migration (2a never shipped). Envelope absent + pointer
present: authenticate the pointer, then write the envelope. Both absent + `units/` or `chunks/`
non-empty: refuse (the 2a missing-manifest rule, now for the pointer). All absent and both folders
empty: a fresh archive, gen 0.

**Chunking for O(delta) (A2 note).** Sections: `0` header — version, stores, collapsed tokens, the
LastSeen stamp table, `index_behind`, `history_gap`, and **`run_details`**: the full plaintext-shape
LastRun of the last run (job flags, source kind, counts, durations — A6 says these "move into the
manifest"; 2a dropped them, this is their home), plus a reserved `pause_note` (owner ruling
2026-10-25: Phase 3 stores the pause note here; MA-662 stays folder-only); `1` capture gaps; `2..N+1`
record buckets; then S "seen" chunks.
- A record's bucket is `(*Key).Bucket(recordKey, N)`: the first 8 bytes of HMAC-SHA256 under a new
  sub-key `HKDF(master, "mailarchive manifest buckets v1")`, mod N. Keyed, so someone who knows a
  message's key cannot watch its chunk change. N is a power of two, at least 16, doubled (one full
  re-seal) when the mean bucket passes 2 MiB of plaintext; the 1 MiB target is an estimate, measured
  in the slice.
- **LastSeen lives in the seen chunks, not the buckets**: Microsoft 365 captures stamp LastSeen on
  every observed record every run (exporter.go:343, :384; graph.go:803, :825), so with it in the
  buckets every scheduled run would re-seal the whole manifest. Seen chunk j holds, for buckets
  16j..16j+15 in key order, a varint index into the header's stamp table.
- At save every section is re-encoded (sorted keys); only sections whose plaintext SHA-256 differs from
  the pointer's `psha256` are re-sealed, at the new gen, under a new name. No dirty tracking, so none
  of the ~19 places that write `m.Entries` can be missed.

**The keyed sweep (P2a-C4, extended).** After a successful load, under the lock, before the first
write: a file under `units/` or `chunks/` is removed only when the loaded manifest (or pointer) does
NOT name it, the inventory (§3) does NOT name it, and its mtime is older than the run's start
(units.go:294's clock rule). With no inventory at all, nothing is swept that run (fail-safe), and the
commit writes one.

## 3. The inventory and F12 (proposed A22)

**Format (A17, sharded for O(delta)).** `.mailarchive-inventory`: `mailarchive-inventory 1`, then
`gen <G>`, `pointer <sha256> <size>`, then one line per non-empty shard `<hh> <sha256> <entries>`.
`inventory/<hh>`: sorted lines `<path> <sha256> <size>` for every file under `units/<hh>/` and
`chunks/<hh>/` that pointer G names. Built from the manifest's Sealed entries and the pointer's chunk
list being committed — never from a folder listing — so an in-flight or failed file never appears. No
unit id, kind or grouping; leak budget = a directory listing plus digests. Only changed shards are
rewritten (each atomic), then the top file (atomic) — the top file is the inventory's commit point. A
single-file inventory would be ~11 MB per commit at 100,000 units: that breaks the O(delta) promise
on an hourly Microsoft 365 schedule.

**Written every commit, BEFORE deletes.** Deletes of commit G run only after inventory G is durable, so
everything any durable inventory lists still exists unless it was really lost — keyless verify never
reports a false "missing" (the critic's order contradiction, settled).

**The F12 rule** — I is the inventory's gen, P the pointer's:

| State | Meaning | Keyed run |
|---|---|---|
| I = P | normal | proceed; sweep |
| I < P | a crash or failed write between the pointer and the inventory | proceed; the sweep keeps anything either names |
| I > P | an older pointer was restored over a newer archive | **refuse**, typed, nothing written or deleted |
| no inventory | first commit crashed, or it was deleted | proceed; no sweep this run |

"Keep and continue" is unsafe and not offered: `sealKind` uses `prevSealed.Rev+1`
(exporter.go:908-911) and `Units.Write` renames over an existing file, so the run would overwrite the
lost newer revision. Refusal wording (final-state, friction review in Phase 3): *"This archive's
record is older than its file list — an older copy of it may have been restored. Restore the newest
copy of the whole archive folder; nothing was changed."* A deliberate "adopt the older record and
discard N newer files" is an owner product question for Phase 3, not offered in 2b. The corrupt-record
remedy (manifest.go:713, "restore it from a backup") gets the same encrypted wording.

**Residual, stated:** restoring the WHOLE folder over a newer one by merging (copy without delete)
brings back a matching older inventory with the older pointer; the sweep would then remove the newer
units. In-archive data cannot tell these apart. The engine therefore takes **`Options.RecordedGen`**,
the highest generation this machine committed, kept outside the archive beside the A15 recorded id,
and refuses when it exceeds P. 2b builds and tests the check; Phase 3–4 persist it (in a per-archive
record in the settings folder — not the archive index, which stays folder-only by the 2026-10-25
ruling). Until then the residual stands, and no keyed run is reachable.

## 4. Keyless tier-1 verify (slice 2b-4, A5)

`app.Verify` gains an encrypted branch that reads only the inventory, the pointer's cleartext header
and the files themselves: each listed file is judged with `Lstat` (a regular file, no link), its size,
then its SHA-256 (reading at most size+1 bytes); files under `units/` or `chunks/` that are not listed
are "unexpected" (temps excepted); `-record` is refused. Outcomes: **intact** (I = P, all match);
**not attested** (I < P: listed files are still checked, newer ones cannot be); **damaged** (a listed
file missing, changed or truncated, or the pointer's digest wrong at I = P); **restored-older** (I >
P). The CLI and Desktop "Check integrity" get it through `app.Verify`. The last-verify record keeps
only time + `attested`/`intact` flags (A18; no counts — the envelope has none and a count of sealed
files is not a count of messages); `ARCHIVE-INTEGRITY-ATTENTION.txt` gets fixed wording; status shows
RED on damaged or restored-older. Tier 2 (decrypt + cross-check) is keyed, Phase 3. `status --json`
reports an encrypted archive's `History` as `null`, not `{Exists:false}`.

## 5. The sealed history (slice 2b-2)

Folded into the root pointer as kind-3 chunks; the chunk plaintext is today's JSON lines, unchanged, so
one parser serves both. A `state.Timeline` interface that `*HistoryWriter` already satisfies keeps
plaintext byte-identical. The sealed timeline buffers events; each commit re-seals the open LAST chunk
as a new file (never in place) until it holds 256 KiB of plaintext, then starts a new one; earlier
chunks are never re-sealed. Loading the manifest never opens history chunks; go-back opens them
lazily, and a chunk that fails authentication makes go-back "partial" and never blocks a backup
(today's "history damage only weakens the timeline", graph.go:220-221). Keyed reindex redaction, when
it comes (Phase 3), re-seals the affected chunks in the same commit as the manifest, which also closes
the plaintext redaction window (reindex.go:202-209) for encrypted archives. The A12 conversion parses
the plaintext `.jsonl` into chunks and counts dropped bad lines into the header.

## 6. The sealed index (slice 2b-3)

**Image, not rows.** The index stays one SQLite database held in memory, sealed as fixed POSITIONAL
256 KiB chunks (64 pages of 4096 bytes). Positional chunks are stable for SQLite (it never shifts
existing pages; new pages go at the end or into freed pages), so the A2 note's warning applies to the
manifest's JSON, not here. `page_size=4096`, `auto_vacuum=NONE`, never `VACUUM` on the sealed image.
At each commit, if `total_changes()` moved since the last seal, the image is taken and only chunks
whose plaintext digest changed (or are new) are re-sealed.

**Load route (amends A19's "deserialize").** The pinned `modernc.org/sqlite` v1.38.2's `Deserialize`
is unsafe (sqlite.go:1813/1822; fixed upstream later — confirmed by the critic). So: decrypt every
chunk, check it against the pointer, register a read-only in-memory VFS over the image, and
`NewRestore` it into a private `:memory:` connection; then `journal_mode=MEMORY` (**not** `OFF`: Add's
per-row savepoints (F49) need `ROLLBACK TO` — A19's `MEMORY|OFF` is amended to `MEMORY`),
`temp_store=MEMORY`, `mmap_size=0`; unregister the VFS and zero the buffer. **The slice opens with a
spike test proving this route on v1.38.2** (restore a fixture through the VFS, run a MATCH); if it
fails, the fallback is the scratch file below for every size. An upgrade to a fixed modernc is a
separate, operator-gated change.

**Big indexes.** Above a 512 MiB image (an estimate, measured in the slice), the working index is a
scratch file in the per-user CACHE folder (`job.ScratchDir`'s root, `index/<hash of out>` — the same
reason as 2a's scratch copies: on Windows the settings folder roams), same pragmas, swept at run start,
removed on every exit, added to `secretDirs` and to uninstall's `ScratchTargets`. It is plaintext on
local disk while a run is going — a disclosed residual under R-E2.

**No index write after the last seal.** `RepairKeys` (run.go:641) runs before the walk and is sealed
by the first commit; `finish`'s `indexCount` must not commit a batch on the encrypted path; `Close`
never seals. A test-build check fails a run whose `total_changes()` at Close differs from the last
sealed value.

**Keyed Rebuild** is built in 2b at engine level — the only remedy for a damaged index section or
`index_behind` — reading content through `Units.Read`, building in memory or the scratch file, never
under `out`, committing through the manifest save at gen+1. Unreachable by users (MA-726). Keyed
Reindex (prune) waits for Phase 3.

## 7. Cross-cutting rules

- **Reads** of chunks, the pointer and inventory files use the unit store's rule: `Lstat`, refuse
  links and special files, `util.DescriptorReadable` (accepts a Windows cloud placeholder), read
  capped at the recorded size (pointer cap 64 MiB; inventory caps by line count).
- **mtimes** of chunks, the pointer and inventory files are set to the run's start (P2a-C19), so file
  times reveal run starts only, as with units.
- **Windows:** pointer and inventory replaces use `util.ReplaceRetrying`; a superseded chunk whose
  delete hits a sharing violation is left for the next sweep, never an error.
- **Recognition predicate** (MA-727): the pointer and inventory names (any form, Lstat) and a
  `chunks/` folder holding a 2-hex shard with a matching 32-hex file are encrypted signals.
- **Tree grammar** (MA-743) gains exactly: `.mailarchive-manifest.ptr`, `.mailarchive-inventory`,
  `inventory/[0-9a-f]{2}`, `chunks/[0-9a-f]{2}/[0-9a-f]{32}` (temps already match `.mailarchive-*.tmp`).
  This was anticipated by 2a §7 item 4 ("container files"); it is an allow-list change the review must
  confirm, not one self-granted.
- **Leak budget addition (R-E2 note):** the number and sizes of chunk files (roughly the index and
  history sizes) and the last history chunk's growth per commit are visible, as unit sizes already are.

## 8. Tests

Every container slice proves, test-first with prove-fail → prove-pass:
1. **Positive twin first:** a keyed run then a keyed load gives exactly the records / events / rows a
   plaintext run of the same mail writes; plaintext bytes unchanged (MA-329).
2. **Crash proofs** at every step of §1's order (a kill-at-named-step seam like copyexport's): after
   each, the next keyed run opens the last committed gen, every record's units open, the tree grammar
   passes, and only files older than the run start that nothing names are swept.
3. **Rollback:** the previous pointer swapped back while its chunks remain → refused (I > P), nothing
   removed; with the old chunks already gone → a typed missing-chunk refusal. The F12 xfail encoder
   (MA-747, R37) lands FIRST in 2b-1 and becomes a permanent guard.
4. **Tamper:** one byte flipped in a chunk, the pointer, or the pointer's cleartext gen; a chunk
   swapped with another of the same kind, moved to another position or kind, or from an older gen; an
   envelope naming another archive; plain JSON at the envelope's name → each refused through
   `assure.Refused` (archive named, remedy named, nothing changed or swept).
5. **Changed-only writes**, via a write-counting seam: one new message re-seals at most the header,
   one bucket and one seen chunk, the same count at 200 and 2,000 messages; a Microsoft 365 re-run of
   unchanged mail re-seals no bucket; history re-seals one chunk per commit; the index re-seals a
   measured, pinned bound, and zero chunks on a no-change run; the inventory rewrites only touched
   shards.
6. **Tree grammar + C2 leak scan** (MA-742/743) at every observation, the slice's 2b-list entry
   removed in the same commit; the decrypted scratch index is never under `out` at any observation.
7. **Inventory:** byte-equal to one rebuilt from a listing plus digests; every F12 table row; keyless
   verify's four outcomes.

## 9. Slices

- **2b-0** vault + codecs: `ContainerHistory = 3`; a kind check on `SealChunk`/`OpenChunk`/
  `SealPointer`/`OpenPointer`; `(*Key).Bucket` and its sub-key; known-answer rows (MA-704/716 amended);
  in `internal/container` the pointer-file and inventory codecs (pure).
- **2b-1** sealed manifest + inventory + F12 + strict load + sweep + `RecordedGen` + predicate + grammar.
  Removes `.mailarchive-manifest.json` from the 2b list. Many 2a tests that read the plaintext
  manifest move to `container.LoadManifest`.
- **2b-2** history. Removes `.mailarchive-history.jsonl`.
- **2b-3** index (spike first), `index_behind`, keyed Rebuild, cache-folder scratch. Removes `search.db`
  and its `-wal`/`-shm`/`-journal`.
- **2b-4** keyless tier-1 verify, last-verify allow-list, status (MA-728 amended).

New catalog rows are allocated as each slice lands (from MA-749). Rows amended: MA-704, MA-716,
MA-712, MA-725, MA-727, MA-728, MA-742, MA-743, MA-744 (the list empties; the encoder stays as a guard
that it stays empty — no gate is lowered), MA-747 (its known-gap sentence goes when F12 closes).
Each slice owes its adversarial pass; the refusal and verify wording owes Phase 3's friction review.

## 10. Proposed parent amendments (land after the review)

- **A21** — one root pointer commits manifest, history and index (§1); `ContainerHistory = 3`; the
  pointer plaintext lists sections of `{i, name, gen, sha256, size, psha256}` (refines A2's "file name
  and ciphertext SHA-256, and the chunk count"); A11's "one file, inside the A4 envelope" reads "one
  file the A4 envelope names".
- **A22** — the inventory is a top file plus shards, written every commit before deletes, and is the
  F12 anchor (§3); A7's "fixity/inventory anchor … in the manifest envelope/container" reads "in the
  inventory beside the pointer".
- **A23** — A19: the index load route is restore-from-a-read-only-VFS on the pinned SQLite;
  `journal_mode=MEMORY` only; the writer scratch is the cache folder.
- **A24** — A6's moved fields live in the sealed header's `run_details`.
- **A25** — the highest committed generation is recorded outside the archive beside the A15 id
  (Phase 3–4), and every keyed write path takes it.
- Housekeeping: A4's line references are now manifest.go:723 (refuse-newer) and :712-713 (parse).
