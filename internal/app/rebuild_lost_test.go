package app

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/index"
)

// rebuildSurface reads what a rebuild may replace: search.db, the manifest and
// every folder/root index.html, keyed by path relative to out.
func rebuildSurface(t *testing.T, out string) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	read := func(rel string) {
		data, err := os.ReadFile(filepath.Join(out, rel))
		if err != nil {
			t.Fatal(err)
		}
		files[rel] = data
	}
	read("search.db")
	read(".mailarchive-manifest.json")
	err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "index.html" {
			return err
		}
		rel, rerr := filepath.Rel(out, p)
		if rerr != nil {
			return rerr
		}
		read(rel)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// covers: MA-388, R8, R13, S33
// `reindex -rebuild` keeps the same books as a run (F49): when SQLite abandons
// a batch mid-rebuild — RAISE(ROLLBACK) planted in the fresh index (fault
// injection; the archive, the rebuild and SQLite are real), the shape of a
// full disk or an I/O error — the rows whose Add had succeeded go with it, so
// that index must never be renamed over search.db and reported rebuilt. The
// rebuild fails naming the lost count and the re-run; search.db, the folder
// pages and the manifest are left byte-for-byte as they were, no temp index
// is left behind, and status still reads every row. Positive twin: the same
// rebuild without the fault swaps in an index holding every record.
func TestRebuildRefusesAnIndexThatLostRows(t *testing.T) {
	const n = 5
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, n)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	tmpIndex := filepath.Join(out, "search.db.rebuild")

	orig := openRebuildIndex
	t.Cleanup(func() { openRebuildIndex = orig })
	openRebuildIndex = func(path string) (*index.Index, error) {
		ix, err := index.Open(path)
		if err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", path)
		if err != nil {
			ix.Close()
			return nil, err
		}
		defer db.Close()
		if _, err := db.Exec(`CREATE TRIGGER inject_fault BEFORE INSERT ON docs WHEN NEW.subject = 'msg 3'
			BEGIN SELECT RAISE(ROLLBACK, 'injected index fault'); END`); err != nil {
			ix.Close()
			return nil, err
		}
		return ix, nil
	}

	before := rebuildSurface(t, out)
	rep, err := Rebuild(out, logger)
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names("3 message(s)", "lost", "search.db", "reindex -rebuild -out"),
		assure.NoSideEffect(func() bool {
			after := rebuildSurface(t, out)
			if len(after) != len(before) {
				return false
			}
			for rel, data := range before {
				if !bytes.Equal(after[rel], data) {
					t.Logf("the failed rebuild changed %s", rel)
					return false
				}
			}
			for _, s := range []string{"", "-wal", "-shm"} {
				if _, serr := os.Stat(tmpIndex + s); !os.IsNotExist(serr) {
					t.Logf("the failed rebuild left %s behind", filepath.Base(tmpIndex+s))
					return false
				}
			}
			return true
		}))
	if rep.Lost != 3 {
		t.Errorf("the rebuild report counts %d lost rows, want the 3 abandoned with the batch", rep.Lost)
	}
	if in := health.Gather(out, ""); in.Indexed != n || in.Messages != n {
		t.Fatalf("after the refused rebuild status reads %d indexed of %d, want the old index's %d of %d", in.Indexed, in.Messages, n, n)
	}

	// Positive twin: without the fault the rebuild swaps in a complete index.
	openRebuildIndex = orig
	rep, err = Rebuild(out, logger)
	if err != nil {
		t.Fatalf("positive twin: rebuild failed: %v", err)
	}
	if rep.Rebuilt != n || rep.Lost != 0 {
		t.Fatalf("positive twin: rebuilt=%d lost=%d, want %d/0", rep.Rebuilt, rep.Lost, n)
	}
	if in := health.Gather(out, ""); in.Indexed != n {
		t.Fatalf("positive twin: status reads %d indexed, want %d", in.Indexed, n)
	}
}
