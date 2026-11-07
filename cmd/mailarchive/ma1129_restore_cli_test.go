package main

import (
	"bytes"
	"context"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/tbengine"
	"mail-archive-tool/internal/tbrestore"
)

// The command line's restore-thunderbird (design-restore-thunderbird §10 as folded; slice
// R6), in-process with a scripted person at the terminal, as decrypt's tests run, and its
// help and its no-terminal refusal through the real program. Fake Thunderbird profiles in
// temp folders only, named with -profile; the Thunderbird process list (an OS probe — the
// person's own Thunderbird may be running here) answers "none".

const tbMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Ann <ann@example.org>\r\nSubject: first\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\nMessage-ID: <first@example.org>\r\n\r\nfirst body\r\n\r\n" +
	"From - Tue Jan 02 00:00:00 2024\r\n" +
	"From: Bob <bob@example.org>\r\nSubject: second\r\nDate: Tue, 02 Jan 2024 10:00:00 +0000\r\nMessage-ID: <second@example.org>\r\n\r\nsecond body\r\n"

type tbCLI struct {
	t       *testing.T
	archive string
	profile string
	local   string
}

// newTBCLI is a throwaway home, a real archive with its originals kept, and a fake
// profile; keyed: the archive is encrypted (its key in u, still unlocked).
func newTBCLI(t *testing.T, profileUnder string) *tbCLI {
	t.Helper()
	newCLIDel(t)
	t.Cleanup(tbrestore.SetProcessProbeForTest(func() (int, bool) { return 0, true }))
	e := &tbCLI{t: t, archive: filepath.Join(t.TempDir(), "Kept mail")}
	src := filepath.Join(t.TempDir(), "Mail")
	writeTB(t, filepath.Join(src, "Inbox"), tbMbox)
	if _, err := app.Run(context.Background(), app.Options{Inputs: []string{src}, Out: e.archive, Mode: export.Incremental, Index: true, KeepRaw: true}, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	e.makeProfile(profileUnder)
	return e
}

func (e *tbCLI) makeProfile(under string) {
	if under == "" {
		under = e.t.TempDir()
	}
	e.profile = filepath.Join(under, "p.default")
	writeTB(e.t, filepath.Join(e.profile, "prefs.js"), `user_pref("mail.accountmanager.localfoldersserver", "server1");`+"\n"+
		`user_pref("mail.server.server1.directory-rel", "[ProfD]Mail/Local Folders");`+"\n"+`user_pref("mail.server.server1.type", "none");`+"\n")
	os.Chmod(e.profile, 0o700)
	e.local = filepath.Join(e.profile, "Mail", "Local Folders")
	writeTB(e.t, filepath.Join(e.local, "Trash"), "From - x\nSubject: old\n\nkept\n")
	e.profile, _ = filepath.EvalSymlinks(e.profile)
	e.local, _ = filepath.EvalSymlinks(e.local)
}

func writeTB(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
}

// run runs restore-thunderbird with args, the person a, the signals sigs; it returns the
// exit code, stdout and stderr.
func (e *tbCLI) run(a asker, sigs chan os.Signal, args ...string) (int, string, string) {
	e.t.Helper()
	fs := flag.NewFlagSet("mailarchive restore-thunderbird", flag.ContinueOnError)
	o := restoreTBFlags(fs)
	k := readKeyFlags(fs)
	if err := fs.Parse(args); err != nil {
		e.t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err := restoreTBWith(a, o, k, fs, &stdout, &stderr, sigs)
	if err != nil {
		if _, ok := err.(*exitError); !ok {
			stderr.WriteString("mailarchive: " + err.Error() + "\n")
		}
	}
	return exitCode(err), stdout.String(), stderr.String()
}

// state is the profile's files and restores.log, byte for byte.
func (e *tbCLI) state() string {
	m := tree(e.t, e.profile)
	lp, _ := tbrestore.LogPath()
	b, _ := os.ReadFile(lp)
	var keys []string
	for k, v := range m {
		keys = append(keys, k+" "+v)
	}
	return strings.Join(sortedTB(keys), "\n") + "\nLOG " + string(b)
}

func sortedTB(s []string) []string {
	for i := range s {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
	return s
}

func (e *tbCLI) restored() bool {
	_, err := os.Lstat(filepath.Join(e.local, "Restored — Kept mail"))
	return err == nil
}

// covers: MA-1129, R40, R33
// The verb's flags and exit codes end to end: -plan prints the plan on stderr and writes
// nothing (exit 0, nothing on stdout); with no terminal and no -yes it is refused before
// anything (1); at a terminal "Restore? [y/N]" answered no restores nothing (1); -yes
// restores everything (0), the result on stdout, -record-out saved; a partial restore (an
// original gone) exits 3; Ctrl-C and a terminate signal stop it (130, 143), nothing added;
// -not-encrypted-ok on a plain archive is refused (1); -list-profiles lists the profiles.
func TestRestoreThunderbirdAtTheCommandLine(t *testing.T) {
	e := newTBCLI(t, "")
	before := e.state()
	code, stdout, stderr := e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-plan")
	reached(t, code == 0 && stdout == "" && strings.Contains(stderr, "Restored — Kept mail") && strings.Contains(stderr, tbrestore.WaitWords()), "-plan prints the plan on stderr: %d %q %q", code, stdout, stderr)
	reached(t, e.state() == before, "-plan writes nothing")

	code, _, stderr = e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("no terminal to ask at", "-yes"), assure.NoSideEffect(func() bool { return e.state() == before }))

	code, _, stderr = e.run(&scripted{isAttended: true, answers: []string{"n"}}, nil, "-out", e.archive, "-profile", e.profile)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("nothing was restored"), assure.NoSideEffect(func() bool { return e.state() == before }))

	code, _, stderr = e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-yes", "-not-encrypted-ok")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("-not-encrypted-ok", "not encrypted"), assure.NoSideEffect(func() bool { return e.state() == before }))

	rec := filepath.Join(t.TempDir(), "record.txt")
	a := &scripted{isAttended: true, answers: []string{"y"}}
	code, stdout, stderr = e.run(a, nil, "-out", e.archive, "-profile", e.profile, "-record-out", rec)
	reached(t, code == 0 && strings.Contains(stdout, "Restored 2 messages into Thunderbird") && e.restored(), "at a terminal, yes restores: %d %q %q", code, stdout, stderr)
	reached(t, prompted(a, "Restore? [y/N]"), "it asked Restore? [y/N]: %v", a.prompts)
	recText, _ := os.ReadFile(rec)
	reached(t, strings.Contains(string(recText), "Restored 2 messages") && strings.Contains(string(recText), "Restored — Kept mail"), "-record-out saves the plan and the result")

	// Partial: an original gone, -yes without a terminal.
	var eml string
	filepath.WalkDir(e.archive, func(p string, d os.DirEntry, err error) error {
		if err == nil && eml == "" && strings.HasSuffix(p, ".eml") {
			eml = p
		}
		return nil
	})
	os.Remove(eml)
	code, stdout, _ = e.run(&scripted{}, nil, "-out", e.archive, "-profile", e.profile, "-yes")
	reached(t, code == 3 && strings.Contains(stdout, "1 chosen message did not go back"), "partial exits 3: %d %q", code, stdout)

	// Ctrl-C (130) and terminate (143): the run is stopped before it writes; nothing added.
	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143} {
		e.makeProfile("")
		before := e.state()
		sigs := make(chan os.Signal, 2)
		was := restoreTBRun
		restoreTBRun = func(ctx context.Context, p *tbengine.Plan, s tbengine.Start) (*tbengine.Result, error) {
			sigs <- sig
			<-ctx.Done()
			return was(ctx, p, s)
		}
		code, _, stderr = e.run(&scripted{}, sigs, "-out", e.archive, "-profile", e.profile, "-yes")
		restoreTBRun = was
		reached(t, code == want && strings.Contains(stderr, "stopped") && !e.restored(), "a %v stops it: %d %q", sig, code, stderr)
		reached(t, e.state() == before, "a stop before writing adds nothing")
	}

	// -list-profiles: this computer's profiles (the throwaway home's).
	wasP := restoreTBProfiles
	restoreTBProfiles = func() (*tbrestore.ProfileList, error) {
		base := t.TempDir()
		writeTB(t, filepath.Join(base, "profiles.ini"), "[Profile0]\nName=Everyday\nIsRelative=0\nPath="+e.profile+"\nDefault=1\n")
		return tbrestore.ReadProfiles([]string{base})
	}
	code, stdout, _ = e.run(&scripted{}, nil, "-list-profiles")
	restoreTBProfiles = wasP
	reached(t, code == 0 && strings.Contains(stdout, "Everyday — "+e.profile), "-list-profiles: %q", stdout)
}

