# Design — go-back: a point-in-time (Wayback-style) view of the archive

**Revision:** 1 (2026-09-11). **BUILD STATUS:** not built — input to the 10-lens
pre-code design review. Architecture-relevant: it makes the archive temporal (a
delta over time), changes what the dedup key means (one physical copy per
message, not per folder), adds an append-only history log and a `serve` date
view, and must preserve R13 (append-only), R19 (static pages are script-free),
R8 (search↔export parity), R2/R17. It **supersedes**
`docs/design-repeat-dedup-and-trash.md`, folding its dedup core in.

Operator decisions already fixed (2026-09-11): the go-back slider lives in
`serve` (the static file:// browse stays current-only and script-free); daily
granularity is fine and daily fetch is the recommended cadence; Deleted Items
and Junk are excluded by default and the config asks (opt-in to include). The
user-facing docs and the README must carry all of this — it is part of the
build, not an afterthought.

## 1. Problem

The archiver captures a live mailbox on a schedule. Today the dedup key is
`(mailbox, folder, Message-ID)`, so a message that **moves** between folders —
most commonly delete-to-trash — is archived a second time under the new folder,
and there is no way to ask "what did this mailbox look like on date D." Two
wants emerged:

1. **One record per message, and the browsable view should follow the live
   mailbox's folder structure** (a moved message shows up where it now is, not
   as a duplicate).
2. **A go-back track**, like the Internet Archive's Wayback Machine: slide to a
   past date and see the mailbox as it was then, with every file preserved.

## 2. Properties (what this design makes true)

- **T1 — One physical copy per message, append-only.** A message is stored once
  per mailbox, keyed by its identity — `Message-ID`, or a content fingerprint
  when absent — and, when a Message-ID is reused by a genuinely different
  message, qualified by the existing envelope-fingerprint (`#fp`) so a distinct
  message is never dropped or overwritten. The physical file lives under its
  first-captured folder path and is **never moved or deleted** by a normal run
  (R13). Later appearances in other folders do not write a second file. (subsumes
  the dedup design; preserves R13, R8, R17. Changes R3 — see §5.)
- **T2 — The archive records a delta over time.** An append-only history log,
  `.mailarchive-history.jsonl`, records each scheduled run as a small set of
  **change events**: a message newly seen, a message whose folder changed, and a
  message no longer present in the mailbox. Runs append only what changed since
  the last observation (not a full snapshot), so the log grows with churn, not
  with mailbox size × runs. Each run also appends a run header (id, UTC time,
  mailboxes, mode). This log **is** the go-back timeline. (new: append-only
  history; complements R13.)
- **T3 — The current view follows the live mailbox.** The static file:// browse
  and `serve`'s default view show the **latest** observation: each message under
  its current folder, and a message deleted from the mailbox is not shown in the
  current view (its file stays; see T5). Folder index pages are generated from
  current folder membership (from the manifest), so the browsable structure
  mirrors the mailbox as of the last run. The physical file path is opaque
  (hash+slug) and stays at its first-captured location; the reader navigates by
  the generated folder pages, which follow the mailbox. (R8; R13 — files never
  move.)
- **T4 — `serve` hosts the go-back date track.** `serve` gains a date control (a
  slider / list of observed dates). Selecting date D renders the archive **as
  observed at or before D**: the messages present as of D, each grouped under the
  folder they were in at D, computed server-side by folding the history log up to
  D. It is dynamic and server-rendered, so **no script is added to the static
  archived pages** (R19 preserved). The raw file:// browse always shows the
  current state; go-back is a `serve` feature, stated plainly in the docs.
- **T5 — Two kinds of "gone", kept distinct, and reindex is redaction.**
  - *Deleted from the mailbox* (online delete, or moved to an excluded folder) is
    a **timeline event**: the file is kept, past-date views still show it under
    the folder it was in, and it is labelled "no longer in the mailbox after D."
  - *Removed from the archive* (the operator deletes the exported files and runs
    `reindex`) is a **redaction across all of history**. Every date's view is
    computed as `(history projection) ∩ (files present on disk)`, so a message
    whose file has been deleted never appears at **any** date, past or present.
    `reindex` additionally compacts the history log to drop events for messages
    no longer on disk. This keeps the existing delete-and-refresh escape hatch
    working and gives it a clear meaning: permanent removal from the whole
    archive, all dates. (new invariant R21 — see §5; extends R13.)
- **T6 — Daily fetch is the recommended cadence; granularity is the run cadence.**
  The go-back resolution is exactly how often the archive observes the mailbox —
  a daily schedule gives daily resolution. A message that appeared and vanished
  between two runs may never be observed (inherent to any polling archiver). The
  docs recommend `schedule -interval daily`, and the design does not force it.
- **T7 — Deleted Items and Junk are excluded by default; the config asks.**
  (Operator ruling.) The two well-known folders are not captured by default; the
  GUI wizard and the CLI/schedule ask the operator to include or exclude, a
  click-through takes the excluded default, and opting in captures them deduped
  and timelined like any folder.
- **T8 — The behaviour is documented for the operator.** The build ships: a
  README "Going back in time" section (the `serve` date track, that go-back is
  serve-only, daily-fetch recommendation, the two meanings of "gone", the trash
  default); a user-docs section (`docs/` guide); a release-notes entry; and a
  line in the in-archive `README.txt` naming `.mailarchive-history.jsonl` and
  pointing at `mailarchive serve` for the date view.

## 3. Mechanism (sketch — the gate refines this)

### 3.1 Identity, single copy, current-folder metadata

- `state.Key` becomes `Key(token, identity)` for the physical record — dropping
  the folder from the *dedup* key so a message is one record per mailbox — with
  the `#fp` fingerprint qualifier unchanged (a reused Message-ID with a different
  envelope is a distinct record, never a silent overwrite; MA-86).
