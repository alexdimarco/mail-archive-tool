package wizard

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// errOneArchivePath ends a test's flow before the one-archive path's export, whose windows are not
// stood in: the flow's own test answers "Export mode:" with it unless a set's plan was shown, so no
// path through these tests can open a real window, whatever the code under test does.
var errOneArchivePath = errors.New("the test stops the one-archive path before its export")

// shippedAnswers answers the shipped flows' lists as a person choosing One archive for each mailbox
// (or, with one, One archive for all of them) would: the local item, Auto-detect, All of them, the
// question's row, and No to the schedule. "Export mode:" before any plan was shown ends the flow.
func shippedAnswers(s *flowScript, row string) func(text string) (string, bool, error) {
	return func(text string) (string, bool, error) {
		switch {
		case text == "What would you like to do?":
			return surfaceLocalItem, true, nil
		case text == "What are you exporting?":
			return srcAuto, true, nil
		case text == pickPrompt:
			return allOfThem, true, nil
		case text == eachQuestion:
			return row, true, nil
		case strings.HasPrefix(text, "Keep these archives current automatically?"):
			return scheduleNo, true, nil
		case text == "Export mode:":
			for _, q := range s.questions {
				if strings.HasPrefix(q, "MailArchive will make") || strings.HasPrefix(q, "No new archive") {
					return "", false, nil
				}
			}
			return "", true, errOneArchivePath
		}
		return "", false, nil
	}
}

// dialogsReachable lets AddArchives' and AddEncryptedArchive's preflight pass on Linux and the BSDs,
// where it looks for a dialog program and a desktop session: a stand-in named zenity that only fails
// (every dialog here goes through the seams, so it is never run) and a DISPLAY. macOS and Windows
// always pass.
func dialogsReachable(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "zenity"), []byte("#!/bin/sh\nexit 1\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DISPLAY", ":99")
}

// storesIn are the store ids the made-list's archive folders hold, one folder each, sorted; it fails
// the test when a folder holds more or less than one store.
func storesIn(t *testing.T) []string {
	t.Helper()
	var ids []string
	for _, m := range madeLines(t) {
		st := archindex.Stores(m.Path)
		if st.Answer != archindex.StoresArchive || len(st.IDs) != 1 {
			t.Errorf("%s holds %v, want exactly one mailbox's store", m.Path, st.IDs)
			continue
		}
		ids = append(ids, st.IDs[0])
	}
	sort.Strings(ids)
	return ids
}

// rowIDs are the store ids of the rows, sorted.
func rowIDs(rows []setRow) []string {
	var ids []string
	for _, r := range rows {
		ids = append(ids, state.PathSourceID(r.path))
	}
	sort.Strings(ids)
	return ids
}

