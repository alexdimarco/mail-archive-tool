# Encrypted archives — Phase 2b design

Status: **reviewed 2026-10-25 — pre-code review GO_WITH_CONDITIONS
(`docs/review-encrypted-archives-phase2b.md`); all 24 conditions (P2b-C1..C24) are folded in below.**
Parent: `docs/design-encrypted-archives.md` (A1–A20 govern; the parent changes this doc forces are
A21–A25, §10, recorded in the parent with this revision). Sibling:
`docs/design-encrypted-archives-phase2a.md` (built, `fdb9c15`). Grounded in a read-only map of the
manifest, index, history, inventory, readers and tests (`wf_6af3aaff-e53`) and the review's SQLite
spike (`wf_6be7d577-b8d`) at `fdb9c15`.

## 0. Scope

Phase 2a sealed message content under opaque names but left three files in cleartext beside it: the
manifest `.mailarchive-manifest.json`, the search index `search.db` (+ `-wal`/`-shm`/`-journal`) and
the go-back history `.mailarchive-history.jsonl` (the "2b list", MA-744). Phase 2b seals all three,
adds the keyless tier-1 inventory (A17), **narrows** the deferred F12 gap (a restored older record
makes the unit sweep delete newer mail) to one residual that Phase 3–4 close (§3, P2b-C21), and gives
keyless `verify` its tier-1 work (A5). It adds no UX (Phase 3) and no unattended path (Phase 4). Every
keyed path stays unreachable by users (P2a-C20, MA-726): the allow-list stays empty. Plaintext archives
stay byte-identical (MA-329).

## 1. One commit point for all three containers (A21)

**One root pointer commits the manifest, the history and the index together, by one rename.**

- **`.mailarchive-manifest.json`** — the frozen A4 envelope, its `pointer` field always the fixed
  name `.mailarchive-manifest.ptr`. Written by the first commit (P2b-C3) and never rewritten by a
  backup; healed only inside a commit, after every load check has passed (P2b-C4).
- **`.mailarchive-manifest.ptr`** — the root pointer, the ONLY file whose rename commits:
  `"MAPF" ‖ file_format u16 ‖ gen u64 ‖ SealPointer(ContainerManifest, gen, plaintext)`.
  `file_format` equals the AAD `format_version` (A2), so it is authenticated; the cleartext `gen` is
  what keyless readers compare (§4) and a keyed reader trusts it only after `OpenPointer` succeeds at
  that gen. **`.mailarchive-manifest.ptr.bak`** mirrors it, written after the primary and before the
  inventory; a keyed load uses the highest generation that authenticates and heals the other copy at
  its next commit, as the vault does (P2b-C4).
- **The mirror's write** failing after the primary landed is logged and counted as not attested (the
  inventory records which copies it saw, §3); it is not a failed save — the primary is the commit —
  and the inventory is still written. The next commit rewrites both.
- **Pointer plaintext**: a small JSON head (`format`, `gen`, `run_start`, `inv` — the generation of the
  last inventory this writer saw become durable (P2b-C7) — the manifest layout, the index meta version
  and image facts) followed by three binary entry tables, `manifest`, `history`, `index`, of 64-byte
  entries `{i u32, name [16]byte, gen u64, sha256 [32]byte, size u32}`: chunk position, file name, the
  generation it was sealed at (A2 refined), ciphertext digest and size. A pointer for a 1 GB index is
  about 0.25 MB at 256 KiB chunks — an estimate (P2b-C23). Each section's chunks are sealed under its
  own `ContainerKind` (manifest 1, index 2, **history 3 — new**), so a chunk can never be read as
  another section's, another position's or another generation's.
- **`chunks/<hh>/<32 hex>`** — every container chunk of every kind, immutable, under a fresh random
  128-bit name the pointer records; ONE shared folder, separate from `units/`, so the unit sweep can
  never touch a chunk. File NAMES say nothing about kind; SIZES do — see the leak budget (§7, P2b-C20).

Why one pointer: the index and the history cannot roll back or run ahead on their own; today's
history-before-manifest ordering (commitInOrder, run.go:902) becomes structure; the "index ahead of the
manifest" window P2a-C22 works around goes away (the reader's hit-dropping stays as defence); one F12
check instead of three. Cost: the pointer and its mirror are rewritten every commit, O(chunks).

**The seam (P2b-C1).** One entry point, `container.Commit(manifest, timeline, indexSource)`, returns
`(manifestErr, indexStageErr, historyStageErr)`. The encrypted branch of `commitInOrder` calls it in
place of the three callbacks. `Persister.Save` on an encrypted manifest delegates to it through
registered timeline and index sources and refuses when they are not registered. `internal/index` gains
an in-memory open and an image accessor; on the sealed path `Timeline.Sync` means "stage", not fsync.
The other direct `Save` callers are already refused on the encrypted path (collapse, graph.go:319;
`verify -record`, verify.go:273) or are Phase 3 (reindex.go:189, :449). Keyed Rebuild uses `Commit`.

**Commit order** (the plaintext branch is untouched): units (already durable, 2a) → stage history
chunks → stage index chunks → stage manifest chunks, each twice (P2b-C5) (each new chunk: temp, fsync,
rename, mtime per P2b-C10; the touched shard folders and `chunks/` fsynced) → `lock.StillHeld()` →
pointer temp `.mailarchive-ptr-*.tmp`, fsync, `util.ReplaceRetrying`, folder fsync **[COMMIT]** → the
mirror → the inventory (§3). **Nothing is deleted during a run** (P2b-C9). The durability rule is
today's (P2b-C2): a failed temp write, file fsync or rename fails the save; a folder fsync is best
effort, and "not supported" (Windows folder handles, EINVAL/ENOTSUP) is not a failure; once the rename
has returned, the save is recorded, and the generation moves forward after every rename attempt and is
never reused *within a run* (*amended, §11 CON-7: across runs a lost attempt's generation can recur; the
§11 OUT-2 check refuses a file list that does not name the pointer on disk*). A staging failure before the rename leaves the old pointer naming old, intact chunks;
the new files are orphans for the next run's sweep. Lost lock before the rename: nothing is renamed.

