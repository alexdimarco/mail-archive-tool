# Design — incremental completeness (manifest knows what is missing)

**Revision:** 2 (2026-09-02). **BUILD STATUS:** built — pre-code review filed as
`docs/review-incremental-completeness-predesign.md` (GO_WITH_CONDITIONS, B1–B12 folded
into this revision). Landed as commits `679d2bc` (B12 slice: atomic files, sweep,
manifest durability — MA-69, MA-94), `49e3386` (completeness core — MA-66, 67, 68,
70, 71) and `751f021` (run lock, checkpoints, Message-ID reuse, stem guard — MA-85..88);
all tier-U rows CI-tested with prove-fail records in the commits.

## 1. Problem

The manifest records a message as exported unconditionally
(`internal/export/exporter.go`, the `Manifest.Add` after rendering), including a
message captured with **no body** or with **zero-byte attachments** — the shape an
on-demand IMAP store (Thunderbird `mime_parts_on_demand`, Evolution cache) or a
windowed Outlook `.ost` ("Mail to keep offline") produces before the content is
downloaded. Consequences, all observed in the code:

- An **incremental** run never revisits such a message (`Manifest.Has` → skip), so
  the recommended repeat mode for exactly the sources that produce partial
  captures hides the gap forever. The only documented remedy is `-mode full`,
  which rewrites the entire archive.
- The verification report `attachments-report.tsv` is **truncated every run**
  (`internal/app/run.go`, `os.Create`), so the record of the gap is lost on the
  next run that has no new issues (it is not rewritten) or has different ones (it
  is overwritten). R1 promises "recorded, never hidden"; across repeated runs it
  is hidden.
- Exported `.html`/`.zip` files are written non-atomically (`os.WriteFile`,
  `os.Create`), so a crash mid-write can leave a truncated file that a later
  manifest save then records as exported.

## 2. Properties (what this design makes true)

- **P1 — Incompleteness is durable.** A capture that is missing content is
  recorded in the manifest *as incomplete, naming what is missing*. (proven by
  MA-66/67)
- **P2 — Incremental fills, and only fills.** An incremental run re-examines every
  *fillable* incomplete entry against the source, regardless of any `-since`
  window (B7); when any previously-missing item is now present (old ⊄ new, B2)
  the entry is rewritten (html + zip + index row) and re-marked by what is still
  missing; when nothing improved, nothing is rewritten. Over an unchanged source an
  incremental run therefore still exports zero items (R2 holds). *Conditional:*
  "fills" requires the source to actually hold the content — for on-demand
  IMAP/OST that means the user enabled offline download and the client synced;
  the tool cannot make the client download (B11). (MA-66)
- **P2a — Terminal vs fillable (B1).** Every missing item is classed. A source that
  is complete-at-fetch (Graph: one GET returns the whole MIME) records its gaps as
  *terminal*: they appear in the report as "source-empty", count separately, and
  are never re-downloaded — R17 and MA-62 are unchanged. On-demand sources
  (PST/OST, Thunderbird mbox/maildir, Evolution) record *fillable* gaps. The
  summary, report and `status` show both counts; posture warns on fillable only.
- **P3 — The report is a view, not a log.** `attachments-report.tsv` is
  regenerated at the end of every run (and by `reindex`) from the manifest's
  incomplete set plus each entry's unresolved inline references, so nothing is
  lost between runs and a filled entry disappears from it. (MA-68)
- **P4 — No partial file is ever visible.** Every `.html` and `.zip` is written to
  a uniquely named temp file in its own directory (`os.CreateTemp`, B6), fsynced,
  and renamed into place (zip, then html); the manifest is saved after, and its own
  temp is fsynced before rename with the directory fsynced after (B5). A crash
  leaves at most an unreferenced `.mailarchive-*.tmp` or an orphan zip, both swept
  at the start of the next run and by `reindex` (B6), and never a truncated
  exported file recorded as done. *Proven* (fault injection, MA-69) for the
  namespace guarantee; content durability across power loss is *conditional* on
  the filesystem honouring fsync (ext4/APFS/NTFS do; FAT/exFAT do not). Extends
  R5 from the manifest to the files. Lands as its own commit before the schema
  change (B12).
