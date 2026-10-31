# Pre-code design review: the attended Thunderbird sync (Run now really syncs, skips are told, encrypted archives can sync)

**Verdict: GO_WITH_CONDITIONS, not ready for slice AS1 as rev 1 was written.** 47 conditions
(C-1 to C-47) are folded into the design's rev 2 (`docs/design-presync-attended.md`, each fold
marked with its C-id). Five of them need an owner or operator answer before the slice they touch
(listed under "Owner and operator decisions" below). The rev-2 folds have **not** been
re-verified by a second reader; that re-verification is owed before AS1 starts.

| | |
|---|---|
| Date | 2026-10-30 (the review findings were read on 2026-10-30; the task text was dated 2026-10-28) |
| Register | a finding: the synthesis of a pre-code design review, done by reading only |
| Design reviewed | `docs/design-presync-attended.md` **rev 1**, sha256 `107c75bafbaa503de02370cbc3c256df0c8694eea0e7b0c9b587665f46b0a012` (untracked at the time of review) |
| Repository | branch `presync-attended`, commit `ea6515aca8bea1d268ecd3c697045f35371d48ad` |
| Process | `../assurance-kit/process/design-review.md` |
| Reviewers | A model-run workflow, not a named human. Five lens finders (safety, security, UX, data, tests), one skeptic pass that checked every finding against the design text and the code, then one synthesiser (this record and the rev-2 fold). Findings were weighed against the code, not accepted on authority. |
| Commands (synthesiser) | `shasum -a 256` on rev 1 (status 0); `git rev-parse HEAD` (status 0); a grep of `internal/presync/run.go` (line 23: `sessionBudget = defAwaitUp + defMaxWait + defQuitGrace + 30*time.Second`; lines 29-49: `runWatchdog`, whose timeout returns `OutcomeInternalError` with no release) and of `internal/presync/env_darwin.go` (line 109 `quitTimeout = 20 * time.Second`; line 428 `context.WithTimeout(context.Background(), quitTimeout)`); a grep of `docs/scenario-catalog.md` (no `R38`; highest MA row MA-894). Nothing was built and no test was run. |
| Rescue pass | **Not run in this workflow.** See "What this review did not do". |

Labels. **Proven** = read in the code at the cited line on this commit (read, not run).
**Inferred** = reasoning, not run. The skeptic's verdict is CONFIRMED when the design as written
allows the failure, REFUTED when it could quote what already handles it, NARROWED when it confirmed
the failure but changed its scope or grade.

## Why GO_WITH_CONDITIONS and not NO_GO

The owner's four rulings and the design's shape survive: an attended mode that skips only the two
idle checks, the outcome carried into the plaintext records, an elapsed-time 3-day warning, nothing
about the sync written beside an encrypted archive, and the key cleared before Thunderbird is asked
to close. Every HIGH finding has a modest, named change that the skeptic confirmed. None needs a
re-design.

The heaviest roots, in order:

1. **Quitting outside S8 and quitting late.** The new Cancel quit (A4) skipped the "more than one
   Thunderbird" check, the 2-minute attended quit request let a held quit reach a newly opened
   Thunderbird, and a watchdog that fires leaves a session able to quit later and holding its
   profile lock forever inside the long-lived Desktop. Folded as one shared pre-quit function (C-1),
   a computed time limit (C-2), an abandoned-session rule (C-3) and keeping the 20-second request
   (C-6).
2. **The Lock did not end every wait.** The quit request ran on its own background context, the
   watchdog ignored cancellation, and a cancel that came before the launch still launched
   Thunderbird. Folded as C-5 (every quit takes a context the Lock ends; the watchdog returns on a
   Lock) and C-10 (no launch after a cancel).
3. **The outcome would never reach the record.** `app.Run` replaces its result late in the run,
   and the post-hook cancel check sat before `defer recordRun`, leaving "running" records. Folded as
   C-12 and C-11.
4. **"Scheduled" and "sync configured" were never defined.** A plaintext Run now already runs
   with `Unattended: true`, and a missing hook cannot tell "sync off" from "hook setup failed".
   Folded as C-33 and C-34.
5. **No test seam reaches the Desktop and wizard paths**, and the catalog-first slice would turn
   the covers-map gate red. Folded as C-41 and C-42.

## Lenses run

| Finder | Lens(es) of the battery it covered | Findings |
|---|---|---|
| SAF | 7 failure domains, with parts of 1 (purpose: R31 fit) | 10 |
| SEC | 6 security / adversarial | 9 |
| UX | 4 usability / friction, with parts of 10 (honesty of texts) | 15 |
| D | 3 durability, 8 migration | 10 |
| T | the repo's testing discipline (assurance kit), with parts of 5 (integration: build order) | 16 |
| Skeptic | all of the above, plus six missed findings (M1-M6) | 6 |

**Not run as their own lens:** 2 (focus / proportionality) and 9 (dependency and operational
cost). Lenses 1, 5 and 10 were covered only as parts of other finders' work. The process asks for
all ten; this is stated for the operator (operator decision OD-1), not self-certified.

## Findings

Severity is the skeptic's final grade; "was X" marks a re-grade. Full finder text is kept in the
workflow trace; each row here is the claim, the skeptic's evidence and where it was folded.

### Safety (SAF)

| ID | Sev. | Finding (design section: failure) | Verdict: evidence | Condition |
|---|---|---|---|---|
| SAF-1 | HIGH | §2.4 A4: the Cancel quit path leaves out C3, so on macOS a bundle-wide quit closes a second Thunderbird (breaks R31). | CONFIRMED, proven. C3 lives only in S8 (`session.go:236-244`); `macEnv.quit` addresses the bundle (`env_darwin.go:426-431`). | C-1 |
| SAF-2 | HIGH | A3, `run.go:23-50`: the longest attended path (~1544-1557 s) exceeds the 1540 s limit; a fired watchdog returns with no release, the goroutine can still quit under the reader, and the profile lock stays held for the Desktop's life, so every later sync ends `profile-busy` and the 3-day warning never fires. | CONFIRMED, proven. `run.go:47-48`; the pid lock is stale only when its holder dies (`lockfile.go:83, 112`). | C-2, C-3, C-4 |
| SAF-3 | MEDIUM | §2.5 residual (a): the person opens our hidden Thunderbird during the wait; at quiet it is asked to quit with no check; a draft prompt can hold the quit past the grace, then Thunderbird exits during the read. | CONFIRMED. The fix (a "Keep Thunderbird open" button) is a product addition: owner ruling. | C-46, OQ-1 |
| SAF-4 | MEDIUM | §1.2 ruling 2 on Windows: Thunderbird ignores SW_HIDE, shows, and is minimized only after adoption (up to ~17 s) while the person is typing. | CONFIRMED for the window showing (`quit_windows.go:71-76, 106-118`); taking keyboard focus is inferred. | C-47, OQ-2 |
| SAF-5 | MEDIUM | A4 S6: a Cancel in the first ~90 s quits nothing; the `launching` record's weak match then treats the person's own later Thunderbird as the leftover until reboot. | CONFIRMED, proven (`session.go:158-160, 296-302`; `recovery.go:50-61`). | C-7 |
| SAF-6 | MEDIUM | §3.2: "~15 s from Lock to key cleared" fails during the S8 request and the A4 quit (up to ~2¼ min). | CONFIRMED, proven (`env_darwin.go:428`). Duplicate of SEC-2(1)(2). | C-5 |
| SAF-7 | MEDIUM | A3: a 2-minute held quit request can reach a Thunderbird the person opened meanwhile. | CONFIRMED as plausible, inferred; keeping the 20 s bound until lab L-A2 answers is the cautious default. | C-6 |
| SAF-8 | MEDIUM | The write-ahead record is best effort; with it missing, our hidden copy reads as `already-running`, which §5.2 counts as synced, so it is never announced. | CONFIRMED, proven (`session.go:62-66, 117-119, 142-143`). | C-8 |
| SAF-9 | LOW | The C3 count drops processes `ps` cannot read, so it can undercount and quit the person's Thunderbird. | CONFIRMED, proven (`env_darwin.go:364-368`). | C-1 |
| SAF-10 | LOW | §2.2: an `ssh -t` session passes the foreground-terminal test. | CONFIRMED (`encrypted.go:188-190`); only if AS10 ships. | C-9 |

### Security (SEC)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| SEC-1 | HIGH ((a) narrowed to MEDIUM) | (a) A Lock between the click and the hook's start cancels nothing; the run then starts Thunderbird with the key held. (b) Any cancel before S5 still launches Thunderbird: `runSession` has no `ctx.Err()` check before `startHidden`, and `app.Run` calls the hook with a done context. | (b) CONFIRMED, proven (`session.go:69-151`; `run.go:577-588`), stays HIGH. (a) CONFIRMED (`run.go:548, 571`) but NARROWED: ruling 4 says "while it waits for Thunderbird", so widening the window to the lease is the owner's call. | C-10, OQ-3 |
| SEC-2 | MEDIUM | The key outlives a Lock by ~2 min in the S8 request, the A4 quit, and an uninterruptible walk under a watchdog that ignores `ctx`. | CONFIRMED for macOS, proven (`env_darwin.go:426-428`; `run.go:44-49`; `quiet.go:92-141`). Windows quit not read. | C-5, C-3 |
| SEC-3 | MEDIUM | §3.2 race: the window check and cancel are not stated atomic; the post-hook check sits before `defer recordRun`; a hook panic leaves the window open. | CONFIRMED, proven (`run.go:588-600`). Overlaps D1. | C-11 |
| SEC-4 | MEDIUM | A Lock during the read phase replies plain ok; the run's own lease keeps the key for hours while the person believes it is gone. | CONFIRMED, proven (`encryption.go:134-139`; `schedule.go:677-684`). | C-13 |
| SEC-5 | MEDIUM | §4.8: the plaintext Run now log, if opened before the encrypted-archive refusals, writes cleartext beside an encrypted archive (0644). | CONFIRMED, proven (`runlog.go:32`; `wizard.go:222-235`; `schedule.go:664`). | C-14 |
| SEC-6 | LOW | §4.4: an encrypted job's activity (lines, result, new sync line) stays served by `/api/activity` after a Lock or switch. | CONFIRMED (`maintenance.go:34`); switch not read. | C-15 |
| SEC-7 | LOW | MA-904 scans for words, not keys; in-memory `LastRun.Presync` for keyed runs is one refactor from a leak; the streak writer is gated by prose. | CONFIRMED (`encrypted_record.go:131-157`). | C-16 |
| SEC-8 | LOW | Two residuals widen, not "unchanged": the cleartext Started/Finished span now includes the wait (hints at a Thunderbird source), and the decrypted manifest sits in memory through the wait. | CONFIRMED, proven (`run.go:548, 571`; `encrypted_record.go:133-141`). Owner acceptance. | C-17, OQ-4 |
| SEC-9 | LOW | §8.2 route: nil `drawnFor` let through, `d.api` not named, a missing `presync` read as off, the edited job file may not be the one Run now runs, locked behaviour unstated, a job-named profile skips the `profiles.ini` check. | CONFIRMED, proven (`schedule.go:211-213`; `kindevidence.go:116-140`; `hook.go:32`). | C-18 |

