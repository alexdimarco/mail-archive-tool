package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/schedule"
)

// sandboxScheduler points HOME at a temp dir (launchd plists land under it) and
// puts fake `launchctl` and `crontab` first on PATH, so `schedule -install` and
// `-remove` run end to end through the real binary without touching the
// developer's launchd or crontab. The fakes log their argv and keep the
// crontab in a file. The test process gets the same environment, so
// schedule.Query here sees what the binary installed.
func sandboxScheduler(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake scheduler binaries are POSIX sh scripts; the -remove name resolution under test is OS-independent")
	}
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skip("no /bin/sh for the fake scheduler binaries")
	}
	home, bin := t.TempDir(), t.TempDir()
	fakes := map[string]string{
		"launchctl": "#!/bin/sh\necho \"$@\" >> \"$FAKE_SCHED_LOG\"\nexit 0\n",
		"crontab": "#!/bin/sh\ncase \"$1\" in\n" +
			"-l) [ -f \"$FAKE_CRONTAB\" ] || { echo \"no crontab for $USER\" >&2; exit 1; }; cat \"$FAKE_CRONTAB\" ;;\n" +
			"-) cat > \"$FAKE_CRONTAB\" ;;\n*) exit 2 ;;\nesac\n",
	}
	for name, body := range fakes {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+"/usr/bin"+string(os.PathListSeparator)+"/bin")
	t.Setenv("FAKE_SCHED_LOG", filepath.Join(home, "launchctl.log"))
	t.Setenv("FAKE_CRONTAB", filepath.Join(home, "crontab"))
	// Never fall through to the real scheduler: both names must resolve to the fakes.
	for name := range fakes {
		if p, err := exec.LookPath(name); err != nil || filepath.Dir(p) != bin {
			t.Fatalf("%s resolves to %q (%v), not the sandbox fake in %s", name, p, err, bin)
		}
	}
}

