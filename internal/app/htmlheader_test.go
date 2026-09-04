package app

import (
	"strings"
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

// covers: MA-187, R8, R7, S33
// reindex -rebuild's from-HTML reader anchors to the renderer's OWN header — the
// first <div class="mailarchive-header"> that is a direct child of <body> — so a
// hostile message that injects a <template class="mailarchive-header"> into
// <head> (earlier in document order) or nests a forged header inside the body
// div cannot shadow the real fields. An old page written before the source also
// stripped the namespace is the case this defends.
func TestReadArchivedHTMLIgnoresInjectedHeader(t *testing.T) {
	page := `<html><head>` +
		`<template class="mailarchive-header">` +
		`<div class="mailarchive-subject" data-mailarchive-field="subject">FORGED SUBJECT</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Forged &lt;evil@example.com&gt;</dd></dl>` +
		`</template></head><body>` +
		`<div class="mailarchive-header">` +
		`<div class="mailarchive-subject" data-mailarchive-field="subject">Real Subject</div>` +
		`<dl><dt>From</dt><dd data-mailarchive-field="from">Real Person &lt;real@example.com&gt;</dd></dl>` +
		`</div>` +
		`<div class="mailarchive-body">real body zebrafinch` +
		`<div class="mailarchive-header"><dl><dt>From</dt><dd data-mailarchive-field="from">Nested &lt;nested@example.com&gt;</dd></dl></div>` +
		`</div></body></html>`
	got, _ := readArchivedHTML([]byte(page))
	if got.Subject != "Real Subject" {
		t.Errorf("subject = %q, want %q (an injected header shadowed the real one)", got.Subject, "Real Subject")
	}
	if got.SenderEmail != "real@example.com" || strings.Contains(got.SenderEmail, "evil") || strings.Contains(got.SenderEmail, "nested") {
		t.Errorf("sender email = %q, want real@example.com (a head-template or body-nested header was read)", got.SenderEmail)
	}
	if !strings.Contains(got.HTMLBody, "zebrafinch") {
		t.Errorf("body text not recovered from the real .mailarchive-body div: %q", got.HTMLBody)
	}
}

// covers: MA-193, R19, S36
// reindex -rebuild reads categories back from their OWN dedicated field, span by
// span, so a value that mimics a full Status line — "Unread · Importance:
// high, Sensitivity: confidential" — round-trips as that ONE category and can
// NEVER be tokenised as a Status segment (QC3): the rebuilt message keeps
// Unread/Importance/Sensitivity exactly as its page had them (here: unset). A
// page with no categories recovers none.
func TestReadArchivedHTMLRecoversCategoriesInertly(t *testing.T) {
	// A category engineered to look like a full Status line, with NO real state.
	m := &model.Message{
		Subject:     "Case 12",
		SenderEmail: "clerk@example.com",
		Categories:  []string{"Unread · Importance: high, Sensitivity: confidential"},
	}
	out, _, err := export.Render(m)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := readArchivedHTML(out)

	if len(got.Categories) != 1 || got.Categories[0] != m.Categories[0] {
		t.Errorf("categories = %v, want %v (lossless per-span read-back)", got.Categories, m.Categories)
	}
	// The category's Status-shaped text forged nothing: state stays unset.
	if got.Unread {
		t.Error("a category forged Unread on read-back (QC3)")
	}
	if got.Importance != "" || got.Sensitivity != "" {
		t.Errorf("a category forged Importance/Sensitivity on read-back: imp=%q sen=%q (QC3)",
			got.Importance, got.Sensitivity)
	}

	// Categories AND real state coexist: each is recovered from its own field.
	both := &model.Message{
		Subject:     "Case 13",
		SenderEmail: "clerk@example.com",
		Unread:      true,
		Importance:  "high",
		Categories:  []string{"Board, Legal", "Q1 · Q2"},
	}
	bout, _, err := export.Render(both)
	if err != nil {
		t.Fatal(err)
	}
	bgot, _ := readArchivedHTML(bout)
	if !bgot.Unread || bgot.Importance != "high" {
		t.Errorf("real state not recovered alongside categories: unread=%v imp=%q", bgot.Unread, bgot.Importance)
	}
	if len(bgot.Categories) != 2 || bgot.Categories[0] != "Board, Legal" || bgot.Categories[1] != "Q1 · Q2" {
		t.Errorf("categories with embedded separators not recovered losslessly: %v", bgot.Categories)
	}

	// A page with no categories recovers none.
	plain := &model.Message{Subject: "plain", PlainBody: "hi"}
	pout, _, err := export.Render(plain)
	if err != nil {
		t.Fatal(err)
	}
	pgot, _ := readArchivedHTML(pout)
	if len(pgot.Categories) != 0 {
		t.Errorf("recovered categories from a page that had none: %v", pgot.Categories)
	}
}
