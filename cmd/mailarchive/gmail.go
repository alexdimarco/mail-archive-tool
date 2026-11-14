package main

// The Gmail question at a terminal (Q-B; docs/design-gmail-dedup.md §7.4 fold c; slice B3, and the
// B3 friction review's F4, F7, F13 and F14). A backup typed at a terminal asks it once, at the first
// Gmail account of an existing archive that is not switched on, before that account's mail is
// read; a scheduled run (-unattended), or one whose input or output is not a terminal, never asks.
// `-gmail-folders` answers yes for scripts. The question is asked on standard error, where the
// backup talks to the person (UX contract X8), and waits at most gmailAnswerWait: the run holds
// the archive's lock while it asks, so a person who walks away must not keep a scheduled backup out
// (F7). An answered Not now is remembered on this computer (gmailanswer), for the archive the
// folder holds now, so the next terminal backup asks nothing. Only a typed line is an answer: the
// end of the input, a read error, Ctrl-C or the time limit is no answer, which is Not now for this
// run only (the B3 adversarial pass's A4). Ctrl-C ends the question at once, and the run then
// stops as an interrupted run does.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/gmailanswer"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/tty"
	"mail-archive-tool/internal/util"
)

// The flag's help (F14): what it does first, that it cannot be undone, and what a backup does
// without it.
var gmailFlagHelp = "Gmail accounts in Thunderbird: Say yes to the Gmail question (\"" + state.GmailQuestionHeading + "\") for this archive without being asked, for scripts. This cannot be undone: " +
	"every computer that backs up into this archive then needs MailArchive 1.8 or newer. A backup typed at a terminal asks instead; a scheduled one never asks"

// The words around the question at a terminal (the question itself is the owner's approved words,
// in state; these were not in that sign-off).
const (
	gmailHowToAnswer = "Type y and press Enter for Yes. Press Enter for Not now."
	gmailSaidYes     = "Yes: from this backup on, each Gmail email is stored once and shown in all of its folders."
	gmailSaidNotNow  = "Not now: nothing changed, and MailArchive will not ask again on this computer. To say yes later, add -gmail-folders to a backup, or use Settings in MailArchive Desktop."
	// gmailSaidNotNowEncrypted is an encrypted archive's Not now: it is never kept beside the
	// archive (O1), so the next terminal backup asks again (the B3 UX walk's U5).
	gmailSaidNotNowEncrypted = "Not now: nothing changed. MailArchive asks again at the next backup you start at a terminal. To say yes without being asked, add -gmail-folders to a backup."
	gmailNoAnswer            = "No answer, so Not now for this backup: nothing changed. MailArchive asks again at the next backup you start at a terminal."
	// gmailStopped is said when Ctrl-C ends the question (A4): the run then stops as interrupted.
	gmailStopped = "Stopped before an answer: nothing changed, and nothing is remembered."
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

// setGmailQuestion is setGmailQuestionCtx with a question no interrupt ends; for tests.
func setGmailQuestion(opts *app.Options, unattended bool) {
	setGmailQuestionCtx(context.Background(), opts, unattended)
}

// setGmailQuestionCtx sets the run's Gmail answers from this computer (the Not now and the yes it
// keeps for the archive the folder holds now: the Desktop's, or an earlier terminal's) and, for a
// person at this terminal, the question, which ctx's end (the run's Ctrl-C) stops. A scheduled
// run is never asked.
func setGmailQuestionCtx(ctx context.Context, opts *app.Options, unattended bool) {
	kept := gmailanswer.Read(opts.Out)
	opts.GmailNotNow = kept.NotNow
	opts.GmailYesStores = append(opts.GmailYesStores, kept.Yes...)
	opts.GmailYesFor = kept.Archive
	if unattended || !gmailAtTerminal() {
		return
	}
	out, encrypted := opts.Out, opts.Key != nil
	opts.AskGmail = func(question string) bool {
		yes, answered := askGmailAnswerCtx(ctx, gmailInput(), os.Stderr, question, encrypted)
		// An answered Not now is remembered here, for the archive the run holds (its running
		// record's id); no answer is not an answer. Never for an encrypted archive: nothing
		// beside it may say it holds a Gmail account (O1).
		if !yes && answered && !encrypted {
			err := gmailanswer.KeepHere(out, gmailanswer.Answer{Archive: state.ArchiveID(out), NotNow: true})
			if err != nil {
				fmt.Fprintf(os.Stderr, "warning: MailArchive could not remember your Not now on this computer (%v); it asks again at the next backup you start at a terminal\n", err)
			}
		}
		return yes
	}
}

// gmailStoreShown is a store name as a terminal line may show it: control characters (C0, DEL,
// C1) and bidirectional overrides taken out, as every other untrusted name the command line
// prints (scrubTTY, util.StripBidi; the B3 adversarial pass's A5).
func gmailStoreShown(t string) string {
	return util.StripBidi(scrubTTY(t))
}

// askGmail asks question on w and reads one line from r: "y" or "yes" (any case) is Yes;
// anything else typed is Not now. It says what follows.
func askGmail(r io.Reader, w io.Writer, question string) bool {
	yes, _ := askGmailAnswer(r, w, question)
	return yes
}

// askGmailAnswer is askGmailAnswerCtx for a plaintext archive, with nothing to interrupt it.
func askGmailAnswer(r io.Reader, w io.Writer, question string) (yes, answered bool) {
	return askGmailAnswerCtx(context.Background(), r, w, question, false)
}

// askGmailAnswerCtx is askGmail, also saying whether the person answered: only a typed line (one
// ending in Enter) is an answer. The end of the input or a read error (Ctrl-D; on Windows a
// console read that Ctrl-C ends), ctx's end (Ctrl-C) and no answer within gmailAnswerWait are no
// answer: Not now for this run, never remembered.
func askGmailAnswerCtx(ctx context.Context, r io.Reader, w io.Writer, question string, encrypted bool) (yes, answered bool) {
	fmt.Fprintf(w, "\n%s\n%s\n%s ", state.GmailQuestionHeading, question, gmailHowToAnswer)
	type read struct {
		line string
		err  error
	}
	got := make(chan read, 1)
	go func() {
		line, err := bufio.NewReader(r).ReadString('\n')
		got <- read{line, err}
	}()
	timer := time.NewTimer(gmailAnswerWait)
	defer timer.Stop()
	select {
	case g := <-got:
		if g.err != nil {
			fmt.Fprintln(w, "\n"+gmailNoAnswer)
			return false, false
		}
		switch strings.ToLower(strings.TrimSpace(g.line)) {
		case "y", "yes":
			fmt.Fprintln(w, gmailSaidYes)
			return true, true
		}
		if encrypted {
			fmt.Fprintln(w, gmailSaidNotNowEncrypted)
		} else {
			fmt.Fprintln(w, gmailSaidNotNow)
		}
		return false, true
	case <-ctx.Done():
		fmt.Fprintln(w, "\n"+gmailStopped)
		return false, false
	case <-timer.C:
		fmt.Fprintln(w, "\n"+gmailNoAnswer)
		return false, false
	}
}
