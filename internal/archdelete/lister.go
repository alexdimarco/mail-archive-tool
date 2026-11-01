package archdelete

import (
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/seal"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/util"
	"mail-archive-tool/internal/vault"
)

// lister builds a plan's file list through an os.Root on the archive folder. Every
// relative name it handles is forward-slashed.
type lister struct {
	root   *os.Root
	folder string
	goos   string
	plan   *DeletePlan

	removed map[string]bool // fold(rel) of every listed Item
	anchors []Item          // removed last, in this order
	// passed are the folders a removed name passes through (fold(rel) → rel), the
	// root ("") included; unrecDirs those a §4.3 file passes through.
	passed    map[string]string
	unrecDirs map[string]string
	// litter holds, per folder, the litter files found there by a walk.
	litter map[string][]string
	// checked caches checkPath's verdict on a folder: true exists, false missing.
	checked map[string]bool
}

// lstat is Lstat of rel inside the root.
func (l *lister) lstat(rel string) (fs.FileInfo, error) {
	if rel == "" {
		rel = "."
	}
	return l.root.Lstat(filepath.FromSlash(rel))
}

// linkLike is the link rule of §4: a symbolic link, or on Windows a reparse point
// whose tag is a name surrogate (a junction, a mount point); a cloud placeholder or
// a deduplicated file is an ordinary file [NEW-reparse-onedrive].
func (l *lister) linkLike(rel string, fi fs.FileInfo) (bool, error) {
	if fi.Mode()&fs.ModeSymlink != 0 {
		return true, nil
	}
	if l.goos != "windows" || (!fi.IsDir() && fi.Mode()&fs.ModeIrregular == 0) {
		return false, nil
	}
	return util.IsLinkLike(filepath.Join(l.folder, filepath.FromSlash(rel)))
}

// fileLike says a listed entry is a file Delete may remove: a regular file, or on
// Windows an irregular one that is not a folder (a cloud placeholder,
// util.DescriptorReadable's rule).
func (l *lister) fileLike(fi fs.FileInfo) bool {
	return util.DescriptorReadable(l.goos, fi.Mode())
}

// checkPath walks rel's elements from the root with Lstat: a link at any element
// refuses the whole delete [C11]; ok is false when an element is missing (a named
// file already gone counts as removed).
func (l *lister) checkPath(rel string) (fi fs.FileInfo, ok bool, err error) {
	parts := strings.Split(rel, "/")
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/")
		key := fold(l.goos, dir)
		if exists, seen := l.checked[key]; seen {
			if !exists {
				return nil, false, nil
			}
			continue
		}
		dfi, derr := l.lstat(dir)
		if derr != nil {
			l.checked[key] = false
			if isNotExist(derr) {
				return nil, false, nil
			}
			return nil, false, refuse(CaseUnreachable, msgUnreachable)
		}
		if link, lerr := l.linkLike(dir, dfi); lerr != nil || link {
			return nil, false, linkRefusal(filepath.Join(l.folder, filepath.FromSlash(dir)))
		}
		if !dfi.IsDir() {
			l.checked[key] = false
			return nil, false, nil
		}
		l.checked[key] = true
	}
	fi, err = l.lstat(rel)
	if err != nil {
		if isNotExist(err) {
			return nil, false, nil
		}
		return nil, false, refuse(CaseUnreachable, msgUnreachable)
	}
	if link, lerr := l.linkLike(rel, fi); lerr != nil || link {
		return nil, false, linkRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)))
	}
	return fi, true, nil
}

// add lists rel for removal once.
func (l *lister) add(rel string, fi fs.FileInfo, k ItemKind) {
	key := fold(l.goos, rel)
	if l.removed[key] {
		return
	}
	l.removed[key] = true
	l.plan.Remove = append(l.plan.Remove, Item{Rel: rel, Size: fi.Size(), Kind: k})
	l.pass(rel, l.passed)
}

// pass marks every folder rel passes through, the root included.
func (l *lister) pass(rel string, into map[string]string) {
	into[""] = ""
	for d := path.Dir(rel); d != "." && d != "/"; d = path.Dir(d) {
		into[fold(l.goos, d)] = d
	}
}

