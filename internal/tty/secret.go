package tty

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
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
		return string(l.b), l.err
	case s := <-sigs:
		fmt.Fprintln(out)
		return "", &ErrInterrupted{Signal: s}
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
