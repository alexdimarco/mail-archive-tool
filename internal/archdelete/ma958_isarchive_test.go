package archdelete

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
)

// bigArchive is a plain archive whose manifest is over 1 MiB: 5,000 messages.
func bigArchive(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	var recs []rec
	for i := 0; i < 5000; i++ {
		recs = append(recs, rec{Path: fmt.Sprintf("Mailbox/Inbox/2024-03-01_0900_a-long-subject-slug-for-a-real-message-of-the-mailbox_%08x.html", i), Date: "2024-03-01T09:00:00Z"})
	}
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, recs)
	writeFile(t, filepath.Join(dir, filepath.FromSlash(recs[0].Path)), "page")
	fi, err := os.Stat(filepath.Join(dir, manifestName))
	if err != nil || fi.Size() <= 1<<20 {
		t.Fatalf("fixture must exceed 1 MiB: %v %v", fi, err)
	}
	return dir
}

// covers: MA-958, R36
func TestAnArchiveWithAManifestOverOneMiBIsPlanned(t *testing.T) {
	dir := bigArchive(t)
	// Positive twin first: today's list reads it as no archive (1 MiB cap, C10's
	// separate row) — the defect the delete must not inherit.
	if archindex.IsArchive(dir) {
		t.Fatal("expected archindex's capped reader to miss this archive")
	}
	if !IsArchive(dir) {
		t.Fatal("§4's archive test must see an archive with a manifest over 1 MiB")
	}
	p := mustPlan(t, dir, testDeps())
	if p.Kind != Plain || p.Captured.Messages != 5000 || len(p.Remove) != 2 {
		t.Fatalf("plan = kind %v, %d messages, %d removals", p.Kind, p.Captured.Messages, len(p.Remove))
	}
	// Remove from list of the current archive is refused while it is reachable and
	// an archive.
	deps := testDeps()
	deps.Settings.Current = dir
	err := UnlistRefusal(dir, deps)
	r := AsRefusal(err)
	if r == nil {
		t.Fatalf("Remove from list of the current over-1-MiB archive must be refused, got %v", err)
	}
	assure.Refused(t, r.ExitCode(), r.Message, assure.Code(1), assure.Names("This is the archive in use", "Add another archive", "remove this one from the list"))
}

// covers: MA-958, R36
func TestAFolderWithNoArchiveIsRefusedAsNotAnArchive(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "taxes.pdf"), "mine")
	writeFile(t, filepath.Join(dir, "Mailbox", "x.html"), "mine")
	before := tree(t, dir)
	// Positive twin: with a manifest the same folder is planned.
	twin := t.TempDir()
	writeManifest(t, twin, 6, map[string]string{"path:/m": "Mailbox"}, nil)
	mustPlan(t, twin, testDeps())

	rc, msg, c := refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("This folder no longer holds an archive. Use Remove from list."),
		assure.NoSideEffect(func() bool { return sameTree(before, tree(t, dir)) }))
	if c != CaseNotArchive || IsArchive(dir) {
		t.Fatalf("case = %s", c)
	}
	// A manifest that does not parse is no archive either.
	writeFile(t, filepath.Join(dir, manifestName), "{not json")
	rc, msg, _ = refusalOf(t, dir, testDeps())
	assure.Refused(t, rc, msg, assure.Code(1), assure.Names("no longer holds an archive"))
}
