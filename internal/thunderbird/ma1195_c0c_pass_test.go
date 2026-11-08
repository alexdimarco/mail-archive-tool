package thunderbird

// The C0c adversarial pass's findings (docs/review-thunderbird-completeness-c0c-adversarial.md),
// each brought in from the attack clone and kept as a permanent guard.

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// noAccount asserts FindAccountForStore gave no account for store, with an error naming
// prefs.js and the store folder, and returns the error's words.
func noAccount(t *testing.T, profile, store string) string {
	t.Helper()
	acct, err := FindAccountForStore(profile, filepath.Join(profile, "ImapMail", store))
	if acct != nil {
		t.Fatalf("store %s was given %s", store, acct.ServerKey)
	}
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("prefs.js", store))
	return msg
}

// covers: MA-1195, R9
// Only a live account owns a store (CON-01): the servers of the accounts listed in
// mail.accountmanager.accounts (the last line wins, as in Thunderbird), each by its
// mail.account.<id>.server. A server whose account was removed but whose lines stayed is
// never an owner, so it neither blocks the live account nor takes a leftover folder.
// Positive twin first: a live server is found by its folder.
func TestOnlyALiveAccountOwnsAStore(t *testing.T) {
	healthy := profileWith(t, live("server3")+imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", ""), "imap.gmail.com")
	if got := accountKey(t, healthy, "imap.gmail.com"); got != "server3" {
		t.Fatalf("the positive twin = %s, want server3", got)
	}

	t.Run("a ghost sharing the live account's folder does not block it", func(t *testing.T) {
		prefs := accountsLine("account3", "server3") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + // ghost
			imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") // live
		p := profileWith(t, prefs, "imap.gmail.com")
		if got := accountKey(t, p, "imap.gmail.com"); got != "server3" {
			t.Fatalf("want the live server3, got %s", got)
		}
	})
	t.Run("a ghost does not own a leftover folder", func(t *testing.T) {
		prefs := accountsLine("account3", "server3") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + // ghost
			imapServer("server3", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "") // live
		p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
		noAccount(t, p, "imap.gmail.com")
		if got := accountKey(t, p, "imap.gmail.com-1"); got != "server3" {
			t.Fatalf("the live account's own folder = %s, want server3", got)
		}
	})
	t.Run("the last accounts line wins", func(t *testing.T) {
		prefs := accountsLine("account1", "server1") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") +
			prefLine("mail.accountmanager.accounts", "account9")
		noAccount(t, profileWith(t, prefs, "imap.gmail.com"), "imap.gmail.com")
	})
	t.Run("no accounts line", func(t *testing.T) {
		p := profileWith(t, imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", ""), "imap.gmail.com")
		if msg := noAccount(t, p, "imap.gmail.com"); !strings.Contains(msg, "prefs.js lists no account (mail.accountmanager.accounts)") {
			t.Fatalf("the refusal does not say prefs.js lists no account: %s", msg)
		}
	})
}

// covers: MA-1195, R9
// The hostname fallback is used only when exactly one live server has the hostname
// (§4.1 step 1 item 2; INS-02): two servers on imap.gmail.com, one with no folder setting
// and one whose folder is imap.gmail.com-1, give the leftover imap.gmail.com no account,
// never the one without a folder setting. Positive twin first: one server on the host
// with no folder setting is found by its hostname.
func TestTheHostnameFallbackNeedsOneServerOnTheHost(t *testing.T) {
	one := profileWith(t, live("server1")+imapServer("server1", "imap.gmail.com", "", ""), "imap.gmail.com")
	if got := accountKey(t, one, "imap.gmail.com"); got != "server1" {
		t.Fatalf("the positive twin = %s, want server1", got)
	}
	prefs := live("server1", "server2") + imapServer("server1", "imap.gmail.com", "", "") +
		imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "")
	p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
	if msg := noAccount(t, p, "imap.gmail.com"); !strings.Contains(msg, "more than one account") {
		t.Fatalf("the refusal does not say two accounts could own it: %s", msg)
	}
	if got := accountKey(t, p, "imap.gmail.com-1"); got != "server2" {
		t.Fatalf("server2's own folder = %s, want server2", got)
	}
}

// offProfile is a one-account profile (server1, live) whose prefs.js ends with body, and its
// store.
func offProfile(t *testing.T, body string) (string, string) {
	t.Helper()
	p := profileWith(t, live("server1")+imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+body, "imap.gmail.com")
	return p, filepath.Join(p, "ImapMail", "imap.gmail.com")
}

// accountOn is FindAccountForStore's OfflineDownload for store, failing if there is no account.
func accountOn(t *testing.T, profile, store string) bool {
	t.Helper()
	acct, err := FindAccountForStore(profile, store)
	if err != nil || acct == nil {
		t.Fatalf("no account (%v)", err)
	}
	return acct.OfflineDownload
}

const (
	on1  = `user_pref("mail.server.server1.offline_download", true);`
	off1 = `user_pref("mail.server.server1.offline_download", false);`
)

// covers: MA-1195, R9
// EnableOffline acts on the line Thunderbird applies, the last one (CON-02), and when it
// changes the setting it changes every line for it, so the file says one thing; changed is
// true only when the account was off. Positive twin first: one off line is turned on.
func TestEnableOfflineActsOnTheLastLine(t *testing.T) {
	p, store := offProfile(t, off1+"\n")
	if c, err := EnableOffline(p, "server1"); err != nil || !c.Changed || !accountOn(t, p, store) {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}

	t.Run("two off lines", func(t *testing.T) {
		p, store := offProfile(t, off1+"\n"+off1+"\n")
		c, err := EnableOffline(p, "server1")
		if err != nil || !c.Changed {
			t.Fatalf("changed %v, err %v", c.Changed, err)
		}
		if !accountOn(t, p, store) {
			t.Fatal("EnableOffline said changed, yet the last line (the one Thunderbird applies) is still false")
		}
		b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
		if strings.Contains(string(b), off1) || strings.Count(string(b), on1) != 2 {
			t.Fatalf("the file does not say one thing:\n%s", b)
		}
	})
	t.Run("on, then off", func(t *testing.T) {
		p, store := offProfile(t, on1+"\n"+off1+"\n")
		if c, err := EnableOffline(p, "server1"); err != nil || !c.Changed || !accountOn(t, p, store) {
			t.Fatalf("an account whose last line is off: %+v, %v", c, err)
		}
	})
	t.Run("off, then on", func(t *testing.T) {
		body := off1 + "\n" + on1 + "\n"
		p, _ := offProfile(t, body)
		before, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
		c, err := EnableOffline(p, "server1")
		if err != nil || c.Changed {
			t.Fatalf("an account whose last line is on was reported changed: %+v, %v", c, err)
		}
		if !bytesStill(filepath.Join(p, "prefs.js"), before)() || !absent(filepath.Join(p, "prefs.js.mailarchive-bak")) {
			t.Fatal("an account already on was written to")
		}
	})
}

// covers: MA-1195, R9
// A commented-out line never counts (CON-03): //, # and /* */ (on one line or across lines),
// for EnableOffline and for FindAccountForStore. Positive twin first: a live true line is
// "already on".
func TestACommentedLineNeverCounts(t *testing.T) {
	p, _ := offProfile(t, on1+"\n")
	if c, err := EnableOffline(p, "server1"); err != nil || c.Changed {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}

	for name, comment := range map[string]string{
		"//":             "// " + on1 + "\n",
		"#":              "# " + on1 + "\n",
		"/* */ one line": "/* " + on1 + " */\n",
		"/* */ across":   "/*\n" + on1 + "\n*/\n",
	} {
		t.Run(name, func(t *testing.T) {
			p, store := offProfile(t, comment+off1+"\n")
			c, err := EnableOffline(p, "server1")
			if err != nil || !c.Changed {
				t.Fatalf("changed %v, err %v", c.Changed, err)
			}
			if !accountOn(t, p, store) {
				t.Fatal("after EnableOffline the account is still off (a comment was taken for the setting)")
			}
			b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
			if !strings.Contains(string(b), comment) {
				t.Fatalf("the comment was changed:\n%s", b)
			}
		})
	}

	t.Run("a commented folder setting", func(t *testing.T) {
		// Counted, server2's commented line would make the store ambiguous.
		prefs := live("server1", "server2") +
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") +
			imapServer("server2", "other.example.com", "", "") +
			"/*\n" + prefLine("mail.server.server2.directory-rel", "[ProfD]ImapMail/imap.gmail.com") + "*/\n"
		p := profileWith(t, prefs, "imap.gmail.com")
		if got := accountKey(t, p, "imap.gmail.com"); got != "server1" {
			t.Fatalf("a commented directory-rel counted: %s", got)
		}
	})
}

// covers: MA-1195, S10
// The first backup is kept (CON-04): -enable-offline loops over every store, and a second
// account's change never overwrites prefs.js.mailarchive-bak, which keeps prefs.js as it was
// before MailArchive first changed it; the second call says so (BackupKept). Positive twin
// first: the first change writes the backup of the original.
func TestTheFirstBackupIsKept(t *testing.T) {
	prefs := live("server1", "server2") +
		imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "") + off1 + "\n" +
		imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", "") +
		`user_pref("mail.server.server2.offline_download", false);` + "\n"
	p := profileWith(t, prefs, "imap.gmail.com", "imap.gmail.com-1")
	first, err := EnableOffline(p, "server1")
	if err != nil || first.BackupKept {
		t.Fatalf("the positive twin: %+v, %v", first, err)
	}
	if b, _ := os.ReadFile(assure.Reached(t, first.Backup, "the first backup")); string(b) != prefs {
		t.Fatalf("the positive twin's backup is not the original:\n%s", b)
	}
	second, err := EnableOffline(p, "server2")
	if err != nil || !second.Changed {
		t.Fatalf("the second change: %+v, %v", second, err)
	}
	if b, _ := os.ReadFile(assure.Reached(t, second.Backup, "the second call's backup")); string(b) != prefs {
		t.Fatalf("the backup is not the original prefs.js any more:\n%s", b)
	}
	if !second.BackupKept {
		t.Fatal("the second call does not say it kept the first backup")
	}
}