- **P5 — Legible.** The run summary prints `incomplete=N (filled M this run)` and
  the report path; the GUI summary shows the same; `status` (design-schedule-v2)
  shows the standing count. (MA-66, X6)
- **P6 — Legacy archives migrate themselves (B3/B4).** A manifest written before
  this design (version 1) cannot know which of its entries are incomplete. On load,
  every version-1 record is marked `Missing:["unknown"]` (fillable), so the next
  incremental run re-examines each once through the normal retry path and records
  the truth; the count of `unknown` entries is shown by the summary and `status`
  until it reaches zero. The same rule applies to a version-1 manifest that already
  holds entries after a binary downgrade, so an old binary rewriting the file costs
  one re-examination pass, never silently "complete" records. An existing
  `attachments-report.tsv` is renamed to `attachments-report-legacy.tsv` before the
  first regenerated report so earlier findings are preserved. (MA-70)

Conceded non-goals (scope discipline):

- A **legitimately empty** message (no body, no attachments) from an on-demand
  source is indistinguishable from a not-downloaded one at the reader boundary. It
  is recorded as fillable-missing `body` and re-examined on every incremental run
  by a *probe* (body presence and attachment sizes via a counting writer — no
  render, no temp files; B10), and never rewritten unless content appears. Cost:
  one probe per empty message per run; accepted and stated. From Graph the same
  message is terminal and costs nothing.
