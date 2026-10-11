package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-531, R9, R29, S52
// A manifest handed over in another archive folder is DATA (E4-AO1): LoadFrom reads the store ids in
// it as text and never goes to the file system with one. An unprefixed id is attacker-chosen: resolved
// with filepath.EvalSymlinks it makes the machine Lstat that path (on Windows a UNC id such as
// \\host\share\x opens an SMB connection and offers the owner's credentials), and a symlink inside
// the folder is followed out of it. The snapshot keeps the id as the lexical "path:" form, and Load,
// the owner's own manifest, still resolves it (the positive twin).
func TestLoadFromNeverResolvesAStoreIDOnTheFileSystem(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "elsewhere")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link-to-elsewhere")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	hostile := []string{link, `\\attacker.example\share\x`, "../../etc/passwd"}
	stores := map[string]string{"mailbox:bob@example.org": "bob"}
	for i, id := range hostile {
		stores[id] = "tok" + string(rune('a'+i))
	}
	doc, err := json.Marshal(map[string]any{"version": 4, "entries": map[string]any{}, "stores": stores})
	if err != nil {
		t.Fatal(err)
	}

	snap, err := LoadFrom("the manifest", doc)
	if err != nil {
		t.Fatalf("LoadFrom: %v", err)
	}
	fold := func(s string) string {
		if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
			return strings.ToLower(s)
		}
		return s
	}
	if tok := snap.Stores["mailbox:bob@example.org"]; tok != "bob" {
		t.Errorf("the mailbox store's id was changed: %v", snap.Stores)
	}
	want := "path:" + fold(filepath.Clean(link))
	if tok, ok := snap.Stores[want]; !ok || tok != "toka" {
		t.Errorf("the id %q was not kept as its lexical form %q: %v (an id read from a manifest is never resolved on the file system)", link, want, snap.Stores)
	}
	for id := range snap.Stores {
		if strings.Contains(id, "elsewhere") && !strings.Contains(id, "link-to-elsewhere") {
			t.Errorf("the id %q is the symlink's target: the snapshot followed a link named by the manifest", id)
		}
		if cwd, _ := os.Getwd(); cwd != "" && strings.HasPrefix(id, "path:"+fold(cwd)) && !strings.HasPrefix(id, "path:"+fold(dir)) {
			t.Errorf("the id %q was made absolute against the working directory", id)
		}
	}

	// the positive twin: the owner's own manifest, read with Load, still canonicalizes its ids
	path := filepath.Join(dir, ".mailarchive-manifest.json")
	if err := os.WriteFile(path, doc, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	resolved, rerr := filepath.EvalSymlinks(link)
	if rerr != nil {
		t.Fatal(rerr)
	}
	if tok, ok := m.Stores["path:"+fold(resolved)]; !ok || tok != "toka" {
		t.Errorf("Load no longer resolves the owner's own store ids: %v", m.Stores)
	}
}
