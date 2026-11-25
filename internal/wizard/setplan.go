package wizard

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/text/unicode/norm"

	"mail-archive-tool/internal/app"
	"mail-archive-tool/internal/archindex"
	"mail-archive-tool/internal/job"
	"mail-archive-tool/internal/schedule"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/thunderbird"
)

// A folder that is already there (docs/design-per-mailbox-archives.md §7, revision 2's C3, C4, C5,
// C12, C13 and R2, revision 2.1's F-05, F-07 and F-11; MA-1494). Running the same choice into the
// same parent again is how a person adds mailboxes found since and brings the others up to date, so
// before naming anything the wizard reads each folder directly inside the parent and finds each
// mailbox's archive by what it holds, never by its name. The plan (setPlan) is what the set's
// export (step 3b) makes, continues or leaves.

const (
	// setChildMax is how many folders directly inside a set's parent are looked at; more are left
	// out, and the plan says so.
	setChildMax = 500
	// setEntryMax bounds the entries read from the parent to find them (files are not looked at).
	setEntryMax = 100_000
)

// setChild is one folder directly inside a set's parent, as the child read found it.
type setChild struct {
	name string // its name in the parent
	path string // the parent's path joined with it
	// looked: a real directory (not a link, junction or other reparse point), opened through the
	// parent's os.Root, owned by the person running the wizard and writable by no one else, so what
	// it holds was read (stores). Any other child is never read, never matched, never written into.
	looked bool
	stores archindex.FolderStores
	// id is the folder a looked child is, as its open handle said (os.SameFile): the export proves
	// it again through the parent's handle before it writes anything there (setChildSame, INS-02).
	id os.FileInfo
}

// unread reports a child whose read failed and may pass later (§7 "Cannot read now").
func (c setChild) unread() bool { return c.looked && c.stores.Answer == archindex.StoresCannotRead }

// archiveIDs are the store ids of a child that is a plaintext archive; nil for any other.
func (c setChild) archiveIDs() []string {
	if !c.looked || c.stores.Answer != archindex.StoresArchive || c.stores.Encrypted {
		return nil
	}
	return c.stores.IDs
}

// readSetChildren reads the folders directly inside parent (§7, C3, C12), sorted by name: at most
// setChildMax are looked at, and more reports that others were left out. Each is read only when it
// is a real directory, through the parent's os.Root, owned by this user and writable by no one else
// (setChildMine, on the open folder), and the folder read is the folder checked (its identity is the
// one archindex.Stores opened): what it holds is archindex.Stores' answer. A child that cannot be
// looked at now (its facts not readable, swapped while it was read) is cannot-read-now.
func readSetChildren(parent string) (kids []setChild, more bool, err error) {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil, false, err
	}
	defer root.Close()
	d, err := root.Open(".")
	if err != nil {
		return nil, false, err
	}
	var names []string
	entries := 0
	for {
		ents, rerr := d.ReadDir(1000)
		for _, e := range ents {
			if !e.Type().IsRegular() { // a file is not a folder: not looked at
				names = append(names, e.Name())
			}
		}
		entries += len(ents)
		if errors.Is(rerr, io.EOF) || len(ents) == 0 {
			break
		}
		if rerr != nil {
			d.Close()
			return nil, false, rerr
		}
		if entries >= setEntryMax {
			more = true
			break
		}
	}
	d.Close()
	sort.Strings(names)
	if len(names) > setChildMax {
		names, more = names[:setChildMax], true
	}
	cannot := archindex.FolderStores{Answer: archindex.StoresCannotRead}
	for _, n := range names {
		c := setChild{name: n, path: filepath.Join(parent, n)}
		fi, lerr := root.Lstat(n)
		switch {
		case lerr != nil && errors.Is(lerr, fs.ErrNotExist):
			continue // gone since it was listed
		case lerr != nil:
			c.looked, c.stores = true, cannot
		case fi.Mode().Type() != fs.ModeDir || setReparse(fi):
			// A link, a junction, a file of another kind: never read, never written into.
		default:
			c.looked, c.stores, c.id = readSetChild(root, n, fi)
			if !c.looked {
				c.stores, c.id = archindex.FolderStores{}, nil
			}
		}
		kids = append(kids, c)
	}
	return kids, more, nil
}

