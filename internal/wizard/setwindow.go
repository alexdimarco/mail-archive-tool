package wizard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/util"
)

// The plan, before anything is written (docs/design-per-mailbox-archives.md §8, UX contract X14;
// revision 2's C21; MA-1495): right after the parent folder, before any other question, a question
// box lists every archive the set will make and every other line §7 gives, with the buttons "Make
// the archives" and "Cancel". Cancel writes nothing.

const (
	setMakeLabel = "Make the archives"
	// setOnceWords follow a saved-once row's name in the plan.
	setOnceUnused = ": saved once and not kept current, since no account uses it"
	setOnceOlder  = ": saved once and not kept current, since it is an older copy"
)

// setPlanText is the plan window's text for p (§8): the archives to make in its parent, with
// singular and zero forms written out ("1 archive", "No new archive: 2 brought up to date"), then
// one line for each of: an archive brought up to date, a paused one, a saved-once mailbox already
// kept, another folder that also holds a mailbox's archive, a mailbox not made and why, a folder
// that could not be read just now, an archive whose mailbox is not found now, folders left out, and
// the accounts the full offline download is asked about. Names and paths are shown as written, their
// control characters dropped; the dialogs show the text without markup.
func setPlanText(p setPlan) string {
	clean := util.StripControl
	parent := clean(p.parent)
	var made, upToDate, paused, kept, also, notMade, asked []string
	for _, it := range p.items {
		name, folder := clean(it.row.name), clean(it.folder)
		switch it.outcome {
		case outNew, outUnfinished:
			line := folder
			switch {
			case it.row.once && it.row.row.unused:
				line += setOnceUnused
			case it.row.once:
				line += setOnceOlder
			}
			made = append(made, line)
			if it.ask {
				asked = append(asked, name)
			}
		case outContinue:
			upToDate = append(upToDate, "Brought up to date: "+folder+" (already in this folder)")
		case outPaused:
			paused = append(paused, "Paused, so "+folder+" is not being backed up now; click Resume (make writable) on its row to back it up again.")
		case outKept:
			if it.exact {
				kept = append(kept, name+": kept as it is in "+folder+"; Thunderbird no longer uses this folder. Click Pause on it to stop its backups.")
			} else {
				kept = append(kept, name+": kept as it is in "+folder)
			}
		case outNotMade:
			switch it.why {
			case notMadeEmpty:
				notMade = append(notMade, "Not made: "+name+", which is empty.")
			case notMadeUnread:
				notMade = append(notMade, name+": not made this time, since a folder here could not be read; run this again later")
			case notMadeDelete:
				notMade = append(notMade, name+": not made. "+clean(it.delWhy))
			case notMadeNoNumber:
				notMade = append(notMade, fmt.Sprintf("%s: not made, since this folder already holds folders named %s (2) to (%d).", name, name, setNumberMax))
			case notMadeNoName:
				notMade = append(notMade, fmt.Sprintf("%s: not made, since %d other mailboxes have its name.", name, setNumberMax))
			}
		}
		for _, o := range it.also {
			also = append(also, clean(o)+" also holds this mailbox's archive: left as it is")
		}
	}
	var head string
	switch n := len(made); {
	case n == 1:
		head = "MailArchive will make 1 archive in " + parent + ":"
	case n > 1:
		head = fmt.Sprintf("MailArchive will make %d archives in %s:", n, parent)
	case len(upToDate) > 0:
		head = fmt.Sprintf("No new archive: %d brought up to date in %s.", len(upToDate), parent)
	default:
		head = "No new archive in " + parent + "."
	}
	var rest []string
	rest = append(rest, upToDate...)
	rest = append(rest, paused...)
	rest = append(rest, kept...)
	rest = append(rest, also...)
	rest = append(rest, notMade...)
	for _, u := range p.unread {
		rest = append(rest, clean(u)+": could not be read just now")
	}
	for _, g := range p.notFound {
		rest = append(rest, "In this folder, not found now: "+clean(g))
	}
	if p.more {
		rest = append(rest, fmt.Sprintf("Only the first %d folders in %s were looked at; the others were left out.", setChildMax, parent))
	}
	if len(asked) > 0 {
		rest = append(rest, "Asked about a full offline download: "+strings.Join(asked, ", "))
	}
	parts := []string{head}
	if len(made) > 0 {
		parts = append(parts, strings.Join(made, "\n"))
	}
	if len(rest) > 0 {
		parts = append(parts, strings.Join(rest, "\n"))
	}
	return strings.Join(parts, "\n\n")
}

// showSetPlan shows the plan in a question box (§8) through the questionDialog seam: true for "Make
// the archives"; false for Cancel or closing it, which writes nothing.
func showSetPlan(p setPlan) (bool, error) {
	err := questionDialog(setPlanText(p), zenity.Title(appTitle), zenity.OKLabel(setMakeLabel), zenity.CancelLabel("Cancel"))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, zenity.ErrCanceled):
		return false, nil
	}
	return false, err
}

// setChosen is what a set's steps before its export chose: the checked parent folder and the plan
// the person agreed to. The set's export (step 3b) makes it.
type setChosen struct {
	parent *app.SetParent
	plan   setPlan
}

// chooseSet runs a set's steps up to its plan (§6, §7, §8): the parent folder, checked; the folders
// already in it read and each mailbox's outcome planned; the plan shown. Cancel on the plan (or on
// the folder question) ends with zenity.ErrCanceled and nothing written. A parent whose folders
// cannot be read is said in the parent's words and the folder asked again.
func chooseSet(rows []setRow, goos string) (setChosen, error) {
	for {
		sp, err := askSetParent()
		if err != nil {
			return setChosen{}, err
		}
		p, err := planSet(sp.Parent, rows, goos)
		if err != nil {
			if err := noticeClosed(messageDialog(setParentWords(err, sp.Parent, ""), zenity.Title(appTitle))); err != nil {
				return setChosen{}, err
			}
			continue
		}
		ok, err := showSetPlan(p)
		switch {
		case err != nil:
			return setChosen{}, err
		case !ok:
			return setChosen{}, zenity.ErrCanceled
		}
		return setChosen{parent: sp, plan: p}, nil
	}
}