- **Unresolved inline `cid:` references** are recorded (they appear in the
  report) but do NOT make an entry incomplete: they are almost always dangling in
  the source mail itself (a reply quoting an original's images) and are not
  recoverable by re-syncing. Re-examining them every run would be wasted work.
- Content the source itself will never provide (a server-side-deleted
  attachment) stays incomplete forever and stays in the report. That is the
  honest state.

## 3. Mechanism

### 3.1 Manifest record

```go
type Record struct {
    Path       string    `json:"path"`
    Folder     string    `json:"folder"`
    ExportedAt time.Time `json:"exported_at"`
    Missing    []string  `json:"missing,omitempty"`    // fillable: "body" | attachment label | "unknown" (legacy)
    Terminal   []string  `json:"terminal,omitempty"`   // source-empty: same labels, never retried
    Unresolved []string  `json:"unresolved,omitempty"` // cid tokens with no part (informational)
    Subject    string    `json:"subject,omitempty"`    // only when Missing/Terminal/Unresolved is non-empty (B8)
    Date       time.Time `json:"date,omitempty"`       // idem
}
func (r Record) Fillable() bool { return len(r.Missing) > 0 }
func (r Record) Complete() bool { return len(r.Missing) == 0 && len(r.Terminal) == 0 }
```

`manifestVersion` becomes 2. `Load` accepts 1 or 2; a version-1 file's records are
migrated to `Missing:["unknown"]` in memory (B3/B4); `Save` always writes 2. Fields
are additive, so an older binary reading a version-2 manifest ignores them. New
accessors: `Get(key) (Record, bool)`, `Issues() []Record` (records with any
Missing/Terminal/Unresolved, sorted by folder then date), `Counts() (fillable,
terminal, unknown int)`. Growth bound: Subject/Date/lists exist only on records
with issues (B8).

### 3.2 Exporter

`Exporter` gains `SourceComplete bool` (set by the caller: true for Graph, false
for every on-demand local source) which decides the class of a gap (B1).
`Export` splits into *decision*, *probe*, *capture* and *commit*:

1. **Decision.** `rec, seen := Manifest.Get(key)` FIRST (B7). Then:
   - Full mode: capture and commit (the date filter applies as today).
   - Not seen: date filter as today; then capture and commit.
   - Seen and `rec.Complete()` (or only terminal/unresolved): skip
     (`SkippedManifest++`), as today — regardless of the date window.
   - Seen and fillable: **retry**, regardless of the date window.
2. **Probe** (B10) computes the source's current presence set without rendering
   or writing: body present (any of the three sources non-blank) and each
   attachment's byte count via a counting writer. If no previously-missing item
   is now present (old ⊆ new-missing), stop: `StillIncomplete++`, nothing written.
3. **Capture** renders the html to a unique temp (`os.CreateTemp(dir,
   ".mailarchive-*.tmp")`) and streams attachments into a unique temp zip exactly
   as `WriteZip` does today (one attachment buffered at a time — the acknowledged
   memory bound is unchanged), collecting the missing set (`"body"`; each
   zero-byte attachment label) and `unresolved`. The class of the set is
   fillable or terminal per `SourceComplete`.
4. **Commit** fsyncs and renames zip then html into place, removes a stale zip
   if this capture has none, then `Manifest.Add` with the fresh sets (B2: the
   record always carries the newly computed sets) and `OnExported` (the index
   replaces by key). A crash between the two renames leaves the record
   un-advanced, so the next run's retry repairs it (B6). `Filled++` on a
   promoted retry.

Stats gain `Retried`, `Filled`, `StillIncomplete`, `IndexErrors` (B9: an
`index.Add` failure is counted and surfaced, not only logged). `Result` gains
`Fillable`, `Terminal`, `Unknown` (manifest counts after the run).

### 3.3 Report

`writeIssuesReport` becomes `report.Write(out, manifest)`: one row per missing
item, per terminal item and per unresolved token across the whole manifest,
columns `kind  class  folder  date  subject  detail  path` with kinds
`missing-body`, `empty-attachment`, `unresolved-inline-image`, `unknown` and
class `fillable` | `terminal` | `info`. Date/subject come from the record (B8).
Written atomically; deleted when there is nothing to report. On the first save
of a migrated (version-1) manifest an existing report is renamed
`attachments-report-legacy.tsv` first (B3). `reindex` regenerates it.

### 3.4 Graph fast-path (unchanged; B1)

Graph sets `SourceComplete`, so its gaps are terminal and `RunGraph` keeps
skipping every seen record by Message-ID without a download. R17 and MA-62 are
untouched; MA-71 asserts that a Graph message captured with an empty body is
recorded terminal, reported as source-empty, and NOT re-downloaded on the next run.

### 3.5 Run lock and sweep (B6/B9)

`app.Run`/`RunGraph` take an exclusive advisory lock on `<out>/.mailarchive.lock`
(flock on Unix, LockFileEx on Windows; the file records pid + start time) for the
whole run and refuse a second run with a typed message naming the lock file and
the holder. Before opening the index they sweep `.mailarchive-*.tmp` files older
than the run start and `*-attachments.zip` files with no sibling `.html`;
`reindex` does the same and reports the count.

### 3.6 Surfaces

- CLI summary: `Done. exported=… filled=… fillable=… terminal=… …` and, when
  `fillable>0`, `Verification: N message(s) still missing content — details in
  <report>` plus the IMAP guidance (ending "…then re-run; incremental fills
  them"); when `unknown>0`: "N entries predate completeness tracking and will be
  re-examined on the next run(s)". `index errors=N` when non-zero.
- GUI summary: the same counts and the report path.

## 4. Build order and seams

0. **B12 slice (own commit):** unique temp + fsync + rename for html/zip, sweep
   of stale temps and orphan zips in Run and reindex, manifest fsync, a legible
   refusal for a corrupt manifest (MA-69, MA-94).
1. `state`: Record fields, version 2, sentinel migration, accessors (MA-70).
2. `export`: decision/probe/capture/commit, classes, subset promotion (MA-66,
   MA-67); `SourceComplete`.
3. `app`: run lock (MA-85), report regeneration + legacy rename (MA-68), index
   error counting, stats/result plumbing, `reindex` hook.
4. Graph: `SourceComplete` + MA-71 (terminal, not re-downloaded). 5. CLI/GUI
   summaries.

No new dependency (Windows lock via `golang.org/x/sys/windows`, already an
indirect module). No schema change to `search.db`. Catalog: R1 reworded
("…recorded **durably in the manifest, classed fillable or terminal**, and
fillable gaps are revisited by incremental runs"), R5 extended to exported
files, R17 **unchanged**, new rows MA-66..MA-71, MA-85, MA-94.

## 5. Honesty of claims

- P1, P2a, P3, P5, P6 and the namespace half of P4: **proven** by the listed MA
  rows once built (all tier U, CI).
- P2 "fills": **conditional** (stated inline at P2).
- P4 content durability across power loss: **conditional** on fsync semantics of
  the archive's filesystem (stated inline at P4 and in the README).
