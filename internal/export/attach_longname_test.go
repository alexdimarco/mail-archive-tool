package export

import (
	"archive/zip"
	"bytes"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/util"
)

// covers: MA-03, MA-29, MA-30, R4, R5
// MS2-AO9. One hostile attachment name cannot cost the message its other attachments: a name
// whose "extension" is 70,000 characters (a zip entry name is at most 65,535 bytes) is
// bounded like any other name, so the zip is written with every attachment the message had,
// and no attachment is marked as an error. SanitizeFilename keeps a real extension and
// bounds the length of the whole name.
func TestALongAttachmentNameDoesNotLoseTheMessagesOtherAttachments(t *testing.T) {
	long := "a." + strings.Repeat("x", 70_000)
	if got := util.SanitizeFilename(long, 0); len([]rune(got)) > 140 || got == "" || strings.HasSuffix(got, ".") {
		t.Errorf("SanitizeFilename of a 70,002-character name is %d runes: %.40q", len([]rune(got)), got)
	}
	if got := util.SanitizeFilename("Quarterly report 2023.PDF", 0); got != "Quarterly report 2023.PDF" {
		t.Errorf("a real extension is changed: %q", got)
	}
	if got := util.SanitizeFilename(strings.Repeat("n", 300)+".pdf", 0); !strings.HasSuffix(got, ".pdf") || len([]rune(got)) > 130 {
		t.Errorf("a long base lost its extension or its bound: %d runes, %.20q...%q", len([]rune(got)), got, got[len(got)-8:])
	}

	write := func(s string) func(io.Writer) (int64, error) {
		return func(w io.Writer) (int64, error) { n, err := w.Write([]byte(s)); return int64(n), err }
	}
	atts := []model.Attachment{
		{Filename: long, WriteTo: write("one")},
		{Filename: "good.pdf", WriteTo: write("two")},
		{Filename: long, WriteTo: write("three")}, // a second one with the same name
	}
	path := filepath.Join(t.TempDir(), "msg.attachments.zip")
	res, err := WriteZip(path, atts, nil)
	if err != nil {
		t.Fatalf("WriteZip: %v (the one long name aborts the whole archive)", err)
	}
	if res.Written != 3 || len(res.Failed) != 0 || len(res.Empty) != 0 {
		t.Errorf("written %d failed %v empty %v, want all 3 written", res.Written, res.Failed, res.Empty)
	}
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
		if len(f.Name) > 600 {
			t.Errorf("a zip entry name is %d bytes", len(f.Name))
		}
	}
	if len(zr.File) != 3 || !names["good.pdf"] {
		t.Errorf("the zip holds %d entries, want 3 with good.pdf", len(zr.File))
	}
	var buf bytes.Buffer
	for _, f := range zr.File {
		if f.Name == "good.pdf" {
			rc, _ := f.Open()
			io.Copy(&buf, rc)
			rc.Close()
		}
	}
	if buf.String() != "two" {
		t.Errorf("good.pdf holds %q", buf.String())
	}
}
