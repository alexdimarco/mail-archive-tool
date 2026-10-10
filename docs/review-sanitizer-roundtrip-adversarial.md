# Adversarial pass — sanitizer round trip after the x/net bump (commit 033b1e3)

**Reviewed:** commit `033b1e3`, which bumps `golang.org/x/net` from v0.10.0 to v0.60.0
(together with x/text 0.21 → 0.42 and oauth2 0.24 → 0.37) and adds a fail-closed re-parse
check to `internal/export/html.go`. After `sanitizeHTML` (`:142`) serializes the head and
body, `hostileSurvives` (`:218`) re-parses the output, and a live hostile meta sends the body
to `plainFallback` (`:199`, escaped source in `<pre class="plain">`). The tests are MA-300
(foreign-content raw-text meta never comes out live, `html_roundtrip_test.go:57`) and MA-301
(fault-injected renderer → fail closed, `:104`). Parent commit for old/new comparisons:
`066f86d` (x/net v0.10.0).

Per assurance-kit `process/adversarial-review.md`. The pass ran as a workflow on 2026-10-09.
Each finder worked in its own throwaway worktree at `033b1e3`. Every finding then went to a
refutation-default skeptic, who reproduced it independently with their own harness, beacon
port and PoC test. Personas / lenses:

- **Outside aggressor (hostile email sender).** Crafted bodies rendered through
  `export.RenderWith` and opened from `file://` in headless Edge 143.0.3650.139, with
  JavaScript on and off, against a local beacon server that logs every GET.
- **Integrity.** The export → `reindex -rebuild` → `search` round trip with binaries built
  from `033b1e3` and from `066f86d`; the race detector; escaping; dependency-diff review.
- **Differential (x/net tree vs browser DOM, plus a false-positive check on ordinary mail).**
  Did **not** complete in round 1. The pass stopped before any testing, so round 1 has no
  differential findings and no negative results. The lens ran in round 2, against `aa1c98d`
  (see *Differential lens results* under *Re-verification of aa1c98d*).

**Round 2** (2026-10-09) re-verified the fix commit `aa1c98d` with the integrity and outside
aggressor lenses and completed the differential lens, under the same finder → skeptic process.
See *Re-verification of aa1c98d* below.

**Round 1 (`033b1e3`): 4 found / 4 confirmed / 0 refuted.** The skeptic rated them 0 high,
3 medium and 1 low. The finders had rated OUT-1 and INT-1 high; the skeptic lowered both to
medium, with reasons given below. All 4 are fixed in `aa1c98d`. **Round 2 (`aa1c98d`): 12 found
/ 12 confirmed / 0 refuted**, rated 0 high, 3 medium and 9 low; the finder had rated RV-3 medium
and the skeptic lowered it to low. **Both rounds: 16 found / 16 confirmed / 0 refuted (0 high,
6 medium, 10 low); 15 fixed (round 1's 4 in `aa1c98d`, 11 of round 2's 12 on
`wip/sanitizer-r2-integrated`, which round 3 then attacked), 1 open (RV-10, low).**

**Round 3** (2026-10-09, `wip/sanitizer-r2-integrated` at `829f51d`, the adversarial pass the
rulings log's §9 row 1 owes before the branch merges) used three lenses (outside aggressor, the
rebuild forger, integrity and the differential): **14 found / 8 confirmed / 0 refuted**, with 5
duplicates and 1 non-goal counted separately; the confirmed ones are rated 0 high, 1 medium (R3-AO1)
and 7 low. **All three rounds: 30 found / 24 confirmed / 0 refuted (0 high, 7 medium, 17 low); 22
fixed, 2 open (RV-10, R3-AF2).** Seven of round 3's eight are fixed on the same branch (see *Round 3*
at the end, with its commits, tombstones and a corpus run on a binary built at the fixed head), and
so are both san-Q2 shapes. The two round-3 duplicates of known gaps (R3-AF1, R3-AC1) and the
non-goal (R3-AO2) are named there as still open or ruled. A fresh-eyes pass over round 3's own
fixes (see *Re-verification of round 3's fixes* at the end of *Round 3*) found four more low
defects (NF-1 to NF-4) and two incomplete fixes (R3-AC3, san-Q2 shape 1); NF-1 to NF-3 and both
incomplete fixes are fixed on the same branch with tombstones, and NF-4 is recorded as defense in
depth.

**Verdict: FINDINGS OPEN.** Open: **RV-10** (the fallback is not surfaced in `status`, the GUI or
the desktop message; the status branch owns it), **R3-AF2** (a quoted local part holding ` <` makes
the rebuilt sender differ from the live one; the fix changes the page's sender format, so it is the
owner's decision and a skipped MA-187 encoder holds it), the known gaps encoded as skipped guards and
not accepted — **MA-187's erase half** (R3-AF1; FB-1, FX-187's writer signal) and **MA-492's
outer-noscript escaped text** (R3-AC1; MA-476's escaped twin) — the owner question behind **R3-AO2**
(fail closed on any live mail `<link href>`?), and a **second look at the fixes that
re-verification itself made** (NF-1's standards-mode parse, the form unwrapping, the plaintext refusal
and NF-3's noscript refusal were written and tested by the fixing agent and have had no adversarial
pass of their own). Round 3's fixes were read by a fresh-eyes pass, whose findings are fixed (see
*Re-verification*). Note: no row in the rulings log exists for R3-AF2 or R3-AC1 (MA-476's escaped
twin), so neither is on the owner's list yet; they are escalations to be filed. The verdict can say
"no findings open" only when every finding of every round is fixed and re-verified. As of round 2
the twelve findings were: three confirmed medium, all
present at `066f86d` — RV-1 (a pre-v0.3.0 page lets a mail forge its own subject, sender and date
on `reindex -rebuild`), RV-2 (`<link rel="preconnect">` survives sanitizing and opens a
connection to the sender's host when the archived page is viewed) and RV-4 (benign mail that
omits `</head>` gets no body words in the live index) — and nine low: DIFF-FP-1, DIFF-FP-2, RV-3
and RV-5 to RV-10. Eleven of the twelve are now fixed on `wip/sanitizer-r2-integrated`; only
RV-10 is still open (its owner is the status branch). See *Round-2 fixes* below for the commits,
tombstones, open owner questions and residuals; the "Fix commit"/"Tombstone test" columns name
each finding's guards. **The verdict stays FINDINGS OPEN** regardless (round 3 attacked these
fixes; see above): RV-10 remains open. Round 1's four findings (OUT-1, INT-1, INT-2,
INT-3) are fixed in `aa1c98d` and were re-verified. In round 1 the commit's central claim, that
the round trip is re-checked "as a browser will" parse it, held only for a browser with
scripting **on**; `aa1c98d` re-checks under both scripting modes, and the differential lens
found no offensive parser differential left.

| ID | Lens | Claim | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| OUT-1 | outside aggressor (hostile email sender) | A `<meta http-equiv="refresh">` placed inside `<noscript>` comes out of `sanitizeHTML` **live**, and `hostileSurvives` does not fail closed. x/net parses with scripting **enabled** by default (`x/net@v0.60.0/html/parse.go:2294`, `scripting: true`), so the content of `<noscript>` becomes raw text. `neutralize` (`html.go:274`) never sees an `atom.Meta`, and `xhtml.Render` writes the text back as literal markup (`render.go:240-248`, `childTextNodesAreLiteral` always includes noscript). The re-parse in `hostileSurvives` (`html.go:219`) and MA-300's independent helper `liveHostileMetas` (`html_roundtrip_test.go:18`) both call `xhtml.Parse` with the same default, so both share the blind spot. A browser with scripting **off** parses `<noscript>` content as markup, and the refresh navigates the page to the sender's URL. This defeats R19 (`scenario-catalog.md:132`, which is unconditional) and the noscript coverage that MA-300 names (`scenario-catalog.md:363`). | CONFIRMED (medium; finder: high) | `RenderWith` on `<p>marker</p><noscript><meta http-equiv="refresh" content="0;url=http://127.0.0.1:8741/b/sk_noscript_body"></noscript>` writes `<noscript><meta http-equiv="refresh" …></noscript>` into `div.mailarchive-body`: `http-equiv` is not renamed to `data-mailarchive-neutralized`, and there is no `<pre class="plain">` fallback. The same holds in 4 placements: body, `<head>`, a nested `<div>`, and `<svg><foreignObject><noscript>` (an HTML integration point). `liveHostileMetas` returned 0 matches on every case, while a scripting-disabled re-parse returned 1. Headless Edge 143 (`--headless=new`, throwaway `--user-data-dir`, `file://`): with JS on (the default), no beacon hits on any of the 4 pages. With `--blink-settings=scriptEnabled=false`, the beacon logged `HIT GET` for all 4 (`/b/sk_noscript_body`, `_head`, `_div`, `/b/sk_svg_fo_noscript`). The finder's run on port 8731 got the same result. Controls: a plain body `<meta refresh>` was neutralized and gave no hit with JS off. A raw, unsanitized refresh page and a raw remote `<img>` page with no CSP **did** hit, so the harness was live and the empty JS-on log means the page really was inert. Scope: the threat model concedes no non-goal for viewers with JS off. The archive ships zero JS and its CSP blocks scripts, so a reader with JS off is a reasonable posture (Tor Safest, a policy that blocks JS, `javascript.enabled=false`). Severity is medium, not high: the impact is the same as OFF-2 (`review-archive-offline.md`), an open beacon / IP leak or a phishing landing page. It adds a precondition (scripting off), and default viewers stay inert. The payload is trivial and needs no encoding. | `aa1c98d` | MA-300 `TestRenderForeignContentRawTextStaysInert` (noscript body/head/nested/svg-foreignObject/escaped cases; re-parse with scripting on and off) |
| INT-1 | integrity | The x/net v0.60 parser has a hard nesting cap: `html.Parse` returns `html: open stack of elements exceeds 512 nodes` (`parse.go:235-239`, panic recovered at `:2225`). `readArchivedHTML` (`internal/app/htmlheader.go:43-50`, used by `reindex -rebuild`) treats a parse error as "nothing recovered". Any archived page nested deeper than 512 elements therefore rebuilds with an empty subject, from, date and body, and search can no longer find it. This hits (a) every deep page written by x/net v0.10, which had no cap, and (b) **new** exports, because of a one-level gap. `sanitizeHTML` and `hostileSurvives` parse `html>body>mail`, but the page adds `div.mailarchive-body` on top, so a body that is exactly 512 deep passes sanitize, renders as HTML, and then cannot be read back. This violates R13 ("surviving files stay searchable"). | CONFIRMED (medium; finder: high) | The mbox held text/html messages `'<div>'×K + '<p>body words for NEEDLE</p>' + '</div>'×K` with K = 10, 508, 509, 510 and 600. (1) New export, new rebuild: the live index finds all 5. After `rm search.db*; reindex -rebuild` the output is `rebuilt=5 (from-eml=0 re-derived=5 unrecovered-fields=3)`. `search needleepsilon` (K=509) and `search deep509` both return `0 match(es)`, and the folder row becomes `(empty date) \| (unknown) \| (no subject)`. (2) Upgrade path: an archive exported by `066f86d` and rebuilt by `066f86d` gives `unrecovered-fields=0` and every needle is found. The same archive rebuilt by `033b1e3` gives `unrecovered-fields=9`, and K=509, 510 and 600 return 0 matches (K=508 and K=10 are still found). Severity is medium, not high: only pages nested more than ~508 deep are affected, the pages stay readable on disk, the rebuild summary counts the loss (so it is not fully silent), and archives kept with `-raw` rebuild from the `.eml`. | `aa1c98d` | MA-304 `TestRebuildRecoversDeepAndFallbackPages` (509-deep page) |
| INT-2 | integrity | The same cap changes export output without saying so. A mail nested more than ~509 elements deep fails the first `xhtml.Parse` in `sanitizeHTML` (`html.go:143`), so the whole body is written as escaped source by `plainFallback`. Before this commit, the same mail rendered as HTML. `sanitizeHTML` returns only strings, so the fallback reaches no issue entry, stat, manifest gap or `status` line. The `cid:` inline images were already turned into data URIs and marked Consumed before `sanitizeHTML` ran, so they are left out of the attachment zip and survive only as base64 text inside the escaped source. Every `hostileSurvives` trip (`html.go:187-189`) falls back in the same silent way. This regresses R7 (legible HTML) and R1 (no silent loss of an inline image). | CONFIRMED (medium) | `grep -c '<pre class="plain">'`: `066f86d` gives 0 for every K. `033b1e3` gives 1 for K=510 and K=600. Both runs print the same summary, `Done. exported=5 … non-html=0 no-body=0`. Inline case (multipart/related, `cid:img1@x`, K=600) on `033b1e3`: 0 live `<img src="data:image/png`, 1 escaped `&lt;img src=&#34;data:image/png`, no zip written, the summary still says `inline=2`, the manifest has no missing/terminal/issue entry, and `status` reports `Incomplete: none`. On `066f86d` the image renders live. The bytes are not lost (they survive as base64 text), but they can no longer be viewed and are not archived as a file. | `aa1c98d` | MA-303 `TestExportHTMLFallbackIsVisibleAndKeepsImages` |
| INT-3 | integrity | `reindex -rebuild` indexes a fallback page differently from the live export. The live index uses `index.BodyText` on the original message (clean text). The rebuild takes `innerHTML` of `.mailarchive-body` (`htmlheader.go:68-69`), which for a fallback page is the `<pre>` holding the escaped source, so `htmlToText` indexes the mail's tags, attributes and any base64 payloads as words. After a rebuild, snippets and FTS terms for every fallback page drift from the live export, which goes against R8 (search ↔ export parity). | CONFIRMED (low) | `033b1e3`, K=600 `needlebeta`. Live index: `search -json needlebeta` gives the snippet `body words for needlebeta`. After `rm search.db*; reindex -rebuild`, the snippet is `… div&gt;&lt;p&gt;body words for needlebeta&lt;/p&gt;&lt;/div&gt;…`. Parity drift: `search div` returns `0 match(es)` on the live index and `2 match(es)` after the rebuild (the K=510 and K=600 fallback pages). The K=10 control's snippet is unchanged. Low severity: ranking and snippets are polluted but nothing is lost. Before this commit the fallback was practically unreachable; now any depth over ~509, or any `hostileSurvives` trip, produces one. | `aa1c98d` | MA-304 (fallback page indexed by its words, "div" 0 hits) |

