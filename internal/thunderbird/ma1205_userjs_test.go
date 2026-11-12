package thunderbird_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/tbrestore"
	"mail-archive-tool/internal/thunderbird"
)

// userJSProfile makes a profile with one live IMAP account (server1) set to off in prefs.js,
// and user.js holding userJS when it is not empty. It returns the profile, its store and
// prefs.js's bytes.
func userJSProfile(t *testing.T, userJS string) (string, string, []byte) {
	t.Helper()
	p := t.TempDir()
	store := filepath.Join(p, "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := []byte(`user_pref("mail.accountmanager.accounts", "account1");` + "\n" +
		`user_pref("mail.account.account1.server", "server1");` + "\n" +
		`user_pref("mail.server.server1.hostname", "imap.gmail.com");` + "\n" +
		`user_pref("mail.server.server1.type", "imap");` + "\n" +
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");` + "\n" +
		`user_pref("mail.server.server1.offline_download", false);` + "\n")
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), prefs, 0o600); err != nil {
		t.Fatal(err)
	}
	if userJS != "" {
		if err := os.WriteFile(filepath.Join(p, "user.js"), []byte(userJS), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return p, store, prefs
}

// agrees asserts FindAccountForStore's OfflineDownload is what Thunderbird applies, as the
// restore's reader (prefs.js, then user.js over it) reads it, and returns it.
func agrees(t *testing.T, p, store string) bool {
	t.Helper()
	acct, err := thunderbird.FindAccountForStore(p, store)
	if err != nil || acct == nil {
		t.Fatalf("no account (%v)", err)
	}
	tp, err := tbrestore.ReadPrefs(p)
	if err != nil {
		t.Fatal(err)
	}
	v, _, _ := tp.Get("mail.server.server1.offline_download")
	if acct.OfflineDownload != (v == "true") {
		t.Fatalf("thunderbird says offline_download=%v; Thunderbird (prefs.js then user.js) applies %q", acct.OfflineDownload, v)
	}
	return acct.OfflineDownload
}

// covers: MA-1205
// user.js, which Thunderbird applies over prefs.js at every start, is honoured (the C0c
// pass's CON-06). When user.js sets the account's offline setting, FindAccountForStore reads
// it as Thunderbird applies it, and EnableOffline changes nothing: set to false, it refuses
// with words naming user.js (the file that controls it) and the remedy, writing no backup and
// leaving prefs.js as it was; set to true, the account is already on. Positive twin first: with
// no user.js, the account is turned on, and both readers agree.
func TestUserJSControlsTheOfflineSetting(t *testing.T) {
	p, store, _ := userJSProfile(t, "")
	if c, err := thunderbird.EnableOffline(p, "server1"); err != nil || !c.Changed {
		t.Fatalf("the positive twin: %+v, %v", c, err)
	}
	if !agrees(t, p, store) {
		t.Fatal("the positive twin is still off")
	}

	t.Run("user.js sets it false", func(t *testing.T) {
		p, store, before := userJSProfile(t, `user_pref("mail.server.server1.offline_download", false);`+"\n")
		if agrees(t, p, store) {
			t.Fatal("user.js's false is read as on")
		}
		acct, _ := thunderbird.FindAccountForStore(p, store)
		if !acct.OfflineInUserJS {
			t.Fatal("the account does not say user.js sets its offline setting")
		}
		c, err := thunderbird.EnableOffline(p, "server1")
		rc, msg := 0, ""
		if err != nil {
			rc, msg = 1, err.Error()
		}
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names(filepath.Join(p, "user.js"), "mail.server.server1.offline_download", "changed nothing"),
			assure.NoSideEffect(func() bool {
				now, rerr := os.ReadFile(filepath.Join(p, "prefs.js"))
				_, berr := os.Lstat(filepath.Join(p, "prefs.js.mailarchive-bak"))
				return rerr == nil && string(now) == string(before) && errors.Is(berr, fs.ErrNotExist) && !c.Changed
			}))
		var u *thunderbird.UserJSError
		if !errors.As(err, &u) {
			t.Fatalf("the refusal is not a UserJSError: %T", err)
		}
	})

	t.Run("user.js sets it true", func(t *testing.T) {
		p, store, before := userJSProfile(t, `user_pref("mail.server.server1.offline_download", true);`+"\n")
		if !agrees(t, p, store) {
			t.Fatal("user.js's true is read as off")
		}
		c, err := thunderbird.EnableOffline(p, "server1")
		if err != nil || c.Changed {
			t.Fatalf("an account user.js turns on: %+v, %v", c, err)
		}
		if now, _ := os.ReadFile(filepath.Join(p, "prefs.js")); string(now) != string(before) {
			t.Fatal("prefs.js was written though user.js already turns the account on")
		}
	})
}
