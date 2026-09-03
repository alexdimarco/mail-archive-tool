# Friction review — archive fidelity and the surfaces built after schedule v2

> As of 2026-09-03, walked against the tree at `3b4a191` with the built binaries
> (Linux, plus Windows cross-builds). Method: the assurance-kit friction review
> run as a workflow — five walkers (archivist, upgrading operator, power user,
> Windows reader, inheritor), each cell an actor × scenario executed for real.
> The verdict below is the walk's own (HIGH_FRICTION_NOT_SHIPPABLE); its three
> verdict-driving findings were also confirmed by the adversarial pass filed as
> `docs/review-fidelity-adversarial.md`. The disposition of every finding is at
> the end of this document.

# Fidelity & friction review — mail-archive-tool

*Process: `../assurance-kit/process/friction-review.md`. Each cell was walked as the actual actor — commands typed, errors read, steps counted — not judged in the abstract.*

**Cells walked:** 42
**Functioning:** 27 yes · 13 partial · 2 no
**Friction findings by type:** 1 Type I · **6 Type II** · 27 Type III · 8 clean
**Verdict:** **HIGH_FRICTION_NOT_SHIPPABLE**

Two cells do not function (both Type II) and four more partially fail on Type II grounds. The verdict is driven by three findings that undermine the archiver's central promise — *that you can trust the archive over time*:

- **#29 (headline):** the same physical store re-archived under a different `-input` **path spelling** silently **doubles** the archive (17→34 messages, two store dirs), breaking documented invariants **R2/R3** (incremental idempotence / stable identity). It is **triggered by the product's own surfaces** — `status`'s no-schedule remedy absolutises `-input`, and the GUI writes absolute-path job files — so a user who first archived with a relative path and then pastes the tool's own remedy gets a nightly backup that doubles the archive on its first run. No surface warns; `verify` attests exit 0.
- **#7:** a scheduled `verify` writes no run record, so a **NOT-ATTESTED** result leaves every standing surface (`status` "Last run: ok", Posture WARN-for-no-schedule, "Fixity: N of N carry digests") looking healthy. The README's own advice is to schedule weekly verify, yet the tool cannot answer "did last week's check pass?" without opening a log file — and the schedule preview's printed promise that failures "surface via `mailarchive status`" is **untrue** for verify.
- **#4:** `status` reports fixity **coverage, not integrity**, and never hashes; a wholly bit-rotted archive still prints "Fixity: 19 of 19 records carry digests" with no hint the bytes are bad.

None of these weakens a security control, so all are Type II (reducible by redesign) rather than Type I. They must be addressed — at minimum #29 and #7 — before this ships. The remaining Type II items (#6, #14, #18) and the 27 Type III items form the operability backlog.

---

## Cell table

