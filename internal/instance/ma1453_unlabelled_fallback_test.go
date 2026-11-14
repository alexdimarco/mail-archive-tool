package instance

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/util"
)

// refusing stands in for util.WriteSecretFileAtomic0600 failing as Windows makes it fail
// (createSecretTemp's "createsecret" error), with code the Windows error number under it and
// nothing written.
func refusing(code syscall.Errno) func(string, []byte) error {
	return func(path string, _ []byte) error {
		return fmt.Errorf("temp file in %s: %w", filepath.Dir(path), &os.PathError{Op: "createsecret", Path: filepath.Join(filepath.Dir(path), ".tmp-0123456789abcdef"), Err: code})
	}
}

// privilegeNotHeld is ERROR_PRIVILEGE_NOT_HELD, what Windows answers a label above what the
// writer may set (seen in MA-1453's Windows test); sharingViolation (32) is not about the label.
const (
	privilegeNotHeld = syscall.Errno(1314)
	sharingViolation = syscall.Errno(32)
)

// covers: MA-1453, R23
// The research note's L0: whether a person's own (Medium) program may set the Medium no-read-up
// label is not settled for every account, file system and network folder. When Windows refuses
// the label, the instance file is written exactly as 1.7.1 wrote it (owner-only, unlabelled, 0600
// on Unix), records that it was (Unlabelled), and the writer is told why in one plain sentence;
// MailArchive Desktop then starts as it did in 1.7.1, never worse. Only when that plain write fails
// too is it an error (a KeyFileError naming the plain write's error), and no file is left.
// Positive twin first: with the label written, nothing is said and nothing is recorded.
func TestMA1453_AnInstanceFileWindowsWillNotLabelIsWrittenAs171Did(t *testing.T) {
	dir := t.TempDir()
	keyHex, _ := NewKey()
	info := Info{PID: 7, Dashboard: "127.0.0.1:1", Key: keyHex}
	var notes []string
	note := func(s string) { notes = append(notes, s) }

	labelled := filepath.Join(dir, "labelled", FileName)
	if ok, err := WriteNoting(labelled, info, note); err != nil || !ok || len(notes) != 0 {
		t.Fatalf("positive twin: the labelled write gave %v %v and said %q", ok, err, notes)
	}
	if got, _ := Read(labelled); got.Unlabelled {
		t.Fatal("positive twin: a labelled file records itself unlabelled")
	}

	restore := SetWritersForTest(refusing(privilegeNotHeld), util.WriteFileAtomic0600)
	defer restore()
	path := filepath.Join(dir, "refused", FileName)
	if ok, err := WriteNoting(path, info, note); err != nil || ok {
		t.Fatalf("a refused label gave labelled=%v, %v; want the file written without it", ok, err)
	}
	got, err := Read(path)
	if err != nil || got.Key != keyHex || !got.Unlabelled {
		t.Fatalf("the file written without the label: %+v %v, want a usable file that records it", got, err)
	}
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the file written without the label: %v %v, want owner-only", fi, err)
	}
	want := "warning: Windows would not protect MailArchive's private key file in " + filepath.Join(dir, "refused") + " (" + strings.TrimSuffix(privilegeNotHeld.Error(), ".") + "), so it was saved as MailArchive 1.7.1 saved it: everything works as usual, but a sandboxed program running under your account may be able to read it."
	if len(notes) != 1 || notes[0] != want {
		t.Fatalf("the writer was told %q, want %q", notes, want)
	}

	restore()
	restore = SetWritersForTest(refusing(privilegeNotHeld), func(string, []byte) error { return errors.New("Access is denied.") })
	nowhere := filepath.Join(dir, "nowhere", FileName)
	_, err = WriteNoting(nowhere, info, note)
	var kf *KeyFileError
	rc := 0
	if errors.As(err, &kf) && kf.Dir == filepath.Join(dir, "nowhere") && kf.Plain.Error() == "Access is denied." {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("could not save the private key file in", "Access is denied."),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(nowhere)
			return errors.Is(serr, os.ErrNotExist) && len(notes) == 1
		}))
}

