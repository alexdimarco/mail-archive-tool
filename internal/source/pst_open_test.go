package source

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-57, R10, S18
// A corrupt/truncated Outlook data file must fail to open with a clean error,
// never a panic that escapes the reader — which would crash the whole run, or
// exit the no-console Windows GUI silently. go-pst indexes unchecked into parsed
// buffers, so a bad .ost can panic inside pst.New; Open must contain it.
func TestOpenCorruptPSTDoesNotPanic(t *testing.T) {
	real, err := os.ReadFile("../../testdata/support.pst")
	if err != nil {
		t.Skipf("fixture missing: %v", err)
	}
	data := make([]byte, len(real))
	copy(data, real)
	// Byte mutations found by fuzzing the fixture that drive go-pst's pst.New into
	// a "slice bounds out of range" panic.
	for _, m := range []struct {
		off int
		val byte
	}{
		{66538, 85}, {204130, 1}, {215730, 170}, {247815, 109},
		{462932, 220}, {477864, 116}, {560978, 144}, {609554, 224},
	} {
		data[m.off] = m.val
	}
	p := filepath.Join(t.TempDir(), "corrupt.ost")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}

	// Open must return an error, not panic. (An escaping panic crashes this test
	// binary — exactly the failure mode this guards against.)
	if s, err := Open(p); err == nil {
		s.Close()
		t.Error("expected an error opening a corrupt PST, got nil")
	}

	// The discovery readability probe sits on the same path and must also be
	// panic-safe, reporting the file unreadable so auto-discovery skips it.
	if DataFileReadable(p) {
		t.Error("a corrupt PST must be reported unreadable by DataFileReadable")
	}
}
