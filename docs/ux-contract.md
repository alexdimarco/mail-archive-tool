# UX contract — mail-archive-tool

Per assurance-kit `process/ux-contract.md`. Numbered X-series consistency
invariants for the CLI, each wired to its enforcement (a catalog MA- row) or
recorded as a condition where the code doesn't yet meet it. X-compliance is
necessary but not sufficient — the friction walk (`docs/review-friction.md`)
stays on top.

| X | Invariant | State | Enforcement / condition |
|---|---|---|---|
| X1 | **Typed exits, named once.** Exit codes come from one declared set. | Partial | Today: `0` ok · `1` any error/refusal · `130` interrupt. Refusals are proven to return non-zero and name the problem (MA-33, MA-34). **C1:** split usage/refusal to a distinct `2` from runtime `1`; until then `assure.Refused` asserts `Code(1)`. |
| X2 | **One refusal voice.** Every refusal states WHAT failed and (where actionable) the remediation, via one path. | Partial | Errors surface as `mailarchive: <message>` on stderr, exit non-zero (`cmd/mailarchive/main.go`). MA-33/34 assert the message names the offending flag/file. **C2:** route all refusals through one formatter; no bare `fmt.Fprintln(os.Stderr)`+exit scattered. |
| X3 | **Help is total.** The root and every subcommand emit usage naming their flags. | Met | MA-39 walks `-h`, `serve -h`, `search -h` and asserts each prints usage naming its flags/commands. |
| X4 | **One grammar.** Subcommands are verbs (`serve`, `search`); flags are kebab-case (`-copy-first`, `-enable-offline`, `-sync-wait`); repeated concepts share a name (`-out`, `-input`). | Met (by convention) | Reviewed; consistent. **C4:** a parser-tree lint would make this structural (backlog). |
| X5 | **Flags mean the same everywhere.** `-out` is the archive dir in export/serve/search; `-input` is always a source; `-mode` is always incremental/full. | Met | Same flag names carry the same meaning across the three surfaces (`cmd/mailarchive/main.go`). |
| X6 | **A legibility surface per feature.** Completion prints a summary with GREEN/WARN counts and points at remediation. | Met | The export summary prints `exported/skipped/attachments/inline` and a `Verification:` line with the report path (`printSummary`); IMAP prep prints store-size progress. |
| X7 | **No traceback reaches an operator.** A crash is never the operator-facing failure mode. | Met | Per-message `recover` (F1) + `assure.Refused`'s default `forbid` of `panic`/`goroutine` guards refusal messages; MA-33 asserts no crash text in refusals. |
| X8 | **Output discipline.** stdout = the answer (search results, the served URL); stderr = the operator conversation (progress, warnings, refusals). | Met | `search` prints results to stdout; `serve` prints the URL to stdout; export progress/warnings/refusals go to stderr (`log.New(os.Stderr,…)`). |

## Conditions (backlog, tracked)

- **C1** — introduce a typed usage/refusal exit `2` distinct from runtime `1`;
  update `assure.Refused` call-sites to `Code(2)` for usage errors.
- **C2** — one refusal formatter; ratchet down scattered stderr-print+exit sites.
- **C4** — parser-tree lint for X4 (kebab-case flags, verb subcommands).

None of these block ship; they are the standing ratchet items.
