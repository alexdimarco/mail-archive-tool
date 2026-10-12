# MF-S1 adversarial pass: identity routing, the frozen v1 replay, stems and ParseScheme

**11 found / 4 confirmed / 1 refuted** (3 duplicates and 3 non-goals counted separately).
**Verdict: FINDINGS OPEN.** Of the 4 confirmed findings, 2 are fixed, each with a tombstone
test seen red before its fix or with the fix mutated out, and green after: **MS1-AI1** (the
re-read parse-scheme stamp had no gate) and **MS1-AC4** (two unpinned guards). Two are open
because each remedy changes the frozen replay and needs an owner ruling under the design's
drift policy (section 5, MC28): **MS1-AO1** (identity of an alphabetic-zone Date depends on the
host time zone) and **MS1-AO2** (the frozen replay is quadratic on a hostile header or nested
multipart and has no bound). Each has a skip encoder with a real body that was run unskipped
to show it red.

| | |
|---|---|
| Reviewed commit | `19364f1` (branch `wip/mf-s1-adv`), base `d170993` |
| Fixes | `a5146c4` (MS1-AI1 and MS1-AC4), `5615850` (skip encoders for MS1-AO1 and MS1-AO2) on the same branch; not pushed |
| Finders and skeptic | three finder agents (lenses AO, AI, AC) and one separate skeptic agent, run by the workflow; the fixer (this session) did not find any of the issues |
| Tool versions | Go 1.26.9 (scratchpad toolchain), run through `inhome` so no real settings, sign-ins or schedules are read |
| Baseline digest | none: this repository keeps no covers baseline file; `assureblock` reads `docs/scenario-catalog.md` and the tests' `// covers:` markers |
| Final commands and exit statuses | see "Final verification" at the end |

## MF-S1 adversarial pass

**Scope.** The MF-S1 merge (`609027b`, `451e177`, `4398d46`, `66b9013`, `e8e73bc`, `aeb4bbe`,
`dc0d943`, `44e66ea` and merge `9dc981a`): `git log --oneline d170993..HEAD -- internal/source
internal/model internal/export internal/state` filtered to MF-S1, and `git diff 609027b~1..HEAD --
internal/source internal/model internal/export internal/state internal/app/testdata`. Surfaces:
the frozen v1 replay (`internal/source/parsev1.go`, `parsev1/_dep`, `replayv1.go`,
`stubidentity.go`), the identity view in `internal/model/message.go` (`IdentityTerms`, `idv()`),
`safeParseMessage` routing in `mbox.go`, identity-view stems and the carried-forward original in
`internal/export/exporter.go` and `attach.go`, `ContentHash` on every local write,
`Record.ParseScheme` stamping and the re-read restore in `internal/state/manifest.go`, and the
regenerated p14 golden. Design: `docs/design-mime-fidelity.md` (section 10 S1 row, MC5, MC7, MC8,
MC12, MC26, MC27, MC29); rulings 13.24 and 13.16, and the rulings row 12 of this pass.

**Lenses.** AO (outside aggressor: hostile mail aimed at the identity layer and the frozen
replay), AI (insider: stale and mixed-version archives, stems and the carried-forward original),
AC (integrity and concurrency: determinism, routing and fault paths).

