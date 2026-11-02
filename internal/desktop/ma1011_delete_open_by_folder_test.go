package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
)

const (
	sealedNotOpen = "Messages: not known: encrypted, key not open\n"
	sealedOneMsg  = "Messages: 1 (as recorded in the archive)\n"
)

// covers: MA-1011, R36, R37
// A plan states the sealed values of the archive open on this page only when that archive is the
// folder planned — the same folder on disk — and its archive ID is the one the folder's key files
// claim (the delete residuals' security pass, key-use finding 2). The registry's key folds names
// as encrecord.Spelling does (letter case, and ß/ss on Windows and macOS), while NTFS or a
// case-sensitive drive keeps "Straße" and "Strasse" apart: the open archive of one was taken for
// the other, whose plan and deletion record then stated the open archive's message count and
// dates under its own archive ID. The collision is simulated by entering the open archive under
// the other folder's key (fault injection on the registry); a second case enters an entry naming
// the other folder itself but holding another archive's key. Positive twin first: the open
// archive's own plan states its sealed values.
func TestOnlyTheArchiveOpenInThePlannedFolderGivesItsSealedValues(t *testing.T) {
	open := backedUpEncrypted(t)
	other := filepath.Join(filepath.Dir(open), "Other")
	p, err := keyring.Create(other, keysPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := p.Commit()
	if err != nil {
		t.Fatal(err)
	}
	u.Destroy()
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(open, other)

	ent, err := k.unlock(open, keysPassword, keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if text, _ := e.plan(open)["planText"].(string); !strings.Contains(text, sealedOneMsg) {
		t.Fatalf("positive twin: the open archive's own plan states its sealed values:\n%s", text)
	}

	_, okey := keyOf(other)
	inject := func(en *keyEntry) {
		k.mu.Lock()
		k.entries[okey] = en
		k.mu.Unlock()
	}
	defer func() {
		k.mu.Lock()
		delete(k.entries, okey)
		k.mu.Unlock()
	}()
	cases := []struct {
		what string
		en   *keyEntry
	}{
		{"another folder's open archive under this folder's key", ent},
		{"an entry naming this folder that holds another archive's key", &keyEntry{u: ent.u, path: other, id: ent.id}},
	}
	for _, c := range cases {
		inject(c.en)
		text, _ := e.plan(other)["planText"].(string)
		if !strings.Contains(text, sealedNotOpen) || strings.Contains(text, sealedOneMsg) {
			t.Errorf("%s: the plan of %s states sealed values that are not its own:\n%s", c.what, other, text)
		}
	}

	// A copy of the open archive (the same archive ID) in a folder whose name folds to the open
	// one's key: still another folder, whose own sealed record was not read.
	k.mu.Lock()
	delete(k.entries, okey)
	k.mu.Unlock()
	cp := filepath.Join(filepath.Dir(open), "Copy")
	copyTree(t, open, cp)
	e.list(cp)
	_, ckey := keyOf(cp)
	k.mu.Lock()
	k.entries[ckey] = ent
	k.mu.Unlock()
	defer func() {
		k.mu.Lock()
		delete(k.entries, ckey)
		k.mu.Unlock()
	}()
	cpPlan := e.plan(cp)
	if text, _ := cpPlan["planText"].(string); cpPlan["ok"] != true || !strings.Contains(text, sealedNotOpen) {
		t.Errorf("a copy of the open archive in a folder whose name folds to its key: the plan of %s states the open folder's sealed values: %v\n%s", cp, cpPlan, text)
	}
}

// covers: MA-1012, MA-1011, R36, R37
// An archive open on this page under another spelling of its folder than the row's (the current
// archive set through a link or a mapped drive, the row its real path or network path) is open
// here (the delete residuals' security pass, key-use finding 4): this process's own reader of it
// does not refuse the plan, since step 1 closes it (LockForDelete matches the folder on disk,
// MA-978), and the plan states its sealed values rather than "key not open". Before, both were
// asked by the folded name only: the plan refused "Close MailArchive serve, search or other
// MailArchive windows for this archive first", which the person could not act on, and called the
// key not open. Positive twin first: open under the row's own spelling.
func TestAnArchiveOpenUnderAnotherSpellingOfItsFolderIsOpenHere(t *testing.T) {
	row := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	e := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Store = store; w.cfg.Keys = newArchiveKeys(store, readerLockedNote) })
	k := e.cfg.Keys
	t.Cleanup(func() { k.lockAll(true) })
	e.list(row)
	ownReader := func(spelling, id string) func() {
		t.Helper()
		scratch, err := app.ArchiveScratchFor(spelling, id)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Join(scratch, "index", fmt.Sprintf("reader-%d-1.db", os.Getpid()))
		if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
			t.Fatal(err)
		}
		os.WriteFile(name, []byte("index"), 0o600)
		os.WriteFile(name+".lock", nil, 0o600)
		f, err := lockfile.HoldFile(name + ".lock")
		if err != nil {
			t.Fatal(err)
		}
		return func() { f.Close(); os.RemoveAll(filepath.Join(scratch, "index")) }
	}
	planned := func(what string) {
		t.Helper()
		p := e.plan(row)
		text, _ := p["planText"].(string)
		if p["ok"] != true || !strings.Contains(text, sealedOneMsg) {
			t.Fatalf("%s: the plan should go ahead and state the sealed values: %v\n%s", what, p, text)
		}
	}

	en, err := k.unlock(row, keysPassword, keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	release := ownReader(row, en.id)
	planned("positive twin: open under the row's own spelling, with this page's reader")
	release()
	k.lock(row)

	alias := aliasOf(t, row)
	en, err = k.unlock(alias, keysPassword, keyring.OpenOptions{})
	if err != nil {
		t.Fatalf("unlock under %s: %v", alias, err)
	}
	if k.get(row) != nil {
		t.Skip("this platform folds the two spellings into one registry key: nothing to tell apart here")
	}
	release = ownReader(alias, en.id)
	defer release()
	planned("open under another spelling of the folder, with this page's reader")
}
