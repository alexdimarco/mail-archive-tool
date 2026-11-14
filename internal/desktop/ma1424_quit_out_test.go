package desktop

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/instance"
)

// covers: MA-1424, MA-614, R52
// `quit -out FILE` writes the verb's own answer where the upgrade script can read it (the
// S1 fix round 3, C4: the one-time task's output is not kept, so the script had only the
// exit status). The file holds the outcome word, then the very line the verb printed.
// Positive twin first: an idle copy asked -check writes "would" and its "would quit now"
// line. A busy copy writes "busy" and its busy line; a copy that agreed but had not ended
// after -wait writes "finishing" — not "busy", though both are exit 3 — with its "is
// finishing its work" line; an older copy asked without -end-older writes "could-not-ask"
// with the reason the verb gave; no copy writes "not-running". A line is one line, with no
// control characters.
func TestMA1424_TheQuitVerbWritesItsOwnAnswerForTheScript(t *testing.T) {
	installFrontSeams(t)
	var dash atomic.Pointer[dashboard]
	instanceDashboardMade = func(x *dashboard) { dash.Store(x) }
	t.Cleanup(func() { instanceDashboardMade = nil })
	outDir := t.TempDir()
	n := 0
	answer := func(code int, words string, run func(out string) (int, string)) (int, string, string) {
		t.Helper()
		n++
		out := filepath.Join(outDir, "answer"+string(rune('a'+n))+".txt")
		got, printed := run(out)
		b, err := os.ReadFile(out)
		if err != nil {
			t.Fatalf("no -out file after %q: %v", printed, err)
		}
		lines := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n")
		if len(lines) != 2 {
			t.Fatalf("the -out file has %d lines, want 2: %q", len(lines), b)
		}
		if got != code || !strings.Contains(printed, lines[1]) || !strings.Contains(lines[1], words) {
			t.Errorf("exit %d (want %d); printed %q; the file's line %q (want it printed, naming %q)", got, code, printed, lines[1], words)
		}
		return got, lines[0], lines[1]
	}

	idle := newDRig(t)
	idle.startInstance(idle.env(idle.rec), "-background")
	if _, word, _ := answer(QuitDone, "would quit now", func(out string) (int, string) { return quitRun(t, idle, "-check", "-out", out) }); word != QuitOutcomeWould {
		t.Fatalf("twin: an idle copy's -check wrote %q, want %q", word, QuitOutcomeWould)
	}

	busy := newDRig(t)
	busy.startInstance(busy.env(busy.rec), "-background")
	holdCancellable(t, dash.Load(), t.TempDir(), 50*time.Millisecond)
	if _, word, _ := answer(QuitBusy, "is busy", func(out string) (int, string) { return quitRun(t, busy, "-wait", "1s", "-out", out) }); word != QuitOutcomeBusy {
		t.Errorf("a busy copy wrote %q, want %q", word, QuitOutcomeBusy)
	}

	// An older copy that was ended but has not gone after -wait: the verb's wait is the same
	// one an agreeing copy gets, so it is "finishing", never "busy".
	older := newDRig(t)
	keyHex, _ := instance.NewKey()
	if err := instance.Write(instance.FilePathIn(older.dir, ""), instance.Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex, Version: "v1.7.1"}); err != nil {
		t.Fatal(err)
	}
	lock, err := instance.Acquire(older.dir, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sync.OnceFunc(lock.Close))
	if _, word, _ := answer(QuitBusy, "is finishing its work", func(out string) (int, string) {
		return quitRunEnding(t, older, func(int) error { return nil }, "-end-older", "-wait", "100ms", "-out", out)
	}); word != QuitOutcomeFinishing {
		t.Errorf("a copy still finishing after -wait wrote %q, want %q", word, QuitOutcomeFinishing)
	}
	if _, word, _ := answer(QuitCouldNotAsk, "older version", func(out string) (int, string) { return quitRun(t, older, "-out", out) }); word != QuitOutcomeCouldNotAsk {
		t.Errorf("an older copy asked without -end-older wrote %q, want %q", word, QuitOutcomeCouldNotAsk)
	}

	none := newDRig(t)
	if _, word, _ := answer(QuitNotRunning, "not running", func(out string) (int, string) { return quitRun(t, none, "-out", out) }); word != QuitOutcomeNotRunning {
		t.Errorf("no copy wrote %q, want %q", word, QuitOutcomeNotRunning)
	}

	// The line is bounded and has no control characters, whatever the reason carried.
	p := filepath.Join(outDir, "long.txt")
	if err := writeQuitOut(p, QuitOutcomeCouldNotAsk, "a\x1b[2Jb\r\n"+strings.Repeat("x", 2*maxQuitOutLine)); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if parts := strings.Split(strings.TrimSuffix(string(b), "\n"), "\n"); len(parts) != 2 || len(parts[1]) > maxQuitOutLine || bytes.ContainsAny([]byte(parts[1]), "\x1b\r") {
		t.Errorf("the -out file's line was not one bounded line without control characters: %q", b)
	}
}
