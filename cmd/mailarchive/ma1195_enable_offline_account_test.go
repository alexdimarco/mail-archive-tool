package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// twoAccountProfile makes a Thunderbird profile with two IMAP accounts on imap.gmail.com,
// each set to off, in ImapMail/imap.gmail.com (server1) and ImapMail/imap.gmail.com-1
// (server2), and returns the profile folder.
func twoAccountProfile(t *testing.T) string {
	t.Helper()
	profile := t.TempDir()
	for _, s := range []string{"imap.gmail.com", "imap.gmail.com-1"} {
		if err := os.MkdirAll(filepath.Join(profile, "ImapMail", s), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	prefs := "user_pref(\"mail.accountmanager.accounts\", \"account1,account2\");\n" +
		"user_pref(\"mail.account.account1.server\", \"server1\");\n" +
		"user_pref(\"mail.account.account2.server\", \"server2\");\n" +
		"user_pref(\"mail.server.server1.hostname\", \"imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server1.type\", \"imap\");\n" +
		"user_pref(\"mail.server.server1.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server1.offline_download\", false);\n" +
		"user_pref(\"mail.server.server2.hostname\", \"imap.gmail.com\");\n" +
		"user_pref(\"mail.server.server2.type\", \"imap\");\n" +
		"user_pref(\"mail.server.server2.directory-rel\", \"[ProfD]ImapMail/imap.gmail.com-1\");\n" +
		"user_pref(\"mail.server.server2.offline_download\", false);\n"
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile
}

// covers: MA-1195, R9
// -enable-offline turns on the account the store belongs to, never another account on the
// same server (C0c, the 10-lens review's side finding A: the old match could pick either
// account for ImapMail/imap.gmail.com, then switch on the wrong one and report the right
// one as done). For each store of a two-account profile, only its own account's line
// becomes true. Repeated on fresh profiles, since the old bug showed only in some map
// orders. The wizard's Yes, prepare calls the same FindAccountForStore and EnableOffline
// (wizard.go), so this proves its account too; its dialog is not driven here.
func TestEnableOfflineActsOnTheMatchedAccount(t *testing.T) {
	on := func(key string) string { return `user_pref("mail.server.` + key + `.offline_download", true);` }
	off := func(key string) string { return `user_pref("mail.server.` + key + `.offline_download", false);` }
	for i := 0; i < 20; i++ {
		for _, tc := range []struct{ store, mine, other string }{
			{"imap.gmail.com", "server1", "server2"},
			{"imap.gmail.com-1", "server2", "server1"},
		} {
			profile := twoAccountProfile(t)
			enableOfflineLog(t, filepath.Join(profile, "ImapMail", tc.store))
			b, err := os.ReadFile(filepath.Join(profile, "prefs.js"))
			if err != nil {
				t.Fatal(err)
			}
			got := assure.Reached(t, string(b), "prefs.js after -enable-offline")
			if !strings.Contains(got, on(tc.mine)) {
				t.Fatalf("run %d, store %s: its own account %s was not turned on:\n%s", i, tc.store, tc.mine, got)
			}
			if strings.Contains(got, on(tc.other)) || !strings.Contains(got, off(tc.other)) {
				t.Fatalf("run %d, store %s: the other account %s was changed:\n%s", i, tc.store, tc.other, got)
			}
		}
	}
}
