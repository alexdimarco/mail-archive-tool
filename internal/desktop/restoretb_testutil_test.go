package desktop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbengine"
	"mail-archive-tool/internal/tbrestore"
)

// Restore to Thunderbird's Desktop tests (slice R7): a real archive backed up from real mbox
// files (originals kept), fake Thunderbird profiles in temp folders only — profiles.ini read
// only from the test's own folder through restoreTBBases, never ~/Library/Thunderbird or the
// VM user's — and the real engine. The stand-ins are the OS probes (Thunderbird's process
// list, elevation, a Microsoft Store Thunderbird, a network drive), as the engine's own tests
// have them, and, where a test must act while a restore runs, a hold inside the engine's own
// progress callback (fault injection).

// rtbProfile is one fake Thunderbird profile: its folder and its Local Folders, which holds
// Thunderbird's own Trash (a restore never touches it).
type rtbProfile struct{ dir, local string }

// rtbEnv is a Desktop whose current archive is out, with its own lifecycle, registry of keys and
// secret store, and Thunderbird profiles in base.
type rtbEnv struct {
	t        *testing.T
	d        *dashboard
	h        http.Handler
	cfg      Config
	out      string
	base     string
	profiles []rtbProfile
	stop     context.CancelFunc
}

// rtbFolders are the source's folders: Zebrafolder (two messages) with Quokkasub below it (one),
// and Sentbox (one). Their names are found nowhere else, so a scan for one finds only them.
const (
	rtbFolderA = "Zebrafolder"
	rtbFolderB = "Quokkasub"
	rtbFolderC = "Sentbox"
)

func rtbMbox(subjects ...string) string {
	var b strings.Builder
	for i, s := range subjects {
		day := string(rune('1' + i))
		b.WriteString("From - Mon Jan 0" + day + " 00:00:00 2024\r\n")
		b.WriteString("From: Ann <ann@example.org>\r\nTo: bob@example.org\r\nSubject: " + s + "\r\n")
		b.WriteString("Date: Mon, 0" + day + " Jan 2024 10:00:00 +0000\r\nMessage-ID: <" + strings.ReplaceAll(s, " ", ".") + "@example.org>\r\n\r\n")
		b.WriteString("Body of " + s + "\r\n\r\n")
	}
	return b.String()
}

func rtbWrite(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// rtbPlainArchive backs a Thunderbird-style folder of mbox files under src up into a new plain
// archive, its originals kept.
func rtbPlainArchive(t *testing.T, src string) string {
	t.Helper()
	rtbWrite(t, filepath.Join(src, rtbFolderA), rtbMbox("alpha one", "alpha two"))
	rtbWrite(t, filepath.Join(src, rtbFolderA+".sbd", rtbFolderB), rtbMbox("beta one"))
	rtbWrite(t, filepath.Join(src, rtbFolderC), rtbMbox("gamma one"))
	out := filepath.Join(t.TempDir(), "Work mail")
	opts := app.Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true, KeepRaw: true}
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatalf("backup: %v", err)
	}
	return out
}

