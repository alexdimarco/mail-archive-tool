package main

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
)

// covers: MA-988, R36, R12
// delete-archive and unlist-archive run only with a person at a terminal and never while
// MailArchive Desktop runs in any logon session of the account (design-archive-delete §3.3):
// each refusal comes before the plan or the question, says what to do, and changes nothing.
func TestTheDeleteVerbsRunOnlyAtATerminalWithNoDesktopRunning(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("Archive A")
	before := tree(t, dir)
	unchanged := func() bool { return sameTree(before, tree(t, dir)) && e.listed(dir) && e.logText() == "" }

	// Positive twin: at a terminal with no Desktop, the plan is shown and the name asked for.
	// A Desktop's lock file left by one that ended is no Desktop.
	if err := os.WriteFile(filepath.Join(e.lockDir, "desktop.lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a := &scripted{isAttended: true, answers: []string{""}}
	_ = deleteArchive(a, dir, "", "", io.Discard)
	assure.Reached(t, prompted(a, "Type the folder's name, Archive A, to delete it"), "the plan shown at a terminal with no Desktop running")
	if !strings.Contains(a.transcript(), dir) {
		t.Fatalf("the plan does not name the folder:\n%s", a.transcript())
	}
	q := &scripted{isAttended: true, answers: []string{"n"}}
	_ = unlistArchive(q, dir, io.Discard)
	assure.Reached(t, prompted(q, "Remove from list?"), "Remove from list's question at a terminal with no Desktop running")
	// That delete was refused at its name and that Remove from list declined: nothing changed.
	before = tree(t, dir)
	if !e.listed(dir) {
		t.Fatal("the positive twins removed the row")
	}

	verbs := map[string]func(asker) error{
		"delete-archive": func(a asker) error { return deleteArchive(a, dir, "", "", io.Discard) },
		"unlist-archive": func(a asker) error { return unlistArchive(a, dir, io.Discard) },
	}

	// No terminal: each verb's own words, the Desktop's row actions named, never the
	// Encryption card; nothing asked.
	// Each verb names only its own control, the program's name once (MA-1028, friction B12).
	control := map[string][2]string{"delete-archive": {"Delete…", "Remove from list"}, "unlist-archive": {"Remove from list", "Delete…"}}
	for verb, run := range verbs {
		a := &scripted{isAttended: false}
		err := run(a)
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
			assure.Names(verb+" needs you at a terminal", "Run it in a terminal window (or use "+control[verb][0]+" on its row of Your archives in MailArchive Desktop)."),
			assure.Forbid("Encryption card", "panic", "goroutine", "mailarchive "+verb, control[verb][1]), assure.NoSideEffect(unchanged))
		if len(a.prompts) != 0 || len(a.said) != 0 {
			t.Fatalf("%s without a terminal still asked or said: %s", verb, a.transcript())
		}
	}
	// The key verbs keep the Encryption card.
	if err := keyVerbTerminal("forget-archive", nil); !strings.Contains(err.Error(), "use the Encryption card in MailArchive Desktop") {
		t.Fatalf("forget-archive's terminal refusal lost the Encryption card: %v", err)
	}
	// The real program, with no terminal (a pipe).
	code, stderr := runCLI("delete-archive", "-out", dir)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("mailarchive: delete-archive needs you at a terminal", "Your archives in MailArchive Desktop"),
		assure.Forbid("mailarchive: mailarchive"), assure.NoSideEffect(unchanged))
	code, stderr = runCLI("unlist-archive", "-out", dir)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("mailarchive: unlist-archive needs you at a terminal", "Your archives in MailArchive Desktop"),
		assure.Forbid("mailarchive: mailarchive"), assure.NoSideEffect(unchanged))

	// A Desktop running in the account's own session, then in another logon session.
	for _, session := range []string{"", "2"} {
		release := e.desktopRunning(session)
		for verb, run := range verbs {
			a := &scripted{isAttended: true, answers: []string{"Archive A", "y"}}
			err := run(a)
			assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
				assure.Names("MailArchive Desktop is running", "Use the Desktop, or shut it down first"), assure.NoSideEffect(unchanged))
			if len(a.prompts) != 0 {
				t.Fatalf("%s with a Desktop running in session %q still asked: %s", verb, session, a.transcript())
			}
		}
		release()
	}

	// Lock names are matched without regard to case where the file system ignores it (Windows,
	// and macOS by default): a session lock spelled in capitals is that session's lock.
	if caseInsensitive(t, e.lockDir) {
		l, err := lockfile.AcquireAs(filepath.Join(e.lockDir, "DESKTOP-S3.LOCK"), "desktop")
		if err != nil {
			t.Fatal(err)
		}
		a := &scripted{isAttended: true, answers: []string{"Archive A"}}
		err = deleteArchive(a, dir, "", "", io.Discard)
		l.Release()
		assure.Refused(t, exitCode(err), errText(err), assure.Code(1), assure.Names("MailArchive Desktop is running"), assure.NoSideEffect(unchanged))
	}

	// A lock folder that cannot be found or read: it cannot tell, so it changes nothing.
	desktopLockDir = func() (string, error) { return "", errors.New("%LOCALAPPDATA% is not set") }
	err := deleteArchive(&scripted{isAttended: true, answers: []string{"Archive A"}}, dir, "", "", io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("cannot tell whether MailArchive Desktop is running", "%LOCALAPPDATA% is not set"), assure.NoSideEffect(unchanged))
	notDir := filepath.Join(t.TempDir(), "a file")
	os.WriteFile(notDir, nil, 0o600)
	desktopLockDir = func() (string, error) { return notDir, nil }
	err = unlistArchive(&scripted{isAttended: true, answers: []string{"y"}}, dir, io.Discard)
	assure.Refused(t, exitCode(err), errText(err), assure.Code(1),
		assure.Names("cannot tell whether MailArchive Desktop is running"), assure.NoSideEffect(unchanged))
}

// caseInsensitive reports whether dir's file system ignores the case of names.
func caseInsensitive(t *testing.T, dir string) bool {
	t.Helper()
	p := filepath.Join(dir, "case-probe")
	if err := os.WriteFile(p, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	defer os.Remove(p)
	_, err := os.Stat(filepath.Join(dir, "CASE-PROBE"))
	return err == nil
}
