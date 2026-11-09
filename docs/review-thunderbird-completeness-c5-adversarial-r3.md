# Adversarial pass, round 3 (confirm) and light friction: Thunderbird completeness, slice C5's round-2 fixes

**Verdict: NOT CLEAN (1 MEDIUM, 2 LOW). Friction: SHIPPABLE_WITH_NOTES (1 wrong label).**

All 13 of round 2's attacks, rerun against `505ab5a`, hold. The clock-behind rule, the 30-day
restart and the new pair checks did what the commit says. No normal backup history was judged
damaged, except for one exact tie that a real clock cannot hit (a note, below). The told-file and
`RecordRunWarning` refused every plant tried. Three things are new:

- **CAP-R3-A (MEDIUM).** The 5-try cap can silence a mail server that works again. Five failed
  sends in one streak (on an hourly schedule, an afternoon offline, or a mailbox password that was
  changed) use up the tries. After that, the email is never tried again for that streak, even once
  the server works. The `alert_send_failed` WARN then shows after every due backup, and following
  its own advice ("use Send test email") cannot clear it. This also changes the shipped
  Thunderbird-not-synced email: before `505ab5a` it was tried at every stale backup until it went.
  Measured: 0 emails, and the WARN at 5 of 5 backups, over 5 due backups with a working server
  after a 5-backup outage. The same for the sync email.
- **GAP-R3-C (LOW, residual).** The 30-day restart covers a firmware date more than 30 days back at
  a new record's first backup. A clock 3 to 30 days slow at that first backup still sends the
  still-downloading email one real hour into the streak, saying "still going after N days", where
  N is the clock's error. This happens once, and only for a record with no dates yet.
- **FRIC-R3-1 (friction).** `alert_send_failed` sends the reader to "MailArchive Desktop's
  Settings". The mail server and the Send test email button are on the **Backup alerts** card. The
  Settings card holds only the archive location and the schedule.

No product code was changed by this pass. The failing tests stay in the throwaway clone, quoted
below.

## Header

- **Reviewed:** `505ab5a` on branch `tb-completeness`. The worktree's `git pull --rebase` said
  "Already up to date". Round 2: `review-thunderbird-completeness-c5-adversarial-r2.md` (clones
  `.claude/attack-c5-r2` and `-r2-base`, read only).
- **Process:** `../assurance-kit/process/adversarial-review.md`, the short confirm round, with a
  light friction review of the three WARN sentences. One reviewer, who did not write the commit,
  worked as finder and then as skeptic.
- **Clone:** `.claude/attack-c5-r3` at `505ab5a`, nothing committed. Round 2's four attack files
  were copied in. Attack files: `internal/app/zz_attack_c5r2_test.go`,
  `internal/app/zz_attack_c5r3_test.go`, `internal/backupalert/zz_attack_c5r2_test.go`,
  `internal/backupalert/zz_attack_c5r3_test.go`, `internal/alert/zz_attack_c5r2_test.go`,
  `internal/alert/zz_attack_c5r2_subject_test.go`.
- **Adaptations to round 2's tests, signatures only:**
  - The helper `r2Fold` was renamed `r3Fold`, because the builder's `ma1290_clock_behind_test.go`
    now defines an `r2Fold` of its own.
  - `ImplausibleAt(now)` became `Damaged() || AheadOf(now)`.
  - SPAM-R2-A now runs 8 due backups and expects at most `state.AlertSendTries` (5) hand-overs.
    This is the lead's accepted residual; round 2's test asked for at most 1, before any cap existed.
- **Real primitives only:**
  - the real `foldTBStreak`, `MarkTBNoticesSent`, `MarkTBNoticesFailed`, `RecordRunWarning`,
    `ReportTBBehind` and `ReportSyncStale`;
  - the real streak, told-file and last-run reads and writes, in real temporary folders;
  - the real `alert.Send`, against loopback SMTP servers on 127.0.0.1.

  No real email was sent. No Desktop link was opened. Nothing in the owner's home folder outside
  `repositories/` was searched or opened.
