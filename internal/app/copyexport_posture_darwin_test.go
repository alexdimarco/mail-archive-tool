//go:build darwin

package app

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"mail-archive-tool/internal/assure"
)

// grantEveryone puts an inheritable "everyone allow read" entry on dir with the system's
// own tool (the test's fixture, never the product's path), or skips the test where the
// volume keeps no ACLs.
func grantEveryone(t *testing.T, dir string) {
	t.Helper()
	out, err := exec.Command("chmod", "+a", "everyone allow list,search,read,readattr,readextattr,file_inherit,directory_inherit", dir).CombinedOutput()
	if err != nil {
		t.Skipf("MA-532: this volume does not take an ACL entry here: %v: %s", err, out)
	}
}

// covers: MA-532, R29, R12, S52
// macOS, on the real operating system: a destination with an inheritable `everyone allow
// read` ACL entry is read through getattrlist and the line names everyone; the same
// directory without it reads "only you"; a destination that gains the entry between
// preview and Start makes Start refuse with R-w2, writing nothing; and an export into a
// destination whose line already named everyone proceeds, with the created root and
// marker inheriting the entry the line named.
func TestCopyExportMacOSInheritedACLIsNamed(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	if p := osInheritedPosture(dest); p.Kind != postureKindPrivate {
		t.Fatalf("a directory with no ACL reads %+v, want only you (the positive twin)", p)
	}

	// A destination that gains the entry after the preview.
	p := cePlanAt(t, e, src, ceMsgs(2), dest)
	if p.Posture.Kind != postureKindPrivate {
		t.Fatalf("the preview's line for a plain directory: %+v", p.Posture)
	}
	grantEveryone(t, dest)
	got := osInheritedPosture(dest)
	if got.Kind != postureKindReaders || !strings.Contains(got.Line, "everyone can read what you export here") {
		t.Fatalf("a directory with an inheritable everyone entry reads %+v", got)
	}
	_, err := ceRunPlan(t, e, p, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-w2" {
		t.Fatalf("Start did not refuse with R-w2: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names("everyone", "the preview said only you could", dest),
		assure.NoSideEffect(func() bool { ents, _ := os.ReadDir(dest); return len(ents) == 0 }))

	// A preview that already named everyone: the run proceeds, and the root it created
	// inherits exactly the entry the line named.
	p = cePlanAt(t, e, src, ceMsgs(2), dest)
	if p.Posture.Kind != postureKindReaders {
		t.Fatalf("the preview did not name everyone: %+v", p.Posture)
	}
	res := mustRun(t, e, p, src)
	if res.Counts.Exported != 2 || res.Stop != nil {
		t.Fatalf("an export whose line named everyone ended as %s (%+v)", res.Label, res.Stop)
	}
	if rp := osRealizedPosture(res.Root, true); rp.broaderThan(p.Posture) {
		t.Errorf("the created root's realized posture %+v is broader than the line %+v", rp, p.Posture)
	}
}
