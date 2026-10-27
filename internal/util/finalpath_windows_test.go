//go:build windows

package util

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-849, R37
// A Windows junction is resolved to where it leads, as a symbolic link is (P3-8
// Windows lab, B2): a folder reached through a junction into a synced folder is
// judged synced, so an unencrypted copy written through it asks twice (K5) — Go's
// EvalSymlinks has not followed junctions since 1.23. A path not made yet under the
// junction resolves through it too. Positive twin: the synced folder named directly.
func TestAJunctionIsResolvedToWhereItLeads(t *testing.T) {
	base := t.TempDir()
	synced := filepath.Join(base, "Dropbox", "Mail")
	os.MkdirAll(synced, 0o755)
	link := filepath.Join(base, "Plain")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, synced).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J: %v: %s", err, out)
	}
	if _, direct := UnderCloudSync(ResolveExisting(synced)); !direct {
		t.Fatal("positive twin: the synced folder named directly")
	}
	through := ResolveExisting(filepath.Join(link, "copy"))
	_, viaJunction := UnderCloudSync(through)
	assure.Reached(t, viaJunction && strings.EqualFold(filepath.Dir(through), ResolveExisting(synced)),
		"the junction leads into the synced folder: "+through)
}
