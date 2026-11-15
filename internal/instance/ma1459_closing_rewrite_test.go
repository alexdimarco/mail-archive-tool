package instance

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/util"
)

// covers: MA-1459, MA-1403, R23
// Merging 1.7.2 into the menu-bar slice: the closing mark (MarkClosing, the S1 fix round's A7)
// rewrites the instance file through the 1.7.2 writer like every other write of it. When Windows
// refuses the label at that moment, the file is written as 1.7.1 wrote it, recording both that the
// copy is closing and that the file is unlabelled; the writer is told once, and the holder
// remembers it, so Watch writes the label again as soon as Windows allows it and the file still
// says the copy is closing. Positive twin first: a labelled copy's closing file stays labelled and
// nothing is said.
func TestMA1459_TheClosingMarkGoesThroughTheFallback(t *testing.T) {
	dir := t.TempDir()
	keyHex, _ := NewKey()
	info := Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex}

	twin, err := Acquire(filepath.Join(dir, "twin"), "")
	if err != nil {
		t.Fatal(err)
	}
	var twinNotes []string
	twin.Note = func(s string) { twinNotes = append(twinNotes, s) }
	if err := twin.Publish(info); err != nil {
		t.Fatal(err)
	}
	twin.MarkClosing()
	if got, err := Read(twin.FilePath()); err != nil || !got.Closing || got.Unlabelled || len(twinNotes) != 0 {
		t.Fatalf("positive twin: a labelled copy's closing file is %+v (%v), said %q; want closing, labelled, nothing said", got, err, twinNotes)
	}
	twin.Close()

	var mu sync.Mutex
	refuse := false
	toggled := func(path string, data []byte) error {
		mu.Lock()
		r := refuse
		mu.Unlock()
		if r {
			return refusing(privilegeNotHeld)(path, data)
		}
		return util.WriteSecretFileAtomic0600(path, data)
	}
	restore := SetWritersForTest(toggled, util.WriteFileAtomic0600)
	defer restore()

	h, err := Acquire(filepath.Join(dir, "state"), "")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	var notes []string
	h.Note = func(s string) { notes = append(notes, s) }
	if err := h.Publish(info); err != nil {
		t.Fatal(err)
	}
	if got, _ := Read(h.FilePath()); got.Unlabelled || len(notes) != 0 {
		t.Fatalf("premise: the published file is not labelled (%+v, %q)", got, notes)
	}

	mu.Lock()
	refuse = true
	mu.Unlock()
	h.MarkClosing()
	got, err := Read(h.FilePath())
	if err != nil || got.Key != keyHex || !got.Closing || !got.Unlabelled {
		t.Fatalf("the closing mark with the label refused wrote %+v (%v); want the 1.7.1 file, closing and recording it is unlabelled", got, err)
	}
	if len(notes) != 1 {
		t.Fatalf("the closing mark with the label refused told the writer %q; want the one warning", notes)
	}
	h.mu.Lock()
	remembered := h.info.Unlabelled
	h.mu.Unlock()
	if !remembered {
		t.Fatal("the holder does not remember that the closing file is unlabelled, so Watch would never label it")
	}

	mu.Lock()
	refuse = false
	mu.Unlock()
	var logged []string
	var lmu sync.Mutex
	stop := h.Watch(10*time.Millisecond, func(s string) { lmu.Lock(); logged = append(logged, s); lmu.Unlock() })
	defer stop()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if got, _ := Read(h.FilePath()); !got.Unlabelled && got.Closing && got.Key == keyHex {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Watch never wrote the closing file with its label once Windows allowed it")
		}
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	lmu.Lock()
	defer lmu.Unlock()
	if len(logged) != 1 || logged[0] != "the private key file is protected now" {
		t.Fatalf("Watch logged %q", logged)
	}
}
