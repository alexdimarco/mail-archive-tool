package wizard

import (
	"context"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
)

// archiveFiles lists every file in the archive folder, by its path inside it.
func archiveFiles(t *testing.T, out string) []string {
	t.Helper()
	var files []string
	if err := filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

// covers: MA-943
// "N messages already in the archive were saved again (no copies are made)." (the guide's
// friction review O4, applied by the lead, 2026-10-31: "saved again" alone may read as
// duplicates). It is true of the exporter for the archives Run now backs up (mail files on
// this computer): a message written again replaces its record under the same key, and its
// earlier page is deleted when its name changed (exporter.go, R6/R13); the one case that
// keeps an earlier file, adopting a legacy record, is the Microsoft 365 path only
// (DedupMailboxWide, app/graph.go), which Run now never runs. Driven on a real local mbox
// folder: a first backup, then a run that writes every message again (Full), leaves the
// archive's files and its record count as they were, and Run now's note says so.
func TestSavedAgainMakesNoCopies(t *testing.T) {
	store := filepath.Join(t.TempDir(), "Mail", "Local Folders")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	msg := func(id string) string {
		return "From - Mon Mar  3 09:00:00 2025\nFrom: a@example.com\nTo: b@example.com\nSubject: " + id +
			"\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + id + "@ex>\n\nbody " + id + "\n\n"
	}
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(msg("one")+msg("two")+msg("three")), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "arch")
	run := func(mode export.Mode) app.Result {
		r, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: out, Mode: mode, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	first := run(export.Incremental)
	if first.ManifestSize != 3 {
		t.Fatalf("fixture: the first backup holds %d messages, want 3", first.ManifestSize)
	}
	before := archiveFiles(t, out)
	assure.Reached(t, before, "the archive's files after the first backup")

	again := run(export.Full)
	if again.Stats.Exported != 3 || again.ManifestBefore != 3 || again.ManifestSize != 3 {
		t.Fatalf("fixture: the second run wrote %d messages, record %d → %d; want every message written again and no growth",
			again.Stats.Exported, again.ManifestBefore, again.ManifestSize)
	}
	after := archiveFiles(t, out)
	var pages int
	for _, f := range after {
		if strings.HasSuffix(f, ".html") && !strings.HasSuffix(f, "index.html") {
			pages++
		}
	}
	if strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Errorf("writing the messages again changed the archive's files:\nbefore %q\nafter  %q", before, after)
	}
	if pages < 3 {
		t.Errorf("fixture: %d message pages in the archive, want at least 3", pages)
	}
	if got, want := runNowSummary(again), "3 messages already in the archive were saved again (no copies are made)."; !strings.Contains(got, want) {
		t.Errorf("Run now's note is %q, want it to say %q", got, want)
	}
}