// covers: MA-1195
// prefs.js keeps its own line ending (CON-05): a CRLF file stays CRLF on the append path and
// the replace path, and an LF file stays LF. Positive twin first: an LF file gains an LF line.
func TestPrefsJsKeepsItsLineEnding(t *testing.T) {
	p, _ := offProfile(t, "")
	if _, err := EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(p, "prefs.js")); bytes.Contains(b, []byte("\r")) || !bytes.HasSuffix(b, []byte(on1+"\n")) {
		t.Fatalf("the positive twin: %q", b)
	}
	for name, body := range map[string]string{"append": "", "replace": off1 + "\n"} {
		t.Run(name, func(t *testing.T) {
			crlf := strings.ReplaceAll(live("server1")+imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+body, "\n", "\r\n")
			p := profileWith(t, crlf, "imap.gmail.com")
			if _, err := EnableOffline(p, "server1"); err != nil {
				t.Fatal(err)
			}
			b, _ := os.ReadFile(filepath.Join(p, "prefs.js"))
			if strings.Count(string(b), "\n") != strings.Count(string(b), "\r\n") || !strings.Contains(string(b), on1+"\r\n") {
				t.Fatalf("a bare LF line was added to a CRLF prefs.js: %q", b)
			}
		})
	}
}

// covers: MA-1195, S10
// EnableOffline changes only the one setting (held in the pass, kept as a guard over the
// line-based rewrite): every other byte is identical, including a \\ and \" value; no temp
// file is left; the mode is 0600; a hard link to prefs.js is not written through.
func TestEnableOfflineChangesOnlyTheOneSetting(t *testing.T) {
	head := "// Mozilla User Preferences\n\n" + `user_pref("a.b", "x\\y \"q\"");` + "\n"
	tail := "\n" + `user_pref("z", 1);` + "\n"
	p, _ := offProfile(t, head+off1+tail)
	prefsPath := filepath.Join(p, "prefs.js")
	before, _ := os.ReadFile(prefsPath)
	other := filepath.Join(t.TempDir(), "hardlink")
	if err := os.Link(prefsPath, other); err != nil {
		t.Skip("no hard links:", err)
	}
	if _, err := EnableOffline(p, "server1"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(prefsPath)
	want := bytes.Replace(before, []byte(off1), []byte(on1), 1)
	if !bytes.Equal(assure.Reached(t, after, "prefs.js after"), want) {
		t.Fatalf("bytes beyond the one setting changed:\n%q\nwant\n%q", after, want)
	}
	if hl, _ := os.ReadFile(other); !bytes.Equal(hl, before) {
		t.Fatalf("the hard link was written through")
	}
	ents, _ := os.ReadDir(p)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left: %s", e.Name())
		}
	}
	if fi, _ := os.Stat(prefsPath); runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode().Perm())
	}
}

