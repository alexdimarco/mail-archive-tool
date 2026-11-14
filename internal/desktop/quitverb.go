package desktop

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"mail-archive-tool/internal/instance"
	"mail-archive-tool/internal/procs"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/util"
)

// The `quit` verb (the S1 fix round 2, B1, B2, B5, B7): it asks this account's running
// MailArchive, over the signed hand-off, to quit the way the person's own Quit does when
// nothing runs, and never otherwise: a copy that runs work or has its wizard open says no
// and keeps running (R52). With -check it only asks whether the copy would quit. The
// upgrade script (Install-RMM.ps1) runs it as the copy's own user, from the installed
// program, instead of posting window messages from a PowerShell helper; its exit status
// tells the script what happened. The codes are inside X1's declared set (docs/ux-
// contract.md X1, MA-613: 0, 1, 2, 3), each outcome its own.
const (
	// QuitDone: the copy quit (with -check: it would quit now), or, with -end-older, an
	// older copy was ended (with -check: would be).
	QuitDone = 0
	// QuitCouldNotAsk: the copy could not be asked (the reason is printed), an older copy
	// without -end-older, or a usage error.
	QuitCouldNotAsk = 1
	// QuitNotRunning: no MailArchive of this account runs (as that process).
	QuitNotRunning = 2
	// QuitBusy: the copy runs work or has its wizard open and keeps running, or it agreed
	// but was still finishing its work after -wait.
	QuitBusy = 3
)

// The verb's words: one line for each outcome.
const (
	quitUsageText        = "Usage: mailarchive quit [-check] [-pid N] [-wait 30s] [-end-older]\n\nAsks your account's running MailArchive to quit, as Shut down does when nothing is running. It never stops work: while a backup or another task runs, or its wizard or its choice window is open, it says so and keeps running.\nExit status: 0 it quit (with -check: it would quit now), 1 it could not be asked, 2 it is not running, 3 it is busy or still finishing its work.\n\nFlags:"
	quitDoneText         = "MailArchive (process %d) has quit."
	quitWouldText        = "MailArchive (process %d) would quit now: nothing is running."
	quitAlreadyText      = "MailArchive (process %d) is already quitting."
	quitBusyText         = "MailArchive (process %d) is busy: it is running a backup or another task, or its wizard or its choice window is open. It keeps running. To quit it anyway, use Shut down in MailArchive Desktop."
	quitNotRunningText   = "MailArchive is not running for your account."
	quitNotThisPIDText   = "Process %d is not your account's running MailArchive."
	quitCouldNotAskText  = "MailArchive could not be asked to quit: %s."
	quitOlderText        = "The running MailArchive (process %d) is an older version that cannot be asked to quit. Use Shut down in MailArchive Desktop, or sign out and back in."
	quitOlderWouldText   = "The running MailArchive (process %d) is an older version that cannot be asked to quit; it would be ended."
	quitOlderEndedText   = "The older MailArchive (process %d), which cannot be asked to quit, was ended."
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
	// Program is the full path of the program a process runs (procs.Program): an older
	// copy is ended only when it is proven to be MailArchive (the S1 fix round 3, C3).
	Program func(pid int) (string, error)
	// End ends the process pid at once (an older copy, -end-older only).
	End     func(pid int) error
	Version string
	// Reach is how long a copy that does not answer yet is asked again; Poll how often
	// its end is looked for.
	Reach, Poll time.Duration
}

func osQuitEnv() *quitEnv {
	return &quitEnv{Stdout: os.Stdout, Stderr: os.Stderr, StateDir: instance.Dir, Session: instance.LogonSession(),
		SessionOf: instance.SessionOf, Alive: procs.Alive, Program: procs.Program, End: endProcess, Version: buildVersion(),
		Reach: 5 * time.Second, Poll: 200 * time.Millisecond}
}

// isMailArchiveProgram reports whether path names one of MailArchive's programs: its
// install or former names (schedule.KnownName: mailarchive, mailarchivew,
// mailarchive-desktop, mailarchive-gui, with or without .exe), matched as the system
// matches names (the S1 fix round 3, C3).
func isMailArchiveProgram(path string) bool {
	return path != "" && schedule.KnownName(filepath.Base(path), runtime.GOOS)
}

// endProcess ends pid at once (Windows' TerminateProcess, a kill elsewhere).
func endProcess(pid int) error {
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return p.Kill()
}

// Quit is the `quit` verb; it returns the process's exit status.
func Quit(args []string) int { return quitVerb(context.Background(), args, osQuitEnv()) }

// The outcome words the verb writes first in its -out file (the S1 fix round 3, C4): the
// upgrade script tells apart, by them, two answers that share exit 3 (busy, and agreed but
// still finishing after -wait), and prints the verb's own line for a copy it could not ask.
// MA-614's test pins the script's copies of them.
const (
	QuitOutcomeDone        = "done"          // it quit
	QuitOutcomeWould       = "would"         // -check: it would quit now
	QuitOutcomeAlready     = "already"       // -check: it is already quitting
	QuitOutcomeOlderWould  = "older-would"   // -check -end-older: an older copy would be ended
	QuitOutcomeOlderEnded  = "older-ended"   // -end-older: an older copy was ended
	QuitOutcomeBusy        = "busy"          // it runs work, or its wizard or choice is open
	QuitOutcomeFinishing   = "finishing"     // it agreed, and had not ended after -wait
	QuitOutcomeNotRunning  = "not-running"   // no copy (as that process)
	QuitOutcomeCouldNotAsk = "could-not-ask" // the line says why
)

