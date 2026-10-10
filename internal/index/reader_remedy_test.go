package index

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-386, R12, S33
// The reader's refusals check that their remedy can work (g7 friction F6).
// search, serve and status name `reindex -rebuild` for a damaged or lost
// search.db, but the rebuild refuses an archive with no manifest. With a
// manifest beside it a lost search.db names the rebuild (never "run an export
// first": the archive was exported) and a damaged one names it too; with none, a
// lost one keeps "run an export first" and a damaged one names that there is
// no manifest and moving the file aside. Nothing is written.
func TestReaderRefusalsNameARemedyThatWorks(t *testing.T) {
	refusal := func(path string) (int, string) {
		ix, err := OpenReadonly(path)
		if err != nil {
			return 1, err.Error()
		}
		ix.Close()
		return 0, ""
	}
	junk := bytes.Repeat([]byte("not a sqlite database, just junk bytes. "), 128)

	// Positive twin: a damaged search.db beside its manifest names the rebuild.
	dir := tmpDir(t)
	if err := os.WriteFile(filepath.Join(dir, ".mailarchive-manifest.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "search.db")
	if err := os.WriteFile(path, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	code, msg := refusal(path)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(path, "reindex -rebuild -out"))

	// Lost search.db, manifest intact: the rebuild, not "run an export first".
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	code, msg = refusal(path)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(path, "manifest is intact", "reindex -rebuild -out"),
		assure.Forbid("run an export first"))

	// No manifest: a lost search.db still means nothing was exported.
	bare := tmpDir(t)
	bpath := filepath.Join(bare, "search.db")
	code, msg = refusal(bpath)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(bpath, "run an export first"))

	// No manifest, damaged: the rebuild would refuse, so say to move it aside.
	if err := os.WriteFile(bpath, junk, 0o644); err != nil {
		t.Fatal(err)
	}
	code, msg = refusal(bpath)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(bpath, "no manifest", "move search.db out of "+bare),
		assure.NoSideEffect(func() bool {
			got, err := os.ReadFile(bpath)
			return err == nil && bytes.Equal(got, junk)
		}))
}
