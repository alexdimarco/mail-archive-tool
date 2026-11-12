package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1284
// The account's name as Thunderbird shows it (the owner's ruling of 2026-11-08 on the C3 friction
// review's M1) is read from the same one read of prefs.js and user.js through C0c's allow-list
// reader, as nsMsgIncomingServer::GetPrettyName reads it (ESR 153 nsMsgIncomingServer.cpp:589-595,
// GetStringValue :558-562): the account's own mail.server.<key>.name, else mail.server.default.name.
// Only a quoted text is taken; an empty one, a value of another type, or none at all is no name
// (Thunderbird then builds one from words MailArchive does not model), and the words fall back to
// the folder. The name never decides the account: a mistyped name still matches. Positive twin
// first: a profile with no name line matches its account and has no name.
func TestTheAccountsNameIsReadAsThunderbirdShowsIt(t *testing.T) {
	p, store := factsProfile(t)
	f, err := ReadStoreFacts(p, store)
	if err != nil || f.Account == nil || f.Name != "" {
		t.Fatalf("positive twin: no name line: %+v %v", f, err)
	}
	for _, c := range []struct {
		what  string
		lines []string
		want  string
	}{
		{"its own name", []string{`user_pref("mail.server.server1.name", "sam@c2.example");`}, "sam@c2.example"},
		{"the default's name", []string{`user_pref("mail.server.default.name", "Mail");`}, "Mail"},
		{"its own beats the default's", []string{`user_pref("mail.server.default.name", "Mail");`, `user_pref("mail.server.server1.name", "Work");`}, "Work"},
		{"an empty own name is none (not the default's)", []string{`user_pref("mail.server.default.name", "Mail");`, `user_pref("mail.server.server1.name", "");`}, ""},
		{"a name that is not text", []string{`user_pref("mail.server.server1.name", true);`}, ""},
		{"a number for a name", []string{`user_pref("mail.server.server1.name", 5);`}, ""},
		// Thunderbird may fall back past a mistyped own name to the default's; how libpref types it
		// was not checked, so MailArchive does not guess: no name, the folder words.
		{"a mistyped own name is none (not the default's)", []string{`user_pref("mail.server.default.name", "Mail");`, `user_pref("mail.server.server1.name", true);`}, ""},
		{"escapes decoded as Thunderbird decodes them", []string{`user_pref("mail.server.server1.name", "Sam \"Work\" \\ mail");`}, `Sam "Work" \ mail`},
		{"the last statement wins", []string{`user_pref("mail.server.server1.name", "old");`, `user_pref("mail.server.server1.name", "new");`}, "new"},
		{"another account's name is never this one's", []string{`user_pref("mail.server.server2.name", "Local Folders");`}, ""},
	} {
		p, store := factsProfile(t, c.lines...)
		f, err := ReadStoreFacts(p, store)
		if err != nil || f.Account == nil || f.Account.ServerKey != "server1" {
			t.Errorf("%s: the account does not match: %+v %v", c.what, f, err)
			continue
		}
		if f.Name != c.want {
			t.Errorf("%s: name %q, want %q", c.what, f.Name, c.want)
		}
	}
	// user.js over prefs.js, as Thunderbird applies them.
	p, store = factsProfile(t, `user_pref("mail.server.server1.name", "from prefs");`)
	if err := os.WriteFile(filepath.Join(p, "user.js"), []byte(`user_pref("mail.server.server1.name", "from user.js");`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if f, err := ReadStoreFacts(p, store); err != nil || f.Name != "from user.js" {
		t.Errorf("user.js over prefs.js: %q %v", f.Name, err)
	}
	// A file MailArchive cannot read with certainty gives no account, so no name (C0c's rule).
	p, store = factsProfile(t, `user_pref("mail.server.server1.name", "sam@c2.example"); // a comment`)
	if f, _ := ReadStoreFacts(p, store); f.Account != nil || f.Name != "" {
		t.Errorf("a file read without certainty: %+v", f)
	}
}
