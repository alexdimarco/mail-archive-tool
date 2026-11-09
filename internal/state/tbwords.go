package state

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// The words a backup says about how much of each Thunderbird account Thunderbird had downloaded
// (docs/design-thunderbird-completeness.md §7, slice C3, with the C3 friction review's word changes,
// docs/review-thunderbird-completeness-c3-friction.md P2 to P14), built once here beside
// NotDownloadedSentence so every surface says the same: the wizard's end summary, Run now's note,
// MailArchive Desktop's Recent backups, archive card and Status & health, `mailarchive status`, and
// the backup's own command-line summary. They read an account as the last-run record keeps it
// (TBAccount): a plaintext run's record, or, for the screen only, a run's in-memory results put in
// that shape (an encrypted archive's, which nothing writes, O1).
//
// An account is said by the name Thunderbird shows for it (TBAccount.Name, usually the email
// address; the owner's ruling of 2026-11-08 on the friction review's M1) when the run read one, and
// otherwise as "the Thunderbird account kept in the folder imap.gmail.com-1" (the owner's fallback
// wording): a record keeps no name, so every surface that reads a record says the folder words.
// Both are cleaned and capped (CleanFolderName). No sentence here says "store", "cache" or "msf",
// and "IMAP" only inside Gmail's own labels that the person must find (the cap note, P5): the words
// are read on a phone by someone who does not know those words.

// The run's leads (§4.2's fold): which one a run says is tbcomplete.RunLead's (or RecordLead's,
// for a record) call, never a surface's.
const (
	// TBLeadComplete is said only when every Thunderbird account of the run may say complete
	// (or the run read no Thunderbird account at all). "Backup complete." since the owner's ruling
	// of 2026-11-08 on the friction review's L4 (it was "Export complete."), matching the encrypted
	// Run now note's "Backup complete:".
	TBLeadComplete = "Backup complete."
	// TBLeadSoFar is said when any account is behind or kept only in part by its own settings.
	TBLeadSoFar = "Backed up what Thunderbird has so far."
	// TBLeadFinished is said otherwise: MailArchive cannot say complete, and the account lines
	// say why in one sentence each.
	TBLeadFinished = "Backup finished."
)

// TBSay is what the words need to know of where they are said.
type TBSay struct {
	// Scheduled: a schedule will run this archive's later backups, so "later backups add the
	// rest" is true (§7's TC-17 fold); otherwise the words ask for the backup to be run again.
	Scheduled bool
	// Now dates "Thunderbird's count from DATE": the year is said only when it is not Now's.
	// Zero is time.Now().
	Now time.Time
	// Surface is where the words are said, so a "run it again" names what to do there (the C3
	// friction review's P3): the zero value is the wizard's and status's words.
	Surface TBSurface
}

// TBSurface is where the words are said.
type TBSurface int

const (
	// TBSurfaceDefault: the wizard's end summary, `status` and Status & health ("Run the backup
	// again").
	TBSurfaceDefault TBSurface = iota
	// TBSurfaceRunNow: the Desktop's Run now note ("Click Run now again").
	TBSurfaceRunNow
	// TBSurfaceCommandLine: the backup command's own summary ("Run the same command again").
	TBSurfaceCommandLine
)

// TBFolderNamesMax is how many not-kept folders a line names before "and N more" (§6's TC-7
// fold: the first 10 by listed count).
const TBFolderNamesMax = 10

// tbBehindFoldersMax is how many of a behind account's folders furthest behind its line names.
const tbBehindFoldersMax = 3

// tbMenu is the way into Thunderbird's Account Settings (the friction review's P4): Thunderbird
// hides its menu bar on Windows, so the menu button, as the offline-setting words of C0c's re-look
// (R1) say it, seen on 153.6.0 on Windows; lab row MA-1191 sees it on the Mac's 159.
const tbMenu = "click the menu button (≡, top right), then Account Settings"

// MaySayComplete is §4.2's full test read from a record: the record says the account may say
// complete, and its own numbers agree (caught up, nothing missing, not stale, no cap note, no
// deleted copies, no not-kept folder that lists mail, every message told apart, every mailbox
// split as Thunderbird splits it). A record anyone can write never gets "everything" from its
// complete flag alone.
func (a TBAccount) MaySayComplete() bool {
	return a.Complete && a.Verdict == "caught_up" && a.Missing == 0 && !a.Stale && a.CapNote == 0 && !a.DeletedCopies &&
		len(a.NotKept) == 0 && a.NotKeptMore == 0 && a.NoIdentity == 0 && a.SplitMailboxes == 0 &&
		a.GluedMailboxes == 0 && a.MisalignedMailboxes == 0
}

