# FI-S3 adversarial pass: Graph isolation, network input and the go-back sweep

**25 found / 16 confirmed / 1 refuted** (5 duplicates and 3 non-goals counted separately).
**Verdict: FINDINGS OPEN.** Of the 16 confirmed findings, 14 are fixed, each with a tombstone
test seen red against the code before its fix (or with the fix mutated out) and green after;
S3-AI1 is fixed for the run whose ids are not confirmed, and S3-AI3 for its parts (a) and (b)
(the rest follows the design). Two are open and one variant is open, because each remedy needs
an owner decision and each has a skip encoder that was run unskipped to show it red:
**S3-AC2** (a breaker trip manufactures its own known-poison list; ESC-S3-2), **S3-AI7**
(the stub hold-back of an item whose own immutable id matches no archived record, with
S3-AO5 part b; ESC-S3-3) and the id-confirmed variant of **S3-AI1** (ESC-S3-1). A fresh-eyes
check of the fixes (a second reviewer, who did not write them) found three rows overstated
(S3-AO2, S3-AI3 part b, S3-AC5); each was reproduced and fixed again in a second round, with
a new tombstone seen red first (see "Re-verification"). The other fixes passed the check.

| | |
|---|---|
| Reviewed commit | `039acaa` (branch `wip/fi-s3-adv`), base `5b1d78b` |
| Fixes | `ae7f17d`..`e65fa86` on the same branch (12 fix commits, 2 test-only commits), then the second round `b8d94a4` (S3-AO2), `c647c37` (S3-AI3 b), `d9c55a4` (S3-AC5) and this report's update; not pushed |
| Reviewed file blobs at `039acaa` | `internal/app/graph.go` e94305e2f87d, `graphfaults.go` 6c2626901be1, `graphbreaker.go` a339cdff0157, `internal/state/sweep.go` 86582c1f9395, `gaps.go` 3aad3e34b4dc, `internal/graph/graph.go` 59d59f617318, `graphtest/server.go` b24320ed57ac, `graphtest/faults.go` bace5b6e0a12, `internal/health/gaps.go` be0485b9be5d, `internal/export/exporter.go` b248bc003e8e |
| Finders and skeptic | three finder agents (lenses AO, AI, AC) and one separate skeptic agent, run by the workflow; the fixer (this session) did not find any of the issues |
| Tool versions | Go 1.26.9 (scratchpad toolchain), run through `inhome` so no real settings, sign-ins or schedules are read |
| Baseline digest | none: this repository keeps no covers baseline file; `assureblock` reads `docs/scenario-catalog.md` and the tests' `// covers:` markers |
| Final commands and exit statuses | see "Final verification" at the end |

## FI-S3 adversarial pass

**Scope.** The FI-S3 merge (Graph isolation): `git log --oneline 5b1d78b..HEAD -- internal/app
internal/graph internal/state internal/health` and `git diff c4952b5~1..f5ede35`, reviewed at
`039acaa`. Main files: `internal/app/graph.go` (RunGraph: classification, per-item and
per-folder gaps, stub hold-back, legacy adoption, the -since rule), `graphfaults.go`
(classifyFetch, gap recording), `graphbreaker.go` (rate/time breaker, known-poison exemption,
control probe, run-level breaker), `internal/state/sweep.go` and `gaps.go` (the go-back sweep
rules FC4 and OR10), `internal/graph/graph.go` (Retry-After clamp, probe, caps) and the fake
server in `internal/graph/graphtest`. Design: `docs/design-fault-isolation.md` section 4.8 and
the S3 row of section 10; rulings 13.22; rulings row 14 (this pass) and row 4 (the earlier
network-input review of SG).

**Threat model.** The shipped graph capture path reads the owner's own Microsoft 365 mailbox
and deletes nothing in it. The attacker is a misbehaving, impersonated or compromised Graph
endpoint or proxy and hostile mailbox content (items that error, hang, trickle or lie; pages
that repeat, loop or point at another host; 403/404/410/429/5xx patterns; Retry-After values;
a response that makes a present message look gone; a fault pattern that makes the breaker
mis-trip or never trip). Standing rulings: R10 and the ledger design, J4 and 13.4 (RR-6),
OR7, OR8, OR10, ESC-7 (a foreign-host nextLink is a known open gap, not a finding), ESC-1
and FI-S2b, K2 and ESC-S2A-1. Same-account malware and administrators are out of scope.

**Lenses.** AO: outside aggressor (a hostile or impersonated Graph and hostile mailbox
content). AI: insider (stale and planted archive state, mixed versions and sweep integrity).
AC: integrity and concurrency (breakers, caps, cancellation and crash points).

**Method of this round.** For each confirmed finding: the regression test was written first,
run against the unfixed code and seen red for the stated reason, then the fix, then green.
Where a test could not be run against pre-fix code (a compile break from a new signature, or
a fix applied before its test), the fix was mutated out again and the test seen red; each
commit message records the red line. The skeptic's UNJUDGED verdict on S3-AO9 was settled by
reproduction (three 601-byte ids left one ledger entry, red), so it counts as confirmed.

