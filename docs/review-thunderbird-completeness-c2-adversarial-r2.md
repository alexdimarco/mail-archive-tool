# Adversarial pass, round 2: Thunderbird completeness, slice C2's fix round

**Verdict: NOT CLEAN.** Every round-1 finding is closed. But two new ways remain for mail content
to make an account read "caught up" while messages are missing. That breaks R42 (never a false
"complete"):

- **R2-04 (MEDIUM).** The builder's named residual is wider than the design says. A git patch sent
  as a plain-text attachment has a first line of the separator's shape. That line counts as a
  message in any store written before Thunderbird began escaping such lines.
- **R2-01 (LOW).** A Message-ID line that crosses the 64 KiB header bound is cut. One email kept in
  two Gmail folders can then get two identities.

Five more LOW findings follow. One gives a false "behind" (and a later false email), two let the
"still downloading" email be lost silently or let an account vanish from the record, and two are
small identity and name-cleaning gaps. No product code was changed by this pass.

## Header

- **Reviewed commit:** `1e10b7c` on branch `tb-completeness` (origin tip at the start of the
  pass; the worktree was already up to date).
- **Process:** `../assurance-kit/process/adversarial-review.md`, round 2 ("Rerun after fixes" of
  `docs/review-thunderbird-completeness-c2-adversarial.md`). One reviewer, who did not write the
  fixes, worked as finder and then as skeptic. Every finding below was reproduced against the
  real code by a test, unless it is marked "by reading".
- **Clone:** `.claude/attack-c2-r2`, a git clone of the worktree at `1e10b7c`. Nothing there is
  committed. It holds:
  - **round 1's four attack files**, adapted to the fixed code as
    `internal/{app,source,state,tbcomplete}/zz_r1_test.go` (tests renamed `TestR1_ADV_C2_*`);
  - **this round's attacks:** `internal/source/zz_r2_test.go`, `internal/app/zz_r2_test.go` and
    `internal/state/zz_r2_test.go` (tests `TestR2_*`).
- **Thunderbird source used:** the copies under `.claude/tbsrc-esr153` (ESR 153 and Release 159
  files). They include `nsImapMailFolder.cpp` and `nsMsgBrkMBoxStore.cpp`. They do not include
  `MboxMsgOutputStream.cpp`, Thunderbird's mbox writer.
- **Commands and results** (Go 1.26.0, darwin/arm64, macOS, 2026-11-08):
  - `go test -count=1 -run TestR1_ADV_C2_ ./internal/tbcomplete/ ./internal/state/ ./internal/source/ ./internal/app/`:
    exit 0, all 17 of round 1's tests pass after adaptation.
  - `go test -count=1 -v -run TestR2_ ./internal/source/ ./internal/app/ ./internal/state/`:
    exit 1. Each red test is a confirmed finding below. `TestR2_02` and `TestR2_05` (source) are
    green, so those items are held.
  - `go test -count=1 -skip TestR2_ ./internal/tbcomplete/ ./internal/state/ ./internal/source/ ./internal/thunderbird/ ./internal/health/ ./internal/app/`:
    see "Builder's guards" below.
- **File digests (SHA-256) at `1e10b7c`:**
  - `internal/source/mailboxcount.go`: `1bd26efe…fe3cbe`
  - `internal/source/localwalk.go`: `e79320ae…f398fc`
  - `internal/app/tbcompleteness.go`: `13a3806c…5baf`
  - `internal/tbcomplete/verdict.go`: `1278803d…5530`
  - `internal/tbcomplete/walked.go`: `0d2fd522…ee8a`
  - `internal/state/tbstreak.go`: `983ea9f8…11bd`
  - `internal/state/tbcompleteness.go`: `24b2ebb1…ebc5ebc`
- **Tally:** the 17 round-1 items were rechecked, and all are closed. Round 2 attacked 14 new items:
  7 confirmed (1 MED, 6 LOW) and 7 held.

## 1. Round-1 findings: does each fix close it?

Round 1's own tests were brought in and adapted only where the API changed: streak lookup by store
key, `IDs`/`NoID` renamed `Keys`/`NoKey`, and a blank line before each separator in ADV-C2-13's
fixture, which the new count needs. Each adapted test asserts the safe outcome. All pass.

