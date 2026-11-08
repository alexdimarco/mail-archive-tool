# Adversarial pass: Thunderbird completeness slice C0c

**Verdict: NOT CLEAN.** I tried 9 attacks and all 9 are CONFIRMED (3 MED, 6 LOW). Three more
checks HELD, and I reasoned through the rest. Nothing is HIGH: no attack reads or writes through a
link, loses `prefs.js` or hangs a run. The C0c guards that the brief named (refuse a link, a pipe
or a file over 16 MB on read; temp file then rename on write; the whole-component folder match)
held every time I pushed on them. The findings sit next to those guards: which servers count as
accounts, how the profile is found, what `EnableOffline` takes as the setting, and whether the
reason for "no account" reaches the person.

| Field | Value |
|---|---|
| Reviewed | branch `tb-completeness`, tip `205ee41`; scope commits `f91494f`, `b5c16a8` |
| Date worked | 2026-11-07 |
| Files (sha256) | `internal/thunderbird/thunderbird.go` 775924ba…7024722; `internal/util/openregular.go` 15b46044…d7b7000; `internal/util/securefile.go` c5db1e1a…fc3ba95 |
| Reviewer | one model instance acting as finder and skeptic together (no separate fan-out). Every CONFIRMED item is backed by a test that fails against the real code; every finding is mine, so the lead should re-check them |
| Attack clone | `.claude/attack-c0c` (a clone of the worktree at `205ee41`). Throwaway, safe to delete. It holds the three test files below and no product change |
| Tools | go1.26.0 darwin/arm64, macOS (APFS). Not run on Windows or Linux |
| Commands | `go -C .claude/attack-c0c test -count=1 -run "Attack\|Held" -v ./internal/thunderbird/ ./cmd/mailarchive/`: exit 1 (9 red, 3 green, as listed). `go -C .claude/attack-c0c test -count=1 -skip Attack ./internal/thunderbird/ ./cmd/mailarchive/ ./internal/tbrestore/`: the builder's own tests stay green next to the attack files |
| Count | 9 attacked / 9 confirmed / 0 refuted. Separately: 3 held by test and the rest held by reasoning (§3) |

Only throwaway temp-folder profiles were used. I never touched a real Thunderbird profile or the
MailArchive settings folder. No product code was changed.

## 1. Findings

