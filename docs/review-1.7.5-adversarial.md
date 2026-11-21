*Filed by the lead, 2026-11-20 (clock 21:13 EST). Below this header: the skeptic's report, then the
three finders' reports, each word for word. All four are independent Claude agents that wrote no part
of 1.7.5. They reviewed commit 43d4e4c on branch `combined-archive-gone-mailbox` (diff from 304871c),
following `assurance-kit/process/adversarial-review.md`. The finders were read-only and ran nothing;
the skeptic ran red tests, one at a time, on this Mac.*

# Adversarial pass: MailArchive 1.7.5 (all three parts)

**Result: 18 found / 9 confirmed / 4 refuted / 3 duplicates, plus 2 with the mechanism shown but
not run (one needs Windows, one Linux). 0 HIGH, 2 MEDIUM.** This pass also carries the pre-code
review's owed rescue step: the search for a deeper weakness behind the HIGH L2-02 (OUT-04, refuted;
a structural guard is added anyway).

## The lead's fold

| ID | Severity | Decision | By |
|---|---|---|---|
| OUT-01 (with INS-01) | MEDIUM | Fix: no file check on a non-local record path; the Desktop asks inside `withinSourceWait`; `resolveGoneSince` skips non-local paths. Tombstone in the style of MA-869 | builder 3 |
| INS-02 | MEDIUM | Owner question Q7 (refuse, as 1.7.4 did, or skip with true words). Lead recommends refuse | owner, then builder 3 |
| INS-03 | LOW | Fix: `Lstat` on the cleaned path too | builder 3 |
| INS-04 | LOW | Windows lab (design §4 step 4 already owes the junction case) | lab |
| INS-05 | LOW | Not fixed in 1.7.5: a disk mounted at a profile's `ImapMail` that is missing at boot. Written in design §5 as a known limit | design text |
| INS-06 | LOW | Fix: clean Name and Path on read; cap the gone-mailbox lines | builder 3 |
| CON-01 | LOW | Fix: the named day is the first local day on which every backup is a note | builder 3 |
| CON-02 | LOW | Owner question Q8 (keep a later first-found date, which changes one MA-1472 test case). Lead recommends yes | owner, then builder 3 |
| CON-03 | LOW | Fix: no broken sentence for a warning with no path | builder 3 |
| OUT-02 | LOW | Fix: true words when the row was removed from the list during the check | builder 3 |
| OUT-03 | INFO | Fix: a sent delete's token leaves `delTokens` | builder 3 |
| OUT-04 | refuted | Structural guard added anyway: every automatic delete post carries `armed:true` and the row | builder 3 |
| OUT-05 (a), CON-05 | refuted | No change (design §3.1 accepts it) | — |
| OUT-05 (b), CON-06 | refuted as a defect | Owner question Q10 (should Lock end a waiting delete plan?). Lead recommends yes | owner, then builder 3 |
| INS-07 | refuted | No change | — |
| CON-04 | refuted as a defect | Design text: R9's note says 1.7.5's own refusals reach rule 2 too | builder 3 |

