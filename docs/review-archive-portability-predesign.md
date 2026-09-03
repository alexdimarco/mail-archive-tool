# Pre-code design review — archive portability and message state (10 lenses)

**Design under review:** `docs/design-archive-portability.md`, revision 1
(2026-09-03). **Verdict:** GO_WITH_CONDITIONS — conditions **PC1–PC17** below are
folded into revision 2; the build must not start from revision 1. Per-slice:
slice A (`reindex -rebuild`) — GO with PC1–PC7, PC17; slice B (`extract`) — GO
with PC8–PC15; slice C (message state) — GO with PC16, PC17.

**Method.** The assurance-kit design gate as a workflow on 2026-09-03: five
finders holding two adjacent lenses each, a refutation-default skeptic per
finding with the code in hand, and a rescue reviewer on the surviving
blocker/high findings. 38 findings; 37 confirmed or partial, 1 refuted; several
blocker/high survived to rescue and all dissolved with modest mechanism changes.
The rescue surfaced a hidden blocker the lenses circled but none stated plainly.

## Hidden blocker (rescue reviewer)

The headline scenario (nas-04 / P4a) is an **already-existing** PST/Outlook
archive whose source was deleted per the README's own invitation and whose
`search.db` is now lost. A PST archive never has a sibling `.eml` (`-raw` is off
by default and is a no-op for PST), so its rebuild is **100% from-HTML** — and
revision 1's only robustness mechanism, the renderer's `data-mailarchive-field`
attributes, is written at **capture** time, so it helps only archives exported
*after* this feature ships. Every archive that already exists — precisely the
source-deleted ones this recovery is for — carries the **old** `.html`: the
subject in a `.mailarchive-subject` div (not the `<dl>`), no machine-readable
date, the sender as a `formatSender` display string. So the from-HTML parser
must be written to recover fields from **what the current renderer already
emits**, with the going-forward attributes as belt-and-suspenders, not as the
primary mechanism. Revision 2's PC3 is written to the installed base first.

## Findings and disposition

Severity is the skeptic's verified severity. Blocker/high were sent to rescue;
their outcome is in the last column. The condition that closes each is named.

