package export

import (
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// covers: MA-21
func TestVerifyEmptyAndUnresolved(t *testing.T) {
	out := t.TempDir()
	manifest, err := state.Load(filepath.Join(out, "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	exp := &Exporter{OutDir: out, Manifest: manifest, Log: log.New(io.Discard, "", 0)}

	msg := &model.Message{
		Subject:  "Test",
		Received: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		// References a cid image that has no matching attachment part.
		HTMLBody: `<p><img src="cid:missing123@x"> hello</p>`,
		Attachments: []model.Attachment{
			// A declared attachment whose content is empty (e.g. not downloaded).
			{Filename: "empty.bin", WriteTo: func(w io.Writer) (int64, error) { return 0, nil }},
		},
	}

	if _, err := exp.Export("store", []string{"Inbox"}, msg); err != nil {
		t.Fatal(err)
	}

	if exp.Stats.AttachmentsEmpty != 1 {
		t.Errorf("AttachmentsEmpty = %d, want 1", exp.Stats.AttachmentsEmpty)
	}
	if exp.Stats.UnresolvedInlineRef != 1 {
		t.Errorf("UnresolvedInlineRef = %d, want 1", exp.Stats.UnresolvedInlineRef)
	}
	if len(exp.Issues) != 2 {
		t.Fatalf("issues = %d, want 2: %+v", len(exp.Issues), exp.Issues)
	}

	// A fully-present attachment produces no issue.
	exp2 := &Exporter{OutDir: t.TempDir(), Manifest: mustManifest(t), Log: log.New(io.Discard, "", 0)}
	ok := &model.Message{
		Subject:  "Clean",
		Received: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		HTMLBody: "<p>no images</p>",
		Attachments: []model.Attachment{
			{Filename: "doc.txt", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("hi")); return int64(n), nil }},
		},
	}
	if _, err := exp2.Export("store", []string{"Inbox"}, ok); err != nil {
		t.Fatal(err)
	}
	if len(exp2.Issues) != 0 || exp2.Stats.AttachmentsEmpty != 0 || exp2.Stats.UnresolvedInlineRef != 0 {
		t.Errorf("clean message produced issues: %+v", exp2.Issues)
	}
	if exp2.Stats.Attachments != 1 {
		t.Errorf("expected 1 archived attachment, got %d", exp2.Stats.Attachments)
	}
}

func mustManifest(t *testing.T) *state.Manifest {
	t.Helper()
	m, err := state.Load(filepath.Join(t.TempDir(), "m.json"))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// covers: MA-69, R5, R1, S2
// Exported files are written to unique temp files and renamed into place, so a
// failure mid-write can never leave a truncated or partial exported file: an
// attachment whose stream fails after some bytes leaves NO zip on disk (neither
// the final name nor a temp), and the failure is recorded as a verification
// issue rather than silently dropped. Positive twin first: a healthy attachment
// lands as the final zip with no temp left behind.
func TestAtomicWritesLeaveNoPartialFiles(t *testing.T) {
	out := t.TempDir()
	exp := &Exporter{OutDir: out, Manifest: mustManifest(t), Log: log.New(io.Discard, "", 0)}

	good := &model.Message{
		Subject:  "Good",
		Received: time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC),
		HTMLBody: "<p>ok</p>",
		Attachments: []model.Attachment{
			{Filename: "doc.txt", WriteTo: func(w io.Writer) (int64, error) { n, _ := w.Write([]byte("hello")); return int64(n), nil }},
		},
	}
	if _, err := exp.Export("store", []string{"Inbox"}, good); err != nil {
		t.Fatal(err)
	}
	if n := countSuffix(t, out, "-attachments.zip"); n != 1 {
		t.Fatalf("healthy attachment: want 1 zip, got %d", n)
	}
	if n := countSuffix(t, out, ".tmp"); n != 0 {
		t.Fatalf("temp files left after a healthy export: %d", n)
	}

	// An attachment whose stream fails mid-way (a torn IMAP fetch, a read error).
	bad := &model.Message{
		Subject:  "Bad",
		Received: time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC),
		HTMLBody: "<p>torn</p>",
		Attachments: []model.Attachment{
			{Filename: "torn.bin", WriteTo: func(w io.Writer) (int64, error) {
				w.Write([]byte("partial-bytes"))
				return 13, errors.New("stream failed")
			}},
		},
	}
	if _, err := exp.Export("store", []string{"Inbox"}, bad); err != nil {
		t.Fatalf("a torn attachment must not abort the export: %v", err)
	}
	if n := countSuffix(t, out, "-attachments.zip"); n != 1 {
		t.Errorf("torn attachment: a partial or empty zip was left on disk (zips=%d, want only the good one)", n)
	}
	if n := countSuffix(t, out, ".tmp"); n != 0 {
		t.Errorf("temp files left behind after a failed attachment write: %d", n)
	}
	if !hasIssue(exp.Issues, "attachment-error", "torn.bin") {
		t.Errorf("failed attachment not recorded as a verification issue: %+v", exp.Issues)
	}
	// The html itself still landed, atomically named.
	if n := countSuffix(t, out, ".html"); n != 2 {
		t.Errorf("html files = %d, want 2", n)
	}
}

// covers: MA-69, R5, S2
// A crash can leave a stale temp file or an orphan zip (renamed before its html
// or its manifest entry). SweepOrphans removes temps older than the run start and
// zips with no sibling html, and leaves everything else alone.
func TestSweepOrphans(t *testing.T) {
	out := t.TempDir()
	dir := filepath.Join(out, "store", "Inbox")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	kept := write("2026-06-01_1200_msg_abcd1234.html", "<p>x</p>")
	keptZip := write("2026-06-01_1200_msg_abcd1234-attachments.zip", "PK")
	orphanZip := write("2026-06-01_1200_gone_ffff0000-attachments.zip", "PK")
	staleTmp := write(".mailarchive-123.tmp", "partial")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(staleTmp, old, old)
	freshTmp := write(".mailarchive-456.tmp", "in-flight") // belongs to a run that started after ours

	removed := SweepOrphans(out, time.Now().Add(-time.Minute), log.New(io.Discard, "", 0))
	if removed != 2 {
		t.Errorf("removed = %d, want 2 (stale temp + orphan zip)", removed)
	}
	for _, p := range []string{kept, keptZip, freshTmp} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("sweep removed a live file: %s", p)
		}
	}
	for _, p := range []string{orphanZip, staleTmp} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("sweep left an orphan: %s", p)
		}
	}
}

func countSuffix(t *testing.T, root, suffix string) int {
	t.Helper()
	n := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(path, suffix) {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func hasIssue(issues []Issue, kind, detail string) bool {
	for _, is := range issues {
		if is.Kind == kind && is.Detail == detail {
			return true
		}
	}
	return false
}
