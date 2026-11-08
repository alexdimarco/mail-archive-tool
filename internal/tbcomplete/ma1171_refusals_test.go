package tbcomplete

// Slice C1a, MA-1171: what the reader cannot read, it says it cannot read, field by field and
// file by file (design §2.3 and §4.1 steps 2 and 3, with TC-2, TC-3, TC-15, foldcheck M3 and the
// M4 re-read). The reader decides no verdict: C1b turns each of these into `unknown`.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// cacheRefused asserts ReadCache refuses profile's folder cache with an error of kind, naming
// folderCache.json, and leaving the profile's files as they were.
func cacheRefused(t *testing.T, profile string, kind CacheErrorKind) {
	t.Helper()
	before := listing(t, profile)
	c, err := ReadCache(profile)
	if c != nil {
		t.Fatalf("ReadCache gave a cache for a file it must refuse")
	}
	rc, msg := 0, ""
	var ce *CacheError
	if err != nil {
		rc, msg = 1, err.Error()
	}
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names(CacheFile),
		assure.NoSideEffect(func() bool { return listing(t, profile) == before }))
	if !errors.As(err, &ce) || ce.Kind != kind {
		t.Fatalf("error %v (%T); want a CacheError of kind %v", err, err, kind)
	}
}

// listing is the profile's top-level names, sizes and modes, for "changed nothing".
func listing(t *testing.T, dir string) string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range ents {
		fi, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		b.WriteString(e.Name() + fi.Mode().String() + "|")
		if fi.Mode().IsRegular() {
			b.WriteString(fi.ModTime().String())
		}
		b.WriteString("\n")
	}
	return b.String()
}

// covers: MA-1171
// A folder cache that cannot be read is an error, never an empty list (design §2.3: missing,
// unreadable, over 16 MB, not JSON, not an object; TC-15: the opener). Positive twin first: a
// healthy cache reads. Then: no file; a folder at the name; a file of 16 MB and one byte (one of
// exactly 16 MB still reads, so the bound is exact); and, for the decoder, an empty file, a
// truncated object, NaN, a byte-order mark, a top level that is a list, a string, a number or
// null, and an object with more after it.
func TestACacheThatCannotBeReadIsAnError(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	writeCache(t, p, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)})
	if s := readStore(t, p, store); len(s.Folders) != 1 {
		t.Fatalf("the positive twin read %+v", s)
	}

	t.Run("no file", func(t *testing.T) {
		p, _ := newProfile(t, "imap.gmail.com")
		cacheRefused(t, p, CacheMissing)
	})
	t.Run("a folder at the name", func(t *testing.T) {
		p, _ := newProfile(t, "imap.gmail.com")
		mkdir(t, filepath.Join(p, CacheFile))
		cacheRefused(t, p, CacheNotRegular)
	})
	t.Run("the 16 MB bound is exact", func(t *testing.T) {
		p, store := newProfile(t, "imap.gmail.com")
		body := cacheJSON(t, pair{filepath.Join(store, "INBOX.msf"), folder(0, 1, 1)})
		pad := strings.Repeat(" ", 16<<20-len(body)) // the design's 16 MB, not the constant under test
		write(t, filepath.Join(p, CacheFile), body+pad)
		if s := readStore(t, p, store); len(s.Folders) != 1 {
			t.Fatalf("a cache of exactly 16 MB read as %+v", s)
		}
		write(t, filepath.Join(p, CacheFile), body+pad+" ")
		cacheRefused(t, p, CacheUnreadable)
	})
	for name, body := range map[string]string{
		"empty":                  "",
		"spaces only":            "  \n",
		"truncated":              `{"/p/ImapMail/h/INBOX.msf": {"flags": 1`,
		"NaN":                    `{"/p/ImapMail/h/INBOX.msf": {"flags": NaN}}`,
		"a byte-order mark":      "\xef\xbb\xbf{}",
		"a list":                 `[{"flags": 1}]`,
		"an empty list":          `[]`,
		"a string":               `"folderCache"`,
		"a number":               `5`,
		"null":                   `null`,
		"an object and more":     `{} {}`,
		"an object and garbage":  `{}x`,
		"a key that is a number": `{5: {}}`,
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := newProfile(t, "imap.gmail.com")
			write(t, filepath.Join(p, CacheFile), body)
			cacheRefused(t, p, CacheMalformed)
		})
	}
}

