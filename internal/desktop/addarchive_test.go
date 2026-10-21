package desktop

import (
	"errors"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

// covers: MA-696
// The "Your archives" card's "Add another archive" button runs the wizard's local
// add flow in process and folds the new archive into the known-archives index so it
// LISTS — without making it current (an ADDITIONAL archive, not a switch). A cancel
// ("" path) adds nothing; a second click while one is open is refused (the native
// dialogs are process-global); a failure in the flow is reported. The flow itself is
// the native-dialog wizard (lab-tier), so it is exercised here through the
// addArchiveFn seam rather than a real GUI.
func TestMA696_AddAnotherArchive(t *testing.T) {
	restore := addArchiveFn
	defer func() { addArchiveFn = restore }()

	guarded := func(d *dashboard) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/add-archive", nil)
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		rec := httptest.NewRecorder()
		d.addArchive(rec, req)
		return rec
	}

	// Success: a new archive is created, registered, and listed — but not made current.
	idx := filepath.Join(t.TempDir(), archindex.Name)
	newArch := t.TempDir()
	writeManifest(t, newArch)
	called := false
	addArchiveFn = func() (string, error) { called = true; return newArch, nil }
	d := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idx})
	if body := guarded(d).Body.String(); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("a created archive must be added, got %s", body)
	}
	if !called {
		t.Fatal("the add flow was never invoked")
	}
	after, err := archindex.ReadFile(idx)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Entries) != 1 || cleanPath(after.Entries[0].Path) != cleanPath(newArch) {
		t.Fatalf("the new archive must be in the index, got %+v", after.Entries)
	}
	if d.cfg.effectiveOut() == newArch {
		t.Fatal("adding an archive must NOT switch to it (it is additional, not current)")
	}

	// Cancel: the user backs out ("" path) — nothing is added.
	idx2 := filepath.Join(t.TempDir(), archindex.Name)
	addArchiveFn = func() (string, error) { return "", nil }
	d2 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s2.json"), IndexPath: idx2})
	if body := guarded(d2).Body.String(); !strings.Contains(body, `"canceled":true`) {
		t.Fatalf("a cancelled add must report canceled, got %s", body)
	}
	if got, _ := archindex.ReadFile(idx2); len(got.Entries) != 0 {
		t.Fatalf("a cancelled add must register nothing, got %+v", got.Entries)
	}

	// Failure in the flow is reported, not swallowed.
	addArchiveFn = func() (string, error) { return "", errors.New("the wizard could not open its windows") }
	d3 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s3.json"), IndexPath: filepath.Join(t.TempDir(), archindex.Name)})
	if body := guarded(d3).Body.String(); !strings.Contains(body, `"ok":false`) || !strings.Contains(body, "could not open its windows") {
		t.Fatalf("a flow error must be reported, got %s", body)
	}

	// Busy: a second add while one is open is refused, and the flow is not entered.
	entered := false
	addArchiveFn = func() (string, error) { entered = true; return t.TempDir(), nil }
	d4 := newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s4.json"), IndexPath: filepath.Join(t.TempDir(), archindex.Name)})
	d4.addBusy.Store(true)
	if body := guarded(d4).Body.String(); !strings.Contains(body, "already in progress") {
		t.Fatalf("a concurrent add must be refused, got %s", body)
	}
	if entered {
		t.Fatal("a refused add must not enter the flow")
	}

	// registerArchive is monotone: re-registering the same archive is a no-op.
	idx5 := filepath.Join(t.TempDir(), archindex.Name)
	cfg := Config{IndexPath: idx5}
	p := t.TempDir()
	writeManifest(t, p) // Reconcile only folds in a real archive folder
	if err := cfg.registerArchive(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := cfg.registerArchive(p, time.Now()); err != nil {
		t.Fatal(err)
	}
	if got, _ := archindex.ReadFile(idx5); len(got.Entries) != 1 {
		t.Fatalf("re-registering the same archive must not duplicate it, got %+v", got.Entries)
	}
}

// covers: MA-696
// Regression (discoverability): "Add another archive" must be reachable in the common
// case of a single, CURRENT archive — exactly when the "Your archives" list card is
// deliberately hidden (MA-666, "nothing new to say"). The add action therefore lives in
// its own always-present card (#sec-add-archive), not nested inside the list card, so a
// one-archive user can still set up a second. Before the fix the button rendered only
// inside the hidden list card, so a lone-current user could not reach it at all.
func TestMA696_AddArchiveReachableWithLoneCurrent(t *testing.T) {
	dir := t.TempDir()
	idxPath := filepath.Join(dir, archindex.Name)
	settings := filepath.Join(dir, "s.json")
	a := archiveAt(t, dir, "A", map[string]string{"path:/mail/a.mbox": "a"})
	if err := archindex.WriteFile(idxPath, archindex.Index{Entries: []archindex.Entry{
		{Path: a, AddedAt: at0, LastSeenAt: at0},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(settings, Settings{Out: a}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: settings, IndexPath: idxPath, GOOS: "darwin"}))
	assure.Reached(t, page, "overview page")
	// The list card stays hidden for a lone current archive (MA-666 holds) ...
	if strings.Contains(page, `id="sec-archives"`) {
		t.Error("the Your-archives list card must stay hidden when the only archive is the current one")
	}
	// ... but the Add-another-archive action must still be present and reachable.
	if !strings.Contains(page, `id="sec-add-archive"`) {
		t.Error("the Add-another-archive card must be present for a lone current archive")
	}
	if !strings.Contains(page, `id="addArchiveBtn"`) {
		t.Error("the Add-another-archive button must be reachable for a lone current archive")
	}
}
