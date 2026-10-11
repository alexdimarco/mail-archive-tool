package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"mail-archive-tool/internal/browser"
	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/localweb"
	"mail-archive-tool/internal/tty"
)

// linkEnv is what `serve` and `setup` need from their surroundings to deliver a
// launch link (design-local-confidentiality §4.C3): the streams, whether they are
// terminals, the browser opener, the per-account state directory and the clock.
// The binary uses osLinkEnv; a test injects each, so no browser is ever started
// and no terminal is needed to run a surface in-process.
type linkEnv struct {
	Stdin       *os.File  // identifies the terminal checks apply to; nil when there is none
	StdinReader io.Reader // what the Enter loop reads (Stdin when nil)
	Stdout      io.Writer
	StdoutFile  *os.File // the file behind Stdout, for the terminal check
	Stderr      io.Writer
	Getenv      func(string) string
	GOOS        string
	Open        func(url string) error
	StateDir    func() (string, error)
	IsTerminal  func(*os.File) bool
	Foreground  func(*os.File) bool
	Now         func() time.Time
	// Ready, when set, is told the origin (http://host:port) the surface serves on,
	// once it is serving: how a test finds the port.
	Ready func(origin string)
}

func osLinkEnv() *linkEnv {
	return &linkEnv{
		Stdin: os.Stdin, Stdout: os.Stdout, StdoutFile: os.Stdout, Stderr: os.Stderr,
		Getenv: os.Getenv, GOOS: runtime.GOOS, Open: browser.Open, StateDir: instance.Dir,
		IsTerminal: tty.IsTerminal, Foreground: tty.Foreground, Now: time.Now,
	}
}

// surfaceLinks delivers the one-time links of one surface of this process: the
// opener's own, the printed one, and a fresh one on Enter [CC5, CC18, CC19].
type surfaceLinks struct {
	env    *linkEnv
	sess   *localweb.Session
	origin string // http://127.0.0.1:8099 — where /_open?t=… is served
	// noBrowser is -no-browser.
	noBrowser bool
	// hint is printed in place of a link when stdout is not a terminal (a journal,
	// nohup.out, a CI log), where a token must not be written [CC5].
	hint string

	mu sync.Mutex // one print at a time
}

// canOpen: -no-browser not given and a display to open a browser on [CC18].
func (l *surfaceLinks) canOpen() bool {
	return !l.noBrowser && !browser.NoDisplay(l.env.GOOS, l.env.Getenv)
}

func (l *surfaceLinks) stdoutIsTerminal() bool {
	return l.env.StdoutFile != nil && l.env.IsTerminal(l.env.StdoutFile)
}

func (l *surfaceLinks) stdinIsTerminal() bool {
	return l.env.Stdin != nil && l.env.IsTerminal(l.env.Stdin)
}

// mint makes a one-time link of the given lifetime and returns it with its token.
func (l *surfaceLinks) mint(ttl time.Duration) (link, token string) {
	token = l.sess.Mint(ttl)
	return l.origin + localweb.OpenPath(token), token
}

// handoffOpen is the opener a handoff uses: the same one, or none when this run
// opens no browser.
func (l *surfaceLinks) handoffOpen() func(string) error {
	if !l.canOpen() {
		return nil
	}
	return l.env.Open
}

// start opens the browser at its own 60-second link unless it may not, and says
// where the way in is: on a terminal, a separate 10-minute link (worded as a
// fallback when the browser was opened); otherwise no token at all, only how to
// get one [CC5].
func (l *surfaceLinks) start() {
	l.mu.Lock()
	defer l.mu.Unlock()
	opened := false
	if l.canOpen() {
		link, _ := l.mint(localweb.OpenerTTL)
		opened = l.env.Open(link) == nil
	}
	switch {
	case !l.stdoutIsTerminal():
		fmt.Fprintln(l.env.Stdout, l.hint)
	case opened:
		link, _ := l.mint(localweb.LinkTTL)
		fmt.Fprintf(l.env.Stdout, "If your browser did not open, use: %s\n", link)
	default:
		link, _ := l.mint(localweb.LinkTTL)
		fmt.Fprintf(l.env.Stdout, "Open this link in your browser: %s\n", link)
	}
}

// printFresh prints a new 10-minute link for a second browser or device. The
// token goes to a terminal only: stdout when it is one, else stderr when that is
// (stdin is a terminal here), else nowhere — never into a log.
func (l *surfaceLinks) printFresh() {
	l.mu.Lock()
	defer l.mu.Unlock()
	link, _ := l.mint(localweb.LinkTTL)
	switch {
	case l.stdoutIsTerminal():
		fmt.Fprintf(l.env.Stdout, "Open this link in your browser: %s\n", link)
	case l.env.Stdin != nil && l.env.IsTerminal(l.env.Stdin):
		fmt.Fprintf(l.env.Stderr, "Open this link in your browser: %s\n", link)
	}
}

// pollForeground is how often a backgrounded process looks for the terminal back.
const pollForeground = 250 * time.Millisecond

// enter prints a fresh link each time Enter is pressed, while stdin is a terminal
// and this process is in the terminal's foreground process group — checked
// before each read, so a backgrounded `serve &` never reads (SIGTTIN would stop
// it) and picks the line up once `fg` brings it back [CC18]. It returns at once,
// reading nothing, when stdin is not a terminal. It reports whether it started.
func (l *surfaceLinks) enter(ctx context.Context) bool {
	if !l.stdinIsTerminal() {
		return false
	}
	var rd io.Reader = l.env.StdinReader
	if rd == nil {
		rd = l.env.Stdin
	}
	go func() {
		br := bufio.NewReader(rd)
		for {
			for !l.env.Foreground(l.env.Stdin) {
				select {
				case <-ctx.Done():
					return
				case <-time.After(pollForeground):
				}
			}
			if _, err := br.ReadString('\n'); err != nil {
				return
			}
			l.printFresh()
		}
	}()
	return true
}

// stopHint is the last line of a surface's start-up text.
func (l *surfaceLinks) stopHint(entering bool) string {
	if entering {
		return "Press Enter for a new link, Ctrl-C to stop."
	}
	return "Press Ctrl-C to stop."
}

// quoteForHint shows a path in a hint the way a shell would need it.
func quoteForHint(p string) string {
	if strings.ContainsAny(p, " \t'\"$`\\") {
		return "'" + strings.ReplaceAll(p, "'", `'\''`) + "'"
	}
	return p
}