- **Commands** (Go 1.26.0, darwin/arm64, 2026-11-09), each `go -C <clone> test -count=1 ...`:
  - `-run "TestR2Rerun|TestR2Attack" -v ./internal/app/ ./internal/backupalert/`: all PASS (13).
  - `-run TestR2 -v ./internal/alert/`: the server-behaviour table PASS. The subject test is red
    only on "LONG SUBJECT", round 2's note, unchanged.
  - `-run TestR3Attack -v ./internal/backupalert/`: CAP-R3-A red twice; the told-file attacks PASS.
  - `-run "TestR3Attack_(SmallStep|Gap|SlowClock|RecordRunWarning)" -v ./internal/app/`: GAP-R3-C
    red (5, 10 and 29 days); the rest PASS.
  - `-run "TestR3Note_ExactTie|TestR3Attack_(NormalHistories|OtherHistories)" -v ./internal/app/`:
    "histories with a false damage: 0" and "other histories with a false damage: 0" (both PASS);
    the note's tie repro red as expected ("TIE-R3 CONFIRMED (exact tie only)"), its
    nanosecond-off twin clean.
  - `-skip "TestR2|TestR3" ./internal/state/ ./internal/backupalert/ ./internal/health/ ./internal/alert/`:
    all ok. The builder's `TestTheDownloadEmailIsTriedAtMostFiveTimes` and the wizard's
    `TestAFailingDownloadEmailIsGivenUpWithAWarning` and
    `TestTheScheduledRunSendsTheDownloadEmailsOncePerStreak`: ok. The unfiltered `./...` run was not
    repeated here; the builder's commit records it.
- **Count:** 27 attack tests (13 rerun, 14 new); 3 red (CAP-R3-A twice, GAP-R3-C), 24 held. That
  makes 2 adversarial findings, plus 1 friction finding (FRIC-R3-1) and 1 note (TIE-R3).

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| CAP-R3-A | integrity (availability of the alert) | `internal/backupalert/tbbehind.go:63-71` and `syncalert.go:144-147` (at the cap, never tried again in that streak); nothing lowers `StillTries`, `StoppedTries` or the told-file's `tries` except a new streak | CONFIRMED, MEDIUM | `TestR3Attack_OutageThenWorkingServerDownloadEmail`: "delivered=0, flag=false, runs with the WARN=5 of 5". `TestR3Attack_OutageThenWorkingServerSyncEmail`: "delivered=0, runs with the WARN=5 of 5" | (owed; lead's call) | (owed) |
| GAP-R3-C | clock | `internal/state/tbstreak.go:273` (the restart needs a gap of more than 30 days); a new record has no dates for `AheadOf` to compare with | CONFIRMED, LOW (residual) | `TestR3Attack_SlowClockAtTheFirstBackup`: 5, 10, 29 days slow: "still going after 5 / 10 / 29 days" one real hour in; 2 and 31 days: nothing | (owed, or accept) | (owed) |
| FRIC-R3-1 | words | `internal/backupalert/tbbehind.go:142-144` (`sendFailedMessage`); the card is `internal/desktop/dashboard.go:2266-2287` | CONFIRMED, LOW (friction) | Settings card (`dashboard.go:2125-2133`) holds "Archive location" and the schedule; "Send test email" is at `:2287`, on "Backup alerts" (`:2267`). The guide calls it Backup alerts too (`help.go:203`) | (owed) | (owed: the words test) |

### CAP-R3-A (MEDIUM): five failed sends silence the email for the rest of the streak, and the WARN cannot be cleared

**Mechanism.** Each failed send adds one to the streak's count, up to 5 (`MarkFailed`;
`syncSendFailed`). At 5, `ReportTBBehind` and `ReportSyncStale` give the email up before trying
(`sendGivenUp`). Nothing lowers the count except a new streak:
- not a later working server;
- not a delivered email of another kind;
- not "Send test email". A search of `internal/` and `cmd/` finds the counts read or written only in
  `state`, `app` and `backupalert`, never in `internal/desktop`.

So for the rest of the streak:
- every due backup adds `alert_send_failed` to the last-run record;
- the person follows the WARN's advice, the test email arrives, and the WARN is back after the next
  backup;
- the Thunderbird email itself never comes.

A stuck account's streak can last weeks.

**Real triggers.** Five failures in one streak need only five due backups:
- an hourly schedule on a laptop offline for an afternoon (the local Thunderbird backup still runs);
- a mailbox password changed before the stored one is updated;
- an Office 365 SMTP sign-in outage;
- a Gmail app password that was revoked.

The shipped sync email (MA-910) behaved differently before `505ab5a`: "not delivered: the next
stale run tries again", with no cap (`git show 505ab5a -- internal/backupalert/syncalert.go`). I
read that diff; I did not run the old code.

