package testhome

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// homeVars name this computer's home, settings, cache and state folders on macOS, Linux and
// Windows, as each package's TestMain points them.
var homeVars = []string{"HOME", "USERPROFILE", "XDG_CONFIG_HOME", "XDG_CACHE_HOME", "XDG_STATE_HOME", "APPDATA", "LOCALAPPDATA"}

func pointAt(t *testing.T, dir string) {
	t.Helper()
	for _, v := range homeVars {
		t.Setenv(v, dir)
	}
}

// runs counts the times a test run was started.
type runs struct{ n int }

func (r *runs) Run() int { r.n++; return 0 }

// covers: MA-1327, R37
// A test package's guard against this computer's real settings (the lead's follow-up to the B3
// adversarial pass, round 2): Contained passes only while MailArchive's settings folder resolves
// inside the throw-away folder, and it resolves the path without opening it; Run then runs the
// tests, and otherwise runs none and fails. Positive twin first: with the folders pointed at a
// throw-away folder, Contained passes and Run runs the tests once. Pointed elsewhere (as the real
// home would be), it fails naming both folders, runs no test, and nothing is created or read.
func TestTheGuardFailsWhenTheSettingsFolderIsOutsideTheThrowAwayFolder(t *testing.T) {
	inside, elsewhere := t.TempDir(), t.TempDir()
	pointAt(t, inside)
	if err := Contained(inside); err != nil {
		t.Fatalf("positive twin: %v", err)
	}
	r := &runs{}
	if Run(inside, r) != 0 || r.n != 1 || Check(inside, 3) != 3 {
		t.Fatalf("positive twin: a contained run did not run once with its own exit code (runs %d)", r.n)
	}

	pointAt(t, elsewhere) // the settings folder now resolves outside the throw-away folder
	err := Contained(inside)
	msg, rc := "", 0
	if err != nil {
		msg, rc = err.Error(), 2
	}
	r = &runs{}
	assure.Refused(t, rc, msg, assure.Names(inside, elsewhere, "real settings"),
		assure.NoSideEffect(func() bool {
			a, _ := os.ReadDir(inside)
			b, _ := os.ReadDir(elsewhere)
			return len(a) == 0 && len(b) == 0 && Run(inside, r) != 0 && r.n == 0
		}))
	// A sibling whose name only begins like the throw-away folder is outside it too.
	pointAt(t, inside+"-sibling")
	if Contained(inside) == nil {
		t.Fatalf("a folder beside %s counts as inside it", filepath.Base(inside))
	}
}
