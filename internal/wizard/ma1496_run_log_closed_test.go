package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// runLogs keeps every run log an export opens, through the openRunLog seam: each file is runlog.Open's
// own, kept only so the test can ask it, once the export returned, whether it is closed.
type runLogs struct{ files []*os.File }

func (r *runLogs) install(t *testing.T) {
	t.Helper()
	real := openRunLog
	t.Cleanup(func() {
		openRunLog = real
		// A log a failing run left open is closed here, so the test's folders can be removed.
		for _, f := range r.files {
			_ = f.Close()
		}
	})
	openRunLog = func(p string) (*os.File, error) {
		f, err := real(p)
		if err == nil {
			r.files = append(r.files, f)
		}
		return f, err
	}
}

// open names each kept log that is still open (a closed file's Stat says os.ErrClosed).
func (r *runLogs) open() []string {
	var out []string
	for _, f := range r.files {
		if _, err := f.Stat(); !errors.Is(err, os.ErrClosed) {
			out = append(out, f.Name())
		}
	}
	return out
}

// covers: MA-93, MA-1496
// The one-archive export closes its run log when it ends, on every path (round D; the Windows lab of
// MA-1503): newLogger opened mailarchive.log in the archive's folder and nothing closed it, so on
// Windows a person could not delete or move a just-made archive's folder while the wizard or
// MailArchive Desktop stayed open, until the garbage collector happened to close the file. Positive
// twin first: while the export runs, its log is open (the check sees an open file). Then the log is
// closed: after an export that finished, before its summary is shown; after one that was cancelled,
// before its note; after one that failed (its mailbox gone); after one whose progress window could
// not be opened once the log was.
func TestTheOneArchiveExportClosesItsRunLogOnEveryPath(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	logs := &runLogs{}
	logs.install(t)

	// Finished. The summary is answered Close on every path here, so nothing is ever opened.
	out := filepath.Join(t.TempDir(), "Archive")
	logPath := filepath.Join(out, "mailarchive.log")
	s := &flowScript{summary: zenity.ErrCanceled}
	var during, atSummary []string
	s.onOpen = func(int) { during = logs.open() }
	s.install(t)
	q := questionDialog
	questionDialog = func(text string, o ...zenity.Option) error { atSummary = logs.open(); return q(text, o...) }
	if err := runExport(wizardChoice{inputs: []string{rows[2].path}, out: out, mode: export.Incremental}); err != nil {
		t.Fatalf("the export: %v", err)
	}
	if !reflect.DeepEqual(during, []string{logPath}) {
		t.Fatalf("positive twin: while the export runs, its log is open: %q, want %q", during, logPath)
	}
	assure.Reached(t, s.questions, "the summary")
	if len(atSummary) != 0 || len(logs.open()) != 0 {
		t.Errorf("a finished export's log is still open: at the summary %q, after it %q", atSummary, logs.open())
	}

	// Cancelled: closed before the note waits for the person.
	s = &flowScript{summary: zenity.ErrCanceled, cancel: map[int]bool{1: true}}
	s.install(t)
	var atNote []string
	m := messageDialog
	messageDialog = func(text string, o ...zenity.Option) error { atNote = logs.open(); return m(text, o...) }
	n := len(logs.files)
	err := runExport(wizardChoice{inputs: []string{rows[0].path}, out: filepath.Join(t.TempDir(), "Cancelled"), mode: export.Incremental})
	notes := assure.Reached(t, s.notes, "the cancel note")
	if err != nil || !strings.HasPrefix(notes[0], "Export cancelled.") || len(logs.files) != n+1 {
		t.Fatalf("the cancelled export: %v %q, logs opened %d", err, notes, len(logs.files)-n)
	}
	if len(atNote) != 0 || len(logs.open()) != 0 {
		t.Errorf("a cancelled export's log is still open: at its note %q, after it %q", atNote, logs.open())
	}

	// Failed: its mailbox is gone.
	gone := filepath.Join(t.TempDir(), "imap.gone.example")
	failed := filepath.Join(t.TempDir(), "Failed")
	s = &flowScript{summary: zenity.ErrCanceled}
	s.install(t)
	n = len(logs.files)
	err = runExport(wizardChoice{inputs: []string{gone}, out: failed, mode: export.Incremental})
	code := 0
	if err != nil {
		code = 1
	}
	assure.Refused(t, code, errString(err), assure.Code(1),
		assure.Names(gone, "A log was written to:\n"+filepath.Join(failed, "mailarchive.log")),
		assure.NoSideEffect(func() bool { return len(logs.files) == n+1 && len(logs.open()) == 0 }))

	// The progress window cannot be opened, after the log was.
	s = &flowScript{summary: zenity.ErrCanceled}
	s.install(t)
	progressDialog = func(string) (zenity.ProgressDialog, error) {
		return nil, errors.New("no progress window could be opened")
	}
	n = len(logs.files)
	err = runExport(wizardChoice{inputs: []string{rows[2].path}, out: filepath.Join(t.TempDir(), "No window"), mode: export.Incremental})
	code = 0
	if err != nil {
		code = 1
	}
	assure.Refused(t, code, errString(err), assure.Code(1), assure.Names("no progress window could be opened"),
		assure.NoSideEffect(func() bool { return len(logs.files) == n+1 && len(logs.open()) == 0 }))
}

