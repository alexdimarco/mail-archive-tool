package thunderbird

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// covers: MA-35, R9
// The prefs-edit gate's sensor detects running by PID LIVENESS, not file
// existence: a live-pid lock is running; a stale (dead-pid) lock and the
// always-persistent .parentlock are NOT — otherwise the GUI loops forever on
// "please quit Thunderbird".
func TestRunningDetectsLock(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses the Unix Mozilla lock symlink")
	}
	prof := t.TempDir()
	lock := filepath.Join(prof, "lock")

	if Running(prof) {
		t.Fatal("a clean profile (no lock) must not look running")
	}

	// Live lock → running.
	if err := os.Symlink(fmt.Sprintf("127.0.1.1:+%d", os.Getpid()), lock); err != nil {
		t.Fatal(err)
	}
	if !Running(prof) {
		t.Error("a lock pointing at a live pid must be detected as running")
	}

	// Stale lock (dead pid) → NOT running. This is the reported bug: file
	// existence alone must not count, or the quit-Thunderbird dialog cycles.
	cmd := exec.Command("sleep", "0")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	deadPID := cmd.Process.Pid
	_ = os.Remove(lock)
	if err := os.Symlink(fmt.Sprintf("127.0.1.1:+%d", deadPID), lock); err != nil {
		t.Fatal(err)
	}
	if Running(prof) {
		t.Errorf("a stale lock (dead pid %d) must NOT be treated as running", deadPID)
	}

	// A persistent .parentlock alone (Mozilla never removes it) is not running.
	_ = os.Remove(lock)
	if err := os.WriteFile(filepath.Join(prof, ".parentlock"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Running(prof) {
		t.Error(".parentlock existing must not be treated as running (it always persists)")
	}
}

// covers: MA-26
func TestIsImapStore(t *testing.T) {
	if !IsImapStore("/home/u/.thunderbird/x.default/ImapMail/mail.example.com") {
		t.Error("ImapMail path should be an IMAP store")
	}
	if IsImapStore("/home/u/.thunderbird/x.default/Mail/Local Folders") {
		t.Error("Local Folders should not be an IMAP store")
	}
}

// covers: MA-27
func TestFindAccountAndEnableOffline(t *testing.T) {
	profile := t.TempDir()
	store := filepath.Join(profile, "ImapMail", "mail.example.com")
	if err := os.MkdirAll(store, 0o755); err != nil {
		t.Fatal(err)
	}
	prefs := `user_pref("mail.server.server3.type", "imap");
user_pref("mail.server.server3.hostname", "mail.example.com");
user_pref("mail.server.server3.offline_download", false);
`
	if err := os.WriteFile(filepath.Join(profile, "prefs.js"), []byte(prefs), 0o644); err != nil {
		t.Fatal(err)
	}

	got, ok := FindProfileDir(store)
	if !ok || got != profile {
		t.Fatalf("FindProfileDir = %q, %v; want %q", got, ok, profile)
	}

	acct, err := FindAccountForStore(profile, store)
	if err != nil {
		t.Fatal(err)
	}
	if acct.ServerKey != "server3" || acct.Type != "imap" || acct.Hostname != "mail.example.com" {
		t.Fatalf("account = %+v", acct)
	}
	if acct.OfflineDownload {
		t.Error("offline download should start false")
	}

	changed, backup, err := EnableOffline(profile, "server3")
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("expected a change")
	}
	if _, err := os.Stat(backup); err != nil {
		t.Errorf("backup not written: %v", err)
	}
	acct2, _ := FindAccountForStore(profile, store)
	if !acct2.OfflineDownload {
		t.Error("offline download should now be true")
	}

	// Idempotent: enabling again is a no-op.
	changed2, _, err := EnableOffline(profile, "server3")
	if err != nil {
		t.Fatal(err)
	}
	if changed2 {
		t.Error("second EnableOffline should report no change")
	}
}

// covers: MA-28
func TestStableWaiter(t *testing.T) {
	dir := t.TempDir()
	inbox := filepath.Join(dir, "Inbox")
	if err := os.WriteFile(inbox, []byte("From x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	now := time.Unix(1000, 0)
	w := NewStableWaiter(dir, 30*time.Second, now)

	if _, stable := w.Poll(now.Add(10 * time.Second)); stable {
		t.Error("should not be stable after only 10s")
	}
	if _, stable := w.Poll(now.Add(31 * time.Second)); !stable {
		t.Error("should be stable after 31s with no growth")
	}

	// Growing the store resets the stability window.
	if err := os.WriteFile(inbox, []byte("From x\nmore and more data here"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stable := w.Poll(now.Add(35 * time.Second)); stable {
		t.Error("growth should reset stability")
	}
}
