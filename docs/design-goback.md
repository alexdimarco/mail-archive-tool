# Design — go-back: a point-in-time (Wayback-style) view of the archive

**Revision:** 2 (2026-09-12). **BUILD STATUS:** approved with conditions, not
built — two 10-lens pre-code reviews are filed as
`docs/review-goback-predesign.md` (the go-back gate, GO_WITH_CONDITIONS) and the
superseded `docs/design-repeat-dedup-and-trash.md`'s gate (dedup core,
GO_WITH_CONDITIONS). Their conditions are folded in below (GB/dedup findings →
the numbered mechanisms of §3). It supersedes `docs/design-repeat-dedup-and-trash.md`.

Operator decisions fixed: go-back lives in `serve`; daily fetch recommended,
daily granularity; Deleted Items and Junk excluded by default, config asks;
adopt the reworded R3 and the new R21 (§5). Docs are part of the build.

## 1. Problem

A scheduled `graph` capture dedups correctly within a folder but the key is
`(mailbox, folder, Message-ID)` and the folder walk has no exclusions, so a
folder MOVE — most often delete-to-trash — archives the message again under the
new folder. And there is no way to ask "what did this mailbox look like on date
D." Wanted: one record per message with the browsable view following the live
mailbox, plus a Wayback-style go-back date track, every file preserved.

## 2. Properties

- **T1 — One physical copy per message, append-only.** A live/repeat capture
  stores a message once per mailbox, keyed by identity (`Message-ID`, or a
  content fingerprint when absent). A reused Message-ID by a *genuinely
  different* message stays a distinct record via the envelope-fingerprint
  qualifier (never a silent merge — §3.1). The file lives at its first-captured
  path and is never moved or deleted by a normal run (R13). One-shot local
  imports (PST/mbox) keep the folder-scoped key and R3 unchanged — the
  mailbox-wide behaviour is confined to the live path (§3.6).
- **T2 — The archive records a delta over time.** An append-only history log,
  `.mailarchive-history.jsonl`, records each run as change events: newly-seen,
  folder-assertion (moved), gone (present last run, absent now), and
  present-again. Runs append only what changed. This log is the go-back
  timeline; the manifest is its fold-to-now projection (§3.5).
- **T3 — Views: static = first-captured; current and past = `serve`.** The
  static file:// pages stay grouped by each message's first-captured folder
  (stable, R13-clean, no cross-directory links). `serve` provides two dynamic
  projections over the manifest+history: the **current** view (each message
  under its current folder; departed messages hidden) and the **at-date-D**
  view. The static browse never changes; go-back and current-following are
  `serve` features (§3.4).
- **T4 — `serve` hosts the date track.** `serve` gains `?at=<date>` and a date
  slider; selecting D folds the history to D and renders that day's folders and
  messages, server-side, adding no script to the archived pages (R19).
- **T5 — Two kinds of "gone"; reindex is redaction.** *Deleted from the mailbox*
  is a timeline event (file kept, visible in past views). *Removed from the
  archive* (delete files + `reindex`) is a redaction: every date's view =
  (history projection) ∩ (files on disk), so a redacted message never appears at
  any date; `reindex` compacts the history log (§3.4). (R21.)
- **T6 — Daily fetch recommended; granularity = run cadence.** Between-run
  changes may be unobserved. Docs recommend `schedule -interval daily`.
- **T7 — Deleted Items and Junk excluded by default; config asks.** (Operator
  ruling.) Exclusion is by resolved well-known-folder ids (`deletedItems`,
  `junkemail`), locale-independent; the GUI and CLI/schedule ask; click-through
  excludes; opting in captures them deduped and timelined.
- **T8 — Documented in the build.** README "Going back in time"; a `docs/` user
  guide; release notes; a `README.txt` line naming `.mailarchive-history.jsonl`
  and pointing at `serve`; the daily-fetch recommendation.

## 3. Mechanism

### 3.1 Key format v4, identity, and the fingerprint-safe collapse

- **Stop discriminating key format by NUL count.** Bump `manifestVersion` 3→4
  and the index meta version 2→3. The legacy v2→v3 re-scope is gated on the
  stored **version integer**, not on `keySeparator` count (fixes GB-01/GB-1/F1).
  The live-path physical key becomes `Key(token, identity)`; `Qualify` appends
  the fingerprint as before. Because discrimination is now version-based, the
  one-NUL folder-less key is never mistaken for a v2 key.
- **v3→v4 migration (fingerprint-safe, no silent drop).** On first load of a v3
  archive, records sharing `(token, identity)` are collapsed to one `(token,
  identity)` record **only when their stored `Fingerprint` matches** — using the
  per-record fingerprint the manifest already holds, so no download. Two
  genuinely different messages that reused one Message-ID in different folders
  have different fingerprints and stay separate as `#fp`-qualified siblings
  (resolves the hidden blocker; R1 preserved). The surviving record keeps the
  first-captured file and `FirstFolder`/`FirstSeen` (earliest `ExportedAt`, then
  lexical key — deterministic); each collapsed sibling becomes a folder-assertion
  event in the history log so no location is lost; the loser index rows are
  pruned; the loser files stay on disk and are reported by `verify` as
  `unexpected` (documented; a later reconcile may sweep them). This is done once,
  logged ("collapsed N move-duplicates by identity"), under the v4 migration —
  the retroactive dedup §1 raised is handled here, not deferred.
