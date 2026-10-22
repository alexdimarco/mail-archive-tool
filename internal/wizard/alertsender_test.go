package wizard

import (
	"os"
	"testing"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/backupalert"
)

// TestMain installs a no-op backup-alert sender for the whole package, so a test
// that exercises a scheduled run's failure path (RunHeadless -> FireForRunDefault)
// can NEVER send a real email through whatever alert config happens to be on the
// machine running the tests. Tests that assert on the alert install their own
// recording sender via SetSenderForTest and restore to this no-op.
func TestMain(m *testing.M) {
	backupalert.SetSenderForTest(func(alert.Config, string, alert.Event) error { return nil })
	os.Exit(m.Run())
}
