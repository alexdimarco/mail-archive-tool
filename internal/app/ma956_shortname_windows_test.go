//go:build windows

package app

import (
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// covers: MA-956, R36
// Windows: a run log given by its 8.3 short name (MAILAR~1.LOG, as a schedule typed by
// hand may name it) is the run's own file, which the folder lists by its long name; the
// person's file beside it is still refused (del-s2-A1-07). Skipped where the volume makes
// no short names.
func TestMA956_WindowsShortNameIsTheRunsOwn(t *testing.T) {
	out := t.TempDir()
	long := filepath.Join(out, "mailarchive-backup.log")
	ma956Write(t, long, "a line\n")
	p, err := syscall.UTF16PtrFromString(long)
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]uint16, 1024)
	n, err := syscall.GetShortPathName(p, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		t.Skipf("no short name for %s: %v", long, err)
	}
	short := syscall.UTF16ToString(buf[:n])
	if strings.EqualFold(filepath.Base(short), filepath.Base(long)) {
		t.Skipf("this volume makes no 8.3 short names (%s)", short)
	}
	if err := RefuseEmptiedFolder(out, "", short); err != nil {
		t.Fatalf("the run's log named by its short name %s must pass: %v", short, err)
	}
	ma956Write(t, filepath.Join(out, "notes.txt"), "mine")
	before := ma956List(t, out)
	ma956RefusedEmptied(t, RefuseEmptiedFolder(out, "", short), out, func() bool { return slices.Equal(before, ma956List(t, out)) })
}