// covers: MA-1496, MA-93
// Each archive of a set closes its run log when its export returns, before the next archive starts
// (round D; the Windows lab of MA-1503, where ten set tests failed on a log still open). Positive twin
// first: while the k-th archive exports, its log is open and every earlier archive's is closed. Then
// none is left open once the set returns: after a set that finished; one whose second archive was
// cancelled; one whose second archive's mailbox was gone; one whose second progress window could not be
// opened after its log was; and (not on Windows, where the parent cannot be renamed while a log in it is
// open) one whose parent went while the second archive's log was open.
func TestEachArchiveOfASetClosesItsRunLogBeforeTheNextStarts(t *testing.T) {
	ownSettings(t)
	rows, _ := setMail(t)
	logs := &runLogs{}
	logs.install(t)

	parent := setFresh(t)
	s := &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	var wrong []string
	s.onOpen = func(k int) {
		want := []string{filepath.Join(parent, rows[k-1].name, "mailarchive.log")}
		if got := logs.open(); !reflect.DeepEqual(got, want) {
			wrong = append(wrong, fmt.Sprintf("archive %d: open %q, want %q", k, got, want))
		}
	}
	s.install(t)
	made, err := makeSet(rows, "darwin")
	if err != nil || len(made) != 5 || len(s.opened) != 5 || len(logs.files) != 5 {
		t.Fatalf("the set: %+v %v, windows %q, logs %d", made, err, s.opened, len(logs.files))
	}
	if len(wrong) != 0 {
		t.Fatalf("positive twin: while an archive exports, its log alone is open: %q", wrong)
	}
	if open := logs.open(); len(open) != 0 {
		t.Errorf("a finished set left logs open: %q", open)
	}

	// The second archive cancelled.
	s = &flowScript{folders: []string{setFresh(t)}, summary: zenity.ErrCanceled, cancel: map[int]bool{2: true}}
	s.install(t)
	n := len(logs.files)
	made, err = makeSet(rows, "darwin")
	if err != nil || len(made) != 5 || made[1].Outcome != MadeStopped || len(logs.files) != n+2 {
		t.Fatalf("the cancelled set: %+v %v, logs opened %d", made, err, len(logs.files)-n)
	}
	if open := logs.open(); len(open) != 0 {
		t.Errorf("a cancelled set left logs open: %q", open)
	}

	// The second archive's mailbox gone since the plan.
	s = &flowScript{folders: []string{setFresh(t)}, summary: zenity.ErrCanceled}
	s.install(t)
	ch, err := chooseSet(rows, "darwin")
	if err != nil {
		t.Fatal(err)
	}
	aside := rows[1].path + ".kept"
	if err := os.Rename(rows[1].path, aside); err != nil {
		t.Fatal(err)
	}
	n = len(logs.files)
	made, err = runSet(ch, "darwin")
	if err := os.Rename(aside, rows[1].path); err != nil {
		t.Fatal(err)
	}
	if err != nil || len(made) != 5 || made[1].Outcome != MadeFailed || len(logs.files) != n+5 {
		t.Fatalf("the set with a mailbox gone: %+v %v, logs opened %d", made, err, len(logs.files)-n)
	}
	if open := logs.open(); len(open) != 0 {
		t.Errorf("a set with a failed archive left logs open: %q", open)
	}

	// The second progress window cannot be opened, after its log was.
	s = &flowScript{folders: []string{setFresh(t)}, summary: zenity.ErrCanceled}
	s.install(t)
	fp, calls := progressDialog, 0
	progressDialog = func(title string) (zenity.ProgressDialog, error) {
		if calls++; calls == 2 {
			return nil, errors.New("no progress window could be opened")
		}
		return fp(title)
	}
	n = len(logs.files)
	made, err = makeSet(rows, "darwin")
	code := 0
	if err != nil && len(made) == 5 && made[1].Outcome == MadeNotStarted {
		code = 1
	}
	assure.Refused(t, code, errString(err), assure.Code(1), assure.Names("no progress window could be opened"),
		assure.NoSideEffect(func() bool { return len(logs.files) == n+2 && len(logs.open()) == 0 }))

	// The parent gone while the second archive's log is open: its export is refused after the log
	// opened. Windows refuses to rename a folder that holds an open file, so it cannot happen there.
	if runtime.GOOS == "windows" {
		return
	}
	parent = setFresh(t)
	s = &flowScript{folders: []string{parent}, summary: zenity.ErrCanceled}
	s.onOpen = func(k int) {
		if k == 2 {
			if err := os.Rename(parent, parent+" gone"); err != nil {
				t.Error(err)
			}
		}
	}
	s.install(t)
	n = len(logs.files)
	made, err = makeSet(rows, "darwin")
	if err != nil || len(made) != 5 || made[1].Outcome != MadeFailed || len(logs.files) != n+2 {
		t.Fatalf("the set whose parent went: %+v %v, logs opened %d", made, err, len(logs.files)-n)
	}
	if open := logs.open(); len(open) != 0 {
		t.Errorf("a set whose parent went left logs open: %q", open)
	}
}
