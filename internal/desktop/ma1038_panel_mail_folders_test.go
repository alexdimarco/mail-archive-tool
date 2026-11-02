package desktop

import (
	"testing"

	"mail-archive-tool/internal/archdelete"
)

// covers: MA-1038, R36, S41
// The delete panel's "Mail folders:" names each mail folder with the mail folders the archive
// recorded inside it, as the terminal does (the 1.5.3 re-walk's N16: a Windows walk saw "Mail
// folders: mbox", the source's folder, not its Inbox): the plan's reply gives each store a label,
// which the panel shows. Positive twin first: a store that is its only mail folder is named once.
func TestThePanelNamesTheMailFoldersInsideEachStore(t *testing.T) {
	label := func(p *archdelete.DeletePlan, i int) string {
		t.Helper()
		stores, _ := planView(p)["stores"].([]map[string]any)
		if len(stores) <= i {
			t.Fatalf("stores %v", stores)
		}
		s, _ := stores[i]["label"].(string)
		return s
	}
	twin := &archdelete.DeletePlan{Folder: "/a/T", Name: "T", Kind: archdelete.Plain, Stores: []string{"Inbox"}, StoreFolders: [][]string{{"Inbox"}}}
	if got := label(twin, 0); got != "Inbox" {
		t.Fatalf("the twin: %q", got)
	}
	p := &archdelete.DeletePlan{Folder: "/a/P", Name: "P", Kind: archdelete.Plain, Stores: []string{"Archive", "mbox"}, StoreFolders: [][]string{nil, {"Inbox", "Sent"}}}
	if got0, got1 := label(p, 0), label(p, 1); got0 != "Archive" || got1 != "mbox (Inbox, Sent)" {
		t.Fatalf("labels %q, %q", got0, got1)
	}
	// The panel shows the labels (the page script under Node).
	if passed := runPageScenario(t, "mailFolderLabels"); len(passed) != 1 {
		t.Errorf("the mailFolderLabels scenario passed %d of its 1 check: %v", len(passed), passed)
	}
}
