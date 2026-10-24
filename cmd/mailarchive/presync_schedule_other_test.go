//go:build !darwin && !windows

package main

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-550
// On a platform WITHOUT a pre-sync adapter (Linux in this version), -thunderbird-sync
// is refused at PARSE time — "fail now, not at 02:00" (R30, inert where unavailable) —
// so a schedule can never bake in a sync the runtime could not perform. The accepting
// twin is TestScheduledJobCarriesThunderbirdSync, build-tagged to macOS and Windows,
// where the adapter exists.
func TestScheduledJobRefusesThunderbirdSyncOffAdapter(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()
	code, msg := parseJobRefusal([]string{"-out", out, "-input", store, "-thunderbird-sync"})
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-thunderbird-sync", "macOS and Windows"))
}
