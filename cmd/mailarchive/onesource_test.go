package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// moduleRoot walks up from the test's working directory to the module root (the
// directory holding go.mod), so a tree-walking invariant can scan the whole repo.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}

// covers: MA-621, S59
// No new per-user state location: the set of non-test files that compute a
// per-user state directory (os.UserConfigDir, LOCALAPPDATA, XDG_STATE_HOME)
// equals the set pinned at the single-executable move. A planted new call site
// is flagged, so a new on-disk state location cannot slip in without an operator
// decision (R33). When a file here legitimately changes, the pin is restated in
// that same commit — never widened to make the test pass.
func TestMA621_NoNewPerUserStateLocation(t *testing.T) {
	pinned := map[string]bool{
		"cmd/mailarchive/main.go":                     true,
		"internal/app/run.go":                         true,
		"internal/graphconfig/config.go":              true,
		"internal/graphconfig/secretstore_default.go": true,
		"internal/job/job.go":                         true,
		"internal/schedule/wrapper.go":                true,
		// fix/review's single-exe foundation adds two legitimate per-user-state
		// dirs already present at this move: the instance file (§4.5.3) and the
		// workspace state dir. Pinned here — not new locations SE1 introduced.
		"internal/instance/instance.go": true,
		"internal/desktop/ws_util.go":   true,
	}
	re := regexp.MustCompile(`UserConfigDir|LOCALAPPDATA|XDG_STATE_HOME`)
	root := moduleRoot(t)
	found := map[string]bool{}
	for _, base := range []string{"cmd", "internal"} {
		err := filepath.WalkDir(filepath.Join(root, base), func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			data, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			if re.Match(data) {
				rel, _ := filepath.Rel(root, path)
				found[filepath.ToSlash(rel)] = true
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", base, err)
		}
	}
	if len(found) == 0 {
		t.Fatal("the walk found no per-user-state files — it is not scanning the tree")
	}
	for f := range found {
		if !pinned[f] {
			t.Errorf("NEW per-user-state computation in %s — not in the pinned set; a new on-disk state location needs an operator decision, not a widened pin (R33, MA-621)", f)
		}
	}
	for f := range pinned {
		if !found[f] {
			t.Errorf("pinned per-user-state file %s no longer computes one — restate the pin in this same commit", f)
		}
	}
}
