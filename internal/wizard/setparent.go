package wizard

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
)

// A set's parent folder (docs/design-per-mailbox-archives.md §6, revision 2's C9, C14 and C15,
// revision 2.1's F-04 and citation 1; MA-1493): asked once, in place of "Select the output folder",
// judged as the unencrypted copy judges a new folder inside a chosen parent (app.CheckSetParent),
// each refusal said in the wizard's own words and the folder asked again, nothing created. Each
// archive folder is made through a handle on that checked parent (makeSetFolder).

// setParentTitle is the folder question's title (§6).
const setParentTitle = "Select the folder to keep the archives in (a folder is made inside it for each mailbox)"

// setEnv is the copy export environment a set's checks run in. They keep no state of their own.
var setEnv = func() *app.CopyExportEnv { return app.NewCopyExportEnv("", "", "") }

// askSetParent asks for the set's parent folder until one passes the check, and returns it checked.
// A refusal is said in the wizard's words (setParentWords), then the folder is asked again; nothing
// is created. Cancel on the chooser ends the flow with its error (zenity.ErrCanceled), as Cancel on
// today's folder question does.
func askSetParent() (*app.SetParent, error) {
	env := setEnv()
	for {
		parent, err := folderDialog(zenity.Title(setParentTitle), zenity.Directory())
		if err != nil {
			return nil, err
		}
		sp, cerr := env.CheckSetParent(filepath.Clean(parent))
		if cerr == nil {
			return sp, nil
		}
		// Closing the note asks again too, as OK does (MA-1209's rule for a one-button notice).
		if err := noticeClosed(messageDialog(setParentWords(cerr, parent, ""), zenity.Title(appTitle))); err != nil {
			return nil, err
		}
	}
}

// setParentWords are the wizard's words for err, a refusal of the set's parent folder or of making a
// folder in it (§6): every code the reused check returns is worded for the unencrypted copy, so each
// has words of its own here (revision 2.1's citation 1): R-e, R-q, R-w, R-f, R-f2, R-set (the
// own-folders refusal) and R-enc. name is the folder that was being made, for R-q.
func setParentWords(err error, parent, name string) string {
	p := util.StripControl(parent)
	var r *app.CopyRefusal
	if errors.As(err, &r) {
		switch r.ID {
		case "R-f":
			return fmt.Sprintf("%s is an archive, or is inside one, so the new archives cannot go there. Choose a folder outside every archive.", p)
		case "R-enc":
			return fmt.Sprintf("%s is an encrypted archive, or is inside one, so the new archives cannot go there. Choose a folder outside every archive.", p)
		case "R-set":
			return fmt.Sprintf("%s is one of the folders MailArchive keeps its settings or working files in, or is inside one, and uninstalling MailArchive removes those, so the new archives cannot go there. Choose a folder of your own.", p)
		case "R-f2":
			return fmt.Sprintf("MailArchive could not confirm that %s is outside every archive: it, or an archive this computer knows, is on a network share or another kind of mount, or an archive is too large or partly unreadable to check. Nothing was made. Choose a folder on this computer's own drive.", p)
		case "R-e":
			return fmt.Sprintf("%s is not a folder MailArchive can use just now: it is not there, it is a file, or it changed while MailArchive was checking it. Nothing was made. Choose the folder again, or another one.", p)
		case "R-q":
			return fmt.Sprintf("The folder %s could not be made in %s: something there already has that name, or the name cannot be used there. Nothing was made in it.", util.StripControl(name), p)
		case "R-w":
			return fmt.Sprintf("MailArchive could not make a folder in %s: this computer did not let it write there. Nothing was made. Choose a folder you can write to, or allow MailArchive to write there (Windows: Windows Security, Controlled folder access; macOS: System Settings, Privacy and Security, Files and Folders).", p)
		}
	}
	return fmt.Sprintf("MailArchive could not check %s, so nothing was made there. Choose the folder again, or another one.", p)
}

// errNotRecorded is makeSetFolder's error for a folder it made but could not record in the made-list:
// the folder is there and empty; the export step decides whether to go on (step 3b).
var errNotRecorded = errors.New("the new folder could not be recorded in the wizard's list of the folders it made")

// makeSetFolder makes the archive folder name for a set's mailbox in its checked parent (§6, F-04:
// through a handle on the parent, the set's checked result re-proved after the handle opens, the
// folder made at 0700) and records it in the made-list right after it is made (§9, C5), with
// listName, the mailbox's name in the plan, which §7's unfinished-folder rule matches by. The set's
// export (step 3b) calls it for each new archive, just before that archive's export. A refusal makes
// nothing and records nothing; a folder made but not recorded comes back with its path and
// errNotRecorded.
func makeSetFolder(sp *app.SetParent, name, listName string) (string, error) {
	path, err := setEnv().MakeSetFolder(sp, name)
	if err != nil {
		return "", err
	}
	f, err := job.MadeListFile()
	if err == nil {
		err = job.RecordMade(f, path, listName)
	}
	if err != nil {
		return path, fmt.Errorf("%w: %v", errNotRecorded, err)
	}
	return path, nil
}