A note on the skeptic's slip, which it reported itself: one search read the session's own saved copy
of a `git show` output (1.7.4's `tbcompleteness.go`), kept by the tool harness outside the review
folder. Nothing else there was opened.

---

## The skeptic's report

**18 found / 9 confirmed / 4 refuted / 3 duplicates** (2 more are mechanism shown, not run here: one needs Windows, one Linux)

**Verdict:** two MEDIUM findings should be fixed before 1.7.5 ships. Nothing is HIGH.
- **OUT-01:** drawing the page, and `status`, look at mail paths taken from the archive's untrusted record.
- **INS-02:** a Thunderbird account that was moved, not removed, is skipped quietly.

The delete part holds. Its three confirmed findings are wrong words and a race of a few milliseconds that fails safe.

---

**Header**
- **Date, from the clock:** Fri 20 Nov 2026, 20:58 EST.
- **FROZEN_SHA:** 43d4e4c (43d4e4c10fa4b68dba5f9297a28973b5b3aabf15), in worktree `r175-adv`. It was clean before I began. Diff base: 304871c.
- **Go:** go1.26.0 darwin/arm64.
- **Skeptic:** a fresh session. I am not a finder and did not write the change.
- **Commands and exit status:**
  - `date` 0; `go version` 0; `go env GOROOT` 0. I ran the last one only to see where the toolchain lives. I did not open it.
  - `git -C WORKTREE`: `status --short` twice, 0 (clean at the start; at the end only my 4 files); `log -1` 0; `diff --stat 304871c 43d4e4c` 0; `diff … -- lastrun.go health.go` 0; `show 304871c:internal/app/tbcompleteness.go` 0; `show 304871c:internal/backupalert/tbbehind.go` 0; `grep RecordRunWarning 304871c` 0.
  - `memory_pressure -Q`, 5 times: 0, with 67% free each time.
  - The tests, each `go -C WORKTREE test -p 2 -count=1 -run ZZSkeptic175 ./internal/<package>/`, one at a time:
    - health: exit 1 (3 red, as intended)
    - state: exit 1 (1 red)
    - app: exit 1 (3 red)
    - desktop: exit 1 (3 red)
  - All 10 skeptic tests are red. Every positive twin passed.
  - About 30 read-only grep searches in the worktree: exit 0. Five failed harmlessly: three zsh "no matches" errors from an `--include` pattern, and two from a wrong file name.
  - **One slip, said plainly:** one grep read the harness's saved copy of my own `git show` output. The harness had stored it under `/Users/alex.dimarco/.claude/projects/…/tool-results/`, which is outside the allowed folder. It held only 1.7.4's `tbcompleteness.go`. I listed and opened nothing else there.
- **Read:**
  - the brief and `assurance-kit/process/adversarial-review.md`
  - the three finder reports
  - the design, lines 1–1000 (§0 to §4)
  - all of `autoplace.go`, `health/mailboxgone.go`, `app/sourceloss.go` and `desktop/pending.go`
  - parts of `health.go`, `archives.go`, `settingscard.go`, `backuproute.go`, `maintenance.go`, `archivedelete.go`, `deletescript.go`, `lastrun.go`, `securefile.go`, `run.go`, `thunderbird.go` (matchWith), `archdelete/execute.go`, `archdelete/archdelete.go` and `cmd/mailarchive/job.go`
  - 1.7.4's `RecordRunWarning` and `tbbehind.go`
  - the tests for MA-869, 964, 1471, 1472, 1476, 1477 and 1479, and the delete test rig
  - the catalog rows MA-869, 972, 1471, 1472, 1475, 1476 and 1479
- **Not read:**
  - friction.txt, the other briefs and lenses.md (as told)
  - the design after line 1000
  - Go's own source, which lies outside the folders I may read. So Go's handling of Windows junctions stays recalled.
- **Mutations:** none run. No dialog, browser or notification guard was touched. Nothing is owed on CI from this pass.

---

| ID | Lens | Claim (file:line) | Verdict | Severity after check | Evidence | Fix needed |
|---|---|---|---|---|---|---|
| OUT-01 (dups: INS-01) | outside, insider | Drawing the page, the Settings card and `status` run file checks on `-input` paths read from the untrusted record. Nothing first filters non-local paths, and nothing bounds the wait. Code: `health/mailboxgone.go:84,89` → `autoplace.go:150,167,170`, called at `desktop/archives.go:218`, `settingscard.go:209` and `health/health.go:511`. A run also resolves record paths: `app/sourceloss.go:156-158`. | CONFIRMED (the look). MECHANISM SHOWN, not run: the Windows network sign-in, and the page waiting on stalled storage | MEDIUM | `TestZZSkeptic175_OUT01_DrawingThePageNeverLooksAtAPlantedNonLocalPath`: "drawing the page looked at the planted path "\\qz-host\s\x": the banner says Repair only because os.Stat found it". Twin in health: "the helper said yes because os.Stat found it", and status says "if a mail account was removed…". MA-869 says such a path "is skipped before any file check". | Yes |
| OUT-02 | outside | After Remove from list, an armed delete is refused as "asked from another row's panel" (`archivedelete.go:621-623`) | CONFIRMED (the words only) | LOW | `TestZZSkeptic175_OUT02_…`: refused "This delete was asked from another row's panel…". The folder is kept. Delete permanently afterwards deletes it with "No row for this folder was found in Your archives". That last part was already so (MA-964). | Yes (words) |
| OUT-03 | outside | `delSend` keeps the token in `delTokens` until the reply comes (`deletescript.go:349-350`). `pagehide` posts a discard for every held token (`:491`). | CONFIRMED | INFO | `TestZZSkeptic175_OUT03_…`: "delSend posts the delete while its token is still in delTokens (dropped at 599, posted at 338)". When the discard lands first, the delete is refused "That delete is no longer waiting…" and the folder is kept. | Yes (small) |
| OUT-04 | outside | The server checks the plan only when a request says `armed` (`archivedelete.go:620`) | REFUTED | none | Nothing is wrong today. The only automatic send carries `armed:true` and the row (`deletescript.go:320` → `:349`). The "changed" re-ask sends no delete (`:354`). | No (optional structural guard) |
| OUT-05 (dups: CON-05, CON-06) | outside, concurrency | (a) A plan whose page is gone waits 24 hours. (b) An encrypted archive's plan, with values read using its key, outlives Lock. | REFUTED as defects. Both are true. | INFO | (a) Design §3.1 accepts it ("waits out its 24 hours. That is bounded"). (b) Lock calls `discardFor(key)` (`dashboard.go:302`, `pending.go:238-240`). A delete plan's key is `"delete\x00"+k` (`archivedelete.go:80-83`), so Lock never matches it. This was so before 1.7.5 (`pending.go:133`, "holds no secret"). No route returns the values after Lock. | No. The lead may write down "kept through Lock" or end it there (owner's call). |
| INS-02 | insider | `GoneSkippable` never asks whether a live account still owns the folder (`autoplace.go:146-161`) | CONFIRMED | MEDIUM | `TestZZSkeptic175_INS02_…`: "an account Thunderbird still lists, moved to …/QZUSB/imap.qzb.example, was skipped as gone (… err <nil>); its new folder is read: false". The run even reads the account's name: "qz-imap-qzb-example@example.com". | Yes (owner picks the form) |
| INS-03 | insider | `os.Lstat` runs on the path as typed, before cleaning (`autoplace.go:150`, `:153`) | CONFIRMED | LOW | `TestZZSkeptic175_INS03_…`: "GoneSkippable(…/imap.qzb.example/) is true for a dangling link". The same for `/.` and `zz-dangling/../imap.qzb.example`. A folder that is truly gone, typed with "/", is still skipped (positive twin passed). | Yes |
| INS-04 | insider | On Windows, junctions below the root count as in place (`autoplace.go:170,178`) | MECHANISM SHOWN, not run (Windows) | LOW | `parentInPlace` relies only on `filepath.EvalSymlinks`. That Go 1.23 and later does not follow junctions is recalled, not read. Design §4, step 4 already owes this junction check to the lab. | Lab first |
| INS-05 | insider | An unmounted disk's mount point looks like a real parent folder (`autoplace.go:165-183`) | MECHANISM SHOWN, not run (Linux) | LOW | Any real folder passes rule (c). There is no device check. Rare setup. | Owner/lead call |
| INS-06 | insider | A gone mailbox's Name and Path read back with control characters stripped only, and not capped (`lastrun.go:410`) | CONFIRMED | LOW | `TestZZSkeptic175_INS06_…`: "Name is not cleaned as an account name: got "qz@qz.example\u202etxt.exe\u200baaa…" (222 runes)" and "Path keeps a right-to-left override". Only a planted or damaged record shows this: the run writes names cleaned (`sourceloss.go:252`). | Yes |
| INS-07 | insider | A planted old first-found date silences a new loss (`sourceloss.go:157-160`, `184-196`) | REFUTED | INFO | True, but it needs write access to the archive folder. Anyone with that can already plant an "ok" record for a failing archive. No rule makes planted values true. | No |
| CON-01 | concurrency | The WARN names a calendar day, but the rule counts exact hours (`mailboxgone.go:31-33`, `:155-156`) | CONFIRMED | LOW | `TestZZSkeptic175_CON01_…`: "the backup of 2026-11-15T02:00:00-05:00 still WARNs, while the WARN says that from the first backup on or after Nov 15, 2026 it is only a note". | Yes (words) |
| CON-02 | concurrency | A clock that is behind at one run moves the first-found date earlier for good (`clampGoneSince`, `sourceloss.go:217-222`) | CONFIRMED | LOW | `TestZZSkeptic175_CON02_…`: "4 days after the mailbox was first found gone (2026-11-01), status has no WARN: first-found carried as 2026-10-22". | Lead's call (it changes an MA-1472 test case) |
| CON-03 | concurrency | An empty Path prints a broken sentence (`mailboxgone.go:135-156`). 1.7.4's `RecordRunWarning` drops path, name and since when it writes the record back (1.7.4's `tbcompleteness.go:696-722`). | CONFIRMED | LOW | `TestZZSkeptic175_CON03_…`: "no longer on this computer: , not found since Nov 19, 2026 … put it back where it was". | Yes |
| CON-04 | concurrency | A refusal at discovery drops every skip warning (`run.go:656-687`) | REFUTED as a defect. It is true. | INFO | The next run restarts with a later date, so the WARN lasts longer. §1.3 accepts that direction. Only R9's text (design lines 229-235) is wrong: 1.7.5's own refusals reach rule 2 too. | Text only |

---

**Confirmed: the smallest fix, and what each tombstone test asserts**

- **OUT-01**
  - Fix, part 1: `GoneMailboxRepairable` says no before any file check when the named input, or any listed input, is non-local by `isRemotePath`'s rule. Move that rule to one shared place (for example `autoplace`), so health and the Desktop use the same one.
  - Fix, part 2: the Desktop gets the answer inside `withinSourceWait` (archives.go:218, settingscard.go:209, and the health view's Assess). It could pass the answer in through `health.Input`.
  - Fix, part 3: `resolveGoneSince` skips record warnings whose Path is non-local, before `GoneSourceID`.
  - Tombstone: written like MA-869's test. Plant a failed `-auto` record naming `\\evil-host\share\Mail` and `//evil-host/share/Mail`, both as the refused input and as another input, with a streak past the threshold.
    - The helper says no.
    - Status, the banner and the Settings card keep today's words.
    - A seam over autoplace's and health's file checks records no look at evil-host. The seam is new code.
    - A stalled-check twin draws the page within `sourceWait`.
    - Positive twin: MA-1479's local case still says Repair.
  - Who can plant it: on macOS and Linux, a streak file planted by another user cannot be read (it must be mode 600, so only its owner can read it, `securefile.go:205`). So the cross-user attack is a Windows one. There, the look would sign in to the planted host (recalled, not run). With no attacker, a profile under a redirected APPDATA whose share is offline makes every page draw wait.
- **INS-02**
  - Smallest safe fix: refuse, as 1.7.4 did, when Thunderbird's settings still list a live account for the gone folder and that account's folder setting names another place. In `matchWith` terms, that is a hostname match, not a folder match: the account was moved, not removed.
  - Owner question: refuse, or skip with true words that name the new folder?
  - Tombstone: the red test's profile. `DiscoverInputs` refuses with `sourceUnreachableError` naming the old folder (`assure.Refused`: nothing read, streak moved).
    - Positive twins: the account removed from the list is skipped; an account whose folder setting is unchanged, with the folder deleted by hand, is skipped as Q4 rules.
  - Recalled, not checked: Thunderbird does not move the files itself, so this case also needs the person to delete the old folder.
- **INS-03**
  - Fix: `goneSkippable` also runs `Lstat` on the cleaned path, and skips only when both spellings say "does not exist".
  - Tombstone: with `acct` a dangling link, `acct/`, `acct/.` and `dangling/../acct` are not skippable, and `DiscoverInputs` refuses naming the input.
  - Positive twin: a truly gone `acct/` is still skipped.
  - Reach: only a hand-typed command line, cron or Task Scheduler line, or a hand-edited job file. `schedule`, the wizard and the Desktop clean the path first (`job.go:288`).
- **INS-06**
  - Fix: `ReadLastRun` cleans Name with `state.CleanFolderName` and Path with `util.StripBidi`. Cap how many `mailbox_gone` warnings status shows and `resolveGoneSince` scans (for example 64, as the completeness record caps accounts).
  - Tombstone: a planted record reads back with Name equal to `CleanFolderName(planted)` and Path with no U+202E. With 10,000 planted warnings, status shows at most the cap.
- **CON-01**
  - Fix: name the first local day on which every backup is a note. When since plus 14 days is not local midnight, name the next day. The friction review owns the words.
  - Tombstone: since Nov 1 at 15:00 local. A record started Nov 15 at 02:00 WARNs and names Nov 16. A record started Nov 16 at 00:30 is a note.
- **CON-02**
  - Fix: a first-found date later than this run's start is kept, not moved to the start. Status already treats a date after the run's start as "no time", so it keeps warning. A date written by a clock far ahead then warns longer, which is the direction §1.3 accepts. Status's "on or after" date must then come from since, not from the run's start.
  - Tombstone: first found at T, one run at T minus 10 days, then a run at T plus 4 days: WARN. Positive twin: at T plus 15 days, a note. The encrypted path shares `clampGoneSince`, so it is covered too.
- **CON-03**
  - Fix: `mailboxGone` says nothing for a warning with an empty Path, as the run's rule 1 already does (`sourceloss.go:158`). Or it says a fixed sentence with no path.
  - Tombstone: an ok record with `{code: mailbox_gone, message: x}` and no path never shows "computer: , not found". Positive twin: with a path, today's WARN.
- **OUT-02**
  - Fix: when the armed request names the held plan's own row (`in.Row == heldPlan.row`) but `matchRow` no longer finds it, refuse with true words, for example "This archive was removed from Your archives while its check ran, so nothing was deleted." The friction review owns the words. Every other mismatch keeps `armedOtherRowText`, so MA-1476's pinned cases stay as they are.
  - Tombstone: plan X, remove X from the list, send the armed delete. It is refused with the new words, the folder is there, no deletions.log line is written and the token is kept.
  - Whether Delete permanently should still work after Remove from list was decided before 1.7.5 (MA-964). It is the owner's to reopen, not this fix's.
- **OUT-03**
  - Fix: `delSend` removes the token from `delTokens` when it sends. It puts the token back when the reply refuses before the delete began and keeps the token (no `id`, no `planUsed`).
  - Tombstone: a page-harness scenario. Send a delete and hold its reply, then fire `pagehide`: no discard is posted for that token. A refused-before-begun reply, then `pagehide`: the discard is posted.

**The lead's three notes**
1. **OUT-01 and INS-01 are the same finding.** They are merged under OUT-01. INS-01 adds the `status` command and the run's own look at `sourceloss.go:156-158`. Both are in the row above.
2. **INS-02 reproduced** with the repo's helpers: `tbPrefsJS` with `directory` set to a folder outside the places, and the old `ImapMail/…` folder removed.
3. **CON-02 and the MA-1472 test case.**
   - `TestTheRecordKeepsWhenAMailboxWasFirstFoundGone` (`internal/app/ma1472_gone_since_test.go:96-108`) plants a first-found date 48 hours after now. It asserts that the next run records its own start instead. It asserts the same for a 2020 date and a zero date.
   - Judged on the person's outcome, that rule can cut the WARN short, or end it at once after the clock resets to a firmware date. That breaks the design's own "longer, never shorter".
   - The fix flips only the 48-hour assertion. That is the operator's call; I do not grant it to myself. The source-loss streak (MA-891) has the same weakness.

**Left in the worktree** (untracked, not committed, no covers marker):
- /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-adv/internal/health/zz_skeptic_175_health_test.go
- /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-adv/internal/state/zz_skeptic_175_state_test.go
- /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-adv/internal/app/zz_skeptic_175_app_test.go
- /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-adv/internal/desktop/zz_skeptic_175_desktop_test.go

**Smallest check for you:** run `go -C /Users/alex.dimarco/repositories/mail-archive-tool/.claude/worktrees/r175-adv test -p 2 -count=1 -run ZZSkeptic175 ./internal/health/` and see three red tests. That is CON-01, CON-03 and OUT-01.

**Owner questions:**
- INS-02: should a moved account refuse, or warn with true words?
- CON-02: may the MA-1472 48-hour test case be amended?
- OUT-05 (b): should Lock end a delete plan?

Working under the Limen method. Clock read: 20 Nov 2026. Register: a finding report, the skeptic's verdicts. Sources: the worktree's code, tests and design (reached); 1.7.4's code through git (reached). Not checked: Windows' network sign-in on a UNC look, Go's junction rule and Thunderbird's Local Directory behaviour (all recalled). Borrowed lines: test failure lines and code names, quoted as they are. Drift: none noticed. Touched: 4 untracked test files only. Persona: none.


---

## Finder report: OUT (outside aggressor, the Desktop's wire)

**Verdict:** Part 3 (the delete routes) holds against an attacker on the wire. One MEDIUM regression sits elsewhere in the Desktop: drawing the page now looks at mail paths taken from the archive's own untrusted record.

**Header**
- **Date:** 2026-11-20. This is the session's date. I ran no clock command, because the brief limits Bash to `git -C`.
- **FROZEN_SHA:** 43d4e4c, worktree r175-adv.
- **Read:**
  - the finder brief and adversarial-review.md
  - design §0 and §3
  - the pre-code review's header and L2-02
  - the diffs of archivedelete.go, pending.go, dashboard.go, archives.go, deletescript.go, localschedule.go, settingscard.go, health.go and jobrun.go
  - all of archivedelete.go and pending.go, and deletescript.go lines 1–95 and 228–491
  - d.api (workspace.go:104–138), GuardLocalPOST, and the Host and session lines of localweb
  - archdelete: execute.go:40–160 and 300–400, the DeletePlan struct, planAny, the synced line, result.go's Finishes and Held, discover.go's notRemoved, lister.go's seal line
  - autoplace.go, health/mailboxgone.go, util.ReadSecureFile, backuproute.go:195–232, maintenance.go:251–284, the ma869 test
- **Not read:**
  - lenses.md (as told)
  - the page-script harness and the MA-1475 to MA-1477 tests
  - part 1's run.go and sourceloss.go, and part 2's wizard code
  - archindex and keyOf
  - other finders' reports
- **Ran:** no tests and no programs.

---

**OUT-01 — MEDIUM — Drawing the page now touches mail paths from the archive's untrusted record: no network check, no time limit (breaks MA-869)**

- **Claim:** `health.GoneMailboxRepairable` (internal/health/mailboxgone.go:62–96) calls `autoplace.GoneSkippable(named)` (:84). That function runs `os.Lstat(path)` first (autoplace.go:150), before any pattern check. Then it runs `os.Stat(in)` on every listed `-input` (mailboxgone.go:89–91).
- **Where it runs, on every page draw:**
  - archives.go:218, for each "Your archives" row with a loud source-loss record (dashboard.go:1577).
  - settingscard.go:209 (dashboard.go:1711).
  - Status & health, through health.go:511 (maintenance.go:279).
- **What is missing:** none of these calls checks `isRemotePath` or runs inside `withinSourceWait`.
- **The existing rule:** backuproute.go:200–215 says a path in the record "is skipped before any file check, so drawing the page never reaches out to another host". The rebuild check does that: it checks `isRemotePath` first (localschedule.go:100) and runs inside the time limit (dashboard.go:1666). Catalog MA-869 (scenario-catalog.md:1207) requires both.
- **Sequence (Windows):** someone who can write the archive folder plants two files. This is C-17's own threat model ("anyone who can write the archive").
  - The source-loss record, with values that make `SurfaceLoudly()` true and `LastKind` set to "local-source-unreachable". `ReadSecureFile` checks no owner or ACL on Windows (securefile.go:205).
  - `.mailarchive-lastrun.json`: plaintext, status failed, job `["-auto","-input","\\\\evil-host\\share\\Mail"]`, error exactly `autoplace.GoneText` of that path. `ReadLastRun` checks no owner (lastrun.go:388).
  - Then the person opens or reloads the Overview, or the Status & health view.
- **What goes wrong:** every page view runs `os.Lstat` on `\\evil-host\share\Mail`. The attacker needs no knowledge of the person's Thunderbird profile, because the Lstat comes before the pattern match.
  - Recalled, not read: Windows then opens an SMB session to that host with the person's sign-in (an NTLM exposure).
  - If the host does not answer, the page waits for each look to time out. MA-869 also forbids that wait.
- **Why tests stay green:** the existing MA-869 watcher only records the `lstatSource` and `findProfileDir` seams (ma869 test:77–86). Its planted record also says status "ok" (:104), so the new helper returns false before it looks at anything.
- **Red test:**
  - Plant the record and source-loss file above, draw GET / and GET the health view, and assert that no file check names evil-host. This needs a seam on autoplace's and health's stat calls, or a check that the helper drops `isRemotePath` inputs first.
  - A stalled-stat twin: the page still draws within `sourceWait`.

**OUT-02 — LOW — "Remove from list" does not end the row's delete plan, which now waits 24 hours**

- **Claim:** `unlistAction` (archivedelete.go:1162–1218) only ends delete plans through `afterSwitch`, and only when it cleared the current archive (:1207–1209). Delete permanently never matches the row again (:604–655). An armed request whose row is gone is refused as `armedOtherRowText` (:621–623). That refusal is untrue: nobody used another row.
- **Sequence in one tab** (the archive is not the current one and has no schedule to stop):
  1. Delete… on row X, type the name, click **Delete when the check finishes**.
  2. While the check runs, click Remove from list on X, then Yes. The armed panel survives both `delClose` (deletescript.go:48) and `delRowGone` (:87–92). The page's reload is held.
  3. The check ends. The armed delete is refused: "This delete was asked from another row's panel…".
  4. The plan is shown with Delete permanently enabled and the name already typed (:360). One click deletes the folder that Remove from list had just promised to keep ("The folder and its files are kept").
- **Same in two tabs**, for up to 24 hours (was 10 minutes).
- **What goes wrong:** "acts only on a row of Your archives" is enforced only when the plan is made. The armed refusal also tells the person something false.
- **Limit:** if the archive had schedules, the re-check catches the difference and refuses it as changed.
- **Red test:** plan X, remove X from the list, then send the delete without `armed`. `assure.Refused` with words that say X is no longer in the list, the folder still there, and no deletions.log line. Then the same with `armed`, refused with true words.

**OUT-03 — INFO — Closing the page can still cancel a delete the panel said "can no longer be stopped"**

- **Claim:** the token stays in `delTokens` until the delete's reply comes back (deletescript.go:350). The `pagehide` handler (:491) posts a discard for every held token. If that discard lands between deleteAction's `get` (archivedelete.go:604) and `take` (:651), the delete is refused "no longer waiting".
- **Sequence:** the delete is sent (armed, or by Delete permanently), then within milliseconds the person reloads or closes the tab.
- **Effect:** it fails safe: nothing is deleted. But the panel had already said "It can no longer be stopped", and no result is held for the next page.
- **Red test:** in the page harness, send a delete, then fire `pagehide`. Assert no discard is posted for the sent token. The fix is to drop the token from `delTokens` when the delete is sent.

**OUT-04 — INFO — Rescue step, the deeper weakness: the server's check only applies when the page says "armed"**

- **Claim:** a request without `armed` is checked on its token and typed name only (archivedelete.go:604–613, 620). The server cannot tell a person's click from a script's.
- **What this means:** "never deletes unseen a plan that needs a decision" still depends on every automatic send in the page carrying `armed:true`. Today there is one such send (deletescript.go:318–320 into :349), and it is correct. The "changed" re-ask (:354) asks for a new plan but sends nothing.
- **What goes wrong:** nothing today. A later page change that drops the flag would bring L2-02 back, with the server's check silently not applying.
- **Red test:** a structural page-script check that every post to `api/archive/delete` outside the Delete permanently click carries `armed:true` and the panel's row.

**OUT-05 — INFO — Plans now stay in memory for up to 24 hours**

- A plan keeps the folder's whole file list. One plan per row can be held for a day when the page closes without saying so, which `pagehide` cannot cover for a killed browser or a computer that slept.
- An encrypted archive's plan survives its Lock: its key is not the archive's (pending.go:78–87). It keeps the values read with the key (message count, mail dates, first run; archivedelete.go:260–270) for up to 24 hours instead of 10 minutes. The page already holds the same text, so no response carries anything new.

---

**Held up** (attacks that failed, and the code that stops each)

| Attack | What stops it |
|---|---|
| Another web page posts to the new routes (CSRF) | d.api accepts POST only and runs GuardLocalPOST: loopback Host, loopback Origin, X-CSRF-Token compared in constant time (workspace.go:116–129; server/setup.go:174–183). The session cookie is SameSite=Strict (session.go:265). |
| DNS rebinding | The Host allow-list on every route (localweb.go:71) and the Host check in GuardLocalPOST (setup.go:175). |
| Guessing a token, or using discard to test one | Tokens are 16 random bytes (pending.go:158–160). Discard always answers `{ok:true}` (archivedelete.go:712–713). |
| Discarding a plan of another kind | `take` with `isDeletePlan` leaves other kinds untouched (pending.go:203–209). |
| Keeping a token alive | The timer is set once, at `put` (pending.go:178). `get`, discard and the armed refusals never reset it. |
| Getting more authority from the 24-hour life | None gained: the same CSRF token and session already make a fresh plan. A restart makes a new CSRF token (dashboard.go:292) and an empty store, and old pages are refused (workspace.go:121–127). |
| Armed request naming the wrong row | Exact-spelling `matchRow` compared with `heldPlan.row` (archivedelete.go:621). The page sends its button's path, captured when the panel opened (deletescript.go:229, 349). |
| Armed request on a plan that is not armable | Refused at archivedelete.go:625. |
| Armed request after Cancel, or a late reply | Cancel and late replies discard the token (deletescript.go:74, 396–401); the server finds nothing (archivedelete.go:604–607). |
| Replay or second request | `take` is one-use (archivedelete.go:651). |
| Two tabs on one row | `put` replaces the old plan and discards its token (pending.go:167–187). |
| Archive changed between check and delete | `samePlan` re-checks under the lock (execute.go:340–352, 372–378). It covers every blocking input except the synced line, which comes from the path (archdelete.go:705–707), and held files, which only appear on a plan that finishes an earlier delete and so is never armable (archdelete.go:524–526). |
| Archive paused during the wait | The pause file's name starts `.mailarchive-` (seal.go:29; names.go:53), so the re-checked file list differs and the delete is refused as changed. |
| "Changed" re-ask deleting by itself | It opens a new panel that is not armed (deletescript.go:354). |
| Switch to another archive during an armed check | The schedules the switch rule protects are compared by `samePlan` (archdelete.go:801–813). Settings are written only where they still hold what was loaded (archivedelete.go:986–999). |
| Reload, Shut down or restart during the check | An orphan plan has no token holder and is replaced by the row's next plan. After Shut down, `put` keeps nothing, so the page gets an error (pending.go:162–166, 249–254). |
| New data in JSON responses | Only `armable` (a true/false value, :497) and `changed: true` (:686–688) are new, plus fixed refusal words. `data-delete-name` is the last part of a path the row already carries (archives.go `DeleteName`), escaped by the template, with no folder read. |

---

Register: findings report. Borrowed lines: the code quoted above, attributed by file and line. Drift: none noticed. Touched: nothing changed in the worktree. Persona: none. Sources reached: the source files listed under "Read". Not reached: Windows' SMB behaviour (recalled) and how Go's timers handle sleep (the design's own recalled point).


---

## Finder report: INS (insider and local file tricks)

**INS lens report: insider and local file tricks (part 1 first)**

- **Date:** 2026-11-20, from the session's date line. I ran no clock command, because the brief allows only `git -C WORKTREE …` in Bash.
- **FROZEN_SHA:** 43d4e4c, read-only worktree `r175-adv`. Nothing was changed, built or run.
- **Read:** the brief; `assurance-kit/process/adversarial-review.md`; the design lines 1–698 (§0, §1, §2 and the start of §3); the diff stat; the full diffs of `run.go`, `sourceloss.go`, `lastrun.go`, `encrypted_record.go`, `health.go`, `mailboxgone.go`, `jobrun.go`, `wizard.go`, `main.go`, `localschedule.go`, `archives.go` and `settingscard.go`; all of `autoplace.go`. Also the parts I needed of `RunHeadless`, `newRunRecord`, `writeRefusedRun`, `recordRun`, `ReadLastRun`, `ReadSecureFile`, `isRemotePath`, the Desktop's job log (`capture.go`, `maintenance.go`), `matchAccountIn` and `ReadStoreFacts`. Catalog rows MA-869, MA-1288, MA-1471 and MA-1480. I searched `review-1.7.5-predesign.md` for overlap.
- **Not read:** `lenses.md` (as told); the design from line 699 on; the code for parts 2 and 3; most test files; other finders' reports.
- **Recalled, not checked here:**
  - Go 1.23's change that stops `EvalSymlinks` following Windows junctions.
  - The POSIX rule that a trailing "/" makes `lstat` follow a link, and Go's Windows `Lstat` doing the same.
  - Go mapping ERROR_BAD_NETPATH to "does not exist".
  - Thunderbird's "Local Directory" behaviour.

**Verdict:** encrypted-archive privacy and the account-name log rule (MA-1288) hold. The skip rule mostly holds, with two MEDIUM gaps: an untrusted record makes status and the Desktop page touch paths it names (a network path included), and an account that was moved, not removed, is skipped quietly. There are also four LOW findings and one INFO.

---

**INS-01 — MEDIUM — Paths named in the untrusted record are looked at when the page is drawn and when status runs**

- **Claim:**
  - `health.GoneMailboxRepairable` (`internal/health/mailboxgone.go:62-97`) reads the input paths from the archive's last-run record. It calls `autoplace.GoneSkippable(named)` (`:84`), whose first step is `os.Lstat(path)` (`autoplace.go:150`). Then `os.Stat` (`autoplace.go:167`), `EvalSymlinks` (`:170`, `:178`) and `os.Stat(in)` for each listed input (`mailboxgone.go:89`).
  - It is called on every Desktop page draw for each source-lost row (`archives.go:218`), by the Settings card (`settingscard.go:209`) and by `status` (`health.go:511`).
  - Nothing first applies the `isRemotePath` filter, and nothing bounds the wait with `sourceWait`.
  - This breaks MA-869 (C-17): "a network (UNC) or non-local path planted in the record is skipped before any file check — drawing the page … never look[s] at it; drawing the page never waits on source storage." It also breaks the page's rule SD-1 (`archives.go:171-178`).
  - The rebuild path keeps MA-869 (`localschedule.go:100`, before `lstatSource`). The new helper does not.
  - A run that skips also calls `autoplace.GoneSourceID(w.Path)` on each `mailbox_gone` path in the previous record (`sourceloss.go:158`). That is an `EvalSymlinks` on a path the record names.
- **Sequence:**
  1. Someone who can write into a plaintext archive's folder plants two files. That could be an archive on a NAS share, a shared synced folder, or a USB drive. On Windows, `ReadSecureFile` checks no owner or mode (`securefile.go:205`), and `ReadLastRun` reads any regular file (`lastrun.go:388`).
  2. File one, `.mailarchive-lastrun.json`: `status:"failed"`, `job:["-out",X,"-mode","incremental","-auto","-input","\\\\qz-host\\s\\x"]`, and `error` set to exactly `autoplace.GoneText` of that input.
  3. File two, `.mailarchive-sourcestreak.json`: `last_kind:"local-source-unreachable"`, with `first_unreachable` at least 3 days before `last_unreachable`, so the streak is loud.
  4. The person opens MailArchive Desktop, or runs `mailarchive status -out X`. `GoneSkippable` runs `os.Lstat("\\qz-host\s\x")`.
- **What goes wrong:**
  - On Windows, that look-up opens an SMB session to the planter's host with the person's sign-in. That sends their NTLM credential hash, which can be cracked or relayed.
  - On any system, a stalled share or an automount path (macOS `/net/host/...`) holds the page or status with no bound.
  - **An accident, with no attacker:** a source on a network-redirected APPDATA that went offline gets recorded in the "does not exist" words. Every page draw then waits on that share's timeouts.
- **Red test:** use the style of MA-869's `TestAPlantedNetworkPathIsNeverLookedAt`, with this planted failed `-auto` record and loud streak, and a seam over the file look-ups in health and autoplace.
  - Assert that `archiveRows`, `settingsCardFor` and `health.Assess` never look at `\\qz-host\s\x` or `//qz-host/s/x`, and say today's words.
  - Assert that a stalled look-up does not hold the draw past `sourceWait`.
  - Positive twin: MA-1479's local case still says Repair.

**INS-02 — MEDIUM — An account moved (not removed) with Thunderbird's "Local Directory" setting is skipped quietly forever**

- **Claim:**
  - `GoneSkippable` (`autoplace.go:146-161`) asks only whether the folder is gone, sits in a place auto-discovery looks, and has a real parent there. It never asks whether a live account still owns that folder.
  - The code already knows the answer. `goneAccountName` (`sourceloss.go:243`) calls `ReadStoreFacts`. Through `matchAccountIn`'s hostname rule (`thunderbird.go:660-664`), that matches the live account whose directory now lies elsewhere, and returns its name.
  - The design's reason for skipping ("one the same run would back up by itself if it came back") is false here. `autoDiscover` (`run.go:2103`) globs only the standard places.
  - The owner was told "an unplugged backup drive still stops the backup". That no longer holds for this mailbox.
- **Sequence:**
  1. An "All of them" job lists `P/ImapMail/imap.qz.example` and `P/Mail/Local Folders`.
  2. In Thunderbird, Account Settings → Server Settings → Local Directory, the person points the account at `/Volumes/QZUSB/imap.qz.example`. This is Mozilla's documented way to move an account. The old folder is then removed; `ImapMail` stays.
  3. The next run returns ok, with no email.
  4. Status warns: "… no longer on this computer: NAME (PATH) … If you didn't remove it, add the account back in Thunderbird". But the account is still in Thunderbird.
  5. After 14 days of backups this becomes a quiet note. The mail in the account's new folder is never read, whether the USB drive is plugged in or not.
  6. 1.7.4 refused every run, with an email, a notice and the 30-day pause.
- **Twin:** an Outlook `.pst` moved out of `Documents\Outlook Files` and reopened in Outlook from another drive. That case can't be checked from a file, so it is INFO only.
- **What goes wrong:** a live mailbox stops being backed up for good. Its warning fades to a quiet note, and its words are wrong for this case.
- **Red test:** a profile whose account is listed in `mail.accountmanager.accounts` and has `directory` outside the places, with its old `ImapMail` folder gone.
  - `DiscoverInputs(…, true)` refuses with `sourceUnreachableError` naming the old folder, checked with `assure.Refused`.
  - Positive twin first: the same account removed from the accounts list is skipped.

**INS-03 — LOW — A trailing "/" (or "/." or "link/..") gets a dangling link past rule (a)**

- **Claim:**
  - `goneSkippable` calls `os.Lstat(path)` on the path as given (`autoplace.go:150`), and cleans it only afterwards (`:153`).
  - With a trailing separator, the OS follows the final link. Go on Windows does the same with a trailing `\`.
  - So a dangling link reads as gone. The cleaned path matches a place, and its parent is real, so it is skipped.
  - The fold promised "a dangling link … is not skipped". The pre-code review's line 289 finding (gone checked with `os.Stat`) is therefore only part fixed.
- **Sequence:**
  1. `P/ImapMail/acct` is a link to `/Volumes/QZUSB/acct`, or a junction to `E:\acct`, and the drive is unplugged.
  2. The person runs `mailarchive -out A -auto -input P/ImapMail/acct/`. Tab-completion adds the "/".
  3. It exits 0 with "Not found, so skipped". 1.7.4 refused.
  4. The same happens through a hand-written cron or Task Scheduler line, or a hand-edited job file.
  5. It is not reached through `mailarchive schedule` (`job.go:288` cleans the path with `abspath`), the wizard or the Desktop.
- **Red test:** `goneSkippable` returns false, and `DiscoverInputs` refuses, for a dangling link spelled `…/acct/`, `…/acct/.` and `…/dangling/../acct`. Positive twin: a truly gone folder spelled with a trailing "/" is still skipped.

**INS-04 — LOW — Windows junctions below the root are not resolved, so rule (c) counts them "in place"**

- **Claim:**
  - `parentInPlace` relies on `filepath.EvalSymlinks` (`autoplace.go:170`, `:178`).
  - From Go 1.23 on, `EvalSymlinks` does not follow junctions on Windows. `go.mod` sets 1.26.0 and has no `winsymlink` setting. This is recalled, not run.
  - So a profile folder or `ImapMail` that is a junction is treated as in place. Design §1.2 (c) and MA-1471 say it refuses.
  - The tests use only `os.Symlink` (`ma1471_gone_mailbox_test.go:207`, `:239`). The row itself says the junction case was not run, and the pre-code review lists it as "Not reached" (line 566).
- **Sequence:** `mklink /J %APPDATA%\Thunderbird\Profiles\p.default D:\TB\p.default` with D: plugged in, and one account folder removed on D: → skipped. The design says refuse.
- **What goes wrong:** only the design's "something larger may be missing" case. With D: unplugged, `os.Stat(parent)` (`:167`) still refuses.
- **Red test (Windows lab):** MA-1471's link-below-root refusal, repeated with a junction, is refused. Otherwise the design must say junctions count as in place.

**INS-05 — LOW — An unmounted filesystem's mount point looks like a real, empty parent folder**

- **Claim:** `parentInPlace` (`autoplace.go:167-182`) accepts any real folder that is not a link. The empty mount-point folder of an unmounted disk qualifies.
- **Sequence:**
  1. On Linux, a second disk is mounted at `P/ImapMail` (fstab, `nofail`). The disk is missing at boot.
  2. Every account folder is "gone", while `ImapMail` is an empty real folder.
  3. Local Folders, in `P/Mail` on the main disk, remains, so rule (d) holds.
  4. All the mail accounts are skipped. The run is ok, with no email; a warning, then a quiet note.
  5. This is the unplugged-drive case. It is a rare setup.
- **Red test:** with a seam for the device check, a parent on a different device from the pattern's resolved root is treated as outside the place, and refused.

**INS-06 — LOW — The gone mailbox's Path and Name read back from disk are only control-stripped and not capped**

- **Claim:**
  - `lastrun.go:410` applies `StripControl` only.
  - The same reader cleans the completeness record's account names with `CleanFolderName`. That removes invisible format characters, the bidi overrides among them, and caps a name at 80 characters. It also caps accounts at 64 (`tbcompleteness.go:166-180`, `:202-208`; `lastrun.go:424`).
  - `mailboxGone` prints `w.Name` and `w.Path` uncapped (`mailboxgone.go:135-157`), for every warning (`health.go:793-823`).
- **Sequence:** plant a record of up to 4 MB (`lastrun.go:696`) with:
  - a Name like `"qz@qz.example\u202Etxt.exe"`, or a look-alike of a real account name padded with zero-width characters;
  - thousands of `mailbox_gone` warnings.
- **What goes wrong:** status and the Desktop's health view show reversed or look-alike text, or a flood of lines. The same flood makes `resolveGoneSince` do one `EvalSymlinks` per planted warning (`sourceloss.go:158`).
- **Not reachable:** markup injection (the page is built with textContent only, `pages_scripts.go:4`); email alerts (`backupalert` reads no warnings); Since (a time, clamped).
- **Red test:** a record planted as above reads back with Name cleaned as `CleanFolderName` cleans, Path without bidi characters, and a capped number of gone-mailbox lines in status.

**INS-07 — INFO — A planted, believable old date can silence a new loss**

- `resolveGoneSince` and `resolveEncryptedGone` (`sourceloss.go:155-160`, `:184-196`) carry any date between 2024-01-01 and now.
- A planted plaintext warning for this input with `since` 2024-02-01 shows the quiet note at once.
- So does a planted encrypted record with `mailbox_gone_since` 2024-02-01 and `mailbox_gone_count` 999,999: that count is under the 1,000,000 cap, so any later loss keeps the old date.
- It needs write access to the archive folder. Noted only.

---

**Held up (attacks that failed, and the line that stops each)**

- **A dangling link spelled cleanly:** `autoplace.go:150`. `Lstat` finds the link, so the run refuses.
- **A link below the root on macOS or Linux:** `autoplace.go:170-182`. The resolved parent lies outside the place, so the run refuses.
- **A link at or above the root whose drive is unplugged, or a gone profile or `ImapMail`:** `autoplace.go:167`. The run refuses.
- **Letter case on macOS or Linux:** `autoplace.go:205-211` compares paths as spelled. It can only refuse.
- **Unicode normalisation on macOS:** the fixed parts of the patterns come from `$HOME`; profile and account names fall under `*`; `EvalSymlinks` keeps the spelling it was given. A mismatch can only refuse.
- **8.3 short names:** above the root they fail rule (b) and refuse. Below the root, `EvalSymlinks` resolves them to long names and the result is in place. No false skip.
- **Trailing dots or spaces on Windows:** Windows strips them, so `Lstat` finds the real entry. No skip.
- **A profile on another drive (`profiles.ini` absolute path):** matches no pattern (`autoplace.go:154-157`). Refused.
- **Nothing remaining:** `run.go:2077`. Refused.
- **Local Folders as the only remaining mailbox:** the owner's Q4 ruling, as built.
- **Encrypted record:**
  - `newRunRecord`'s encrypted branch (`run.go:1420-1435`) and `marshalLastRun` (`encrypted_record.go:161-178`) write only `mailbox_gone`, `mailbox_gone_since` and `mailbox_gone_count`.
  - Running, refused (`run.go:1497-1503`), cancelled and failed records (`recordRun` from `begun`) are all minimal.
  - Run history stays minimal (`runhistory.go:143-145`), and the attention notice is unchanged.
  - A `-log` inside an encrypted archive is refused (`main.go:616-622`). The job file stays in the settings folder. Run now's note and job lines stay in memory (`capture.go:321-338`).
- **Account names in log files (MA-1288):**
  - The run's own log line gives the path only (`run.go:672-674`).
  - The command line's summary names an account only on a terminal (`main.go:2411-2414`, `:2456`).
  - The scheduled run's log gives counts only (`wizard.go:608-615`).
  - Alerts read no warnings.
- **A rebuilt schedule from a planted record:** `localschedule.go:100` filters network paths first. `:104` requires a recorded store, and the record is read with the owner check (`:28`).
- **1.7.4 beside 1.7.5:** 1.7.4 reads 1.7.5's records leniently (`lastrun.go:395`). A 1.7.4 refusal only restarts the date later, so the warning lasts longer.
- **A planted count below 1 or above the cap, or a planted date in the future or before 2024:** clamped (`sourceloss.go:184-196`, `clampGoneSince`; `mailboxgone.go:23-25`).

---

Counters: Register: finding report, adversarial pass, INS lens. Borrowed lines: quoted code identifiers and catalog words only. Drift: none known. Touched: nothing written. Persona: none. Source reached: worktree source, yes. Go 1.23's release notes, POSIX `lstat` rules and Thunderbird's behaviour: recalled, not reached. Every reproduction is left to the skeptic.


---

## Finder report: CON (integrity, time and concurrency)

**Finder report: CON (integrity, time and concurrency), MailArchive 1.7.5**

Date: 2026-11-20. This is the session's date line, not a clock I read, because the brief allows only `git -C` in the shell. Reviewed commit: 43d4e4c, compared against 304871c.

**Verdict:** nothing HIGH or MEDIUM. The races and the delete hold. I found three LOW gaps in the 14-day count and the record's format, and three INFO notes.

**What I read**
- The brief and the adversarial-review process.
- The design file `docs/design-1.7.5-fixes.md`, lines 1 to 957 (§0 to §3 and the start of §4).
- The full diff for `run.go`, `sourceloss.go`, `autoplace.go`, `lastrun.go`, `encrypted_record.go`, `health.go`, `mailboxgone.go`, `localschedule.go`, `archives.go`, `settingscard.go`, `pending.go`, `archivedelete.go`, `dashboard.go`, `deletescript.go`, `jobrun.go`, `wizard.go`, `picker.go` and `main.go`.
- The code around the diff: `Run` and its record writers, `state/sourceloss.go`, `archdelete/execute.go` (lines 40 to 440), the anchor code in `lister.go`, `discover.go`, `RunHeadless`, and `RecordRunWarning` in both versions.
- The MA-1472 tests in `app` and in `health`.

**What I did not read**
- The design from line 957 to the end, the catalog rows, and `review-1.7.5-predesign.md`.
- The page-script harness and the tests for MA-1471, 1473 to 1477, 1479 and 1480.
- The rest of `internal/thunderbird`, `archdelete`'s `Plan` and the lister, and the inner workings of `container`.
- `lenses.md`, by instruction. I read no other finder's report and ran nothing.

---

**CON-01 (LOW): the WARN promises a calendar day, but the rule counts exact hours**
- **Claim:**
  - `internal/health/mailboxgone.go:31-33` decides with an exact length of time: the WARN stays while `started.Sub(since)` is under 14×24 hours.
  - `mailboxgone.go:155-156` tells the person "from the first backup on or after DATE this is only a note". DATE is `since` plus 14 days, printed as a local day.
- **How it happens:**
  - The mailbox is first found gone by a Run now on Nov 1 at 15:00, and the archive backs up daily at 2:00 AM.
  - The Nov 15 backup at 02:00 is only 13 days 11 hours later, so it is still a WARN. The note comes on Nov 16.
  - With a weekly schedule, suppose the first-found run started late (the computer woke at 3:05) and the run 14 days later started on time. That run is a few minutes short of 14 days, so the WARN lasts a third week.
  - Near a daylight-saving change, adding 14×24 hours can print the day before.
- **What the person sees:** status says "from Nov 15 this is only a note", and on Nov 15 it still warns. With a weekly schedule it warns for a whole extra week.
- **Red test:** Since = Nov 1 15:00 local, the record's start = Nov 15 02:00 local. Status must say the note, or print a date the rule actually honours. Counting by local calendar days would match the words. The current test only checks day 13 and day 15, never this edge.

**CON-02 (LOW): a clock that is behind moves "first found gone" earlier, for good**
- **Claim:**
  - `clampGoneSince` (in `internal/app/sourceloss.go`) replaces any first-found time that is later than this run's start with this run's start. The design meant this for "a clock that was ahead".
  - When it is this run's clock that is behind, the true date is replaced by the wrong, earlier one.
  - Once the clock is right again, rule 1 carries that earlier date forward as believable, because it is after 2024-01-01 and before the start.
  - Encrypted archives do the same through `resolveEncryptedGone`.
- **How it happens:**
  - Real first-found date: Nov 1.
  - At one backup on Nov 2, the clock reads Oct 23. The first-found date becomes Oct 23.
  - The clock is fixed. The Nov 6 backup is 14 days after Oct 23, so it is already a note.
  - Variant: the very first skip runs with the clock reset to the firmware date, for example a 2025 date after a flat battery. The next backup is a note at once.
- **What the person sees:** the WARN is cut short or never shows, and status says "not found since Oct 23", before the mailbox went. The design itself says such restarts should only ever make the WARN longer, never shorter (§1.3, "Why not a small file").
- **Note:** the source-loss streak has the same weakness (`Observe`, the MA-891 class). From the records alone you cannot tell "the old date was ahead" from "this clock is behind".
- **Red test:** record a first-found date T, run with the clock at T minus 10 days, then run at T plus 4 days. Status must still warn.
  - Possible fix: keep a later date as it is. Status already treats a date after the run's start as "no date", so it keeps warning. The catch is that a date written by a clock far ahead would keep warning until that date passes.
  - This contradicts the current MA-1472 test, which expects a planted future date to become this run's start. It is the lead's call.

**CON-03 (LOW): an older 1.7.4 program can strip the new fields, and status then prints an empty name**
- **Claim:**
  - In 1.7.4, `app.RecordRunWarning` (`internal/app/tbcompleteness.go:696-722` at 304871c) is called from `internal/backupalert/tbbehind.go:163` after 5 failed email sends.
  - It reads the record using 1.7.4's warning format, which has no `path`, `name` or `since`, and writes it back. That drops those three fields from every mailbox-gone warning but keeps the code and the message.
  - 1.7.5's status (`mailboxgone.go:135-156`) then prints "no longer on this computer: , not found since <the run's date>". The last sentence falls back to "put it back where it was".
  - The next run's rule 1 ignores warnings with an empty path (`w.Path != ""`), so the first-found date restarts.
- **How it happens:**
  - Both versions are active on one computer during an upgrade: an old Desktop still running, or a schedule pointing at an old copy.
  - A 1.7.4 run ends ok, and its Thunderbird email then fails 5 times.
  - Meanwhile a 1.7.5 run writes a record that includes a skip.
  - The 1.7.4 alert step then adds `alert_send_failed` to that record.
  - The window is narrow. The same broken words appear for any damaged record whose path is empty. `ReadLastRun` removes control characters (`lastrun.go:410`), so a path made only of control characters also ends up empty.
- **What the person sees:** a broken sentence in status and the health view, and the WARN starting over.
- **Red test:** a plaintext ok record with `{code: "mailbox_gone", message: "x"}` and no path. Status must not print "computer: , not found".

**CON-04 (INFO): a refusal at discovery drops every skip warning**
- **Claim:**
  - When `DiscoverInputs` refuses (`run.go:2049`, or the "nothing remains" branch), it returns no skipped list. So `run.go:657-676` adds no warnings, and `RecordRefusedRun` (`run.go:684`) writes a failed record without them.
  - The next run restarts every first-found date. The one input the refusal named can instead take the streak's first date through rule 2.
- **How it happens:**
  - Mailbox A has been gone since Nov 1 and has been a note since Nov 15.
  - On Dec 1, the profile's `ImapMail` folder is briefly missing, so the run refuses on rule (c), naming A.
  - On Dec 2 the folder is back, and A warns again, "not found since Dec 1", for 14 more days.
- **What the person sees:** a warning comes back for a mailbox they already accepted as gone, with a later, untrue date. The design accepts that direction (longer, not shorter). But its note R9 says rule 2 is reached only "on the way up from 1.7.4", and 1.7.5's own refusals reach it too.
- **What to do:** fix the text of R9, or have the record keep the previous warnings on a refusal at discovery.

**CON-05 (INFO): a delete plan that finishes after its page has gone waits 24 hours**
- **Claim:**
  - The page discards only the plans it already holds (`delTokens` in `deletescript.go`). A check still running when the page reloads or closes has no token yet. Its plan is stored (`archivedelete.go:422`) and waits out the 24 hours (`pending.go:174-178`).
  - Second case: Cancel clicked just as the check's reply lands. `endPlan` runs while the panel holds no token yet, the reply then fills the panel that is about to close, and that plan is not discarded until the page goes away.
- **What the person sees:** nothing goes wrong with their data. There is at most one plan per row. The next plan for the row replaces it, a switch or Shut down ends it, and it cannot delete anything without the typed name.
- **Test:** none needed, unless the lead wants the gap closed.

**CON-06 (INFO, for the confidentiality lens): an encrypted archive's delete plan now outlives Lock by up to 24 hours**
- **Claim:**
  - The plan holds the archive's sealed figures, read with its key: message count, the mail's date range and the first run (`archdelete.go:120-136`).
  - Nothing that locks the archive discards delete plans. Only a switch (`dashboard.go:704-710`, `:728`), Shut down, uninstall and forget-all do.
  - Before 1.7.5 the plan was gone after 10 minutes. Now it can last 24 hours.
- **What the person sees:** sealed figures stay in the Desktop's memory long after Lock. A Delete permanently clicked after Lock writes them into the deletion record.
- **Test:** after a plan, Lock the archive, then check whether the plan was ended. Or write down that it is kept on purpose.

---

**Attacks that held up**
- **Scheduled run and Run now at the same time:**
  - Both read the record before taking the lock.
  - The loser is refused at the lock and writes nothing (`run.go:721-724`, `emptied.go:213-220`).
  - A losing scheduled run skips quietly (`wizard.go:342-346`).
  - `RecordRefusedRun` skips writing when the lock is held (`run.go:1474-1477`).
- **A run cancelled, crashed, killed, or refused after discovery:**
  - The warnings join the run's own list before the "running" record is written (`run.go:669`, `:788`, `:1442`).
  - The refused record and the final record are built from that same list (`run.go:1498`, `recordRun`).
- **Encrypted record:**
  - The flag, the first-found date and the count come from the run's warnings (`run.go:1432-1434`).
  - They are written only beside the flag (`encrypted_record.go:175-177`).
  - A planted count below 1, or over 1,000,000, is read as 1.
- **Dates:**
  - A first-found date in the future, or before 2024, is replaced by the run's start (`clampGoneSince`).
  - In status, a date after the run's start counts as no date, so the WARN shows (`mailboxgone.go:23-25`).
  - Status never compares with today's date.
- **A mailbox that comes back and then goes again:**
  - The run that finds it again records no warning, so the next loss starts a new date.
  - Rule 2 needs a failed record whose error is that mailbox's own refusal.
- **Two mailboxes gone at different times:** plaintext keeps one date per input, matched by `GoneSourceID`. For encrypted archives, a higher count restarts the date and the same or a lower count keeps it (`len(skipped) <= count`).
- **The 1.7.4 streak seeding (rule 2):** it applies only when the streak's last kind is `local-source-unreachable` and the previous run failed with this input's exact refusal words (`autoplace.GoneText`).
- **1.7.4 reading a 1.7.5 encrypted record:** it reads the record with plain `json.Unmarshal`, which ignores unknown fields (`lastrun.go:395`).
- **The Desktop's schedule rebuild while a run is going:**
  - The "running" record lists the same job (`run.go:1442`).
  - A gone input is kept only if it is one of the archive's recorded stores (`localschedule.go:104`).
  - The rebuild needs at least one listed input that is there.
- **A backup between the delete plan and the delete:**
  - The plan is made again under the archive lock and must match.
  - Plaintext: the manifest's size, time and SHA-256, and every name to remove (`execute.go:340-353`, `:372-378`).
  - Encrypted: each save writes under new file names (`container/store.go:879-880`), so the names differ and the delete is refused.
- **Can a backup start after the re-check but before the delete? No.**
  - The delete holds `.mailarchive.lock` (`names.go:137`), the same lock backups use, from `lockAndRecheck` until the files are removed (`execute.go:227-246`).
  - Backups take that lock without waiting, so they are refused.
- **The armed delete:**
  - The token is read, the name, row and "nothing to decide" checks run, then the token is taken. A token is never reused, and a new plan of the same row replaces the old token (`archivedelete.go:604-655`, `pending.go:157-213`).
  - Both armed refusals keep the token and change nothing.
  - After a "changed" refusal, the page asks for the plan again without arming it, so deleting needs a new click.
  - The page's count of waits it is holding always comes back to zero, so its reloads are not held forever.
- **The armed delete's second check:** the plan made again under the lock is compared on every "nothing to decide" condition except one, a synced folder. Files held by another program only arise in a plan that finishes an earlier delete, which is never armable (`archdelete.go:524-526`). For the gap to matter, a folder would have to become synced during the second walk, which is not a practical case.
- **The schedule answer:**
  - Only the daily or the weekly row installs a schedule.
  - Cancel and "No — just this once" install nothing.
  - A garbled answer is accepted only when exactly one row matches it.
  - The question repeats at most 10 times, then installs nothing (`wizard.go`, `askSchedule`).
  - The Thunderbird sync question that follows needs the exact "Yes". Anything else means no sync, and it never installs a schedule by itself.
  - No answer, on any system, defaults to daily, and nothing loops forever.

---

**Method notes (Limen):** this is a finding report. The sources I drew on are the worktree's code and design text, all reached. I did not check by running anything; each finding names the test that would settle it. To settle CON-01 fastest, run the health test with the record's start at Nov 15 02:00 and the first-found time at Nov 1 15:00. Borrowed lines: none. Drift: none noticed. Touched: nothing written. Persona: none.
