package main

import (
	"bytes"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
)

// covers: MA-1027, R36, R18
// `status` says when a delete of the archive did not finish (the 1.5.3 friction review's B2: it
// warned only "no schedule" and blamed a missing -raw): with the manifest still there, RED "a
// delete of this archive did not finish — run mailarchive delete-archive -out DIR again, or click
// Delete… on its row in MailArchive Desktop, to finish it", and no "no schedule is recorded"
// advice; with the archive's own files gone, its "no archive at DIR" says the same in place of
// "run an export into it first". Positive twin first: before the delete, nothing of the kind.
func TestStatusSaysADeleteDidNotFinish(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Half Deleted")
	before, code := statusOf(t, dir)
	if code != 0 || strings.Contains(before, "did not finish") {
		t.Fatalf("the positive twin:\n%s", before)
	}
	want := "a delete of this archive did not finish — run mailarchive delete-archive -out " + encmark.CommandPath(dir) + " again, or click Delete… on its row in MailArchive Desktop, to finish it"

	// Stopped before step 4 (a Desktop appeared): the manifest is still there.
	release := func() {}
	beforeDeleteStep = func(step int) {
		if step == 4 {
			release = e.desktopRunning("2")
		}
	}
	var out bytes.Buffer
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Half Deleted"}}, dir, "", "", &out)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 {
		t.Fatalf("the delete should stop part way: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
	release() // the Desktop shut down again
	got, code := statusOf(t, dir)
	if code != 0 || !strings.Contains(got, "RED") || !strings.Contains(got, want) || strings.Contains(got, "no schedule is recorded") {
		t.Fatalf("status of a delete stopped before step 4:\n%s", got)
	}

	// Stopped before step 8: the archive's own files are gone, the person's file keeps the folder.
	d2 := e.plainArchive("Files Gone")
	want2 := strings.ReplaceAll(want, encmark.CommandPath(dir), encmark.CommandPath(d2))
	beforeDeleteStep = func(step int) {
		if step == 8 {
			e.desktopRunning("3")
		}
	}
	out.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Files Gone"}}, d2, "", "", &out)
	beforeDeleteStep = func(int) {}
	if exitCode(err) != 3 {
		t.Fatalf("the second delete should stop part way: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}
	got, code = statusOf(t, d2)
	assure.Refused(t, code, got, assure.Code(1), assure.Names("no archive at "+d2, want2),
		assure.NoSideEffect(func() bool { return !strings.Contains(got, "run an export into it first") }))
}
