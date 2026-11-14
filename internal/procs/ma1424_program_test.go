package procs

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-1424, R52
// What a recorded pid runs now is read from the system, never from the record (the S1 fix
// round 3, C3: `quit -end-older` ends only a process proven to be MailArchive). Positive
// twin first: this test process's program is its own executable. Then a real process —
// a copy of a long-running system program (sleep; on Windows PING.EXE) named like
// MailArchive — is read under that name, and the system program itself under its own; a
// process that has ended has no program to read.
func TestMA1424_TheProgramOfAProcessIsReadFromTheSystem(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("this system does not say which program a process runs; Program answers an error, which the quit verb treats as cannot tell")
	}
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Program(os.Getpid())
	if err != nil || !sameFile(got, self) {
		t.Fatalf("twin: this process's program read as %q (%v), want %q", got, err, self)
	}

	src, args, name := "/bin/sleep", []string{"30"}, "mailarchive"
	if runtime.GOOS == "windows" {
		src = filepath.Join(os.Getenv("SystemRoot"), "System32", "PING.EXE")
		args, name = []string{"-n", "30", "127.0.0.1"}, "mailarchivew.exe"
	}
	copyPath := filepath.Join(t.TempDir(), name)
	copyFile(t, src, copyPath)
	for _, c := range []struct{ path, want string }{{copyPath, name}, {src, filepath.Base(src)}} {
		cmd := exec.Command(c.path, args...)
		if err := cmd.Start(); err != nil {
			t.Fatalf("starting %s: %v", c.path, err)
		}
		pid := cmd.Process.Pid
		got, err := Program(pid)
		if err != nil || !strings.EqualFold(filepath.Base(got), c.want) {
			t.Errorf("the program of process %d (%s) read as %q (%v), want one named %s", pid, c.path, got, err, c.want)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if got, err := Program(pid); err == nil && got != "" && sameFile(got, c.path) {
			t.Errorf("process %d has ended, yet its program read as %q", pid, got)
		}
	}
}

func sameFile(a, b string) bool {
	fa, ea := os.Stat(a)
	fb, eb := os.Stat(b)
	return ea == nil && eb == nil && os.SameFile(fa, fb)
}

func copyFile(t *testing.T, src, dst string) {
	t.Helper()
	in, err := os.Open(src)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
