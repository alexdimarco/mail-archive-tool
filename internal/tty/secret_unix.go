//go:build linux || darwin || freebsd || netbsd || openbsd || dragonfly

package tty

import (
	"fmt"
	"io"
	"os"
	"os/signal"

	"golang.org/x/sys/unix"
)

// awaitContinue waits, after a stop, for SIGCONT; an interrupt that comes first
// ends the prompt (ok false, naming it).
func awaitContinue(sigs <-chan os.Signal) (os.Signal, bool) {
	for s := range sigs {
		switch s {
		case unix.SIGCONT:
			return s, true
		case unix.SIGTSTP:
		default:
			return s, false
		}
	}
	return nil, false
}

// ReadSecret asks for a secret at the terminal in, printing prompt to out, with
// typing not shown (design-encrypted-archives-phase3 §3.5). The terminal is put
// back as it was on every way out: the line read, an error, and SIGINT, SIGTERM or
// SIGHUP (which end the prompt with *ErrInterrupted). Ctrl-Z (SIGTSTP) puts it back
// before the process stops, and hides typing again when it is continued.
func ReadSecret(in *os.File, out io.Writer, prompt string) ([]byte, error) {
	if !IsTerminal(in) || !Foreground(in) {
		return nil, ErrNotTerminal
	}
	fd := int(in.Fd())
	old, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return nil, err
	}
	hidden := *old
	hidden.Lflag &^= unix.ECHO
	hidden.Lflag |= unix.ICANON | unix.ISIG
	hidden.Iflag |= unix.ICRNL
	set := func(t *unix.Termios) { unix.IoctlSetTermios(fd, ioctlSetTermios, t) }

	sigs := make(chan os.Signal, 4)
	signal.Notify(sigs, unix.SIGINT, unix.SIGTERM, unix.SIGHUP, unix.SIGTSTP, unix.SIGCONT)
	defer signal.Stop(sigs)
	set(&hidden)
	defer set(old)
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
	for {
		select {
		case l := <-lines:
			fmt.Fprintln(out)
			return l.b, l.err
		case s := <-sigs:
			switch s {
			case unix.SIGTSTP:
				// Show typing again, then stop as Ctrl-Z would have. SIGSTOP, not a
				// SIGTSTP re-raised at ourselves: that would come back to this very
				// channel and loop, flipping the echo while the person types.
				set(old)
				fmt.Fprintln(out)
				unix.Kill(unix.Getpid(), unix.SIGSTOP)
				// The stop lands asynchronously (on Linux the process runs on for a
				// moment), so typing is hidden again only once it is continued (fg).
				if s, ok := awaitContinue(sigs); !ok {
					return nil, &ErrInterrupted{Signal: s}
				}
				set(&hidden)
				fmt.Fprint(out, prompt)
			case unix.SIGCONT:
				// Continued after a stop this prompt did not see (SIGSTOP), or after
				// its own: typing is hidden again either way.
				set(&hidden)
			default:
				fmt.Fprintln(out)
				return nil, &ErrInterrupted{Signal: s}
			}
		}
	}
}
