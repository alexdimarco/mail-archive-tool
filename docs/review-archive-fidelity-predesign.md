# Pre-code design review — archive fidelity (10 lenses)

**Design under review:** `docs/design-archive-fidelity.md`, revision 1 (2026-09-02).
**Verdict:** GO_WITH_CONDITIONS — conditions **FC1–FC16** below are folded into
revision 2 of the design; the build must not start from revision 1.
Per-slice verdicts (the design bundles four independent slices; condition FC16):
slice (a) identity — GO with FC1, FC2, FC3, FC9; slice (b) fixity + `verify` — GO
with FC4, FC5, FC6, FC10, FC11, FC12, FC14; slice (c) headers — GO with FC8;
slice (d) Task Scheduler XML — GO with FC7, FC13, FC15.

**Method.** The assurance-kit design gate run as a workflow on 2026-09-03: five
finders, each holding two adjacent lenses of the ten (purpose/proportionality,
durability/usability, integration/security, failure domains/migration,
cost/honesty), each finding then handed to a refutation-default skeptic with
the design text and the code, and the surviving blocker/high findings to a
separate rescue reviewer whose job was to save the design with modest
mechanism changes or say honestly that a finding survives. 36 findings; 35
confirmed or partial, 1 refuted; 5 blocker/high survived to rescue; all five
dissolved (one partially) — and the rescue surfaced a hidden blocker (below)
that every finder's proposed condition would have missed.

## Hidden blocker (found by the rescue reviewer)

