package delmark

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// covers: MA-1046, R36
// The marker's bytes (§15.1) are one JSON object of exactly the fixed fields — `what`, `version` 1,
// `run`, `began` in UTC to the second (a time zone and the fraction dropped) and the fixed `note` —
// under 512 bytes, and the bounded read gives back that run and began (Read, and ReadIn through a
// root already open on the folder). Positive twin first: a folder without the marker reads nothing.
func TestMA1046_TheMarkersBytesAreItsFixedFields(t *testing.T) {
	dir := t.TempDir()
	if d := Read(dir); d.Known {
		t.Fatalf("positive twin: no marker, nothing read: %+v", d)
	}
	began := time.Date(2026, 11, 2, 16, 4, 5, 987654321, time.FixedZone("CET", 3600))
	b := Content("0123456789abcdef", began)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	if strings.Join(keys, ",") != "began,note,run,version,what" || len(b) >= 512 ||
		m["what"] != What || m["version"] != float64(Version) || m["run"] != "0123456789abcdef" || m["began"] != "2026-11-02T15:04:05Z" || m["note"] != Note {
		t.Fatalf("%d bytes: %s", len(b), b)
	}
	if err := os.WriteFile(filepath.Join(dir, Name), b, 0o600); err != nil {
		t.Fatal(err)
	}
	if d := Read(dir); !d.Known || d.Run != "0123456789abcdef" || d.Began != "2026-11-02T15:04:05Z" {
		t.Fatalf("Read: %+v", d)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if d := ReadIn(root, dir); !d.Known || d.Run != "0123456789abcdef" || !d.BeganAt().Equal(began.Truncate(time.Second)) {
		t.Fatalf("ReadIn: %+v", d)
	}
}
