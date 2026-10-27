package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/keyring"
	"mail-archive-tool/internal/thunderbird"
)

// runDecrypt makes an unencrypted copy of an encrypted archive (design-encrypted-
// archives-phase3 §10): the plan first, without the key; a destination that may
// leave this computer refused unless its path is typed back at a terminal (K5); then
// the key, and the copy. It is the escape hatch: no record of this computer is needed.
func runDecrypt(args []string) error {
	fs := flag.NewFlagSet("mailarchive decrypt", flag.ContinueOnError)
	fs.Usage = decryptUsage(fs)
	out := fs.String("out", "", "the encrypted archive folder to copy")
	dest := fs.String("dest", "", "an existing folder to make the unencrypted copy in (a new folder is made inside it)")
	logPath := fs.String("log", "", "also write the run's log to this file (counts only)")
	k := readKeyFlags(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the encrypted archive folder to copy)")
	}
	if *dest == "" {
		return errors.New("-dest is required (an existing folder to make the unencrypted copy in)")
	}
	return decryptWith(stdAsker(), abspath(*out), abspath(*dest), *logPath, k, time.Now(), os.Stdout, os.Stderr)
}

// decryptWith is runDecrypt after its flags, with what it asks and prints passed in.
func decryptWith(a asker, out, dest, logPath string, k *keyFlags, now time.Time, stdout, stderr io.Writer) error {
	env := app.NewCopyExportEnv("", "", "")
	plan, err := env.PlanDecryptCopy(out, dest, nil, nil, now)
	if err != nil {
		return err
	}
	if err := readKeyAvailable(a, out, "making an unencrypted copy of it", k); err != nil {
		return err
	}
	if err := app.RefuseLogInside(logPath); err != nil {
		return err
	}
	if plan.AskTwice() && !a.attended() {
		return fmt.Errorf("the unencrypted copy would be made in %s, which may leave this computer:\n  %s\nrun decrypt at a terminal to confirm it, or choose a folder on this computer's own drive", plan.Parent, strings.Join(plan.Leaves, "\n  "))
	}
	// The copy takes the archive's lock: a run holding it refuses before the plan's
	// question and the password (P3-8 friction review, T2-1, B15).
	if err := refuseBusy(out); err != nil {
		return err
	}
	// The plan and its notes are messages, on stderr; stdout carries only the
	// result (X8; P3-8 friction review, Tier C).
	fmt.Fprintln(stderr, decryptPlanLines(plan))
	fmt.Fprintln(stderr, "  Ctrl-C stops the copy and leaves its folder marked unfinished; delete that folder yourself — MailArchive does not keep track of it.")
	if a.attended() {
		ok, err := askYes(a, "Make this unencrypted copy?", false)
		if err != nil {
			return interrupted(err, stderr)
		}
		if !ok {
			return errors.New("nothing was copied")
		}
		if plan.AskTwice() {
			typed, err := a.line("The readable copy may leave this computer. To make it anyway, type the new folder's full path exactly as shown above: ")
			if err != nil {
				return interrupted(err, stderr)
			}
			if strings.TrimSpace(typed) != plan.Dest {
				return errors.New("the path typed does not match; nothing was copied")
			}
		}
	}
	u, err := unlockForRead(a, out, "making an unencrypted copy of it", k)
	if err != nil {
		return interrupted(err, stderr)
	}
	defer u.Destroy()
	logger, closeLog, err := newRunLogger(logPath)
	if err != nil {
		return err
	}
	defer closeLog()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var stopCode atomic.Int32
	stopWatch := watchVerifyStop(sigs, cancel, &stopCode, os.Exit)
	defer func() { signal.Stop(sigs); stopWatch() }()

	// The count is said once, by the summary below: without -log the copy's own
	// count line would repeat it on the terminal (X8).
	copyLog := logger
	if logPath == "" {
		copyLog = nil
	}
	rep, err := decryptCopy(ctx, u, env, plan, nil, nil, copyLog, nil)
	switch {
	case errors.Is(err, app.ErrCopyStopped):
		fmt.Fprintf(stderr, "mailarchive: decrypt stopped: %v\n", err)
		return &exitError{code: int(stopCode.Load())}
	case err != nil:
		return err
	}
	for _, line := range decryptSummary(rep) {
		fmt.Fprintln(stdout, line)
	}
	for _, p := range rep.Problems {
		fmt.Fprintln(stderr, "left out: "+p)
	}
	if code := rep.ExitCode(); code != 0 {
		return &exitError{code: code}
	}
	return nil
}

