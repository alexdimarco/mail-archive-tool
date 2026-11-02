package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
)

// covers: MA-997, R36, R12
// A Your archives list that cannot be read refuses both verbs and is never read as empty, and
// a saved file is refused when the archives MailArchive knows cannot all be read: otherwise a
// listed archive would be deleted as "not a row", or a record saved inside one (slice 4's
// adversarial pass, del-s4-A2-02, A3-03).
func TestAnUnreadableListOfArchivesRefusesTheDeleteVerbs(t *testing.T) {
	e := newCLIDel(t)
	l := e.plainArchive("Archive L")
	o := e.plainArchive("Archive O")
	inO := filepath.Join(o, "record.txt")

	// Positive twin: with the list readable, a record inside another listed archive is refused as
	// inside it.
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive L"}}, l, "", inO, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("is inside the archive", o),
		assure.NoSideEffect(func() bool { _, serr := os.Stat(inO); return os.IsNotExist(serr) && e.logText() == "" }))

	beforeL := tree(t, l)
	unchanged := func() bool {
		_, serr := os.Stat(inO)
		return sameTree(beforeL, tree(t, l)) && e.logText() == "" && os.IsNotExist(serr)
	}
	good, err := os.ReadFile(e.indexPath())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(e.indexPath(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	for name, run := range map[string]func(a asker) error{
		"delete-archive":                  func(a asker) error { return deleteArchive(a, l, "", "", io.Discard) },
		"delete-archive with -record-out": func(a asker) error { return deleteArchive(a, l, "", inO, io.Discard) },
		"unlist-archive":                  func(a asker) error { return unlistArchive(a, l, io.Discard) },
	} {
		a := &scripted{isAttended: true, answers: []string{"Archive L", "y"}}
		err := run(a)
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
			assure.Names("Your archives", "cannot be read", "cannot tell whether", l, "nothing was changed"),
			assure.NoSideEffect(func() bool { return unchanged() && len(a.prompts) == 0 }))
		if strings.Contains(errText(err), "is not in Your archives") {
			t.Fatalf("%s blames the folder for a list it could not read: %v", name, err)
		}
	}

	// The list readable again but this computer's record of encrypted archives not: a saved file
	// cannot be checked against every known archive, so it is refused.
	if err := os.WriteFile(e.indexPath(), good, 0o600); err != nil {
		t.Fatal(err)
	}
	dir, err := encrecord.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, encrecord.FileName), []byte("not a record\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "record.txt")
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive L"}}, l, "", outside, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("cannot read every archive it knows", "nothing was deleted"),
		assure.NoSideEffect(func() bool { _, serr := os.Stat(outside); return unchanged() && os.IsNotExist(serr) }))
}
