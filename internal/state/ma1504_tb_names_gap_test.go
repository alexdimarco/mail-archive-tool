package state

import "testing"

// covers: MA-1504, MA-1481
// The read side of a one-folder archive's name kept across a gap (docs/design-per-mailbox-archives.md
// revision 2.2): a record with completeness entries of its own reads a carried name (thunderbird_names)
// only for a store whose own entry names no account. Positive twin first: an entry's own name wins
// over a carried one, and a carried name for a store with no entry is still not read (RV3-01's rule,
// pinned in ma1481_tb_names_record_test.go). A record with no entries reads its carried names as
// before.
func TestACarriedNameIsReadOnlyForAStoreWhoseEntryNamesNone(t *testing.T) {
	carried := []TBName{{StoreID: "aa", Name: "qza@qzg.example"}, {StoreID: "bb", Name: "qzb@qzg.example"}}
	both := LastRun{Thunderbird: []TBAccount{{StoreID: "bb", Name: "qzown@qzg.example"}}, ThunderbirdNames: carried}
	if TBNameOf(both, "bb") != "qzown@qzg.example" || TBNameOf(both, "aa") != "" {
		t.Fatalf("positive twin: an entry's name wins, and a store with no entry reads no carried name: %q %q", TBNameOf(both, "bb"), TBNameOf(both, "aa"))
	}
	gap := LastRun{Thunderbird: []TBAccount{{StoreID: "bb", Reason: "no_match"}}, ThunderbirdNames: carried}
	if TBNameOf(gap, "bb") != "qzb@qzg.example" || TBNameOf(gap, "aa") != "" {
		t.Errorf("a store whose entry names no account reads its carried name: %q %q", TBNameOf(gap, "bb"), TBNameOf(gap, "aa"))
	}
	if got := TBNamesOf(gap); len(got) != 1 || got[0] != carried[1] {
		t.Errorf("the names handed on: %+v", got)
	}
	if TBNameOf(LastRun{ThunderbirdNames: carried}, "aa") != "qza@qzg.example" {
		t.Error("a record with no entries reads its carried names, as before")
	}
	if TBNamesOf(LastRun{Encrypted: true, Thunderbird: gap.Thunderbird, ThunderbirdNames: carried}) != nil {
		t.Error("an encrypted record keeps no names")
	}
}