**Standing rulings (not findings).** J5, OR4 (identity is v1's, content still v1 in S1), J6, J7,
ESC-2 (the R2 clarification is not in S1), ESC-1 (the raw-stub identity fold is FI-S2b-id), and
the owner's decision of 2026-10-11 that `p14-golden.json` carries exactly the two additive fields
`chash` and `parse_scheme`.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| MS1-AO1 | AO | An alphabetic zone abbreviation in `Date` (EST, PST, CET) is resolved by `time.Parse` through the host's `time.Local`, so identity, fingerprint, `ContentDigest` and the UTC stem move with the host zone. `parsev1/_dep/netmail/message.go:126` (zones `MST`), `:186-188` | CONFIRMED, medium, **open** (owner ruling, MC28) | Reproduced twice (finder, skeptic) by swapping `time.Local`. Same bytes (`Message-ID: <est@x.example>`, `Date: Mon, 02 Jan 2006 15:04:05 EST`): host UTC gives fp `376f4140d094bcbd` and `.../2006-01-02_1504_Quarterly_58833848.html`; America/New_York gives fp `2c977d1a7e6cc44b`, and the exporter files a second record under a `#fp` key (records=2, skipped=0). Without a Message-ID the key itself changes. Numeric zones, UT, GMT and UTC are host-independent. The corpus has no alphabetic-zone Date, so MA-330/331 cannot see it. Pre-existing in v0.7.1 (same stdlib parser); S1 freezes it. Fixing it (mapping abbreviations to one fixed rule) moves identity for one host population. Confirmed again here: with the skip removed, `TestFrozenReplayIdentityDoesNotDependOnTheHostZone` fails with `one message, two identities by host zone: UTC "mid:est@x.example"/376f4140d094bcbd, America/New_York "mid:est@x.example"/2c977d1a7e6cc44b` | none (escalated) | `TestFrozenReplayIdentityDoesNotDependOnTheHostZone` in `internal/source/s1adversarial_test.go` (skip encoder; `covers: MA-330, R2, R3, R27`) |
| MS1-AO2 | AO | The frozen replay is quadratic on an unclosed comment in `Message-ID` (`header.go:73-92`, `comment += p.s[:1]`) and on nested multipart, with no deadline; `replayv1.go:20-27` recovers only panics. `parsev1/_dep/mail/header.go:69-94`, `_dep/mail/reader.go:82-114`, `_dep/textproto/multipart.go` | CONFIRMED, medium, **open** (owner ruling, MC28). Duplicate MS1-AC1 | Reproduced (skeptic): `Message-ID: <a@b> (` plus n bytes: 25k 26 ms, 50k 100 ms, 100k 407 ms (4x per 2x); nested multipart depth 500/1000/2000/4000: replay 36/95/385/1508 ms, content parser the same. `safeParseMessage` runs both parsers, so S1 doubles a cost v0.7.1 already had; one message stalls an archive run. MC14 concedes only fatal errors and OOM; MA-349's deadlines cover the future walker, not the frozen replay. A bound on the frozen copy (builder patch, depth cap) is a drift decision. With the skip removed, `TestFrozenReplayIsBoundedOnAHostileHeader` fails with `the frozen replay of a 200 KB header took over a second (quadratic)` | none (escalated) | `TestFrozenReplayIsBoundedOnAHostileHeader` in `internal/source/s1adversarial_test.go` (skip encoder; `covers: MA-331, R27`) |
| MS1-AO3 | AO | The vendored net/mail, mime, base64 and go-message copies live under the module's own import paths, so govulncheck never matches them and the MC28 CVE trigger cannot fire. `parsev1/README.txt:8-19`, `.github/workflows/ci.yml:44-46` | NON_GOAL, low | Not reproducible offline. In S1 the content parser still imports the real net/mail and go-message (`mbox.go:7,14,15`; `go.mod` still requires go-message v0.16.0), so the scan still fires there. What remains, a copy lagging a fixed upstream after a toolchain bump, is the residual design P1(b),(c) names ("checked only on corpus shapes, aspirational beyond them") and hands to section 5's drift policy; `README.txt` records each copy's upstream version. An automated upstream-lag check is hardening, not an S1 defect. Duplicate: MS1-AC3 | n/a | n/a |
| MS1-AO4 | AO | `ContentDigest` (the MC5 same-message proof, written on every local record) is not injective: NUL-joined fields, attachment bytes absent. `message.go` `ContentDigest`, `exporter.go:521` | DUPLICATE of MS1-AC2 (NUL half); attachment half not a gap | The NUL half is MS1-AC2 (reproduced there). The formulas are byte-identical to v0.7.1's and pinned by MA-330's golden. The attachment half is not a gap in MC5: design V1C 3 matches every archived zip entry by (size, SHA-256), so an equal digest alone is never the proof; same-Message-ID different-attachment-bytes is the documented resend limit (Fingerprint doc comment) | n/a | n/a |
| MS1-AI1 | AI | The skip branches stamp `state.ParseSchemeCurrent` on every re-read with no predicate, so once a pre-S7 slice bumps the constant a no-op re-read marks unrepaired records current and S7's rule `n.Since > prev.ParseScheme` never fires for them. `exporter.go:312`, `:323`; `manifest.go:190`, `:1227`; design lines 977-982, 1447, 1762 | CONFIRMED, **low** (skeptic; finder said medium), **fixed** | Mechanism read from source and confirmed by the skeptic; not simulated, because S2 does not exist. Latent at `19364f1` (scheme 1, no note registered, nothing acts on the marker). The finder's run with a bumped constant stamped 111 of 111 records without a repair. Fix: both skip branches stamp only when `examinedBy(m, prev)`: no note the re-read recorded has `Since` above the record's scheme. Nothing records a note before S2, so this build's behaviour is unchanged today. Red before the fix: `ParseScheme = 1 after a re-read that carries a note the record's scheme predates, want 0 (not examined)` (both the complete and the still-incomplete subcases) | `a5146c4` | `TestReReadStampsTheParseSchemeUnlessANoteIsUnexamined` in `internal/export/mimeidentity_test.go` (`covers: MA-347, R27`) |
| MS1-AI2 | AI | The carried-forward `.eml` is linked and vouched for as the "Original message" of a re-captured message whose content changed (`carriedOriginal` ties it to the record by path and file presence only). `exporter.go:379-386`, `:421-423`, `:521`; `html.go:2293` | NON_GOAL, low | Behaviour reproduced (skeptic): a -raw capture of `<q@x>`, then a full-mode re-capture without -raw with a new body, leaves one record at the same path, the page shows the new body and links the `.eml`, `Fixity.EML` unchanged, the `.eml` holds the old body. It is the design: section 3 (lines 422-423) "every re-capture of a seen record keeps its stem and carries its .eml forward (MC8)"; MC11 (lines 1118-1122) says the `.eml` and `Fixity.EML` are "always carried forward" and "today's source bytes are not assumed equal to the preserved original". Overwriting a changed message's page on a full re-capture is F46, owned by the re-capture-integrity design. Deleting the old `.eml` would lose evidence (R20); gating the carry on content equality would contradict MC8 and MC11. A label wording tweak is at most a friction item | n/a | n/a |
| MS1-AI3 | AI | Ruling 13.24 says `chash` is "restored on re-read", but only `parse_scheme` is. `owner-rulings-2026-10-09.md:1367`, `design-mime-fidelity.md:1073` | REFUTED, low | The behaviour is real and not a defect: the skip branches restore `parse_scheme` only; the design (S1 row line 1751, line 1234) says "stamps re-read records" for `ParseScheme` only, and restoring `chash` would bless whatever the source now holds. Design V1C 5 (lines 1069-1078) already handles an absent `chash`: the third branch (archived page body text against v1-now's selected body) applies "otherwise". The sentence in 13.24 reads naturally as the stamp being restored: a wording nit in the rulings log, which this pass does not edit. An S7 implementation must key on `chash == ""`, not on `ParseScheme >= 1`, which the design already says | n/a | n/a |
| MS1-AC1 | AC | S1 doubles an unbounded quadratic parse of nested multipart, with no deadline or stub seam. `mbox.go:215-246`, `replayv1.go:20-27`, vendored `_dep/mail/reader.go`, `_dep/textproto/multipart.go` | DUPLICATE of MS1-AO2 | Nested multipart depth 4000 (281 KB): replay 1.51 s, content parser 1.52 s; pre-existing in v0.7.1, no deadline; a bound on the frozen replay is a drift decision under MC28. Open with MS1-AO2 | none (escalated with MS1-AO2) | `TestFrozenReplayIsBoundedOnAHostileHeader` (covers the header shape; nested multipart is the same escalation) |
| MS1-AC2 | AC | The frozen identity formulas join fields with `\x00` and never escape it, so distinct messages that carry NULs collide and the later one is silently dropped. `message.go:312-320`, `:357-370`, `:381-393` | NON_GOAL, low | Mechanism reproduced (skeptic): html `A\0B` with plain `C` against html `A` with plain `B\0C` give equal Identity and ContentDigest; through the real parser and exporter the first was exported and the second skipped (records=1, skipped=1). The formulas are byte-identical to v0.7.1's, pinned by MA-330's golden (design lines 190, 1656, 1672); a fix is an identity migration. Design section 3 non-goal 1 reads "Changing the identity scheme... v1 identity quirks therefore persist for new captures too... (an existing limit, now written down)", and rulings J5 and OR4 say identity is v1's; its list says "notably", so it is not exhaustive. Exploiting it needs NUL bytes in both colliding messages, authored by the same sender. Optional follow-up for the design owner: name the NUL-join ambiguity in non-goal 1 (this pass does not edit the design) | n/a | n/a |
| MS1-AC3 | AC | The release gate cannot see advisories against the vendored stdlib copies. `ci.yml:46`, `release.yml:35-36`, `parsev1/_dep/*` | DUPLICATE of MS1-AO3 | Same claim; see MS1-AO3 | n/a | n/a |
| MS1-AC4 | AC | Two S1 guards have no test that pins them (mutants survive): (a) the `parse_scheme` stamp on the still-incomplete re-read branch (MC12, mutant M08); (b) `carriedOriginal`'s `prev.Path != relSlash` same-stem condition (mutant M28). `exporter.go:323`, `:626-638` | CONFIRMED, low, **fixed** | Reproduced by mutation by the finder and the skeptic: both mutants passed `go test ./internal/state ./internal/export`. Tests added and shown red against the mutations here. (a) mutation `if false {` in place of the stamp gate on the still-incomplete branch: `--- FAIL: TestReReadStampsTheParseSchemeUnlessANoteIsUnexamined/still-incomplete_fillable_record: ParseScheme = 0 after a re-read with no unexamined note, want 1`. (b) mutation `if prev.Path == "" {`: `--- FAIL: TestRecaptureAtAnotherStemKeepsTheOriginalOnDisk: the page links an original that is not the record's own (an orphan at the new stem)`. The test moves a record to another stem and back, so the first capture's original lies, orphaned, at the stem the record returns to. Both green on the unmutated code | `a5146c4` | (a) `TestReReadStampsTheParseSchemeUnlessANoteIsUnexamined` (`covers: MA-347, R27`); (b) the new final step of `TestRecaptureAtAnotherStemKeepsTheOriginalOnDisk` (`covers: MA-346, R20, R13`) |

## Known items this round also closes

| Item | Outcome | Commit |
|---|---|---|
| K1: MA-346 (b, d) and MA-347's repair half are skip encoders for MF-S7 | Confirmed present and still skipped with the invariant cited: `mimeidentity_test.go` MA-346 (a, second half), (b), (d) and the MA-347 repair-half subtest. No test gap: they cannot be written before S7 (V1C, the restamp and the repair branch do not exist). Unchanged | none |
| K2: the v1-panicking corpus item does not exist (MF-S0 fuzzed 14 million executions) | Confirmed: the MA-330 panic corpus case in `mimecorpus_test.go:163` stays a skip; the stub identity is pinned from the constructed stub and fault injection, and this pass's fuzzing (about 4.2M, 5.1M, 3.8M and 4.85M executions across the three finders) found no panic either. Unchanged | none |
| K3: `TestCopyExportScalesToSixtyThousandMessages` is near its heap bound | The owner's call. Not touched; it ran green in the final unfiltered run (see below) | none |
| K4: ESC-1, the raw-stub identity collapse | Standing ruling (FI-S2b-id). Not touched | none |
| K5: content still comes from v1 | Not a finding in S1; no change | none |
| K6: go1.26.9 stdlib against the release toolchain (OR4) | Standing ruling. The finders measured the `x/text` and `x/net` drift (v0.21.0 to v0.42.0, v0.10.0 to v0.60.0) as unobservable on 1182 labels. Not touched | none |

## Escalations (need an owner ruling)

| Key | Why | Skip encoder |
|---|---|---|
| MS1-AO1 | Pinning the host-zone dependence of alphabetic-zone Dates needs a rule (for example, abbreviations read as one fixed offset or UTC on every host) that moves the identity, fingerprint and stem of any message read today on a host that knows the abbreviation, so it is a drift decision under MC28, and J5/OR4 say identity is v1's. A population is moved either way: hosts whose zone database knows EST today, or hosts that do not. Invariants it breaks: R2 (zero exports over an unchanged source), R3, MA-330, P1(b) | `TestFrozenReplayIdentityDoesNotDependOnTheHostZone` |
| MS1-AO2 (with MS1-AC1) | Bounding the frozen replay (a semantic-preserving `strings.Builder` patch to the vendored `consumeComment`, with MA-330 green and a differential fuzz; a depth cap on nested multipart, which moves identity for a message that crosses it) is a drift decision under MC28 and MA-331 pins the copies' bytes. A wrapper-level deadline would turn a slow parse into a stub identity for an otherwise ordinary message, which is also an identity change. Invariants: R27, MA-331, the design's P1 | `TestFrozenReplayIsBoundedOnAHostileHeader` |

## What held

The finders' negative results, kept as evidence (the S1 surfaces they probed and found sound):

- **Frozen replay equals v0.7.1.** `parseMessage`, `fallbackParse`, `addressList` and `bytesWriterTo` in `parsev1.go` are byte-identical to v0.7.1's `mbox.go` (0 diff lines; V1 suffix renames only). The vendored tree equals the pinned upstream (go-message v0.16.0, go-textwrapper, go1.26.9 mime/net/mail/quotedprintable/base64/byteorder), differing only by import paths and gofmt; `gopst.go` matches go-pst v6.0.2's nameMap including the 7 unresolved labels.
- **Pins bite.** MA-331's pin check fails on a one-byte edit (comment appended to `_dep/mail/header.go`: red naming the file and both hashes); every file under `parsev1/` is covered; mutating `identityTermsOf` (dropping Bcc, Cc or IdentityDate) turns the identity golden red.
- **Dependency drift is not observable.** x/text v0.21.0 to v0.42.0 and x/net v0.10.0 to v0.60.0: 1182 labels over 4 probe streams are byte-identical, 20,000 random short samples times about 560 labels give identical decode hashes, and the S1 goldens and pins pass with the old versions. A `go.mod` replace directive is same-account and out of scope.
- **Differential fuzz, no difference.** 60 s (5.1M executions) of `parseMessage` against `parseMessageV1`, 3.8M executions of `replayV1` against the shared parse, 4.85M executions of `safeParseMessage` (no panic, no hang, terms always set, `V1Inventory` length equal to the terms' attachment count), about 4.2M over raw bytes and structured headers; panic parity held.
- **Cost beyond nesting is bounded.** 1M header lines, 300k empty parts, 100k attachments, 60k addresses, 200k folded lines, 100k header fields, 2M qp breaks, 1M invalid base64 lines: linear, transient, under 0.2 s for shaped inputs; a 100 MB message peaks at about 1.5 times the old heap and 2 times the time. Only multipart nesting and the Message-ID comment are quadratic (MS1-AO2).
- **Mixed versions.** v0.7.1's `Load` is lenient JSON (no `DisallowUnknownFields`), `manifestVersion` stays 6, and an older binary drops `chash` and `parse_scheme` harmlessly. Real v0.7.1 and HEAD binaries alternated on 111-message and 123-message mbox corpora (-raw on/off, incremental and full): zero new keys, zero `#fp` splits, no stem change, no file added or removed, record counts stable. The same 559 hostile fuzz inputs archived by both binaries give identical file sets and keys (175 records).
- **Stems and the carried original.** `carriedOriginal` is same-stem, regular-file (Lstat), fixity carried not recomputed; the old `.eml` is removed only when a new one is written; a stem change leaves the old `.eml` orphaned on disk (ruling J6, tested). Nothing overwrites or deletes an owner's `.eml` in the paths read. After an older binary's full run drops `Fixity.EML`, this build re-links the `.eml` with a nil digest, so `verify` exits 2 (unrecorded) until `verify -record`: an observation, not an S1 defect.
- **Identity, fingerprint and digest equal v0.7.1** on the corpus and on 30,000 mutated messages (no stub produced); the p14 golden diff is only `chash` (34 local healthy records) and `parse_scheme` (all 46), the 12 live records kept their `chash`, and no fixity, path or fp moved.
- **Graph stub.** `legacyStubIdentity(t)` equals the identity of the S1 stub once Received is applied; the three "(unreadable message)" constants agree; `SetInternetMessageID` is pinned (mutants M02, M30 killed); no post-parse code mutates an identity field other than the two designed writers.
- **Determinism and concurrency.** `-race -count=3` on source, model, state, export and p14 passes; a 24-goroutine parse is bit-identical; the vendored tree has no `sync.Pool` and no mutable globals beyond init-time tables; `Manifest.StampParseScheme` takes the lock. `.gitattributes` marks `testdata/mime` and `parsev1` `-text`, so Windows autocrlf cannot move the pins; the source package compiles for Windows.
- **Mutation testing** of the S1 code: 34 mutants of identity terms, stems, `chash`, stamps, recover and the Graph stub terms; 32 killed, 2 survived (MS1-AC4, now pinned).

## What this round did not cover

- **govulncheck** was not run (it needs the network); MS1-AO3/AC3 rest on how it matches advisories by path and on the design text.
- **MS1-AI1 was not simulated against a bumped constant**: `ParseSchemeCurrent` is a constant and S2 does not exist. The tombstone drives the same predicate with a re-read note whose `Since` is above the record's scheme. When a slice bumps the constant it must register its notes with `Since` equal to the new value, which the gate relies on.
- **The host-zone class beyond alphabetic abbreviations** was probed by the finder's 50 s date fuzz across four host zones with abbreviations excluded; nothing else was found. Other host inputs (locale, `GODEBUG`, a different Go release) are K6-class and not probed.
- **Real mail, the network and a live Graph tenant** were not used; `ContentDigest` of attachment bytes and Graph fast-path semantics beyond the stub terms are outside the S1 change set.
- **The PST twin of MA-330** and `internal/app` beyond the regenerated p14 golden were read only as far as the finders' differential runs went.
- **The fixes were checked by their authors' own tests** and the unfiltered suite; no second reviewer has re-verified them.

## Final verification

Run at code head `5615850` (the report commit adds only this file), all through `inhome` with
Go 1.26.9:

| Command | Result |
|---|---|
| `go test ./... -count=1` (unfiltered; run per package with output to a file, because `go test ./...` with a pipe held open by a test child looked stalled) | exit 0 for all 33 packages, `internal/export` 11 s (includes `TestCopyExportScalesToSixtyThousandMessages`), `internal/source` 7 s, `internal/state` 5 s, `internal/app` 173 s, `assureblock` ok |
| `go test -race -count=1 ./internal/source ./internal/model ./internal/export ./internal/state` | exit 0 |
| `vet3` | clean on darwin, linux and windows |
| `gofor windows test -c` of `internal/source` and `internal/export` | exit 0 (both build) |
| `gofmt -l` over the worktree | no output |

**Re-verification of the fixes.** MS1-AI1: red before the fix (`ParseScheme = 1 after a re-read that
carries a note the record's scheme predates, want 0 (not examined)`), green after. MS1-AC4 (a) and
(b): green on the code, red under the two mutations quoted in the table, mutations applied in a
detached probe worktree only. The two skip encoders were run unskipped in the same probe
worktree and are red for the stated reasons. No second reviewer has re-verified these fixes.

**Open.** MS1-AO1 and MS1-AO2 (with MS1-AC1): each needs an owner ruling under MC28 before the
skip encoder can be removed.
