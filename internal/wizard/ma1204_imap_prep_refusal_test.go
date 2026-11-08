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
	// Thunderbird writes compatibility.ini at every start; a profile is known by it (MA-1206).
	writeCompat(t, p, "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\n")
	return p, store
}

// writeCompat writes the profile's compatibility.ini.
func writeCompat(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(p, "compatibility.ini"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
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

// imapGoesAhead is the friction review's W0, the tail of every Yes, prepare refusal.
const imapGoesAhead = "This export goes ahead now with the mail already on this computer. Mail that Thunderbird has not downloaded yet is not in it; a later run adds it once Thunderbird has it."

// covers: MA-1204, MA-1205
// The wizard's Yes, prepare shows why it cannot prepare and does not carry on as if it had
// (the C0c pass's OUT-01). When no account can be told, the refusal says, in plain words, that
// no Thunderbird setting was changed, why, what to make sure of by hand for each account on
// that server, and that the export goes ahead with the mail already here and what that leaves
// out (the friction review's W1, W0); it names neither prefs.js nor a server key. When user.js
// keeps the setting off, it names user.js and gives the file last (W4). prepareThunderbirdGUI
// shows that refusal and returns "not prepared" at once, so no sync is guided and the export is
// not run as a prepared Full export (its dialogs are zenity, not driven in a test: that part is
// checked in its source). Nothing is written. Positive twin first: one account gives the account
// and no refusal.
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
			assure.Names("MailArchive could not tell which of your Thunderbird accounts on imap.gmail.com this mail belongs to, so it did not change any Thunderbird setting.",
				`3. Make sure "Keep messages in all folders for this account on this computer" is ticked.`,
				"Why: more than one account in Thunderbird uses imap.gmail.com, and MailArchive does not guess.",
				imapGoesAhead),
			assure.Forbid("prefs.js", "server1", "server2"),
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
			assure.Names(userJS, "changed nothing",
				"Thunderbird is set, in its file user.js, not to keep the mail of imap.gmail.com on this computer",
				// The C0c friction re-look's R6 (2026-11-08): no clipped "your IT", and the way to
				// open the file on Windows, where double-clicking a .js file runs it.
				"If you, or whoever looks after this computer, set this on purpose, leave it.",
				`Otherwise: close Thunderbird and open the file below in a plain text editor (on Windows: start Notepad, choose File, then Open, and type or paste the whole path into "File name"; double-clicking the file does not open it for editing). Delete the line that contains mail.server.server1.offline_download (or change its false to true), and save.`,
				"The file: "+userJS+"\n\n"+imapGoesAhead),
			assure.Forbid("prefs.js"),
			assure.NoSideEffect(func() bool { return filesIn(t, p) == before }))
	})

	// Structural: the dialog shows the refusal and returns "not prepared" at once.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{
		"profile, acct, refusal := imapPrepAccount(store)\n\tif refusal != \"\" {",
		"return false, noticeClosed(zenity.Warning(refusal, zenity.Title(appTitle)))",
	} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("prepareThunderbirdGUI does not stop on the refusal: %q is gone from wizard.go", wired)
		}
	}
	if strings.Contains(string(src), "acct, _ := thunderbird.FindAccountForStore") {
		t.Error("wizard.go still throws the reason away")
	}
}