## Findings

Severities are the skeptic's, which may be lower than the finder's. "Fix commit" is on
`wip/fi-s3-adv`. A test named in italics is a skip encoder.

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| S3-AO1 | AO | A 200 listing with no `value` array (`{}`, `null`, an error object) reads as an empty folder and sweeps its records gone; a `{}` folder list walks nothing and records ok. `internal/graph/graph.go` Messages (decode, ~634-700) and Folders (~397-428); consumed at `internal/app/graph.go:783-806` | CONFIRMED (low) | Reproduced by the skeptic and again by the fixer (red: run 2 returned nil; folder list `{}`/`null`/`{"value":[]}` did not fail the mailbox). An honest `{"value":[]}` cannot be told from an empty folder and still sweeps (documented in the test's twin); a lying well-known-folder resolution is the same indistinguishable class, not addressed | `b4696f2` | `TestGraphMalformedListingIsNeverAnEmptyFolder` (MA-316) |
| S3-AO2 | AO | A 200 `$value` that is not a message (HTML, empty) is archived as one headerless record, no gap, clean exit. `internal/app/graph.go:748-766`; `internal/graph/graph.go` MIMETo | CONFIRMED (low) | Reproduced (three items collapsed into one `sha:` record; red: run returned nil). Fix: the body must open with an RFC 5322 header field, else a fillable fetch-failed gap. Well-formed junk from a determined impersonator is out of its reach. Second round: the first test let a one-line JSON error, "Error: sign in required" and minified HTML through (a name of any printable characters before a colon); the check now needs a field name of letters, digits, hyphen and underscore and at least one of From, Date, Message-ID, Subject, To, Received, MIME-Version, Content-Type, Return-Path, Sender in the header block | `5c9af35`, `b8d94a4` | `TestGraphValueThatIsNotAMessageIsAGapNotARecord`, `TestGraphOneLineErrorAnswerIsNotAMessage` (MA-310) |
| S3-AO3 | AO | A close-delimited (HTTP/1.x, no Content-Length, not chunked) `$value` ends at the first clean FIN and a truncated MIME is accepted as complete and terminal. `internal/graph/graph.go` MIMETo (~846-868), `internal/app/graph.go:129` | CONFIRMED (medium) | Reproduced (a hijacked 200 with 30 bytes, then close: err nil, exported 1; red). Fix refuses the answer as an unexpected EOF (transport class); gzip answers (`resp.Uncompressed`) and HTTP/2 are not refused. A body that frames a short length correctly stays undetectable. Trade-off: a genuine HTTP/1.0-style proxy in the path now makes every message a fetch-transport gap (loud, not silent) | `0b1f73e` | `TestGraphCloseDelimitedBodyIsATransportFault` (MA-314) |
| S3-AO4 | AO | OR10's spare rule misses PhysID-bearing records: phantom gone while the gap says `defers=true`. `graphfaults.go:223-250` | DUPLICATE of S3-AI1 | Same mechanism; fixed under S3-AI1 | `569444f` | `TestGraphUnconfirmedIDsFaultSparesRecordsThatHoldAnImmutableID` |
| S3-AO5 | AO | Message-ID membership closes a failed item's gap (a), and the stub hold-back hides a parse-failing distinct message under an info gap (b). `graph.go:732`; `graphfaults.go:113-117` | DUPLICATE of S3-AI7 (b); (a) is the acknowledged floor residual #8 | (b) is S3-AI7, open as ESC-S3-3. (a): the membership skip is documented in the code as the bounded #8 residual, and before S3 the item was skipped with no gap at all, so closing a stale gap there is consistent with the rule, not a new defect | none (see S3-AI7) | `TestGraphHeldBackStubOfAnUnprovenItemIsNotQuietlyDropped` (skip) |
| S3-AO6 | AO | A Retry-After whose seconds overflow Duration (9223372037) wraps negative and defeats the clamp: six requests with no wait. `graph.go:1075-1082`, `987-998` | CONFIRMED (low) | Reproduced (6 requests in 67 ms against 1.58 s for Retry-After 100; red). Fix caps the seconds at a year before the multiplication | `ae7f17d` | `TestGraphRetryAfterOverflowIsClamped` (MA-313) |
| S3-AO7 | AO | Hostile error-body text reaches the terminal and run log with ESC and bidi characters (fatal 401/403 paths build raw text). `graph.go:361-389`; `graphfaults.go:292`, `329-333`; `cmd/mailarchive/main.go:1238-1241` | CONFIRMED (low) | Reproduced (red for `$value` 403, `$value` 401 and a listing 401). Fix: the fatal error is cleaned where it is built (`cleanErr`, chain kept), as S2A's are | `d733cf4` | `TestGraphFatalErrorTextIsCleanedWhereBuilt` (MA-313) |
| S3-AO8 | AO | No wall-clock bound on a run: a healthy control probe forgives every counted fault, and known poison is exempt even in a full outage. `graphbreaker.go:57-93`, `126-129`; `graphfaults.go:295-298` | NON_GOAL | Reproduced, but it is the documented open condition: design FC10 (lines 199-213) says known poison never counts and pays its timer every run, that with a healthy control request nothing trips and the bound is per item, and that this is "an open condition, not accepted"; a probe-immune time budget would reopen the hostile-sender wedge and a ledger-driven limit would narrow P5 (ruling J7) | none | none |
| S3-AO9 | AO | The gap key is built from the raw id but stored sanitized and cut at 512 bytes: long or odd ids collapse into one gap, and recovery and known-poison lookups miss. `graphfaults.go:80`, `273-277`; `graphbreaker.go:126-129`; `state/gaps.go:164-173` | CONFIRMED (low; skeptic UNJUDGED, reproduced by the fixer) | Red: three 601-byte ids left 1 ledger entry and "1 item(s) not captured"; the healed run closed it as gone, not recovered. Fix: `state.BoundLocator` (clean text kept; any other id gets its cleaned, shortened form plus a digest), used for item, folder and exporter locators | `049cee6` | `TestGraphLongItemIDsKeepTheirOwnGaps` (MA-311, MA-323); `TestBoundLocatorKeysEveryRawLocatorToItsOwnEntry` |
| S3-AI1 | AI | OR10 hole: a present record that carries an immutable id is swept gone when the run's ids are unconfirmed and its download fails; the gap says `defers=true`. `graphfaults.go:223-250`; `state/sweep.go:32-42`; `graph.go:803-806` | CONFIRMED (low) | Reproduced; red with the fix mutated out (N: Present false, gone event true). Fixed for the id-less run (the unmatched fault spares every record: `SweepSpare.AllRecords`). **Open:** the same hole with ids confirmed (a message restored under a new immutable id, Message-IDs mismatched, download failing) is not closed: the only rule that closes it spares every record on every unmatched fault, which makes each new poison item defer the whole mailbox's gone-detection and contradicts OR10's text (PhysID-less records only). Escalated as ESC-S3-1 | `569444f` (id-less run) | `TestGraphUnconfirmedIDsFaultSparesRecordsThatHoldAnImmutableID` (MA-312 case h); *`TestGraphReissuedIDFaultSparesTheArchivedRecord`* (skip) |
| S3-AI2 | AI | A `-since` run silently closes the folder's overflow entry, so items the ledger could not list leave the ledger and status. `state/gaps.go:439-463`; `graph.go:811-813` | CONFIRMED (low) | Reproduced (1010 failing items: after the -since run the ledger counted 1000, overflow entries 0; red). Fix: a -since run keeps every overflow entry | `287e321` | `TestGraphSinceRunKeepsTheOverflowEntry` (MA-323) |
| S3-AI3 | AI | Deferred gone-detection is unannounced for terminal gaps, folder-unwritable gaps and young fillable gaps, and the note understates its scope. `health/gaps.go:239-255`, `277-286`; `graph.go:742-745`; `graphfaults.go:407-415`; `state/gaps.go:507-516` | CONFIRMED (low) for (a) and (b) only | (a) the terminal status line now carries the deletion-tracking sentence; (b) the folder-unwritable gap now records `DefersGone` when any message it counted deferred the sweep, and `ObserveGap` keeps a true over a later false in one run (a first version of that merge was caught by the test's second case). (c), and the young fillable case, follow the design (the stuck line names the gap's own folder; only a persisting gap is named) and are unchanged. Second round: (b) made a stuck folder-unwritable gap carry the item sentence ("move or delete the item out of B in the mailbox"), wrong for a local directory fault; a folder-scope gap now says deletion tracking resumes once the archive location is writable and a run reaches the folder again | `70fa50f`, `c647c37` | `TestStatusTerminalGapThatDefersGoneNamesIt` (MA-326); `TestGraphUnwritableFolderGapRecordsThatItDefersGoneDetection` (MA-501, MA-312); `TestStatusStuckFolderUnwritableGapDoesNotSendTheOwnerToMoveMail` (MA-326) |
| S3-AI4 | AI | Full mode with `-since`: archived messages older than the window are downloaded, skipped by date, never stamped, and swept gone. `export/exporter.go:286-340`; `graph.go:803-806` | CONFIRMED (medium) | Reproduced (red: both records Present false with gone events, and a move not followed). Pre-existing exporter behaviour, not introduced by S3. Fix: one `observeArchived` helper stamps and follows a move before the date skip (Graph path only) | `9f65d0d` | `TestGraphFullModeSinceRunStampsArchivedMessagesOlderThanTheWindow` (MA-312) |
| S3-AI5 | AI | The stuck line for vanished-at-fetch tells the owner the message was deleted and the row closes next run, for an item that is still listed and fails every run. `state/gapremedy.go:63-64`, `123-124`, `304-314`; `graphfaults.go:54-55` | CONFIRMED (low) | Reproduced in the status line and the gap list (red). Fix: a stuck Graph vanished-at-fetch gap says the mailbox still lists the message and how to save a copy (`GapStuckWhy`, a stuck remedy). The run summary and the report TSV have no stuck notion and keep the generic words (not changed) | `70fa50f` | `TestStatusStuckVanishedAtFetchDoesNotPromiseTheRowCloses` (MA-326) |
| S3-AI6 | AI | A hostile or long item id merges distinct gaps; recovery and known-poison lookups use a different key than the stored one. `state/gaps.go:164-189`, `239-288`; `graphfaults.go:80`, `271-277`; `graphbreaker.go:126-129` | DUPLICATE of S3-AO9 | Same finding, reproduced under S3-AO9 | `049cee6` | as S3-AO9 |
| S3-AI7 | AI | The stub hold-back drops a distinct message that reuses an archived Message-ID, behind "nothing is lost". `graphfaults.go:109-118`; `state/gapremedy.go:125-126` | CONFIRMED (low) | Reproduced (B1: exported 0, kept 1, err nil, one info gap, no record). Not fixed: writing the stub as an `#fp` split, or a fillable gap with its own reason and words, changes P15's hold-back and the section 4.1 reason table, and keeping the info row with corrected words cannot be told apart from the benign refresh. Escalated as ESC-S3-3 | none | *`TestGraphHeldBackStubOfAnUnprovenItemIsNotQuietlyDropped`* (skip) |
| S3-AI8 | AI | Legacy-stub adoption can relabel a real archived message as a legacy placeholder, which the sweep then spares for ever. `state/sweep.go:115-139`; `graphfaults.go:119-121` | CONFIRMED (low) | Reproduced (red: the look-alike became Terminal `[legacy-placeholder]`). Fix: the record's identity must be the stub identity of the message's received time | `641d6be` | `TestGraphAdoptionLeavesARealMessageThatLooksLikeAStub` (MA-323) |
| S3-AI9 | AI | The gap Detail stores the failing request URL verbatim, so the nextLink query reaches the ledger and the report. `graphfaults.go:335-336`, `306`; `report.go:162-165` | REFUTED | The mechanism is real but no exposure follows: the bearer token travels in the Authorization header, never the URL; a nextLink carries only paging cursors and the endpoint's own text; Detail already carries the server's error body (bounded to 300 bytes, control-stripped); listing errors were returned raw at `5b1d78b`; the "no URL query in any text" rule cited does not exist for ledger or report text | none | none |
| S3-AI10 | AI | Gaps for items in a folder that no longer exists are never closed. `graphfaults.go:345-356`; `state/gaps.go:439-463` | NON_GOAL | Reproduced, but documented: design line 1984 ("a row of a token or folder that is no longer walked shows as not attempted, never as stuck", FC14) and MA-323; closing on a folder's absence from the listing would let a hostile childFolders answer hide gaps (R10) | none | none |
| S3-AI11 | AI | `AdoptLegacyGraphStubs` scans the whole manifest for every new unparseable message with a Message-ID. `state/sweep.go:115-139` | CONFIRMED (low) | Confirmed from source (an O(archive) scan under the manifest lock per stub; the finder measured 8.3 ms at 200,000 records). Fix: the candidates come from the identity index, one lookup per stub. Shown red by mutating the lookup back into a scan (3.9 s for 500 adoptions over 100,000 records against a 500 ms bound) | `641d6be` | `TestAdoptLegacyGraphStubsMatchesTheStubIdentityThroughTheIndex` (MA-323) |
| S3-AC1 | AC | Slow item-class faults are invisible to every breaker bound, so the walk can run unbounded with the control request down. `graphfaults.go:294-298`; `graphbreaker.go:57-87` | NON_GOAL | Reproduced, but it is FC10's documented open condition (a fault in `$value` alone while listings answer does not trip; each item pays its timer; ruling J7 rejects a probe-immune time budget) | none | none |
| S3-AC2 | AC | A breaker trip manufactures its own known-poison list, so each outage run costs more than the last. `graphfaults.go:295-297`; `graphbreaker.go:126-129`; `state/gaps.go:282` | CONFIRMED (low) | Reproduced (fetches before the trip, run by run: 25, 50, 75, 100). Not fixed: treating a gap recorded in a run whose mailbox tripped as not-known-poison changes FC10's rule as ruled ("known poison never counts, whatever the control request returns", RR6-C4) and needs a persisted mark in the ledger. Escalated as ESC-S3-2 | none | *`TestGraphOutageDoesNotRatchetTheKnownPoisonList`* (skip) |
| S3-AC3 | AC | The Retry-After clamp is bypassed by a value that overflows Duration. `graph.go:1075-1082`, `987-998` | DUPLICATE of S3-AO6 | Same defect, reproduced under S3-AO6 | `ae7f17d` | as S3-AO6 |
| S3-AC4 | AC | Gaps of a folder the mailbox no longer has are never closed and keep consuming the lifetime caps. `state/gaps.go:429-463`; `graphfaults.go:354-356` | DUPLICATE of S3-AI10 | Same behaviour, documented (FC14, MA-323); the cap erosion needs 1000 poison items in a deleted folder, and the entries show as not attempted | none | none |
| S3-AC5 | AC | The breaker's refusal text carries the archive path and the server's words, which `status` reads as an authentication failure. `graphbreaker.go:105-108`; `health/health.go:355`, `896-904` | CONFIRMED (low) | Reproduced (red for a path ending `backup-2401` and for a 504 body naming `invalid_client`: `auth_remedy` printed for an outage). Fix: `looksLikeAuthFailure` reads the error without those two stretches; the pinned wording of the refusal is unchanged; a refused sign-in is still an auth failure (twin). Second round: cutting delimited stretches is forgeable (a 504 body carrying the closing words ended the cut early; a mailbox named `it401` or `authentication-team` was scanned). The run's failure is now recorded by kind (`error_kind: service_unavailable`, set when the first mailbox error is the breaker's refusal) and `status` does not read that text at all; the cutting code is removed | `2ae1243`, `d9c55a4` | `TestStatusReadsABreakerTripAsAnOutageNotAnAuthFailure`, `TestStatusBreakerTripIsAnOutageWhateverItsTextSays` (MA-313) |