| ID | Lens | Claim | Severity | New in C0c? | Verdict | Failing test |
|---|---|---|---|---|---|---|
| CON-01 | integrity | A server whose account was removed but whose `mail.server.serverN.*` lines stayed in `prefs.js` still counts as an account (`matchAccount`, `thunderbird.go:128-177`; `serverKeys` never reads `mail.accountmanager.accounts`) | MED | yes (new rule) | CONFIRMED | `TestAttackGhostServerIsNotAnAccount` |
| INS-01 | insider (mail content) | A top-level IMAP folder named `prefs.js` makes `FindProfileDir` (`thunderbird.go:41-54`, checks the store itself first) take the **store** for the profile. With one crafted message line, `-enable-offline` then **writes into the mailbox file** and leaves a backup in the store, and the real account is never touched | MED (low likelihood) | no (`FindProfileDir` predates C0c), but C0c's write guard does not stop it | CONFIRMED | `TestAttackAMailFolderNamedPrefsJS` |
| OUT-01 | honesty | When no account can be picked, C0c returns an error naming `prefs.js` and the competing accounts, but both callers throw it away (`acct, _ :=` at `main.go:2121` and `wizard.go:1575`). The command line says only "could not match an account". The wizard's **Yes, prepare** says nothing, changes nothing, then guides the sync and runs a Full export as if it had prepared | MED | the throw-away predates C0c; C0c adds new reasons to land there (several accounts, a refused link, a pipe, too big) | CONFIRMED | `TestAttackAmbiguityReasonReachesThePerson` |
| INS-02 | spec | The server-name fallback is **looser** than §4.1 step 1 item 2 in one case. Two servers have `imap.gmail.com`; one has no folder setting, the other's folder is `-1`. The build drops the `-1` server, counts one, and picks the other. The §0 ruling accepted the stricter reading because it "fails toward no account"; here it does not | LOW | yes | CONFIRMED | `TestAttackHostnameFallbackWithTwoServersOnTheHost` |
| CON-02 | integrity | Two `offline_download` = `false` lines for one account: `EnableOffline` (`:306-316`) replaces only the first and reports `changed=true`. Thunderbird applies the last line, so the account is still off | LOW (hand-edited file) | no | CONFIRMED | `TestAttackDuplicateOffLines` |
| CON-03 | integrity | A comment line `// user_pref("…offline_download", true);` makes `EnableOffline` return "already on" (`strings.Contains`, `:307`) while the real line is `false`. The command line then prints nothing about it | LOW (hand-edited file) | no | CONFIRMED | `TestAttackCommentedOnLine` |
| CON-04 | integrity | `-enable-offline` loops over every store (`main.go:2107`). The second account's call overwrites `prefs.js.mailarchive-bak` with the already-changed file, so the backup the first log line named no longer holds the original | LOW | no | CONFIRMED | `TestAttackSecondEnableOverwritesTheBackup` |
| CON-05 | integrity | On a CRLF `prefs.js` (Thunderbird on Windows writes CRLF; recalled, not checked in its source), the append path adds a line ending in a bare LF | LOW (cosmetic: Mozilla's parser treats LF as whitespace, and Thunderbird rewrites the file at its next save) | no | CONFIRMED | `TestAttackCRLFKept` |
| CON-06 | integrity | `user.js`, which Thunderbird applies over `prefs.js` at every start (`tbrestore.ReadPrefs` honours it, MA-1095), is ignored by both `FindAccountForStore` and `EnableOffline`. A `user.js` that sets `offline_download` to `false` undoes the change at the next start, and MailArchive's two readers disagree about the same account | LOW | no | CONFIRMED | `TestAttackUserJSOverridesTheChange` |

### What each MED means, with a suggested direction (the builder decides)

- **CON-01.** §4.1 step 1 says the matched account supplies the account kind, Gmail status and
  limits for C1a/C1b. A removed account that still owns a leftover folder would get a full verdict
  instead of `unknown` (the M6 rule). And a removed account that shares the live account's folder
  makes the store ambiguous, so the live account can no longer be prepared. Direction: count only
  the servers named by `mail.account.<id>.server` for the ids in
  `mail.accountmanager.accounts`, and keep "no list → no account" as the safe default. How often
  Thunderbird leaves such lines behind is not proven: it normally deletes a removed account's
  `mail.server.<key>.*` lines (recalled, not checked), so the trigger needs an older leftover or a
  crash during removal. A lab question.
- **INS-01.** Likelihood is low: it needs a top-level server folder literally named `prefs.js`,
  and the write also needs a message line that starts with `user_pref(`. Impact is a write into a
  mail store, which MailArchive otherwise never does. Even without the crafted line, a folder named
  `prefs.js` alone makes the real account unpreparable without a word. The log in the test even
  says "Offline download was already on for : …", with an empty account name. Direction: in
  `FindProfileDir`, never accept the store folder itself, or anything inside `ImapMail/` or
  `Mail/`, as the profile. Better still, accept a folder only if it holds `prefs.js` as a regular
  file and the store sits under its `ImapMail/` or `Mail/`.
- **OUT-01.** This is the milder twin of the review's side finding A ("report the right one as
  done"). Direction: give the person the error's words on both surfaces, and have the wizard say
  the setting was not changed. This also belongs to the **friction review** this feature owes.

### The failing tests (from the attack clone, verbatim in substance)

`internal/thunderbird/zz_c0c_attack_test.go` (package `thunderbird`; it reuses the builder's
`prefLine`, `imapServer` and `profileWith`):

```go
func accountsLine(pairs ...string) string {
	var ids []string
	s := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		ids = append(ids, pairs[i])
		s += prefLine("mail.account."+pairs[i]+".server", pairs[i+1])
	}
	return prefLine("mail.accountmanager.accounts", strings.Join(ids, ",")) + s
}

// CON-01
func TestAttackGhostServerIsNotAnAccount(t *testing.T) {
	t.Run("a ghost sharing the live account's folder blocks the live account", func(t *testing.T) {
		prefs := accountsLine("account3", "server3") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + // ghost
			imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") // live
		p := profileWith(t, prefs, "imap.gmail.com")
		acct, err := FindAccountForStore(p, filepath.Join(p, "ImapMail", "imap.gmail.com"))
		if acct == nil || acct.ServerKey != "server3" {
			t.Fatalf("want the live server3, got %+v (err %v)", acct, err)
		}
	})
	t.Run("a ghost owns a leftover folder", func(t *testing.T) {
		prefs := accountsLine("account3", "server3") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + // ghost
			imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "") // live
		p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
		acct, _ := FindAccountForStore(p, filepath.Join(p, "ImapMail", "imap.gmail.com"))
		if acct != nil {
			t.Fatalf("the leftover folder of a removed account was given %s, which is no account", acct.ServerKey)
		}
	})
}
// red: "more than one account in prefs.js (server1, server3) could own the store folder
// imap.gmail.com"; and "the leftover folder of a removed account was given server1"

// INS-02
func TestAttackHostnameFallbackWithTwoServersOnTheHost(t *testing.T) {
	prefs := imapServer("server1", "imap.gmail.com", "", "") +
		imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "")
	p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
	acct, _ := FindAccountForStore(p, filepath.Join(p, "ImapMail", "imap.gmail.com"))
	if acct != nil {
		t.Fatalf("two servers have hostname imap.gmail.com, yet %s was picked by hostname", acct.ServerKey)
	}
}
// red: "yet server1 was picked by hostname"

func offProfile(t *testing.T, body string) (string, string) {
	t.Helper()
	p := profileWith(t, imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+body, "imap.gmail.com")
	return p, filepath.Join(p, "ImapMail", "imap.gmail.com")
}

// CON-02
func TestAttackDuplicateOffLines(t *testing.T) {
	off := `user_pref("mail.server.server1.offline_download", false);` + "\n"
	p, store := offProfile(t, off+off)
	changed, _, err := EnableOffline(p, "server1")
	if err != nil {
		t.Fatal(err)
	}
	acct, err := FindAccountForStore(p, store)
	if err != nil {
		t.Fatal(err)
	}
	if changed && !acct.OfflineDownload {
		t.Fatalf("EnableOffline said changed, yet the last line (the one Thunderbird applies) is still false")
	}
}

// CON-03
func TestAttackCommentedOnLine(t *testing.T) {
	body := `// user_pref("mail.server.server1.offline_download", true);` + "\n" +
		`user_pref("mail.server.server1.offline_download", false);` + "\n"
	p, store := offProfile(t, body)
	if _, _, err := EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	acct, err := FindAccountForStore(p, store)
	if err != nil {
		t.Fatal(err)
	}
	if !acct.OfflineDownload {
		t.Fatalf("after EnableOffline the account is still off (a comment was taken for the setting)")
	}
}

