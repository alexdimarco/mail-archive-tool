package app

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
)

// reasonFor returns the status reason carrying code, or "" when none does.
func reasonFor(rep health.Report, code string) string {
	for i, c := range rep.Codes {
		if c == code {
			return rep.Reasons[i]
		}
	}
	return ""
}

// followReindexRemedy runs the reindex command a status/summary remedy names:
// `reindex -rebuild -out` runs Rebuild, a plain `reindex -out` runs Reindex. It
// fails the test when the text names neither.
func followReindexRemedy(t *testing.T, remedy, out string) {
	t.Helper()
	logger := log.New(io.Discard, "", 0)
	switch {
	case strings.Contains(remedy, "mailarchive reindex -rebuild -out"):
		if _, err := Rebuild(out, logger); err != nil {
			t.Fatalf("following the remedy (reindex -rebuild): %v", err)
		}
	case strings.Contains(remedy, "mailarchive reindex -out"):
		if _, _, err := Reindex(out, logger); err != nil {
			t.Fatalf("following the remedy (reindex): %v", err)
		}
	default:
		t.Fatalf("the remedy names no reindex command: %q", remedy)
	}
}

// covers: MA-389, R8, R18, S33
// When the search index holds fewer messages than the manifest, status's
// index_behind WARN names a remedy that actually heals it (F50): following it
// restores every missing row and clears the WARN. Plain `reindex` only prunes
// rows whose file is gone and can never add one, so the remedy must be
// `reindex -rebuild`. The gap is produced for real: one message's index insert
// is refused (fault injection) while all three are exported.
func TestIndexBehindRemedyHeals(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	logger := log.New(io.Discard, "", 0)

	// Positive twin: a healthy archive carries no index_behind.
	okOut := tmpDir(t)
	if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: okOut, Mode: export.Incremental, Index: true}, logger, nil); err != nil {
		t.Fatal(err)
	}
	if r := reasonFor(health.Assess(health.Gather(okOut, ""), time.Now()), "index_behind"); r != "" {
		t.Fatalf("healthy archive reports index_behind: %s", r)
	}

	out := tmpDir(t)
	injectIndexFault(t, out, "msg 1", "ABORT")
	res, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: true}, logger, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Indexed != 2 || res.ManifestSize != 3 {
		t.Fatalf("fixture: indexed=%d manifest=%d, want 2 of 3", res.Indexed, res.ManifestSize)
	}
	in := health.Gather(out, "")
	remedy := assure.Reached(t, reasonFor(health.Assess(in, time.Now()), "index_behind"), "the index_behind reason for 2 of 3 indexed")

	followReindexRemedy(t, remedy, out)

	after := health.Gather(out, "")
	if r := reasonFor(health.Assess(after, time.Now()), "index_behind"); r != "" || after.Indexed != after.Messages {
		t.Fatalf("following the index_behind remedy %q did not heal the index: indexed %d of %d (%s)", remedy, after.Indexed, after.Messages, r)
	}
}
