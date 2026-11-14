package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/state"
)

// Slice B2 of docs/design-gmail-dedup.md, MA-1216's rows for a build without the merge step
// (moved from MA-1229 by fold check 6's M3): this build reads at most version 7, so it is a
// genuine "build capped at 7" and no seam is used. A version-8 archive (a merge part-way, written
// by the test) is refused by the backup, reindex, rebuild and verify -record, each after its
// positive twin on a version-7 archive, and nothing the merge owns is touched.

// dedupArchive is a real plaintext archive of three messages with their originals (-raw), made by
// a backup, then switched on to the Gmail rule with one membership (version 7). With pending, a
// merge part-way is written over it by hand, as the merge step would leave it after a batch's
// fold save: message 1 folded into message 0, its record gone and its page and original named by
// MergedFiles entries on the survivor, still on disk; the manifest is then version 8. A planted
// old temp file and a planted zip of an unowned stem whose page is gone sit in the archive.
func dedupArchive(t *testing.T, pending bool) (out string, owned []string) {
	t.Helper()
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	out = tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, KeepRaw: true, Index: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(out, "search.db")); err != nil {
		t.Fatalf("the fixture has no search index: %v", err)
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	keys := make([]string, 0, 3)
	for k := range m.All() {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if len(keys) != 3 {
		t.Fatalf("the fixture archived %d messages, want 3", len(keys))
	}
	tok, _, _ := strings.Cut(keys[0], "\x00")
	survivor, _ := m.Get(keys[0])
	survivor.Folders = []state.Membership{{Folder: "Sent", Kind: state.MemberCopy, FirstSeen: survivor.ExportedAt}}
	m.Add(keys[0], survivor)
	m.SwitchOnGmail(tok)
	if pending {
		copyRec, _ := m.Get(keys[1])
		if copyRec.Fixity == nil || copyRec.Fixity.EML == nil || copyRec.Fixity.HTML == nil || survivor.Fixity == nil || survivor.Fixity.EML == nil {
			t.Fatal("the fixture's records carry no fixity for their page and original")
		}
		stem := strings.TrimSuffix(copyRec.Path, ".html")
		survivor.MergedFiles = []state.MergedFile{
			{Path: copyRec.Path, Role: "html", Size: copyRec.Fixity.HTML.Size, SHA256: copyRec.Fixity.HTML.SHA256, Proof: survivor.Fixity.EML.SHA256},
			{Path: stem + ".eml", Role: "eml", Size: copyRec.Fixity.EML.Size, SHA256: copyRec.Fixity.EML.SHA256, Proof: survivor.Fixity.EML.SHA256},
		}
		m.Add(keys[0], survivor)
		m.Delete(keys[1])
		m.MergePending = &state.MergePending{PlanDigest: strings.Repeat("d", 64), Started: time.Now().UTC(),
			Groups: []state.MergeGroup{{Survivor: keys[0], Copies: []string{keys[1]}}}, Folded: 1}
		owned = []string{copyRec.Path, stem + ".eml"}
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	want := `{"version":7,`
	if pending {
		want = `{"version":8,`
	}
	if data, _ := os.ReadFile(mpath); !strings.HasPrefix(string(data), want) {
		t.Fatalf("the fixture's manifest starts %.20s, want %s", data, want)
	}
	old := time.Now().Add(-time.Hour)
	pageDir := filepath.Join(out, filepath.FromSlash(filepath.ToSlash(filepath.Dir(survivor.Path))))
	for _, p := range []string{filepath.Join(out, ".mailarchive-planted.tmp"), filepath.Join(pageDir, "unowned-attachments.zip")} {
		if err := os.WriteFile(p, []byte("planted"), 0o644); err != nil {
			t.Fatal(err)
		}
		os.Chtimes(p, old, old)
	}
	return out, owned
}

// tree is every file under out, by its path relative to out, with its SHA-256.
func tree(t *testing.T, out string) map[string]string {
	t.Helper()
	files := map[string]string{}
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		data, rerr := os.ReadFile(p)
		if rerr != nil {
			t.Fatal(rerr)
		}
		sum := sha256.Sum256(data)
		rel, _ := filepath.Rel(out, p)
		files[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	return files
}

// changes lists what differs between two trees: created, changed and removed paths.
func changes(before, after map[string]string) (created, changed, removed []string) {
	for p, s := range after {
		switch b, ok := before[p]; {
		case !ok:
			created = append(created, p)
		case b != s:
			changed = append(changed, p)
		}
	}
	for p := range before {
		if _, ok := after[p]; !ok {
			removed = append(removed, p)
		}
	}
	sort.Strings(created)
	sort.Strings(changed)
	sort.Strings(removed)
	return
}

// refusedAs8 checks err is the newer-format refusal naming version 8 with the tidy-up words.
func refusedAs8(t *testing.T, what string, err error) {
	t.Helper()
	var nf *state.NewerFormatError
	if !errors.As(err, &nf) || nf.Version != 8 || nf.Understood != 7 {
		t.Fatalf("%s on a version-8 archive: %v, want the newer-format refusal naming version 8", what, err)
	}
	if !strings.Contains(err.Error(), state.TidyUpWords) {
		t.Fatalf("%s: the refusal does not say the tidy-up words: %v", what, err)
	}
}

// covers: MA-1216, R45, S82, S83
// The backup, as a scheduled run takes it (unattended, the Thunderbird pre-sync hook on), on a
// version-8 archive: refused with the newer-format refusal naming version 8 and the tidy-up words,
// also in the run's failure record. A build without the merge step has no header read, so the
// pre-sync hook is called once and the start sweep really runs before the refusal (the planted
// temp and the page-less zip are gone) and removes nothing the merge owns. The manifest, the
// index and every message file are byte for byte as before; the only files the backup creates or
// changes are its last-run record, its run-history entry and the lock (fold check 6's S5 list for
// a local run). Positive twin first: the same backup into a version-7 archive runs.
func TestABuildWithoutTheMergeStepRefusesAPendingMergesBackup(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	backup := func(out string, calls *int) error {
		src := filepath.Join(tmpDir(t), "Inbox")
		maildirMessages(t, src, 3)
		_, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, KeepRaw: true, Index: true, Unattended: true,
			BeforeRead: func(ctx context.Context, files []string) BeforeReadResult { *calls++; return BeforeReadResult{} }}, logger, nil)
		return err
	}

	twin, _ := dedupArchive(t, false)
	calls := 0
	if err := backup(twin, &calls); err != nil || calls != 1 {
		t.Fatalf("positive twin: a backup into a version-7 archive: %v (pre-sync called %d times)", err, calls)
	}

	out, owned := dedupArchive(t, true)
	before := tree(t, out)
	calls = 0
	err := backup(out, &calls)
	refusedAs8(t, "the backup", err)
	if calls != 1 {
		t.Fatalf("the pre-sync hook was called %d times; a build without the merge step calls it once, before its load refuses", calls)
	}
	after := tree(t, out)
	created, changed, removed := changes(before, after)
	// S5's list, plus the attention note a failed scheduled run leaves at the root (found here:
	// fold check 6's S5 list does not name it; a root text file, not a message file).
	allowed := map[string]bool{state.LastRunName: true, state.RunHistoryName: true, lockfile.Name: true, state.AttentionName: true}
	for _, p := range append(created, changed...) {
		if !allowed[p] {
			t.Errorf("the refused backup created or changed %s; only its run records and the lock may change", p)
		}
	}
	wantRemoved := []string{".mailarchive-planted.tmp", filepath.ToSlash(filepath.Dir(owned[0])) + "/unowned-attachments.zip"}
	sort.Strings(wantRemoved)
	if strings.Join(removed, " ") != strings.Join(wantRemoved, " ") {
		t.Fatalf("the sweep removed %v, want exactly the planted temp and page-less zip %v (it ran, and removed nothing the merge owns)", removed, wantRemoved)
	}
	for _, p := range owned {
		if _, ok := after[p]; !ok {
			t.Fatalf("a file the merge owns (%s) is gone", p)
		}
	}
	// Amended for the B3 UX walk's U2 (the lead's call, MA-1310): the refusal is a skip, not a
	// failed run, so the archive's last-run record is put back exactly as it was before the backup
	// (it said a failure before U2), and the refusal is said by the run's own error.
	if after[state.LastRunName] != before[state.LastRunName] {
		t.Fatalf("the skipped backup left a record in the archive: before %q, after %q", before[state.LastRunName], after[state.LastRunName])
	}
	if lr, st, _ := state.ReadLastRun(out); st == state.LastRunPresent && lr.Status == state.RunFailed && strings.Contains(lr.Error, state.TidyUpWords) {
		t.Fatal("the skipped backup is recorded as a failed run")
	}
	if !TidyingElsewhere(err) {
		t.Fatalf("the refusal is not told as another computer's tidy-up: %v", err)
	}
}

// covers: MA-1216, R45, S82, S83
// reindex, rebuild and verify -record on a version-8 archive each refuse with the newer-format
// refusal naming version 8, and change no file at all: the manifest, every message file, the
// planted files and search.db with its -wal and -shm are byte for byte as before. reindex loads
// the manifest before it opens the index (fold check 6's S6), so the index's header is not
// written either. Each after its positive twin on a version-7 archive.
func TestABuildWithoutTheMergeStepRefusesAPendingMergesMaintenance(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	programs := []struct {
		name string
		run  func(out string) error
	}{
		{"reindex", func(out string) error { _, _, err := Reindex(out, logger); return err }},
		{"rebuild", func(out string) error { _, err := Rebuild(out, logger); return err }},
		{"verify -record", func(out string) error {
			_, err := Verify(out, VerifyOptions{Record: true}, logger, nil)
			return err
		}},
	}
	for _, p := range programs {
		twin, _ := dedupArchive(t, false)
		if err := p.run(twin); err != nil {
			t.Fatalf("positive twin: %s on a version-7 archive: %v", p.name, err)
		}
		out, owned := dedupArchive(t, true)
		before := tree(t, out)
		refusedAs8(t, p.name, p.run(out))
		created, changed, removed := changes(before, tree(t, out))
		for _, c := range append(append(created, changed...), removed...) {
			if c != lockfile.Name {
				t.Errorf("%s on a version-8 archive created, changed or removed %s (search.db and its -wal/-shm included)", p.name, c)
			}
		}
		for _, f := range owned {
			if _, err := os.Stat(filepath.Join(out, filepath.FromSlash(f))); err != nil {
				t.Fatalf("%s: a file the merge owns is gone: %v", p.name, err)
			}
		}
	}
}

// covers: MA-1236, R45, S82
// An existing archive nobody switched on stays version 6, with a version-3 index, through this
// build's backups, its rebuild and its verify -record, so v1.6.1 and 1.7.x keep backing up into
// it; the switch is the person's alone (Q-B).
func TestAnArchiveNobodySwitchedOnStaysVersion6(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	for i := 0; i < 2; i++ {
		if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, logger, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Rebuild(out, logger); err != nil {
		t.Fatal(err)
	}
	if _, err := Verify(out, VerifyOptions{Record: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(mpath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), `{"version":6,`) || strings.Contains(string(data), "dedup_stores") {
		t.Fatalf("an archive nobody switched on was written as %.40s", data)
	}
	db, err := sql.Open("sqlite", filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT version FROM meta LIMIT 1`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if assure.Reached(t, v, "the index's stored version") != 3 {
		t.Fatalf("its index is version %d, want 3", v)
	}
}
