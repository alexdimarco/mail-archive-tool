package presync

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-562
// deriveProfile picks the single profiles.ini-listed profile that contains an
// archive input, so -thunderbird-sync works without an explicit -thunderbird-profile
// (§4.3); it fails safe (no derivation → presync_profile_unusable) when no listed
// profile contains an input or when inputs span more than one, never guessing a
// profile to sync or quit.
func TestDeriveProfile(t *testing.T) {
	canon := func(p string) string { c, _ := filepath.EvalSymlinks(p); return c }
	base := t.TempDir()
	profA := filepath.Join(base, "a.default")
	profB := filepath.Join(base, "b.default")
	inA := filepath.Join(profA, "Mail", "Local Folders")
	inB := filepath.Join(profB, "ImapMail")
	for _, d := range []string{filepath.Join(inA, "x", "y"), inB} {
		if err := os.MkdirAll(d, 0o755); err != nil { // creates inA and profA too
			t.Fatal(err)
		}
	}
	listed := []string{canon(profA), canon(profB)}

	if p, ok := deriveProfile([]string{inA}, listed); !ok || p != canon(profA) {
		t.Errorf("one input under a.default: derived (%q,%v), want (%q,true)", p, ok, canon(profA))
	}
	if p, ok := deriveProfile([]string{filepath.Join(inA, "x", "y")}, listed); !ok || p != canon(profA) {
		t.Errorf("a deeper input under a.default was not derived: (%q,%v)", p, ok)
	}
	if p, ok := deriveProfile([]string{t.TempDir()}, listed); ok {
		t.Errorf("input in no listed profile: derived %q, want none", p)
	}
	if p, ok := deriveProfile([]string{inA, inB}, listed); ok {
		t.Errorf("inputs spanning two listed profiles: derived %q, want none (ambiguous)", p)
	}
	if _, ok := deriveProfile(nil, listed); ok {
		t.Error("no inputs: derived a profile, want none")
	}
}
