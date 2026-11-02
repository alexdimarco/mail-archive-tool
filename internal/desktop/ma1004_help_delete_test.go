package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/archdelete"
	"mail-archive-tool/internal/assure"
)

// The guide's two parts on Delete and Remove from list (docs/design-archive-delete.md §10, §2,
// §13 slice 5), pinned whole: what every platform's guide says, then each platform's own words,
// Windows first.

// helpDeleteSentences are said on every platform.
var helpDeleteSentences = []string{
	// Deleting an archive
	"Delete… erases an archive's files from this computer for good. They are not moved to the Recycle Bin or the Trash, so they cannot be restored from there. Use it for mail you no longer need to keep, for example under a retention rule.",
	"Each archive has its own Delete… button: on its row under Your archives, or on the This archive card when it is your only archive. When the button cannot be used, point at it to see why.",
	"Click Delete…: nothing is deleted yet. Beside the archive, MailArchive shows what Delete would remove (how many files, and their size), the scheduled backups it would stop, and how many files in the folder are not the archive's, which are kept. To have the deletion approved first, click Save this plan to save the plan as a text file.",
	"Type the folder's name exactly as it is shown, then click Delete permanently. Cancel deletes nothing.",
	"The result says what was removed and what was kept. Click Save deletion record to save a copy as a text file: MailArchive keeps no copy after you close the result. Then click Dismiss.",
	"What Delete removes: the archive's own files (its message pages and attachments, its search index and its records), its scheduled backups, its row under Your archives and, when nothing else is left in it, the folder. Files of your own in the folder are kept, and the folder with them. Mail copies in the folder that the archive did not record are kept too, unless you tick the box the plan shows for them. For an encrypted archive, this computer also forgets its key: any other copy of it then opens only with its password or recovery key.",
	"What Delete does not reach: " + archdelete.DoesNotReach + ".",
	"Each delete adds a line to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed. Uninstalling MailArchive removes that log: copy deletions.log first if you need it to check saved deletion records.",
	// Removing an archive from the list
	"Remove from list takes an archive off Your archives and keeps its folder and every file in it, for example an archive you now keep somewhere else, or one on a drive you no longer use.",
	"Click Remove from list on the archive's row. MailArchive asks first, and names any scheduled backups that would stop with it: they would otherwise keep backing up a folder this page no longer shows. Click Yes to remove it, or No to change nothing.",
	"An archive whose drive is unplugged can be removed from the list too; nothing on the drive is touched.",
	"The archive marked Current archive can be removed only when its folder cannot be reached or no longer holds an archive. Otherwise, switch to another archive first, or add one with Add another archive.",
	"For an encrypted archive, Remove from list keeps this computer's record of it and the key it remembers; Forget this encrypted archive here is what forgets those.",
	"To use the archive again later, click Open an archive folder… and choose its folder.",
}

// helpDeletePlatforms are each platform's own sentences in the two parts, and what each must not say.
var helpDeletePlatforms = []helpPlatform{
	{goos: "windows", want: []string{
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. On Windows this is usually a program holding a file open, such as Outlook, an antivirus or Windows Search: close it, then click Delete… again on the archive's row. If the archive's drive was unplugged, reconnect it first.",
		"Synced folders: if the archive's folder is in OneDrive or in another sync service's folder that MailArchive recognises (Dropbox, Google Drive, iCloud Drive, Box and others), the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
		"A SharePoint library synced to this computer is not recognised yet: if the archive is in one, empty its recycle bin on SharePoint too.",
	}, not: []string{"Library/CloudStorage", "the Dock", "System Settings", "a folder you may not change"}},
	{goos: "darwin", want: []string{
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. This is usually a drive that was unplugged (reconnect it), or a file macOS did not let MailArchive remove (a locked file, or a read-only drive): fix that, then click Delete… again on the archive's row.",
		"Synced folders: if the archive's folder is in iCloud Drive, in a folder under Library/CloudStorage (OneDrive, Dropbox, Google Drive and others) or in another sync service's folder that MailArchive recognises, the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
		"If your Desktop and Documents folders are kept in iCloud, an archive in them is not recognised as synced: empty iCloud Drive's Recently Deleted too.",
	}, not: []string{"Windows Search", "SharePoint", "taskbar", "press F5"}},
	{goos: "linux", want: []string{
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. This is usually a drive that was unplugged (reconnect it), or a file the system did not let MailArchive remove (a locked file, a folder you may not change, or a read-only drive): fix that, then click Delete… again on the archive's row.",
		"Synced folders: if the archive's folder is in a sync service's folder that MailArchive recognises (Dropbox, Nextcloud, Google Drive and others), the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
	}, not: []string{"Windows Search", "SharePoint", "Library/CloudStorage", "the Dock", "taskbar"}},
}

