package health

import (
	"os"
	"testing"

	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/vault"
)

// TestMain points this computer's record of encrypted archives at a throwaway
// folder, so status's tests never read the developer's real record. Its tests
// make keys at the cheap test cost (MA-776).
func TestMain(m *testing.M) {
	vault.UseTestCost()
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
