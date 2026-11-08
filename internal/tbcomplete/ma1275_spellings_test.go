package tbcomplete

// C1a adversarial pass round 2's R2-01 (docs/review-thunderbird-completeness-c1a-adversarial-r2.md):
// the store's path as given ranks above its other spellings, as C1A-05 ranks byte for byte above
// folded.

import (
	"path/filepath"
	"testing"
)

// covers: MA-1275, MA-1278
// Thunderbird writes a key byte for byte from the path it holds and never removes the old ones
// (bug 1726660), so a profile path once held through a link and now through its target, or once
// spelled 8.3 short and now long on Windows, leaves two key sets, each byte-exact for one of the
// store's spellings. A key byte-exact at the store's path as given wins; a key byte-exact only at
// another spelling (the link's target, a long or short form) is then an old-path key: ignored and
// counted beside an exact key for its folder, "can't tell" (OldPathOnly) while the folder is still
// in the store, ignored when it is gone. With no key at the path as given, the other spellings are
// exact, as before (MA-1278's cases, macOS's /var). Positive twins first.
func TestTheStoresPathAsGivenWinsOverItsOtherSpellings(t *testing.T) {
	long := `C:\Users\Alex Long\AppData\Roaming\Thunderbird\Profiles\ab.default\ImapMail\imap.example.com`
	short := `C:\Users\ALEXLO~1\AppData\Roaming\THUNDE~1\Profiles\AB~1.DEF\ImapMail\IMAPEX~1.COM`
	link, target := "/link/p/ImapMail/h", "/real/p/ImapMail/h"
	read := func(data string, spellings []string, goos string, variants ...string) Store {
		t.Helper()
		c, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		return c.storeAt(spellings, goos, variants...)
	}
	for _, c := range []struct {
		name, given, other, sep, goos string
		spellings, variants           []string
	}{
		{"Windows, given long, keys short and long", long, short, `\`, "windows", []string{long}, []string{long, short}},
		{"Windows, given short, keys long and short", short, long, `\`, "windows", []string{short}, []string{short, long}},
		{"a link and its target, given the link", link, target, "/", "linux", []string{link, target}, nil},
		{"a link and its target, given the target", target, link, "/", "linux", []string{target, link}, nil},
	} {
		t.Run(c.name, func(t *testing.T) {
			// Positive twin: with only the other spelling's keys, they are exact, as before.
			only := cacheJSON(t, pair{c.other, root()}, pair{c.other + c.sep + "INBOX.msf", folder(FlagInbox, 1, 1)})
			if s := read(only, c.spellings, c.goos, c.variants...); s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || s.Root == nil {
				t.Fatalf("the positive twin: mode %v, folders %q", s.Mode, paths(s.Folders))
			}
			// Both written, the other spelling's first in the file: the path as given wins.
			both := cacheJSON(t, pair{c.other, root()}, pair{c.other + c.sep + "INBOX.msf", folder(FlagInbox, 1, 1)},
				pair{c.other + c.sep + "Gone.msf", folder(0, 4, 4)},
				pair{c.given, root()}, pair{c.given + c.sep + "INBOX.msf", folder(FlagInbox, 2, 2)})
			s := read(both, c.spellings, c.goos, c.variants...)
			if s.Mode != Exact || len(s.Duplicates) != 0 || !equal(paths(s.Folders), []string{"INBOX"}) || s.Root == nil || s.IgnoredOldKeys != 3 || len(s.OldPathOnly) != 0 {
				t.Fatalf("got mode %v, folders %q, duplicates %q, old-path-only %q, ignored %d; want exact, INBOX, no duplicate, 3 ignored",
					s.Mode, paths(s.Folders), s.Duplicates, paths(s.OldPathOnly), s.IgnoredOldKeys)
			}
			if e := folderAt(t, s, "INBOX"); e.Key != c.given+c.sep+"INBOX.msf" || e.TotalMsgs.Value != 2 {
				t.Fatalf("INBOX read from %s", e.Key)
			}
			if s.Root.Key != c.given {
				t.Fatalf("the root read from %s", s.Root.Key)
			}
		})
	}

	t.Run("an other spelling's key alone for a folder still in the store: can't tell", func(t *testing.T) {
		_, store := newProfile(t, "imap.example.com")
		write(t, filepath.Join(store, "Sent"), "From x\n")
		other := "/elsewhere/p/ImapMail/imap.example.com" // stands for the link target the store is reached through
		goos := "linux"
		if filepath.Separator == '\\' {
			goos = "windows"
		}
		sep := string(filepath.Separator)
		data := cacheJSON(t, pair{store + sep + "INBOX.msf", folder(FlagInbox, 1, 1)},
			pair{other + "/Sent.msf", folder(0, 5, 5)}, pair{other + "/Gone.msf", folder(0, 5, 5)})
		s := read(data, []string{store, other}, goos)
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || !equal(paths(s.OldPathOnly), []string{"Sent"}) || s.IgnoredOldKeys != 1 {
			t.Fatalf("got folders %q, old-path-only %q, ignored %d; want INBOX, Sent can't tell, Gone ignored", paths(s.Folders), paths(s.OldPathOnly), s.IgnoredOldKeys)
		}
	})

	t.Run("a path as given with no parts matches no key itself", func(t *testing.T) {
		s := read(cacheJSON(t, pair{target + "/INBOX.msf", folder(FlagInbox, 1, 1)}), []string{"/", target}, "linux")
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || len(s.Refused) != 0 {
			t.Fatalf("mode %v, folders %q, refused %v; want exact by the other spelling", s.Mode, paths(s.Folders), s.Refused)
		}
	})

	t.Run("a refused key at the path as given does not demote the other spelling's keys", func(t *testing.T) {
		data := cacheJSON(t, pair{target + "/INBOX.msf", folder(FlagInbox, 1, 1)}, pair{link + "/..sbd/INBOX.msf", folder(0, 1, 1)})
		s := read(data, []string{link, target}, "linux")
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || len(s.Refused) != 1 {
			t.Fatalf("mode %v, folders %q, refused %v; want exact by the target's key, the bad key refused", s.Mode, paths(s.Folders), s.Refused)
		}
	})
}