// covers: MA-1004, R36, R12, S41
// The guide (How to use MailArchive) has "Deleting an archive" and "Removing an archive from
// the list" (design-archive-delete §10), closed, after Microsoft 365, each sentence pinned whole
// per platform and true of this version: "What Delete does not reach" is §2's line itself
// (archdelete.DoesNotReach), the Windows guide names what holds a file open there and says a
// synced SharePoint library is not recognised, and no platform's guide says another's words.
// Positive twin first: the guide's other parts are still there and still say what they said.
func TestHowToUseMailArchiveExplainsDeleteAndRemoveFromList(t *testing.T) {
	for _, p := range helpDeletePlatforms {
		page := getOverview(t, Config{GOOS: p.goos, ConfigPath: filepath.Join(t.TempDir(), "none.json"), SettingsPath: filepath.Join(t.TempDir(), "s.json")})
		h := readHelpCard(t, page)
		assure.Reached(t, h.text, p.goos+": the guide's text")
		// Positive twin: the guide as it was.
		for _, s := range helpSentences[:3] {
			if !strings.Contains(h.text, s) {
				t.Fatalf("%s: positive twin: the guide lacks %q", p.goos, s)
			}
		}
		for _, part := range []string{"Deleting an archive", "Removing an archive from the list"} {
			if open, ok := h.open[part]; !ok || open {
				t.Errorf("%s: the guide's part %q is there = %v, open = %v; want there and closed", p.goos, part, ok, open)
			}
		}
		for _, s := range append(append([]string{}, helpDeleteSentences...), p.want...) {
			if !strings.Contains(h.text, s) {
				t.Errorf("%s: the guide lacks %q", p.goos, s)
			}
		}
		for _, s := range p.not {
			if strings.Contains(h.text, s) {
				t.Errorf("%s: the guide says %q, which is not true here", p.goos, s)
			}
		}
	}
}

// covers: MA-1004, X11
// The two parts name only controls that exist: each button and mark they tell a person to look
// for is drawn by the row's template (dashboard.go) or the delete's page script (deletescript.go),
// in those very words.
func TestTheGuidesDeletePartsNameRealControls(t *testing.T) {
	read := func(path string) string {
		b, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	dash, script := read("dashboard.go"), read("deletescript.go")
	for _, c := range []struct{ label, src, where string }{
		{`>Delete…</button>`, dash, "dashboard.go"},
		{`>Remove from list</button>`, dash, "dashboard.go"},
		{`<h2>This archive</h2>`, dash, "dashboard.go"},
		{`<h2>Your archives</h2>`, dash, "dashboard.go"},
		{`<h2>Add another archive</h2>`, dash, "dashboard.go"},
		{`Current archive</span>`, dash, "dashboard.go"},
		{`>Forget this encrypted archive here</button>`, dash, "dashboard.go"},
		{`>Open an archive folder…</button>`, dash, "dashboard.go"},
		{`delButton('Save this plan'`, script, "deletescript.go"},
		{`delButton('Delete permanently'`, script, "deletescript.go"},
		{`delButton('Cancel'`, script, "deletescript.go"},
		{`delButton('Save deletion record'`, script, "deletescript.go"},
		{`delButton('Dismiss'`, script, "deletescript.go"},
		{`delButton('Yes'`, script, "deletescript.go"},
		{`delButton('No'`, script, "deletescript.go"},
	} {
		if !strings.Contains(c.src, c.label) {
			t.Errorf("the guide names %s, which %s does not draw", c.label, c.where)
		}
	}
	// The plan's own sentences the guide repeats.
	if archdelete.PlanLogLine != "A line is added to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed." {
		t.Errorf("the plan's deletion-log line changed; the guide repeats it: %q", archdelete.PlanLogLine)
	}
}
