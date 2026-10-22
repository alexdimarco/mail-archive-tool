package presync

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"mail-archive-tool/internal/util"
)

// recordState is the phase a session had reached when the record was last
// written (design section 4.6). A transition is written BEFORE it acts, so a
// crash always leaves a record that over-states what happened, and recovery is
// conservative.
type recordState string

const (
	stateLaunching   recordState = "launching"
	stateSyncing     recordState = "syncing"
	stateQuitting    recordState = "quitting"
	stateLeftRunning recordState = "left-running"
	stateIdle        recordState = "idle"
)

// recordVersion is the on-disk schema version. A record whose version is higher
// is read as unreadable: this binary is older than the one that wrote it.
const recordVersion = 1

// recordMaxBytes bounds the state record (design sections 4.6 / 4.11).
// storeRecord refuses a larger one rather than truncating it.
const recordMaxBytes = 4 * 1024

// record is the write-ahead state record (design section 4.6): what a session
// launched, so a later run can recover by ANNOUNCING a leftover — never quitting
// it, in v1. It deliberately holds NO hostname and NO wall-clock deadline: the
// directory is local, so a network-driven hostname change would only strand a
// record, and a wall-clock deadline is what a clock step or a DST change breaks
// (PC10, PC11).
type record struct {
	Version     int         `json:"version"`
	Boot        int64       `json:"boot"` // boot time, unix seconds — a reboot clears the record
	Profile     string      `json:"profile"`
	App         string      `json:"app"`
	State       recordState `json:"state"`
	ToolPID     int         `json:"tool_pid"`
	ToolStart   int64       `json:"tool_start"`
	LaunchAt    int64       `json:"launch_at"`
	ChildPID    int         `json:"child_pid,omitempty"`
	ChildStart  int64       `json:"child_start,omitempty"` // UTC unix seconds (PC11)
	ChildArgs   string      `json:"child_args,omitempty"`  // the -profile argument
	WarnStreak  int         `json:"warn_streak"`
	NoteStreak  int         `json:"note_streak"`
	StreakSince int64       `json:"streak_since,omitempty"`
}

// errUnreadable marks a record that must be treated as absent and warned once
// (presync_state_unreadable): corrupt JSON, an over-long file, or a newer
// version this binary does not understand.
var errUnreadable = errors.New("presync: state record unreadable")

// storeRecord writes r atomically at mode 0600 (temp + fsync + rename, so a
// crash leaves the old record or the new one, never a torn one). It refuses a
// record that would exceed the size bound rather than truncating it.
func storeRecord(path string, r record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return err
	}
	if len(b) > recordMaxBytes {
		return fmt.Errorf("presync: state record is %d bytes, over the %d-byte bound", len(b), recordMaxBytes)
	}
	return util.WriteFileAtomic0600(path, b)
}

// loadRecord reads the record at path. A missing file is (zero, false, nil) —
// the normal no-prior-session case. Corrupt JSON, an over-long file or a newer
// version is errUnreadable (treated as absent, warned once). Unknown fields are
// tolerated, so a newer minor writer's extra fields never strand an older reader.
func loadRecord(path string) (record, bool, error) {
	// ReadSecureFile Lstats first and opens O_NOFOLLOW|O_NONBLOCK, capping the
	// read with an io.LimitReader: a planted FIFO/symlink/device or an oversized
	// file is refused WITHOUT blocking or over-allocating (R32), the way the
	// sibling lockfile already hardens its read.
	b, err := util.ReadSecureFile(path, "pre-sync state record", recordMaxBytes)
	if errors.Is(err, fs.ErrNotExist) {
		return record{}, false, nil // the normal no-prior-session case
	}
	if err != nil {
		// Non-regular, over-long, over-permissive, or otherwise unreadable: treat
		// as absent and warn once.
		return record{}, false, errUnreadable
	}
	var r record
	if err := json.Unmarshal(b, &r); err != nil {
		return record{}, false, errUnreadable
	}
	if r.Version > recordVersion {
		return record{}, false, errUnreadable
	}
	return r, true, nil
}