// TBLines is every account's lines, in the record's order, then one sentence for the accounts
// the record left out (more, its thunderbird_more): what the wizard, Run now and the command
// line say under the lead. Notes that block nothing (Spam and Trash, Gmail's labels) are left to
// Status & health and `status` (TBNotes). With two or more accounts behind, each one's line stops
// after its numbers and what to do is said once, after the last of them (the friction review's
// P2).
func TBLines(accounts []TBAccount, more int, say TBSay) []string {
	behind, last := 0, -1
	for i, a := range accounts {
		if a.Verdict == "behind" {
			behind, last = behind+1, i
		}
	}
	var lines []string
	for i, a := range accounts {
		lines = append(lines, tbAccountLines(a, say, behind < 2)...)
		if behind >= 2 && i == last {
			lines = append(lines, tbBehindRemedy(say))
		}
	}
	if s := TBMoreSentence(more); s != "" {
		lines = append(lines, s)
	}
	return lines
}

// TBAccountLines is one account's lines: its headline, then the limit Thunderbird's own settings
// put on it, then its folders that are not kept on this computer, then Gmail's possible folder
// size limit.
func TBAccountLines(a TBAccount, say TBSay) []string { return tbAccountLines(a, say, true) }

func tbAccountLines(a TBAccount, say TBSay, remedy bool) []string {
	lines := []string{tbHeadline(a, say, remedy)}
	if a.Verdict == "limited" {
		lines = append(lines, TBLimitLines(a)...)
	}
	if s := TBNotKeptLine(a); s != "" {
		lines = append(lines, s)
	}
	if s := TBCapNote(a); s != "" {
		lines = append(lines, s)
	}
	out := lines[:0]
	for _, l := range lines {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}

// TBHeadline is the account's one sentence (§7's headline sentences, with their folds): how much
// Thunderbird has downloaded, or, when MailArchive cannot say complete, why, in one sentence and
// its remedy.
func TBHeadline(a TBAccount, say TBSay) string { return tbHeadline(a, say, true) }

// tbHeadline is TBHeadline; remedy false leaves a behind account's remedy to the line TBLines
// says once for every account behind.
func tbHeadline(a TBAccount, say TBSay, remedy bool) string {
	name := tbName(a)
	switch a.Verdict {
	case "behind":
		return tbBehind(a, name, say, remedy)
	case "limited":
		if a.Listed <= 0 {
			return ""
		}
		return fmt.Sprintf("Thunderbird has downloaded about %s of %s in %s.", tbN(a.OnDisk), tbOfMessages(a.Listed, a.Floor), name)
	case "caught_up":
		return tbCaughtUp(a, name, say)
	}
	return tbUnknown(a, name)
}

// tbBehind is a behind account's headline: both numbers, the stale count's date, the folders
// furthest behind (never for Gmail, which is judged per account, §3), and, with remedy, what to do.
func tbBehind(a TBAccount, name string, say TBSay, remedy bool) string {
	s := fmt.Sprintf("Thunderbird has downloaded about %s of %s in %s so far", tbN(a.OnDisk), tbOfMessages(a.Listed, a.Floor), name)
	if a.Stale && !a.AsOf.IsZero() {
		s += " (Thunderbird's count from " + tbDate(a.AsOf, say) + ")"
	}
	s += "."
	if !a.Gmail {
		var far []string
		for _, f := range a.Folders {
			if len(far) == tbBehindFoldersMax {
				break
			}
			if f.OnDisk < f.Listed {
				far = append(far, tbFolderName(f.Folder)+" "+tbN(f.OnDisk)+" of "+tbN(f.Listed))
			}
		}
		if len(far) > 0 {
			s += " Furthest behind: " + strings.Join(far, "; ") + "."
		}
	}
	if !remedy {
		return s
	}
	const leave = " Leave Thunderbird open (it downloads mostly while the computer is left alone)"
	if say.Scheduled {
		return s + leave + "; later backups add the rest."
	}
	return s + leave + ". " + tbAgain(say) + " once Thunderbird has downloaded more, to add the rest."
}

// tbAgain is how the words ask for another backup, named for where they are said (P3).
func tbAgain(say TBSay) string {
	switch say.Surface {
	case TBSurfaceRunNow:
		return "Click Run now again"
	case TBSurfaceCommandLine:
		return "Run the same command again"
	}
	return "Run the backup again"
}

// tbBehindRemedy is what to do, said once after two or more accounts behind (P2).
func tbBehindRemedy(say TBSay) string {
	const leave = "Leave Thunderbird open (it downloads mostly while the computer is left alone)"
	if say.Scheduled {
		return leave + "; later backups add the rest."
	}
	return leave + ", then " + strings.ToLower(tbAgain(say)[:1]) + tbAgain(say)[1:] + " to add the rest."
}

// tbCaughtUp is a caught-up account's headline: "everything" only when it may say complete
// (R42); otherwise both numbers and, in one sentence, why not.
func tbCaughtUp(a TBAccount, name string, say TBSay) string {
	switch {
	case a.MaySayComplete() && a.ListsNoMail:
		return "Thunderbird lists no mail for " + name + ", so there is nothing more to download."
	case a.MaySayComplete():
		return fmt.Sprintf("Thunderbird has downloaded everything it lists for %s (%s).", name, tbMessages(a.Listed))
	case a.Stale && a.Missing == 0:
		// §2.3's foldcheck M9 fold: no "everything" when the count is stale; P8's next step.
		return fmt.Sprintf("As of %s, Thunderbird's count matched what is on this computer for %s (%s). It has not counted since, so newer mail may not be here yet. Open Thunderbird and leave it open so it can count again.",
			tbDate(a.AsOf, say), name, tbMessages(a.Listed))
	case a.Missing > 0:
		s := fmt.Sprintf("Thunderbird has downloaded about %s of the %s it lists for %s. The rest is likely mail that arrived or was deleted since Thunderbird's count.",
			tbN(a.OnDisk), tbMessages(a.Listed), name)
		if a.Stale {
			s += " These numbers are Thunderbird's count from " + tbDate(a.AsOf, say) + ". Open Thunderbird and leave it open so it can count again."
		}
		return s
	case a.DeletedCopies:
		return fmt.Sprintf("Thunderbird lists %s for %s and about %s are on this computer. MailArchive cannot be exact, because Thunderbird still keeps copies of deleted messages until it compacts its folders.",
			tbMessages(a.Listed), name, tbN(a.OnDisk))
	case a.NoIdentity > 0:
		return fmt.Sprintf("Thunderbird has downloaded about %s of the %s it lists for %s, but MailArchive could not tell %s apart from the others, so it cannot be sure of the count.",
			tbN(a.OnDisk), tbMessages(a.Listed), name, Count(int(a.NoIdentity), "message", "messages"))
	case len(a.NotKept) > 0 || a.NotKeptMore > 0:
		return fmt.Sprintf("Thunderbird has downloaded the %s it lists in the folders it keeps on this computer for %s.", tbMessages(a.Listed), name)
	case a.Complete && a.CapNote == 0:
		// The record says complete but its own numbers do not agree (a planted record): the
		// numbers, never "everything".
		return fmt.Sprintf("Thunderbird has downloaded about %s of the %s it lists for %s.", tbN(a.OnDisk), tbMessages(a.Listed), name)
	}
	return fmt.Sprintf("Thunderbird has downloaded the %s it lists for %s.", tbMessages(a.Listed), name)
}

// tbUnknown is an account MailArchive cannot tell about (§4.4's fold table, the reason codes of
// C1b and C2): one sentence why, never "complete", and, where the person can do something, what
// (the friction review's P8).
func tbUnknown(a TBAccount, name string) string {
	pre := "MailArchive cannot tell whether Thunderbird has downloaded all of " + name + ", because "
	const again = " so it saves that list again, then run the backup again."
	switch a.Reason {
	case "panorama":
		return "This Thunderbird uses its newer storage, which MailArchive cannot read yet, so it cannot tell whether Thunderbird has downloaded all of " + name + "."
	case "never_checked":
		return "Thunderbird has not checked the server for " + name + " yet, so MailArchive cannot tell how much is still to come. Open Thunderbird and leave it open."
	case "gmail_floor":
		return tbGmailFloor(a, name)
	case "partial_input":
		return "Only part of " + name + " was backed up, so MailArchive did not compare it with Thunderbird's count."
	case "account_type", "no_match":
		return pre + "MailArchive does not yet know how this kind of Thunderbird account keeps its mail."
	case "old_path_only":
		if a.CantTellFolders > 0 {
			return pre + "Thunderbird's list of folders still names " + Count(a.CantTellFolders, "folder", "folders") + " by where " + pick(a.CantTellFolders, "it", "they") +
				" used to be. Open Thunderbird and leave it open for a minute" + again
		}
		return pre + "Thunderbird's list of folders still names some folders by where they used to be. Open Thunderbird and leave it open for a minute" + again
	case "not_counted_yet":
		return pre + "Thunderbird has not counted all of its folders yet. Open Thunderbird, leave it open for a few minutes, then run the backup again."
	case "expunged_unsized":
		return pre + "Thunderbird holds copies of deleted messages that MailArchive cannot count."
	case "split_differently":
		return pre + "some of its mail files are laid out in a way MailArchive could not count message by message."
	case "count_uncertain":
		return pre + "MailArchive could not count some of its messages for certain."
	case "other_reader":
		return pre + "this backup read its mail the way it reads another mail app's, which does not count it."
	case "walk_failed":
		return pre + "this backup could not read all of its mail."
	}
	// cache_unreadable, duplicate_key, invalid_entry, and a reason this version does not know.
	// "Usually": which event makes Thunderbird write its folder list again is not yet seen in a
	// lab (the friction review's P8 note); a lab note should confirm it before the word goes.
	return pre + "it could not read Thunderbird's list of folders. Opening Thunderbird and letting it check your mail usually fixes this; then run the backup again."
}

// tbGmailFloor is O3's sentence (§3's TC-1 fold) as the friction review's P9 rewords it: the
// number on this computer with the account; Thunderbird's floor only when it is above that number
// (at or below it, it adds nothing and reads as "all is well"); and All Mail not kept points at the
// line below that says how to keep it.
func tbGmailFloor(a TBAccount, name string) string {
	here := fmt.Sprintf("About %s of %s %s on this computer", tbMessages(a.OnDisk), name, pick(int(min(a.OnDisk, 2)), "is", "are"))
	if a.AllMail == "not_kept" {
		return here + ". MailArchive cannot tell how many more there are, because Gmail's All Mail folder, which holds all of this account's mail, is not kept on this computer (see below to keep it)."
	}
	if a.OnDisk < a.Listed {
		here += ", and Thunderbird lists at least " + tbN(a.Listed)
	}
	return here + ". MailArchive cannot tell whether that is all of it, because Gmail's All Mail " + tbAllMailClause(a.AllMail) + "."
}

// tbAllMailClause is O3's last clause, by what All Mail did for Y (§3's TC-1 fold).
func tbAllMailClause(use string) string {
	switch use {
	case "not_kept":
		return "is not kept on this computer"
	case "not_checked":
		return "has not been checked by Thunderbird yet"
	case "below_largest":
		return "lists fewer messages than another of its folders"
	}
	return "is not shown to Thunderbird"
}

// TBLimitLines are a limited account's sentences (§6's limited words with the TC-9, TC-13, M4
// re-read and L5 folds): each limit Thunderbird's own settings put on it, and how to lift it,
// labelled for the Thunderbird version that last opened the profile, reached by the menu button
// (the friction review's P4).
func TBLimitLines(a TBAccount) []string {
	name := tbName(a)
	var lines []string
	if l := a.Limit; l != nil {
		if l.MaxAgeDays > 0 {
			lines = append(lines, fmt.Sprintf("Thunderbird is set to keep only the most recent %s of mail for %s on this computer, so older mail is not backed up. To back up all of it: %s.",
				Count(l.MaxAgeDays, "day", "days"), name, tbAgeRemedy(a)))
		}
		if l.MaxSizeKB > 0 {
			lines = append(lines, fmt.Sprintf("Thunderbird is set not to download messages larger than %s KB for %s, so those are not backed up. To back them up: %s.",
				thousands(l.MaxSizeKB), name, tbSizeRemedy(a)))
		}
		if l.NoBackgroundDownload {
			// TC-9: the download-now command's label is lab-required (MA-1191), so the remedy
			// stays neutral.
			lines = append(lines, "Thunderbird is set not to download "+tbOwn(a, "mail")+" in the background, so MailArchive backs up only what you have opened or downloaded by hand. In Thunderbird, "+tbMenu+"; for "+name+", turn on the setting that saves copies of this account's messages on this computer.")
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "Thunderbird's own settings keep only part of "+tbOwn(a, "mail")+" on this computer, so the rest is not backed up. To check: in Thunderbird, "+tbMenu+", and open "+tbOwn(a, `"Synchronization & Storage" page`)+".")
	}
	return lines
}

// tbAgeRemedy lifts the age limit: Thunderbird's own label below 160, and every version that
// cannot be told; neutral words at 160 and above, where no label for it was found (L5).
func tbAgeRemedy(a TBAccount) string {
	if a.ThunderbirdVersion >= 160 {
		return "in Thunderbird, " + tbMenu + "; for " + tbName(a) + ", set it to keep messages of every age on this computer"
	}
	return "in Thunderbird, " + tbMenu + "; on " + tbOwn(a, `"Synchronization & Storage" page`) + `, choose "Synchronize all messages locally regardless of age"`
}

// tbSizeRemedy lifts the size limit, by version (L5): below 160 the older label, at 160 and
// above the newer one, and both when the version cannot be told.
func tbSizeRemedy(a TBAccount) string {
	older := "in Thunderbird, " + tbMenu + "; on " + tbOwn(a, `"Synchronization & Storage" page`) + `, untick "Don't download messages larger than"`
	newer := "in Thunderbird, " + tbMenu + "; under " + tbOwn(a, `"Download limits"`) + `, untick "Skip downloading messages larger than:"`
	switch {
	case a.ThunderbirdVersion >= 160:
		return newer
	case a.ThunderbirdVersion > 0:
		return older
	}
	return older + ` (in Thunderbird 160 and later: under "Download limits", untick "Skip downloading messages larger than:")`
}

// tbKeepRemedy turns on keeping the whole account on this computer, by version (L5).
func tbKeepRemedy(a TBAccount) string {
	older := "In Thunderbird, " + tbMenu + "; on " + tbOwn(a, `"Synchronization & Storage" page`) + `, tick "Keep messages in all folders for this account on this computer"`
	newer := "In Thunderbird, " + tbMenu + "; under " + tbOwn(a, `"Offline message storage"`) + `, tick "Save copies of all folders and messages on this computer for offline use"`
	switch {
	case a.ThunderbirdVersion >= 160:
		return newer + "."
	case a.ThunderbirdVersion > 0:
		return older + "."
	}
	return older + ` (in Thunderbird 160 and later: under "Offline message storage", tick "Save copies of all folders and messages on this computer for offline use").`
}

// TBNotKeptLine is the account's folders that are not kept on this computer and list mail (§6,
// the thunderbird_not_kept WARN), the first TBFolderNamesMax by listed count and "and N more",
// with Thunderbird's own words to keep them; or, when no folder of it is kept, the account's
// own setting. "" when there are none.
func TBNotKeptLine(a TBAccount) string {
	n := len(a.NotKept) + a.NotKeptMore
	if n == 0 {
		return ""
	}
	name := tbName(a)
	if len(a.Folders) == 0 && a.FoldersMore == 0 {
		return "Thunderbird is not set to keep " + tbOwn(a, "mail") + " on this computer, so MailArchive cannot back it up. " + tbKeepRemedy(a)
	}
	var shown []string
	for _, f := range a.NotKept {
		if len(shown) == TBFolderNamesMax {
			break
		}
		shown = append(shown, tbFolderName(f.Folder)+" ("+tbMessages(f.Listed)+")")
	}
	list := strings.Join(shown, ", ")
	if rest := n - len(shown); rest > 0 {
		list += " and " + thousands(rest) + " more"
	}
	if n == 1 {
		return "This folder of " + name + " is not kept on this computer, so MailArchive cannot back it up: " + list +
			`. To keep it: in Thunderbird, right-click the folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`
	}
	return "These folders of " + name + " are not kept on this computer, so MailArchive cannot back them up: " + list +
		`. To keep them: in Thunderbird, right-click each folder → Properties → Synchronization → tick "Select this folder for offline use", then click Download Now.`
}

// TBCapNote is TC-22's note, when All Mail lists exactly a size Gmail may cap a folder at, with
// where Gmail keeps the setting (the friction review's P5). Both the cap and Gmail's labels are
// recollection, unverified: lab row MA-1201 confirms them. "" otherwise.
func TBCapNote(a TBAccount) string {
	if a.CapNote <= 0 {
		return ""
	}
	return fmt.Sprintf(`Gmail may be showing Thunderbird only the newest %s messages of each folder of %s. To change it: in Gmail on the web, click the gear (top right), then See all settings, then the "Forwarding and POP/IMAP" tab; under "Folder size limits", choose "Do not limit the number of messages in an IMAP folder", then click Save Changes.`,
		tbN(a.CapNote), tbName(a))
}

// TBNotes are the account's notes that block nothing (§0's lead's call): Spam and Trash not
// kept (Thunderbird's usual setting), and Gmail's labels not kept as folders of their own while
// All Mail backs their mail up (TC-8). Status & health and `status` say them.
func TBNotes(a TBAccount) []string {
	var notes []string
	for _, s := range []string{TBDefaultNotKeptNote(a), TBLabelsNote(a)} {
		if s != "" {
			notes = append(notes, s)
		}
	}
	return notes
}

// TBDefaultNotKeptNote is the note for Thunderbird's usual folders not kept (§6), naming only
// the ones the record lists, each by the last part of its name ("Trash", "Spam"; the friction
// review's P11), "" when they are kept.
func TBDefaultNotKeptNote(a TBAccount) string {
	if len(a.DefaultNotKept)+a.DefaultNotKeptMore == 0 {
		return ""
	}
	seen := map[string]bool{}
	var names []string
	for _, f := range a.DefaultNotKept {
		n := f.Folder
		if i := strings.LastIndex(n, "/"); i >= 0 {
			n = n[i+1:]
		}
		if n = tbFolderName(n); !seen[n] {
			seen[n], names = true, append(names, n)
		}
	}
	sort.Strings(names)
	var list string
	switch {
	case len(names) == 0:
		list = "the usual folders"
	case len(names) == 1:
		list = "the " + names[0] + " folder"
	default:
		list = "the " + strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1] + " folders"
	}
	if more := a.DefaultNotKeptMore; more > 0 {
		list += " and " + Count(more, "more folder", "more folders")
	}
	if len(names) == 1 && a.DefaultNotKeptMore == 0 {
		return "Thunderbird does not keep " + list + " of " + tbName(a) + " on this computer (its usual setting), so it is not backed up."
	}
	return "Thunderbird does not keep " + list + " of " + tbName(a) + " on this computer (its usual setting), so they are not backed up."
}

