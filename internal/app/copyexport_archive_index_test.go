package app

import (
	"database/sql"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/index"
	"mail-archive-tool/internal/state"
)

// indexMeta is what the workspace's preview reads of each message from the archive's index, for the
// names and the manifest: every row of every folder, by manifest key.
func indexMeta(t *testing.T, s *ArchiveSource) map[string]index.ListRow {
	t.Helper()
	h, err := s.OpenList()
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	meta := map[string]index.ListRow{}
	for _, n := range s.Tree() {
		tok, folder, _ := s.Locate(n.ID)
		if err := h.EachInFolder(tok, folder, func(r index.ListRow) error {
			if k, ok := s.MapKey(h.Version, r); ok {
				meta[k] = r
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	return meta
}

// covers: MA-435, MA-436, R29, R20, S52
// One row of the index with a date past year 9999 (E4-AO4) does not make the export impossible: the
// run record is written as JSON, whose time cannot hold such a year, and the export used to be refused
// with "couldn't record this export (disk full or access denied)" - and again for every selection that
// held the record. The row reads as no date, the message carries the date its manifest record has, and
// the export completes.
func TestArchiveIndexRowWithAFarDateStillExports(t *testing.T) {
	a := buildCEArchive(t)
	e := newCEEnv(t)
	db, err := sql.Open("sqlite", filepath.Join(a.dir, "search.db"))
	if err != nil {
		t.Fatal(err)
	}
	for _, slug := range []string{"hello", "second"} {
		if _, err := db.Exec(`UPDATE docs SET date = 300000000000 WHERE key = ?`, a.key(slug)); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	a.setRec("hello", func(r *state.Record) { r.Date = "2025-03-03T09:00:00Z" }) // a record that carries its own date

	s := a.open()
	p, _ := a.planWith(e, s, allKeys(s), indexMeta(t, s))
	for _, m := range p.Messages {
		if m.Received.Year() > 9999 {
			t.Errorf("%q has the date %v: a date the run record cannot hold", m.Subject, m.Received)
		}
		if m.Subject == "hello" && m.Received.Year() != 2025 {
			t.Errorf("hello has the date %v: a row with no valid date takes its manifest record's", m.Received)
		}
		if m.Subject == "second" && !m.Received.IsZero() {
			t.Errorf("second has the date %v: a row with no valid date, and a record with none, has no date", m.Received)
		}
	}
	res, err := e.NewCopyRun(p, s).Run(t.Context())
	if err != nil {
		t.Fatalf("the export was refused: %v", err)
	}
	if res.Status != CopyStatusComplete || res.Counts.Exported != 6 {
		t.Errorf("the run: %+v", res)
	}
}
