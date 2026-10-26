package health

import (
	"os"
	"testing"

	"mail-archive-tool/internal/encrecord"
)

// TestMain points this computer's record of encrypted archives at a throwaway
// folder, so status's tests never read the developer's real record.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "mailarchive-health-record-")
	if err != nil {
		panic(err)
	}
	restore := encrecord.SetDirForTest(dir)
	code := m.Run()
	restore()
	os.RemoveAll(dir)
	os.Exit(code)
}
