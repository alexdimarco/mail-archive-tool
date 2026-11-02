package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1001, R36, R12
// A -plan-out or -record-out file that is created but cannot be written to the end is removed,
// so no partial plan or record is left; the text is printed in full instead (§8.1; slice 4's
// adversarial pass, del-s4-A2-08).
func TestASavedFileThatCannotBeWrittenToTheEndIsRemoved(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Archive W")

	// Positive twin: the plan saved whole.
	whole := filepath.Join(t.TempDir(), "plan.txt")
	var out bytes.Buffer
	if err := deleteArchive(&scripted{isAttended: true}, dir, whole, "", &out); err != nil {
		t.Fatalf("a plan-only run: %v", err)
	}
	if b, err := os.ReadFile(whole); err != nil || !strings.Contains(string(b), "Status: planned, nothing deleted") {
		t.Fatalf("the plan was not saved whole: %v\n%s", err, b)
	}

	// Created, but every write fails (a disk that fills, a drive pulled out): the file is removed.
	brokenOpen := func(path string) (*os.File, error) {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err != nil {
			return nil, err
		}
		f.Close()
		return os.Open(path) // read-only: the write fails
	}
	openSavedFile = brokenOpen
	partial := filepath.Join(t.TempDir(), "plan.txt")
	out.Reset()
	err := deleteArchive(&scripted{isAttended: true}, dir, partial, "", &out)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("The plan could not be saved to "+partial, "it is shown above", "Nothing was deleted"),
		assure.NoSideEffect(func() bool {
			_, serr := os.Lstat(partial)
			return os.IsNotExist(serr) && strings.Contains(out.String(), "Status: planned, nothing deleted") && e.listed(dir)
		}))

	// The record of a delete: removed, printed in full, the delete's own exit code.
	rec := filepath.Join(t.TempDir(), "record.txt")
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive W"}}, dir, "", rec, &out); err != nil {
		t.Fatalf("the delete's exit is not its own when the record could not be saved: %v\n%s", err, out.String())
	}
	if _, serr := os.Lstat(rec); !os.IsNotExist(serr) {
		t.Fatalf("a partial record was left at %s: %v", rec, serr)
	}
	for _, want := range []string{"Deletion record (copy it now", "Status: complete", "The deletion record could not be saved to " + rec + ": it is shown above"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("the result does not say %q:\n%s", want, out.String())
		}
	}
}
