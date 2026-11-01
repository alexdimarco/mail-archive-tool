package archdelete

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"mail-archive-tool/internal/state"
)

const (
	stemA = "Mailbox/Inbox/2024-03-01_0900_hello_ab12cd34"
	stemB = "Mailbox/Sent/Deep/2024-03-02_1000_reply_cd34ef56"
)

// plainBesidePersonsFiles is a plain archive sharing its folder with the person's
// own files: in the root, in a folder of their own, and in a store folder of the
// same name as theirs.
func plainBesidePersonsFiles(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/mail/Mailbox.pst": "Mailbox"}, []rec{
		{Path: stemA + ".html", Date: "2024-03-01T09:00:00Z"},
		{Path: stemB + ".html", Also: []string{"Mailbox/Inbox/2024-03-02_1000_reply_00ff00ff.html"}, Date: "2024-03-02T10:00:00Z"},
	})
	for _, f := range []string{
		stemA + ".html", stemA + "-attachments.zip", stemA + ".eml",
		stemB + ".html",
		"Mailbox/Inbox/2024-03-02_1000_reply_00ff00ff.html", "Mailbox/Inbox/2024-03-02_1000_reply_00ff00ff.eml",
		"Mailbox/Inbox/index.html", "Mailbox/Inbox/index-2.html", "Mailbox/index.html", "Mailbox/Sent/Deep/index.html",
		"index.html", "search.db", "search.db-wal", "README.txt", "attachments-report.tsv", "mailarchive.log",
		".mailarchive-history.jsonl", ".mailarchive-lastrun.json", ".mailarchive.lock",
		// The person's own files: kept.
		"taxes.pdf", "Photos/beach.jpg", "Mailbox/Inbox/notes.txt",
	} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), "x "+f)
	}
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", ".DS_Store"), "litter")
	writeFile(t, filepath.Join(dir, "Mailbox", "Sent", "Deep", "desktop.ini"), "litter")
	return dir
}

