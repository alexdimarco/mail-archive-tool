package tbrestore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1138, R41, S70
// The skip set's refresh (foldcheck M3) and committed flag (foldcheck2 P3): an ID the set
// knows answers at once; an unknown ID with the log as it was when the set was built
// (size and modification time) is answered from one stat, not a re-read — proven by a
// log rewritten to name it with its size and time kept, which the set does not see —
// and once the log changed, one re-read finds it. "Committed" is a "checked" line and no
// "taken" line; a marker naming another profile's key is not this record's restore.
func TestRestoreSkipSetRefreshAndCommitted(t *testing.T) {
	p := filepath.Join(t.TempDir(), LogName)
	l := NewLog(p)
	_ = l.Append(LogLine{Event: EvStarted, ID: "aaaaaaaa", Profile: "k1"})
	_ = l.Append(LogLine{Event: EvChecked, ID: "aaaaaaaa"})
	_ = l.Append(LogLine{Event: EvStarted, ID: "bbbbbbbb", Profile: "k1"})
	_ = l.Append(LogLine{Event: EvChecked, ID: "bbbbbbbb"})
	_ = l.Append(LogLine{Event: EvTaken, ID: "bbbbbbbb"})
	s := LoadSkipSet(p)
	if k, c := s.Lookup("aaaaaaaa", "k1"); !k || !c {
		t.Fatal("checked, no taken: committed")
	}
	if k, c := s.Lookup("bbbbbbbb", "k1"); !k || c {
		t.Fatal("a taken line: not committed")
	}
	if k, _ := s.Lookup("aaaaaaaa", "k2"); k {
		t.Fatal("another profile's key: not this record's restore")
	}

	fi, _ := os.Stat(p)
	data, _ := os.ReadFile(p)
	same := []byte(strings.ReplaceAll(string(data), "bbbbbbbb", "cccccccc")) // the same size
	if len(same) != len(data) {
		t.Fatal("the rewrite keeps the size")
	}
	_ = os.WriteFile(p, same, 0o600)
	if lines, _ := NewLog(p).Read(); len(Open(Restores(lines), "k1")) != 2 {
		t.Fatal("the rewritten log does name cccccccc: a re-read would find it")
	}
	_ = os.Chtimes(p, fi.ModTime(), fi.ModTime())
	if k, _ := s.Lookup("cccccccc", "k1"); k {
		t.Fatal("the log as it was (size and time): no re-read")
	}
	_ = os.Chtimes(p, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second))
	_ = l.Append(LogLine{Event: EvStarted, ID: "dddddddd", Profile: "k1"})
	if k, c := s.Lookup("dddddddd", "k1"); !k || c {
		t.Fatal("the log changed: one re-read finds a restore that began since")
	}
	if s.LogUnreadable() {
		t.Fatal("readable")
	}
}