- `Record` gains: `Folder` (current), `FirstFolder`, `FirstSeen`, `LastSeen`
  (the run time it was last observed present), `Present bool`. The physical
  `Path` stays the first-captured location (R13).
- Exporter: a message already recorded for `(token, identity)` is not
  re-written; instead the run updates `Folder`/`LastSeen`/`Present` and emits a
  history event when the folder changed. The MIME is not re-fetched (the Graph
  fast-path stays: recognised by Message-ID before download — R17).

### 3.2 The history log (the delta)

- `.mailarchive-history.jsonl` under `-out`, append-only, one JSON object per
  line. A run appends a `{"run": id, "at": RFC3339, "mailboxes": […]}` header,
  then one event per changed message: `{"k": key, "folder": "Inbox"}` (new or
  moved), or `{"k": key, "gone": true}` (present last time, absent now). Unchanged
  messages produce no line. Writes are append-only and fsync'd; a torn final line
  is tolerated on read (skip it). This file is itself a natural, auditable
  "delta over time."
- Manifest + history are the two halves: the manifest is the current projection
  (fast to read for the current view), the history log is the past.

### 3.3 `serve` date view

- `serve` gains `?at=<YYYY-MM-DD>` (default: now). The handler folds the history
  log to D — start from first-seen, apply folder-change and gone events with
  `at <= D` — to compute each on-disk message's folder-at-D and present-at-D,
  then renders the folder listing and message links for that date, intersected
  with files currently on disk (T5 redaction). A compact date track (the observed
  run dates, newest first, as a slider or list) sits atop the served pages.
  Everything is server-side; the archived `.html` files are unchanged and remain
  CSP-locked and script-free (R19).
- Performance: folding a churn-sized log is linear in events; for a large archive
  the handler caches the projection per requested date within the process.

### 3.4 reindex, pages, trash

- `reindex`: after pruning index/manifest rows whose files are gone (today's
  behaviour), compact `.mailarchive-history.jsonl` to drop events keyed to absent
  messages, so redaction spans all dates (T5). Regenerate current folder pages.
- `pages.Generate`: group by `Record.Folder` (current), not the physical path, so
  the static browse follows the current mailbox (T3).
- Folder policy: `graph.Folders` gains a default exclude of well-known
  `deletedItems` and `junkemail` (stable ids, locale-independent); the CLI adds
  `-include-deleted`/`-include-junk` (or `-include-folders`), and the GUI wizard
  and `schedule` preview surface the choice (T7).

## 4. Build order and seams

1. **Slice A — single copy + current-folder metadata + history log.** `state.Key`,
   `Record` fields, exporter/graph move-recording, the append-only history writer.
   Tests: a message that moves folder between two runs is one file with an updated
   `Folder` and a history event, not a duplicate; a reused Message-ID with a
   different envelope stays two records (MA-86 holds); the history log round-trips
   and tolerates a torn last line; incremental re-run with no change appends no
   events.
2. **Slice B — `serve` date track + `?at=`.** Fold-to-date projection, the date
   UI, redaction intersection with on-disk files. Tests: the view at a past date
   shows a since-deleted message under its then-folder; the current view does not;
   a file removed + `reindex` disappears from every date; the static pages carry
   no script (R19 stays green).
3. **Slice C — current-following pages + reindex history compaction + trash
   policy.** `pages.Generate` by current folder; `reindex` log compaction; the
   well-known-folder exclude + the config ask (CLI flags, GUI step, schedule
   preview). Tests: browse groups a moved message under its current folder;
   reindex drops a redacted message's history; Deleted Items/Junk are absent by
   default and present when opted in.
4. **Slice D — docs (T8).** README "Going back in time" section, `docs/` user
   guide, release notes, `README.txt` line, and the daily-fetch recommendation
   throughout. (Docs land with the code, not after.)

Each slice ships prove-fail → prove-pass and catalog rows; the temporal/serve
and trash paths owe an adversarial pass (a hostile history log, a `?at=` that
tries to escape the on-disk set, a folder name / date injection into the served
page), and slice C owes a friction check of the date track and the config ask.

## 5. Invariants and honesty

- **R3 re-worded (operator decision).** From "the same mail filed in two folders
  exports to both" to: "a message is stored once per mailbox; its folder over
  time is recorded, and the current/served view groups it under its current (or a
  chosen date's) folder." If a single observation genuinely reports a message in
  two folders at once (labels/copies), both are recorded for that date and the
  view shows it under both. One-shot local imports (PST/mbox) have a single
  observation, so go-back over them shows just that one state; the timeline is
  built by repeat/live capture (Graph, and IMAP if built).
- **New R21 (proposed).** The served point-in-time view shows exactly the
  messages present on disk, projected to the chosen date; a message removed from
  the archive (files deleted, then `reindex`) never appears at any date. The
  history log is append-only and reindex-compactable.
- **R13 preserved.** No exported message file is moved or deleted by a normal
  run; go-back never mutates the archived bytes. The physical path reflects the
  first-captured folder (opaque hash+slug), while the browsable grouping follows
  the current mailbox — stated so a reader is not surprised the path says one
  folder and the listing another.
- **R19 preserved.** The date slider is a `serve` feature; the static pages stay
  script-free and CSP-locked. There is no offline go-back — the raw files show
  the current state only.
- **Granularity is the run cadence** (daily recommended); between-run changes may
  be unobserved. Retroactive de-duplication of move-duplicates already on disk in
  a pre-existing archive is a separate follow-up (append-only makes it delicate).
- **R21, R3's re-word, and any new scenarios (S37+)** are proposals for the
  OPERATOR to accept into `docs/scenario-catalog.md`; the build does not
  self-grant them.
