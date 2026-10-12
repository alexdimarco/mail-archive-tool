package util

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-322, R25, R10
// OpenRegularFile reads a regular file and refuses, without blocking, what a mail
// store must never have read: a pipe, a directory and (Unix) a link. The positive
// twin first: a regular file reads. A pipe that would block a plain os.Open is
// refused within a second.
func TestOpenRegularFileRefusesWhatIsNotAFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "msg")
	if err := os.WriteFile(good, []byte("From: a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := OpenRegularFile(good)
	if err != nil {
		t.Fatalf("a regular file: %v", err)
	}
	b, _ := io.ReadAll(f)
	f.Close()
	if string(b) != "From: a\n" {
		t.Fatalf("read %q", b)
	}

	if _, err := OpenRegularFile(dir); err == nil {
		t.Error("a directory was opened as a file")
	} else {
		var nr *NotRegularError
		if !errors.As(err, &nr) {
			t.Errorf("directory: %v; want a NotRegularError", err)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	pipe := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(pipe, 0o644); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	done := make(chan error, 1)
	go func() {
		f, err := OpenRegularFile(pipe)
		if f != nil {
			f.Close()
		}
		done <- err
	}()
	select {
	case err := <-done:
		var nr *NotRegularError
		assure.Refused(t, 1, errString(err), assure.Code(1), assure.Names("pipe"))
		if !errors.As(err, &nr) {
			t.Errorf("pipe: %v; want a NotRegularError", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("OpenRegularFile blocked on a pipe")
	}

	link := filepath.Join(dir, "link")
	if err := os.Symlink(good, link); err != nil {
		t.Fatal(err)
	}
	if f, err := OpenRegularFile(link); err == nil {
		f.Close()
		t.Error("a link was followed")
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// covers: MA-322, R25
// OpenRegularFileIn never leaves its root: a link inside the root that points out of
// it is refused (os.Root), a file inside is read.
func TestOpenRegularFileInStaysInTheRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("links need privileges on Windows")
	}
	parent := t.TempDir()
	root := filepath.Join(parent, "store")
	if err := os.MkdirAll(filepath.Join(root, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cur", "in"), []byte("in"), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(parent, "secret")
	if err := os.WriteFile(secret, []byte("TOP-SECRET"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(secret, filepath.Join(root, "cur", "out")); err != nil {
		t.Fatal(err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	f, err := OpenRegularFileIn(r, "cur/in")
	if err != nil {
		t.Fatalf("inside: %v", err)
	}
	f.Close()
	if f, err := OpenRegularFileIn(r, "cur/out"); err == nil {
		f.Close()
		t.Error("a link out of the root was opened")
	}
}

// covers: MA-322, R25, R10
// ReadDirIn lists a directory of the root in name order, and a pipe where a directory
// was expected is refused without blocking.
func TestReadDirInListsInOrderAndRefusesAPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("FIFOs need privileges on Windows")
	}
	dir := t.TempDir()
	for _, n := range []string{"b", "c", "a"} {
		if err := os.WriteFile(filepath.Join(dir, n), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := syscall.Mkfifo(filepath.Join(dir, "pipe"), 0o644); err != nil {
		t.Skip("no FIFOs here:", err)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	ents, err := ReadDirIn(r, ".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range ents {
		names = append(names, e.Name())
	}
	if got := assure.Reached(t, names, "the entries"); len(got) != 4 || got[0] != "a" || got[1] != "b" || got[2] != "c" || got[3] != "pipe" {
		t.Errorf("entries %v; want a b c pipe in order", got)
	}
	done := make(chan error, 1)
	go func() {
		_, err := ReadDirIn(r, "pipe")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a pipe was listed as a directory")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadDirIn blocked on a pipe")
	}
}

// covers: MA-322, R25, R10
// The Windows rule of the descriptor check (FC1), pinned on any OS: a regular file is
// readable everywhere; a reparse file that is not a directory (a cloud placeholder,
// ModeIrregular) is readable only on Windows; pipes, sockets, devices and
// directories never are.
func TestDescriptorReadableTable(t *testing.T) {
	cases := []struct {
		name string
		goos string
		mode fs.FileMode
		want bool
	}{
		{"regular, unix", "linux", 0o644, true},
		{"regular, windows", "windows", 0o644, true},
		{"placeholder, windows", "windows", fs.ModeIrregular, true},
		{"placeholder, unix", "linux", fs.ModeIrregular, false},
		{"pipe, windows", "windows", fs.ModeNamedPipe, false},
		{"pipe, unix", "linux", fs.ModeNamedPipe, false},
		{"socket", "linux", fs.ModeSocket, false},
		{"device", "windows", fs.ModeDevice, false},
		{"char device", "linux", fs.ModeDevice | fs.ModeCharDevice, false},
		{"directory", "windows", fs.ModeDir, false},
		{"irregular directory", "windows", fs.ModeDir | fs.ModeIrregular, false},
	}
	for _, c := range cases {
		if got := DescriptorReadable(c.goos, c.mode); got != c.want {
			t.Errorf("%s: DescriptorReadable(%s, %v) = %v, want %v", c.name, c.goos, c.mode, got, c.want)
		}
	}
}
