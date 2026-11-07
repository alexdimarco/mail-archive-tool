package tbrestore

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/interchange"
)

// covers: MA-1119, R40
// The layout (design §3.4 as folded): one new folder "Restored — Work mail" (its
// empty parent file, made last) and its .sbd holding the marker (0600, one JSON line
// naming this restore and profile), the made-list (one line per entry made), and one
// mbox file per archive folder — a folder with subfolders gets its file (empty when it
// holds no messages) and a sibling NAME.sbd; no .msf; a name that had to change shows
// changed; Thunderbird's own folders in Local Folders untouched.
func TestRestoreLayoutOnDisk(t *testing.T) {
	in, msgs := sampleFolders()
	fx := newFixture(t, in, msgs)
	trash, _ := os.ReadFile(filepath.Join(fx.lf.Dir, "Trash"))
	res, id := fx.write(context.Background(), nil)
	if res.Outcome != OutDone || res.Restored != 5 {
		t.Fatalf("got %s %d %v: %s", res.Outcome, res.Restored, res.Err, res.Words)
	}
	mustContain(t, res.Words, "Restored 5 messages", "'Restored — Work mail'", "checked every message")
	top := filepath.Join(fx.lf.Dir, "Restored — Work mail")
	c := top + ".sbd"
	for _, want := range []string{"Inbox", "Inbox.sbd/Receipts", "Sent", "Projects", "Projects.sbd/Q1 Reports~"} {
		found := false
		for _, got := range madeOnDisk(t, c) {
			if got == want || (strings.HasSuffix(want, "~") && strings.HasPrefix(got, want)) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s not in the layout: %v", want, madeOnDisk(t, c))
		}
	}
	if fi, err := os.Lstat(top); err != nil || !fi.Mode().IsRegular() || fi.Size() != 0 {
		t.Fatalf("the parent file is empty and regular: %v %v", fi, err)
	}
	if fi, _ := os.Lstat(filepath.Join(c, "Projects")); fi == nil || fi.Size() != 0 {
		t.Error("a parent with no messages of its own is an empty file")
	}
	for _, e := range madeOnDisk(t, c) {
		if strings.HasSuffix(e, ".msf") {
			t.Errorf("no .msf is written: %s", e)
		}
	}
	m, err := os.ReadFile(filepath.Join(c, MarkerName))
	if err != nil || bytes.Count(m, []byte("\n")) != 1 {
		t.Fatalf("the marker is one line: %q %v", m, err)
	}
	var mk Marker
	if json.Unmarshal(m, &mk) != nil || mk.ID != id || mk.MailArchive != "restore" || mk.Profile != ProfileKeyOf(fx.profile) || mk.Started == "" {
		t.Fatalf("the marker names this restore and profile: %s", m)
	}
	if runtime.GOOS != "windows" {
		for _, n := range []string{MarkerName, MadeListName, "Inbox"} {
			if fi, _ := os.Lstat(filepath.Join(c, n)); fi == nil || fi.Mode().Perm() != 0o600 {
				t.Errorf("%s is 0600", n)
			}
		}
		if fi, _ := os.Lstat(c); fi.Mode().Perm() != 0o700 {
			t.Error("the container is 0700")
		}
	}
	l := listed(t, fx.root, "Restored — Work mail.sbd")
	for _, e := range madeOnDisk(t, c) {
		if !l[e] {
			t.Errorf("%s on disk is not on the made-list", e)
		}
	}
	if len(l) != len(madeOnDisk(t, c)) {
		t.Errorf("the made-list names exactly what was made: %v vs %v", l, madeOnDisk(t, c))
	}
	if got, _ := os.ReadFile(filepath.Join(fx.lf.Dir, "Trash")); !bytes.Equal(got, trash) {
		t.Error("Thunderbird's own folder was changed")
	}
	if ev := fx.events(id); dump(ev) != "[started checked ended]" {
		t.Errorf("the log: %v", ev)
	}

	// Each message reads back as its original (MA-1089's product reader, against the file).
	data, err := os.ReadFile(filepath.Join(c, "Inbox"))
	if err != nil {
		t.Fatal(err)
	}
	var got [][]byte
	if err := interchange.ReadMboxrd(bytes.NewReader(data), func(b []byte) error { got = append(got, append([]byte(nil), b...)); return nil }); err != nil {
		t.Fatal(err)
	}
	assure.Reached(t, got, "messages read back")
	if !bytes.Equal(got[0], msgs[0][0]) || !bytes.Equal(got[1], append(append([]byte(nil), msgs[0][1]...), '\n')) {
		t.Fatalf("read back: %q", got)
	}
	if !bytes.Contains(data, []byte("\r\n>From here a line\r\n")) || !bytes.Contains(data, []byte("\n>>>From deep\n")) {
		t.Fatalf("mboxrd quoting in the file: %q", data)
	}
}

// covers: MA-1119, R40
// One store vs several (the archive side gives each store a level of its own, named by
// its label, only when more than one store has something to restore), and "(no
// folder)" for records with none: the layout keeps the levels it is given.
func TestRestoreLayoutStoresAndNoFolder(t *testing.T) {
	in := []FolderIn{
		{Names: []string{"ann@example.org"}},
		{Names: []string{"ann@example.org", "Inbox"}, Messages: 1},
		{Names: []string{"Local — old.mbox"}},
		{Names: []string{"Local — old.mbox", "Inbox"}, Messages: 1},
		{Names: []string{"(no folder)"}, Messages: 1},
	}
	m := []byte("Subject: x\n\nbody\n")
	fx := newFixture(t, in, [][][]byte{nil, {m}, nil, {m}, {m}})
	res, _ := fx.write(context.Background(), nil)
	if res.Outcome != OutDone {
		t.Fatalf("%s %v", res.Outcome, res.Err)
	}
	got := strings.Join(madeOnDisk(t, filepath.Join(fx.lf.Dir, "Restored — Work mail.sbd")), "|")
	for _, want := range []string{"ann@example.org.sbd/Inbox", "Local — old.mbox.sbd/Inbox", "(no folder)"} {
		if !strings.Contains(got, want) {
			t.Errorf("%s missing: %s", want, got)
		}
	}
}