// covers: MA-1171
// No key of the cache belongs to the store (design §2.3's "has no key under this account's
// folder"): the store reads as NoKeys with no folders, never as a store with nothing missing.
// Positive twin first: the other store's key is that store's.
func TestAStoreWithNoKeyHasNoFolders(t *testing.T) {
	p, store := newProfile(t, "imap.gmail.com")
	other := filepath.Join(p, "ImapMail", "imap.gmail.com-1")
	writeCache(t, p, pair{filepath.Join(other, "INBOX.msf"), folder(0, 1, 1)})
	if s := readStore(t, p, other); s.Mode != Exact || len(s.Folders) != 1 {
		t.Fatalf("the positive twin: %+v", s)
	}
	s := readStore(t, p, store)
	if s.Mode != NoKeys || len(s.Folders) != 0 || s.Root != nil {
		t.Fatalf("got %+v; want no keys", s)
	}
	writeCache(t, p)
	if s := readStore(t, p, store); s.Mode != NoKeys {
		t.Fatalf("an empty cache gave mode %v", s.Mode)
	}
}

// covers: MA-1171
// Each field is validated as Thunderbird writes it (design §4.1 step 3, TC-2, the §2.1 fold's
// types): flags an unsigned 32-bit integer; totalMsgs, pendingMsgs, serverTotal and
// lastSyncTimeInSec signed 32-bit; expungedBytes and folderSize signed 64-bit; every one a JSON
// integer (1.5, 1e3 and 1e308 are not; -0 is 0). A field that is a string, true, null, an object
// or a list, out of its range, named twice in one entry, or negative off the account root is
// Broken; one not there is Missing. Positive twin first: the healthy entry is valid.
func TestEachFieldIsValidated(t *testing.T) {
	const store = "/p/ImapMail/imap.gmail.com"
	read := func(t *testing.T, value string) Entry {
		t.Helper()
		s := parseStore(t, `{"`+store+`/INBOX.msf": `+value+`}`, store, "linux")
		if len(s.Folders) != 1 {
			t.Fatalf("got %+v; want the one folder", s)
		}
		return s.Folders[0]
	}
	healthy := valueJSON(t, folder(0, 5, 5))
	if e := read(t, healthy); !e.Valid() || !e.FlagsKnown() {
		t.Fatalf("the positive twin is not valid: %+v", e)
	}
	field := func(e Entry, name string) Field {
		return map[string]Field{"flags": e.Flags, "totalMsgs": e.TotalMsgs, "pendingMsgs": e.PendingMsgs,
			"serverTotal": e.ServerTotal, "expungedBytes": e.ExpungedBytes, "folderSize": e.FolderSize,
			"lastSyncTimeInSec": e.LastSyncTimeInSec}[name]
	}
	set := func(name, raw string) string {
		return valueJSON(t, folder(0, 5, 5).with(name, json.RawMessage(raw)))
	}
	for _, tc := range []struct {
		name, raw string
		want      State
		value     int64
	}{
		{"flags", `4294967295`, OK, 4294967295},
		{"flags", `4294967296`, Broken, 0},
		{"flags", `-1`, Broken, 0},
		{"totalMsgs", `2147483647`, OK, 2147483647},
		{"totalMsgs", `2147483648`, Broken, 0},
		{"totalMsgs", `-0`, OK, 0},
		{"totalMsgs", `-1`, Broken, 0},
		{"totalMsgs", `"5"`, Broken, 0},
		{"totalMsgs", `5.0`, Broken, 0},
		{"totalMsgs", `1.5`, Broken, 0},
		{"totalMsgs", `1e3`, Broken, 0},
		{"totalMsgs", `1e308`, Broken, 0},
		{"totalMsgs", `99999999999999999999`, Broken, 0},
		{"totalMsgs", `true`, Broken, 0},
		{"totalMsgs", `null`, Broken, 0},
		{"totalMsgs", `{}`, Broken, 0},
		{"totalMsgs", `[5]`, Broken, 0},
		{"pendingMsgs", `-3`, Broken, 0},
		{"serverTotal", `-2147483649`, Broken, 0},
		{"expungedBytes", `9223372036854775807`, OK, 9223372036854775807},
		{"expungedBytes", `9223372036854775808`, Broken, 0},
		{"expungedBytes", `-1`, Broken, 0},
		{"folderSize", `-5`, Broken, 0},
		{"lastSyncTimeInSec", `2147483648`, Broken, 0},
		{"lastSyncTimeInSec", `-2147483648`, OK, -2147483648}, // the year-2038 wrap: "never checked", for C1b
		{"lastSyncTimeInSec", `0`, OK, 0},
	} {
		e := read(t, set(tc.name, tc.raw))
		if got := field(e, tc.name); got.State != tc.want || got.State == OK && got.Value != tc.value || got.State == Broken && got.Why == "" {
			t.Errorf("%s = %s read as %+v; want %v %d (a Broken field says why)", tc.name, tc.raw, got, tc.want, tc.value)
		}
		if tc.want == Broken && e.Valid() {
			t.Errorf("%s = %s: the entry is still valid", tc.name, tc.raw)
		}
	}

	t.Run("a field named twice is broken", func(t *testing.T) {
		e := read(t, `{"flags": 134225920, "totalMsgs": 5, "totalMsgs": 0, "expungedBytes": 0, "serverTotal": 5}`)
		if e.TotalMsgs.State != Broken || e.Valid() {
			t.Fatalf("totalMsgs named twice read as %+v", e.TotalMsgs)
		}
	})
	t.Run("required: flags, totalMsgs, expungedBytes; serverTotal for an IMAP folder", func(t *testing.T) {
		for _, name := range []string{"flags", "totalMsgs", "expungedBytes", "serverTotal"} {
			e := read(t, valueJSON(t, folder(0, 5, 5).with(name, nil)))
			if field(e, name).State != Missing || e.Valid() {
				t.Errorf("without %s: %+v, valid %v; want Missing and not valid", name, field(e, name), e.Valid())
			}
		}
		notImap := folder(0, 5, 5).with("flags", FlagOffline, "serverTotal", nil)
		if e := read(t, valueJSON(t, notImap)); !e.Valid() {
			t.Errorf("a folder without the ImapBox flag needs no serverTotal: %+v", e)
		}
	})
	t.Run("optional: pendingMsgs, folderSize, lastSyncTimeInSec", func(t *testing.T) {
		e := read(t, valueJSON(t, folder(0, 5, 5).with("pendingMsgs", nil, "folderSize", nil, "lastSyncTimeInSec", nil)))
		if !e.Valid() || e.LastSyncTimeInSec.State != Missing {
			t.Fatalf("without the optional fields: %+v; want valid", e)
		}
		if n, ok := e.Listed(); !ok || n != 5 {
			t.Fatalf("listed %d (%v), want 5", n, ok)
		}
	})
	t.Run("the account root may be negative and is not validated", func(t *testing.T) {
		s := parseStore(t, cacheJSON(t, pair{store, root().with("flags", -1)}, pair{store + "/INBOX.msf", folder(0, 1, 1)}), store, "linux")
		if s.Root == nil || s.Root.TotalMsgs != (Field{OK, -1, ""}) || s.Root.FolderSize != (Field{OK, -1, ""}) || !s.Root.Valid() {
			t.Fatalf("root read as %+v", s.Root)
		}
		if s.Root.Flags.State != Broken {
			t.Fatalf("the root's flags -1 read as %+v; flags is unsigned even on the root", s.Root.Flags)
		}
		if _, ok := s.Root.Listed(); ok {
			t.Fatal("the root gave a listed count")
		}
	})
}

