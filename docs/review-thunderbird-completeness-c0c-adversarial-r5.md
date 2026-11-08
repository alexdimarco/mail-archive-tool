# Adversarial pass, round 5: the C0c allow-list reader (a short confirmation)

**Verdict: NOT CLEAN.** Two findings are CONFIRMED by failing tests: one MED and one LOW.

- **R5-02 (MED).** Thunderbird itself writes a setting name with `\\` in it on Windows, after a
  person prints to a network printer (`\\SERVER\Printer`). The allow-list refuses every
  backslash in a name, so that profile is told it was "changed outside Thunderbird". That is
  false. Nothing is written, so this is the safe direction, but it blocks `-enable-offline` and
  "Yes, prepare" on a common office setup, on Windows.
- **R5-01 (LOW).** Thunderbird drops a statement whose value has the wrong type for a setting
  that has a built-in default. MailArchive takes it, because the last statement wins. Thunderbird
  never writes such a statement. It needs a hand edit.

On question 1, the lexer, the allow-list holds. I found no accepted byte sequence on which
Mozilla's parser (`lib.rs`) ends a string, comment, line or statement at a different place, or
reads a different name or value. The gap in R5-01 is one level up. It is not in `lib.rs`: it is
in `Preferences.cpp`'s type rule. The fixes for R4-03, R4-04 and R4-05 hold under one new attack
each. Every finding test from rounds 1 to 4 is green. The only exceptions are the by-design
reds (one top-level test and seven subtests, as the commit lists them; see §3), and all of
them are the safe direction.

