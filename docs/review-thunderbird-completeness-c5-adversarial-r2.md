# Adversarial pass, round 2 (confirm): Thunderbird completeness, slice C5's fixes

**Verdict: NOT CLEAN (1 MEDIUM, 3 LOW).** All five of round 1's findings are fixed: their attack
tests, rerun unchanged against `2d17423`, are green. The shared sender never counts an email as
sent when the server did not take it (9 server behaviours tried). The second guard never dropped a
new notice. No header injection came through the new subject. One new problem comes from the
PLANT-01 fix itself:

- **CLOCK-R2-A (MEDIUM, a regression of the fix).** A backup whose clock is more than a day
  behind the record's dates now makes the record "damaged". The streak starts fresh, and its
  "sent" flags are lost with it. A computer with a dead clock battery resets its clock at every
  cold start. If it resets to a date in 2024 or later and a backup runs before the time is
  corrected, every cold start sends the still-downloading email again, with a false "still going
  after 610 days". The status WARN comes back each time too. Measured: 6 emails in 6 days, where
  the code before the fix (`a385f97`) sent 1. The stopped email never comes while this lasts.
- **WARN-R2 (LOW).** The WARN's last sentence tells the person that another program is changing
  the file, in exactly the case above: by the time they read it, the clock is right again.
- **PLANT-R2 (LOW).** A planted `first_no_progress` with `no_progress_runs` 0 passes the new
  checks. It moves the stopped email about 22 hours earlier, past the 24-hour rule.
- **SPAM-R2-A (LOW, residual, not a regression).** A server that takes the message but breaks
  its reply to the end of DATA is counted as a failure, and the email is handed over again at
  every due run. No cap on retries per streak was built.

No product code was changed by this pass. The failing tests stay in the throwaway clone, quoted
below.

## Header

- **Reviewed:** `0735332` and `2d17423` on branch `tb-completeness`. The worktree's
  `git pull --rebase` said "Already up to date". Round 1: `review-thunderbird-completeness-c5-adversarial.md`
  (clone `.claude/attack-c5`, read only).
- **Process:** `../assurance-kit/process/adversarial-review.md`, the short confirm round. One
  reviewer, who did not write the commits, worked as finder and then as skeptic.
- **Clones:** `.claude/attack-c5-r2` at `2d17423` (attack files `internal/app/zz_attack_c5r2_test.go`,
  `internal/alert/zz_attack_c5r2_test.go`, `internal/alert/zz_attack_c5r2_subject_test.go`,
  `internal/backupalert/zz_attack_c5r2_test.go`). `.claude/attack-c5-r2-base` at `a385f97` (one
  baseline test, `internal/app/zz_base_c5r2_test.go`). Nothing in either clone is committed.
- **Real primitives only:** real temporary archive folders; the real streak record read and write;
  the real `foldTBStreak`, `MarkTBNoticesSent`, `ReportTBBehind`, `state.TBNoticeEmail` and
  `alert.Config.Message`; and the real `alert.Send` SMTP transport. The SMTP servers are the repo's
  loopback `scriptedSMTPServer` and one written in the test, all on 127.0.0.1. No real email was
  sent. No Desktop link was opened. The owner's home folder outside `repositories/` was not
  touched.
- **Commands and results** (Go 1.26.0, darwin/arm64, 2026-11-09). Each was run with
  `go -C <clone> test -count=1 ...`:
  - `-run TestR2 -v ./internal/app/`: the round-1 reruns are all PASS. CLOCK-R2-A and PLANT-R2 are
    red; the other new attacks are green.
  - `-run TestR2Attack -v ./internal/alert/`: the server-behaviour table is PASS (nothing wrongly
    counted sent). The subject test is red only on "LONG SUBJECT", a note below.
  - `-run TestR2Attack -v ./internal/backupalert/`: SPAM-R2-A is red; the refusal and second-guard
    tests are PASS.
  - Baseline `-run TestR2Base -v ./internal/app/` at `a385f97`: "1 emails over 6 days".
  - `-skip TestR2 ./internal/state/ ./internal/alert/ ./internal/backupalert/ ./internal/health/`:
    all ok. The builder's C5 app tests (`TestAttackC5|TestTheFoldNamesTheStreaksDueAnEmail|TestANoticeNeedsTheWrittenRecord|TestR4_0|TestARealBehindRunCarriesItsNotice`):
    ok. Wizard `TestTheScheduledRunSendsTheDownloadEmailsOncePerStreak`: ok. The unfiltered
    `./...` run was not repeated here; the builder's commit records it.