// TBLabelsNote is TC-8's note for Gmail's labels not kept while All Mail backs their mail up, ""
// when there are none.
func TBLabelsNote(a TBAccount) string {
	if a.LabelsNotKept <= 0 {
		return ""
	}
	return "Some of " + tbOwn(a, "labels") + " are not kept on this computer as folders of their own. Their mail is still backed up through All Mail."
}

// TBMoreSentence is the sentence for the accounts a record left out (thunderbird_more, R2-06):
// they are never said to be complete. "" when none.
func TBMoreSentence(more int) string {
	if more <= 0 {
		return ""
	}
	return "MailArchive did not keep the details of " + Count(more, "more Thunderbird account", "more Thunderbird accounts") + " in this backup's record, so it does not say " + pick(more, "it is", "they are") + " complete."
}

// TBCompactSentence is a Recent backups row's words for one account of a history entry, which
// keeps only the verdict and the two numbers (§9.1): "" for an account caught up with nothing
// missing (the row's "OK" says it), and behind reports whether the account is behind (its row
// then says "OK — Thunderbird still downloading", §7). A history entry keeps no name, so the
// account is said by its folder.
func TBCompactSentence(c TBCompact) (s string, behind bool) {
	a := TBAccount{Account: c.Account}
	name := tbName(a)
	switch c.Verdict {
	case "behind":
		return fmt.Sprintf("Thunderbird has downloaded about %s of %s in %s so far", tbN(c.X), tbOfMessages(c.Y, c.Floor), name), true
	case "limited":
		return fmt.Sprintf("Thunderbird's own settings keep only part of %s on this computer (about %s of %s)", tbOwn(a, "mail"), tbN(c.X), tbOfMessages(c.Y, c.Floor)), false
	case "caught_up":
		if c.X < c.Y {
			return fmt.Sprintf("Thunderbird has downloaded about %s of the %s it lists for %s", tbN(c.X), tbMessages(c.Y), name), false
		}
		return "", false
	}
	return "MailArchive could not tell how much of " + name + " Thunderbird has downloaded", false
}

