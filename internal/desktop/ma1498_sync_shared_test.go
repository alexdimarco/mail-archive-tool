package desktop

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
)

// sharedRig is two archives made from one Thunderbird profile where auto-detect finds it (the
// test's own home), both in the Desktop's list: "other" stands for an archive whose backup is set up
// (its job file written by the test, its scheduler entry stood in), and the Desktop shows "mine",
// whose Settings card switches the sync. No real scheduler entry is ever made.
type sharedRig struct {
	t           *testing.T
	profile     string
	storeOther  string
	storeMine   string
	other, mine string
	otherJob    string
	mineJob     string
	h           http.Handler
	csrf        string
	settings    string
	mu          sync.Mutex
	installed   map[string]bool
	installs    int
}

func newSharedRig(t *testing.T) *sharedRig {
	t.Helper()
	home := t.TempDir()
	for _, v := range []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "AppData", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(v, home)
	}
	g := &sharedRig{t: t, installed: map[string]bool{}}
	g.profile = filepath.Join(home, ".thunderbird", "q1w2e3r4.default-release")
	if err := os.MkdirAll(g.profile, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.profile, "prefs.js"), []byte(localFoldersPrefs), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.profile, "compatibility.ini"), []byte("[Compatibility]\nLastVersion=153.6.0\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	g.storeOther = filepath.Join(g.profile, "Mail", "Local Folders")
	g.storeMine = filepath.Join(g.profile, "Mail", "pop.b.example")
	dir := t.TempDir()
	g.other = g.archive(g.storeOther, filepath.Join(dir, "other"), "o1")
	g.mine = g.archive(g.storeMine, filepath.Join(dir, "mine"), "m1")
	var err error
	if g.otherJob, err = job.PathFor(schedule.DefaultNameFor(g.other)); err != nil {
		t.Fatal(err)
	}
	if g.mineJob, err = job.PathFor(schedule.DefaultNameFor(g.mine)); err != nil {
		t.Fatal(err)
	}
	idx := filepath.Join(dir, archindex.Name)
	if err := archindex.WriteFile(idx, archindex.Index{Version: archindex.Version, Entries: []archindex.Entry{
		{Path: g.other, AddedAt: at0, LastSeenAt: at0}, {Path: g.mine, AddedAt: at0, LastSeenAt: at0},
	}}); err != nil {
		t.Fatal(err)
	}
	g.settings = filepath.Join(dir, "s.json")
	cfg := Config{Out: g.mine, GOOS: "darwin", Exe: "/opt/mailarchive", SettingsPath: g.settings, IndexPath: idx,
		InstallBackup: func(s schedule.Spec) error {
			g.mu.Lock()
			defer g.mu.Unlock()
			g.installs++
			g.installed[s.Name] = true
			return nil
		},
		BackupInstalledFor: func(out string) bool {
			g.mu.Lock()
			defer g.mu.Unlock()
			return g.installed[schedule.DefaultNameFor(out)]
		},
		RemoveBackupFor: func(out string) error {
			g.mu.Lock()
			defer g.mu.Unlock()
			delete(g.installed, schedule.DefaultNameFor(out))
			return nil
		},
	}
	g.h = DashboardHandler(cfg)
	_, g.csrf = getPage(t, g.h)
	return g
}

// archive archives store (one message tagged tag) into out with auto-discovery off.
func (g *sharedRig) archive(store, out, tag string) string {
	g.t.Helper()
	if err := os.MkdirAll(store, 0o700); err != nil {
		g.t.Fatal(err)
	}
	msg := "From - Mon Mar  3 09:00:00 2025\nFrom: a@example.com\nTo: b@example.com\nSubject: hi\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + tag + "@ex>\n\nbody\n\n"
	if err := os.WriteFile(filepath.Join(store, "Inbox"), []byte(msg), 0o600); err != nil {
		g.t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{store}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, log.New(io.Discard, "", 0), nil); err != nil {
		g.t.Fatal(err)
	}
	return out
}

