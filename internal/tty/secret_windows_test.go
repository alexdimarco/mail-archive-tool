package tty

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
)

// covers: MA-853, R37
// Ctrl-C at a hidden prompt on Windows is an interrupt, as on Unix (the P3-8 Windows
// lab, B10): Windows ends the console read as if the input had run out and delivers
// the interrupt a moment later, so the prompt waits for it and ends with
// *ErrInterrupted (which the program tells as "interrupted; nothing was changed",
// exit 130), never "EOF". A typed line is returned at once, input that really ran out
// is still end-of-input, and an interrupt before the read ends is one too. The read is
// the real one over a pipe; only the interrupt is handed in, since nothing here can
// press Ctrl-C at a real console (that check is left for a person).
func TestCtrlCAtAHiddenPromptIsAnInterruptOnWindows(t *testing.T) {
	read := func(typed string, closeIt bool) (<-chan readResult, func()) {
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		if typed != "" {
			w.WriteString(typed)
		}
		if closeIt {
			w.Close()
		}
		lines := make(chan readResult, 1)
		go func() {
			b, err := readLine(r)
			lines <- readResult{b, err}
		}()
		return lines, func() { w.Close(); r.Close() }
	}

	// Positive twin: a typed line is the secret, returned without waiting.
	lines, done := read("hunter2 correct horse\r\n", false)
	start := time.Now()
	b, err := awaitLine(lines, make(chan os.Signal), interruptGrace)
	done()
	if err != nil || string(b) != "hunter2 correct horse" || time.Since(start) >= interruptGrace {
		t.Fatalf("a typed line: %q, %v, after %v", b, err, time.Since(start))
	}

	// Input that ran out with no Ctrl-C stays end-of-input.
	lines, done = read("", true)
	b, err = awaitLine(lines, make(chan os.Signal), interruptGrace)
	done()
	var ie *ErrInterrupted
	if !errors.Is(err, io.EOF) || errors.As(err, &ie) || b != nil {
		t.Fatalf("input that ran out: %q, %v", b, err)
	}

	// Ctrl-C: the read ends as if the input ran out, and the interrupt follows.
	lines, done = read("", true)
	sigs := make(chan os.Signal, 1)
	go func() {
		time.Sleep(50 * time.Millisecond)
		sigs <- os.Interrupt
	}()
	b, err = awaitLine(lines, sigs, interruptGrace)
	done()
	assure.Reached(t, errors.As(err, &ie) && ie.Signal == os.Interrupt && b == nil, "Ctrl-C that ended the console read is told as an interrupt, not EOF")

	// An interrupt while the read still waits.
	lines, done = read("half typ", false)
	sigs = make(chan os.Signal, 1)
	sigs <- os.Interrupt
	b, err = awaitLine(lines, sigs, interruptGrace)
	done()
	assure.Reached(t, errors.As(err, &ie) && ie.Signal == os.Interrupt && b == nil, "Ctrl-C while the read waits is an interrupt")
}
