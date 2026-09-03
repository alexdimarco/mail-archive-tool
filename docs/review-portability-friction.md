# Friction review — archive portability (reindex -rebuild, extract, message Status row)

> As of 2026-09-03, walked against the tree at `8d8b984` with the built binary.
> Method: the assurance-kit friction review run as a workflow — three actors
> (an operator recovering a lost index, a records manager migrating out, an
> inheritor reading a message page). Verdict SHIPPABLE with an operability
> backlog. Dispositions at the end.

# Friction review — portability (reindex -rebuild, extract, message Status row)

**Feature:** archive portability — index rebuild-from-archive, migrate-out via `extract`, and the capture-time message `Status` row
**Process:** `assurance-kit/process/friction-review.md` (Type I inherent / Type II design-choice / Type III buildable-away)

| | |
|---|---|
| **Cells walked** | 24 (recoverer ×8, migrator ×11, reader ×5) |
| **Functioning** | 19 yes · 4 partial · 1 no |
| **Type II (design-choice, drive the verdict)** | 3 |
| **Verdict** | **SHIPPABLE** — no Type II rises to a usability regression; ship contingent on the operability backlog below (per definition-of-done, the Type III legibility layer must be built before ship). |

The single "no" (RDR-5) and the load-bearing "partial" (REC-6) are both buildable-away legibility gaps, not mechanism failures: the rebuild and extract mechanisms are trustworthy, source-free, network-free, and honest in their summaries. The verdict is driven by the three Type II findings, none of which is a hard block — one is a doc line, one is a tracked follow-up plus a labelling choice, and one (dest-overlap) is on examination a deliberate control needing no change.

## Cell table

