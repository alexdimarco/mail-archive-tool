package main

import (
	"context"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/outlookcom"
)

// The command line's fixes from slice 2's adversarial pass (design-archive-delete §7;
// findings del-s2-A1-01, A2-01, A1-02, A2-02, A2-03).

func ma956CLINames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var n []string
	for _, e := range entries {
		if e.Name() != ".mailarchive.lock" { // the run's own lock file: the known leftover (lockfile Release)
			n = append(n, e.Name())
		}
	}
	slices.Sort(n)
	return n
}

// covers: MA-956, R36
// `-unattended -outlook` (a scheduled command-line Outlook backup): the PST export never
// makes the archive folder itself; a run that found its archive at its entry check and
// reaches the engine after a delete that could not remove what it holds open (Windows: its
// -log, the PSTs) is refused under its lock — not rebuilt — and takes back what it wrote:
// the PSTs, the PST folder, and the -log it made there, with no FAILED line written into
// the folder. Outlook is stood in for (this repo's tests cannot drive it). Positive twin
// first: the same backup into an empty folder archives and reclaims its PSTs.
func TestMA956_ScheduledCLIOutlookRefusedAfterADeleteTakesBackItsWrites(t *testing.T) {
	pst, err := os.ReadFile(filepath.Join("..", "..", "testdata", "support.pst"))
	if err != nil {
		t.Fatal(err)
	}
	var pstOpts []outlookcom.Options
	oldPST, oldAvail, oldEngine := cliCreatePSTs, outlookAppAvailable, exportEngine
	t.Cleanup(func() { cliCreatePSTs, outlookAppAvailable, exportEngine = oldPST, oldAvail, oldEngine })
	outlookAppAvailable = func() bool { return true }
	cliCreatePSTs = func(dir string, o outlookcom.Options, _ *log.Logger) ([]outlookcom.Store, []outlookcom.Skip, error) {
		pstOpts = append(pstOpts, o)
		mk := os.MkdirAll
		if o.ParentMustExist {
			mk = func(p string, m os.FileMode) error { return os.Mkdir(p, m) }
		}
		if err := mk(dir, 0o755); err != nil && !os.IsExist(err) {
			return nil, nil, err
		}
		p := filepath.Join(dir, "Mailbox.pst")
		if err := os.WriteFile(p, pst, 0o600); err != nil {
			return nil, nil, err
		}
		return []outlookcom.Store{{Name: "Mailbox", Path: p}}, nil, nil
	}
	backup := func(out string) (int, string) {
		return refusalOf(runExport([]string{"-out", out, "-unattended", "-outlook", "-outlook-sync-wait", "0s", "-log", filepath.Join(out, "mailarchive-backup.log")}))
	}

	first := t.TempDir()
	if code, msg := backup(first); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, msg)
	}
	assure.Reached(t, !absent(filepath.Join(first, ".mailarchive-manifest.json")), "the twin's archive")
	assure.Reached(t, len(pstOpts) == 1 && pstOpts[0].ParentMustExist, "the scheduled export makes only its own folder")

	// The archive, made at a terminal with no -log; the delete finishes as the run reaches the
	// engine and leaves only what the run holds open.
	kept := t.TempDir()
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	if code, msg := refusalOf(runExport([]string{"-out", kept, "-input", src})); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, msg)
	}
	reached := false
	exportEngine = func(ctx context.Context, o app.Options, l *log.Logger, p app.ProgressFunc) (app.Result, error) {
		reached = true
		for _, n := range ma956CLINames(t, kept) {
			if n != "mailarchive-backup.log" && n != "_outlook-pst" {
				if err := os.RemoveAll(filepath.Join(kept, n)); err != nil {
					t.Fatal(err)
				}
			}
		}
		return oldEngine(ctx, o, l, p)
	}
	code, msg := backup(kept)
	assure.Reached(t, reached, "the engine reached after the PST export")
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(kept, "held a MailArchive archive when this backup started", "remove its scheduled backup"),
		assure.NoSideEffect(func() bool { return len(ma956CLINames(t, kept)) == 0 }))
}

// covers: MA-956, R36
// `graph -unattended` with its -log in the archive folder: a delete during the sign-in that
// leaves only the -log the capture holds open is refused under the lock (RunGraph saw the
// archive before it signed in), and the -log this capture made is taken back, with no
// FAILED line written into the folder (del-s2-A2-03). Positive twin first: a capture left
// alone keeps its -log.
func TestMA956_ScheduledCLICaptureTakesBackItsLog(t *testing.T) {
	newFakeM365(t)
	target, err := url.Parse(graphFake.base)
	if err != nil {
		t.Fatal(err)
	}
	var onMe func()
	proxy := httputil.NewSingleHostReverseProxy(target)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/me" && onMe != nil {
			onMe()
		}
		proxy.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	graphFake.base, graphFake.token, graphFake.deviceAuth = srv.URL, srv.URL+"/token", srv.URL+"/devicecode"
	cache := filepath.Join(t.TempDir(), "graph.token")
	primeCache(t, cache)
	capture := func(out string, withLog bool) (int, string) {
		args := []string{"-unattended", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-token-cache", cache}
		if withLog {
			args = append(args, "-log", filepath.Join(out, "mailarchive-capture.log"))
		}
		return graphInProcess(args...)
	}

	twin := t.TempDir()
	if code, msg := capture(twin, true); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, msg)
	}
	assure.Reached(t, !absent(filepath.Join(twin, "mailarchive-capture.log")), "the twin's -log")

	kept := t.TempDir()
	if code, msg := capture(kept, false); code != 0 {
		t.Fatalf("the archive: exit %d %s", code, msg)
	}
	onMe = func() {
		for _, n := range ma956CLINames(t, kept) {
			if n != "mailarchive-capture.log" {
				_ = os.RemoveAll(filepath.Join(kept, n))
			}
		}
	}
	code, msg := capture(kept, true)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(kept, "held a MailArchive archive when this backup started"),
		assure.NoSideEffect(func() bool { return len(ma956CLINames(t, kept)) == 0 }))
}
