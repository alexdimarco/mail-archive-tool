package desktop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/util"
)

// ma1453Log is a launch's stderr, safe to read while the launch writes it.
type ma1453Log struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *ma1453Log) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *ma1453Log) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

// ma1453Start launches g's instance with its stderr kept in logs and returns once it serves, or the
// launch's error when it ends first.
func ma1453Start(t *testing.T, g *dRig, logs *ma1453Log) error {
	t.Helper()
	env := g.env(g.rec)
	env.Stderr = logs
	ready := make(chan instance.Info, 1)
	env.Ready = func(i instance.Info) { ready <- i }
	ctx, cancel := context.WithCancel(context.Background())
	g.cancel = cancel
	g.done = make(chan error, 1)
	go func() { g.done <- launch(ctx, g.args(), env) }()
	t.Cleanup(g.stop)
	select {
	case g.info = <-ready:
		return nil
	case err := <-g.done:
		g.cancel = nil
		return err
	case <-time.After(15 * time.Second):
		t.Fatalf("the instance was not serving after 15 s")
		return nil
	}
}

// covers: MA-1453, R23
// The research note's L0, at MailArchive Desktop's start: when Windows will not give its private
// start file the Medium no-read-up label, the Desktop still starts and serves, its start file
// written as 1.7.1 wrote it, with one plain warning in its log; 1.7.2 is never worse than 1.7.1
// there. Only when no usable file can be written at all does it refuse to start, in the words and
// the dialog of MA-1451. Positive twin first: a start with the label written serves and says nothing
// of it.
func TestADesktopWhoseStartFileWindowsWillNotLabelStillStarts(t *testing.T) {
	g := newDRig(t)
	logs := &ma1453Log{}
	if err := ma1453Start(t, g, logs); err != nil || strings.Contains(logs.String(), "would not protect") {
		t.Fatalf("positive twin: the start gave %v and logged %q", err, logs.String())
	}
	g.stop()

	refuse := func(path string, _ []byte) error {
		return fmt.Errorf("temp file in %s: %w", filepath.Dir(path), &os.PathError{Op: "createsecret", Path: filepath.Join(filepath.Dir(path), ".tmp-0123456789abcdef"), Err: errors.New("A required privilege is not held by the client.")})
	}
	restore := instance.SetWritersForTest(refuse, util.WriteFileAtomic0600)
	defer restore()
	g = newDRig(t)
	logs = &ma1453Log{}
	if err := ma1453Start(t, g, logs); err != nil {
		t.Fatalf("a start file Windows would not label stopped the Desktop: %v (dialogs %q)", err, g.rec.refusals)
	}
	if got, err := instance.Read(instance.FilePathIn(g.dir, g.session)); err != nil || got.Key == "" {
		t.Fatalf("the start file written without the label is not usable: %+v %v", got, err)
	}
	want := "mailarchive desktop: warning: Windows would not protect MailArchive Desktop's private start file in " + g.dir + " (A required privilege is not held by the client.), so it was written as before 1.7.2: a sandboxed program running under your account may be able to read it."
	if !strings.Contains(logs.String(), want) {
		t.Fatalf("the Desktop's log lacks %q:\n%s", want, logs.String())
	}
	if _, refusals, _ := g.rec.snapshot(); len(refusals) != 0 {
		t.Fatalf("a start that worked showed dialogs: %q", refusals)
	}
	g.stop()

	restore()
	restore = instance.SetWritersForTest(refuse, func(string, []byte) error { return errors.New("Access is denied.") })
	g = newDRig(t)
	logs = &ma1453Log{}
	err := ma1453Start(t, g, logs)
	rc := 0
	if RefusalShown(err) {
		rc = 2
	}
	assure.Refused(t, rc, fmt.Sprint(err), assure.Names("MailArchive Desktop cannot start: Windows would not protect its private start file in "+g.dir, "Do this: start MailArchive Desktop from the Start menu"),
		assure.NoSideEffect(func() bool {
			_, serr := os.Stat(instance.FilePathIn(g.dir, g.session))
			return errors.Is(serr, os.ErrNotExist)
		}))
	if _, refusals, _ := g.rec.snapshot(); len(refusals) != 1 || refusals[0] != err.Error() {
		t.Fatalf("the refusal was shown as %q, want one dialog with the same words", refusals)
	}
}
