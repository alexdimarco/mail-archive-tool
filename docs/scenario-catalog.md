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
- **R3 — Stable identity.** A message's dedup key is stable across runs
  (Internet Message-ID when present, deterministic content hash otherwise) and
  folder-scoped — the same mail in two folders exports to both, but never twice
  within one folder.
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
  reconciled set; nothing on disk is deleted.
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
  Graph app-only source enumerates every folder (including nested) and every
  message and archives each via its raw MIME through the shared parser; it issues
  only GET requests (the app holds the read-only `Mail.Read` application
  permission, so a mailbox is never modified); and an incremental re-run archives
  zero new items and re-downloads no already-archived message body, matched by
  Internet-Message-ID.
- **R19 — Offline-inert archive.** Every exported `.html` is safe to open from
  disk: its `<head>` begins with a Content-Security-Policy meta identical to the
  policy `serve` sends (no scripts, no remote loads, no `<base>`, no forms) and a
  no-referrer meta, placed before any mail-supplied markup; mail-supplied
  `refresh` and `content-security-policy` metas are neutralized. `serve` sends
  that policy for archived files, a `script-src 'self'` policy for its own UI,
  HTML-escapes search snippets, never follows a symlink out of the archive root,
  and warns when bound to a non-loopback address.

## 3. Scenarios

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S1 Untrusted mail names a file `../../x` or a folder `..` | R4 | name neutralized to a safe in-root segment | MA-01, MA-03, MA-29, MA-30 |
| S2 Run interrupted mid-export | R5, R2 | manifest intact; no partial html/zip visible; orphans swept; already-written items survive; resume skips them | MA-11, MA-22, MA-69, MA-94 |
| S3 Re-run over an unchanged source | R2 | zero new exports | MA-22 |
| S4 Same email filed in two folders | R3 | exported to both; never twice in one | MA-09, MA-13 |
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
| S21 A hostile email (script, tracking pixel, remote CSS, `<base>`, meta refresh) is archived and opened from disk | R19, R7 | renders inertly: policy meta precedes the mail's markup; refresh neutralized; content preserved | MA-80 |
| S22 `serve` faces a hostile archive or network: script in a body/snippet, a symlink inside the archive, a non-loopback bind | R19, R4, R8 | UI/API/file responses carry a strict policy; snippets are escaped; symlinked paths are 404; non-loopback bind warns | MA-81, MA-82, MA-83, MA-84 |

