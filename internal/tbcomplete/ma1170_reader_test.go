package tbcomplete

// Slice C1a, MA-1170: the folderCache.json reader gives, for one store, each folder the cache
// lists with its fields read, by docs/design-thunderbird-completeness.md §2.1, §4.1 step 2 (its
// TC-3, TC-10, TC-15, foldcheck M2 and foldcheck2 L2 notes) and §4.1 step 3 (TC-2, M3).

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// gmailShape is an owner-shaped Gmail store (the numbers' shape only, invented label names): the
// account root, INBOX, [Gmail]/All Mail, Sent Mail, Spam (not kept) and two labels, one of them
// listed with headers still to come.
func gmailShape(store string) []pair {
	j := func(p ...string) string { return filepath.Join(append([]string{store}, p...)...) }
	return []pair{
		{store, root()},
		{j("INBOX.msf"), folder(FlagInbox, 10881, 10881)},
		{j("[Gmail].sbd", "All Mail.msf"), folder(FlagAllMail|FlagArchive, 238169, 238169)},
		{j("[Gmail].sbd", "Sent Mail.msf"), folder(FlagSentMail, 33822, 33822)},
		{j("[Gmail].sbd", "Spam.msf"), folder(FlagJunk, 126, 126).with("flags", FlagJunk|FlagImapBox)},
		{j("Projects.msf"), folder(0, 772, 772)},
		{j("Receipts.msf"), folder(0, 30, 31).with("pendingMsgs", 3)},
		{j("Headers to come.msf"), folder(0, 10, 12)},
		{j("Outlook-ish.msf"), folder(0, 5, 0)},
	}
}

// covers: MA-1170
// The positive twin of every refusal: an owner-shaped Gmail profile, with a second account on the
// same server (ImapMail/imap.gmail.com-1) and Local Folders in the same cache, gives the store
// exactly its own folders, each field as written: listed is totalMsgs + pendingMsgs, or
// serverTotal when that is larger (design §2.2: one Outlook folder never opened showed totalMsgs 5
// and serverTotal 0, and listed keeps 5); lastSyncTimeInSec is read; the root keeps its -1; Spam's
// flags say not kept; and All Mail is the one folder with the All Mail flag.
func TestTheReaderGivesEachFolderOfTheStore(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	other := filepath.Join(p, "ImapMail", "imap.gmail.com-1")
	local := filepath.Join(p, "Mail", "Local Folders")
	pairs := append(gmailShape(store),
		pair{other, root()},
		pair{filepath.Join(other, "INBOX.msf"), folder(FlagInbox, 9, 9)},
		pair{filepath.Join(local, "Inbox.msf"), fields{"flags": 0x1000, "totalMsgs": 2, "expungedBytes": 0}},
	)
	writeCache(t, p, pairs...)

	s := readStore(t, p, store)
	want := []string{"Headers to come", "INBOX", "Outlook-ish", "Projects", "Receipts", "[Gmail]/All Mail", "[Gmail]/Sent Mail", "[Gmail]/Spam"}
	if s.Mode != Exact || !equal(paths(s.Folders), want) {
		t.Fatalf("mode %v, folders %q; want exact, %q", s.Mode, paths(s.Folders), want)
	}
	inbox := folderAt(t, s, "INBOX")
	if !inbox.Valid() || !inbox.FlagsKnown() || !inbox.Has(FlagInbox) || !inbox.Has(FlagOffline) ||
		inbox.TotalMsgs != (Field{OK, 10881, ""}) || inbox.LastSyncTimeInSec != (Field{OK, 1762500000, ""}) ||
		inbox.ExpungedBytes != (Field{OK, 0, ""}) || inbox.FolderSize != (Field{OK, 10881000, ""}) {
		t.Fatalf("INBOX read as %+v", inbox)
	}
	for path, listed := range map[string]int64{"INBOX": 10881, "[Gmail]/All Mail": 238169, "Receipts": 33, "Headers to come": 12, "Outlook-ish": 5} {
		if got, ok := folderAt(t, s, path).Listed(); !ok || got != listed {
			t.Errorf("%s listed %d (%v), want %d", path, got, ok, listed)
		}
	}
	if spam := folderAt(t, s, "[Gmail]/Spam"); spam.Has(FlagOffline) || !spam.Has(FlagJunk) {
		t.Errorf("Spam's flags read as %#x", spam.Flags.Value)
	}
	if s.Root == nil || s.Root.TotalMsgs != (Field{OK, -1, ""}) || !s.Root.Root {
		t.Fatalf("root %+v; want its totalMsgs -1", s.Root)
	}
	if s.AllMail.Kind != AllMailOne || s.AllMail.Entry == nil || s.AllMail.Entry.Path != "[Gmail]/All Mail" {
		t.Fatalf("All Mail %+v; want the one folder [Gmail]/All Mail", s.AllMail)
	}
	if len(s.OldPathOnly)+len(s.Duplicates)+len(s.DuplicateKeys)+len(s.Refused)+s.IgnoredOldKeys != 0 {
		t.Fatalf("a healthy cache reported problems: %+v", s)
	}
	for _, e := range s.Folders {
		if !strings.HasPrefix(e.Key, store+string(filepath.Separator)) {
			t.Errorf("folder %s comes from another store's key %s", e.Path, e.Key)
		}
	}
	// The other account's store gets its own folders only.
	if o := readStore(t, p, other); !equal(paths(o.Folders), []string{"INBOX"}) {
		t.Fatalf("imap.gmail.com-1 got %q", paths(o.Folders))
	}
}

