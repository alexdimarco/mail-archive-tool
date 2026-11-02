package desktop

import (
	"os"
	"path/filepath"
	"runtime"
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
	"What Delete removes: the archive's own files (its message pages and attachments, its search index and its records), its scheduled backups, its row under Your archives (for an encrypted archive, only when its folder goes too: see below) and, when nothing else is left in it, the folder. Files of your own in the folder are kept, and the folder with them. Mail copies in the folder that the archive did not record are kept too, unless you tick the box the plan shows for them.",
	// An encrypted archive, as step 8 and step 9 do it (design §5.3; del-s5-A1-01, A2-01): the
	// key goes only with the last recorded copy, and only when the folder goes.
	"An encrypted archive: when the delete leaves its folder gone, this computer forgets its record of that folder. If that was the only copy of the archive this computer recorded, it also forgets the archive's key, and any other copy then opens only with its password or recovery key. If this computer recorded other copies of it, such as one on a USB drive, it keeps the key for them, so they still open here without the password: the result names them. When the folder is kept, because files of your own are in it, this computer still remembers the key and the archive stays under Your archives: the result says so, and Forget this encrypted archive here on its row forgets it. To be sure that no encrypted archive opens on this computer without its password, use Forget every encrypted archive on this computer… on the Uninstall MailArchive card.",
	// The two partial results whose remedy is not Delete again (design §8 (a), §5.4;
	// del-s5-A1-03, A2-05).
	"Two results are different, and say so: if the only files left are mail copies you ticked to delete, the archive is already off Your archives, so there is no Delete… to click again: delete those files by hand. If it says the folder still holds files the delete can no longer identify, delete them by hand, or use Remove from list.",
	// "What Delete does not reach" names only this platform's items since MA-1019 (B4): pinned
	// whole per platform below (helpReach…), each with the other platforms' items refused.
	// Amended 2026-11-02 (MA-1032, the 1.5.3 friction review's B16): the deletion log's sentence
	// now says where the log is (per platform, below) and where to copy it; "Delete not finished",
	// deleting the archive the page shows, and the command line are said.
	"The deletion log: each delete adds a line to this computer's deletion log, deletions.log, which keeps the date, the folder and what was removed.",
	"(a MailArchive Desktop started with -config keeps it beside that config file). Uninstalling MailArchive removes that log: copy deletions.log first, somewhere you keep records, if you need it to check saved deletion records.",
	// Amended 2026-11-02 (the uninstall-docs group's adversarial pass, finding 6): "that archive is
	// not backed up" was more than is enforced (a scheduled backup a delete stopped before step 2
	// left, and mailarchive -out at a terminal, read no deletions.log); Archive now and Run now do
	// refuse (MA-987).
	"Delete not finished: if a delete stops part way, for example because the computer was switched off or the result said Not fully deleted, the archive's row says Delete not finished: click Delete again. Until the delete is finished, Archive now and Run now start nothing on that archive, and it cannot be paused, resumed, switched to or removed from the list. Click Delete… on its row again: its plan begins \"This finishes the delete begun\" and the date, and its deletion record counts what every run of it removed.",
	"Deleting the archive this page shows leaves no archive chosen. Until you click Dismiss on the result, that archive's cards are greyed out. Then, to carry on, click Switch to this archive on another archive's row under Your archives, or click Open an archive folder… to choose one.",
	"At the command line: in a terminal, mailarchive delete-archive -out FOLDER shows the same plan and asks you to type the folder's name; add -plan-out FILE to only save the plan, or -record-out FILE to save the deletion record. mailarchive unlist-archive -out FOLDER is Remove from list. Both work only on an archive under Your archives, and only while MailArchive Desktop is shut down. mailarchive add-archive FOLDER adds an archive folder you already have to Your archives, as Open an archive folder… does, without making it the archive this page shows.",
	// Removing an archive from the list
	"Remove from list takes an archive off Your archives and keeps its folder and every file in it, for example an archive you now keep somewhere else, or one on a drive you no longer use.",
	"Click Remove from list on the archive's row. MailArchive asks first, and names any scheduled backups that would stop with it: they would otherwise keep backing up a folder this page no longer shows. Click Yes to remove it, or No to change nothing.",
	"An archive whose drive is unplugged can be removed from the list too; nothing on the drive is touched.",
	"The archive marked Current archive can be removed only when its folder cannot be reached or no longer holds an archive. Otherwise, switch to another archive first, or add one with Add another archive.",
	// Forget is on the row only, so it comes first (del-s5-A2-06).
	"For an encrypted archive, Remove from list keeps this computer's record of it and the key it remembers. To forget those too, click Forget this encrypted archive here on its row first, then Remove from list: once the row is gone, that button is gone with it.",
	"To use the archive again later, click Open an archive folder… and choose its folder.",
}

