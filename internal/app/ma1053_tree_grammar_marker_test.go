package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure/leak"
	"mail-archive-tool/internal/delmark"
	"mail-archive-tool/internal/encmark"
	"mail-archive-tool/internal/export"
)

// covers: MA-1053, MA-743, R37
// MA-743's tree grammar, pinned to encmark's reserved names, allows the delete marker
// at an encrypted archive's root (§15.7, MA-1053), at rest and in flight, since a
// delete writes it into the archive before it removes anything. Only a regular file
// there: a folder or a link at the name is still reported, and the name anywhere
// but the root is a file the archive does not hold. Positive twin first: the
// well-formed tree passes without it.
func TestTheTreeGrammarAllowsTheDeleteMarkerAtTheRoot(t *testing.T) {
	unit := "ab" + strings.Repeat("0", 30)
	good := func(t *testing.T) string {
		out := tmpDir(t)
		p14Write(t, filepath.Join(out, export.UnitsDir, "ab", unit), "sealed")
		p14Write(t, filepath.Join(out, ".mailarchive-manifest.json"), "{}")
		return out
	}
	if vs := leak.CheckTree(good(t), leak.AtRest, cleartext2bNames()); len(vs) != 0 {
		t.Fatalf("the positive twin is reported: %v", vs)
	}
	if !slices.Contains(encmark.ReservedNames(), delmark.Name) {
		t.Fatal("the grammar's root names and encmark's reserved names differ on the delete marker")
	}
	out := good(t)
	p14Write(t, filepath.Join(out, delmark.Name), `{"version":1}`)
	for _, mode := range []leak.Mode{leak.AtRest, leak.InFlight} {
		if vs := leak.CheckTree(out, mode, cleartext2bNames()); len(vs) != 0 {
			t.Fatalf("a regular delete marker at the root is reported (mode %d): %v", mode, vs)
		}
	}
	for label, plant := range map[string]func(t *testing.T, out string){
		"a folder at the name": func(t *testing.T, out string) {
			if err := os.Mkdir(filepath.Join(out, delmark.Name), 0o700); err != nil {
				t.Fatal(err)
			}
		},
		"the name in a shard": func(t *testing.T, out string) {
			p14Write(t, filepath.Join(out, export.UnitsDir, "ab", delmark.Name), "x")
		},
		"a link at the name": func(t *testing.T, out string) {
			target := filepath.Join(tmpDir(t), "x")
			p14Write(t, target, "x")
			if err := os.Symlink(target, filepath.Join(out, delmark.Name)); err != nil {
				t.Skip("no symlinks here:", err)
			}
		},
	} {
		t.Run(label, func(t *testing.T) {
			out := good(t)
			plant(t, out)
			if vs := leak.CheckTree(out, leak.AtRest, cleartext2bNames()); len(vs) == 0 {
				t.Fatalf("%s is not reported", label)
			}
		})
	}
}
