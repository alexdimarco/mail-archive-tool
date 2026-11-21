package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/autoplace"
)

// covers: MA-1479, MA-692, R18
// After Repair the words say what happens next (the 1.7.5 friction review's F2 and F10): with the
// archive's scheduled entry back, its banner says "Its scheduled backup is set, and MailArchive now
// skips a mailbox that is gone, so the next backup backs up the others; to back up now, click Run now
// on its Scheduled backup card.", no longer "click Repair"; Repair's reply for such an archive says
// "Repaired. The next backup skips the missing mailbox; to back up now, click Run now."; and status's
// RED line for the refused run drops the drive advice where the helper says yes. Positive twins
// first: auto-paused (no entry), the banner says Repair and names the mailbox; an archive the helper
// says no to keeps the drive advice and has no Repair words.
func TestAfterRepairTheWordsSayWhatHappensNext(t *testing.T) {
	a, b, _ := goneHome(t)
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	out := pausedStopped(t, autoplace.GoneText(b), []string{"-auto", "-input", a, "-input", b}, []string{a, b})
	page, _ := drawGoneWith(t, out, 30*time.Second, false)
	if !strings.Contains(page, fmt.Sprintf(repairBanner, b)) || !strings.Contains(page, `id="schedRepair" data-gone-repair`) {
		t.Fatalf("positive twin: auto-paused, the banner says Repair naming the mailbox, and Repair is marked")
	}
	set := fmt.Sprintf("This archive's source hasn't been reachable since Oct 12, 2026 (the missing mailbox: %s). Its scheduled backup is set, and MailArchive now skips a mailbox that is gone, so the next backup backs up the others; to back up now, click Run now on its Scheduled backup card.", b)
	page, _ = drawGoneWith(t, out, 30*time.Second, true)
	if !strings.Contains(page, set) || strings.Contains(page, "If a mail account was removed, click Repair") {
		t.Errorf("F2: with the entry back, the banner says the next backup skips it")
	}
	if !strings.Contains(dashboardJS, "srp.hasAttribute('data-gone-repair')?'Repaired. The next backup skips the missing mailbox; to back up now, click Run now.':'Repaired.'") {
		t.Error("F2: Repair's reply for such an archive says what happens next")
	}
	drive := " does not exist (check the path, or that the drive holding it is mounted)"
	if got := statusOf(out); !strings.Contains(got, "FAILED: input "+b+" does not exist\n") || strings.Contains(got, b+drive) {
		t.Errorf("F10: the RED line drops the drive advice where Repair works:\n%s", got)
	}
	usb := filepath.Join(t.TempDir(), "USB", "Inbox")
	other := pausedStopped(t, autoplace.GoneText(usb), []string{"-auto", "-input", a, "-input", usb}, []string{a, usb})
	if got := statusOf(other); !strings.Contains(got, usb+drive) || strings.Contains(got, "missing mailbox") {
		t.Errorf("an archive the helper says no to keeps the drive advice:\n%s", got)
	}
}
