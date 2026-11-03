package desktop

import (
	"context"
	"encoding/json"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/schedule"
)

// The other actions on a delete only the folder's marker knows (docs/design-archive-delete.md
// §15.4; §15.10 slice 4; MA-1052): Open an archive folder… lists such a folder and never makes it
// current (DM-17), a save that keeps the location is allowed (DM-18), and Remove from list is
// allowed (DM-21).

// ma1052Started writes a started line for folder into this Desktop's deletion log, as a delete
// begun here and stopped leaves it.
func ma1052Started(t *testing.T, cfg Config, folder string) {
	t.Helper()
	p := cfg.deletionsLogPath()
	line, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "ffffeeeeddddcccc", "folder": folder, "row": folder, "kind": "plain"})
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

// ma1052MarkerOnlyFolder is a folder a delete begun elsewhere left after its anchor went: its
// marker, a state file and the person's own file.
func ma1052MarkerOnlyFolder(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for f, body := range map[string]string{".mailarchive-lastrun.json": "{}", "notes.txt": "the person's own notes\n"} {
		if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	ma1048Plant(t, dir)
	return dir
}

// covers: MA-1052, R36
// Open an archive folder… lists a folder holding the delete marker, with or without its manifest,
// and never makes it the current archive (DM-17): its own route adds the row (as add-archive does)
// and answers the words "Added to Your archives: DIR. A delete of this archive did not finish: to
// finish it, click Delete… on its row." with no path for the page to save; the settings, the
// current archive and the folder are as they were; picked again it is "already in Your archives";
// its row offers Delete…, and Delete finishes it. Positive twin first: an ordinary archive picked is
// answered with its path for the switch, and listed by nothing here.
func TestMA1052_OpenAnArchiveFolderListsAMarkerFolderAndNeverSwitches(t *testing.T) {
	e := newDeleteEnv(t)
	cur := e.plainArchive("Current")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: cur}); err != nil {
		t.Fatal(err)
	}
	picked := ""
	e.d.pick = func(context.Context, string) (string, error) { return picked, nil }
	open := func() map[string]any {
		t.Helper()
		_, m := e.post("/api/open-archive", map[string]any{})
		return m
	}

	// Positive twin.
	ordinary := filepath.Join(t.TempDir(), "Ordinary")
	if err := os.MkdirAll(ordinary, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, ordinary)
	picked = ordinary
	if m := open(); m["ok"] != true || m["path"] != ordinary || m["added"] != nil || e.listed(ordinary) {
		t.Fatalf("positive twin: %v", m)
	}

	withManifest := filepath.Join(t.TempDir(), "Begun elsewhere")
	if err := os.MkdirAll(withManifest, 0o755); err != nil {
		t.Fatal(err)
	}
	buildArchiveAt(t, withManifest)
	ma1048Plant(t, withManifest)
	for _, dir := range []string{withManifest, ma1052MarkerOnlyFolder(t, "Files gone")} {
		before := treeState(t, dir)
		settings, _ := os.ReadFile(e.cfg.SettingsPath)
		picked = dir
		m := open()
		want := "Added to Your archives: " + dir + ". A delete of this archive did not finish: to finish it, click Delete… on its row."
		if m["ok"] != true || m["added"] != true || m["note"] != want || m["path"] != nil {
			t.Fatalf("%s: %v", dir, m)
		}
		now, _ := os.ReadFile(e.cfg.SettingsPath)
		if !e.listed(dir) || e.d.cfg.effectiveOut() != cur || string(now) != string(settings) || !reflect.DeepEqual(before, treeState(t, dir)) {
			t.Fatalf("%s: listed %v, current %s, settings changed %v", dir, e.listed(dir), e.d.cfg.effectiveOut(), string(now) != string(settings))
		}
		if m := open(); m["ok"] != true || m["added"] != false || m["note"] != dir+" is already in Your archives. A delete of this archive did not finish: to finish it, click Delete… on its row." {
			t.Fatalf("picked again: %v", m)
		}
		row := rowFor(t, e.page(), dir)
		if !strings.Contains(row, "data-delete-unfinished") || !strings.Contains(row, `data-delete-ok="true"`) || strings.Contains(row, "data-switch-archive") {
			t.Fatalf("its row: %s", html.UnescapeString(row))
		}
		if d := e.planAndDelete(dir); d["status"] != archdelete.StatusComplete {
			t.Fatalf("Delete finishes it: %v", d)
		}
	}
}

