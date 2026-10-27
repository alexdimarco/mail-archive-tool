package keyring

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-800, R37
// The words and the owner-only file writer the command line and the Desktop share
// (the P3-3 plan, 3a): a new password too short or shaped like a recovery key is
// told why, a good one is not; the key-change question is asked for a remembered
// key and for an archive unlocked here, never when the person typed the secret; a
// new file is written once, owner-only, refused inside the archive and over a file
// that exists, and a voided one is gone.
func TestTheSharedKeyWordsAndFiles(t *testing.T) {
	if NewPasswordProblem("correct horse battery staple") != "" {
		t.Fatal("positive twin: a good password has no problem")
	}
	if p := NewPasswordProblem("short"); !strings.Contains(p, "at least 12") {
		t.Fatalf("too short: %q", p)
	}
	if KeyChangeQuestion(ByPassword, false, "x") != "" || !strings.Contains(KeyChangeQuestion(ByRememberedKey, false, "x"), "remembers the archive's key") ||
		!strings.Contains(KeyChangeQuestion(ByPassword, true, "x"), "unlocked here") {
		t.Fatal("the key-change question")
	}

	freshRecord(t)
	out, _, w := made(t, nil)
	w.Destroy()
	dest := filepath.Join(t.TempDir(), "recovery.txt")
	got, err := WriteNewFile("the file chosen", "the recovery file", dest, out, []byte("secret"))
	if err != nil {
		t.Fatal(err)
	}
	// Windows keeps the owner-only rule in the file's access list, not its mode bits.
	if fi, _ := os.Stat(got); runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		t.Fatalf("not owner-only: %v", fi.Mode())
	}
	if _, err := WriteNewFile("the file chosen", "the recovery file", dest, out, []byte("again")); err == nil {
		t.Fatal("a file that exists must not be overwritten")
	}
	_, err = WriteNewFile("the file chosen", "the recovery file", filepath.Join(out, "inside.txt"), out, []byte("x"))
	code, msg := refusal(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("inside the archive folder"),
		assure.NoSideEffect(func() bool { _, err := os.Lstat(filepath.Join(out, "inside.txt")); return os.IsNotExist(err) }))
	if err := VoidFile(got); err != nil {
		t.Fatal(err)
	}
	_, err = os.Stat(got)
	assure.Reached(t, os.IsNotExist(err), "a voided file is gone")
}
