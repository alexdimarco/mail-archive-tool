package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

const (
	oldLabel = `"Keep messages in all folders for this account on this computer"`
	newLabel = `"Save copies of all folders and messages on this computer for offline use"`
	// The wizard's leftover-folder sentence, naming the folder's server (the lead's split of the
	// C0c friction re-look's R2 and R3, 2026-11-08).
	leftoverGmail  = "If you once removed an account on imap.gmail.com from Thunderbird, this may be its leftover mail folder; if so, there is nothing to turn on."
	leftoverOldjob = "If you once removed an account on mail.oldjob.com from Thunderbird, this may be its leftover mail folder; if so, there is nothing to turn on."
	// Step 1 says where Account Settings is (the re-look's R1: Thunderbird on Windows hides its
	// menu bar), and step 2 how to tell the account by its server (R5).
	step1      = "1. In Thunderbird, click the menu button (≡, top right), then Account Settings.\n"
	serverHint = ` (To see which server an account uses, look at its "Server Settings" page.)`
)

// prepRefusal is imapPrepAccount's refusal for store, failing when it gives an account.
func prepRefusal(t *testing.T, store string) string {
	t.Helper()
	_, acct, refusal := imapPrepAccount(store, nil)
	if acct != nil {
		t.Fatalf("an account was given: %+v", acct)
	}
	return assure.Reached(t, refusal, "the Yes, prepare refusal")
}

// covers: MA-1260, MA-1268
// Every Yes, prepare refusal in the wizard is in plain words (the C0c friction review's W0 to W4,
// W8, F2 to F8, F11): what happened and that no Thunderbird setting was changed; where the setting
// is, by the version that last opened the profile (below 160 "Synchronization & Storage", 160 and
// later "Offline message storage", both when the version cannot be read), for each account on
// the server; a plain Why with no prefs.js, no server key and no raw error; the leftover-folder
// sentence where a removed account can cause it; and every one ends with W0, which says what the
// export leaves out. Nothing is written in any case. Positive twin first: a healthy profile gives
// its account and no refusal.
func TestYesPrepareRefusalsArePlain(t *testing.T) {
	if _, acct, refusal := imapPrepAccount(func() string { _, s := prepProfile(t, prepOne); return s }(), nil); acct == nil || refusal != "" {
		t.Fatalf("the positive twin: account %+v, refusal %q", acct, refusal)
	}
	two := prepOne + `user_pref("mail.accountmanager.accounts", "account1,account2");` + "\n" +
		`user_pref("mail.account.account2.server", "server2");` + "\n" +
		`user_pref("mail.server.server2.hostname", "imap.gmail.com");` + "\n"
	plain := []string{"prefs.js", "server1", "server2", "mail.accountmanager", "mail.server"}

	for _, tc := range []struct {
		name, compat string
		want, never  []string
	}{
		{"153", "LastVersion=153.6.0", []string{
			`2. For each account on imap.gmail.com, open its "Synchronization & Storage" page.` + serverHint,
			"3. Make sure " + oldLabel + " is ticked."}, []string{newLabel, "Offline message storage"}},
		// 160's page is not named: the source read did not reach it (the re-look's R4).
		{"160", "LastVersion=160.0", []string{
			`2. For each account on imap.gmail.com, look through the pages listed under its name on the left until you see "Offline message storage".` + serverHint,
			"3. Make sure " + newLabel + " is ticked."}, []string{oldLabel, "Synchronization & Storage"}},
		{"version unknown", "LastVersion=not-a-version", []string{
			`2. For each account on imap.gmail.com, open its "Synchronization & Storage" page (in Thunderbird 160 and later, look through its pages for "Offline message storage").` + serverHint,
			"3. Make sure " + oldLabel + " is ticked (in Thunderbird 160 and later it is called " + newLabel + ")."}, nil},
	} {
		t.Run("two accounts on one server, "+tc.name, func(t *testing.T) {
			p, store := prepProfile(t, two)
			writeCompat(t, p, "[Compatibility]\n"+tc.compat+"\n")
			before := filesIn(t, p)
			refusal := prepRefusal(t, store)
			assure.Refused(t, 1, refusal, assure.Code(1),
				assure.Names(append([]string{
					"MailArchive could not tell which of your Thunderbird accounts on imap.gmail.com this mail belongs to, so it did not change any Thunderbird setting.",
					"To make sure Thunderbird keeps all of this mail on this computer:\n" + step1 + "2.",
					"Why: more than one account in Thunderbird uses imap.gmail.com, and MailArchive does not guess. " + leftoverGmail,
				}, tc.want...)...),
				assure.Forbid(append(append([]string{}, plain...), tc.never...)...),
				assure.NoSideEffect(func() bool { return filesIn(t, p) == before && strings.HasSuffix(refusal, "\n\n"+imapGoesAhead) }))
		})
	}

	cases := map[string]struct {
		make func(t *testing.T) (string, string)
		want []string
	}{
		"a removed account's folder": {func(t *testing.T) (string, string) {
			p, _ := prepProfile(t, prepOne+`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");`+"\n")
			left := filepath.Join(p, "ImapMail", "mail.oldjob.com")
			if err := os.MkdirAll(left, 0o755); err != nil {
				t.Fatal(err)
			}
			return p, left
		}, []string{"MailArchive could not find a Thunderbird account for the mail in mail.oldjob.com, so it did not change any Thunderbird setting.",
			"Why: no account in Thunderbird uses this mail now. " + leftoverOldjob,
			"If the account is still in Thunderbird:\n" + step1 + "2. For that account, open its \"Synchronization & Storage\" page."}},
		"no account list": {func(t *testing.T) (string, string) {
			return prepProfile(t, `user_pref("mail.server.server1.hostname", "imap.gmail.com");`+"\n")
		}, []string{"MailArchive could not tell which of your Thunderbird accounts on imap.gmail.com this mail belongs to",
			"Why: Thunderbird's settings list no accounts."}},
		"prefs.js it cannot read": {func(t *testing.T) (string, string) {
			p, store := prepProfile(t, prepOne)
			f, err := os.OpenFile(filepath.Join(p, "prefs.js"), os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				t.Fatal(err)
			}
			if err := f.Truncate(16<<20 + 1); err != nil {
				t.Fatal(err)
			}
			f.Close()
			return p, store
		}, []string{"MailArchive could not read Thunderbird's settings, so it did not change any of them.",
			"2. For the account on imap.gmail.com, open its \"Synchronization & Storage\" page." + serverHint}},
		// The re-look's R7: "changed outside Thunderbird", not "edited by hand", which reads as
		// blame (an add-on or an IT tool may have made the change).
		"a hand-edited line": {func(t *testing.T) (string, string) {
			return prepProfile(t, strings.Replace(prepOne, `offline_download", false);`, `offline_download", false); // mine`, 1))
		}, []string{"Thunderbird's settings file was changed outside Thunderbird, so MailArchive could not be sure what it says and did not change any Thunderbird setting.",
			"2. For the account on imap.gmail.com, open its \"Synchronization & Storage\" page." + serverHint}},
		// The strict reading (MA-1261): a line Thunderbird never writes, anywhere in the file.
		"a line Thunderbird never writes": {func(t *testing.T) (string, string) {
			return prepProfile(t, prepOne+`pref("mail.zz", 1);`+"\n")
		}, []string{"Thunderbird's settings file was changed outside Thunderbird, so MailArchive could not be sure what it says and did not change any Thunderbird setting.",
			"2. For the account on imap.gmail.com, open its \"Synchronization & Storage\" page." + serverHint}},
		"no profile": {func(t *testing.T) (string, string) {
			q := t.TempDir()
			store := filepath.Join(q, "ImapMail", "imap.gmail.com")
			if err := os.MkdirAll(store, 0o755); err != nil {
				t.Fatal(err)
			}
			return q, store
		}, []string{"MailArchive could not find the Thunderbird profile this mail belongs to (the mail may have been copied out of Thunderbird), so it did not change any Thunderbird setting."}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			p, store := tc.make(t)
			before := filesIn(t, p)
			refusal := prepRefusal(t, store)
			assure.Refused(t, 1, refusal, assure.Code(1),
				assure.Names(tc.want...),
				assure.Forbid(append(append([]string{}, plain...), "bound", string(filepath.Separator)+"prefs")...),
				assure.NoSideEffect(func() bool { return filesIn(t, p) == before && strings.HasSuffix(refusal, "\n\n"+imapGoesAhead) }))
		})
	}
}

