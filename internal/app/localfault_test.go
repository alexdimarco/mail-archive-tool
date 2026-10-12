package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/graph/graphtest"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// withAttachment is a multipart message with one attachment: its capture writes a
// page and a zip.
func withAttachment(id string) string {
	return "From: a@example.com\r\nSubject: subj-" + id + "\r\nMessage-ID: <" + strings.ToLower(id) + "@x>\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMIME-Version: 1.0\r\nContent-Type: multipart/mixed; boundary=\"b\"\r\n\r\n" +
		"--b\r\nContent-Type: text/plain\r\n\r\nbody-" + id + "\r\n--b\r\nContent-Type: application/octet-stream\r\n" +
		"Content-Disposition: attachment; filename=\"a.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\nQUJDRA==\r\n--b--\r\n"
}

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
// The local readers' folder faults, end to end (FI-S2b): in one Thunderbird-style store a
// maildir folder whose cur/ cannot be listed and an mbox file that cannot be opened each
// cost that folder only, are fillable folder-unreadable gaps whose remedy is the
// permission one, and make the run partial; every other folder is archived. After the
// permissions are fixed the next run archives both and the gaps close. (The mbox
// mid-stream error with its count, and the Evolution walkers, are the reader's own tests
// in internal/source, which cite the same row.)
func TestLocalFolderFaultsCostOneFolder(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a unix permission check that root and Windows do not make")
	}
	store := mboxStore(t, tmpDir(t), "tb", []string{"A", "B", "C"}, 2)
	maildirStore(t, store, "M/cur/x:2,S", "N/cur/y:2,S")
	lockedMbox, lockedDir := filepath.Join(store, "B"), filepath.Join(store, "M", "cur")
	for _, p := range []string{lockedMbox, lockedDir} {
		if err := os.Chmod(p, 0); err != nil {
			t.Fatal(err)
		}
		p := p
		t.Cleanup(func() { os.Chmod(p, 0o755) })
	}
	out := tmpDir(t)
	res, err := runLocal(t, out, store)
	if !errors.Is(err, ErrPartial) || !res.Partial {
		t.Fatalf("run 1 err = %v; want ErrPartial", err)
	}
	if n := recordCount(t, out); n != 5 {
		t.Errorf("%d records; want A (2), C (2) and N (1): one folder's fault costs that folder only", n)
	}
	gaps := ledgerOf(t, out)
	got := map[string]state.CaptureGap{}
	for _, g := range gaps {
		got[g.Locator] = g
	}
	for _, loc := range []string{"folder:B", "folder:M/cur"} {
		g, ok := got[loc]
		if !ok || g.Reason != "folder-unreadable" || g.Scope != state.GapScopeFolder || g.Class != state.ClassFillable {
			t.Errorf("ledger %s; want a fillable folder-unreadable folder gap at %s", gapReasons(gaps), loc)
		}
	}
	if len(gaps) != 2 || res.Gaps.FolderGaps != 2 {
		t.Errorf("ledger %s, folder gaps %d; want exactly the two folders", gapReasons(gaps), res.Gaps.FolderGaps)
	}
	if lines := strings.Join(res.Gaps.Lines, "\n"); !strings.Contains(lines, "fix the permissions on the folder") {
		t.Errorf("summary %q; want the permission remedy for a folder", lines)
	}

	for _, p := range []string{lockedMbox, lockedDir} {
		if err := os.Chmod(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	ageLedger(t, out, 48*time.Hour)
	res, err = runLocal(t, out, store)
	if err != nil || res.Partial || recordCount(t, out) != 8 || len(ledgerOf(t, out)) != 0 || res.Gaps.ClosedGone != 2 {
		t.Errorf("run 2: err %v, partial %v, %d records, ledger %s, closed %d; want B and M archived and both gaps closed", err, res.Partial, recordCount(t, out), gapReasons(ledgerOf(t, out)), res.Gaps.ClosedGone)
	}
}

// TestRawStubsKeepTheirBytesAndTheirRecord, once a skip encoder here, is a real test in
// rawstub_test.go (FI-S2b): the raw-preserving stub of a MIME-panicking maildir message,
// and TestGraphStubKeepsItsBytes for the Graph half.

// covers: MA-318, R1, R3, R7, R10, S46
// KEEP twin on Graph (P15, FC5): a message archived complete (a page and a zip)
// whose Full-mode re-read now fails to parse — under a RE-ISSUED immutable id, the
// case in which an adopted stub would also move the id — leaves the record, the
// page and the zip as they were: nothing is written, the record is stamped seen,
// it adopts no new immutable id, no stub record is minted (the archive holds the
// same records), and one info refresh-failed gap names the held-back capture. It
// is not a partial run: nothing is missing from the archive.
func TestGraphFullModeKeepsAnArchivedRecordWhenItsParseNowPanics(t *testing.T) {
	s := graphtest.NewServer()
	defer s.Close()
	s.HonorImmutableIDs(true)
	s.AddFolder("u1", "F_IN", "Inbox", 10)
	s.AddMessage("u1", "F_IN", "M")
	s.SetValue("M", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, withAttachment("M")) })
	out := tmpDir(t)
	g := isoOptions(s)
	if res, err := sweepRun(g, out, export.Incremental); err != nil || res.Stats.Exported != 1 {
		t.Fatalf("run 1: exported %d, err %v; want 1, nil", res.Stats.Exported, err)
	}
	files := messageFiles(t, out)
	zips := 0
	for name := range files {
		if strings.HasSuffix(name, "-attachments.zip") {
			zips++
		}
	}
	if zips != 1 {
		t.Fatalf("the archived message has %d zips, want its one attachment zip", zips)
	}
	before := loadArchive(t, out)
	k, rec := recordForID(t, out, "m@x")

	// The mailbox re-issues M's id; the service's body is as it was, but this build's
	// parse of it fails.
	s.RemoveMessage("u1", "F_IN", "M")
	s.AddMessage("u1", "F_IN", "M2")
	s.SetMid("M2", "m@x")
	parseRFC822 = func([]byte) *model.Message { return &model.Message{Subject: "(unreadable message)"} }
	t.Cleanup(func() { parseRFC822 = source.ParseRFC822 })
	res, err := sweepRun(g, out, export.Full)
	if err != nil {
		t.Fatalf("run 2 (Full, the parse fails): %v; a held-back capture is info, not a partial run", err)
	}
	if res.Stats.Kept != 1 || res.Stats.Exported != 0 {
		t.Errorf("Kept %d, Exported %d; want 1 and 0", res.Stats.Kept, res.Stats.Exported)
	}
	if after := messageFiles(t, out); !reflect.DeepEqual(after, files) {
		t.Errorf("the archive's files changed: %v -> %v", files, after)
	}
	k2, rec2 := recordForID(t, out, "m@x")
	seen := rec2.LastSeen
	rec2.LastSeen = rec.LastSeen
	if k2 != k || !reflect.DeepEqual(rec2, rec) {
		t.Errorf("the record changed: %+v -> %+v (key %q -> %q); want it as it was, but for its stamp", rec, rec2, k, k2)
	}
	if !seen.Equal(runStart(t, out)) {
		t.Errorf("the record was not stamped seen by the run (LastSeen %v, run %v)", seen, runStart(t, out))
	}
	if rec2.PhysID != "M" || len(rec2.AltPhysIDs) != 0 {
		t.Errorf("the record's immutable ids are %q + %v; want M alone: a held-back capture adopts no id", rec2.PhysID, rec2.AltPhysIDs)
	}
	if after := loadArchive(t, out); after.Len() != before.Len() {
		t.Errorf("the archive holds %d records, want the same %d: no stub record is minted", after.Len(), before.Len())
	}
	g2 := graphGaps(t, out)
	if len(g2) != 1 || g2[0].Reason != "refresh-failed" || g2[0].Class != state.ClassInfo || g2[0].Locator != "graph:M2" {
		t.Errorf("ledger %+v; want one info refresh-failed gap at graph:M2", g2)
	}
}

// covers: MA-318, R1
func TestTwoUnparseableRawMessagesDoNotCollapse(t *testing.T) {
	t.Skip("R1 (ruling J8 = ESC-1): two distinct MIME-panicking raw messages in one folder still share today's constant stub identity and collapse into one record; the fold that separates them moves MA-330's identity golden, so it waits for the owner's answer to ESC-1 (slice FI-S2b-id); closes when FI-S2b-id lands")
}
