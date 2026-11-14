package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/model"
)

// covers: MA-1222, R45
// The search reader (serve, and the Desktop's reader of a plaintext archive) finds a Gmail email
// under every folder it is in (docs/design-gmail-dedup.md §8.3): the folder list counts it in its
// home folder and in each other folder, the Sent Mail filter finds it although it lives in All
// Mail, it is one hit, and the hit names its other folders, which the page shows as "In: All Mail,
// INBOX, Sent Mail", each escaped. Positive twin first: an email in one folder shows its folder
// alone, with no "In:".
func TestTheReaderFindsAnEmailUnderEveryFolderItIsIn(t *testing.T) {
	dir := tmpDir(t)
	ix, err := index.Open(filepath.Join(dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer ix.Close()
	d := time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)
	ix.Add("s", []string{"All Mail"}, &model.Message{Subject: "Receipt", Received: d, PlainBody: "paid"}, "s/All Mail/r.html", "k1")
	ix.Add("s", []string{"INBOX"}, &model.Message{Subject: "Lunch", Received: d.Add(time.Hour), PlainBody: "noon"}, "s/INBOX/l.html", "k2")
	ix.SetMembers("k1", "s", []string{"Sent Mail", "INBOX"})
	ix.Flush()

	ts := httptest.NewServer(New(dir, ix))
	defer ts.Close()
	get := func(u string, v any) {
		t.Helper()
		resp, err := http.Get(ts.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	var facets struct {
		Folders []index.FolderCount `json:"folders"`
		Total   int                 `json:"total"`
	}
	get("/api/facets", &facets)
	if want := []index.FolderCount{{Folder: "All Mail", Count: 1}, {Folder: "INBOX", Count: 2}, {Folder: "Sent Mail", Count: 1}}; !reflect.DeepEqual(facets.Folders, want) || facets.Total != 2 {
		t.Fatalf("facets %+v total %d, want %+v total 2", facets.Folders, facets.Total, want)
	}
	var res struct {
		Total   int            `json:"total"`
		Results []index.Result `json:"results"`
	}
	get("/api/search?folder=INBOX&sort=date", &res)
	if res.Total != 2 || res.Results[0].Subject != "Lunch" || len(res.Results[0].Also) != 0 {
		t.Fatalf("INBOX: %+v", res)
	}
	get("/api/search?folder=Sent+Mail", &res)
	if res.Total != 1 || res.Results[0].Subject != "Receipt" || !reflect.DeepEqual(res.Results[0].Also, []string{"INBOX", "Sent Mail"}) {
		t.Fatalf("Sent Mail: %+v", res)
	}
	// Amended 2026-11-13: each folder is named by shownFolder first (F11, MA-1299).
	if !strings.Contains(appJS, `'In: '+[m.folder].concat(m.also).map(shownFolder).map(esc).join(', ')`) || !strings.Contains(appJS, `'<span class="pill">'+inFolders(m)+'</span>'`) {
		t.Fatal("the reader's page does not show a hit's folders with In:, escaped")
	}
}
