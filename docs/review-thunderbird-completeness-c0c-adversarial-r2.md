# Adversarial pass, round 2: the C0c fixes

**Verdict: NOT CLEAN.** I attacked the fixes for all nine first-round findings. Six findings
are CONFIRMED by failing tests (1 MED, 5 LOW), and one more LOW comes from reading the code
only. Nothing is HIGH. All nine original attacks, rerun against the fixed code, now pass. The
new holes sit just beside the fixes:

- the profile finder still takes a mail store for the profile in one shape (MED, very
  unlikely);
- the comment-aware rewrite edits the wrong text in two hand-edited shapes;
- the "checks every store before changing any" claim covers only two kinds of refusal;
- the wizard's "this export goes ahead" can be false.

| Field | Value |
|---|---|
| Reviewed | branch `tb-completeness`, tip `ec9ee2b` (the fix commit) |
| Date worked | 2026-11-08 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` 2e7dcc8b3416b44f6c48ab21fc02db4e77406b46695a490a6e76e23ebb0e852a; `cmd/mailarchive/main.go` 8bfaf21bd96c53160a7775047cc6cb69773ed58eb625fbc52ac389f7b26efae3; `internal/wizard/wizard.go` 93325b39bf1a98a811ea703fabb3680b6e8b31536a0b6795ba061c30ebbf5469 |
| Reviewer | one model instance, finder and skeptic together. It did not write the fixes. Every CONFIRMED item has a test that fails against the real code. The lead should re-check them |
| Attack clone | `.claude/attack-c0c-r2`, a clone at `ec9ee2b`. Throwaway, safe to delete. It holds four test files and no product change: `internal/thunderbird/zz_r2_attack_test.go`, `zz_r2_attack_unix_test.go`, `zz_r2_attack_ext_test.go`, and `cmd/mailarchive/zz_r2_attack_test.go` |
| Tools | go1.26.0 darwin/arm64, macOS (APFS). Not run on Windows or Linux |
| Commands | `go -C .claude/attack-c0c-r2 test -count=1 -v -run TestR2 ./internal/thunderbird/ ./cmd/mailarchive/` exits 1 with the reds listed below, and every other TestR2 test is green. The builder's own tests beside the attack files (`-skip TestR2`) stay green for `internal/thunderbird`, `internal/wizard`, `internal/tbrestore` and `cmd/mailarchive` |

Only throwaway temp-folder profiles were used. No real Thunderbird profile and no MailArchive
settings folder was touched. No product code was changed.

## 1. Findings

| ID | Fix attacked | Claim | Severity | Verdict | Failing test |
|---|---|---|---|---|---|
| R2-03 | INS-01 (MA-1203) | `isProfileFor` (`thunderbird.go:67-83`) checks only that the first part of the path under the candidate folder is named `ImapMail` or `Mail`. Take an account folder that holds a regular file `prefs.js` (an mbox-format folder called "prefs.js") and also a directory `Mail` (a maildir-format folder called "Mail"). Point `-enable-offline` at `ImapMail/<host>/Mail`, and the account folder is taken for the profile. A crafted message in that mbox (an accounts list plus a server with hostname `Mail`) then gets `-enable-offline` to **write into the mail file** and leave a backup inside the store. The real account is never touched | MED (very low likelihood: one account would need both store formats, plus a person pointing the input at that subfolder, plus a crafted message. The impact is the same kind as INS-01's: a write into mail) | CONFIRMED | `TestR2ProfileFinder/a_Mail_folder_inside_a_store`, `TestR2MailFolderInsideAStoreIsNeverWritten` |
| R2-01 | CON-03 (MA-1195) | One line holds a `/* … */` copy of the setting before the live setting: `/* user_pref("…offline_download", false); */ user_pref("…offline_download", false);`. `EnableOffline` (`:510-516`) replaces only the **first** match of the cleaned code text, and that match is inside the comment. It reports `Changed`, but Thunderbird still applies `false` | LOW (only in a hand-edited file) | CONFIRMED | `TestR2CommentedCopyBeforeTheSettingOnItsLine` |
| R2-02 | CON-02/03 (MA-1195) | Two settings on one line: the pattern `rePref` (`:113`) reads everything up to the last `);` as the value. (a) With `user_pref("…offline_download", false); user_pref("…name", "keep me");`, the whole line is replaced and **the other setting is deleted**. (b) With `…offline_download", true); user_pref(…)`, the account reads as off even though Thunderbird applies on | LOW (hand-edited; more plausible in a hand-written `user.js`, which is read by the same scanner) | CONFIRMED | `TestR2TwoSettingsOnOneLine` (both subtests) |
| R2-04 | INS-01 (MA-1203) + the hostname fallback | Put a copied old profile under this profile's `Mail/` folder (`<profile>/Mail/old-profile/ImapMail/imap.gmail.com`). The folder search returns the outer profile (the first part of the path under it is `Mail`), and the hostname fallback hands that store the outer profile's live `server1`. That account's setting would then be changed for a store it does not own, and the sync wait would watch a folder that never grows | LOW (unlikely layout; the old code did the same) | CONFIRMED | `TestR2ProfileFinder/another_profile's_store_under_this_profile's_Mail` |
| R2-05 | OUT-01 (MA-1204) "checks every store before changing any" | The check before any change (`main.go:2107-2117`) asks only `AccountToEnable`. Two refusals come after a change has already been made. First, Thunderbird running for the **second** profile (checked inside `prepareStore`). Second, `EnableOffline`'s own refusals: a comment inside the setting, or a link or folder at the backup. Either way the first profile has already been changed in a run that exits 1, while the code comment says "a refusal … means nothing was changed in this run". The change itself is correct, and the log names it | LOW (honesty) | CONFIRMED | `TestR2LaterStoreRefusalLeavesEarlierChanged` (both subtests) |
| R2-06 | INS-01 (MA-1203), the "profile itself" rule | `FindProfileDir(profile)` refuses a profile folder whose parent is named `Mail` (or `mail` on macOS and Windows, which ignore letter case), for example `~/Mail/tb-profile`. The old code accepted it. The CLI and the wizard never pass a profile itself, because they require `/ImapMail/` in the path. The Desktop's pre-sync check (`localarchive.go:304`) does pass the archive's recorded source paths, so for such an archive the pre-sync option would stay hidden. Safe direction | LOW (asks for too much; whether a recorded path can be a profile folder is not proven) | CONFIRMED | `TestR2ProfileFinder/the_profile_itself,_under_a_folder_named_Mail` |
| R2-07 | OUT-01 (MA-1204), the wizard | The refusal returns `zenity.Warning`'s result as is (`wizard.go:1575`). On Linux, the zenity program exits with code 1 when the window is closed, and the zenity library turns that into `ErrCanceled` (`util.go:96`, read). The wizard then ends quietly (`wizard.go:154`) and **no export runs**, even though the words just said "This export goes ahead with the mail already on this computer". `wizard.go:885` already shows the right pattern: ignore `ErrCanceled` after an information dialog. The `EnableOffline` failure dialog (`:1597`) has the same shape | LOW | reasoned only (zenity is not driven in a test). On macOS and Windows I did not trace what closing a one-button dialog returns | none |

