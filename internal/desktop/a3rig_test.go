package desktop

import (
	"bytes"
	"html"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/graphconfig"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// a3Rig is a Desktop for one archive with a stand-in scheduler (slice A3's tests): every install
// is recorded and marks the entry installed, Remove takes it out, and next, when set, decides
// what the next install does (a failure injected at the scheduler step). No real schedule is
// installed and no real configuration is read: the home folder is the test's own.
type a3Rig struct {
	t        *testing.T
	out      string
	cfg      Config
	h        http.Handler
	csrf     string
	settings string
	jobPath  string
	fake     *backupFake
	installs []schedule.Spec
	next     []func(schedule.Spec) error
}

// isolateHome points the home and config folders at a fresh temporary folder, so the job files,
// the default Microsoft 365 configuration and the Desktop settings the kind guard reads are this
// test's own. Call it before anything that computes a job file's path.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("AppData", filepath.Join(home, "AppData"))
	if d, err := job.Dir(); err == nil {
		os.MkdirAll(d, 0o700)
	}
	return home
}

// countCapture sets Microsoft 365 up on this computer (the default configuration, with a tenant
// and a client id) with the Desktop's recorded BackupFor at out: today's owner-side evidence then
// counts a capture for out (CaptureCorroborates), as the kind guard does. It is counted through
// BackupFor, not the settings' Out; until MA-874 a settings file whose Out named out was itself
// read as a run-job of out by the evidence's job-folder scan, and the fixture still checks that
// it is not (TestTheDesktopSettingsFileIsNeverCountedAsARunJob guards it).
func countCapture(t *testing.T, out string) {
	t.Helper()
	cfgPath, err := graphconfig.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := graphconfig.Save(cfgPath, &graphconfig.Config{Tenant: "t", ClientID: "c"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveSettings(filepath.Join(filepath.Dir(cfgPath), "desktop-settings.json"), Settings{Out: t.TempDir(), BackupFor: out}); err != nil {
		t.Fatal(err)
	}
	ev := schedule.BuildKindEvidence(out)
	if !ev.Has(schedule.KindCapture) {
		t.Fatal("fixture: no capture is counted")
	}
	if ev.Has(schedule.KindRunJob) && strings.HasSuffix(ev.JobPath, "desktop-settings.json") {
		t.Fatal("fixture: the settings file reads as a run-job")
	}
}

func newA3Rig(t *testing.T, out, goos string, mod ...func(*Config)) *a3Rig {
	t.Helper()
	g := &a3Rig{t: t, out: out, settings: filepath.Join(t.TempDir(), "s.json"), fake: &backupFake{tasks: map[string]bool{}}}
	jp, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		t.Fatal(err)
	}
	g.jobPath = jp
	t.Cleanup(func() { os.Remove(jp) })
	g.cfg = Config{Out: out, GOOS: goos, Exe: "/opt/mailarchive", SettingsPath: g.settings,
		BackupInstalledFor: g.fake.installedFor, RemoveBackupFor: g.fake.removeFor, EntryState: g.fake.entryState,
		InstallBackup: func(s schedule.Spec) error {
			if len(g.next) > 0 {
				f := g.next[0]
				g.next = g.next[1:]
				return f(s)
			}
			g.record(s)
			return nil
		}}
	for _, m := range mod {
		m(&g.cfg)
	}
	g.h = DashboardHandler(g.cfg)
	_, g.csrf = getPage(t, g.h)
	return g
}

// record is a working scheduler's install: the entry is there.
func (g *a3Rig) record(s schedule.Spec) {
	g.installs = append(g.installs, s)
	g.fake.tasks[s.Out] = true
}

func (g *a3Rig) post(body string) string {
	g.t.Helper()
	return postJSON(g.h, "/api/schedule-install", g.csrf, body).Body.String()
}

func (g *a3Rig) page() string {
	g.t.Helper()
	p, _ := getPage(g.t, g.h)
	return p
}

// recordLast writes the descriptor the last install would have written (the stand-in writes
// none), as schedule.Install does after the scheduler took the entry.
func (g *a3Rig) recordLast() {
	g.t.Helper()
	if len(g.installs) == 0 {
		g.t.Fatal("nothing was installed to record")
	}
	if err := schedule.WriteDescriptor(g.out, schedule.DescribeSpec(g.installs[len(g.installs)-1])); err != nil {
		g.t.Fatal(err)
	}
}

// a3Snapshot is what a refused or rolled-back request must leave as it was: the job file, the
// descriptor, the settings file, the installs and the scheduler's entries.
type a3Snapshot struct {
	g                   *a3Rig
	jobB, descB, settsB []byte
	installs            int
	tasks               map[string]bool
}

func (g *a3Rig) snapshot() a3Snapshot {
	s := a3Snapshot{g: g, jobB: fileBytes(g.jobPath), descB: fileBytes(filepath.Join(g.out, schedule.DescriptorName)),
		settsB: fileBytes(g.settings), installs: len(g.installs), tasks: map[string]bool{}}
	for k, v := range g.fake.tasks {
		s.tasks[k] = v
	}
	return s
}

func (s a3Snapshot) holds() bool {
	g := s.g
	if !bytes.Equal(s.jobB, fileBytes(g.jobPath)) || !bytes.Equal(s.descB, fileBytes(filepath.Join(g.out, schedule.DescriptorName))) ||
		!bytes.Equal(s.settsB, fileBytes(g.settings)) || s.installs != len(g.installs) || len(s.tasks) != len(g.fake.tasks) {
		return false
	}
	for k, v := range s.tasks {
		if g.fake.tasks[k] != v {
			return false
		}
	}
	return true
}

// shows reports whether the page carries text as the template renders it (escaped).
func shows(page, text string) bool { return strings.Contains(page, html.EscapeString(text)) }

// setStores rewrites the archive's record to name exactly these sources.
func setStores(t *testing.T, out string, ids ...string) {
	t.Helper()
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	m.Stores = map[string]string{}
	for _, id := range ids {
		m.Stores[id] = filepath.Base(id)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// addStore adds a source to the archive's record, keeping the ones it has.
func addStore(t *testing.T, out, id string) {
	t.Helper()
	m, err := state.Load(filepath.Join(out, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	if m.Stores == nil {
		m.Stores = map[string]string{}
	}
	m.Stores[id] = "x"
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// emptyArchive is a folder holding an archive record with no source recorded yet.
func emptyArchive(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "empty")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	m, err := state.Load(filepath.Join(dir, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	m.Stores = map[string]string{}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	return dir
}