- **The mailbox-wide identity index** is `mid → []{key, fingerprint}` (not one
  key), built at load and maintained on Add and Delete, coexisting with
  `#fp`-qualified siblings and any pre-existing multi-folder records
  (GB-2/F2/F3).
- **New pre-existing records get load-time defaults** under the v4 migration:
  `Present=true`, `FirstFolder=Folder=`existing folder (from the pre-collapse
  key), `FirstSeen=LastSeen=ExportedAt` (GB-05).

### 3.2 Never dedup on Message-ID alone; the move fast-path

- **Widen the Graph listing `$select`** to carry the envelope fields the
  fingerprint needs (`subject, from, toRecipients, ccRecipients,
  receivedDateTime, hasAttachments`) so an envelope signature is computable
  **before** download. The mailbox-wide skip fires **only** when the candidate's
  envelope signature matches an archived sibling's stored fingerprint; on any
  mismatch, DOWNLOAD and let the exporter's `#fp` split file it as a distinct
  message (dedup-F1). A reused Message-ID never drops a distinct message.
- **Move recording, no download (R17 intact).** The fast-path probes
  `Key(token, identity)`; when a message is seen and its observed folder differs
  from `Record.Folder`, it does a **no-download in-place merge**: rewrite
  `Folder`/`LastSeen`/`Present`, append a folder-assertion event, and update the
  index's folder column via a body-free `UPDATE docs SET folder=? WHERE key=?`
  (GB-06/GB-3/E). The folder is known from the listing, so nothing is fetched.
  The merge preserves `Fixity`/`Fingerprint`/`ExportedAt`/completeness (in-place
  field merge, never a Record rebuild — dedup-F4/G).
- **Both run modes dedup mailbox-wide** (with the envelope-signature
  discrimination), so a `full` run does not re-materialise per-folder duplicates;
  `full` stays a complete recovery path (dedup-F6).

### 3.3 The history log (the delta)

- `.mailarchive-history.jsonl` under `-out`, append-only, one JSON object per
  line: a run header `{"run":id,"at":RFC3339,"mailboxes":[…]}` then, per changed
  message, `{"k":key,"folder":"Inbox"}` (new/moved/present-again) or
  `{"k":key,"gone":true}`, and a folder-rename line applying to all messages
  under an old path in one event (bounds rename cost — GB-5). A **run-completed
  footer** marks a clean run.
- **Torn-tail (write side).** On open-for-append, if the file does not end in
  `\n`, truncate the torn trailing partial line before appending, so a torn line
  is always genuinely last and the read-side skip suffices (GB-4).
- **Present-again.** A message re-observed after `gone` emits a folder assertion
  (`Present` false→true). The fold at D = the latest of {folder-assertion, gone,
  present-again} with `at ≤ D` (GB-03/F).

### 3.4 gone-detection, serve views, reindex redaction

- **gone-detection by full reconciliation.** Every observation — including a
  fast-path or manifest skip — stamps `LastSeen=thisRun` (folder known from the
  listing, no body, R17 intact). After **every non-excluded folder of a mailbox
  is fully walked** in a run that held its lock, `gone` = that mailbox's
  `Present` records with `LastSeen < thisRun`, **scoped to folders actually
  walked** — a message in an excluded/unwalked folder retains its last state and
  is never marked gone (GB-4/GB-08/GB-3/F3/D). Never computed at a checkpoint.
- **serve current + at-D projection.** `serve` renders the current view and any
  `?at=D` server-side over the manifest (current) and the folded history (past),
  intersected with files on disk (T5). A compact date track (observed run dates,
  newest first) sits atop. Static pages are untouched (R19).
- **reindex redaction.** After pruning index/manifest rows for files gone from
  disk (today), `reindex` compacts `.mailarchive-history.jsonl` to drop events
  for messages no longer on disk, so redaction spans all dates (T5/R21).
- **History-log recovery/legibility.** `status`/`verify` report history coverage
  and flag a torn tail (GREEN/WARN/RED with a remedy, X6); `serve` announces
  "go-back unavailable/partial" rather than silently showing current-only when
  the log is missing/corrupt; the `README.txt` and docs name the file (F2/H).

### 3.5 Crash ordering (the two/three durable stores)

Pin the order **history-append+fsync → index → manifest.Save (the trailing
anchor)**, at run end AND at every checkpoint: extend `commit()`/the checkpoint
to append+fsync the run's folder/gone events before advancing the manifest's
`Folder`/`Present`/`LastSeen`. A crash between steps yields at most a **duplicate
event** (idempotent under the fold), never a lost move (GB-02/GB-2/GB-5/F4/B).
The message file is fsync'd before its events.

