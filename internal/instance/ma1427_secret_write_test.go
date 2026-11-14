package instance

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"mail-archive-tool/internal/util"
)

// covers: MA-1427, R23
// Every instance file (the desktop's and a serve's; both hold a hand-off key K) is written
// by the secret writer, util.WriteSecretFileAtomic0600, which on Windows creates it with a
// Medium integrity label no lower integrity can read (the menu-bar S1 adversarial re-check
// r3, C1; what the label does is TestMA1427_ALowerIntegrityProcessCannotReadTheInstanceFile,
// on Windows). Checked on every OS, so the gating run sees the wiring. Positive twin first:
// what Write made is read back whole, 0600 on Unix.
func TestMA1427_TheInstanceFileIsWrittenAsASecret(t *testing.T) {
	path := filepath.Join(t.TempDir(), "acct", FileName)
	keyHex, _ := NewKey()
	if err := Write(path, Info{PID: 7, Dashboard: "127.0.0.1:1", Key: keyHex}); err != nil {
		t.Fatal(err)
	}
	got, err := Read(path)
	if err != nil || got.Key != keyHex || got.PID != 7 {
		t.Fatalf("twin: the instance file was not read back: %+v %v", got, err)
	}
	if fi, err := os.Stat(path); err != nil || (runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600) {
		t.Errorf("the instance file's mode: %v %v", fi, err)
	}
	if reflect.ValueOf(writeSecret).Pointer() != reflect.ValueOf(util.WriteSecretFileAtomic0600).Pointer() {
		t.Error("instance files must be written by util.WriteSecretFileAtomic0600, the writer that labels them on Windows (C1)")
	}
}
