# Design — archive fidelity: store-scoped identity, fixity + `verify`, original headers, resilient Windows scheduling

**Revision:** 1 (2026-09-02). **BUILD STATUS:** not built — this revision is the
input to the 10-lens pre-code design review (architecture-relevant: the manifest
key and version change, a new subcommand, a new install path for the Windows
scheduler, a new model field rendered into every page).

## 1. Problem (from the 2026-09-02 product review, archivist + Windows personas)

Four confirmed gaps in what the archive *is*, as opposed to how it is operated:

- **P1 (archivist) — the manifest key ignores the store.** `state.Key(folder,
  identity)` scopes identity by folder only. Two mailboxes archived into one
  `-out` that both hold `Inbox/<same Message-ID>` collide: the second store's copy
  is "skipped(seen)" and never written, although R6 promises each store its own
  tree and R3 promises the same mail in two places exports to both. Silent,
  per-mailbox completeness loss. Reproduced.
- **P2 (archivist) — no fixity.** Nothing records or re-checks the bytes written.
  `Record.Fingerprint` hashes the *source envelope*, not the file; `reindex`
  reconciles by existence only, so a truncated or bit-rotted `.html`/`.zip`/`.eml`
  is "kept". For multi-year offline storage that is the one property the README
  cannot currently promise.
- **P3 (archivist) — original internet headers are discarded for PST/OST.** The
  reader takes a curated header subset; `PidTagTransportMessageHeaders` (0x007D)
  — Received chain, Return-Path, Authentication-Results, List-Id — is never read
  and, because PST items carry no raw bytes, cannot be recovered later. mbox /
  maildir / Graph sources have the block inside `Raw` but the page never shows it
  either, so provenance is invisible on every source.
- **P1 (windows) — the nightly task never fires on a sleeping/battery PC.**
  `schtasks /Create … /SC DAILY /ST 02:00` yields Task Scheduler defaults: no
  catch-up of a missed start, do-not-start-on-battery. For the single-PC persona
  who sleeps the machine overnight the README's "keep it current" promise is
  never met; `status` reports staleness only after the fact.

Deferred to a later design (large, and each needs its own fixture story):
`reindex -rebuild` from archived files (nas-04), export back-out to mbox/eml
(P4), message state capture (P6-archivist).

## 2. Properties (what this design makes true)

- **F1 — Identity is store-scoped.** The manifest key is
  `Key(storeSegment, folderKey, identity)` where `storeSegment` is the on-disk
  store directory name (`util.SanitizeSegment(store)`), so a message that exists
  in two stores is archived under both stores' trees, while a re-run still skips
  each (store, folder, message) it has written. The Graph fast path uses the
  same key. (R3, R6; extends MA-86.)
- **F2 — Old archives migrate losslessly and once.** A version-2 manifest loads
  with every key re-scoped from the record's own `Path` (its first segment *is*
  the store segment); the search index carries its own schema version and is
  re-keyed the same way inside one transaction; both migrations are idempotent
  and keyed on the stored version, so a crash between the two leaves nothing
  half-done that a re-open does not finish. No file on disk is renamed or
  rewritten by the migration. (R5, R2.)
- **F3 — Every exported artifact has recorded fixity.** At write time the
  exporter records `sha256` + byte length for each file it writes for a record
  (`.html`, `-attachments.zip`, `.eml`) in `Record.Files`, keyed by the
  archive-relative path. The bytes are hashed from what is written (the html /
  raw byte slices; the zip through a tee on the temp writer), never by re-reading.
  Legacy records have no fixity and are reported as *unrecorded*, never as
  *modified*. (R5, R1.)
- **F4 — `verify` is a real check, read-only, contained.** `mailarchive verify
  -out DIR` re-hashes every recorded file and reports `ok` / `modified` (size or
  digest mismatch) / `missing` / `unrecorded` per file, plus `unexpected` for
  `.html`/`.zip`/`.eml` files under a store directory that no record owns. It
  writes nothing, deletes nothing, follows no symlink out of the archive, refuses
  a directory with no manifest, refuses while a run holds the archive lock (and
  holds it itself so an export cannot race the check), and exits non-zero iff
  any file is `modified` or `missing` — the exit code is the answer, the report
  is the explanation (X1/X8). Optional `-json`. (R4, R5, R12.)
