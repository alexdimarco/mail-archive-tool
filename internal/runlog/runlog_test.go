package runlog

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-93, R14, S28
// The run log appends across runs with a header per run, and is rotated to
// <path>.1 when it exceeds the cap — so an unattended daily job never grows a
// file without bound.
func TestRunLogAppendsAndRotates(t *testing.T) {
	old := MaxSize
	MaxSize = 200
	t.Cleanup(func() { MaxSize = old })

	path := filepath.Join(t.TempDir(), "backup.log")
	f, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("first run body\n")
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("second run body\n")
	f.Close()
	data, _ := os.ReadFile(path)
	if strings.Count(string(data), "==== run started") != 2 || !strings.Contains(string(data), "first run body") {
		t.Errorf("log did not append two runs with headers:\n%s", data)
	}

	// Push it past the cap, then the next open rotates.
	f, _ = Open(path)
	f.WriteString(strings.Repeat("x", 300))
	f.Close()
	f, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString("after rotation\n")
	f.Close()
	rotated, err := os.ReadFile(path + ".1")
	if err != nil || !strings.Contains(string(rotated), "first run body") {
		t.Errorf("previous log not kept as .1: %v", err)
	}
	fresh, _ := os.ReadFile(path)
	if strings.Contains(string(fresh), "first run body") || !strings.Contains(string(fresh), "after rotation") {
		t.Errorf("log not rotated:\n%s", fresh)
	}
}

// covers: MA-93, R14, R4, S28
// A planted symlink at the log path (or at the rotation target) is refused,
// never written through.
func TestRunLogRefusesSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(dir, "victim")
	os.WriteFile(victim, []byte("precious"), 0o600)
	link := filepath.Join(dir, "job.log")
	if err := os.Symlink(victim, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if f, err := Open(link); err == nil {
		f.Close()
		t.Fatal("opened a log through a symlink")
	} else if !strings.Contains(err.Error(), "not a regular file") {
		t.Errorf("refusal must say why: %v", err)
	}
	if data, _ := os.ReadFile(victim); string(data) != "precious" {
		t.Errorf("symlink target modified: %q", data)
	}
}
