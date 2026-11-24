package thunderbird

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ownerProfile is a profile folder holding the store folders dirs under ImapMail/, with a
// compatibility.ini as Thunderbird writes it; prefs writes its prefs.js from lines.
func ownerProfile(t *testing.T, dirs ...string) (profile string, prefs func(lines ...string)) {
	t.Helper()
	profile = t.TempDir()
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(profile, "ImapMail", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(profile, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return profile, func(lines ...string) {
		t.Helper()
		body := "// Mozilla User Preferences\n\n" + strings.Join(lines, "\n") + "\n"
		if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

// ownerAcct is one live account's prefs.js lines: account n on server n, host, and its folder under
// ImapMail/ ("" for no folder setting), named name ("" for none).
func ownerAcct(n, host, folder, name string) []string {
	id, key := "account"+n, "server"+n
	lines := []string{
		`user_pref("mail.account.` + id + `.server", "` + key + `");`,
		`user_pref("mail.server.` + key + `.hostname", "` + host + `");`,
		`user_pref("mail.server.` + key + `.type", "imap");`,
	}
	if folder != "" {
		lines = append(lines, `user_pref("mail.server.`+key+`.directory-rel", "[ProfD]ImapMail/`+folder+`");`)
	}
	if name != "" {
		lines = append(lines, `user_pref("mail.server.`+key+`.name", "`+name+`");`)
	}
	return lines
}

// ownerPrefs is the prefs.js lines of the accounts, listed in mail.accountmanager.accounts.
func ownerPrefs(accts ...[]string) []string {
	var lines, ids []string
	for i, a := range accts {
		lines = append(lines, a...)
		ids = append(ids, "account"+string(rune('1'+i)))
	}
	return append(lines, `user_pref("mail.accountmanager.accounts", "`+strings.Join(ids, ",")+`");`)
}

// covers: MA-1504, R12
// C2's second condition (docs/design-per-mailbox-archives.md §11, as revision 2.1's F-01 corrected
// it): NoLiveOwner says, from the same strict read of prefs.js and user.js, that no live account uses
// a store folder: no live account's folder setting names it, and every live account whose server name
// matches the folder's name has a folder setting naming another folder. Positive twins first (it says
// no): the account still names the folder; two accounts on one server, one of which still names it; a
// same-server account with no folder setting; settings that cannot be read for certain; no account
// listed; a folder not directly under ImapMail/ or Mail/. Then F-01's case: two accounts on one mail
// server, the first removed (its folder left) and added again, which Thunderbird keeps at "-2": for
// the old folder ReadStoreFacts says NoAccountAmbiguous, and NoLiveOwner says no live owner. One
// account on its server, re-added at "-1", and the second account's leftover "-1" folder beside a
// re-added "-2", say no live owner too.
func TestNoLiveOwnerSaysNoAccountUsesTheFolder(t *testing.T) {
	profile, prefs := ownerProfile(t, "imap.gmail.com", "imap.gmail-1.com", "imap.gmail-2.com", "qz.nested")
	old := filepath.Join(profile, "ImapMail", "imap.gmail.com")
	a := ownerAcct("1", "imap.gmail.com", "imap.gmail.com", "qza@gmail.example")
	b := ownerAcct("2", "imap.gmail.com", "imap.gmail-1.com", "qzb@gmail.example")

	// Positive twins: some live account may use the folder, so the answer is no.
	prefs(ownerPrefs(a, b)...)
	if NoLiveOwner(profile, old) {
		t.Error("positive twin: the account whose folder setting names the folder still owns it")
	}
	if NoLiveOwner(profile, filepath.Join(profile, "ImapMail", "imap.gmail-1.com")) {
		t.Error("positive twin: two accounts on one server, the second still names its folder")
	}
	prefs(ownerPrefs(ownerAcct("1", "imap.gmail.com", "", "qza@gmail.example"), b)...)
	if NoLiveOwner(profile, old) {
		t.Error("positive twin: a same-server account with no folder setting could keep its mail there")
	}
	reAdded := ownerAcct("1", "imap.gmail.com", "imap.gmail-2.com", "qza@gmail.example")
	prefs(append(ownerPrefs(reAdded, b), `user_pref("mail.server.server1.name", "x"); // a comment`)...)
	if NoLiveOwner(profile, old) {
		t.Error("positive twin: settings that cannot be read for certain tell nothing")
	}
	prefs(append(append([]string(nil), reAdded...), b...)...) // no mail.accountmanager.accounts
	if NoLiveOwner(profile, old) {
		t.Error("positive twin: settings that list no account tell nothing")
	}
	prefs(ownerPrefs(reAdded, b)...)
	if NoLiveOwner(profile, filepath.Join(profile, "ImapMail", "qz.nested", "imap.gmail.com")) {
		t.Error("positive twin: a folder not directly under ImapMail/ or Mail/ is not judged")
	}
	if NoLiveOwner(t.TempDir(), old) {
		t.Error("positive twin: a profile with no prefs.js tells nothing")
	}

	// F-01: the first account removed, its folder left, and added again: Thunderbird keeps it at
	// "-2", since "-1" is the second account's. ReadStoreFacts cannot say none for the old folder.
	_, err := ReadStoreFacts(profile, old)
	if k, ok := NoAccountKindOf(err); !ok || k != NoAccountAmbiguous {
		t.Fatalf("the case F-01 is about: ReadStoreFacts must say NoAccountAmbiguous, got %v %v (%v)", k, ok, err)
	}
	if !NoLiveOwner(profile, old) {
		t.Error("F-01: two accounts on one server, neither using the old folder: no live owner")
	}
	// One account on its server, re-added at "-1".
	prefs(ownerPrefs(ownerAcct("1", "imap.gmail.com", "imap.gmail-1.com", "qza@gmail.example"))...)
	if !NoLiveOwner(profile, old) {
		t.Error("one account on its server, re-added at -1: no live owner of the old folder")
	}
	// The second account removed and re-added at "-2": its leftover "-1" folder, Thunderbird's own
	// spelling (the "-N" just before the last dot), has no live owner either.
	prefs(ownerPrefs(a, ownerAcct("2", "imap.gmail.com", "imap.gmail-2.com", "qzb@gmail.example"))...)
	if !NoLiveOwner(profile, filepath.Join(profile, "ImapMail", "imap.gmail-1.com")) {
		t.Error("the second account re-added at -2: no live owner of its leftover -1 folder")
	}
}