| Field | Value |
|---|---|
| Reviewed | branch `tb-completeness`, tip `ab3c572` (origin, same) |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` d84dfd9c2b922208c3637d307325234206bd70e539899db9b29c2ce47de3d6cf; `internal/thunderbird/offline_words.go` 1c5ea2763d07b74422d8ecdebd4c3915e01738a4b99fd9435e509892dbe40a9c; `cmd/mailarchive/main.go` 353ec5130c9c78c37af4aba239ed48d705554517ee9a1d0d76f45f4afd46d787; `internal/wizard/wizard.go` d38a8ab72098bf620c134070b11749ea84d7b3c4013a6ccc2ec1ab94facb97c9 |
| Reviewer | One model instance, acting as both finder and skeptic. It did not write the fix. Every CONFIRMED item has a test that fails against the real code. The lead should re-check them |
| Attack clone | `.claude/attack-c0c-r5` in the main checkout: a throwaway clone at `ab3c572`, safe to delete. Test files only, no product change. New: `internal/thunderbird/zz_r5_attack_test.go`, `internal/wizard/zz_r5_attack_test.go`, `cmd/mailarchive/zz_r5_attack_test.go`. Ported unchanged from `.claude/attack-c0c-r4`: the rounds 1 to 4 files. One port was adapted for its signature only: `internal/wizard/zz_r4_attack_test.go` now passes `imapPrepAccount`'s new `changed` argument |
| Mozilla source | `lib.rs` (the copy at `.claude/mozsrc-r4`, read again in full for its lexer). `Preferences.cpp` (the copy at `.claude/mozsrc-r5`, sha256 196973de…, the same file the builder cites). `nsMsgTagService.cpp` (`.claude/mozsrc-r5`). Downloaded today, each to `.claude/mozsrc-r5b`: comm-central tip `mailnews/mailnews.js` (41ebf6b1…), and from firefox `main`, `widget/nsPrintSettingsService.cpp` (df696522…) and `modules/libpref/init/all.js` (34eca713…). None is a pinned ESR copy |
| Tools | go1.26.0 darwin/arm64, macOS. Not run on Windows or Linux. Windows path shapes were checked through `matchAccount(…, "windows")`. No real Thunderbird was run |
| Commands | `go -C .claude/attack-c0c-r5 test -count=1 ./internal/thunderbird/ ./internal/wizard/ ./cmd/mailarchive/`. `cmd/mailarchive`: ok. `internal/wizard`: ok. `internal/thunderbird`: exits 1, and the only reds are the eight by-design reds (§3) and the four R5 tests below. The builder's own tests (`-skip "TestR1\|…\|TestR5"`) are green for `internal/thunderbird` |

Only throwaway temp-folder profiles were used. No real profile was touched, and no product code
was changed.

## 1. Findings

| ID | Claim | Severity | Verdict | Failing test |
|---|---|---|---|---|
| R5-02 | **A name that Thunderbird writes with `\\` is refused.** `GetAdjustedPrinterName` (nsPrintSettingsService.cpp:839-864) replaces only the space, `\n` and `\r` in a printer's name with `_`. It uses the result as the prefix of `print.printer_<name>.*`, and Thunderbird saves those settings when someone prints (`print.save_print_settings` is `true` in all.js:736). A Windows network printer is named `\\PRINTSRV\HP LaserJet 4`. `pref_savePrefs` runs `StrEscape` on the NAME (Preferences.cpp:1744), and that writes `user_pref("print.printer_\\\\PRINTSRV\\HP_LaserJet_4.print_bgcolor", false);`. `allowedLine` refuses any backslash in a name (`whatNameBS`). So `-enable-offline` exits 1 and "Yes, prepare" warns. Both say "prefs.js line 6 has a setting name with a backslash … Thunderbird never writes such a line, so the file was changed outside Thunderbird". That is false | MED. Nothing is written, so it is safe. But it falsely refuses a profile exactly as Thunderbird left it, on Windows (the biggest user group) and on an ordinary office setup, and the person is accused of a hand edit. The words still give the by-hand steps. **Derived from source, not seen on a real Windows Thunderbird** (see §5) | CONFIRMED | `TestR5NetworkPrinterNameIsNotHandEdited` |
| R5-01 | **A statement that Thunderbird rejects for its type is read.** `Pref::SetUserValue` (Preferences.cpp:862-866): "If we have a default value, types must match when setting the user value", else `NS_ERROR_UNEXPECTED` and "Rejected attempt to change type of pref" (1884-1893). prefs.js and user.js load with `fromInit=true` (2028-2030). `parsePrefsText().values()` keeps the last statement whatever its type. mailnews.js gives two of the settings the match reads a default: `pref("mail.server.default.offline_download",true)` (line 622) and `pref("mail.accountmanager.accounts", "")` (line 535). Three cases. (a) prefs.js holds `…default.offline_download", false);` and then `…", 0);`. Thunderbird applies false, but MailArchive reads `OnByDefault=true`. (b) The same, with the second statement as a string in user.js. (c) `user_pref("mail.accountmanager.accounts", true);` together with a made-up `mail.account.true.server` line. Thunderbird's live list stays `account1`, but MailArchive's live list becomes `true` → server2. A leftover folder of a removed account then reads as owned and on (`OfflineDownload=true`): the "already on" path, with nothing changed. That is round 1's CON-01 class again | LOW. Thunderbird never writes a wrong-typed value. Each case needs a hand edit, and (c) needs two invented lines. In (a) and (b) only the words are wrong. The log says "was already on … written into prefs.js to make sure" (main.go:2246), but true is written anyway, so the end state is right. Case (c) is a false "already on". This is the same class as R4-01 and R4-02 (LOW): a shape the allow-list exists to refuse | CONFIRMED | `TestR5RetypedDefaultIsNotOnByDefault`, `TestR5RetypedDefaultInUserJSIsNotOnByDefault`, `TestR5RetypedAccountListIsNotRead` |

### Suggested directions (the builder decides)

- **R5-02.** In a name, allow `\\` and `\"`, decoded the same way as in a value. `StrEscape`
  writes exactly those for a name. `lib.rs` decodes them identically: one character after the
  backslash, never the closing quote. So every argument the reader already makes for values
  carries over to names. Keep refusing `\n`, `\r`, `\x` and `\u` in a name, unless the lead
  wants `\n` and `\r` too: `StrEscape` writes them, but no name holds them in practice. Positive
  twin: the printer prefs.js above reads and `EnableOffline` changes it.
