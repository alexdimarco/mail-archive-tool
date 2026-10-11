package health

import (
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
)

// stateDirFor points the per-account state directory at a temp dir and returns it.
func stateDirFor(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", home)
	t.Setenv("LOCALAPPDATA", home)
	t.Setenv("AppData", home)
	dir, err := instance.Dir()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func publish(t *testing.T, dir string, pid int, former string) {
	t.Helper()
	k, _ := instance.NewKey()
	err := instance.Write(instance.FilePathIn(dir, instance.LogonSession()), instance.Info{PID: pid, Dashboard: "127.0.0.1:53210", Key: k, Former: former})
	if err != nil {
		t.Fatal(err)
	}
}

// covers: MA-376, MA-384, R18, R23, X6
// `status` says what holds MailArchive's former address 127.0.0.1:8097 when the
// running desktop recorded it at start — another program, or an older instance it
// could not end — as a WARN with the §7.6 words and its own reason code; with
// nothing recorded, a dead instance, or a kind this version does not know (the
// file is writable by the account's own processes and parsed defensively), it
// says nothing [CC22, CC23, ruling OQ-9].
func TestStatusNamesWhatHoldsTheFormerAddress(t *testing.T) {
	dir := stateDirFor(t)
	out := t.TempDir()
	assess := func() (Input, Report) {
		in := Gather(out, "")
		return in, Assess(in, time.Now())
	}
	codes := func(r Report) string { return strings.Join(r.Codes, ",") }

	if in, rep := assess(); in.DesktopFormer != "" || strings.Contains(codes(rep), "desktop_") {
		t.Fatalf("with no instance recorded, status names a desktop notice: %q %q", in.DesktopFormer, codes(rep))
	}
	for _, c := range []struct{ kind, code, text string }{
		{instance.FormerProgram, "desktop_former_address", instance.FormerProgramNotice},
		{instance.FormerPreUpgrade, "desktop_preupgrade", instance.PreUpgradeNotice},
	} {
		publish(t, dir, os.Getpid(), c.kind)
		in, rep := assess()
		if in.DesktopFormer != c.kind || !strings.Contains(codes(rep), c.code) || rep.Posture != "WARN" {
			t.Errorf("%s: Gather %q, codes %q, posture %s; want the WARN %s", c.kind, in.DesktopFormer, codes(rep), rep.Posture, c.code)
		}
		if got := strings.Join(Summary(in, rep), "\n"); !strings.Contains(got, c.text) {
			t.Errorf("%s: the summary lacks %q:\n%s", c.kind, c.text, got)
		}
	}
	// A desktop that is gone, or a recorded kind we do not know, says nothing.
	publish(t, dir, 1<<30, instance.FormerProgram)
	if in, _ := assess(); in.DesktopFormer != "" {
		t.Errorf("a dead instance's notice is still shown: %q", in.DesktopFormer)
	}
	publish(t, dir, os.Getpid(), "somethingelse")
	if in, rep := assess(); in.DesktopFormer != "" || strings.Contains(codes(rep), "desktop_") {
		t.Errorf("an unknown recorded kind is shown: %q %q", in.DesktopFormer, codes(rep))
	}
}
