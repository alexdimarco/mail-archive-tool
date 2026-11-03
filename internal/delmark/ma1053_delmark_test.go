package delmark

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// marker is a well-formed marker's bytes, as §15.1 writes them.
const marker = `{"what":"MailArchive delete marker","version":1,"run":"0123456789abcdef","began":"2026-11-02T15:04:05Z","note":"x"}`

func plant(t *testing.T, dir, content string) string {
	t.Helper()
	p := filepath.Join(dir, Name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skip("no symbolic links here:", err)
	}
}

// covers: MA-1053, R36
// Presence is the signal (§15.1, §15.3; DM-20): a folder without the name has no
// marker, and a regular file at the name is a marker whatever it holds — empty, not
// JSON, unreadable by this account. A name that is not a regular file — a link to a
// file, to a folder or to nothing, a folder — is never followed and still counts,
// as one MailArchive did not write. A folder that is not there has no marker (MA-689's
// refusal speaks for it).
func TestPresenceIsTheSignal(t *testing.T) {
	// Positive twin first: no marker.
	dir := t.TempDir()
	if st, err := Present(dir); st != Absent || err != nil {
		t.Fatalf("an archive folder without the marker: %v %v", st, err)
	}
	if st, err := Present(filepath.Join(dir, "unplugged")); st != Absent || err != nil {
		t.Fatalf("a folder that is not there: %v %v", st, err)
	}
	if st, err := Present(""); st != Absent || err != nil {
		t.Fatalf("no folder: %v %v", st, err)
	}

	for label, content := range map[string]string{"well formed": marker, "empty": "", "not JSON": "{{{", "version 2": strings.Replace(marker, `"version":1`, `"version":2`, 1)} {
		d := t.TempDir()
		plant(t, d, content)
		if st, err := Present(d); st != Regular || err != nil {
			t.Fatalf("%s: %v %v", label, st, err)
		}
	}
	if runtime.GOOS != "windows" {
		d := t.TempDir()
		p := plant(t, d, marker)
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		if st, err := Present(d); st != Regular || err != nil {
			t.Fatalf("a marker this account cannot read: %v %v", st, err)
		}
	}

	elsewhere := t.TempDir()
	plant(t, elsewhere, marker)
	for label, target := range map[string]string{
		"a link to a marker elsewhere": filepath.Join(elsewhere, Name),
		"a link to a folder":           elsewhere,
		"a link to nothing":            filepath.Join(elsewhere, "nothing"),
	} {
		d := t.TempDir()
		symlinkOrSkip(t, target, filepath.Join(d, Name))
		if st, err := Present(d); st != NotRegular || err != nil {
			t.Fatalf("%s: %v %v", label, st, err)
		}
	}
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, Name), 0o700); err != nil {
		t.Fatal(err)
	}
	if st, err := Present(d); st != NotRegular || err != nil {
		t.Fatalf("a folder at the name: %v %v", st, err)
	}
}

// covers: MA-1053, R36
// Presence by goos (DM-20): on Windows an irregular entry that is not a folder and
// not a link (a OneDrive or HSM placeholder) is a marker; a link-like one is not
// regular; on macOS and Linux an irregular entry is not regular.
func TestRegularIsTheListersRule(t *testing.T) {
	notLink := func(string) (bool, error) { return false, nil }
	isLink := func(string) (bool, error) { return true, nil }
	cannotTell := func(string) (bool, error) { return false, errors.New("access denied") }
	for _, c := range []struct {
		goos   string
		mode   fs.FileMode
		link   func(string) (bool, error)
		want   State
		reason string
	}{
		{"linux", 0o600, notLink, Regular, "a regular file"},
		{"windows", 0o600, notLink, Regular, "a regular file on Windows"},
		{"windows", fs.ModeIrregular, notLink, Regular, "a cloud placeholder"},
		{"windows", fs.ModeIrregular, isLink, NotRegular, "a junction or other name surrogate"},
		{"windows", fs.ModeIrregular, cannotTell, NotRegular, "a reparse point whose tag cannot be read"},
		{"windows", fs.ModeIrregular | fs.ModeDir, notLink, NotRegular, "a folder placeholder"},
		{"linux", fs.ModeIrregular, notLink, NotRegular, "an irregular entry off Windows"},
		{"darwin", fs.ModeSymlink, notLink, NotRegular, "a symbolic link"},
		{"windows", fs.ModeSymlink, notLink, NotRegular, "a symbolic link on Windows"},
		{"linux", fs.ModeNamedPipe, notLink, NotRegular, "a pipe"},
		{"linux", fs.ModeDevice | fs.ModeCharDevice, notLink, NotRegular, "a device"},
		{"linux", fs.ModeDir, notLink, NotRegular, "a folder"},
	} {
		if got := stateOf(c.goos, "x", c.mode, c.link); got != c.want {
			t.Errorf("%s (%s): %v, want %v", c.reason, c.goos, got, c.want)
		}
	}
}

