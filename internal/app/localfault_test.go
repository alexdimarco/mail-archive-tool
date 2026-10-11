package app

import (
	"bytes"
	"context"
	"log"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-323, R1, R5, S46
// An older build's collapsed "(unreadable message)" record is re-classed by a RUN
// (FC27) — never by reading the archive — to the info label legacy-placeholder
// with a line in the run log, once; its page and files are untouched and the
// report row says why and what to do.
func TestLegacyPlaceholderIsReclassedByARunOnce(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatalf("positive twin: the run over a healthy source failed: %v", err)
	}
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	m, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	var tok string
	for _, t2 := range m.Stores {
		tok = t2
	}
	assure.Reached(t, tok, "the source's store token")
	stubKey := state.Key(tok, "Inbox", model.LegacyStubIdentity())
	m.Add(stubKey, state.Record{Path: tok + "/Inbox/0000-00-00_0000_unreadable-message_deadbeef.html", Folder: "Inbox",
		Missing: []string{state.MissingBody}, Subject: state.LegacyPlaceholderSubject})
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}

	var logs bytes.Buffer
	res, err := Run(context.Background(), opts, log.New(&logs, "", 0), nil)
	if err != nil {
		t.Fatalf("a run over an archive holding a legacy placeholder failed: %v", err)
	}
	if !strings.Contains(logs.String(), "re-classed 1 legacy placeholder(s); this version records each unreadable message separately") {
		t.Errorf("the run did not log the re-class once:\n%s", logs.String())
	}
	m2, err := state.Load(mpath)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := m2.Get(stubKey)
	if !ok || len(r.Missing) != 0 || len(r.Terminal) != 1 || r.Terminal[0] != state.LabelLegacyPlaceholder || r.Path == "" {
		t.Fatalf("the legacy record = %+v, %v; want it kept, with Terminal [legacy-placeholder] and nothing Missing", r, ok)
	}
	if res.Partial {
		t.Error("a legacy placeholder is info, nothing missing: it must not make the run partial")
	}
	rows := readLines(t, res.ReportPath)
	var row string
	for _, l := range rows[1:] {
		if strings.HasPrefix(l, "unreadable-legacy-placeholder\tinfo\t") {
			row = l
		}
	}
	if !strings.Contains(assure.Reached(t, row, "the legacy-placeholder report row"), "why: an earlier version of mailarchive kept every message it could not read as this one page") ||
		!strings.Contains(row, "what to do: nothing to do:") {
		t.Errorf("legacy-placeholder row lacks a why and a what to do: %q", row)
	}

	// Once: the next run finds nothing to re-class.
	logs.Reset()
	if _, err := Run(context.Background(), opts, log.New(&logs, "", 0), nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(logs.String(), "legacy placeholder") {
		t.Errorf("the second run re-classed again:\n%s", logs.String())
	}
}

// The cases of the rows FI-S2a lands that need a later slice's code stay skip
// encoders citing their row (FC33): the covers-map reads a row as covered once any
// test cites it, so an unbuilt half must not hide behind the built one.

// covers: MA-316, R10, R6, R1, S46
func TestLocalFolderFaultsCostOneFolder(t *testing.T) {
	t.Skip("MA-316: a maildir non-root ReadDir failure, an mbox open failure, an mbox mid-stream error (folder-partially-read with the count read) and the Evolution walkers' folder errors are FI-S2b's (the mbox, maildir and Evolution readers); closes when FI-S2b lands")
}

// covers: MA-318, R1, R3, R7, R10, S46
func TestRawStubsKeepTheirBytesAndTheirRecord(t *testing.T) {
	t.Skip("MA-318: a MIME-panicking mbox, maildir or Graph message keeps Raw (the .eml under -raw), shows the headers plus the first 64 KiB on its page and in the index, and is an unparsed record with ParserGen and RawDigest (FC25); the raw-preserving stub is FI-S2b's (and FI-S3's for Graph); closes when they land")
}

// covers: MA-318, R1, R3, R7, R10, S46
func TestGraphFullModeKeepsAnArchivedRecordWhenItsParseNowPanics(t *testing.T) {
	t.Skip("MA-318: a Graph Full-mode run where an archived message's parse now panics leaves the record, page and zip unchanged and adopts no PhysID (P15's Graph twin); the Graph walk is FI-S3's; closes when FI-S3 lands")
}

// covers: MA-318, R1
func TestTwoUnparseableRawMessagesDoNotCollapse(t *testing.T) {
	t.Skip("R1 (ruling J8 = ESC-1): two distinct MIME-panicking raw messages in one folder still share today's constant stub identity and collapse into one record; the fold that separates them moves MA-330's identity golden, so it waits for the owner's answer to ESC-1 (slice FI-S2b-id); closes when FI-S2b-id lands")
}