| Round-1 ID | Fix | Round-2 judgement |
|---|---|---|
| ADV-C2-02 (MED) | identity = Message-ID, else content without X-Mozilla lines | **Closed** for the tested case (TestR1_ADV_C2_02). New gaps R2-01, R2-03 below. |
| ADV-C2-03 (MED) | count only pieces at a Thunderbird-shaped separator after an empty line | **Closed** for "From the desk…" lines (TestR1_ADV_C2_03). The residual is wider than written: R2-04. |
| ADV-C2-16 | store under a profile opened by another reader → unknown `other_reader` | **Closed** (TestR1_ADV_C2_16). Detection unchanged, as the design notes. |
| ADV-C2-12 | maildir count skips dotfiles and empty files | **Closed** (TestR1_ADV_C2_12). |
| ADV-C2-14 | obsolete "Message-ID :" read | **Closed** (TestR1_ADV_C2_14; R2-02). |
| ADV-C2-01 | streak and carry-forward keyed by path hash | **Closed** (TestR1_ADV_C2_01, both orders, 2 runs). |
| ADV-C2-05 | loose-mode record: WARN, fresh start, rewritten 0600 | **Closed** as the lead chose: never silent (adapted test: one WARN, then extended). |
| ADV-C2-07 | own settings never in OtherStores | **Closed** (TestR1_ADV_C2_07). |
| ADV-C2-08 | 1,000-folder total cap | **Closed** (TestR1_ADV_C2_08). The account cap is a separate gap: R2-06. |
| ADV-C2-10 | later format refused, never rewritten | **Closed** (TestR1_ADV_C2_10). |
| ADV-C2-11 | Cf and U+2028/2029 removed | **Closed** for the listed characters. Invisible non-Cf characters remain: R2-07. |
| ADV-C2-04, -06, -09, -13, -15, -17 (held in round 1) | — | **Still held** (their adapted tests pass). |

## 2. New findings

| ID | Lens | Claim (file:line) | Verdict | Severity |
|---|---|---|---|---|
| R2-04 | MAIL | `source/mailboxcount.go:272` `separatorShape` accepts any sender word, so a git patch's "From <sha> Mon Sep 17 00:00:00 2001" after an empty line is a separator | CONFIRMED | **MED** |
| R2-01 | MAIL | `source/mailboxcount.go:146` + `:232-242`: the Message-ID is read from a section cut at 64 KiB, so a cut ID is hashed | CONFIRMED | LOW |
| R2-05 | MAIL / notice | `tbcomplete/verdict.go:560-565`: a split mailbox's gap is called "behind" though the uncounted pieces may be the missing messages | CONFIRMED | LOW |
| R2-08 | INS / notice | `state/tbstreak.go` `Behind` (`len(s.Accounts) >= TBAccountsMax` returns unchanged): stale entries are never dropped, so a full record silently refuses new streaks | CONFIRMED | LOW |
| R2-06 | WIRE | `app/tbcompleteness.go:268-277`: accounts past 64 are left out of the record in input order, with no count | CONFIRMED (latent until C3) | LOW |
| R2-03 | MAIL | `source/mailboxcount.go:154-160`: maildir content identity hashes the bytes as on disk, so LF and CRLF copies of one ID-less email have two keys | CONFIRMED | LOW |
| R2-07 | PRIV | `state/tbcompleteness.go:125-136`: invisible characters outside Cf pass name cleaning | CONFIRMED (design gap) | LOW |
| R2-02 | MAIL | Message-ID shapes: folded, case, spaces, brackets, two headers, body-only, encoded word | HELD | — |
| R2-03a | MAIL | CRLF vs LF copies in an mbox store | HELD | — |
| R2-09 | WIRE | every Thunderbird store of a run gets a result (item 5) | HELD | — |
| R2-10 | CON | `walkFailed` reads the walk's real error | HELD (by reading) | — |
| R2-11 | WIRE | the 1,000-folder cap hides a folder's state (item 3) | HELD | — |
| R2-12 | INS | streak fresh start, newer-format refusal, path-hash key (item 4) | HELD | — |
| R2-13 | MAIL | separator scan stays in step with go-mbox's pieces | HELD (by reading) | — |

---

### R2-04 (MED): a git patch attachment counts as a message in a store written before escaping