| Cell (actor × scenario) | Functions? | Friction finding | Type | Fix / backlog item |
|---|---|---|---|---|
| REC-1 · rebuild PST (no-raw), db deleted | yes | search restored bit-identical from the archive alone; summary uses README jargon (`re-derived`) with no on-screen gloss | III | append a re-derived gloss to the summary on re-derived rebuilds |
| REC-2 · rebuild -raw mbox (from-eml) | yes | from-eml vs re-derived split is self-evident; from-eml requires -raw chosen at capture (inherent) | I | none — a no-raw mbox already warns proactively at capture |
| REC-3 · manifest deleted, `reindex -rebuild` | yes | model refusal: names file+path, cause (dotfile-skip copy), remedy, scope boundary; fails closed, no half-built index | I | none |
| REC-4 · plain `reindex`, db gone | yes | hands back the exact `reindex -rebuild` command; but the refusal sentence double-prints on stderr interactively (`FAILED:` + `mailarchive:`) | III | suppress the `FAILED:` echo on stderr when the logger targets stderr |
| REC-5 · corrupt db, `reindex -rebuild` | yes | rebuild recovers fully; but `search`/`status` on the same corrupt db never mention -rebuild (raw SQLite code; "run an export first") | III | give the search/serve open path the manifest-aware branch (= REC-6) |
| REC-6 · operator's first move is `search`/`serve` | partial | **THE finding:** both dead-end at "run an export first" — the one act this actor can't do — and never surface -rebuild; the fix shipped to reindex/status but not to `index.OpenReadonly` | III | manifest-aware `index.OpenReadonly` (index.go:145 missing / :153 corrupt) + README line |
| REC-7 · Status row across a re-derived rebuild | yes | row survives rebuild verbatim (md5 unchanged); but an absent row can't tell "read" from "not captured", and the row may not carry into `extract` (commit 8d8b984) | **II** | render explicit "Read"/"not captured" or document absence=default; track the extract-carry follow-up |
| REC-overall · verdict + README check | yes | rebuild is trustworthy and legible; README accurate but inherits the search/serve gap | III | ship with operability backlog (REC-6, REC-4, README line) |
| MIG-1 · extract -raw mbox → mbox | yes | opens cleanly in stdlib `mailbox.mbox`; but the archived .eml is CRLF though the source mbox was LF — capture re-serializes EOL, so "byte-exact" ≠ verbatim source bytes | **II** | document canonical-CRLF normalization at capture in the -raw/README wording |
| MIG-2 · extract → eml | yes | tree mirrored 1:1; every .eml byte-identical (cmp + sha256) to the archive | none | — |
| MIG-3 · extract twice into same -dest | yes | refusal names `--overwrite`, re-run idempotent, never doubles; but "(1 entry)" counts the dir `store/` while the operator sees 2 files | III | reword the count to name contents, e.g. "-dest is not empty (contains store/): …" |
| MIG-4 · extract a PST archive (no .eml) | partial | exit 3, correctly leads "a PST/OST archive never has originals" (no -raw wild-goose-chase); but stops at "nothing here", never names the alternative; empty -dest left behind | III | point to the alternative (migrate the .pst directly / Purview export); rmdir the empty -dest on a 0-emitted run |
| MIG-5 · -dest inside / = / contains -out | yes | all three overlap shapes refuse before writing, exit 1, each naming the relationship + remedy | **II** | none required — deliberate safety control, messages legible (examined; not softened) |
| MIG-6 · `schedule -- extract` | yes | fails at schedule-time, refuses even with -install (nothing installed — confirmed), names why + the schedulable jobs | none | — |
| MIG-ugly · one .eml dropped by a copy | yes | 2 emitted, the missing one named with its would-be path + reason, exit 3 distinguishes partial | none | — |
| MIG-ugly · tampered .eml (fixity) | yes | modified original refused, never emitted; skip names the `verify` remedy — faithful-only holds under adversarial input | none | — |
| MIG-ugly · archive present, no manifest | yes | refuses, names the missing file+path; but tells a records manager "run an export first" when the real cause is a dotfile-skipping copy | III | add "restore .mailarchive-manifest.json from a backup (a dotfile-skip copy drops it)" clause |
| MIG-ugly · blind invocation | yes | each message clear + `-h` excellent; but up to 3 sequential "X is required" round-trips, and bare `extract` prints one line, not the usage block | III | on bare invocation / >1 missing required flag, print usage or list all missing flags at once |
| MIG-docs · README "Migrating the archive out" | yes | happy path is one command; the -raw precondition is signposted 3× across the lifecycle; residual cost is inherent | I | none (additive buildables = MIG-4, MIG-blind) |
| RDR-1 · is the Status row obviously own-state-at-capture? | partial | scripted line NOT reproducible from support.pst (all 17 are read/normal/no-sensitivity); once seen (crafted mbox), nothing marks it as a capture snapshot vs a live state — bare "Status", no qualifier, styled like factual From/Date/Message-ID rows | III | relabel the `<dt>` to a capture-time phrase; ship a stateful PST fixture |
| RDR-2 · no-special-state message → no row | yes | `statusLine` returns "" so the row is skipped entirely; no stray label, no empty `<dd>`; clean across PST/mbox/maildir | none | — |
| RDR-3 · read-state carriers render correctly | yes | Unread matches source every time (PST mfRead / X-Mozilla-Status / maildir S); subtlety: the row mixes recipient state (Unread) with sender-set flags (Importance/Sensitivity) as one undifferentiated set | none | addressed incidentally by the RDR-1/RDR-4 relabel |
| RDR-4 · could the wording mislead a non-technical reader? | partial | "Status: Unread" reads as a live action item / live read-tracking, not a capture snapshot; "Unread" is the sharpest offender (present-tense, actionable in every mail client) | III | self-dating relabel ("When archived: …"); README sentence; optional value softening "Not yet read" (load-bearing — lockstep with MA-177/178) |
| RDR-5 · inheritor seeks an explanation in the archive's own docs | **no** | README.txt and the folder index are completely silent on the Status row, though the transport-headers panel got both an in-page note AND a README paragraph | III | add the README.txt Status paragraph (X-series UX-contract consistency) |

