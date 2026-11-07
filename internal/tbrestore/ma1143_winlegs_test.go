package tbrestore

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-1143, R40
// The Windows legs' rules, on every platform (design §3.1 step 1 note, §3.2 step 4
// note): a folder directly in %LOCALAPPDATA%\Packages whose name holds "Thunderbird"
// (any case) is a Store Thunderbird (W-STORE), none is not (positive twin); a UNC path
// is a network share and a \\?\ device path to a drive is not; a drive path's root is
// its letter; a volume that reports no file identity refuses the Local Folders with
// W-NOIDENT, naming it, nothing written.
func TestRestoreWindowsLegRules(t *testing.T) {
	pkgs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(pkgs, "Microsoft.WindowsCalculator_8wekyb3d8bbwe"), 0o700); err != nil {
		t.Fatal(err)
	}
	if storePackageIn(pkgs) {
		t.Fatal("no Thunderbird package: no Store warning")
	}
	if storePackageIn(filepath.Join(pkgs, "missing")) {
		t.Fatal("no Packages folder: no Store warning")
	}
	writeFile(t, filepath.Join(pkgs, "Thunderbird-notes.txt"), "a file is not a package")
	if storePackageIn(pkgs) {
		t.Fatal("a file named for Thunderbird is not a package")
	}
	if err := os.MkdirAll(filepath.Join(pkgs, "MozillaThunderbird.ESR_abc123"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !storePackageIn(pkgs) {
		t.Fatal("a Thunderbird package folder gives the Store warning")
	}

	for p, want := range map[string]bool{
		`\\nas\share\Thunderbird\Mail`:       true,
		`\\?\UNC\nas\share\Mail`:             true,
		`\\?\C:\Users\Ann\AppData\Roaming\x`: false,
		`\\.\PhysicalDrive0`:                 false,
		`C:\Users\Ann\AppData\Roaming\Mail`:  false,
	} {
		if isUNC(p) != want {
			t.Errorf("isUNC(%q) = %v, want %v", p, !want, want)
		}
	}

	was := dirIdentitySeam
	dirIdentitySeam = func(string) (Identity, error) { return Identity{}, errNoIdentity }
	defer func() { dirIdentitySeam = was }()
	profile, local := localProfile(t)
	before := snapshot(t, profile)
	p, err := ReadPrefs(profile)
	if err != nil {
		t.Fatal(err)
	}
	_, err = FindLocalFolders(profile, p)
	refusedAs(t, err, "W-NOIDENT", func() bool { return snapshot(t, profile) == before }, "could not identify the drive", local, "network drives")
}
