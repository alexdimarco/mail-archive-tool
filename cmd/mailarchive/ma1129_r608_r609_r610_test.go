package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbengine"
)

// encryptedTB is an encrypted archive of tbMbox, and a password file for it.
func encryptedTB(t *testing.T, dir string) (out, pw string) {
	t.Helper()
	out = filepath.Join(dir, "Locked mail")
	pc, err := keyring.Create(out, flowPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := pc.Commit()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(dir, "src", "Mail")
	writeTB(t, filepath.Join(src, "Inbox"), tbMbox)
	opts := app.Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, KeepRaw: true}
	done, err := keyring.Apply(&opts, w)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	done()
	w.Destroy()
	pw = filepath.Join(dir, "pw.txt")
	writeTB(t, pw, flowPassword+"\n")
	return out, pw
}

// covers: MA-1129, R37, R40
// R6-08 (settled 2026-11-07): -record-out follows the saved file's rules. A record
// inside the encrypted archive is refused before anything is read or written, naming the
// archive and what to do, and no plaintext file appears there; positive twin: outside
// every archive it is saved, readable by its owner alone.
func TestRecordOutIsNeverInsideAnEncryptedArchive(t *testing.T) {
	e := newTBCLI(t, "")
	out, pw := encryptedTB(t, t.TempDir())
	inside := filepath.Join(out, "rec.txt")
	before := e.state()
	code, _, stderr := e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-plan", "-password-file", pw, "-record-out", inside)
	_, statErr := os.Lstat(inside)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-record-out", "is inside the archive folder", "outside it"),
		assure.NoSideEffect(func() bool { return e.state() == before && os.IsNotExist(statErr) }))
	other, _ := encryptedTB(t, t.TempDir())
	inOther := filepath.Join(other, "rec.txt")
	code, _, stderr = e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-plan", "-password-file", pw, "-record-out", inOther)
	_, statErr = os.Lstat(inOther)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-record-out", "inside the encrypted archive", "outside every archive"),
		assure.NoSideEffect(func() bool { return e.state() == before && os.IsNotExist(statErr) }))

	outside := filepath.Join(t.TempDir(), "rec.txt")
	code, _, stderr = e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-plan", "-password-file", pw, "-record-out", outside)
	fi, err := os.Stat(outside)
	assure.Reached(t, code == 0 && err == nil && fi.Size() > 0 && (fi.Mode().Perm()&0o077 == 0 || isWindows()), fmt.Sprintf("positive twin: saved outside, owner-only: %d %v %q", code, err, stderr))
}

func isWindows() bool { return os.PathSeparator == '\\' }

// covers: MA-1129, R37, R40
// R6-09 (settled 2026-11-07): a second Ctrl-C mid-write ends the restore at once (exit 130
// as before), and first says what may be left and where: the unfinished folder's name, the
// Local Folders path, that the next restore removes it, and — for an encrypted archive —
// that it may hold decrypted mail. R6-10: the check's pause is explained ("Checking what
// was written"). The exit itself is the seam (the process cannot end inside a test).
func TestASecondCtrlCSaysWhatIsLeft(t *testing.T) {
	e := newTBCLI(t, "")
	out, pw := encryptedTB(t, t.TempDir())
	exited := make(chan int, 1)
	exitWas := restoreTBExit
	restoreTBExit = func(code int) { exited <- code }
	defer func() { restoreTBExit = exitWas }()
	sigs := make(chan os.Signal, 2)
	runWas := restoreTBRun
	exitCodeSeen := 0
	restoreTBRun = func(ctx context.Context, p *tbengine.Plan, s tbengine.Start) (*tbengine.Result, error) {
		inner, first := s.Progress, true
		s.Progress = func(g tbengine.Progress) {
			if inner != nil {
				inner(g)
			}
			if first {
				first = false
				sigs <- os.Interrupt
				sigs <- os.Interrupt
				select { // the watcher takes both: the first stops the run, the second "exits"
				case exitCodeSeen = <-exited:
				case <-time.After(10 * time.Second):
				}
			}
		}
		return runWas(ctx, p, s)
	}
	defer func() { restoreTBRun = runWas }()
	errb := &syncBuf{}
	code := runTBWith(t, e, sigs, errb, "-out", out, "-profile", e.profile, "-yes", "-not-encrypted-ok", "-password-file", pw)
	said := errb.String()
	assure.Reached(t, exitCodeSeen == 130 && code == 130, fmt.Sprintf("the second Ctrl-C exits 130: %d, then %d", exitCodeSeen, code))
	assure.Reached(t, strings.Contains(said, "stopped at once") && strings.Contains(said, "Restored — Locked mail.sbd") && strings.Contains(said, e.local) &&
		strings.Contains(said, "the next restore into this profile removes it") && strings.Contains(said, "decrypted mail"),
		fmt.Sprintf("it says what may be left and where: %q", said))

	e2 := newTBCLI(t, "")
	errb = &syncBuf{}
	code = runTBWith(t, e2, nil, errb, "-out", e2.archive, "-profile", e2.profile, "-yes")
	assure.Reached(t, code == 0 && strings.Contains(errb.String(), "Checking what was written"), fmt.Sprintf("the check's pause is said: %d %q", code, errb.String()))
}

// runTBWith is e.run with stderr written to w (safe for the signal watcher's goroutine).
func runTBWith(t *testing.T, e *tbCLI, sigs chan os.Signal, w io.Writer, args ...string) int {
	t.Helper()
	fs := flag.NewFlagSet("mailarchive restore-thunderbird", flag.ContinueOnError)
	o := restoreTBFlags(fs)
	k := readKeyFlags(fs)
	if err := fs.Parse(args); err != nil {
		t.Fatal(err)
	}
	err := restoreTBWith(&scripted{}, o, k, fs, io.Discard, w, sigs)
	if err != nil {
		if _, ok := err.(*exitError); !ok {
			io.WriteString(w, "mailarchive: "+err.Error()+"\n")
		}
	}
	return exitCode(err)
}
