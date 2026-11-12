package tbcomplete

// The fixes of C1a's adversarial pass (docs/review-thunderbird-completeness-c1a-adversarial.md,
// C1A-01 and C1A-03 to C1A-08, the lead's calls of 2026-11-08), one test function per row:
// MA-1272 to MA-1278. Real temp profiles where a file is checked; key shapes of another OS are read
// by that OS's rules through storeAt.

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1272
// Thunderbird writes -1 for what it does not know yet, on any folder, not only the account root
// (nsMsgDBFolder.cpp ESR 153: mFolderSize(kSizeUnknown) :243 and mNumTotalMessages(-1) :236,
// written as they are at :1228 and :1233). folderSize -1 is Missing, saying why, and the folder
// stays Valid (4 of the owner's 35 real folders hold it). totalMsgs -1 is NotCounted: the entry is
// not Valid (Thunderbird has no count either) but NotCountedYet says its one failure is that, so
// C1b never words it as changed by hand. Every other negative off the root stays Broken, and the
// root keeps its -1 as a value. Positive twin first: a folder with a known size is Valid.
func TestThunderbirdsMinusOneIsNotKnownYet(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	j := func(name string) string { return filepath.Join(store, name) }
	writeCache(t, p, pair{store, root()},
		pair{j("INBOX.msf"), folder(FlagInbox, 10, 10)},
		pair{j("Size unknown.msf"), folder(0, 10, 10).with("folderSize", -1)},
		pair{j("Size unknown too.msf"), folder(FlagSentMail, 0, 0).with("folderSize", -1)},
		pair{j("Not counted.msf"), folder(0, 0, 5).with("totalMsgs", -1)},
		pair{j("Not counted, size unknown.msf"), folder(0, 0, 5).with("totalMsgs", -1, "folderSize", -1)},
	)
	s := readStore(t, p, store)
	if e := folderAt(t, s, "INBOX"); !e.Valid() || e.NotCountedYet() || e.FolderSize != (Field{OK, 10000, ""}) {
		t.Fatalf("the positive twin: %+v", e)
	}
	for _, name := range []string{"Size unknown", "Size unknown too"} {
		e := folderAt(t, s, name)
		if e.FolderSize.State != Missing || e.FolderSize.Why == "" || !e.Valid() || e.NotCountedYet() {
			t.Errorf("%s: folderSize -1 read as %+v, valid %v; want Missing with why, valid", name, e.FolderSize, e.Valid())
		}
		if n, ok := e.Listed(); !ok || n != e.TotalMsgs.Value {
			t.Errorf("%s: listed %d (%v)", name, n, ok)
		}
	}
	for _, name := range []string{"Not counted", "Not counted, size unknown"} {
		e := folderAt(t, s, name)
		if e.TotalMsgs.State != NotCounted || e.Valid() || !e.NotCountedYet() {
			t.Errorf("%s: totalMsgs -1 read as %+v, valid %v, not counted yet %v; want NotCounted, not valid, not counted yet",
				name, e.TotalMsgs, e.Valid(), e.NotCountedYet())
		}
		if _, ok := e.Listed(); ok {
			t.Errorf("%s: a folder Thunderbird has not counted gave a listed count", name)
		}
		if w := strings.ToLower(e.TotalMsgs.Why); w == "" || strings.Contains(w, "hand") || strings.Contains(w, "outside") {
			t.Errorf("%s: why %q; want it to say not counted yet, never changed by hand", name, e.TotalMsgs.Why)
		}
	}
	if s.Root == nil || s.Root.TotalMsgs != (Field{OK, -1, ""}) || s.Root.FolderSize != (Field{OK, -1, ""}) || s.Root.NotCountedYet() {
		t.Fatalf("the root's -1 read as %+v", s.Root)
	}

	const st = "/p/ImapMail/imap.gmail.com"
	read := func(f fields) Entry {
		t.Helper()
		return folderAt(t, parseStore(t, cacheJSON(t, pair{st + "/A.msf", f}), st, "linux"), "A")
	}
	t.Run("only -1, and only folderSize and totalMsgs", func(t *testing.T) {
		for _, c := range []struct {
			name string
			v    any
		}{
			{"folderSize", -2}, {"folderSize", -1 << 62}, {"totalMsgs", -2}, {"totalMsgs", -1 << 31},
			{"pendingMsgs", -1}, {"serverTotal", -1}, {"expungedBytes", -1},
		} {
			e := read(folder(0, 5, 5).with(c.name, c.v))
			if e.Valid() || e.NotCountedYet() {
				t.Errorf("%s %v: valid %v, not counted yet %v; want neither", c.name, c.v, e.Valid(), e.NotCountedYet())
			}
		}
	})
	t.Run("not counted yet only when nothing else fails", func(t *testing.T) {
		for name, f := range map[string]fields{
			"flags missing":        folder(0, 0, 5).with("totalMsgs", -1, "flags", nil),
			"pendingMsgs a string": folder(0, 0, 5).with("totalMsgs", -1, "pendingMsgs", "3"),
			"no serverTotal":       folder(0, 0, 5).with("totalMsgs", -1, "serverTotal", nil),
			"no expungedBytes":     folder(0, 0, 5).with("totalMsgs", -1, "expungedBytes", nil),
			"folderSize -2":        folder(0, 0, 5).with("totalMsgs", -1, "folderSize", -2),
		} {
			if e := read(f); e.TotalMsgs.State != NotCounted || e.NotCountedYet() || e.Valid() {
				t.Errorf("%s: %+v; want totalMsgs NotCounted, and the entry neither valid nor only not counted", name, e)
			}
		}
	})
}