| Finding | Sev | Verdict | Failure the design permits | Rescue / condition |
|---|---|---|---|---|
| F1 index wipe | blocker | CONFIRMED | `DELETE FROM docs` alone orphans every `docs_fts` row (standalone FTS5, aligned by rowid); search returns stale/ghost hits | DISSOLVED → PC1 (build into a temp DB, rename; no in-place delete) |
| F1 rebuild reads untrusted manifest paths | blocker | CONFIRMED | rebuild opens `<stem>.eml`/`.html` at a manifest-supplied path with no containment; a tampered path reads outside the archive | DISSOLVED → PC2 (validRelPath + inspect before any read) |
| F2 crash mid-rebuild | high | CONFIRMED | in-place wipe + batched Add commits leave a durably partial index and stale pages on a crash; no temp+rename (violates R5) | DISSOLVED → PC1 |
| F3 from-HTML spec wrong | high | CONFIRMED | subject is in a `.mailarchive-subject` div not the `<dl>`; there is no "Date (UTC)" label; Sent/Received are separate rows — rebuild would recover the wrong/empty fields | PARTIAL → PC3 |
| F3 extract symlink follow | high | CONFIRMED | "validated the same containment way" is ambiguous; validRelPath alone does not stop a symlinked `.eml` from being copied | DISSOLVED → PC8 |
| F3 no Date row for received mail | high | CONFIRMED | received mail emits Sent+Received, no Date row; rebuild's Date lookup misses the common case | DISSOLVED → PC3 |
| F4 mbox non-atomic append | high | CONFIRMED | mbox output appends with no temp+rename and no empty-`-dest` rule; a crash tears the last message, a re-run doubles every message | DISSOLVED → PC9 |
| L1 rebuild re-admits untrusted content | high | CONFIRMED | field extraction is not scoped to the header container; a hostile body `<dd data-mailarchive-field=…>` or `<dt>From</dt>` shadows the real fields, silently breaking R8 search parity | DISSOLVED → PC3 (anchor to the first `.mailarchive-header`, first `<dl>`, first-match) |
| L10 Graph state source false | high | CONFIRMED | G5 lists Graph as a state source but the listing `$select` fetches none of importance/isRead/sensitivity | DISSOLVED → PC16 (widen the existing `$select`; no extra request) |
| F2 corrupt-but-present index | med | PARTIAL | rebuild special-cases only an absent index; a corrupt `search.db` makes `index.Open` fail so rebuild refuses instead of repairing | PC1 (discard an unopenable index, rebuild fresh) |
| F4 extract exit partition | med | CONFIRMED | "exit code TBD" collides with ux-contract X1 (verify already owns `2`) | PC11 (pin the codes against X1) |
| F5 handle lifetime / walk order | med | CONFIRMED | "one handle per folder" + map iteration is unbounded fds and non-deterministic order | PC9 (folder-grouped sorted walk, close on leaving a folder) |
| F5 rebuild per-record errors fatal | med | CONFIRMED | a corrupt zip / torn html / I/O error aborts the whole rebuild | PC4 (fault-isolate per record) |
| F6 reindex hint | med | CONFIRMED | plain `reindex` on a lost index still says "run an export first", not the new recovery | PC7 |
| F6 rebuild no containment | med | CONFIRMED | slice A reads file content at manifest paths with no containment validation | PC2 |
| F8 manifest-loss refusal opaque | med | CONFIRMED | a dotfile-skipping copy drops `.mailarchive-manifest.json`; the refusal doesn't name it or the recovery | PC7 |
| L1 extract foreclosed at capture | med | CONFIRMED | extract needs `.eml` (`-raw`), off by default; the README invites deleting the source, permanently foreclosing extract with no earlier warning | PC15 |
| L1 rebuild header field mismatch | med | CONFIRMED | (same root as F3) the `<dl>` field list does not match `renderHeader` | PC3 |
| L10 mbox flavour conditional | med | CONFIRMED | G4/§5 imply byte-faithful, but mboxrd round-trip is conditional on the consumer unquoting `>From ` | PC12 |
| L10 "never changes a file" contradiction | med | CONFIRMED | G2 says rebuild changes no file, but it regenerates folder/root `index.html` | PC5 |
| L2 from-HTML vs capture fields | med | PARTIAL | the design never weighs from-HTML against persisting index fields at capture | PC3/§5 (justify: the `.html`/`.eml` are the only on-disk record for PST; a per-record field store would duplicate the index at manifest-size cost) |
| L9 extract exclusive lock, cold read | med | PARTIAL | extract holds the exclusive lock for a multi-hour read; a scheduled backup meanwhile refuses | PC14 |
| L9 rebuild wipe then partial commit | med | PARTIAL | (same as F2) | PC1 |
| F5 rebuild unbounded reads | low | PARTIAL | a pathological/hostile multi-GB `.eml`/`.html` OOMs the rebuild | PC2 (size cap) |
| F6 slices not file-disjoint | low | PARTIAL | slices A and C both edit `renderHeader` | PC17 (sequence A→C; rebuild reads C's Status attribute or C drops it) |
| F7 extract exit vs X1 | low | PARTIAL | (same as F4-exit) | PC11 |
| F7 extract lock warning | low | PARTIAL | no run-outside-backup-window warning like verify | PC14 |
| F7 rebuild upgrade bookkeeping | low | PARTIAL | rebuild omits the StoresMigrated/Rekeyed log + index meta stamp reindex/Run do | PC6 |
| F8 rebuild single-transaction claim | low | PARTIAL | Add auto-commits every 1000 rows, so the "one transaction" is not atomic | PC1 |
| L1 extract ignores fixity | low | PARTIAL | extract copies `.eml` bytes without checking the recorded `Fixity.EML` | PC13 |
| L1 rebuild manifest-loss out of scope | low | PARTIAL | rebuild recovers the index only; manifest loss is the same P4a risk, unaddressed | PC7 (state it out of scope, name the file) |
| L10 extract exit partition incomplete | low | PARTIAL | only two of four (emitted, skipped) cases have codes | PC11 |
| L9 -dest overlaps -out | low | PARTIAL | nothing forbids `-dest` inside/equal/parent of `-out` | PC10 |
| L9 extract not in knownVerbs | low | PARTIAL | `schedule -- extract` would be misparsed as an export job | PC14 |

Refuted: **L2-bundled-review-uneven-gates** (this review issues per-slice
conditions, which is what GO_WITH_CONDITIONS already does).

## Conditions (folded into revision 2)

- **PC1 — Rebuild is temp+rename, never in-place.** Build the fresh index into a
  sibling `search.db.rebuild` opened on an absent file via `index.Open`
  (so both `docs` and `docs_fts` start empty — the `DELETE FROM docs` orphan and
  the rowid-collision both vanish), populate with the existing batched `Add`,
  `Flush`, fsync, then `os.Rename` over `search.db` only on full success. A
  present-but-unopenable `search.db` is moved aside and rebuilt fresh. A crash
  leaves the old index untouched or an inert leftover temp. (R5.)
- **PC2 — Rebuild trusts no manifest path.** Before opening any file, validate
  `rec.Path` with `validRelPath` (clean, relative, forward-slashed, first segment
  a store token, no `..`/absolute/backslash) and resolve component-wise with
  `Lstat`, never following a symlink at any level; non-regular files are skipped;
  reads are bounded by a cap. A bad path is skipped-and-reported, never read.
  This replaces reindex's plain `os.Stat` existence check on the rebuild path.
  (R4, MA-139.)
- **PC3 — From-HTML recovery matches the current renderer and is header-scoped.**
  The parser anchors to the **first** `.mailarchive-header` element, reads the
  subject from its `.mailarchive-subject` div, and reads the **first** `<dl>`
  within it (first-match), ignoring any `data-mailarchive-field` marker or `<dt>`
  label found outside that container — so mail body markup cannot shadow a header
  field. It recovers From by splitting `formatSender`'s "Name <email>", To/Cc
  from their dds, and the date from `fmtTime`'s parenthetical "(YYYY-MM-DD HH:MM
  UTC)" using Received else Sent (handling the separate Sent/Received rows).
  Going forward the renderer tags those dds and the subject div with
  `data-mailarchive-field` for exactness, but the installed base (old `.html`) is
  recovered by this structure match; whatever cannot be recovered is left empty
  and counted, never fatal. Attachment names come from the sibling zip's central
  directory.
