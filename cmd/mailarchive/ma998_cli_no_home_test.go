package main

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/instance"
)

// covers: MA-998, R36
// With no home folder set, delete-archive refuses before anything, in plain words with a final
// period and what to do, never Go's own error text (the 1.5.3 re-walk's N20: "(cannot determine
// your per-account folder: neither $XDG_CONFIG_HOME nor $HOME are defined), so nothing was changed"
// with no period). Positive twin first: with the home folder set, the same delete is planned.
func TestNoHomeFolderIsSaidPlainly(t *testing.T) {
	e := newCLIDel(t)
	dir := e.plainArchive("No Home")
	desktopLockDir = instance.Dir // the real folder that tells whether a Desktop runs

	// Positive twin: the home folder set, the plan is shown (no name typed: nothing deleted).
	a := &scripted{isAttended: true}
	var out bytes.Buffer
	_ = deleteArchive(a, dir, "", "", &out)
	if !strings.Contains(a.transcript(), "MailArchive will permanently delete") {
		t.Fatalf("the twin did not plan:\n%s\n%s", a.transcript(), out.String())
	}

	before := tree(t, dir)
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "LOCALAPPDATA", "APPDATA", "USERPROFILE"} {
		t.Setenv(k, "")
	}
	a = &scripted{isAttended: true, answers: []string{"No Home"}}
	out.Reset()
	err := deleteArchive(a, dir, "", "", &out)
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	want := "MailArchive could not find your home folder, so it cannot tell whether MailArchive Desktop is running for your account; nothing was changed. Check that your home folder is set (HOME, or Local AppData on Windows), then try again."
	assure.Refused(t, exitCode(err), msg, assure.Code(1), assure.Names(want),
		assure.NoSideEffect(func() bool {
			return msg == want && !strings.Contains(msg, "$HOME") && !strings.Contains(a.transcript(), "MailArchive will permanently delete") && sameTree(before, tree(t, dir))
		}))

	// Another reason the folder cannot be found keeps its cause, never "set HOME" (the settle
	// pass's LOW-5: on Linux a relative XDG_CONFIG_HOME with HOME set). Its error as instance.Dir
	// gives it there, so the words are proven on every platform.
	rel := fmt.Errorf("%w: %w", instance.ErrNoAccountFolder, errors.New("path in $XDG_CONFIG_HOME is relative"))
	if got := cannotTell(rel).Error(); got != "MailArchive cannot tell whether MailArchive Desktop is running for your account (cannot determine your per-account folder: path in $XDG_CONFIG_HOME is relative), so nothing was changed." {
		t.Fatalf("a relative XDG_CONFIG_HOME: %q", got)
	}
}
