package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/export"
	"mail-archive-tool/internal/model"
	"mail-archive-tool/internal/state"
)

// mboxStore writes a Thunderbird-style store of one mbox file per name, each with
// perFolder messages, and returns its directory.
func mboxStore(t *testing.T, parent, dirName string, names []string, perFolder int) string {
	t.Helper()
	dir := filepath.Join(parent, dirName)
	for _, name := range names {
		var b strings.Builder
		for i := 0; i < perFolder; i++ {
			b.WriteString("From alice@example.com Mon Mar  3 09:00:00 2025\n")
			b.WriteString(strings.ReplaceAll(p14Plain(fmt.Sprintf("%s message %d", name, i), fmt.Sprintf("%s-%d", name, i)), "\r\n", "\n"))
			b.WriteString("\n")
		}
		p14Write(t, filepath.Join(dir, name), b.String())
	}
	return dir
}

// failWrites installs a fault on the exporter's page writes: fail(n) is asked with
// the 1-based count of page writes so far and returns the error that write fails
// with (nil: it succeeds).
func failWrites(t *testing.T, fail func(n int, path string) error) {
	t.Helper()
	n := 0
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(path string) error {
			if !strings.HasSuffix(path, ".html") {
				return nil
			}
			n++
			return fail(n, path)
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
}

func pathErr(path string, errno syscall.Errno) error {
	return &fs.PathError{Op: "write", Path: path, Err: errno}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// Folder-unwritable, local (N1, FC12): an mbox store of folders A, B and C with a
// regular file where B's output folder should be archives A and C and records ONE
// folder-unwritable gap — fillable, Skipped = the folder's items not archived this run
// (not accumulated across runs) — and the run is partial. Remove the file and the next
// run archives B, the gap closes, and the run is clean.
func TestFolderUnwritableIsolatesTheFolder(t *testing.T) {
	parent := tmpDir(t)
	store := mboxStore(t, parent, "mstore", []string{"A", "B", "C"}, 3)
	tok := tokenOf(t, store)
	out := tmpDir(t)
	blocker := filepath.Join(out, tok, "B")
	if err := os.MkdirAll(filepath.Dir(blocker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(blocker, []byte("a file, not a folder"), 0o644); err != nil {
		t.Fatal(err)
	}

	res, err := runLocal(t, out, store)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run 1 err = %v; want ErrPartial", err)
	}
	m := loadArchive(t, out)
	if m.Len() != 6 || res.Stats.Exported != 6 {
		t.Errorf("%d records, %d exported; want the 6 messages of A and C", m.Len(), res.Stats.Exported)
	}
	gaps := m.CaptureGapsSorted()
	if len(gaps) != 1 {
		t.Fatalf("ledger %+v; want one folder gap, not one per message", gaps)
	}
	g := gaps[0]
	if g.Reason != "folder-unwritable" || g.Scope != state.GapScopeFolder || g.Class != state.ClassFillable || g.Folder != "B" || g.Skipped != 3 || g.Runs != 1 {
		t.Errorf("gap %+v; want fillable folder-unwritable at B with Skipped 3, Runs 1", g)
	}
	if res.Gaps.FolderGaps != 1 || res.Gaps.ItemGaps != 0 || res.Stats.ExportFaults < 1 {
		t.Errorf("summary %+v, export faults %d; want one folder gap", res.Gaps, res.Stats.ExportFaults)
	}
	if lines := res.Gaps.Lines; len(lines) != 1 || !strings.Contains(lines[0], "could not be written to "+out) {
		t.Errorf("summary lines %q; a destination-side reason names the archive location", lines)
	}

	// Still blocked: the same single gap, Skipped counted for THIS run only.
	if _, err := runLocal(t, out, store); !errors.Is(err, ErrPartial) {
		t.Fatalf("run 2 err = %v; want ErrPartial", err)
	}
	if g := ledgerOf(t, out); len(g) != 1 || g[0].Skipped != 3 {
		t.Errorf("after run 2: %+v; want the one gap, Skipped 3 (reset per run)", g)
	}

	// The remedy followed: remove the file.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	res, err = runLocal(t, out, store)
	if err != nil || res.Partial {
		t.Fatalf("run 3: err %v, partial %v; want nil, false", err, res.Partial)
	}
	if loadArchive(t, out).Len() != 9 || len(ledgerOf(t, out)) != 0 || res.Gaps.ClosedGone != 1 {
		t.Errorf("%d records, %d gaps, closed %d; want B archived (9), the gap closed", loadArchive(t, out).Len(), len(ledgerOf(t, out)), res.Gaps.ClosedGone)
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// Run-fatal twin pairs (FC12, assure.Refused): a full drive (ENOSPC), an exceeded
// quota (EDQUOT) or a read-only location (EROFS), injected through the exporter's
// write seam, ends the run with an exit-1 error naming the drive and the remedy —
// not a partial outcome, no gap, and NO further writes: the second source is never
// read. The twin, the same two sources on a healthy disk, runs to nil.
func TestRunFatalVolumeErrorsStopTheRun(t *testing.T) {
	parent := tmpDir(t)
	first := filepath.Join(parent, "one", "Inbox")
	second := filepath.Join(parent, "two", "Inbox")
	maildirMessages(t, first, 6)
	maildirMessages(t, second, 6)

	healthy := tmpDir(t)
	if res, err := runLocal(t, healthy, first, second); err != nil || res.Partial || loadArchive(t, healthy).Len() == 0 {
		t.Fatalf("twin: err %v, partial %v; want a clean run", err, res.Partial)
	}

	for _, tc := range []struct {
		errno syscall.Errno
		names []string
	}{
		{errFull, []string{"the archive drive is full", "free space", "Messages written before this point are kept"}},
		{errQuota, []string{"the archive drive is full", "free space"}},
		{errReadOnly, []string{"is read-only", "make it writable"}},
	} {
		t.Run(tc.errno.Error(), func(t *testing.T) {
			out := tmpDir(t)
			failWrites(t, func(n int, path string) error {
				if n >= 3 {
					return pathErr(path, tc.errno)
				}
				return nil
			})
			res, err := runLocal(t, out, first, second)
			code, msg := refusalOf(err)
			assure.Refused(t, code, msg, assure.Code(1), assure.Names(append(tc.names, out)...),
				assure.NoSideEffect(func() bool {
					return countHTML(messageFiles(t, out)) == 2 && len(ledgerOf(t, out)) == 0
				}))
			if errors.Is(err, ErrPartial) || res.Partial {
				t.Errorf("a run-fatal fault must not end partial: %v", err)
			}
			var fatal *RunFatalError
			if !errors.As(err, &fatal) || !errors.Is(err, tc.errno) {
				t.Errorf("err %T %v; want a *RunFatalError wrapping %v", err, err, tc.errno)
			}
			if loadArchive(t, out).Len() != 2 {
				t.Errorf("%d records; want the 2 written before the fault, and the second source never read", loadArchive(t, out).Len())
			}
		})
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// `-out` removed mid-walk (FC12): the archive location is gone, so the run ends exit 1
// with "the archive location … is unavailable" and the remedy — never a folder of
// folder-unwritable gaps, never a partial outcome.
func TestArchiveRemovedMidWalkIsRunFatal(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 6)
	out := filepath.Join(tmpDir(t), "archive")
	failWrites(t, func(n int, path string) error {
		if n == 3 {
			return os.RemoveAll(out) // the drive goes away; this write then fails by itself
		}
		return nil
	})
	res, err := runLocal(t, out, src)
	code, msg := refusalOf(err)
	assure.Refused(t, code, msg, assure.Code(1), assure.Names(out, "is unavailable", "reconnect the drive or share and re-run"))
	if errors.Is(err, ErrPartial) || res.Partial || res.Gaps.FolderGaps != 0 || res.Gaps.ItemGaps != 0 {
		t.Errorf("partial %v, gaps %+v: a vanished archive is not a partial outcome and records no folder-unwritable rows", res.Partial, res.Gaps)
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// A SafeToken refusal ends the run, exit 1, naming the token: a store token that is
// not one safe path segment (a tampered manifest) would let a write escape the archive,
// and every later item of that store would be refused the same way. Nothing is written.
func TestUnsafeStoreTokenIsRunFatal(t *testing.T) {
	out := tmpDir(t)
	m := loadArchive(t, out)
	lr := &localRun{exp: &export.Exporter{OutDir: out, Manifest: m, Log: discard()}, out: out, runAt: time.Now().UTC(), logger: discard()}
	src := &sourceRun{lr: lr, token: "../escape", path: "old.pst"}
	msg := &model.Message{Subject: "hello", InternetMessageID: "<x@y>", PlainBody: "body"}
	err := src.handle(context.Background(), nil, []string{"Inbox"}, msg)
	code, text := refusalOf(err)
	assure.Refused(t, code, text, assure.Code(1), assure.Names("../escape", "not a single safe path segment"),
		assure.NoSideEffect(func() bool { return len(messageFiles(t, out)) == 0 && m.Len() == 0 }))
	var fatal *RunFatalError
	if !errors.As(err, &fatal) || !errors.Is(err, export.ErrUnsafeToken) {
		t.Errorf("err %T %v; want a run-fatal wrapping ErrUnsafeToken", err, err)
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// The local breaker (FC12): 50 consecutive failed write attempts end the source with
// an error naming it — item writes, and each FOLDER's first attempt (its later items
// are not attempted) — and a success in between resets the count. The gaps kept are
// the ones it recorded before it stopped.
func TestLocalBreakerStopsAfterFiftyFailedWrites(t *testing.T) {
	t.Run("item writes", func(t *testing.T) {
		src := filepath.Join(tmpDir(t), "Inbox")
		maildirMessages(t, src, 60)
		out := tmpDir(t)
		failWrites(t, func(n int, path string) error { return pathErr(path, syscall.EACCES) })
		_, err := runLocal(t, out, src)
		code, msg := refusalOf(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("stopped reading", src, "after 50 writes in a row failed", "permission denied", "re-run"))
		g := ledgerOf(t, out)
		if len(g) != 50 {
			t.Fatalf("%d gaps; want the 50 failed attempts (the 51st message is never tried)", len(g))
		}
		for _, x := range g {
			if x.Reason != "export-failed" || x.Class != state.ClassFillable || x.Scope != state.GapScopeItem {
				t.Fatalf("gap %+v; want fillable item export-failed", x)
			}
		}
	})
	t.Run("a success resets the count", func(t *testing.T) {
		src := filepath.Join(tmpDir(t), "Inbox")
		maildirMessages(t, src, 120)
		out := tmpDir(t)
		failWrites(t, func(n int, path string) error {
			if n%50 == 0 {
				return nil
			}
			return pathErr(path, syscall.EACCES)
		})
		res, err := runLocal(t, out, src)
		if !errors.Is(err, ErrPartial) || res.Stats.Exported != 2 {
			t.Errorf("err %v, exported %d; want ErrPartial: 49 failures, a success, 49 failures never reach 50 in a row", err, res.Stats.Exported)
		}
	})
	t.Run("each folder's first attempt counts", func(t *testing.T) {
		parent := tmpDir(t)
		names := make([]string, 60)
		for i := range names {
			names[i] = fmt.Sprintf("F%02d", i)
		}
		store := mboxStore(t, parent, "mstore", names, 2)
		tok := tokenOf(t, store)
		out := tmpDir(t)
		for _, n := range names {
			p14Write(t, filepath.Join(out, tok, n), "a file, not a folder")
		}
		_, err := runLocal(t, out, store)
		code, msg := refusalOf(err)
		assure.Refused(t, code, msg, assure.Code(1), assure.Names("after 50 writes in a row failed", store))
		if g := ledgerOf(t, out); len(g) != 50 {
			t.Errorf("%d gaps; want 50 folder gaps, one per folder's first attempt", len(g))
		}
	})
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// The local checkpoint, and the lock re-check inside it, are driven by items processed
// PLUS faults (FC12, §4.3): a PST whose first sixteen messages are all unreadable saves
// its manifest — ledger entries in it — before the seventeenth is exported, instead of
// not at all until the first export.
func TestFaultsDriveTheLocalCheckpoint(t *testing.T) {
	data := pstFixtureData(t)
	nids := pstMessageNIDs(t, data)
	mut := data
	for _, nid := range nids[:16] {
		mut = corruptNode(t, mut, nid)
	}
	path := writeData(t, tmpDir(t), "old.pst", mut)
	out := tmpDir(t)
	mpath := filepath.Join(out, ".mailarchive-manifest.json")
	onDisk := -1
	exporterSetup = func(e *export.Exporter) {
		e.BeforeWrite = func(string) error {
			if onDisk < 0 {
				if m, err := state.Load(mpath); err == nil {
					onDisk = m.GapCount()
				}
			}
			return nil
		}
	}
	t.Cleanup(func() { exporterSetup = nil })
	_, err := Run(context.Background(), Options{Inputs: []string{path}, Out: out, CheckpointEvery: 2}, discard(), nil)
	if !errors.Is(err, ErrPartial) {
		t.Fatalf("run err = %v; want ErrPartial", err)
	}
	if onDisk < 2 {
		t.Errorf("the manifest on disk before the first export held %d gaps; want a checkpoint driven by the faults (at least 2)", onDisk)
	}
}

// covers: MA-319, R1, R6, R13, S46
// A completed walk closes only the gaps it could have SEEN again (§4.3 step 3): the
// PST reader reports every kind of fault it meets, so a gap of its own that it no
// longer meets is "no longer in the source"; the mbox, maildir and Evolution readers
// report none yet (FI-S2b), so a reader-stage gap on such a store (file-unreadable)
// survives their walk — a walk that never looks says nothing — while the export-stage
// gaps the run itself observes (export-failed, folder-unwritable) are closed by any
// source's completed walk that no longer meets them.
func TestAWalkClosesOnlyWhatItCouldHaveSeen(t *testing.T) {
	src := filepath.Join(tmpDir(t), "Inbox")
	maildirMessages(t, src, 2)
	tok := tokenOf(t, src)
	out := tmpDir(t)
	m := loadArchive(t, out)
	m.Token(state.PathSourceID(src), tok)
	seed := time.Now().Add(-48 * time.Hour)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "file:Inbox/gone", Reason: "file-unreadable", Class: state.ClassFillable}, seed)
	m.ObserveGap(state.CaptureGap{Token: tok, Folder: "Inbox", Scope: state.GapScopeItem, Locator: "ident:sha:nolonger", Reason: "export-failed", Class: state.ClassFillable}, seed)
	if err := m.Save(); err != nil {
		t.Fatal(err)
	}
	res, err := runLocal(t, out, src)
	if err != nil || res.Partial {
		t.Fatalf("run: err %v, partial %v; want nil, false (neither seeded gap was observed this run)", err, res.Partial)
	}
	left := ledgerOf(t, out)
	if len(left) != 1 || left[0].Reason != "file-unreadable" || res.Gaps.ClosedGone != 1 {
		t.Errorf("ledger %+v, closed %d; want the export-stage gap closed and the reader-stage gap of a reader that reports no faults kept", left, res.Gaps.ClosedGone)
	}
}

// covers: MA-320, R1, R6, R10, R5, R12, S46
// What a run-fatal refusal says, by cause (§7.5): each names the archive location and
// its own remedy, and a vanished or failing volume is "unavailable", not "full".
func TestRunFatalWordingByCause(t *testing.T) {
	const out = "/archive"
	itemErr := &export.ItemError{Path: "/archive/s/Inbox/x.html"}
	_ = itemErr
	cases := []struct {
		kind  export.FatalKind
		names []string
	}{
		{export.FatalFull, []string{"the archive drive is full", "free space on the drive that holds " + out}},
		{export.FatalReadOnly, []string{"the archive location " + out + " is read-only", "make it writable"}},
		{export.FatalGone, []string{"the archive location " + out + " is unavailable", "reconnect the drive or share and re-run"}},
	}
	for _, c := range cases {
		err := runFatalFor(out, c.kind, &fs.PathError{Op: "write", Path: "/archive/s/x.html", Err: syscall.EIO})
		assure.Refused(t, 1, err.Error(), assure.Code(1), assure.Names(c.names...))
	}
}