// covers: MA-1129, R40, R33, R37
// An encrypted archive at the command line: without a terminal -yes alone is refused
// before any password is asked, naming -not-encrypted-ok; -yes -not-encrypted-ok with a
// password file restores. At a terminal the question says the mail will not be encrypted.
// A Local Folders that may leave this computer is refused without a terminal whatever the
// flags; at a terminal its path must be typed back exactly, else nothing is restored.
func TestRestoreThunderbirdOfAnEncryptedArchive(t *testing.T) {
	e := newTBCLI(t, "")
	out := filepath.Join(t.TempDir(), "Locked mail")
	pc, err := keyring.Create(out, flowPassword, keyring.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	w, err := pc.Commit()
	if err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(t.TempDir(), "Mail")
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
	pw := filepath.Join(t.TempDir(), "pw.txt")
	writeTB(t, pw, flowPassword+"\n")

	before := e.state()
	code, _, stderr := e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-yes")
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("NOT be encrypted", "-not-encrypted-ok"), assure.NoSideEffect(func() bool { return e.state() == before }))

	code, stdout, stderr := e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-yes", "-not-encrypted-ok", "-password-file", pw)
	reached(t, code == 0 && strings.Contains(stdout, "Restored 2 messages"), "-yes -not-encrypted-ok restores: %d %q %q", code, stdout, stderr)
	reached(t, strings.Contains(stderr, "NOT encrypted") && !strings.Contains(stderr, "(Inbox)"), "the plan says NOT encrypted; progress names no folder: %q", stderr)

	// A synced Local Folders: refused without a terminal, whatever the flags.
	e.makeProfile(filepath.Join(t.TempDir(), "Dropbox"))
	before = e.state()
	code, _, stderr = e.run(&scripted{}, nil, "-out", out, "-profile", e.profile, "-yes", "-not-encrypted-ok", "-password-file", pw)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("may leave this computer", "Dropbox", "at a terminal"), assure.NoSideEffect(func() bool { return e.state() == before }))

	// At a terminal: the not-encrypted question, then the path typed back.
	a := &scripted{isAttended: true, answers: []string{flowPassword, "y", "/not/the/path"}}
	code, _, stderr = e.run(a, nil, "-out", out, "-profile", e.profile)
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("does not match", "nothing was restored"), assure.NoSideEffect(func() bool { return e.state() == before }))
	reached(t, prompted(a, "The restored mail will not be encrypted. Restore it?"), "the terminal's question: %v", a.prompts)
	a = &scripted{isAttended: true, answers: []string{flowPassword, "y", e.local}}
	code, stdout, stderr = e.run(a, nil, "-out", out, "-profile", e.profile)
	reached(t, code == 0 && strings.Contains(stdout, "Restored 2 messages"), "the path typed back exactly restores: %d %q %q", code, stdout, stderr)
}

// covers: MA-1129, R33, R12
// Through the real program: `restore-thunderbird -h` names its flags and its exit codes,
// the root usage names the verb, and without a terminal and -yes it is refused (exit 1).
func TestRestoreThunderbirdThroughTheProgram(t *testing.T) {
	_, stderr := runCLI("restore-thunderbird", "-h")
	for _, w := range []string{"-out", "-profile", "-profile-name", "-folder", "-store", "-name", "-plan", "-yes", "-record-out", "-not-encrypted-ok", "-list-profiles", "Exit:", "130 or 143"} {
		if !strings.Contains(stderr, w) {
			t.Errorf("restore-thunderbird -h does not name %q:\n%s", w, stderr)
		}
	}
	_, stderr = runCLI("-h")
	reached(t, strings.Contains(stderr, "restore-thunderbird"), "the root usage names the verb")
	code, stderr := runCLI("restore-thunderbird", "-out", t.TempDir())
	assure.Refused(t, code, stderr, assure.Code(1), assure.Names("no terminal to ask at", "-yes"))
}

func reached(t *testing.T, cond bool, format string, a ...any) {
	t.Helper()
	if !cond {
		t.Fatalf(format, a...)
	}
}