// readSetChild reads the real directory n inside root, which Lstat found as fi: looked is false for a
// folder that is not the person's own; else what it holds, cannot-read-now when it cannot be opened
// or is not the folder found, and id, the folder it is (its open handle's).
func readSetChild(root *os.Root, n string, fi os.FileInfo) (looked bool, st archindex.FolderStores, id os.FileInfo) {
	cannot := archindex.FolderStores{Answer: archindex.StoresCannotRead}
	// Someone else's folder is never opened, so it never reads as cannot-read-now (which would hold
	// back every new archive): judged from what Lstat found where that says it (unix), and an open
	// this user is refused where it does not (Windows).
	if setForeign(fi) {
		return false, archindex.FolderStores{}, nil
	}
	croot, err := root.OpenRoot(n)
	if err != nil {
		if setRefusedIsForeign && errors.Is(err, fs.ErrPermission) {
			return false, archindex.FolderStores{}, nil
		}
		return true, cannot, nil
	}
	defer croot.Close()
	f, err := croot.Open(".")
	if err != nil {
		return true, cannot, nil
	}
	defer f.Close()
	hfi, err := f.Stat()
	if err != nil || !os.SameFile(fi, hfi) {
		return true, cannot, nil
	}
	if !setChildMine(f, hfi) {
		return false, archindex.FolderStores{}, nil
	}
	st = archindex.Stores(filepath.Join(root.Name(), n))
	if st.Dir == nil || !os.SameFile(st.Dir, hfi) {
		return true, cannot, nil
	}
	return true, st, hfi
}

// setChildSame re-proves, through a handle on the set's parent, that the archive folder name in it
// is still the folder id the plan read or the set made (the adversarial pass's INS-02): the parent
// is still the folder the plan read (parentID, when known), and name in it is a real directory (not
// a link, a junction or another reparse point) that is the same folder (os.SameFile), by its own
// handle too. The set's export asks it right before an archive's run log opens and again right
// before its export starts; a folder that is not the same is never written into.
func setChildSame(parent string, parentID os.FileInfo, name string, id os.FileInfo) bool {
	if id == nil {
		return false
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return false
	}
	defer root.Close()
	if parentID != nil {
		rfi, err := root.Stat(".")
		if err != nil || !os.SameFile(rfi, parentID) {
			return false
		}
	}
	fi, err := root.Lstat(name)
	if err != nil || fi.Mode().Type() != fs.ModeDir || setReparse(fi) || !os.SameFile(fi, id) {
		return false
	}
	f, err := root.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	hfi, err := f.Stat()
	return err == nil && hfi.IsDir() && os.SameFile(hfi, id)
}

// setChildID is the folder name in parent is, read through a handle on parent right after the set
// made it: a real directory, or nil.
func setChildID(parent, name string) os.FileInfo {
	root, err := os.OpenRoot(parent)
	if err != nil {
		return nil
	}
	defer root.Close()
	fi, err := root.Lstat(name)
	if err != nil || fi.Mode().Type() != fs.ModeDir || setReparse(fi) {
		return nil
	}
	return fi
}

// setOutcome is what a set does for one mailbox (§7), in the plan's order.
type setOutcome int

const (
	// outNew: a new folder is made for it, named folder (§4, with the next free number).
	outNew setOutcome = iota + 1
	// outContinue: its archive in folder is continued with the answers given now ("brought up to
	// date").
	outContinue
	// outUnfinished: a folder the wizard made for it but never finished (no manifest; the made-list
	// names it for this mailbox in this parent) is continued (C5).
	outUnfinished
	// outPaused: its archive in folder is paused: left as it is, no schedule.
	outPaused
	// outKept: a saved-once row whose store an archive in folder already holds: left as it is (R2);
	// or, since revision 3 (CON-06), an archive in folder whose mailbox's folder Thunderbird no longer
	// uses, its account keeping its mail elsewhere: left as it is, never continued.
	outKept
	// outNotMade: nothing is made or continued for it; why says why.
	outNotMade
)

