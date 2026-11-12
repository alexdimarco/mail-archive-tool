# Adversarial pass, round 4: Thunderbird completeness, slice C2 after round 3's fixes and the header ruling

**Verdict: NOT CLEAN (2 LOW, no MEDIUM or HIGH).** Every round-1, round-2 and round-3 finding holds
against `6afd74d`. The owner's header rule (strict when the header has X-Mozilla-Status OR is
well-formed) adds no shape beyond the widened residual the design names. Name cleaning is held. No
input found makes an account falsely "complete" or "caught up" (R42), and none makes a false
"behind". Both findings are about the streak record, and each needs an unusual record or setup:

- **R4-01 (LOW).** A planted streak record with its run counter at the cap (2^40) freezes the
  counter. Every planted entry then looks "found behind by this run", so a full record of 256 such
  entries turns away every real behind store for 30 days. The run's WARN (`tbstreak_full`) says
  all 256 accounts "were still downloading at the last backup", and no run saw that.
- **R4-02 (LOW).** With the record full (257 or more behind stores across the archive's jobs), a
  store that only a weekly job reaches loses its streak to an hourly job's new behind store. At
  its own run it takes an hourly store's slot back. It never keeps a streak, so its 3-day email
  never comes. The only trace is a log line: there is no WARN, so `status` shows nothing.

No product code was changed by this pass.

## Header

- **Reviewed commit:** `6afd74d` on branch `tb-completeness` (origin tip; `git pull --rebase` in
  the worktree said "Already up to date"). In scope: `git show 9e32179` (round-3 fixes) and
  `git show 6afd74d` (the header ruling).
- **Process:** `../assurance-kit/process/adversarial-review.md`, round 4 ("Rerun after fixes"),
  kept narrow on purpose. One reviewer, who did not write either commit. Every finding was
  reproduced by a test against the real code, using real temporary profile folders, mbox files and
  archive folders. The fault seam `wrapMboxStream` (mocks are allowed for fault injection) was used
  for one test.
- **Clone:** `.claude/attack-c2-r4`, a git clone of the worktree at `6afd74d`. Nothing there is
  committed. It holds:
  - the earlier rounds' attack files, copied from `.claude/attack-c2-r3`. Round 1 is `TestR1_*`
    and round 2 is `TestZ3R2_*`, unchanged. Round 3 is renamed `TestZ4R3_*`, with helpers
    `z4itoa`, `z4r3Long` and `z4r3Run`. The renaming was needed because the builder brought those
    tests in under their original names. The only other change is to the state helper's
    signatures: `StartRun(now)`, `Seen(store, now)` and `Prune(now)` now take the run's time. No
    assertion was changed.
  - this round's attacks: `internal/source/zz_r4_test.go`, `internal/state/zz_r4_test.go` and
    `internal/app/zz_r4_test.go` (tests `TestR4_*`).
- **Real data, as briefed** (the lead's count-only scan of the owner's profile, numbers only, not
  re-checked by this reviewer):
  - 318,861 messages, every separator "From - <ctime>" after an empty line;
  - 0 unescaped body "From " lines and 5,655 escaped ones;
  - 318,859 messages with X-Mozilla-Status. The 2 without pass the lead's header check, which the
    lead describes as equal to or stricter than `wellFormedHeader`;
  - 178 messages with X-Mozilla-Status fail the lead's header check.
- **Commands and results** (Go 1.26.0, darwin/arm64, 2026-11-08):
  - `go test -count=1 -v -run "^(TestR1_|TestZ3R2_|TestZ4R3_)" ./internal/source/ ./internal/state/ ./internal/tbcomplete/ ./internal/app/`:
    every test passes except two, and both fail because the rule they encode was changed on
    purpose. The adapted twin of each passes (section 1).
  - `go test -count=1 -v -run "TestR3_" ./internal/source/ ./internal/state/ ./internal/app/`
    (the builder's brought-in round-3 guards, which include the adapted R3-01): all pass.
  - `go test -count=1 -v -run "TestR4_" ./internal/source/ ./internal/state/ ./internal/app/`:
    `TestR4_01_PlantedCounterAtTheCap` and `TestR4_PlantedRunCounterAtTheCapBlocksNewStreaks`
    (R4-01) and `TestR4_02_WeeklyStoreInAFullRecordNeverKeepsItsStreak` (R4-02) are red. The
    others are green.
  - `go test -count=1 -skip "<the reds above>" ./internal/source/ ./internal/state/ ./internal/tbcomplete/ ./internal/health/`:
    all ok. The clone's own suite is green apart from the attack files.

## 1. Earlier findings: HELD

| Finding | Test (clone) | Result |
|---|---|---|
| ADV-C2-01 to -17 (round 1) | `TestR1_ADV_C2_*` (app, source, state, tbcomplete) | HELD (17/17). C2-13 fails as in round 3, because its fixture has no Date: line and so stays loose. Its adapted twin `TestZ4R3_R1_13_Adapted` (strict 0, loose 2,001, 2,000 loose keys, 1 with no key) passes, so the 64 KiB bound holds. |
| R2-01 to R2-08 (round 2) | `TestZ3R2_*` | HELD (all) |
| R3-01 long "From " line | `TestZ4R3_01_*` (the original), the builder's `TestR3_01_*` (source, app) | HELD under the lead's rule. Two things are unchanged: the original's mechanism check (go-mbox gives 5 pieces for 4 notes) and its effect check ("lenient at least 3"). Both are red, because the lead replaced the expected effect: misaligned, nothing counted, never behind. The builder's adapted tests assert that rule end to end (unknown, split differently, no streak) and pass. |
| R3-02 residual | `TestZ4R3_02_*` | HELD (documented residual, strict 2) |
| R3-03 weekly store pruned | `TestZ4R3_03_*`, `TestR4_ClockBackSixtyDays` | HELD (pruning now goes by time) |
| R3-04 65 behind rotate | `TestZ4R3_04_*` | HELD (cap 256; streaks found behind are never evicted) |
| R3-05 blank names | `TestZ4R3_05_*`, `TestZ4R3_06_*` | HELD |

## 2. The header check (OR rule): HELD

What I checked: can a body "From - <ctime>" line after an empty line, plus attacker text, pass
`wellFormedHeader` more easily than the design's widened residual says?

The fold "owner ruling: header check" describes the residual this way: a separator-shaped body
line followed by X-Mozilla-Status or by a well-formed header. I found no shape outside it.

**The bound, as the walk sees it** (`TestR4_HeaderBoundAsTheWalkSeesIt`). go-mbox hands each piece
on with every line ending rewritten as CRLF, so the 64 KiB bound counts CRLF bytes:

- a CRLF header whose empty line ends exactly at byte 65,536 is strict;
- one byte longer is loose;
- an LF header that fits within 64 KiB as LF but not once rewritten as CRLF is loose.

That last case leans toward lenient, so it is safe. The builder's byte-exact guard tests
`wellFormedHeader` on LF bytes directly; this test confirms the same edge through the real reader.

**The shapes the lead named** (`TestR4_HeaderShapes`, 24 cases through the walk). Every one matches
the fold's stated reading:

- **All header** (no empty line, the piece ends at the next separator): strict when every line is
  a field. One colon-less line ("Thanks") makes it loose.
- **CR-only:** a CR inside one LF line ("Date: d\rFrom: …") is one line, so the second field is
  not seen and the piece is loose. A CR inside a value, followed by a real field on the next LF
  line, is strict, but that still needs Date: and From: at the start of real lines. A "\r\n" line
  is the empty line (go-mbox strips CRLF), so a header cut there to only "From:" is loose. A whole
  file with CR-only line endings is one line, so Unseparated (round 3).
- **NUL bytes:** a NUL in any field name makes the piece loose. A NUL in a value does not.
- **8-bit bytes:** an 8-bit byte in a name makes the piece loose. One in a value does not.
- **Empty values:** "Date:" with no value counts as present, so "Date:\nFrom:\n" is strict (the
  builder's reading).
- **Field combinations:** duplicate fields are strict. Date: with Message-ID: is strict. Date:
  alone is loose, and From: alone is loose.
- **Lines that refuse a header:** a Date: line that is only a continuation, "Date :" (a space
  before the colon), a first line that is a continuation, an empty name (": v"), and an empty
  header followed by a well-formed block are all loose. Lower-case names are strict.

**The least attacker text found** (`TestR4_ResidualLeastText`) is the separator line, then
"Date:\nFrom:\n": 12 bytes, all header, before the next separator. One real message then counts 2.
The positive twin, escaped with ">" as Thunderbird writes it, counts 1. This is the residual the
fold names. Before the ruling, the same split needed only an X-Mozilla-Status line.

In practice the widening is small, and the reason is the separator itself. "From - <ctime>" is
Thunderbird's own separator, so a pasted excerpt carrying it was most likely exported from
Thunderbird and already carries X-Mozilla-Status. Either way the separator has to be unescaped,
and Thunderbird escapes it.

**Skeptic's note.** The 178 real messages with X-Mozilla-Status that fail the lead's check say that
real headers are often not well-formed. Under the OR rule this makes no difference for them. A
future real message without the line and with a messy header would be loose, so its account could
not say complete. That is the safe direction.

## 3. Misaligned mailboxes: HELD

**Reading go-mbox v1.0.4's `reader.go`.** go-mbox checks for "From " only at a line start it knows
about. It sets `atMiddleOfLine` after every chunk that fills its buffer, and the scan likewise sees
no new line until "\n". So go-mbox cannot miss a line the scan notes. Its only extra split is
round 3's: the tail of a long "From " line, read by a fresh piece reader. That means equal numbers
are equal splits, as the builder claims, and a crafted mailbox cannot stay aligned while its pieces
shift. Inflation while aligned is the section-2 residual and nothing more.

**Shapes Thunderbird writes stay aligned** (`TestR4_NoMisalignmentInThunderbirdShapes`):

- long body lines whose 4,096-byte chunks begin "From " in the middle of the line;
- lines of exactly 4,096 and 8,192 bytes before the empty line and the separator;
- CRLF throughout (Windows-shaped), including a long CRLF line whose CR falls on the buffer's last
  byte;
- leading empty lines, and no final newline.

In each case the pieces equal the notes and every message is counted. A glued separator after a
4,096-byte line is aligned and counts as Glued 1.

**A stream that fails mid-file is never aligned and short**
(`TestR4_AStreamFailingMidFileIsNeverAlignedShort`). Through the walk's fault seam, the read fails
at 1,938 offsets across a 40-message file, and all 1,938 come out misaligned. The reason: the piece
being read when the stream failed already had its separator noted, so there is always one more
note than pieces. A file whose first line is not "From " is not read as an mbox at all
(`sniffMbox`).

So no mailbox in normal use is misaligned, and the result is never a permanent false "can't tell".
A mailbox stays misaligned only while it holds an unescaped "From " line of 4,096 bytes or more,
which Thunderbird does not write.

## 4. Time-based pruning and the 256-entry table

**HELD:**

- **A clock that goes back 60 days and stays back** (`TestR4_ClockBackSixtyDays`). A reached behind
  store keeps its streak; its start time restarts at the jump (MA-891's rule) and it reaches the
  threshold again. An unreached entry is pruned about 30 days after the jump, never at the jump.
  The clamp of `seen_at` to `last_run_at` brings any future `seen_at` down within one run.
- **Planted 2099 times** (`TestR4_PlantedFutureTimesArePrunedWithinThirtyDays`). Stale entries
  are gone within 30 days and one run. A reached entry is never pruned.
- **Planted shapes and the earlier caps:** round 1's `-09`, round 2's `-08` and round 3's `-04`
  tests pass. Entries found behind by this run are never evicted: existing streaks are folded
  before new ones, so a newcomer can never evict a streak that is folded later in the same run.

**R4-01, CONFIRMED (LOW): a planted counter at the cap freezes "this run".**

`ReadTBStreak` clamps `run` to at most `maxTBStreakRun` (2^40), and every `seen_run` and
`behind_run` to at most `run`. `StartRun` raises `Run` only while it is below the cap. So a record
planted with `run` = 2^40 never moves again. Every entry then has `BehindRun == Run`, and `oldest()`
reads that as "found behind by this run".

`TestR4_01_PlantedCounterAtTheCap` (app, through `updateTBStreak`, real files) plants 256 entries
that no run reaches. A real behind store gets no streak. The run's WARN reads "is full: all 256
accounts in it were still downloading at the last backup, so it did not start counting 1 more
(imap.real.example)". `TestR4_PlantedRunCounterAtTheCapBlocksNewStreaks` (state, every run
written and read back) shows how long it lasts: 720 hourly runs, which is 30 days, until time
pruning removes the planted entries. With a counter of 1,000 instead, the positive twin evicts a
planted entry on the first run.

**Severity: LOW.**

- It needs the owner-only file to be written by another hand. `util.ReadSecureFile` refuses a
  record that others can read. The counter cannot get near 2^40 by itself.
- It never makes a false "caught up" or a false email.
- It delays the 3-day notice by 30 days. It is not silent: there is a WARN, though its words are
  untrue.

**Possible fixes (the lead's call):**

- make "found behind by this run" independent of the counter. For example, mark it during the fold
  (a set of stores the run found behind) instead of comparing `BehindRun` with `Run`;
- or, when `Run` reaches the cap, shift `Run` and every entry's `SeenRun` and `BehindRun` down by
  the same amount.

**R4-02, CONFIRMED (LOW): in a full record, a weekly job's store never keeps its streak, and no WARN
says so.**

The rule (R3-04, the builder's reading "currently behind = found behind by this run") protects only
entries that this run found behind. A store that only a weekly job reaches is not found behind by
an hourly job's run. Its `SeenRun` is also the oldest, so it is the first entry evicted.

`TestR4_02_WeeklyStoreInAFullRecordNeverKeepsItsStreak` (app, through `updateTBStreak`) sets this
up:

- an hourly job reaches 255 behind stores plus one of two others, alternating (so 257 behind
  stores in all);
- a weekly job reaches store W, which is behind on each of its 4 runs over 3 weeks.

W never holds a streak past its own run (0 of 4 runs). Each time, the next hourly run evicts it,
and at its own run W evicts an hourly store. The log says "dropped the record of
imap.weekly.example" 3 times, and the runs leave 0 WARNs. The 3-day "still downloading" email
never comes for W, and `status` shows nothing.

The same applies to an account that this run found unknown (for example, one with a folder-cache
read error for that run) while the record is full.

**Severity: LOW.** It needs 257 or more behind stores across one archive's jobs, plus a schedule
that reaches some of them less often than others. It points toward a missed email, never a false
one.

**Possible fixes (the lead's call):**

- also protect an entry whose last reach found it behind (`BehindRun == SeenRun`) and that is
  within `TBStreakUnseenFor`. Then, when nothing else can go, give the newcomer TBRoomFull with its
  WARN;
- or leave a WARN whenever an eviction drops an entry that was behind at its last reach.

**Recorded, not a finding** (`TestR4_ForwardClockJumpReachesTheThreshold`). Take a store behind at
T, then behind again at the next hourly run whose clock wrongly reads T + 4 days: it is "still
downloading" after 2 runs and 1 real hour. This is MA-891's rule, the same as the presync streak.
One clock cannot tell a wrong jump from a computer that was switched off for 4 days.

## 5. Name cleaning: HELD

What I checked (`TestR4_NameCleaningSurvivorsAndCollisions`): every code point, as "a" + r + "b".
No character of category Zs other than U+0020, no Cf, and nothing in Go's
`Other_Default_Ignorable_Code_Point` or `Variation_Selector` tables survives. A name made only of
U+2800, U+00A0, U+3000 and U+2003 becomes "".

**Survivors, logged.** Each renders as a mark on nothing, a box or a glyph, or blank only with a
rare font:

- a name made only of combining marks (U+0301 U+0301);
- private use (U+E000);
- U+1D159 MUSICAL SYMBOL NULL NOTEHEAD;
- U+16FE4 KHITAN SMALL SCRIPT FILLER (a filler like the Hangul ones, category Mn; it renders blank
  only with a Khitan font installed, otherwise as a box);
- U+303F;
- U+FFFC.

U+16FE4 is the nearest to R3-05. It is recorded as a candidate for the remove list, not as a
finding, because on a system without that font it shows as a box.

**Collisions, by design.** These pairs now give the same name:

- "Inbox" and "Inbox" + U+00A0;
- "A B" and "A" + U+3000 + "B";
- "AB" and "A" + U+200B + "B";
- "AB" and "A" + tab + "B";
- "A  B" and "A B";
- " Inbox " and "Inbox".

What a collision does: nothing beyond the words.

- Every key and hash uses the raw path, never the cleaned name: `StoreID`, the streak's `store`,
  `NotKeptHash` (`FolderIdentity(f.Path)`) and the carry-forward.
- No map in the product is keyed by a cleaned Thunderbird name (I searched for one).

So two accounts or folders that look alike are two entries with one name. In `tbstreak_full`'s list
of names, or in C3's words, they look the same. They already looked the same before cleaning.

## Skeptic's notes

- R4-01 and R4-02 were reproduced through `updateTBStreak` with real files, not as a full backup
  run. R4-02's setup (an hourly job and a weekly job writing into one archive, on different
  inputs) comes from the builder's own comments and round 3, not from a run of two installed
  schedules.
- The real data above is the lead's, as briefed. In particular, "the 2 without the line are
  well-formed" rests on the lead's check, which the brief describes as equal to or stricter than
  `wellFormedHeader`. I did not see it. A count-only re-scan (MA-1190) would settle it.
- Not checked: Thunderbird's own mbox writer (`MboxMsgOutputStream.cpp`), which is not in the
  copies under `.claude/`; a real Windows store (lab row MA-1199). The CRLF shapes here are
  Windows-shaped files written by the test.
- The reads of go-mbox's splitting rest on v1.0.4's `reader.go` (go.mod pins it). A change of
  version would need this rechecked.

Reviewed 2026-11-08 by Claude (Opus 5.5), round-4 adversarial reviewer. Findings are reproduced
against real code; the severities are this reviewer's judgement.
