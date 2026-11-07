package tbrestore

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-1142, R40, S68
// restores.log (design §7.4 as folded), on the deletion log's append discipline
// (util.AppendLogLine, lifted in this slice with delete's rows green): owner-only, a
// torn line — at the end, or in the middle once the next append started on a line of
// its own — tolerated; a line that is not a torn object refused (W-LOGBAD) and a full
// log refused (W-LOGFULL), each naming the folder and its remedy, with nothing created
// in Local Folders; the started line on disk before the first create (fsync point
// [1]); and a restore's line sequence: started, name, checked, ended — and taken and
// abandoned at a taken name.
func TestRestoresLog(t *testing.T) {
	dir := t.TempDir()
	l := NewLog(filepath.Join(dir, LogName))
	if lines, err := l.Read(); err != nil || len(lines) != 0 {
		t.Fatalf("an absent log has no lines: %v %v", lines, err)
	}
	if err := l.Append(LogLine{Event: EvStarted, ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if fi, _ := os.Stat(l.Path); fi.Mode().Perm() != 0o600 {
			t.Errorf("the log is 0600: %v", fi.Mode())
		}
	}
	f, _ := os.OpenFile(l.Path, os.O_APPEND|os.O_WRONLY, 0)
	_, _ = f.WriteString(`{"at":"x","event":"ended","id":"a","res`) // a power cut mid-append
	f.Close()
	if lines, err := l.Read(); err != nil || len(lines) != 1 {
		t.Fatalf("a torn last line is tolerated: %v %v", lines, err)
	}
	if err := l.Append(LogLine{Event: EvEnded, ID: "a"}); err != nil {
		t.Fatal(err)
	}
	lines, err := l.Read()
	if err != nil || len(lines) != 2 || lines[1].Event != EvEnded {
		t.Fatalf("a torn line in the middle is tolerated and the next line is whole: %v %v", lines, err)
	}

	bad := NewLog(filepath.Join(t.TempDir(), LogName))
	writeFile(t, bad.Path, "{\"event\":\"started\",\"id\":\"a\"}\nnot json at all\n")
	_, err = bad.Read()
	refusedAs(t, err, "W-LOGBAD", nil, "restores.log", "can't be read", "Move that file", "no longer know which folders")

	full := NewLog(filepath.Join(t.TempDir(), LogName))
	writeFile(t, full.Path, strings.Repeat("{\"event\":\"x\",\"id\":\"y\"}\n", (logCap-logReserve)/23+1))
	err = full.Append(LogLine{Event: EvStarted, ID: "b"})
	refusedAs(t, err, "W-LOGFULL", nil, "restores.log", "is full", "delete it")
	if err := full.Append(LogLine{Event: EvEnded, ID: "b"}); err != nil {
		t.Fatalf("a closing line still goes in the reserve: %v", err)
	}

	// Through a write: W-LOGFULL creates nothing in Local Folders.
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	fx.log = full
	before := fx.local()
	res, _ := fx.write(context.Background(), nil)
	refusedAs(t, res.Err, "W-LOGFULL", func() bool { return fx.local() == before }, "is full")

	// The started line is on disk before the first create.
	fx2 := newFixture(t, in, msgs)
	seen := false
	f2 := &faultFS{inner: newRootFS(fx2.root), hook: func(op, name string) error {
		if op == "mkdir" && !seen {
			seen = true
			if ls := mustRead(t, fx2.log); len(ls) != 1 || ls[0].Event != EvStarted || ls[0].Local != fx2.lf.Dir || ls[0].Profile != ProfileKeyOf(fx2.profile) || ls[0].Name != "Restored — Work mail" {
				t.Errorf("the started line before the Mkdir: %+v", ls)
			}
		}
		return nil
	}}
	if res, _ := fx2.write(context.Background(), f2); res.Outcome != OutDone || !seen {
		t.Fatal("the write")
	}

	// An encrypted archive's started line names no mail folder.
	fx3 := newFixture(t, in, msgs)
	fx3.enc = true
	if res, _ := fx3.write(context.Background(), nil); res.Outcome != OutDone {
		t.Fatal(res.Err)
	}
	raw, _ := os.ReadFile(fx3.log.Path)
	for _, folder := range []string{"Inbox", "Receipts", "Sent", "Projects"} {
		if strings.Contains(string(raw), folder) {
			t.Errorf("an encrypted archive's log names the folder %s", folder)
		}
	}
}