// covers: MA-1171
// A broken flags is never read as "not kept" (foldcheck M3): an entry whose flags is missing, a
// string, out of range or not an integer, with nothing on disk, stays in the store's folders with
// FlagsKnown false and every Has false, so C1b treats it as kept (reason invalid_entry). And a
// value that is not an object at all (TC-15), for a key of the store, is such an entry too: in the
// folders, NotObject, its flags not known. Positive twin first: valid flags are known.
func TestABrokenFlagsIsNeverReadAsNotKept(t *testing.T) {
	const store = "/p/ImapMail/imap.gmail.com"
	s := parseStore(t, cacheJSON(t, pair{store + "/Kept.msf", folder(0, 5, 5)}), store, "linux")
	if e := folderAt(t, s, "Kept"); !e.FlagsKnown() || !e.Has(FlagOffline) {
		t.Fatalf("the positive twin: %+v", e)
	}
	for name, v := range map[string]any{
		"flags missing":      folder(0, 5, 5).with("flags", nil),
		"flags a string":     folder(0, 5, 5).with("flags", "134225920"),
		"flags out of range": folder(0, 5, 5).with("flags", json.RawMessage(`8589934592`)),
		"flags 1.0":          folder(0, 5, 5).with("flags", json.RawMessage(`1.0`)),
		"the value a number": json.RawMessage(`5`),
		"the value a string": "x",
		"the value a list":   json.RawMessage(`[1, 2]`),
		"the value null":     json.RawMessage(`null`),
	} {
		t.Run(name, func(t *testing.T) {
			s := parseStore(t, cacheJSON(t, pair{store + "/Never downloaded.msf", v}, pair{store + "/INBOX.msf", folder(0, 1, 1)}), store, "linux")
			e := folderAt(t, s, "Never downloaded")
			if e.FlagsKnown() || e.Has(FlagOffline) || e.Has(0) || e.Valid() {
				t.Fatalf("read as %+v; want flags not known, no flag, not valid", e)
			}
			if _, isObject := v.(fields); isObject == e.NotObject {
				t.Fatalf("NotObject %v for %s", e.NotObject, name)
			}
		})
	}
	t.Run("a value that is not an object, for another store's key, is not this store's", func(t *testing.T) {
		s := parseStore(t, cacheJSON(t, pair{"/p/ImapMail/other/x.msf", json.RawMessage(`5`)}), store, "linux")
		if s.Mode != NoKeys {
			t.Fatalf("got %+v", s)
		}
	})
}

