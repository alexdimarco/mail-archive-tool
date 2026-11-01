package archdelete

import "os"

// RowRefusal is what a row of Your archives says about Delete before any plan is made
// (docs/design-archive-delete.md §10: Delete's enabled state comes from §4's archive test
// and §7, not from the row's greyed state, and when it is disabled its title says why in
// §7's words; MA-975): a Desktop started with -out naming the folder, the folder not
// reachable, the capture of the current archive registered under its name, the archive
// test (not an archive, a mixed folder, a manifest it cannot read or a newer one), a backup
// running on it. An unfinished delete of the folder in deps.Log (§5.4) is never refused
// here: Delete again finishes it, and the plan decides. knownArchive skips the archive test
// when the caller's own read already found an archive (archindex's bounded read), so a
// page load does not parse a large manifest; the plan reads it again in any case. nil:
// Delete is offered.
func RowRefusal(folder string, deps Deps, knownArchive bool) error {
	abs := cleanAbs(folder)
	goos := deps.goos()
	if deps.Settings.LaunchOut != "" && sameFolder(deps.Settings.LaunchOut, abs, goos) {
		return refuse(CaseLaunchOut, msgLaunchOut)
	}
	if deps.Log != "" {
		if u, err := UnfinishedDelete(deps.Log, abs, goos); err == nil && u != nil {
			return nil
		}
	}
	fi, err := os.Stat(abs)
	if err != nil || !fi.IsDir() {
		if err != nil {
			return refuse(CaseUnreachable, msgUnreachable)
		}
		return refuse(CaseNotArchive, msgNotArchive)
	}
	if err := checkBackupFor(abs, deps); err != nil {
		return err
	}
	if !knownArchive {
		k, err := archiveTest(abs)
		if err != nil {
			return err
		}
		if k == 0 {
			return refuse(CaseNotArchive, msgNotArchive)
		}
	}
	if deps.Busy != nil && deps.Busy(abs) {
		return refuse(CaseBusy, msgBusy)
	}
	return nil
}
