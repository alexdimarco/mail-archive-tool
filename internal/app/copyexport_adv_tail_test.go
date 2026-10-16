package app

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-448, R29, R5
// EW-AC1 tombstone, the remaining window (the fresh-eyes check). Where the OS has no append
// mode (Windows) appends of the run record are ordered by recordWriteMu, but opening a
// second handle on the record cut a torn tail back (a truncate) outside it, so the open could
// cut the end of a line the first handle was writing at that moment. The cut is now taken under
// the same mutex: while an append holds it, a second open waits; once it is free, a record
// that was torn by a kill is cut back to its last whole line as before.
func TestCopyExportTornTailCutIsOrderedWithAppends(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"torn"), 0o600); err != nil {
		t.Fatal(err)
	}
	recordWriteMu.Lock() // an append of this process is in progress
	done := make(chan struct{})
	go func() {
		defer close(done)
		if f, err := openAppendable(path, false); err == nil {
			f.Close()
		} else {
			t.Error(err)
		}
	}()
	select {
	case <-done:
		recordWriteMu.Unlock()
		t.Fatal("a second handle cut the record's tail while an append held the write mutex")
	case <-time.After(300 * time.Millisecond):
	}
	recordWriteMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the open did not go on once the append was done")
	}
	if b, _ := os.ReadFile(path); string(b) != "{\"a\":1}\n" {
		t.Errorf("the torn tail was not cut back to the last whole line: %q", b)
	}
}