// covers: MA-1171
// A key that appears twice in the file (TC-15: Go's plain decoding would keep the last one
// silently) is seen: for a key of the store it is listed in DuplicateKeys (C1b: duplicate_key),
// whichever copy is healthy; a duplicate of another store's key is not this store's concern.
// Positive twin: no key twice, none listed.
func TestAKeyThatAppearsTwiceIsSeen(t *testing.T) {
	const store = "/p/ImapMail/imap.gmail.com"
	inbox := store + "/INBOX.msf"
	if s := parseStore(t, cacheJSON(t, pair{inbox, folder(0, 1, 1)}), store, "linux"); len(s.DuplicateKeys) != 0 {
		t.Fatalf("the positive twin listed %q", s.DuplicateKeys)
	}
	for _, rootTwice := range []bool{false, true} {
		key := inbox
		if rootTwice {
			key = store
		}
		s := parseStore(t, cacheJSON(t, pair{key, folder(0, 0, 0)}, pair{inbox, folder(0, 1, 1)}, pair{key, folder(0, 1, 1)}), store, "linux")
		if !equal(s.DuplicateKeys, []string{key}) {
			t.Fatalf("the key %s twice: duplicate keys %q", key, s.DuplicateKeys)
		}
	}
	other := "/p/ImapMail/other/INBOX.msf"
	s := parseStore(t, cacheJSON(t, pair{other, folder(0, 1, 1)}, pair{other, folder(0, 1, 1)}, pair{inbox, folder(0, 1, 1)}), store, "linux")
	if len(s.DuplicateKeys) != 0 || len(s.Folders) != 1 {
		t.Fatalf("another store's duplicate gave %+v", s)
	}
}

