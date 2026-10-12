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
	"mail-archive-tool/internal/state"
)

// covers: MA-348, R1, R7
// F14 of the MF-S2 friction review (cells IN-1, OW-5, OW-2). A parse-gap report row says where
// the original is: "the .eml beside the page" for a record that has one on disk, "in the source,
// no .eml copy" for one that has none. The rows of the older kinds keep the words their own
// assertions pin (TestReportKindsForReadGapLabels).
func TestAParseGapReportRowSaysWhetherAnEmlIsBesideThePage(t *testing.T) {
	out := tmpDir(t)
	m, _ := state.Load(filepath.Join(out, ".mailarchive-manifest.json"))
	l := state.ParseGapLabel(state.GapKindUndecodedPart, "x.dat.undecoded — an encoding")
	m.Add("k1", state.Record{Path: "s/with.html", Folder: "Inbox", Subject: "With", Terminal: []string{l}})
	m.Add("k2", state.Record{Path: "s/without.html", Folder: "Inbox", Subject: "Without", Terminal: []string{l}})
	m.Add("k3", state.Record{Path: "s/old.html", Folder: "Inbox", Subject: "Old", Terminal: []string{"unparsed"}})
	if err := os.MkdirAll(filepath.Join(out, "s"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out, "s", "with.eml"), []byte("From: a@b\r\n\r\nx"), 0o644); err != nil {
		t.Fatal(err)
	}
	path, _ := writeReport(out, m, false, log.New(io.Discard, "", 0))
	rows := map[string]string{}
	for _, line := range readLines(t, path)[1:] {
		f := strings.Split(line, "\t")
		rows[f[4]] = f[5]
	}
	if got := rows["With"]; !strings.Contains(got, "the original is kept as the .eml beside the page") {
		t.Errorf("a record with an .eml: %q", got)
	}
	if got := rows["Without"]; !strings.Contains(got, "the original is kept in the source (no .eml copy was made") || strings.Contains(got, "beside the page") {
		t.Errorf("a record with no .eml: %q", got)
	}
	// The older kind keeps its pinned words.
	if got := rows["Old"]; !strings.HasSuffix(got, "what to do: nothing to do: the original is kept") {
		t.Errorf("an unparsed row changed: %q", got)
	}
}

// covers: MA-348, R1, R7
// F6 of the MF-S2 friction review, end to end (cell CA-3). A plain-text body labelled with a
// base64 transfer encoding it does not honour is cut by the walker; the report row for the cut TEXT
// says the page shows its readable start, not that a .partial file is in the attachments zip: this
// message has no attachment, no zip and no .partial.
func TestACutBodysReportRowDoesNotNameAZipOrAPartialFile(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	if err := os.MkdirAll(filepath.Join(src, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Call me\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <cutbody@x>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=\"x-unknown\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		"Please call me about the invoice on 555-0100.\r\n"
	if err := os.WriteFile(filepath.Join(src, "cur", "1.eml"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
	out := tmpDir(t)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}
	if _, err := Run(context.Background(), opts, log.New(io.Discard, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	rep, ok := readReport(t, out)
	if !ok {
		t.Fatal("no report")
	}
	var row string
	for _, line := range strings.Split(rep, "\n")[1:] {
		if strings.HasPrefix(line, state.GapKindCorruptPart+"\t") {
			row = line
		}
	}
	if row == "" {
		t.Fatalf("no corrupt-part row (the walk of this message changed; the report is:\n%s)", rep)
	}
	if strings.Contains(row, ".partial") || strings.Contains(row, "zip") {
		t.Errorf("the row for a cut body names a .partial file or a zip that does not exist:\n%s", row)
	}
	if !strings.Contains(row, "the page shows") {
		t.Errorf("the row for a cut body does not say what the page shows:\n%s", row)
	}
	var zips []string
	filepath.WalkDir(out, func(p string, d os.DirEntry, err error) error {
		if err == nil && strings.HasSuffix(p, ".zip") {
			zips = append(zips, p)
		}
		return nil
	})
	if len(zips) != 0 {
		t.Errorf("a zip was written for a message with no attachment: %v", zips)
	}
}
