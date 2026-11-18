//go:build !windows

package wizard

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-1466
// The size count never follows a link out of the store, even one swapped in for a folder while it
// counts (the adversarial pass's FS-01, kept as a tombstone): the walk goes through an os.Root opened
// on the store, which refuses a path that leaves it. A file of 777,777,777 bytes outside the store,
// and a folder z.sbd swapped for a link to it and back every 0.2 ms: no count, in about 2 seconds of
// counts, includes it. Positive twins first: with nothing swapped the store counts its own 1,005
// bytes, and a link left in place is not followed (1,000). Structural: storeSize walks root.FS().
func TestAFolderSwappedForALinkIsNotFollowed(t *testing.T) {
	const big = 777_777_777 // a sparse file: no disk used
	outside := t.TempDir()
	pickFile(t, filepath.Join(outside, "BIG"), big)
	store := t.TempDir()
	for i := 0; i < 1000; i++ {
		pickFile(t, filepath.Join(store, fmt.Sprintf("a%04d", i)), 1)
	}
	sub := filepath.Join(store, "z.sbd")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	pickFile(t, filepath.Join(sub, "INBOX"), 5)
	later := func() time.Time { return time.Now().Add(time.Minute) }
	if n, _, floor, ok := storeSize(store, later(), sizeEntryMax); n != 1005 || floor || !ok {
		t.Fatalf("positive twin: nothing swapped: %d %v %v, want 1005", n, floor, ok)
	}
	aside := filepath.Join(t.TempDir(), "z.sbd")
	swapIn := func() { _ = os.Rename(sub, aside); _ = os.Symlink(outside, sub) }
	swapOut := func() { _ = os.Remove(sub); _ = os.Rename(aside, sub) }
	swapIn()
	if n, _, _, ok := storeSize(store, later(), sizeEntryMax); n != 1000 || !ok {
		t.Fatalf("positive twin: the link left in place is not followed: %d %v, want 1000", n, ok)
	}
	swapOut()

	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			swapIn()
			time.Sleep(200 * time.Microsecond)
			swapOut()
			time.Sleep(200 * time.Microsecond)
		}
	}()
	runs := 0
	for end := time.Now().Add(2 * time.Second); time.Now().Before(end); runs++ {
		if n, _, _, ok := storeSize(store, later(), sizeEntryMax); ok && n >= big {
			close(stop)
			<-done
			t.Fatalf("count %d followed the link swapped in for z.sbd: %d bytes, the %d of a file outside the store among them", runs+1, n, int64(big))
		}
	}
	close(stop)
	<-done
	if runs == 0 {
		t.Fatalf("no count ran")
	}

	src, err := os.ReadFile("picker.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, wired := range []string{
		"root, err := os.OpenRoot(dir)",
		"err = fs.WalkDir(root.FS(), \".\",",
		// The folder opened is the one looked at: a store swapped for a link between the two is not
		// counted (not drivable here: it needs the swap between two calls).
		"if ri, err := root.Stat(\".\"); err != nil || !os.SameFile(fi, ri) {\n\t\treturn 0, time.Time{}, false, false\n\t}",
	} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("storeSize no longer walks through an os.Root: %q is gone from picker.go", wired)
		}
	}
}
