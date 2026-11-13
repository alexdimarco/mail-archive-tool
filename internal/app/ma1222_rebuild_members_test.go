package app

import (
	"context"
	"database/sql"
	"io"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// indexVersionAt is the version search.db's meta table holds.
func indexVersionAt(t *testing.T, out string) int {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(out, "search.db"))+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var v int
	if err := db.QueryRow(`SELECT version FROM meta`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

// covers: MA-1222, MA-1243, R45, R6
// `reindex -rebuild` takes each Gmail email's other folders from the manifest (§8.2, Q-L4):
// positive twin first, a real backup of an archive nobody switched on is rebuilt and its index
// stays version 3 with no member rows, so v1.6.1 still opens it (F3a). Then the archive switched
// on with a membership (the fixture's "Sent"): the rebuilt index holds exactly the manifest's
// memberships, is version 4, finds the email under Sent one time, and the rebuilt folder pages
// give Sent a page linking the email in its home folder.
func TestTheRebuildTakesEachEmailsFoldersFromTheManifest(t *testing.T) {
	logger := log.New(io.Discard, "", 0)
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	plain := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: plain, Mode: export.Incremental, KeepRaw: true, Index: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Rebuild(plain, logger); err != nil {
		t.Fatal(err)
	}
	if v := indexVersionAt(t, plain); v != 3 {
		t.Fatalf("a rebuilt index of an archive nobody switched on is version %d, want 3", v)
	}

	out, _ := dedupArchive(t, false)
	m, err := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]string{}
	for k, r := range m.All() {
		for _, f := range r.Folders {
			want[k] = append(want[k], f.Folder)
		}
	}
	if len(want) != 1 {
		t.Fatalf("the fixture has %d records with memberships, want 1", len(want))
	}
	if _, err := Rebuild(out, logger); err != nil {
		t.Fatal(err)
	}
	if v := indexVersionAt(t, out); v != 4 {
		t.Fatalf("the rebuilt index of a switched-on archive is version %d, want 4", v)
	}
	ix, err := index.Open(filepath.Join(out, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	for k := range m.All() {
		if f, _ := ix.Members(k); len(f) > 0 {
			got[k] = f
		}
	}
	for k := range want {
		sort.Strings(want[k])
	}
	if !reflect.DeepEqual(got, want) {
		ix.Close()
		t.Fatalf("the rebuilt index's members %q, want the manifest's %q", got, want)
	}
	res, total, err := ix.Search(index.Query{Folder: "Sent"})
	ix.Close()
	if err != nil || total != 1 || len(res) != 1 {
		t.Fatalf("Sent finds %d (%v), want the one email", total, err)
	}
	tok := strings.SplitN(res[0].Path, "/", 2)[0]
	page, err := os.ReadFile(filepath.Join(out, tok, "Sent", "index.html"))
	if err != nil {
		t.Fatalf("the rebuild wrote no page for Sent: %v", err)
	}
	home := strings.SplitN(res[0].Path, "/", 2)[1]
	if !strings.Contains(string(page), `href="../`+strings.ReplaceAll(home, " ", "%20")+`"`) {
		t.Fatalf("Sent's page does not link %s in its home folder", home)
	}
}
