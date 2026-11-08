package app

// Test helpers for slice C2 (the completeness check wired into the backup run): real temp
// Thunderbird profiles (prefs.js and compatibility.ini as Thunderbird writes them), stores
// with mbox files and .msf summaries, and a folderCache.json naming each folder at the store's
// path as given, as Thunderbird writes its keys.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/tbcomplete"
)

// tbAcct is one live account of a test profile's prefs.js.
type tbAcct struct {
	n      int    // its number: accountN, serverN
	host   string // hostname
	typ    string // imap, none, pop3, ews…
	dirRel string // directory-rel after [ProfD] ("" for none)
	dir    string // directory ("" for none)
	extra  map[string]string
}

// tbPrefsJS is a prefs.js holding the accounts, every line a shape Thunderbird writes.
func tbPrefsJS(accts ...tbAcct) string {
	var b strings.Builder
	b.WriteString("// Mozilla User Preferences\n\n")
	var ids []string
	line := func(name, value string) { fmt.Fprintf(&b, "user_pref(%q, %s);\n", name, value) }
	str := func(s string) string { return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"` }
	for _, a := range accts {
		id, key := fmt.Sprintf("account%d", a.n), fmt.Sprintf("server%d", a.n)
		ids = append(ids, id)
		line("mail.account."+id+".server", str(key))
		if a.dirRel != "" {
			line("mail.server."+key+".directory-rel", str("[ProfD]"+a.dirRel))
		}
		if a.dir != "" {
			line("mail.server."+key+".directory", str(a.dir))
		}
		line("mail.server."+key+".hostname", str(a.host))
		line("mail.server."+key+".type", str(a.typ))
		for k, v := range a.extra {
			line("mail.server."+key+"."+k, v)
		}
	}
	line("mail.accountmanager.accounts", str(strings.Join(ids, ",")))
	return b.String()
}

// newTBProfile makes a Thunderbird profile in a temp folder: prefs.js holding accts and a
// compatibility.ini of Thunderbird 153.6.0esr.
func newTBProfile(t *testing.T, accts ...tbAcct) string {
	t.Helper()
	p := filepath.Join(tmpDir(t), "abcd1234.default-release")
	tbWrite(t, filepath.Join(p, "prefs.js"), tbPrefsJS(accts...))
	tbWrite(t, filepath.Join(p, "compatibility.ini"), "[Compatibility]\nLastVersion=153.6.0_20251212000000/20251212000000\nLastOSABI=Darwin_aarch64-gcc3\n")
	return p
}

func tbWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// tbMsg is one message as Thunderbird keeps it offline; status "0009" marks a deleted copy not
// yet compacted.
func tbMsg(id, status string) string {
	h := "From: Alice <alice@example.com>\nTo: Bob <bob@example.com>\nSubject: message " + id +
		"\nDate: Mon, 03 Mar 2025 09:00:00 +0000\n"
	if id != "" {
		h += "Message-ID: <" + id + "@c2.example>\n"
	}
	if status == "" {
		status = "0001"
	}
	return h + "X-Mozilla-Status: " + status + "\nX-Mozilla-Status2: 00000000\nContent-Type: text/plain\n\nbody of " + id + "\n"
}

// tbMsgs is n messages with ids prefix-1 … prefix-n.
func tbMsgs(prefix string, n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, tbMsg(fmt.Sprintf("%s-%d", prefix, i), ""))
	}
	return out
}

// tbFolder is one folder of a test store: its Path ("[Gmail]/All Mail"), flags beyond ImapBox
// and Offline, whether it is kept, what Thunderbird lists for it, and the messages of its
// mailbox file (nil: no mailbox file, as for a folder not downloaded).
type tbFolder struct {
	path    string
	flags   uint32
	notKept bool
	listed  int
	msgs    []string
	edit    func(map[string]any) // changes the cache entry, after it is built
}

