package uninstall

import "testing"

// covers: MA-938, S41
// After Remove, MailArchive Desktop shows how to remove the program itself. On a Mac
// it names the bundle as it is built and installed, "Mail Archive.app"
// (packaging/macos/build-app.sh), not "MailArchive.app", which a person looking in
// Applications would not find. Windows and Linux keep their own steps.
func TestBinaryNoteNamesTheInstalledProgram(t *testing.T) {
	for _, c := range []struct{ goos, exe, want string }{
		{"windows", "", "To remove the program itself, open Settings → Apps → Installed apps (or Add/Remove Programs) and uninstall MailArchive."},
		{"darwin", "", "To remove the program itself, drag Mail Archive.app (in your Applications folder, or wherever you put it) to the Trash."},
		{"linux", "/usr/local/bin/mailarchive", "To remove the program itself, delete the mailarchive binary (/usr/local/bin/mailarchive)."},
	} {
		if got := binaryNoteFor(c.goos, c.exe); got != c.want {
			t.Errorf("%s: %q, want %q", c.goos, got, c.want)
		}
	}
}