// covers: MA-1053, R36
// A lookup that fails for any reason but "not there" (DM-22, DM-23): when the folder
// itself cannot be reached (a dead drive, a dropped share: Windows' "device not ready"
// or "network name deleted"), there is no marker, so MA-689's archive-absent refusal
// speaks; when the folder is a reachable folder, the answer is not Absent and the
// error is typed, kind archive_unwritable, naming the folder and the cause, and its
// refusal words say what was not done and how to go on.
func TestALookupThatFailsIsRoutedByTheFolder(t *testing.T) {
	deviceNotReady := errors.New("The device is not ready.")
	nameDeleted := errors.New("The specified network name is no longer available.")
	was := lstat
	t.Cleanup(func() { lstat = was })

	// Positive twin first: the real lookup of a reachable folder answers.
	dir := t.TempDir()
	if st, err := Present(dir); st != Absent || err != nil {
		t.Fatalf("%v %v", st, err)
	}
	for _, cause := range []error{deviceNotReady, nameDeleted} {
		lstat = func(string) (fs.FileInfo, error) { return nil, &fs.PathError{Op: "lstat", Path: "x", Err: cause} }
		// The drive is gone: the folder cannot be reached either.
		if st, err := Present(filepath.Join(dir, "on-the-unplugged-drive")); st != Absent || err != nil {
			t.Fatalf("%v, folder unreachable: %v %v", cause, st, err)
		}
		// The folder is there, the marker's lookup still fails.
		st, err := Present(dir)
		var le *LookupError
		if st == Absent || !errors.As(err, &le) || le.Kind() != KindCouldNotCheck || le.Folder != dir || !errors.Is(err, cause) {
			t.Fatalf("%v, folder reachable: %v %v", cause, st, err)
		}
		if got, want := le.Refusal(WhatNotBackedUp), "MailArchive could not check this archive's folder for a delete that did not finish ("+cause.Error()+"), so it was not backed up; nothing was changed. Check that the drive or folder can be read, then try again."; got != want {
			t.Fatalf("words:\n got  %q\n want %q", got, want)
		}
	}
	lstat = was
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		// A folder that cannot be searched (0000), a real permission error.
		d := t.TempDir()
		plant(t, d, marker)
		if err := os.Chmod(d, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(d, 0o700) })
		st, err := Present(d)
		var le *LookupError
		if st == Absent || !errors.As(err, &le) || !errors.Is(err, fs.ErrPermission) {
			t.Fatalf("a folder that cannot be searched: %v %v", st, err)
		}
	}
}

