package desktop

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/procs"
)

// The `quit` verb (the S1 fix round 2, B1, B2, B5, B7): it asks this account's running
// MailArchive, over the signed hand-off, to quit the way the person's own Quit does when
// nothing runs, and never otherwise: a copy that runs work or has its wizard open says no
// and keeps running (R52). With -check it only asks whether the copy would quit. The
// upgrade script (Install-RMM.ps1) runs it as the copy's own user, from the installed
// program, instead of posting window messages from a PowerShell helper; its exit status
// tells the script what happened, each outcome its own code.
const (
	// QuitDone: the copy quit (with -check: it would quit now).
	QuitDone = 0
	// QuitBusy: the copy runs work or has its wizard open, and keeps running.
	QuitBusy = 3
	// QuitNotRunning: no MailArchive of this account runs (as that process).
	QuitNotRunning = 4
	// QuitCouldNotAsk: the copy could not be asked; the reason is printed.
	QuitCouldNotAsk = 5
	// QuitOlder: the copy is from before the quit request (its instance file lists no
	// hand-off actions at all: a version from before the menu-bar release), so it cannot
	// be asked; its own record says so, never a failure to reach it.
	QuitOlder = 6
	// QuitStillClosing: the copy agreed and is finishing its work, but had not ended
	// within -wait.
	QuitStillClosing = 7
	// quitUsageError: a flag the verb does not know (the flag package printed it).
	quitUsageError = 2
)

// The verb's words: one line for each outcome.
const (
	quitUsageText        = "Usage: mailarchive quit [-check] [-pid N] [-wait 30s]\n\nAsks your account's running MailArchive to quit, as Shut down does when nothing is running. It never stops work: while a backup or another task runs, or its wizard is open, it says so and keeps running.\nExit status: 0 it quit (with -check: it would quit now), 3 it is busy, 4 it is not running, 5 it could not be asked, 6 it is an older version that cannot be asked, 7 it was still finishing its work after the wait.\n\nFlags:"
	quitDoneText         = "MailArchive (process %d) has quit."
	quitWouldText        = "MailArchive (process %d) would quit now: nothing is running."
	quitAlreadyText      = "MailArchive (process %d) is already quitting."
	quitBusyText         = "MailArchive (process %d) is busy: it is running a backup or another task, or its wizard is open. It keeps running. To quit it anyway, use Shut down in MailArchive Desktop."
	quitNotRunningText   = "MailArchive is not running for your account."
	quitNotThisPIDText   = "Process %d is not your account's running MailArchive."
	quitCouldNotAskText  = "MailArchive could not be asked to quit: %s."
	quitOlderText        = "The running MailArchive (process %d) is an older version that cannot be asked to quit. Use Shut down in MailArchive Desktop, or sign out and back in."
	quitStillClosingText = "MailArchive (process %d) is finishing its work and quitting, but had not ended after %s."
)

// quitEnv is what the verb needs from its surroundings; a test injects each.
type quitEnv struct {
	Stdout, Stderr io.Writer
	StateDir       func() (string, error)
	// Session is this process's logon session; SessionOf a process's (instance.SessionOf).
	Session   string
	SessionOf func(pid int) (string, error)
	Alive     func(pid int) bool
	Version   string
	// Reach is how long a copy that does not answer yet is asked again; Poll how often
	// its end is looked for.
	Reach, Poll time.Duration
}

func osQuitEnv() *quitEnv {
	return &quitEnv{Stdout: os.Stdout, Stderr: os.Stderr, StateDir: instance.Dir, Session: instance.LogonSession(),
		SessionOf: instance.SessionOf, Alive: procs.Alive, Version: buildVersion(), Reach: 5 * time.Second, Poll: 200 * time.Millisecond}
}

// Quit is the `quit` verb; it returns the process's exit status.
func Quit(args []string) int { return quitVerb(context.Background(), args, osQuitEnv()) }

