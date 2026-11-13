package health

import (
	"os"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
)

// covers: MA-1434, R50
// `status` says whether MailArchive runs, its version and why no icon shows
// (design-menubar MB-20), as information that never changes the posture. Positive twin
// first: with no instance it says nothing. A running instance of this version (its file
// records the icon state) gets the line with its version and "This version shows no icon
// yet"; one of an older version (no icon state) and one that is gone say nothing.
func TestMA1434_StatusSaysMailArchiveRunsAndWhyNoIconShows(t *testing.T) {
	dir := stateDirFor(t)
	out := t.TempDir()
	assess := func() (Input, Report) {
		in := Gather(out, "")
		return in, Assess(in, time.Now())
	}
	in0, rep0 := assess()
	if in0.DesktopRunning != "" || strings.Contains(strings.Join(rep0.Codes, ","), "desktop_running") {
		t.Fatalf("with no instance, status says %q", in0.DesktopRunning)
	}
	write := func(info instance.Info) {
		t.Helper()
		k, _ := instance.NewKey()
		info.Dashboard, info.Key = "127.0.0.1:53210", k
		if err := instance.Write(instance.FilePathIn(dir, instance.LogonSession()), info); err != nil {
			t.Fatal(err)
		}
	}
	write(instance.Info{PID: os.Getpid(), Version: "v1.8.0", Icon: instance.IconNotYet, Acts: instance.KnownActs})
	in, rep := assess()
	got := strings.Join(Summary(in, rep), "\n")
	if !strings.Contains(got, "MailArchive is running for your account (version v1.8.0") || !strings.Contains(got, "This version shows no icon yet.") {
		t.Errorf("status with a running instance:\n%s", got)
	}
	if rep.Posture != rep0.Posture {
		t.Errorf("the running line changed the posture: %s -> %s", rep0.Posture, rep.Posture)
	}
	write(instance.Info{PID: os.Getpid(), Version: "v1.7.1"})
	if in, _ := assess(); in.DesktopRunning != "" {
		t.Errorf("an older version's instance got the line %q", in.DesktopRunning)
	}
	write(instance.Info{PID: 1 << 30, Version: "v1.8.0", Icon: instance.IconNotYet})
	if in, _ := assess(); in.DesktopRunning != "" {
		t.Errorf("a gone instance got the line %q", in.DesktopRunning)
	}
}
