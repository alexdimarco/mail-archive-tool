package main

import (
	"errors"
	"flag"
	"fmt"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/desktop"
	"mail-archive-tool/internal/wizard"
)

// runPause makes the archive at -out read-only ("Pause"): it stops the archive's
// scheduled backup/verify and seals it, so no MailArchive run will change it, until
// `resume`. The archive's mail is never touched. This is the headless un-seal path the
// seal design requires (S2-C4), so a paused archive on a NAS or a headless host is never
// stuck with no way to resume. A folder whose delete did not finish is refused (MA-1015).
func runPause(args []string) error {
	fs := flag.NewFlagSet("mailarchive pause", flag.ContinueOnError)
	out := fs.String("out", "", "the archive directory to pause (make read-only)")
	reason := fs.String("reason", "", "an optional note recorded on the paused archive")
	cfgPath := fs.String("config", "", configLogUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to pause)")
	}
	logPath, err := configLog(*cfgPath)
	if err != nil {
		return err
	}
	if err := wizard.PauseArchive(*out, *reason, logPath); err != nil {
		return deleteRefusalAtTerminal(err, *out, *cfgPath)
	}
	fmt.Printf("Paused %s — it is now read-only and its scheduled backup is stopped. Run `mailarchive resume%s -out %s` to undo. Your mail is untouched.\n", *out, configArg(*cfgPath), *out)
	if *reason != "" && app.ArchiveEncrypted(*out) {
		fmt.Println("The note was not stored: an encrypted archive keeps no note beside it in plain text.")
	}
	return nil
}

// runResume reverses pause: it makes the archive at -out writable again and restores its
// backup schedule. A folder whose delete did not finish is refused (MA-1015).
func runResume(args []string) error {
	fs := flag.NewFlagSet("mailarchive resume", flag.ContinueOnError)
	out := fs.String("out", "", "the archive directory to resume (make writable again)")
	cfgPath := fs.String("config", "", configLogUsage)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to resume)")
	}
	logPath, err := configLog(*cfgPath)
	if err != nil {
		return err
	}
	if err := wizard.ResumeArchive(*out, logPath); err != nil {
		return deleteRefusalAtTerminal(err, *out, *cfgPath)
	}
	fmt.Printf("Resumed %s — it is writable again and its backup schedule is restored.\n", *out)
	return nil
}

// configLogUsage is -config's help on pause and resume (design-archive-delete §16.7; lead's words,
// owner-delegated 2026-11-02).
const configLogUsage = "the config file of a MailArchive Desktop started with -config, to ask the deletion log beside it whether a delete of the folder did not finish (default: MailArchive's own)"

// configLog is the deletion log that pause, resume and schedule given -config cfgPath ask, and only
// it (G3, design-archive-delete §16.2; the owner's ruling: never both logs): the one beside
// cfgPath made absolute as that Desktop reads it (configAbs: a trailing separator kept, MA-1064),
// which that Desktop and `delete-archive -config` write
// (desktop.DeletionsLogFor), or "" — MailArchive's own — without -config. A -config whose folder
// does not exist is refused in the delete verbs' words, so nothing reads a log no Desktop writes.
func configLog(cfgPath string) (string, error) {
	if cfgPath == "" {
		return "", nil
	}
	if err := configFolderMissing(cfgPath); err != nil {
		return "", err
	}
	return desktop.DeletionsLogFor(configAbs(cfgPath)), nil
}

// deleteRefusalAtTerminal is a refusal in archdelete's words (a delete of the folder that did not
// finish, or a deletions.log that cannot be read: MA-1015, MA-1044) as the terminal says it — the
// folder named and the terminal's Delete again (terminalRefusal), carrying the verb's -config
// (cfgPath, "" for a verb without one: G3-5); any other error as it is. The refusal stays beneath
// the words (errors.As, app.FailureKind), so a backup's run log handling still sees why it was
// refused.
func deleteRefusalAtTerminal(err error, out, cfgPath string) error {
	if r := archdelete.AsRefusal(err); r != nil {
		return &terminalWords{text: terminalRefusal(r.Message, out, cfgPath).Error(), err: err}
	}
	return err
}

// terminalWords is an error as the terminal says it (text) over the error it says (err).
type terminalWords struct {
	text string
	err  error
}

func (e *terminalWords) Error() string { return e.text }
func (e *terminalWords) Unwrap() error { return e.err }
