package desktop

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/alert"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/backupalert"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// The migration and rollback check (design-desktop-settings-card section 10, slice A6, MA-864):
// an existing install is shown as it is and nothing on disk changes until the owner clicks Save,
// the one exception being section 8's start-up clean-up of a BackupFor that names a run-job; and
// nothing this version writes is a field an older MailArchive (v1.5.0, commit 1103d02) misreads.

// diskEntry is one file or folder as a snapshot saw it.
type diskEntry struct {
	size  int64
	mode  fs.FileMode
	mtime time.Time
	data  []byte // a regular file's bytes
}

// diskSnapshot is every file and folder under roots: path, size, mode, modification time and,
// for a file, its bytes. A root that isn't there is recorded as absent.
func diskSnapshot(t *testing.T, roots ...string) map[string]diskEntry {
	t.Helper()
	snap := map[string]diskEntry{}
	for _, root := range roots {
		if _, err := os.Lstat(root); errors.Is(err, fs.ErrNotExist) {
			snap[root+" (absent)"] = diskEntry{}
			continue
		}
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			fi, err := os.Lstat(p)
			if err != nil {
				return err
			}
			e := diskEntry{size: fi.Size(), mode: fi.Mode(), mtime: fi.ModTime()}
			if fi.Mode().IsRegular() {
				if e.data, err = os.ReadFile(p); err != nil {
					return err
				}
			}
			snap[p] = e
			return nil
		})
		if err != nil {
			t.Fatalf("snapshot of %s: %v", root, err)
		}
	}
	return snap
}

// diskChange is one path two snapshots disagree on.
type diskChange struct {
	path          string
	before, after diskEntry
	added, gone   bool
}

func (c diskChange) String() string {
	switch {
	case c.added:
		return "added " + c.path
	case c.gone:
		return "removed " + c.path
	}
	b, a := c.before, c.after
	return fmt.Sprintf("changed %s (size %d->%d, mode %v->%v, mtime %v->%v, bytes equal %v)", c.path, b.size, a.size, b.mode, a.mode, b.mtime, a.mtime, bytes.Equal(b.data, a.data))
}

// timeOnly: the path is in both snapshots and only its modification time moved.
func (c diskChange) timeOnly() bool {
	return !c.added && !c.gone && c.before.size == c.after.size && c.before.mode == c.after.mode && bytes.Equal(c.before.data, c.after.data)
}

