package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1472, R37
// A gone mailbox's account Name and Path read back from a planted record are cleaned as the
// completeness record's account names are (the 1.7.5 adversarial pass's INS-06): Name by
// CleanFolderName (no bidi overrides, no invisible characters, capped), Path with no bidi
// override. Positive twin first: a plain name and path read back as written.
func TestAGoneMailboxsNameAndPathAreCleanedOnRead(t *testing.T) {
	read := func(t *testing.T, path, name string) RunWarning {
		t.Helper()
		out := t.TempDir()
		rec := `{"version":1,"status":"ok","warnings":[{"code":"mailbox_gone","message":"x","path":"` + path + `","name":"` + name + `"}]}`
		if err := os.WriteFile(filepath.Join(out, LastRunName), []byte(rec), 0o600); err != nil {
			t.Fatal(err)
		}
		lr, st, err := ReadLastRun(out)
		if err != nil || st != LastRunPresent || len(lr.Warnings) != 1 {
			t.Fatalf("read: %v %v %+v", st, err, lr.Warnings)
		}
		return lr.Warnings[0]
	}
	if w := read(t, "/p/ImapMail/imap.qz.example", "qzsam@qz.example"); w.Path != "/p/ImapMail/imap.qz.example" || w.Name != "qzsam@qz.example" {
		t.Fatalf("positive twin: a plain name and path read back as written: %+v", w)
	}
	name := "qz@qz.example‮txt.exe​" + strings.Repeat("a", 200)
	w := read(t, "/p/ImapMail/q‮z", name)
	if w.Name != CleanFolderName(name) || strings.ContainsRune(w.Name, '‮') || strings.ContainsRune(w.Name, '​') || len([]rune(w.Name)) >= len([]rune(name)) {
		t.Errorf("Name is not cleaned as an account name: got %q (%d runes), CleanFolderName gives %q", w.Name, len([]rune(w.Name)), CleanFolderName(name))
	}
	if strings.ContainsRune(w.Path, '‮') {
		t.Errorf("Path keeps a right-to-left override: %q", w.Path)
	}
}
