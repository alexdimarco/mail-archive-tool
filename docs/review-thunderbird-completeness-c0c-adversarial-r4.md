# Adversarial pass, round 4: the C0c round-3 fixes (a short confirmation)

**Verdict: NOT CLEAN.** Five findings are CONFIRMED by failing tests: one MED and four LOW. Every
round-3 finding that was fixed holds (R3-01 to R3-05). Every earlier attack, from rounds 1, 2
and 3, passes when rerun against `1adcf64`.

The two findings that matter most are both gaps in the strict rule. Thunderbird never writes
either shape, so the strict rule is meant to refuse both. It accepts them instead:

- **R4-01.** A lone carriage return (CR) ends a `//` or `#` comment for Thunderbird, but not for
  MailArchive.
- **R4-02.** One escape shape, a `\u` high surrogate followed by a backslash, makes MailArchive
  and Thunderbird disagree about where a quoted value ends.

Either one can hide a statement from MailArchive that Thunderbird applies. So MailArchive can
still say "already on" while Thunderbird has the setting off, or report "Changed" while user.js
undoes the change. That is the failure the strict rule was built to end.

The MED finding is about behaviour, not safety. With `-enable-offline -sync-wait` and two accounts
in one profile, a person who follows the log's own "START Thunderbird" step can never get the
second account turned on.

| Field | Value |
|---|---|
| Reviewed | branch `tb-completeness`, tip `1adcf64` (the round-3 fix) |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` d57ab4b82f79c43dd1e79c4cb56235deee09a41347e52fd3992690be69c6071b; `internal/thunderbird/offline_words.go` 0e7110440d104deb3b4d998205b0eff2607395c2ef13b0dea3e0218960c09ed2; `cmd/mailarchive/main.go` e1d7a56b9ab1b8800b4ad5c750d898f316bc746c65177ee357642a48f2035fa1; `internal/wizard/wizard.go` bd27761da81c44ca4cfa2d673f4e8a683d50db192c40b05a7cc61facd0653b03 (unchanged since round 3) |
| Reviewer | One model instance, acting as both finder and skeptic. It did not write the fixes. Every CONFIRMED item has a test that fails against the real code. The lead should re-check them |
| Attack clone | `.claude/attack-c0c-r4` in the main checkout: a throwaway clone at `1adcf64`, safe to delete. It holds test files only, no product change. New: `internal/thunderbird/zz_r4_attack_test.go`, `internal/wizard/zz_r4_attack_test.go`, `cmd/mailarchive/zz_r4_attack_test.go`. Ported: round 1 (`zz_r1_port_test.go`, `zz_r1_port_ext_test.go`, `cmd/mailarchive/zz_r1_port_test.go`), and rounds 2 and 3 (the seven files from `.claude/attack-c0c-r3`) |
| Mozilla source | `modules/libpref/parser/src/lib.rs`, downloaded whole on 2026-11-08 from `raw.githubusercontent.com/mozilla/gecko-dev/master` and read line by line (not a summary this time). Copy at `.claude/mozsrc-r4/lib.rs`, sha256 f1bee3cdce55bfffba294145de2eb131f6b5afca98637fadd5a6ef15f44ae0c5. Not a pinned ESR copy |
| Tools | go1.26.0 darwin/arm64, macOS (APFS). Not run on Windows or Linux. The Windows path shapes were checked through `matchAccount(…, "windows")` |
| Commands | `go -C .claude/attack-c0c-r4 test -count=1 -run "TestR1\|TestR2\|TestR3" ./internal/thunderbird/ ./cmd/mailarchive/`: green. `… -run TestR4 ./internal/thunderbird/ ./internal/wizard/ ./cmd/mailarchive/`: exits 1, and the only reds are the five findings below. The builder's own tests beside the attack files (`-skip "TestR1\|TestR2\|TestR3\|TestR4"`) are green for `internal/thunderbird`, `internal/wizard` and `cmd/mailarchive` |

Only throwaway temp-folder profiles were used. No real Thunderbird profile and no MailArchive
settings folder was touched. No product code was changed.

**What Thunderbird applies** comes from `lib.rs`. These are the parts used here, quoted or
paraphrased with line numbers:

- The file header says an end of line is "`\n`", "`\r` (without subsequent `\n`)", or
  "`\r\n`" (lines 40-44).
- `match_single_line_comment`, used for both `//` and `#`, stops at `b'\n'` **or** `b'\r'`
  (lines 651-660).
