# Design — archive fidelity: store-scoped identity, fixity + `verify`, original headers, resilient Windows scheduling

**Revision:** 2 (2026-09-03). **BUILD STATUS:** built — merged as `76966a6`
(slices a+b, MA-128..142, S30/S31), `b8a374b` (slice c, MA-143..145, S32) and
`93e45bb` (slice d, MA-146..148; MA-47/65/73/79 reconciled). Deviation from
FC6 as built: `verify` requires a recorded path's first segment to be a
non-dotfile that resolves to a real directory under the root (it does not
consult the token registry; every legitimately written first segment is a
token, and containment is enforced component-wise regardless). The 10-lens
pre-code review is filed as
`docs/review-archive-fidelity-predesign.md` (GO_WITH_CONDITIONS; FC1–FC16 are
folded into this revision). Four independent slices; each may ship in any
order and carries its own adversarial pass.

## 1. Problem (from the 2026-09-02 product review, archivist + Windows personas)

Four confirmed gaps in what the archive *is*, as opposed to how it is operated:

- **P1 (archivist) — the manifest key ignores the store.** `state.Key(folder,
  identity)` scopes identity by folder only. Two mailboxes archived into one
  `-out` that both hold `Inbox/<same Message-ID>` collide: the second store's copy
  is "skipped(seen)" and never written, although R6 promises each store its own
  tree and R3 promises the same mail in two places exports to both. Silent,
  per-mailbox completeness loss. Reproduced. (The review also found that the
  store *name* is not unique per source — Outlook's default "Outlook Data
  File", Thunderbird's "Local Folders" under every profile — so a store-scoped
  key alone would not fix it: FC2.)
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
  either.
- **P1 (windows) — the nightly task never fires on a sleeping/battery PC.**
  `schtasks /Create … /SC DAILY /ST 02:00` yields Task Scheduler defaults: no
  catch-up of a missed start, do-not-start-on-battery. For the single-PC persona
  who sleeps the machine overnight the README's "keep it current" promise is
  never met.

Deferred to a later design (large, each needs its own fixture story):
`reindex -rebuild` from archived files (nas-04), export back-out to mbox/eml
(P4), message state capture (P6-archivist).

## 2. Properties (what this design makes true)

- **F1 — Identity is store-scoped, and the store token is injective.** The
  manifest key is `Key(token, folderKey, identity)`. The token is the on-disk
  store directory name: `util.SanitizeSegment(store)` for the first source that
  claims that segment, and `segment~<8 hex of sourceID>` for a *different*
  source that resolves to the same segment. The manifest records
  `stores: sourceID → token`, so a token, once assigned, is sticky: names are
  order-independent from run to run (R2). The source id is the original input
  path (never a -copy-first snapshot) or the Graph mailbox address. The same
  token names the directory and the key; the Graph fast path uses the same
  function. (R3, R6; extends MA-86. FC2.)