// CON-04
func TestAttackSecondEnableOverwritesTheBackup(t *testing.T) {
	prefs := imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") +
		`user_pref("mail.server.server1.offline_download", false);` + "\n" +
		imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "") +
		`user_pref("mail.server.server2.offline_download", false);` + "\n"
	p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
	if _, _, err := EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	_, backup, err := EnableOffline(p, "server2")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(backup)
	if string(b) != prefs {
		t.Fatalf("the backup is not the original prefs.js any more:\n%s", b)
	}
}
// red: the backup holds server1.offline_download true

// CON-05
func TestAttackCRLFKept(t *testing.T) {
	crlf := strings.ReplaceAll(imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", ""), "\n", "\r\n")
	p := profileWith(t, crlf, "imap.gmail.com")
	if _, _, err := EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
	if strings.Count(string(b), "\n") != strings.Count(string(b), "\r\n") {
		t.Fatalf("a bare LF line was added to a CRLF prefs.js: %q", b)
	}
}
// red: …"[ProfD]ImapMail/imap.gmail.com\");\r\nuser_pref(\"…offline_download\", true);\n"
```

`internal/thunderbird/zz_c0c_attack_ext_test.go` (package `thunderbird_test`, because
`tbrestore` imports `thunderbird`):

```go
// CON-06
func TestAttackUserJSOverridesTheChange(t *testing.T) {
	p := t.TempDir()
	if err := os.MkdirAll(filepath.Join(p, "ImapMail", "imap.gmail.com"), 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := `user_pref("mail.server.server1.hostname", "imap.gmail.com");` + "\n" +
		`user_pref("mail.server.server1.type", "imap");` + "\n" +
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");` + "\n" +
		`user_pref("mail.server.server1.offline_download", false);` + "\n"
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "user.js"), []byte(`user_pref("mail.server.server1.offline_download", false);`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := thunderbird.EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	acct, err := thunderbird.FindAccountForStore(p, filepath.Join(p, "ImapMail", "imap.gmail.com"))
	if err != nil {
		t.Fatal(err)
	}
	tp, err := tbrestore.ReadPrefs(p)
	if err != nil {
		t.Fatal(err)
	}
	v, _, _ := tp.Get("mail.server.server1.offline_download")
	if acct.OfflineDownload != (v == "true") {
		t.Fatalf("thunderbird says offline_download=%v; Thunderbird (prefs.js then user.js) applies %q", acct.OfflineDownload, v)
	}
}
// red: "thunderbird says offline_download=true; Thunderbird (prefs.js then user.js) applies "false""
```

`cmd/mailarchive/zz_c0c_attack_test.go` (package `main`; it reuses the builder's `offlineProfile`
and `enableOfflineLog`, which drive the real `prepareStore`):

```go
// INS-01
func TestAttackAMailFolderNamedPrefsJS(t *testing.T) {
	profile, store := offlineProfile(t, "user_pref(\"mail.server.server1.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com\");\n"+
		"user_pref(\"mail.server.server1.offline_download\", false);\n")
	mbox := filepath.Join(store, "prefs.js")
	msg := "From - Sat Nov 07 10:00:00 2026\nFrom: someone@example.com\nSubject: hi\n\n" +
		"user_pref(\"mail.server.server9.directory-rel\", \"[ProfD]\");\n\n"
	if err := os.WriteFile(mbox, []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	logText := enableOfflineLog(t, store)
	got, _ := os.ReadFile(mbox)
	realPrefs, _ := os.ReadFile(filepath.Join(profile, "prefs.js"))
	if !bytes.Equal(got, []byte(msg)) {
		t.Errorf("the mailbox file ImapMail/imap.gmail.com/prefs.js was rewritten:\n%s", got)
	}
	if _, err := os.Stat(mbox + ".mailarchive-bak"); err == nil {
		t.Errorf("a backup was written into the store")
	}
	if !strings.Contains(string(realPrefs), `server1.offline_download", true`) {
		t.Errorf("the real account was not turned on; log:\n%s", logText)
	}
}
// red, all three: the mailbox gained `user_pref("mail.server.server9.offline_download", true);`,
// prefs.js.mailarchive-bak appeared inside ImapMail/imap.gmail.com/, server1 stayed off, and the
// log said "Offline download was already on for : … written into prefs.js to make sure"

// OUT-01
func TestAttackAmbiguityReasonReachesThePerson(t *testing.T) {
	_, store := offlineProfile(t, "user_pref(\"mail.server.server2.hostname\", \"imap.gmail.com\");\n"+
		"user_pref(\"mail.server.server2.type\", \"imap\");\n")
	logText := enableOfflineLog(t, store)
	if !strings.Contains(logText, "more than one account") {
		t.Fatalf("the log does not say why no account was matched:\n%s", logText)
	}
}
// red: the log is only "Skipping -enable-offline: could not match an account to imap.gmail.com."
```

## 2. Held by test (green in the clone)

| Check | Test | Result |
|---|---|---|
| `EnableOffline` changes only the one setting: every other byte is identical, including a `\\` and `\"` value; no `.tmp` file is left; mode is 0600; a hard link to `prefs.js` is not written through | `TestHeldOnlyTheOneSettingChanges` | HELD |
| The 16 MB bound is exact: 16 MB is read, 16 MB + 1 is refused with an error naming `prefs.js` and no account | `TestHeldTheBoundIsExact` | HELD |
| Under the macOS rule, two folders that differ only by case (`Mail` and `mail`, on a case-sensitive volume) are ambiguous and never guessed; under the Linux rule, each finds its own | `TestHeldCaseTwinsAreNeverGuessed` | HELD |

## 3. Held by reasoning or by the builder's existing tests

- **A link at `prefs.js`, a pipe, over 16 MB, a link swapped in before the rename:** covered by the
  builder's MA-1195 tests, which pass beside the attack files. `ReadRegularFile` checks with
  `Lstat`, then opens without following links and without blocking, then checks the opened file
  itself, so a pipe or link swapped in after the check is refused, not waited on or followed.
- **A link at the profile folder or at a parent folder:** it is followed, on read and on write. I
  judge this correct: people do link a profile to another disk, and Thunderbird follows it too. Only
  the last part of the path, `prefs.js` itself, is planted-file territory.
- **A crash between the backup and the rename:** `prefs.js` is either the old bytes or the new
  ones (temp file, sync, rename, folder sync). After a `kill -9`, a `.mailarchive-prefs-*.tmp`
  file (0600) can be left in the profile. Thunderbird reads only `prefs.js` and `user.js`, so it
  ignores the file. Nothing cleans it up (the archive's orphan sweep covers archives only). Not a
  finding.
- **Windows:** the open there cannot refuse a link by itself (no `O_NOFOLLOW`; the design says so),
  so a link swapped in between the `Lstat` and the open would be read through. Doing that needs
  write access to the profile, the result is only scanned for `user_pref` lines, and the write side
  is still safe because the rename replaces the name. A junction at `prefs.js` is refused (Go
  reports it as a folder). Reasoned only; I did not run it on the VM.
- **Folder-name variants:** `[ProfD]` with `\\`, a trailing slash, `..` inside `directory-rel`,
  NFC vs NFD names, `imap.gmail.com` vs `imap.gmail.com-1`, and a name with the same ending are all
  in the builder's table and pass. An absolute `directory` outside the profile matches only that
  exact folder. A `\"` escape in a path makes the comparison miss, which falls back to the hostname
  rule and never yields a wrong account for the two-account case. On Unix, `\` and `/` compare
  alike, so a folder literally named `a\b` equals `a/b`; no Thunderbird path makes that, so I did
  not file it.
- **Thunderbird running:** the command line refuses (`main.go:2124`); the wizard waits for "I've
  quit it" (`wizard.go:1578`). What is left: if Thunderbird starts between that check and the
  rename, it can write over the change at its next settings save, and nothing tells the person.
  That is inherent; a one-line "start Thunderbird only after this finishes" would cover it.
- **Mode and owner:** the new `prefs.js` is 0600. I recall that Thunderbird writes it 0600 itself,
  but I did not check its source. On Mac and Linux, the rename gives the file the owner of whoever
  runs MailArchive. Run under `sudo`, it would leave a root-owned `prefs.js` that Thunderbird could
  not read; the old in-place write kept the owner. Not tested (it needs root); LOW, worth one
  sentence in the design.
- **`tbrestore`'s own reader** (`tbrestore/prefs.go:91-115`): it checks with `Lstat`, then calls a
  plain `os.Open`, which follows links and waits on a pipe. So a pipe swapped in after the check
  would hang it. It is attended (restore, with Thunderbird closed), never writes `prefs.js`, and C0c
  does not call it, so it is not a C0c finding. Its next pass should move it to
  `util.ReadRegularFile`. It is also the reader that applies `user.js` and decodes escapes;
  `thunderbird.parsePrefs` does neither, hence CON-06.
- **Other callers of the old matching:** `FindAccountForStore` is called only by
  `wizard.go:1575` and `main.go:2121`, and `EnableOffline` only by `wizard.go:1595` and
  `main.go:2133`. No other code matches servers by hostname.

## 4. What this pass did not do

I did not run on Windows or Linux, and nothing ran against a real Thunderbird. Thunderbird's own
handling (CRLF line endings, file mode 0600, deleting a removed account's lines) is recalled, not
read from its source. The cheapest way to settle all three is one look at Mozilla's
`modules/libpref/Preferences.cpp` (the pref writer) and `nsMsgAccountManager::RemoveIncomingServer`.

Severity is my own judgement. The lead decides which findings block the merge. My
recommendation: CON-01, INS-01 and OUT-01 before merge (each is a small change), and the LOW
items as notes.
