package presyncjob

import (
	"os"
	"path/filepath"
	"time"
)

// attendedMarker is the file, in the pre-sync state folder, that records that an
// attended Run now on this computer has started Thunderbird (design-presync-attended
// §4.6, C-83). The Desktop shows its one-time "New: Run now starts Thunderbird…"
// line beside Run now until it exists. It holds only the date it was written.
const attendedMarker = "attended-sync-started"

// AttendedSyncSeen reports whether an attended Run now has started Thunderbird on
// this computer (the marker exists). With no state folder it answers true: there is
// no sync to announce, so the line is not shown.
func AttendedSyncSeen() bool {
	stateDirMu.Lock()
	dirFn := stateDirFn
	stateDirMu.Unlock()
	dir, err := dirFn()
	if err != nil {
		return true
	}
	_, err = os.Stat(filepath.Join(dir, attendedMarker))
	return err == nil
}

// markAttendedStarted writes the marker, owner-only, when an attended session
// reaches S5 (it reports `starting`). Best effort: a marker that cannot be written
// only means the line shows again next time.
func markAttendedStarted(stateDir string) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return
	}
	p := filepath.Join(stateDir, attendedMarker)
	if _, err := os.Stat(p); err == nil {
		return
	}
	_ = os.WriteFile(p, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o600)
}
