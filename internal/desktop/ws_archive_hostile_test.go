package desktop

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
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

// covers: MA-437, MA-512, R29, R5, R12
// "Export the remaining messages" does not stay offered for a message that is no longer in the archive
// (E4-AC10): a retry of an R-o2 stop drops the records whose files were removed since (a redaction) from its
// plan, so no part of the set ever recorded them, and part 1's card went on offering to export what
// the archive no longer has: a button that answered R-h for ever, and a set that read "5 of 6 exported so
// far" for ever. The retry now carries such a message into its plan, where it is listed as removed from the
// archive (terminal) as a message of Microsoft 365 that is gone is, and a message another part settled that
// way is not offered again.
func TestExportTheRemainingMessagesIsNotOfferedForAMessageTheArchiveNoLongerHas(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	os.Remove(e.tok)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	id, res := e.preview(e.archSel(arch, entry("alice", true), entry("bob", false)))
	if res["count"] != float64(6) {
		t.Fatalf("the preview: %v", res)
	}
	stash := filepath.Join(e.dir, "stash")
	os.MkdirAll(stash, 0o755)
	ents, _ := os.ReadDir(e.out)
	for _, ent := range ents {
		os.Rename(filepath.Join(e.out, ent.Name()), filepath.Join(stash, ent.Name()))
	}
	run := e.ok("/api/exports/start", map[string]any{"previewId": id})["runId"].(string)
	if last := e.waitRun()["last"].(map[string]any); last["stopId"] != "R-o2" {
		t.Fatalf("the stopped export: %v", last)
	}
	for _, ent := range ents { // reconnected, and a redaction has removed one message's files since
		os.Rename(filepath.Join(stash, ent.Name()), filepath.Join(e.out, ent.Name()))
	}
	rec := f.rec("hello")
	os.Remove(f.path(rec.Path))
	os.Remove(f.path(strings.TrimSuffix(rec.Path, ".html") + ".eml"))

	rid := e.ok("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})["previewId"].(string)
	rres := e.waitPreview(rid)
	if w := strings.Join(toStrings(rres["warnings"]), " | "); !strings.Contains(w, "no longer in the archive") {
		t.Errorf("the retry's preview does not say a message is gone: %q", w)
	}
	e.ok("/api/exports/start", map[string]any{"previewId": rid})
	l := e.waitRun()["last"].(map[string]any)
	if l["exported"] != float64(5) || l["failed"] != float64(1) {
		t.Errorf("the second part: %v, want 5 exported and the removed message listed", l)
	} else if man := e.manifestOf(l["rootName"].(string)); man["failures"] == nil || !strings.Contains(man["failures"].([]any)[0].(map[string]any)["reason"].(string), "removed from the archive") {
		t.Errorf("the second part's failures: %v", man["failures"])
	}
	if card := e.card(run); hasStr(card["actions"], "export-remaining") {
		code, out := e.post("/api/exports/retry", map[string]any{"runId": run, "which": "rest"})
		t.Errorf("part 1 still offers Export the remaining messages (it answers %d %v)", code, out["id"])
	}
}

// covers: MA-435, R20, R29, S52
// A list that is not what the owner loaded says so (E4-AC5): the tree and its counts are the manifest read at
// "Load folders", and the list is the index as it is now, paged by offset. After a capture the newest-first
// first page held nothing the snapshot knew ("Showing 0 of 2", a Load more button, no word why), and a message
// added between two pages repeats one at the page break. The list now says so when rows it read are not in
// the snapshot, and when the archive's manifest changed since it was loaded. The positive twin: a list of an
// archive that did not change says nothing of the kind.
func TestArchiveListSaysWhenTheArchiveChangedSinceItWasLoaded(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	arch := archSource("archive", e.out)
	e.archFolders(arch)
	_, out := e.archRows(arch, "alice")
	if n, _ := out["listNote"].(string); n != "" || len(out["rows"].([]any)) != 2 {
		t.Fatalf("the positive twin, a list of an archive that did not change: %v", out)
	}

	// 150 newer messages in the index, which the loaded tree does not know (the manifest is not touched)
	db, err := sqlOpen(f.path("search.db"))
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 150; i++ {
		if _, err := db.Exec(`INSERT INTO docs(key, store, folder, subject, date, path, has_attach) VALUES (?, 'alice', '', ?, ?, ?, 0)`,
			"alice\x00\x00newer"+string(rune('A'+i%26))+string(rune('a'+i/26)), "newer message", 1900000000+i, "alice/new/x.html"); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	_, out = e.archRows(arch, "alice")
	n, _ := out["listNote"].(string)
	if len(out["rows"].([]any)) != 0 || out["more"] != true || !strings.Contains(n, "not shown") || !strings.Contains(n, "Load folders again") {
		t.Errorf("the first page of 100 rows the loaded tree does not know: rows %d, more %v, note %q; want it said why the page is empty", len(out["rows"].([]any)), out["more"], n)
	}

	// the manifest changed since it was loaded (a capture saved its checkpoint)
	f.edit(func(m *state.Manifest) { m.Add(state.Key("alice", "", "later"), state.Record{Path: "alice/later.html"}) })
	_, out = e.archRows(arch, "bob")
	if n, _ := out["listNote"].(string); !strings.Contains(n, "changed since you loaded") || !strings.Contains(n, "Load folders again") {
		t.Errorf("a list after the archive's manifest changed: %q, want it said to load the folders again", n)
	}
}

// covers: MA-531, MA-435, MA-512, R29, S52
// What the archive's notes say of its index and of its records is true of the archive they are about
// (E4-AI8): "Rebuild search index (Maintenance)" acts on the configured archive, so a note about another
// archive folder never sends the owner to it, and a search.db a newer MailArchive wrote is not "an index that
// can't be used" to be rebuilt (a rebuild by this older build would replace it with an index of the older
// format): it says it was written by a newer MailArchive and to update. The preview of a whole folder still
// works from the manifest, with names that carry no subject or date, as it did. The positive twin: the
// configured archive with no search.db is still told to rebuild it.
func TestArchiveIndexNotesNeverSendTheOwnerToRebuildTheWrongArchive(t *testing.T) {
	e := newWsEnv(t)
	f := buildArchiveAt(t, e.out)
	other := filepath.Join(e.dir, "inherited")
	g := buildArchiveAt(t, other)
	warnings := func(src map[string]any, folder string) string {
		e.archFolders(src)
		_, res := e.preview(e.archSel(src, entry(folder, false)))
		return strings.Join(toStrings(res["warnings"]), " | ")
	}
	cfgSrc, otherSrc := archSource("archive", e.out), archSource("archive-other", other)

	// the positive twin: no index at all, the configured archive
	os.Rename(f.path("search.db"), f.path("search.db.away"))
	if got := warnings(cfgSrc, "alice"); !strings.Contains(got, "Rebuild search index (Maintenance)") {
		t.Errorf("the configured archive with no search.db: %q, want the advice to rebuild it", got)
	}
	_, out := e.archRows(cfgSrc, "alice")
	if n, _ := out["listUnavailable"].(string); !strings.Contains(n, "Rebuild search index (Maintenance)") {
		t.Errorf("the configured archive's list with no search.db: %q", n)
	}
	os.Rename(f.path("search.db.away"), f.path("search.db"))

	// a newer index, the configured archive and another folder
	versionOf := func(p string) (v int) {
		t.Helper()
		db, err := sqlOpen(p)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.QueryRow(`SELECT version FROM meta`).Scan(&v); err != nil {
			t.Fatal(err)
		}
		return v
	}
	fv, gv := versionOf(f.path("search.db")), versionOf(g.path("search.db"))
	setIndexVersion(t, f.path("search.db"), 99)
	setIndexVersion(t, g.path("search.db"), 99)
	for name, src := range map[string]map[string]any{"configured": cfgSrc, "another folder": otherSrc} {
		got := warnings(src, "alice")
		if !strings.Contains(got, "newer MailArchive") || !strings.Contains(got, "format 99") || !strings.Contains(got, "Update MailArchive Desktop") || strings.Contains(got, "Rebuild search index") {
			t.Errorf("%s, a search.db a newer MailArchive wrote: %q, want it said so and no rebuild", name, got)
		}
	}
	setIndexVersion(t, f.path("search.db"), fv)
	setIndexVersion(t, g.path("search.db"), gv)

	// another folder: nothing here is Maintenance's to rebuild
	os.Remove(g.path("search.db"))
	if got := warnings(otherSrc, "alice"); strings.Contains(got, "Rebuild") || !strings.Contains(got, "has no search index") {
		t.Errorf("another folder with no search.db: %q, want its index named and no rebuild", got)
	}
	_, out = e.archRows(otherSrc, "alice")
	if n, _ := out["listUnavailable"].(string); n == "" || strings.Contains(n, "Rebuild") || !strings.Contains(n, "Whole-folder selection works") {
		t.Errorf("another folder's list with no search.db: %q", n)
	}
	rec := g.rec("hello")
	os.Remove(g.path(rec.Path)) // a record whose files were removed since: skipped, and counted
	os.Remove(g.path(strings.TrimSuffix(rec.Path, ".html") + ".eml"))
	if got := warnings(otherSrc, "alice"); !strings.Contains(got, "records whose files were removed are skipped") || strings.Contains(got, "Rebuild") {
		t.Errorf("another folder with a record whose files were removed: %q", got)
	}
}

// covers: MA-510, MA-530, R29, R12, S41, S52
// A Microsoft 365 sign-in that completes or is cleared (XC7) does not touch the export or the preview of an
// archive (E4-AI5): an archive source needs no sign-in (MA-510), and the account in "Sign in as <nobody>"
// was empty because the run never had one. signInChanged compared the running slot's mailbox ("" for an
// archive) with the new account and stopped it with R-z (0 of N exported, "Sign in as  (click Sign in...)");
// and dropSession cancelled the preview job whatever its source. The positive twin: the same change stops a
// Microsoft 365 export's preview job, as before.
func TestASignInChangeLeavesAnArchiveExportAndPreviewAlone(t *testing.T) {
	e := newWsEnv(t)
	buildArchiveAt(t, e.out)
	addBulkStore(t, e.out, 150)
	arch := archSource("archive", e.out)
	e.archFolders(arch)

	// the preview job: archive-bound jobs are not cancelled, a Microsoft 365 one is
	for _, c := range []struct {
		name      string
		bind      wsBind
		cancelled bool
	}{{"an archive preview", wsBind{arch: true, path: e.out}, false}, {"a Microsoft 365 preview", wsBind{upn: "alice@contoso.org"}, true}} {
		release := make(chan struct{})
		sawCancel := make(chan bool, 1)
		e.d.ws.startJob(func(ctx context.Context, job *wsPreviewJob) (*app.CopyPlan, map[string]any, *refusal) {
			select {
			case <-ctx.Done():
				sawCancel <- true
			case <-release:
				sawCancel <- false
			}
			return nil, nil, nil
		}, c.bind, "")
		e.d.signInChanged("carol@contoso.org")
		time.Sleep(50 * time.Millisecond)
		close(release)
		select {
		case got := <-sawCancel:
			if got != c.cancelled {
				t.Errorf("%s: cancelled by a sign-in change = %v, want %v", c.name, got, c.cancelled)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: the job never ended", c.name)
		}
	}

	// the export: a sign-in as another account while an archive export runs
	id, _ := e.preview(e.archSel(arch, entry("bulk", false)))
	e.d.ex.afterLaunch = func(s *exportSlot) { e.d.signInChanged("carol@contoso.org") }
	e.ok("/api/exports/start", map[string]any{"previewId": id})
	last := e.waitRun()["last"].(map[string]any)
	if last["status"] != "complete" || last["exported"] != float64(150) {
		t.Errorf("an archive export that a Microsoft 365 sign-in change met: %v, want it complete (no R-z: it never had a sign-in)", last)
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
