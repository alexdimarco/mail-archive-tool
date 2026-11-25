package thunderbird

import (
	"path/filepath"
	"strings"
	"testing"
)

// idAcct is ownerAcct with the account's user name (its userName setting; "" for none).
func idAcct(n, host, folder, name, user string) []string {
	lines := ownerAcct(n, host, folder, name)
	if user != "" {
		lines = append(lines, `user_pref("mail.server.server`+n+`.userName", "`+user+`");`)
	}
	return lines
}

// identityOf is the identity ReadStoreFacts gives the account that owns store.
func identityOf(t *testing.T, profile, store string) string {
	t.Helper()
	f, err := ReadStoreFacts(profile, store)
	if err != nil || f.Account == nil {
		t.Fatalf("ReadStoreFacts(%s): %+v %v", store, f, err)
	}
	return f.Identity
}

// covers: MA-1504, R12
// The account's fixed identity (docs/design-per-mailbox-archives.md revision 3; the adversarial pass's
// INS-03 to INS-06): ReadStoreFacts gives, from the same strict read as the account's name,
// sha256(type, user name, server), 32 hex characters, never the name or the address. Positive twin
// first: an account with its type, user name and server has one, which holds neither. The same
// account renamed, or its server written in another letter case, has the same identity; another user
// name, server or type has another. A user name Thunderbird takes from mail.server.default counts. No
// user name gives no identity (never one two accounts could share), as does a user name that is not
// quoted text.
func TestTheAccountsIdentityIsReadStrictly(t *testing.T) {
	profile, prefs := ownerProfile(t, "imap.gmail.com")
	store := filepath.Join(profile, "ImapMail", "imap.gmail.com")
	prefs(ownerPrefs(idAcct("1", "imap.gmail.com", "imap.gmail.com", "Work", "qza@gmail.example"))...)
	id := identityOf(t, profile, store)
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" || strings.Contains(id, "qza") || strings.Contains(id, "Work") {
		t.Fatalf("positive twin: an account with type, user name and server has a 32-hex identity holding neither name nor address: %q", id)
	}
	same := map[string][]string{
		"renamed":                idAcct("1", "imap.gmail.com", "imap.gmail.com", "qza@gmail.example", "qza@gmail.example"),
		"its server in capitals": idAcct("1", "IMAP.Gmail.com", "imap.gmail.com", "Work", "qza@gmail.example"),
		"its user name from mail.server.default": append(ownerAcct("1", "imap.gmail.com", "imap.gmail.com", "Work"),
			`user_pref("mail.server.default.userName", "qza@gmail.example");`),
	}
	for what, acct := range same {
		prefs(ownerPrefs(acct)...)
		if got := identityOf(t, profile, store); got != id {
			t.Errorf("%s: the same account must keep its identity: %q, want %q", what, got, id)
		}
	}
	other := map[string][]string{
		"another user name": idAcct("1", "imap.gmail.com", "imap.gmail.com", "Work", "qzb@gmail.example"),
		"another server":    idAcct("1", "imap.qzother.example", "imap.gmail.com", "Work", "qza@gmail.example"),
		"another type": {`user_pref("mail.account.account1.server", "server1");`, `user_pref("mail.server.server1.hostname", "imap.gmail.com");`,
			`user_pref("mail.server.server1.type", "pop3");`, `user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");`,
			`user_pref("mail.server.server1.userName", "qza@gmail.example");`},
	}
	for what, acct := range other {
		prefs(ownerPrefs(acct)...)
		if got := identityOf(t, profile, store); got == id || got == "" {
			t.Errorf("%s: another account's identity: %q (the first's %q)", what, got, id)
		}
	}
	prefs(ownerPrefs(idAcct("1", "imap.gmail.com", "imap.gmail.com", "Work", ""))...)
	if got := identityOf(t, profile, store); got != "" {
		t.Errorf("no user name: no identity, got %q", got)
	}
	prefs(ownerPrefs(append(ownerAcct("1", "imap.gmail.com", "imap.gmail.com", "Work"), `user_pref("mail.server.server1.userName", 7);`))...)
	if f, _ := ReadStoreFacts(profile, store); f.Identity != "" {
		t.Errorf("a user name that is not quoted text: no identity, got %q", f.Identity)
	}
}

