package archdelete

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
)

func writeDescriptor(t *testing.T, dir, file string, d schedule.Descriptor) {
	t.Helper()
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, file), string(data))
}

func writeJob(t *testing.T, name, out string) string {
	t.Helper()
	p, err := job.PathFor(name)
	if err != nil {
		t.Fatal(err)
	}
	if err := job.Write(p, job.Job{Name: name, Out: out, Mode: "incremental"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(p) })
	return p
}

// covers: MA-962, R36
func TestFileDiscoveryFindsEveryEntryForTheFolder(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	def := schedule.DefaultNameFor(dir)
	// Positive twin first: nothing recorded, only the derived names.
	d, err := FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d := setDiff("names", d.Names(), []string{def, def + "-verify"}); d != "" {
		t.Fatal(d)
	}

	host, _ := os.Hostname()
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: def, Interval: "daily", At: "02:00", Job: []string{"-out", dir}, Host: host})
	// Two job files for this folder (one through another spelling), one for another
	// folder, and the Desktop's settings file, which is no job file.
	alias := filepath.Join(t.TempDir(), "alias")
	symlinkOrSkip(t, dir, alias)
	writeJob(t, "my-custom", dir)
	writeJob(t, "other-spelling", alias)
	writeJob(t, "elsewhere", t.TempDir())
	cfg, _ := job.Dir()
	writeFile(t, filepath.Join(cfg, "desktop-settings.json"), `{"version":1,"out":"`+filepath.ToSlash(dir)+`","mode":"incremental"}`)
	t.Cleanup(func() { os.Remove(filepath.Join(cfg, "desktop-settings.json")) })

	d, err = FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if d := setDiff("names", d.Names(), []string{def, def + "-verify", "my-custom", "other-spelling"}); d != "" {
		t.Fatal(d)
	}
	if !contains(d.Spellings, alias) {
		t.Fatalf("another spelling must be listed: %q", d.Spellings)
	}
	// The plan lists each discovered name's logs in the folder.
	writeFile(t, filepath.Join(dir, "my-custom.log"), "log")
	writeFile(t, filepath.Join(dir, "my-custom.stderr.log"), "log")
	writeFile(t, filepath.Join(dir, "someone-else.log"), "the person's log")
	deps := testDeps()
	deps.Discover = FileDiscovery{}
	p := mustPlan(t, dir, deps)
	for _, want := range []string{"my-custom.log", "my-custom.stderr.log", schedule.DescriptorName} {
		if !contains(removeRels(p), want) {
			t.Fatalf("%s must be removed: %q", want, removeRels(p))
		}
	}
	if !contains(p.Kept, "someone-else.log") {
		t.Fatalf("an undiscovered name's log is the person's: %q", p.Kept)
	}
}

// covers: MA-962, R36
func TestAnotherComputersAndUnconfirmedDescriptorsAreListedNotStopped(t *testing.T) {
	dir := smallArchive(t, t.TempDir())
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: "laptop-backup", Interval: "daily", At: "02:00", Job: []string{"-out", "/Volumes/USB/Mail"}, Host: "computer-b-not-this-one"})
	d, err := FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.OtherHost) != 1 || d.OtherHost[0].Text != "Computer computer-b-not-this-one also backs up into this folder; remove its schedule there" || contains(d.Names(), "laptop-backup") {
		t.Fatalf("other host: %+v names %q", d.OtherHost, d.Names())
	}

	// A descriptor copied from another archive that still exists on this computer.
	other := smallArchive(t, t.TempDir())
	host, _ := os.Hostname()
	writeDescriptor(t, dir, schedule.DescriptorName, schedule.Descriptor{Version: 1, Name: schedule.DefaultNameFor(other), Interval: "daily", At: "02:00", Job: []string{"-out", other}, Host: host})
	d, err = FileDiscovery{}.Discover(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Unconfirmed) != 1 || contains(d.Names(), schedule.DefaultNameFor(other)) {
		t.Fatalf("unconfirmed %+v names %q", d.Unconfirmed, d.Names())
	}
	want := filepath.Join(dir, schedule.DescriptorName) + " names the schedule " + schedule.DefaultNameFor(other) + ", which MailArchive cannot confirm is this archive's: not removed, check it yourself"
	if d.Unconfirmed[0].Text != want {
		t.Fatalf("note = %q, want %q", d.Unconfirmed[0].Text, want)
	}
}