// helpDeleteNever are claims no platform's guide makes, because the delete does not do them: the
// key always forgotten (step 8 keeps it for other recorded copies and for a kept folder,
// del-s5-A1-01, A2-01), and Forget offered after the row is gone (del-s5-A2-06).
var helpDeleteNever = []string{
	"this computer also forgets its key: any other copy",
	"is what forgets those",
	"not backed up", // finding 6: nothing stops every backup of a delete not finished
}

// "What Delete does not reach", each platform's (MA-1019, amending MA-1004 on 2026-11-02): §2's
// line with only that platform's backup and restore items.
const (
	helpReachSync = "What Delete does not reach: a sync service's online recycle bin (OneDrive keeps deleted files about 30 days on a personal account, up to 93 on a work account) and version history; "
	helpReachRest = "copies, exports or decrypted copies made elsewhere; sync tools other than those detected."
)

// helpDeletePlatforms are each platform's own sentences in the two parts, and what each must not say.
var helpDeletePlatforms = []helpPlatform{
	{goos: "windows", want: []string{
		helpReachSync + "Windows File History and Previous Versions; " + helpReachRest,
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. On Windows this is usually a program holding a file open, such as Outlook, an antivirus or Windows Search: close it, then click Delete… again on the archive's row. If the archive's drive was unplugged, reconnect it first.",
		"Synced folders: if the archive's folder is in OneDrive or in another sync service's folder that MailArchive recognises (Dropbox, Google Drive, iCloud Drive, Box and others), the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
		"A SharePoint library synced to this computer is not recognised yet: if the archive is in one, empty its recycle bin on SharePoint too.",
		`On Windows, it is %APPDATA%\mailarchive\deletions.log (a MailArchive Desktop`,
		// Amended 2026-11-02 (the uninstall-docs group's adversarial pass, finding 7): the .msi
		// puts nothing on PATH, so the full path, quoted.
		`without making it the archive this page shows. On Windows the command-line program is mailarchive.exe, in MailArchive's folder: C:\Program Files\MailArchive Desktop when installed from the .msi, or the folder you unzipped it to. The command line does not find it by name alone, so type its full path in quotes, for example "C:\Program Files\MailArchive Desktop\mailarchive.exe" delete-archive -out FOLDER.`,
	}, not: []string{"Library/CloudStorage", "the Dock", "System Settings", "a folder you may not change", "Time Machine", "~/", "tar.gz"}},
	{goos: "darwin", want: []string{
		helpReachSync + "macOS Time Machine; " + helpReachRest,
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. This is usually a drive that was unplugged (reconnect it), or a file macOS did not let MailArchive remove (a locked file, or a read-only drive): fix that, then click Delete… again on the archive's row.",
		"Synced folders: if the archive's folder is in iCloud Drive, in a folder under Library/CloudStorage (OneDrive, Dropbox, Google Drive and others) or in another sync service's folder that MailArchive recognises, the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
		"If your Desktop and Documents folders are kept in iCloud, an archive in them is not recognised as synced: empty iCloud Drive's Recently Deleted too.",
		"On a Mac, it is ~/Library/Application Support/mailarchive/deletions.log (a MailArchive Desktop",
		"without making it the archive this page shows. On a Mac the command-line program is the separate download mailarchive-cli-macos.tar.gz; run it in Terminal.",
	}, not: []string{"Windows Search", "SharePoint", "taskbar", "press F5", "File History", "%APPDATA%", "mailarchive.exe", "XDG_CONFIG_HOME"}},
	{goos: "linux", want: []string{
		helpReachSync + helpReachRest,
		"If the result says Not fully deleted, the result names the files that could not be removed and what to do. This is usually a drive that was unplugged (reconnect it), or a file the system did not let MailArchive remove (a locked file, a folder you may not change, or a read-only drive): fix that, then click Delete… again on the archive's row.",
		"Synced folders: if the archive's folder is in a sync service's folder that MailArchive recognises (Dropbox, Nextcloud, Google Drive and others), the plan says so. Deleting it here also deletes it there, and the service may keep it in its own recycle bin: empty that too.",
		"On Linux, it is ~/.config/mailarchive/deletions.log ($XDG_CONFIG_HOME/mailarchive/deletions.log when XDG_CONFIG_HOME is set) (a MailArchive Desktop",
		"without making it the archive this page shows. Removing an archive from the list",
	}, not: []string{"Windows Search", "SharePoint", "Library/CloudStorage", "the Dock", "taskbar", "File History", "Time Machine", "%APPDATA%", "mailarchive.exe", "tar.gz"}},
}

