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

// maildirMessage writes one RFC 5322 message with an attachment whose base64
// body is `payload` (empty = declared but not downloaded, the on-demand IMAP
// shape) into a maildir folder, which the source layer reads as one store.
func maildirMessage(t *testing.T, dir, payload string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "cur"), 0o755); err != nil {
		t.Fatal(err)
	}
	msg := "From: a@example.com\r\nTo: b@example.com\r\nSubject: Invoice\r\n" +
		"Date: Mon, 03 Mar 2025 09:00:00 +0000\r\nMessage-ID: <inv@x>\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/mixed; boundary=\"B\"\r\n\r\n" +
		"--B\r\nContent-Type: text/plain\r\n\r\nPlease find attached.\r\n" +
		"--B\r\nContent-Type: application/octet-stream; name=\"empty.bin\"\r\n" +
		"Content-Disposition: attachment; filename=\"empty.bin\"\r\nContent-Transfer-Encoding: base64\r\n\r\n" +
		payload + "\r\n--B--\r\n"
	if err := os.WriteFile(filepath.Join(dir, "cur", "1.eml"), []byte(msg), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readReport(t *testing.T, out string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(out, "attachments-report.tsv"))
	if err != nil {
		return "", false
	}
	return string(data), true
}

// covers: MA-68, R1, S6
// The verification report is a durable view of the manifest, not a per-run log:
// a gap found by one run is still listed by the next run that finds nothing new,
// disappears once filled (and the empty report is removed), and a legacy
// archive's earlier report is preserved as attachments-report-legacy.tsv.
func TestVerificationReportIsRegeneratedFromManifest(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessage(t, src, "")
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	opts := Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}

	r1, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1.Fillable != 1 {
		t.Fatalf("run 1 fillable = %d, want 1 (%+v)", r1.Fillable, r1.Stats)
	}
	rep, ok := readReport(t, out)
	if !ok || !strings.Contains(rep, "empty-attachment") || !strings.Contains(rep, "fillable") || !strings.Contains(rep, "empty.bin") {
		t.Fatalf("run 1 report missing the fillable row: ok=%v\n%s", ok, rep)
	}

	// Run 2 finds nothing new; the earlier finding must still be listed.
	r2, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2.Stats.Exported != 0 || r2.Fillable != 1 {
		t.Errorf("run 2: exported=%d fillable=%d, want 0/1", r2.Stats.Exported, r2.Fillable)
	}
	if rep, ok := readReport(t, out); !ok || !strings.Contains(rep, "empty.bin") {
		t.Errorf("run 2 lost the earlier finding: ok=%v\n%s", ok, rep)
	}

	// The content arrives (the client synced): filled, and the report goes away.
	maildirMessage(t, src, "aGVsbG8=")
	r3, err := Run(context.Background(), opts, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r3.Stats.Filled != 1 || r3.Fillable != 0 {
		t.Errorf("run 3: filled=%d fillable=%d, want 1/0", r3.Stats.Filled, r3.Fillable)
	}
	if _, ok := readReport(t, out); ok {
		t.Error("report still present with nothing to report")
	}

	// Legacy archive: a version-1 manifest plus the old per-run report.
	out2 := tmpDir(t)
	old := `{"version":1,"entries":{"Inbox\u0000mid:<gone@x>":{"path":"Inbox/x.html","folder":"Inbox","exported_at":"2026-01-01T00:00:00Z"}}}`
	if err := os.WriteFile(filepath.Join(out2, ".mailarchive-manifest.json"), []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(out2, "attachments-report.tsv"), []byte("kind\tfolder\nempty-attachment\tInbox\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	opts2 := opts
	opts2.Out = out2
	r4, err := Run(context.Background(), opts2, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r4.Unknown != 1 {
		t.Errorf("legacy entry not counted as unknown: %+v", r4)
	}
	legacy, err := os.ReadFile(filepath.Join(out2, "attachments-report-legacy.tsv"))
	if err != nil || !strings.Contains(string(legacy), "empty-attachment") {
		t.Errorf("earlier report was not preserved as attachments-report-legacy.tsv: %v", err)
	}
	if rep, ok := readReport(t, out2); !ok || !strings.Contains(rep, "unknown") {
		t.Errorf("regenerated report lacks the unknown row: ok=%v\n%s", ok, rep)
	}
}
