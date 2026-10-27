package tty

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// MaxSecret bounds a typed secret, as it bounds a secret file
// (design-encrypted-archives-phase3 §3.4).
const MaxSecret = 4 << 10

// ErrInterrupted is a hidden prompt ended by a signal; Signal says which, so the
// caller exits as that signal would (130 for SIGINT, 143 for SIGTERM).
type ErrInterrupted struct{ Signal os.Signal }

func (e *ErrInterrupted) Error() string {
	return fmt.Sprintf("interrupted (%v) while asking for a secret", e.Signal)
}

// ErrNotTerminal refuses a hidden prompt on something that is not a terminal in the
// foreground.
var ErrNotTerminal = errors.New("a password can only be typed at a terminal in the foreground")

// ErrTooLong refuses a typed line over MaxSecret.
var ErrTooLong = errors.New("what was typed is longer than a secret can be")

// ReadLine asks a question at the terminal in, printing prompt to out, and reads
// the answer as typed (shown). Ctrl-C or SIGTERM ends it with *ErrInterrupted.
func ReadLine(in *os.File, out io.Writer, prompt string) (string, error) {
	if !IsTerminal(in) || !Foreground(in) {
		return "", ErrNotTerminal
	}
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigs)
	return askLine(in, out, prompt, sigs)
}

// askLine prints prompt to out and reads the answer from in, unless a signal on sigs
// ends the prompt first (*ErrInterrupted). On Windows Ctrl-C ends the console read
// as if the input had run out, so a read that ended with nothing typed waits
// interruptGrace for it, as the hidden prompt does (the P3-8 Windows lab, B10).
func askLine(in io.Reader, out io.Writer, prompt string, sigs <-chan os.Signal) (string, error) {
	fmt.Fprint(out, prompt)
	lines := make(chan readResult, 1)
	go func() {
		b, err := readLine(in)
		lines <- readResult{b, err}
	}()
	b, err := awaitLine(lines, sigs, interruptGrace)
	var ie *ErrInterrupted
	if errors.As(err, &ie) {
		fmt.Fprintln(out)
	}
	return string(b), err
}

// readResult is what a read of the terminal gave: the line, or why there is none.
type readResult struct {
	b   []byte
	err error
}

// awaitLine waits for the read or an interrupt, whichever ends the prompt. Where
// grace is more than zero (Windows), a read that ended with nothing typed is held for
// up to grace in case Ctrl-C ended it, so the interrupt is told as one
// (*ErrInterrupted) rather than as input that ran out.
func awaitLine(lines <-chan readResult, sigs <-chan os.Signal, grace time.Duration) ([]byte, error) {
	select {
	case l := <-lines:
		if grace > 0 && l.b == nil && errors.Is(l.err, io.EOF) {
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

// readLine reads one line from r a byte at a time — never past the newline, so
// what follows stays for the next reader — without its line ending.
func readLine(r io.Reader) ([]byte, error) {
	var b bytes.Buffer
	one := make([]byte, 1)
	for {
		n, err := r.Read(one)
		if n == 1 {
			if one[0] == '\n' {
				break
			}
			if b.Len() >= MaxSecret {
				clear(b.Bytes())
				return nil, ErrTooLong
			}
			b.WriteByte(one[0])
		}
		if err != nil {
			if errors.Is(err, io.EOF) && b.Len() > 0 {
				break
			}
			clear(b.Bytes())
			return nil, err
		}
	}
	return bytes.TrimSuffix(b.Bytes(), []byte("\r")), nil
}