### Suggested directions (the builder decides)

- **R2-03 and R2-04.** Follow the first round's "better still": accept a folder as the profile
  only if the store is that folder's own `ImapMail/<name>` or `Mail/<name>`, or a path inside
  one of those that never passes through a candidate holding a `prefs.js` of its own. A simpler
  rule also works: stop at the **highest** candidate rather than the nearest. Separately, the
  hostname fallback could require the store to be exactly `<profile>/ImapMail/<name>`.
- **R2-01 and R2-02.** Rewrite by position, not by finding the text again. Have the scanner
  record where each setting starts and ends on its line, and parse one call at a time, so
  `);` ends a setting only outside quotes. Or refuse, with words, any line that holds more than
  one setting or anything besides the setting.
- **R2-05.** Either move the "Thunderbird is running" check and the backup check into the
  check made before any change, or word the comment, the design and the refusal so they claim
  only what is true. For example: "Nothing was changed for this store; the stores before it
  were turned on (see above)."
- **R2-07.** Treat `ErrCanceled` from the refusal dialog like OK, as line 885 does.

### The failing tests (from the attack clone, in substance)

```go
// R2-01  (internal/thunderbird)
line := `/* user_pref("` + offKey + `", false); */ user_pref("` + offKey + `", false);` + "\n"
p, _ := r2Profile(t, line)                 // live("server1") + server1 at ImapMail/imap.gmail.com
c, _ := EnableOffline(p, "server1")
if v := appliedOffline(t, p); c.Changed && v != "true" { t.Fatalf(...) }
// red: prefs.js now reads
// /* user_pref("mail.server.server1.offline_download", true); */ user_pref("mail.server.server1.offline_download", false);

// R2-02
p, _ := r2Profile(t, `user_pref("`+offKey+`", false); user_pref("mail.server.server1.name", "keep me");`+"\n")
EnableOffline(p, "server1")                // red: the name setting is gone from prefs.js
p, store := r2Profile(t, `user_pref("`+offKey+`", true); user_pref("mail.server.server1.name", "keep me");`+"\n")
acct, _ := FindAccountForStore(p, store)   // red: acct.OfflineDownload == false

// R2-03 (profile finder)
store := filepath.Join(prof, "ImapMail", "imap.gmail.com")
mkdirs(t, filepath.Join(store, "Mail", "cur"))
writeFile(t, filepath.Join(prof, "prefs.js"), "")
writeFile(t, filepath.Join(store, "prefs.js"), "From - x\n\nbody\n")
FindProfileDir(filepath.Join(store, "Mail"))   // red: returns the store, not prof

// R2-03 end to end (cmd/mailarchive): the store's mbox "prefs.js" holds a message with
//   user_pref("mail.accountmanager.accounts", "account9"); user_pref("mail.account.account9.server", "server9");
//   user_pref("mail.server.server9.hostname", "Mail"); user_pref("mail.server.server9.offline_download", false);
prepareStore(ctx, filepath.Join(store, "Mail"), true, false, logger)
// red: the mbox now ends `server9.offline_download", true);`, prefs.js.mailarchive-bak appears
// in the store, the log says "Enabled offline download for Mail", err == nil

