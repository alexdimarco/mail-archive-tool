package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

// covers: MA-1144, R18, S48
// attachments-report.tsv's row for a message a mistake in MailArchive kept out of the archive
// (export-panic) says "the original is untouched" once: its what-to-do is the mistake's remedy,
// which says it already (friction review of plain-failure-words, 2026-11-07: the row read "why: a
// mistake in MailArchive stopped it from saving this message (the original is untouched) · what
// to do: your mail is fine (the original is untouched); please report it at …"). A real run, the
// fault planted through the exporter seam. Positive twin first: the clean run writes no such row.
func TestTheReportsMistakeRowSaysTheOriginalIsUntouchedOnce(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 3)
	clean := tmpDir(t)
	if _, err := runLocal(t, clean, src); err != nil {
		t.Fatalf("the positive twin: %v", err)
	}
	if rep, _ := os.ReadFile(filepath.Join(clean, "attachments-report.tsv")); strings.Contains(string(rep), "export-panic") {
		t.Fatalf("the positive twin has a mistake row:\n%s", rep)
	}

	out := tmpDir(t)
	seen := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if strings.HasSuffix(path, ".html") {
				if seen++; seen == 2 {
					panic("renderer fault")
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	if _, err := runLocal(t, out, src); err == nil {
		t.Fatal("want a partial run")
	}
	rep, err := os.ReadFile(filepath.Join(out, "attachments-report.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, row := range strings.Split(string(rep), "\n") {
		if !strings.HasPrefix(row, "export-panic\t") {
			continue
		}
		rows++
		if n := strings.Count(row, "the original is untouched"); n != 1 || strings.Contains(row, "the original is still") {
			t.Errorf("the mistake's row says the original is untouched %d times, or adds a second note about it: %q", n, row)
		}
	}
	assure.Reached(t, rows == 1 && strings.Contains(string(rep), state.MistakeRemedy), "one mistake row with its remedy:\n"+string(rep))
}