// rtbMakeProfiles writes profiles.ini in base listing n fake profiles (named work, home, …),
// each with prefs.js naming its Local Folders, made with Thunderbird's own Trash in it.
func rtbMakeProfiles(t *testing.T, base string, n int) []rtbProfile {
	t.Helper()
	names := []string{"work", "home", "spare"}
	var ini strings.Builder
	var ps []rtbProfile
	for i := 0; i < n; i++ {
		rel := names[i] + ".default"
		ini.WriteString("[Profile" + string(rune('0'+i)) + "]\nName=" + names[i] + "\nIsRelative=1\nPath=" + rel + "\n")
		if i == 0 {
			ini.WriteString("Default=1\n")
		}
		ini.WriteString("\n")
		dir := filepath.Join(base, rel)
		rtbWrite(t, filepath.Join(dir, "prefs.js"), "// Mozilla User Preferences\n"+strings.Join([]string{
			`user_pref("mail.accountmanager.localfoldersserver", "server1");`,
			`user_pref("mail.server.server1.directory-rel", "[ProfD]Mail/Local Folders");`,
			`user_pref("mail.server.server1.hostname", "Local Folders");`,
			`user_pref("mail.server.server1.type", "none");`,
			`user_pref("mail.server.server1.storeContractID", "@mozilla.org/msgstore/berkeleystore;1");`,
		}, "\n")+"\n")
		if err := os.Chmod(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		local := filepath.Join(dir, "Mail", "Local Folders")
		rtbWrite(t, filepath.Join(local, "Trash"), "From - x\nSubject: old\n\nkept\n")
		rd, _ := filepath.EvalSymlinks(dir)
		rl, _ := filepath.EvalSymlinks(local)
		ps = append(ps, rtbProfile{dir: rd, local: rl})
	}
	rtbWrite(t, filepath.Join(base, tbrestore.ProfilesININame), ini.String())
	return ps
}

// rtbSeams stands in for the OS probes and points profiles.ini at base, for the test's length.
func rtbSeams(t *testing.T, base string) {
	t.Helper()
	t.Cleanup(tbengine.SetOSProbesForTest())
	t.Cleanup(tbrestore.SetProcessProbeForTest(func() (int, bool) { return 0, true }))
	was := restoreTBBases
	restoreTBBases = func() []string { return []string{base} }
	t.Cleanup(func() { restoreTBBases = was })
	every := restoreTBEvery
	restoreTBEvery = 0 // every message's progress is said, so a test reads each
	t.Cleanup(func() { restoreTBEvery = every })
}

// newRtbEnv is a Desktop whose current archive is out (listed under Your archives) with n
// profiles in base (a new temp folder when base is "").
func newRtbEnv(t *testing.T, out, base string, n int) *rtbEnv {
	t.Helper()
	if base == "" {
		base = filepath.Join(t.TempDir(), "Thunderbird")
	}
	rtbSeams(t, base)
	e := &rtbEnv{t: t, out: out, base: base, profiles: rtbMakeProfiles(t, base, n)}
	dir := t.TempDir()
	settings := filepath.Join(dir, "desktop-settings.json")
	if err := SaveSettings(settings, Settings{Out: out}); err != nil {
		t.Fatal(err)
	}
	index := filepath.Join(dir, archindex.Name)
	if err := archindex.WriteFile(index, archindex.Index{Entries: []archindex.Entry{{Path: out}}}); err != nil {
		t.Fatal(err)
	}
	store := graphconfig.NewFileSecretStore(t.TempDir())
	ctx, cancel := context.WithCancel(context.Background())
	e.stop = cancel
	life := NewLifecycle(ctx)
	e.cfg = Config{SettingsPath: settings, IndexPath: index, Store: store, Keys: newArchiveKeys(store, readerLockedNote),
		Lifecycle: life, Host: "testhost", Version: "vTEST", Account: "tester", BackupInstalled: func() bool { return false }}
	e.d = newDashboard(e.cfg)
	e.h = e.d.router()
	t.Cleanup(func() {
		cancel()
		select {
		case <-life.Drained():
		case <-time.After(20 * time.Second):
			t.Error("the lifecycle did not drain")
		}
		e.cfg.Keys.lockAll(true)
	})
	return e
}

// req makes a request as the page does (the CSRF token, a loopback Origin and Host), mut changing
// it first, and returns the recorder.
func (e *rtbEnv) req(method, path string, body any, mut func(*http.Request)) *httptest.ResponseRecorder {
	e.t.Helper()
	var b []byte
	switch v := body.(type) {
	case nil:
	case string:
		b = []byte(v)
	default:
		b, _ = json.Marshal(v)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(b))
	r.Host = "127.0.0.1:8097"
	r.Header.Set("Origin", "http://127.0.0.1:8097")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-CSRF-Token", e.d.csrf)
	if mut != nil {
		mut(r)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

// post is a guarded POST decoded.
func (e *rtbEnv) post(path string, body any) map[string]any {
	e.t.Helper()
	rec := e.req(http.MethodPost, path, body, nil)
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		e.t.Fatalf("%s answered %d with a body that is not JSON: %q", path, rec.Code, rec.Body.String())
	}
	return m
}

// activity is /api/activity's reply.
func (e *rtbEnv) activity() map[string]any {
	e.t.Helper()
	rec := httptest.NewRecorder()
	e.d.activity(rec, httptest.NewRequest(http.MethodGet, "/api/activity", nil))
	var m map[string]any
	json.Unmarshal(rec.Body.Bytes(), &m)
	return m
}

// profilesReply is step 1's reply.
func (e *rtbEnv) profilesReply() map[string]any {
	return e.post("/api/restore-tb/profiles", map[string]any{})
}

// planFor plans a restore into profile position pos (whole, or the folders named) of the list the
// server sends now.
func (e *rtbEnv) planFor(pos int, folders ...string) map[string]any {
	e.t.Helper()
	pr := e.profilesReply()
	if pr["ok"] != true {
		e.t.Fatalf("profiles: %v", pr)
	}
	return e.post("/api/restore-tb/plan", map[string]any{"listID": pr["listID"], "position": pos, "whole": len(folders) == 0, "folders": folders})
}

// waitDone waits for the job slot to be free and returns the restore's activity.
func (e *rtbEnv) waitDone() map[string]any {
	e.t.Helper()
	for i := 0; i < 1200; i++ {
		a := e.activity()
		if a["running"] != true {
			return a
		}
		time.Sleep(10 * time.Millisecond)
	}
	e.t.Fatal("the restore did not end")
	return nil
}

// rtbSnapshot is every entry under each root (kind, and a file's SHA-256), and restores.log's
// bytes: what a refusal must leave as it found it. Thunderbird's own lock files are left out.
func rtbSnapshot(t *testing.T, roots ...string) string {
	t.Helper()
	var lines []string
	for _, root := range roots {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			n := d.Name()
			if n == tbrestore.ThunderbirdRefusalFile || n == tbrestore.ParentLockName || n == "parent.lock" || n == "lock" {
				return nil
			}
			rel, _ := filepath.Rel(root, p)
			if d.IsDir() {
				lines = append(lines, "D "+rel)
				return nil
			}
			b, _ := os.ReadFile(p)
			s := sha256.Sum256(b)
			lines = append(lines, "F "+rel+" "+hex.EncodeToString(s[:]))
			return nil
		})
	}
	lp, _ := tbrestore.LogPath()
	b, _ := os.ReadFile(lp)
	s := sha256.Sum256(b)
	lines = append(lines, "LOG "+hex.EncodeToString(s[:]))
	sort.Strings(lines)
	return strings.Join(lines, "\n")
}