// setItem is one mailbox of a set's plan.
type setItem struct {
	row     setRow
	outcome setOutcome
	// folder is the folder's name in the parent: the new folder's final name, or the existing
	// child's.
	folder string
	// exact (outKept): that archive holds exactly this one store, so the plan advises Pause on it.
	exact bool
	// jobUnreadable (outContinue): its job file is there and cannot be read: continued, and a Yes to
	// the schedule never installs over that job (F-11; step 3b).
	jobUnreadable bool
	// also are the other children that hold this mailbox's archive too: left as they are.
	also []string
	// why (outNotMade) is one of the notMade… reasons.
	why string
	// delWhy (outNotMade, notMadeDelete) is the unfinished delete's own words.
	delWhy string
	// ask: a new archive of an account whose full offline download is asked about (§8; the question
	// itself is step 3b's, MA-1502).
	ask bool
	// id (outContinue, outUnfinished, outPaused) is the existing child's folder as the plan read it
	// (INS-02).
	id os.FileInfo
	// reused are the children that held this mailbox's archive whose folder Thunderbird now uses for a
	// different account (the re-verify of rounds A and B, RV-01): never continued, never taken as this
	// mailbox's paused archive, left as they are; the mailbox's outcome is decided without them.
	reused []setReusedChild
}

// setReusedChild is a child kept as it is because another account uses its mailbox's folder now
// (RV-01): its name in the parent, and whether it is paused already (no Pause advice then).
type setReusedChild struct {
	name   string
	paused bool
}

// Why a mailbox is not made (setItem.why).
const (
	notMadeEmpty    = "empty"     // §5: an empty feeds account, folder no account uses, older copy
	notMadeUnread   = "unread"    // §7: a folder here could not be read just now
	notMadeDelete   = "delete"    // §7: a folder whose delete did not finish
	notMadeNoNumber = "no number" // §4: every number to setNumberMax is taken
	notMadeNoName   = "no name"   // §4: a hundred mailboxes named alike
)

// setPlan is what a set will do in its parent (§7, §8): each mailbox's outcome in the plan's order,
// the children that could not be read just now, the archives whose mailbox is not found now, and
// whether more folders were in the parent than were looked at.
type setPlan struct {
	parent   string
	items    []setItem
	unread   []string
	notFound []string
	more     bool
	// unlooked are the children never looked at (a link, a junction, a folder another account owns
	// or can change): the plan names them, so a " (2)" is never unexplained (INS-01).
	unlooked []string
	// parentID is the parent folder the plan read, through its handle (INS-02, OUT-04).
	parentID os.FileInfo
}

// planSet decides, for each of rows in their order, what the set does in parent (§7, revision 2.1's
// F-05 order): a saved-once row already kept; its archive (continued, or left if paused); a folder
// the wizard made but never finished; nothing new while a child cannot be read now; otherwise a new
// folder, named by §4 and taking the next free number past anything already in the parent; never
// into a folder whose delete did not finish. It writes nothing. goos decides how names are told
// apart (setToldApart's rule).
func planSet(parent string, rows []setRow, goos string) (setPlan, error) {
	kids, more, err := readSetChildren(parent)
	if err != nil {
		return setPlan{}, err
	}
	root, err := os.OpenRoot(parent)
	if err != nil {
		return setPlan{}, err
	}
	defer root.Close()
	p := setPlan{parent: parent, more: more}
	if pfi, err := root.Stat("."); err == nil {
		p.parentID = pfi
	}
	for _, c := range kids {
		if c.unread() {
			p.unread = append(p.unread, c.name)
		}
		if !c.looked {
			p.unlooked = append(p.unlooked, c.name)
		}
	}
	// A made-list folder in this parent, by its name in composed form (OUT-04) → its line. The parent
	// is compared by identity when its spelling differs (letter case, composed form; the skeptic's
	// sibling of OUT-04), never by bytes alone.
	made := map[string]job.Made{}
	if f, err := job.MadeListFile(); err == nil {
		if ms, err := job.ReadMade(f); err == nil {
			for _, m := range ms {
				if setSameParent(filepath.Dir(m.Path), parent, p.parentID) {
					made[norm.NFC.String(filepath.Base(m.Path))] = m
				}
			}
		}
	}
	used := setNamesUsed{transcoded: answerMayBeTranscoded(goos), keys: map[string]bool{}, ascii: map[string]bool{}}
	for _, c := range kids {
		used.add(c.name) // anything already in the parent is never written into
	}
	takenNow := func(name string) bool { // something at name now, looked at or not
		_, err := root.Lstat(name)
		return !errors.Is(err, fs.ErrNotExist)
	}
	found := map[string]bool{}
	for _, r := range rows {
		found[state.PathSourceID(r.path)] = true
	}
	claimed := map[string]bool{}
	for _, r := range rows {
		it := setItem{row: r}
		switch {
		case r.empty:
			it.outcome, it.why = outNotMade, notMadeEmpty
		case r.unnamed:
			it.outcome, it.why = outNotMade, notMadeNoName
		default:
			planOne(&it, parent, kids, claimed, made, len(p.unread) > 0, &used, takenNow)
		}
		p.items = append(p.items, it)
	}
	// An archive here whose mailbox is no longer found (C13): a plaintext archive none of whose
	// stores is a mailbox of this list.
	for _, c := range kids {
		ids := c.archiveIDs()
		if len(ids) == 0 {
			continue
		}
		listed := false
		for _, id := range ids {
			listed = listed || found[id]
		}
		if !listed {
			p.notFound = append(p.notFound, c.name)
		}
	}
	return p, nil
}