// covers: MA-1504, MA-1506, R12
// Moved and removed by the identity (revision 3): MovedByIdentity says where the account with the
// recorded identity keeps its mail now, when exactly one live account has it, with a folder setting
// that does not name the folder; LiveWithIdentity counts the live accounts with it, from the strict
// read. Positive twins first: the account still names its folder (not moved; one live); renamed and
// its folder deleted by hand, its setting still naming it (not moved, and not removed: INS-05); no
// folder setting; two accounts with the identity; settings that cannot be read for certain or list no
// account; a folder not directly under ImapMail/ or Mail/. Then: re-added at "-1" under another name
// (moved, INS-04); removed (none live, for certain); a different account with the same name on
// another server (not moved, and none live with the identity: INS-06).
func TestMovedAndRemovedAreDecidedByTheIdentity(t *testing.T) {
	profile, prefs := ownerProfile(t, "imap.gmail.com", "imap.gmail.com-1", "imap.qzother.example", "qz.nested")
	old := filepath.Join(profile, "ImapMail", "imap.gmail.com")
	work := idAcct("1", "imap.gmail.com", "imap.gmail.com", "Work", "qza@gmail.example")
	prefs(ownerPrefs(work)...)
	id := identityOf(t, profile, old)

	// Positive twins.
	if _, moved := MovedByIdentity(profile, old, id); moved {
		t.Error("positive twin: the account still names its folder: not moved")
	}
	if n, ok := LiveWithIdentity(profile, id); !ok || n != 1 {
		t.Fatalf("positive twin: the account is live: %d %v", n, ok)
	}
	prefs(ownerPrefs(idAcct("1", "imap.gmail.com", "imap.gmail.com", "qz-renamed@example.com", "qza@gmail.example"))...)
	if _, moved := MovedByIdentity(profile, old, id); moved {
		t.Error("positive twin: renamed, its setting still naming the folder (deleted by hand, Q4): not moved")
	}
	if n, ok := LiveWithIdentity(profile, id); !ok || n != 1 {
		t.Errorf("INS-05: a renamed account is still live by its identity: %d %v", n, ok)
	}
	prefs(ownerPrefs(idAcct("1", "imap.gmail.com", "", "Work", "qza@gmail.example"))...)
	if _, moved := MovedByIdentity(profile, old, id); moved {
		t.Error("positive twin: no folder setting tells no move")
	}
	reAdded := idAcct("1", "imap.gmail.com", "imap.gmail.com-1", "qza@gmail.example", "qza@gmail.example")
	prefs(ownerPrefs(reAdded, idAcct("2", "imap.gmail.com", "imap.qzother.example", "Twin", "qza@gmail.example"))...)
	if _, moved := MovedByIdentity(profile, old, id); moved {
		t.Error("positive twin: two live accounts with the identity tell no move")
	}
	prefs(append(ownerPrefs(reAdded), `user_pref("mail.server.server1.name", "x"); // a comment`)...)
	if _, moved := MovedByIdentity(profile, old, id); moved {
		t.Error("positive twin: settings that cannot be read for certain tell nothing")
	}
	if _, ok := LiveWithIdentity(profile, id); ok {
		t.Error("positive twin: settings that cannot be read for certain count nothing")
	}
	prefs(reAdded...) // no mail.accountmanager.accounts
	if _, ok := LiveWithIdentity(profile, id); ok {
		t.Error("positive twin: settings that list no account count nothing")
	}
	prefs(ownerPrefs(reAdded)...)
	if _, moved := MovedByIdentity(profile, filepath.Join(profile, "ImapMail", "qz.nested", "imap.gmail.com"), id); moved {
		t.Error("positive twin: a folder not directly under ImapMail/ or Mail/ is not judged")
	}
	if _, ok := LiveWithIdentity(profile, ""); ok {
		t.Error("positive twin: no identity asked: nothing to tell")
	}

	// INS-04: removed and added again under another name, kept at "-1": moved, by the identity.
	if dir, moved := MovedByIdentity(profile, old, id); !moved || dir != filepath.Join(profile, "ImapMail", "imap.gmail.com-1") {
		t.Errorf("INS-04: re-added at -1 under another name is moved there: %q %v", dir, moved)
	}
	// Removed for good: none live, for certain.
	prefs(ownerPrefs(idAcct("1", "imap.qzother.example", "imap.qzother.example", "Other", "qzo@other.example"))...)
	if n, ok := LiveWithIdentity(profile, id); !ok || n != 0 {
		t.Errorf("removed: no live account has the identity, for certain: %d %v", n, ok)
	}
	// INS-06: a different account named Work on another server: neither moved nor the same account.
	prefs(ownerPrefs(idAcct("1", "imap.qzother.example", "imap.qzother.example", "Work", "qzw@other.example"))...)
	if dir, moved := MovedByIdentity(profile, old, id); moved {
		t.Errorf("INS-06: another account that merely shares the name is not a move: %q", dir)
	}
	if n, ok := LiveWithIdentity(profile, id); !ok || n != 0 {
		t.Errorf("INS-06: no live account has the identity: %d %v", n, ok)
	}
}
