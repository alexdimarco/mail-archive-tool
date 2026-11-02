package archdelete

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"

	"mail-archive-tool/internal/assure"
)

// faultAfter reads r and fails with err once n bytes have been read: a device that gives
// way part way through a file.
type faultAfter struct {
	r   io.Reader
	n   int64
	err error
}

func (f *faultAfter) Read(p []byte) (int, error) {
	if f.n <= 0 {
		return 0, f.err
	}
	if int64(len(p)) > f.n {
		p = p[:f.n]
	}
	k, err := f.r.Read(p)
	f.n -= int64(k)
	return k, err
}

// covers: MA-1006, R36
// An I/O error while reading the manifest past its first 1 MiB — a network drive dropping, a
// failing disk, a file a scan holds part way (Windows) — is "cannot read", never "no longer an
// archive" (manifestRefusal's last return; slice 1a's re-verification): the plan names the
// manifest and the cause and changes nothing, the row's Delete… says the same, and Remove from
// list of it while it is the archive in use stays refused. The fault is injected into the read
// of a real manifest over 1 MiB. Positive twin first: the same manifest read whole is planned.
func TestAnIOErrorPastTheFirstMiBOfTheManifestIsNotCalledNoArchive(t *testing.T) {
	dir := bigArchive(t)
	manifest := filepath.Join(dir, manifestName)
	mustPlan(t, dir, testDeps())

	cause := errors.New("the device stopped answering")
	orig := manifestSource
	t.Cleanup(func() { manifestSource = orig })
	reads := 0
	manifestSource = func(f *os.File) io.Reader {
		reads++
		return &faultAfter{r: f, n: 1<<20 + 4096, err: &fs.PathError{Op: "read", Path: f.Name(), Err: cause}}
	}

	before := statTree(t, dir)
	rc, msg, c := refusalOf(t, dir, testDeps())
	if c != CaseUnreadable {
		t.Fatalf("case = %q (%s): an I/O error must not make the folder %q", c, msg, CaseNotArchive)
	}
	assure.Refused(t, rc, msg, assure.Code(1),
		assure.Names("MailArchive cannot read "+manifest, cause.Error(), "then click Delete again."),
		assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) }))
	assure.Reached(t, reads > 0, "the manifest read through the fault")

	// The row: the archive test asks the manifest whole, and says the same.
	row := AsRefusal(RowRefusal(dir, testDeps(), false))
	if row == nil || row.Case != CaseUnreadable {
		t.Fatalf("the row's Delete… must say the manifest cannot be read, got %v", row)
	}
	// Remove from list of the archive in use stays refused.
	if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the archive in use must stay refused while its manifest cannot be read, got %v", r)
	}
}

// covers: MA-1006, R36
// The same for the manifest's Stat, the other place readManifest marks the manifest open (slice
// 5's re-verification, the gap in MA-1006's cover): a Stat of the opened manifest that fails, even
// with an error that counts as "not found", is "cannot read" — not "no longer an archive", and
// with an unfinished delete of the folder in deletions.log not §5.4's no-manifest re-run. The
// row says the same, and Remove from list of it while it is the archive in use stays refused.
// Positive twin first: with the Stat answering, the folder is an ordinary Delete.
func TestAFailedStatOfTheOpenedManifestIsNotCalledNoArchive(t *testing.T) {
	dir := plainRowArchive(t, "Mail")
	manifest := filepath.Join(dir, manifestName)
	deps := testDeps()
	deps.Log = filepath.Join(t.TempDir(), LogName)
	appendRawLog(t, deps.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r1", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
	if p := mustPlan(t, dir, deps); p.Rerun != RerunNone {
		t.Fatalf("positive twin: with the Stat answering, the folder is an ordinary Delete, got re-run %v", p.Rerun)
	}

	orig := manifestStat
	t.Cleanup(func() { manifestStat = orig })
	stats := 0
	manifestStat = func(f *os.File) (os.FileInfo, error) {
		stats++
		return nil, &fs.PathError{Op: "fstat", Path: f.Name(), Err: syscall.ENOENT}
	}
	before := statTree(t, dir)
	rc, msg, c := refusalOf(t, dir, deps)
	if c != CaseUnreadable {
		t.Fatalf("case = %q (%s): a failed Stat of the opened manifest must not make the folder %q or a re-run", c, msg, CaseNotArchive)
	}
	assure.Refused(t, rc, msg, assure.Code(1),
		assure.Names("MailArchive cannot read "+manifest, "then click Delete again."),
		assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) }))
	assure.Reached(t, stats > 0, "the manifest's Stat went through the fault")
	if row := AsRefusal(RowRefusal(dir, testDeps(), false)); row == nil || row.Case != CaseUnreadable {
		t.Fatalf("the row's Delete… must say the manifest cannot be read, got %v", row)
	}
	if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
		t.Fatalf("Remove from list of the archive in use must stay refused, got %v", r)
	}
}

