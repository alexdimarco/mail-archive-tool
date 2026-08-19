# Friction review — mail-archive-tool

Per assurance-kit `process/friction-review.md`. Walked as the actual actor
(typed the commands, read the errors, counted the steps). Failure modes are
first-class cells, not happy-path only.

Types: **I** security/inherent (count, don't fault) · **II** design-choice
(drives the verdict) · **III** buildable-away (operability backlog).

**12 cells walked · 12 functioning · 0 Type-II blockers · verdict: SHIPPABLE.**

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog |
|---|---|---|---|---|
| new-operator × first Outlook export (`-input x.pst -out ./a`) | yes | must know the `.pst` path | II→III | `-auto` discovers it; documented in README |
| new-operator × Thunderbird IMAP export | yes | the store path is deep (`snap/.thunderbird/xxxx.default/ImapMail/<acct>`) | III | `-auto` finds Thunderbird stores; GUI source picker browses to it |
| new-operator × mistyped input path | yes | `mailarchive: input X: no such file or directory`, exit 1 | I | — legible fail |
| new-operator × forgot `-out` | yes | `-out is required (or use a subcommand: serve, search)`, exit 1 | I | — names the fix (MA-33) |
| returning-operator × incremental re-run | yes | summary shows `exported=0 skipped(seen)=N` — obvious it did nothing new | I | — |
| searcher × find an email later | yes | must run `serve` then open a browser; no "open result N" from the CLI | II | acceptable; `search` gives paths, `serve` gives a reader |
| searcher × `serve` before any export | yes | `no usable index at …/search.db (run an export first)`, exit 1 | I | — fail-closed, names remedy (MA-34) |
| auditor × verify completeness | yes | `Verification: … issue(s) recorded in …/attachments-report.tsv` printed; tsv opens in a spreadsheet | III | summary already points at the report |
| recoverer × Ctrl-C mid-export | yes | `interrupted; progress saved to the manifest`; re-run resumes (skips seen) | I | — crash story intact (R5) |
| IMAP-operator × content not downloaded (cold) | yes | `Verification: N empty/not-downloaded`; guidance to enable offline + `-mode full`; `-enable-offline -sync-wait` automates it | II | inherent to IMAP; the tool assists (guided sync + wait) |
| inheritor × handed only the export dir, no tool | yes | opens `index.html` → browses folders; a single `.html` is self-describing offline | I | — the archive is legible without the tool (R7) |
| operator × `-enable-offline` while Thunderbird open | yes | `Thunderbird looks like it is running — close it, then re-run`; prefs untouched | I | — legible fail-closed (R9, MA-35) |

## Verdict

**SHIPPABLE.** Every cell functions and every failure mode refuses legibly and
names its remedy. No Type-II finding blocks ship. Backlog (Type III, tracked):
richer Thunderbird path discovery beyond `-auto`; a CLI "open a result"
affordance; the `-max-attachment` cap from the adversarial F6 limit.
