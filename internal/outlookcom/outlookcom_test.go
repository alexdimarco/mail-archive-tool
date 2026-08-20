package outlookcom

import (
	"path/filepath"
	"strings"
	"testing"
)

// covers: MA-58, R16, R4
// pstFileName turns an Outlook account display name into a safe .pst file name:
// always a bare .pst filename, never a path separator or traversal, with a
// stable fallback for an empty/unnameable account.
func TestPSTFileName(t *testing.T) {
	for _, in := range []string{"user@example.com", "Work Mailbox", "Bob's Archive"} {
		got := pstFileName(in)
		if !strings.HasSuffix(got, ".pst") {
			t.Errorf("pstFileName(%q)=%q, want a .pst suffix", in, got)
		}
		if filepath.Base(got) != got {
			t.Errorf("pstFileName(%q)=%q is not a bare filename", in, got)
		}
	}

	// Traversal / separators must be neutralized (R4 — containment).
	for _, bad := range []string{"../../etc/passwd", `..\..\win`, "a/b\\c"} {
		got := pstFileName(bad)
		if strings.ContainsAny(got, `/\`) || strings.Contains(got, "..") {
			t.Errorf("pstFileName(%q)=%q leaks a separator or traversal", bad, got)
		}
		if filepath.Base(got) != got {
			t.Errorf("pstFileName(%q)=%q is not a bare filename", bad, got)
		}
	}

	// An empty/unnameable account resolves to a stable fallback, not bare ".pst".
	if got := pstFileName(""); got == ".pst" || got == "" {
		t.Errorf("empty account should get a fallback name, got %q", got)
	}
}