// covers: MA-1273
// A file nested deeper than Thunderbird's own reader goes is refused as CacheMalformed: jsoncpp's
// CharReaderBuilder refuses a value enclosed by more than 256 nodes, the top-level object being
// one (setDefaults' stackLimit 256; OurReader::readValue), so Thunderbird never reads such a file
// either. Positive twins first: a value at depth 256 exactly reads, as does an empty list or object
// there; brackets inside a string are no nesting. Then depth 257 by a list, by an object and by a
// number, and 300, each refused, the profile left as it was.
func TestNestingDeeperThanThunderbirdReadsIsMalformed(t *testing.T) {
	const st = "/p/ImapMail/h"
	entry := `"` + st + `/INBOX.msf":` + valueJSON(t, folder(FlagInbox, 1, 1))
	// lists(n, inner) and objects(n, inner) put inner at depth n: the top object is at 1, "x"'s
	// value at 2, and n-2 lists (or objects) at 2 to n-1 enclose inner.
	lists := func(n int, inner string) string {
		return `{"x":` + strings.Repeat("[", n-2) + inner + strings.Repeat("]", n-2) + `,` + entry + `}`
	}
	objects := func(n int, inner string) string {
		return `{"x":` + strings.Repeat(`{"a":`, n-2) + inner + strings.Repeat("}", n-2) + `,` + entry + `}`
	}
	for name, data := range map[string]string{
		"a number at depth 256":         lists(256, "1"),
		"an empty list at depth 256":    lists(256, "[]"),
		"an empty list, spaces inside":  lists(256, "[ \n\t\r ]"),
		"an empty object at depth 256":  objects(256, "{}"),
		"brackets inside a key's value": `{"x":"` + strings.Repeat("[{", 400) + `",` + entry + `}`,
		"escaped quotes and brackets":   `{"x":"\"` + strings.Repeat("[", 400) + `\\",` + entry + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			s := parseStore(t, data, st, "linux")
			if !equal(paths(s.Folders), []string{"INBOX"}) {
				t.Fatalf("folders %q; want INBOX", paths(s.Folders))
			}
		})
	}
	for name, data := range map[string]string{
		"a number at depth 257":        lists(257, "1"),
		"a list at depth 257":          lists(257, "[]"),
		"an object at depth 257":       objects(257, "{}"),
		"a string at depth 257":        objects(257, `"s"`),
		"depth 300":                    lists(300, "1"),
		"the review's 300 under a key": `{"x":` + strings.Repeat("[", 300) + strings.Repeat("]", 300) + `,` + entry + `}`,
	} {
		t.Run(name, func(t *testing.T) {
			if c, err := Parse([]byte(data)); err == nil || c != nil {
				t.Fatalf("parsed; want refused")
			}
			p, _ := newProfile(t, "h")
			write(t, filepath.Join(p, CacheFile), data)
			cacheRefused(t, p, CacheMalformed)
		})
	}
}

