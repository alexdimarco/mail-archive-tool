package presync

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-565
// The write-ahead state record persists atomically at 0600, refuses to exceed
// its 4 KiB bound (rather than truncate), tolerates unknown fields, reads a
// newer version and corrupt JSON as unreadable, and holds no hostname or
// wall-clock deadline (PC10, PC11); its streak counters survive a restart and a
// missing file is the normal no-prior-session case.
func TestPresyncStateRecord(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "presync-abcd1234.json")

	t.Run("missing file is not an error", func(t *testing.T) {
		if _, ok, err := loadRecord(path); ok || err != nil {
			t.Fatalf("missing record: got (ok=%v, err=%v), want (false, nil)", ok, err)
		}
	})

	t.Run("round-trip at 0600 preserves streaks", func(t *testing.T) {
		in := record{Version: recordVersion, Boot: 111, Profile: "/p", App: "/a", State: stateSyncing,
			ToolPID: 7, ToolStart: 222, LaunchAt: 333, ChildPID: 9, ChildStart: 334, ChildArgs: "-profile /p",
			WarnStreak: 2, NoteStreak: 1, StreakSince: 100}
		if err := storeRecord(path, in); err != nil {
			t.Fatalf("store: %v", err)
		}
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		// The 0600 bits are the Unix owner-only mechanism. Windows does not model
		// them (it reports 0666); there owner-only is the %LOCALAPPDATA% directory
		// ACL instead (PC-W5), so assert the bits only where they carry meaning.
		if runtime.GOOS != "windows" && fi.Mode().Perm() != 0o600 {
			t.Errorf("mode = %v, want 0600", fi.Mode().Perm())
		}
		out, ok, err := loadRecord(path)
		if !ok || err != nil {
			t.Fatalf("load: (ok=%v, err=%v)", ok, err)
		}
		if out.WarnStreak != 2 || out.NoteStreak != 1 || out.State != stateSyncing || out.ChildArgs != "-profile /p" {
			t.Errorf("round-trip lost fields: %+v", out)
		}
	})

	t.Run("holds no host or deadline field", func(t *testing.T) {
		b, _ := json.Marshal(record{Version: recordVersion})
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		for _, banned := range []string{"host", "deadline"} {
			if _, present := m[banned]; present {
				t.Errorf("state record carries a %q field (PC10/PC11 forbid it)", banned)
			}
		}
	})

	t.Run("unknown fields are tolerated", func(t *testing.T) {
		p := filepath.Join(dir, "extra.json")
		if err := os.WriteFile(p, []byte(`{"version":1,"state":"idle","future_field":"x","warn_streak":5}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		r, ok, err := loadRecord(p)
		if !ok || err != nil {
			t.Fatalf("load with an unknown field: (ok=%v, err=%v)", ok, err)
		}
		if r.WarnStreak != 5 {
			t.Errorf("a known field was lost alongside an unknown one: %+v", r)
		}
	})

	t.Run("a newer version is unreadable", func(t *testing.T) {
		p := filepath.Join(dir, "newer.json")
		if err := os.WriteFile(p, []byte(`{"version":999,"state":"idle"}`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, ok, err := loadRecord(p); ok || !errors.Is(err, errUnreadable) {
			t.Errorf("newer version: got (ok=%v, err=%v), want (false, errUnreadable)", ok, err)
		}
	})

	t.Run("corrupt json is unreadable", func(t *testing.T) {
		p := filepath.Join(dir, "corrupt.json")
		if err := os.WriteFile(p, []byte(`{not json`), 0o600); err != nil {
			t.Fatalf("write: %v", err)
		}
		if _, ok, err := loadRecord(p); ok || !errors.Is(err, errUnreadable) {
			t.Errorf("corrupt: got (ok=%v, err=%v), want (false, errUnreadable)", ok, err)
		}
	})

	t.Run("an over-long record is refused, not truncated", func(t *testing.T) {
		big := record{Version: recordVersion, ChildArgs: strings.Repeat("x", recordMaxBytes+100)}
		if err := storeRecord(filepath.Join(dir, "big.json"), big); err == nil {
			t.Errorf("storing a record over the %d-byte bound should refuse, not write a truncated one", recordMaxBytes)
		}
	})
}