// diskChanges lists what differs between two snapshots, sorted by path.
func diskChanges(before, after map[string]diskEntry) []diskChange {
	var out []diskChange
	for p, b := range before {
		a, ok := after[p]
		switch {
		case !ok:
			out = append(out, diskChange{path: p, before: b, gone: true})
		case b.size != a.size || b.mode != a.mode || !b.mtime.Equal(a.mtime) || !bytes.Equal(b.data, a.data):
			out = append(out, diskChange{path: p, before: b, after: a})
		}
	}
	for p, a := range after {
		if _, ok := before[p]; !ok {
			out = append(out, diskChange{path: p, after: a, added: true})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func changeLines(ch []diskChange) string {
	var s []string
	for _, c := range ch {
		s = append(s, c.String())
	}
	return strings.Join(s, "\n")
}

// statusProbeFiles are the archive files the status check (health.Gather, which the page's
// /api/health and `mailarchive status` run) touches whenever it looks at an archive, written by no
// backup setting or record: it test-acquires the archive lock (rewriting .mailarchive.lock), opens
// the search index read-only (SQLite keeps search.db-wal and -shm beside it), and writes and removes
// a probe file to see the folder is writable (the archive folder's own time). The owner ruled on
// 2026-10-28 that "no write on page load" covers the backup's settings and records, not these;
// TestLoadingTheOverviewWritesOnlyTheStatusChecksHousekeeping holds them to what they are.
func statusProbeFiles(out string) map[string]bool {
	return map[string]bool{out: true, filepath.Join(out, lockfile.Name): true,
		filepath.Join(out, "search.db-wal"): true, filepath.Join(out, "search.db-shm"): true}
}

// without drops the changes to the paths in skip.
func without(ch []diskChange, skip map[string]bool) []diskChange {
	var out []diskChange
	for _, c := range ch {
		if !skip[c.path] {
			out = append(out, c)
		}
	}
	return out
}

// loadOverview is what a browser does when it opens the Overview page: the page itself, then the
// requests its script makes on load (the activity, the card's schedule state and the status).
func loadOverview(t *testing.T, h http.Handler) string {
	t.Helper()
	page := ""
	for _, p := range []string{"/", "/api/activity", "/api/schedule-state", "/api/health"} {
		req := httptest.NewRequest("GET", p, nil)
		req.Host = "127.0.0.1:8097"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d: %s", p, rec.Code, rec.Body.String())
		}
		if p == "/" {
			page = rec.Body.String()
		}
	}
	return page
}

// existingInstall is an install an earlier version left, for the page-load checks: a local archive
// whose hourly run-job syncs Thunderbird, its descriptor, a source streak carrying last_unreachable,
// an alert streak, the Desktop settings file and the known-archives index (already listing the
// archive, as an earlier page load left it). It returns the rig and the folders to snapshot: the
// home folder (the job folder and the configuration), the settings folder, the index's folder and
// the archive. Positive twin first: a Save (the sync switched off) changes the job file, and the
// snapshot notices it.
func existingInstall(t *testing.T) (*a3Rig, []string) {
	t.Helper()
	home := isolateHome(t)
	out := tbProfileArchive(t)
	idx := filepath.Join(t.TempDir(), "known-archives.json")
	g := newA3Rig(t, out, "darwin", func(c *Config) { c.IndexPath = idx })
	if body := g.post(`{"interval":"hourly","time":"00:15","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	if err := UpdateSettings(g.settings, "test", func(s *Settings) { s.Out = out; s.AlertOnFailure = true; s.ExportDest = t.TempDir() }); err != nil {
		t.Fatal(err)
	}
	t0 := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	if err := state.WriteSourceStreak(out, state.SourceStreak{}.Observe(false, "missing", t0).Observe(false, "missing", t0.Add(time.Hour))); err != nil {
		t.Fatal(err)
	}
	backupalert.Report(g.settings, nil, backupalert.KeyFor(out), out, alert.ResultFailed, errors.New("source gone"), 0, nil)
	if _, err := os.Stat(backupalert.StreakPath(g.settings, backupalert.KeyFor(out))); err != nil {
		t.Fatalf("fixture: no alert streak file: %v", err)
	}
	loadOverview(t, g.h) // an earlier version's page load already recorded this archive in the index
	if _, err := os.Stat(idx); err != nil {
		t.Fatalf("fixture: no known-archives index: %v", err)
	}
	roots := []string{home, filepath.Dir(g.settings), filepath.Dir(idx), out}

	// The positive twin: a Save is seen.
	before := diskSnapshot(t, roots...)
	if body := g.post(`{"interval":"hourly","time":"00:15","presync":false}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("the positive twin's Save: %s", body)
	}
	saved := diskChanges(before, diskSnapshot(t, roots...))
	if !strings.Contains(changeLines(saved), "changed "+g.jobPath+" ") {
		t.Fatalf("the positive twin: the snapshot did not see the Save change the job file:\n%s", changeLines(saved))
	}
	return g, roots
}

// loadThreeTimes builds the Desktop afresh and loads the Overview page three times, returning
// what changed on disk under roots meanwhile.
func loadThreeTimes(t *testing.T, g *a3Rig, roots []string) []diskChange {
	t.Helper()
	before := diskSnapshot(t, roots...)
	h := DashboardHandler(g.cfg)
	var page string
	for i := 0; i < 3; i++ {
		page = loadOverview(t, h)
	}
	if !strings.Contains(cardOf(page, "sec-settings"), `<option value="hourly" selected>`) {
		t.Fatal("fixture: the page did not draw the installed hourly schedule")
	}
	return diskChanges(before, diskSnapshot(t, roots...))
}

// covers: MA-864, R37
// Loading the Overview page writes no backup setting or record (section 10): for an existing
// install (existingInstall, its positive twin first), building the Desktop and loading the page and
// its on-load requests three times leaves every file and folder under the home folder (the job
// folder and the configuration), the settings folder, the index's folder and the archive as they
// were — path, size, mode, modification time and bytes — but for the status check's own probe files
// (statusProbeFiles), which the open gap below keeps in view.
func TestLoadingTheOverviewWritesNothing(t *testing.T) {
	g, roots := existingInstall(t)
	if ch := without(loadThreeTimes(t, g, roots), statusProbeFiles(g.out)); len(ch) > 0 {
		t.Errorf("loading the page wrote to disk:\n%s", changeLines(ch))
	}
}

// covers: MA-864, R37
// The owner's ruling of 2026-10-28 on the gap slice A6 found: "no write on page load" covers the
// backup's settings and records; the status check the page runs on load (/api/health,
// health.Gather, the same check `mailarchive status` runs) keeps its housekeeping in the archive.
// That housekeeping is held to what it is, so nothing else can hide in it: the archive lock is
// rewritten, never removed; SQLite's search.db-wal and -shm may appear or change beside the index;
// the archive folder's own time moves (the writable probe, written and removed); and no other file
// in the archive is added, changed or removed, the probe included.
func TestLoadingTheOverviewWritesOnlyTheStatusChecksHousekeeping(t *testing.T) {
	g, roots := existingInstall(t)
	lock := filepath.Join(g.out, lockfile.Name)
	sawLock := false
	for _, c := range loadThreeTimes(t, g, roots) {
		sawLock = sawLock || c.path == lock
		switch {
		case !statusProbeFiles(g.out)[c.path]:
			t.Errorf("loading the page wrote outside the status check's housekeeping: %s", c)
		case c.path == lock && (c.gone || c.added):
			t.Errorf("loading the page removed or made the archive lock instead of rewriting it: %s", c)
		case c.path != lock && c.path != g.out && c.gone:
			t.Errorf("loading the page removed a search index side file: %s", c)
		}
	}
	// the snapshot reads the archive: the housekeeping it holds to is seen happening
	assure.Reached(t, sawLock, "the status check's rewrite of the archive lock seen by the snapshot")
}

// settingsWithEveryField is a settings file an earlier version could have left: every field set,
// BackupFor naming bf.
func settingsWithEveryField(t *testing.T, bf string) Settings {
	on, off := true, false
	return Settings{Out: "", KeepRaw: true, IncludeDeleted: true, IncludeJunk: true, WeeklyDay: "Sunday", WeeklyTime: "04:00",
		Interval: "daily", BackupFor: bf, ExportDest: t.TempDir(), ExportPreserve: &off, ExportHTML: &on, ExportAttachments: &on,
		ExportAttachmentsAs: "loose", AlertEnabled: true, AlertOnFailure: true, AlertOnSuccess: true, AlertOnPause: true,
		AlertProvider: "smtp", AlertHost: "mail.example.com", AlertPort: 587, AlertSecurity: "starttls",
		AlertFrom: "a@example.com", AlertTo: "b@example.com", AlertUsername: "a"}
}

// covers: MA-864, MA-862, R37
// The start-up BackupFor clean-up (section 8, rev4.1) is the only write before Save and changes only
// that field: a real Desktop start (serveInstance) whose settings file records a run-job's folder as
// BackupFor clears it, every other key of the settings file byte-identical, its mode kept, and
// nothing else under the configuration folder (the archive included), the home folder (the job
// folder) or the run-job's archive changed — the settings folder's own time aside, which the
// file's atomic replace (a temporary file renamed over it) moves. Positive twin first: with a
// capture's BackupFor the same start changes nothing at all. Not snapshotted: the per-account state
// folder the launch keeps its own lock and instance file in (it is the running instance's, not a
// setting or a record).
func TestTheStartUpCleanUpIsTheOnlyWriteBeforeSave(t *testing.T) {
	home := isolateHome(t)
	start := func(bf string, extra ...string) (sp string, ch []diskChange, before, after map[string]diskEntry) {
		g := newDRig(t)
		sp = filepath.Join(filepath.Dir(g.cfgPath), "desktop-settings.json")
		s := settingsWithEveryField(t, bf)
		s.Out = g.out
		if err := SaveSettings(sp, s); err != nil {
			t.Fatal(err)
		}
		roots := append([]string{filepath.Dir(g.cfgPath), home}, extra...)
		before = diskSnapshot(t, roots...)
		g.start("-no-browser")
		g.stop()
		after = diskSnapshot(t, roots...)
		return sp, diskChanges(before, after), before, after
	}

	capture := t.TempDir()
	if _, ch, _, _ := start(capture, capture); len(ch) > 0 {
		t.Fatalf("the positive twin: a start with a capture's BackupFor wrote to disk:\n%s", changeLines(ch))
	}

	a := t.TempDir()
	jp := plantRunJob(t, a)
	sp, ch, before, after := start(a, a)
	var other []diskChange
	sawSettings := false
	for _, c := range ch {
		switch {
		case c.path == sp && !c.added && !c.gone:
			sawSettings = true
		case c.path == filepath.Dir(sp) && c.timeOnly():
		default:
			other = append(other, c)
		}
	}
	if !sawSettings || len(other) > 0 {
		t.Fatalf("the start-up clean-up changed more than the settings file (or not it):\n%s", changeLines(ch))
	}
	if before[sp].mode != after[sp].mode {
		t.Errorf("the settings file's mode changed: %v -> %v", before[sp].mode, after[sp].mode)
	}
	var b, af map[string]json.RawMessage
	if err := json.Unmarshal(before[sp].data, &b); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after[sp].data, &af); err != nil {
		t.Fatal(err)
	}
	if string(b["backupFor"]) == "" || af["backupFor"] != nil {
		t.Errorf("BackupFor was not cleared: before %s, after %s", b["backupFor"], af["backupFor"])
	}
	delete(b, "backupFor")
	if len(b) != len(af) {
		t.Errorf("the settings file's keys changed: before %d (without backupFor), after %d", len(b), len(af))
	}
	for k, v := range b {
		if !bytes.Equal(v, af[k]) {
			t.Errorf("the clean-up changed %q: %s -> %s", k, v, af[k])
		}
	}
	if _, err := job.Read(jp); err != nil {
		t.Errorf("the run-job's job file no longer reads: %v", err)
	}
}

// v150Job is the job file as MailArchive v1.5.0 reads it (internal/job/job.go at 1103d02), copied
// so this check keeps the older reader's shape whatever this version's own becomes.
type v150Job struct {
	Version         int         `json:"version"`
	Name            string      `json:"name"`
	Inputs          []string    `json:"inputs,omitempty"`
	Auto            bool        `json:"auto,omitempty"`
	Out             string      `json:"out"`
	Mode            string      `json:"mode"`
	Since           string      `json:"since,omitempty"`
	CopyFirst       bool        `json:"copy_first,omitempty"`
	Outlook         bool        `json:"outlook,omitempty"`
	OutlookSyncWait string      `json:"outlook_sync_wait,omitempty"`
	KeepRaw         bool        `json:"raw,omitempty"`
	FollowLinks     []string    `json:"follow_links,omitempty"`
	ThunderbirdSync *v150TBSync `json:"thunderbird_sync,omitempty"`
	ProgramCopies   string      `json:"program_copies,omitempty"`
}

type v150TBSync struct {
	Profile string `json:"profile,omitempty"`
}

// v150ReadJob applies v1.5.0's job.Read rules to a job file's bytes, strictly: every key must be
// one v1.5.0 has a field for, of the type it reads (an unknown key it would ignore is reported too,
// so a new key is a decision, never an accident); the version is at most 1; out is set.
func v150ReadJob(data []byte) (v150Job, error) {
	var j v150Job
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&j); err != nil {
		return j, err
	}
	if j.Version > 1 {
		return j, fmt.Errorf("version %d: v1.5.0 refuses it as newer", j.Version)
	}
	if j.Out == "" {
		return j, errors.New("names no output folder")
	}
	return j, nil
}

