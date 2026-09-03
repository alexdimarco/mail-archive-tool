# Friction review — schedule v2, incremental completeness, and the archive as inherited

> As of 2026-09-02, walked against the tree at `2c99b4a` with the real binaries
> (Linux, plus a Windows cross-build for the Task Scheduler entry). Method: the
> assurance-kit friction review run as a workflow — one walker per actor
> (new operator, scheduler, IMAP recoverer, reviewer, inheritor, Windows admin,
> M365 admin/operator, non-technical owner, Windows-Exchange user), each cell an
> actor × scenario, classified Type I (security-inherent), Type II
> (design-choice), Type III (buildable-away). The disposition of every finding
> is at the end of this document.


**Cells walked:** 54 &nbsp;·&nbsp; **Functioning:** 41 yes / 13 partial / 0 no &nbsp;·&nbsp; **Type I (security-inherent):** 4 &nbsp;·&nbsp; **Type II (design-choice, drives verdict):** 5 &nbsp;·&nbsp; **Type III (buildable-away backlog):** 26 &nbsp;·&nbsp; **No friction:** 19

**Verdict: SHIPPABLE** (design verdict) — with the 5 Type II items as a required design-backlog and the 26 Type III items as the operability backlog that must be burned down before the next release.

**Rationale.** No cell fails outright (zero "no"); every failure mode walked — bad flag, missing input, world-readable secret, concurrent run, dead pid, torn record, missing index, non-backup verb, expired M365 secret — refuses fail-closed with a typed non-zero exit that names the offending file/policy and the remediation. The refusal surfaces (`schedule`, secret-file validation, the lock) are the strongest in the tool and are Type I where they cost the operator effort. The 5 Type II findings are genuine and none are softened below, but each has a documented recovery path and none strand the user, so they lower the grade rather than block ship. The one that comes closest to blocking is finding #1 (Windows-Exchange `-auto` routing a live `.ost` into the reader the docs themselves flag as unreliable): for what is plausibly the *primary* audience, the headline command can dead-end and demand a full wizard restart. Treat #1 as release-gating for the Windows-Exchange path specifically.

---

## Cell table

| # | Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|---|
| 1 | new operator × first PST archive; read index.html + README.txt | yes | Self-explanatory. Nits: printed summary vocab (`skipped(seen)`, `non-html`, `no-body`) doesn't match README's `filled=/fillable=/terminal=/unknown=` + `Verification:`; README.txt says lock "held only while a run is in progress" but it persists on disk with a stale pid | III | Align README summary example with real tokens; note the lock persists |
| 2 | new operator × `status` after first archive | yes | Clean one-shot archive shows **Posture: WARN** solely for lack of a schedule — alarm word for a deliberate choice. `Last run:` timestamp is unlabeled local time while the rest of the archive is UTC | **II** | Separate "no schedule" into an INFO/advisory tier; label/print status timestamps in UTC |
| 3 | new operator × re-run incremental | yes | Idempotence obvious: `exported=0 skipped(seen)=17`, counts unchanged | none | — |
| 4 | new operator × mistype flag / forget `-out` / missing `-input` / bad `-mode` / bad `-since` | partial | Most refusals excellent. But missing `-input` prints `Exporting…` then a green-looking `Done. exported=0` **then** `FAILED` — reads as success; also creates the out dir, `.mailarchive.lock`, and a `failed` lastrun. `status` on that dir says "no archive … run an export first" — cannot surface a failed *first* run. Unknown flag dumps ~45 lines of usage and prints the error twice | III | Stat all `-input` before the lock/`Exporting`/`Done`; `status` falls back to lastrun; one-line unknown-flag error + "run -h", no duplicate |
| 5 | new operator × terminal `search` then `serve` (curl `/`, `/api/search`, non-loopback warn) | partial | `serve` strong: strict CSP + no-referrer + nosniff, escaped snippets, loud non-loopback warning, legible no-index refusal. But terminal `search` does **not** parse `from:`/`after:` tokens the README example uses — `from:support`→0 while `-sender support`→11, and the same query works via `/api`. Silent zero results, no hint | III | Share the token parser with the serve box, or fix the README example + warn on unrecognized `field:` barewords |
| 6 | new operator × delete an .html, `reindex` | yes | Exactly per R13: stale until reindex, then `kept=16 pruned=1`, survivors stay searchable | none | — |
| 7 | new operator × Quick Start's literal `mailarchive -auto -out ./archive` | partial | `-auto` (the first documented command) immediately reads/exports **every** discovered store — real Thunderbird IMAP, Local Folders, Evolution — no preview, no size estimate, no confirm (grew to 472 MB before killed). No `-dry-run`/`-list` exists to see what it would grab | III | Add `-list`/`-dry-run`; have `-auto` list stores and confirm on a TTY (`-y` to skip); reorder README so `-input` leads |
| 8 | new operator × paste `status`'s WARN remedy to schedule a `-input`-built archive | partial | `schedule` itself is safe-by-default. But `status`'s remedy always says `-auto -install` regardless of origin; on this box that folds every Thunderbird/Evolution mailbox into a PST-only archive — a different, larger job | III | Derive the remedy from the recorded originating job (`-input`/`-auto`), or phrase it generically |
| 9 | new operator × read GUI wizard (`main.go`) for a non-technical actor | yes | GUI closes the CLI's gap: auto path *lists* discovered stores with "All of them" before exporting, fails safe on none-found, explains skipped mode, cancellable, warns on Downloads/temp binaries | none | Optional: default auto picker to no selection so a fast click-through can't grab everything |
| 10 | scheduler × preview nightly `schedule -auto` | yes | Entry understandable, log path printed twice. But cadence shown only as raw `0 2 * * *` (the *install* message translates it; the preview — the decision moment — doesn't); nothing says a failed cron run is silent unless MAILTO is set | III | In `schedule.Preview()` cron branch, print `# daily at 02:00` + a failure-visibility note |
| 11 | scheduler × `--` export job + named weekly variant | yes | Clean: paths canonicalized, `--` not re-serialized, weekly→`* * 0`, name flows to marker+log. Same cron-not-translated nit; `-mode full` weekly accepted silently (re-exports everything each run) | III | Same cadence gloss; optionally warn when `-mode full` is scheduled |
| 12 | scheduler × `--` graph job, 0600 secret (preview) | yes | Secret validated *now* (regular file, 0600, ≤4KB); value never printed, only the path. Secret-file ceremony is the control | I | None (optionally note perms are re-checked at run time) |
| 13 | scheduler × same graph job, 0644 secret (refusal) | yes | Model refusal (R12): names file, exact mode (0644), exact `chmod 600 <path>`, refuses before install | I | None |
| 14 | scheduler × refuse `schedule -- serve …` (also search/status/schedule) | yes | Typed, exit 1, names valid alternatives (export/graph/reindex) | none | None |
| 15 | scheduler × refuse flat `-enable-offline` / `-sync-wait` on schedule | yes | Refuses interactive one-time-prep flags, points to README IMAP and the correct next step. Prep is inherently a hand step | I | Optional: give the exact README anchor/URL |
| 16 | scheduler × mix flat job flag with `--`; confirm pre-`--` schedule flags accepted | yes | Names the stray flag, `-interval`/`-at`/`-name` correctly not treated as stray. When the same flag is on both sides, "move -out after --" misleads (it's already there — the fix is to drop the leading one) | III | Phrase as "drop the `-out` before `--` (the job after `--` already carries it)" |
| 17 | scheduler × bad `-name` (illegal char, >40 chars) | yes | Both refusals state the constraint and the why (names files + a scheduler entry) | none | None |
| 18 | scheduler × graph `--` job, missing secret file / no secret flag | yes | No-flag message excellent (no env for a secret; exact remedy). But missing-file from the same validator leaks raw Go `lstat` text, prints path twice, no remedy — inconsistent with its siblings | III | In `readSecret`, special-case `os.ErrNotExist`: "…does not exist — create it readable only by you (`chmod 600 PATH`)"; drop the raw lstat + duplicate path |
| 19 | scheduler × `-remove` by `-out` / `-name` / neither (crontab sandboxed) | partial | Reports "Removed scheduled backup …" + exit 0 **even when nothing was ever installed** — can't tell cleanup from no-op; runs `crontab -` unconditionally, materializing an empty crontab as a side effect of a no-op. Neither-arg case refused correctly | III | Check descriptor + `schedule.Query(name)` first; if neither present, print "nothing to remove" and skip the crontab rewrite |
| 20 | scheduler × `status` before any schedule | yes | Names the missing piece, hands a copy-pasteable install command + GUI equivalent | none | None |
| 21 | scheduler × `status` for a FAILED last run (AADSTS/401) | yes | RED with verbatim failure + failure-specific remedy (rotate the secret in Entra); second WARN flags the never-installed descriptor | none | None |
| 22 | scheduler × `status` for a RUNNING record whose pid is dead | yes | Fails closed: judged a crashed run (RED), names causes (crash/kill/power loss) + remedy | none | None |
| 23 | scheduler × `status` for an OLD run (10 days, daily schedule) | yes | Staleness caught + WARNed. But "is the machine on and logged in at 02:00?" is macOS/Windows framing; Linux cron runs while logged out — misleading for this actor's OS | III | Make the staleness remedy OS-aware in `health.Assess`: Linux → "was the machine on (and crond running)?"; keep "logged in" for launchd/schtasks |
| 24 | scheduler × `status` edges: fresh, empty, nonexistent, torn record | yes | Every edge fails closed with a remedy; `status` is read-only. Wart: torn-record WARN surfaces the raw `encoding/json` error string | III | Replace the raw json error in the `LastRunUnreadable` WARN with "unreadable (corrupt or truncated)"; keep "the next run rewrites it" |
| 25 | scheduler × read README schedule/status + Windows notes as a Linux operator | yes | Lifecycle is real and internally consistent; per-OS notes clearly labelled. Gaps for a Linux reader: the Linux note never states cron runs logged *out* (contradicting the staleness WARN's "logged in"); the schedule section doesn't foreshadow `SchedulerUnavailable` on a host with no `crontab` | III | Add a Linux per-OS line (cron runs whether or not you're logged in; no-crontab host → `status` reports scheduler unavailable); align the staleness WARN wording |
| 26 | scheduler × incidental: `.mailarchive.lock` persists after a clean run | yes | Functionally correct (OS flock released on exit; lingering file deliberate, doesn't block re-runs). But README output-layout line reads as "the file exists only during a run"; a stale-looking lock naming a dead pid misleads | III | Reword README line 119 to "lock file (persists; the actual lock is an OS flock released when the run ends)" |
| 27 | imap-recoverer × first maildir export (empty-attachment, no-body); read report | yes | Report exemplary (one row per gap, class, folder, UTC date, subject, missing item, html path). But the run prints **two** `Verification:` lines with overlapping/differing vocab (findings vs messages; fillable vs "still missing"); the missing-body detail column is the bare token `body` | III | Collapse the two Verification lines (or mark the second a continuation); render the body gap as "(message body)" |
| 28 | imap-recoverer × `status` after first export | yes | Strongest legibility surface: completeness, last run, schedule, posture, a concrete remedy per WARN. Exit 0 while reporting is per contract | none | — |
| 29 | imap-recoverer × incremental again, source unchanged | partial | *Why* gaps persist is explained. But the run doesn't show that it re-examined the two fillable gaps and found nothing — `Stats.Retried`/`StillIncomplete` exist but are printed nowhere, so `skipped(seen)=1` doesn't reconcile to `manifest=3` and "re-checked, still absent" is indistinguishable from "skipped entirely" | III | Print re-examination in the summary, e.g. `re-examined=2 still-missing=2` from `Stats.Retried`/`StillIncomplete` |
| 30 | imap-recoverer × fill the empty attachment, re-run | yes | Fill surfaced in all three places: `filled=1 attachments=1`, report row drops + sibling zip appears, `status` shows `filled 1` / `1 still fillable` | none | — |
| 31 | imap-recoverer × re-run with `-since 30d` excluding the 2024 messages | yes | README promise proven: a 2024 gap re-examined + filled under a 30d window. Same invisibility as #29, worse under `-since` (`skipped(date)=0` always, no signal the window spared the old gap) | III | Same summary fix as #29; optionally note N gaps re-examined despite the window |
| 32 | imap-recoverer × legacy v1 manifest ("not yet re-examined") | yes | Migration truthful: load-time line, `status` count, WARN with remedy; `status` read-only. Nits: run *summary* doesn't show a residual countdown (sentinels clear in-run) though README says "the summary … count them down"; `status`'s Incomplete line cites `attachments-report.tsv` even when the file is absent | III | Reword README to attribute the countdown to `status` (and interrupted runs); in `status`, cite the report path only when fillable+terminal>0 |
| 33 | imap-recoverer × second run while another holds the lock (6000-msg background) | yes | Fail-closed per R5: refuses non-zero, names the live holder (pid, UTC start, host) + exact lock path, writes nothing. Nit: trailing "… : held by another run" restates the sentence | I | Keep the refusal; cosmetically drop the redundant trailing `: held by another run` |
| 34 | reviewer × README "Incremental model" + "Verifying completeness" vs behaviour | partial | Every *mechanical* promise reproduced (re-examine, `-since` regardless, report regenerated/removed, migration, lock refusal). Clarity drift: README:~444 lists four tokens as one summary but no single printed line carries all four (three live on a conditional second line with different words); README:~236 credits "the summary and status" with the legacy countdown when only `status` counts down | III | Align README:~444 with the two lines the tool prints (or unify them); correct README:~236 to `status` only |
| 35 | inheritor × cold start: is there a README.txt, self-standing? | yes | Genuinely sufficient (layout, zips, `.eml`/`-raw`, inert pages, UTC convention, search.db schema, rg/Windows-Search fallback). Same lock-line nit as #1/#26 | III | Reword the README.txt housekeeping lock line (`internal/app/readme.go:45`) to "may remain after a run; its presence does not mean a run is active" |
| 36 | inheritor × navigate root → folder → message → back, HTML only | yes | Every hop a real relative link resolving on `file://`; `RootRelPrefix` depth correct for nested folders | none | — |
| 37 | inheritor × attachments named + reachable (mbox: report.pdf, notes.txt, inline logo) | yes | Names inline on the page, sibling zip one click away, zip name derived from the message name, inline-consumed image omitted from list + zip | none | — |
| 38 | inheritor × same on the PST archive | yes | Same contract holds; From/Bcc/Date/Message-ID present, names match the zip | none | — |
| 39 | inheritor × times: UTC in names/tables vs original offset on the page (−0800 msg) | partial | Conversion correct + column labelled "Date (UTC)". But folder table (`2024-07-19` UTC) and message page (`…−0800`, `2024-07-18`) show **different calendar days** for the same email; nothing on the page restates the UTC instant, nothing in the table hints at local time — only the README reconciles them | **II** | On the message page show the UTC instant alongside the offset time (e.g. `… −0800 (2024-07-19 07:15 UTC)`), or add a `title=` tooltip with the original local time to the table cell |
| 40 | inheritor × large-folder pagination + pager link text (read `pages.go`) | yes | Correct, never truncates; "newer/older" matches newest-first sort. But single-step only — no numbered pages/first/last/jump; a >5000-msg folder needs one click per page to reach the middle. In-page filter sees only the current page | III | When `Pages` is large, add a compact numbered pager (first/last + a few numbers) |
| 41 | inheritor × `-raw` `.eml` experience | yes | Unmodified byte stream, discoverable by `ls` and an on-page link, documented; PST items have none (stated) | none | — |
| 42 | inheritor × folder search box with no server running | partial | Works offline, honestly labelled a **Filter** (no false promise). But two unanticipated scope caveats: matches only visible columns (not bodies), and only rows on the current page (can't reach another page of the same folder) | **II** | Keep the honest "Filter" label; for paginated folders note in the placeholder/help that it covers this page only, and point to `serve`/`rg` for whole-archive body search |
| 43 | inheritor × does exported HTML reach the network or run script? | yes | Message pages inert as advertised: strict CSP, no script, no remote loads, inline images as `data:`. Only scripted surface is the tool-generated folder listing (no network); localhost text is an example. Honest nuance: mail bodies keep original clickable http/mailto links (nothing auto-loads) | none | — |
| 44 | inheritor × message with a dangling `cid:` inline image | partial | Honest + non-crashing (broken-image placeholder under `img-src data:`, surrounding text intact, gap in the TSV). But the page gives no inline hint the image was unrecoverable — looks like a rendering failure; the explanation lives only in the TSV | III | Replace an unresolved `cid:` `<img>` with an inline caption, e.g. "[inline image not included in the archived message]" |
| 45 | inheritor × non-Latin (Cyrillic) subject findable/openable in browser + shell | yes | Letters preserved on disk, in the table link text, in title/header; href percent-encoded so it resolves | none | — |
| 46 | inheritor × count steps "folder in hand" → "found a specific attachment" | yes | ~4 steps either way; a 📎 column reaches the zip without opening the message, zip name guessable from the shell. No dead ends | none | — |
| 47 | inheritor × a full-HTML-document message renders its metadata header UNSTYLED | partial | Purely cosmetic — every field/link/attachment present — but the full-`<html>` branch in `html.go` never injects `docTemplate`'s `<style>`, so From/To/Bcc render as a bare `<dl>`, visibly different from styled plain/fragment messages; reads as "unfinished" | III | Inject the `.mailarchive-header` CSS (a scoped `<style>` in the metas) in the full-HTML branch of `RenderWith` |
| 48 | windows-admin × README `.ost`/Exchange row + Windows schedule notes; enumerate by-hand steps | partial | All facts documented but **scattered**: completeness prereq in the row, "answer the programmatic-access prompt once by hand BEFORE scheduling" only in Sources, logged-in/console-flash only in the schedule section. The row's "Keep it current" cell doesn't restate the interactive-first requirement, so a top-to-bottom reader can schedule a COM job that never cleared the prompt → the 02:00 job stalls on an unanswerable dialog, surfacing only later as RED "never finished" | III | Add to the `.ost` row's "Keep it current" cell: "run `mailarchive -outlook` once by hand first to clear Outlook's one-time programmatic-access prompt; a scheduled run cannot answer it"; optionally have `schedule … -outlook -install` print the reminder |
| 49 | windows-admin × cross-compile + render the real Task Scheduler entry (paths with spaces + `&`) | yes | Wrapper text exemplary (self-labelling rem lines, every token quoted, stderr → sibling `.stderr.log`; `&` stays inside quotes). One asymmetry: CLI **always** wraps on Windows (`main.go:454`) → a brief console flash *every* run even for a trivial command, while the GUI wraps only when `TaskRunLength > 261` — same job, two behaviours by surface | **II** | Defensible as-is (CLI already lives in a console + wants the stderr sink). If the flash is unwanted, let the CLI skip the wrapper when `TaskRunLength ≤ 261`, matching the GUI |
| 50 | windows-admin × walk GUI wizard, count dialogs, hunt dead ends / inapplicable questions | yes | Counts reasonable (~8–10); no nonsensical questions — "mail app open?" suppressed for COM + directory stores, mode skipped after forced-Full prep. Soft dead end: auto-detect finding nothing loops back to the type list, where a cold user must know their mail layout | none | Optional: link the README "Start here" table in the found-nothing Warning |
| 51 | m365-admin × walk `graph-app-setup.md`; run `graph` against a bad tenant/app; secret-file refusals | yes | Doc clear + minimal + revocable; token-time failures name AADSTS code + trace/correlation IDs + timestamp; secret-file refusals fail-closed (typed exit, name file + remedy, never echo the secret). But: a failed `graph` run still writes a full **empty** archive scaffold (README.txt/index.html/search.db/manifest/lastrun) — looks populated, contains nothing; and the AADSTS detail is dropped from lastrun (`error="1 mailbox(es) failed"`), so `status` shows a generic RED and `health.looksLikeAuthFailure` can't fire the expired-secret remedy — the single most likely months-later failure | III | Propagate the per-mailbox auth error / an is-auth flag into the last-run record so `status` fires the expired-secret hint; defer writing the scaffold until ≥1 mailbox succeeds, or point `status`/summary at `<out>/<name>.log` for AADSTS detail |
| 52 | m365-operator × preview a scheduled `graph` backup with a chmod-600 secret file | yes | Preview references the secret file *path*, never the value; validated now with the same `readSecret()` the job uses; "This was NOT applied" + printed log path make dry-run/apply unambiguous. Strongest surface in the review | none | None |
| 53 | non-technical owner × judge `status` + run-summary wording | partial | (1) Clean fully-captured first export shows **WARN** solely for no schedule — same alarm word as real incompleteness. (2) "Incomplete:" vocab ("fillable", "source-empty (terminal)", "not yet re-examined") is opaque jargon with no plain-language gloss. (3) Raw run summary (`skipped(seen)`, `non-html`, `no-body`, `inline`) cryptic (GUI summary is friendlier) | III | Keep a clean-but-unscheduled archive at GREEN with an informational tip (or add a NOTE tier below WARN); gloss the Incomplete line inline — purely cosmetic-string changes |
| 54 | windows-exchange user × follow headline `mailarchive -auto` where the only mailbox is a live Exchange `.ost` | partial | The most prominent entry point (`-auto`, GUI's default "Auto-detect") routes a live Exchange `.ost` straight into the go-pst **direct reader** — the very path Sources warns "vary by Outlook build and some can't be parsed directly" — instead of the reliable COM path (reachable only by explicitly choosing `-outlook` / "Outlook account (via Outlook app)"). So the primary Windows-Exchange user's happy path can dead-end into a failure + full wizard restart. Recovery hint exists but only *after* the failed run, only on Windows, only when the error text contains ".ost" | **II** | On Windows with classic Outlook present, have auto-detect prefer/offer the COM path for `.ost` caches (or transparently fall back to `-outlook` when a discovered `.ost` fails `DataFileReadable`); at minimum surface the COM suggestion *up front* when a live `.ost` is auto-detected, not only after failure |

---

## Findings to fix

Every Type II and worth-fixing Type III item, most valuable first. Type II is marked; the rest are Type III. Related cells are consolidated where the fix is one change.

1. **[Type II · cell 54] Windows-Exchange `-auto` routes a live `.ost` into the fragile direct reader.** The flagship command, for what is plausibly the primary audience, can fail on the very path the docs flag as unreliable, with recovery only after a failed run + wizard restart.
   - *Change:* `internal/app` (`DiscoverInputs`) + the `-auto` routing in `cmd/mailarchive/main.go` and the GUI auto path. On Windows with classic Outlook present, route a discovered `.ost` through the COM path (`-outlook`) rather than `go-pst`; or probe `DataFileReadable` first and fall back to COM on failure. At minimum, when a live `.ost` is auto-detected, print the COM-path suggestion up front (before attempting the direct read), not only in the post-failure error.

2. **[cell 4] The missing-`-input` path claims success then fails, and leaves a half-archive `status` can't explain.** A newcomer reads `Done. exported=0` and believes it worked; then `FAILED`; then `status` on the litter says "no archive … run an export first."
   - *Change:* In `internal/app/run.go`, `stat()` every `-input` path *before* acquiring the lock, printing `Exporting to …`, or emitting any `Done.` summary — fail immediately with a typed non-zero, creating no out dir, no `.mailarchive.lock`, no lastrun, exactly as `-mode`/`-since` already validate up front. Separately, make `status` fall back to the `.mailarchive-lastrun.json` record when no manifest exists, so a failed *first* run is reportable.

3. **[cells 5, 34] Terminal `search` silently ignores the `from:`/`after:` tokens the README teaches.** `search … from:support release` returns 0; `-sender support release` returns 11; `/api/search?q=from:support` matches. A copied README example yields zero results with no error.
   - *Change:* Share one query parser between the serve box and the terminal `search` subcommand so `from:`/`after:`/`before:` behave identically; and/or warn when a bareword looks like an unrecognized `field:token`. If not unified, fix README.md:262 (see Documentation corrections #3).

4. **[Type II · cells 2, 53] A clean, deliberately one-shot archive reports `Posture: WARN` purely for lack of a schedule.** The same alarm word used for genuine incompleteness is the first thing every first-time user (and the non-technical owner) sees after a flawless run; it also silently uses unlabeled local time.
   - *Change:* In `internal/health`, split "no schedule recorded" out of WARN into an INFO/advisory tier (or let a run record explicit one-shot intent so a clean unscheduled archive reports GREEN with a tip). Label `status` timestamps with their zone, or print them in UTC to match the archive. Gloss the `Incomplete:` line inline ("still fillable = not downloaded yet; fills on the next run"; "source-empty = the mailbox has no such content").

5. **[cell 7] `-auto` has no preview and no confirmation — it exports every discovered mailbox the moment it runs.** The first documented command silently starts a multi-hundred-MB, multi-account export.
   - *Change:* Add `-list`/`-dry-run` to `cmd/mailarchive` that prints the stores `-auto` found (with rough sizes) and exits without exporting; on an interactive TTY, have `-auto` print the discovered store list and require confirmation (skippable with `-y` for scheduled runs).

6. **[cell 51] A failed `graph` run writes an empty archive scaffold and drops the AADSTS detail, so `status` can't show the expired-secret remedy.** Months later, an expired client secret (the ≤24-month cap) is the most likely failure and the one `status` is least able to explain.
   - *Change:* Propagate the per-mailbox auth error (or an is-auth-failure flag) into `.mailarchive-lastrun.json` so `health.looksLikeAuthFailure` matches and fires the existing rotate-the-secret hint. Defer writing the archive scaffold (README.txt/index.html/search.db/manifest) until at least one mailbox succeeds, or have `status`/the run summary point at `<out>/<name>.log` for the AADSTS detail.

7. **[cell 8] `status`'s WARN remedy hard-codes `-auto -install` regardless of how the archive was built.** Pasted verbatim on a `-input`-built archive, it schedules a job that folds in every discovered mailbox — a different, much larger job.
   - *Change:* Record the input/`-auto` choice in the lastrun record (mode/exe are already stored) and have `internal/health` shape the remedy to match the originating job, or phrase it generically ("schedule the same job that made this archive").

8. **[Type II · cell 39] The folder table and the message page show different calendar days for the same email.** UTC in the table (`2024-07-19`) vs the original `−0800` offset on the page (`2024-07-18`); only the README reconciles them.
   - *Change:* In `internal/html` (`RenderWith`) show the UTC instant beside the original-offset time on the message page, e.g. `Thu, 18 Jul 2024 23:15:00 −0800 (2024-07-19 07:15 UTC)`; or give the folder-table date cell a `title=` tooltip with the original local time in `internal/pages` templates.

9. **[cell 19] `-remove` claims "Removed" and exits 0 even when nothing was installed, and rewrites the crontab unconditionally.** The operator can't distinguish cleanup from a no-op, and a no-op remove materializes an empty crontab on a host that had none.
   - *Change:* In the `schedule -remove` path, check the descriptor and `schedule.Query(name)` first; if neither the managed marker nor the descriptor is present, print "no schedule was installed for `<archive/name>` — nothing to remove" and skip the `crontab -` rewrite entirely.

10. **[cell 48] The Windows `.ost` scheduling path can install a COM job that never cleared the programmatic-access prompt.** A top-to-bottom reader of the "Start here" row schedules a job that then stalls at 02:00 on an unanswerable Outlook dialog, surfacing only later as a RED "never finished."
    - *Change:* Doc fix in README.md:33 (see Documentation corrections #1); optionally have `schedule … -outlook -install` print a reminder that `mailarchive -outlook` must already have been run interactively.

11. **[cells 1, 27, 29, 31, 34] The completeness summary is illegible and drifts from the docs.** The run prints two overlapping `Verification:` lines with different vocab; no single printed line carries the four tokens the README lists; the re-examination that incremental runs perform is invisible (`Stats.Retried`/`Stats.StillIncomplete` are computed but printed nowhere), so the counts don't reconcile; the body-gap detail is the bare token `body`.
    - *Change:* In `cmd/mailarchive/main.go` / `internal/app/run.go`, collapse the two `Verification:` lines into one (or make the second an explicit continuation), and append `re-examined=<Stats.Retried> still-missing=<Stats.StillIncomplete>` to the summary so an unchanged incremental run positively confirms it looked again. Render the body gap in `attachments-report.tsv` as "(message body)" rather than `body`. Align README (see Documentation corrections #4).

12. **[cell 18] `readSecret`'s missing-file message is worse than its siblings.** It leaks the raw Go `lstat` text, prints the path twice, and offers no remedy — unlike the world-readable and no-flag cases from the same validator.
    - *Change:* In `readSecret`, special-case `os.ErrNotExist`: "client secret file `PATH` does not exist — create it readable only by you (`chmod 600 PATH`)"; do not surface the raw lstat error or repeat the path.

13. **[Type II · cell 49] The CLI always wraps on Windows, so a CLI-scheduled job flashes a console every run; the GUI wraps only past 261 chars.** Same job, two behaviours by surface.
    - *Change:* Defensible as-is (the CLI already lives in a console and wants the stderr sink). If the nightly flash is unwanted, in `cmd/mailarchive/main.go:454` let the CLI skip the wrapper when `TaskRunLength ≤ 261` (matching the GUI's `offerSchedule`), falling back to the wrapper only for length or an explicit stderr sink.

14. **[cells 23, 25] The staleness WARN says "logged in," which is false for Linux cron.** cron runs whether or not anyone is logged in; the WARN and the README together send mixed signals about whether the operator must stay logged in.
    - *Change:* Make the staleness remedy OS-aware in `health.Assess`: on Linux say "— was the machine on (and crond running) at 02:00?"; keep "logged in" only for launchd/schtasks. Add the matching README line (Documentation corrections #6).

15. **[cells 10, 11] The `schedule` preview — the moment of decision — never translates the cron cadence or explains how a failed run surfaces.** The *install* message translates `0 2 * * *` to "daily at 02:00"; the preview shows only the raw field.
    - *Change:* In `schedule.Preview()`'s cron branch, print a one-line gloss above the cron line (e.g. `# daily at 02:00`) and a note that a failed run is surfaced only via `mailarchive status` or cron `MAILTO`. Optionally note when `-mode full` is scheduled that every run re-exports in full.

16. **[cell 44] A dangling `cid:` inline image looks like a rendering failure with no inline explanation.** The only explanation is a TSV row a casual reader won't correlate.
    - *Change:* In `internal/html`, replace an unresolved `cid:` `<img>` with a small inline placeholder/caption, e.g. "[inline image not included in the archived message]," so the page explains itself.

17. **[cell 47] Full-HTML-document messages render their metadata header unstyled**, visibly different from plain/fragment messages and reading as "unfinished."
    - *Change:* In `html.go`'s full-`<html>` branch of `RenderWith`, inject the `.mailarchive-header` CSS (a scoped `<style>` in the injected metas) so the header box is consistent regardless of source body type.

18. **[cell 40] Folder pagination is single-step only.** A folder needing many pages forces one click per page to reach the middle, and the in-page filter can't see other pages.
    - *Change:* In `internal/pages` templates, when `Pages` is large add a compact numbered pager (first/last + a few page numbers).

19. **[cell 32] The legacy "count them down" is attributed to the run summary but only `status` does it, and `status` cites a report file that isn't there.** With only "not yet re-examined" entries, `attachments-report.tsv` does not exist on disk, yet `status`'s Incomplete line cites its path.
    - *Change:* In `status`, omit or caption the report path when the report file is absent (cite it only when fillable+terminal > 0). Reword the README (Documentation corrections #5).

20. **[cell 16] "move `-out` after `--`" misleads when the flag is present on both sides of `--`.** It is already after `--`; the fix is to drop the leading one.
    - *Change:* When a named stray is also present in the post-`--` job, phrase it as "drop the `-out` before `--` (the job after `--` already carries it)."

21. **[cell 24] The torn-last-run WARN surfaces the raw `encoding/json` error string.** Legible-ish but not operator-friendly.
    - *Change:* Replace the raw json error in the `LastRunUnreadable` WARN with "unreadable (corrupt or truncated)"; keep "the next run rewrites it."

22. **[cells 1, 26, 35] `.mailarchive.lock` persists after a clean run, contradicting both READMEs.** A stale-looking lock naming a dead pid can read as a stuck run.
    - *Change:* Reword README.md:119 and the embedded README.txt template (`internal/app/readme.go:45`) to say the file persists and its presence does not mean a run is active (see Documentation corrections #2). Alternatively, truncate/empty the lock file on release so no stale pid is left behind.

23. **[Type II · cell 42] The folder filter's scope isn't stated: it matches only visible columns and only the current page.** The honest "Filter" label prevents a false promise, but a user won't anticipate that a match on another page of the same folder is invisible.
    - *Change:* Keep the "Filter" label. In `internal/pages` templates, for paginated folders note in the placeholder/help that the filter covers this page only, and point to `serve`/`rg` for whole-archive body search.

---

## Documentation corrections

Exact passages that were wrong or unclear, with corrected wording.

**1. README.md:33 — "Start here" `.ost`/Exchange row, "Keep it current" cell.** The row lets a top-to-bottom reader schedule a COM job that never cleared Outlook's one-time prompt.

- Current: `` `mailarchive schedule -out ./archive -outlook -install` — runs while you are logged in ``
- Corrected: `` `mailarchive schedule -out ./archive -outlook -install` — runs while you are logged in. Run `mailarchive -outlook` once by hand first to clear Outlook's one-time "allow programmatic access" prompt; a scheduled run cannot answer it. ``

**2. README.md:119 and internal/app/readme.go:45 — the lock line reads as "exists only during a run."**

- Current (README.md:119): `.mailarchive.lock                                          # held only while a run is in progress`
- Current (readme.go:45): `.mailarchive.lock            held only while an archive run is in progress`
- Corrected (both): `.mailarchive.lock  # lock file; may remain after a run — its presence does not mean a run is active (the real lock is an OS flock released when the run ends)`

**3. README.md:262 — the Terminal search example uses tokens the terminal `search` subcommand does not parse.** (Only needed if the parser is not unified per finding #3.)

- Current:
  ```sh
  mailarchive search -out ./export from:bob invoice
  mailarchive search -out ./export -folder Inbox -after 2025-01-01 contract
  ```
- Corrected:
  ```sh
  mailarchive search -out ./export -sender bob invoice
  mailarchive search -out ./export -folder Inbox -after 2025-01-01 contract
  ```
  with a following note: "Inline `from:`/`after:`/`before:` tokens are understood by the `serve` search box and `/api/search`; the terminal `search` command uses the `-sender`/`-after`/`-before`/`-folder` flags instead."

**4. README.md "Verifying completeness" (the sentence at ~line 444) — describes one summary line that the tool does not print as one line.**

- Current: "The run summary shows `filled=… fillable=… terminal=… unknown=…` and a `Verification:` line pointing at the report."
- Corrected: "The `Done.` summary line shows `filled=…` among its counts. When any gaps exist, a separate `Verification:` line reports how many messages are still missing content, are source-empty (terminal), or are not yet re-examined, and points at `attachments-report.tsv`." (Or unify the tool's two `Verification:` lines to match the original prose — see finding #11.)

**5. README.md "Incremental model" (the sentence at ~line 236) — credits the run summary with the legacy countdown.**

- Current: "on first use every entry is marked *not yet re-examined* and the next incremental runs check each once (the summary and `status` count them down)."
- Corrected: "on first use every entry is marked *not yet re-examined*; `status` counts them down as incremental runs check each once (a single run typically clears the whole set, so the countdown is visible in `status`, or across runs only if a run is interrupted)."

**6. README.md "Per OS → Linux (cron)" (lines 410–412) — never states cron's logged-out behaviour, and doesn't foreshadow `SchedulerUnavailable`.**

- Current: "**Linux (cron):** the job logs itself; anything it cannot log (a crash) reaches cron's mail — set `MAILTO` in your crontab if you want that pushed to you."
- Corrected: append: "cron runs whether or not you are logged in, as long as the machine is on and `crond` is running. On a minimal host with no `crontab` binary, `status` reports that this host's scheduler cannot be queried."

**7. README.md:51 (Quick start) / lines 60–61 (Start here note) — `-auto` is presented as the headline first command with no statement that it archives *every* discovered mailbox.**

- Current (line 51): `mailarchive -auto -out ./archive                       # first archive (incremental by default)`
- Corrected: add a caveat line under the block, e.g. "`-auto` discovers and archives **every** Outlook/Thunderbird/Evolution store it finds; run `mailarchive -auto -list -out ./archive` first to see what it would grab, or use `-input` to archive one store." (Pairs with finding #5; if `-list` is not built, drop that clause and keep the warning.)

No corrections are needed to `docs/graph-app-setup.md` — the walk (cell 51) found it clear, minimal, and revocable; that cell's defects are code behaviour, not documentation.

---

## Disposition (2026-09-03)

Every Type II and Type III finding above was either built or explicitly left,
as recorded here. Merge commits on `main`: search/run-flow `dd6963d`, GUI
`52af13e`, status/schedule `330b8c1`, pages `54b4883`; the earlier renderer and
integrity fixes are in `11f4d35`.

| Finding | Disposition |
|---|---|
| 1 (Type II, .ost via -auto) | Built, reduced: an up-front advisory names `-outlook` before the first read when a live `.ost` is auto-detected on Windows with classic Outlook (MA-106); the GUI's auto-detect steers to the Outlook-app path in the same case (MA-126). Routing itself is unchanged. |
| 2 (missing -input claims success) | Built: inputs are checked before the lock, the out dir, or any output; nothing is created on refusal (MA-103). `status` reports a failed first run from the last-run record when no manifest exists (MA-111 block). |
| 3 (terminal search ignores tokens) | Built: one query parser serves the terminal `search`, the serve box and `/api/search`; partial dates (`2025-01`, `2025`) apply a bound (MA-101). |
| 4 (Type II, WARN for no schedule) | Left as WARN: MA-75 enumerates it and the product review ruled the reclassification an operator decision; the WARN now acknowledges external schedulers, `status` labels its timestamps, and the Incomplete line carries a plain-language gloss. |
| 5 (-auto has no preview) | Built: `-list` previews the discovered stores with sizes and exits without exporting; the README says `-auto` archives every store it finds (MA-105). |
| 6 (failed graph run: empty scaffold, no AADSTS detail) | Built: the first per-mailbox error (AADSTS included) is the last-run error, so `status` fires the rotate-the-secret remedy; no scaffold is written when no mailbox succeeded (MA-107). |
| 7 (remedy hard-codes -auto) | Built: the last-run record carries the job's canonical arguments and the remedy repeats them; older records get the generic phrase (MA-111). |
| 8 (Type II, UTC vs offset days) | Built earlier: the message page shows the UTC instant beside the original-offset time (`11f4d35`, MA-90). |
| 9 (-remove claims Removed on a no-op) | Built: a no-op remove says so and does not rewrite the crontab (MA-112). |
| 10 (COM job without the one-time prompt) | Built: README row + the reminder printed by `schedule … -outlook -install` (MA-116). |
| 11 (illegible completeness summary) | Built: one Verification line with re-examined counts; the body gap reads "(message body)"; README aligned (MA-104, MA-71). |
| 12 (readSecret missing-file message) | Built earlier (`11f4d35`, MA-74). |
| 13 (Type II, CLI always wraps on Windows) | Left as designed: the wrapper is the stderr sink and is codified in R14; the GUI's headless job is the console-free path. |
| 14 (staleness says "logged in" on Linux) | Built: OS-aware wording, README Linux note (MA-113). |
| 15 (preview shows raw cron) | Built: cadence gloss, failure-visibility note, `-mode full` note (MA-114). |
| 16 (dangling cid: image) | Built earlier: inline placeholder caption (`11f4d35`, MA-80). |
| 17 (unstyled header on full-HTML mail) | Built earlier: the page is always the tool's document (`11f4d35`). |
| 18 (single-step pager) | Built: numbered pager when a folder has more than three pages (MA-119). |
| 19 (status cites an absent report) | Built: the path is cited only when the file exists (MA-115). |
| 20 (misleading "move -out after --") | Built: "drop the -out before --" when the job already carries it (MA-116). |
| 21 (raw json error in the torn-record WARN) | Built (MA-117). |
| 22 (lock line reads as "exists only during a run") | Built: README and README.txt reworded; both logs listed. |
| 23 (Type II, filter scope) | Built: paginated folders say "this page only (N of M)" and point to `serve`/grep (MA-120). |
| Doc corrections 1–7 | All applied in the merges above (1 → `330b8c1`; 2, 4, 5, 7 → `dd6963d`; 3 → made moot by the shared parser; 6 → `330b8c1`). |
