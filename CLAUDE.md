# mail-archive-tool — agent instructions

A source-agnostic mail archiver (Outlook `.pst`/`.ost`, Thunderbird mbox/maildir)
that exports to self-contained HTML + attachment zips with a full-text search
index. Pure Go, no cgo. See `README.md` for the product and `docs/invariants.md`
for what must always hold.

<!-- assurance-kit-block v3 -->
## Testing discipline (non-negotiable)

- The assurance kit's Go block meta-test is vendored at assureblock/; the full
  methodology and process gates live in ../assurance-kit (read METHODOLOGY.md
  first). The test command is `go test ./...`; "an unfiltered run" means that
  command with no -run/package filters. assureblock/ is the wiring point: it
  holds the block meta-test and the covers-map meta-test.
- The invariants are numbered in docs/invariants.md; every test cites what it
  proves via a `// covers: INV-N[, INV-M]` marker on the test function. The
  covers-map meta-test (assureblock/) fails if any invariant has no test or any
  test omits a marker. The tests encode the invariant, not the other way around.
  NEVER weaken a gate, delete a covers marker, add a suppression, or edit a
  baseline to make a test pass. A red gate means the code is wrong until a human
  says otherwise; if you believe the gate itself is wrong, STOP and say so
  instead of routing around it.
- The covers baseline (docs/invariants.md is the catalog of record) is the count
  of record; counts do not live in prose. Never lower a gate to make it pass.
  Intentional reductions are accepted by the OPERATOR, never self-granted.
- Import the assure helpers (assure/); never hand-roll refusal checks. A refusal
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