// setOther writes the other archive's job file and says whether its scheduler entry is there.
func (g *sharedRig) setOther(j job.Job, installed bool) {
	g.t.Helper()
	j.Name, j.Out = schedule.DefaultNameFor(g.other), g.other
	if j.Mode == "" {
		j.Mode = "incremental"
	}
	_ = os.Remove(g.otherJob)
	if err := job.Write(g.otherJob, j); err != nil {
		g.t.Fatal(err)
	}
	g.mu.Lock()
	g.installed[schedule.DefaultNameFor(g.other)] = installed
	g.mu.Unlock()
}

// post sends the Settings card's save for "mine" and returns the reply's body.
func (g *sharedRig) post(body string) string {
	g.t.Helper()
	return postJSON(g.h, "/api/schedule-install", g.csrf, body).Body.String()
}

// mineSyncs reports whether "mine"'s job file carries the sync.
func (g *sharedRig) mineSyncs() bool {
	j, err := job.Read(g.mineJob)
	return err == nil && j.ThunderbirdSync != nil
}

// still snapshots what a refused switch must leave as it was: both job files, mine's descriptor,
// the settings file and the number of scheduler installs.
func (g *sharedRig) still() func() bool {
	files := []string{g.mineJob, g.otherJob, filepath.Join(g.mine, schedule.DescriptorName), g.settings}
	var before [][]byte
	for _, f := range files {
		before = append(before, fileBytes(f))
	}
	g.mu.Lock()
	n := g.installs
	g.mu.Unlock()
	return func() bool {
		for i, f := range files {
			if !bytes.Equal(before[i], fileBytes(f)) {
				return false
			}
		}
		g.mu.Lock()
		defer g.mu.Unlock()
		return g.installs == n
	}
}