// covers: MA-1490, MA-1499, MA-1507, S90
// The feature is switched on (docs/design-per-mailbox-archives.md §16 step 4; perMailboxBuilt): the
// shipped flows reach the question after All of them and make the set end to end, through the dialog
// seams only (every list, question, folder chooser and progress window stood in; no real window).
// Positive twin first: the double-click wizard (its first step, Auto-detect, All of them, One archive
// for each mailbox, the parent, the plan, the answers, five archives one after another, the summary,
// No to the schedule) makes one archive per mailbox, each holding exactly its own mailbox's store.
// MailArchive Desktop's Make a new archive… (AddArchives) does the same and answers with every
// archive, finished, with the set's folder (MA-1499). Make a new encrypted archive…
// (AddEncryptedArchive) reaches the list and All of them, never the question (MA-1490's keyed gate),
// and hands on every mailbox found with auto-discovery on, as today. On the one-archive path the
// question's One archive for all of them reaches MA-1507's question for a folder that holds two
// archives; "Choose another folder" there writes nothing.
func TestTheShippedFlowsReachOneArchiveForEachMailbox(t *testing.T) {
	if !perMailboxBuilt {
		t.Fatal("the feature is not switched on")
	}
	fresh := func() ([]setRow, string) {
		t.Helper()
		ownSettings(t)
		rows, _ := setMail(t)
		if found, _, err := app.DiscoverInputs(nil, true); err != nil || len(found) != len(rows) {
			// Never drive a flow whose auto-detect would find nothing: its note is a real window.
			t.Fatalf("fixture: auto-detect found %v (%v), want the %d mailboxes", found, err, len(rows))
		}
		parent := filepath.Join(t.TempDir(), "Mail archives")
		if err := os.Mkdir(parent, 0o700); err != nil {
			t.Fatal(err)
		}
		return rows, parent
	}
	asked := func(s *flowScript, text string) bool {
		for _, l := range s.lists {
			if l == text {
				return true
			}
		}
		return false
	}

	// Positive twin: the double-click wizard, after its first step.
	rows, parent := fresh()
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.answer = shippedAnswers(s, eachMany)
	s.install(t)
	if err := wizard(); err != nil {
		t.Fatalf("the double-click wizard: %v (lists %q)", err, s.lists)
	}
	if !asked(s, "What would you like to do?") || !asked(s, eachQuestion) {
		t.Fatalf("the double-click wizard did not ask the question after All of them: %q", s.lists)
	}
	if got, want := storesIn(t), rowIDs(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("the double-click wizard's archives hold %v, want one each of %v", got, want)
	}
	if len(s.opened) != len(rows) || !strings.HasPrefix(s.summaryOf(t), "5 archives are ready in "+parent) {
		t.Errorf("the double-click wizard's set: windows %q, summary %q", s.opened, s.summaryOf(t))
	}

	// MailArchive Desktop's Make a new archive….
	dialogsReachable(t)
	rows, parent = fresh()
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.answer = shippedAnswers(s, eachMany)
	s.install(t)
	made, err := AddArchives()
	if err != nil || len(made) != len(rows) {
		t.Fatalf("Make a new archive…: %+v %v (lists %q)", made, err, s.lists)
	}
	for _, m := range made {
		if m.Outcome != MadeFinished || m.Parent != parent || filepath.Dir(m.Folder) != parent || !m.New {
			t.Errorf("Make a new archive…'s archive: %+v", m)
		}
	}
	if !asked(s, "What are you exporting?") || !asked(s, eachQuestion) || asked(s, "What would you like to do?") {
		t.Errorf("Make a new archive… lists: %q", s.lists)
	}
	if got, want := storesIn(t), rowIDs(rows); !reflect.DeepEqual(got, want) {
		t.Errorf("Make a new archive…'s archives hold %v, want one each of %v", got, want)
	}

	// Make a new encrypted archive…: never the question.
	rows, _ = fresh()
	empty := t.TempDir()
	s = &flowScript{folders: []string{empty}, summary: zenity.ErrCanceled}
	s.answer = func(text string) (string, bool, error) {
		switch text {
		case "What are you exporting?":
			return srcAuto, true, nil
		case pickPrompt:
			return allOfThem, true, nil
		}
		return "", false, nil
	}
	s.install(t)
	var checked string
	pending, err := AddEncryptedArchive(func(out string) error { checked = out; return nil })
	if asked(s, eachQuestion) {
		t.Errorf("Make a new encrypted archive… asked the question after All of them: %q", s.lists)
	}
	if err != nil || pending == nil || checked != empty || !pending.c.auto || len(pending.c.inputs) != len(rows) {
		t.Errorf("Make a new encrypted archive… after All of them: %+v %v (checked %q)", pending, err, checked)
	}
	if len(madeLines(t)) != 0 || len(s.opened) != 0 {
		t.Errorf("Make a new encrypted archive… made a set: made-list %v, windows %q", madeLines(t), s.opened)
	}

	// The one-archive path asks MA-1507's question; Choose another folder writes nothing.
	fresh()
	held := t.TempDir()
	archiveHolding(t, filepath.Join(held, "a@x.example"), "path:/mail/a")
	archiveHolding(t, filepath.Join(held, "b@x.example"), "path:/mail/b")
	before := treeOf(t, held)
	s = &flowScript{folders: []string{held}, summary: zenity.ErrCanceled}
	s.answer = shippedAnswers(s, eachOne)
	s.install(t)
	made, err = AddArchives()
	code, msg := 0, ""
	for _, q := range s.questions {
		if strings.HasPrefix(q, held+" already holds 2 archives.") && err == nil && made == nil {
			code, msg = 1, q
		}
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(held+" already holds 2 archives. An archive made here would hold them inside it, and making one archive for each mailbox in this folder again would then be refused. Choose another folder?"),
		assure.NoSideEffect(func() bool { return reflect.DeepEqual(treeOf(t, held), before) && !asked(s, "Export mode:") }))
}
