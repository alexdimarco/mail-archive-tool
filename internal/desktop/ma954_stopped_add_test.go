package desktop

import (
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/wizard"
)

// covers: MA-954
// A copy stopped part way is not reported as a finished archive (the friction review of
// the 1.5.2 rulings, 2026-10-31, finding 6). Positive twin first: a finished copy is
// added and reported as before. When the wizard says the copy was stopped
// (wizard.ErrStoppedEarly) and the folder holds an archive (the mail copied so far), it
// is added to the list, the page is not reloaded as for a finished one, and the note
// says, whole, that it was stopped, what it holds and how to finish it. A folder the
// copy never reached (no archive in it) is not added, and the note says so.
func TestStoppedCopyIsNotReportedAsAdded(t *testing.T) {
	restore := addArchiveFn
	defer func() { addArchiveFn = restore }()
	post := func(d *dashboard) string {
		req := httptest.NewRequest("POST", "http://127.0.0.1:8097/api/add-archive", nil)
		req.Header.Set("Origin", "http://127.0.0.1:8097")
		req.Header.Set("X-CSRF-Token", d.csrf)
		rec := httptest.NewRecorder()
		d.addArchive(rec, req)
		return rec.Body.String()
	}
	listed := func(idx string) []archindex.Entry {
		got, _ := archindex.ReadFile(idx)
		return got.Entries
	}

	// Positive twin: a finished copy.
	done := t.TempDir()
	writeManifest(t, done)
	idx := filepath.Join(t.TempDir(), archindex.Name)
	addArchiveFn = func() (string, error) { return done, nil }
	body := post(newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idx}))
	if !strings.Contains(body, `"ok":true`) || !strings.Contains(body, "Archive added") || len(listed(idx)) != 1 {
		t.Fatalf("positive twin: a finished copy: %s, listed %v", body, listed(idx))
	}

	// Stopped part way, with the mail copied so far.
	part := t.TempDir()
	writeManifest(t, part)
	idx2 := filepath.Join(t.TempDir(), archindex.Name)
	addArchiveFn = func() (string, error) { return part, wizard.ErrStoppedEarly }
	body = assure.Reached(t, post(newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s2.json"), IndexPath: idx2})), "the stopped add's reply")
	if strings.Contains(body, `"ok":true`) || strings.Contains(body, "Archive added") || !strings.Contains(body, `"stopped":true`) {
		t.Errorf("a stopped copy is reported as added: %s", body)
	}
	if want := stoppedAddMessage; !strings.Contains(body, want) {
		t.Errorf("a stopped copy's note: want %q in %s", want, body)
	}
	if e := listed(idx2); len(e) != 1 || cleanPath(e[0].Path) != cleanPath(part) {
		t.Errorf("a stopped copy with mail in it is not listed: %v", e)
	}

	// Stopped before any mail reached the folder.
	empty := t.TempDir()
	idx3 := filepath.Join(t.TempDir(), archindex.Name)
	addArchiveFn = func() (string, error) { return empty, wizard.ErrStoppedEarly }
	body = post(newDashboard(Config{SettingsPath: filepath.Join(t.TempDir(), "s3.json"), IndexPath: idx3}))
	if strings.Contains(body, `"ok":true`) || !strings.Contains(body, stoppedEmptyMessage) {
		t.Errorf("a copy stopped before any mail: want %q in %s", stoppedEmptyMessage, body)
	}
	if e := listed(idx3); len(e) != 0 {
		t.Errorf("a folder the copy never reached is listed: %v", e)
	}

	// The page shows the reply's message and reloads only for a finished add.
	if !strings.Contains(dashboardJS, "n.textContent=d.message||d.error||'Could not add an archive.'") {
		t.Error("the page does not show the add's message for a stopped copy")
	}
}

const (
	stoppedAddMessage   = "The wizard was stopped before it finished copying your mail. The archive keeps the mail copied so far and is listed under Your archives once you reload this page. To finish it, click Make a new archive… again and choose the same mail and the same folder."
	stoppedEmptyMessage = "The wizard was stopped before it copied any mail. No archive was added."
)