// covers: MA-1498, S90
// The Settings card's sync switch (docs/design-per-mailbox-archives.md §11, revision 2's C24): it
// refuses to switch the Thunderbird sync on when another of the Desktop's archives, whose backup is
// set up and not paused, already syncs the same Thunderbird profile, naming that archive in §11's
// words, with nothing changed. Positive twin first: with the other archive's job not syncing, the
// sync switches on. Then the other archive syncs the same profile, known by its inputs' profile, by
// its job's named profile (its input elsewhere), and by auto-discovery (no input at all): each is
// refused. Its job file that cannot be read stops the switch, naming the file. Passed over: the
// other archive's backup not set up, or the archive paused. An input on a network path is never
// looked at, and a look that stalls is given up within the bound (the page's sourceWait), the switch
// then going on. Archives already set up both ways are left as they are: an edit that keeps the
// sync on is not refused. No sync question is asked for a set (MA-1497 proves no set job holds it).
func TestTheSyncSwitchRefusesASecondArchiveForOneProfile(t *testing.T) {
	g := newSharedRig(t)
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("mine's backup set up without the sync: %s", body)
	}

	// Positive twin: the other archive's backup is set up but does not sync.
	g.setOther(job.Job{Inputs: []string{g.storeOther}}, true)
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) || !g.mineSyncs() {
		t.Fatalf("positive twin: no other archive syncs this profile, the sync switches on: %s", body)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":false}`); !strings.Contains(body, `"ok":true`) || g.mineSyncs() {
		t.Fatalf("switched off again: %s", body)
	}

	words := "Thunderbird sync is already on for " + g.other + ", which backs up the same Thunderbird profile. Only one archive can sync a profile for now: switch it off there first."
	for _, c := range []struct {
		name string
		j    job.Job
	}{
		{"by its inputs' profile", job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}},
		{"by its job's named profile", job.Job{Inputs: []string{plainMboxStore(t)}, ThunderbirdSync: &job.ThunderbirdSync{Profile: g.profile}}},
		{"by auto-discovery", job.Job{Auto: true, ThunderbirdSync: &job.ThunderbirdSync{}}},
	} {
		g.setOther(c.j, true)
		still := g.still()
		body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
		t.Logf("the other archive syncs the profile %s", c.name)
		assure.Refused(t, refusedCode(body), said(body), assure.Names(words), assure.NoSideEffect(still))
		if g.mineSyncs() {
			t.Errorf("%s: mine's sync was switched on", c.name)
		}
	}

	// The other archive's job file cannot be read: the switch stops, naming the file.
	g.setOther(job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}, true)
	if err := os.WriteFile(g.otherJob, []byte(`{"version":1,"out":`), 0o600); err != nil {
		t.Fatal(err)
	}
	still := g.still()
	body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
	assure.Refused(t, refusedCode(body), said(body),
		assure.Names("Could not read the settings of "+g.other+": "+g.otherJob+". Try again, or Repair that archive."),
		assure.NoSideEffect(still))

	// Passed over: its backup not set up; the archive paused.
	g.setOther(job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}, false)
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) || !g.mineSyncs() {
		t.Errorf("an archive whose backup is not set up holds the switch back: %s", body)
	}
	g.post(`{"interval":"weekly","time":"03:00","presync":false}`)
	g.setOther(job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}, true)
	if err := seal.Seal(g.other, "parked"); err != nil {
		t.Fatal(err)
	}
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) || !g.mineSyncs() {
		t.Errorf("a paused archive holds the switch back: %s", body)
	}
	if err := seal.Unseal(g.other); err != nil {
		t.Fatal(err)
	}
	g.post(`{"interval":"weekly","time":"03:00","presync":false}`)

	// An input on a network path is never looked at.
	var looked []string
	var lookMu sync.Mutex
	orig := findProfile
	t.Cleanup(func() { findProfile = orig })
	findProfile = func(p string) (string, bool) {
		lookMu.Lock()
		looked = append(looked, p)
		lookMu.Unlock()
		return orig(p)
	}
	share := "//nas.example/mail/Local Folders"
	g.setOther(job.Job{Inputs: []string{share}, ThunderbirdSync: &job.ThunderbirdSync{}}, true)
	if body := g.post(`{"interval":"weekly","time":"03:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Errorf("a network input: %s", body)
	}
	lookMu.Lock()
	for _, p := range looked {
		if strings.HasPrefix(p, "//") || strings.HasPrefix(p, `\\`) {
			t.Errorf("a network path was looked at: %q", p)
		}
	}
	if len(looked) == 0 {
		t.Error("no look was made at all (the test proves nothing)")
	}
	lookMu.Unlock()
	g.post(`{"interval":"weekly","time":"03:00","presync":false}`)

	// A look that stalls is given up within the bound.
	g.setOther(job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}, true)
	origWait := sourceWait
	sourceWait = 50 * time.Millisecond
	release := make(chan struct{})
	t.Cleanup(func() { sourceWait = origWait })
	findProfile = func(p string) (string, bool) { <-release; return orig(p) }
	start := time.Now()
	body = g.post(`{"interval":"weekly","time":"03:00","presync":true}`)
	took := time.Since(start)
	close(release)
	findProfile = orig
	if took > 3*time.Second {
		t.Errorf("a stalled look held the switch %v", took)
	}
	if !strings.Contains(body, `"ok":true`) {
		t.Errorf("a stalled look: %s", body)
	}

	// Archives already set up both ways are left as they are: an edit keeping the sync on goes on.
	g.setOther(job.Job{Inputs: []string{g.storeOther}, ThunderbirdSync: &job.ThunderbirdSync{}}, true)
	otherBefore := fileBytes(g.otherJob)
	if !g.mineSyncs() {
		t.Fatal("fixture: mine syncs after the stalled look's switch")
	}
	if body := g.post(`{"interval":"daily","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) || !g.mineSyncs() {
		t.Errorf("an edit that keeps the sync on, both archives syncing one profile: %s", body)
	}
	if !bytes.Equal(otherBefore, fileBytes(g.otherJob)) {
		t.Error("the other archive's job file was changed")
	}
}

// plainMboxStore is a mail folder outside any Thunderbird profile.
func plainMboxStore(t *testing.T) string {
	t.Helper()
	d := filepath.Join(t.TempDir(), "mail")
	if err := os.MkdirAll(d, 0o700); err != nil {
		t.Fatal(err)
	}
	return d
}
