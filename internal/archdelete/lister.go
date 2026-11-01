package archdelete

import (
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/encmark"
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
	// checked caches checkPath's verdict on a folder, keyed by the folder's exact
	// spelling — never folded, so on a case-sensitive volume of a case-insensitive
	// OS (a case-sensitive APFS drive, a Windows folder with per-folder case
	// sensitivity) "store/sub" is checked for links on its own even after
	// "store/Sub" was found to be a real folder (del-s1a-A1-06): true a real folder,
	// false missing or not a folder.
	checked map[string]bool
}

// ioRefusal is the refusal for an error, other than "not there", reading rel in
// the archive folder: unreachable when the folder itself is gone now (a drive
// unplugged during the plan), else that name's own cause (no permission, a sharing
// violation), never "reconnect" for a folder that is plainly there
// (del-s1a-A2-06).
func (l *lister) ioRefusal(rel string, err error) error {
	if _, ferr := os.Lstat(l.folder); ferr != nil {
		return refuse(CaseUnreachable, msgUnreachable)
	}
	return unreadableRefusal(filepath.Join(l.folder, filepath.FromSlash(rel)), err)
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
	for _, part := range parts {
		if rootCannotName(part) {
			return nil, false, nil // a device name: never a file the exporter wrote
		}
	}
	for i := 1; i < len(parts); i++ {
		dir := strings.Join(parts[:i], "/")
		if isDir, seen := l.checked[dir]; seen {
			if !isDir {
				return nil, false, nil
			}
			continue
		}
		dfi, derr := l.lstat(dir)
		if derr != nil {
			l.checked[dir] = false
			if isNotExist(derr) {
				return nil, false, nil
			}
			return nil, false, l.ioRefusal(dir, derr)
		}
		if link, lerr := l.linkLike(dir, dfi); lerr != nil || link {
			return nil, false, linkRefusal(filepath.Join(l.folder, filepath.FromSlash(dir)))
		}
		if !dfi.IsDir() {
			l.checked[dir] = false
			return nil, false, nil
		}
		l.checked[dir] = true
	}
	fi, err = l.lstat(rel)
	if err != nil {
		if isNotExist(err) {
			return nil, false, nil
		}
		return nil, false, l.ioRefusal(rel, err)
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
		return nil, l.ioRefusal(rel, err)
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
		return manifestRefusal(l.folder, err)
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
	// Only a folder checkPath found to be a real folder (no link on its way) is one a
	// record passes through: an element of a planted record that is a regular file
	// ("taxes.pdf/2024-…html", "Mailbox/notes.txt/…") is the person's file, never a
	// folder Delete may remove (del-s1a-A2-01: os.Remove deletes a file too).
	for k, v := range recordDirs {
		if l.checked[v] {
			l.passed[k] = v
		} else {
			delete(recordDirs, k)
		}
	}

	// Index pages: only in folders a record passes through, never by walking a tree.
	dirs := sortedValues(recordDirs)
	for _, d := range dirs {
		if d == "" {
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
		// Each log runlog.Open writes (<name>.log) has its rotated generation (.1);
		// the scheduler's stderr log is not rotated (del-s1a-A2-04).
		fixed[fold(l.goos, n+".log")] = true
		fixed[fold(l.goos, n+".log"+rotatedSuffix)] = true
		fixed[fold(l.goos, n+".stderr.log")] = true
	}
	folderOwn := map[string]bool{}
	for _, n := range folderOwnNames {
		folderOwn[fold(l.goos, n)] = true
	}
	// The store folders by identity as well as by name: a listing can spell a store
	// folder otherwise than its token (an HFS+ drive's decomposed form, a Windows 8.3
	// alias, a case-insensitive volume on Linux), and the store loop reaches it by
	// the token, so it must not also be judged here as the person's (del-s1a-A1-10).
	var storeInfos []fs.FileInfo
	for _, t := range p.Stores {
		if rootCannotName(t) {
			continue
		}
		if sfi, err := l.lstat(t); err == nil {
			storeInfos = append(storeInfos, sfi)
		}
	}
	isStoreFolder := func(f string, fi fs.FileInfo) bool {
		if _, ok := tokenByFold[f]; ok {
			return true
		}
		for _, sfi := range storeInfos {
			if os.SameFile(fi, sfi) {
				return true
			}
		}
		return false
	}
	ents, err := l.readDir("")
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		f := fold(l.goos, name)
		if f == fold(l.goos, manifestName) {
			continue
		}
		if rootCannotName(name) {
			p.Kept = append(p.Kept, name) // a device name: the person's (del-s1a-A1-01)
			continue
		}
		fi, err := l.lstat(name)
		if err != nil {
			return l.ioRefusal(name, err)
		}
		link, lerr := l.linkLike(name, fi)
		if lerr != nil {
			return l.ioRefusal(name, lerr)
		}
		if f == fold(l.goos, lockName) {
			// Step 7 removes this name after unlocking, and step 1 takes the lock on
			// it: a link there refuses here, not at step 1 (del-s1a-A1-11).
			if link {
				return linkRefusal(filepath.Join(l.folder, name))
			}
			continue
		}
		// A store folder (or a link in its place) is the store loop's; a file that
		// only shares a token's name is judged as any root file is.
		if (link || fi.IsDir()) && isStoreFolder(f, fi) {
			continue
		}
		removable := fixed[f] || f == "index.html" || strings.HasPrefix(f, stateFilePrefix)
		switch {
		case link && (removable || f == fold(l.goos, outlookPSTDir)):
			return linkRefusal(filepath.Join(l.folder, name))
		case link:
			p.Kept = append(p.Kept, name)
		case folderOwn[f]:
			p.Kept = append(p.Kept, name) // the folder's own, not litter (del-s1a-A1-04)
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
		if rootCannotName(t) {
			continue
		}
		fi, err := l.lstat(t)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return l.ioRefusal(t, err)
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
		if rootCannotName(e.Name()) {
			l.plan.Kept = append(l.plan.Kept, rel)
			continue
		}
		fi, err := l.lstat(rel)
		if err != nil {
			return l.ioRefusal(rel, err)
		}
		link, lerr := l.linkLike(rel, fi)
		isPST := strings.EqualFold(path.Ext(e.Name()), ".pst")
		switch {
		case lerr != nil:
			return l.ioRefusal(rel, lerr)
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

// walkStore walks one store tree. A link whose name Delete would remove (a listed
// name, a run leftover) or that stands where another archive's marker would refuses
// the delete; any other link is not followed and is kept, as links among files
// Delete does not touch are ignored (§4) — a link only shaped like mail or an index
// page is never removed, not even with §4.3's box ticked, so it does not block the
// delete (del-s1a-A1-09). A folder that holds an encrypted archive by encmark.Here
// (the vault's markers: its key files, sealed-record files, or its units/, chunks/,
// inventory/ layouts; or this computer's record of that very folder) is another
// archive: mixed (del-s1a-A1-08).
func (l *lister) walkStore(dir string) error {
	if enc, _, err := encmark.Here(filepath.Join(l.folder, filepath.FromSlash(dir))); enc {
		return refuse(CaseMixed, msgMixed)
	} else if err != nil {
		return l.ioRefusal(dir, err)
	}
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		name := e.Name()
		rel := join(dir, name)
		if rootCannotName(name) {
			l.plan.Kept = append(l.plan.Kept, rel) // a device name: the person's (del-s1a-A1-01)
			continue
		}
		fi, err := l.lstat(rel)
		if err != nil {
			if isNotExist(err) {
				continue
			}
			return l.ioRefusal(rel, err)
		}
		link, lerr := l.linkLike(rel, fi)
		if lerr != nil {
			return l.ioRefusal(rel, lerr)
		}
		if link {
			if isLeftover(name) || isMarker(l.goos, name) || l.removed[fold(l.goos, rel)] {
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
		if rootCannotName(name) {
			return refuse(CaseMixed, msgMixed) // a name an encrypted archive never holds (del-s1a-A1-01)
		}
		fi, err := l.lstat(name)
		if err != nil {
			return l.ioRefusal(name, err)
		}
		link, lerr := l.linkLike(name, fi)
		if lerr != nil {
			return l.ioRefusal(name, lerr)
		}
		isDirName := name == container.InventoryDir || name == container.ProgramDir || name == unitsDirs[0] || name == unitsDirs[1]
		switch {
		case util.IsLitter(name) && !link && !fi.IsDir():
			l.litter[""] = append(l.litter[""], name)
		case util.IsLitter(name) && !link:
			p.Kept = append(p.Kept, name) // a litter folder is kept, never cleared (§4.4)
		case name == lockName && link:
			return linkRefusal(filepath.Join(l.folder, name)) // step 7 removes it (del-s1a-A1-11)
		case name == lockName:
		case link && (isDirName || encRootFiles[name] || name == vault.FileName || name == vault.BackupName || encRootTmp(name)):
			return linkRefusal(filepath.Join(l.folder, name))
		case link:
			return refuse(CaseMixed, msgMixed)
		case (name == vault.FileName || name == vault.BackupName) && l.fileLike(fi):
			vaultItems = append(vaultItems, Item{Rel: name, Size: fi.Size(), Kind: Anchor})
		case (encRootFiles[name] || encRootTmp(name)) && l.fileLike(fi):
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
	if len(l.anchors) == 0 {
		// No vault file: the archive is still recognised by its sealed record
		// (vault.EncryptedMarker), so the first of its files present is the anchor,
		// removed last, and the record names it as what it is (del-s1a-A2-07).
		for _, n := range encAnchorFallback {
			if i := l.removedIndex(n); i >= 0 {
				l.anchors = []Item{l.plan.Remove[i]}
				l.anchors[0].Kind = Anchor
				l.plan.Remove = append(l.plan.Remove[:i], l.plan.Remove[i+1:]...)
				break
			}
		}
	}
	if len(l.anchors) > 0 {
		if facts, err := hashFileIn(l.root, l.anchors[len(l.anchors)-1].Rel); err == nil {
			p.Anchor = facts
		}
	}
	p.ArchiveID = archiveIDOf(l.root)
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
			// Not in §7 (conformance 4, for the operator): without the record the plan
			// cannot say whether step 8 must forget this archive's key.
			return refuse(CaseRecord, "This computer's record of encrypted archives cannot be read, so MailArchive cannot tell whether it must forget this archive's key: %s", util.StripControl(err.Error()))
		}
		p.Record = ref
	}
	return nil
}

// encAnchorFallback are an encrypted archive's files, in the order one is taken as
// the anchor when no vault file is there.
var encAnchorFallback = []string{container.PointerName, container.MirrorName, container.EnvelopeName, container.InventoryName}

// removedIndex is the index in the plan's Remove of the root name n, or -1.
func (l *lister) removedIndex(n string) int {
	for i, it := range l.plan.Remove {
		if it.Rel == n {
			return i
		}
	}
	return -1
}

// archiveIDOf is the archive id an encrypted archive's vault file (either copy)
// records, read without the key (vault.Parse), or else its manifest envelope's; ""
// when neither says.
func archiveIDOf(root *os.Root) string {
	for _, n := range []string{vault.FileName, vault.BackupName} {
		if data, err := readSmallIn(root, n, vaultCap); err == nil {
			if v, err := vault.Parse(data); err == nil {
				return v.ArchiveID.String()
			}
		}
	}
	if data, err := readSmallIn(root, container.EnvelopeName, vaultCap); err == nil {
		var env vault.ManifestEnvelope
		if json.Unmarshal(data, &env) == nil && validArchiveID(env.ArchiveID) {
			return env.ArchiveID
		}
	}
	return ""
}

// vaultCap bounds the read of a vault file or envelope for its id.
const vaultCap = 1 << 20

// validArchiveID is an archive id's shape: 32 lower-case hex digits.
func validArchiveID(s string) bool { return len(s) == 32 && unitRe.MatchString(s) }

// walkSharded lists units/ or chunks/: <hh>/<32 hex starting with hh> files and the
// sweepable temps in a shard; anything else is kept.
func (l *lister) walkSharded(dir string) error {
	ents, err := l.readDir(dir)
	if err != nil {
		return err
	}
	for _, e := range ents {
		rel := join(dir, e.Name())
		if rootCannotName(e.Name()) {
			l.plan.Kept = append(l.plan.Kept, rel)
			continue
		}
		fi, err := l.lstat(rel)
		if err != nil {
			return l.ioRefusal(rel, err)
		}
		link, lerr := l.linkLike(rel, fi)
		switch {
		case lerr != nil:
			return l.ioRefusal(rel, lerr)
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
		if rootCannotName(e.Name()) {
			l.plan.Kept = append(l.plan.Kept, rel)
			continue
		}
		fi, err := l.lstat(rel)
		if err != nil {
			return l.ioRefusal(rel, err)
		}
		link, lerr := l.linkLike(rel, fi)
		switch {
		case lerr != nil:
			return l.ioRefusal(rel, lerr)
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
	// The root state files that keep an archive as it was (its history, schedule
	// records and pause seal) go after every other name, then the anchor
	// (del-s1a-A2-05).
	lateRank := map[string]int{}
	for i, n := range lateStateFiles {
		lateRank[fold(l.goos, n)] = i + 1
	}
	sort.SliceStable(p.Remove, func(i, j int) bool {
		return lateRank[fold(l.goos, p.Remove[i].Rel)] < lateRank[fold(l.goos, p.Remove[j].Rel)]
	})
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
