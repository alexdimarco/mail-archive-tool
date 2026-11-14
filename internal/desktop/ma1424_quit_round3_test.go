package desktop

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-1424, R52
// `quit -end-older` ends only an older copy that is proven to be MailArchive and whose
// record is live (the S1 fix round 3, C3): a record of a copy from before the menu-bar
// release (no hand-off actions) whose lock nobody holds was left by a copy that ended
// without its clean-up, and its pid, reused, may be any program's (the re-check ended a
// PING.EXE that way). Positive twin first: a live record (its lock held) whose process runs
// an installed MailArchive program is "would be ended" with -check (and, last, ended
// without it, exit 0). Then, with the same live pid each time: the record with its lock
// let go is "not running", exit 2, with or without -end-older; a held record whose process
// runs another program (PING.EXE) is "could not ask", exit 1, naming it; one whose program
// cannot be read likewise. None of them is ended, -check or not.
func TestMA1424_QuitEndsOnlyALiveOlderMailArchive(t *testing.T) {
	dir := t.TempDir()
	keyHex, _ := instance.NewKey()
	// record writes the older copy's record (a holder's Close removes it, so each phase
	// writes it again).
	record := func() {
		t.Helper()
		if err := instance.Write(instance.FilePathIn(dir, ""), instance.Info{PID: os.Getpid(), Dashboard: "127.0.0.1:1", Key: keyHex, Version: "v1.7.1"}); err != nil {
			t.Fatal(err)
		}
	}
	var ended atomic.Int32
	run := func(program func(int) (string, error), args ...string) (int, string) {
		t.Helper()
		var out, errb bytes.Buffer
		env := &quitEnv{
			Stdout: &out, Stderr: &errb,
			StateDir:  func() (string, error) { return dir, nil },
			SessionOf: func(int) (string, error) { return "", nil },
			// The recorded pid is alive throughout (a reused pid is a live process), until
			// the twin's end.
			Alive:   func(pid int) bool { return pid == os.Getpid() && ended.Load() == 0 },
			Program: program,
			End:     func(int) error { ended.Add(1); return nil },
			Reach:   time.Second, Poll: 10 * time.Millisecond,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		code := quitVerb(ctx, args, env)
		return code, out.String() + errb.String()
	}
	// Paths in this system's own form, as procs.Program reads them.
	installed := func(int) (string, error) {
		return filepath.Join(t.TempDir(), "Program Files", "MailArchive Desktop", "mailarchivew.exe"), nil
	}
	ping := func(int) (string, error) { return filepath.Join(t.TempDir(), "Windows", "System32", "PING.EXE"), nil }
	unreadable := func(int) (string, error) { return "", os.ErrPermission }
	notEnded := func() bool { return ended.Load() == 0 }

	hold := func() func() {
		t.Helper()
		lock, err := instance.Acquire(dir, "")
		if err != nil {
			t.Fatal(err)
		}
		release := sync.OnceFunc(lock.Close)
		t.Cleanup(release)
		record()
		return release
	}

	// The twin's question first (it ends nothing): a live record of an installed MailArchive.
	release := hold()
	code, words := run(installed, "-check", "-end-older")
	if code != QuitDone || !bytes.Contains([]byte(words), []byte("it would be ended")) || !notEnded() {
		t.Fatalf("twin: -check -end-older on a live older MailArchive: %d %q, ended %d", code, words, ended.Load())
	}

	// The same record stale: nobody holds its lock.
	release()
	record()
	for _, args := range [][]string{{"-end-older", "-wait", "1s"}, {"-check", "-end-older"}, {}} {
		code, words := run(installed, args...)
		assure.Refused(t, code, words, assure.Code(QuitNotRunning), assure.Names("not running for your account"), assure.NoSideEffect(notEnded))
	}

	hold()
	for _, c := range []struct {
		program func(int) (string, error)
		names   []string
	}{
		{ping, []string{"PING.EXE", "not MailArchive", "not ended"}},
		{unreadable, []string{"cannot tell which program", "not ended"}},
	} {
		for _, args := range [][]string{{"-end-older", "-wait", "1s"}, {"-check", "-end-older"}} {
			code, words := run(c.program, args...)
			assure.Refused(t, code, words, assure.Code(QuitCouldNotAsk), assure.Names(c.names...), assure.NoSideEffect(notEnded))
		}
	}

	// The twin's end, last (it ends the copy).
	code, words = run(installed, "-end-older", "-wait", "5s")
	if code != QuitDone || !bytes.Contains([]byte(words), []byte("was ended")) || ended.Load() != 1 {
		t.Errorf("twin: -end-older on a live older MailArchive: %d %q, ended %d", code, words, ended.Load())
	}
}