// takeOutOfSandbox removes an entry from the scheduler sandboxScheduler set up,
// by hand, as an operator might in launchd or crontab, leaving the archive's
// records alone.
func takeOutOfSandbox(t *testing.T, name string) {
	t.Helper()
	if runtime.GOOS == "darwin" {
		if err := os.Remove(filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", schedule.LaunchdLabel(name)+".plist")); err != nil {
			t.Fatal(err)
		}
		return
	}
	tab, err := os.ReadFile(os.Getenv("FAKE_CRONTAB"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(os.Getenv("FAKE_CRONTAB"), []byte(schedule.RemoveCronBlock(string(tab), schedule.CronMarker(name))), 0o600); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-454, MA-455, R14, R18, S14, S28
// `-remove` reverses exactly the entry it names and leaves every other one
// alone. An archive with a nightly backup and a weekly verify (two entries; the
// archive's descriptor names the backup): `-remove -- verify -out X` — the
// install command with -install swapped for -remove — removes the VERIFY entry
// and keeps the backup and its descriptor; `-name <verify> -out X -remove` does
// the same; `-name <never installed> -out X -remove` removes nothing, says so,
// and keeps the descriptor; and the plain `-out X -remove` (the positive twin)
// still removes the backup and its descriptor.
func TestScheduleRemoveTargetsTheNamedEntry(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	backup := schedule.DefaultNameFor(out)
	verify := backup + "-verify"

	install := func() {
		t.Helper()
		for _, args := range [][]string{
			{"schedule", "-install", "--", "-out", out, "-auto"},
			{"schedule", "-interval", "weekly", "-at", "05:00", "-install", "--", "verify", "-out", out},
		} {
			if code, _, stderr := runCLIOut(t, args...); code != 0 {
				t.Fatalf("%v exited %d: %s", args, code, stderr)
			}
		}
		if schedule.Query(backup) != schedule.Installed || schedule.Query(verify) != schedule.Installed {
			t.Fatalf("fixture: backup=%v verify=%v, want both installed", schedule.Query(backup), schedule.Query(verify))
		}
	}
	descriptorName := func() string {
		d, err := schedule.ReadDescriptor(out)
		if err != nil {
			return ""
		}
		return d.Name
	}
	expect := func(step string, wantBackup, wantVerify bool, wantDesc string) {
		t.Helper()
		if got := schedule.Query(backup) == schedule.Installed; got != wantBackup {
			t.Errorf("%s: backup entry installed=%v, want %v", step, got, wantBackup)
		}
		if got := schedule.Query(verify) == schedule.Installed; got != wantVerify {
			t.Errorf("%s: verify entry installed=%v, want %v", step, got, wantVerify)
		}
		if got := descriptorName(); got != wantDesc {
			t.Errorf("%s: descriptor names %q, want %q", step, got, wantDesc)
		}
	}

	install()
	assure.Reached(t, descriptorName(), "the backup's descriptor")
	code, stdout, stderr := runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", out)
	if code != 0 || !strings.Contains(stdout, verify) {
		t.Errorf("remove -- verify: exit %d, want it to name %q:\n%s%s", code, verify, stdout, stderr)
	}
	expect("remove -- verify -out X", true, false, backup)

	install()
	code, stdout, stderr = runCLIOut(t, "schedule", "-name", verify, "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, verify) {
		t.Errorf("remove -name verify: exit %d, want it to name %q:\n%s%s", code, verify, stdout, stderr)
	}
	expect("remove -name <verify> -out X", true, false, backup)

	install()
	code, stdout, stderr = runCLIOut(t, "schedule", "-name", "typo-name", "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, "nothing to remove") || !strings.Contains(stdout, "typo-name") || strings.Contains(stdout, "Removed") {
		t.Errorf("remove -name typo: exit %d, want a 'nothing to remove' naming typo-name:\n%s%s", code, stdout, stderr)
	}
	expect("remove -name <never installed> -out X", true, true, backup)

	// Positive twin: the archive's own backup is removed with its descriptor.
	code, stdout, stderr = runCLIOut(t, "schedule", "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, backup) {
		t.Errorf("remove -out: exit %d, want it to name %q:\n%s%s", code, backup, stdout, stderr)
	}
	expect("remove -out X", false, true, "")
}

// covers: MA-455, R14, R18, S14
// A descriptor that cannot be read goes with the archive's default-named
// backup only: `-out X -remove` deletes it with that entry (568c992). Under a
// custom -name nothing in it says it was that backup's — `schedule` writes it
// for a backup under any -name — so `-name NAME -out X -remove` leaves it, names
// its path and the remedy, and `status` keeps reporting it. Through the real
// binary (sandboxed scheduler): a backup under a custom -name has its
// descriptor cut short — or, in a second leg, rewritten with a name outside the
// -name grammar — and is removed: the entry is gone, the descriptor is
// byte-identical, stdout names the file, "delete it" and -install, and `status`
// (on a folder with no manifest or run record yet) reports the file, the
// reader's own cause ("is unreadable", "names an invalid schedule") and its
// remedy instead of refusing it as "no archive" or saying no schedule is
// recorded. A backup under the archive's default name, its descriptor cut
// short, is removed by `-out X -remove` together with the descriptor, and
// nothing is named as left behind. The positive twin: with a readable
// descriptor the custom-name removal deletes it and names nothing left behind.
func TestScheduleRemoveNamesAnUnreadableDescriptorItLeaves(t *testing.T) {
	sandboxScheduler(t)
	const custom = "nightly-mail-ma455"
	broken := []byte("{\"version\":1,\"name\":") // a hand edit cut short
	install := func(t *testing.T, out, name string) {
		t.Helper()
		args := []string{"schedule", "-install", "--", "-out", out, "-auto"}
		if name != "" {
			args = append([]string{"schedule", "-name", name}, args[1:]...)
		}
		if code, _, stderr := runCLIOut(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, stderr)
		}
	}

	// Positive twin: a readable descriptor goes with the schedule it records.
	out := t.TempDir()
	install(t, out, custom)
	if d, err := schedule.ReadDescriptor(out); err != nil || d.Name != custom {
		t.Fatalf("fixture: the descriptor should record %s, got %+v (%v)", custom, d, err)
	}
	code, stdout, stderr := runCLIOut(t, "schedule", "-name", custom, "-out", out, "-remove")
	if code != 0 || !strings.Contains(stdout, "Removed scheduled backup") {
		t.Fatalf("remove -name %s: exit %d:\n%s%s", custom, code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(out, schedule.DescriptorName)); !os.IsNotExist(err) {
		t.Errorf("the readable descriptor outlived the schedule it records (stat: %v)", err)
	}
	if strings.Contains(stdout, "Left in place") {
		t.Errorf("a removal that left nothing behind names a leftover:\n%s", stdout)
	}

	cutShort := func(string) []byte { return broken }
	// A hand edit that keeps the JSON but puts a name outside the -name grammar.
	invalidName := func(out string) []byte {
		return []byte(fmt.Sprintf(`{"version":1,"name":"bad name","interval":"daily","at":"02:00","exe":"/usr/local/bin/mailarchive","job":["-out",%q]}`, out))
	}
	removeCustom := func(out string) []string { return []string{"schedule", "-name", custom, "-out", out, "-remove"} }
	for _, leg := range []struct {
		name    string
		install string // -name at install ("" = the archive's default)
		damage  func(out string) []byte
		cause   string // the reader's own wording, which status prints
		remove  func(out string) []string
		goes    bool // the unreadable descriptor goes with the removal
	}{
		{"custom -name", custom, cutShort, "is unreadable", removeCustom, false},
		{"custom -name, invalid name", custom, invalidName, "names an invalid schedule", removeCustom, false},
		{"default -name", "", cutShort, "is unreadable", func(out string) []string { return []string{"schedule", "-out", out, "-remove"} }, true},
	} {
		t.Run(leg.name, func(t *testing.T) {
			out := t.TempDir()
			install(t, out, leg.install)
			name := leg.install
			if name == "" {
				name = schedule.DefaultNameFor(out)
			}
			assure.Reached(t, schedule.Query(name) == schedule.Installed, "fixture: "+name+" installed")
			path := filepath.Join(out, schedule.DescriptorName)
			broken := leg.damage(out)
			if err := os.WriteFile(path, broken, 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := schedule.ReadDescriptor(out); err == nil || !strings.Contains(err.Error(), leg.cause) {
				t.Fatalf("fixture: the descriptor should fail to read with %q, got %v", leg.cause, err)
			}

			code, stdout, stderr := runCLIOut(t, leg.remove(out)...)
			if code != 0 || !strings.Contains(stdout, "Removed scheduled backup") || !strings.Contains(stdout, name) {
				t.Errorf("%v: exit %d, want it to remove %s:\n%s%s", leg.remove(out), code, name, stdout, stderr)
			}
			if schedule.Query(name) == schedule.Installed {
				t.Errorf("the backup %s is still installed", name)
			}
			if leg.goes {
				// The archive's default-named schedule: the descriptor goes with it.
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Errorf("an unreadable descriptor outlived the archive's default-named schedule, so status and -install still point at a schedule that is gone (stat: %v)", err)
				}
				if strings.Contains(stdout, "Left in place") {
					t.Errorf("a removal that took the descriptor with it names a leftover:\n%s", stdout)
				}
				return
			}
			if after, err := os.ReadFile(path); err != nil || string(after) != string(broken) {
				t.Errorf("the removal changed or deleted the unreadable descriptor, which records no name it can trust (%v)", err)
			}
			for _, want := range []string{"Left in place", path, "delete it", "-install"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("the removal does not name the descriptor it left and the remedy (lacks %q):\n%s", want, stdout)
				}
			}

			// status keeps naming it, although the folder has no manifest or run record.
			code, stdout, stderr = runCLIOut(t, "status", "-out", out)
			if code != 0 {
				t.Fatalf("status refused the folder holding the leftover descriptor (exit %d): %s", code, stderr)
			}
			for _, want := range []string{path, "unreadable", leg.cause, "delete it"} {
				if !strings.Contains(stdout, want) {
					t.Errorf("status does not name the leftover descriptor and its remedy (lacks %q):\n%s", want, stdout)
				}
			}
			if strings.Contains(stdout, "Schedule:   none recorded") {
				t.Errorf("status says no schedule is recorded beside a descriptor it cannot read:\n%s", stdout)
			}
		})
	}
}

// covers: MA-457, R14, R18, S14, S28
// Through the real binary (sandboxed scheduler), a scheduled verify records its
// own descriptor and each descriptor goes only with its own entry (owner ruling
// FB-4). `schedule -install -- verify -out X` beside a backup writes
// .mailarchive-verify-schedule.json recording the verify, names that file on
// its Log line, and leaves the backup's descriptor byte-identical.
// `-out X -remove` removes the backup with its descriptor and keeps the
// verify's entry and descriptor; `-remove -- verify -out X` removes the verify
// with its descriptor. A verify installed under its own -name is recorded under
// that name, and `-name NAME -out X -remove` reports it as the verify it is and
// deletes its descriptor, keeping the backup. A verify under the backup's name
// is refused (exit 1, naming the name, "backup schedule" and the -name remedy)
// with the backup's entry, its descriptor and the absent verify descriptor all
// unchanged — the positive twin being the same verify under its own name,
// installed above. A verify descriptor cut short is never deleted: the verify's
// removal names it and its remedy.
func TestScheduleVerifyRecordsItsOwnDescriptor(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	backup := schedule.DefaultNameFor(out)
	derived := backup + "-verify"
	const custom = "weekly-verify-ma457"
	descPath, verifyPath := filepath.Join(out, schedule.DescriptorName), filepath.Join(out, schedule.VerifyDescriptorName)
	run := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := runCLIOut(t, args...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
		return stdout
	}
	read := func(p string) string { // "" when absent
		data, err := os.ReadFile(p)
		if err != nil {
			return ""
		}
		return string(data)
	}
	recorded := func() string {
		d, err := schedule.ReadVerifyDescriptor(out)
		if err != nil {
			return ""
		}
		return d.Name
	}
	// hostEntry is the backup's entry as the sandboxed scheduler holds it.
	hostEntry := func() string {
		if runtime.GOOS == "darwin" {
			return read(filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", schedule.LaunchdLabel(backup)+".plist"))
		}
		return read(os.Getenv("FAKE_CRONTAB"))
	}
	verifyArgs := func(name string) []string {
		args := []string{"schedule", "-interval", "weekly", "-at", "05:00"}
		if name != "" {
			args = append(args, "-name", name)
		}
		return append(args, "-install", "--", "verify", "-out", out)
	}

	run("schedule", "-install", "--", "-out", out, "-auto")
	backupBytes := assure.Reached(t, read(descPath), "the backup's descriptor")
	stdout := run(verifyArgs("")...)
	if !strings.Contains(stdout, "descriptor: "+verifyPath) {
		t.Errorf("the verify install does not name its own descriptor:\n%s", stdout)
	}
	if got := recorded(); got != derived {
		t.Errorf("the verify descriptor records %q, want %q", got, derived)
	}
	if read(descPath) != backupBytes {
		t.Errorf("installing the verify changed the backup's descriptor")
	}

	// The backup's removal keeps the verify's entry and descriptor.
	verifyBytes := read(verifyPath)
	run("schedule", "-out", out, "-remove")
	if read(descPath) != "" || read(verifyPath) != verifyBytes || schedule.Query(derived) != schedule.Installed {
		t.Errorf("-out X -remove: want the backup's descriptor gone and the verify's entry (%v) and descriptor kept", schedule.Query(derived))
	}
	// The verify's removal takes the verify's descriptor.
	stdout = run("schedule", "-remove", "--", "verify", "-out", out)
	if !strings.Contains(stdout, "Removed scheduled verify") || read(verifyPath) != "" || schedule.Query(derived) == schedule.Installed {
		t.Errorf("-remove -- verify -out X: want the verify's entry and descriptor gone:\n%s", stdout)
	}

	// Under its own -name: recorded so, and removed by -name as the verify.
	run("schedule", "-install", "--", "-out", out, "-auto")
	run(verifyArgs(custom)...)
	if got := recorded(); got != custom {
		t.Errorf("a verify installed with -name %s is recorded as %q", custom, got)
	}
	stdout = run("schedule", "-name", custom, "-out", out, "-remove")
	if !strings.Contains(stdout, `Removed scheduled verify "`+custom+`"`) || read(verifyPath) != "" || schedule.Query(custom) == schedule.Installed {
		t.Errorf("-name %s -out X -remove: want the verify reported, its entry and descriptor gone:\n%s", custom, stdout)
	}
	if schedule.Query(backup) != schedule.Installed || read(descPath) == "" {
		t.Errorf("removing the custom-named verify removed the backup or its descriptor")
	}

	// A verify under the backup's own name would replace the backup: refused.
	backupBytes, entry := read(descPath), hostEntry()
	assure.Reached(t, entry, "the backup's entry in the sandboxed scheduler")
	code, _, stderr := runCLIOut(t, verifyArgs(backup)...)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names(backup, "backup schedule", "-name"),
		assure.NoSideEffect(func() bool {
			return read(descPath) == backupBytes && read(verifyPath) == "" && hostEntry() == entry && schedule.Query(backup) == schedule.Installed
		}))

	// A verify descriptor cut short: never deleted, named with its remedy.
	run(verifyArgs("")...)
	broken := "{\"version\":1,\"name\":" // a hand edit cut short
	if err := os.WriteFile(verifyPath, []byte(broken), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout = run("schedule", "-remove", "--", "verify", "-out", out)
	if read(verifyPath) != broken {
		t.Errorf("the verify's removal changed or deleted its unreadable descriptor")
	}
	for _, want := range []string{"Removed scheduled verify", "Left in place", verifyPath, "delete it if no verify schedule", "-install -- verify"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the verify's removal does not name the descriptor it left and the remedy (lacks %q):\n%s", want, stdout)
		}
	}
}

// covers: MA-457, R14, S14, S28
// A verify renamed by -name leaves its earlier entry running, and the install
// names it. Install rewrites the verify descriptor for the new name and never
// removes the old entry (on launchd and Task Scheduler nothing can say the old
// name is still this archive's, UJ-6), so once the descriptor moves on nothing
// records it: the install's note names it with the command that removes it.
// Through the real binary (sandboxed scheduler), the positive twins come first:
// a first install and a re-install under the same name print no note. Then the
// verify is installed under -name v2: the derived entry stays installed, and the
// note names it with `mailarchive schedule -name <derived> -remove -- verify
// -out X`; followed, that removes only the derived entry, keeping v2's entry,
// its descriptor (byte-identical) and the backup. Renamed again (v3), the note
// names v2, and followed it removes v2. An old entry already gone from the
// scheduler (taken out by hand) is named by no note: nothing is left running.
func TestScheduleVerifyRenameNamesTheEntryItLeaves(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	backup := schedule.DefaultNameFor(out)
	derived := backup + "-verify"
	const v2, v3 = "v2-ma457", "v3-ma457"
	const note = "Note: this archive's verify was installed as"
	verifyPath := filepath.Join(out, schedule.VerifyDescriptorName)
	run := func(args ...string) string {
		t.Helper()
		code, stdout, stderr := runCLIOut(t, args...)
		if code != 0 {
			t.Fatalf("%v exited %d: %s%s", args, code, stdout, stderr)
		}
		return stdout
	}
	install := func(name string) string {
		t.Helper()
		args := []string{"schedule", "-interval", "weekly", "-at", "05:00"}
		if name != "" {
			args = append(args, "-name", name)
		}
		return run(append(args, "-install", "--", "verify", "-out", out)...)
	}
	removal := func(name string) string { // the command the note gives
		return fmt.Sprintf("mailarchive schedule -name %s -remove -- verify -out %q", name, out)
	}
	installed := func(name string) bool { return schedule.Query(name) == schedule.Installed }
	recorded := func() string {
		d, err := schedule.ReadVerifyDescriptor(out)
		if err != nil {
			return ""
		}
		return d.Name
	}

	run("schedule", "-install", "--", "-out", out, "-auto")
	// Positive twins: nothing is left behind, so nothing is named.
	for _, step := range []string{"a first install", "a re-install under the same name"} {
		if stdout := install(""); strings.Contains(stdout, note) {
			t.Errorf("%s names a superseded verify:\n%s", step, stdout)
		}
	}

	// Renamed: the derived entry keeps running, and the note names it.
	stdout := install(v2)
	if got := recorded(); got != v2 {
		t.Fatalf("the verify descriptor records %q after the install under -name %s", got, v2)
	}
	if !installed(derived) || !installed(v2) {
		t.Fatalf("fixture: derived=%v v2=%v, want both installed (the install removes nothing)", installed(derived), installed(v2))
	}
	for _, want := range []string{note, `"` + derived + `"`, "still installed", removal(derived)} {
		if !strings.Contains(stdout, want) {
			t.Errorf("renaming the verify to %s does not name the entry it leaves running (lacks %q):\n%s", v2, want, stdout)
		}
	}
	// The note followed removes only that entry.
	before, err := os.ReadFile(verifyPath)
	if err != nil {
		t.Fatal(err)
	}
	run("schedule", "-name", derived, "-remove", "--", "verify", "-out", out)
	after, err := os.ReadFile(verifyPath)
	if installed(derived) || !installed(v2) || !installed(backup) || err != nil || string(after) != string(before) {
		t.Errorf("following the note: derived=%v v2=%v backup=%v, verify descriptor unchanged=%v (%v); want only the derived entry gone",
			installed(derived), installed(v2), installed(backup), string(after) == string(before), err)
	}

	// Renamed again: the note names v2, and followed it removes v2.
	if stdout := install(v3); !strings.Contains(stdout, removal(v2)) {
		t.Errorf("renaming the verify to %s does not name %s, which it leaves running:\n%s", v3, v2, stdout)
	}
	run("schedule", "-name", v2, "-remove", "--", "verify", "-out", out)
	if installed(v2) || !installed(v3) || recorded() != v3 {
		t.Errorf("following the second note: v2=%v v3=%v recorded=%q; want only v3 left", installed(v2), installed(v3), recorded())
	}

	// An old entry already gone from the scheduler: nothing left running, no note.
	takeOutOfSandbox(t, v3)
	if stdout := install(""); strings.Contains(stdout, note) {
		t.Errorf("an install names %s, which the scheduler no longer has:\n%s", v3, stdout)
	}
}

// covers: MA-454, R14, R18, S14, S28
// A verify removal that finds nothing under the archive's derived
// "<hash>-verify" name never claims the archive has no schedule: its backup is
// installed here, and its verify still runs under the -name it was installed
// with (the old README passed a fixed -name). So `-remove -- verify -out X`
// exits 0, says "nothing to remove" naming the absent entry, hints at removing
// the verify by the -name it was installed with, and removes nothing: the
// backup, both descriptors and the custom-named verify all survive. The hint
// names the entry when something records it: the verify's own descriptor
// (FB-4) for a verify this release installed, or a backup descriptor that
// records the verify itself (written by a release from before a verify kept
// its own entry name); a verify an earlier release installed, recorded
// nowhere, gets the generic "-name NAME". The positive twin is that hint
// followed, the same command with `-name <custom>` added: it removes the verify
// and keeps the backup and its descriptor.
func TestScheduleRemoveVerifyNotFoundNamesTheEntry(t *testing.T) {
	sandboxScheduler(t)
	out := t.TempDir()
	backup := schedule.DefaultNameFor(out)
	derived := backup + "-verify"
	const custom = "weekly-verify-ma454"
	descPath := filepath.Join(out, schedule.DescriptorName)
	verifyDescPath := filepath.Join(out, schedule.VerifyDescriptorName)
	// forgetVerify makes the verify look installed by an earlier release, which
	// wrote no verify descriptor.
	forgetVerify := func() {
		t.Helper()
		if err := os.Remove(verifyDescPath); err != nil {
			t.Fatalf("fixture: the verify install wrote no verify descriptor to forget: %v", err)
		}
	}

	installCustomVerify := func() {
		t.Helper()
		args := []string{"schedule", "-interval", "weekly", "-at", "05:00", "-name", custom, "-install", "--", "verify", "-out", out}
		if code, _, stderr := runCLIOut(t, args...); code != 0 {
			t.Fatalf("%v exited %d: %s", args, code, stderr)
		}
	}
	if code, _, stderr := runCLIOut(t, "schedule", "-install", "--", "-out", out, "-auto"); code != 0 {
		t.Fatalf("backup install exited %d: %s", code, stderr)
	}
	installCustomVerify()
	if schedule.Query(backup) != schedule.Installed || schedule.Query(custom) != schedule.Installed || schedule.Query(derived) == schedule.Installed {
		t.Fatalf("fixture: backup=%v custom verify=%v derived=%v, want the backup and the custom-named verify only",
			schedule.Query(backup), schedule.Query(custom), schedule.Query(derived))
	}

	// notFound runs `-remove -- verify -out X` and checks it removed nothing,
	// named the absent derived entry, and carried the hint (with want in it).
	notFound := func(step, want string) {
		t.Helper()
		before, err := os.ReadFile(descPath)
		if err != nil {
			t.Fatal(err)
		}
		verifyBefore, verifyErr := os.ReadFile(verifyDescPath)
		code, stdout, stderr := runCLIOut(t, "schedule", "-remove", "--", "verify", "-out", out)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s%s", step, code, stdout, stderr)
		}
		for _, w := range []string{"nothing to remove", derived, "-name", want} {
			if !strings.Contains(stdout, w) {
				t.Errorf("%s: the not-found report lacks %q:\n%s", step, w, stdout)
			}
		}
		for _, bad := range []string{"no schedule was installed for", "Removed"} {
			if strings.Contains(stdout, bad) {
				t.Errorf("%s: the report says %q while this archive's schedules are installed:\n%s", step, bad, stdout)
			}
		}
		if schedule.Query(backup) != schedule.Installed || schedule.Query(custom) != schedule.Installed {
			t.Errorf("%s: a not-found verify removal removed something: backup=%v custom verify=%v", step, schedule.Query(backup), schedule.Query(custom))
		}
		if after, err := os.ReadFile(descPath); err != nil || string(after) != string(before) {
			t.Errorf("%s: a not-found verify removal changed the descriptor (%v)", step, err)
		}
		if after, err := os.ReadFile(verifyDescPath); (err == nil) != (verifyErr == nil) || string(after) != string(verifyBefore) {
			t.Errorf("%s: a not-found verify removal changed the verify descriptor (%v)", step, err)
		}
	}

	notFound("verify installed under a custom -name, recorded in its own descriptor", "-name "+custom)
	forgetVerify()
	notFound("verify installed under a custom -name by an earlier release, recorded nowhere", "-name NAME")

	// Positive twin: the hint, followed, removes that verify and only it.
	code, stdout, stderr := runCLIOut(t, "schedule", "-name", custom, "-remove", "--", "verify", "-out", out)
	if code != 0 || !strings.Contains(stdout, custom) || !strings.Contains(stdout, "Removed scheduled verify") {
		t.Errorf("remove -name <custom> -- verify -out X: exit %d, want it to remove %q:\n%s%s", code, custom, stdout, stderr)
	}
	if schedule.Query(custom) == schedule.Installed || schedule.Query(backup) != schedule.Installed {
		t.Errorf("remove -name <custom> -- verify: custom verify=%v backup=%v, want only the verify gone", schedule.Query(custom), schedule.Query(backup))
	}
	if d, err := schedule.ReadDescriptor(out); err != nil || d.Name != backup {
		t.Errorf("remove -name <custom> -- verify: the backup's descriptor was not kept (%+v, %v)", d, err)
	}

	// A descriptor that records the verify itself, as an older release wrote it
	// (that release wrote no verify descriptor either).
	installCustomVerify()
	forgetVerify()
	legacy := schedule.Spec{Name: custom, Interval: schedule.Weekly, At: "05:00", Exe: "/usr/local/bin/mailarchive",
		Args: []string{"verify", "-out", out, "-unattended"}, Out: out}
	if err := schedule.WriteDescriptor(out, schedule.DescribeSpec(legacy)); err != nil {
		t.Fatal(err)
	}
	notFound("descriptor records a verify", "-name "+custom)
}