func (l *lister) init() {
	l.removed, l.passed, l.unrecDirs = map[string]bool{}, map[string]string{"": ""}, map[string]string{}
	l.litter, l.checked = map[string][]string{}, map[string]bool{}
}

func (l *lister) readDir(rel string) ([]fs.DirEntry, error) {
	if rel == "" {
		rel = "."
	}
	ents, err := util.ReadDirIn(l.root, filepath.FromSlash(rel))
	if err != nil && !isNotExist(err) {
		return nil, refuse(CaseUnreachable, msgUnreachable)
	}
	return ents, nil
}

func join(dir, name string) string {
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

// plain is §4.1's list, §4.3's unrecorded copies and §4.4's litter for a plain
// archive.
func (l *lister) plain() error {
	l.init()
	m, facts, err := readManifest(l.root)
	if err != nil {
		return refuse(CaseNotArchive, msgNotArchive)
	}
	p := l.plan
	p.Anchor = facts
	p.Stores = tokenSet(m, l.goos)
	p.Captured = capturePlain(l.root, m)
	tokenByFold := map[string]string{}
	for _, t := range p.Stores {
		tokenByFold[fold(l.goos, t)] = t
	}

	// Records [C11]: a recorded Path or AlsoFiles entry counts only in the shape of
	// §4.1, and only its stem's three files are removed — never a bare Path.
	recordDirs := map[string]string{}
	keys := make([]string, 0, m.Len())
	all := m.All()
	for k := range all {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r := all[k]
		for _, rp := range append([]string{r.Path}, r.AlsoFiles...) {
			parts, ok := util.ValidRecordedPath(rp)
			if !ok || len(parts) < 2 || !strings.HasSuffix(rp, ".html") {
				continue
			}
			if _, isStore := tokenByFold[fold(l.goos, parts[0])]; !isStore {
				continue
			}
			l.pass(rp, recordDirs)
			stem := strings.TrimSuffix(rp, ".html")
			for _, suffix := range []string{".html", "-attachments.zip", ".eml"} {
				fi, ok, err := l.checkPath(stem + suffix)
				if err != nil {
					return err
				}
				if ok && l.fileLike(fi) {
					l.add(stem+suffix, fi, Message)
				}
			}
		}
	}
	for k, v := range recordDirs {
		l.passed[k] = v
	}

	// Index pages: only in folders a record passes through, never by walking a tree.
	dirs := sortedValues(recordDirs)
	for _, d := range dirs {
		// Only a folder checkPath found to be a real folder (no link on its way).
		if d == "" || !l.checked[fold(l.goos, d)] {
			continue
		}
		ents, err := l.readDir(d)
		if err != nil {
			return err
		}
		for _, e := range ents {
			if !indexPageRe.MatchString(e.Name()) {
				continue
			}
			rel := join(d, e.Name())
			fi, ok, err := l.checkPath(rel)
			if err != nil {
				return err
			}
			if ok && l.fileLike(fi) {
				l.add(rel, fi, IndexPage)
			}
		}
	}

	// The root: fixed names, state files, the root index page, _outlook-pst's .pst
	// files, litter; everything else is the person's and is named once, unwalked.
	fixed := map[string]bool{}
	for _, n := range plainFixedNames {
		fixed[fold(l.goos, n)] = true
	}
	for _, n := range p.Schedules.logNames() {
		fixed[fold(l.goos, n+".log")] = true
		fixed[fold(l.goos, n+".stderr.log")] = true
	}
	ents, err := l.readDir("")
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		f := fold(l.goos, name)
		if f == fold(l.goos, manifestName) || f == fold(l.goos, lockName) {
			continue
		}
		fi, err := l.lstat(name)
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(name, fi)
		if lerr != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		// A store folder (or a link in its place) is the store loop's; a file that
		// only shares a token's name is judged as any root file is.
		if _, isStore := tokenByFold[f]; isStore && (link || fi.IsDir()) {
			continue
		}
		removable := fixed[f] || f == "index.html" || strings.HasPrefix(f, stateFilePrefix) || f == fold(l.goos, outlookPSTDir)
		switch {
		case link && removable:
			return linkRefusal(filepath.Join(l.folder, name))
		case link:
			p.Kept = append(p.Kept, name)
		case util.IsLitter(name) && !fi.IsDir():
			l.litter[""] = append(l.litter[""], name)
		case f == fold(l.goos, outlookPSTDir) && fi.IsDir():
			if err := l.outlookPST(name); err != nil {
				return err
			}
		case removable && l.fileLike(fi):
			k := Fixed
			if strings.HasPrefix(f, stateFilePrefix) {
				k = State
			} else if f == "index.html" {
				k = IndexPage
			}
			l.add(name, fi, k)
		default:
			p.Kept = append(p.Kept, name)
		}
	}

	// The store trees: run leftovers at any depth, §4.3's unrecorded copies, litter,
	// and another archive's marker (which refuses the whole delete).
	for _, t := range p.Stores {
		fi, err := l.lstat(t)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return refuse(CaseUnreachable, msgUnreachable)
		}
		if link, lerr := l.linkLike(t, fi); lerr != nil || link {
			return linkRefusal(filepath.Join(l.folder, t))
		}
		if !fi.IsDir() {
			continue
		}
		if err := l.walkStore(t); err != nil {
			return err
		}
	}
	l.anchors = []Item{{Rel: manifestName, Size: facts.Size, Kind: Anchor}}
	return nil
}

