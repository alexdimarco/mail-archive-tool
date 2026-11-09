# Adversarial pass: Thunderbird completeness, slice C5 (the "still downloading" and "stopped" emails)

**Verdict: NOT CLEAN (2 MEDIUM, 3 LOW).** No header injection, no display name or address in the
email, no email for an encrypted archive, no email for a store the run did not reach or found
unknown, and one email per run for all accounts. All of that HELD. Five findings were reproduced
by a failing test against the real code. Each finding is about either when an email goes or how
often it goes:

- **SPAM-01 (MEDIUM).** Suppose the mail server accepts the message (it replies "250" after DATA)
  and then the reply to QUIT is lost. The transport returns an error, so the streak's flag is never
  set. Every later behind run then delivers the same email again: hourly, until the account
  catches up.
- **FALSE-01 (MEDIUM).** Take a computer switched off for 3 days or more during a first download.
  Its first behind run afterwards is due the **stopped** email, because X could not grow while
  the computer was off. Design §5 says "a computer that was switched off trips nothing".
- **FALSE-02 (LOW).** X falls during a streak (a bulk delete, or a folder repaired and fetched
  again) and then grows from run to run, but stays below its highest value in the streak for 3
  days. The account gets the stopped email while it is downloading.
- **MISS-01 (LOW).** Suppose the streak's first run had a clock before `MinPlausibleTime`. Then
  `LastGrew` stays zero for the whole streak unless X grows, so an account that has really stopped
  never gets the stopped email.
- **PLANT-01 (LOW).** A planted owner-only record can carry plausible counters, so R4-01 does not
  catch it, and still hold `last_grew`, `grew_x` and `first_behind` values no run could have
  written. The read clamps none of the three. A plant can make the stopped email go at the first
  behind run, dated before the streak began, or hold it back for good. It can also make the still
  line read "for 1,035 days", and it can move `behind_since`.

No product code was changed by this pass. The failing tests stay in the throwaway clone and are
quoted below. They are not added to the branch, because they would turn the suite red.

## Header

- **Reviewed commit:** `a385f97` on branch `tb-completeness`. The worktree's `git pull --rebase`
  said "Already up to date". Scope: `git show a385f97` and design
  `docs/design-thunderbird-completeness.md` §5 (with its TC-20 and M10 folds), §7 (the email words),
  §9.3 and §13's C5 note.
- **Process:** `../assurance-kit/process/adversarial-review.md`. One reviewer, who did not write
  the commit, worked as finder and then as skeptic. Every CONFIRMED row was reproduced by a test
  against the real code. Every HELD row cites the code or test that refutes it.
- **Clone:** `.claude/attack-c5`, a git clone of the worktree at `a385f97`. Nothing in it is
  committed. Its attack files are `internal/app/zz_attack_c5_test.go` and
  `internal/backupalert/zz_attack_c5_test.go` (tests `TestAttackC5_*`).
- **Real primitives only.** The tests use real temporary archive folders, the real streak record
  read and write, the real `foldTBStreak`, `MarkTBNoticesSent`, `ReportTBBehind`, `state.TBNoticeEmail`
  and `alert.Config.Message`, and the real `alert.Send` SMTP transport. The SMTP server is a
  loopback server on 127.0.0.1, written in the test. No real email was sent. The owner's home folder
  outside `repositories/` was not touched, and no Desktop link was opened.
- **Commands and results** (Go 1.26.0, darwin/arm64, 2026-11-09):
  - `go test -count=1 -run TestAttackC5 -v ./internal/app/`: FALSE-01, FALSE-02, MISS-01 and
    PLANT-01 (all three parts) are red. The injection test is green.
  - `go test -count=1 -run TestAttackC5 -v ./internal/backupalert/`: SPAM-01 is red. The server
    accepted 5 emails over 5 due runs, and the flag was never set.
  - `go test -count=1 -skip TestAttackC5 ./internal/state/ ./internal/alert/ ./internal/backupalert/ ./internal/health/`:
    all ok.
  - `go test -count=1 -run "TestTheFoldNamesTheStreaksDueAnEmail|TestANoticeNeedsTheWrittenRecord|TestR4_0|TestARealBehindRunCarriesItsNotice|TestTheReadmeNamesTheBehindStreakRecord|TestAFullStreakRecordMakesRoomAndSaysSo" ./internal/app/`:
    ok.
  - `go test -count=1 -run TestTheScheduledRunSendsTheDownloadEmailsOncePerStreak -v ./internal/wizard/`:
    PASS.
