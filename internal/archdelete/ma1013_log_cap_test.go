package archdelete

import (
	"os"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-1013, R36
// MailArchive never writes deletions.log past what it reads (the core-plan-record adversarial
// pass, finding 4): a delete's started line is refused when the log, with it and room for that
// delete's later lines, would pass logCap — step 0's stop, nothing deleted, the log unchanged and
// still readable. Before, the append had no limit: one more started line made every later read of
// the log fail "too large", the unfinished delete's own line with it. Positive twin first: a log
// with room left takes a delete whole and stays readable.
func TestTheDeletionLogIsNeverWrittenPastWhatMailArchiveReads(t *testing.T) {
	r := newRig(t, smallArchive(t, t.TempDir()))
	pad := func(path string, size int64) {
		t.Helper()
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(size); err != nil {
			t.Fatal(err)
		}
		f.Close()
	}

	pad(r.x.Log, logCap-logReserve-64<<10)
	if res := r.run(t, false); res.Status != StatusComplete {
		t.Fatalf("positive twin: a log with room left: %s", res.Message())
	}
	if _, err := readLog(r.x.Log); err != nil {
		t.Fatalf("positive twin: the log after the delete cannot be read: %v", err)
	}

	r2 := newRig(t, smallArchive(t, t.TempDir()))
	pad(r2.x.Log, logCap-logReserve)
	before := tree(t, r2.dir)
	res := r2.run(t, false)
	assure.Refused(t, res.ExitCode(), res.Message(), assure.Code(1), assure.Names(msgLogFailed),
		assure.NoSideEffect(func() bool {
			fi, err := os.Stat(r2.x.Log)
			_, rerr := readLog(r2.x.Log)
			return sameTree(before, tree(t, r2.dir)) && err == nil && fi.Size() == logCap-logReserve && rerr == nil
		}))
}
