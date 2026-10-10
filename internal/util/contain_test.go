package util

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// covers: MA-375, R12, S45
// The shared containment helper places a path that does not exist yet through
// its deepest existing ancestor, symlinks resolved, so a credential file a run is
// about to create under -out — directly, or through a symlinked parent — is found
// inside it [CC16]. Containment is component-wise: a sibling that only shares a
// name prefix is outside.
func TestContainmentResolvesTheDeepestExistingAncestor(t *testing.T) {
	tmp := t.TempDir()
	out := filepath.Join(tmp, "archive")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	realOut := ResolveExisting(out)

	// A file under -out whose parent does not exist yet.
	deep := filepath.Join(out, "not", "yet", "graph.token")
	if !Within(realOut, ResolveExisting(deep)) {
		t.Errorf("%s (parent not created yet) is not placed inside %s", deep, realOut)
	}
	if ok, why := Overlap(deep, out); !ok || why != "is inside" {
		t.Errorf("Overlap(%s, %s) = %v %q, want true \"is inside\"", deep, out, ok, why)
	}

	// The same file reached through a symlinked parent of -out.
	link := filepath.Join(tmp, "link")
	if err := os.Symlink(out, link); err != nil {
		t.Logf("symlinks unavailable here, skipping the symlinked-parent case: %v", err)
	} else {
		viaLink := filepath.Join(link, "not", "yet", "graph.token")
		if !Within(realOut, ResolveExisting(viaLink)) {
			t.Errorf("%s (through a symlinked parent) is not placed inside %s", viaLink, realOut)
		}
		if ok, why := Overlap(link, out); !ok || why != "is the same directory as" {
			t.Errorf("Overlap(link, out) = %v %q, want true \"is the same directory as\"", ok, why)
		}
	}

	// Component-wise: a sibling sharing the name prefix is outside, and an
	// ancestor contains.
	if Within(realOut, ResolveExisting(filepath.Join(tmp, "archive2", "graph.token"))) {
		t.Errorf("a sibling archive2/ was placed inside archive/ (string-prefix match)")
	}
	if ok, why := Overlap(tmp, out); !ok || why != "contains" {
		t.Errorf("Overlap(parent, out) = %v %q, want true \"contains\"", ok, why)
	}
	if ok, _ := Overlap(filepath.Join(tmp, "elsewhere"), out); ok {
		t.Errorf("an unrelated folder overlaps the archive")
	}
}

// covers: MA-375, R12, S45
// Containment folds case on Windows and macOS, whose file systems are
// case-insensitive by default, so an -out spelled differently from the
// credential's path still contains it; elsewhere case is significant.
func TestWithinFoldsCaseOnWindowsAndMacOS(t *testing.T) {
	parent := t.TempDir()
	base := filepath.Join(parent, "Archive")
	target := filepath.Join(parent, "archive", "graph.token")
	for _, goos := range []string{"darwin", "windows"} {
		if !within(goos, base, target) {
			t.Errorf("%s: %s is not within %s (case must fold)", goos, target, base)
		}
	}
	// filepath.Rel itself folds case on a Windows host, so the case-sensitive
	// half can only be shown elsewhere.
	if runtime.GOOS != "windows" && within("linux", base, target) {
		t.Errorf("linux: %s is within %s although the case differs", target, base)
	}
}
