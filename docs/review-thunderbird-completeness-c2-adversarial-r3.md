# Adversarial pass, round 3: Thunderbird completeness, slice C2's round-2 fixes

**Verdict: NOT CLEAN (4 LOW, no MEDIUM or HIGH).** Every round-1 and round-2 finding holds against
`57d9cd3`. No input found makes an account falsely "complete" or "caught up" (R42) beyond the
residual the design already names. Four LOW findings remain:

- **R3-01 (LOW).** A body line starting "From " that is longer than 4,096 bytes knocks the
  separator scan out of step with the mbox reader. The real last message is then counted nowhere,
  so the count that may say "behind" comes up short. Only a file Thunderbird did not write can hold
  such a line.
- **R3-03 (LOW).** A store reached only by a weekly job, beside an hourly job on the same archive,
  is pruned between its runs. Its streak starts over every week, so the 3-day "still downloading"
  email never comes.
- **R3-04 (LOW).** With 65 or more behind stores on an hourly schedule, eviction rotates through the
  record. No streak lives past 64 runs, so no store ever reaches the 3-day email.
- **R3-05 (LOW).** Characters that render blank but are not Default_Ignorable survive name
  cleaning: the Braille blank U+2800, and the space characters U+00A0, U+2000 to U+200A, U+205F
  and U+3000.

No product code was changed by this pass.

## Header

- **Reviewed commit:** `57d9cd3` on branch `tb-completeness` (origin tip; `git pull --rebase` in
  the worktree said "Already up to date").
- **Process:** `../assurance-kit/process/adversarial-review.md`, round 3 ("Rerun after fixes" of
  `-adversarial.md` and `-adversarial-r2.md`), kept narrow on purpose. One reviewer, who did not
  write the fixes. Every finding was reproduced by a test against the real code, with real
  temporary profile folders and mbox files.
- **Clone:** `.claude/attack-c2-r3`, a git clone of the worktree at `57d9cd3`. Nothing there is
  committed. It holds:
  - round 2's seven attack files, copied from `.claude/attack-c2-r2`. The round-2 tests were
    renamed `TestZ3R2_*`, and two helpers were renamed (`zr2BigHeader`, `zr2Patch`, `zcat`). That
    was needed because the builder brought those tests in under their original names. No
    assertion was changed.
  - this round's attacks: `internal/source/zz_r3_test.go` and `internal/state/zz_r3_test.go`
    (tests `TestR3_*`).
- **Real data used** (the lead's count-only scan of the owner's profile, numbers only): 318,856
  separators, every one "From - <ctime>" after an empty line; 0 other "From " lines; 5,655 escaped
  ">From " lines; 318,854 messages with X-Mozilla-Status and 2 without.
- **Commands and results** (Go 1.26.0, darwin/arm64, 2026-11-08):
  - `go test -count=1 -v -run "TestR1_|TestR3_|TestZ3R2_" ./internal/source/ ./internal/state/ ./internal/tbcomplete/ ./internal/app/`.
    Of the 30 round-1 and round-2 tests, 29 pass. The 30th, `TestR1_ADV_C2_13` (source), fails
    as the builder predicted: its fixture has no X-Mozilla-Status, so its 2,001 pieces are now
    Loose rather than strict. Its adapted twin `TestR3_R1_13_Adapted` asserts strict 0, loose
    2,001, 2,000 loose keys and 1 with no key. It passes, so the 64 KiB bound still holds.
  - The `TestR3_*` tests: `R3_01`, `R3_03`, `R3_04` and `R3_05` are red (confirmed findings).
    `R3_02`, `R3_R42_ShapesThatMustNotInflate`, `R3_06` and `R3_R1_13_Adapted` are green.

## 1. Earlier findings: HELD

| Finding | Test (clone) | Result |
|---|---|---|
| ADV-C2-01 to -17 (round 1) | `TestR1_ADV_C2_*` (app, source, state, tbcomplete) | HELD (17/17; C2-13 by its adapted twin) |
| R2-01 cut Message-ID | `TestZ3R2_01_*` (source, app) | HELD |
| R2-02 Message-ID shapes | `TestZ3R2_02_MessageIDShapes` | HELD |
| R2-03 LF and CRLF copies | `TestZ3R2_03_LineEndingsOfOneEmail` | HELD |
| R2-04 git patch / body separator | `TestZ3R2_04_*` (source, app) | HELD |
| R2-05 glued separator, false behind | `TestZ3R2_05_*` (source, app) | HELD (glued, unknown; never behind) |
| R2-06 accounts past the cap | `TestZ3R2_06_AccountsPastTheCapLeaveNoTrace` | HELD |
| R2-07 invisible non-Cf names | `TestZ3R2_07_InvisibleNonCfCharacters` | HELD |
| R2-08 stale entries fill the cap | `TestZ3R2_08_StaleEntriesFillTheStreakCap` | HELD |

