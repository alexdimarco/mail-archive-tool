package schedule

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// sandboxHostScheduler points Install, Remove and Query at stand-ins for the
// host scheduler: HOME is a temp dir and a fake launchctl is first on PATH for
// launchd, and the crontab seams keep the crontab in memory on cron hosts. It
// returns that crontab. There is no stand-in for schtasks, so the caller is
// skipped on Windows.
func sandboxHostScheduler(t *testing.T) *string {
	t.Helper()
	switch runtime.GOOS {
	case "darwin", "linux", "freebsd", "openbsd", "netbsd":
	default:
		t.Skip("the scheduler is sandboxed for launchd (HOME + a fake launchctl) and cron (the crontab seams) only")
	}
	t.Setenv("HOME", t.TempDir())
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "launchctl"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	if p, err := exec.LookPath("launchctl"); err != nil || filepath.Dir(p) != bin {
		t.Fatalf("launchctl resolves to %q (%v), not the sandbox fake", p, err)
	}
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	crontab := new(string)
	crontabList = func() (string, error) { return *crontab, nil }
	crontabInstall = func(c string) error { *crontab = c; return nil }
	return crontab
}

func backupSpecFor(out, name string) Spec {
	return Spec{Name: name, Interval: Daily, At: "02:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"-out", out, "-mode", "incremental", "-unattended"}, Out: out}
}

func verifySpecFor(out, name string) Spec {
	return Spec{Name: name, Interval: Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", out, "-unattended"}, Out: out}
}

// covers: MA-457, R14, R18, S14, S28
// A scheduled verify records its own descriptor (owner ruling FB-4): Install
// writes .mailarchive-verify-schedule.json beside the backup's descriptor with
// the same discipline — version 1; the verify's name, cadence, time, program
// and job; a 0600 (owner-only) file renamed into place, no temp left behind —
// and leaves the backup's descriptor byte-identical, as a backup install leaves
// the verify's absent. Each descriptor goes only with its own entry: removing
// the backup deletes the backup's descriptor and keeps the verify's
// byte-identical (the verify entry still installed), and removing the verify
// deletes the verify's and keeps the backup's. A verify the descriptor records
// but the scheduler no longer has (taken out by hand) still counts as present,
// so its removal clears the stale record. A verify descriptor that cannot be
// read (bad JSON) is never deleted, with either entry, and
// UnreadableVerifyDescriptor names it each time. The scheduler is sandboxed
// (temp HOME and a fake launchctl first on PATH, or the crontab seams).
func TestVerifyDescriptorGoesOnlyWithItsOwnEntry(t *testing.T) {
	crontab := sandboxHostScheduler(t)
	out := t.TempDir()
	backup := backupSpecFor(out, DefaultNameFor(out))
	verify := verifySpecFor(out, DefaultNameFor(out)+"-verify")
	backupPath, verifyPath := filepath.Join(out, DescriptorName), filepath.Join(out, VerifyDescriptorName)
	read := func(p string) string {
		t.Helper()
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return string(b)
	}
	gone := func(p string) bool { _, err := os.Lstat(p); return errors.Is(err, fs.ErrNotExist) }
	install := func(s Spec) {
		t.Helper()
		if err := Install(s); err != nil { // the sandboxed launchctl / crontab seams
			t.Fatal(err)
		}
		assure.Reached(t, Query(s.Name) == Installed, "fixture: "+s.Name+" installed")
	}
	remove := func(s Spec) {
		t.Helper()
		removed, err := RemoveIfInstalled(s)
		if err != nil {
			t.Fatal(err)
		}
		assure.Reached(t, removed, "removal of "+s.Name)
		if Query(s.Name) == Installed {
			t.Errorf("%s is still installed after its removal", s.Name)
		}
	}

	install(backup)
	if !gone(verifyPath) {
		t.Errorf("a backup install wrote the verify descriptor")
	}
	backupBytes := read(backupPath)

	install(verify)
	d, err := ReadVerifyDescriptor(out)
	if err != nil {
		t.Fatalf("the verify install left no readable verify descriptor: %v", err)
	}
	if d.Name != verify.Name || d.Interval != "weekly" || d.At != "05:00" || d.Exe != verify.Exe ||
		strings.Join(d.Job, " ") != strings.Join(verify.Args, " ") || d.InstalledAt.IsZero() || d.Host == "" {
		t.Errorf("the verify descriptor does not record the verify schedule: %+v", d)
	}
	var doc map[string]any
	if err := json.Unmarshal([]byte(read(verifyPath)), &doc); err != nil || doc["version"] != float64(1) {
		t.Errorf("the verify descriptor carries no version 1 (%v): %v", err, doc)
	}
	for _, p := range []string{verifyPath, backupPath} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != 0o600 {
			t.Errorf("%s is not an owner-only (0600) file: %v %v", p, fi.Mode(), err)
		}
	}
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("a descriptor write left its temp file behind: %s", e.Name())
		}
	}
	if read(backupPath) != backupBytes {
		t.Errorf("installing the verify changed the backup's descriptor")
	}

	// The backup's removal takes only the backup's descriptor.
	verifyBytes := read(verifyPath)
	remove(backup)
	if !gone(backupPath) {
		t.Errorf("the backup's descriptor outlived the backup's entry")
	}
	if gone(verifyPath) || read(verifyPath) != verifyBytes {
		t.Errorf("removing the backup changed or deleted the verify's descriptor")
	}
	if Query(verify.Name) != Installed {
		t.Errorf("removing the backup removed the verify's entry")
	}

	// The verify's removal takes only the verify's descriptor.
	install(backup)
	backupBytes = read(backupPath)
	remove(verify)
	if !gone(verifyPath) {
		t.Errorf("the verify's descriptor outlived the verify's entry")
	}
	if read(backupPath) != backupBytes || Query(backup.Name) != Installed {
		t.Errorf("removing the verify changed the backup's descriptor or entry")
	}

	// A verify recorded but gone from the scheduler (taken out by hand) is still
	// the archive's: its removal clears the stale record, as the backup's does.
	install(verify)
	if runtime.GOOS == "darwin" {
		if err := os.Remove(launchdPlistPath(verify.Name)); err != nil {
			t.Fatal(err)
		}
	} else {
		*crontab = RemoveCronBlock(*crontab, CronMarker(verify.Name))
	}
	assure.Reached(t, Query(verify.Name) != Installed, "fixture: the verify's entry taken out by hand")
	removed, err := RemoveIfInstalled(verify)
	if err != nil {
		t.Fatal(err)
	}
	if !removed || !gone(verifyPath) {
		t.Errorf("a verify recorded but gone from the scheduler was not cleared (removed=%v, descriptor gone=%v)", removed, gone(verifyPath))
	}
	if read(backupPath) != backupBytes || Query(backup.Name) != Installed {
		t.Errorf("clearing the verify's stale record changed the backup's descriptor or entry")
	}

	// A verify descriptor that cannot be read goes with neither entry.
	install(verify)
	broken := "{\"version\":1,\"name\":" // a hand edit cut short
	if err := os.WriteFile(verifyPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadVerifyDescriptor(out); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("fixture: the verify descriptor should exist and be unreadable, got %v", err)
	}
	for _, s := range []Spec{backup, verify} {
		remove(s)
		if gone(verifyPath) || read(verifyPath) != broken {
			t.Errorf("removing %s changed or deleted the unreadable verify descriptor, which records no name -remove can trust", s.Name)
		}
		if got := UnreadableVerifyDescriptor(out); got != verifyPath {
			t.Errorf("after removing %s, UnreadableVerifyDescriptor = %q, want %s", s.Name, got, verifyPath)
		}
	}
}

