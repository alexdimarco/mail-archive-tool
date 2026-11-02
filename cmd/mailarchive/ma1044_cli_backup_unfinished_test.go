package main

import (
	"context"
	"encoding/json"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// ma1044Line appends one deletions.log line to the log at logPath.
func ma1044Line(t *testing.T, logPath string, line map[string]any) {
	t.Helper()
	b, err := json.Marshal(line)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr != nil || cerr != nil {
		t.Fatal(werr, cerr)
	}
}

// ma1044Started appends a delete's started line for folder, and no finished line.
func ma1044Started(t *testing.T, logPath, folder string) {
	t.Helper()
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "c1044-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
	if u, err := archdelete.UnfinishedDelete(logPath, folder, ""); err != nil || u == nil {
		t.Fatalf("precondition: the log says the delete of %s did not finish: %v %v", folder, u, err)
	}
}

// ma1044Files is every file under dir with its bytes, the lock file aside (a released lock's
// known leftover).
func ma1044Files(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	if err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if d.IsDir() || rel == ".mailarchive.lock" {
			return nil
		}
		b, rerr := os.ReadFile(p)
		out[rel] = string(b)
		return rerr
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func ma1044SameFiles(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}

// ma1044PackageLog is MailArchive's own deletions.log in this package's throwaway home (the one
// the real program and the in-process verbs read), put back as it was when the test ends.
func ma1044PackageLog(t *testing.T) string {
	t.Helper()
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	before, rerr := os.ReadFile(logPath)
	t.Cleanup(func() {
		os.RemoveAll(logPath)
		if rerr == nil {
			os.WriteFile(logPath, before, 0o600)
		}
	})
	return logPath
}

const ma1044Words = "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed."

// covers: MA-1044, R36
// A backup started at the terminal — `mailarchive -out DIR -input …`, the same with -unattended
// (what a schedule from `schedule -install` runs) and `mailarchive graph -out DIR …` — into a
// folder whose delete did not finish is refused at its start, before a recorded refusal, its -log
// or the sign-in (the 1.5.3 re-walk's guards pass, G2): exit 1, MA-1015's words for a backup, "(Folder:
// DIR)", and the terminal's Delete again; the folder is byte-identical. A deletions.log that cannot
// be read refuses (fail closed). A delete that finished does not stop a backup into a fresh folder
// made at its path. Run as the real program in this package's throwaway home, whose deletions.log
// it reads. Positive twin first: another folder, with that unfinished delete in the same log.
func TestMA1044_TerminalBackupRefusesAFolderWhoseDeleteDidNotFinish(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	half, other := filepath.Join(t.TempDir(), "Half deleted"), filepath.Join(t.TempDir(), "Other")
	for _, d := range []string{half, other} {
		if code, stderr := runCLI("-out", d, "-input", src); code != 0 {
			t.Fatalf("the archive %s: exit %d %s", d, code, stderr)
		}
	}
	// Earlier scheduled runs left their -log files in the folder: the refusals must not write to
	// them.
	if code, stderr := runCLI("-out", half, "-input", src, "-unattended", "-log", filepath.Join(half, "mailarchive-backup.log")); code != 0 {
		t.Fatalf("the earlier scheduled backup: exit %d %s", code, stderr)
	}
	if err := os.WriteFile(filepath.Join(half, "mailarchive-capture.log"), []byte("an earlier capture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ma1044Started(t, logPath, half)

	// Positive twin.
	if code, stderr := runCLI("-out", other, "-input", src); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, stderr)
	}

	before := ma1044Files(t, half)
	unchanged := func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }
	names := assure.Names(ma1044Words+" (Folder: "+half+")", "At this terminal, Delete again is: mailarchive delete-archive -out")

	code, stderr := runCLI("-out", half, "-input", src)
	assure.Refused(t, code, stderr, assure.Code(1), names, assure.NoSideEffect(unchanged))

	code, stderr = runCLI("-out", half, "-input", src, "-unattended", "-log", filepath.Join(half, "mailarchive-backup.log"))
	assure.Refused(t, code, stderr, assure.Code(1), names, assure.NoSideEffect(unchanged))

	code, stderr = runCLI("graph", "-auth", "device", "-out", half, "-tenant", "t", "-client-id", "c", "-unattended", "-log", filepath.Join(half, "mailarchive-capture.log"))
	assure.Refused(t, code, stderr, assure.Code(1), names, assure.NoSideEffect(unchanged))

	// A delete that finished: a backup into a fresh folder made at its path.
	fresh := filepath.Join(t.TempDir(), "Deleted then made again")
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T09:00:00Z", "event": "started", "run": "c1044fin", "folder": fresh, "row": fresh, "kind": "plain"})
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T09:00:01Z", "event": "folder removed", "run": "c1044fin", "folder": fresh})
	ma1044Line(t, logPath, map[string]any{"at": "2026-11-02T09:00:02Z", "event": "finished", "run": "c1044fin", "folder": fresh, "outcome": "complete"})
	if code, stderr := runCLI("-out", fresh, "-input", src); code != 0 {
		t.Fatalf("a finished delete must not stop a backup into a fresh folder at its path: exit %d %s", code, stderr)
	}
	assure.Reached(t, !absent(filepath.Join(fresh, ".mailarchive-manifest.json")), "the fresh folder's archive")

	// A deletions.log that cannot be read.
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	beforeOther := ma1044Files(t, other)
	code, stderr = runCLI("-out", other, "-input", src)
	assure.Refused(t, code, stderr, assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "Check that file, then try again. (Folder: "+other+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(beforeOther, ma1044Files(t, other)) }))
}

