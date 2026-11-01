package archdelete

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/schedule"
)

// covers: MA-962, R36
// A copy of archive B, whose drive is unplugged, carries B's descriptor: schedule's
// binding reads B's missing folder as "this archive, moved from there", but for a
// delete it is never this archive's — listed as unconfirmed, never stopped, and B's
// folder is not one of this archive's spellings (del-s1a-A1-02).
func TestADescriptorCopiedFromAnUnpluggedArchiveIsNotStopped(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	unplugged := filepath.Join(t.TempDir(), "unplugged", "B")
	b := schedule.DefaultNameFor(unplugged)
	host, _ := os.Hostname()
	// Positive twin first: this archive's own descriptor is stopped.
	own := schedule.DefaultNameFor(dir)
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: own, Interval: "daily", At: "02:00", Job: []string{"-out", dir}, Host: host})
	d, err := FileDiscovery{}.Discover(dir)
	if err != nil || len(d.Unconfirmed) != 0 || !contains(d.Names(), own) {
		t.Fatalf("own descriptor: %+v %v", d, err)
	}

	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: b, Interval: "daily", At: "02:00", Job: []string{"-out", unplugged}, Host: host})
	d, err = FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if contains(d.Names(), b) || contains(d.Spellings, unplugged) {
		t.Fatalf("B's schedule must not be this archive's: names %q spellings %q", d.Names(), d.Spellings)
	}
	if len(d.Unconfirmed) != 1 || d.Unconfirmed[0].Name != b || !strings.HasSuffix(d.Unconfirmed[0].Text, "not removed, check it yourself") {
		t.Fatalf("unconfirmed %+v", d.Unconfirmed)
	}
}

// covers: MA-962, R36
// A descriptor that records this folder under another spelling (here a link to it;
// on Windows also another case, an 8.3 name or a mapped drive) with the name
// derived from that spelling is this archive's schedule: it is stopped, not left
// running as "check it yourself" (del-s1a-A3-03).
func TestADescriptorRecordingAnotherSpellingOfThisFolderIsStopped(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, dir, alias)
	name := schedule.DefaultNameFor(alias)
	host, _ := os.Hostname()
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: name, Interval: "daily", At: "02:00", Job: []string{"-out", alias}, Host: host})
	d, err := FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !contains(d.Names(), name) || len(d.Unconfirmed) != 0 || !contains(d.Spellings, alias) {
		t.Fatalf("names %q unconfirmed %+v spellings %q", d.Names(), d.Unconfirmed, d.Spellings)
	}
	// Positive twin of the listing: the same name for another existing folder is
	// still listed, not stopped.
	other := smallArchive(t, t.TempDir())
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: schedule.DefaultNameFor(other), Interval: "daily", At: "02:00", Job: []string{"-out", other}, Host: host})
	if d, _ := (FileDiscovery{}).Discover(dir); len(d.Unconfirmed) != 1 {
		t.Fatalf("another folder's: %+v", d)
	}
}

// covers: MA-962, R36
// The typed name is the folder's own name as its parent lists it, not the caller's
// spelling (another case on macOS and Windows; an 8.3 name on Windows)
// (del-s1a-A3-03).
func TestTheNameToTypeIsTheFoldersOwn(t *testing.T) {
	parent := t.TempDir()
	dir := smallArchive(t, filepath.Join(parent, "My Archive"))
	if p := mustPlan(t, dir, testDeps()); p.Name != "My Archive" { // positive twin
		t.Fatalf("name = %q", p.Name)
	}
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("another case names another folder here")
	}
	upper := filepath.Join(parent, "MY ARCHIVE")
	if _, err := os.Stat(upper); err != nil {
		t.Skipf("this volume does not ignore case: %v", err)
	}
	if p := mustPlan(t, upper, testDeps()); p.Name != "My Archive" {
		t.Fatalf("name = %q, want the folder's own", p.Name)
	}
}

// covers: MA-962, R36
// A descriptor's name MailArchive cannot confirm is still MailArchive's record, and
// so are the logs of that name in the folder: listed for removal with it, never
// called the person's (del-s1a-A3-06).
func TestTheLogsOfAnUnconfirmedNameAreTheArchives(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	other := smallArchive(t, t.TempDir())
	name := schedule.DefaultNameFor(other)
	host, _ := os.Hostname()
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: name, Interval: "daily", At: "02:00", Job: []string{"-out", other}, Host: host})
	for _, f := range []string{name + ".log", name + ".stderr.log", "someone-else.log"} {
		writeFile(t, filepath.Join(dir, f), "log")
	}
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	p := mustPlan(t, dir, deps)
	if len(p.Schedules.Unconfirmed) != 1 || contains(p.Schedules.Names(), name) {
		t.Fatalf("fixture: %+v", p.Schedules)
	}
	for _, want := range []string{name + ".log", name + ".stderr.log"} {
		if !contains(removeRels(p), want) {
			t.Fatalf("%s must be removed: %q", want, removeRels(p))
		}
	}
	if !contains(p.Kept, "someone-else.log") { // positive twin
		t.Fatalf("kept %q", p.Kept)
	}
}