// covers: MA-1170
// Fixtures in the key shapes Thunderbird writes on each OS (design §2.1 and §2.5): a Mac profile's
// POSIX keys, and the Windows lab's backslash keys with JSON-escaped names (\u2014, ü). Each is
// read by its own OS's path rules (MA-1265's rule: a backslash separates folders on Windows only),
// so the tests run on any OS. On Windows a drive letter and names compare case-blind.
func TestKeysInEachOSsShape(t *testing.T) {
	mac, err := os.ReadFile(filepath.Join("testdata", "mac-gmail.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := parseStore(t, string(mac), "/Users/tester/Library/Thunderbird/Profiles/ab12cd34.default-release/ImapMail/imap.gmail.com", "darwin")
	if want := []string{"INBOX", "Notes — 2026", "[Gmail]", "[Gmail]/All Mail", "[Gmail]/Sent Mail"}; s.Mode != Exact || !equal(paths(s.Folders), want) {
		t.Fatalf("mac: mode %v, folders %q; want %q", s.Mode, paths(s.Folders), want)
	}

	win, err := os.ReadFile(filepath.Join("testdata", "windows-lab.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, storePath := range []string{
		`C:\Users\Public\tb-completeness-lab\fresh-imap\ImapMail\imap.example.invalid`,
		`c:\users\public\TB-COMPLETENESS-LAB\fresh-imap\ImapMail\imap.example.invalid`,
	} {
		s := parseStore(t, string(win), storePath, "windows")
		if want := []string{"Drafts — old", "Entwürfe", "INBOX", "INBOX/Sub"}; s.Mode != Exact || !equal(paths(s.Folders), want) {
			t.Fatalf("windows %s: mode %v, folders %q; want %q", storePath, s.Mode, paths(s.Folders), want)
		}
		if inbox := folderAt(t, s, "INBOX"); inbox.LastSyncTimeInSec.State != Missing || !inbox.Valid() {
			t.Fatalf("the never-connected INBOX read as %+v; want no lastSyncTimeInSec, valid", inbox)
		}
	}
}

// covers: MA-1170
// Keys are compared with the store by whole path components (TC-10): the store imap.gmail.com never
// takes keys of imap.gmail.com-1, of imap.gmail.com.sbd, or of a folder whose name only starts
// with it. Positive twin: its own key is taken.
func TestKeysAreComparedByWholeComponents(t *testing.T) {
	const store = "/p/ImapMail/imap.gmail.com"
	data := cacheJSON(t,
		pair{store + "/INBOX.msf", folder(FlagInbox, 1, 1)},
		pair{"/p/ImapMail/imap.gmail.com-1/Spoof.msf", folder(0, 2, 2)},
		pair{"/p/ImapMail/imap.gmail.com.sbd/Spoof2.msf", folder(0, 2, 2)},
		pair{"/p/ImapMail/imap.gmail.comX/Spoof3.msf", folder(0, 2, 2)},
		pair{"/p/ImapMail/ximap.gmail.com/Spoof4.msf", folder(0, 2, 2)},
	)
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if s := parseStore(t, data, store, goos); !equal(paths(s.Folders), []string{"INBOX"}) {
			t.Fatalf("%s: folders %q; want only INBOX", goos, paths(s.Folders))
		}
	}
}

// covers: MA-1170
// A key's separators follow the OS it is read on (MA-1265's rule), so a key in another OS's shape
// fails safe: a Windows-shaped key read on macOS or Linux is one name with backslashes in it,
// matches no store, and the store has no keys (C1b: can't tell); a POSIX key read on Windows (a
// profile copied from a Mac) is found by its tail. On macOS and Linux a backslash is part of a
// folder's name.
func TestKeysFollowTheOSsPathRules(t *testing.T) {
	winKey := `C:\Users\x\AppData\Roaming\Thunderbird\Profiles\p\ImapMail\imap.gmail.com\INBOX.msf`
	for _, goos := range []string{"darwin", "linux"} {
		s := parseStore(t, cacheJSON(t, pair{winKey, folder(FlagInbox, 1, 1)}), "/Users/x/p/ImapMail/imap.gmail.com", goos)
		if s.Mode != NoKeys || len(s.Folders)+len(s.Refused) != 0 {
			t.Fatalf("%s: a Windows-shaped key gave %+v; want no keys of the store", goos, s)
		}
	}
	s := parseStore(t, cacheJSON(t, pair{"/Users/x/p/ImapMail/imap.gmail.com/INBOX.msf", folder(FlagInbox, 1, 1)}),
		`C:\Users\x\p\ImapMail\imap.gmail.com`, "windows")
	if s.Mode != Tail || !equal(paths(s.Folders), []string{"INBOX"}) {
		t.Fatalf("windows: a POSIX key gave mode %v, %q; want it found by its tail", s.Mode, paths(s.Folders))
	}
	s = parseStore(t, cacheJSON(t, pair{`/p/ImapMail/imap.gmail.com/a\b.msf`, folder(0, 1, 1)}), "/p/ImapMail/imap.gmail.com", "linux")
	if !equal(paths(s.Folders), []string{`a\b`}) {
		t.Fatalf("linux: a backslash in a name gave %q", paths(s.Folders))
	}
	// A profile on a network share (UNC): the key's doubled leading separator is the share's.
	unc := `\\fileserver\home\x\Thunderbird\Profiles\p\ImapMail\imap.gmail.com`
	s = parseStore(t, cacheJSON(t, pair{unc + `\INBOX.msf`, folder(FlagInbox, 1, 1)}), unc, "windows")
	if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) {
		t.Fatalf("windows: a UNC store gave mode %v, %q; want it matched exactly", s.Mode, paths(s.Folders))
	}
}

// covers: MA-1170
// Unicode NFC and, on Windows and macOS, case (TC-10): a key in NFD still matches a store spelled
// in NFC exactly; two keys that differ only by normalisation, or (on Windows and macOS) by case,
// are two keys for one folder (design §4.1 step 2 item 4, M2: two exact keys → can't tell), while
// on Linux the case twins are two folders, as its file systems make them.
func TestUnicodeAndCaseVariants(t *testing.T) {
	nfc, nfd := "Entw\u00fcrfe", "Entwu\u0308rfe"
	store := "/p/ImapMail/imap.example.com"
	s := parseStore(t, cacheJSON(t, pair{"/p/ImapMail/imap.example.com/" + nfd + ".msf", folder(0, 1, 1)}), "/p/ImapMail/imap.exam\u0070le.com", "linux")
	if s.Mode != Exact || len(s.Folders) != 1 {
		t.Fatalf("an NFD name gave %+v; want it read", s)
	}
	s = parseStore(t, cacheJSON(t, pair{"/p/ImapMail/mail.müller.de/INBOX.msf", folder(0, 1, 1)}), "/p/ImapMail/mail.müller.de", "linux")
	if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) {
		t.Fatalf("an NFD key gave %+v; want it matched exactly", s)
	}
	twins := cacheJSON(t, pair{store + "/" + nfc + ".msf", folder(0, 1, 1)}, pair{store + "/" + nfd + ".msf", folder(0, 1, 1)})
	for _, goos := range []string{"linux", "darwin", "windows"} {
		if s := parseStore(t, twins, store, goos); len(s.Folders) != 0 || len(s.Duplicates) != 1 {
			t.Fatalf("%s: NFC and NFD keys for one folder gave %+v; want one duplicate", goos, s)
		}
	}
	cases := cacheJSON(t, pair{store + "/INBOX.msf", folder(0, 1, 1)}, pair{store + "/Inbox.msf", folder(0, 1, 1)})
	for goos, dup := range map[string]bool{"darwin": true, "windows": true, "linux": false} {
		s := parseStore(t, cases, store, goos)
		if dup && (len(s.Duplicates) != 1 || len(s.Folders) != 0) || !dup && len(s.Folders) != 2 {
			t.Fatalf("%s: INBOX.msf and Inbox.msf gave folders %q, duplicates %q", goos, paths(s.Folders), s.Duplicates)
		}
	}
}