## Fix directions (from the skeptic) and tombstones to write

All four were fixed in `aa1c98d` and re-verified in round 2; the directions are kept as the
record of what was asked. Three were taken only in part: INT-1(b), closing the one-level gap at
export, was not done (a 509-deep body still renders as HTML and its page is read back by the
tokenizer path; see RV-5 and RV-7); INT-2's `status`/`verify` visibility was not delivered
(RV-10); and INT-2's depth-flattening idea was not taken (DIFF-FP-2).

- **OUT-1.** Make `hostileSurvives` check **both** scripting modes and fail closed if either
  is hostile: `xhtml.ParseWithOptions(r, xhtml.ParseOptionEnableScripting(s))` for
  `s ∈ {true, false}`. The option exists in v0.60.0 (`parse.go:2277`). The skeptic prototyped
  this: all 4 noscript placements fell back to `<pre class="plain">` with 0 live metas under
  either parse, the plain-refresh control still rendered as (neutralized) markup, and
  `go test ./internal/export` stayed green. **Do not** only switch the main `sanitizeHTML`
  parse to scripting off. The skeptic tested that: `<noscript>&lt;meta http-equiv="refresh"
  …&gt;</noscript>` then becomes a text child of a noscript element, which Render writes
  literally, giving a live refresh for JS-off viewers that a scripting-on re-check misses. If
  the main parse moves to scripting off, the dual-mode re-check is still mandatory. Change
  `liveHostileMetas` to re-parse under both flags too.
  *Tombstone:* `<noscript><meta refresh>` in body, head, div and `svg>foreignObject`, plus the
  escaped-in-noscript variant. It must be RED at `033b1e3` and GREEN after the fix. The MA-300
  and MA-301 catalog rows should then say "as a browser with or without scripting will".
- **INT-1.** (a) Make `readArchivedHTML` independent of depth: on a Parse error, fall back to
  an `xhtml.NewTokenizer` pass (no depth cap; `index/htmltext.go` already uses the tokenizer).
  The pass reads the `data-mailarchive-field` values from the first `.mailarchive-header` and
  the text of `.mailarchive-body`. (b) Close the one-level gap at export: re-parse with the
  page's real nesting (or the final assembled page), so a body the page itself cannot parse
  fails closed at export time.
  *Tombstone:* a rebuild over a K=509 page from the new renderer and a K=600 fixture from the
  old renderer, asserting that subject, from and date are recovered and that search finds
  both. It must be RED at `033b1e3`.
- **INT-2.** Have `sanitizeHTML` return a fallback reason (`parse-error` / `depth` /
  `hostile-reparse`) carried on `RenderResult`, counted in the run summary
  (`html-fallback=N`) and recorded as a durable manifest note that `status`/`verify` can see.
  On fallback, release the consumed `cid:` parts into the attachment zip and header list, or
  emit them as `<img src=data:>` after the `<pre>`. For depth specifically, consider
  flattening past a safe depth (as Chromium does at 512) instead of falling back.
  *Tombstones:* a K=600 mail with a `cid:` image asserts that the fallback is reported, the
  image is in the zip and the inline counts are honest. A MA-301 fault-injection trip asserts
  a reported fallback.
- **INT-3.** Mark fallback pages (`<pre class="plain" data-mailarchive-source="html">`). For a
  marked page, `readArchivedHTML` takes the pre's text (the original source) as `HTMLBody`, so
  it is indexed the same way as the live path. Unmarked plain-text pages keep their current
  behaviour.
  *Tombstone:* rebuild a fallback page and assert that the snippet equals the live snippet and
  that `search div` returns 0.

## What held

**Outside aggressor (all in headless Edge, inert or blocked):**
- A plain `<meta refresh>` was neutralized to `data-mailarchive-neutralized`.
- A remote `<img src=http://…>` was blocked by CSP `img-src data:`.
- An `<iframe srcdoc="<meta refresh>">` was blocked by `frame-src` (via `default-src 'none'`)
  and did not navigate.
- `<math><annotation-xml encoding="text/html"><meta refresh>` and a direct
  `<svg><foreignObject><meta refresh>` were neutralized: x/net sees `atom.Meta` at these HTML
  integration points.
- `<template><meta refresh>` was neutralized.
- An abrupt `<!-->` comment and a bogus `<![CDATA[x]]>` comment in the HTML body were
  neutralized: x/net agrees with the browser on both.
- An `<svg><style></style><meta refresh></svg>` style breakout was neutralized (the meta
  lands in the HTML namespace).
- `<title>x</title><meta refresh>` was neutralized (the meta lands in head).
- `http-equiv=" Refresh "` with surrounding whitespace did not fire, because the browser
  matches it exactly. `hostileEquiv` trims it anyway, a safe false positive.
- Every noscript payload was **inert with JS on**, so the OUT-1 break is specific to the
  JS-off viewing posture.
- The original MA-300 class (x/net before v0.13.0 writing SVG/MathML raw text unescaped,
  GO-2023-1988) is confirmed **fixed** by v0.60.0, under both JS on and JS off.

**Integrity:**
- Unfiltered `go test ./...` on go1.26.9 passed for every package, and
  `go test -race -count=1 ./internal/export/` passed.
- `renderNode` is not a production race: the only write to it is MA-301's non-parallel
  fault injection, restored with `t.Cleanup`, and the exporter has no concurrent render
  goroutines.
- `plainFallback` escaping (`html.EscapeString`: `< > & ' "`) means a `</pre>` inside the
  mail cannot break out of the fallback.
- x/text v0.42's Unicode 17 tables sit behind `//go:build go1.27`. On Go 1.26 it still uses
  tables15.0.0, matching the stdlib, so NFC in `SanitizeSegment`/`Slug` (folder keys, file
  stems) is unchanged on this toolchain.
- `plaintextAbort` in v0.60 render acts per child, because `sanitizeHTML` renders body
  children one at a time. Content after `<plaintext>` re-parses as text and stays inert.
- The render errors that `sanitizeHTML` ignores (doctype quoting, a void element with
  children) cannot come from head or body children built by the parser.
- The tokenizer changes (duplicate-attribute dedup, NUL replacement, self-closing detection)
  only slightly affect `index/htmltext.go` text extraction, and no words were lost.
- A rebuild of pages up to K=508 deep gives `unrecovered-fields=0` after the bump.

**Not covered by this pass** (nothing here counts as a negative result):
- **Second-parse cost.** One sample showed `RenderWith` about 8× slower on `033b1e3`
  (1.44 s vs 0.17 s for a 29 KB body that expands to ~16 MB of output), with similar peak RSS
  (~320 MB vs ~310 MB). This was not pursued, is not reported as a finding, and is left for
  the owner to judge. (Round 2 measured it: `aa1c98d` roughly doubles it again, RV-8.)
- **oauth2 v0.24 → v0.37** (Graph token handling) and the GODEBUG default changes that come
  with the `go 1.26` directive were not examined.

**Differential lens (listed here as not covered in round 1; completed in round 2, against
`aa1c98d`).** No offensive x/net-vs-browser differential survives `aa1c98d`: on 25 crafted
vectors, x/net under both scripting modes, the Edge 143 DOM and an Edge navigation oracle agree
on 0 live refresh metas and 0 navigations. The false-positive run on ordinary mail found 1 of
36 realistic samples pushed into the escaped-source fallback, and two low findings on what
triggers it (DIFF-FP-1, DIFF-FP-2). Details are under *Differential lens results* below.

All throwaway worktrees, beacon servers and headless Edge processes were removed or killed.
The main working tree was not touched by any finder.

## Re-verification of aa1c98d

**Reviewed:** fix commit `aa1c98d`, which addresses round 1. `hostileSurvives`
(`internal/export/html.go:250`) now re-parses the sanitized output under scripting on **and**
off and fails closed if either reading is hostile (OUT-1). `sanitizeHTML` returns a fallback
reason (`too-deeply-nested`, `unparseable`, `unsafe-after-reparse`) that is carried on
`RenderResult.BodyFallback`, written on the fallback `<pre class="plain"
data-mailarchive-fallback="…">`, recorded as `body_fallback` on the manifest, counted in
`Stats.HTMLFallback` and the CLI Verification line, and listed in the TSV report as
`html-shown-as-source`; on fallback, the `cid:` inline images go into the attachment zip
(INT-2). `readArchivedHTML` falls back to a tokenizer split (`splitAtBodyDiv`,
`internal/app/htmlheader.go:74-119`) when the page is too deep for the tree parser (INT-1), and
indexes a marked fallback page by the `<pre>`'s text, i.e. the mail's source (INT-3). Tests:
MA-300 (extended), MA-303 and MA-304 (new).

Finders and skeptic worked in throwaway worktrees at `aa1c98d`, building comparison binaries
from `066f86d`, `033b1e3` and v0.2.3 inside them, on Go 1.26.9. IDs: the workflow emitted these
findings as `DIFF-DIFF-FP-n` and `RV-RV-n`; this report shortens them to `DIFF-FP-n` and `RV-n`,
the form the finders and the skeptic use in their cross-references.

**12 found / 12 confirmed / 0 refuted.** The skeptic rated them 0 high, 3 medium (RV-1, RV-2,
RV-4) and 9 low; the finder had rated RV-3 medium and the skeptic lowered it, with reasons in
the table. By origin: 4 are introduced by `aa1c98d` (DIFF-FP-1, RV-5, RV-6, RV-9, all low).
3 come from how `aa1c98d` fixed round 1 rather than from a new defect: RV-7 (the INT-1 fix does
not reach old full-document pages), RV-8 (the OUT-1 re-parse doubles an existing cost) and RV-10
(the INT-2 fallback is not visible in `status`, the GUI or the desktop message). DIFF-FP-2 dates
from the x/net bump in `033b1e3`. The remaining 4 (RV-1, RV-2, RV-3, RV-4), including all three
mediums, already reproduce at `066f86d`.

