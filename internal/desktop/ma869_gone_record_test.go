package desktop

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/autoplace"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The 1.7.5 adversarial pass's OUT-01 (with INS-01): the Repair words of MA-1479 come from a look
// at the mail paths a stopped archive's record names. The record is untrusted (C-17), so a planted
// network path is never looked at (MA-869), and drawing the page never waits on that look.

// pausedStopped is stoppedArchive (its record's job jobArgs) with its source streak past the 30
// days auto-pause takes, so the Settings card says "Backups stopped", and the person's own job file
// in the settings folder listing the local inputs mine (the record alone is planted).
func pausedStopped(t *testing.T, errText string, jobArgs, mine []string) string {
	t.Helper()
	out := stoppedArchive(t, errText, jobArgs, "local-source-unreachable", "daily", false)
	t0 := time.Date(2026, 10, 12, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{ConsecutiveFails: 31, FirstUnreachable: t0, LastUnreachable: t0.Add(31 * 24 * time.Hour), LastKind: "local-source-unreachable"}); err != nil {
		t.Fatal(err)
	}
	name := schedule.DefaultNameFor(out)
	jp, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Write(jp, job.Job{Version: 1, Name: name, Inputs: mine, Auto: true, Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(jp) })
	return out
}

// drawGone draws the page (the archive current, listed in Your archives, its scheduler entry gone)
// and the Status & health view, each within limit, and returns them; a draw that does not finish
// within limit fails the test.
func drawGone(t *testing.T, out string, limit time.Duration) (page, healthView string) {
	t.Helper()
	return drawGoneWith(t, out, limit, false)
}

// drawGoneWith is drawGone with the archive's scheduler entry there when installed (a Repair done).
func drawGoneWith(t *testing.T, out string, limit time.Duration, installed bool) (page, healthView string) {
	t.Helper()
	idxPath := filepath.Join(t.TempDir(), archindex.Name)
	if err := archindex.WriteFile(idxPath, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{{Path: out}}}); err != nil {
		t.Fatal(err)
	}
	settings := filepath.Join(t.TempDir(), "s.json")
	if err := SaveSettings(settings, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	fake := &backupFake{tasks: map[string]bool{out: installed}}
	if !installed {
		fake.tasks = map[string]bool{}
	}
	h := DashboardHandler(Config{Out: out, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: settings, IndexPath: idxPath,
		BackupInstalledFor: fake.installedFor, RemoveBackupFor: fake.removeFor, EntryState: fake.entryState})
	get := func(path string) (string, bool) {
		done := make(chan string, 1)
		go func() {
			req := httptest.NewRequest("GET", path, nil)
			req.Host = "127.0.0.1:8097"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			done <- rec.Body.String()
		}()
		select {
		case s := <-done:
			return s, true
		case <-time.After(limit):
			return "", false
		}
	}
	var ok bool
	if page, ok = get("/"); !ok {
		t.Fatalf("drawing the page waited past %s", limit)
	}
	if healthView, ok = get("/api/health"); !ok {
		t.Fatalf("drawing the Status & health view waited past %s", limit)
	}
	return page, healthView
}

// pathWatch records every path autoplace looks at on disk (autoplace.WatchLooksForTest).
type pathWatch struct {
	mu    sync.Mutex
	paths []string
}

func watchGoneLooks(t *testing.T) *pathWatch {
	t.Helper()
	w := &pathWatch{}
	t.Cleanup(autoplace.WatchLooksForTest(func(p string) { w.mu.Lock(); w.paths = append(w.paths, p); w.mu.Unlock() }))
	return w
}

func (w *pathWatch) at(part string) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.paths {
		if strings.Contains(p, part) {
			return p
		}
	}
	return ""
}

