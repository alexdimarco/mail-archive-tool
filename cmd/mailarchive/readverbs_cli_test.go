package main

import (
	"context"
	"errors"
	"flag"
	"io"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/encrecord"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/vault"
)

// The read verbs on an encrypted archive at the command line
// (design-encrypted-archives-phase3 §3.4, §7.5; P3-2a-3, MA-797).

const readMbox = "From - Mon Jan 01 00:00:00 2024\r\n" +
	"From: Bob <bob@example.com>\r\nSubject: Hello HTML\r\nDate: Mon, 01 Jan 2024 10:00:00 +0000\r\n" +
	"Message-ID: <abc@example.com>\r\nMIME-Version: 1.0\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
	"<p>Hello <b>world</b> invoice</p>\r\n"

// backedUpArchive is an encrypted archive with one message, its index and its
// original, and a password file for it.
func backedUpArchive(t *testing.T) (out, pwFile string) {
	t.Helper()
	flowRecord(t)
	out = makeArchive(t)
	u, err := keyring.Open(out, keyring.Password(flowPassword), keyring.OpenOptions{Write: true})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	box := filepath.Join(t.TempDir(), "Inbox")
	os.WriteFile(box, []byte(readMbox), 0o600)
	opts := app.Options{Inputs: []string{box}, Out: out, Index: true, KeepRaw: true}
	done, err := keyring.Apply(&opts, u)
	if err != nil {
		t.Fatal(err)
	}
	defer done()
	if _, err := app.Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	pwFile = filepath.Join(t.TempDir(), "pw")
	if err := os.WriteFile(pwFile, []byte(flowPassword+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return out, pwFile
}

// stdout runs f with os.Stdout captured.
func stdout(t *testing.T, f func() error) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stdout
	os.Stdout = w
	got := make(chan string)
	go func() { b, _ := io.ReadAll(r); got <- string(b) }()
	ferr := f()
	w.Close()
	os.Stdout = orig
	return <-got, ferr
}

// covers: MA-797, MA-733, R37
// search and extract unlock an encrypted archive by -password-file with no
// terminal and read it: search finds its message; extract writes its original out.
// With no file, no remembered key and no terminal they are refused naming the files
// that would do; -paths is refused before anything is read; a non-loopback serve is
// refused before anything is asked; and a plaintext archive refuses the key flags.
func TestTheReadVerbsUnlockAnEncryptedArchive(t *testing.T) {
	out, pwFile := backedUpArchive(t)
	// A read records nothing and heals nothing: an unrecorded folder stays
	// unrecorded, a missing copy of the key file stays missing (§3.3, §3.6).
	id := recordedAt(t, out)
	if _, err := encrecord.Update("test", func(r *encrecord.Record) error { delete(r.Archives, id); return nil }); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(out, vault.BackupName))

	got, err := stdout(t, func() error { return runSearch([]string{"-out", out, "-password-file", pwFile, "-json", "hello"}) })
	if err != nil || !strings.Contains(got, "Hello HTML") {
		t.Fatalf("search: %v\n%s", err, got)
	}
	if recordedAt(t, out) != "" || exists(filepath.Join(out, vault.BackupName)) {
		t.Fatal("a read recorded the folder or healed its key file")
	}
	dest := filepath.Join(t.TempDir(), "x")
	if _, err := stdout(t, func() error {
		return runExtract([]string{"-out", out, "-password-file", pwFile, "-format", "eml", "-dest", dest})
	}); err != nil {
		t.Fatalf("extract: %v", err)
	}
	var emls int
	filepath.WalkDir(dest, func(p string, d fs.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".eml") {
			emls++
		}
		return nil
	})
	assure.Reached(t, emls == 1, "the original extracted")

	for _, verb := range []func() error{
		func() error { return runSearch([]string{"-out", out, "x"}) },
		func() error {
			return runExtract([]string{"-out", out, "-format", "eml", "-dest", filepath.Join(t.TempDir(), "y")})
		},
	} {
		code, msg := refused(verb())
		assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "encrypted archive", "password or recovery key", "-password-file"))
	}

	// -paths and a non-loopback serve are refused first, before even the want of a
	// key is judged — with no secret at all — writing nothing (the P3-2a pass, FID-08).
	tree := filesUnder(t, out)
	unchanged := func() bool { return len(filesUnder(t, out)) == len(tree) }
	err = runSearch([]string{"-out", out, "-paths", "x"})
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-paths", "-json"), assure.Forbid("password-file"), assure.NoSideEffect(unchanged))

	err = serveMain(context.Background(), []string{"-out", out, "-addr", "0.0.0.0:0"}, osLinkEnv())
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("loopback", out), assure.Forbid("password-file"), assure.NoSideEffect(unchanged))

	err = serveMain(context.Background(), []string{"-out", out, "-addr", "127.0.0.1:0"}, osLinkEnv())
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "password or recovery key", "-password-file", "-recovery-key-file"), assure.NoSideEffect(unchanged))

	plain := t.TempDir()
	err = runSearch([]string{"-out", plain, "-password-file", pwFile, "x"})
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-password-file", "not encrypted"))
}

