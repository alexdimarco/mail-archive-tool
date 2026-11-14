package pages

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// covers: MA-1336
// The folder pages are written one by one with a check asked before each (the archive guard's
// tail, J1): a backup proves with it that the folder is still its own. When the check answers no,
// nothing more is written, the root page included, and that answer is returned. Positive twin
// first: a check that always answers yes asks before every page, the root page last, and every
// page is written.
func TestFolderPagesStopWritingWhenTheCheckSaysNo(t *testing.T) {
	ix, err := index.Open(filepath.Join(t.TempDir(), "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	for i, folder := range []string{"Alpha", "Beta", "Gamma"} {
		m := &model.Message{Subject: "Subject-" + folder, SenderName: "S", Received: time.Date(2025, 1, 1+i, 9, 0, 0, 0, time.UTC), PlainBody: "b"}
		if err := ix.Add("store", []string{folder}, m, fmt.Sprintf("store/%s/m.html", folder), folder+"\x00id"); err != nil {
			t.Fatal(err)
		}
	}
	if err := ix.Flush(); err != nil {
		t.Fatal(err)
	}
	quiet := log.New(io.Discard, "", 0)
	written := func(out string) int {
		n := 0
		for _, rel := range []string{"store/Alpha/index.html", "store/Beta/index.html", "store/Gamma/index.html", RootPage} {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(rel))); err == nil {
				n++
			}
		}
		return n
	}

	all := t.TempDir()
	var asked []string
	if err := GenerateChecked(all, ix, quiet, 0, func(rel string) error { asked = append(asked, rel); return nil }); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	if len(asked) != 4 || asked[len(asked)-1] != RootPage || written(all) != 4 {
		t.Fatalf("positive twin: asked %v, %d of 4 pages written", asked, written(all))
	}

	out := t.TempDir()
	stop := errors.New("lock was replaced during the run")
	calls := 0
	err = GenerateChecked(out, ix, quiet, 0, func(rel string) error {
		calls++
		if calls == 2 {
			return stop
		}
		return nil
	})
	rc := 0
	if err != nil {
		rc = 1
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Code(1), assure.Names("was replaced during the run"),
		assure.NoSideEffect(func() bool {
			_, rootErr := os.Stat(filepath.Join(out, RootPage))
			return errors.Is(err, stop) && written(out) == 1 && os.IsNotExist(rootErr)
		}))
}
