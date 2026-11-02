package archdelete

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
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
