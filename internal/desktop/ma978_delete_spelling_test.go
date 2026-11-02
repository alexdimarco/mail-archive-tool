package desktop

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
)

// aliasOf is a second spelling of folder: the same folder reached through a link to its parent,
// standing in for a mapped drive beside its network path, an 8.3 name or a junction above it.
func aliasOf(t *testing.T, folder string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(filepath.Dir(folder), link); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}
	return filepath.Join(link, filepath.Base(folder))
}

// unlistedArchive is a plain archive not yet listed.
func unlistedArchive(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, dir)
	return dir
}

// covers: MA-978, R36
// Of two rows that name one folder (archindex keeps both spellings), the row the person clicked
// is the one planned and removed (del-s3a-A1-01): the plan answers with that row, step 9 removes
// that row, and a link row listed first does not turn the real row's Delete or Remove from list
// into the link's. Positive twin first: the first row's own spelling plans as itself.
func TestTheRowClickedIsTheRowPlannedAndRemoved(t *testing.T) {
	e := newDeleteEnv(t)
	dir := unlistedArchive(t, "Mail")
	alias := aliasOf(t, dir)
	e.list(alias, dir)

	if p := e.plan(alias); p["ok"] != true || p["row"] != alias {
		t.Fatalf("the positive twin: the alias row: %v", p)
	}
	p := e.plan(dir)
	if p["ok"] != true || p["row"] != dir {
		t.Fatalf("the clicked row %s was planned as %v", dir, p["row"])
	}
	r := e.deleteWith(p["token"].(string), p["name"].(string), false)
	if r["ok"] != true || r["row"] != dir || r["rowRemoved"] != true {
		t.Fatalf("the delete: %v", r)
	}
	if e.listed(dir) {
		t.Errorf("the clicked row %s is still listed after a delete that said it removed it: %v", dir, e.rows())
	}

	// A link row listed before the real one.
	e2 := newDeleteEnv(t)
	real := unlistedArchive(t, "Real")
	link := filepath.Join(t.TempDir(), "MailLink")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}
	e2.list(link, real)
	p = e2.plan(real)
	if p["ok"] != true || p["row"] != real {
		t.Errorf("the real row behind a link row: %v", p)
	}
	_, u := e2.post("/api/archive/unlist", map[string]any{"path": real})
	if u["ok"] != true || u["row"] != real {
		t.Errorf("Remove from list of the real row: %v", u)
	}
}