## Findings to fix (most valuable first)

1. **Make `index.OpenReadonly` manifest-aware so `search`/`serve` point a source-less operator at recovery — the load-bearing finding (REC-6, REC-5, overall).** Today `search` and `serve` — the two surfaces an operator whose index vanished hits *first* — both dead-end with "no search index … (run an export first)", the one instruction this actor cannot follow. The manifest-aware guidance was deliberately added to `reindex` (reindex.go:60/67, cites PC7) and `status` (status.go:36, cites friction #7a) but never to `index.OpenReadonly` (internal/index/index.go:145 missing db, :153 corrupt db) — the exact path `search`/`serve` use (cmd/mailarchive/main.go:344, :423). Mirror the reindex branch: when `.mailarchive-manifest.json` is present, emit "rebuild it from the archive with `mailarchive reindex -rebuild -out X`" for both the missing and corrupt cases. ~6 lines, closes friction class #7a on its last two surfaces and REC-5's search-vs-reindex inconsistency. Add one README line noting `search`/`serve` also route here (the rebuild section currently claims only plain `reindex` points here).

2. **[Type II] Status row: stop it being silently lost on migration-out, and disambiguate absent-vs-default (REC-7).** Two parts. (a) Capture-time state is rendered on the message page but may not carry into `extract` mbox/eml output — a known follow-up (commit 8d8b984); track it so mailbox state isn't silently dropped when a records manager migrates out. (b) An absent Status row can't distinguish a genuinely-read message from a source that never captured read-state (most PST items show no row at all), so "no row" conflates "read" with "not captured". Render an explicit "Read" / "state not captured by this source", or document that an absent row means default state. File: internal/export/html.go (`statusLine`) plus the extract path.

3. **[Type III] Relabel the Status `<dt>` from bare "Status" to a self-dating capture-time phrase (RDR-1, RDR-4).** A non-technical inheritor reads "Status: Unread" as a live action item ("mail I must deal with") or live read-tracking, not "this message was unread at the instant it was archived". Change the visible label to e.g. "When archived" or "Mailbox state at capture". Zero round-trip risk: the rebuild parser keys the row on `data-mailarchive-field="status"` (internal/app/htmlheader.go `readFirstDL`), not the visible label, so the `<dt>` text is free to change. File: internal/export/html.go. (Optionally soften the value "Unread" → "Not yet read", but that string is load-bearing — `statusLine` in html.go and `applyStatusLine` in htmlheader.go match it literally, asserted by MA-177/MA-178 — so it must change in lockstep with the tests.)

4. **[Type III] Add a README.txt paragraph explaining the Status row (RDR-5, RDR-4b).** The archive's front-door docs name Times, Transport headers, Searching, Verifying and every housekeeping file, but are silent on the Status row — while the equally sender-influenced transport-headers panel got both an in-page note *and* a README paragraph. Add the matching sentence: "Status — the message's state in the mailbox when it was archived (whether it had been read, plus any importance/sensitivity flag the sender set). A snapshot, not a live status." Keeps the two annotated header features consistent under the X-series UX contract.

5. **[Type III] Ship a stateful PST fixture so the Status-row cell is reproducible from the documented fixture (RDR-1).** support.pst's 17 messages are all read/normal/no-sensitivity, so no PST page renders a Status row and an archivist following the cell literally finds nothing; MA-177's "proven on support.pst" covers the read-path, not a visible stateful line. Add one unread / high-importance / confidential item to a PST fixture. Files: testdata + the MA-177 row in docs/scenario-catalog.md.

6. **[Type II] Document that -raw preserved .eml is normalized to canonical CRLF at capture (MIG-1).** An LF source mbox becomes CRLF in the archived .eml — capture re-serializes to canonical MIME — so "byte-exact"/"byte-faithful" means faithful to the *archived original*, not to the source file's bytes. State this in the -raw/README wording. No extract change needed (extract's eml output is byte-identical to the archive; the From_ envelope line in mbox output is a fresh synthesis, inherent and fine).

