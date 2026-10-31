//go:build unix

package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
)

// covers: MA-931
// A pipe planted at the run history's name never hangs the Desktop's page (gate-2
// adversarial pass 1, as7-1): the overview reads the history on every render.
// Positive twin first: a healthy history's run shows on the Recent backups card. Then,
// with a pipe at that name, the page renders at once with no rows.
func TestAPlantedRunHistoryPipeNeverHangsThePage(t *testing.T) {
	out := t.TempDir()
	writeManifest(t, out)
	if err := state.AppendRunHistory(out, state.RunHistoryEntry{When: time.Date(2026, 10, 31, 9, 0, 0, 0, time.UTC), Status: state.RunOK, Exported: 7}); err != nil {
		t.Fatal(err)
	}
	cfg := Config{SettingsPath: filepath.Join(t.TempDir(), "s.json"), ConfigPath: filepath.Join(t.TempDir(), "none.json"), Out: out}
	if rows := assure.Reached(t, recentRunRows(out), "the positive twin's rows"); rows[0].Detail != "7 messages" ||
		!strings.Contains(cfg.overview("csrf-token"), "7 messages") {
		t.Fatalf("the positive twin: rows %+v, or the page does not show the run", rows)
	}

	pipe := filepath.Join(out, state.RunHistoryName)
	if err := os.Remove(pipe); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(pipe, 0o600); err != nil {
		t.Skip("no pipes here:", err)
	}
	t.Cleanup(func() { // unblock a reader stuck on the pipe, so no goroutine is left
		if f, err := os.OpenFile(pipe, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
			f.Close()
		}
	})
	done := make(chan string, 1)
	go func() { done <- cfg.overview("csrf-token") }()
	select {
	case page := <-done:
		if !strings.Contains(page, "Recent backups") || strings.Contains(page, "7 messages") {
			t.Fatal("the page rendered with the pipe, but not as an empty Recent backups card")
		}
		if rows := recentRunRows(out); rows != nil {
			t.Fatalf("a pipe gave rows %+v", rows)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the page waited on the pipe planted at the run history's name")
	}
}
