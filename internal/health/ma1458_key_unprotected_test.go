package health

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-1458, R23
// The final check's security-1 and wording-2: when Windows would not protect the running
// MailArchive Desktop's private key file (the research note's L0 fallback, MA-1453), `status` and
// Status & health say so in one plain information line, never a warning and never a change of
// posture (it works as 1.7.1 did), so a person, and the signed-in L0 check, can see whether the
// protection applied. Positive twin first: with the file protected, nothing is said.
func TestAKeyFileKeptAsIn171IsOneInformationLine(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	in := healthyInput(now)
	rep := Assess(in, now)
	if rep.Posture != "GREEN" || strings.Contains(strings.Join(rep.Reasons, "\n"), "private key file") {
		t.Fatalf("positive twin: %s %v", rep.Posture, rep.Reasons)
	}
	in.DesktopUnlabelled = true
	rep = Assess(in, now)
	want := "On this computer Windows would not protect MailArchive Desktop's private key file, so it is kept as in 1.7.1: a sandboxed program running under your account may be able to read it."
	if rep.Posture != "GREEN" || !hasCode(rep, "desktop_key_unprotected") {
		t.Fatalf("posture %s, codes %v: want GREEN with desktop_key_unprotected", rep.Posture, rep.Codes)
	}
	found := false
	for _, r := range rep.Reasons {
		if r == want {
			found = true
		}
		if strings.Contains(r, "private key file") && r != want {
			t.Errorf("the line is %q, want %q (no WARN or RED prefix)", r, want)
		}
	}
	if !found {
		t.Fatalf("the reasons lack %q: %v", want, rep.Reasons)
	}
	if summary := strings.Join(Summary(in, rep), "\n"); !strings.Contains(summary, want) {
		t.Errorf("status's summary lacks the line:\n%s", summary)
	}
}
