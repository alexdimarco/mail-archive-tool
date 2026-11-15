package export

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-1440, R5
// The exporter asks BeforeReplace right before it writes over a file already in a plaintext
// archive or removes one (the 1.7.2 archive guard; the round-5 review's K1), never before a new
// file. Positive twin first: a first export asks nothing; a Full re-export of the same message
// asks about its page and writes it again. Then, the hook refusing: a Full re-export leaves the
// page as it is (here the other archive's page, its bytes kept) and returns the hook's answer;
// a re-export under a new name writes the new page (a new file) but leaves the old-named page,
// which it would otherwise remove, and returns the answer.
func TestTheExporterAsksBeforeItReplacesOrRemovesAFile(t *testing.T) {
	out := t.TempDir()
	manifest := mustManifest(t)
	e := fullExporter(out, manifest)
	var asked []string
	var answer error
	e.BeforeReplace = func(path string) error { asked = append(asked, path); return answer }
	msg := gapMsg("Replace me", "<r@x>", nil)

	if ok, err := e.Export("store", []string{"Inbox"}, msg); err != nil || !ok || len(asked) != 0 {
		t.Fatalf("positive twin: a first export: %v, %v; asked about %v (want nothing: new files)", ok, err, asked)
	}
	key := state.Key("store", "Inbox", msg.Identity())
	rec, _ := manifest.Get(key)
	page := filepath.Join(out, filepath.FromSlash(rec.Path))
	if ok, err := e.Export("store", []string{"Inbox"}, msg); err != nil || !ok || !containsPath(asked, page) {
		t.Fatalf("positive twin: a Full re-export: %v, %v; asked about %v (want its page %s)", ok, err, asked, page)
	}

	refused := errors.New("lock " + filepath.Join(out, ".mailarchive.lock") + " was replaced during the run: stopping so two runs cannot overlap; run again")
	answer = refused
	if err := os.WriteFile(page, []byte("the other archive's page"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := e.Export("store", []string{"Inbox"}, msg)
	assure.Refused(t, 2, fmt.Sprint(err), assure.Names("was replaced during the run"),
		assure.NoSideEffect(func() bool {
			b, rerr := os.ReadFile(page)
			return errors.Is(err, refused) && rerr == nil && string(b) == "the other archive's page"
		}))

	// A re-export under a new name: the record names an old file, which the export would remove.
	oldRel := filepath.ToSlash(filepath.Join(filepath.Dir(filepath.FromSlash(rec.Path)), "old-"+filepath.Base(rec.Path)))
	oldPath := filepath.Join(out, filepath.FromSlash(oldRel))
	if err := os.Rename(page, oldPath); err != nil {
		t.Fatal(err)
	}
	rec.Path = oldRel
	manifest.Add(key, rec)
	_, err = e.Export("store", []string{"Inbox"}, msg)
	assure.Refused(t, 2, fmt.Sprint(err), assure.Names("was replaced during the run"),
		assure.NoSideEffect(func() bool {
			_, oldErr := os.Stat(oldPath)
			return errors.Is(err, refused) && oldErr == nil
		}))
}

// containsPath says paths holds p.
func containsPath(paths []string, p string) bool {
	for _, q := range paths {
		if strings.EqualFold(filepath.Clean(q), filepath.Clean(p)) {
			return true
		}
	}
	return false
}
