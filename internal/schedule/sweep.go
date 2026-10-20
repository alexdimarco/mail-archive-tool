package schedule

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// InstalledEntry is one scheduler entry this install owns, found by sweeping our own
// namespaces rather than by a known name — so a full uninstall reaches even a schedule
// created from the CLI that the folder-only archive index never recorded (U-C3, SD-6,
// docs/design-archive-manager.md §5.8.1). Name is the scheduler entry name (DefaultName,
// a custom <name>, or <name>-verify); Out is the archive -out it targets when that is
// recoverable from the entry, else "".
type InstalledEntry struct {
	Name string
	Out  string
}

// OurInstalled enumerates every mailarchive-owned scheduler entry on this host by our own
// namespaces: on macOS the launchd labels com.dimarcotech.mailarchive.* , on other unix
// the crontab blocks tagged with our marker, on Windows the schtasks side-files beside
// the wrapper (DefaultWrapperPath's dir). It reads no archive and never resolves a
// descriptor path;
// uninstall removes each entry by name, so a schedule the index never recorded is still
// swept away (Query is exact-name-only and would miss it).
func OurInstalled() []InstalledEntry {
	switch runtime.GOOS {
	case "darwin":
		return ourLaunchd()
	case "windows":
		return ourSchtasks()
	default:
		return ourCron(readCrontab())
	}
}

// ourLaunchd lists our LaunchAgents plists (com.dimarcotech.mailarchive.<name>.plist).
func ourLaunchd() []InstalledEntry {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	ents, err := os.ReadDir(filepath.Join(home, "Library", "LaunchAgents"))
	if err != nil {
		return nil
	}
	prefix := LaunchdLabel("") // "com.dimarcotech.mailarchive."
	var out []InstalledEntry
	for _, e := range ents {
		n := e.Name()
		if e.IsDir() || !strings.HasPrefix(n, prefix) || !strings.HasSuffix(n, ".plist") {
			continue
		}
		if name := strings.TrimSuffix(strings.TrimPrefix(n, prefix), ".plist"); name != "" {
			out = append(out, InstalledEntry{Name: name})
		}
	}
	return out
}

// ourCron lists the crontab blocks tagged with our marker ("# mailarchive-backup" or
// "# mailarchive-backup <name>"), recovering each block's -out. A line that merely starts
// with the marker text but is a different token ("# mailarchive-backupX") is not ours.
func ourCron(tab string) []InstalledEntry {
	var out []InstalledEntry
	for _, line := range strings.Split(tab, "\n") {
		t := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(t, "# "+DefaultName)
		if !ok {
			continue
		}
		if rest != "" && !strings.HasPrefix(rest, " ") {
			continue
		}
		name := DefaultName
		if r := strings.TrimSpace(rest); r != "" {
			name = r
		}
		out = append(out, InstalledEntry{Name: name, Out: cronBlockOut(tab, t)})
	}
	return out
}

// ourSchtasks lists our Windows schtasks by the side-files Install always writes beside
// the wrapper (DefaultWrapperPath's dir; <name>.cmd / <name>.xml). The real side-file
// naming is confirmed on the Windows lab box; the glob logic is unit-tested on any OS
// via the wrapper-dir seam. The per-user-dir computation stays in DefaultWrapperPath
// (wrapper.go), which this only reads — no new state location (MA-621).
func ourSchtasks() []InstalledEntry {
	ents, err := os.ReadDir(filepath.Dir(DefaultWrapperPath("probe")))
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	var out []InstalledEntry
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		ext := filepath.Ext(e.Name())
		if ext != ".cmd" && ext != ".xml" {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ext)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		out = append(out, InstalledEntry{Name: name})
	}
	return out
}
