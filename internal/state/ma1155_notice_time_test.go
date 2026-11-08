package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1155, R18, S48
// BACKUP-NEEDS-ATTENTION.txt says when the run ended in the reader's own local time with its
// offset from UTC, as the alert email gives local time (the 1.6.1 friction review's P5: Notepad
// showed "When: 2026-11-07T22:25:10Z"): the outright failure's notice, the partial run's and an
// encrypted archive's alike, never the UTC ISO form. Positive twin first: the failure's notice
// still names its reason beside the time.
func TestTheAttentionNoticeSaysWhenInLocalTime(t *testing.T) {
	ended := time.Date(2026, 11, 7, 22, 25, 10, 0, time.UTC)
	want := "When:    " + ended.Local().Format("2006-01-02 15:04:05 -07:00") + "\n"
	notice := func(r LastRun) string {
		t.Helper()
		out := t.TempDir()
		r.Status, r.Finished = RunFailed, ended
		if err := WriteLastRun(out, r); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(out, AttentionName))
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	failed := notice(LastRun{Error: "the archive drive is full"})
	if !strings.Contains(failed, "Reason:  the archive drive is full") {
		t.Fatalf("the positive twin: the failure's notice lost its reason:\n%s", failed)
	}
	p := &PartialCounts{Mistake: PartialKind{Messages: 1}}
	partial := notice(LastRun{Error: PartialText(*p, true), Partial: p})
	enc := notice(LastRun{Encrypted: true, ErrorKind: FailMailboxFailed})
	for name, body := range map[string]string{"failure": failed, "partial": partial, "encrypted": enc} {
		assure.Reached(t, strings.Contains(body, want) && !strings.Contains(body, "2026-11-07T22:25:10Z"),
			"the "+name+" notice says "+strings.TrimSpace(want)+":\n"+body)
	}
}
