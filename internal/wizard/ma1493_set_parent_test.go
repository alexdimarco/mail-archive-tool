package wizard

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/job"
)

// setScript replaces the dialogs of a set's steps for one test: the folder chooser gives folders in
// turn (then endErr, or zenity.ErrCanceled), the question gives answers in turn (nil is its OK
// button; then zenity.ErrCanceled), the note records its text. No real dialog opens.
type setScript struct {
	folders   []string
	endErr    error
	answers   []error
	notes     []string
	questions []string
	chooser   []string // the chooser's titles, one per time it opened
	lists     []dialogCall
}

func (s *setScript) install(t *testing.T) {
	t.Helper()
	oldMsg, oldList, oldQ, oldF := messageDialog, listDialog, questionDialog, folderDialog
	t.Cleanup(func() { messageDialog, listDialog, questionDialog, folderDialog = oldMsg, oldList, oldQ, oldF })
	messageDialog = func(text string, _ ...zenity.Option) error {
		s.notes = append(s.notes, text)
		return nil
	}
	listDialog = func(text string, items []string, _ ...zenity.Option) (string, error) {
		s.lists = append(s.lists, dialogCall{kind: "list", text: text, items: items})
		return "", zenity.ErrCanceled
	}
	questionDialog = func(text string, _ ...zenity.Option) error {
		s.questions = append(s.questions, text)
		if len(s.answers) == 0 {
			return zenity.ErrCanceled
		}
		a := s.answers[0]
		s.answers = s.answers[1:]
		return a
	}
	folderDialog = func(opts ...zenity.Option) (string, error) {
		s.chooser = append(s.chooser, "chooser")
		if len(s.folders) == 0 {
			if s.endErr != nil {
				return "", s.endErr
			}
			return "", zenity.ErrCanceled
		}
		f := s.folders[0]
		s.folders = s.folders[1:]
		return f, nil
	}
}

// ownSettings gives the test its own home, settings and cache folders, so the made-list, the
// Desktop's list and the last-archive note it reads are its own.
func ownSettings(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "LOCALAPPDATA"} {
		t.Setenv(v, home)
	}
}

// treeOf lists every path under dir, itself included, with its kind, so a test can tell nothing was
// made, moved or removed there.
func treeOf(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		out = append(out, p+" "+d.Type().String())
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
	sort.Strings(out)
	return out
}

