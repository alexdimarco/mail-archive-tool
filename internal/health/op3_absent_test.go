package health

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-647, R18
// An absent archive location (an unplugged removable/network drive, a deleted
// folder) must read as unreachable, not as a brand-new empty archive whose
// no-schedule remedy would invite a SECOND schedule install (OP-3, R18): Gather
// sets OutAbsent and Assess raises out_absent and suppresses no_schedule. An
// existing empty archive still reads no_schedule.
func TestGatherAbsentArchiveLocationIsUnreachableNotNew(t *testing.T) {
	t.Skip("xfail: OP-3 (R18) — out_absent is not wired until the fix commit removes this skip")

	has := func(r Report, code string) bool {
		for _, c := range r.Codes {
			if c == code {
				return true
			}
		}
		return false
	}

	absent := filepath.Join(t.TempDir(), "unplugged-drive")
	in := Gather(absent, "")
	if !in.OutAbsent {
		t.Fatalf("Gather(%q): OutAbsent=false, want true for a missing location", absent)
	}
	rep := Assess(in, time.Now())
	if !has(rep, "out_absent") {
		t.Errorf("absent location: codes %v lack out_absent", rep.Codes)
	}
	if has(rep, "no_schedule") {
		t.Errorf("absent location: no_schedule present — it invites a second install (%v)", rep.Codes)
	}

	// Positive twin: an existing empty archive still warns no_schedule, not out_absent.
	empty := t.TempDir()
	ein := Gather(empty, "")
	if ein.OutAbsent {
		t.Fatalf("Gather(%q): OutAbsent=true for an existing empty dir", empty)
	}
	erep := Assess(ein, time.Now())
	if !has(erep, "no_schedule") {
		t.Errorf("existing empty archive: codes %v lack no_schedule", erep.Codes)
	}
	if has(erep, "out_absent") {
		t.Errorf("existing empty archive: out_absent wrongly present (%v)", erep.Codes)
	}
}
