//go:build windows

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

// covers: MA-532, R29, R12, S52
// Windows, on the real operating system (this leg runs on the Windows CI host only): an
// inherit-only (OI|CI|IO) ACE granting BUILTIN\Users read, put on the destination with the
// system's own icacls (the test's fixture), is read through GetNamedSecurityInfo and
// named by the line though the destination's own access is unchanged; a destination that
// gains it between preview and Start makes Start refuse with R-w2, writing nothing.
func TestCopyExportWindowsInheritOnlyACEIsNamed(t *testing.T) {
	e, src := newCEEnv(t), newFakeSrc()
	dest := filepath.Join(t.TempDir(), "dest")
	os.MkdirAll(dest, 0o755)
	p := cePlanAt(t, e, src, ceMsgs(2), dest)
	for _, r := range p.Posture.Readers {
		if strings.Contains(r, "Users") {
			t.Skipf("MA-532: this temp directory already lets %s read: the case needs one that does not", r)
		}
	}
	if out, err := exec.Command("icacls", dest, "/grant", "*S-1-5-32-545:(OI)(CI)(IO)(R)").CombinedOutput(); err != nil {
		t.Skipf("MA-532: icacls could not set the ACE: %v: %s", err, out)
	}
	got := osInheritedPosture(dest)
	if got.Kind != postureKindReaders || !strings.Contains(strings.Join(got.Readers, ","), `BUILTIN\Users`) {
		t.Fatalf("a destination with an inherit-only Users ACE reads %+v", got)
	}
	_, err := ceRunPlan(t, e, p, src)
	var ref *CopyRefusal
	if !errors.As(err, &ref) || ref.ID != "R-w2" {
		t.Fatalf("Start did not refuse with R-w2: %v", err)
	}
	assure.Refused(t, ref.ExitCode(), ref.Message, assure.Code(1), assure.Names(`BUILTIN\Users`, "nothing was exported"),
		assure.NoSideEffect(func() bool { ents, _ := os.ReadDir(dest); return len(ents) == 0 }))
}