**Repro** (`internal/backupalert/zz_attack_c5r3_test.go`):

```go
g := newTBRig(t, alertsOnFailure, false)
g.fail = failures(5)
for i := 0; i < 5; i++ { finishedRun(t, g.out); g.report(false, nil) }
g.fail = nil // the server works again
for i := 0; i < 5; i++ { finishedRun(t, g.out); g.report(false, nil); warned += len(sendFailedWarn(t, g.out)) }
// after the server came back: delivered=0, flag=false, runs with the WARN=5 of 5
// sync email, the same: delivered=0, runs with the WARN=5 of 5
```

**Why it is not the accepted residual.** The lead accepted "up to 5 tries per email type per
streak": a bound on how many times a broken server is handed the email. This is the other side of
the same cap, and the lead's statement did not name it: a server that works again is never tried,
and the WARN's own remedy cannot clear the WARN.

**Fix direction (the lead's call).** Recommended: let a delivery lift the cap. The sender records
when it last delivered any alert email, the test email included, beside the settings file. Failed
tries from before that time no longer count, so the next due backup tries once more. The WARN then
clears itself once the email goes. Two other ways:
- count a failed try only if it is at least a few hours after the last counted one (an outage of an
  afternoon then costs one try, not five);
- keep the cap and change the WARN's words to say that this email will not be tried again until
  Thunderbird finishes downloading.

The last one leaves the email lost and is the weakest.

### GAP-R3-C (LOW, residual): a clock 3 to 30 days slow at a new record's first backup

The restart (`gap`) needs more than 30 days since the latest behind backup, and `AheadOf` needs
dates in the record to compare with. A new record has none. So a first backup whose clock reads 3
to 30 days slow starts a streak at that date. The next backup, with the right clock, spans 3 days
or more, and the still email is due at once, saying "still going after N days", where N is the
clock's error. Measured: N = 5, 10 and 29 send it one real hour in; N = 2 (under 3 days) and N = 31
(the restart) do not.

The trigger is narrow:
- a record with no dates: the archive's first Thunderbird check, or one just started fresh as
  damaged or removed;
- a first backup that runs on a clock 3 to 30 days slow. A dead clock battery's firmware date is
  usually years back, so the restart already covers it.

It sends one early email per such streak, with a wrong number of days, never a repeat. Round 2
recorded the 610-day case of the same mechanism as "recorded, not a finding". **Lead's call:**
accept it as a documented residual beside "a clock that once ran fast". A fix needs some sign that
the first backup's clock was wrong, and a 2-backup streak spanning 3 to 30 days is also what a
daily schedule gives after a computer was off for a week.

### FRIC-R3-1 (friction): the WARN names the wrong card

See the friction section below.

## HELD (refuted, with evidence)

| Attack | Verdict | Evidence |
|---|---|---|
| Round 2's 13 attacks rerun, signatures only adapted (FALSE-01, FALSE-02, MISS-01, PLANT-01 a/b/c, INJ, CLOCK-R2-A, its implausible twin, the flapping clock, PLANT-R2, the stopped-after-still guard, SPAM-R2-A at the cap, the 554 refusal, the second guard) | HELD | All PASS. CLOCK-R2-A: "over 6 days: map[stopped:1]" (round 2: 6 still emails). Flapping: "2 emails, 10 WARNs" in 20 days (one still, one stopped). PLANT-R2: "the plant is caught as damaged". SPAM-R2-A: "handed over in full: 5 over 8 due runs" |
| The 30-day restart re-sends an email already sent | HELD | `TestR3Attack_GapDoesNotResend`: still sent before a 35-day gap; after it, 10 daily backups with X stuck: "still=1 stopped=1" in all, serial and flags kept (`tbstreak.go:281-284`) |
| The restart keeps a really-behind account from its 3-day email | HELD | `TestR3Attack_GapNeverHidesAWeeklyStreak`: weekly behind backups for 8 weeks, X growing, still email due. The gap is measured from the latest behind backup, which every schedule MailArchive offers (hourly, daily, weekly) moves well inside 30 days. A computer off for more than 30 days starts its count again at its return: 3 more days, never silence |
| A record a normal backup writes, judged damaged (false reset, so emails sent again) | HELD | `TestR3Attack_NormalHistoriesAreNeverDamaged`: 150 seeded histories of 80 backups each, two accounts, times jittered to the nanosecond. The real time steps were an hour, a day, 3 days, or a gap of 31 to 40 days. The clock was right, 1 to 20 hours behind or ahead (inside the one-day grace), 3 days behind, a 2025 firmware date, or 2001. X grew, stuck, fell, or caught up. Each notice was marked sent, marked failed, or left. Result: 0 damaged records and 0 `tbstreak_unreadable`. `TestR3Attack_OtherHistoriesAreNeverDamaged` (no small steps): 0. An unjittered first run found only exact ties (note TIE-R3) |
| The pairs: each one a normal backup could write apart | HELD by reading | `BehindEvicting` (`tbstreak.go:280-300`) sets `no_progress_runs` and `first_no_progress` together, and clears both together in every other branch. `first_no_progress` is set only on a plausible run, after `last_grew` was set (the MISS-01 branch runs first). `no_progress_runs` < `runs` always: a streak's first backup counts none, and each later one adds at most 1 to each. `MarkSent` sets both flags for the stopped email. `MarkFailed` caps at 5 |
| A failing server's sends unbounded across streaks | HELD (bounded by time) | Tries reset only with a new streak, and a new streak needs 3 days before any email is due. So at most 10 tries (5 still, 5 stopped) per account per 3 days. A planted damaged record (owner's write access) or one readable by others also starts fresh, which is round 2's accepted planted-file class |
| The cap silences a working server | CONFIRMED | CAP-R3-A |
| The told-file `<key>.sync-alert` as a link to a file saying "5 tries, this streak" | HELD | `TestR3Attack_SyncTriesFileIsALink`: the link is refused (`ReadSecureFile`), the email goes (1), the target is untouched, and the atomic write replaces the link with a regular file |
| The told-file as garbage, a negative count, a huge count for another streak, wrong JSON types | HELD | `TestR3Attack_SyncTriesFileGarbage`: 1 email each; garbage then a failure writes a fresh count of 1 and retries |
| A planted "5 tries, this streak" as a plain file | HELD (as a planted "told" always could) | `TestR3Attack_SyncTriesPlantedAtCap`: no email, and the WARN is shown (1). It needs write access to the owner's own settings folder |
| `RecordRunWarning` into an archive encrypted by its key file, though the planted last-run record says plaintext | HELD | "(a) err=an encrypted archive's last-run record keeps no warnings, changed=false" |
| `RecordRunWarning` through a last-run record that is a link | HELD | "(b) ... is not a regular file (a link), target changed=false" (`ReadRegularFile`) |
| `RecordRunWarning` re-writing escape codes, changing the status, or leaving an attention notice | HELD | (c): the status is kept as ok; the stored message is cleaned by the read it goes through (`lastrun.go:378-380`); no `BACKUP-NEEDS-ATTENTION.txt` for an ok record |
| `RecordRunWarning` into a running record, or twice | HELD | The builder's `TestRecordRunWarningAddsToTheLastRun` (passes in the clone); the code refuses `RunRunning` and keeps one per code (`tbcompleteness.go:710-717`) |
| A due notice wrongly read as sent, or a new notice dropped by the second guard | HELD | `TestR2Attack_StoppedAfterStillIsNotDropped` and `TestR2Attack_SecondGuard` PASS; `Sent` and `Tries` share `entry` (record, store, serial) |

