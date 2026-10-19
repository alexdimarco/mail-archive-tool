package health

import "testing"

// covers: MA-618, R18, S59
// §4.6.3's re-point term compares PATHS, not os.SameFile, so re-pointing to a
// different path — a different directory, or (same directory) a different name —
// is a re-point, and the macOS .app link (same file, different path) counts too.
// The basis gate and the ProgramForSchedule call that drive ExeRepoint in Gather
// are §4.6.2's identity (resolved through injectable seams); this pins the pure
// comparison the flag turns on.
func TestMA618_RepointDiffers(t *testing.T) {
	cases := []struct {
		name           string
		exe, rec, goos string
		want           bool
	}{
		{"console to sibling, same dir", "/opt/ma/mailarchivew.exe", "/opt/ma/mailarchive.exe", "windows", true},
		{"the .app link name differs", "/Applications/MailArchive.app/Contents/MacOS/mailarchive", "/Applications/MailArchive.app/Contents/MacOS/mailarchive-gui", "darwin", true},
		{"different directory", "/opt/ma/mailarchive", "/old/place/mailarchive", "linux", true},
		{"identical path is no re-point", "/opt/ma/mailarchive", "/opt/ma/mailarchive", "linux", false},
		{"windows name is case-insensitive", "/opt/ma/MailArchive", "/opt/ma/mailarchive", "windows", false},
		{"linux name is case-sensitive", "/opt/ma/MailArchive", "/opt/ma/mailarchive", "linux", true},
	}
	for _, c := range cases {
		if got := repointDiffers(c.exe, c.rec, c.goos); got != c.want {
			t.Errorf("%s: repointDiffers(%q,%q,%s) = %v, want %v", c.name, c.exe, c.rec, c.goos, got, c.want)
		}
	}
}
