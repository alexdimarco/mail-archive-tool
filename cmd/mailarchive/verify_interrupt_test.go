package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// covers: MA-394, R18, R20, S31
// Ctrl-C (or a terminate) during `verify` says what happened (g7 friction F10):
// it used to die silently by the signal while export prints "interrupted;
// progress saved". It now prints one line to stderr — the check did not finish,
// no new verdict was recorded, the last finished verdict still stands, run
// verify again — and exits with the shell's code for that signal (130, 143),
// leaving the running record exactly as a kill leaves it (status reports it as
// never finished, naming Ctrl-C). Positive twin: with no signal nothing is
// printed and nothing exits.
func TestVerifyInterruptSaysWhatHappened(t *testing.T) {
	var quiet bytes.Buffer
	exited := -1
	armed := make(chan os.Signal, 1)
	stop := watchVerifyInterrupt(armed, &quiet, "/arch", func(c int) { exited = c })
	stop()
	if quiet.Len() != 0 || exited != -1 {
		t.Fatalf("positive twin: no signal, yet printed %q / exited %d", quiet.String(), exited)
	}

	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143} {
		var buf bytes.Buffer
		exitCh := make(chan int, 1)
		ch := make(chan os.Signal, 1)
		done := watchVerifyInterrupt(ch, &buf, "/arch", func(c int) { exitCh <- c })
		ch <- sig
		code := <-exitCh
		done()
		assure.Refused(t, code, buf.String(), assure.Code(want),
			assure.Names("verify interrupted", "last finished verdict still stands", `mailarchive verify -out "/arch"`))
	}
}

// covers: MA-773, R37
// An encrypted archive's Ctrl-C stops the check instead of killing it (phase3 §8):
// the first signal keeps the shell's code for it (130, 143) and cancels the check,
// which then records itself as stopped and releases the lock; only a second signal
// exits at once. Positive twin: with no signal nothing is cancelled or exited.
func TestAnEncryptedChecksCtrlCStopsItCleanly(t *testing.T) {
	var code atomic.Int32
	cancelled, exited := false, -1
	stop := watchVerifyStop(make(chan os.Signal, 1), func() { cancelled = true }, &code, func(c int) { exited = c })
	stop()
	if cancelled || exited != -1 || code.Load() != 0 {
		t.Fatalf("positive twin: no signal, yet cancelled=%v exited=%d code=%d", cancelled, exited, code.Load())
	}
	for sig, want := range map[os.Signal]int32{os.Interrupt: 130, syscall.SIGTERM: 143} {
		var code atomic.Int32
		cancelCh, exitCh := make(chan struct{}), make(chan int, 1)
		ch := make(chan os.Signal, 2)
		done := watchVerifyStop(ch, func() { close(cancelCh) }, &code, func(c int) { exitCh <- c })
		ch <- sig
		<-cancelCh
		if code.Load() != want {
			t.Fatalf("%v: code %d, want %d", sig, code.Load(), want)
		}
		select {
		case c := <-exitCh:
			t.Fatalf("%v: the first signal exited (%d) instead of stopping the check", sig, c)
		default:
		}
		ch <- sig
		if c := <-exitCh; c != int(want) {
			t.Fatalf("%v: a second signal exited %d, want %d", sig, c, want)
		}
		done()
	}
}

// covers: MA-775, MA-773, R37
// The CLI wires that stop through (the P3-0 adversarial pass, FID-05): on an
// encrypted archive a signal cancels the check instead of exiting, and the stopped
// check exits with the signal's code (130, 143), says so, and is recorded as
// stopped. The positive twins: on a plaintext archive the same signal exits at once,
// the old way; and a check nothing stopped finishes intact.
func TestTheCLIWiresAnEncryptedChecksStop(t *testing.T) {
	plain := cliArchive(t, false)
	var plainCode atomic.Int32
	exitCh, ch := make(chan int, 1), make(chan os.Signal, 1)
	stop := verifyStopWatch(plain, ch, func() { t.Error("a plaintext check was cancelled, not exited") }, &plainCode, func(c int) { exitCh <- c })
	ch <- os.Interrupt
	if c := <-exitCh; c != 130 {
		t.Fatalf("a plaintext check exited %d on Ctrl-C, want 130", c)
	}
	stop()

	opts := func(out string) *verifyOpts {
		return &verifyOpts{out: &out, record: new(bool), asJSON: new(bool), log: new(string), unattended: new(bool)}
	}
	quiet := log.New(io.Discard, "", 0)
	for sig, want := range map[os.Signal]int{os.Interrupt: 130, syscall.SIGTERM: 143} {
		// A whole encrypted archive, written by a keyed run of the PST fixture.
		out := t.TempDir()
		p, err := vault.PrepareCreate(out, "cli test passphrase")
		if err != nil {
			t.Fatal(err)
		}
		key, err := p.Commit()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := app.Run(context.Background(), app.Options{Inputs: []string{"../../testdata/support.pst"}, Out: out, Key: key}, quiet, nil); err != nil {
			t.Fatal(err)
		}
		key.Destroy()
		var code atomic.Int32
		if err := checkArchive(context.Background(), opts(out), quiet, &code, io.Discard); err != nil {
			t.Fatalf("the positive twin: a check nothing stopped: %v", err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		cancelled, ch := make(chan struct{}), make(chan os.Signal, 1)
		stop := verifyStopWatch(out, ch, func() { cancel(); close(cancelled) }, &code, func(c int) { t.Errorf("%v: the first signal exited %d", sig, c) })
		ch <- sig
		select {
		case <-cancelled:
		case <-time.After(10 * time.Second):
			stop()
			t.Fatalf("%v: the signal did not cancel an encrypted archive's check", sig)
		}
		stop()
		var stderr bytes.Buffer
		err = checkArchive(ctx, opts(out), quiet, &code, &stderr)
		var ee *exitError
		if !errors.As(err, &ee) {
			t.Fatalf("%v: a stopped check returned %v, not an exit code", sig, err)
		}
		assure.Refused(t, ee.code, stderr.String(), assure.Code(want),
			assure.Names("verify stopped", "last finished verdict still stands", out))
		if lv, st, rerr := state.ReadLastVerify(out); st != state.LastVerifyPresent || rerr != nil || lv.Status != state.VerifyStopped {
			t.Fatalf("%v: the record after a stop: %+v %v %v", sig, lv, st, rerr)
		}
	}
}
