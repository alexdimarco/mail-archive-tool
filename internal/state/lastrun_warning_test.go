package state

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// covers: MA-375, R12, S45
// A run's setup warnings round-trip through the last-run record, and like the
// rest of it they are untrusted on read (anyone who can write the archive can
// edit the record): control characters are stripped, so a forged warning cannot
// carry a terminal escape or a forged line into `status`. A record with no
// warnings carries no "warnings" key, so older binaries read it unchanged.
func TestLastRunWarningsRoundTripStripped(t *testing.T) {
	out := t.TempDir()
	clean := RunWarning{Code: "credential_in_archive", Message: "the token cache /a/graph.token is inside the archive folder /a"}
	if err := WriteLastRun(out, LastRun{Status: RunOK, Started: time.Now(), Finished: time.Now(), Warnings: []RunWarning{
		clean,
		{Code: "config_in_archive\x1b[2J", Message: "settings inside\r\nRED: forged line\x07"},
	}}); err != nil {
		t.Fatal(err)
	}
	lr, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent {
		t.Fatalf("read: state=%v err=%v", st, err)
	}
	if len(lr.Warnings) != 2 || !reflect.DeepEqual(lr.Warnings[0], clean) {
		t.Fatalf("warnings did not round-trip: %+v", lr.Warnings)
	}
	if w := lr.Warnings[1]; strings.ContainsAny(w.Code+w.Message, "\x1b\x07\r\n") || w.Code != "config_in_archive[2J" {
		t.Errorf("control characters survived ReadLastRun: %q", w)
	}

	if err := WriteLastRun(out, LastRun{Status: RunOK, Started: time.Now(), Finished: time.Now()}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(out, LastRunName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "warnings") {
		t.Errorf("a clean run's record carries a warnings key:\n%s", raw)
	}
	if lr, _, _ = ReadLastRun(out); lr.Warnings != nil {
		t.Errorf("a clean run read back warnings %+v", lr.Warnings)
	}
}
