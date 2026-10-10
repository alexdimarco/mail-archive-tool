package health

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/state"
)

// covers: MA-466, R18, S29
// OPEN ENCODER (G8-AI4, R18; an owner decision). `status` stats the program path
// each descriptor records (the backup's and now the scheduled verify's), a string
// whoever handed the folder over can write: a UNC path (\\host\share\x.exe) makes
// Windows open an SMB connection with the viewer's credentials, and a path on a
// hung mount blocks `status`. Not new in g8 (the backup descriptor has always
// been stat'ed; the verify's adds a second planted path, no new capability). The
// fix is to refuse to stat a UNC program path — which turns a legitimate
// network-share install into verify_exe_missing/exe_missing, a narrowing of what
// status reports that is the owner's to accept — and cannot cover a dead mount.
// The encoder wants exeIdentity to leave a UNC program path unstat'ed: reported
// as neither existing nor this binary, without touching the network.
func TestStatusDoesNotStatAUNCProgramPath(t *testing.T) {
	t.Skip("open (owner decision, G8-AI4): exeIdentity stats a descriptor-supplied program path; refusing UNC paths narrows status for network-share installs, and a hung mount cannot be detected without a deadline.")
	exists, isThis := exeIdentity(schedule.Descriptor{Exe: `\\attacker-host\share\x.exe`})
	if exists || isThis {
		t.Errorf("a UNC program path was resolved (exists=%v isThis=%v)", exists, isThis)
	}
}

// covers: MA-157, MA-158, R18, S29
// G8-AO2 (g8 adversarial pass): the moved-archive remedy repeats the recorded
// job, which is untrusted (a handed-over folder writes it), as a pasteable line.
// On Windows a token cannot be quoted for both shells: inside double quotes
// PowerShell still expands $(...) and $env:, and cmd expands %VAR% even quoted
// (^ is cmd's escape), so a token carrying any of those is not pasted at all — it
// is replaced by a placeholder that neither shell will run (`<`) — while a
// benign token keeps MA-157's double quotes (a spaced path, and a share root
// that ends in the administrative `$`). On a POSIX host the single-quoted form
// stays inert and unchanged. The positive twin first: a benign Windows job
// renders with its spaced path in double quotes.
func TestMovedArchiveRemedyNeverPastesAShellExpansion(t *testing.T) {
	if !filepath.IsAbs("/elsewhere") {
		t.Skip("the moved-archive WARN needs POSIX-absolute -out spellings; Windows hosts are covered by TestMovedArchivePathComparison")
	}
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	moved := func(goos string, extra ...string) string {
		in := healthyInput(now)
		in.Out = "/handed/archive"
		in.Desc.Name = "mailarchive-x"
		in.Desc.Job = append([]string{"-out", "/elsewhere", "-auto"}, extra...)
		in.GOOS = goos
		return joinReasons(Assess(in, now))
	}

	// Positive twin: benign tokens keep the double quotes on Windows.
	if r := moved("windows", "-input", `C:\Mail Box\a.pst`, "-input", `\\nas\backup$`); !strings.Contains(r, `-input "C:\Mail Box\a.pst"`) || !strings.Contains(r, `-input "\\nas\backup$"`) {
		t.Fatalf("a benign Windows job is not rendered as MA-157 renders it:\n%s", r)
	}

	for _, hostile := range []string{"$(Start-Process calc)", "`$env:USERPROFILE`", "$env:APPDATA\\x", "%USERPROFILE%", "a^&b", "x${calc}y"} {
		r := moved("windows", "-input", hostile)
		if strings.Contains(r, hostile) {
			t.Errorf("the Windows remedy pastes %q, which PowerShell or cmd would expand:\n%s", hostile, r)
		}
		if !strings.Contains(r, "mailarchive schedule") || !strings.Contains(r, "<") {
			t.Errorf("the Windows remedy for %q is not a placeholder-bearing command:\n%s", hostile, r)
		}
	}
	// POSIX: single-quoted, unchanged (inert in sh).
	if r := moved("linux", "-input", "$(id)"); !strings.Contains(r, "'$(id)'") {
		t.Errorf("the POSIX remedy no longer single-quotes the token:\n%s", r)
	}
}

