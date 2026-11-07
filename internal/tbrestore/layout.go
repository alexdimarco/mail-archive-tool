package tbrestore

import (
	"io/fs"
	"path/filepath"
	"strings"
)

// FolderIn is one folder the archive side asks the restore to create (internal/app's
// RestoreFolder, slice R1): its path below the new top folder as the archive spells
// it, parents before children, every parent present.
type FolderIn struct {
	Names    []string
	Messages int
	Bytes    int64
}

// Folder is one folder of the layout, every name final.
type Folder struct {
	// Names are the archive's own names; Parts the file names written, one per level.
	Names []string
	Parts []string
	// Parent is the index of the folder it sits in, -1 for one directly in the top
	// folder; HasChildren says it gets a NAME.sbd folder.
	Parent      int
	HasChildren bool
	Messages    int
	Bytes       int64
	// LongPath: on Windows the folder (or one above it) would not fit the path budget
	// even shortened, so it is not restored (W-LONGPATH, §4.5).
	LongPath bool
	// Renamed: its own name (the last part) is not the archive's spelling.
	Renamed bool
}

// File is the folder's mbox file, relative to the container ("A.sbd/B"); Dir its
// NAME.sbd folder.
func (f Folder) File() string { return strings.Join(sbdJoin(f.Parts), "/") }
func (f Folder) Dir() string  { return f.File() + ".sbd" }

// sbdJoin is the parts with every one but the last as its ".sbd" folder.
func sbdJoin(parts []string) []string {
	out := make([]string, len(parts))
	for i, p := range parts {
		if i < len(parts)-1 {
			out[i] = p + ".sbd"
		} else {
			out[i] = p
		}
	}
	return out
}

// Layout is everything the restore will create, decided in the plan (§4.4 note): the
// top name without its " (n)" (the free one is chosen against Local Folders), and the
// folders.
type Layout struct {
	Top     string
	Folders []Folder
	// Changed is how many folder names had to change from the archive's own: the top's
	// NAME when it changed, and each restored folder whose own name changed, once each
	// (ChangedNames lists them).
	Changed int
	// TopAsAsked is the top name as it would be with NAME as given ("Restored — " + it),
	// kept so the plan can say what it became.
	TopAsAsked string
	topChanged bool
}

// NameChange is one changed name, as the plan lists it: the archive's spelling (a folder's
// path below the top, parts joined with "/") and the name written.
type NameChange struct{ Old, New string }

// ChangedNames are the changed names in the plan's order (settled 2026-11-07, the lead's
// call after ADV-N1): the top first when its NAME changed (New is the layout's top, before
// any " (n)"; the plan puts in the free name it chose), then each restored folder whose
// own name changed, parents first. len is Changed.
func (l *Layout) ChangedNames() []NameChange {
	var out []NameChange
	if l.topChanged {
		out = append(out, NameChange{l.TopAsAsked, l.Top})
	}
	for _, i := range l.Restored() {
		if f := l.Folders[i]; f.Renamed {
			out = append(out, NameChange{strings.Join(f.Names, "/"), strings.Join(f.Parts, "/")})
		}
	}
	return out
}

// count sets Changed from the top and the folders' Renamed (once each, restored only).
func (l *Layout) count() { l.Changed = len(l.ChangedNames()) }

// Restored are the folders that will be written (not LongPath).
func (l *Layout) Restored() []int {
	var out []int
	for i, f := range l.Folders {
		if !f.LongPath {
			out = append(out, i)
		}
	}
	return out
}