// tbEntry is the folderCache.json value Thunderbird writes for an IMAP folder, checked an hour
// ago.
func tbEntry(f tbFolder, checked time.Time) map[string]any {
	flags := f.flags | tbcomplete.FlagImapBox
	if !f.notKept {
		flags |= tbcomplete.FlagOffline
	}
	e := map[string]any{
		"flags": flags, "totalMsgs": f.listed, "totalUnreadMsgs": 0, "pendingUnreadMsgs": 0,
		"pendingMsgs": 0, "expungedBytes": 0, "folderSize": f.listed * 1000, "boxFlags": 0, "hierDelim": 47,
		"onlineName": "x", "aclFlags": 0, "serverTotal": f.listed, "serverUnseen": 0, "serverRecent": 0,
		"nextUID": f.listed + 1, "lastSyncTimeInSec": checked.Unix(),
	}
	if f.edit != nil {
		f.edit(e)
	}
	return e
}

// tbStore writes the store folder dir: each folder's .msf and (when it has messages) its mbox
// file, its containers named .sbd. It returns the cache entries naming the folders and the store's
// root at dir as given.
func tbStoreAt(t *testing.T, dir string, folders ...tbFolder) map[string]any {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	checked := time.Now().Add(-time.Hour)
	entries := map[string]any{dir: map[string]any{"flags": 0x0c, "totalMsgs": -1, "totalUnreadMsgs": -1, "pendingUnreadMsgs": 0, "pendingMsgs": 0, "expungedBytes": 0, "folderSize": -1}}
	for _, f := range folders {
		parts := strings.Split(f.path, "/")
		for i := range parts[:len(parts)-1] {
			parts[i] += ".sbd"
		}
		mailbox := filepath.Join(append([]string{dir}, parts...)...)
		tbWrite(t, mailbox+".msf", "")
		if f.msgs != nil {
			var b strings.Builder
			for _, m := range f.msgs {
				b.WriteString("From - Mon Mar  3 09:00:00 2025\n")
				b.WriteString(m)
				b.WriteString("\n")
			}
			tbWrite(t, mailbox, b.String())
		}
		entries[mailbox+".msf"] = tbEntry(f, checked)
	}
	return entries
}

// writeTBCache writes profile/folderCache.json holding every entry of every store.
func writeTBCache(t *testing.T, profile string, stores ...map[string]any) {
	t.Helper()
	all := map[string]any{}
	for _, s := range stores {
		for k, v := range s {
			all[k] = v
		}
	}
	data, err := json.Marshal(all)
	if err != nil {
		t.Fatal(err)
	}
	tbWrite(t, filepath.Join(profile, tbcomplete.CacheFile), string(data))
}

// imapProfile is a profile with one live IMAP account on host whose store holds folders, its
// folder cache written. It returns the profile and the store.
func imapProfile(t *testing.T, host string, extra map[string]string, folders ...tbFolder) (string, string) {
	t.Helper()
	p := newTBProfile(t, tbAcct{n: 1, host: host, typ: "imap", dirRel: "ImapMail/" + host, extra: extra})
	store := filepath.Join(p, "ImapMail", host)
	writeTBCache(t, p, tbStoreAt(t, store, folders...))
	return p, store
}

// countCacheReads counts the folder-cache reads, through the real reader, and calls at (if set)
// at each one.
func countCacheReads(t *testing.T, at func(profile string)) *int {
	t.Helper()
	n := 0
	prev := readTBCache
	readTBCache = func(profile string) (*tbcomplete.Cache, error) {
		n++
		if at != nil {
			at(profile)
		}
		return prev(profile)
	}
	t.Cleanup(func() { readTBCache = prev })
	return &n
}

// lastRunRaw is the archive's last-run record as JSON.
func lastRunRaw(t *testing.T, out string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, ".mailarchive-lastrun.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// onlyResult is the run's one completeness result, failing the test unless there is exactly one.
func onlyResult(t *testing.T, r Result) tbcomplete.Result {
	t.Helper()
	if len(r.Thunderbird) != 1 {
		t.Fatalf("the run gave %d completeness results, want 1: %+v", len(r.Thunderbird), r.Thunderbird)
	}
	return r.Thunderbird[0]
}