### UX

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| UX-1 | HIGH | One waiting text for start-up, wait and close; the counter can read "23 min so far, up to 20"; no text tells the person to answer macOS's question; the log line overwrites the note every 1.5 s. | CONFIRMED (`dashboard.go:2136`). | C-19 |
| UX-2 | HIGH | The 3-day warning always says "click Run now", which does not fix most miss reasons; for `owner-active-skipped` the warning returns three days later unexplained. | CONFIRMED (§2.3 keeps those checks). | C-20 |
| UX-3 | HIGH | The Lock note vanishes when the page reloads 6 s later, and nothing on the reloaded page says Thunderbird is still running. | CONFIRMED (`encryptionscript.go:89`). | C-21 |
| UX-4 | MEDIUM | A reload during a ~25-minute Run now loses its note and Cancel and labels errors "Capture failed:". | CONFIRMED (`dashboard.go:2073, 2083`). | C-22 |
| UX-5 | MEDIUM | The cancel line is wrong in S6 and in the read; the outcome cannot tell "closed" from "left open"; the Cancel tooltip omits the close and its on-exit actions. | CONFIRMED (`dashboard.go:1502`). | C-23 |
| UX-6 | MEDIUM | The texts are Mac-only ("Dock", "out of sight") but the feature ships on Windows. | CONFIRMED. | C-24 |
| UX-7 | MEDIUM | The encrypted Run now tooltip is not corrected for a sync-on job. | CONFIRMED (`dashboard.go:1623`). | C-25 |
| UX-8 | MEDIUM | The encrypted card reuses texts about scheduled backups it can never have. | CONFIRMED (`api.go:105`; A14). | C-25 |
| UX-9 | MEDIUM | No control or press feedback named for the encrypted box; the card's existing Save is refused for every encrypted save. | CONFIRMED. | C-26 |
| UX-10 | MEDIUM | X16 requires status and the health view to show the sync setting and its last result; for an encrypted archive neither shows. | CONFIRMED (`health.go:598`). | C-27, OD-3 |
| UX-11 | MEDIUM | "(see Status)" points at a Status that shows only the last run, and Status would point at itself. | CONFIRMED (`health.go:641-660`). | C-28 |
| UX-12 | MEDIUM | No email wording; it goes under a checkbox labelled "a failed backup", so a helpdesk reads it as a failure. | CONFIRMED (`dashboard.go:1633`). | C-29 |
| UX-13 | LOW | The note promises Thunderbird's draft prompt, which is unverified (L-A5). | CONFIRMED. | C-30 |
| UX-14 | LOW | `SyncNotice` is one text shared with surfaces that have no Run now. | CONFIRMED (`api.go:91-99`). | C-31 |
| UX-15 | LOW | Existing sync-on owners get a silent behaviour change on their next Run now. | CONFIRMED (judgement). | C-32 |

### Data (D)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| D1 | HIGH | The outcome never reaches the last-run record (`result` is replaced at `run.go:782`, early returns give `Result{}`), and the post-hook check before `defer recordRun` leaves a "running" record shown RED. | CONFIRMED, proven (`run.go:600, 622, 635, 695, 705, 782`; `health.go:626-627`). | C-12, C-11 |
| D2 | HIGH | "Scheduled" is undefined; the obvious `opts.Unattended` is true for a plaintext Run now, so a missed Run now would extend the streak. | CONFIRMED, proven (`jobrun.go:75, 130`; `run.go:1245`). | C-33 |
| D3 | MEDIUM | "No hook" cannot tell "sync off" from "hook setup failed" or a manual CLI run; both would delete the streak and silence the warning. | CONFIRMED, proven (`jobrun.go:132-135`). Same as T9. | C-34 |
| D4 | MEDIUM | `ToldFor = FirstMissed` breaks under MA-891's clock restarts; `==` on times is fragile; implausible-time counting unstated; told-file written before delivery; a run can send both the recovery and the sync email. | CONFIRMED, proven (`sourceloss.go:85-88`; `streak.go:146-151`; `wizard.go:326-327`). | C-35 |
| D5 | MEDIUM | §4.2 persists `attended` while MA-898 forbids any JSON tag for it. | CONFIRMED. | C-36 |
| D6 | MEDIUM | Archive-writable text (`LastName`, the presync name) reaches Status and email; `ReadLastRun` does not strip `Presync`. | CONFIRMED, proven (`lastrun.go:280-286`). | C-37 |
| D7 | LOW | Which surface's warning words are saved is unstated; health shows the saved message on both surfaces. | CONFIRMED, proven (`health.go:645-656`). | C-38 |
| D8 | LOW | Downgrade then upgrade: a stale `FirstMissed`, and v1.5.1 rewrites the history dropping `presync`. | CONFIRMED, proven (`runhistory.go:75-91`). | C-39 |
| D9 | LOW | Uninstall leaves `<config>/presync/`; `ClearStreak` leaves `<key>.sync-alert`. | CONFIRMED, proven (`uninstall/config.go:91-94`; `streak.go:204-206`). | C-40 |
| D10 | LOW | §5.2 omits `state-unreadable`. | **REFUTED.** The table's last row is a catch-all ("everything else"), so it falls in "missed", and today it can never be the main outcome (`recovery.go:40-43`; `session.go:106-109`). Naming it is still clearer: folded editorially. | (C-45, editorial) |

### Tests (T)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| T1 | HIGH | No seam puts a scripted pre-sync into the real Run now, run-job or Desktop paths: `SetHook` is a no-op on the Linux CI gate, and `fakeEnv` is test-only inside `presync`. | CONFIRMED, proven (`hook_other.go:16`). | C-41 |
| T2 | HIGH | AS0's "pending" U/S rows turn the covers-map gate red. | CONFIRMED, proven (`covers_map_test.go:86-97`). | C-42 |
| T3 | HIGH | `env.quit` takes no context, so the attended bound and the cancel-quit's own context cannot be observed. | CONFIRMED, proven (`env.go:78`; `env_darwin.go:428`). | C-5 |
| T4 | MEDIUM | One package budget variable; the context-aware sleep lives in build-tagged adapters the Linux gate never runs. | CONFIRMED, proven (`run.go:23, 29`; `env_darwin.go:294`; `env_windows.go:85`). | C-2 |
| T5 | MEDIUM | AS1 builds A7, `OutcomeLine` and `Waited` with no AS1 test. | CONFIRMED. | C-43 |
| T6 | MEDIUM | The note is built in the page script, which CI can only string-check. | CONFIRMED (`p38_desktop_test.go:215`). | C-19 |
| T7 | MEDIUM | Nothing resets `syncing` and the Lock window after a hook panic or early cancel. | CONFIRMED, proven (`run.go:582-587`). | C-11 |
| T8 | MEDIUM (naming part refuted) | Cancel and Lock together, in both orders, are untested; the key-destroy seam is unnamed. | CONFIRMED for the orders and the seam (`schedule.go:677`). **Refuted** on test naming: `ci.yml:60`'s `-run` pattern already matches Lock, Cancel and RunNow. | C-5 |
| T9 | MEDIUM | Same as D3, from the test side. | CONFIRMED. | C-34 |
| T10 | MEDIUM | The AST walk for a literal misses a mode set by a variable; no behavioural twin; Windows `Foreground` equals `IsTerminal`. | CONFIRMED (`tty_windows.go:22`). | C-9, C-41 |
| T11 | MEDIUM | MA-904 checks named files only, has no plaintext twin, and its streak clause lands before the streak exists. | CONFIRMED (`internal/assure/leak/tree.go:66` exists). | C-16 |
| T12 | LOW | "No tag on encrypted rows" passes trivially. | CONFIRMED (`runhistory.go:73-79`). | C-16 |
| T13 | MEDIUM | The slice that first sets attended for Run now is unstated. | CONFIRMED. | C-44 |
| T14 | MEDIUM | Platform cases untested: the tooltip ignores the platform; `presyncOfferable` reads `runtime.GOOS`. | CONFIRMED (`localschedule.go:483-493`; `wizard.go:1749`). | C-24 |
| T15 | LOW | MA-914 cannot see the cancel cause at the Desktop level. | CONFIRMED (`jobcontrol.go:101`). | C-41 |
| T16 | LOW | MA-916's failing test edits the test's own reader, not product code. | CONFIRMED. | C-39 |

### Missed by the finders (skeptic)

| ID | Sev. | Finding | Condition |
|---|---|---|---|
| M1 | MEDIUM | Today's watchdog is already too short: unattended worst path ≈ 1444 s against 1440 s. A current bug, not a new one. | C-2 |
| M2 | MEDIUM | The Run now tooltip reads the default job name; Run now runs the job found by `BuildKindEvidence`, which can be another file. A sync-on job under another name starts and quits Thunderbird while the tooltip says nothing (an X16 gap). | C-18 |
| M3 | LOW | How `leftover-found` counts towards the streak, and its remedy, are unstated. | C-20 |
| M4 | LOW | "The idle lock cannot fire inside a wait" is false (poll routes do not touch the idle clock); harmless, but used as a reason. | C-17 |
| M5 | LOW | The plaintext Run now log is opened before the archive lock: a refused Run now still writes a header; rotation races a scheduled append. | C-14 |
| M6 | LOW | A6 would put a note-class `already-running` from recovery's `matchOther` into the side outcomes. | C-45 |

## Conditions (each folded into rev 2; the design marks every fold with its id)