// plainArchive makes dir an archive folder (its manifest is there).
func plainArchive(t *testing.T, dir string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), []byte(`{"version":6,"stores":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// madeLines are the made-list's lines.
func madeLines(t *testing.T) []job.Made {
	t.Helper()
	f, err := job.MadeListFile()
	if err != nil {
		t.Fatal(err)
	}
	ms, err := job.ReadMade(f)
	if err != nil {
		t.Fatal(err)
	}
	return ms
}

// covers: MA-1493
// The parent folder (§6): judged as the unencrypted copy judges a new folder inside a chosen parent.
// Positive twin first: a plain folder passes, and a folder made in it through makeSetFolder is there
// at 0700 and in the made-list. Refused, each with the wizard's own words naming the folder, the
// folder asked again and nothing created: an archive, a folder inside one, one reached through a
// link, an encrypted archive and a folder inside one, a folder inside an archive this computer knows
// only from the wizard's made-list (knownArchives now reads it), MailArchive's own settings folder.
// Cancel on the chooser ends with nothing.
func TestASetsParentFolderIsCheckedAndRefusedInTheWizardsWords(t *testing.T) {
	ownSettings(t)
	root := t.TempDir()
	good := filepath.Join(root, "Mail archives")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	s := &setScript{folders: []string{good}}
	s.install(t)
	sp, err := askSetParent()
	if err != nil || sp == nil || sp.Parent != good || len(s.notes) != 0 || len(s.chooser) != 1 {
		t.Fatalf("positive twin: a plain folder: %+v %v, notes %q", sp, err, s.notes)
	}
	path, err := makeSetFolder(sp, "qzsam.walker@gmail.com", "qzsam.walker@gmail.com")
	fi, serr := os.Lstat(path)
	if err != nil || serr != nil || !fi.IsDir() || path != filepath.Join(good, "qzsam.walker@gmail.com") {
		t.Fatalf("positive twin: the folder is made: %q %v %v", path, err, serr)
	}
	if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o700 {
		t.Errorf("the folder is made owner-only: %v", fi.Mode().Perm())
	}
	if got := madeLines(t); !reflect.DeepEqual(got, []job.Made{{Path: path, Name: "qzsam.walker@gmail.com"}}) {
		t.Errorf("the made-list records it right after it is made: %+v", got)
	}

	arch := plainArchive(t, filepath.Join(root, "Old archive"))
	inArch := filepath.Join(arch, "inside")
	enc := filepath.Join(root, "Locked")
	inEnc := filepath.Join(enc, "inside")
	known := filepath.Join(root, "Unfinished")
	inKnown := filepath.Join(known, "inside")
	for _, d := range []string{inArch, inEnc, inKnown} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(enc, ".mailarchive-vault.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	mf, _ := job.MadeListFile()
	if err := job.RecordMade(mf, known, "pat@b.example"); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	own, err := job.Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(own, 0o700); err != nil {
		t.Fatal(err)
	}
	// Amended 2026-11-25 (round D; nothing weakened): each case is its own subtest and the link is made
	// just before its case (symlinkOrSkip), so where a symbolic link cannot be made (an ordinary
	// Windows user, the Windows lab of MA-1503) only that case is skipped, with the reason, and every
	// other case still runs. The link was made before the loop, failing the whole test there.
	for _, c := range []struct {
		name, folder string
		words        []string
	}{
		{"an archive", arch, []string{"is an archive, or is inside one, so the new archives cannot go there. Choose a folder outside every archive."}},
		{"inside an archive", inArch, []string{"is an archive, or is inside one"}},
		{"a link into an archive", link, []string{"is an archive, or is inside one"}},
		{"an encrypted archive", enc, []string{"is an encrypted archive, or is inside one", "Choose a folder outside every archive."}},
		{"inside an encrypted archive", inEnc, []string{"is an encrypted archive, or is inside one"}},
		{"inside an archive only the made-list knows", inKnown, []string{"is an archive, or is inside one"}},
		{"MailArchive's own settings folder", own, []string{"MailArchive keeps its settings or working files in", "Choose a folder of your own."}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.folder == link {
				symlinkOrSkip(t, inArch, link)
			}
			before := treeOf(t, root)
			s := &setScript{folders: []string{c.folder, good}}
			s.install(t)
			sp, err := askSetParent()
			if err != nil || sp == nil || sp.Parent != good || len(s.chooser) != 2 {
				t.Fatalf("%s: the folder is asked again and the good one taken: %+v %v, chooser %d", c.name, sp, err, len(s.chooser))
			}
			code, note := 0, ""
			if len(s.notes) == 1 {
				code, note = 1, s.notes[0]
			}
			assure.Refused(t, code, note, assure.Code(1), assure.Names(append([]string{c.folder}, c.words...)...),
				assure.Forbid("unencrypted", "panic", "goroutine"),
				assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeOf(t, root), before) }))
		})
	}

	// Cancel on the chooser: nothing.
	s = &setScript{}
	s.install(t)
	if sp, err := askSetParent(); sp != nil || !errors.Is(err, zenity.ErrCanceled) {
		t.Errorf("Cancel: %+v %v", sp, err)
	}
}

// covers: MA-1493
// A parent swapped for a link between the check and the make is refused, nothing made in the
// folder the link leads to nor in the moved one, and nothing recorded (F-04: the set's checked
// result is re-proved against the folder the handle on the parent holds). Positive twin first: the
// same parent, unswapped, makes its folder; a name already there is R-q, nothing made. Swaps: to a
// link into an archive (the path checks again), to a link to a plain folder elsewhere (the parent's
// identity is not the one checked). And each refusal code the reused check returns has the wizard's
// own words, naming the folder, never the unencrypted copy's.
func TestASetsParentSwappedForALinkAfterTheCheckIsRefused(t *testing.T) {
	ownSettings(t)
	root := t.TempDir()
	good := filepath.Join(root, "Mail archives")
	if err := os.Mkdir(good, 0o755); err != nil {
		t.Fatal(err)
	}
	sp, err := setEnv().CheckSetParent(good)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := makeSetFolder(sp, "Local Folders", "Local Folders"); err != nil || p != filepath.Join(good, "Local Folders") {
		t.Fatalf("positive twin: unswapped, the folder is made: %q %v", p, err)
	}
	if p, err := makeSetFolder(sp, "Local Folders", "Local Folders"); p != "" || !strings.Contains(setParentWords(err, good, "Local Folders"), "something there already has that name") {
		t.Errorf("a name already there is refused: %q %v", p, err)
	}
	lines := madeLines(t)

	arch := plainArchive(t, filepath.Join(t.TempDir(), "archive"))
	other := t.TempDir()
	moved := good + ".moved"
	if err := os.Rename(good, moved); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name, target string
		words        string
	}{
		{"a link into an archive", arch, "is an archive, or is inside one"},
		{"a link to another folder", other, "it changed while MailArchive was checking it"},
	} {
		// Amended 2026-11-25 (round D; nothing weakened): each swap is its own subtest (symlinkOrSkip),
		// so where a symbolic link cannot be made (an ordinary Windows user) the swaps are skipped with
		// the reason and the words below still run.
		t.Run(c.name, func(t *testing.T) {
			_ = os.Remove(good)
			symlinkOrSkip(t, c.target, good)
			beforeT, beforeM := treeOf(t, c.target), treeOf(t, moved)
			p, err := makeSetFolder(sp, "qzsam.walker@gmail.com", "qzsam.walker@gmail.com")
			code := 0
			if err != nil && p == "" {
				code = 1
			}
			assure.Refused(t, code, setParentWords(err, good, "qzsam.walker@gmail.com"), assure.Code(1), assure.Names(good, c.words),
				assure.NoSideEffect(func() bool {
					return reflect.DeepEqual(treeOf(t, c.target), beforeT) && reflect.DeepEqual(treeOf(t, moved), beforeM) && reflect.DeepEqual(madeLines(t), lines)
				}))
		})
	}

	// Each code has the wizard's words, naming the folder; none is the unencrypted copy's.
	seen := map[string]string{}
	for _, id := range []string{"R-e", "R-q", "R-w", "R-f", "R-f2", "R-set", "R-enc"} {
		w := setParentWords(&app.CopyRefusal{ID: id, Message: "the unencrypted copy's words"}, "/x/Mail archives", "qzsam@x.example")
		if !strings.Contains(w, "/x/Mail archives") || strings.Contains(w, "unencrypted") || strings.Contains(w, "could not check") {
			t.Errorf("%s: %q", id, w)
		}
		for other, ow := range seen {
			if ow == w {
				t.Errorf("%s and %s have the same words", id, other)
			}
		}
		seen[id] = w
	}
	if w := setParentWords(&app.CopyRefusal{ID: "R-q"}, "/x", "qzsam@x.example"); !strings.Contains(w, "qzsam@x.example") {
		t.Errorf("R-q names the folder being made: %q", w)
	}
}