// covers: MA-1171
// The Panorama signals (TC-3, lead's call L4): a panorama.sqlite in the profile folder, seen by
// Lstat and never opened, and mail.panorama.enabled as Thunderbird applies it (prefs.js, then
// user.js over it, read through C0c's reader; thunderbird.PanoramaEnabled), each reported on its
// own; a pref that is not true or false, or a settings file MailArchive cannot read with
// certainty, is can't tell. Also the Thunderbird version from compatibility.ini (§6's L5 words).
// Positive twin first: a healthy profile says no, no, 153.
func TestThePanoramaSignalsAndTheVersion(t *testing.T) {
	p, _ := newProfile(t, "imap.gmail.com")
	if got := ReadProfile(p); got != (Profile{PanoramaFile: No, PanoramaPref: No, VersionMajor: 153, VersionKnown: true}) {
		t.Fatalf("the positive twin read %+v", got)
	}
	pref := func(v string) string { return `user_pref("mail.panorama.enabled", ` + v + `);` + "\n" }
	for name, tc := range map[string]struct {
		prefs, user string
		file        bool
		want        Profile
	}{
		"panorama.sqlite alone": {file: true, want: Profile{PanoramaFile: Yes, PanoramaPref: No}},
		"the pref alone":        {prefs: pref("true"), want: Profile{PanoramaFile: No, PanoramaPref: Yes}},
		"the pref false":        {prefs: pref("false"), want: Profile{PanoramaFile: No, PanoramaPref: No}},
		"user.js over prefs.js": {prefs: pref("false"), user: pref("true"), want: Profile{PanoramaFile: No, PanoramaPref: Yes}},
		"user.js false":         {prefs: pref("true"), user: pref("false"), want: Profile{PanoramaFile: No, PanoramaPref: No}},
		"the pref a string":     {prefs: pref(`"true"`), want: Profile{PanoramaFile: No, PanoramaPref: CantTell}},
		"the pref a number":     {user: pref("1"), want: Profile{PanoramaFile: No, PanoramaPref: CantTell}},
		"a wrong type before":   {prefs: pref("1") + pref("false"), want: Profile{PanoramaFile: No, PanoramaPref: CantTell}},
		"a hand-edited line":    {prefs: "/* x */\n", want: Profile{PanoramaFile: No, PanoramaPref: CantTell}},
	} {
		t.Run(name, func(t *testing.T) {
			p, _ := newProfile(t, "imap.gmail.com")
			if tc.prefs != "" {
				write(t, filepath.Join(p, "prefs.js"), prefsFor("imap.gmail.com")+tc.prefs)
			}
			if tc.user != "" {
				write(t, filepath.Join(p, "user.js"), tc.user)
			}
			if tc.file {
				mkdir(t, filepath.Join(p, "panorama.sqlite")) // any kind of entry counts; it is never opened
			}
			tc.want.VersionMajor, tc.want.VersionKnown = 153, true
			if got := ReadProfile(p); got != tc.want {
				t.Fatalf("read %+v, want %+v", got, tc.want)
			}
		})
	}
	t.Run("no prefs.js: can't tell", func(t *testing.T) {
		p, _ := newProfile(t, "imap.gmail.com")
		if err := os.Remove(filepath.Join(p, "prefs.js")); err != nil {
			t.Fatal(err)
		}
		if got := ReadProfile(p); got.PanoramaPref != CantTell {
			t.Fatalf("read %+v", got)
		}
	})
	t.Run("a version that cannot be read", func(t *testing.T) {
		p, _ := newProfile(t, "imap.gmail.com")
		write(t, filepath.Join(p, "compatibility.ini"), "[Compatibility]\nLastVersion=x\n")
		if got := ReadProfile(p); got.VersionKnown || got.VersionMajor != 0 {
			t.Fatalf("read %+v", got)
		}
	})
}
