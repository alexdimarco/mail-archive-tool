package desktop

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// unlistDisabledAsRouted checks the row of path on page: Remove from list disabled, its title the
// route's own refusal of path (e posts it), which must name says.
func unlistDisabledAsRouted(t *testing.T, e *delEnv, page, path, says string) {
	t.Helper()
	_, refused := e.post("/api/archive/unlist", map[string]any{"path": path})
	why := replyErr(refused)
	assure.Refused(t, http.StatusOK, why, assure.Code(http.StatusOK), assure.Names(says),
		assure.NoSideEffect(func() bool { return e.listed(path) }))
	unl := rowButton(rowFor(t, page, path), "data-unlist-archive")
	if !strings.Contains(unl, " disabled") || !strings.Contains(unl, `title="`+why+`"`) {
		t.Errorf("the row of %s: Remove from list is not disabled with the route's words (%q) as its title: %s", path, why, unl)
	}
}

// covers: MA-1016, R36, S41
// Remove from list is disabled, the route's own words as its title, for each refusal the row asks
// (the 1.5.3 friction fixes' adversarial pass, M2; before, only the archive in use, by its exact
// spelling, was tested): a Desktop started with -out naming the folder, another archive's
// scheduled capture registered under the folder's name, and the archive in use listed under
// another spelling of its folder (a trailing separator, a ".." on its path). Positive twin first:
// a plain other archive's Remove from list is enabled.
func TestRemoveFromListIsDisabledForEachRefusal(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	under := e.plainArchive("Under")
	plain := e.plainArchive("Plain")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur, BackupFor: under}); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	trailing := cur + sep
	dotted := cur + sep + ".." + sep + filepath.Base(cur)
	e.list(trailing, dotted)
	page := e.page()
	if unl := rowButton(rowFor(t, page, plain), "data-unlist-archive"); unl == "" || strings.Contains(unl, "disabled") {
		t.Fatalf("the positive twin: a plain other archive's Remove from list is not enabled: %s", unl)
	}
	unlistDisabledAsRouted(t, e, page, under, "is still registered under this folder's name")
	unlistDisabledAsRouted(t, e, page, trailing, "This is the archive in use.")
	unlistDisabledAsRouted(t, e, page, dotted, "This is the archive in use.")

	// A Desktop started with -out naming the folder.
	launched := e.plainArchive("Launched")
	e2 := newDeleteEnv(t, func(w *wsEnv) { w.cfg.Out = launched })
	e2.list(launched, filepath.Join(t.TempDir(), "Another"))
	unlistDisabledAsRouted(t, e2, e2.page(), launched, "Restart MailArchive without -out, then remove it from the list.")
}

// covers: MA-1016, R36, S41
// The page asks Remove from list's refusal of the archive in use through its rows check, with the
// row's kept manifest parse (the 1.5.3 friction fixes' adversarial pass, M1; MA-1021): a manifest
// unchanged since the last page is not read whole again on each page load. Kept by identity,
// size and modification time, so a manifest that changed is read again and the page then offers
// Remove from list as the route does. Positive twin first: the archive in use's Remove from list
// is disabled. (The parse is shown kept by bytes that do not parse written in place at the same
// size, the modification time put back.)
func TestTheRowsUnlistCheckKeepsTheManifestsParse(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	e.plainArchive("Other")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	if unl := rowButton(rowFor(t, e.page(), cur), "data-unlist-archive"); !strings.Contains(unl, " disabled") {
		t.Fatalf("the positive twin: the archive in use's Remove from list is offered: %s", unl)
	}
	man := filepath.Join(cur, ".mailarchive-manifest.json")
	fi, err := os.Stat(man)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(man)
	if err != nil {
		t.Fatal(err)
	}
	data[len(data)-1] = ' ' // the closing brace gone: the same size, no longer parses
	f, err := os.OpenFile(man, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt(data, 0); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.Chtimes(man, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	if unl := rowButton(rowFor(t, e.page(), cur), "data-unlist-archive"); !strings.Contains(unl, " disabled") {
		t.Errorf("the page read the unchanged manifest (identity, size, time) again: %s", unl)
	}
	later := fi.ModTime().Add(time.Minute)
	if err := os.Chtimes(man, later, later); err != nil {
		t.Fatal(err)
	}
	if unl := rowButton(rowFor(t, e.page(), cur), "data-unlist-archive"); unl == "" || strings.Contains(unl, "disabled") {
		t.Errorf("a changed manifest that no longer parses is not an archive, which the route lets go; the page still disables it: %s", unl)
	}
}
