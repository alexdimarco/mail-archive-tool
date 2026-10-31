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
// at out, quoted for goos's shell: the command line's removal by -name (runSchedule). It runs
// the console program beside the running Desktop, exe (consoleProgram), since no installer
// puts mailarchive on the PATH.
func removeByNameCommand(goos, exe, out, name string) string {
	return shellJoin([]string{consoleProgram(goos, exe), "schedule", "-out", out, "-name", name, "-remove"}, goos)
}

// consoleProgram is the program to type at a terminal, from exe, the running Desktop's own
// path (Config.Exe, never a path from the archive's record, which is untrusted): exe itself,
// except under a name that is not the console program — mailarchivew.exe (Windows' windowed
// build), or a former name (schedule.FormerName: mailarchive-desktop, mailarchive-gui), which
// routes every command to the Desktop or the wizard (dispatch.go) — where it is the mailarchive program in
// the same folder: mailarchive.exe in the Windows install folder, the program inside the Mac
// app. With exe unknown it is the bare name.
func consoleProgram(goos, exe string) string {
	sep := "/"
	if goos == "windows" {
		sep = `\/`
	}
	i := strings.LastIndexAny(exe, sep)
	if strings.TrimSpace(exe) == "" || i == len(exe)-1 {
		return "mailarchive"
	}
	base := exe[i+1:]
	if schedule.FormerName(base, goos) || strings.TrimSuffix(strings.ToLower(base), ".exe") == "mailarchivew" {
		name := "mailarchive"
		if goos == "windows" {
			name += ".exe"
		}
		return exe[:i+1] + name
	}
	return exe
}

// inTerminal is where to type a command on goos: a quoted program path is a command in
// Command Prompt but not in PowerShell, so on Windows it names Command Prompt.
func inTerminal(goos string) string {
	if goos == "windows" {
		return "in Command Prompt"
	}
	return "in a terminal"
}

// removeByHandGeneric is how to remove a schedule set up under a name this page cannot show.
const removeByHandGeneric = "Remove it at the command line where it was set up: mailarchive schedule -name followed by its name, then -remove."

// removeNothingNote is Remove's reply when it removed nothing. A record that is there but
// cannot be read is named with the command line's remedy (ESC-10), never as "no scheduled
// backup": nothing in it says which schedule it was, and that schedule may still run.
func removeNothingNote(goos, exe, out string) string {
	if p := schedule.UnreadableDescriptor(out); p != "" {
		return "Nothing was removed. This archive's schedule record, " + p + ", can't be read, so MailArchive can't tell which scheduled backup it records, and that backup may still be running. To fix the record: " +
			schedule.UnreadableDescriptorRemedy + "."
	}
	switch own, name := recordedOwnName(out); {
	case own && name != "":
		return "Nothing was removed. This archive's scheduled backup was set up at the command line under its own name, " + name +
			", and Remove here reaches only a backup named for this archive folder. To remove it (your archive and its mail are kept), type this " + inTerminal(goos) + ": " +
			removeByNameCommand(goos, exe, out, name)
	case own:
		return "Nothing was removed. This archive's scheduled backup was set up under a name of its own, and Remove here reaches only a backup named for this archive folder. " + removeByHandGeneric
	default:
		return "Nothing was removed: MailArchive found no scheduled backup for this archive folder."
	}
}

// ownNameExportLine is the Scheduled backup card's line for a schedule set up at the command
// line under its own name (MA-942's line, MA-948): toRunJob says this page can then give the
// archive a backup of its own (with Run now).
func ownNameExportLine(goos, exe, out, name string, d schedule.Descriptor, toRunJob bool) string {
	called, how := "a name of its own", removeByHandGeneric
	if name != "" {
		called, how = "its own name ("+name+")", "The command that removes it: "+removeByNameCommand(goos, exe, out, name)
		if goos == "windows" {
			how = "The command that removes it, typed " + inTerminal(goos) + ": " + removeByNameCommand(goos, exe, out, name)
		}
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