// PlanLayout makes the layout for the archive folder's name and its folders. On
// Windows (goos "windows") every path is held to the budget of §4.5 under localDir:
// shortened where over, else the folder is not restored (W-LONGPATH); every other
// platform has no such limit at these lengths, but the rule is the same code, run with
// goos "windows" by the tests on every leg (MA-1126).
func PlanLayout(archiveName string, in []FolderIn, localDir, goos string) (*Layout, error) {
	top, topChanged := topName(archiveName)
	// The top folder's own name is a changed name too (MA-1095 lab).
	l := &Layout{Top: top, TopAsAsked: TopPrefix + archiveName, topChanged: topChanged}
	index := map[string]int{}
	children := map[int][]int{} // parent index (-1 top) -> child indexes, archive order
	for _, f := range in {
		if len(f.Names) == 0 {
			continue
		}
		key := strings.Join(f.Names, "\x00")
		if _, dup := index[key]; dup {
			continue
		}
		parent := -1
		if len(f.Names) > 1 {
			p, ok := index[strings.Join(f.Names[:len(f.Names)-1], "\x00")]
			if !ok {
				continue // its parent is not in the list: the archive side always lists parents
			}
			parent = p
			l.Folders[p].HasChildren = true
		}
		index[key] = len(l.Folders)
		children[parent] = append(children[parent], len(l.Folders))
		l.Folders = append(l.Folders, Folder{Names: append([]string(nil), f.Names...), Parent: parent, Messages: f.Messages, Bytes: f.Bytes})
	}
	// Names, level by level, siblings together (steps 1–6).
	for _, kids := range children {
		orig := make([]string, len(kids))
		for i, k := range kids {
			orig[i] = l.Folders[k].Names[len(l.Folders[k].Names)-1]
		}
		names, changed := FinalNames(orig)
		for i, k := range kids {
			l.Folders[k].Parts = []string{names[i]}
			l.Folders[k].Renamed = changed[i]
		}
	}
	l.rebuildParts()
	if goos == "windows" {
		if err := l.windowsBudget(localDir, children); err != nil {
			return nil, err
		}
	}
	l.count()
	if err := l.ThunderbirdReadsAll(withNumber(l.Top, 99)); err != nil {
		return nil, err
	}
	return l, nil
}

// ThunderbirdReadsAll is the defence in depth of the MA-1095 lab fix (settled
// 2026-11-07): every name the restore would write — top, the top's " (n)" form, and each
// restored folder's every part — is one Thunderbird reads as written (ThunderbirdReads).
// The name steps already make every name so; if one ever did not, the restore is refused
// in words before anything is written, never left to say "restored" over a folder
// Thunderbird shows empty.
func (l *Layout) ThunderbirdReadsAll(top string) error {
	names := []string{l.Top, top}
	for _, i := range l.Restored() {
		names = append(names, l.Folders[i].Parts...)
	}
	for _, n := range names {
		if !ThunderbirdReads(n) {
			return refuse("W-TBNAME", wTBName, nil)
		}
	}
	return nil
}

// rebuildParts sets each folder's Parts to its ancestors' last parts and its own.
func (l *Layout) rebuildParts() {
	own := make([]string, len(l.Folders))
	for i, f := range l.Folders {
		own[i] = f.Parts[len(f.Parts)-1]
	}
	for i := range l.Folders {
		var parts []string
		for j := i; j >= 0; j = l.Folders[j].Parent {
			parts = append([]string{own[j]}, parts...)
		}
		l.Folders[i].Parts = parts
	}
}

// The Windows path budget (§4.5 note): counted in UTF-16 units from the resolved Local
// Folders path, each entry under its final name (no working names, foldcheck M5): a
// file's path plus Thunderbird's ".msf" beside it at most 259, a folder's path at most
// 247 (CreateDirectoryW's 248 with its terminator). The marker, the made-list and the
// parent file are counted as files.
const (
	winFileMax = 259
	winDirMax  = 247
	msfLen     = 4
	shortRunes = 30
)

func winFileOK(p string) bool { return utf16Len(p)+msfLen <= winFileMax }
func winDirOK(p string) bool  { return utf16Len(p) <= winDirMax }

