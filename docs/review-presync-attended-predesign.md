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
