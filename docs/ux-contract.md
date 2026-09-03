# UX contract — mail-archive-tool

Per assurance-kit `process/ux-contract.md`. Numbered X-series consistency
invariants for the CLI, each wired to its enforcement (a catalog MA- row) or
recorded as a condition where the code doesn't yet meet it. X-compliance is
necessary but not sufficient — the friction walk (`docs/review-friction.md`)
stays on top.

| X | Invariant | State | Enforcement / condition |
|---|---|---|---|
| X1 | **Typed exits, named once.** Exit codes come from one declared set. | Partial | Today: `0` ok — including a handled interrupt (a single Ctrl-C / SIGTERM), which logs "interrupted; progress saved to the manifest" and exits 0 · `1` any error/refusal. The tool never returns `130` itself; only a signal it does not catch reaches Go's default handler, which the shell reports as 128+signum. Refusals are proven to return non-zero and name the problem (MA-33, MA-34). **C1:** split usage/refusal to a distinct `2` from runtime `1`; until then `assure.Refused` asserts `Code(1)`. |
| X2 | **One refusal voice.** Every refusal states WHAT failed and (where actionable) the remediation, via one path. | Partial | Errors surface as `mailarchive: <message>` on stderr, exit non-zero (`cmd/mailarchive/main.go`). MA-33/34 assert the message names the offending flag/file. **C2:** route all refusals through one formatter; no bare `fmt.Fprintln(os.Stderr)`+exit scattered. |
| X3 | **Help is total.** The root and every subcommand emit usage naming their flags. | Met | MA-39 walks `-h`, `serve -h`, `search -h`, `reindex -h`, `schedule -h`, `graph -h`, `status -h` and asserts each names its flags. The root `-h` also enumerates every subcommand (`exportUsage`). |
| X4 | **One grammar.** Subcommands are verbs (`serve`, `search`, `reindex`, `schedule`, `graph`, `status`); flags are kebab-case (`-copy-first`, `-enable-offline`, `-sync-wait`, `-client-secret-file`); repeated concepts share a name (`-out`, `-input`, `-mode`, `-log`). A *flag* (`-outlook`) augments the default export; a *subcommand* (`graph`) is a distinct acquisition mode; `schedule … -- <job>` carries any backup job verbatim. | Met (by convention) | Reviewed; consistent. **C4:** a parser-tree lint would make this structural (backlog). |
| X5 | **Flags mean the same everywhere.** `-out` is the archive dir across export/serve/search/reindex/schedule/graph/status; `-input` is always a source; `-mode` is always incremental/full; `-log` is always the run log file. The scheduler dry-parses jobs through the same flag builders the jobs run with (`cmd/mailarchive/job.go`), so a flag cannot mean two things. | Met | Same flag names carry the same meaning across every surface; MA-72 proves the shared parse. |
| X6 | **A legibility surface per feature.** Every feature's state is inspectable with GREEN/WARN/RED posture, each WARN/RED naming its remedy. | Met | `mailarchive status -out DIR` (MA-75, R18): completeness (fillable/terminal/unknown), last run (running/ok/failed/cancelled, MA-76), schedule (descriptor + three-state host query, MA-78), executable identity; fails closed on missing evidence. The export summary and the GUI summary/health view carry the same counts. |
| X7 | **No traceback reaches an operator.** A crash is never the operator-facing failure mode. | Met | Per-message `recover` (F1) + `assure.Refused`'s default `forbid` of `panic`/`goroutine` guards refusal messages; MA-33 asserts no crash text in refusals. |
| X8 | **Output discipline.** stdout = the answer (search results, the served URL); stderr = the operator conversation (progress, warnings, refusals). | Met | `search` prints results to stdout; `serve` prints the URL to stdout; export progress/warnings/refusals go to stderr (`log.New(os.Stderr,…)`). |

## Conditions (backlog, tracked)

- **C1** — introduce a typed usage/refusal exit `2` distinct from runtime `1`;
  update `assure.Refused` call-sites to `Code(2)` for usage errors.
- **C2** — one refusal formatter; ratchet down scattered stderr-print+exit sites.
- **C4** — parser-tree lint for X4 (kebab-case flags, verb subcommands).
- **C5** — a machine-readable health signal from `status` (a `-json` form or a
  distinct exit code for RED) so monitoring need not scrape stdout; today
  `status` exits 0 whenever it reports and the posture is textual (S29/MA-75).

None of these block ship; they are the standing ratchet items.
