package archdelete

import (
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-973
// §5.1's discovery asks the scheduler too (HostDiscovery): an entry this tool installed whose
// -out, read back where the host keeps it, is this folder is found, and its spelling joins the
// archive's; one backing up another folder is not; on macOS, which cannot read an entry back,
// a custom-named entry is "not removed: check it yourself" and a derived one is not; a
// scheduler that does not answer in time is said so, never taken for no entries. Positive
// twin first: with no entries installed, discovery finds only the files' own.
func TestHostDiscoveryAsksTheScheduler(t *testing.T) {
	folder, other := t.TempDir(), t.TempDir()
	base := HostDiscovery{Files: FileDiscovery{GOOS: "linux"}, Installed: func() []schedule.InstalledEntry { return nil }}
	d0, err := base.Discover(folder)
	if err != nil || len(d0.Entries) != 2 || len(d0.CheckYourself) != 0 {
		t.Fatalf("the positive twin: %+v %v", d0, err)
	}

	outs := map[string]string{"nightly-mine": folder + "/", "nightly-other": other}
	h := HostDiscovery{Files: FileDiscovery{GOOS: "linux"},
		Installed: func() []schedule.InstalledEntry {
			return []schedule.InstalledEntry{{Name: "nightly-mine"}, {Name: "nightly-other"}, {Name: "cron-mine", Out: folder}, {Name: "unknown"}}
		},
		Out: func(n string) (string, bool) { o, ok := outs[n]; return o, ok }}
	d, err := h.Discover(folder)
	if err != nil {
		t.Fatal(err)
	}
	names := strings.Join(d.Names(), ",")
	for _, want := range []string{"nightly-mine", "cron-mine"} {
		if !strings.Contains(names, want) {
			t.Errorf("discovery did not find %s: %s", want, names)
		}
	}
	if strings.Contains(names, "nightly-other") || strings.Contains(names, "unknown") {
		t.Errorf("discovery found another folder's entry: %s", names)
	}
	if !strings.Contains(strings.Join(d.Spellings, "|"), folder+"/") {
		t.Errorf("the entry's spelling is not among the archive's: %v", d.Spellings)
	}

	mac := h
	mac.Files.GOOS = "darwin"
	mac.Installed = func() []schedule.InstalledEntry {
		return []schedule.InstalledEntry{{Name: "custom-mac"}, {Name: schedule.DefaultNameFor(other)}}
	}
	mac.Out = func(string) (string, bool) { return "", false }
	dm, _ := mac.Discover(folder)
	if len(dm.CheckYourself) != 1 || !strings.Contains(dm.CheckYourself[0], "custom-mac") {
		t.Errorf("macOS custom-named entry: check yourself %v", dm.CheckYourself)
	}

	hung := base
	hung.Timeout = 30 * time.Millisecond
	hung.Installed = func() []schedule.InstalledEntry { time.Sleep(time.Second); return nil }
	dh, err := hung.Discover(folder)
	if err != nil || len(dh.CheckYourself) != 1 || dh.CheckYourself[0] != SchedulerSilentNote {
		t.Errorf("a scheduler that does not answer: %v %v", dh.CheckYourself, err)
	}
}

// covers: MA-973
// Each scheduler call has a time limit (§5.1): one that does not return in time is not waited
// on and is reported as not answered; one that does is its answer.
func TestASchedulerCallHasATimeLimit(t *testing.T) {
	if v, ok := timed(time.Second, func() int { return 7 }); !ok || v != 7 {
		t.Fatalf("a call that answers: %d %v", v, ok)
	}
	start := time.Now()
	if _, ok := timed(30*time.Millisecond, func() int { time.Sleep(2 * time.Second); return 1 }); ok {
		t.Fatal("a hung call was taken as answered")
	}
	if time.Since(start) > time.Second {
		t.Error("the time limit was not kept")
	}
}

// covers: MA-973
// The Desktop's own settings folders are protected as MailArchive's settings folder is
// (Protected.Settings, §7 protected row, C15): a folder that holds one is refused.
func TestTheDesktopsSettingsFoldersAreProtected(t *testing.T) {
	root := t.TempDir()
	inner := root + "/settings"
	if err := checkProtected(root, Protected{}); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if err := os.Mkdir(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	err := checkProtected(root, Protected{Settings: []string{inner}})
	if r := AsRefusal(err); r == nil || r.Case != CaseProtected || !strings.Contains(r.Message, inner) {
		t.Fatalf("a folder holding the Desktop's settings: %v", err)
	}
}
