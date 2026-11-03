package archdelete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/util"
)

// planUnfinished plans Delete again of a folder deletions.log has an unfinished
// delete of (u), which is not reachable or no longer an archive (orig, the refusal
// it would otherwise get), by §5.4's rules:
//   - not reachable, with the log's `folder removed` line and the folder's parent
//     reachable: the folder is gone, and the re-run finishes the rest
//     (RerunFolderGone);
//   - not reachable without that line: "Reconnect the drive or folder first" — a
//     drive unplugged after a crash in step 6 must not make the keyring step forget
//     a vault still on that drive;
//   - there, holding no manifest or vault, without that line: what §4 can still
//     name is removed (RerunNoAnchor);
//   - there again after its `folder removed` line: a new folder, not this delete's;
//     orig stands;
//   - there, but not the folder the delete started on (another identity than its
//     started line records, or none recorded — a folder always has one): not this
//     delete's either; orig stands (planNoAnchor; del-s1b-A1-01: another drive under
//     the same letter, an empty mount point, a folder made again at the path).
//
// Not reachable without that line, but the folder confirmed gone and its parent reachable, Delete
// again asks whether the delete was finished elsewhere (CaseFinishedElsewhere; §15.8 question 2,
// the owner's ruling Q4, MA-1057); the answer "It was finished elsewhere" (deps.FinishedElsewhere)
// is the folder-gone re-run, its plan saying so (Elsewhere). Only the person's answer tells a folder
// removed elsewhere from a drive whose parent stays reachable unplugged (macOS's /Volumes, Linux's
// /media), so the question names that case too.
func planUnfinished(folder string, deps Deps, u *Unfinished, orig *Refusal) (*DeletePlan, error) {
	abs := cleanAbs(folder)
	_, lerr := os.Lstat(abs)
	switch {
	case lerr != nil:
		if !errors.Is(lerr, fs.ErrNotExist) || !dirReachable(filepath.Dir(abs)) {
			return nil, refuse(CaseUnreachable, msgUnreachable)
		}
		if !u.FolderRemoved {
			if otherDrive(filepath.Dir(abs), u) {
				// Another drive at the same letter or label (the slice-4 adversarial pass's LOW-2): plug
				// in the one the delete began on, never the question.
				return nil, refuse(CaseUnreachable, msgOtherDrive)
			}
			if !deps.FinishedElsewhere {
				return nil, refuse(CaseFinishedElsewhere, "%s", ElsewhereQuestion(u.Kind))
			}
			p, err := planFolderGone(abs, deps, u)
			if p != nil {
				p.Elsewhere = true
			}
			return p, err
		}
		return planFolderGone(abs, deps, u)
	case u.FolderRemoved || orig.Case != CaseNotArchive:
		return nil, orig
	}
	return planNoAnchor(abs, deps, u, orig)
}

// markerFinishes is the delete a folder's marker names, for a plan whose anchor is there and whose
// folder no line of this computer's log knows (§15.4, DM-8): planned from the marker (FromMarker),
// its first run and when it began as the marker says them (not known when it does not say: an empty
// or damaged marker), what its earlier runs removed not known. nil when the folder holds no regular
// marker (a name that is not a regular file is the lister's refusal).
func markerFinishes(folder string) *Unfinished {
	st, err := delmark.Present(folder)
	if err != nil || st != delmark.Regular {
		return nil
	}
	d := delmark.Read(folder)
	return &Unfinished{Folder: folder, Row: folder, Marker: st, MarkerOnly: true, MarkerDetails: d, FromMarker: true,
		FirstRun: d.Run, FirstStarted: d.BeganAt()}
}

