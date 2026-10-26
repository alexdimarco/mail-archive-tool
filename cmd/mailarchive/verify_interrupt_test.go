package main

import (
	"bytes"
	"os"
	"sync/atomic"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
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