**When a section cannot be staged.** *Index*: the pointer commits with the OLD index section, the
header records `index_behind: {since: gen}`, and the run reports the typed `index` kind (P2b-C16).
*History*: the buffered events stay buffered and the SAME commit's header records `history_gap: {from:
gen}`, cleared only when the events land (P2b-C14). *Manifest*: the commit fails.

**The first commit (P2b-C3).** On a fresh archive, under the lock and before the first unit write, the
first keyed run writes the envelope FIRST, then commits an empty generation-1 pointer, its mirror and
inventory 1. So an older MailArchive meets the A4 envelope before any pointer or unit exists and
refuses (manifest.go:723) instead of exporting plaintext into the archive; a crash inside the first
commit leaves "envelope, no pointers, empty folders", which resumes the first commit; a first run
killed before its first checkpoint is an ordinary I = P = 1 state whose orphans the next sweep
removes; and "files present, no pointer" can only mean loss. Phase-3 condition: vault creation writes
the envelope and this commit immediately.

## 2. The sealed manifest (slice 2b-1)

**Seam.** `state` gains a `Persister` interface (`Load`, `Save`, `OnSaved`); the plaintext persister is
today's code, byte-identical. The sealed persister lives in `internal/container` (`state` must not
import `vault`: vault's in-package tests import `state`). `parseManifest` splits into decode and finish
so both persisters share every post-parse check (unit uniqueness, Sealed entries, P2a-C3/C5).
`openEncrypted` (encrypted.go:131) calls `container.LoadManifest(out, key)` and NEVER falls back to
`state.Load`.

**Strict keyed load (P2b-C4, C11).** In order: the envelope's `version` first (newer → the typed
"upgrade" refusal); then unknown fields refused, `encrypted: true`, `archive_id ==` the key's id,
`pointer ==` the fixed name; anything else at that name — plain manifest JSON, i.e. a 2a-shaped test
archive or a planted downgrade — is a typed refusal, never a migration (2a never shipped). Then the
pointer pair: each copy's `file_format` checked first (newer → "upgrade"), then authenticated; the
highest authenticating generation wins. Then the header's own version, under the same newer-format
gate. Then the F12 checks (§3). Only then may the commit heal a missing or unparseable envelope, or the
lagging pointer copy. Rows:

| State | Result |
|---|---|
| everything absent, `units/` and `chunks/` empty | fresh archive: the first commit (§1) |
| envelope present, both pointers absent, `units/`, `chunks/` and `inventory/` empty | a first commit that crashed: resume it |
| envelope present, both pointers absent, any of those folders not empty | refuse, typed: the record's pointer is missing |
| pointers present, envelope absent or unreadable | heal it inside the next commit, after the checks |
| files present, no envelope and no pointer | refuse, typed (loss; P2b-C3 makes this unreachable by crash) |
| one pointer copy fails, the other authenticates | load the good one; heal at the next commit |
| both pointer copies fail | refuse, typed: damaged record, naming both files |
| a chunk the pointer names fails, its mirror opens | load the mirror; the next commit re-seals that section |
| both copies of a manifest chunk fail | refuse, typed, naming the chunk file and its date — the damaged file's mtime, which is its sealing run's start (§7): "restore this one file from any backup made after <date>" (chunks never change after they are written); if both copies are missing, "from your newest backup" |

**Every manifest chunk is written twice (P2b-C5).** In an encrypted archive the manifest is the only
key to the content — a unit's name and AAD come from its record — so a damaged bucket would make its
messages unreadable even though their units are intact. Header, gaps and buckets are each sealed as two
chunk files (two entries in the manifest table, same position); load takes whichever authenticates.
This doubles the manifest's per-commit bytes. History and index are not mirrored: history damage only
weakens the timeline, and the index is rebuildable.

**Chunking for O(delta) (A2 note, P2b-C12).** Sections: `0` header — version, stores, collapsed tokens,
the per-mailbox `seen_at`, `index_behind`, `history_gap`, **`run_details`** (P2b-C13), a reserved `pause_note`
(owner ruling 2026-10-25: Phase 3 stores the pause note here; MA-662 stays folder-only); `1` capture
gaps; `2..N+1` record buckets.
- A record's bucket is `(*Key).Bucket(recordKey, N)`: the first 8 bytes of HMAC-SHA256 under a new
  sub-key `HKDF(master, "mailarchive manifest buckets v1")`, mod N. Keyed, so someone who knows a
  message's key cannot watch its chunk change. N is a power of two, at least 16, doubled (one full
  re-seal) when the mean bucket passes 256 KiB of plaintext, so the mean stays near 128 KiB — both
  figures estimates, measured in the slice.
- **LastSeen is encoded relative to the header.** Microsoft 365 captures stamp LastSeen with the run's
  stamp (`runAt`, graph.go:218) on every observed record every run (manifest.go:1517, :1572), while a
  newly exported record's LastSeen is its own export time (exporter.go:661, :725), so an absolute stamp
  would change every bucket every run. The header keeps, per mailbox (the scope one walk covers),
  `seen_at`: the run stamp of that mailbox's last FINISHED walk, moved forward only by the commit that
  ends the walk (graph.go:418). A record stores `latest` when its LastSeen equals its mailbox's
  `seen_at` or this run's stamp (mid-walk both decode to `seen_at`), and otherwise its time (a
  capture time or an older walk's — a time that changes only when the record does; *as built, 2b-1:
  the `captured` marker the fold verifier proposed is not needed for this and was dropped*). A steady
  scheduled run then changes only the header and, once each, the buckets of records that newly went
  unseen or were newly restamped after their capture. Losing a mid-walk
  advance on a crash decodes those records as last seen at the previous finished walk, or at their first
  sighting when that is later (*amended, §11 FID-4*) — never later than the truth and never before they
  were first seen, which is all gone detection reads (`LastSeen.Before(thisRun)`, sweep.go:71); proven at
  the record level (MA-755), not by an end-to-end gone run. Local records
  carry no LastSeen (omitempty), so they cost nothing here.
- **The bytes model, stated:** k changed records touch about N(1 − e^(−k/N)) buckets, so one commit
  re-seals about 2 × 128 KiB per changed record while k ≪ N, saturating toward twice the manifest as k
  approaches N (a first import). A Microsoft 365 run makes about 8 checkpoint commits, plus one per
  mailbox and a final one (graph.go:386-395, :418, :491); a no-change commit re-seals the header pair
  only. Estimates; test 5 measures them.
- At save every section is re-encoded (sorted keys) and compared with the plaintext digests computed at
  load or at the last commit; only changed sections, and any section loaded from its second copy, are
re-sealed (both copies), at the new gen, under new names. A format change re-seals every chunk at its
first commit (P2b-C11). No
  dirty tracking, so none of the ~19 places that write `m.Entries` can be missed.

**`run_details` (P2b-C13, A24).** The header carries THIS run's job flags, source kind and per-run
counts as of the run's final commit; the outcome itself stays in the minimised cleartext lastrun
(A18). A run that never commits leaves the previous run's details in place.

## 3. The inventory and F12 (A22)

**Format (A17).** `.mailarchive-inventory` (the top file): `mailarchive-inventory 1`, `gen <G>`,
`pointer <sha256> <size>`, `mirror <sha256> <size>` (or `mirror none` when its write failed),
`fanout <d>` (d from 2 to 4), then one line per non-empty shard `<prefix> <file name>
<sha256> <entries>`. A shard `inventory/<prefix>.<gen>` holds sorted lines `<path> <sha256> <size>
<state>` for every file under `units/` and `chunks/` whose name starts with that prefix, `state` being
`live` (pointer G names it) or `retained` (superseded during the run that wrote G, deleted by the next
run's start sweep, P2b-C9). Built from the manifest's Sealed entries and the pointer's chunk tables —
never from a folder listing — so an in-flight or failed file never appears. No unit id, kind or
grouping; leak budget = a directory listing plus digests.
- **Shards are immutable (P2b-C6):** a changed shard is written under a new generation-named file; the
  top file names it; the top file's atomic replace is the inventory's commit point; superseded shard
  files are deleted by the next run's start sweep. A crash leaves the G−1 listing intact.
- **Fan-out grows (P2b-C6):** `d` hex digits of prefix (2 to start), raised by one — a full rewrite —
  when the mean shard passes 512 entries, so a commit touching k files rewrites about min(k, 16^d)
  shards of at most ~1,000 lines each (~110 KB). Estimates; test 5 measures the bytes at 100,000 and
  1,000,000 files.

**Written every commit, after the pointer and its mirror.** A failed inventory write is a typed run
failure, a new kind `inventory_failed` added to the closed A18 list through the catalog ("this
backup's file list could not be saved; the next backup retries" — not `manifest_save_failed`, whose
"progress could not be saved" would be false: the commit landed); the run shows RED and the next
commit retries (P2b-C7). A newer inventory version (`mailarchive-inventory 2`) is the typed "upgrade"
refusal, checked before I is read (P2b-C11).

**A damaged inventory.** When the top file parses, its gen equals P and its pointer digest matches,
but a shard is missing or fails its digest (bit rot in one cleartext file), the run proceeds with no
sweep and rewrites every shard at its commit; any other unparseable or self-inconsistent inventory
counts as "no inventory" below, refused with its own wording: "this archive's file list is damaged". Writers enforce
the same size caps readers enforce, before the rename, and fail the commit typed if they would be
exceeded (P2b-C18).

**The F12 rule** — I is the inventory's gen (absent, unparseable or self-inconsistent counts as "no
inventory"), P the loaded pointer's gen, `inv` the pointer's authenticated last-durable-inventory gen:

| State | Meaning | Keyed run |
|---|---|---|
| I = P | normal | proceed; sweep |
| I < P, I ≥ inv | the inventory write after a commit failed or crashed (already reported RED) | proceed; the sweep keeps anything either names |
| I > P | an older record was restored over a newer archive — or, at I = P + 1 only, the last save's pointer rename was lost (a non-journalled file system's power cut) | **refuse**, typed, nothing written or deleted; at I = P + 1 the wording names both causes |
| I < inv | an inventory older than one this archive already saw become durable was restored | **refuse**, typed as I > P |
| no inventory, inv = 0 | the first commit's inventory never landed (reported) | proceed; no sweep this run |
| no inventory, inv ≥ 1 | an inventory that existed was lost or damaged | **refuse**, typed: "this archive's file list is missing or damaged" |

Defence in depth (P2b-C7): `Units.Write` checks, under the lock, that a new revision's target name is
absent, and refuses typed as restored-older when it is present — an existing `rev+1` file is keyed
evidence of an older manifest, so a lost newer revision is never overwritten. "Keep and continue" is
not offered: `sealKind` uses `prevSealed.Rev+1` (exporter.go:908-911).

**The sweep (P2a-C4, extended; P2b-C9).** After a successful load and the F12 checks, under the lock,
before the first write: a file under `units/` or `chunks/` is removed only when the loaded pointer
does not name it, the inventory does not list it as `live`, and its mtime is older than the run's
start (units.go:294's rule; skew errs toward keeping) — *and (amended, §11 CRIT-3b) only when the
inventory lists it `retained` or a run since the record's last commit wrote it (mtime at or after that
commit's run start): an older unnamed file came from elsewhere and is kept*; a file under `inventory/` is
removed only when the top file does not name it and its mtime is older than the run's start. `retained` files, superseded
shard files and orphans of a crashed commit go here, so a file-by-file copy taken between runs always
holds a pointer whose chunks all exist. Disclosed: a copy taken DURING a run is guaranteed loadable
only from a file-system snapshot; disk cost is one run's superseded files.

**RecordedGen (P2b-C8, A25).** The engine takes `Options.RecordedGen`, the highest generation this
machine committed, kept outside the archive beside the A15 recorded id and written only after the
pointer rename returns. When RecordedGen > P: refuse only when `units/` or `chunks/` holds a file that
neither pointer P nor inventory I names — the signature of a merge-restore (copy without delete), the
one case in-archive data cannot tell from normal. With no such file (a clean whole-folder restore, a
rotated drive) the run proceeds, says "this archive was restored to an older copy", and lowers
RecordedGen to P. 2b builds and tests the check; Phase 3–4 persist it (a per-archive record in the
settings folder — not the archive index, which stays folder-only by the 2026-10-25 ruling).
**Precondition for persisting it:** an explicit, confirmed "continue from this older copy" action
(Phase 3) that resets RecordedGen to P — the way out of a merge-like refusal — is an owner product
question that must be settled first. Residuals, stated (P2b-C21): (1) until RecordedGen is persisted, a
whole-folder merge-restore can still make the sweep remove newer units — encoded by a skipped test
citing R37 until Phase 3–4; (2) a stale inventory already reported RED, followed by a pointer-only
restore to a generation between I and the true P, can still sweep units — two faults, the first
visible; (3) on a FAT or exFAT drive, a power cut that loses both pointer renames but keeps the
inventory leaves I = P + 1, refused with no in-product way out until Phase 3's action.

**Wording (final-state; friction review in Phase 3), separate per case (P2b-C8):** I > P — *"This
archive's record is older than its file list — an older copy of the record may have been restored.
Restore the newest copy of the whole archive folder; nothing was changed."* I = P + 1 — *"This
archive's last save did not finish, or an older copy of its record was restored. Nothing was changed."*
RecordedGen — *"This archive is older than the last backup this computer made…"* The corrupt-record remedy (manifest.go:713, "restore
it from a backup") gets the per-chunk wording of §2.

## 4. Keyless tier-1 verify (slice 2b-4, A5; P2b-C17)

`app.Verify` gains an encrypted branch, under the same archive lock (verify.go:149-153), that reads only
the inventory, the pointers' cleartext headers and the files: each listed file is judged with `Lstat`
(a regular file, no link), its size, then its SHA-256 (reading at most size+1 bytes), with the Windows
cloud-placeholder rule plaintext verify applies today; `-record` is refused. The outcome is one value of
a closed list, stored as such in the last-verify allow-list (A18) with its time — no counts:

| Outcome | When | Status |
|---|---|---|
| `intact` | I = P (primary or mirror), both pointer digests match the top file (or `mirror none` and the primary matches), every `live` entry matches | green |
| `not_attested` | I < P; no inventory; `mirror none`; or RecordedGen > P with no file that neither P nor I names ("restored to an older copy; the next backup accepts it"); listed files still checked | amber — "run a backup, then check again" |
| `damaged` | a `live` entry missing, changed or truncated; a pointer digest wrong at I = P; both pointers missing or unreadable | RED |
| `restored_older` | I > P; I < inv; or RecordedGen > P with files under `units/` or `chunks/` that neither P nor I names | RED — "older than this computer's last backup" |
| `newer_format` | the inventory, a pointer or the envelope is from a newer MailArchive | amber — "upgrade" |

A missing `retained` entry is not damage (the next run's start sweep removes them by design); only
`live` entries count. Files under `units/` or `chunks/` that are not listed, and files under
`inventory/` the top file does not name, are reported as leftovers the next backup removes —
informational, never `damaged`. The I = P + 1 case is reported with §3's two-cause wording. `ARCHIVE-INTEGRITY-ATTENTION.txt` gets fixed wording
per outcome. Tier 2 (decrypt + cross-check) is keyed, Phase 3. `status --json` reports an encrypted
archive's `History` as `null`, not `{Exists:false}`.

## 5. The sealed history (slice 2b-2; P2b-C14)

Folded into the root pointer as kind-3 chunks; the chunk plaintext is today's JSON lines, so one parser
serves both, and **every chunk starts with a copy of the run header line current at its first event**,
marked as a copy, so each chunk reads on its own and readers count a run once (`HistoryStatus.Runs`,
history.go:400-401, counts each run id once). A `state.Timeline` interface that `*HistoryWriter` already satisfies
keeps plaintext byte-identical. The sealed timeline buffers events; each commit re-seals the open LAST
chunk as a new file (never in place) until it holds 256 KiB of plaintext, then starts a new one; earlier
chunks are never re-sealed.
- Loading the manifest never opens history chunks; go-back opens them lazily. A chunk that fails
  authentication makes go-back "partial": the reader inserts a no-valid-header barrier (haveAt = false
  until the next run header, as history.go:274-280 does for a bad header), so no event is ever dated by
  an earlier run's header (the wrong-date bug, #3). It never blocks a backup (graph.go:220-221).
- An open last chunk the writer cannot read is never dropped from the pointer: it stays named, its range
  goes into `history_gap` (one widening interval — a superset, the safe direction), and a new last
  chunk starts.
- *As built (2b-2, MA-756):* the copy and the barrier are two `HistoryEvent` fields (`copy`, `barrier`,
  omitted when false, so plaintext logs are byte-identical); `FoldEvents` treats a barrier as a header
  that does not parse, and `HistoryStatus` counts a copied header's run once. The history step runs
  first in each commit, so a failed one records its gap in that commit's header.
- Keyed reindex redaction (Phase 3) re-seals the affected chunks in the same commit as the manifest,
  closing the plaintext redaction window (reindex.go:202-209) for encrypted archives. The A12 conversion
  (Phase 3) parses the plaintext `.jsonl` into chunks and counts dropped bad lines into the header.

## 6. The sealed index (slice 2b-3; P2b-C15, C16, C24)

**Image, not rows.** The index stays one SQLite database held in memory, sealed as fixed POSITIONAL
chunks (256 KiB to start; the size is decided by the measurement below). `page_size=4096`,
`auto_vacuum=NONE`, never `VACUUM` on the sealed image; the image always carries a rollback-journal
header (bytes 18/19 = 1).

**Load and capture — proven by the review's spike on the pinned v1.38.2** (TestSpikeRestoreSerialize,
macOS arm64): decrypt every chunk and check it against the pointer; register a read-only one-file
in-memory VFS (`modernc.org/sqlite/vfs.New`) over the image; through `(*sql.Conn).Raw`, `NewRestore(
"file:search.db?vfs=NAME&mode=ro")`, `Step(-1)`, `Finish()` into a private `:memory:` connection (one
open connection, no idle expiry); `fsys.Close()` unregisters the VFS; zero the buffer. Then
`journal_mode=MEMORY`, `temp_store=MEMORY`, `mmap_size=0`. **`journal_mode=OFF` is ruled out**: the
spike showed it leaves a rolled-back row in `docs` and corrupts the FTS5 index (Add's per-row savepoints,
F49, need `ROLLBACK TO`) — A19's `MEMORY|OFF` is amended to `MEMORY`. The image is captured at commit
with the driver's `Serialize()` (safe: balanced scratch use); `Deserialize` is never used (the spike
crashed it: SIGSEGV in SQLite's realloc of a buffer it did not allocate). The VFS stays `mode=ro` and
serves one name: modernc's VFS has no write path, and a write through it aborts the process. The A12
conversion (Phase 3) loads a plaintext `search.db` by backing it up from the on-disk file — a WAL-header
image does not load as bytes. No automatic fallback route exists: an alternative would widen a
disclosed residual and needs an owner-visible amendment (P2b-C15).

**Memory, stated (P2b-C15).** Spike figures at a 120 MB image (20,000 messages): Serialize 29 ms,
restore 37 ms, peak memory about 3× the image while sealing and 2× while loading. The in-memory route is
used while 3 × image ≤ a 768 MiB budget (image ≤ 256 MiB, roughly 40,000 messages — an estimate). The
budget is the index's alone: it does not overlap 2a's per-message attachment peak (about 2–3× one
message's attachments, phase2a §2) because the image is captured only at commit, between messages, so
the run's peak is the larger of the two, not their sum (2b-3 measures it);
above it the working index is a scratch file in the per-user cache folder (`job.ScratchDir`'s root,
`index/<hash of out>` — on Windows the settings folder roams), same pragmas, sealed and hashed 256 KiB
at a time with bounded memory, guarded by 2a's free-space precheck, swept at run start, removed on
every exit, in `secretDirs` and uninstall's `ScratchTargets`. Residual under R-E2: that file is
plaintext on local disk while a run is going and, after a crash, until the next run of that archive or
uninstall. *Narrowed by the 2b-2 + 2b-3 pass (§12):* "the next run" is the archive's next keyed run at
the same location that reaches its lock — whatever its `-index` — so a run refused before the lock
(the archive paused, its source or drive missing) leaves the file until a later one or uninstall; a
file left by a run at another path of the same archive (a remounted drive) waits for uninstall.

**Lazy (P2b-C15).** The sealed index is opened only on the run's first index write (Add, UpdateFolder,
or a RepairKeys that has work — the index meta version is in the pointer's head so RepairKeys can be
skipped unopened), so a no-change run never decrypts it. At each commit, if `total_changes()` moved
since the last seal, the image is captured and only chunks whose plaintext digest changed (or are new)
are re-sealed. **Index O(delta) is conditional**, not proven: it holds while no large FTS5 merge runs in
the commit, and every scattered B-tree page write costs a whole chunk. 2b-3 measures it on a realistic
fixture with a simulated month of hourly runs (bytes re-sealed per run), and chooses the chunk size and
FTS5 merge settings from that.

**Falling behind (P2b-C16).** `index_behind` is set when a section cannot be staged (§1), when a keyed
backup cannot load the index section (it proceeds with an empty index, `since: 0` — backups never stop
for the index, C3's "fail closed to needs rebuild"), and when an archive with an index section commits
records this run did not add to the sealed index (`-index=false`). Any keyed run with indexing on that
loads with `index_behind` set runs a resumable catch-up: it compares the manifest's keys with the index's and adds
the missing ones by reading their units, each commit sealing progress; until it finishes every run ends
with the typed `index` kind, so status keeps showing it; the flag clears when a commit seals the full
index. **No index write after the last seal:** `RepairKeys` (run.go:641; graph.go:251; reindex.go:106)
is sealed by the first commit; `finish`'s `indexCount` must not commit a batch on the encrypted path;
`Close` never seals. A test-build check fails a run that reached its final commit while
`total_changes()` differs from the last sealed value.

*As built (2b-3a, MA-757) and what moves to 2b-3b.* 2b-3a seals the index image in memory as
described, captures it only when its change count moved since the last capture (so an unchanged run
neither serializes nor re-seals it), and marks the index behind wherever §6 says. Four pieces of P2b-C15/
C16 are a separate slice, **2b-3b**, built before Phase 3 and before 2b's closing adversarial pass:
(1) lazy open — 2b-3a decrypts and restores the index at the start of every indexed run; (2) the
cache-folder scratch file above the memory budget — 2b-3a always works in memory, so a very large
index costs about 3× its size in memory at a commit; (3) the resumable catch-up and keyed Rebuild that
clear `index_behind` — until then the mark persists and every keyed run with it set reports nothing
more than the note; (4) the test-build check that no index write follows the run's last seal. None
of these is reachable by users (MA-726).

*As built (2b-3b, MA-758).* (1) Lazy open: `index.OpenLazy` loads the image on the first use that
touches the database; until then it answers `Count` from the record and `Changes` as zero, skips
`RepairKeys` when the pointer's `index_meta` is current, and refuses a newer one at open. The row count
lives in the sealed header as `index_rows` (the pointer's frozen layout is unchanged). A run with nothing
new decrypts nothing of the index — damage in it goes unseen until a run needs it. (3) Catch-up and
keyed Rebuild: `catchUpIndex` adds every record the index lacks, derived from its sealed units (the
original when kept, else the page, with attachment names from the zip); a record that cannot be read is
logged and left out, never removed. It runs before the walk when the index is behind or absent beside
records, and again at the end when the index was first loaded during the walk and found damaged, so the
run heals it either way. It is not resumable within a run — it adds everything, then the next commit
seals it; a crash part-way loses only that work, which the next run redoes. `RebuildEncrypted` builds a
whole new index under the lock. Departure: a run still behind does not yet end with the typed `index`
kind (status shows the mark only through the record) — Phase 3, with the status work. (4) The late-write
check is built as an always-on guard, not a test-build check: a write after the last seal is marked
behind and committed once more. Found while building it: a capture was counted as sealed before its
commit landed, so a commit that failed after the capture let the next one skip the index; a capture now
counts only once its pointer is written. (2) The scratch file moves to its own slice, **2b-3c**.

*As built (2b-3c, MA-759).* The scratch file is `<scratch folder>/<archive id>/index/search.db` (the
archive's existing scratch folder, one per archive location, so no second naming scheme), chosen when
the recorded image or an estimate for the archive's records (6 KiB a message, from the spike) is over
256 MiB. It is loaded lazily by streaming the image a chunk at a time and captured a chunk at a time from
the file, so memory stays at one chunk; a write-ahead-log image is refused there (SQLite would create
companion files). Every indexed run first removes a file a crashed run left. The free-space precheck
does not refuse the backup: with no room the run goes on without its index, marked behind and said in
the log (C3, backups never stop for the index); a keyed rebuild without room is refused
(`scratch` kind). Found while building it: a message a run could not add to the sealed index was
counted but never marked behind, so no later run added it; it is now marked. Not separately provable:
a failed load removes its own scratch file, but the replacement load that always follows removes it
too, so only a run whose replacement also fails depends on it (kept as defence; the next run's
removal is the backstop).

**Keyed Rebuild** is built in 2b at engine level — the remedy behind the catch-up, and for a damaged
section — reading content through `Units.Read`, building in memory or the scratch file, never under
`out`, committing through `container.Commit` at gen+1. Unreachable by users (MA-726). Keyed Reindex
(prune) waits for Phase 3.

## 7. Cross-cutting rules

- **Files and links (P2b-C18).** Reads of chunks, pointers and inventory files use the unit store's
  rule: `Lstat`, refuse links and special files, `util.DescriptorReadable`, read capped at the recorded
  size (pointer cap 64 MiB; inventory caps by line count). Chunk staging, inventory writes and the
  extended sweep judge `chunks/`, `inventory/` and their shard folders with realDir/ensureDir, refusing
  links, exactly as `units/` does. Writers enforce the readers' caps before the rename.
- **File times (P2b-C10).** Chunks and shard files take the run's start as mtime (P2a-C19). The two
  fixed-name files a run rewrites at every commit — the pointer pair and the top inventory file — take
  max(run start + 2 s × the commit's number within the run, the file's previous mtime + 2 s), so a
  short run right after another never repeats a time, and so mirror-style backup tools that compare size
  and mtime (rsync's quick check, robocopy /MIR) never skip a changed pointer of identical size; that
  reveals only the commit count, which the cleartext gen already shows. ctime, birth time and folder
  times still carry real timing (R-E2's "filesystem timestamps are not hidden").
- **Readers (Phase 3).** A reader loads every chunk of one pointer generation at open; on a missing
  chunk it re-reads the pointer and, if the generation advanced, reloads once before reporting damage.
  With no deletes during a run (P2b-C9), a reader that opened mid-run keeps its chunks until the next
  run's start.
- **Windows.** Pointer and inventory replaces use `util.ReplaceRetrying`; a sweep delete that hits a
  sharing violation is left for the next sweep, never an error.
- **Recognition predicate** (MA-727): the pointer names, the inventory names (any form, Lstat) and a
  `chunks/` or `inventory/` folder holding unit- or shard-shaped files are encrypted signals.
- **Tree grammar (P2b-C19; MA-743).** It gains exactly: `.mailarchive-manifest.ptr`,
  `.mailarchive-manifest.ptr.bak`, `.mailarchive-inventory`, `chunks/[0-9a-f]{2}/[0-9a-f]{32}`,
  `inventory/[0-9a-f]{2,4}\.[0-9]+` (the fan-out d is 2 to 4); `.mailarchive-*.tmp` temps at the root,
  inside a `chunks/` shard folder and inside `inventory/`; the envelope `.mailarchive-manifest.json` stays allowed, now permanently (it leaves the 2b
  list, not the grammar). Anticipated by 2a §7 item 4 ("container files"); confirmed by this review.
- **Leak budget, stated honestly (P2b-C20).** File NAMES under `chunks/` say nothing about kind, but
  SIZES do: a sealed chunk is its plaintext + 40 bytes, unpadded (aead.go:149-158), so equal-sized index
  chunks show whether indexing is on and roughly how big the index is; history chunks exist only for
  Microsoft 365 archives (local imports keep no timeline, run.go:696-697), so their presence suggests
  the source kind, and the last chunk's growth per commit shows roughly how many changes each Microsoft
  365 commit made. This is inside A6's frozen residual ("file sizes, file count, timestamps") but is
  named here so the claim is not larger than the mechanism; padding every chunk to one size class would
  remove the exact-size signal for about 25% more disk and upload, and is not done. Also visible: the
  cleartext generation counter (the number of commits), the inventory's file sizes and digests, and the
  file times above.
- **An older MailArchive (P2b-C22).** It refuses at the envelope, but a scheduled run of an older binary
  first writes its cleartext lastrun (with the job flags and source path) into the archive. Disclosed
  beside A4. Phase-4 condition: an encrypted archive's job file carries a bumped job-file version, so an
  older binary refuses at `job.Read` (job.go:167-179) before it touches the archive; the next keyed run
  rewrites the last-run record in its minimised form.

## 8. Tests

Every container slice proves, test-first with prove-fail → prove-pass:
1. **Positive twin first:** a keyed run then a keyed load gives exactly the records / events / rows a
   plaintext run of the same mail writes; plaintext bytes unchanged (MA-329).
2. **Crash proofs** at every step of §1's order, the first commit included (a kill-at-named-step seam
   like copyexport's): after each, the next keyed run opens the last committed gen, every record's units
   open, the tree grammar passes, and only files older than the run start that nothing names as live
   are swept.
3. **F12 and rollback:** every row of the §3 table; the previous pointer pair swapped back while its
   chunks remain → refused, nothing removed; with the old chunks gone → a typed missing-chunk refusal;
   `Units.Write` refusing an existing target name; the RecordedGen rows (clean restore proceeds and
   lowers it; a merge refuses). The F12 encoder (MA-747, R37) turns green in 2b-1; a second encoder for
   the merge case stays skipped until Phase 3–4 persist RecordedGen.
4. **Tamper and damage:** one byte flipped in a chunk, either pointer, a pointer's cleartext gen or
   file_format; a chunk swapped with another of the same kind, moved to another position or kind, or
   from an older gen; one copy of a manifest chunk damaged (loads from the mirror) and both (refused,
   naming the file and date); an envelope naming another archive; plain JSON at the envelope's name; a
   newer format in each file → each through `assure.Refused` (archive named, remedy named, nothing
   changed or swept) or the stated heal.
5. **Changed-only writes**, via a write-counting seam, asserting bytes as well as chunks: one new message
   re-seals at most the header pair and one bucket pair, at 200 and at 20,000 records; k = N changed
   records stay within the stated model; an unchanged Microsoft 365 re-run re-seals the header pair only;
   history re-seals one chunk per commit; the index stays within the bound measured in 2b-3 and seals
   nothing on a no-change run (and never opens); the inventory rewrites only touched shards, measured at
   100,000 and 1,000,000 files.
6. **Tree grammar + C2 leak scan** (MA-742/743) at every observation, the slice's 2b-list entry removed
   in the same commit; the decrypted scratch index never under `out` at any observation.
7. **Inventory and verify:** the inventory is byte-equal to one rebuilt from the pointer's chunk tables
   and the manifest's Sealed entries plus the files' on-disk sizes and digests;
   each verify outcome; leftovers informational; a crash between shard writes and the top file leaves a
   G−1 listing that verifies `not_attested`, never `damaged`.
8. **History:** a damaged middle chunk dates no event by an earlier header; an unreadable last chunk is
   kept and a gap recorded.

## 9. Slices

- **2b-0** vault + codecs: `ContainerHistory = 3`; a kind check on `SealChunk`/`OpenChunk`/
  `SealPointer`/`OpenPointer`; `(*Key).Bucket` and its sub-key; known-answer rows (MA-704/716 amended);
  in `internal/container` the pointer-file codec (head, binary tables, `inv`, newer-format gate) and the
  inventory codec (top file, generation-named shards, fan-out), pure.
- **2b-1** sealed manifest (mirrored chunks, relative LastSeen) + `container.Commit` + first commit +
  pointer mirror + strict load + inventory + F12 + `Units.Write` check + sweep (no in-run deletes —
  this changes 2a's in-run `flushDeletes`) + `RecordedGen` + file times + predicate + grammar. Removes
  `.mailarchive-manifest.json` from the 2b list. Many 2a tests that read the plaintext manifest move to
  `container.LoadManifest`. Until 2b-2 and 2b-3 land, the history and the index stay plaintext and are
  synced before `Commit`, as today, and the pointer's history and index sections are empty.
- **2b-2** history. Removes `.mailarchive-history.jsonl`.
- **2b-3** index (the measurement first), `index_behind` + catch-up, keyed Rebuild, cache-folder
  scratch. Removes `search.db` and its `-wal`/`-shm`/`-journal`.
- **2b-4** keyless tier-1 verify, last-verify outcome, status (MA-728 amended).

New catalog rows are allocated as each slice lands (from MA-749). Rows amended: MA-704, MA-716, MA-712,
MA-725, MA-727, MA-728, MA-742, MA-743, MA-744 (the list empties; the encoder stays as a guard that it
stays empty — no gate is lowered), MA-747 (its known-gap sentence becomes the narrowed residual of §3).
Each slice owes its adversarial pass; the refusal and verify wording owes Phase 3's friction review.

## 10. Parent amendments (recorded in the parent with this revision)

- **A21** — one root pointer, mirrored, commits manifest, history and index (§1); `ContainerHistory =
  3`; the pointer lists sections of `{i, name, gen, sha256, size}` and carries `inv` (refines A2's
  "file name and ciphertext SHA-256, and the chunk count"); manifest chunks are written twice; A11's
  "one file, inside the A4 envelope" reads "one file the A4 envelope names"; nothing superseded is
  deleted until the next keyed run's start.
- **A22** — the inventory is a top file plus immutable generation-named shards, written every commit
  after the pointer, and is the F12 anchor (§3); A7's "fixity/inventory anchor … in the manifest
  envelope/container" reads "in the inventory beside the pointer".
- **A23** — A19 and P2a-C13 (phase2a §4): the index load route is restore-from-a-read-only-VFS plus
  `Serialize` on the pinned SQLite (spike-proven); `journal_mode=MEMORY` only; the writer scratch is the
  cache folder, used above a stated memory budget.
- **A24** — A6's moved fields live in the sealed header's `run_details`, as of each run's final commit.
- **A25** — the highest committed generation is recorded outside the archive beside the A15 id (Phase
  3–4), written after the pointer rename; it refuses only a merge-restore; persisting it waits on the
  Phase-3 "continue from this older copy" action.
- Housekeeping: A4's line references are now manifest.go:723 (refuse-newer) and :712-713 (parse).

## 11. The 2b-0 + 2b-1 adversarial pass — amendments (2026-10-25)

The pass (`docs/review-encrypted-archives-phase2b1-adversarial.md`, workflow `wf_4492cc54-4a7` at
`050b670`: five finders, three skeptics, a critic) confirmed 23 findings and the critic proved 3 more.
These govern where they conflict with §1–§10; each is fixed with a regression test (MA-755) shown to fail
before its fix.

- **The sweep window (CRIT-3b, OUT-6).** The start sweep removes a file nothing names only when the
  inventory lists it `retained`, or a run since the record's last commit wrote it (its mtime is at or
  after that commit's `run_start`). An older unnamed file — from a half-synced copy, another device or a
  merge — is kept. The unit store's taken-name check uses the same bound: a taken name a run since then
  wrote is that run's crash orphan and is replaced; an older one is refused (a per-message gap, the file
  kept — no longer deleted by the next sweep).
- **A new archive only when nothing says otherwise (CRIT-3, CON-1).** No pointer is a first commit to
  make (or resume) only when units/ and inventory/ are empty, chunks/ holds only an interrupted first
  commit's leftovers (removed under the lock before it), there is no search index or go-back history, and
  this machine recorded no generation; anything else is a missing record, refused.
- **The envelope and the pointer copies (CRIT-1, CRIT-2).** The envelope is judged after the pointers: a
  damaged, foreign or newer-looking envelope beside an authentic pointer is healed, never trusted over
  it; a plaintext manifest at its name is refused either way. A pointer copy in a newer format decides
  only when no authentic copy outranks it (a later generation); otherwise it is damage. A file list
  whose top line reads as newer is a damaged file list (an inventory format change comes with a pointer
  format change).
- **A file list must belong to this record (OUT-2).** At I = P the top file must name the pointer on
  disk (primary or mirror digest); otherwise it is another copy's — no inventory, refused once one has
  landed (`ErrInventoryDamaged`).
- **Both copies are checked (OUT-1).** A section's second copy is checked at load (size and digest); a
  missing or damaged one is noted and both are re-sealed by the next commit.
- **Bounded file-list reads (OUT-3).** Each shard is judged by size against its line count before it is
  read, and the first bad shard ends the reading.
- **Newer formats ask for an upgrade (OUT-5, FID-1).** The sealed header's version is read before the
  strict decode; a newer record is `ErrNewerFormat`, run kind `record_newer` ("upgrade MailArchive").
- **The record's fields are pinned to its version (FID-2).** A test pins `state.Record`'s JSON fields at
  each manifest version: adding one requires bumping the version, so an older build asks for an upgrade.
- **Kinds and run behaviour (CON-2, CON-3).** A first commit whose file list fails continues the run
  (`inventory_failed`); a lock found lost at the commit point is `lock_lost` and ends the run.
- **One lost last save is not a merge (CON-6).** RecordedGen = P + 1 is "the last save did not finish".
- **Times (CON-5).** The pointer pair and top file move at least 2 whole seconds past their last time.
- **What reaches a message or the log (LEAK-2, LEAK-3).** A refusal never echoes the envelope's text;
  every notice — including those raised during a commit — is logged when raised (`LoadOptions.Logf`).
- **The recognition predicate (FC-4).** An I/O error on one reserved name no longer ends the search; and
  the pause note and a refused run's record treat "cannot tell" as encrypted.
- **Generations (CON-7).** Never reused *within a run*; across runs the OUT-2 check covers the case.
- **Residual, stated:** the remedy date for a damaged chunk comes from its mtime, which a folder writer
  can set (it only steers the advice). The critic's CRIT-3b sync-conflict step (a sync tool keeping the
  local pointer) was assumed, not run against a real sync tool.

## 12. The 2b-2 + 2b-3 adversarial pass — amendments (2026-10-25)

The pass (`docs/review-encrypted-archives-phase2b23-adversarial.md`, workflow `wf_363505d8-6a0` at
`3571519`: five finders, three skeptics, a critic) confirmed 17 of 21 findings, which merge to 12
defects, and the critic proved 3 more. These govern where they conflict with §1–§11. Each fix has a
regression test (MA-760..MA-762) shown to fail before it. The owner's "keep and warn" ruling is built
with them (MA-763).

- **What the history lost (OUT-01, CRIT-2; OUT-02, CON-04, FID-01; FID-02).** An unreadable open last
  chunk's gap reaches back to the generation after the chunk before it was last sealed. The open chunk
  is re-sealed at every commit, so its own generation says only when it was last written. The header's
  `history_gap` keeps two facts apart in memory: a loss, kept for good, and a failed staging's hold. The
  commit that lands held events seals the header with only the loss. After a reload every gap is a loss,
  because held events do not outlive their run.
- **A commit that does not land changes no header field ahead of its pointer (CON-06).** The index row
  count and a cleared hold go back to what they were.
- **An index is "whole" only when it is (LOC-02, FID-03, CON-01).** A catch-up that cannot add a message
  it read keeps the index marked behind, and marks it when it was only absent. A record whose units
  cannot be read is still left out without holding the mark (§6). Rows lost with a batch at its commit
  mark the index behind in the same commit. An index that lost a batch is unsound: a commit that failed
  part-way may leave the database inconsistent, as the pass's own test showed. It takes no more messages
  and is not sealed again that run; the record keeps the last sound image, and the next run catches up.
- **A sealed index never stops a backup, fresh or not (CRIT-3).** A new index that cannot be opened (its
  scratch folder unwritable) is dropped for the run and marked behind, as one with no room is.
- **The memory budget holds during a run (CRIT-1).** The choice at open sees only the record, so a first
  backup always starts in memory. Every 256 messages the run checks the in-memory index's size; past the
  budget it moves to its scratch file (`index.MoveToScratch`; its change count carries on). With no room
  for the file, the index takes no more messages that run and is marked behind. Peak memory at the move
  is about twice the budget, once.
- **The scratch file (LOC-01, LEAK-01/LOC-03, LEAK-03, LOC-04).** It is opened through the escaped
  `file:` URI. Every keyed run removes a crash-left one once it holds the lock, whatever its `-index`
  (`openEncrypted`). A close that fails is said in the log. The scratch folder's name hashes the archive's
  location AND its archive id, so two archives never share one, even when their folders differ only in
  case. The folder still cannot be computed without the key, which every caller now holds.
- **Keep and warn (owner ruling 2026-10-25).** An encrypted archive's indexed backup that finishes with
  the index still behind records a successful run with the kind `index_behind`. This is the only kind a
  success carries, it is in the closed list, and the A6 allow-list is unchanged. `status` WARNs it
  without the key, and the Recent-backups card says some messages are not searchable yet. This replaces
  §6's departure ("a run still behind does not yet end with the typed `index` kind"). A run with indexing
  off records no kind: there, a behind index is the run's own choice.
- **Refuted, kept as design (CON-02, CON-03, FID-04, LEAK-02).** The catch-up compares keys only: a
  message rewritten or moved while the index was behind keeps its old body or folder in search, and a
  unit that fails to read once is not retried. Both stay as §6 states them, and are noted for Phase 3.
  A scratch file at an old path of the same archive waits for uninstall (above).
- **Residuals, stated.** Three things are not separately provable: the late-write guard's RunGraph
  line; a failed scratch load's own cleanup; and the behind mark a dropped index sets itself. On every
  path a test reaches, the failed load has already marked the index. The index's re-seal bytes per run and its peak memory (§6, "2b-3
  measures") are still estimates: no measurement was made.

## 13. Slice 2b-4 as built — the keyless check (2026-10-25)

Built as §4 states (`container.CheckKeyless`, `app.Verify`, MA-764/MA-765), with these departures and
precisions. They govern where they conflict with §4; each owes the slice's adversarial pass.

- **A sixth outcome, `unfinished_save` (RED).** §4 reports I = P + 1 "with §3's two-cause wording",
  but the record keeps the outcome alone (A18), so status could not say it. The two-cause case is its
  own value of the closed list; so is RecordedGen = P + 1 (CON-6).
- **A top file present but unreadable is `damaged`, not "no inventory".** A writer only ever renames
  the top file whole, so a torn or garbled one is damage or tampering, never a crash; and the keyed run
  refuses it whenever a file list had landed. A top line that reads as newer beside a current pointer
  is the same (§11 CRIT-2). An absent top file stays `not_attested`, as §4 says. Residual: an archive
  whose file list was deleted reads amber here while its next keyed run refuses it RED (`inv` is
  sealed, so the check cannot tell a list that never landed from one that was lost).
- **What needs the key is not checked.** "I < inv" (an older file list restored under a pointer that
  saw a newer one) needs the authenticated `inv`; the keyed run refuses it. RecordedGen applies only at
  I = P, where the file list names exactly what the pointer names; it is 0 until Phases 3–4 persist it.
- **A pointer copy missing at I = P is `damaged`** (its digest cannot match), as is one present but
  unreadable; one in a newer format outranked by a current copy is damage too (§11 CRIT-2).
- **No pointer at all** is `not_attested` ("no backup has saved this archive's record yet") only when
  the keyed load would make a first commit (its `checkFresh`, without the key); otherwise `damaged`.
- **Leftovers are normal.** After every run, the shard files that run superseded stay until the next
  run's start sweep, and are reported as leftovers.
- **The file rule** is the keyed load's: every folder real, no link, a regular file or a Windows cloud
  placeholder (`util.DescriptorReadable`), then size, then SHA-256 over at most size + 1 bytes.
- **The record and status.** The last-verify record's allow-list is version, status, started,
  finished, outcome, encrypted and the carried previous verdict in the same shape. Status codes are
  `verify_<outcome>`. The integrity notice is written for the three RED outcomes, removed by `intact`,
  and left standing by the two amber ones.

## 14. The 2b-4 adversarial pass — amendments (2026-10-25)

The pass (`docs/review-encrypted-archives-phase2b4-adversarial.md`) amends §4 and §13 as follows; each
is built and tested (MA-766, MA-767, with test gaps closed under MA-764 and MA-765).

- **The rule behind `damaged` is §13's own: the keyed run refuses the state.** Applied where the first
  build did not: a folder, link or special file at the envelope's name is `damaged` (the keyed load
  refuses it before anything else), while a missing one stays healable; a plaintext manifest is
  `damaged` whatever version it claims, before any newer format; a newer envelope asks for an upgrade
  only where no pointer copy stands; a part of the file list that cannot be read is `damaged` when
  I ≠ P (the keyed load tolerates one only at I = P); with no pointer, the folder is a new archive only
  under `checkFresh`'s exact rules (two-character shard folders, regular files), each other state with
  its own reason; a generation no writer reaches (2^64 − 1) is damage, so P + 1 never wraps.
- **A file missing or changed only under `chunks/` is `not_attested`, not `damaged`** (CRIT-1). Every
  record chunk has a second copy: the keyed load reads the good one and the next save rewrites both,
  and it refuses, naming the file, only when both are lost. The search index is rebuildable. Without
  the key the check cannot pair the copies, so it cannot attest "both lost"; and "restore the whole
  archive folder from your newest backup copy" — `damaged`'s fixed remedy — would roll back every run
  newer than that copy for a fault the next backup heals. Damage under `units/` stays `damaged`, and so
  does every listed chunk missing or changed (both copies of every section are then gone, which the
  check can tell). This applies §13's rule rather than adding one. The owner accepted it on 2026-10-25,
  with this rule's own next step in place of `not_attested`'s fixed sentence: "What to do: run a backup
  now. It rewrites the damaged copy from the good one. Then check the archive again. If the backup
  stops and names a file, both copies are lost: restore the whole archive folder from your newest
  backup copy." (`Check.Advice`, shown by `verify` and carried in `verify -json`; status keeps the
  outcome's sentence, since the record holds no reason). Residual: a damaged search-index or history chunk is not healed by a
  backup that does not load it, so the check stays amber until one does, or until Phase 3's keyed check
  names the file.
- **A link or a file where a folder belongs is `damaged`** wherever the backup refuses it: at
  `inventory/` (the file-list write cannot pass it; the shards are never read or stat'd through it),
  at `units/` or `chunks/`, and a link or special file where one of their shard folders belongs (the
  start sweep refuses it once the file list is whole). A regular file there the sweep leaves alone.
- **What nothing names is said plainly.** Files under `units/` and `chunks/` the file list does not
  name are reported as unnamed, with nothing said of their removal: a backup removes only those a run
  wrote since the record's last commit, which the check cannot tell without the key. The superseded
  parts of the file list (and an interrupted first commit's files) are leftovers the next backup
  removes — except where the file list is missing, behind (I < P) or partly unread, when the next
  backup sweeps nothing and they are said to stay.
- **The file opened must be the one judged** (`os.SameFile` between the per-segment judgement and the
  opened descriptor), so a folder swapped for a link mid-check is never read as matching. An
  `os.Root`-contained open was tried and set aside: on Windows it refuses reparse points, which may
  include the cloud placeholders the keyed load reads, and would turn every OneDrive-kept archive RED.
- **The last-verify record is read as a planted file**: no link, no pipe, at most 64 KiB; its status
  is `running` or `done`, a carried verdict is a finished one, and an encrypted finished verdict has an
  outcome of the closed list — anything else is unreadable, and no text of it (a decoder's error
  quoting a time field included) is echoed. An unreadable record is a WARN even with no verify
  schedule. A carried verdict with no outcome of the list is not carried. `status -json` gives an
  encrypted check — at the top and carried — verify's own exit for its outcome, and never counts.

Residuals, stated:
- A listed file's size has no upper bound (OUT-01): a folder writer can list a huge sparse file and
  keep the check hashing for days under the lock. Deliberate substitution is outside tier 1 (A5); a
  unit has no size ceiling a bound could use (a preserved original is sealed whole); Ctrl-C ends the
  check and releases the lock. A cancellable check is left for Phase 3's Desktop work.
- The keyed side of CON-01: a backup that heals a damaged part of the file list and is killed after
  its pointer lands, before its file list, leaves a state the keyed load refuses for good ("restore the
  whole archive folder"). The check now says `damaged`, matching it. Making the keyed load accept that
  state touches the F12 rollback guard and is left for its own review.
- The plaintext last-run record (`.mailarchive-lastrun.json`) is still read with a plain read; the same
  guarded reader should replace it (status would hang on a pipe there too).
- A record shaped as a plaintext one inside an encrypted archive is judged by the plaintext rules
  (LEAK-01, refuted: a folder writer can forge any verdict in either shape, and nothing of the mail
  leaks). Phase 3's conversion should remove a plaintext archive's old record.
- Windows was not exercised: the same-file check and a cloud placeholder at a listed path or folder.
