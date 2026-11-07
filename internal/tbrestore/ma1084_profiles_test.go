package tbrestore

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1084, R40
// The restore's use of the shared profiles.ini parser (design §3.1, slice R3): every
// listed profile with its name and folder (relative and absolute Path=), the one an
// [Install…] Default= names (relative, and absolute read as absolute), Default=1,
// names shown without bidi or control characters, a profile missing or without
// prefs.js not offered, and no profiles.ini at all an empty list.
func TestRestoreProfileListFromProfilesINI(t *testing.T) {
	base := t.TempDir()
	absProfile := makeProfile(t, filepath.Join(t.TempDir(), "elsewhere"))
	makeProfile(t, filepath.Join(base, "Profiles", "a.default"))
	makeProfile(t, filepath.Join(base, "Profiles", "b.work"))
	if err := os.MkdirAll(filepath.Join(base, "Profiles", "c.noprefs"), 0o700); err != nil {
		t.Fatal(err)
	}
	ini := strings.Join([]string{
		"[General]", "StartWithLastProfile=1", "",
		"[Profile0]", "Name=default", "IsRelative=1", "Path=Profiles/a.default", "Default=1", "",
		"[Profile1]", "Name=W‮ork\x1b[31m‎", "IsRelative=1", "Path=Profiles/b.work", "",
		"[Profile2]", "Name=moved", "IsRelative=0", "Path=" + absProfile, "",
		"[Profile3]", "Name=gone", "IsRelative=1", "Path=Profiles/nothere", "",
		"[Profile4]", "Name=noprefs", "IsRelative=1", "Path=Profiles/c.noprefs", "",
		"[Install308046B0AF4A39CB]", "Default=" + absProfile, "Locked=1", "",
	}, "\n")
	writeFile(t, filepath.Join(base, ProfilesININame), ini)

	l, err := ReadProfiles([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	ps := assure.Reached(t, l.Profiles, "listed profiles")
	if len(ps) != 3 {
		t.Fatalf("want 3 profiles (gone and no prefs.js left out), got %+v", ps)
	}
	if ps[0].Name != "default" || ps[0].Dir != canonical(filepath.Join(base, "Profiles", "a.default")) || !ps[0].DefaultFlag || ps[0].OpensByDefault {
		t.Errorf("profile 0: %+v", ps[0])
	}
	if ps[1].Name != "Work[31m" {
		t.Errorf("name shown with its bidi and control characters removed: got %q", ps[1].Name)
	}
	if ps[2].Dir != canonical(absProfile) || !ps[2].OpensByDefault {
		t.Errorf("an absolute Path= and an absolute [Install] Default= read as absolute: %+v", ps[2])
	}
	for _, p := range ps {
		if !p.Usable {
			t.Errorf("%s should be usable: %s", p.Dir, p.Unusable)
		}
	}
	if l.ID == "" {
		t.Fatal("the list carries no id")
	}

	// A relative [Install] Default= is joined to profiles.ini's folder.
	base2 := t.TempDir()
	makeProfile(t, filepath.Join(base2, "Profiles", "x"))
	writeFile(t, filepath.Join(base2, ProfilesININame), "[Profile0]\nName=x\nIsRelative=1\nPath=Profiles/x\n[InstallABC]\nDefault=Profiles/x\n")
	l2, err := ReadProfiles([]string{base2})
	if err != nil || len(l2.Profiles) != 1 || !l2.Profiles[0].OpensByDefault {
		t.Fatalf("relative install default: %+v %v", l2, err)
	}

	// No profiles.ini at all: an empty list, not an error.
	empty, err := ReadProfiles([]string{t.TempDir()})
	if err != nil || len(empty.Profiles) != 0 {
		t.Fatalf("no profiles.ini: %+v %v", empty, err)
	}
}

// covers: MA-1084, R40
// A profiles.ini over 256 KiB is refused for the restore (W-PROFINI), never cut short
// as presync's read is; one at exactly the cap is read (positive twin).
func TestRestoreProfilesINIOverTheCapIsRefused(t *testing.T) {
	base := t.TempDir()
	makeProfile(t, filepath.Join(base, "p"))
	head := "[Profile0]\nName=p\nIsRelative=1\nPath=p\n"
	pad := func(total int) string { return head + "#" + strings.Repeat("x", total-len(head)-2) + "\n" }

	writeFile(t, filepath.Join(base, ProfilesININame), pad(256*1024))
	l, err := ReadProfiles([]string{base})
	if err != nil || len(assure.Reached(t, l.Profiles, "profiles at the cap")) != 1 {
		t.Fatalf("a file at exactly the cap is read: %v", err)
	}

	writeFile(t, filepath.Join(base, ProfilesININame), pad(256*1024+1))
	_, err = ReadProfiles([]string{base})
	refusedAs(t, err, "W-PROFINI", nil, "profiles.ini", "larger than MailArchive will read", "Profile Manager")
}

// covers: MA-1084, R40
// The list is bound to its id (§3.1 step 6 note): the same list resolves; a list whose
// profiles.ini was rewritten between two clicks, a folder swapped at the same path,
// and a stale list id are each refused with "Choose the profile again".
func TestRestoreProfileListBinding(t *testing.T) {
	base := t.TempDir()
	makeProfile(t, filepath.Join(base, "a"))
	makeProfile(t, filepath.Join(base, "b"))
	ini := "[Profile0]\nName=a\nIsRelative=1\nPath=a\n[Profile1]\nName=b\nIsRelative=1\nPath=b\n"
	writeFile(t, filepath.Join(base, ProfilesININame), ini)
	l, err := ReadProfiles([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	p, err := l.Resolve(l.ID, 1)
	if err != nil || p.Dir != canonical(filepath.Join(base, "b")) {
		t.Fatalf("the same list resolves: %+v %v", p, err)
	}
	_, err = l.Resolve("0000000000000000", 1)
	refusedAs(t, err, "W-PROFCHANGED", nil, "Choose the profile again")

	// profiles.ini rewritten: position 1 now names a.
	writeFile(t, filepath.Join(base, ProfilesININame), "[Profile0]\nName=b\nIsRelative=1\nPath=b\n[Profile1]\nName=a\nIsRelative=1\nPath=a\n")
	_, err = l.Resolve(l.ID, 1)
	refusedAs(t, err, "W-PROFCHANGED", nil, "Choose the profile again")

	// The folder swapped at the same path (same profiles.ini, another folder).
	writeFile(t, filepath.Join(base, ProfilesININame), ini)
	l, err = ReadProfiles([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(base, "b"), filepath.Join(base, "b.old")); err != nil {
		t.Fatal(err)
	}
	makeProfile(t, filepath.Join(base, "b"))
	_, err = l.Resolve(l.ID, 1)
	refusedAs(t, err, "W-PROFCHANGED", nil, "Choose the profile again")
}

// covers: MA-1084, R40
// macOS and Linux (new code on Linux): a profile folder that is group- or
// world-writable is listed as not usable with W-PROFMODE, never dropped silently, and
// Resolve and CheckProfile refuse it; the same folder made 0700 is usable.
func TestRestoreProfileModeChecks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ownership and mode are not checked on Windows (design §9 item 4; presync's stated reduction)")
	}
	base := t.TempDir()
	dir := makeProfile(t, filepath.Join(base, "p"))
	writeFile(t, filepath.Join(base, ProfilesININame), "[Profile0]\nName=p\nIsRelative=1\nPath=p\n")
	l, err := ReadProfiles([]string{base})
	if err != nil || !assure.Reached(t, l.Profiles, "profiles")[0].Usable {
		t.Fatalf("a 0700 profile is usable: %+v %v", l, err)
	}
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatal(err)
	}
	l, err = ReadProfiles([]string{base})
	if err != nil {
		t.Fatal(err)
	}
	p := assure.Reached(t, l.Profiles, "profiles")[0]
	if p.Usable || !strings.Contains(p.Unusable, "can be changed by other accounts") || !strings.Contains(p.Unusable, "chmod go-w") {
		t.Fatalf("a group-writable profile is listed as not usable with W-PROFMODE: %+v", p)
	}
	_, err = l.Resolve(l.ID, 0)
	refusedAs(t, err, "W-PROFMODE", nil, "chmod go-w", canonical(dir))
	_, err = CheckProfile(dir)
	refusedAs(t, err, "W-PROFMODE", nil, "chmod go-w")
	if _, ok := l.Default(nil, ""); ok {
		t.Error("a profile that is not usable is never the default")
	}
}

// covers: MA-1084, R40
// The Linux profile locations, in order: the plain one, Snap, then Flatpak (design
// §3.1 step 1); macOS ~/Library/Thunderbird.
func TestRestoreProfileBases(t *testing.T) {
	got := homeBasesFor(t, "/h", "linux")
	want := []string{"/h/.thunderbird", "/h/snap/thunderbird/common/.thunderbird", "/h/.var/app/org.mozilla.Thunderbird/.thunderbird"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("linux bases: %v", got)
	}
	if got := homeBasesFor(t, "/h", "darwin"); len(got) != 1 || got[0] != "/h/Library/Thunderbird" {
		t.Errorf("darwin bases: %v", got)
	}
}

// covers: MA-1084, R40, S70
// The default profile (design §3.1 step 4 as folded, RT-10): with several usable
// profiles, the first in the order opens-by-default → Default=1 → listed that the
// archive does not back up; with every one backed up, the old order (the source's
// profile first); with one profile, that one; with none chosen among several, the
// person chooses. BacksUp is the same containment test on every platform.
func TestRestoreDefaultProfile(t *testing.T) {
	base := t.TempDir()
	a := makeProfile(t, filepath.Join(base, "a"))
	b := makeProfile(t, filepath.Join(base, "b"))
	c := makeProfile(t, filepath.Join(base, "c"))
	writeFile(t, filepath.Join(base, ProfilesININame),
		"[Profile0]\nName=a\nIsRelative=1\nPath=a\n[Profile1]\nName=b\nIsRelative=1\nPath=b\nDefault=1\n[Profile2]\nName=c\nIsRelative=1\nPath=c\n[InstallX]\nDefault=c\n")
	l, err := ReadProfiles([]string{base})
	if err != nil || len(l.Profiles) != 3 {
		t.Fatalf("%+v %v", l, err)
	}
	inputs := []string{filepath.Join(c, "Mail", "Local Folders"), filepath.Join(a, "ImapMail")}
	backs := func(dir string) bool { return BacksUp(inputs, dir) }
	if !BacksUp(inputs, c) || !BacksUp(inputs, a) || BacksUp(inputs, b) {
		t.Fatal("BacksUp: an input inside a profile backs it up, none inside b")
	}
	if pos, ok := l.Default(backs, c); !ok || l.Profiles[pos].Dir != canonical(b) {
		t.Errorf("several profiles: the first not backed up (b, Default=1, after the backed-up install default c): got %d %v", pos, ok)
	}
	all := func(string) bool { return true }
	if pos, ok := l.Default(all, a); !ok || l.Profiles[pos].Dir != canonical(a) {
		t.Errorf("every profile backed up: the source's profile first: got %d", pos)
	}
	if pos, ok := l.Default(all, ""); !ok || l.Profiles[pos].Dir != canonical(c) {
		t.Errorf("every profile backed up, no source: the one Thunderbird opens: got %d", pos)
	}

	one := t.TempDir()
	makeProfile(t, filepath.Join(one, "only"))
	writeFile(t, filepath.Join(one, ProfilesININame), "[Profile0]\nName=only\nIsRelative=1\nPath=only\n")
	l1, _ := ReadProfiles([]string{one})
	if pos, ok := l1.Default(all, ""); !ok || pos != 0 {
		t.Errorf("one profile is shown, not asked: %d %v", pos, ok)
	}

	two := t.TempDir()
	makeProfile(t, filepath.Join(two, "x"))
	makeProfile(t, filepath.Join(two, "y"))
	writeFile(t, filepath.Join(two, ProfilesININame), "[Profile0]\nName=x\nIsRelative=1\nPath=x\n[Profile1]\nName=y\nIsRelative=1\nPath=y\n")
	l2, _ := ReadProfiles([]string{two})
	if _, ok := l2.Default(all, ""); ok {
		t.Error("two profiles, both backed up, no default marked: the person chooses")
	}
}

func homeBasesFor(t *testing.T, home, goos string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Unix bases are not built on Windows")
	}
	return unixHomeBases(home, goos)
}
