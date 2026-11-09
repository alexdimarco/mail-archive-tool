# Adversarial pass, round 4 (tiny confirm): Thunderbird completeness, slice C5's round-3 fixes

**Verdict: NOT CLEAN (0 MEDIUM, 2 LOW). The round-3 MEDIUM (CAP-R3-A) is fixed.**

The fix holds where the lead asked. After an outage, a working mail server gets each email: at the
first daily try, or at the very next backup once Send test email goes. A dead server is tried 5
times, then once a day, for each kind of email. No planted, damaged or linked `alert-delivered`
file made the tries unbounded or silent. The words match the lead's, and the Desktop labels. Two
small things are new:

- **QUIET-R4-A (LOW).** Some servers refuse only one of the emails, for example a spam filter on
  outgoing mail, or a relay that limits how many messages it takes in a few seconds. If such a
  server refuses the Thunderbird download email but takes the "every successful backup" email,
  then every backup's success email writes a new delivery id. The download email's count then
  never gets past 1. It is tried at every backup, it never arrives, and the `alert_send_failed`
  WARN never shows. Before this commit, the WARN showed after 5 tries.
- **DEFER-R4-B (LOW).** A run whose "working again" email *failed* still counts as "this run got
  an email through". In that run the download email waits, and the run gets no WARN even when
  another account's email is waiting for its daily try. One run only; the next backup shows the
  WARN again.

No product code was changed by this pass. The failing tests stay in the throwaway clone, quoted
below.

## Header

- **Reviewed:** `517cb52` on branch `tb-completeness`. The worktree's `git pull` said "Already up
  to date". Round 3: `review-thunderbird-completeness-c5-adversarial-r3.md` (clone
  `.claude/attack-c5-r3`, read only).
- **Process:** `../assurance-kit/process/adversarial-review.md`, the tiny confirm round, narrowed
  to the one commit. One reviewer, who did not write the commit, worked as finder and then as
  skeptic.
- **Clone:** `.claude/attack-c5-r4` at `517cb52`, nothing committed. Attack files:
  - new: `internal/backupalert/zz_attack_c5r4_test.go`;
  - round 3's, copied in: `internal/backupalert/zz_attack_c5r3_test.go`,
    `internal/app/zz_attack_c5r3_test.go`, plus the round-2 helper file
    `internal/app/zz_attack_c5r2_test.go`.
- **Adaptations to round 3's tests (signatures only):**
  - `MarkTBNoticesFailed(out, ns)` became `MarkTBNoticesFailed(out, ns, fmt.Sprint(step/9), clock)`.
    The random-history search now also changes the delivery id every 9 backups, and stamps each
    failure with that backup's clock, wrong clocks included.
  - `r.triesOf(s)` became `r.triesOf(s, "")`, since no mark existed at that failure.
- **Real primitives only:**
  - the real `ReportTBBehind`, `ReportSyncStale`, `Report`, `SendTest`, `deliveredID` and
    `markDelivered`;
  - the real streak record, told-file, delivery mark and last-run record, in real temporary folders;
  - the real `alert.Send`, against a loopback SMTP server on 127.0.0.1 written for this pass. It
    accepts or refuses each message by its content.

  The fault-injection sender (`SetSenderForTest`) was used only where the server's answer did not
  matter. No real email was sent. No Desktop link was opened. Nothing in the owner's home folder
  outside `repositories/` was searched or opened.
- **Commands** (Go 1.26.0, darwin/arm64, 2026-11-09), each `go -C .claude/attack-c5-r4 test -count=1 ...`:
  - `-run TestR4_ -v ./internal/backupalert/`: 2 red (QUIET-R4-A, DEFER-R4-B), 8 held.
  - `-run TestR3 -v ./internal/backupalert/ ./internal/app/`: round 3's results are below.
  - The builder's new tests, `-run "TestADeadServer|TestAWorkingServer|TestASentEmail|TestAWaitingEmail|TestTheDeliveryMark|TestADeliveryStarts|TestAnEmailThatFailedFive|TestASentTestEmailLets|TestTheClockAndSendWarnings|TestAClockOnTheFirstTime"`
    over `state`, `backupalert`, `desktop` and `health`: all ok.
  - The unfiltered `./...` run was not repeated; the builder's commit records it.