// covers: MA-1170
// One key-matching rule (foldcheck M2, its six fixtures; the "with the other folders behind"
// fixture is the verdict's, C1b, and reads here as the "can't tell" one does). Exact mode applies
// when any key matches the store's current path: an old-path key beside an exact key for the same
// folder is ignored and counted; an old-path key for a folder with no exact key is "can't tell"
// when the folder is still in the current store (its mailbox file or its .msf is there, checked
// with Lstat inside the store), and ignored and counted when it is not; two exact keys for one
// folder are a duplicate. Tail mode applies only when no key matches exactly (a moved profile):
// folders are found by their tail, and two tail keys for one folder are a duplicate.
func TestOneKeyMatchingRule(t *testing.T) {
	old := filepath.Join(string(filepath.Separator)+"old-place", "profile", "ImapMail", "imap.gmail.com")
	old2 := filepath.Join(string(filepath.Separator)+"another-place", "ImapMail", "imap.gmail.com")

	t.Run("a copied profile with old and new keys for every folder is read", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(store, "INBOX"), "From x\n") // the folders are on disk, so an old
		write(t, filepath.Join(store, "Sent"), "From x\n")  // key is ignored for its exact key alone
		var pairs []pair
		for _, f := range []string{"INBOX.msf", "Sent.msf"} {
			pairs = append(pairs, pair{filepath.Join(old, f), folder(0, 9, 9)}, pair{filepath.Join(store, f), folder(0, 1, 1)})
		}
		writeCache(t, p, append(pairs, pair{old, root()}, pair{store, root()})...)
		s := readStore(t, p, store)
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX", "Sent"}) || s.IgnoredOldKeys != 3 || len(s.OldPathOnly)+len(s.Duplicates) != 0 {
			t.Fatalf("got %+v; want exact, both folders from their current keys, 3 old keys ignored", s)
		}
		if got := folderAt(t, s, "INBOX").TotalMsgs.Value; got != 1 {
			t.Fatalf("INBOX read from the old key (totalMsgs %d)", got)
		}
	})

	t.Run("an old key only, the folder's mailbox file still in the store: can't tell", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(store, "Sent"), "From x\n")
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "Sent.msf"), folder(0, 5, 5)})
		s := readStore(t, p, store)
		if s.Mode != Exact || !equal(paths(s.Folders), []string{"INBOX"}) || !equal(paths(s.OldPathOnly), []string{"Sent"}) {
			t.Fatalf("got folders %q, old-path-only %q; want INBOX and Sent can't tell", paths(s.Folders), paths(s.OldPathOnly))
		}
	})

	t.Run("an old key only, the folder's .msf still in the store: can't tell", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(store, "[Gmail].sbd", "Sent Mail.msf"), "// mork\n")
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "[Gmail].sbd", "Sent Mail.msf"), folder(0, 5, 5)})
		if s := readStore(t, p, store); !equal(paths(s.OldPathOnly), []string{"[Gmail]/Sent Mail"}) {
			t.Fatalf("got old-path-only %q; want [Gmail]/Sent Mail", paths(s.OldPathOnly))
		}
	})

	t.Run("an old key only for a folder gone from the store is ignored and counted", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "Deleted.msf"), folder(0, 5, 5)})
		s := readStore(t, p, store)
		if !equal(paths(s.Folders), []string{"INBOX"}) || len(s.OldPathOnly) != 0 || s.IgnoredOldKeys != 1 {
			t.Fatalf("got %+v; want INBOX only, the old key ignored and counted", s)
		}
	})

	t.Run("an old key only, a file where its folder would be: gone", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(store, "a.sbd"), "a file, not a folder")
		writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{filepath.Join(old, "a.sbd", "b.msf"), folder(0, 5, 5)})
		if s := readStore(t, p, store); len(s.OldPathOnly) != 0 || s.IgnoredOldKeys != 1 {
			t.Fatalf("got %+v; want the old key ignored and counted", s)
		}
	})

	t.Run("two exact keys for one folder are a duplicate", func(t *testing.T) {
		const store = "/p/ImapMail/imap.gmail.com"
		data := cacheJSON(t, pair{store + "/INBOX.msf", folder(0, 1, 1)}, pair{store + "//INBOX.msf", folder(0, 1, 1)},
			pair{store + "/Sent.msf", folder(0, 1, 1)}, pair{store + "/sent.msf", folder(0, 1, 1)})
		s := parseStore(t, data, store, "darwin")
		if !equal(paths(s.Folders), []string{"INBOX"}) || !equal(s.Duplicates, []string{"Sent"}) || len(s.Refused) != 1 {
			t.Fatalf("got folders %q, duplicates %q, refused %v; want Sent a duplicate and the doubled separator refused", paths(s.Folders), s.Duplicates, s.Refused)
		}
	})

	t.Run("tail mode: a moved profile is read by its tails", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		writeCache(t, p, pair{old, root()}, pair{filepath.Join(old, "INBOX.msf"), folder(0, 4, 4)}, pair{filepath.Join(old, "[Gmail].sbd", "All Mail.msf"), folder(FlagAllMail, 7, 7)})
		s := readStore(t, p, store)
		if s.Mode != Tail || !equal(paths(s.Folders), []string{"INBOX", "[Gmail]/All Mail"}) || s.Root == nil {
			t.Fatalf("got %+v; want tail mode, both folders and the root", s)
		}
	})

	t.Run("tail mode: the store's name higher up the old place is not the store", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		odd := filepath.Join(string(filepath.Separator)+"imap.gmail.com", "profile", "ImapMail", "imap.gmail.com")
		writeCache(t, p, pair{filepath.Join(odd, "INBOX.msf"), folder(0, 4, 4)})
		if s := readStore(t, p, store); s.Mode != Tail || !equal(paths(s.Folders), []string{"INBOX"}) {
			t.Fatalf("got %+v; want INBOX by the tail after the last imap.gmail.com", s)
		}
	})

	t.Run("tail mode: two tail keys for one folder are a duplicate", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		writeCache(t, p, pair{filepath.Join(old, "INBOX.msf"), folder(0, 4, 4)}, pair{filepath.Join(old2, "INBOX.msf"), folder(0, 9, 9)},
			pair{filepath.Join(old, "Sent.msf"), folder(0, 1, 1)})
		s := readStore(t, p, store)
		if s.Mode != Tail || !equal(paths(s.Folders), []string{"Sent"}) || !equal(s.Duplicates, []string{"INBOX"}) {
			t.Fatalf("got folders %q, duplicates %q; want Sent, and INBOX a duplicate", paths(s.Folders), s.Duplicates)
		}
	})
}

