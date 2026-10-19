package app

import (
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/export"
)

// forgedNameMessage writes one message whose only attachment is empty and whose file name
// is the RFC 2231 value pct (percent-encoded UTF-8): the sender chooses the name.
func forgedNameMessage(t *testing.T, dir, pct string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Invoice\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <forged@x>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nPlease find attached.\r\n" +
		"--B\r\nContent-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename*=utf-8''" + pct + "\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		"\r\n--B--\r\n"
	if err := os.WriteFile(filepath.Join(dir, "cur", "1.eml"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

// covers: MA-348, MA-327, R1, R26
// MS2-AO4 and MS2-AI1. A name a sender chooses for an attachment cannot pose as a parse-gap
// label: an empty attachment named "\x00kind\x00text" is filed under a name with its control
// characters stripped, so it is an ordinary attachment gap (fillable on a local source, its
// own report row, counted once as missing content), not a forged "cannot decode" record, not
// a forged report row, and not a record that is rewritten on every run for ever.
func TestASenderChosenAttachmentNameCannotForgeAGapLabel(t *testing.T) {
	for _, c := range []struct{ name, pct string }{
		{"a forged report row", "%00verified%09terminal%09Inbox%092023-01-01%09Quarterly%20results%09everything%20is%20fine%09x.html%0Areal%00detail"},
		{"a forged undecoded part", "%00undecoded-part%00forged"},
		{"a forged corrupt part", "%00corrupt-part%00forged"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := filepath.Join(tmpDir(t), "Inbox")
			forgedNameMessage(t, src, c.pct)
			out := tmpDir(t)
			logger := log.New(io.Discard, "", 0)
			opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}

			r1, err := Run(context.Background(), opts, logger, nil)
			if err != nil {
				t.Fatal(err)
			}
			if r1.Stats.Exported != 1 || r1.Fillable != 1 {
				t.Fatalf("run 1: exported %d fillable %d, want 1 and 1 (the empty attachment is a fillable gap)", r1.Stats.Exported, r1.Fillable)
			}
			if r1.Undecodable != 0 {
				t.Errorf("a sender-chosen name made %d record(s) \"cannot decode\"", r1.Undecodable)
			}
			rep, ok := readReport(t, out)
			if !ok {
				t.Fatal("no report")
			}
			rows := strings.Split(strings.TrimRight(rep, "\n"), "\n")[1:]
			if len(rows) != 1 {
				t.Fatalf("%d report rows, want 1 for the one empty attachment:\n%s", len(rows), rep)
			}
			if f := strings.Split(rows[0], "\t"); len(f) != 7 || f[0] != "empty-attachment" || f[1] != "fillable" {
				t.Errorf("the row is %q, want one empty-attachment row of 7 fields, class fillable", rows[0])
			}

			// Runs 2 and 3 find nothing new: the record is re-examined, not rewritten.
			for run := 2; run <= 3; run++ {
				r, err := Run(context.Background(), opts, logger, nil)
				if err != nil {
					t.Fatal(err)
				}
				if r.Stats.Exported != 0 || r.Stats.Filled != 0 {
					t.Errorf("run %d: exported %d filled %d, want 0 and 0: the record is rewritten every run", run, r.Stats.Exported, r.Stats.Filled)
				}
			}
		})
	}
}