## 1. Round 3's attacks, rerun

| Attack | Result on `517cb52` | Verdict |
|---|---|---|
| `TestR3Attack_OutageThenWorkingServer{Download,Sync}Email`, unchanged (10 backups inside one second) | red: "delivered=0 ... WARN=5 of 5" | HELD as the lead's accepted point: no time passes, so no daily try is due, and no email got through. The time-stepped versions below deliver. |
| The same outage, an hour per backup, real SMTP, **both emails in one archive** in the wizard's order (`TestR4_OutageThenWorkingServer_RealSMTP`) | sync email accepted at hour 28 and download email at hour 29 (one email per run), each once; WARN at hours 4 to 27, none after | HELD |
| The same, with Send test email at hour 6 (`TestR4_OutageThenSendTestEmail_RealSMTP`) | sync email at hour 6 (the next backup), download email at hour 7; no WARN after | HELD |
| Told-file link, garbage, planted at 5 (`TestR3Attack_SyncTries*`) | PASS. A planted "5 tries" with no time is tried at once | HELD |
| `TestR3Attack_NormalHistoriesAreNeverDamaged` / `...OtherHistories...`, with failure marks under changing ids and wrong clocks | "histories with a false damage: 0" for both | HELD |
| `TestR3Note_ExactTieIsDamage` (TIE-R3) | PASS | HELD (fixed) |
| `TestR3Attack_SlowClockAtTheFirstBackup` (GAP-R3-C) | red at 5, 10 and 29 days, as before | the lead's accepted residual; unchanged |
| `TestR3Attack_GapDoesNotResend`, `...GapNeverHidesAWeeklyStreak`, `...SmallStepBack...`, `...RecordRunWarningPlants` | PASS | HELD |

## 2. The dead server: tries per day

`TestR4_DeadServerAllKinds_RealSMTP`: a server that refuses everything, 100 hourly backups. One
archive has both emails due, and "every successful backup" is ticked, so every run also tries a
success email, which fails too.

> tries per day [download, sync]: day 0: [5 5], day 1: [1 1], day 2: [1 1], day 3: [1 1], day 4: [0 0]

The WARN is there at hour 99. **HELD.** Two archives sharing one settings folder (one mark) and a
dead server: 8 tries each over 77 hours, and neither resets the other
(`TestR4_TwoArchivesOneMarkDeadServer`). **HELD.** So N archives mean N tries a day of each kind.
That is by design: the counts live in each archive.

## 3. The `alert-delivered` file

`TestR4_MarkPlantedVariants`. Each case plants the mark, runs 77 hourly backups against a dead
server, then sends a working Send test email, then runs one more backup.

| Plant | Dead-server tries | After Send test email | Verdict |
|---|---|---|---|
| garbage bytes | hours 0-4, 28, 52, 76 | a fresh 32-hex mark; the email goes at hour 77 | HELD |
| empty file | same | same | HELD |
| wrong JSON types | same | same | HELD |
| a fixed id that never changes | same: a fixed id voids nothing, and a delivery replaces it | same | HELD |
| `"id":""` | same | same | HELD |
| mode 0644 (refused as readable by others) | same | replaced by a 0600 file | HELD |
| bigger than the read limit | same | same | HELD |
| a link to `/dev/zero` | same; the link is not read | replaced by a regular file | HELD |
| a directory (the mark cannot be written) | same | the test email goes but no mark is written; the email waits for its daily try | NOTE N1 |

- **Deleted between runs** (`TestR4_MarkDeletedBetweenRuns`): one deletion gives one more run of 5
  tries (12 tries in 73 hours). Each deletion is bounded, and only the owner can delete it.
  **HELD.**
- **Another user's file:** not run (no second user here). By reading the code: `ReadSecureFile`
  fails to open it, so the id reads "". The atomic rename in the owner's own folder replaces it.
  That is the same path as garbage.
