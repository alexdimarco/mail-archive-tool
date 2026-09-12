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

// covers: MA-92, R7, S27
// Every archive carries a README.txt at its root that explains, to someone
// with no tool and no documentation, what the folders and files are, that all
// file-name and table times are UTC, how to browse and search without the
// tool, and what the report and manifest are. Run writes it; reindex restores
// it if it was deleted.
func TestArchiveCarriesReadme(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	out := tmpDir(t)
	logger := log.New(io.Discard, "", 0)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true, Pages: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(out, "README.txt")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("README.txt not written: %v", err)
	}
	s := string(data)
	for _, want := range []string{
		"index.html", "UTC", "-attachments.zip", "search.db", "SQLite",
		"attachments-report.tsv", ".mailarchive-manifest.json", "no software",
		// Post-v2 additions (D10): the verify path, the transport-headers panel,
		// and the run/verify/schedule records + attention sidecars.
		"mailarchive verify -out", "Transport headers as stored (unverified)",
		".mailarchive-lastrun.json", ".mailarchive-lastverify.json",
		".mailarchive-schedule.json", "BACKUP-NEEDS-ATTENTION.txt",
		"ARCHIVE-INTEGRITY-ATTENTION.txt",
		// go-back (slice E, design T8): the timeline file is named, serve's
		// point-in-time view is described, and the reindex redaction is spelled
		// out (delete files + reindex removes a message across all dates).
		".mailarchive-history.jsonl", "point-in-time", "mailarchive reindex -out",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("README.txt lacks %q", want)
		}
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Reindex(out, logger); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Error("reindex did not restore README.txt")
	}
}
