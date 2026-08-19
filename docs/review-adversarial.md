# Adversarial review — mail-archive-tool

Per assurance-kit `process/adversarial-review.md`. Mail content is fully
attacker-controlled (anyone can send you an email), so every reader/exporter
path treats it as untrusted input.

**8 found / 5 confirmed / 3 refuted.** Confirmed findings are fixed with a
tombstone test seen RED against pre-fix code (prove-fail recorded), then GREEN.
F8 was surfaced by an operator report (the friction gate) and folded in here.

## Lenses

- **L-out** — outside aggressor: crafts the email being archived (headers,
  filenames, MIME structure, HTML body).
- **L-in** — malformed/low-quality input: corrupt PST nodes, broken MIME,
  truncated mbox.
- **L-int** — integrity/state: dedup identity, atomic writes, the file server.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro / refutation) | Fix | Tombstone |
|---|---|---|---|---|---|---|
| F1 | L-in | A panic deep in the PST/MIME parser on one crafted message aborts the whole archive — `source/reader.go` walk / `source/mbox.go` had no per-message recovery | CONFIRMED | design as-written permits it: a single `panic` unwinds through `Walk` and kills the run. No current input reproduces a parser panic, so this is defensive hardening. | per-message `recover` in `safeConvertMessage`/`safeParseMessage` (stub, never dropped) and in `app.runFile`'s handler (skip+log) | MA-31 (robustness contract) |
| F2 | L-int | The fallback dedup identity omits the body — `model/message.go:63` — so two distinct no-Message-ID mails with identical subject/sender/recipient/second/attach-count hash equal and the second is dropped: **silent data loss** | CONFIRMED | prove-fail: `TestIdentityDistinguishesBodies` RED ("messages differing only in body must not share an identity") against pre-fix code, GREEN after | include `HTMLBody`/`PlainBody`/`RTFBody` in the identity hash | MA-37 |
| F3 | L-out | The search server serves attacker-authored email HTML at `/files/` with no CSP — `server/server.go:57` — so a `<script>` in an archived email executes in the `localhost` origin and can read other archived mail via `/api/search` + `/files/` and exfiltrate; remote `<img>` trackers also fire | CONFIRMED | prove-fail: `TestFileServerSetsCSP` RED ("served with no Content-Security-Policy") against pre-fix code, GREEN after | strict CSP (`default-src 'none'; img-src data:; style-src 'unsafe-inline'`) + `nosniff` on `/files/` | MA-38 |
| F4 | L-out | FTS injection / query crash via the search box | REFUTED | `index/query.go` `ftsMatch` quotes every term (`"term"`, internal `"`→`""`); operators are literal. `TestSearchTreatsOperatorsLiterally` runs 8 operator/quote inputs — none error | — (confirmed-safe) | MA-32 |
| F5 | L-out | Path traversal / zip-slip via attachment, folder, or store names (`../../x`, absolute, separators) | REFUTED | `util.SanitizeSegment`/`SanitizeFilename` strip `/\`, trim `..`/dots, prefix reserved device names; attachments only ever become sanitized zip entry names. `TestContainment` + `TestZipEntryNamesContained` assert no separator/`..` survives and no join escapes root | — (confirmed-safe) | MA-29, MA-30 |
| F6 | L-out | A crafted large attachment/message is buffered whole in memory (`export/attach.go` `bytes.Buffer`; `source/mbox.go` `io.ReadAll`) → memory exhaustion | CONFIRMED (acknowledged limit) | true by construction; bounded by the largest single item, not the mailbox | not fixed — recorded as an acknowledged limit in the catalog; a `-max-attachment` cap is a backlog item (Type III) | — |
| F7 | L-int | `/files/` path traversal (`GET /files/../../etc/passwd`) | REFUTED | Go's `http.FileServer(http.Dir(...))` cleans and rejects `..`; the export tree contains only tool-written `.html`/`.zip` (no attacker-authored symlinks) | — (stdlib guarantee) | — |
| F8 | operator | `Running()` counted any lock FILE's existence as "running" — `thunderbird.go` — but Mozilla's `.parentlock` persists after quit and a crash leaves a stale `lock` symlink, so the GUI's "please quit Thunderbird" dialog looped forever (reported on Linux/snap) | CONFIRMED | prove-fail: `TestRunningDetectsLock` stale-lock case RED against the file-existence check, GREEN after | detect by the `lock` symlink's PID liveness (+ Windows `parent.lock` open-probe); a stale lock reads as not-running | MA-35 |

## Disposition

- F1–F3 fixed and tombstoned; F2/F3 prove-fail recorded above (F1 is defensive,
  tombstoned by the MA-31 robustness contract — no input currently panics the
  parser, so there is no stronger reproducer).
- F4/F5/F7 refuted with evidence and left as standing confirmed-safe tests so a
  future regression reds loudly.
- F6 is an acknowledged design limit, not a defect; it is recorded in the
  catalog's "Acknowledged limits" note and the friction backlog.
