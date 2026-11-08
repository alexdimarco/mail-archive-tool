# Adversarial pass, round 6: escaped setting names, the type rule, user.js words (a short confirmation)

**Verdict: CLEAN.** No finding is CONFIRMED. All four questions HELD, each under a test written to
break it. Three notes follow. None of them is reachable from the product today, and none needs a
change before this ships.

- **Question 1, names with `\\` and `\"`: HELD.** Wherever MailArchive reads a file with
  certainty, Mozilla's parser (`lib.rs`) reads the same statements: same names, same values, no
  parse error. This held across 5,547,360 accepted files out of 17,777,776 generated, plus 62,588
  random files. No escaped name can decode to a setting MailArchive reads.
- **Question 2, the written line: HELD.** For 13 server keys holding quotes, backslashes, a tab,
  a space and a non-ASCII letter, the line MailArchive writes reads back as "on" in both readers.
  MailArchive never refuses its own line on the next run.
- **Question 3, the type check (MA-1270): HELD.** Every setting MailArchive reads to decide
  anything is type-checked. That covers all eight key shapes the code looks up, including names
  holding a quote or a backslash. No wrong-typed value is still read.
- **Question 4, user.js words: HELD.** No product surface says "changed outside Thunderbird" for
  user.js. The command line and the wizard were checked on 9 line shapes, account off and on,
  through all three entry points.