// outlookPST lists -outlook's export folder: its .pst files only (§4.1).
func (l *lister) outlookPST(dir string) error {
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		rel := join(dir, e.Name())
		fi, err := l.lstat(rel)
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(rel, fi)
		isPST := strings.EqualFold(path.Ext(e.Name()), ".pst")
		switch {
		case lerr != nil:
			return refuse(CaseUnreachable, msgUnreachable)
		case link && isPST:
			return linkRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)))
		case !link && isPST && l.fileLike(fi):
			l.add(rel, fi, Fixed)
		case !link && util.IsLitter(e.Name()) && !fi.IsDir():
			l.litter[dir] = append(l.litter[dir], e.Name())
		default:
			l.plan.Kept = append(l.plan.Kept, rel)
		}
	}
	return nil
}

// walkStore walks one store tree. A link whose name Delete would remove refuses
// the delete; any other link is not followed and is kept, as links among files
// Delete does not touch are ignored.
func (l *lister) walkStore(dir string) error {
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		rel := join(dir, name)
		fi, err := l.lstat(rel)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(rel, fi)
		if lerr != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		if link {
			if isLeftover(name) || mailShaped(name) || indexPageRe.MatchString(name) || isMarker(l.goos, name) || l.removed[fold(l.goos, rel)] {
				return linkRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)))
			}
			l.plan.Kept = append(l.plan.Kept, rel)
			continue
		}
		if fi.IsDir() {
			// A litter folder (a trash, a Spotlight index) is never cleared or walked: it
			// is kept, and keeps its parent (§4.4).
			if util.IsLitter(name) {
				l.plan.Kept = append(l.plan.Kept, rel)
				continue
			}
			if err := l.walkStore(rel); err != nil {
				return err
			}
			continue
		}
		if isMarker(l.goos, name) {
			return refuse(CaseMixed, msgMixed)
		}
		switch {
		case l.removed[fold(l.goos, rel)]:
		case isLeftover(name) && l.fileLike(fi):
			l.add(rel, fi, Leftover)
		case util.IsLitter(name):
			l.litter[dir] = append(l.litter[dir], name)
		case mailShaped(name) && l.fileLike(fi):
			l.plan.Unrecorded = append(l.plan.Unrecorded, Item{Rel: rel, Size: fi.Size()})
			l.pass(rel, l.unrecDirs)
		default:
			l.plan.Kept = append(l.plan.Kept, rel)
		}
	}
	return nil
}

func isMarker(goos, name string) bool {
	for _, m := range otherArchiveMarkers {
		if eqName(goos, name, m) {
			return true
		}
	}
	return false
}

