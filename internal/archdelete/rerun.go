package archdelete

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
//     orig stands.
func planUnfinished(folder string, deps Deps, u *Unfinished, orig *Refusal) (*DeletePlan, error) {
	abs := cleanAbs(folder)
	_, lerr := os.Lstat(abs)
	switch {
	case lerr != nil:
		if !errors.Is(lerr, fs.ErrNotExist) || !u.FolderRemoved || !dirReachable(filepath.Dir(abs)) {
			return nil, refuse(CaseUnreachable, msgUnreachable)
		}
		return planFolderGone(abs, deps, u)
	case u.FolderRemoved || orig.Case != CaseNotArchive:
		return nil, orig
	}
	return planNoAnchor(abs, deps, u)
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
	p := &DeletePlan{Folder: abs, Name: filepath.Base(abs), Kind: u.Kind, Rerun: RerunFolderGone}
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
// and the delete is never complete while the folder holds files it can no longer
// identify.
func planNoAnchor(abs string, deps Deps, u *Unfinished) (*DeletePlan, error) {
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
	p := &DeletePlan{Folder: abs, Name: onDiskName(abs, fi), Identity: id, Kind: u.Kind, Rerun: RerunNoAnchor}
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
		if p.Kind == Encrypted {
			removable = encRootFiles[name] || encRootTmp(name) || isLeftover(name)
		} else {
			removable = fixed[f] || f == "index.html" || strings.HasPrefix(f, stateFilePrefix)
		}
		switch {
		case f == fold(l.goos, lockName):
			if link {
				return nil, linkRefusal(filepath.Join(abs, name))
			}
		case link && removable:
			return nil, linkRefusal(filepath.Join(abs, name))
		case link:
			p.Kept = append(p.Kept, name)
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
