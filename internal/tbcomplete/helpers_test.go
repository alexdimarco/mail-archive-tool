package tbcomplete

// Test helpers for slice C1a: real temp Thunderbird profiles (prefs.js, compatibility.ini,
// ImapMail/<host>/ with .msf and mailbox files) and folderCache.json files written key by key,
// in order, so a duplicated key can be written as Thunderbird never would.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
)

// prefsFor is a minimal prefs.js as Thunderbird writes it, with one live IMAP account whose
// store is ImapMail/<host>.
func prefsFor(host string) string {
	return "// Mozilla User Preferences\n\n" +
		`user_pref("mail.account.account1.server", "server1");` + "\n" +
		`user_pref("mail.accountmanager.accounts", "account1");` + "\n" +
		`user_pref("mail.server.server1.directory-rel", "[ProfD]ImapMail/` + host + `");` + "\n" +
		`user_pref("mail.server.server1.hostname", "` + host + `");` + "\n" +
		`user_pref("mail.server.server1.type", "imap");` + "\n"
}

// newProfile makes a Thunderbird profile in a temp folder: prefs.js, a compatibility.ini of
// Thunderbird 153.6.0esr and an empty store ImapMail/<host>. It returns the profile and store.
func newProfile(t *testing.T, host string) (string, string) {
	t.Helper()
	p := t.TempDir()
	store := filepath.Join(p, "ImapMail", host)
	mkdir(t, store)
	write(t, filepath.Join(p, "prefs.js"), prefsFor(host))
	write(t, filepath.Join(p, "compatibility.ini"), "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n")
	return p, store
}

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, body string) {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// fields is one folder-cache value. Numbers are written as JSON numbers; a json.RawMessage is
// written as it is (a string, 1.5, null, …).
type fields map[string]any

// folder is the value Thunderbird writes for an IMAP folder that is kept on this computer and
// checked: the fields of nsMsgDBFolder::WriteToFolderCacheElem and nsImapMailFolder's (ESR 153,
// the source re-read's claims 1a and 1b), with flags plus the ImapBox and Offline flags.
func folder(flags uint32, total, server int) fields {
	return fields{
		"flags": flags | FlagImapBox | FlagOffline, "totalMsgs": total, "totalUnreadMsgs": 0,
		"pendingUnreadMsgs": 0, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": total * 1000,
		"boxFlags": 0, "hierDelim": 47, "onlineName": "x", "aclFlags": 0, "serverTotal": server,
		"serverUnseen": 0, "serverRecent": 0, "nextUID": total + 1, "lastSyncTimeInSec": 1762500000,
	}
}

// root is the value Thunderbird writes for an account's own top level: its counts start at -1
// (nsMsgDBFolder.cpp, mNumTotalMessages(-1)).
func root() fields {
	return fields{"flags": 0x0c, "totalMsgs": -1, "totalUnreadMsgs": -1, "pendingUnreadMsgs": 0,
		"pendingMsgs": 0, "expungedBytes": 0, "folderSize": -1}
}

// with is f with the given fields changed; a nil value removes the field.
func (f fields) with(kv ...any) fields {
	out := fields{}
	for k, v := range f {
		out[k] = v
	}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] == nil {
			delete(out, kv[i].(string))
			continue
		}
		out[kv[i].(string)] = kv[i+1]
	}
	return out
}

// pair is one key and its value; val is fields, a json.RawMessage, or anything json.Marshal takes.
type pair struct {
	key string
	val any
}

// cacheJSON is a folderCache.json holding pairs in order (so a key may appear twice).
func cacheJSON(t *testing.T, pairs ...pair) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("{")
	for i, p := range pairs {
		if i > 0 {
			b.WriteString(",")
		}
		k, err := json.Marshal(p.key)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(k)
		b.WriteString(":")
		b.WriteString(valueJSON(t, p.val))
	}
	b.WriteString("}")
	return b.String()
}

func valueJSON(t *testing.T, v any) string {
	t.Helper()
	if raw, ok := v.(json.RawMessage); ok {
		return string(raw)
	}
	if f, ok := v.(fields); ok {
		keys := make([]string, 0, len(f))
		for k := range f {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var b strings.Builder
		b.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				b.WriteString(",")
			}
			kj, _ := json.Marshal(k)
			b.Write(kj)
			b.WriteString(":")
			b.WriteString(valueJSON(t, f[k]))
		}
		b.WriteString("}")
		return b.String()
	}
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// writeCache writes profile/folderCache.json holding pairs in order.
func writeCache(t *testing.T, profile string, pairs ...pair) {
	t.Helper()
	write(t, filepath.Join(profile, CacheFile), cacheJSON(t, pairs...))
}

// readStore reads profile's folder cache and gives store's view of it, failing the test when the
// cache does not read.
func readStore(t *testing.T, profile, store string) Store {
	t.Helper()
	c, err := ReadCache(profile)
	if err != nil {
		t.Fatalf("ReadCache: %v", err)
	}
	s, err := c.Store(store)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	return s
}

// parseStore parses data and gives the view of the store at storePath by goos's path rules,
// for key shapes of another OS.
func parseStore(t *testing.T, data, storePath, goos string) Store {
	t.Helper()
	c, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return c.storeAt([]string{storePath}, goos)
}

// paths is each folder's Path, sorted.
func paths(es []Entry) []string {
	out := make([]string, 0, len(es))
	for _, e := range es {
		out = append(out, e.Path)
	}
	sort.Strings(out)
	return out
}

// folderAt is the folder of s whose Path is p.
func folderAt(t *testing.T, s Store, p string) Entry {
	t.Helper()
	for _, e := range s.Folders {
		if e.Path == p {
			return e
		}
	}
	t.Fatalf("no folder %q among %q", p, paths(s.Folders))
	return Entry{}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// lstatLog records every path the reader checks with Lstat, through the real os.Lstat (the
// seam observes; it changes nothing the reader sees).
type lstatLog struct {
	mu    sync.Mutex
	paths []string
}

func recordLstat(t *testing.T) *lstatLog {
	t.Helper()
	l := &lstatLog{}
	prev := lstat
	lstat = func(p string) (os.FileInfo, error) {
		l.mu.Lock()
		l.paths = append(l.paths, p)
		l.mu.Unlock()
		return os.Lstat(p)
	}
	t.Cleanup(func() { lstat = prev })
	return l
}

func (l *lstatLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.paths...)
}

// outside is every recorded path that is not inside dir.
func (l *lstatLog) outside(dir string) []string {
	var out []string
	for _, p := range l.all() {
		if !strings.HasPrefix(p, dir+string(filepath.Separator)) {
			out = append(out, p)
		}
	}
	return out
}
