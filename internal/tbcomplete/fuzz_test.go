package tbcomplete

// The decoder's fuzz test (design §13, C1a's gate: "a fuzz test of the decoder"). Run as an
// ordinary test it replays the seeds; `go test -fuzz=FuzzFolderCache ./internal/tbcomplete/`
// searches further.

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1170, MA-1171, MA-1276
// Whatever bytes folderCache.json holds, Parse either refuses them or gives a cache whose view of a
// store keeps the reader's promises: no panic; every Lstat is made inside the store; a folder is
// never both read and a duplicate, and appears once; every folder has a name, comes from a key of
// the store and is shaped X.sbd/…/Y.msf; a refused key is never also a folder; All Mail, when
// found, is one of the folders and carries 0x8000; and a store whose keys cannot be told
// (CantTellKeys) gives no folder at all.
func FuzzFolderCache(f *testing.F) {
	for _, seed := range []string{
		`{}`, `{"/p/ImapMail/h": {"flags": 12, "totalMsgs": -1}}`,
		`{"/p/ImapMail/h/INBOX.msf": {"flags": 134225920, "totalMsgs": 5, "serverTotal": 5, "expungedBytes": 0}}`,
		`{"/p/ImapMail/h/a.sbd/../../x.msf": {}, "/old/ImapMail/h/b.msf": {"flags": 32768}}`,
		`{"C:\\p\\ImapMail\\h\\INBOX.msf": {"flags": 1.5}, "/p/ImapMail/h/INBOX.msf": 5}`,
		`{"/p/ImapMail/h/x.msf": {"flags": 32768, "flags": 0}, "/p/ImapMail/h/x.msf": []}`,
		`{"/p/ImapMail/h/\u0000.msf": {}, "/p/ImapMail/h//y.msf": {}, "/p/ImapMail/h/C:z.msf": {}}`,
		`[1]`, `{"a": {"b": {"c": [[[[]]]]}}}`, "\xef\xbb\xbf{}",
		`{"x":` + strings.Repeat("[", 255) + strings.Repeat("]", 255) + `}`,
		`{"/old/ImapMail/h/a.msf": {"totalMsgs": -1, "folderSize": -1}, "/older/ImapMail/h/b.msf": {}}`,
		`{"\\\\p\\ImapMail\\h\\a.msf": {}, "\\p\\ImapMail\\h\\b.msf": {}, "/P/IMAPMAIL/h/c.msf": {}}`,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		c, err := Parse(data)
		if err != nil {
			if c != nil {
				t.Fatal("Parse gave a cache with an error")
			}
			return
		}
		for _, tc := range []struct{ store, goos string }{
			{"/p/ImapMail/h", "linux"}, {"/p/ImapMail/h", "darwin"}, {`C:\p\ImapMail\h`, "windows"},
		} {
			log := recordLstat(t)
			s := c.storeAt([]string{tc.store}, tc.goos)
			for _, p := range log.all() {
				if !strings.HasPrefix(p, filepath.Clean(tc.store)+string(filepath.Separator)) {
					t.Fatalf("%s: Lstat outside the store: %q", tc.goos, p)
				}
			}
			seen := map[string]bool{}
			for _, d := range s.Duplicates {
				seen["dup:"+d] = true
			}
			refused := map[string]bool{}
			for _, r := range s.Refused {
				refused[r.Key] = true
			}
			for _, e := range append(append([]Entry(nil), s.Folders...), s.OldPathOnly...) {
				if e.Path == "" || seen[e.Path] || seen["dup:"+e.Path] || refused[e.Key] {
					t.Fatalf("%s: folder %q (key %q) empty, twice, a duplicate or refused: %+v", tc.goos, e.Path, e.Key, s)
				}
				seen[e.Path] = true
			}
			if s.AllMail.Kind == AllMailOne && (s.AllMail.Entry == nil || !s.AllMail.Entry.Has(FlagAllMail) || !seen[s.AllMail.Entry.Path]) {
				t.Fatalf("%s: All Mail %+v is not a folder with 0x8000", tc.goos, s.AllMail)
			}
			if s.AllMail.Kind != AllMailOne && s.AllMail.Entry != nil {
				t.Fatalf("%s: All Mail %+v names a folder", tc.goos, s.AllMail)
			}
			if s.Mode == CantTellKeys && (len(s.Folders)+len(s.OldPathOnly)+len(s.Duplicates) != 0 || s.Root != nil || s.AllMail.Kind != AllMailCantTell) {
				t.Fatalf("%s: can't tell, yet %+v", tc.goos, s)
			}
		}
	})
}