// covers: MA-456, R14, S28
// OPEN RESIDUAL ENCODER (R14; owner ruling FB-4: an open residual, not
// accepted). The one-archive-per-name guard should refuse a second archive's
// verify under a -name the first archive's verify already holds on every OS, as
// it does on cron hosts (MA-456). On launchd and Task Scheduler hosts it cannot:
// their entries are never read back (UJ-6, upheld by FB-4), and an archive's
// own descriptors know nothing of another archive's names. So the second
// install replaces the first's entry, and what stays visible is the first
// archive's verify_stale (MA-466). Closing this would read those entries back,
// which UJ-6 rules out, so the encoder stays skipped. On a launchd host
// (sandboxed: temp HOME, fake launchctl first on PATH) it installs archive A's
// verify under the shared name the old README passed; the positive twin is
// archive B's verify under its own derived name; then it wants B's verify under
// the shared name refused, naming the name, A and the -name remedy, with A's
// entry (its plist) unchanged.
func TestSharedVerifyNameAcrossArchivesIsRefusedOnLaunchd(t *testing.T) {
	t.Skip("open residual, not accepted (owner ruling FB-4): launchd and Task Scheduler entries are never read back (UJ-6), so a second archive's verify under a -name the first archive's verify holds is not detected at install there; the archive that lost its verify shows verify_stale (MA-466). Closing it would reopen UJ-6.")
	if runtime.GOOS != "darwin" {
		t.Skip("shown on a launchd host: cron hosts already refuse it (MA-456), and Task Scheduler has no sandbox here")
	}
	sandboxHostScheduler(t)
	const shared = "mailarchive-verify"
	a, b := t.TempDir(), t.TempDir()
	for _, s := range []Spec{verifySpecFor(a, shared), verifySpecFor(b, DefaultNameFor(b)+"-verify")} {
		if err := Install(s); err != nil {
			t.Fatal(err)
		}
		assure.Reached(t, Query(s.Name) == Installed, "fixture: "+s.Name+" installed")
	}
	plist := func() string {
		data, _ := os.ReadFile(launchdPlistPath(shared))
		return string(data)
	}
	was := assure.Reached(t, plist(), "archive A's verify entry")
	err := Install(verifySpecFor(b, shared))
	code, msg := 0, ""
	if err != nil {
		code, msg = 1, err.Error()
	}
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(shared, a, "-name"),
		assure.NoSideEffect(func() bool { return plist() == was }))
}