// covers: MA-1274
// On Windows a store on a network share (\\server\share\…) and a key rooted on the current drive
// (\server\share\…), or on a drive letter, are on different volumes: neither is ever an exact key
// of the other (a doubled leading separator is a root of its own). Such a key may still be read by
// its tail, as a moved profile's (§4.1 step 2's tail mode), which never gives exact mode. Positive
// twins first: a UNC key of the UNC store is exact, also spelled in another case; a drive-letter
// key of a drive-letter store is exact.
func TestAUNCStoreAndADriveKeyNeverMatchExactly(t *testing.T) {
	unc, drive := `\\srv\share\h`, `C:\srv\share\h`
	read := func(store, key string) Store {
		return parseStore(t, cacheJSON(t, pair{key, folder(0, 1, 1)}), store, "windows")
	}
	for store, key := range map[string]string{unc: unc + `\A.msf`, `\\SRV\Share\h`: unc + `\A.msf`, drive: drive + `\A.msf`} {
		if s := read(store, key); s.Mode != Exact || !equal(paths(s.Folders), []string{"A"}) {
			t.Fatalf("the positive twin %s, %s: mode %v, %q", store, key, s.Mode, paths(s.Folders))
		}
	}
	for _, c := range []struct{ store, key string }{
		{unc, `\srv\share\h\A.msf`},  // the review's case: the current drive's root
		{unc, `/srv/share/h/A.msf`},  // the same with forward slashes
		{unc, `C:\srv\share\h\A.msf`}, // a drive letter
		{drive, `\\srv\share\h\A.msf`},
		{drive, `\srv\share\h\A.msf`},
		{`\\srv\share`, `\srv\share\A.msf`},
	} {
		if s := read(c.store, c.key); s.Mode == Exact {
			t.Errorf("store %s, key %s: read as an exact key", c.store, c.key)
		}
	}
}

