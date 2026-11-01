package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-956, R36
// `graph -unattended` is an unattended run in the engine too: its archive folder, there
// when the command checked it, removed while it signs in (the one long wait before the
// engine), is refused as not present and never recreated — the engine makes no folder for
// a scheduled capture. Positive twin first: the same capture with the folder left alone
// archives. A proxy in front of the fake Microsoft 365 removes the folder when the capture
// asks who signed in (/me).
func TestMA956_ScheduledCLICaptureNeverRecreatesItsFolder(t *testing.T) {
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
	capture := func(out string) (int, string) {
		return graphInProcess("-unattended", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-token-cache", cache)
	}

	twin := t.TempDir()
	if code, msg := capture(twin); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, msg)
	}
	assure.Reached(t, capturedPages(twin), "pages the twin captured")

	gone := filepath.Join(t.TempDir(), "archive")
	if err := os.Mkdir(gone, 0o700); err != nil {
		t.Fatal(err)
	}
	onMe = func() { _ = os.RemoveAll(gone) }
	code, msg := capture(gone)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(gone, "not present", "is the backup drive mounted"),
		assure.NoSideEffect(func() bool { return absent(gone) }))
}

// covers: MA-956, R36
// The command line's scheduled runs (`-unattended` export and `graph -unattended`) and the
// emptied-folder guard (design-archive-delete §7): a folder of the person's files that an
// archive was deleted from is refused before the run's -log is opened into it and before
// any sign-in — nothing written there, not one request to Microsoft 365. Positive twins
// first: a first scheduled run into an empty folder whose schedule keeps its -log there
// (no descriptor) works, for both verbs.
func TestMA956_ScheduledCLIRefusesEmptiedFolder(t *testing.T) {
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	names := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		var n []string
		for _, e := range entries {
			n = append(n, e.Name())
		}
		return n
	}
	mine := []byte("my own notes\n")
	keptFolder := func() string {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "notes.txt"), mine, 0o600); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	untouched := func(dir string) func() bool {
		return func() bool {
			got, _ := os.ReadFile(filepath.Join(dir, "notes.txt"))
			return slices.Equal(names(dir), []string{"notes.txt"}) && bytes.Equal(got, mine)
		}
	}

	// export -unattended.
	first := t.TempDir()
	if code, stderr := runCLI("-out", first, "-input", src, "-unattended", "-log", filepath.Join(first, "mailarchive-backup.log")); code != 0 {
		t.Fatalf("a first scheduled export beside its own -log: exit %d\n%s", code, stderr)
	}
	assure.Reached(t, !absent(filepath.Join(first, ".mailarchive-manifest.json")), "the first scheduled export's archive")
	kept := keptFolder()
	code, stderr := runCLI("-out", kept, "-input", src, "-unattended", "-log", filepath.Join(kept, "mailarchive-backup.log"))
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names(kept, "holds no MailArchive archive", "remove its scheduled backup"),
		assure.NoSideEffect(untouched(kept)))

	// graph -unattended.
	fake := newFakeM365(t)
	cache := filepath.Join(t.TempDir(), "graph.token")
	primeCache(t, cache)
	graph := func(out string) []string {
		return []string{"-unattended", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-token-cache", cache, "-log", filepath.Join(out, "mailarchive-capture.log")}
	}
	firstCapture := t.TempDir()
	if code, msg := graphInProcess(graph(firstCapture)...); code != 0 {
		t.Fatalf("a first scheduled capture beside its own -log: exit %d %s", code, msg)
	}
	assure.Reached(t, capturedPages(firstCapture), "pages the first scheduled capture wrote")
	keptCapture := keptFolder()
	before := fake.hits.Load()
	code, msg := graphInProcess(graph(keptCapture)...)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(keptCapture, "holds no MailArchive archive", "remove its scheduled backup"),
		assure.NoSideEffect(func() bool { return untouched(keptCapture)() && fake.hits.Load() == before }))
}
