//go:build windows

package presync

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-552
// The Windows run wiring: runWith threads the resolved plan into the shared
// S1..S9 session and reports its Outcome. With the owner active at S4, the session
// ends presync_owner_active_skipped WITHOUT launching — proving resolve → session
// → outcome wiring on Windows over a fully faked win32 seam (no GUI, no launch).
func TestWinRunWiringOwnerActiveSkips(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "Mozilla Thunderbird", "thunderbird.exe")
	mkFile(t, exe)
	prof := t.TempDir()
	mkFile(t, filepath.Join(prof, "prefs.js"))

	e := &winEnv{
		w: fakeWin32{
			idleOK: true, idle: 0, // owner active (idle below the away threshold)
			boot: 1000,
		},
		clock:    time.Now,
		appCands: func() []string { return []string{exe} },
		roaming:  func() (string, bool) { return "", false },
	}
	cfg := Config{Profile: prof, ProfileNamed: true, StateDir: t.TempDir()}

	res := runWith(context.Background(), cfg, e, sessionCtl{})
	if res.Release != nil {
		// The session holds the profile lock until the caller releases it (C2); on
		// Windows an open lock file cannot be removed by the temp-dir cleanup.
		defer res.Release()
	}
	if res.Outcome != OutcomeOwnerActiveSkipped {
		t.Fatalf("runWith outcome = %v, want presync_owner_active_skipped", res.Outcome)
	}
	// Nothing was launched, so no write-ahead record remains in the state dir.
	entries, _ := os.ReadDir(cfg.StateDir)
	for _, d := range entries {
		if filepath.Ext(d.Name()) == ".json" {
			t.Errorf("a state record %q was left though nothing launched", d.Name())
		}
	}
}
