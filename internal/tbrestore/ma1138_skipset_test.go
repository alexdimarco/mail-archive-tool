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
// "taken" line (and, ADV-07, no "abandoned" closing line); "open" is no closing line; a
// marker naming another profile's key is not this record's restore, nor (ADV-06) one met
// in a folder that is not, by identity, the Local Folders its started line records.
func TestRestoreSkipSetRefreshAndCommitted(t *testing.T) {
	p := filepath.Join(t.TempDir(), LogName)
	local, _ := filepath.EvalSymlinks(t.TempDir())
	lid, err := DirIdentity(local)
	if err != nil {
		t.Fatal(err)
	}
	holder, _ := os.Stat(local)
	elsewhere, _ := os.Stat(t.TempDir())
	started := func(id string) LogLine {
		return LogLine{Event: EvStarted, ID: id, Profile: "k1", Local: local, LocalID: &lid}
	}
	l := NewLog(p)
	_ = l.Append(started("aaaaaaaa"))
	_ = l.Append(LogLine{Event: EvChecked, ID: "aaaaaaaa"})
	_ = l.Append(started("bbbbbbbb"))
	_ = l.Append(LogLine{Event: EvChecked, ID: "bbbbbbbb"})
	_ = l.Append(LogLine{Event: EvTaken, ID: "bbbbbbbb"})
	_ = l.Append(started("eeeeeeee"))
	_ = l.Append(LogLine{Event: EvChecked, ID: "eeeeeeee"})
	_ = l.Append(LogLine{Event: EvAbandoned, ID: "eeeeeeee"})
	s := LoadSkipSet(p)
	if k, o, c := s.Lookup("aaaaaaaa", "k1", holder); !k || !o || !c {
		t.Fatal("checked, no taken, no closing line: open and committed")
	}
	if k, _, c := s.Lookup("bbbbbbbb", "k1", holder); !k || c {
		t.Fatal("a taken line: not committed")
	}
	if k, o, c := s.Lookup("eeeeeeee", "k1", holder); !k || o || c {
		t.Fatal("abandoned: closed and not committed (ADV-07)")
	}
	if k, _, _ := s.Lookup("aaaaaaaa", "k2", holder); k {
		t.Fatal("another profile's key: not this record's restore")
	}
	if k, _, _ := s.Lookup("aaaaaaaa", "k1", elsewhere); k {
		t.Fatal("met in another folder than the Local Folders it wrote into: not this record's restore (ADV-06)")
	}
	if k, _, _ := s.Lookup("aaaaaaaa", "k1", nil); k {
		t.Fatal("no folder to compare: not this record's restore")
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
	if k, _, _ := s.Lookup("cccccccc", "k1", holder); k {
		t.Fatal("the log as it was (size and time): no re-read")
	}
	_ = os.Chtimes(p, fi.ModTime().Add(time.Second), fi.ModTime().Add(time.Second))
	_ = l.Append(started("dddddddd"))
	if k, o, c := s.Lookup("dddddddd", "k1", holder); !k || !o || c {
		t.Fatal("the log changed: one re-read finds a restore that began since")
	}
	if s.LogUnreadable() {
		t.Fatal("readable")
	}
}