// planMarkerNoAnchor is §5.4's no-anchor re-run planned from the folder's marker, when no line of
// this computer's log knows the delete (§15.4): the folder's identity is the one it has now (the
// marker is in it); its kind and an encrypted archive's record are this computer's record of the
// folder, looked up while it exists (§3.1 [NF-2]) — recorded here, it is encrypted and step 8 uses
// that record; not recorded here, its kind is not known, step 8 does not apply and no record is
// touched; and it removes only MailArchive's own names (DM-19, planNoAnchor). orig is the refusal
// a folder without the marker gets.
func planMarkerNoAnchor(folder string, deps Deps, orig *Refusal) (*DeletePlan, error) {
	abs := cleanAbs(folder)
	st, err := delmark.Present(abs)
	if err != nil {
		// The lookup failed at a folder that is there: whether a delete is under way is not known, so
		// not "no longer holds an archive" (the slice-4 adversarial pass's LOW-5; DM-23's words).
		if r := MarkerRefusal(nil, err, "it was not deleted"); r != nil {
			return nil, r
		}
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	if st == delmark.Absent {
		return nil, orig
	}
	id, ok := folderIdentity(abs)
	if !ok {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	u := &Unfinished{Folder: abs, Row: abs, Marker: st, MarkerOnly: true, FromMarker: true, Identity: id}
	if st == delmark.Regular {
		u.MarkerDetails = delmark.Read(abs)
		u.FirstRun, u.FirstStarted = u.MarkerDetails.Run, u.MarkerDetails.BeganAt()
	}
	if deps.Record != nil {
		ref, rerr := deps.Record(abs)
		if rerr != nil {
			return nil, refuse(CaseRecord, "This computer's record of encrypted archives cannot be read, so MailArchive cannot tell whether it must forget this archive's key: %s", util.StripControl(rerr.Error()))
		}
		if ref.Recorded {
			u.Kind, u.Record, u.RecordKnown = Encrypted, ref, true
		}
	}
	return planNoAnchor(abs, deps, u, orig)
}

// volumeKeptAcrossReplug: this system's volume number survives a drive unplugged and plugged in
// again — Windows' volume serial number does; macOS's and Linux's device number changes on a
// remount — so a parent on another volume than the started line records is another drive at the
// same place (the slice-4 adversarial pass's LOW-2). A var: a test turns it on elsewhere.
var volumeKeptAcrossReplug = runtime.GOOS == "windows"

// msgOtherDrive is Delete again's refusal when the folder is gone and its parent is on another
// drive than the one the delete began on (lead's words, owner-delegated 2026-11-02): this person
// began the delete, so "again" is right.
const msgOtherDrive = "This archive's folder is not there, and the drive it was on is not the one plugged in now. Plug in the drive that holds this archive, then click Delete again."

// appleDoubleOfGone are the names of litter (operating-system files) a plan from the marker alone
// removes (the slice-4 adversarial pass's LOW-1): an AppleDouble file `._X` only when X goes too (a
// name in remove, or the marker) or is not there. `._X` holds X's Finder tags, colour label and
// resource fork on exFAT, FAT and network drives, so it is X's while X is kept; and the folder's
// own (`.VolumeIcon.icns`, `desktop.ini`, `.DS_Store`, `Thumbs.db`, …) stays while the folder does.
// Step 7 removes every litter file with the folder once nothing else is left in it.
func appleDoubleOfGone(goos string, litter []string, ents []fs.DirEntry, remove []Item, marker string) []string {
	present, going := map[string]bool{}, map[string]bool{}
	for _, e := range ents {
		present[fold(goos, e.Name())] = true
	}
	for _, it := range remove {
		going[fold(goos, it.Rel)] = true
	}
	if marker != "" {
		going[fold(goos, marker)] = true
	}
	var out []string
	for _, n := range litter {
		if !strings.HasPrefix(n, "._") {
			continue
		}
		if x := fold(goos, n[2:]); going[x] || !present[x] {
			out = append(out, n)
		}
	}
	return out
}

// otherDrive reports a parent on another volume than the one u's started line records, where the
// volume number survives a replug (volumeKeptAcrossReplug). Delete refuses a drive's top folder
// (driveRoot), so the folder a delete began on was on its parent's volume then.
func otherDrive(parent string, u *Unfinished) bool {
	if !volumeKeptAcrossReplug || u.Identity == (Identity{}) {
		return false
	}
	v, ok := volumeOf(parent)
	return ok && v != u.Identity.Volume
}

func dirReachable(dir string) bool {
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// recordFrom sets an encrypted plan's record from the original started line
// [R3:NF-NF2-RERUN]: its ID and Spelling, never a new lookup; "not recorded" leaves
// none; a line that gives neither makes step 8 skip, keeping the row.
func recordFrom(p *DeletePlan, u *Unfinished) {
	if p.Kind != Encrypted {
		return
	}
	p.KeyForgottenBefore = u.KeyForgotten
	switch {
	case u.Record.Recorded:
		p.Record = u.Record
	case !u.RecordKnown:
		p.RecordUnknown = true
	}
}

// planFolderGone is §5.4's folder-gone re-run: steps 1 and 5 to 7 are skipped, the
// rest (the schedules this computer records, their settings files, the settings,
// the keyring, the row) run.
func planFolderGone(abs string, deps Deps, u *Unfinished) (*DeletePlan, error) {
	if deps.Settings.LaunchOut != "" && sameFolder(deps.Settings.LaunchOut, abs, deps.goos()) {
		return nil, refuse(CaseLaunchOut, msgLaunchOut)
	}
	if err := checkBackupFor(abs, deps); err != nil {
		return nil, err
	}
	// Identity is the folder's as the original started line recorded it: the deletion
	// record names the folder deleted, and this re-run's own started line carries it.
	p := &DeletePlan{Folder: abs, Name: filepath.Base(abs), Identity: u.Identity, Kind: u.Kind, Rerun: RerunFolderGone, Finishes: u}
	recordFrom(p, u)
	if deps.Records != nil {
		d, err := deps.Records.Discover(abs)
		if err != nil {
			return nil, err
		}
		p.Schedules = d
	}
	addCaptureBackupFor(p, abs, deps)
	return p, nil
}

// planNoAnchor is §5.4's re-run of a folder that is there but holds no manifest or
// vault: at its root, the names §4 can still name — the fixed root names, the
// state files and run leftovers, the root index page (a plain archive's), the
// reserved names (an encrypted one's) — and its litter; everything else is kept,
// and the delete is never complete (§5.4). The folder must be the one the delete
// started on (u.Identity); another one gets orig.
func planNoAnchor(abs string, deps Deps, u *Unfinished, orig *Refusal) (*DeletePlan, error) {
	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	if link, lerr := util.IsLinkLike(abs); lerr != nil || link || !fi.IsDir() {
		return nil, linkRefusal(abs)
	}
	if err := checkProtected(abs, deps.Protected); err != nil {
		return nil, err
	}
	if deps.Settings.LaunchOut != "" && sameFolder(deps.Settings.LaunchOut, abs, deps.goos()) {
		return nil, refuse(CaseLaunchOut, msgLaunchOut)
	}
	if deps.Busy != nil && deps.Busy(abs) {
		return nil, refuse(CaseBusy, msgBusy)
	}
	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	defer root.Close()
	if same, serr := rootIsFile(root, fi); serr != nil || !same {
		return nil, linkRefusal(abs)
	}
	id, err := rootIdentity(root)
	if err != nil {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	if id != u.Identity {
		return nil, orig
	}
	p := &DeletePlan{Folder: abs, Name: onDiskName(abs, fi), Identity: id, Kind: u.Kind, Rerun: RerunNoAnchor, Finishes: u}
	recordFrom(p, u)
	if err := checkBackupFor(abs, deps); err != nil {
		return nil, err
	}
	if deps.Discover != nil {
		d, derr := deps.Discover.Discover(abs)
		if derr != nil {
			return nil, derr
		}
		p.Schedules = d
	}
	addCaptureBackupFor(p, abs, deps)
	l := lister{root: root, folder: abs, goos: deps.goos(), plan: p}
	l.init()
	// A delete planned from the folder's marker, with no line of this computer's log knowing its start
	// (or this computer's run that finishes such a delete): only MailArchive's own names (DM-19). The
	// marker says only that someone wrote that name, so README.txt, index.html and the *.log files of
	// a folder of the person's own files are kept; a real delete's step 6 removed them before its
	// anchor, so nothing of the archive's is lost.
	ownLogs := map[string]bool{}
	for _, n := range p.Schedules.Names() {
		ownLogs[fold(l.goos, n+".log")] = true
		ownLogs[fold(l.goos, n+".log"+rotatedSuffix)] = true
		ownLogs[fold(l.goos, n+".stderr.log")] = true
	}
	fixed := map[string]bool{}
	for _, n := range plainFixedNames {
		fixed[fold(l.goos, n)] = true
	}
	for _, n := range p.Schedules.logNames() {
		fixed[fold(l.goos, n+".log")] = true
		fixed[fold(l.goos, n+".log"+rotatedSuffix)] = true
		fixed[fold(l.goos, n+".stderr.log")] = true
	}
	own := map[string]bool{}
	for _, n := range folderOwnNames {
		own[fold(l.goos, n)] = true
	}
	ents, err := l.readDir("")
	if err != nil {
		return nil, err
	}
	marker := deleteMarkerIn(root, ents)
	// This computer's log says the delete wrote (or found) its marker at step 6, and neither the
	// anchor nor the marker is here now: the marker goes only after the anchor, so step 6 had
	// finished, here or where another computer or user account finished the delete (the slice-4
	// adversarial pass's MED-1). What its earlier runs removed is then not known.
	if u.Marked && marker == "" {
		p.NoteGone = true
		u.RemovedKnown = false
	}
	// A plan from the marker alone keeps the operating system's files that belong to what it keeps
	// (the slice-4 adversarial pass's LOW-1): decided once every name is classed.
	var maybeLitter []string
	for _, e := range ents {
		name := e.Name()
		f := fold(l.goos, name)
		if rootCannotName(name) {
			p.Kept = append(p.Kept, name)
			continue
		}
		efi, err := l.lstat(name)
		if err != nil {
			return nil, l.ioRefusal(name, err)
		}
		link, lerr := l.linkLike(name, efi)
		if lerr != nil {
			return nil, l.ioRefusal(name, lerr)
		}
		var removable bool
		switch {
		case u.FromMarker:
			removable = strings.HasPrefix(f, stateFilePrefix) || isLeftover(name) || ownLogs[f]
		case p.Kind == Encrypted:
			removable = encRootFiles[name] || encRootTmp(name) || isLeftover(name)
		default:
			removable = fixed[f] || f == "index.html" || strings.HasPrefix(f, stateFilePrefix)
		}
		switch {
		case f == fold(l.goos, lockName):
			if link {
				return nil, linkRefusal(filepath.Join(abs, name))
			}
		case name == marker && link:
			return nil, linkRefusal(filepath.Join(abs, name)) // §15.3; the slice-1 pass's LOW-2
		case name == marker && l.fileLike(efi):
			// The delete's own note, never in the plan's lists (§15.2).
		case name == marker:
			return nil, markerPlanRefusal(abs, name, false) // a folder, a device or a pipe (§15.3, MA-1049)
		case link && removable:
			return nil, linkRefusal(filepath.Join(abs, name))
		case link:
			p.Kept = append(p.Kept, name)
		case util.IsLitter(name) && !efi.IsDir() && !(p.Kind != Encrypted && own[f]) && u.FromMarker:
			maybeLitter = append(maybeLitter, name)
		case util.IsLitter(name) && !efi.IsDir() && !(p.Kind != Encrypted && own[f]):
			p.Litter = append(p.Litter, name)
		case removable && l.fileLike(efi):
			k := Fixed
			if strings.HasPrefix(f, stateFilePrefix) {
				k = State
			}
			p.Remove = append(p.Remove, Item{Rel: name, Size: efi.Size(), Kind: k})
		default:
			p.Kept = append(p.Kept, name)
		}
	}
	p.Litter = append(p.Litter, appleDoubleOfGone(l.goos, maybeLitter, ents, p.Remove, marker)...)
	sort.Slice(p.Remove, func(i, j int) bool { return p.Remove[i].Rel < p.Remove[j].Rel })
	sort.Strings(p.Litter)
	sort.Strings(p.Kept)
	for _, it := range p.Remove {
		p.Files++
		p.Bytes += it.Size
	}
	p.Files += len(p.Litter)
	return p, nil
}
