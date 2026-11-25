package thunderbird

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1463
// NoAccountKindOf reads why the match gave no account from the error ReadStoreFacts returns, with
// no second read of prefs.js: the wizard's list says "Not in Thunderbird now" only for NoAccountNone,
// which the match gives only after prefs.js and user.js both read with certainty. Positive twin
// first: a live store gives no error and no kind. A leftover folder beside the live one (the shape
// Thunderbird's CreateUnique makes: imap.gmail-1.com for the live account, imap.gmail.com left
// behind) is NoAccountNone; a leftover named like the host two live accounts share is
// NoAccountAmbiguous; a hand-edited line and a limit of the wrong type are no kind at all, so the list
// never calls those folders unused.
func TestTheMatchsKindIsReadFromItsError(t *testing.T) {
	p := t.TempDir()
	for _, d := range []string{"imap.gmail.com", "imap.gmail-1.com", "imap.gmail-2.com"} {
		if err := os.MkdirAll(filepath.Join(p, "ImapMail", d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(p, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	prefs := func(lines ...string) {
		t.Helper()
		body := "// Mozilla User Preferences\n\n"
		for _, l := range lines {
			body += l + "\n"
		}
		if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	one := []string{
		`user_pref("mail.account.account1.server", "server1");`,
		`user_pref("mail.accountmanager.accounts", "account1");`,
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail-1.com");`,
		`user_pref("mail.server.server1.hostname", "imap.gmail.com");`,
		`user_pref("mail.server.server1.type", "imap");`,
	}
	live, leftover := filepath.Join(p, "ImapMail", "imap.gmail-1.com"), filepath.Join(p, "ImapMail", "imap.gmail.com")

	prefs(one...)
	f, err := ReadStoreFacts(p, live)
	if err != nil || f.Account == nil || !f.Whole {
		t.Fatalf("positive twin: the live store: %+v %v", f, err)
	}
	if k, ok := NoAccountKindOf(err); ok {
		t.Fatalf("positive twin: no error gives no kind, got %v", k)
	}

	_, err = ReadStoreFacts(p, leftover)
	if k, ok := NoAccountKindOf(err); !ok || k != NoAccountNone {
		t.Errorf("a leftover beside the live account: kind %v %v (err %v), want NoAccountNone", k, ok, err)
	}

	// Two live accounts on imap.gmail.com, kept in imap.gmail-1.com and imap.gmail-2.com: the
	// leftover imap.gmail.com could be either's by its name, so the match does not say "none".
	prefs(
		`user_pref("mail.account.account1.server", "server1");`,
		`user_pref("mail.account.account2.server", "server2");`,
		`user_pref("mail.accountmanager.accounts", "account1,account2");`,
		`user_pref("mail.server.server1.hostname", "imap.gmail.com");`,
		`user_pref("mail.server.server1.type", "imap");`,
		`user_pref("mail.server.server2.hostname", "imap.gmail.com");`,
		`user_pref("mail.server.server2.type", "imap");`,
	)
	_, err = ReadStoreFacts(p, leftover)
	if k, ok := NoAccountKindOf(err); !ok || k != NoAccountAmbiguous {
		t.Errorf("a leftover named like two live accounts' host: kind %v %v (err %v), want NoAccountAmbiguous", k, ok, err)
	}

	// A line Thunderbird never writes: the file is not read with certainty, which is no kind.
	prefs(append(append([]string(nil), one...), `user_pref("mail.server.server1.name", "x"); // a comment`)...)
	_, err = ReadStoreFacts(p, leftover)
	var h *HandEditedError
	if !errors.As(err, &h) {
		t.Fatalf("the hand-edited file: want a HandEditedError, got %v", err)
	}
	if k, ok := NoAccountKindOf(err); ok {
		t.Errorf("a hand-edited file gave the kind %v; it must give none", k)
	}

	// A limit of a type Thunderbird never writes: a LimitsError, no kind.
	prefs(append(append([]string(nil), one...), `user_pref("mail.server.server1.autosync_max_age_days", "30");`)...)
	_, err = ReadStoreFacts(p, live)
	var le *LimitsError
	if !errors.As(err, &le) {
		t.Fatalf("the mistyped limit: want a LimitsError, got %v", err)
	}
	if k, ok := NoAccountKindOf(err); ok {
		t.Errorf("a mistyped limit gave the kind %v; it must give none", k)
	}
	if k, ok := NoAccountKindOf(errors.New("read prefs.js: permission denied")); ok {
		t.Errorf("a plain error gave the kind %v", k)
	}
}