### 3.6 Scope, folder identity, pages, trash

- **Scope.** Mailbox-wide dedup is confined to the live/repeat path (Graph;
  IMAP later) via an explicit `Exporter.DedupMailboxWide` flag set only there;
  one-shot local imports keep the folder-scoped key and R3 (dedup-F3/E).
- **Folder identity by stable Graph `Folder.ID`**, not the display-name path, so
  a rename is one folder-rename event, not a mass move (GB-5/dedup-F2).
- **Static pages** group by first-captured folder (physical, R13-clean);
  current-folder and at-D grouping are serve-only projections (G — this replaces
  rev 1's "pages.Generate by current folder", removing the cross-dir-link
  blockers F5/GB-6).
- **Trash/junk** excluded by default by resolved well-known-folder ids
  (`deletedItems`, `junkemail`); `-include-deleted`/`-include-junk` opt in; the
  display-name `-exclude-folders` form is documented as locale-fragile
  (dedup-F6/F7).
- **No-Message-ID messages** dedup mailbox-wide on the post-download content hash
  (stable for the same body); wording aligned so a moved no-mid message is one
  record too (dedup-F4).

## 4. Build order and seams

1. **Slice A — v4 key + fingerprint-safe collapse migration + mailbox-wide
   identity index + envelope-signature dedup + move fast-path + history log +
   crash ordering.** The foundation and the riskiest slice; heaviest tests. Must
   prove: an unchanged re-run downloads nothing (R17) and the first upgraded run
   over a v3 archive re-downloads nothing; a moved message is one file with an
   updated folder + a history event (no duplicate); two distinct messages sharing
   a Message-ID across folders both survive the collapse (R1); the log tolerates
   a torn tail (read and write side); crash order holds (log before manifest);
   an old (v3) binary refuses a v4 archive.
2. **Slice B — gone/present-again detection (full-reconciliation, scoped to
   walked folders) + the index folder-update path.** Tests: a deleted-online
   message is marked gone after a full walk, not on a checkpoint; an excluded
   folder never triggers gone; re-appearance restores it; the index folder column
   follows a move.
3. **Slice C — serve current + at-D projection + date track + history recovery
   legibility.** Tests: at-D shows a since-deleted message under its then-folder;
   current view hides it; a redacted (deleted+reindex) message shows at no date;
   static pages stay script-free (R19); a corrupt log makes serve say
   "go-back partial", status WARN.
4. **Slice D — trash/junk exclude-by-default + config ask** (well-known-id
   resolution; CLI flags; GUI step; schedule preview).
5. **Slice E — docs** (README, `docs/` guide, release notes, `README.txt`,
   daily-fetch recommendation) — lands with the code.

Each slice ships prove-fail → prove-pass and catalog rows. Slice A owes an
adversarial pass (the fingerprint-safe collapse and the envelope-signature dedup
are R1-critical); slice C owes an adversarial pass (a hostile history log fed to
`serve`; `?at=` escaping the on-disk set; folder/date injection into the served
page) and a friction check of the date track; slice D owes a friction check of
the config ask.

## 5. Invariants and honesty

- **R3 reworded (operator-adopted):** "a message captured from a live source is
  stored once per mailbox; its folder over time is recorded, and the current/
  served view groups it under its current (or a chosen date's) folder. A one-shot
  local import keeps per-folder copies." Genuine simultaneous two-folder
  membership in one observation is recorded for that date.
- **R17 reworded:** "an incremental re-run re-downloads no already-archived body;
  a cross-folder Message-ID hit costs an envelope-signature check (no body) and
  downloads only a genuinely new/distinct message." No-download stays true for
  the unchanged case and for a move.
- **New R21 (operator-adopted):** the served point-in-time view shows exactly the
  messages present on disk projected to the chosen date; a message removed from
  the archive (files deleted, then `reindex`) never appears at any date. The
  history log is append-only and reindex-compactable.
- **R13 preserved** (files never moved/deleted by a normal run; the v3→v4
  collapse leaves loser files on disk, reported by `verify`, never auto-deleted).
  **R8 preserved** (one index row per message; the folder column follows the
  move). **R19 preserved** (static pages script-free; the slider is serve-only —
  no offline go-back).
- **Honest limits:** granularity = run cadence; the physical path keeps the
  first-captured folder while serve's grouping follows current; the v3→v4
  collapse leaves superseded duplicate files on disk until a reconcile sweeps
  them; a folder move is inferred from per-run folder observation, not a
  server-side move event.
- **Catalog impact (for the OPERATOR / the build):** R3/R17 reworded, R21 added;
  S4/MA-09/MA-13 and the Acknowledged-limits paragraph updated; new scenarios
  S37 (point-in-time/go-back), S38 (move deduped + timelined), S39 (v3→v4
  fingerprint-safe collapse); new MA rows for each slice; the covers baseline
  moves accordingly. The build states the exact row list in the catalog.
