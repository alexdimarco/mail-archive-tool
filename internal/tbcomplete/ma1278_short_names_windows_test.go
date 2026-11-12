//go:build windows

package tbcomplete

import (
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// covers: MA-1278
// On a real Windows volume: a store folder with an 8.3 short spelling (the C1a pass's
// TestAdvC1aW02 shape) finds its keys whether the keys are spelled short and the store long, the
// reverse, or the keys short in their first long-named part only. And a store path that does not
// exist has no spellings to look up, so it reads as before (no keys), not as can't tell. Positive
// twin first: keys and store spelled alike are exact. The test is skipped, saying so, where the
// volume gives no short names (8.3 names turned off).
func TestShortAndLongSpellingsOnWindows(t *testing.T) {
	p := t.TempDir()
	long := filepath.Join(p, "A Long Profile Folder", "ImapMail", "imap.example.com")
	mkdir(t, long)
	short, err := winPath(long, syscall.GetShortPathName)
	if err != nil {
		t.Fatal(err)
	}
	if strings.EqualFold(short, long) {
		t.Skipf("this volume gives %s no short spelling (8.3 names are off): nothing to prove here", long)
	}
	// Short in the first long-named part only: the profile folder's own name.
	mixedParts := strings.Split(short, `\`)
	longParts := strings.Split(long, `\`)
	if len(mixedParts) != len(longParts) {
		t.Fatalf("short %s and long %s have different part counts", short, long)
	}
	n := len(longParts) - 3 // "A Long Profile Folder"
	mixed := strings.Join(append(append(append([]string{}, longParts[:n]...), mixedParts[n]), longParts[n+1:]...), `\`)
	t.Logf("long %s\nshort %s\nmixed %s", long, short, mixed)

	read := func(keys, store string) Store {
		t.Helper()
		write(t, filepath.Join(p, CacheFile), cacheJSON(t, pair{keys, root()}, pair{keys + `\INBOX.msf`, folder(FlagInbox, 1, 1)}))
		return readStore(t, p, store)
	}
	if s := read(long, long); s.Mode != Exact {
		t.Fatalf("the positive twin: %+v", s)
	}
	for _, c := range []struct{ keys, store string }{{short, long}, {long, short}, {mixed, long}, {mixed, short}} {
		if s := read(c.keys, c.store); s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) {
			t.Errorf("keys %s, store %s: mode %v, folders %q; want exact, INBOX", c.keys, c.store, s.Mode, paths(s.Folders))
		}
	}

	gone := filepath.Join(p, "Not There", "ImapMail", "imap.example.com")
	if s := read(long, gone); s.Mode == CantTellKeys {
		t.Fatalf("a store that is not there read as can't tell: %+v", s)
	}
}
