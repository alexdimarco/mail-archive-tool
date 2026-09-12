# Scenario catalog — mail-archive-tool

Per assurance-kit `process/scenario-catalog.md`. The tables here are the machine
contract the covers-map meta-test (`assureblock/covers_map_test.go`) parses — do
not restyle them. The tests encode the invariants; a change that cannot satisfy
an invariant is the thing that is wrong.

## 1. ID prefix

`MA-` (test-spec IDs). Invariants use the `R` series.

## 2. Invariants (what must always hold)

- **R1 — No silent loss.** Every message the reader yields is exported, and no
  attachment/inline image is silently dropped: content referenced but absent
  (not downloaded, unparseable, torn) is recorded durably in the manifest —
  classed *fillable* (an on-demand source may still deliver it) or *terminal*
  (a complete-at-fetch source never will) — and listed in the verification
  report regenerated from the manifest on every run, never hidden. Incremental
  runs revisit every fillable gap and fill it once the source holds the content.
- **R2 — Incremental idempotence.** An incremental re-run over an unchanged
  source exports zero new items; `full` re-exports all. The manifest is the sole
  dedup authority.
- **R3 — Stable identity; one copy per mailbox on the live path.** A message's
  dedup key is stable across runs (Internet Message-ID when present,
  deterministic content hash otherwise). A message captured from a LIVE source
  (Graph; IMAP later) is stored once per mailbox, keyed by identity: its folder
  over time is recorded (in the record's Folder/FirstFolder and the history log),
  and the current/served view groups it under its current — or a chosen date's —
  folder. A one-shot LOCAL import (PST/mbox/maildir) keeps the folder-scoped key,
  so the same mail filed in two folders exports to both, but never twice within
  one folder. Genuine simultaneous two-folder membership observed in one run is
  recorded for that date.
- **R4 — Containment.** Every path the exporter writes stays inside the output
  root. Store/folder/attachment names derived from untrusted mail cannot
  traverse out (`..`, absolute paths, path separators, reserved device names) or
  collide destructively.
- **R5 — Crash-safe state.** The manifest and every exported `.html`/`.zip` are
  written to a temp file and renamed into place (fsynced first); an interrupted
  or failed run never corrupts the manifest, never leaves a partial exported file
  under its final name, and never records a file that was not fully written;
  stale temps and orphan zips are swept; progress already made survives.
- **R6 — Faithful structure.** Output mirrors the source folder tree; each store
  gets its own top-level directory; a single mbox/maildir folder does not
  double-nest under its own name.
- **R7 — Self-contained, legible HTML.** Each exported `.html` renders the
  message body offline: inline `cid:` images are embedded as data URIs; header
  fields are HTML-escaped. The page stands on its own for a reader with no
  tool: it links back to its folder page and the archive root, lists its
  archived attachments with a link to the sibling zip, shows Message-ID,
  Sent/Received (with their original UTC offsets), Reply-To and Bcc, and links
  the preserved original `.eml` when one was kept. Folder pages paginate (never
  truncate) and label their dates UTC; the archive root carries a README.txt
  that explains the layout without the tool.
- **R8 — Search ↔ export parity.** The index holds exactly the exported
  messages; a full-text query returns matching items; free-text is matched as
  literal terms (no FTS operator injection or query crash).
- **R9 — Read-only source.** Reading a mail store never modifies the source
  mailbox. The only exceptions are opt-in local sync/write paths that never alter
  mailbox content: `-enable-offline` edits Thunderbird's `prefs.js` (backs up
  first, refuses while the app is running), and `-outlook`'s pre-copy
  Send/Receive lets Outlook refresh its own cache. Graph capture is GET-only.
- **R10 — Robust parsing.** Malformed/truncated input (bad mbox framing,
  non-MIME message, unparseable node) is skipped or fallback-parsed — never a
  fatal crash of the whole run.
- **R11 — Correct date semantics.** `-since` parsing is total (relative +
  absolute + rejects garbage); the date filter excludes exactly the items
  outside the window.
- **R12 — Refusal legibility.** Invalid operator input is refused with a typed
  non-zero exit and a message naming the problem — never a panic or stack trace.
- **R13 — Reindex reconciles to disk.** `reindex` reconciles the archive to what
  is on disk: every indexed message whose exported file has been deleted, moved,
  or renamed is pruned from both the search index and the manifest; surviving
  files stay searchable; the browsable folder pages regenerate from the
  reconciled set; no exported message file is deleted — only stale
  `.mailarchive-*.tmp` temps and orphan `-attachments.zip` files with no sibling
  `.html` are swept (R5, MA-69).
- **R14 — Schedule is correct, opt-in, and reversible.** `schedule` generates a
  correct scheduler entry for the host OS (cron/launchd/Task Scheduler) carrying
  exactly the operator's backup job (an export, `graph` or `reindex` job),
  validated at schedule time through the job's own flag definitions: what
  cannot run unattended (interactive prep, a non-backup verb, `-outlook` off
  Windows, a Graph job without a secret file) is refused now. It is correct for
  any host path (Windows runs the job through a batch wrapper with every token
  quoted, so the 261-character run-string limit and cmd.exe metacharacters
  cannot bite); the job writes a size-capped log; one schedule per archive by
  default (the name derives from the archive path) and an archive-local
  descriptor names it. It prints the entry by default and only applies it
  under `--install`; `--install` is idempotent (re-running yields one entry) and
  `--remove` cleanly reverses it (entry, wrapper, descriptor), leaving
  unrelated entries untouched.
- **R15 — Evolution stores read faithfully.** An Evolution store is read without
  silent loss: the local Maildir++ store's dot-encoded, `_XX`-escaped folder
  hierarchy is decoded (every subfolder read, no traversal out of the root), and
  an IMAP disk cache's per-folder maildirs are all walked, whether nested
  directly or under a `subfolders` container. Each cached message is yielded once
  with its correct folder path.
- **R16 — Outlook COM export is opt-in and safe.** On Windows with *classic*
  Outlook, `-outlook` (and the GUI's Outlook-app option) drives Outlook to write
  a fresh, standard `.pst` per mail account, which then archives through the
  normal pipeline; every created PST file name stays inside the output root. On
  any platform without classic Outlook it refuses with a legible message naming
  the requirement — never a crash. (COM behaviour is lab-validated; go-pst
  cannot read every live `.ost`, so this is the reliable path for Exchange.)
- **R17 — Graph capture is complete, read-only, incremental.** The Microsoft
  Graph app-only source enumerates every folder (including nested) — excluding the
  Deleted Items and Junk Email subtrees by default, which `-include-deleted` /
  `-include-junk` restore (the exclusion is by resolved well-known-folder id, so
  it is locale-independent — T7, operator ruling) — and every message in the
  walked folders and archives each via its raw MIME through the shared parser; it issues
  only GET requests (the app holds the read-only `Mail.Read` application
  permission, so a mailbox is never modified); and an incremental re-run archives
  zero new items and re-downloads no already-archived message body. A cross-folder
  Internet-Message-ID hit costs only an envelope-signature check (computed from
  the widened listing `$select`, no body fetched) and downloads only a genuinely
  new or distinct message; an already-archived message that merely moved between
  folders is recorded as a folder change with no download.
- **R18 — Every scheduled run is recorded and `status` reports it.** A run
  writes a `running` record the moment it holds the archive lock and finalizes
  it (ok / failed / cancelled, with counts and error) on the way out; `status`
  reports the archive's completeness, last run and schedule posture as
  GREEN/WARN/RED, failing closed on missing evidence (no record, an unreadable
  record, a run that never finished, a missing or moved scheduled program, a
  scheduler that cannot be queried), every WARN/RED naming its remedy; it exits
  0 whenever it reports. The GUI's job file round-trips the wizard's answers.
- **R19 — Offline-inert archive.** Every exported `.html` is safe to open from
  disk: its `<head>` begins with a Content-Security-Policy meta identical to the
  policy `serve` sends (no scripts, no remote loads, no `<base>`, no forms) and a
  no-referrer meta, placed before any mail-supplied markup; mail-supplied
  `refresh` and `content-security-policy` metas are neutralized. `serve` sends
  that policy for archived files, a `script-src 'self'` policy for its own UI,
  HTML-escapes search snippets, never follows a symlink out of the archive root,
  and warns when bound to a non-loopback address.
- **R20 — Extract is faithful-or-absent and contained.** `extract` migrates the
  archive out as standard interchange — mboxrd (one file per folder) or
  byte-exact `.eml` (one per message, mirroring the tree) — by copying only each
  record's PRESERVED original bytes (`<stem>.eml`, kept at capture with `-raw`);
  it never synthesizes or re-serializes a message, so a record with no preserved
  bytes (every PST/OST item; any archive captured without `-raw`) is counted,
  listed and skipped, never written. Each `.eml` is confirmed through the gate
  `verify` uses — a validated relative path resolved component-wise, no symlink
  followed at any level, regular-file-only, the read bounded to the recorded
  size — and, when a `Fixity.EML` is recorded, verified against it before
  emitting; a mismatch is skipped like an absence. Output is atomic and
  idempotent (a temp file per message/folder renamed on success, truncating a
  pre-existing file never appending, so a re-run does not double) and contained
  within `-dest`, which may not equal, sit inside, or contain `-out`
  (symlink-resolved). mbox is mboxrd (a reader that unquotes `>From ` recovers
  the exact bytes); `-format eml` is byte-exact. extract writes nothing into the
  archive, holds the archive's exclusive lock for its whole run, and is not a
  schedulable backup. Its exit is `0` (whole set emitted), `3` (partial — some
  records had no preserved bytes, including an archive from which nothing is
  extractable) or `1` (refusal/error). Capture and `verify` warn when a
  raw-capable source is archived WITHOUT `-raw`, so the dependency is visible
  before the source is deleted.
- **R21 — Point-in-time truth (go-back).** The served point-in-time view shows
  exactly the messages present on disk projected to the chosen date: `serve`
  renders the current mailbox (each message under its current folder, departed
  messages hidden) and any `?at=D` (the append-only history log folded to D, each
  message under its then-folder), each intersected with the files actually on
  disk — so a message removed from the archive (its files deleted, then
  `reindex`) never appears at ANY date, whether or not the log has yet been
  compacted. The history log is append-only and reindex-compactable: `reindex`
  drops the events of a removed message so redaction spans all dates, while a
  message merely departed from the live mailbox keeps its file, its manifest row
  and its timeline (R13). The go-back page is fully server-rendered and
  script-free (R19); a hostile folder name, subject or `?at` folded into it is
  inert, escaped text and can neither inject markup nor surface an off-disk
  message. When the log is missing or damaged, `serve` announces go-back
  unavailable/partial rather than silently showing current-only, and `status`
  reports the timeline's coverage and flags a torn tail (GREEN/WARN/RED with a
  remedy, X6).

