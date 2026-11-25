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
// counts (the adversarial pass's FS-01 and round 2's R2-01, kept as tombstones): the walk lists
// folders and reads each file's size and time through an os.Root opened on the store, which refuses
// a path that leaves it. Outside the store: a file of 777,777,777 bytes, and the same 1,000 names
// the store's folder z.sbd holds, at 1,000,000 bytes each and dated 2031. z.sbd is swapped for a
// link to that folder and back every 0.2 ms: no count, in about 2 seconds of counts, takes a size or
// a time from outside. Positive twins first: with nothing swapped the store counts its own 1,005
// bytes, and a link left in place is not followed (5). Structural: storeSize walks root.FS().
func TestAFolderSwappedForALinkIsNotFollowed(t *testing.T) {
	const big = 777_777_777 // sparse files: no disk used
	outside := t.TempDir()
	pickFile(t, filepath.Join(outside, "BIG"), big)
	store := t.TempDir()
	pickFile(t, filepath.Join(store, "INBOX"), 5)
	sub := filepath.Join(store, "z.sbd")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	future := time.Date(2031, 1, 2, 3, 4, 5, 0, time.UTC)
	for i := 0; i < 1000; i++ {
		name := fmt.Sprintf("f%04d", i)
		pickFile(t, filepath.Join(sub, name), 1)
		pickFile(t, filepath.Join(outside, name), 1_000_000)
		if err := os.Chtimes(filepath.Join(outside, name), future, future); err != nil {
			t.Fatal(err)
		}
	}
	later := func() time.Time { return time.Now().Add(time.Minute) }
	if n, newest, floor, ok := storeSize(store, later(), sizeEntryMax); n != 1005 || floor || !ok || !newest.Before(future) {
		t.Fatalf("positive twin: nothing swapped: %d %v %v %v, want 1005", n, newest, floor, ok)
	}
	aside := filepath.Join(t.TempDir(), "z.sbd")
	swapIn := func() { _ = os.Rename(sub, aside); _ = os.Symlink(outside, sub) }
	swapOut := func() { _ = os.Remove(sub); _ = os.Rename(aside, sub) }
	swapIn()
	if n, _, _, ok := storeSize(store, later(), sizeEntryMax); n != 5 || !ok {
		t.Fatalf("positive twin: the link left in place is not followed: %d %v, want 5", n, ok)
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
		if n, newest, _, ok := storeSize(store, later(), sizeEntryMax); ok && (n > 1005 || !newest.Before(future)) {
			close(stop)
			<-done
			t.Fatalf("count %d took sizes or times from outside the store through the link swapped in for z.sbd: %d bytes (the store holds 1,005), newest %v", runs+1, n, newest)
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
		"info, err := root.Lstat(p)",
		// The folder opened is the one looked at: a store swapped for a link between the two is not
		// counted (not drivable here: it needs the swap between two calls).
		"if ri, err := root.Stat(\".\"); err != nil || !os.SameFile(fi, ri) {\n\t\treturn 0, time.Time{}, false, false\n\t}",
	} {
		if !strings.Contains(string(src), wired) {
			t.Errorf("storeSize no longer walks through an os.Root: %q is gone from picker.go", wired)
		}
	}
}
