package thunderbird

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1235, R25
// Only an IMAP account is Gmail (the lead's call on B1, 2026-11-08): Thunderbird keeps one copy of
// a Gmail message across its folders only for IMAP (the single-copy rule sits in
// nsImapMailFolder.cpp), so an is_gmail on any other account type never applies the Gmail rule.
// Positive twin first: the IMAP account with is_gmail true is Gmail. Then a POP account whose store
// is under Mail/, with is_gmail true written for it or taken from mail.server.default.is_gmail, is
// not; nor is an account whose type is missing.
func TestGmailIsAnIMAPAccountsOnly(t *testing.T) {
	p, store := tbProfile(t, onePrefs+gmailLine("server1", "true"), "")
	if !isGmail(t, p, store) {
		t.Fatal("the positive twin: an IMAP account with is_gmail true is not Gmail")
	}

	pop := func(t *testing.T, typ, lines string) (string, string) {
		t.Helper()
		prefs := live("server1") +
			prefLine("mail.server.server1.hostname", "pop.gmail.com") +
			prefLine("mail.server.server1.directory-rel", "[ProfD]Mail/pop.gmail.com") + lines
		if typ != "" {
			prefs += prefLine("mail.server.server1.type", typ)
		}
		profile := profileWith(t, prefs)
		store := filepath.Join(profile, "Mail", "pop.gmail.com")
		if err := os.MkdirAll(store, 0o755); err != nil {
			t.Fatal(err)
		}
		return profile, store
	}
	for name, tc := range map[string]struct{ typ, lines string }{
		"a POP account with is_gmail true":         {"pop3", gmailLine("server1", "true")},
		"a POP account taking the default true":    {"pop3", gmailLine("default", "true")},
		"an account of no type with is_gmail true": {"", gmailLine("server1", "true")},
	} {
		t.Run(name, func(t *testing.T) {
			p, store := pop(t, tc.typ, tc.lines)
			if isGmail(t, p, store) {
				t.Fatal("a non-IMAP account is Gmail")
			}
		})
	}
}
