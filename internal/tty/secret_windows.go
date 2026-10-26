//go:build windows

package tty

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	"golang.org/x/sys/windows"
)

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

	type line struct {
		b   []byte
		err error
	}
	lines := make(chan line, 1)
	go func() {
		b, err := readLine(in)
		lines <- line{b, err}
	}()
	select {
	case l := <-lines:
		fmt.Fprintln(out)
		return l.b, l.err
	case s := <-sigs:
		windows.SetConsoleMode(h, old)
		fmt.Fprintln(out)
		return nil, &ErrInterrupted{Signal: s}
	}
}
