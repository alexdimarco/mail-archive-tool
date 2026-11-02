package archdelete

import (
	"os"
	"path/filepath"
	"testing"

	"mail-archive-tool/internal/assure"
)

// covers: MA-980, R36
// The row refuses what the plan refuses, in the plan's order (del-s3a re-verification, R4): a
// manifest that is itself a link is refused on the row even when the caller's own read already
// found an archive (knownArchive), and a folder the Desktop was started with -out naming that is
// also a link, or also protected, says on the row what the plan says — the link or the protected
// folder, not "Restart MailArchive without -out". Positive twins first: the archive's own row is
// offered, and the -out row alone says the -out refusal on both.
func TestARowRefusesALinkedManifestAndKeepsThePlansOrder(t *testing.T) {
	dir := plainRowArchive(t, "Mail")
	deps := testDeps()
	if err := RowRefusal(dir, deps, true); err != nil {
		t.Fatalf("the positive twin: the archive's own row: %v", err)
	}
	launch := deps
	launch.Settings.LaunchOut = dir
	if err := RowRefusal(dir, launch, true); AsRefusal(err) == nil || AsRefusal(err).Case != CaseLaunchOut {
		t.Fatalf("the positive twin: the -out row alone: %v; want the -out refusal", err)
	}
	if _, _, pc := refusalOf(t, dir, launch); pc != CaseLaunchOut {
		t.Fatalf("the positive twin: the plan of the -out row alone says %s", pc)
	}

	// A manifest that is a link to a real manifest elsewhere.
	linked := plainRowArchive(t, "Linked")
	real := filepath.Join(t.TempDir(), manifestName)
	data, err := os.ReadFile(filepath.Join(linked, manifestName))
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, real, string(data))
	if err := os.Remove(filepath.Join(linked, manifestName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(linked, manifestName)); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}

	// The folder a link, and protected, each also the -out folder.
	link := filepath.Join(t.TempDir(), "MailLink")
	if err := os.Symlink(dir, link); err != nil {
		t.Skipf("links cannot be made here: %v", err)
	}
	outer := plainRowArchive(t, "Outer")
	inner := filepath.Join(outer, "Inner")
	writeManifest(t, inner, 6, map[string]string{"path:/m": "Mailbox"}, []rec{{Path: "Mailbox/Inbox/two.html"}})

	for _, c := range []struct {
		name, row, launchOut string
		protected            []string
		want                 Case
		says                 string
	}{
		{"a linked manifest", linked, "", nil, CaseLink, filepath.Join(linked, manifestName)},
		{"a link started with -out", link, link, nil, CaseLink, "is, or holds where its files are, a link: " + link},
		{"a protected folder started with -out", outer, outer, []string{outer, inner}, CaseProtected, "it is (or is inside, or holds) " + inner},
	} {
		d := testDeps()
		d.Settings.LaunchOut = c.launchOut
		if c.protected != nil {
			d.Protected = DefaultProtected(c.protected)
		}
		err := RowRefusal(c.row, d, true)
		r := AsRefusal(err)
		if r == nil || r.Case != c.want {
			t.Fatalf("%s: the row says %v; want the %s refusal", c.name, err, c.want)
		}
		assure.Refused(t, 1, r.Message, assure.Code(r.ExitCode()), assure.Names(c.says),
			assure.NoSideEffect(func() bool { return exists(filepath.Join(c.row, manifestName)) }))
		if _, msg, pc := refusalOf(t, c.row, d); pc != c.want || msg != r.Message {
			t.Errorf("%s: the plan says %s %q; the row %s %q", c.name, pc, msg, r.Case, r.Message)
		}
	}
}
