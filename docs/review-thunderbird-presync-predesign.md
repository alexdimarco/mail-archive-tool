# Pre-code design review — Thunderbird pre-sync

**Design reviewed:** `docs/design-thunderbird-presync.md`, revision 1 (2026-10-13), git blob
`9c7d096c30b732064ed752fe152ce65de755adf8` at repository head `23f8adf` (branch
`wip/tb-presync-design`). The conditions below are folded into revision 2 of the same file.

**Procedure:** the 10-lens battery of `assurance-kit/process/design-review.md`, run on
2026-10-13 by one independent reviewer (not the design's author), read-only against the design,
the four designs it touches (`design-schedule-v2.md`, `design-fault-isolation.md`,
`design-extraction-workspace.md`, `design-local-confidentiality.md`), the catalog, the rulings log
and the code it cites. **Deviation from the procedure, stated plainly:** finder, skeptic and
rescue were three sequential passes by the same reviewer, not three agents. The skeptic pass
tried to refute each finding by quoting the design; the rescue pass tried to dissolve each major
by a modest mechanism change. A second reviewer reading the amended text before TP3 would
tighten this (see "Residual risk" at the end).

**Lenses run:** all ten. 1 purpose / threat-model fit · 2 focus / proportionality ·
3 durability · 4 usability · 5 integration / seams · 6 security / adversarial ·
7 failure domains / recoverability · 8 migration / coexistence · 9 dependency and operational
cost (including catalog allocation) · 10 honesty of claims.

**Findings: 24 found · 24 confirmed · 0 blocker · 10 major · 14 minor.** Six further candidates
were raised and **refuted** by quoting the design (table below). The rescue handled all 10 majors:
8 DISSOLVED by a named change, 2 PARTIAL (TB-04, TB-05: the mechanism is fixed on paper, but
whether the feature works at all is a fact about macOS that only a lab run can establish), 0
SURVIVES. No lens fails.

**Verdict: APPROVED WITH CONDITIONS.** Conditions **PC1..PC22** are folded into revision 2
(§15 of the design maps each to its section). Scope of the approval: **macOS v1, slices
TP0..TP5.** The Windows and Linux sections (§4.7.2, §4.7.3, TP6, TP7) were not attacked and are
not approved (PC21). Four items need the owner (Q2, Q4, Q16, Q17) and one needs the operator
(OPEN-X1); each has a safe interim that does not weaken R31 or R32. TP3 additionally carries a
**viability gate** that no amount of design can discharge: a quit path from a scheduler-started
program must be shown on a real Mac (L-M8 or L-M9, Q16).

## The real blocker (rescue pass): the feature is only as viable as one macOS permission

Everything in the design degrades safely if its unknowns turn out false: a Thunderbird that will
not start, will not hide, will not go quiet or will not quit each ends in a named warning and the
archive still runs. That is the design's strength. What the rescue pass could not dissolve is
that **one unknown decides whether the owner's headline request ("shut it down") works at all**,
and revision 1 treated it as a verification step instead of a gate:

- Quitting Thunderbird from a program is an Apple event to another application. macOS asks per
  requesting program, through a prompt on screen, and remembers the answer.
- A run the owner starts from Terminal is attributed to Terminal. The same binary started by
  launchd at 02:00 is a different requester. A grant made at the first does not, as far as
  anyone here knows, cover the second, and at 02:00 nobody is there to answer a prompt.
- Revision 1's operator walk and its answer to Q9 ("a manual run is where macOS asks
  permission") assumed the opposite. The owner would have tested from Terminal, clicked Allow,
  scheduled the job, and found Thunderbird open and hidden every morning.
- The fallback that revision 1 relied on (leave it open; the next run finds it and warns) was
  itself broken by TB-02: the design expired its claim on a leftover after 32 minutes, so from
  the second night on the failure would have been silent while a hidden Thunderbird ran
  permanently.

Resolution: PC4 (the first approval comes from a scheduler-started run, which the install walks
the owner through), PC5 (L-M8 and L-M9 are viability gates; with no quit path the scheduled form
does not ship, Q16), and PC2 (the announcement never expires).

## Findings

Severity: **major** = would produce a wrong, unsafe or silent outcome for the owner as written;
**minor** = a real defect with a local fix. "Cond." is the condition number in the list below.

| Key | Lens | Sev. | Finding | Scenario | Required change | Cond. |
|---|---|---|---|---|---|---|
| TB-01 | 6 | major | R31 and P2 say a quit is sent "only to a process identified by path and start time", but §4.7.1 sends `tell application id "org.mozilla.thunderbird" to quit`: delivery is by bundle id, not by pid. The check cannot bind the delivery. | The check passes for our pid P at t. The owner (or an update relaunch) closes P and opens another Thunderbird at t+50 ms; `osascript` quits whichever instance Launch Services resolves, possibly the owner's with a draft open. With two instances of the bundle the target is undefined. The script may also launch Thunderbird if it is not running (unverified). | State the delivery model honestly in P2/R31 (conditional, residual window named); constant script guarded by `if … is running`; re-check identity immediately before and our pid immediately after; a different Thunderbird appearing afterwards is announced, never asked again; lab L-M8c (two instances, not-running behaviour). | PC1 |
| TB-02 | 3, 7 | major | §4.6 expires the tool's claim on a leftover at `deadline` (32 min) and says that after it "the process is the owner's" and the record is cleared "silently"; the `left-running` row and the failure F10 both depend on the next run announcing it. | Night 1: macOS denies the quit; Thunderbird stays open hidden; warning shown. Night 2 (24 h later, past `deadline`): S3 clears the record silently, S4 sees Thunderbird running and records the *note* `already-running`. The failure is silent from the second night and a hidden Thunderbird runs for ever, contradicting P5. | Separate authority from announcement: the tool never quits a leftover in v1, and announces it on every run while the exact same process (pid, start, path, `-profile` argument) is open, with no expiry. | PC2 |
| TB-03 | 6 | major | A profile *derived* from an `-input` store (`FindProfileDir` ascends up to six levels to a directory holding `prefs.js`) is launched in Thunderbird. Nothing requires that Thunderbird itself knows the profile. | The owner archives `~/old-thunderbird-profile-2022` (a backup copy, an inherited profile, a mounted disk image) with `-auto` or `-input`. The sync starts Thunderbird on it: it logs in to that profile's accounts with stale credentials, sends its Outbox, runs its add-ons and writes into what the owner thought was a read-only source. | Accept a derived profile only if it is listed in Thunderbird's own `profiles.ini` (capped, untrusted-text parse, compare only); `-thunderbird-profile` is the owner's explicit consent and bypasses it; the refusal text names why. | PC3 |
| TB-04 | 1, 4 | major | The design's operator walk and Q9 say a manual run is where macOS asks the Automation permission, and that the owner "allows it". A grant is per requesting program. | The owner tests from Terminal, allows, installs the schedule. At 02:00 launchd runs the binary; the request is attributed to the scheduler-run program, not to Terminal; no one answers; `osascript` hangs to the 20 s deadline (`-1712`, not mapped) or is denied (`-1743`). Every night F10. | The install prints the exact command to start the installed job once now (`launchctl kickstart gui/<uid>/<label>`) and what to expect; the GUI offers the same; `status` stays on `presync_quit_not_permitted` until a scheduler-started run quits successfully; map `-1712` and the deadline to `quit-not-permitted` and name an unanswered prompt; lab L-M8 (a)-(e). | PC4 |
| TB-05 | 1, 9 | major | If L-M8 fails the design has no plan: it ships a nightly "start Thunderbird, cannot close it" and treats the loud warning as acceptable degradation. | The lab shows no grant covers launchd. The feature is "viable" in the design's terms (it never gates the archive) but leaves a hidden mail client permanently open: worse than no feature. | L-M8 and a second, Automation-free quit candidate (L-M9: a `-quit`-style request forwarded to the running instance) are viability gates for the scheduled form; owner decision Q16 (recommended: do not ship TP4/TP5 without a quit path). | PC5, Q16 |
| TB-06 | 6, 7 | major | Identity is path + start time within 2 s + "only candidate". It ignores the command line and any pre-launch snapshot, and it handles only a process that appears in the first 90 s and never disappears. | (a) An update is pending: Thunderbird relaunches itself; our proven pid exits (`exited-early`) and a new, unproven Thunderbird (perhaps with a visible updater window) stays open, unannounced. (b) A first start slower than 90 s appears after S6 gave up and is never found. (c) The owner opens Thunderbird 1.5 s before `launchAt`: path matches, start is inside the 2 s tolerance. | Add the `-profile <canonical>` argument (`ps -o args=`) to the identity; take a process snapshot at S4 and require the candidate's pid not be in it; successor scan when our process vanishes and a late scan at S9; anything found that way is announced, never quit. | PC6 |
| TB-07 | 4 | major | The owner-activity gate is deferred to TP8 (Q2), yet the dominant laptop case puts the owner at the keyboard: a missed 02:00 `StartCalendarInterval` job runs when the lid opens. | 07:00, the owner opens the lid; launchd runs the missed backup; Thunderbird starts hidden; the owner clicks its Dock icon and starts a reply; at S8 the tool asks it to quit. | Move the gate into v1 (TP3): do not start when the owner has been idle under 2 minutes (macOS `HIDIdleTime` via a fixed `ioreg` helper; unreadable = present), and do not quit a Thunderbird the owner has since used. Lab L-M10. Owner confirms the threshold (Q2). | PC7, Q2 |
| TB-08 | 4, 5 | major | The session holds the archive lock for up to about 25 minutes (D1a), but a manual run, `verify` and `status` are told only "held by mailarchive: pid=… verb=export". README line 1091 says a scheduled backup that fires during a verify "refuses and records nothing"; the same holds in reverse and the window grows from minutes to 25. | The owner runs `mailarchive` by hand at 02:10 and reads "export in progress": nothing says that the wait is Thunderbird or for how long. A weekly verify scheduled at 02:15 refuses every week and records nothing; `verify_stale` appears two periods later. | The running last-run record carries `presync: {state: syncing, since, expected_until}` from S1; the "in use" refusal, `status` and `verify`'s refusal quote it; README's verify-window sentence gains 30 minutes. (The extraction workspace's archive-source export takes no lock, MA-431, and is not blocked.) | PC8 |
| TB-09 | 7 (privacy), 10 | major | §6 lists network and notification side effects but not what Thunderbird does *on quit* and on fetch: empty Trash and clean up on exit, message filters (move, delete, forward), Outbox sends. | An account is set to "Empty Trash on exit". Each night the sync quits Thunderbird *before* the archive reads, so mail the owner deleted today is removed from Trash before it is ever archived: a capture gap with no ledger entry. | §6 and `-h` text say so; the install advisory (reads values only, same `parsePrefs`) warns for accounts with exit actions on; lab L-C6; owner accepts the trade (Q17). Archive-first-then-quit is rejected: it archives a Thunderbird that is still writing. | PC9, Q17 |
| TB-10 | 9 | major | Catalog ids collide: the design proposes MA-546..574; MA-546..548 are now taken by the desktop local-origin work. | The catalog-first PR lands MA-546..548 twice; the covers-map meta-test fails on a duplicate or a stolen id. | Re-allocate MA-549..MA-577 (shift of three) plus MA-578, MA-579 for the rows this review adds; internal cross-references and the slice table updated. | PC19 |
| TB-11 | 3 | minor | The state record carries `host` and treats a different host as "ignore with a note". macOS changes its hostname with the network (mDNS conflicts, DHCP-supplied names). | The hostname flips between runs; a leftover's record is discarded as "from another host" and the leftover is never found. | Drop `host`; store the boot time (`sysctl kern.boottime`): a different boot clears every record (nothing can survive a reboot), the same boot matches by pid + start + path + arguments. | PC10 |
| TB-12 | 3 | minor | Start time comes from `ps -o lstart=` (local time, no zone) and the record has a wall-clock `deadline`. | In the repeated hour of a DST change, or after an NTP step on wake, `lstart` is ambiguous by an hour and `deadline` is hours early or late: a still-open Thunderbird is judged ours or not ours wrongly. | Derive the start from `etime` (elapsed) subtracted from the injected `Now()`, store UTC unix seconds, match within ±1 s; no wall-clock deadline in the record; fake-clock tests for a DST fold, an NTP step and a suspend jump. | PC11 |
| TB-13 | 5 | minor | Three designs name the per-user state directory three ways: this design `os.UserConfigDir()/mailarchive/presync`; local-confidentiality `os.UserConfigDir()/mailarchive` and `%LOCALAPPDATA%\MailArchiveDesktop` (and its P3 owner-only DACL applies to "the tool's own state directory"); the workspace's XC1 `…/MailArchive`, `$XDG_STATE_HOME/mailarchive`. | Case differs on Linux and Windows; a Windows presync directory would miss P3's DACL unless named as a state directory. | Use the product's config directory as the root and a `presync/` subdirectory, through one shared helper; cite local-confidentiality P3; reconcile the workspace's name at the operator level. | PC12, OPEN-X1 |
| TB-14 | 10 | minor | P7 is titled "Older binaries stay harmless" but §7 shows an older CLI refuses the new flag every night: the unattended run fails and **no backup is made** until `schedule -remove`. An older GUI skips the sync silently (the new `status` is the only voice). | A rollback of the binary without removing the schedule stops all backups, loudly but completely. | Reword P7; state the consequence in §7 and the README rollback note. | PC13 |
| TB-15 | 3 | minor | The time budget does not add up: 5 s + 3 min recovery quit + 90 s + 20 min + 120 s + 10 s is 25.2 min, "about 24"; the hard deadline `max + 5 min` can expire during S8, so context cancellation would kill `osascript` before the request is sent. S8c's 30 s grace exceeds launchd's default ExitTimeOut (20 s) after SIGTERM. | A slow session reaches S8 at 24.9 min; the deadline cancels the context; no request is sent; a hidden Thunderbird is left. `launchctl stop` kills the process mid-quit. | S1..S7 under their own deadline; S8 and S9 on a separate 150 s context; cancel grace 15 s; the owner is told 25 minutes. | PC14 |
| TB-16 | 4 | minor | `presync_not_recorded` fires when the last run has no `presync` record, including "no run since install" and after a run that the archive lock refused. | The owner installs the schedule at 22:00 and runs `status`: WARN "this schedule did not report a sync". Type II friction: a false alarm on the first night. | Judge only the last *completed* run that started after `InstalledAt`; say "on, no run yet" before it. | PC15 |
| TB-17 | 6, 10 | minor | (a) The `presync` block in `.mailarchive-lastrun.json` is archive-resident and can be forged to read `ok`. (b) P3 says no outcome makes the *read* unsafe, while L-C1 (what a read of a still-writing mbox yields) is unknown. | (a) An archive writer shows a sync that never happened. (b) A `quit-timeout` night reads a Thunderbird that is appending and produces a truncated last message that does not heal. | (a) Persona row: the run record is a report, never an input; same trust as the whole last-run record (Type I). (b) P3's claim labelled conditional on L-C1 beside the claim. | PC16 |
| TB-18 | 6 | minor | The bundle id check "reads `Info.plist` as bytes, searched for the key". | A binary plist (macOS may convert) fails the search: the real Thunderbird is refused. | `/usr/bin/plutil -extract CFBundleIdentifier raw` as a fixed helper under the §4.7 rules. | PC17 |
| TB-19 | 3 | minor | One `streak` counter is described as counting "notes or warnings" while `already-running` "does not escalate", and `presync_stuck` needs "3 consecutive warning outcomes" and notes "7". A single counter cannot answer both. | A run alternating a note and a warning never reaches either threshold, or reaches the wrong one; MA-558's "3 and 7" row cannot be written. | Two counters (`warn_streak`, `note_streak`), `already-running` neither counts nor resets; rule written once. | PC18 |
| TB-20 | 5 | minor | The design cites `internal/app/run.go` lines 391 and 397, `internal/health/health.go:420-425` and a README line number. Fault-isolation and the workspace edit `Run` too. | The citations rot before TP2; a reviewer checks the wrong lines; the seam collision with `recordRun`/`ErrPartial` goes unnoticed. | Cite by function name and block; the seam row (MA-568) asserts position by structure. | PC20 |
| TB-21 | 2 | minor | Configurable quiet and maximum waits (two flags, two job-file fields, two descriptor fields, range checks, GUI inputs) and `-thunderbird-app` (an owner-supplied execution path) are in v1, though the owner asked for fixed 3 and 20 minutes and a standard install. | Surface and attack area (a planted app path, bundle-id spoofing by a same-user attacker) for no stated need. | Fix both waits; no app override; refuse a non-standard install with a message. | PC22 |
| TB-22 | 2 | minor | Four mechanisms exist to handle rare cases and carry the highest risk: a quit of a leftover found by the next run, a 5-minute wait on a shared profile lock, a second quit request at +60 s, and a note-to-warning escalation after 7 runs. | The leftover quit is the only path where the tool quits a process on the strength of a stored record; each of the others adds a state or a counter to prove. | v1: announce-only for leftovers (Q4), try-lock with `profile-busy` note, one quit request, no 7-run escalation. All four move to TP8. | PC22 |
| TB-23 | 2, 10 | minor | §4.7.2 and §4.7.3 (Windows `WM_CLOSE` to another process's windows, Toolhelp, Linux with no display and no known quit) are written as design and carry identity rules the review replaced. | A reader builds TP6 from rev 1 text; the reviewed rules (PC6, PC10, PC11) never reach it. | Mark both sections informative, deferred, not reviewed; TP6 and TP7 need their own pre-code review. | PC21 |
| TB-24 | 9 | minor | R30..R32, S53..S58 and X16 are claimed from "the last allocated R29/S52/X15" at one moment; other unbuilt designs are mid-flight. | A second design claims R30 before TP0. | Re-check against the operator's log at TP0 (PC19); the review found no collision at this head. | PC19 |

## Candidates raised and refuted (with the refuting text)

| ID | Claimed failure | Refuted by |
|---|---|---|
| RF-1 | The 25-minute lock blocks the extraction workspace's export of the archive | design-extraction-workspace MA-431: "an archive-source export takes no lock (a capture started mid-export succeeds …)" and §4.14; the export reads the previous run's data, as it would between runs. Recorded in PC8. |
| RF-2 | Two schedules sharing a profile deadlock on archive lock and profile lock | Lock order is archive lock then profile lock; the holder of the profile lock never waits for another archive lock. With the v1 try-lock there is no wait at all. |
| RF-3 | A run can be steered by a descriptor an archive writer plants | design P6 and MA-562: "an archive-resident descriptor naming a different app or profile (ignored by the run)". |
| RF-4 | A hostile `lock` symlink naming pid 1 or our pid gets an innocent process quit | design §5.3: "a lock pid is only a hint checked against executable path and start time; a quit goes by bundle id … never by a pid from the lock alone". (The bundle-id delivery is what TB-01 faults, not the lock.) |
| RF-5 | `status` shows nothing while the session runs | §4.9 and health: a `running` record with a live pid is already `run_in_progress` (note) inside one period. TB-08 concerns *what it says*, not whether it shows. |
| RF-6 | Launchd kills a hidden Thunderbird when the job exits | Not refuted and not confirmed: Launch Services starts the app outside the job's process group, which should survive; unverified. Added to lab L-M2 (survival of the agent's exit and of a SIGKILL of the agent). Not a finding. |

## Rescue pass (per major)

| Finding | Outcome | The change that dissolves it |
|---|---|---|
| TB-01 | DISSOLVED-BY | Honest restatement of P2/R31 plus guarded constant script, pre- and post-checks, announce-never-requote (PC1) |
| TB-02 | DISSOLVED-BY | Authority (never quit a leftover, v1) separated from announcement (every run, no expiry) (PC2) |
| TB-03 | DISSOLVED-BY | `profiles.ini` membership for derived profiles; explicit flag as consent (PC3) |
| TB-04 | PARTIAL | First approval from a scheduler-started run, walked in the install (PC4). Whether a grant to the scheduled program is even possible and persists is lab L-M8 |
| TB-05 | PARTIAL | Gate the scheduled form on L-M8/L-M9; owner decision Q16 (PC5). The mechanism is fixed; viability is a lab fact |
| TB-06 | DISSOLVED-BY | `-profile` argument + S4 snapshot + successor/late scans (PC6) |
| TB-07 | DISSOLVED-BY | Idle gate in v1, before start and before quit, unreadable = present (PC7) |
| TB-08 | DISSOLVED-BY | In-flight state in the running record, read by the refusal texts and `status`; README sentence (PC8) |
| TB-09 | DISSOLVED-BY | Text, install advisory, lab L-C6; owner accepts the trade (PC9, Q17) |
| TB-10 | DISSOLVED-BY | Mechanical re-allocation (PC19) |

## Owner decisions this review surfaces

Each has a recommended default and a safe interim in the design's §13. None may be answered by
weakening R31 or R32.

| # | Question | Recommended | Safe interim (if unanswered) | Blocks |
|---|---|---|---|---|
| Q16 | If the lab finds no way to close Thunderbird from a scheduler-started program, ship the scheduled form anyway? | No. Keep only the manual run, or drop the feature: a sync that opens Thunderbird nightly and cannot close it is worse than no feature | no scheduled form | TP3 (scheduled use), TP4, TP5 |
| Q2 | When should the tool leave Thunderbird alone because you are at the computer? (promoted from TP8 to TP3 by TB-07) | Idle under 2 minutes means present: do not start, and do not quit a Thunderbird you have used | the gate as written, unreadable idle = present | TP3 |
| Q4 | Quit a leftover Thunderbird found by the next run? | No in v1: announce on every run, no expiry; revisit in TP8 after the lab (the reviewer's recommendation; revision 1 recommended yes) | announce only | TP3 |
| Q17 | Accept that quitting Thunderbird runs its on-quit actions (empty Trash, clean up) before the archive reads, so mail deleted since the last run may not be archived? | Yes, with the install advisory and the text | yes, with the advisory | TP4 |
| OPEN-X1 (operator) | Which root does the per-user state directory live under: the product's config directory (local-confidentiality, job file), or the workspace's `MailArchive` / `XDG_STATE_HOME` names? | Product config directory, one shared helper; this design follows whatever lands, as a `presync/` subdirectory | `os.UserConfigDir()/mailarchive/presync/` | none (reconcile before TP6; TP1 should use the shared helper) |

## Conditions

All are folded into revision 2 unless marked. Each is a tracked change to the design, to be
re-verified by the build's catalog-first PR.

- **PC1** Restate P2/R31: on macOS the identity check gates the request, and the request is delivered by bundle id; residual window named; guarded constant script; pre- and post-check; a second Thunderbird appearing afterwards is announced, never asked again; lab L-M8c.
- **PC2** A leftover is announced on every run while the exact same process (pid, start, path, `-profile` argument) stays open; no expiry, no wall-clock deadline; never quit in v1.
- **PC3** A derived profile must be listed in Thunderbird's `profiles.ini`; `-thunderbird-profile` is the explicit consent.
- **PC4** A manual run does not test the scheduled quit permission; `schedule -install` and the GUI walk the owner through a scheduler-started first run; `-1712` and the helper deadline mean `quit-not-permitted`.
- **PC5** L-M8 and L-M9 are viability gates for the scheduled form (Q16). **OPEN: Q16.**
- **PC6** Identity includes the `-profile` argument and the S4 snapshot; successor scan on vanish; late scan at S9; found processes are announced, never quit.
- **PC7** The owner-activity gate (idle under 2 minutes) is in v1, before start and before quit. **OPEN: Q2 (threshold).**
- **PC8** The running record carries the in-flight sync state; the "in use" refusals, `status` and `verify` say what the wait is and until when; README verify-window sentence.
- **PC9** §6, the `-h` text and the GUI text name exit actions, filters and the Outbox; install advisory for exit-action accounts; lab L-C6. **OPEN: Q17.**
- **PC10** No hostname in the record; boot time instead; a different boot clears the record.
- **PC11** Start time from elapsed time, stored in UTC unix seconds, ±1 s; no wall-clock deadline; clock tests (DST fold, NTP step, suspend).
- **PC12** One state-directory convention, a subdirectory of the product's config directory through one helper. **OPEN: OPEN-X1 (operator).**
- **PC13** P7 reworded; the older-CLI consequence (no backups) in §7 and the README rollback note.
- **PC14** S1..S7 and S8..S9 on separate contexts; cancel grace 15 s; the time budget corrected; the owner is told 25 minutes.
- **PC15** `presync_not_recorded` is judged only on completed runs after `InstalledAt`.
- **PC16** The forged-run-record persona and P3's conditional label.
- **PC17** `plutil -extract` for the bundle id.
- **PC18** Two streak counters, one rule.
- **PC19** MA ids re-allocated from MA-549 (MA-549..MA-579); R30..R32, S53..S58, X16 re-checked against the operator's log at TP0.
- **PC20** Cite code by function, not line.
- **PC21** Windows and Linux text marked informative and unreviewed; own pre-code review before TP6 and TP7.
- **PC22** v1 scope cut: fixed quiet (3 min) and maximum (20 min) waits, no `-thunderbird-app`, announce-only leftovers, try-lock on the profile, one quit request, no note escalation; all return in TP8 on a stated need.

## Lenses: what each found, in one line

1. **Purpose / threat model:** the quit's delivery model over-claimed R31 (TB-01); P3's "read is safe" over-claimed pending L-C1 (TB-17).
2. **Focus / proportionality:** the smallest reviewable unit is TP1..TP2; v1 can lose two flags, an exec path, a leftover quit, a lock wait, a second request, an escalation and two unreviewed OS adapters (TB-21..TB-23).
3. **Durability:** a record that expires a claim and so silences a failure (TB-02); hostname, `lstart` and wall-clock `deadline` across years of DST and clock steps (TB-11, TB-12); budget arithmetic (TB-15); one counter for two jobs (TB-19).
4. **Usability:** the permission cannot be tested from Terminal (TB-04); the owner is present on laptop wake (TB-07); the wait is illegible to the people it blocks (TB-08); a false alarm on night one (TB-16).
5. **Integration:** the id collision (TB-10, TB-24); three state-directory conventions (TB-13); stale citations and the seam with the other two unbuilt designs (TB-20). Position and fields of `BeforeRead` are otherwise sound: producer slices precede sinks.
6. **Security:** bundle-id delivery (TB-01), a non-Thunderbird-listed profile (TB-03), identity gaps (TB-06), plist parsing (TB-18). The hostile `prefs.js`, lock and folder-name rows hold.
7. **Failure domains:** successor and late processes (TB-06), exit actions (TB-09), silent leftover (TB-02). No new single point of loss: the state record only ever speaks.
8. **Migration:** older CLI stops backups (TB-14); older GUI skips the sync and only the new `status` says so; additive fields are otherwise correct and unversioned on purpose.
9. **Dependency and cost:** the viability of one OS permission (TB-05); standing reliance on `open`, `osascript`, `ps`, `pgrep` plus (now) `plutil`, `ioreg`, `sysctl`: each failure ends in a named warning (the design's strength).
10. **Honesty:** P7, P3, R31 claims corrected; the design's labelling (proven / conditional / aspirational / lab item) is otherwise good, and the revised §12 carries the conditions beside the claims.

## Lab priorities (what decides viability), ranked

1. **L-M8** (and **L-M9**): quit from a scheduler-started program. Decides whether the scheduled form exists.
2. **L-M5**: whether file silence tracks the end of a fetch (and whether `.msf` churn with no mail makes "quiet" unreachable). If not, the wait is a timer.
3. **L-M2**: hidden start from a real LaunchAgent, locked screen, survival of the agent's exit.
4. **L-M3 / L-M4**: process name, whether the `lock` symlink exists on macOS, the `etime`/`args` strings: the identity rules stand on these.
5. **L-M10**: `HIDIdleTime` for a launchd agent.
6. **L-M7**: wake from sleep, network not yet up, `caffeinate` holding 25 minutes.
7. **L-C1, L-C6**: truncated last message heals; exit actions before the archive.
8. **L-M1, L-M6**: bundle id; a profile absent from `profiles.ini`.

## The mechanical check

`docs/design-thunderbird-presync.md` carries a BUILD STATUS naming this file; the file exists.
Nothing in the design is built; the catalog, `assureblock/`, baselines, the rulings log and
settings were not touched.

## Residual risk of this review

One reviewer ran all three passes; the lab facts above are unknowns, not findings, and several
(`HIDIdleTime`, `etime`, `plutil -extract raw`, `-1712`, `launchctl kickstart` for an agent
loaded with `launchctl load`) are the reviewer's memory of vendor behaviour and carry lab items
for that reason. The amended text should be read once more by a second reviewer before TP3
starts, with L-M8 and L-M9 in hand.
