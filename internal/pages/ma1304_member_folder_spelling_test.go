package pages

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// covers: MA-1304, R45
// A member folder is put on a page only when each of its segments is what the exporter's name
// rule writes (the B3 adversarial pass's A2, turned from its probes
// TestB3AdvTrailingDotMemberFolderAliasesAnotherPage and TestB3AdvDotDotSpaceMemberFolder): a
// folder Windows spells another way ("INBOX." is INBOX's own folder there, ".. " is "..") gets no
// page, so INBOX's own page keeps its own email and the page build does not fail. Positive twin
// first: a member folder spelled as the exporter spells it ("Receipts") gets its page.
func TestAMemberFolderWindowsSpellsAnotherWayGetsNoPage(t *testing.T) {
	out, ix := memberArchive(t)
	// Receipt lives in All Mail; its member folders name the others.
	if err := ix.SetMembers("k-Receipt", "s", []string{"Receipts", "INBOX.", ".. ", "x/.. ", "CON", "Sent "}); err != nil {
		t.Fatal(err)
	}
	ix.Flush()
	if err := Generate(out, ix, log.New(io.Discard, "", 0), 0); err != nil {
		t.Fatalf("the page build failed: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(out, "s", "Receipts", "index.html")); err != nil || !strings.Contains(string(b), "Receipt") {
		t.Fatalf("positive twin: the member page for Receipts: %v", err)
	}
	// On Windows "INBOX." is INBOX's own folder: a member-only page for it would replace INBOX's.
	inbox, err := os.ReadFile(filepath.Join(out, "s", "INBOX", "index.html"))
	if err != nil || !strings.Contains(string(inbox), "Lunch") || strings.Contains(string(inbox), "Receipt") {
		t.Fatalf("INBOX's own page lost its own email, or lists the member of \"INBOX.\": %v", err)
	}
	for _, f := range []string{"INBOX.", ".. ", "x", "CON", "Sent "} {
		if runtime.GOOS == "windows" && strings.TrimRight(f, ". ") != f {
			continue // Windows finds INBOX (or the parent) by this spelling: the page check above stands
		}
		if _, err := os.Lstat(filepath.Join(out, "s", f)); err == nil {
			t.Fatalf("a page folder was made for the member folder %q", f)
		}
	}
	if root, _ := os.ReadFile(filepath.Join(out, "index.html")); strings.Contains(string(root), "INBOX.") {
		t.Fatal("the root page lists the member folder \"INBOX.\"")
	}
}
