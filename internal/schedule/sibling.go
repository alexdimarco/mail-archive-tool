package schedule

import (
	"debug/buildinfo"
	"os"
	"runtime/debug"

	"mail-archive-tool/internal/instance"
)

// Seams for SiblingQualifies, overridden in tests so the owner and build-info
// reads can be injected without a real second binary on disk. The production
// values read the real file; nothing here ever executes the sibling.
var (
	lstatFile         = os.Lstat
	readFileBuildInfo = buildinfo.ReadFile
	readSelfBuildInfo = debug.ReadBuildInfo
)

// SiblingQualifies reports whether p — a file the caller has already placed
// beside the running program by a fixed string+stat bound (§4.6.1) — is a
// sibling a schedule may run instead of self. All three hold, checked in order
// (§4.6.0): p is a regular file (not a link); instance.OwnerReader reports the
// same owner for p as for self, compared file against file (never CheckOwner,
// which compares with the process token and would refuse an MSI file owned by
// Administrators); and p is the SameBuild as this program. The first failure
// names the reason. It reads p's owner and embedded build info and never runs it.
func SiblingQualifies(self, p string) (bool, Reason) {
	fi, err := lstatFile(p)
	if err != nil {
		return false, ReasonSiblingAbsent
	}
	if !fi.Mode().IsRegular() {
		return false, ReasonSiblingNotRegular
	}
	selfOwner, _, selfErr := instance.OwnerReader(self)
	pOwner, _, pErr := instance.OwnerReader(p)
	if selfErr != nil || pErr != nil || selfOwner != pOwner {
		return false, ReasonSiblingOtherOwner
	}
	var selfInfo *debug.BuildInfo
	if bi, ok := readSelfBuildInfo(); ok {
		selfInfo = bi
	}
	pInfo, _ := readFileBuildInfo(p) // a read error yields nil, a different build
	if !SameBuild(selfInfo, pInfo) {
		return false, ReasonPairOutOfStep
	}
	return true, ReasonSibling
}

// SameBuild reports whether two build infos are the same build of the one
// program: Path (the main package), Main.Path, Main.Version, GoVersion, every
// dependency, and every build setting EXCEPT -ldflags are equal — vcs.revision
// and vcs.modified included. A nil (or unreadable) side is a different build.
//
// Because Path is part of it, no binary of the three-file era
// (cmd/mailarchive-gui, cmd/mailarchive-desktop) is ever the same build as the one
// program (./cmd/mailarchive): their main packages differ. -ldflags is the one
// setting that legitimately differs between the console build (mailarchive.exe)
// and the no-console build (mailarchivew.exe, -H=windowsgui), so it is excluded.
//
// This is §4.2's build-info equality as one function (§4.6.0, SC7). MA-628 uses it
// to compare the Windows pair; §4.6.1's ProgramForSchedule and §4.6.2's identity
// use it through SiblingQualifies.
func SameBuild(a, b *debug.BuildInfo) bool {
	if a == nil || b == nil {
		return false
	}
	if a.Path != b.Path || a.GoVersion != b.GoVersion {
		return false
	}
	if a.Main.Path != b.Main.Path || a.Main.Version != b.Main.Version || a.Main.Sum != b.Main.Sum {
		return false
	}
	if !sameDeps(a.Deps, b.Deps) {
		return false
	}
	return sameSettings(a.Settings, b.Settings)
}

// sameDeps compares the dependency sets by module path (order-independent),
// including each module's version, checksum and one-level replacement.
func sameDeps(a, b []*debug.Module) bool {
	if len(a) != len(b) {
		return false
	}
	am := make(map[string]*debug.Module, len(a))
	for _, m := range a {
		if m != nil {
			am[m.Path] = m
		}
	}
	for _, mb := range b {
		if mb == nil {
			continue
		}
		ma, ok := am[mb.Path]
		if !ok || ma.Version != mb.Version || ma.Sum != mb.Sum {
			return false
		}
		if (ma.Replace == nil) != (mb.Replace == nil) {
			return false
		}
		if ma.Replace != nil && (ma.Replace.Path != mb.Replace.Path ||
			ma.Replace.Version != mb.Replace.Version || ma.Replace.Sum != mb.Replace.Sum) {
			return false
		}
	}
	return true
}

// sameSettings compares every build setting except -ldflags.
func sameSettings(a, b []debug.BuildSetting) bool {
	am, bm := settingsMap(a), settingsMap(b)
	delete(am, "-ldflags")
	delete(bm, "-ldflags")
	if len(am) != len(bm) {
		return false
	}
	for k, v := range am {
		if bv, ok := bm[k]; !ok || bv != v {
			return false
		}
	}
	return true
}

func settingsMap(s []debug.BuildSetting) map[string]string {
	m := make(map[string]string, len(s))
	for _, kv := range s {
		m[kv.Key] = kv.Value
	}
	return m
}
