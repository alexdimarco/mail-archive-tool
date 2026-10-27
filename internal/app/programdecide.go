package app

import (
	"fmt"
	"strings"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/progcache"
)

// Deciding what an encrypted archive's program folder should hold (program design
// §4.2, R7, R8, R15, R16; PG3): a pure function of what the record sealed, what was
// found on the drive, this program, and what the cache holds verified.

// programReads is the record-format revision this program opens (C4): bumped whenever
// an older program could no longer open what this one seals.
const programReads = 1

// judged is what was found at a sealed name.
type judged int

const (
	jAbsent     judged = iota // nothing there
	jOK                       // a regular file with the sealed bytes
	jChanged                  // a regular file whose bytes are not the sealed ones
	jUnchecked                // an online-only placeholder: present, never opened (R4)
	jUnreadable               // could not be read this run: never grounds for a rewrite or a removal (R4)
	jBlocked                  // a folder, a link or a special file stands at the name (R8)
)

// present says a judged copy counts as there.
func (j judged) present() bool { return j == jOK || j == jUnchecked || j == jUnreadable }

// programView is decide's input.
type programView struct {
	set          container.ProgramSet // the landed set
	state        map[string]judged    // by sealed name
	keep         bool
	version      string // this program's version
	release      bool   // this program is a release build (C12)
	platform     string // this computer's release platform, or ""
	selfPlatform string // this computer's <os>-<arch>
	selfName     string // the name this computer's self-copy takes
	cacheVersion string // the verified cached release copies are written from, or ""
	cacheAssets  map[string]progcache.Asset
	cacheSums    progcache.Asset // the cached version's SHA256SUMS
	now          string          // RFC 3339, for unchecked_since
}

// programAdd is a file the step writes.
type programAdd struct {
	file  container.ProgramFile
	asset string // the cached asset it is copied from; "" for the checksum file or a self-copy
	sums  bool
	self  bool
	why   string // why a self-copy is written (a self-copy trigger), for the warning
}

// programPlan is decide's output.
type programPlan struct {
	add      []programAdd
	remove   []string                // sealed names whose files go
	keep     []container.ProgramFile // entries kept as they are (counters updated)
	warnings []string
	blocked  []string // platforms a folder, link or special file blocks (R8)
	changed  []string // platforms whose kept copy was changed: removed or replaced, and told (P3-8, B3)
}

// selfReleasePlatform is the release platform a self-copy's <os>-<arch> stands in for.
func selfReleasePlatform(selfPlatform string) string {
	goos, goarch, _ := strings.Cut(selfPlatform, "-")
	if goos == "darwin" {
		return "macos"
	}
	return container.ReleasePlatform(goos, goarch)
}

