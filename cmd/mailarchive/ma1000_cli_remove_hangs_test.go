package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1000, R36, R12
// Ctrl-C while -plan-out is being written, on a folder whose remove then hangs (a network share
// that stopped answering after the file was made), stops the verb within createWait of the
// removal, naming the file as maybe incomplete (the delete residuals' security pass, CON-03):
// the close was bounded by createWait but the remove after it was not, and it ran under the
// watch's lock, so the verb never exited, a second Ctrl-C was never handled and the verb itself
// stalled on the same lock. Nothing is deleted. Positive twin first: with the removal answering,
// the unfinished file is removed and said so.
func TestCtrlCWhileThePlanIsSavedIsNotHeldByARemoveThatHangs(t *testing.T) {
	e := newCLIDel(t)
	f := e.fakeSignals()
	origOpen, origRemove := openSavedFile, removeOwnedFile
	t.Cleanup(func() { openSavedFile, removeOwnedFile = origOpen, origRemove })
	openSavedFile = func(path string) (*os.File, error) {
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			go func() { f.sigs <- os.Interrupt }()
			time.Sleep(50 * time.Millisecond)
		}
		return fh, err
	}
	run := func(pl, planFile string) (code int, took time.Duration, said string, ran chan struct{}) {
		t.Helper()
		before := len(f.said.String())
		ran = make(chan struct{})
		start := time.Now()
		go func() {
			defer close(ran)
			_ = deleteArchive(&scripted{isAttended: true}, pl, planFile, "", io.Discard)
		}()
		code = f.waitExit()
		return code, time.Since(start), f.said.String()[before:], ran
	}

	twin := e.plainArchive("Archive R0")
	twinFile := filepath.Join(t.TempDir(), "plan.txt")
	code, took, said, ran := run(twin, twinFile)
	<-ran
	if _, err := os.Lstat(twinFile); code != 130 || err == nil || took > createWait+time.Second || !strings.Contains(said, "the unfinished file was removed") {
		t.Fatalf("positive twin: Ctrl-C while the plan is saved, the removal answering: exit %d after %v, file left %v\n%s", code, took, err == nil, said)
	}

	pl := e.plainArchive("Archive R1")
	before := tree(t, pl)
	planFile := filepath.Join(t.TempDir(), "plan.txt")
	hang := make(chan struct{})
	removeOwnedFile = func(path string) error {
		<-hang // the share stopped answering
		return os.Remove(path)
	}
	code, took, said, ran = run(pl, planFile)
	close(hang)
	select {
	case <-ran:
	case <-time.After(10 * time.Second):
		t.Fatal("the verb never returned once the remove answered")
	}
	assure.Refused(t, code, said, assure.Code(130),
		assure.Names("stopped by Ctrl-C while saving the plan to "+planFile, "that file may be incomplete, so delete it", "Nothing was deleted."),
		assure.NoSideEffect(func() bool { return took <= 2*createWait+time.Second && sameTree(before, tree(t, pl)) && e.listed(pl) }))
}
