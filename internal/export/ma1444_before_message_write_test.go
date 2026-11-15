package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1444, R5
// The exporter asks BeforeMessageWrite once per message right before it writes anything of it,
// and never for a message it only skips (the 1.7.2 archive guard; the patch review's P1: a run
// proves while it writes, never while it only reads). Positive twin first: a first export asks
// once and writes the message; an incremental re-export of it only skips and asks nothing. Then,
// the hook refusing: a new message writes nothing at all (no folder, page or record) and Export
// returns the hook's answer.
func TestTheExporterAsksBeforeAMessagesFirstWrite(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := incExporter(out, manifest)
	asked := 0
	var answer error
	e.BeforeMessageWrite = func() error { asked++; return answer }
	first := gapMsg("First", "<first@x>", nil)
	if ok, err := e.Export("store", []string{"Inbox"}, first); err != nil || !ok || asked != 1 {
		t.Fatalf("positive twin: a first export: %v, %v; asked %d times, want once", ok, err, asked)
	}
	if ok, err := e.Export("store", []string{"Inbox"}, first); err != nil || ok || asked != 1 {
		t.Fatalf("positive twin: a re-export that only skips: %v, %v; asked %d times in all, want still once", ok, err, asked)
	}

	refused := errors.New("the archive location is unavailable")
	answer = refused
	second := gapMsg("Second", "<second@x>", nil)
	_, err := e.Export("store", []string{"Sent"}, second)
	rc := 0
	if errors.Is(err, refused) {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("is unavailable"),
		assure.NoSideEffect(func() bool {
			_, dirErr := os.Stat(filepath.Join(out, "store", "Sent"))
			_, recorded := manifest.Get(state.Key("store", "Sent", second.Identity()))
			return errors.Is(dirErr, os.ErrNotExist) && !recorded
		}))
}

// covers: MA-1445, R5
// The crash-leftover sweep a run makes at its start asks its check right before each removal, and
// a refusal stops it there: it removes nothing more and returns the refusal (the patch review's
// P5: the sweep walks the whole folder by path, which after a swap is the other archive's).
// Positive twin first: a check that always agrees is asked before each of a stale temp in the
// folder, a stale temp in a message folder and an orphan zip, and all three are removed. Then a
// check that refuses at its first ask: nothing is removed.
func TestTheCheckedSweepAsksBeforeEachRemoval(t *testing.T) {
	plant := func(out string) []string {
		old := time.Now().Add(-time.Hour)
		files := []string{filepath.Join(out, ".mailarchive-stale1.tmp"), filepath.Join(out, "tok", ".mailarchive-stale2.tmp"), filepath.Join(out, "tok", "zz-only-a-zip"+zipSuffix)}
		for _, p := range files {
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(p, old, old); err != nil {
				t.Fatal(err)
			}
		}
		return files
	}
	there := func(files []string) int {
		n := 0
		for _, p := range files {
			if _, err := os.Stat(p); err == nil {
				n++
			}
		}
		return n
	}
	out := t.TempDir()
	files := plant(out)
	var asked []string
	n, err := SweepOrphansChecked(out, time.Now(), nil, func(p string) error { asked = append(asked, p); return nil })
	if err != nil || n != 3 || len(asked) != 3 || there(files) != 0 {
		t.Fatalf("positive twin: removed %d (%v), asked about %v, %d still there", n, err, asked, there(files))
	}

	out = t.TempDir()
	files = plant(out)
	refused := errors.New("lock was replaced during the run")
	n, err = SweepOrphansChecked(out, time.Now(), nil, func(string) error { return refused })
	rc := 0
	if errors.Is(err, refused) {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("was replaced during the run"),
		assure.NoSideEffect(func() bool { return n == 0 && there(files) == 3 }))
	if n, err = SweepTempsChecked(out, time.Now(), nil, func(string) error { return refused }); !errors.Is(err, refused) || n != 0 || there(files) != 3 {
		t.Fatalf("the temps-only sweep removed %d (%v), %d still there", n, err, there(files))
	}
}

// covers: MA-1446, R5
// An exporter with KeepOutDir (a run's, once it has begun on its archive) never makes its OutDir
// itself: a message's folder is made only inside an OutDir that is there, and an OutDir that is
// not there (a drive disconnected or swapped, the folder moved or renamed) is the run-fatal
// ErrArchiveGone (FatalGone), so the run ends as the archive location being unavailable instead of
// writing its new files into an empty stand-in (the 1.7.2 friction review's F5). Positive twins
// first: without KeepOutDir an OutDir that is not there is made, as an export's own first setup
// makes it; with KeepOutDir and the OutDir there, the message's folders are made inside it.
func TestAnExporterThatKeepsItsOutDirNeverMakesIt(t *testing.T) {
	parent := t.TempDir()
	fresh := filepath.Join(parent, "Fresh")
	if ok, err := incExporter(fresh, mustManifest(t)).Export("store", []string{"Inbox"}, gapMsg("One", "<one@x>", nil)); err != nil || !ok {
		t.Fatalf("positive twin: an exporter without KeepOutDir: %v, %v", ok, err)
	}
	if _, err := os.Stat(filepath.Join(fresh, "store", "Inbox")); err != nil {
		t.Fatalf("positive twin: the OutDir and its message folder were not made: %v", err)
	}
	kept := filepath.Join(parent, "Kept")
	if err := os.Mkdir(kept, 0o755); err != nil {
		t.Fatal(err)
	}
	e := incExporter(kept, mustManifest(t))
	e.KeepOutDir = true
	if ok, err := e.Export("store", []string{"Inbox", "Deep"}, gapMsg("Two", "<two@x>", nil)); err != nil || !ok {
		t.Fatalf("positive twin: KeepOutDir with the OutDir there: %v, %v", ok, err)
	}
	if err := os.Rename(kept, kept+"-moved"); err != nil {
		t.Fatal(err)
	}
	_, err := e.Export("store", []string{"Inbox", "Deep"}, gapMsg("Three", "<three@x>", nil))
	rc := 0
	if errors.Is(err, ErrArchiveGone) && FatalKindAt(err, kept) == FatalGone && IsRunFatal(err) {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names(kept),
		assure.NoSideEffect(func() bool {
			_, serr := os.Lstat(kept)
			return errors.Is(serr, os.ErrNotExist)
		}))
}
