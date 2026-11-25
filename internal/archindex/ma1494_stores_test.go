package archindex

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1494
// archindex.Stores (docs/design-per-mailbox-archives.md §7, C3): one of three answers for a folder,
// from the bounded walk recognition does. Positive twin first: an archive gives its store ids, read
// as a snapshot reads them (an unsafe token's store dropped, each id once, sorted), and the folder
// it opened. An encrypted archive is an archive with no ids (its stores are sealed). Not an archive:
// an empty folder or one not there (no manifest at all), and a manifest this build does not read or
// a link at the manifest's name (a manifest, not read). Cannot read now: a manifest read that fails
// part way, a folder this user cannot open. The ids are read lexically: a planted id that names a
// link is kept as that link's path, never where it leads, and a UNC-shaped id opens nothing.
func TestStoresGivesAnArchivesStoreIdsOrSaysWhyNot(t *testing.T) {
	dir := t.TempDir()
	arch := filepath.Join(dir, "qzsam@x.example")
	writeArchive(t, arch, map[string]string{
		"path:/mail/ImapMail/imap.b.example": "b",
		"path:/mail/ImapMail/imap.a.example": "a",
		"path:/mail/Mail/Local Folders":      "a2",
		"path:/mail/planted":                 "..",
	})
	got := Stores(arch)
	want := []string{"path:/mail/ImapMail/imap.a.example", "path:/mail/ImapMail/imap.b.example", "path:/mail/Mail/Local Folders"}
	if got.Answer != StoresArchive || !reflect.DeepEqual(got.IDs, want) || got.Encrypted || got.NoManifest {
		t.Fatalf("positive twin: an archive's ids: %+v", got)
	}
	fi, err := os.Lstat(arch)
	if err != nil || got.Dir == nil || !os.SameFile(fi, got.Dir) {
		t.Errorf("the folder it opened: %v %v", got.Dir, err)
	}
	// The same folder again: the kept verdict gives the same ids.
	if again := Stores(arch); !reflect.DeepEqual(again.IDs, want) {
		t.Errorf("read again: %+v", again)
	}

	enc := filepath.Join(dir, "locked")
	if err := os.MkdirAll(enc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(enc, ".mailarchive-vault.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Stores(enc); got.Answer != StoresArchive || !got.Encrypted || got.IDs != nil {
		t.Errorf("an encrypted archive: %+v", got)
	}

	empty := filepath.Join(dir, "empty")
	if err := os.MkdirAll(empty, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, folder := range map[string]string{"an empty folder": empty, "a folder not there": filepath.Join(dir, "gone")} {
		if got := Stores(folder); got.Answer != StoresNotArchive || !got.NoManifest {
			t.Errorf("%s: %+v", name, got)
		}
	}
	bad := filepath.Join(dir, "bad")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, manifestName), []byte("not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Stores(bad); got.Answer != StoresNotArchive || got.NoManifest {
		t.Errorf("a manifest this build does not read: %+v", got)
	}
	if runtime.GOOS != "windows" {
		linked := filepath.Join(dir, "linked")
		if err := os.MkdirAll(linked, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(filepath.Join(arch, manifestName), filepath.Join(linked, manifestName)); err != nil {
			t.Fatal(err)
		}
		if got := Stores(linked); got.Answer != StoresNotArchive || got.NoManifest {
			t.Errorf("a link at the manifest's name is not followed: %+v", got)
		}
	}

	// Cannot read now: a read that fails part way (fault injection), a folder this user cannot open.
	failing := filepath.Join(dir, "failing")
	writeArchive(t, failing, map[string]string{"path:/mail/a": "a"})
	old := manifestReader
	manifestReader = func(f *os.File) io.Reader { return &failingReader{r: f, n: 8} }
	got = Stores(failing)
	manifestReader = old
	code, msg := 0, ""
	if got.Answer == StoresCannotRead {
		code, msg = 1, "cannot read now: "+failing
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(failing))
	if again := Stores(failing); again.Answer != StoresArchive {
		t.Errorf("a read that failed is not kept: the next read passes: %+v", again)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		shut := filepath.Join(dir, "shut")
		writeArchive(t, shut, map[string]string{"path:/mail/a": "a"})
		if err := os.Chmod(shut, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(shut, 0o700) })
		if got := Stores(shut); got.Answer != StoresCannotRead {
			t.Errorf("a folder this user cannot open: %+v", got)
		}
	}

	// Lexical: a legacy id naming a link is kept as the link's path; a UNC-shaped id is text.
	target := t.TempDir()
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(target, link); err != nil {
		// Amended 2026-11-25 (round D; nothing weakened elsewhere): an ordinary Windows user may not
		// make a symbolic link (the Windows lab of MA-1503), so there this last part is skipped with
		// the reason, as this package's other link checks are (index_test.go); elsewhere it fails.
		if runtime.GOOS == "windows" {
			t.Skipf("no symbolic link here: an ordinary Windows user may not make one (%v)", err)
		}
		t.Fatal(err)
	}
	planted := filepath.Join(dir, "planted")
	writeArchive(t, planted, map[string]string{link: "a", `path:\\attacker\share\x`: "b"})
	ids := Stores(planted).IDs
	if len(assure.Reached(t, ids, "planted ids")) != 2 {
		t.Fatalf("planted ids: %q", ids)
	}
	lexical, resolved := state.SnapshotStoreID(link), state.PathSourceID(link)
	if lexical == resolved {
		t.Fatalf("the fixture's link must resolve elsewhere: %q", lexical)
	}
	has := func(id string) bool {
		for _, s := range ids {
			if s == id {
				return true
			}
		}
		return false
	}
	if !has(lexical) || has(resolved) {
		t.Errorf("the link's id is its own path, never where it leads: %q (lexical %q, resolved %q)", ids, lexical, resolved)
	}
	if !strings.Contains(strings.Join(ids, "\n"), `path:\\attacker\share\x`) {
		t.Errorf("a UNC-shaped id is kept as text: %q", ids)
	}
}