// maxQuitOutLine bounds the line the -out file carries.
const maxQuitOutLine = 400

func quitVerb(ctx context.Context, args []string, env *quitEnv) (code int) {
	fs := flag.NewFlagSet("mailarchive quit", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), quitUsageText)
		fs.PrintDefaults()
	}
	check := fs.Bool("check", false, "only ask whether it would quit now; stop nothing")
	pid := fs.Int("pid", 0, "the process to ask (default: this sign-in's MailArchive)")
	wait := fs.Duration("wait", 30*time.Second, "how long to wait for it to end once it agreed")
	endOlder := fs.Bool("end-older", false, "end an older MailArchive that cannot be asked (its live record lists no requests, and its process runs a MailArchive program), as an upgrade does")
	outPath := fs.String("out", "", "also write the outcome (one word, then the line printed) to this file, for a script that cannot read this program's output (the upgrade script)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return QuitDone
		}
		return QuitCouldNotAsk
	}
	// say prints the verb's one line and remembers it, with its outcome word, for -out.
	outcome, said := "", ""
	say := func(w io.Writer, word, line string) {
		fmt.Fprintln(w, line)
		outcome, said = word, line
	}
	if *outPath != "" {
		defer func() {
			if err := writeQuitOut(*outPath, outcome, said); err != nil {
				fmt.Fprintln(env.Stderr, "quit could not write its outcome to "+*outPath+": "+err.Error())
			}
		}()
	}
	if fs.NArg() > 0 {
		say(env.Stderr, QuitOutcomeCouldNotAsk, fmt.Sprintf("quit takes no arguments, only flags (got %q); run mailarchive quit -h", fs.Arg(0)))
		return QuitCouldNotAsk
	}
	cannot := func(why string) int {
		say(env.Stderr, QuitOutcomeCouldNotAsk, fmt.Sprintf(quitCouldNotAskText, why))
		return QuitCouldNotAsk
	}
	notRunning := func() int {
		if *pid != 0 {
			say(env.Stderr, QuitOutcomeNotRunning, fmt.Sprintf(quitNotThisPIDText, *pid))
		} else {
			say(env.Stderr, QuitOutcomeNotRunning, quitNotRunningText)
		}
		return QuitNotRunning
	}
	// waitEnd waits up to -wait for the copy to end and says how it went.
	waitEnd := func(p int, word, done string) int {
		deadline := time.Now().Add(*wait)
		for env.Alive(p) {
			if time.Now().After(deadline) {
				say(env.Stderr, QuitOutcomeFinishing, fmt.Sprintf(quitStillClosingText, p, *wait))
				return QuitBusy
			}
			select {
			case <-ctx.Done():
				return cannot("it was interrupted while waiting")
			case <-time.After(env.Poll):
			}
		}
		say(env.Stdout, word, fmt.Sprintf(done, p))
		return QuitDone
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
		// Only when asked to (the upgrade script) is it ended, as upgrades always did — and
		// only when the record is live and its process is MailArchive (the S1 fix round 3,
		// C3): a record whose lock nobody holds was left by a copy that ended without its
		// clean-up, and its pid may now be any program's.
		if !instance.LockIsHeld(dir, session) {
			return notRunning()
		}
		if !*endOlder {
			say(env.Stderr, QuitOutcomeCouldNotAsk, fmt.Sprintf(quitOlderText, info.PID))
			return QuitCouldNotAsk
		}
		prog, err := env.Program(info.PID)
		if err != nil {
			return cannot(fmt.Sprintf("cannot tell which program process %d runs (%v), so it is not ended", info.PID, err))
		}
		if !isMailArchiveProgram(prog) {
			return cannot(fmt.Sprintf("process %d runs %s, which is not MailArchive, so it is not ended", info.PID, stripControl(prog)))
		}
		if *check {
			say(env.Stdout, QuitOutcomeOlderWould, fmt.Sprintf(quitOlderWouldText, info.PID))
			return QuitDone
		}
		if err := env.End(info.PID); err != nil {
			return cannot(fmt.Sprintf("the older copy (process %d) could not be ended (%v)", info.PID, err))
		}
		return waitEnd(info.PID, QuitOutcomeOlderEnded, quitOlderEndedText)
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
		say(env.Stdout, QuitOutcomeWould, fmt.Sprintf(quitWouldText, info.PID))
		return QuitDone
	case rep.Reason == instance.ReasonClosing && *check:
		say(env.Stdout, QuitOutcomeAlready, fmt.Sprintf(quitAlreadyText, info.PID))
		return QuitDone
	case rep.Shown, rep.Reason == instance.ReasonClosing:
		// It agreed (or was already stopping): wait for it to end, its work drained.
		return waitEnd(info.PID, QuitOutcomeDone, quitDoneText)
	case rep.Reason == instance.ReasonBusy:
		say(env.Stderr, QuitOutcomeBusy, fmt.Sprintf(quitBusyText, info.PID))
		return QuitBusy
	}
	return cannot(fmt.Sprintf("it answered %q", rep.Reason))
}

// writeQuitOut writes the -out file: the outcome word on the first line and the line the
// verb printed on the second, control characters dropped and bounded, atomically. Nothing
// said (a usage error before the verb began) is the could-not-ask word with no line.
func writeQuitOut(path, outcome, line string) error {
	if outcome == "" {
		outcome = QuitOutcomeCouldNotAsk
	}
	line = stripControl(line)
	if len(line) > maxQuitOutLine {
		line = strings.ToValidUTF8(line[:maxQuitOutLine], "")
	}
	return util.WriteFileAtomic0600(path, []byte(outcome+"\n"+line+"\n"))
}