func quitVerb(ctx context.Context, args []string, env *quitEnv) int {
	fs := flag.NewFlagSet("mailarchive quit", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), quitUsageText)
		fs.PrintDefaults()
	}
	check := fs.Bool("check", false, "only ask whether it would quit now; stop nothing")
	pid := fs.Int("pid", 0, "the process to ask (default: this sign-in's MailArchive)")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for it to end once it agreed")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return QuitDone
		}
		return quitUsageError
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(env.Stderr, "quit takes no arguments, only flags (got %q); run mailarchive quit -h\n", fs.Arg(0))
		return quitUsageError
	}
	cannot := func(why string) int {
		fmt.Fprintf(env.Stderr, quitCouldNotAskText+"\n", why)
		return QuitCouldNotAsk
	}
	notRunning := func() int {
		if *pid != 0 {
			fmt.Fprintf(env.Stderr, quitNotThisPIDText+"\n", *pid)
		} else {
			fmt.Fprintln(env.Stderr, quitNotRunningText)
		}
		return QuitNotRunning
	}

	dir, err := env.StateDir()
	if err != nil {
		return cannot(fmt.Sprintf("it has no folder of your account to read (%v)", err))
	}
	session := env.Session
	if *pid != 0 {
		if !env.Alive(*pid) {
			return notRunning()
		}
		// The copy's own sign-in, which may not be this one's (B1: an account signed in
		// twice); its instance file is that session's.
		s, err := env.SessionOf(*pid)
		if err != nil {
			return cannot(fmt.Sprintf("cannot tell which sign-in process %d belongs to (%v)", *pid, err))
		}
		session = s
	}
	info, rerr := instance.Read(instance.FilePathIn(dir, session))
	switch {
	case errors.Is(rerr, os.ErrNotExist):
		if instance.LockIsHeld(dir, session) {
			return cannot("it is starting, or its record is missing; try again in a moment")
		}
		return notRunning()
	case rerr != nil:
		return cannot(fmt.Sprintf("its record cannot be read (%v)", rerr))
	case !env.Alive(info.PID):
		return notRunning()
	case *pid != 0 && info.PID != *pid:
		return notRunning() // that process is not the copy (a launch on its way, say)
	}
	if hp := instance.HolderPID(dir, session); hp != 0 && hp != info.PID {
		return cannot("its record does not name the copy that holds its lock")
	}
	if len(info.Acts) == 0 {
		// Its own record lists no hand-off actions: a version from before the menu-bar
		// release, which has no way to be asked (never decided by failing to reach it).
		fmt.Fprintf(env.Stderr, quitOlderText+"\n", info.PID)
		return QuitOlder
	}
	if !info.HasAct(instance.ActQuit) {
		return cannot("this copy does not know the quit request")
	}
	key, err := info.KeyBytes()
	if err != nil {
		return cannot(err.Error())
	}
	act := instance.ActQuit
	if *check {
		act = instance.ActCanQuit
	}
	rep, err := reachAct(ctx, info.Dashboard, key, instance.ActRequest{Act: act, Version: env.Version}, env.Reach,
		func() bool { return instance.LockIsHeld(dir, session) })
	if err != nil {
		return cannot(fmt.Sprintf("it did not answer (%v)", err))
	}
	switch {
	case rep.Shown && *check:
		fmt.Fprintf(env.Stdout, quitWouldText+"\n", info.PID)
		return QuitDone
	case rep.Reason == instance.ReasonClosing && *check:
		fmt.Fprintf(env.Stdout, quitAlreadyText+"\n", info.PID)
		return QuitDone
	case rep.Shown, rep.Reason == instance.ReasonClosing:
		// It agreed (or was already stopping): wait for it to end, its work drained.
	case rep.Reason == instance.ReasonBusy:
		fmt.Fprintf(env.Stderr, quitBusyText+"\n", info.PID)
		return QuitBusy
	default:
		return cannot(fmt.Sprintf("it answered %q", rep.Reason))
	}
	deadline := time.Now().Add(*wait)
	for env.Alive(info.PID) {
		if time.Now().After(deadline) {
			fmt.Fprintf(env.Stderr, quitStillClosingText+"\n", info.PID, *wait)
			return QuitStillClosing
		}
		select {
		case <-ctx.Done():
			return cannot("it was interrupted while waiting")
		case <-time.After(env.Poll):
		}
	}
	fmt.Fprintf(env.Stdout, quitDoneText+"\n", info.PID)
	return QuitDone
}
