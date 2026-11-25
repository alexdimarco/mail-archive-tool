package wizard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/winsec"
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
			if w := setUnconfirmedWords(r, p); w != "" {
				return w
			}
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

// setUnconfirmedWords are the wizard's words for an R-f2 refusal that names its archive (the
// friction review's F9: the one sentence for every cause gave advice that was wrong for most): the
// archive named, and each cause's own remedy, as the unencrypted copy's own words give them; "" when
// the refusal names no archive.
func setUnconfirmedWords(r *app.CopyRefusal, p string) string {
	if r.Archive == "" {
		return ""
	}
	a := util.StripControl(r.Archive)
	head := fmt.Sprintf("MailArchive could not confirm that %s is outside the archive %s", p, a)
	switch {
	case r.Cause == "walk":
		return head + ": that archive is too large or partly unreadable to check. Nothing was made. Choose a folder on another drive; if that archive is no longer used, remove it from Your archives in MailArchive Desktop."
	case r.Cause == "fuse" && r.ArchiveSide:
		return head + ": that archive is on an sshfs or other FUSE mount, where folders cannot be compared. Nothing was made. Choose a folder on a local drive, or mount that archive's share with your system's SMB or NFS client."
	case r.Cause == "fuse":
		return head + ": the folder is on an sshfs, FUSE or overlay mount, where folders cannot be compared. Nothing was made. Choose a folder on a local drive, or one mounted with your system's SMB or NFS client."
	case r.Cause == "here":
		return head + ": one of them is reached through a share on this computer, which can show one folder under two names. Nothing was made. Choose the folder by its local path instead."
	}
	return head + ": the two are reached through different network shares, which can show one folder under two names. Nothing was made. Choose a folder on a local drive or a USB drive."
}

// The one-archive and single-row paths' folder question (§3 "A set's parent chosen for one archive",
// revision 2's C8, revision 2.1's F-10a; MA-1507).
const (
	outputFolderTitle = "Select the output folder"
	useFolderAnyway   = "Use this folder anyway"
	chooseAnother     = "Choose another folder"
)

// askOutputFolder asks for one archive's output folder, as the wizard always has. With guard (the
// one-archive and single-row paths of auto-detect), a folder that directly holds two or more archive
// folders (§7's child read: setArchivesIn) asks first, before anything is written, in F-10a's words.
// "Choose another folder", the default (and closing the box), asks the folder question again,
// nothing written; "Use this folder anyway" takes it. Cancel on the chooser ends the flow as today.
func askOutputFolder(guard bool) (string, error) {
	for {
		out, err := folderDialog(zenity.Title(outputFolderTitle), zenity.Directory())
		if err != nil || !guard {
			return out, err
		}
		n := setArchivesIn(out)
		if n < 2 {
			return out, nil
		}
		q := questionDialog(heldArchivesWords(out, n), zenity.Title(appTitle), zenity.OKLabel(useFolderAnyway), zenity.CancelLabel(chooseAnother), zenity.DefaultCancel())
		switch {
		case q == nil:
			return out, nil
		case !errors.Is(q, zenity.ErrCanceled):
			return "", q
		}
	}
}

// heldArchivesWords is the question about a folder that directly holds n archives (F-10a: the words
// do not say how those archives were made, which the wizard cannot tell).
func heldArchivesWords(folder string, n int) string {
	return fmt.Sprintf("%s already holds %d archives. An archive made here would hold them inside it, and making one archive for each mailbox in this folder again would then be refused. Choose another folder?", util.StripControl(folder), n) +
		" " + heldArchivesAdd
}

// heldArchivesAdd is the "already holds" question's way to add a mailbox to those archives (the
// friction review's F1, its words).
const heldArchivesAdd = "To add a new mailbox to these archives instead, click Choose another folder, then Cancel, and start again: All of them, One archive for each mailbox, and this folder."

// setArchivesIn counts the archive folders directly inside folder, plaintext or encrypted, as §7's
// child read finds them (only folders that are the person's own are read); 0 when the folder cannot
// be read.
func setArchivesIn(folder string) int {
	kids, _, err := readSetChildren(folder)
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range kids {
		if c.looked && c.stores.Answer == archindex.StoresArchive {
			n++
		}
	}
	return n
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
	path, _, err := makeSetFolderAt(sp, name, listName, "")
	return path, err
}

// restrictSetFolder restricts a folder the set just made to its owner (winsec.RestrictFolderToOwner:
// on Windows an explicit, protected owner-only list, inherited by what is made in it; nothing
// elsewhere, where the folder is made 0700). A seam, so a test sees which folders it is asked for.
var restrictSetFolder = winsec.RestrictFolderToOwner

// errSetFolderChanged is an archive folder of the set that is no longer the folder the plan read or
// the set made (INS-02): nothing is written into it.
var errSetFolderChanged = errors.New("its folder is no longer the folder MailArchive checked")

// makeSetFolderAt is makeSetFolder, also giving the folder made as read through the parent's handle
// right after it was made (setChildID), which the export proves again before it writes there
// (INS-02). Right after it is made and read, the folder is restricted to its owner (INS-01: on
// Windows a folder made with Mkdir takes its parent's access list, which outside the person's own
// folders lets other accounts change it, so a re-run would never look at it and another account
// could put a link in its place). A restriction the drive cannot take (a FAT or exFAT drive keeps no
// access list) leaves the folder as Mkdir made it: the export's identity proof still holds, and the
// plan of a re-run names such a folder as not looked at. store is the mailbox's store id, which its
// made-list line keeps (OUT-03; "" keeps none).
func makeSetFolderAt(sp *app.SetParent, name, listName, store string) (string, os.FileInfo, error) {
	path, err := setEnv().MakeSetFolder(sp, name)
	if err != nil {
		return "", nil, err
	}
	id := setChildID(sp.Parent, name)
	if id == nil {
		return "", nil, fmt.Errorf("%w (%s was changed right after it was made)", errSetFolderChanged, util.StripControl(path))
	}
	_ = restrictSetFolder(path)
	f, err := job.MadeListFile()
	if err == nil {
		err = job.RecordMadeStore(f, path, listName, store)
	}
	if err != nil {
		return path, id, fmt.Errorf("%w: %v", errNotRecorded, err)
	}
	return path, id, nil
}