## Known items this round also closes

| ID | Item | Outcome | Fix commit | Test |
|---|---|---|---|---|
| K1 | MA-314 `fetch-too-large` gap case and MA-540 case 4: skip encoders, because capture passes no size cap (RR6-OR1, ESC-8) | Still skip encoders, still citing their invariants; nothing to build until a documented figure bounds a stored item | none | *`TestOverCapBodyIsAFetchTooLargeGap`* (`internal/graph/download_test.go:436`); the MA-540 case (4) encoder (`internal/app/graph_sg_test.go:236`) |
| K2 | MA-314's capture foreign-host case waits for FI-SGc / ESC-7 | Still a skip encoder ("an open gap, not accepted") | none | the foreign-nextLink encoder (`internal/graph/listing_test.go:198`) |
| K3 | MA-323's parser-generation re-fetch and MA-318's raw-preserving stub are FI-S2b's | Still skip encoders | none | `TestStaleParserGenStubRefetchedOnce` (`internal/state/gaps_test.go:596`); the MA-318 raw-stub case (`internal/app/localfault_test.go:113`) |
| K4 | Two no-Message-ID stubs with the same Received time still collapse (ESC-1) | Still a skip encoder citing R1 and ruling J8 | none | the ESC-1 encoder (`internal/app/localfault_test.go:189`) |
| K5 | MA-541's breadth case | Still a skip encoder citing R10 ("no total-folder cap", RR6-OR2) | none | the MA-541 breadth encoder (`internal/graph/listing_test.go:417`) |
| K6 | The S3 build changed the MA-540 stop tests and the transport-cancel test to assert the gap behaviour. Do they still assert everything the originals did, except the sanctioned change? | **CONFIRMED, with one gap closed.** Compared with `git show 5b1d78b:internal/app/graph_sg_test.go` (and `graph_review_test.go`, `graph/download_test.go`, `graph/listing_test.go`): every original assertion is kept except the typed-error checks on the stop (`errors.As` to `*ListingError` / `*TimeoutError`, Kind, Limit, MessageID), which became substring checks on the gap's detail (the words naming the stop, the message and the limit): the sanctioned change. The primitive-level proof that those stops match no context error stays in `internal/graph` (`download_test.go:67`). Not kept, because it cannot be: "zero `{k,gone}`, every record still Present" used to hold because the mailbox returned before its sweep; it now holds because a folder whose listing stopped defers the sweep, and the adapted case does not exercise that (nothing moves in its fixture; shown by mutation: with `w.folderFault` removed the adapted tests stay green). The new test moves a message from Archive into the Inbox that stops and goes red under that mutation | `e65fa86` | `TestRunGraphListingStopDefersTheSweepOfAMovedMessage` (MA-540) |
| K7 | Real-tenant behaviour is proven only against the fake server | Unchanged: the design says so; this round adds no real-tenant evidence | none | none |