- `get_string_token`, the `\u` escape (lines 906-931): after a high surrogate (`\uD800` to
  `\uDBFF`) it runs `if self.match_char(b'\\') && self.match_char(b'u')`. When the backslash
  matches and the `u` does not, the backslash has already been consumed and is not given back.
  The token becomes "expected low surrogate after high surrogate", and scanning goes on from the
  next character. So in `"\ud800\"`, the final `"` **closes** the string.
- After an error token, "Panic-mode" recovery skips tokens up to the next `;`, then parsing goes
  on (lines 575-595).
- Whitespace is `\t`, `\v`, `\f` and space (line 38). Keywords are case-sensitive: `true` and
  `false` only (lines 294-324). An integer above 2147483647 is a parse error (lines 432-440).

## 1. Findings

| ID | Claim | Severity | Verdict | Failing test |
|---|---|---|---|---|
| R4-01 | **A lone CR ends a comment for Thunderbird, not for MailArchive.** `parsePrefsText` splits lines on `\n` only, and `codeOf` drops everything after `//` or `#` to that line's end. Thunderbird ends the comment at a bare `\r`. Take prefs.js holding `user_pref("…server1.offline_download", true);`, then `// note⟨CR⟩user_pref("…offline_download", false);⟨LF⟩`. MailArchive sees only the `true`: `AccountToEnable` reads the account as on, and `EnableOffline` returns "already enabled", with no change and no error. Thunderbird applies the later `false`. The same happens with `#`, and in a file that uses CR-only line endings throughout. In user.js, `// set by IT⟨CR⟩user_pref(…, false);` hides the user.js line, so `EnableOffline` writes prefs.js and reports `Changed`, and Thunderbird undoes the change at its next start (the MA-1205 check is bypassed) | LOW. It needs a hand edit or another program's edit. Thunderbird writes LF or CRLF, never a bare CR. But it defeats the strict rule's own claim, the same way R3-01 did, which the lead fixed before merge | CONFIRMED | `TestR4LoneCREndsAComment` (4 subtests: `//`, `#`, a CR-only file, user.js) |
| R4-02 | **The surrogate escape moves the end of a quoted value.** `quoted()` and `codeOf` treat every backslash as "skip the next character", but Thunderbird's `\u` high-surrogate path consumes one backslash without its partner. Take prefs.js holding `user_pref(K, true);` and then `user_pref("mail.zz", "\ud800\"); user_pref('K', false); //");`. MailArchive reads one whole `mail.zz` statement, so K is on: "already on". Thunderbird ends `mail.zz`'s value at the quote after `\ud800\`, recovers at the `;`, and applies `user_pref('K', false);` (single quotes are allowed), and the `//` comments out the rest. The other way round also works: with `"\ud800\\"`, MailArchive closes the string and reads a `user_pref(K, true);` after it, while Thunderbird's string runs on, so that statement is never applied. In user.js, the first form hides a `false` from the user.js check, and `EnableOffline` reports `Changed` | LOW. It needs a hand-crafted value. Thunderbird writes no lone surrogate. Same consequence as R4-01 | CONFIRMED | `TestR4SurrogateEscapeHidesAStatement` (3 subtests) |
| R4-03 | **"Directly in ImapMail/" counts a backslash as a separator on macOS and Linux.** `direct` (`thunderbird.go:533`) compares `components(store)`, and `components` turns every `\` into `/` whatever the OS. On macOS or Linux, a single folder named `ImapMail\imap.gmail.com` sitting in the profile folder itself, not under `ImapMail/`, passes the test, and `AccountToEnable` gives it server1 by hostname | LOW. The folder has to be made on purpose inside the profile, and a person has to point MailArchive at it. Same harm as R3-05 (another account's setting changed, a false log line), far less likely | CONFIRMED | `TestR4BackslashInAFolderNameIsNotImapMail` |
| R4-04 | **`-sync-wait` with two accounts never finishes when the person follows the log.** In `prepareThunderbird`, the changes and the waits take turns, one store at a time. Store A is changed, then the log says "Next: START Thunderbird, then right-click the account -> Download/Sync Now." and `waitForSync(A)` waits. A person who does that has Thunderbird running when the loop reaches store B, and B's check refuses: "Thunderbird looks like it is running … Close it, then run the same command again". The MA-1263 words are true here ("Before it stopped, it changed prefs.js … for imap.gmail.com …"). On the rerun, A is already on, the log again says START Thunderbird and waits, and B is refused again. Following the steps, the second account is never turned on, and nothing is ever exported. Round 3 called this case "a race". With `-sync-wait` it is not a race: it is the guided path | MED, for behaviour, not safety. Nothing is written wrongly, every word is true, and the exit code is 1. But the feature's own multi-account path cannot finish. The way out, which no message gives: press Enter at the wait without starting Thunderbird, or run without `-sync-wait` | CONFIRMED | `cmd/mailarchive` `TestR4SyncWaitStartsThunderbirdBeforeTheNextStoreIsChanged` (Thunderbird's running state is injected through the builder's `tbRunning` seam; it turns true once the log has said "START Thunderbird") |
| R4-05 | **The wizard's second refusal still says "did not change any Thunderbird setting" after a change.** MA-1263 fixed the command line only. The wizard calls `prepareThunderbirdGUI` once per input (`wizard.go:851-867`), and an auto run defaults to "All of them". When input 1's "Yes, prepare" changed prefs.js and input 2 is refused, input 2's warning says "MailArchive could not find a Thunderbird account for the mail in imap.old-provider.example, so it did not change any Thunderbird setting." The common case is a removed account's leftover folder. The sentence is false for this run | LOW. Wording: the person did see the first change go through | CONFIRMED in substance. The zenity dialogs have no test seam, so the test replays the loop's two real calls in order: `EnableOffline` for input 1, then `imapPrepAccount` for input 2 | `internal/wizard` `TestR4WizardSecondRefusalAfterAChange` |

### Suggested directions (the builder decides)

- **R4-01:** refuse (`HandEditedError`) any `\r` that is not directly followed by `\n`, in
  either file. Thunderbird writes a CR inside a value as the escape `\r`, never as a raw byte,
  so this refuses only files changed outside Thunderbird. It is one more check in the
  `parsePrefsText` loop, beside the NUL check.
- **R4-02:** refuse any quoted value whose escapes are not ones MailArchive reads exactly as
  `lib.rs` does. The smallest rule that closes this finding: refuse a `\u` escape in the range
  `D800` to `DFFF`. A wider and simpler rule: refuse every `\u` and `\x` escape. Thunderbird writes
  non-ASCII as raw UTF-8, which the round-3 held check "an escape code (backslash-u-00e9) in another
  value" would then refuse. That needs a look at what Thunderbird's writer actually emits.
  `Preferences.cpp` (`StrEscape`) was not read in this pass.
- **R4-03:** compute `direct` natively, by comparing `filepath.Dir(store)` with
  `filepath.Join(profile, "ImapMail")` using the case rule. Or count `\` as a separator only when
  `goos == "windows"`.
- **R4-04:** with `-sync-wait`, make every store's change first and then do the waits. Or check
  Thunderbird's running state only for a store that still needs a change, and say "Changes are
  done; now start Thunderbird" once. Either way the multi-account path finishes in one run.
- **R4-05:** carry "this run already changed X" across the wizard's input loop, the way
  `stoppedAfterChange` does on the command line. Or word the refusal as "did not change a setting
  for this mail".

### The failing tests (from the attack clone, in substance)

```go
// R4-01 (internal/thunderbird)
prefs := r4Base() + `user_pref("`+offKey+`", true);` + "\n" + "// note\r" + `user_pref("`+offKey+`", false);` + "\n"
// red: AccountToEnable → OfflineDownload true; EnableOffline → {Changed:false}, nil
writeAt(t, user.js, "// set by IT\r"+`user_pref("`+offKey+`", false);`+"\n") // red: EnableOffline → Changed