// covers: MA-869, MA-1479, R37
// A network (UNC) path planted in a stopped archive's record, as the refused input or as another
// listed input, is never looked at by drawing the page (the banner and the Settings card) or the
// Status & health view, and all three keep today's words (C-17; the 1.7.5 adversarial pass's
// OUT-01 and INS-01). Positive twin first: with every listed input local, the inputs are looked at,
// and the banner, the Settings card and the health view say Repair (MA-1479's local case).
func TestAPlantedNetworkPathInAStoppedRecordIsNeverLookedAt(t *testing.T) {
	a, b, _ := goneHome(t)
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	w := watchGoneLooks(t)
	mine := []string{a, b}
	page, hv := drawGone(t, pausedStopped(t, autoplace.GoneText(b), []string{"-auto", "-input", a, "-input", b}, mine), 30*time.Second)
	if w.at(a) == "" || !strings.Contains(page, "If a mail account was removed, click Repair (or Run now) on its Scheduled backup card") ||
		!strings.Contains(cardOf(page, "sec-settings"), "If a mail account was removed, click Repair") || !strings.Contains(hv, "if a mail account was removed") {
		t.Fatalf("positive twin: the local inputs are looked at (%v) and the banner (%v), the Settings card and the health view say Repair:\n%s\n%s", w.at(a) != "",
			strings.Contains(page, "If a mail account was removed, click Repair (or Run now) on its Scheduled backup card"), cardOf(page, "sec-settings"), hv)
	}

	for _, host := range []string{`\\evil-host\share\Mail`, "//evil-host/share/Mail"} {
		for name, c := range map[string]struct {
			refused string
			inputs  []string
		}{
			"as the refused input": {host, []string{"-auto", "-input", a, "-input", host}},
			"as another input":     {b, []string{"-auto", "-input", a, "-input", b, "-input", host}},
		} {
			w := watchGoneLooks(t)
			out := pausedStopped(t, autoplace.GoneText(c.refused), c.inputs, mine)
			page, hv := drawGone(t, out, 30*time.Second)
			if p := w.at("evil-host"); p != "" {
				t.Errorf("%s %s: the planted path was looked at: %s", host, name, p)
			}
			if !strings.Contains(page, todaysBanner) || strings.Contains(page, "If a mail account was removed") {
				t.Errorf("%s %s: the banner must keep today's words", host, name)
			}
			if s := cardOf(page, "sec-settings"); !strings.Contains(s, "Backups stopped") || strings.Contains(s, "If a mail account was removed") {
				t.Errorf("%s %s: the Settings card must keep today's line alone:\n%s", host, name, s)
			}
			if strings.Contains(hv, "if a mail account was removed") || !strings.Contains(hv, "reconnect the source") {
				t.Errorf("%s %s: the health view must keep today's words:\n%s", host, name, hv)
			}
		}
	}
}

// covers: MA-869, MA-1479, R37
// Drawing the page never waits on a gone mailbox's storage (C-17; the 1.7.5 adversarial pass's
// OUT-01): the banner's, the Settings card's and the health view's Repair answer is asked within
// sourceWait, and when the looks stall, the page and the health view are drawn within the bound
// with today's words. Positive twin first: with the storage answering, they say Repair.
func TestDrawingThePageNeverWaitsOnAGoneMailboxsStorage(t *testing.T) {
	a, b, _ := goneHome(t)
	if err := os.RemoveAll(b); err != nil {
		t.Fatal(err)
	}
	out := pausedStopped(t, autoplace.GoneText(b), []string{"-auto", "-input", a, "-input", b}, []string{a, b})
	if page, hv := drawGone(t, out, 30*time.Second); !strings.Contains(page, "If a mail account was removed") || !strings.Contains(hv, "if a mail account was removed") {
		t.Fatal("positive twin: with the storage answering, the page and the health view say Repair")
	}

	// Every look waits until the test ends: storage that stalls.
	release := make(chan struct{})
	restore := autoplace.WatchLooksForTest(func(string) { <-release })
	origW := sourceWait
	sourceWait = 50 * time.Millisecond
	t.Cleanup(func() {
		restore()
		close(release)
		sourceWait = origW
	})
	page, hv := drawGone(t, out, 5*time.Second)
	if strings.Contains(page, "If a mail account was removed") || !strings.Contains(page, todaysBanner) {
		t.Error("with the looks stalled, the banner must keep today's words")
	}
	if s := cardOf(page, "sec-settings"); !strings.Contains(s, "Backups stopped") || strings.Contains(s, "If a mail account was removed") {
		t.Errorf("with the looks stalled, the Settings card keeps today's line alone:\n%s", s)
	}
	if strings.Contains(hv, "if a mail account was removed") || !strings.Contains(hv, "reconnect the source") {
		t.Errorf("with the looks stalled, the health view keeps today's words:\n%s", hv)
	}
}