// R2-04
store := filepath.Join(prof, "Mail", "old-profile", "ImapMail", "imap.gmail.com")
got, _ := FindProfileDir(store); acct, _ := AccountToEnable(got, store)
// red: "… was given server1 of <prof>"

// R2-05 (cmd/mailarchive): two one-account profiles, the second with a `lock` symlink to
// this test's own pid (Thunderbird "running"), or a folder at its prefs.js.mailarchive-bak.
code, msg, logText := enableOfflineRun(t, storeA, storeB)
// red: exit 1 ("Thunderbird looks like it is running …" / "back up prefs.js: … is not a
// regular file (a directory)"), yet the first profile was changed; log line 1:
// "Enabled offline download for imap.gmail.com (prefs backed up to prefs.js.mailarchive-bak)."

// R2-06
prof := filepath.Join(t.TempDir(), "Mail", "tb-profile")   // holds prefs.js and ImapMail/
FindProfileDir(prof)                                        // red: "", false
```

## 2. Held by test (green in the clone)

| Attack on the fix | Test | Result |
|---|---|---|
| Accounts list with spaces, empty items, a repeated id and a trailing comma. Two accounts naming one server. An earlier list naming a removed account that the last list drops. A list only inside `/* */` or after `//` | `TestR2AccountsListShapes` (5 subtests) | HELD |
| A value holding `\"`, a comma, `//`, `/*` and `#` inside quotes is left byte for byte, and parsed whole | `TestR2StringValuesKeepTheirMarkers` | HELD |
| A `/* */` comment spanning lines that holds a `… true` line never counts and is left as it is | `TestR2BlockCommentSpanningLines` | HELD |
| CRLF kept when an existing line is replaced, including a `// note` after the setting | `TestR2CRLFReplaceKeepsCR` | HELD |
| `user.js` turns one server on and another off. The first is "already on" with nothing written; the second is refused (`UserJSError` naming `server2`); `prefs.js` is unchanged and no backup is made | `TestR2UserJSPerServer` | HELD |
| `user.js` as a link (to a file turning it on), a folder, over 16 MB, or a pipe: each is refused by the same bounded reader (`util.ReadRegularFile`), not followed and not waited on. The words name `user.js` and "changed nothing", and `prefs.js` is unchanged | `TestR2UserJSPlanted`, `TestR2PipesAreNotWaitedOn/user.js` | HELD |
| The `compatibility.ini` label: missing, garbled, an empty value, no `LastVersion`, or a link all give both labels. `160.0a1` gives the new label. `153.6.0esr` and `159.0b3` (with CRLF) give the old one. Spaces around `=` are fine | `TestR2LabelByVersion` (9 subtests) | HELD |
| `prefs.js.mailarchive-bak` as a link, a dangling link, a folder or a pipe: refused, nothing written through, the link's target never created, `prefs.js` unchanged. A planted regular file is kept and never rewritten | `TestR2BackupPlanted`, `TestR2PipesAreNotWaitedOn/prefs.js.mailarchive-bak` | HELD (see §3 on "kept") |
| The profile under an ancestor folder named `ImapMail`. A symlinked `ImapMail`. A store of a copied profile with no `prefs.js`, sitting beside a real profile | `TestR2ProfileFinder` (3 subtests) | HELD |
| **Reruns of the first round's attacks** with fixtures that list their accounts: the removed account sharing a folder or owning a leftover folder (CON-01), the shared hostname (INS-02), two off lines (CON-02), a commented on line (CON-03), the second change and the backup (CON-04, now kept), the CRLF append (CON-05), the two readers agreeing on `user.js` off, on and missing (CON-06), the mail folder named `prefs.js` (INS-01, the original fixture), the reason reaching the person (OUT-01, now a refusal), and the three held checks (only one setting changes, the exact 16 MB bound, case twins) | `TestR2RerunGhostAndHostname`, `TestR2RerunLinesBackupCRLF`, `TestR2RerunHeld`, `TestR2RerunUserJSReadersAgree`, `TestR2RerunInsOut` | HELD: no regression |