## Escalations (each needs an owner decision; each has a skip encoder)

| Key | Question | Recommended | Safe interim |
|---|---|---|---|
| ESC-S3-1 (S3-AI1, id-confirmed variant) | A message restored under a new immutable id, listing and parsed Message-IDs mismatched, download failing, has its archived record swept gone (reversible: the next run that lists it records it present). Closing it spares every record on every unmatched fault, which in an id-confirmed run makes each new poison item defer the whole mailbox's gone-detection. Accept the phantom gone, or widen OR10? | Accept for now (the trigger is a restore plus a Message-ID mismatch plus a failing download), and state it as a known limit | OR10 as ruled (PhysID-less records spared); the id-less variant is fixed |
| ESC-S3-2 (S3-AC2) | Should a gap recorded in a run whose mailbox the breaker ended count as known poison in the next run? Today it does, so each outage run fetches 25 more items before it trips. The remedy changes FC10's rule as ruled and adds a persisted ledger mark | Yes: mark the gaps a trip leaves as the service's evidence, additive field, cleared when a later run observes the item without a trip | FC10 as ruled (the documented open condition, P3) |
| ESC-S3-3 (S3-AI7, S3-AO5 b) | A parse-failing item whose Message-ID matches an archived record but whose own immutable id matches none is held back with an info row that says nothing is lost. Write it as an `#fp`-split record, record a fillable gap with words of its own, or keep the info row and correct only its wording? | A fillable gap with its own reason and words (the item is then visible as not captured, and the benign Full-mode refresh keeps its info row because its immutable id matches) | P15's hold-back as built; the info row is visible in the report |