- **R5-01.** A short type rule for the settings the match reads. Refuse (HandEditedError) any
  statement where (1) `mail.server.*.offline_download` (including `default`) is not `true` or
  `false`, or (2) `mail.accountmanager.accounts`, `mail.account.*.server` or
  `mail.server.*.{hostname,type,directory,directory-rel}` is not a quoted string. Thunderbird
  never writes another type for these, so the rule cannot refuse a real profile. A wider rule
  would also work: refuse any name that is set twice with two value types. But it is wider than
  needed.

## 2. Question 1: is there any accepted byte sequence on which lib.rs differs? HELD

Held by test (`TestR5AllowListAgreesWithLibpref`, 25 subtests, green). Each case was checked
against my reading of `lib.rs`.

- **Names** holding a space, `/`, `*`, `;`, `(`, `)`, `,`, `//`, `/*` or `'`. In a `"` string,
  `lib.rs` treats only the closing quote, `\`, LF, CR and NUL as special (`get_string_token`,
  850-858, 879-985). The other quote character is pushed as it is (982-983). MailArchive reads
  the same key.
- **Invalid UTF-8** (`\xff\xfe` in a name, a lone `\xc3` in a value). `lib.rs` works on bytes and
  never checks UTF-8. `Preferences.cpp` stores the name as a byte string. MailArchive reads the
  same bytes.
- **A raw tab, UTF-8 or VT in a value** is read as itself.
- **Escapes.** `\\n` is a backslash then `n`. A value ending `\\"` closes at that quote. `x\\\"y`
  decodes to `x\"y`. A statement's text inside a value (`"a\"); user_pref(\"x\", true); //"`)
  is one value. `\x41` is refused.
- **Integers.** -2147483648 and 2147483647 are accepted. `lib.rs` takes `-` then
  `Int(2147483648)` and gives `i32::MIN` (453-459). 2147483648, -2147483649 and 4294967296 are
  refused. `-0` and `007` are read: `lib.rs` gives 0 and 7, MailArchive keeps the text. No
  setting the match reads is an integer, so this changes nothing.
- **Lines.** A `//` comment holding `"` and `/*` ends at its LF, and the next line is read. A
  statement followed by a `//` comment, or with a space before `;`, is refused. A CRLF file whose
  last line has no newline is read. So is an LF one. A CRLF file ending in a lone CR is refused.
  An empty file and a comments-only file read with certainty and hold no statements (their match
  then gives "lists no account"). A NUL and an empty name are refused.
- **Held by reasoning.** A BOM is refused, and `lib.rs` would error on it and drop the first
  statement (`CharKind::Other`, 832). `pref(`, `sticky_pref(`, `#`, `/*`, single quotes, tabs and
  other spacing are all refused. In the CRLF mode, `parsePrefsText` strips one CR per line and
  refuses any other. In the LF mode it refuses every CR. So `lib.rs`'s CR handling (656-659,
  826-829, 969-976) never sees a CR that MailArchive did not account for.
  `planOffline` rewrites a line by `strings.Trim(line, " \r")`, which is exactly the statement,
  because the allow-list accepts only spaces around it.

## 3. Questions 2 and 3: what Thunderbird writes, and the by-design reds

**Question 2, refused although Thunderbird writes it.** One shape: R5-02, a name holding `\` or
`"`. The rest is held, by reading the writer. `kPrefFileHeader` is all `//` lines and blank lines
(3157-3177). Each statement is `user_pref(%s, %s);` (1746), joined with `NS_LINEBREAK`, which is
CRLF on Windows and LF elsewhere and the same through the whole file (3225-3230). Booleans are
`true`/`false`, integers are `%d`, and strings go through `StrEscape` (`\n`, `\r`, `\\`, `\"`,
everything else raw). All of that is accepted. Tag keys (`nsMsgTagService.cpp` 245-266) leave out
`"` and `\` (they become `=22` and `=5c`), and `$label1` and `=20` pass the wider name rule.
Not checked: names that add-ons write, which could hold a `\` or `"` for the same reason as
printers. R5-02's fix covers them too.