The index re-key is the real trap, and every surviving condition under-specifies it. All the finders/skeptic say 'apply the same NUL guard inside index.MigrateKeys' — but MigrateKeys is gated on the index's OWN meta.version, and an old binary never touches the meta table, so that counter NEVER regresses under the downgrade scenario. Result: after an old-binary excursion the MANIFEST self-heals (its Version WAS regressed to 2 by Save, so its migration re-fires) but MigrateKeys stays a no-op forever and never runs the guard, so the old binary's 1-NUL index rows persist beside the 2-NUL survivors (they don't collide: key is UNIQUE and the strings differ; deleteByKeyTx replaces only on exact key match, so it inserts duplicates). R8 search<->export parity is then PERMANENTLY broken — every touched message returns two search hits, one pointing at the orphaned old file — and no version-gated migration will ever repair it. The NUL-discriminator-inside-MigrateKeys 'fix' is dead code exactly when it is needed. The fix that actually closes it: trigger the index repair from the MANIFEST's regression signal (not the index's own version), re-keying rows from the index's existing `store` column (index.go:31). Modest, but NOT what the conditions say — it would have been built wrong.

## Findings

Severity is the skeptic's verified severity (the finder's claim in
parentheses). The last column names the condition that closes the finding.

| Finding | Lenses | Severity | Verdict | Failure the design as written permits | Condition |
|---|---|---|---|---|---|
| D3-1 | L3-4 | blocker (blocker) | CONFIRMED | The v2->v3 re-key is a blind structural transform gated ONLY on the global version counter, unlike the idempotent (content-checked) v1->v2 sentinel migration. Any older binary that runs export/graph/reindex against an upgraded archive rewrites the manifest… | FC1 |
| F1 | L5-6 | blocker (blocker) | CONFIRMED | F1 keys identity by storeSegment = util.SanitizeSegment(store), and §3.1 makes the manifest key store\x00folder\x00identity. But the store name is NOT injective per source: pstStoreName (reader.go:325) prefers the message-store DISPLAY NAME (default 'Outlook… | FC2 |
| F7.8-1 | L7-8 | blocker (blocker) | CONFIRMED | The dedup key format changes from folder\x00identity (v2) to store\x00folder\x00identity (v3), but Load (manifest.go) still has no forward-version guard and Save unconditionally rewrites Version=manifestVersion. A still-shipped v2 binary that opens a v3… | FC1 |
| F7.8-2 | L7-8 | high (high) | CONFIRMED | The existing sentinel migration is gated `if m.Version < manifestVersion` (manifest.go:125). The design bumps manifestVersion to 3 and only says the v1→v2 sentinel migration 'still applies first', without decoupling its gate from the constant. Built… | FC3 |
| L10-1 | L9-10 | high (high) | CONFIRMED | P2 names multi-year offline storage as the persona and calls fixity "the one property the README cannot currently promise." But fixity is computed only at write time, and the v2->v3 migration explicitly rewrites no files, so it backfills no digests. Every… | FC4 |
| D3-2 | L3-4 | medium (medium) | CONFIRMED | P2 names 'multi-year offline storage' as the population that needs fixity, but the design closes it only for NEW captures. An existing multi-year archive migrated to v3 keeps empty Files maps; a clean incremental run skips already-complete records (manifest… | FC4 |
| D3-3 | L3-4 | medium (medium) | PARTIAL | F4 claims full symlink containment, but §3.3 only Lstats the LEAF file in the recorded-file pass and only skips symlinked directories in the separate `unexpected` WalkDir. A symlinked PARENT directory is therefore not covered by the recorded-file pass: verify… | FC6 |
| F2 | L5-6 | medium (high) | PARTIAL | verify 'Lstat the path (refuse to follow a symlink) ... read and hash' guards ONLY against symlinks and reads the whole file unbounded. An insider (in-scope persona: write access to the archive) can (a) replace a recorded 100-byte file with a 100 GB regular… | FC6 |
| F3 | L5-6 | medium (medium) | CONFIRMED | F4 claims verify 'follows no symlink out of the archive' but nothing cleans the manifest-supplied paths. verify reads Record.Files keys / Record.Path as-is via filepath.Join(out, FromSlash(relPath)); a tampered or bit-rotted manifest whose key is… | FC6 |
| F5 | L5-6 | medium (medium) | CONFIRMED | F5 renders the header block under the label 'Original internet headers' and calls it 'the verbatim internet header block'. Two problems the design permits: (1) the bytes are attacker-supplied (an aggressor controls a message's… | FC8 |
| F6 | L5-6 | medium (medium) | CONFIRMED | This one design ships BOTH verify (holds the exclusive archive lock for its entire byte-by-byte duration, §5: 'a long, deliberate operation') and the resilient nightly scheduler (P1-windows, whose whole point is that the backup runs). They coordinate only… | FC5 |
| F7.8-3 | L7-8 | medium (medium) | CONFIRMED | Fixity is populated only by Export (rec.Files at write time). Incremental runs skip complete records (exporter.go:119-129: prev.Fillable()==false → SkippedManifest, never rewritten), so a v2 archive migrated to v3 and then run incrementally NEVER backfills… | FC4 |
| F7.8-5 | L7-8 | medium (medium) | CONFIRMED | verify acquires the exclusive archive lock and holds it while reading every byte of every recorded file (§5: 'a long, deliberate operation, and it holds the archive lock'). A scheduled nightly export that fires while verify runs fails lockfile.Acquire, which… | FC5 |
| L1-f6-remove-contradiction | L1-2 | medium (medium) | CONFIRMED | F6 introduces a new persisted artifact -- the Task Scheduler XML written next to the wrapper as `<wrapper>.xml`. Two sentences of the design contradict each other on cleanup: F6 says "`Remove` is unchanged" while §3.5 says "`Remove` deletes it with the… | FC7 |
| L1-verify-exit-ignores-unrecorded | L1-2 | medium (medium) | CONFIRMED | The design promotes verify's exit code as the machine-readable fixity signal ("the exit code is the answer"), and it is the only such signal the tool offers for fixity (status "does not hash"; ux-contract C5 notes status itself always exits 0). But the exit… | FC4 |
| L10-3 | L9-10 | medium (low) | CONFIRMED | verify's exit code and summary conflate "verified intact" with "nothing was recorded to verify." On a legacy (or any pre-v3) archive every file is `unrecorded`, verified==0, and modified+missing==0, so verify prints a benign summary and exits 0. An operator… | FC4 |
| L9-1 | L9-10 | medium (high) | PARTIAL | verify acquires the same single exclusive archive lock (lockfile.Name) that export/graph/reindex use, and on a large multi-year archive it reads every byte of every recorded file (Section 5: "a long, deliberate operation") -- potentially hours. The design… | FC5 |
| L9-2 | L9-10 | medium (medium) | CONFIRMED | Fixity is only as good as how often verify actually runs, and for the stated persona (multi-year OFFLINE storage) nobody is at the keyboard. The only automation the tool offers is `schedule`, but MA-72 refuses any non-backup verb, and the design neither adds… | FC10 |
| L9-3 | L9-10 | medium (medium) | CONFIRMED | The per-message forever-cost is understated. Files is a map keyed by the archive-relative path (up to the 200-char relPathBudget), so each entry is roughly path-key (~60-200 B) + sha256 hex (64 B) + JSON scaffolding (~30 B) ~= 150-300 B, at 1-3 files per… | FC11 |
| U4-1 | L3-4 | medium (medium) | CONFIRMED | verify newly introduces a long-lived holder of the single EXCLUSIVE archive lock (Acquire without waiting; MA-85). On a large archive a daytime verify can run for many minutes to hours. If it overruns into the nightly scheduled window, the scheduled export's… | FC5 |
| U4-2 | L3-4 | medium (medium) | CONFIRMED | The v3 migration on first open is silent and unbounded. The design counts Rekeyed but never says it is reported, and unlike the v1->v2 case (run.go logs '%d manifest entries predate completeness tracking...') there is no log line for the re-key.… | FC9 |
| D3-4 | L3-4 | low (medium) | PARTIAL | Fixity adds up to three digest entries (~120 B each) to a manifest that is already loaded WHOLE into memory for every run/status/verify and rewritten IN FULL and fsynced at every checkpoint (every 1000 messages) via MarshalIndent. At the million-file scale… | FC11 |
| D3-5 | L3-4 | low (low) | CONFIRMED | verify's Report has no stated size bound. On a pathologically drifted archive (a bad restore that re-encoded every file, or a wholly-legacy archive) every one of N files is modified/unrecorded/unexpected, so verify accumulates O(N) findings in memory and… | FC14 |
| F4 | L5-6 | low (medium) | PARTIAL | F4 says 'the exit code is the answer' and 'exits non-zero iff any file is modified or missing'. But exit 1 already means 'any error/refusal' (X1), and verify itself refuses (no manifest -> exit 1; locked archive -> exit 1). So a monitor cannot distinguish… | FC4 |
| F7 | L5-6 | low (low) | CONFIRMED | The design adds a new 'verify' subcommand but says nothing about wiring it into the command-grammar surface: exportUsage (main.go:664) enumerates every subcommand for the root -h (X3), and MA-39 walks each subcommand's -h asserting it names its flags. Neither… | FC12 |
| F7.8-4 | L7-8 | low (medium) | PARTIAL | Slice (d) replaces the working `schtasks /Create /TR` install with `/Create /XML file /F`, but real acceptance of the XML by schtasks.exe is MA-79 (lab, pending) and §4 ships slice (d) on 'vet under GOOS=windows', which proves the generator, not the import. A… | FC15 |
| F7.8-6 | L7-8 | low (low) | PARTIAL | The search index gets its own version scheme (meta absent=1, MigrateKeys sets 2) decoupled from the manifest's version 3, with idempotence described only as 'a version-2 index is a no-op'. There is no forward-compat guard: a v2 binary opening a v3-migrated… | FC1 |
| F8 | L5-6 | low (low) | CONFIRMED | status is to print 'Fixity: recorded for N of M files' and 'does not hash'. M — the total number of files the archive SHOULD have — is underspecified: legacy records have no Files map, and whether a record has a zip/eml is otherwise only knowable by statting… | FC12 |
| L1-f5-header-label-semantics | L1-2 | low (low) | REFUTED | The one UI label "Original internet headers" names materially different content per source: for PST it is the 0x007D transport subset (Received/Return-Path/Authentication-Results/List-Id -- the provenance P3 is about), but for mbox/maildir/Graph it is the… | — |
| L1-fixity-coverage-overclaim | L1-2 | low (medium) | PARTIAL | Fixity is recorded ONLY at write time (F3: digests written by the exporter for files it writes; §3.2 tees the write). There is no back-fill of digests for files already on disk (reindex -rebuild is explicitly deferred in §1). So every message written BEFORE… | FC4 |
| L10-2 | L9-10 | low (medium) | PARTIAL | The design replaces the currently CI-tested and (per MA-47/MA-65) proven `/TR`+wrapper install with a `/XML` install whose acceptance by schtasks.exe is only lab-pending (MA-79). The xml.Unmarshal round-trip proves the document is well-formed Go-side; it… | FC15 |
| L2-four-features-one-gate | L1-2 | low (medium) | PARTIAL | The doc bundles four mechanistically unrelated changes -- store-scoped identity + dual migration (F1/F2), fixity + a new `verify` subcommand (F3/F4), a rendered transport-headers field (F5), and Task Scheduler XML (F6) -- under one pre-code gate and (per the… | FC16 |
| L2-migration-cost-invisible | L1-2 | low (low) | PARTIAL | The v2->v3 upgrade re-keys the manifest AND rewrites every row of search.db in a single transaction, performed transparently at the start of the next Run/Graph/Reindex with no progress surface and no cost bound. For the touted "very large" archive (millions… | FC9 |
| L2-verify-exclusive-lock-blocks-backup | L1-2 | low (medium) | PARTIAL | `verify` is read-only yet takes the SAME exclusive archive lock a write run takes (lockfile.Acquire, MA-85) and holds it for the entire multi-hour byte read of a large archive. Because the lock is exclusive, a scheduled backup whose trigger fires during a… | FC5 |
| L9-4 | L9-10 | low (medium) | PARTIAL | The index key migration is O(whole archive) work executed inside the FIRST scheduled run after upgrade, as a single SQLite transaction (journal_mode=WAL, synchronous=NORMAL). Re-keying every row of a multi-million-row FTS index in one transaction balloons the… | FC9 |
| U4-3 | L3-4 | low (medium) | PARTIAL | SchtasksXML sets StartBoundary to now's DATE at the chosen HH:MM. When the operator installs AFTER the daily time (e.g. installs at 14:00 for a 02:00 job), StartBoundary is in the past. Combined with StartWhenAvailable=true, Task Scheduler can treat today's… | FC13 |

## Rescue outcomes (surviving blocker/high findings)

- **D3-1** — DISSOLVED: Make the re-key CONTENT-driven and self-healing, not blind-version-gated. Manifest Load: rekey an entry iff its key has exactly one 0x00 (v2); leave two-0x00 keys (already v3) untouched. The #fp suffix carries no NUL and store/identity are NUL-free (the existing keySeparator invariant, manifest.go:29-32), so the NUL count is an exact v2/v3 discriminator and a survivor is never double-prefixed. Because Save() regresses Version to 2 under an old binary, this migration re-fires on re-upgrade and repairs the old binary's 1-NUL entries. CRITICAL correction to the proposed condition: 'apply the same guard inside index.MigrateKeys' is INSUFFICIENT (see hidden_blocker) — MigrateKeys is gated on the index's own meta.version, which the old binary never regresses. Trigger the index repair from the MANIFEST's regression signal (pass force from app.Run/Graph/Reindex when the manifest rekeyed) and re-
- **F1** — DISSOLVED: Make the store token injective. When two sources in one run resolve to the same SanitizeSegment(store) (confirmed non-injective: paths.go:46-53 adds its ~hash only on changed=true, and MA-89 excludes whitespace tidying; pstStoreName defaults to 'Outlook Data File'/'Personal Folders', mbox uses filepath.Base='Local Folders'), disambiguate the second deterministically (append ~ShortHash of a stable source id) and use the SAME token for BOTH the on-disk dir AND the key — then the migration's firstSegment(Path) reproduces it for free and composes cleanly with D3-1. Graph needs no change (keys by mailbox address, injective — graph.go:208). Add a covering test with two same-named stores (slice-a's current two-store test uses distinctly-named stores and is structurally blind). Document the caveat: on one machine discovery order is sorted/stable so the token is stable run-to-run (R2 holds); a pa
- **F7.8-1** — DISSOLVED: Same content-driven self-heal as D3-1, PLUS a forward-version refusal in manifest Load: refuse Version > manifestVersion ('written by a newer mailarchive; upgrade this copy'), mirroring the already-shipped job.go:94-95 pattern (its comment even says 'like the manifest'); add the same guard to the index. Be honest in the doc: forward-refusal only protects v3-and-later against still-newer binaries — it CANNOT retroactively teach the already-shipped v2 binary (no forward guard) to refuse a v3 archive, so nothing prevents that one shipped binary from a single destructive excursion. The content-driven self-heal + verify's unexpected/unrecorded reporting is what bounds that damage to detectable/recoverable, and the README must state the all-machines-upgrade rule for a shared/synced -out (do not run an older mailarchive against an archive a newer one has written).
- **F7.8-2** — DISSOLVED: Decouple the two gates and state both in §3.1: gate the sentinel migration on a fixed literal `Version < 2` (NOT `< manifestVersion`), and gate the rekey on `Version < 3` (or, per D3-1, make it content-driven). Confirmed necessary: today the sentinel gate is `m.Version < manifestVersion` (manifest.go:125) with the stamp at 127-130; bumping the constant to 3 makes 2 < 3 fire, re-sentinelling every issue-free v2 record → Fillable()=true → source re-read/rewrite, and for a dead-source one-shot PST import (P3's own headline) permanent status WARN (health.go:92-93) and whole-archive 'unknown' report (report.go:81-82). The literal gate preserves MA-70 (v1 < 2 still sentinels) while never touching v2.
- **L10-1** — PARTIAL: MANDATORY (fully dissolves the lens-10 over-claim): state the condition inline next to the §5/README fixity claim — 'bit-rot, truncation and accidental edits are detected for artifacts written by this version or later; files in an archive created before the upgrade show as unrecorded until re-exported.' That satisfies lens 10's 'condition stated NEXT TO the claim'. RECOMMENDED but not sufficient-by-honesty-alone: the headline P2 value (fixity for multi-year offline archives) stays unmet for exactly the existing archives P2 names, since the v2->v3 migration rewrites no files (F2) and reindex checks existence only (reindex.go:74) — so add a one-time fixity-backfill (a verify/reindex mode that hashes on-disk files and records them as the baseline), documented as establishing a baseline from CURRENT bytes, not proof the legacy bytes were pristine. Marked PARTIAL because the claim becomes hon

## Conditions (folded into revision 2)

- **FC1 — Re-key is content-driven and self-healing; formats refuse forward.**
  A manifest entry is re-scoped iff its key holds exactly one NUL separator
  (a v3 key holds two); the file's version integer is never trusted for this.
  The index is repaired the same way: rows whose key holds one NUL are re-keyed
  from the row's own `path` (first segment), and the repair runs whenever the
  manifest load re-keyed anything OR the index's meta version is below target —
  never only on the index's own counter, which an old binary never regresses.
  Manifest `Load` and index `Open` refuse a stored version greater than the
  binary understands ("written by a newer mailarchive; upgrade this copy"),
  mirroring the job-file guard. The design states plainly that the already-
  shipped v2 binary has no forward guard, so a shared archive must be written
  only by upgraded binaries (README rule) and the content-driven repair is what
  bounds a v2 excursion to detectable and recoverable.
- **FC2 — The store token is injective and sticky.** Distinct sources that
  resolve to the same store segment (Outlook's default "Outlook Data File",
  Thunderbird's "Local Folders" under every profile) get distinct tokens: the
  manifest records `stores: sourceID → token`; the first source to claim a
  segment keeps the plain segment, a later different source with the same
  segment gets `segment~<8hex of sourceID>`. Tokens persist, so names are
  order-independent run to run (R2). The same token names the on-disk directory
  and the key. The source id is the original input path (never the -copy-first
  snapshot) or the Graph mailbox. A covering test uses two same-named stores.
- **FC3 — Migration gates are literals.** The v1 sentinel migration is gated on
  `Version < 2`, never on the current constant; the re-key is content-driven
  (FC1). Bumping the constant cannot re-sentinel a complete v2 archive.
- **FC4 — Fixity is honest about coverage, and can be established after the
  fact.** `verify -record` hashes the current bytes of every recorded file that
  lacks fixity and records them as the baseline, labelled as fixity from now,
  not proof the legacy bytes were pristine. `verify` exits 0 only when every
  recorded file was checked and intact and no record lacks fixity; exit 2 =
  not attested (modified, missing, or unrecorded > 0, each named, with
  `verify -record` as the remedy for the last); exit 1 = refusal or error. The
  summary always prints the unrecorded count and a WARN line when nothing was
  checkable; `-json` carries coverage fields. The README claim carries its
  condition inline ("for files written by this version or later, or
  baselined with verify -record"). `unexpected` files are reported, not an
  exit condition. The verify exit set is declared in docs/ux-contract.md X1
  with a covering test.
- **FC5 — The verify/backup collision is stated and legible.** verify holds the
  exclusive archive lock for its whole duration (F4 stands: an export cannot
  race the check). The design, `verify -h` and the README say a scheduled run
  whose trigger fires during verify refuses and records nothing (S25), so
  verify belongs outside the backup window; the lock holder line names the
  verb ("held by mailarchive verify …") so the refusal is self-explaining.
- **FC6 — verify trusts nothing in the manifest paths.** Every recorded path is
  rejected as an integrity failure unless it is a clean, relative,
  forward-slash path whose first segment is a store token and which contains
  no `..`; each path is resolved component-wise inside the archive root and any
  symlink at any level is an integrity failure (never followed); a non-regular
  file (FIFO, device, socket, directory) is classified without being opened;
  reads are capped at the recorded size + 1 byte (longer = modified).
- **FC7 — Remove reverses the XML too.** `Remove` deletes `<wrapper>.xml`
  (tolerating absence); the reversal assertions (MA-47, MA-79 lab) require it
  absent.
- **FC8 — The header block is labelled as what it is.** "Transport headers as
  stored (unverified)"; §5 states the block is sender-influenced text, not
  authentication; for PST it is decoded text, not verbatim bytes; the raw-source
  extractor uses the header section only when a blank line terminates it
  within 64 KiB, otherwise nothing is shown.
- **FC9 — Migration is visible.** The load logs the re-key count; the index
  repair logs "migrating index keys (N rows)" before it starts, runs in one
  transaction (idempotence and R5 come from it), and the README upgrade note
  states the one-time cost is proportional to archive size.
- **FC10 — verify is schedulable.** `schedule … -- verify -out DIR` is a valid
  job (MA-72's verb set extended; `-unattended` and `-log` apply), so cold
  storage can be checked periodically; the README suggests a weekly slot away
  from the backup.
- **FC11 — Fixity storage is compact and its cost stated.** The record carries a
  `fixity` object with up to three digests (html, zip, eml) — siblings are
  derived from the record's path, never repeated as keys; ~100 bytes per file,
  carried on every record and rewritten with the manifest at every checkpoint;
  compact JSON (nas-03) lands before slice (b).
- **FC12 — The new verb is wired into the grammar; status counts records.**
  `verify` appears in the root usage and the MA-39 help walk (naming `-out`,
  `-json`, `-record`); status prints "Fixity: N of M records carry digests"
  from manifest fields alone.
- **FC13 — StartBoundary is the next future occurrence** of HH:MM (today if
  still ahead, else tomorrow / next Sunday), so a fresh task is never a
  "missed start".
- **FC14 — The verify report is bounded.** Per-category detail lists cap at
  10,000 entries with exact counts and a visible "list truncated" note.
- **FC15 — The install-path swap is stated and checked.** F6 says plainly that
  `/TR` is replaced by `/XML` with real-host acceptance pending MA-79; after
  `/Create`, Install runs `schtasks /Query /TN name` and fails legibly if the
  task is absent; MA-65 is reconciled: the `/TR` quoting guarantee is replaced
  by XML escaping plus the wrapper's own quoting, and the test asserts the XML.
- **FC16 — Per-slice verdicts.** This review issues a verdict per slice
  (above) so one slice's condition does not gate the independent others.

## Refuted

- L1-f5-header-label-semantics (raw sources would show the full RFC 822 header
  block under the same label as PST's transport subset) — refuted as a
  non-failure: the content differs by source but nothing is misrepresented
  once FC8's label and honesty line apply; the `.eml` redundancy for KeepRaw
  sources is intentional.
