package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/schedule"
)

// covers: MA-1000, R36, R12
// The delete verbs watch Ctrl-C and the terminate signal from start to end (slice 4's
// adversarial pass, del-s4-A2-01, A2-07, A2-08, A3-01; conformance 4): while planning a signal
// stops the verb at once with nothing changed; at a question the prompt tells it; after the
// started line and before step 2 it is held and the delete refuses before step 2, its finished
// line written; from step 2 on it stops at once and names the row's own spelling to finish it;
// while a finished delete's record is saved it prints the record and keeps the delete's code;
// while the plan is saved, or the saved file's folder probed, the file just made is removed
// before the verb exits; once the delete has returned it changes nothing, and the record is
// printed (del-s4 residuals 1, 2); while Remove from list acts it waits for it.
func TestTheDeleteVerbsWatchSignalsFromStartToEnd(t *testing.T) {
	e := newCLIDel(t)
	logPath, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}

	// The real watch subscribes Ctrl-C and the terminate signal.
	var subscribed []os.Signal
	notifyDeleteSignals = func(_ chan<- os.Signal, sigs ...os.Signal) { subscribed = append(subscribed, sigs...) }
	startVerbWatch(io.Discard, io.Discard, "delete-archive").stop()
	if !slices.Contains(subscribed, os.Signal(os.Interrupt)) || !slices.Contains(subscribed, os.Signal(syscall.SIGTERM)) {
		t.Fatalf("the watch subscribes %v, want Ctrl-C and the terminate signal", subscribed)
	}
	f := e.fakeSignals()

	// Positive twin: with no signal the delete completes, and the verb's own watch is where the
	// verb is: reading while it plans, asking at the name, changing from step 2 on.
	p := e.plainArchive("Archive S0")
	phases := map[string]watchPhase{}
	deleteInstalled = func() []schedule.InstalledEntry {
		if _, ok := phases["plan"]; !ok {
			phases["plan"] = f.phase()
		}
		return nil
	}
	beforeDeleteStep = func(step int) {
		if step == 4 {
			phases["step 4"] = f.phase()
		}
	}
	h := &hooked{scripted: &scripted{isAttended: true, answers: []string{"Archive S0"}}, at: "Type the folder's name", hook: func() { phases["name"] = f.phase() }}
	if err := deleteArchive(h, p, "", "", io.Discard); err != nil {
		t.Fatalf("a delete with no signal: %v", err)
	}
	assure.Reached(t, !e.listed(p), "the delete with no signal")
	for at, want := range map[string]watchPhase{"plan": phaseReading, "name": phaseAsking, "step 4": phaseChanging} {
		if got, ok := phases[at]; !ok || got != want {
			t.Fatalf("the watch at the %s is %v (seen %v), want %v", at, got, ok, want)
		}
	}
	beforeDeleteStep = func(int) {}

	// At a question the prompt tells the signal; the watch does not exit.
	q := e.plainArchive("Archive S1")
	beforeQ := tree(t, q)
	h = &hooked{scripted: &scripted{isAttended: true, interrupt: "Type the folder's name"}, at: "Type the folder's name", hook: func() { f.send(t, os.Interrupt) }}
	err = interrupted(deleteArchive(h, q, "", "", io.Discard), io.Discard)
	if exitCode(err) != 130 || f.exited() != -1 || !sameTree(beforeQ, tree(t, q)) || strings.Contains(e.logText(), q) {
		t.Fatalf("Ctrl-C at the name: exit %d (%v)", exitCode(err), err)
	}

	// While planning: stopped at once, nothing changed.
	planned := false
	deleteInstalled = func() []schedule.InstalledEntry {
		if !planned {
			planned = true
			f.send(t, syscall.SIGTERM)
		}
		return nil
	}
	_ = deleteArchive(&scripted{isAttended: true, answers: []string{"not the name"}}, q, "", "", io.Discard)
	if code := f.exited(); code != 143 || !strings.Contains(f.said.String(), "delete-archive was stopped by a terminate signal; nothing was changed") {
		t.Fatalf("a terminate signal while planning: exit %d\n%s", code, f.said.String())
	}

	// After the started line, before step 2 (step 1's re-check of the plan): held, then the
	// delete refuses before step 2 with nothing changed, its finished line written, and exits
	// with the signal's code.
	s := e.plainArchive("Archive S2")
	entry := schedule.DefaultNameFor(s)
	e.sched.installed[entry] = true
	beforeS := tree(t, s)
	armed, sent := false, false
	deleteInstalled = func() []schedule.InstalledEntry {
		if armed && !sent {
			sent = true
			f.send(t, os.Interrupt)
		}
		return nil
	}
	h = &hooked{scripted: &scripted{isAttended: true, answers: []string{"Archive S2"}}, at: "Type the folder's name", hook: func() { armed = true }}
	var out bytes.Buffer
	err = deleteArchive(h, s, "", "", &out)
	afterS := tree(t, s)
	delete(afterS, filepath.Join(s, ".mailarchive.lock"))
	assure.Refused(t, exitCode(err), out.String(), assure.Code(130),
		assure.Names("Stopped by Ctrl-C before anything was deleted, so nothing was changed."),
		assure.NoSideEffect(func() bool {
			u, uerr := archdelete.UnfinishedDelete(logPath, s, runtime.GOOS)
			return sent && f.exited() == -1 && sameTree(beforeS, afterS) && e.listed(s) && e.sched.installed[entry] &&
				uerr == nil && u == nil && strings.Contains(e.logText(), s)
		}))
	if !strings.Contains(f.said.String(), "stopping: the delete stops before it changes anything") {
		t.Fatalf("the held signal was not acknowledged:\n%s", f.said.String())
	}
	deleteInstalled = func() []schedule.InstalledEntry { return nil }

	// From step 2 on: stopped at once, did not finish; the command to finish it names the row's
	// own spelling, not the link it was started through.
	c := e.plainArchive("Archive S3")
	spelling := c
	if runtime.GOOS != "windows" {
		spelling = filepath.Join(t.TempDir(), "MyMail")
		if err := os.Symlink(c, spelling); err != nil {
			t.Fatal(err)
		}
	}
	e.sched.installed[schedule.DefaultNameFor(c)] = true
	e.sched.onRemove = func(string) { f.send(t, os.Interrupt) }
	saidBefore := len(f.said.String())
	_ = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive S3"}}, spelling, "", "", io.Discard)
	e.sched.onRemove = nil
	said := f.said.String()[saidBefore:]
	if code := f.exited(); code != 130 || !strings.Contains(said, "did not finish") || !strings.Contains(said, "mailarchive delete-archive -out "+encmark.CommandPath(c)) {
		t.Fatalf("Ctrl-C after step 2 began: exit %d\n%s", code, said)
	}
	if spelling != c && strings.Contains(said, encmark.CommandPath(spelling)) {
		t.Fatalf("the stop sends the person back to the link they typed:\n%s", said)
	}

	// While a finished delete's record is saved: the record printed, the file named, the
	// delete's own code.
	d := e.plainArchive("Archive S4")
	recFile := filepath.Join(t.TempDir(), "record.txt")
	openSavedFile = func(path string) (*os.File, error) {
		f.send(t, os.Interrupt)
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	stdout := &lockedBuffer{}
	saidBefore = len(f.said.String())
	_ = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive S4"}}, d, "", recFile, stdout)
	openSavedFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	said = f.said.String()[saidBefore:]
	if code := f.exited(); code != 0 || !strings.Contains(stdout.String(), "Deletion record (copy it now") || !strings.Contains(stdout.String(), "Status: complete") ||
		!strings.Contains(said, "may be incomplete") || !strings.Contains(said, recFile) {
		t.Fatalf("Ctrl-C while the record is saved: exit %d\n%s\n%s", code, stdout.String(), said)
	}

	// While -plan-out is being written (the file just made): the file is removed before the verb
	// exits, nothing deleted (del-s4 residual 1).
	pl := e.plainArchive("Archive S6")
	beforePl := tree(t, pl)
	planFile := filepath.Join(t.TempDir(), "plan.txt")
	planLeft := true
	f.atExit = func() {
		_, err := os.Lstat(planFile)
		planLeft = err == nil
	}
	openSavedFile = func(path string) (*os.File, error) {
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			fh.WriteString("Status: planned, nothing del")
			f.send(t, os.Interrupt)
		}
		return fh, err
	}
	saidBefore = len(f.said.String())
	_ = deleteArchive(&scripted{isAttended: true}, pl, planFile, "", io.Discard)
	code := f.waitExit()
	openSavedFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	said = f.said.String()[saidBefore:]
	if code != 130 || planLeft || !strings.Contains(said, "stopped by Ctrl-C while saving the plan to "+planFile+": the unfinished file was removed. Nothing was deleted.") ||
		!sameTree(beforePl, tree(t, pl)) || !e.listed(pl) {
		t.Fatalf("Ctrl-C while the plan is saved: exit %d, file left at exit %v\n%s", code, planLeft, said)
	}

	// While the saved file's folder is probed (the probe just made): the probe is removed before
	// the verb exits, nothing changed.
	var probe string
	probeLeft := true
	f.atExit = func() {
		_, err := os.Lstat(probe)
		probeLeft = probe == "" || err == nil
	}
	openProbeFile = func(path string) (*os.File, error) {
		fh, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			probe = path
			f.send(t, syscall.SIGTERM)
		}
		return fh, err
	}
	saidBefore = len(f.said.String())
	_ = deleteArchive(&scripted{isAttended: true}, pl, filepath.Join(t.TempDir(), "plan.txt"), "", io.Discard)
	code = f.waitExit()
	openProbeFile = func(path string) (*os.File, error) {
		return os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	}
	f.atExit = nil
	said = f.said.String()[saidBefore:]
	if code != 143 || probeLeft || !strings.Contains(said, "delete-archive was stopped by a terminate signal; nothing was changed") {
		t.Fatalf("a terminate signal while the folder is probed: exit %d, probe %q left at exit %v\n%s", code, probe, probeLeft, said)
	}

	// Between the delete's return and its result: the delete is over, so the signal changes
	// nothing; the result and the deletion record are printed and the exit is the delete's own
	// (del-s4 residual 2).
	fin := e.plainArchive("Archive S7")
	afterDeleteReturned = func() { f.send(t, os.Interrupt) }
	stdout = &lockedBuffer{}
	saidBefore = len(f.said.String())
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive S7"}}, fin, "", "", stdout)
	afterDeleteReturned = func() {}
	said = f.said.String()[saidBefore:]
	if err != nil || f.exited() != -1 || strings.Contains(said, "did not finish") || !strings.Contains(stdout.String(), "Deleted.") ||
		!strings.Contains(stdout.String(), "Deletion record (copy it now") || !strings.Contains(stdout.String(), "Status: complete") || e.listed(fin) {
		t.Fatalf("Ctrl-C just after the delete returned: %v\n%s\n%s", err, said, stdout.String())
	}

	// While Remove from list acts: the signal waits for it to finish.
	g := e.plainArchive("Archive S5")
	e.sched.installed[schedule.DefaultNameFor(g)] = true
	e.sched.onRemove = func(string) { f.send(t, os.Interrupt) }
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, g, io.Discard)
	e.sched.onRemove = nil
	if err != nil || f.exited() != -1 || e.listed(g) || !strings.Contains(f.said.String(), "Remove from list is finishing what it began") {
		t.Fatalf("Ctrl-C while Remove from list acts: %v, listed %v\n%s", err, e.listed(g), f.said.String())
	}
}