## 3. Scenarios

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S1 Untrusted mail names a file `../../x` or a folder `..` | R4 | name neutralized to a safe in-root segment | MA-01, MA-03, MA-29, MA-30 |
| S2 Run interrupted mid-export | R5, R2 | manifest intact; no partial html/zip visible; orphans swept; already-written items survive; resume skips them | MA-11, MA-22, MA-69, MA-94 |
| S3 Re-run over an unchanged source | R2 | zero new exports | MA-22 |
| S4 Same email filed in two folders (one-shot local import) | R3 | exported to both; never twice in one | MA-09, MA-13 |
| S5 Message with an inline `cid:` image | R7 | embedded as a data URI; renders offline | MA-19 |
| S6 Attachment/inline content not present locally | R1, R2 | skipped from the zip; recorded in the manifest (fillable or terminal) and the regenerated report; a fillable gap is re-examined by incremental runs and filled once the content is there; a legacy manifest migrates to "unknown" and is re-examined once | MA-20, MA-21, MA-66, MA-67, MA-68, MA-70, MA-71 |
| S7 Malformed or non-MIME message | R10, R1 | fallback-parsed or skipped; run continues | MA-13, MA-31 |
| S8 Search box contains FTS operators/quotes | R8 | treated as literal terms; no crash, no injection | MA-23, MA-32 |
| S9 Operator omits `-out`, gives a bad `-mode`, or serves a missing index | R12 | typed non-zero refusal naming the problem; no panic | MA-08, MA-33, MA-34 |
| S10 `-enable-offline` while the mail app is running | R9 | refused; prefs.js untouched (no backup written) | MA-27, MA-35 |
| S11 Reading a mail store | R9 | source bytes unchanged after a full read | MA-36 |
| S12 A store name/segment resolves to empty after sanitizing | R4, R6 | falls back to a stable placeholder, still in-root | MA-01 |
| S13 Operator deletes/renames exported files, then runs `reindex` | R13 | dangling index+manifest entries pruned; survivors searchable; pages regenerate | MA-40, MA-41, MA-42, MA-43, MA-44 |
| S14 Operator schedules a recurring backup, then re-runs / removes it | R14 | correct host-OS entry generated; install idempotent; remove reverses; unrelated entries kept | MA-45, MA-46, MA-47, MA-48, MA-49, MA-50 |
| S15 Evolution local Maildir++ store with nested, dot-encoded subfolders | R15, R1, R6 | every subfolder decoded and read; escaped names cannot traverse out | MA-52, MA-53, MA-55 |
| S16 Evolution IMAP disk cache: folders/<f>/{cur,new}, nested directly and via subfolders/ | R15, R1 | all folders walked; messages read as RFC822; single source | MA-54, MA-55 |
| S17 Auto-discovery hits an orphaned/corrupt Outlook `.ost` (removed account); or one locked by a running Outlook | R1 | unparseable stub excluded from the discovered set; a locked-but-valid file is kept | MA-56 |
| S18 A corrupt/truncated `.pst`/`.ost` (orphaned stub, bad OST variant) that panics go-pst | R10 | `Open` fails with a clean error; no panic escapes; the run continues and the no-console GUI never exits silently | MA-57 |
| S19 A live Exchange/IMAP `.ost` go-pst can't read; operator uses `-outlook` / the GUI Outlook-app option | R16 | on Windows, Outlook writes a clean `.pst` per account that archives normally; off Windows it refuses legibly | MA-58, MA-59, MA-60 |
| S20 Server-side archive of a (large) M365 mailbox via an app-only Graph grant | R17, R2, R9 | full first capture; incremental re-run adds nothing and re-downloads nothing; read-only | MA-61, MA-62, MA-63, MA-64 |
| S23 Two distinct messages share one Message-ID in one folder (a bug or a reused id) | R3, R1 | both exported and recorded (the second under a content-qualified key); an incremental re-run exports zero; names stay stable across full re-runs in any order | MA-86 |
| S24 Two messages' file stems collide (32-bit hash), or a message is re-exported under a new name | R4, R6, R13 | the second stem is lengthened deterministically from its key, never overwriting; a renamed message's old html/zip are removed | MA-87 |
| S25 A scheduled run overlaps a manual run on the same archive | R5, R12 | the second run refuses naming the lock file and the holder; nothing is written; the lock vanishes with its process | MA-85 |
| S26 Folder/subject names the file system cannot carry: illegal characters, reserved device names with extensions, trailing dots, non-Latin scripts, decomposed Unicode, very long paths | R4, R6 | names stay distinct, creatable and portable across OSes; slugs stay readable; long paths shrink the slug | MA-01, MA-03, MA-04, MA-89 |
| S27 An inheritor opens the archive folder with no tool, years later | R7 | README.txt explains the layout; every page navigates, names its attachments and links its zip/.eml; times carry their offsets; folder pages paginate | MA-90, MA-91, MA-92, MA-96, MA-13 |
| S28 Operator schedules any backup job (export, Graph, reindex) on any OS, including Windows paths with spaces/metacharacters | R14, R12 | job validated at schedule time (non-backup verbs, interactive flags, missing secret file refused); Windows wrapper quotes every token; job logs rotate; descriptor written; per-archive default name | MA-72, MA-73, MA-93, MA-97 |
| S29 Operator (or the GUI) asks whether the archive is healthy and the backup is running | R18, R12 | last-run record present and truthful; status GREEN/WARN/RED with remedies; three-state install detection; GUI job file round-trips | MA-75, MA-76, MA-77, MA-78 |
| S21 A hostile email (script, tracking pixel, remote CSS, `<base>`, meta refresh) is archived and opened from disk | R19, R7 | renders inertly: policy meta precedes the mail's markup; refresh neutralized; content preserved | MA-80 |
| S22 `serve` faces a hostile archive or network: script in a body/snippet, a symlink inside the archive, a non-loopback bind | R19, R4, R8 | UI/API/file responses carry a strict policy; snippets are escaped; symlinked paths are 404; non-loopback bind warns | MA-81, MA-82, MA-83, MA-84 |
| S30 Two mailboxes with the same store display name archived into one `-out`, or an archive upgraded from a pre-store-scoped key format | R6, R3, R2, R5, R8 | each store gets its own tree under a distinct, sticky token (the second `segment~hash`); the same mail in both stores is exported to both; an incremental re-run exports zero; an older archive re-scopes its manifest and index once, by content (from each record's own path), logged, and self-heals after an old-binary excursion without double-prefixing | MA-128, MA-129, MA-130, MA-131, MA-134 |
| S31 An archived file is bit-rotted, truncated, deleted or replaced (or predates fixity), or a manifest path is tampered, and the operator runs `verify` | R1, R4, R5 | verify re-hashes every recorded file under the archive lock and classifies each ok/modified/missing/unrecorded, reporting stray files as unexpected; a clean archive with full fixity exits 0, any modified/missing/unrecorded exits 2 (each named, `verify -record` offered for unrecorded), a refusal/error exits 1; recorded paths are validated and resolved component-wise, symlinks and non-regular files are integrity failures never followed or opened, and reads stop at recorded size+1; `-record` baselines current bytes and `status` counts coverage | MA-135, MA-136, MA-137, MA-138, MA-139, MA-140, MA-141, MA-142 |
| S32 An inheritor or auditor needs a message's original internet headers, and a hostile message forges its Received/Authentication-Results lines | R7, R19, R3 | the transport-header block is kept as stored (PST 0x007D decoded; raw sources' header section within 64 KiB) and shown in a collapsed, escaped, "unverified", 64-KiB-capped panel — never executed, never indexed, never in the fingerprint | MA-143, MA-144, MA-145 |
| S33 The search index (search.db) is lost, corrupted, or left out of a copy, but the manifest and the archived `.html`/`.eml`/`.zip` files survive; the operator runs `reindex -rebuild` | R13, R8, R5, R4 | the index and folder pages are reconstructed from the archive alone (no source, no network): a record with a preserved `.eml` is parsed from those bytes, otherwise its fields are re-derived from the archived page (searchable text, not the wire bytes); the fresh index is built into a temp db and renamed over search.db only on success, carrying no orphaned FTS row; a record whose file is missing or fails the path gate is pruned-and-reported and never read (`..`/absolute/symlinked-component/oversized skipped); a per-record read/parse/zip error is isolated, never fatal; the summary reports the from-eml vs re-derived split and the unrecovered-field count; plain `reindex` on a lost index names `reindex -rebuild`, and rebuild with no manifest refuses naming `.mailarchive-manifest.json` | MA-171, MA-172, MA-173, MA-174, MA-175, MA-176 |
| S34 Operator migrates the archive out with `extract` (mbox/eml), or points `-dest` at/inside/over `-out`, or a preserved `.eml` is symlinked/oversized/fixity-mismatched, or the archive has no preserved originals | R20, R4, R5, R12 | a `-raw` archive emits one mboxrd file per folder (`>From `-quoted, net/mail round-trips the boundaries) or a byte-exact `.eml` tree; a re-run does not double (temp+rename, truncate); a PST-only/no-raw archive emits nothing, names the skipped count and reason and exits the partial code (3); a `-dest` overlapping `-out`, a `../`/absolute/symlinked `-dest`, a symlinked/oversized `.eml` and a `Fixity.EML` mismatch are refused/skipped; extract writes nothing into the archive and is refused as a scheduled job; capture and `verify` warn when a raw-capable source is archived without `-raw` | MA-181, MA-182, MA-183, MA-184, MA-185, MA-186 |
| S35 A message carries read/importance/sensitivity state at capture (PST flags; mbox/maildir Importance/X-Priority/Sensitivity headers or the maildir "S" flag; Graph's listing `$select`), and later that state changes | R7, R3, R1 | the reader captures Importance/Sensitivity/Unread; the message page shows them in one HTML-escaped "Status" row when any is set (no row when none is), reversible so `reindex -rebuild` reads them back; the fields are a snapshot excluded from contentHash and Fingerprint and not indexed, so a message marked read or re-prioritised is still the same message | MA-177, MA-178, MA-179, MA-180 |
| S36 A message carries operator/source categories at capture (the PST named `Keywords` property; Thunderbird's `X-Mozilla-Keys`; Graph's `categories` array), and that classification later changes | R7, R3, R1 | the reader captures Categories (the multi-valued unicode blob parsed under hard pre-allocation bounds and a localized recover); the page shows them in their OWN HTML-escaped, control-stripped "Categories" field (never the " · " Status line), capped in number and total length, recovered per-span by `reindex -rebuild`; the fields are a snapshot excluded from contentHash and Fingerprint and not indexed, so a re-classified message is still the same message | MA-190, MA-191, MA-192, MA-193, MA-197 |
| S39 A v3 archive keyed by the pre-v4 folder-scoped format is opened by the live path, or two messages reuse one Message-ID across folders | R1, R2, R3, R5 | records sharing a (token, identity) with an equal, non-empty fingerprint collapse to one mailbox-wide LiveKey record (first-captured file kept, R13; folder-over-time recorded in the history log so no location is lost); distinct-fingerprint reuses stay #fp-qualified siblings (no silent drop); the v2→v3 re-scope is version-gated so a folder-less one-NUL live key is never mis-read as v2; the append-only history log round-trips and tolerates a torn tail (write-truncate + read-skip); Load refuses a stored version above 4 | MA-198, MA-199, MA-200, MA-201 |
| S38 A live-source (Graph) message moves between folders across runs, or a distinct message reuses an already-archived Message-ID in another folder | R3, R17, R1, R2, R5 | one physical copy is kept at its first-captured folder (R13); an incremental re-run records the move as a manifest folder change (Folder/LastSeen/Present) + a history folder-assertion + a body-free index folder update, with no re-download; a distinct id-reuse fails the pre-download envelope-signature check, is downloaded and kept as a #fp-qualified mailbox-wide sibling (no silent drop); a full run re-materialises no per-folder duplicate; the crash order (history append+fsync → index → manifest anchor) never advances the manifest past the events that explain a move; a no-Message-ID message deduped on its content hash still stamps LastSeen and follows a move at the exporter's skip (re-downloaded each run, the acknowledged no-mid price) | MA-202, MA-203, MA-204, MA-205, MA-206 |
| S37 An operator asks what a mailbox looked like on a past date, or a message is deleted online and later restored | R3, R5, R1, R2 | the append-only history log is the go-back timeline: a message absent from a FULL, lock-held mailbox walk is marked gone AFTER the walk (a {k,gone} event + manifest Present=false), its first-captured file KEPT on disk (a timeline event, not a redaction — R13/T5), scoped to the folders actually walked so a message in an excluded/unwalked folder — or any message during a partial/aborted run — is never marked gone; a re-observed message emits a present-again folder-assertion (Present false→true); the fold of the log to a date D is that day's folders/messages, which `serve` renders intersected with the files on disk (the point-in-time view, slice C) | MA-207, MA-208, MA-209 |

Acknowledged limits (not defects): two messages that reuse one Message-ID with
an identical envelope (subject, sender, recipients, date, attachment names) and
differ only in body are treated as one message (a resend); very large attachments are buffered whole in
memory (bounded by the largest single attachment, not the mailbox); the Graph
incremental fast-path recognises an already-archived message by an
envelope-signature match on its Internet-Message-ID (so a distinct message
reusing an id is downloaded and kept as a #fp sibling, R1), but a message with NO
Internet-Message-ID is deduped mailbox-wide on its post-download content hash and
so is re-downloaded on every run — its identity is only knowable once the body
arrives (the acknowledged no-mid price); every manifest record
carries a 16-hex content fingerprint (~16% growth) so that Message-ID reuse is
detected for local sources; PST message categories are read from go-pst's flat
`StringToID["Keywords"]` map, so a store that also defined an unrelated named
property called `Keywords` in another property set could shadow the categories
one (an unlikely, acknowledged limit), the multi-value parse is bounds-checked
and best-effort, and real categorized-PST validation is lab-pending (MA-197).
Recorded here so a finding against them is a design conversation, not a silent
gap.

## 4. Test specs

Tiers: **U** unit property (every commit) · **S** structural whole-tree walk
(every commit) · **A** fail-closed, infra-free · **L** lab (real infra).

| ID | Tier | Asserts | Covers |
|---|---|---|---|
| MA-01 | U | SanitizeSegment strips separators/illegal/reserved (reserved names with any extension too), trims dots, `..`→placeholder | R4, S1, S12 |
| MA-02 | U | SanitizeSegment bounds segment length | R4 |
| MA-03 | U | SanitizeFilename preserves extension, drops path separators, never ends in a dot/space | R4, S1 |
| MA-04 | U | Slug is filesystem-safe, stable, keeps letters/digits of every script, drops bidi overrides, NFC-normalized (segments too) | R4, R6 |
| MA-05 | U | ShortHash is deterministic and collision-distinct | R3 |
| MA-06 | U | ParseSince relative windows (`30d`,`4w`,`12h`) | R11 |
| MA-07 | U | ParseSince absolute dates | R11 |
| MA-08 | U | ParseSince rejects garbage with an error (no silent zero) | R11, R12 |
| MA-09 | U | manifest Key (the one-shot LOCAL-import key) is store- and folder-scoped (same identity in a different folder, or a different store, → different keys); the live path keys per mailbox (LiveKey), not per folder | R3, R6, S4 |
| MA-10 | U | a missing manifest loads as empty, not an error | R5 |
| MA-11 | U | manifest Add/Save/reload round-trips; atomic write | R5, R2, S2 |
| MA-12 | U | decodeBytes returns UTF-8 for UTF-8 and Windows-1252 for legacy bytes | R1 |
| MA-13 | U | mbox reader extracts headers/body/Message-ID (plus Bcc, Reply-To, In-Reply-To, References, the original Date offset and the raw bytes); single file has no double-nest | R1, R3, R10, S4, S7, S27 |
| MA-14 | U | IsMailStoreDir detects mbox/maildir dirs, rejects a dir of `.pst` | R6 |
| MA-15 | U | maildir reader reads cur/new; folder = dir name | R1, R6 |
| MA-16 | S | walking a real PST fixture yields ≥1 mail item with subject | R1, R6 |
| MA-17 | U | plain body is HTML-escaped; header block + charset present | R7 |
| MA-18 | U | a full-HTML message keeps its doc and gets the metadata header injected | R7 |
| MA-19 | U | inline cid image embedded as data URI and excluded from the zip | R7, S5 |
| MA-20 | U | a zero-byte attachment is skipped and its name returned as empty | R1, S6 |
| MA-21 | U | empty-attachment and unresolved-cid produce verification issues | R1, S6 |
| MA-22 | S | full → incremental(0 new) → full lifecycle; html count matches | R2, R6, S2, S3 |
| MA-23 | U | index Add then Search (text, filters, facets); replace-by-key no dup | R8 |
| MA-24 | U | the SQLite build has FTS5; bm25 ranking + snippet work | R8 |
| MA-25 | U | server /api/search, /api/facets, /files serve the exported set | R8 |
| MA-26 | U | IsImapStore true under ImapMail, false for Local Folders | R9 |
| MA-27 | U | account matched by directory; EnableOffline backs up + is idempotent | R9, S10 |
| MA-28 | U | StableWaiter reports stable only after no growth for the window | R9 |
| MA-29 | U | SanitizeSegment/SanitizeFilename neutralize traversal, absolute paths, separators, reserved names | R4, S1, S12 |
| MA-30 | U | zip entry names carry no path separator or `..` (zip-slip contained) | R4, S1 |
| MA-31 | U | malformed/garbage/oversized input parses to a stub without crashing | R10, S7 |
| MA-32 | U | FTS search treats operators/quotes as literal terms (no error, no injection) | R8, S8 |
| MA-33 | U | CLI refuses missing `-out` / bad `-mode` with a typed non-zero naming the problem, no panic | R12, S9 |
| MA-34 | U | CLI `serve`/`search` with no index refuses naming the missing index | R12, S9 |
| MA-35 | U | running detected by lock-PID liveness: live lock = running; stale/dead-pid lock and persistent `.parentlock` = not running | R9, S10 |
| MA-36 | U | reading a mail store leaves its bytes unchanged | R9, S11 |
| MA-37 | U | fallback identity distinguishes messages differing only in body; Message-ID wins | R3, R1, S4 |
| MA-38 | U | archived mail is served under a CSP blocking scripts/remote loads + nosniff | R4 |
| MA-39 | U | root and each subcommand emit help naming their flags (UX contract X3) | R12 |
| MA-40 | U | reindex prunes index rows + manifest entries whose exported file is gone | R13, S13 |
| MA-41 | U | reindex keeps present files searchable (survivors not dropped) | R13, S13 |
| MA-42 | U | reindex regenerates folder pages so the pruned message is no longer listed | R13, S13 |
| MA-43 | U | index EachRow enumerates rows; DeleteByID/DeleteByKey remove from docs + docs_fts | R13 |
| MA-44 | U | manifest Delete removes an entry (persisted); absent key is a no-op | R13 |
| MA-45 | U | cron line: interval schedule fields + exe/job flags incl. -log, no shell redirect; block carries the marker | R14, S14 |
| MA-46 | U | launchd plist: label, ProgramArguments, StartCalendarInterval (Minute/Hour/Weekday by cadence), StandardErrorPath to the .stderr.log sibling | R14, S14 |
| MA-47 | U | schtasks command is now /Create /TN /XML /F followed by /Query /TN (the retired /TR /SC /ST /D SUN are gone, the definition carries them); the install argv is that /Create pair plus the /Query; delete reverses by name and Remove also deletes the wrapper and the XML | R14, S14 |
| MA-48 | U | UpsertCronBlock idempotent (twice = one block); unrelated crontab lines preserved | R14, S14 |
| MA-49 | U | RemoveCronBlock reverses install by marker; empty-crontab remove is a no-op | R14, S14 |
| MA-50 | U | schedule refuses missing -out / bad -interval / install+remove with a typed non-zero naming the problem | R14, R12, S9, S14 |
| MA-51 | U | DiscoverInputs expands a dir to its .pst/.ost and dedups; a mail-store dir is one source | R6 |
| MA-52 | U | decodeMaildirName: dot-split + _XX unescape + drop-empty + sanitize (no traversal) | R15, R6, S15 |
| MA-53 | U | Evolution Maildir++ reader reads root INBOX + every dot-encoded subfolder (nested included) | R15, R1, S15 |
| MA-54 | U | Evolution cache reader walks folders/<f>/{cur,new}, nested directly and via subfolders/ | R15, R1, S16 |
| MA-55 | U | Evolution detection is exclusive (maildir++/cache/plain) and both are a single mail store | R15, R6, S15, S16 |
| MA-56 | U | DataFileReadable drops empty/corrupt Outlook stubs from auto-discovery but keeps a valid or unopenable (locked) file | R1, S17 |
| MA-57 | U | Open contains a go-pst parse panic on a corrupt .pst/.ost as a clean error (no crash); DataFileReadable stays panic-safe | R10, S18 |
| MA-58 | U | pstFileName yields a bare, in-root .pst name (no separator/traversal); empty account gets a fallback | R16, R4 |
| MA-59 | A | CLI -outlook off Windows refuses with a typed non-zero naming the Windows/Outlook requirement (no crash) | R16, R12 |
| MA-60 | L | on Windows + classic Outlook, -outlook runs Send/Receive (bounded wait), writes a PST per account (AddStoreEx + CopyTo) that archives normally — **pending**, validated on a real Outlook install via `scripts/test-outlook.ps1` | R16 |
| MA-61 | U | Graph client walks the full folder tree + pages all messages + fetches MIME, issuing only GET (read-only) | R17, R9 |
| MA-62 | U | RunGraph captures every message into the pipeline; incremental re-run exports 0 new and re-downloads no bodies | R17, R2 |
| MA-63 | U | graph subcommand refuses missing -out/-tenant/-client-id/-mailbox/secret with a typed non-zero naming the problem | R17, R12 |
| MA-215 | U | Deleted Items and Junk Email are ABSENT from the walked folder set by default — the client resolves their well-known-folder ids (`deletedItems`/`junkemail`) via Graph's well-known-folder endpoint and skips each folder AND its subtree by that id (never a display-name match, which is locale-fragile) — and PRESENT when the FolderFilter opts them in (`-include-deleted`/`-include-junk`); a mailbox lacking a well-known folder (the resolution 404s) simply has nothing to skip; the resolution and walk issue only GET requests (read-only, R17) | R17, S38 |
| MA-216 | U | the schedule preview surfaces the Deleted-Items/Junk choice for a graph job: with neither flag the preview names the default exclusion and the `-include-deleted`/`-include-junk` opt-in flags, and opting in both carries the flags into the canonical job so the preview's command line shows them (default excluded emits no flag); a non-graph (export) job carries and shows no such note | R14, S28 |
| MA-217 | U | the GUI's Deleted-Items/Junk decision function defaults to EXCLUDED on a click-through: includeDeletedJunk maps the default/any non-include answer to (false,false) and only the explicit include item to (true,true); deletedJunkApplies is false for the GUI's local source types, so the question is not put where the walk cannot honor the well-known-id exclusion (the dialog is lab-tier; the pure decision is U-tested) | R17, S38 |
| MA-64 | L | end-to-end against a real M365 tenant (app-only consent, throttling, real folder set) — **pending**, validated on a live tenant | R17 |
| MA-65 | U | reconciled to the XML install: the path-with-spaces guarantee is carried by XML escaping of <Command>/<Arguments> plus the wrapper's own token quoting — a wrapper path with a space, "&" and a non-ASCII rune is escaped (&amp;, never a bare &) and round-trips through xml.Unmarshal to the identical path, a no-wrapper spec's exe lands in <Command> with each argument quoted in <Arguments> so it splits back (C-runtime rules) to the exact args, and the wrapper body still quotes every token | R14, S14 |
| MA-101 | U | the terminal `search` verb and the serve `/api/search` box share one inline-token grammar: from:/folder:/after:/before: tokens return the same hit set as the equal flags and as the API, and a partial `after:YYYY-MM` applies a real month bound (a message before it excluded, one on/after it included) | R8, R11 |
| MA-102 | U | machine search output: `-json` is a valid JSON array of the result fields with the `<mark>` highlight tags stripped, `-paths` prints one archive-relative path per hit (NUL-separated with `-0`), stdout carries only the data (the match-count line goes to stderr), and `-json` with `-paths` is refused with a typed non-zero naming the conflict | R8, R12 |
| MA-161 | U | the plain `search` printer control-scrubs every mail-derived field (sender, subject, folder, body snippet), mapping C0/C1 controls and DEL to a space, so a hostile subject or body carrying ESC/BEL cannot inject a terminal escape sequence; the scrubbed text still prints and a clean fixture is untouched | R8, R19, S22 |
| MA-162 | U | `ParseQuery` accepts a quoted token value (`folder:"Sent Messages"`, `from:'a b'`) so a space-bearing value is one token instead of being re-split into a silently dropped free term; an unterminated quote runs to end of input; an unquoted single-word value is unchanged; the terminal and the serve box share this one parser | R8 |
| MA-163 | U | a scheduled verify job derives a distinct default name (`<archive-default>-verify`) that never overwrites the archive's backup entry; its preview and install print the whole-run-lock / backup-window warning and refuse with a typed non-zero when its interval+time equal the archive's recorded backup schedule | R14, R12, S28 |
| MA-164 | U | the Windows schtasks install preview carries the cadence gloss the cron preview has, labels StartBoundary as local time, and adds the resilience gloss (a run missed while the PC slept is caught up on wake; runs on battery; a night the PC is off is skipped) and the failure-visibility note | R14, S14 |
| MA-165 | U | the archive's root `index.html` front-door note points the reader at `mailarchive verify -out .` to confirm the files are intact | R7, S27 |
| MA-80 | U | Render (both paths) starts `<head>` with the archive CSP meta + no-referrer meta before mail markup; neutralizes mail `refresh`/`content-security-policy` metas; adds a `<head>` when missing; body preserved | R19, R7, S21 |
| MA-81 | U | search snippets are HTML-escaped with only the tool's own `<mark>` tags live, in FTS and browse modes | R19, R8, S22 |
| MA-82 | U | `serve` refuses a path whose resolved location is outside the archive root (symlink escape → 404) and does not list directories lacking an index.html | R19, R4, S22 |
| MA-83 | U | `serve` sends the archive CSP + nosniff for files, and a `script-src 'self'` CSP + nosniff + frame-ancestors none for the UI and API; the UI carries no inline script | R19, R4, S22 |
| MA-84 | U | loopback-address classification: 127.0.0.1/::1/localhost are loopback; an empty host (all interfaces), 0.0.0.0, and LAN addresses are not | R19, S22 |
| MA-143 | U | the readers keep the transport-header block: PST via PidTagTransportMessageHeaders (0x007D) through readTextProperty (proven on the support.pst fixture); mbox/maildir/Graph from the header section of Raw, only when a blank line terminates it within 64 KiB — a body-only blob or a block that runs past the bound yields "" | R7, S32 |
| MA-144 | U | the message page shows a collapsed "Transport headers as stored (unverified)" panel after the header <dl>, the block HTML-escaped inside `<pre>` (a forged `<script>`/entity line is inert), capped at 64 KiB with a visible truncation note, and omitted entirely when the block is empty | R7, R19, S32 |
| MA-145 | U | the envelope Fingerprint (and the fallback content Identity) is unchanged when only TransportHeaders differs — the sender-influenced block is excluded from identity and never indexed; extended to the message-state fields (Importance/Sensitivity/Unread), which are likewise excluded | R3, R1, S32, S35 |
| MA-177 | U | a message's capture-time state is read (PST PidTagImportance/Sensitivity/MessageFlags → low/""/high, ""/personal/private/confidential, Unread from the mfRead bit's absence, proven on the support.pst fixture) and, when any is set, shown in one HTML-escaped "Status" row (data-mailarchive-field="status") whose reversible line `reindex -rebuild` reads back into the model | R7, R3, R1, S35 |
| MA-178 | U | the mbox/maildir reader captures Importance/X-Priority and Sensitivity headers, and sets Unread from the Thunderbird X-Mozilla-Status read bit else the maildir "S" (Seen) info flag threaded from the filename (a message in new/ or lacking the flag is unread; X-Mozilla-Status wins when present) | R1, S35 |
| MA-179 | U | the Graph source's one widened listing $select (id,internetMessageId,receivedDateTime,importance,isRead,sensitivity,categories) populates Importance/Sensitivity/Unread on each message with no extra request; a field the tenant omits stays empty and the incremental fast-path still keys on the id alone | R3, S35 |
| MA-180 | U | the message page shows no "Status" row when none of Importance/Sensitivity/Unread is set | R7, S35 |
| MA-190 | U | parseMVUnicode bounds an untrusted PT_MV_UNICODE blob BEFORE allocating — the declared 4-byte count is clamped to `(len-4)/4` and a small ceiling, no slice is pre-sized from it, offset arithmetic is 64-bit, and each offset must fall in `[headerEnd, len]` and not move backwards (first violation stops) — so a hostile (count 0xFFFFFFFF, offsets out of range) or truncated blob yields only its in-range values with no OOM/panic; the PST `Keywords` named property is read under readCategories' OWN recover (a corrupt node costs only the categories, not the message) and support.pst yields none without error | R10, R1, S36 |
| MA-191 | U | the mbox/maildir reader captures Categories from Thunderbird's `X-Mozilla-Keys` header ONLY (whitespace-separated, control-stripped, de-duplicated, order-stable); the sender-settable RFC `Keywords` header is NOT read, so a sender cannot inject categories; a hostile huge X-Mozilla-Keys value is bounded before allocating (token count capped) | R1, R10, S36 |
| MA-192 | U | the Graph source's one widened listing `$select` adds `categories` (no extra request) and the array reaches the message's OWN "Categories" page field; a message the tenant leaves uncategorized shows no categories field | R3, S36 |
| MA-193 | U | categories render in their OWN escaped, control-stripped `data-mailarchive-field="categories"` field (each value in its own `mailarchive-category` span), NEVER the " · "-joined Status line, capped in number and total rendered length with a visible truncation note; `reindex -rebuild` reads them back per-span losslessly, so a category value that mimics a Status line can never forge Unread/Importance/Sensitivity; the row is absent when none | R7, R19, S36 |
| MA-197 | L | real categorized Outlook `.pst` end-to-end (a store whose items carry the named `Keywords` property) captures those categories into the page's Categories field — **pending**, no categorized `.pst` fixture can be built without Outlook; the parseMVUnicode parser and the graceful-absence path are U-tested (MA-190) | R7, S36 |
| MA-103 | U | a missing -input path is refused before any output dir, lock, last-run record or attention sidecar is created, naming the path; a healthy source exports (positive twin) | R12, S9 |
| MA-104 | U | printSummary prints one coherent Verification line (still-missing/source-empty/not-yet-re-examined/re-examined=Retried/report path only when a report exists) and one -raw no-op WARNING when raw was asked for but no .eml was written | R1, R12 |
| MA-105 | U | export -list previews the stores that would be archived (one per line, rough size, resolving a friendly store label — e.g. an Evolution IMAP cache account name — beside an opaque path) and exits 0 without exporting or creating the output dir | R12 |
| MA-106 | U | the .ost advisory is a pure function: windows + a discovered .ost + classic Outlook names -outlook up front; off Windows, no .ost, or no classic Outlook yields empty | R16 |
| MA-107 | U | a Graph run where every mailbox fails records the first per-mailbox error (with its AADSTS code) in the last-run error and writes no README/index.html scaffold, while manifest and last-run are still written | R17, R18 |
| MA-108 | U | folder pages regenerate only when a run exported something or the root index.html is missing: a first run creates it, a zero-change re-run leaves it untouched, a new message rewrites it | R7 |
| MA-109 | U | effectiveEvery clamps the checkpoint cadence to [floor, 25000] scaling with archive size (n/8); floor is the configured CheckpointEvery | R5 |
| MA-110 | U | reclaimPSTDir removes the temporary -outlook PST scratch dir and reports the bytes reclaimed only after a clean run; a failed run leaves it in place | R5 |
| MA-66 | U | a capture missing content is recorded fillable naming the item; an incremental re-run re-examines it (ignoring -since), rewrites nothing while unchanged, fills it once present (subset promotion: any recovered item promotes), then never re-examines; subject/date only on records with issues | R1, R2, S6 |
| MA-67 | U | a missing body is a fillable "body" gap that fills when the body appears; body-without-attachments is complete; a SourceComplete exporter records gaps as terminal and never re-examines them | R1, S6 |
| MA-68 | U | the report is regenerated from the manifest each run: a gap found earlier is still listed by a later run, disappears when filled (empty report removed); a legacy archive's earlier report is preserved as attachments-report-legacy.tsv | R1, S6 |
| MA-70 | U | a version-1 manifest (incl. one an older binary rewrote) migrates every record to the "unknown" sentinel and counts them; Save writes v2; reload keeps the sentinel; a sentinel record is re-captured once by the next incremental run | R1, R5, S6 |
| MA-71 | U | a Graph message captured with no body is recorded terminal and reported; an incremental re-run re-downloads nothing and retries nothing (R17 unchanged); a legacy sentinel on a Graph archive is resolved without a download | R17, R1, R2, S6 |
| MA-79 | L | on a real Windows box: `schedule -install` imports the generated XML definition on a real host (schtasks /Create /XML then /Query confirms it), writes the wrapper, the task runs the wrapper AUTOMATICALLY at the set time under the logged-in user (no manual start; the interactive-logon principal is accepted), catches up a start missed while the PC slept, the job logs to <out>/<name>.log and crashes to .stderr.log, `-remove` deletes all three and leaves no XML — **pending**, validated by hand per README → Windows notes | R14, S28 |
| MA-95 | L | the GUI wizard on a real desktop: health view on launch with an installed schedule, source re-ask when nothing is found, prep for auto-detected IMAP/.ost inputs, mode question skipped after prep, "mail app open?" only for a data file, Evolution option on Linux, verification counts in the summary, the "keep it current" step installing a job that `-job` then runs headlessly, and real-host `status` detection on each OS — **pending**, walked by hand per docs/review-schedule-v2-friction.md | R18, R14, S29 |
| MA-98 | U | Graph requests are bounded: a server that stalls before headers or mid-body fails within the configured deadline (listing and MIME download); a prompt server succeeds | R17, S29 |
| MA-99 | U | -unattended (baked into every scheduled job) refuses to create a new archive when -out does not exist, naming the likely unmounted drive, and writes nothing; an existing -out is accepted | R14, R12, S28 |
| MA-100 | U | a -copy-first snapshot is created on the archive's own volume, never in the system temp directory | R5, S2 |
| MA-210 | U | `serve` renders the go-back projection server-side over the manifest + folded history, intersected with the files on disk: `/goback` (current) groups each present message under its current folder and HIDES a message swept gone from the live mailbox, while `/goback?at=D` folds the log to the end of day D and shows that same gone message under its THEN-folder for a date before the gone event (and again after a present-again); the compact date track lists the observed run dates newest-first; the static file:// pages are untouched (grouped by first-captured folder, R13) | R21, R3, S37 |
| MA-211 | U | the on-disk intersection is the redaction belt: deleting a message's exported `.html` from the archive (WITHOUT reindex, so its manifest row and history events still exist) makes it vanish from the current view AND from every `?at=D` view — the projection shows only keys whose file is present on disk — so a removed message shows at NO date even before the log is compacted | R21, S37 |
| MA-212 | U | the go-back page is script-free and CSP-locked (`/goback` carries a `default-src 'none'` policy with no script-src and no inline `<script>`, R19); a hostile history event is inert — a folder name or subject bearing markup/control chars is HTML-escaped by contextual templating (never injected), a `?at` that is not a valid `YYYY-MM-DD` (a time component, an out-of-range field, a traversal attempt) is rejected without crashing and without echoing the raw value, and an event key that is not a manifest record present on disk surfaces no message | R19, R21, S37, S22 |
| MA-213 | U | history-log recovery legibility (X6): a MISSING log makes `serve` announce "go-back unavailable" (not silently current-only) and `status` report "History: none recorded" with GREEN posture; a torn/corrupt log makes `serve` announce "go-back partial" and `status` WARN with a remedy naming the log and `reindex`; a clean log reads "go-back available" and GREEN; `status -json` carries a `history {exists,runs,events,bad_lines,torn_tail}` object and the posture reason code (`history_torn_tail`/`history_corrupt`) | R21, R18, S37 |
| MA-214 | U | `reindex` compacts the history log so redaction spans all dates: after pruning the index/manifest rows of files gone from disk it drops every per-message event whose key is no longer in the reconciled manifest (a removed message), while KEEPING run headers/footers, folder renames and the events of a message merely departed from the mailbox whose file is still on disk (R13); the rewrite is atomic (temp+rename) and a no-op when nothing is redacted | R21, S37, R13 |
| MA-181 | U | `extract -format mbox` on a -raw archive writes one mboxrd `.mbox` per folder that round-trips through net/mail (correct message boundaries, count preserved) and `>`-quotes a body `From `/`>From ` line so it is not mistaken for a boundary; the folder tree is mirrored | R20, S34, R12 |
| MA-182 | U | `extract -format eml` mirrors the folder tree with one byte-exact `.eml` per record; a re-run into the same -dest does NOT double (temp+rename over the existing file, never appended) and -dest empty-or-`--overwrite` is enforced | R20, S34, R5 |
| MA-183 | U | a PST-only (or no-raw) archive emits nothing: every record is skipped, the summary names the skipped count and reason ("no preserved original"), and the run exits the partial code 3 (never 0, never verify's 2) | R20, S34, R12 |
| MA-184 | U | extract trusts nothing: a `-dest` equal to/inside/containing `-out` (symlink-resolved) is refused naming the overlap; a symlinked, oversized or fixity-mismatched `<stem>.eml` is skipped-and-reported, never read/emitted; and extract writes nothing into the archive (positive twin first, assure.Refused + NoSideEffect) | R20, R4, R5, S34 |
| MA-185 | U | `schedule -- extract -out X` is refused with a typed non-zero message calling extract an operator-driven migration, not a backup; `extract -h` names -out, -format and -dest and states the lock/backup-window posture | R20, R12, S34 |
| MA-186 | U | the capture-time warning fires when a raw-capable source (mbox/maildir/Graph, RawAvailable>0) is archived WITHOUT -raw ("extract will produce nothing … re-run with -raw") and is ABSENT with -raw; `verify` on an archive with no preserved `.eml` prints the same not-extractable note | R20, S34 |
| MA-188 | U | extract's write side is symlink-strict like its read side: a symlink planted as a directory component under -dest is refused, the record skipped and reported, and nothing is written through it to the target | R20, R4, S34 |
| MA-122 | U | the GUI's "open the archive" command is built per-GOOS without executing: xdg-open (Linux), open (macOS), rundll32 url.dll,FileProtocolHandler (Windows), each carrying the path | R18, S29 |
| MA-123 | U | the launch health view offers "repair" only for the states a re-install fixes (not-installed / moved / missing program, same host), never for a healthy or other-host schedule; the rebuilt Spec uses the CURRENT exe, takes only the cadence/time from the descriptor, and reconstructs the job args and log from the sanitized name and -out (with a name-derived wrapper path), and validates | R18, S29 |
| MA-124 | U | a failed headless (scheduled) run raises exactly one desktop notification naming the backup; a healthy run raises none | R18, S29 |
| MA-125 | U | the GUI success summary keeps the counts status reports (X6) but in plain words: filled → "newly downloaded", fillable+unknown merged into one "not fully downloaded yet" line, terminal → "empty at the source" (omitted at 0), keep-raw explains .html vs .eml, whole-archive search points at the CLI | R18, S29 |
| MA-126 | U | the keep-raw question is asked only for raw-capable sources (never the Outlook picks) and rides into the job file; on Windows with classic Outlook a discovered live .ost steers to the Outlook-app path | R18, R16, S29, S19 |
| MA-127 | U | the "nothing found" guidance is a pure function of GOOS (macOS names Apple Mail / New Outlook and the Graph path; Windows/Linux name New Outlook and the Graph path), and the Outlook-app scratch dir <out>/_outlook-pst is removed after a successful export and kept on failure, its reclaimed-space log line human-readable (HumanBytes, names the directory) not a raw byte count | R18, R16, S29, S19 |
| MA-166 | U | a tampered schedule descriptor cannot redirect the repaired job: repairSpec ignores the (untrusted) descriptor's Job/Log/Exe and reconstructs the args from the sanitized name and the log from -out (the current exe), so an attacker who rewrites the descriptor's `-job` at a file they control does not get it installed | R14, S29 |
| MA-167 | U | the headless start-failure breadcrumb (config-dir last-failure.json) round-trips name/time/reason, is written atomically owner-only, and has control characters stripped on read before its strings reach a dialog | R18, S29 |
| MA-168 | U | the launch health view surfaces a recorded headless start-failure only when there is one and either the archive is unreachable (its own last-run record cannot speak) or the failure is newer than the archive's last recorded run (a later successful run supersedes it) | R18, S29 |
| MA-169 | U | after a successful Repair the wizard offers to continue into the export wizard (OK) or stop quietly (Cancel), and the keep-raw question is skipped when every input of a raw-capable run is an Outlook data file (no raw bytes to keep) while a mixed/folder/non-Outlook run still asks | R18, R16, S29, S19 |
| MA-75 | U | status posture rules: healthy GREEN; fillable/unknown/index-behind/no-schedule/never-ran/unreadable/stale/other-host/scheduler-unavailable/not-installed/moved-binary/cancelled WARN; failed/never-finished/exe-missing RED, each with a remedy; liveness of a running record comes from the archive lock, not a pid; auth failure adds the rotation remedy; the CLI reports a real archive (exit 0), falls back to the last-run record when only that exists, and refuses a directory with neither manifest, descriptor nor last-run record | R18, R12, S29 |
| MA-76 | U | a run writes a running record first and finalizes it ok/failed/cancelled with counts, error and pid; absent vs unreadable records are distinguished | R18, R5, S29 |
| MA-77 | U | the GUI job file round-trips inputs/auto/out/mode/since/copy-first/outlook/raw, is owner-only, ignores unknown fields, refuses a newer version or a missing out | R18, S29 |
| MA-78 | U | install detection is three-state (installed / not installed / scheduler unavailable) over injected crontab, schtasks and launchd evidence | R18, S29 |
| MA-72 | U | schedule refuses a non-backup verb (serve/search/status/schedule), an interactive flag, a Graph job without -client-secret-file, a flat flag mixed with a -- job, a bad -name, and a control character in any argument, each naming the problem; valid export, graph and verify -- jobs preview with -log and -unattended and are not applied; the secret never appears in output | R14, R12, S28 |
| MA-74 | U | -client-secret-file: a missing, empty, oversized, world-readable (Unix) or non-regular (symlink/pipe) file is refused naming the requirement, at run time as at schedule time; the secret never appears in output; a proper file is accepted | R12, S28 |
| MA-73 | U | the Windows wrapper quotes every token, doubles %, redirects stderr to the .stderr.log sibling, never enables delayed expansion; the task registers from the XML definition (/Create /TN /XML /F) whose <Command> is the wrapper path; the direct form still works without the wrapper (the program lands in <Command>) | R14, S28 |
| MA-93 | U | the run log appends with a header per run and rotates to .1 past the size cap; a symlink at the log or rotation path is refused untouched | R14, R4, S28 |
| MA-97 | U | Install's descriptor round-trips (name, cadence, real exe + size/mtime, job, host); Remove deletes it; DefaultNameFor is stable per archive; SanitizeName bounds length and characters; a read descriptor is control-character-free with its name/cadence validated; Install refuses to replace another archive's schedule (descriptor or cron block mismatch); Validate refuses control characters and a relative wrapper path | R14, R12, S28 |
| MA-146 | U | the Windows Task Scheduler XML (v1.2, UTF-16LE+BOM, encoding="UTF-16") for daily and weekly specs round-trips through xml.Unmarshal to the identical <Command> wrapper path (space, "&" escaped as &amp;, and a non-ASCII rune survive), the right calendar schedule (ScheduleByDay/1 or ScheduleByWeek/1+Sunday, Enabled), and the resilience settings (StartWhenAvailable=true, batteries allowed, IgnoreNew, WakeToRun=false) | R14, S28 |
| MA-147 | U | nextOccurrence returns the first fire at or after now — today when the time is still ahead, tomorrow (daily) or the next Sunday (weekly) when it has passed or is exactly now — so a fresh task's StartBoundary is never in the past (FC13) | R14, S14 |
| MA-148 | U | the Windows install preview prints the schtasks /Create-from-XML command and the definition body decoded to UTF-8 (no raw UTF-16 bytes), naming the XML path; Remove's side-file cleanup deletes both the wrapper and the XML, tolerating either (or the wrapper path) being absent | R14, S14 |
| MA-170 | U | the Windows task definition makes the run identity explicit so auto-start does not depend on what schtasks infers from a principal-less document: an InteractiveToken principal referenced by the Actions Context, and the task and its trigger both Enabled | R14, S28 |
| MA-111 | U | the no-schedule WARN shapes its remedy from the archive's recorded job (its -input/-auto, never a hard-coded -auto) and falls back to a generic phrase for older records with no job; it stays WARN and acknowledges an external scheduler (systemd timer, NAS task) | R18, S29 |
| MA-112 | U | the staleness WARN is OS-aware: cron hosts read "was the machine on (and crond running)"; launchd/schtasks hosts keep "logged in" | R18, S29 |
| MA-113 | U | status's completeness labels match the export/GUI wording (still missing content · source-empty (never fillable) · not yet re-examined) with a plain-language gloss; the report path is cited only when it exists on disk; Summary prints the archived date range when the index has one | R18, S29 |
| MA-114 | U | a torn/unreadable last-run record WARNs "corrupt or truncated" and does not echo the raw decoder error | R18, S29 |
| MA-115 | U | health.JSON / `status -json` emits a typed, versioned document (version, posture, reasons, messages, indexed, fillable, terminal, unknown, last_run, schedule, out) and the exit stays 0 | R18, S29 |
| MA-116 | U | Assess WARNs when the installed schedule's job -out targets a different archive (naming both paths and the re-install/remove remedy), and when the archive's -out sits inside a cloud-sync folder (naming the service and the remedy) | R18, S29 |
| MA-117 | U | util.UnderCloudSync detects a OneDrive-synced path by a path segment (equal to or prefixed by OneDrive, e.g. "OneDrive - Company") and by the OneDrive* environment roots; a non-synced path is not flagged | R18, S29 |
| MA-118 | U | index.Range returns the oldest and newest dated messages (date>0) and ok=false on an empty index | R8, R18, S29 |
| MA-90 | U | a message page links its folder page and the root, lists archived (not inline) attachments with a zip link, shows Message-ID, Sent/Received when both known, Reply-To, Bcc; times keep their original offset; a bare render carries no links | R7, S27 |
| MA-91 | U | folder pages paginate at pageSize with prev/next and root links, every message on some page, no extra empty page; the date column is labelled UTC; the root page links each folder's first page and README.txt | R7, S27 |
| MA-92 | U | Run writes README.txt at the archive root naming the layout, UTC convention, tool-free browsing, search.db (SQLite), the report and the manifest; reindex restores it | R7, S27 |
| MA-96 | U | with KeepRaw a message's original bytes are preserved byte-identically as <stem>.eml and linked from the page; none for a message without raw bytes; nothing without KeepRaw | R1, S27 |
| MA-89 | U | a folder segment that sanitization had to alter (illegal chars, truncation) carries a deterministic ~hash suffix from the original name so distinct source folders never merge; whitespace tidying adds none; the exporter shrinks the subject slug when the relative path would exceed 200 characters | R6, R4, S26 |
| MA-85 | U | an exclusive lock on <out>/.mailarchive.lock is held for the whole run (export, Graph and reindex): a second Acquire fails naming the path and holder; a planted symlink at the lock path is refused untouched; a held lock notices removal/replacement (StillHeld); the holder line is control-character-free; the CLI refuses a locked archive with a typed non-zero naming the lock (no manifest written) | R5, R4, R12, S25 |
| MA-86 | U | two messages with the same Message-ID but different envelopes in one folder are both exported and recorded (the second under a fingerprint-qualified key joined by NUL, which no crafted Message-ID — even one literally containing "#"+fingerprint — can pre-occupy, so a third distinct message reusing the id is also kept, never silently dropped; the qualified key holds three NULs, leaving the v2→v3 discriminator untouched) (also while the first is still incomplete); a fill of an incomplete message keeps its key; incremental re-run exports zero; a full re-run in reversed order yields the same file names | R3, R1, S23 |
| MA-149 | U | one physical store archived under different -input spellings (relative, absolute, a symlinked component, a trailing separator, letter-case on case-insensitive volumes; Graph: mailbox case) resolves to one canonical store id and token, so a re-run exports zero and writes no duplicate tree; a manifest keyed by a raw spelling is migrated to the canonical id on load and two spellings that resolve to one id collapse onto the plain-segment token | R2, R3, R6, R8, S30 |
| MA-150 | U | a tampered manifest store token that is not a single safe path segment (".."/absolute/containing a separator/control chars) cannot make the exporter write outside -out: Load drops it, Token re-derives a fresh in-root token, and Export refuses it with a typed error naming the token; nothing is created outside the output root | R4, S30 |
| MA-151 | U | when a run loses its lock mid-walk (the lock file removed or replaced) it stops at the next message boundary and writes NO shared state into the archive another run may own — no manifest rewrite after the loss, no README scaffold — returning the typed lock-loss error naming the lock; the positive twin (lock intact) completes and scaffolds | R5, R12, S25 |
| MA-152 | U | self-heal after an old-binary excursion keeps the canonical file as the record of truth: when a re-scoped legacy (v2) key collides with a surviving v3 key, the pre-existing survivor's record (canonical path) is kept and the legacy entry (excursion duplicate) dropped, so verify checks the canonical file and never flags it as unexpected while trusting the duplicate | R8, R2, S30 |
| MA-153 | U | a run that FAILS prints only the failure, never a misleading "Done. exported=0 … manifest=0" summary first: a run refused inside app.Run (archive in use) emits "FAILED" and "in use" with exit 1 and no "Done." line; the positive twin (a healthy export) prints the "Done." summary | R12, S25 |
| MA-87 | U | a file-stem collision between two keys lengthens the second stem from its own key (deterministic, no overwrite); re-exporting a message under a new name removes its previous html/zip | R4, R6, R13, S24 |
| MA-88 | U | the manifest and index are checkpointed every CheckpointEvery messages inside a store walk, so a hard crash keeps the progress made | R5, S2 |
| MA-198 | U | the fingerprint-safe v3→v4 collapse (CollapseByIdentity) unifies same-(token,identity) records with an EQUAL non-empty stored fingerprint into one mailbox-wide LiveKey record (first-captured file kept; FirstFolder/FirstSeen earliest, current Folder/LastSeen latest) while DIFFERENT-fingerprint reuses of one Message-ID stay separate #fp-qualified siblings (no silent drop, R1); each unified-away loser is returned for the caller to record and prune (file left on disk, R13); the identity index maps the id to every surviving key | R1, R2, S39, S30 |
| MA-199 | U | Load fills the v4 timeline defaults on a pre-v4 record (Present=true, FirstFolder=Folder, FirstSeen=LastSeen=ExportedAt); the in-place field merge rewrites only Folder/LastSeen/Present (the no-download move merge) and preserves Fixity/Fingerprint/ExportedAt/FirstFolder/FirstSeen/completeness; a merge on an absent key is a no-op | R2, R5, S39 |
| MA-200 | U | the append-only history log round-trips its run header / folder-assertion & gone / folder-rename / run-completed footer; the reader skips a torn last line and open-for-append truncates it (both-side torn-tail defence); Fold replays folder-assertion/gone/present-again to the latest transition at ≤ D | R5, R2, S39 |
| MA-201 | U | v4 key-format discrimination is version-gated: a folder-less one-NUL live key in a v4 manifest is not re-scoped (Rekeyed==0) though the identical bytes are re-scoped under a v2 manifest; Load refuses a stored version above 4 (version 5) naming the file and the upgrade remedy, byte-unchanged | R5, R12, S30, S39 |
| MA-202 | U | an incremental Graph re-run over a message MOVED between folders records the move as a manifest folder change (Folder/LastSeen/Present) + a history folder-assertion event + a body-free `UPDATE docs SET folder`, keeping ONE physical file at its first-captured folder and downloading no body (envelope signature matches, so no re-fetch); the timeline log carries the run header, the new-capture assertions, and the move | R17, R3, R1, R5, S38 |
| MA-203 | U | a DISTINCT message reusing an already-archived Internet-Message-ID in another folder fails the pre-download envelope-signature check, is downloaded, and is filed as a #fp-qualified mailbox-wide sibling — both survive (R1, MA-86 mailbox-wide); the exporter's mailbox-wide keying splits it exactly as the folder-scoped path splits a within-folder reuse | R1, R3, R17, S38 |
| MA-204 | U | Exporter.DedupMailboxWide off keeps the folder-scoped key so a one-shot local import stores the same mail in each folder (R3); on keys mailbox-wide by identity + envelope signature so one message is one file across folders and modes, and a full run re-materialises no per-folder duplicate (R2, full still re-exports all) | R3, R2, R17, S38 |
| MA-205 | U | the crash-safe durability order is history append+fsync → index flush → manifest.Save (the trailing anchor), applied at every checkpoint and at run end, so the manifest — the fold-to-now projection — never advances past the history events that explain it (a move is never lost; a crash costs at most a duplicate, idempotent, event) | R5, S38 |
| MA-206 | U | a no-Internet-Message-ID Graph message is deduped mailbox-wide on its post-download content-hash identity: it reaches the exporter's mailbox-wide skip (not the pre-download fast-path) so it is re-downloaded each run (the acknowledged no-mid price), but EVERY observation — including the write-nothing skip — stamps LastSeen=thisRun (so a still-present message is never wrongly marked gone) and, when the observed folder differs, records the move (Folder field via MergeFields + a history folder-assertion + a body-free index folder update), keeping ONE physical file at its first-captured folder (R13) | R3, R5, S38 |
| MA-207 | U | gone-detection by full reconciliation (SweepGone): after EVERY non-excluded folder of a Graph mailbox is fully walked in a lock-holding run, a message deleted online (absent from the walk) is marked gone — a {k,gone} history event + manifest Present=false — while its first-captured file is KEPT on disk (a timeline event, not a redaction, R13); the gone conclusion is drawn ONCE at the walk's end, never at a checkpoint, and the record keeps its last known Folder/LastSeen so a past view still shows where/when it last lived | R5, R1, S38, S37 |
| MA-208 | U | gone-detection is SCOPED to the folders actually walked AND to a COMPLETED, lock-held walk: SweepGone marks only a still-Present record of the swept token whose folder was walked and whose LastSeen precedes this run — it never touches a record in an excluded/unwalked folder (retained), one observed this run (LastSeen==thisRun), one already gone, or another mailbox token's record; and an incremental Graph run whose lock is lost mid-walk returns BEFORE the sweep (re-checked via abort), so a deleted-online message stays Present and no {k,gone} event is written — a partial/aborted run marks nothing gone | R5, R1, S38, S37 |
| MA-209 | U | a Graph message marked gone that RE-APPEARS in a later run emits a present-again folder-assertion (the fast-path records an assertion when the pre-observation record was not Present, even if the observed folder is unchanged) and the manifest record returns to Present=true under its observed folder, so the folded timeline reads absent between the gone and the present-again and present thereafter (§3.3) | R1, R5, S38, S37 |
| MA-128 | U | two stores with the same display name archived into one -out get distinct, sticky tokens (the second `segment~hash`) so both export into their own tree and an incremental re-run exports zero | R6, R3, R2, S30 |
| MA-129 | U | two distinctly-named stores each keep their plain sanitized segment as token, export into separate trees, and an incremental re-run exports zero | R6, R2, S30 |
| MA-130 | U | an archive whose manifest and search index predate store-scoped keys re-scopes both on first open (by content, from each record's own path), an incremental run then exports zero, and a second load re-scopes nothing (Rekeyed==0) | R5, R2, R8, S30 |
| MA-131 | U | a manifest and index holding a mix of one-NUL (legacy) and two-NUL (current) keys — an old-binary excursion — are repaired by content: legacy keys are re-scoped, current keys are left untouched (never double-prefixed), and a collision leaves exactly one key/row per message | R5, R8, S30 |
| MA-132 | U | a manifest, and a search index, written by a newer mailarchive (a higher stored version) are refused naming the version and the upgrade remedy, without mutating the file | R5, R12 |
| MA-133 | U | the sentinel migration fires only for a literal version-1 manifest: a complete version-2 record stays complete (never re-sentinelled) after the v3 re-scope | R1, R5, S6 |
| MA-134 | U | the one-time upgrade is logged: the manifest re-scope count and the index repair line ("migrating index keys (N rows)") both appear in the run log | R5, S30 |
| MA-135 | U | the exporter records a sha256+length for every file it writes (html always, zip/eml when present) at write time; a fresh archive verifies as attested (exit 0), and `-json` carries the coverage fields records/with_fixity/checked | R1, R5, S31 |
| MA-136 | U | a flipped byte in an archived file is reported `modified` naming the path, and a deleted zip is reported `missing`, each exit 2; the same archive was attested (exit 0) before the damage | R1, R5, S31 |
| MA-137 | U | a record with no recorded fixity is `unrecorded`, exit 2, and the summary names `verify -record`; `verify -record` baselines the current bytes (labelled a baseline, not proof of pristineness) and a following verify exits 0 | R1, R5, S31 |
| MA-138 | U | a stray html/zip/eml under a store directory is reported `unexpected` and does not change the exit; folder index.html pages and the tool's own files (manifest, index, report, logs, dotfiles) are never flagged | R1, S31 |
| MA-139 | U | verify trusts nothing in a manifest path: a `..` path, an absolute path, a symlinked directory component, and a FIFO (skipped on Windows) are each an integrity failure classified without following the symlink or opening the special file (the symlink target is never read), and an oversized file stops reading at recorded size+1; printed problem lines are control-character-free | R4, R5, S31 |
| MA-140 | U | verify writes nothing to the manifest without `-record` (its bytes are unchanged) and refuses a locked archive with a typed exit 1 naming the lock and the holder verb; the positive twin verifies once the lock is released | R4, R5, R12, S25, S31 |
| MA-141 | U | each per-category detail list caps at the configured maximum with exact counts preserved and a visible "list truncated" note | R1, S31 |
| MA-142 | U | status prints a "Fixity coverage: N of M records recorded" line from manifest fields alone (hashing nothing — that is `verify`), so the wording reads as coverage not integrity and points at `mailarchive verify` to check the bytes; when N is below M it also names the `mailarchive verify -record` remedy | R18, S31 |
| MA-194 | U | extractability is one file-presence signal three surfaces share: on a `-raw` archive `app.ExtractableCount`'s withEML equals what `verify` reports (WithEML) and what `extract` emits (Emitted), all via the shared emlPresent predicate (Lstat of `<stem>.eml`, never a hash), so status, verify and extract cannot report different numbers | R20, S34 |
| MA-195 | U | the extractable count is FILE PRESENCE, not a recorded digest: on a fixity-era archive (every `.eml` baselined) deleting one preserved `.eml` drops the count by one and drops verify's WithEML with it, even though the manifest still records that record's `Fixity.EML`; status's line then reads "N−1 of N" and never claims all N records "have" a preserved original | R20, S34, R18, S29 |
| MA-196 | U | status prints the K4 "Extractable: N of M records have a preserved original (.eml) on disk" line after the fixity-coverage line; when N<M it names both remedies (re-run a raw-capable source with `-raw`; keep the Outlook `.pst`/`.ost` itself, which never carries one) and never a categorical "re-archive" verdict; a PST-only archive reports N=0 end-to-end; the line is omitted when the manifest has no records; `status -json` carries `extractable {records_with_eml, records}` (null when no manifest); the posture is unchanged | R18, R20, S29, S34 |
| MA-154 | U | verify persists a SEPARATE `.mailarchive-lastverify.json` — written "running" once it holds the lock and commits to checking, then finalized with the verdict (attested + per-category counts + process exit code); a refusal before commit (no manifest, locked archive) writes none, and the record never disturbs the export last-run record | R18, R5, S31 |
| MA-155 | U | health reads the last-verify record and status reflects it: a "Last verify" line (attested / NOT attested with modified/missing/unrecorded counts), Assess RED on modified+missing (restore-or-re-export remedy), WARN on unrecorded-only, WARN when a verify is stale against its own verify schedule; status -json is version 2 with fixity{records,with_fixity} and last_verify{…} present (null when absent), a reason_codes array parallel to reasons, and last_run.finished null while a run is in progress | R18, S29, S31 |
| MA-156 | U | a not-attested verify with modified or missing files writes `ARCHIVE-INTEGRITY-ATTENTION.txt` naming the archive, the UTC time, the counts and the restore/re-export/re-verify remedy; an attested verify removes it; the export `BACKUP-NEEDS-ATTENTION.txt` sidecar stays export-scoped | R18, S31 |
| MA-157 | U | the last-run record is untrusted: ReadLastRun strips control characters from every Job element and from Error/Exe/Mode at the read choke point, and status's pasteable remedy shell-quotes each job token (POSIX single quotes on unix, double quotes on windows) so a Job carrying a newline/ESC and one carrying spaces/$() render as one inert, correctly quoted line | R18, S29 |
| MA-158 | U | the moved-archive WARN compares the two -out spellings by os.SameFile when both stat (collapsing symlink and case), else case-insensitively only on darwin/windows and byte-exact on linux, so a case-only or symlink spelling of the same directory does not trip a false "backs up a different archive" | R18, S29 |
| MA-159 | U | a manifest file that exists but does not load (newer format, or corrupt) is a RED naming the file, the loader's own wording (format version / corrupt) and the remedy — status no longer reports it as "no archive … run an export first" | R18, S29 |
| MA-160 | U | util.UnderCloudSync also recognises Dropbox, Google Drive ("My Drive"), iCloud Drive and Box segments with the matched service name; the no-schedule remedy targets the archive's actual -out when it differs from the recorded job's, the moved-archive remedy substitutes the recorded job (no "…" placeholder), and the schedule line labels the installed timestamp UTC | R18, S29 |
| MA-69 | U | html/zip are written to unique temp files and renamed into place: an attachment stream error leaves no partial or final zip and is recorded as an issue; SweepOrphans removes `.mailarchive-*.tmp` older than the run start and `-attachments.zip` files with no sibling html, and Run/reindex call it | R5, R1, S2 |
| MA-94 | U | a corrupt/truncated manifest is refused naming the file and the remedy (restore or delete to re-export), never a crash; Save fsyncs its temp before the atomic rename | R5, R12, S2 |
| MA-171 | U | `reindex -rebuild` reconstructs the index from the archive alone after search.db is deleted: a `-raw` record is re-indexed from its preserved `.eml` (from-eml) and a record with no `.eml` is re-derived from its archived page (from-html) with the right subject/sender/recipients/date (fixture with Sent≠Received and a non-empty subject), and the same queries return the same hits as before | R13, R8, S33 |
| MA-172 | U | rebuild builds a fresh temp db renamed over search.db, carrying no orphaned docs_fts row: a term present only in a pre-rebuild record whose file was removed returns no hit after rebuild, and that record is pruned from index and manifest | R5, R8, S33 |
| MA-173 | U | rebuild validates every recorded path with the exact verify gate before opening: a `..`/absolute/symlinked-component/oversized path is pruned-and-reported and never read — its out-of-archive or oversized content never enters the index (assure NoSideEffect) | R4, S33 |
| MA-174 | U | a per-record fault is isolated, never fatal: a corrupt sibling attachment zip on one record leaves that record indexed with no attachment names and every other record still rebuilt | R13, S33 |
| MA-175 | U | rebuild with no manifest refuses naming `.mailarchive-manifest.json` and the dotfile-copy hint (positive twin rebuilds cleanly); plain `reindex` on a manifest-present-but-index-missing archive refuses naming `reindex -rebuild` | R12, S33 |
| MA-176 | U | the rebuild summary reports the from-eml vs re-derived split and the count of core fields that could not be recovered (a page missing its header contributes to the unrecovered count and is still indexed, not dropped) | R13, S33 |
| MA-187 | U | mail may not wear the tool's private namespace: neutralize strips class="mailarchive-*" and data-mailarchive-* from every mail-supplied element so a hostile HTML body cannot inject a header marker, and reindex -rebuild's from-HTML reader anchors to the first <div class="mailarchive-header"> that is a direct child of <body> (never a <head> <template> or a body-nested div), so a crafted message cannot forge or erase its own indexed sender/subject/date | R19, R8, R7, S33 |
| MA-119 | U | a folder of many pages renders a compact numbered pager (first, last, current, ±2 neighbours, ellipses for the gaps) with the right relative hrefs and the current page not self-linked; page 1 has no "newer" link; the newer/older links stay | R7, S27 |
| MA-120 | U | the in-page filter keeps the honest "Filter" label; a paginated folder's help says the box matches this page only (N of M columns shown) and points to `mailarchive serve`/`rg` for whole-archive and body search, while an unpaginated folder keeps the short placeholder and shows no such help | R7, S27 |
| MA-121 | U | the exporter's date filter excludes exactly the items outside the -since window: an unseen message dated before -since is skipped (SkippedDate==1, no file written, not recorded in the manifest) while one on/after the window is exported | R11 |
| MA-189 | U | a routine reindex (holding the archive lock) reclaims a search.db.rebuild temp left by a crashed rebuild | R5, R13, S33 |

Rows MA-29..MA-37 were added by the adversarial pass; see
`docs/review-adversarial.md` for the findings they encode. Rows MA-40..MA-44
cover the `reindex` self-repair subcommand (R13); MA-45..MA-50 cover the
`schedule` subcommand (R14); MA-51 covers the input discovery behind the CLI
`-auto` flag and the GUI auto-detect step; MA-52..MA-55 cover Evolution store
support (R15); MA-56 covers auto-discovery filtering out unreadable Outlook data
files (R1); MA-57 covers containing a go-pst open-time panic on a corrupt data
file (R10); MA-58..MA-60 cover the `-outlook` Outlook-COM PST export (R16) — the
COM behaviour itself is lab-tier (MA-60, pending a real Windows+Outlook box),
with the pure name-safety (MA-58) and the off-Windows refusal (MA-59) tested in
CI. MA-61..MA-64 cover the Microsoft Graph app-only server-side source (R17):
the client, the RunGraph pipeline+incremental, and the refusals are CI-tested
against an in-process fake Graph server; MA-64 (a real tenant) is lab-tier.