// covers: MA-1195, R25
// The 16 MB bound is exact (held in the pass, kept as a guard): exactly the bound is read,
// one byte more is refused with no account and an error naming prefs.js.
func TestThePrefsBoundIsExact(t *testing.T) {
	head := live("server1") + imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")
	pad := func(n int) string { return head + strings.Repeat(" ", n-len(head)) }
	p, store := offProfile(t, "")
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(pad(prefsMaxBytes)), 0o600); err != nil {
		t.Fatal(err)
	}
	if a, err := FindAccountForStore(p, store); err != nil || a == nil {
		t.Fatalf("exactly the bound: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(pad(prefsMaxBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	a, err := FindAccountForStore(p, store)
	if a != nil {
		t.Fatalf("over the bound: an account")
	}
	rc, msg := 0, ""
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("prefs.js"))
}

// covers: MA-1195
// Two folders that differ only by case (a case-sensitive volume) are ambiguous under the
// macOS rule, never guessed, and each finds its own under the Linux rule (held in the pass,
// kept as a guard).
func TestCaseTwinsAreNeverGuessed(t *testing.T) {
	prefs := map[string]string{}
	for k, v := range map[string]string{
		"mail.accountmanager.accounts":      "account1,account2",
		"mail.account.account1.server":      "server1",
		"mail.account.account2.server":      "server2",
		"mail.server.server1.hostname":      "a.example.com",
		"mail.server.server1.directory-rel": "[ProfD]ImapMail/Mail",
		"mail.server.server2.hostname":      "b.example.com",
		"mail.server.server2.directory-rel": "[ProfD]ImapMail/mail",
	} {
		prefs[k] = `"` + v + `"`
	}
	if a, _ := matchAccount(prefs, "/p", "/p/ImapMail/mail", "linux"); a == nil || a.ServerKey != "server2" {
		t.Fatalf("the positive twin, linux: %+v", a)
	}
	if a, err := matchAccount(prefs, "/p", "/p/ImapMail/mail", "darwin"); a != nil {
		t.Fatalf("darwin guessed %s (%v)", a.ServerKey, err)
	}
}