// covers: MA-799, R37
// verify -full unlocks an encrypted archive by -password-file with no terminal and
// checks it with its key: a whole archive exits 0; a message's file gone is named
// and exits 2. -full on an archive that is not encrypted, -full with -unattended, a
// key flag without -full, and a scheduled verify -full are each refused by name,
// writing nothing.
func TestVerifyFullAtTheCommandLine(t *testing.T) {
	out, pwFile := backedUpArchive(t)
	got, err := stdout(t, func() error { return runVerify([]string{"-out", out, "-full", "-password-file", pwFile}) })
	if err != nil || !strings.Contains(got, "with the key") {
		t.Fatalf("positive twin: %v\n%s", err, got)
	}

	var unit string
	filepath.WalkDir(filepath.Join(out, "units"), func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && unit == "" {
			unit = p
		}
		return nil
	})
	os.Remove(unit)
	rel, _ := filepath.Rel(out, unit)
	got, err = stdout(t, func() error { return runVerify([]string{"-out", out, "-full", "-password-file", pwFile}) })
	var ee *exitError
	if !errors.As(err, &ee) || ee.code != 2 || !strings.Contains(got, filepath.ToSlash(rel)) || !strings.Contains(got, "missing") {
		t.Fatalf("a message gone: %v\n%s", err, got)
	}

	tree := filesUnder(t, out)
	unchanged := func() bool { return len(filesUnder(t, out)) == len(tree) }
	code, msg := refused(runVerify([]string{"-out", out, "-full", "-unattended", "-password-file", pwFile}))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "-full"), assure.NoSideEffect(unchanged))
	code, msg = refused(runVerify([]string{"-out", out, "-password-file", pwFile}))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-password-file", "add -full"), assure.NoSideEffect(unchanged))
	code, msg = parseJobRefusal([]string{"verify", "-out", out, "-full"})
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-full", "schedule plain verify"))

	plain := t.TempDir()
	code, msg = refused(runVerify([]string{"-out", plain, "-full", "-password-file", pwFile}))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-full", "not one this computer can check", plain),
		assure.NoSideEffect(func() bool { return len(filesUnder(t, plain)) == 0 }))
}

// covers: MA-803, R37
// verify -full at the command line, after the P3-2b pass: a busy archive is refused
// before any key is asked or derived — a wrong password file is not what is told
// (FID-07); key flags on an archive that is not encrypted say so, never "add -full"
// (FID-07); a stopped check with the key names -full in how to finish it (KEY-03).
func TestVerifyFullAfterThePass(t *testing.T) {
	out, pwFile := backedUpArchive(t)
	wrong := filepath.Join(t.TempDir(), "wrong")
	os.WriteFile(wrong, []byte("not the password at all\n"), 0o600)
	holder, err := lockfile.AcquireAs(filepath.Join(out, lockfile.Name), "")
	if err != nil {
		t.Fatal(err)
	}
	code, msg := refused(runVerify([]string{"-out", out, "-full", "-password-file", wrong}))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "being written"), assure.Forbid("password"))
	holder.Release()

	plain := t.TempDir()
	code, msg = refused(runVerify([]string{"-out", plain, "-password-file", pwFile}))
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-password-file", "not encrypted"), assure.Forbid("add -full"))

	u, err := keyring.Open(out, keyring.Password(flowPassword), keyring.OpenOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer u.Destroy()
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	o := verifyFlags(fs)
	fs.Parse([]string{"-out", out})
	o.keyed = u
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stopCode atomic.Int32
	stopCode.Store(130)
	var stderr strings.Builder
	err = checkArchive(ctx, o, log.New(io.Discard, "", 0), &stopCode, &stderr)
	var ee *exitError
	assure.Reached(t, errors.As(err, &ee) && ee.code == 130 && strings.Contains(stderr.String(), "mailarchive verify -full -out"), "the stop names -full")
}

// covers: MA-797, R37
// reindex on an encrypted archive only rebuilds: without -rebuild it is refused
// naming it; with -rebuild but no terminal it is refused (it is a write, §3.4),
// writing nothing.
func TestReindexOnAnEncryptedArchiveOnlyRebuilds(t *testing.T) {
	out, pwFile := backedUpArchive(t)
	before := filesUnder(t, out)
	err := runReindex([]string{"-out", out, "-password-file", pwFile})
	code, msg := refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("-rebuild", "never removes a message"))
	err = runReindex([]string{"-out", out, "-rebuild", "-password-file", pwFile})
	code, msg = refused(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names("terminal", "password or recovery key", "-rebuild"),
		assure.Forbid("Desktop", "backups"), // no remedy that does not exist (the P3-2a pass, REG-02)
		assure.NoSideEffect(func() bool { b := filesUnder(t, out); return len(b) == len(before) }))
}