// covers: MA-1170
// A key's tail is refused before any file check (completeness foldcheck2 L2): a tail with a "." or
// ".." part, an absolute tail (a leading separator, a doubled one), a drive letter, a UNC prefix
// or a NUL byte is never used to build a path. It is listed in Refused (C1b: invalid_entry) and no
// Lstat is made for it, in exact mode and for an old-path key alike. Positive twin: an old-path
// key with a good tail is checked, and only inside the store.
func TestKeyTailsAreRefusedBeforeAnyFileCheck(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	sep := string(filepath.Separator)
	oldPlace := sep + "old" + sep + "ImapMail" + sep + "imap.gmail.com"
	log := recordLstat(t)
	writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{oldPlace + sep + "Gone.msf", folder(0, 1, 1)})
	s := readStore(t, p, store)
	if s.IgnoredOldKeys != 1 || len(log.all()) == 0 || len(log.outside(store)) != 0 {
		t.Fatalf("the positive twin: ignored %d, lstat %q; want the old key checked inside the store only", s.IgnoredOldKeys, log.all())
	}

	bad := []string{
		".", "..", "../x.msf", "./INBOX.msf", "a.sbd/../../x.msf", "a.sbd/./x.msf",
		"/etc/passwd.msf", "//srv/x.msf", `\x.msf`, `\\srv\share\x.msf`, `..\..\x.msf`,
		"C:x.msf", `C:\Windows\x.msf`, "a.sbd/C:x.msf", "x\x00.msf",
		// a folder named . or .. (found by FuzzFolderCache: the mailbox file's path, the name
		// without .msf, would be the store itself or its parent)
		"..msf", "...msf", "a.sbd/...msf", "..sbd/x.msf", `a\...msf`,
		// dots and spaces only, which Windows reads as . or .. (it drops trailing dots and spaces)
		".. .msf", "....msf", ". .sbd/x.msf", " .msf",
	}
	for _, tail := range bad {
		for _, base := range []string{store, oldPlace} {
			name := strings.ReplaceAll(tail, "\x00", "<NUL>") + " under " + filepath.Base(filepath.Dir(filepath.Dir(base)))
			t.Run(name, func(t *testing.T) {
				log := recordLstat(t)
				key := base + sep + tail
				writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)}, pair{key, folder(0, 1, 1)})
				s := readStore(t, p, store)
				if len(s.Refused) != 1 || s.Refused[0].Key != key || s.Refused[0].Why == "" {
					t.Fatalf("refused %+v; want the key %q refused with a reason", s.Refused, key)
				}
				if got := log.all(); len(got) != 0 {
					t.Fatalf("Lstat made for a refused tail: %q", got)
				}
				if !equal(paths(s.Folders), []string{"INBOX"}) || len(s.OldPathOnly) != 0 {
					t.Fatalf("folders %q, old-path-only %q; want INBOX only", paths(s.Folders), paths(s.OldPathOnly))
				}
			})
		}
	}

	t.Run("on Windows a colon anywhere in a tail", func(t *testing.T) {
		const store = `C:\p\ImapMail\imap.gmail.com`
		s := parseStore(t, cacheJSON(t, pair{store + `\x.msf`, folder(0, 1, 1)}), store, "windows")
		if !equal(paths(s.Folders), []string{"x"}) {
			t.Fatalf("the positive twin: %q", paths(s.Folders))
		}
		// Neither starts with a drive letter, and both end in .msf: only the colon refuses them.
		for _, key := range []string{store + `\ab:c.msf`, store + `\a.sbd\name:stream.msf`} {
			s := parseStore(t, cacheJSON(t, pair{key, folder(0, 1, 1)}), store, "windows")
			if len(s.Refused) != 1 || len(s.Folders) != 0 {
				t.Fatalf("%s: refused %+v, folders %q; want it refused", key, s.Refused, paths(s.Folders))
			}
		}
	})
}

