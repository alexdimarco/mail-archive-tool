package wizard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// prepProfile makes a profile whose prefs.js is prefs, with the store ImapMail/imap.gmail.com,
// and returns the profile and the store.
func prepProfile(t *testing.T, prefs string) (string, string) {
	t.Helper()
	p := t.TempDir()
	store := filepath.Join(p, "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "prefs.js"), []byte(prefs), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, store
}

// filesIn are the names in dir.
func filesIn(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	return strings.Join(names, ",")
}

const prepOne = `user_pref("mail.accountmanager.accounts", "account1");` + "\n" +
	`user_pref("mail.account.account1.server", "server1");` + "\n" +
	`user_pref("mail.server.server1.hostname", "imap.gmail.com");` + "\n" +
	`user_pref("mail.server.server1.type", "imap");` + "\n" +
	`user_pref("mail.server.server1.offline_download", false);` + "\n"

// covers: MA-1204, MA-1205
// The wizard's Yes, prepare shows why it cannot prepare and does not carry on as if it had
// (the C0c pass's OUT-01). When no account can be told, the refusal gives the reason (naming
// prefs.js and the accounts), says nothing was changed and what to turn on by hand, and that
// the export goes ahead with the mail already here; when user.js keeps the setting off, it
// names user.js. prepareThunderbirdGUI shows that refusal and returns "not prepared" at once,
// so no sync is guided and the export is not run as a prepared Full export (its dialogs are
// zenity, not driven in a test: that part is checked in its source). Nothing is written.
// Positive twin first: one account gives the account and no refusal.
func TestYesPrepareSaysWhyAndDoesNotCarryOn(t *testing.T) {
	_, store := prepProfile(t, prepOne)
	if _, acct, refusal := imapPrepAccount(store); acct == nil || refusal != "" {
		t.Fatalf("the positive twin: account %+v, refusal %q", acct, refusal)
	}

	t.Run("no account it can tell", func(t *testing.T) {
		p, store := prepProfile(t, prepOne+
			`user_pref("mail.accountmanager.accounts", "account1,account2");`+"\n"+
			`user_pref("mail.account.account2.server", "server2");`+"\n"+
			`user_pref("mail.server.server2.hostname", "imap.gmail.com");`+"\n")
		before := filesIn(t, p)
		_, acct, refusal := imapPrepAccount(store)
		rc := 0
		if refusal != "" && acct == nil {
			rc = 1
		}
		assure.Refused(t, rc, refusal, assure.Code(1),
			assure.Names("MailArchive could not tell which Thunderbird account the folder imap.gmail.com belongs to, so it changed nothing",
				`turn on "Keep messages in all folders for this account on this computer"`,
				"more than one account in prefs.js (server1, server2)",
				"This export goes ahead with the mail already on this computer."),
			assure.NoSideEffect(func() bool { return filesIn(t, p) == before }))
	})

	t.Run("user.js keeps it off", func(t *testing.T) {
		p, store := prepProfile(t, prepOne)
		userJS := filepath.Join(p, "user.js")
		if err := os.WriteFile(userJS, []byte(`user_pref("mail.server.server1.offline_download", false);`+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		before := filesIn(t, p)
		_, acct, refusal := imapPrepAccount(store)
		rc := 0
		if refusal != "" && acct == nil {
			rc = 1
		}
		assure.Refused(t, rc, refusal, assure.Code(1),
			assure.Names(userJS, "changed nothing", "This export goes ahead with the mail already on this computer."),
			assure.NoSideEffect(func() bool { return filesIn(t, p) == before }))
	})

	// Structural: the dialog shows the refusal and returns "not prepared" at once.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{
		"profile, acct, refusal := imapPrepAccount(store)\n\tif refusal != \"\" {",
		"return false, zenity.Warning(refusal, zenity.Title(appTitle))",
	} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("prepareThunderbirdGUI does not stop on the refusal: %q is gone from wizard.go", wired)
		}
	}
	if strings.Contains(string(src), "acct, _ := thunderbird.FindAccountForStore") {
		t.Error("wizard.go still throws the reason away")
	}
}
