package thunderbird

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// prefLine is one prefs.js line setting key to the string value, as Thunderbird writes it
// (a backslash doubled inside the quotes).
func prefLine(key, value string) string {
	return fmt.Sprintf("user_pref(%q, \"%s\");\n", key, strings.ReplaceAll(value, `\`, `\\`))
}

// accountsLine is mail.accountmanager.accounts plus each account's server, as Thunderbird
// writes them, from (account id, server key) pairs: only these servers are live accounts.
func accountsLine(pairs ...string) string {
	var ids []string
	s := ""
	for i := 0; i+1 < len(pairs); i += 2 {
		ids = append(ids, pairs[i])
		s += prefLine("mail.account."+pairs[i]+".server", pairs[i+1])
	}
	return prefLine("mail.accountmanager.accounts", strings.Join(ids, ",")) + s
}

// live is accountsLine for servers "serverN", each the server of account "accountN".
func live(servers ...string) string {
	var pairs []string
	for _, s := range servers {
		pairs = append(pairs, "account"+strings.TrimPrefix(s, "server"), s)
	}
	return accountsLine(pairs...)
}

// imapServer is the prefs.js lines of one IMAP server. rel and dir are left out when empty.
func imapServer(key, host, rel, dir string) string {
	s := prefLine("mail.server."+key+".hostname", host) + prefLine("mail.server."+key+".type", "imap")
	if rel != "" {
		s += prefLine("mail.server."+key+".directory-rel", rel)
	}
	if dir != "" {
		s += prefLine("mail.server."+key+".directory", dir)
	}
	return s
}

// profileWith makes a profile folder holding prefs.js and an empty folder under ImapMail/
// for each store name, and returns the profile folder.
func profileWith(t *testing.T, prefs string, stores ...string) string {
	t.Helper()
	profile := t.TempDir()
	for _, s := range stores {
		if err := os.MkdirAll(filepath.Join(profile, "ImapMail", s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile
}

// accountKey is the server key FindAccountForStore gives the store, failing the test if it
// finds none.
func accountKey(t *testing.T, profile, store string) string {
	t.Helper()
	acct, err := FindAccountForStore(profile, filepath.Join(profile, "ImapMail", store))
	if err != nil || acct == nil {
		t.Fatalf("store %s: no account (%v)", store, err)
	}
	return assure.Reached(t, acct.ServerKey, "the matched server key")
}

// covers: MA-1195, R9
// Two accounts on one server each find their own account (C0c, the 10-lens review's side
// finding A). FindAccountForStore used to return the first server, in Go's random map
// order, whose hostname equals the store folder's name, so with ImapMail/imap.gmail.com and
// ImapMail/imap.gmail.com-1 the store imap.gmail.com got either account. It now matches the
// store folder against each server's directory-rel and directory by whole path components.
// Each lookup is repeated, since the old bug showed only in some map orders. Positive twin
// first: one account on one host with no folder setting is found by its hostname, as today.
func TestEachStoreOfOneServerFindsItsOwnAccount(t *testing.T) {
	one := profileWith(t, live("server3")+imapServer("server3", "mail.example.com", "", ""), "mail.example.com")
	if got := accountKey(t, one, "mail.example.com"); got != "server3" {
		t.Fatalf("the positive twin: one account by hostname = %s, want server3", got)
	}

	t.Run("by directory-rel", func(t *testing.T) {
		p := profileWith(t, live("server1", "server2")+imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", "")+
			imapServer("server2", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", ""),
			"imap.gmail.com", "imap.gmail.com-1")
		for i := 0; i < 50; i++ {
			if got := accountKey(t, p, "imap.gmail.com"); got != "server1" {
				t.Fatalf("lookup %d: imap.gmail.com = %s, want server1", i, got)
			}
			if got := accountKey(t, p, "imap.gmail.com-1"); got != "server2" {
				t.Fatalf("lookup %d: imap.gmail.com-1 = %s, want server2", i, got)
			}
		}
	})

	t.Run("by directory", func(t *testing.T) {
		base := t.TempDir()
		pre := live("server1", "server2") + imapServer("server1", "imap.gmail.com", "", filepath.Join(base, "ImapMail", "imap.gmail.com")) +
			imapServer("server2", "imap.gmail.com", "", filepath.Join(base, "ImapMail", "imap.gmail.com-1"))
		if err := os.WriteFile(filepath.Join(base, "prefs.js"), []byte(pre), 0o600); err != nil {
			t.Fatal(err)
		}
		for _, s := range []string{"imap.gmail.com", "imap.gmail.com-1"} {
			if err := os.MkdirAll(filepath.Join(base, "ImapMail", s), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 50; i++ {
			if got := accountKey(t, base, "imap.gmail.com"); got != "server1" {
				t.Fatalf("lookup %d: imap.gmail.com = %s, want server1", i, got)
			}
			if got := accountKey(t, base, "imap.gmail.com-1"); got != "server2" {
				t.Fatalf("lookup %d: imap.gmail.com-1 = %s, want server2", i, got)
			}
		}
	})

	t.Run("a name that ends the same", func(t *testing.T) {
		// The old directory-rel test was a string suffix, so xmail.example.com's folder
		// matched the store mail.example.com.
		p := profileWith(t, live("server1", "server2")+imapServer("server1", "xmail.example.com", "[ProfD]ImapMail/xmail.example.com", "")+
			imapServer("server2", "mail.example.com", "[ProfD]ImapMail/mail.example.com", ""),
			"xmail.example.com", "mail.example.com")
		for i := 0; i < 50; i++ {
			if got := accountKey(t, p, "mail.example.com"); got != "server2" {
				t.Fatalf("lookup %d: mail.example.com = %s, want server2", i, got)
			}
		}
	})
}

// covers: MA-1195, R9
// When the store folder still does not tell which account it is, no account is returned:
// never a guess. hostname is used only when exactly one server has it, and only for a
// server whose own folder setting (if any) names a folder of the store's name. Positive
// twin first: a moved profile whose directory still names the old place, with no
// directory-rel, is found by its one hostname.
func TestAnAccountIsNeverGuessed(t *testing.T) {
	moved := profileWith(t, live("server4")+imapServer("server4", "mail.example.com", "", "/old/place/ImapMail/mail.example.com"), "mail.example.com")
	if got := accountKey(t, moved, "mail.example.com"); got != "server4" {
		t.Fatalf("the positive twin: a moved profile = %s, want server4", got)
	}

	cases := []struct {
		name, prefs, store string
	}{
		{"two servers with the hostname and no folder setting",
			imapServer("server1", "imap.gmail.com", "", "") + imapServer("server2", "imap.gmail.com", "", ""),
			"imap.gmail.com"},
		{"a leftover folder of a server whose own folder is another",
			imapServer("server1", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com-1", ""),
			"imap.gmail.com"},
		{"two servers naming the same folder",
			imapServer("server1", "a.example.com", "[ProfD]ImapMail/shared", "") +
				imapServer("server2", "b.example.com", "[ProfD]ImapMail/shared", ""),
			"shared"},
		{"a -1 folder with only hostnames",
			imapServer("server1", "imap.gmail.com", "", "") + imapServer("server2", "imap.gmail.com", "", ""),
			"imap.gmail.com-1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := profileWith(t, live("server1", "server2")+tc.prefs, tc.store)
			for i := 0; i < 50; i++ {
				acct, err := FindAccountForStore(p, filepath.Join(p, "ImapMail", tc.store))
				if acct != nil {
					t.Fatalf("lookup %d: guessed %s for %s", i, acct.ServerKey, tc.store)
				}
				rc, msg := 0, ""
				if err != nil {
					rc, msg = 1, err.Error()
				}
				assure.Refused(t, rc, msg, assure.Code(1), assure.Names("prefs.js", tc.store))
			}
		})
	}
}

// covers: MA-1195, MA-1265
// Store paths compare by whole path components after one normalising: Unicode NFC, on Windows "\"
// and "/" alike (a doubled "\\" is one separator), and case-folded on Windows and macOS only (§4.1
// step 2's fold, used by C0c's match). On macOS and Linux "\" is part of a name, so a Windows path
// there is one odd name, equal to no path with slashes (MA-1265, the C0c pass round 4's R4-03;
// before it these two rows read true on every OS). Positive twin first: a path equals itself on
// every OS.
func TestStorePathsCompareByWholeComponents(t *testing.T) {
	all := []string{"linux", "darwin", "windows"}
	for _, goos := range all {
		if !samePath("/p/ImapMail/imap.gmail.com", "/p/ImapMail/imap.gmail.com", goos) {
			t.Fatalf("the positive twin on %s: a path does not equal itself", goos)
		}
	}
	cases := []struct {
		a, b string
		want map[string]bool
	}{
		{"/p/ImapMail/imap.gmail.com", "/p/ImapMail/imap.gmail.com-1", map[string]bool{"linux": false, "darwin": false, "windows": false}},
		{"/p/ImapMail/mail.example.com", "/p/ImapMail/xmail.example.com", map[string]bool{"linux": false, "darwin": false, "windows": false}},
		{"/p/ImapMail/imap.gmail.com/", "/p/ImapMail/imap.gmail.com", map[string]bool{"linux": true, "darwin": true, "windows": true}},
		{`C:\\Users\\a\\ImapMail\\x`, `C:\Users\a\ImapMail\x`, map[string]bool{"linux": false, "darwin": false, "windows": true}},
		{`C:\Users\a\ImapMail\x`, "C:/Users/a/ImapMail/x", map[string]bool{"linux": false, "darwin": false, "windows": true}},
		{`/p/ImapMail\x`, "/p/ImapMail/x", map[string]bool{"linux": false, "darwin": false, "windows": true}},
		{`/p/ImapMail\x`, `/p/ImapMail\x`, map[string]bool{"linux": true, "darwin": true, "windows": true}},
		{"/P/imapmail/IMAP.gmail.com", "/p/ImapMail/imap.gmail.com", map[string]bool{"linux": false, "darwin": true, "windows": true}},
		{"/p/Caf\u00e9", "/p/Cafe\u0301", map[string]bool{"linux": true, "darwin": true, "windows": true}},
		{"/p/a/../ImapMail/x", "/p/ImapMail/x", map[string]bool{"linux": true, "darwin": true, "windows": true}},
		{"", "/p", map[string]bool{"linux": false, "darwin": false, "windows": false}},
		{"", "", map[string]bool{"linux": false, "darwin": false, "windows": false}},
		{"/p/ImapMail/x", "p/ImapMail/x", map[string]bool{"linux": false, "darwin": false, "windows": false}},
	}
	for _, tc := range cases {
		for _, goos := range all {
			if got := samePath(tc.a, tc.b, goos); got != tc.want[goos] {
				t.Errorf("samePath(%q, %q, %s) = %v, want %v", tc.a, tc.b, goos, got, tc.want[goos])
			}
		}
	}
}
