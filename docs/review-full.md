# Full design + friction review — mail-archive-tool

Cross-cutting review of the whole tool after the v0.2.x work (6 subcommands, 5
sources, R1–R17). Two lenses: **design coherence** and a **friction walk**, with
a dedicated **"language matches function"** audit (help text, catalog prose, and
docs vs. what the code actually does). Walked as the actual actor (commands typed,
output read); docs cross-checked against code. Cheap mismatches were fixed in the
same pass — each is marked **[fixed]** below.

## 1. Design coherence — sound

One spine, many sources. Every source normalizes to `model.Message`, then the
*same* pipeline runs: `export` (self-contained HTML + attachment zip) → FTS
`index` → browsable `pages` → crash-safe `manifest`. Adding a source never
touches the downstream — proven by how cleanly PST, mbox/maildir, Evolution,
Outlook-COM→PST, and Graph all plugged in.

**Source-selection model (now documented, was implicit):** local stores via
`-input`/`-auto`; a *flag* (`-outlook`) augments the local export (it produces
PSTs then exports them); a *subcommand* (`graph`) is a distinct acquisition mode
(network, per-mailbox, app auth). This principle is now stated in
`docs/ux-contract.md` X4 so future sources (e.g. IMAP) know a network source is a
subcommand, not a flag. **[fixed]**

Access-reality boundary, and it's coherent:

- **Tenant you administer** (St. Augustine's; own domains) → server-side Graph
  app-only (`graph`), zero client footprint.
- **Borrowed-access tenant** (U of T) → local-client capture (`-outlook`,
  Thunderbird/Evolution stores) riding the user's already-approved session, since
  you can't authorize an app there.

## 2. Friction walk — every surface exercised

Refusal voice is **uniform** across all six surfaces: `mailarchive: <problem>`,
exit 1, no stack trace. Help is total (each subcommand's `-h` names its flags;
the root now enumerates every subcommand). stdout/stderr discipline holds (results
& URL to stdout; progress & refusals to stderr).

| Cell (actor × scenario) | Functions? | Finding | Status |
|---|---|---|---|
| operator × root `-h` | yes | header said "export **Outlook .pst/.ost**"; Usage/Examples omitted reindex/schedule/graph | **[fixed]** header broadened; all subcommands + a Graph example listed |
| operator × `reindex`/`schedule`/`graph` refusals | yes | each names the missing flag, exit 1 | ok |
| operator × forgot `-out` (export) | yes | hint listed only "serve, search" | **[fixed]** now lists all five subcommands |
| operator × `serve`/`search` with no index | yes | error leaked "…out of memory (14)" (misleading; it's file-not-found) | **[fixed]** `OpenReadonly` stats first → "no search index at X (run an export first)" |
| operator × `-auto` help | yes | said "Outlook…; Thunderbird…" — omitted Evolution (which it discovers) | **[fixed]** flag help + `run.go` doc comments now say Evolution |
| operator × generated cron marker | yes | doubled: `# mailarchive-backup mailarchive-backup` | **[fixed]** default name no longer repeated |
| operator × `graph` help/refusals | yes | secret only via env var; refusals name each input | ok |
| operator × `-outlook` off Windows | yes | immediate clean refusal (fixed in v0.2 review) | ok |

## 3. Language matches function — audit + fixes

Beyond the friction table, a docs-vs-code sweep found and fixed:

- **`CLAUDE.md` pointed agents at a nonexistent `docs/invariants.md` (3×) and an
  `INV-N` marker syntax that isn't used** (the real marker is `MA-N`). Corrected
  to `docs/scenario-catalog.md` and `MA-N`, and the source list broadened to all
  four sources. Block meta-test re-run green. **[fixed]**
- **`docs/plan-imap-m365-mvp.md` was superseded** by the Graph app-only source
  and defined a **conflicting "R17"** (Live IMAP) that clashes with the shipped
  R17 (Graph). Added a SUPERSEDED banner pointing at the Graph implementation and
  neutralized the R17 wording. **[fixed]**
- **`docs/ux-contract.md` X3/X4/X5 described the old three-subcommand world.**
  Updated: X3 now lists MA-39's six `-h` targets + the root enumeration; X4/X5
  enumerate all current subcommands and the flag-vs-subcommand principle. **[fixed]**
- **`README.md` said `AddStore`; code uses `AddStoreEx`.** **[fixed]**
- **Catalog R9 ("single write path")** predated `-outlook`'s Send/Receive.
  Reworded to name both opt-in local sync/write paths and affirm the source
  mailbox is never modified (Graph is GET-only). **[fixed]**
- **Catalog invariants were out of numeric order** (R14 → R17 → R16 → R15).
  Reordered R15/R16/R17. **[fixed]**
- **`docs/review-friction.md`** (the v0.1 walk) now carries a scope banner
  pointing at `review-v0.2.md` and this doc. **[fixed]**
- Minor vestiges: `graph-app-setup.md` IMAP-era heading tidied. **[fixed]**

## 4. Consistency check

- **Refusal voice / exit codes:** uniform (`mailarchive: …`, exit 1). Matches
  X1/X2 (the standing C1/C2 conditions — a distinct usage exit `2`, one formatter
  — remain backlog, not regressions).
- **Flag grammar:** kebab-case throughout; `-out`/`-input`/`-mode`/`-since` mean
  the same on every surface. ✓
- **Catalog integrity:** R1–R17, S1–S20, MA-01–MA-64 — all present, no numbering
  gaps; ordering fixed. The covers-map + no-vacuous gates stay green.
- **Store naming:** every source's top-level dir is a human label (PST display
  name, folder base, Evolution account / "On This Computer", Graph UPN, Outlook
  account name). ✓

## 5. Verdict & remaining backlog

**Verdict: coherent and shippable.** The system model is clean, the surfaces are
consistent, and after this pass the language matches the function across help,
catalog, and docs. No Type-II blocker.

Backlog (tracked, none blocking):

- **C1/C2** (ux-contract): a distinct usage exit code `2` and a single refusal
  formatter.
- **Lab-tier validation still pending:** MA-60 (`-outlook` COM on real Outlook,
  via `scripts/test-outlook.ps1`) and MA-64 (`graph` against a real M365 tenant).
- `schedule`'s host-apply wrappers (cron/launchd/schtasks) are unit-tested only
  (a fake-`crontab` round-trip would close this).
- GUI runtime is compile-verified only (no display in CI).
- `-interval` value list ordering differs cosmetically across help/README (all
  three values valid).
