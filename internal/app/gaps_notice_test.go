package app

import (
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: R7, R13, S46
// `reindex -rebuild`'s from-HTML reader skips the record notice (fault-isolation
// §4.7.5): a partly-read page rebuilds with its own subject, sender, date and body
// words, recovers every core field, and the notice's words (and the hostile reason
// it quotes) never reach the rebuilt index.
func TestRebuildReaderSkipsTheRecordNotice(t *testing.T) {
	m := &model.Message{
		Subject: "Partly read", SenderName: "Ann", SenderEmail: "ann@example.com", To: "bob@example.com",
		Received: time.Date(2025, 3, 3, 9, 0, 0, 0, time.UTC), InternetMessageID: "<n@x>",
		PlainBody: "the words of the body", ReadGaps: []string{state.LabelUnreadableAttachments},
		ReadError: "needle-reason from a damaged store",
	}
	rr, err := export.RenderWith(m, export.RenderContext{Store: "old-pst", Folder: "Inbox"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(rr.HTML), "mailarchive-notice") || !strings.Contains(string(rr.HTML), "needle-reason") {
		t.Fatal("fixture: the page carries no notice")
	}
	got, unrecovered := readArchivedHTML(rr.HTML)
	if got == nil || unrecovered != 0 {
		t.Fatalf("rebuild read of a partly-read page: message %v, %d unrecovered fields; want it read in full", got, unrecovered)
	}
	if got.Subject != "Partly read" || got.SenderEmail != "ann@example.com" || got.Date().IsZero() {
		t.Errorf("rebuilt fields: subject %q sender %q date %v", got.Subject, got.SenderEmail, got.Date())
	}
	text := got.PlainBody + got.HTMLBody
	if !strings.Contains(text, "the words of the body") {
		t.Errorf("the body words were lost: %q", text)
	}
	for _, leak := range []string{"could only be partly read", "needle-reason", "Source: old-pst"} {
		if strings.Contains(text, leak) {
			t.Errorf("the notice reached the rebuilt index text: %q is in %q", leak, text)
		}
	}
}
