package app

import (
	"errors"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/source"
	"mail-archive-tool/internal/state"
)

// covers: MA-321, R10, R15, R1, S46
// MA-321's EIO case end to end: a maildir file whose read fails with an I/O error (the
// read is failed through source.InjectMaildirReadFaults; no file on a healthy disk does
// it) is the SAME gap as a permission failure, one fillable file-unreadable at
// maildir:Inbox/<unique> with no record, and the run says the DISK's remedy. Once the
// disk reads again the next run archives the file and the gap closes as recovered.
func TestMaildirFileEIOIsTheSameGapWithTheDiskRemedy(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("maildir names carry a colon")
	}
	store := filepath.Join(tmpDir(t), "mail")
	maildirStore(t, store, "Inbox/cur/a:2,S", "Inbox/cur/b:2,S", "Inbox/cur/c:2,S", "Work/cur/d:2,S")
	restore := source.InjectMaildirReadFaults(func(rel string) error {
		if rel == "Inbox/cur/b:2,S" {
			return &fs.PathError{Op: "read", Path: rel, Err: errors.New("input/output error")}
		}
		return nil
	})
	defer restore()

	out := tmpDir(t)
	res, err := runLocal(t, out, store)
	if !errors.Is(err, ErrPartial) || !res.Partial {
		t.Fatalf("run 1 err = %v; want ErrPartial", err)
	}
	if n := recordCount(t, out); n != 3 {
		t.Errorf("%d records; want a, c and d only: the unreadable file mints no record (FC7)", n)
	}
	gaps := assure.Reached(t, ledgerOf(t, out), "the ledger")
	g := gaps[0]
	if len(gaps) != 1 || g.Reason != "file-unreadable" || g.Class != state.ClassFillable || g.Scope != state.GapScopeItem || g.Locator != "maildir:Inbox/b" {
		t.Fatalf("ledger %s; want one fillable item gap file-unreadable at maildir:Inbox/b", gapReasons(gaps))
	}
	lines := strings.Join(res.Gaps.Lines, "\n")
	if !strings.Contains(lines, "check the drive") || strings.Contains(lines, "fix the permissions") {
		t.Errorf("summary %q; want the disk remedy (check the drive), not the permission one", lines)
	}

	restore()
	ageLedger(t, out, 48*time.Hour)
	res, err = runLocal(t, out, store)
	if err != nil || res.Partial {
		t.Fatalf("run 2 err %v, partial %v; want clean", err, res.Partial)
	}
	if recordCount(t, out) != 4 || len(ledgerOf(t, out)) != 0 || res.Gaps.Recovered != 1 {
		t.Errorf("%d records, ledger %s, recovered %d; want b archived and its gap closed as recovered", recordCount(t, out), gapReasons(ledgerOf(t, out)), res.Gaps.Recovered)
	}
}
