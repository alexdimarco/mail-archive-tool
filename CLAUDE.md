# mail-archive-tool — agent instructions

A source-agnostic mail archiver (Outlook `.pst`/`.ost`, Thunderbird mbox/maildir,
Evolution Maildir++/IMAP cache, and Microsoft 365 via Graph) that exports to
self-contained HTML + attachment zips with a full-text search index. Pure Go, no
cgo. It ships a CLI, a native-dialog wizard, and **MailArchive Desktop** — a local
browser dashboard with an archive manager (multiple named archives, pause/resume),
scheduling with built-in email alerts (Office 365 / Gmail / standard SMTP), a
backup run-history, source-loss surfacing, and program uninstall (archives are
always kept). See `README.md` for the product and `docs/scenario-catalog.md` for the
invariants (R-series) and test-specs (MA-series) that must always hold.

<!-- assurance-kit-block v3 -->
## Testing discipline (non-negotiable)

- The assurance kit's Go block meta-test is vendored at assureblock/; the full
  methodology and process gates live in ../assurance-kit (read METHODOLOGY.md
  first). The test command is `go test ./...`; "an unfiltered run" means that
  command with no -run/package filters. assureblock/ is the wiring point: it
  holds the block meta-test and the covers-map meta-test.
- The invariants (R-series) and test-specs (MA-series) are numbered in
  docs/scenario-catalog.md; every test cites what it proves via a
  `// covers: MA-N[, R-M]` marker on the test function. The
  covers-map meta-test (assureblock/) fails if any U/S test-spec (MA row) has no
  covering test, if any product test omits a marker, or if a marker cites an id
  absent from the catalog (R-invariants are covered through their MA rows). The tests encode the invariant, not the other way around.
  NEVER weaken a gate, delete a covers marker, add a suppression, or edit a
  baseline to make a test pass. A red gate means the code is wrong until a human
  says otherwise; if you believe the gate itself is wrong, STOP and say so
  instead of routing around it.
- The covers baseline (docs/scenario-catalog.md is the catalog of record) is the
  count of record; counts do not live in prose. Never lower a gate to make it pass.
  Intentional reductions are accepted by the OPERATOR, never self-granted.
- Import the assure helpers (internal/assure/); never hand-roll refusal checks. A refusal
  test asserts the exact typed exit/behaviour, that the message names the
  offending file/policy/remediation, and that the side effect is ABSENT
  (assure.Refused); prove the positive twin on a healthy fixture first.
  assure.Reached/assure.Refused treat empty/zero as failure; a test that asserts
  nothing FAILS. A legitimately assertion-free test declares itself with a
  `// no-assert: reason` marker.
- Real primitives, no mocks of core logic. Mocks are for tempdir paths, external
  binaries (to assert argv hygiene), and fault injection only.
- Every fix ships prove-fail -> prove-pass (mutate the fix, watch the covering
  test go red, revert), recorded in the commit. A confirmed-open invariant gap
  is a t.Skip/xfail encoder citing its invariant; when it starts passing, REMOVE
  the marker so it becomes a permanent guard.
- This repo has no lab tier; catalog rows needing real infrastructure (a live
  IMAP/Exchange server, a Windows Outlook install) are marked "pending" with the
  lab story stated.
- Process gates are defined in ../assurance-kit/process/ and METHODOLOGY.md:
  the adversarial pass for security-sensitive changes, the friction review for
  every shippable feature, the 10-lens pre-code design review for security- or
  architecture-relevant designs, and the UX contract (docs/ux-contract.md,
  X-series) enforced structurally. When your change owes one of these reviews,
  SAY WHICH ONE and stop at the gate; do not self-certify or skip.
<!-- /assurance-kit-block -->

## Running unattended: avoiding permission prompts in bypass mode

Even with bypass-permissions on, the `blockReadsOutsideWorkingDirectories` guard
(a user-scope setting) still prompts for any command the shell parser cannot
statically analyse, and for reads outside the working directory. For an autonomous
run to stay prompt-free:

- Use the dedicated file tools (Read, Write, Edit, Grep, Glob) for file work, not
  shell `cat`/`sed`/`awk`.
- Never run code on stdin: no here-documents, no `python3 - <<EOF …`, no
  `bash -c '<blob>'`. The parser cannot analyse these, so they ALWAYS prompt.
  Write the script to a file in the repo and run it by path (`python3
  scripts/x.py`).
- Keep each Bash command one simple, analysable invocation; avoid long
  `&&`/`;`/pipe chains and `cd … &&` prefixes (use absolute paths instead).
- Pin the toolchain once with `go env -w GOTOOLCHAIN=go1.26.0` so a plain
  `go test ./...` uses the CI toolchain without a `GOTOOLCHAIN=…` env prefix.
- Keep temporary files inside a gitignored path in the repo, not /tmp or an
  out-of-tree scratch dir, so reads stay inside the working directory.
- Subagents/workflows inherit the session mode but multiply any prompt that does
  slip through; prefer the main loop with clean single commands for work in this
  repo.