**What happens.** The count now takes a piece only when its "From " line follows an empty line and
matches `separatorShape`. The shape accepts any sender word: `^From \S+ +<day> <mon> …`. It was
widened for the repo's fixtures (`From sender Mon Mar  3 …`). Thunderbird's own ImapMail separator
is `From - ` plus `ctime` (`nsImapMailFolder.cpp:4199` in the ESR 153 copy).

`git format-patch` writes its first line as `From <40-hex sha> Mon Sep 17 00:00:00 2001`. That has
the separator's shape exactly. A patch sent as a 7bit plain-text attachment puts that line right
after the part's empty line. In a store whose writer did not escape it, the attachment's tail
counts as one more message.

The design calls the residual "a body line of exactly Thunderbird's separator shape". It is in
fact any classic-mbox-shaped line. The patch case is ordinary mail on developer lists, not crafted
mail.

**Tests.** Positive twin first: the same line escaped (`>From …`, as current Thunderbird writes it)
counts 1 and reads behind 2 of 3.

```
--- FAIL: TestR2_04_SeparatorShapedBodyLine            (source)
    unescaped git patch: counted 2 (unseparated 0, archived 2), want 1
    Thunderbird's separator quoted in a body after an empty line: counted 2, want 1
--- FAIL: TestR2_04_GitPatchAttachmentMakesFalseCaughtUp (app)
    2 emails on disk, one with an unescaped git patch attachment, 3 listed:
    caught_up X 3 Y 3 split 0 complete true — a false caught_up (R42)
```

**How common.**
- Each such email adds 1 to X in its own folder. A list folder holding hundreds of patch
  attachments can cover hundreds of missing messages. The tolerance is at most 20 for any folder
  under 10,000 messages.
- The `nsMsgBrkMBoxStore.cpp:748` comment says current Thunderbird escapes "From " lines when it
  writes. That means such lines appear only in mail written before that change, or by another
  writer.
- **Not checked:** which Thunderbird version started escaping, and whether it writes `>From`
  (mboxrd style). `MboxMsgOutputStream.cpp` is not in the copies. Years-old offline copies are the
  common case for a long-lived profile.

**Cheap ways to close it** (for the count only; archiving is unchanged):
1. **Narrow the shape to Thunderbird's own separator for ImapMail stores:** `^From - ` plus the
   `ctime` time. This closes the patch case outright. The fixtures that use `From sender …` would
   read `split_differently`, which fails safe; they would need Thunderbird's form. **Not checked:**
   whether `MboxMsgOutputStream` also writes `From - `. Lab row MA-1199 should look at a real
   ESR 153 store.
2. **Also require the piece's header block to hold `X-Mozilla-Status`.** Thunderbird writes it at the
   top of every offline copy (`nsImapMailFolder.cpp:4207`). The patch's second piece starts with
   `From: Dev …` and has none.
   - In a mailbox where most pieces have it, treat a separator-shaped piece without it as
     unseparated (`split_differently`).
   - What still gets through is mail crafted to carry both a `From - <ctime>` line and an
     `X-Mozilla-Status` line in an unescaped store.
3. With 1 and 2, the residual is only deliberately crafted mail, and only in a store written by an
   older Thunderbird.

### R2-01 (LOW): a Message-ID line cut at the 64 KiB bound gives one email two identities

**What happens.**
- `IdentityKey` reads the Message-ID from `headerSection(raw)`. When no empty line ends the header
  within 64 KiB, that is the raw bytes **cut at 64 KiB**.
- If the Message-ID line crosses the cut, the part before it is read and hashed as the ID (here,
  `bbbbbbb`).
- A second copy of the same email, whose header is a few bytes shorter, is read whole and hashed
  as the full ID.
- Thunderbird's per-copy lines (`X-Mozilla-Status`, `-Status2`, `-Keys`) sit at the top, before the
  Message-ID. So a different `X-Mozilla-Keys` length between two copies moves the ID across the
  bound in one copy only. For Gmail, X then counts the email twice.

**Tests.** The setup is one email with about 64 KiB of `X-Junk` headers, held in All Mail without an
`X-Mozilla-Keys` line and in INBOX with one (16 + 80 spaces). All Mail lists 2. Positive twin: both
copies alike read behind 1 of 2.

