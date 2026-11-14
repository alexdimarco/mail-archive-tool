package main

// The Gmail question at a terminal (Q-B; docs/design-gmail-dedup.md §7.4 fold c; slice B3, and the
// B3 friction review's F4, F7, F13 and F14). A backup typed at a terminal asks it once, at the first
// Gmail account of an existing archive that is not switched on, before that account's mail is
// read; a scheduled run (-unattended), or one whose input or output is not a terminal, never asks.
// `-gmail-folders` answers yes for scripts. The question is asked on standard error, where the
// backup talks to the person (UX contract X8), and waits at most gmailAnswerWait: the run holds
// the archive's lock while it asks, so a person who walks away must not keep a scheduled backup out
// (F7). An answered Not now is remembered on this computer (gmailanswer), so the next terminal
// backup asks nothing.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tty"
)

// The flag's help (F14): what it does first, that it cannot be undone, and what a backup does
// without it.
var gmailFlagHelp = "Gmail accounts in Thunderbird: Say yes to the Gmail question (\"" + state.GmailQuestionHeading + "\") for this archive without being asked, for scripts. This cannot be undone: " +
	"every computer that backs up into this archive then needs MailArchive 1.8 or newer. A backup typed at a terminal asks instead; a scheduled one never asks"

// The words around the question at a terminal (for the owner's sign-off).
const (
	gmailHowToAnswer = "Type y and press Enter for Yes. Press Enter for Not now."
	gmailSaidYes     = "Yes: from this backup on, each Gmail email is stored once and shown in all of its folders."
	gmailSaidNotNow  = "Not now: nothing changed, and MailArchive will not ask again on this computer. To say yes later, add -gmail-folders to a backup, or use Settings in MailArchive Desktop."
	gmailNoAnswer    = "No answer, so Not now for this backup: nothing changed. MailArchive asks again at the next backup you start at a terminal."
)

// gmailAnswerWait is how long the terminal waits for an answer before it backs up as Not now
// (F7: the review's "about 2 minutes"). A var so a test can shorten it.
var gmailAnswerWait = 2 * time.Minute

// gmailAtTerminal says a person is there to answer: stdin, stdout and stderr are each a terminal
// and this process is in the foreground. A var so a test stands in for a terminal.
var gmailAtTerminal = func() bool {
	return tty.IsTerminal(os.Stdin) && tty.Foreground(os.Stdin) && tty.IsTerminal(os.Stdout) && tty.IsTerminal(os.Stderr)
}

// gmailInput is where the answer is read from. A var so a test stands in for the keyboard.
var gmailInput = func() io.Reader { return os.Stdin }

// setGmailQuestion sets the run's Gmail answers from this computer (the Not now and the yes it
// keeps for the archive: the Desktop's, or an earlier terminal's) and, for a person at this
// terminal, the question. A scheduled run is never asked.
func setGmailQuestion(opts *app.Options, unattended bool) {
	kept := gmailanswer.Read(opts.Out)
	opts.GmailNotNow = kept.NotNow
	opts.GmailYesStores = append(opts.GmailYesStores, kept.Yes...)
	if unattended || !gmailAtTerminal() {
		return
	}
	out, encrypted := opts.Out, opts.Key != nil
	opts.AskGmail = func(question string) bool {
		yes, answered := askGmailAnswer(gmailInput(), os.Stderr, question)
		// An answered Not now is remembered here; no answer in time is not an answer. Never for
		// an encrypted archive: nothing beside it may say it holds a Gmail account (O1).
		if !yes && answered && !encrypted {
			if err := gmailanswer.KeepHere(out, gmailanswer.Answer{NotNow: true}); err != nil {
				fmt.Fprintf(os.Stderr, "warning: MailArchive could not remember your Not now on this computer (%v); it asks again at the next backup you start at a terminal\n", err)
			}
		}
		return yes
	}
}

// gmailWaitingLines are status's lines for the Gmail stores the archive's last backup found off
// and waiting (F8): none after a Not now kept here, and none for a store a kept yes names. The
// record is plaintext only (an encrypted archive's has no such field), each store a safe name.
func gmailWaitingLines(out string, lr state.LastRun) []string {
	if len(lr.GmailOff) == 0 {
		return nil
	}
	kept := gmailanswer.Read(out)
	if kept.NotNow {
		return nil
	}
	var lines []string
	for _, t := range lr.GmailOff {
		if state.SafeToken(t) && !slices.Contains(kept.Yes, t) {
			lines = append(lines, state.GmailWaitingLine(t))
		}
	}
	return lines
}

// askGmail asks question on w and reads one line from r: "y" or "yes" (any case) is Yes;
// anything else, an empty line, the end of the input, or no answer within gmailAnswerWait, is
// Not now. It says what follows.
func askGmail(r io.Reader, w io.Writer, question string) bool {
	yes, _ := askGmailAnswer(r, w, question)
	return yes
}

// askGmailAnswer is askGmail, also saying whether the person answered (false: no answer in time).
func askGmailAnswer(r io.Reader, w io.Writer, question string) (yes, answered bool) {
	fmt.Fprintf(w, "\n%s\n%s\n%s ", state.GmailQuestionHeading, question, gmailHowToAnswer)
	got := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(r).ReadString('\n')
		got <- line
	}()
	timer := time.NewTimer(gmailAnswerWait)
	defer timer.Stop()
	select {
	case line := <-got:
		switch strings.ToLower(strings.TrimSpace(line)) {
		case "y", "yes":
			fmt.Fprintln(w, gmailSaidYes)
			return true, true
		}
		fmt.Fprintln(w, gmailSaidNotNow)
		return false, true
	case <-timer.C:
		fmt.Fprintln(w, "\n"+gmailNoAnswer)
		return false, false
	}
}
