package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/lockfile"
)

// endsAt is a scripted person whose input ends (Ctrl-D; Ctrl-Z then Enter on Windows) at a
// prompt containing at.
type endsAt struct {
	*scripted
	at string
}

func (x *endsAt) line(prompt string) (string, error) {
	if strings.Contains(prompt, x.at) {
		x.prompts = append(x.prompts, prompt)
		return "", io.EOF
	}
	return x.scripted.line(prompt)
}

// covers: MA-1002, R36, R12
// The delete verbs speak the terminal's words (slice 4's adversarial pass, del-s4-A2-04, A2-06,
// A3-06, A3-08): input that ends at a question changes nothing and says so; a name holding a
// control character is shown written out and can be typed as shown; a Desktop control a line
// names is followed by the terminal's equivalent; an archive another delete holds is "being
// deleted", not "a backup is running".
func TestTheDeleteVerbsSpeakTheTerminalsWords(t *testing.T) {
	e := newCLIDel(t)

	// Positive twin: a lock held by an export keeps "A backup of this archive is running".
	b := e.plainArchive("Archive B")
	l, err := lockfile.AcquireAs(filepath.Join(b, lockfile.Name), "export")
	if err != nil {
		t.Fatal(err)
	}
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive B"}}, b, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("A backup of this archive is running", "(Folder: "+b+")"),
		assure.NoSideEffect(func() bool { return e.listed(b) && e.logText() == "" }))
	l.Release()

	// Another delete holds the lock: at the plan, and at step 1 (taken while the name is typed).
	l, err = lockfile.AcquireAs(filepath.Join(b, lockfile.Name), "delete")
	if err != nil {
		t.Fatal(err)
	}
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive B"}}, b, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("being deleted by another MailArchive window or terminal"),
		assure.NoSideEffect(func() bool { return e.listed(b) && e.logText() == "" && !strings.Contains(errText(err), "backup") }))
	l.Release()
	var held *lockfile.Lock
	h := &hooked{scripted: &scripted{isAttended: true, answers: []string{"Archive B"}}, at: "Type the folder's name", hook: func() {
		held, _ = lockfile.AcquireAs(filepath.Join(b, lockfile.Name), "delete")
	}}
	var out bytes.Buffer
	err = deleteArchive(h, b, "", "", &out)
	if held != nil {
		held.Release()
	}
	if exitCode(err) != 1 || !strings.Contains(out.String(), "being deleted by another MailArchive window or terminal") || strings.Contains(out.String(), "A backup of this archive is running") || !e.listed(b) {
		t.Fatalf("another delete holding the lock at step 1: exit %d (%v)\n%s", exitCode(err), err, out.String())
	}

	// Input that ends at a question or at the name: nothing changed, said plainly.
	before, logBefore := tree(t, b), e.logText()
	for _, c := range []struct {
		name string
		run  func(a asker) error
	}{
		{"Type the folder's name", func(a asker) error { return deleteArchive(a, b, "", "", io.Discard) }},
		{"Remove from list?", func(a asker) error { return unlistArchive(a, b, io.Discard) }},
	} {
		err := deleteOrUnlist(c.run, &endsAt{scripted: &scripted{isAttended: true}, at: c.name})
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("no answer was typed (the input ended), so nothing was changed"),
			assure.NoSideEffect(func() bool { return sameTree(before, tree(t, b)) && e.listed(b) && e.logText() == logBefore }))
		if strings.Contains(errText(err), "EOF") {
			t.Fatalf("input that ended at %s says EOF: %v", c.name, err)
		}
	}

	// A name holding a control character: shown written out, and typed as shown it deletes.
	odd := e.plainArchive("Arch\u0085ive")
	a := &scripted{isAttended: true, answers: []string{`Arch\u0085ive`}}
	out.Reset()
	if err := deleteArchive(a, odd, "", "", &out); err != nil {
		t.Fatalf("delete-archive of a name with a control character: %v\n%s", err, a.transcript())
	}
	assure.Reached(t, prompted(a, `Type the folder's name, Arch\u0085ive, to delete it`), "the name written out")
	if _, err := os.Lstat(filepath.Join(odd, ".mailarchive-manifest.json")); !os.IsNotExist(err) || e.listed(odd) || !strings.Contains(out.String(), "Deleted.") {
		t.Fatalf("the archive was not deleted: %v\n%s", err, out.String())
	}

	// A line naming a Desktop control is followed by the terminal's equivalent: the archive in
	// use (Switch, Add another archive), and a delete stopped part way (Delete again).
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = b }); err != nil {
		t.Fatal(err)
	}
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, b, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("This is the archive in use", "Switching archives, Add another archive and Settings are in MailArchive Desktop."),
		assure.NoSideEffect(func() bool { return e.listed(b) }))
	if err := desktop.UpdateSettings(e.settingsPath(), "test", func(s *desktop.Settings) { s.Out = "" }); err != nil {
		t.Fatal(err)
	}
	var release func()
	beforeDeleteStep = func(step int) {
		if step == 8 {
			release = e.desktopRunning("")
		}
	}
	out.Reset()
	err = deleteArchive(&scripted{isAttended: true, answers: []string{"Archive B"}}, b, "", "", &out)
	beforeDeleteStep = func(int) {}
	if release != nil {
		release()
	}
	if exitCode(err) != 3 || !strings.Contains(out.String(), "At this terminal, Delete again is: mailarchive delete-archive -out "+encmark.CommandPath(b)) {
		t.Fatalf("a stop part way does not give the terminal's Delete again: exit %d\n%s", exitCode(err), out.String())
	}
}

// deleteOrUnlist runs a verb for a person.
func deleteOrUnlist(run func(a asker) error, a asker) error { return run(a) }
