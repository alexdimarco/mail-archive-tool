# Adversarial pass, round 3: the C0c round-2 fixes

**Verdict: NOT CLEAN.** Five findings are CONFIRMED by failing tests. All five are LOW, and none
is HIGH or MED. Every round-2 finding that was fixed holds: R2-01, R2-02, R2-03, R2-05, R2-06 and
R2-07. All round-2 attacks, rerun against the new code, pass, and so do the round-1 attacks that
round 2 had ported.

The new holes are mostly in one place. The new statement reader says it reads prefs.js and user.js
"as Thunderbird's parser reads them" (design §4.1 step 1, round-2 note). It does not, in four
ways that Mozilla's own parser source spells out:

1. Mozilla ignores `pref(...)` and `sticky_pref(...)` lines in a user's settings files. MailArchive
   counts them.
2. Mozilla decodes escape codes in setting names. MailArchive does not.
3. Mozilla stops reading at a NUL byte. After a broken statement, it skips ahead to the next `;`.
4. A Mozilla string value may run across several lines.

So in a hand-edited file, MailArchive can say a setting is on when Thunderbird has it off. It can
also report "Changed" when Thunderbird will never apply the change. And in one shape, it can
rewrite text inside another setting's value. The fifth finding is the known R2-04 leftover,
confirmed end to end.

| Field | Value |
|---|---|
| Reviewed | branch `tb-completeness`, tip `32c2e53` (fix `f04e02a`; `32c2e53` only renumbers MA-1210 to MA-1260) |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` ee55c7af6f07554a113d4ab75c76d731d1d2e5dc061870d04d314c2afa0fb9d9; `internal/thunderbird/offline_words.go` 699c62ef316df117247dc99d759951042fc299e836910f84020146f25c84e93f; `cmd/mailarchive/main.go` 25e57af388eaf4057c7c58a6141f0a0f1b307c18a365dd9287e5f9039aaa20dd; `internal/wizard/wizard.go` bd27761da81c44ca4cfa2d673f4e8a683d50db192c40b05a7cc61facd0653b03 |
| Reviewer | One model instance, acting as both finder and skeptic. It did not write the fixes. Every CONFIRMED item has a test that fails against the real code. The lead should re-check them |
| Attack clone | `.claude/attack-c0c-r3` in the main checkout: a throwaway clone at `32c2e53`, safe to delete. It holds test files and no product change. New: `internal/thunderbird/zz_r3_attack_test.go`, `internal/thunderbird/zz_r3_attack_unix_test.go`, `cmd/mailarchive/zz_r3_attack_test.go`. Ported from round 2: `internal/thunderbird/zz_r2_attack_test.go`, `zz_r2_attack_unix_test.go`, `zz_r2_attack_ext_test.go`, `cmd/mailarchive/zz_r2_attack_test.go` |
| Tools | go1.26.0 darwin/arm64, macOS (APFS). Not run on Windows or Linux |
| Commands | `go -C .claude/attack-c0c-r3 test -count=1 -run "TestR2\|TestR3" ./internal/thunderbird/ ./cmd/mailarchive/` exits 1. The only reds are the ones listed below; every other TestR2 and TestR3 test is green. The builder's own tests beside the attack files (`-skip "TestR2\|TestR3"`) are green for `internal/thunderbird`, `internal/wizard` and `cmd/mailarchive` |

Only throwaway temp-folder profiles were used. No real Thunderbird profile and no MailArchive
settings folder was touched. No product code was changed.

**What Thunderbird applies** comes from Mozilla's settings parser,
`modules/libpref/parser/src/lib.rs` (Thunderbird uses the same one). I read it from the
`mozilla/gecko-dev` master mirror on 2026-11-08, through a fetch tool that summarises the page and
quotes from it. These are the parts quoted from it:

- In a user's settings file, only `user_pref` is accepted. The error for anything else is
  "expected 'user_pref' at start of pref definition", and `pref` and `sticky_pref` are allowed
  only in default files.
- Error recovery is "Panic-mode": it skips tokens until a `;` or the end of the file.
- Setting names have their escape codes decoded, by the same routine as values.
- A NUL byte counts as the end of the file.
- A raw newline inside a string is allowed.
- `#`, `//` and `/* */` are comments, and an unclosed `/*` swallows the rest of the file.

