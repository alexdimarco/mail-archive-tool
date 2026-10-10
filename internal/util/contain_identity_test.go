package util

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-375, R12, S45
// Containment compares places, not spellings: a folder reached under a spelling
// the file system treats as the same one (Unicode normalization on APFS and
// HFS+, case on a case-insensitive mount of any OS) is the same folder, so a
// credential under it is inside the archive, and a not-yet-created path under
// it too. The spelling is probed on the machine: where the file system keeps the
// two spellings apart there is nothing to alias and the case is skipped.
// Positive twin: a sibling folder is outside under every spelling.
// (Adversarial pass AC3.)
func TestContainmentIsByFileIdentityNotSpelling(t *testing.T) {
	base := ResolveExisting(t.TempDir())
	nfc := "Arché"  // é as one code point
	nfd := "Arché" // e + combining acute
	if err := os.Mkdir(filepath.Join(base, nfc), 0o755); err != nil {
		t.Fatal(err)
	}
	sibling := filepath.Join(base, "Other")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	spellings := map[string]string{}
	if _, err := os.Stat(filepath.Join(base, nfd)); err == nil {
		spellings["normalization"] = nfd
	}
	if _, err := os.Stat(filepath.Join(base, "arché")); err == nil {
		spellings["case"] = "arché"
	}
	if len(spellings) == 0 {
		t.Skip("this file system keeps every spelling of the folder apart")
	}
	for kind, alias := range spellings {
		inside := filepath.Join(base, nfc)
		viaAlias := filepath.Join(base, alias)
		for _, tc := range []struct{ name, a, b string }{
			{"an existing file", filepath.Join(inside, "graph.token"), viaAlias},
			{"a file not created yet", filepath.Join(viaAlias, "graph.token"), inside},
			{"the folders themselves", viaAlias, inside},
		} {
			if tc.name == "an existing file" {
				if err := os.WriteFile(tc.a, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if ok, _ := Overlap(tc.a, tc.b); !ok {
				t.Errorf("%s alias, %s: Overlap(%q, %q) = false, want true (the same folder)", kind, tc.name, tc.a, tc.b)
			}
		}
		if ok, how := Overlap(filepath.Join(viaAlias, "graph.token"), sibling); ok {
			t.Errorf("%s alias: a sibling folder overlapped (%s)", kind, how)
		}
	}
}
