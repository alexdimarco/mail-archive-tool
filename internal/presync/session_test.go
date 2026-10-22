package presync

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-552
// The pre-sync state machine's happy path (design 4.5), on a fake clock: S1..S9
// in order; the write-ahead record goes launching -> syncing -> quitting and is
// cleared at the end; a Thunderbird started hidden that fetches then goes quiet
// (the rev-3 rule) is asked to quit once and the run ends ok with the process
// gone.
func TestPresyncStateMachineHappyPath(t *testing.T) {
	base := time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)
	const app = "/Applications/Thunderbird.app/Contents/MacOS/thunderbird"
	const profile = "/Users/x/profile.default"
	statePath := filepath.Join(t.TempDir(), "presync-abcd.json")

	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true
	f.lockOK = true
	f.appearIn, f.exitIn = 4*time.Second, 2*time.Second
	f.changeAt = base.Add(24 * time.Second)
	f.dgBase, f.dgChanged = 1, 2
	f.nowT = base
	f.child = proc{pid: 200, exe: app, start: base.Add(1 * time.Second), args: "thunderbird -profile " + profile}

	d := deps{
		env:       f,
		cfg:       quietConfig{settle: 10 * time.Second, quiet: 30 * time.Second, maxWait: 600 * time.Second},
		app:       app,
		profile:   profile,
		watch:     []string{"/w"},
		statePath: statePath,
		lockPath:  "/l",
		base:      record{Version: recordVersion, Profile: profile, App: app, ToolPID: 1, ToolStart: 1},
		awaitPoll: 2 * time.Second,
		awaitUp:   90 * time.Second,
		pollEvery: 10 * time.Second,
		quitGrace: 120 * time.Second,
		idleAway:  120 * time.Second,
	}

	res := runSession(d)

	if want := []string{"S1", "S2", "S3", "S4", "S5", "S6", "S7", "S8", "S9"}; !eqStrings(res.states, want) {
		t.Errorf("state sequence = %v, want %v", res.states, want)
	}
	if want := []recordState{stateLaunching, stateSyncing, stateQuitting}; !eqRecordStates(res.records, want) {
		t.Errorf("record states = %v, want %v", res.records, want)
	}
	if res.outcome != OutcomeOK {
		t.Errorf("outcome = %q, want ok", res.outcome.Name)
	}
	if !f.quitted {
		t.Errorf("Thunderbird was never asked to quit")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("the state record was not cleared at the end: %v", err)
	}
}

// covers: MA-552
// The S4 gates each stop the session before any launch (design 4.5): the owner
// at the computer, an already-running Thunderbird, and a busy profile lock.
func TestPresyncStateMachineGatesStopBeforeLaunch(t *testing.T) {
	const app, profile = "/a/thunderbird", "/p"
	mk := func(f *fakeEnv) deps {
		return deps{
			env: f, app: app, profile: profile, watch: []string{"/w"},
			statePath: filepath.Join(t.TempDir(), "s.json"), lockPath: "/l",
			base:      record{Version: recordVersion},
			cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
			awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
		}
	}

	t.Run("owner at the computer -> skipped, never launched", func(t *testing.T) {
		f := newFakeEnv()
		f.lockOK = true
		f.idle, f.idleOK = 30, true // under the 2-minute threshold
		if res := runSession(mk(f)); res.outcome != OutcomeOwnerActiveSkipped || f.started {
			t.Errorf("want owner-active-skipped and no launch; got %q started=%v", res.outcome.Name, f.started)
		}
	})

	t.Run("already running -> not started, not quit", func(t *testing.T) {
		f := newFakeEnv()
		f.lockOK = true
		f.idle, f.idleOK = 300, true
		f.procs = []proc{{pid: 9, exe: "/x/thunderbird"}}
		if res := runSession(mk(f)); res.outcome != OutcomeAlreadyRunning || f.started || f.quitted {
			t.Errorf("want already-running, no launch/quit; got %q started=%v quitted=%v", res.outcome.Name, f.started, f.quitted)
		}
	})

	t.Run("profile lock busy -> profile-busy", func(t *testing.T) {
		f := newFakeEnv() // lockOK defaults false
		if res := runSession(mk(f)); res.outcome != OutcomeProfileBusy || f.started {
			t.Errorf("want profile-busy and no launch; got %q", res.outcome.Name)
		}
	})
}

// covers: MA-571
// When the launcher reports no GUI session (startHidden -> errNoDesktopSession),
// S5 ends the session presync_no_desktop_session, removes the write-ahead record
// and reports no launch — and the archive, as ever, still runs afterwards.
func TestPresyncNoDesktopSession(t *testing.T) {
	statePath := filepath.Join(t.TempDir(), "s.json")
	f := newFakeEnv()
	f.sm = true
	f.boot = 1000
	f.idle, f.idleOK = 300, true // owner away
	f.lockOK = true
	f.startErr = errNoDesktopSession
	f.nowT = time.Date(2026, 10, 22, 2, 0, 0, 0, time.UTC)

	d := deps{
		env: f, app: "/a/thunderbird", profile: "/p", watch: []string{"/w"},
		statePath: statePath, lockPath: "/l",
		base:      record{Version: recordVersion},
		cfg:       quietConfig{settle: time.Second, quiet: time.Second, maxWait: time.Minute},
		awaitPoll: time.Second, awaitUp: time.Minute, pollEvery: time.Second, quitGrace: time.Minute, idleAway: 120 * time.Second,
	}

	res := runSession(d)
	if res.outcome != OutcomeNoDesktopSession {
		t.Errorf("outcome = %q, want no-desktop-session", res.outcome.Name)
	}
	if f.started {
		t.Errorf("startHidden reported a launch despite the no-session failure")
	}
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Errorf("the write-ahead record was not removed: %v", err)
	}
}

func eqStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func eqRecordStates(a, b []recordState) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