// setSameParent reports whether dir, a made-list line's folder, is the set's parent: the same
// spelling; or, when the two are alike but for letter case or composed form, the same folder by
// identity (parentID, the parent the plan read). Other folders are never looked at, so a line on a
// drive that is not there costs no wait.
func setSameParent(dir, parent string, parentID os.FileInfo) bool {
	dir, parent = filepath.Clean(dir), filepath.Clean(parent)
	if dir == parent {
		return true
	}
	if parentID == nil || !strings.EqualFold(norm.NFC.String(dir), norm.NFC.String(parent)) {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && os.SameFile(fi, parentID)
}

// madeFor reports whether a made-list line m is the folder an earlier run made for the mailbox of r
// (C5): by its store id when the line keeps one (OUT-03: never another mailbox that has its name now);
// by the mailbox's name, in composed form, for a line written before.
func madeFor(m job.Made, r setRow) bool {
	if m.Store != "" {
		return m.Store == state.PathSourceID(r.path)
	}
	return m.Name != "" && norm.NFC.String(m.Name) == norm.NFC.String(r.name)
}

// planOne decides the outcome of the one mailbox of it (planSet's rules in F-05's order).
func planOne(it *setItem, parent string, kids []setChild, claimed map[string]bool, made map[string]job.Made, unread bool, used *setNamesUsed, takenNow func(string) bool) {
	r := it.row
	id := state.PathSourceID(r.path)
	// A saved-once row already kept (R2, F-05): first, so a leftover's own archive is left as it is.
	if r.once {
		for _, c := range kids {
			if ids := c.archiveIDs(); containsID(ids, id) {
				it.outcome, it.folder, it.exact = outKept, c.name, len(ids) == 1
				claimed[c.name] = true
				return
			}
		}
	}
	// Its archive: exactly this one store, and a job file, when there is one, with auto-discovery off
	// and this one input (C3, C4); the first by name is continued, the others left (F-11: a job that
	// cannot be read does not stop the match).
	var matches []setChild
	unreadable := map[string]bool{}
	for _, c := range kids {
		if ids := c.archiveIDs(); len(ids) == 1 && ids[0] == id && !claimed[c.name] {
			if ok, bad := setJobFits(c.path, r.path); ok {
				// RV-01 (the re-verify of rounds A and B): Thunderbird gave this folder to another account
				// since the archive was made (by the identity its record kept, as its run decides it), so
				// its every run stops as reused. Asked before continuing and before the paused check: it
				// is never this mailbox's archive, so the mailbox gets one of its own, and the old one is
				// kept as it is, with the Pause advice when it is not paused already.
				if app.ThunderbirdReused(c.path, r.path) {
					it.reused = append(it.reused, setReusedChild{name: c.name, paused: seal.IsSealed(c.path)})
					claimed[c.name] = true
					continue
				}
				matches = append(matches, c)
				unreadable[c.name] = bad
			}
		}
	}
	if len(matches) > 0 {
		c := matches[0]
		it.folder, it.jobUnreadable, it.id = c.name, unreadable[c.name], c.id
		for _, o := range matches[1:] {
			it.also = append(it.also, o.name)
		}
		for _, o := range matches {
			claimed[o.name] = true
		}
		if seal.IsSealed(c.path) {
			it.outcome = outPaused
			return
		}
		// CON-06 (docs/design-per-mailbox-archives.md revision 3): a folder Thunderbird no longer uses,
		// whose account now keeps its mail elsewhere (by the identity the archive's record kept, else by
		// its name), would only stop at its run, and "run this again" would repeat: kept as it is, and
		// the plan says to click Pause on it (outKept, exact: its archive holds exactly this one store).
		if app.ThunderbirdMovedAway(c.path, r.path) {
			it.outcome, it.exact = outKept, true
			return
		}
		it.outcome = outContinue
		refuseDeleted(it, c.path)
		return
	}
	// A folder the wizard made for it but never finished (C5; OUT-03: for that store only; OUT-04: the
	// child's name in composed form, as a drive that lists names decomposed shows it).
	for _, c := range kids {
		m, listed := made[norm.NFC.String(c.name)]
		if c.looked && c.stores.Answer == archindex.StoresNotArchive && c.stores.NoManifest && !claimed[c.name] && listed && madeFor(m, r) {
			it.outcome, it.folder, it.id = outUnfinished, c.name, c.id
			claimed[c.name] = true
			it.ask = setAsks(r)
			refuseDeleted(it, c.path)
			return
		}
	}
	// A child that cannot be read now may be its archive under another name (F-07).
	if unread {
		it.outcome, it.why = outNotMade, notMadeUnread
		return
	}
	name := used.free(r.name, takenNow)
	if name == "" {
		it.outcome, it.why = outNotMade, notMadeNoNumber
		return
	}
	used.add(name)
	it.outcome, it.folder = outNew, name
	it.ask = setAsks(r)
	refuseDeleted(it, filepath.Join(parent, name))
}

// containsID reports whether ids holds id.
func containsID(ids []string, id string) bool {
	for _, s := range ids {
		if s == id {
			return true
		}
	}
	return false
}

// setJobFits reports whether the job file of the archive folder out, when there is one, has
// auto-discovery off and exactly the one input in (C4): no job file fits; a job file that is there
// but cannot be read fits too, with unreadable set (F-11).
func setJobFits(out, in string) (fits, unreadable bool) {
	p, err := job.PathFor(schedule.DefaultNameFor(out))
	if err != nil {
		return true, true
	}
	if _, err := os.Lstat(p); errors.Is(err, fs.ErrNotExist) {
		return true, false
	}
	j, err := job.Read(p)
	if err != nil {
		return true, true
	}
	return !j.Auto && len(j.Inputs) == 1 && state.PathSourceID(j.Inputs[0]) == state.PathSourceID(in), false
}

// refuseDeleted makes it not made when the folder at out holds a delete that did not finish (§7,
// app.RefuseUnfinishedDelete), keeping the delete's words.
func refuseDeleted(it *setItem, out string) {
	if err := app.RefuseUnfinishedDelete(out, ""); err != nil {
		it.outcome, it.why, it.delWhy, it.ask = outNotMade, notMadeDelete, err.Error(), false
	}
}

// setAsks reports whether a new archive of the mailbox of r asks about a full offline download (§8,
// the rule today's flow asks by: a Thunderbird IMAP folder a live account uses). A row saved once is
// never asked: no account downloads into a folder no account uses, and an older copy is not kept
// current.
func setAsks(r setRow) bool {
	if r.once {
		return false
	}
	fi, err := os.Stat(r.path)
	return err == nil && fi.IsDir() && thunderbird.IsImapStore(r.path) && !storeUnused(r.path)
}