| C | From | What rev 2 must say or do |
|---|---|---|
| C-1 | SAF-1, SAF-9 | One shared pre-quit function used by S8, the Cancel quit and the S6 cancel path: ownership re-check, then the C3 count from raw `pgrep` pids with any unreadable pid counted as a second Thunderbird, then record `quitting`, then one request. MA-900 twins: Cancel with a second Thunderbird → zero quits, `left-running`; one pid unreadable → zero quits. |
| C-2 | SAF-2(1), T4, M1 | A pure, untagged `sessionBudget()` computed from every bound in the path plus stated slack; a test that the longest path (and the cancel-quit paths) fits; states today's 1440 s shortfall as an existing bug this fixes. |
| C-3 | SAF-2(2)(3), SEC-2(3) | On the watchdog firing, or on a cause that must stop at once (§3.2), the watchdog cancels the session's own context and sets an abandoned flag; the session checks it before writing `quitting` and before any quit request, and its quit context is derived from the session context so an in-flight request dies; an abandoned session releases its own profile lock and records `left-running` when it returns. `runWatchdog` also returns on the stop signal. |
| C-4 | SAF-2(4) | Three `profile-busy` results in a row on scheduled runs count as one miss, so a stuck lock cannot hide the streak. |
| C-5 | SAF-6, SEC-2(1)(2), T3, T8 | `env.quit(ctx, target)`; the session builds the deadline; every quit context (S8, the Cancel quit) derives from a stop context that only the Lock (and Shut down) ends, separate from the Cancel cause. §3.2 states the real worst-case Lock-to-key bound. MA-913 adds Lock during the S8 request, Lock during the Cancel quit, Lock-then-Cancel (zero quits) and Cancel-then-Lock (bounded), with the `runLease` seam named. The fake records the context and its deadline. |
| C-6 | SAF-7 | The attended quit request keeps the 20 s bound (A3's 2 minutes is withdrawn) until lab L-A2 proves a delayed quit cannot reach a newly opened Thunderbird. A quit that times out goes through `quit-not-permitted` with a desktop remedy. The delayed-delivery case is added to L-A2. |
| C-7 | SAF-5 | A Cancel in S6 keeps polling for the proven child up to the 90 s await limit on its own short context (ended by a Lock), then takes C-1's path; with no proven child it records that and says "Thunderbird may still be starting; quit it from the Dock/taskbar". A Lock still quits nothing. |
| C-8 | SAF-8 | S5 launches only after the `launching` record is saved; a failed save ends `cannot-start` (a warning) and launches nothing. A failed save of `quitting` or `left-running` is a side warning. §6 gains crash rows for a Desktop kill during attended S7, during the Cancel quit and during the S8 request. |
| C-9 | SAF-10, T10 (Windows) | An attended CLI run also requires a local session (no `SSH_CONNECTION`, no `SSH_TTY`); R38 says so; MA-899 adds the SSH case and states Windows' console rule. |
| C-10 | SEC-1 | (b) `app.Run` does not call the hook with a done context; `runSession` checks `ctx.Err()` right before saving `launching` and before `startHidden`. MA-900/MA-913 add "cancel before S5 → zero launches, no `launching` record". (a) proposed default: for a job carrying the sync, the Lock-cancel window opens when the Run now's lease is taken and closes when the hook returns — **owner ruling OQ-3**. |
| C-11 | SEC-3, T7, D1 (placement) | `stopSyncWait` checks the window and cancels under `c.mu`; the hook wrapper closes the window and resets `syncing` in a `defer`; the T16 cancel check runs after `defer recordRun` and `defer presyncRelease`, before the sweep, returning the current result. MA-913 drives the race through a pause seam; MA-906/MA-913 include a panicking hook. |
| C-12 | D1 | The outcome rides on the record value (`begun.Presync`, set inside the hook block like `begun.Warnings`); `Result.Presync` is re-set after the result is rebuilt and on every early return. MA-902/MA-903 drive a real `app.Run` through a success and a post-hook failure and read the last-run file on disk. |
| C-13 | SEC-4 | A Lock during an encrypted Run now's read phase replies with a note: the backup goes on with its own key until it ends; Cancel stops it. |
| C-14 | SEC-5, M5 | The Run now log opens only after `job.Read`, `RequireExistingOut`, `seal.RefuseWrite` and `RefuseKeyless`, in `RunHeadless`'s order; §6 shows the step. MA-907 adds "plaintext-path Run now on an encrypted archive leaves no `mailarchive.log`". A header on a run then refused by the archive lock, and a rotation racing a scheduled append, are stated residuals. |
| C-15 | SEC-6 | Lock of an archive and a switch away from it clear that encrypted job's activity (lines, result, sync fields); `runJobOn` resets the new sync fields with `lines` and `result`. MA-906 covers it. |
| C-16 | SEC-7, T11, T12 | MA-904 asserts the encrypted last-run's and every history entry's key set equals the A18 allow-list, runs `leak.CheckTree` plus a profile-path marker scan over the whole archive folder, after a synced, a skipped and a Lock-cancelled Run now, with a plaintext positive twin. `recordRun` leaves `Presync` nil when `opts.Key != nil`; the streak writer gates on `opts.Key == nil && !ArchiveEncrypted(out)`; a keyed run removes a stray sync-streak file. The streak clause moves to AS5. MA-905 plants a `presync` name in an encrypted history and still shows no tag, and checks every outcome name is mapped. |
| C-17 | SEC-8, M4 | §3.2, §3.3 and §4.4 state as **widened** residuals: the encrypted record's cleartext start/finish span includes the wait; the decrypted manifest, units and store sit in memory through the wait; the idle lock may fire during a long run and does not stop it. **Owner acceptance OQ-4.** |
| C-18 | SEC-9, M2 | The route is registered through `d.api`; `drawnFor` is required (nil refused); `presync` must be an explicit boolean; the edited file is exactly the job Run now would run (`BuildKindEvidence`'s `JobPath`, re-resolved at request time, `SameArchivePath`); switching on writes `ThunderbirdSync{}` with no profile; the route works while locked (stated why). In attended mode a job-named profile must also be in `profiles.ini`. The tooltip, the box and the route all use that one job path. |
| C-19 | UX-1, T6 | `/api/activity` carries a sync phase (`starting`, `waiting`, `closing`, `cancelling`) and a server-built note; the minutes count only in `waiting`, from the wait's start, and never exceed 20; the Mac closing text names macOS's question. The note text is built and tested in Go. |
| C-20 | UX-2, M3 | The 3-day warning's remedy is chosen by the last miss reason; "click Run now" only where Run now syncs; `owner-active-skipped` adds "change the backup's time"; `leftover-found` counts as missed and its remedy is "quit the Thunderbird left open". |
| C-21 | UX-3 | When the Lock reply carries a note, the page does not auto-reload; it shows a Done button. |
| C-22 | UX-4 | On page load, a running Run now re-attaches its poll beside Run now, with the button disabled. |
| C-23 | UX-5 | The cancel line is chosen by phase; the outcome carries `Left` (none / closed / left-open / may-be-starting) so the end line is true; the Cancel tooltip has a sync-window variant naming the close and its on-exit actions. |
| C-24 | UX-6, T14 | Words by platform (Dock / taskbar, out of sight / minimized); the tooltip and `jobThunderbirdSourced` take a `goos` parameter; Linux gets no sync promise. MA-915 covers darwin, windows and linux. |
| C-25 | UX-7, UX-8 | A sync-on variant of the encrypted Run now tooltip; encrypted-card variants of `SyncNotice` and the Mac note with no scheduled-backup sentences. |
| C-26 | UX-9 | The encrypted box has its own Save (the only Save on a sync-only card), posting to `api/runnow-sync`, `busy()` with "Saving…", plain success reply. |
| C-27 | UX-10 | The encrypted card and the Desktop health view show the sync setting (read from the job file in the settings folder) and the last result from memory for this Desktop session; `status` shows the setting and says the last result is not kept for encrypted archives. The remaining gap is an explicit X16 exception for the operator (OD-3). |
| C-28 | UX-11 | Each warning reason has a short plain phrase used by both the tag and the Status line; no "see Status". |
| C-29 | UX-12 | The email's subject and body are written in the design; the alert checkbox is relabelled to name it. Both go to the friction review. |
| C-30 | UX-13 | The note ships without the draft-prompt clause until L-A5 passes. |
| C-31 | UX-14 | `SyncNotice` wording fits every surface. |
| C-32 | UX-15 | A release-note line, and a one-time notice before the first attended sync on this computer. |
| C-33 | D2 | Defined: extend = `opts.Unattended && !attended && sync configured`; reset = any synced session; a run that is neither (manual CLI before AS10, a piped one after) counts as neither. MA-908 includes a plaintext Run now (Unattended true) whose sync missed: the streak does not move. |
| C-34 | D3, T9 | `Options.PresyncWanted` carries "the job asks for the sync" apart from "a hook ran"; a hook-setup failure on such a job is the outcome `not-set-up`, counted missed; the streak file is deleted only where the sync is switched off (Settings save, schedule edit) or by a scheduled run whose own job has no sync. |
| C-35 | D4 | The streak gains an integer `Serial`; `ToldFor` = that serial; `Missed` counts up on an implausible time; `ToldFor` is written only after delivery (or at once when alerts are off); a run that sends the recovery email defers the sync email to the next stale run. |
| C-36 | D5 | `attended` is not written to the record. |
| C-37 | D6 | Every surface shows presync names only through a closed name-to-words map (unknown → "not synced"); `ReadLastRun` and the streak reader strip `Presync` strings. Hostile-record cases in MA-909/MA-910. |
| C-38 | D7 | Presync warnings are rendered by code in each surface's words when read; the saved message is the fallback for unknown codes. |
| C-39 | D8, T16 | §10 states the downgrade residuals; MA-916 uses frozen golden v1.5.1 record bytes from `ea6515a`, the v1.5.1 structs copied verbatim, and a product-side mutation as its failing test; it is split into an AS2 record row and an AS5 streak row. |
| C-40 | D9 | Uninstall removes the presync state folder's named files and then the empty folder (no recursion, same overlap guards); `ClearStreak` also removes `<key>.sync-alert`. MA-910 asserts both. |
| C-41 | T1, T10, T15 | An untagged injection point (`presyncjob.runSession`, a package variable defaulting to `presync.Run`) that wizard and desktop tests replace with a scripted fake that blocks until its context ends, records `context.Cause`, calls the phase hooks and returns a chosen outcome. Named as the allowed external-binary mock (Thunderbird is the external binary). Quit counts are proven only in `presync`'s session tests. R38 gets a behavioural twin; MA-914 asserts the Cancel cause. |
| C-42 | T2 | AS0 accepts only R38, S64, S65 and the S54 and X16 amendments; each slice adds its own MA rows with their tests, so the covers-map gate stays green. |
| C-43 | T5 | MA-897 is extended in AS1 to cover the once-a-minute progress line, `OutcomeLine` for every outcome on both surfaces, `Waited` and `Started`. |
| C-44 | T13 | Run now passes attended only from AS4; MA-898's positive twin lands there. |
| C-45 | M6, D10 | Side outcomes from recovery are warn-class only; §5.2 names `state-unreadable` (side only, never counted). |
| C-46 | SAF-3 | Proposed: a "Keep Thunderbird open" button in the Run now note during the wait (skips only the quit; recorded left open by choice; the archive still runs). L-A5 on macOS and Windows becomes a gate before AS4 ships; if Thunderbird does not ask before discarding a message being written, attended quit does not ship. **Owner ruling OQ-1.** |
| C-47 | SAF-4 | The design states Windows does not meet ruling 2 (the window shows, then is minimized). Lab L-A7 tries a minimized-not-active start (SW_SHOWMINNOACTIVE); MA-918 and L-A1 assert Thunderbird does not take keyboard focus from a person typing elsewhere. Attended mode does not ship on Windows until that lab passes. **Owner nod OQ-2.** |

## Owner and operator decisions (not imposed by this review)

Owner rulings (product):

- **OQ-1 (SAF-3).** Add a "Keep Thunderbird open" button to the Run now note while it waits? And
  make "Thunderbird asks before closing a message you are writing" (lab L-A5) a ship gate, so that
  if it does not ask, Run now does not close Thunderbird at all? Proposed: yes to both.
- **OQ-2 (SAF-4).** On Windows, Thunderbird cannot start hidden: its window appears for a few
  seconds and is then minimized. Accept that ruling 2 is met on Mac only, and hold attended Run now
  on Windows until a lab shows Thunderbird starting minimized without taking the keyboard?
  Proposed: yes.
- **OQ-3 (SEC-1(a)).** Should Lock cancel a sync Run now at any point before it starts reading
  (from the click), not only while it waits for Thunderbird? Without it, a Lock in the first minutes
  of a large encrypted archive's Run now does nothing, and Thunderbird then starts with the key
  held. Proposed: yes.
- **OQ-4 (SEC-8, M4).** Accept two widened leftovers for encrypted archives: the start and finish
  times kept in clear beside the archive now include the Thunderbird wait (a long gap hints the
  archive comes from Thunderbird), and the unlocked archive's index stays in memory through the
  wait.

Operator acceptances (gates and invariants):

- **OD-1.** This review ran five finders, not ten lenses; lenses 2 (focus) and 9 (dependency cost)
  had no pass, and no rescue pass ran. Accept, or run them before AS1.
- **OD-2.** The new ids (R38, S64, S65), the S54 amendment, the X16 amendment, the amendment to
  phase 3 §3.2's "a Lock does not stop a Run now" rule, and the per-slice row plan of C-42.
- **OD-3.** The X16 exception for encrypted archives (C-27): `mailarchive status` shows the setting
  but not the last result.
- **OD-4.** The tightening of rev-3's PC3 for attended runs (C-18): a profile named in a job file
  must also be in `profiles.ini`.

## What this review did not do

- **No rescue pass** (step 3 of the process). The blockers were dissolved by the finders' and
  skeptic's named changes, not by a separate agent trying to save the design.
- **No re-verification of the rev-2 folds.** Rev 2's digest differs from the reviewed rev 1, so the
  affected verdicts are stale until a second reader checks each fold against its condition.
- Not read: `env_windows.go`'s quit path in full (SEC-2 is confirmed for macOS only);
  `container.Store`'s key copies after `vault.Key.Destroy`; `encryption_create.go` beyond its call
  site; `alert.go`; how a switch treats the capture state; any real Mac, Windows or Thunderbird
  behaviour (labs L-A1 to L-A7).
- No test was run.

Smallest checks that would settle the open points: add up the longest path with every probe bound
against C-2's computed limit; read `env_windows.go`'s quit for a context; on a Mac, see whether a
delayed `osascript` quit still reaches a newly started Thunderbird (L-A2).

## Owner answers and operator calls, 2026-10-30

Added 2026-10-30, after the review above; nothing above is changed. **Register:** a record of
decisions, as passed to the design writer by the workflow lead (the owner's answers relayed, the
operator calls made by the lead); not a re-review. The design was revised to **rev 3** to fold them
(`docs/design-presync-attended.md`, sha256 `6a63ef6db7ecda518adaccfb3d6c0f2d79433c1a301a370a183bb8902d47df26`),
each fold marked `[OQ-n]` or `[OD-n]`.

### Owner answers

| Q | Answer | What changes against this review's conditions |
|---|---|---|
| OQ-1 (SAF-3, C-46) | **Yes to both.** The Run now note shows a "Keep Thunderbird open" button while it waits; and if lab L-A5 shows Thunderbird would discard an in-progress draft without asking, Run now never closes Thunderbird (leaves it open, says so). | C-46 adopted as proposed, with one shape change: a failing L-A5 does not stop AS4 shipping; it switches that platform to "never close" (rev 3 §2.8: a per-platform table, false until a lab run sets it; new state step A12; new row MA-922). The button gets a route, `api/runnow-keepopen` (rev 3 §2.8, MA-919). |
| OQ-2 (SAF-4, C-47) | **Windows too, now.** Attended Run now on Windows also starts Thunderbird (minimized, accepting the brief window flash). The Windows lab (Parallels VM) check of start, minimize and close is a ship-gate test, not a feature gate. Wording names the taskbar. | **Overrides C-47's proposal to hold Windows back.** C-47's other parts stand: the minimized-not-active start (L-A7, now a decision plus new row MA-923) and the focus check (in MA-918). MA-918 becomes a ship-gate test before the first release with Windows attended. Every Windows text says "taskbar" (C-24 extended). |
| OQ-3 (SEC-1(a), C-10(a)) | **Yes.** Lock cancels an encrypted Run now any time before mail reading starts (from the click to the read); after reading starts, Lock says the backup finishes with its own key and Cancel stops it. | C-10(a)'s proposed default adopted; C-13's reply wording aligned. |
| OQ-4 (SEC-8, M4, C-17) | **Accepted.** The two widened encrypted leftovers: the plaintext start/finish times include the Thunderbird wait; the unlocked index stays in memory during the wait. | C-17's residuals now stated as accepted, not pending. |

### Operator calls (made by the lead)

| D | Call | Effect |
|---|---|---|
| OD-1 | **The gate is NOT reduced.** The missing lenses run now. | Lens 2 (focus / proportionality) and lens 9 (dependency and operational cost) owe their own passes before AS1, over rev 3. This review's coverage gap is therefore still open until they run; nothing in this record claims they have. |
| OD-2 | **Accepted.** New ids R38, S64, S65; the S54 and X16 wording; the encrypted design's "a Lock never stops a Run now" rule (phase 3 §3.2) amended per OQ-3; catalog rows slice by slice (C-42). | Rev 3 §3.2 gives the amendment text; it is to be written into `design-encrypted-archives-phase3.md` at AS8. |
| OD-3 | **Accepted as a stated residual.** For an encrypted archive, `mailarchive status` shows the sync setting but not the last result (C-27). | Stated in rev 3 §4.7 and the X16 amendment. |
| OD-4 | **Accepted.** A profile named in a job file must also be in `profiles.ini` for an attended run (C-18). | Stated in rev 3 §2.1. |

### Notes from folding them (the design writer's, for the lead and the owner)

- **New design text no reviewer has read.** OQ-1 and OQ-2 added design that did not exist when this
  review ran: the never-close rule and A12, the keep-open route, and Windows attended with the
  minimized-not-active start (rev 3 §2.7, §2.8; rows MA-922, MA-923). The OD-1 passes and the owed
  fold re-verification should read these, not only the rev-2 folds.
- **OQ-3's scope is read narrowly.** The answer as relayed says "an encrypted Run now"; the
  question asked about a Run now **with the sync**. Rev 3 builds the Lock window for sync jobs only;
  a Run now without the sync keeps phase 3's rule. If every encrypted Run now was meant, that is a
  wider phase-3 change and needs a second answer.
- **The Windows flash, not a focus steal.** The owner accepted the window flashing; this review's
  SAF-4 concern was Thunderbird taking the keyboard while the person types. Rev 3 keeps that as a
  check inside MA-918, and a focus steal found there goes back to the owner before release rather
  than being treated as accepted.
- **OD-1's reach.** "The missing lenses" is read as lenses 2 and 9. Whether lenses 1, 5 and 10
  (covered only as parts of other finders) and the rescue pass also run is left open for the
  operator.
- **The Windows lab has a known obstacle** (from the project's lab notes, not re-checked): Thunderbird
  is not installed in the VM, and `prlctl exec` runs as SYSTEM, which cannot drive a signed-in
  person's Thunderbird. MA-918 needs a signed-in desktop session in the VM.

## Second round, 2026-10-30 (rev 3 reviewed; folded as rev 4)

Added 2026-10-30; nothing above is changed. **Verdict: GO_WITH_CONDITIONS.** All four passes
returned GO_WITH_CONDITIONS and no finding needs a redesign. **39 conditions, C-48 to C-86**, are
folded into the design's **rev 4**, each fold marked with its C-id. **May AS1 start? Yes, once a
second reader has fold-checked C-48..C-86** (a check of each fold against its condition below; no new
lens and no owner answer is owed for AS1). AS1 is now two slices, AS1a (scheduled-run bug fixes) and
AS1b (the attended mode); AS1a is the natural first. Four **new owner questions** (OQ-5..OQ-8) gate
later slices only.

| | |
|---|---|
| Date | 2026-10-30 |
| Register | a finding: the synthesis of the second review round, by reading only |
| Design reviewed | `docs/design-presync-attended.md` **rev 3**, sha256 `6a63ef6db7ecda518adaccfb3d6c0f2d79433c1a301a370a183bb8902d47df26` (uncommitted on the branch at the time) |
| Design after folding | **rev 4**, sha256 `d18276a9d6b1794da95ec9dfb6fb531f683841f13081a4216188aba0d5c7f271` |
| Repository | branch `presync-attended`, commit `2d9c899` plus the uncommitted rev 3 |
| Process | `../assurance-kit/process/design-review.md`; this round answers OD-1 ("the gate is not reduced; the missing lenses run now") |
| Reviewers | A model-run workflow, not a named human: four passes — **scope** (lens 2, focus / proportionality), **deps** (lens 9, dependency and operational cost), **rescue** (process step 3, an adversarial read looking for the likeliest field failure), **foldcheck** (process step 5, a second reader of every C-1..C-47 fold and the rev-3 text) — then one synthesiser (this record and the rev-4 fold), who checked every finding against the code before folding it. |
| Commands (synthesiser) | reads only (no test run, nothing built): `internal/presync/quit_windows.go`, `session.go`, `quiet.go`, `run.go`, `api.go` (each whole); `internal/app/run.go:535-814`; `internal/desktop/schedule.go:640-730`; `internal/presyncjob/statedir_{darwin,windows}.go`; `internal/instance/instance.go:40-60`; `.github/workflows/release.yml:1-12`; `internal/runlog/runlog.go`; `cmd/mailarchive/noselfexec_test.go:140-165`; `internal/wizard/wizard.go:218-242`; `internal/uninstall/config.go:85-110`; `internal/state/lastrun.go:130-145`; `docs/design-thunderbird-presync.md:495-503`; the catalog row MA-570; greps of `env_darwin.go` (probes on `context.Background()`, 303-357), `lab_windows_test.go` (`labLogWindows` line 56, `TestLabHiddenLaunchAndClose` line 90), `noarg_darwin.go`, `packaging/macos/build-app.sh:44-48`; in the go1.26.0 toolchain, `src/syscall/exec_windows.go` (`HideWindow` line 293; only `SW_HIDE`, 393-395) and `src/syscall/syscall_windows.go:395` (`sharemode`); `shasum -a 256` of rev 3 and rev 4; a grep of the catalog (no MA-9xx row exists; MA-924..MA-928 are free). |

Labels as in round 1. The synthesiser's verdict per finding is **CONFIRMED** (the design as written
allows it, checked in the code where the finding cites code), **NARROWED** (confirmed, with its scope
or remedy changed, reason given), or **REFUTED** (none this round). Duplicates across passes are
merged into one condition.

### Verdicts of the passes

| Pass | Lens | Verdict | Findings |
|---|---|---|---|
| scope (SC) | 2 focus / proportionality | GO_WITH_CONDITIONS | 8 (2 HIGH, 5 MEDIUM, 1 LOW) |
| deps (DP) | 9 dependency and operational cost | GO_WITH_CONDITIONS | 14 (7 MEDIUM, 7 LOW) |
| rescue (RS) | process step 3 | GO_WITH_CONDITIONS ("rev 3 does not yet stop the most likely field failure") | 7 (2 HIGH, 4 MEDIUM, 1 LOW) |
| foldcheck (FC) | process step 5 | GO_WITH_CONDITIONS (C-1, C-2, C-4..C-6, C-8..C-10, C-12..C-22, C-24..C-30, C-33..C-38, C-40, C-42..C-45 folded correctly; OD-1..OD-4 recorded consistently) | 16 (8 MEDIUM, 8 LOW) |

### Scope (lens 2)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| SC-1 | HIGH | AS4 waited on L-A5 for no safety gain: with every `AttendedQuitAllowed` entry false, Run now already never closes Thunderbird, which is exactly OQ-1's outcome when L-A5 fails. | CONFIRMED (rev 3 §2.8: the table is false by default, so its "ship gate" bullet protected nothing). | C-48 |
| SC-2 | HIGH | One tag builds every platform, so "a release with Windows attended waits for MA-918" holds the macOS fix too; MA-918's `WM_CLOSE` and Windows L-A5 checks cannot matter while Windows never closes. | CONFIRMED (`release.yml:1-9`). Merged with DP-7 and RS-6. | C-49, OQ-7 |
| SC-3 | MEDIUM | Much of AS1/AS4 is dead under never-close: the keep-open button and route, the Cancel quit, the S6 C-7 path, the watchdog's wait for a Cancel quit, the `closing`/`cancelling` phases, the first-sync notice and its marker, the "is closed" text variants. | **NARROWED.** Moved to the new slice AS-Q: the button and route, the Cancel quit, `closing`, the close variants. Kept in AS1b/AS4, with reasons: the **S6 child poll** and the watchdog's wait for it (FC-3 shows the poll is needed under never-close to keep SAF-5 fixed); the **first-sync line and its marker** in never-close wording (the start-side change C-32 discloses happens at AS4; FC-13 moves the line before the start). | C-50 |
| SC-4 | MEDIUM | AS1 is not small and mixes live scheduled-run bug fixes with the new mode. | CONFIRMED. | C-51 |
| SC-5 | MEDIUM | `SyncNotice` ("a Thunderbird … you are using is left alone") turns false at AS4 but is fixed only at AS9; the Mac note's "click Run now … choose Allow" is false under never-close. | CONFIRMED (`api.go:100, 105`). | C-52 |
| SC-6 | MEDIUM | Most of AS2 (record writes, readers, MA-903, MA-916) has no user before AS7/AS5. | CONFIRMED (the note's line comes from the hook callback, not the record). | C-53 |
| SC-7 | MEDIUM | The lasting Run now log is in AS4 though no ruling asks for it, and it puts the SEC-5 ordering on the owner's critical path. | CONFIRMED (§1.2 lists no such ruling; only trace item T7). | C-54 |
| SC-8 | LOW | `status -json`'s new fields are in no slice and no row. | CONFIRMED; kept (a status surface X16 covers) and named rather than cut. | C-55 |

### Dependencies and operational cost (lens 9)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| DP-1 | MEDIUM | "Up to about 17 s" bounds the minimize **attempt**, not how long the window is visible; on a slow start, a password prompt or two qualifying windows the window can stay up the whole wait. | CONFIRMED (`quit_windows.go:62, 104-118, 166-178`). | C-56 |
| DP-2 | MEDIUM | `SW_SHOWMINNOACTIVE` needs a raw `windows.CreateProcess` path (Go has only `HideWindow`); there is no "hidden flag" today; MA-612's walk would not see the new launch site; MA-923's prove-fail tests nothing; Thunderbird may ignore the flag as it ignored `SW_HIDE`. | CONFIRMED (go1.26.0 `exec_windows.go:293, 393-395`; `quit_windows.go:71-83`; `noselfexec_test.go:149-163`). Merged with RS-5 and FC-7. | C-57 |
| DP-3 | MEDIUM | The backstop `SW_MINIMIZE` activates the next window, which can move the person's keyboard focus. | **NARROWED** (with RS-4). Confirmed (`quit_windows.go:35-37, 218`). Remedy changed: `SW_SHOWMINNOACTIVE` when Thunderbird is not the foreground window, `SW_MINIMIZE` only when it is (it then hands focus back, likely to where the person was typing; **inferred**, sampled by MA-918), no call when already minimized. Always using `SW_SHOWMINNOACTIVE` could leave the keyboard with a minimized Thunderbird that had taken it (**inferred**). Attended runs only. | C-58 |
| DP-4 | MEDIUM | The lab plan's obstacle is stale (MA-570 records L-W4 confirmed on 2026-10-23 with an existing harness), and the plan does not say how focus is measured. | CONFIRMED (catalog row MA-570; `lab_windows_test.go:56, 90`). | C-59 |
| DP-5 | MEDIUM | `winEnv.quit` returns nil when it posts nothing (a compose window makes the window set ambiguous), so the session waits out the grace for nothing, says "being asked to close", and a Windows L-A5 could be misread as "it asked". | CONFIRMED (`quit_windows.go:129-161`). Also affects scheduled runs today (the 2-minute wait and the `quit-timeout` text). | C-60 |
| DP-6 | MEDIUM | With Thunderbird's "minimize to tray" option, the minimized window is invisible, `quit` finds no main window, and every run leaves it running; "the taskbar" texts are then wrong. | CONFIRMED for the code (`isMainWindow` requires visible, `quit_windows.go:184-199`); the Thunderbird option is from memory, **inferred**. | C-61 |
| DP-7 | MEDIUM | Nothing lets a release ship macOS attended with Windows attended off; add `AttendedStartAllowed(goos)`. | CONFIRMED. Merged with SC-2. | C-49 |
| DP-8 | LOW | Which program macOS names in its question depends on how the Desktop was started, and an unsigned build may lose the permission on update. | CONFIRMED as unknowns (`noarg_darwin.go:6-12`; `build-app.sh:44-48`). | C-62 |
| DP-9 | LOW | On Windows the pre-sync state folder is under `%LOCALAPPDATA%`, which uninstall never touches. | CONFIRMED (`statedir_windows.go`; `instance.go:48-57`; `uninstall/config.go:85-110`). | C-63 |
| DP-10 | LOW | On Windows a held `mailarchive.log` makes rotation fail, and `RunHeadless` then reports a failed backup and emails. | CONFIRMED (go1.26.0 `syscall_windows.go:395`; `runlog.go:27-31`; `wizard.go:235-239`). Lands with the optional AS-L. | C-54 |
| DP-11 | LOW | `minimizeMain` sleeps without the session context, so a Lock early in S6 can leave the window up while the reply says "minimized". | CONFIRMED (`session.go:180-182`; `quit_windows.go:106-118`). Choice made: the loop uses `sleepCtx(sctx)`, and the texts say "may still be on screen" when `Minimized` is false. | C-64 |
| DP-12 | LOW | Launching from the Desktop: a no-breakaway job object makes every launch fail; the launch inherits the Desktop's working folder; the elevated remedy names the wrong fix. | CONFIRMED for the code (`buildLaunchCmd` sets no `Dir`); the job-object case is documented Windows behaviour, which hosts do it is **inferred**. | C-65 |
| DP-13 | LOW | Both lab rules rest on memory: nothing notices a change to the Windows start/minimize/close code, and a test cannot read commit messages. | CONFIRMED (`docs/lab/` holds only `presync-lab-mac.py`). | C-66 |
| DP-14 | LOW | L-A1..L-A5 need only a test profile and existing harnesses; tying them to the owner's profile makes the macOS release wait on the owner. | CONFIRMED (`docs/lab/presync-lab-mac.py`; `lab_darwin_test.go`, MA-573). | C-67 |

### Rescue

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| RS-1 | HIGH | Started hidden, Thunderbird can sit on a Primary Password, expired sign-in or crash window nobody sees; nothing changes; the run ends `ok-noactivity`, is tagged "synced first" and resets the streak, so the 3-day warning never fires for the likeliest lasting failure. | CONFIRMED (`quiet.go:73-84`; `session.go:200-206`, where a truncated no-change run is renamed `watch-truncated`; `api.go:138`; the older design admits the case at `design-thunderbird-presync.md:500` and deferred an escalation). **NARROWED** in one part: under never-close (every platform from AS4) an attended run does not close Thunderbird, so "hiding the window for good" applies to scheduled runs and to AS-Q. Whether to warn on no-change streaks changes what "synced" means: **owner question OQ-5**. | C-68, OQ-5 |
| RS-2 | HIGH | An attended Run now waits about 13.5 min with new mail and 20 min without; the only exit, Cancel, skips the whole backup. | CONFIRMED (`defMaxWait`, `api.go:42`; the first fetch at +628 s in the project's 2026-10-21 lab note; §3.1). The new button is a product addition: **owner question OQ-6**; the explaining texts are folded now. | C-69, OQ-6 |
| RS-3 | MEDIUM | A start-up touch anywhere under `ImapMail`/`Mail` (a `.msf`, a POP account, filters) trips the first-change gate, so quiet is declared at about 4 min, before the fetch, and the run reports "synced". | CONFIRMED as possible (the digest folds the size and mtime of every regular file under the watch roots, `quiet.go:86-141`, `run.go:88-107`); whether Thunderbird does it is lab L-A9. | C-70 |
| RS-4 | MEDIUM | The backstop `SW_MINIMIZE` can hand the keyboard to the wrong window. | Duplicate of DP-3; NARROWED there. | C-58 |
| RS-5 | MEDIUM | §2.7 names a hidden flag that does not exist and no mechanism; MA-923 cannot go red. | Duplicate of DP-2. | C-57 |
| RS-6 | MEDIUM | Per-platform vs whole-release gating is contradictory; the Windows lab blocks the macOS fix. | Duplicate of SC-2 / DP-7; the release choice is **owner question OQ-7**. | C-49, OQ-7 |
| RS-7 | LOW | A late `Phase` call from an abandoned session can overwrite the next run's note; `KeepOpen` has the same single-flag shape. | CONFIRMED (rev 3 §4.6 keeps one `syncPhase`). | C-71 |

### Fold check (second reader)

| ID | Sev. | Finding | Verdict: evidence | Condition |
|---|---|---|---|---|
| FC-1 | MEDIUM | §5.2 counts `Left` = left-open-by-rule/by-choice as synced, so a cancelled never-close Run now would reset the streak. | CONFIRMED (rev 3 §5.2's synced row against its `cancelled` row). | C-72 |
| FC-2 | MEDIUM | Keep-open then Cancel says "being asked to close"; the `closing`/`cancelling` notes lack never-close and kept-open variants; the order of `closing` and `KeepOpen()` is unstated. | CONFIRMED. Lands in AS-Q. | C-73 |
| FC-3 | MEDIUM | Under never-close an S6 Cancel is handled "as (i)", skipping C-7's poll, so SAF-5 returns. | CONFIRMED (`session.go:296-302`, the weak match). | C-74 |
| FC-4 | MEDIUM | The Lock window opens at the lease (before the sync flag is known) and closes only in the hook wrapper, so runs with no hook leave it open; a later Lock would falsely reply "cancelled". | CONFIRMED (`schedule.go:686-699`). | C-75 |
| FC-5 | MEDIUM | "The T16 check follows with no work between" is false: sweeps, manifest load, index open, key repair and the sealed catch-up run after the hook. | CONFIRMED (`app/run.go:600-716`). Remedy chosen: extend the window to read start with a second check, since OQ-3 said "before mail reading starts". | C-76 |
| FC-6 | MEDIUM | C-3's abandoned session releases the profile lock mid-read, breaking C2. | CONFIRMED (`run.go:593-599`; rev 3 §3.3 states both rules). | C-77 |
| FC-7 | MEDIUM | No hidden flag exists today; `SW_SHOWMINNOACTIVE` cannot go through `exec.Cmd`; the change would reach scheduled runs. | CONFIRMED; merged with DP-2. | C-57 |
| FC-8 | MEDIUM | "No quit is ever sent after the watchdog returns" is unproven: killing `osascript` does not take back a sent Apple Event. | CONFIRMED as a labelling error (the behaviour is **inferred**). | C-78 |
| FC-9 | LOW | The fake does not record `NeverQuit` or call `KeepOpen`; no test override for the table. | CONFIRMED. | C-79 |
| FC-10 | LOW | `begun.Presync` is the persisted, JSON-tagged type; assigning the in-memory outcome pushes `Attended` toward disk. | CONFIRMED (`run.go:571, 1033`; `lastrun.go:135-142`). | C-80 |
| FC-11 | LOW | §2.2 says keyed attended from AS8, MA-898 says AS4. | CONFIRMED. | C-81 |
| FC-12 | LOW | MA-920 has no named prove-fail. | CONFIRMED. | C-82 |
| FC-13 | LOW | The one-time notice shows only once Thunderbird is already starting. | CONFIRMED. | C-83 |
| FC-14 | LOW | `SyncNotice`'s "a backup you start yourself and watch" is untrue for the CLI until AS10. | CONFIRMED. | C-84 |
| FC-15 | LOW | OQ-3's scope is open, but no gate requires the answer before AS8. | CONFIRMED; asked as **OQ-8**. | C-85 |
| FC-16 | LOW | Editorial: (a) `left-open-by-rule` missing from §4.1's list; (b) §6's crash row says `launching`, the code saves `syncing`; (c) A3's remedy points to §4.6, which lacks it; (d) `Busy`'s reset is undefined; (e) the probes run on `context.Background()`, not the session context. | CONFIRMED each ((b) `session.go:172-174`; (e) `env_darwin.go:303-357`). | C-86 |

**Cross-pass conflict resolved.** SC-3 asked to move the S6 Cancel path (C-7) to AS-Q; FC-3 showed
the S6 child poll is still needed under never-close. Resolution: the **poll** stays in AS1b (it
adopts the child and clears the record, so the weak match cannot catch the person's own Thunderbird);
only the **quit after it** moves to AS-Q.

### Conditions (each folded into rev 4, marked with its id)

| C | From | What rev 4 must say or do |
|---|---|---|
| C-48 | SC-1 | AS4 ships with every `AttendedQuitAllowed` entry false and no L-A5 gate; the new slice AS-Q is the only commit that flips an entry, citing that platform's L-A5 record. |
| C-49 | SC-2, DP-7, RS-6 | State the one-tag coupling; add `AttendedStartAllowed(goos)`, false by default (macOS true citing the L-A1/L-A9 record, Windows citing MA-918's first-release scope); while false, Run now installs no hook and says so; start MA-918's set-up now; the release choice if the lab is late is the owner's (OQ-7). |
| C-50 | SC-3 (narrowed by FC-3) | AS1b/AS4 build the never-close path only; the button and route, the Cancel quit, `closing`, the close-variant texts and MA-919 move to AS-Q; the S6 child poll, the watchdog's wait for it, and the never-close first-sync line stay. |
| C-51 | SC-4 | Split AS1 into AS1a (scheduled-run bug fixes; can ship as a patch) and AS1b (the attended mode), each with its own prove-fail record. |
| C-52 | SC-5 | The plaintext `SyncNotice` correction, the release line and the AS4 Mac note (no Run-now instruction) land in AS4; encrypted variants in AS8; AS9 is only the Mac note's Run-now wording after L-A2 and macOS's flip; MA-915 asserts the note never names Run now while macOS's quit entry is false. |
| C-53 | SC-6 | AS2 keeps only the in-memory outcome, the options fields and the checks (MA-902); the record writes, readers, MA-903, MA-916 and MA-904's record parts merge into AS7 right after AS4, before AS8. |
| C-54 | SC-7, DP-10 | The lasting Run now log becomes the optional slice AS-L; a failed rotation is non-fatal for both callers, with a warning; the Windows behaviour is stated; MA-907 adds an injected rename failure. |
| C-55 | SC-8 | `status -json`'s fields are named in AS5 and asserted by MA-909. |
| C-56 | DP-1 | Correct the 17 s claim; retry the minimize on each S7 poll until it succeeds once; record `Minimized`; MA-918 adds slow-start cases; a long flash goes back to the owner. |
| C-57 | DP-2, RS-5, FC-7 | Correct "today's hidden flag"; run a lab spike (L-A10) first; build the `CreateProcess` start only if Thunderbird honours `SW_SHOWMINNOACTIVE`, for attended runs only, with the named mechanism, a pure builder, closed handles and MA-612's walk extended; MA-923 rewritten or dropped. |
| C-58 | DP-3, RS-4 | The attended backstop's show mode comes from a pure `minimizeMode`: none if minimized, `SW_SHOWMINNOACTIVE` if Thunderbird is not foreground, `SW_MINIMIZE` if it is; MA-918 samples the foreground window. |
| C-59 | DP-4 | Rewrite MA-918's method on the existing harness and MA-570's run: launched by the real Desktop from a browser click; a separate typist and a separate 100 ms observer; no input from the launcher; timings stated as ARM-emulated. |
| C-60 | DP-5 | `winEnv.quit` returns `errQuitNotPosted`; `preQuit` maps it at once to the new outcome `quit-no-window` (left open, no grace wait, no "asked to close"); Windows L-A5 logs the window set and whether the post happened, and may flip the entry only if the post happened with a draft open and Thunderbird asked. |
| C-61 | DP-6 | MA-918 covers the tray option and an already-running Thunderbird; §2.7 states the outcome; Windows texts say "the taskbar or the notification area" until the lab settles it; `prefs.js` is not read. |
| C-62 | DP-8 | L-A2 adds `.app` vs Terminal and survival across an update; until then no text names the asking program or promises scheduled runs need no answer after an update. |
| C-63 | DP-9 | Uninstall targets `presyncjob.StateDir()` per platform; MA-910 checks the Windows location. |
| C-64 | DP-11 | `minimizeMain` waits on `sleepCtx(sctx)`; Windows Lock, Cancel and left-open texts say "its window may still be on screen" when `Minimized` is false; MA-913, MA-921. |
| C-65 | DP-12 | A launch "access denied" maps to the new outcome `start-blocked` with its own line, never a retry without breakaway; the launch's working folder is Thunderbird's install folder; a Desktop-elevated line; MA-918 starts the Desktop from its shortcut and from a terminal. |
| C-66 | DP-13 | Each table entry names a lab record under `docs/lab/` that a test checks; a hash pin of the Windows start/minimize/close functions with the latest MA-918 record's date is the re-run tripwire, changed only with a new record. |
| C-67 | DP-14 | L-A1..L-A5 (and L-A8, L-A9) run on a test profile on the development Mac with the existing harnesses; the owner's profile is optional, never a gate. |
| C-68 | RS-1 | New outcome `watch-truncated-noactivity`; it and `ok-noactivity` get their own words ("nothing arrived: no new mail, or waiting for a password or sign-in; open Thunderbird to check"), never "synced first"; whether a no-change streak warns is OQ-5 (recommended: a separate softer warning); lab L-A8; from AS-Q an attended no-change run leaves Thunderbird open (a decision, the R31-safe direction); MA-925, MA-926. |
| C-69 | RS-2 | The waiting note explains the 13-20 minute wait; the waiting Cancel tooltip says Cancel skips the whole backup; "Stop waiting and back up now" is OQ-6. |
| C-70 | RS-3 | Lab L-A9 (several accounts, the product's own digest) gates macOS's start entry; if early touches show up, the first-change gate counts only message-store growth, through the pre-sync quiet-rule review. |
| C-71 | RS-7 | `Phase` and `KeepOpen` are bound to the Run now's slot id; no `Phase` call after abandonment; an MA-906 case. |
| C-72 | FC-1 | `Synced()` reads the outcome alone; left-open endings keep the S7 verdict; `cancelled` is never synced whatever its `Left`; MA-903, MA-908, MA-922 assert it. |
| C-73 | FC-2 | `preQuit` publishes `closing` under `c.mu` before reading `KeepOpen()`, and reads it before any quit; a Cancel after a press leaves Thunderbird open by choice and says so; no phase is shown when no quit will be sent (AS-Q). |
| C-74 | FC-3 | Under never-close an S6 Cancel still polls for the proven child up to 90 s, then re-verifies and clears the record (`left-open-by-rule`), or leaves `may-be-starting`; MA-922's S6 case changed. |
| C-75 | FC-4 | The window opens after one read of the job's sync flag (the same read decides the hook; a changed file refuses the run); it closes under `c.mu` at read start or in the Run now job function's `defer`; MA-913's three no-stale-window cases. |
| C-76 | FC-5 | Correct the claim; the window lasts until right before the source loop; `OnReadStart` closes it, then a second cancellation check takes the loop's own cancel path; the Lock reply says the key is cleared once the current step ends; MA-902 and MA-913 cases. |
| C-77 | FC-6 | After abandonment the profile lock is released by a once-both latch (the session returned and the read's `Release` called); MA-921 asserts it is held during the read. |
| C-78 | FC-8 | The "no quit after the watchdog or a Lock" claim is labelled inferred and narrowed to "no request started"; L-A2 adds the kill-then-Allow case; a residual is stated if the quit is delivered. |
| C-79 | FC-9 | The fake records the whole `Config` and calls `KeepOpen()`; one exported test-only override of both tables, never called from product code. |
| C-80 | FC-10 | The hook block converts the outcome to `state.PresyncRecord` (code, name, result, since); the full outcome stays in a local; `Attended` is dropped from `PresyncOutcome`. |
| C-81 | FC-11 | Keyed Run now is attended only from AS8; MA-898's keyed half moves to AS8. |
| C-82 | FC-12 | MA-920's prove-fail: read a missing streak file as stale → the "starts clean" case fails. |
| C-83 | FC-13 | The one-time notice is a line beside Run now on the card, shown before the first attended Run now, not during the start. |
| C-84 | FC-14 | `SyncNotice` says "a backup you start with Run now"; widened only when AS10 ships. |
| C-85 | FC-15 | §14 gate: the owner answers OQ-8 (the Lock scope) before AS8 is built. |
| C-86 | FC-16 | The five editorial fixes (a)-(e); `Busy` resets on any other scheduled outcome (MA-908); the probes keep their own 5 s limit, stated. |

### New owner questions (product rulings only)

- **OQ-5 (RS-1).** When Thunderbird starts but nothing arrives in 20 minutes (no new mail, or a
  hidden password or sign-in window), should scheduled backups warn after 3 days of only that?
  Recommended: yes, as a separate, softer warning. Until answered, such runs count as synced and
  AS5's no-change part waits.
- **OQ-6 (RS-2).** Add a "Stop waiting and back up now" button while Run now waits for Thunderbird
  (today the only way out is Cancel, which skips the whole backup)? Recommended: yes.
- **OQ-7 (SC-2, DP-7, RS-6).** If the Windows lab test is not done when the Mac part is ready, should
  the release wait, or ship with Run now's Thunderbird start on for the Mac only? Recommended: ship
  the Mac fix; Windows switches on in the next release once its lab passes.
- **OQ-8 (FC-15).** Should a Lock cancel every encrypted Run now that has not started reading mail,
  or only those that sync Thunderbird first? Recommended: every encrypted Run now (it is what the
  OQ-3 answer said). Needed before AS8.

### What this round did not do

- No test was run and nothing was built; every "proven" is by reading.
- No lab ran (L-A1..L-A10 are all pending); the Parallels VM was not inspected.
- The rev-4 folds have **not** been re-read by a second reader; that fold check is owed before AS1.
- Thunderbird's Windows tray option and the job-object breakaway behaviour were not checked against
  Thunderbird or Windows (both inferred, both now lab cases).
- Lenses 1, 5 and 10 have still had no pass of their own; round 2's scope and rescue passes covered
  parts of them. Whether more is wanted is the operator's call (OD-1's reach).

Smallest checks that would settle the most: on the dev Mac, set a Primary Password on a test profile
and click Run now (does the note say "synced"? L-A8); run the product's digest on a two-account test
profile from a hidden start and list what changes before the first fetch (L-A9); in the VM, start
Thunderbird once with `SW_SHOWMINNOACTIVE` (L-A10).

## Owner answers round 2 and fold check, 2026-10-30

Added 2026-10-30; nothing above is changed. **Result: the fold check passes — every condition C-48
to C-86 is folded into rev 4; twelve gaps or inconsistencies were found and are fixed in rev 5.
AS1a may start.** The owner's answers to OQ-5..OQ-8 are folded into the design's **rev 5**.

| | |
|---|---|
| Date | 2026-10-30 |
| Register | a record of decisions (the owner's answers, as relayed by the workflow lead) and a finding (the fold check, by reading only) |
| Design checked | `docs/design-presync-attended.md` **rev 4**, sha256 `d18276a9d6b1794da95ec9dfb6fb531f683841f13081a4216188aba0d5c7f271` as recorded above, committed at `88d582e` |
| Design after folding | **rev 5**, sha256 `3befba2350dc13c8296c2a3600273f6554000280a4d5abf9611b9799faf3403a` |
| Reader | A model-run workflow step, not a named human. It did not write rev 4's folds; it wrote rev 5. So rev 4's folds have had a second reader, and rev 5's own text has not. |
| Commands | reads only: both docs in full; `internal/presync/outcome.go:46-85` (the outcome list; no `wait-ended`); `internal/health/health.go:317-336` (`warn`, `red`, and the informational `note`); greps of `docs/scenario-catalog.md` (highest MA row MA-894, so MA-929 is free), of `internal` and `cmd` for `OnSyncEnd`/`OnEnd` (neither exists in code yet); `shasum -a 256` of rev 5; `git rev-parse HEAD`. No test run, nothing built. |

### Owner answers (round 2)

| Q | Answer | Where rev 5 folds it |
|---|---|---|
| OQ-5 (RS-1, C-68) | **Yes.** A softer, separate warning after 3 days in which scheduled syncs started Thunderbird but no mail arrived; its words name a possible hidden password or sign-in window; not a failure, not the missed-sync warning. | Slice **AS5** (§5.1, §5.2, §5.4, MA-926). New in rev 5: `Result.Verdict` carries the S7 verdict so a scheduled no-change run that then ends `owner-active` or `quit-*` still counts (rev 4 counted by outcome, which would miss those); an attended run where mail arrived resets the count, an attended no-change run never extends it (rev 4's §5.4 and MA-926 disagreed on this). Design decisions, for the friction review: a WARN line whose first sentence says the backups ran, **no alert email**, and left out while the missed-sync warning shows. |
| OQ-6 (RS-2, C-69) | **Yes.** A "Stop waiting and back up now" button while Run now waits; it ends the wait and backs up what has arrived. | With **AS4's Run now note** (§4.6: the button, `api/runnow-stopwait`, texts; §7 tooltip), the session part in **AS1b** (`Config.EndWait`, A16, new outcome `wait-ended`); new row **MA-929**. Design decisions: shown only in `waiting`; a stopped wait never closes Thunderbird, even after AS-Q; counted neither synced nor missed; record `result` `wait_ended`. |
| OQ-7 (SC-2, DP-7, RS-6, C-49) | **Ship the Mac first.** Windows' `AttendedStartAllowed` entry stays off until its lab passes; the code for both platforms is built now. | §2.7, §7 (a macOS-only release note), §12 (AS4's ship line), §14 gate 4. Also: the L-A10 spike no longer holds AS4 (today's launch is kept until it runs), and the MA-927 hash pin works before any MA-918 record exists (see fold finding 4). |
| OQ-8 (FC-15, C-85) | **Every encrypted Run now** that has not started reading mail, sync or not, is cancelled by a Lock. | §3.2 (the phase-3 amendment text rewritten for every Run now; the window opens at the lease), §9 (a no-sync Lock reply), §7 (the sync-off encrypted tooltip), §11 (S64, MA-913), AS8. C-75's early flag read and its "settings changed" refusal are **removed**: they existed only to keep a sync-only window in step with the hook, and the window no longer reads the flag. |

### Fold check of C-48..C-86 (against rev 4)

Every condition was checked against rev 4's text and the slice table. **Folded correctly:** C-48,
C-50 to C-60, C-63 to C-65, C-67, C-69 to C-71, C-74, C-76 to C-82, C-84, C-86 (29). **Folded, with a
gap fixed in rev 5 (10: C-49, C-61, C-62, C-66, C-68, C-72, C-73, C-75, C-83, C-85), plus two
editorial fixes outside the range:**

| # | Condition | Gap in rev 4 | Fix in rev 5 |
|---|---|---|---|
| 1 | C-49 | MA-906 asserted a "start not allowed" note, but §4.6 gave it no words. | §4.6 gives the line. |
| 2 | C-49, C-34 | Unclear whether a Run now on a platform whose start entry is false records `not-set-up` (a "missed" outcome with a warning). | §2.2: it records no sync outcome and is not `not-set-up`; MA-906 asserts it. |
| 3 | C-61 | Two Windows texts in §4.6 (the first-sync line, the no-change line) named only "the taskbar", not "or the notification area". | Both fixed. |
| 4 | C-66 | The hash pin assumed an MA-918 record exists; under OQ-7, AS4 lands before one. | The pin carries "no lab record" until the first record; Windows' entry can be true only with a matching record (§2.7, MA-927). |
| 5 | C-62 | The AS4 Mac note said "allow Mail Archive to control Thunderbird", which names the asking program before L-A2 says which it is. | Reworded to "the entry listed there". |
| 6 | C-68, C-72 | A10 step 4's list of kept S7 verdicts left out `watch-truncated-noactivity` (A12's list had it). | Added. |
| 7 | C-73 | §4.6 said "a phase is never reported when no quit will be sent", contradicting the never-close `cancelling` phase (C-74). | Narrowed to `closing`. |
| 8 | C-75 | Folded correctly in rev 4; **superseded in part by OQ-8**. | The opening half (one flag read, the refusal) is removed; the closing half stands (§3.2, MA-913). |
| 9 | C-83 | No row tested the first-attended-sync line and its marker. | MA-906 gains the case. |
| 10 | C-85 | Folded as a gate; now answered. | §14 gate 5 and AS8 updated. |
| 11 | C-34 (editorial) | §10 counted three new outcome names and left out `not-set-up`. | Now five, with `wait-ended`. |
| 12 | editorial | The hook's end callback was `OnEnd` in §2.2 and `OnSyncEnd` in §6 and MA-906. | One name, `OnEnd`. |

No condition was found unfolded, and no fix needed a redesign or an owner answer.

### May AS1a start?

**Yes.** AS1a (the scheduled-run bug fixes: the computed watchdog, `sctx`, the abandoned rule and
latch, `sleepCtx`, `quit(ctx)`, A9, A11, `preQuit` with the raw-pid count, `quit-no-window`, the
Windows working folder, B6; rows MA-900, MA-901, MA-921, MA-928 in part) depends on none of the
rev-5 changes. AS1a still owes its own prove-fail records and the adversarial pass after it (§14
gate 2).

### What this step did not do

- **Rev 5's own text has had no second reader:** OQ-5's `Verdict` count and softer warning, OQ-6's
  `EndWait`, A16 and route, OQ-7's pin before any record, OQ-8's widened window, and the twelve fixes.
  Each is read by the adversarial pass of the slice that builds it (AS1b, AS4, AS5, AS8).
- A16's claim that the readers handle a store Thunderbird is still writing the same way as in the
  `already-running` case is **inferred**; the readers' code was not read for it.
- Lenses 1, 5 and 10 still have had no pass of their own (OD-1's reach, the operator's call).
- No lab ran; no test ran; nothing was built.

Smallest check for the owner: read the five new texts in rev 5 (the no-change warning in §5.4, the
Stop-waiting button note and tooltip in §4.6 and §7, the macOS-only release note in §7, and the
no-sync Lock reply in §9) and say whether any reads wrong.

## Quiet-rule review of the wait fix, 2026-10-31

Added 2026-10-31; nothing above is changed. This is the pre-sync design's quiet-rule review that
design §13 L-A9 asked for before the first-change gate changed (MA-932, commit `4f090e8`).

| | |
|---|---|
| Date | 2026-10-31 |
| Register | a report of findings (by reviewer agents) and their verdicts and fixes (by the workflow step that wrote this section) |
| Change reviewed | commit `4f090e8`, "the wait's first-change gate counts only mail data" (MA-932) |
| Reviewers | **Independent reviewer agents** (model-run; three lenses: Thunderbird's file layouts, the gate's logic, its effects on the person). **Not the owner**: the owner has not read this review. |
| Verdicts and fixes | A model-run workflow step. Each finding was checked against the code before it was accepted; the Thunderbird and Go runtime source the reviewers cite (comm-esr115/esr128/esr140 compaction code, Go 1.26's monotonic clock per platform) was **not re-read** by this step; the fixes do not depend on it being exact, since each only narrows what counts as mail or widens what counts as a sleep. |
| Fix commit | `b23b486` (MA-933, MA-934, MA-935; MA-932 amended) |
| Runs | `internal/presync` whole with `-race`, `presyncjob`, `health`, `assureblock`, `internal/app` whole; both vets; on the Windows 11 lab VM, the new classifier, gate and sleep tests, and the real keep-awake request read back through `powercfg /requests`. |

| Id | Severity | Finding | Verdict | Commit |
|---|---|---|---|---|
| layout-1 | high | Thunderbird 128 compacts INBOX into "INBOX-1" (no extension, no index), which the classifier called mail, so a compaction could open the gate and end the wait "synced" before the fetch | **Fixed.** An mbox is mail only beside its `.msf`; a file with no extension no longer counts on its own (MA-933) | `b23b486` |
| layout-2 | medium | Thunderbird 115 local-folder compaction writes `nstmp` beside `nstmp.msf`; the index rule ran before the `nstmp` exclusion | **Fixed.** The compaction-temp check runs first (MA-933) | `b23b486` |
| layout-3 | low | any file in a maildir `cur`/`new`, a Finder `.DS_Store` included, counted as mail | **Fixed.** Dot files, `Thumbs.db` and `desktop.ini` there are not mail; `.eml` is not required (older stores) (MA-933) | `b23b486` |
| layout-4 | info | the owner's real profile: every file classified correctly | **Dropped**: context, no defect. Under the layout-1 fix all six of its mboxes still count (each has its `.msf`) | — |
| logic-1 | high | the starting snapshot is the first wait poll, taken after Thunderbird is up (on Windows up to ~17 s later), so early mail hides in it and only index touches follow: "nothing arrived" after 20 minutes | **Fixed** (same defect as effects-1). The session walks the watch set in S5, before the launch, and the first poll is compared with it (MA-934) | `b23b486` |
| logic-2 | medium | nested watch roots (the usual `ImapMail/<account>` input inside `ImapMail`) plus the entry cap can make a seen mailbox look new | **Fixed.** A root inside another is dropped, and a file the walk reaches twice keeps its first key (MA-933) | `b23b486` |
| logic-3 | medium | an index-only session now ends `ok-noactivity` and adds to the OQ-5 no-change count, so a healthy quiet mailbox reads like one stuck on a password | **Held, not fixed: owner ruling owed** (how index-only sessions count toward OQ-5 is a product decision on an owner ruling). See the owner question below | — |
| logic-4 | low | the suspend gap is measured on the monotonic clock (stops in sleep on macOS); on Windows maxWait includes sleep; a slow walk looks like a sleep | **Fixed.** Gap on wall time, from one walk's end to the next walk's start; maxWait counts time awake (MA-935) | `b23b486` |
| logic-5 | low | a compaction and new mail in the same 10 s gap cancel out | **Held, recorded as a known gap** in design §13 L-A9; the reviewer's "changed, unclear" outcome is a new outcome, an owner question | — |
| logic-6 | low | a compaction temp named `nstmp-1` would count as mail | **Fixed.** `nstmp` and `nstmp-N`, any case, are never mail (MA-933). Thunderbird's own use of a unique `nstmp` name was not checked here | `b23b486` |
| logic-7 | low | the gate is profile-wide: one account's prompt fetch opens it while another's mail waits for the ~10-minute check | **Held, recorded as a known gap** (predates MA-932); a per-account gate is a design question for the owner. The RSS case is fixed (effects-5) | — |
| effects-1 | high | the same as logic-1 | **Fixed** with logic-1 (MA-934) | `b23b486` |
| effects-2 | high | Windows: a computer that sleeps during the now-common 20-minute wait ends "nothing arrived" (counted) or internal-error | **Fixed in part.** The session keeps Windows from idle sleep from the launch to its end (the copy export's request, now `internal/keepawake`, which now returns only once the request is in place); maxWait counts time awake. **Not covered:** a sleep the person forces (lid closed) can still trip the session watchdog on waking (MA-935) | `b23b486` |
| effects-3 | medium | the softer "nothing arrived" warning now fires on every quiet mailbox, and following its advice (open Thunderbird, Run now) cannot clear it | **Held, not fixed: owner ruling owed.** Confirmed in `internal/app/syncstreak.go`: an attended no-change run never moves the count, and the already-running reset applies to scheduled runs only. Both the reset and the warning's words sit on owner ruling OQ-5. *Answered 2026-10-31: owner rulings 3 and 4; see "Owner answers, 2026-10-31" below (MA-926)* | — |
| effects-4 | medium | macOS: the suspend-gap rule never fires (monotonic clock) | **Fixed** with logic-4 (MA-935) | `b23b486` |
| effects-5 | medium | RSS feed folders (and Outbox sends into Sent) count as mail, opening the gate and hiding the password case | **Fixed in part.** Everything under a folder holding `feeds.json` (or `feeds.rdf`) is not mail (MA-933). Local Folders' Sent/Unsent and the per-account limit are recorded as known gaps | `b23b486` |
| effects-6 | medium | hourly schedules with the sync now keep Thunderbird open up to 20 minutes of every quiet hour, with no note | **Held, not fixed: owner ruling owed** (whether hourly with the sync stays, and any note; MA-859's test asserts no note today). *Answered 2026-10-31: owner ruling 5, "yes with note"; see "Owner answers, 2026-10-31" below (MA-936)* | — |
| effects-7 | low | the no-change text names two causes, but the gate now has others | **Held, fixed in part.** The main extra cause (mail before the first poll) is removed by MA-934; a folder with no index, or one the walk cannot read, remain. The wording ("either … or") is left for the owner with logic-3 and effects-3 | — |

### What the fixes do not cover

- A sleep inside a walk is not seen (a walk is normally well under a second).
- A sleep the person forces on Windows can still trip the session watchdog (internal-error).
- A compaction and new mail in one poll gap cancel out (logic-5).
- Any account's mail opens the gate for the whole profile (logic-7); Local Folders' Sent growing
  from an Outbox send counts as mail.
- A mailbox whose `.msf` lies just past a capped walk's stopping point is "not mail" for that poll,
  so growth in that one poll is not counted (the cap is 200,000 entries).

### Owner question (one, combining logic-3, effects-3, effects-6 and effects-7)

The 20-minute "nothing arrived" ending is now the usual result of a quiet night, not a rare one. Should
(a) an attended Run now that finds the person's Thunderbird open reset the no-change count, as a
scheduled one does; (b) the warning say it clears by itself at the next backup that brings mail; and
(c) hourly schedules with the sync stay, given each quiet hour now keeps Thunderbird open up to 20
minutes? This step recommends yes, yes, and keep hourly with a note — but each changes an owner ruling
(OQ-5, the hourly ruling), so none was made here.

### Owner answers, 2026-10-31

Added 2026-10-31; nothing above is changed. The owner (Alex DiMarco) answered five questions in chat
on 2026-10-31, relayed to the builder by the workflow; the reply, verbatim: "1 - yes. 2. test 3 -
tes 4 - yes 5 - yes with note". Questions 3, 4 and 5 are (a), (b) and (c) above; questions 1 and 2
are the two Windows questions (the code pin, and the Windows Run now switch).

| Question | Answer (owner's words) | What was done |
|---|---|---|
| 1. May the Windows code pin (MA-927) follow code changes, re-pinned and marked "no lab record", until the first Windows lab record exists, with every Windows `AttendedStartAllowed` and `AttendedQuitAllowed` entry false meanwhile? | "1 - yes" | **Answered: yes, operator-accepted gate change.** Recorded in the pin file and MA-927 with the date and the owner's words; MA-927 now also checks the Windows quit entry, and refuses "no lab record" once an MA-918 record exists. Then the as1a-4 fix (the Windows quit posts nothing once its context has ended) landed, its skip removed, re-pinned under this rule (MA-921). |
| 2. Flip the Windows Run now switch now, or test first (lab MA-918)? | "2. test" | **Answered: test first.** The Windows `AttendedStartAllowed` entry stays false; lab MA-918 comes before any flip. Nothing built this round. |
| 3 = (a). Should an attended Run now that finds the person's Thunderbird open reset the no-change count, as a scheduled one does? | "3 -  tes" (read as "yes" by the workflow that relayed it; the word as typed could also be "test", so the owner is asked to confirm) | **Answered: yes (as relayed).** Built: design §5.2 amended, MA-926 amended; an attended Run now's `already-running` resets the count; a manual run's still does not. Held finding **effects-3** answered in part (its reset half; its words half is (b)). |
| 4 = (b). Should the warning say it clears by itself at the next backup that brings mail? | "4 - yes" | **Answered: yes.** Built: the warning says "This warning clears by itself at the next backup that brings new mail." on every surface that shows it (design §5.4 amended, MA-926 amended), and the 1.5.2 release-notes line says so. The warning sends no email (unchanged), so no email was changed. With (a), held finding **effects-3** is answered. |
| 5 = (c). Should hourly schedules with the sync stay, given each quiet hour now keeps Thunderbird open up to 20 minutes? | "5 - yes with note" | **Answered: yes, with a note.** Hourly with the sync stays. Built (MA-936, MA-859 amended): where a person picks Hourly with the sync on, the Desktop's Settings reply and `mailarchive schedule` (output and `-h`) say "With Hourly and the Thunderbird sync, Thunderbird stays open for up to 20 minutes in an hour with no new mail, while the backup waits for mail."; the wizard offers no Hourly, so it has none; the 1.5.2 release notes say so. Held finding **effects-6** is answered. |

Not answered by these rulings, still held: **logic-3** (how an index-only session counts toward the
no-change count) and **effects-7** (the no-change text's "either … or" naming two causes). Ruling 3's
reset and ruling 4's sentence narrow what they cost a person with a quiet mailbox, but neither was
asked about directly.

### Review of the answers' commits, 2026-10-31

Added 2026-10-31; nothing above is changed (corrections sit here, beside the rows they correct).
Independent reviewer agents (model-run; **not the owner**) read the commits that built the answers
above (`05221db`, `555f0c7`, `9c42765`, `75f2322`, `c13eaaf`). A model-run workflow step checked
each finding against the code and fixed or held it.

| Id | Severity | Finding | Verdict | Commit |
|---|---|---|---|---|
| gate-1 | medium | the "no lab record" period ended only when the Windows **start** record (MA-918) appears, but question 1 said "the first Windows lab record"; a Windows **close** record (L-A5) arriving first left the pin free to follow code it measured | **Fixed.** MA-927 refuses "no lab record" once either Windows record exists; the pin file, MA-927 and design §2.7 now say "the first Windows lab record" and name both files. This corrects answer 1's "refuses 'no lab record' once an MA-918 record exists" above: it is now "once any Windows lab record exists" (stricter) | see the commit log |
| behaviour-1 | medium | answer 4's sentence, "clears by itself at the next backup that brings new mail", was not true: the count clears only when a sync's wait sees mail arrive, or Thunderbird is found already open; a nightly backup that starts Thunderbird with its mail already on disk archives new messages yet counts up | **Fixed: the words now match the code** (design §5.4, MA-926, release notes). macOS Desktop: "This warning clears by itself the next time a backup sees new mail arrive in Thunderbird, or a scheduled backup or Run now finds Thunderbird already open." Elsewhere: "… or a scheduled backup finds Thunderbird already open." **For the owner:** this replaces the sentence answer 4 approved; the owner is asked to confirm it reads right | see the commit log |
| behaviour-2 | medium | on Windows ruling 3 does nothing: Run now does not start Thunderbird there, so no session runs and the count never moves; effects-3 was marked answered with no Windows exception | **Fixed in the record and the words; the behaviour is unchanged** (ruling 2: test first). This corrects answer 3 and effects-3's "answered" above: **on Windows, effects-3 stays open** until the Windows Run now switch is turned on after lab MA-918. Design §5.2 and MA-926 say so; the Windows Desktop warning names only a scheduled backup finding Thunderbird open, pinned by a test | see the commit log |
| behaviour-3 | low | the hourly note said Thunderbird stays open "up to 20 minutes", but the 20 minutes is only the wait while the computer is awake; the start (up to 90 s), the quit (up to 2 minutes) and any sleep come on top | **Fixed.** The note and the 1.5.2 release-notes line say "about 20 minutes" (MA-936). This corrects answer 5's quoted note above | see the commit log |
| behaviour-4 | low | the comment on `SyncStreak.MailArrived` still described the already-open reset as scheduled-only | **Fixed** (comment only; the behaviour is proven by MA-926's tests, unchanged) | see the commit log |

Held from this review, not built: once the pin is strict (a Windows record exists), MA-927 ties only
the start record to the hash; requiring the Windows close record (L-A5) to carry the pinned hash
before the Windows quit entry may be true is a new rule for what an L-A5 record must carry, older
than these commits, and is left for the owner.
