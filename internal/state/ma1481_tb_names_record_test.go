package state

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-1481, R18
// The record's carried names (LastRun.ThunderbirdNames; the 1.7.5 re-verify, round 3's RV3-01) are
// read back as a planted record may hold them: at most TBAccountsMax, each name cleaned as the
// completeness record's are (control and bidi characters gone), a name two entries share cleared
// (two folders are never said as one), and an entry with no id or no name left out. TBNameOf finds a
// name by store id in the completeness entries first, else in the carried names. And a record with a
// field this version does not know reads as present, as 1.7.4 (json.Unmarshal, no strict reader of
// the record) reads one with thunderbird_names. Positive twin first: a clean list reads back whole.
func TestTheCarriedNamesReadBackCleanAndBounded(t *testing.T) {
	out := t.TempDir()
	clean := []TBName{{StoreID: "aa", Name: "qza@qzg.example"}, {StoreID: "bb", Name: "qzb@qzg.example"}}
	if err := WriteLastRun(out, LastRun{Status: RunFailed, ThunderbirdNames: clean}); err != nil {
		t.Fatal(err)
	}
	lr, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent || len(lr.ThunderbirdNames) != 2 || lr.ThunderbirdNames[1] != clean[1] {
		t.Fatalf("positive twin: a clean list reads back whole: %+v %v", lr.ThunderbirdNames, err)
	}
	if TBNameOf(lr, "bb") != "qzb@qzg.example" || TBNameOf(lr, "cc") != "" || TBNameOf(lr, "") != "" {
		t.Errorf("TBNameOf by store id: %q", TBNameOf(lr, "bb"))
	}
	// The completeness entries come first.
	both := LastRun{Thunderbird: []TBAccount{{StoreID: "bb", Name: "qzown@qzg.example"}}, ThunderbirdNames: clean}
	if TBNameOf(both, "bb") != "qzown@qzg.example" || TBNameOf(both, "aa") != "" {
		t.Errorf("a record with entries of its own names by them: %q %q", TBNameOf(both, "bb"), TBNameOf(both, "aa"))
	}
	if TBNamesOf(LastRun{Encrypted: true, ThunderbirdNames: clean}) != nil {
		t.Error("an encrypted record hands on names")
	}

	var planted []TBName
	for i := range TBAccountsMax + 10 {
		planted = append(planted, TBName{StoreID: fmt.Sprintf("id%03d", i), Name: fmt.Sprintf("qz%03d@qz.example", i)})
	}
	planted[0].Name = "qz\x1b[31mred‮@qz.example"
	planted[1].Name, planted[2].Name = "qzsame@qz.example", "QZSAME@qz.example"
	planted[3].StoreID = ""
	data, _ := json.Marshal(map[string]any{"version": 1, "status": "failed", "thunderbird_names": planted, "qz_a_later_field": map[string]int{"x": 1}})
	if err := os.WriteFile(filepath.Join(out, LastRunName), data, 0o600); err != nil {
		t.Fatal(err)
	}
	lr, st, err = ReadLastRun(out)
	if err != nil || st != LastRunPresent {
		t.Fatalf("a record with a field this version does not know must read as present: %v %v", st, err)
	}
	if n := len(lr.ThunderbirdNames); n > TBAccountsMax || n != TBAccountsMax-3 {
		t.Errorf("at most %d entries, the shared and id-less ones left out: %d", TBAccountsMax, n)
	}
	for _, e := range lr.ThunderbirdNames {
		if strings.ContainsAny(e.Name, "\x1b‮") || e.StoreID == "" || strings.EqualFold(e.Name, "qzsame@qz.example") {
			t.Errorf("read back unclean or shared: %+v", e)
		}
	}
	if TBNameOf(lr, "id000") != "qz[31mred@qz.example" {
		t.Errorf("a planted name is cleaned: %q", TBNameOf(lr, "id000"))
	}
}
