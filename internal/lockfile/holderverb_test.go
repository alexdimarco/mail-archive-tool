package lockfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-394, R18
// HolderVerb reads back the verb a holder named through AcquireAs, which is
// how `status` tells a verify that holds the archive lock from any other
// holder: "verify" for a verify, "export" for an export, "" for a plain
// Acquire (status's own probe). The verb is written last, after the host
// name, so a long host name must not hide it (the line is read whole, not cut
// at Holder's 256-byte display cap).
func TestHolderVerbNamesWhoHoldsTheLock(t *testing.T) {
	dir := t.TempDir()
	for _, verb := range []string{"verify", "export", ""} {
		path := filepath.Join(dir, verb+Name)
		l, err := AcquireAs(path, verb)
		if err != nil {
			t.Fatal(err)
		}
		got := HolderVerb(path)
		l.Release()
		if got != verb {
			t.Errorf("a lock acquired as %q reads back verb %q (holder line %q)", verb, got, Holder(path))
		}
	}

	// A holder line as AcquireAs writes it, on a host whose name is long.
	path := filepath.Join(dir, "longhost"+Name)
	line := "pid=4242 started=2026-10-09T12:00:00Z host=" + strings.Repeat("h", 200) + ".example nonce=0123456789abcdef0123456789abcdef verb=verify\n"
	if len(line) <= 256 {
		t.Fatalf("fixture line is %d bytes; it must exceed the 256-byte display cap", len(line))
	}
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := HolderVerb(path); got != "verify" {
		t.Fatalf("a %d-byte holder line reads back verb %q, want verify", len(line), got)
	}
}
