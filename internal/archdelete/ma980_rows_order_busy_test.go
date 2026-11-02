package archdelete

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
	"mail-archive-tool/internal/lockfile"
	"mail-archive-tool/internal/util"
)

// covers: MA-980, R36
// The row says what the plan says when a folder is more than one thing (the core-plan-record
// adversarial pass, finding 3 (b)): busy is asked in the plan's place — after -out, before the
// manifest and the archive test — in the plan's words ("being deleted" when another delete holds
// the lock); the archive test comes before the capture registered under the folder's name, as in
// the plan; and a link check that fails is the plan's answer too: for the folder, "not
// reachable"; for its manifest, a link. Positive twin first: the archive's own row is offered and
// planned, with the link seam in place.
func TestARowAndItsPlanAgreeOnBusyAndOnALinkThatCannotBeTold(t *testing.T) {
	failLink := map[string]bool{}
	orig := linkLike
	linkLike = func(p string) (bool, error) {
		if failLink[p] {
			return false, errors.New("the link tag cannot be read")
		}
		return util.IsLinkLike(p)
	}
	t.Cleanup(func() { linkLike = orig })

	dir := plainRowArchive(t, "Mail")
	if err := RowRefusal(dir, testDeps(), true); err != nil {
		t.Fatalf("positive twin: the archive's own row: %v", err)
	}
	mustPlan(t, dir, testDeps())

	linked := plainRowArchive(t, "Linked")
	real := filepath.Join(t.TempDir(), manifestName)
	data, err := os.ReadFile(filepath.Join(linked, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, real, string(data))
	os.Remove(filepath.Join(linked, manifestName))
	if err := os.Symlink(real, filepath.Join(linked, manifestName)); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}
	empty := filepath.Join(t.TempDir(), "Empty")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	deleting := plainRowArchive(t, "Deleting")
	held, err := lockfile.AcquireAs(filepath.Join(deleting, lockName), "delete")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	unknownFolder := plainRowArchive(t, "Unknown folder")
	unknownManifest := plainRowArchive(t, "Unknown manifest")
	failLink[unknownFolder] = true
	failLink[filepath.Join(unknownManifest, manifestName)] = true

	busy := func(string) bool { return true }
	for _, c := range []struct {
		name  string
		row   string
		known bool
		edit  func(*Deps)
		want  Case
		says  string
	}{
		{"a linked manifest while a backup runs", linked, true, func(d *Deps) { d.Busy = busy }, CaseBusy, msgBusy},
		{"a folder that is no archive while a backup runs", empty, false, func(d *Deps) { d.Busy = busy }, CaseBusy, msgBusy},
		{"another delete holds the lock", deleting, true, func(d *Deps) { d.Busy = func(f string) bool { return lockfile.Held(filepath.Join(f, lockName)) } }, CaseBusy, msgDeleting},
		{"a folder that is no archive, the capture registered under its name", empty, false, func(d *Deps) { d.Settings.CaptureBackupFor = empty }, CaseNotArchive, msgNotArchive},
		{"a folder whose link check fails", unknownFolder, true, nil, CaseUnreachable, msgUnreachable},
		{"a manifest whose link check fails", unknownManifest, true, nil, CaseLink, filepath.Join(unknownManifest, manifestName)},
	} {
		d := testDeps()
		if c.edit != nil {
			c.edit(&d)
		}
		r := AsRefusal(RowRefusal(c.row, d, c.known))
		if r == nil || r.Case != c.want {
			t.Errorf("%s: the row says %v; want the %s refusal", c.name, r, c.want)
			continue
		}
		assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(c.says),
			assure.NoSideEffect(func() bool { return exists(c.row) }))
		if _, msg, pc := refusalOf(t, c.row, d); pc != c.want || msg != r.Message {
			t.Errorf("%s: the plan says %s %q; the row %s %q", c.name, pc, msg, r.Case, r.Message)
		}
	}
}