I did not read a pinned ESR copy of that file.

## 1. Findings

| ID | Claim | Severity | Verdict | Failing test |
|---|---|---|---|---|
| R3-01 | **`pref(` and `sticky_pref(` are counted as settings.** `parseStatements` (`thunderbird.go:263`) accepts all three call names, and `values()` lets the last one win whatever its call name. Mozilla rejects `pref` and `sticky_pref` in prefs.js and user.js. Take user.js `pref("mail.server.server1.offline_download", true);` with prefs.js `user_pref(…, false)`. The account reads as on (`OfflineInUserJS`), and `-enable-offline` logs "Offline download already enabled for imap.gmail.com (folder imap.gmail.com, server1) (user.js turns it on)." Nothing is changed and the export runs, yet Thunderbird applies `false`. The same thing happens with a `pref(…, true)` line after the `user_pref(…, false)` in prefs.js: EnableOffline returns "already enabled". The design note says this case is "refused or read conservatively, never guessed" (`pref()` against `user_pref()` priority in user.js). It is not: it is read as on | LOW. It needs a hand-written user.js (or hand-edited prefs.js) that uses the wrong call name. It is the most consequential LOW: a false "already on" leaves mail out of the export with no warning, which is exactly what this feature exists to prevent | CONFIRMED | `TestR3PrefCallInUserJSIsNotApplied` (`pref`, `sticky_pref`), `TestR3PrefCallInPrefsJSIsNotApplied`, `TestR3PrefCallInUserJSOnTheCommandLine` |
| R3-02 | **Escape codes in setting names are not decoded.** A statement's name is compared as written (`key[1:len(key)-1]`, `:291`). user.js `user_pref("mail.server.server1.offline_download", false);`, with the underscore in `offline_download` written as the escape code backslash-u-005f, is, to Thunderbird, the setting `mail.server.server1.offline_download` set to false over prefs.js. MailArchive does not see it. EnableOffline writes prefs.js and reports `Changed`, and Thunderbird undoes the change at its next start. The user.js check (MA-1205) is bypassed | LOW. It needs a hand-written user.js with escape codes in a name | CONFIRMED | `TestR3EscapedNameInUserJS` |
| R3-03 | **A line added where Thunderbird never reads it.** (a) After a NUL byte: prefs.js holding a line with just `\x00`. MailArchive skips that line as unreadable, appends `user_pref(…, true);` after it, and reports `Changed`, but Thunderbird stops reading at the NUL. (b) After an unfinished last statement such as `user_pref("mail.zz", 1)` with no `;`: Thunderbird's recovery skips ahead to the next `;`, which is the end of the added line, so the added setting is lost. Either way the log says "Enabled offline download …" and the setting stays at its earlier value or Thunderbird's default. The `openAtEnd` guard (`:808`) covers only an unclosed `/*` | LOW. Thunderbird itself never writes such a prefs.js. It needs a hand edit or a damaged file (a NUL-filled tail is a known crash leftover on some file systems) | CONFIRMED | `TestR3NulEndsTheFile`, `TestR3UnfinishedLastStatementSwallowsTheAddedLine` |
| R3-04 | **A rewrite can change another setting's value.** Thunderbird allows a raw newline inside a string, but `codeOf` forgets an open quote at each line's end (`:315-351`). Take `user_pref("mail.server.server1.name", "line one⏎user_pref("…offline_download", false);⏎end");`. The middle line reads as a setting standing alone, so EnableOffline rewrites it to `true`. That text is part of the `name` value, so the rewrite changes another setting's bytes, and the account's own setting is still not set (Thunderbird's default applies). Asked: "can a rewrite ever alter another setting's bytes?" Yes, in this shape | LOW. Hand-edited only: Thunderbird writes a newline in a value as `\n` | CONFIRMED | `TestR3RewriteInsideAMultiLineString` |
| R3-05 | **The R2-04 leftover (known, listed as a follow-up in `f04e02a`).** A copied profile under this profile's `Mail/` or `ImapMail/` that has its own prefs.js but no compatibility.ini. `FindProfileDir` gives the copy's store the outer profile, and the hostname fallback hands it the outer profile's live `server1`. End to end, `-enable-offline` on the copy's store changes the **outer** profile's prefs.js (backup made). The log says "Enabled offline download for imap.gmail.com (folder imap.gmail.com, server1)", and the sync wait (with `-sync-wait`) would watch the copy's folder, which never grows | LOW. It needs a profile copy kept inside the live profile's mail folders, missing compatibility.ini, and a person pointing the input at it. The change itself turns on a real account of the same profile (no mail written, backup kept), so the harm is an unasked-for setting change plus a false log line | CONFIRMED | `TestR3CopiedProfileWithoutItsMarker` (`under Mail`, `under ImapMail`), `TestR3CopyWithoutMarkerChangesTheOuterAccount` |

### Suggested directions (the builder decides)

- **R3-01:** read `pref(` and `sticky_pref(` in prefs.js and user.js as Mozilla does, which is
  "not a setting". The simplest faithful rule is to drop them from `stmts`, or to treat their lines
  as unreadable so that a setting they name gives `HandEditedError`. Either one also makes the
  design note true.
- **R3-02:** treat any setting name that holds a backslash as unreadable (`HandEditedError` when
  it could be the setting), or decode `\uNNNN` and `\xNN` in names before comparing.
- **R3-03 and R3-04:** one rule closes both. Thunderbird never writes a line MailArchive cannot
  read, so refuse with `HandEditedError` whenever prefs.js has **any** unreadable line, or a NUL
  anywhere, before changing anything. That is wider than today's "an unreadable line naming the
  setting". A narrower option is to carry an open quote across lines in `codeOf` and refuse an
  append after any unreadable line.
- **R3-05:** the builder's own follow-up works: allow the hostname fallback only for a store
  sitting directly in `<profile>/ImapMail/`.

### The failing tests (from the attack clone, in substance)

```go
// R3-01 (internal/thunderbird)
p, store := r3Profile(t, r3Base()+`user_pref("`+offKey+`", false);`+"\n")
writeFile(t, filepath.Join(p, "user.js"), `pref("`+offKey+`", true);`+"\n")   // also sticky_pref
acct, err := AccountToEnable(p, store)      // red: err == nil, acct.OfflineDownload == true
// prefs.js user_pref(…, false) then pref(…, true): FindAccountForStore reads on; EnableOffline
// returns {Changed:false}, nil and writes nothing.
// cmd/mailarchive: prepareStore(…) logs "Offline download already enabled for imap.gmail.com
// (folder imap.gmail.com, server1) (user.js turns it on)." and returns nil.

// R3-02
bs := string(rune(0x5c))                    // a backslash: the name's "_" is written as bs+"u005f"
writeFile(t, filepath.Join(p, "user.js"), `user_pref("mail.server.server1.offline`+bs+`u005fdownload", false);`+"\n")
c, err := EnableOffline(p, "server1")      // red: c.Changed, err == nil

// R3-03
p, _ := r3Profile(t, r3Base()+"\x00\n")                         // (a) red: Changed, the line added after the NUL
p, _ := r3Profile(t, r3Base()+`user_pref("mail.zz", 1)`+"\n")   // (b) red: Changed, the line added after it

// R3-04
other := `user_pref("mail.server.server1.name", "line one` + "\n" +
	`user_pref("` + offKey + `", false);` + "\n" + `end");` + "\n"
// red: prefs.js now holds …"line one\nuser_pref("…offline_download", true);\nend");

// R3-05 (cmd/mailarchive)
old := filepath.Join(profile, "Mail", "old-profile")            // prefs.js only, no compatibility.ini
prepareStore(ctx, filepath.Join(old, "ImapMail", "imap.gmail.com"), true, false, logger)
// red: the outer profile's prefs.js changed; log "Enabled offline download for imap.gmail.com
// (folder imap.gmail.com, server1); prefs.js backed up to …"
```

## 2. Held by test (green in the clone)

| Attack | Test | Result |
|---|---|---|
| compatibility.ini with a BOM and CRLF, or blank lines first: found. Exactly 64 KB: found. One byte over: no profile. Empty, a NUL in the first line, `[Compatibility]` second, `LastVersion` in another section, or an mbox whose message holds the lines: no profile | `TestR3CompatibilityIniShapes` (9 subtests) | HELD |
| compatibility.ini as a pipe: no profile, and the finder does not wait on it. prefs.js as a link: no profile | `TestR3CompatibilityIniPlanted` (unix) | HELD |
| A store whose mbox files named `prefs.js` and `compatibility.ini` start with "From ", plus maildir message files named `prefs.js` and `compatibility.ini` inside `cur/`: the profile is still the real one. The climb looks only at the direct children of the input and of its parents, so a message file in `cur/` is never consulted | `TestR3StoreIsAProfileOnlyThroughACraftedFile` | HELD |
| A statement for the setting split across lines; trailing text after it; a NUL on its line; a `pref(` line for it: each refused or not rewritten, with prefs.js byte-identical. An unclosed `/*` at the end: `HandEditedError`, file unchanged | `TestR3ReaderHeld` (5 subtests) | HELD |
| The setting's own text, plus `)`, `;`, `//`, `/*` and `'`, inside another value on one line: not counted, and that value is kept byte for byte | `TestR3ReaderHeld/the setting's text inside another value on one line` | HELD |
| Spaces inside the statement, a tab before it, CRLF, a header comment, a block comment, a `#` comment, a non-ASCII letter, an escape code (backslash-u-00e9) and escaped quotes in another value: exactly that line's statement becomes `user_pref("…", true);`, with the tab and trailing spaces kept, and every other byte unchanged | `TestR3ReaderHeld/spaces inside the statement, …` | HELD |
| **Check, then write:** CheckOffline writes nothing. prefs.js swapped for a link after the check: EnableOffline plans again and refuses, nothing is written through the link, the link stays, no backup is made. The backup path made a dangling link after the check: refused, and the link's target is never created | `TestR3SwapAfterTheCheck` (3 subtests) | HELD |
| **Round-2 reruns.** R2-01 and R2-02 are now refused (`HandEditedError`, prefs.js unchanged). R2-03 end to end: no store file changed. R2-05: both later-store refusals leave the first profile byte-identical, with exit 1. R2-06: the profile under `Mail/` is found. Plus every round-2 held check: the accounts-list shapes, markers in strings, block comments, the user.js per-server rule, planted user.js files and backups, the label by version, and the ported round-1 attacks (CON-01 to CON-06, INS-01, INS-02, OUT-01, the 16 MB bound, the hard link, case twins). Ported fixture changes: a profile now needs compatibility.ini; R2-01, R2-02 and the CRLF-with-comment case accept a clean refusal; the labels' new names | all `TestR2*` in both packages | HELD: no regression |

The round-1 attack files (`.claude/attack-c0c`) no longer compile against today's API
(`EnableOffline` now returns two values, and the helpers were renamed). Round 2 had already ported
every one of their attacks as `TestR2Rerun*`, which I reran above.

## 3. Held by reasoning

- **The wizard's close button (R2-07):** both one-button notices after which the wizard goes on
  now go through `noticeClosed` (`wizard.go:1576` for the refusal warning, `:1600` for the failed
  change). `ErrCanceled` from either one goes on to the export with `prepared=false`, so the mode
  stays Incremental. Any other error still stops the wizard. "Skip this step" in the "Thunderbird
  is running" loop is unchanged and predates C0c. The wizard's `CheckOffline` runs before the
  person is asked to quit, and `EnableOffline` plans again after the quit, so a prefs.js that
  Thunderbird rewrites on quit is re-read, not trusted from the check.
- **Can a store's mbox start with `[Compatibility]`?** Not when Thunderbird writes it. The mbox
  store writes through `MboxMsgOutputStream`, a wrapper that the store's code describes as handling
  the mbox `From ` separator ("Wrap to handle mbox "From " separator, escaping etc.",
  `nsMsgBrkMBoxStore.cpp:748`, esr153 copy in `.claude/tbsrc-esr153`). I did not read
  `MboxMsgOutputStream.cpp` itself. An empty mbox (a folder with no messages yet) fails the check.
  In a maildir store a folder is a directory and messages are in `cur/`, `new/` or `tmp/`. What
  is left is a file planted by something other than Thunderbird, for example a raw mbox file
  copied into the profile. That is out of reach for anyone who cannot already edit prefs.js.
- **Thunderbird started between the pre-pass and a later store.** `prepareStore` checks again, so
  a later store whose Thunderbird was started mid-run is refused with "-enable-offline changed
  nothing, and nothing was exported", although an earlier store was changed in that run. The code
  comment names only "a write that then fails" as the way this can happen. It is a race, not made
  reproducible here (no timing seam). It is a wording note, not a finding.
- **Two stores in one profile:** the pre-pass checks both against the same prefs.js. The first
  store's write (its own lines, or one appended line, plus the backup) cannot create a refusal for
  the second store that the pre-pass did not already see. An open `/*` at the end would already
  have refused the second store in the pre-pass.
- **"Inside the profile but outside its mail folders ⇒ no profile":** this errs on the safe side.
  On the command line a store with no profile is a log line ("Could not find the Thunderbird
  profile …; skipping prep") and the export runs. That is not the "changed nothing, and nothing was
  exported" refusal. This predates the fix, but the fix makes it a little more common, for example
  a profile folder missing its compatibility.ini. It is a friction note.
- **The climb stops after six folders.** A store more than five folders below its profile has no
  profile, which errs on the safe side. This predates the fix.

## 4. What this pass did not do

It was not run on Windows or Linux, and nothing ran against a real Thunderbird. Mozilla's parser
rules come from a fetch tool's summary of `lib.rs` on gecko-dev master. A pinned ESR copy was not
read, and Thunderbird was not driven to confirm R3-01 to R3-04. The smallest check: in a throwaway
profile, put `pref("mail.server.serverN.offline_download", true);` in user.js, keep prefs.js's
`user_pref(…, false)`, start Thunderbird, and look at the account's offline checkbox (R3-01). The
light friction re-look of the new words is a separate gate, and this pass did not do it.

Severity is my own judgement, and the lead decides what blocks the merge. My recommendation: fix
R3-01 before merge, because it can make "already on" false, which defeats the feature, and the
design note claims the opposite. R3-03 and R3-04 close with one wider "refuse any unreadable line"
rule. R3-02 and R3-05 are each a few lines. None of the five writes into mail.

---
Register: finding (an adversarial review report). Drift: none noticed. Touched: this review file
in the worktree; seven test files in the throwaway clone `.claude/attack-c0c-r3` (three new, four
ported from round 2); no product code. Persona: none. Source reached: `thunderbird.go`,
`offline_words.go`, `main.go` (`prepareThunderbird`, `prepareStore`, `checkOfflineStore`),
`wizard.go` (`prepareThunderbirdGUI`, `noticeClosed`, `imapPrepAccount`, the caller at 846-887),
design §4.1 step 1 round-2 note, and the round-2 review (all read at `32c2e53`); Mozilla's
`lib.rs` (reached through a fetch summary, not pinned); `nsMsgBrkMBoxStore.cpp` esr153 (one comment
read); `MboxMsgOutputStream.cpp` (not reached).
