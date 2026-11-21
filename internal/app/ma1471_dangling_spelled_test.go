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

	// Added by the lead, 2026-11-21: a permission error when looking is not "gone". An input inside
	// a folder that cannot be searched (os.Stat fails with a permission error) keeps today's plain
	// refusal, inconclusive about the source (classifySourceReach: a present-but-unreadable source is
	// never "gone"), so it never moves the streak toward the 30-day pause and is never told "does
	// not exist". It is still refused: nothing is backed up from it and nothing is skipped.
	inside := filepath.Join(locked, "imap.qzc.example")
	if _, err := os.Stat(inside); !errors.Is(err, os.ErrPermission) {
		t.Skipf("this system does not refuse a look inside an unsearchable folder with a permission error: %v", err)
	}
	files, skipped, err := DiscoverInputs([]string{a, inside}, true)
	if err == nil || len(files) != 0 || len(skipped) != 0 {
		t.Fatalf("an input inside an unsearchable folder must still be refused, nothing skipped: %v %v %v", files, skipped, err)
	}
	if errors.As(err, &sue) {
		t.Errorf("a permission error when looking is refused as unreachable (it counts toward the 30-day pause and says \"does not exist\"): %v", err)
	}
	if reach, _ := classifySourceReach(err); reach != sourceInconclusive {
		t.Errorf("a permission error when looking must leave the source streak alone, got reach %v", reach)
	}
}
