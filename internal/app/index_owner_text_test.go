package app

import (
	"bytes"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/index"
)

// covers: MA-386, R12, S33
// The refusals the owner meets on MailArchive Desktop ("It didn't finish: ...")
// and mailarchive-gui's job log are put in the owner's words (g7 friction F3):
// IndexOpenError.ForOwner names the folder, the cause in plain terms and the
// remedy the owner can act on (the app's own rebuild action when the index is
// damaged), with no `mailarchive reindex` command line and none of SQLite's own
// text, which stays in Error() for the log and the CLI. Positive twin: the CLI
// text of the same errors is unchanged and still names the command.
func TestIndexOpenErrorSpeaksToTheOwner(t *testing.T) {
	out := tmpDir(t)
	idxPath := filepath.Join(out, "search.db")
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	if err := os.WriteFile(mpath, []byte(`{"version":5,"entries":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	const rebuild = "Maintenance → Rebuild search index"

	// Real SQLite errors: damaged file, a held lock, a full disk.
	junk := filepath.Join(tmpDir(t), "junk.db")
	if err := os.WriteFile(junk, bytes.Repeat([]byte("not a sqlite database "), 200), 0o644); err != nil {
		t.Fatal(err)
	}
	_, damaged := index.Open(junk)
	if damaged == nil {
		t.Fatal("fixture: junk opened")
	}
	capped, err := sql.Open("sqlite", filepath.Join(tmpDir(t), "capped.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer capped.Close()
	_, full := capped.Exec(`CREATE TABLE t(x); PRAGMA max_page_count = 2; INSERT INTO t VALUES (randomblob(65536));`)
	if full == nil {
		t.Fatal("fixture: no SQLITE_FULL")
	}
	live := filepath.Join(tmpDir(t), "live.db")
	ix, err := index.Open(live)
	if err != nil {
		t.Fatal(err)
	}
	ix.Close()
	mk := func() *sql.DB {
		db, err := sql.Open("sqlite", live)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		return db
	}
	holder := mk()
	if _, err := holder.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	other := mk()
	_, busy := other.Exec(`PRAGMA journal_mode=WAL;`)
	other.Close()
	holder.Close()
	if !index.Busy(busy) {
		t.Fatalf("fixture: not busy: %v", busy)
	}

	cases := []struct {
		name  string
		err   error
		wants []string
	}{
		{"damaged", fmt.Errorf("index pragmas: %w", damaged), []string{"damaged", rebuild}},
		{"busy", fmt.Errorf("index pragmas: %w", busy), []string{"in use by another program", "Close any other program"}},
		{"full disk", fmt.Errorf("index pragmas: %w", full), []string{"full or failing", "free space"}},
	}
	for _, c := range cases {
		err := indexOpenError(idxPath, out, c.err)
		var ie *IndexOpenError
		if !errors.As(err, &ie) {
			t.Fatalf("%s: not an *IndexOpenError: %T %v", c.name, err, err)
		}
		cli := assure.Reached(t, err.Error(), c.name+" CLI text")
		if c.name == "damaged" && !strings.Contains(cli, "mailarchive reindex -rebuild -out") {
			t.Fatalf("positive twin: the CLI text lost its command: %q", cli)
		}
		owner := assure.Reached(t, ie.ForOwner(rebuild), c.name+" owner text")
		for _, want := range append([]string{out}, c.wants...) {
			if !strings.Contains(owner, want) {
				t.Errorf("%s: owner text lacks %q: %q", c.name, want, owner)
			}
		}
		for _, bad := range []string{"mailarchive reindex", "SQLITE", "(5)", "(26)", "(13)", "database is locked", "not a database", "pragmas"} {
			if strings.Contains(owner, bad) {
				t.Errorf("%s: owner text carries %q: %q", c.name, bad, owner)
			}
		}
	}

	// No manifest to rebuild from: the owner is told to move the file aside.
	if err := os.Remove(mpath); err != nil {
		t.Fatal(err)
	}
	var ie *IndexOpenError
	if !errors.As(indexOpenError(idxPath, out, fmt.Errorf("index pragmas: %w", damaged)), &ie) {
		t.Fatal("no-manifest damaged index is not an *IndexOpenError")
	}
	owner := ie.ForOwner(rebuild)
	if !strings.Contains(owner, "no manifest") || !strings.Contains(owner, "Move search.db") || strings.Contains(owner, rebuild) {
		t.Errorf("no manifest: owner text must say to move search.db aside, not to rebuild: %q", owner)
	}
}
