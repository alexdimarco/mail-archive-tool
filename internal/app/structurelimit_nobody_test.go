package app

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// deepWithUU is a message whose first part is a uuencoded file (a transfer encoding the
// earlier parser stops at, so the walker's reading stands) and whose second part nests
// multiparts levels deep around one text part: the walker keeps the rest unread past its
// depth bound, so the message's text is in remaining-parts.undecoded and the page has no body.
func deepWithUU(levels int) string {
	body := fmt.Sprintf("--b%d\r\nContent-Type: text/plain\r\n\r\nthe payroll is attached\r\n--b%d--\r\n", levels, levels)
	for i := levels - 1; i >= 2; i-- {
		body = fmt.Sprintf("--b%d\r\nContent-Type: multipart/mixed; boundary=\"b%d\"\r\n\r\n%s--b%d--\r\n", i, i+1, body, i)
	}
	return "From: a@example.com\r\nTo: b@example.com\r\nSubject: Payroll\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <deep@x>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"b1\"\r\n\r\n" +
		"--b1\r\nContent-Type: application/octet-stream\r\nContent-Disposition: attachment; filename=\"payroll.dat\"\r\n" +
		"Content-Transfer-Encoding: x-uuencode\r\n\r\nbegin 644 payroll.dat\r\n#86%B\r\n`\r\nend\r\n" +
		"--b1\r\nContent-Type: multipart/mixed; boundary=\"b2\"\r\n\r\n" + body + "--b1--\r\n"
}

// covers: MA-348, MA-349, R1, R10
// F5 of the MF-S2 friction review (cell OW-10). A message nested past the structure bound keeps
// the rest unread as remaining-parts.undecoded with a terminal structure-limit row that says where
// the text is. It does not also get a fillable missing-body row ("download the message in your
// mail app"): no download changes a bound, so that row never clears and the run summary counts
// "1 still missing content" for ever. The positive twin: a message with no body and no bound still
// has its missing-body row.
func TestAMessageWhoseTextIsInTheUnreadRestHasNoFillableMissingBodyRow(t *testing.T) {
	run := func(t *testing.T, msg string) (Result, string) {
		src := filepath.Join(tmpDir(t), "Inbox")
		if err := os.MkdirAll(filepath.Join(src, "cur"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "cur", "1.eml"), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		out := tmpDir(t)
		opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}
		r, err := Run(context.Background(), opts, log.New(io.Discard, "", 0), nil)
		if err != nil {
			t.Fatal(err)
		}
		rep, _ := readReport(t, out)
		return r, rep
	}

	// The positive twin: no body and nothing kept unread is a missing body, fillable on a local source.
	empty := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Empty\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n" +
		"Message-ID: <empty@x>\r\nMIME-Version: 1.0\r\nContent-Type: text/plain\r\n\r\n"
	r, rep := run(t, empty)
	if r.Stats.Exported != 1 || !strings.Contains(rep, "missing-body\t") {
		t.Fatalf("positive twin: an empty message has no missing-body row (exported %d):\n%s", r.Stats.Exported, rep)
	}

	r, rep = run(t, deepWithUU(34))
	if r.Stats.Exported != 1 {
		t.Fatalf("exported %d, want 1", r.Stats.Exported)
	}
	if !strings.Contains(rep, state.GapKindStructureLimit+"\tterminal\t") {
		t.Fatalf("no terminal structure-limit row (the walk of this message changed):\n%s", rep)
	}
	if strings.Contains(rep, "missing-body\t") || r.Fillable != 0 {
		t.Errorf("the text is kept in remaining-parts.undecoded, yet the report has a missing-body row and %d message(s) are \"still missing content\":\n%s", r.Fillable, rep)
	}
}
