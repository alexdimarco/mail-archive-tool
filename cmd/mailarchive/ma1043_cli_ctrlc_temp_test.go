package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/util"
)

// tempsIn are the atomic writer's temp files left in dir.
func tempsIn(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, en := range ents {
		if strings.HasPrefix(en.Name(), ".tmp-") {
			out = append(out, en.Name())
		}
	}
	return out
}

// covers: MA-1043, R36
// Ctrl-C while the delete changes things ends delete-archive at once, and a settings file being
// written at that moment (Your archives at step 9) leaves no ".tmp-…" file beside it (the 1.5.3
// re-walk's N19: a 35-byte one stayed in the settings folder, and uninstall did not remove it).
// Positive twin first: a write that finishes leaves no temp file, and the stop's words and exit
// are as before.
func TestCtrlCMidWriteLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	list := filepath.Join(dir, "known-archives.json")
	if err := util.WriteFileAtomic0600(list, []byte(`{"entries":[]}`)); err != nil || len(tempsIn(t, dir)) != 0 {
		t.Fatalf("the twin: %v, temps %v", err, tempsIn(t, dir))
	}

	reached, release := make(chan string, 1), make(chan struct{})
	orig := util.BeforeAtomicRename
	t.Cleanup(func() { util.BeforeAtomicRename = orig })
	util.BeforeAtomicRename = func(tmp string) { reached <- tmp; <-release }
	done := make(chan error, 1)
	go func() { done <- util.WriteFileAtomic0600(list, []byte(`{"entries":[{"path":"/x"}]}`)) }()
	tmp := <-reached
	if _, err := os.Stat(tmp); err != nil {
		t.Fatalf("fixture: the temp file is not there mid-write: %v", err)
	}

	sigs, exits := make(chan os.Signal, 1), make(chan int, 1)
	var said bytes.Buffer
	v := newVerbWatch(sigs, &said, &bytes.Buffer{}, "delete-archive", func(code int) { exits <- code })
	defer v.stop()
	v.setRow("/a/Arc")
	v.set(phaseChanging)
	sigs <- os.Interrupt
	var code int
	select {
	case code = <-exits:
	case <-time.After(5 * time.Second):
		t.Fatal("the stop did not exit")
	}
	left := tempsIn(t, dir)
	close(release)
	<-done
	if code != 130 || len(left) != 0 || !strings.Contains(said.String(), "delete-archive was stopped by Ctrl-C and did not finish") {
		t.Fatalf("exit %d, temp files left %v, said %q", code, left, said.String())
	}
	if b, err := os.ReadFile(list); err != nil || string(b) != `{"entries":[]}` {
		t.Fatalf("the list it was replacing changed: %q %v", b, err)
	}
}