## Fixer's re-verification notes (a fix can introduce its own finding)

- **S3-AO3** refuses a close-delimited answer for every consumer of `MIMETo`, the copy export
  included. The cost is that an HTTP/1.0-style proxy in the path turns every message into a
  fetch-transport gap: loud and counted by the breaker, never silent. gzip and HTTP/2 answers
  are not refused (the twin passes). The whole app suite, the CLI tests and the copy export
  tests stayed green.
- **S3-AO2** (as first fixed) accepted any first line of the form `Name: ...`, so a one-line
  JSON error or "Error: ..." was still taken for a message; the second round closed that (see
  "Re-verification"). What remains: an answer that imitates a header block (a first line
  `From: x` or `Subject: x`) is indistinguishable from a message and is archived as one, and a
  real message whose header block holds none of the ten fields in `messageFields` would become
  a fillable fetch-failed gap (loud, retried every run; no Graph-converted message lacks all of
  them in practice).
- **S3-AO1**'s empty-folder-list error cannot fire on a real mailbox (an Inbox always exists).
- **S3-AO9** changes a locator only when the ledger would have changed it, so every existing
  entry keeps its key; the exporter's refresh-failed locator takes the same function.
- **S3-AI1** can only defer more, never sweep more. In a mailbox that was once id-confirmed and
  now is not, one unmatched fault defers the whole sweep until it is gone.