// covers: MA-1004, MA-1032, R36, R12, S41
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
		for _, s := range append(append([]string{}, p.not...), helpDeleteNever...) {
			if strings.Contains(h.text, s) {
				t.Errorf("%s: the guide says %q, which is not true here", p.goos, s)
			}
		}
	}
}

// covers: MA-1004, MA-1032, X11
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
		{`>Forget every encrypted archive on this computer…</button>`, dash, "dashboard.go"},
		{`<h2>Uninstall MailArchive</h2>`, dash, "dashboard.go"},
		{`>Open an archive folder…</button>`, dash, "dashboard.go"},
		{`delButton('Save this plan'`, script, "deletescript.go"},
		{`delButton('Delete permanently'`, script, "deletescript.go"},
		{`delButton('Cancel'`, script, "deletescript.go"},
		{`delButton('Save deletion record'`, script, "deletescript.go"},
		{`delButton('Dismiss'`, script, "deletescript.go"},
		{`delButton('Yes'`, script, "deletescript.go"},
		{`delButton('No'`, script, "deletescript.go"},
		// Amended 2026-11-02 (MA-1032, B16): the parts now also name these.
		{`>Switch to this archive</button>`, dash, "dashboard.go"},
		{`<span class="archive-tag">Delete not finished: click Delete again</span>`, dash, "dashboard.go"},
	} {
		if !strings.Contains(c.src, c.label) {
			t.Errorf("the guide names %s, which %s does not draw", c.label, c.where)
		}
	}
	// The commands the guide names (MA-1032, B16) are verbs the program runs, with those flags.
	mainGo, delGo := read(filepath.Join("..", "..", "cmd", "mailarchive", "main.go")), read(filepath.Join("..", "..", "cmd", "mailarchive", "archivedelete.go"))
	for _, verb := range []string{"delete-archive", "unlist-archive", "add-archive"} {
		if !strings.Contains(mainGo, `args[0] == "`+verb+`"`) {
			t.Errorf("the guide names mailarchive %s, which the program does not run", verb)
		}
	}
	for _, flag := range []string{`fs.String("out"`, `fs.String("plan-out"`, `fs.String("record-out"`, `fs.String("config"`} {
		if !strings.Contains(delGo, flag) {
			t.Errorf("the guide names the flag of %s, which delete-archive does not define", flag)
		}
	}
	// The deletion log's place, as the guide gives it for this computer, is where MailArchive
	// keeps it (archdelete.DefaultLogPath); on Linux with XDG_CONFIG_HOME set (as this package's
	// tests set it), the guide's note on XDG_CONFIG_HOME.
	want, err := archdelete.DefaultLogPath()
	if err != nil {
		t.Fatal(err)
	}
	got, note, _ := strings.Cut(helpDataFor(runtime.GOOS, false).LogPath, " (")
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" && os.Getenv("XDG_CONFIG_HOME") != "" {
		got, _, _ = strings.Cut(note, " when")
		got = strings.Replace(got, "$XDG_CONFIG_HOME", os.Getenv("XDG_CONFIG_HOME"), 1)
	}
	home, _ := os.UserHomeDir()
	got = strings.Replace(got, "~", home, 1)
	got = strings.Replace(got, "%APPDATA%", os.Getenv("APPDATA"), 1)
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Errorf("the guide says deletions.log is at %s on this computer; MailArchive keeps it at %s", got, want)
	}
	// The Windows guide's folder for mailarchive.exe is where the .msi installs it, and the .msi
	// puts nothing on PATH, which is why the guide gives the full path (finding 7).
	wxs := read(filepath.Join("..", "..", "packaging", "mailarchive-desktop", "Product.wxs"))
	for _, w := range []string{`<Directory Id="ProgramFiles64Folder">`, `<Directory Id="INSTALLFOLDER" Name="MailArchive Desktop">`, `Source="$(var.SourceDir)\mailarchive.exe"`} {
		if !strings.Contains(wxs, w) {
			t.Errorf(`the Windows guide says C:\Program Files\MailArchive Desktop\mailarchive.exe, but Product.wxs lacks %s`, w)
		}
	}
	if strings.Contains(wxs, "<Environment") {
		t.Error("Product.wxs now sets an environment variable (PATH?): the Windows guide's full path may no longer be needed")
	}
	// The plan's own sentences the guide repeats.
	// "If you delete it" (MA-1028, B5): the guide's own sentence, "Each delete adds a line…", fits.
	if archdelete.PlanLogLine != "If you delete it, a line is added to this computer's deletion log (deletions.log), which keeps the date, the folder and what was removed." {
		t.Errorf("the plan's deletion-log line changed; the guide repeats it: %q", archdelete.PlanLogLine)
	}
}