- **F5 — Original headers are kept and shown, never executed.** `model.Message`
  gains `TransportHeaders` (the verbatim internet header block). PST/OST fills it
  from 0x007D; mbox/maildir/Graph fill it from the header section of `Raw` (up
  to the first blank line). The page renders it in a collapsed
  `<details>` block as escaped text inside `<pre>`, capped at 64 KiB with a
  visible "(truncated)" note; it is excluded from the fingerprint (mutable
  transport metadata must not change identity), not indexed, and never used to
  synthesize an `.eml` (R7: `.eml` is original bytes or nothing). (R7, R19, R3.)
- **F6 — The Windows task is defined by XML, not by a `/TR` string.** Install
  writes a Task Scheduler XML definition (UTF-16LE with BOM, as the scheduler
  emits) and runs `schtasks /Create /TN <name> /XML <file> /F`. The XML carries:
  the calendar trigger (daily, or weekly on Sunday) with a start boundary at the
  chosen time; the action = the wrapper `.cmd` path (or the exe + arguments when
  no wrapper); settings `StartWhenAvailable=true` (catch up a missed start),
  `DisallowStartIfOnBatteries=false`, `StopIfGoingOnBatteries=false`,
  `MultipleInstancesPolicy=IgnoreNew`; `WakeToRun` stays **false** (it cannot
  power on an off machine and depends on wake timers — documented as weak, not
  offered as a promise). Every text node is produced by `encoding/xml`
  marshalling, so `&`, `<`, quotes and non-ASCII in paths are escaped, and the
  generated document round-trips through `xml.Unmarshal` to the same path. The
  wrapper still quotes every token (R14 unchanged); `Remove` is unchanged.
  Preview prints the XML. Real import is lab-validated under MA-79. (R14.)

## 3. Mechanism

### 3.1 Key and manifest version 3

```go
// state
const manifestVersion = 3
func Key(storeSegment, folderKey, identity string) string // store\x00folder\x00identity
```

- `exporter.Export`: `seg := util.SanitizeSegment(store)`; `key :=
  state.Key(seg, folderKey, m.Identity())`; the dir already uses `seg`.
- `graph.go` fast path: `state.Key(util.SanitizeSegment(mailbox), folderKey,
  "mid:"+id)` — the same `mailbox` value later passed to `exp.Export`.
- `Load`: when `Version < 3`, for every entry with a `Path`, `newKey =
  firstSegment(Path) + sep + oldKey` (the `#fp` suffix rides along untouched);
  an entry with no `Path` keeps its key (cannot occur; defensive). `Manifest.
  Rekeyed` counts them; v1→v2 sentinel migration still applies first.
- Index: a `meta(version INTEGER)` table (absent = 1). `index.MigrateKeys(2,
  func(key, path string) string)` re-keys every row in one transaction and sets
  version 2; `app.Run`, `app.Graph` and `Reindex` call it right after opening
  the index (before any Add). Idempotent: a version-2 index is a no-op.
- Crash story: manifest saved v3 + index still v1 → next open migrates the
  index (keyed on its own version). Index migrated + manifest not saved → next
  load re-derives the same keys from paths; `Add` by new key replaces correctly.

### 3.2 Fixity

```go
type FileDigest struct { SHA256 string `json:"sha256"`; Size int64 `json:"size"` }
// Record
Files map[string]FileDigest `json:"files,omitempty"` // archive-relative path → digest
```

- `writeFileAtomic` gains a sibling `writeFileAtomicDigest(dst, data) (FileDigest,
  error)`; `WriteZip` tees the temp writer through `sha256` and returns
  `ZipResult.Digest`/`.Size`. The exporter fills `rec.Files` for the html, the
  zip when written, the eml when written. A re-capture replaces the map (old
  names are removed by the move-on-rename rule already in place).
- Manifest `Save` is compact JSON (nas-03, landing separately); fixity adds
  ~120 bytes per file.

### 3.3 `verify`

`cmd/mailarchive verify -out DIR [-json]` → `app.Verify(out, logger,
onProgress) (Report, error)`:

1. Lock (`lockfile.Acquire`), load the manifest (refuse: no manifest → "no
   archive at DIR").
2. For each record, for each `Files` entry: `Lstat` the path (refuse to follow a
   symlink; a symlink counts as *modified* with reason "is a symlink"); read and
   hash; classify. Records with an empty `Files` → each of the record's expected
   files (`Path`, and the zip/eml if present on disk) is *unrecorded*.
3. Walk the store directories (`filepath.WalkDir`, skipping symlinked dirs) and
   report `.html`/`.zip`/`.eml` files owned by no record as *unexpected*
   (`index.html` pages, `README.txt`, the report, logs and dotfiles are the
   tool's own and are skipped).
4. Print a summary line (`verified=N ok=… modified=… missing=… unrecorded=…
   unexpected=…`), one line per problem (`modified  <path>  expected sha256:…
   size … got …`), and exit 1 when modified+missing > 0. `-json` emits the
   report document on stdout.

`status` gains one line when the manifest has fixity: "Fixity: recorded for N of
M files (run `mailarchive verify -out DIR` to check them)". It does not hash.

### 3.4 Transport headers

- `model.Message.TransportHeaders string`; `contentHash`/`Fingerprint` untouched.
- `source/reader.go`: `pidTagTransportMessageHeaders = 125`;
  `msg.TransportHeaders = readTextProperty(m, pidTagTransportMessageHeaders)`.
- `source/mbox.go` (and the maildir/Evolution paths that share it) and
  `graph`: `TransportHeaders = headerBlock(raw)` — bytes up to the first
  CRLF CRLF / LF LF, as text.
- `export/html.go` `renderHeader`: after the `<dl>`, when non-empty, emit
  `<details class="mailarchive-headers"><summary>Original internet
  headers</summary><pre>` + `html.EscapeString(capped)` + `</pre></details>`.
  Cap 64 KiB (+ " … (truncated)"). Styled by the existing header CSS.

### 3.5 Task Scheduler XML

```go
func SchtasksXML(s Spec, now time.Time) ([]byte, error) // UTF-16LE + BOM
```

Structs marshalled with `encoding/xml` (namespace
`http://schemas.microsoft.com/windows/2004/02/mit/task`, version 1.2):
`RegistrationInfo/Description` ("mailarchive: <name>"), `Triggers/CalendarTrigger`
(`StartBoundary` = `now`'s date at HH:MM, `ScheduleByDay/DaysInterval=1` or
`ScheduleByWeek/WeeksInterval=1 + DaysOfWeek/Sunday`), `Settings` as in F6,
`Actions/Exec/Command` (+ `Arguments` when no wrapper). `Install` on Windows:
write the XML next to the wrapper (`<wrapper>.xml`, atomic), run `schtasks
/Create /TN name /XML path /F`, keep the file (it documents the task; `Remove`
deletes it with the wrapper). `SchtasksCreateCmd` (used by Preview and MA-65)
now returns the `/XML` form and Preview prints the XML body; MA-65's test is
*extended*, not weakened: the path with spaces and `&` must appear in the XML
escaped and round-trip through `xml.Unmarshal` to the original, and the wrapper
must still quote every token.

## 4. Build order and seams

1. **Slice (a) identity + migration** — `state.Key`, manifest v3 load
   migration, `index.MigrateKeys` + meta table, call sites (exporter, graph,
   run, reindex). Tests: the two-store reproduction now writes both files; a v2
   manifest + v1 index fixture migrates and a following incremental run exports
   zero; migration is idempotent and survives a crash between the two halves.
2. **Slice (b) fixity + verify** — digests at write time, `Record.Files`,
   `app.Verify`, the `verify` verb, `status` line, README. Tests: fresh archive
   verifies clean (exit 0); one flipped byte → `modified` exit 1 naming the
   path; a deleted zip → `missing`; a legacy record → `unrecorded` (exit 0); a
   stray file → `unexpected`; a symlink → refused/modified without following;
   verify writes nothing (NoSideEffect) and refuses a locked archive.
3. **Slice (c) headers** — model field, PST + raw readers, renderer, fixture
   assertions (escaped, capped, absent when empty), fingerprint unchanged.
4. **Slice (d) Task Scheduler XML** — generator, round-trip test, Install path
   on Windows (vet under `GOOS=windows`), Preview, MA-65 extension, README
   per-OS note. Lab: MA-79 gains "import the generated XML on a real host".

Each slice ships prove-fail → prove-pass and an adversarial pass follows (b),
(c) and (d): the verify walk (containment, symlinks, huge files), the header
block (escaping, size), and the XML (escaping of every attacker-influenced
string: name, paths, args).

## 5. Honesty of claims

- Fixity detects *change*, not *authorship*: the manifest is not signed, so an
  adversary who can rewrite files can rewrite the manifest too. The claim is
  "bit-rot, truncation and accidental edits are detected", stated that way in
  the README.
- `verify` reads every byte of every recorded file; on a large archive it is a
  long, deliberate operation, and it holds the archive lock while it runs.
- `TransportHeaders` for PST items is whatever Outlook stored in 0x007D — often
  absent for items that never crossed the internet (drafts, internal MAPI
  items); the block is shown when present and simply absent otherwise.
- The XML settings make a *sleeping* PC catch up a missed run when it next
  wakes; they do not turn a machine on. `WakeToRun` is off by design.
