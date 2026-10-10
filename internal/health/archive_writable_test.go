package health

import (
	"os"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-392, R18, S29
// A scheduled backup whose archive folder cannot be written is visible on status
// (g7 friction F2). A run that cannot write into its folder cannot write its
// failed last-run record or the attention file either, so status kept showing
// the previous ok until the stale WARN two periods later. status now probes that
// the folder takes a write (a small file, removed again, so a full disk shows
// too): with a schedule recorded for this host, a folder that refuses is RED
// archive_unwritable naming the folder, the cause and the remedy; a full disk
// says free space. An archive with no schedule (an inheritor's read-only copy)
// is not flagged, and a schedule recorded for another host is not judged here.
// Positive twin: a writable folder probes clean and leaves nothing behind.
func TestStatusProbesThatTheArchiveFolderIsWritable(t *testing.T) {
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)

	// Gather, for real: a writable folder probes clean and the probe cleans up.
	out := t.TempDir()
	if in := Gather(out, ""); in.WriteProbeErr != nil {
		t.Fatalf("positive twin: a writable folder failed the probe: %v", in.WriteProbeErr)
	}
	ents, _ := os.ReadDir(out)
	for _, e := range ents {
		if strings.Contains(e.Name(), "probe") {
			t.Fatalf("the probe left %s in the archive folder", e.Name())
		}
	}
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		ro := t.TempDir()
		if err := os.Chmod(ro, 0o555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(ro, 0o755) })
		if in := Gather(ro, ""); in.WriteProbeErr == nil {
			t.Fatal("a read-only folder passed the write probe")
		}
	}

	// Assess: the judgement, on fabricated facts.
	in := healthyInput(now)
	in.ThisHost = in.Desc.Host
	if rep := Assess(in, now); hasCode(rep, "archive_unwritable") {
		t.Fatalf("positive twin: a writable archive drew archive_unwritable: %v", rep.Reasons)
	}
	in.WriteProbeErr = &os.PathError{Op: "open", Path: "/a/probe", Err: syscall.EACCES}
	rep := Assess(in, now)
	got := assure.Reached(t, reasonOf(rep, "archive_unwritable"), "the archive_unwritable reason")
	if rep.Posture != "RED" || !strings.HasPrefix(got, "RED:") {
		t.Fatalf("a scheduled backup that cannot write is RED: %s %v", rep.Posture, rep.Reasons)
	}
	for _, want := range []string{"/a", "cannot be written", "last-run record", "make the folder writable", "permission denied"} {
		if !strings.Contains(got, want) {
			t.Errorf("reason lacks %q: %s", want, got)
		}
	}

	full := healthyInput(now)
	full.ThisHost = full.Desc.Host
	full.WriteProbeErr = &os.PathError{Op: "write", Path: "/a/probe", Err: syscall.ENOSPC}
	if got := reasonOf(Assess(full, now), "archive_unwritable"); !strings.Contains(got, "free space") {
		t.Errorf("a full disk must say to free space: %q", got)
	}

	noSched := healthyInput(now)
	noSched.HasDescriptor = false
	noSched.WriteProbeErr = in.WriteProbeErr
	if rep := Assess(noSched, now); hasCode(rep, "archive_unwritable") {
		t.Fatalf("a read-only copy with no schedule is not a failing backup: %v", rep.Reasons)
	}
	other := healthyInput(now)
	other.ThisHost = "elsewhere"
	other.WriteProbeErr = in.WriteProbeErr
	if rep := Assess(other, now); hasCode(rep, "archive_unwritable") {
		t.Fatalf("a schedule recorded for another host is not judged from here: %v", rep.Reasons)
	}
}
