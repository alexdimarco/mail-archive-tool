package thunderbird

import (
	"reflect"
	"testing"
)

// covers: MA-1084, R40
// The shared profiles.ini parser (docs/design-restore-thunderbird.md §3.1, slice R2)
// reads each section as written: its header, Name=, the last Path= and IsRelative=,
// the last Default= (a Profile section's flag), and every Default= that names a profile
// (an [Install…] section's), in file order, with several profiles. Control characters
// are removed from every line before it is read, lines before the first section are
// ignored, and nothing is resolved or opened here (relative and absolute paths come back
// as written).
func TestParseProfilesINIReadsEachSectionAsWritten(t *testing.T) {
	ini := "stray=before any section\n" +
		"[Install308046B0AF4A39CB]\r\nDefault=Profiles/inst.default\r\nLocked=1\r\n\r\n" +
		"[Profile1]\nName=Work\nIsRelative=0\nPath=/home/me/elsewhere/work\n\n" +
		"[Profile0]\nName= Personal \x07\nIsRelative=1\nPath=Profiles/rel.default\nDefault=1\n\n" +
		"[General]\nStartWithLastProfile=1\nVersion=2\n"
	got := ParseProfilesINI([]byte(ini))
	want := []ProfilesSection{
		{Header: "Install308046B0AF4A39CB", Default: "Profiles/inst.default", Defaults: []string{"Profiles/inst.default"}},
		{Header: "Profile1", Name: "Work", Path: "/home/me/elsewhere/work"},
		{Header: "Profile0", Name: "Personal", Path: "Profiles/rel.default", IsRelative: true, Default: "1"},
		{Header: "General"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sections:\n got %+v\nwant %+v", got, want)
	}
	if !got[0].IsInstall() || got[1].IsInstall() {
		t.Errorf("IsInstall: %v %v, want true false", got[0].IsInstall(), got[1].IsInstall())
	}
	if got[2].DefaultFlag() != true || got[1].DefaultFlag() {
		t.Errorf("DefaultFlag: Profile0 %v Profile1 %v, want true false", got[2].DefaultFlag(), got[1].DefaultFlag())
	}

	// The last Path= and IsRelative= of a section stand; every naming Default= is kept.
	again := ParseProfilesINI([]byte("[Install1]\nDefault=a\nDefault=0\nDefault=b\n[Profile0]\nPath=x\nPath=y\nIsRelative=1\nIsRelative=0\n"))
	if d := again[0].Defaults; !reflect.DeepEqual(d, []string{"a", "b"}) || again[1].Path != "y" || again[1].IsRelative {
		t.Errorf("repeated keys: defaults %q path %q rel %v", d, again[1].Path, again[1].IsRelative)
	}
	// A control character inside a value is removed, not kept.
	if s := ParseProfilesINI([]byte("[Profile0]\nPath=Profiles/rel\x07.default\x1b\n")); s[0].Path != "Profiles/rel.default" {
		t.Errorf("control characters kept in a value: %q", s[0].Path)
	}
	// Nothing to read: no sections, no panic.
	for _, data := range [][]byte{nil, []byte("no sections at all\n"), make([]byte, ProfilesINIMaxBytes+100)} {
		if s := ParseProfilesINI(data); len(s) != 0 {
			t.Errorf("%d bytes with no section gave %d sections", len(data), len(s))
		}
	}
}