- **S3-AI4** stamps only on the Graph path (`DedupMailboxWide`) and only for a record that
  exists; a local import is unchanged.
- **S3-AI8** needs the stub identity to equal the hash of the placeholder subject and the
  message's exact received time. Graph's `receivedDateTime` has no fractional seconds, so a
  genuine legacy stub is still adopted (the walk test adopts one in the same second as the
  look-alike); a timestamp with a fraction would leave an old stub un-adopted and swept like any
  record whose message left.
- **S3-AC5** (as first fixed) coupled `health` to two delimiter strings of the breaker's
  wording, and the cut was forgeable; the second round replaced it with a typed kind
  (`service_unavailable`) that the app records and `status` reads. What remains: a run whose
  first mailbox error is not a trip but whose later mailboxes tripped (the run-level stop)
  is read from its text as any other failure is, and that text names the mailboxes, so a
  mailbox named like an auth needle can still print the auth note there (the owner's own
  configuration, not wire text; the server's words are not in that text).

## Re-verification

A second reviewer (who did not write the fixes or this report) checked the fixes at `8943815`:
reverted each fix with its tombstone kept, mutated where the reverse patch no longer applied,
and probed the shipped code. The outcome per item, and what this round did about each:

| Item | Checker's result | Outcome |
|---|---|---|
| S3-AO6 (S3-AC3) | Pass: red for 9223372037 and MaxInt64 when reverted | Stands |
| S3-AO7 | Pass: red for `$value` 403, 401 and listing 401 with `cleanErr` mutated to identity; no other path carries wire text | Stands |
| S3-AO3 | Pass: red when reverted; gzip twin and HTTP/2 pass | Stands |
| S3-AO1 | Pass: all six malformed subtests red when reverted | Stands |
| S3-AO2 | **Fail**: closed for spaced HTML, a doctype and an empty body; a 200 `$value` of a one-line JSON error, `Error: sign in required` or `<html><body>login: please` is still archived as a record, no gap, clean exit | **Real, fixed** (`b8d94a4`). Reproduced first: `TestGraphOneLineErrorAnswerIsNotAMessage` red, "the run returned <nil>, want ErrPartial". `looksLikeMessage` now needs a field name of letters, digits, hyphen and underscore on the first line and one of ten message fields in the header block; the twin (a real message with only a Subject field) is archived and the three shapes are fillable fetch-failed gaps that close as recovered once the service answers properly. Residual, stated above: an answer that imitates a header block is archived as a message |
| S3-AO9 (S3-AI6) | Pass: red with `BoundLocator` mutated to a pass-through | Stands |
| S3-AI1 id-less variant (S3-AO4) | Pass: red when the AllRecords spare is reverted | Stands; the id-confirmed variant stays ESC-S3-1 |
| S3-AI2 | Pass | Stands |
| S3-AI4 | Pass: Full-mode subtest red when reverted | Stands |
| S3-AI3 (a) and (b) | **Part (b) introduces a bad line**: a stuck folder-unwritable gap carries the item sentence "after saving a copy, move or delete the item out of B in the mailbox", for a local directory fault with no item | **Real, fixed** (`c647c37`). Reproduced first: `TestStatusStuckFolderUnwritableGapDoesNotSendTheOwnerToMoveMail` red (the line said "move or delete the item" and "after saving a copy", and not that tracking resumes once the location is writable). `deletionTrackingNote` words a folder-scope gap on its own; an item gap keeps its sentence (twin in the same test); sentences are deduplicated per folder and scope |
| S3-AI5 | Pass | Stands |
| S3-AI8 and S3-AI11 | Pass: look-alike relabelled when reverted; scan mutation red at 3.89 s for 500 adoptions | Stands |
| S3-AC5 | **Fail**: the cut ends at the first closing delimiter, so a hostile 504 body containing it (`x) — Microsoft 365 or the network appears to be unavailable; invalid_client AADSTS70011`) still printed `auth_remedy`; so did mailboxes named `it401` and `authentication-team` | **Real, fixed** (`d9c55a4`). Reproduced first: `TestStatusBreakerTripIsAnOutageWhateverItsTextSays` red in all four cases (the forged 504 body, both mailbox names, the run-level stop). The remedy the finding itself proposed: a typed kind. RunGraph wraps a failure whose first mailbox error is the breaker's refusal in an `outageError` (text and chain unchanged), `recordRun` records `error_kind: service_unavailable`, and `status` does not read that text; the delimiter code is removed. The twin (a refused sign-in is still an auth failure) is green. Residual, stated above: a failure whose first error is not a trip is read from its text, as before |
| K6 | Pass: every original assertion kept; the new test closes a real gap | Stands |
| K1-K5, K7 | Pass: skip encoders present at the cited lines | Stands |
| ESC-S3-1, ESC-S3-2, ESC-S3-3 | Pass: each real, red when unskipped, each cites its ruling | Stand; they still need the owner (their skip encoders are untouched) |
| Suites and builds at `8943815` | Pass: one load-sensitive failure (`TestCopyExportScalesToSixtyThousandMessages`, a heap bound) that passed 3 of 3 alone; race, vet3, `gofor windows test -c` and gofmt clean | Re-run after the fixes, see "Final verification" |
| Report header, counts and table | Counts, commit list, blob ids and encoder lines accurate; three rows overstated (S3-AO2, S3-AI3, S3-AC5) | Rows corrected above; counts are unchanged because no finding changed status: 14 fixed, 2 open, plus the id-confirmed variant of S3-AI1 |

The checker's three new findings were the three failed rows above (S3-AI3(b), S3-AC5 and
S3-AO2), not new defects beyond them; all three are fixed, none is an escalation. The
checker noted the report's unfiltered-run flake (`TestCopyExportTransportOutageAndTheBreaker`)
differed from the one it saw (`TestCopyExportScalesToSixtyThousandMessages`); both are
copy-export tests sensitive to load, both passed alone, and this round's unfiltered run
failed on the same two (see below).