- **F2 — Old archives migrate losslessly, once, by content.** A manifest entry
  whose key holds exactly one NUL separator is a v2 key and is re-scoped from
  its record's own `Path` (its first segment *is* the token); a key with two
  separators is already v3 and is left alone — the file's version integer is
  never the trigger. The index is repaired the same way (rows whose key holds
  one NUL, re-keyed from the row's `path`), and the repair runs whenever the
  manifest load re-keyed anything or the index's meta version is below target.
  Both `Load` and index `Open` refuse a stored version greater than the binary
  understands. No file on disk is renamed or rewritten by the migration. The
  v1 sentinel migration is gated on the literal `Version < 2`. (R5, R2. FC1,
  FC3.) Honesty: the already-shipped v2 binary has no forward guard; the
  README states that an archive written by this version must not be written
  by an older mailarchive (shared/synced `-out`), and the content-driven repair
  is what makes a v2 excursion detectable (duplicates) and recoverable (the
  next v3 open repairs it).
- **F3 — Every exported artifact has recorded fixity.** At write time the
  exporter records `sha256` + byte length of each file it writes for a record
  (`.html`, `-attachments.zip`, `.eml`) in `Record.Fixity` — three optional
  digests, siblings derived from the record's path, never repeated as keys.
  Bytes are hashed from what is written (the html / raw byte slices; the zip
  through a tee on the temp writer), never by re-reading. Records that predate
  fixity, or that were written before `verify -record` baselined them, are
  *unrecorded*, never *modified*. (R5, R1. FC11.)
- **F4 — `verify` is a real check, read-only, contained, and honest about
  coverage.** `mailarchive verify -out DIR [-json] [-record]`:
  - classifies every recorded file as `ok` / `modified` (size or digest
    mismatch, or a non-regular file, or a symlink at any path component) /
    `missing` / `unrecorded`, and reports `unexpected` for `.html`/`.zip`/`.eml`
    files under a store directory that no record owns;
  - exits **0** only when every recorded file was checked and intact **and** no
    record lacks fixity; **2** when the archive is not attested (any modified,
    missing, or unrecorded file — each named, with `verify -record` as the
    remedy for the last); **1** on refusal or error (no manifest, locked
    archive, unreadable manifest). `unexpected` is reported, not an exit
    condition. The summary always prints the unrecorded count and a WARN line
    when nothing was checkable; `-json` carries coverage fields
    (`records`, `with_fixity`, `checked`). The exit set is declared in
    docs/ux-contract.md X1 with a covering test. (FC4.)
  - `-record` hashes the current bytes of every recorded file that lacks
    fixity and stores them as the baseline — labelled in output and README as
    fixity *from now*, not proof the legacy bytes were pristine. (FC4.)
  - trusts nothing in the manifest: a recorded path is an integrity failure
    unless it is a clean, relative, forward-slash path with no `..`, whose
    first segment is a store token; every path is resolved component-wise
    inside the archive root and any symlink at any level fails it, never
    followed; a non-regular file is classified without being opened; reads
    stop at the recorded size + 1 byte. (R4. FC6.)
  - holds the exclusive archive lock for its whole duration so an export
    cannot race the check; a scheduled run whose trigger fires meanwhile
    refuses and records nothing (S25), so verify belongs outside the backup
    window — stated in `verify -h`, the README and here; the lock holder line
    names the verb so the refusal reads "held by mailarchive verify …". (FC5.)
  - is schedulable: `schedule … -- verify -out DIR` is a valid job, with
    `-unattended` and `-log` (MA-72's verb set extended). (FC10.)
  - bounds its report: per-category detail lists cap at 10,000 with exact
    counts and a visible truncation note. (FC14.)
  - appears in the root usage and the MA-39 help walk. `status` prints
    "Fixity: N of M records carry digests" from manifest fields alone. (FC12.)
- **F5 — Transport headers are kept and shown as what they are, never
  executed.** `model.Message.TransportHeaders` holds the internet header block:
  PST/OST from 0x007D (decoded text); mbox/maildir/Graph from the header section
  of `Raw`, used only when a blank line terminates it within 64 KiB. The page
  renders it in a collapsed `<details>` labelled "Transport headers as stored
  (unverified)", as escaped text inside `<pre>`, capped at 64 KiB with a
  visible "(truncated)" note; it is excluded from the fingerprint, not indexed,
  and never used to synthesize an `.eml` (R7). (R7, R19, R3. FC8.)
- **F6 — The Windows task is defined by XML, not by a `/TR` string.** Install
  writes a Task Scheduler XML definition (UTF-16LE with BOM) next to the wrapper
  and runs `schtasks /Create /TN <name> /XML <file> /F`, then `schtasks /Query
  /TN <name>` and fails legibly if the task is absent. The XML carries the
  calendar trigger (daily, or weekly on Sunday) whose StartBoundary is the
  *next* occurrence of HH:MM (FC13); the action = the wrapper `.cmd` (or exe +
  arguments when no wrapper); settings `StartWhenAvailable=true`,
  `DisallowStartIfOnBatteries=false`, `StopIfGoingOnBatteries=false`,
  `MultipleInstancesPolicy=IgnoreNew`; `WakeToRun` stays false (cannot power on
  an off machine; documented as weak). The run identity is explicit — an
  `InteractiveToken` principal referenced by the `Actions` `Context`, and the
  task and trigger both `Enabled` — so automatic firing does not depend on
  what `schtasks` infers from a principal-less document. Every text node comes from
  `encoding/xml`, so `&`, `<`, quotes and non-ASCII in paths are escaped, and
  the document round-trips through `xml.Unmarshal`. The wrapper still quotes
  every token (R14). `Remove` deletes the task, the wrapper and the XML
  (tolerating absence) (FC7). This *replaces* the `/TR` install: the `/TR`
  quoting guarantee of MA-65 is superseded by XML escaping plus the wrapper's
  own quoting, and MA-65's test asserts the XML; real-host acceptance is
  lab-pending under MA-79 (FC15).

## 3. Mechanism

### 3.1 Token, key and manifest version 3

```go
// state
const manifestVersion = 3
func Key(token, folderKey, identity string) string // token\x00folder\x00identity
type Manifest struct { …; Stores map[string]string `json:"stores,omitempty"` } // sourceID → token
func (m *Manifest) Token(sourceID, store string) string
func Load(path string) (*Manifest, error) // refuses Version > 3; sentinels iff Version < 2; re-keys 1-NUL keys from Path
```

- `Token`: if `Stores[sourceID]` exists return it; else `seg :=
  util.SanitizeSegment(store)`; if no other sourceID owns `seg`, claim it;
  otherwise claim `seg + "~" + util.HashHex(sourceID, 8)` (looping on the
  improbable second collision with a longer hash). Persisted with the manifest.
- Exporter: `Export(token, folderPath, m)` — callers pass the token (the
  exporter's `store` argument becomes the token; the directory is `OutDir/token/…`).
  `run.go` computes the token once per input from its original path; `graph.go`
  from the mailbox address.
- `Load`: after unmarshal, refuse `Version > manifestVersion`; if `Version < 2`
  apply the sentinel migration; then for every entry whose key holds exactly one
  `\x00` and whose `Path` is non-empty, `newKey = firstSegment(Path) + "\x00" +
  key`; count in `Rekeyed`; log it at the app layer ("re-scoped N manifest
  entries by store"). A v2 archive has no `stores` map: tokens are then
  assigned first-come on the next run, which reproduces the plain segment the
  files already use.
- Index: `meta(version INTEGER)` (absent = 1; target 2). `Open` refuses
  `version > 2`. `index.RepairKeys(force bool, fn func(key, path string)
  (string, bool))` runs when `force` (the manifest re-keyed something) or
  `version < 2`: logs "migrating index keys (N rows)", re-keys every row whose
  key holds one NUL, in one transaction, then sets version 2. `app.Run`,
  `app.RunGraph` and `Reindex` call it right after opening the index.
- Crash story: manifest saved v3 + index unrepaired → next open: version < 2
  fires. Index repaired + manifest not saved → next load re-keys by content
  again (idempotent). Old-binary excursion (manifest rewritten with Version 2
  and 1-NUL entries; index gained 1-NUL rows) → next v3 open re-keys both by
  content; the old binary's duplicate files are reported by `verify` as
  unexpected.

### 3.2 Fixity

```go
type FileDigest struct { SHA256 string `json:"sha256"`; Size int64 `json:"size"` }
type Fixity struct { HTML *FileDigest `json:"html,omitempty"`; Zip *FileDigest `json:"zip,omitempty"`; EML *FileDigest `json:"eml,omitempty"` }
// Record
Fixity *Fixity `json:"fixity,omitempty"`
```

- `writeFileAtomicDigest(dst, data) (FileDigest, error)`; `WriteZip` tees the
  temp writer through `sha256` and returns `ZipResult.Digest`. The exporter
  fills `rec.Fixity` for what it wrote; a re-capture replaces it.
- Cost: ~100 bytes per file, carried on every record and rewritten with the
  whole manifest at each checkpoint; compact JSON (nas-03) is a prerequisite
  and lands first.

### 3.3 `verify`

`cmd/mailarchive verify -out DIR [-json] [-record]` → `app.Verify(out, opts,
logger, onProgress) (Report, error)`:

1. Lock (`lockfile.Acquire`; the holder line carries `verb=verify`), load the
   manifest (refuse: no manifest → "no archive at DIR").
2. For each record: validate `Path` (clean, relative, no `..`, first segment a
   token); derive the zip/eml names from the stem; for each expected file:
   resolve component-wise under the root (any symlink → `modified: symlink`),
   Lstat (`missing` / non-regular → `modified: not a regular file`), then, if a
   digest is recorded, read at most Size+1 bytes and compare (`ok` /
   `modified`); if none is recorded, `unrecorded` — and under `-record`, hash
   the file and store the digest (the manifest is saved once at the end).
3. Walk the store directories (never descending into symlinks) and report
   `.html`/`.zip`/`.eml` files owned by no record as `unexpected` (the tool's
   own files — `index.html` pages, `README.txt`, the report, logs, dotfiles —
   are skipped).
4. Print `verified=N ok=… modified=… missing=… unrecorded=… unexpected=…`,
   then one line per problem (capped per category at 10,000 with a note),
   then, when unrecorded > 0, a WARN naming `verify -record`. Exit per F4.
   `-json` emits the report document (counts, coverage, problem lists).

`status` prints "Fixity: N of M records carry digests" when M > 0 and N < M
adds "(run `mailarchive verify -record -out DIR` to baseline the rest)".

### 3.4 Transport headers

- `model.Message.TransportHeaders string`; `contentHash`/`Fingerprint` untouched.
- `source/reader.go`: `pidTagTransportMessageHeaders = 125`;
  `msg.TransportHeaders = readTextProperty(m, pidTagTransportMessageHeaders)`.
- Raw sources: `headerBlock(raw []byte) string` returns the bytes before the
  first blank line only when that blank line occurs within 64 KiB; otherwise "".
- `export/html.go` `renderHeader`: when non-empty, after the `<dl>`, emit
  `<details class="mailarchive-headers"><summary>Transport headers as stored
  (unverified)</summary><pre>` + escaped text capped at 64 KiB (+ " … (truncated)")
  + `</pre></details>`.

### 3.5 Task Scheduler XML

```go
func SchtasksXML(s Spec, now time.Time) ([]byte, error) // UTF-16LE + BOM
func nextOccurrence(now time.Time, iv Interval, hour, min int) time.Time
```

Structs marshalled with `encoding/xml` (namespace
`http://schemas.microsoft.com/windows/2004/02/mit/task`, version 1.2):
`RegistrationInfo/Description`, `Triggers/CalendarTrigger` (`StartBoundary` =
`nextOccurrence`; `ScheduleByDay/DaysInterval=1` or `ScheduleByWeek/
WeeksInterval=1 + DaysOfWeek/Sunday`), `Settings` as in F6, `Actions/Exec/
Command` (+ `Arguments` when no wrapper). Windows `Install`: write
`<wrapper>.xml` atomically, `schtasks /Create /TN name /XML path /F`, then
`schtasks /Query /TN name` (absent → error naming the XML path). `Remove`:
`/Delete`, then remove the wrapper and the XML (ignore not-exist).
`SchtasksCreateCmd` returns the `/XML` form; Preview prints the XML body.

## 4. Build order and seams

Prerequisite: nas-03 compact manifest JSON (lands with the run-flow slice).

1. **Slice (a) identity + migration** — token registry, `Key`, manifest v3
   load rules (forward refuse, literal sentinel gate, content re-key), index
   meta + `RepairKeys`, call sites. Tests: two *same-named* stores both export
   and re-run exports zero; two distinct-named stores likewise; a v2 manifest +
   v1 index fixture migrates, an incremental run then exports zero, and a
   second load is a no-op; a manifest/index with a mix of 1-NUL and 2-NUL keys
   (an old-binary excursion) is repaired without double-prefixing; a
   Version-4 manifest and a version-3 index are refused naming the remedy;
   the re-key count is logged.
2. **Slice (b) fixity + verify** — digests at write time, `Record.Fixity`,
   `app.Verify` (+ `-record`), the verb (usage, MA-39, schedulable in MA-72),
   the `status` line, ux-contract X1, README (claim with its condition
   inline, "run outside the backup window", upgrade rule). Tests: fresh
   archive → exit 0; one flipped byte → `modified`, exit 2, names the path; a
   deleted zip → `missing`; a legacy record → `unrecorded`, exit 2 with the
   `-record` remedy, then `-record` baselines it and verify exits 0; a stray
   file → `unexpected`, exit unchanged; a `..` path, an absolute path, a
   symlinked component, a FIFO → `modified` without following/opening; an
   oversized file stops reading at Size+1; verify writes nothing without
   `-record` (NoSideEffect) and refuses a locked archive naming the holder
   verb; the report caps at 10,000 lines; `-json` coverage fields.
3. **Slice (c) headers** — model field, PST + raw readers (bounded), renderer
   (label, escaping, cap, absent when empty), fingerprint unchanged.
4. **Slice (d) Task Scheduler XML** — generator + `nextOccurrence`, round-trip
   test (paths with spaces, `&`, non-ASCII), Install/Query/Remove on Windows
   (vet under `GOOS=windows`), Preview, MA-65 reconciled, MA-79 lab row
   extended ("import the generated XML on a real host; -remove leaves no XML").

Each slice ships prove-fail → prove-pass, and an adversarial pass follows (b),
(c) and (d).

## 5. Honesty of claims

- Fixity detects *change*, not *authorship*: the manifest is not signed, so an
  adversary who can rewrite files can rewrite the manifest too. The claim is
  "bit-rot, truncation and accidental edits are detected **for files written
  by this version or later, or baselined with `verify -record`**", stated that
  way in the README; a baseline records the bytes as they are now.
- `verify` reads every byte of every recorded file: a long, deliberate
  operation that holds the archive lock; a scheduled run that fires meanwhile
  refuses and records nothing. Run it outside the backup window (or schedule
  it there).
- The transport-header block is sender-influenced text as stored by the source
  — Received/Authentication-Results lines can be forged — and is shown as
  such, not as proof of provenance. For PST it is Outlook's decoded copy, often
  absent for items that never crossed the internet.
- The first run after upgrading re-scopes the manifest and index once; its
  duration is proportional to the archive size and it is logged. The
  already-shipped v2 binary cannot refuse a v3 archive: do not run an older
  mailarchive against an archive a newer one has written.
- The XML settings make a *sleeping* PC catch up a missed run when it next
  wakes; they do not turn a machine on. `WakeToRun` is off by design. Real
  acceptance of the XML by `schtasks` is lab-pending (MA-79).
