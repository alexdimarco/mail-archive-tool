package desktop

import (
	"net/http"
	"regexp"
	"strings"
	"testing"
)

// The extraction workspace's slice E6, the desktop's half of its friction review (docs/review-extraction-workspace-e6-friction.md):
// what the owner reads on Export activity and on the workspace page about Extract attachments.

// withAttachment makes every message of the folder carry one attachment, report.pdf.
func (e *wsEnv) withAttachment(folder string) {
	e.fg.mutate(func() {
		for _, m := range e.fg.folder(folder).Msgs {
			id := m.ID
			e.fg.valueFn[id] = func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte("From: a@example.com\r\nSubject: with a file\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\nMIME-Version: 1.0\r\n" +
					"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n--B\r\nContent-Type: text/plain\r\n\r\nsee the file\r\n--B\r\n" +
					"Content-Type: application/pdf; name=\"report.pdf\"\r\nContent-Disposition: attachment; filename=\"report.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\nSGVsbG8gd29ybGQ=\r\n--B--\r\n"))
			}
		}
	})
}

// covers: MA-424, MA-533, MA-430, R12
// F3, F11, F1 (friction review of EW-E6): an export's card on Export activity says in a line what became of
// the attachments and in which way they were made, so two exports of the same folder, one with a zip per
// message and one with loose files, are told apart without opening either folder; a run without Extract
// attachments has no such line; and the page's script draws the line and the warning of a message that was
// exported without all of its attachments.
func TestExportActivityCardsSayWhetherAttachmentsAreZippedOrLoose(t *testing.T) {
	e := newWsEnv(t)
	ids := e.folders()
	e.withAttachment("F-SENT")
	run := func(attach bool, as string) map[string]any {
		t.Helper()
		sel := e.selection(entry(ids["Sent Items"], false))
		sel["options"] = map[string]any{"preserve": true, "html": false, "attachments": attach, "attachmentsAs": as}
		id := e.startExport(sel)
		e.waitRun()
		return e.card(id)
	}
	zipCard, looseCard, offCard := run(true, "zip"), run(true, "loose"), run(false, "zip")
	zipLine, _ := zipCard["attachLine"].(string)
	looseLine, _ := looseCard["attachLine"].(string)
	if !regexp.MustCompile(`^Attachments, one zip per message: \d+ attachments? in \d+ zips?\.$`).MatchString(zipLine) {
		t.Errorf("the zip run's card says %q", zipLine)
	}
	if !regexp.MustCompile(`^Attachments, loose files: \d+ files? from \d+ messages?\.$`).MatchString(looseLine) {
		t.Errorf("the loose run's card says %q", looseLine)
	}
	if offCard["attachLine"] != nil || offCard["attachWarn"] != nil {
		t.Errorf("a run without Extract attachments has a card line: %v / %v", offCard["attachLine"], offCard["attachWarn"])
	}
	if zipCard["attachWarn"] != nil || looseCard["attachWarn"] != nil {
		t.Errorf("a run that made every attachment has a warning: %v / %v", zipCard["attachWarn"], looseCard["attachWarn"])
	}
	for _, want := range []string{"c.attachLine", "c.attachWarn"} {
		if !strings.Contains(exportsJS, want) {
			t.Errorf("exports.js does not draw %q: the card's attachment line would never be seen", want)
		}
	}
}