## Note (not a finding)

**TIE-R3: an exact tie.** My first random search (whole-hour times) found 2 histories judged
damaged. Both had a backup whose clock read exactly, to the nanosecond, the streak's first behind
time while it was before its latest. `spanAt` then returns the same first time
(`syncstreak.go:161-164`). `BehindEvicting` detects "the clock went back" only by the first time
changing (`tbstreak.go:281`), so it misses the step back. `first_no_progress` is then left after
the new `last_behind`, and the next backup calls the record damaged. `TestR3Note_ExactTieIsDamage`
reproduces it; the same history a nanosecond off is not damaged. A real clock does not land on a
stored time to the nanosecond, so I do not count it as a finding. If the builder touches this code
again, one line closes it: treat `now.Before(previous last_behind)` as "went back" as well.

## Friction review: the three WARN sentences

The reader is on a phone, most likely on Windows, and is not technical. I read each sentence as it
is shown in `status` and in Status & health. I checked the labels it names against the Desktop page
(`internal/desktop/dashboard.go`) and its guide (`help.go`).

1. **`tbstreak_clock_behind`**: "This backup's clock read Mar 1, 2025, earlier than a previous
   backup of this archive, so MailArchive did not count it toward how long Thunderbird has been
   downloading. Check this computer's date and time."
   It is plain and gives one clear action. The date, with its year, shows what is wrong. One small
   point: a backup has no clock; the computer does. It also shows when every account is already
   caught up (the record keeps its last backup's time), and "how long Thunderbird has been
   downloading" then reads oddly. Optional words:
   > "When this backup ran, this computer's clock said Mar 1, 2025, which is earlier than a previous backup, so MailArchive did not use this backup to judge how Thunderbird's download is going. Check this computer's date and time. If it is wrong each time the computer starts, its clock battery may need replacing."
2. **`tbstreak_unreadable`** (damaged): "MailArchive did not trust its record of how long
   Thunderbird has been downloading (PATH), because it holds counts or dates that do not fit
   together, so it started that count again at the last backup. If this shows after every backup,
   something other than MailArchive is changing that file."
   This fixes WARN-R2. Its blame is now conditional ("if this shows after every backup"), so the
   clock case is no longer sent to the wrong cause, because a clock behind now gets its own WARN.
   The full path is long on a phone, but it names the file. No change needed.
3. **`alert_send_failed`**: "MailArchive could not send its email about Thunderbird (5 tries).
   Check the email settings in MailArchive Desktop's Settings, then use Send test email."
   - **Wrong label (FRIC-R3-1).** The mail server, password and the "Send test email" button are
     on the **Backup alerts** card. The **Settings** card has the archive location and the
     schedule, and no email fields. A reader who opens Settings finds nothing to check.
   - It does not say what happens next. Under CAP-R3-A as built, the email is not tried again in
     this streak.

   Replacement, if CAP-R3-A is fixed so a delivery lifts the cap:
   > "MailArchive tried 5 times to send you an email about Thunderbird and could not. Open MailArchive Desktop, go to Backup alerts, check the mail server details, then click Send test email. Once an email gets through, MailArchive tries this one again at the next backup."

   Replacement, if the cap stays as built:
   > "MailArchive tried 5 times to send you an email about Thunderbird and could not, so it will not try that email again until Thunderbird finishes downloading. To fix your email alerts, open MailArchive Desktop, go to Backup alerts, check the mail server details, then click Send test email."

**Friction verdict: SHIPPABLE_WITH_NOTES.** Sentence 3's card name must change (Backup alerts, not
Settings). Its words also depend on the CAP-R3-A call. Sentence 1's change is optional.

