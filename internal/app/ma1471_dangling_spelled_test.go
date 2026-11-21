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

// covers: MA-1471, MA-688, R12, R18
// A listed input that cannot be examined (os.Stat fails with anything but "does not exist") is
// refused with the typed source refusal naming it, as a gone input the rule does not skip is, so the
// run is recorded failed in those words and the streak moves (Windows CI at 1ba95c8: a dangling link
// spelled with a trailing "\" gives ERROR_DIRECTORY there, and the run ended untyped). Here, with no
// Windows: a path under a regular file (ENOTDIR). Positive twin first: the archive as it was backs
// up. Today's behaviour kept: a folder that exists but cannot be read is not refused as unreachable.
func TestAnInputThatCannotBeExaminedIsRefusedAsUnreachable(t *testing.T) {
	f := goneFixture(t, false, "imap.qza.example", "imap.qzb.example")
	a, b := f.stores["imap.qza.example"], f.stores["imap.qzb.example"]
	out := tmpDir(t)
	if _, err, _ := goneRun(t, Options{Inputs: []string{a, b}, Auto: true, Out: out}); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	plain := filepath.Join(tmpDir(t), "qz-a-file")
	if err := os.WriteFile(plain, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	under := filepath.Join(plain, "x")
	if _, err := os.Stat(under); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Skipf("this system does not refuse a path under a file with another error: %v", err)
	}
	refusedGone(t, Options{Inputs: []string{a, under}, Auto: true, Out: out}, under)

	locked := filepath.Join(tmpDir(t), "qz-locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	var sue *sourceUnreachableError
	if _, _, err := DiscoverInputs([]string{a, locked}, true); errors.As(err, &sue) {
		t.Errorf("a folder that exists but cannot be read is refused as unreachable, a change from today: %v", err)
	}
}
