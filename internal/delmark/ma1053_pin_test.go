package delmark_test

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1053, R36
// The name is frozen (DM-24, §15.1): every later version refuses on its presence, so
// it is pinned here as the pause seal's is; and the refusal kinds delmark owns are the
// failure kinds the engine and the alerts know.
func TestTheMarkersNameAndKindsArePinned(t *testing.T) {
	if delmark.Name != ".mailarchive-deleting.json" {
		t.Fatalf("the delete marker's name changed: %q", delmark.Name)
	}
	if delmark.KindUnfinished != state.FailDeleteUnfinished || delmark.KindCouldNotCheck != state.FailArchiveUnwritable {
		t.Fatalf("kinds %q %q, want %q %q", delmark.KindUnfinished, delmark.KindCouldNotCheck, state.FailDeleteUnfinished, state.FailArchiveUnwritable)
	}
	if delmark.Version != 1 || delmark.MaxRead != 4096 || delmark.What != "MailArchive delete marker" {
		t.Fatalf("version %d, cap %d, what %q", delmark.Version, delmark.MaxRead, delmark.What)
	}
	if delmark.Note != "A MailArchive delete of this archive is under way or did not finish. MailArchive does not back up into this folder until the delete is finished: click Delete on its row in MailArchive Desktop." {
		t.Fatalf("note %q", delmark.Note)
	}
}

// covers: MA-1053, R36
// delmark is a leaf (§15.3): the standard library and internal/util only, so the
// engine can ask it where archdelete's check is not installed, and archdelete, encmark
// and health can import it for the name without a cycle.
func TestDelmarkIsALeaf(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	scanned := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(token.NewFileSet(), f, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		scanned++
		for _, imp := range parsed.Imports {
			p := strings.Trim(imp.Path.Value, `"`)
			if strings.Contains(strings.SplitN(p, "/", 2)[0], ".") || (strings.HasPrefix(p, "mail-archive-tool/") && p != "mail-archive-tool/internal/util") {
				t.Errorf("%s imports %s: delmark imports only the standard library and internal/util", f, p)
			}
		}
	}
	if scanned == 0 {
		t.Fatal("no source file of delmark was read")
	}
}

// covers: MA-1053, R36
// No run's crash sweep removes the marker (§15.1: it matches neither
// `.mailarchive-*.tmp` nor `.mailarchive-snapshot-*`), and it is not an encrypted
// archive's temp by its shape. Positive twin first: an old temp and an old snapshot
// beside it are swept.
func TestNoSweepTakesTheMarker(t *testing.T) {
	out := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	for _, n := range []string{".mailarchive-manifest-12.tmp", export.SnapshotPrefix + "1.pst", delmark.Name} {
		p := filepath.Join(out, n)
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if n := export.SweepOrphans(out, time.Now(), nil); n != 2 {
		t.Fatalf("swept %d files, want the temp and the snapshot", n)
	}
	if _, err := os.Lstat(filepath.Join(out, delmark.Name)); err != nil {
		t.Fatalf("a sweep removed the delete marker: %v", err)
	}
	if encmark.IsReservedTemp(delmark.Name) {
		t.Fatal("the marker reads as an encrypted archive's temp")
	}
}

// covers: MA-1053, R36
// One owner of the marker-only words (DM-24): the refusals' words are delmark's,
// exactly §15.3's builder's words (DM-11 for a delete only the marker knows; a name
// that is not a regular file, for a backup and for Delete's plan).
func TestTheMarkerOnlyWordsAreDelmarks(t *testing.T) {
	for _, c := range [][2]string{
		{delmark.MsgOnlyMarker, "Delete not finished: a delete of this archive was begun on another computer or user account, or this computer's deletion log has no record of it, and it did not finish, so it was not backed up; nothing was changed. To finish it, click Delete… on its row in MailArchive Desktop."},
		{delmark.OnlyMarkerTerminal(`"/Volumes/Backup/Mail"`), `To finish it at this terminal: mailarchive delete-archive -out "/Volumes/Backup/Mail"`},
		{delmark.MsgNotRegular, "This archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a link, a folder or a device), so it was not backed up; nothing was changed. If no delete of this archive is under way, remove it by hand, then try again."},
		{delmark.MsgPlanNotRegular, "This archive's folder holds .mailarchive-deleting.json, which MailArchive did not write (it is a folder or a device): remove it by hand, then click Delete again."},
	} {
		if c[0] != c[1] {
			t.Errorf("words:\n got  %q\n want %q", c[0], c[1])
		}
	}
}
