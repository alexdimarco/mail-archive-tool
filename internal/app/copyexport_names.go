package app

import (
	"fmt"
	"hash/fnv"
	"strings"
	"unicode/utf16"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/util"
)

// The namer decides every path of an export before any file is written (design
// §4.9): folder segments through the export-only naming pass (util.ExportSegment,
// ruling Q15), sibling fold-key de-duplication, month directories of at most 1,000
// files (XC12), and the message stem by the archive's own scheme with the subject
// slug shrinking under the path budget. The preview runs it with no filesystem to
// name the first target paths and the longest; the run runs it again against the
// real root, and the same messages in the same order always get the same names.

// ceDir is one directory of the export tree as the namer tracks it.
type ceDir struct {
	rel    string             // path relative to the export root, forward slashes
	used   map[string]string  // fold key of a child name → its owner
	kids   map[string]*ceDir  // child folder by Graph folder id
	months map[string]*cePart // the month directory in use, by "2006-01"
}

// cePart is where a month's messages are going: part 1 is "2026-03", part 2
// "2026-03~2", and so on, each holding at most copyFilesPerDir files.
type cePart struct {
	idx  int
	name string
}

type ceNamer struct {
	preserve bool
	rootAbs  string              // the absolute path of the export root, for the path budget
	root     *ceDir              // "messages"
	stems    map[uint64]struct{} // 64-bit digests of (directory, stem fold key): a stem already placed in a month directory. A digest keeps the map small for 200,000 messages, and a collision of two digests only costs the later message a longer hash in its name.
	counts   map[string]int      // files placed per month directory
}

func newCeNamer(preserve bool, rootAbs string) *ceNamer {
	n := &ceNamer{preserve: preserve, rootAbs: rootAbs, stems: map[uint64]struct{}{}, counts: map[string]int{}}
	n.root = n.newDir("messages")
	return n
}

func (n *ceNamer) newDir(rel string) *ceDir {
	return &ceDir{rel: rel, used: map[string]string{}, kids: map[string]*ceDir{}, months: map[string]*cePart{}}
}

// cePlace is where one message goes.
type cePlace struct {
	Dir  string // the directory, relative to the export root, forward slashes
	Stem string // the file stem; the original is Stem + ".eml"
}

// Path is the original's path relative to the export root.
func (p cePlace) Path() string { return p.Dir + "/" + p.Stem + ".eml" }

// folderDir finds or makes the directory for m's folder path. Siblings whose names
// have one fold key (case, NFC/NFD, the upcase-table equivalents) are told apart
// by a "~" and a hash of the Graph folder id, as are 8.3 short-name aliases: names
// never silently merge (XC13, MA-418).
func (n *ceNamer) folderDir(m *CopyMessage) *ceDir {
	d := n.root
	if !n.preserve {
		return d
	}
	for _, ref := range m.Folder {
		if k := d.kids[ref.ID]; k != nil {
			d = k
			continue
		}
		base := util.ExportSegment(ref.Name)
		name := base
		if util.IsShortNameAlias(name) {
			name = util.JoinSuffix(base, "~"+util.HashHex(ref.ID, 8), util.ExportSegmentMaxBytes)
		}
		for _, hn := range []int{8, 12, 16, 24, 40} {
			if _, taken := d.used[util.FoldKey(name)]; !taken {
				break
			}
			name = util.JoinSuffix(base, "~"+util.HashHex(ref.ID, hn), util.ExportSegmentMaxBytes)
		}
		d.used[util.FoldKey(name)] = "folder:" + ref.ID
		k := n.newDir(d.rel + "/" + name)
		d.kids[ref.ID] = k
		d = k
	}
	return d
}

// monthDir is the month directory messages go into, starting a new part when the
// current one would pass copyFilesPerDir files.
func (n *ceNamer) monthDir(d *ceDir, m *CopyMessage, files int) string {
	month := "0000-00"
	if !m.Received.IsZero() {
		month = m.Received.UTC().Format("2006-01")
	}
	p := d.months[month]
	if p == nil {
		p = &cePart{}
		d.months[month] = p
	}
	if p.name != "" && n.counts[d.rel+"/"+p.name]+files <= copyFilesPerDir {
		return p.name
	}
	for {
		p.idx++
		name := month
		if p.idx > 1 {
			name = fmt.Sprintf("%s~%d", month, p.idx)
		}
		key := util.FoldKey(name)
		if _, taken := d.used[key]; taken {
			continue // a sibling folder already has that name
		}
		d.used[key] = "month:" + name
		p.name = name
		return name
	}
}

