package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/export"
)

// covers: MA-348, R1, R18
// F3 of the MF-S2 friction review, end to end (cell OW-3). A mailbox file whose last message is cut
// inside an attachment (no closing boundary) is archived with the readable start kept as .partial and
// a fillable corrupt-part gap; the run's Result says that one message is still missing content, that
// it is one of the cannot-decode ones, and that it is fillable for nothing else, which is what the run
// summary's remedy keys on. A message with an ordinary empty attachment is the positive twin: it is
// missing content with no parse gap.
func TestResultSaysWhichMissingMessagesAreOnlyParseGaps(t *testing.T) {
	write := func(t *testing.T, name, msg string) string {
		src := filepath.Join(tmpDir(t), "Inbox")
		if err := os.MkdirAll(filepath.Join(src, "cur"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(src, "cur", name), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		return src
	}
	run := func(t *testing.T, src string) Result {
		opts := Options{Inputs: []string{src}, Out: tmpDir(t), Mode: export.Incremental, Index: true, Pages: true}
		r, err := Run(context.Background(), opts, log.New(io.Discard, "", 0), nil)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	head := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Contract\r\nDate: Mon, 03 Mar 2025 09:00:00 +0000\r\n" +
		"Message-ID: <cut@x>\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"B\"\r\n\r\n"
	text := "--B\r\nContent-Type: text/plain\r\n\r\nSee the contract.\r\n"

	// Positive twin: an empty attachment is missing content and not a parse gap.
	empty := run(t, write(t, "1.eml", head+text+"--B\r\nContent-Type: application/pdf\r\nContent-Disposition: attachment; filename=\"c.pdf\"\r\nContent-Transfer-Encoding: base64\r\n\r\n\r\n--B--\r\n"))
	if empty.Fillable != 1 || empty.FillableParseGap != 0 || empty.FillableOnlyParseGap != 0 {
		t.Fatalf("positive twin: an empty attachment: fillable %d, parse-gap %d, only %d; want 1, 0, 0", empty.Fillable, empty.FillableParseGap, empty.FillableOnlyParseGap)
	}

	cut := run(t, write(t, "1.eml", head+text+"--B\r\nContent-Type: application/pdf; name=\"contract.pdf\"\r\nContent-Disposition: attachment; filename=\"contract.pdf\"\r\n"+
		"Content-Transfer-Encoding: base64\r\n\r\nQUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo=\r\nQUJDREVGR0hJSktMTU5P"))
	if cut.Fillable != 1 || cut.Undecodable != 1 || cut.FillableParseGap != 1 || cut.FillableOnlyParseGap != 1 {
		t.Errorf("a cut attachment: fillable %d, undecodable %d, fillable parse-gap %d, only %d; want 1 each", cut.Fillable, cut.Undecodable, cut.FillableParseGap, cut.FillableOnlyParseGap)
	}
}
