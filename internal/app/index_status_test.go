package app

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/health"
)

// covers: MA-391, R18, R8, S33, S29
// status fails closed on a damaged or lost search index while the manifest is
// intact (F82): a search.db that is present but unreadable is a RED
// (index_unreadable) naming the file and the rebuild; a search.db that is gone
// from an indexed archive is a WARN (index_missing) naming the rebuild; each
// remedy, followed, restores every row and clears the reason. An archive the
// operator deliberately builds without an index (-index=false) is not warned.
func TestStatusFlagsDamagedOrMissingIndex(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	logger := log.New(io.Discard, "", 0)
	codes := func(out string) (health.Input, health.Report) {
		in := health.Gather(out, "")
		return in, health.Assess(in, time.Now())
	}
	build := func(index bool) string {
		out := tmpDir(t)
		if _, err := Run(context.Background(), Options{Inputs: []string{src}, Out: out, Mode: export.Incremental, Index: index}, logger, nil); err != nil {
			t.Fatal(err)
		}
		return out
	}
	dbPath := func(out string) string { return filepath.Join(out, "search.db") }

	// Positive twin: a healthy indexed archive carries neither reason.
	okOut := build(true)
	if _, rep := codes(okOut); reasonFor(rep, "index_unreadable") != "" || reasonFor(rep, "index_missing") != "" {
		t.Fatalf("healthy archive flagged: %v", rep.Reasons)
	}

	// Present but unreadable: RED naming the file and the rebuild.
	corrupt := build(true)
	removeDBFiles(dbPath(corrupt))
	junk := bytes.Repeat([]byte("this is not a sqlite database "), 200)
	if err := os.WriteFile(dbPath(corrupt), junk, 0o644); err != nil {
		t.Fatal(err)
	}
	_, rep := codes(corrupt)
	red := assure.Reached(t, reasonFor(rep, "index_unreadable"), "the index_unreadable reason for a corrupt search.db")
	if rep.Posture != "RED" || !strings.HasPrefix(red, "RED:") || !strings.Contains(red, dbPath(corrupt)) || !strings.Contains(red, "mailarchive reindex -rebuild -out") {
		t.Fatalf("corrupt index: posture %s, reason %q — want a RED naming %s and `reindex -rebuild`", rep.Posture, red, dbPath(corrupt))
	}
	followReindexRemedy(t, red, corrupt)
	if in, rep := codes(corrupt); reasonFor(rep, "index_unreadable") != "" || in.Indexed != in.Messages {
		t.Fatalf("following %q left indexed %d of %d (%v)", red, in.Indexed, in.Messages, rep.Reasons)
	}

	// Gone from an indexed archive: WARN naming the rebuild.
	lost := build(true)
	removeDBFiles(dbPath(lost))
	_, rep = codes(lost)
	warn := assure.Reached(t, reasonFor(rep, "index_missing"), "the index_missing reason for a deleted search.db")
	if !strings.HasPrefix(warn, "WARN:") || !strings.Contains(warn, "mailarchive reindex -rebuild -out") {
		t.Fatalf("missing index: reason %q — want a WARN naming `reindex -rebuild`", warn)
	}
	followReindexRemedy(t, warn, lost)
	if in, rep := codes(lost); reasonFor(rep, "index_missing") != "" || in.Indexed != in.Messages {
		t.Fatalf("following %q left indexed %d of %d (%v)", warn, in.Indexed, in.Messages, rep.Reasons)
	}

	// Deliberately unindexed (-index=false): no index_missing.
	plain := build(false)
	if _, err := os.Stat(dbPath(plain)); !os.IsNotExist(err) {
		t.Fatalf("an -index=false run created search.db: %v", err)
	}
	if _, rep := codes(plain); reasonFor(rep, "index_missing") != "" {
		t.Fatalf("an -index=false archive is warned index_missing: %v", rep.Reasons)
	}
}