```
--- FAIL: TestR2_01_PerCopyLineShiftsTheIDAcrossTheBound (source)
    one email, two copies differing only in X-Mozilla-Keys: two identity keys …
    walked: 2 distinct keys for one email (no key: INBOX 0, All Mail 0)
--- FAIL: TestR2_01_GmailCutIDMakesFalseCaughtUp (app)
    one email on disk (two copies differing in X-Mozilla-Keys), 2 listed:
    caught_up X 2 Y 2 complete true no identity 0 — a false caught_up (R42)
```

**Why LOW.** It needs both of these:
- a sender who writes about 64 KiB of header before the Message-ID (Gmail's header size limit:
  not checked);
- copies of one email whose per-copy lines differ in length. Thunderbird pads `X-Mozilla-Keys` so
  that tags can be written in place. Whether copies in two folders ever differ in length (a long
  tag, or a copy written by another version) is **not checked**.

Each such email covers only one missing message.

**Cheap fix.** Take a Message-ID only from a header block whose end lies within the bound, as
`headerBlock` finds it. Otherwise the message has no identity: it gets `no_identity`, X is lowered,
and the account may not say complete. A smaller alternative: accept the ID line only when it, and
its continuation lines, end before the cut.

### R2-05 (LOW): a split mailbox reads "behind" because of the very pieces it could not count

**What happens.**
- A real Thunderbird separator with no empty line before it is not counted (fail-safe for R42, as
  the design says). Its message drops out of X.
- `Assess` checks `r.Missing > r.Tolerance` **before** `SplitMailboxes > 0`, so the gap these
  dropped messages make reads as a "certain" behind.
- The behind streak then starts. C5's "still downloading" email reads that streak, so the email
  can go out for an account that holds every message.

The design's own §2.4 note foresees "count low and read `behind`". This finding is that the
"certain gap" rule is not certain here.

**Test** (`TestR2_05_UnseparatedPiecesMakeAFalseBehind`). Positive twin: the same five emails with
empty lines read caught up.

```
all 5 listed emails on disk, separators with no empty line before them:
behind X 1 Y 5 split 1; streak [{… Runs:1 …}] — a behind streak from pieces the count could not tell
```

**Not checked:** whether any Thunderbird version writes a separator with no empty line before it.
Lab row MA-1199 again.

**Cheap fix.** For a split mailbox, say behind only if it would still be short with every
unseparated piece taken as a message. That is, compare against `X + Unseparated` for the kept
folders; otherwise give `split_differently`. A false complete stays impossible, because X itself
is unchanged.

### R2-08 (LOW): stale streak entries fill the record, and new behind accounts are refused silently

**What happens.**
- An account's streak entry ends only on a `caught_up` or `limited` run of the same store.
- A store that stops being backed up keeps its entry forever. This happens when a profile moves (a
  new path, so a new key), a store is dropped from the job's inputs, or an account is removed.
- `TBStreak.Behind` returns the record unchanged once it holds 64 entries. A new behind store then
  never gets a streak.
- Nothing logs it and no WARN is raised, so the "still downloading" email can never come for that
  store.

```
--- FAIL: TestR2_08_StaleEntriesFillTheStreakCap
    64 stale entries: a new behind store gets no streak (its "still downloading" email can never come), silently
```

**Why LOW.** It needs 64 abandoned behind stores in one archive's record.

**Fix.** Drop entries whose `LastBehind` is older than some span (for example 30 days), or drop the
oldest entry first. Either way, raise a WARN when a streak cannot start.

**For C5:** an abandoned entry should never trigger an email for a store the run did not reach.

### R2-06 (LOW, latent until C3): accounts past 64 vanish from the record with no count

`tbRecords` keeps the first 64 results in input order and drops the rest. Nothing records how many
were dropped. When the 65th account is the only behind one, the record shows 64 complete accounts.
`RunLead` over the full results says `so_far`.

```
--- FAIL: TestR2_06_AccountsPastTheCapLeaveNoTrace
    65 accounts, the 65th behind: the record keeps 64, all complete, and no count of the one left out (lead from the record: so_far)
```

- **Pre-existing:** this was already at `18f0b6c`, and round 1 did not attack it.
- **Why latent:** nothing reads the record's accounts for a lead today. `health.go` reads only the
  new WARN code. C3 will read them.
- **Fix:** keep the weakest accounts first (behind, limited, unknown) and add an `accounts_more`
  count. C3 must never build the lead from a record that has `accounts_more` set.

