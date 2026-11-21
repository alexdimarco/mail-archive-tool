package app

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/autoplace"
)

// covers: MA-1471, MA-103, R12
// A dangling link is refused however its path is spelled (the 1.7.5 adversarial pass's INS-03): a
// trailing separator, "/." or "dangling/../acct" make the system follow the last link, so rule (a)
// now also looks at the cleaned path and skips only when both spellings say it does not exist;
// DiscoverInputs refuses with the typed error naming the input as spelled. Positive twins first: the
// link spelled cleanly refuses; a folder truly gone, spelled with a trailing separator, is still
// skipped.
func TestADanglingLinkSpelledAnotherWayIsRefused(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	sep := string(filepath.Separator)
	if _, skipped, err := DiscoverInputs([]string{a, b + sep}, true); err != nil || len(skipped) != 1 {
		t.Fatalf("positive twin: a folder truly gone, spelled with a trailing separator, is skipped: %v %v", skipped, err)
	}
	if err := os.Symlink(filepath.Join(tmpDir(t), "gone-target"), b); err != nil {
		t.Skipf("this system cannot make a link here: %v", err)
	}
	var sue *sourceUnreachableError
	if _, _, err := DiscoverInputs([]string{a, b}, true); !errors.As(err, &sue) {
		t.Fatalf("positive twin: the dangling link spelled cleanly refuses: %v", err)
	}
	other := filepath.Join(filepath.Dir(b), "zz-dangling")
	if err := os.Symlink(filepath.Join(tmpDir(t), "gone-other"), other); err != nil {
		t.Fatal(err)
	}
	for _, spelled := range []string{b + sep, b + sep + ".", other + sep + ".." + sep + filepath.Base(b)} {
		if autoplace.GoneSkippable(spelled) {
			t.Errorf("GoneSkippable(%q) is true for a dangling link", spelled)
		}
		files, skipped, err := DiscoverInputs([]string{a, spelled}, true)
		code, msg := 0, ""
		if err != nil {
			code, msg = 1, err.Error()
			if !errors.As(err, &sue) {
				code = 3
			}
		}
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(autoplace.GoneText(spelled)),
			assure.NoSideEffect(func() bool { return len(files) == 0 && len(skipped) == 0 }))
	}
}