// decryptCopy makes the copy; a test injects a stop through it (fault injection).
var decryptCopy = keyring.DecryptCopy

// decryptPlanLines is the plan as the terminal shows it (X14).
func decryptPlanLines(p *app.DecryptPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "An UNENCRYPTED copy of %s will be made — readable by anyone who can open its folder:\n", p.Out)
	fmt.Fprintf(&b, "  new folder: %s\n", p.Dest)
	fmt.Fprintf(&b, "  size: up to %s", thunderbird.HumanBytes(p.NeedBytes))
	if p.FreeKnown {
		fmt.Fprintf(&b, " (%s free)", thunderbird.HumanBytes(int64(p.FreeBytes)))
	}
	b.WriteString("\n")
	for _, l := range p.Leaves {
		fmt.Fprintf(&b, "  ! %s\n", l)
	}
	for _, w := range p.Warnings {
		fmt.Fprintf(&b, "  warning: %s\n", w)
	}
	return strings.TrimRight(b.String(), "\n")
}

// decryptSummary is what the copy did, in counts.
func decryptSummary(r app.DecryptReport) []string {
	lines := []string{fmt.Sprintf("Unencrypted copy made at %s: %d of %d messages copied.", r.Dest, r.Copied, r.Messages)}
	if r.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("%d messages were left out — each is listed, with why, in %s inside the copy (and below). The encrypted archive was not changed.", r.Skipped, filepath.Join(r.Dest, app.CopyProblemsName)))
	}
	if r.HistoryLost > 0 || r.HistoryGap {
		lines = append(lines, "Part of the archive's go-back timeline could not be read, so the copy's go-back view is partial too.")
	}
	switch r.Index {
	case "rebuilt":
		lines = append(lines, "The copy's search index was rebuilt from the copied files: the archive's own could not be used as it was (unreadable, missing messages, or listing ones left out).")
	case "none":
		lines = append(lines, fmt.Sprintf("The copy has no search index: run `mailarchive reindex -rebuild -out %q` to make one.", r.Dest))
	}
	return lines
}

func decryptUsage(fs *flag.FlagSet) func() {
	return func() {
		fmt.Fprintf(os.Stderr, `mailarchive decrypt - make an unencrypted copy of an encrypted archive

Usage:
  mailarchive decrypt -out DIR -dest PARENT [-password-file FILE | -recovery-key-file FILE]

Makes a new folder in PARENT, named <archive>-unencrypted-<date>, holding a
plaintext archive of everything in the encrypted archive at DIR: every message,
the search index, the go-back history, the folder pages and the report. The
encrypted archive is not changed. The copy is READABLE by anyone who can open
its folder: keep it somewhere only you can reach, and delete it when done.

The plan is shown first. A folder that may leave this computer — synced to a
cloud service, on a removable or external drive, or on a network share — is
used only when you type the new folder's path back at a terminal.

Exit: 0 the whole archive copied; 3 partial (messages that could not be
copied are listed inside the copy, in _COPY-PROBLEMS.txt); 1 refusal/error;
130 or 143 stopped by Ctrl-C or a terminate signal (the folder is left marked
unfinished: delete it and make the copy again).

decrypt holds the archive's lock while it copies, so a scheduled backup that
fires meanwhile refuses. It cannot be scheduled.

Flags:
`)
		fs.PrintDefaults()
	}
}