## What held (reported by the finders; not re-run by the fixer except where a fix touched it)

Item-level 403 is loud and ends the mailbox (OR7 stands; whether a per-item 403 should be told
from a revoked grant by the control probe is left to the owner as a ruling question). Truncation
by Content-Length and by a missing chunk terminator is an unexpected EOF and a fetch-transport
gap; redirects off the host are refused. Repeat, endless and over-cap nextLinks end the folder
with a typed listing stop, a folder-list-failed gap and a deferred sweep, and a partly consumed
folder is never marked walked. Gap Subject, Folder, Locator and Detail are control- and
bidi-stripped and bounded; the breaker text goes through cleanText; the last-run record is
sanitized on write; no token reaches the ledger. Item-class faults never count toward the
breaker, the rate and consecutive bounds trip under 1-in-10 success, a failed probe ends the
mailbox with no sweep, and two trips in a row stop the run. A listing that omits a folder, or a
folder that disappears and reappears, leaves its records Present. A hostile receivedDateTime
hides an item from a `-since` window with or without a fetch failure (equivalent to any lie in
the listing). A single unmatched poison item defers gone-detection for all PhysID-less records
of the token (OR10 as ruled). The manifest version is unchanged and an older binary drops
`defers_gone` and this build recomputes it; nothing but the status wording reads the stored
flag; a planted gap does not stop a capture; a failed mailbox leaves no half-written sweep;
cancel, transport-cancel and an interrupt after the walk are told apart (RR6-C1); crash points
leave no duplicate; 100,000 failing items stay within 9 to 17 MiB and about 9 s; there is no
lock-ordering hazard; the breaker's ring, consecutive count, rate bound, time bound and reset
are arithmetically right; per-mailbox breakers count across folders only.