| Cell (actor × scenario) | Functions? | Friction findings | Type | Fix / backlog item |
|---|---|---|---|---|
| **archivist** — export PST+mbox into one `-out`, run verify | yes | `records=19 … checked=21` unexplained (checked counts files, records counts messages); "with-fixity"/"attested" slightly crypto-flavoured | III | gloss the count line in `VerifySummary` |
| archivist — flip byte / delete zip / stray html; verify human + `-json` | yes | exemplary; only nit: JSON has no explicit `attested` boolean, consumer must re-derive verdict from exit code | III | add `"attested": bool` (+ version) to `-json` |
| archivist — strip all fixity, verify → `-record` → verify | yes | copy-pasteable remedy, honest caveat; "recorded fixity for N file(s)" prints **twice** under `-record` (logger + summary) | III | emit the line once |
| archivist — `status` at each stage, judge Fixity line | **partial** | Fixity line reports **coverage, not integrity**; status never hashes; rotted archive still reads healthy; `status -json` omits fixity entirely | **II** | wording "Fixity coverage: N of M recorded (run `verify` to check bytes)"; add `with_fixity` to status JSON; persisted last-verify facet |
| archivist — transport-headers panel on a message page | yes | collapsed, escaped, CSP-inert, transport tokens unindexed — well done; "(unverified)" never says *why* on the self-contained page | III | one-line note inside the panel |
| archivist — `schedule … -- verify` preview: lock & backup window | **partial** | preview **silent** about verify's whole-run lock & backup-window conflict; default verify time (02:00) == default backup time; verify & backup schedules for one archive derive the **same name** → collision | **II** | warn/refuse on `-at` collision; derive distinct verify name; fix README example |
| archivist — is a scheduled verify's pass/fail visible on `status`? | **no** | verify writes no lastrun record; NOT-ATTESTED leaves Last run "ok", Posture WARN, Fixity "N of N" — verdict lives only in the job log; preview's "surfaces via status" note is false | **II** | verify finalizes a last-check record; `status` "Last verified: … → attested/NOT-ATTESTED" drives Posture RED |
| archivist — symlink→/etc/passwd + `../../escape.html` manifest path; verify | yes | fail-safe: symlink "not followed", escaping path refused; one tampered record reads as 2 modified + 2 unexpected (multiplicity briefly confusing) — inherent to the control | I | none required; optional legibility note |
| archivist — README fixity + "Upgrading" truthfulness | **partial** | fixity section truthful & honest about limits; one untruth: verify-schedule preview promises `status` visibility verify can't deliver; README example omits `-name` | III | see doc corrections D1, D2 |
| **operator upgrading** — first incremental after upgrade; migration logs | yes | frictionless happy path; index-migration line logged **before** its cause (manifest re-scope); "migrating index keys" line lacks the "one-time upgrade" reassurance | III | reorder log lines (run.go); append "one-time upgrade" to RepairKeys message |
| operator upgrading — second run is a true no-op? | yes | genuinely one-time; no migration lines, exports 0, pages untouched | none | — |
| operator upgrading — `reindex` as first post-upgrade command | yes | correct; kept/pruned sane; but reindex logs cause→effect order, **opposite** of the export path | III | align log order across run.go / reindex.go |
| operator upgrading — duplicates after upgrade? (`status`, `search`) | yes | no duplicates (17/17, distinct paths); surprise: `status` shows "Fixity: 0 of 17" post-upgrade with no doc warning | III | README sentence on post-upgrade fixity (D4) |
| operator upgrading — mixed 1-/2-NUL keys, self-heal, verify | yes | self-heal collapses to 17; but de-dup keeps the **re-scoped duplicate** path → verify flags the **original canonical** file as `unexpected`; `unexpected:` lines carry no remedy; an excursion **wipes fixity archive-wide** | **II** | explain+remedy `unexpected` in VerifySummary; prefer pre-existing record's path on self-heal; document archive-wide fixity clear |
| operator (downgrade) — manifest `version:9`, refusal legible? unmutated? | **partial** | export/verify refuse correctly (names file, versions, two remedies, exit 1, byte-unchanged); but `status` swallows the load error → "no archive … run an export first" (the **wrong** advice); "Done. exported=0" prints **before** "FAILED:" | III | health.Gather record LoadErr → RED naming version/upgrade; skip/relabel printSummary on error |
| new operator — read "Upgrading" cold | **partial** | sets expectation well; gaps: sample log order matches reindex not export; silent on post-upgrade 0 fixity; doesn't warn `status` misleads after a downgrade; "verify reports the duplicate files" overstates | III | doc corrections D3–D6 |
| **power user** — preview stores with `-list` / `-auto -list` | yes | `-list` writes nothing; but Evolution IMAP caches shown by opaque hash dir, not account name | III | resolve & show the Evolution account label in `-list` |
| power user — inline tokens vs flags agree | **partial** | agree for single-word values; a **space-bearing** `folder:`/`from:` token is re-split by `strings.Fields` → **silent 0 matches**, while the flag returns 11 | **II** | support quoted token value or don't silently drop to 0; document the limit |
| power user — tokens/flags vs running `serve` `/api/search` | yes | full parity; harmless shape diffs (bare array vs `{total,results…}`; default limit 20 vs 50) | none | optional README note |
| power user — `-json` \| jq; `-paths -0` \| `xargs -0`; `-json -paths` refusal | yes | excellent hygiene (stdout data-only, NUL-safe, refusal exit 1); `-paths` is archive-relative → consumer must `cd` into `-out` | III | optional `-paths-abs` |
| power user — `status -json` as a CI gate | **partial** | well-formed & versioned; key set/enums **undocumented**; **no structured reason codes** (gates must string-match prose); `status` never sets non-zero exit | III | document schema; add machine-readable reason `code` |
| power user — OneDrive cloud-sync WARN | yes | fires correctly, wording actionable; detection is **OneDrive-only** (Dropbox/Drive/iCloud/Box get nothing); no-schedule remedy names the *original* path, not the copy | III | recognise sync roots generically; build remedy from actual `-out` |
| power user — moved-archive WARN | yes | legible (names stale target, this archive, reinstall + by-name removal); remedy carries literal `...` placeholder; "installed" date in local time, unlabelled, vs UTC range | III | substitute lastrun job into remedy; label/UTC the date |
| power user — is the scheduled-run remedy pasteable from another cwd? | yes | fully pasteable (job absolutises `-out` and `-input`) — but this absolutisation is what triggers #29 | none | fixed by #29 |
| power user — failed run → BACKUP-NEEDS-ATTENTION.txt → fix → re-run | yes | exemplary fail-closed loop; typed exit 1, plain-language sidecar, RED status, auto-removed on success | none | none |
| power user — second export while lock held (verb=verify) | yes | correct refusal on all 3 verbs (names holder verb/pid/host/path, exit 1, stdout empty); cosmetic: "Done. exported=0" prints before FAILED | III | suppress "Done." when run aborted pre-lock |
| power user — `verify` as a fixity gate (clean/tampered/`-json`) | yes | proper machine gate (0/2/1, structured `problems[]`); verify JSON has no `version` field (status's does); schemas undocumented | III | add `version` to verify JSON; document both schemas |
| power user — GUI headless `-job FILE` (happy + unattended guard) | yes | robust; guard refuses to invent an archive; `.jobfail.log` + desktop notification | none | none (duplication it exposed is #29) |
| **power user (HEADLINE)** — same store re-archived under a different `-input` **spelling** doubles the archive | **no** | store identity keyed on raw `-input` string; abs vs relative spelling = two stores; **silent** 17→34, two dirs; breaks **R2/R3**; triggered by the tool's own remedy & GUI job files; not in acknowledged limits | **II** | normalise source identity (`filepath.Abs`+`Clean`+symlink resolve) before `Token()`; or key on stable content/store metadata; safety-net dup warning in status/verify |
| **windows user** — cross-compile CLI + GUI, confirm subsystem | yes | both cross-compile cleanly; `file` confirms console vs GUI subsystem; matches docs | none | — |
| windows user — `SchtasksXML` + Preview (unicode/`&`/space, catch-up) | yes | mechanically flawless (future StartBoundary, `&`→`&amp;`, `ö` round-trips, UTF-16LE+BOM); but the cron-preview legibility fix was **not** applied to the schtasks branch — raw XML, no cadence gloss, no resilience gloss, bare local time | III | give schtasksPreview cadenceGloss + resilience gloss + failure note; label StartBoundary tz |
| windows user — README `.ost` row + per-OS notes cold | **partial** | every fact present & consistent; facts **scattered** across four places; the sleep-vs-off distinction lives far from the `.ost` row a row-only reader stops at | III | add sleep-vs-off clause to the `.ost` row (D8) |
| windows user — GUI wizard (Outlook path + auto path) | yes | coherent; keep-raw & "mail app open?" correctly suppressed for COM; nothing-found loops back legibly; nits: keep-raw asked on auto even when only store is `.ost`; Repair/Remove exits the program | III | offer "continue to wizard" after Repair; skip keep-raw when all inputs are Outlook files |
| windows user — headless GUI job with `-out` missing | yes | exemplary fail-closed guard (names dir, cause, safe refusal; `.jobfail.log`; degrades w/o DISPLAY); gap: no `-out` folder means no lastrun → `status`/health can't show this failure | III | write a breadcrumb (last-failure.json in config dir) the health surface can find |
| windows user — `-outlook` advisory + reclaimed-PST log line | **partial** | advisory & CompletenessNote good, printed up-front; **defect:** GUI logs "Reclaimed 2251799813685 bytes" (raw int64) while CLI logs "Reclaimed 2.1 GB" — GUI already imports HumanBytes | III | GUI use `thunderbird.HumanBytes`, match CLI wording |
| **inheritor** — cold-start read of in-archive README.txt | **partial** | lock line now correct; covers manifest/attachments/inertness/UTC/search; **silent on verify/fixity**, transport-headers panel, and lastrun/schedule/BACKUP-NEEDS sidecars | III | extend README.txt template (D10) |
| inheritor — open a message with a real Received chain | yes | position & framing right; collapsed, "as stored (unverified)", escaped; minor redundancy on plain internal mail (hidden behind disclosure) | none | optional |
| inheritor — folder-page in-browser filter (paginated vs not) | yes | honest & correctly state-dependent; paginated case states scope + two escape hatches | none | none |
| inheritor — numbered pager on a 12-page folder (file://) | yes | every link resolves; current unlinked; first/last always reachable; window ±2 so end→middle takes 2–3 clicks | III | optional wider window / jump-to-page |
| inheritor — from manifest alone, would you know to run verify? | **partial** | manifest self-describes fixity; verify verdict is the exit code; **discovery gap** — README.txt, index.html note, and even `main.go:115` bare-invocation hint all omit `verify` (only full `-h` lists it) | III | add `verify` to subcommand list (main.go:115); Verifying line in README.txt; index.html pointer |
| inheritor — is BACKUP-NEEDS-ATTENTION.txt self-explaining to a toolless reader? | **partial** | headline + 3 facts clear; but written for the operator: only remedy is "run mailarchive status" (a toolless inheritor can't); graph `Reason` is a raw AADSTS/Trace-ID dump; no reassurance that on-disk messages are still readable | III | add reassurance line to the attention template |
| inheritor — count steps "folder in hand" → "I trust these files" | **partial** | content trust ~4 steps, dead-end-free; integrity trust is one clean command *once you know it exists* — but nothing in the folder mentions verify; `status` on a flawless archive still says WARN (known deliberate v2 Type II) | III | the verify-discovery fixes above convert integrity-trust from out-of-band knowledge into a step the folder teaches |

---

## Findings to fix

*Type II first (verdict-driving), then Type III by descending value. Each names the file, the behaviour, and the wording where wording is the fix.*

### Type II — design-choice; these must be addressed before ship

**1. Source identity must not depend on the `-input` path spelling (headline).**
`internal/app/run.go:439` keys a store on the raw caller-supplied path string (`Token(path, store)`; `internal/state/manifest.go` `Stores` map). `testdata/support.pst` vs its absolute path mint two tokens (`support` and `support~<hash>`) and re-export every message — 17→34 entries, 34 html files, two store dirs — breaking R2/R3 silently. **Fix:** normalise before `Token()` — `filepath.Abs` + `filepath.Clean`, and resolve symlinks — so relative and absolute spellings of one file collapse to one store; better, key identity on stable content/store metadata rather than the path string. **Safety net:** have `status`/`verify` flag when two store tokens share a display name and identical content and offer a reindex/de-dup remedy. This is urgent because the product's own surfaces trigger it: the `status` no-schedule remedy absolutises `-input` (see #13) and the GUI writes absolute-path job files.

**2. A scheduled `verify` must be visible on the standing status surface.**
`verify` writes no `.mailarchive-lastrun.json`, so `status` "Last run" always reflects the last *backup*, never a verify, and a NOT-ATTESTED (exit 2) verify leaves Posture WARN-for-no-schedule and Last run "ok". **Fix:** have `verify` finalize a last-check record (verified-at, verdict, counts) and give `status` a `Last verified: <date> → attested/NOT-ATTESTED` line that drives **Posture RED** on a not-attested result. This also makes the schedule-preview note (D1) true instead of a lie.

**3. Distinguish fixity coverage from integrity in `status`, and expose it in JSON.**
`internal/health/health.go` prints "Fixity: N of M records carry digests" — pure coverage; `status` never hashes, so a rotted archive reads healthy, and `status -json` (version 1) omits the field entirely. **Fix:** reword to "Fixity coverage: N of M recorded (run `verify` to check the bytes are intact)"; add `with_fixity` to the status JSON document; and land the persisted last-verify facet from finding 2 so status has a real integrity signal.

**4. The `schedule` preview for a verify job must warn about the lock/backup-window conflict and the name collision.**
`internal/schedule/schedule.go` — the preview is silent about verify's whole-run exclusive lock and the backup-window conflict the README/help stress, the default verify time (02:00) equals the default backup time, and a verify schedule and the backup schedule for one archive derive the **same** name (`mailarchive-<hash of -out>`), so installing the README's own example replaces/collides with the nightly backup. **Fix:** (a) print the lock/backup-window warning and refuse (or warn) when `-at` collides with the archive's recorded backup time; (b) default a verify schedule to a distinct derived name (e.g. `mailarchive-<hash>-verify`) so it coexists with the backup; (c) fix the README example (D2).

**5. Self-heal must keep the canonical filename, and `unexpected` must carry a remedy.**
`internal/app/verify.go` / the excursion self-heal keeps the **re-scoped (later) record**, which points at the excursion's *duplicate* copy, so `verify` flags the **original, canonically-named** file as `unexpected` — an operator told "verify reports the duplicate files" and following the output would delete the *wrong* file. Separately, `unexpected:` lines carry no detail or remedy, and a single pre-fixity excursion silently wipes the fixity baseline for the whole archive. **Fix:** in `VerifySummary`, give `unexpected` a one-line explanation + remedy, e.g. `unexpected (not owned by any record — a leftover from an old-binary excursion or a hand-added file; safe to delete if the former)`; have the self-heal prefer the pre-existing record's path over the excursion duplicate; document the archive-wide fixity clear (D4/D6).

**6. A space-bearing `folder:`/`from:` inline token must not silently return zero.**
`index.ParseQuery` runs the query through `strings.Fields`, so `folder:Sent Messages` re-splits into `folder:Sent` + free-text `Messages` and yields 0 matches, while `-folder "Sent Messages"` returns 11 — silent wrong results, not an error. **Fix:** support a quoted token value (`folder:"Sent Messages"`) or capture everything-after-colon; at minimum, when a `folder:`/`from:` token is followed by bare terms that match no field, do not silently drop to 0 — and document the limit (D7).

### Type III — buildable-away; the operability backlog

**7. `status` must not report a present-but-newer/corrupt manifest as "no archive," and "Done." must not precede "FAILED."**
`internal/health/health.go` `Gather` swallows the manifest load error (`if m, err := state.Load(...); err == nil`), so a complete `version:9` (or corrupt) archive is reported as "no archive … run an export first" — the exact wrong advice, contradicting the README's "do not run an older mailarchive." **Fix:** when `state.Load` returns a non-nil error on a manifest that exists on disk, record it (e.g. `HasManifest` + `LoadErr`) and surface it as RED naming the version/upgrade remedy. Separately, in `cmd/mailarchive/main.go` `runExport` (~line 223), skip or relabel `printSummary` when `app.Run` returned an error so "Done. exported=0 … manifest=0" never precedes "FAILED:".

**8. Make the archive point at its own verifiability.**
`verify` is undiscoverable from inside a handed-over archive. **Fix, three one-liners:** add `verify` to the subcommand hint at `cmd/mailarchive/main.go:115` (currently `serve, search, reindex, schedule, graph, status`); add a "Verifying the files" line to the `internal/app/readme.go` README.txt template (D10); add a pointer to the `index.html` front-door note ("run `mailarchive verify -out .` to confirm the files are intact"). Highest-value inheritor fix — converts integrity-trust from out-of-band knowledge into a step the folder teaches.

**9. GUI reclaimed-PST log line must be human-readable.**
`cmd/mailarchive-gui/main.go:231` prints `Reclaimed %d bytes` (raw int64, e.g. "2251799813685 bytes") while the CLI (`cmd/mailarchive/main.go:230`) prints "Reclaimed 2.1 GB" via `thunderbird.HumanBytes`. The GUI writes the log a non-technical Windows user reads, and it already imports `HumanBytes`. **Fix:** `logger.Printf("Reclaimed %s by removing the temporary Outlook PST copy (%s).", thunderbird.HumanBytes(n), dir)`.

**10. Add an explicit verdict field to the machine outputs.**
`verify -json` has no explicit `attested` boolean (a script must re-derive the verdict from the exit code or from `modified+missing+unrecorded==0`) and no `version` field, while `status -json` has `version:1`. **Fix:** add `"attested": bool` and a `"version"` field to the verify JSON document.

**11. Give the Windows `schtasks` preview the legibility the cron preview already has.**
`schtasksPreview` prints raw Task Scheduler XML with no cadence gloss, no plain-language note that `StartWhenAvailable=true` catches up a run missed while the laptop slept, that batteries-allowed keeps it running unplugged, and that `WakeToRun=false` skips an off machine — exactly the semantics the "sleeps-the-laptop" persona needs; `StartBoundary` is bare local wall-clock, unlabelled. **Fix:** prepend the existing `cadenceGloss` ("daily at 03:30" / "weekly on Sunday at 03:30"), add a one-line resilience gloss ("a run missed while the PC slept is caught up on wake; runs on battery; a night the PC is off is skipped") and the failure-visibility note, and label `StartBoundary` as local time. (Real `schtasks.exe` acceptance is lab-pending, MA-79.)

**12. Document the status and verify JSON schemas and add reason codes.**
Neither JSON schema is documented anywhere user-facing (README shows only the text form; `-h` says "typed, versioned JSON"), and `status` reasons are free-text prose only (cloud-sync, moved-archive, staleness), so a gate must string-match human wording that can change. **Fix:** document both schemas (keys + `posture` GREEN/WARN/RED and `last_run.status` ok/failed/cancelled/running enums) in the README (D9); add a machine-readable reason `code` alongside each free-text reason.

**13. Broaden cloud-sync detection and build the remedy from the archive's real path.**
`util.UnderCloudSync` matches only `onedrive*` segments / OneDrive env vars and hard-codes the service name; Dropbox / Google Drive ("My Drive") / iCloud Drive / Box get no warning despite identical risk. And the no-schedule remedy replays the recorded lastrun `-out` verbatim, so after relocating an archive the "keep it current" command targets the old path. **Fix:** recognise the common sync roots generically with the matched service name; build the no-schedule remedy from the archive's actual `-out` (`in.Out`) when it differs from the stored job's.

**14. Complete the moved-archive remedy and label its timestamp.**
The moved-archive WARN's re-install remedy carries a literal `...` placeholder (not pasteable) though the recorded lastrun job could complete it, and "installed 2026-08-31" is shown in local time with no tz label while "Archived range" is UTC. **Fix:** substitute the recorded lastrun job into the remedy; label the installed date's timezone or render it UTC.

**15. Gloss the verify count line.**
`records=19 … checked=21` reads as a discrepancy because `checked` counts files (html + zips) while `records` counts messages. **Fix:** in `VerifySummary`, gloss it, e.g. `records=19 messages, files checked=21 (html + attachment zips)`, or add a one-word legend.

**16. Print "recorded fixity for N file(s)" once.**
Under `verify -record` the line prints twice — once from the run logger to stderr, once from `VerifySummary` to stdout. **Fix:** drop the `logger.Printf` in `Verify()` when not writing to a `-log` file, or suppress the duplicate in `VerifySummary`.

**17. Align the migration log order across the two verbs.**
The export path (`internal/app/run.go`) logs "migrating index keys" (~line 170) *before* "re-scoped … manifest entries" (~line 180) — effect before cause — while `reindex` (`internal/app/reindex.go`) logs cause then effect, matching the README. **Fix:** log the manifest re-scope before calling `idx.RepairKeys` in run.go so both verbs match; append a "one-time upgrade" clause to the "migrating index keys" message in `internal/index/index.go` `RepairKeys`.

**18. Explain *why* the transport headers are unverified, on the page itself.**
The panel is labelled "Transport headers as stored (unverified)" but the reason (Received/Authentication-Results are sender-supplied and forgeable) lives only in the README. **Fix:** add a one-line note inside the panel, e.g. "These lines are supplied by the sending/relaying servers and can be forged — shown as stored, not proof of origin."

**19. Reassure the toolless inheritor in BACKUP-NEEDS-ATTENTION.txt.**
`internal/state/lastrun.go` `updateAttentionSidecar` — the sidecar's only remedy is "run mailarchive status," which an inheritor without the tool cannot do, and a caps-locked "FAILED" can read as "the whole archive is corrupt." **Fix:** add "The messages already in this folder are unaffected — open index.html to read them," note the notice concerns the most recent backup run only, and (for a toolless reader) where to get the tool to resume backups.

**20. Leave a breadcrumb when a headless job's `-out` is absent.**
When `-out` is missing there is no folder to write `.mailarchive-lastrun.json` into, so `status` and the GUI launch-health view can't show the failure — the only signal is a transient toast and the buried `.jobfail.log`. **Fix:** record the last headless failure (name, time, reason) in the config dir (e.g. `%APPDATA%\mailarchive\last-failure.json`) and have the GUI health view / `status -json` surface "the last scheduled run could not start: <reason>."

**21. Resolve the Evolution account label in `-list`.**
`-auto -list` prints Evolution IMAP caches by their opaque on-disk hash dir (`.cache/evolution/mail/3c07b233…`), not the account name the exporter uses for the store dir. **Fix:** resolve and show the account label beside the hash path in `-list`.

**22. Suppress the "Done." summary when a run aborts before acquiring the lock.**
A run that never acquired the lock still prints "Done. exported=0 …" to stderr just before "FAILED: archive is in use." **Fix:** suppress the summary line when the run aborted pre-lock, or move it after the success check.

**23. Add a sleep-vs-off clause to the README `.ost` row.** (see doc correction D8)

**24. After a successful GUI Repair, offer to continue into the wizard** instead of exiting (`cmd/mailarchive-gui/main.go`), so a returning user who repairs a schedule isn't forced to re-launch to run an export. *(Optional; does not strand the user.)* Also optional: skip the keep-raw question on the auto path when every discovered input is an Outlook file.

**25. Widen the pager window or add a jump-to-page input** for very large folders (`folderTemplate` / `Pager()`); the current ±2 + first/last window takes 2–3 clicks to reach a deep middle page. *(Optional; conventional as built.)*

**26. Consider `-paths-abs`** (or joining `-paths` output with `-out`) so `search … -paths -0 | xargs -0 <fileop>` works from any cwd without a manual `cd`. *(Optional.)*

---

## Documentation corrections

*Exact passages that are wrong or unclear, with corrected wording.*

**D1 — The schedule preview promises status visibility `verify` cannot deliver.**
`internal/schedule/schedule.go:326` prints, for every job including a `verify` job:
> `A failed run surfaces only via `mailarchive status` or cron's MAILTO.`

This is **false for a verify job** — `verify` writes no run record, so `status` cannot surface its result (see finding 2). Until finding 2 lands, branch the wording by verb, e.g. for verify:
> `A failed verify surfaces only in the job's own log (\`<name>.log\`) or via cron's MAILTO — \`mailarchive status\` does not yet show scheduled-verify results.`

Once finding 2 lands (verify records a last-check that drives Posture), restore the original wording — it becomes true.

**D2 — The README verify-schedule example omits `-name`, colliding with the backup.**
`README.md:577`:
> `mailarchive schedule -interval weekly -at 05:00 -install -- verify -out ./archive`

A verify schedule and the backup schedule for the same `-out` derive the same name (`mailarchive-<hash of -out>`), so installing this against an already-scheduled archive replaces/collides with the nightly backup. Corrected:
> `mailarchive schedule -interval weekly -at 05:00 -name mailarchive-verify -install -- verify -out ./archive`

and add a sentence: "Give a verify schedule an explicit `-name` that differs from the backup's, or the two collide (both derive their name from `-out`)." (Preferable: land finding 4 so the tool derives a distinct verify name automatically.)

**D3 — The "Upgrading" sample log block shows the wrong order for the export path.**
`README.md:429–430`:
> ```
> re-scoped 4213 manifest entries by store (one-time upgrade; cost scales with archive size)
> migrating index keys (4213 rows)
> ```

This matches `reindex` but **not** the export path, which prints "migrating index keys" first, then "re-scoped …" (finding 17 — a language-vs-function drift, the class the last commit was fixing). Either fix the code order (finding 17, preferred, so one order holds everywhere) or, if the code order stays, note that the export verb prints these two lines in the reverse order.

**D4 — "Upgrading" says nothing about fixity being empty post-upgrade.**
Add to the `### Upgrading an existing archive` section (after `README.md:441`):
> "The re-scope re-exports nothing, so it does not backfill fixity: legacy bytes cannot be attested as pristine. A freshly-upgraded archive shows `Fixity: 0 of N records carry digests` and `verify` reports NOT-ATTESTED until the next full re-export or an explicit `mailarchive verify -record`. This is expected, not data loss."

**D5 — "Upgrading" warns against an old binary but not that `status` misleads if you used one.**
The bold warning at `README.md:432` ("**do not run an older `mailarchive` against it**") is correct, but doesn't tell an operator who already did that the tool's own `status` will report "no archive … run an export first" (finding 7). Add:
> "If an older binary (or a newer-format archive on an older build) has already touched a shared `-out`, `mailarchive status` may wrongly say 'no archive here'. Trust the export/verify message instead — 'written by a newer mailarchive (format version N)' names the real cause — and upgrade the binary."

**D6 — "verify reports the duplicate files … as unexpected" overstates the CLI output.**
`README.md` verify blockquote:
> `\`verify\` reports the duplicate files an old binary leaves behind as \`unexpected\`.`

In practice `verify`'s `unexpected` lines carry no explanation or remedy, and (per finding 5) they flag the **original, canonically-named** files, not the duplicates. Corrected until finding 5 lands:
> "After such an excursion, `verify` lists the leftover, unowned files under `unexpected` (exit 2). Note that self-heal may retain the excursion's duplicate copy as the record of truth, so the file flagged `unexpected` can be the original, identically-named file — compare bytes before deleting anything."

**D7 — "Search & discovery" claims token equivalence without the space caveat.**
`README.md:316`:
> `identical to \`-sender bob invoice\`; a token overrides the matching flag.`

True only for single-word values. Add:
> "A token's value cannot contain a space: `folder:Sent Messages` is re-split into `folder:Sent` plus the free term `Messages` and matches nothing. For a value with spaces, use the flag form — `-folder \"Sent Messages\"`."

**D8 — The `.ost` Start-here row omits the sleep-vs-off fact a laptop user needs.**
`README.md:48`, the row's final "What to know" cell currently ends at the programmatic-access sentence. Append:
> "Scheduled runs happen while you are logged in; a night the laptop merely sleeps is caught up when it wakes, but a night it is fully powered off is skipped (`status` shows it)."

This pulls the single most decision-relevant fact for a laptop user into the row so it need not be reconstructed from the per-OS notes further down.

**D9 — The status and verify JSON schemas are undocumented.**
The README documents only the text form of `status` and only the exit-code contract of `verify`. Add a short "Machine-readable output" subsection listing the `status -json` keys with the `posture` (GREEN/WARN/RED) and `last_run.status` (ok/failed/cancelled/running) enums, and the `verify -json` keys (`records`, `with_fixity`, `checked`, `ok/modified/missing/unrecorded/unexpected`, `problems[].{path,kind,detail}`), noting `search -json` is a bare array while `/api/search` wraps it as `{total,results,limit,offset}` and the CLI `-limit` default is 20 vs the API's 50.

**D10 — The in-archive README.txt template is silent on the post-v2 additions.**
`internal/app/readme.go` — the generated `README.txt` has no "Verifying" section, never mentions the collapsed transport-headers panel, and its "Housekeeping" list omits `.mailarchive-lastrun.json`, `.mailarchive-schedule.json` and `BACKUP-NEEDS-ATTENTION.txt`. Add:
> "Verifying the files — every archived file carries a sha256 in `.mailarchive-manifest.json`; run `mailarchive verify -out .` to re-check them (exit 0 = every file intact)."
> "Each message page has a collapsed 'Transport headers as stored (unverified)' panel — the raw delivery headers, shown as received and not proof of origin."

and, under Housekeeping:
> `.mailarchive-lastrun.json    record of the last backup run (status, counts)`
> `.mailarchive-schedule.json   the recurring-backup descriptor, if one was set`
> `BACKUP-NEEDS-ATTENTION.txt   present only when the last run failed; explains what to do; auto-removed after the next successful run`

---

## Disposition (2026-09-03)

Closing-fixes merges on `main`: identity/exporter eb37754, verify/status
28fcc77, search/schedule/pages 58bec68, GUI b788788.

| Finding | Disposition |
|---|---|
| 1 (Type II, source identity by path spelling) | Built (X1): the store token is seeded from a canonical source id (`path:` + resolved absolute path, case-folded on case-insensitive platforms; `mailbox:` + lower-cased address); existing registries migrate on load. |
| 2 (Type II, scheduled verify invisible) | Built (X2): verify writes `.mailarchive-lastverify.json`; `status` shows "Last verify" and goes RED on modified/missing, WARN on unrecorded-only. |
| 3 (Type II, coverage vs integrity) | Built (X2): the line reads "Fixity coverage: …"; `status -json` v2 carries `fixity` and `last_verify`. |
| 4 (Type II, verify schedule name/time collision) | Built (X3): a verify job derives `<name>-verify`; the preview warns about the lock and refuses a verify scheduled at the archive's backup time. |
| 5 (Type II, self-heal keeps the duplicate; `unexpected` has no remedy) | Built (X1 keeps the surviving record's path on re-key; X2 explains `unexpected` with a remedy). |
| 6 (Type II, space-bearing token) | Built (X3): quoted token values (`folder:"Sent Messages"`), documented. |
| 7 (status swallows a newer/corrupt manifest; "Done." before "FAILED") | Built (X2 RED naming the file and the upgrade remedy; X1 no "Done." on a failed run). |
| 8 (archive points at its own verifiability) | Built (X1 subcommand hint; X2 README.txt; X3 index.html note). |
| 9 (GUI raw byte count) | Built (X4). |
| 10 (verify -json verdict/version) | Built (X2). |
| 11 (schtasks preview gloss) | Built (X3). |
| 12 (JSON schemas + reason codes) | Built (X2): README "Machine-readable output"; `reason_codes` in `status -json`. |
| 13 (cloud-sync breadth; remedy from the real path) | Built (X2). |
| 14 (moved-archive remedy; timestamp label) | Built (X2). |
| 15, 16 (verify count gloss; duplicate line) | Built (X2). |
| 17 (migration log order) | Built (X1). |
| 18 (why headers are unverified) | Built (X3). |
| 19 (sidecar reassures a toolless reader) | Built (X2). |
| 20 (headless failure breadcrumb) | Built (X4). |
| 21 (Evolution account label in -list) | See the X1 result: built if the label is derivable without opening the account file, else recorded as not done. |
| 22 ("Done." on a pre-lock refusal) | Built (X1). |
| 23 / D8 (.ost row sleep-vs-off) | Built (X3). |
| 24 (continue after Repair; skip keep-raw for all-Outlook auto) | Built (X4). |
| 25, 26 (wider pager; -paths-abs) | Left: conventional as built; `-paths` output joins with `-out` by design. |
| D1–D10 | D1, D2, D8 → X3; D3 (code order) → X1; D4, D5 → X1; D6 → X1/X2; D7 → X3; D9, D10 → X2. |