// covers: MA-1209
// Closing the Yes, prepare warning with the window's close button continues the export, as its
// words say (the C0c pass round 2's R2-07, the friction review's F12): zenity returns
// ErrCanceled for a closed window on Linux, and the wizard ends quietly on ErrCanceled, so the
// warning's answer goes through noticeClosed, which reads OK and a closed window alike, as the
// file already does after its information dialog. Any other error still stops the wizard.
// Positive twin first: OK goes on. The dialog itself is zenity, not driven in a test: that the
// refusal and the failed-change dialogs both go through noticeClosed is checked in the source.
func TestClosingThePrepWarningGoesOn(t *testing.T) {
	if err := noticeClosed(nil); err != nil {
		t.Fatalf("the positive twin: OK gave %v", err)
	}
	if err := noticeClosed(zenity.ErrCanceled); err != nil {
		t.Fatalf("a closed warning gave %v, which ends the wizard with no export", err)
	}
	other := errors.New("no display")
	if err := noticeClosed(other); !errors.Is(err, other) {
		t.Fatalf("another error was swallowed: %v", err)
	}
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	body := assure.Reached(t, string(src), "wizard.go")
	for _, wired := range []string{
		"return false, noticeClosed(zenity.Warning(refusal, zenity.Title(appTitle)))",
		"return false, noticeClosed(zenity.Error(",
	} {
		if !strings.Contains(body, wired) {
			t.Errorf("prepareThunderbirdGUI does not read a closed dialog as OK: %q is gone from wizard.go", wired)
		}
	}
}