// writeV150Job writes the job file as v1.5.0's job.Write did for j: indented JSON, version 1,
// owner-only, with the Thunderbird sync on (its profile kept) or off (the key absent).
func writeV150Job(t *testing.T, path string, j job.Job, sync bool) {
	t.Helper()
	v := v150Job{Version: 1, Name: j.Name, Inputs: j.Inputs, Auto: j.Auto, Out: j.Out, Mode: j.Mode, Since: j.Since,
		CopyFirst: j.CopyFirst, Outlook: j.Outlook, OutlookSyncWait: j.OutlookSyncWait, KeepRaw: j.KeepRaw,
		FollowLinks: j.FollowLinks, ProgramCopies: j.ProgramCopies}
	if sync {
		v.ThunderbirdSync = &v150TBSync{}
		if j.ThunderbirdSync != nil {
			v.ThunderbirdSync.Profile = j.ThunderbirdSync.Profile
		}
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	os.Remove(path)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-864, R37
// Settings shows the installed values as they are, with the sync on or off (section 10): for a
// job file written as v1.5.0 wrote it, with thunderbird_sync present (on) or absent (off), and the
// descriptor's cadence daily at 04:00, hourly at :15 or weekly at 03:00, the editable form (macOS)
// selects that How often, shows that time and ticks the sync box exactly when the job file syncs;
// the read-only form (Linux) says the same in words. Positive twin first: the schedule this version
// itself created draws the same way.
func TestSettingsShowsTheInstalledValuesWithSyncOnOrOff(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	if body := g.post(`{"interval":"daily","time":"04:00","presync":true}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("create: %s", body)
	}
	g.recordLast()
	lin := newA3Rig(t, out, "linux")
	spec := g.installs[0]
	j0, err := job.Read(g.jobPath)
	if err != nil || j0.ThunderbirdSync == nil {
		t.Fatalf("fixture: the created job file (%v) does not sync", err)
	}
	check := func(what, iv, at string, sync bool) {
		t.Helper()
		page := g.page()
		s := cardOf(page, "sec-settings")
		syncWord := map[bool]string{true: "on", false: "off"}[sync]
		for _, want := range []string{`<option value="` + iv + `" selected>`, `id="schedTime" type="time" value="` + at + `"`,
			`data-installed-interval="` + iv + `"`, `data-installed-at="` + at + `"`} {
			if !strings.Contains(s, want) {
				t.Errorf("%s: Settings lacks %q", what, want)
			}
		}
		if ticked := strings.Contains(s, `id="schedPresync" checked`); ticked != sync {
			t.Errorf("%s: the sync box is ticked %v, the job file's sync is %s", what, ticked, syncWord)
		}
		if marked := strings.Contains(s, `data-installed-sync="1"`); marked != sync {
			t.Errorf("%s: the installed sync the page compares with is %v, the job file's is %s", what, marked, syncWord)
		}
		if strings.Contains(s, "No scheduled backup") {
			t.Errorf("%s: Settings offers \"No scheduled backup\" for an installed schedule", what)
		}
		ro := cardOf(lin.page(), "sec-settings")
		if want := "How often: " + cadenceWords(iv, at) + ". Sync Thunderbird first: " + syncWord + "."; !shows(ro, want) {
			t.Errorf("%s: the read-only Settings lacks %q", what, want)
		}
	}
	check("the positive twin, this version's own create", "daily", "04:00", true)

	for _, c := range []struct {
		iv   schedule.Interval
		at   string
		sync bool
	}{{schedule.Daily, "04:00", true}, {schedule.Daily, "04:00", false}, {schedule.Hourly, "00:15", true},
		{schedule.Hourly, "00:15", false}, {schedule.Weekly, "03:00", true}, {schedule.Weekly, "03:00", false}} {
		writeV150Job(t, g.jobPath, j0, c.sync)
		s := spec
		s.Interval, s.At = c.iv, c.at
		if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(s)); err != nil {
			t.Fatal(err)
		}
		files := []string{g.jobPath, filepath.Join(out, schedule.DescriptorName)}
		before := diskSnapshot(t, files...)
		check(fmt.Sprintf("a v1.5.0 job file, %s at %s, sync %v", c.iv, c.at, c.sync), string(c.iv), c.at, c.sync)
		if ch := diskChanges(before, diskSnapshot(t, files...)); len(ch) > 0 {
			t.Errorf("drawing Settings rewrote the v1.5.0 job file or the descriptor (%s, sync %v):\n%s", c.iv, c.sync, changeLines(ch))
		}
	}
}

// covers: MA-864, MA-857, R37
// A capture installed on a local archive is shown, with Remove, and never converted (section 10,
// section 3.2's "Local" rows): drawing the page and Settings, three times, leaves its descriptor,
// its scheduler entry, the job folder and the settings files as they were — no install, no
// removal, no job file — whether a capture is counted here (Microsoft 365 set up, with section 4's
// notice) or not. (The status check's own probe files are left to the open gap above.) Settings offers no schedule inputs for it. Positive twin first: the same archive
// with nothing recorded is offered its local backup.
func TestACaptureOnALocalArchiveIsShownNotConverted(t *testing.T) {
	for _, counted := range []bool{false, true} {
		home := isolateHome(t)
		out := tbProfileArchive(t)
		if !counted {
			fresh := newA3Rig(t, out, "darwin")
			if s := cardOf(fresh.page(), "sec-settings"); !strings.Contains(s, `id="schedInterval"`) {
				t.Fatalf("the positive twin: the local archive with nothing recorded is not offered its backup:\n%s", s)
			}
		}
		var mods []func(*Config)
		if counted {
			countCapture(t, out)
			mods = append(mods, withM365(t))
		}
		writeCaptureDescriptor(t, out, schedule.Daily, "06:30")
		g := newA3Rig(t, out, "darwin", mods...)
		g.fake.tasks[out] = true
		roots := []string{home, filepath.Dir(g.settings), out}
		before := diskSnapshot(t, roots...)
		var page string
		for i := 0; i < 3; i++ {
			page = loadOverview(t, g.h)
		}
		what := map[bool]string{false: "an uncounted capture", true: "a counted capture"}[counted]
		card, set := cardOf(page, "sec-backup"), cardOf(page, "sec-settings")
		if !strings.Contains(card, `id="schedRemove"`) {
			t.Errorf("%s on a local archive has no Remove:\n%s", what, card)
		}
		for _, bad := range []string{`id="schedInterval"`, `id="schedTime"`, `id="schedPresync"`, `id="runBackupBtn"`} {
			if strings.Contains(card+set, bad) {
				t.Errorf("%s on a local archive is offered %s, as if it could be made a local backup", what, bad)
			}
		}
		if counted && !shows(page, captureCountedLine) {
			t.Errorf("%s: section 4's notice is not shown", what)
		}
		if ch := without(diskChanges(before, diskSnapshot(t, roots...)), statusProbeFiles(out)); len(ch) > 0 {
			t.Errorf("%s: drawing the page wrote to disk:\n%s", what, changeLines(ch))
		}
		if len(g.installs) != 0 || len(g.fake.removed) != 0 || !g.fake.tasks[out] || fileBytes(g.jobPath) != nil {
			t.Errorf("%s was converted: installs %d, removed %v, entry %v, job file %v", what, len(g.installs), g.fake.removed, g.fake.tasks[out], fileBytes(g.jobPath) != nil)
		}
		if d, err := schedule.ReadDescriptor(out); err != nil || schedule.KindOf(d.Job) != schedule.KindCapture {
			t.Errorf("%s: its descriptor no longer records a capture (%v)", what, err)
		}
	}
}

// v150SourceStreak is the source streak as MailArchive v1.5.0 reads it (internal/state/
// sourceloss.go at 1103d02), with plain json.Unmarshal.
type v150SourceStreak struct {
	Version          int       `json:"version"`
	ConsecutiveFails int       `json:"consecutive_fails"`
	FirstUnreachable time.Time `json:"first_unreachable,omitempty"`
	LastSuccess      time.Time `json:"last_success,omitempty"`
	LastKind         string    `json:"last_kind,omitempty"`
}

// v150Descriptor is the schedule descriptor as v1.5.0 reads it (internal/schedule/descriptor.go
// at 1103d02).
type v150Descriptor struct {
	Version     int       `json:"version"`
	Name        string    `json:"name"`
	Interval    string    `json:"interval"`
	At          string    `json:"at"`
	Exe         string    `json:"exe"`
	ExeSize     int64     `json:"exe_size,omitempty"`
	ExeMTime    time.Time `json:"exe_mtime,omitempty"`
	Wrapper     string    `json:"wrapper,omitempty"`
	Job         []string  `json:"job"`
	Log         string    `json:"log,omitempty"`
	InstalledAt time.Time `json:"installed_at"`
	Host        string    `json:"host"`
	Presync     *struct {
		Kind    string `json:"kind"`
		Profile string `json:"profile,omitempty"`
	} `json:"presync,omitempty"`
}

// v150ConfigFolderReads are the ways v1.5.0 finds files in the owner's config folder by name, as
// predicates on a file name (each copied from 1103d02): the job-folder scan of BuildKindEvidence
// opens every "*.json" (kindevidence.go corroboratedRunJob); uninstall lists the fixed names,
// "*.json" and "*.jobfail.log" (uninstall/config.go deletableConfigName); a headless run opens its
// own "<name>.json" and appends to "<name>.jobfail.log"; the Desktop and the wizard open their
// fixed files by name.
var v150ConfigFolderReads = map[string]func(string) bool{
	"the job-folder scan (*.json)": func(n string) bool { return strings.HasSuffix(n, ".json") },
	"uninstall's list": func(n string) bool {
		return v150FixedNames[n] || strings.HasSuffix(n, ".json") || strings.HasSuffix(n, ".jobfail.log")
	},
	"a run's failure log": func(n string) bool { return strings.HasSuffix(n, ".jobfail.log") },
	"a fixed file":        func(n string) bool { return v150FixedNames[n] },
}

var v150FixedNames = map[string]bool{"desktop-settings.json": true, "graph-config.json": true, "last-archive.txt": true,
	"known-archives.json": true, "desktop-capture.log": true}

// covers: MA-864, R37
// Rollback: nothing this version writes is a field an older MailArchive misreads (section 10).
//   - The job file the Desktop writes — a create, hourly with the sync on; an in-place edit that
//     switches the sync off; one that changes the cadence — holds only keys v1.5.0's reader has a
//     field for, of the type it reads, at version 1, naming its folder: v1.5.0's rules accept it
//     (and job.Read does), and it reads back the same sync and folder. Its descriptor is v1.5.0's
//     shape, and "hourly" is an interval v1.5.0 parses (it would read an unknown one as daily).
//   - The source streak with last_unreachable parses with plain json.Unmarshal into v1.5.0's
//     shape, every field it knows kept, last_unreachable the one key it skips, within its size cap.
//   - The alert streak file (with its 'noticed' field) is a name none of v1.5.0's config-folder
//     reads match. Positive twin first: each of those reads matches a real job file in the same
//     folder, so the check is not vacuous.
func TestNothingThisVersionWritesIsMisreadByAnOlderOne(t *testing.T) {
	isolateHome(t)
	out := tbProfileArchive(t)
	g := newA3Rig(t, out, "darwin")
	jobAfter := func(what, body string, sync bool) {
		t.Helper()
		if r := g.post(body); !strings.Contains(r, `"ok":true`) {
			t.Fatalf("%s: %s", what, r)
		}
		g.recordLast()
		data := fileBytes(g.jobPath)
		v, err := v150ReadJob(data)
		if err != nil {
			t.Errorf("%s: v1.5.0's job reader misreads the job file: %v\n%s", what, err, data)
			return
		}
		if _, err := job.Read(g.jobPath); err != nil {
			t.Errorf("%s: job.Read refuses it: %v", what, err)
		}
		if (v.ThunderbirdSync != nil) != sync || !schedule.SameArchivePath(v.Out, out, "darwin") {
			t.Errorf("%s: v1.5.0 reads sync %v and folder %q; want sync %v and %s", what, v.ThunderbirdSync != nil, v.Out, sync, out)
		}
		var d v150Descriptor
		dec := json.NewDecoder(bytes.NewReader(fileBytes(filepath.Join(out, schedule.DescriptorName))))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&d); err != nil {
			t.Errorf("%s: v1.5.0 misreads the descriptor: %v", what, err)
		}
		if iv := d.Interval; iv != "hourly" && iv != "daily" && iv != "weekly" {
			t.Errorf("%s: the descriptor's interval %q is not one v1.5.0 parses", what, iv)
		}
	}
	jobAfter("a create, hourly with the sync on", `{"interval":"hourly","time":"00:15","presync":true}`, true)
	jobAfter("an in-place edit, the sync off", `{"interval":"hourly","time":"00:15","presync":false}`, false)
	jobAfter("an in-place edit, the cadence", `{"interval":"daily","time":"05:00","presync":true}`, true)
	if d, err := schedule.ReadDescriptor(out); err != nil || d.Interval != "daily" || d.At != "05:00" {
		t.Errorf("fixture: the last edit is not recorded (%+v, %v)", d, err)
	}

	// The source streak.
	t0 := time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC)
	st := state.SourceStreak{}.Observe(false, "missing", t0).Observe(false, "missing", t0.Add(4*24*time.Hour))
	if st.LastUnreachable.IsZero() {
		t.Fatal("fixture: the streak carries no last_unreachable")
	}
	if err := state.WriteSourceStreak(out, st); err != nil {
		t.Fatal(err)
	}
	data := fileBytes(filepath.Join(out, state.SourceStreakName))
	var old v150SourceStreak
	if err := json.Unmarshal(data, &old); err != nil {
		t.Fatalf("v1.5.0's plain json.Unmarshal fails on the source streak: %v\n%s", err, data)
	}
	if old.Version != 1 || old.ConsecutiveFails != 2 || !old.FirstUnreachable.Equal(t0) || old.LastKind != "missing" || len(data) > 4*1024 {
		t.Errorf("v1.5.0 misreads the source streak: %+v (%d bytes)\n%s", old, len(data), data)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(data, &keys); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"version": true, "consecutive_fails": true, "first_unreachable": true, "last_success": true, "last_kind": true}
	var unknown []string
	for k := range keys {
		if !known[k] {
			unknown = append(unknown, k)
		}
	}
	if len(unknown) != 1 || unknown[0] != "last_unreachable" {
		t.Errorf("the source streak's keys v1.5.0 has no field for are %v; want only last_unreachable", unknown)
	}

	// The alert streak, where a scheduled run keeps it: beside the default settings file, in the
	// job folder.
	sp := backupalert.DefaultSettingsPath()
	if filepath.Dir(sp) != filepath.Dir(g.jobPath) {
		t.Fatalf("fixture: the default settings file %s is not in the job folder %s", sp, filepath.Dir(g.jobPath))
	}
	key := backupalert.KeyFor(out)
	backupalert.Report(sp, nil, key, out, alert.ResultFailed, errors.New("source gone"), 0, nil)
	streak := backupalert.StreakPath(sp, key)
	raw := assure.Reached(t, fileBytes(streak), "the alert streak file a failed run wrote")
	if !bytes.Contains(raw, []byte(`"noticed":true`)) {
		t.Errorf("fixture: the alert streak carries no noticed field: %s", raw)
	}
	// Positive twins: each read matches the file it is for.
	for how, name := range map[string]string{"the job-folder scan (*.json)": filepath.Base(g.jobPath), "uninstall's list": filepath.Base(g.jobPath),
		"a run's failure log": key + ".jobfail.log", "a fixed file": "desktop-settings.json"} {
		if !v150ConfigFolderReads[how](name) {
			t.Fatalf("the positive twin: v1.5.0's %s does not match %s", how, name)
		}
	}
	for how, reads := range v150ConfigFolderReads {
		if reads(filepath.Base(streak)) {
			t.Errorf("v1.5.0's %s matches the alert streak file %s", how, filepath.Base(streak))
		}
	}
}
