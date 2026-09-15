package state

import (
	"path/filepath"
	"testing"
	"time"
)

// covers: MA-240, R5, R12, S39
// Manifest format v6 adds Record.PhysID (the per-physical-message hint): it
// round-trips across save+load, and a pre-v6 record loads with an EMPTY PhysID
// (so the live path degrades to the Message-ID floor until a run backfills it).
func TestV6PhysIDRoundTrip(t *testing.T) {
	t1 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	withID := LiveKey("mbox", "mid:<p@x>")
	legacy := LiveKey("mbox", "mid:<l@x>")
	path := filepath.Join(t.TempDir(), "v6.json")
	writeFullManifest(t, path, 6, map[string]Record{
		withID: {Path: "mbox/Inbox/p.html", Folder: "Inbox", ExportedAt: t1, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true,
			Fingerprint: "pppppppppppppppp", FpScheme: FpSchemeCurrent, PhysID: "IMMUTABLE-P"},
		legacy: {Path: "mbox/Inbox/l.html", Folder: "Inbox", ExportedAt: t1, FirstFolder: "Inbox", FirstSeen: t1, LastSeen: t1, Present: true,
			Fingerprint: "llllllllllllllll", FpScheme: FpSchemeCurrent}, // no PhysID
	})
	m, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if r, _ := m.Get(withID); r.PhysID != "IMMUTABLE-P" {
		t.Errorf("PhysID did not load: %q", r.PhysID)
	}
	if r, _ := m.Get(legacy); r.PhysID != "" {
		t.Errorf("a record with no PhysID loaded as %q, want empty", r.PhysID)
	}
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	m2, _ := Load(path)
	if r, _ := m2.Get(withID); r.PhysID != "IMMUTABLE-P" {
		t.Errorf("PhysID did not survive save+load: %q", r.PhysID)
	}
}
