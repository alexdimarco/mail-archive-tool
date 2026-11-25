package job

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1500
// A made-list line keeps the mailbox's store id (the adversarial pass's OUT-03; the lead's fold).
// RecordMadeStore writes the folder's path, a tab, its mailbox's folder name, a tab and the store id;
// the reader gives the three back; the same line recorded again writes nothing; a line recorded again
// for the same folder with another store replaces it; RecordMade (no store id) writes the two fields
// it always wrote, and a store id holding a control character is refused and nothing is written.
func TestAMadeListLineKeepsItsStoreID(t *testing.T) {
	file, parent := madeFixture(t)
	a := filepath.Join(parent, "ann@qz-a.example")
	b := filepath.Join(parent, "Local Folders")
	if err := RecordMadeStore(file, a, "ann@qz-a.example", "path:/p/ImapMail/imap.qz-a.example"); err != nil {
		t.Fatal(err)
	}
	if err := RecordMade(file, b, "Local Folders"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(file)
	if err != nil || string(data) != a+"\tann@qz-a.example\tpath:/p/ImapMail/imap.qz-a.example\n"+b+"\tLocal Folders\n" {
		t.Fatalf("the file: %q %v", data, err)
	}
	got := readMadeT(t, file)
	if len(got) != 2 || got[0] != (Made{Path: a, Name: "ann@qz-a.example", Store: "path:/p/ImapMail/imap.qz-a.example"}) || got[1] != (Made{Path: b, Name: "Local Folders"}) {
		t.Fatalf("read back: %+v", got)
	}
	fi, _ := os.Stat(file)
	if err := RecordMadeStore(file, a, "ann@qz-a.example", "path:/p/ImapMail/imap.qz-a.example"); err != nil {
		t.Fatal(err)
	}
	if fi2, _ := os.Stat(file); !os.SameFile(fi, fi2) {
		t.Error("the same line recorded again rewrote the file")
	}
	if err := RecordMadeStore(file, a, "ann@qz-a.example", "path:/p/ImapMail/imap.qz-a-1.example"); err != nil {
		t.Fatal(err)
	}
	if got := readMadeT(t, file); len(got) != 2 || got[1].Path != a || got[1].Store != "path:/p/ImapMail/imap.qz-a-1.example" {
		t.Errorf("a line for the same folder with another store: %+v", got)
	}

	before, _ := os.ReadFile(file)
	err = RecordMadeStore(file, filepath.Join(parent, "x"), "x", "path:/p\tinjected")
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("holds a control character", "it was not recorded"),
		assure.NoSideEffect(func() bool {
			now, _ := os.ReadFile(file)
			return string(now) == string(before) && !strings.Contains(string(now), "injected")
		}))
}