// covers: MA-1044, R36
// A scheduled terminal backup and capture whose delete begins after the run's start (its check
// passed, its -log opened in the folder) and before the engine's lock: the engine refuses it under
// its lock, the terminal says so in the same words with its Delete again, and the -log the run made
// in the folder is taken back (the folder byte-identical). Positive twins first: the same runs left
// alone.
func TestMA1044_TerminalBackupRefusesADeleteThatBeganBeforeItsLock(t *testing.T) {
	logPath := ma1044PackageLog(t)
	src := filepath.Join(t.TempDir(), "Inbox")
	writeMaildirMsg(t, src, "1.eml", sampleMsg)
	backup := func(out string) (int, string) {
		return refusalOf(runExport([]string{"-out", out, "-input", src, "-unattended", "-log", filepath.Join(out, "mailarchive-backup.log")}))
	}
	archive := func() string {
		out := t.TempDir()
		if code, msg := refusalOf(runExport([]string{"-out", out, "-input", src})); code != 0 {
			t.Fatalf("the archive: exit %d %s", code, msg)
		}
		return out
	}

	// Positive twin.
	twin := archive()
	if code, msg := backup(twin); code != 0 {
		t.Fatalf("positive twin: exit %d %s", code, msg)
	}

	raced := archive()
	before := ma1044Files(t, raced)
	old := exportEngine
	t.Cleanup(func() { exportEngine = old })
	reached := false
	exportEngine = func(ctx context.Context, o app.Options, l *log.Logger, p app.ProgressFunc) (app.Result, error) {
		reached = true
		ma1044Started(t, logPath, raced)
		return old(ctx, o, l, p)
	}
	code, msg := backup(raced)
	assure.Reached(t, reached, "the engine reached after the run's own start")
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(ma1044Words+" (Folder: "+raced+")", "At this terminal, Delete again is: mailarchive delete-archive -out"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, raced)) }))
	exportEngine = old

	// The capture: a delete begins during the sign-in.
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
		return graphInProcess("-unattended", "-auth", "device", "-out", out, "-tenant", "t", "-client-id", "c", "-token-cache", cache,
			"-log", filepath.Join(out, "mailarchive-capture.log"))
	}
	ctwin := t.TempDir()
	if code, msg := graphInProcess("-auth", "device", "-out", ctwin, "-tenant", "t", "-client-id", "c", "-token-cache", cache); code != 0 {
		t.Fatalf("the capture archive: exit %d %s", code, msg)
	}
	if code, msg := capture(ctwin); code != 0 {
		t.Fatalf("positive twin: the capture: exit %d %s", code, msg)
	}
	craced := t.TempDir()
	if code, msg := graphInProcess("-auth", "device", "-out", craced, "-tenant", "t", "-client-id", "c", "-token-cache", cache); code != 0 {
		t.Fatalf("the capture archive: exit %d %s", code, msg)
	}
	before = ma1044Files(t, craced)
	onMe = func() { ma1044Started(t, logPath, craced) }
	code, msg = capture(craced)
	onMe = nil
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(ma1044Words+" (Folder: "+craced+")", "At this terminal, Delete again is: mailarchive delete-archive -out"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, craced)) }))
}