| ID | Lens | Claim | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| DIFF-FP-1 | differential (false-positive cost); introduced by `aa1c98d` | The new two-mode re-parse pushes legitimate mail into the escaped-source fallback (`unsafe-after-reparse`) when the mail puts a **harmless** `<meta charset>` or `<meta http-equiv="Content-Type">` inside `<noscript>`. The fix introduced an asymmetry: `neutralize` (`html.go:316`) walks only the scripting-on tree, where noscript content is opaque raw text, so it can never rename or remove a meta there. `hostileSurvives` (`html.go:250`) then re-parses with scripting off, sees the meta as live markup, and `metaIsCharset`/`hostileEquiv` fail closed. The same metas placed directly in `<body>` are renamed or removed with no fallback (the exporter writes its own utf-8 charset first and overrides Content-Type). So a charset or Content-Type meta inside noscript can never be rendered, only fallen back. | CONFIRMED (low) | Skeptic's PoC test in `internal/export` at `aa1c98d`: `RenderWith` gives `BodyFallback="unsafe-after-reparse"` and a `<pre data-mailarchive-fallback>` for a body `<noscript><meta http-equiv=Content-Type content="text/html; charset=UTF-8"><img src=cid:px@x></noscript>`, a body `<noscript><meta charset=utf-8>`, a head `<noscript>` carrying either meta, an AMP-style head `<noscript><meta charset>`, and a full ESP document inside `<noscript>`. The same metas directly in `<body>` give `BodyFallback=""` (charset removed, http-equiv renamed). Common noscript patterns render normally: a viewport meta, a data: `<img>` pixel, `<style>`. The same test file at `033b1e3` gives no `<pre class="plain">` for the Content-Type or charset case, so `aa1c98d` introduced this. A meta charset of UTF-8 matches the page's own charset, so the fallback guards nothing here. Finder (harness `adv2-differential/cmd/advdiff`): 1 of 36 realistic samples fell back (an AMP email with charset in noscript), and 3 of 6 constructed noscript-meta probes (the three with a charset or Content-Type meta). The skeptic did not re-run the 36-sample corpus; its own probes agree with the split. Low: no data loss (the INT-2 fix puts inline images in the zip and records the fallback on the manifest), and the trigger is uncommon in real mail; it is a rendering-fidelity regression. | `fed6670`, `d787013` (coupling), `c3b26a6` (closing and page checks) | MA-476 `TestRenderNoscriptMetasRenderNeutralized`; MA-492 `TestLiveAndRebuiltIndexAgreeOnHeadNoscriptMarkup`; MA-476/MA-474 `TestRenderKeepsThePageHeaderAndBodyLive` |
| DIFF-FP-2 | differential (false-positive cost); since `033b1e3` (x/net bump), the legibility residual of INT-2 | `sanitizeHTML` (`html.go:163-172`) returns `FallbackTooDeep` whenever x/net v0.60's hard cap of 512 open elements is hit, but a real browser renders far deeper. About 128 nested `<table>` layers (table + implied tbody + tr + td, 4 open elements each) is enough, so ordinary-looking nested-table mail is shown as escaped source. The cap is the one behind INT-1/INT-2, here as a false positive on benign mail rather than a hostile-input concern. | CONFIRMED (low) | Skeptic PoC at `aa1c98d`: N nested `<table><tr><td>` wrappers give `BodyFallback=""` for N = 30, 100, 120, 126, 127 and `"too-deeply-nested"` for N = 128, 129, 130, 150 (4 open elements per table plus html/body passes 512). Plain `<div>` nesting falls back at 511. `033b1e3` already gives `<pre class="plain">` at 128 tables, so this predates `aa1c98d`, which only made it visible. Browser side: a raw 150-nested-table page in headless Edge 143 (JS on) dumps a DOM with all 150 `<table>`/`<td>` and the deep `<p>` as a live element, so the browser renders what the exporter shows as source. Finder: 150 tables fall back, while a 30-deep Outlook-style nested-table newsletter renders normally. Low: the realistic trigger is machine-generated or very deeply forwarded mail. | `30f0838`, `544fa39`, `a397085` (foreign/table on merge) | MA-473 `TestRenderDeepMailRendersFlattened`; MA-477 `TestFlattenedBlocksKeepWordBoundaries`; MA-492 `TestLiveAndRebuiltIndexAgreeOnDeepBlockText` |
| RV-1 | integrity (old-format page, rebuild header forgery); pre-existing, reproduces at `066f86d` | `reindex -rebuild` takes a **forged** subject, From and date from a page written by v0.1.x–v0.2.x for a mail that was a full HTML document. Those releases did not render the mail into the page template: they kept the mail's own document and regex-injected the real header after its first `<body>` tag (v0.2.3 `html.go` `Render`, `reBodyOpen`). If the mail puts `<div class="mailarchive-header">…</div>` inside its own `<head>`, the tree parser closes head at that div and opens body implicitly, so the forged div becomes the first direct child of `<body>`, and `readArchivedTree` (`internal/app/htmlheader.go:148-170`, `firstChildDiv`) reads it. This defeats MA-187's claim that old pages written before the source stripped the namespace are the case it defends; MA-187 tests only a head `<template>`. The finder also claimed that `splitAtBodyDiv` anchors on the real header on these bytes; the skeptic corrected that. | CONFIRMED (medium); secondary claim corrected | Skeptic, own binaries built from git-archive exports. Exported with v0.2.3 one mbox text/html message from `Mallory <mallory@evil.example>`, Subject 'benign skepnewsletter', whose `<head>` holds a `div.mailarchive-header` with `data-mailarchive-field` subject 'skepforged wire approval', from `Chief Executive <ceo@skepbank.example>` and date 'Mon, 01 Jan 2001'. The page keeps the forged div in the mail's `<head>`; the real header follows the literal `<body>`. Live index: `skepnewsletter` = 1, `from:mallory` = 1. After `rm search.db*; reindex -rebuild` with `066f86d` and with `aa1c98d`, both give `skepforged` = 1, `wire` = 1, `from:ceo` = 1, `skepnewsletter` = 0, `from:mallory` = 0, and `search -json` returns subject 'skepforged wire approval', senderEmail `ceo@skepbank.example`, date 2001-01-01. `readArchivedHTML` on the page bytes returns the forged subject and sender with unrecovered=0. MA-187's test (`internal/app/htmlheader_test.go:54`) covers only a head `<template>` and a body-nested header, not a head `<div>` that the tree builder moves into `<body>`. The finder's run (subject 'URGENT wire approval', `adv2-scripts/mkforge.py`) matched, with `unrecovered-fields=0` on both binaries. Correction: on these bytes `splitAtBodyDiv` returns ok=false, because v0.2 full-document pages have no `mailarchive-body` div, so a 600-deep variant recovers nothing (unrecovered=3) rather than anchoring on the real header. Medium, not high (AGG-1 in `review-portability-adversarial.md`, the same forgery on then-current pages, was high): it needs pre-v0.3.0 pages, no `.eml`, and a rebuild, and it forges only the hostile mail's own record, though with the real sender unsearchable. | `e0bcf52` | MA-187 `TestRebuildAnchorsLegacyHeaderNotForgedOne` (erase half: open gap, skipped `TestRebuildCurrentPageRecoversRealHeaderDespiteMimicInHead`, `9f4cb6a`) |
| RV-2 | outside aggressor (offline-inert archive, R19); pre-existing, reproduces at `066f86d` | `<link rel="preconnect" href="http://attacker/…">` in a mail's head or body survives `sanitizeHTML`: `neutralize` rewrites only meta and img (`html.go:316-356`), and CSP has no directive that governs a preconnect hint. In the default posture (JS on), an archived page opened from `file://` makes the reader's browser open a TCP connection to the sender's host, and resolve its DNS name when it is a hostname. That is an open-tracking / IP-leak beacon, the same impact class as OFF-2, despite R19's "no remote loads". | CONFIRMED (medium) | Skeptic's own mbox (`skeppre-*`), exported with `066f86d` and `aa1c98d`. The pages keep `<link rel="preconnect" href="http://127.0.0.1:8799/...">` after the CSP meta, in head and body. Each opened in headless Edge 143 (`--headless=new`, fresh `--user-data-dir` per open, `file://`, JS on) against the skeptic's raw-socket logger: the head and body pages each logged a bare TCP connect on both revisions, 4 of 4. Same-host `dns-prefetch`, `prefetch` and `preload as=image` pages logged nothing, a negative control in the same harness, so the connect is specific to preconnect. A raw remote `<img>` page with no CSP logged `GET /sk/ctl_img`, so the harness was live. The finder got the same 4 of 4 on port 8754 within about 0.5 s of each open (`adv2-scripts/mkpre.py`, `beacon.py`). No conceded non-goal covers link hints. A parser check at `aa1c98d` shows that a `<noscript><link rel=preconnect>` is a live `<link>` only in the scripting-off reading (on: 0, off: 1), so the finder's fix direction (neutralize "on the tree") misses noscript copies. The noscript copy was not verified in a browser: the skeptic's JS-off Edge harness loaded nothing, not even a raw control. | `64b2814`, `b2f120d` | MA-470 `TestRenderResourceHintLinksAreInert`, `TestHostileSurvivesCatchesLiveResourceHints` |
| RV-3 | integrity (search parity, R8); pre-existing in `internal/index/htmltext.go` | The full-text extractor `htmlToText` (`internal/index/htmltext.go:34-66`) is a bare tokenizer with one skip counter. Any `</style>`, `</script>` or `</head>` end tag decrements it, and the tokenizer enters raw-text mode on `<style>`/`<script>` even inside SVG/MathML, where the tree parser and browsers do not. A hostile mail can therefore hide words that the archived page visibly renders from both the live and the rebuilt index, e.g. `<svg width=0 height=0><style><!--</style><script>--></style></svg>` before the text: the tokenizer leaves the style at the comment's `</style>`, enters script data at `<script>` and never leaves it, while the tree keeps the comment whole and Render writes it literally (`escapeComment` does not escape `<`). INT-3's "indexed the same way as the live path" holds, but both paths are blind to this. | CONFIRMED (low; finder: medium) | Skeptic's mbox body `<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please wire funds to account skephidden today</p>`. On `066f86d` and `aa1c98d`, live and after `rm search.db*; reindex -rebuild`: `skephidden` = 0, `wire` = 0, `skepvisible` = 1. Headless Edge 143 (JS on) `--dump-dom` of the `aa1c98d` page has `<p>please wire funds to account skephidden today</p>` as a live sibling after `</svg>` in `div.mailarchive-body`, so the reader sees it. A control that keeps the counter balanced (math>style twice) indexed normally. The finder's run matched (`kestreleleven`, `adv2-scripts/mkhide.py`). Lowered to low: a sender already has trivial ways to evade keyword search (soft hyphens, text in images), the effect reaches only that sender's own message, and nothing on disk is lost. The same tokenizer blindness drives RV-5. | `927c74a`, `8ab1e71`, `1cbf466`, `a397085` (flatten on merge) | MA-488 `TestBodyTextKeepsWordsAfterForeignRawTextTrick`; MA-492 `TestLiveAndRebuiltIndexReadTheSameVisibleWords` |
| RV-4 | integrity (search parity for ordinary mail, R8/R13); pre-existing in `internal/index/htmltext.go` | An ordinary, valid HTML mail that leaves out the optional `</head>` end tag gets **no** body words in the live full-text index: `htmlToText` increments `skip` on `<head>` and decrements it only on `</head>`, `</style>` or `</script>`, so the whole body is skipped. `reindex -rebuild` reads the body div instead and does index the words, so the live and rebuilt indexes disagree on benign mail. | CONFIRMED (medium) | Skeptic's mbox with three messages: `<html><head><meta name=viewport …><body><p>hello skepnoheadword</p>`; the same with `</head>` (`skepwithheadword`); and `<head><title>t</title><body>` with no `</head>` (`skepnoheadtitleword`). Live index on `066f86d` and `aa1c98d`: `skepnoheadword` = 0, `skepnoheadtitleword` = 0, `skepwithheadword` = 1. After `rm search.db*; reindex -rebuild`: all three = 1. The finder's run matched (`kestreltwelve` = 0 and `kestrelthirteen` = 1 live, both 1 after the rebuild; `adv2-scripts/mknohead.py`). Medium: silent loss of searchability on non-hostile mail (R8). | `927c74a`, `0e8d14b` | MA-489 `TestBodyTextIndexesBodyAfterUnclosedHead` |
| RV-5 | integrity (`splitAtBodyDiv` anchoring); introduced by `aa1c98d` as an incomplete INT-1 fix | `splitAtBodyDiv` (`internal/app/htmlheader.go:74-119`) does not anchor "exactly as the tree reader is". The bare tokenizer enters raw-text mode on `<style>`/`<xmp>`/`<plaintext>` and similar even in foreign content, while the tree parser, the sanitizer and Render do not (the parser calls `NextIsNotRawText`; `escapeComment` leaves `<` alone). Head content such as `<template><svg><style><!--</style>…-->` lets the tokenizer see tags that the tree sees as comment text. On a page whose body is ~509 deep (the only pages that reach the tokenizer path), a hostile mail can choose the split point: (a) an early split, so `readArchivedTree` finds no header, subject/From/date are lost, and the real header and transport headers are indexed as body text; (b) `<plaintext>`, giving ok=false and a record with no subject, From, date or body; (c) `<xmp>` plus a body trick, so the words before the split are not indexed (the live index shares the blind spot, RV-3). No **forged** field could be produced: `readArchivedTree` re-anchors on the prefix tree, and mail cannot carry reserved classes. The damage is limited to the hostile mail's own record, but it restores INT-1's "vanishes from search by sender/subject/date" symptom on demand. | CONFIRMED (low) | Skeptic's mbox, exported and rebuilt with `aa1c98d`, bodies nested 509 deep, head payloads "early" (`<template><svg><style><!--</style><body><div class=mailarchive-header>SKFORGED…<div class=mailarchive-body>--></style></svg></template>`) and "none" (`…<plaintext>…`). Live index: `skepearlysubj` = 1, `from:skearly` = 1, `skepnonesubj` = 1, `from:sknone` = 1. The rebuild prints `rebuilt=4` with `unrecovered-fields=6`; afterwards `from:skearly` = 0, `from:sknone` = 0, `skepnonesubj` = 0, and `search -json skepearlyneedle` returns subject "", senderEmail "", date 0001-01-01 and the snippet `… text/html; charset=utf-8 skepearlyword skepearlyneedle` (header text indexed as body). `skforged` = 0 and `from:forged` = 0, so no forged value appears. The 509-deep control and the same early head on a 10-deep page rebuild correctly. The finder's run matched (`rebuilt=5 … unrecovered-fields=6`; unit PoC: `readArchivedHTML` gives unrecovered=3 for early/none at depth 509 and 0 at depth 10). `033b1e3` recovered nothing on any 509-deep page, so this is an incomplete fix rather than a new loss. The skeptic did not test variant (c). | `9516e8f` (reader), `30f0838` (export), `cdd593f` (mimic follow-up) | MA-482 `TestRebuildDeepPageSplitsWhereTheTreeDoes`; MA-474 `TestRenderNeverWritesAPageTooDeepToParse` |
| RV-6 | integrity (reserved fallback attribute on old pages); introduced by `aa1c98d` | The commit says `data-mailarchive-fallback` is an attribute "mail can never carry (stripReservedNamespace)". That holds only for pages written by v0.3.0 or later: `stripReservedNamespace` arrived in `91a7b00`, first released in v0.3.0. Pages written by v0.1.1–v0.2.3 insert a mail fragment raw into the body div, so a mail-supplied `<pre data-mailarchive-fallback="x">decoy</pre>` as the first top-level element satisfies `readArchivedTree`'s check (`htmlheader.go:161-165`, `pre.Parent == body`), and the rebuild indexes only the decoy text. Every other word of the mail leaves search: a regression from `033b1e3`, which indexed the whole body div. | CONFIRMED (low) | Skeptic exported with v0.2.3 a fragment message `<pre data-mailarchive-fallback="x">skepdecoy only</pre><p>the real message says skeprealword</p>`; v0.2.3 writes it raw into `div.mailarchive-body`. Live: `skeprealword` = 1, `skepdecoy` = 1. Rebuild with `066f86d`: 1/1; with `033b1e3`: 1/1; with `aa1c98d`: `skeprealword` = 0, `skepdecoy` = 1. `readArchivedHTML` on the same page bytes returns `HTMLBody="skepdecoy only"`. The finder's run matched (`kestrelsix`, `adv2-scripts/mkold.py`). | `ce48dcb`, `affd96f` | MA-481 `TestRebuildHonoursFallbackOnlyOnRendererLayout`, `TestRebuildLegacyPageNeverHonoursFallback` |
| RV-7 | integrity (INT-1 fix incomplete for old full-document pages; the commit's "(old v0.7.1 pages too)" claim); deep-page part since `033b1e3`, shallow-page part pre-existing | `splitAtBodyDiv` requires `<div class="mailarchive-body">` to be the next element after the header. Pages written by v0.1–v0.2 for full-document mail have no body div (the header was injected into the mail's own `<body>`), so a deep page of that format still recovers nothing on rebuild: `066f86d` (x/net v0.10) recovered its subject and From, `033b1e3` and `aa1c98d` do not. Separately, and pre-existing: every v0.1–v0.2 full-document page rebuilds with no body words even when shallow, because `readArchivedTree` needs a `mailarchive-body` div. That was true at `066f86d` as well. | CONFIRMED (low) | Skeptic exported with v0.2.3 a 10-deep full-document mail (`skepfullsubj`/`skepfullword`/`skepfullneedle`) and a 600-deep one (`skepdeepoldsubj`/`skepdeepoldneedle`); the live index finds all. Rebuild with `066f86d`: `skepdeepoldsubj` = 1, `from:skdeepold` = 1, `skepdeepoldneedle` = 0, `skepfullword` = 0, `skepfullneedle` = 0, `unrecovered-fields=4`. With `033b1e3`: deep page subject/from = 0, unrecovered = 8. With `aa1c98d`: deep page subject/from = 0, body words 0, unrecovered = 6. The 600-deep **fragment** page (`skepdeepfragsubj`) is fixed by `aa1c98d` (1, against 0 at `033b1e3`). A unit call of `splitAtBodyDiv` on the full-document page returns ok=false. The finder's run matched (`deepoldsubj`, `needleseven`/`needleeight`, `adv2-scripts/mkold.py`). | `0c802d7`, `affd96f` | MA-483 `TestRebuildReadsLegacyPageWithoutBodyDiv`, `TestRebuildLegacyPageIndexesContentBeforeHeader` |
| RV-8 | performance / DoS (dual-mode re-parse while the archive lock is held); cost doubled by `aa1c98d`, blow-up pre-existing | `hostileSurvives` now re-parses the whole serialized body twice. On output that x/net's reconstruct-active-formatting step inflates, each re-parse costs roughly O(paragraphs × N²), from Noah's-ark attribute comparisons on every explicit `<b id=i>` in the output. The first parse is cheap, so `aa1c98d` about doubles the CPU that `033b1e3` added, and the cost grows linearly with the mail. The exporter's re-render-without-zip path (every attachment embedded inline) runs `RenderWith` twice, i.e. four re-parses. Pre-existing and larger: the same 160 KB mail already costs 2.4 GB RSS and a 120 MB page at `066f86d`. | CONFIRMED (low) | Skeptic timing test (the same file in each revision's tree), `RenderWith` on `<p>` + 400×`<b id=i>` + `</p>` + n×`<p>x</p>` (66 KB at n=8000, inflating to 50.4 MB of output): `066f86d` 0.41 s, `033b1e3` 3.52 s, `aa1c98d` 6.58 s; at n=2000: 0.14 s, 0.90 s, 1.68 s. A code read of `exporter.go:319-330` confirms the second `RenderWith` when every attachment was embedded. Finder's full CLI export (`-pages=false`, multipart/related with a `cid:` PNG, `adv2-scripts/mkexplode.py`): at n=8000 (68 KB mbox) 0.93 s / 867 MB maxrss, 7.67 s / 1196 MB and 14.46 s / 1253 MB; at n=20000 (160 KB) `066f86d` 2.48 s / 2412 MB against `aa1c98d` 35.99 s / 3238 MB. Ordinary input adds little (5 MB body: 135 → 235 ms, RSS 90 → 133 MB; a 20 MB inline image likewise). The skeptic did not re-measure the CLI export or RSS. | `432509a`, `deb2e9e`, `a9cea49` (CLI line) | MA-471 `TestRecheckReparsesScriptingOffOnlyForNoscript`, `TestExportRendersEachMessageOnce`; MA-472 `TestExportInflatingMailIsBoundedAndRenderedOnce`, `TestSizeCapIsSharedByHeadAndBody` |
| RV-9 | integrity (fallback reason; false positive from the scripting-off parse); introduced by `aa1c98d` | `hostileIn` returns true on **any** parse error (`html.go:261-264`). A benign mail that is shallow with scripting on but deeper than 512 with scripting off, such as a `<noscript>` wrapping deep markup, is shown as source and reported `unsafe-after-reparse`; at `033b1e3` the same mail rendered as HTML. Failing closed is safe, but the report wrongly calls a harmless message unsafe, and the mail loses legibility (R7). | CONFIRMED (low) | Skeptic PoC at `aa1c98d`: `<p>x</p><noscript>` + N×`<div>` + `y` + N×`</div>` + `</noscript>`. N = 100, 505, 508, 509 render as HTML; N = 510 and 600 give `BodyFallback="unsafe-after-reparse"` with `<pre class="plain">`. The same N=510 body at `033b1e3` renders as markup. Mechanism: with scripting on, noscript is raw text and shallow; the scripting-off re-parse passes the 512 cap, and the depth error is reported as hostile. The finder's run matched at N=600. The trigger is rare. | `21756a9` | MA-475 `TestReparseCheckNamesItsFailureTruthfully`, `TestRenderDeepNoscriptRenders` |
| RV-10 | integrity (reporting surfaces; MA-303 "never silently"); incomplete INT-2 fix | Only the CLI's per-run Verification line and the TSV report show a fallback. `status` prints `Incomplete: none` with no pointer to the report (`internal/health/health.go:572-581` counts only fillable, terminal and unknown). The GUI's `exportSummary` (`cmd/mailarchive-gui/main.go:1144-1185`) and the desktop capture message (`internal/desktop/capture.go:146`) never mention `HTMLFallback` and show the report path only for fillable/terminal gaps. On a later incremental run the CLI line drops it too, because it counts "this run" only. README's report-kind table (`README.md:860-866`) does not list `html-shown-as-source`. INT-2's fix direction asked for a note that `status`/`verify` can see. | CONFIRMED (low) | Skeptic's mbox with one 600-deep text/html message, exported with `aa1c98d`. Run 1 prints `Verification: … 1 HTML message(s) shown as source this run … — details in …/attachments-report.tsv`, and the TSV has the row `html-shown-as-source` / `info` / … / `too-deeply-nested`. `status -out` then prints `Incomplete: none` and nothing about the fallback or the report. An incremental run 2 prints no Verification line at all, though the TSV still lists the row and the manifest keeps `"body_fallback":"too-deeply-nested"`. `git grep` finds no reference to `HTMLFallback` or `BodyFallback` in `cmd/mailarchive-gui`, `internal/desktop` or `internal/health`. R1 is met (the TSV regenerated from the manifest lists the fallback), so this is a discoverability and documentation gap. GUI and desktop: code read only. | open | open |

