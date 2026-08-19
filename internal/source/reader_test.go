package source

import (
	"os"
	"testing"

	"mail-archive-tool/internal/model"
)

const fixture = "../../testdata/support.pst"

// covers: MA-16
func TestWalkFixture(t *testing.T) {
	if _, err := os.Stat(fixture); err != nil {
		t.Skipf("fixture missing: %v", err)
	}

	r, err := Open(fixture)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer r.Close()

	var count, withSubject, withAttachments, withHTML, withPlain int
	err = r.Walk(func(folderPath []string, m *model.Message) error {
		count++
		if m.Subject != "" {
			withSubject++
		}
		if len(m.Attachments) > 0 {
			withAttachments++
		}
		if m.HTMLBody != "" {
			withHTML++
		}
		if m.PlainBody != "" {
			withPlain++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}

	t.Logf("store=%q messages=%d withSubject=%d withHTML=%d withPlain=%d withAttachments=%d",
		r.StoreName(), count, withSubject, withHTML, withPlain, withAttachments)
	if count == 0 {
		t.Fatal("expected at least one message in fixture")
	}
}