Acknowledged limits (not defects): very large attachments are buffered whole in
memory (bounded by the largest single attachment, not the mailbox); the Graph
incremental fast-path dedups by Internet-Message-ID alone (a second, different
message reusing an already-archived id in the same folder is skipped without a
download — the price of R17's no-re-download guarantee); every manifest record
carries a 16-hex content fingerprint (~16% growth) so that Message-ID reuse is
detected for local sources. Recorded here so a finding against them is a design
conversation, not a silent gap.

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
| MA-09 | U | manifest Key is folder-scoped (same identity, different folders → different keys) | R3, S4 |
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
| MA-47 | U | schtasks command: /TN /TR /SC /ST, weekly /D SUN, delete reverses by name | R14, S14 |
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
| MA-64 | L | end-to-end against a real M365 tenant (app-only consent, throttling, real folder set) — **pending**, validated on a live tenant | R17 |
| MA-65 | U | schtasks /TR quotes the executable and every argument containing spaces, in both the install argv and the pasteable preview; splitting the run string with Windows C-runtime argv rules round-trips to the exact exe+args | R14, S14 |
| MA-80 | U | Render (both paths) starts `<head>` with the archive CSP meta + no-referrer meta before mail markup; neutralizes mail `refresh`/`content-security-policy` metas; adds a `<head>` when missing; body preserved | R19, R7, S21 |
| MA-81 | U | search snippets are HTML-escaped with only the tool's own `<mark>` tags live, in FTS and browse modes | R19, R8, S22 |
| MA-82 | U | `serve` refuses a path whose resolved location is outside the archive root (symlink escape → 404) and does not list directories lacking an index.html | R19, R4, S22 |
| MA-83 | U | `serve` sends the archive CSP + nosniff for files, and a `script-src 'self'` CSP + nosniff + frame-ancestors none for the UI and API; the UI carries no inline script | R19, R4, S22 |
| MA-84 | U | loopback-address classification: 127.0.0.1/::1/localhost are loopback; an empty host (all interfaces), 0.0.0.0, and LAN addresses are not | R19, S22 |
| MA-66 | U | a capture missing content is recorded fillable naming the item; an incremental re-run re-examines it (ignoring -since), rewrites nothing while unchanged, fills it once present (subset promotion: any recovered item promotes), then never re-examines; subject/date only on records with issues | R1, R2, S6 |
| MA-67 | U | a missing body is a fillable "body" gap that fills when the body appears; body-without-attachments is complete; a SourceComplete exporter records gaps as terminal and never re-examines them | R1, S6 |
| MA-68 | U | the report is regenerated from the manifest each run: a gap found earlier is still listed by a later run, disappears when filled (empty report removed); a legacy archive's earlier report is preserved as attachments-report-legacy.tsv | R1, S6 |
| MA-70 | U | a version-1 manifest (incl. one an older binary rewrote) migrates every record to the "unknown" sentinel and counts them; Save writes v2; reload keeps the sentinel; a sentinel record is re-captured once by the next incremental run | R1, R5, S6 |
| MA-71 | U | a Graph message captured with no body is recorded terminal and reported; an incremental re-run re-downloads nothing and retries nothing (R17 unchanged); a legacy sentinel on a Graph archive is resolved without a download | R17, R1, R2, S6 |
| MA-72 | U | schedule refuses a non-backup verb (serve/search/status/schedule), an interactive flag, a Graph job without -client-secret-file, a flat flag mixed with a -- job, and a bad -name, each naming the problem; valid export and graph -- jobs preview with -log and are not applied; the secret never appears in output | R14, R12, S28 |
| MA-74 | U | -client-secret-file: a missing, empty, oversized, world-readable (Unix) or non-regular (symlink/pipe) file is refused naming the requirement, at run time as at schedule time; the secret never appears in output; a proper file is accepted | R12, S28 |
| MA-73 | U | the Windows wrapper quotes every token, doubles %, redirects stderr to the .stderr.log sibling, never enables delayed expansion; /TR is the quoted wrapper path (< 261); the direct form still works without the wrapper | R14, S28 |
| MA-93 | U | the run log appends with a header per run and rotates to .1 past the size cap | R14, S28 |
| MA-97 | U | Install's descriptor round-trips (name, cadence, real exe + size/mtime, job, host); Remove deletes it; DefaultNameFor is stable per archive; SanitizeName bounds length and characters | R14, S28 |
| MA-90 | U | a message page links its folder page and the root, lists archived (not inline) attachments with a zip link, shows Message-ID, Sent/Received when both known, Reply-To, Bcc; times keep their original offset; a bare render carries no links | R7, S27 |
| MA-91 | U | folder pages paginate at pageSize with prev/next and root links, every message on some page, no extra empty page; the date column is labelled UTC; the root page links each folder's first page and README.txt | R7, S27 |
| MA-92 | U | Run writes README.txt at the archive root naming the layout, UTC convention, tool-free browsing, search.db (SQLite), the report and the manifest; reindex restores it | R7, S27 |
| MA-96 | U | with KeepRaw a message's original bytes are preserved byte-identically as <stem>.eml and linked from the page; none for a message without raw bytes; nothing without KeepRaw | R1, S27 |
| MA-89 | U | a folder segment that sanitization had to alter (illegal chars, truncation) carries a deterministic ~hash suffix from the original name so distinct source folders never merge; whitespace tidying adds none; the exporter shrinks the subject slug when the relative path would exceed 200 characters | R6, R4, S26 |
| MA-85 | U | an exclusive lock on <out>/.mailarchive.lock is held for the whole run: a second Acquire fails naming the path and holder; Release frees it; the CLI refuses a locked archive with a typed non-zero naming the lock (no manifest written) | R5, R12, S25 |
| MA-86 | U | two messages with the same Message-ID but different content in one folder are both exported and recorded; incremental re-run exports zero; a full re-run in reversed order yields the same file names | R3, R1, S23 |
| MA-87 | U | a file-stem collision between two keys lengthens the second stem from its own key (deterministic, no overwrite); re-exporting a message under a new name removes its previous html/zip | R4, R6, R13, S24 |
| MA-88 | U | the manifest and index are checkpointed every CheckpointEvery messages inside a store walk, so a hard crash keeps the progress made | R5, S2 |
| MA-69 | U | html/zip are written to unique temp files and renamed into place: an attachment stream error leaves no partial or final zip and is recorded as an issue; SweepOrphans removes `.mailarchive-*.tmp` older than the run start and `-attachments.zip` files with no sibling html, and Run/reindex call it | R5, R1, S2 |
| MA-94 | U | a corrupt/truncated manifest is refused naming the file and the remedy (restore or delete to re-export), never a crash; Save fsyncs its temp before the atomic rename | R5, R12, S2 |

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
