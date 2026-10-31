package desktop

import (
	"runtime"
	"strings"

	"mail-archive-tool/internal/schedule"
)

// What the Desktop's Remove reaches, and what it says when it removed nothing (MA-948).
//
// Remove takes out the scheduler entry named for the archive folder (removeTaskAt →
// schedule.DefaultNameFor). A schedule set up at the command line with its own -name is
// another entry, which Remove leaves in place. So Remove's reply says when it removed
// nothing, and the Scheduled backup card and Settings do not send the owner of such a
// schedule to Remove: they give the command that removes it. Remove itself never acts on
// the name the archive's record holds (the record is untrusted input; widening what
// Remove deletes is left to its own security pass). The words name it, for the owner to
// remove by hand, only when it binds to this archive (schedule.RecordedBackupName, the
// binding `mailarchive schedule -out X -remove` and the wizard's Remove use); otherwise
// they stay generic.

// recordedOwnName reports whether the archive's schedule record names an entry other than
// the one named for the folder: a schedule set up under a name of its own, which Remove does
// not reach. name is that name when it binds to this archive, "" when it does not.
func recordedOwnName(out string) (own bool, name string) {
	if strings.TrimSpace(out) == "" {
		return false, ""
	}
	d, err := schedule.ReadDescriptor(out)
	if err != nil || sameEntryName(d.Name, schedule.DefaultNameFor(out)) {
		return false, ""
	}
	n, err := schedule.RecordedBackupName(out)
	if err != nil {
		return true, ""
	}
	return true, n
}

// sameEntryName reports whether two schedule names are one scheduler entry on this computer:
// launchd and Task Scheduler ignore letter case, cron does not (as schedule.sameName).
func sameEntryName(a, b string) bool {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// removeByNameCommand is the command that removes the schedule called name for the archive
// at out, quoted for goos's shell: the command line's removal by -name (runSchedule).
func removeByNameCommand(goos, out, name string) string {
	return shellJoin([]string{"mailarchive", "schedule", "-out", out, "-name", name, "-remove"}, goos)
}

// removeByHandGeneric is how to remove a schedule set up under a name this page cannot show.
const removeByHandGeneric = "Remove it at the command line where it was set up: mailarchive schedule -name followed by its name, then -remove."

// removeNothingNote is Remove's reply when it removed nothing.
func removeNothingNote(goos, out string) string {
	switch own, name := recordedOwnName(out); {
	case own && name != "":
		return "Nothing was removed. This archive's scheduled backup was set up at the command line under its own name, " + name +
			", and Remove here reaches only a backup named for this archive folder. To remove it (your archive and its mail are kept), type this in a terminal: " +
			removeByNameCommand(goos, out, name)
	case own:
		return "Nothing was removed. This archive's scheduled backup was set up under a name of its own, and Remove here reaches only a backup named for this archive folder. " + removeByHandGeneric
	default:
		return "Nothing was removed: MailArchive found no scheduled backup for this archive folder."
	}
}

// ownNameExportLine is the Scheduled backup card's line for a schedule set up at the command
// line under its own name (MA-942's line, MA-948): toRunJob says this page can then give the
// archive a backup of its own (with Run now).
func ownNameExportLine(goos, out, name string, d schedule.Descriptor, toRunJob bool) string {
	called, how := "a name of its own", removeByHandGeneric
	if name != "" {
		called, how = "its own name ("+name+")", "The command that removes it: "+removeByNameCommand(goos, out, name)
	}
	if !toRunJob {
		return "It was set up at the command line under " + called + ", so change, repair or remove it there; MailArchive Desktop can't remove it. " + how
	}
	carry := "It runs " + cadenceWords(d.Interval, d.At) + " now. Removing it does not keep that: afterwards, choose How often and the time again in Settings."
	if d.Presync != nil {
		carry = "It runs " + cadenceWords(d.Interval, d.At) + " now, with the Thunderbird sync on. Removing it does not keep that: afterwards, choose How often and the time again in Settings, and tick Sync Thunderbird before each backup."
	}
	return "Run now isn't shown for it: MailArchive Desktop can't run, change or remove a backup set up at the command line under " + called +
		". To get Run now, remove it in a terminal (your archive and its mail are kept), reload this page (" + helpDataFor(goos, false).Reload +
		"), then in Settings choose How often and click Save settings. That sets up a backup MailArchive Desktop can run, and Run now then appears here. " +
		carry + " " + how
}

// commandLineOwnNameToRunJob follows commandLineSetUp in Settings for such a schedule.
const commandLineOwnNameToRunJob = " To get one MailArchive Desktop can run, with Run now, remove it in a terminal first (the Scheduled backup card says how), then choose How often here and click Save settings."
