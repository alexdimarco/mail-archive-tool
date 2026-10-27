//go:build windows

package tty

import (
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
// P3-8 Windows lab, B10); without the wait, Ctrl-C read as "EOF" and exit 1. The
// hidden prompt and the shown one (ReadLine) both wait.
const interruptGrace = time.Second

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
