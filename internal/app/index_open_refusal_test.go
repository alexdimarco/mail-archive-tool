package app

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
)

// covers: MA-386, R12, S33
// An export or a Graph run whose search.db is present but damaged (not a
// database) fails at once with a refusal that names the file and the remedy —
// the rebuild from the archive's own files, `mailarchive reindex -rebuild -out
// DIR` — never SQLite's bare "index pragmas: file is not a database". The
// refusal writes nothing: the damaged file and the manifest keep their bytes.
// The remedy, followed, heals: the rebuild replaces the damaged file and the
// next run succeeds with every record indexed. A folder with no manifest has
// nothing to rebuild from, so there the refusal names moving the file aside,
// never the rebuild, and that remedy heals too. Positive twin first: the same
// run over the healthy index succeeds.
func TestRunOverADamagedIndexNamesTheRebuild(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	logger := log.New(io.Discard, "", 0)
	_, srv := newFakeGraphServer()
	defer srv.Close()
	g := GraphOptions{Tenant: "t", ClientID: "c", ClientSecret: "s", Mailboxes: []string{"u1"}, BaseURL: srv.URL, TokenURL: srv.URL + "/token"}
	junk := bytes.Repeat([]byte("this is not a sqlite database "), 200)
	refusal := func(err error) (int, string) {
		if err != nil {
			return 1, err.Error()
		}
		return 0, ""
	}

	for _, r := range []struct {
		name string
		run  func(out string) (Result, error)
	}{
		{"local", func(out string) (Result, error) {
			return Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, logger, nil)
		}},
		{"graph", func(out string) (Result, error) {
			return RunGraph(context.Background(), g, Options{Out: out, Mode: export.Incremental, Index: true}, logger)
		}},
	} {
		t.Run(r.name, func(t *testing.T) {
			out := tmpDir(t)
			idxPath := filepath.Join(out, "search.db")
			mpath := filepath.Join(out, ".mailarchive-manifest.json")
			healthy, err := r.run(out)
			if err != nil {
				t.Fatalf("positive twin: the run over a healthy index failed: %v", err)
			}
			assure.Reached(t, healthy.Indexed, "messages the healthy run indexed")

			removeDBFiles(idxPath)
			if err := os.WriteFile(idxPath, junk, 0o644); err != nil {
				t.Fatal(err)
			}
			manifest, err := os.ReadFile(mpath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = r.run(out)
			code, msg := refusal(err)
			assure.Refused(t, code, msg, assure.Code(1),
				assure.Names(idxPath, "file is not a database", "mailarchive reindex -rebuild -out", out),
				assure.NoSideEffect(func() bool {
					db, derr := os.ReadFile(idxPath)
					m, merr := os.ReadFile(mpath)
					return derr == nil && merr == nil && bytes.Equal(db, junk) && bytes.Equal(m, manifest)
				}))

			followReindexRemedy(t, msg, out)
			res, err := r.run(out)
			if err != nil {
				t.Fatalf("after following %q the run still fails: %v", msg, err)
			}
			if res.Indexed != res.ManifestSize || res.Indexed != healthy.Indexed {
				t.Fatalf("after following the remedy the index holds %d of %d records, want all %d", res.Indexed, res.ManifestSize, healthy.Indexed)
			}
		})
	}

	t.Run("no manifest to rebuild from", func(t *testing.T) {
		out := tmpDir(t)
		idxPath := filepath.Join(out, "search.db")
		if err := os.WriteFile(idxPath, junk, 0o644); err != nil {
			t.Fatal(err)
		}
		opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}
		_, err := Run(context.Background(), opts, logger, nil)
		code, msg := refusal(err)
		assure.Refused(t, code, msg, assure.Code(1),
			assure.Names(idxPath, "file is not a database", "no manifest", "move it out of"),
			assure.Forbid("reindex -rebuild"),
			assure.NoSideEffect(func() bool {
				db, derr := os.ReadFile(idxPath)
				_, merr := os.Stat(filepath.Join(out, ".mailarchive-manifest.json"))
				return derr == nil && bytes.Equal(db, junk) && errors.Is(merr, fs.ErrNotExist)
			}))
		// Followed: the file moved out of the archive folder, the run builds
		// a fresh index and succeeds.
		if err := os.Rename(idxPath, filepath.Join(tmpDir(t), "search.db")); err != nil {
			t.Fatal(err)
		}
		res, err := Run(context.Background(), opts, logger, nil)
		if err != nil || res.Indexed != 3 {
			t.Fatalf("after moving the damaged file aside: indexed=%d err=%v, want all 3", res.Indexed, err)
		}
	})
}

// covers: MA-386, R12, S33
// The rebuild is the remedy for a damaged index only: a run refused for any
// other reason keeps the remedy its cause has. An index written by a newer
// mailarchive keeps its own refusal (the file and the upgrade), and a busy
// index — another connection held it past the writable open's wait; here a
// real SQLITE_BUSY from a foreign exclusive lock — is named busy with the
// retry. Neither is ever told to rebuild, which would replace a newer index
// with an older one, or a healthy index that was merely in use. Positive twin
// first: the run over the healthy index succeeds.
func TestIndexOpenRefusalKeepsItsCause(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	logger := log.New(io.Discard, "", 0)
	out := tmpDir(t)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}
	if _, err := Run(context.Background(), opts, logger, nil); err != nil {
		t.Fatalf("positive twin: the run over a healthy index failed: %v", err)
	}
	idxPath := filepath.Join(out, "search.db")
	raw := func() *sql.DB { // a foreign connection with no busy wait: a held lock answers at once
		t.Helper()
		db, err := sql.Open("sqlite", idxPath)
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		return db
	}

	// Busy: one foreign connection holds search.db in exclusive locking mode,
	// so the writable open's first statement (index.Open's pragmas) is refused
	// SQLITE_BUSY. Waiting out index.Open's own 5 s busy timeout through Run
	// would only delay that same error, so it is built from the real refusal.
	holder := raw()
	if _, err := holder.Exec(`PRAGMA locking_mode=EXCLUSIVE; BEGIN; UPDATE meta SET version = version; COMMIT;`); err != nil {
		t.Fatal(err)
	}
	other := raw()
	_, busy := other.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`)
	other.Close()
	holder.Close()
	if !index.Busy(busy) {
		t.Fatalf("fixture: the foreign exclusive lock did not make the open busy: %v", busy)
	}
	err := indexOpenError(idxPath, out, fmt.Errorf("index pragmas: %w", busy))
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "busy", "close any other program", "run again"),
		assure.Forbid("reindex -rebuild", "cannot be opened"))

	// Newer: the index's stored format version is above this build's.
	db := raw()
	if _, err := db.Exec(`DELETE FROM meta; INSERT INTO meta(version) VALUES(99);`); err != nil {
		t.Fatal(err)
	}
	db.Close()
	_, err = Run(context.Background(), opts, logger, nil)
	code, msg = 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "newer mailarchive", "upgrade"),
		assure.Forbid("reindex -rebuild", "cannot be opened"))
}
