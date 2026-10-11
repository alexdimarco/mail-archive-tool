package desktop

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/index"
)

// covers: MA-531, MA-434, R29, R12, S52
// Create folder never makes a directory in another archive folder, from the moment the owner has named it
// (E4-AI4): the guard was the archive session, which exists only after Load folders, so a Create folder
// whose parent was the folder just chosen in the Source control (validated, not yet loaded), or one made
// after the session was released at 30 idle minutes, wrote into it: a write into the folder MA-531 says is
// never written (it may be a read-only share, or a forensic copy). The positive twin: a folder outside it.
func TestCreateFolderNeverWritesInAnotherArchiveFolderTheOwnerHasNamed(t *testing.T) {
	e := newWsEnv(t)
	other := filepath.Join(e.dir, "inherited")
	buildArchiveAt(t, other)
	before := dirDigest(t, other)
	listing := func() string { return strings.Join(dirNames(t, other), ",") }
	was := listing()

	if code, out := e.post("/api/workspace/mkdir", map[string]any{"parent": e.dest, "name": "positive twin"}); code != 200 || out["ok"] != true {
		t.Fatalf("the positive twin, a folder outside the archive: %d %v", code, out)
	}
	// chosen in the Source control: validated, not loaded
	e.ok("/api/workspace/source", map[string]any{"otherArchive": other})
	code, body := e.post("/api/workspace/mkdir", map[string]any{"parent": other, "name": "New folder"})
	refusedBody(t, code, body, "R-f", "inside your archive", other)
	// loaded, then released after 30 idle minutes
	e.archFolders(archSource("archive-other", other))
	e.d.ws.reap(e.d.now().Add(2 * time.Hour))
	if e.d.ws.otherNow() != "" {
		t.Fatalf("the fixture: the archive session was not released")
	}
	code, body = e.post("/api/workspace/mkdir", map[string]any{"parent": other, "name": "New folder"})
	refusedBody(t, code, body, "R-f", "inside your archive", other)
	if got := listing(); got != was || dirDigest(t, other) != before {
		t.Errorf("Create folder wrote into the folder: %s, was %s", got, was)
	}
}

// covers: MA-435, MA-531, R29, S52
// The preview's read of the index (the subject, sender and date of each message of the selected folders)
// stops when the preview is cancelled (E4-AC7): it ignored the context, so a cancelled preview of a
// folder of a million rows read them all, holding the list gate Rebuild waits behind, and the index
// is closed all the same.
func TestArchiveMetaStopsWhenThePreviewIsCancelled(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	a, err := app.OpenArchive(e.out, app.ArchiveOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	var ids []string
	for _, n := range a.Tree() {
		ids = append(ids, n.ID)
	}
	meta, _, err := e.d.archiveMeta(context.Background(), a, ids) // the positive twin: nobody cancels
	if err != nil || len(meta) != 6 {
		t.Fatalf("the positive twin: %d rows, %v", len(meta), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	meta, _, err = e.d.archiveMeta(ctx, a, ids)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("a cancelled preview's read of the index: %v, want context.Canceled", err)
	}
	if len(meta) != 0 {
		t.Errorf("a cancelled preview read %d rows of the index", len(meta))
	}
	if n := index.OpenListHandles(); n != 0 {
		t.Errorf("%d index handle(s) left open", n)
	}
}

// covers: MA-435, MA-531, R29, S52
// The text of a row of an archive's index is shown with no control character and no bidirectional
// formatting control in it (E4-AO7): a right-to-left override in a subject or a sender's name made the
// rest of the line in the page read reversed. The list returns the text with only the controls gone.
func TestArchiveListRowsCarryNoBidiControls(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	db, err := sqlOpen(f.path("search.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE docs SET subject = ?, sender_name = ?, sender_email = ? WHERE key = ?`,
		"‮exe.gnp invoice⁦", "A‮⁩B", "x‮@example.com", f.key("hello")); err != nil {
		t.Fatal(err)
	}
	db.Close()
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	_, out := e.archRows(arch, "alice")
	found := false
	for _, r := range out["rows"].([]any) {
		row := r.(map[string]any)
		for _, k := range []string{"subject", "from", "fromName"} {
			if s, _ := row[k].(string); strings.ContainsAny(s, "‪‫‬‭‮⁦⁧⁨⁩") {
				t.Errorf("the row's %s %q carries a bidi control", k, s)
			}
		}
		if row["subject"] == "exe.gnp invoice" {
			found = true
			if row["fromName"] != "AB" || row["from"] != "x@example.com" {
				t.Errorf("the row's sender: %v", row)
			}
		}
	}
	if !found {
		t.Errorf("the row's text was lost, not just its controls: %v", out["rows"])
	}
}
