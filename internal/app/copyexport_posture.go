package app

import (
	"sort"
	"strings"
)

// The hand-over posture (design §4.7 step 3, RR2-C6; ruling J3's mechanism as
// amended by RR-2, owner rulings §13.5). A hand-over copy inherits its destination's
// access control, so the preview names who will be able to read it before the first
// write, computed over what a NEW CHILD of the destination inherits (an inherit-only
// ACE is not effective on the destination itself, and an inheritable grant to one
// named account reads "Private" through a check of the destination's own access),
// recomputed at Start, and checked again on the realized root and marker before the
// first message. It is never "only you can read this folder" when it cannot tell.

// CopyPosture is the posture line and what it was computed from.
type CopyPosture struct {
	Kind      string   // "private", "readers", "unknown" or "unenforceable"
	Readers   []string // the principals other than the user, SYSTEM and Administrators that can read (Kind "readers")
	Predicted bool     // a share computes inheritance on the server: the line is a prediction
	Line      string
}

// Posture kinds.
const (
	postureKindPrivate       = "private"
	postureKindReaders       = "readers"
	postureKindUnknown       = "unknown"
	postureKindUnenforceable = "unenforceable"
)

// newPosture words a posture from its kind and readers.
func newPosture(kind string, readers []string) CopyPosture {
	p := CopyPosture{Kind: kind, Readers: sortedUnique(readers)}
	switch kind {
	case postureKindPrivate:
		p.Line = "only you can read this folder"
	case postureKindReaders:
		p.Line = joinNames(p.Readers) + " can read what you export here"
	case postureKindUnenforceable:
		p.Line = "permissions can't be restricted on this drive"
	default:
		p.Kind = postureKindUnknown
		p.Line = "couldn't check who can read this folder"
	}
	return p
}

func sortedUnique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

// joinNames is "A", "A and B" or "A, B and C".
func joinNames(n []string) string {
	switch len(n) {
	case 0:
		return ""
	case 1:
		return n[0]
	}
	return strings.Join(n[:len(n)-1], ", ") + " and " + n[len(n)-1]
}

// broaderThan reports whether p lets more readers in than then did: a reader then
// did not name, or a line that cannot tell where it could before (a posture that
// cannot be checked is never taken for "only you").
func (p CopyPosture) broaderThan(then CopyPosture) bool {
	switch p.Kind {
	case postureKindUnknown:
		return then.Kind != postureKindUnknown
	case postureKindReaders:
		named := map[string]bool{}
		for _, r := range then.Readers {
			named[r] = true
		}
		for _, r := range p.Readers {
			if !named[r] {
				return true
			}
		}
	case postureKindPrivate:
		return false
	case postureKindUnenforceable:
		return then.Kind == postureKindPrivate
	}
	return false
}

// ceACE is one access-control entry of a Windows discretionary ACL, reduced to what
// the posture needs.
type ceACE struct {
	Allow bool
	SID   string // the principal's SID, in string form
	Who   string // the principal as the line names it ("BUILTIN\Users")
	Read  bool   // the mask grants read access (FILE_READ_DATA, GENERIC_READ, GENERIC_ALL)

	// Inheritance flags: OBJECT_INHERIT (files), CONTAINER_INHERIT (directories),
	// INHERIT_ONLY (not effective here) and NO_PROPAGATE_INHERIT.
	OI, CI, IO, NP bool
}

// Well-known SIDs the line never names: the user themself, SYSTEM and
// Administrators (and CREATOR OWNER, which a new child gives to its creator).
const (
	sidSystem        = "S-1-5-18"
	sidAdministrator = "S-1-5-32-544"
	sidCreatorOwner  = "S-1-3-0"
	sidCreatorGroup  = "S-1-3-1"
)

// inheritedACEs is the DACL a new child receives from a parent's, by the Windows
// inheritance algorithm (what CreatePrivateObjectSecurityEx computes): a new
// directory takes an entry marked CONTAINER_INHERIT, a new file one marked
// OBJECT_INHERIT, an INHERIT_ONLY entry included, because inherit-only means "not
// effective on the parent", not "not inherited". The entries come back effective
// (INHERIT_ONLY cleared); a directory also keeps the inheritable copies of those it
// passes on.
func inheritedACEs(parent []ceACE, forDir bool) []ceACE {
	var out []ceACE
	for _, a := range parent {
		if forDir && a.CI || !forDir && a.OI {
			c := a
			c.IO = false
			if forDir && a.NP {
				c.OI, c.CI, c.NP = false, false, false
			}
			if !forDir {
				c.OI, c.CI, c.NP = false, false, false
			}
			out = append(out, c)
		}
	}
	return out
}

// effectiveACEs are the entries of an actual descriptor that apply to the object
// itself (INHERIT_ONLY ones do not).
func effectiveACEs(dacl []ceACE) []ceACE {
	var out []ceACE
	for _, a := range dacl {
		if !a.IO {
			out = append(out, a)
		}
	}
	return out
}

