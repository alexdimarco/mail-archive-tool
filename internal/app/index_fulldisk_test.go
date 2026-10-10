//go:build darwin || linux

package app

import (
	"context"
	"path/filepath"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
)

// withFileSizeLimit runs fn with this process's RLIMIT_FSIZE lowered to limit
// bytes — a write past that offset fails with EFBIG, the full-disk model the
// friction walk used — and restores the limit however fn ends. The Go runtime
// does not die of SIGXFSZ (it is a notify-only signal), so the writes just
// fail. Tests in this package run one at a time, so no other test writes
// meanwhile.
func withFileSizeLimit(t *testing.T, limit uint64, fn func()) {
	t.Helper()
	var orig syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
		t.Skipf("cannot read RLIMIT_FSIZE: %v", err)
	}
	capped := orig
	capped.Cur = limit
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &capped); err != nil {
		t.Skipf("cannot lower RLIMIT_FSIZE: %v", err)
	}
	defer func() {
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &orig); err != nil {
			t.Fatalf("restore RLIMIT_FSIZE: %v", err)
		}
	}()
	fn()
}

// covers: MA-386, R12, S33
// The walked full-disk shape (F1): with writes past 24 KB refused (a full
// disk modelled by RLIMIT_FSIZE, as the friction walk did), a run's writable
// open of search.db fails when SQLite cannot grow the -shm
// (SQLITE_IOERR_SHMSIZE, "disk I/O error (4874)"). The refusal names the
// file, SQLite's cause, the full disk and the remedy (free space), never the
// rebuild, and writes nothing: search.db and the manifest keep their bytes.
// The rebuild, run on that disk anyway, names the same cause and leaves no
// search.db.rebuild behind. Followed (space freed), the next run indexes
// every record. Positive twin first: the run with room on the disk.
func TestIndexOpenRefusalNamesTheFullDiskShape(t *testing.T) {
	opts, healthy, unchanged := unwritableFixture(t, 3)
	out, idxPath := opts.Out, filepath.Join(opts.Out, "search.db")

	var runErr, rebuildErr error
	withFileSizeLimit(t, 24<<10, func() {
		_, runErr = Run(context.Background(), opts, discard(), nil)
		_, rebuildErr = Rebuild(out, discard())
	})
	code, msg := refusalOf(runErr)
	assure.Refused(t, code, msg, assure.Code(1),
		assure.Names(idxPath, "disk I/O error", "the disk holding "+out+" is full or failing", "free space", "run again"),
		assure.Forbid("reindex -rebuild", "rebuild it"),
		assure.NoSideEffect(unchanged))
	rcode, rmsg := refusalOf(rebuildErr)
	assure.Refused(t, rcode, rmsg, assure.Code(1),
		assure.Names("search.db.rebuild", "the disk holding "+out+" is full or failing", "free space"),
		assure.NoSideEffect(func() bool { return unchanged() && noRebuildTemp(out) }))

	res, err := Run(context.Background(), opts, discard(), nil)
	if err != nil || res.Indexed != res.ManifestSize || res.Indexed != healthy.Indexed {
		t.Fatalf("after freeing space: indexed=%d of %d, err=%v; want all %d", res.Indexed, res.ManifestSize, err, healthy.Indexed)
	}
}
