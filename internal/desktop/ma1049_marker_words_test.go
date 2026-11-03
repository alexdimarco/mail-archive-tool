package desktop

import (
	"encoding/json"
	"errors"
	"html"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/delmark"
)

// ma1049Health is the Desktop's Status & health: its posture and its lines.
func ma1049Health(t *testing.T, e *delEnv) (string, string) {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(e.get("/api/health").Body.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	lines, _ := m["summary"].([]any)
	var s []string
	for _, l := range lines {
		s = append(s, l.(string))
	}
	posture, _ := m["posture"].(string)
	return posture, strings.Join(s, "\n")
}

// covers: MA-1049, R36
// Status & health says what the marker's three LOW-4 cases are (the slice-2 pass; lead's words,
// owner-delegated 2026-11-02): a folder at the marker's name is something MailArchive did not
// write, to remove by hand — never a Delete… that refuses it; a lookup that fails at a folder that
// is there, with no line in this Desktop's log, is RED "could not check" (it said nothing); and
// with this Desktop's deletion log unreadable the row and the line say the log could not be read,
// never that it "has no record of it". Positive twins first: a regular marker with the log read
// gives the marker-only line; the lookup working, the archive says nothing of a delete.
func TestMA1049_StatusAndHealthSayWhatIsAtTheMarkersName(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Begun elsewhere")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}

	// Positive twins.
	if posture, summary := ma1049Health(t, e); posture == "RED" || strings.Contains(summary, "could not check") {
		t.Fatalf("positive twin: no marker: %s\n%s", posture, summary)
	}
	ma1048Plant(t, dir)
	if posture, summary := ma1049Health(t, e); posture != "RED" || !strings.Contains(summary, "this computer's deletion log has no record of it") {
		t.Fatalf("positive twin: a regular marker, the log read: %s\n%s", posture, summary)
	}

	// A folder at the marker's name.
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, delmark.Name), 0o755); err != nil {
		t.Fatal(err)
	}
	posture, summary := ma1049Health(t, e)
	if posture != "RED" || !strings.Contains(summary, "RED: this archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so nothing is backed up into it — if no delete of this archive is under way, remove it by hand.") ||
		strings.Contains(summary, "Delete…") {
		t.Fatalf("a folder at the marker's name: %s\n%s", posture, summary)
	}
	if err := os.Remove(filepath.Join(dir, delmark.Name)); err != nil {
		t.Fatal(err)
	}

	// The lookup fails at a folder that is there; this Desktop's log has no line.
	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	posture, summary = ma1049Health(t, e)
	restore()
	if posture != "RED" || !strings.Contains(summary, "RED: MailArchive could not check this archive's folder for a delete that did not finish (input/output error), so nothing is backed up or changed in it — check that the drive or folder can be read.") {
		t.Fatalf("a lookup that fails, no log line: %s\n%s", posture, summary)
	}

	// This Desktop's deletion log cannot be read; the marker is there.
	ma1048Plant(t, dir)
	lp := e.cfg.deletionsLogPath()
	os.Remove(lp)
	if err := os.Mkdir(lp, 0o700); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(lp)
	cause := "MailArchive's deletion log " + lp + " must be a regular file (not a symlink, pipe or device)"
	began := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC).Local().Format("2006-01-02 15:04 -07:00")
	posture, summary = ma1049Health(t, e)
	if posture != "RED" || !strings.Contains(summary, "RED: a delete of this archive begun "+began+" did not finish, and MailArchive could not read this computer's deletion log ("+cause+") — check that file, then click Delete… on its row in Your archives to finish it.") ||
		strings.Contains(summary, "has no record") {
		t.Fatalf("the log unreadable: %s\n%s", posture, summary)
	}
	row := html.UnescapeString(rowFor(t, e.page(), dir))
	if !strings.Contains(row, "Delete not finished: a delete of this archive begun "+began+" did not finish, and MailArchive could not read this computer's deletion log ("+cause+"). Check that file, then click Delete… to finish it.") ||
		strings.Contains(row, "has no record") {
		t.Fatalf("the row with the log unreadable: %s", row)
	}
}