// rtbEntries is rtbSnapshot without restores.log: what Thunderbird would see in the folders. A
// stopped restore leaves its record's started and ended lines in restores.log, as it must.
func rtbEntries(t *testing.T, roots ...string) string {
	t.Helper()
	s := rtbSnapshot(t, roots...)
	var keep []string
	for _, l := range strings.Split(s, "\n") {
		if !strings.HasPrefix(l, "LOG ") {
			keep = append(keep, l)
		}
	}
	return strings.Join(keep, "\n")
}

// restoredTops are the "Restored — …" entries in a Local Folders.
func restoredTops(t *testing.T, local string) []string {
	t.Helper()
	es, _ := os.ReadDir(local)
	var out []string
	for _, e := range es {
		if strings.HasPrefix(e.Name(), "Restored") {
			out = append(out, e.Name())
		}
	}
	return out
}

// holdRun makes the engine's run stop inside its progress callback at the first message, telling
// started, until release is closed: a test then acts while the restore really runs (its locks
// held, its first message written).
func holdRun(t *testing.T) (started, release chan struct{}) {
	t.Helper()
	started, release = make(chan struct{}), make(chan struct{})
	was := restoreTBRun
	restoreTBRun = func(ctx context.Context, p *tbengine.Plan, s tbengine.Start) (*tbengine.Result, error) {
		inner, once := s.Progress, false
		s.Progress = func(g tbengine.Progress) {
			inner(g)
			if !once {
				once = true
				close(started)
				<-release
			}
		}
		return tbengine.Run(ctx, p, s)
	}
	t.Cleanup(func() { restoreTBRun = was })
	return started, release
}

// unlock opens the encrypted current archive in the registry, as Unlock on its card does.
func (e *rtbEnv) unlock() {
	e.t.Helper()
	if _, err := e.cfg.Keys.unlock(e.out, keysPassword, keyring.OpenOptions{}); err != nil {
		e.t.Fatal(err)
	}
}
