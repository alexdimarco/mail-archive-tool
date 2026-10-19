package app

import (
	"strings"
	"testing"
	"time"
)

// covers: MA-417, R4, R29
// EW-AO1 tombstone. A subject of 60 four-byte letters (math-bold alphanumerics, CJK
// extension B) gave a 269-byte message file name: the slug was bounded in runes and the
// path budget counts UTF-16 units, so nothing kept the name within the 255-byte
// NAME_MAX of ext4, xfs, btrfs, tmpfs and NFS, and five such messages in a row stopped
// the export as R-g. The namer now bounds the whole file name in bytes too. The
// ordinary subject next to it is the positive twin: its name keeps the full slug.
func TestCopyExportNamesStayWithinNameMax(t *testing.T) {
	const nameMax = 255
	hostile := []string{
		strings.Repeat("\U0001D5E6", 60), // MATHEMATICAL SANS-SERIF BOLD CAPITAL S
		strings.Repeat("\U00020000", 80), // CJK extension B
		strings.Repeat("\U0001D4B6", 200),
	}
	for _, subj := range hostile {
		n := newCeNamer(true, "/home/u/Documents/MailExport-20260304-ab12")
		m := ceMsgs(1)[0]
		m.Subject = subj
		m.Received = time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)
		pl := n.place(&m, 1)
		name := pl.Stem + ".eml"
		if len(name) > nameMax {
			t.Errorf("subject of %d runes gave a %d-byte file name %q: over NAME_MAX %d", len([]rune(subj)), len(name), name, nameMax)
		}
		if !strings.HasSuffix(name, ".eml") || !strings.HasPrefix(name, "2026-03-04_0506_") {
			t.Errorf("the name lost its date or extension: %q", name)
		}
	}

	// Positive twin: an ASCII subject keeps its full 60-rune slug.
	n := newCeNamer(true, "/home/u/Documents/MailExport-20260304-ab12")
	m := ceMsgs(1)[0]
	m.Subject = strings.Repeat("a", 80)
	m.Received = time.Date(2026, 3, 4, 5, 6, 0, 0, time.UTC)
	pl := n.place(&m, 1)
	if !strings.Contains(pl.Stem, strings.Repeat("a", 60)) || strings.Contains(pl.Stem, strings.Repeat("a", 61)) {
		t.Errorf("an ordinary subject lost its slug: %q", pl.Stem)
	}
}
