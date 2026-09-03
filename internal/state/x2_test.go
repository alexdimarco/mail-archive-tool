package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// covers: MA-157, R18, S29
// The last-run record is untrusted (anyone who can write the archive directory
// can edit it): ReadLastRun strips control characters from every Job element
// and from Error/Exe/Mode at the read choke point, so an ANSI escape or an
// embedded newline cannot forge an output line on any consumer.
func TestReadLastRunStripsControl(t *testing.T) {
	out := t.TempDir()
	if err := WriteLastRun(out, LastRun{
		Status:  RunFailed,
		Started: time.Now(),
		Job:     []string{"-out", "/a\x1b[2J", "-input", "/b\r\nevil"},
		Error:   "boom\x1b]0;pwn\x07",
		Exe:     "/bin/mailarchive\nfake",
		Mode:    "incremental\x1b[31m",
	}); err != nil {
		t.Fatal(err)
	}
	lr, st, err := ReadLastRun(out)
	if err != nil || st != LastRunPresent {
		t.Fatalf("read: state=%v err=%v", st, err)
	}
	all := strings.Join(append([]string{lr.Error, lr.Exe, lr.Mode}, lr.Job...), "\x00")
	if strings.ContainsAny(all, "\x1b\x07\r\n") {
		t.Errorf("control characters survived ReadLastRun: %q / Job=%q", all, lr.Job)
	}
}

// covers: MA-76, R18, S29
// The BACKUP-NEEDS-ATTENTION.txt sidecar reassures a toolless reader: the
// messages already archived are unaffected and readable via index.html, the
// notice is about the most recent backup run only, and where to get the tool to
// resume backups — while still naming the status remedy for an operator.
func TestAttentionSidecarReassures(t *testing.T) {
	out := t.TempDir()
	if err := WriteLastRun(out, LastRun{Status: RunFailed, Started: time.Now(), Finished: time.Now(), Error: "the drive was not mounted"}); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(filepath.Join(out, AttentionName))
	if err != nil {
		t.Fatalf("failed run did not write %s: %v", AttentionName, err)
	}
	s := string(body)
	for _, want := range []string{"unaffected", "index.html", "most recent backup run", "self-contained executable", "mailarchive status"} {
		if !strings.Contains(s, want) {
			t.Errorf("%s lacks reassurance %q:\n%s", AttentionName, want, s)
		}
	}
}