// covers: MA-1275
// A key at exactly the current spelling of the store's path wins: Thunderbird looks a key up byte
// for byte (nsMsgFolderCache.cpp :335), so after the profile's path is respelled in case only, or
// in Unicode form only, it writes new keys beside the old ones, which it removes only when a
// folder is deleted (bug 1726660). With a byte-exact key in the file, a key that matches only once
// case or Unicode form is folded is an old-path key: ignored and counted beside an exact key for
// its folder (never a duplicate, so never "can't tell" for good), "can't tell" (OldPathOnly) while
// the folder is still in the store, and ignored when it is gone. With no byte-exact key, the folded
// match is exact, as before. Case on macOS and Windows; NFC and NFD on macOS and Linux.
func TestAByteExactKeyWinsOverAFoldedOne(t *testing.T) {
	cafeNFC, cafeNFD := "Caf\u00e9", "Cafe\u0301"
	for _, c := range []struct {
		name, goos, old, cur string
	}{
		{"case, macOS", "darwin", "/Users/Alex/Thunderbird/Profiles/x/ImapMail/h", "/Users/alex/Thunderbird/Profiles/x/ImapMail/h"},
		{"case, Windows", "windows", `C:\Users\Alex\AppData\Roaming\Thunderbird\Profiles\x\ImapMail\h`, `c:\Users\alex\AppData\Roaming\Thunderbird\Profiles\x\ImapMail\h`},
		{"NFD old, NFC now, macOS", "darwin", "/Users/" + cafeNFD + "/p/ImapMail/h", "/Users/" + cafeNFC + "/p/ImapMail/h"},
		{"NFC old, NFD now, macOS", "darwin", "/Users/" + cafeNFC + "/p/ImapMail/h", "/Users/" + cafeNFD + "/p/ImapMail/h"},
		{"NFD old, NFC now, Linux", "linux", "/home/" + cafeNFD + "/p/ImapMail/h", "/home/" + cafeNFC + "/p/ImapMail/h"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sep := "/"
			if c.goos == "windows" {
				sep = `\`
			}
			// Positive twin: with only the respelled keys, they are exact (folded), as before.
			only := cacheJSON(t, pair{c.old, root()}, pair{c.old + sep + "INBOX.msf", folder(FlagInbox, 1, 1)})
			if s := parseStore(t, only, c.cur, c.goos); s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) {
				t.Fatalf("the positive twin: mode %v, %q", s.Mode, paths(s.Folders))
			}
			both := cacheJSON(t, pair{c.old, root()}, pair{c.old + sep + "INBOX.msf", folder(FlagInbox, 1, 1)},
				pair{c.cur, root()}, pair{c.cur + sep + "INBOX.msf", folder(FlagInbox, 2, 2)})
			s := parseStore(t, both, c.cur, c.goos)
			if s.Mode != Exact || len(s.Duplicates) != 0 || !equal(paths(s.Folders), []string{"INBOX"}) || s.IgnoredOldKeys != 2 || s.Root == nil {
				t.Fatalf("got mode %v, folders %q, duplicates %q, ignored %d; want exact, INBOX, no duplicate, 2 ignored", s.Mode, paths(s.Folders), s.Duplicates, s.IgnoredOldKeys)
			}
			if e := folderAt(t, s, "INBOX"); e.Key != c.cur+sep+"INBOX.msf" || e.TotalMsgs.Value != 2 {
				t.Fatalf("INBOX read from %s", e.Key)
			}
		})
	}

	t.Run("a byte-exact key that is refused, or of no folder's shape, does not demote the folded keys", func(t *testing.T) {
		old, cur := "/Users/Alex/p/ImapMail/h", "/Users/alex/p/ImapMail/h"
		// "..sbd/INBOX.msf" is shaped as a folder's key but refused (a folder named "."); "notes.txt"
		// is not refused by the tail rule but is of no folder's shape.
		for _, bad := range []string{"..sbd/INBOX.msf", "notes.txt"} {
			s := parseStore(t, cacheJSON(t, pair{old + "/INBOX.msf", folder(FlagInbox, 1, 1)}, pair{cur + "/" + bad, folder(0, 1, 1)}), cur, "darwin")
			if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || len(s.Refused) != 1 {
				t.Errorf("%s: mode %v, folders %q, refused %v; want exact by the folded key, the bad key refused", bad, s.Mode, paths(s.Folders), s.Refused)
			}
		}
	})

	t.Run("a folded key alone for a folder still in the store: can't tell; gone: ignored", func(t *testing.T) {
		_, store := newProfile(t, "imap.example.com")
		write(t, filepath.Join(store, "Sent"), "From x\n")
		respelled := strings.Replace(store, string(filepath.Separator)+"ImapMail"+string(filepath.Separator), string(filepath.Separator)+"IMAPMAIL"+string(filepath.Separator), 1)
		if respelled == store {
			t.Fatal("the fixture did not respell the store")
		}
		c, err := Parse([]byte(cacheJSON(t, pair{filepath.Join(store, "INBOX.msf"), folder(FlagInbox, 1, 1)},
			pair{filepath.Join(respelled, "Sent.msf"), folder(0, 5, 5)}, pair{filepath.Join(respelled, "Gone.msf"), folder(0, 5, 5)})))
		if err != nil {
			t.Fatal(err)
		}
		goos := "darwin" // case-blind, as Windows: the real temp folder's separators decide which
		if filepath.Separator == '\\' {
			goos = "windows"
		}
		s := c.storeAt([]string{store}, goos)
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || !equal(paths(s.OldPathOnly), []string{"Sent"}) || s.IgnoredOldKeys != 1 {
			t.Fatalf("got folders %q, old-path-only %q, ignored %d; want INBOX, Sent can't tell, Gone ignored", paths(s.Folders), paths(s.OldPathOnly), s.IgnoredOldKeys)
		}
	})
}

// covers: MA-1276
// In tail mode (no key matches the store's current path: a moved profile), a key is the store's
// only under a part named as the store, itself under a part named as the store's parent (ImapMail:
// §4.1 step 2's "the key's path under ImapMail/<host>/"), and when such keys come from more than one
// place the store is CantTellKeys: no folders, All Mail can't tell, never another account's
// folders. Positive twin first: a moved profile's keys from one old place are read.
func TestTailModeNeverTakesAnotherAccountsKeys(t *testing.T) {
	moved := "/new/Profiles/x/ImapMail/imap.example.com"
	oldOwn := []pair{
		{"/old/Profiles/x/ImapMail/imap.example.com", root()},
		{"/old/Profiles/x/ImapMail/imap.example.com/INBOX.msf", folder(FlagInbox, 1, 1)},
	}
	if s := parseStore(t, cacheJSON(t, oldOwn...), moved, "darwin"); s.Mode != Tail || !equal(paths(s.Folders), []string{"INBOX"}) || s.Root == nil {
		t.Fatalf("the positive twin: %+v", s)
	}
	allMail := folder(FlagAllMail|FlagArchive, 9, 9)

	// The review's case: another live account whose own folder, elsewhere, has the store's name.
	other := "/Volumes/Data/imap.example.com"
	s := parseStore(t, cacheJSON(t, append(oldOwn, pair{other, root()}, pair{other + "/Work.msf", allMail})...), moved, "darwin")
	if s.Mode != Tail || !equal(paths(s.Folders), []string{"INBOX"}) || s.AllMail.Kind != AllMailNone {
		t.Fatalf("another folder named as the store: mode %v, folders %q, All Mail %v; want tail, INBOX only, none", s.Mode, paths(s.Folders), s.AllMail.Kind)
	}
	if s := parseStore(t, cacheJSON(t, pair{other, root()}, pair{other + "/Work.msf", allMail}), moved, "darwin"); s.Mode != NoKeys || len(s.Folders) != 0 {
		t.Fatalf("only another account's keys: %+v; want no keys", s)
	}

	// Two places under ImapMail with the store's name: which is the store's cannot be told.
	for name, extra := range map[string][]pair{
		"another ImapMail":                {{"/Volumes/Data/ImapMail/imap.example.com/Work.msf", allMail}},
		"a second old place, its root":    {{"/older/ImapMail/imap.example.com", root()}},
		"a second old place, in Windows":  {{`C:\Users\x\ImapMail\imap.example.com\INBOX.msf`, folder(FlagInbox, 5, 5)}},
		"a doubled separator is one place": nil,
		"case is one place on macOS":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			goos := "darwin"
			if strings.Contains(name, "Windows") {
				goos = "windows"
			}
			pairs := append(append([]pair{}, oldOwn...), extra...)
			if extra == nil {
				respelled := "/old/Profiles//x/ImapMail/imap.example.com/Sent.msf"
				if strings.HasPrefix(name, "case") {
					respelled = "/OLD/profiles/X/imapmail/IMAP.example.com/Sent.msf"
				}
				pairs = append(pairs, pair{respelled, folder(0, 1, 1)})
				if s := parseStore(t, cacheJSON(t, pairs...), moved, goos); s.Mode != Tail || len(s.Folders) != 2 {
					t.Fatalf("one place spelled %s: %+v; want tail, both folders", respelled, s)
				}
				return
			}
			s := parseStore(t, cacheJSON(t, pairs...), moved, goos)
			if s.Mode != CantTellKeys || s.CantTellWhy == "" || len(s.Folders)+len(s.OldPathOnly)+len(s.Duplicates) != 0 || s.Root != nil || s.AllMail.Kind != AllMailCantTell {
				t.Fatalf("got %+v; want can't tell, no folders", s)
			}
		})
	}
}

// covers: MA-1277
// Every folder of an IMAP store needs serverTotal, not only one carrying the ImapBox flag 0x2000:
// every folder of an IMAP account's tree is an IMAP folder (nsImapMailFolder.cpp ESR 153
// :353-356), which writes serverTotal whatever its flags (:1901), while ImapBox is set only once
// the server has listed it (SetBoxFlags, :1107). Positive twin first: the owner's real shape, 2
// folders in IMAP stores with no ImapBox and no lastSyncTimeInSec but serverTotal there (also the
// Windows lab's never-connected INBOX, flags 0x08001014), is Valid. An entry without serverTotal,
// with or without ImapBox, is not.
func TestEveryFolderOfAnIMAPStoreNeedsServerTotal(t *testing.T) {
	p, store := newProfile(t, "imap.example.com")
	real := folder(0, 3, 3).with("flags", 0x08001014, "lastSyncTimeInSec", nil)
	writeCache(t, p, pair{store, root()},
		pair{filepath.Join(store, "Real.msf"), real},
		pair{filepath.Join(store, "Real too.msf"), folder(0, 0, 0).with("flags", FlagOffline, "lastSyncTimeInSec", nil, "serverTotal", 0)},
		pair{filepath.Join(store, "Hand-made.msf"), fields{"flags": FlagOffline, "totalMsgs": 3, "pendingMsgs": 0, "expungedBytes": 0}},
		pair{filepath.Join(store, "ImapBox, no serverTotal.msf"), folder(0, 3, 3).with("serverTotal", nil)},
	)
	s := readStore(t, p, store)
	for _, name := range []string{"Real", "Real too"} {
		if e := folderAt(t, s, name); e.Has(FlagImapBox) || e.LastSyncTimeInSec.State != Missing || !e.Valid() {
			t.Fatalf("the positive twin %s: %+v, valid %v", name, e, e.Valid())
		}
	}
	for _, name := range []string{"Hand-made", "ImapBox, no serverTotal"} {
		e := folderAt(t, s, name)
		if e.Valid() {
			t.Errorf("%s: valid without serverTotal", name)
		}
		if _, ok := e.Listed(); ok {
			t.Errorf("%s: listed from totalMsgs alone", name)
		}
	}
}

// covers: MA-1278
// On Windows the store's path may be spelled with 8.3 short names (Windows hands out a short user
// folder in places), so Store adds the store's long and short spellings, looked up for the store's
// own path only, never a key's, and merged part by part: a key spelled long, short, or long in
// one part and short in another is exact. When the store's path exists and its spellings cannot be
// found, the store is CantTellKeys. This test runs on every OS: the spellings are given to
// storeAt as Windows gives them, and the lookup's failure is injected at its seam (the real lookup
// is MA-1278's Windows test). Positive twin first: the store spelled as its keys are is exact.
func TestTheStoresShortAndLongSpellingsAreOne(t *testing.T) {
	long := `C:\Users\Alex Long\AppData\Roaming\Thunderbird\Profiles\ab.default\ImapMail\imap.example.com`
	short := `C:\Users\ALEXLO~1\AppData\Roaming\THUNDE~1\Profiles\AB~1.DEF\ImapMail\IMAPEX~1.COM`
	mixed := `C:\Users\ALEXLO~1\AppData\Roaming\Thunderbird\Profiles\ab.default\ImapMail\imap.example.com`
	keysAt := func(base string) string {
		return cacheJSON(t, pair{base, root()}, pair{base + `\INBOX.msf`, folder(FlagInbox, 1, 1)})
	}
	read := func(data, store string, variants ...string) Store {
		c, err := Parse([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		return c.storeAt([]string{store}, "windows", variants...)
	}
	if s := read(keysAt(long), long); s.Mode != Exact {
		t.Fatalf("the positive twin: %+v", s)
	}
	if s := read(keysAt(short), long); s.Mode == Exact {
		t.Fatalf("without the spellings a short key was exact: the test would prove nothing")
	}
	for _, c := range []struct{ name, keys, store, variant string }{
		{"short keys, long store", short, long, short},
		{"long keys, short store", long, short, long},
		{"keys short in one part only", mixed, long, short},
		{"keys short in one part, store short", mixed, short, long},
	} {
		s := read(keysAt(c.keys), c.store, c.store, c.variant) // the lookup gives both spellings, one as given
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || s.Root == nil {
			t.Errorf("%s: mode %v, folders %q; want exact, INBOX", c.name, s.Mode, paths(s.Folders))
		}
	}

	t.Run("a lookup that fails: can't tell", func(t *testing.T) {
		p, store := newProfile(t, "imap.example.com")
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(FlagInbox, 1, 1)})
		if s := readStore(t, p, store); s.Mode != Exact {
			t.Fatalf("the positive twin: %+v", s)
		}
		prev := spellingsOf
		spellingsOf = func(string) ([]string, error) { return nil, errors.New("access is denied") }
		t.Cleanup(func() { spellingsOf = prev })
		s := readStore(t, p, store)
		if s.Mode != CantTellKeys || !strings.Contains(s.CantTellWhy, "access is denied") || len(s.Folders) != 0 || s.AllMail.Kind != AllMailCantTell {
			t.Fatalf("got %+v; want can't tell, saying why, no folders", s)
		}
	})
}
