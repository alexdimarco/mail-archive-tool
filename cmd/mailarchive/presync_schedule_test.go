package main

import (
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-550
// Schedule-time refusals (§4.2.4): -thunderbird-profile without -thunderbird-sync
// is refused. The pre-sync is NO LONGER refused on an hourly schedule (owner ruling
// 2026-10-23: many run Thunderbird only for archiving) — it is allowed on any
// cadence, and the runtime detectRunning defer (session_test.go, OutcomeAlreadyRunning)
// never starts or quits a Thunderbird the owner is actively using, which is what
// makes a frequent pre-sync safe. The platform refusal (-thunderbird-sync off an
// adapter) is TestScheduledJobRefusesThunderbirdSyncOffAdapter (Linux-tagged).
func TestScheduledThunderbirdSyncRefusals(t *testing.T) {
	out := t.TempDir()
	store := t.TempDir()

	code, msg := parseJobRefusal([]string{"-out", out, "-input", store, "-thunderbird-profile", store})
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-thunderbird-profile", "-thunderbird-sync"))
}