### Fix directions (from the skeptic) and tombstones to write

Every tombstone below must be RED at `aa1c98d` and GREEN after its fix, except RV-8's, which is
a cost guard.

- **DIFF-FP-1.** Keep the dual-mode re-check as it is; it is mandatory (see OUT-1 above). When
  it trips, retry `sanitizeHTML` once with the **main** parse at scripting off
  (`xhtml.ParseWithOptions(…, xhtml.ParseOptionEnableScripting(false))`), which makes noscript
  content markup that `neutralize` can rename or remove. Accept the result only if
  `hostileSurvives` passes again under both modes; otherwise fall back as now.
  *Tombstone (MA-300 family):* a noscript-wrapped `<meta charset=utf-8>` and Content-Type meta,
  in body and in head, render as HTML with the meta neutralized under both parses and
  `BodyFallback == ""`. The existing noscript-refresh cases must still be neutralized or fall
  back.
- **DIFF-FP-2.** Don't fall back on depth alone. Before the main parse, flatten past a safe
  depth, as Chromium does at 512: a tokenizer pass drops the start/end tags of non-void
  containers once the running depth passes ~480, keeping their text and void elements. Then
  parse, neutralize and run the dual-mode re-check as usual, so safety still rests on the
  re-check, and fall back only if the flattened output still fails. At minimum, show an
  extracted-text rendering (`index.BodyText`) above the escaped source so the mail stays
  legible. *Tombstone:* a 150-nested-table mail renders as HTML with its text, the re-check
  passes, and `BodyFallback` is empty. (Editor's note: a bare tokenizer pass has the
  foreign-content raw-text blindness behind RV-3 and RV-5, which is acceptable here only
  because the re-check, not the pass, carries safety.)
- **RV-1.** On a page the old renderer wrote, anchor the header where v0.1–v0.2 injected it:
  take the first match of the old `reBodyOpen` (`(?i)<body[^>]*>`) and require
  `\n<div class="mailarchive-header">` immediately after it in the bytes. Read that header
  (parse the substring up to its matching `</div>`) and ignore any header-classed div earlier
  in document order. If the bytes don't match, count the core fields as unrecovered instead of
  trusting the first direct-child div. The skeptic proposed recognizing an old page by the
  absence of `data-mailarchive-field` markers; note that the forged header in this repro
  itself carries `data-mailarchive-field` attributes, which a pre-v0.3.0 page did not strip,
  so the old-page test must look at the bytes after the literal `<body>`, not at markers
  anywhere in the page. *Tombstone (extend MA-187):* a v0.2.3-format fixture with the forged
  `<head><div class=mailarchive-header>` rebuilds with the real subject, sender and date, and
  `search from:ceo` returns 0.
- **RV-2.** In `neutralize`, rename `href` (to `data-mailarchive-neutralized-href`) on every
  `<link>` whose `rel` contains preconnect, dns-prefetch, prefetch, preload, modulepreload or
  prerender; simpler and also safe, on every mail-supplied `<link>`, since CSP already blocks
  every fetch a `<link>` could legitimately make. Also extend `hostileIn` so that a live
  `<link>` with `href` and any such `rel`, in **either** scripting reading, fails closed; that
  covers noscript copies, which the scripting-on tree cannot see. *Tombstone:* a unit test
  with no live resource-hint `<link href>` in the output under both parse modes, for head,
  body, template and noscript placements, plus a browser leg (Edge and a beacon, 0 connects).
- **RV-3.** Make `htmlToText` foreign-content aware: track an svg/math stack (respecting HTML
  integration points and breakout tags) and, inside foreign content, call
  `z.NextIsNotRawText()` after a raw-text start tag; keep a per-element skip stack, so only
  `</style>` closes a style skip and only `</script>` closes a script skip. Better: extract
  text from the x/net tree and use the tokenizer only when the tree parse hits the depth cap.
  *Tombstone (R8):* this payload makes `search skephidden` return 1 on the live index and
  after a rebuild.
- **RV-4.** Stop skipping on `<head>`: head's only text-bearing children are title, style,
  script and noscript, so skip title, style and script contents by their own end tags; or end
  the head skip at the first `<body>` or body-content start tag. A tree-based extractor (RV-3)
  fixes both. *Tombstone (R8):* a head-without-`</head>` mail is found on the live index and
  after a rebuild with the same hit count.