// covers: MA-1170
// A key is a string, never a path that is opened (design §4.1 step 2): a key naming a file outside
// the profile (/etc/passwd, a folder of another program) is not the store's and is never checked;
// a key under the store that is not shaped like a folder's (X.sbd/…/Y.msf) is refused, never
// checked; and an exact key whose path holds a link to a file outside the profile is read from the
// cache alone, with no Lstat at all.
func TestAKeyIsNeverOpened(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	secret := filepath.Join(t.TempDir(), "secret.msf")
	write(t, secret, "not Thunderbird's")
	log := recordLstat(t)
	writeCache(t, p,
		pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)},
		pair{secret, folder(0, 1, 1)},
		pair{filepath.Join(string(filepath.Separator)+"etc", "passwd"), folder(0, 1, 1)},
		pair{filepath.Join(store, "notes.txt"), folder(0, 1, 1)},
		pair{filepath.Join(store, "a.sbd", "b"), folder(0, 1, 1)},
		pair{filepath.Join(store, "plain", "b.msf"), folder(0, 1, 1)},
	)
	s := readStore(t, p, store)
	if !equal(paths(s.Folders), []string{"INBOX"}) || len(s.Refused) != 3 {
		t.Fatalf("folders %q, refused %+v; want INBOX, and the two keys of no folder's shape refused", paths(s.Folders), s.Refused)
	}
	if got := log.all(); len(got) != 0 {
		t.Fatalf("keys were checked on disk: %q", got)
	}
	if b, _ := os.ReadFile(secret); string(b) != "not Thunderbird's" {
		t.Fatal("the file a key named was changed")
	}
}

// covers: MA-1170
// A key may hold control characters, a bidi override or a newline (JSON escapes them; the names
// come from the mail server): the folder's Path keeps them as written, for C3 to clean (TC-7). A
// NUL byte in a tail is refused (L2), never handed to the file system.
func TestControlCharactersInKeysAreKeptForTheWords(t *testing.T) {
	const store = "/p/ImapMail/imap.gmail.com"
	odd := "a\u0001b\u202ec\nd"
	data := cacheJSON(t, pair{store + "/" + odd + ".msf", folder(0, 1, 1)}, pair{store + "/x\u0000y.msf", folder(0, 1, 1)})
	s := parseStore(t, data, store, runtime.GOOS)
	if !equal(paths(s.Folders), []string{odd}) || len(s.Refused) != 1 {
		t.Fatalf("folders %q, refused %+v; want the odd name kept and the NUL refused", paths(s.Folders), s.Refused)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(data), &raw); err != nil || len(raw) != 2 {
		t.Fatalf("the fixture is not the JSON it means to be: %v", err)
	}
	assure.Reached(t, s.Refused[0].Why, "the NUL refusal's reason")
}