- **A failed write after a delivered email** (`TestR4_MarkWriteFailsAfterDelivery`, settings
  folder made read-only): the email is delivered and the log says the mark "could not be written
  ..., so an email about Thunderbird that failed 5 times waits a day before its next try". The
  next backup does not try it, and the WARN stays. This is note N1.
- **Can a planted file give unbounded sends?** No. The id changes only when `markDelivered` writes
  after a real delivery. Nothing in a plant makes `deliveredID` return a different value on each
  read; a FIFO is refused without blocking (`securefile.go:140-154`). **Can it make the counts
  never void, so the email goes silent?** No. It only falls back to the daily try.

## 4. More than ~1 try a day, or a working server that never gets the email

- **QUIET-R4-A (LOW): confirmed.** See below.
- **DEFER-R4-B (LOW): confirmed.** See below.
- **Clock edges** (`TestR4_ClockAheadHoldsHowLong`): a latest failure stamped 1 h, 23 h or 24 h
  ahead of a clock that was later corrected holds the next try for 25, 47 or 48 hours. Stamped
  more than 24 h ahead, it is tried at once. So the longest wait is about 2 days, once, after a
  clock that was up to a day fast. NOTE N2. It is not a finding.

## 5. The words

- `alert_send_failed` (`tbbehind.go`, `sendFailedMessage`) is, word for word, round 3's
  replacement "if CAP-R3-A is fixed so a delivery lifts the cap"
  (`review-...-r3.md:226`). **HELD.**
- `tbstreak_clock_behind` (`app/tbcompleteness.go`) is, word for word, round 3's optional
  wording (`review-...-r3.md:209`). **HELD.**
- The labels: "Backup alerts" is the card's heading (`internal/desktop/dashboard.go:2267`) and its
  link (`:1987`). "Mail server" is the row label (`:2281`). "Send test email" is the button
  (`:2287`). The WARN's "check the mail server details" points at that row. **HELD.**
