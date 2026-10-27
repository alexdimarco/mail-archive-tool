//go:build windows

package tty

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"time"

	"golang.org/x/sys/windows"
)

// interruptGrace is how long a console read that ended with nothing typed waits for
// the Ctrl-C that may have ended it. Windows ends the read first, as if the input
// had run out, and delivers the interrupt a moment later on another thread (the
// P3-8 Windows lab, B10); without the wait, Ctrl-C read as "EOF" and exit 1.
const interruptGrace = time.Second

// readResult is what the console read gave: the line, or why there is none.
type readResult struct {
	b   []byte
	err error
}

// ReadSecret asks for a secret at the console in, printing prompt to out, with
// typing not shown (design-encrypted-archives-phase3 §3.5). The console is put back
// as it was on every way out, Ctrl-C included (*ErrInterrupted).
func ReadSecret(in *os.File, out io.Writer, prompt string) ([]byte, error) {
	if !IsTerminal(in) {
		return nil, ErrNotTerminal
	}
	h := windows.Handle(in.Fd())
	var old uint32
	if err := windows.GetConsoleMode(h, &old); err != nil {
		return nil, err
	}
	hidden := (old &^ windows.ENABLE_ECHO_INPUT) | windows.ENABLE_PROCESSED_INPUT | windows.ENABLE_LINE_INPUT
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt)
	defer signal.Stop(sigs)
	if err := windows.SetConsoleMode(h, hidden); err != nil {
		return nil, err
	}
	defer windows.SetConsoleMode(h, old)
	fmt.Fprint(out, prompt)

	lines := make(chan readResult, 1)
	go func() {
		b, err := readLine(in)
		lines <- readResult{b, err}
	}()
	b, err := awaitLine(lines, sigs, interruptGrace)
	windows.SetConsoleMode(h, old)
	fmt.Fprintln(out)
	return b, err
}

// awaitLine waits for the console read or an interrupt, whichever ends the prompt.
// A read that ended with nothing typed is held for up to grace in case Ctrl-C ended
// it, so the interrupt is told as one (*ErrInterrupted) rather than as input that
// ran out.
func awaitLine(lines <-chan readResult, sigs <-chan os.Signal, grace time.Duration) ([]byte, error) {
	select {
	case l := <-lines:
		if l.b == nil && errors.Is(l.err, io.EOF) {
			t := time.NewTimer(grace)
			defer t.Stop()
			select {
			case s := <-sigs:
				return nil, &ErrInterrupted{Signal: s}
			case <-t.C:
			}
		}
		return l.b, l.err
	case s := <-sigs:
		return nil, &ErrInterrupted{Signal: s}
	}
}