// covers: MA-978, R36
// The page's reader and its lock of an encrypted archive reach the archive under any spelling
// (del-s3a-A1-02, conformance gap 3): a hold for the row's spelling closes the reader's index of
// the archive opened under another spelling of the same folder and refuses that spelling until
// released; LockForDelete closes an archive open under another spelling and refuses its open
// under any spelling until EndDelete. Positive twins first: a hold of another archive leaves the
// reader open, and the archive unlocks before the mark.
func TestTheReaderAndThePageLockReachTheArchiveUnderAnySpelling(t *testing.T) {
	e := newDeleteEnv(t)
	dir := unlistedArchive(t, "Read")
	alias := aliasOf(t, dir)
	other := unlistedArchive(t, "Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: alias}); err != nil {
		t.Fatal(err)
	}
	rm := &readerMux{cfg: e.cfg}
	t.Cleanup(func() { rm.Close() })
	serve := func() *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
		return rec
	}
	if rec := serve(); rec.Code != http.StatusOK || rm.ix == nil {
		t.Fatalf("the reader opens the archive under its alias: %d %s", rec.Code, rec.Body.String())
	}
	release := rm.holdFor(other)
	if rm.ix == nil {
		t.Fatal("the positive twin: a hold of another archive closed this one's index")
	}
	release()

	release = rm.holdFor(dir)
	rm.mu.Lock()
	closed := rm.ix == nil
	rm.mu.Unlock()
	if !closed {
		t.Fatal("a hold for the row's spelling left the index of the same folder open under its alias")
	}
	rec := serve()
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusServiceUnavailable), assure.Names(readerDeletingNote),
		assure.NoSideEffect(func() bool { rm.mu.Lock(); defer rm.mu.Unlock(); return rm.ix == nil }))
	release()
	if rec := serve(); rec.Code != http.StatusOK {
		t.Errorf("after the hold the reader does not open the archive again: %d", rec.Code)
	}

	// The registry: an encrypted archive open under a spelling other than the row's.
	out := backedUpEncrypted(t)
	store := graphconfig.NewFileSecretStore(t.TempDir())
	k := newArchiveKeys(store, readerLockedNote)
	t.Cleanup(func() { k.lockAll(true) })
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	// Stand in for its open under another spelling: the same entry under the alias's key.
	aliasOut := aliasOf(t, out)
	_, realKey := keyOf(out)
	_, aliasKey := keyOf(aliasOut)
	k.mu.Lock()
	k.entries[aliasKey], k.entries[realKey] = k.entries[realKey], nil
	delete(k.entries, realKey)
	k.mu.Unlock()
	if k.get(aliasOut) == nil {
		t.Fatal("the stand-in: the archive is not open under its alias")
	}
	if err := k.LockForDelete(out); err != nil {
		t.Fatal(err)
	}
	if k.get(aliasOut) != nil {
		t.Fatal("LockForDelete of the row's spelling left the archive open under another spelling")
	}
	// An Unlock under a third spelling, which no entry was open under: refused before its key is
	// derived.
	third := aliasOf(t, out)
	derived := false
	_, err := k.open(third, false, nil, func(string) (*keyring.Unlocked, error) {
		derived = true
		return openArchive(out, keysPassword, keyring.OpenOptions{})
	})
	if !errors.Is(err, errBeingDeleted) || derived {
		t.Fatalf("an Unlock under another spelling during the delete: %v (key derived %v); want %q before any key is derived", err, derived, errBeingDeleted)
	}
	assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names("This archive is being deleted."),
		assure.NoSideEffect(func() bool { return k.get(third) == nil && k.get(aliasOut) == nil && k.get(out) == nil }))
	k.EndDelete(out)
	if _, err := k.unlock(out, keysPassword, keyring.OpenOptions{}); err != nil {
		t.Errorf("after EndDelete the archive does not unlock: %v", err)
	}
	if _, err := k.open(aliasOut, false, nil, func(string) (*keyring.Unlocked, error) {
		return openArchive(out, keysPassword, keyring.OpenOptions{})
	}); err != nil {
		t.Errorf("after EndDelete the archive does not unlock under the other spelling LockForDelete marked: %v", err)
	}
	k.lock(aliasOut)

	// A delete of the folder that begins while an Unlock under another spelling derives its key:
	// the open loses.
	k.lock(out)
	_, err = k.open(third, false, nil, func(string) (*keyring.Unlocked, error) {
		if err := k.LockForDelete(out); err != nil {
			return nil, err
		}
		return openArchive(out, keysPassword, keyring.OpenOptions{})
	})
	if !errors.Is(err, errBeingDeleted) || k.get(third) != nil {
		t.Errorf("an Unlock that a delete of the same folder landed on: %v; want %q", err, errBeingDeleted)
	}
	k.EndDelete(out)
}

// covers: MA-978, R36
// An Unlock under another spelling of a folder, begun before the folder's delete locks it and
// finishing between LockForDelete's two locked sections (after it looked for the archive open
// under other spellings, before it closes them), is refused "This archive is being deleted." and
// leaves the archive locked (del-s3a re-verification, R2): the folder's mark is on the delete list
// from LockForDelete's first section, so open's own check after the key is derived refuses it.
// Positive twin first: the same Unlock with no delete opens the archive.
func TestAnUnlockFinishingInsideLockForDeleteIsRefused(t *testing.T) {
	out := backedUpEncrypted(t)
	k := newArchiveKeys(graphconfig.NewFileSecretStore(t.TempDir()), readerLockedNote)
	t.Cleanup(func() { k.lockAll(true) })
	alias := aliasOf(t, out)
	opener := func(string) (*keyring.Unlocked, error) { return openArchive(out, keysPassword, keyring.OpenOptions{}) }
	if _, err := k.open(alias, false, nil, opener); err != nil || k.get(alias) == nil {
		t.Fatalf("the positive twin: an Unlock under the other spelling: %v", err)
	}
	k.lock(alias)

	_, aliasKey := keyOf(alias)
	derive, opened := make(chan struct{}), make(chan error, 1)
	go func() {
		_, err := k.open(alias, false, nil, func(abs string) (*keyring.Unlocked, error) {
			<-derive
			return opener(abs)
		})
		opened <- err
	}()
	for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(5 * time.Millisecond) {
		k.mu.Lock()
		busy := k.busy[aliasKey]
		k.mu.Unlock()
		if busy {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the Unlock never began")
		}
	}
	t.Cleanup(func() { lockForDeleteGap = nil })
	var gapErr error
	lockForDeleteGap = func() {
		close(derive)
		gapErr = <-opened
	}
	if err := k.LockForDelete(out); err != nil {
		t.Fatal(err)
	}
	lockForDeleteGap = nil
	got := ""
	if gapErr != nil {
		got = gapErr.Error()
	}
	assure.Refused(t, 1, got, assure.Code(1), assure.Names("This archive is being deleted."),
		assure.NoSideEffect(func() bool { return errors.Is(gapErr, errBeingDeleted) && k.get(alias) == nil && k.get(out) == nil }))
	k.EndDelete(out)
}