// covers: MA-457, R14, S28
// On every OS the name guard reads the archive's own two descriptors (owner
// ruling FB-4), so neither of the archive's entries can take the name the other
// holds: a verify under the name the backup's descriptor records would replace
// the backup's entry, and a backup under the name the verify's descriptor
// records would replace the verify's. Each is refused for darwin, windows and
// linux alike, naming the name, the archive and the -name remedy, with nothing
// written. It is the descriptors that refuse: the crontab is empty, and
// launchd and Task Scheduler entries are never read back (UJ-6). The positive
// twins come first: each entry re-installs under its own name, the verify may
// move to another name of its own, and a descriptor an older release wrote for
// the verify itself is that verify's own record, not a backup's name. Through
// Install on this host (sandboxed), the refused verify leaves the backup's
// entry, both descriptors and the scheduler as they were.
func TestNameGuardReadsTheArchivesOwnDescriptors(t *testing.T) {
	origList, origInstall := crontabList, crontabInstall
	t.Cleanup(func() { crontabList, crontabInstall = origList, origInstall })
	wrote := false
	crontabList = func() (string, error) { return "", nil }
	crontabInstall = func(string) error { wrote = true; return nil }

	const backupName, verifyName = "nightly-ma457", "weekly-verify-ma457"
	// Archive a records both entries; archive b only its verify (its backup was
	// removed); archive c a verify in its backup descriptor, as an older release
	// wrote it.
	a, b, c := t.TempDir(), t.TempDir(), t.TempDir()
	for _, w := range []error{
		WriteDescriptor(a, DescribeSpec(backupSpecFor(a, backupName))),
		WriteVerifyDescriptor(a, DescribeSpec(verifySpecFor(a, verifyName))),
		WriteVerifyDescriptor(b, DescribeSpec(verifySpecFor(b, verifyName))),
		WriteDescriptor(c, DescribeSpec(verifySpecFor(c, "mailarchive-verify"))),
	} {
		if w != nil {
			t.Fatal(w)
		}
	}
	snapshot := func() string {
		var s strings.Builder
		for _, dir := range []string{a, b, c} {
			for _, name := range []string{DescriptorName, VerifyDescriptorName} {
				data, _ := os.ReadFile(filepath.Join(dir, name))
				s.WriteString(name + "=" + string(data) + "\n")
			}
		}
		return s.String()
	}
	before := snapshot()
	refused := func(err error) (int, string) {
		if err != nil {
			return 1, err.Error()
		}
		return 0, ""
	}

	for _, goos := range []string{"darwin", "windows", "linux"} {
		t.Run(goos, func(t *testing.T) {
			for _, s := range []Spec{
				backupSpecFor(a, backupName),
				verifySpecFor(a, verifyName),
				verifySpecFor(a, DefaultNameFor(a)+"-verify"),
				backupSpecFor(b, DefaultNameFor(b)),
				verifySpecFor(c, "mailarchive-verify"),
			} {
				if err := refuseCollision(s, goos); err != nil {
					t.Errorf("%v under %q for its own archive was refused: %v", s.Args, s.Name, err)
				}
			}

			code, msg := refused(refuseCollision(verifySpecFor(a, backupName), goos))
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(backupName, a, "backup schedule", "-name"),
				assure.NoSideEffect(func() bool { return !wrote && snapshot() == before }))

			code, msg = refused(refuseCollision(backupSpecFor(b, verifyName), goos))
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(verifyName, b, "verify schedule", "-name"),
				assure.NoSideEffect(func() bool { return !wrote && snapshot() == before }))
		})
	}

	t.Run("Install", func(t *testing.T) {
		crontab := sandboxHostScheduler(t)
		out := t.TempDir()
		backup, verify := backupSpecFor(out, backupName), verifySpecFor(out, verifyName)
		for _, s := range []Spec{backup, verify} { // the positive twin: side by side
			if err := Install(s); err != nil {
				t.Fatal(err)
			}
			assure.Reached(t, Query(s.Name) == Installed, "fixture: "+s.Name+" installed")
		}
		state := func() string {
			entry := *crontab
			if runtime.GOOS == "darwin" {
				data, _ := os.ReadFile(launchdPlistPath(backupName))
				entry = string(data)
			}
			bd, _ := os.ReadFile(filepath.Join(out, DescriptorName))
			vd, _ := os.ReadFile(filepath.Join(out, VerifyDescriptorName))
			return entry + "\x00" + string(bd) + "\x00" + string(vd)
		}
		assure.Reached(t, state(), "the backup's entry and both descriptors")
		was := state()
		code, msg := refused(Install(verifySpecFor(out, backupName)))
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(backupName, out, "-name"),
			assure.NoSideEffect(func() bool { return state() == was && Query(backupName) == Installed }))
	})
}
