package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/thunderbird"
)

// covers: MA-1267, R12
// A wizard run that changed a Thunderbird setting for one input never says, for a later input it
// refuses, that it "did not change any Thunderbird setting" (the C0c pass round 4's R4-05,
// MA-1263's rule on the wizard's surface). The zenity dialogs have no test seam, so the loop is
// replayed with its real calls in order, as the review did: imapPrepAccount and EnableOffline for
// the first input, noteChange as prepareThunderbirdGUI records it, then imapPrepAccount for each
// later input. Each refusal then opens with what was changed and that it is kept, says its
// "nothing changed" of this mail only, still ends with W0, and changes nothing more. Positive twin
// first: with no earlier change the words are as before.
func TestARefusalAfterAChangeSaysWhatWasChanged(t *testing.T) {
	p, first := prepProfile(t, prepOne+`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/imap.gmail.com");`+"\n")
	leftover := filepath.Join(p, "ImapMail", "imap.old-provider.example")
	if err := os.MkdirAll(leftover, 0o755); err != nil {
		t.Fatal(err)
	}
	if refusal := prepRefusal(t, leftover); !strings.Contains(refusal, "so it did not change any Thunderbird setting.") || strings.Contains(refusal, "Earlier in this run") {
		t.Fatalf("the positive twin: %s", refusal)
	}

	// Input 1: Yes, prepare goes through.
	var changed []string
	profile, acct, refusal := imapPrepAccount(first, changed)
	if refusal != "" || acct == nil {
		t.Fatalf("input 1: %q", refusal)
	}
	c, err := thunderbird.EnableOffline(profile, acct.ServerKey)
	if err != nil || !c.Changed {
		t.Fatalf("input 1 was not changed: %+v, %v", c, err)
	}
	noteChange(&changed, acct, c)
	once := "Earlier in this run, MailArchive set Thunderbird to keep all mail on this computer for the account on imap.gmail.com. That change is kept: it is what Yes, prepare is for.\n\n"

	userOff, userStore := prepProfile(t, prepOne)
	if err := os.WriteFile(filepath.Join(userOff, "user.js"), []byte(`user_pref("mail.server.server1.offline_download", false);`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, handStore := prepProfile(t, prepOne+"/* mine */\n")
	loose := filepath.Join(t.TempDir(), "ImapMail", "imap.gmail.com")
	if err := os.MkdirAll(loose, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, store := range map[string]string{
		"a removed account's folder (R4-05)": leftover,
		"user.js keeping it off":             userStore,
		"a line Thunderbird never writes":    handStore,
		"no profile":                         loose,
	} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Dir(filepath.Dir(store))
			before := filesIn(t, dir)
			_, acct, refusal := imapPrepAccount(store, changed)
			assure.Refused(t, 1, refusal, assure.Code(1),
				assure.Names(once, "for this mail", imapGoesAhead),
				assure.Forbid("did not change any Thunderbird setting", "did not change any of them.", "changed nothing:"),
				assure.NoSideEffect(func() bool { return acct == nil && filesIn(t, dir) == before }))
		})
	}

	// After the quit, a failed change gives prepFailWords through the same rule.
	words := afterChange(prepFailWords(errors.New("write prefs.js: disk full"), profile, "imap.gmail.com"), changed)
	if !strings.HasPrefix(words, once) || strings.Contains(words, "did not change any Thunderbird setting") {
		t.Fatalf("the failed change's words after a change:\n%s", words)
	}

	// Two changes: "Those changes are kept".
	two := append(append([]string(nil), changed...), "the account on outlook.office365.com")
	if _, _, refusal := imapPrepAccount(leftover, two); !strings.HasPrefix(refusal, "Earlier in this run, MailArchive set Thunderbird to keep all mail on this computer for the account on imap.gmail.com, and for the account on outlook.office365.com. Those changes are kept: they are what Yes, prepare is for.\n\n") {
		t.Fatalf("two changes:\n%s", refusal)
	}

	// Structural: prepareThunderbirdGUI keeps the run's changes across the loop's inputs.
	src, err := os.ReadFile("wizard.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{
		"p, err := prepareThunderbirdGUI(in, &changed)",
		"profile, acct, refusal := imapPrepAccount(store, *changed)",
		"noteChange(changed, acct, c)",
		"afterChange(prepFailWords(e, profile, acct.Hostname), *changed)",
	} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("the wizard does not carry this run's changes: %q is gone from wizard.go", wired)
		}
	}
}