- **PC4 — Per-record derivation is fault-isolated.** A read/parse/zip error on
  one record is counted and that record skipped (or indexed from whatever fields
  succeeded); it never aborts the rebuild.
- **PC5 — Rebuild is honest.** The summary reports from-eml vs re-derived counts
  and the count of records whose date/fields could not be recovered. G2 is
  corrected to "never changes a message (`.html`/`.zip`/`.eml`) file"; it
  acknowledges the folder and root `index.html` are regenerated, and that a
  from-HTML record's folder-table columns can be less complete than its intact
  message page.
- **PC6 — Rebuild carries the upgrade bookkeeping.** After repopulating, it logs
  the `StoresMigrated`/`Rekeyed` lines and stamps the index meta version, exactly
  as `reindex`/`Run` do, so a rebuild that is also the first post-upgrade open
  matches their coexistence contract.
- **PC7 — The recovery is discoverable and its scope stated.** Plain `reindex`
  on a manifest-present-but-index-missing/unopenable archive names `reindex
  -rebuild` as the recovery. The no-manifest refusal names
  `.mailarchive-manifest.json`, notes a dotfile-skipping copy may have dropped
  it, and states that rebuild requires the manifest (manifest reconstruction is
  out of scope).
- **PC8 — Extract confirms each `.eml` with the inspect gate.** `validRelPath` +
  `inspect` (component-wise `Lstat`, no symlink follow, regular-file-only, read
  bounded to the recorded size) before reading; a symlink/non-regular/oversized/
  absent `.eml` is counted Skipped and reported, never read. "The containment
  way" in the design is named as `inspect`, not the string check alone.
