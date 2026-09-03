# Pre-code design review — incremental completeness

**Design reviewed:** `docs/design-incremental-completeness.md` revision 1 (2026-09-02).
**Procedure:** assurance-kit `process/design-review.md` — 10 lenses run as 5 independent
reviewer passes (two adjacent lenses each), a refutation-default skeptic per finding, then
the rescue pass. Executed as a 91-agent workflow (this doc + the sibling schedule-v2 review);
per-agent transcripts in the session's workflow journal. **39 findings raised · 38 survived
the skeptic · 1 refuted.** Rescue: 10 of 10 surviving blocker/high findings DISSOLVED by
modest mechanism changes; one hidden blocker surfaced.

**Verdict: GO_WITH_CONDITIONS.** Conditions B1–B12 below are must-fix-before-code; each is
folded into design revision 2 and re-verified against the catalog.

## Hidden blocker (found by the rescue pass, missed by all ten lenses)

The design had no notion of *terminal* vs *fillable* incompleteness. Composed with §3.4
(an incomplete Graph record "falls through to download + retry") and the §2 concession
(legitimately-empty messages and server-deleted attachments stay incomplete forever), every
nightly Graph run would re-download the full MIME of every permanently-empty message,
forever — breaking R17/MA-62 and the "cheap re-runs" property. → **B1**.

## Findings → conditions

| ID(s) | Lens | Section | Claimed failure (verified) | Verdict | Condition |
|---|---|---|---|---|---|
| F1, L4-1, L9-1, F2, F6, L10-3 | 1, 2, 4, 8, 9, 10 | §3.4, §2, §4 | Graph is complete-at-fetch, so an incomplete Graph capture can never fill; re-fetching it every run is pure cost and contradicts R17 as written; §4 omitted the R17/MA-62 consequence | PARTIAL/CONFIRMED (medium) | **B1** |
| L4-2, F4 (lens 7) | 4, 7 | §2 non-goals + schedule-v2 posture | Nothing distinguishes fillable from terminal gaps, so a healthy archive with one empty message is WARN forever | CONFIRMED (high) | **B1** |
| F2 (lens 7), F4 (lens 1), L3-2 | 1, 3, 7 | §3.2 retry rule | Count comparison discards a genuine fill whose missing-set changes composition | CONFIRMED (high) | **B2** |
| L10-1, F3 (lens 1), L3-1, F5 (lens 8) | 1, 3, 8, 10 | P6 + §3.1 | Legacy hint tied to a load-time transient; the first Save of any mode ends it while nothing was filled | CONFIRMED (blocker) | **B3** |
| F3 (lens 8), L5-6 | 5, 8 | §3.3 / P3 | Regenerated report deletes a legacy archive's existing durable findings | CONFIRMED (high/medium) | **B3** |
| F1 (lens 8), L3-5 | 3, 8 | §3.1 | An older binary that SAVES rewrites the manifest as v1, silently erasing completeness data | PARTIAL (high) | **B4** |
| L10-2, L3-3 | 3, 10 | P4, §5 | "No partial file" is claimed with rename atomicity only; no fsync of temp or directory | CONFIRMED (high/medium) | **B5** |
| L6-3, L3-4, L9-3, F8, F7 (lens 7) | 3, 6, 7, 9 | §3.2 commit / P4 | Deterministic temp names race two processes; stale `*.tmp` and orphan zips never swept; retry crash ordering unstated | PARTIAL/CONFIRMED (low) | **B6** |
| F6 (lens 1) | 1 | §3.2 step 1 | The `-since` filter runs before the retry decision, so windowed runs never revisit older gaps | CONFIRMED (medium) | **B7** |
| F5 (lens 2), L9-2 | 2, 9 | §3.1/§3.3 | Date/Subject persisted on every record; growth unbounded | CONFIRMED (medium/low) | **B8** |
| L6-2 (+ schedule-v2 F2) | 6 | OMISSION | No cross-process lock on `-out`; a scheduled run overlapping a manual one clobbers the manifest/index | CONFIRMED (high) | **B9** |
| L10-5 | 10 | §2 | "Parse cost only" is false: the retry renders and writes temp files | PARTIAL (low) | **B10** |
| L10-4 | 10 | P2 | The fill precondition (client synced) sits in the appendix, not at the claim | PARTIAL (low) | **B11** |
| F7 (lens 2) | 2 | §4 | Atomic writes (P4) are an independently shippable slice bundled into the schema change | PARTIAL (low) | **B12** |
| L6-2 rider | 6 | run.go/graph.go | `index.Add` failures are swallowed as warnings (R8 parity silently broken) | CONFIRMED (medium) | **B9** |
| (1 refuted) | — | — | recorded in the journal with its refuting quote | REFUTED | — |

## Conditions (all folded into revision 2)

- **B1 — Terminal vs fillable.** Each missing item carries a class. A source that is
  complete-at-fetch (Graph today) marks its gaps *terminal*: recorded, reported as
  "source-empty", never re-downloaded — R17 and MA-62 stay **unchanged**. On-demand
  sources (PST/OST, Thunderbird, Evolution) mark gaps *fillable*. Counts, the report,
  the summary and `status` distinguish the two; posture warns on fillable only.
- **B2 — Subset promotion.** A retry is promoted when any previously-missing item is now
  present (old ⊄ new); the record then carries the freshly computed sets.
- **B3 — Sentinel migration.** Loading a version-1 manifest marks every record
  `Missing:["unknown"]` (fillable); each is re-examined once by the normal retry path on
  the next incremental run. No transient flag. An existing `attachments-report.tsv` is
  renamed to `attachments-report-legacy.tsv` before the first regenerated report.
- **B4 — Downgrade safety.** A version-1 manifest that already holds entries is treated
  exactly as B3 (completeness unknown → sentinel). A downgrade therefore costs one
  re-examination pass, never silent "complete" records. Stated in the README.
- **B5 — Durability, honestly.** Temp files are fsynced before rename and the directory
  after (best-effort); `Manifest.Save` likewise. P4's namespace guarantee (no partial
  file recorded) is proven by fault injection; content durability on power loss is
  labelled *conditional on the filesystem honouring fsync*.
- **B6 — Temp hygiene.** Temps are created with `os.CreateTemp` (unique per process);
  stale `.mailarchive-*.tmp` older than the run start and orphan `-attachments.zip`
  files with no sibling `.html` are swept at run start and by `reindex`. Retry commit
  order is stated (zip, then html; the record advances only after both).
- **B7 — Retry ignores `-since`.** A seen-incomplete entry is re-examined regardless of
  the date window; the window applies to unseen messages only.
- **B8 — Bounded growth.** Subject/Date are persisted only on records with a non-empty
  Missing or Unresolved set; the bound (entries with issues × ~100 B) is stated.
- **B9 — Run lock and index errors.** `app.Run`/`RunGraph` hold an exclusive lock on
  `<out>/.mailarchive.lock` for the whole run; a second run refuses with a typed message
  naming the lock. `index.Add` failures are counted and surfaced (summary + status).
- **B10 — Cheap re-examination.** The retry first *probes* (body presence, attachment
  sizes via a counting writer) with no render and no temp files; only a probe that shows
  an improvement captures and commits.
- **B11 — Claims inline.** P2's fill half is conditional on the client having synced the
  content; stated at the claim.
- **B12 — Build order.** Atomic writes + sweep (B5/B6, MA-69) land as their own commit
  before the schema change.

Type-III operability backlog shipped with the feature: none beyond the above.
