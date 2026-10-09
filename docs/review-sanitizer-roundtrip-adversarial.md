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
6 medium, 10 low); 4 fixed, 12 open (3 medium, 9 low).**

**Verdict: FINDINGS OPEN.** Three confirmed medium findings remain open, all from round 2 and
all already present at `066f86d`: RV-1 (a pre-v0.3.0 page lets a mail forge its own subject,
sender and date on `reindex -rebuild`), RV-2 (`<link rel="preconnect">` survives sanitizing and
opens a connection to the sender's host when the archived page is viewed) and RV-4 (benign mail
that omits `</head>` gets no body words in the live index). Nine low findings are open too:
DIFF-FP-1, DIFF-FP-2, RV-3 and RV-5 to RV-10. Round 1's four findings (OUT-1, INT-1, INT-2,
INT-3) are fixed in `aa1c98d` and were re-verified. In round 1 the commit's central claim, that
the round trip is re-checked "as a browser will" parse it, held only for a browser with
scripting **on**; `aa1c98d` re-checks under both scripting modes, and the differential lens
found no offensive parser differential left.

| ID | Lens | Claim | Verdict | Evidence (repro or refutation) | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| OUT-1 | outside aggressor (hostile email sender) | A `<meta http-equiv="refresh">` placed inside `<noscript>` comes out of `sanitizeHTML` **live**, and `hostileSurvives` does not fail closed. x/net parses with scripting **enabled** by default (`x/net@v0.60.0/html/parse.go:2294`, `scripting: true`), so the content of `<noscript>` becomes raw text. `neutralize` (`html.go:274`) never sees an `atom.Meta`, and `xhtml.Render` writes the text back as literal markup (`render.go:240-248`, `childTextNodesAreLiteral` always includes noscript). The re-parse in `hostileSurvives` (`html.go:219`) and MA-300's independent helper `liveHostileMetas` (`html_roundtrip_test.go:18`) both call `xhtml.Parse` with the same default, so both share the blind spot. A browser with scripting **off** parses `<noscript>` content as markup, and the refresh navigates the page to the sender's URL. This defeats R19 (`scenario-catalog.md:132`, which is unconditional) and the noscript coverage that MA-300 names (`scenario-catalog.md:363`). | CONFIRMED (medium; finder: high) | `RenderWith` on `<p>marker</p><noscript><meta http-equiv="refresh" content="0;url=http://127.0.0.1:8741/b/sk_noscript_body"></noscript>` writes `<noscript><meta http-equiv="refresh" …></noscript>` into `div.mailarchive-body`: `http-equiv` is not renamed to `data-mailarchive-neutralized`, and there is no `<pre class="plain">` fallback. The same holds in 4 placements: body, `<head>`, a nested `<div>`, and `<svg><foreignObject><noscript>` (an HTML integration point). `liveHostileMetas` returned 0 matches on every case, while a scripting-disabled re-parse returned 1. Headless Edge 143 (`--headless=new`, throwaway `--user-data-dir`, `file://`): with JS on (the default), no beacon hits on any of the 4 pages. With `--blink-settings=scriptEnabled=false`, the beacon logged `HIT GET` for all 4 (`/b/sk_noscript_body`, `_head`, `_div`, `/b/sk_svg_fo_noscript`). The finder's run on port 8731 got the same result. Controls: a plain body `<meta refresh>` was neutralized and gave no hit with JS off. A raw, unsanitized refresh page and a raw remote `<img>` page with no CSP **did** hit, so the harness was live and the empty JS-on log means the page really was inert. Scope: the threat model concedes no non-goal for viewers with JS off. The archive ships zero JS and its CSP blocks scripts, so a reader with JS off is a reasonable posture (Tor Safest, a policy that blocks JS, `javascript.enabled=false`). Severity is medium, not high: the impact is the same as OFF-2 (`review-archive-offline.md`), an open beacon / IP leak or a phishing landing page. It adds a precondition (scripting off), and default viewers stay inert. The payload is trivial and needs no encoding. | `aa1c98d` | MA-300 `TestRenderForeignContentRawTextStaysInert` (noscript body/head/nested/svg-foreignObject/escaped cases; re-parse with scripting on and off) |
| INT-1 | integrity | The x/net v0.60 parser has a hard nesting cap: `html.Parse` returns `html: open stack of elements exceeds 512 nodes` (`parse.go:235-239`, panic recovered at `:2225`). `readArchivedHTML` (`internal/app/htmlheader.go:43-50`, used by `reindex -rebuild`) treats a parse error as "nothing recovered". Any archived page nested deeper than 512 elements therefore rebuilds with an empty subject, from, date and body, and search can no longer find it. This hits (a) every deep page written by x/net v0.10, which had no cap, and (b) **new** exports, because of a one-level gap. `sanitizeHTML` and `hostileSurvives` parse `html>body>mail`, but the page adds `div.mailarchive-body` on top, so a body that is exactly 512 deep passes sanitize, renders as HTML, and then cannot be read back. This violates R13 ("surviving files stay searchable"). | CONFIRMED (medium; finder: high) | The mbox held text/html messages `'<div>'×K + '<p>body words for NEEDLE</p>' + '</div>'×K` with K = 10, 508, 509, 510 and 600. (1) New export, new rebuild: the live index finds all 5. After `rm search.db*; reindex -rebuild` the output is `rebuilt=5 (from-eml=0 re-derived=5 unrecovered-fields=3)`. `search needleepsilon` (K=509) and `search deep509` both return `0 match(es)`, and the folder row becomes `(empty date) \| (unknown) \| (no subject)`. (2) Upgrade path: an archive exported by `066f86d` and rebuilt by `066f86d` gives `unrecovered-fields=0` and every needle is found. The same archive rebuilt by `033b1e3` gives `unrecovered-fields=9`, and K=509, 510 and 600 return 0 matches (K=508 and K=10 are still found). Severity is medium, not high: only pages nested more than ~508 deep are affected, the pages stay readable on disk, the rebuild summary counts the loss (so it is not fully silent), and archives kept with `-raw` rebuild from the `.eml`. | `aa1c98d` | MA-304 `TestRebuildRecoversDeepAndFallbackPages` (509-deep page) |
| INT-2 | integrity | The same cap changes export output without saying so. A mail nested more than ~509 elements deep fails the first `xhtml.Parse` in `sanitizeHTML` (`html.go:143`), so the whole body is written as escaped source by `plainFallback`. Before this commit, the same mail rendered as HTML. `sanitizeHTML` returns only strings, so the fallback reaches no issue entry, stat, manifest gap or `status` line. The `cid:` inline images were already turned into data URIs and marked Consumed before `sanitizeHTML` ran, so they are left out of the attachment zip and survive only as base64 text inside the escaped source. Every `hostileSurvives` trip (`html.go:187-189`) falls back in the same silent way. This regresses R7 (legible HTML) and R1 (no silent loss of an inline image). | CONFIRMED (medium) | `grep -c '<pre class="plain">'`: `066f86d` gives 0 for every K. `033b1e3` gives 1 for K=510 and K=600. Both runs print the same summary, `Done. exported=5 … non-html=0 no-body=0`. Inline case (multipart/related, `cid:img1@x`, K=600) on `033b1e3`: 0 live `<img src="data:image/png`, 1 escaped `&lt;img src=&#34;data:image/png`, no zip written, the summary still says `inline=2`, the manifest has no missing/terminal/issue entry, and `status` reports `Incomplete: none`. On `066f86d` the image renders live. The bytes are not lost (they survive as base64 text), but they can no longer be viewed and are not archived as a file. | `aa1c98d` | MA-303 `TestExportDeepHTMLFallsBackVisiblyAndKeepsImages` |
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
| DIFF-FP-1 | differential (false-positive cost); introduced by `aa1c98d` | The new two-mode re-parse pushes legitimate mail into the escaped-source fallback (`unsafe-after-reparse`) when the mail puts a **harmless** `<meta charset>` or `<meta http-equiv="Content-Type">` inside `<noscript>`. The fix introduced an asymmetry: `neutralize` (`html.go:316`) walks only the scripting-on tree, where noscript content is opaque raw text, so it can never rename or remove a meta there. `hostileSurvives` (`html.go:250`) then re-parses with scripting off, sees the meta as live markup, and `metaIsCharset`/`hostileEquiv` fail closed. The same metas placed directly in `<body>` are renamed or removed with no fallback (the exporter writes its own utf-8 charset first and overrides Content-Type). So a charset or Content-Type meta inside noscript can never be rendered, only fallen back. | CONFIRMED (low) | Skeptic's PoC test in `internal/export` at `aa1c98d`: `RenderWith` gives `BodyFallback="unsafe-after-reparse"` and a `<pre data-mailarchive-fallback>` for a body `<noscript><meta http-equiv=Content-Type content="text/html; charset=UTF-8"><img src=cid:px@x></noscript>`, a body `<noscript><meta charset=utf-8>`, a head `<noscript>` carrying either meta, an AMP-style head `<noscript><meta charset>`, and a full ESP document inside `<noscript>`. The same metas directly in `<body>` give `BodyFallback=""` (charset removed, http-equiv renamed). Common noscript patterns render normally: a viewport meta, a data: `<img>` pixel, `<style>`. The same test file at `033b1e3` gives no `<pre class="plain">` for the Content-Type or charset case, so `aa1c98d` introduced this. A meta charset of UTF-8 matches the page's own charset, so the fallback guards nothing here. Finder (harness `adv2-differential/cmd/advdiff`): 1 of 36 realistic samples fell back (an AMP email with charset in noscript), and 3 of 6 constructed noscript-meta probes (the three with a charset or Content-Type meta). The skeptic did not re-run the 36-sample corpus; its own probes agree with the split. Low: no data loss (the INT-2 fix puts inline images in the zip and records the fallback on the manifest), and the trigger is uncommon in real mail; it is a rendering-fidelity regression. | open | open |
| DIFF-FP-2 | differential (false-positive cost); since `033b1e3` (x/net bump), the legibility residual of INT-2 | `sanitizeHTML` (`html.go:163-172`) returns `FallbackTooDeep` whenever x/net v0.60's hard cap of 512 open elements is hit, but a real browser renders far deeper. About 128 nested `<table>` layers (table + implied tbody + tr + td, 4 open elements each) is enough, so ordinary-looking nested-table mail is shown as escaped source. The cap is the one behind INT-1/INT-2, here as a false positive on benign mail rather than a hostile-input concern. | CONFIRMED (low) | Skeptic PoC at `aa1c98d`: N nested `<table><tr><td>` wrappers give `BodyFallback=""` for N = 30, 100, 120, 126, 127 and `"too-deeply-nested"` for N = 128, 129, 130, 150 (4 open elements per table plus html/body passes 512). Plain `<div>` nesting falls back at 511. `033b1e3` already gives `<pre class="plain">` at 128 tables, so this predates `aa1c98d`, which only made it visible. Browser side: a raw 150-nested-table page in headless Edge 143 (JS on) dumps a DOM with all 150 `<table>`/`<td>` and the deep `<p>` as a live element, so the browser renders what the exporter shows as source. Finder: 150 tables fall back, while a 30-deep Outlook-style nested-table newsletter renders normally. Low: the realistic trigger is machine-generated or very deeply forwarded mail. | open | open |
| RV-1 | integrity (old-format page, rebuild header forgery); pre-existing, reproduces at `066f86d` | `reindex -rebuild` takes a **forged** subject, From and date from a page written by v0.1.x–v0.2.x for a mail that was a full HTML document. Those releases did not render the mail into the page template: they kept the mail's own document and regex-injected the real header after its first `<body>` tag (v0.2.3 `html.go` `Render`, `reBodyOpen`). If the mail puts `<div class="mailarchive-header">…</div>` inside its own `<head>`, the tree parser closes head at that div and opens body implicitly, so the forged div becomes the first direct child of `<body>`, and `readArchivedTree` (`internal/app/htmlheader.go:148-170`, `firstChildDiv`) reads it. This defeats MA-187's claim that old pages written before the source stripped the namespace are the case it defends; MA-187 tests only a head `<template>`. The finder also claimed that `splitAtBodyDiv` anchors on the real header on these bytes; the skeptic corrected that. | CONFIRMED (medium); secondary claim corrected | Skeptic, own binaries built from git-archive exports. Exported with v0.2.3 one mbox text/html message from `Mallory <mallory@evil.example>`, Subject 'benign skepnewsletter', whose `<head>` holds a `div.mailarchive-header` with `data-mailarchive-field` subject 'skepforged wire approval', from `Chief Executive <ceo@skepbank.example>` and date 'Mon, 01 Jan 2001'. The page keeps the forged div in the mail's `<head>`; the real header follows the literal `<body>`. Live index: `skepnewsletter` = 1, `from:mallory` = 1. After `rm search.db*; reindex -rebuild` with `066f86d` and with `aa1c98d`, both give `skepforged` = 1, `wire` = 1, `from:ceo` = 1, `skepnewsletter` = 0, `from:mallory` = 0, and `search -json` returns subject 'skepforged wire approval', senderEmail `ceo@skepbank.example`, date 2001-01-01. `readArchivedHTML` on the page bytes returns the forged subject and sender with unrecovered=0. MA-187's test (`internal/app/htmlheader_test.go:54`) covers only a head `<template>` and a body-nested header, not a head `<div>` that the tree builder moves into `<body>`. The finder's run (subject 'URGENT wire approval', `adv2-scripts/mkforge.py`) matched, with `unrecovered-fields=0` on both binaries. Correction: on these bytes `splitAtBodyDiv` returns ok=false, because v0.2 full-document pages have no `mailarchive-body` div, so a 600-deep variant recovers nothing (unrecovered=3) rather than anchoring on the real header. Medium, not high (AGG-1 in `review-portability-adversarial.md`, the same forgery on then-current pages, was high): it needs pre-v0.3.0 pages, no `.eml`, and a rebuild, and it forges only the hostile mail's own record, though with the real sender unsearchable. | open | open |
| RV-2 | outside aggressor (offline-inert archive, R19); pre-existing, reproduces at `066f86d` | `<link rel="preconnect" href="http://attacker/…">` in a mail's head or body survives `sanitizeHTML`: `neutralize` rewrites only meta and img (`html.go:316-356`), and CSP has no directive that governs a preconnect hint. In the default posture (JS on), an archived page opened from `file://` makes the reader's browser open a TCP connection to the sender's host, and resolve its DNS name when it is a hostname. That is an open-tracking / IP-leak beacon, the same impact class as OFF-2, despite R19's "no remote loads". | CONFIRMED (medium) | Skeptic's own mbox (`skeppre-*`), exported with `066f86d` and `aa1c98d`. The pages keep `<link rel="preconnect" href="http://127.0.0.1:8799/...">` after the CSP meta, in head and body. Each opened in headless Edge 143 (`--headless=new`, fresh `--user-data-dir` per open, `file://`, JS on) against the skeptic's raw-socket logger: the head and body pages each logged a bare TCP connect on both revisions, 4 of 4. Same-host `dns-prefetch`, `prefetch` and `preload as=image` pages logged nothing, a negative control in the same harness, so the connect is specific to preconnect. A raw remote `<img>` page with no CSP logged `GET /sk/ctl_img`, so the harness was live. The finder got the same 4 of 4 on port 8754 within about 0.5 s of each open (`adv2-scripts/mkpre.py`, `beacon.py`). No conceded non-goal covers link hints. A parser check at `aa1c98d` shows that a `<noscript><link rel=preconnect>` is a live `<link>` only in the scripting-off reading (on: 0, off: 1), so the finder's fix direction (neutralize "on the tree") misses noscript copies. The noscript copy was not verified in a browser: the skeptic's JS-off Edge harness loaded nothing, not even a raw control. | open | open |
| RV-3 | integrity (search parity, R8); pre-existing in `internal/index/htmltext.go` | The full-text extractor `htmlToText` (`internal/index/htmltext.go:34-66`) is a bare tokenizer with one skip counter. Any `</style>`, `</script>` or `</head>` end tag decrements it, and the tokenizer enters raw-text mode on `<style>`/`<script>` even inside SVG/MathML, where the tree parser and browsers do not. A hostile mail can therefore hide words that the archived page visibly renders from both the live and the rebuilt index, e.g. `<svg width=0 height=0><style><!--</style><script>--></style></svg>` before the text: the tokenizer leaves the style at the comment's `</style>`, enters script data at `<script>` and never leaves it, while the tree keeps the comment whole and Render writes it literally (`escapeComment` does not escape `<`). INT-3's "indexed the same way as the live path" holds, but both paths are blind to this. | CONFIRMED (low; finder: medium) | Skeptic's mbox body `<p>Hello skepvisible</p><svg width=0 height=0><style><!--</style><script>--></style></svg><p>please wire funds to account skephidden today</p>`. On `066f86d` and `aa1c98d`, live and after `rm search.db*; reindex -rebuild`: `skephidden` = 0, `wire` = 0, `skepvisible` = 1. Headless Edge 143 (JS on) `--dump-dom` of the `aa1c98d` page has `<p>please wire funds to account skephidden today</p>` as a live sibling after `</svg>` in `div.mailarchive-body`, so the reader sees it. A control that keeps the counter balanced (math>style twice) indexed normally. The finder's run matched (`kestreleleven`, `adv2-scripts/mkhide.py`). Lowered to low: a sender already has trivial ways to evade keyword search (soft hyphens, text in images), the effect reaches only that sender's own message, and nothing on disk is lost. The same tokenizer blindness drives RV-5. | open | open |
| RV-4 | integrity (search parity for ordinary mail, R8/R13); pre-existing in `internal/index/htmltext.go` | An ordinary, valid HTML mail that leaves out the optional `</head>` end tag gets **no** body words in the live full-text index: `htmlToText` increments `skip` on `<head>` and decrements it only on `</head>`, `</style>` or `</script>`, so the whole body is skipped. `reindex -rebuild` reads the body div instead and does index the words, so the live and rebuilt indexes disagree on benign mail. | CONFIRMED (medium) | Skeptic's mbox with three messages: `<html><head><meta name=viewport …><body><p>hello skepnoheadword</p>`; the same with `</head>` (`skepwithheadword`); and `<head><title>t</title><body>` with no `</head>` (`skepnoheadtitleword`). Live index on `066f86d` and `aa1c98d`: `skepnoheadword` = 0, `skepnoheadtitleword` = 0, `skepwithheadword` = 1. After `rm search.db*; reindex -rebuild`: all three = 1. The finder's run matched (`kestreltwelve` = 0 and `kestrelthirteen` = 1 live, both 1 after the rebuild; `adv2-scripts/mknohead.py`). Medium: silent loss of searchability on non-hostile mail (R8). | open | open |
| RV-5 | integrity (`splitAtBodyDiv` anchoring); introduced by `aa1c98d` as an incomplete INT-1 fix | `splitAtBodyDiv` (`internal/app/htmlheader.go:74-119`) does not anchor "exactly as the tree reader is". The bare tokenizer enters raw-text mode on `<style>`/`<xmp>`/`<plaintext>` and similar even in foreign content, while the tree parser, the sanitizer and Render do not (the parser calls `NextIsNotRawText`; `escapeComment` leaves `<` alone). Head content such as `<template><svg><style><!--</style>…-->` lets the tokenizer see tags that the tree sees as comment text. On a page whose body is ~509 deep (the only pages that reach the tokenizer path), a hostile mail can choose the split point: (a) an early split, so `readArchivedTree` finds no header, subject/From/date are lost, and the real header and transport headers are indexed as body text; (b) `<plaintext>`, giving ok=false and a record with no subject, From, date or body; (c) `<xmp>` plus a body trick, so the words before the split are not indexed (the live index shares the blind spot, RV-3). No **forged** field could be produced: `readArchivedTree` re-anchors on the prefix tree, and mail cannot carry reserved classes. The damage is limited to the hostile mail's own record, but it restores INT-1's "vanishes from search by sender/subject/date" symptom on demand. | CONFIRMED (low) | Skeptic's mbox, exported and rebuilt with `aa1c98d`, bodies nested 509 deep, head payloads "early" (`<template><svg><style><!--</style><body><div class=mailarchive-header>SKFORGED…<div class=mailarchive-body>--></style></svg></template>`) and "none" (`…<plaintext>…`). Live index: `skepearlysubj` = 1, `from:skearly` = 1, `skepnonesubj` = 1, `from:sknone` = 1. The rebuild prints `rebuilt=4` with `unrecovered-fields=6`; afterwards `from:skearly` = 0, `from:sknone` = 0, `skepnonesubj` = 0, and `search -json skepearlyneedle` returns subject "", senderEmail "", date 0001-01-01 and the snippet `… text/html; charset=utf-8 skepearlyword skepearlyneedle` (header text indexed as body). `skforged` = 0 and `from:forged` = 0, so no forged value appears. The 509-deep control and the same early head on a 10-deep page rebuild correctly. The finder's run matched (`rebuilt=5 … unrecovered-fields=6`; unit PoC: `readArchivedHTML` gives unrecovered=3 for early/none at depth 509 and 0 at depth 10). `033b1e3` recovered nothing on any 509-deep page, so this is an incomplete fix rather than a new loss. The skeptic did not test variant (c). | open | open |
| RV-6 | integrity (reserved fallback attribute on old pages); introduced by `aa1c98d` | The commit says `data-mailarchive-fallback` is an attribute "mail can never carry (stripReservedNamespace)". That holds only for pages written by v0.3.0 or later: `stripReservedNamespace` arrived in `91a7b00`, first released in v0.3.0. Pages written by v0.1.1–v0.2.3 insert a mail fragment raw into the body div, so a mail-supplied `<pre data-mailarchive-fallback="x">decoy</pre>` as the first top-level element satisfies `readArchivedTree`'s check (`htmlheader.go:161-165`, `pre.Parent == body`), and the rebuild indexes only the decoy text. Every other word of the mail leaves search: a regression from `033b1e3`, which indexed the whole body div. | CONFIRMED (low) | Skeptic exported with v0.2.3 a fragment message `<pre data-mailarchive-fallback="x">skepdecoy only</pre><p>the real message says skeprealword</p>`; v0.2.3 writes it raw into `div.mailarchive-body`. Live: `skeprealword` = 1, `skepdecoy` = 1. Rebuild with `066f86d`: 1/1; with `033b1e3`: 1/1; with `aa1c98d`: `skeprealword` = 0, `skepdecoy` = 1. `readArchivedHTML` on the same page bytes returns `HTMLBody="skepdecoy only"`. The finder's run matched (`kestrelsix`, `adv2-scripts/mkold.py`). | open | open |
| RV-7 | integrity (INT-1 fix incomplete for old full-document pages; the commit's "(old v0.7.1 pages too)" claim); deep-page part since `033b1e3`, shallow-page part pre-existing | `splitAtBodyDiv` requires `<div class="mailarchive-body">` to be the next element after the header. Pages written by v0.1–v0.2 for full-document mail have no body div (the header was injected into the mail's own `<body>`), so a deep page of that format still recovers nothing on rebuild: `066f86d` (x/net v0.10) recovered its subject and From, `033b1e3` and `aa1c98d` do not. Separately, and pre-existing: every v0.1–v0.2 full-document page rebuilds with no body words even when shallow, because `readArchivedTree` needs a `mailarchive-body` div. That was true at `066f86d` as well. | CONFIRMED (low) | Skeptic exported with v0.2.3 a 10-deep full-document mail (`skepfullsubj`/`skepfullword`/`skepfullneedle`) and a 600-deep one (`skepdeepoldsubj`/`skepdeepoldneedle`); the live index finds all. Rebuild with `066f86d`: `skepdeepoldsubj` = 1, `from:skdeepold` = 1, `skepdeepoldneedle` = 0, `skepfullword` = 0, `skepfullneedle` = 0, `unrecovered-fields=4`. With `033b1e3`: deep page subject/from = 0, unrecovered = 8. With `aa1c98d`: deep page subject/from = 0, body words 0, unrecovered = 6. The 600-deep **fragment** page (`skepdeepfragsubj`) is fixed by `aa1c98d` (1, against 0 at `033b1e3`). A unit call of `splitAtBodyDiv` on the full-document page returns ok=false. The finder's run matched (`deepoldsubj`, `needleseven`/`needleeight`, `adv2-scripts/mkold.py`). | open | open |
| RV-8 | performance / DoS (dual-mode re-parse while the archive lock is held); cost doubled by `aa1c98d`, blow-up pre-existing | `hostileSurvives` now re-parses the whole serialized body twice. On output that x/net's reconstruct-active-formatting step inflates, each re-parse costs roughly O(paragraphs × N²), from Noah's-ark attribute comparisons on every explicit `<b id=i>` in the output. The first parse is cheap, so `aa1c98d` about doubles the CPU that `033b1e3` added, and the cost grows linearly with the mail. The exporter's re-render-without-zip path (every attachment embedded inline) runs `RenderWith` twice, i.e. four re-parses. Pre-existing and larger: the same 160 KB mail already costs 2.4 GB RSS and a 120 MB page at `066f86d`. | CONFIRMED (low) | Skeptic timing test (the same file in each revision's tree), `RenderWith` on `<p>` + 400×`<b id=i>` + `</p>` + n×`<p>x</p>` (66 KB at n=8000, inflating to 50.4 MB of output): `066f86d` 0.41 s, `033b1e3` 3.52 s, `aa1c98d` 6.58 s; at n=2000: 0.14 s, 0.90 s, 1.68 s. A code read of `exporter.go:319-330` confirms the second `RenderWith` when every attachment was embedded. Finder's full CLI export (`-pages=false`, multipart/related with a `cid:` PNG, `adv2-scripts/mkexplode.py`): at n=8000 (68 KB mbox) 0.93 s / 867 MB maxrss, 7.67 s / 1196 MB and 14.46 s / 1253 MB; at n=20000 (160 KB) `066f86d` 2.48 s / 2412 MB against `aa1c98d` 35.99 s / 3238 MB. Ordinary input adds little (5 MB body: 135 → 235 ms, RSS 90 → 133 MB; a 20 MB inline image likewise). The skeptic did not re-measure the CLI export or RSS. | open | open |
| RV-9 | integrity (fallback reason; false positive from the scripting-off parse); introduced by `aa1c98d` | `hostileIn` returns true on **any** parse error (`html.go:261-264`). A benign mail that is shallow with scripting on but deeper than 512 with scripting off, such as a `<noscript>` wrapping deep markup, is shown as source and reported `unsafe-after-reparse`; at `033b1e3` the same mail rendered as HTML. Failing closed is safe, but the report wrongly calls a harmless message unsafe, and the mail loses legibility (R7). | CONFIRMED (low) | Skeptic PoC at `aa1c98d`: `<p>x</p><noscript>` + N×`<div>` + `y` + N×`</div>` + `</noscript>`. N = 100, 505, 508, 509 render as HTML; N = 510 and 600 give `BodyFallback="unsafe-after-reparse"` with `<pre class="plain">`. The same N=510 body at `033b1e3` renders as markup. Mechanism: with scripting on, noscript is raw text and shallow; the scripting-off re-parse passes the 512 cap, and the depth error is reported as hostile. The finder's run matched at N=600. The trigger is rare. | open | open |
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
