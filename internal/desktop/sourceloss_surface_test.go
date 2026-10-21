package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/state"
)

func writeManifest(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, manifestName), []byte(`{"version":6,"stores":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-692
// The Desktop surfaces source-loss on the Your-archives card (archive-manager Slice 3c, S3-C6):
// archiveRows derives SourceLostSince from the archive-local source-loss streak — a "since DATE"
// string once the source has been unreachable past the surface threshold, "" below it — and the
// Overview renders a "source hasn't been reachable since DATE" banner for such an archive.
func TestMA692_SourceLossSurfacedOnArchiveCard(t *testing.T) {
	lost := t.TempDir()
	writeManifest(t, lost)
	if err := state.WriteSourceStreak(lost, state.SourceStreak{
		ConsecutiveFails: state.SurfaceAfterRuns,
		FirstUnreachable: time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatal(err)
	}
	okArch := t.TempDir()
	writeManifest(t, okArch)
	if err := state.WriteSourceStreak(okArch, state.SourceStreak{ConsecutiveFails: 1}); err != nil { // below the threshold
		t.Fatal(err)
	}

	// Derivation: the lost archive surfaces a since-date; the below-threshold one does not.
	rows := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json")}.archiveRows(
		archindex.Index{Entries: []archindex.Entry{{Path: lost}, {Path: okArch}}})
	byPath := map[string]ArchiveRow{}
	for _, r := range rows {
		byPath[r.Path] = r
	}
	if byPath[lost].SourceLostSince == "" {
		t.Fatal("an archive whose source is unreachable past the threshold must surface a since-date")
	}
	if s := byPath[okArch].SourceLostSince; s != "" {
		t.Fatalf("an archive below the threshold must not surface (got %q)", s)
	}

	// Rendering: the Overview shows the banner for the lost archive.
	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: lost}, {Path: okArch}}}); err != nil {
		t.Fatal(err)
	}
	page, _ := getPage(t, DashboardHandler(Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), IndexPath: idxPath, GOOS: "darwin"}))
	if !strings.Contains(page, "hasn't been reachable since") {
		t.Fatal("the Overview must render the source-loss banner for an archive whose source is lost")
	}
}