- **RV-5.** Check each candidate split with the tree parser instead of trusting the
  tokenizer. For each `<div class="mailarchive-body"` start tag the tokenizer finds, in order,
  parse prefix + `<div class="mailarchive-body" data-mailarchive-sentinel></div></body></html>`
  and accept the first candidate where the sentinel is an element, a direct child of `<body>`,
  and the next element sibling after the first direct-child `div.mailarchive-header`; a split
  that ends inside a comment or raw text swallows the sentinel as text and is rejected. (The
  finder's alternatives: track foreign content in `splitAtBodyDiv`, or recognize only the
  renderer's own `\n</head>\n<body>\n<div class="mailarchive-header">` byte sequence.)
  Separately, close INT-1(b) at export: fall back when the final assembled page is too deep to
  parse, so new pages never need the tokenizer path. *Tombstone:* the early and none payloads
  at depth 509 rebuild with subject, From and date recovered (unrecovered=0).
- **RV-6.** Honor `data-mailarchive-fallback` only when the page matches exactly what the
  renderer writes: the `<pre>` is the **only** non-whitespace child of `div.mailarchive-body`
  (no other element and no non-whitespace text), with `class="plain"` and a value in the known
  set {`too-deeply-nested`, `unparseable`, `unsafe-after-reparse`}. Otherwise index innerHTML
  as before. *Tombstone:* the v0.2.3-format decoy fixture rebuilds with `skeprealword` = 1,
  and an `aa1c98d` fallback page still indexes by its words (MA-304).
- **RV-7.** Support the legacy layout in both readers. In `readArchivedTree`, when no
  direct-child `div.mailarchive-body` exists but a header does, take the innerHTML of
  `<body>`'s children after the header as `HTMLBody`. In `splitAtBodyDiv`, when the token after
  the header's end is not the body div, split at the header end and return the rest of
  `<body>` as the body bytes, with the header anchored as in the RV-1 fix. *Tombstone:*
  v0.2.3-format full-document fixtures at depth 10 and 600 rebuild with subject, From, date and
  body needles all found.
- **RV-8.** Skip the scripting-off parse when the serialized head+body has no
  case-insensitive `<noscript`: the flag changes only noscript handling, so the two parses
  would be identical. In the exporter, don't call `RenderWith` twice: keep the sanitized head
  and body from the first call and re-render only the header and template with the zip link
  removed. Bound the pre-existing blow-up: if the serialized body is larger than K× the input
  or an absolute cap (say 16× or 32 MB), fall back with a new reason `too-large`.
  *Tombstone:* a timing/size guard on the 400×`<b>` body (output cap enforced, one
  `RenderWith` call per message).
- **RV-9.** Have `hostileSurvives` return a reason as well as a bool: a depth-cap error (the
  error text contains "exceeds") in either reading maps to `FallbackTooDeep`, not
  `FallbackHostile`. Better still, the DIFF-FP-2 flattening and the DIFF-FP-1 scripting-off
  retry let a benign deep noscript render. *Tombstone:* this body records
  `body_fallback="too-deeply-nested"` (or renders, once flattening lands).
- **RV-10.** In health/`status`, count manifest records with `BodyFallback` and print e.g.
  `Shown as source: N (content kept) — <report path>` (and the same for unresolved inline
  images). In the GUI's `exportSummary` and the desktop capture message, mention
  `Stats.HTMLFallback` and the report path when it is above 0. In the CLI, point at the report
  whenever it has rows, even when nothing changed this run. In the README, add the
  `html-shown-as-source | info | …` row. *Tombstone:* after a 600-deep export, `status` output
  names the fallback count and the report path, and `exportSummary`'s text includes it.

### Round-2 fixes

The round-2 findings were fixed on three parallel branches and integrated on
`wip/sanitizer-r2-integrated` (merges `4e4db2f` export, `de3f2f2` reader,
`8f900c9` index), with the cross-branch problems closed by the integration
commits below. The "Fix commit" and "Tombstone test" columns above name each
finding's commits and guards; all commit ids are on that branch. **The verdict
stays FINDINGS OPEN:** these fixes were attacked in round 3 (see the end of this
report, and the header for what is still open), and RV-10 is still open.

Fixed (11 of 12): DIFF-FP-1, DIFF-FP-2, RV-1, RV-2, RV-3, RV-4, RV-5, RV-6,
RV-7, RV-8, RV-9. **Still open: RV-10** (the fallback is not surfaced in
`status`, the GUI or the desktop message; its owner is the status branch — the
CLI Verification line was corrected to name every reason in `a9cea49`, but the
status/GUI/desktop visibility is unchanged).

Cross-branch problems closed during integration (each prove-fail → prove-pass,
recorded in its commit):

- **Merge made MA-492 RED (flattener).** `flattenDeep` dropped an `<svg>`/
  `<math>` container past depth 480 but kept a `<style>`/`<script>` inside it, so
  the rendered page and the rebuilt index hid the body after `</svg>`, and a
  dropped `<td>` glued adjacent cell words. Fixed in `a397085`: the flattener
  keeps the current node's namespace known in the dropped region (a full drop
  stack), so a raw-text element inside a dropped SVG/MathML is a foreign element
  (and a stray `</p>`/`</br>` breaks out), and a dropped table's cells leave a
  word boundary. Guards: MA-492 `TestLiveAndRebuiltIndexReadTheSameVisibleWords`,
  `TestLiveAndRebuiltIndexAgreeOnDeepBlockText`; and, for a table part that
  arrives while a kept table is open but the current node is dropped (pinned by
  no test until the fresh-eyes review's item 5), MA-473/MA-492
  `TestLiveAndRebuiltIndexAgreeOnTablePartsPastTheCap`.
- **DIFF-FP-1 coupling.** The scripting-off retry moved a head `<noscript>`'s
  flow content into the body, so a word the live index skips (an explicit head)
  was in the rebuilt index only (R8). Fixed in `d787013`: the retry neutralizes
  each noscript's content in place (scripting-off fragment parse) instead of
  re-parsing the whole document with scripting off, so the document keeps its
  scripting-on structure and a head noscript's content stays in the head. As
  `d787013` wrote it, that held only where the rewritten content was written
  back as one closed noscript: x/net writes nothing after a `<plaintext>` (not
  even the `</noscript>`), and a script left in an escape state by
  `<!--<script>` runs past it with scripting off, so a rewritten head noscript
  could swallow the page's own header and body — a hostile mail blanked its
  page and erased its own record on `reindex -rebuild` (the fresh-eyes review
  of the integration, item 1: 9 of the 372 adversarial vectors,
  unrecovered-fields=18). Since `c3b26a6` a rewrite is kept only where it closes
  itself in both readings (otherwise the noscript keeps its raw text), and every
  sanitizing pass is accepted only with a page whose header and body are live
  directly in `<body>` in every reading. Guards: MA-492
  `TestLiveAndRebuiltIndexAgreeOnHeadNoscriptMarkup`; MA-476/MA-474
  `TestRenderKeepsThePageHeaderAndBodyLive`; MA-187/MA-492
  `TestRebuildHostileHeadMarkupCannotEraseItsRecord`.
- **Forged header hidden in a head `<noscript>` (reader review issue 6).**
  Reserved-namespace stripping ran only on the scripting-on tree, so a head
  `<noscript>` holding `<body>` and a `mailarchive-header` div reached a JS-off
  reader as a forged tool-styled header. Fixed in `cbc9047`: the re-check fails
  closed on a live element carrying a reserved marker neutralize did not add, in
  either reading, so the retry strips it. Guard: MA-496
  `TestReservedMarkerInNoscriptIsStripped` (since the fresh-eyes review's item 2
  with an oracle of its own and each marker kind forged alone).
- **Implied-head drift (index review issue 5).** A fragment leading with a
  text-bearing `<noscript>`/`<noframes>` was indexed live only. Fixed in
  `7f2fd05`: `reindex -rebuild` wraps the body div's inner HTML in `<body>`, and
  the extractor skips every head (explicit or implied). Guards: MA-491, MA-492
  `TestLiveAndRebuiltIndexAgreeOnHeadsAroundNoscript`.
- **Live/rebuild word boundaries (index review).** `index.blockElements` was
  widened to the flattener's block set (`0f824b9`), so dt/dd/dl/pre/address/…
  split words the same on both indexes (a better ordinary-mail word boundary).
  Guards: MA-477 (now asserts the index too), and
  `TestFlattenerAndIndexSplitWordsAtTheSameNames`, which pins the two sets equal
  name for name (the fresh-eyes review's item 3).

Open owner questions (listed in the integration report, not decided here):

- **MA-187 erase (operator ruling pending).** A current page whose head raw text
  mimics an old header's bytes has its real subject/From/date erased (never
  forged). The settled resolution needs an out-of-band writer-version signal on
  new pages (internal/state + internal/export); pages already on disk from
  v0.3.0 to aa1c98d stay an open gap. Encoded as a skipped guard
  (`TestRebuildCurrentPageRecoversRealHeaderDespiteMimicInHead`, `9f4cb6a`) that
  fails if un-skipped today.
- **DIFF-FP-1 option (a).** The live indexer could instead read exactly what the
  exporter rendered; deferred because it touches internal/app capture code the
  g7 branch is also changing. The in-place retry (option b) was taken.
- **RV-2 Link pragma** (keep or drop disarming `<meta http-equiv=Link>`) and
  **RV-10 wording/surfaces** remain for their owners.

Stated residuals carried from the branch work (not regressions):

- **RV-8 memory:** the main parse still builds the inflated tree before the size
  cap can stop serialization; the cap removes the quadratic re-parses, the
  multi-megabyte page and the double render, not the peak tree memory.
- **Fallback still reachable:** mail whose formatting elements the tree builder
  re-opens out of the flattener's sight (`reopenedBold`) still falls back, and
  the final page check catches a page one level past the cap.
- **Deep-path fallback extractor** (bodies past 512 open elements) does not model
  every table-insertion / adoption-agency case; a fuzz corpus finds a small
  number of contrived hides, some regressions against the pre-bump tokenizer,
  listed in the `tokenText` comment. Safety rests on the re-parse, not the pass.
- **No JS-off browser check** of the noscript link/meta copies; the scripting-off
  x/net reading is what covers them.

Open gap found by the fresh-eyes review's corpus run (not a regression of these
fixes: already present at the merge `8f900c9`; open, not accepted, and for the
round-3 pass): on rv3/corpus-adv, after `c3b26a6`, 8 of the 372 mails still
have words found live and rebuilt a different number of times (R8). A
`<plaintext>` that runs to the end of the mail
(adv_plaintext_{body,head}_{esc,lnk,ref}_b) also swallows the page's own
closing tags, whose names the rebuild indexes; and a `<noscript>` nested past
`flattenDepth` (adv_deep_xmp, adv_deep2_template) loses its tags to the
flattener, so an `<xmp>` or `<template>` straddling the noscript's raw-text end
reads differently on the page. Encoded by the skipped MA-492
`TestLiveAndRebuiltIndexAgreeOnCorpusResidualShapes`, which failed if un-skipped.
**CLOSED in round 3 (san-Q2, `6bea1cd` and `9f6a850`): the encoder passes and its Skip is
removed.**

### Differential lens results

**Offensive differential: none survives `aa1c98d`.** The finder exported 25 crafted vectors
through `export.RenderWith` in a worktree at `aa1c98d` and compared the x/net v0.60.0 tree,
under scripting on **and** off, with the DOM of Microsoft Edge 143, then checked navigation
against a local beacon. Every vector carried a `<meta http-equiv=refresh>` aimed at the beacon.
The vectors: raw-text breakouts (noembed, noframes, iframe, xmp, style, plus nested
noembed>noscript and noscript>noscript); foreign content (svg script/style/desc/title/
foreignObject; math `annotation-xml` with `encoding=text/html` and
`encoding=application/xhtml+xml`; CDATA in svg and in svg>style); a meta foster-parented out of
a table; a meta in `<select>`; `<template>`; content after `</body>`; a mail `<head>` with an
unclosed `<style>`; and a `meta name=referrer` relaxation. Three oracles agree:

1. x/net under both modes on the exported bytes: 0 live refresh metas in all 25 files. The 2
   noscript-hostile vectors fall back to escaped source.
2. Edge with JS on (`--dump-dom`): all 25 inert, with zero live meta **elements**, so Edge and
   x/net agree. The finder's breakdown covers 23 of them: 11 neutralized
   (`data-mailarchive-neutralized`), 6 with the meta as a text child of a raw-text element
   (noembed, iframe, xmp, noframes and style content is text to Edge exactly as to x/net),
   4 escaped source, 1 harmless referrer meta, 1 otherwise inert. The other 2 were not
   categorized.
3. Edge navigation over the 25 files with JS on and off, 50 renders: zero navigations to the
   beacon, not even a favicon.

*Oracle validity.* `--dump-dom` neither completes subresource loads nor reliably fires a
meta-refresh navigation, and with `--blink-settings=scriptEnabled=false` it returns an empty
DOM (serialization needs scripting), so a `--dump-dom` beacon test is a false oracle; the
re-verifier found the same. The navigation oracle used old `--headless` with `--screenshot` and
`--virtual-time-budget`, validated with controls on a fresh beacon: a raw `<meta refresh>`
navigates with JS on and with JS off; a refresh inside the archive's own CSP also navigates
(CSP `default-src 'none'` does not stop a meta refresh, which is why the tool must neutralize
or fall back); a neutralized meta does not navigate. **Limit:** in this harness a
`<noscript><meta refresh>` never navigated with JS off, and the finder guessed that
`--blink-settings=scriptEnabled=false` does not flip the parser's scripting flag in this Edge
build. That guess is contradicted: in round 1 the finder and the skeptic both logged JS-off
hits from noscript refreshes, and in round 2 the re-verifier's JS-off positive control (the
same noscript mail exported by `033b1e3`) logged `GET /b/ns_body_033b1e3`. The cause was not
established. So the differential lens's JS-off result has no working noscript positive control;
JS-off coverage of the `aa1c98d` noscript pages rests on the re-verifier's browser check (see
*What held* below) and on the x/net scripting-off oracle.