// TBCardBanner is the archive card's one line (§7's Desktop archive card row, with the friction
// review's P10 fold): an account still downloading, an account Thunderbird keeps only in part, and
// how many folders are not kept on this computer, each pointing at Status & health. "" when there
// is nothing to say.
func TBCardBanner(accounts []TBAccount) string {
	var behind, limited []TBAccount
	notKept := 0
	for _, a := range accounts {
		switch a.Verdict {
		case "behind":
			behind = append(behind, a)
		case "limited":
			limited = append(limited, a)
		}
		notKept += len(a.NotKept) + a.NotKeptMore
	}
	var parts []string
	switch len(behind) {
	case 0:
	case 1:
		a := behind[0]
		parts = append(parts, fmt.Sprintf("Thunderbird is still downloading %s: about %s of %s are on this computer so far", tbName(a), tbN(a.OnDisk), tbOfMessages(a.Listed, a.Floor)))
	default:
		parts = append(parts, "Thunderbird is still downloading "+thousands(len(behind))+" accounts")
	}
	switch len(limited) {
	case 0:
	case 1:
		a := limited[0]
		s := "Thunderbird keeps only part of " + tbOwn(a, "mail") + " on this computer"
		if a.Listed > 0 {
			s += " (about " + tbN(a.OnDisk) + " of " + tbOfMessages(a.Listed, a.Floor) + ")"
		}
		parts = append(parts, s+", so the rest is not backed up")
	default:
		parts = append(parts, "Thunderbird keeps only part of the mail of "+thousands(len(limited))+" accounts on this computer, so the rest is not backed up")
	}
	if notKept > 0 {
		parts = append(parts, Count(notKept, "folder is", "folders are")+" not kept on this computer, so "+pick(notKept, "it is", "they are")+" not backed up")
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ") + " — see Status & health."
}

// tbFolderPrefix is the owner's fallback wording for an account the words cannot name.
const tbFolderPrefix = "the Thunderbird account kept in the folder "

// tbName is an account as the words say it: the name Thunderbird shows for it when the run read
// one, else by the folder its mail is kept in, else "this account"; each cleaned and capped.
func tbName(a TBAccount) string {
	if n := CleanFolderName(a.Name); n != "" {
		return n
	}
	if f := CleanFolderName(a.Account); f != "" {
		return tbFolderPrefix + f
	}
	return "this account"
}

// tbOwn is the account's thing: "sam@example.com's mail", "the mail of the Thunderbird account
// kept in the folder imap.example.com", or "this account's mail".
func tbOwn(a TBAccount, thing string) string {
	if n := CleanFolderName(a.Name); n != "" {
		return n + "'s " + thing
	}
	if f := CleanFolderName(a.Account); f != "" {
		return "the " + thing + " of " + tbFolderPrefix + f
	}
	return "this account's " + thing
}

// tbFolderName is a folder's name as the words say it: cleaned and capped, and the top-level
// INBOX (any letter case) as Thunderbird shows it, "Inbox" (the friction review's P12).
func tbFolderName(folder string) string {
	n := CleanFolderName(folder)
	if n == "" {
		return "(a folder with no name)"
	}
	top, rest, sub := strings.Cut(n, "/")
	if strings.EqualFold(top, "INBOX") {
		if sub {
			return "Inbox/" + rest
		}
		return "Inbox"
	}
	return n
}

// tbOfMessages is "Y messages", or, for a floor (§3's TC-1 fold), "at least Y messages", with
// "message" after a Y of 1 (the friction review's P13).
func tbOfMessages(y int64, floor bool) string {
	s := tbMessages(y)
	if floor {
		return "at least " + s
	}
	return s
}

// tbN is n with its digits grouped.
func tbN(n int64) string { return thousands(int(max(0, n))) }

// tbMessages is n messages in words.
func tbMessages(n int64) string { return Count(int(max(0, n)), "message", "messages") }

// tbDate is "Nov 2", with the year when it is not now's ("Nov 2, 2025"), in local time.
func tbDate(t time.Time, say TBSay) string {
	now := say.Now
	if now.IsZero() {
		now = time.Now()
	}
	if t.Local().Year() != now.Local().Year() {
		return t.Local().Format("Jan 2, 2006")
	}
	return t.Local().Format("Jan 2")
}
