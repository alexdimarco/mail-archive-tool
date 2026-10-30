package presync

import "strings"

// winInstallDir returns the folder holding a Windows executable path, with
// Windows' own rules, on any platform: either separator; a drive root keeps its
// separator ("C:\thunderbird.exe" → "C:\", not "C:", which would mean the drive's
// current folder); a name with no folder gives "" (the launch then keeps the
// caller's working folder, as before). The Windows launch runs Thunderbird in this
// folder (design-presync-attended C-65), so a Thunderbird left open for hours
// never pins the folder Mail Archive was started in — an archive, a removable
// drive. Untagged and pure, so the Linux gate checks it; on Windows it equals
// filepath.Dir for every absolute path (TestBuildLaunchCmdWorkingFolder).
func winInstallDir(exe string) string {
	i := strings.LastIndexAny(exe, `\/`)
	if i < 0 {
		return ""
	}
	dir := exe[:i]
	if dir == "" || (len(dir) == 2 && dir[1] == ':') {
		return exe[:i+1] // a root: keep its separator
	}
	return dir
}
