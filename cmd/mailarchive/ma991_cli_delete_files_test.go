package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-991, R36, R12
// -plan-out FILE saves the plan and deletes nothing; -record-out FILE saves the deletion record.
// Each is checked before the plan is shown and before step 0, created only as a new file and only
// when its text is final, not owner-only; a file that cannot be written leaves its text on the
// terminal (design-archive-delete §8.1 "Files on the command line").
func TestDeleteArchiveSavesThePlanAndTheRecordAsNewFiles(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Archive H")
	saveDir := t.TempDir()

	// Positive twin: the plan saved by a plan-only run, nothing deleted, nothing logged.
	before := tree(t, dir)
	planFile := filepath.Join(saveDir, "plan.txt")
	a := &scripted{isAttended: true}
	var out bytes.Buffer
	if err := deleteArchive(a, dir, planFile, "", &out); err != nil {
		t.Fatalf("a plan-only run: %v\n%s", err, a.transcript())
	}
	plan, err := os.ReadFile(planFile)
	if err != nil || !bytes.HasPrefix(plan, []byte("Status: planned, nothing deleted\n")) || !bytes.Contains(plan, []byte("Record-SHA256: ")) {
		t.Fatalf("the saved plan: %v\n%s", err, plan)
	}
	if len(a.prompts) != 0 || !sameTree(before, tree(t, dir)) || !e.listed(dir) || e.logText() != "" {
		t.Fatalf("a plan-only run asked, deleted or logged something:\n%s\n%s", a.transcript(), e.logText())
	}
	if !strings.Contains(strings.Join(a.said, "\n"), dir) || !strings.Contains(out.String(), "The plan was saved to "+planFile) {
		t.Fatalf("a plan-only run did not show the plan or say where it was saved:\n%s\n%s", a.transcript(), out.String())
	}
	sharedMode(t, planFile)

	// The record saved by -record-out, the very bytes whose digest deletions.log keeps.
	recFile := filepath.Join(saveDir, "record.txt")
	out.Reset()
	if err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive H"}}, dir, "", recFile, &out); err != nil {
		t.Fatalf("a delete with -record-out: %v\n%s", err, out.String())
	}
	rec, err := os.ReadFile(recFile)
	if err != nil || !bytes.HasPrefix(rec, []byte("Status: complete\n")) || !recordDigestOK(string(rec), e.logText()) {
		t.Fatalf("the saved record: %v\n%s", err, rec)
	}
	if !strings.Contains(out.String(), "The deletion record was saved to "+recFile) || strings.Contains(out.String(), "Status: complete") {
		t.Fatalf("a saved record is said, not printed:\n%s", out.String())
	}
	sharedMode(t, recFile)

	// Each refusal comes before the plan is shown and before step 0.
	k := e.plainArchive("Archive K")
	other := e.plainArchive("Archive L")
	beforeK := tree(t, k)
	logBefore := e.logText()
	unchanged := func() bool { return sameTree(beforeK, tree(t, k)) && e.listed(k) && e.logText() == logBefore }
	exists := filepath.Join(saveDir, "exists.txt")
	os.WriteFile(exists, []byte("keep me"), 0o644)
	cases := []struct {
		what, plan, rec string
		names           []string
	}{
		{"an existing file", exists, "", []string{"-plan-out", exists, "already exists"}},
		{"an existing record file", "", exists, []string{"-record-out", exists, "already exists"}},
		{"a path inside the folder", filepath.Join(k, "plan.txt"), "", []string{"-plan-out", "inside the archive folder"}},
		{"a path inside another known archive", "", filepath.Join(other, "record.txt"), []string{"-record-out", "inside the archive", other}},
		{"a folder that is not there", filepath.Join(saveDir, "no such folder", "plan.txt"), "", []string{"-plan-out", "no such folder", "does not exist"}},
		{"both files", filepath.Join(saveDir, "p2.txt"), filepath.Join(saveDir, "r2.txt"), []string{"-plan-out", "-record-out"}},
	}
	if runtime.GOOS != "windows" {
		link := filepath.Join(saveDir, "link.txt")
		if err := os.Symlink(filepath.Join(saveDir, "nowhere.txt"), link); err != nil {
			t.Fatal(err)
		}
		cases = append(cases, struct {
			what, plan, rec string
			names           []string
		}{"a link", "", link, []string{"-record-out", link, "already exists"}})
		if os.Geteuid() != 0 {
			ro := filepath.Join(t.TempDir(), "read-only")
			os.Mkdir(ro, 0o500)
			t.Cleanup(func() { os.Chmod(ro, 0o700) })
			cases = append(cases, struct {
				what, plan, rec string
				names           []string
			}{"a folder that cannot be written", filepath.Join(ro, "plan.txt"), "", []string{"-plan-out", ro, "cannot be written"}})
		}
	}
	for _, c := range cases {
		a := &scripted{isAttended: true, answers: []string{"Archive K"}}
		err := deleteArchive(a, k, c.plan, c.rec, &bytes.Buffer{})
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names(c.names...), assure.NoSideEffect(unchanged))
		if len(a.said) != 0 || len(a.prompts) != 0 {
			t.Fatalf("%s: the plan was shown or a question asked before the refusal:\n%s", c.what, a.transcript())
		}
	}
	if b, _ := os.ReadFile(exists); string(b) != "keep me" {
		t.Fatal("an existing file was written over")
	}
	if left, _ := filepath.Glob(filepath.Join(saveDir, ".mailarchive-probe-*")); len(left) != 0 {
		t.Fatalf("the writable-folder probe was left: %v", left)
	}

	// A plan-only run whose file cannot be written: the plan's text printed, exit 1.
	openSavedFile = func(string) (*os.File, error) { return nil, errors.New("the disk is full") }
	out.Reset()
	p3 := filepath.Join(saveDir, "p3.txt")
	err = deleteArchive(&scripted{isAttended: true}, k, p3, "", &out)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("The plan could not be saved to "+p3+": it is shown above", "the disk is full"),
		assure.NoSideEffect(unchanged))
	if !strings.Contains(out.String(), "Status: planned, nothing deleted") || !strings.Contains(out.String(), "Record-SHA256: ") {
		t.Fatalf("the plan that could not be saved was not shown:\n%s", out.String())
	}

	// A record that cannot be written after the delete: printed, said, and the exit stays the
	// deletion's.
	out.Reset()
	r3 := filepath.Join(saveDir, "r3.txt")
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive K"}}, k, "", r3, &out)
	if err != nil {
		t.Fatalf("a record that could not be saved changed the exit: %v\n%s", err, out.String())
	}
	res := out.String()
	if !strings.Contains(res, "The deletion record could not be saved to "+r3+": it is shown above") || !recordDigestOK(recordIn(res), e.logText()) {
		t.Fatalf("the record that could not be saved was not shown and said:\n%s", res)
	}
	if _, err := os.Lstat(r3); !os.IsNotExist(err) {
		t.Fatalf("a record that could not be written left a file: %v", err)
	}
}

// sharedMode fails unless path has the mode a new file meant to be shared gets here (0644 under
// this process's umask): the plan and the record are not made owner-only.
func sharedMode(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	probe := filepath.Join(t.TempDir(), "probe")
	f, err := os.OpenFile(probe, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	want, _ := os.Stat(probe)
	got, err := os.Stat(path)
	if err != nil || got.Mode().Perm() != want.Mode().Perm() {
		t.Fatalf("%s has mode %v, want %v (not owner-only)", path, got.Mode().Perm(), want.Mode().Perm())
	}
}