// covers: MA-458, R18, S29
// K3 (g8 completion run's fresh-eyes check): a stale scheduled verify whose last
// record is still "running" while a process holds the archive lock is running
// now, so the remedy must not say it "stopped running" and tell the operator to
// re-install it or run another verify — that would change nothing, or collide
// with the lock. It says a verify holds the lock and is still running, and to
// let it finish (or end it if it hung). The positive twin: the same record with
// the lock free (the process is gone) keeps the "stopped running ... re-install"
// remedy.
func TestStaleVerifyThatStillHoldsTheLockIsNotToldToReinstall(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	started := now.Add(-30 * 24 * time.Hour)
	build := func(lockHeld bool) Input {
		in := healthyInput(now)
		in.GOOS = "linux"
		in.HasVerifyDescriptorFile, in.HasVerifyDescriptor = true, true
		in.VerifyDesc = schedule.Descriptor{Version: 1, Name: "mailarchive-1234abcd-verify", Interval: "weekly", At: "05:00",
			Exe: "/usr/local/bin/mailarchive", Job: []string{"verify", "-out", "/a", "-unattended"}, InstalledAt: now.Add(-90 * 24 * time.Hour), Host: "box"}
		in.VerifySchedState, in.VerifyExeExists, in.VerifyExeIsThis = schedule.Installed, true, true
		in.LastVerifyState = state.LastVerifyPresent
		in.LastVerify = state.LastVerify{Status: state.VerifyRunning, Started: started}
		in.LockHeld = lockHeld
		return in
	}
	stale := func(in Input) string {
		rep := Assess(in, now)
		for i, c := range rep.Codes {
			if c == "verify_stale" {
				return rep.Reasons[i]
			}
		}
		t.Fatalf("no verify_stale (reasons: %v)", rep.Reasons)
		return ""
	}

	if r := stale(build(false)); !strings.Contains(r, "stopped running") || !strings.Contains(r, "re-install it with") {
		t.Errorf("a stale verify whose process is gone lost its re-install remedy:\n%s", r)
	}
	r := stale(build(true))
	if strings.Contains(r, "stopped running") || strings.Contains(r, "re-install") {
		t.Errorf("a verify that holds the archive lock is told it stopped running and to re-install:\n%s", r)
	}
	for _, want := range []string{"holds the archive lock", "still running"} {
		if !strings.Contains(r, want) {
			t.Errorf("the remedy for a running verify lacks %q:\n%s", want, r)
		}
	}
}

// covers: MA-466, R18, S29
// K5 (g8 completion run's fresh-eyes check): Gather records the scheduled
// verify's program identity from the verify descriptor, as it does the
// backup's, so `status` judges verify_exe_missing and verify_exe_moved from
// facts, and a healthy scheduled verify (its program is this binary) is neither.
// Without it every healthy scheduled verify would WARN verify_exe_missing, the
// zero value of the facts. Through a real descriptor written to a temp archive:
// the program is this binary (the same file), a copy of it with the size and
// modification time the install recorded (this binary by identity), another file
// changed since the install (exists, is not this binary), and a path that is gone.
func TestGatherRecordsTheScheduledVerifysProgramIdentity(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	copyOfSelf := filepath.Join(dir, "copy-of-mailarchive")
	data, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(copyOfSelf, data, 0o755); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(self)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(copyOfSelf, fi.ModTime(), fi.ModTime()); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(dir, "other-program")
	if err := os.WriteFile(other, []byte("not this binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		step               string
		exe                string
		wantExists, wantIs bool
	}{
		{"the program is this binary", self, true, true},
		{"a copy with the recorded size and time", copyOfSelf, true, true},
		{"another program", other, true, false},
		{"a program that is gone", filepath.Join(dir, "gone"), false, false},
	} {
		out := t.TempDir()
		spec := schedule.Spec{Name: schedule.DefaultNameFor(out) + "-verify", Interval: schedule.Weekly, At: "05:00", Exe: c.exe,
			Args: []string{"verify", "-out", out, "-unattended"}, Out: out}
		d := schedule.DescribeSpec(spec)
		if c.exe == copyOfSelf { // as if the install had recorded this binary's identity
			d.ExeSize, d.ExeMTime = fi.Size(), fi.ModTime().UTC()
		}
		if err := schedule.WriteVerifyDescriptor(out, d); err != nil {
			t.Fatal(err)
		}
		if c.exe == other { // changed since the install recorded it: not the program that was installed
			if err := os.WriteFile(other, []byte("changed after the install"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		in := Gather(out, "")
		if !in.HasVerifyDescriptor {
			t.Fatalf("%s: the verify descriptor was not gathered: %v", c.step, in.VerifyDescErr)
		}
		if in.VerifyExeExists != c.wantExists || in.VerifyExeIsThis != c.wantIs {
			t.Errorf("%s: Gather recorded exists=%v isThis=%v, want %v %v", c.step, in.VerifyExeExists, in.VerifyExeIsThis, c.wantExists, c.wantIs)
		}
	}
}
