package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// labelRefused stands in for Windows refusing the instance file's label: the error
// util.WriteSecretFileAtomic0600 returns then (createSecretTemp's "createsecret"), with nothing
// written.
func labelRefused(path string, _ []byte) error {
	err := &os.PathError{Op: "createsecret", Path: filepath.Join(filepath.Dir(path), ".tmp-0123456789abcdef"), Err: errors.New("A required privilege is not held by the client.")}
	return fmt.Errorf("temp file in %s: %w", filepath.Dir(path), err)
}

// covers: MA-1453, R23
// The research note's L0: whether a person's own (Medium) program may set the Medium no-read-up
// label is not settled for every account, file system and network folder. When Windows will not set
// it, the instance file is written exactly as 1.7.1 wrote it (owner-only, unlabelled, 0600 on Unix)
// and the writer is told why in one plain sentence; MailArchive Desktop then starts as it did in
// 1.7.1, never worse. Only when that plain write fails too is it an error, still the label's
// refusal (util.SecretCreateRefused), and no file is left. Positive twin first: with the label
// written, nothing is said.
func TestMA1453_AnInstanceFileWindowsWillNotLabelIsWrittenAs171Did(t *testing.T) {
	dir := t.TempDir()
	keyHex, _ := NewKey()
	info := Info{PID: 7, Dashboard: "127.0.0.1:1", Key: keyHex}
	var notes []string
	note := func(s string) { notes = append(notes, s) }

	labelled := filepath.Join(dir, "labelled", FileName)
	if err := WriteNoting(labelled, info, note); err != nil || len(notes) != 0 {
		t.Fatalf("positive twin: the labelled write gave %v and said %q", err, notes)
	}

	restore := SetWritersForTest(labelRefused, util.WriteFileAtomic0600)
	defer restore()
	path := filepath.Join(dir, "refused", FileName)
	if err := WriteNoting(path, info, note); err != nil {
		t.Fatalf("a refused label failed the write: %v", err)
	}
	got, err := Read(path)
	if err != nil || got.Key != keyHex {
		t.Fatalf("the file written without the label is not a usable instance file: %+v %v", got, err)
	}
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the file written without the label: %v %v, want owner-only", fi, err)
	}
	want := "warning: Windows would not protect MailArchive Desktop's private start file in " + filepath.Join(dir, "refused") + " (A required privilege is not held by the client.), so it was written as before 1.7.2: a sandboxed program running under your account may be able to read it."
	if len(notes) != 1 || notes[0] != want {
		t.Fatalf("the writer was told %q, want %q", notes, want)
	}

	restore()
	restore = SetWritersForTest(labelRefused, func(string, []byte) error { return errors.New("the folder is read-only") })
	nowhere := filepath.Join(dir, "nowhere", FileName)
	err = WriteNoting(nowhere, info, note)
	rc := 0
	if _, _, refused := util.SecretCreateRefused(err); refused && err != nil {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("createsecret", "written without the label it failed too", "the folder is read-only"),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(nowhere)
			return errors.Is(serr, os.ErrNotExist) && len(notes) == 1
		}))
}