## 2. R42: can the STRICT count be inflated?

**HELD**, apart from the named residual and R3-01.

`TestR3_R42_ShapesThatMustNotInflate` gives strict 0 for each of these shapes:

- X-Mozilla-Status only in a continuation line;
- X-Mozilla-Status only in the body;
- a header that runs past 64 KiB, with the status line after the bound;
- CR-only line endings (the whole file is one long line, so it counts as Unseparated);
- a separator with a time zone;
- a localised day name ("Lun");
- a header with X-Mozilla-Status2 only.

A glued separator counts 1 strict and 1 glued, as R2-05 intends.

**R3-02, HELD (documented residual).** A separator-shaped body line after an empty line,
unescaped and followed by an X-Mozilla-Status line, counts strict. One message then counts 2. A
variant does the same: the separator, then an empty line, then the body's first block holding the
status line. Both are the residual the design names in §2.4 ("mail crafted to carry both…").
Thunderbird escapes such lines (the owner's 5,655 ">From " lines, 0 bare ones), so neither shape
appears in a store Thunderbird wrote. This is not a new finding.

**R3-01, CONFIRMED (LOW): a "From " line longer than go-mbox's buffer shifts every later note.**

The cause is in go-mbox v1.0.4 (`reader.go`). It reads lines through a 4,096-byte
`bufio.Reader`. When a separator line runs past the buffer, the reader ends the piece at the
line's first chunk. A fresh `messageReader` then reads the second chunk with
`atMiddleOfLine=false`. If that chunk also begins "From ", go-mbox ends one more, empty, piece.
`separatorScan` notes only one line. So `start(n)` hands each later piece the next piece's note,
and the last piece gets none (`startBody`, so Unseparated).

`TestR3_01_LongFromLineShiftsTheNotes` shows the mechanism: go-mbox gives 5 pieces for 4 notes. It
then shows the effect on 3 real Thunderbird-shaped messages. A crafted piece carrying
X-Mozilla-Status `0009` takes message c's separator and is set aside as Expunged. Message c becomes
Unseparated. The result is lenient 2 (strict 2, loose 0, expunged 1, unseparated 2), when it
should be 3.

This breaks the design's claim (§2.4, R2-05 fold) that an Unseparated piece "cannot hold a
message Thunderbird wrote". Each such line takes one real message out of the lenient count. Enough
of them make a false "behind", and the 3-day email starts. Shifted notes can also pair a status
piece with a separator, which raises the strict count, but the mailbox is then always "split
differently", so it is never told caught up.

Severity is LOW. It needs an unescaped "From " body line of 4,096 bytes or more, and Thunderbird
escapes those. The archived bytes are not touched (the extra empty piece comes from go-mbox, as
before this slice).

Possible fixes (the lead's call):

- have the scan copy go-mbox's chunking: a "From " line longer than the reader's buffer adds one
  note per further chunk that starts "From ";
- or count pieces by the reader's own split, not by a parallel scan;
- or treat any line over 4,096 bytes that starts "From " as glued, so the account is never told
  behind.

## 3. Strict and lenient split: false "behind" or false "caught up"

**HELD for the split's own logic.** I read the code (`verdict.go` Assess) and checked it against
the tests above:

- "behind" uses only MissingLeast, the gap by the lenient count, taken folder by folder, so a
  surplus in one folder never hides a gap in another;
- "caught up" uses only Missing, the gap by the strict count;
- glued wins over both and gives unknown;
- between the two counts the account is unknown, reason count_uncertain.

The only false-"behind" path found is R3-01, which happens below the split, in the scan.

Observation, not a finding: `MaySayComplete` does not check NoStatus. A folder whose strict count
equals its listed count can say "complete" while it also holds Loose pieces. Those pieces are
archived, so nothing listed is missing. A Loose piece cut out of a real message by an unescaped
body separator is the named residual's shape.

## 4. Streak pruning and eviction

**R3-03, CONFIRMED (LOW): a store a weekly job reaches, beside an hourly job, never keeps its
streak.**

`TBStreakUnseenRuns` is 72 runs, and the record's run count goes up on every run of any job that
writes into the archive. The builder's comment covers "a nightly job beside an hourly one on other
inputs" (safe). A weekly job is not covered: 168 hourly runs pass between its runs, so its entry
is pruned every week.

`TestR3_03_WeeklyStoreBesideHourlyNeverKeepsItsStreak` (state, folding runs the way
`updateTBStreak` does) has store B behind on each of its 3 weekly runs over 14 days. B's streak
ends with first behind 2026-11-15 and 1 run. It should be 2026-11-01. So a store that really is
behind never gets the "still downloading" email.

The failure points toward a missed email, never a false one. That matches the comment's "the
notice comes later, never falsely", except that here "later" is never. Weekly schedules exist
(`internal/schedule/kind.go`).

Possible fixes (the lead's call): count unseen time by the clock instead of by runs (for example
prune after 3 days plus the longest schedule interval), or count runs per job.

**R3-04, CONFIRMED (LOW): 65 or more behind stores rotate under 3 days.**

When every entry was found behind this run, they all tie on (SeenRun, BehindRun). `oldest()` then
takes index 0, and the newcomer is appended at the end. So each run evicts the oldest-added entry,
and each store's streak lives 64 runs.

`TestR3_04_SixtyFiveBehindStoresRotateUnderThreeDays`: 65 stores behind every hourly run for 200
runs leave 0 streaks older than 72 hours. The safe result would be at least 63. On an hourly
schedule no store ever gets the email.

Severity is LOW: it needs 65 Thunderbird IMAP accounts or stores, all behind. Each eviction is
logged.

Possible fix (the lead's call): when every entry was found behind this run, evict the newcomer
(the round-1 behaviour, now logged) instead of a running streak.

**HELD:**

- An entry pruned while it is unreachable restarts its streak. That is the safe direction:
  "Pruning a live entry only restarts its streak".
- Planted SeenRun and BehindRun values are clamped to at most Run, so they cannot keep an entry
  forever (`TestZ3R2_08` and the builder's guards).
- Stores whose verdict is unknown are still marked seen, so they are not pruned while reached.

## 5. RunLeadWith and thunderbird_more

**HELD** (by reading, and by `TestZ3R2_06` and the builder's app test):

- `tbRecords` sorts by weakness before cutting, so behind, limited and unknown are kept first.
- `ReadLastRun` adds the accounts it cuts to ThunderbirdMore, clamped at 0 and at most 2^20.
- `RunLeadWith` starts at LeadFinished whenever `more > 0`, and nothing in its loop lifts it to
  LeadComplete.
- `run.go:1423` is the only writer of `lr.Thunderbird`, and it sets ThunderbirdMore beside it.
- The one other place an account drops out before the record is `assessThunderbird`'s
  told/wholes skip. That is pre-existing: a part walk is skipped only when the same store is told
  by another walk.

C3 is not built yet. Its rule, "build a record's lead with RunLeadWith", is the remaining
obligation.

## 6. Default_Ignorable stripping

**HELD for Default_Ignorable** (`TestR3_06_BidiControlsGone`). Every bidi control is removed, and
so are the zero-width characters, the byte-order mark, U+180E, the invisible operators, U+FFF0,
tag characters, musical and Egyptian format controls, and the shorthand format controls. Strong
right-to-left letters still reorder text by the Unicode bidi algorithm. That is inherent to
showing the name, not an invisible character.

**R3-05, CONFIRMED (LOW): blank-rendering characters outside Default_Ignorable are kept.**

`TestR3_05_BlankRenderingCharactersKept`: "Inbox" plus U+2800 (Braille pattern blank, category So)
keeps its last character. So do U+3000, U+2003, U+205F and U+00A0. "Inbox⠀" and "Inbox" look the
same, and a name made only of these looks empty. The CleanFolderName comment's "looks as empty as
it is" then does not hold.

U+2800 is the same trick as the Hangul fillers R2-07 removed. The space characters are a softer
case: they show as a gap.

Possible fix (the lead's call): remove U+2800 and the Zs characters other than U+0020, or fold
them to U+0020 and trim.

## Skeptic's notes

- R3-01, R3-02 and R42's crafted shapes all need a file Thunderbird did not write, or one written
  before Thunderbird escaped "From " lines. The owner's real profile has 0 bare "From " lines.
  R3-01 is kept as a finding, not folded into the residual, because it reaches the direction the
  design calls impossible (a Thunderbird message counted nowhere).
- R3-03 and R3-04 were tested on the state model, folded in `updateTBStreak`'s order. They were
  not run as an end-to-end backup. Whether one archive can carry both an hourly and a weekly job
  on different inputs comes from the builder's own comment ("a nightly job beside an hourly one on
  other inputs"), not from a run.
- Not checked: Thunderbird's own mbox writer, `MboxMsgOutputStream.cpp`, which is not in the copies
  under `.claude/`. A Windows store, whose ctime day padding is still unseen (lab row MA-1199).

Reviewed 2026-11-08 by Claude (Opus 5.5), round-3 adversarial reviewer. Findings are reproduced
against real code; the severities are this reviewer's judgement.