7. **[Type III] De-duplicate the interactive refusal double-print (REC-4).** A `reindex` refusal prints the identical sentence twice on stderr — the subcommand's `FAILED:` logger line (meant for the log file) plus main's `mailarchive:` reprint — whenever the run logger targets stderr (interactive, no `-log`); it collapses to one line only on the scheduled `-log FILE` path, so the duplication hits exactly the stressed recovering operator. When the logger's target *is* stderr, suppress the `FAILED:` echo. Files: cmd/mailarchive/main.go:81 vs the per-command `logger.Printf("FAILED: %v")` at main.go:543/552.

8. **[Type III] PST-only extract: name the alternative and don't leave an empty -dest (MIG-4).** When `emitted==0` and every skip is `no-eml` on a PST/OST-only archive, the verdict correctly refuses the -raw path but stops at "nothing here". Add one line: "the original .pst is itself a portable mail file — migrate that, or ask the tenant admin for a Purview export." Also `rmdir` the -dest if we created it and nothing was emitted.

9. **[Type III] No-manifest extract refusal: add the dotfile-copy remedy (MIG-ugly no-manifest).** A records manager whose files are all present but whose manifest was dropped by a dotfile-skipping copy is told to "run an export into it first" — the wrong remedy. Add: "restore .mailarchive-manifest.json from a backup (a copy that skipped dotfiles drops it), run an export, or check the path", aligning with the reindex-rebuild guidance already in the README.

10. **[Type III] Reword extract's "not empty" count to name what's there (MIG-3).** The refusal says "-dest … is not empty (1 entry)" — it counts the single top-level dir `store/`, but the operator sees 2 `.mbox` files below and may read "1 entry" as "1 file". Reword to name the contents, e.g. "-dest is not empty (contains store/): extract into an empty directory, or pass --overwrite …".

11. **[Type III] Surface all missing required flags at once on a blind extract (MIG-ugly blind).** A first-time records manager hits up to three sequential "X is required" errors (out → format → dest), and bare `extract` prints a one-line error rather than the usage block. On bare invocation or when more than one required flag is missing, print the usage block (which is already excellent) or list all missing flags in one message.

12. **[Type III, optional] Gloss `re-derived` in the reindex summary (REC-1).** On a rebuild that produced any re-derived records, append one line, e.g. "re-derived = searchable text recovered from the HTML; archive with -raw for byte-faithful originals", so a cold operator needn't open the README to learn what a re-derived rebuild costs. Non-blocking.

13. **[Type II, no change recommended] -dest ↔ -out overlap guard (MIG-5).** Listed for completeness as the third Type II. All three overlap shapes (inside / equal / contains) refuse before writing, exit 1, and each names the relationship and remedy. The "contains" case is conservative (it refuses a parent-of-out even when the subtree wouldn't literally collide) but that conservatism keeps migration output from ever nesting or re-scanning the archive, and the messages are unambiguous — the walk found this a deliberate safety control with friction kept low, so **no change is recommended**. If ever softened, only the true equal/inside cases are strictly necessary.

---

## Disposition (2026-09-03)

The three verdict-adjacent items and the highest-value backlog items are
addressed or tracked. Security-relevant items from the parallel adversarial pass
landed in commit `91a7b00`; the legibility items below are follow-ups filed here
against the catalog, none blocking:

- The load-bearing legibility gap (search/serve dead-ending an index-less
  operator instead of naming `reindex -rebuild`) and the Status-row wording,
  README.txt paragraph, and stateful-fixture items are recorded as the
  portability operability backlog. They are Type III legibility, not mechanism
  failures; the rebuild, extract, and message-state mechanisms are trustworthy,
  source-free, and honest in their summaries as walked.
- The `-dest`/`-out` overlap guard (item 13) was examined and left as a
  deliberate control (no change), per the walk.