// decide plans the program folder (§4.2): with keep off, every sealed file goes; else
// per platform a release copy from the verified cached version when it is at or above
// what is kept (a repair at the same version), older copies kept where the newer
// release has none, and a self-copy for this computer's platform when no copy there
// opens this archive. A development build writes no release copy and removes none.
func decide(v programView) programPlan {
	var p programPlan
	if !v.keep {
		for _, f := range v.set.Files {
			switch v.state[f.Name] {
			case jBlocked:
				p.blocked = append(p.blocked, f.Platform)
				p.keep = append(p.keep, f)
			case jAbsent:
			default:
				p.remove = append(p.remove, f.Name)
			}
		}
		return p
	}
	byPlatform := map[string]container.ProgramFile{}
	var selfs, sums []container.ProgramFile
	for _, f := range v.set.Files {
		switch {
		case f.Origin == container.OriginSelf:
			selfs = append(selfs, f)
		case f.Platform == container.PlatformChecksums:
			sums = append(sums, f)
		default:
			byPlatform[f.Platform] = f
		}
	}
	// opens records, per release platform, whether a copy that opens this archive will
	// be there after the plan.
	opens := map[string]bool{}
	versions := map[string]bool{} // the release versions whose copies stay or arrive
	for _, plat := range container.ReleasePlatforms {
		e, has := byPlatform[plat]
		st := v.state[e.Name]
		var want progcache.Asset
		if v.release && v.cacheVersion != "" {
			want = v.cacheAssets[progcache.AssetFor(plat)]
		}
		repair := has && v.cacheVersion == e.Version && (st == jAbsent || st == jChanged)
		switch {
		case has && st == jBlocked:
			p.blocked = append(p.blocked, plat)
			p.keep = append(p.keep, e)
			versions[e.Version] = true
		case want.SHA256 != "" && (!has || container.CompareVersions(v.cacheVersion, e.Version) > 0 || repair):
			f := container.ProgramFile{Name: progcache.AssetFor(plat), SHA256: want.SHA256, Size: want.Size, Origin: container.OriginRelease,
				Version: v.cacheVersion, Platform: plat, Reads: programReads}
			p.add = append(p.add, programAdd{file: f, asset: f.Name})
			opens[plat] = true
			versions[v.cacheVersion] = true
			if has && st == jChanged {
				p.changed = append(p.changed, plat)
				p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s in this archive was changed; it was replaced with the release's own", plat))
			}
		case has && st == jUnreadable:
			// Never grounds for a rewrite or a removal (R4), but never silent: it is told on
			// every run and counts toward the lasting "incomplete" line (SWAP-01, CRIT-05).
			e.MissingRuns++
			p.keep = append(p.keep, e)
			p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s in this archive cannot be read; it was left as it is", plat))
			opens[plat] = e.Reads >= programReads
			versions[e.Version] = true
		case has && st.present():
			p.keep = append(p.keep, v.checked(e, st))
			opens[plat] = e.Reads >= programReads
			versions[e.Version] = true
		case has && st == jChanged:
			// Its bytes are not the release's and the cache cannot write them again: the
			// file goes, its entry stays marked missing.
			p.remove = append(p.remove, e.Name)
			e.MissingRuns++
			p.keep = append(p.keep, e)
			p.changed = append(p.changed, plat)
			p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s in this archive was changed; it was removed", plat))
			versions[e.Version] = true
		case has:
			e.MissingRuns++
			p.keep = append(p.keep, e)
			p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s is missing from this archive", plat))
			versions[e.Version] = true
		}
	}
	// Each kept version's own checksum file (R16): written with its copies, removed when
	// none of that version remains.
	for _, s := range sums {
		ver := strings.TrimPrefix(s.Name, "SHA256SUMS-")
		st := v.state[s.Name]
		switch {
		case !versions[ver] && st != jBlocked:
			if st != jAbsent {
				p.remove = append(p.remove, s.Name)
			}
		case st == jBlocked || st.present():
			p.keep = append(p.keep, s)
			delete(versions, ver) // present: nothing to write
		default:
			if st == jChanged {
				p.remove = append(p.remove, s.Name)
			}
			if ver != v.cacheVersion {
				p.keep = append(p.keep, s) // cannot be written again: kept, marked missing
				delete(versions, ver)
			}
		}
	}
	if versions[v.cacheVersion] && v.cacheVersion != "" && v.cacheSums.SHA256 != "" {
		name := "SHA256SUMS-" + v.cacheVersion
		p.add = append(p.add, programAdd{sums: true, file: container.ProgramFile{Name: name, SHA256: v.cacheSums.SHA256, Size: v.cacheSums.Size,
			Origin: container.OriginRelease, Version: v.cacheVersion, Platform: container.PlatformChecksums, Reads: programReads}})
	}
	// Self-copies (§4.2, C5, R15).
	thisOpens := v.platform != "" && opens[v.platform]
	var why string
	if !thisOpens {
		why = "no copy for this computer's system"
		if e, has := byPlatform[v.platform]; has && e.Reads < programReads {
			why = "the kept copies cannot open this archive"
			p.warnings = append(p.warnings, "the copies of MailArchive kept in this archive cannot open it; this computer's own program is kept beside them")
		}
	}
	ownWritten := false
	for _, s := range selfs {
		st := v.state[s.Name]
		rp := selfReleasePlatform(s.Platform)
		switch {
		case st == jBlocked:
			p.blocked = append(p.blocked, s.Platform)
			p.keep = append(p.keep, s)
		case s.Reads < programReads:
			if st != jAbsent {
				p.remove = append(p.remove, s.Name)
			}
			p.warnings = append(p.warnings, fmt.Sprintf("a copy of MailArchive for %s kept in this archive cannot open it; it was removed", s.Platform))
		case rp != "" && opens[rp]:
			if st != jAbsent {
				p.remove = append(p.remove, s.Name) // a release copy arrived for its platform
			}
		case st == jChanged && !(s.Platform == v.selfPlatform && !thisOpens && v.selfName != ""):
			// Another computer's own copy, changed: it cannot be written again here, so it
			// goes, its entry marked missing — never left on the drive to be run (B3).
			p.remove = append(p.remove, s.Name)
			s.MissingRuns++
			p.keep = append(p.keep, s)
			p.changed = append(p.changed, s.Platform)
			p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s in this archive was changed; it was removed", s.Platform))
		case s.Platform == v.selfPlatform && !thisOpens && st.present() &&
			((s.Name == v.selfName && s.Version == v.selfVersion()) || container.CompareVersions(s.Version, v.selfVersion()) > 0):
			// This system's own copy — this program's, or a newer one another computer of
			// the same system wrote: kept, never replaced by an older program (CRIT-04).
			p.keep = append(p.keep, v.checked(s, st))
			ownWritten = true
		case s.Platform == v.selfPlatform && !thisOpens && v.selfName != "":
			if st != jAbsent {
				p.remove = append(p.remove, s.Name) // replaced by this program's own
			}
			if st == jChanged { // never silently (the program design §4.2, §6; P3-8, B3)
				p.changed = append(p.changed, s.Platform)
				p.warnings = append(p.warnings, fmt.Sprintf("the copy of MailArchive for %s in this archive was changed; it was replaced with this computer's own program", s.Platform))
			}
		case st.present():
			p.keep = append(p.keep, v.checked(s, st))
		default:
			s.MissingRuns++
			p.keep = append(p.keep, s)
		}
	}
	if !thisOpens && !ownWritten && v.selfName != "" {
		p.add = append(p.add, programAdd{self: true, why: why, file: container.ProgramFile{Name: v.selfName, Origin: container.OriginSelf,
			Version: v.selfVersion(), Platform: v.selfPlatform, Reads: programReads}})
	}
	p.remove = dedupe(p.remove, p.add)
	return p
}