**Question 3, the by-design reds.** All of them are the safe direction. Each is refused
with a HandEditedError from `planOffline` (thunderbird.go:813) or `matchAccountIn` (519). Both
come before `backupOnce` and `writeOverRegular`, so nothing is written. The tests also show
`Changed:false`. None is a shape Thunderbird writes:

1. `TestR2BlockCommentSpanningLines`: a `/* */` comment.
2. `TestR2TwoSettingsOnOneLine/on then another`: two statements on one line.
3. `TestR3ReaderHeld/spaces inside…`: a `/* */` block, a `#` line, a tab, spaces inside the
   statement, and a `\u00e9` escape. `StrEscape` writes é raw.
4. and 5. `TestR4StrictReadingHeld/'true'`, in prefs.js and in user.js: single quotes.
6. and 7. `TestR4StrictReadingHeld/+0`, in prefs.js and in user.js: Thunderbird writes `%d`,
   which never has a `+`.

8. `TestR4StrictReadingHeld/vertical tab, form feed, CR and a block comment`: a lone CR, `\v`,
   `\f` and a `/* */` between tokens.

So the four checks hold eight reds: one top-level test and seven subtests. The commit says "7
subtests", which leaves out the top-level `TestR2BlockCommentSpanningLines`, so the two counts
agree. Each refuses as designed.

**A note, not a finding (N1, wording, for the lead).** Thunderbird never writes user.js. People
and other programs write it, for example from a privacy guide, and those files usually hold
`/* */` blocks and statements followed by `//` comments. All of those now refuse. That is
correct for safety. But the words "the file was changed outside Thunderbird" are odd for a file
that is never Thunderbird's. Consider saying for user.js that "MailArchive reads only lines in
Thunderbird's own form".

## 4. Question 4: spot checks of R4-03, R4-04 and R4-05, one new attack each. HELD

- **R4-03** (`TestR5BackslashInProfilePath`, 5 subtests, green). On Linux, a profile under a
  folder named `a\b` matches by `directory-rel`, by hostname, and by `directory` written as
  Thunderbird writes it (`/home/a\\b/…`). `a\b` is not `a/b`. On Windows, `directory`
  `C:\\Users\\x\\ImapMail\\other` matches its store.
- **R4-04** (`cmd/mailarchive` `TestR5SyncWaitSecondProfileRefusedChangesNothing`, 2 subtests,
  green). `-enable-offline -sync-wait` with stores in two profiles, where the second is refused
  at its check: once for a block comment, once for Thunderbird running for that profile only. It
  exits 1, both profiles' files are byte-identical, and there is no "START Thunderbird" and no
  "Waiting for".
- **R4-05** (`internal/wizard` `TestR5WizardEveryRefusalAfterAChange`, 3 subtests, green). After a
  change earlier in the run, the other refusal forms (a hand-edited prefs.js, user.js keeping it
  off, no profile found) all say "Earlier in this run … outlook.office365.com". None still says
  "did not change any Thunderbird setting.", "did not change any of them." or "So MailArchive
  changed nothing: a change".

## 5. Question 5, and what this pass did not do

**Question 5.** Every finding test from rounds 1 to 4 was rerun against `ab3c572`, with
`.claude/attack-c0c-r4`'s files copied into the r5 clone and only the wizard port's signature
adapted. All are green in `cmd/mailarchive`, `internal/wizard` and `internal/thunderbird`. The
only exceptions are §3's by-design reds.

**Not done:**

- No real Thunderbird was run. R5-02 rests on reading the source. The smallest check that
  settles it: in the Windows 11 VM, add a shared network printer, print one message from
  Thunderbird with its print dialog, quit Thunderbird, and look in prefs.js for a
  `print.printer_\\\\…` line. A local printer has no backslash, so it does not test this.
- `lib.rs` and `Preferences.cpp` are master copies, not the ESR that users run.
- Out of scope, already assumed by the design: a default changed outside prefs.js (an
  enterprise policy, autoconfig, or a distribution's default prefs) can turn
  `mail.server.default.offline_download` off. `OnByDefault` would then be wrong. The reader
  never reads those files.
- This was a narrow confirmation pass. Severity is my own judgement.
