package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

// covers: MA-1023, R36, R12
// add-archive takes -config before or after the folder (the 1.5.3 friction fixes' adversarial
// pass, finding 3): Go's flag parsing stopped at the folder, so "add-archive DIR -config FILE" —
// the order its own usage line gave — exited 1 "give one folder". Its usage now reads
// "add-archive [-config FILE] DIR", and two folders are still refused, nothing listed. Positive
// twin first: the flag before the folder. The program itself is run.
func TestAddArchiveTakesConfigBeforeOrAfterTheFolder(t *testing.T) {
	e := newCLIDel(t)
	cfgDir := t.TempDir()
	cfg := filepath.Join(cfgDir, "graph-config.json")
	list := filepath.Join(cfgDir, archindex.Name)
	listed := func(dir string) bool {
		idx, err := archindex.ReadFile(list)
		if err != nil {
			return false
		}
		for _, en := range idx.Entries {
			if en.Path == dir {
				return true
			}
		}
		return false
	}

	before := filepath.Join(t.TempDir(), "Flag Before")
	e.archiveAt(before)
	if code, stderr := runCLI("add-archive", "-config", cfg, before); code != 0 || !listed(before) {
		t.Fatalf("the positive twin, -config before the folder: exit %d (listed %v)\n%s", code, listed(before), stderr)
	}

	after := filepath.Join(t.TempDir(), "Flag After")
	e.archiveAt(after)
	if code, stderr := runCLI("add-archive", after, "-config", cfg); code != 0 || !listed(after) || e.listed(after) {
		t.Fatalf("-config after the folder: exit %d (listed beside the config %v, in the default list %v)\n%s", code, listed(after), e.listed(after), stderr)
	}

	// Two folders: refused, nothing listed.
	a, b := filepath.Join(t.TempDir(), "A"), filepath.Join(t.TempDir(), "B")
	e.archiveAt(a)
	e.archiveAt(b)
	listBefore := readFile(t, list)
	code, stderr := runCLI("add-archive", a, "-config", cfg, b)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("give one folder: mailarchive add-archive [-config FILE] DIR."),
		assure.NoSideEffect(func() bool { return bytes.Equal(listBefore, readFile(t, list)) && !e.listed(a) && !e.listed(b) }))

	if code, stderr := runCLI("add-archive", "-h"); code != 0 || !strings.Contains(stderr, "Usage: mailarchive add-archive [-config FILE] DIR\n") {
		t.Fatalf("add-archive -h (exit %d):\n%s", code, stderr)
	}
}

// covers: MA-1024, R36
// A -config whose folder does not exist is refused by the three Your-archives verbs (the 1.5.3
// friction fixes' adversarial pass, finding 4): a mistyped folder made a new, empty list there, so
// add-archive "added" to it and delete-archive then deleted against a list and a deletions.log the
// real Desktop never sees. Refused naming the file, its folder and what to give, nothing created,
// no list, no log, the archive untouched. Positive twin first: a -config FILE that was never
// written, in a folder that exists, is used (a Desktop keeps its list beside FILE before FILE).
func TestAConfigInAFolderThatDoesNotExistIsRefused(t *testing.T) {
	e := newCLIDel(t)
	dir := filepath.Join(t.TempDir(), "Real Archive")
	e.archiveAt(dir)

	okCfg := filepath.Join(t.TempDir(), "graph-config.json") // never written
	if err := addArchiveWith(okCfg, dir, io.Discard); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(okCfg), archindex.Name)); err != nil {
		t.Fatalf("the positive twin wrote no list beside its config: %v", err)
	}

	typo := filepath.Join(t.TempDir(), "no-such-folder")
	cfg := filepath.Join(typo, "graph-config.json")
	folderBefore, defaultListBefore := tree(t, dir), readFile(t, e.indexPath())
	nothing := func() bool {
		_, err := os.Stat(typo)
		return os.IsNotExist(err) && sameTree(folderBefore, tree(t, dir)) && bytes.Equal(defaultListBefore, readFile(t, e.indexPath())) && e.logText() == ""
	}
	want := []string{"There are no MailArchive Desktop settings beside", "graph-config.json", "no-such-folder", "does not exist", "nothing was changed",
		"Give -config the config file MailArchive Desktop was started with."}

	err := addArchiveWith(cfg, dir, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(want...), assure.NoSideEffect(nothing))

	a := &scripted{isAttended: true, answers: []string{"Real Archive"}}
	err = deleteArchiveWith(cfg, a, dir, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(want...),
		assure.NoSideEffect(func() bool { return nothing() && len(a.prompts) == 0 }))

	u := &scripted{isAttended: true, answers: []string{"y"}}
	err = unlistArchiveWith(cfg, u, dir, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(want...),
		assure.NoSideEffect(func() bool { return nothing() && len(u.prompts) == 0 }))
}

// covers: MA-1023, R36, R12
// add-archive's own refusals end with their periods and say "readable by other users" once, with
// the remedy, as delete-archive says it (the 1.5.3 friction fixes' adversarial pass, finding 11:
// its list-unreadable refusal repeated the file's own "…readable by other users (mode 0644): run
// `chmod 600 PATH`: readable by other users"). Positive twin first: the list readable by this
// account alone, the folder is listed.
func TestAddArchiveRefusalsReadPlainly(t *testing.T) {
	e := newCLIDel(t)
	good := filepath.Join(t.TempDir(), "Good")
	e.archiveAt(good)
	if err := addArchiveWith("", good, io.Discard); err != nil || !e.listed(good) {
		t.Fatalf("the positive twin: %v", err)
	}

	// No folder given: its period.
	if code, stderr := runCLI("add-archive"); code != 1 || !strings.Contains(stderr, "the folder is required: mailarchive add-archive [-config FILE] DIR.") {
		t.Fatalf("no folder (exit %d):\n%s", code, stderr)
	}

	if runtime.GOOS == "windows" {
		return // modes are not kept on Windows: the list cannot be readable by other users there
	}
	other := filepath.Join(t.TempDir(), "Other")
	e.archiveAt(other)
	if err := os.Chmod(e.indexPath(), 0o644); err != nil {
		t.Fatal(err)
	}
	listBefore := readFile(t, e.indexPath())
	err := addArchiveWith("", other, io.Discard)
	os.Chmod(e.indexPath(), 0o600)
	msg := errText(err)
	assure.Refused(t, exitCode(err), msg, assure.Code(1),
		assure.Names(`Your archives ("`+e.indexPath()+`") cannot be read: other users of this computer can read it`, "nothing was changed", `Run chmod 600 "`+e.indexPath()+`", then try again.`),
		assure.Forbid("readable by other users", "panic", "goroutine"),
		assure.NoSideEffect(func() bool { return bytes.Equal(listBefore, readFile(t, e.indexPath())) && !e.listed(other) }))
}