// covers: MA-1053, R36
// The bounded read, for words only (§15.3, DM-8): a well-formed marker gives its run
// id and when it began, as written; anything else — empty, not JSON, not one object,
// another version, a run or began of the wrong shape (a time zone, control characters,
// a bidi override), over 4 KiB, unreadable — is a marker whose details are not known,
// and nothing of it is kept. A link is never followed, even to a well-formed marker.
func TestTheBoundedReadGivesOnlyTheRunAndWhenItBegan(t *testing.T) {
	// Positive twin first.
	d := t.TempDir()
	plant(t, d, marker)
	got := Read(d)
	if !got.Known || got.Run != "0123456789abcdef" || got.Began != "2026-11-02T15:04:05Z" {
		t.Fatalf("a well-formed marker: %+v", got)
	}
	if want := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC); !got.BeganAt().Equal(want) {
		t.Fatalf("began at %v", got.BeganAt())
	}
	// Extra fields of the same version do not hide the details.
	d = t.TempDir()
	plant(t, d, strings.Replace(marker, `"note":"x"`, `"note":"x","later":true`, 1))
	if !Read(d).Known {
		t.Fatal("a field this version does not use hides the details")
	}

	unknown := map[string]string{
		"empty":               "",
		"not JSON":            "not json",
		"an array":            `[` + marker + `]`,
		"two objects":         marker + marker,
		"version 2":           strings.Replace(marker, `"version":1`, `"version":2`, 1),
		"version a string":    strings.Replace(marker, `"version":1`, `"version":"1"`, 1),
		"version 1.0":         strings.Replace(marker, `"version":1`, `"version":1.0`, 1),
		"a byte-order mark":   "\xef\xbb\xbf" + marker,
		"no version":          strings.Replace(marker, `"version":1,`, ``, 1),
		"run too short":       strings.Replace(marker, `0123456789abcdef`, `0123456789abcde`, 1),
		"run upper case":      strings.Replace(marker, `0123456789abcdef`, `0123456789ABCDEF`, 1),
		"run not hex":         strings.Replace(marker, `0123456789abcdef`, `0123456789abcdeg`, 1),
		"run missing":         strings.Replace(marker, `"run":"0123456789abcdef",`, ``, 1),
		"began with a zone":   strings.Replace(marker, `2026-11-02T15:04:05Z`, `2026-11-02T15:04:05+01:00`, 1),
		"began with nanos":    strings.Replace(marker, `2026-11-02T15:04:05Z`, `2026-11-02T15:04:05.5Z`, 1),
		"began not a time":    strings.Replace(marker, `2026-11-02T15:04:05Z`, `yesterday`, 1),
		"began a bad date":    strings.Replace(marker, `2026-11-02T15:04:05Z`, `2026-13-02T15:04:05Z`, 1),
		"began controls":      strings.Replace(marker, `2026-11-02T15:04:05Z`, `2026-11-02T15:04:05Z\u001b[31m`, 1),
		"began bidi":          strings.Replace(marker, `2026-11-02T15:04:05Z`, `‮2026-11-02T15:04:05Z`, 1),
		"began missing":       strings.Replace(marker, `"began":"2026-11-02T15:04:05Z",`, ``, 1),
		"over the read's cap": marker[:len(marker)-1] + `,"pad":"` + strings.Repeat("a", MaxRead) + `"}`,
	}
	for label, content := range unknown {
		d := t.TempDir()
		plant(t, d, content)
		if got := Read(d); got.Known || got.Run != "" || got.Began != "" {
			t.Errorf("%s: %+v", label, got)
		}
	}
	// 64 MiB: not known, and read no further than the cap (see the bounded reader's test).
	d = t.TempDir()
	f, err := os.Create(filepath.Join(d, Name))
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(marker)
	if err := f.Truncate(64 << 20); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if got := Read(d); got.Known {
		t.Fatalf("a 64 MiB marker: %+v", got)
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		d := t.TempDir()
		if err := os.Chmod(plant(t, d, marker), 0); err != nil {
			t.Fatal(err)
		}
		if got := Read(d); got.Known {
			t.Fatalf("an unreadable marker: %+v", got)
		}
	}
	// A link at the name, to a well-formed marker: never followed.
	elsewhere := t.TempDir()
	plant(t, elsewhere, marker)
	d = t.TempDir()
	symlinkOrSkip(t, filepath.Join(elsewhere, Name), filepath.Join(d, Name))
	if got := Read(d); got.Known {
		t.Fatalf("a link to a marker was followed: %+v", got)
	}
	// No marker, no folder: nothing known.
	if got := Read(t.TempDir()); got.Known {
		t.Fatalf("no marker: %+v", got)
	}
	if got := Read(filepath.Join(t.TempDir(), "gone")); got.Known {
		t.Fatalf("no folder: %+v", got)
	}
}

// endless is a reader that never ends, counting what it gave.
type endless struct{ n int64 }

func (e *endless) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = ' '
	}
	e.n += int64(len(p))
	return len(p), nil
}

// covers: MA-1053, R36
// The read is bounded (§15.3: at most 4 KiB): a marker that never ends is read no
// further than the cap and one byte, and refused as over it. Positive twin first: a
// marker of exactly the cap is read whole.
func TestTheReadStopsAtItsCap(t *testing.T) {
	exact := strings.Repeat(" ", MaxRead)
	if data, ok := readBounded(strings.NewReader(exact)); !ok || len(data) != MaxRead {
		t.Fatalf("exactly the cap: %d %v", len(data), ok)
	}
	src := &endless{}
	done := make(chan bool, 1)
	go func() {
		_, ok := readBounded(src)
		done <- ok
	}()
	select {
	case ok := <-done:
		if ok {
			t.Fatal("a marker over the cap was accepted")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read did not stop at its cap")
	}
	if src.n > 2*MaxRead+512 {
		t.Fatalf("read %d bytes for a cap of %d", src.n, MaxRead)
	}
}