// R4-02
hidden := `user_pref("mail.zz", "\ud800\"); user_pref('` + offKey + `', false); //");` + "\n"
// after an on line in prefs.js: red, already on; in user.js: red, Changed
line := `user_pref("mail.zz", "\ud800\\"); user_pref("` + offKey + `", true);` + "\n" // after an off line: red, already on

// R4-03
odd := filepath.Join(p, `ImapMail\imap.gmail.com`) // one folder in the profile folder
AccountToEnable(p, odd)                              // red: server1 by hostname

// R4-04 (cmd/mailarchive): one profile, server1 + server2 both off, -enable-offline -sync-wait,
// tbRunning true once the log says "START Thunderbird"; two runs; red: server2 still off,
// last error "Thunderbird looks like it is running … Close it, then run the same command again"

// R4-05 (internal/wizard)
thunderbird.EnableOffline(profile, "server1")  // input 1 changed
_, _, refusal := imapPrepAccount(leftover)    // red: "... so it did not change any Thunderbird setting."
```

## 2. Held by test (green in the clone)

| Attack | Test | Result |
|---|---|---|
| **The round-3 findings, rerun:** R3-01 (`pref(` and `sticky_pref(` in either file), R3-02 (a backslash in a name), R3-03 (a NUL; an unfinished last statement), R3-04 (a value running across lines), R3-05 (a copied profile without its marker, in the package and end to end) | `TestR3*` (ported unchanged) | HELD: each one is now refused, with both files byte for byte |
| **Every round-3 held check** (compatibility.ini shapes, planted files, the reader's held shapes, check-then-write) and **every round-2 attack and held check**, with the round-1 attacks round 2 had ported | all `TestR2*`, `TestR3*` | HELD: no regression |
| **Round 1 rerun directly** (`.claude/attack-c0c`): A1 ghost server, A2 two servers on one host, A4 duplicate off lines, A5 a commented-on line, A6 the backup kept, A7 CRLF kept, A8 the user.js readers agree, H1 only the one setting changes (hard link, mode 0600, no temp file), H2 the exact 16 MB bound, H3 case twins, A3 a mail folder named prefs.js, A9 the ambiguity reason. Port changes: the `EnableOffline` signature; fixtures given a live accounts list (the CON-01 rule); A8 now expects the `UserJSError` (CON-06); A9 reads the refusal's words instead of a log line | `TestR1*` | HELD |
| **The strict rule on odd spellings, all refused with both files unchanged and no backup:** `True` in user.js, `TRUE` in prefs.js, a BOM before the first statement, a no-break space before `;`, an escape in a name in user.js, a statement split by a `//` comment, a raw newline in user.js's value, `, locked` in user.js, a NUL inside a value, a backslash at a value's line end | `TestR4StrictReadingHeld` (10 subtests) | HELD |
| **Values Thunderbird does not read as true** (`1`, `"true"`, `'true'`, `+0`): never "already on". In prefs.js the line is rewritten to `true`. In user.js it is a `UserJSError` | `TestR4StrictReadingHeld` (8 subtests) | HELD |
| `\v`, `\f`, a CR inside a line and a `/* */` comment between tokens: read as Thunderbird reads them (the later off line counts) | `TestR4StrictReadingHeld/vertical tab…` | HELD |
| The same name in both files: user.js on over prefs.js off is "already on" (from user.js). user.js off over prefs.js on is a `UserJSError` | `TestR4StrictReadingHeld/user.js …` (2) | HELD |
| A 15 MB value on its own line, near the 16 MB cap: read, the one line appended, every other byte kept | `TestR4StrictReadingHeld/a 15 MB value…` | HELD |
| **The hostname fallback on other path shapes:** on Windows, direct; a trailing backslash; other case (`IMAPMAIL`); forward slashes; `Mail\..\ImapMail`; a UNC profile: found. A copy under `Mail\` or `ImapMail\`, and `Mail\imap.gmail.com`: none. On macOS, `imapmail/` (other case): found. On Linux, `imapmail/` (another folder there): none. The profile folder itself named like the host: none | `TestR4HostnameFallbackPaths` (12 subtests) | HELD |

## 3. Held by reasoning

- **Integer overflow** (`99999999999`). MailArchive accepts it, and Thunderbird drops the
  statement. For the offline setting, a dropped integer can never be read as "already on".
  MailArchive only rewrites it (in prefs.js) or refuses it (`UserJSError` in user.js). The
  settings the match reads are all strings. Not exploitable.
- **Other bad escapes** (`\q`, a short `\x4`, `\u12`, `\x00`, `\u0000`). Thunderbird marks the
  token as an error but scans on to the same closing quote MailArchive finds. So only that one
  statement is lost, and it is never the offline statement, whose value is bare `true` or
  `false`. A lost `directory-rel` or `hostname` makes MailArchive's match differ from
  Thunderbird's, but it does not give MailArchive a false "on". The surrogate path (R4-02) is the
  one escape that moves the end of the string.
- **R4-01 and R4-02 can also make MailArchive change the wrong account.** A
  `mail.server.server2.directory-rel` that only Thunderbird reads would make Thunderbird's owner
  of the folder server2, while MailArchive picks server1 by hostname. One shape per finding was
  tested, and the fix for each closes this case too.
- **The MA-1263 words on the command line.** `stoppedAfterChange` strips the "changed nothing"
  opening from every refusal type (`cliNothing` opens `NoAccountError`, `HandEditedError`,
  `UserJSError`, the running check and the wrapped file refusals). The wrapped text holds no
  second "changed nothing". A failed write (`write prefs.js: …`, `back up prefs.js: …`) has no
  such opening. A Ctrl-C during a later store's wait gives "Why it stopped: context canceled",
  which is true. A backup written just before a failed prefs.js write is not reported as a
  change. That is true of prefs.js, and the backup is only an extra file. No command-line path
  was found that says "changed nothing" after a change.
- **Case-blind disks on Linux** (exFAT, or a case-folded ext4 folder). `sameComponent` is exact
  on Linux, so `imapmail/<host>` gets no hostname fallback, a refusal. That errs on the safe side.

## 4. What this pass did not do

It was not run on Windows or Linux, and nothing ran against a real Thunderbird. The Mozilla rules
come from `lib.rs` on gecko-dev master, read in full, but not from a pinned ESR copy.
`Preferences.cpp` (whether a BOM is stripped before parsing, and what `StrEscape` writes) was not
read. The wizard finding replays the loop's calls, because no dialog was driven. The smallest
checks:

- **R4-01:** in a throwaway profile, add `// x⟨CR⟩user_pref("mail.server.serverN.offline_download", false);` after a `true` line, start Thunderbird, and look at the account's offline checkbox.
- **R4-02:** do the same with the `\ud800\"` line.

This was a narrow confirmation pass, not a full adversarial pass. Severity is my own judgement,
and the lead decides what blocks the merge. My recommendation: fix R4-01 and R4-02 before merge,
for the same reason R3-01 was fixed. Each one is a few lines in the reader, and each one makes
the strict rule's claim true again. Decide R4-04 before shipping `-sync-wait` for profiles with
more than one account. R4-03 and R4-05 are small.

---
Register: finding (an adversarial confirmation report). Drift: none noticed. Touched: this review
file in the worktree; in the throwaway clone `.claude/attack-c0c-r4`, three new test files and
ten ported ones; `.claude/mozsrc-r4/lib.rs` (downloaded source, read only); no product code.
Persona: none. Source reached: `thunderbird.go` (the reader, the match, `planOffline`,
`EnableOffline`), `offline_words.go`, `main.go` (`prepareThunderbird`, `stoppedAfterChange`,
`prepareStoreNoting`, `checkOfflineStore`, `waitForSync`), `wizard.go` (step 1a's loop,
`prepareThunderbirdGUI`, `imapPrepAccount`, `pickAutoInputs`), the round-3 review, and the design
§4.1 round-3 note, all at `1adcf64`; Mozilla `lib.rs` (master, read in full); `Preferences.cpp`
(not reached).