// windowsBudget holds every path to the budget, with the " (99)" the free top name
// may need: the container's own entries must fit, else nothing is restored; a folder
// over budget has the names on its branch shortened, deepest first (each cut to 30
// characters plus the hash suffix of its original); a folder still over is LongPath,
// with every folder below it.
func (l *Layout) windowsBudget(localDir string, children map[int][]int) error {
	top := withNumber(l.Top, 99)
	base := strings.TrimRight(filepath.Clean(localDir), `\/`) + `\`
	container := base + top + ".sbd"
	if !winFileOK(base+top) || !winDirOK(container) || !winFileOK(container+`\`+MadeListName) || !winFileOK(container+`\`+MarkerName) {
		for i := range l.Folders {
			l.Folders[i].LongPath = true
		}
		return refuse("W-LONGPATH", fill(wLongPathAll, "PATH", localDir), nil)
	}
	pathOf := func(i int, dir bool) string {
		p := container + `\` + strings.ReplaceAll(l.Folders[i].File(), "/", `\`)
		if dir {
			p += ".sbd"
		}
		return p
	}
	ok := func(i int) bool {
		if !winFileOK(pathOf(i, false)) {
			return false
		}
		return !l.Folders[i].HasChildren || winDirOK(pathOf(i, true))
	}
	shortened := map[int]bool{}
	for i := range l.Folders {
		for j := i; !ok(i) && j >= 0; j = l.Folders[j].Parent {
			if shortened[j] {
				continue
			}
			f := &l.Folders[j]
			own := f.Parts[len(f.Parts)-1]
			// Counted in UTF-16 units (the same as characters but for those outside the
			// BMP), so a cut name is always within Thunderbird's 55 (MA-1095 lab).
			if utf16Len(own) <= shortRunes+hashSuffixLen {
				continue
			}
			f.Parts[len(f.Parts)-1] = cutUnits(own, shortRunes) + hashOf(f.Names[len(f.Names)-1])
			shortened[j] = true
			l.rebuildParts()
		}
	}
	// A shortened name may now spell the same as a sibling: dedupe again, then check.
	for _, kids := range children {
		names := make([]string, len(kids))
		orig := make([]string, len(kids))
		ch := make([]bool, len(kids))
		for i, k := range kids {
			names[i] = l.Folders[k].Parts[len(l.Folders[k].Parts)-1]
			orig[i] = l.Folders[k].Names[len(l.Folders[k].Names)-1]
		}
		dedupe(names, orig, ch)
		for i, k := range kids {
			l.Folders[k].Parts[len(l.Folders[k].Parts)-1] = names[i]
			if ch[i] {
				l.Folders[k].Renamed = true
			}
		}
	}
	l.rebuildParts()
	for i := range l.Folders {
		if l.Folders[i].Parent >= 0 && l.Folders[l.Folders[i].Parent].LongPath {
			l.Folders[i].LongPath = true // parents come first
			continue
		}
		if !ok(i) {
			l.Folders[i].LongPath = true
		}
	}
	for j := range shortened {
		l.Folders[j].Renamed = true // counted once, and only when restored (count)
	}
	return nil
}

// FreeTopName is the first of "Restored — NAME", "Restored — NAME (2)" … " (99)" for
// which neither the name nor its ".sbd" is in Local Folders, compared after NFC and
// case-blind (§3.5): entries are what the folder holds now. Past 99: W-NAMES. Settled
// 2026-11-07 [PCT-1]: compared by the restore's siblingKey, so a name Thunderbird's own
// compare calls the same as an entry there is never chosen.
func FreeTopName(top string, entries []fs.DirEntry) (string, int, error) {
	have := map[string]bool{}
	for _, e := range entries {
		have[siblingKey(e.Name())] = true
	}
	for n := 1; n <= 99; n++ {
		c := withNumber(top, n)
		if !have[siblingKey(c)] && !have[siblingKey(c+".sbd")] {
			return c, n, nil
		}
	}
	return "", 0, refuse("W-NAMES", fill(wNames, "NAME", strings.TrimPrefix(top, TopPrefix)), nil)
}