- **Count:** 18 attacks; 5 confirmed; 13 held or refuted.

## Findings

| ID | Lens | Claim (file:line) | Verdict | Evidence | Fix commit | Tombstone test |
|---|---|---|---|---|---|---|
| SPAM-01 | integrity | `internal/alert/alert.go:402-405` returns `client.Quit()`'s error after the server accepted DATA. `internal/backupalert/tbbehind.go:55-56` then marks nothing | CONFIRMED, MEDIUM | `TestAttackC5_AcceptedThenQuitLostResendsEveryRun`: 5 accepted, flag false | (owed) | (owed) |
| FALSE-01 | integrity / clock | `internal/state/tbstreak.go:296-298` (`Stopped` spans `LastGrew` to `LastBehind` over any gap) | CONFIRMED, MEDIUM | `TestAttackC5_SwitchedOffFourDaysSendsStopped` | (owed; lead/owner rule) | (owed) |
| FALSE-02 | integrity | `internal/state/tbstreak.go:245-250` (growth means above the streak's high mark `GrewX`) | CONFIRMED, LOW | `TestAttackC5_XDropThenGrowthSendsStopped` | (owed) | (owed) |
| MISS-01 | clock | `internal/state/tbstreak.go:225-228`, `:239-244` (zero `LastGrew` is never set when the streak's first run had an implausible clock) | CONFIRMED, LOW | `TestAttackC5_ImplausibleFirstClockNeverStopped` | (owed) | (owed) |
| PLANT-01 | insider (planted file) | `internal/state/tbstreak.go:334-349` (read clamps `SeenAt` and run numbers but not `FirstBehind`, `LastBehind`, `LastGrew` or `GrewX`); `internal/state/tbnotice.go:81-91` (`Implausible` checks counters only) | CONFIRMED, LOW | `TestAttackC5_PlantedTimesShapeTheEmail` (3 parts) | (owed) | (owed) |

### SPAM-01 (MEDIUM): a lost QUIT reply makes every due run send the email again

**Mechanism.** `smtpTransport` writes the message and `w.Close()` gets the server's 250. That 250
means the server has taken the message. The code then returns `client.Quit()`. If the 221 never
arrives (the connection is dropped, an antivirus mail proxy cuts it, or the `smtpSendTimeout`
deadline fires), `Send` returns an error. `ReportTBBehind` reads that as "not delivered" and sets no
flag (`tbbehind.go:55-56`). `Due()` stays true for every later behind run of the streak
(`tbnotice.go:41-51`), and each of those runs delivers the email again.

**How bad.** The repeats have no limit: one per scheduled run until the account catches up or the
QUIT problem stops. A first Gmail download can take weeks, and the owner's real profile has
318,861 messages. On an hourly schedule that is 24 identical emails a day. A lost QUIT reply after
an accepted message should be rare with Office 365 or Gmail directly. It is less rare behind
Windows antivirus "mail shield" proxies, and Windows is the biggest user group. The same transport
shape also affects the existing Thunderbird-not-synced email (`syncalert.go`), which was already
shipped and which this slice did not change. It is recorded here because C5 adds a new path to the
same loop.

**Repro** (`internal/backupalert/zz_attack_c5_test.go`). The server replies 250 to DATA, then
drops the connection on QUIT:

```go
host, port, accepted := quitDropSMTP(t) // 250 after DATA, then drops on QUIT with no 221
cfg := fmt.Sprintf(`{"alertEnabled":true,"alertOnFailure":true,"alertProvider":"smtp","alertHost":%q,"alertPort":%d,"alertSecurity":%q,"alertFrom":"b@example.org","alertTo":"t@example.org"}`, host, port, alert.SecurityNone)
g := newTBRig(t, cfg, false)
restore := SetSenderForTest(func(c alert.Config, pw string, ev alert.Event) error { return alert.Send(c, pw, ev) })
defer restore()
for i := 0; i < 5; i++ { g.report(false, nil) }
// got: accepted by the server: 5; still_downloading_sent=false
```

**Fix direction (for the builder).** Once `w.Close()` has succeeded, count the message as delivered.
Then log a failed QUIT but do not return it. This changes the shared transport, so it fixes the
sync email too. A second guard could also be added: a cap on how many times one streak's email is
retried.

### FALSE-01 (MEDIUM): a computer switched off for 3 days gets "Thunderbird has stopped downloading"

**Mechanism.** `Stopped()` is `spanOf(Runs, LastGrew, LastBehind) >= 3 days`. It needs only 2 behind
runs, with no rule about the gap between them. Picture a first download in progress: a behind run on
Friday, then the computer is off until Tuesday. The first scheduled run after it starts finds X
unchanged, because Thunderbird has not had time to fetch more, so the stopped email is due at once:

```
"Thunderbird has not downloaded any more of the Thunderbird account kept in the folder imap.k.example
 since Nov 1 (about 100 of 1,000 messages are on that computer)."
subject: "MailArchive: Thunderbird has stopped downloading mail — <archive>"
```

**Repro** (`internal/app/zz_attack_c5_test.go`):

```go
aFold(t, out, c5Behind("k", 100), aT0)
n := aFold(t, out, c5Behind("k", 100), aT0.Add(4*aDay)) // first run after 4 days off
// n = [{Stopped:true ... LastGrew:2026-11-01 09:00 LastBehind:2026-11-05 09:00}]
```

**Skeptic's check.** Round 4 of the C2 pass recorded the *still-downloading* threshold over a gap
as MA-891's rule (`review-thunderbird-completeness-c2-adversarial-r4.md:233-236`). That ruling
covered "still downloading", which stays true over a gap. The *stopped* email makes a different
claim: it says Thunderbird stopped. That claim is false when the computer was off, and design §5
says outright that "a computer that was switched off trips nothing". So this is a real conflict
between the code and the design, not a repeat of the earlier ruling. The wizard's own end-to-end
test makes the same shape (`tbBackdate(false)`: two runs 4 days apart, X equal, and the stopped
email goes), so the scheduled path sends it.

**Fix direction (lead's or owner's call).** One option: require at least N behind runs (for example
3) after the last growth before "stopped" is due. Another: also require a behind run within the
last day before the latest one, so one run after a long gap is never enough. Either way, the
design's sentence and the code must agree.

### FALSE-02 (LOW): X falls and then grows, but the email says it stopped

**Mechanism.** Growth is `x > a.GrewX`, the highest X seen in the streak (`tbstreak.go:245`). When X
falls (the person deletes a batch of mail during the first download, or Thunderbird's "Repair
Folder" fetches a folder again), later growth counts only once X passes the old highest value. If
that takes 3 days, the stopped email goes while Thunderbird is plainly downloading:

```go
aFold(t, out, c5Behind("k", 1000), aT0)
aFold(t, out, c5Behind("k", 200), aT0.Add(1*aDay))
aFold(t, out, c5Behind("k", 500), aT0.Add(2*aDay))
n := aFold(t, out, c5Behind("k", 800), aT0.Add(3*aDay))
// due STOPPED: "Thunderbird has not downloaded any more of ... since Nov 1 (about 800 of 1,000 ...)"
```

**Fix direction.** Count growth against the previous behind run's X (keep `last_x`). Or, when X
falls, set `GrewX` down to the new X (and `LastGrew` to now).

### MISS-01 (LOW): the stopped email never comes when the streak began with a wrong clock

**Mechanism.** A streak started at a clock before `MinPlausibleTime` gets zero for `FirstBehind`,
`LastBehind` and `LastGrew` (`tbstreak.go:226-228`). At the next plausible run, `spanAt` sets
`FirstBehind`. The "clock went back" reset of `LastGrew` is skipped, because `restarted` was zero
(`tbstreak.go:241`). From then on `LastGrew` stays zero unless X grows, so `spanOf` returns 0 and
`Stopped()` is never true:

```go
aFold(t, out, c5Behind("k", 100), time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))
for d := 0; d <= 10; d++ { /* X stuck at 100, daily behind runs, each notice marked */ }
// still=1 stopped=0; LastGrew 0001-01-01
```

**How likely.** It needs a dead clock battery or a fresh install whose clock has not been set yet,
at the streak's first run. The still-downloading email still goes. **Fix direction.** When
`FirstBehind` is set from zero, set `LastGrew` (and `GrewX`) from the same run.

### PLANT-01 (LOW): planted times reach the email and behind_since

The record is owner-only (`ReadSecureFile`), so a plant needs the owner's write access to the
archive folder. That is the same kind of attacker as R4-01, which the lead chose to handle by
calling such a record damaged. `Implausible()` checks only counters. The read clamps `SeenAt`,
`SeenRun` and `BehindRun`, but not `FirstBehind`, `LastBehind`, `LastGrew` or `GrewX`.
`spanAt` repairs `FirstBehind` only when it is after now or before `MinPlausibleTime`. Three parts,
each red:

- (a) `last_grew` 2024-01-02 and `grew_x` 2^40, with a streak 1 hour old. The first real behind run
  is due the stopped email: "…since Jan 1, 2024…", a date before the streak began.
- (b) `first_behind` = `MinPlausibleTime`, `runs` 1. The first real behind run is due the still
  email: "…for 1,035 days…".
- (c) `last_grew` in 2099 and `grew_x` 2^40. X stays stuck for 10 daily behind runs, and the stopped
  email never comes for that streak.

`behind_since` (`internal/health/tbcompleteness.go:100-109`) is guarded only by `MinPlausibleTime`
and the last run's end, so (b) also sets `behind_since` to 2024-01-01.

**Fix direction.** Extend `Implausible()` (R4-01's route: fresh streak plus a `tbstreak_unreadable`
WARN) to cover these cases: an entry whose `FirstBehind` is after `LastBehind`; `LastBehind` after
the record's `LastRunAt`; `LastGrew` outside [`FirstBehind`, `LastBehind`] when both are set; and
`GrewX` below `FirstX`. A plant that keeps all of these consistent can then move the email by at
most what a real run history could show.

## HELD (refuted, with evidence)

| Attack | Verdict | Evidence |
|---|---|---|
| Header injection through the store folder name, archive path or computer name (CR/LF) | HELD | `TestAttackC5_FolderNameInTheEmail`: a folder name `"imap.‮…‬\r\nBcc: evil@example.org\r\n\r\nX"+500×"A"` passed through the real fold, `TBNoticeEmail` and `Config.Message`. It produced no `Bcc:` line in the headers or the body, no U+202E or U+202C, and a name cut to `TBNameMax` with "…". The fold cleans with `CleanFolderName` (`app/tbcompleteness.go:557`). `header()` strips CR/LF from the subject, the archive, the host and every line (`alert.go:313`, `:270-275`) |
| Display name or address in the email (MA-1288) | HELD | `tbNoticeName` builds a `TBAccount` with only `Account` (the store folder) set (`tbnotice.go:136`), so `tbSayName` has no name to give. The wizard's end-to-end test asserts that `qzsam` (the account's `name` pref) is absent from the line and the remedy, and that test passes |
| Mail folder names in the email (§9.3) | HELD | `TBNotice` carries the store folder and counts only (`tbnotice.go:21-35`). No folder list reaches `TBNoticeEmail` |
| Encrypted archive | HELD | The fold returns no notices and removes a stray record when it is keyed or encrypted (`app/tbcompleteness.go:501-504`). `ReportTBBehind` refuses an encrypted `out` even when handed a notice (`tbbehind.go:45`). `MarkTBNoticesSent` refuses too. `TestAnEncryptedArchiveGetsNoDownloadEmail` passes |
| Alerts off, or on without the failure option | HELD | `ShouldSend` is false, nothing is sent, and the flag is set at once (`tbbehind.go:54-62`). This matches the sync email's documented behaviour. The builder's test passes |
| An unreached or `unknown` store named | HELD | `tbNotices` takes only `Verdict == Behind` with `BehindRun == s.Run` (`app/tbcompleteness.go:598-607`). The builder's test covers both cases |
| One email per account per run | HELD | Every notice of a run goes into one `Event` (`tbbehind.go:77-79`). A run that sent the recovery or the sync email defers it (`wizard.go:410-415`) |
| A failed mark-as-sent (lock busy, write refused) loops forever | HELD | It is bounded. The fold gives no notice when its own write failed (`app/tbcompleteness.go:588-591`), so a record that cannot be written never sends. A busy lock (5 s) fails one mark, and the next run's mark normally succeeds. `TestTheMarkWaitsForTheArchiveLock` passes. A persistently failing SMTP server delivers nothing, so it repeats attempts, not emails. (A lost QUIT after acceptance is the exception: see SPAM-01) |
| Newer format, loose permissions, malformed JSON, implausible counters (R4-01) | HELD | Newer: `ErrTBStreakNewer`, so no notice and a WARN. Loose permissions: a fresh streak and a WARN. Malformed: zero, so the streak starts fresh. A notice can then only come later, never falsely. Counters past 2^24 or an entry serial above the record's: `Implausible`, so a fresh streak and a WARN. `TestR4_01_AnImplausibleCounterStartsFresh` passes |
| R4-02: an eviction is silent | HELD | Every eviction adds to `tbstreak_full`, naming the dropped store (`app/tbcompleteness.go:560-576`). `TestR4_02_EveryEvictionWarns` passes |
| Clock went back | HELD | `spanAt` restarts the first time and keeps the serial (`tbstreak.go:239-244`). The flags stay on the streak, so no email repeats |
| Clock jumped forward for one run | Recorded, not a finding | This is MA-891's rule, already recorded in C2 round 4 (`review-…-c2-adversarial-r4.md:233-236`). For the *stopped* email over a real gap, see FALSE-01 |
| Two archives in one synced folder; another archive's copy of the record | HELD / out of scope | The record lives in each archive's own folder and is keyed by the hash of the store's full path, so two archives never share one. One archive written from two computers is outside the single-computer case (owner rulings 2026-10-09, `owner-rulings-2026-10-09.md:1420-1423`). A copied archive folder carries its streak, which is the same archive's history |
| README line | HELD | It names the file and says what it holds: times and the "sent" flags, with no names. An encrypted archive gets `EncryptedNote` instead (`readme.go:163-171`), so the line never claims the file is beside one |
| `behind_since` privacy and shape | HELD, apart from PLANT-01 | It is a time, shown only for an account the last ok backup found behind. It is never read beside an encrypted archive or from an implausible record (`health.go:1919-1923`). `JSONTBAccount` embeds `state.TBAccount`, which has no custom `MarshalJSON`, so the added key is not lost |

## Notes (not findings)

- **"One email per run" is narrower than it reads.** With "email on success" switched on, a run
  sends its "backup ok" email and the Thunderbird download email as well. This is the same pattern
  as the already-shipped sync email (`wizard.go:410-415` defers only after the recovery or sync
  email). The commit message's "one email per run" is true among the attention emails.
- **The subject's em dash (—)** goes into the `Subject:` header as raw UTF-8, without RFC 2047
  encoding (`alert.go:303`). The shipped sync email already does this (`alert.go:231`), and the
  owner confirmed Gmail delivery of alerts. It is recorded only because a strict relay could garble
  it.
- **The computer's name and the archive path** are in every alert email (`alert.go:174`,
  `tbbehind.go:75`). This is not new in C5. §9.3 restricts mail folder names, which stay out.
- **The race between two runs** is recorded, not a finding. If a second run ended its whole fold
  inside the first run's few seconds of SMTP sending, both would send. That gives at most one
  extra email. A run that overlaps the first fails as busy and folds nothing.

## Owed by the builder

Fix SPAM-01, FALSE-01 (after the lead's or owner's rule), FALSE-02, MISS-01 and PLANT-01, each with
its prove-fail then prove-pass test. Then run a confirm round of this pass on the fix commit.
FALSE-01 changes when the stopped email goes, so its wording also feeds the email friction review
that is owed (§13's C5 row).
