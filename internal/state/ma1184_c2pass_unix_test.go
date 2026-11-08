//go:build unix

package state

// The C2 adversarial pass's ADV-C2-09 on Unix (links and FIFOs as Unix makes them; the Windows
// refusals are util.ReadSecureFile's own tests), brought in from the pass's clone.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// covers: MA-1184
// ADV-C2-09 (held, brought in): planted streak records — a link, a FIFO, a folder, a huge file,
// garbage, a negative count — never hang, never follow, and never read as a streak with runs.
// Positive twin: a real record reads back, written 0600.
func TestADV_C2_09_PlantedStreakShapes(t *testing.T) {
	out := t.TempDir()
	good := TBStreak{}.Behind("k", "imap.x.example", 3, time.Now()).Named()
	if err := WriteTBStreak(out, good); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadTBStreak(out); err != nil || len(s.Accounts) != 1 {
		t.Fatalf("positive twin: %+v %v", s, err)
	}
	if fi, _ := os.Stat(filepath.Join(out, TBStreakName)); fi.Mode().Perm() != 0o600 {
		t.Errorf("written %v, want 0600", fi.Mode().Perm())
	}
	p := func(dir string) string { return filepath.Join(dir, TBStreakName) }
	link := t.TempDir()
	if err := os.Symlink(p(out), p(link)); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadTBStreak(link); err == nil || len(s.Accounts) != 0 {
		t.Errorf("a link was followed: %+v %v", s, err)
	}
	fifo := t.TempDir()
	if err := syscall.Mkfifo(p(fifo), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadTBStreak(fifo); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a FIFO read as a record")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadTBStreak hung on a FIFO")
	}
	dir := t.TempDir()
	if err := os.Mkdir(p(dir), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadTBStreak(dir); err == nil {
		t.Error("a folder read as a record")
	}
	huge := t.TempDir()
	if err := os.WriteFile(p(huge), []byte(`{"version":1,"accounts":[`+strings.Repeat(`{"store":"k","account":"a","runs":9},`, 5000)+`{}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// The streak's own cap (TBStreakAccountsMax, 256 since the C2 pass round 3's R3-04).
	if s, err := ReadTBStreak(huge); err == nil && len(s.Accounts) > TBStreakAccountsMax {
		t.Errorf("a huge record read %d accounts", len(s.Accounts))
	}
	// Past the reader's byte bound, a record is refused, never read.
	if err := os.WriteFile(p(huge), []byte(`{"version":1,"accounts":[`+strings.Repeat(`{"store":"k","account":"a","runs":9},`, 20000)+`{}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadTBStreak(huge); err == nil || len(s.Accounts) != 0 {
		t.Errorf("a record past the byte bound read: %d accounts, %v", len(s.Accounts), err)
	}
	garbage := t.TempDir()
	if err := os.WriteFile(p(garbage), []byte("\x00\xff{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if s, err := ReadTBStreak(garbage); err != nil || len(s.Accounts) != 0 {
		t.Errorf("garbage: %+v %v", s, err)
	}
	neg := t.TempDir()
	if err := os.WriteFile(p(neg), []byte(`{"version":1,"serial":-5,"record_id":"../../x","accounts":[{"store":"k","account":"a‮\u001b[31m","runs":-3,"first_x":-1,"serial":-9}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := ReadTBStreak(neg)
	if err != nil || len(s.Accounts) != 1 {
		t.Fatalf("negative record: %+v %v", s, err)
	}
	if a := s.Accounts[0]; a.Runs < 0 || a.FirstX < 0 || a.Account != "a[31m" || s.RecordID != "" {
		t.Errorf("negative record read as %+v (record id %q)", a, s.RecordID)
	}
}