// covers: MA-1052, R36
// A settings save that keeps the location of a current archive only the marker knows, and installs
// or repairs nothing, is allowed (DM-18): on a computer that began no delete nothing clears the
// current archive, so its person could otherwise save no setting at all. A save that newly makes
// the folder current, and Install and Repair, stay refused in the marker-only words; a delete this
// computer's log knows keeps MA-1015's refusal of the same save. Positive twins first: the two
// refusals, nothing saved.
func TestMA1052_ASaveKeepingAMarkerOnlyCurrentArchiveIsAllowed(t *testing.T) {
	e := newDeleteEnv(t)
	installs := 0
	e.cfg.InstallBackup = func(schedule.Spec) error { installs++; return nil }
	e.d.cfg = e.cfg
	other := e.plainArchive("Other")
	marked := e.plainArchive("Marked")
	logged := e.plainArchive("Logged")
	ma1048Plant(t, marked)
	// A delete begun here and stopped in step 6: this computer's log knows it, and its marker is there.
	ma1052Started(t, e.cfg, logged)
	ma1048Plant(t, logged)
	words := func(done string) string { return delmark.OnlyMarkerWords(done) + reloadRowText }

	// Positive twins: refused, nothing saved.
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: other}); err != nil {
		t.Fatal(err)
	}
	_, s := e.post("/api/settings", map[string]any{"Out": marked})
	assure.Refused(t, http.StatusOK, replyErr(s), assure.Code(http.StatusOK), assure.Names(words("it was not made the current archive")),
		assure.NoSideEffect(func() bool { return s["ok"] == false && e.d.cfg.effectiveOut() == other }))
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: logged}); err != nil {
		t.Fatal(err)
	}
	_, l := e.post("/api/settings", map[string]any{"Out": logged, "KeepRaw": true})
	assure.Refused(t, http.StatusOK, replyErr(l), assure.Code(http.StatusOK),
		assure.Names("Delete not finished: click Delete again. This archive's delete did not finish, so these settings were not saved; nothing was changed."),
		assure.NoSideEffect(func() bool { return l["ok"] == false && !LoadSettings(e.cfg.SettingsPath).KeepRaw }))

	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: marked}); err != nil {
		t.Fatal(err)
	}
	if _, k := e.post("/api/settings", map[string]any{"Out": marked, "KeepRaw": true}); k["ok"] != true || !LoadSettings(e.cfg.SettingsPath).KeepRaw || e.d.cfg.effectiveOut() != marked {
		t.Fatalf("a save keeping the location: %v", k)
	}
	for name, body := range map[string]map[string]any{
		"Install": {"interval": "weekly", "time": "03:00", "drawnFor": marked},
		"Repair":  {"drawnFor": marked},
	} {
		code, m := e.post("/api/schedule-install", body)
		assure.Refused(t, code, replyErr(m), assure.Code(http.StatusOK), assure.Names(delmark.OnlyMarkerWords("its scheduled backup was not installed")),
			assure.NoSideEffect(func() bool { return m["ok"] == false && installs == 0 }))
		if t.Failed() {
			t.Fatalf("%s: %v", name, m)
		}
	}
}

// covers: MA-1052, R36
// Remove from list on the Desktop is allowed for a delete only the marker knows (DM-21): the
// question names this computer's schedule into the folder, Yes stops it and removes the row, and the
// folder and its marker are kept; the row drawn before says nothing of Remove from list being
// refused. Positive twin first: a delete this computer's log knows is refused (MA-1029).
func TestMA1052_RemoveFromListOnTheDesktopIsAllowedForAMarkerOnlyDelete(t *testing.T) {
	e := newDeleteEnv(t)
	logged := e.plainArchive("Logged")
	ma1052Started(t, e.cfg, logged)
	_, r := e.post("/api/archive/unlist", map[string]any{"path": logged})
	assure.Refused(t, http.StatusOK, replyErr(r), assure.Code(http.StatusOK), assure.Names("Delete not finished: click Delete again", "it was not removed from the list"),
		assure.NoSideEffect(func() bool { return e.listed(logged) }))

	marked := e.plainArchive("Marked")
	ma1048Plant(t, marked)
	def := schedule.DefaultNameFor(marked)
	e.sched.installed[def] = true
	if row := rowFor(t, e.page(), marked); strings.Contains(row, "data-unlist-why") {
		t.Fatalf("its row says Remove from list is refused: %s", html.UnescapeString(row))
	}
	before := treeState(t, marked)
	_, q := e.post("/api/archive/unlist", map[string]any{"path": marked})
	if q["ok"] != true || !strings.Contains(q["question"].(string), def) {
		t.Fatalf("the question: %v", q)
	}
	_, y := e.post("/api/archive/unlist", map[string]any{"path": marked, "confirm": true, "named": q["named"]})
	if y["ok"] != true || e.listed(marked) || !contains(e.sched.removedNames(), def) || !reflect.DeepEqual(before, treeState(t, marked)) {
		t.Fatalf("Yes: %v (removed %q)", y, e.sched.removedNames())
	}
}

// covers: MA-1052, R36, S41
// The page's Open an archive folder… button, for a folder the server listed itself (DM-17): the
// page shows the server's words and never sends the save that would switch to it, then draws the
// page again with the words kept; for an ordinary folder it saves as before. Positive twin first.
func TestMA1052_TheOpenButtonListsAMarkerFolderWithoutTheSave(t *testing.T) {
	if passed := runPageScenario(t, "openMarker"); len(passed) != 4 {
		t.Errorf("the openMarker scenario passed %d of its 4 checks: %v", len(passed), passed)
	}
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
