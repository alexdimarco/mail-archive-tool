package desktop

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/state"
)

// mailIn makes dir a Thunderbird store holding one message.
func mailIn(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	box := "From - Mon Mar  3 09:00:00 2025\nFrom: x@example.com\nSubject: s\nDate: Mon, 03 Mar 2025 09:00:00 +0000\nMessage-ID: <" + filepath.Base(dir) + "@ex>\n\nbody\n\n"
	if err := os.WriteFile(filepath.Join(dir, "INBOX"), []byte(box), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "INBOX.msf"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
}

// archiveOf backs inputs up into a new archive, with auto-discovery when auto is set.
func archiveOf(t *testing.T, auto bool, inputs ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "arch")
	if _, err := app.Run(context.Background(), app.Options{Inputs: inputs, Auto: auto, Out: out, Mode: export.Incremental}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	return out
}

// covers: MA-1473, MA-581, R14
// The Desktop's schedule rebuild (reconstructLocalJob: a Settings create with no job file yet, and
// the command-line-schedule conversion check) keeps a gone input of an -auto job that the run's own
// rule would skip (autoplace.GoneSkippable), when it is one of the archive's recorded stores
// (autoplace.GoneSourceID against the manifest, A2), so the run then skips it and warns on every run
// (docs/design-1.7.5-fixes.md §1.4). Positive twin first: with every input there it rebuilds as
// before. It still refuses when no listed input exists, for a gone input the rule does not skip
// (outside the places, its group folder gone), for one that is not a recorded store, and for any
// gone input of a job without -auto.
func TestTheRebuildKeepsAGoneMailboxTheRunWouldSkip(t *testing.T) {
	a, b, profile := goneHome(t)
	mailIn(t, a)
	mailIn(t, b)
	out := archiveOf(t, true, a, b)
	j, ok := reconstructLocalJob(out)
	if !ok || len(j.Inputs) != 2 {
		t.Fatalf("positive twin: every input there rebuilds: %v %+v", ok, j)
	}

	os.RemoveAll(b)
	j, ok = reconstructLocalJob(out)
	if !ok || !j.Auto || len(j.Inputs) != 2 || j.Inputs[0] != a || j.Inputs[1] != b {
		t.Fatalf("the rebuild must keep the gone input %s of an -auto job: %v %+v", b, ok, j)
	}

	// Through Settings: a create with no job file yet saves, and the job file it writes keeps it.
	g := newA3Rig(t, out, "darwin")
	if p := g.page(); !strings.Contains(p, `id="schedInterval"`) {
		t.Fatal("Settings does not offer to create the schedule")
	}
	if body := g.post(`{"interval":"weekly","time":"03:00"}`); !strings.Contains(body, `"ok":true`) {
		t.Fatalf("a Settings create with a gone mailbox: %s", body)
	}
	if saved, err := job.Read(g.jobPath); err != nil || len(saved.Inputs) != 2 || saved.Inputs[1] != b {
		t.Fatalf("the job file must keep the gone input: %+v %v", saved, err)
	}
	// The conversion check of a command-line schedule: its words offer the Desktop's own backup.
	os.Remove(g.jobPath)
	delete(g.fake.tasks, out)
	writeCommandLineSchedule(t, out)
	g.fake.tasks[out] = true
	if p := g.page(); !strings.Contains(p, "To get Run now, click Remove below") {
		t.Error("the conversion check must find the archive's backup can be rebuilt")
	}

	refused := func(t *testing.T, what, out string) {
		t.Helper()
		if j, ok := reconstructLocalJob(out); ok {
			t.Errorf("%s: the rebuild must refuse, got %+v", what, j)
		}
	}
	// No listed input exists (the archive's own store a too).
	os.RemoveAll(a)
	refused(t, "no listed input there", out)
	mailIn(t, a)

	// A gone input the rule does not skip: outside the places, and one whose group folder is gone.
	usb := filepath.Join(t.TempDir(), "USB", "Inbox")
	mailIn(t, usb)
	outside := archiveOf(t, true, a, usb)
	os.RemoveAll(usb)
	refused(t, "a gone input outside the places", outside)
	second := filepath.Join(filepath.Dir(profile), "efgh5678.second", "ImapMail", "imap.qzc.example")
	mailIn(t, second)
	group := archiveOf(t, true, a, second)
	os.RemoveAll(filepath.Dir(second))
	refused(t, "a gone input whose group folder is gone", group)

	// A gone input that is not one of the archive's recorded stores (A2): b is gone while this
	// archive is made, so its manifest never records it.
	other := archiveOf(t, true, a)
	lr, _, _ := state.ReadLastRun(other)
	lr.Job = append(lr.Job, "-input", b)
	if err := state.WriteLastRun(other, lr); err != nil {
		t.Fatal(err)
	}
	refused(t, "a gone input not recorded by the archive", other)

	// Any gone input of a job without -auto.
	mailIn(t, b)
	plain := archiveOf(t, false, a, b)
	os.RemoveAll(b)
	refused(t, "a gone input of a job without -auto", plain)
}
