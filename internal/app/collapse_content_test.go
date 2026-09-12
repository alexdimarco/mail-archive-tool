package app

import (
	"os"
	"path/filepath"
	"testing"
)

// covers: MA-236, R3, R1, S39
// The collapse's move-duplicate discriminator compares the PRESERVED .eml (raw
// wire bytes, folder-independent), NOT the rendered .html (which embeds the
// folder breadcrumb / depth-dependent paths / key-derived names and so differs
// between a message's two folder-copies). So a genuine move-duplicate whose
// two .html renderings differ but whose .eml is identical is judged the SAME
// (merged, no R3 duplicate); two distinct messages with different .eml are judged
// distinct (kept, no R1 drop); and with no .eml it falls back to merge.
func TestSameArchivedEMLComparesRawNotRenderedHTML(t *testing.T) {
	out := tmpDir(t)
	put := func(relHTML, html, eml string) {
		p := filepath.Join(out, filepath.FromSlash(relHTML))
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(html), 0o644); err != nil {
			t.Fatal(err)
		}
		if eml != "" {
			if err := os.WriteFile(p[:len(p)-len(".html")]+".eml", []byte(eml), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	// Move-duplicate: DIFFERENT rendered html, IDENTICAL raw .eml.
	put("s/Inbox/m.html", "<html>Inbox &raquo; m</html>", "RAW-M-BYTES")
	put("s/Archive/2024/m.html", "<html>Archive &raquo; 2024 &raquo; m</html>", "RAW-M-BYTES")
	if !sameArchivedEML(out, "s/Inbox/m.html", "s/Archive/2024/m.html") {
		t.Errorf("a move-duplicate (identical .eml, different rendered .html) was judged distinct — collapse would wrongly split it (R3)")
	}
	// Distinct reuse: different .eml.
	put("s/Inbox/a.html", "x", "RAW-A")
	put("s/Sent/b.html", "x", "RAW-B")
	if sameArchivedEML(out, "s/Inbox/a.html", "s/Sent/b.html") {
		t.Errorf("two distinct messages (different .eml) were judged the same — collapse would wrongly merge (R1)")
	}
	// No .eml (non-raw archive): fall back to merge (the common move-duplicate case).
	put("s/Inbox/n1.html", "x", "")
	put("s/Sent/n2.html", "y", "")
	if !sameArchivedEML(out, "s/Inbox/n1.html", "s/Sent/n2.html") {
		t.Errorf("with no .eml the comparator must fall back to merge (common move-duplicate)")
	}
}
