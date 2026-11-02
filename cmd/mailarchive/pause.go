package main

import (
	"errors"
	"flag"
	"fmt"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archdelete"
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to pause)")
	}
	if err := wizard.PauseArchive(*out, *reason, ""); err != nil {
		return deleteRefusalAtTerminal(err, *out)
	}
	fmt.Printf("Paused %s — it is now read-only and its scheduled backup is stopped. Run `mailarchive resume -out %s` to undo. Your mail is untouched.\n", *out, *out)
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
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *out == "" {
		return errors.New("-out is required (the archive directory to resume)")
	}
	if err := wizard.ResumeArchive(*out, ""); err != nil {
		return deleteRefusalAtTerminal(err, *out)
	}
	fmt.Printf("Resumed %s — it is writable again and its backup schedule is restored.\n", *out)
	return nil
}

// deleteRefusalAtTerminal is a refusal in archdelete's words (a delete of the folder that did not
// finish, or a deletions.log that cannot be read: MA-1015) as the terminal says it — the folder
// named and the terminal's Delete again (terminalRefusal); any other error as it is.
func deleteRefusalAtTerminal(err error, out string) error {
	if r := archdelete.AsRefusal(err); r != nil {
		return terminalRefusal(r.Message, out, "")
	}
	return err
}