- **Count:** 22 attacks; 4 confirmed; 18 held.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| CLOCK-R2-A | clock / integrity | `internal/state/tbnotice.go:112`, `:115` ("more than a day after now" is damage, including `LastRunAt`); `internal/app/tbcompleteness.go:518-521` (damage gives a fresh streak, so the flags are lost) | CONFIRMED, MEDIUM (regression) | `TestR2Attack_ColdBootClockResetRepeatsTheEmail`: 6 still emails and 5 WARNs in 6 days; baseline `a385f97`: 1 | (owed; lead's call) | (owed) |
| WARN-R2 | words | `internal/app/tbcompleteness.go:681` (`tbStreakDamagedMessage`) | CONFIRMED, LOW | The quoted text below, shown by CLOCK-R2-A | (owed) | (owed) |
| PLANT-R2 | insider (planted file) | `internal/state/tbnotice.go:119-135` (no check that `first_no_progress` is set only together with `no_progress_runs` > 0) | CONFIRMED, LOW | `TestR2Attack_PlantedNoProgressWithoutCount`: stopped is due 2 hours into the planted window | (owed) | (owed) |
| SPAM-R2-A | integrity | `internal/alert/alert.go:445-447` (a broken reply to the end of DATA is a failure); `internal/backupalert/tbbehind.go` (no retry cap) | CONFIRMED, LOW (residual) | `TestR2Attack_CommittedButReplyBrokenResends`: 5 full hand-overs over 5 due runs, flag false | (owed, or accept) | (owed) |

### CLOCK-R2-A (MEDIUM): a clock reset at every cold start repeats the email every day

**Mechanism.** `ImplausibleAt(now)` calls any date "more than a day after now" damage, when `now`
itself is plausible (2024 or later). That covers `LastRunAt` and every entry's times. A run whose
clock is behind the record by more than a day therefore takes the damaged path
(`tbcompleteness.go:518-521`). The streak starts fresh at that wrong time, and `StillSent` and
`StoppedSent` go with the old record. At the next run, with the clock corrected, the fresh streak's
first time is the wrong date. Under MA-891's forward-jump rule the span is then huge, so the
still-downloading email is due again at once, and it says "still going after 610 days". Before the
fix, the same run took `spanAt`'s "clock went back" path. That restarts the first time but keeps
the serial and the flags, so nothing repeated.

**Real trigger.** A dead clock battery that resets to a firmware date in 2024 or later (newer
machines). A backup has to run before the operating system corrects the time, such as a missed
scheduled run that starts at boot. It happens on every cold start. It is likeliest on Windows
desktops, which are the biggest user group. I have not checked how often Windows corrects the time
before the first scheduled task. That is the smallest check to settle how often this happens.

**Repro** (`internal/app/zz_attack_c5r2_test.go`):

```go
bios := time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)
for d := 0; d < 6; d++ {
    r2Fold(t, out, c5Behind("k", 100), bios.Add(time.Duration(d)*time.Minute)) // boot run, reset clock
    for h := 1; h <= 8; h++ { /* hourly runs at the right time, each due notice marked sent */ }
}
// over 6 days: map[still:6 warn:5]
// "The account kept in the folder imap.k.example: about 100 of 1,000 messages downloaded, still going after 610 days."
// ... 611, 612, 613, 614, 615 days
// baseline a385f97, same scenario: "1 emails over 6 days"
```

**Asked: can a flapping clock suppress the 3-day email forever?** Not the still email: a clock
set back by 3 days every other day gave 9 emails and 10 WARNs in 20 days
(`TestR2Attack_FlappingClockSuppresses`). The flapping repeats the email; it does not hide it. The
**stopped** email is suppressed, though: X stuck for 6 days under the daily reset gave 0 stopped
emails, because each fresh streak resets the count of backups without progress. A reset to a date
before 2024 does none of this: 1 email in 6 days (`TestR2Attack_ImplausibleResetDoesNotRepeat`).

**Fix direction (the lead's call).** Choose one:
- A run whose own clock is more than a day behind the record's `LastRunAt` folds nothing: no
  write and no notice, as for an implausible clock, perhaps with a clock WARN.
- Damage from "ahead" dates keeps each entry's serial and sent flags, and resets only its times.

Either way, the lead's rule "a clock put back more than a day starts the streak again" has to stop
throwing away the "sent" flags.

### WARN-R2 (LOW): the WARN blames another program in the clock case

The WARN as shown (`tbstreak_unreadable`, the exact text from the CLOCK-R2-A run):

> "MailArchive did not trust its record of how long Thunderbird has been downloading
> (…/.mailarchive-tbstreak.json), because it holds counts or dates no backup of this archive could
> have written (a computer clock that was set wrong can cause the dates), so it started that count
> again at the last backup. Nothing is needed. If this shows again and the computer's clock is
> right, the file is being changed by something other than MailArchive."

It is plain: no jargon. It is not wholly true in the clock-back case, for three reasons:
1. A backup of this archive did write those dates, at the right time. The run reading them had
   the wrong clock.
2. "Nothing is needed" is false when the clock keeps going wrong. The battery or the time
   setting needs fixing.
3. In CLOCK-R2-A the WARN does show again, and when the person looks, the clock is right. The last
   sentence then leads them to "something other than MailArchive" is changing the file, which is
   the wrong cause.

**Fix direction.** When the damage is a date after this run's clock, say so: "this backup's
clock was more than a day behind the last backup's. If this keeps happening, check that the
computer's date and time are right after it starts". Keep the "something else" sentence for
counter damage only.

### PLANT-R2 (LOW): a planted first_no_progress with no count skips the 24-hour rule

`first_no_progress` is only ever written together with `no_progress_runs` ≥ 1, but no check holds
that pair together. A plant with `first_no_progress` one minute after `last_grew` (4 days ago) and
`no_progress_runs: 0` passes `ImplausibleAt`. The real hourly runs then reach 3 runs without
progress in 2 hours, and the window is measured from the planted time:
"PLANT-R2 CONFIRMED: the stopped email is due 2 hours into the planted window". Like round 1's
PLANT-01, this needs the owner's write access to the archive folder. The effect is about 22 hours
early, never a date in the email the run did not see. **Fix direction:** call it damage when
`first_no_progress` is set and `no_progress_runs` is 0. The other way round (a count with no date)
only delays the email, so it needs no check.

### SPAM-R2-A (LOW, residual): a broken reply to the end of DATA is retried at every due run

The fix counts an email as sent only once the 250 after DATA has been read. That is right: no
behaviour was wrongly counted as sent (table below). The other side of the same ambiguity stays
open. Some server behaviours take the message but break the reply to the end of DATA: a cut reply
("25"), a multi-line "250-" and a drop, a reply after the 60-second limit, or a 251. Each of those
counts as a failure. The run sets no flag, and the next due run hands the email over again. Through
the real alert layer: `TestR2Attack_CommittedButReplyBrokenResends`, "handed over in full: 5;
flag=false". By the SMTP rules (RFC 5321), the server takes responsibility only when its 250 goes
out, so a correct server would drop these messages. A repeat therefore needs a misbehaving server
or a lost reply, and it continues only while that keeps happening. The realistic repeating case is
a relay or antivirus mail proxy that always takes more than 60 seconds. The code before the fix
behaved the same; this is not a regression. Round 1 suggested a retry cap per streak; it was not
built. **Lead's call:** add a cap (for example, at most 3 hand-overs per streak's email), or record
this as accepted.

## HELD (refuted, with evidence)

| Attack | Verdict | Evidence |
|---|---|---|
| Round 1's five attacks rerun (only `TBNoticeEmail`'s new return shape adapted) | HELD | `TestR2Rerun_FALSE01`: the first run after 4 days off is due the still email, not stopped. `_FALSE02`: still, not stopped. `_MISS01`: stopped=1. `_PLANT01` (a, b, c): none confirmed. `_INJ`: no `Bcc:`, no bidi, the name cut to "…" |
| The 24-hour span (2d17423): hourly after 4 days off | HELD | The builder's `TestAttackC5_SwitchedOffFourDaysSendsStopped` passes in the clone: nothing due for 23 h or at 23h59, exactly one stopped email at 24 h. The lead's accepted residual (Thunderbird quit before the switch-off) was not re-reported |
| 554 or 451 after DATA, a drop during DATA | HELD | `TestR2Attack_EveryServerBehaviourAfterData`: each counts as not sent. `TestR2Attack_PermanentRefusalDeliversNothing`: 3 attempts, 0 accepted, flag false. That is repeated attempts, not repeated emails |
| A cut, multi-line or late reply, or 251, after DATA | HELD for "wrongly counted sent": each counts as not sent | Same table. The repeat this leaves is SPAM-R2-A |
| 500 to QUIT after a 250; 250 then 221 | HELD | Counted sent (`ErrAfterAccepted`, then nil), matching what the server did |
| A send path that skips the new rule | HELD | `sendFn(` appears only in `backupalert.send` (`backupalert.go:54`). The backup-result, recovery, sync and download emails and the test email all go through it |
| TLS close after the 221 | HELD by reading, not run | `client.Quit` returns `ErrAfterAccepted` for any error after the 250. The deferred `conn.Close` error is ignored. A loopback TLS server was not run, because a self-signed certificate fails `ServerName` checking |
| The second guard dropping a new notice | HELD | `TestR2Attack_StoppedAfterStillIsNotDropped`: still, then stopped, both due; `Sent` was false for each when due. `TestR2Attack_SecondGuard`: the same notice twice sends 1; a stopped notice after the still email was sent goes (2 emails). `Sent` needs the same `RecordID`, store, serial and kind (`tbnotice.go:71-80`). A fresh record has a new `RecordID` |
| A flapping clock suppresses the still email | HELD (it repeats instead) | `TestR2Attack_FlappingClockSuppresses`: 9 emails in 20 days. See CLOCK-R2-A for the repeat and the missing stopped email |
| A reset to a date before 2024 | HELD | 1 email in 6 days: the "ahead" check is skipped when the run's clock is implausible |
| A planted `last_x` | HELD | Clamped at 0 or above. A too-high value costs one run of "no progress", which the 3-run and 24-hour rules absorb. A too-low one delays |
| An account limited to recent mail, where new mail keeps raising X (never "stopped") | HELD, out of this email's scope | `tbcomplete/verdict.go` has a separate "limited" verdict for the sync setting (`ma1177_limited_test.go`), so such an account is not "behind" |
| Header injection through the archive name in the subject (CR/LF in the last part or an earlier part) | HELD | `TestR2Attack_SubjectArchiveName`: no `Bcc:` header. `name` is `header(ev.Archive)` before `archiveFolderName`, and the subject passes `header()` again (`alert.go:185`, `:346`) |
| A display name or address in the new words | HELD | `tbEmailAccount` uses only `CleanFolderName(n.Account)` (`tbnotice.go:201-206`). `Remedy` and `Which` are constants. The wizard's end-to-end test (asserts `qzsam` absent) passes |

