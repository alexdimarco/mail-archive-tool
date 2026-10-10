# Adversarial pass — slice LC-C1 (local web surfaces, part one)

**17 found / 9 confirmed / 2 refuted** (and 5 duplicates, 1 non-goal, counted apart).
Of the 9 confirmed: 6 fixed with a tombstone, 2 left open as skip encoders and
escalated (AI2, AC4), and 1 fixed in part with its other half escalated (AC8). No
finding was above low after the skeptic's pass (AI1 and AC1 were raised as medium and
ruled low).

## Scope

- Slice LC-C1: the Host check (`localweb.Wrap`), the one route model (`localweb.Mux`
  and its structural check), the response headers, the `/files/` allow-list served
  through `os.Root`, and credentials outside the archive (P10) — local confidentiality
  §4.C, slice C1 (rulings §9 row 3). Catalog rows MA-371, MA-374, MA-375, MA-378.
- Base `26b7f81` (`fix/review-2026-10`); reviewed head `2423a74` (`wip/lc-c1`).
  Fixes are the commits listed below, on top of it.
- Lenses: outside aggressor (a remote page in the user's browser, hostile archived
  mail); insider (a co-account on the same computer, and one that can write the
  archive); integrity and concurrency (check-then-use, links, error paths).
- Procedure: `assurance-kit/process/adversarial-review.md` §3 and §4. Every finding
  was judged by a skeptic before any fix; the verdict column is that judgment.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| AO1 | outside | `OPTIONS *` is answered by net/http before `Wrap`: no Host check, no policy headers (`localweb.go:42-69`; the `http.Server` literals in `cmd/`) | DUPLICATE of AC8 | Reproduced over a real listener: `200`, no CSP, no-store, CORP, nosniff or no-referrer. Same mechanism as AC8, which is kept. | e41a6f0 (via AC8) | `TestOptionsStarReachesTheGuard` |
