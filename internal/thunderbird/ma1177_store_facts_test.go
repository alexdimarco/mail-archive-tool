package thunderbird

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// factsProfile is a Thunderbird profile (prefs.js as Thunderbird writes it, compatibility.ini) whose
// prefs.js holds lines, and a store ImapMail/imap.c2.example with an INBOX mbox file.
func factsProfile(t *testing.T, lines ...string) (string, string) {
	t.Helper()
	p := t.TempDir()
	store := filepath.Join(p, "ImapMail", "imap.c2.example")
	if err := os.MkdirAll(filepath.Join(store, "Archive.sbd"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(store, "INBOX"), filepath.Join(store, "Archive.sbd", "2024")} {
		if err := os.WriteFile(f, []byte("From - x\nSubject: x\n\nx\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	prefs := "// Mozilla User Preferences\n\n" +
		`user_pref("mail.account.account1.server", "server1");` + "\n" +
		`user_pref("mail.account.account2.server", "server2");` + "\n" +
		`user_pref("mail.accountmanager.accounts", "account1,account2");` + "\n" +
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.c2.example");` + "\n" +
		`user_pref("mail.server.server1.hostname", "imap.c2.example");` + "\n" +
		`user_pref("mail.server.server1.type", "imap");` + "\n" +
		`user_pref("mail.server.server2.directory-rel", "[ProfD]Mail/Local Folders");` + "\n" +
		`user_pref("mail.server.server2.hostname", "Local Folders");` + "\n" +
		`user_pref("mail.server.server2.type", "none");` + "\n" +
		strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, store
}

// covers: MA-1177
// The account's limits are read from prefs.js and user.js as Thunderbird applies them (slice C2;
// the C1b note: Input.Limits' zero value is Thunderbird's defaults, so C2 reads the account's own
// settings): the account's own line, else mail.server.default's, else mailnews.js's built-in
// default (age -1, no size limit, 50 KB, background download on). A limit setting with a value of
// a type Thunderbird never writes for it gives no account and a *LimitsError (never a guess).
// Positive twin first: a profile with no limit line reads the defaults.
func TestTheAccountsLimitsAreReadAsThunderbirdAppliesThem(t *testing.T) {
	p, store := factsProfile(t)
	f, err := ReadStoreFacts(p, store)
	if err != nil || f.Account == nil || f.Account.Type != "imap" || !f.Whole || f.Gmail != GmailNo {
		t.Fatalf("positive twin: %+v, %v", f, err)
	}
	if f.Limits != (Limits{MaxAgeDays: -1, MaxSizeKB: 50}) {
		t.Errorf("the defaults: %+v", f.Limits)
	}
	for _, c := range []struct {
		name  string
		lines []string
		want  Limits
	}{
		{"own age", []string{`user_pref("mail.server.server1.autosync_max_age_days", 30);`}, Limits{MaxAgeDays: 30, MaxSizeKB: 50}},
		{"default age", []string{`user_pref("mail.server.default.autosync_max_age_days", 90);`}, Limits{MaxAgeDays: 90, MaxSizeKB: 50}},
		{"own beats default", []string{`user_pref("mail.server.default.autosync_max_age_days", 90);`, `user_pref("mail.server.server1.autosync_max_age_days", -1);`}, Limits{MaxAgeDays: -1, MaxSizeKB: 50}},
		{"size", []string{`user_pref("mail.server.server1.limit_offline_message_size", true);`, `user_pref("mail.server.server1.max_size", 200);`}, Limits{MaxAgeDays: -1, SizeLimit: true, MaxSizeKB: 200}},
		{"no background", []string{`user_pref("mail.server.server1.autosync_offline_stores", false);`}, Limits{MaxAgeDays: -1, MaxSizeKB: 50, NoBackgroundDownload: true}},
		{"default no background", []string{`user_pref("mail.server.default.autosync_offline_stores", false);`}, Limits{MaxAgeDays: -1, MaxSizeKB: 50, NoBackgroundDownload: true}},
	} {
		p, store := factsProfile(t, c.lines...)
		f, err := ReadStoreFacts(p, store)
		if err != nil || f.Account == nil || f.Limits != c.want {
			t.Errorf("%s: %+v, %v; want %+v", c.name, f.Limits, err, c.want)
		}
	}
	for _, line := range []string{
		`user_pref("mail.server.server1.autosync_max_age_days", "30");`,
		`user_pref("mail.server.server1.autosync_max_age_days", true);`,
		`user_pref("mail.server.default.max_size", "big");`,
		`user_pref("mail.server.server1.limit_offline_message_size", 1);`,
		`user_pref("mail.server.server1.autosync_offline_stores", "false");`,
	} {
		p, store := factsProfile(t, line)
		f, err := ReadStoreFacts(p, store)
		var le *LimitsError
		if f.Account != nil || !errors.As(err, &le) {
			t.Errorf("%s: account %+v, err %v; want none and a LimitsError", line, f.Account, err)
		}
	}
	// A limit line on another account, or on Local Folders, does not stop this one.
	p, store = factsProfile(t, `user_pref("mail.server.server2.autosync_max_age_days", "x");`)
	if f, err := ReadStoreFacts(p, store); err != nil || f.Account == nil {
		t.Errorf("another account's line stopped this one: %v", err)
	}
	if f, err := ReadStoreFacts(p, filepath.Join(p, "Mail", "Local Folders")); err != nil || f.Account == nil || f.Account.Type != "none" {
		t.Errorf("Local Folders' limits are not read: %+v %v", f.Account, err)
	}
}

// covers: MA-1202, MA-1276
// The store is the input itself when an account owns it, and otherwise the nearest folder above
// the input, below the profile, that an account owns (TC-11): one mbox file of a store and a
// subfolder are that account's, not whole. An input no account owns, inside the profile, has no
// account. The profile's other stores are every other live account's own folder (R2-02), never
// this store's. Gmail is is_gmail's answer, from the same read.
func TestTheStoreIsFoundFromTheInput(t *testing.T) {
	p, store := factsProfile(t, `user_pref("mail.server.server1.is_gmail", true);`)
	f, err := ReadStoreFacts(p, store)
	if err != nil || !f.Whole || f.Store != store || f.Gmail != GmailYes {
		t.Fatalf("positive twin: %+v %v", f, err)
	}
	if len(f.OtherStores) != 1 || !samePath(f.OtherStores[0], filepath.Join(p, "Mail", "Local Folders"), runtime.GOOS) {
		t.Errorf("other stores: %q", f.OtherStores)
	}
	for _, in := range []string{filepath.Join(store, "INBOX"), filepath.Join(store, "Archive.sbd"), filepath.Join(store, "Archive.sbd", "2024")} {
		f, err := ReadStoreFacts(p, in)
		if err != nil || f.Account == nil || f.Account.ServerKey != "server1" || f.Whole || f.Store != store {
			t.Errorf("%s: %+v %v; want server1's store, not whole", in, f, err)
		}
	}
	leftover := filepath.Join(p, "ImapMail", "imap.gone.example")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if f, err := ReadStoreFacts(p, leftover); f.Account != nil || err == nil {
		t.Errorf("a leftover folder no account owns: %+v %v", f.Account, err)
	}
}
