package desktop

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/delmark"
)

// covers: MA-1049, MA-1027, R36
// A failed lookup of the delete marker does not hide what this Desktop's deletion log knows (the
// slice-2 adversarial pass's LOW-3): with the marker's lookup failing at a folder that is there
// (an I/O error), Status & health still says MA-1027's RED "a delete of this archive did not
// finish — click Delete… on its row in Your archives to finish it" from the log's started line.
// Positive twin first: the same log line with the lookup working says it.
func TestMA1049_AFailedMarkerLookupKeepsTheLogsLineInHealth(t *testing.T) {
	e := newDeleteEnv(t)
	dir := e.plainArchive("Half Deleted")
	if err := SaveSettings(e.cfg.SettingsPath, Settings{Out: dir}); err != nil {
		t.Fatal(err)
	}
	health := func() (string, string) {
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
	line, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "event": "started", "run": "run0000000001049", "folder": dir, "row": dir, "kind": "plain"})
	f, err := os.OpenFile(e.cfg.deletionsLogPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
	const want = "a delete of this archive did not finish — click Delete… on its row in Your archives to finish it."

	// Positive twin.
	if posture, summary := health(); posture != "RED" || !strings.Contains(summary, want) {
		t.Fatalf("positive twin: %s\n%s", posture, summary)
	}

	restore := delmark.SetLookupForTest(func(name string) (fs.FileInfo, error) {
		return nil, &fs.PathError{Op: "lstat", Path: name, Err: errors.New("input/output error")}
	})
	defer restore()
	if posture, summary := health(); posture != "RED" || !strings.Contains(summary, want) {
		t.Fatalf("a failed marker lookup hid the log's line: %s\n%s", posture, summary)
	}
}
