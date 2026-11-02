package desktop

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/graphconfig"
)

// ma1044Started appends a delete's started line for folder, and no finished line.
func ma1044Started(t *testing.T, logPath, folder string) {
	t.Helper()
	b, err := json.Marshal(map[string]any{"at": "2026-11-02T10:00:00Z", "event": "started", "run": "d1044-" + filepath.Base(folder), "folder": folder, "row": folder, "kind": "plain"})
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

// covers: MA-1044, R36
// A scheduled Microsoft 365 capture (HeadlessCapture, through scheduledCapture as `desktop
// -capture` runs it) into a folder whose delete did not finish is refused at its start, before its
// log is opened in the folder, any start-failure record or the sign-in (the 1.5.3 re-walk's guards
// pass, G2): MA-1015's words for a backup with the folder, logged beside the Desktop's settings
// (desktop-capture.log) and recorded in the alert streak; the folder is byte-identical. It asks
// this Desktop's deletion log (its settings folder's, here a -config one). A delete that begins
// during the sign-in (after the start, before the lock) is refused by the engine under its lock,
// and the log the capture made in the folder is taken back. A log that cannot be read refuses.
// Positive twin first: another folder, with that unfinished delete in the same log, is captured.
func TestMA1044_ScheduledCaptureRefusesAFolderWhoseDeleteDidNotFinish(t *testing.T) {
	fake := newFakeM365(t)
	target, err := url.Parse(fake.URL)
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
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "graph-config.json")
	settingsPath := filepath.Join(dir, "desktop-settings.json")
	settingsLog := filepath.Join(dir, "desktop-capture.log")
	logPath := filepath.Join(dir, archdelete.LogName) // this Desktop's (-config) deletion log
	tok := filepath.Join(dir, "tok.json")
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c", Auth: "device"}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tok, []byte(`{"upn":"alice@contoso.org","token":{"access_token":"AT1","refresh_token":"RT1","expiry":"2999-01-01T00:00:00Z"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := func(out string) Config {
		return Config{ConfigPath: cfgPath, SettingsPath: settingsPath, TokenCachePath: tok, Out: out,
			BaseURL: srv.URL, TokenURL: srv.URL + "/token", DeviceAuthURL: srv.URL + "/devicecode"}
	}
	if got := cfg("").deletionsLogPath(); got != logPath {
		t.Fatalf("precondition: this Desktop's deletion log is %s, got %s", logPath, got)
	}
	read := func(p string) string { b, _ := os.ReadFile(p); return string(b) }
	rc := func(err error) int {
		if err != nil {
			return 1
		}
		return 0
	}
	words := "Delete not finished: click Delete again. This archive's delete did not finish, so it was not backed up; nothing was changed."
	archive := func() string {
		out := t.TempDir()
		if err := HeadlessCapture(cfg(out), io.Discard); err != nil {
			t.Fatal(err)
		}
		return out
	}

	half, other := archive(), archive()
	// An earlier scheduled capture left its log in the folder: the refusal must not write to it.
	if err := scheduledCapture(cfg(half), filepath.Join(half, "mailarchive-capture.log"), settingsLog, io.Discard); err != nil {
		t.Fatalf("the earlier scheduled capture: %v", err)
	}
	ma1044Started(t, logPath, half)

	// Positive twin.
	if err := scheduledCapture(cfg(other), filepath.Join(other, "mailarchive-capture.log"), settingsLog, io.Discard); err != nil {
		t.Fatalf("positive twin: %v", err)
	}

	// At its start: nothing written in the folder (not its log), no sign-in.
	before := ma1044Files(t, half)
	signedIn := false
	onMe = func() { signedIn = true }
	err = scheduledCapture(cfg(half), filepath.Join(half, "mailarchive-capture.log"), settingsLog, io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1),
		assure.Names(words+" (Folder: "+half+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) && !signedIn }))
	assure.Reached(t, strings.Contains(read(settingsLog), "FAILED: "+words), "the refusal logged beside the settings")

	// Microsoft 365 not set up on this Desktop: the delete's refusal comes first, so no
	// start-failure record is written into the half-deleted folder.
	notSetUp := cfg(half)
	notSetUp.ConfigPath = filepath.Join(dir, "not-set-up.json") // the same settings folder, so the same deletion log
	err = HeadlessCapture(notSetUp, io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1),
		assure.Names(words+" (Folder: "+half+")"), assure.Forbid("not configured"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, half)) }))
	streak := backupalert.StreakPath(settingsPath, backupalert.KeyFor(half))
	var rec struct {
		Noticed bool `json:"noticed"`
	}
	_ = json.Unmarshal([]byte(read(streak)), &rec)
	assure.Reached(t, rec.Noticed, "the refusal recorded in the alert streak (its failure notice raised)")

	// A delete begins during the sign-in: refused under the lock, the capture's log taken back.
	raced := archive()
	before = ma1044Files(t, raced)
	onMe = func() { ma1044Started(t, logPath, raced) }
	err = scheduledCapture(cfg(raced), filepath.Join(raced, "mailarchive-capture.log"), settingsLog, io.Discard)
	onMe = nil
	assure.Refused(t, rc(err), errText(err), assure.Code(1),
		assure.Names(words+" (Folder: "+raced+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, raced)) }))
	assure.Reached(t, strings.Count(read(settingsLog), "FAILED: "+words) == 2, "the refusal under the lock logged beside the settings")

	// A deletion log that cannot be read.
	if err := os.Remove(logPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(logPath, 0o700); err != nil { // a folder where the file goes
		t.Fatal(err)
	}
	before = ma1044Files(t, other)
	err = HeadlessCapture(cfg(other), io.Discard)
	assure.Refused(t, rc(err), errText(err), assure.Code(1),
		assure.Names("MailArchive could not read its deletion log, "+logPath, "Check that file, then try again. (Folder: "+other+")"),
		assure.NoSideEffect(func() bool { return ma1044SameFiles(before, ma1044Files(t, other)) }))
}
