package app

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strings"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/state"
)

const (
	reportName       = "attachments-report.tsv"
	legacyReportName = "attachments-report-legacy.tsv"
)

// writeReport regenerates <out>/attachments-report.tsv from the manifest (R1):
// one row per fillable or terminal gap and per unresolved inline reference,
// classed and in a stable order, so nothing found by an earlier run is lost and
// a filled gap disappears. With nothing to report the file is removed. On the
// first run over a migrated (version-1) manifest, a report left by the old
// per-run writer is preserved as attachments-report-legacy.tsv. It returns the
// report path ("" when there is nothing to report) and the row count.
func writeReport(out string, manifest *state.Manifest, migrated bool, logger *log.Logger) (string, int) {
	path := filepath.Join(out, reportName)
	if migrated {
		legacy := filepath.Join(out, legacyReportName)
		if _, err := os.Stat(path); err == nil {
			if _, lerr := os.Stat(legacy); errors.Is(lerr, fs.ErrNotExist) {
				if rerr := os.Rename(path, legacy); rerr == nil {
					logger.Printf("kept the earlier verification report as %s", legacy)
				}
			}
		}
	}

	issues := manifest.Issues()
	if len(issues) == 0 {
		os.Remove(path)
		return "", 0
	}

	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	fmt.Fprintln(w, "kind\tclass\tfolder\tdate\tsubject\tdetail\tpath")
	rows := 0
	emit := func(kind, class string, r state.Record, detail string) {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			kind, class, tsv(r.Folder), r.Date, tsv(r.Subject), tsv(detail), r.Path)
		rows++
	}
	for _, r := range issues {
		for _, item := range r.Missing {
			emit(gapKind(item), "fillable", r, item)
		}
		for _, item := range r.Terminal {
			emit(gapKind(item), "terminal", r, item)
		}
		for _, cid := range r.Unresolved {
			emit("unresolved-inline-image", "info", r, cid)
		}
	}
	w.Flush()
	if err := export.WriteFileAtomic(path, buf.Bytes()); err != nil {
		logger.Printf("warning: could not write verification report: %v", err)
		return "", rows
	}
	return path, rows
}

func gapKind(item string) string {
	switch item {
	case state.MissingBody:
		return "missing-body"
	case state.UnknownSentinel:
		return "unknown"
	default:
		return "empty-attachment"
	}
}

func tsv(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\t", " "), "\n", " ")
}