| AO2 | outside | Responses net/http writes itself (400, 408, 431) lack the policy headers (`localweb.go:171-201`) | DUPLICATE of AC8 | Reproduced: `GET /%zz` and `Host: a b` got a bare 400 with no policy headers. Folded into AC8. | open, see AC8 | `TestParseErrorAnswersCarryThePolicyHeaders` (skip encoder) |
| AO3 | outside | `/files/` does not pin the type of an `.eml`; a raw message that starts with HTML can be served as `text/html` on a host with no `.eml` mapping (`files.go:66-71`, `server.go:77`) | CONFIRMED (low) | Mechanism in the source (`fs.go` takes the type from the host's mime table, else sniffs 512 bytes); not run on a host without an `.eml` mapping (macOS maps it). Behaviour predates C1. Capped by `ArchiveCSP` and nosniff. | 6de5739 | `TestFilesPinsTheTypeOfAnEmlAndAZip` |
| AO4 | outside | The CC15 structural check scans only `cmd/` for servers and misses a dot-import (`structure_test.go:156`, `:82-94`) | DUPLICATE of AI1 | Dot-import bypass is AI1 (reproduced). The `cmd/`-only scope matches the design (§4.C, MA-378), so no change to it. | ed6cb8b (via AI1) | `TestStructuralChecksFailClosedOnHiddenHTTP` |
| AO5 | outside | `serve -addr 127.0.0.2:8099` prints a URL its own Host check refuses (`surfaces.go:41-47`, `localweb.go:76-86`, `main.go:362-374`) | CONFIRMED (low) | From the source (127.0.0.2 does not bind on the review host): `IsLoopback` accepts it so no warning, `servedURL` prints it, `allowed` refuses it, and the refusal names 127.0.0.1 where nothing listens. Fails closed; a misleading message. | f8e9dc0 | `TestLoopbackServeAcceptsTheAddressItPrints` |
| AO6 | outside | Windows: an 8.3 short name of `_outlook-pst` passes the name refusal (`files.go:57-59`) | DUPLICATE of AI3 | See AI3. | none needed | none |
| AI1 | insider | A dot-import of `net/http` bypasses both CC15 structural checks (`structure_test.go:82-94`, `:97-104`, `:156`, `:243`) | CONFIRMED (low, raised as medium) | Reproduced in a probe worktree: a `cmd/mailarchive` file with `import . "net/http"`, a mux and `Server{Handler: m}.Serve(ln)` passed both tests. A tripwire against accident; closing it takes a deliberate source change, and nothing remote or co-account reaches it. | ed6cb8b | `TestStructuralChecksFailClosedOnHiddenHTTP` |
| AI2 | insider | A planted `last-run.json` puts attacker text into `status`'s WARN (`health.go:231-235`, `lastrun.go:201-203`) | CONFIRMED (low) | From the source: a known code is accepted and its message printed verbatim after `StripControl` only. Same class as the `error` field the same file already prints in a RED reason. The design closes it with the private root (P1/P2, slice A). **Escalated, not fixed**: rebuilding the text drops the path P10's text names, and bounding it is a rule the design does not state. | bdf8804 (encoder only) | `TestStatusDoesNotEchoAPlantedWarningText` (skip encoder, MA-375, R12) |
| AI3 | insider | Windows 8.3 short names bypass the `_outlook-pst` refusal and the dotfile rule in `/files/` (`files.go:45-71`) | REFUTED | `_outlook-pst` holds only `.pst`, deleted after export; the tool's dot-files are temp and `.bak` names, none ending `.html`, `.eml` or `-attachments.zip`; the suffix rule is case-sensitive and an 8.3 alias of a long `.html` ends in `.HTM`; search.db, tokens, json and logs fail the suffix rule under any spelling. An alias exposes only a servable-suffix file someone planted, and an archive writer can plant one at an allowed location anyway. No Windows host; no impact path from the source. | none needed | none |
| AC1 | integrity | An intermediate-folder swap serves files P9 refuses by location (`files.go:127-145`, `:152-180`) | CONFIRMED (low, raised as medium) | Reproduced through the `probeEntry` seam: after the Lstat of `store/Inbox`, rename it aside and link it to `../_outlook-pst`, `..` or `.dotdir`; `/files/store/Inbox/stray.html` returned 200 with the planted page (a direct request was 404). Needs write access to the archive tree during the request (the co-account row until the root is private). `os.Root` still blocks escape and the file name is still judged, so search.db and tokens stay unreachable. | c6108ad | `TestFilesJudgesTheOpenedPathNotTheLstat` |
| AC2 | integrity | A FIFO swapped in after the check blocks the handler (`files.go:166-171`) | CONFIRMED (low) | Reproduced with the `afterCheck` seam and `Mkfifo`: no return within 3 s. Needs a writer of the archive tree and a narrow race. An improvement on `26b7f81`, whose `os.Open` blocked on a FIFO with no race. | c6108ad | `TestFilesNeverBlocksOnAFifoSwappedInAfterTheCheck` |
| AC3 | integrity | P10 containment compares spellings, not identity (`contain.go:18-58`) | CONFIRMED (low) | Reproduced on APFS: `Overlap(<NFC>, <NFD>/graph.token)` was false, true for the same spelling. Needs two spellings of one folder in one command, so accident only. | 80faa4f | `TestContainmentIsByFileIdentityNotSpelling` |
| AC4 | integrity | An interactive "Archive now" erases the `config_in_archive` warning while the condition persists (`capture.go:41-55`, `health.go:231-237`) | CONFIRMED (low) | Source: only `HeadlessCapture` calls `settingsInArchive`; `startCapture` records no warning; `status` reads the last run alone. A coverage gap, not a breach of MA-375, whose P10 surfaces are the Settings POST and `HeadlessCapture`. **Escalated, not fixed**: covering the interactive run (refuse or warn) is a decision. | 63fc130 (encoder only) | `TestArchiveNowKeepsTheConfigInArchiveWarning` (skip encoder, MA-375, R12) |
| AC5 | integrity | `graph -unattended` disables the interactive P10 refusal (`main.go:1082-1090`) | NON_GOAL | Design §4.C P10 (row and MA-375) rules it: interactive refuses, `-unattended` warns and the capture runs, so an installed backup is not silently stopped. | none needed | none |
| AC6 | integrity | Windows: 8.3 short names alias the folders P9 refuses by name (`files.go:45-61`) | DUPLICATE of AI3 | See AI3. | none needed | none |
| AC7 | integrity | `classify` fails open when `Readlink` fails on a `ModeIrregular` entry (`files.go:87-118`) | REFUTED | On Windows only a mount point among the non-symlink reparse tags is traversed by Win32, and `Root.Readlink` parses it deterministically, so `classify` refuses it. A tag `Readlink` cannot parse (a WSL LX link) is not traversed, so `Root.Open` reads the entry itself and reaches nothing outside; the handle must still match the Lstat entry and pass the suffix rule. The remainder is a speculative transient failure. | none needed | none |
| AC8 | integrity | net/http's own answers bypass the guard and lack the policy headers (`localweb.go:56-69`) | CONFIRMED (low) | Reproduced over a real listener: `OPTIONS *` got `200` with no policy headers; `GET /%zz` and `Host: a b` got a bare 400. A request that reaches a handler got the guard and all five headers. No data exposed; a browser cannot send `OPTIONS *`. **Fixed in part**: `DisableGeneralOptionsHandler: true` on all four servers puts `OPTIONS *` behind the guard. **Escalated**: the 400 answers net/http writes while parsing run before any handler and cannot carry the headers through `http.Server`; delivering them needs a listener-level design, or narrowing the "every response" wording to handler answers. | e41a6f0 (OPTIONS half) | `TestOptionsStarReachesTheGuard`, `TestEveryServedHandlerIsGuarded` (every `cmd/` server sets the option); `TestParseErrorAnswersCarryThePolicyHeaders` (skip encoder, MA-378, R19, R23) |

Follow-up commit `4755eb2` makes the AI1 tombstone visible to the no-vacuous-tests
gate (raw-string `func` lines at column 0 ended the test's body for the gate); no
gate was changed.

## Escalations (need an owner decision)

1. **AC8, parse-time answers.** net/http answers a request it cannot parse (bad path
   escape, malformed Host, oversized header, read timeout) before any handler, so
   the policy headers cannot be added there. Options: a listener-level wrapper that
   writes its own error response, or narrow §4.C1 and MA-378's "every response" to
   "every response a handler writes, `OPTIONS *` included". Encoder:
   `TestParseErrorAnswersCarryThePolicyHeaders`.
2. **AI2, planted warning text.** `status` echoes the recorded message of a known
   warning code. Rebuild it from the code and recomputed paths (loses the file path
   P10's text names), bound and validate it (a new rule), or leave it to the private
   root (P1/P2, slice A). Encoder: `TestStatusDoesNotEchoAPlantedWarningText`.
3. **AC4, interactive capture.** Cover the dashboard's "Archive now" with the same
   `config_in_archive` check, as a refusal or a warning. Encoder:
   `TestArchiveNowKeepsTheConfigInArchiveWarning`.

## Notes

- AO3 pins only the Content-Type. A `Content-Disposition: attachment` for `.eml` and
  `.zip`, which the finder also suggested, would change how the reader's links open
  and is not a decision this pass takes.
- AC1's fix narrows nothing: every request that was served is still served, and the
  location rules now hold for the path actually opened. P9's word "race-free" is now
  true for intermediate components too.
- AC3's fix only adds a comparison (file identity), so nothing judged inside before is
  now outside.
- AO4's other claim (servers started under `internal/` are not scanned) matches the
  design's `cmd/` scope for the server check; `internal/` is covered for muxes by the
  route-model test.