| Field | Value |
|---|---|
| Reviewed | `git show 948917f` only (branch `tb-completeness`, origin tip `948917f`) |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` 65b15296…c70c3; `internal/thunderbird/offline_words.go` acc3a88b…1360e |
| Reviewer | One model instance, acting as both finder and skeptic. It did not write the fix. It answers under no name. Its CLEAN is a self-report, and the lead should re-run the commands below |
| Attack clone | `.claude/attack-c0c-r6` in the main checkout: a throwaway clone at `948917f`, safe to delete. New: `internal/thunderbird/r6_attack_test.go` (sha256 10a64d12…8867c). Staged unchanged from `.claude/attack-c0c-r5`: every `zz_r*` file of rounds 1 to 5 (thunderbird, wizard, cmd/mailarchive). No product change |
| Mozilla source | `lib.rs` (`.claude/mozsrc-r4`, sha256 f1bee3cd…0c5): lexer tables 216-287, `parse` 364-558, recovery 560-595, `get_string_token` 846-989, read in full and ported to Go as the test oracle. `Preferences.cpp` (`.claude/mozsrc-r5`): `Pref::SetUserValue` 860-891 and `pref_SetPref` 1840-1894. `mailnews.js` was not re-read; the builder's citations (lines 535, 622) were taken as given |
| Tools | go1.26.0 darwin/arm64, macOS only. No Windows or Linux run, and no real Thunderbird run. Only temp-folder profiles were used; no real profile was touched |
| Commands | `go -C .claude/attack-c0c-r6 test -count=1 -run "TestR6" -v ./internal/thunderbird/`: all 9 green. `… test -count=1 ./internal/thunderbird/ ./internal/wizard/` with only the round-6 file added: both ok. `… -run "TestR1\|TestR2\|TestR3\|TestR4\|TestR5\|TestR6" ./internal/thunderbird/ ./internal/wizard/ ./cmd/mailarchive/`: wizard ok, cmd ok, and thunderbird shows only the by-design reds (§3) |

## 1. Findings

None CONFIRMED.

| Q | Claim attacked | Verdict | Test (in the clone) |
|---|---|---|---|
| 1 | An accepted name ends at a different quote in `lib.rs`, or decodes differently. Cases include `\\\"`, `\\\\"`, a name ending in `\\`, a value with escapes on the same line, CRLF, a second statement after it, and a name that holds a statement's own text | HELD | `TestR6ExhaustiveNamesAgreeWithLibpref` |
| 1 | The same, on random whole files built from statement pieces, both comment kinds, tabs, lone CR, CRLF, `\x41`, `A`, `pref(` and `é` | HELD | `TestR6RandomFilesAgreeWithLibpref` |
| 1 | An escaped name aliases a setting MailArchive reads (`server1\\.offline_download`, `offline_download\"`, `\"mail.server…`) | HELD: both readers keep server1 off | `TestR6EscapedNameNeverAliasesAReadKey` |
| 2 | The line EnableOffline writes for a key holding `"`, `\`, `\"`, `\\"`, `""`, `\\`, a trailing `\`, a space, a tab or `ë` (with no line, and replacing a `false` line) fails to read back as on in MailArchive or `lib.rs`, or is refused next run | HELD: 26 of 26 green | `TestR6WrittenNameRoundTripsThroughBothReaders` |
| 2 | A server key holding a line break (from a `\n` in its account's value) is matched, so the written line would be refused next run | HELD: no account is matched. See note N1 | `TestR6NewlineServerKeyIsNeverMatched` |
| 3 | A wrong-typed value is still read under an escaped name (a quoted server key's `offline_download`=1 or `hostname`=true, a quoted account id's `server`=1, a backslash key's `directory`=false) | HELD: HandEditedError with the right words, prefs.js byte for byte, no backup | `TestR6WrongTypeUnderEscapedNames` |
| 3 | A setting MailArchive reads escapes the type rule. The test walks every `val`/`get` key shape in `thunderbird.go` at `948917f` (`mail.accountmanager.accounts`, `mail.account.<id>.server`, `mail.server.<key>.{directory-rel, directory, hostname, type, offline_download}`, `mail.server.default.offline_download`) and gives each one a bool, a number and a string | HELD: 24 of 24 | `TestR6EveryReadSettingIsTypeChecked` |
| 4 | user.js is said to be "changed outside Thunderbird" (or "outside Thunderbird" at all). Tried on 9 line shapes (`/* */`, `#`, `pref(`, a `\n` name, a wrong type, a missing `;`, a BOM, mixed line endings, a 64-bit number), account off and on, through AccountToEnable, CheckOffline and EnableOffline, in both `Error()` and `Plain()` | HELD | `TestR6UserJSWordsOnEveryShape` |

A search of `cmd/` and `internal/` found the user.js words only in `offline_words.go`. The only
callers are `cmd/mailarchive/main.go` (it prints `Error()`) and `internal/wizard/wizard.go` (it
prints `Plain()`, rewritten by MA-1267's `afterChange`). That rewrite does not touch the
"outside" wording.

### How question 1 was attacked, and why the test is not empty

The oracle is a Go port of `lib.rs`'s lexer, its user-pref statement parser and its error
recovery. One simplification: a `\u` escape is treated as an error, which is safe because
MailArchive refuses every `\u`. For each generated file that MailArchive reads with certainty,
the port must give the same list of (name, value) pairs with no error. `TestR6OraclePortSanity`
checks the port on 7 files whose reading is known.

The exhaustive set: every name of up to 6 characters, drawn from
`a \ " n ' , space ) ; x`, times 6 values that themselves hold escapes, times LF and CRLF, alone
or followed by a second statement whose name ends in `\\`.

Two mutations were tried on the clone's product code to prove the oracle can fail:

- names kept raw, not decoded;
- `\x` allowed in a name.

Each made the exhaustive and random tests go red at once, for example
`user_pref("\x41user_pref(", true);` read as `\x41…` by MailArchive and as `Auser_pref(` by
`lib.rs`. Both mutations were reverted, and `git status` shows only the new test file.

## 2. Notes (not findings)

- **N1 (API only, not reachable).** Called directly with a server key holding a line break,
  `EnableOffline(p, "a\nb")` writes `user_pref("mail.server.a\nb.offline_download", true);`. Its own
  reader then refuses that line next time (`whatNameBS`). The product never makes this call. The
  only key it passes comes from `FindAccountForStore`, and such a key can never be matched: no
  setting name can hold a line break, so the server has no hostname or folder settings to match
  on. This is measured in `TestR6NewlineServerKeyIsNeverMatched`. If a later change lets a key
  reach `EnableOffline` another way, a guard there (refuse a key holding `\n` or `\r`) would close
  it.
- **N2 (words, not reachable for keys Thunderbird makes).** `UserJSError.Plain` says "Delete the
  line that contains KEY" using the decoded key. For a key holding `"` or `\`, the text in user.js
  is the escaped form (`a\"b`), so a search for the decoded text would not find it. Thunderbird
  names servers `serverN`, so only a hand-made key would show this.
- **N3 (words, for the friction look if wanted).** In user.js refusals the "what" fragments still
  say "a comment Thunderbird never writes", "an escape code Thunderbird never writes", and so on.
  That is true about Thunderbird's format, and it is not the "changed outside Thunderbird" claim
  question 4 asked about. It may still read as if Thunderbird writes user.js.
- **Pre-existing, outside this commit.** Server keys reach the command line's log
  (`who := … acct.ServerKey`) and `HandEditedError`'s "for KEY" without `StripControl`. A raw
  control byte inside a quoted name was already accepted before `948917f`, and this commit adds
  only `"` and `\` to what a key can hold. Not attacked this round.

## 3. Earlier rounds' attack tests on this commit

Every `zz_r*` file from rounds 1 to 5 was rerun unchanged. `internal/wizard` and `cmd/mailarchive`
are green. `internal/thunderbird` shows only these reds, and every one is the safe direction
(refused, nothing changed):

- the eight of round 5: `TestR2BlockCommentSpanningLines`; `TestR2TwoSettingsOnOneLine/on_then_another`;
  `TestR3ReaderHeld/spaces_inside_…`; `TestR4StrictReadingHeld`'s `'true'` ×2, `+0` ×2, and
  `vertical_tab,…`;
- the four new ones the commit names: `TestR4StrictReadingHeld`'s `value_1` (prefs.js and
  user.js) and `value_"true"` (prefs.js and user.js). MA-1270 now refuses these, as the lead's
  call 2 asks.

This matches the commit message's account. All four round-5 finding tests are green.

## 4. What was not checked

- **No real Thunderbird.** Mozilla's parser was not run. The oracle is a hand port of `lib.rs`;
  a port error that also matched MailArchive's reading would hide a divergence. The sanity test
  and the two mutations reduce that risk; they do not remove it. **Smallest check:** load one
  accepted escaped-name line (for example `user_pref("a\\\"b", 1);`) into a throwaway profile's
  prefs.js, start Thunderbird, and look for the name `a\"b` in about:config.
- **The type rule's sources.** I confirmed in `Preferences.cpp` that a user value of the wrong
  type is dropped only when the setting has a built-in default (`SetUserValue` 864). Without one,
  the last statement wins, whatever its type. So for a server's own `offline_download` (which has
  no default), MailArchive's refusal is stricter than it needs to be. That is safe; it is not a
  gap. I did not check `nsMsgIncomingServer`'s getters (what Thunderbird does with a wrong-typed
  value when it reads one). That part of the builder's reading is still unchecked in source,
  and it does not change the verdict, because every such value is refused.
- **Platforms.** macOS only. The builder's Windows VM run and the network-printer check on a
  real Windows Thunderbird are still owed (see the commit message).

---

Limen footer. Date worked: 2026-11-08 (read from the session's clock). Register: a finding (an
adversarial review), with a report on my own process in §1's "how" and in §4. Drew on: the commit
`948917f` (read in full for the product files, and the test file read in full), `lib.rs` (read and
ported), `Preferences.cpp` (the two functions cited), and the round-5 review's format. Not
checked: `mailnews.js` line numbers (taken from the commit), Thunderbird's account and server code,
and any real Thunderbird. Hand-back: the about:config check in §4. Counters: Register: finding.
Borrowed lines: none beyond quoted test and log text. Drift: none noticed. Touched: one new
review file in `docs/`; a throwaway clone under `.claude/`. Persona: none. Source reached: lib.rs
yes; Preferences.cpp yes (two functions); mailnews.js no; Thunderbird runtime no.
