package job

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-77, R18, S29
// The GUI job file round-trips the wizard's answers (inputs or auto, out,
// mode, since, copy-first, outlook, raw), is owner-only, ignores unknown fields
// (additive evolution), and refuses a file from a newer program or one with no
// output folder — so a headless run never guesses.
func TestJobFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mailarchive", "mailarchive-1234abcd.json")
	j := Job{Name: "mailarchive-1234abcd", Inputs: []string{"/x/a.pst"}, Auto: true, Out: "/archive", Mode: "full",
		Since: "30d", CopyFirst: true, Outlook: false, KeepRaw: true}
	if err := Write(path, j); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
			t.Errorf("job file mode %04o, want 0600", fi.Mode().Perm())
		}
	}
	got, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version || got.Out != "/archive" || got.Mode != "full" || !got.Auto || got.Since != "30d" || !got.CopyFirst || !got.KeepRaw || len(got.Inputs) != 1 {
		t.Errorf("round trip lost fields: %+v", got)
	}

	// Unknown fields are ignored; a missing mode defaults; no out is refused.
	extra := filepath.Join(t.TempDir(), "x.json")
	os.WriteFile(extra, []byte(`{"version":1,"out":"/o","future_field":true}`), 0o600)
	if got, err := Read(extra); err != nil || got.Mode != "incremental" {
		t.Errorf("unknown field / default mode: %+v %v", got, err)
	}
	os.WriteFile(extra, []byte(`{"version":1,"mode":"full"}`), 0o600)
	if _, err := Read(extra); err == nil || !strings.Contains(err.Error(), "output folder") {
		t.Errorf("job without out accepted: %v", err)
	}
	os.WriteFile(extra, []byte(`{"version":99,"out":"/o"}`), 0o600)
	if _, err := Read(extra); err == nil || !strings.Contains(err.Error(), "newer") {
		t.Errorf("newer job file accepted: %v", err)
	}
	if _, err := Read(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("missing job file accepted")
	}
}