// withoutFailed is v with the files that could not be written taken out of what it
// offers (DUR-01, DUR-02): deciding again keeps each failed file's earlier entry, and
// every removal that depended on it — the platform's self-copy, the older checksum
// file — does not happen.
func (v programView) withoutFailed(failed map[string]bool) programView {
	w := v
	w.cacheAssets = map[string]progcache.Asset{}
	for a, x := range v.cacheAssets {
		if !failed[a] {
			w.cacheAssets[a] = x
		}
	}
	if failed["SHA256SUMS-"+v.cacheVersion] {
		w.cacheSums = progcache.Asset{}
	}
	if failed[v.selfName] {
		w.selfName = ""
	}
	return w
}

// checked is a kept entry as this run found it (R4): checked, its marks cleared; kept
// online only, marked unchecked from the first run that found it so — a self-copy as a
// release copy (the PG4 pass, COR-6).
func (v programView) checked(e container.ProgramFile, st judged) container.ProgramFile {
	if st == jOK {
		e.MissingRuns, e.UncheckedSince = 0, ""
	}
	if st == jUnchecked && e.UncheckedSince == "" {
		e.UncheckedSince = v.now
	}
	return e
}

// selfVersion is the version a self-copy of this program records: its release version,
// or none for a development build.
func (v programView) selfVersion() string {
	if v.release {
		return v.version
	}
	return ""
}

// dedupe drops from remove a name an add writes again (a replacement is a rename over
// it) and repeats.
func dedupe(remove []string, add []programAdd) []string {
	written := map[string]bool{}
	for _, a := range add {
		written[strings.ToLower(a.file.Name)] = true
	}
	seen := map[string]bool{}
	var out []string
	for _, r := range remove {
		l := strings.ToLower(r)
		if written[l] || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, r)
	}
	return out
}
