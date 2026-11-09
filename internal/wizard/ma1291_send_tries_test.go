package wizard

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/health"
	"mail-archive-tool/internal/state"
)

// covers: MA-1291, MA-1184
// SPAM-R2-A through the real scheduled entry point (RunHeadless) and a real Thunderbird profile with
// an account still downloading: every send of the still-downloading email fails. The first 5 due
// backups try it; the 6th and 7th do not, and after each of them `status` (health.Gather and Assess,
// as Status & health reads it) shows the alert_send_failed WARN, so the WARN survives the run writing
// its own last-run record first. Positive twin first: one failure, then the next backup sends it,
// and no WARN.
func TestAFailingDownloadEmailIsGivenUpWithAWarning(t *testing.T) {
	codes := func(out string) string {
		t.Helper()
		return strings.Join(health.Assess(health.Gather(out, ""), time.Now()).Codes, " ")
	}
	fail := func(n int) []error {
		var e []error
		for range n {
			e = append(e, errors.New("the mail server did not answer"))
		}
		return e
	}
	tw := newLocalRunRig(t)
	tj := tw.jobAt(filepath.Join(tw.dir, "tb.json"), tbStoreFixture(t, tw.dir, "imap.qzc5t.example", 40, 3))
	RunHeadless(tj)
	tw.tbBackdate(true)
	tw.sendErrs = fail(1)
	if RunHeadless(tj) != 0 || RunHeadless(tj) != 0 || tw.tbSent() != 1 || strings.Contains(codes(tw.out), "alert_send_failed") {
		t.Fatalf("positive twin: sent %q, codes %s", tw.results(), codes(tw.out))
	}

	g := newLocalRunRig(t)
	j := g.jobAt(filepath.Join(g.dir, "tb.json"), tbStoreFixture(t, g.dir, "imap.qzc5g.example", 40, 3))
	RunHeadless(j)
	g.tbBackdate(true)
	g.sendErrs = fail(9)
	for i := 1; i <= 7; i++ {
		if RunHeadless(j) != 0 {
			t.Fatalf("backup %d failed", i)
		}
		if got := strings.Contains(codes(g.out), "alert_send_failed"); got != (i >= state.AlertSendTries) {
			t.Errorf("backup %d: status shows alert_send_failed %v", i, got)
		}
	}
	if tried := 9 - len(g.sendErrs); tried != state.AlertSendTries || g.tbSent() != 0 {
		t.Errorf("%d sends tried over 7 due backups (want %d), %d sent", tried, state.AlertSendTries, g.tbSent())
	}
}