### R2-03 (LOW): LF and CRLF copies of one ID-less email in a maildir store count twice

In an mbox store, go-mbox rewrites every line ending to CRLF, so the content identity is the same
for both copies (held: R2-03a). A maildir file is hashed as it is on disk. An LF copy and a CRLF
copy of one email with no Message-ID therefore have two keys, and Gmail's X counts the email twice.

```
--- FAIL: TestR2_03_LineEndingsOfOneEmail
    maildir (bytes as on disk): LF and CRLF copies of one ID-less email have two keys
```

- **Why LOW.** It needs all three: the maildir store (not the default), a Gmail email with no
  Message-ID, and copies written with different line endings (for example, a profile moved between
  Windows and macOS).
- **Not checked:** whether Thunderbird rewrites line endings when it writes maildir files.
- **Fix:** make line endings the same (CRLF to LF) before hashing the content identity.
- **Note for the gmail-dedup design:** its §4.4 split is the source of this identity, so the same
  question applies there.

### R2-07 (LOW, design gap): invisible characters outside Cf pass name cleaning

The Cf characters and U+2028 and U+2029 are all removed. Held, tested: U+200B, U+200D, U+2060,
U+FEFF, U+00AD, U+180E, U+FFF9, U+E0001, U+202E, U+2066 and U+0085.

These invisible characters remain:
- U+034F, the combining grapheme joiner;
- the Hangul fillers U+115F, U+1160, U+3164 and U+FFA0;
- U+17B4 and U+17B5;
- the variation selectors U+180B to U+180F, U+FE0F and U+E0100.

A folder name made only of Hangul fillers stays and looks empty. Two names that differ only by
U+034F look the same.

```
--- FAIL: TestR2_07_InvisibleNonCfCharacters
    11 invisible non-Cf characters kept; a name of only Hangul fillers stays "ㅤㅤᅟ" (looks empty)
```

**Fix:** also remove the characters in `unicode.Other_Default_Ignorable_Code_Point` and
`unicode.Variation_Selector`. Together with Cf, these cover Unicode's Default_Ignorable_Code_Point
set.

---

## 3. Held (attacked, not reproduced)

- **R2-02: Message-ID shapes** (`TestR2_02_MessageIDShapes`, green). These all give one key:
  - folded (`Message-ID:` then `<abc@x>` on the next line, LF or CRLF);
  - any letter case;
  - white space before the colon or inside the brackets;
  - no brackets.

  The other shapes:
  - Of two `Message-ID` headers, the first wins. Copies of one email are byte-equal there, so they
    agree.
  - A Message-ID only in the body is never read: two ID-less emails with the same body line keep
    two keys.
  - An encoded-word ID is hashed as written, so it is stable.
  - Letter case inside the ID is kept, so `<ABC@x>` and `<abc@x>` are two keys. Copies of one email
    never differ there, so this cannot inflate X.

  Distinct emails that share one Message-ID count once. That only lowers X, toward "behind".
- **R2-03a: CRLF vs LF in mbox.** go-mbox writes CRLF for every line it hands on (`reader.go:59,69`),
  so the two copies have one key (green part of `TestR2_03`).
- **R2-09: every Thunderbird store gets a result** (item 5), by tests and by reading.
  - `runFile` plans every local source. The mbox reader gives `planTB`. Any other local reader
    gives `planTBOther`, kept only under a Thunderbird profile. Only PST is excluded.
  - All local readers are `FaultWalker`s (`evolution.go:76`, `mboxreader.go:49`), so no local store
    skips the planning branch.
  - The defer appends every planned store whatever the error. A not-applicable result (POP3, Local
    Folders, a plain folder) is the only kind left out, and that is by design.
  - A whole and a partial walk of one store are told once, by the whole walk, so a failed whole
    walk gives `walk_failed`.
- **R2-10: `walkFailed`, by reading.** `runFile` has a named result `(err error)` (`run.go:1663`).
  - The closure at `:1753` is bound to it.
  - The `err :=` at `:1769` is in an inner block, but `return err` at `:1773` assigns the named
    result before the defer runs.
  - A mailbox read only in part is a fault, not an error, so it does not set `walkFailed`. It only
    lowers X, toward "behind".