// covers: MA-1458, R23
// The fallback is visible (the final check's security-1): the instance file records it
// (Unlabelled), and UnlabelledIn, which `status` and Status & health ask, says so while the
// Desktop that wrote it runs. Positive twin first: a labelled file says nothing; a file whose
// writer is gone says nothing either.
func TestMA1458_TheFallbackIsReadOffTheRunningDesktopsFile(t *testing.T) {
	dir := t.TempDir()
	keyHex, _ := NewKey()
	if _, err := WriteNoting(FilePathIn(dir, ""), Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex}, nil); err != nil || UnlabelledIn(dir, "") {
		t.Fatalf("positive twin: a labelled file of a live Desktop reads unlabelled (%v)", err)
	}
	restore := SetWritersForTest(refusing(privilegeNotHeld), util.WriteFileAtomic0600)
	defer restore()
	if _, err := WriteNoting(FilePathIn(dir, ""), Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex}, nil); err != nil || !UnlabelledIn(dir, "") {
		t.Fatalf("the fallback's file of a live Desktop does not read unlabelled (%v)", err)
	}
	gone := filepath.Join(dir, "gone")
	if _, err := WriteNoting(FilePathIn(gone, ""), Info{PID: 999999999, Dashboard: "127.0.0.1:1", Key: keyHex}, nil); err != nil || UnlabelledIn(gone, "") {
		t.Fatalf("the fallback's file of a Desktop no longer running reads unlabelled (%v)", err)
	}
}

// covers: MA-1459, R23
// The final check's security-3: only Windows refusing the label itself (ERROR_PRIVILEGE_NOT_HELD,
// or what a file system without labels answers) costs the protection at once. Any other failure
// of the protected write (a sharing violation from a scanner holding the new file, a full disk) is
// asked about once more first, so a passing fault leaves the file protected; when it stays and the
// plain write works, the folder can be written and only the protected write cannot, so the file is
// written as 1.7.1 did (never worse than 1.7.1). And while MailArchive Desktop runs, a file the
// fallback wrote is written with its label again at each Watch interval until Windows allows it.
// Positive twin first: util.LabelRefused knows the label's own codes and nothing else.
func TestMA1459_OnlyARefusedLabelCostsTheProtectionAndWatchTriesAgain(t *testing.T) {
	for code, want := range map[syscall.Errno]bool{1314: true, 50: true, 87: true, 32: false, 112: false, 5: false} {
		if util.LabelRefused(code) != want {
			t.Fatalf("positive twin: util.LabelRefused(%d) is %v, want %v", code, !want, want)
		}
	}
	dir := t.TempDir()
	keyHex, _ := NewKey()
	info := Info{PID: 7, Dashboard: "127.0.0.1:1", Key: keyHex}

	asks := 0
	once := func(path string, data []byte) error {
		asks++
		if asks == 1 {
			return refusing(sharingViolation)(path, data)
		}
		return util.WriteSecretFileAtomic0600(path, data)
	}
	restore := SetWritersForTest(once, util.WriteFileAtomic0600)
	defer restore()
	var notes []string
	passing := filepath.Join(dir, "passing", FileName)
	if ok, err := WriteNoting(passing, info, func(s string) { notes = append(notes, s) }); err != nil || !ok || asks != 2 || len(notes) != 0 {
		t.Fatalf("a passing fault gave labelled=%v, %v after %d asks, said %q; want the protected write asked again and done", ok, err, asks, notes)
	}
	if got, _ := Read(passing); got.Unlabelled {
		t.Fatal("a file the second protected write made records itself unlabelled")
	}

	restore()
	restore = SetWritersForTest(refusing(sharingViolation), util.WriteFileAtomic0600)
	stays := filepath.Join(dir, "stays", FileName)
	if ok, err := WriteNoting(stays, info, func(s string) { notes = append(notes, s) }); err != nil || ok || len(notes) != 1 {
		t.Fatalf("a fault that stays, the plain write working, gave labelled=%v, %v, said %q; want the 1.7.1 file and the warning", ok, err, notes)
	}

	// Watch: a holder whose file the fallback wrote tries the label again and records it.
	restore()
	var mu sync.Mutex
	refuse := true
	toggled := func(path string, data []byte) error {
		mu.Lock()
		r := refuse
		mu.Unlock()
		if r {
			return refusing(privilegeNotHeld)(path, data)
		}
		return util.WriteSecretFileAtomic0600(path, data)
	}
	restore = SetWritersForTest(toggled, util.WriteFileAtomic0600)
	h, err := Acquire(filepath.Join(dir, "state"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	if err := h.Publish(info); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(h.FilePath()); !got.Unlabelled {
		t.Fatal("premise: the published file is not the fallback's")
	}
	var logged []string
	var lmu sync.Mutex
	stop := h.Watch(10*time.Millisecond, func(s string) { lmu.Lock(); logged = append(logged, s); lmu.Unlock() })
	defer stop()
	time.Sleep(50 * time.Millisecond)
	if got, _ := Read(h.FilePath()); !got.Unlabelled {
		t.Fatal("Watch replaced the fallback's file while Windows still refused the label")
	}
	mu.Lock()
	refuse = false
	mu.Unlock()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _ := Read(h.FilePath()); !got.Unlabelled && got.Key == keyHex {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Watch never wrote the file with its label once Windows allowed it")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	lmu.Lock()
	defer lmu.Unlock()
	if strings.Join(logged, "\n") != "the private key file is protected now" {
		t.Fatalf("Watch logged %q", logged)
	}
}
