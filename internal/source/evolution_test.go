package source

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/model"
)

// writeMaildirMsg writes a minimal RFC 5322 message into <dir>/cur so the dir
// becomes a readable maildir folder.
func writeMaildirMsg(t *testing.T, dir, subject string) {
	t.Helper()
	cur := filepath.Join(dir, "cur")
	if err := os.MkdirAll(cur, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: A <a@example.com>\r\nTo: B <b@example.com>\r\n" +
		"Subject: " + subject + "\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n" +
		"Message-ID: <" + subject + "@x>\r\n\r\nbody of " + subject + "\r\n"
	if err := os.WriteFile(filepath.Join(cur, subject+":2,S"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// walkAll opens src and collects (folderPath, subject) for every message.
func walkAll(t *testing.T, dir string) map[string][]string {
	t.Helper()
	src, err := Open(dir)
	if err != nil {
		t.Fatalf("open %s: %v", dir, err)
	}
	defer src.Close()
	got := map[string][]string{} // subject → folder path
	if err := src.Walk(func(fp []string, m *model.Message) error {
		got[m.Subject] = fp
		return nil
	}); err != nil {
		t.Fatalf("walk: %v", err)
	}
	return got
}

// covers: MA-52, R15, R6, S15
// A Maildir++ subfolder directory name decodes to a sanitized folder path: '.'
// separates components, _XX escapes are unescaped, empty components are dropped,
// and a decoded separator cannot escape the output root.
func TestDecodeMaildirName(t *testing.T) {
	cases := []struct {
		name string
		want []string
	}{
		{".Drafts", []string{"Drafts"}},
		{".dimarcotech email", []string{"dimarcotech email"}},
		{".ischool_2Eutoronto_2Eca.INBOX.Tickets", []string{"ischool.utoronto.ca", "INBOX", "Tickets"}},
		{".Utoronto.Archive.older-than-2017", []string{"Utoronto", "Archive", "older-than-2017"}},
		{".Utoronto.", []string{"Utoronto"}}, // trailing empty component dropped
	}
	for _, c := range cases {
		got := decodeMaildirName(c.name)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("decodeMaildirName(%q) = %v, want %v", c.name, got, c.want)
		}
	}

	// A decoded path separator ("_2F") must be neutralized, never a traversal.
	traversal := decodeMaildirName("._2E_2E_2Fescape")
	for _, seg := range traversal {
		if strings.ContainsAny(seg, `/\`) || seg == ".." {
			t.Errorf("decoded segment escapes the root: %q", seg)
		}
	}
	assure.Reached(t, traversal, "decoded traversal-attempt segments")
}

// covers: MA-53, R15, R1, S15
// The Maildir++ reader reads the root INBOX and every dot-encoded subfolder —
// including nested ones — so no folder is silently missed.
func TestEvolutionMaildirPlusPlusReader(t *testing.T) {
	store := t.TempDir()
	// Root INBOX + the store marker.
	writeMaildirMsg(t, store, "root-inbox")
	if err := os.WriteFile(filepath.Join(store, maildirPlusPlusMarker), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A sidecar that must be ignored, not read as a folder.
	if err := os.WriteFile(filepath.Join(store, ".Work.cmeta"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMaildirMsg(t, filepath.Join(store, ".Work"), "work-msg")
	writeMaildirMsg(t, filepath.Join(store, ".Work.Projects"), "nested-msg")
	writeMaildirMsg(t, filepath.Join(store, ".ischool_2Eutoronto_2Eca.INBOX"), "imap-msg")

	if !isEvolutionMaildirStore(store) {
		t.Fatal("store with ..maildir++ marker not detected as Maildir++")
	}

	got := walkAll(t, store)
	assure.Reached(t, got, "messages read from the Maildir++ store")
	if len(got) != 4 {
		t.Fatalf("read %d messages, want 4 (a folder was missed): %v", len(got), keys(got))
	}
	want := map[string][]string{
		"root-inbox": nil,
		"work-msg":   {"Work"},
		"nested-msg": {"Work", "Projects"},
		"imap-msg":   {"ischool.utoronto.ca", "INBOX"},
	}
	for subj, wantFP := range want {
		if fp, ok := got[subj]; !ok {
			t.Errorf("message %q missing", subj)
		} else if !reflect.DeepEqual(fp, wantFP) {
			t.Errorf("%q folder = %v, want %v", subj, fp, wantFP)
		}
	}
}

// covers: MA-54, R15, R1, S16
// The cache reader walks folders/<f>/{cur,new} for every folder, including
// subfolders nested directly and under a "subfolders" container.
func TestEvolutionCacheReader(t *testing.T) {
	acct := t.TempDir()
	folders := filepath.Join(acct, "folders")
	writeMaildirMsg(t, filepath.Join(folders, "INBOX"), "inbox-a")
	// A second message in the same folder's new/ dir.
	newDir := filepath.Join(folders, "INBOX", "new")
	if err := os.MkdirAll(newDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inboxB := "From: a@example.com\r\nSubject: inbox-b\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <inbox-b@x>\r\n\r\nb\r\n"
	if err := os.WriteFile(filepath.Join(newDir, "inbox-b"), []byte(inboxB), 0o644); err != nil {
		t.Fatal(err)
	}
	// Evolution shards messages into two-hex-char buckets under cur/ — a real
	// message must be read from cur/NN/<msg>, not just cur/<msg>.
	shardDir := filepath.Join(folders, "INBOX", "cur", "16")
	if err := os.MkdirAll(shardDir, 0o755); err != nil {
		t.Fatal(err)
	}
	inboxSharded := "From: a@example.com\r\nSubject: inbox-sharded\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <inbox-sharded@x>\r\n\r\nc\r\n"
	if err := os.WriteFile(filepath.Join(shardDir, "3437"), []byte(inboxSharded), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMaildirMsg(t, filepath.Join(folders, "Archive"), "arch-a")
	writeMaildirMsg(t, filepath.Join(folders, "Archive", "2021"), "arch-nested")        // direct nesting
	writeMaildirMsg(t, filepath.Join(folders, "Gmail", "subfolders", "All"), "gmail-a") // subfolders container
	// Account-name resolution has no .source here; store name falls back cleanly.
	if err := os.WriteFile(filepath.Join(acct, ".ev-store-summary"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if !isEvolutionCacheStore(acct) {
		t.Fatal("cache account with folders/ not detected")
	}

	got := walkAll(t, acct)
	assure.Reached(t, got, "messages read from the cache store")
	want := map[string][]string{
		"inbox-a":       {"INBOX"},
		"inbox-b":       {"INBOX"},
		"inbox-sharded": {"INBOX"}, // read from cur/16/3437
		"arch-a":        {"Archive"},
		"arch-nested":   {"Archive", "2021"},
		"gmail-a":       {"Gmail", "All"},
	}
	if len(got) != len(want) {
		t.Fatalf("read %d messages, want %d: %v", len(got), len(want), keys(got))
	}
	for subj, wantFP := range want {
		if fp, ok := got[subj]; !ok {
			t.Errorf("message %q missing", subj)
		} else if !reflect.DeepEqual(fp, wantFP) {
			t.Errorf("%q folder = %v, want %v", subj, fp, wantFP)
		}
	}
}

// covers: MA-55, R15, R6, S16
// Store detection is exclusive and routes correctly: a Maildir++ store, a cache
// store, and a plain directory are each classified once, and both Evolution
// layouts count as a single mail store (so discovery does not expand them).
func TestEvolutionDetection(t *testing.T) {
	// Maildir++ store.
	mpp := t.TempDir()
	writeMaildirMsg(t, mpp, "x")
	if err := os.WriteFile(filepath.Join(mpp, maildirPlusPlusMarker), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Cache store.
	cache := t.TempDir()
	writeMaildirMsg(t, filepath.Join(cache, "folders", "INBOX"), "y")
	// A plain, empty directory is neither.
	plain := t.TempDir()

	if !isEvolutionMaildirStore(mpp) || isEvolutionCacheStore(mpp) {
		t.Error("Maildir++ store misclassified")
	}
	if !isEvolutionCacheStore(cache) || isEvolutionMaildirStore(cache) {
		t.Error("cache store misclassified")
	}
	if isEvolutionMaildirStore(plain) || isEvolutionCacheStore(plain) {
		t.Error("plain directory misdetected as an Evolution store")
	}

	// Both are single mail stores (discovery must not expand them into files).
	if !IsMailStoreDir(mpp) || !IsMailStoreDir(cache) {
		t.Error("Evolution stores must be treated as a single mail store")
	}

	// Open routes both to the Evolution reader (store name resolves without panic).
	for _, dir := range []string{mpp, cache} {
		src, err := Open(dir)
		if err != nil {
			t.Fatalf("open %s: %v", dir, err)
		}
		if _, ok := src.(*evolutionReader); !ok {
			t.Errorf("%s did not open as an evolutionReader (got %T)", dir, src)
		}
		src.Close()
	}
}

func keys(m map[string][]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