// readersOf names every principal other than me, SYSTEM, Administrators and the
// creator placeholders that the entries (in evaluation order) let read: a deny
// before an allow takes that principal's read away.
func readersOf(aces []ceACE, me string) []string {
	denied := map[string]bool{}
	var out []string
	for _, a := range aces {
		if a.SID == me || a.SID == sidSystem || a.SID == sidAdministrator || a.SID == sidCreatorOwner {
			continue
		}
		switch {
		case !a.Allow && a.Read:
			denied[a.SID] = true
		case a.Allow && a.Read && !denied[a.SID]:
			who := a.Who
			if who == "" {
				who = a.SID
			}
			out = append(out, who)
		}
	}
	return sortedUnique(out)
}

// windowsPosture is the hand-over posture of a destination whose DACL is parent,
// for the signed-in user me: the readers of the root (a new directory) and of a
// file below it. An unreadable descriptor is the unknown posture.
func windowsPosture(parent []ceACE, me string, readable bool) CopyPosture {
	if !readable {
		return newPosture(postureKindUnknown, nil)
	}
	readers := append(readersOf(inheritedACEs(parent, true), me), readersOf(inheritedACEs(parent, false), me)...)
	if len(readers) == 0 {
		return newPosture(postureKindPrivate, nil)
	}
	return newPosture(postureKindReaders, readers)
}

// windowsRealized is the posture of an object that already exists: the entries
// that apply to it, not what a child would inherit.
func windowsRealized(dacl []ceACE, me string, readable bool) CopyPosture {
	if !readable {
		return newPosture(postureKindUnknown, nil)
	}
	if readers := readersOf(effectiveACEs(dacl), me); len(readers) > 0 {
		return newPosture(postureKindReaders, readers)
	}
	return newPosture(postureKindPrivate, nil)
}

// ceMacACE is one entry of a macOS access-control list, reduced the same way.
type ceMacACE struct {
	Allow bool
	Who   string // "everyone", "group:staff", "user:bob"
	Read  bool   // read or list
	// file_inherit, directory_inherit, limit_inherit and only_inherit
	FileInherit, DirInherit, Limit, Only bool
}

// macInherited is what a new child receives from its parent's ACL on macOS: a new
// directory the entries marked directory_inherit, a new file those marked
// file_inherit.
func macInherited(parent []ceMacACE, forDir bool) []ceMacACE {
	var out []ceMacACE
	for _, a := range parent {
		if forDir && a.DirInherit || !forDir && a.FileInherit {
			a.Only = false
			out = append(out, a)
		}
	}
	return out
}

// macReaders names who the entries let read, except the user themself.
func macReaders(aces []ceMacACE, me string) []string {
	denied := map[string]bool{}
	var out []string
	for _, a := range aces {
		if a.Who == me {
			continue
		}
		switch {
		case !a.Allow && a.Read:
			denied[a.Who] = true
		case a.Allow && a.Read && !denied[a.Who]:
			out = append(out, a.Who)
		}
	}
	return sortedUnique(out)
}

// macPosture is the hand-over posture of a destination with ACL entries parent.
func macPosture(parent []ceMacACE, me string, readable bool) CopyPosture {
	if !readable {
		return newPosture(postureKindUnknown, nil)
	}
	readers := append(macReaders(macInherited(parent, true), me), macReaders(macInherited(parent, false), me)...)
	if len(readers) == 0 {
		return newPosture(postureKindPrivate, nil)
	}
	return newPosture(postureKindReaders, readers)
}

// macRealized is the posture of an existing object's own entries.
func macRealized(own []ceMacACE, me string, readable bool) CopyPosture {
	if !readable {
		return newPosture(postureKindUnknown, nil)
	}
	var eff []ceMacACE
	for _, a := range own {
		if !a.Only {
			eff = append(eff, a)
		}
	}
	if readers := macReaders(eff, me); len(readers) > 0 {
		return newPosture(postureKindReaders, readers)
	}
	return newPosture(postureKindPrivate, nil)
}

// unenforceableFS reports whether the filesystem type cannot restrict access by
// mode: FAT, exFAT, and the network filesystems whose server decides (design §4.7).
func unenforceableFS(fsType string) (unenforceable, share bool) {
	switch strings.ToLower(fsType) {
	case "vfat", "msdos", "msdosfs", "exfat", "fat", "fat32":
		return true, false
	case "smbfs", "smb", "smb2", "cifs", "afpfs", "webdav":
		return true, true
	}
	return false, false
}

// destinationPosture is the posture line for destination dir: the filesystem's
// type decides first (a drive that cannot restrict access says so), then the
// operating system's reading of what a new child inherits.
func (e *CopyExportEnv) destinationPosture(dir string, fsType string) CopyPosture {
	if e.posture != nil {
		return e.posture(dir)
	}
	if un, share := unenforceableFS(fsType); un {
		p := newPosture(postureKindUnenforceable, nil)
		p.Predicted = share
		return p
	}
	return osInheritedPosture(dir)
}

// realizedPosture is the posture of an object the run created, read from the
// object itself.
func (e *CopyExportEnv) realizedPosture(path string, isDir bool, fsType string) CopyPosture {
	if e.realized != nil {
		return e.realized(path, isDir)
	}
	if un, _ := unenforceableFS(fsType); un {
		return newPosture(postureKindUnenforceable, nil)
	}
	return osRealizedPosture(path, isDir)
}
