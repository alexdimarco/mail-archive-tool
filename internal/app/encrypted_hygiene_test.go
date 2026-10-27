package app

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/container"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
	"mail-archive-tool/internal/vault"
)

// seedLedgerGap records one item gap in out's manifest, so the next run has a
// report row and a gap banner to write.
func seedLedgerGap(t *testing.T, out string, key *vault.Key) {
	t.Helper()
	var m *state.Manifest
	if key != nil {
		m = loadSealed(t, out, key)
	} else {
		var err error
		if m, err = state.Load(filepath.Join(out, ".mailarchive-manifest.json")); err != nil {
			t.Fatal(err)
		}
	}
	var tok string
	for _, tk := range m.Stores {
		tok = tk
	}
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:Inbox/x",
		Reason: "file-unreadable", Class: state.ClassFillable, Subject: "SECRET-SUBJECT"}, time.Now().UTC().Add(-time.Hour))
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
}

// requireNoCleartextScaffold fails if out holds a folder page, a root index.html,
// the attachments report, any folder but units/ and the sealed record's chunks/
// and inventory/, or a README other than the encrypted one.
func requireNoCleartextScaffold(t *testing.T, out string) {
	t.Helper()
	filepath.WalkDir(out, func(p string, d fs.DirEntry, err error) error {
		if err != nil || p == out {
			return nil
		}
		rel, _ := filepath.Rel(out, p)
		rel = filepath.ToSlash(rel)
		if top := strings.SplitN(rel, "/", 2)[0]; d.IsDir() && top != export.UnitsDir && top != container.ChunksDir && top != container.InventoryDir {
			t.Errorf("an encrypted archive holds the folder %s", rel)
		}
		if strings.HasSuffix(rel, ".html") || rel == "attachments-report.tsv" {
			t.Errorf("an encrypted archive holds %s", rel)
		}
		return nil
	})
	if b, err := os.ReadFile(filepath.Join(out, "README.txt")); err != nil || string(b) != EncryptedNote() {
		t.Errorf("an encrypted archive's README is not the encrypted one (%v):\n%s", err, b)
	}
}

// covers: MA-737, R37
// An encrypted run — local and Microsoft 365, with the search index and folder pages
// switched on and a gap in the ledger — writes no folder page or root index.html (its
// folder tree, subjects and senders in cleartext), no attachments-report.tsv (folders,
// subjects and source ids) and no layout README: its README only says it is encrypted
// and how to open it, and a partial run's text points at MailArchive, not the report.
// The positive twin: the same local runs without a key write the pages, the report
// with the gap's row and the layout README, as before.
func TestAnEncryptedRunWritesNoPagesReportOrLayoutReadme(t *testing.T) {
	src := encryptedSourcePST(t)

	plain := tmpDir(t)
	opts := Options{Inputs: []string{src}, Out: plain, Index: true, Pages: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	seedLedgerGap(t, plain, nil)
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	report, _ := os.ReadFile(filepath.Join(plain, "attachments-report.tsv"))
	readme, _ := os.ReadFile(filepath.Join(plain, "README.txt"))
	if _, err := os.Stat(filepath.Join(plain, "index.html")); err != nil || !strings.Contains(string(report), "SECRET-SUBJECT") || string(readme) != archiveReadme {
		t.Fatalf("the positive twin: a plaintext run writes its pages, report and layout README (%v)", err)
	}

	out, key := encryptedOut(t)
	opts = Options{Inputs: []string{src}, Out: out, Key: key, Index: true, Pages: true}
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	seedLedgerGap(t, out, key)
	if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
		t.Fatal(err)
	}
	requireNoCleartextScaffold(t, out)

	s := sweepServer(false, []string{"A1", "A2"}, []string{"B1"}, nil)
	defer s.Close()
	captured, ckey := encryptedOut(t)
	if _, err := RunGraph(context.Background(), isoOptions(s), Options{Out: captured, Key: ckey, Index: true, Pages: true}, discard()); err != nil {
		t.Fatal(err)
	}
	requireNoCleartextScaffold(t, captured)

	partial := &PartialError{Summary: GapSummary{ItemGaps: 1}, Encrypted: true}
	if strings.Contains(partial.Error(), "attachments-report.tsv") || !strings.Contains(partial.Error(), "MailArchive") {
		t.Fatalf("an encrypted partial run's text points at a report it has none of: %q", partial.Error())
	}
	if p := (&PartialError{Summary: GapSummary{ItemGaps: 1}}); !strings.Contains(p.Error(), "attachments-report.tsv") {
		t.Fatalf("the positive twin: a plaintext partial run names its report: %q", p.Error())
	}
}

// covers: MA-737, R37
// A REAL partial encrypted run — a Full re-run whose every message write is refused
// (EACCES), so its items fail — returns ErrPartial whose text points at MailArchive,
// never at attachments-report.tsv, which an encrypted archive has none of. The
// positive twin: the same partial run on a plaintext archive names its report.
func TestARealPartialEncryptedRunPointsAtMailArchive(t *testing.T) {
	src := encryptedSourcePST(t)
	orig := exporterSetup
	t.Cleanup(func() { exporterSetup = orig })
	partialRerun := func(opts Options, fault func(path string) error) error {
		t.Helper()
		if _, err := Run(context.Background(), opts, discard(), nil); err != nil {
			t.Fatal(err)
		}
		exporterSetup = func(e *export.Exporter) { e.BeforeWrite = fault }
		defer func() { exporterSetup = orig }()
		opts.Mode = export.Full // re-writes every archived message
		_, err := Run(context.Background(), opts, discard(), nil)
		if !errors.Is(err, ErrPartial) {
			t.Fatalf("want a partial run, got %v", err)
		}
		return err
	}

	plain := tmpDir(t)
	err := partialRerun(Options{Inputs: []string{src}, Out: plain}, func(path string) error {
		return &fs.PathError{Op: "write", Path: path, Err: syscall.EACCES}
	})
	if !strings.Contains(err.Error(), "attachments-report.tsv") {
		t.Fatalf("the positive twin: a plaintext partial run names its report: %q", err)
	}

	out, key := encryptedOut(t)
	err = partialRerun(Options{Inputs: []string{src}, Out: out, Key: key}, func(string) error {
		return sealedWriteErr(out, syscall.EACCES)
	})
	if strings.Contains(err.Error(), "attachments-report.tsv") || !strings.Contains(err.Error(), "MailArchive") {
		t.Fatalf("a real partial encrypted run's text points at a report it has none of: %q", err)
	}
}
