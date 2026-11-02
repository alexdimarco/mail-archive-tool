package desktop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// covers: MA-1027, R36, R18
// The Desktop's Status & health says when a delete of the archive did not finish (the 1.5.3
// friction review's B2): RED, "a delete of this archive did not finish — click Delete… on its
// row in Your archives to finish it", read from this Desktop's deletions.log. Positive twin first:
// with no such delete, it says nothing of one.
func TestStatusAndHealthSaysADeleteDidNotFinish(t *testing.T) {
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
	if _, summary := health(); strings.Contains(summary, "did not finish") {
		t.Fatalf("the positive twin says a delete did not finish:\n%s", summary)
	}

	line, _ := json.Marshal(map[string]any{"at": time.Now().UTC().Format(time.RFC3339Nano), "event": "started", "run": "run0000000000001", "folder": dir, "row": dir, "kind": "plain"})
	f, err := os.OpenFile(e.cfg.deletionsLogPath(), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.Write(append(line, '\n'))
	f.Close()
	posture, summary := health()
	if posture != "RED" || !strings.Contains(summary, "a delete of this archive did not finish — click Delete… on its row in Your archives to finish it") ||
		strings.Contains(summary, "no schedule is recorded") || strings.Contains(summary, "mailarchive delete-archive") {
		t.Fatalf("a delete not finished: %s\n%s", posture, summary)
	}
}
