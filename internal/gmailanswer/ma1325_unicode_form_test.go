package gmailanswer

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-1325, R45
// The answer file's name folds the folder's Unicode normal form where the file system does (the B3
// adversarial pass, round 2's G7, turned from its probe TestB3AdvR2AnswerKeyUnicodeForm): macOS
// treats "Archivé" typed in its composed form (NFC, a terminal) and its decomposed form (NFD, some
// dialogs) as one folder, so the key does too, and a Not now kept under one spelling is found under
// the other. Positive twin first: on Linux and Windows, whose file systems tell the two spellings
// apart, the key does too.
func TestTheAnswerKeyFoldsTheUnicodeFormOnMacOS(t *testing.T) {
	const nfcName, nfdName = "Archivé", "Archivé"
	base := filepath.Join(t.TempDir(), "absent")
	for _, goos := range []string{"linux", "windows"} {
		if folderKey(filepath.Join(base, nfcName), goos) == folderKey(filepath.Join(base, nfdName), goos) {
			t.Fatalf("positive twin: on %s the two spellings give one key", goos)
		}
	}
	if folderKey(filepath.Join(base, nfcName), "darwin") != folderKey(filepath.Join(base, nfdName), "darwin") {
		t.Fatal("on macOS the composed and decomposed spellings of one folder give two keys")
	}

	if runtime.GOOS != "darwin" {
		return // the same folder under both spellings is macOS's
	}
	home := isolate(t)
	nfc, nfd := filepath.Join(home, nfcName), filepath.Join(home, nfdName)
	id := backedUp(t, nfc)
	if err := KeepHere(nfc, Answer{Archive: id, NotNow: true}); err != nil {
		t.Fatal(err)
	}
	a, err1 := os.Stat(nfc)
	b, err2 := os.Stat(nfd)
	if err1 != nil || err2 != nil || !os.SameFile(a, b) {
		t.Skip("this volume tells the two spellings apart")
	}
	if !Read(nfd).NotNow {
		t.Fatal("the Not now kept for the composed spelling is not found for the decomposed spelling of the same folder")
	}
}