## 3. Held by reasoning

- **The wizard after a refusal:** no code path runs the sync or a Full export.
  `imapPrepAccount`'s refusal returns `false` before the "Thunderbird is running" loop and
  before `guiWaitForSync` (`wizard.go:1571-1576`). A failed `EnableOffline` also returns
  `false` before the sync (`:1596-1598`). So the export mode stays Incremental (`:882-884`).
  The one exception is "Skip this step" in the "Thunderbird is running" loop, which still guides
  the sync and returns `true`. That is by design and predates C0c. The opposite problem, the
  export not running at all, is R2-07.
- **The kept backup:** a regular file already at `prefs.js.mailarchive-bak` is kept, whoever
  put it there. That follows the lead's "keep the first original" call, and the CLI says it was
  kept, "from before MailArchive first changed prefs.js". That statement is false for a file
  planted by someone else, or one left by some other tool. But anyone able to plant it can also
  edit `prefs.js` directly, so it is not a finding. Note: a kept backup can be months older
  than the `prefs.js` beside it. Restoring it would roll back every later setting, and the words
  do not warn about that.
- **CRLF mixed with LF:** the line ending is taken from the first line break. A file whose first
  line ends in LF and whose later lines end in CRLF gets an LF line on append. Mozilla's parser
  accepts both, so this is cosmetic. I did not file it.
- **A link at `user.js` is refused, not followed.** This is the planted-file rule. A person
  whose `user.js` is a link (for example, managed by a settings-sync tool) gets a refusal that
  names `user.js` and changes nothing. Safe direction. A friction-review note, not a finding.
- **The 160 label boundary itself** comes from design §6 fold L5. I did not check it against
  Thunderbird's own strings. A 160 test build (`160.0a1`) gets the newer label, which is right
  only if the label changed before that build.

## 4. What this pass did not do

It was not run on Windows or Linux, and nothing ran against a real Thunderbird. The zenity
behaviour behind R2-07 was read from the library's source (Linux path), not observed. The
friction review that the wizard's new words owe is a separate gate, and this pass did not do
it.

Severity is my own judgement, and the lead decides what blocks the merge. My recommendation:
fix R2-03 before merge (a small change to the profile finder, which also fixes R2-04), and
keep the LOW items as notes or fix them in the same commit (R2-05's wording, and R2-07's
one-line `ErrCanceled` handling, are each a few lines).

---
Register: finding (an adversarial review report). Drift: none noticed. Touched: one review
file in the worktree; four throwaway test files in `.claude/attack-c0c-r2`; no product code.
Persona: none. Source reached: `thunderbird.go`, `main.go`, `wizard.go`, `localarchive.go` and
`state/manifest.go` at `ec9ee2b` (read); zenity v0.10.12 `msg.go` and `util.go` (read); the
first round's review and its attack clone (read); Thunderbird's own source (not reached).
