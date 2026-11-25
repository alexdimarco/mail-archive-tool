package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1507
// A set's parent chosen for one archive (§3, C8; revision 2.1's F-10a): on the one-archive and
// single-row paths, an output folder that directly holds two or more archive folders asks first, in
// F-10a's words, before anything is written. Positive twins first: an empty folder, and one holding
// one archive, ask nothing. A folder holding two (a plaintext and an encrypted one) asks; "Choose
// another folder" (and closing the box) writes nothing and asks the folder again; "Use this folder
// anyway" takes it. Without the guard (every other path, and the shipped flow until step 3b) nothing
// is asked. Through recorders for every dialog.
func TestAFolderHoldingArchivesAsksBeforeOneArchiveIsMadeInIt(t *testing.T) {
	ownSettings(t)
	empty := t.TempDir()
	one := t.TempDir()
	archiveHolding(t, filepath.Join(one, "a@x.example"), "path:/mail/a")
	held := t.TempDir()
	archiveHolding(t, filepath.Join(held, "a@x.example"), "path:/mail/a")
	enc := filepath.Join(held, "Locked")
	if err := os.Mkdir(enc, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(enc, ".mailarchive-vault.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(held, "Not an archive"), 0o700); err != nil {
		t.Fatal(err)
	}

	for _, folder := range []string{empty, one} {
		s := &setScript{folders: []string{folder}}
		s.install(t)
		if out, err := askOutputFolder(true); err != nil || out != folder || len(s.questions) != 0 {
			t.Fatalf("positive twin: %s asks nothing: %q %v, questions %q", folder, out, err, s.questions)
		}
	}

	before := treeOf(t, held)
	s := &setScript{folders: []string{held, empty}, answers: []error{zenity.ErrCanceled}}
	s.install(t)
	out, err := askOutputFolder(true)
	code, msg := 0, ""
	if err == nil && out == empty && len(s.questions) == 1 && len(s.chooser) == 2 {
		code, msg = 1, s.questions[0]
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(held+" already holds 2 archives. An archive made here would hold them inside it, and making one archive for each mailbox in this folder again would then be refused. Choose another folder?"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeOf(t, held), before) }))

	s = &setScript{folders: []string{held}, answers: []error{nil}}
	s.install(t)
	if out, err := askOutputFolder(true); err != nil || out != held || len(s.questions) != 1 {
		t.Errorf("Use this folder anyway takes it: %q %v", out, err)
	}
	s = &setScript{folders: []string{held}}
	s.install(t)
	if out, err := askOutputFolder(false); err != nil || out != held || len(s.questions) != 0 {
		t.Errorf("without the guard nothing is asked: %q %v %q", out, err, s.questions)
	}
	s = &setScript{folders: []string{held}, answers: []error{errors.New("the dialog program failed")}}
	s.install(t)
	if out, err := askOutputFolder(true); err == nil || out != "" {
		t.Errorf("a failing question ends the step: %q %v", out, err)
	}

	// The wiring: the auto-detect paths, held until step 3b; "Choose another folder" the default.
	for file, wired := range map[string]string{
		"wizard.go":    "askOutputFolder(perMailboxBuilt && srcType == srcAuto)",
		"setparent.go": "questionDialog(heldArchivesWords(out, n), zenity.Title(appTitle), zenity.OKLabel(useFolderAnyway), zenity.CancelLabel(chooseAnother), zenity.DefaultCancel())",
	} {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), wired) {
			t.Errorf("%s no longer holds %q", file, wired)
		}
	}
	if useFolderAnyway != "Use this folder anyway" || chooseAnother != "Choose another folder" {
		t.Errorf("the buttons: %q %q", useFolderAnyway, chooseAnother)
	}
}
