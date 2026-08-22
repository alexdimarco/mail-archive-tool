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
  (not downloaded, unparseable) is recorded in the verification report, never
  hidden.
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
- **R5 — Crash-safe state.** The manifest is written atomically; an interrupted
  or failed run never corrupts it, and progress already made survives.
- **R6 — Faithful structure.** Output mirrors the source folder tree; each store
  gets its own top-level directory; a single mbox/maildir folder does not
  double-nest under its own name.
- **R7 — Self-contained HTML.** Each exported `.html` renders the message body
  offline: inline `cid:` images are embedded as data URIs; header fields are
  HTML-escaped.
- **R8 — Search ↔ export parity.** The index holds exactly the exported
  messages; a full-text query returns matching items; free-text is matched as
  literal terms (no FTS operator injection or query crash).
- **R9 — Read-only source.** Reading a mail store never modifies it. The single
  write path (`-enable-offline`) is opt-in, backs up before editing, and refuses
  while the mail app is running.
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
  the operator's export flags; it prints the entry by default and only applies it
  under `--install`; `--install` is idempotent (re-running yields one entry) and
  `--remove` cleanly reverses it, leaving unrelated entries untouched.
- **R16 — Outlook COM export is opt-in and safe.** On Windows with *classic*
  Outlook, `-outlook` (and the GUI's Outlook-app option) drives Outlook to write
  a fresh, standard `.pst` per mail account, which then archives through the
  normal pipeline; every created PST file name stays inside the output root. On
  any platform without classic Outlook it refuses with a legible message naming
  the requirement — never a crash. (COM behaviour is lab-validated; go-pst
  cannot read every live `.ost`, so this is the reliable path for Exchange.)
- **R15 — Evolution stores read faithfully.** An Evolution store is read without
  silent loss: the local Maildir++ store's dot-encoded, `_XX`-escaped folder
  hierarchy is decoded (every subfolder read, no traversal out of the root), and
  an IMAP disk cache's per-folder maildirs are all walked, whether nested
  directly or under a `subfolders` container. Each cached message is yielded once
  with its correct folder path.

## 3. Scenarios

| Scenario | Must hold | Recovery/response | Proven by |
|---|---|---|---|
| S1 Untrusted mail names a file `../../x` or a folder `..` | R4 | name neutralized to a safe in-root segment | MA-01, MA-03, MA-29, MA-30 |
| S2 Run interrupted mid-export | R5, R2 | manifest intact; already-written items survive; resume skips them | MA-11, MA-22 |
| S3 Re-run over an unchanged source | R2 | zero new exports | MA-22 |
| S4 Same email filed in two folders | R3 | exported to both; never twice in one | MA-09, MA-13 |
| S5 Message with an inline `cid:` image | R7 | embedded as a data URI; renders offline | MA-19 |
| S6 Attachment/inline content not present locally | R1 | skipped from the zip but recorded in the report | MA-20, MA-21 |
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

Acknowledged limits (not defects): very large attachments are buffered whole in
memory (bounded by the largest single attachment, not the mailbox) — recorded
here so a finding against it is a design conversation, not a silent gap.

## 4. Test specs

Tiers: **U** unit property (every commit) · **S** structural whole-tree walk
(every commit) · **A** fail-closed, infra-free · **L** lab (real infra).

| ID | Tier | Asserts | Covers |
|---|---|---|---|
| MA-01 | U | SanitizeSegment strips separators/illegal/reserved, trims dots, `..`→placeholder | R4, S1, S12 |
| MA-02 | U | SanitizeSegment bounds segment length | R4 |
| MA-03 | U | SanitizeFilename preserves extension, drops path separators | R4, S1 |
| MA-04 | U | Slug is filesystem-safe and stable | R4, R6 |
| MA-05 | U | ShortHash is deterministic and collision-distinct | R3 |
| MA-06 | U | ParseSince relative windows (`30d`,`4w`,`12h`) | R11 |
| MA-07 | U | ParseSince absolute dates | R11 |
| MA-08 | U | ParseSince rejects garbage with an error (no silent zero) | R11, R12 |
| MA-09 | U | manifest Key is folder-scoped (same identity, different folders → different keys) | R3, S4 |
| MA-10 | U | a missing manifest loads as empty, not an error | R5 |
| MA-11 | U | manifest Add/Save/reload round-trips; atomic write | R5, R2, S2 |
| MA-12 | U | decodeBytes returns UTF-8 for UTF-8 and Windows-1252 for legacy bytes | R1 |
| MA-13 | U | mbox reader extracts headers/body/Message-ID; single file has no double-nest | R1, R3, R10, S4, S7 |
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
| MA-45 | U | cron line: interval schedule fields + exe/export-flags + log redirect; block carries the marker | R14, S14 |
| MA-46 | U | launchd plist: label, ProgramArguments, StartCalendarInterval (Minute/Hour/Weekday by cadence) | R14, S14 |
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
CI.