*Why it holds (finder's analysis).* The HTML scripting flag is the only parse divergence that
matters here, and it governs only `<noscript>`; the dual-mode re-parse matches a JS-off
browser's noscript-as-markup reading and fails closed, closing OUT-1. The other raw-text
elements (noembed, noframes, iframe, xmp, style, script) do not depend on scripting and are
parsed the same by x/net and Edge. The foreign-content integration points (svg desc, title and
foreignObject; math `annotation-xml` text/html) surface the meta as `atom.Meta`, so
`neutralize` defangs it, and CDATA and foreign text are escaped. The CSP meta is always head
child #2 (after the charset) and precedes all mail head content, so mail cannot displace it. A
mail `<meta name=referrer content=unsafe-url>` survives live (it is not in `hostileEquiv`) but
is harmless: there is no HTTP request to leak a referrer on, and `file://` origins send no
referrer to http. (RV-2 is the one exception to "CSP blocks every remote load": a preconnect
opens a bare TCP connection, but it sends no request, so the referrer conclusion stands.)
`go test ./internal/export ./internal/app` was green at `aa1c98d`.

**False-positive cost (does ordinary mail fall into the fallback?).** The finder rendered a
36-sample corpus of realistic mail through `export.RenderWith` at `aa1c98d`: Outlook/Word, a
Gmail reply with signature, Apple Mail, Mailchimp/HubSpot/Litmus newsletters, inline SVG (icon,
title/desc, foreignObject, gradient), inline MathML with `annotation-xml encoding="text/html"`,
a noscript tracking-pixel `<img>`, and noscript style and markup fallbacks. Exactly 1 of the 36
fell back to escaped source: an AMP-style email with `<meta charset>` inside `<noscript>`
(DIFF-FP-1). Everything else rendered as HTML, including the common noscript patterns (pixel
`<img>`, `<style>`, a markup block, a viewport meta). Of 6 constructed noscript-meta probes,
the 3 that carry a charset or Content-Type meta all fell back. The causes of a benign fallback
found in both runs are a charset or Content-Type meta inside `<noscript>` (DIFF-FP-1), more
than ~512 open elements, which about 128 nested tables reach (DIFF-FP-2), and, found by the
re-verification, a `<noscript>` that is only too deep when read with scripting off (RV-9). The
corpus was chosen by hand, not drawn from real traffic, so 1 in 36 is not a fallback rate; the
skeptic did not re-run it, but its own probes agree with the split.

### What held

**Re-verification (integrity and outside aggressor):**
- Unfiltered `go test ./...` on Go 1.26.9: all 26 packages ok (`procs` has no tests).
  `go test -race -count=1 ./internal/export/ ./internal/app/`: ok. gofmt is clean on the
  changed files, and `go vet` is clean on export, app, state and `cmd/mailarchive`.
- The commit's prove-fail claims, spot-checked: making `hostileSurvives` scripting-on only
  turns MA-300 RED (`noscript body/head/nested/svg fo: … [refresh (scripting=false)]`), and
  disabling the `splitAtBodyDiv` branch turns MA-304 RED
  (`post-rebuild "needleepsilon" = 0, want 1` and `deep509 = 0`).
- The OUT-1 fix in a real browser: headless Edge 143, JS off, no `--dump-dom`, kept alive 6 s
  against a TCP beacon. The `aa1c98d` noscript pages gave no hits: body, head, nested div, svg
  foreignObject, the escaped twin, and a `<noscript></div><meta refresh>` variant. All but the
  escaped twin fell back as `unsafe-after-reparse`. Positive controls fired: a raw refresh page
  with JS off, and the same noscript mail exported by `033b1e3` with JS off
  (`GET /b/ns_body_033b1e3`). The JS-on runs gave no noscript hits. A `--dump-dom` JS-off
  harness is not valid: it exits before navigation, and the raw-refresh control did not fire
  under it.
- The fallback attribute on current (v0.3+) pages: mail cannot carry it, because
  `stripReservedNamespace` runs on every tree element, and noscript/xmp/style raw text stays
  text under the rebuild's scripting-on parse. `bodyAttributes` copies only
  style/class/dir/lang/bgcolor. Plain-text pages carry no attribute, so they are unchanged.
  (Pre-v0.3.0 pages: RV-6.)
- Forged fields on current pages: no way was found to forge subject/from/date through
  `splitAtBodyDiv`. `readArchivedTree` re-anchors on the prefix tree, the header between
  `<body>` and the body div is renderer-escaped, and the tokenizer's div-depth count is sound
  there. (Old-format pages: RV-1. Erasure rather than forgery: RV-5.)
- Whitespace and raw text in the head: TrimSpace versus HTML whitespace is moot, because only
  the template's "\n" sits between `<body>`, the header and the body div. Head `<style>`,
  `<title>`, `<noscript>` (scripting on) and `<script>` tokenize identically in the bare
  tokenizer and the tree. (Foreign content in the head is the exception: RV-5.)
- The inline-image double read: every source re-reads. mbox, maildir, Graph and Evolution go
  through `source.ParseMIME` and `bytesWriterTo`, and go-pst v6.0.2 `Attachment.WriteTo` builds
  a fresh property reader on each call. On fallback, consumed={} keeps hasArchivable true, so
  there is no re-render without the zip link; the header lists every attachment, and the zip
  and the `inline=` count are honest. Note: `model.Attachment`'s doc comment says `WriteTo`
  "may be called at most once per intended output". The commit, and the older double render,
  rely on the opposite, which holds for today's sources.
- Re-parse modes: `ParseWithOptions` with scripting on is the same code path as `Parse`, and
  x/net v0.60 implements `inHeadNoscriptIM` and the in-body scripting-off noscript path.
- `body_fallback` on the manifest: there is exactly one Record construction, and every
  mutation path copies the whole struct, so Load, MergeFields, Resolve and the v1/v2 migrations
  keep the field. In `HasIssues` the only side effects are that Subject/Date are now stored on
  fallback records and that the record appears in `Issues()` and the report; Resolve and Counts
  are unaffected. An older binary drops the field on rewrite, which falls under the README's
  "upgraded binaries only" rule.
- Cost on ordinary bodies: a 5 MB plain body and a 20 MB inline image add little. (Hostile
  bodies: RV-8.)
- Rebuild of new fallback pages: 600-deep pages index the mail's words (`div` = 0 hits),
  509-deep pages recover their header, and 600-deep v0.2.3 fragment pages are fixed as well.

**Differential:** see *Differential lens results* above.

**Not covered by round 2** (nothing here counts as a negative result):
- oauth2 and Graph, and the GODEBUG defaults of the `go 1.26` directive (still open from
  round 1).
- A PST fixture end to end: the go-pst re-read was checked by code only.
- Chromium 143's customizable-select parsing against x/net's handling of `<noscript>` inside
  `<select>` (a lead from the re-verifier; the differential lens tested a meta in `<select>`,
  not a noscript there). The re-verifier's other lead, Chromium's 512-depth flattening, became
  DIFF-FP-2.
- RV-2's noscript copy in a JS-off browser (no JS-off harness that loaded anything), RV-5's
  variant (c), RV-8's CLI and RSS numbers, and the 36-sample corpus were not re-run by the
  skeptic. RV-10's GUI and desktop legs are a code read.

The re-verifier's worktree was removed with `git worktree remove --force`; all beacons and
headless Edge processes were killed, and the main working tree was not touched. Scripts, mboxes,
archives, beacon logs and binaries are in the session scratchpad (`adv2-scripts/`,
`adv2-data/`, `out-*`, `adv2-bin/`), with the differential harness in
`adv2-differential/cmd/advdiff`.

## Round 3 (`wip/sanitizer-r2-integrated` at `829f51d`)

**Scope.** The adversarial pass owed by the rulings log's §9 row 1 (FX-SAN) before the sanitizer
branch merges: everything on `wip/sanitizer-r2-integrated` since `176c4b2`, the base it shares
with `fix/review-2026-10` (42 commits; `git diff 176c4b2..829f51d -- internal/ cmd/`). That is
the round-2 fixes on three branches, merged as `4e4db2f` (export: link hints, render once and
too-large, flatten past 480, re-check reasons, noscript retry), `de3f2f2` (reader: old-page header
anchoring, fallback layout, tree-validated deep split, legacy full documents, UTC-only stamps) and
`8f900c9` (index: tree-walk extractor, depth-cap fallback tokenizer, noscript as markup,
live/rebuild parity guards), and the integration fixes `a397085` to `829f51d` (the in-place noscript
retry `d787013` with `fd28e01` and `c3b26a6`'s structure check, `cbc9047`'s reserved markers,
`0f824b9` and `e8e6129`'s block set, `7f2fd05`'s implied head, `a9cea49`'s Verification line,
`9f4cb6a`'s MA-187 encoder, `829f51d`'s R8 encoder). Rulings san-Q1 (the retry also runs on a
failed page check; stray noscript end tags under SVG/MathML integration points are refused) and
san-Q2 (the two R8 shapes left after `c3b26a6`) name two of the things attacked.

Three lenses, each a finder followed by a refutation-default skeptic who reproduced the claim with
a harness of its own (the same finder → skeptic process as rounds 1 and 2):

- **Outside aggressor (hostile sender against export):** crafted mail through `export.RenderWith`
  and the CLI, read in BOTH scripting readings with x/net (the oracle of rounds 1 and 2, which
  agreed with Edge 143 there) plus grammar fuzzing of noscript/head/svg/plaintext shapes (about
  48M mails). No browser was run.
- **The rebuild forger (hostile mail and legacy pages against `reindex -rebuild`):** forging and
  erasing subject, sender and date on current and old-format pages, the fallback attribute, the
  date stamp readers, the deep-page split (about 1.45M fuzzed mails).
- **Integrity and the differential (live index against rebuilt index):** export → `reindex
  -rebuild` → per-term counts through `fts5vocab`, page layout in both readings over 612 pages,
  cost, the deep-flatten differential (about 3.5M fuzz cases).

**Round 3: 14 found / 8 confirmed / 0 refuted** (5 duplicates and 1 non-goal counted separately;
the skeptic rated the 8 confirmed 0 high, 1 medium and 7 low, the finders' own ratings unchanged).
Counting each defect once, R3-AF3 stands for three reports (R3-AO3, R3-AC4), R3-AC5 for two
(R3-AF5) and R3-AC2 for two (R3-AF4): 7 distinct confirmed defects. **Fixed: 7 of the 8
confirmed** (R3-AO1, AF3, AF4, AC2, AC3, AC5, AC6; with them the duplicates R3-AO3, R3-AC4 and
R3-AF5) and both san-Q2 shapes. **Open: R3-AF2** (low; its fix changes the page's sender format, an owner
decision: a skip encoder cites MA-187). The duplicates R3-AF1 (MA-187's erase half, FB-1/FX-187)
and R3-AC1 (MA-492's outer-noscript escaped text; the owner question on MA-476's escaped twin is
not yet in the rulings log) are the known
open gaps, unchanged; R3-AO2 is the stated policy (rulings: "whether the re-check should fail
closed on any live mail `<link href>`" is an open question).

| ID | Lens | Claim (file:line) | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| R3-AO1 | outside aggressor | A stray `</div>` in a body `<noscript>` makes a meta refresh, preconnect, second charset or forged header live for a reader with JavaScript off. `hostileSurvives` (`internal/export/html.go:1210`) re-checks the sanitized body in a bare `<body>` with no `div.mailarchive-body` around it, so the stray `</div>` is inert there and a later `<plaintext>` swallows the payload as text; in the page, with scripting off, it closes the noscript and the page's own body div, `</noscript>` no longer closes an `<svg>` opened inside it, `<plaintext>` becomes an SVG element and `<meta>` breaks out. `pageFallback` (`:176`) tested only header/body anchoring and `sanitizeChecked` (`:320`) accepted the first pass on that basis | CONFIRMED (medium) | Body `<p>x</p><noscript></div><svg></noscript><plaintext><meta http-equiv=refresh content='0;url=http://e.test/r'>`: no fallback, no Verification line, the page keeps the bytes verbatim; pagecheck (x/net, both readings) reports `meta:refresh(scripting=false)`, scripting on is clean. Variants `<math>`, `<svg><textarea>`, `<xmp>`, a full ESP document, `</div></div></div>`, `<link rel=preconnect>` (`hint(scripting=false)`), a second charset and a forged `<div class=mailarchive-header data-mailarchive-field=from>` (`hdrs=2(scripting=false)`) all reproduce. Control: a harmless `<noscript><meta charset=x></noscript>` first makes the retry rewrite every noscript and neutralize it. JS-off readers only; a JS-on reader and the rebuild are unaffected. Same class as round 1 OUT-1 (its check document has the same shape). Reproduced in-process and end to end by the skeptic; no browser | `1406fe8` | MA-300/MA-474 `TestRenderStrayDivInNoscriptCannotMakeBreakoutLive` (RED at `829f51d`: `svg plaintext: a live hostile meta ... [refresh (scripting=false)]`, `resource hint ... [preconnect http://e.test (scripting=false)]`, `second charset ... got [1 2]`, `forged header (reading 1) ... got 2 and 1`) |
| R3-AO2 | outside aggressor | A non-hint `<link href>` (`rel=icon`, `manifest`) inside a `<noscript>` stays live for a reader with JavaScript off: `hostileIn` fails closed only on resource-hint rels (`html.go:1176-1187`, `:1299-1301`), and the noscript copy is raw text to `neutralizeLink` (`:1624-1644`) | NON-GOAL | Parser-level reproduction is true (`<p>x</p><noscript><link rel=icon href=http://e.test/i.ico></noscript>` exports with no fallback and a live `link-href:icon` with scripting off). But it is the stated policy: `neutralizeLink`'s own comment says a noscript copy fails closed only for a hint and "any other link type there keeps its href in that reading, its fetch refused by the policy" (ArchiveCSP `default-src 'none'; img-src data:; font-src data:`), the round-2 report lists the missing JS-off browser check of the noscript link/meta copies as a residual, and the rulings log lists "whether the re-check should fail closed on any live mail `<link href>`" as an open question, not ruled. No request was shown (no browser; a favicon fetch from the browser process outside the page policy is a suspicion only). The one network effect CSP cannot govern, the hints, is already closed (RV-2). Cheap to close if the owner wants it | none (owner question) | none |
| R3-AO3 | outside aggressor | A `<frameset>` mail archives an empty page with no fallback (`html.go:412-419` bodyNode nil) | DUPLICATE of R3-AF3 | Same defect; reproduced: `<div class="mailarchive-body">\n\n</div>`, `BodyFallback ""`, clean pagecheck | `36cfa22` | `TestRenderFramesetMailIsShownAsSource` |
| R3-AF1 | rebuild forger | MA-187's erase half is open for every page this release writes; a head `<style>`/`<script>` carrying the old header's bytes makes the rebuild read a current page as a legacy one (`internal/app/htmlheader.go:244` `legacyHeaderSpan`, `:271` `anchorHeader`); a writer-side refusal in `pageFallback` (`html.go:176`) would close it for new pages | DUPLICATE of the MA-187 erase-half open gap | Reproduces (CLI: unrecovered-fields=3, empty subject and sender, date 0, nothing forged) but it is the known, ruled, encoded gap: the catalog's MA-187 row, the skipped `TestRebuildCurrentPageRecoversRealHeaderDespiteMimicInHead` (`internal/app/rebuild_mimic_test.go:137`) and ruling FB-1 (an out-of-band writer signal, FX-187, with "erase, never forge" kept until then). The finder's writer-side guard is an alternative to FB-1's choice, so the owner's call; it would not cover pages already on disk. Unchanged by this round (the `erase` corpus: unrecovered=3 before and after) | none (FX-187) | the skipped MA-187 encoder (unchanged) |
| R3-AF2 | rebuild forger | Rebuild re-splits sender names on the last ` <`, so a quoted local part holding ` <` rewrites or swaps the indexed sender (`internal/app/htmlheader.go:579` `splitSender`, `internal/export/html.go:1945` `formatSender`) | CONFIRMED (low) | Reproduced end to end with real mbox parsing. `From: Mallory <"x <ceo"@bank.example>`: live ("Mallory", `x <ceo@bank.example`), rebuilt ("Mallory <x", `ceo@bank.example`); `From: "ceo@bank.example> <mallory"@evil.example`: live name "" and email the whole string, rebuilt the reverse (name the string, email ""). Display-level only: FTS tokenizes the sender column the same and `from:` matches both columns. Pre-existing | **open**: needs an owner decision (the page's sender format changes, or the live index takes the same lossy round trip) | MA-187 `TestRebuildRecoversSenderWithAnEmbeddedAngleBracket` (SKIPPED encoder, `c1c9ec0`; un-skipped it fails with `sender after the rebuild = ["Mallory <x" "ceo@bank.example"], live = ["Mallory" "x <ceo@bank.example"]`) |
| R3-AF3 | rebuild forger | A frameset mail loses its whole body from the page, with no fallback, no manifest note and no words in the rebuilt index (`html.go:354-419` `sanitizeParsed`, bodyNode nil) | CONFIRMED (low) | Frameset mail `<html>...<frameset cols="*"><frame src=...><noframes>framesetword alternate text</noframes></frameset></html>`: no Verification line, the page holds an empty body div, `search framesetword` live 1, rebuilt 0. Pre-existing (the base has the same nil-body path). Canonical for R3-AO3 and R3-AC4 | `36cfa22` | MA-303 `TestRenderFramesetMailIsShownAsSource` (RED: `a mail with no body must be shown as source, not as an empty page`); MA-492 `TestLiveAndRebuiltIndexAgreeOnAFramesetMail` (RED: `search "framesetword": live=1 rebuilt=0`) |
| R3-AF4 | rebuild forger | Serialization that is not a fixed point lets mail content leave `div.mailarchive-body`; visible on the page, never indexed on rebuild (`html.go:176-213` `pageFallback`/`anchoredPage` check no containment; `internal/app/htmlheader.go:334`) | CONFIRMED (low; same mechanism as R3-AC2, fixed with it) | `<li><table><div><li></div> escapeword visible tail`: the page's body div closes at the mail's `</div>` and the tail is body-level text after it; CLI: `search escapeword` live 1, rebuilt 0; no fallback. Other trigger `<button><table><div><button></div> endmarker`. Kept as a duplicate of R3-AC2 | `1406fe8` | MA-474 `TestRenderKeepsTheWholeMailInsideItsBodyDiv` (RED: `li table div li (reading 0, fallback ""): content follows div.mailarchive-body in <body>`) |
| R3-AF5 | rebuild forger | `flattenDeep` drops `<template>` tags past depth 480, so the page shows hidden content the live index skips (`html.go:499-1000` `flattenAt`; `internal/index/htmltext.go` `walkElement`) | DUPLICATE of R3-AC5 | Variant (a) of R3-AC5: 485 or 500 nested `<div>` then `<template>hiddenword</template> visibleword` gives hiddenword live 0, rebuilt 1 (10 divs: both 0) | `9f6a850` | `TestFlattenerRefusesWhatItCouldNotDropWithoutChangingTheMail`, `TestLiveAndRebuiltIndexAgreeOnTagsThePageCannotFlatten` |
| R3-AC1 | integrity / differential | The in-place noscript retry decodes the text of EVERY noscript in the mail once any one trips the re-check, so escaped text can become live markup for a JS-off reader and live/rebuilt words differ (`html.go` `neutralizeNoscriptContents`/`neutralizeOneNoscript` ~`:1385-1497`) | DUPLICATE of the MA-492 / MA-476 open gap | Reproduced (`<noscript><meta charset=utf-8></noscript><noscript>A &lt;b&gt;fzbolded&lt;/b&gt; ...`: live `lt`=1 `gt`=1, rebuilt 0) but it is the known skipped `TestLiveAndRebuiltIndexAgreeOnOuterNoscriptEscapedText` (pending the owner on MA-476's escaped twin); only the trigger is broader (a charset meta in a different noscript). Safety unaffected: the dual-mode re-check still runs after the rewrite | `c1c9ec0` (the skipped encoder gains this fixture; no assertion loosened) | `TestLiveAndRebuiltIndexAgreeOnOuterNoscriptEscapedText` (SKIPPED; un-skipped: `search "lt": live=1 rebuilt=0`, `gt` 1/0, `outeresctag` 1/0) |
| R3-AC2 | integrity / differential | Serialization is not a fixed point for malformed nesting, so mail markup closes `div.mailarchive-body` early and content falls outside it; rebuild loses those words (`html.go` `anchoredPage` `:194-213`, `pageFallback` `:176`) | CONFIRMED (low) | `<button> f4zero <table/> f4one <div> f4two <button> f4three </div> f4four`: live f4four 1, rebuilt 0 (f4zero..f4three 1/1); `<li> f3zero <table> f3one <div> f3two <li> f3three <colgroup> f3four` likewise; no fallback, pagecheck clean. Fuzz rate 2 of 800k. Canonical for R3-AF4 | `1406fe8` | MA-474 `TestRenderKeepsTheWholeMailInsideItsBodyDiv` (RED: `f3 li colgroup: "f3four" must be shown in the body div`, `f4 button table: content follows div.mailarchive-body`) |
| R3-AC3 | integrity / differential | A script left in an escape state plus foster-parenting hides the first table cell's words from the page and the rebuilt index (`html.go` `sanitizeParsed` serialization, `hostileSurvives`/`pageFallback` compare no text) | CONFIRMED (low) | `<table><tr><td> f1zero <thead> f1one <menuitem> f1two <script><!-- f1three <script><!-- f1four </div>`: f1zero live 1, rebuilt 0; in the page the script swallows the table (a visible-text walk shows only `f1one f1two`). Fuzz: 25 of 1.5M shallow cases, all this family. Same family as R3-AC2, a different symptom and fix (a words check) | `1406fe8` | MA-474 `TestRenderKeepsTheWholeMailInsideItsBodyDiv` (RED: `f1 script escape table: "f1zero" must be shown in the body div`) |
| R3-AC4 | integrity / differential | A mail with a body-level `<frameset>` renders an empty page with no fallback (`html.go` `sanitizeParsed` ~`:412-419`) | DUPLICATE of R3-AF3 | Same defect; reproduced (`f2beta` live 1, rebuilt 0; empty page, `BodyFallback ""`). Its fix direction (fall back when body is nil and the mail was non-empty) was taken | `36cfa22` | `TestRenderFramesetMailIsShownAsSource` |
| R3-AC5 | integrity / differential | Flattening past depth 480 changes what hidden or foreign content means: (a) `<template>` dropped, its text shown and indexed (rebuilt only); (b) an SVG `<style>` beyond a dropped `<g>` indexed (rebuilt only); (c) a dropped SVG's self-closing `<textarea/>` becomes an HTML textarea that swallows the rest of the page, whose closing tags are indexed as 480 extra `div` terms; (d) a 481-deep `<noscript>` wrapper hides the words after it (live only; the known second san-Q2 gap); (e) a CDATA section past the cap becomes a bogus comment, its text live only (`html.go` `flattenAt`/`flattener` `:516-1010`; `internal/index/htmltext.go`) | CONFIRMED (low) | Reproduced in-process (live index against rebuild): f5secret live 0 rebuilt 1 (485 or 500 divs, 10 divs both 0), f6hidden 0/1, the term `div` 0/1, f7shown 1/0, f9cdata 1/0. All shapes need 480+ nested elements and are hostile-shaped; the ordinary-mail and nested-table parity tests are unaffected; no R19 effect. (a), (b), (c), (e) new; (d) is the known gap. Canonical for R3-AF5 | `9f6a850` | MA-473/MA-492 `TestFlattenerRefusesWhatItCouldNotDropWithoutChangingTheMail` and `TestLiveAndRebuiltIndexAgreeOnTagsThePageCannotFlatten` (RED at `6bea1cd`: `template ... must be shown as source ("too-deeply-nested"), got ""`, `svg cdata ... "f9cdata" must be live text`, `search "f5secret": live=0 rebuilt=1`, `"f9cdata": live=1 rebuilt=0`) |
| R3-AC6 | integrity / differential | The 16x serialization cap (`432509a`) scales with input size, so padding the mail lifts the cost guard (`html.go` `serializedCap` `:435`, `capWriter`, `sanitizeParsed` `:397-423`) | CONFIRMED (low) | A mail of `<!--` + pad + `-->` + `<p>` + 400 `<b id=iN>` + 20000 `<p>x</p>`: with no pad (0.16 MB) 1.3 s and a too-large fallback (the guard works); with a 10 MB pad the cap is 160 MB, so it exports in 17.4 s, 3.4 GB peak and a 134 MB rendered page (the finder: 18.6 s, 3.2 GB, 126 MB). Linear (about 1.7 s per padded MB), not quadratic; the archive lock is held. RV-8's direction had been "16x or an absolute cap (say 32 MB)" and only the relative half was taken | `655861c` | MA-472 `TestSerializedCapDoesNotScaleWithPadding` (RED: `serializedCap(10485760) = 167772160, want 44040192`), `TestPaddingCannotLiftTheSerializationCap` |

**Known items this round also closed (san-Q2, rulings §13.3).** Both R8 shapes of the skipped
`TestLiveAndRebuiltIndexAgreeOnCorpusResidualShapes` (8 of the 372 corpus-adv mails) were fixable
without a design change, and its Skip is removed (it is a permanent guard):

- **A `<plaintext>` at the end of a mail swallows the page's closing tags** (`6bea1cd`): the
  rebuild removes the template's closing bytes (`export.PageClosers`, `\n</div>\n</body>\n</html>\n`)
  from a final plaintext's text before indexing (`trimSwallowedClosers`), so "div", "body" and
  "html" are found on neither side. The writer cannot close a plaintext, and the exporter's page
  check allows exactly this deviation (the existing twin "body plaintext at the end" still renders
  as HTML). Tombstone `TestLiveAndRebuiltIndexAgreeOnAFinalPlaintext` (MA-492; RED with the trim
  mutated out: `search "body": live=1 rebuilt=3`, also `div` and `html`). Only the encoded closers
  were fixed here: the family (elements the tree builder puts inside or after a final plaintext)
  was not, and is closed by NF-2 below.
- **The flattener drops a noscript's tags past depth 480** (`9f6a850`): with R3-AC5, it now
  declines to drop a `<noscript>`, `<template>` or SVG `<style>`/`<script>` start tag past the cap
  (the mail is shown as source, `too-deeply-nested`, as before the flattener existed), drops a
  self-closing leaf in a dropped SVG/MathML region with it, and writes a CDATA section there as its
  escaped text. The existing tests that keep the SVG root dropped (`tp foreignobject cell`, a
  600-deep SVG group) still render as HTML.

**What the fixes are.** One page-level check carries R3-AO1, AC2, AC3 and AF4 (`1406fe8`):
`pageFallback` now takes the sanitized body and, in every reading, requires that nothing but
whitespace follows `div.mailarchive-body`, that the page holds exactly the template's own charset
and policy metas, no live resource-hint link and at most one header and one body marker element;
with scripting on, the body div's children must serialize to exactly the sanitized markup (modulo
the stray `</noscript>` end tags of a rewritten noscript and the closing tags swallowed by a final
`<plaintext>` or script escape). A failing page is `unsafe-after-reparse`, and a noscript mail still
gets the in-place retry first. The catalog rows MA-474, MA-303, MA-472, MA-473, MA-492 and MA-187
carry the new statements.

**A regression of my own fix, found by the corpus run and fixed (`c076187`).** The first version of
the body-div check (`1406fe8`) pushed 8 of the 49 benign corpus mails (Outlook simple and chains,
Mailchimp, HubSpot, Litmus, a video/picture mail) into the fallback: `neutralize` had always appended
a second `alt` and `class` to an `<img src="cid:...">` with no matching part that already had its own,
which the parser drops on re-parse (the first attribute wins), so the serialization was not a fixed
point. `neutralize` now adds the caption and the marker class only where the image has none, which is
what a browser used anyway. Tombstone `TestRenderMissingInlineImageWithItsOwnAttributesRendersAsHTML`
(MA-80/MA-474; RED at `c1c9ec0`: `a mail whose inline images are missing must render as HTML, got
fallback "unsafe-after-reparse"`). Benign new fallbacks: 8, then 0.

**Corpora, end to end, on a binary built at the final code head (`c076187`) against one built at
`829f51d`** (`reindex -rebuild` against the live index through `fts5vocab`, every term; pagecheck:
x/net parses each page in both scripting readings):

| Corpus | Mails | Per-term count differences, final vs base | Documents whose body terms differ | Fallback pages | Unrecovered fields | Pages with a live header/body/meta/hint/marker issue |
|---|---|---|---|---|---|---|
| rv3/corpus-adv | 372 | 0 vs 14 | 0 vs 8 | 39 vs 30 | 0 vs 0 | 0 vs 0 |
| rv3/corpus-benign | 49 | 0 vs 0 | 0 vs 0 | **0 vs 0** | 0 vs 0 | 0 vs 0 |
| r3v/corpus-blocks | 96 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 |
| rv3/rv5.mbox | 5 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 |
| the 76-mail hostile set | 76 | 0 vs 0 | 0 vs 0 | 3 vs 2 | 0 vs 0 | 0 vs 0 |
| fv4 corpus | 35 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 | 0 vs 0 |
| rv3/corpus-findings | 46 | 0 vs 0 | 0 vs 0 | 2 vs 2 | 0 vs 0 | 0 vs 0 |
| round-3 vectors (generated: AO1 and variants, AC2/AC3/AF4, frameset, AC5 shapes, san-Q2, benign twins) | 41 | 0 vs 31 | 0 vs 30 | 11 vs 0 | 0 vs 0 | 0 vs 18 |
| fv4/runs/erase (the MA-187 erase half, known) | 4 | 8 vs 8 | 2 vs 2 | 0 vs 0 | 3 vs 3 | 0 vs 0 |

Reading it: the 8 corpus-adv documents that differed at `829f51d` (the san-Q2 shapes) now agree on
every term, with 9 more adversarial fallbacks (all nine are `adv_deep*` too-deeply-nested shapes;
the page check added none on this corpus: the 30 unsafe-after-reparse ones are the same 30 as at
`829f51d`); no new fallback on benign mail (corpus-benign, corpus-blocks, fv4, rv5: 0 before and
after; the `.words` tokens missing on corpus-benign, 8 of them, are the same 8 before and after:
single-letter MathML tokens and accented words, outside this change set); the one new fallback in the
hostile set is its `own deep hide` mail (a deep SVG `<style>` payload, now shown as source, parity
kept); the round-3 vectors show 18 live issues at `829f51d` and 0 now; the `erase` corpus is
unchanged (the known MA-187 gap, 3 unrecovered). Cost: `explode.mbox` and `explode8k.mbox` fall back as
too-large in 0.2 s and 0.6 s, as before; the padded RV-8 mail (10 MB comment) exports in 1.7 s as a
10.9 MB too-large page where `829f51d` took 18.0 s and wrote a 134 MB rendered page.

**What held** (finders, round 3):
- **Outside aggressor.** No live meta, link hint, reserved marker, second charset, header-count or
  anchoring fault in any JS-on page across about 48M fuzzed mails (general and noscript/head
  grammars, about 12M noscript-heavy with a retry trigger: a charset-meta noscript, a head
  `<noscript><div>`, a preconnect link; foreignObject, desc, annotation-xml and mtext noscripts,
  select/table/template contexts, plaintext, escape-state scripts, CDATA, bogus comments, duplicate
  attributes, NUL in attribute values). 120k mails with a ~480 nesting prefix were clean: the
  flattener past 480 added no live content. In the JS-off reading, noscript-heavy mail with no
  stray `</div>` gave nothing beyond that class (10M mails), including the retry on page-check
  failure (san-Q1 part 1) and the retry mixed with a plain meta/charset/link trigger. san-Q1 part 2
  (stray noscript end tags under SVG/MathML integration points): the `foreignParent` refusal in
  `closesItself` held; a noscript under foreignObject, desc, title, annotation-xml or mtext produced
  no live element in either reading. The rewritten-noscript path is guarded against the same `</div>`
  trick (`neutralizeOneNoscript` drops the stray end tag in the fragment parse; `closesItself`'s
  scripting-off sentinel check). `http-equiv` matching (ASCII case, whitespace, `refresh\x00`,
  duplicate attributes), link-pragma metas and mixed-case or multi-token `rel` all behaved as a
  browser reads them. Fallback pages: `plainFallback` escapes every byte; the size cap and too-large
  path fall back before any re-parse; render-once renders once. Left to CSP, not tested in a
  browser: remote img/srcset/picture, CSS url()/@import/@font-face, object/embed/iframe/form/base.
  `cid:` embedding (the attacker's MIME type goes into a data: URI before sanitizing), search
  snippets (escaped before `<mark>`).
- **Rebuild forger.** Forging subject, sender or date on a CURRENT page: none in about 1.45M fuzz
  mails; exactly one live header and one body div in both readings. On OLD pages: v0.2.3-shaped pages
  with a forged header in the head, at the body start, after a stray `</div>`, doubled, 600 deep, with
  a `<title><body>` placement never read the forged values (the only effect is an erase when the real
  span is not live, counted honestly, as at `066f86d`); benign v0.2.3 pages recover every field. Deep
  aa1c98d-format pages (509-570 nested): no forged field in 50k pages. Date stamps (`affd96f`,
  `aa565c7`, `7b22df7`): no forged or wrong date; only UTC reads the abbreviated v0.2 stamp, a
  non-UTC offset loses seconds by design. The fallback attribute cannot be made to fire on a current
  page or honoured on an old one. A rejected noscript rewrite keeps the raw text, a failed retry keeps
  the first pass's reason. Reserved markers (MA-496): never live in either reading. `splitAtBodyDiv` and
  `splitAtLegacyHeader` cannot be moved by decoys. Corrections to expectations: today's exporter
  rewrites a head noscript or comment mimic, so only a head `<style>` or `<script>` erases (R3-AF1).
- **Integrity.** Header: 0 subject/from/date loss or forgery in about 3.5M fuzz cases and the 24-shape
  matrix. Page layout in both readings over 612 pages: 0 pages with a missing header or body div and
  0 with content after the body div (at `829f51d`, before R3-AC2's shapes were generated). Benign
  parity on corpus-benign, corpus-blocks, fv4, rv5 and the glue and pre corpora. The skipped residual
  encoder was accurate: corpus-adv left exactly the 8 documented mails. Block-set parity (`0f824b9`,
  `e8e6129`) and the implied head (`7f2fd05`). Cost: no quadratic shape found (34 shapes at 20k/40k,
  8 at 160k/320k, 7 explode-plus-b variants, linear). Tokenizer fallback against the tree on 100k
  self-differential cases: only the documented residuals (frameset, svg-title plus template, table-mode
  around svg style, NUL as a word). `go test -race` on export, app, index: ok. Not filed (out of scope or
  pre-existing): the index prefers the plain body over HTML (MA-493); nameless attachments index as ''
  live and `attachment-N` after a rebuild (zip names).

**Not covered by round 3** (nothing here counts as a negative result):
- **No browser was run** by any finder or by the fixes: the oracle is x/net in both scripting
  readings. R3-AO2's suspicion (a favicon fetch outside the page policy) and the noscript link/meta
  copies in a JS-off browser (a standing residual since round 2) are unverified.
- **At filing, the round-3 fixes had had no adversarial pass** (superseded: see *Re-verification of
  round 3's fixes* below, which found NF-1 to NF-4). The page check (fixed-point comparison
  with its two allowed deviations, the stray-`</noscript>` normalization, the marker counts), the
  flattener's refusal set and leaf handling, `trimSwallowedClosers` and the cap's headroom were written
  and tested by the fixing agent, and one of them produced a false positive on benign mail that only
  the corpus run caught (the duplicate `alt` above). Other non-fixed-point serializations that benign
  mail may hit are known only through the corpora listed, which were chosen by hand, not drawn from
  real traffic.
- oauth2 and Graph token handling, the GODEBUG defaults of the `go 1.26` directive, a PST fixture end
  to end, and Chromium 143's customizable-select parsing against x/net's `<noscript>`-in-`<select>`
  (still open from rounds 1 and 2); the finders' fuzz counts are theirs, not re-run by the skeptic.
- RV-10's GUI and desktop legs (still open).

**Open after round 3.** Still FINDINGS OPEN, each named: **RV-10** (round 2: the fallback is not
surfaced in `status`, the GUI or the desktop message; its owner is the status branch); **R3-AF2**
(a skipped MA-187 encoder; an owner decision on the page's sender format); the known gaps that
duplicate findings, still encoded as skipped guards and not accepted: **MA-187's erase half** (R3-AF1,
FB-1, FX-187's writer signal) and **MA-492's outer-noscript escaped text** (R3-AC1, MA-476's escaped
twin); the owner question behind R3-AO2 (**fail closed on any live mail `<link href>`?**); and the
re-verification's own fixes (below), which no one but their author has read yet (a round 4 before
the branch merges, if the owner wants the guarantee "every finding of every round is fixed and
re-verified").

### Re-verification of round 3's fixes (`4b71152`, then the fix commit of this section)

A fresh-eyes pass (not the author of the fixes or of the round) re-ran the fixes' tombstones with
each fix mutated out, generated variants and grammar-fuzzed mails (about 12.6M for R3-AO1) and
re-measured the cost numbers. Outcome per item:

| Item | Outcome |
|---|---|
| R3-AO1 | Closed (266 variants and 12.6M fuzz mails: 0 live issues in both readings at head, 71 and 5607+ at `829f51d`). The marker half and the scripting-off trailing-sibling check are each redundant with the other: see NF-4. |
| R3-AC2 with R3-AF4 | Closed (tombstone red with the containment removed; 12,000-mail grammar corpus: live-vs-rebuilt differences 507 to 10, the 10 being the plaintext family of NF-2). |
| R3-AC3 | **Finding closed, mechanism regressed benign mail: fixed here (NF-1).** The byte-equality comparison needs the sanitizer's tree to serialize as the page's tree; it did not for ordinary markup. |
| R3-AF3 with R3-AO3, R3-AC4 | Closed (tombstones red with the nil-body branch restored). |
| san-Q2 shape 1 | **Encoded closers fixed; the family was not: fixed here (NF-2).** |
| R3-AC5 with R3-AF5, san-Q2 shape 2 | Closed (each refusal and leaf rule separately pinned; 2300 deep mails, 0 differences). Disclosed trade-off, not a defect: a benign mail nested past 480 levels that holds an inline SVG with a `<style>`, a `<template>` or a `<noscript>` there now falls back as too-deeply-nested. |
| R3-AC6 | Closed (cost numbers reproduce: padded 10 MB mail 17.4 s / 134 MB at base, 1.7 s / 10.9 MB at head; worst remaining case 3.9 s, 0.85 GB, 26.8 MB page, bounded by the 32 MiB headroom). |
| c076187 (own false positive) | Closed for the shape it names; NF-1 is the same defect class (a serialization that is not a fixed point reads as hostile) and the corpora held none of its shapes. |
| R3-AF2, R3-AC1 (escalations), R3-AF1, R3-AO2 | Confirmed as filed. The encoders are right and unchanged (R3-AF2: not invertible even in principle, since `From: "Mallory <x" <ceo@bank.example>` and `From: Mallory <"x <ceo"@bank.example>` give the same header text, so a format change or a lossy live index is an owner decision; R3-AC1: no ruling row exists for MA-476's escaped twin, so it is not yet on the owner's list; R3-AF1 is the known FB-1/FX-187 gap; R3-AO2 is policy). Nothing changed. |

**New findings of the re-verification** (all low, none a regression of safety; all found with the
finder's own x/net oracle in both scripting readings, no browser). I reproduced each before fixing it.

| ID | Defect | Fix and tombstone |
|---|---|---|
| NF-1 | Benign mail fell back as `unsafe-after-reparse`, breaking the round's own bar of no new fallback on benign mail (R7). The page check compares the page's tree (standards mode, inside a div) with the sanitizer's tree byte for byte; the sanitizer parsed the mail in the MAIL's own compatibility mode. In quirks mode a `<table>` in an open `<p>` stays inside the `<p>`, in standards mode it closes it: every mail with no doctype or a quirks doctype and `<p>...<table>` (a Thunderbird html/head/body page, `<P align=center>..<TABLE>` after the HTML 4.01 Transitional doctype, FrontPage) fell back, 3 of 4 in an end-to-end run, all of them rendered at `829f51d`; a fuzz of 400k sloppy newsletter-like mails gave 17,833 head-only fallbacks, 15,362 of them this class. A second class: `<form><b>x</form><form>y</form>` (the second form is built inside the re-opened `<b>`, and a form start tag in a form is ignored on re-read). `0 of 49` on corpus-benign hid it. | `sanitizeParsed` parses the mail behind a `<!DOCTYPE html>` of ours (standards mode, as the page is read; the mail's own doctype is then an ignored parse error), and `unnestForms` unwraps an HTML form inside a form (what the re-read does anyway; not across a template). The fixed-point check is unchanged. Tombstone `TestRenderBenignQuirksModeMarkupStaysHTML` (MA-80, MA-474; RED: 7 of 8 vectors `got fallback "unsafe-after-reparse"`; with only the form unwrapping mutated out, the two form vectors stay red). |
| NF-2 | A final `<plaintext>` still drifted live vs rebuilt when the tree builder put elements inside it (a formatting element re-opened as a clone) or after it in tree order (a foster-parented table): x/net writes them as markup after the plaintext's text, the page reads all of it as the plaintext's TEXT, the check passes (the strings are equal), the page shows literal tags and the rebuild indexes `i`, `table`, `href`, `color`, `font`... (rebuilt only). 5 of 8 minimal vectors, 3 of 8526 and 10 of 9899 grammar mails. Not a regression. | `plaintextHoldsElements`: a tree with any element at or after an HTML `<plaintext>` in document order other than the plaintext itself is not written as HTML (shown as source, `unsafe-after-reparse`; both indexes then read the source with the one extractor). Plain plaintext tails render as before. Tombstone `TestLiveAndRebuiltIndexAgreeOnAPlaintextThatHoldsElements` (MA-492, R8; RED: `search "href": live=0 rebuilt=1`, also `pfx`, `color`, `pfred`). |
| NF-3 | After the in-place noscript retry, an HTML iframe, noembed, noframes or xmp inside `<math><mtext>` inside a noscript was written with its text escaped, which a scripting-off read takes literally: rebuilt-only `lt`, `gt` (and `amp`). Contrived, pre-existing. | `neutralizeOneNoscript` does not rewrite a noscript that holds such an element (`rawTextUnderForeign`); it keeps its raw text, which both indexes read the same way. Tombstone `TestLiveAndRebuiltIndexAgreeOnRawTextUnderMathInANoscript` (MA-492, R8; RED: `search "lt": live=0 rebuilt=1`, `gt` 0/1). |
| NF-4 | Five layers of the page check (`pageMarkersAreOurs`, the scripting-off trailing-sibling check, the meta count, the resource-hint part and the header/body count) survive mutation: removing any one alone leaves the packages green, because each stray-div breakout is caught by two of them. | **Accepted and recorded, no change.** They are defense in depth for R19; a vector where exactly one layer fires has not been found, and a test per layer would have to invent one. Only the fixed-point comparison and the scripting-on trailing-sibling check are individually pinned. Owner may ask for per-layer vectors. |

Also observed, not fixed (pre-existing, out of this change set): for `<table>pfseven<plaintext>pfeight`
both indexes find neither word (0 and 0), so the two agree but the mail's words are not searchable.

**Corpora after the fixes** (binaries built at `4b71152` and at the fixed head, run through
`r3v/parity.py`): rv3/corpus-adv 372 mails, 0 differing terms, 0 differing documents, 39 fallbacks,
0 unrecovered, both binaries; corpus-benign 49, 0/0/0 fallbacks, the same 8 `.words` tokens missing
(outside this change set); corpus-blocks 96, 0/0/0; corpus-findings 46, 0/0, 2 fallbacks (too-large);
rv5 5, 0/0/0. No change on any of them, so the standards-mode parse and the unwrapping move no
benign document and add no fallback.

**Residual of this section.** The page check's fixed-point comparison can still read a benign
mail as hostile for shapes no corpus holds (the NF-1 fuzz still left 525 head-only fallbacks of
600k random-tag mails: nested heading/a/form-in-table tag soup). Each is a safe fallback with parity,
not a loss.