// encrypted is §4.2's list: the fixed root names, units/, chunks/ and inventory/ in
// their validated layouts, program/ in the program copies' layout, the vault last.
// Any other name at the root is a mixed folder (encmark's case Other).
func (l *lister) encrypted(deps Deps) error {
	l.init()
	p := l.plan
	ents, err := l.readDir("")
	if err != nil {
		return err
	}
	var vaultItems []Item
	for _, e := range ents {
		name := e.Name()
		fi, err := l.lstat(name)
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(name, fi)
		if lerr != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		isDirName := name == container.InventoryDir || name == container.ProgramDir || name == unitsDirs[0] || name == unitsDirs[1]
		switch {
		case util.IsLitter(name) && !link && !fi.IsDir():
			l.litter[""] = append(l.litter[""], name)
		case util.IsLitter(name) && !link:
			p.Kept = append(p.Kept, name) // a litter folder is kept, never cleared (§4.4)
		case name == lockName:
		case link && (isDirName || encRootFiles[name] || name == vault.FileName || name == vault.BackupName || encRootTmp.MatchString(name)):
			return linkRefusal(filepath.Join(l.folder, name))
		case link:
			return refuse(CaseMixed, msgMixed)
		case (name == vault.FileName || name == vault.BackupName) && l.fileLike(fi):
			vaultItems = append(vaultItems, Item{Rel: name, Size: fi.Size(), Kind: Anchor})
		case (encRootFiles[name] || encRootTmp.MatchString(name)) && l.fileLike(fi):
			l.add(name, fi, State)
		case fi.IsDir() && (name == unitsDirs[0] || name == unitsDirs[1]):
			if err := l.walkSharded(name); err != nil {
				return err
			}
		case fi.IsDir() && name == container.InventoryDir:
			if err := l.walkFlat(name, func(n string) bool { return invShardRe.MatchString(n) || unitTmp.MatchString(n) }); err != nil {
				return err
			}
		case fi.IsDir() && name == container.ProgramDir:
			if err := l.walkFlat(name, func(n string) bool { return container.ProgramNameOK(n) || programTmp.MatchString(n) }); err != nil {
				return err
			}
		default:
			return refuse(CaseMixed, msgMixed)
		}
	}
	// The vault last: its mirror, then the key file itself.
	sort.Slice(vaultItems, func(i, j int) bool {
		return vaultItems[i].Rel == vault.BackupName && vaultItems[j].Rel != vault.BackupName
	})
	for _, it := range vaultItems {
		l.removed[fold(l.goos, it.Rel)] = true
	}
	l.anchors = vaultItems
	for _, n := range []string{vault.FileName, vault.BackupName, container.PointerName, container.EnvelopeName} {
		if facts, err := hashFileIn(l.root, n); err == nil {
			p.Anchor = facts
			break
		}
	}
	p.Captured = Captured{LastBackup: lastBackup(l.root)}
	if deps.Sealed != nil {
		if c, ok := deps.Sealed(l.folder); ok {
			c.Known, c.KeyOpen = true, true
			c.LastBackup = p.Captured.LastBackup
			p.Captured = c
		}
	}
	if deps.Record != nil {
		ref, err := deps.Record(l.folder)
		if err != nil {
			return refuse(CaseUnreachable, "This computer's record of encrypted archives cannot be read, so MailArchive cannot tell whether it must forget this archive's key: %s", util.StripControl(err.Error()))
		}
		p.Record = ref
	}
	return nil
}

// walkSharded lists units/ or chunks/: <hh>/<32 hex starting with hh> files and the
// sweepable temps in a shard; anything else is kept.
func (l *lister) walkSharded(dir string) error {
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		rel := join(dir, e.Name())
		fi, err := l.lstat(rel)
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(rel, fi)
		switch {
		case lerr != nil:
			return refuse(CaseUnreachable, msgUnreachable)
		case link && shardRe.MatchString(e.Name()):
			return linkRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)))
		case !link && fi.IsDir() && shardRe.MatchString(e.Name()):
			shard := e.Name()
			if err := l.walkFlat(rel, func(n string) bool {
				return (unitRe.MatchString(n) && strings.HasPrefix(n, shard)) || unitTmp.MatchString(n)
			}); err != nil {
				return err
			}
		case !link && util.IsLitter(e.Name()) && !fi.IsDir():
			l.litter[dir] = append(l.litter[dir], e.Name())
		default:
			l.plan.Kept = append(l.plan.Kept, rel)
		}
	}
	return nil
}

