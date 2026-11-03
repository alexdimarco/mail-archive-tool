package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/seal"
)

// covers: MA-1064, R36
// A -config typed with a trailing separator ("…/Desktop Two/") names, for the terminal's verbs, the
// same folder a MailArchive Desktop started with that -config keeps its settings and deletions.log
// in (desktop.DeletionsLogFor: the folder itself), not that folder's parent, which making the path
// absolute would otherwise give (the slice-6 and G3 adversarial pass's LOW-1; design-archive-delete
// §16.5 "the verbs never read a different log from the delete that wrote it"). pause and resume read
// that log, delete-archive and the Your-archives verbs write and read it, and the hint keeps the
// separator. Positive twins first: another folder is paused and resumed with that -config.
func TestMA1064_AConfigWithATrailingSeparatorReadsTheDesktopsLog(t *testing.T) {
	cfgDir := filepath.Join(t.TempDir(), "Desktop Two")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatal(err)
	}
	slashed := cfgDir + string(filepath.Separator)
	desktopLog := desktop.DeletionsLogFor(slashed) // what that Desktop reads and writes (MA-1062)
	if want := filepath.Join(cfgDir, archdelete.LogName); desktopLog != want {
		t.Fatalf("precondition: the Desktop started with -config %q keeps its log at %s, got %s", slashed, want, desktopLog)
	}
	cfgArg := encmark.CommandPath(slashed)
	half := g3Archive(t, filepath.Join(t.TempDir(), "Begun from Desktop Two"))
	other := g3Archive(t, filepath.Join(t.TempDir(), "Other"))

	// Positive twins: another folder, and the hint keeps the separator.
	code, stdout, stderr := runCLIOut(t, "pause", "-config", slashed, "-out", other)
	if code != 0 || !seal.IsSealed(other) {
		t.Fatalf("positive twin: pause -config %q of another folder (%d): %s%s", slashed, code, stdout, stderr)
	}
	if want := "Run `mailarchive resume -config " + cfgArg + " -out " + other + "` to undo."; !strings.Contains(stdout, want) {
		t.Errorf("pause's line does not keep the -config as typed: want %q in\n%s", want, stdout)
	}
	if code, stdout, stderr := runCLIOut(t, "resume", "-config", slashed, "-out", other); code != 0 || seal.IsSealed(other) {
		t.Fatalf("positive twin: resume -config %q of another folder (%d): %s%s", slashed, code, stdout, stderr)
	}

	// The terminal's own path to the log, and delete-archive's, are the Desktop's.
	if got, err := configLog(slashed); err != nil || got != desktopLog {
		t.Fatalf("pause, resume and schedule given -config %q read %q (%v), want the Desktop's %q", slashed, got, err, desktopLog)
	}
	seams, _, err := commandLineDelete(slashed)
	if err != nil {
		t.Fatal(err)
	}
	if got := seams.LogPath(); got != desktopLog {
		t.Fatalf("delete-archive -config %q writes %q, want the Desktop's %q", slashed, got, desktopLog)
	}
	// add-archive writes the Desktop's list, beside its log, not one in the parent folder.
	var added bytes.Buffer
	if err := addArchiveWith(slashed, other, &added); err != nil {
		t.Fatalf("add-archive -config %q: %v", slashed, err)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, archindex.Name)); err != nil {
		t.Fatalf("add-archive -config %q did not write the Desktop's list in %s: %v\n%s", slashed, cfgDir, err, added.String())
	}
	if _, err := os.Lstat(filepath.Join(filepath.Dir(cfgDir), archindex.Name)); !os.IsNotExist(err) {
		t.Fatalf("add-archive -config %q wrote a list in the parent folder: %v", slashed, err)
	}

	// A delete in the Desktop's log refuses pause and resume given that -config.
	g3Started(t, desktopLog, half, "g3slash1064")
	code, stdout, stderr = runCLIOut(t, "pause", "-config", slashed, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("so it was not paused; nothing was changed. (Folder: "+half+")",
			"At this terminal, Delete again is: mailarchive delete-archive -config "+cfgArg+" -out "+encmark.CommandPath(half)),
		assure.NoSideEffect(func() bool { return !seal.IsSealed(half) && stdout == "" }))
	if code, stdout, stderr := runCLIOut(t, "pause", "-out", half); code != 0 || !seal.IsSealed(half) {
		t.Fatalf("pause without -config (%d): %s%s", code, stdout, stderr)
	}
	code, stdout, stderr = runCLIOut(t, "resume", "-config", slashed, "-out", half)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("so it was not resumed; nothing was changed. (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return seal.IsSealed(half) && stdout == "" }))
}
