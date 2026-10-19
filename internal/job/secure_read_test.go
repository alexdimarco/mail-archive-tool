//go:build unix

package job

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

// covers: MA-640, R12, S59
// Read is the secure job-file read a headless run relies on: a symbolic link
// (its target left untouched), a FIFO (caught before any open, so no hang), a
// group- or world-accessible file, and an oversize file are each refused, while
// a valid owner-only file the wizard wrote is accepted. Unix-gated: the
// link/FIFO/mode cases are POSIX. The refusal is what RunHeadless turns into a
// jobFail with no run log and app.Run never started (the headless failure path,
// MA-122/MA-123).
func TestMA640_ReadIsSecure(t *testing.T) {
	validJSON := []byte(`{"version":1,"out":"/tmp","mode":"incremental"}`)

	// Positive twin first: a file written by Write is accepted, so the refusals
	// below are not over-broad.
	good := filepath.Join(t.TempDir(), "good.json")
	if err := Write(good, Job{Name: "ok", Out: t.TempDir(), Mode: "incremental"}); err != nil {
		t.Fatalf("write good job: %v", err)
	}
	if _, err := Read(good); err != nil {
		t.Fatalf("Read must accept a valid owner-only job file: %v", err)
	}

	cases := []struct {
		name string
		mk   func(t *testing.T, dir string) string
		want string // a substring the refusal must name
	}{
		{"symlink", func(t *testing.T, dir string) string {
			target := filepath.Join(dir, "target.json")
			if err := os.WriteFile(target, validJSON, 0o600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(dir, "job.json")
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink unsupported here: %v", err)
			}
			return link
		}, "symbolic link"},
		{"fifo", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "job.json")
			if err := syscall.Mkfifo(p, 0o600); err != nil {
				t.Skipf("mkfifo unsupported here: %v", err)
			}
			return p
		}, "regular file"},
		{"group-accessible", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "job.json")
			if err := os.WriteFile(p, validJSON, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(p, 0o644); err != nil { // explicit, so umask cannot hide it
				t.Fatal(err)
			}
			return p
		}, "owner-only"},
		{"oversize", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "job.json")
			if err := os.WriteFile(p, make([]byte, maxJobFileSize+1), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, "limit"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir := t.TempDir()
			path := c.mk(t, dir)
			_, err := Read(path)
			if err == nil {
				t.Fatalf("Read(%s) must be refused, not accepted", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s: refusal should name %q; got %v", c.name, c.want, err)
			}
			if c.name == "symlink" {
				if b, rerr := os.ReadFile(filepath.Join(dir, "target.json")); rerr != nil || string(b) != string(validJSON) {
					t.Errorf("the symlink's target must be left untouched (read=%q err=%v)", b, rerr)
				}
			}
		})
	}
}
