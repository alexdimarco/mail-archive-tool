package app

import (
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
)

// covers: MA-177, S35
// The renderer's "Status" line is reversible: reindex -rebuild's from-HTML
// reader recovers Unread/Importance/Sensitivity from the archived page (PC17),
// so a rebuilt model matches the page it came from. These fields are not
// indexed and not part of identity, so the read-back changes no search result —
// it only keeps the rebuilt model faithful.
func TestReadArchivedHTMLRecoversStatus(t *testing.T) {
	m := &model.Message{
		Subject:     "Board memo",
		SenderName:  "Alice",
		SenderEmail: "alice@example.com",
		Unread:      true,
		Importance:  "high",
		Sensitivity: "confidential",
	}
	out, _, err := export.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readArchivedHTML(out)
	if !got.Unread {
		t.Error("Unread not recovered from the Status row")
	}
	if got.Importance != "high" {
		t.Errorf("Importance recovered = %q, want high", got.Importance)
	}
	if got.Sensitivity != "confidential" {
		t.Errorf("Sensitivity recovered = %q, want confidential", got.Sensitivity)
	}

	// A page with no state has no Status row, so nothing is recovered.
	plain := &model.Message{Subject: "Ordinary", PlainBody: "hi"}
	pout, _, err := export.Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	pgot, _ := readArchivedHTML(pout)
	if pgot.Unread || pgot.Importance != "" || pgot.Sensitivity != "" {
		t.Errorf("recovered state from a stateless page: unread=%v imp=%q sen=%q",
			pgot.Unread, pgot.Importance, pgot.Sensitivity)
	}
}
