package thunderbird

import (
	"errors"
	"path/filepath"
	"testing"
)

// covers: MA-1262, R9, S10
// The hostname fallback gives an account only to a store directly in <profile>/ImapMail/<dir>
// (the C0c pass round 3's R3-05, the R2-04 leftover). A copied profile kept under this profile's
// Mail/ or ImapMail/ without its own compatibility.ini is found as this profile, and its store
// ImapMail/imap.gmail.com, deeper down, was given this profile's live server1 by hostname, so
// -enable-offline changed an account whose mail is elsewhere. A deeper store, or one under Mail/,
// now gets no account by hostname: a NoAccountError, nothing changed. A store matched by its own
// folder setting (directory-rel or directory) is found wherever it is. Positive twins first: a
// store directly in ImapMail/ with no folder setting is found by its hostname.
func TestTheHostnameFallbackIsOnlyForAStoreDirectlyInImapMail(t *testing.T) {
	p := t.TempDir()
	markProfile(t, p, live("server1")+imapServer("server1", "imap.gmail.com", "", "")+off1+"\n")
	direct := filepath.Join(p, "ImapMail", "imap.gmail.com")
	mkdirs(t, direct)
	if got, ok := FindProfileDir(direct); !ok || got != p {
		t.Fatalf("the positive twin: profile %q, %v", got, ok)
	}
	if acct, err := AccountToEnable(p, direct); err != nil || acct.ServerKey != "server1" {
		t.Fatalf("the positive twin: %+v, %v", acct, err)
	}

	for name, store := range map[string]string{
		"a copied profile under Mail/":     filepath.Join(p, "Mail", "old-profile", "ImapMail", "imap.gmail.com"),
		"a copied profile under ImapMail/": filepath.Join(p, "ImapMail", "old-profile", "ImapMail", "imap.gmail.com"),
		"a folder under Mail/ by the name": filepath.Join(p, "Mail", "imap.gmail.com"),
	} {
		t.Run(name, func(t *testing.T) {
			mkdirs(t, store)
			if name != "a folder under Mail/ by the name" {
				writeAt(t, filepath.Join(filepath.Dir(filepath.Dir(store)), "prefs.js"), live("server7")+imapServer("server7", "imap.gmail.com", "[ProfD]ImapMail/imap.gmail.com", ""))
			}
			got, ok := FindProfileDir(store)
			if !ok {
				return // no profile at all: nothing can be changed
			}
			acct, err := AccountToEnable(got, store)
			var n *NoAccountError
			if !errors.As(err, &n) || acct != nil {
				t.Fatalf("the store %s was given %+v of the profile %s by hostname (err %v)", store, acct, got, err)
			}
			refusedNaming(t, err, func() bool { return acct == nil && accountKeyIsOff(t, p) }, "changed nothing", "imap.gmail.com")
		})
	}

	// A folder setting still finds a store anywhere: server1's directory names the deep store.
	deep := filepath.Join(p, "Mail", "elsewhere", "imap.gmail.com")
	mkdirs(t, deep)
	q := t.TempDir()
	markProfile(t, q, live("server1")+imapServer("server1", "imap.gmail.com", "", deep)+off1+"\n")
	if acct, err := AccountToEnable(q, deep); err != nil || acct.ServerKey != "server1" {
		t.Fatalf("a store named by its directory setting: %+v, %v", acct, err)
	}
}

// accountKeyIsOff reports whether server1 in profile p still reads off.
func accountKeyIsOff(t *testing.T, p string) bool {
	t.Helper()
	acct, err := FindAccountForStore(p, filepath.Join(p, "ImapMail", "imap.gmail.com"))
	return err == nil && acct != nil && !acct.OfflineDownload
}
