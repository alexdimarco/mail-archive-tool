package main

import (
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-271, R19, R12, S41
// `mailarchive-desktop` refuses a non-loopback bind: the dashboard is a control
// surface (capture / sign-in / schedule) and must never be reachable from the
// network. The refusal happens before the server binds.
func TestDesktopRefusesNonLoopback(t *testing.T) {
	err := run([]string{"-addr", "0.0.0.0:8097", "-out", t.TempDir()})
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	assure.Reached(t, msg, "non-loopback refusal")
	if !strings.Contains(msg, "loopback") {
		t.Errorf("refusal should name 'loopback': %v", err)
	}
}
