package app

import (
	"bytes"
	"context"
	"errors"
	"log"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/outlookcom"
)

// The re-verification round of the FI-S2a adversarial pass
// (docs/review-fault-isolation-s2a-adversarial.md, "Re-verification"): the tombstones
// for the fresh-eyes check's new findings.

// covers: MA-320, R10, R12
// NF3 (the sibling of S2A-AO5): the warning a run logs when one message's export
// panics names the message's folder and the panic's own words. Both can carry a data
// file's bytes (a folder name keeps C1 controls, DEL and bidi overrides through
// SanitizeSegment; a parser's panic can quote what it read), and the line reaches the
// terminal and the log raw. It is stripped where it is built, like the refusals.
func TestPanicWarningCarriesNoControlBytesFromTheFile(t *testing.T) {
	const bad = "Fo\u009bl‮de\x7fr"
	store := mboxStore(t, tmpDir(t), "mstore", []string{bad}, 3)
	out := tmpDir(t)
	seen := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if strings.HasSuffix(path, ".html") {
				if seen++; seen == 2 {
					panic("parser fault near \u009b[31m‮ and \x7f")
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	var buf bytes.Buffer
	res, err := Run(context.Background(), Options{Inputs: []string{store}, Out: out, Mode: export.Incremental}, log.New(&buf, "", 0), nil)
	if !errors.Is(err, ErrPartial) || res.Stats.ExportPanics != 1 {
		t.Fatalf("run err = %v, %d panics; want ErrPartial and 1 panic", err, res.Stats.ExportPanics)
	}
	line := ""
	for _, l := range strings.Split(buf.String(), "\n") {
		if strings.Contains(l, "recovered from panic") {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no panic warning in the log:\n%s", buf.String())
	}
	for _, r := range line {
		if r == '\u009b' || r == '‮' || r == 0x7f {
			t.Errorf("the panic warning carries %U from the file: %q", r, line)
		}
	}
	if !strings.Contains(line, "parser fault near") {
		t.Errorf("the panic warning lost its words: %q", line)
	}
}

// covers: MA-320, MA-323, R1
// NF1 (of the fresh-eyes check of eeab3d4): an outlook-export-skipped gap closes only
// when the walk's PST carries the folder, so a folder that is no longer in Outlook (it
// was deleted or renamed since), or whose name Outlook could not read when it failed
// ("folder 3"), stayed a fillable gap that nothing could ever close: every later run was
// partial, and its remedy ("open the folder in Outlook, then run again") cannot be
// followed. A PST that THIS run's Outlook export wrote is a fresh image of the mailbox:
// what it neither carries nor skips again is not in Outlook, so its complete walk closes
// the gap as no longer in the source. A PST left over from an earlier export says
// nothing about Outlook today and closes nothing, and a skip that happens again this run
// is observed again and stays.
func TestAFreshOutlookExportClosesWhatOutlookNoLongerHolds(t *testing.T) {
	data := pstFixtureData(t)
	for _, c := range []struct {
		name       string
		first      outlookcom.Skip // the run-1 skip
		fresh      bool            // run 2 comes with a fresh export of the PST
		again      bool            // run 2's export skips the same folder again
		wantClosed int
	}{
		{"the folder was deleted in Outlook", outlookcom.Skip{Account: "ann@contoso.com", Folder: "Projects", Reason: "CopyTo: 0x80040119"}, true, false, 1},
		{"a folder Outlook could not name", outlookcom.Skip{Account: "ann@contoso.com", Folder: "folder 3", Reason: "Item: 0x80004005"}, true, false, 1},
		{"the same folder is skipped again", outlookcom.Skip{Account: "ann@contoso.com", Folder: "Projects", Reason: "CopyTo: 0x80040119"}, true, true, 0},
		{"a left-over PST of an earlier export", outlookcom.Skip{Account: "ann@contoso.com", Folder: "Projects", Reason: "CopyTo: 0x80040119"}, false, false, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			pstPath := writeData(t, filepath.Join(tmpDir(t), "_outlook-pst"), "ann@contoso.com.pst", data)
			out := tmpDir(t)
			skip := c.first
			skip.Path = pstPath
			_, err := Run(context.Background(), Options{Inputs: []string{pstPath}, Out: out, Mode: export.Incremental, OutlookSkips: []outlookcom.Skip{skip}, OutlookExported: []string{pstPath}}, discard(), nil)
			if len(ledgerOf(t, out)) != 1 || (err != nil && !errors.Is(err, ErrPartial)) {
				t.Fatalf("run 1: err %v, ledger %+v; want the one skip gap", err, ledgerOf(t, out))
			}
			ageLedger(t, out, 48*time.Hour)
			opts := Options{Inputs: []string{pstPath}, Out: out, Mode: export.Incremental}
			if c.fresh {
				opts.OutlookExported = []string{pstPath}
			}
			if c.again {
				opts.OutlookSkips = []outlookcom.Skip{skip}
			}
			res, err := Run(context.Background(), opts, discard(), nil)
			if err != nil && !errors.Is(err, ErrPartial) {
				t.Fatalf("run 2: %v", err)
			}
			if res.Gaps.ClosedGone != c.wantClosed || len(ledgerOf(t, out)) != 1-c.wantClosed {
				t.Errorf("run 2 closed %d, ledger %+v; want %d closed", res.Gaps.ClosedGone, ledgerOf(t, out), c.wantClosed)
			}
		})
	}
}