- **R2-11: the 1,000-folder cap** (item 3). The account's verdict and `complete` come from the full
  result, never from the cut lists.
  - Each account's share is at least 15 (1,000 ÷ 64), and not-kept folders are placed first. So an
    account with any not-kept folder always shows at least one, with an exact `not_kept_more`. The
    builder's TestADV_C2_08 checks that every cut is counted.
  - Kept folders are listed largest gap first, so the folders that are cut have the smallest gaps,
    and `folders_more` says how many.
  - A reader cannot see a cut folder's state. It can tell that more exist. Nothing reads these lists
    today (C3 will).
- **R2-12: the streak** (item 4).
  - **Path-hash key:** two stores of one name keep separate streaks (round-1 test, both orders).
  - **Fresh start on a loose record:** a WARN in the record that `status` shows. On a drive that
    cannot keep files private, the streak never passes one run and the email never comes. This is
    disclosed in the WARN's own words. It is not silent and never gives a false notice.
  - **Newer format:** refused and never rewritten, with a WARN.
  - **A planted record** (a link, a pipe, a huge file) gives the WARN on every run. Anyone able to
    plant one can already write every record (round 1, ADV-C2-09).
  - **Not reproduced:** a way to fire the email falsely forever, beyond R2-05's false behind (once
    per streak, per the once-per-streak flags).
- **R2-13: the separator scan keeps step with go-mbox**, by reading `reader.go`. go-mbox starts a
  piece only at a line start that is not inside a long line (`atMiddleOfLine`). The scan marks
  every line start that begins "From ".
  - **Long lines** (over 4,096 bytes for go-mbox's buffer, or over 256 for the scan): go-mbox does
    split at a long "From " line, and the scan marks it "not a separator".
  - **The empty-line lookahead** applies to both.
  - **Leading empty lines** are skipped by both.
  - **CR handling:** both strip one CR before LF.

  The builder's `TestTheSeparatorScanIsReadSizeBlind` covers the one-byte reads.

## 4. Builder's guards

The packages touched by `1e10b7c` were run with the round-2 tests skipped (`-skip TestR2_`):
- `tbcomplete`, `state`, `source`, `thunderbird` and `health`: **ok**.
- `app`: hit Go's default 10-minute package limit while still in encrypted-archive and
  program-copy tests that `1e10b7c` does not touch. This was not an assertion failure; the builder's
  own run used `-timeout 60m`. The completeness tests in `app` were then rerun with `-timeout 30m`:
  - the builder's guards `TestHealthyInputsMatchTheGolden` (the P14 golden),
    `TestTheCheckChangesNothingArchived`, `TestAnEncryptedArchiveKeepsNoCompleteness`,
    `TestTheAccountsTypeDecidesNeverItsPath`, `TestAPartialInputIsUnknownAndNeverCompared`,
    `TestTheM6FactsComeFromTheRun`, `TestAFailedRunRecordsNoCompleteness`,
    `TestAStoreWhoseWalkFailedIsUnknown` and `TestALaterStreakRecordIsNeverDowngraded`;
  - every `ADV_C2` test, the builder's and round 1's.

  Result: **ok** (10.9 s).

**Not run here:** the unfiltered `go test ./...`. This pass changed no product code.

Round 1's 17 adapted tests are also green.

## 5. What the lead should decide

1. **R2-04 (MED)** breaks R42 with ordinary mail in stores written before escaping. The two cheap
   checks (only `From - ` separators, and `X-Mozilla-Status` in the piece's header) close the
   realistic case. They need lab row MA-1199 to confirm Thunderbird's separator before the release
   notes say the count is exact.
2. **R2-01 (LOW)** breaks R42 with crafted mail under conditions not checked. The one-line rule
   (an ID only from a header block that ends inside the bound) closes it.
3. **R2-05 and R2-08** are about the C5 email, not R42: a false "behind", and a silent refusal.
   Fold them before C5 sends anything.
4. **R2-06** must be settled before C3 reads accounts from the record.
5. **R2-03 and R2-07** are small. Fold them with the next C2 touch, or note them for C3.

## Rerun after fixes

- A fix to `mailboxcount.go`, `verdict.go` or `tbstreak.go` makes the matching lens stale.
- Rerun the three `zz_r2_test.go` files and the four `zz_r1_test.go` files from `.claude/attack-c2-r2`
  against the fixed commit.
- `TestR2_05` (source) and `TestR2_02` are guards already.