// covers: MA-1006, R36
// The same, for a read error that counts as "not found" (del-s5-A2-04): ENOENT, or on Windows
// ERROR_PATH_NOT_FOUND or ERROR_BAD_NETPATH when a network folder drops part way through the
// manifest. The manifest was open, so the folder is still "cannot read", never "no longer an
// archive": the plan refuses naming the manifest, the row says the same, Remove from list of
// it while it is the archive in use stays refused, and — with a delete of this folder left
// unfinished in deletions.log — the plan is not §5.4's no-manifest re-run, whose rule would
// remove the manifest itself and leave the mail unidentifiable. Positive twin first: read
// whole, with the same log, it is planned as an ordinary Delete.
func TestANotFoundErrorPartWayThroughTheManifestIsNotCalledNoArchive(t *testing.T) {
	dir := bigArchive(t)
	manifest := filepath.Join(dir, manifestName)
	deps := testDeps()
	deps.Log = filepath.Join(t.TempDir(), LogName)
	appendRawLog(t, deps.Log, map[string]any{"at": "2026-11-01T10:00:00Z", "event": "started", "run": "r1", "folder": dir, "row": dir, "kind": "plain", "identity": idOf(t, dir)})
	if p := mustPlan(t, dir, deps); p.Rerun != RerunNone {
		t.Fatalf("positive twin: read whole, the folder is an ordinary Delete, got re-run %v", p.Rerun)
	}

	causes := []error{syscall.ENOENT, fmt.Errorf("the network path dropped: %w", fs.ErrNotExist)}
	if runtime.GOOS == "windows" {
		causes = append(causes, syscall.Errno(3), syscall.Errno(53)) // ERROR_PATH_NOT_FOUND, ERROR_BAD_NETPATH
	}
	orig := manifestSource
	t.Cleanup(func() { manifestSource = orig })
	for _, cause := range causes {
		if !errors.Is(cause, fs.ErrNotExist) {
			t.Fatalf("the fault %v must count as not found, or this test proves nothing", cause)
		}
		reads := 0
		manifestSource = func(f *os.File) io.Reader {
			reads++
			return &faultAfter{r: f, n: 1<<20 + 4096, err: &fs.PathError{Op: "read", Path: f.Name(), Err: cause}}
		}
		before := statTree(t, dir)
		rc, msg, c := refusalOf(t, dir, deps)
		if c != CaseUnreadable {
			t.Fatalf("%v: case = %q (%s): a read that failed part way must not make the folder %q or a re-run", cause, c, msg, CaseNotArchive)
		}
		assure.Refused(t, rc, msg, assure.Code(1),
			assure.Names("MailArchive cannot read "+manifest, "then click Delete again."),
			assure.NoSideEffect(func() bool { return sameTree(before, statTree(t, dir)) }))
		assure.Reached(t, reads > 0, "the manifest read through the fault")
		// The row is asked without the log: a row with an unfinished delete offers Delete
		// again by design (§5.4), and the plan above is what answers it.
		if row := AsRefusal(RowRefusal(dir, testDeps(), false)); row == nil || row.Case != CaseUnreadable {
			t.Fatalf("%v: the row's Delete… must say the manifest cannot be read, got %v", cause, row)
		}
		if r := AsRefusal(unlistedAsCurrent(dir)); r == nil || r.Case != CaseCurrentArchive {
			t.Fatalf("%v: Remove from list of the archive in use must stay refused, got %v", cause, r)
		}
	}
}