// walkFlat lists the files of one folder that ok accepts; anything else is kept.
func (l *lister) walkFlat(dir string, ok func(string) bool) error {
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		rel := join(dir, e.Name())
		fi, err := l.lstat(rel)
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		link, lerr := l.linkLike(rel, fi)
		switch {
		case lerr != nil:
			return refuse(CaseUnreachable, msgUnreachable)
		case link && ok(e.Name()):
			return linkRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)))
		case !link && ok(e.Name()) && l.fileLike(fi):
			l.add(rel, fi, Sealed)
		case !link && util.IsLitter(e.Name()) && !fi.IsDir():
			l.litter[dir] = append(l.litter[dir], e.Name())
		default:
			l.plan.Kept = append(l.plan.Kept, rel)
		}
	}
	return nil
}

// finish places the litter (§4.4: removed from folders a removed name passes
// through; from folders only a §4.3 file passes through when the box is ticked;
// kept elsewhere), the anchor last, the folders deepest first and the counts.
func (l *lister) finish() {
	p := l.plan
	for dir, names := range l.litter {
		for _, n := range names {
			rel := join(dir, n)
			switch _, inPassed := l.passed[fold(l.goos, dir)]; {
			case inPassed:
				p.Litter = append(p.Litter, rel)
			default:
				if _, inUnrec := l.unrecDirs[fold(l.goos, dir)]; inUnrec {
					p.UnrecordedLitter = append(p.UnrecordedLitter, rel)
				} else {
					p.Kept = append(p.Kept, rel)
				}
			}
		}
	}
	sort.Slice(p.Remove, func(i, j int) bool { return p.Remove[i].Rel < p.Remove[j].Rel })
	p.Remove = append(p.Remove, l.anchors...)
	sort.Strings(p.Litter)
	sort.Strings(p.UnrecordedLitter)
	sort.Strings(p.Kept)
	sort.Slice(p.Unrecorded, func(i, j int) bool { return p.Unrecorded[i].Rel < p.Unrecorded[j].Rel })
	folders := map[string]string{}
	for k, v := range l.passed {
		folders[k] = v
	}
	for k, v := range l.unrecDirs {
		folders[k] = v
	}
	for _, v := range folders {
		if v != "" {
			p.Folders = append(p.Folders, v)
		}
	}
	sort.Slice(p.Folders, func(i, j int) bool {
		di, dj := strings.Count(p.Folders[i], "/"), strings.Count(p.Folders[j], "/")
		if di != dj {
			return di > dj
		}
		return p.Folders[i] < p.Folders[j]
	})
	p.Files, p.Bytes = 0, 0
	for _, it := range p.Remove {
		p.Files++
		p.Bytes += it.Size
	}
	for _, rel := range p.Litter {
		p.Files++
		if fi, err := l.lstat(rel); err == nil {
			p.Bytes += fi.Size()
		}
	}
	if _, err := l.lstat(seal.Name); err == nil {
		p.Paused = true
	}
}

func sortedValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// tokenSet is §4.1's store token set at every manifest version: the manifest's
// Stores values together with the first segments of the records' Paths that pass
// util.ValidRecordedPath and state.SafeToken [R4:NEW-R4-stores-gate], in byte
// order, one spelling per name the file system tells apart.
func tokenSet(m *state.Manifest, goos string) []string {
	set := map[string]string{}
	add := func(t string) {
		if !state.SafeToken(t) || strings.HasPrefix(t, ".") {
			return
		}
		k := fold(goos, t)
		if cur, ok := set[k]; !ok || t < cur {
			set[k] = t
		}
	}
	for _, t := range m.Stores {
		add(t)
	}
	for _, r := range m.All() {
		for _, rp := range append([]string{r.Path}, r.AlsoFiles...) {
			if parts, ok := util.ValidRecordedPath(rp); ok && len(parts) > 1 {
				add(parts[0])
			}
		}
	}
	return sortedValues(set)
}
