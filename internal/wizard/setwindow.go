package wizard

import (
	"errors"
	"fmt"
	"strings"

	"github.com/ncruces/zenity"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
)

// The plan, before anything is written (docs/design-per-mailbox-archives.md §8, UX contract X14;
// revision 2's C21; MA-1495): right after the parent folder, before any other question, a question
// box lists every archive the set will make and every other line §7 gives, with the buttons "Make
// the archives" and "Cancel". Cancel writes nothing.

const (
	setMakeLabel = "Make the archives"
	// setUpToDateLabel is the plan's button when it makes no new archive and brings some up to date
	// (the friction review's F11).
	setUpToDateLabel = "Bring them up to date"
	// setAskedLead leads the plan's line naming the accounts the download question asks about next,
	// and setNotFoundAdvice follows an archive whose mailbox is not found now (F11's words).
	setAskedLead = "Next you are asked whether to prepare a full offline download of: "
	// The re-verify of rounds A and B, RF-04 (its words): F11's "; its mail stays in its archive; click
	// Pause on its row to stop its backups." did not say where Pause is.
	setNotFoundAdvice = ". Its mail stays in that archive. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
	// setKeptExact follows "NAME: kept as it is in FOLDER" for an archive that holds exactly this one
	// store (RF-03, its words): "; Thunderbird no longer uses this folder. Click Pause on it to stop its
	// backups." before, where "this folder" read as the archive's folder and Pause had no place.
	setKeptExact = ", since Thunderbird no longer uses that mailbox's folder. To stop its backups, click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
	// setUpToDateLead leads an archive brought up to date, then ", for MAILBOX." (RF-07, its words): it
	// read "Brought up to date: FOLDER (already in this folder)" before, as if done before anything ran
	// (F11's own point), and never named the mailbox.
	setUpToDateLead = "To bring up to date: "
	// setOnceWords follow a saved-once row's name in the plan.
	setOnceUnused = ": saved once and not kept current, since no account uses it"
	setOnceOlder  = ": saved once and not kept current, since it is an older copy"
	// setUnlookedOne and setUnlookedMany lead the line naming the folders in the parent the plan
	// never looked at (the adversarial pass's INS-01; the builder's words).
	setUnlookedOne  = "Left as it is, not looked at (a link, or a folder another account owns or can change): "
	setUnlookedMany = "Left as they are, not looked at (each a link, or a folder another account owns or can change): "
	// setUnlookedLinkOne and setUnlookedLinkMany are said instead on a drive that keeps no access lists
	// (FAT, exFAT on Windows), where every real folder is looked at whatever its owner and "another
	// account owns or can change" would be untrue (the re-verify of rounds A and B, RV-02; the
	// builder's words).
	setUnlookedLinkOne  = "Left as it is, not looked at (a link): "
	setUnlookedLinkMany = "Left as they are, not looked at (each a link): "
	// setReusedKept follows the name of an archive kept as it is because Thunderbird now uses its
	// mailbox's folder for another account (the re-verify of rounds A and B, RV-01: the lead's words,
	// "kept as it is … another account uses its folder now; click Pause (make read-only) on its row
	// under Your archives in MailArchive Desktop"); setReusedPaused, the builder's, when it is paused
	// already.
	setReusedKept   = ": kept as it is, since another account uses its folder now; click Pause (make read-only) on its row under Your archives in MailArchive Desktop."
	setReusedPaused = ": kept as it is, paused, since another account uses its folder now."
)

// setPlanText is the plan window's text for p (§8): the archives to make in its parent, with
// singular and zero forms written out ("1 archive", "No new archive: 2 brought up to date"), then
// one line for each of: an archive brought up to date, a paused one, a saved-once mailbox already
// kept, an archive kept as it is because another account uses its mailbox's folder now (RV-01),
// another folder that also holds a mailbox's archive, a mailbox not made and why, a folder
// that could not be read just now, an archive whose mailbox is not found now, folders left out, and
// the accounts the full offline download is asked about (each by the name the list showed, as the
// question names it, MA-1502). Names and paths are shown as written, their
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
				// Said as the download question will say it: the name the list showed (MA-1502).
				asked = append(asked, clean(it.row.listName()))
			}
		case outContinue:
			// ", for MAILBOX" only when the archive's folder has another name than the mailbox (a
			// renamed account's archive, found by what it holds): the Windows lab of MA-1503 (part 2,
			// 2026-11-25) saw "To bring up to date: qza@qz.example, for qza@qz.example.", the address
			// said twice, in the commonest case.
			line := setUpToDateLead + folder
			if mailbox := clean(it.row.listName()); state.TBTextKey(mailbox) != state.TBTextKey(folder) {
				line += ", for " + mailbox
			}
			upToDate = append(upToDate, line+".")
		case outPaused:
			paused = append(paused, "Paused, so "+folder+" is not being backed up now; click Resume (make writable) on its row to back it up again.")
		case outKept:
			if it.exact {
				kept = append(kept, name+": kept as it is in "+folder+setKeptExact)
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
		// RV-01: an archive whose mailbox's folder another account uses now, kept as it is.
		for _, c := range it.reused {
			if c.paused {
				kept = append(kept, clean(c.name)+setReusedPaused)
			} else {
				kept = append(kept, clean(c.name)+setReusedKept)
			}
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
	// Every child never looked at, so a " (2)" beside one of their names is never unexplained (INS-01);
	// on a drive that keeps no access lists, said as links only (RV-02).
	one, many := setUnlookedOne, setUnlookedMany
	if p.noAccessLists {
		one, many = setUnlookedLinkOne, setUnlookedLinkMany
	}
	switch len(p.unlooked) {
	case 0:
	case 1:
		rest = append(rest, one+clean(p.unlooked[0]))
	default:
		var names []string
		for _, u := range p.unlooked {
			names = append(names, clean(u))
		}
		rest = append(rest, many+strings.Join(names, ", "))
	}
	for _, g := range p.notFound {
		// The friction review's F11: the line now says what to do ("In this folder, not found now:
		// NAME" before); RF-04: and where.
		rest = append(rest, "In this folder, not found now: "+clean(g)+setNotFoundAdvice)
	}
	if p.more {
		rest = append(rest, fmt.Sprintf("Only the first %d folders in %s were looked at; the others were left out.", setChildMax, parent))
	}
	if len(asked) > 0 {
		// F11: said as what comes next ("Asked about a full offline download: …" before, which read
		// as if it had happened).
		rest = append(rest, setAskedLead+strings.Join(asked, ", "))
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
	err := questionDialog(setPlanText(p), zenity.Title(appTitle), zenity.OKLabel(setPlanButton(p)), zenity.CancelLabel("Cancel"))
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, zenity.ErrCanceled):
		return false, nil
	}
	return false, err
}

// setPlanButton is the plan's OK button: "Make the archives", or "Bring them up to date" when the
// plan makes no new archive and brings at least one up to date (the friction review's F11).
func setPlanButton(p setPlan) string {
	upToDate := false
	for _, it := range p.items {
		switch it.outcome {
		case outNew, outUnfinished:
			return setMakeLabel
		case outContinue:
			upToDate = true
		}
	}
	if upToDate {
		return setUpToDateLabel
	}
	return setMakeLabel
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