- **PC9 — Extract output is atomic and idempotent.** Each folder's `.mbox` (and
  each `.eml`) is written to a temp path under `-dest` and renamed on success;
  a pre-existing folder file is truncated, never appended; `-dest` must be empty
  or `--overwrite` is given. The walk is folder-grouped (sorted by key, which
  groups folder-adjacent) and closes each folder's handle on leaving it. ENOSPC
  and second-run behaviour are defined (refuse legibly; a partial temp is
  discarded).
- **PC10 — `-dest` cannot overlap `-out`.** Refuse a `-dest` equal to, inside, or
  a parent of `-out` (symlink-resolved, component-wise), with a typed non-zero
  naming the overlap. State inline that extract copies the full `.eml` volume, so
  `-dest` needs that much free space.
- **PC11 — Extract's exit codes are pinned against X1.** Declared in
  `docs/ux-contract.md` X1 as a non-colliding set: **0** = the whole requested set
  emitted (skipped == 0); a fresh distinct code (NOT `2`, which is verify's "not
  attested") = **partial**, some records had no preserved bytes; **1** =
  refusal/error. The emitted==0 case (nothing extractable, e.g. a PST-only
  archive) maps to the partial code, not 0, and not an error.
- **PC12 — mbox flavour is stated.** Output is mboxrd; a reader that unquotes
  `>From ` recovers the exact original bytes, an mboxo reader does not; `-format
  eml` is named as the byte-exact format for a consumer of unknown flavour.
- **PC13 — Extract checks fixity when present.** Each `<stem>.eml` is verified
  against its recorded `Fixity.EML` (MA-135) before emitting; a mismatch is
  counted and named like Skipped, never written. When a record has no recorded
  fixity, the bytes are emitted with the "as stored, run `verify` first"
  caveat in §5 and the summary.
- **PC14 — Extract's lock posture is stated; it is not schedulable.** Extract
  holds the exclusive archive lock for its whole run; `extract -h` and the README
  warn to run it outside the backup window (as verify does). `extract` is
  registered in `knownVerbs` and refused as a scheduled backup job with a typed
  message ("an operator-driven migration, not a backup"). It appears in the root
  `-h` and the MA-39 help walk.
- **PC15 — The capture-time dependency is surfaced early.** A run (and `verify`)
  warns when a raw-capable source is archived **without** `-raw`, so the operator
  can choose before deleting the source; `status` reports "not extractable (no
  preserved originals)" for such an archive. §5 states this next to the extract
  claim.
- **PC16 — Graph state is one widened request.** The listing `$select` in
  `graph.Messages` is widened to
  `id,internetMessageId,receivedDateTime,importance,isRead,sensitivity` (all real
  Graph scalars — no extra round-trip, keeping the "no extra request" promise),
  carried on `MessageRef`, and set on the message after `ParseRFC822`. A field
  the tenant omits stays empty. The incremental fast-path key still uses only the
  id.
- **PC17 — Slice sequencing and the shared header.** Slices A and C both edit
  `renderHeader`, so they are not file-disjoint: C builds on A's commit (or the
  two header edits are folded into one). Rebuild reads C's Status
  `data-mailarchive-field` back into the model, or C omits that attribute. The
  message-state fields stay excluded from `contentHash`/`Fingerprint` (the
  identity-stability guard).