// covers: MA-978, R36
// With no archive set (Settings cleared, or a Delete of the current archive cleared it), the
// reader closes the index it had open, so this process does not keep the old archive's
// search.db, which Windows would refuse to delete (del-s3a-A1-02). Positive twin first: with the
// archive set, the reader serves it from its open index.
func TestTheReaderLetsGoOfTheIndexOnceNoArchiveIsSet(t *testing.T) {
	e := newDeleteEnv(t)
	dir := unlistedArchive(t, "Was")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	rm := &readerMux{cfg: e.cfg}
	t.Cleanup(func() { rm.Close() })
	rec := httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
	if rec.Code != http.StatusOK || rm.ix == nil {
		t.Fatalf("the positive twin: %d", rec.Code)
	}
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: ""}); err != nil {
		t.Fatal(err)
	}
	rec = httptest.NewRecorder()
	rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
	assure.Refused(t, rec.Code, rec.Body.String(), assure.Code(http.StatusServiceUnavailable), assure.Names("No archive location set yet"),
		assure.NoSideEffect(func() bool { rm.mu.Lock(); defer rm.mu.Unlock(); return rm.ix == nil }))
}

// covers: MA-978, R36
// The page's reader hold is released however the delete ends, a panic before the delete's own
// cleanup is set up included (conformance gap 2): the reader serves the archive again, and the
// delete is no longer under way. Positive twin first: the reader serves the archive before.
func TestTheReaderIsReleasedWhenTheDeleteStopsUnexpectedly(t *testing.T) {
	var rm *readerMux
	releases := 0
	e := newDeleteEnv(t, func(w *wsEnv) {
		rm = &readerMux{}
		w.cfg.ReaderHold = func(out string) func() {
			release := rm.holdFor(out)
			return func() { releases++; release() }
		}
	})
	rm.cfg = e.cfg
	t.Cleanup(func() { rm.Close() })
	dir := e.plainArchive("Stops")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	serve := func() int {
		rec := httptest.NewRecorder()
		rm.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/search?q=hello", nil))
		return rec.Code
	}
	if c := serve(); c != http.StatusOK {
		t.Fatalf("the positive twin: %d", c)
	}
	p := e.plan(dir)
	if p["ok"] != true {
		t.Fatalf("the plan: %v", p)
	}
	before := treeState(t, dir)
	e.d.now = func() time.Time { panic("the clock stopped") } // step 0 reads it first
	code, r := e.post("/api/archive/delete", map[string]any{"token": p["token"], "typed": p["name"]})
	assure.Refused(t, code, replyErr(r), assure.Code(http.StatusInternalServerError), assure.Names("stopped this request unexpectedly"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeState(t, dir), before) }))
	if releases != 1 {
		t.Fatalf("the reader hold was released %d times; want once", releases)
	}
	if c := serve(); c != http.StatusOK {
		t.Errorf("the reader still refuses the archive after the delete stopped: %d", c)
	}
	if e.d.running.of(dir) != nil {
		t.Error("the delete is still recorded as under way")
	}
	if strings.Contains(deletionsLogFor(t, e.cfg, dir), `"started"`) {
		t.Error("a started line was written for a delete that stopped before step 0")
	}
}

// deletionsLogFor is the deletion log's lines that name folder, in cfg's settings folder.
func deletionsLogFor(t *testing.T, cfg Config, folder string) string {
	t.Helper()
	var out []string
	for _, l := range strings.Split(deletionsLog(t, cfg), "\n") {
		if strings.Contains(l, folder) {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