// covers: MA-957, R36
func TestPlainPlanListsTheArchivesFilesAndKeepsThePersons(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	before := tree(t, dir)
	p := mustPlan(t, dir, testDeps())
	if p.Kind != Plain {
		t.Fatalf("kind = %v, want plain", p.Kind)
	}
	if d := setDiff("removed", removeRels(p), []string{
		stemA + ".html", stemA + "-attachments.zip", stemA + ".eml", stemB + ".html",
		"Mailbox/Inbox/2024-03-02_1000_reply_00ff00ff.html", "Mailbox/Inbox/2024-03-02_1000_reply_00ff00ff.eml",
		"Mailbox/Inbox/index.html", "Mailbox/Inbox/index-2.html", "Mailbox/index.html", "Mailbox/Sent/Deep/index.html",
		"index.html", "search.db", "search.db-wal", "README.txt", "attachments-report.tsv", "mailarchive.log",
		".mailarchive-history.jsonl", ".mailarchive-lastrun.json", manifestName,
	}); d != "" {
		t.Fatal(d)
	}
	if last := p.Remove[len(p.Remove)-1]; last.Rel != manifestName || last.Kind != Anchor {
		t.Fatalf("the manifest must be removed last, got %+v", last)
	}
	if d := setDiff("kept", p.Kept, []string{"taxes.pdf", "Photos", "Mailbox/Inbox/notes.txt"}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("litter", p.Litter, []string{"Mailbox/Inbox/.DS_Store", "Mailbox/Sent/Deep/desktop.ini"}); d != "" {
		t.Fatal(d)
	}
	if contains(removeRels(p), ".mailarchive.lock") || contains(p.Kept, ".mailarchive.lock") {
		t.Fatal("the lock file is step 7's, never the plan's")
	}
	if p.Anchor.SHA256 == "" || p.Anchor.Size == 0 || p.Identity == (Identity{}) {
		t.Fatalf("the plan must record the manifest digest and the folder identity: %+v %+v", p.Anchor, p.Identity)
	}
	if len(p.Folders) == 0 || p.Folders[0] != "Mailbox/Sent/Deep" {
		t.Fatalf("folders deepest first, got %q", p.Folders)
	}
	if !sameTree(before, tree(t, dir)) {
		t.Fatal("planning changed the folder")
	}
}

// covers: MA-957, R36
func TestPlansOfManifestsWithNoStoresMapListTheirMail(t *testing.T) {
	// Versions 1 and 2 have no Stores map; 3 to 6 have one.
	for v := 1; v <= 6; v++ {
		dir := t.TempDir()
		var stores map[string]string
		if v >= 3 {
			stores = map[string]string{"path:/mail/Mailbox.pst": "Mailbox"}
		}
		writeManifest(t, dir, v, stores, []rec{{Path: stemA + ".html"}})
		writeFile(t, filepath.Join(dir, filepath.FromSlash(stemA+".html")), "page")
		writeFile(t, filepath.Join(dir, filepath.FromSlash(stemA+"-attachments.zip")), "zip")
		p := mustPlan(t, dir, testDeps())
		if d := setDiff("removed", removeRels(p), []string{stemA + ".html", stemA + "-attachments.zip", manifestName}); d != "" {
			t.Fatal(d)
		}
		if d := setDiff("stores", p.Stores, []string{"Mailbox"}); d != "" {
			t.Fatal(d)
		}
	}
}

// covers: MA-957, R36
func TestAVersion6ManifestMissingAStoreStillHasThatStoresMailListed(t *testing.T) {
	dir := t.TempDir()
	// A version-2 archive of stores A and B, upgraded by a run of A only: Stores={A}.
	writeManifest(t, dir, 6, map[string]string{"path:/mail/A.pst": "A"}, []rec{
		{Path: "A/Inbox/2024-01-01_0000_a_11111111.html"},
		{Path: "B/Inbox/2024-01-01_0000_b_22222222.html"},
	})
	writeFile(t, filepath.Join(dir, "A", "Inbox", "2024-01-01_0000_a_11111111.html"), "a")
	writeFile(t, filepath.Join(dir, "B", "Inbox", "2024-01-01_0000_b_22222222.html"), "b")
	writeFile(t, filepath.Join(dir, "B", "Inbox", ".mailarchive-77.tmp"), "crashed run")
	p := mustPlan(t, dir, testDeps())
	if d := setDiff("removed", removeRels(p), []string{"A/Inbox/2024-01-01_0000_a_11111111.html", "B/Inbox/2024-01-01_0000_b_22222222.html", "B/Inbox/.mailarchive-77.tmp", manifestName}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("stores", p.Stores, []string{"A", "B"}); d != "" {
		t.Fatal(d)
	}
}

// covers: MA-957, R36
func TestACrashedRunsLeftoversAreListed(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	for _, f := range []string{stemA + ".html", "Mailbox/Inbox/.mailarchive-123.tmp", "Mailbox/Inbox/Sub/.mailarchive-snapshot-9.pst",
		".mailarchive-manifest-4.tmp", ".mailarchive-snapshot-1.mbox", "search.db.rebuild", "search.db.rebuild-wal", "_COPY-NOT-FINISHED.txt",
		"_outlook-pst/Personal.pst", "_outlook-pst/notes.txt"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	// An orphan zip a crash left: no record names it, so it is listed, not removed.
	writeFile(t, filepath.Join(dir, "Mailbox", "Inbox", "2024-01-01_0000_gone_deadbeef-attachments.zip"), "zip")
	p := mustPlan(t, dir, testDeps())
	if d := setDiff("removed", removeRels(p), []string{stemA + ".html", "Mailbox/Inbox/.mailarchive-123.tmp", "Mailbox/Inbox/Sub/.mailarchive-snapshot-9.pst",
		".mailarchive-manifest-4.tmp", ".mailarchive-snapshot-1.mbox", "search.db.rebuild", "search.db.rebuild-wal", "_COPY-NOT-FINISHED.txt",
		"_outlook-pst/Personal.pst", manifestName}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("unrecorded", unrecordedRels(p), []string{"Mailbox/Inbox/2024-01-01_0000_gone_deadbeef-attachments.zip"}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("kept", p.Kept, []string{"_outlook-pst/notes.txt"}); d != "" {
		t.Fatal(d)
	}
}

// covers: MA-957, R36
func TestAPlantedRecordNamingAPersonsFileListsOnlyStemSiblings(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{
		{Path: stemA + ".html", Also: []string{"Docs/report.pdf", "Docs/../taxes.html", "/etc/passwd", `Docs\report.pdf`, ".hidden/x.html"}},
		{Path: "Docs/report.pdf"},
		{Path: "Docs/budget.html"},
	})
	for _, f := range []string{stemA + ".html", "Docs/report.pdf", "Docs/notes.txt", "Docs/budget.html", "taxes.html", ".hidden/x.html"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	before := tree(t, dir)
	p := mustPlan(t, dir, testDeps())
	// The recorded Path is as untrusted as a token: Docs becomes a store token only
	// through a valid .html record, and then only budget's stem siblings go.
	if d := setDiff("removed", removeRels(p), []string{stemA + ".html", "Docs/budget.html", manifestName}); d != "" {
		t.Fatal(d)
	}
	for _, kept := range []string{"Docs/report.pdf", "Docs/notes.txt", "taxes.html", ".hidden"} {
		if !contains(p.Kept, kept) {
			t.Fatalf("%s must be kept, kept = %q", kept, p.Kept)
		}
	}
	if !sameTree(before, tree(t, dir)) {
		t.Fatal("planning changed the folder")
	}
}

// covers: MA-957, R36
func TestAPlantedStoresTokenNamingAPersonsFolderListsOnlyStems(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox", "path:/planted": "Documents"}, []rec{
		{Path: stemA + ".html"},
		{Path: "Documents/2024-05-05_0505_x_abcdef01.html"},
	})
	for _, f := range []string{stemA + ".html", "Documents/2024-05-05_0505_x_abcdef01.html", "Documents/2024-05-05_0505_x_abcdef01.eml",
		"Documents/cv.docx", "Documents/Taxes/2023.pdf", "Documents/old-letter.html"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	p := mustPlan(t, dir, testDeps())
	if d := setDiff("removed", removeRels(p), []string{stemA + ".html", "Documents/2024-05-05_0505_x_abcdef01.html", "Documents/2024-05-05_0505_x_abcdef01.eml", manifestName}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("kept", p.Kept, []string{"Documents/cv.docx", "Documents/Taxes/2023.pdf"}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("unrecorded", unrecordedRels(p), []string{"Documents/old-letter.html"}); d != "" {
		t.Fatal(d)
	}
}

// covers: MA-957, R36
func TestUnrecordedMailCopiesAreListedNotRemoved(t *testing.T) {
	dir := t.TempDir()
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}})
	for _, f := range []string{stemA + ".html", "Mailbox/x-attachments.zip", "Mailbox/Old/2019-01-01_0000_old_99999999.html",
		"Mailbox/Old/2019-01-01_0000_old_99999999.eml", "Mailbox/Old/._2019-01-01_0000_old_99999999.html", "Mailbox/Old/index.html", "y-attachments.zip"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	p := mustPlan(t, dir, testDeps())
	// An index page in a folder no record passes through is not found by walking a
	// tree: it is an unrecorded copy like any other page there.
	if d := setDiff("removed", removeRels(p), []string{stemA + ".html", manifestName}); d != "" {
		t.Fatal(d)
	}
	if d := setDiff("unrecorded", unrecordedRels(p), []string{"Mailbox/x-attachments.zip", "Mailbox/Old/2019-01-01_0000_old_99999999.html", "Mailbox/Old/2019-01-01_0000_old_99999999.eml", "Mailbox/Old/index.html"}); d != "" {
		t.Fatal(d)
	}
	// AppleDouble litter in a folder only an unrecorded copy passes through goes only
	// when the box is ticked; a zip outside the store trees is the person's.
	if d := setDiff("unrecorded litter", p.UnrecordedLitter, []string{"Mailbox/Old/._2019-01-01_0000_old_99999999.html"}); d != "" {
		t.Fatal(d)
	}
	if !contains(p.Kept, "y-attachments.zip") {
		t.Fatalf("a zip outside the store trees is kept, kept = %q", p.Kept)
	}
}

// covers: MA-957, R36
func TestATokenNamingARootFileHidesNothingAndLitterFoldersAreKept(t *testing.T) {
	dir := t.TempDir()
	// A planted record makes "search.db" a store token while search.db is a file.
	writeManifest(t, dir, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: stemA + ".html"}, {Path: "search.db/2024-01-01_0000_x_12345678.html"}})
	for _, f := range []string{stemA + ".html", "search.db", "Mailbox/.Trashes/501/2019-01-01_0000_old_99999999.html", "Mailbox/Inbox/.Spotlight-V100/store.db"} {
		writeFile(t, filepath.Join(dir, filepath.FromSlash(f)), f)
	}
	p := mustPlan(t, dir, testDeps())
	if d := setDiff("removed", removeRels(p), []string{stemA + ".html", "search.db", manifestName}); d != "" {
		t.Fatal(d)
	}
	// A litter folder is never walked or cleared: what is in it is not listed.
	if d := setDiff("kept", p.Kept, []string{"Mailbox/.Trashes", "Mailbox/Inbox/.Spotlight-V100"}); d != "" {
		t.Fatal(d)
	}
	if len(p.Unrecorded) != 0 {
		t.Fatalf("nothing in a litter folder is an unrecorded copy: %q", unrecordedRels(p))
	}
}

// covers: MA-957, R36
func TestThePlanCapturesTheRecordsValuesWhileTheArchiveIsThere(t *testing.T) {
	dir := plainBesidePersonsFiles(t)
	writeFile(t, filepath.Join(dir, state.HistoryName),
		`{"run":1,"at":"2023-06-01T08:00:00Z","mailboxes":["Mailbox"]}`+"\n"+`{"k":"x","folder":"Inbox"}`+"\n"+`{"run":2,"at":"2024-01-01T08:00:00Z"}`+"\n")
	h, _ := json.Marshal([]state.RunHistoryEntry{
		{When: time.Date(2024, 3, 3, 9, 0, 0, 0, time.UTC), Status: state.RunOK},
		{When: time.Date(2024, 3, 4, 9, 0, 0, 0, time.UTC), Status: "failed"},
	})
	writeFile(t, filepath.Join(dir, state.RunHistoryName), string(h))
	p := mustPlan(t, dir, testDeps())
	c := p.Captured
	if !c.Known || c.Messages != 2 {
		t.Fatalf("captured = %+v", c)
	}
	if !c.FirstRun.Equal(time.Date(2023, 6, 1, 8, 0, 0, 0, time.UTC)) || !c.LastBackup.Equal(time.Date(2024, 3, 3, 9, 0, 0, 0, time.UTC)) {
		t.Fatalf("first run / last ok backup = %v / %v", c.FirstRun, c.LastBackup)
	}
	if !c.MailFrom.Equal(time.Date(2024, 3, 1, 9, 0, 0, 0, time.UTC)) || !c.MailTo.Equal(time.Date(2024, 3, 2, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("mail dates = %v to %v", c.MailFrom, c.MailTo)
	}
}
