package main

// The Gmail question at a terminal (Q-B; docs/design-gmail-dedup.md §7.4 fold c; slice B3). A
// backup typed at a terminal asks it once, at the first Gmail account of an existing archive
// that is not switched on, before that account's mail is read; a scheduled run (-unattended),
// or one whose input or output is not a terminal, never asks. `-gmail-folders` answers yes for
// scripts. The words are B2's (state.GmailSwitchQuestion), for the friction review.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"mail-archive-tool/internal/app"
	jobfile "mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/tty"
)

// The flag's help, in the question's own words.
const (
	gmailQuestionShort = "Show Sent Mail and your other Gmail folders in full, and store each email once?"
	gmailFlagAfter     = "Every computer that backs up into this archive then needs MailArchive 1.8 or later. A backup typed at a terminal asks instead; a scheduled one never asks"
)

// The words around the question at a terminal (for the friction review).
const (
	gmailHowToAnswer = "Type y and press Enter for Yes. Press Enter for Not now."
	gmailSaidYes     = "Yes: from this backup on, each Gmail email is stored once and shown in all of its folders."
	gmailSaidNotNow  = "Not now: nothing changed. MailArchive asks again at the next backup you start at a terminal. To say yes without being asked, add -gmail-folders."
)

// gmailAtTerminal says a person is there to answer: stdin and stdout are both a terminal and
// this process is in the foreground. A var so a test stands in for a terminal.
var gmailAtTerminal = func() bool {
	return tty.IsTerminal(os.Stdin) && tty.Foreground(os.Stdin) && tty.IsTerminal(os.Stdout)
}

// setGmailQuestion sets the run's question for a person at this terminal: none for a scheduled
// run, and none while this computer keeps a Not now for the archive (said in MailArchive
// Desktop, kept in the archive's job file).
func setGmailQuestion(opts *app.Options, unattended bool) {
	opts.GmailNotNow = gmailNotNowKept(opts.Out)
	if unattended || !gmailAtTerminal() {
		return
	}
	opts.AskGmail = func(question string) bool { return askGmail(os.Stdin, os.Stdout, question) }
}

// gmailNotNowKept reports whether this computer keeps a Not now for the archive at out: the
// Desktop's answer, in the archive's job file. A job file that cannot be read keeps none.
func gmailNotNowKept(out string) bool {
	p, err := jobfile.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return false
	}
	if _, err := os.Lstat(p); err != nil {
		return false
	}
	j, err := jobfile.Read(p)
	return err == nil && j.GmailNotNow
}

// askGmail asks question on w and reads one line from r: "y" or "yes" (any case) is Yes;
// anything else, an empty line or the end of the input, is Not now. It says what follows.
func askGmail(r io.Reader, w io.Writer, question string) bool {
	fmt.Fprintf(w, "\n%s\n%s ", question, gmailHowToAnswer)
	line, _ := bufio.NewReader(r).ReadString('\n')
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes":
		fmt.Fprintln(w, gmailSaidYes)
		return true
	}
	fmt.Fprintln(w, gmailSaidNotNow)
	return false
}
