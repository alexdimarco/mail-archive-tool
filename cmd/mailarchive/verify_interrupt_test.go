package main

import (
	"bytes"
	"os"
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