## What this round did not cover

- The capture host/path pin (FI-SGc): a foreign-host nextLink in capture's listings is a known
  open gap (ESC-7, skip encoder), not a finding.
- Real-tenant behaviour (K7): everything ran against the fake Graph server through
  `WrapTransport`; no network, no tenant. Windows behaviour was compiled with `gofor`, not run.
- Items the finders surfaced and left out as unbounded by a documented figure or outside the S3
  change set: `AltPhysIDs`, the listing's Message-ID text on stubs and the folder count in
  `Folders` have no size or count cap (SG and rev-6 code; the no-size-cap item is RR6-OR1/K1);
  a lying well-known-folder resolution (Deleted Items resolved to the Inbox id) silently drops a
  folder from the walk and is indistinguishable from an endpoint that omits it; an honest-looking
  `{"value":[]}` is swept like an empty folder.
- The extraction workspace's listing decoders (`internal/graph/workspace.go`, lines 104 and 252)
  have the same shape as S3-AO1 (an absent `value` reads as an empty list). They feed the copy
  export, which has no sweep, so an empty answer costs a copy of nothing rather than a phantom
  gone, and they are outside the FI-S3 change set; noted for the extraction workspace's owner.
- The run's console summary and the verification report's TSV word a stuck Graph
  vanished-at-fetch gap with the generic first-time words (they have no stuck notion); only
  `status` was corrected (S3-AI5).
- Token-endpoint 5xx classed as auth (pre-existing, FC11), and the 10-minute to 2-hour cap
  itself (a design decision, 13.22), were not re-examined.
- One second review of the fixes (see "Re-verification"); the second-round fixes (S3-AO2,
  S3-AI3 b, S3-AC5) were written by a different session from the first and have had no
  further review.

## Final verification

All through `inhome` with a scratch home, at the head of the fix commits (`e65fa86`), before
this report's own commit.

| Command | Status |
|---|---|
| `go test ./...` (unfiltered), run 1 | exit 1: one failure, `TestCopyExportTransportOutageAndTheBreaker` (`copyexport_breaker_test.go:86`), a copy-export test whose "outage" is a 30 ms wall-clock window from the test's start (`until(t0, 30*time.Millisecond, ...)`), so a loaded machine misses it; not this slice (the copy export, not capture) |
| `go test -run '^TestCopyExportTransportOutageAndTheBreaker$' -count=3` | exit 0, 3 of 3 pass |
| `go test ./...` (unfiltered), run 2 | exit 0: every package `ok`, `assureblock` included (the covers map: every new test carries a marker citing a catalog row) |
| `go test -race -count=1 ./internal/app ./internal/graph ./internal/state ./internal/health` | exit 0 |
| `vet3` (go vet for darwin, linux and windows) | clean on all three |
| `gofor windows test -c ./internal/app` | exit 0 (test binary built, not run) |
| `gofmt -l internal cmd assureblock` | no files listed |

During the fixes one other timing-sensitive test failed once under load and passed alone
three times (`TestGraphBreakerTripsOnlyWithAFailingControlRequest`, the breaker's time bound,
scaled to milliseconds).

### Second round (after the re-verification fixes)

All through `inhome` with a scratch home, at the head of the second-round fix commits
(`d9c55a4`), before this report's own update (a documentation change only).

| Command | Status |
|---|---|
| `go test ./...` (unfiltered), run 1 | exit 1: two failures, both copy-export tests that depend on load, not this slice: `TestCopyExportTransportOutageAndTheBreaker` (`copyexport_breaker_test.go:86`, a 30 ms wall-clock outage window) and `TestCopyExportScalesToSixtyThousandMessages` (a 40 MiB heap bound, grew 40 MiB) |
| `go test -run '^(TestCopyExportTransportOutageAndTheBreaker\|TestCopyExportScalesToSixtyThousandMessages)$' -count=3 ./internal/app` | exit 0, both pass 3 of 3 |
| `go test ./...` (unfiltered), run 2 | no failing line: every package `ok`, `assureblock` included |
| `go test -race -count=1 ./internal/graph ./internal/state ./internal/health ./internal/app` | exit 0 |
| `vet3` | clean on darwin, linux and windows |
| `gofor windows test -c ./internal/app` | exit 0 (test binary built, not run) |
| `gofmt -l internal cmd assureblock` | no files listed |