// place names where m goes. files is how many files the message will put in its
// directory (the original, plus a readable copy when that slice lands), so the
// 1,000-file bound counts files, not messages.
func (n *ceNamer) place(m *CopyMessage, files int) cePlace {
	d := n.folderDir(m)
	month := n.monthDir(d, m, files)
	dir := d.rel + "/" + month
	key := dir + "\x00"
	stem := n.stem(m, dir, key)
	n.stems[stemDigest(key, stem)] = struct{}{}
	n.counts[dir] += files
	return cePlace{Dir: dir, Stem: stem}
}

// stem picks the stem inside dir: the archive's scheme (export.StemName) with the
// slug shrinking 60 → 24 → 0 until the absolute path is within the budget, then
// the digest lengthening 8 → 12 → 16 → 24 → 40 while another message's stem has
// the same fold key in the directory (deterministic from this message alone).
func (n *ceNamer) stem(m *CopyMessage, dir, key string) string {
	slug := 60
	for {
		cand := export.StemName(m.Received, subjectSlug(m.Subject, slug), m.SourceID, 8)
		// The name must also fit the 255-byte NAME_MAX of ext4, xfs, btrfs, tmpfs and
		// NFS, with the longest digest the de-duplication below can use: the slug is
		// bounded in runes, and a letter can take four bytes (EW-AO1).
		widest := export.StemName(m.Received, subjectSlug(m.Subject, slug), m.SourceID, 40)
		if slug == 0 || (utf16Len(n.rootAbs+"/"+dir+"/"+cand+".eml") <= copyPathBudget && len(widest)+len(".eml") <= copyNameMaxBytes) {
			break
		}
		if slug == 60 {
			slug = 24
		} else {
			slug = 0
		}
	}
	var stem string
	for _, hn := range []int{8, 12, 16, 24, 40} {
		stem = export.StemName(m.Received, subjectSlug(m.Subject, slug), m.SourceID, hn)
		if _, taken := n.stems[stemDigest(key, stem)]; !taken {
			return stem
		}
	}
	return stem
}

// ceUniqueName is the casefold-unique form of name among the names a directory
// already holds (used maps fold keys to nothing): the archive's own uniqueName
// (internal/export/attach.go) is case-sensitive, which is fine for zip entries and
// wrong on disk. A name whose fold key is taken becomes "report (2).pdf", then
// "report (3).pdf"; a name matching the 8.3 short-name alias pattern gets a "~" and a
// hash first (XC13). It is the helper the attachment writer (slice E6) uses.
func ceUniqueName(used map[string]struct{}, name string) string {
	if util.IsShortNameAlias(name) {
		stem, ext := splitExt(name)
		name = util.JoinSuffix(stem, "~"+util.HashHex(name, 8), util.ExportSegmentMaxBytes-len(ext)) + ext
	}
	cand := name
	for i := 2; ; i++ {
		if _, taken := used[util.FoldKey(cand)]; !taken {
			used[util.FoldKey(cand)] = struct{}{}
			return cand
		}
		stem, ext := splitExt(name)
		cand = util.JoinSuffix(stem, fmt.Sprintf(" (%d)", i), util.ExportSegmentMaxBytes-len(ext)) + ext
	}
}

// splitExt splits name at its last dot, "" for a name with no extension.
func splitExt(name string) (stem, ext string) {
	if i := strings.LastIndexByte(name, '.'); i > 0 {
		return name[:i], name[i:]
	}
	return name, ""
}

// stemDigest is the 64-bit FNV-1a digest of a directory key and a stem's fold key.
func stemDigest(dirKey, stem string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(dirKey))
	h.Write([]byte(util.FoldKey(stem)))
	return h.Sum64()
}

// subjectSlug is the subject slug bounded to n runes, or "" for n == 0.
func subjectSlug(subject string, n int) string {
	if n <= 0 {
		return ""
	}
	return util.Slug(subject, n)
}

// utf16Len is a path's length in UTF-16 units, Windows' unit for its 260 limit.
func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// sumsPath is a path as SHA256SUMS.txt writes it: relative to the root, forward
// slashes.
func sumsPath(rel string) string { return strings.ReplaceAll(rel, "\\", "/") }