## Owed by the builder

- CAP-R3-A: fix after the lead's call (recommended: a delivery lifts the cap), with
  prove-fail/prove-pass using `TestR3Attack_OutageThenWorkingServer*` as the attack. Apply the same
  to the sync email.
- FRIC-R3-1: the card name, and the words to match the CAP-R3-A call.
- GAP-R3-C: accept as a documented residual, or fix.
- Then a short confirm round on the fix commit.

## Limen footer

- **Clock:** 2026-11-09 (the session's date; the test logs agree).
- **Drew on:**
  - the commit `505ab5a` and its diff, read in full for `state`, `app`, `backupalert` and `health`;
  - `spanAt`, `ReadLastRun`, `encmark` and the Desktop page, read in part;
  - round 2's review and clones;
  - the runs above, all reached.

  Not checked:
  - real Windows (only the macOS clone ran);
  - how often a real Windows desktop's clock steps back after a dual-boot or a resume;
  - the pre-`505ab5a` sync email under an outage (read from the diff, not run);
  - the unfiltered `./...` (the builder's run is cited, not repeated).
- **Register:** finding (adversarial confirm) plus opinion (friction words).
- **Smallest check back:** run `go -C .claude/attack-c5-r3 test -count=1 -run TestR3Attack_OutageThenWorkingServer -v ./internal/backupalert/`
  and read the two "delivered=0" lines. For the friction finding, open MailArchive Desktop and look
  for the mail server fields on the Settings card.
- **Counters:** Register: finding/opinion. Borrowed lines: 0 beyond the quoted WARN sentences and
  test output. Drift: none noticed. Touched: one new docs file (this one). Persona: none. Source
  reached: commit diff yes; clone runs yes; Desktop page source yes; Windows no.