## Notes (not findings)

- **A subject longer than 998 bytes** (RFC 5322's line limit) comes only from the fallback for an
  archive path whose last part is all spaces (`archiveFolderName` then returns the whole path).
  Windows does not allow such a folder name. Mac and Linux do. A 3,013-byte path gave a 3,082-byte
  subject. The older alert emails already put the whole path in the subject, so the new subject is
  shorter in every normal case.
- **Bidi controls** in the archive's own folder name reach the subject (`TestR2Attack_SubjectArchiveName`).
  The person chose that name, it sits last in the subject, and the older subjects already carry the
  whole path, so this is not new.
- **A backslash in a Mac or Linux folder name** is taken as a separator: a folder named
  `Thunderbird archive\Thunderbird stopped (OK)` shows as "Thunderbird stopped (OK)". This is
  cosmetic, and the name is the person's own.
- **The default archive folder name:** a search of `internal/wizard` and `internal/desktop` found
  no default folder name built from an account's address or display name. If one exists, the
  subject would carry it. I searched; I did not prove it absent.
- **"still going after 610 days"** also happened once before the fix (MA-891's forward-jump rule,
  round 1's "recorded, not a finding"). CLOCK-R2-A makes it repeat.

## Owed by the builder

Fix CLOCK-R2-A (after the lead's call) and WARN-R2 with it. Fix PLANT-R2. Then either add a retry
cap for SPAM-R2-A or record it as accepted. Each fix needs its prove-fail then prove-pass test, then
a confirm round on the fix commit. CLOCK-R2-A changes when the WARN shows and what it says, so the
new words also go to the email friction review.