- Status & health shows both sentences whole: the builder's `TestTheClockAndSendWarningsAreShown`
  is ok.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| QUIET-R4-A | integrity (the alert reaches the person) | `internal/backupalert/delivered.go:72` (any delivered email writes a new id) with `state/tbnotice.go:208` (a new id zeroes the counts); the WARN needs 5 in a row (`tbbehind.go:82`, `syncalert.go:182`) | CONFIRMED, LOW | `TestR4_ServerRefusesOnlyTheDownloadEmail_RealSMTP`: "download email tried 48 (accepted 0); all messages 96 (accepted 48); runs with the WARN 0" | (owed; lead's call) | (owed) |
| DEFER-R4-B | words / WARN | `internal/wizard/wizard.go:413-415` passes `o.RecoveryEmailed`, which `streak.go:174` sets "sent, delivered or not"; `tbbehind.go:75` says "this run got an email through: no WARN" | CONFIRMED, LOW | `TestR4_DeferredByAFailedRecoveryDropsTheWarn`: "RecoveryEmailed=true with the recovery send failing ... WARN this run: 0" | (owed; lead's call) | (owed) |
| N1 | words | the WARN's "Once an email gets through, MailArchive tries this one again at the next backup" when the mark cannot be written | NOTE | `TestR4_MarkWriteFailsAfterDelivery`; the log says so honestly | — | — |
| N2 | clock | `state/tbnotice.go:110` (`AlertTryDue`): a latest failure up to 24 h ahead holds the next try up to 48 h | NOTE | `TestR4_ClockAheadHoldsHowLong` | — | — |

### QUIET-R4-A (LOW): an email that only this server refuses is tried at every backup, and the person is never told

**Mechanism.** Any delivered email writes a new id. A count made under another id reads 0. The
WARN is added only when one email has failed 5 times in a row under one id. Suppose a server
takes the success email ("every successful backup" ticked) and refuses the download email in the
same run. The success email writes a new id each run, so the download email's count is 1 at every
run.

**Measured** (a loopback server that refuses any message containing "still downloading", 48
hourly backups):

> download email tried 48 (accepted 0); all messages 96 (accepted 48); runs with the WARN 0

**Why LOW, not MEDIUM.**
- It needs "every successful backup" ticked. That box is off by default and not recommended.
- It also needs a server that refuses one kind of message and takes another. Two real cases are an
  outgoing spam filter, and a relay that refuses the second message sent within a few seconds.
  The second case was reasoned from how such relays behave, not run here.
- The extra traffic is one refused try per backup. The person already asked for one email per
  backup.
- What is lost is the warning, not the cap on tries.
- The same holds for the sync email (same code path; not run separately).

**What changed.** At `505ab5a`, the same server got 5 tries and then a WARN at every backup (read
from that commit's diff, not run here).

**One way to fix it (the lead's call).** Keep the reset for *trying* (any delivered email) but
not for *warning*. For example, count failures of that email since its streak began, never
reset, and add the WARN at 5 of those. Another way: reset a count only on Send test email, or on
a delivery of the same kind of email.

### DEFER-R4-B (LOW): a failed "working again" email still counts as an email that got through

**Mechanism.** `Report` sets `RecoveryEmailed = true` whether or not the recovery email was
delivered (`streak.go:174`, "sent, delivered or not"). The wizard passes that as `deferred`
(`wizard.go:415`). In `ReportTBBehind`, a run with some notices waiting for their daily try and
others due reaches `if deferred { ... return false }` (`tbbehind.go:75`) before any WARN. The
comment there says "this run got an email through", which is false in this case.

**Measured:** account A at 5 failures (waiting), account B due, and the recovery email refused:

> RecoveryEmailed=true with the recovery send failing (9 sends left of 10); WARN this run: 0

**Why LOW.**
- It affects one run per failure streak.
- The next backup adds the WARN again.
- No email is lost: A still gets its daily try, and B is tried at the next backup.

**One way to fix it.** Pass "delivered" rather than "sent" for the defer, or add the WARN in the
defer branch when `spent` is not empty.

### Notes

- **N1.** If the settings folder cannot be written, a delivered email lifts no count. Then the
  WARN's last sentence ("tries this one again at the next backup") is wrong until the daily try.
  That folder also holds the Desktop settings and the sync told-file, so other things break first.
  The log line says what happened.
- **N2.** A clock up to a day fast at the latest failure, then corrected, holds the next try for
  up to 2 days, once. The builder's rule frees anything more than a day ahead at once.

## Limen footer

- **Clock:** 2026-11-09 (the session's date; the test logs agree: "When: 2026-11-09 07:42:10 EST").
- **Drew on:**
  - commit `517cb52`'s diff: `delivered.go`, `backupalert.go`, `streak.go`, `syncalert.go`,
    `tbbehind.go`, `state/tbnotice.go`, `state/tbstreak.go`, `app/tbcompleteness.go`,
    `uninstall/config.go`, read in full;
  - the builder's new tests, read in part;
  - `wizard.go:405-416` and `util/securefile.go:128-168`, read;
  - `dashboard.go:2258-2289`, read;
  - round 3's review and clone;
  - the runs above, all reached.

  Not checked:
  - Windows and Linux (only the macOS clone ran);
  - another user's mark file (no second user);
  - a real rate-limiting relay (QUIET-R4-A's second case is reasoned, not run);
  - `505ab5a`'s behaviour for QUIET-R4-A (read from the diff, not run);
  - the unfiltered `./...` (the builder's run is cited, not repeated).
- **Register:** finding (adversarial confirm).
- **Smallest check back:** run
  `go -C .claude/attack-c5-r4 test -count=1 -run "TestR4_ServerRefusesOnly|TestR4_DeferredByAFailed" -v ./internal/backupalert/`
  and read the two "CONFIRMED (LOW)" lines.
- **Counters:** Register: finding. Borrowed lines: the two WARN sentences and test output, quoted.
  Drift: none noticed. Touched: one new docs file (this one). Persona: none. Source reached:
  commit diff yes; clone runs yes; Desktop page source yes; Windows no; a real relay no.
